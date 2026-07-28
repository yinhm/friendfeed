# DB Migration (old_db → new_db)

> 本文档从 README 拆出，记录 `old_db` 到 `new_db` 的迁移命令。
> 这些工具属于 `v1.0.0` 基线（tag）；old_db 迁移与 Pebble v2 升级均已完成。
> 其中 `meta`、`sync_meta`、`public_feed`、`profile`、`count_meta` 命令及 `debug` 的 mdb 参数已在 master 退役删除，仅存在于 `v1.0.0` tag；master 保留 `db`、`sync`、`rebuild_timeline`、`rebuild_social_graph`、`migrate_media_urls`、`purge_profile`、`purge_oauth`、`debug`，以及诊断/修复命令 `inspect_profile`、`audit_profiles`、`fix_twitter_oauth_fields`、`backfill_actor_uuids`、`inspect_user_rename_map`、`purge_user_rename_map`（见下文）。
>
> `-from` 只对读取源库的命令（`db`、`sync`、无 `-table` 的 `debug`）为必填；其余命令仅操作 `-to` 目标库，无需 `-from`。

当前版本可在 new DB 上重建社交图：

    ./tools -to new_db -c rebuild_social_graph

社交图完成后重建用户 timeline。

单个用户：

    ./tools -to new_db -c rebuild_timeline -user yinhm

全部用户：

    ./tools -to new_db -c rebuild_timeline

迁移媒体 URL 到 R2：

    ./tools -to new_db -c migrate_media_urls

## v1.0.0 old DB 迁移记录

`meta`、`sync_meta`、`public_feed`、`profile`、`count_meta` 仅存在于 `v1.0.0` tag。需要处理尚未迁移的 old DB 时，必须先使用 v1.0.0 工具完成迁移，再使用当前版本打开 new DB；不要在 master 上寻找或重新实现这些命令。

当前仍保留 `db` 与 `sync` 供内部数据复制使用：

```bash
./tools -from old_db -to new_db -c db
./tools -from old_db -to new_db -c sync
```

整表清理命令执行前会要求输入完整命令名确认；脚本化可通过管道输入：

```bash
./tools -to new_db -c purge_profile
./tools -to new_db -c purge_oauth
./tools -to new_db -c purge_user_rename_map
```

# 诊断与修复命令

以下命令均只需 `-to` 目标库，不需要 `-from`。`inspect_profile`、`audit_profiles` 以只读方式打开数据库（`store.NewStoreReadOnly`），不会写盘，但 Pebble 仍要求取得数据库锁；需停止使用该目录的服务，或改为检查一致性备份副本。

## inspect_profile

追踪某个登录名的 `UserMap -> uuid -> Profile` 解析链路，用于排查 `/feed/:name` 为什么 404。

    ./tools -to new_db -c inspect_profile -id elonmusk

## inspect_user_rename_map / purge_user_rename_map

查看 soft rename 的 `old_id -> UUID -> current_id` 映射：

```bash
./tools -to new_db -c inspect_user_rename_map
./tools -to new_db -c inspect_user_rename_map -id old_id
./tools -to new_db -c inspect_user_rename_map -max-limit 20
```

整表回收 rename metadata：

```bash
echo purge_user_rename_map | \
  ./tools -to new_db -c purge_user_rename_map
```

inspect 只读；purge 会释放全部保留的旧 ID，并允许相关用户再次 rename，因此必须停服、备份并输入完整命令名确认。详细契约见 [profile_rename.md](profile_rename.md)。

## audit_profiles

全库审计 feed 路由不变量（每个非删除 `Profile.Id` 是否能经 `UserMap` 解析回自身 uuid），并报告 OAuth twitter handle 与 feed id 的差异、以及把 handle 作为别名回填的可行性（是否与其他 profile 冲突）。

    ./tools -to new_db -c audit_profiles

背景：feed 路由只认 `Profile.Id`（FriendFeed 昵称），而 twitter 的 OAuth `NickName` 是 screen_name（handle），二者可以合法地不同。例如 `/feed/elon_musk`（handle）会 404，而 `/feed/elon_musk`（feed id）正常。

## fix_twitter_oauth_fields

对每一条 `provider == "twitter"` 的 OAuth 记录，交换 `Name` 与 `NickName`。迁移进来的旧记录字段顺序是 `Name=显示名、NickName=handle`，而当前登录实现（`httpd/src/auth.go`）期望 `Name=handle、NickName=显示名`，此命令把旧记录翻正。

    ./tools -to new_db -c fix_twitter_oauth_fields

注意：命令**不是幂等的** —— 每执行一次就翻转一次，重复执行会翻回去，只应运行一次。仅影响 twitter provider，其他 provider（如 google）不受影响。

## backfill_actor_uuids

为当前 new DB 的历史 Entry 回填稳定身份：

- `entry.From.Uuid` 与 `entry.ProfileUuid`；
- `comment.From.Uuid`；
- `like.From.Uuid`。

本命令依赖一次性迁移前提：目标 dev/production 数据从导入至今没有发生 Profile ID 修改，因此历史 `From.Id` 仍能通过当前 `UserMap -> Profile` 证明原 owner。命令仍会完整校验映射链；缺失、损坏或与已有 UUID 冲突的记录只计数，不会猜测或覆盖。

使用 `-dry-run` 时仅报告，并以只读方式打开目标 DB，但仍需取得 Pebble 数据库锁。请停止使用该目录的服务，或对一致性备份副本执行：

```bash
./tools -to new_db -c backfill_actor_uuids -dry-run
./tools -to new_db -c backfill_actor_uuids -user yinhm -max-limit 20 -dry-run
```

确认 dry-run 的 `unresolved`/`conflicts` 后，停止所有使用该 Pebble 目录的服务、完成备份，再执行写入：

```bash
./tools -to new_db -c backfill_actor_uuids
```

迁移不依赖 old DB，不修改 ID/Name/Picture 等展示快照，不修改 `FeedUuid`；可重复执行，第二次应报告零 changed。dry-run 的安全边界是核心迁移函数在所有 mutation API 之前返回；末尾是否调用 Pebble `Flush` 不决定数据是否已经写入。

# Pebble v2 / FMV 升级（2026-07，dev 与 production 已完成）

代码基线：`v1.0.0` 是最后一个 pebble v1 / FMV 1 兼容版本（tag）；`deps: switch to pebble/v2 v2.1.6` 起为 v2 代码。

前提：全部 `old_db -> new_db` 迁移已完成。运行时只需升级 new_db 主目录（`mdb = rdb`，meta 子目录属 old_db 时代残留，不升级）。

升级步骤（每个 Pebble 数据目录，顺序不可颠倒）：

1. 停写：停掉指向数据目录的 `ffdb`/`httpd`，确认无人持有 `<db-dir>/LOCK`。
2. 备份：`cp -a <db-dir> <db-dir>.bak-fmv1`（FMV 升级不可逆，唯一恢复手段）。
3. 格式升级（有交互确认，脚本化必须管道喂 `Y`；FMV 14 为 blocking 迁移，等自然退出，中断只能回备份重来）：

```bash
echo Y | go run github.com/cockroachdb/pebble/cmd/pebble@v1.1.5 db upgrade <db-dir>
```

4. 核对 FMV（预期 16）：只读探针打开并打印 `db.FormatMajorVersion()`。
5. v1 基线二进制只读冒烟（证明格式无损），再用 v2 二进制冷启动验证读写与重启（证明代码无损）。两步顺序不能反。
6. 全量门禁 `go build ./... && go vet ./... && go test ./...`；记录耗时、磁盘变化、最终 FMV。

注意：v2 二进制无法打开 FMV < 13 的数据库；无自定义 Comparer/Merger/BlockPropertyCollector，官方 `db upgrade` 工具适用。
