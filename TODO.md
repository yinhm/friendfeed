# Web UI 收敛任务

目标：按 `docs/web_architecture.md` 将 Web UI 从“Pongo2 SSR + React 二次渲染”逐步收敛为“Go BFF + React 主导 UI”。本清单只处理 Browser Web UI；Public Feed API、API Key、Node SSR 和 CSRF 全面加固不混入本轮。

## 执行规则

- 按编号顺序实施，每一项单独提交并保持可回退。
- ffdb 继续负责领域事实和最终授权；React 只负责展示与交互。
- 不改现有 route、protobuf、cursor、媒体上传协议和持久化 schema。
- SSR 只保留给匿名访问的 `/public`、公开 `/feed/:name` 和公开 `/e/:uuid`；登录用户访问这些页面以及 Home 时都走薄 bootstrap + React 完整渲染，不再输出重复的 Entry SSR DOM。
- “匿名 SSR”始终以可见性检查通过为前提；private Feed/Entry 不因命中上述 route 而输出正文。
- 删除模板前必须证明 route 已无消费方，并有等价测试。
- `rawBody` 只用于编辑器 round-trip；展示只使用服务端消毒后的 `Body`。
- 所有浏览器 principal 从 session 获得，不能信任客户端提交的 actor UUID。

每项最低门禁：

```text
cd httpd/app
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build

cd ../../
go build ./...
go vet ./...
go test ./...
git diff --check
```

涉及完整浏览器流程、route 或模板切换时额外执行 `pnpm run test:e2e`。

## 进度

- [x] 1. 锁定 Web route 与渲染基线
- [x] 2. 定义 Browser bootstrap 与 Feed DTO
- [x] 3. 建立单一 React bootstrap dispatcher
- [ ] 4. 将 Account/Profile 切到统一 app shell
- [ ] 5. 将 Feed Import 切到统一 app shell
- [ ] 6. 迁移 Notifications 页面
- [ ] 7. 迁移 Follow Requests 页面
- [ ] 8a. 迁移 Group Create
- [ ] 8b. 迁移 Group Settings
- [ ] 8c. 迁移 Group Members
- [ ] 9. 迁移 Group discovery 与 User Groups 列表
- [ ] 10. 收敛 Feed interactive UI
- [ ] 11. 收敛 layout/navigation

---

## 1. 锁定 Web route 与渲染基线

产物：

- 新增 `docs/web_routes.md`，逐条登记 `httpd/main.go` 的 route：auth、页面、`/a/*` action、SSE、upload、static/media。
- 每个页面记录：登录要求、handler、template、React mount、数据来源、SSR 要求、private-aware 情况。
- 明确当前四个 React mount：Feed `#root`、Search `#search`、Account `#account-root`、Feed Import `#feed-import-root`。
- 明确完整 Pongo2 页面：Notifications、Requests、Group discovery/create/settings/members、User Groups。

补齐 characterization tests：

- anonymous `/public` 与公开 `/feed/:name` 在 JS 执行前包含 Entry HTML；
- anonymous permalink `/e/:uuid` 保留可读 SSR；
- 记录同一 Public/Feed/Entry route 当前在登录状态下仍会输出 Entry SSR DOM；第 2/10 项再将该 characterization test 改为只输出 React bootstrap；
- Home 登录要求、private Feed 403、login-required redirect 不变；
- 非 Home 页面不建立 `/a/events`；
- `window.appData` 不包含 session、OAuth token、secret 或内部错误；
- notification 与 upload 各补一条端到端 happy path，填补当前 E2E 基线缺口。

验收：

- 本项不修改生产页面结构、route 或 handler 行为；
- `docs/web_routes.md` 与 router 测试能在新增/删除 route 时显式失败或要求更新；
- 全量门禁与 E2E 通过。

## 2. 定义 Browser bootstrap 与 Feed DTO

在 ffweb 定义显式 Go DTO，不把 `pb.Feed` 或任意 `pongo2.Context` 直接序列化给浏览器。第一版只覆盖当前 Feed React 真正消费的字段：

- `PageBootstrap`：version、page kind、current user summary、page data；
- `FeedView`：metadata、entries、paging、commands/permissions、realtime flags；
- `EntryView`、`ProfileSummary`、`PagingView`；
- 对应的 TypeScript discriminated union/type。

实施：

- 匿名 Public/Feed/Entry 保留 Pongo2 SSR context，登录态只使用 bootstrap DTO；`window.appData` 只能来自 DTO；
- DTO mapping 集中在 ffweb，不进入 model/server；
- commands/permissions 由 ffdb response 映射，不在 React 重新推导；
- `Body` 是展示字段，`rawBody` 仅在可编辑场景下提供；
- JSON 写入 `<script>` 时保持安全转义，禁止 `</script>` 注入。

验收：

- Go fixture 的 DTO JSON 与 TypeScript consumer 契约一致；
- 实施前按第 10 项逐项核对 Entry/commands/paging/realtime/editor/media 所需字段，避免 Feed 收敛时重新设计 DTO；后续新增字段只能兼容增加；
- Home/Public/profile/group/likes/comments/permalink 均保持现有展示与分页；
- private visibility negative tests 不退化；
- 匿名 SSR 与登录态 React 使用同一份 Feed DTO 语义数据；
- bundle size 不超过现有预算。

## 3. 建立单一 React bootstrap dispatcher

目标：替换 `index.jsx` 中按多个 DOM id 分别 `createRoot` 的方式，但暂不迁移页面内容。

实施：

- 统一使用一个 main app root（例如 `#app-root`）；
- `PageBootstrap.page` 决定渲染 Feed、Account、Import 或后续页面；
- Search/sidebar 暂时保留独立 mount，避免把全站 layout 一次性迁入 React；
- 增加通用的 bootstrap parse/validation 与未知 page fail-loud；
- 建立薄 `app_shell.html`，只负责 layout、root、bootstrap JSON 和 hashed assets；
- Public/Feed/Entry template 在本项保留，但 handler 必须能按登录态选择匿名 SSR 或 React bootstrap；

验收：

- 同一页面只创建一个主 React root；
- Account 和 Feed Import 可先在旧模板上通过 dispatcher 运行；
- 缺失/损坏 bootstrap 不造成空白页面：服务端测试能捕获，客户端显示安全错误；
- Search、navigation、SSE listener 数量不增加；
- frontend unit tests 和现有 E2E 全过。

## 4. 将 Account/Profile 切到统一 app shell

现有 `AccountPage` 已是 React，先用它验证迁移模式。

实施：

- GET handler 只做 session 校验、读取数据、构造 `AccountPageData`；
- mutation 继续走现有 BFF handler 与 ffdb 权限边界；
- route 与 redirect 完全不变；
- 切到 `app_shell.html` 后删除仅服务该页面的完整 Pongo2 markup；
- 保留 profile picture、private、rename、错误提示等全部现有行为。

验收：

- component tests 覆盖初始值、成功、校验错误、server error；
- Go tests 覆盖本人 session、deleted profile、非法 rename；
- Playwright 覆盖 profile 更新与失败后表单状态；
- `/account/`、`/account/profile` 行为和跳转不变。

## 5. 将 Feed Import 切到统一 app shell

现有 `FeedImportPage` 已是 React，本项只收敛 bootstrap/template，不重做 FeedService 领域设计。

实施：

- personal/group import 共用 typed `FeedImportPageData`；
- actor/target 从 session 与服务端 resolved Feed 得出；
- Add、Disable、Refresh、Remove 继续走现有 handler；
- 二次确认继续使用统一原生 popover；
- 删除不再使用的 `feed_import.html` 完整页面结构。

验收：

- personal owner、Group admin 正常；普通 Group member 返回 403；
- source dead/degraded 状态和安全化错误仍正确显示；
- Remove 不删除历史 Entry；
- component、Go handler 和 Playwright import 流程通过。

## 6. 迁移 Notifications 页面

实施：

- 新增 typed `NotificationsPageData`，只包含渲染所需 snapshot、链接、分页与 read state；
- GET 仍由服务端按 session recipient 查询，客户端不能指定 recipient；
- 保留现有语义：GET 成功渲染后由 ffweb best-effort mark-all-read；不为迁移新增 action route，hint 仍只作 dirty signal；
- 页面切到 app shell 后删除 `notifications.html` 的完整 UI markup。

验收：

- 未登录跳转登录；用户只能看到自己的通知；
- 八类通知渲染、空状态、分页、mark-read 均有测试；
- badge 弱一致语义不变；
- E2E 覆盖 notification 页面与 mark-read。

## 7. 迁移 Follow Requests 页面

实施：

- typed `RequestsPageData`；
- approve/reject/cancel 保留既有 route 和 ffdb authorization；
- personal private Feed owner 与 Group admin 的批准语义不在 React 推导；
- 页面切到 app shell，删除 `account_requests.html` 的完整 UI markup。

验收：

- requester、target owner、Group admin、无权用户矩阵完整；
- 同秒重复申请 occurrence 行为不变；
- component、Go negative tests、Playwright follow-request 流程通过。

## 8. 迁移 Group Create / Settings / Members

分三个独立提交按顺序完成，不能合成一次大改：

### 8a. Group Create

- typed form data、服务端 validation、成功 redirect；
- 创建者原子成为 member/admin 的领域逻辑保持在 ffdb；
- 删除 `group_create.html` / `group_form.html` 中不再使用的 UI。

### 8b. Group Settings

- metadata/private 设置、统一 Group nav、危险删除居中二次确认；
- admin/super 权限由 ffdb 最终校验；
- 删除 `group_settings.html` 的完整 UI。

### 8c. Group Members

- bounded member/admin/request data；
- promote/demote/remove 等 mutation 保持既有约束；
- 最后 admin、admin 退出、private membership 边界必须有 negative tests；
- 删除 `group_members.html` 的完整 UI。

每一步验收：component + Go handler + permission negative + 对应 Playwright，用统一 app shell 且 route 不变。

## 9. 迁移 Group discovery 与 User Groups 列表

实施：

- `/groups` 和 `/feed/:id/groups` 使用同一 React list component、不同 typed page data；
- GroupIndex 的活跃排序、cursor、10 条 sidebar 限制不变；
- 列表页不新增 admin/member/request 状态查询；进入 Group Feed 后再管理；
- `/groups` 即使匿名访问也使用薄 bootstrap + React；匿名 SSR 白名单只包含 Public/Feed/Entry。

验收：

- anonymous discovery、本人 My Groups 403 边界、cursor 翻页、private icon/nav 全覆盖；
- 不新增 N+1 关系查询；
- Group discovery 与 My Groups 都不保留完整 SSR 页面。

## 10. 收敛 Feed interactive UI

本项消除 Feed 双渲染：匿名 Public/Feed/Entry 保留可读 SSR；登录态完全由 React 渲染。

实施：

- React 成为 Entry、Like、Comment、Edit、media lightbox、paging、realtime banner 的唯一交互实现；
- `feed.html`/`_feed.html` 只为匿名 Public/Feed/Entry 保留静态可读 markup；登录态 response 只包含 app shell/bootstrap；
- SSR 与 React 共用第 2 项 Feed DTO；
- Home dirty refresh、180s reconciliation、cursor、legacy `?start=` 兼容保持不变；
- editor 继续 lazy-load，普通 Feed 首包不引入 Plate runtime。

验收：

- 匿名且 JS 禁用时 Public/Feed/Entry 正文仍可读；登录态明确依赖 JS；
- 登录用户访问 Public/Feed/Entry 时 response 中没有重复的 SSR Entry markup；
- React mount 后无重复 media、明显 layout shift 或事件重复绑定；
- publish/edit/delete/like/comment/media/upload/realtime 全套测试通过；
- 匿名 SSR 与登录态 React 展示均只使用 sanitized `Body`；
- bundle size gate 与 E2E 全过。

## 11. 收敛 layout/navigation（最后执行）

只有 4–10 稳定后才执行：

- 建立统一 React navigation/sidebar；
- Pongo2 收缩到 `layout.html`、`app_shell.html`、匿名 Public/Feed/Entry SSR、403/404 和必要过渡模板；
- 删除多 root mount、废弃 context key、无 route 消费的模板与测试；
- 删除 `App`/`AccountPage`/`FeedImportPage` 为旧单测保留的 `window.appData`、`window.accountData`、`window.feedImportData` 回退，并把测试统一切到 dispatcher props；
- 更新 `docs/web_routes.md` 和 `docs/web_architecture.md` 的实现状态。

验收：

- 页面 active nav、mobile、private icon、Archive、Group sidebar、notification badge 不退化；
- authenticated 页面不再新增完整 Pongo2 template；
- SSR 白名单固定为匿名 Public/Feed/Entry，登录态页面全部 React-first；
- `rg` 证明删除的 template/context/mount 已无消费方；
- 全量前端、Go、E2E 门禁通过。

---

## 本轮明确不做

- 不实现 Public Feed API 或 Feed API Key；
- 不引入 Node production runtime、React SSR、RSC 或新 Web framework；
- 不改变 gRPC 暴露边界；
- 不调整 Feed/Group/private 权限模型；
- 不顺带修改 protobuf、Pebble 表、cursor 或 media upload contract；
- 不以“React 已隐藏按钮”代替 ffdb mutation authorization；
- 不在页面迁移中顺手清理受保护的 legacy API。
