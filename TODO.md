# TODO

当前可直接实施的改进清单，按收益、风险和依赖排序。本文件不重复 `OPEN_DECISIONS.md` 中尚未定案的架构问题，也不授权改变 `AGENTS.md` 保护的 API、protobuf 或持久化契约。

> `model/like.go`、`model/like_test.go`、`server/server_test.go` 中的 “TODO.md Step N” 指已经完成的 comment principal 加固计划，不指向本文。

## 1. Job queue 原子 claim 与独立锁（已完成，2026-07-29）

### 已确认问题

- `GetFeedJob` 使用 `ApiServer` 内嵌的全局 `RWMutex`，而同一把锁还用于保护 `cached` map。
- queue→running 当前是多个独立步骤：扫描 queued job、删除 queued key、生成并写入 running key。进程或写入在中间失败会留下不一致状态。
- 多个 job consumer 需要串行 claim，避免领取同一 queued job；producer 入队不需要占用该锁。

### 实施

- [x] characterization test 锁定空队列、正常 claim、损坏 job、running job 字段和队列顺序。
- [x] 为 `ApiServer` 增加独立 `jobMu`；`GetFeedJob` 不再占用保护 feed cache 的全局锁。
- [x] 确认并保持 `cached` map 在构造完成后不可变；`IndexJobTicker`、`FetchFeed` 只并发读取 map，不使用 `jobMu` 保护它。未来若支持动态增删 cache，必须另设 `cacheMu` 并统一保护所有访问点。
- [x] 在 `jobMu` 内完成 queued job 选择、running job 编码和 key 生成。
- [x] 使用一次 `store.ApplyBatch` 原子删除 queued key并写入 running key。
- [x] claim 准备或提交失败时 queued job 保持不变，不产生 running 残片。
- [x] 并发测试证明两个 consumer 竞争一个 job 时只能一个成功，FIFO job 不重复领取。
- [x] `EnqueJob`、`FinishJob`、`RedoFailedJob` 的既有导出 API 和持久化 key 编码保持不变。

### 不做

- 不合并 `PurgeJobs`、`FixTooMuchJobs`、`TestJob`、`RefetchUserFeed` 等 command 抽象。
- 不修改 protobuf RPC 或字段。
- 不为测试增加生产 hook。

## 2. Entry 写入与删除的错误边界（已完成，2026-07-29）

### 现有覆盖

已有 `TestPutEntry`、`TestDeleteEntryPropagatesReadError` 和 `TestFanoutEntryAndDeleteFanoutEntry`。不要重复声称该路径没有测试。

### 已确认问题

- `PutEntry` 忽略 `FanoutEntry` 返回的 `n, err`。
- `DeleteEntry` 忽略 author/group index 删除和 `DeleteFanoutEntry` 的错误。
- entry、author/group index 和 timeline fanout 是多次独立写；中途失败可能留下部分状态。
- `EntryIndex.Index` 自行 scan/delete/put，不能直接复用于外层 Pebble batch。

### 第一阶段：先明确错误语义

- [x] follower update 的底层错误传播已有直接测试；不为不可安全注入的 Pebble 写错误增加生产 hook。
- [x] mutation 已部分成功时返回带底层 cause 的错误：PutEntry 的 core commit 已完成但 fanout 失败时允许调用方按相同 entry ID 重试；DeleteEntry 在索引/fanout 失败时不删除 entry 本体。
- [x] 修复 `EntryIndex.Index`、`PutEntry`、`FanoutEntry`、`DeleteEntry`、`DeleteFanoutEntry` 忽略的错误，成功路径、key 布局、索引顺序和 fanout 范围不变。
- [x] group cross-post 已覆盖 author index、group index、author timeline、followers timeline 的完整断言。
- [x] 普通 entry 与 group cross-post 删除后的反向索引断言均已覆盖；顺带修复普通 entry 遗留 author timeline 的 bug。

### 第二阶段：评估最小原子边界

- [x] 新增私有 `indexBatch`，复用既有 key 编码和重复索引清理语义。
- [x] entry 本体及 author/group 直接索引原子提交；fanout 因 follower 数量无上限而保留为独立阶段。
- [x] `PutEntry` 在任何写入前完成 UUID、日期和 protobuf 校验，失败不会留下 entry 孤儿。
- [x] 保留 `PutEntry`、`DeleteEntry`、`FanoutEntry`、`EntryIndex.Index` 的既有签名。
- [x] Table 前缀、key 编码和迭代顺序不变。

## 3. FeedIndex rebuild 缩短锁持有时间（已完成，2026-07-30）

### 已确认问题

- `FeedIndex.rebuild` 原先持有 `FeedIndex` 写锁执行 DB 查询；pending queue 无上限，实际查询数不保证最多 1000。
- 这不会占用 `ApiServer` 大锁，但会阻塞该 index 的 `Push`、`snapshot`、`dump` 和 `load`。
- 简单地“锁内快照、锁外检查、锁内覆盖”可能丢掉检查期间发生的 `Push`，不能直接实施。

### 实施

- [x] 并发测试证明 rebuild 与 Push 交错时 pending entry 最终保留；顺序、去重和 missing entry 过滤已有测试。
- [x] DB 查询已全部移出 FeedIndex 数据锁；每个 rebuild 对每个 unique candidate 最多查询一次，直到收集 1000 条 live entry 或耗尽输入。
- [x] 两阶段 rebuild 在锁内摘取 pending/old buffer 并清空 dirty，锁外检查，锁内只替换结果；期间新增项留在 pending queue 并保持 dirty，由下一轮处理。
- [x] `rebuildMu` 只串行 rebuild/load/dump，防止启动 load 或定时 dump 与两阶段替换互相覆盖；Push/snapshot 不受 DB 查询阻塞。
- [x] Stop 仍等待 Serve 中正在执行的 rebuild 完成后返回，Shutdown 关闭 DB 前的等待契约不变。
- [x] `go test -race ./server` 通过。

### 不做

- 不改变 `MinQueue`、队列容量或 public index 持久化格式。
- 不顺带重写 FeedIndex 数据结构。

## 4. 关闭路径 characterization（已完成，2026-07-30）

- [x] Pebble v2.1.6 明确禁止并发/重复 `DB.Close`；旧 Store 的普通 `closed bool` 有 data race，多个调用还可能同时进入 Pebble Close 后阻塞或 panic。
- [x] Store 使用私有 `sync.Once` 保证现有 `Close()` 并发安全且幂等，不修改导出签名；关闭错误沿用既有不可返回契约并写日志。
- [x] 并发测试覆盖 16 个 caller；`go test -race ./store ./server` 通过。
- [x] 生产关停顺序已确认是 gRPC `GracefulStop` 排干请求后调用 `ApiServer.Shutdown`；Shutdown 后直接调用内部方法属于生命周期外误用，不增加全路径 nil 检查。
- [x] Pebble closed DB 的 `NewIter` 自身直接 panic而非返回 error；不修改现有 `Iterator()`、`NewIterator()` 签名，也不新增无法兑现安全保证的 wrapper。

本项只修复实际可达的 Store 重复 Close；不把测试/内部误用描述成生产 RPC 普遍路径，不重构已验证的 ApiServer shutdown 状态机。

## 延期到架构决策

以下事项继续由 `OPEN_DECISIONS.md` 管理，不在本文直接实施：

- mirrorMedia 的目标存储、失败策略、同步/异步边界与 R2 上传实现；
- gRPC/Command principal 与非 loopback 暴露策略；
- 多实例 Profile cache 失效；
- 分页最终协议；
- Pebble shared cache 的所有权、内存预算与 BackupDB 策略；
- job 公共抽象、key API、linkify、日志框架和 archive RPC 退役。

## 每项门禁

- Go：`go build ./... && go vet ./... && go test ./...`
- 并发或生命周期：增加对应 package 的 `go test -race`
- 前端行为变化：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`
- Web 交互变化：`pnpm run test:e2e`
- 最后：`git diff --check`、`git status --short`
