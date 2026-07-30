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

## 3. FeedIndex rebuild 缩短锁持有时间

### 已确认问题

- `FeedIndex.rebuild` 持有 `FeedIndex` 写锁执行最多约 1000 条 `db.Exist`。
- 这不会占用 `ApiServer` 大锁，但会阻塞该 index 的 `Push`、`snapshot`、`dump` 和 `load`。
- 简单地“锁内快照、锁外检查、锁内覆盖”可能丢掉检查期间发生的 `Push`，不能直接实施。

### 实施

- [ ] 先增加并发 characterization test：rebuild 期间 Push 的 entry 最终必须保留；重复 key 去重；已删除 entry 被移除；顺序保持。
- [ ] 记录 1000 项 rebuild 的锁持有时间和 `db.Exist` 数量，证明优化价值。
- [ ] 设计带 generation 或 pending queue 的两阶段 rebuild：锁内摘取稳定输入，锁外检查，锁内合并期间新增项。
- [ ] Stop 与 rebuild 并发时不得访问已关闭 DB，既有 shutdown 等待契约保持不变。
- [ ] 使用 `go test -race ./server/` 验证。

### 不做

- 不改变 `MinQueue`、队列容量或 public index 持久化格式。
- 不顺带重写 FeedIndex 数据结构。

## 4. 分页行为 characterization

当前 `cachedFeed`、`ForwardFetchFeed` 和 `Search` 的停止条件不一致，均可能返回 `PageSize+1`。协议方案仍在 `OPEN_DECISIONS.md`，本项只收集事实，不提前选择新协议。

- [ ] 分别锁定 cached public feed、profile feed、timeline 和 search 的 `Start/PageSize` 当前返回数量及边界。
- [ ] 覆盖 `PageSize <= 0`、`PageSize >= 100`、最后一页和缺失 entry。
- [ ] 记录 httpd 模板/JSON 消费方是否依赖额外一条探测 next page。
- [ ] 将测试证据写入 `OPEN_DECISIONS.md`，再决定统一截断还是扩展分页元数据。

本项不修改 protobuf，不在决策前改变线上返回数量。

## 5. 关闭路径 characterization

- [ ] 补 Store 并发 Close 测试，确认 `closed` 数据竞争和 Pebble 重复 Close 的实际行为。
- [ ] 补 ApiServer Shutdown 后内部直接调用路径测试，区分生产 gRPC `GracefulStop` 已保护的路径与测试/内部误用。
- [ ] 使用 `go test -race ./store ./server` 取得证据。
- [ ] 若要让 iterator 创建返回错误，必须新增兼容 API；不得修改现有 `Iterator()`、`NewIterator()` 签名。

在证据出现前不重构关闭状态机，不把测试误用描述成生产 RPC 普遍可达问题。

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
