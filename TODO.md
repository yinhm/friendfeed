# TODO：用稳定 UUID 统一 comment/like 身份

## 现象

profile 修改 id/name 后，entry 作者已能通过 `ProfileUuid` 正确刷新，但 comment/like 仍依赖写入时的 `From.Id` 快照：

- comment/like 继续显示旧名字和 `/feed/<old-id>` 链接；
- 改名前自己赞过的 entry 显示 Like，再点会产生重复 like，旧 like 也无法取消；
- 改名前自己的 comment 不再显示 Edit/Delete，编辑接口返回 `403: perm error`；
- `DeleteComment` 当前没有执行所有权校验，任何能调用接口的登录用户都可能删除别人的 comment。

## 根因

comment/like 的 `From` 使用 `pb.Feed`，但当前写入只保存 id/name/type，没有保存已有的稳定字段 `Feed.Uuid`：

- `httpd/src/server.go` `CommentHandler` 手工构造 `pb.Feed{Id, Name, Type}`；
- `model/like.go` `Like` 手工构造 `pb.Feed{Id, Name, Type}`。

消费和 mutation 又把可修改的 `From.Id` 当作身份：

| 位置 | 当前行为 | 后果 |
|---|---|---|
| `pb/helper.go` `RebuildCommand` | `like.From.Id == profile.Id` | rename 后 Like/Unlike 状态错误 |
| `pb/helper.go` `RebuildCommentsCommand` | `comment.From.Id == profile.Id` | rename 后 Edit/Delete 消失 |
| `model/like.go` `Like/DeleteLike` | 按 id 去重和定位 | 重复 like、旧 like 无法取消 |
| `model/like.go` `Comment` | 比较两个客户端快照的 id | rename 后编辑失败，信任边界错误 |
| `model/like.go` `DeleteComment` | 不检查 profile | 越权删除 |
| React entry 渲染 | 直接显示 `From.Id/Name` | 旧名字、旧链接 |

entry 本体有 `ProfileUuid`，comment/like 没有被正确填充稳定身份。profile id、显示名、头像都只能作为快照，不能用于授权。

## 身份模型

`pb.Feed` 已足够表示 denormalized actor reference，不需要修改持久化消息：

```text
From.Uuid    稳定身份；关联、去重和授权使用
From.Id      写入时 profile ID 快照
From.Name    写入时显示名快照
From.Picture 写入时头像快照
From.Type    写入时类型快照
```

规则：

1. UUID 是身份，其他字段只是可刷新的展示快照。
2. 新 comment/like 必须写入 UUID。
3. server/model 根据 canonical profile 生成 `From`，不直接信任调用方提交的 `From`。
4. 带 UUID 的记录只按 UUID 比较；UUID 不同或无效时不得退回比较 ID。
5. 缺 UUID 的 legacy 记录默认保留原快照，不能用当前 `UserMap[old-id]` 自动认领。

## 实施方案

按依赖顺序执行，每步独立测试、独立提交。

### 0. 明确目标权限并先写测试

`DeleteComment` 的现状是已知漏洞，不能用 characterization test 将“任何人可删”锁成契约。先确定并测试目标规则：

- 编辑 comment：仅 comment 作者；
- 删除 comment：comment 作者、entry 作者或 super；
- 普通其他用户不能编辑或删除；
- like 不重复，unlike 只能删除自己的 like；
- nil `From`、空 UUID、非法 UUID 不 panic；
- entry 作者必须通过 `entry.ProfileUuid` 判定；缺稳定 UUID 的 legacy entry 不按 `From.Id` 获得 moderation 权限；
- super 使用 canonical `profile.IsSuper` 判定；
- group admin moderation 不在本任务中顺带扩张，若需要应单独定义；
- UI commands 应与后端目标规则一致，但不能代替服务端授权。

先写会在当前实现上失败的目标行为测试，证明 `DeleteComment` 缺少权限检查，再修改生产代码。

### 1. 集中生成 canonical actor reference

在 model 增加内部 helper（名称可调整）：

```go
func feedRefFromProfile(profile *pb.Profile) *pb.Feed
```

它统一填充：

```text
Uuid, Id, Name, Picture, Type
```

Like 和 Comment 保存时都使用该 helper。不要继续在 httpd、server、model 多处手写 `pb.Feed{...}`。

比较逻辑集中为内部 helper：

```go
func sameProfileRef(ref *pb.Feed, profile *pb.Profile) bool
```

语义必须是：

- nil 输入返回 false；
- `ref.Uuid` 存在时，解析并与 `profile.Uuid` 比较；
- UUID 无效或不一致返回 false，不得 fallback 到 ID；
- legacy 无 UUID 默认返回 false，除非将来有经过产品决策的历史 alias resolver。

### 2. command 请求先携带稳定 principal

Like 已通过 `LikeRequest.User` 传 profile UUID。Comment 应与其统一。

兼容扩展 protobuf：

```proto
message CommentRequest {
  string entry = 1;
  Comment comment = 2;
  string user_uuid = 3;
}

message CommentDeleteRequest {
  string entry = 1;
  string comment = 2;
  string user = 3;       // legacy profile ID，暂时保留
  string user_uuid = 4;  // stable principal
}
```

这是 protobuf 兼容新增，不改已有字段编号和方法：

- httpd 从 session 的 `CurrentUserUuid` 填充 `user_uuid`；
- server 只根据 `user_uuid` 查询 canonical profile；
- 既有 `user`/`comment.From` 字段保留以维持 protobuf wire/API 兼容，但不再作为授权 fallback；
- 缺失、非法或查不到的 `user_uuid` 必须拒绝请求，不能把客户端 `From.Id` 固化成一个伪造 UUID；
- server 保存 Comment 前覆盖 `comment.From`，不能信任客户端提供的 UUID/id/name；
- 长期应由 gRPC authentication/context 提供 principal；当前内部部署先用显式 `user_uuid` 过渡。

### 3. 新 comment/like 写入 UUID

- `model/like.go` `Like` 使用 `feedRefFromProfile(profile)`；
- Comment 保存前由 server/model 使用第 2 步解析出的 canonical profile 覆盖 `comment.From`；
- `httpd/src/server.go` 不再承担构造可信作者身份的职责；它提交的展示字段不能成为最终授权依据；
- 新数据必须包含 `From.Uuid`，同时保留当前 id/name/picture/type 快照。

Comment 写入依赖第 2 步，不能先从不可信 `comment.From.Id` 查 profile 再写 UUID。

### 4. 提前完成读路径 hydration

写入 UUID 后即可独立修复新数据的显示，不必等待全部 mutation 授权改造。将身份刷新职责从仅处理 entry author 的 `fmtEntryProfile` 中拆清，形成类似：

```go
hydrateEntryActorRefs
hydrateFeedRef
```

处理 entry author、comments 和 likes：

- `From.Uuid` 有效且 profile 存在：刷新 `Id/Name/Picture/Type`；
- UUID 无效、profile 不存在：保留快照，不让单条引用拖垮整个 feed；
- `From.Uuid` 缺失：保留 legacy 快照，不按当前 UserMap 自动解析；
- nil `From`：跳过，不 panic。

httpd 必须在 hydration 后执行 `RebuildCommand`/`RebuildCommentsCommand`。带 UUID 的数据会刷新成当前 ID，从而保持现有 helper 行为；长期可再评估让 command helper 直接按 UUID 判断，但不能在本任务中混入无测试重构。

这一阶段只修复部署后写入 UUID 的数据。已经落库且没有 UUID 的 comment/like 仍受 legacy 边界限制，不能通过 hydration 猜测归属。

#### 4b. 可选性能优化：resolver cache

第一版可以直接按 UUID 做 Pebble point lookup，先保证正确性。请求级 resolver/cache：

```text
map[profile UUID]*pb.Profile
```

属于可独立增加的性能优化，不作为正确性实现的前置条件。只有 profiling、查询计数或真实 feed 数据证明重复 lookup 明显时再加入，并单独测试同一 UUID 的读取次数。

### 5. 所有 mutation 按 UUID 授权

接入 `sameProfileRef`：

- `Like`：按 UUID 去重；
- `DeleteLike`：按 UUID 定位；
- `Comment` 更新：stored comment 的 `From.Uuid` 必须属于 canonical profile，只有 comment 作者可编辑；
- `DeleteComment`：comment 作者、`entry.ProfileUuid` 对应的 entry 作者或 super 可以删除，其他用户拒绝；
- 更新 comment 时保留不应由客户端覆盖的字段，例如原始 author、Date、Id，只更新允许编辑的 body/rawBody。

legacy 无 UUID 的记录不能仅凭当前 ID 获得修改权限。若没有可靠历史映射，保持只读比错误授权安全。

### 6. Legacy 数据策略

默认采用方案 A。

严格 no-fallback 会冻结所有升级前且没有 UUID 的身份操作，不只影响已经 rename 的用户：

- 即使用户从未改名，旧 like 也不能安全证明属于当前用户，因此不能 unlike；
- 旧 comment 不能仅凭相同 ID 恢复编辑/删除权限；
- 旧 like 不参与 UUID 去重，用户再次 Like 可能形成一条新的 UUID like；
- hydration 只能保留旧快照，不能刷新其名称和链接。

这是安全性优先的显式成本。可靠的方案 C 回填越完整，只读范围越小；没有可靠映射时不能为了便利恢复 ID fallback。

#### 方案 A：保留快照、不给恢复权限（默认）

- 无 UUID 的历史 comment/like 原样显示；
- 不根据当前 `From.Id` 重新归属；
- 不允许仅凭旧 id 编辑、删除或 unlike；
- 新数据逐渐替代旧数据，风险最低。

#### 方案 B：独立历史 alias（需要产品决策）

若必须恢复旧内容归属，建立独立命名空间：

```text
UserMap[current ID]             -> 当前路由所有者
HistoricalProfileAlias[old ID] -> 历史原始所有者
```

禁止把可覆盖 alias 混入普通 `UserMap`。否则旧 ID 被新用户注册后，历史 comment/like 会错误归给新用户。

需要另行决定：

- old ID 是否永久保留、禁止复用；
- `/feed/<old-id>` 是 404、重定向，还是指向新注册者；
- HistoricalProfileAlias 只用于展示归属，还是也可恢复 mutation 权限；
- alias 的创建、冲突、删除和迁移规则。

#### 方案 C：离线回填 UUID

只有在能可靠获得 `old ID -> original UUID` 映射时，才可离线扫描并回填 comment/like UUID：

- 必须 dry-run、计数和抽样；
- 只能操作 new DB；
- 不能按当前 UserMap 猜测已被复用的 ID；
- 大库全扫描成本高，不作为在线 rename 流程。

方案 C 是缓解严格 no-fallback 对全体存量数据影响的首选方向，但必须先证明历史映射来源可靠。成功回填的记录恢复正常 hydration 和 mutation；无法确认归属的记录继续按方案 A 保持只读。

## 测试计划

### model

- 新 like/comment 的 `From.Uuid` 和全部展示快照正确；
- rename 后 Like 不重复、DeleteLike 能删除；
- comment owner rename 后仍可编辑和删除；
- entry owner 和 super 可以删除他人 comment，但不能编辑；
- 他人编辑/删除仍返回权限错误；
- malformed/mismatched UUID 不 fallback 到同名 ID；
- comment 编辑保留 Id、Date、author，只更新允许修改的内容；
- legacy 无 UUID 默认不能获得 mutation 权限。

### server

- CommentRequest `user_uuid` 解析 canonical profile；
- 调用方伪造 `comment.From` 时，保存结果仍来自 canonical profile；
- hydration 刷新带 UUID 的 comment/like；
- legacy 无 UUID、profile 缺失、nil From 均保留快照且不报错；
- 若实现可选 resolver cache：同一 UUID 只读取一次 profile。

### httpd

- comment 提交携带当前 session UUID；
- comment 删除携带当前 session UUID；
- hydration 后 like 状态和 comment commands 正确；
- 未登录、UUID 缺失和伪造 From 不能绕过服务端权限。

### 门禁

- Go：`go build ./... && go vet ./... && go test ./...`；
- 前端（若展示或交互有改动）：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`；
- E2E：rename 后 like/unlike、comment edit/delete；
- 最后：`git diff --check`、`git status --short`。

## 不做的事

- 不改变 `pb.Feed` 的已有字段编号或持久化 key/schema；
- 不删除或改名已有 RPC/字段；只允许兼容新增 principal UUID 字段；
- 不把 `From.Id` 继续升级为身份或授权凭据；
- 不在 rename 请求内做全库扫描；
- 不把历史 alias 混入可复用的当前 `UserMap`；
- 不让单条无法解析的旧 comment/like 导致整个 feed 请求失败。

## 完成条件

- 所有新 comment/like 都持久化稳定 UUID；
- 所有 mutation 由 canonical principal UUID 授权；
- `DeleteComment` 越权路径已关闭；
- profile rename 后新数据的显示、Like/Unlike、Comment Edit/Delete 均正常；
- legacy 数据行为有明确、安全且经过测试的边界；
- 若 profiling 证明存在明显 N+1，再完成并验证 resolver cache；
- 全量门禁和 rename E2E 通过。
