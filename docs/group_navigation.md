# Sidebar Group 导航设计规范

本文定义 sidebar 中独立的 Group 导航区域：创建 Group 的入口页面，以及当前用户
已加入的 Group 列表（自己创建的 Group 也在此列表中，因为 `CreateGroup` 会写入
creator 的 Follow 边）。领域规则、权限与 mutation 契约见 `docs/group.md`；本文
只定义读取接口与 UI。sidebar 是展示层，不是授权边界。

## 数据来源：ListUserGroups

现有 `FetchGraph` 出于体量顾虑不返回 following（`server/helper.go` 的
`BuildGraph` 刻意跳过 subscriptions），当前没有任何可复用的「用户已加入的
Group 列表」读取 API。必须新增有界读取：

```text
rpc ListUserGroups(ListUserGroupsRequest) returns (ListUserGroupsResponse)

ListUserGroupsRequest  { user_uuid, limit, cursor }
ListUserGroupsResponse { groups: repeated Profile, next_cursor }
```

语义：

- 按 `Follow` 表前缀（`prefix | user UUID`）流式迭代该用户的订阅边，逐条解析
  目标 Profile，只保留 `Type == "group"` 且未删除的；解析失败或已删除的边跳过，
  不视为错误。
- `limit` 计数的是**返回的 group**，不是扫描的边：持续扫描直到凑满 `limit` 个
  group 或订阅边迭代耗尽。`limit` 默认 100，上限 200，服务端强制截断。
- 扫描本身另设上限：单次调用最多扫描 1000 条边，到达即返回当前已收集结果，
  防止订阅几乎全是用户的极端账号退化为无界前缀扫。因此一页可能不足 `limit`
  甚至为空却仍携带 `next_cursor`，调用方必须按 `next_cursor` 是否为空判断
  迭代结束，不得按本页条数判断。
- `cursor` 为上一页**最后一条被扫描的边**（而非最后返回的 group）的 feed
  UUID，编码索引位置、按当前前缀重建 key，遵循与 `FetchFeed` cursor 相同的
  「不得解释为业务 ID」规则。
- 迭代顺序即 Follow key 顺序（feed UUID 序），服务端不排序；展示排序是 httpd
  的职责。
- 返回的 Profile 只需列表展示字段（uuid、id、name、picture、type、private），
  不携带成员、admin 或 entries。
- 这是纯读取，任何登录用户都只能列出自己的订阅；在 gRPC 仍限 loopback 的现状
  下由 httpd 保证 `user_uuid` 来自会话，与 `docs/group.md` 的 actor 约定一致。
- `order_by_activity=true` 是 sidebar 专用模式：忽略 cursor，读取该用户固定的
  Group activity Meta key，按已物化顺序返回前 `limit` 个仍有效且仍为成员的 Group。
  Meta key 尚未迁移时退回普通 membership 顺序，不能让升级窗口出现空导航。

## Group activity 排名

sidebar 排名是可重建派生数据，不是权限或 membership 来源。每个用户只有一个 key：

```text
TableMeta | "group-activity/v1/" | user UUID
  -> JSON [{"group_uuid":"...","score":123}, ...]
```

JSON 始终按 `score DESC, group_uuid ASC` 排好；同分使用稳定 UUID 排序，读路径不再
扫描或临时排序全部 Follow。创建者身份不能从可转让的 GroupAdmin 推断，因此另有
稳定 metadata：`TableMeta | "group-owner/v1/" | group UUID -> creator UUID`。

评分仅统计当前仍存在的事实，并且只排名用户当前加入的 Group：创建该 Group +100，
向 Group 新发一条 Entry +10，保留一个 Like +3，保留一条 Comment +4；编辑不重复
计分，Unlike、删除 Comment、删除 Entry 会扣除对应分值且最低为 0。点击不记录。
Create/Join/Leave 与 score 行在同一 batch 维护；Entry/Like/Comment 的 score 更新与
权威 mutation 同一 batch。JSON 损坏必须报错，不得静默猜测。

`rebuild_group_activity` 按用户的 Follow、EntryIndex、LikeTimeline、CommentTimeline
流式重建；支持 `-user <id>` 与 `-dry-run`。部署先对单用户 dry-run，再全量 apply。

## httpd 数据流

- helper `UserGroups(c)` 每次调用 `ListUserGroups(order_by_activity=true, limit=10)`；
  该 RPC 只读取一个已排序 JSON key 和至多十个 Profile，不再使用会掩盖实时行为更新
  的 5 分钟 httpd cache。
- 缓存失效：`FollowHandler` 成功、未来的 CreateGroup/JoinGroup/LeaveGroup
  handler 成功后，删除该用户的 `groups:` 缓存键。注意这是**新增行为**：现有
  `FollowHandler` 不做任何缓存失效（连 `graph:` 键也不删，只靠 5 分钟过期
  兜底），实施时必须补上，不能假设已有。5 分钟兜底过期意味着通过其他路径
  发生的成员变化最迟 5 分钟收敛，可接受。
- `Server.HTML` 在渲染带 sidebar 的页面（现状即 `!onpage`）且已登录时注入
  `user_groups`；未登录不注入、不请求。

## Sidebar UI

在 `layout.html` 的 sidebar 中新增独立 section，位于主导航（Home/My feed/
Public）与 Account 之间：

```html
<div class="section">
    <h3>Groups</h3>
    <ul>
        {% for g in user_groups %}
        <li><a href="/feed/{{ g.Id }}" title="{{ g.Id }}">{{ g.Name }}</a></li>
        {% endfor %}
        <li><a href="/groups/create">Create a group</a></li>
    </ul>
</div>
```

规则：

- 仅登录用户可见该 section；未登录完全不渲染。
- 空状态：没有任何 Group 时 section 仍渲染，仅含 "Create a group" 链接，作为
  功能引导；不额外显示占位文案。在 create 链接尚未上线的阶段（见实施顺序
  第 2 步），列表为空则整个 section 不渲染，不留下只有标题的空框。
- sidebar 最多显示 10 个 Group，并始终显示 "All groups…" 指向 `/groups`。
  主导航在 "My feed" 下也提供 `/groups` 入口。完整列表页逐页消费
  `ListUserGroups.next_cursor` 直到结束，不受 sidebar 单页退化边界影响。
- private Group 对成员正常显示（列表来自用户自己的 Follow 边，天然只含自己
  可见的内容），名称后加锁形标记（文本 `(private)` 即可，不引入图标资源）。
- 展示用 `Name`，`title` 属性带 `Id`；链接沿用既有 `/feed/:name` 路由。
- 移动端（<=600px）沿用现有 `.menu` 的 `<details>` 折叠行为，无特殊处理。
- 样式使用 `docs/theme.md` 的 token，不新增裸颜色值。

## 创建 Group 页面

- 路由：`GET /groups/create` 渲染表单页，`POST /groups/create` 提交；两者都在
  `LoginRequired` 之内。页面依赖 `docs/group.md` 的 `CreateGroup` RPC——在该
  RPC 落地前不注册路由，sidebar 的 create 链接同步隐藏。
- 表单字段：
  - `id`：必填。与 user ID 共用全局命名空间与格式校验（见 `docs/group.md`）；
    前端仅做格式提示，冲突与保留名以服务端 `CreateGroup` 的错误为准，错误回显
    在表单上。
  - `name`：必填。
  - `description`：选填。
  - `picture`：选填，缺省由服务端沿用 wallpaper 默认头像逻辑。
  - `private`：复选框。在 private 审批流程落地前禁用并附说明（服务端本就拒绝
    `private=true`，见 `docs/group.md`），不允许出现「前端能勾、后端必拒」的
    半可用状态。
- 提交成功后重定向到 `/feed/{id}`，并使当前用户的 `groups:` 缓存失效；创建者
  此时已因 `CreateGroup` 的原子写入出现在自己的 Group 列表中。
- 模板为新的 SSR 页面（如 `groups_create.html`），复用 layout 与现有表单样式，
  不引入 React 依赖。

## 实施顺序与验收

1. 新增 `ListUserGroups` RPC（proto + server + 有界迭代测试）。此步不依赖
   `CreateGroup`，可先行——用户现在就能通过既有 subscribe 动作加入 Group。
2. httpd `UserGroups` helper、缓存与失效、layout.html 的 Groups section
   （此阶段 create 链接尚未渲染）。
3. `CreateGroup` RPC 落地后（`docs/group.md` 实施顺序第 2 步），新增
   `/groups/create` 页面并在 sidebar 显示 create 链接。
4. `/groups` 完整列表页逐页读取并展示当前用户加入的全部 Group。

验收至少覆盖：未登录不渲染 section 也不发起 RPC；加入/退出后 sidebar 在缓存
失效路径上立即收敛、其余路径 5 分钟内收敛；超过 10 个 Group 时截断并链接完整列表；
private Group 仅出现在成员自己的 sidebar；创建成功后新 Group 立即出现在列表；
`ListUserGroups` 对大量订阅保持流式有界、跳过已删除 Profile 不报错，且触发扫描
上限时正确返回不满页/空页 + `next_cursor`，续页不重复、不遗漏。
