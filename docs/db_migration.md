# DB Migration (old_db → new_db)

> 本文档从 README 拆出，记录 `old_db` 到 `new_db` 的迁移命令。
> 这些工具属于 `v1.0.0` 基线（tag）；old_db 迁移与 Pebble v2 升级均已完成。
> 其中 `meta`、`sync_meta`、`public_feed`、`profile`、`count_meta` 命令及 `debug` 的 mdb 参数已在 master 退役删除，仅存在于 `v1.0.0` tag；master 保留 `db`、`sync`、`rebuild_timeline`、`rebuild_social_graph`、`migrate_media_urls`、`purge_profile`、`purge_oauth`、`debug`，以及诊断/修复命令 `inspect_profile`、`audit_profiles`、`fix_twitter_oauth_fields`（见下文）。
>
> `-from` 只对读取源库的命令（`db`、`sync`、无 `-table` 的 `debug`）为必填；其余命令仅操作 `-to` 目标库，无需 `-from`。

rebuild public feed

    ./tools -to new_db -c public_feed

rebuild social graph after db migrated

    ./tools -to new_db -c rebuild_social_graph

rebuild user timeline after social graph

for one user:

    ./tools -to new_db -c rebuild_timeline -user yinhm

for all users:

    ./tools -to new_db -c rebuild_timeline

migrate to R2

    ./tools -to new_db -c migrate_media_urls

migrate all to new db

```
./tools -from old_db -to new_db -c db
// ./tools -from old_db -to new_db -c meta # may not needed
./tools -from old_db -to new_db -c sync_meta

./tools -from old_db -to new_db -c public_feed
./tools -to new_db -c rebuild_social_graph
./tools -to new_db -c rebuild_timeline
./tools -to new_db -c migrate_media_urls


./tools -from old_db -to new_db -c profile
./tools -from old_db -to new_db -c debug

```


purge and rebuild meta if wrong oauth（`purge_*` 会整表删除，执行前需在提示中输入完整命令名确认；脚本化用 `echo purge_profile | ./tools ...` 管道喂入）:

```
./tools -to new_db -c purge_profile
./tools -to new_db -c purge_oauth
./tools -from old_db -to new_db -c sync_meta
```

# 诊断与修复命令

以下命令均只需 `-to` 目标库，不需要 `-from`。`inspect_profile`、`audit_profiles` 以只读方式打开数据库（`store.NewStoreReadOnly`），不会写盘、也不与其他进程争抢写锁，可在服务运行时安全执行。

## inspect_profile

追踪某个登录名的 `UserMap -> uuid -> Profile` 解析链路，用于排查 `/feed/:name` 为什么 404。

    ./tools -to new_db -c inspect_profile -id elonmusk

## audit_profiles

全库审计 feed 路由不变量（每个非删除 `Profile.Id` 是否能经 `UserMap` 解析回自身 uuid），并报告 OAuth twitter handle 与 feed id 的差异、以及把 handle 作为别名回填的可行性（是否与其他 profile 冲突）。

    ./tools -to new_db -c audit_profiles

背景：feed 路由只认 `Profile.Id`（FriendFeed 昵称），而 twitter 的 OAuth `NickName` 是 screen_name（handle），二者可以合法地不同。例如 `/feed/elon_musk`（handle）会 404，而 `/feed/elon_musk`（feed id）正常。

## fix_twitter_oauth_fields

对每一条 `provider == "twitter"` 的 OAuth 记录，交换 `Name` 与 `NickName`。迁移进来的旧记录字段顺序是 `Name=显示名、NickName=handle`，而当前登录实现（`httpd/src/auth.go`）期望 `Name=handle、NickName=显示名`，此命令把旧记录翻正。

    ./tools -to new_db -c fix_twitter_oauth_fields

注意：命令**不是幂等的** —— 每执行一次就翻转一次，重复执行会翻回去，只应运行一次。仅影响 twitter provider，其他 provider（如 google）不受影响。

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
