# 延期与架构决策

本文档只记录当前确实需要产品或架构选择的事项，不是自动执行清单。兼容契约和已确定的工程边界见 `AGENTS.md`。

## 前端与 Web

- **SSR/CSR 边界**：entry 当前保留 SSR 首屏，React 挂载后再替换内容。迁为纯 CSR 或 hydration 前，需一起评估首屏、SEO、模板与错误回退。

## 部署与运行维护

- **`deploy_client` 现代化**：兼容 task 仍指向已退役的 `client/` 与 Upstart，当前不可用于部署。需先明确 `cli/` 是否仍作为常驻同步服务运行；若保留，再迁移构建路径并提供 systemd unit，不能直接删除导出 task。
- **Python 依赖锁定**：确定生产 Python 版本后，再统一锁定 `twikit`、`pandas`、`numpy` 及 protobuf/gRPC 兼容范围。
- **CLI `--debug`**：该兼容 flag 当前没有行为。后续应选择实现明确的 verbose 日志，或经过退役周期后删除；不直接破坏外部 CLI 契约。

## 协议、数据与服务模型

- **股票存储**：`GetStockList`/`GetStock` 当前读取整表 gob。按 symbol 建索引会改变 schema，需先确定新数据模型和迁移方式。
- **Twitter 写入模型**：先决定 `fetch_user` 是否继续维护 legacy Entry feed，还是迁往 Tweet/PostTweet，再考虑复用转换代码。
- **Group comment moderation**：当前仅评论作者、entry 作者和 super 可删除评论。是否允许 group admin 审核 cross-post comment，需要先定义 graph 缺失、缓存过期和跨 feed 的授权语义。

## 数据库性能与扩展

- **Feed N+1 读取**：先分别测量冷、热 Pebble cache 下的 index scan、Entry Get、protobuf decode 与渲染耗时；没有生产证据前不把 Entry 本体或摘要冗余进 fanout index。
- **Prefix bloom**：配置 `Comparer.Split`、`SeekPrefixGE` 或 prefix bloom 前，必须覆盖全部 table key 布局并设计 SSTable filter 重写、全量 compaction 与回退验证；不作为普通 iterator 优化启用。
- **Durable fanout outbox**：当前保持同步 fanout 与 audit/rebuild。只有实际发生 fanout 超时、部分写失败或 celebrity feed 写放大后，才设计持久化 outbox；不提前建立通用 job 框架。
