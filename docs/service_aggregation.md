# 服务聚合（RSS/Atom）与 Job 系统重构设计

复刻 FriendFeed 的核心前提是把外部内容汇成流。本文先审计现有 Job/爬虫架构的
缺陷，再给出替代架构：RSS/Atom 订阅模型，以及既有 Job 系统的处置方案。

## 现状审计：Job 系统与爬虫的实际形态

### 调度层已经空转

`RefetchJobTicker` 每 2 分钟调用 `RefetchUserFeed`（server/job.go:17-34），意图是
给每个绑定了 twitter service 的用户入队一个抓取 job。但 `RefetchUserFeed` 用
`model.ProfileToFeedinfo(profile)` 重建 feedinfo（server/job.go:66），该函数把
`Services` 硬编码为空切片（model/profile.go:98）；`BuildGraph` 只从这个空切片构造
`graph.Services`（server/helper.go:182-198），于是 `graph.Services["twitter"]` 恒不
命中（server/job.go:69），**该 ticker 实际入队数恒为 0**。正确的补载写法在同仓库
就有：`FetchGraph` 用 `model.GetServicesForProfile` 显式加载服务
（server/server.go:278-281）。也就是说，服务端这套"定时调度 → 队列入队"链路当前
不产生任何工作。

### 队列与 worker 的设计缺陷

1. **任务类型无法区分**：`FeedJob` 没有 type 字段（pb/api.proto:86-104）；消费端
   `fetchService` 不看 `job.Service.Id`，无条件走 Twitter API（cli/cmd/twitter.go）。
2. **死字段**：`max_limit` 仅被 `FixTooMuchJobs` 的 `==99` 分支匹配，而全仓库没有
   任何地方把它设为 99（server/command.go:129,144 为不可达代码）；`force_update`
   零使用；`target_id` 入队时从不赋值，但 `FinishJob` 用它做 history key
   （server/job.go:194）——所有 history 记录写到同一个空 meta key 上互相覆盖。
3. **入队无去重**：ticker 每 2 分钟无条件入队（server/job.go:57-96），不检查
   pending/running；`PurgeJobs`/`FixTooMuchJobs` 这类手工命令就是为擦这个屁股
   存在的。
4. **无租约与心跳**：worker ID 是进程启动时的随机 hash（cli/cmd/root.go），worker
   进程一死，job 永久卡在 `TableJobRunning`，只能人工 `RedoFailedJob` 重入队。
5. **信任边界错位**：`FinishJob` 直接 `hex.DecodeString(job.Key)` 后删除
   （server/job.go:188-189），worker 传什么删什么。
6. **job 载荷是过期快照**：整个 `Profile` + `Service`（含 OAuth token）序列化进队
   列表——敏感数据落盘副本 + 入队后改名/刷新 token 不会传播。
7. **元数据语义混乱**：`EnqueJob` 写 `Created`，`GetFeedJob` claim 时又覆盖
   `Created`（server/job.go:130），排队耗时永远丢失。
8. **worker 目标硬编码**：`UserTimelineParams{ScreenName: "yinhm"}`（cli/cmd
   /twitter.go:97）——名义上按 profile 调度，实际永远抓同一个人。
9. **空队列靠错误驱动轮询**：`GetFeedJob` 返回 error，worker 打日志 sleep 5s
   （cli/cmd/root.go:113-131），空转期每 5 秒刷一条错误日志。
10. **Python crawler 从不消费队列**：`twitter/crawler.py` 全文无
    `GetFeedJob`/`FinishJob` 调用，是 `--run init/user/list` 手动驱动的一次性
    脚本，无常驻循环、无 systemd unit、无 fab task。

### 入库路径的分裂

tweet → Entry 的转换存在三份（Python `fetch_user` → PostEntry、Python
`tweet_to_pb` + 服务端 `PostTweet`、Go worker → ArchiveFeed），UUID 方案两套：
路径 1/3 用 `uuid5(canonical_url)`，路径 2 用 `UniqueKeyFrom("twitter", tweet.Id)`
（server/server.go:1050）——同一条 tweet 经不同路径产生两个 entry，重复落库。
只有 ArchiveFeed 路径带 R2 媒体镜像（mirrorMedia，server/server.go:310）；
Python 两条路径保留 CDN 原始 URL，视频静默丢弃。`TableTweet`(6) 是 write-only
表，全仓库无读取方。

## 架构决策：订阅模型替代队列模型

RSS/Atom 聚合**不接入 FeedJob 队列**，采用订阅模型。理由：

- 周期抓取的本质是"拉状态"而非"派任务"。队列的价值是可靠投递；而抓取失败时
  下一轮条件 GET 天然重试，可靠投递毫无意义。队列在这里只贡献了去重、租约、
  尸体清理三类问题。
- 抓取状态（ETag、Last-Modified、退避）是每订阅一份的长期状态，放 job 的
  一次性历史记录里语义错位；独立状态表才是它的位置。
- 同一 URL 被多人订阅时应只抓一次：队列模型按用户入队必然重复抓取，订阅模型
  按 URL 唯一、靠既有 follow 边扇出。

> **修订**：本节"不接入队列"仅指旧 FeedJob 队列。后续评审决定 RSS 抓取的
> **执行**接入通用 Task 队列（`docs/task_queue.md`）：调度状态仍留
> SubscriptionState，调度器到期仅入队 `rss.fetch` task；下文的进程内调度器
> 并发控制由队列的进程内 worker pool 承载。

### 存储（两个新表，纯新增）

```text
TableSubscription = 111                    // 抓取源，按规范化 URL 全局唯一
key   = prefix(4) | feed UUID(16)          // feed UUID = UniqueKeyFrom("rss", normalizedURL)
value = pb.Subscription{ url, title, added_by, created }

TableSubscriptionState = 112               // 抓取状态，与源表分离避免高频重写
key   = prefix(4) | feed UUID(16)
value = pb.SubscriptionState{ etag, last_modified, last_fetch, next_fetch,
        consecutive_failures, http_status }
```

订阅关系复用既有社交图：用户订阅 = 创建 `Follow(subscriber → 合成 feed
profile)` 边（TableFollow/Follower，GraphFollow 已有的双边写入）。合成 profile
的 UUID 即 feed UUID，`From`/`ProfileUuid` 都指向它——Home timeline、fanout、
profile 页、搜索索引零改动复用。退订 = 删边；调度器跳过无任何 follower 的源
（抓取前查一次 Follower 前缀，点查成本）。

### Entry 入库

- Entry 身份统一为 `UniqueKeyFrom("rss", normalizedURL, itemKey)`；itemKey 依次取
  GUID → link → `hash(title+published)`。全链路只此一套方案，显式吸取 twitter
  双 UUID 方案的教训。
- 落库走既有 `PostEntry` 路径，获得 timeline fanout 与搜索索引。首版不导入 RSS
  远程图片，避免另开一条未经审计的下载路径；`ArchiveFeed` 的同步 mirrorMedia
  契约保持不变。重复抓取由稳定 key 检测后跳过，天然幂等。
- `Entry.Date` 取 item 发布时间（缺省回退抓取时刻），必须满足 model 的 RFC3339
  校验；`Via = { feed title, feed url }`。

### 调度器（ffdb 进程内）

```text
每 60s 流式扫描 TableSubscriptionState，挑出 next_fetch <= now 的源
  → Queue worker 全局并发上限 4、每 host 同时在飞 1 个
  → 条件 GET（If-None-Match / If-Modified-Since），304 零解析
  → 成功：自适应间隔（连续空转翻倍，30min 起、24h 封顶）
  → 最终失败：长期退避 1h 起、24h 封顶；失败次数入 State
  → 每源每次最多处理 25 个新 item，新→旧
```

- 出站立即要做 **SSRF 防护**：只允许 http/https；解析域名后拒绝
  loopback/RFC1918/link-local；跟随重定向（≤5 次）时每一跳都复查；响应上限
  5MB、总超时 15s；UA 标识 `ffdb-bot`。
- ffdb 已有出站抓取先例（mirrorMedia 拉远程媒体），调度器在 ffdb 进程内符合
  现有职责边界，不新增部署单元；loopback 红线不受影响。
- 关停挂入既有 `beginBackgroundJob`/`wg`/`Shutdown` 设施。

### 新依赖决策

新增 `github.com/mmcdole/gofeed`（RSS2/Atom/JSON Feed 解析）。理由：RSS/Atom 的
日期格式、命名空间、内容编码边缘 case 极多，手写最小解析器的长期成本高于引入
一个成熟、无传递依赖负担的库。若评审不接受新依赖，备选是限定支持 RSS2/Atom
的手写解析器并显式放弃 JSON Feed——需要在实施前单独确认。

### httpd 与前端

- 新 RPC（纯新增）：`SubscribeService`（输入 URL，规范化、查重、建源、建 follow
  边、立即异步首抓）、`UnsubscribeService`（删边）、`ListSubscriptions`。首版只
  交付 API；账户页 UI 不是 Task 队列成立条件，另行设计。

## 既有 Job 系统的处置

`EnqueJob`/`GetFeedJob`/`FinishJob` 是受保护的 RPC 面（AGENTS 兼容契约），全部
保留原路径与语义，仅供 twitter 遗留链路使用。通用队列的替代设计见
`docs/task_queue.md`。处置建议：

1. **停止 `RefetchJobTicker`**：它当前入队数恒为 0（见审计），每 2 分钟全表扫
   profile 纯属空转。与其修复为正确加载 services，不如直接停掉——Go worker 的
   抓取目标硬编码为 `yinhm`，这条链路即使修通调度也没有真实消费能力。停止方式：
   不再启动该 goroutine，代码与命令保留。
2. **Job 表（200/201/202）标注为 legacy**：不再接受新任务类型；已知缺陷（本文
   审计清单）不再逐项修补，因为唯一的理论消费方是 twitter。
3. **twitter 链路的整合是独立后续工作**：统一双 UUID 方案、让 Python 路径也走
   ArchiveFeed 以获得 R2 镜像、TableTweet write-only 表的取舍，连同
   `docs/open_decisions.md` 里的既有条目一起单独设计，不在本文范围内。

## 安全与隐私

- 订阅 URL 是用户输入：SSRF 防护见调度器一节；规范化（小写 scheme/host、去
  默认端口、去 fragment）保证同一源不产生两份订阅。
- 抓取不携带任何用户凭据；请求不转发用户 Cookie。
- 日志只记录 URL host 与状态码，不记录响应正文；任何级别不记录 token/Cookie。

## 迁移与验收

- 新表 111/112 纯新增，无需数据迁移；表号与编码按 AGENTS 流程同步进契约文档。
- `audit_store` 可后续增加"无 follower 的订阅源"统计，非首版必需。
- 验收清单：订阅一个公开 RSS → 新 item 在下次抓取周期内出现在该 feed 的
  profile 页与订阅者 Home；重复抓取不产生重复 entry；源 404/超时按退避表
  推进；退订后该源不再被调度（无其他 follower 时）。

## 测试清单

- model：两个新表的编码/解码、边界值（非法长度、零 UUID）。
- server：调度器的 due 选择、退避推进、host 串行与全局并发上限、SSRF 地址族
  拒绝（用 httptest 模拟）、Entry 幂等覆盖。
- httpd：订阅/退订 RPC 的鉴权与参数校验。
- 前端：订阅表单与列表的 vitest。
