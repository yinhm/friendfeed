# Task Queue 实施清单

依据 `docs/task_queue.md` 实施。每个 checkpoint 单独提交；legacy FeedJob API 与表
200-202 保持不变。完成项必须同时满足代码、测试、文档和兼容契约。

## M1：Queue 核心

### 1. protobuf 与表注册

- [x] 新增 TaskState、TaskCompletionStatus、FailTaskOutcome。
- [x] 新增 Task、TaskCompletion 及五组 RPC request/response message；客户端不得提交
      state、attempts、lease 或服务端时间。
- [x] 在 Api service 追加 EnqueueTask、ClaimTasks、CompleteTask、FailTask、
      RenewTaskLease，不改旧 RPC。
- [x] 注册 TableTask 203、Ready 204、Lease 205、Idem 206、Done 207 和对应 Table。
- [x] 使用仓库现有 protoc 流程重新生成，确认生成物与当前 grpc/protobuf 版本兼容。
- [x] 测试表号、字段及旧 RPC descriptor 仍存在。

### 2. key codec 与注册表

- [x] 实现 raw flake task id 与 32-char hex RPC id 双向转换。
- [x] 实现 Task/Ready/Lease/Idem/Done key 编解码和严格长度、type、时间校验。
- [x] Ready type prefix 为单次 Claim 内 k-way merge 提供有界扫描前缀。
- [x] 实现 Definition registry，启动时校验 attempts、lease、max lease、backoff、
      payload/version/大小限制。
- [x] 单测正常编码、零 ID、损坏 key、type 前缀、时间边界、idem hash 和注册失败。

### 3. Queue 状态机

- [x] Enqueue：Task+Ready+Idem 单 batch；active idem 返回已有 Task。
- [x] EnqueueWith：固定 `Queue.mu → Store.ApplyBatch`，同批去重，callback 失败零写入。
- [x] Claim：按 type/due time 公平选择；READY→INFLIGHT；attempt/epoch/claimed/lease
      一次提交；空队列正常返回空结果。
- [x] Complete：fencing 校验；删除主记录/Lease/Idem；写 Done(OK)。
- [x] Fail：RETRY/DEAD、确定性可注入 jitter、首次 base、attempts 封顶。
- [x] Renew：旧 Lease key 原子替换；过期拒绝；claimed_at+MaxLease 硬上限。
- [x] ReapOnce：只回收仍匹配主记录 epoch/lease 的到期项，复用 Fail 状态迁移。
- [x] 明确损坏队头 loud failure；不静默跳过或在线修复。
- [x] 全部状态机测试通过 `go test -race ./task/...`。

## M2：RPC、worker 与生命周期

### 4. server gRPC 适配

- [x] ApiServer 持有 Queue；构造失败返回 error，不在库代码 Fatal。
- [x] 五个 RPC 只做参数验证、错误码映射与 Queue 委托。
- [x] worker_id/type/task id/批量上限校验；空 Claim 返回 OK。
- [x] gRPC 测试覆盖 InvalidArgument、NotFound、FailedPrecondition、Internal。

### 5. reaper 与进程内 worker

- [x] ReapLoop(ctx) 60 秒 tick；ReapOnce 使用可注入 clock，loop 受 context 控制。
- [x] 有界 worker pool 通过 Queue Claim，不绕过状态机。
- [x] handler 完成/失败/续租路径和 panic 恢复不遗失 Task。
- [x] 关停先停止 enqueue/claim 和新 handler，再排干 RPC/后台任务，最后关闭 Pebble。
- [x] 测试 GracefulStop、handler 取消、过期回收和 Store.Close 顺序。

## M3：RSS 首个使用方

### 6. Subscription 数据模型

- [ ] 注册 Subscription 111、SubscriptionState 112；定义 protobuf 和 key codec。
- [ ] URL 规范化、全局 feed identity、Follow/Follower 原子订阅关系。
- [ ] State 保存 ETag/Last-Modified/next_fetch/长期失败状态。
- [ ] model/server 测试覆盖订阅幂等、退订和无 follower。

### 7. RSS 调度与 handler

- [ ] due State 流式扫描，入队 `rss.fetch`，active idem=`feed_uuid`。
- [ ] 进程内 RSS worker；首版 per-host 串行、全局并发有界。
- [ ] SSRF：scheme、DNS/IP、每跳 redirect、响应大小、超时和 UA。
- [ ] 条件 GET、格式解析、稳定 item UUID、PostEntry/mirrorMedia 既有路径复用。
- [ ] 成功先提交 Entry+State 再 Complete；最终失败先推进 State 再 DEAD。
- [ ] 重复执行、提交后崩溃、短期 retry、最终失败、304 和 no-op 全覆盖。
- [ ] 稳定后停止启动空转 RefetchJobTicker，但不删除旧符号/API。

## M4：运维闭环

### 8. audit、inspect、replay 与 retention

- [ ] audit_store 增加 Task↔Ready/Lease/Idem/Done 双向不变量，流式且内存有界。
- [ ] tools 提供有界 list ready/inflight/dead 和按 task id inspect。
- [ ] replay-dead 经 Queue 生成新 task id，保留原历史并重新校验 type/payload。
- [ ] Done 按时间/数量裁剪，使用安全 RangeDelete，支持 dry-run。
- [ ] 指标/日志只含 task id/type/worker/耗时/截断错误，不记录 payload 或 secret。

## M5：外部 worker 与文档

### 9. Python 示例和部署边界

- [ ] Python 示例闭环 Claim/Complete/Fail/Renew，空队列客户端退避。
- [ ] requirements 与 Fabric/systemd 文档使用现行 uv 流程。
- [ ] 明确仅 loopback；不开放远程 worker，不把 worker_id 当 principal。
- [ ] 多进程 RSS/per-host 限流留作重新评审，不用进程内锁冒充分布式锁。

### 10. 收尾

- [ ] AGENTS.md 只补 203-207 key/schema 与必要状态机不变量。
- [ ] 更新 database/task/service aggregation/迁移和运维文档，删除过时描述。
- [ ] 完整执行 `go build ./... && go vet ./... && go test ./...`，Task 并发测试加 race。
- [ ] 前端无变化时不运行前端生成流程；若增加 UI，再跑完整前端门禁。
- [ ] 删除本 TODO，确保工作区干净并形成可回退提交序列。
