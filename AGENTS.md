# AGENTS.md

## 兼容契约

- 仓库内零引用不代表无外部调用。删除或改动导出符号、接口、构造函数、CLI/Fabric task、Python 模块 API 或 protobuf RPC 前，必须单独设计退役方案。
- 不改变 Storage、graph、protobuf 或持久化 key/schema 契约。尤其保留：
  - `media.Object.Bucket`、`Object.Url`、`Storage.Fetch/Post`。
  - `RebuildCommentsCommand` 的 graph 参数和 `NewServer` 签名。
  - `EnqueJob`、`ArchiveFeed`、`ForceArchiveFeed` 的方法与路径；纠错只能新增兼容 RPC。
  - model/store 的表前缀、错误码、key 编码与迭代顺序。
  - `TableUserRenameMap = 7`，编码为 `old_id -> 16-byte user UUID`。
  - `TableTimelineIndex = 108`、`TableTimelinePosition = 109`、`TableTimelineState = 110` 及其 key/value 编码。
  - FeedService 表 101、Service 表 111-113 与 Task 表 203-207 的表号及相关设计文档所列编码。
  - `TableGroupAdmin = 114`，编码为 `group UUID + admin user UUID -> 空`，是 Group admin 角色的权威来源；LikeTimeline/CommentTimeline/CommentTimelinePosition 固定为 115/116/117，编码见 `docs/feed.md`。
  - `TableFollowRequest = 118`，编码为 `target feed UUID + requester user UUID -> RFC3339 申请时间`，仅是 private feed/Group 关注审批的工作流数据；批准后的关系仍以 Follow/Follower 边表示。
  - Entry 与 EntryIndex 中的 Entry key 固定为 `4-byte table prefix + 16-byte raw UUID`，不得写 UUID/hex 字符串。
- 受保护的导出 API：`model.Table` 查询/迭代方法、`SeekZero`、表变量/前缀、`GetFeedinfo/PutFeedinfo`、`KeyPrefixToBytes`；`store.DestroyStore`、错误码、`Key` 排序方法、`Iterator` 方法、`Store.Options()`；`util.UrlToLink`（输入为 sanitized HTML fragment）、时间常量、`cli/cmd.OldWallpapers`、`httpd/src.CurrentUserId`；`twitter/client.py` 的 `get_ohlcs/adjust`、`twitter/config.py` 的 `zh_names` 和 Fabric task。
- `model/feed.go` 的旧 Feedinfo、`UserMap` 仍用于迁移，不按运行时零引用删除。所有 iterator 必须关闭。
- Pebble 同步写入开关必须真实控制底层写入模式。

## 明确保留或暂缓

- `mirrorMedia` 不可删除；ArchiveFeed 在 `PutEntry` 前同步完成 Fetch、Post、URL 改写并随 entry 持久化。
- `ArchiveFeed`/`ForceArchiveFeed` 暂不内部重构；退役需整体确认部署、抓取与迁移依赖。
- 注释中的迁移、排障、备用 SSR 和调试代码不能仅因注释或零引用删除。
- 暂不机械处理股票 gob schema、job 公共抽象、key API、Twitter Entry/Tweet 模型和 Python 依赖锁定。保留现有 stdlib `log` 与 `slog`，不为形式统一迁移。

## 数据与并发不变量

- ffdb 仅允许监听 loopback，不得绑定通配地址、网卡地址或对外暴露 gRPC；改变此边界前必须先设计可信 principal。
- job claim 使用独立 `jobMu`，queued→running 在同一 Pebble batch 提交。
- Task 使用 READY/INFLIGHT 主状态、lease epoch fencing 与同批派生索引；handler 必须幂等，payload 与日志不得保存或输出凭据、正文。
- `PutEntry` 的 entry 与 author/group 直接索引原子提交；Home activity timeline 只 fanout 到 TimelineState 有效的 viewer，独立执行且错误必须返回。活跃 Home 最多 10,000 条，inactive 冷缓存保留 500 条，当前时间窗口为 MAX。`DeleteEntry` 不枚举 viewer，timeline 孤儿由读路径懒删及 audit/rebuild 清理。
- public timeline 是 TimelineIndex 的保留 viewer（`model.PublicTimelineUUID`），不是真实 profile：仅新建 Entry、首次 Like、新建 Comment 触发 bump，私有/已删除/不可解析的 target feed 一律不进入；不写 TimelineState，compact 永不把它当 inactive；trim 由 bump 计数驱动在后台 goroutine 执行，不进入请求路径。
- profile/timeline/public 通过 `FetchFeed` 的 cursor 模式分页，并兼容旧 Home/public `Start/PageSize` 链接；search 继续使用 `Start/PageSize`。cursor 只编码索引位置，解码后按当前 feed 前缀重建 key，不得解释为 entry UUID。public 响应的 `Feed.Uuid == "Public"` 字面量是 httpd 的识别契约。
- `Store.Close` 并发安全且幂等。生产关停先用 gRPC `GracefulStop` 排干请求，再关闭服务和数据库。

## 迁移边界

- 仅支持 Pebble v2 和已由 v1.0 工具迁移完成的新库，不兼容旧库、Pebble v1 或降级运行。
- 全库迁移/重建必须流式且内存有界；不得把全部 record、key 或 value 收集进 slice/map。小 batch 只限制单次写入，不代表内存有界；需要全量预检时使用多遍扫描，整段清理优先使用范围删除。
- 社交图、timeline 重建和 R2 URL 改写只依赖 new DB，不重新引入 old DB。
- timeline 运行时活跃性只由 TimelineState 判断；全量 `rebuild_timeline` 以 Profile+OAuth 预热，显式 `-user` 可初始化指定 profile。迁移先针对指定用户和小 feed 上限 dry-run。
- PublicFeed 的 `public` metadata/UUID 不得由普通 feed 初始化覆盖。
- GCS 已退出运行时，媒体使用 R2。

## 验证与提交

- Go：`go build ./... && go vet ./... && go test ./...`
- 前端：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`；需要时执行 `pnpm run test:e2e`。
- 前端先于 Go 构建；Go 二进制嵌入 `httpd/static/`。不提交生成的 JS、CSS、manifest；保留手写 `style.css`。
- 测试只清理本次创建的进程，不影响 systemd 服务。生产服务只写 stdout/stderr，由 journald 管理，禁止 ANSI 颜色和应用内日志文件；库代码与请求处理不得调用 `Fatal`；任何日志级别均不得记录 token、Cookie、session、密码、secret 或完整正文。
- 提交保持单一、可回退，并使用实际改动者的 `Co-authored-by`；不得冒用其他助手身份。
