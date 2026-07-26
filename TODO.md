# TODO：profile 改名后 like/comment/command 状态不同步

## 现象

profile edit 修改 id/name 后，entry 作者已能正确刷新（`fmtEntryProfile`），但：

- comment/like 的作者名和 `/feed/<id>` 链接仍是旧值，旧 id 链接 404；
- 改名前自己赞过的 entry 显示 "Like" 而非 "Unlike"，再点会产生重复 like，也无法取消；
- 改名前自己发的 comment 不再显示 Edit/Delete，调用编辑接口返回 `403: perm error`。

## 根因

comment/like 的 `From`（`pb.Feed`）在写入时**只存了 id/name/type 快照，没有存 uuid**：

- `httpd/src/server.go` `CommentHandler`：`pb.Feed{Id, Name, Type}`
- `model/like.go` `Like()`：`pb.Feed{Id, Name, Type}`

而所有消费点都拿这个会过期的 `From.Id` 与当前 `profile.Id` 直接比较：

| 失效点 | 位置 | 后果 |
| --- | --- | --- |
| like 状态判定 | `pb/helper.go:27` `RebuildCommand` | 旧 like 认不出是当前用户 → 显示 Like/重复 like |
| comment 可编辑判定 | `pb/helper.go:52` `RebuildCommentsCommand` | 自己的旧 comment 无 Edit/Delete |
| comment 编辑权限 | `model/like.go:54` `Comment()` | 旧 comment 编辑 → `403: perm error` |
| like 去重/删除 | `model/like.go:17,37` `Like/DeleteLike` | 重复 like；旧 like 无法取消 |
| 作者名/链接显示 | `entry.jsx` / `entry-like.jsx` 渲染 `from.id/from.name` | 旧名字、旧链接 404 |

entry 本体没这个问题，因为它有稳定的 `ProfileUuid` 可以重查 profile；comment/like 缺这个稳定锚点。id 一旦 rename（UserMap 旧映射删除），旧快照就**无法解析、无法比对**。

## 方案

按依赖顺序分四步，每步独立测试、独立提交。（第 1、2 步曾是 2026-07-24 被撤销的实现，本次保留并补上当时缺失的第 3 步。）

### 1. 新数据写入稳定 uuid

- `model/like.go` `Like()`：`From` 增加 `Uuid: profile.Uuid`；
- `httpd/src/server.go` `CommentHandler`：`From` 增加 `Uuid: profile.Uuid`。

`pb.Feed.Uuid` 字段已存在，无协议变更。

### 2. 读路径统一刷新

- `server/helper.go` `fmtEntryProfile` 遍历 `entry.Comments`/`entry.Likes`，用 `fmtFeedRef` 刷新 `From.Id/Name/Picture`：优先 `From.Uuid` 解析，缺失回退 `From.Id`，解析失败保留快照且不报错（一条旧 comment 不能拖垮整个 feed）；
- 效果：显示恢复；且 httpd 在**刷新后**的数据上跑 `RebuildCommand`/`RebuildCommentsCommand`，like 状态和 comment 的 Edit/Delete 对带 uuid 的数据自动恢复，无需改 `pb/helper.go`。

### 3. 写路径按 uuid 比对（操作的是库里未刷新的原始数据，必须单独修）

- 新增比较辅助（如 `model.sameProfileRef(from *pb.Feed, profile *pb.Profile) bool`）：双方都有 uuid 时比 uuid，否则退回比 id；
- 接入点：
  - `model/like.go` `Like()`/`DeleteLike()` 去重与定位；
  - `model/like.go` `Comment()` 的 403 权限复核；
- 同时把新 comment/like 的 `From.Id` 写成当前 id（已是），保证无 uuid 的旧逻辑路径行为不变。

### 4. legacy 数据边界（需先决策再动手）

第 2、3 步只能修复**带 uuid** 的数据。改名前产生的 comment/like 没有 uuid，旧 id 映射已删，无法归属：

- 方案 A：接受现状——旧数据显示旧名、不可编辑（评论者重新编辑自己新评论即可，影响随时间消失）；
- 方案 B：rename 时在 UserMap 保留 `oldid → uuid` 别名（标记 alias，允许被他人注册新 id 时覆盖），旧链接/旧比对全部续命；代价是 id 命名空间语义复杂化；
- 方案 C：rename 时全库扫描重写该用户所有 comment/like 快照；代价大、非原子，不推荐。

建议先落地 1–3，legacy 走方案 A；若用户反馈强烈再上方案 B。

## 测试计划

- `server/helper_test.go`：comment/like 作者 rename+改名后刷新；无 uuid 且 id 失效的旧快照原样保留、不报错（含 nil From 防御）；
- `model/like_test.go`：新 like 记录 uuid；rename 后 `Like` 不重复、`DeleteLike` 能删、`Comment` 编辑权限按 uuid 放行、他人仍 403；
- `httpd` handler 层：comment 提交后 `From.Uuid` 非空；
- 门禁：`go build/vet/test ./...`，前端 `lint/typecheck/test/build/e2e`。

## 不做的事

- 不改 `pb` 协议、不改持久化 key/schema；
- 不动 `RebuildCommand`/`RebuildCommentsCommand` 的比较逻辑（靠刷新后的数据自然正确）；
- 不做 rename 时的全库重写。
