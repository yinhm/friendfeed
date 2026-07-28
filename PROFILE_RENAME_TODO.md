# Profile Rename 后续 TODO

当前 UUID 身份、comment/like 授权、read-path hydration 和 rename E2E 已完成。本文件只记录后续改进，不重新打开已经验证通过的身份契约。

## 不可改变的原则

- UUID 是唯一稳定身份；`Id`、Name、Picture、Type 只是展示快照。
- UUID 缺失、非法或为零时安全失败，不得 fallback 到当前 `UserMap`。
- 不修改既有 protobuf 字段编号、持久化 key/schema 或公开方法签名。
- 不在在线 rename 请求中扫描全库。
- 功能修复、存储重构、UI调整和迁移工具分开提交。
- 每项先补行为测试；完成后执行全量门禁。

## 1. Rename 原子性与并发安全（已完成，2026-07-28）

实施结果：

- `store.ApplyBatch` 统一创建、关闭并原子提交 Pebble batch，沿用 `SetSync` 写入模式；callback 返回错误或空 batch 时不提交。
- `RenameProfileId` 在同一临界区内读取 Profile、验证旧映射、检查新 ID冲突并一次提交旧映射删除、新映射写入和 Profile 更新。
- 零 UUID、旧映射缺失/损坏/指向他人、新映射损坏和底层读取错误均安全失败且不修改数据。
- `ApiServer.PostFeedinfo` 串行化完整的 read/rename/patch 流程，避免 atomic rename 后被并发请求的旧 profile 快照覆盖。
- store/model/server 的原子失败、损坏映射和并发抢占测试已覆盖；定向 race 测试及 Go 全量门禁通过。

### 当前问题

`model.RenameProfileId` 依次删除旧 `UserMap`、写入新 `UserMap`、更新 Profile，只能在普通错误路径尽力回滚：

- 进程在中间崩溃会留下不一致状态；
- 回滚本身失败时无法恢复；
- collision lookup 的非 NotFound 存储错误不能被当作“ID 可用”；
- 两个并发 rename 在检查与写入之间存在竞争窗口；
- “atomically”注释与实际保证不符。

### 目标方案

- 先写 characterization test，锁定成功、no-op、ID冲突、格式校验和旧映射删除行为。
- 为 store 增加最小的内部 batch 能力；不要改变已有 `Store`/`Table` 方法契约。
- 在同一个 Pebble batch 中完成：
  1. 删除旧 `UserMap`；
  2. 写入新 `UserMap`；
  3. 更新 Profile。
- rename 的 collision check 与 batch commit 必须处于同一个进程内临界区。
- 新 ID lookup：
  - 明确 NotFound：允许继续；
  - 指向自身：允许幂等继续；
  - 指向其他 UUID：返回冲突；
  - 其他读取/解码错误：原样返回，不得继续写。
- 所有错误保留底层 cause，便于诊断。

### 测试

- 成功 rename 后 Profile、新映射、旧映射三者一致；
- 任一步构造失败时数据库保持旧状态；
- 两个 profile 并发抢同一 ID，只能一个成功；
- collision lookup 的非 NotFound 错误不会执行写入；
- no-op 不产生无意义写；
- malformed/zero profile UUID安全失败。

## 2. UI commands 与 UUID授权统一

### 当前问题

`pb/helper.go` 的 `RebuildCommand`/`RebuildCommentsCommand` 仍按 ID判断 owner、like/unlike 和 comment commands。后端已 UUID-only，因此 legacy 或 ID回收场景可能显示最终会被后端拒绝的按钮。

### 目标行为

- 保留现有方法签名和 `RebuildCommentsCommand` 的 graph 参数。
- entry owner 只按有效 `entry.ProfileUuid` 判断。
- like/unlike 只按有效 `like.From.Uuid` 判断。
- comment edit 只对有效 `comment.From.Uuid` 作者显示。
- comment delete 对评论作者、entry 作者和 super 显示。
- UUID缺失、非法或为零时不显示 mutation command。
- group admin moderation 不在本项顺带加入。
- UI commands 只是提示；后端授权仍是最终边界。

### 测试

- rename 后 owner/edit/unlike commands 保持正确；
- 同 ID、不同 UUID不获得 commands；
- UUID-less/malformed/zero legacy refs 不获得 edit/delete/unlike；
- entry 作者和 super 可看到 comment delete，但不能看到他人 comment edit；
- nil entry/comment/like refs 不 panic。

## 3. Rename 后缓存失效

### 当前问题

当前只删除本 httpd 实例中的：

```text
profile:<当前 UUID>
graph:<当前 UUID>
```

其他用户的 graph cache 可能仍包含旧 profile 快照；多 httpd 实例也不会同步失效。

### 分阶段方案

1. 先补测试证明 rename 后本地相关缓存不会返回旧 ID/Name。
2. 单实例短期方案：rename 很少发生，可在成功后清空 profile/graph cache namespace；若 cache 不支持 namespace，评估清空整个本地 cache。
3. 多实例长期方案：使用 profile revision、失效事件或共享 cache；没有多实例部署需求前不提前建设。

不得为缓存方便重新按 ID认领历史数据。

## 4. 请求级 hydration resolver cache（需要 profiling）

先记录一次 feed 请求的 profile lookup 总数、唯一 UUID数和耗时。只有真实数据证明重复 lookup 明显时实施：

```text
map[uuid.UUID]profile lookup result
```

- cache 生命周期仅限单次请求；
- 同时缓存 NotFound；
- malformed/zero UUID不查询；
- 不做跨请求 profile cache，避免新增 rename 失效问题；
- 测试同一 UUID每个请求最多读取一次。

## 5. Legacy UUID回填工具（需要可靠来源）

默认继续采用方案 A：UUID-less comment/like 保留快照且只读。

只有存在可证明归属的数据来源时才设计离线回填：

- 历史导出中的稳定 UUID；
- provider immutable user ID 与已验证映射；
- 独立历史 alias；
- 其他可审计 provenance。

禁止使用当前 `UserMap[From.Id]` 猜测归属。

工具要求：

- 默认 dry-run；
- 可限制用户、entry 数量；
- 输出 scanned/eligible/updated/skipped/conflict 统计；
- 每类回填来源有测试；
- 修改前备份，支持重复运行且结果幂等；
- 不依赖 old DB，除非另行确认数据来源与迁移边界。

## 6. 需要产品/架构决策的延期项

### 旧 ID策略

明确 rename 后旧 ID是：

- 立即释放并允许复用；
- 冷却期内保留；
- 永久保留；
- 或重定向到新 ID。

若需要历史链接，使用独立命名空间：

```text
CurrentUserMap[id]      -> 当前路由所有者
HistoricalAlias[oldId] -> 原 profile UUID / redirect target
```

不得把历史 alias 混入普通 `UserMap`。

### gRPC principal

当前 `user_uuid` 是可信内部调用方提交的过渡 principal。gRPC 对不可信客户端开放前，必须从认证 middleware/context 获取 UUID，不再信任请求字段。

### Group admin moderation

另行定义 group、cross-post、graph 缺失和缓存过期时的授权语义，不在 UI command 重构中顺带扩张。

## 推荐执行顺序

1. Rename 原子性与并发安全；
2. UI commands UUID化；
3. 单实例缓存失效完善；
4. 根据 profiling 决定 resolver cache；
5. 根据产品决策决定旧 ID与 legacy 回填；
6. 仅在部署边界变化时做 gRPC principal 和多实例失效机制。

## 每项门禁

- Go：`go build ./... && go vet ./... && go test ./...`
- 前端涉及 UI时：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`
- rename/commands 交互变化：`pnpm run test:e2e`
- 最后：`git diff --check`、`git status --short`
