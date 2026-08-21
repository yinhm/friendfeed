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
GroupAdmin、Like、Comment 等领域表仍是事实来源；通知被删除、trim、过期或读取失败都不能改变
领域状态。

本文定义 V1 的持久化、事件、接收者、未读、retention、fanout、隐私和 HTTP/UI 契约。

## 核心不变量

必须始终满足：

1. Notification 是 recipient-owned durable data，不是 TimelineIndex 的别名；
2. recipient 必须是存在、未删除的 `Profile(Type=user)`；Group、Feed 或其他对象不能成为 recipient；
3. 单 recipient 通知与产生它的领域 mutation 在同一个 Pebble batch 提交；
4. 多 recipient fanout 才进入 durable Task，worker 必须 bounded、幂等、可恢复；
5. Inbox 排序统一使用 `^UnixMillis(activity_at) + notification UUID`，不引入 reverse Flake；
6. `activity_at` 是领域事件实际发生时间；`created_at_ns` 是通知真正写入 Inbox 的时间；
7. unread 使用 `created_at_ns > last_read_at_ns` 的时间水位，不使用 Flake sequence；
8. 每个 recipient 最终只保留最新 500 条通知；trim 在后台执行，不进入用户 mutation 的历史清理路径；
9. Notification 不保存 Entry/Comment 完整正文，读取时仍按当前对象状态和权限 fail closed；
10. deterministic notification ID 保证 mutation/task retry 不产生重复通知；
11. self action 不通知自己，例如自己给自己的 Entry 点赞/评论；
12. workflow 页面仍是 authoritative action UI，Notification 只链接过去，不复制 approve/reject 权限逻辑。

## V1 范围

V1 支持以下八类事件：

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

V1 明确不做：

- comment thread subscribers；
- 通知同一楼层的其他 commenter；
- mention / reply-to（当前领域模型没有该事实）；
- email、Web Push、mobile push；
- Like 聚合（`Alice and 3 others liked...`）；
- 单条 mark-read/unread；
- notification preferences；
- `FOLLOW_REQUEST_CLEARED_BY_PUBLIC`；
- `GROUP_DELETED`；
- `FEED_SERVICE_FAILED`；
- 普通 public follow 的 `NEW_FOLLOWER`。

后四类可在后续版本加入；它们不得反向污染 V1 的 key/schema 契约。

## 数据模型

新表从 120 开始；119 保留为空，不复用。

```text
120 Notification
121 NotificationInbox
122 NotificationState
```

表号上线时必须同时登记到 `model/types.go`、`docs/database_design.md` 和 `AGENTS.md`。

### Notification

```text
key   = T120 | recipient UUID(16) | notification UUID(16)
value = versioned Notification record
```

V1 record 字段：

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

V1 实现使用 versioned JSON value；key/value encoding 是持久化契约。未来若迁移 protobuf，必须
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
发生、admin fanout worker 18:03 执行，Inbox 仍按 18:00 排序。

### NotificationState

```text
key   = T122 | recipient UUID(16)
value = versioned state record
```

V1 state：

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

V1 接受 `MarkNotificationsRead` 与一条刚提交 Notification 恰好竞争的极小窗口；Pebble
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
```

Group role/member mutation 在当前模型中一次真实状态 transition 只允许发生一次；transition token
使用 mutation 的 `activity_at`（UnixNano）以允许未来同一用户被降级后再次提升时产生新的通知。

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

V1 comment 只通知 Entry author，不通知楼内其他 commenter。

### Follow request

private user feed：recipient 是 target feed 本人，但必须验证 target Profile 是未删除 user。

private Group：recipient 是 request 创建时可管理该 Group 的当前 GroupAdmin user 集合。Group
本身不能成为 recipient；super 不因为全局权限自动收到所有 Group request 通知。

### Group admin/member mutation

recipient 是 target member user；actor==target 时不产生“你把自己提升/移除”式通知。

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

## Multi-recipient fanout

只有 recipient 数量不天然有界时使用 Task。V1 只有 private Group 的
`FOLLOW_REQUEST_RECEIVED` 需要 fanout 当前 Group admins。

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

worker 每次最多处理固定数量（建议 100）GroupAdmin edges；每个 recipient 使用 deterministic ID，
retry 安全。需要 continuation 时入队下一页 task。不得把全部 admins 装入 slice/map。

如果 request 在 worker 执行前已经被 cancel/reject/approve，V1 允许仍展示“requested to join”这条
历史通知；Notification 表示事件曾发生，不代表 request 当前仍可操作。点击后 authoritative Requests
页面会展示当前真实状态。

## Follow request occurrence

`FollowRequest` value 中已有 `requested_at`，它是 request occurrence identity 的组成部分。

- 重复提交同一 pending request 保留原 timestamp -> 不重复通知；
- reject 后重新 request 会得到新 timestamp -> 新 notification；
- cancel 后重新 request 同理；
- approve/reject 通知必须在删除 request 前读取同一个 `requested_at`，并在同 batch 铸造结果通知。

V1 不改变 `StageDeleteFollowRequestsByTarget` 的接口。

### private -> public

`FOLLOW_REQUEST_CLEARED_BY_PUBLIC` 不在 V1。

未来实现时必须满足：

- 只挂在真实 `wasPrivate && !newPrivate` transition；
- 不能挂在“每次 public profile save 都执行”的 self-heal cleanup；
- 必须有 bounded workflow 获取 `(requester, requested_at)`，不能一次把所有 request 收进内存；
- silent self-heal cleanup 与 transition notification 是两个不同概念。

## Retention

读有界不等于存有界。Notification/Inbox 必须 per-recipient bounded。

V1 常量：

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
回调检查 State 并调度 trim；该回调即使目标 feed 为 private 也会先执行 retention 检查。任何未来绕过
server RPC、直接调用 model 互动 helper 的写入入口，都必须在成功提交后执行同等的 trim 调度，不能只
依赖进程启动 recovery。

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

### source Entry/Comment delete

Notification 不保存正文。读取时若 source object 已删除或 viewer 当前无权读取：

- 不泄漏正文；
- 可显示通用不可用文案，或 lazy skip；
- 可顺手清理 orphan Notification/Inbox，但清理失败不影响页面。

## Privacy

Notification metadata 只能包含呈现通知所需最小信息：kind、actor/target snapshot、稳定对象 ID、时间。
不得复制 Entry body、Comment body、OAuth、FeedService credential 等内容。

点击/渲染 source content 时必须重新执行当前权限：private Group/member 状态变化后，旧通知不能成为
绕过 `enforcePrivateFeedRead` 的旁路。

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

当前 `pb/api.pb.go` 是 legacy 单体 generated 文件。V1 为避免一次 Notification 功能同时引入 protobuf
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

新增：

```text
GET /notifications
```

V1 使用 SSR 页面，不要求 React。

sidebar 主导航：

```text
Home
My feed
Notifications (N)
Groups
Likes
Comments
Public
```

Notifications 属于主产品导航，不放在 Account 设置区。

badge：

```text
0       -> Notifications
1..99   -> Notifications (N)
>=100   -> Notifications (99+)
```

`Server.HTML` 已经为登录用户注入 sidebar context；V1 在该路径请求 `NotificationSummary`，失败时只省略
badge，不能让所有页面因通知摘要失败而 500。

`/notifications` 每行按 kind render user-facing 文案和 source link：

```text
Alice requested to follow you               -> /account/requests
Alice requested to join Go Developers       -> Group members/request UI
Bob commented on your post                  -> /e/<entry>
Carol liked your post                       -> /e/<entry>
David promoted you to admin of Go Developers -> /feed/<group>
...
```

通知页不直接复制 Approve/Reject 表单；workflow action 继续在 `/account/requests` 或 Group 管理页执行。

成功 render 当前页后 mark all read；mark-read 失败不阻止页面展示，只保留 badge 到下次成功。

## 文案

V1 Notification record 不存最终英文句子，render 根据 `kind + snapshots + current profile` 生成文案。
这样 profile rename、未来国际化和 wording 调整不需要重写历史 record。

snapshot 只作为对象删除后的 fallback；对象存在时优先使用当前 Profile name。

## 后台维护与并发

- Notification direct stage 运行在调用方现有 `ApplyBatch` serialization boundary；
- trim 不持 profile/follow 全局 mutation lock；
- fanout worker 使用 Task 的 lease/idempotency 机制，不新建第二套队列；
- fanout/trim 所有扫描必须关闭 iterator；
- 不允许在 request mutation 内同步枚举大型 Group members/admins 或历史 notifications；
- background error 记录 `slog`，不得 `Fatal`；
- payload/log 不记录正文、Cookie、token、OAuth secret。

## GROUP_DELETED 后续契约

`GROUP_DELETED` 不在 V1，但未来加入前必须实现 durable barrier。

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
- orphan Inbox lazy cleanup。

Domain：

- Comment/Like 只通知 user Entry author；Group self-post skip；self interaction skip；
- repeated Like / repeated pending request 不重复；
- reject + re-request 是两个 occurrence；
- approve/reject relationship/request/notification 原子；
- Group admin/member 仅真实 transition 通知；
- private Group request fanout task retry 幂等且 bounded。

HTTP：

- sidebar badge 0/1/99/99+；
- Notification page render 和 link；
- Notification summary failure 不拖垮普通页面；
- notification 页面 render 后 mark read；
- source object 不可见时不泄漏正文。

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

需要时补 E2E：private request -> recipient notification -> approve -> requester notification。

## 实施顺序

建议保持可回退的小提交：

1. `docs: define notification subsystem contract`
2. `feat: add notification storage model`
3. `feat: expose notification read adapter and SSR inbox`
4. `feat: emit follow-request notifications`
5. `feat: emit social interaction notifications`
6. `feat: emit group role notifications`
7. `feat: add bounded notification fanout and retention`
8. `test: cover notification end-to-end flows`
9. `docs: register notification storage invariants`

每一步都必须保持旧业务状态 source-of-truth 不变；不能为了通知让 FollowRequest、Follow/Follower、
GroupAdmin 或 interaction 行变成派生数据。
