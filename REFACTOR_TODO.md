# ffdb 重构任务清单

> 来源：2026-07-17 全项目审查（`go vet` 已通过；死代码均经全仓库 Grep 验证无调用者）。
> 建议顺序：先确认第一节 bug → 再做第二节批量清理 → 最后单独提交重复代码合并。

## 一、疑似 bug（需业务确认，优先于重构）

- [x] `httpd/src/auth.go:20` — `LoginRequired` 重定向后未 `Abort()`，后续受保护 handler 仍执行
- [x] `server/command.go:140` — `RedoFailedJob` 疑似逻辑反转：入队失败才删除 running 记录，应是成功后删除
- [x] `server/stock.go:395` — `GetKLines` 中 `Bars > 3650` 被钳为 1 而非 3650，确认是否笔误
- [x] `server/server.go:334` ↔ `server/index.go:87` — `cachedFeed` 无锁遍历 `bufq`，与 `rebuild` 并发重写存在数据竞争；读取时持锁或快照拷贝
- [x] `model/entry.go:103` — `DeleteEntry` 把任何 DB 错误都当"不存在"吞掉，应只在 `errors.Is(err, ErrNotFound)` 时吞
- [x] `media/media.go:116` — `Fetch` 的 `resp.Body` 未关闭，HTTP 连接泄漏
- [x] `cli/cmd/wallpaper.go:70` — `resp.Body` 未关闭
- [x] `search/search.go:13` — 全局 `Indexer` 在 goroutine 里赋值（`server.go:76`），启动竞速期 `PutEntry` 触发索引会 nil panic；改为同步初始化
- [x] `cli/cmd/twitter.go:71` — `defer stream.CloseAndRecv()` 在 err 检查之前，失败时 nil interface 求值 panic
- [x] `cli/cmd/wallpaper.go:117` — `Thumbnail` 出错后继续使用可能为 nil 的 `thumbObj`
- [x] `pb/helper.go:79` — `FormatLikes` 中 `Body` 用 `length-3` 而 `Num` 用 `length-2`，对照 `FormatComments`（皆 `length-2`）疑似不一致
- [x] `twitter/client.py:87` — `resp.klines` 应为 `resp.KLines`，一调用即抛 AttributeError
- [x] `twitter/crawler.py:41` — 格式串无占位符却 `% GROUP_NAME`，`--run init` 必抛 TypeError
- [x] `twitter/crawler.py:62` — `except UserNotFound` 后未 return，第 68 行 `user.id` 必抛 NameError
- [x] `twitter/client.py:173` — `adjust()` 分支返回 `True`，正常路径返回 DataFrame，类型不一致
- [x] `twitter/crawler.py:29` — **安全**：明文 Twitter 账号/密码已进 git 历史，改读环境变量并轮换密码（旧凭据仍需在 Twitter 侧轮换）
- [x] `fabfile.py:291` — `deploy_nginx` 引用的 `conf/nginx_http.conf` 不存在，任务必失败
- [x] `server/job.go:87,112,155` — 三处 `s.mdb.Put` 错误被丢弃；`job.go:159` `ListJobQueue` 命名返回 err 恒为 nil
- [x] `server/command.go:244` — `BackupDB` 的 `ndb.Set` 逐条丢弃错误，备份可能静默丢数据
- [x] `server/index.go:74` — `Push` 向缓冲为 1 的 channel 发送，rebuild 期间会阻塞 gRPC handler；改非阻塞 select
- [x] `httpd/src/server.go:361` — `CommentDeleteHandler` 丢弃 `MustBindWith` 错误，绑定失败仍返回 200
- [x] `helper.go:120`（server）— `entry.Thumbnails[0]` 无长度检查，脏数据会 panic；同类 `httpd/src/server.go:226`、`entry.go:44` 的 `feed.Entries[0]`
- [x] `store/store.go:199` — `SetSync` 替换指针与并发读取存在数据竞争（当前未实际触发，至少注明非并发安全）

## 二、高优先级重构：死代码清理（已验证无调用者）

### server/
- [x] 删 `server/helper.go:23` `FormatEntry`（与 `FormatFeedEntry` 完全重复）
- [x] 删 `server/server.go:67` `SetLogFile`
- [x] 删 `server/server.go:173` `ArchiveProfilePicture`（依赖的 friendfeed-api.com 已关停）
- [x] 删整个 `server/utils.go`（`CheckRedirect` 唯一调用者是上面的死代码，且有 body 未关闭问题）
- [x] 删 `server/command.go:250` `TempFix`（一次性修复脚本，未挂载）
- [x] 删 `server/stock.go:366` 的 `fmt.Println` 调试残留

### model/
- [x] 删 `model/entry.go:158` `DeleteTweet`、`model/key.go:21` `NewBlankUUIDKey`
- [x] 删 `model/key_test.go:78-160` 中整块已失效的旧测试；保留业务代码中的注释调试信息

### store/
- [x] 删 `store/store.go` 中无引用的私有常量 `defaultWorkerId`、`defaultDatacenterId`
- [x] 删 `store/iterator.go` 内部多余的 `options` 字段
- [x] 保留 `MetaKey`/`NewMetaKey` 与 `UUIDFlakeKey`：前者用于 job history，后者用于 entry index 生产路径，并非仅测试使用
- [x] 删 `cli/tools/migrate_db.go` 中未使用返回值的 `db.Options()` 调用
- [x] 清理 `server/server.go` 中处理 `ExistItem` 的不可达分支

### util/ + cli/
- [x] 保留导出的跨平台 `RedirectStderr` API；修复不支持平台分支引用不存在的 `Errorf`
- [x] 删 `util/truncate.go:40` 恒真 `isHTML` 及不可达分支
- [x] 删 `cli/config.toml`（另一个项目 ctdx 的配置残留）

### httpd/（含前端）
- [x] 删 `httpd/app/src/options.js`（341 行，其 import 的文件均已不存在）
- [x] 删模板未使用的 `httpd/src/filter.go` `timeSince` filter；保留导出的 `CurrentUserId`
- [x] 处理 `httpd/src/account.go:12` 空 `AccountHandler`（重定向到 `/account/import`）
- [x] 删 `feed.html` 中无对应路由且命令不可达的 subscribe/unsubscribe 表单；保留注释调试路径引用的 `_feed.html`
- [x] 删未引用的 `logo.svg` 及 `entry.jsx` 中引用不存在方法的 `onFocus`；保留实际加载的 `App.css` 和调试信息

### twitter/（Python）与部署
- [x] 删 Python 私有死代码：`crawler.py` 未用的 datetime/json/pickle import、`client.py` 未用的 time import 与 `_DESCRIPTION`；保留配置数据 `zh_names`
- [x] 停止运行时构造未使用的 `from_feed`；将构造与使用方式一起保留为调试注释

### 构建产物瘦身（httpd 二进制 105MB 的根因）
- [x] 修 `httpd/app/scripts/publish-build.mjs`：发布前清空 `static/js`、`static/css`（保留手写 `style.css`）
- [x] 删除 `httpd/static/js/` 下 3 套旧 CRA bundle、约 19.3 MB sourcemap、旧 chunk 及 `static/css/` 重复文件；production Vite 构建不生成 sourcemap

## 三、高优先级重构：机械化现代替换（go 1.21）

- [x] `golang.org/x/net/context` → 标准库 `context`（全仓约 12 处：`server/` 7 处、`cli/cmd/` 4 处、`httpd/src/server.go:23`）
- [x] `grpc.WithInsecure()` → `grpc.WithTransportCredentials(insecure.NewCredentials())`（`cli/cmd/root.go:42`、`httpd/main.go:202`、`server/server_test.go:508`）
- [x] 删 `server/server.go:48` 的 `rand.Seed(...)`（Go 1.20+ 自动播种）
- [x] 哨兵错误改 `errors.Is`：`server/auth.go:16,39`、`model/oauth.go:34`、`store/store.go:215`、`search/search.go:31`、`cli/tools/migrate_db.go:843`
- [x] `store/store.go` 的错误类型断言改 `errors.As`；`server/server.go` 对应断言已随不可达 `ExistItem` 分支删除
- [x] `fmt.Errorf` 无格式参数 → `errors.New`（`server/server.go:154`、`job.go:129`、`model/` 多处、`cli/cmd/twitter.go:80`）
- [x] 错误包装 `%s` → `%w`：`model/table.go`、`media/media.go`、`cli/cmd/twitter.go`；`entry.go` 已使用 `%w`，URL 字符串格式化保持 `%s`
- [x] `ioutil` → `io`/`os`：`media/media.go:5,122`、`cli/cmd/wallpaper.go`、`util/config.go`
- [x] `interface{}` → `any`：`search/search.go:35,46,77,81`、`search/mock.go`、`httpd/render.go:55`
- [x] `endTime.Sub(startTime)` → `time.Since`（`server/server.go`、`stock.go`）；`util.FormatTime` 的反向时间差改 `time.Until`
- [x] `util/config.go:26` — `NewConfigFromJSON` 读取失败时返回 error，不在库函数中 `log.Fatal`；httpd 启动日志保留配置路径和原始错误
- [x] `store/utils.go` — `mkdir` 简化为一行 `os.MkdirAll`，删 snake_case 的 `path_exists`
- [x] `store/store.go:45,62` — 统一使用标准库 `log`，移除单点 `glog` Fatal
- [x] `media/media.go:106` — 补 `os.MkdirAll` 错误检查；写文件权限 0755 → 0644
- [x] `media/media.go:68` — `Exists` 对 `os.ErrNotExist` 返回 `(false, nil)` 并修测试断言
- [x] `server/command.go:19` — `Command` switch 增加 default 分支，不再吞子命令 error
- [x] `server.go:83`（根目录）— 处理 `rpcServer.Serve(lis)` 返回的 error，忽略正常 Stop 的 `grpc.ErrServerStopped`

## 四、中优先级：重复代码合并

- [x] `server/stock.go` — 四个常规 `Archive*` 流式 handler 抽泛型公共函数；保留 EOF 时需批量落库的 `ArchiveXRXD` 独立流程
- [x] `httpd/src/feed.go` — 五个 feed handler 的 `prevStart` + pongo2.Context 块（5×12 行）抽 helper
- [x] `httpd/src/server.go` — 合并 `LikeHandler`/`LikeDeleteHandler`（仅差一个 bool）；合并 `ExpandCommentHandler`/`ExpandLikeHandler`
- [x] `model/entry.go` — 合并 `FanoutEntry`/`DeleteFanoutEntry`（:66 vs :139，仅差 Index/RemoveIndex）
- [x] `model/like.go:15,39,79` — 三处查找循环改 `slices.IndexFunc`
- [x] `model/` — 提取 `TimelineUUID` 消除 3 处重复（`entry.go:68,141`、`server/server.go:394`）
- [x] `store/store.go` — `NewStoreOptions`/`NewMetaStoreOptions` 25 行逐字重复抽 `configureLevels`；`NewStore`/`NewMetaStore` 合并为私有 `openStore()`
- [x] `store/key.go` — 四个 key 类型的 `Bytes()` 统一实现，去 `unsafe.Sizeof` 与永不触发的 panic
- [x] `search/mock.go` — MockIndex 改接口嵌入 `bleve.Index`，删 ~80 行手写样板
- [x] `twitter/client.py:105-118` — 三个 `archive_*` 方法合并为 `_archive(rpc, func, name)`
- [x] `twitter/crawler.py` — media→Thumbnail 转换两段重复抽 `media_to_thumbnails`
- [x] `fabfile.py` — 三个 deploy 任务的 "code_root + git + build" 块抽 helper
- [x] `cli/tools/migrate_db.go:539` — 335 行巨型 switch 每个 case 抽独立函数

## 五、低优先级：风格与小清理

- [x] 私有命名：`profileUUid`→`profileUUID`、`rumCmd`→`runCmd`、`BASE_URL`→`baseURL`、`_oauthUserIdFrom`→`oauthUserIDFrom`
- [x] 将多处无语义的局部变量 `uuid1` 按上下文改名
- [x] 拼写：`stoped`→`stopped`、"Falke"→"Flake"、"comptabile"→"compatible"、"diable"→"disable"
- [x] 冗余写法：`entries[:]`（server.go:372 等）、`[]byte(kb)`/`string(item)`（index.go:102）、`store.KeyFromString(k)[:]`、`buf.Write(b[:])`
- [x] `time.Tick` → `time.NewTicker`（`server/job.go:17,25`）
- [x] 修正 server helper/stock 的复制日志文案；store 的 128 MB/512 MB 错误注释已随 `openStore` 重构移除
- [x] `model/table.go:85`、`key.go:45` — 局部变量遮蔽 `bytes` 包
- [x] `httpd/main.go:112` — 废弃的 gplus provider 改 `goth/providers/google`，删全局 map 篡改 hack
- [x] `httpd/main.go:62` — 硬编码 base64 favicon 改 serve 嵌入的 `static/favicon.ico`（且 MIME 声明错误）
- [ ] `httpd/src/entry.go:174` — `UploadHandler` 加 `http.MaxBytesReader` 限制
- [ ] `httpd/main.go` — `r.Run` 错误处理、HTTP graceful shutdown
- [ ] `httpd/render.go:56` — `pongo2.Must` + 非检查断言改返回错误
- [ ] 测试侧：`server_test.go:55` 固定端口 :12019；`search_test.go` 改 `t.TempDir()`；`media_test.go` 外网依赖改 `httptest.Server`
- [ ] `media/` — `shardFilepath` 简化为 `filepath.Join` 一行；`Object.Bucket` 死字段；`Fetch`/`Post` 冗余返回值简化（动接口，需改 3 个调用文件）
- [ ] `twitter/` 风格：`class Client(object)` 去 `(object)`、print % 改 f-string、`if args.run ==` 改 `elif`、变量遮蔽 `uuid`、`quote` 的 None 判断
- [ ] `Makefile` — 根与 `twitter/Makefile` 补 `.PHONY`，可加 `proto-py` 统一入口
- [ ] `pb/helper.go` — 冗余 `if !ownerOrSuper { ownerOrSuper = true }` 块、末尾裸 `return`、`RebuildCommentsCommand` 死参数 `graph`

## 六、架构层面（仅记录，不在本清单执行）

- `httpd` SSR 模板与 React 渲染存在结构性重复（React mount 后替换 `#root`），需架构决策
- `fabfile.py` 基于 Fabric 1.x（已 EOL），迁移是大工程
- `model/feed.go` 的 Feedinfo 表整体退役评估
- `media.Mirror` 未实现导致的整条媒体镜像链路空转，需决定实现还是放弃

## 七、未决定项目（待评估，暂不执行）

- `httpd/src/server.go:116` — `renderFeed` JSON 分支不做 sanitize：HTML 分支对 `entry.Body` 做了 `htmlSanitizer.Sanitize`，XHR/JSON 分支返回原始 Body，前端轮询后用 `dangerouslySetInnerHTML`（`app/src/content.jsx:46`）渲染。曾尝试把 sanitize 移到分支判断之前（2026-07-17），后撤销。待定：服务端统一 sanitize 是否会破坏前端期望的原始内容，还是应由前端渲染时处理。
- `model/table.go` 方法群（`GetRaw`、`Keys`、`Iter`、`IterValue`、`Find`、`ToStringKey`）— 部分方法当前仓库内无调用者，但可能属于运维或外部使用接口；不能仅凭静态零引用删除。`Keys` 的迭代器泄漏已修复，查询范围语义仍待确认。需先确认 API 边界、外部调用和长期维护意图。
- `model/key.go` 的 `SeekZero` — 当前由 `Table.Keys` 用于构造扫描范围，并非死代码。是否移除取决于 `Keys` 查询语义的最终决定；相关注释代码块暂不混合清理。
- `model/types.go` 的导出类型、表变量和表前缀 — 原清单拟删除 `ProtoMessageFunc`/`NewMessage`、`TableMax`、`UserMap/File/JobFeed/JobRunning/Config/Topic` 等。复查发现 `UserMap` 仍被迁移工具使用，多个 `Table*` 前缀被生产代码使用或属于持久化 schema 保留编号；其余符号也构成公共 API。删除前需分别确认外部调用和数据兼容性，不能按零引用整组删除。
- `model/feed.go` 的 `GetFeedinfo`/`PutFeedinfo` — 虽标记 Obsoleted 且新运行时已将 Feedinfo 虚拟化，但它们是导出 API，旧 Feedinfo 表仍用于迁移时重建社交图。需确认外部工具和历史数据兼容策略后再决定是否删除。
- `store.DestroyStore` 与错误码 `OK`/`Unknown` — 均为导出 API；`DestroyStore` 虽仍是 stub，但可能是预留能力，错误码数值也可能被外部调用者依赖。不能仅凭仓库内零引用删除。
- `store/key.go` 的 key-ordering 方法链（`KeyMin`/`KeyMax`/`BytesNext`/`Next`/`IsPrev`/`PrefixEnd`/`Equal`/`Compare`）— 除内部 helper `bytesPrefixEnd` 外均为导出 API，且 helper 被导出方法使用。不能仅因源自 CockroachDB、仓库内零引用就整组删除；需先决定 `store.Key` 的公共 API 边界。
- `store.Iterator` 的 `Prev`/`SeekLT`/`Last`/`UnsafeRawKey`/`ValueProto` — 当前仓库内虽无调用，但都是导出方法，属于 iterator 公共能力；不能仅按零引用删除。
- `store.NewMetaStore`/`NewMetaStoreOptions` — 不只是导出 API，`cli/tools/migrate_db.go` 仍实际调用它们读取旧版独立 meta 数据库。属于迁移兼容路径，不能按“仅注释引用”删除。
- `store.Store.Options()` — 迁移工具中未使用返回值的调用已删除，但方法本身是导出 API，可能供外部诊断或调优使用，不能仅按仓库内零引用删除。
- `store.ExistItem` — 当前仓库内没有生产者，但它是导出错误码，外部调用者可能依赖其符号和值；保留错误码，仅删除内部不可达消费分支。
- `util.UrlToLink` 与时间常量 `Month`/`Year`/`LongTime` — 均为导出 API，不能仅凭仓库内零引用删除；私有 `layoutDayMonth` 只存在于仍需保留的注释逻辑中，也不单独清理。待确认公共 API 边界及注释功能是否会恢复后再处理。
- `cli/cmd/wallpaper.go` 的 `OldWallpapers` — 当前仓库内无调用，但它是导出变量，且保存了可用于历史壁纸回填的原始数据；不能仅凭零引用删除。待确认历史数据是否已永久迁移、是否仍有外部回填工具依赖后再处理。
- `cli/cmd/root.go` 的 `config.debug`/`--debug` — 字段从引入起未被读取，但 `--debug` 是 CLI 对外参数；直接删除会导致仍传该参数的脚本解析失败。待决定保留兼容 no-op、实现实际调试行为或允许破坏性移除后再处理。
- `cli/tools/migrate_db.go` 的 `debug` case — 硬编码用户、entry、OAuth ID 及注释切换代码属于迁移排障素材；按“调试信息即使注释也需保留”的决定不做清理。若未来改造，应先设计可传参的诊断命令替代，而不是直接删除。
- `httpd/src/auth.go` 的 `CurrentUserId` — 当前仓库内无调用，但它是导出函数，且与仍在使用的 `CurrentUserUuid` 构成 session accessor API；不能仅凭零引用删除。待确认 httpd 包的公共 API 边界后再处理。
- `httpd/src/server.go` 的 `Server.secretKey`/`httpclient`/`assets`/`worker` — 四个私有字段只在构造时写入，但删除它们会连带改变导出构造函数 `NewServer` 的签名；保留参数并改成 no-op 也不是可接受的设计。待确认 httpd 包无外部调用者或设计兼容的新构造 API 后整体处理。
- `httpd/templates/_feed.html` — 当前生产路径不渲染该模板，但 `PublicHandler` 中保留了切换到它的注释调试代码。按“注释调试信息仍需保留”的决定，不单独删除其依赖模板；待确认该备用 SSR 路径不再需要后再一起清理。
- 前端调试代码清理 — `App.css` 仍由 `index.jsx` 加载；`utils.js` 的 `dprint` 被保留的注释调试代码引用，`editor.jsx`/`content.jsx`/`App.jsx` 的注释块及 `entry.jsx` 的 `console.log` 均属于要求保留的调试信息，不能按死代码批量删除。
- httpd 处理器注释块 — `CurrentFeedinfo` 旧实现、SSR 模板切换、gRPC send-size、旧 import 路由、Babel 初始化及 `entry.go` 的编辑/格式化说明均具有调试、兼容或设计记录价值；按“注释调试信息仍需保留”的决定不做批量删除。
- `twitter/client.py` 的 `get_ohlcs`/`adjust` — 两者实现股票复权数据转换，是可被外部脚本显式导入的模块级公共函数，且 `get_ohlcs` 实际调用 `adjust`；不能仅凭仓库内零引用删除。相关 pandas/numpy/datetime import 也随该能力保留。
- `twitter/config.py` 的 `zh_names` — 当前仓库内无引用，但它与实际使用的 `screen_names` 一样属于可供外部抓取脚本导入的账号配置数据；不能仅凭零引用删除。待确认中文账号抓取入口已永久弃用后再处理。
- `fabfile.py` 的 `deploy_client` — 当前引用已不存在的 `client/`、`conf/ffclient.conf` 和 Upstart，确实不可执行；但它是 Fabric 对外任务，README 仍记录独立部署 client 的意图。应先提供基于 `cli/` 与 systemd 的替代任务，再决定迁移或删除，不能仅以当前失效为由抹掉接口。
- `fabfile.py` 的 `line_in_file`/`test_if` 与 env 属性 — `line_in_file` 带 `@task`，是 Fabric 对外命令，`test_if` 是其实际依赖，并非互引用死函数；部分 env 属性虽无仓库内直接读取，也可能供 Fabric 扩展、模板或外部任务使用。需先界定部署 API 后再清理。
- `twitter/pip.txt` 依赖补齐与版本锁定 — `twikit`、`pandas`、`numpy` 确为运行时依赖，但版本选择会同时影响 Python 支持范围、gRPC/protobuf 兼容性和抓取 API 行为；用户决定暂时跳过，待确定部署 Python 版本与升级策略后再处理。
- `server.ArchiveFeed`/`ForceArchiveFeed` — 用户确认该 archive 链路未来没有维护价值，不再为两者抽公共实现；2026-07-18 的合并改动已撤销。后续应在确认无部署依赖后整体退役接口，而不是继续重构内部代码。
- feed/search 分页统一与 off-by-one — `cachedFeed`/`ForwardFetchFeed`/`Search` 当前多取第 `PageSize+1` 条，httpd 用 `len(entries) > PageSize` 判断是否显示下一页，但又未在渲染前截掉探测条目，造成每页多一条且翻页重复。`pb.Feed` 没有 `has_more/next` 元数据，不能只在 server 截断，否则前端无法判断下一页。需先决定扩展协议，或在 httpd 计算 `show_paging` 后统一截断，再抽公共分页逻辑。
- `GetStockList`/`GetStock` 数据读取重构 — 当前整表以 gob blob 持久化，简单抽 `loadStockList` 只有代码去重价值；改为按 symbol 索引又会改变数据 schema 和迁移要求。用户决定完整撤销并跳过本项，待结合股票数据设计统一处理。
- `server/server.go:233` + `media/media.go:77` — `mirrorMedia` 媒体镜像链：`Mirror` 自初始提交起即为 stub（恒返回 not implemented），链路从未真正下载过文件，且 URL 改写发生在 `PutEntry` 之后不落库。2026-07-18 曾删除 `mirrorMedia` 及其调用，**用户决定保留该代码**（未来要实现镜像功能），已撤销删除。未来实现方向：补全 `Mirror`（`Fetch`+`Post`）并把镜像调到 `PutEntry` 之前。
- `server/command.go`/`server/job.go` 的 job 重复代码合并 — `PurgeJobs`/`FixTooMuchJobs` 的双表扫描在错误处理、计数语义上需进一步确认；`TestJob`/`RefetchUserFeed` 的 job 构造还涉及 profile/service 选择和时间戳语义。用户要求完整撤销本次重构，待明确底层抽象边界后再讨论。
- `model.KeyPrefixToBytes` — 实现与 `store.KeyPrefix.Bytes()` 等价，但它是导出 API，且迁移测试仍用它构造 legacy table。不能仅因内部可替代就直接删除；需先确认外部调用与 model/store API 边界，再决定保留兼容包装还是做破坏性移除。
- `model.KeyFromString` vs `store.KeyFromString` — 两者同名但有 package 限定，不构成实际符号冲突；新增 `KeyFromParts` 并保留兼容包装只会扩大 API，收益不足，已完整撤销。真正需要评估的是 store 侧“合法 hex 则解码，否则原样返回”的模糊持久化键语义，待统一设计 key API 时再处理。
- `util/text.go` 与 `cli/cmd/twitter.go` 的 linkify 重复 — 曾新增带 hashtag URL format 参数的公共 `Linkify` 和回调式替换 helper，但该抽象扩大了 util API、参数约束不清，用户判定实现不可接受，已完整撤销。待从文本实体及 HTML 输出模型重新设计后再讨论。
- `twitter/crawler.py` 的 `fetch_user` 复用 `tweet_to_pb` — `fetch_user` 当前构造并发布 legacy `Entry`，而 `tweet_to_pb` 构造归档用 `Tweet`。直接调用只为复用少数字段会额外耦合 TweetUser 和统计字段；改为 `PostTweet` 则会改变写入模型。需先决定 `fetch_user` 是否继续维护 Entry feed，不能机械合并。
- 日志框架统一 — `server` 的 logrus 承担级别控制并接管 gRPC 日志，CLI、迁移工具、store 等使用 stdlib `log`，httpd 另有单点 glog。全局替换会影响日志级别、stdout/stderr、库包依赖及 systemd/journald 行为；需先确定目标框架和各二进制的日志策略，再按 package 边界迁移，不能作为机械清理执行。
- `media.Object.Url`→`URL` — `Object` 及其字段均为导出 API，直接重命名会破坏外部源码兼容；不能与私有命名清理混做。需先确认外部调用，或设计兼容迁移方案后再处理。
- `EnqueJob`→`EnqueueJob` — 该拼写已进入 `pb/api.proto` 的 RPC 名和 gRPC 方法路径，并生成 Go/Python 公共客户端 API。直接改名会破坏现有客户端；如需迁移，应先新增正确拼写的兼容 RPC、保留旧方法，再制定退役周期。
