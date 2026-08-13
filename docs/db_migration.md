# DB Migration (old_db → new_db)

数据库表、key 编码、Flake 与 EntryIndex 的设计背景见
[database_design.md](database_design.md)。

> 本文档从 README 拆出，记录 `old_db` 到 `new_db` 的迁移命令。
> 这些工具属于 `v1.0.0` 基线（tag）；old_db 迁移与 Pebble v2 升级均已完成。
> 其中 `meta`、`sync_meta`、`public_feed`、`profile`、`count_meta` 命令及 `debug` 的 mdb 参数已在 master 退役删除，仅存在于 `v1.0.0` tag；master 保留 `db`、`sync`、`rebuild_timeline`、`rebuild_social_graph`、`migrate_media_urls`、`purge_profile`、`purge_oauth`、`debug`，以及诊断/修复命令 `inspect_profile`、`audit_profiles`、`fix_twitter_oauth_fields`、`backfill_actor_uuids`、`migrate_entry_keys`、`inspect_user_rename_map`、`purge_user_rename_map`、`rebuild_search_index`（见下文）。
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

## EntryIndex 44 B 格式迁移

当前 EntryIndex 为 `T | owner UUID | reverse Unix ms(8) | entry UUID(16)`（44 B，value
为空）。含旧格式（56 B reverse Flake + canonical key 后缀，或更早的 36 B 无后缀）的库
升级后执行：

    ./tools -to new_db -c migrate_entry_index -dry-run        # 演练，不写盘
    ./tools -to new_db -c migrate_entry_index                 # 可用 -max-limit 先小范围验证

迁移按键直接转换：Entry.Date 为整秒，reverse Unix ms 可由 reverse Flake 精确还原，无需
读取 Entry；重复的 legacy 行会折叠到同一新 key。也可以用 `rebuild_entry_index` 从 Entry
源数据清空重建。应按 schema 升级惯例停服执行：旧代码写入的 56/36 B 行在新代码的读取
路径上会被判为非法 key，混跑期间 feed 读取会报错；旧格式的分页 cursor 在升级后失效
（cursor 为不透明短期令牌，重新翻页即可）。

## Home Timeline 有界缓存迁移

TimelineIndex/Position 现在只维护 TimelineState 有效的活跃 viewer，每次维护收敛到最多
10,000 条；内容时间窗口当前为 MAX，不按日期裁剪。升级顺序：

```text
1. 停止 ffdb/ffweb，完成 Entry/EntryIndex/interaction 必要迁移
2. 部署新二进制并恢复服务
3. 让真实用户访问 Home，按需建立 TimelineState
4. 对重点用户验证：./tools -to new_db -c rebuild_timeline -user yinhm -max-limit 20 -dry-run
5. 正式重建重点用户：./tools -to new_db -c rebuild_timeline -user yinhm
6. 评估清理：./tools -to new_db -c compact_timelines -dry-run
7. 执行清理：./tools -to new_db -c compact_timelines
8. ./tools -to new_db -c audit_store
```

`rebuild_timeline` 从 Follow、direct EntryIndex、Entry、Like、Comment 重新选择最多 10,000 个
publish 候选并计算 activity；`-user` 成功后创建/刷新 State，默认只重建 State 仍有效的用户。
候选外的长尾历史互动不保证恢复。

`compact_timelines` 不读取 canonical 数据、不重算 activity：有效 State 仅裁剪现有排序，过期
或无 State 的 viewer 删除全部 108/109 行。必须先 dry-run；执行可安全重跑。回滚旧二进制会
恢复全 follower fanout，但新版本期间跳过的 inactive 派生行不会自动出现，应按用户重建，
不要默认恢复旧式全库 timeline。

生产执行时保留一份简短验收记录：执行日期与环境、compact dry-run/正式执行删除的 viewer
和 108/109 行数、audit_store 摘要、重点用户首次 Home 重建耗时及随后一次热读耗时。范围删除
先产生逻辑 tombstone，磁盘占用不保证立即下降；如需记录空间收益，应等待 Pebble 后台压缩后
再测量，不把命令结束时的目录大小当作验收条件。

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

## EntryIndex 与互动表离线升级

当前版本把 EntryIndex 升级为同秒不碰撞的 key，并把 Like/Comment 从 Entry value
拆到独立表。升级不维护旧格式双轨，完成后不得用旧程序打开或降级写入数据库。

大库命令必须保持流式、内存有界。`rebuild_entry_index` 会先流式预检 Entry，再用
Pebble range tombstone 清理 EntryIndex，最后第二遍流式重建；dry-run 也不保留全量
Entry 或 index。不要把“每 500 条提交一次”误当成内存上限：如果提交前已经把全部
key/record 收进 slice/map，内存仍会随全库数据线性增长。需要先验证后写入时，应接受
多遍扫描，而不是缓存全库中间结果。

推荐依次使用全量 `rebuild_entry_index` 和 `rebuild_timeline`。前者从 Entry 重建
Profile/target feed direct index，并清除 orphan/旧 EntryIndex timeline；后者从 Entry、
当前 Like/Comment 与当前 Follow 关系重建 Home activity timeline。
`migrate_entry_index` 只转换现存旧 key，不能恢复已经丢失的索引；执行全量 rebuild
时不需要先运行它。

按以下顺序操作，每个数据库目录单独完成：

1. 停止所有使用该 Pebble 目录的进程并确认释放 `LOCK`；保存升级前二进制。
   新二进制已经读取 TimelineIndex，必须保持服务停止直到步骤 6 的 `rebuild_timeline`
   完成；提前启动会让 Home 暂时为空或只包含升级后的新帖。
2. 使用 `cp -a <db-dir> <db-dir>.bak-entry-index` 创建离线备份，并在副本上确认
   `audit_store` 能打开。备份只用于升级验收失败时整体恢复，不能与已升级库混合。
3. 若历史 actor UUID 尚未回填，先按上一节完成 `backfill_actor_uuids`；互动迁移不按
   可回收的 `From.Id` 猜测身份。
4. 先修复历史字符串 Entry key。该命令只认已知的
   `TableEntry + 36-character UUID` 污染格式，并核对 key UUID、`Entry.Id` 及已有
   canonical row；冲突会在任何写入前报错：

```bash
./tools -to <db-dir> -c migrate_entry_keys -dry-run
./tools -to <db-dir> -c migrate_entry_keys
```

5. 再对一个用户、小上限执行其余 dry-run，最后执行全库 dry-run：

```bash
./tools -to <db-dir> -c migrate_interactions -user yinhm -max-limit 20 -dry-run
./tools -to <db-dir> -c rebuild_entry_index -user yinhm -max-limit 20 -dry-run
./tools -to <db-dir> -c rebuild_timeline -user yinhm -max-limit 20 -dry-run
./tools -to <db-dir> -c migrate_interactions -dry-run
./tools -to <db-dir> -c rebuild_entry_index -dry-run
./tools -to <db-dir> -c rebuild_timeline -dry-run
```

`migrate_interactions` 全量 apply 前会再次完整预检。已经过
`backfill_actor_uuids` 仍无法可靠映射的 archive actor 会保留为只读展示快照：
迁移只为独立表生成确定性的内部 row UUID，不填写 `From.Uuid`，因此不会按当前同名
profile 授予编辑、删除或 unlike 权限。非 UUID 的历史 comment ID 同样会确定性转换为
UUID。日志中的 `legacy_actors`、`generated_comment_ids` 是兼容计数，不阻断迁移；只有
会覆盖同一独立表 key 的 `duplicates` 非零时拒绝开始写入。
历史 Like/Comment 缺少或带有非法 `Date` 时仍保留展示，但 `rebuild_timeline` 会计数并
跳过该互动的 activity bump；不得用当前时间或 Entry 发布时间伪造互动时间。Entry
自身的发布时间无效仍会阻断重建，因为无法建立基础排序位置。

全量 dry-run 和 apply 都应观察进程 RSS；内存应主要由 Pebble cache 和少量按 feed 的
校验状态构成，不应随已扫描 Entry 数持续线性增长。`rebuild_timeline` 按 profile 逐个
处理，内存上限与单个用户的 Home timeline 规模相关，而非全库 Entry 总数。

6. 确认 dry-run 后执行写入。命令默认会写库，`-dry-run` 才是不写开关：

```bash
./tools -to <db-dir> -c migrate_interactions
./tools -to <db-dir> -c rebuild_entry_index
./tools -to <db-dir> -c rebuild_timeline
```

7. 执行 `./tools -to <db-dir> -c audit_store`。必须确认 `noncanonical_entries=0`、
   `entry_key_id_mismatches=0`、`noncanonical_indexes=0`、`missing_direct=0`、
   `orphan_indexes=0`，且 timeline 的 missing/duplicate/timestamp mismatch 均为 0；抽查目标用户的 cursor 多页顺序、首尾、
   无重复项，以及 Like/Comment 数量、顺序、编辑/删除权限和 rename 后身份。
8. 用新二进制冷启动，验证 feed 与互动后正常停止并再次启动。若验收失败，应在没有
   新写入的前提下整体恢复步骤 2 的目录并中止升级；不要让旧程序打开已升级目录。

## rebuild_search_index

把所有带有 OAuth 登录信息的用户（含按登录名绑定的旧 OAuth 行）的历史 entry 收录进 bleve 搜索索引；`-user` 可指定单个登录名并绕过 OAuth 检查。扫描 author 索引（`EntryIndex | profile UUID`），每条 entry 只收录一次；entry 记录缺失或 `Body` 为空的行只计数（对齐 `PutEntry` 只索引非空 Body 的行为）。索引路径默认 `<to>/index`，与服务端布局一致，可用 `-index-path` 覆盖。本命令以只读方式打开 DB、不写 DB，只写搜索索引；可重复执行（bleve 按 entry ID upsert）。

```bash
./tools -to new_db -c rebuild_search_index -dry-run
./tools -to new_db -c rebuild_search_index -user yinhm -dry-run
./tools -to new_db -c rebuild_search_index
```

注意：只读打开仍需取得 Pebble 数据库锁，且 bleve 索引不允许多进程同时写。执行前停止使用该目录的 `ffdb`/`httpd`，或对一致性备份副本执行。
写入索引时必须使用 `ffdb.service` 的 `User=` 身份，不能以 root 或其他部署用户执行；Bleve segment 是 owner-only 文件，错误用户生成的索引会导致 ffdb 启动时报 `permission denied`。例如：

```bash
sudo -u <ffdb-user> ./tools -to new_db -c rebuild_search_index
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
