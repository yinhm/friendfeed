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
- [ ] 删注释掉的死代码块：`model/key_test.go:78-160`（~83 行）、`entry.go:69,73,78`、`profile.go:39,60` 等

### store/
- [ ] 删 `store/store.go:96` `DestroyStore`（未实现）、:17-18 死常量、`codes.go:7-8` `OK`/`Unknown`
- [ ] 删 `store/key.go:19-118` CockroachDB 死链：`KeyMin`/`KeyMax`/`BytesNext`/`bytesPrefixEnd`/`Next`/`IsPrev`/`PrefixEnd`/`Equal`/`Compare`
- [ ] 删 `store/iterator.go` 死方法：`Prev`/`SeekLT`/`Last`/`UnsafeRawKey`/`ValueProto` 及多余 `options` 字段
- [ ] 删 `store/store.go:73,143` `NewMetaStore`/`NewMetaStoreOptions`（仅注释引用）
- [ ] 删 `store/key.go:169-198` `MetaKey`/`NewMetaKey`、:283-314 `UUIDFlakeKey`（仅测试使用，连测试一起删）
- [ ] 删 `store/store.go:195` `Store.Options()`（注意 `cli/tools/migrate_db.go:556` 有一句无效调用一并删）
- [ ] 清理 `store/codes.go:10` `ExistItem`（无生产者）及 `server/server.go:218-228` 不可达分支

### util/ + cli/
- [ ] 删 `util/redirect_stderr*.go` 三个文件（无调用者，且引用了不存在的 `Errorf`）
- [ ] 删 `util/text.go:18` `UrlToLink`、`util/format.go` 死链（`LongTime`/`layoutDayMonth`/`Month`/`Year` + 注释块）
- [ ] 删 `util/truncate.go:40` 恒真 `isHTML` 及不可达分支
- [ ] 删 `cli/cmd/wallpaper.go:175-349` 的 175 行硬编码 `OldWallpapers`
- [ ] 删 `cli/config.toml`（另一个项目 ctdx 的配置残留）
- [ ] 删 `cli/cmd/root.go:23,64` 从未读取的 `config.debug` 字段
- [ ] 清理 `cli/tools/migrate_db.go` "debug" case 的注释死代码块（:766-774 等）与硬编码 ID

### httpd/（含前端）
- [ ] 删 `httpd/app/src/options.js`（341 行，其 import 的文件均已不存在）
- [ ] 删 `httpd/src/auth.go:157` `CurrentUserId`、`httpd/src/filter.go` 的 `timeSince` filter
- [ ] 删 `httpd/src/server.go:33-37` `Server` 四个死字段（`secretKey`/`httpclient`/`assets`/`worker`）及 `NewServer` 多余参数
- [ ] 处理 `httpd/src/account.go:12` 空 `AccountHandler`（实现重定向或删路由）
- [ ] 删 `httpd/templates/_feed.html` 及连带死逻辑（`feed.html:14-29` 的 subscribe 表单等）
- [ ] 删前端死代码：`App.css`/`logo.svg`/`utils.js dprint`、`editor.jsx`/`content.jsx`/`App.jsx` 的注释代码块、`entry.jsx` 的 console.log 与引用不存在方法的 `onFocus`
- [ ] 删各处理器的注释代码块：`httpd/src/server.go:156-177`、`main.go:51,152,205`、`feed.go:171`、`entry.go` 多处

### twitter/（Python）与部署
- [ ] 删 `twitter/client.py:130-206` 死代码 `get_ohlcs`/`adjust`（连带清理未用 import）
- [ ] 删未用 import/常量：`crawler.py:8,9,11`（datetime/json/pickle）、`client.py:4`（time）、`client.py:22`（_DESCRIPTION）、`config.py:68`（zh_names）
- [ ] 删 `twitter/crawler.py:85-89` 从未使用的 `from_feed`、:142-144 注释代码块
- [ ] 删 `fabfile.py:168-207` 过时的 `deploy_client` 任务（引用已不存在的 `client/` 目录与 upstart）
- [ ] 删 `fabfile.py` 的 `test_if`/`line_in_file` 互引用死函数与未读 env 变量
- [ ] 补齐 `twitter/pip.txt` 缺失依赖（twikit、pandas、numpy）并锁定版本

### 构建产物瘦身（httpd 二进制 105MB 的根因）
- [ ] 修 `httpd/app/scripts/publish-build.mjs`：发布前清空 `static/js`、`static/css`（保留手写 `style.css`）
- [ ] 删除 `httpd/static/js/` 下 3 套旧 CRA bundle（各 ~2MB + 6.4MB sourcemap）、旧 chunk 及 `static/css/` 重复文件

## 三、高优先级重构：机械化现代替换（go 1.21）

- [ ] `golang.org/x/net/context` → 标准库 `context`（全仓约 12 处：`server/` 7 处、`cli/cmd/` 4 处、`httpd/src/server.go:23`）
- [ ] `grpc.WithInsecure()` → `grpc.WithTransportCredentials(insecure.NewCredentials())`（`cli/cmd/root.go:42`、`httpd/main.go:202`、`server/server_test.go:508`）
- [ ] 删 `server/server.go:48` 的 `rand.Seed(...)`（Go 1.20+ 自动播种）
- [ ] 哨兵错误改 `errors.Is`：`server/auth.go:16,39`、`model/oauth.go:34`、`store/store.go:215`、`search/search.go:31`、`cli/tools/migrate_db.go:843`
- [ ] 类型断言改 `errors.As`：`server/server.go:220`、`store/store.go:300`
- [ ] `fmt.Errorf` 无格式参数 → `errors.New`（`server/server.go:154`、`job.go:129`、`model/` 多处、`cli/cmd/twitter.go:80`）
- [ ] 错误包装 `%s` → `%w`：`model/table.go:70`、`entry.go:92`、`media/media.go:84`、`cli/cmd/twitter.go:99`
- [ ] `ioutil` → `io`/`os`：`media/media.go:5,122`、`cli/cmd/wallpaper.go`、`util/config.go`
- [ ] `interface{}` → `any`：`search/search.go:35,46,77,81`、`search/mock.go`、`httpd/render.go:55`
- [ ] `endTime.Sub(startTime)` → `time.Since`（`server/server.go:205`、`stock.go` 多处、`util/format.go:23`）
- [ ] `util/config.go:26` — `NewConfigFromJSON` 失败时 `log.Fatal` 改为返回 error（4 个调用方都在等这个 err）
- [ ] `store/utils.go` — `mkdir` 简化为一行 `os.MkdirAll`，删 snake_case 的 `path_exists`
- [ ] `store/store.go:45,62` — 统一 `glog`/`log` Fatal 混用
- [ ] `media/media.go:106` — 补 `os.MkdirAll` 错误检查；写文件权限 0755 → 0644
- [ ] `media/media.go:68` — `Exists` 对 `os.ErrNotExist` 返回 `(false, nil)` 并修测试断言
- [ ] `server/command.go:19` — `Command` switch 增加 default 分支，不再吞子命令 error
- [ ] `server.go:83`（根目录）— 处理 `rpcServer.Serve(lis)` 返回的 error

## 四、中优先级：重复代码合并

- [ ] `server/stock.go` — 四个 `Archive*` 流式 handler 抽公共函数（回调或泛型），每个收敛到 ~10 行
- [ ] `server/server.go` — 合并 `ArchiveFeed`/`ForceArchiveFeed`（:189 vs :242）
- [ ] `server/server.go` — 统一 `cachedFeed`/`ForwardFetchFeed`/`Search` 三处分页逻辑，修 `found > PageSize` 的 off-by-one（实际返回 PageSize+1 条）
- [ ] `server/stock.go` — `GetStockList`/`GetStock` 抽 `loadStockList()`
- [ ] `server/command.go` — `PurgeJobs`/`FixTooMuchJobs` 双表重复改循环；`TestJob`/`RefetchUserFeed` 抽 `buildTwitterJob`
- [ ] `httpd/src/feed.go` — 五个 feed handler 的 `prevStart` + pongo2.Context 块（5×12 行）抽 helper
- [ ] `httpd/src/server.go` — 合并 `LikeHandler`/`LikeDeleteHandler`（仅差一个 bool）；合并 `ExpandCommentHandler`/`ExpandLikeHandler`
- [ ] `model/entry.go` — 合并 `FanoutEntry`/`DeleteFanoutEntry`（:66 vs :139，仅差 Index/RemoveIndex）
- [ ] `model/like.go:15,39,79` — 三处查找循环改 `slices.IndexFunc`
- [ ] `model/` — 提取 `TimelineUUID` 消除 3 处重复（`entry.go:68,141`、`server/server.go:394`）
- [ ] 删 `model/key.go:44` `KeyPrefixToBytes`，统一用 `store.KeyPrefix.Bytes()`
- [ ] 解决命名撞车：`model.KeyFromString` vs `store.KeyFromString` 同名不同义（store 侧自带 FIXME），至少一侧改名
- [ ] `store/store.go` — `NewStoreOptions`/`NewMetaStoreOptions` 25 行逐字重复抽 `configureLevels`；`NewStore`/`NewMetaStore` 合并为私有 `open()`
- [ ] `store/key.go` — 四个 key 类型的 `Bytes()` 统一实现，去 `unsafe.Sizeof` 与永不触发的 panic
- [ ] `search/mock.go` — MockIndex 改接口嵌入 `bleve.Index`，删 ~80 行手写样板
- [ ] `util/text.go` 与 `cli/cmd/twitter.go:150` — 抽公共 linkify 逻辑（三处雷同）
- [ ] `twitter/client.py:105-118` — 三个 `archive_*` 方法合并为 `_archive(rpc, func, name)`
- [ ] `twitter/crawler.py` — media→Thumbnail 转换两段重复抽函数；`fetch_user` 复用 `tweet_to_pb`
- [ ] `fabfile.py` — 三个 deploy 任务的 "code_root + git + build" 块抽 helper
- [ ] `cli/tools/migrate_db.go:539` — 335 行巨型 switch 每个 case 抽独立函数

## 五、低优先级：风格与小清理

- [ ] 日志统一：stdlib `log` / logrus `logger` / glog 混用（`server/command.go`、`httpd/src/feed.go:90`、`entry.go:129` 等）
- [ ] 命名：`uuid1` 多处、`profileUUid`（auth.go:36）、`rumCmd`→`runCmd`、`BASE_URL`→`baseURL`、`_oauthUserIdFrom`→`oauthUserIdFrom`、`Url`→`URL`（media Object）
- [ ] 拼写：`stoped`→`stopped`（根 server.go）、"Falke"→"Flake"（flake.go）、"comptabile"（model/entry.go:15）、"diable"（stock.go:31）、`EnqueJob`（job.go，改名需动 .proto）
- [ ] 冗余写法：`entries[:]`（server.go:372 等）、`[]byte(kb)`/`string(item)`（index.go:102）、`store.KeyFromString(k)[:]`、`buf.Write(b[:])`
- [ ] `time.Tick` → `time.NewTicker`（`server/job.go:17,25`）
- [ ] `helper.go:103`（server）日志文案复制错误；`stock.go:231` 注释复制错误；`store/store.go:80` 注释 `128 << 20 // 512 MB` 应为 128 MB
- [ ] `model/table.go:85`、`key.go:45` — 局部变量遮蔽 `bytes` 包
- [ ] `httpd/main.go:112` — 废弃的 gplus provider 改 `goth/providers/google`，删全局 map 篡改 hack
- [ ] `httpd/main.go:62` — 硬编码 base64 favicon 改 serve 嵌入的 `static/favicon.ico`（且 MIME 声明错误）
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
- `server/server.go:233` + `media/media.go:77` — `mirrorMedia` 媒体镜像链：`Mirror` 自初始提交起即为 stub（恒返回 not implemented），链路从未真正下载过文件，且 URL 改写发生在 `PutEntry` 之后不落库。2026-07-18 曾删除 `mirrorMedia` 及其调用，**用户决定保留该代码**（未来要实现镜像功能），已撤销删除。未来实现方向：补全 `Mirror`（`Fetch`+`Post`）并把镜像调到 `PutEntry` 之前。
