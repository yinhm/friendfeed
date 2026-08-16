# Group 设计规范

Group 是一种可由多个用户共同订阅和投稿的特殊 Feed。它复用 Profile、Follow/Follower、
Entry、timeline 和 FeedService，不是能够登录的用户，也不建立第二套社交图。

本文定义目标契约；当前实现与目标的差距列在文末。在相应服务端权限落地前，前端按钮不能
被视为授权边界。

## 核心模型

```text
Profile(Type=group)                 GroupAdmin
Group 身份和 Feed metadata           group UUID + user UUID

Follow                              Entry
user UUID -> group UUID             ProfileUuid = 发帖用户
表示用户已经加入 Group              FeedUuid = Group UUID
```

- Group 使用普通 Profile UUID、全局唯一 ID、名称、头像和 description；`Type` 固定为
  `group`。
- Follow/Follower 是成员关系的唯一来源：订阅 Group 就是加入 Group，取消订阅就是退出。
- GroupAdmin 是成员之上的权限角色，必须按稳定 UUID 存储；Profile ID 和 Profile 快照
  不能作为权威身份。
- Group 没有 OAuth identity，不能登录，也不能作为可信 actor 发起管理或投稿请求。
- FeedService 可以绑定到 Group，但只能由 Group admin 管理；OAuth 凭据仍属于具体
  FeedService，不属于全局 Service。

必须始终满足：

```text
GroupAdmin(group, user) => Follow(user, group)
每个未删除 Group 至少有一个 admin
admin 不得直接退出 Group
Group Entry 的作者始终是真实用户；外部 Service 导入除外
```

## 权威数据

Group identity 继续存放于 Profile/UserMap。成员关系继续使用现有 Follow/Follower 两条互为
反向的边，并在同一 Pebble batch 中写入或删除。

GroupAdmin 应使用独立表：

```text
key   = table prefix | group UUID(16) | admin user UUID(16)
value = nil
```

建议使用 100 段下一个空闲表号 `TableGroupAdmin = 114`（111-113 已被 Service 占用）；
表号必须在实施变更时统一登记到 `model/types.go`、`docs/database_design.md` 和
`AGENTS.md`，以实际登记为准。旧 `Feedinfo.Admins []*Profile` 及 `Graph.admins` 快照仅
作为迁移输入，在 GroupAdmin 上线后不再是运行时权限来源。不得按可回收的 Profile ID
授权。

## 创建 Group

新增明确的 `CreateGroup` mutation，不复用通用 `PostFeedinfo` 隐式创建：

```text
CreateGroup(actor_uuid, id, name, description, picture, private)
```

创建者必须是存在且未删除的 user Profile。以下写入必须处于同一临界区和 Pebble batch：

1. 创建 `Profile(Type=group)`；
2. 创建 Group ID 到 UUID 的 UserMap；
3. 写入 creator 到 Group 的 Follow；
4. 写入 Group 到 creator 的 Follower；
5. 写入 creator 的 GroupAdmin。

Group ID 与 user ID 共用全局命名空间、格式校验和 UserRenameMap 保留规则。创建者自动加入并
成为 admin，但没有独立于 GroupAdmin 的永久 owner 特权。任一步失败都不得留下半个 Group。

首版若尚未实现 private Group 的审批/邀请流程，`CreateGroup(private=true)` 必须明确拒绝，
不能创建一个成员无法正常读取或加入的半可用 Group。

## 加入、退出与成员管理

对 Group：

```text
Follow Group   = Join Group
Unfollow Group = Leave Group
```

公开 Group 允许登录用户直接加入。普通成员可以主动退出；admin 退出必须先由另一 admin 撤销
其角色。成员和 admin mutation 遵循以下规则：

- 提升 admin 前，目标用户必须已经是成员；
- admin 不能直接退出，也不能被普通的成员删除操作移除；
- 不允许撤销或删除最后一个 admin；
- admin 可以移除普通成员；要移除另一 admin，必须先完成合法降级；
- Follow 和 Follower 必须原子维护，失败时不得留下单边关系；
- Join/Leave 成功后必须使该用户的 Home timeline 异步重建或失效，不能只改变未来 fanout。

用户账号注销（Profile soft delete）视同退出其所有 Group，因此受同一约束：当用户
仍是任一未删除 Group 的唯一 admin 时，注销必须被拒绝，并列出这些 Group；用户须
先移交 admin，或行使最后 admin 的权利删除该 Group。该检查必须与账号 soft delete
处于同一临界区，防止与并发的 admin 降级/移交竞争后留下「未删除但无有效 admin」
的 Group——这正是 audit 要抓的状态，不能靠 audit 兜底制造。

私有 Group 的 Join 必须经过邀请或 join request/批准。批准后仍以 Follow/Follower 表示最终
成员关系，不额外建立另一套 Member 表。申请状态是工作流数据，不能冒充已生效的 membership。

现有 `GraphFollow` RPC 可以保留兼容入口，但目标是 Group 时必须进入同一套 Join/Leave 领域
逻辑，不能直接无条件写删两条边。

## 可见性与投稿

权限矩阵：

| 操作 | 公开 Group 非成员 | 普通成员 | Group admin | super |
| --- | --- | --- | --- | --- |
| 查看 Group Feed | 允许 | 允许 | 允许 | 允许 |
| 加入公开 Group | 允许 | 已加入，幂等 | 已加入，幂等 | 允许 |
| 向 Group 投稿 | 拒绝 | 允许 | 允许 | 允许 |
| 管理成员/admin | 拒绝 | 拒绝 | 允许 | 允许 |
| 管理 FeedService | 拒绝 | 拒绝 | 允许 | 允许 |
| 修改/删除 Group | 拒绝 | 拒绝 | 允许 | 允许 |

私有 Group 仅成员、admin 和 super 可读取；非成员不能靠知道 Group ID、Entry UUID、Search
结果或旧 timeline 行绕过。private Group 内容不得进入 Public timeline，Search 返回前也必须
执行可见性检查。

投稿权限必须在 ffdb mutation 边界验证。httpd 可以隐藏输入框，但不能成为唯一权限检查。
Group Entry 的 canonical identity 为：

```text
ProfileUuid = 实际发帖用户 UUID
FeedUuid    = Group UUID
From        = 发帖用户快照
To          = 唯一的 Group 快照
```

服务端根据 Profile/FeedUuid 重建 From/To，不信任客户端提交的快照或多个 target。通过外部
FeedService 导入的 Entry 是明确例外：它属于目标 Group，可以使用 Group 作为来源快照，但
`Via` 必须记录外部 Service，不能伪造某个 admin 为作者。

## 内容权限与审核

- Entry 作者可以编辑、删除自己的投稿；
- Group admin 可以删除 Group 内的 Entry，但不能编辑其他作者的正文；
- Comment 作者可以编辑、删除自己的 Comment；
- Group admin 可以删除 Group Entry 下的 Comment，但不能编辑或冒充 Comment 作者；
- super 保留恢复和审核权限；
- admin 审核只适用于 `Entry.FeedUuid == group UUID` 的内容，不能因 Entry 的其他快照字段或
  cross-post 表象扩张到另一个 Feed。

权限判断必须使用 UUID，并在 model/server 层保持同一规则。前端 commands 只是服务端授权
结果的展示提示。

## Timeline

Group 使用普通 direct EntryIndex。新 Group Entry 只向当时的成员 fanout，并沿用现有有效
TimelineState、有界 Home 和失败返回规则。

成员关系变化还必须处理存量缓存。异步重建复用现有 task queue（表 203-207）：Join/Leave
成功后入队幂等的 home rebuild task，由 `model.BuildHomeTimeline`/`ReplaceHomeTimeline`
完成有界重建，不新建第二套后台机制。

- Join 后异步重建该用户的 bounded Home，使 Group 的近期内容可见；
- Leave 或被移除后异步重建，移除该 Group 的 Home 行；
- private Group 的 stale timeline 行即使暂未清理，读路径也必须重新校验可见性；
- 所有成员和 timeline 扫描必须流式、有界，不得把大型 Group 的成员或历史 Entry 全量装入
  内存。

Group 本身不是 viewer，不写 TimelineState。Public timeline 仍是保留 viewer，不属于 Group
membership 模型。

## Group metadata、rename 与删除

只有 Group admin 或 super 可以修改 Group metadata。Group rename 使用与 user Profile 相同的
ID 冲突和 UserRenameMap soft redirect 规则，不改变 Group UUID、成员边或 admin 关系。

删除 Group 采用 soft delete：

- 立即禁止 Join、投稿和新的 Service 投递；
- 历史 Entry、Like、Comment 不在请求中同步批量删除；
- Follow/Follower、timeline 和 FeedService 的无上限清理由后台维护任务或独立运维命令有界
  完成；
- Group ID 的保留或回收遵守统一的 Profile 删除/rename 规则；
- 删除 Group 不得删除任何成员用户或其个人 Feed 内容。

删除前仍须满足可信 actor 授权；最后一个 admin 可以删除整个 Group，但不能通过先删除自己
制造一个无 admin 的存活 Group。

## API 边界

目标 mutation 应明确表达领域动作：

```text
CreateGroup
UpdateGroup
JoinGroup
LeaveGroup
AddGroupAdmin
RemoveGroupAdmin
RemoveGroupMember
DeleteGroup
```

读取至少应能返回 Group metadata、当前用户的 member/admin 状态，以及分页的成员/admin
列表。不要把完整成员列表塞进每次 Feed 响应。另需有界分页的
`ListUserGroups(user_uuid, limit, cursor)` 返回用户已加入的 Group 列表，服务 sidebar
导航与 Group 列表页；契约见 `docs/group_navigation.md`。现有 `FetchGraph` 刻意不返回
following，不得为此把全量订阅塞回 Graph 响应。

当前 gRPC 只监听 loopback，可暂时在请求中携带 actor UUID；所有 mutation 仍必须在 ffdb
重新验证 actor。若将来对外开放 gRPC，必须由认证 principal 注入 actor，不能信任客户端自报。

## 当前实现差距

实现状态追踪（✅ 为已落地项）：

- ✅ 原子 CreateGroup RPC 已落地（创建者同事务成为成员/admin，ID 冲突与失败无残留）。
  `PostFeedinfo` 仍可隐式创建任意 Type 的 Profile——这是**有意保留**：stock/系统
  feed（`server/stock.go`、`CreateSystemProfile`）依赖它创建无成员语义的 Type=group
  系统 feed，用户面 Group 创建事实收口于 CreateGroup。若将来要彻底关闭此旁路，需先
  给系统 feed 独立 Type 或内部直写路径。
- ✅ admin 权威已切换到 GroupAdmin 表。**迁移注意**：现有 Group 的 admin 若仅存在于
  legacy Feedinfo.Admins 快照中，需通过 super 手动执行 JoinGroup + AddGroupAdmin
  引导，或运行一次性 backfill 命令（当前默认无生产 Group，暂未实现 backfill）；
- ✅ GraphFollow 对 Group 目标已路由进 Join/Leave 领域层（admin 退出拦截、最后 admin
  保护在所有入口一致生效）。
- ✅ PostEntry 已在 mutation 边界验证 Group 成员资格（`authorizeEntryPost`），
  FeedService 自发帖（ProfileUuid == FeedUuid）豁免；
- ✅ private Group 读取已闭环：legacy 与 cursor 两条 Feed 路径、FetchEntry、Home stale
  行重校验、Search 结果过滤均执行成员/super 可见性检查；private Group 的 Join 在审批
  流程落地前一律拒绝（`StageJoinGroup` 与 CreateGroup 一致）。
- ✅ Join/Leave/RemoveMember 在同一 batch 入队幂等 `home.rebuild` task，按当前 Follow
  边有界重建 Home（docs/task_queue.md）。
- ✅ `ListUserGroups`（docs/group_navigation.md 契约）与 `GetGroup`/`ListGroupMembers`
  读取 API 已落地。
- ✅ Group admin 的 Entry/Comment moderation 已落地（delete-only，admin 不可编辑
  他人内容）。UpdateGroup、DeleteGroup RPC 已实现。
- 账号注销（MarkDelete）已执行唯一 admin 拦截并列出阻塞 Group。

有意暂缓（规范允许的缺口）：

- private Group 的邀请/join request 审批流程（在此之前 private 创建与加入均拒绝）；
- 删除 Group 后 Follow/Follower、timeline、FeedService 的无上限清理（由 audit 报告，
  后台/运维命令有界清理）；
- `/groups` 完整列表页（sidebar 超过 20 个时只截断，不出死链）。

## 实施顺序与验收

1. 先增加 GroupAdmin 稳定 UUID 表、resolver 和权限矩阵测试；
2. 实现原子 CreateGroup，并锁定 ID 冲突和失败无残留；
3. 将 Group Join/Leave、admin 升降级和成员移除收口到同一领域层；
4. 将投稿、metadata、FeedService 和审核授权统一下沉到 ffdb；
5. 修复 private visibility，或在审批流程完成前拒绝创建 private Group；
6. 接入 Join/Leave 后的异步 Home rebuild；
7. 最后实现 Group 创建、成员、admin 和 Service 管理 UI（sidebar 导航与创建页规范
   见 `docs/group_navigation.md`，主题体系见 `docs/theme.md`）；
8. 增加 audit，检查 admin 非成员、无 admin Group、单边 membership 和 deleted Group 残留。

验收测试至少覆盖：创建者原子成为成员/admin、普通用户加入/退出、admin 退出被拒、最后 admin
降级被拒、唯一 admin 注销账号被拒、非成员投稿被拒、admin 只能删除不能编辑他人内容、private 数据不可旁路读取、关系
变化后 timeline 收敛，以及所有失败路径不产生单边或半状态。
