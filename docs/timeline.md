# Home Timeline 设计

本文记录 FriendFeed 风格 Home timeline 的排序语义、持久化编码、并发边界和重建规则。
Profile/target feed 的 direct EntryIndex 仍按原帖发布时间排序，不使用本文的活动排序。

## 视图语义

| 视图 | 排序字段 | 互动后是否移动 |
| --- | --- | --- |
| Profile / target feed | `entry.Date` | 否 |
| Home | viewer 对该 Entry 的 `activity_at` | 按规则有限 bump |
| Public / Search | 各自现有规则 | 否 |

Home 是会移动的 ranked stream，不是分页快照。翻页期间 Entry 移到 cursor 之前，可能造成
跨请求暂时重复或漏看；这是预期的弱一致性。必须保证单页无重复、cursor 不死循环、删除
锚点后可继续，不承诺多个请求之间形成稳定快照。

## 持久化编码

所有 table prefix、UUID 和时间均为 raw bytes；不得写 UUID 字符串或 hex 文本。整数使用
big-endian。`T` 是 4 B table prefix，UUID 是 16 B。

### TimelineIndex（prefix 108）

```text
key =
  TableTimelineIndex       4 B
  viewer UUID             16 B
  ^UnixMillis(activity)    8 B
  entry UUID              16 B
                            ----
                            44 B

value = empty
```

对同一个 viewer，按 key 前向扫描即可得到 `activity_at` 从新到旧的 Entry；同一毫秒内以
entry UUID 作稳定 tie-break，同时避免 key 碰撞。反转时间使用 `^uint64(unixMillis)`，不是
字符串、Flake ID 或 `MaxTime - timestamp`。

value 必须为空。Entry UUID 已在 key 末尾，canonical Entry key 可确定性构造为：

```text
TableEntry(4 B) | entry UUID(16 B)
```

重复保存 canonical Entry key 会每行额外增加 20 B，没有读取收益。

### TimelinePosition（prefix 109）

```text
key =
  TableTimelinePosition    4 B
  viewer UUID             16 B
  entry UUID              16 B
                            ----
                            36 B

value = UnixMillis(activity), 8 B uint64 big-endian
```

TimelinePosition 提供反向查询 `(viewer, entry) -> activity_at`。移动一个 Entry 时，必须先
用旧 activity 构造并删除旧 TimelineIndex key。没有 Position 表就只能扫描整个用户
timeline，单次 bump 会从点查退化为 O(n)。

两张表都包含 viewer、entry 和 activity 的信息，是两种查询方向所需的双向派生索引：

```text
(viewer, activity, entry) -> 按活动时间扫描
(viewer, entry)           -> 定位当前活动时间
```

这不是无意义的 value 重复；两张表均可从源数据完全重建。

## 原子移动与并发

单个 `(viewer, entry)` 的移动必须在一个 Pebble batch 串行边界中完成：

```text
atomic bump(viewer, entry, newActivity)
├── read   TimelinePosition(viewer, entry) = oldActivity
├── check  单调性、Like 资格与冷却
├── delete TimelineIndex(viewer, reverse(oldActivity), entry)
├── put    TimelineIndex(viewer, reverse(newActivity), entry) = empty
└── put    TimelinePosition(viewer, entry) = newActivity
```

活动时间只允许单调前进。position 不存在时，若 viewer 按当前 Follow/Follower 关系应拥有
该 Entry，则直接插入。源 Entry/Like/Comment 先提交；无上限 follower fanout 位于源 mutation
batch 外，失败必须返回，并由 audit/rebuild 修复派生状态。

## Bump 规则

```text
新 Entry：activity_at = min(entry.Date, serverNow)

新 Comment：总是 bump
编辑 Comment：不 bump
删除 Comment：不 bump、不回退

首次成功写入 Like：
  - 0 <= serverNow - entry.Date <= 7 天；且
  - 该 (viewer, entry) 距上次 activity bump 至少 10 分钟
  满足时 bump

重复 Like：不 bump
Unlike：不 bump、不回退
Unlike 后重新 Like：仍受 7 天窗口和 10 分钟冷却约束
```

Comment 的 Date 由服务端在创建时写入，编辑保留原 Date；不信任客户端事件时间。Comment
不受 Like 冷却影响。未来 Entry 不能借负年龄绕过 Like 时间窗口。

## 分页 cursor

Home cursor 只编码 TimelineIndex 中 viewer 固定前缀之后的位置：

```text
reverse activity time   8 B
entry UUID             16 B
                        ----
                        24 B，再用 Base58 传输
```

解码后必须与当前请求的 `TableTimelineIndex | viewer UUID` 前缀重新拼接。cursor 不是 Entry
UUID，也不得跨 viewer 解释。旧 Home `Start/PageSize` 链接仍在 TimelineIndex 上跳过 Start
行兼容，不回读已退役的 `EntryIndex | TimelineUUID`。

当前 cursor 编解码对 TimelineIndex 和 direct EntryIndex 使用同一 24 B 位置格式
（reverse ms 8 B + entry UUID 16 B）；同秒多帖由 UUID 后缀消歧。这是显式表格式选择，
不是从任意内容猜测编码。

## 删除、审计与重建

DeleteEntry 不枚举全部 viewer。Home 读取发现 TimelineIndex 指向缺失 Entry 时，在一个
batch 中删除对应 index 与 position；未再次读取的孤儿由 `audit_store` 和
`rebuild_timeline` 处理。

`audit_store` 检查：

- TimelineIndex 与 TimelinePosition 双向缺失；
- TimelineIndex 指向缺失 Entry；
- 同一 `(viewer, entry)` 的重复 index；
- index 与 position 的 activity timestamp 不一致。

`rebuild_timeline` 只读取 canonical raw Entry/EntryIndex。若发现字符串 Entry key 或 index
value，必须先运行：

```text
migrate_entry_keys
rebuild_entry_index
rebuild_timeline
audit_store
```

对每个目标 viewer，重建从自己的 direct feed 和当前 Follow 关系取得 Entry，收集当前 Like
与 Comment，按时间升序重放；同一时间固定为 publish、Like（actor UUID）、Comment
（comment UUID）。已 Unlike、已删除 Comment 和历史 Follow 变化没有 event log，不能精确
复现其过去 bump；重建只承诺从当前 canonical 源数据生成确定结果。

## 为什么不继续压缩 key

当前 44 B TimelineIndex key 已是合理的最小实用结构：

- 4 B table prefix 维持统一 keyspace 分区；Pebble block prefix compression 会压缩相邻行
  共同的 table + viewer 前缀；
- 8 B timestamp 使用标准整数编码。改为自定义 6 B 只理论节省 2 B，却增加 schema、溢出和
  cursor 复杂度；
- 16 B entry UUID 同时承担唯一性和同毫秒 tie-break，不能删除或移到 value；
- viewer UUID 不能安全截短，另建短 ID 映射会引入新的源数据、分配和迁移契约；
- Entry UUID 不具备活动时间顺序，不能替代 timestamp。

因此不采用自定义 48-bit 时间、短 viewer ID、hash/sequence tie-break 或非空 index value。
除非实际存储测量证明该布局是瓶颈，否则保持编码稳定优先于节省少量理论字节。

## 容量与生命周期

TimelineIndex 与 TimelinePosition 的编码适合 Home 的活动排序，但当前“为全部 follower 永久
保存完整历史”的生命周期不适合实际数据分布。一次生产审计得到：

```text
Entry                 7,733,672
TimelineIndex        28,582,865
TimelinePosition     28,582,865
```

平均每条 Entry 约产生 3.7 份 Home 副本，两张派生表合计超过 5,700 万行。两表必须同时存在：
Index 支持按活动时间扫描，Position 支持 O(1) bump；只压缩 key 或删除 Position 不能解决无限
增长。根因是运行时 fanout 遍历全部历史 follower，而有 OAuth 身份也只表示曾经登录，不等于
当前仍活跃。

### 后续目标：活跃用户的有界 Home 缓存

TimelineIndex/TimelinePosition 应继续作为可丢弃、可重建的派生数据，但其职责收窄为活跃用户
的有界 Home 缓存，而不是永久历史索引。后续设计引入独立的 TimelineState：

```text
canonical source
├── Entry / direct EntryIndex
├── Like / Comment
└── Follow / Follower

derived Home cache
├── TimelineState(viewer)
├── TimelineIndex(viewer, activity, entry)
└── TimelinePosition(viewer, entry)
```

TimelineState 使用 table prefix 110：

```text
key   = TableTimelineState(4 B) | viewer UUID(16 B)
value = last_access UnixMillis(8 B, big-endian)
```

最近 30 天访问过 Home 的 viewer 视为活跃；距离上次写入不足 1 小时时不重复 touch。不得复用
OAuth 存在性充当活跃状态。

目标运行规则：

1. 只有近期访问过 Home、拥有有效 TimelineState 的 viewer 才持续接收 fanout。
2. 长期未访问的 viewer 不维护 Home；再次访问时从当前 Follow 和 direct EntryIndex 按需重建。
3. 每个 viewer 最多保留 10,000 条，并保留可调整的时间窗口。当前时间窗口为 MAX（不按
   日期裁剪），实际仅受条数上限约束；未来可调整为 90 天或其他窗口。当前数据以历史内容
   为主且活跃度低，默认启用日期裁剪可能让用户 Home 无内容。
4. 裁剪、过期或重建必须同时处理 TimelineIndex 与 TimelinePosition。
5. TimelineState 过期后可删除该 viewer 的全部 Home 派生行，不影响 canonical source。
6. 新 Entry 必须能进入活跃 viewer 的 Home，不能用“该 Entry 已有 TimelinePosition”代替
   TimelineState 判断，否则首次插入会被错误跳过。

Home 热读先检查 TimelineState：一小时内无需维护时不获取协调锁。需要初始化、重建或裁剪时，
按 viewer UUID 通过 singleflight 合并并发请求；同一 viewer 只执行一次，不同 viewer 可并行。
跨 viewer 的昂贵维护最多同时执行 8 个，避免冷启动流量同时扫描 Pebble 导致 I/O 与内存尖峰。
singleflight 在调用完成后自动回收 key，不保留随用户数增长的锁表。

该模型将写放大从“全部历史 follower × 永久历史”收敛为“活跃 follower × 有界窗口”。当前
规模下不采用完全读时合并：跨多个 direct feed 的 iterator 合并、互动 bump 排序和 cursor
状态更复杂，读取成本也会随关注数增长。

### 落地边界

当前实现已按以下顺序落地，表前缀 108/109 和 key/value 编码保持不变：

1. 定义 TimelineState schema、活跃判定和窗口常量；
2. Home 访问按需初始化或刷新有界缓存；
3. fanout 只面向活跃 viewer；
4. 增加成对裁剪与过期清理；
5. 让 rebuild、audit 和迁移工具遵守相同窗口；
6. 清理既有 inactive viewer 的派生行。

运行时不得通过检查 TimelinePosition 是否存在来限制 fanout，也不得把 OAuth 用户集合当成
长期容量边界。

### 有界重建的精度边界

本轮不新增按事件时间排列的全局 activity index。现有 Like/Comment 按 Entry UUID 分组，
无法从“最近发生的互动”反查 Entry。因此按需重建不能同时做到扫描量有界和精确恢复全部历史
bump。

`rebuild_timeline` 先从当前关注 feed 的 direct EntryIndex 中按发布时间归并候选，再只对候选
Entry 重放当前 Like/Comment：

- 时间窗口为 MAX 时，不按发布日期淘汰候选，但仍受 10,000 个唯一 Entry 的数量上限约束；
- 时间窗口改为 90 天等有限值时，候选同时受日期和数量限制；
- 候选集合内的 activity 排序与 bump/cooldown 规则应精确；
- 候选集合外的历史 Entry，即使最近新增 Comment，也可能不进入重建结果；
- viewer 成为 active 后的新互动由实时 fanout 正常维护。

这是明确的产品取舍。若未来要求精确恢复所有长尾 bump，必须另行设计 interaction activity
index；不能通过重新扫描全部历史数据规避容量边界。

### rebuild_timeline 与 compact_timelines

两个命令都操作派生表，但解决的问题不同，不能互相替代。

`rebuild_timeline` 是语义重算：

```text
canonical source
  Follow + direct EntryIndex + Entry + Like + Comment
      ↓ 重新选择候选并计算 activity
TimelineIndex + TimelinePosition + TimelineState
```

它适用于首次初始化、关注关系变化后修复、排序漂移、Index/Position 不一致，或明确要求重新
计算某个用户的 Home。改造后：

- `-user <id>` 重建指定用户，不要求其已有 TimelineState，并在成功后创建或刷新 State；
- 默认模式只处理已有有效 TimelineState 的 viewer，不再把 OAuth 记录当成活跃用户集合；
- 使用与在线初始化相同的时间窗口和 10,000 条上限；
- 逐个 direct EntryIndex 从最新端前向扫描，用 10,000 条最小堆保留全局最新候选；同一时间
  只打开一个 Pebble iterator，避免关注数较大时耗尽 iterator 资源；
- 对候选读取 Entry/Like/Comment，重算最终 activity；
- 用固定 batch 替换该 viewer 的 108/109 行，成功后才更新 TimelineState；
- dry-run 只报告候选数、现有行数、差异和预计写入量，不修改数据库；
- 全过程内存上限与候选上限相关，不与全库 Entry 数相关。

`compact_timelines` 是容量清理：

```text
existing TimelineState + TimelineIndex + TimelinePosition
      ↓ 按活跃状态、时间窗口和条数裁剪
same derived tables, fewer rows
```

它不读取 Follow、direct EntryIndex、Like 或 Comment，不重新计算 activity，也不会补回缺失 Entry。
它适用于部署后的批量空间回收和周期维护：

- 有效 TimelineState：保留现有 activity 排序中满足时间窗口的前 10,000 条，成对删除其余
  108/109 行；有限窗口在 rebuild 中约束 publish 候选，在 compact 中只能按现有 activity
  判断，因为 compact 刻意不读取 Entry；
- 无 State 或 State 已过期：用 viewer prefix 范围删除其全部 TimelineIndex/Position；
- 当前时间窗口为 MAX，因此第一版只按活跃状态和 10,000 条上限清理；
- dry-run 流式统计 viewer、现有行和预计删除行，不保留全表 key；
- apply 使用 DeleteRange 或固定 batch，可中断并安全重跑；
- 它只保证容量和两表成对清理，不保证 Home 与 canonical source 语义一致。

典型执行顺序：

```text
部署 TimelineState 与受限 fanout
→ 让真实用户访问并建立 State
→ rebuild_timeline -user <id> 验证重点用户
→ compact_timelines -dry-run
→ compact_timelines
→ audit_store
```

若 compact 后发现某个 active 用户内容或排序不正确，应对该用户运行 `rebuild_timeline -user`，
而不是再次 compact。
