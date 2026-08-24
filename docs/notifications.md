# Notification 设计规范

Notification 是面向接收者（recipient）的持久化站内通知系统，用来回答：

> 最近有什么事情直接发生在我身上，或者需要我处理？

它与 Home timeline、Like/Comment interaction feed 和 FollowRequest workflow 是不同的数据域：

```text
Home
  我关注的 Feed/Group 最近发生了什么

Notifications
  哪些事件直接影响我、需要我知道

Requests
  当前还有哪些审批工作需要我处理
```

Notification 只负责告知和导航，不成为任何业务状态的权威来源。FollowRequest、Follow/Follower、
GroupAdmin、Like、Comment、ServiceState 等领域表仍是事实来源；通知被删除、trim、过期或读取失败都不能改变
领域状态。

本文定义当前已实现 Notification 的持久化、事件、接收者、未读、retention、fanout、隐私和 HTTP/UI 契约。

## 核心不变量

必须始终满足：

1. Notification 是 recipient-owned durable data，不是 TimelineIndex 的别名；
2. recipient 必须是存在、未删除的 `Profile(Type=user)`；Group、Feed 或其他对象不能成为 recipient；
3. 单 recipient 通知与产生它的领域 mutation 在同一个 Pebble batch 提交；
4. recipient 数量不天然有界的通知进入 durable Task，worker 必须 bounded、幂等、可恢复；
5. Inbox 排序统一使用 `^UnixMillis(activity_at) + notification UUID`，不引入 reverse Flake；
6. `activity_at` 是领域事件实际发生时间；`created_at_ns` 是通知真正写入 Inbox 的时间；
7. unread 使用 `created_at_ns > last_read_at_ns` 的时间水位，不使用 Flake sequence；
8. 每个 recipient 最终只保留最新 500 条通知；trim 在后台执行，不进入用户 mutation 的历史清理路径；
9. Notification 不保存 Entry/Comment 完整正文，也不保存 FeedService URL query、响应正文、错误详情或凭据；
10. deterministic notification ID 保证 mutation/task retry 不产生重复通知；
11. self action 不通知自己，例如自己给自己的 Entry 点赞/评论；
12. workflow 页面仍是 authoritative action UI，Notification 只链接过去，不复制 approve/reject 或 Service 管理逻辑。

## 已实现范围

基础 V1 的八类事件以及后续加入的 `FEED_SERVICE_FAILED` 均已实现：

| Kind | 触发条件 | Recipient | Fanout |
| --- | --- | --- | --- |
| `FOLLOW_REQUEST_RECEIVED` | private user feed / private Group 新建 pending request | user feed owner；Group 当前 admins | user=direct；Group=task |
| `FOLLOW_REQUEST_APPROVED` | pending request 被批准并建立关系 | requester | direct |
| `FOLLOW_REQUEST_REJECTED` | pending request 被明确拒绝 | requester | direct |
| `ENTRY_COMMENTED` | 新 Comment 首次创建 | Entry author user | direct |
| `ENTRY_LIKED` | Like 从不存在变为存在 | Entry author user | direct |
| `GROUP_ADMIN_ADDED` | member 从非 admin 变为 admin | target user | direct |
| `GROUP_ADMIN_REMOVED` | admin 从 admin 变为普通 member | target user | direct |
| `GROUP_MEMBER_REMOVED` | admin/super 移除普通 member | target user | direct |
| `FEED_SERVICE_FAILED` | canonical Service 进入长期 `dead` 终态 | personal Feed owner；Group 当前 admins | single durable task |

当前明确不做：

- comment thread subscribers；
- 通知同一楼层的其他 commenter；
- mention / reply-to（当前领域模型没有该事实）；
- email、Web Push、mobile push；
- Like 聚合（`Alice and 3 others liked...`）；
- 单条 mark-read/unread；
- notification preferences；
- `FOLLOW_REQUEST_CLEARED_BY_PUBLIC`；
- `GROUP_DELETED`；
- 普通 public follow 的 `NEW_FOLLOWER`。

后三类领域事件可在后续版本加入；它们不得反向污染当前 key/schema 契约。

## 数据模型

Notification 表从 120 开始；119 已由 GroupIndex 使用，不复用。

```text
120 Notification
121 NotificationInbox
122 NotificationState
```

表号已经同时登记到 `model/types.go`、`docs/database_design.md` 和 `AGENTS.md`。

### Notification

```text
key   = T120 | recipient UUID(16) | notification UUID(16)
value = versioned Notification record
```

当前 record 字段：

```text
version              uint32, 当前固定 1
id                   notification UUID
kind                 NotificationKind
recipient_uuid       user UUID
actor_uuid           user UUID，可空（系统事件）
target_uuid          feed/group UUID，可空
entry_uuid           Entry UUID，可空
comment_uuid         Comment UUID，可空
requested_at         FollowRequest occurrence timestamp，可空
activity_at_ms       Unix milliseconds
created_at_ns        Unix nanoseconds
actor_name_snapshot  actor 删除后 graceful fallback
target_name_snapshot target 删除后 graceful fallback
```

实现使用 versioned JSON value；key/value encoding 是持久化契约。未来若迁移 protobuf，必须
作为显式离线 schema migration，不能在同一表号下静默混写两种 value 编码。

Notification canonical key recipient-first，便于 account deletion 和 retention 对单个 recipient
做有界/前缀清理；不需要全表按 value 找 recipient。

### NotificationInbox

```text
key   = T121 | recipient UUID(16) | ^UnixMillis(activity_at)(8) | notification UUID(16)
value = empty
```

与当前 `TimelineIndex` / `EntryIndex` 一致：8-byte big-endian Unix milliseconds 逐位取反，
前向扫描得到 newest -> oldest；notification UUID 只负责同毫秒稳定唯一排序，不代表真实先后。

`activity_at` 必须使用领域事件时间，而不是 worker 执行时间。例如 Group follow request 18:00
发生、admin fanout worker 18:03 执行，Inbox 仍按 18:00 排序。Service failure 同理：fanout worker
稍后执行时，`activity_at` 仍使用本次 `ServiceState.DeadAtMs`。

### NotificationState

```text
key   = T122 | recipient UUID(16)
value = versioned state record
```

当前 state：

```text
version          uint32, 当前固定 1
last_read_at_ns  int64
unread_count     uint32
total_count      uint32
```

`unread_count` 和 `total_count` 是可校验/可修复状态，用于 sidebar badge 和 retention trigger；
Notification/Inbox 才是通知事实。audit 可根据 canonical rows 重建 State。

## 时间与未读语义

排序与未读使用不同时间：

```text
activity_at_ms
  领域事件何时发生
  用于 Inbox 排序

created_at_ns
  这条 Notification 何时真正写入 recipient Inbox
  用于 unread
```

判定：

```text
notification.created_at_ns > state.last_read_at_ns
    => unread
```

打开 `/notifications` 时，HTTP handler 在成功取得当前页面后调用 `MarkNotificationsRead`：

```text
state.last_read_at_ns = server now UnixNano
state.unread_count = 0
```

Mark-all-read 是 O(1)，不更新 500 条 Notification。

当前实现接受 `MarkNotificationsRead` 与一条刚提交 Notification 恰好竞争的极小窗口；Pebble
`ApplyBatch` 已串行 mutation，实际结果由 commit 顺序决定。当前项目不为该极端窗口引入
sequence/generation/CAS。若未来需要严格“页面快照之后到达的通知绝不能被 mark read”，再升级
State 协议，不能提前为当前规模增加复杂度。

`last_read_at_ns` 使用 Unix nanoseconds，不能直接复用 Inbox 的 millisecond cursor。

## Notification ID 与幂等

notification UUID 必须 deterministic，并包含 recipient：

```text
UUIDv5(namespaceURL,
       "notification:" + kind + ":" + occurrence_identity + ":" + recipient_uuid)
```

同一 notification 重试时：

- canonical row 已存在 -> no-op；
- Inbox row 不重复；
- State counter 不重复增加。

各 kind occurrence identity：

```text
FOLLOW_REQUEST_RECEIVED
  target UUID + requester UUID + requested_at

FOLLOW_REQUEST_APPROVED
  target UUID + requester UUID + requested_at

FOLLOW_REQUEST_REJECTED
  target UUID + requester UUID + requested_at

ENTRY_COMMENTED
  comment UUID

ENTRY_LIKED
  entry UUID + actor UUID

GROUP_ADMIN_ADDED
  group UUID + target UUID + transition token

GROUP_ADMIN_REMOVED
  group UUID + target UUID + transition token

GROUP_MEMBER_REMOVED
  group UUID + target UUID + transition token

FEED_SERVICE_FAILED
  canonical service UUID + target Feed UUID + ServiceState.DeadAtMs
```

Group role/member mutation 在当前模型中一次真实状态 transition 只允许发生一次；transition token
使用 mutation 的 `activity_at`（UnixNano）以允许未来同一用户被降级后再次提升时产生新的通知。

Service failure 直接复用已有 `DeadAtMs` 作为故障周期 token，不增加新的 ServiceState/schema 字段：

- 同一个 dead 周期的 Task retry、进程重启和重复 binding 会得到相同 notification ID；
- successful Refresh / fetch 会按既有规则将 `DeadAtMs` 清零并恢复 `active`；
- 后续再次进入 `dead` 会得到新的 `DeadAtMs`，因此是新的 occurrence 和新的通知。

Like 特意采用：

```text
entry UUID + actor UUID
```

因此 `like -> unlike -> like` 最终只保留一条历史 Like notification，避免 toggle spam。

## State transition，而不是 API call

通知必须由真实领域状态变化产生，不能按 RPC/HTTP 被调用次数产生。

```text
Like missing -> present
  emit ENTRY_LIKED

Like present -> present
  no notification

not admin -> admin
  emit GROUP_ADMIN_ADDED

admin -> admin
  no notification

member -> removed
  emit GROUP_MEMBER_REMOVED

not member -> remove request
  no notification

Service active/degraded -> degraded
  no notification

Service active/degraded -> dead
  emit FEED_SERVICE_FAILED fanout task

Service dead + retry of same occurrence
  no duplicate notification
```

如果现有 model stage 函数无法判断 changed/no-op，新增兼容 helper 或在同一个 `ApplyBatch`
serialization boundary 中先读 authoritative state；不要为了通知改变旧导出 API 的语义。

## Recipient 解析

### Entry Comment / Like

`ENTRY_COMMENTED` 和 `ENTRY_LIKED` recipient **只**从 `Entry.ProfileUuid` 解析，不从
`Entry.FeedUuid` 解析：

```text
resolve Entry.ProfileUuid
  profile exists
  && !Deleted
  && Type == "user"
  && recipient != actor
      => notify
  else
      => skip
```

因此：

```text
用户 Alice 发帖到 Group G
ProfileUuid = Alice
FeedUuid    = G
=> notify Alice
```

而 FeedService self-post：

```text
ProfileUuid = G
FeedUuid    = G
=> G 不是 user，skip
```

comment 只通知 Entry author，不通知楼内其他 commenter。

### Follow request

private user feed：recipient 是 target feed 本人，但必须验证 target Profile 是未删除 user。

private Group：recipient 是 fanout 时可管理该 Group 的当前 GroupAdmin user 集合。Group
本身不能成为 recipient；super 不因为全局权限自动收到所有 Group request 通知。

### Group admin/member mutation

recipient 是 target member user；actor==target 时不产生“你把自己提升/移除”式通知。

### FeedService failure

`FEED_SERVICE_FAILED` 从 canonical `Service` 的当前 `ServiceFeedIndex` binding 解析受影响 target：

```text
binding exists
&& binding.Enabled
&& binding.ServiceUuid == canonical service UUID
&& binding.Created <= dead occurrence
```

然后按 target 类型解析 recipient：

```text
Profile(Type=user)
  => target user 本人

Profile(Type=group)
  => fanout 执行时的当前 GroupAdmin users

其他 / deleted / missing
  => skip
```

Group 自身绝不能成为 Notification recipient。多个 binding 指向同一 target 时按 target 去重；
notification ID 仍包含 target 和 recipient，因此整个 task 重试时不会产生重复通知。

## 单 recipient 原子提交

只影响一个 recipient 的通知必须和领域 mutation 同 batch：

```text
Comment mutation
├── Comment row / interaction indexes
└── ENTRY_COMMENTED Notification + Inbox + State

Like false -> true
├── Like row
└── ENTRY_LIKED Notification + Inbox + State

Approve FollowRequest
├── delete FollowRequest
├── Follow/Follower
├── home.rebuild Task
└── FOLLOW_REQUEST_APPROVED Notification + Inbox + State

Reject FollowRequest
├── delete FollowRequest
└── FOLLOW_REQUEST_REJECTED Notification + Inbox + State

Add/Remove GroupAdmin / RemoveGroupMember
├── authoritative GroupAdmin/Follow state
└── corresponding Notification + Inbox + State
```

这样 mutation 成功意味着必须一致的通知也已经持久化；若 Notification stage 失败，领域 mutation
一起回滚。Notification trim 属于历史维护，不进入这个 batch。

Service failure 仍使用 durable Task，而不是在抓取失败处理路径同步生成通知：这样 dead ServiceState 与
“存在待处理通知工作”可以保持同一个原子 durability boundary。按当前项目规模，task 执行时一次读取
全部 Service bindings（理论上 <10）；Group target 一次读取全部 admins（通常 3–5）。

## Multi-recipient fanout

真正可能无界的 recipient 集合使用 bounded Task。目前 private Group `FOLLOW_REQUEST_RECEIVED` 继续按
既有 pagination/continuation 设计 fanout 当前 Group admins。

`FEED_SERVICE_FAILED` 也使用 durable Task，但原因是需要把 dead transition 与通知工作原子绑定，而不是
因为当前 recipient 集合很大。一个 root task 一次完成所有受影响 bindings 和 Group admins，不创建
binding continuation 或 Group 二级 task。

### Group follow request

Request 创建和 fanout task 必须同 batch：

```text
RequestFollow(private Group)
├── FollowRequest
└── notification.follow_request_group task
```

Task payload 只保存稳定 ID/时间，不保存 secret 或正文：

```text
version
feed_uuid
requester_uuid
requested_at
activity_at_ms
cursor_admin_uuid (continuation 时使用)
```

worker 每次最多处理固定数量（当前 100）GroupAdmin edges；每个 recipient 使用 deterministic ID，
retry 安全。需要 continuation 时入队下一页 task。不得把全部 admins 装入 slice/map。

如果 request 在 worker 执行前已经被 cancel/reject/approve，允许仍展示“requested to join”这条
历史通知；Notification 表示事件曾发生，不代表 request 当前仍可操作。点击后 authoritative Requests
页面会展示当前真实状态。

### FeedService failure

只有真正进入长期 `dead` 时创建 task。`degraded`、短暂网络失败或仍在永久失败观察窗口中的来源
不创建 notification task。

`ServiceState` 的 dead transition 与 root task 是同一个 Pebble/Task atomic boundary：

```text
Service active/degraded -> dead
├── ServiceState(Status=dead, DeadAtMs=occurrence)
└── notification.feed_service_failed task
```

通过现有 `Task.EnqueueWith(..., business batch)` 提交。若 task queue 已停止接受新任务，dead state 也不能
单独提交，避免出现“来源已经永久 dead，但通知工作永久丢失”的半状态。

root task 直接读取当前完整 `ServiceFeedIndex(canonical service UUID)`。按当前项目实际规模，一个 Service
理论上不超过约 10 个 bindings；Group 通常只有 3–5 个 admins，因此不引入 cursor、page 或 continuation
状态。对 user target 直接写本人通知；对 Group target 直接读取 `ListGroupAdmins` 并写当前 admins。

不同 target 分别使用小 Pebble batch 提交通知。这样同一用户如果同时管理多个受影响 Group，每个 batch
都能读取前一个 target 已提交的 NotificationState，避免同一大 batch 内 counter update 相互覆盖。若 task
处理中途失败，已完成 target 保留；整 task 重试时 deterministic notification ID 会让已完成 target no-op，
再继续补齐未完成 target。

Service failure task payload 只保存：

```text
service_uuid
dead_at_ms
```

禁止保存 canonical/fetch URL、URL query、响应正文、HTTP error detail、OAuth/token/cookie 或其他凭据。

## Follow request occurrence

`FollowRequest` value 中已有 `requested_at`，它是 request occurrence identity 的组成部分。

- 重复提交同一 pending request 保留原 timestamp -> 不重复通知；
- reject 后重新 request 会得到新 timestamp -> 新 notification；
- cancel 后重新 request 同理；
- approve/reject 通知必须在删除 request 前读取同一个 `requested_at`，并在同 batch 铸造结果通知。

当前实现不改变 `StageDeleteFollowRequestsByTarget` 的接口。

### private -> public

`FOLLOW_REQUEST_CLEARED_BY_PUBLIC` 当前不实现。

未来实现时必须满足：

- 只挂在真实 `wasPrivate && !newPrivate` transition；
- 不能挂在“每次 public profile save 都执行”的 self-heal cleanup；
- 必须有 bounded workflow 获取 `(requester, requested_at)`，不能一次把所有 request 收进内存；
- silent self-heal cleanup 与 transition notification 是两个不同概念。

## Retention

读有界不等于存有界。Notification/Inbox 必须 per-recipient bounded。

当前常量：

```text
NotificationMaxEntries  = 500
NotificationTrimTrigger = 550
NotificationTrimBatch   = 100
```

正常写 notification 时只更新 State counter；当 commit 后发现：

```text
total_count > NotificationTrimTrigger
```

只 signal/schedule 后台 trim，不在请求路径同步扫描和删除历史。

trim：

```text
scan NotificationInbox(recipient) newest -> oldest
keep first 500
for older rows, bounded batch:
    delete Inbox row
    delete canonical Notification row
    state.total_count--
    if deleted row.created_at_ns > last_read_at_ns:
        state.unread_count--
```

一次 trim 最多删除 `NotificationTrimBatch`；仍超过 500 时继续后台 iteration。

同一 recipient 的 trim 应 singleflight/coalesce，避免 550->551->552 连续写入启动多个维护 goroutine。

进程启动后需要流式 recovery sweep：遍历 NotificationState，发现 `total_count > 500` 的 recipient
只 schedule trim，不把全部 recipient 收进 map。这样 crash 发生在 notification commit 与 trim 之间
也不会永久超限。

trim 失败不得回滚已经成功的领域 mutation；错误记录并由后续 trigger/startup recovery 重试。

Like/Comment 的通知在 model 层与互动行同批写入，commit 后由 server 的 public-timeline bump
回调检查 State 并调度 trim；该回调即使目标 feed 为 private 也会先执行 retention 检查。Fanout task
成功 stage notification 后同样调用统一的 post-commit trim 调度。任何未来绕过 server RPC、直接调用
model 互动 helper 的写入入口，都必须在成功提交后执行同等的 trim 调度，不能只依赖进程启动 recovery。

## 删除与 orphan

### recipient account delete

账号 soft delete 的有界核心 mutation 不得同步扫描最多 500 条通知之外的全库。由于 recipient-owned
key 前缀，账号清理任务可以范围/分批删除：

```text
Notification(recipient prefix)
NotificationInbox(recipient prefix)
NotificationState(recipient)
```

删除通知不影响业务状态。

### actor delete

不 fanout 删除所有历史通知。render 时 actor 不存在/Deleted：

```text
actor_name_snapshot -> "Deleted user" fallback
```

### source Entry/Comment/Feed delete

Notification 不保存正文或 Service source details。读取时若 source object 已删除或 viewer 当前无权读取：

- 不泄漏正文、URL query 或 error detail；
- 可显示通用不可用文案，或 lazy skip；
- 可顺手清理 orphan Notification/Inbox，但清理失败不影响页面。

## Privacy

Notification metadata 只能包含呈现通知所需最小信息：kind、actor/target snapshot、稳定对象 ID、时间。
不得复制 Entry body、Comment body、OAuth、FeedService credential、FeedService URL/query、抓取响应正文或
错误详情等内容。

点击/渲染 source content 时必须重新执行当前权限：private Group/member 状态变化后，旧通知不能成为
绕过 `enforcePrivateFeedRead` 的旁路。Service failure 通知只导航到现有 Import Services 管理页面，具体
Service 状态和可执行操作继续由该页面的现有授权逻辑决定。

## 读取 API / transport

Notification domain 对 server 暴露 typed Go helpers：

```text
ListNotifications(recipient, limit, cursor)
NotificationSummary(recipient)
MarkNotificationsRead(recipient, now)
```

cursor 编码完整 Inbox position：

```text
^UnixMillis(activity_at)(8) + notification UUID(16)
```

limit 默认 30，最大 100。读取 canonical row 缺失时 lazy 删除 orphan Inbox row 并继续；不得因为一个
坏 row 让整页不可用。

当前 `pb/api.pb.go` 是 legacy 单体 generated 文件。为避免 Notification 功能同时引入 protobuf
生成链迁移，HTTP 层暂通过现有 loopback-only `Command` RPC 使用一个窄 adapter：

```text
NotificationList
NotificationSummary
NotificationMarkRead
```

`CommandResponse.Result` 只承载 versioned JSON DTO；httpd 不自行读 Pebble。该 adapter 不是领域 API，
不得在新业务代码里扩散。未来整理 protobuf 生成链后可增加 dedicated RPC 并删除 adapter，存储和
Notification domain 契约不变。

所有 adapter 的 actor/recipient UUID 仍由当前 loopback HTTP server 从 session 注入；若 gRPC 将来对外
监听，必须先引入可信 principal，不能继续信任自报 UUID。

## HTTP / UI

```text
GET /notifications
```

使用 SSR 页面，不要求 React。

sidebar 主导航：

```text
Home
My feed
Notifications [bell-ring when unread]
Groups
Likes
Comments
Public
```

Notifications 属于主产品导航，不放在 Account 设置区。

badge：

```text
0  -> Notifications
>0 -> Notifications + bell-ring icon
```

`Server.HTML` 已经为登录用户注入 sidebar context；在该路径请求 `NotificationSummary`，失败时只省略
badge，不能让所有页面因通知摘要失败而 500。

通知事务提交后，ffdb 只发送 `NOTIFICATIONS_DIRTY` hint；ffweb 转成 `notifications-dirty`。
已加载 Feed React 的页面收到 hint 后只显示新通知图标，不额外请求 summary，也不猜测未读数。
hint 不携带通知正文或计数；下次 SSR 页面加载时只根据 `NotificationSummary` 收敛图标的有/无状态。

`/notifications` 每行按 kind render user-facing 文案和 source link：

```text
Alice requested to follow you                -> /account/requests
Alice requested to join Go Developers        -> Group members/request UI
Bob commented on your post                   -> /e/<entry>
Carol liked your post                        -> /e/<entry>
David promoted you to admin of Go Developers -> /feed/<group>
An imported service for Book Club needs attention
                                             -> /feed/<target ID>/import
```

Service failure 文案只包含安全摘要和 target 名称，不显示 source URL、query、响应正文或内部错误。

通知页不直接复制 Approve/Reject/Refresh 表单；workflow action 继续在 `/account/requests`、Group 管理页
或 Import Services 页面执行。

成功 render 当前页后 mark all read；mark-read 失败不阻止页面展示，只保留 badge 到下次成功。

## 文案

Notification record 不存最终英文句子，render 根据 `kind + snapshots + current profile` 生成文案。
这样 profile rename、未来国际化和 wording 调整不需要重写历史 record。

snapshot 只作为对象删除后的 fallback；对象存在时优先使用当前 Profile name。

## 后台维护与并发

- Notification direct stage 运行在调用方现有 `ApplyBatch` serialization boundary；
- trim 不持 profile/follow 全局 mutation lock；
- fanout worker 使用 Task 的 lease/idempotency 机制，不新建第二套队列；
- 真正可能无界的 fanout/trim 扫描必须关闭 iterator 并保持 bounded；
- Service failure 的 bindings/admins 按当前实际规模一次性读取，不引入 cursor/continuation；
- background error 记录 `slog`，不得 `Fatal`；
- payload/log 不记录正文、URL query、Cookie、token、OAuth secret 或抓取响应详情。

## GROUP_DELETED 后续契约

`GROUP_DELETED` 当前不实现，但未来加入前必须实现 durable barrier。

当前 Group soft delete 故意保留 Follow/Follower edges；这些 edges 在 group-deleted notification fanout
完成前是 recipient snapshot 的权威来源。DeleteGroup 必须同 batch：

```text
mark Group deleted
set group-delete-notify:<group> = pending
enqueue bounded GROUP_DELETED fanout task
```

worker 分页扫描 Follower，fanout 完成后才清 pending marker。任何 edge cleanup/ops command 必须：

```text
pending marker exists => skip that Group
```

不能依赖“通常 notification task 会比 maintenance 先跑”的时间顺序。

## Audit

`audit_store` 检查：

- Notification key recipient/id 与 value 一致；
- recipient 是未删除 user（删除账号的残留可以报 orphan）；
- 每个 Inbox row 指向存在且同 recipient 的 Notification；
- canonical Notification 对应 Inbox row 存在；
- Inbox timestamp/UUID 与 Notification activity/id 一致；
- State total_count 与 canonical count 一致；
- State unread_count 与 `created_at_ns > last_read_at_ns` 的实际数量一致；
- per-recipient row count > 550 报 maintenance lag，>500 可由 repair/trim 收敛。

repair 必须有界执行，不能全量加载 notification 表。

当前 audit 已实现上述检查并输出 canonical、Inbox、State、orphan、counter drift 与 retention lag
计数；它只做流式扫描和点查，不自动修改数据。State counter 漂移由后续有界 repair 工具修复，超限记录
由现有 trim/recovery 收敛。

## 测试要求

Model：

- key encoding/parse；
- same-millisecond UUID tiebreak；
- deterministic id；
- duplicate stage 不重复 counter；
- unread/mark-read；
- retention trim 到 500；
- orphan Inbox lazy cleanup；
- staged ServiceState 写入保持现有编码。

Domain：

- Comment/Like 只通知 user Entry author；Group self-post skip；self interaction skip；
- repeated Like / repeated pending request 不重复；
- reject + re-request 是两个 occurrence；
- approve/reject relationship/request/notification 原子；
- Group admin/member 仅真实 transition 通知；
- private Group request fanout task retry 幂等且 bounded；
- Service 只在长期 `dead` transition 创建通知工作，`degraded` 不通知；
- personal Feed 每个 dead occurrence 一条；
- Group fanout 当前 admins，普通 member 和 Group profile 不收；
- 多个 binding 指向同一 target 去重；同一 admin 管理多个受影响 target 时 State counter 正确；
- Task retry、同一 `DeadAtMs` 不重复；
- dead ServiceState 与 root task 原子提交；
- successful Refresh 恢复 active 后，再次 dead 形成新的 occurrence。

HTTP：

- sidebar badge 仅区分无通知/有通知图标；
- Notification page render 和 link；
- Notification summary failure 不拖垮普通页面；
- notification 页面 render 后 mark read；
- source object 不可见时不泄漏正文；
- `FEED_SERVICE_FAILED` 只显示安全摘要并链接对应 `/feed/<target ID>/import`。

完整验证沿用 `AGENTS.md`：

```text
go build ./...
go vet ./...
go test ./...

pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build
```

需要时补 E2E：private request -> recipient notification -> approve -> requester notification；以及
Service dead -> notification -> Import Services -> successful Refresh。

## 实施顺序

基础 Notification 已按小提交完成；后续领域扩展同样保持可回退：

1. 定义领域触发与 occurrence identity；
2. 复用现有 Notification record/schema，不为单一新 kind 改持久化契约；
3. 按实际 recipient 规模选择最简单的 fanout；只有真正无界时才引入分页/continuation；
4. 领域终态与 root durable task 建立原子边界；
5. SSR 只渲染安全摘要并导航到既有 authoritative workflow；
6. 补 domain/idempotency/privacy/recovery tests。

每一步都必须保持旧业务状态 source-of-truth 不变；不能为了通知让 FollowRequest、Follow/Follower、
GroupAdmin、interaction 或 ServiceState 变成派生数据。
