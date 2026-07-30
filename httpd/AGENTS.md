# AGENTS.md

本文件补充仓库根目录规则，适用于 `httpd/` 和 `httpd/app/`。

## 模板与渲染

- 交互放在 React bundle，不新增内联行为脚本；`feed.html` 的 `window.appData` 引导脚本是现有例外。
- 保留 SSR entry 首屏及 React mount 后替换 `#root` 的双渲染，改为纯 CSR 需单独决策。
- `show_share` 默认 false，只在 Home 打开；Public、普通 Feed 和未登录首页不得展示或加载编辑器。

## 资源管线

- 模板只通过 manifest 加载带 hash 的资源；build 清理发布目录时必须保留手写 `httpd/static/css/style.css`。
- 改依赖或 import 后比较主 bundle 与 editor chunk；静态 entry 不能引入 Plate editor runtime。

## rawBody 与安全

- `plate-plugin-keys.ts` 的节点/mark 字符串属于持久化格式，只能兼容新增；旧 rawBody 节点和 mark 必须继续 round-trip。
- rawBody 必须递归验证并由 entry 级错误边界回退到服务端消毒 HTML；静态渲染不得加载编辑器 runtime。
- 不把未经消毒的 `Body` 交给 `dangerouslySetInnerHTML`。服务端 feed body 使用 `util.DefaultSanitize`（保留合法富文本），标题使用严格纯文本策略。
- 链接、图片和 embed 必须经过协议白名单；调用 `js-video-url-parser` 前保留 2048 字节限制。不得允许 `javascript:`、HTML data URL 或任意 iframe。

## Plate

- Plate/Slate 必须按兼容版本整体升级并核对 peer 依赖。不得重新引入 `@udecode/*` 或已失效的 `@platejs/autoformat`。
- 保留 Shift+Enter、图片 Backspace 先选中、H1-H6 exit-break 和 reset rules；blockquote 同时兼容旧扁平结构与新 `blockquote > p` 结构。

## 前端约束

- `tsconfig` 不使用 `baseUrl`，保持 TypeScript 与 tsgo 双兼容。
- ESLint 必须零 warning。Playwright 文件只放 `e2e/**`，Vitest 必须排除该目录。

## OAuth、配置与日志

- OAuth identity 必须按稳定的 `provider:user-id` key 查找；不能以“找不到”作为自动创建重复 profile 的理由。
- 配置中的 `~` 必须显式展开并保留原始 OS error。
- OAuth secret 只允许 debug 模式输出；生产日志写 stdout/stderr，由 journald 管理。
