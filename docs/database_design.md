# 数据库设计

本文记录当前 Pebble 数据模型、关键编码和重建边界。迁移操作见
[db_migration.md](db_migration.md)，未决事项见 [open_decisions.md](open_decisions.md)。

## 总体结构

项目使用单个 Pebble DB。所有表共享同一有序 keyspace，以 4 字节大端整数作为表
前缀；protobuf 是主要 value 编码，少量映射和集合使用原始字节。

```text
Pebble
├── 源数据
│   ├── Profile / OAuth / UserMap / UserRenameMap
│   ├── Entry
│   ├── Like / Comment
│   └── Follow / Follower
├── 必须随源数据原子维护的直接索引
│   └── author feed / target feed EntryIndex
└── 可重建派生数据
    ├── TimelineIndex / TimelinePosition（含 public timeline）
    └── Bleve search index
```

`mdb` 与 `rdb` 目前指向同一个 `Store`，只是保留历史职责名称。Pebble 没有 column
family；表前缀承担逻辑分区作用。

## 通用 key 片段

| 片段 | 长度 | 编码 |
| --- | ---: | --- |
| table prefix | 4 B | `uint32` big-endian |
| UUID | 16 B | UUID 原始字节，不是字符串 |
| Flake ID | 16 B | timestamp 8 B + worker 6 B + sequence 2 B，big-endian |
| string identity | 可变 | 规范化后的原始字符串 |
| canonical Entry key | 20 B | `TableEntry(4) + Entry UUID(16)` |

前缀扫描依赖字节序和字段顺序，不得在没有离线迁移时调整持久化编码。
Entry 与 EntryIndex 中的 UUID 均为 raw bytes，不允许用 UUID 字符串或 hex 文本代替。

### 历史 Entry 字符串 key

2021 年短期使用过 `Entry.Put(db, entry.Id, ...)`。当 `entry.Id` 是带连字符 UUID 时，
`KeyFromString` 无法按 hex 解码，因而写出了错误的 `TableEntry(4) + UUID string(36)`；
后来的 EntryIndex rebuild 又曾直接复用该 40 B key，使污染进入 index value 和 key 后缀。
这不是受支持的第二种编码。当前运行时和 TimelineIndex 不兼容字符串 key；如
`verify_schema` 检出该遗留格式，只能使用 `v2.2.0` 的一次性工具在离线副本修复，再用
当前 `rebuild_entry_index` 丢弃并重建派生索引。

## 主要表与 key 设计

`T` 表示 4 字节 table prefix，`U` 表示 16 字节 UUID。

| Prefix | 表 | Key（省略 value 中的 protobuf 细节） | Value | 性质 |
| ---: | --- | --- | --- | --- |
| 0 | Meta | 领域固定字符串 + UUID | raw UUID、JSON 或 protobuf | 小型 metadata 与可重建物化视图；含 Group owner/activity、Feed archive，以及 `import-operator-token/v1` 短期导入凭据 digest |
| 1 | Feed | 保留，当前无运行时表实例 | — | 历史前缀 |
| 2 | Feedinfo | `T + feed UUID` | `pb.Feedinfo` | 历史/迁移 metadata |
| 3 | Entry | `T + entry UUID` | `pb.Entry`，不再内嵌 canonical Like/Comment | 源数据 |
| 4 | UserMap | `T + current profile id` | 16 B profile UUID | 源映射 |
| 5 | EntryIndex | 见下一节 | 空 | Profile/target feed direct 索引 |
| 6 | Tweet | `T + tweet key` | `pb.Tweet` | 导入数据 |
| 7 | UserRenameMap | `T + old profile id` | 16 B profile UUID | soft rename metadata |
| 100 | Profile | `T + profile UUID` | `pb.Profile` | 源数据 |
| 101 | FeedService | `T + target Feed UUID + service id` | `pb.FeedService` | Feed 外部服务绑定及独立授权 |
| 102 | Follow | `T + follower UUID + feed UUID` | `"1"` | 正向社交边 |
| 103 | Follower | `T + feed UUID + follower UUID` | `"1"` | 反向社交边 |
| 104 | OAuth | `T + provider:user_id` | `pb.OAuthUser` | 稳定上游身份 |
| 105 | File | 保留，当前无运行时读写 | — | 预留前缀 |
| 106 | Like | `T + entry UUID + actor UUID` | `pb.Like` | 源数据、天然幂等 |
| 107 | Comment | `T + entry UUID + comment UUID` | `pb.Comment` | 源数据 |
| 108 | TimelineIndex | `T + viewer UUID + reverse Unix ms + entry UUID` | 空 | Home 排序派生数据 |
| 109 | TimelinePosition | `T + viewer UUID + entry UUID` | Unix ms（8 B big-endian） | Home 位置派生数据 |
| 110 | TimelineState | `T + viewer UUID` | last-access Unix ms（8 B big-endian） | Home 活跃状态派生数据 |
| 111 | Service | `T + service UUID` | `pb.Service` | 全局规范化外部来源，不含凭据 |
| 112 | ServiceState | `T + service UUID` | `pb.ServiceState` | 条件 GET 与调度状态 |
| 113 | ServiceFeedIndex | `T + service UUID + target Feed UUID + service ID` | 空 | Service→FeedService 派生索引 |
| 114 | GroupAdmin | `T + group UUID + admin user UUID` | 空 | Group admin 角色权威来源 |
| 115 | LikeTimeline | `T + actor UUID + reverse Unix ms + entry UUID` | 空 | 用户 Like 排序派生索引 |
| 116 | CommentTimeline | `T + actor UUID + reverse Unix ms + entry UUID` | 16 B latest comment UUID | 用户 Comment 折叠排序索引 |
| 117 | CommentTimelinePosition | `T + actor UUID + entry UUID` | reverse ms + latest comment UUID | Comment 排序位置索引 |
| 118 | FollowRequest | `T + target feed UUID + requester user UUID` | RFC3339 时间字符串；新写为 RFC3339Nano | private feed/Group 关注申请（工作流数据，非关系事实） |
| 119 | GroupIndex | `T + reverse activity Unix ms + group UUID` | 空 | Group 发现页排序派生索引 |
| 120 | Notification | `T + recipient UUID + notification UUID` | versioned JSON NotificationRecord | recipient-owned canonical 通知 |
| 121 | NotificationInbox | `T + recipient UUID + ^UnixMillis(activity_at) + notification UUID` | 空 | newest-first 通知排序索引 |
| 122 | NotificationState | `T + recipient UUID` | versioned JSON：read watermark + counters | O(1) unread/total 状态 |
| 123 | FeedApiKey | `T + raw Feed UUID` | `pb.FeedApiKeyRecord`：8 B key ID、32 B secret SHA-256、生命周期时间 | 每 Feed 至多一个 active Public API credential；不存明文 token |
| 200–202 | 保留表号 | 不再读写 | 不再读写 | 禁止复用 |
| 203 | Task | `T + raw Flake ID` | `pb.Task` | Task 权威状态 |
| 204 | TaskReady | `T + type_len(1) + type + run time + task ID` | 空 | READY 派生索引 |
| 205 | TaskLease | `T + lease time + task ID` | 空 | INFLIGHT 派生索引 |
| 206 | TaskIdem | `T + SHA-256(type,idempotency key)` | raw Flake ID | 活跃去重索引 |
| 207 | TaskDone | `T + finish time + task ID` | `pb.TaskCompletion` | 完成/死信历史 |
| 300–303 | 已退役 Stock 子系统 | 不再读写 | 不再读写 | 永久保留表号，禁止复用 |

Follow/Follower 必须在同一个 Pebble batch 中更新：

```text
follow(A, B)
  └── atomic batch
      ├── Follow   | A | B = "1"
      └── Follower | B | A = "1"
```

Like 使用 `(entry UUID, actor UUID)` 作为 key，因此同一用户重复点赞覆盖同一行；Comment
使用稳定 comment UUID。读取 Entry 时分别前缀扫描两张表完成 hydration，不逐 actor
查询 Profile。

Notification 的完整领域契约见 [notifications.md](notifications.md)。Inbox 的 reverse
Unix ms 与 TimelineIndex/EntryIndex 采用同一排序约定，并以 notification UUID 作为同毫秒
tiebreak；未读判断使用 canonical record 的 `created_at_ns` 与 State 的
`last_read_at_ns`，不能把 activity timestamp 当作 read watermark。每个 recipient 正常
保留 500 条，超过 550 时按最多 100 条的有界后台 trim 收敛。

## Entry 身份与时间排序

普通 Web 新帖目前不使用 Flake 生成 Entry ID：

```text
entry UUID = 随机 UUIDv4
entry Date = 服务端当前时间（RFC3339，秒精度）
```

Twitter/股票等导入 Entry 也主要使用基于外部稳定标识的 UUIDv5；Archive 保留输入的
历史 UUID。UUIDv5 是哈希，字节顺序不保留时间，不能替代 EntryIndex 的时间字段。

Web 评论同样使用随机 UUIDv4。UUID 与时间相互独立，因此同一用户同秒发布多条 Entry
或 Comment 仍具有不同身份；排序继续由各索引中的时间字段决定。

## Flake 的两种用法

### 实时唯一 ID

`Store` 持有一个长期存活的 `idGen`。`Store.NextId()` 复用该 generator，同一毫秒内
sequence 正常递增，并由 mutex 串行：

```text
time T | worker W | sequence 0
time T | worker W | sequence 1
time T | worker W | sequence 2
```

生产代码主要用它生成 JobFeed/JobRunning key。这是 Flake 的正常用途：实时、时间向
前、每个 Store/worker 一个 generator；不要求进程级全局 singleton。

### 历史时间排序 token

`TimeTravelId(t)` 和 `TimeTravelReverseId(t)` 每次创建临时 generator。相同输入时间
总是得到相同结果，sequence 每次都是 0：

```text
TimeTravelReverseId(T) -> new generator -> sequence 0
TimeTravelReverseId(T) -> new generator -> sequence 0
```

它实际是确定性的排序 token，不是唯一 ID。简单改成 singleton 不可靠：Archive/rebuild
按任意历史顺序输入时间，generator 会遇到时钟回拨；重启后 sequence 也无法恢复。

## EntryIndex

### 当前格式

与 TimelineIndex 同构：

```text
key   = T(4) | owner UUID(16) | reverse Unix ms(8) | entry UUID(16)
value = 空
```

reverse Unix ms 取 Entry.Date，前向扫描仍得 newest → oldest；entry UUID 后缀保证唯一
与幂等：重复写同一 Entry 覆盖同一行，同秒不同 Entry 共存。value 为空，读取时按 key
后缀重建 canonical Entry key。正文永不冗余进索引：fanout 按 follower 复制索引行，
冗余会产生写放大。Feed 读取的 index scan + N 次 Entry 点查（N+1）是既定设计，不是
待优化项。

direct feed cursor 编码完整 24 B 位置（reverse ms + entry UUID），与 timeline cursor
同构；同秒多帖的分页边界由 UUID 后缀消歧。

### 历史格式

早期格式为 `T | owner UUID | reverse Flake(16)`（36 B，value 为 canonical Entry key），
后追加 canonical Entry key 后缀变为 56 B。Flake 的 worker/sequence 对派生索引不贡献
唯一性，value 与 key 后缀冗余。两种旧格式的一次性转换器只保留在 `v2.2.0` tag；当前
版本只支持用 `rebuild_entry_index` 从 canonical Entry 源数据清空重建。

## Entry 写入和读取路径

```text
PostEntry
  ├── one bounded atomic batch
  │   ├── Entry record
  │   ├── author direct EntryIndex
  │   ├── target feed direct EntryIndex（若不同）
  │   └── 请求携带的 Like/Comment 独立行
  ├── Home activity timeline 初始化（batch 外，无上限）
  └── Bleve body index
```

fanout 不进入 Entry 主 batch，避免 follower 数量无限扩大一次同步 commit；错误必须返回，
可用 `audit_store` 和 `rebuild_timeline` 修复派生 timeline。

```text
FetchFeed(cursor mode)
  ├── 由 feed UUID 重建固定 EntryIndex prefix
  ├── cursor 只恢复该 prefix 下的索引位置
  ├── ForwardScan / Next 取得每页 index value
  ├── N 次 Entry Get
  ├── 两次 interaction prefix scan / Entry
  └── profile hydration + formatting
```

Profile/Group direct Feed 的年度计数与导航边界是 Meta 中的可重建快照，完整编码与失效规则见
[feed_archive.md](feed_archive.md)。它不改变 EntryIndex 排序或 cursor 契约。

profile/timeline/public 使用 cursor；这些 Feed 的匿名旧 `?start=N` URL 由 ffweb 直接 302
到第一页，登录用户仅可读取一次 legacy offset 页，随后通过响应的 `NextCursor` 继续。
Search/Tag 继续使用 Start/PageSize。cursor 不是 Entry UUID，不得脱离当前 feed prefix 解释。

## Home activity timeline

完整契约见 [timeline.md](timeline.md)。TimelineIndex 使用 44 B raw key 和空 value，
TimelinePosition 提供 `(viewer, entry) -> activity_at` 的反向定位；两张派生表共同支持
前向时间排序、稳定 cursor 和 O(1) bump。新表仍在发帖与互动时执行 O(followers)
fanout；它解决排序和生命周期问题，不消除写放大。

## 原子性与并发边界

- Entry record 与 author/target direct index 同 batch；timeline fanout 独立。
- DeleteEntry 原子删除 Entry、direct index、Like、Comment；不枚举 timeline viewer。
- activity timeline 对已删除 Entry 采用读路径懒删，不在 DeleteEntry 中增加无上限 fanout。
- Entry 创建/编辑/删除持 `entryLifecycleMu` 写锁；Like/Comment 持读锁，互动可彼此并发，
  但不能与 Entry 删除并发产生 orphan 行。
- Store 的同步写开关最终必须选择 Pebble `Sync` 或 `NoSync`，不能只改上层状态。

## 运维与迁移原则

application schema 使用独立 marker：

```text
TableMeta | "db-schema/version" -> uint32 big-endian
```

当前版本为 `1`。它证明应用数据已通过当前结构验证，与 Pebble
`FormatMajorVersion` 完全无关。非空数据库不得自动补 marker；空库可由 ffdb 首次启动初始化。
非空 missing/older/future/malformed marker 均拒绝启动；`v2.2.0` 是最后一个允许未盖章
数据库启动、并包含一次性迁移写入器的版本。
marker 只证明持久化编码和一次性迁移边界，不承诺 timeline、graph、Group admin、Notification、
Task 等运行时派生关系永远没有 drift；这些差异由 audit/rebuild 单独报告和修复。

- schema 升级必须停服、备份、先按用户和小上限 dry-run，再全量执行及 audit。
- `rebuild_entry_index` 从 Entry 恢复 direct index；`rebuild_timeline` 从 Entry、互动和当前
  社交图恢复 Home。全量重建还会清除旧 timeline 与 orphan EntryIndex 行。
- 升级后不支持旧程序打开、Pebble v1 或降级写入。
- TimelineIndex/TimelinePosition（含 public timeline）和 Bleve 是派生数据；Profile、OAuth、Entry、互动和社交边
  才是恢复时不可丢失的源数据。
