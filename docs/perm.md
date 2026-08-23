# 可见性与权限收口

本文定义 Feed、Entry permalink、Home、Public、Search 与 Interaction Feed 的统一读取权限。
目标是在 ffdb 内形成单一语义，使同一 Entry 无论从哪个入口读取都得到一致结果。本文只沿用
当前 loopback gRPC 可信边界；`viewer_uuid` 仍是 ffweb 传入的过渡身份，不把它描述成可抵御
恶意本机调用方的认证 principal。

## 已修复的历史缺口

下表记录统一 resolver 落地前的缺口，防止后续重构重新引入同类旁路。当前实现已经由
`server/visibility.go` 统一这些判断。

| 入口 | 当前行为 | 缺口 |
|---|---|---|
| Feed legacy/cursor | 校验正在访问的 Feed 是否 private | 不复核列表中每条 Entry 的实际 target |
| Entry permalink | 传递 `ViewerUuid` 并检查 private target | 独立实现；target 缺失、deleted 或非法时 fail-open |
| Home | 重新校验 stale private 行的 Follow 边 | target 损坏时 fail-open；未校验 `ViewerUuid == ProfileUuid` |
| Public | target 缺失、deleted 或 private 时 fail-closed | 与其他入口语义不同 |
| Search | private hit 按 viewer 过滤 | 无法解析或缺失 target 时可能继续展示 |
| Likes/Comments | Web 与 RPC 双层 owner-only | 不校验其中 Entry 的当前可见性 |
| Like/Comment mutation | 校验 actor Profile 与互动所有权 | 不校验 actor 当前是否可读 Entry target |
| Group metadata | 对匿名公开 | 有意保留，作为 follow request 入口 |
| Group members | private Group 仅 follower/member/super 可读 | 当前符合目标语义 |

### 作者 Feed 泄露 private target

`model.PutEntry` 同时写 author 与 target 的 direct EntryIndex：

```text
EntryIndex(author, entry)
EntryIndex(target feed, entry)
```

用户向 private Group 投稿后，同一 Entry 同时存在于 Group Feed 和作者 Profile Feed。当前读取只
检查“正在访问的 Profile Feed”是否 private：`/feed/private-group` 会拒绝 outsider，但公开的
`/feed/author` 可能返回实际 `FeedUuid` 指向 private Group 的同一 Entry。这是本项最高优先级的
读取旁路。

### Home 身份未在 ffdb 收口

Home 使用 `FeedRequest.ProfileUuid` 选择该用户的 TimelineIndex。正常 ffweb 同时把 session UUID
写入 `ViewerUuid`，但 ffdb 未验证两者相同。loopback gRPC 调用方可指定其他用户的
`ProfileUuid` 读取其物化 Home。Home 必须是 owner-only：有效且存在的
`ViewerUuid == ProfileUuid`。

### 损坏数据被当作公开

当前 `privateFeedEntryVisible` 默认 `visible = true`，只有成功读取到 private Profile 时才收紧；
`entryVisibilityTarget` 又把空、非法和无法解析统一成“没有限制”。因此 target missing、deleted、
非法 UUID 或真实存储错误可能被解释为可见。权限判断必须区分业务拒绝、永久无效数据和存储
故障，不能静默 fail-open。

## 统一目标语义

Entry 的 target 唯一按以下规则解析：

```text
target = FeedUuid（非空时）
       | ProfileUuid（FeedUuid 为空时）
```

target Profile 必须存在、未删除且 UUID 合法。metadata 是否公开与内容读取是两个不同问题；
private Feed/Profile/Group 的名称、头像、简介仍可公开用于发现和 follow request，但其中 Entry
必须遵守本节权限。该边界已经在当前实现中统一：ffdb `GetGroup` 对匿名 viewer 返回 private
Group metadata，httpd 也据此渲染 follow-request 入口；本项保持此行为，不再把 metadata 当作
private 内容，也不重新收紧为 `PermissionDenied`。

| target | owner | follower/member | super | outsider | anonymous | deleted viewer |
|---|---:|---:|---:|---:|---:|---:|
| Public user Feed | 允许 | 允许 | 允许 | 允许 | 允许 | 拒绝 |
| Public Group | 允许 | 允许 | 允许 | 允许 | 允许 | 拒绝 |
| Private user Feed | 允许 | 允许 | 允许 | 拒绝 | 拒绝 | 拒绝 |
| Private Group | admin/member 允许 | 允许 | 允许 | 拒绝 | 拒绝 | 拒绝 |
| Missing/deleted/非法 target | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 拒绝 |

Group member 与普通 Feed subscriber 都由既有 Follow/Follower 边表达。非空 viewer 还必须解析为
存在且未删除的 canonical user Profile；不能让 deleted viewer 依靠遗留 Follow 边继续访问。
这一检查有意应用于带 viewer 的 public target 读取，而不只应用于 private 分支：deleted session
不再被视为有效登录身份，也避免同一 viewer 在 public/private target 间出现两种身份语义。resolver
每个请求只解析一次 viewer，因此不会按 Entry 重复读取 Profile。匿名请求仍不读取 viewer Profile。

错误边界统一为：

- private 权限不足：`PermissionDenied`；
- 直接请求的 Entry、Feed 或 target missing/deleted：`NotFound`；
- 非空但非法的 viewer UUID：`InvalidArgument`；
- Pebble 等真实读取故障：`Internal`，不得伪装为公开、不可见或不存在；
- 聚合列表中的 denied/missing/deleted Entry：跳过；真实存储故障中止请求。

## 最小架构

不引入通用 policy engine，也不新增表、迁移或 RPC。ffdb 内增加请求级 resolver：

```go
type entryVisibilityResolver struct {
    viewer  viewerIdentity
    targets map[uuid.UUID]targetVisibility
}

func (r *entryVisibilityResolver) CanReadEntry(entry *pb.Entry) (bool, error)
```

resolver 在一次请求中只解析一次 viewer，并缓存 target Profile、private 状态与 Follow 决策。
decision 与 gRPC 状态码转换分离：列表路径需要跳过 denied 行，permalink 则需要把同一 decision
转成明确的 `PermissionDenied` 或 `NotFound`。不得用一个 `bool` 同时表达不可见、目标损坏和
存储错误。

Feed 级入口继续先检查 Feed metadata 的可读性；随后所有 Entry 仍必须逐条检查实际 target。
`FetchEntry`、Home、Public 和 Search 不再各自手写 private 判断。

## 各读取路径

### Profile/Group Feed

先校验目标 Feed，再逐条校验 Entry target。这会阻止公开作者 Feed 泄露其 private Group 投稿。
分页按可见 Entry 计数，不能先按原始索引应用 `start` 再过滤。

为防止含大量不可见 Entry 的 Profile Feed 扫描无界，cursor 路径采用与 Interaction Feed 同类的
有界扫描预算：

```text
max(page_size * 10, 300)
```

预算耗尽时 cursor 锚定最后扫描位置，即使页面不足 `page_size` 也允许返回 `next_cursor`。旧
`Start/PageSize` 链接继续兼容，但 Start 必须表示跳过的可见 Entry 数。

### Entry permalink 与展开接口

`FetchEntry` 调统一 resolver；httpd 的 permalink、展开 Like/Comment、编辑前读取等已经传递
session `ViewerUuid`，不再保留独立判断。不可见返回 403，Entry 或 target missing/deleted 返回
404。这里明确选择 403 而不是把 private 权限不足伪装成 404：登录用户需要区分“内容存在但当前
无权访问”和“内容不存在”，以获得一致的 follow/request UX。该选择会向已知 UUID 的调用方泄露
Entry 存在性，但 UUID 本身不是授权凭据；内容、作者快照和 target metadata 不随 403 返回。知道
Entry UUID 仍不能成为访问授权。

### Home

Home 首先要求 `ViewerUuid == ProfileUuid`，随后逐条调用 resolver。退出 private Group、被移除
成员或 Feed 改为 private 后，尚未清理的 TimelineIndex 行必须立即在读取时隐藏。不可见行可由
后续 rebuild/audit 收敛；是否在请求中懒删 derived row 不影响授权结论。

### Public

Public 保持 fail-closed：只有 target 可解析、存在、未删除且当前为 public 的 Entry 才能返回。
Public timeline 是派生数据而非权限事实，旧行必须在读取时重新校验。

### Search

每个 hit 调用 resolver：

- 当前 viewer 无权读取 private target：跳过，不删除搜索文档；
- target missing/deleted 或 Entry identity 永久损坏：作为 unusable document 清理；
- 存储故障：中止请求。

Search 暂时保留现有 `Start/PageSize` 协议；权限收口不借机调整分页 RPC。

### Likes/Comments

`/feed/:name/likes` 与 `/feed/:name/comments` 继续 owner-only。本项不会因为统一权限就自动开放
其他用户的互动页。

owner-only 只决定谁能读取这份互动历史，不代表其中所有 Entry 永远可见。用户退出 private
Group 或失去某 private Feed 的 Follow 边后，对应 Entry 必须从互动页隐藏：

```text
viewer == interaction owner
AND
viewer 当前可读 Entry target
```

Like/Comment timeline 是派生索引；暂时不可见的行不删除，未来重新获得权限后可再次显示。

## Mutation 权限

读取统一后，还必须关闭以 mutation 作为读取旁路的情况。Like、Unlike、Comment create/edit/delete
均会返回 Entry，目前只检查 actor Profile 或互动所有权，没有检查 actor 当前能否读取 target。
这些操作在 mutation 前必须调用同一 Entry 可见性判断；不可见时返回 `PermissionDenied`。

`authorizeEntryPost` 另有相邻缺口：target 不是 Group 时当前直接允许，loopback RPC 调用方理论上
可以把自己的 Entry 写入另一个 user Feed。该修复应作为本项的独立 mutation 提交：普通用户只
能向自己的 Feed 或自己有投稿权的 Group 写入；可信 Service/system producer 继续走现有私有
内部入口。不得借此改变 `PostEntry` protobuf RPC 契约。

当前 gRPC 只监听 loopback，actor/viewer 字段仍由可信 ffweb 断言。本项收紧领域权限，但不声称
解决恶意本机进程伪造 principal；若未来开放 gRPC，必须先设计经过认证的 principal。

## 测试与验收

先为 resolver 写完整矩阵测试，再用少量跨入口集成测试证明所有读取路径已接线，避免为每条
路径复制整张矩阵。

必须覆盖：

1. private user Feed 与 private Group 的 owner、follower/member、super、outsider、anonymous；
2. nonempty invalid viewer、missing/deleted viewer；
3. missing/deleted/非法 target 与真实存储错误；
4. private Group Entry 经 Group Feed、作者公开 Feed、permalink、Home、Search、Interaction；
5. 用户退出 Group 或被移除后，Home、Search、permalink 和 Interaction 立即拒绝同一 Entry；
6. Home 请求其他 `ProfileUuid` 被拒；
7. Public 不返回 private、deleted、missing 或非法 target；
8. Like/Comment 对不可见 Entry 被拒，且失败不产生权威或派生写入；
9. 公开 target 的匿名读取行为保持不变；
10. legacy 与 cursor Feed 路径结论一致，过滤后 cursor 在扫描预算内可继续翻页；
11. HTTP 将 `PermissionDenied` 稳定映射为 403、`NotFound` 映射为 404。

验收标准：同一 Entry 在 Feed、作者 Feed、permalink、Home、Public、Search 和 Interaction 中对
同一 viewer 得到一致允许/拒绝结果；任何 Entry-returning mutation 也不能成为 private 内容旁路。

## 已完成的实施步骤

1. ✅ 增加 resolver characterization/权限矩阵测试，先证明作者 Feed 泄露、Home 越权和 target
   fail-open；
2. ✅ 实现 viewer/target resolver，严格区分 denied、missing/deleted 与存储错误；
3. ✅ 接入 legacy/cursor Feed、permalink、Home、Public 和 Search；
4. ✅ 修正过滤后的 Feed 分页预算与 cursor 锚点；
5. ✅ 接入 Interaction Feed，继续保持 owner-only；
6. ✅ 接入 Like/Comment mutation；
7. ✅ 单独收紧向其他 user Feed 投稿；
8. ✅ 补 HTTP 状态测试并同步 `docs/feed.md`、`docs/group.md`。
