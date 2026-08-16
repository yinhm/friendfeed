# 通用 Task 队列设计

本文定义 FeedJob 的后继系统。目标不是复刻消息中间件，而是在 ffdb 现有的单机
Pebble 架构内提供可靠、可恢复、可审计的后台执行能力。Service 抓取是第一个使用方；
调度状态仍属于 `ServiceState`，Task 只承载一次到期执行。

## 决策与边界

- ffdb 是 Pebble 唯一写者，也是唯一 broker；不引入 Redis/RabbitMQ。
- worker 通过 loopback gRPC pull task；进程内 worker 使用同一 Queue API，不另造
  dispatcher。
- 交付语义是 **at-least-once**。外部 HTTP、R2 等副作用不能加入 Pebble 事务，
  handler 必须幂等。
- Task payload 只保存稳定引用和小参数，不保存 Profile/Service 快照、token、正文或
  Cookie；handler 执行时读取最新数据。
- READY/INFLIGHT 的权威状态在 Task 主记录中；Ready/Lease/Done 均为索引或历史，
  不能反向取代主记录。
- 旧 `EnqueJob`、`GetFeedJob`、`FinishJob` 和表 200-202 按兼容契约保留并标记
  deprecated；新系统统一使用 Task 命名，不复用 FeedJob 符号。
- timeline rebuild 已有 singleflight、并发上限和派生缓存收敛，不接 Task 队列。
- `mirrorMedia` 仍是 `ArchiveFeed` 的同步契约；本设计不顺带异步化。

通用队列的成立前提是 RSS 之后确有外部 worker（例如 Python crawler）。若最终只有
进程内 RSS 抓取，应在 M2 后重新评估是否继续暴露外部 worker RPC，而不是为了假想
扩展提前实现所有消费者。

## 包与依赖

```text
task/                 Queue 状态机、key codec、类型注册、reaper
  ↓
store + model + pb    Pebble、表注册、protobuf

server/task.go        参数校验、gRPC 适配、生命周期
  ↓
task.Queue

cli/tools             inspect/dead replay，经 task.Queue 改状态
```

`task` 不 import `server`。reaper 和进程内 worker 的启动/停止由 ApiServer 挂入既有
`beginBackgroundJob`、`wg`、`Shutdown` 生命周期；Queue 本身不拥有无约束 goroutine。

## 持久化结构

表号 203-207 是全局持久化契约，实施时同步 `model/types.go`、AGENTS.md、audit 和
数据库设计文档。

```text
TableTask = 203
key   = prefix(4) | task_id(16-byte raw flake)
value = pb.Task                         // 权威主记录

TableTaskReady = 204
key   = prefix(4) | type_len(1) | type | run_at_ms(8 BE) | task_id(16)
value = nil                             // READY 派生索引

TableTaskLease = 205
key   = prefix(4) | lease_until_ms(8 BE) | task_id(16)
value = nil                             // INFLIGHT 派生索引

TableTaskIdem = 206
key   = prefix(4) | sha256(type + NUL + idempotency_key)(32)
value = task_id(16)                     // 活跃任务去重

TableTaskDone = 207
key   = prefix(4) | finished_at_ms(8 BE) | task_id(16)
value = pb.TaskCompletion               // 有界完成/死信历史
```

选择说明：

- task id 使用现有 singleton flake generator。数据库 key 存 16-byte raw flake；RPC
  中使用不带表前缀的 32 字符小写 hex，不混用 UUID 或完整 Pebble key。
- 所有时间是非负 Unix milliseconds、UTC，以 `uint64` big-endian 编码；拒绝 epoch
  前和溢出值。
- Ready 把 type 放在时间前，使按类型 claim 只扫描相关前缀。订阅多个 type 时，
  Queue 在**单次 claim 调用内**为每个 type 创建一个迭代器并做小规模 k-way merge，
  取全局最早的 due task；返回前关闭全部迭代器，不跨调用持有，也不扫描无关类型
  形成队头阻塞。
- type 仅允许注册表中的 ASCII 标识，最长 64 字节；idempotency key 最长 256 字节，
  payload 默认上限 64 KiB。哈希 idem key 固定数据库 key 长度，原字符串保留在 Task
  供审计。
- Done 保存完整 Task，而非只有结果摘要，否则 DEAD 无法 replay。
- Done 按完成时间自然排序；清理使用 `[table prefix, cutoff key)` RangeDelete，不能
  把“RangeDelete 前缀”误解为删除整张表。

## protobuf 模型

字段号实施时只追加，不复用；下列名称表达目标语义，不要求照抄序号。

```proto
enum TaskState {
  TASK_STATE_UNSPECIFIED = 0;
  TASK_STATE_READY = 1;
  TASK_STATE_INFLIGHT = 2;
}

enum TaskCompletionStatus {
  TASK_COMPLETION_STATUS_UNSPECIFIED = 0;
  TASK_COMPLETION_STATUS_OK = 1;
  TASK_COMPLETION_STATUS_DEAD = 2;
}

message Task {
  string id = 1;                 // 32-char raw flake hex
  string type = 2;               // service.fetch / feed_service.seed / twitter.crawl
  bytes payload = 3;             // 该 type 的 protobuf，只有引用和小参数
  uint32 payload_version = 4;
  string idempotency_key = 5;
  TaskState state = 6;
  int64 run_at_ms = 7;
  uint32 attempts = 8;           // 已经开始执行的次数
  uint32 max_attempts = 9;
  int64 lease_until_ms = 10;
  string leased_by = 11;
  uint64 lease_epoch = 12;       // fencing token，每次 claim +1
  string last_error = 13;        // UTF-8，截断到固定上限
  int64 created_at_ms = 14;
  int64 updated_at_ms = 15;
  int64 claimed_at_ms = 16;      // 本轮 claim 起点，READY 时为 0
}

message TaskCompletion {
  Task task = 1;                 // replay DEAD 所需的完整定义
  TaskCompletionStatus status = 2;
  string last_error = 3;
  int64 finished_at_ms = 4;
}
```

`max_attempts`、backoff、lease duration 不接受生产者自定义，来自服务端 type registry。
Task 主记录的 state 与索引不一致属于数据损坏，运行路径返回明确错误，audit 给出修复
建议；不能静默猜测状态。若损坏 Task 位于某 type 的队头，后续 claim 会持续明确失败，
包含该 type 的整次多类型 claim 会持续明确失败；由 audit 定位后按本节 runbook 恢复
后恢复消费。选择 loud failure 是为了防止跳过损坏后继续执行造成顺序和状态误判。

## 类型注册表

每个 task type 在服务端注册：

```go
type Definition struct {
    ValidatePayload func([]byte, uint32) error
    MaxAttempts     uint32
    LeaseDuration   time.Duration
    MaxLease        time.Duration
    BackoffBase     time.Duration
    BackoffCap      time.Duration
    Handler         Handler // 仅进程内消费者需要
}
```

未知 type、未知 payload version 或非法 payload 在 enqueue 时拒绝，不能等 worker claim
后才发现。注册表在服务启动时一次性校验：MaxAttempts 必须大于零，LeaseDuration 和
MaxLease 必须为正且 `LeaseDuration <= MaxLease`，backoff base/cap 必须有效；任一配置
非法则拒绝启动。错误摘要、type 和 worker id 也有长度上限。

## RPC API

客户端不能提交完整 Task，避免伪造 attempts、state、lease 或服务端时间字段。

```proto
rpc EnqueueTask(EnqueueTaskRequest) returns (EnqueueTaskResponse);
rpc ClaimTasks(ClaimTasksRequest) returns (ClaimTasksResponse);
rpc CompleteTask(CompleteTaskRequest) returns (google.protobuf.Empty);
rpc FailTask(FailTaskRequest) returns (FailTaskResponse);
rpc RenewTaskLease(RenewTaskLeaseRequest) returns (Task);
```

```proto
message ClaimTasksResponse {
  repeated Task tasks = 1;
}

enum FailTaskOutcome {
  FAIL_TASK_OUTCOME_UNSPECIFIED = 0;
  FAIL_TASK_OUTCOME_RETRY = 1;
  FAIL_TASK_OUTCOME_DEAD = 2;
}

message FailTaskResponse {
  FailTaskOutcome outcome = 1;
  int64 next_run_at_ms = 2; // RETRY 时有效；DEAD 时为 0
}
```

请求语义：

- `EnqueueTaskRequest = {type, payload, payload_version, idempotency_key, run_at_ms}`；
  response 返回服务端构造的 Task 和 `already_exists`。
- `ClaimTasksRequest = {worker_id, types[], max_tasks}`；lease duration 由注册表决定，
  不由 worker 指定。`max_tasks` 有服务端上限。空队列返回空列表和 OK，不用错误表示。
- Complete/Fail/Renew 都必须携带 `{worker_id, task_id, lease_epoch}`。
- Claim 把 `claimed_at_ms` 设为服务端当前时间。Renew 的新截止时间为
  `min(now + LeaseDuration, claimed_at_ms + MaxLease)`；达到硬上限后拒绝继续续租。
  Fail/reap 回到 READY 时清零 claimed_at，下一次 claim 才开始新的执行窗口。因此活着
  但卡住的 worker 也不能无限占住 Task。
- `now >= lease_until_ms` 表示 worker 已丢失租约，即使 reaper 尚未扫描到该 Lease，Renew
  也返回 FailedPrecondition；不能利用 60 秒 reaper 窗口复活过期租约。
- 参数错误用 InvalidArgument；未知 task 用 NotFound；worker/epoch/state 不匹配用
  FailedPrecondition；存储故障用 Internal。
- 不做服务端 long-poll。worker 空取后从 1 秒指数退避到 30 秒并加 jitter；成功 claim
  后重置。

命名使用 Complete/Fail/Renew 而不是 Ack/Nack/Heartbeat，直接表达领域行为，避免
Heartbeat 被误认为 worker 存活探针。

## 状态机与事务

```text
enqueue                   claim
  ┌─────────┐          ┌──────────┐
  │  READY  │─────────▶│ INFLIGHT │
  └─────────┘          └──────────┘
      ▲                  │   │   │
      │ fail/reap retry  │   │   └── attempts exhausted ──▶ Done(DEAD)
      └──────────────────┘   └────── complete ────────────▶ Done(OK)
```

每次迁移都在 `Queue.mu` 内完成读取、校验和单个 Pebble batch 提交：

- enqueue：写 Task(READY)、Ready、可选 Idem。
- claim：只处理 `run_at <= now`；验证 Task 存在、state=READY、type/run_at 与索引
  一致；state→INFLIGHT，attempts+1、epoch+1、claimed_at=now、
  lease_until=now+LeaseDuration，删除 Ready，写 Lease。
- complete：验证 state/worker/epoch；删除 Task、Lease、Idem，写 Done(OK)。
- fail：验证 fencing；仍可重试时 state→READY、清 lease/claimed_at 字段、计算新
  run_at，删除 Lease、写 Ready；attempts 已达上限时删除 Task/Lease/Idem，写
  Done(DEAD)。
- renew：验证 fencing；删除旧 Lease key、更新主记录 lease_until、写新 Lease key。
- reap：扫描到期 Lease，重新读取主记录并核对 state、epoch 和 lease_until；过期索引
  不能回收已续租任务。合法过期任务复用内部 fail 迁移，reason=`lease_lost`。

`attempts` 在 claim 时递增，第一次执行的 attempts=1。第 n 次失败后的退避定义为：

```text
min(base * 2^(n-1), cap) ± 20% jitter
```

因此第一次失败等待 base，不存在 off-by-one。时间源和 jitter source 可注入，保证测试
确定性。

### 幂等键语义

Idem 只保证同一 type 的**活跃 Task**唯一：READY/INFLIGHT 期间重复 enqueue 返回原
Task；完成或 DEAD 后 Idem 被删除，之后允许重新入队。它不是永久业务幂等记录，最终
副作用仍由 handler 的 canonical key 保证幂等。

发现 Idem 指向不存在的 Task 时视为孤儿：普通 enqueue 返回数据损坏错误，不在业务
请求中静默修复；当前 audit 只诊断、不猜测修复。

## 与业务写原子提交（outbox）

不得从已有 `Store.ApplyBatch` callback 内再次调用 Queue.Enqueue，否则会嵌套事务并
造成锁顺序问题。Queue 提供唯一的组合入口：

```go
Queue.EnqueueWith(ctx, specs, func(batch *pebble.Batch) error {
    // 业务 mutation
    return nil
}) ([]*pb.Task, error)
```

固定顺序是 `Queue.mu → Store.ApplyBatch(batchMu)`：Queue 先在锁内读取 Idem、准备 task
id，并在一个本地集合中消解同批重复 spec；随后单个 ApplyBatch 同时写业务 mutation
和 Task/Ready/Idem。callback 不得回调 Queue。普通 Enqueue 是该入口的空业务 callback
封装。

callback 在 Queue.mu 下执行，只允许快速、确定性的 batch mutation；不得做网络 I/O、
等待其他 goroutine、启动事务或执行无界扫描。复杂读取和计算必须在进入 EnqueueWith
前完成，并由业务层处理其结果过期风险。

Idem 命中时仍执行并提交业务 callback，只是不重复创建 Task；response 中对应项返回
已有 Task 并标记 `already_exists`。callback 必须自身满足业务写的幂等约束，不能依赖
“Task 已存在”来跳过 canonical mutation。

这样可以证明：业务写和 task 要么同时可见，要么都不可见；也避免公开一个要求调用方
自行持锁的危险 `EnqueueBatch` API。

## 租约、worker 与幂等 handler

- lease + epoch 只阻止僵尸 worker 修改队列状态，不能阻止它已发出的 HTTP/R2 副作用。
- handler 必须使用稳定业务 key：RSS Entry 使用规范化 item identity；媒体对象按内容
  key 覆盖；重复执行不得制造第二份 canonical 数据。
- worker 在预计超出租约时主动 Renew；超时后继续执行得到的结果不得 Complete/Fail。
- `worker_id` 是诊断和 fencing 的一部分，不是身份认证。安全边界仍依赖 ffdb 仅监听
  loopback；若未来允许远程 worker，必须先设计可信 principal，不能只相信 worker_id。

## Service 的一致性规则

- `ServiceState.next_fetch` 决定何时调度；到期调度器 enqueue
  `service.fetch`，idem 使用 service UUID 与 due window。Queue 不复制 ETag、token 或完整 URL 快照。
- handler 开始时重新读取 Service、State 和 ServiceFeedIndex；源已删除、无 binding、或
  `next_fetch` 已被一次较新的执行推进时，任务成为幂等 no-op 并 Complete。
- handler 必须先完成所有 FeedService 的幂等 Entry 投递并提交新的 ServiceState，
  再 Complete Task。若提交后进程
  崩溃，lease 重派会由最新 State/Entry identity 识别为重复执行。
- 尚有队列重试次数的抓取或投递错误调用 Fail；最后一次仍失败时，handler 必须先推进
  ServiceState 的来源退避或独立 delivery 退避，再 Complete 当前 Task，禁止下一分钟再次入队，
  也禁止把正常来源生命周期复制成持续增长的 TaskDone(DEAD)。只有失败状态无法持久化等未处理
  故障才让 Task 进入 DEAD。
  临时远程故障持续探测；确定性 4xx/解析失败连续至少六次且持续至少七天后才把来源置为 dead，调度器停止
  自动入队，手动 seed 成功可复活。binding 投递错误不增加来源失败计数；目标已删除的 binding
  自动 disable。ETag、HTTP 状态、来源生命周期和长期退避始终只属于 ServiceState，不能产生
  两套真相。若更新 State 后崩溃，重派 handler 由已推进的 `next_fetch` 幂等 Complete。
- 非 seed 的 handler 读到 dead 状态必须直接 Complete；状态转换前遗留的 `service.fetch` 无权
  重新激活来源。只有用户显式触发的 `feed_service.seed` 成功后可以复活。
- 首版 RSS 只由 ffdb 进程内 worker 执行，因此 per-host 锁能保证同 host 串行。开放
  多进程 RSS worker 前必须增加跨 worker 的 host 并发方案；进程内 mutex 不能冒充
  分布式互斥。
- 同一 Service 的 ServiceState 读改写目前也依赖这条单进程、per-host 串行边界。修改
  host 锁粒度或开放第二个 worker 进程前，必须先引入按 Service UUID 的持久化 lease/CAS，
  不能把内存 mutex 当作数据层并发契约。

## Reaper、关停与重启

- reaper 每 60 秒扫描 `lease_until <= now`；因此实际重派延迟是 lease 剩余时间加最多
  60 秒，这是明确 SLA。
- 收到关停信号后先把 Queue 标记为不接受 enqueue/claim，并停止调度器和进程内 worker
  领取新任务；随后 gRPC GracefulStop 排干正在执行的 Complete/Fail/Renew RPC。
- 排干 RPC 后取消进程内 handler context 并停止续租；未完成的 Task 保持 INFLIGHT，
  由下次启动后的 reaper 回收。关停不为外部 HTTP 设置无限宽限期。
- reaper/worker 退出并由既有 wg 确认后，才关闭 Pebble。kill -9 后不需特殊恢复流程。

## 审计、历史与运维

audit 至少验证：

- READY 主记录恰有一个匹配 Ready key，且无 Lease；INFLIGHT 反之。
- Ready/Lease 不得指向缺失 Task，索引时间/type 必须与主记录一致。
- Idem hash、type、原 key 与目标 Task 一致，不得指向 Done 或缺失 Task。
- Done key 的时间/task id 与 value 一致；DEAD 必须保留可校验、可 replay 的 Task。

工具分两类：

- `tools -c list_tasks -task-state ready|inflight|dead -max-limit N` 只读、有界输出。
- `tools -c inspect_task -id ID` 检查单条 active/Done 记录。
- `tools -c replay_dead_task -id ID` 经 Queue 校验 payload/type，生成**新 task id 和新
  attempts**；原 Done 历史保留，避免审计链被改写。
- `tools -c purge_task_done -before RFC3339 -dry-run` 先计数；去掉 dry-run 后以相同
  cutoff 做 RangeDelete。没有隐式 retention，操作者必须明确给出边界。

Done 由显式时间 cutoff 裁剪。list/inspect 的输出和内存有界，但检查稀疏 DEAD 或按 task ID
查 Done 的扫描时间可能随 TaskDone 总量增长；运维必须定期以明确 cutoff 执行 purge。日志只记录 task id、type、worker、
耗时和截断错误；不得记录 payload、正文或凭据。

损坏 Task/Ready/Lease/Idem 行采取 loud-failure：停止 ffdb，对数据库做一致性备份，再在副本
运行 `audit_store` 和 `inspect_task` 保存证据。当前没有自动 reconcile 命令；无法由主记录和
索引唯一确定正确状态时必须从备份恢复，不能直接用 Pebble 工具猜测删 key。确需原地修复时，
应针对已确认的不变量新增带 dry-run、精确 task ID 和回归测试的一次性命令，验证副本后再操作
停服目录。

## 分阶段实施

每阶段单独提交、可回退，不能把 RSS 与尚未验证的队列核心绑成一个大提交。

### M1：持久化与纯 Queue 状态机

- protobuf、五张表、key codec、type registry。
- Enqueue/Claim/Complete/Fail/Renew 和 `EnqueueWith`，不接 gRPC、不接真实 handler。
- 单测覆盖编码边界、注册表启动校验、同批/并发 idem、并发 claim、fencing、
  renew/reap stale lease、过期租约 Renew 拒绝、Renew 固定硬上限、backoff 边界、
  attempts→DEAD、outbox 原子失败；`go test -race ./task/...`。

### M2：server 适配与生命周期

- 五个新增 RPC、参数/状态码映射、空 claim 正常响应。
- reaper、受控进程内 worker pool、GracefulStop/Store.Close 顺序。
- 用假 handler 做崩溃、超时、续租、关停集成测试；此时队列核心可独立验收。

### M3：Service 接入

- Service/State 调度只 enqueue 有 binding 的 due service；handler 按上述一致性规则执行。
- 验证重复执行、状态更新后崩溃、无 binding、条件 GET、SSRF 和 host 串行。
- legacy `RefetchJobTicker` 继续为 Twitter FeedJob 表 200-202 和 FeedAgent 服务；它与
  Service/RSS Task 并行运行。只有 Twitter 生产链路另行迁移并验收后才能退役。

### M4：运维闭环

- audit、list、replay-dead 和 Done retention；首版不提供会猜测状态的自动 reconcile。
- 运维状态由 `audit_store` 与有界 `list_tasks` 查看；需要常驻 metrics 时另行设计，
  不在首版伪造一套未消费的指标。

### M5：外部 worker

- Python crawler 使用新 RPC 的样例和 systemd 部署方式。
- 在开放多进程 RSS 或远程 worker 前，重新评审 principal、host 并发和 loopback 边界。
- 单独设计旧表 200-202 的数据清理与旧 RPC 退役；不得随 Task 上线直接删除。

参考 worker 为 `scripts/task_worker.py`。使用 `uv sync`/`uv pip install -r requirements.txt`
准备环境后，可用如下形式运行本地 handler：

```bash
uv run python scripts/task_worker.py --worker-id crawler-1 --type twitter.crawl -- ./handler
```

handler 从 stdin 读取 protobuf payload，退出 0 表示 Complete，非零表示 Fail；wrapper
在执行期间 Renew，并在空队列时 1-30 秒指数退避。示例强制 gRPC target 为 loopback，
不提供 systemd unit：仓库目前没有已注册的外部 task type，部署一个空转 unit 没有价值。
新增真实外部使用方时再以专用系统用户配置 unit，且不得记录 stdin/payload。

## 非目标

- 不承诺 exactly-once。
- 不实现优先级、DAG、广播、跨主机 broker、无限 long-poll 或任意用户自定义 task type。
- 不把 ServiceState、timeline 状态或业务失败历史塞进 Task 主记录。
- 不因新 Task 系统顺手删除 legacy Job API、同步 mirrorMedia 或现有调试/迁移路径。
