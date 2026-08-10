# AGENTS.md

## 兼容契约

- 仓库内零引用不代表无外部调用。删除或改动导出符号、接口、构造函数、CLI/Fabric task、Python 模块 API 或 protobuf RPC 前，必须单独设计退役方案。
- 不改变 Storage、graph、protobuf 或持久化 key/schema 契约。尤其保留：
  - `media.Object.Bucket`、`Object.Url`、`Storage.Fetch/Post`。
  - `RebuildCommentsCommand` 的 graph 参数和 `NewServer` 签名。
  - `EnqueJob`、`ArchiveFeed`、`ForceArchiveFeed` 的方法与路径；纠错只能新增兼容 RPC。
  - model/store 的表前缀、错误码、key 编码与迭代顺序。
  - `TableUserRenameMap = 7`，编码为 `old_id -> 16-byte user UUID`。
- 受保护的导出 API：`model.Table` 查询/迭代方法、`SeekZero`、表变量/前缀、`GetFeedinfo/PutFeedinfo`、`KeyPrefixToBytes`；`store.DestroyStore`、错误码、`Key` 排序方法、`Iterator` 方法、`Store.Options()`；`util.UrlToLink`、时间常量、`cli/cmd.OldWallpapers`、`httpd/src.CurrentUserId`；`twitter/client.py` 的 `get_ohlcs/adjust`、`twitter/config.py` 的 `zh_names` 和 Fabric task。
- `model/feed.go` 的旧 Feedinfo、`UserMap` 仍用于迁移，不按运行时零引用删除。所有 iterator 必须关闭。
- Pebble 同步写入开关必须真实控制底层写入模式。

## 明确保留或暂缓

- `mirrorMedia` 不可删除；ArchiveFeed 在 `PutEntry` 前同步完成 Fetch、Post、URL 改写并随 entry 持久化。
- `ArchiveFeed`/`ForceArchiveFeed` 暂不内部重构；退役需整体确认部署、抓取与迁移依赖。
- 注释中的迁移、排障、备用 SSR 和调试代码不能仅因注释或零引用删除。
- 暂不机械处理 feed/search 分页协议、股票 gob schema、job 公共抽象、key API、linkify、Twitter Entry/Tweet 模型、日志框架和 Python 依赖锁定。分页需统一覆盖 cached/profile/timeline/search 及消费方。

## 数据与并发不变量

- ffdb 仅允许监听 loopback，不得绑定通配地址、网卡地址或对外暴露 gRPC；改变此边界前必须先设计可信 principal。
- job claim 使用独立 `jobMu`，queued→running 在同一 Pebble batch 提交。
- `ApiServer.cached` 构造后只读；若允许动态增删，须用独立锁保护全部访问。
- `PutEntry` 的 entry 与 author/group 直接索引原子提交；无上限的 timeline fanout 独立执行。索引/fanout 错误必须返回，删除普通 entry 也要清理 author timeline。
- FeedIndex 的 DB 检查在数据锁外；rebuild/load/dump 由 `rebuildMu` 串行，rebuild 期间的 Push 必须保留。
- `Store.Close` 并发安全且幂等。生产关停先用 gRPC `GracefulStop` 排干请求，再关闭服务和数据库。

## 迁移边界

- 社交图、timeline 重建和 R2 URL 改写只依赖 new DB，不重新引入 old DB。
- timeline 只处理同时具有 profile 与 OAuth 身份的活跃用户；先针对指定用户和小 feed 上限 dry-run。
- PublicFeed 的 `public` metadata/UUID 不得由普通 feed 初始化覆盖。
- GCS 已退出运行时，媒体使用 R2。

## 验证与提交

- Go：`go build ./... && go vet ./... && go test ./...`
- 前端：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`；需要时执行 `pnpm run test:e2e`。
- 前端先于 Go 构建；Go 二进制嵌入 `httpd/static/`。不提交生成的 JS、CSS、manifest；保留手写 `style.css`。
- 测试只清理本次创建的进程，不影响 systemd 服务。生产服务只写 stdout/stderr，由 journald 管理，禁止 ANSI 颜色和应用内日志文件；库代码与请求处理不得调用 `Fatal`；任何日志级别均不得记录 token、Cookie、session、密码、secret 或完整正文。不为统一框架机械迁移现有 stdlib log/logrus。
- 提交保持单一、可回退，并使用实际改动者的 `Co-authored-by`；不得冒用其他助手身份。
