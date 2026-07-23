# DB Migration (old_db → new_db)

> 本文档从 README 拆出，记录 `old_db` 到 `new_db` 的迁移命令。
> 这些工具属于 `v1.0` 基线；按 `PEBBLE_TODO.md`，必须在升级 Pebble v2 之前用它们完成全部迁移。

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


purge and rebuild meta if wrong oauth:

```
./tools -from old_db -to new_db -c purge_profile
./tools -from old_db -to new_db -c purge_oauth
./tools -from old_db -to new_db -c sync_meta
```

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
