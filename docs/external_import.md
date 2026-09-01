# 外部内容导入协议

状态：V1 已实现

本文只定义 FriendFeed 外部内容导入的稳定协议与数据不变量。`twitter-import` 的构建、归档检查、
GetXAPI 同步、断点续传和 production 重放命令见 [`twitter-import.md`](twitter-import.md)。

## 1. 系统边界

外部 provider 的解析、分页、付费读取、rate limit、媒体下载和 checkpoint 属于独立 connector：

```text
provider/archive
      |
      v
independent connector
      | HTTPS + Feed credential
      v
ffweb import endpoint
  authenticate, sanitize, validate/promote media
      | loopback gRPC + trusted Feed principal
      v
ffdb
  deterministic identity, canonical Entry/index commit
```

- connector 不得直接打开 Pebble、调用 loopback gRPC 或写 ffdb 表；
- provider SDK、OAuth、Cookie、抓取 cursor、付费状态和同步任务不得进入 ffdb；
- ffdb 继续独占 canonical Entry、direct index、search、archive dirty 和媒体引用；
- import 是 push 模式，不创建 Service、ServiceState 或 FeedService；
- 普通 `POST /api/v1/feed/entries` 保持“服务端当前时间、每次生成新 UUID”的非幂等语义；历史导入
  使用独立 endpoint，不以 flag 改写普通发布；
- 历史表号 200–202 永久保留，不得复用。

## 2. 认证与目标 Feed

### Feed API key

单 Feed 导入使用现有 Bearer credential：

```text
Authorization: Bearer <feed-api-key>
```

- ffweb 复用 Public Feed API 的严格 Bearer parser 和统一 401；
- request 不携带 target Feed，canonical target 只来自 credential；
- secret 不进入 gRPC metadata、Task、日志或数据库明文字段；
- ffweb 只通过可信 loopback metadata 传递 Feed UUID 和非敏感 key ID；
- ffdb import RPC 必须要求 trusted Feed principal，拒绝缺失 principal、混用 viewer identity 或目标不一致；
- rotate/revoke 后旧 key 对 read、普通 publish 和 import 同时失效；
- personal Feed key 由 owner/super 管理，Group key 由 Group admin/super 管理；导入 Group 时 author 是
  Group，不是执行操作的 admin；
- `TableFeedApiKey = 123`、token/digest 格式和管理 RPC 均不改变。

### Import operator token

站点管理员批量导入使用全站唯一、最长一小时的 import-only token，不为每个 Feed 创建长期 key。

固定记录：

```text
TableMeta | "import-operator-token/v1" -> pb.ImportOperatorTokenRecord
```

记录只保存 8-byte key ID、32-byte secret SHA-256、创建/过期/撤销时间和诊断性 `issued_by`；明文只在
签发时写入一次 0600 文件。再次签发原子覆盖旧 token。

operator token 只能调用 `POST /api/v1/feed/imports`。connector 通过 `X-FF-Import-Target` 指定 Feed
ID/UUID；该 header 不是 credential。ffweb 必须在读取 multipart body 前同时认证 token 与 target，解析
canonical Feed UUID 后再进入媒体和导入路径。普通 Feed API key 携带该 header 必须拒绝。

operator token 不能读取 Feed、普通发布、编辑、删除或管理 Feed/API key；不得进入 argv、manifest、
checkpoint 或日志，批量导入结束后应立即 revoke。

## 3. Source identity 与 Entry UUID

每条外部内容必须携带：

```text
source.kind        provider 稳定类型，例如 "twitter"
source.account_id  provider-native immutable account ID
source.item_id     provider-native immutable item ID
source.url         canonical item permalink
```

Twitter 的 `account_id` 和 `item_id` 必须是十进制数字。username/screen name 可变，只能用于展示，不能
参与 identity。Twitter `source.url` 必须是与 `item_id` 一致的 Twitter/X HTTP(S) status permalink；
用户名路径段不参与校验，服务端不会访问该 URL。

新 Entry UUID 固定为：

```text
UniqueKeyFrom(
  "external-entry",
  target Feed UUID,
  source.kind,
  source.item_id,
)
```

target Feed 是 identity 的一部分，同一 item 导入不同 Feed 会形成独立 Entry。V1 不增加 Entry protobuf
字段、来源表或反向索引；来源展示复用 `Url`、`RawLink` 和 `Via`，幂等性只依赖确定性 UUID。

## 4. Replay 与历史兼容

导入是 create-only、幂等 mutation：

1. 计算 `UniqueKeyFrom("external-entry", target Feed UUID, "twitter", tweet ID)`；
2. 新 ID 已存在且 canonical target、结构化 URL 与 source identity 相符时，返回 `created=false`，不改写；
3. 新 ID 已存在但 target 或 tweet ID 不符时，返回 identity conflict；
4. 新 ID 不存在时，精确检查 `UniqueKeyFrom("twitter", tweet ID)`；
5. legacy ID 属于同一 target，且 `Url`、`RawLink` 或 `Via.Url` 能严格解析出相同 `status/<id>` 或
   `statuses/<id>` 时，返回 replay；
6. legacy ID 属于其他 Feed 时不占用当前 target，继续以新 ID 创建；
7. legacy ID 位于当前 Feed 但 URL 不能证明 identity 时，返回 conflict；
8. 两种 ID 都不存在时，以新 ID 创建并返回 `created=true`。

replay 不刷新日期、不改正文或媒体、不重建 derived state。V1 不自动更新或删除已导入 Entry。

更早迁移数据若没有两种确定性 UUID，由 connector 在导入前流式读取目标 Feed，从 permalink 建立本地
disk-backed tweet-ID 索引：严格命中则记 `legacy_skipped`；含糊、缺失或无法解析时不得按正文模糊匹配。
checkpoint 只是加速器，服务端的确定性 replay 才是正确性边界。

## 5. HTTP contract

```text
POST /api/v1/feed/imports
Content-Type: multipart/form-data
Authorization: Bearer <feed-api-key-or-operator-token>
```

请求字段：

```text
metadata  required JSON part
file      repeated binary parts, optional
```

`metadata` V1：

```json
{
  "source": {
    "kind": "twitter",
    "account_id": "12345678",
    "item_id": "1295071681511407617",
    "url": "https://x.com/i/status/1295071681511407617"
  },
  "published_at": "2020-08-17T12:34:56Z",
  "title": "",
  "body_html": "untrusted HTML fragment"
}
```

约束：

- source 字段必须非空、UTF-8、长度有界且不得含 NUL；provider adapter 执行额外格式校验；
- `published_at` 必须为 RFC3339Nano，可规范化到 UTC，不得晚于服务端时间五分钟；历史日期允许；
- title/body/file 复用 Public Feed API 与 media upload 的既有上限；
- 不接受 Entry UUID、ProfileUuid、FeedUuid、From、To、Via、RawBody、Commands、Like、Comment、
  remote media URL 或 storage key；
- 默认不返回 CORS header，该 endpoint 只供 server-side connector 使用。

响应：

```text
201 Created  { "created": true,  "data": <public Entry DTO> }
200 OK       { "created": false, "data": <public Entry DTO> }
409 Conflict { "error": { "code": "source_identity_conflict", ... } }
```

其他认证、格式、大小、媒体和服务错误沿用 [`web_api.md`](web_api.md) 的 JSON envelope。

## 6. ffdb mutation

外部导入使用 additive `ImportFeedEntry` RPC。RPC 只接受 source identity、published timestamp、经 ffweb
sanitize 的 title/body，以及共享 media pipeline 已验证和 promote 的 canonical refs。target Feed 只来自
trusted metadata。

ffdb 必须再次验证 source、时间、长度和 canonical media URL。mutation 在 entry lifecycle/ApplyBatch
串行边界内：

1. 计算新 UUID并精确检查 legacy UUID；
2. 读取并验证已存在 Entry；
3. replay 直接返回，不写任何 derived state；
4. create 原子提交 Entry 与 author/target direct index；
5. commit 后复用 search、archive dirty 和 gated media mirror；
6. archive import 不写 Home/Public/Group activity timeline，不发布 realtime hint，不触发 notification。

历史条目按 `published_at` 进入 direct Feed 和 search，不会在导入当天表现成新动态，也不会造成 public
trim 或 follower fanout。不得为 import 复制另一套 Entry/key/index 实现。

## 7. Canonical author

外部导入是目标 Feed 的 machine-authored content：

```text
ProfileUuid = target Feed UUID
FeedUuid    = target Feed UUID
From        = canonical target Feed snapshot
To          = empty
Via.Name    = provider/display source
Via.Url     = source canonical URL 或 provider homepage
Date        = published_at
```

personal Feed 与 Group Feed 使用同一编码。Group 内真实用户投稿仍为：

```text
ProfileUuid = user UUID
FeedUuid    = Group UUID
From        = user snapshot
To          = Group snapshot
```

connector 不得伪装 Group admin 或远端 provider 用户为本地 author。

## 8. 正文与媒体

- connector 只提交有限 HTML fragment；ffweb 必须 sanitize/linkify；
- 外部 `img/video/audio/iframe/object/embed` 节点全部剥离；mention、hashtag 和链接只能成为普通安全链接；
- provider 原始 JSON、token、Cookie 和完整正文不得写入 Entry、Task 或日志；
- connector 下载媒体后上传本地 bytes，ffweb 不接受 remote media URL，不代替 connector fetch；
- 媒体复用 [`media_upload.md`](media_upload.md) 的 MIME/magic/container、大小、数量、thumbnail、staging、
  promote、content-addressed key 和主动内容下载规则；
- 全部文件 promote 成功后才调用 ffdb；失败不得创建 Entry；
- replay 清理 staging，但不把新媒体附加到旧 Entry，也不猜测删除已 promote 的全局 canonical object；
- 只有 `created=true` 才按 `media_mirror` gate 安排 R2 mirror；
- 媒体无法恢复时允许显式降级为仅正文与 source permalink，并记录 `media_missing`。

Twitter V1 一条 tweet 对应一条 Entry；reply 默认跳过，显式选择时也只作为独立 Entry，不创建 Comment
或本地 parent；retweet/quote/thread 不创建本地关系图；删除、编辑和易变统计不回写。

## 9. 失败、重试与日志

- 200 replay：成功；
- 401/403/404：credential 或 target 永久失效，立即停止且不推进当前 checkpoint；
- 400/409：内容或 identity 永久失败，记录并跳过该 item；
- 413/415：媒体/请求永久失败，可显式降级为无媒体导入并记录；
- 429、5xx、网络超时：临时失败，遵守 Retry-After，指数退避并加入 jitter；
- 结果未知：允许重试，由 deterministic identity 收敛。

日志和 report 可以记录 source kind、account ID、item ID、HTTP status、request ID 与聚合计数；不得记录
API key、operator token、OAuth token、Cookie、完整正文、归档内容或带 secret query 的 URL。

V1 不新增专用表或 Entry 字段；`audit_store` 只校验 Meta 中 operator token 的编码与生命周期。connector
的全部扫描、归档解析和 checkpoint 必须流式且内存有界。停止 connector 即可回滚执行过程；已经创建的
Entry 是正常 canonical 数据，删除必须走正常 Entry 删除或另行设计显式、可确认的 purge。
