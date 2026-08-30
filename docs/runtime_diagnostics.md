# 运行时诊断

ffdb 提供两组只读诊断，均通过仅监听 loopback 的 gRPC `Command` 调用。它们不注册
HTTP 路由，不开放 pprof，也不读取配置、凭据、Entry/Comment 正文或 Task payload。

## 内存与进程状态

在 ffdb 所在主机执行：

```bash
cli --address 127.0.0.1:8901 inspect-runtime
cli --address 127.0.0.1:8901 inspect-runtime --json
```

基础快照为 O(1) 操作，不扫描数据库，也不触发 GC、Flush 或 cache eviction。主要字段：

- `process.rss_bytes`：操作系统观察到的当前常驻内存；它包含 Go、Pebble 手工分配、
  allocator 保留页等，不能与 Go heap 直接等同。
- `go.heap_alloc_bytes`：当前存活的 Go heap；`heap_sys/idle/released` 用于区分活对象与
  Go runtime 高水位保留。
- `go.memory_limit_bytes`：当前 `GOMEMLIMIT`；默认无限制。
- `pebble.block_cache_bytes/block_cache_limit_bytes`：Pebble block cache 当前使用量与
  固定上限。cache 接近上限是正常缓存行为，不等于泄漏。
- `pebble.memtable_bytes`：当前 memtable 分配。
- `zombie_memtable_bytes`、`zombie_table_bytes`、`open_table_iterators`、`open_snapshots`：
  持续增长时优先排查未关闭 iterator/snapshot。
- `compaction_debt_bytes`：待压缩估算；与正在运行的 compaction/flush 一起判断写入压力。
- `realtime.subscribers/dropped_hints`：ffweb stream 数量和进程启动以来丢弃的弱一致 hint。

一次结果只能说明组成，不能说明趋势。调查内存上涨时至少在空闲、流量高峰、流量停止后各保存
一次 `--json` 输出，对比 RSS、Go HeapAlloc、Pebble cache 和 zombie 指标：

- HeapAlloc 回落而 RSS 不回落通常是 runtime/allocator 高水位，不应先判为存活对象泄漏；
- block cache 稳定在配置上限是有界行为；
- zombie 或 goroutine/FD 持续增长才是资源生命周期异常的强信号；
- RSS 明显高于 Go heap、Pebble cache 和 memtable 之和时，再检查搜索索引、C allocator 与
  mmap/page cache。

## 一次性 Go heap profile

只有基础快照不能定位 Go 活对象归属时才执行：

```bash
cli --address 127.0.0.1:8901 inspect-runtime --heap-profile
```

服务端将 profile 写入固定目录 `/tmp/ffdb-diagnostics`，目录权限 `0700`、文件权限 `0600`。
客户端不能指定路径；同一时间只允许一个 capture，最多保留 3 份。达到上限后命令拒绝继续，
操作者应先复制必要证据，再显式删除旧文件。profile 可能包含进程内对象内容，不得上传到公开
位置。该操作不会自动上传、轮换、强制 GC 或删除证据。

分析示例：

```bash
go tool pprof /path/to/ffdb /tmp/ffdb-diagnostics/heap-*.pprof
```

## 后台系统摘要

```bash
cli --address 127.0.0.1:8901 inspect-system
cli --address 127.0.0.1:8901 inspect-system --json
```

输出包括：

- Task `ready/inflight/dead`、最老 ready age；
- Service `active/degraded/dead/due`；
- Feed API key `active/revoked` 聚合数量（不含 Feed UUID、key ID 或 secret）；
- Home timeline maintenance 并发占用和失败退避数；
- Notification trim、Public timeline trim/bump 状态。

Task、Service 与 Feed API key 统计是流式扫描，单表最多检查 100,000 行；达到预算时返回
`truncated=true`，此时计数是下界，不能当作精确总量。诊断保持常数内存，但扫描仍占用数据库
读取资源，不应被监控系统高频轮询；建议只在人工排障或低频巡检时调用。损坏记录会使命令明确
失败，不会静默跳过并给出虚假健康状态。

## 边界

- 两个接口依赖 ffdb 只监听 loopback 的部署契约，不得经 nginx 或公网转发。
- 诊断只观察，不负责修复；不得用强制 GC、主动清空 cache 或缩小 cache 掩盖未定位问题。
- 需要调整 Pebble cache、memtable、`GOMEMLIMIT` 或 systemd `MemoryHigh/MemoryMax` 时，先保留
  调整前后的 JSON 快照并分别验证命中率、延迟与 RSS。
