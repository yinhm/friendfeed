# Feed archive statistics

Profile Feed 与 Group Feed 使用同一套可重建年度统计。它只描述 direct EntryIndex，不适用于
Home、Public、Search、Likes 或 Comments。

## 持久化格式

权威数据仍是 Entry 与 direct EntryIndex。统计快照存于 Meta：

```text
key   = TableMeta | "feed-archive/v1/" | raw feed UUID (16 B)
value = pb.FeedArchiveStats
```

`FeedArchiveStats` 包含版本、Feed 总 Entry 数和按年份倒序排列的 `FeedArchiveYear`。每年记录：

- `year`：由 EntryIndex 中的 publish time 计算；
- `entry_count`：该年可解析、且 canonical Entry 仍存在的索引行数；
- `cursor`：该年最旧（最后）Entry 之前那条较新索引行的 24 B position，经 Base58 编码；若该条
  Entry 本身就是全 Feed 第一条则为空。

cursor 仍是位置而非 Entry UUID。请求 `/feed/:id?cursor=...` 时，既有分页逻辑跳过锚点，
因此把目标年份的最后一条 Entry 放在页面第一行；该语义不依赖页面大小。年份之间没有 Entry
的年份不会生成空行。

## 生命周期

- 新建 Entry 时，作者 Profile Feed 与不同的目标 Group Feed 在同一 Entry batch 中删除旧快照；
- 删除 Entry 同样使两类快照失效；
- 重复归档同一 Entry 不改变 direct Feed，不使快照失效；
- 登录用户读取 direct Feed 时，有快照才随 `pb.Feed.archive` 返回；缺失或损坏时仅幂等入队
  `feed.archive.rebuild`，本次 Feed 请求正常返回且不显示 Archive；
- 匿名读取既不返回统计，也不触发 rebuild；
- task 的扫描与快照发布持有 Entry lifecycle 读锁，不能覆盖并发 Entry mutation 留下的失效状态。

Task handler 流式扫描单个 Feed，内存只随年份数增长。EntryIndex orphan 不计入统计；它仍由既有
读路径、audit 与 `rebuild_entry_index` 清理。

## 页面

登录用户访问 `/feed/:id` 时，sidebar 在快照存在后显示：

- `All (总数)`；
- 每年的 `年份 (数量)` 链接。

快照尚未生成时不显示占位、loading 或错误信息。Private Feed/Group 先通过既有可见性检查，统计
不会成为权限旁路。

## 离线重建

先对单个 Feed dry-run：

```bash
./tools -to <db-dir> -c rebuild_feed_archive -user yinhm -dry-run
./tools -to <db-dir> -c rebuild_feed_archive -user yinhm
```

再执行全量：

```bash
./tools -to <db-dir> -c rebuild_feed_archive -dry-run
./tools -to <db-dir> -c rebuild_feed_archive
```

全量模式逐个扫描未删除的 user/group Profile，不收集全库 Entry 或 key。
