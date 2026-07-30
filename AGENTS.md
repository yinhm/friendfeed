# AGENTS.md

## 项目结构

- `server/`：gRPC 后端、feed index、job、股票数据。
- `httpd/`：Gin Web 服务、Pongo2 模板、嵌入静态资源；另见 `httpd/AGENTS.md`。
- `httpd/app/`：React/Vite/Plate/Tailwind 前端。
- `model/`、`store/`、`pb/`：持久化模型、Pebble 封装和公共协议。
- `media/`、`search/`、`util/`：共享能力；`cli/`、`twitter/`：运维、迁移和抓取工具。

## 不可擅自改变的契约

- 仓库内零引用不等于无用。导出符号、接口、构造函数、CLI/Fabric 命令和 protobuf 方法都可能被外部程序使用；删除、改名、改签名前必须确认外部调用并设计兼容迁移。
- 不改变 `Storage`、graph、protobuf 和持久化 key/schema 契约。必须保留：
  - `media.Object.Bucket` 及 `Storage.Fetch/Post`，它们为 S3/R2 类存储预留；`Object.Url` 不能直接改名。
  - `RebuildCommentsCommand` 的 graph 参数。
  - `NewServer` 现有签名；不能把参数变成无意义 no-op 来“简化”。
  - protobuf 中既有的 `EnqueJob`、`ArchiveFeed`、`ForceArchiveFeed` 方法和路径。纠正拼写只能新增兼容 RPC，再安排退役周期。
  - model/store 的表前缀、错误码、key 编码和迭代顺序；数值与字节布局都是数据兼容的一部分。
  - `TableUserRenameMap = 7`，编码为 `old_id -> 16-byte user UUID`；它与 `UserMap` 分属历史跳转和当前路由命名空间。
- `model/feed.go` 的旧 Feedinfo、`UserMap` 等仍服务历史数据迁移；不能按运行时零引用删除。（`store.NewMetaStore`/`NewMetaStoreOptions` 已于 old_db 迁移完成、全量升级 Pebble v2 后退役删除。）
- Pebble 的同步写入开关必须真实控制写入模式，不能重构成不影响底层写入的空逻辑。

明确处于兼容保护中的导出 API 包括：

- `model.Table` 的查询/迭代方法、`SeekZero`、导出表变量/前缀、`GetFeedinfo/PutFeedinfo`、`KeyPrefixToBytes`。
- `store.DestroyStore`、错误码、`Key` 排序方法、`Iterator` 导出方法、`Store.Options()`；所有 iterator 使用后必须关闭。
- `util.UrlToLink`、时间常量、`cli/cmd.OldWallpapers`、`httpd/src.CurrentUserId`。
- `twitter/client.py` 的 `get_ohlcs/adjust`、`twitter/config.py` 的 `zh_names`、Fabric 导出 task。

若要处理上述符号，必须单独提出 API 退役方案，不能夹在清理或重构提交中。

## 已明确保留或暂缓的代码

- `mirrorMedia` 链路不可删除。未来实现应补全 `Mirror(Fetch+Post)`，并在 `PutEntry` 前完成 URL 改写和持久化。
- `ArchiveFeed`/`ForceArchiveFeed` 不再做内部重构；若退役，先确认部署、抓取和迁移依赖，再整体处理协议。
- 注释中的迁移、排障、备用 SSR 路径和调试代码要保留；先提供等价的可参数化诊断能力，不能以“已注释/零引用”为由批量删除。
- 不机械处理以下未决项：feed/search 分页协议、股票 gob schema、job 公共抽象、key API、linkify、Twitter Entry/Tweet 模型、日志框架统一、Fabric 1 迁移、Python 依赖锁定。分页必须覆盖 cached/profile/timeline/search 及全部消费方统一设计，当前协议和返回数量不允许局部调整。
- `--debug`、Fabric task、Python 模块级函数和配置数据属于外部接口；即使当前仓库未调用也不能直接删除。

## 修改原则

- 先写行为测试，再重构；迁移完成后同一组测试应原样通过。不要为测试向生产接口注入无意义 hook 或 helper。
- 持久化格式、protobuf、session/OAuth key、rawBody 节点类型先做 characterization test，再改生产者或消费者。
- 大规模机械迁移用脚本、字节/hash 或精确 diff 证明等价，不靠肉眼抽查。
- 只删除“私有、全仓库含测试零引用、且不承担兼容/调试/迁移职责”的代码。
- 保持改动单一：功能修复、重构、依赖升级、格式化和生成文件不要混在一个提交。
- 不凭印象判断库行为、版本或部署状态；能运行就实测，未运行要明确说明。

## 并发与写入不变量

- job claim 使用独立 `jobMu`，queued→running 必须在同一 Pebble batch 中提交；不要重新使用 ApiServer 大锁或拆回 delete/put 两步。
- `ApiServer.cached` 构造后只读；若未来需要动态增删，必须新增独立 cache 锁并统一保护所有访问点。
- `PutEntry` 的 entry 本体与 author/group 直接索引原子提交；timeline fanout 因 followers 无上限而保持独立阶段。所有 index/fanout 错误必须向上传播，普通 entry 删除也必须清理 author timeline。
- FeedIndex rebuild 的 DB existence 检查必须在数据锁外；rebuild/load/dump 由 `rebuildMu` 串行，rebuild 期间新增 Push 留在 pending queue，不能丢失。
- `Store.Close` 通过 `sync.Once` 并发安全且幂等。生产关停顺序是 gRPC `GracefulStop` 排干请求后再 Shutdown；不要用全路径 nil 检查掩盖生命周期误用。

## 数据迁移

- 明确 old DB 与 new DB 的边界。社交图、timeline 重建及 R2 URL 改写只依赖 new DB 时，不得重新引入 old DB。
- timeline 只针对具有 profile 与 OAuth 身份的真实活跃用户；先用指定用户和小 feed 上限 dry-run，再扩大范围。
- PublicFeed 的 `public` metadata/UUID 是兼容数据，不得用普通 feed 初始化逻辑覆盖。
- GCS 已退出运行时，媒体使用 R2；不要重新引入 GCS SDK、配置或 URL 写入。

## 验证门禁

按影响范围执行完整门禁，不只跑局部测试：

- Go：`go build ./... && go vet ./... && go test ./...`
- 前端（`httpd/app`）：`pnpm lint`、`pnpm run typecheck`、`CI=true pnpm test`、`pnpm run build`
- E2E（`httpd/app`）：`pnpm run test:e2e`
- 最后：`git diff --check`、`git status --short`

测试使用随机端口和独立临时目录；清理只能终止本次创建的进程，不能误杀 systemd/生产服务。systemd 服务默认写 stdout/stderr 交给 journald，避免依赖工作目录创建日志文件。

## 构建与提交

- 先构建前端，再构建 Go；Go 二进制嵌入 `httpd/static/`。
- 不提交生成的 `httpd/static/js/*`、`httpd/static/css/*`、`httpd/static/manifest.json`；保留手写 `style.css`。
- 不使用破坏性 Git 命令覆盖用户改动。提交应小而可回退，并带 `Co-authored-by` 署名；署名必须反映实际执行改动的 AI 助手（例如 Kimi 用 `Co-authored-by: Kimi <kimi@moonshot.cn>`，Codex 用 `Co-authored-by: Codex <codex@openai.com>`），不要照抄其他助手的身份。
