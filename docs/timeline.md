# Home Timeline 设计

本文记录 FriendFeed 风格 Home timeline 的排序语义、持久化编码、并发边界和重建规则。
Profile/target feed 的 direct EntryIndex 仍按原帖发布时间排序，不使用本文的活动排序。

## 视图语义

| 视图 | 排序字段 | 互动后是否移动 |
| --- | --- | --- |
| Profile / target feed | `entry.Date` | 否 |
| Home | viewer 对该 Entry 的 `activity_at` | 按规则有限 bump |
| Public | 最后一次 push/bump 的服务器时间 | 是，见下文 Public Timeline 设计 |
| Search | 各自现有规则 | 否 |

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

Follow 关系变化不触发全量 Home rebuild。公开 user Follow 和 Group Join 只从新增 Feed 的 direct
EntryIndex 选取最新至多 100 条，恢复其当前 activity 后增量写入，再 trim 到 Home 全局上限；
Unfollow/Leave/RemoveMember 只删除现有 bounded Home 中属于该 Feed 的行。关系写入与维护 task
同批提交，task 执行时必须重新核对当前 Follow 边，避免快速反向操作被旧 task 覆盖。完整
`rebuild_timeline` 只用于迁移、显式修复和冷缓存重建，不进入普通 Follow 请求路径。

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
UUID，也不得跨 viewer 解释。匿名 HTTP 请求携带旧 Home/Public/Profile `?start=N` 时直接
302 到对应 Feed 第一页，不执行 O(Start) 扫描；登录用户可读取一次 legacy offset 页，但该页
只生成 cursor 形式的下一页链接，不再延续 offset 链。不回读已退役的
`EntryIndex | TimelineUUID`。这条兼容入口在当前版本明确保留，不进入普通退役清单。

旧 Home 实现使用的 `TimelineUUID`、`FanoutEntry`、`DeleteFanoutEntry` 及其读取分支已经退役；
`EntryIndex` 只保留 Profile/Group 的 direct feed 索引。登录用户的旧 `?start=N` Home 链接仍由
`TimelineIndex` 读取一次并切换到 cursor，不依赖旧 Home 索引。

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
v2.2.0 的 migrate_entry_keys
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

### 当前实现：活跃用户的有界 Home 缓存

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
3. 活跃 viewer 最多保留 10,000 条，inactive viewer 保留最近 500 条冷缓存，并保留可调整的时间窗口。当前时间窗口为 MAX（不按
   日期裁剪），实际仅受条数上限约束；未来可调整为 90 天或其他窗口。当前数据以历史内容
   为主且活跃度低，默认启用日期裁剪可能让用户 Home 无内容。
4. 裁剪、过期或重建必须同时处理 TimelineIndex 与 TimelinePosition。
5. TimelineState 过期后不再接收 fanout；compact 将其收敛为 500 条冷缓存，不影响 canonical source。
6. 新 Entry 必须能进入活跃 viewer 的 Home，不能用“该 Entry 已有 TimelinePosition”代替
   TimelineState 判断，否则首次插入会被错误跳过。

Home 热读先检查 TimelineState：一小时内无需维护时不获取协调锁。需要初始化、重建或裁剪时，
请求只按 viewer UUID 通过 singleflight 调度后台维护，不等待工作完成；同一 viewer 只执行一次，
不同 viewer 可并行。跨 viewer 的昂贵维护最多同时执行 8 个，避免冷启动流量同时扫描 Pebble
导致 I/O 与内存尖峰。singleflight 在调用完成后自动回收 key，不保留随用户数增长的锁表。
过期 State 仍有冷缓存时立即返回 stale 数据；完全无缓存时 gRPC 返回稳定的 initializing 状态，
ffweb 映射为 HTTP 202、`Retry-After: 2` 和两秒自动刷新。后台任务不继承 RPC deadline，并纳入
ApiServer 的关停 WaitGroup，关闭 Pebble 前必须排干。OAuth 登录成功后只触发后台预建，不阻塞回调。
维护失败不替换旧缓存、不刷新 State，并对该 viewer 退避 30 秒后才允许重试，避免坏数据随每次
Home 请求反复触发昂贵扫描。

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
- 默认模式预热同时具有 Profile 与 OAuth 身份的真实用户，并创建或刷新 TimelineState；这会把
  预热用户暂时视为活跃，只用于部署或明确的全量重建，不作为周期任务；
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
- 无 State 或 State 已过期：保留最近 500 条冷缓存，成对删除其余 TimelineIndex/Position；只有
  完全没有 Index 的 position-only viewer 才整段回收；
- 当前时间窗口为 MAX，因此 2.0 只按活跃状态、10,000/500 条上限清理；
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

## Public Timeline 设计

Public feed 从内存 `FeedIndex` 缓存迁移到 TimelineIndex 体系。已确认的决策：私有 feed
的 Entry 不进入 public；仅新建 Entry、首次 Like、新建 Comment 触发 bump；当前实现
`FeedIndex` 整体清理；部署空窗可接受，不做双写。可能引入请求路径 O(n) 开销或写放大的
方案在设计阶段拒绝，见"性能约束"一节。

### 现状行为（迁移基准）

当前 public 是 `server/index.go` 的 `FeedIndex`：内存 buffer 保存最多 1000 条 hex Entry
key，每秒重建一次，每 5 分钟及关停时 gob 序列化到 Meta 表（key 为
`uuidv5("index:public:cache")`）。只支持 `Start/PageSize` 翻页。

buffer 排序不是 `entry.Date`，而是**最后一次 push 的到达时间**：`rebuildFeedBuffer`
把 pending push 按新到旧排列、按 key 去重后接在旧 buffer 前面，因此任何一次 push 都把
对应 Entry 顶到最前。push 触发点：

- `ArchiveFeed` / `ForceArchiveFeed` / `PostEntry`（`PostTweet` 复用 `PostEntry`）：
  每次 `PutEntry` 成功都 push，包括重复 archive 已存在的 Entry。`PutEntry` 对已有 key
  覆盖写成功，因此 refetch 周期中 crawler 重发的近期 Entry 会被反复顶到最前；
- `LikeEntry`：`PutLike` 对重复 like 同样返回 Entry key，因此每次 like 请求都 bump，
  没有 Home 的 7 天窗口和 10 分钟冷却；
- `CommentEntry`：`PutComment` 对评论编辑也返回 Entry key，因此创建和编辑都 bump；
- Unlike、DeleteComment、DeleteEntry 不 push；
- 无任何隐私过滤：私有 feed 的 Entry 同样进入 public。

### 目标语义

Public 作为 TimelineIndex 体系中的一个特殊 viewer，不新增表，复用 108/109 的 key/value
编码、cursor 编解码、懒删除、audit 与 DeleteRange 工具：

```text
publicTimelineUUID = uuidv5("timeline:public")   // 新的保留 UUID
prefix = TableTimelineIndex | publicTimelineUUID
```

不复用旧的 `index:public:cache` UUID，避免与退役中的 gob 缓存 key 混淆。

bump 规则在现状基础上收敛为"仅新建触发"，与现状的差异均为已确认决策：

```text
新建 Entry（PutEntry 且 Entry key 此前不存在）：activity_at = serverNow，插入到最前
重复 archive 已有 Entry：不 bump（偏离现状：消除 refetch 周期重发造成的无效写放大，
  并避免历史导入把旧内容冲刷到 public 首页）
首次 Like：bump，无 7 天窗口、无 10 分钟冷却；重复 Like 不 bump（偏离现状）
新建 Comment：bump；编辑 Comment 不 bump（偏离现状）
Unlike / DeleteComment / DeleteEntry：不 bump、不回退
```

注意 public 的 `activity_at` 是 bump 事件的服务器时间，不是 `entry.Date`：今天新抓取
到一条历史旧帖也会排到最前。移动直接复用 `MoveTimelineEntry`（position 读取在
`ApplyBatch` 串行边界内，天然并发安全），`qualify` 为 nil，不做 Home 的资格检查。

**隐私过滤**是本次有意改变的行为：bump 触发点在写入前解析 Entry 的 target feed
（`FeedUuid`）profile，`Private` 为 true 时跳过。已入库的历史私有 Entry 由回填阶段的
过滤保证不进入新 timeline。

### 独立容量上限

```go
const publicTimelineMaxEntries = 10_000 // 独立于 homeTimelineMaxEntries
```

比现在 1,000 条深，public 长尾翻页能力随迁移自然获得。裁剪分两层：

- 事件驱动的摊还裁剪：bump 计数每攒满 100 次（`publicTimelineTrimEvery`）就在后台
  goroutine 执行一次 trim，从最新端前向数到第 Max 行，对尾部 `DeleteRange`，并从解析
  出的 key 成对删除 Position；最多同时一个 trim，由 `ApiServer` 的关停 WaitGroup 排干。
  不挂周期 ticker（job 系统即将退役），无 bump 时零开销，裁剪不进入任何请求路径；
- `compact_timelines`：对 public viewer 使用 `publicTimelineMaxEntries`，永不进入
  500 条冷缓存路径。

### 性能约束

以下方案因可能造成性能问题在设计阶段明确拒绝：

- 请求路径内的裁剪或行数统计：任何 RPC 不做 O(Max) 扫描，裁剪只在后台 ticker 执行；
- 每次 bump 后触发裁剪：bump 必须保持 O(1)，即一次 position 点读加一个三条记录的
  小 batch；
- 新旧双写过渡：旧 buffer 在新版本下没有读方，双写只会放大 archive 流的写延迟；
- 为 created/隐私判断改动 `PutEntry`/`PutLike`/`PutComment` 签名：契约风险大于收益，
  一律用主键点读前置判断；
- 用 OAuth 或 Profile 全表扫描推导 public 成员资格：不存在该需求，逐事件点读即可。

已知且接受的有界开销：

- "仅新建 Entry bump" 的存在性判断为 `db.Exists(entryKey)` 点读，发生在 archive 流
  的每条 Entry 上；archive 流内隐私结论按 `FeedUuid` 缓存，每个流每个 feed 只点读
  一次 profile；
- 匿名旧 `?start=N` 链接直接 302 到第一页；仅登录用户允许执行一次 O(Start) 兼容扫描，
  当前页之后立即切换 cursor，深度仍被 public Max（10,000）约束；
- 读路径每页为 PageSize 次 Entry 点读加 interaction 加载，与 Home/profile 读路径相同；
- trim 单次为 O(Max + 尾部行数) 的 iterator 步进，每 100 次 bump 最多触发一次，无新
  bump 时不执行。

### TimelineState 特例

Public 不受"30 天不访问过期"约束。新增 `model.IsPublicTimeline(uuid)` 判断：

- `prepareHomeTimeline` 与活跃性判定对 public 短路，永远视为活跃，不写 State 行；
- fanout 不会触及 public：bump fanout 只走向 follower/owner，public UUID 不是真实
  用户，不会被枚举；
- State 语义保持"真实用户活跃度"，不被特殊行污染。

### 写路径

`spread()` 的 5 个调用点（ArchiveFeed、ForceArchiveFeed、PostEntry、LikeEntry、
CommentEntry）改为调用 public bump，调用前完成两个前置判断（均为主键点读）：

1. created 判断：archive/post 路径用 `db.Exists(entryKey)`；like/comment 路径用导出的
   `LikeKey`/`CommentKey` 前置点查，仅新建时继续。并发下偶发的重复 bump 由
   `MoveTimelineEntry` 的单调性兜底，不产生不一致；
2. 隐私判断：解析 Entry 的 target feed profile，`Private` 则跳过；archive 流内按
   `FeedUuid` 缓存结论；

随后复用 `MoveTimelineEntry` 提交原子 bump batch，失败必须返回错误，与 Home fanout
的错误策略一致。ArchiveFeed/ForceArchiveFeed 的 bump 保持在 `entryLifecycleMu` 互斥锁
外（与现状 `spread` 的位置一致）；PostEntry 的 bump 在锁内，新增开销为两次点读加一个
小 batch，与锁内已有的 profile 读取同量级。

### 读路径

`FetchFeed("public")` 改走 `ForwardFetchFeedWithCursor`，prefix 为
`TableTimelineIndex | publicTimelineUUID`：

- cursor 分页直接可用，ffweb 后续可迁移；
- 匿名旧 `?start=N` 链接按 Home 相同规则回到第一页；登录用户可读取一次原页，下一页切换
  cursor；
- 读到指向缺失 Entry 的孤儿行时复用 Home 的懒删除逻辑。

### 回填

`rebuild_timeline` 增加 `-public` 模式：

- 候选来自所有**非私有** feed 的 direct EntryIndex，从最新端前向扫描，用大小为
  `publicTimelineMaxEntries` 的最小堆做 k-way 归并，同一时间只打开一个 iterator；
- 初始 `activity_at = entry.Date`（发布时间的 reverse ms），写入 Index + Position；
- 精度边界：历史 push 顺序（到达时间）没有 event log，无法恢复，回填只能按发布时间
  近似；切换后的实时 bump 会迅速把活跃内容顶到前面，这与 `rebuild_timeline` 对 Home
  的精度承诺一致；
- 流式、内存有界、支持 dry-run，先针对小 feed 上限验证。

### 已完成的 FeedIndex 退役

2.0 已完成以下清理：

- `server/index.go` 整体（`FeedIndex`、`rebuildFeedBuffer`、`MinQueue`、gob load/dump）；
- `ApiServer.cached`、`cachedFeed`、`FetchFeed` 的 cached 分支、`spread()` 中的 Push；
- `IndexJobTicker` 的周期 dump 直接删除（job 系统即将退役，不往里加新任务），public
  timeline 的裁剪由 bump 驱动的后台调度承担；
- Meta 表中 `index:public:cache` 的 gob 行随迁移删除；
- 引用它的测试（`index_test.go`、`entry_concurrency_test.go`、`server_test.go` 中的
  public cache 用例）改写为 public timeline 用例。

### 部署顺序

```text
1. 部署新代码（写路径 bump + 读路径 cursor，public timeline 初始为空）
2. rebuild_timeline -public（先 dry-run 验证，再 apply）
3. audit_store 验证 108/109 双向一致
4. `v2.2.0` 升级期确认 public 页面正常后，删除 `index:public:cache` Meta 行
```

部署到回填完成之间 public 页面为空，该空窗已确认可接受。本阶段不做双写：`FeedIndex`
在同一次变更中移除，回滚方式为回滚部署版本并重新跑回填（旧 gob 缓存在新版本下不再
被读取）。
