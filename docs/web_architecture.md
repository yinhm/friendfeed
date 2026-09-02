# Web 架构与演进规范

> **状态：Alpha。** 本文用于指导 Web 架构演进，目标边界已基本确定，但分阶段方案和 Public API 设计仍可能随实现验证调整；尚不构成稳定的外部兼容承诺。

本文定义 FriendFeed Web 层的当前架构、职责边界和下一阶段演进路线。目标不是把现有 Go Web 服务替换为另一套技术栈，而是在保持 `ffdb` 领域边界和运行稳定性的前提下，逐步把 Web UI 从“Pongo2 SSR + React 二次渲染”收敛为“Go BFF + React 主导 UI”，并为正式 Public Feed API 保留清晰、稳定的接口边界。

本文是 Web 架构的总规范。Feed、Group、Notification、Realtime、权限、Task、数据库和媒体上传等领域不变量仍分别以现有文档为准；发生冲突时，领域文档中的 source-of-truth / authorization / persistence 约束优先。媒体格式、限额、staging、promote、下载与 R2 mirror 的完整契约以 [`media_upload.md`](media_upload.md) 为准。

---

## 1. 目标与非目标

### 1.1 当前目标

Web 层需要同时满足：

1. 保持现有 Go `ffweb/httpd` 的稳定运行和单二进制部署优势；
2. 保持 `ffdb` 是业务领域、权限、数据持久化和 mutation invariant 的权威后端；
3. React 成为新 Web UI 的默认实现方式，逐步减少 Pongo2 中的页面业务结构；
4. 不为了“技术栈统一”重写 OAuth、session、SSE、gRPC、upload、permission adapter；
5. Public Feed API 与浏览器 BFF 明确分层，避免把外部 API 契约绑到 React 或内部 protobuf；
6. 所有迁移按页面/能力小步进行，每一步都能单独验证和回滚；
7. 当前项目规模下优先简单、明确、可运维的方案，不为理论上的超大规模提前引入 Node SSR 集群、API Gateway、Kubernetes 或额外服务。

### 1.2 非目标

本轮 Web 演进明确不做：

- 不把 `ffdb` 重写为 Node.js；
- 不把 production gRPC 暴露给浏览器或公网；
- 不要求一次性把所有 Pongo2 页面迁成 React；
- 不要求一次性改成纯 CSR；
- 不要求引入 Next.js / Remix / React Server Components；
- 不要求把所有 HTTP endpoint 重新设计成 REST；
- 不要求把现有内部 protobuf 当成 Public API schema；
- 不因为 Web 重构改变 Pebble schema、Feed/Entry/Group/Notification 权限规则；
- 不在没有真实需求前拆出独立 `ffapi` 服务。

---

# 2. 当前架构

## 2.1 进程与网络拓扑

当前生产拓扑：

```text
Browser
   |
   | HTTPS
   v
nginx
   |\
   | \-- canonical media --> media_path
   |
   | HTTP / SSE
   v
ffweb / httpd (Go) ---- staging / canonical promote ----> media_path
   |
   | loopback gRPC
   v
ffdb (Go)
   |\
   | \-- background media mirror task --> R2 replica
   v
Pebble / background task workers
```

核心边界：

- nginx 是公网入口；
- `ffweb` 是唯一浏览器 HTTP application server；
- `ffdb` 只监听 loopback gRPC，不对公网暴露；
- 浏览器不能直接调用 `ffdb`；
- `ffweb` 通过生成的 Go protobuf client 调用 `ffdb`；
- 用户上传先落本机 staging，发布时由 `ffweb` promote 到本机 canonical media；
- production canonical media 由独立 media origin/nginx 提供，R2 是异步 replica，不进入发布同步事务；
- 业务领域校验不能只存在于 `ffweb`，最终 mutation authorization 必须由 `ffdb` 保证。

这一边界必须继续保留。

---

## 2.2 ffdb 的职责

`ffdb` 是业务后端和 source of truth，当前包括但不限于：

- Profile / Feed / Group；
- Entry / Comment / Like；
- Follow / Follower / FollowRequest；
- GroupAdmin；
- FeedService / ServiceState；
- Timeline / Public timeline；
- Notification；
- Task queue；
- Search；
- 数据库持久化与索引；
- mutation concurrency / atomicity；
- visibility / authorization 的最终领域校验；
- background lifecycle；
- realtime event 的领域来源；
- Entry 中结构化 media refs 的持久化，以及新增 canonical refs 的 R2 mirror task。

Web 架构演进不得把这些权威规则复制到 React。

原则：

```text
React decides presentation.
ffweb decides Web transport/session adaptation.
ffdb decides domain truth and final authorization.
```

例如，Web 可以根据当前用户状态隐藏“发布”按钮，但 `ffdb.PostEntry` 仍必须自己验证该 principal 是否允许向目标 Feed 发布。

---

## 2.3 ffweb/httpd 当前职责

当前 `httpd` 并不是一个纯静态服务器，而是完整的 Web application / BFF 层。

主要职责：

### HTTP transport

- Gin router；
- request parsing；
- form / multipart；
- redirect；
- HTTP error mapping；
- static/media serving；
- body size limit。

### Browser identity

- Cookie/session；
- LoginRequired；
- OAuth；
- CurrentUser / CurrentGraph；
- 浏览器 session 到内部 actor UUID 的转换。

### Backend adaptation

- gRPC client；
- protobuf request 构造；
- gRPC error 到 HTTP response 的转换；
- 针对页面的 DTO / context 组装；
- 少量 Web cache。

### Web-specific capability

- 图片、远程图片与文件附件上传；
- staging 生命周期、asset token 校验与 publish-time canonical promote；
- Plate 剪贴板图片接入和上传错误呈现；
- SSE endpoint；
- asset manifest；
- embedded static files；
- graceful HTTP shutdown。

### Server-side rendering

- Pongo2 templates；
- layout/sidebar；
- Feed/Entry 首屏；
- Group 页面；
- Account 页面；
- Notification 页面；
- 一部分管理页面。

因此目前的 `ffweb` 同时承担：

```text
BFF responsibilities
+
SSR/UI responsibilities
```

下一阶段的方向不是删除前者，而是逐渐减少后者。

---

## 2.4 当前 React 层

当前前端已经是完整 Node.js build toolchain：

- Node 24；
- pnpm；
- Vite；
- React 19；
- TypeScript type checking；
- oxlint；
- Vitest；
- Playwright；
- Tailwind；
- Plate/Slate editor。

Node.js 当前是 build-time runtime，不是 production Web runtime。

生产结构：

```text
pnpm/vite build
      |
      v
hashed JS/CSS/assets
      |
      v
embedded into ffweb Go binary
```

这一点具有明显运维优势：生产环境运行 `ffweb` 时不依赖 `node_modules` 或 Node application server。

---

## 2.5 Feed 渲染边界（已收敛）

登录态 Feed 已不再双渲染：

```text
GET /feed/:id
      |
      v
Go fetches protobuf Feed
      |
      +--> authenticated: typed bootstrap -> app_shell.html -> one React root
      |
      +--> anonymous public target: readable Pongo2 Feed HTML
                                   + typed bootstrap for progressive enhancement
```

`app_shell.html` 只输出文档壳、typed bootstrap 和一个 `#app-root`。登录态页面的
navigation、sidebar、Search 与页面内容都由这个 root 管理；不再使用 `window.appData`、
`window.accountData` 或独立 Search root。匿名 Public/Feed/Entry 仍使用 `feed.html`，其
React 渐进增强使用 `createRoot` 替换 SSR tree，并非 `hydrateRoot`。

### 当前不应立即删除匿名 SSR

未登录用户访问 Public、公开 Feed 和公开 Entry 时，SSR 仍有实际价值：

- 首屏内容立即可见；
- JS 失败时仍有可读内容；
- Public/Entry 对 crawler/social preview 更友好；
- 调试时页面不依赖完整客户端启动。

这项价值不适用于登录态：登录用户已经依赖完整交互、session DTO 和 React，继续输出同一批 Entry SSR DOM 只会造成重复渲染与维护。目标边界固定为：

```text
anonymous /public, /feed/:name, /e/:uuid, /groups
  -> readable Pongo2 SSR (可由 React 渐进增强)

authenticated Home/Public/Feed/Entry
  -> thin bootstrap + React complete render
```

取消匿名 Public/Feed/Entry SSR 必须是单独决策，不能作为普通 React refactor 顺带完成。

---

# 3. 目标架构

## 3.1 总体目标：Go BFF + React 主导 UI

目标拓扑：

```text
Browser / React
      |
      | cookie session / JSON / form / SSE
      v
+---------------------------------------------+
| ffweb (Go)                                  |
|                                             |
| Browser BFF                                 |
| - routing / session / OAuth                 |
| - Web authorization adaptation              |
| - gRPC client                               |
| - browser-safe DTO                          |
| - SSE                                       |
| - upload/media                              |
| - HTML bootstrap where needed               |
|                                             |
| Public Feed API                             |
| - /api/v1/*                                 |
| - Bearer Feed API Key                       |
| - stable public JSON/multipart contract      |
+----------------------+----------------------+
                       |
                       | internal loopback gRPC
                       v
+---------------------------------------------+
| ffdb                                        |
| - domain source of truth                    |
| - final authorization                       |
| - Entry/Feed/Group/Notification/etc.         |
| - Feed API key authoritative state          |
| - persistence / tasks / realtime            |
+---------------------------------------------+
```

React 逐步负责：

- Feed interactive UI；
- Entry；
- composer/editor；
- Account；
- Import；
- Group 管理；
- Notification；
- 后续新页面。

Pongo2 最终主要保留：

- layout/bootstrap；
- 明确需要 SSR 的页面或 fallback；
- 404/403 等简单 server pages；
- 尚未迁移页面。

---

## 3.2 BFF 的定义

本文中的 BFF（Backend for Frontend）专指：

> 为 Web 浏览器这个客户端提供身份、transport、DTO 和后端调用适配的服务端层。

BFF 不应成为新的业务 source of truth。

适合放在 ffweb 的逻辑：

- Cookie/session；
- OAuth callback；
- CSRF/browser transport；
- browser route；
- 调 ffdb RPC；
- 把 protobuf 转成 browser DTO；
- Web-only asset URL；
- SSE connection；
- upload parsing；
- HTTP cache/header；
- HTML bootstrap。

不应该只存在于 BFF 的规则：

- Group admin 是否有最终 mutation 权限；
- private Feed 是否可见；
- Entry 是否允许写入某 Feed；
- FollowRequest 是否可批准；
- Feed API Key 是否最终授权某个 Feed；
- canonical Entry author/target；
- 持久化状态 transition。

这些必须在 ffdb 有最终校验。

---

## 3.3 Browser BFF API 与 Public API 分离

Public Feed API V1 的 endpoint、credential、DTO、持久化和验收细节统一见
`docs/web_api.md`，生产操作见 `docs/web_api_operations.md`；历史外部内容导入扩展见
`docs/external_import.md`。本文件只保留 Web 总体分层，若出现
冲突，Public API 专项规范优先。

必须区分两类接口：

### Browser BFF endpoint

面向本站 React/browser：

```text
/a/*
或未来 /web-api/*
```

特点：

- Cookie session；
- 同源；
- 可以为当前 React UI 专门定制；
- 可随 Web 页面一起演进；
- 不承诺第三方长期兼容；
- 可以返回页面专属 DTO。

### Public Feed API

面向外部脚本、应用和 integration：

```text
/api/v1/*
Authorization: Bearer <feed-api-key>
```

特点：

- 是正式对外契约；
- 必须版本化；
- 不能绑定 React 内部状态；
- 不能直接暴露 protobuf；
- 必须有稳定错误格式和 pagination contract；
- credential 生命周期独立于 browser session。

两者可以暂时共用 `ffweb` 进程，但不能在语义上混为同一 API。

---

# 4. Public Feed API 架构

## 4.1 Credential model

当前设计采用：

> 每个 Feed 独立 API Key，而不是每个 User 一个 API Key。

原因：

- API 的核心资源就是一个 Feed；
- personal Feed 和 Group Feed 统一建模；
- Group 不属于某个管理员个人；
- key 泄露的 blast radius 限制在一个 Feed；
- API Key 本身可以作为 Feed machine capability。

第一版约束：

```text
Feed -> 0 or 1 active API key
```

未来如有明确需求，可扩展为一个 Feed 多 key；第一版不实现 named keys、IP allowlist、OAuth client 或复杂 scope RBAC。

---

## 4.2 API Key ownership

API Key 属于 Feed，不属于生成它的管理员。

例如：

```text
Group G
  admins: Alice, Bob
  API key: key_G
```

Alice 可以因为当前 admin 权限 generate/rotate/revoke `key_G`，但：

- key 不代表 Alice；
- Alice 被移除 admin 后，Group integration 不应自动失效；
- Alice 失去之后管理 key 的权限；
- 只有显式 rotate/revoke 才使旧 key 失效。

---

## 4.3 API Key persistence

API Key authoritative record 必须在 ffdb/Pebble。

不得：

- 放在 ffweb config；
- 只放内存；
- 只依靠 nginx；
- 在日志中记录完整 key；
- 持久化明文 secret。

建议记录概念字段：

```text
feed_uuid
key_id
secret_hash
created_at
revoked_at (optional)
```

API secret 必须使用 cryptographically secure random 生成，只在创建/rotate 时完整返回一次。

由于 API key 是高熵随机 secret，存储可使用安全的 keyed hash/HMAC 或 cryptographic hash 比较；不要把完整 secret 作为 Pebble key/value。

API key 的具体 table number/key encoding 必须在实现阶段单独加入 `database_design.md` 和根 `AGENTS.md`，本文不预留表号。

---

## 4.4 Feed principal

认证成功后，ffweb 得到权威 Feed identity：

```text
FeedUUID
KeyID (non-secret)
```

Public API 请求不应该允许调用方决定 author UUID。

Feed API key 是 machine capability，只能向它绑定的 Feed 发布。客户端不能提交或覆盖 author/target：

```text
Bearer key
    |
    v
AuthenticateFeedApiKey
    |
    v
ffweb { FeedUUID = F, KeyID }
    |
    +-- trusted internal metadata --> existing PostEntry
    |
    +--> ProfileUuid = F (server derived)
    +--> FeedUuid    = F (server derived)
    +--> From        = canonical Feed snapshot
    +--> Via.Name    = "FriendFeed API" (server derived)
    +--> Via.Url     = empty
```

对 personal Feed，`F` 同时是用户 Profile 与目标 Feed。对 Group Feed，`ProfileUuid = FeedUuid = Group UUID` 是明确、受限的 **machine-authored Entry**，与 FeedService 导入使用相同领域语义；它不表示某个 admin 发帖，也不能借用 key 创建者的 user identity。

两种 Feed 都满足 author 与 canonical target 相同，因此 `To` 为空；Group 展示身份来自 `From`
的 canonical Group 快照。`Via` 则区分具体 machine producer。

这不会重新引入历史迁移修复的数据错误：普通用户向 Group 投稿仍必须保存 `ProfileUuid = user UUID, FeedUuid = Group UUID`。只有 ffweb 完成 Feed API key 认证并在 loopback context 注入可信 Feed identity metadata 时，既有 `PostEntry` 才进入 Group machine-author 分支；metadata 缺失时继续拒绝 Group 冒充用户 principal。

ffdev 历史数据验证了这一区分：明确的 Group-author 行同时满足 `From = Group` 与非空 `Via`；
另有 `ProfileUuid = Group` 但 `From = user` 的历史投稿，其中一部分因用户 Profile 缺失无法迁移，
仍不得被推断成 machine entry。旧 machine row 的空 FeedUuid 只作读取兼容，新写入不延续该编码。

Group machine entry 的权限边界：

- key 只能写入其自身 `FeedUUID`，不能指定其他 Feed、Profile 或 admin；
- V1 不提供 update/delete endpoint；Group admin/super 仍可按既有 Group moderation 规则删除；
- machine entry 没有可登录的用户作者，不产生“通知作者本人”的语义；
- author/feed direct index、timeline、realtime、media 与普通 Entry 使用同一 mutation invariant；
- `Via` 必须由服务端固定为 `FriendFeed API`，不能由客户端伪造。

这样不会出现：

```text
key for Feed A
+
client payload says author/feed B
```

---

## 4.5 Public API transport boundary

Public HTTP endpoint 放在 ffweb，例如：

```text
GET  /api/v1/feed
GET  /api/v1/feed/entries
GET  /api/v1/feed/entries/:id
POST /api/v1/feed/entries
```

API key 已经唯一决定 Feed，因此 V1 默认不要求 URL 中再次传 Feed UUID。

如未来使用 `/feeds/:id` 形式，server 必须验证 URL feed 与 key principal 一致，不能相信 path 参数。

---

## 4.6 Public DTO 不等于 pb.Entry

Public API 必须定义自己的 DTO。

禁止直接把 `pb.Entry` JSON 化作为长期 API contract，因为 protobuf Entry 含：

- legacy fields；
- denormalized snapshots；
- Web display data；
- internal identifiers；
- Likes/Comments/Commands；
- RawBody 兼容字段；
- 未来可能变化的内部结构。

Public DTO 应只包含对外承诺字段，例如：

```json
{
  "id": "...",
  "title": "...",
  "body": "...",
  "created_at": "...",
  "attachments": [
    {
      "url": "...",
      "thumbnail_url": "...",
      "mime_type": "image/jpeg"
    }
  ]
}
```

转换关系：

```text
Public DTO
   |
   v
ffweb API adapter
   |
   v
ffdb dedicated command/RPC
   |
   v
domain model
```

---

## 4.7 附件

Public API 对调用方仍优先保持一次 multipart 发布，不提前暴露 browser staging protocol：

```text
POST /api/v1/feed/entries
Content-Type: multipart/form-data
Authorization: Bearer ...

body=...
raw_body=...
file=@...
```

这只是外部 transport 形态，不代表另建一套媒体实现。ffweb adapter 必须复用 browser upload 已有的服务端能力：验证输入、生成 thumbnail、写 staging、promote 为 canonical object，再构造最终 Entry。Public API credential 不能复用 browser asset token，也不能让调用方提交 storage path。

必须保持：

- 与 browser endpoint 相同的 request、文件数、总大小和类型 allowlist；
- MIME/magic/container 验证，不能信任扩展名或 multipart MIME；
- server-derived extension 和 content-addressed canonical key；
- filename 只作为清理后的 display name，不进入 object key；
- 图片与文件分别进入 `Entry.thumbnails[]` / `Entry.files[]`；
- HTML、SVG 等主动内容只能强制下载，不能在主站 origin inline 执行；
- 上传或 promote 失败不得提交 Entry；promote 成功后 domain mutation 失败所留下的 canonical orphan 由未来离线 GC 处理；
- Entry 成功后 R2 mirror 异步执行，本机 canonical object 仍是当前 serving source。

如果未来出现大文件、对象存储直传或多阶段草稿，再新增独立 upload resource。

---

# 5. 代码边界目标

建议逐步形成：

```text
httpd/
  main.go

  src/
    # Browser BFF / legacy SSR
    auth.go
    feed.go
    group.go
    account.go
    notification.go
    realtime.go
    ...

  api/
    # Public Feed API
    auth.go
    feed.go
    entry.go
    upload.go
    response.go

  app/
    # React application
    src/
      pages/
      components/
      api/
      ...

  templates/
    # shrinking SSR/bootstrap surface

server/
  # ffdb domain RPC

model/
  # persistence/domain storage
```

不要求第一步就移动所有旧文件；新增能力优先遵守该边界即可。

---

# 6. 演进原则

所有 Web 演进必须遵守：

### 6.1 页面逐个迁移

禁止“大爆炸式”把 Pongo2 一次替换成 React。

每个页面迁移需要：

1. 保留旧 route；
2. 保留后端领域 contract；
3. 新 React 页面达到等价功能；
4. 测试通过；
5. E2E 通过；
6. 再删除该页面不再需要的 template。

### 6.2 不在 UI 迁移中重写领域 RPC

例如迁移 Group members 页面时，不应该顺便重做 Group 权限模型或存储。

如果现有 gRPC 缺少适合 UI 的 typed response，可以：

- 优先新增兼容 RPC；
- 或在 ffweb BFF 组合现有 RPC；
- 不允许把数据库访问搬进 ffweb。

### 6.3 不把 protobuf 直接扩散到 React

React 应使用 Web DTO/JS object，而不是依赖 protobuf generated JS runtime。

当前统一使用 versioned `window.pageBootstrap`，其 page data 与 layout data 都有显式
Go DTO 和对应 TypeScript type；protobuf 不直接跨浏览器边界。

### 6.4 Server remains authoritative

React 不得成为权限 source of truth。

隐藏按钮只是 UX；server mutation 仍必须拒绝非法请求。

### 6.5 保持 progressive rollback

每个 migration commit 应允许：

```text
revert React page
->
old route/template still works
```

直到对应页面完整迁移并稳定后，才删除旧模板。

---

# 7. 分阶段实施记录与后续 Spec

Phase 0–4 已完成，以下内容保留为当前架构的实施依据与回归边界。Phase 5 及以后仍是后续 Spec；实施时可以在一个阶段内分多个小提交，但不建议跨阶段同时做大范围改动。

---

## Phase 0（已完成）：建立 Web contract baseline

### 目标

在动 UI 架构前，把现有 Web 行为变成明确可验证的 baseline。

### 实施

1. 整理 Web route inventory：
   - public routes；
   - authenticated routes；
   - `/a/*` browser action routes；
   - SSE；
   - upload；
   - static/media；
   - OAuth。
2. 为主要页面记录：
   - 是否 SSR；
   - 是否 React mount；
   - 数据来源；
   - 是否需要 auth；
   - 是否 private-aware。
3. 补足现有 E2E 覆盖：
   - anonymous Public；
   - logged-in Home；
   - Feed；
   - Entry；
   - login-required redirect；
   - publish；
   - comment/like；
   - Group；
   - notification；
   - upload。
4. 在建立 baseline 时保留当时的 SSR + React 双渲染，不改变行为。

### 验收

必须通过：

```text
go build ./...
go vet ./...
go test ./...

cd httpd/app
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build
pnpm run test:e2e
```

并且：

- anonymous `/public` 首屏仍含 Entry HTML；
- 匿名 `/feed/:id` JS 加载前仍有服务端 Entry；
- React mount 后页面功能与原行为一致；
- SSE 仅在允许的 Home 页面建立；
- private Feed 不因测试/DTO 改造泄漏。

### 回滚边界

Phase 0 不允许删除模板或 route，只增加测试/文档/typed helper。

---

## Phase 1（已完成）：定义 Browser DTO 层

### 目标

把“Go protobuf object 直接塞给模板/React”收敛为显式 browser DTO，为 React 页面建立稳定界面。

### 实施

1. 为 Feed 页面定义 browser DTO，例如：
   - FeedView；
   - EntryView；
   - ProfileSummary；
   - Paging；
   - Permissions；
   - Realtime bootstrap。
2. DTO 由 ffweb 构造；
3. DTO 只包含 React/SSR 真正需要的字段；
4. `rawBody` 只作为 editor round-trip 数据，展示只使用服务端消毒后的 `Body`；
5. versioned `window.pageBootstrap` 只承载该 DTO，不序列化任意 `pongo2.Context`；
6. 给对应 DTO 写 TypeScript type；
7. 保证 SSR 与 React 都从同一 DTO 语义生成 UI。

### 必须避免

- 不在 React 中重新推导 Group admin；
- 不让 React 根据 URL 猜 Feed UUID；
- 不把完整 Graph 或无关 protobuf 字段发给浏览器；
- 不改变 Feed cursor contract。

### 验收

- 使用固定 fixture，同一个 Feed 的 Go DTO JSON 与 TypeScript type 匹配；
- browser payload 不包含 OAuth token、session、credential、internal error；
- private/profile visibility 与旧页面一致；
- existing Feed React tests 全通过；
- SSR 页面 snapshot/semantic assertions 不退化；
- bundle size gate 不显著增长。

### 完成标准

达到：

```text
pb.Feed
  -> Go browser DTO
  -> SSR + React
```

而不是：

```text
pb.Feed / arbitrary pongo2.Context
  -> browser
```

---

## Phase 2（已完成）：React 接管非核心管理页面

### 目标

SSR 价值较低、交互较多的 authenticated management pages 已迁移到统一 React dispatcher。

实际实施顺序：

1. Account；
2. Feed Import；
3. Group settings；
4. Group members；
5. Notifications；
6. Requests。

这些页面通常：

- 登录后才能访问；
- SEO 不重要；
- 更适合 React interaction；
- SSR 完整正文价值有限。

### 实施模式

旧：

```text
GET /account/...
   -> Go
   -> Pongo2 renders complete page
```

当前：

```text
GET /account/...
   -> Go verifies session
   -> thin bootstrap template
   -> initial browser DTO
   -> React page
```

Mutation：

```text
React
   -> Browser BFF endpoint
   -> ffweb session actor
   -> ffdb gRPC
   -> ffdb authorization
```

### 每个页面的迁移检查表

#### 功能等价

- 所有表单操作存在；
- error state 存在；
- loading state 合理；
- permissions 与旧页一致；
- redirect 行为一致。

#### 安全

- actor 永远从 session 获取；
- 不信任 hidden input 中的 actor UUID；
- mutation 最终由 ffdb 校验；
- 不误称现状已有 CSRF token：当前 production session cookie 是 `Secure + SameSite=None`，debug 为 `SameSite=Lax`，browser action 尚无独立 CSRF token；页面迁移不得扩大 mutation 面，CSRF hardening 需单独实施并统一覆盖旧、新 action；
- secret 不进入 initial data/log。

#### 可访问性

- form label；
- keyboard；
- focus；
- dialog；
- error announcement；
- existing axe tests。

#### 测试

- component test；
- Go handler test；
- permission negative test；
- Playwright happy path；
- Playwright forbidden path（适用时）。

### 删除旧模板的条件

一个页面只有在以下全部满足后才能删除旧完整模板：

1. React 页面已在 production-equivalent route 使用；
2. Go tests 通过；
3. frontend tests 通过；
4. E2E 覆盖主要 mutation；
5. 至少不存在 JS-disabled fallback 的明确产品要求；
6. 旧 template 已无 route 使用。

---

## Phase 3（已完成）：Feed React 单一客户端实现

### 目标

消除 Feed 页面客户端部分的重复 DOM 实现。

注意：

> 本阶段删除登录态 Feed SSR，但保留匿名 Public/Feed/Entry SSR。

### 实施

1. 把 Feed header / Entry / Comments / Likes / paging 的 React component 定义为唯一 interactive implementation；
2. 服务端模板不再承载行为脚本；
3. 所有 client mutation 统一通过 Browser BFF endpoint；
4. React 的 Entry state 与 realtime refresh 使用同一 DTO；
5. Editor 继续 lazy-load，静态 Feed 不引入 Plate runtime；
6. 保持旧 `rawBody` editor round-trip；Entry 展示只渲染服务端消毒后的 `Body`，不从 `rawBody` 回退渲染。

### SSR 策略

Phase 3 的固定实现：

```text
anonymous Public/Feed/Entry
  -> Pongo2 outputs readable static content
  -> React may progressively enhance

authenticated Home/Public/Feed/Entry
  -> Pongo2 outputs app bootstrap only
  -> React renders the complete page
```

优点：

- 保留匿名 no-JS/readable SSR；
- 登录态不再维护或替换重复 Entry DOM；
- 不引入 Node production runtime。

缺点：

- 匿名 static markup 与 React component 仍有两份，但 template 不再承担登录态交互。

匿名 Feed 改为 React-only CSR 只允许在单独 architecture decision 后进行。

必须先回答：

- Public/Entry SEO 是否可接受；
- social crawler 是否需要正文；
- no-JS fallback 是否可删除；
- first content latency 是否可接受。

没有这些证据前不取消匿名 SSR。

### 验收

- Feed interaction 不再依赖 template 中的行为 DOM 细节；
- Entry/Like/Comment/Edit tests 只测试 React interactive implementation；
- anonymous Public/Feed/Entry 首屏 SSR 仍可读；
- authenticated Public/Feed/Entry response 不再包含重复 Entry SSR DOM；
- React mount 后不出现明显 layout shift；
- realtime refresh 后 UI 与完整 reload 一致；
- editor chunk 不进入静态 Feed initial bundle。

---

## Phase 4（已完成）：Pongo2 收缩为 layout/bootstrap

### 目标

大部分 authenticated Web 页面不再使用完整 Pongo2 页面结构。

当前边界：

```text
templates/
  layout.html
  app_shell.html
  feed.html          # anonymous Public/Feed/Entry SSR
  403.html
  404.html
  small transitional templates only
```

### 实施

1. 建立通用 React page bootstrap：
   - title；
   - current user summary；
   - navigation；
   - asset manifest；
   - page kind；
   - initial data。
2. Sidebar/navigation 由 authenticated React layout 统一提供；
3. 避免每个新页面增加新的 Pongo2 template；
4. 新 authenticated feature 默认 React page；
5. Pongo2 include 只用于仍保留 SSR 的静态内容。

### 验收

- 新增普通管理页面不需要新增 Pongo2 页面模板；
- nav/sidebar active state 由统一 React navigation 管理；
- authenticated page initial DTO 有统一 envelope；
- template 只负责 bootstrap，不复制 domain UI；
- old templates 删除后 Go template tests 相应收敛。

---

## Phase 5：正式 Public Feed API V1

该阶段可以和 Phase 2–4 独立推进，但必须遵守本文定义的边界。

Public Feed API V1 已由 `docs/web_api.md` 定稿并完成实施；生产操作见
`docs/web_api_operations.md`。以下内容保留为架构摘要，不再作为
字段、表号、限额或逐步实施的权威来源。

### 5.1 ffdb：Feed API Key domain

新增：

- Feed API Key persistence；
- Generate；
- Rotate；
- Revoke；
- Authenticate；
- authenticated Feed identity；
- 既有 Feed/Entry RPC 的有界 machine-capability 分支。

管理 API Key 的 browser action：

```text
Browser session user
  -> ffweb
  -> ffdb
  -> verify user can manage target Feed
  -> generate/rotate/revoke
```

API 使用：

```text
Bearer key
  -> Public API
  -> ffdb authenticate
  -> Feed UUID + non-secret key ID
  -> reuse FetchFeed / FetchEntry / PostEntry
```

### 5.2 Public API V1

V1 至少支持：

```text
GET  /api/v1/feed
GET  /api/v1/feed/entries
GET  /api/v1/feed/entries/:id
POST /api/v1/feed/entries
```

Pagination：

- 使用 opaque cursor；
- 默认 limit 应与当前 Feed read 规模一致；
- 对外只承诺 cursor opaque，不暴露 Pebble key 意义；
- invalid cursor 返回稳定 4xx。

### 5.3 Authentication

统一：

```http
Authorization: Bearer <feed-api-key>
```

必须拒绝：

- missing key；
- malformed key；
- revoked key；
- unknown key；
- URL/feed mismatch（如果以后 URL 带 feed id）。

失败不得通过 timing/log 暴露完整 secret。

### 5.4 POST Entry

V1 允许 personal Feed 和 Group Feed 使用 multipart。ffweb 完成 key 认证后，通过仅限 loopback
内部 context 的 Feed identity metadata 调用既有 `PostEntry`；ffdb 据此派生 author/target。
metadata 缺失时 `PostEntry` 的现有用户语义和 Group 拒绝规则保持不变。
Feed API metadata 分支固定为 create-only，由 ffdb 生成新的 Entry UUID；复用 `PostEntry` 不代表
向 API key 暴露其既有编辑路径。

Server derived：

- FeedUUID；
- author identity；
- created timestamp（若 contract 如此定义）；
- media object path；
- canonical From/To。

Client 可提供的字段必须白名单化。

禁止 client 直接控制：

- ProfileUuid；
- FeedUuid；
- From；
- Group principal；
- internal Commands；
- Likes/Comments；
- server-only metadata。

### 5.5 API error contract

Public API 统一响应，例如：

```json
{
  "error": {
    "code": "invalid_api_key",
    "message": "Invalid API key"
  }
}
```

不得直接把 gRPC/internal error string 原样返回。

至少定义：

- invalid_api_key；
- forbidden；
- not_found；
- invalid_request；
- payload_too_large；
- unsupported_media；
- internal_error。

### 5.6 API 验收

#### Auth

- Feed A key 可以读 A；
- Feed A key 不可操作 B；
- revoked key 立即失效；
- rotate 后旧 key 失效，新 key 生效；
- logs 中不存在完整 key。

#### Read

- personal Feed 正常；
- Group Feed 正常；
- private Feed 的 key 可以读取其自身内容；
- cursor pagination 无重复/漏读；
- deleted/missing Entry 有稳定 404。

#### Write

- personal Feed key 发布后 author/feed 正确；
- Group Feed key 发布后得到带 API provenance 的 Group machine-authored Entry；
- 普通用户向 Group 投稿仍以真实用户作为 `ProfileUuid`；
- client 伪造 ProfileUuid 无效；
- Entry domain/timeline/realtime invariant 与正常创建一致；
- multipart media 成功时 Entry 能引用正确资源；
- upload 失败时不创建半 Entry；
- 超限文件返回 413。

#### Compatibility

- Public DTO 不依赖 protobuf field name；
- protobuf 增字段不会自动暴露到 Public API；
- internal Go refactor 不改变 V1 JSON contract。

---

## Phase 6：评估是否需要 Node SSR

只有在完成前述收敛后，才重新评估 production Web runtime 是否从 Go 切换到 Node。

触发条件应是实际问题，而不是技术偏好。

可以考虑 Node SSR 的信号：

- 绝大多数页面已经是 React；
- Pongo2 只剩少量 bootstrap；
- React/Pongo2 fallback 仍造成高维护成本；
- 产品明确需要 React server rendering + hydration；
- Web 团队主要开发边界已经是 TypeScript；
- ffweb Go 已经退化成很薄的 session/gRPC proxy；
- 有清晰收益覆盖 OAuth/session/SSE/gRPC/upload 迁移成本。

在这些条件出现之前：

> Go ffweb 保持 production Web runtime。

如果未来决定迁移，必须另写独立 ADR/spec，不在普通 UI PR 内完成。

---

# 8. 数据流规范

## 8.1 Browser read

目标：

```text
React page
   |
   | same-origin request or initial bootstrap
   v
ffweb
   |
   | session -> actor
   | build typed gRPC request
   v
ffdb
   |
   | visibility / domain read
   v
ffweb browser DTO
   |
   v
React
```

不得：

```text
React -> direct ffdb
React -> Pebble
React -> trust local permission state
```

---

## 8.2 Browser mutation

```text
React
  -> ffweb browser endpoint
      -> session actor
      -> validation/transport
      -> ffdb RPC
          -> final domain authorization
          -> atomic mutation
      -> browser DTO
  -> React state
```

前端发送的 actor/user UUID 不能成为可信 principal。

---

## 8.3 Public API read/write

```text
External client
  -> /api/v1
      -> parse Bearer key
      -> ffdb authenticate key
      -> Feed principal
      -> domain read/write
      -> public DTO
```

Public API 不复用 browser cookie session 作为主认证方式。

---

# 9. Auth 与权限规范

## 9.1 Browser

Browser principal 来源：

```text
secure session -> current user UUID
```

不接受 request body 中自报 actor 作为身份。

### ffweb 可以做

- 提前隐藏不可用 UI；
- 提前返回 obvious 403；
- 组装 actor UUID 到 RPC。

### ffdb 必须做

- authoritative profile lookup；
- Group role check；
- private visibility；
- mutation permission；
- canonical author/target derivation。

---

## 9.2 Public API

Feed API key 代表 Feed capability，不代表任何 User。

管理 key 的权限仍由 user session + Group/Feed manage permission 决定。

使用 key 时不应该借用创建者的 user 权限。

---

# 10. SSR 与 CSR 决策矩阵

默认建议：

| 页面 | 当前/目标 | 原因 |
| --- | --- | --- |
| Public | 匿名 SSR；登录态 React | anonymous readability/crawler；登录态避免双渲染 |
| Feed | 匿名 SSR；登录态 React | 公开内容可读；登录态完整交互 |
| Entry | 匿名 SSR；登录态 React | anonymous permalink/social；登录态完整交互 |
| Home | React + bootstrap | authenticated，SEO 无价值 |
| Account | React | authenticated management |
| Import | React | interactive management |
| Group settings | React | authenticated management |
| Group members | React | interactive management |
| Notifications | React | authenticated |
| Requests | React | workflow |
| Group discovery | 匿名 SSR；登录态 React | 公开目录在无 JS 时仍可浏览 |
| 403/404 | server-rendered simple page | 简单可靠 |

该表不是永久 contract；未来改变匿名 Public/Feed/Entry SSR 需独立决策。private target 仍必须先通过服务端可见性检查，SSR 白名单不改变权限。

---

# 11. Static asset 与 build 规范

继续保持：

```text
frontend build first
   ->
Vite hashed assets
   ->
publish into httpd/static
   ->
Go build embeds static
```

要求：

- 不提交 generated JS/CSS/manifest；
- 保留手写 style.css；
- production asset 使用 content hash；
- editor/Plate 必须 lazy chunk；具体页面展示与投稿权限边界见 `docs/group.md`、`docs/perm.md`；
- 普通 static Feed 不加载 editor runtime；
- build manifest 缺失要在 build/CI 阶段失败，不能等 production request 才发现；
- Go binary 与其 embedded assets 必须来自同一 commit。

---

# 12. Realtime

Realtime contract 继续以 `docs/realtime_sse.md` 为准。

Web 演进中必须保持：

- SSE endpoint 仍由 ffweb 管理；
- ffdb 只提供内部 realtime source；
- only eligible newest authenticated Home opens SSE；
- React page unmount/route change 要关闭 listener；
- deploy graceful shutdown 先 signal realtime stream，再 HTTP Shutdown；
- React page 迁移不能扩大 SSE 到普通 Feed。

---

# 13. Upload / media

媒体上传的 browser protocol 和发布编排属于 ffweb；Entry 及其中的结构化 media refs 属于 ffdb domain。详细契约以 [`media_upload.md`](media_upload.md) 为准，本节只固定 Web 架构边界。

## 13.1 当前 Browser 写入链路

```text
Plate / file picker / clipboard
  -> POST /a/upload | /a/upload_file
  -> ffweb validates bytes/type/dimensions/container
  -> media_path/upload-staging (24h TTL)
  -> actor-bound HMAC asset token
  -> POST /a/share with final editor state + referenced tokens
  -> verify token, digest, size and final reference
  -> atomic promote on the same filesystem
  -> content-addressed canonical object
  -> rewrite body/rawBody and build Thumbnails[] / Files[]
  -> ffdb PostEntry
  -> when media_mirror is enabled, enqueue newly added canonical refs for R2 mirror
```

关键点：

- staging URL/token 只服务未发布编辑态，不能进入最终 Entry；
- `/a/share` 只 promote 最终编辑态仍引用的对象；取消或移除的上传等待 staging TTL 清理；
- 编辑既有 Entry 时保留的 canonical refs 不重复 promote/mirror；
- canonical key 由已验证内容摘要和服务端扩展名决定，不含用户 filename；
- ffweb promote 与 Pebble mutation 无跨系统事务，少量 canonical orphan 是明确接受的边界；
- R2 mirror 受 `media_mirror` 开关控制（默认关闭）；开启且配置完整时才入队。它是 best-effort background task，不阻塞本机 canonical media 的发布和读取。

## 13.2 编辑器与剪贴板

Plate editor 负责识别本地文件、剪贴板 binary/data/blob 和 HTML 内远程图片，但所有内容最终都必须经过 ffweb upload endpoint。不得把 `data:`、`blob:`、staging URL、asset token 或 remote source URL 写入最终 `rawBody/body`。

上传是可失败的网络操作：pending/error 时不得发布引用不完整的 Entry；失败应在 composer 操作区给出不阻断编辑的可见提示，不能静默删除图片，也不要求用户额外 dismiss。编辑器内部 Slate fragment 的复制粘贴不得重新上传已发布 media。

## 13.3 读取与下载

当前 media URL 是公开 capability，不随 Feed private 权限做读取鉴权。production 由独立 media origin/nginx 从 `media_path` serve；dev/local 可由 ffweb fallback serve。

- 光栅图片允许 inline 与 modal preview；
- 附件必须带 download 语义；
- HTML、SVG 等主动内容强制 `Content-Disposition: attachment`、`application/octet-stream` 与 `nosniff`；
- 主站 `/file` 仅作为历史 fallback，不能成为主动内容的 inline 执行入口；
- external image 只为历史 Entry 保持读取兼容，新上传必须 canonicalize 到本站 media。

## 13.4 共享实现边界

Browser 和未来 Public API 可以复用底层 helper，但不能复制两套安全逻辑。合理分层是：

```text
transport/auth adapter
  -> bounded input
  -> verified media pipeline
  -> staging/promote
  -> canonical media refs
  -> domain mutation
```

Browser endpoint 和 Public API endpoint 只负责不同的 principal、request/response contract；类型识别、SSRF 防护、限额、thumbnail、canonical key 和下载安全规则必须共用。

不得：

- 让 React 直接写 storage；
- 让 Public API 直接信任 object path；
- 用原始用户 filename 作为真实 storage key；
- 在失败时提交引用不存在附件的 Entry；
- 把 R2 当作同步发布前置条件；
- 在 read path 自动 mirror 历史外站图片。

---

# 14. 错误处理

## Browser BFF

可以根据当前页面返回：

- JSON；
- redirect；
- 403/404 template；
- form validation response。

但 internal gRPC details 不应直接展示给用户。

## Public API

必须稳定 JSON error contract。

Public API 不允许：

- HTML error page；
- raw Gin panic；
- raw gRPC status detail；
- stack trace；
- credential-related debug detail。

---

# 15. Observability

ffweb / ffdb 继续：

- stdout/stderr；
- journald；
- 不写应用日志文件；
- 不使用 Fatal 处理请求；
- 不记录 Cookie/session/OAuth/API key；
- 不记录完整 Entry body。

建议 Public API 增加安全字段：

```text
route
status
latency
feed key id (non-secret identifier only, if implemented)
feed UUID (when safe)
request id
```

严禁记录 Bearer header。

---

# 16. 部署与兼容

当前部署仍以 Go binaries + systemd 为主。

Web React 演进不应要求 production Node runtime。

推荐发布顺序：

```text
frontend verify/build
  ->
Go build/tests
  ->
deploy ffdb
  ->
health
  ->
deploy ffweb
  ->
HTTP health/E2E smoke
```

如果未来 master 自动部署，应部署 CI 验证过的 exact commit SHA，而不是 production 上裸 `git pull` 最新 master。

---

# 17. 测试分层

## 17.1 Go domain tests

验证 ffdb：

- source of truth；
- authorization；
- persistence；
- API key；
- Entry mutation；
- Group；
- visibility。

## 17.2 Go httpd tests

验证：

- routing；
- session principal；
- DTO mapping；
- HTTP status；
- SSR/bootstrap；
- Public API transport；
- multipart limit。

## 17.3 React unit/component tests

验证：

- rendering；
- interactions；
- local state；
- accessibility；
- browser API request handling。

不要在 React test 中重新模拟 domain authorization 作为唯一保证。

## 17.4 E2E

关键用户流：

```text
Browser -> ffweb -> ffdb -> persisted result -> Browser
```

Public API：

```text
curl/API client -> ffweb -> ffdb -> persisted Entry -> GET verification
```

---

# 18. 每次 Web PR 的验收模板

涉及 Web architecture 的 PR 至少回答：

### Architecture

- 改动属于 React UI、Browser BFF、Public API 还是 ffdb domain？
- 是否把领域规则错误地搬到 Web？
- 是否新增外部 contract？
- 是否影响 SSR？

### Security

- principal 来自哪里？
- 是否信任客户端 UUID？
- ffdb 是否做最终 authorization？
- 是否可能记录 secret/body？
- private Feed 是否有 negative test？

### Compatibility

- 是否改 protobuf？
- 是否改 public JSON？
- 是否改 route？
- 是否改 cursor？
- 是否删除 template/exported helper？
- 是否需要退役方案？

### Performance

- 是否把 editor 放入 initial bundle？
- 是否新增无必要 RPC round-trip？
- 是否把本可 bootstrap 的数据改成页面加载后二次 fetch？
- 是否扩大 SSE connection 数？

### Verification

```text
go build ./...
go vet ./...
go test ./...

cd httpd/app
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build
pnpm run test:e2e   # 页面/flow 有变化时
```

---

# 19. 完成状态的判定

“Go BFF + React 占更多页面，减少 Pongo2”不意味着必须达到零模板。

下一阶段完成的合理状态是：

```text
ffdb
  = domain backend

ffweb Go
  = Browser BFF
  + Public Feed API transport
  + session/OAuth/SSE/upload
  + thin bootstrap
  + selected SSR

React
  = primary interactive UI implementation

Pongo2
  = bootstrap + selected SSR/fallback
```

满足以下条件即可认为架构演进成功：

1. 新 authenticated 页面默认 React，而不是新增完整 Pongo2 template；
2. 管理类页面基本由 React 主导；
3. Feed interactive UI 只有一套 React 实现；
4. SSR 只服务匿名 Public/Feed/Entry；登录态页面由 React 完整渲染；
5. Browser DTO 和 Public DTO 都不直接等于 protobuf；
6. ffdb 继续掌握最终权限和业务不变量；
7. production 仍保持简单的 Go binary + systemd 运维；
8. Public Feed API 能独立演进，不被 React 页面重构破坏；
9. 不为了“未来可能”引入当前规模不需要的额外服务和基础设施。

---

# 20. 后续决策点

以下事项到出现真实需求时再单独写 ADR/spec：

- 是否取消匿名 Public/Feed/Entry 的 Pongo2 SSR；
- 是否引入 React SSR/hydration；
- production ffweb 是否迁到 Node.js；
- 一个 Feed 是否支持多个 active API keys；
- browser mutation 的统一 CSRF token/origin 防护；
- Public API 是否支持 comments/likes/group management；
- Public API OAuth/User token；
- 大文件 direct upload / presigned URL；
- 是否拆独立 ffapi service；
- rate limit 的持久化/分布式实现。

在此之前，默认路径保持：

> **Go ffdb + Go ffweb BFF/Public API transport + React 主导交互 UI + 逐步收缩 Pongo2。**
