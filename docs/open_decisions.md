# 延期与架构决策

本文档只记录当前确实需要产品或架构选择的事项，不是自动执行清单。兼容契约和已确定的工程边界见 `AGENTS.md`。

## 前端与 Web

- **SSR/CSR 边界**：entry 当前保留 SSR 首屏，React 挂载后再替换内容。迁为纯 CSR 或 hydration 前，需一起评估首屏、SEO、模板与错误回退。

## 部署与运行维护

- **`deploy_client` 现代化**：兼容 task 仍指向已退役的 `client/` 与 Upstart，当前不可用于部署。需先明确 `cli/` 是否仍作为常驻同步服务运行；若保留，再迁移构建路径并提供 systemd unit，不能直接删除导出 task。
- **日志策略**：当前 stdout/stderr 交给 journald 可以继续使用。若统一日志框架，先定义各二进制的级别、字段和敏感信息规则，再按 package 迁移。
- **Python 依赖锁定**：确定生产 Python 版本后，再统一锁定 `twikit`、`pandas`、`numpy` 及 protobuf/gRPC 兼容范围。
- **CLI `--debug`**：该兼容 flag 当前没有行为。后续应选择实现明确的 verbose 日志，或经过退役周期后删除；不直接破坏外部 CLI 契约。

## 协议、数据与服务模型

- **feed/search 分页**：`cachedFeed`、profile feed、timeline、search 及 httpd 消费方必须统一审计和迁移。在方案确定前，不局部修改 `Start/PageSize`、`PageSize+1`、缺失 entry 处理或 protobuf 分页字段。
- **股票存储**：`GetStockList`/`GetStock` 当前读取整表 gob。按 symbol 建索引会改变 schema，需先确定新数据模型和迁移方式。
- **linkify**：重新设计文本实体识别、HTML 安全输出和 hashtag URL 规则；不继续扩大参数语义不清的 util API。
- **Twitter 写入模型**：先决定 `fetch_user` 是否继续维护 legacy Entry feed，还是迁往 Tweet/PostTweet，再考虑复用转换代码。
- **Group comment moderation**：当前仅评论作者、entry 作者和 super 可删除评论。是否允许 group admin 审核 cross-post comment，需要先定义 graph 缺失、缓存过期和跨 feed 的授权语义。
- **多实例 Profile cache**：当前按单实例运行。引入多实例且要求 rename 即时一致前，需要设计 revision、失效事件或共享 cache。
- **Pebble shared cache**：每个 `Store` 当前创建 512 MiB cache；BackupDB 和同时打开 source/target 的工具会叠加内存占用。共享前需明确 cache `Ref/Unref` 所有权、内存预算和备份生命周期。
