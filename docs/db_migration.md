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
