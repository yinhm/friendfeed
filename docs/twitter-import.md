# Twitter Import 使用手册

`twitter-import` 是独立于 ffdb 的 Twitter/X 导入工具。它不直接打开 FriendFeed 数据库，只通过 HTTPS
import API 写入数据。数据契约、安全边界和确定性 replay 语义以
[`external_import.md`](external_import.md) 为准。

## 1. 构建与密钥

```bash
git clone https://github.com/yinhm/twitter-import.git
cd twitter-import
go build ./cmd/twitter-import
```

密钥必须保存在普通的 `0600` 文件中，不得放进命令行参数：

```bash
chmod 0600 api-key operator-key getx-api-key
```

- `api-key`：单个 Feed 的 Public Feed API key；
- `operator-key`：管理员签发的最长一小时 import-only token；
- `getx-api-key`：付费 GetXAPI 凭据。

## 2. 检查 Twitter 官方归档

完全本地、只读，不连接 FriendFeed：

```bash
./twitter-import inspect ~/backup/twitter.zip
```

输出条目、回复、转推、引用、媒体和时间范围统计。

## 3. 向单个 Feed 导入官方归档

```bash
./twitter-import import ~/backup/twitter.zip \
  --endpoint https://friendfeed.example \
  --key-file ./api-key \
  --state ./twitter-import.db \
  --report ./twitter-import.jsonl
```

默认跳过 reply，与历史 FriendFeed Twitter 导入行为一致。确实需要 reply 时显式增加：

```text
--include-replies
```

`state` 是断点数据库，`report` 是追加写入的结果记录。重跑依赖确定性 Entry identity 返回 replay，不会
重复创建同一条 tweet。

## 4. 管理员批量导入 ZIP

先在 ffdb 项目中签发短期 operator token：

```bash
./cli import-token issue --ttl 1h --out /secure/path/operator-key
```

先验证 manifest 和全部归档，不执行写入：

```bash
./twitter-import batch ./output/manifest.json \
  --endpoint https://friendfeed.example
```

确认后执行：

```bash
./twitter-import batch ./output/manifest.json \
  --endpoint https://friendfeed.example \
  --key-file ./operator-key \
  --apply
```

完成后立即撤销：

```bash
./cli import-token revoke
```

operator token 只能调用 import endpoint，不能读取 Feed、普通发帖或执行管理操作。

## 5. GetXAPI 账号映射

`collect` 和 `sync` 都使用 TSV 把不可变 Twitter user ID 映射到 canonical FriendFeed Feed UUID：

```bash
./cli export-twitter-users --out ./twitter-users.tsv
```

命令通过 loopback ffdb 只读导出，目标文件必须不存在并以 0600 创建。

```text
feed_id	feed_uuid	twitter_username	twitter_user_id	boundary_tweet_id	boundary_at
alice	9e43d39c-2358-40a4-80ab-08a79a7b21e2	AliceX	42	100	2026-01-12T13:44:55Z
```

身份以 `feed_uuid` 和十进制 `twitter_user_id` 为准；用户名只用于操作时识别，改名不影响 identity。CLI
导出 TSV 时把该 Feed 最新的既有 Twitter 条目固定为 `boundary_tweet_id` 和 `boundary_at`。`sync` 遇到该
ID，或时间不晚于该时间点的 tweet，即结束该用户的增量区间；`--resume` 始终复用 TSV 中的固定值，不会
因刚导入的条目移动边界。两列可由操作者在同步前清空或调整，`--full` 忽略边界。

## 6. GetXAPI collect：只采集，不导入

```bash
./twitter-import collect \
  --accounts-file ./twitter-users.tsv \
  --getxapi-key-file ./getx-api-key
```

默认每用户最多 100 条并写入 `./output`：

```text
output/
├── user-<twitter-user-id>.zip
├── manifest.json
└── state/
```

常用选项：

```text
--limit 20             调整每用户采集上限
--no-media             不下载媒体，用于低成本 canary
--output /path/output  修改输出目录
```

`collect` 的目标是生成完整离线包，因此媒体下载失败会中止；它不会向 FriendFeed 写入数据。

## 7. GetXAPI sync：日常增量同步

推荐的常规命令：

```bash
./twitter-import sync \
  --accounts-file ./twitter-users.tsv \
  --endpoint https://friendfeed.example \
  --getxapi-key-file ./getx-api-key \
  --operator-key-file ./operator-key
```

默认行为：

- 每个账号从最新页开始，最多检查 100 条；
- 遇到服务端 replay 后停止该账号，不付费遍历已建立的历史；
- 默认跳过 reply；
- 媒体下载失败记为 `media_missing`，降级为纯文本导入；
- 内容级永久错误记为 `rejected` 并继续；凭据错误和耗尽重试的临时错误中止；
- GetXAPI 单账号 401/403/404 记为 `account_unavailable` 并继续后续账号；429、5xx 或网络错误仍立即
  停止。若所有账号均不可访问，命令最终返回非零，避免把 key/套餐问题误报为成功；
- 串行执行，不隐藏并发，也不自动重试付费 GetXAPI 读取；
- 默认输出目录是 `./output`，可用 `--output` 修改。

每个付费页面在导入前原子保存：

```text
output/
├── manifest.json
├── twitter-sync.json
├── twitter-sync.jsonl
├── replay-state/
└── <twitter-user-id>/
    └── page-<first-id>-<last-id>.zip
```

包含新建 Entry 的页面永久保留；只包含 replay 的临时页面删除。同名页面 ZIP 不覆盖。

### 7.1 继续未完成的同步

```bash
./twitter-import sync \
  --accounts-file ./twitter-users.tsv \
  --endpoint https://friendfeed.example \
  --getxapi-key-file ./getx-api-key \
  --operator-key-file ./operator-key \
  --resume
```

`--resume` 优先消费本地 ZIP，不重新读取同一付费页面。某用户没有 continuation 时，从该用户最新页正常
开始，不会跳过首次同步账号。保存页耗尽后，如果存在 `NextCursor`，仍会请求新的 GetXAPI 页面；它避免
重复费用，但不是零费用模式。

普通 `sync` 总是检查最新页，同时保留旧 continuation；多个未完成区间按每用户位置栈保存，不会被新的
增量检查覆盖。

### 7.2 明确执行完整遍历

```bash
./twitter-import sync \
  --accounts-file ./twitter-users.tsv \
  --endpoint https://friendfeed.example \
  --getxapi-key-file ./getx-api-key \
  --operator-key-file ./operator-key \
  --full
```

`--full` 会清除该用户未完成的 continuation、忽略 replay 和每用户 limit，并保留全部分页 ZIP，可能产生
大量 GetXAPI 费用。`--full` 与 `--resume` 不能同时使用。

## 8. 从 dev 离线重放到 production

dev 同步完成后，不应在 production 再次调用 GetXAPI 或下载媒体。`sync` 已根据所有保留的分页 ZIP 原子
维护 `output/manifest.json`。

复制整个产物目录：

```bash
rsync -a ./output/ production:/path/to/twitter-output/
```

在 production 签发新的短期 operator token，然后先验证：

```bash
./twitter-import batch /path/to/twitter-output/manifest.json \
  --endpoint https://friendfeed.example
```

确认后重放：

```bash
./twitter-import batch /path/to/twitter-output/manifest.json \
  --endpoint https://friendfeed.example \
  --key-file ./operator-key \
  --apply
```

生产重放使用：

```text
output/replay-state/<twitter-user-id>.db
output/production-replay.jsonl
```

不得把 dev 的 `twitter-sync.json` 当成 production checkpoint。重复执行 production batch 是安全的：服务端
根据确定性 identity 返回 replay；生产过程不会访问 GetXAPI。

## 9. 推荐操作顺序

小规模验证：

```text
准备两账号 TSV
→ 签发 dev operator token
→ sync --limit 5 --no-media
→ 检查 Feed、twitter-sync.jsonl 和 ZIP
→ 正常 sync
→ capped=true 时运行 sync --resume
→ 撤销 dev token
→ 复制 output 到 production
→ 签发 production token
→ batch dry-run
→ batch --apply
→ 检查 production-replay.jsonl
→ 撤销 production token
```

任何日志、manifest、ZIP、state 或 report 都不得包含 GetXAPI key、Feed API secret 或 operator token。
