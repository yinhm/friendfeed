# Web route 与渲染基线

本文锁定 Web UI 收敛前的 route、身份和渲染边界。router 权威定义仍是 `httpd/main.go`；修改 route 时必须同步本文和对应 handler/router test。

## 渲染边界

| 请求 | 当前实现 | 目标边界 |
| --- | --- | --- |
| 匿名 `/public`、公开 `/feed/:name`、公开 `/e/:uuid` | Pongo2 Entry SSR 后由 React `#root` 替换 | 保留可读 SSR；允许 React 渐进增强 |
| 登录态 Home/Public/Feed/Entry | `app_shell.html` + React dispatcher | 已收敛为薄 bootstrap，React 完整渲染 |
| Account/Profile | `app_shell.html` + React `#app-root` dispatcher | 已收敛为统一 app shell + React |
| Feed Import | `app_shell.html` + React `#app-root` dispatcher | 已收敛为统一 app shell + React |
| Notifications | `app_shell.html` + React dispatcher | 已迁移 |
| Follow Requests | `app_shell.html` + React dispatcher | 已迁移 |
| Group 管理/列表 | `app_shell.html` + React dispatcher | 已迁移 |
| Sidebar Search | authenticated app shell 内的 React navigation | 仅登录用户显示；匿名 SSR 不提供 Search |
| 403/404 | Pongo2 | 保留简单服务端页面 |

匿名 SSR 白名单不改变权限：private Feed/Entry 必须先通过 ffdb 可见性检查，未授权时不得输出正文。登录态明确依赖 JavaScript。

## Identity routes

| Method/path | Handler | 身份 | 响应 |
| --- | --- | --- | --- |
| `GET /auth/:provider` | `AuthProvider` | anonymous | OAuth redirect |
| `GET /auth/:provider/callback` | `AuthCallback` | OAuth state | session + redirect |
| `GET /logout` | `LogoutHandler` | optional session | clear session + redirect |

## Account 与 workflow

以下 route 都经过 `LoginRequired`。

| Method/path | Handler | 当前页面/用途 |
| --- | --- | --- |
| `GET /account/` | `AccountHandler` | Account redirect/entry |
| `GET /account/profile` | `AccountProfileHandler` | `app_shell.html` + `#app-root` |
| `POST /account/profile` | `AccountProfileUpdateHandler` | Profile mutation |
| `GET /account/import/` | `ImportHandler` | legacy import page |
| `GET /account/import/twitter` | `TwitterImportHandler` | legacy Twitter import |
| `GET /account/requests` | `AccountRequestsHandler` | `app_shell.html` + React `requests` |
| `POST /account/requests/action` | `AccountRequestActionHandler` | approve/reject request |
| `POST /account/feed-service` | `AddFeedServiceHandler` | add Service binding |
| `POST /account/feed-service/:service/:action` | `FeedServiceActionHandler` | disable/refresh/remove binding |
| `GET /account/service/:service/delete` | `DeleteServiceHandler` | legacy delete compatibility |

## Feed 与内容页面

| Method/path | Handler | 身份/可见性 | 当前渲染 |
| --- | --- | --- | --- |
| `GET /` | `HomeHandler` | anonymous redirect；登录态 Home | 登录态 `app_shell.html` + React `feed` |
| `GET /public` | `PublicHandler` | public | 匿名 `feed.html` SSR；登录态 app shell |
| `GET /feed/:name` | `FeedHandler` | ffdb visibility | 匿名公开 Feed SSR；登录态 app shell |
| `GET /e/:uuid` | `EntryHandler` | ffdb visibility | 匿名公开 Entry SSR；登录态 app shell |
| `GET /feed/:name/likes` | `InteractionFeedHandler` | login + owner-only | Feed React |
| `GET /feed/:name/comments` | `InteractionFeedHandler` | login + owner-only | Feed React |
| `GET /feed/:name/following` | `ProfileRelationsHandler` | login + Feed visibility；User only | app shell + React `profile-relations` |
| `GET /feed/:name/followers` | `ProfileRelationsHandler` | login + Feed visibility；User only | app shell + React `profile-relations` |
| `GET /feed/:name/api` | `FeedApiKeyPageHandler` | login + owner/Group admin/super | app shell + React `feed-api-key` |
| `GET /search` | `SearchHandler` | login required、visibility-filtered | Feed React |
| `GET /tag/:name` | `TagHandler` | visibility-filtered | Feed React |
| `GET /a/entry/:uuid` | `ExpandCommentHandler` | entry visibility | HTML fragment |
| `GET /a/expandlikes/:uuid` | `ExpandLikeHandler` | entry visibility | HTML fragment |

`?start=N` 是受限的 legacy 兼容入口；后续页面链接必须转入 cursor，迁移不得改变该契约。

## Browser actions

以下 `/a/*` route 除标注的 legacy fragment 外均经过 `LoginRequired`，actor 只来自 session。

| Method/path | Handler | 用途 |
| --- | --- | --- |
| `GET /a/events` | `EventsHandler` | 仅 eligible Home 的 SSE |
| `POST /a/share` | `EntryPostHandler` | publish/edit Entry，含 media promote |
| `POST /a/upload` | `UploadHandler` | image/remote image staging |
| `POST /a/upload_file` | `UploadFileHandler` | attachment staging |
| `POST /a/profile-avatar` | `ProfileAvatarHandler` | publish only the current user's avatar |
| `POST /a/group-avatar` | `GroupAvatarHandler` | publish only a managed Group's avatar |
| `POST /a/follow` | `FollowHandler` | follow/unfollow |
| `POST /a/feed-request` | `FeedRequestHandler` | private follow request |
| `POST /a/feed-request/cancel` | `FeedRequestCancelHandler` | cancel request |
| `POST /a/delete` | `EntryDeleteHandler` | delete Entry |
| `POST /a/like` | `LikeHandler` | Like |
| `POST /a/like/delete` | `LikeDeleteHandler` | Unlike |
| `POST /a/comment` | `CommentHandler` | create/edit Comment |
| `POST /a/comment/delete` | `CommentDeleteHandler` | delete Comment |

## Group pages

| Method/path | Handler | 身份 | 当前渲染 |
| --- | --- | --- | --- |
| `GET /groups` | `GroupDiscoveryPageHandler` | anonymous allowed | 匿名 `groups_public.html` SSR；登录态 app shell + React `groups` |
| `GET /feed/:name/groups` | `UserGroupsPageHandler` | login + self-only | `app_shell.html` + React `groups` |
| `GET /feed/:name/import` | `FeedImportPageHandler` | owner/Group admin | `app_shell.html` + `#app-root` |
| `GET /groups/create` | `GroupCreatePageHandler` | login | `app_shell.html` + React `group-create` |
| `POST /groups/create` | `GroupCreateHandler` | login | create Group |
| `GET /groups/:name/settings` | `GroupSettingsPageHandler` | Group admin | `app_shell.html` + React `group-settings` |
| `POST /groups/:name/settings` | `GroupSettingsHandler` | Group admin | update Group |
| `GET /groups/:name/members` | `GroupMembersPageHandler` | login + private visibility；管理按钮仅 admin/super | `app_shell.html` + React `group-members` |
| `POST /groups/:name/members/action` | `GroupMemberActionHandler` | Group admin | membership/admin mutation |
| `POST /groups/:name/delete` | `GroupDeleteHandler` | Group admin | destructive mutation |

## Public Feed API

`/api/v1` 是 Bearer credential 的 machine API 边界，不使用 session，也不属于 Browser BFF。当前
transport shell 统一提供 request ID、JSON error、no-store/nosniff、30 秒 timeout、请求体上限和
32 个全局并发槽；未知路径返回同形状 JSON 404。V1 已开放 `GET /api/v1/feed`、
`GET /api/v1/feed/entries` 和 `GET /api/v1/feed/entries/:entry_id`；不得为了诊断临时暴露
principal/debug endpoint。`POST /api/v1/feed/entries` 接受严格白名单 multipart，具体字段、媒体
限额、非幂等与 canonical orphan 边界以 `docs/web_api.md` 为准。历史外部内容使用
`POST /api/v1/feed/imports`，其确定性 identity、旧 Twitter 兼容和 archive side-effect 以
`docs/external_import.md` 为准。

## Notification、static 与 media

| Method/path | Handler | 边界 |
| --- | --- | --- |
| `GET /notifications` | `NotificationsHandler` | login；成功渲染后 best-effort mark-all-read |
| `GET /favicon.ico` | `FaviconHandler` | public static |
| `GET /static/*path` | embedded/debug static handler | build artifact/manual asset |
| `GET /app/build/static/*` | debug static handler | debug only |
| `GET/HEAD /file/*filepath` | `localMediaHandler` | legacy media fallback；主动内容强制下载 |
| unmatched | `NotFoundHandler` | `404.html` |

production canonical media 由独立 media origin/nginx 从 `media_path` serve；`/file` 不是新上传内容的首选 URL。

## 基线验收

- 匿名 Public/Feed/Entry 的 HTML response 在执行 JS 前含可读正文；
- 登录态非 Home 页面不请求 `/a/events`；
- private Feed/Entry 未授权时不出现正文；
- bootstrap JSON 不包含 token、Cookie、session、password、secret 或 internal error；
- route/template 切换必须有 Go handler test，完整用户流程必须有 Playwright test。
