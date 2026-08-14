# 通用 Task 队列设计

`docs/service_aggregation.md` 的审计结论是旧 FeedJob 队列不可逐项修补；本文给出
替代它的通用队列设计，并修正该文的一处结论：RSS 抓取的**执行**应走通用队列
（调度状态仍留 SubscriptionState），而不是进程内自造 dispatcher。

## 定位与前提

- Pebble 单写者（文件锁）⇒ ffdb 进程是队列的唯一服务端，自身即 broker；不引入
  Redis/RabbitMQ 等外部组件。
- worker = gRPC 客户端。进程内 goroutine pool、同机 Python crawler、CLI 工具对
  队列完全同构。ffdb 仅监听 loopback 的红线不变，worker 天然同机。
- 进程内嵌 worker 只是 `ClaimTasks` 的一个直连调用方，单进程形态不为通用性付
  额外复杂度。

## 现状：保留与废弃

保留（方向正确的部分）：

- `jobMu` 串行化 claim、queued→running 在同一 Pebble batch 提交
  （server/job.go:116-146，AGENTS 数据不变量）。

废弃（审计十宗罪，详见 service_aggregation.md）：

- FeedJob 无类型字段、载荷是含 token 的快照、无租约、入队无去重、history 写
  空 key、claim 覆盖 Created、`FinishJob` 信任 worker 传入的 key。

兼容边界（AGENTS 契约）：

- `EnqueJob`/`GetFeedJob`/`FinishJob` 原样保留，标 deprecated；旧表
  200/201/202 停止新写入，存量数据留待 M3 评估清理。纠错只能新增兼容 RPC。
- **命名**：新系统一律用 Task（pb.Task / TableTask* / EnqueueTask），与受保护
  的 legacy Job 符号（FeedJob / TableJobFeed / jobMu）消歧——两套系统长期
  共存，grep 必须能分清。

## 语义约定

- **at-least-once + 幂等 handler**。副作用（HTTP 抓取、R2 上传）进不了 Pebble
  事务，exactly-once 不可达成，不做此承诺。handler 必须幂等：同 key 覆盖写、
  重复 mirror 无害。
- **pull/claim，不 push**。worker 有容量才 claim，背压零成本；`types[]` 订阅
  即路由，不引入命名 queue 层。
- **延迟任务原生支持**：`run_at` 是就绪索引的排序键，定时/退避重试同构。

## 代码结构

通用组件，独立顶层包 `task/`，不 import server：

- `task/`：Queue 引擎——Enqueue/Claim/Ack/Nack/Heartbeat 五个领域方法、taskMu、
  key 编解码、backoff 注册表、`ReapLoop(ctx)`。依赖仅 store + model + pb，
  引擎可脱离 ApiServer 单测。
- `model/types.go`：只加五行表注册（203-207）——表号是全局命名空间，注册表
  必须保持集中完整。
- `server/task.go`：薄 gRPC 适配——参数校验后委托 `s.tasks`（*task.Queue）；
  reaper 与进程内 worker pool 的生命周期挂既有
  `beginBackgroundJob`/`wg`/`Shutdown`。worker pool 是消费者，不在引擎内。
- `cli/tools`：经 task 包做审计与 replay-dead，与 RPC 走同一条状态迁移路径。

依赖方向 `task → {store, model, pb}`、`server → task`，无环。outbox 生产者
用 `EnqueueBatch(batch, task)` 变体把入队塞进业务写的同一 batch。

## 存储（五个新表，纯新增）

```text
TableTask       = 203
key   = prefix(4) | task_id(16, flake)
value = pb.Task                                   // 主记录

TableTaskReady  = 204                             // 就绪索引，时间序即出队序
key   = prefix(4) | run_at(8, big-endian) | task_id(16)
value = nil

TableTaskLease  = 205                             // 租约索引，reaper 扫描用
key   = prefix(4) | lease_until(8, big-endian) | task_id(16)
value = nil

TableTaskIdem   = 206                             // 入队去重
key   = prefix(4) | idempotency_key(变长)
value = task_id(16)

TableTaskDone   = 207                             // 完成/死信，有界
key   = prefix(4) | finished_at(8, big-endian) | task_id(16)
value = pb.TaskResult{ type, status(OK|DEAD), attempts, last_error(截断),
        created, finished }
```

- task_id 用 flake（`s.mdb.NextId()`），与现有 key 实践一致，天然时间有序。
- ready/lease 索引 value 为空，状态全在主记录；done 表用 RangeDelete 前缀裁剪
  保留最近 N 条。
- 表号与编码实施时按流程同步 AGENTS.md 契约清单。

## Task 模型

```proto
message Task {
  string id = 1;               // flake hex
  string type = 2;             // "rss.fetch" / "media.mirror" / "twitter.crawl"
  bytes  payload = 3;          // 只放引用(UUID)+小参数；禁快照、禁 token
  string idempotency_key = 4;  // 空 = 不去重
  int64  run_at = 5;
  int32  attempts = 6;
  int32  max_attempts = 7;
  int64  lease_until = 8;
  string leased_by = 9;
  uint64 lease_epoch = 10;     // fencing token
  string last_error = 11;      // 截断
  int64  created = 12;         // claim 不再覆盖
  int64  updated = 13;
}
```

payload 约定：proto bytes，按 type 独立演进；只放引用（entry_uuid、feed_uuid）
与小参数（cursor），handler 执行时现查现取——修掉"快照过期"与"token 落盘"
两条罪。

## RPC（新增五个，旧三个保留）

```proto
rpc EnqueueTask(Task) returns (EnqueueResponse);      // idem 命中返回已有 task
                                                      // EnqueueResponse = {task, already_exists}
rpc ClaimTasks(ClaimRequest) returns (ClaimResponse); // {worker_id, types[], max, lease_seconds}
rpc AckTask(AckRequest) returns (google.protobuf.Empty);     // {worker_id, task_id, epoch}
rpc NackTask(NackRequest) returns (google.protobuf.Empty);   // {worker_id, task_id, epoch, error}
rpc HeartbeatTask(HeartbeatRequest) returns (Task);          // {worker_id, task_id, epoch, extend_seconds}
```

空队列不做服务端 long-poll：worker 端指数退避（1s 起、30s 封顶），同机 IPC
成本足够低——修掉"错误驱动 5s 轮询"。

## 状态机与原子迁移

READY → INFLIGHT → done(OK)；INFLIGHT → READY（nack / 租约过期）；
INFLIGHT → done(DEAD)（attempts 用尽）。

- **enqueue**：主记录 + ready key + idem，一条 batch。进程内生产者走 outbox——
  塞进业务写的同一 batch（如 PutEntry + mirror task），入队与状态变更原子。
- **claim**（taskMu 内，单 batch；沿用 jobMu 的串行化模式）：扫 ready 取
  `run_at ≤ now` 且 type 匹配的前 max 条 → 主记录改 INFLIGHT、attempts+1、
  lease_epoch+1、lease_until/leased_by；删 ready、写 lease。
- **ack**（校验 worker+epoch）：删主记录、删 idem、删 lease、写 done(OK)，一条
  batch。
- **nack**（校验 fencing）：未满 → 主记录 `run_at = now + backoff(attempts)`、
  写 ready、删 lease；已满 → 删主记录/idem/lease、写 done(DEAD)。
- backoff：`min(base·2^attempts, cap)` ±20% jitter，base/cap/max_attempts 按
  type 注册表配置。

## 多 worker 正确性：租约与 fencing

- claim 不删 task，只推 `lease_until`；worker 崩溃后由 reaper 回收——修"无租约、
  task 永久卡 running"。
- `lease_epoch` 每次 claim 递增；Ack/Nack/Heartbeat 必须匹配
  `(task_id, worker, epoch)`，僵尸 worker 的迟到写回收 FailedPrecondition。
- 僵尸的副作用可能已发生 ⇒ 幂等 handler 兜底（见语义约定）。
- 同一实体的互斥靠幂等键：同一 feed 同一时刻最多存在一个 `rss.fetch` task；
  不引入 per-entity 锁表。

## Reaper、关停与重启

- reaper 为 ffdb 内 goroutine（挂既有 `beginBackgroundJob`/`wg`/`Shutdown`
  设施），60s tick，扫 `lease_until < now`，按 nack 路径重派或入死信
  （reason=lease_lost）。
- 关停：GracefulStop 排干请求后停止接受新 claim，短等在飞 ack；未 ack 的靠
  租约过期回收。kill -9 安全，重启后 reaper 自愈，无任何恢复流程。

## 观测与运维

- ready 深度 / 最老 task 年龄（lag）：扫 ready 前缀即得。
- done 表给出错率与死信清单；`tools -c tasks`：list ready/inflight/dead、
  replay-dead（done→ready 单条重放）。
- 日志只记 type/task_id/耗时/错误摘要；payload 模型层面禁 secret，任何级别不
  记正文。

## 任务类型映射

- `rss.fetch`：调度器（service_aggregation.md）到期入队，
  idem=`fetch:<feed_uuid>`；worker 做条件 GET/解析/PostEntry/推进
  SubscriptionState。分工：短期传输错误走队列 nack backoff；源级长期状态
  （etag、退避、失败计数）留订阅表。每 host 串行（抓取 politeness）由 handler
  内 per-host 锁实现，不进队列层。
- `media.mirror`（将来异步化时）：outbox 随 PutEntry 同 batch 入队，
  idem=`mirror:<entry_uuid>`。当前 mirrorMedia 是同步契约（AGENTS），不动。
- `twitter.crawl`（将来）：Python crawler 改 `ClaimTasks(["twitter.crawl"])`
  闭环，替换硬编码 `yinhm` 的 Go worker（cli/cmd/twitter.go:97）。
- timeline rebuild **不接队列**：on-demand + singleflight + 并发上限已是最简
  形态，可靠投递对它无意义。

## 分阶段实施

- M1：pb.Task + 五表 + `task/` 包引擎 + server 薄适配 + 进程内 worker pool；
  RSS 迁入。落位见"代码结构"一节。
- M2：reaper + done 表裁剪 + `tools -c tasks`（cli/tools 经 task 包）。
- M3：外部 worker 样例（Python crawler 闭环）；评估旧表 200-202 数据清理与旧
  RPC 退役方案（按 AGENTS 契约需独立设计）。

## 测试清单

- task 包（脱离 ApiServer）：五表 key/value 编码、边界（空 idem key、零
  task_id、变长 idem 前缀冲突）；并发 claim 同一 task 仅一人成功；过期 epoch
  的 Ack/Nack 被拒；租约过期重派；backoff 推进与封顶；attempts 用尽入 DEAD；
  idem 重复入队返回已有；outbox 与业务写同 batch 原子。全部过 `-race`。
- server：五个 RPC 的校验与委托、GracefulStop 排干、reaper 生命周期。
