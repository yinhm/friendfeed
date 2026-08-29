# DB Migration (old_db → new_db)

数据库表、key 编码、Flake 与 EntryIndex 的设计背景见
[database_design.md](database_design.md)。

> 本文档从 README 拆出，记录 `old_db` 到 `new_db` 的历史迁移，以及当前 new DB 的诊断、
> rebuild 和一次性修复命令。`v1.0.0` 是 old DB/Pebble v1 的最后迁移工具基线；2.0 只运行
> Pebble v2 new DB，不兼容旧库或降级。
> `meta`、`sync_meta`、`public_feed`、`profile`、`count_meta` 及 `debug` 的 mdb 参数仅存在于
> `v1.0.0` tag，不在 master 重新实现。当前命令及顺序以本文后续章节和工具实际 `-c` 分支为准，
> 不在此维护一份容易过期的穷举列表。
>
> master 中的 `db`、`sync` 已退役并在开库前 fail-loud；必须 checkout `v1.0.0` 且只对
> 离线副本执行 old-DB copy。`-from` 目前只供无 `-table` 的历史 `debug` 使用。

## v2.2 application schema 盖章

Pebble FMV 只表示底层文件格式。ffdb 另用
`TableMeta | "db-schema/version" -> uint32 big-endian` 记录 application schema，当前为 `1`。
v2.2 是最后迁移窗口：非空 missing/older 数据库启动时告警但继续服务；future 或损坏 marker
直接拒绝。空数据库首次 ffdb 启动会初始化 current marker。

`inspect_schema` 只读取 marker 与 Pebble FMV，是 O(1) 状态检查。`verify_schema` 才执行完整
流式验证。在一致性副本执行：

```bash
./tools -to <db-copy> -c inspect_schema
./tools -to <db-copy> -c verify_schema
```

`verify_schema` 已包含 store audit，不要求先重复执行 `audit_store`；需要单独保存完整 audit
统计时才额外运行。验证在已有 audit 扫描之外，把互动与媒体残留合并为一次 Entry 扫描、把媒体与
默认头像合并为一次 Profile 扫描，并独立检查历史 Group 作者和 retired public cache。
blocker 只表示当前持久化编码或一次性迁移尚未完成：noncanonical Entry/EntryIndex、Entry key/ID
不一致、仍嵌入 Entry 的互动、可解析且仍可修复的 Group 作者、旧媒体 URL、旧默认头像和 retired
public cache。blocker 非零时命令失败，并逐项打印建议命令。

timeline/graph/Group admin/Group index/Notification/Task 等一致性差异是运行时可能再次产生的
drift，只输出非阻塞 warning；它们由 audit、懒清理、rebuild 或人工维护单独收敛，不能用来定义
application schema。无法解析本地 Profile 的历史 Group 作者同样只是 warning。archive 中无法映射
本地 Profile 的历史 actor、Feedinfo/UserMap、legacy rawBody/HTML/blockquote 和保留表号不是 blocker。

Twitter OAuth 的历史 Name/NickName 顺序无法仅凭记录可靠判定；运行
`fix_twitter_oauth_fields` 的历史证据和抽查结果必须由操作者单独保存，工具不得用启发式结果冒充证明。

修复全部 blocker 后，停服、备份，再执行：

```bash
echo stamp_schema | ./tools -to <db> -c stamp_schema
./tools -to <db> -c inspect_schema
```

apply 在同一进程重新验证后写入，不提供 force。`stamp_schema -dry-run` 也会完整验证但不写
marker，只在需要额外演练时使用；刚完成 `verify_schema` 后不必把它列为第二次强制扫描。写入后不需要手动
`Flush()` 才生效。dev 与 production 都必须保存 verify/inspect 输出；若另跑 audit，也一并保存。
v2.3 只有在二者盖章并
运行一个发布周期后，才强制拒绝 missing/older 数据库并删除一次性迁移代码。

无行为兼容 flag 已进入一个版本的弃用期：`cli --debug` 与显式
`mirror_twimg --no-wayback` 会输出 warning，计划在 v2.3 删除。Wayback 默认关闭，只有
`mirror_twimg --wayback` 会启用。

当前版本可在 new DB 上重建社交图：

    ./tools -to new_db -c rebuild_social_graph

首次部署 Group 活跃度排序后，先验证一个用户，再全量重建：

    ./tools -to /srv/ffdb/db -c rebuild_group_activity -user yinhm -dry-run
    ./tools -to /srv/ffdb/db -c rebuild_group_activity -user yinhm
    ./tools -to /srv/ffdb/db -c rebuild_group_activity -dry-run
    ./tools -to /srv/ffdb/db -c rebuild_group_activity

该命令只依赖当前库的 membership、Entry 与独立 Like/Comment 索引，不需要 old DB。
默认模式只重建持有 OAuth 登录身份的活跃用户（去重后逐个 Profile 处理，每次只保留
一个用户的 Group score，内存有界）；无 Group 成员关系的用户直接跳过，不写空排行。

Feed 年度统计同样先按用户验证，再全量重建；它逐个处理全部未删除 user/group Profile：

    ./tools -to /srv/ffdb/db -c rebuild_feed_archive -user yinhm -dry-run
    ./tools -to /srv/ffdb/db -c rebuild_feed_archive -user yinhm
    ./tools -to /srv/ffdb/db -c rebuild_feed_archive -dry-run
    ./tools -to /srv/ffdb/db -c rebuild_feed_archive

把 Profile 上残留的 FriendFeed 旧默认头像（`friendfeed.com/static/images/group-large.png`，
任意 scheme 与 `?v=` 变体）改写为本地默认图 `/static/images/ff-default.jpg`（先
-dry-run 看计数再实跑，可重复执行，`-user` 可限定单个 profile ID 或 UUID）：

    ./tools -to /srv/ffdb/db -c fix_default_picture -dry-run
    ./tools -to /srv/ffdb/db -c fix_default_picture

社交图完成后重建用户 timeline。

单个用户：

    ./tools -to new_db -c rebuild_timeline -user yinhm

全部用户：

    ./tools -to new_db -c rebuild_timeline

迁移媒体 URL 到 R2（先 -dry-run 看计数再实跑，可重复执行）：

    ./tools -to new_db -c migrate_media_urls -dry-run
    ./tools -to new_db -c migrate_media_urls

覆盖 `Profile.Picture`、`Entry.Thumbnails[].Url` 与 `Entry.Thumbnails[].Link`（模板
渲染媒体块的 `<a href>`），以及 `Entry.Body`/`RawBody` 内嵌的媒体 URL（`<img src>`
与媒体链接均处理，外部链接不动）。已退役 host 一律改写为
`https://m.friendfeed.me` 且对象路径不变（serving 契约见 conf/nginx_media.conf）：
`storage.googleapis.com/lastff01/*`、`m.friendfeed-media.com`、`i.friendfeed.com`、
`friendfeed-media.com`；`http://m.friendfeed.me` 会被升级为 https。`Entry.Files`
暂不迁移。命令只改 DB、不搬运对象；执行前应确认媒体目录中对象实际存在。

### 抢救 Twitter 历史媒体

`mirror_twimg` 只读扫描 Entry，抢救 `p.twimg.com` 与 `pbs.twimg.com` 媒体并写入本地和
R2；它不修改数据库。已失效且无法确定映射的 `a0.twimg.com` 在扫描阶段直接排除，不发起
DNS、HTTP 或 Wayback 请求，也不占用 `-max-limit`。旧 `p.twimg.com/<basename>` 会先尝试
`https://pbs.twimg.com/media/<basename>` 及其 `format/name=orig` 形式。

先在离线数据库副本上运行小批量：

```bash
./tools -to <offline-db> -c mirror_twimg -config <config.json> \
  -out <twimg-sync.jsonl> -max-limit 100 -dry-run
./tools -to <offline-db> -c mirror_twimg -config <config.json> \
  -out <twimg-sync.jsonl> -max-limit 100
```

默认只启用 2 个 live-fetch worker，所有 worker 共享每秒最多一次的请求门控；网络错误、
HTTP 5xx 和 429 最多重试 3 次，按 5、10、20 秒退避，429 的全局冷却不少于 60 秒。
403/404 和 DNS NXDOMAIN 不重试。Wayback 默认关闭；只有明确传入 `-wayback` 才会启用，
不应作为全量 mirror 的默认来源。启用后它单线程执行，不同 URL 之间默认等待 2 秒；
其 429 和非标准 498 响应均视为限流，优先遵守 `Retry-After`，否则至少等待 60 秒并指数退避。
这些参数可通过 `-workers`、`-request-delay`、`-retries`、`-backoff-base` 和
`-wayback-delay` 调低速率；批量抢救时不应提高默认请求速率。

结果文件采用 append-only JSONL，每个已处理 URL 一行。成功记录包含原 URL、候选来源、
最终 `m.friendfeed.me` URL、R2 object key、内容 SHA-256、字节数、MIME、完整引用计数和
最多 100 个 Entry ID 样本；失败记录包含 HTTP 状态与错误摘要。每行写入后都会 flush 并
`fsync`，写盘失败会使命令失败。启动时先读取整个结果文件：格式完整的成功记录和 `dead`
记录不会再次排队、抓取或追加记录，只有 `error` 会重试；同一次扫描也按原 URL 去重。
R2 key 是内容 SHA-256，因此异常中断发生在上传成功但结果落盘之前时，极少量必要重试仍是幂等的。

该 JSONL 是后续生产数据库 URL 改写的唯一输入。改写命令应重新流式扫描生产 Entry，并按
`url -> new_url` 替换所有 Body/RawBody/Thumbnail/File 引用，不依赖样本 `refs`，且必须另行
提供 dry-run、备份和逐 Entry 原子更新。在该改写命令完成并验证前，不要直接修改生产 DB。

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

TimelineIndex/Position 对 TimelineState 有效的活跃 viewer 最多维护 10,000 条；inactive viewer
保留最近 500 条冷缓存，内容时间窗口当前为 MAX，不按日期裁剪。升级顺序：

```text
1. 停止 ffdb/ffweb，完成 Entry/EntryIndex/interaction 必要迁移
2. 对重点用户验证：./tools -to new_db -c rebuild_timeline -user yinhm -max-limit 20 -dry-run
3. 全量演练：./tools -to new_db -c rebuild_timeline -dry-run
4. 预热全部 Profile + OAuth 用户：./tools -to new_db -c rebuild_timeline
5. 评估清理：./tools -to new_db -c compact_timelines -dry-run
6. 执行清理：./tools -to new_db -c compact_timelines
7. ./tools -to new_db -c audit_store
8. 部署新二进制并恢复服务，验证重点用户首次 Home 请求直接命中预热缓存
```

`rebuild_timeline` 从 Follow、direct EntryIndex、Entry、Like、Comment 重新选择最多 10,000 个
publish 候选并计算 activity；`-user` 成功后创建/刷新 State，默认预热同时具有 Profile 与 OAuth
身份的用户并为其创建/刷新 State。候选外的长尾历史互动不保证恢复。全量预热会把这些用户暂时
视为活跃，因此只在部署或明确需要重建全部真实用户时运行，不作为周期任务。

`compact_timelines` 不读取 canonical 数据、不重算 activity：有效 State 保留现有排序前 10,000
条，过期或无 State 的 viewer 保留前 500 条冷缓存，均成对裁剪 108/109。必须先 dry-run；执行
可安全重跑。回滚旧二进制会恢复全 follower fanout，但新版本期间跳过的 inactive 派生行不会
自动出现，应按用户重建，不要默认恢复旧式全库 timeline。

开发环境需要清空全部 Home 派生缓存并测试冷启动时，停掉 ffdb 后执行：

```bash
echo purge_timeline | ./tools -to new_db -c purge_timeline
```

该命令只清空 TimelineIndex、TimelinePosition、TimelineState 三张表，不删除 Entry、互动或
社交图数据；随后访问 Home 会触发冷启动重建。

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

## 修复历史 Group Entry 作者

早期归档器在抓取 Group Feed 时曾把 `Entry.ProfileUuid` 覆盖为 Group UUID，原作者仍保留在
`Entry.From.Id`，目标 Group 则保留在 `Entry.FeedUuid`。这会令页面把 Group 显示为作者，并令
作者自己的 Profile Feed 缺少该 Entry。

命令只接受可以严格证明的记录：当前 `ProfileUuid` 和 `FeedUuid` 指向同一 Group，且
`From.Id` 能通过当前 `UserMap -> Profile` 解析为另一个有效 user。Group 自身发布的 Service/system
Entry、目标不一致及无法解析的记录均不修改。修复 Entry 与新增作者 EntryIndex 在同一 Pebble batch
提交；原 Group 目标索引保留。

先对单个作者、小批量 dry-run：

```bash
./tools -to <db-dir> -c migrate_group_entry_authors -user yinhm -max-limit 20 -dry-run
```

确认计数后执行同一范围，再全量 dry-run 和执行：

```bash
./tools -to <db-dir> -c migrate_group_entry_authors -user yinhm -max-limit 20
./tools -to <db-dir> -c migrate_group_entry_authors -dry-run
./tools -to <db-dir> -c migrate_group_entry_authors
./tools -to <db-dir> -c audit_store
```

该命令流式处理且幂等，不依赖 old DB。它已经补齐作者索引，因此正常执行后无需再运行
`rebuild_entry_index`；全量 `rebuild_entry_index` 仍会从修复后的 `ProfileUuid`（作者）和
`FeedUuid`（Group 目标）正确重建两类 direct index，可作为额外恢复手段。

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

## Like/Comment timeline 重建

表 115-117 是可重建的用户互动派生索引。首次上线或 `audit_store` 报告
`interaction_orphans` / `interaction_mismatches` 时，在停止所有打开该 Pebble 目录的
进程后执行。先对单个用户 dry-run，再对全库 dry-run；确认统计后去掉 `-dry-run`：

```bash
./tools -to <db-dir> -c rebuild_interaction_timelines -user yinhm -dry-run
./tools -to <db-dir> -c rebuild_interaction_timelines -user yinhm
./tools -to <db-dir> -c rebuild_interaction_timelines -dry-run
./tools -to <db-dir> -c rebuild_interaction_timelines
./tools -to <db-dir> -c audit_store
```

命令只从权威 Like/Comment 表重建派生索引；Comment 按 `(actor, entry)` 只保留最新一条，
同秒以 comment UUID 决胜。扫描使用固定 500 条工作集，不随全库记录数增长。缺失或非法
actor UUID、缺失日期的 archive 互动只计入 `unresolved_actor` / `missing_date`，保留权威
数据但不生成可授权的 timeline 行。apply 会先按目标用户范围清空三张派生表，因此执行
期间不得启动 ffdb 写入互动。

验收时 `audit_store` 的 `interaction_orphans=0`、`interaction_mismatches=0`；并抽查
`/feed/:name/likes`、`/feed/:name/comments` 的顺序、next cursor、Comment 折叠及本人访问
限制。当前页面仅允许本人查看，不能用迁移工具扩大可见性。

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

## Group 发现索引重建

表 119 是可重建的 Group 活动排序索引。首次上线先对一个 Group 验证，再执行全量重建：

```bash
./tools -to <db-dir> -c rebuild_group_index -group <group-id> -dry-run
./tools -to <db-dir> -c rebuild_group_index -group <group-id>
./tools -to <db-dir> -c rebuild_group_index -dry-run
./tools -to <db-dir> -c rebuild_group_index
./tools -to <db-dir> -c audit_store
```

全量 apply 会先范围删除 T119，再流式扫描 Profile；每个有效 Group 只读取其最新 direct Entry，
无历史 Entry 的 Group 使用 Unix epoch。运行时 Like/Comment activity 无法由 direct EntryIndex
完整还原，重建后会回到最新发帖位置，之后的新互动会再次移动它。命令必须停服执行；中途中断时
目录可能不完整，直接重跑即可。

## Task Queue / Service 聚合上线

表 111-113、203-207 都是新增表，无历史数据迁移或 rebuild。部署前停止 ffdb 并备份
Pebble 目录；升级二进制后正常启动即可。启动后检查：

```bash
./tools -to <db-copy> -c audit_store
./tools -to <db-copy> -c list_tasks -task-state ready -max-limit 20
./tools -to <db-copy> -c list_tasks -task-state inflight -max-limit 20
./tools -to <db-copy> -c list_tasks -task-state dead -max-limit 20
```

这些工具会自行以只读或读写模式打开数据库，但 Pebble 仍不允许另一进程同时打开同一
目录；生产检查应针对停服目录或一致性副本执行。正常的
旧库首次启动没有 Service/Task 行；不得为了“初始化”手工写空表。既有表 101 的
Twitter FeedService 会按原字段号继续读取，但不会被 Web Feed 调度器抓取。旧 FeedJob
客户端、RPC 和调度器已经退役；表号 200–202 永久保留且不得复用。Done 清理必须先带 `-dry-run` 和
明确 `-before`，确认计数后再执行同一 cutoff。

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
