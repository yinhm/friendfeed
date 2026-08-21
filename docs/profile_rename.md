# Profile rename 与稳定身份

本文记录 profile rename 完成后的运行时、持久化和运维契约。它是当前实现说明，不是实施 TODO。

## 身份与授权

- 用户 UUID 是唯一稳定身份。Profile ID、Name、Picture、Type 都是可变化的展示字段或写入时快照。
- 新 OAuth profile 的初始 ID 由系统生成（`ff-` + 8 个随机 `[a-z0-9]` 字符，满足 `ValidateProfileId`），provider 显示名只写入 `Name`；用户在 profile 页通过 rename 换成自己的 ID。`PutOAuth` 通过 gRPC response header `x-profile-newly-created` 标记本次新建的 profile（Profile 是持久化类型，不承载 transient 状态），httpd 据此把首次登录 redirect 到 `/account/profile?welcome=1`。
- `GetOrCreateProfileFromOAuthUser` 把"按 UUID 查 profile 是否存在"与创建写入放进同一个 `ApplyBatch`：同一账号并发首次登录只有一个调用创建成功（`created=true`），其余调用拿到胜者的已存 profile，不会为同一 UUID 铸出第二个游离 ID 别名。遇到 soft-deleted profile 必须返回 `ErrProfileDeleted`（与 `GetProfileFromUuid` 语义一致），已删除账号不能通过 OAuth 登录。`NewProfileFromOAuthUser` 保留原有 create/overwrite 语义并标记 `Deprecated`，新代码不得使用。
- `UpdateProfile` 在同一个 `ApplyBatch` 内完成保留 ID 检查、`UserMap` 冲突检查和两项写入（与 `RenameProfileId`、`StageCreateGroup` 串行化提交）：并发创建相同 ID 只有一方成功（另一方收到 `ErrProfileIdTaken`），提交失败不留孤儿 `UserMap` 映射。`ErrProfileIdTaken` 与 `ErrProfileIdReserved` 同属"ID 不可用"，生成 ID 分配遇到任一都会重试。
- Entry 作者使用 `ProfileUuid`；comment/like 的 `From.Uuid` 保存稳定 UUID。
- 新 comment/like 的 `From` 必须由 canonical Profile 生成，不能信任客户端提交的身份字段。
- like 去重与 unlike、comment edit/delete、UI commands 都按有效且非零的 UUID 判断，不得 fallback 到当前 `UserMap` 或相同 ID。
- comment 仅作者可编辑；作者、entry 作者或 super 可删除。group admin 的 comment moderation 尚未定义。
- 缺失、非法或零 UUID 的 legacy actor ref 保留展示快照且默认只读，避免 ID 回收后错误认领。
- 读路径按 UUID 刷新 Id、Name、Picture、Type；单次请求内缓存 profile lookup，不使用跨请求 profile cache。

`CommentRequest.user_uuid=3` 与 `CommentDeleteRequest.user_uuid=4` 是兼容新增字段。当前 gRPC server 仍信任内部调用方提供的 UUID；端口暴露给不可信客户端前必须增加服务端认证 principal。

## Rename 写入

`model.RenameProfileId` 在同一 Store 临界区和同一个 Pebble batch 内完成：

1. 验证 Profile、旧 `UserMap` 和新 ID；
2. 拒绝损坏映射、ID 冲突、零 UUID和重复 rename；
3. 删除 `UserMap[old_id]`；
4. 写入 `UserMap[new_id] -> UUID`；
5. 更新 `Profile[UUID].Id`；
6. 写入 `UserRenameMap[old_id] -> UUID`。

`store.ApplyBatch` 使用 Store 当前的同步写入选项。callback 失败或空 batch 不提交。`ApiServer.PostFeedinfo` 串行化 read/rename/patch，避免并发请求用旧 Profile 快照覆盖 rename。

## Soft redirect 与 ID 保留

持久化表契约：

```text
TableUserRenameMap = 7
key   = UTF-8 old_id
value = 16-byte stable user UUID
```

- `/feed/:old_id` 在普通 `UserMap` lookup 失败后直接查询 `UserRenameMap`，再按 UUID读取当前 Profile。
- Web 在私有 feed 权限检查后跳转到 `/feed/:current_id`。
- 使用临时 `302`，不用永久 `301`；rename metadata 回收后旧 ID 可以重新分配。
- 一条 rename 记录存在期间，该 UUID不能再次 rename，old ID 也不能被创建或其他 rename 占用。
- 当前 soft redirect 只覆盖 `/feed/:id`；entry permalink 和搜索不解析旧 ID。

## 运维与历史数据

查看 rename 记录：

```bash
./tools -to new_db -c inspect_user_rename_map
./tools -to new_db -c inspect_user_rename_map -id old_id
./tools -to new_db -c inspect_user_rename_map -max-limit 20
```

输出格式是 `old_id -> UUID -> current_id`。该命令只读打开数据库。

整表回收：

```bash
echo purge_user_rename_map | \
  ./tools -to new_db -c purge_user_rename_map
```

命令要求输入完整命令名确认。表中没有时间字段，因此当前只能整表回收；回收后相关旧链接不再跳转、旧 ID重新可用，原用户也可以再次 rename。执行前应停止使用该 Pebble 目录的服务并备份。

历史 Entry actor UUID 使用 `backfill_actor_uuids` 修复。它只依赖 new DB；执行方法、安全前提和 dry-run 规则见 [db_migration.md](db_migration.md)。

## 缓存与验证

- Profile 更新后只失效当前实例的 `profile:<uuid>` 与 `graph:<uuid>`。
- 其他用户嵌入的 Profile 快照允许按当前 5 分钟 TTL 自然刷新，不遍历全部用户 cache。
- 多实例 cache invalidation 尚未实现。
- rename E2E 使用独立身份，不通过二次 rename 恢复测试数据，并覆盖旧 feed ID 跳转、作者显示、like/unlike 与 comment edit。

