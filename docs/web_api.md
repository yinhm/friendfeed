# Public Feed API V1 规范

> **状态：V1 定稿。** 本文是 Public Feed API V1 的权威产品与技术规范；
> `docs/web_architecture.md` 只保留总体架构和边界摘要。实现不得在未更新本文的情况下自行扩大
> endpoint、principal 或持久化语义。

## 1. 目标与非目标

V1 为外部脚本和 integration 提供一个稳定、版本化的 Feed API，使 personal Feed 和 Group
Feed 都能通过各自的 machine credential 读取和发布内容。

V1 支持：

```text
GET  /api/v1/feed
GET  /api/v1/feed/entries
GET  /api/v1/feed/entries/:entry_id
POST /api/v1/feed/entries
```

V1 不支持：

- Entry edit/delete；
- Like、Comment、Follow、Group membership 或管理；
- User OAuth token、第三方 OAuth client；
- 一个 Feed 多个 active key、named scope、IP allowlist；
- 大文件 direct upload、presigned URL；
- 独立 ffapi service 或应用层 per-key/per-IP rate limiter。

浏览器管理 key 仍使用登录 session；Public API 调用只使用 Bearer key，两种 principal 不得混用。

## 2. 信任边界

```text
Browser session user
  -> ffweb Browser BFF
  -> ffdb: authorize Feed management
  -> generate / rotate / revoke key

External client
  -> ffweb /api/v1
  -> parse bounded request and Bearer key
  -> ffdb AuthenticateFeedApiKey
  -> receive authoritative Feed UUID and non-secret key ID
  -> reuse FetchFeed / FetchEntry / PostEntry with trusted internal metadata
```

- ffweb 负责 HTTP contract、multipart、限额和 Public DTO；
- ffdb 负责 credential authority、Feed principal、private read、canonical author/target 和 Entry
  mutation；
- nginx 只能提供连接/请求体等外围限制，不能成为 credential authority；
- Public API 不复制 Feed/Entry RPC；鉴权后复用 `FetchFeed`、`FetchEntry`、`PostEntry`，仅在
  ffweb 注入可信 Feed identity metadata 时进入 machine capability 分支；
- ffdb 继续只监听 loopback，Public API 只由 ffweb 暴露。

## 3. Credential model

### 3.1 所有权和权限

每个 Feed 第一版最多一个 active API key。key 属于 Feed，不属于创建它的用户。

- personal Feed：本人或 super 可以 generate/rotate/revoke；
- Group Feed：当前 Group admin 或 super 可以 generate/rotate/revoke；
- admin 被移除不会自动撤销 Group key；只有显式 rotate/revoke 改变 key；
- deleted Feed、deleted actor、Group 非 admin 均拒绝管理操作；
- key 只代表其 Feed capability，不能继承创建者的 User 权限。

### 3.2 外部 token

固定格式：

```text
ffk1_<feed_uuid_base64url>_<key_id_base64url>_<secret_base64url>
```

- `feed_uuid`：16-byte raw UUID 的无 padding Base64URL；
- `key_id`：8-byte cryptographically random 非敏感标识，可用于安全审计；
- `secret`：32-byte cryptographically random secret；
- parser 必须拒绝未知版本、错误分段、错误长度、非法 Base64URL 和零 UUID；
- 完整 token 只在 generate/rotate 成功响应中返回一次；此后无法取回；
- URL、query、日志、错误、Task payload、Notification、HTML bootstrap 均不得包含完整 token。

token 携带 Feed UUID 是为了 O(1) 点查，不构成额外授权；ffdb 仍必须验证记录、key ID、secret
digest、Feed 状态。Feed UUID 本身不是 credential。

### 3.3 持久化

V1 固定使用：

```text
TableFeedApiKey = 123

key   = table prefix(4) | feed UUID(16)
value = FeedApiKeyRecord protobuf

FeedApiKeyRecord:
  key_id          bytes(8)
  secret_sha256   bytes(32)
  created_at_ms   int64
  rotated_at_ms   int64 (0 until first rotation)
  revoked_at_ms   int64 (0 while active)
```

secret 来自 256-bit 随机空间，持久化 `SHA-256(secret)`，比较使用 constant-time compare。不得把
token 或 secret 明文写入 Pebble。每次 rotate 生成全新 key ID 和 secret，单 batch 覆盖记录；旧
token 在提交后立即失效。revoke 保留 key ID 和时间但清空 secret digest，便于管理 UI 显示状态。

表号和编码在实现时必须同时登记到 `model/types.go`、`docs/database_design.md` 和根
`AGENTS.md`。这是向前兼容的可空新表，不要求重写旧记录或提升 application schema marker。

## 4. ffdb API 与状态转换

只为 API key 生命周期与认证新增 additive protobuf RPC：

```text
GetFeedApiKeyStatus(actor_uuid, feed_uuid)
GenerateFeedApiKey(actor_uuid, feed_uuid)
RotateFeedApiKey(actor_uuid, feed_uuid)
RevokeFeedApiKey(actor_uuid, feed_uuid)
AuthenticateFeedApiKey(feed_api_key)
```

管理 RPC 返回 metadata；只有 Generate/Rotate 返回一次完整 token。Generate 在已有 active key 时
返回 `AlreadyExists`，不能隐式 rotate。Rotate 要求 active key，Revoke 幂等。

`AuthenticateFeedApiKey` 返回权威 Feed UUID 与非敏感 key ID；raw key 只进入这一个 loopback
RPC，interceptor 与日志不得输出 request。ffweb 不缓存认证结果，每个 Public HTTP 请求只认证
一次，随后复用现有业务 RPC。rotate/revoke 从下一个 HTTP 请求起生效；不为消除这段极短窗口
复制三套数据 RPC。

ffweb 在 outgoing gRPC context 中加入仅供内部使用的 Feed identity metadata：

```text
x-ff-feed-uuid = canonical Feed UUID
x-ff-feed-key-id = non-secret key ID
```

外部 HTTP 同名 header 必须丢弃，不能透传。metadata 只有 loopback ffweb 可以产生；ffdb 的三个
既有 RPC 在 metadata 缺失时保持原有用户语义，在 metadata 存在时执行以下额外约束：

- `FetchFeed`：请求目标必须等于 Feed UUID，并允许该 capability 读取自身 private Feed；
- `FetchEntry`：Entry 的 effective target（FeedUuid，历史空值回退 ProfileUuid）必须等于 Feed UUID；
- `PostEntry`：忽略客户端 identity/provenance，按 Feed UUID 派生 machine author、target、From、
  To、Via、Entry UUID 和服务端时间；Group 使用本文定义的 Group machine author 语义。该分支
  固定为 create-only，不接受或复用现存 Entry ID，不得进入既有 PostEntry 的编辑路径。

该分支不是 User principal，不能调用 Like、Comment、Follow、Group 管理或其他用户 mutation。

## 5. HTTP authentication 与错误契约

请求必须使用：

```http
Authorization: Bearer <feed-api-key>
```

不接受 query key、Cookie fallback、Basic auth 或 body credential。missing、malformed、unknown、
revoked key 对外统一为 `401 invalid_api_key`，不暴露哪个校验阶段失败。权限变化导致 Feed 不可用时
返回稳定的 `403 forbidden` 或 `404 not_found`，不得原样透传 gRPC message。

所有响应包括：

```http
Content-Type: application/json
Cache-Control: no-store
X-Content-Type-Options: nosniff
```

V1 默认不返回 CORS header，不允许任意第三方网页在浏览器中直接调用。外部 integration 使用
server-side HTTP client；未来若要开放浏览器 client，必须另行定义 origin allowlist。

错误结构固定为：

```json
{
  "error": {
    "code": "invalid_request",
    "message": "Request is invalid",
    "request_id": "..."
  }
}
```

V1 code 集合：

| HTTP | code | 语义 |
| --- | --- | --- |
| 400 | `invalid_request` | 字段、cursor 或 multipart 错误 |
| 401 | `invalid_api_key` | 缺失或无效 credential |
| 403 | `forbidden` | credential 有效但 Feed 状态拒绝操作 |
| 404 | `not_found` | Entry 不存在或不属于 principal Feed |
| 413 | `payload_too_large` | body/file/总请求超限 |
| 415 | `unsupported_media` | 文件类型或内容不支持 |
| 500 | `internal_error` | 未分类服务端错误 |
| 503 | `unavailable` | ffdb/存储暂时不可用 |

message 是稳定、安全、可展示的概述，不含内部路径、UUID 之外的存储细节、secret 或正文。

## 6. Read contract

### 6.1 Feed DTO

```json
{
  "data": {
    "id": "book-club",
    "uuid": "...",
    "name": "Book Club",
    "description": "...",
    "picture_url": "...",
    "type": "group",
    "private": true
  }
}
```

只能返回 key 自身 Feed。private Feed key 可以读取自己的 metadata/content；deleted Feed 返回
`forbidden`，不降级为公开读取。

### 6.2 Entry DTO

```json
{
  "id": "entry-uuid",
  "title": "optional title",
  "body_html": "server-sanitized HTML",
  "created_at": "RFC3339Nano",
  "author": {"id": "...", "name": "...", "picture_url": "..."},
  "feed": {"id": "...", "name": "..."},
  "via": {"name": "FriendFeed API"},
  "images": [{"url": "...", "thumbnail_url": "...", "width": 0, "height": 0}],
  "files": [{"url": "...", "name": "...", "mime_type": "...", "size": 0}]
}
```

不返回 `rawBody`、Commands、Like/Comment 集合、OAuth、internal key、asset token、staging URL 或
protobuf 未声明字段。新增 protobuf field 不得自动进入 DTO。

### 6.3 Pagination

```json
{
  "data": [...],
  "pagination": {"next_cursor": "opaque-or-empty"}
}
```

- 默认 limit 50，范围 1–100；
- cursor 使用现有 direct Feed cursor 的 opaque string，但 API contract 不承诺编码；
- cursor 只在同一 credential Feed 下解释；错误长度/编码返回 400；
- response 不提供 `start`、prev cursor 或总数；
- Entry 的 canonical target 是非空 `Entry.FeedUuid`；仅为兼容历史 personal Entry，空值回退到
  `Entry.ProfileUuid`。canonical target 必须等于 principal Feed UUID。Group 成员投稿因此按
  `FeedUuid = Group UUID` 与列表 direct index 保持一致，不能仅因知道 Entry UUID 跨 Feed 获取。

## 7. Write contract

### 7.1 Request

V1 接受 `multipart/form-data`：

```text
title             optional UTF-8, max 512 bytes
body_html         optional sanitized HTML fragment, max 256 KiB
file              repeated, follows media upload limits
```

`title`、非空 `body_html`、至少一个 `file` 三者必须至少存在一项。

不接受 `raw_body`、ProfileUuid、FeedUuid、From、To、Via、created_at、storage path、remote image
URL、Commands、Like 或 Comment。`body_html` 进入现有 sanitize/linkify pipeline；服务端生成日期、
identity、provenance 和 canonical media refs。HTML 中的 `img`、`picture`、`video`、`audio`、
`object`、`embed`、`iframe` 等外部媒体节点必须剥离；V1 图片和附件只能通过 multipart 上传，
不能借正文触发 remote fetch 或持久化外站 URL。

### 7.2 非幂等语义

V1 的 `POST` 明确是非幂等操作：每个成功请求创建一条新 Entry。客户端在网络超时后盲目重试
可能产生重复内容，应先通过 Entry list 确认结果。V1 忽略且不储存 `Idempotency-Key`，不新增
幂等表、request hash、replay 或 tombstone 语义；出现真实自动重试需求后再作为独立版本能力设计。

create-only 必须在 ffdb 的可信 metadata 分支再次执行，而不能只依赖 HTTP route：ffdb 无条件
生成新的 Entry UUID，并拒绝任何引用、覆盖或复用已有 Entry 的执行模式。Public API V1 不得因
复用 `PostEntry` 而继承其编辑能力。

### 7.3 Canonical machine author

ffdb 根据已认证的 Feed identity 派生：

```text
ProfileUuid = principal.FeedUUID
FeedUuid    = principal.FeedUUID
From        = canonical Feed snapshot
To          = empty（machine author 与 canonical target 是同一个 Feed）
Via.Name    = "FriendFeed API"
Via.Url     = empty
Date        = server time
```

Group key 创建 Group machine-authored Entry 延续 FriendFeed 历史上 Group Service 导入的既有
领域语义。普通用户向 Group 投稿仍为
`ProfileUuid = user UUID, FeedUuid = Group UUID, From = user snapshot, To = Group snapshot`。
Public API 不能伪装 admin，也不产生“通知作者本人”的用户语义。历史归档中 FeedUuid 为空的
Group machine Entry 仅作为读取兼容；API 新写入必须使用上述完整 canonical identity。

Entry、author/group direct index、Home/Public timeline、realtime、search、archive dirty 和 media
mirror 必须复用正常 Entry mutation 的不变量；不能复制一套简化写路径。

## 8. Media

Public API multipart 必须复用 `docs/media_upload.md` 的服务端 pipeline：类型/magic/container 验证、
限额、thumbnail、staging、promote、content-addressed canonical key 和主动内容下载策略。

- Public API 不暴露 browser asset token；
- 上传或 promote 失败不得提交 Entry；
- promote 后 domain mutation 失败允许留下 canonical orphan，由未来 GC 处理；
- R2 mirror 仍受 `media_mirror` gate，best effort，不是发布前置条件；
- V1 不接受 remote URL mirror，避免把 Public API 变成 SSRF fetch endpoint。

为了避免两套安全逻辑，实施前先把 browser upload 中可复用的验证/promote 部分抽为内部 helper；
Browser 与 Public API 只保留不同的 authentication 和 transport adapter。

## 9. 限额与可观测性

V1 固定：

- HTTP request body、文件数、单文件和总大小沿用 media upload 上限；
- list limit 最大 100；
- ffweb Public API handler 全局并发上限 32，满时返回 503；
- 配合 HTTP server timeout 和 nginx 现有请求体/连接边界；
- V1 不实现 per-key、per-IP 或 unauthenticated token bucket。256-bit secret 不以防暴力猜测为
  目标；若出现实际滥用，再基于可信代理 client IP 和部署拓扑单独设计限流。

安全日志只允许：request ID、route、status、latency、Feed UUID、非敏感 key ID。严禁 Bearer
header、secret、Cookie、multipart 内容、Entry body、filename 全路径或 gRPC request dump。

Generate/Rotate/Revoke 还必须记录不含 secret 的管理审计日志：actor UUID、Feed UUID、key ID、
action、结果和时间。不得记录响应 token。

## 10. Browser key management

登录态管理界面增加 `API` 页面：个人 Feed 从 `/account/profile` 的 account nav 进入；Group
管理员先进入 Settings，再从 Group management nav 进入。普通 Feed header 不显示 API 入口。

```text
/feed/:id/api
```

- 仅 owner / Group admin / super 可访问；
- 显示 active/revoked、key ID、created/rotated/revoked 时间；
- Generate/Rotate 使用原生 confirmation popover；Revoke 必须二次确认；
- 完整 token 只在 mutation 成功后显示一次，并明确提示立即复制；
- 页面刷新后不得再次获得 token；
- token 不进入 URL、history、localStorage、analytics 或 server bootstrap；只保存在当前 React state；
- API 页面 UI 权限不是权威边界，ffdb RPC 必须重复授权。

## 11. Compatibility 与发布

- 所有 protobuf 变更 additive；不得更改 legacy RPC field number 或语义；
- 新表号 123 永久固定，旧二进制忽略新表；
- API 路径始终包含 `/v1`；V1 DTO 只能 additive 增可选字段，破坏性变化使用新版本；
- nginx、ffweb 和 ffdb 必须按 ffdb -> ffweb 顺序部署，旧 ffweb 不调用新 RPC；
- key 管理 UI 和 Public endpoint 在 ffdb 支持上线后才能启用；
- rollback 到不认识新表的旧版本不会删除 key row，但 Public API 暂停服务；
- release 前必须在日志、heap/profile、HTTP error 和 browser history 中做 credential 泄漏审计。

生产部署顺序、curl 验收、rotate/revoke、泄漏响应、回滚与 canonical media orphan 处置见
`docs/web_api_operations.md`。

## 12. 实施基线

V1 实施复用以下现有边界，不另造第二套默认值：

- 单文件及 browser upload request 上限以 `media.MaxUploadFileBytes` 和现有 upload handler 为准；
- Entry 写入必须经过 `ApiServer.postEntry`、`model.PutEntryWithTimelineObserver` 及其 public timeline、
  realtime、search、archive dirty、media mirror hooks；
- Feed 管理权限复用 personal owner、Group admin、super 的既有领域判断；
- direct Feed 分页复用 `FetchFeed` 的 opaque cursor，不解释其编码，也不开放 Start/PageSize；
- Phase 0 golden fixtures 位于 `httpd/testdata/public_api_v1`，生产 route 在 transport phase 前保持
  未注册。
