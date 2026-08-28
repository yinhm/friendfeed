# Web route 与渲染基线

本文锁定 Web UI 收敛前的 route、身份和渲染边界。router 权威定义仍是 `httpd/main.go`；修改 route 时必须同步本文和对应 handler/router test。

## 渲染边界

| 请求 | 当前实现 | 目标边界 |
| --- | --- | --- |
| 匿名 `/public`、公开 `/feed/:name`、公开 `/e/:uuid` | Pongo2 Entry SSR 后由 React `#root` 替换 | 保留可读 SSR；允许 React 渐进增强 |
| 登录态 Home/Public/Feed/Entry | 同一批 Entry 同时 SSR 与 React 重绘 | 薄 bootstrap，React 完整渲染 |
| Account/Profile | `app_shell.html` + React `#app-root` dispatcher | 已收敛为统一 app shell + React |
| Feed Import | `app_shell.html` + React `#app-root` dispatcher | 已收敛为统一 app shell + React |
| Notifications | `app_shell.html` + React dispatcher | 已迁移 |
| Requests、Group 管理/列表 | 完整 Pongo2 页面 | 统一 app shell + React |
| Sidebar Search | React `#search` | layout 最后收敛，迁移期间保持独立 mount |
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
| `GET /account/requests` | `AccountRequestsHandler` | `account_requests.html` |
| `POST /account/requests/action` | `AccountRequestActionHandler` | approve/reject request |
| `POST /account/feed-service` | `AddFeedServiceHandler` | add Service binding |
| `POST /account/feed-service/:service/:action` | `FeedServiceActionHandler` | disable/refresh/remove binding |
| `GET /account/service/:service/delete` | `DeleteServiceHandler` | legacy delete compatibility |

## Feed 与内容页面

| Method/path | Handler | 身份/可见性 | 当前渲染 |
| --- | --- | --- | --- |
| `GET /` | `HomeHandler` | anonymous redirect；登录态 Home | `feed.html` + React `#root` |
| `GET /public` | `PublicHandler` | public | `feed.html` + React `#root` |
| `GET /feed/:name` | `FeedHandler` | ffdb visibility | `feed.html` + React `#root` |
| `GET /e/:uuid` | `EntryHandler` | ffdb visibility | `feed.html` + React `#root` |
| `GET /feed/:name/likes` | `InteractionFeedHandler` | login + owner-only | Feed React |
| `GET /feed/:name/comments` | `InteractionFeedHandler` | login + owner-only | Feed React |
| `GET /search` | `SearchHandler` | visibility-filtered | Feed React |
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
| `GET /groups` | `GroupDiscoveryPageHandler` | anonymous allowed | `groups.html` |
| `GET /feed/:name/groups` | `UserGroupsPageHandler` | login + self-only | `groups.html` |
| `GET /feed/:name/import` | `FeedImportPageHandler` | owner/Group admin | `app_shell.html` + `#app-root` |
| `GET /groups/create` | `GroupCreatePageHandler` | login | `group_create.html` |
| `POST /groups/create` | `GroupCreateHandler` | login | create Group |
| `GET /groups/:name/settings` | `GroupSettingsPageHandler` | Group admin | `group_settings.html` |
| `POST /groups/:name/settings` | `GroupSettingsHandler` | Group admin | update Group |
| `GET /groups/:name/members` | `GroupMembersPageHandler` | Group admin | `group_members.html` |
| `POST /groups/:name/members/action` | `GroupMemberActionHandler` | Group admin | membership/admin mutation |
| `POST /groups/:name/delete` | `GroupDeleteHandler` | Group admin | destructive mutation |

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
