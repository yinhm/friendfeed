# Service 聚合设计

FriendFeed 的 Service 是附着在一个 Feed 上的外部内容来源。用户可以把博客、RSS、
Atom 等来源导入自己的 Feed；Group 管理员也可以把来源导入 Group。Service 不是社交
订阅关系，不创建虚拟用户，不写 Follow/Follower。

本文记录 2.0 当前 Service 聚合契约。早期未部署的 `Subscription` 草案已经由
`Service(111)`、`ServiceState(112)` 和 `ServiceFeedIndex(113)` 取代；运行时不存在
Subscription 双写或兼容读取。

## 领域模型

模型分成三层：

```text
Service                         FeedService                     Entry
规范化外部来源                   目标 Feed 上的绑定                 导入后的本地内容
全局抓取一次                     用户或 Group 各自管理               进入目标 Feed 和 timeline

https://example.com/feed ──┬── personal-feed / blog
                           └── group-feed / news
```

- **Service**：可抓取的外部端点，按 `kind + canonical URL` 全局唯一。它只负责
  来源身份和抓取，不属于任何用户。
- **FeedService**：既有 `TableFeedService(101)` 中的一条 Feed 绑定。key 的第一个 UUID 是
  目标 Feed；目标可以是 user 或 group。FeedService 负责展示名称、启停、所有权和独立授权。
- **Entry**：由 Service 导入后属于目标 Feed。相同来源绑定到两个 Feed 时，各生成一
  条本地 Entry；其身份包含目标 Feed UUID，互不覆盖。

这种分层满足两个要求：同一 URL 只抓取一次，同时不同 Feed 可以独立添加、删除和展示
自己的 Service。Follow 继续只表达“某个 profile 关注另一个 Feed”。

## 支持范围

当前实现支持公开 Web Feed和一个内建来源：

- RSS 2.0；
- Atom 1.0；
- JSON Feed；
- 能由解析器识别的常见 RSS/RDF 变体。
- Bing Wallpaper，使用 `kind = "bing_wallpaper"`，由可信 CLI 绑定，Web 不提供创建入口。

Web Feed 格式统一使用 `kind = "web_feed"`。内建来源按 `kind` 静态分派；当前种类很少，
不引入动态插件、运行时加载或配置化 connector。以后接入 Flickr、GitHub 等 provider 时新增
小型 adapter；需要 OAuth/token 的 provider，其凭据
只存 FeedService 或后续专用凭据记录，不进入 Task payload、日志或全局 Service。

## 持久化结构

### Service（111）

```text
TableService = 111
key   = prefix(4) | service UUID(16)
value = pb.Service {
          uuid, kind, canonical_url, fetch_url,
          title, site_url, icon_url,
          created_at_ms, updated_at_ms
        }

service UUID = UniqueKeyFrom("service", kind, canonical_url)
```

URL 规范化只处理身份等价项：scheme/host 小写、默认端口移除、fragment 移除。不得随意
排序或删除 query，因为部分 Feed 的 query 有业务含义。创建前执行 SSRF URL 静态校验，
实际请求及每次 redirect 仍必须重新解析地址并校验 IP。

`canonical_url` 是不可变身份，继续决定 Service UUID；`fetch_url` 是可变抓取端点，空值回退到
`canonical_url`。成功跟随 301/308 后保存最终 `fetch_url`，不改变 UUID、绑定或历史 Entry。
302/303/307 只在当次请求内跟随。这样来源搬迁不会生成新 Service，也不会破坏已有幂等键。

`bing_wallpaper` 使用固定身份与端点，不接受操作者提供任意 URL：

```text
canonical_url = https://www.bing.com/HPImageArchive.aspx
fetch_url     = https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=10&mkt=zh-CN
```

### ServiceState（112）

```text
TableServiceState = 112
key   = prefix(4) | service UUID(16)
value = pb.ServiceState {
          service_uuid,
          etag, last_modified,
          last_fetch_ms, next_fetch_ms,
          consecutive_failures, empty_fetches,
          http_status, last_error,
          status, permanent_failures, permanent_failure_since_ms, delivery_failures,
          last_success_ms, dead_at_ms
        }
```

Service 和高频 State 分表，避免每轮条件请求重写来源元数据。`last_error` 只存截断后的安全
摘要，不得包含带 userinfo/query secret 的完整 URL 或响应正文。

`status` 取 `active/degraded/dead`；旧记录空值视为 active。状态是来源级运行状态，不改变
FeedService 的用户配置。DEAD 也不自动删除绑定或历史 Entry。

### FeedService（既有 101）

```text
TableFeedService = 101
key   = prefix(4) | target Feed UUID(16) | service ID(bytes)
value = pb.FeedService {
          id, kind, service_uuid,
          name, icon, profile, username,
          enabled, added_by_uuid,
          created, updated,
          ...provider-specific compatible fields
        }
```

`service ID` 在目标 Feed 内唯一；Web Feed 使用由 service UUID 派生的稳定 ID。现有
Twitter/OAuth FeedService 继续使用原 ID；它可以暂时没有 `service_uuid`，不强造全局来源。

OAuth 是某个本地 Feed 对外部账号的授权，不是全局来源属性，因此只属于 FeedService：

- `Service` 只保存公开、稳定、可共享的来源身份，严禁保存 token；
- `FeedService.oauth` 保持既有字段号与数据编码，个人 Feed 和 Group 的授权互相独立；
- `authorized_by_uuid` 记录谁为该 Feed 授权，Group 管理员离开或撤权时可精确处理；
- 如果以后确需多个 FeedService 共享一次授权，应新增独立 Credential 并显式引用，不能把
  token 上移到全局 Service。

### ServiceFeedIndex 索引（113）

```text
TableServiceFeedIndex = 113
key   = prefix(4) | service UUID(16) | target Feed UUID(16) | service ID(bytes)
value = nil
```

这是 `TableFeedService` 的派生反向索引，用于一次抓取后找到所有目标 Feed，也用于判断来源
是否仍有消费者。添加/删除 FeedService 时，主记录与该索引必须在同一个 Pebble batch 更新。
不另建“订阅关系”表。

删除 FeedService 只删除绑定及对应的 ServiceFeedIndex 行，不删除已经导入的 Entry，也不清理
其 Like、Comment、direct index 或 timeline 行。历史内容可能已经产生互动，批量删除既无上限
又容易误删，因此必须由另行设计、显式确认的维护命令处理；UI 的 Remove 不承担该语义。

## Entry 身份和投递

外部 item 的稳定 key 依次取：规范化 GUID、规范化 item URL、最后才是内容字段哈希。

```text
entry UUID = UniqueKeyFrom(
  "external-entry", target Feed UUID, service UUID, external item key)
```

- `ProfileUuid` 与 `FeedUuid` 都使用目标 Feed UUID；导入 Group 时 Entry 明确属于 Group，
  不伪造执行抓取的管理员为作者。
- `From` 是目标 Feed 的快照；`Via` 记录 Service 名称和来源站点 URL。
- 入库走统一的 `PostEntry`/`PutEntry` 生命周期，获得 direct index、Home/public timeline
  和 search 行为，不另写半套索引。
- 重复抓取依靠稳定 Entry UUID 幂等。一次 Service 抓取对多个 FeedService 的投递可以逐 Feed
  提交；只有全部投递成功后才推进 ServiceState。中途失败重试时，已完成部分不会重复。
- 每轮每个目标 Feed 最多导入固定数量的新 item；按发布时间从旧到新提交，避免 timeline
  顺序倒置。日期缺失时使用服务端抓取时间。
- Bing 的 `urlbase` 与日期必须严格解析；UHD 原图先经现有受控媒体存储持久化并生成
  thumbnail，Entry 只保存站内媒体 URL。图片失败时不提交该 Entry，由 Task 重试。

新绑定必须能获得近期内容。添加 FeedService 后入队一次 `feed_service.seed`：对该 Service 做一次
不带条件头的有界抓取，只投递到新目标 Feed。周期性 `service.fetch` 继续全局条件抓取并
投递到所有有效 binding。这样不需要长期保存一份完整的外部 item cache。

## Task 与调度

Task 只表示一次执行，不承载长期抓取状态：

```text
service.fetch { service_uuid }
feed_service.seed { service_uuid, target_feed_uuid, service_id }
```

- 调度器流式扫描 `ServiceState`，仅为存在有效 ServiceFeedIndex 的到期来源创建
  `service.fetch`；idempotency key 使用 `service UUID + due window`。
- Task payload 只含稳定 UUID/ID，不含 URL、Service 快照、token 或正文。
- handler 执行时读取最新 Service、ServiceState 和 FeedService。Service 被删除后，陈旧 task 是
  幂等 no-op。
- Task 是 at-least-once；抓取、Entry 写入和状态推进都必须可重试。
- 全局抓取并发和 per-host 并发由 handler pool 控制；ServiceState 的 ETag、退避和
  `next_fetch` 不复制到 Task。
- 单次抓取必须尝试全部 binding，不能让列表中第一个投递错误阻止后续健康目标。目标 Profile
  已删除或不存在属于失效配置：原子 disable FeedService 并移除 ServiceFeedIndex 后继续；
  其他写入错误汇总并使 Task 重试，不能自动停用。

Web Feed 成功无新内容时自适应放慢，从 30 分钟逐步增加到 24 小时；Bing Wallpaper
无论本轮是否有新内容，成功后均固定 24 小时再抓取。频率属于内建 connector 规则，
不写数据库配置、不开放 cron。来源失败按统一生命周期处理：

- 网络、DNS、TLS、408/425/429 和 5xx 是临时失败；Task 内重试结束后置 degraded，并按
  1 小时至 24 小时退避，仍持续探测；
- 其他 4xx、响应过大和持续无法解析是永久失败候选；每个调度周期只计一次。只有连续至少
  6 次并且从首次候选错误起已持续至少 7 天，才置 dead 并停止自动入队。临时错误会中断并
  清除此候选窗口；单次 404/410 不删除配置；
- 任意成功响应把状态恢复为 active，清空失败计数；手动 Refresh 即使对 dead 来源也会执行
  无条件探测：成功即复活为 active，探测失败同样按上述生命周期落地（临时错误置 degraded
  并退避），即用户发起的探测无论成败都使来源重新进入调度评估；
- 某个 binding 的持久投递错误不归咎于远端来源，但最终失败必须推进独立的 delivery 退避，
  不能让调度器每分钟产生新的 task。失败已持久化为来源生命周期状态后，当前 Task 正常完成，
  不再额外生成 TaskDone(DEAD)；来源保持 active，内部投递错误不得写入会返回 Web 的
  `last_error`；健康 binding 已完成的幂等投递保留；
- dead 来源只停止自动抓取，用户仍可 Disable、Remove 或 Refresh。运维通过 `inspect_service`
  查看状态；不得依赖不断增长的 TaskDone 充当来源状态。
- 删除后重新添加相同 URL 会复用原 ServiceState；若来源仍为 dead，用户需显式 Refresh 探测
  并复活，重新绑定本身不隐式改变来源状态。

## HTTP 行为与 User-Agent

请求只允许公开 `http`/`https`：拒绝 loopback、RFC1918、link-local、CGNAT 和其他
非公网地址；DNS 的所有解析结果均须通过，连接使用已校验地址；每次 redirect 重新校验。
限制 redirect 次数、响应大小和总超时，不转发 Cookie、Authorization 或用户请求头。

部分旧 Feed 会拒绝陌生 bot UA。默认使用常见浏览器兼容形式，同时诚实标识产品：

```text
Mozilla/5.0 (compatible; FriendFeed/1.0; +https://friendfeed.me/)
```

UA 是抓取器配置常量，不由用户、Service 或 Task payload 覆盖。不得冒充具体 Chrome
版本，也不得按来源维护 UA 例外表；若站点仍拒绝，应记录状态并退避。

## API 命名与授权

`SubscribeService`/`UnsubscribeService`/`ListSubscriptions` 会混淆 Follow，且无法表达
目标 Group，因此 2.0 使用：

```proto
rpc AddFeedService(AddFeedServiceRequest) returns (FeedService);
rpc RemoveFeedService(RemoveFeedServiceRequest) returns (google.protobuf.Empty);
rpc ListFeedServices(ListFeedServicesRequest) returns (ListFeedServicesResponse);
rpc SetFeedServiceEnabled(SetFeedServiceEnabledRequest) returns (FeedService);
rpc RefreshFeedService(RefreshFeedServiceRequest) returns (google.protobuf.Empty);

message AddFeedServiceRequest {
  string actor_uuid = 1;
  string target_feed_uuid = 2;
  string kind = 3;              // Web 使用 web_feed；可信 CLI 可使用内建 kind
  string url = 4;
}
```

Remove/List 使用 `target_feed_uuid` 和稳定 `service_id`。RPC 不接受客户端提交完整
Service、Service UUID、抓取状态或 Task 参数。

授权规则：

- user Feed：仅本人可以添加、删除和手动刷新 Service；
- group Feed：仅明确的 Group admin 可以管理 Service，普通 follower/可发帖成员不行；
- super 可用于运维恢复，但必须走相同审计路径；
- List 可按 Feed 可见性返回安全展示字段，永不返回 OAuth/token 或抓取错误详情。

可信 loopback CLI 可以不提供 actor 创建已注册的内建 Service；这不是用户权限模型。
ffweb 的创建 handler 始终固定 `kind = "web_feed"`，不信任表单传入的任意 kind。
已绑定的内建 Service 与 Web Feed 共用 owner/Group admin 的 Refresh、Disable 和
Remove 权限。

服务端从可信 actor principal 做授权；`actor_uuid` 是当前 loopback RPC 的过渡字段，未来
若 gRPC 对外开放必须先建立认证 principal，不能继续信任请求自报身份。

## Web 管理界面

账户 Import 页面管理当前用户 Feed 的 Service。Group 管理页面增加同一套 Service
组件，只有 Group admin 可见：

- 列出现有 Service：类型、解析后的 title、最近抓取结果和 active/degraded/dead 状态；
- 添加公开 Feed URL 后立即显示 pending；异步 seed 成功后更新解析出的 title 与状态；
- 启用/停用、删除、显式“立即刷新”；
- 不在浏览器直接抓取 URL，不显示完整内部错误或敏感 query。

前端组件以 `target_feed_uuid` 为输入复用，不能分别实现 user/group 两套 API。添加成功
立即显示 pending 状态；`feed_service.seed` 异步完成后更新最近抓取状态。

## 已完成的实施顺序

1. **纠正模型和命名**：直接替换未发布的 111/112 protobuf、表变量、model API 和
   RPC；新增 113；删除 synthetic profile、Follow/Follower 复用及 Subscription 命名。
2. **FeedService 写路径**：实现 user/group 授权，FeedService + ServiceFeedIndex 原子写，
   相同 URL 全局 Service 去重，删除最后 FeedService 后 Service 转 dormant。
3. **抓取 adapter**：统一 RSS/Atom/JSON Feed 解析、SSRF、条件请求、UA、限制和
   ServiceState。
4. **Task 投递**：实现 `service.fetch` 与 `feed_service.seed`，锁定幂等、部分投递重试、
   陈旧任务 no-op 和关停排干。
5. **Web UI**：账户 Import 与 Group admin 页面复用 Service 管理组件。
6. **运维**：audit 检查 FeedService↔ServiceFeedIndex、State↔Service、无 binding dormant
   Service；提供 inspect/refetch/disable 工具，不提供绕过授权的普通写接口。

## 运维命令

绑定 Bing Wallpaper 默认只预览：

```bash
./cli service add --kind bing_wallpaper --feed <feed-id-or-uuid>
./cli service add --kind bing_wallpaper --feed <feed-id-or-uuid> --apply
```

命令先通过 `InspectFeed` 解析 canonical Feed UUID，然后复用 `AddFeedService`、
FeedService/ServiceFeedIndex 原子写和 `feed_service.seed`。重复执行复用同一
Service 与绑定。

生产目录必须停服后操作；日常诊断优先针对一致性副本：

```bash
./tools -to <db> -c audit_store
./tools -to <db> -c inspect_service -id <service-uuid>
./tools -to <db> -c refetch_feed_service -user <target-feed-uuid> -id <service-id>
./tools -to <db> -c disable_feed_service -user <target-feed-uuid> -id <service-id>
```

`inspect_service` 不输出 OAuth。`refetch_feed_service` 只入队标准 seed task；
`disable_feed_service` 是停服维护用的显式管理员修复命令，原子更新 FeedService 与反向索引，
不对 Web 暴露，也不替代有 actor 授权的 RPC。

上述步骤已经落地并通过 Go/前端门禁。111/112 的 Subscription 草案从未部署，因此没有
Subscription 数据迁移工具，也不保留旧 API 的双写兼容层。

## 非目标

- 不把 RSS 来源建成 Profile，不通过 Follow 表表达导入；
- 不让每个绑定重复抓取相同 URL；
- 不在 2.0 镜像 Feed 中的远程图片或执行 HTML 内脚本；
- 不在本轮重构 ArchiveFeed 或 OAuth Service；
- 不引入 Redis、消息中间件或独立 scheduler 服务。
