# 实时推送（SSE）设计

Home 页实时更新提示。本文定义 ffdb → gRPC stream → ffweb/httpd → SSE → 浏览器的
最小可用链路，并明确事件语义、生命周期、关停顺序、前端刷新方式与测试边界。

V1 的核心原则是：**实时事件只表示“Home 可能变脏了”，不承载业务状态。** 事件可以
丢失、重复、乱序；权威状态始终由现有 `FetchFeed` / TimelineIndex 读取路径提供。

## 目标与边界

- 第一版只做登录用户 **Home 第一页** 的实时提示。
- 收到事件时只显示“有新动态”一类 dirty banner，不承诺精确的“N 条新内容”。
  Like/Comment 可能只是把已有 Entry bump 到顶部，事件数不等于新 Entry 数。
- 点击 banner 后重新拉取 **Home 最新第一页（不带 cursor）**，用服务端返回的权威
  第一页替换当前第一页，随后清除 dirty 状态。
- 事件不携带 Entry 正文，不用于构造 timeline，不做 replay，不落盘。
- 暂不做：Public/普通 feed 页推送、评论实时插入、Entry 内容流式下发、多 tab 状态
  同步、跨进程 durable queue。
- gRPC transport 从一开始使用泛化的 realtime 命名，后续 notification badge 可复用同
  一条流；V1 只实现 timeline dirty producer/consumer。

## 现状依据

- ffdb 与 ffweb 是两个进程。浏览器只连接 nginx/ffweb；ffweb 通过
  `pb.NewApiClient` 访问 ffdb。因此事件必须跨进程：

  ```text
  ffdb -> gRPC server stream -> ffweb/httpd -> SSE -> browser
  ```

- `model.FanoutTimelineActivity` 已经是正确的事件源位置，但它**当前并没有保留“实际
  moved 的 viewer 集合”**：
  - `MoveTimelineEntry` 返回 `(moved bool, error)`；
  - `FanoutTimelineActivity` 当前调用后丢弃 `moved`；
  - follower 的返回计数表示 active follower，不严格等于实际 move 数量；author 也不
    在该 follower 计数里。
- Like 的 cooldown 会导致 active viewer 被检查但 timeline 不发生 move；这种情况不
  应产生 realtime hint。
- Home cursor 是向旧内容翻页的 **older-page cursor**：命中 cursor 后会 `Next()` 再
  读取更旧的行。它不是 `since` cursor，不能拿来获取“刚出现的新内容”。
- 当前 React `Feed` 每 20s 会 `getJSON(url)` 做一次刷新。SSE 落地后必须把这条 polling
  路径与 dirty banner 统一，否则后台刷新已经拿到新内容时 banner 仍可能保持 dirty。
- 当前 ffdb 的 `ApiServer.Shutdown()` 才会设置 `shuttingDown`、关闭 `done`、等待 `wg`
  并关闭 Pebble；而进程入口当前先执行 `grpc.GracefulStop()` 再调用 `Shutdown()`。
  引入永久 server-streaming RPC 后必须拆开“发出 shutdown 信号”和“等待/关库”两个
  阶段，否则 `GracefulStop()` 可能等待一个尚未收到关闭信号的永久流。

## 总体拓扑

V1 不在 ffdb 按 viewer 维护订阅。ffdb 只有一个**进程级广播总线**；每个 ffweb 实例
建立一条 gRPC 长流并收到全部 realtime hints，viewer 过滤在 ffweb 本地完成。

```text
canonical mutation
      |
      v
MoveTimelineEntry(viewer)
      |
      | moved == true
      v
non-blocking TimelineMoveSink
      |
      v
ffdb realtime broadcaster
      |
      | broadcast to every ffweb subscriber
      v
SubscribeRealtimeEvents stream
      |
      v
ffweb eventsHub[viewer_uuid]
      |
      +------ SSE tab 1
      +------ SSE tab 2
      +------ SSE tab 3
```

这样避免了“ffdb per-viewer subscription + httpd 单条全局 gRPC stream”的拓扑冲突，
也不需要在 gRPC 上动态同步当前有哪些浏览器 viewer 在线。

即使以后部署多个 ffweb 实例，每个实例都收到全部 hint 再本地过滤即可。事件很小且是
best-effort；在当前规模下没有必要引入 Redis Pub/Sub 或按 viewer 做跨进程 routing。

## 事件模型

gRPC 面使用泛化的 realtime 类型，而不是把 transport 固定成 TimelineEvent：

```proto
// pb/api.proto 追加

enum RealtimeEventType {
  REALTIME_EVENT_UNSPECIFIED = 0;
  REALTIME_EVENT_TIMELINE_DIRTY = 1;
  // 预留后续 notification badge；V1 可先不产生该事件。
  REALTIME_EVENT_NOTIFICATIONS_DIRTY = 2;
}

message RealtimeEvent {
  string viewer_uuid = 1;
  RealtimeEventType type = 2;
  string object_uuid = 3;     // timeline dirty 时为 entry UUID，可为空
  int64 activity_at_ms = 4;
}

message SubscribeRealtimeEventsRequest {
  string subscriber_id = 1;   // ffweb 实例/进程标识，只用于日志与诊断
}

rpc SubscribeRealtimeEvents(SubscribeRealtimeEventsRequest)
    returns (stream RealtimeEvent) {}
```

语义约定：

- `TIMELINE_DIRTY` 只在某个 viewer 的 `MoveTimelineEntry` **实际 `moved == true`** 后产生。
- inactive viewer 不产生事件；它的 Home 本来就依赖下一次访问时 rebuild。
- Like cooldown 阻止 move 时不产生事件。
- 允许丢失、重复、乱序。客户端只把它折叠成一个 boolean dirty 状态。
- 不提供 event id、sequence、Last-Event-ID 或 replay。
- `viewer_uuid` 只用于 ffweb 内部分流，不下发给浏览器。
- `object_uuid` 为后续诊断/去重扩展保留；V1 浏览器不依赖它。
- realtime event 永远不是权限事实。最终 Home 内容仍由既有 TimelineIndex + read-time
  visibility 校验决定。

## ffdb：从实际 timeline move 产生 hint

### 1. 保留 `moved` 语义，不返回 viewer slice

不要把 `FanoutTimelineActivity` 从 `(int, error)` 改成 `([]uuid.UUID, error)`。
Follower 数量无界，为 SSE 再构造一个 O(follower count) 的 viewer slice 没必要。

改为给 fanout 增加一个可选、同步、**必须非阻塞**的 sink：

```go
type TimelineMoveSink func(
    viewer uuid.UUID,
    entry uuid.UUID,
    kind TimelineActivityKind,
    at time.Time,
)

func FanoutTimelineActivity(
    db *store.Store,
    entry *pb.Entry,
    activity time.Time,
    kind TimelineActivityKind,
    onMoved TimelineMoveSink,
) (int, error)
```

内部 `update(viewer)` 必须使用 `MoveTimelineEntry` 的真实返回值：

```go
moved, err := MoveTimelineEntry(...)
if err != nil {
    return false, err
}
if moved && onMoved != nil {
    onMoved(viewer, entryUUID, kind, activity)
}
return moved, nil
```

返回的 `int` 建议同步修正为 **实际 moved viewer 数量**，包含 author（如果 author 的
Home 也真的 move），使日志/测试语义与名字一致。

### 2. sink 只观察已经 commit 的 derived move

`MoveTimelineEntry` 自己通过独立 `ApplyBatch` 提交一个 viewer 的 TimelineIndex 变更。
因此 sink 必须在该调用成功且 `moved == true` 后执行。

Follower fanout 不是一个大事务：如果前 100 个 viewer 已经 move，第 101 个失败，前
100 个派生写入不会回滚。对应的前 100 个 hint 可以已经发布；不要求“整个 fanout
全成功后才发事件”。这与 hint 的 best-effort 语义一致，也避免为了延迟发布而缓存
viewer 列表。

### 3. 保持 model API 的兼容 wrapper

当前 `model.PutEntry`、`PutLike`、`PutComment` 内部自己调用
`FanoutTimelineActivity`。realtime sink 不能只在 RPC 返回后补发，否则 server 层拿不到
每个实际 moved viewer。

实现时保留现有无 sink 的 wrapper，供 migration/rebuild/测试等旧调用方使用；新增
带 timeline sink 的 variant 或内部共享函数。示意：

```go
func PutEntry(db *store.Store, entry *pb.Entry) (store.Key, error) {
    return PutEntryWithTimelineSink(db, entry, nil)
}

func PutEntryWithTimelineSink(
    db *store.Store,
    entry *pb.Entry,
    sink TimelineMoveSink,
) (store.Key, error) {
    // canonical write ...
    // FanoutTimelineActivity(..., sink)
}
```

Like/Comment 同理：保留当前 public wrapper/created-hook 语义，内部共享实现增加可选
`TimelineMoveSink`。不要让 rebuild、migration、audit 工具产生 realtime 事件。

server 的 `PostEntry` / `LikeEntry` / `CommentEntry` 调用带 sink 的 runtime variant；
删除 Like/Comment 暂不产生 timeline dirty（当前删除路径也不做 Home bump）。

## ffdb：进程级 realtime broadcaster

`server` 包新增一个进程内 broadcaster，subscriber 是**下游 ffweb stream**，不是
viewer：

```text
realtimeBus
  mu
  subscribers: map[subscriberID]chan *pb.RealtimeEvent
```

建议：

- 每个 subscriber channel 使用有界缓冲（例如 64）；
- `Publish` 对每个 subscriber 都使用 non-blocking send；缓冲满直接丢该 subscriber 的
  本次 hint，绝不阻塞 canonical mutation / timeline fanout；
- 一个慢 ffweb 不能拖慢其他 ffweb；
- register/unregister 与 publish 只持有很短的 mutex；
- 不持久化、不重放；
- 日志只记录 subscriber id、drop counter、连接/断开，不记录正文。

`ApiServer` 持有 broadcaster，并提供一个 `TimelineMoveSink` adapter：把
`TimelineActivityKind` 转成 `REALTIME_EVENT_TIMELINE_DIRTY` 后 non-blocking publish。

## ffdb：gRPC stream

`SubscribeRealtimeEvents`：

1. 校验 request/subscriber id；
2. 注册一个 broadcaster subscriber；
3. 循环 select：
   - subscriber channel 有事件 → `stream.Send(event)`；
   - `stream.Context().Done()` → 退出；
   - `s.done` → 退出；
4. `defer` unregister；
5. handler 生命周期可挂入 `beginBackgroundJob` / `wg`，但必须保证 `s.done` 关闭后
   能立即退出。

只有本机 ffweb 会调用该 RPC；继续遵守 ffdb gRPC 只暴露在内部网络/loopback 的现有
部署边界。

## ffdb：关停顺序

永久 streaming RPC 不能沿用当前“先 `GracefulStop`、后 `Shutdown` 才 close(done)”的
顺序。建议把现有 `Shutdown` 拆成两个幂等阶段：

```go
func (s *ApiServer) BeginShutdown() {
    // StopTaskClaims / taskCancel as appropriate
    // lifecycleMu: shuttingDown = true
    // close(done) exactly once
    // 不 wg.Wait，不关 Pebble
}

func (s *ApiServer) Shutdown() {
    s.BeginShutdown()
    s.wg.Wait()
    s.rdb.Close()
}
```

进程入口顺序：

```text
health -> NOT_SERVING
StopTaskClaims / BeginShutdown
        |
        +-- close(done)
        +-- realtime stream handlers return
        +-- background loops stop accepting new work
        v
grpc.GracefulStop()
        v
ApiServer.Shutdown() -> wg.Wait -> close Pebble
        v
close search index
```

关键不变量：**先向永久流发出退出信号，再等待 `GracefulStop()`。**

## ffweb/httpd：eventsHub

ffweb 建立一条到 ffdb 的全局 realtime stream，然后在本地按 `viewer_uuid` fanout。

`Server` 增加：

```text
eventsHub
  mu
  viewers: map[uuid.UUID]set[connection]
  totalConnections

realtime lifecycle
  realtimeCancel
  realtimeWG
```

建议不要在 `NewServer` 构造函数里隐式启动无法回收的 goroutine；由 main 显式：

```text
s.StartRealtime(...)
...
s.ShutdownRealtime()
```

### gRPC receive loop

- 建立 `SubscribeRealtimeEvents` 长流；
- **不使用** `DefaultTimeoutContext`，它是普通 RPC 的秒级 timeout；
- 断线后指数退避重连：1s 起，上限 30s，带 jitter；
- shutdown context cancel 后立即退出，不再重连；
- 收到事件后只读取 `viewer_uuid` + `type`，交给 eventsHub；
- 当前没有对应 SSE viewer 时直接丢弃；
- 不缓存离线用户事件。

## HTTP SSE 路由

挂在现有 `/a` 登录鉴权组：

```go
action.GET("/events", s.EventsHandler)
```

鉴权复用 `LoginRequired()` + session cookie。viewer 只能来自
`CurrentUserUuid(c)`，请求参数不得允许客户端指定其他 viewer UUID。

### EventsHandler

在写出响应头前先检查连接限额与 `http.Flusher` 支持；失败直接返回普通 HTTP 错误。
连接建立后：

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `X-Accel-Buffering: no`
- 首帧写：

  ```text
  retry: 3000
  :ok

  ```

- 每 20–25s 写 heartbeat comment（例如 `:ping\n\n`），每次 flush；
- `c.Request.Context().Done()` 时 unregister 并返回；
- 单用户最多 3 条 SSE 连接；
- 全局最多 512 条；
- 单连接使用有界 channel（例如 32）；
- 慢消费者 channel 满时让该连接退出，浏览器随后自动重连，不扩大内存队列。

为了避免“publisher 关闭 channel 与并发 send”一类竞态，连接对象可使用独立 `done`
信号；hub 在慢消费者时标记/注销连接并 signal done，handler 自己返回并完成资源释放，
不要依赖并发 close 数据 channel。

### 浏览器 SSE payload

V1 只需要 dirty bit，因此不下发 viewer UUID、entry UUID 或计数：

```text
event: timeline-dirty
data: {}

```

如果短时间收到 20 个事件，React 仍只保存 `homeDirty = true`。

## nginx

`conf/nginx_http.conf`、`conf/nginx_https.conf` 在通用 `location /` 之外为 SSE 单独开口：

```nginx
location /a/events {
    proxy_pass http://%(ffweb_bind)s;
    proxy_http_version 1.1;
    proxy_set_header Connection "";

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    # X-Forwarded-Proto / X-Scheme 等保持与所在模板主 location 一致

    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
}
```

`proxy_buffering off` 是关键项；`X-Accel-Buffering: no` 由应用再做一层防护。
Heartbeat 必须明显短于 `proxy_read_timeout` 与任何中间代理的 idle timeout。

## 前端：放在现有 Feed 状态边界内

不要在 `index.jsx` 再挂一个与 `App/Feed` 平行的独立 React root 来管理 Home banner。
独立 root 无法自然更新 `Feed` 自己的 state，最后会被迫用全局事件或 DOM 桥接。

V1 直接把 realtime 状态放进现有 `Feed`（或它调用的 hook/component）里：

```text
Feed state
  feed entries
  homeDirty
  realtime connection state
        |
        +-- HomeDirtyBanner
        +-- refreshNewestHome()
```

### 明确 Home 第一页身份

不要长期用 `show_share` 充当“这是 Home”的隐式 page identity。HomeHandler 明确给
appData 增加一个字段，例如：

```text
realtime_home = true
```

仅在登录用户 Home **第一页** 设置：

```text
req.Cursor == "" && req.Start == 0
```

older cursor page、legacy `?start=N`、Public、普通 feed 都为 false，不建立 SSE。

### refreshNewestHome：不使用现有 cursor

收到 `timeline-dirty`：

```text
homeDirty = true
```

点击 banner：

```text
GET /                // 不带 cursor / start
  -> 现有 Home FetchFeed 最新第一页
  -> 用响应中的 feed/entries 替换当前第一页权威状态
  -> homeDirty = false
```

不要请求当前 `next_cursor`；它的语义是“从当前最后一行继续向旧内容翻页”。

V1 也不需要手工计算“新增了几条”再 prepend。重新取服务端最新第一页并替换最稳妥：
排序、Like/Comment bump、visibility、删除/隐藏等都继续由现有读取路径决定，也天然避免
Entry UUID 重复。

### polling 与 SSE 的关系

当前 20s polling 不能原样与 dirty banner 并存。建议把两者统一到同一个
`refreshNewestHome()`：

- SSE：秒级提示，设置 dirty；
- banner click：立即 `refreshNewestHome()`；
- `visibilitychange`：
  - hidden → `EventSource.close()`；
  - visible → 重建 EventSource，并立即 `refreshNewestHome()`，覆盖隐藏期间丢失的 hint；
- periodic reconciliation：保留低频兜底（例如 60s），仅在 Home 第一页且页面可见时
  调用 `refreshNewestHome()`；成功刷新时同步清除 dirty。

这样 SSE 丢事件、ffdb/ffweb 重启或代理短暂断流只会让提示变慢，最终仍会被
reconciliation 修正。

EventSource 自带 reconnect；服务端 `retry: 3000` 给出基础重连间隔。客户端不再另外
实现一套与 EventSource 冲突的网络重试状态机。

## ffweb 关停

当前 `http.Server.Shutdown(10s)` 会等待 SSE 这类长请求结束。加入 realtime 后 main 在
调用 HTTP graceful shutdown 前要先主动结束 realtime 生命周期：

```text
receive SIGTERM
    |
    v
s.ShutdownRealtime()
    +-- cancel ffdb SubscribeRealtimeEvents
    +-- signal all local SSE handlers to return
    +-- stop reconnect loop
    v
httpServer.Shutdown(ctx)
    v
wait realtimeWG
    v
close grpc.ClientConn / process exit
```

这样不会让每次部署都依赖 10s timeout 强行切断 SSE。

## 故障语义

### ffdb realtime stream 断开

- ffweb receive loop 退避重连；
- 期间 hint 丢失；
- Home 的 canonical/timeline 数据不受影响；
- 前端 visibility refresh / periodic reconciliation 最终恢复显示。

### ffweb 重启

- SSE 连接断开；
- 浏览器按 EventSource/retry 自动重连；
- 新 ffweb 重新建立 ffdb realtime stream；
- 重启窗口内事件不 replay。

### 慢浏览器

- 单连接 buffer 满 → 断开该连接；
- 不允许慢 tab 对 ffweb hub、ffdb stream 或 mutation path 形成反压。

### realtime bus 满/慢 ffweb

- ffdb broadcaster 对该 subscriber 丢 hint；
- 不阻塞 `MoveTimelineEntry` / canonical mutation；
- 不把 realtime delivery failure 返回给用户写操作。

这是与 timeline fanout 错误不同的边界：**timeline fanout 是派生数据维护的一部分，原有
错误语义保持；realtime delivery 只是 fanout 成功后的 best-effort observer。**

## 测试清单

### model

- active follower + `MoveTimelineEntry moved == true` → sink 收到该 viewer；
- inactive follower → 无 sink event；
- Like cooldown 导致 `moved == false` → 无 sink event；
- author timeline 实际 move 时也计入 moved count / sink；
- 同一 entry 的旧 activity 不向后移动 → 无 event；
- follower 扫描中途失败时，失败前已 commit 的 move 可以已经产生 hint；错误仍向上返回；
- nil sink 完全保持旧调用语义。

### ffdb server/realtime bus

- 一个 publish 广播到所有 ffweb subscribers；
- subscriber 缓冲满时 publish 不阻塞；
- 一个慢 subscriber 不影响另一个 subscriber；
- stream context cancel 后 unregister；
- `BeginShutdown` 关闭 `done` 后永久 stream 能退出；
- shutdown 顺序测试确保不会由 streaming RPC 卡住 `GracefulStop`。

### httpd

- 未登录 `/a/events` 被 `LoginRequired` 拦截；
- 登录连接收到 `text/event-stream` / no-cache / buffering headers；
- ffdb event 只分发到相同 viewer 的 SSE connections；
- 单用户第 4 条连接返回 503；
- 全局超限返回 503；
- 慢消费者会断开而不是阻塞 hub；
- receive loop 断线会退避重连；shutdown cancel 后不再重连；
- heartbeat 能 flush；request cancel 后连接从 hub 移除。

### frontend

- 仅 `realtime_home` 首页建立 EventSource；
- 收到任意数量 `timeline-dirty` 后只显示一个 dirty banner；
- 点击 banner 请求 `/`，不带 current cursor，并用最新第一页更新 Feed；
- refresh 成功清 dirty；失败保留 dirty，允许重试；
- hidden 时 close EventSource；visible 时重连并立即 refresh；
- periodic reconciliation 成功后清除已过期 dirty；
- Public/feed/older Home page 不创建 EventSource；
- 不把 editor runtime 拉入静态 reader bundle，继续满足现有 bundle/lint/typecheck 约束。

### nginx / 手动验收

- 通过真实 nginx 访问 `/a/events`，确认首帧立即到达而非被 proxy buffering；
- 两浏览器登录不同账号：A 发帖，B 若是 active follower，B Home 秒级显示 dirty banner；
- A Like/Comment 一条会实际 bump B Home 的 Entry，B 收到 dirty；cooldown 内不实际 bump
  时不要求产生事件；
- B 点击 banner 后看到权威最新 Home 第一页，且没有重复 Entry；
- 隐藏 B tab 一段时间后恢复，哪怕期间丢事件，也会通过 visible refresh 得到正确内容；
- 重启 ffdb/ffweb 后页面无需手动刷新即可恢复 realtime 连接。

## 实施阶段

1. **model + ffdb producer**：`TimelineMoveSink`、moved 语义修正、runtime mutator variant、
   realtime broadcaster；此阶段不改变浏览器行为。
2. **protobuf + gRPC stream + shutdown**：新增 `RealtimeEvent` /
   `SubscribeRealtimeEvents`，实现 ffdb stream，并先解决 `BeginShutdown -> GracefulStop ->
   Shutdown` 生命周期。
3. **ffweb hub + SSE + nginx**：全局 receive loop、本地 viewer hub、`/a/events`、连接
   限额、heartbeat、显式 ffweb realtime shutdown、nginx no-buffering。
4. **frontend**：增加 `realtime_home` page flag，在现有 Feed state 内加入 dirty banner，
   抽出 `refreshNewestHome()`，把原 20s polling 改成低频 reconciliation。
5. **观察与扩展**：记录连接数、bus/hub drop counter、gRPC reconnect 次数；稳定后再评估
   notification badge、Public/feed 页或更细粒度 realtime UI。

## 明确不做的复杂化

V1 不需要：

- Redis / NATS / Kafka；
- durable realtime event table；
- sequence / ACK / delivery guarantee；
- Last-Event-ID replay；
- ffdb per-viewer gRPC subscription；
- 浏览器直接连接 ffdb；
- WebSocket 双向协议。

只要保持“**权威数据在现有存储与 FetchFeed，SSE 只是 dirty hint**”这个边界，实时层
可以独立故障、独立重启、随时丢消息，而不会影响 Home 的正确性。
