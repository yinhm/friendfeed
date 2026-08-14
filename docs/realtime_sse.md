# 实时推送（SSE）设计

Home 页"有 N 条新内容"的实时提示。本文细化事件模型、ffdb/httpd/nginx/前端四层的
改动点、关停顺序与测试清单。

## 目标与边界

- 第一版只做登录用户的 Home：新内容到达时在页面顶部出现"有 N 条新内容"，点击后
  用现有 cursor 拉取并 prepend。
- 事件是**纯提示（hint）**：不携带 entry 内容，丢失不影响任何正确性；客户端永远
  通过既有 FetchFeed 路径取数据。
- 暂不做：Public/普通 feed 页推送、评论实时插入、entry 内容的流式下发、多 tab
  状态同步。这些在事件总线落地后都是增量扩展，不需要返工。

## 现状依据

- ffdb 与 ffweb 是两个进程：ffdb 的 gRPC 只监听 loopback（AGENTS 红线），ffweb
  （httpd，gin）通过 `pb.NewApiClient` 访问（httpd/src/server.go:34,47），浏览器
  只与 nginx/ffweb 通信。因此事件必须跨进程：ffdb → gRPC stream → ffweb → SSE →
  浏览器。
- 事件源已经存在：`model.FanoutTimelineActivity` 在 Entry/Like/Comment 提交后计算
  出"哪些活跃 viewer 的 Home 被 bump"，但目前只把数量返回给调用方，viewer 集合被
  丢弃。
- 页面形态：Home 是 SSR + React mount 双渲染（httpd/AGENTS.md），交互只能进 React
  bundle，不新增内联脚本。

## 事件模型

```text
TimelineEvent = { viewer_uuid, entry_uuid, kind, at }
kind ∈ { publish, like, comment }   // 直接复用 model.TimelineActivityKind
```

语义约定：

- 只有 fanout 实际写入派生行的 viewer 才产生事件（即 TimelineState 有效的活跃
  viewer）。inactive viewer 的缓存本来就是 stale，访问时走重建，不需要事件。
- 允许丢失：bus 缓冲溢出、httpd 断线重连、ffdb 重启都可能丢事件。客户端把事件当
  "可能脏了"信号，不依赖事件构造状态。
- 不保证跨事件有序；同 entry 的连续 bump 允许在客户端折叠为一次提示。

## ffdb 侧改动

1. **取回 bumped viewer 集合**：`FanoutTimelineActivity` 目前返回 `(int, error)`
   （model/timeline.go FanoutTimelineActivity，返回值仅是数量）。改为返回
   `([]uuid.UUID, error)`——实际更新的 viewer 列表。这是 model 包内函数签名调整，
   不涉及受保护的 gRPC 面；调用方（server PutEntry/LikeEntry/CommentEntry、
   rebuild 工具）同步适配。
2. **进程内事件总线**：`server` 包新增 eventBus：

   ```text
   subscribers: map[uuid.UUID]map[chan pb.TimelineEvent]struct{}  // per-viewer 本地订阅
   每 channel 缓冲 16；写入时缓冲满即丢（hint 语义），绝不阻塞 fanout 路径
   ```

   发布点在各 RPC 的 fanout 成功返回之后；fanout 报错则不发布（与 AGENTS"fanout
   错误必须返回"不变量一致，不产生"提示了但没写"的假事件）。
3. **新 gRPC 流（纯新增，不改既有 RPC）**：

   ```proto
   // pb/api.proto 追加
   message TimelineEvent {
     string viewer_uuid = 1;
     string entry_uuid = 2;
     int32  kind = 3;
     int64  at = 4;
   }
   rpc SubscribeTimelineEvents(Worker) returns (stream TimelineEvent) {}
   ```

   httpd 作为唯一订阅者建立一条长流；ffdb 侧把流注册进 eventBus，流结束时注销。
   订阅流的生命周期挂入既有 `beginBackgroundJob`/`wg`/`Shutdown` 设施：Shutdown 先
   取消活跃流、排干后台任务，再关 Pebble。

## httpd 侧改动

1. **eventsHub**：`Server` 增加一个字段（httpd/src/server.go:32 的结构体）：

   ```text
   eventsHub = { mu sync.Mutex; conns map[uuid.UUID]map[chan string]struct{} }
   ```

   NewServer 启动一个后台 goroutine：对 ffdb 的 `SubscribeTimelineEvents` 建立长
   流，断线指数退避重连（1s 起、上限 30s、带 jitter）；收到事件后按 viewer_uuid
   分发到本地连接 channel。该 goroutine **不使用** `DefaultTimeoutContext`（那是
   给普通 RPC 的秒级超时）。
2. **路由**：挂在既有鉴权组（httpd/main.go:176 的 `action` group）：

   ```go
   action.GET("/events", s.EventsHandler)
   ```

   鉴权复用 `LoginRequired()` + 会话 cookie；事件只发给 `CurrentUserUuid(c)` 对应
   的 viewer，不存在跨用户订阅入口。
3. **EventsHandler 细节**：
   - 首部：`Content-Type: text/event-stream`、`Cache-Control: no-cache`、
     `X-Accel-Buffering: no`（双保险，防 nginx/中间层缓冲）；
   - 先发 `retry: 3000\n\n` 和一个 `:ok` 注释行，让浏览器尽快确定重连间隔；
   - 每 25s 写心跳注释行 `:ping`（小于常见代理 60s 空闲超时）；
   - 用 `c.Writer.(http.Flusher)` 每条即 flush；`c.Request.Context().Done()` 时注销
     连接并退出 goroutine；
   - 上限：单用户最多 3 条并发连接（多 tab），全局 512 条；超限返回 503，
     EventSource 会自动重试；
   - 单连接 channel 缓冲 32 条，满则直接断开该连接（慢消费者让浏览器重连，比
     堆积内存便宜）；
   - 任何日志级别不记录事件内容（hint 也不含敏感数据，但保持零正文纪律）。
4. **payload**：SSE data 只放 JSON `{"n":1}` 级别的增量提示，或干脆空 data 的
   `refresh` 事件——内容由客户端经既有接口拉取。

## nginx 改动

`conf/nginx_http.conf`、`conf/nginx_https.conf` 在通用 `location /` 之外为 SSE 单独
开口（模板是百分号格式，deploy_nginx 渲染）：

```nginx
location /a/events {
    proxy_pass http://%(ffweb_bind)s;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
    # 其余 proxy_set_header 与主 location 相同
}
```

`proxy_buffering off` 是关键路径；缺了它事件会在 nginx 侧堆积直到缓冲满。

## 前端改动（httpd/app/src）

- 新增小组件（如 `home-events.jsx`），由 index.jsx 在**仅 Home** 时挂载——判定用
  现有 `window.appData.show_share === true`（httpd/AGENTS 规定 show_share 只在 Home
  打开），不给 Public/feed 页增加连接。
- 行为：EventSource 收到事件 → 未读计数 +1 → 渲染"有 N 条新内容"横幅；点击横幅
  用现有 cursor 模式拉第一页并 prepend（prepend 行为已有，见
  App.behavior.test.jsx 的 posted-entry prepend 用例），随后清零计数。
- 连接管理：EventSource 自带按 `retry:` 重连；额外监听
  `document.visibilitychange`——隐藏时 `close()`，可见时重建并立即拉一次增量
  （可见性间隙期间的事件可能已丢）。
- 约束：遵守 httpd/AGENTS（零 ESLint warning、vitest 覆盖计数/点击/可见性分支、
  静态 entry 不引入 editor runtime）。

## 关停与故障语义

- 计划内重启：先摘 nginx 流量或重启 ffweb（浏览器自动重连），ffdb
  `GracefulStop`→取消订阅流→`wg.Wait`→关库，顺序与现有 Shutdown 不变量一致。
- ffdb 不可用：httpd 订阅流退避重连；期间事件丢失，用户体感退化为"无实时提示"
  ——功能正确性不受影响。
- ffweb 重启：所有 SSE 断开，浏览器 3s 重连；hub 的 ffdb 订阅流随之重建。

## 测试清单

- model/server：fanout 返回 bumped viewer 集合；fanout 成功后事件入 bus、失败不
  入；bus 缓冲满丢弃不阻塞；Shutdown 取消订阅流。
- httpd：未登录访问 `/a/events` 被 LoginRequired 拦截；登录连接收到首部
  `text/event-stream`；模拟事件只到达对应 viewer；单用户第 4 条连接 503。
- 前端：vitest 覆盖计数、点击拉取、可见性开关。
- 手动验收：两个浏览器登录不同账号，A 发贴/评论 B 的 entry，B 的 Home 在秒级出现
  横幅且点击后内容正确 prepend。

## 实施阶段

1. pb 消息与流 RPC、fanout 返回值调整、eventBus 与发布点（无消费方，可独立合入）；
2. httpd hub + `/a/events` + nginx 模板；
3. 前端横幅；
4. 观察运行后评估扩展项（Public 页、feed 页、评论实时插入）。
