# 延期与架构决策

本文档只记录当前确实需要产品或架构选择的事项，不是自动执行清单。兼容契约和已确定的工程边界见 `AGENTS.md`。

## 前端与 Web

- **SSR/CSR 边界**：entry 当前保留 SSR 首屏，React 挂载后再替换内容。迁为纯 CSR 或 hydration 前，需一起评估首屏、SEO、模板与错误回退。
- **媒体/附件 URL 权限模型**：当前 V1 明确采用 capability URL 语义，正文图片和文件附件都不按
  Feed/Entry visibility 再做鉴权；拿到正确 canonical URL 即可访问，包括 private Feed/Group 内容。
  内容 hash 不被视为密码学授权凭据，这只是当前规模下的主动简化。未来如需要 signed URL、
  Entry-aware download route 或 private media ACL，必须一起评估历史 Entry URL、media origin/CDN 缓存、
  R2/local serving 与迁移兼容，不能只在单个下载 handler 上局部加权限。

## 部署与运行维护

- **Python 依赖锁定**：确定生产 Python 版本后，再统一锁定 `twikit` 及 protobuf/gRPC 兼容范围。
- **CLI `--debug`**：该兼容 flag 当前没有行为。后续应选择实现明确的 verbose 日志，或经过退役周期后删除；不直接破坏外部 CLI 契约。

## 协议、数据与服务模型

- **Feed legacy offset 分页**：当前版本明确保留 `?start=N` 兼容入口。匿名请求重定向到
  Feed 第一页；登录用户允许读取一次 legacy offset 页，下一页立即切换 cursor。暂不继续退役。
- **Twitter 写入模型**：先决定 `fetch_user` 是否继续维护 legacy Entry feed，还是迁往 Tweet/PostTweet，再考虑复用转换代码。

## 数据库性能与扩展

- **Prefix bloom**：配置 `Comparer.Split`、`SeekPrefixGE` 或 prefix bloom 前，必须覆盖全部 table key 布局并设计 SSTable filter 重写、全量 compaction 与回退验证；不作为普通 iterator 优化启用。
- **Durable fanout outbox**：当前保持同步 fanout 与 audit/rebuild。只有实际发生 fanout 超时、部分写失败或 celebrity feed 写放大后，才设计持久化 outbox；不提前建立通用 job 框架。
