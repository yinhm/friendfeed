# Group 发现页设计

本文定义公开 Group 发现页。领域规则、membership、admin、private 内容权限仍以
`docs/group.md` 为准；目录只负责回答“有哪些 Group，以及按什么顺序展示”。

## 产品语义

```text
/groups                  公开 Group 发现页
/feed/:id/groups         当前用户加入的全部 Group
sidebar Groups           当前用户 Group 的个人活跃度前十
```

- `GET /groups` 对匿名用户开放。
- 所有未删除的 `Profile.Type == "group"` 默认可发现；Public、Private Group 都展示公开
  metadata，Private 名称后显示 lock icon，但不因此开放内容。
- 发现页只展示头像、名称、锁状态和简介，样式与现有用户 Group 列表一致。
- 列表不计算成员数、admin、membership 或 pending request，也不提供行内 Join/Remove 操作。
  用户点击 Group 进入 `/feed/:id`，由现有 Feed 页面统一展示状态并处理 Join、申请和管理。
- 第一版按最近活动时间排序，不做推荐、分类、全文搜索或个性化排名。Group 创建、向 Group
  新建 Entry、首次 Like 和新建 Comment 会更新活动时间；编辑、Unlike、删除和成员变化不移动
  目录位置。
- 历史 stock/system Group 暂不特殊过滤；其退役问题记录在 `docs/open_decisions.md`，不把
  临时兼容逻辑写入 Group 目录。

## Group directory

新增可丢弃、可重建的派生表：

```text
TableGroupIndex = 119

key   = T119 | reverse_activity_ms(8-byte BE) | group_uuid(16-byte raw)
value = empty
```

`reverse_activity_ms = ^uint64(activity_ms)`，Pebble 前向扫描直接得到最近活动的 Group；同一
毫秒以 UUID 升序稳定排序。Profile 是 metadata 权威来源，目录行仅表达顺序，不保存 Profile、
成员数或权限状态。

运行时维护规则：

- `CreateGroup` 在创建 Profile 的同一 batch 写首条目录行，时间使用服务端时间。
- 向 Group 新建 Entry、首次 Like 或新建 Comment 时，在权威 mutation 成功后移动目录位置；
  目录更新失败必须返回错误并由 audit/rebuild 收敛，不回滚已提交的权威数据。
- 为定位旧 key，移动时按 `T119` 扫描最多一个完整目录并匹配 UUID。Group 数量当前较小，第一版
  接受这一成本，避免为定位记录再建第二张表；实现必须关闭 iterator、检查错误且保持恒定内存。
- soft delete 不要求请求路径扫描删除目录行；读路径跳过 deleted/missing Profile，audit/rebuild
  清理孤儿。
- Comment 编辑、重复 Like、Unlike、Join、Leave 和 metadata 编辑不更新目录。

未来若需要更复杂 ranking，应更换 activity 的计算规则并重建 T119，而不是向 value 填入成员或
viewer 状态。

## gRPC 契约

新增兼容 RPC，不改变 `ListUserGroups`：

```proto
rpc ListGroups(ListGroupsRequest) returns (ListGroupsResponse);

message ListGroupsRequest {
  int32 limit = 1;   // default 30, max 100
  string cursor = 2; // opaque directory position
}

message ListGroupsResponse {
  repeated Profile groups = 1;
  string next_cursor = 2;
}
```

读取规则：

- cursor 只编码 `reverse_activity_ms | group_uuid`，服务端补 T119 前缀重建 seek key；严格从
  cursor 之后继续。
- 单次最多扫描 `max(limit*3, 100)`、上限 300 条目录行。Profile 缺失、deleted 或非 Group 时
  跳过，但仍推进 cursor；因此页面可能不足 limit 而仍有 `next_cursor`。
- RPC 不接收 viewer，不读取 Follow、GroupAdmin 或 FollowRequest，也不返回用户关系状态。
- 所有 iterator 必须关闭并检查 `Error()`；请求路径不得回退为全表 Profile 扫描。
- 排名流翻页期间可能因 Group 活动上移出现重复或漏看，这是动态目录的弱一致性边界。

## Web 路由与 UI

- `GET /groups`：公开发现页，不挂 `LoginRequired`。
- `GET /feed/:name/groups`：登录用户自己的完整 Group 列表，继续使用 `ListUserGroups`；解析
  `:name` 后必须与 session UUID 相同，其他用户返回 403。rename 时保留 `/groups` 后缀并 302 到
  新 ID。
- `/groups/create` 以及 Group mutation 路由继续要求登录；注册顺序不得让公开 `/groups` 放宽其
  权限。
- 发现页与用户 Group 页复用同一列表模板/partial：`.item-list`、头像、名称、private lock、简介。
  两页只改变标题、数据源与分页链接。
- sidebar 最多显示 10 个个人活跃 Group；`All groups…` 指向
  `/feed/{current_user.Id}/groups`。主导航另提供 `/groups` 发现入口。
- 发现页只提供 `Next »`；点击名称进入既有 `/feed/:id` 页面，不在列表复制状态或操作按钮。

## rebuild、audit 与部署

新增 `rebuild_group_directory`：

- 支持 `-group <id>` 与 `-dry-run`；全量模式流式扫描 Profile，只保留有效、未删除的 Group。
- 每个 Group 的活动时间取其最新 direct Entry；无 Entry 的历史 Group 使用 Unix epoch，新建 Group
  运行时使用真实创建时间。
- apply 可范围删除 T119 后以小 batch 重建；全流程内存有界。

`audit_store` 检查目录孤儿、重复 Group UUID、Profile 缺失/deleted/非 Group，以及有效 Group
缺少目录行。audit 只报告；rebuild 才修复。

首次部署在停服状态执行：指定 Group dry-run/apply、全量 dry-run/apply、`audit_store`，然后启动
服务。实现时将表号和编码同步登记到 `model/types.go`、`docs/database_design.md` 与根
`AGENTS.md`。

## 实施步骤

1. 固定 T119 编码，增加 Create/新 Entry/首次 Like/新 Comment 的目录维护及 model 测试。
2. 实现有界 `rebuild_group_directory`、audit 与迁移测试。
3. 实现 `ListGroups`、cursor/孤儿/扫描预算测试。
4. 将 `/groups` 改为公开发现页，把用户列表迁到 `/feed/:name/groups`，复用列表 UI 并更新导航。
5. 更新 protobuf、数据库与部署文档，执行 Go/前端全量门禁。

## 验收

- `/groups` 匿名可访问；列出有效 Public/Private Group，跳过 deleted、非 Group 和孤儿目录行。
- 列表不发起 membership/admin/request 查询，不显示相关状态或操作；点击后由 Feed 页面管理。
- 排序按最近创建/发帖/首次 Like/新 Comment 活动，cursor 在静态数据下不重不漏，短页仍能继续。
- `/feed/:id/groups` 只允许本人访问，rename 保留后缀；sidebar 仍最多十条并链接该页面。
- 两个列表页使用相同 item 样式，Private 使用 lock icon，不显示 `private` 文本。
- rebuild 幂等、dry-run 零写入、audit 可发现漂移，所有全表流程内存有界。
