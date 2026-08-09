# 延期与架构决策

本文档只记录尚未决定、必须先明确兼容边界或需要独立设计的事项，不是自动执行清单。已完成的升级与重构历史不在此保留；不可擅自改变的公共 API、持久化和协议契约见 `AGENTS.md`。

## 前端与 Web 架构

- **SSR/CSR 边界**：entry 当前保留 SSR 首屏，React 挂载后仍存在重复渲染。是否迁为纯 CSR 需要结合首屏、SEO、模板和 hydration 策略整体决定。
- **TypeScript 7**：`tsgo` 已可通过现有类型检查，但正式门禁继续使用稳定版 TypeScript 5；待 TypeScript 7 稳定及工具链 peer 契约成熟后再切换。
- **叶子组件迁移**：稳定的 class component 不为统一写法而机械迁移；只有出现明确交互收益并先建立行为测试时才处理。

## 部署与运行维护

- **Fabric 1.x 退役**：先为现有导出 task、`deploy_client` 和扩展环境提供 systemd/现代 Fabric 替代方案，再安排兼容迁移；不能直接删除外部部署入口。
- **日志框架统一**：先确定各二进制的日志级别、stdout/stderr 与 journald 策略，再按 package 边界迁移，不能全仓库机械替换。
- **CLI `--debug`**：需要决定实现真实调试行为、保留兼容 no-op，还是允许破坏性移除。
- **Python 依赖锁定**：待明确部署 Python 版本后，再统一锁定 `twikit`、`pandas`、`numpy` 及 protobuf/gRPC 兼容范围。

## 协议、数据与服务模型

- **feed/search 分页**：当前协议与返回数量不允许局部调整。`cachedFeed`、profile feed、timeline、search 及 httpd 模板/JSON 消费方必须在同一任务中完成现状审计、协议选择、兼容迁移和回归测试；在统一方案确定前，不修改 `Start/PageSize`、`PageSize+1`、缺失 entry 处理或 protobuf 分页字段。
- **股票存储**：`GetStockList`/`GetStock` 当前读取整表 gob。按 symbol 建索引会改变 schema 和迁移要求，必须结合股票数据模型整体设计。
- **job 公共抽象**：合并 `PurgeJobs`/`FixTooMuchJobs`、`TestJob`/`RefetchUserFeed` 前，先明确双表错误处理、计数、profile/service 选择和时间戳语义。
- **key API**：需要统一 model/store 的 key 构造与解析规则，尤其是“合法 hex 自动解码，否则原样返回”的模糊语义；不能通过新增包装函数回避设计。
- **linkify**：从文本实体识别、HTML 安全输出和 hashtag URL 规则重新设计，不能继续扩大参数不清的 util API。
- **Twitter 写入模型**：先决定 `fetch_user` 是否继续维护 legacy Entry feed，还是迁往 Tweet/PostTweet，再考虑复用转换代码。
- **Feedinfo 退役**：旧 Feedinfo 仍用于历史数据迁移和社交图重建。确认外部工具与历史数据不再依赖后，才能整体退役表及导出 API。
- **archive RPC 退役**：`ArchiveFeed`/`ForceArchiveFeed` 不再做内部重构；确认部署、抓取、E2E 和迁移依赖后，才能制定协议级退役方案。
- **gRPC principal**：comment mutation 当前信任内部调用方提交的 `user_uuid`。对不可信网络开放 ffdb 前，必须从认证 middleware/context 获取 principal；此前应绑定 loopback 或用防火墙限制来源。
- **Group comment moderation**：comment delete 当前只允许评论作者、entry 作者和 super。是否让 group admin 审核 cross-post comment，需要先定义 graph 缺失、缓存过期和跨 feed 的授权语义。
- **多实例 Profile cache 失效**：单实例 rename 只失效本地 `profile:<uuid>` 与 `graph:<uuid>`，其他实例及跨用户快照依赖 TTL。出现多实例即时一致性需求后，再设计 revision、失效事件或共享 cache。
- **Pebble shared cache**：每个 `Store` 当前创建 512 MiB cache；正常 server 已统一为一个 Store，但 BackupDB 和同时打开 source/target 的工具会增加内存占用。共享前必须明确 cache `Ref/Unref` 所有权、进程内存预算和备份生命周期。

## 媒体镜像

- `mirrorMedia` 链路必须保留。`media.Mirror` 已实现为 `Fetch + Post`（受控 HTTP client：超时、2xx 校验、32MB 响应上限、重定向上限、SSRF 防护），`ArchiveFeed` 在 `PutEntry` 前同步完成镜像与 URL 改写并随 entry 持久化；单个媒体失败只记录日志并保留原 URL。单 entry 镜像设有总时长预算（`mirrorMediaBudget`）与媒体数量上限（`mirrorMediaMaxObjects`），超限的媒体保留原 URL；`Thumbnail.Link` 是点击导航 URL，不再抓取。
- 对象 key 不可信输入（URL path、`file.Name`）经严格清理：拒绝绝对路径与 `..` 穿越、逐段替换不安全字符，清理后为空时回退内容 sha256 作为 key；`Post` 落盘前校验解析后的路径仍在 MediaPath 内。分片子目录方案不变。
- R2 写入已实现：`media.NewStorage` 返回 `MirrorStorage` 组合存储，`Post` 先写本地分片路径、再用标准库实现的 SigV4 签名 S3 PUT 写 R2（region `auto`，无新依赖）；`Object.Bucket` 填 R2 bucket，`Url` 指向 `media_url`（默认 `https://m.friendfeed.me`）+ key，R2 bucket 与本地 media 目录在同一域名下对外服务。R2 失败整体算镜像失败（保留原 URL，本地副本可留）；凭据（`r2_account_id`/`r2_access_key_id`/`r2_secret_access_key`/`r2_bucket`）全部缺失时为显式仅本地模式（打一次日志），部分配置视为配置错误，`Mirror`/`Post` 直接失败并保留原 URL。`MediaPath` 为空时默认 `<db_path>/files`。

## 公共 API 退役边界

下列能力可能被外部工具、历史迁移或持久化数据依赖。若要删除或改名，必须先做调用审计、兼容 API 和退役周期；详细保护清单见 `AGENTS.md`。

- model/store 的导出表、前缀、错误码、key、iterator 和查询方法。
- `GetFeedinfo`/`PutFeedinfo`、`NewMetaStore*`、`Store.Options()` 等迁移兼容路径。
- `Storage`、graph、protobuf、session/OAuth、rawBody 与持久化 schema 契约。
- `media.Object.Url`、`EnqueJob`、`CurrentUserId`、`OldWallpapers`、时间常量及 Python/Fabric 导出接口。

## 明确保留，不作为待办

- 注释中的迁移、排障、备用 SSR 路径和调试信息。
- `_feed.html` 及其备用 SSR 调试路径。
- `mirrorMedia`、S3/R2 所需的 `Object.Bucket` 与 `Storage.Fetch/Post`。
- 已确认稳定且没有明确收益的组件和导出接口。
- 评论输入框 focus ring：已明确不采用，不再列入改进计划。
