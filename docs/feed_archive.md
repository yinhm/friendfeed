# Feed archive statistics

Profile Feed 与 Group Feed 使用同一套可重建年度统计。它只描述 direct EntryIndex，不适用于
Home、Public、Search、Likes 或 Comments。

## 持久化格式

权威数据仍是 Entry 与 direct EntryIndex。统计快照存于 Meta：

```text
key   = TableMeta | "feed-archive/v1/" | raw feed UUID (16 B)
value = pb.FeedArchiveStats

dirty key   = TableMeta | "feed-archive-dirty/v1/" | raw feed UUID (16 B)
dirty value = first dirty Unix milliseconds (8 B big-endian)
```

key 前缀保持 `v1` 不变；快照语义的演进只提升 `FeedArchiveStats.version`。旧版本快照读取即
失败，由登录访问或离线重建自动覆盖，不遗留第二套 key。

`FeedArchiveStats` 包含版本、Feed 总 Entry 数和按年份倒序排列的 `FeedArchiveYear`。每年记录：

- `year`：由 EntryIndex 中的 publish time 计算；
- `entry_count`：该年可解析、且 canonical Entry 仍存在的索引行数；
- `cursor`：上一年（更近年份）最后一行的 24 B position，经 Base58 编码；最新年份没有更新
  的边界，cursor 为空。

cursor 仍是位置而非 Entry UUID。请求 `/feed/:id?cursor=...` 时，既有分页逻辑跳过锚点，
因此把目标年份的最新一条 Entry 放在页面第一行；该语义不依赖页面大小。最新年份的链接
不带 cursor，即 Feed 首页。年份之间没有 Entry 的年份不会生成空行。

## 生命周期

- 新建 Entry 时，作者 Profile Feed 与不同的目标 Group Feed 在同一 Entry batch 中保留快照并写
  dirty marker；删除 Entry 同样标记两类快照；
- marker 记录首次 dirty 时间，后续 mutation 不刷新它，避免活跃 Feed 无限推迟维护；
- 重复归档同一 Entry 不改变 direct Feed，不标记快照；
- 登录用户每次读取 direct Feed 都检查 marker；已有快照继续返回，marker 满 7 天后才幂等入队
  `feed.archive.rebuild`；
- 缺失、损坏或版本不符的快照没有 stale 数据可用，登录用户读取时立即入队首次构建；
- 匿名读取不访问快照、不检查 marker，也不触发 rebuild；Archive 只服务登录用户；
- rebuild 以同一 batch 写新快照并删除 marker；
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

只需清空派生统计并让登录用户访问时按需重建，可停服后执行：

```bash
echo purge_feed_archive | ./tools -to <db-dir> -c purge_feed_archive
```

该命令只删除 `feed-archive/v1/` 快照和 `feed-archive-dirty/v1/` marker，不触碰
其他 Meta 数据。缺失快照会在登录用户访问 direct Feed 时入队重建；匿名访问不触发维护。
