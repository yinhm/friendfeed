# 延期与架构决策

本文档只记录当前确实需要产品或架构选择的事项，不是自动执行清单。兼容契约和已确定的工程边界见 `AGENTS.md`。

## 前端与 Web

- **SSR/CSR 边界**：entry 当前保留 SSR 首屏，React 挂载后再替换内容。迁为纯 CSR 或 hydration 前，需一起评估首屏、SEO、模板与错误回退。

## 部署与运行维护

- **`deploy_client` 现代化**：兼容 task 仍指向已退役的 `client/` 与 Upstart，当前不可用于部署。需先明确 `cli/` 是否仍作为常驻同步服务运行；若保留，再迁移构建路径并提供 systemd unit，不能直接删除导出 task。
- **Python 依赖锁定**：确定生产 Python 版本后，再统一锁定 `twikit`、`pandas`、`numpy` 及 protobuf/gRPC 兼容范围。
- **CLI `--debug`**：该兼容 flag 当前没有行为。后续应选择实现明确的 verbose 日志，或经过退役周期后删除；不直接破坏外部 CLI 契约。
- **后台系统状态摘要**：是否提供 loopback/CLI 可读取的有界诊断摘要尚未决定。候选指标包括 Task ready/inflight/dead 与最老 ready age、Service 状态、Realtime subscriber/drop、Timeline maintenance 失败与 backlog、Notification trim backlog。若实施，不得输出 payload、URL query、token 或正文，也不得用全表加载换取统计。
- **受控运行时内存诊断**：是否增加仅可从 loopback 管理入口显式触发的 Go runtime 诊断尚未决定。基础输出可包含 `runtime.MemStats`、goroutine 数以及 Pebble cache/memtable 指标；需要定位对象归属时，可选生成一次性 heap profile。不得开放公网 pprof，不得常驻采样或自动上传；profile 必须写入受限目录、使用 `0600` 权限、限制并发与保留数量，并由操作者显式清理。日志和 profile 之外的响应不得携带配置、凭据、请求正文或用户内容。该能力只用于区分存活对象、Go heap 高水位和底层 cache，不应以强制 GC 或缩小 cache 掩盖未定位的问题。

## 协议、数据与服务模型

- **Stock 退役**：Stock 功能即将退役，不再为其设计新 schema。历史 stock/system Feed 可能以
  `Type=group` 存在，因此 2.0 Group 发现页可能展示这些记录；退役时统一删除或改正其 Profile
  类型和派生目录，不在 `ListGroups` 中长期维护特判。
- **Twitter 写入模型**：先决定 `fetch_user` 是否继续维护 legacy Entry feed，还是迁往 Tweet/PostTweet，再考虑复用转换代码。

## 数据库性能与扩展

- **Prefix bloom**：配置 `Comparer.Split`、`SeekPrefixGE` 或 prefix bloom 前，必须覆盖全部 table key 布局并设计 SSTable filter 重写、全量 compaction 与回退验证；不作为普通 iterator 优化启用。
- **Durable fanout outbox**：当前保持同步 fanout 与 audit/rebuild。只有实际发生 fanout 超时、部分写失败或 celebrity feed 写放大后，才设计持久化 outbox；不提前建立通用 job 框架。
