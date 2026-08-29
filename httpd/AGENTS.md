# AGENTS.md

本文件补充仓库根目录规则，适用于 `httpd/` 和 `httpd/app/`。

## 模板与渲染

- 交互放在 React bundle；内联脚本只允许写入服务端生成的 `window.pageBootstrap` JSON。唯一例外是 `layout.html` 中不依赖 React bundle 的匿名 SSR 侧边栏渐进增强。
- 匿名 Public/公开 Feed/公开 Entry 及 Group 发现页保留可读 SSR；登录态页面使用 `#app-root` 单一 React dispatcher。
- Editor 默认不加载；页面展示边界见 `docs/web_architecture.md` 与 `docs/group.md`。前端 `show_share` 仅是展示提示，投稿授权以 `docs/perm.md` 和 ffdb mutation 校验为准。

## 资源管线

- 模板只通过 manifest 加载带 hash 的资源；build 清理发布目录时必须保留手写 `httpd/static/css/style.css`。
- 改依赖或 import 后比较主 bundle 与 editor chunk；静态 entry 不能引入 Plate editor runtime。

## rawBody 与安全

- `plate-plugin-keys.ts` 的节点/mark 字符串属于持久化格式，只能兼容新增；旧 rawBody 节点和 mark 必须继续 round-trip。
- rawBody 只用于编辑器数据和兼容 round-trip，不得作为 Entry 展示来源；展示只使用服务端消毒后的 Body。静态渲染不得加载编辑器 runtime。
- 不把未经消毒的 `Body` 交给 `dangerouslySetInnerHTML`。服务端 feed body 使用 `util.DefaultSanitize`（保留合法富文本），标题使用严格纯文本策略。
- 链接、图片和 embed 必须经过协议白名单并保留 2048 字节限制；视频 URL 解析保持本地、无依赖且只允许 YouTube。不得允许 `javascript:`、HTML data URL 或任意 iframe。

## Plate

- Plate/Slate 必须按兼容版本整体升级并核对 peer 依赖。不得重新引入 `@udecode/*` 或已失效的 `@platejs/autoformat`。
- 保留 Shift+Enter、图片 Backspace 先选中、H1-H6 exit-break 和 reset rules；blockquote 同时兼容旧扁平结构与新 `blockquote > p` 结构。

## 前端约束

- `tsconfig` 不使用 `baseUrl`；类型门禁使用 TypeScript 7 原生 `tsc`。TypeScript 6 compatibility package 仅供仍依赖 programmatic API 的 ESLint 工具链使用。
- ESLint 必须零 warning。Playwright 文件只放 `e2e/**`，Vitest 必须排除该目录。

## OAuth、配置与日志

- OAuth identity 必须按稳定的 `provider:user-id` key 查找；不能以“找不到”作为自动创建重复 profile 的理由。
- 配置中的 `~` 必须显式展开并保留原始 OS error。
- OAuth token、Cookie、session、密码和 secret 在任何日志级别均不得输出；生产日志只写 stdout/stderr，由 journald 管理。
