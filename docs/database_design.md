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
    ├── TimelineIndex / TimelinePosition
    ├── public FeedIndex cache
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
这不是受支持的第二种编码。升级时必须先用 `migrate_entry_keys` 修复源 Entry，再用
`rebuild_entry_index` 丢弃并重建派生索引；运行时和 TimelineIndex 不兼容字符串 key。

## 主要表与 key 设计

`T` 表示 4 字节 table prefix，`U` 表示 16 字节 UUID。

| Prefix | 表 | Key（省略 value 中的 protobuf 细节） | Value | 性质 |
| ---: | --- | --- | --- | --- |
| 0 | Meta | 保留 | — | 历史前缀 |
| 1 | Feed | 保留，当前无运行时表实例 | — | 历史前缀 |
| 2 | Feedinfo | `T + feed UUID` | `pb.Feedinfo` | 历史/迁移 metadata |
| 3 | Entry | `T + entry UUID` | `pb.Entry`，不再内嵌 canonical Like/Comment | 源数据 |
| 4 | UserMap | `T + current profile id` | 16 B profile UUID | 源映射 |
| 5 | EntryIndex | 见下一节 | canonical Entry key | Profile/target feed direct 索引 |
| 6 | Tweet | `T + tweet key` | `pb.Tweet` | 导入数据 |
| 7 | UserRenameMap | `T + old profile id` | 16 B profile UUID | soft rename metadata |
| 100 | Profile | `T + profile UUID` | `pb.Profile` | 源数据 |
| 101 | Service | `T + profile UUID + service id` | `pb.Service` | 源数据 |
| 102 | Follow | `T + follower UUID + feed UUID` | `"1"` | 正向社交边 |
| 103 | Follower | `T + feed UUID + follower UUID` | `"1"` | 反向社交边 |
| 104 | OAuth | `T + provider:user_id` | `pb.OAuthUser` | 稳定上游身份 |
| 105 | File | 保留，当前无运行时读写 | — | 预留前缀 |
| 106 | Like | `T + entry UUID + actor UUID` | `pb.Like` | 源数据、天然幂等 |
| 107 | Comment | `T + entry UUID + comment UUID` | `pb.Comment` | 源数据 |
| 108 | TimelineIndex | `T + viewer UUID + reverse Unix ms + entry UUID` | 空 | Home 排序派生数据 |
| 109 | TimelinePosition | `T + viewer UUID + entry UUID` | Unix ms（8 B big-endian） | Home 位置派生数据 |
| 200 | JobFeed | `T + Flake ID` | job protobuf | queued job |
| 201 | JobRunning | `T + Flake ID` | job protobuf | claimed job |
| 202 | JobHistory | `T + target id` | `pb.FeedJob` | 历史记录 |
| 300–303 | Config/Topic/Stock/KLine | 表专用 key | 各领域编码 | 独立领域数据 |

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

## Entry 身份与时间排序

普通 Web 新帖目前不使用 Flake 生成 Entry ID：

```text
entry UUID = UUIDv5(namespaceURL, profile UUID + "/" + RFC3339 time)
entry Date = 同一个 RFC3339 time
```

Twitter/股票等导入 Entry 也主要使用基于外部稳定标识的 UUIDv5；Archive 保留输入的
历史 UUID。UUIDv5 是哈希，字节顺序不保留时间，不能替代 EntryIndex 的时间字段。

当前 Web 生成式只精确到秒，同一 profile 同秒发两帖会得到相同 UUID；这是独立的待修
正确性问题，不应通过 EntryIndex 掩盖。

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

### 历史格式及缺陷

```text
key   = EntryIndex T | owner/feed UUID | reverse Flake
value = canonical Entry key
```

确定性 key 的原意合理：重复 `PutEntry`、编辑、Archive 重跑或旧式互动重写 Entry 时，
应覆盖同一索引行而不是产生重复行。但它错误地把 `(feed, second)` 当成 Entry identity。
同一 feed 同秒两条 Entry 的 reverse Flake 完全相同，后写会覆盖先写。

### 当前分支的最小修正

```text
key   = T(4) | owner UUID(16) | reverse Flake(16) | canonical Entry key(20)
value = canonical Entry key(20)
```

Entry identity 后缀使重复写同一 Entry 仍然幂等，并让同秒不同 Entry 共存。direct feed
cursor 按产品取舍只编码 16 B reverse Flake，不携带 Entry identity；同一用户同秒发帖的
分页边界不作保证。前向扫描仍按 reverse time 得到 newest → oldest，算法复杂度不变。

完整 canonical Entry key 中 4 字节 `TableEntry` 是固定冗余；只追加 Entry UUID 可将
key 从 56 B 缩到 52 B。

### 更干净的目标格式（尚未实施）

引入 Entry UUID 后，Flake 的 worker/sequence 对这个派生索引不再贡献唯一性。若本轮
决定继续做一次离线 schema break，建议直接使用：

```text
key   = T(4) | owner UUID(16) | reverse timestamp(8) | entry UUID(16)
value = canonical Entry key(20)
```

共 44 B。职责明确：reverse timestamp 排序，Entry UUID 唯一且作为同时间 tie-break。
这仍保持 `First/SeekGE + Next` 的前向扫描优势。不能只保留 Entry UUID，因为现有 UUIDv4/
UUIDv5 不具备时间字节序，Archive 的导入时间也不等于 Entry.Date。

目标格式只有在迁移与代码同时调整后才成立；当前数据库和 cursor 必须按“当前分支的
最小修正”解释。

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

profile/timeline 使用 cursor；旧客户端、public cache 和 search 保持 Start/PageSize。cursor
不是 Entry UUID，不得脱离当前 feed prefix 解释。

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
- queued → running job claim 使用 `jobMu`，并在同一 batch 中完成。
- Store 的同步写开关最终必须选择 Pebble `Sync` 或 `NoSync`，不能只改上层状态。

## 运维与迁移原则

- schema 升级必须停服、备份、先按用户和小上限 dry-run，再全量执行及 audit。
- `rebuild_entry_index` 从 Entry 恢复 direct index；`rebuild_timeline` 从 Entry、互动和当前
  社交图恢复 Home。全量重建还会清除旧 timeline 与 orphan EntryIndex 行。
- 升级后不支持旧程序打开、Pebble v1 或降级写入。
- Public FeedIndex、TimelineIndex/TimelinePosition 和 Bleve 是派生数据；Profile、OAuth、Entry、互动和社交边
  才是恢复时不可丢失的源数据。
