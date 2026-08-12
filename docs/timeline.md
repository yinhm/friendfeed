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

当前 cursor 编解码根据传入 prefix 的 table number 区分 TimelineIndex（24 B position）和
direct EntryIndex（16 B reverse Flake）。同一用户同秒发帖的分页边界不作保证。这是显式
表格式选择，不是从任意内容猜测编码。

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
