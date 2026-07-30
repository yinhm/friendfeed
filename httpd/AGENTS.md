# AGENTS.md

本文件补充仓库根目录规则，适用于 `httpd/` 和 `httpd/app/`。

## 模板与渲染

- 模板不新增内联行为脚本或事件处理器；交互放在 React bundle。`feed.html` 中引导 `window.appData` 的脚本是现有例外。
- SSR entry 首屏明确保留；React mount 后替换 `#root` 的双渲染现状未经架构决策不得删除或改成纯 CSR。
- `show_share` 默认 false，只在 Home 打开；Public、普通 Feed 和未登录首页不得展示或加载编辑器。

## 资源管线

- Vite 生成带 hash 的 JS/CSS 和 `static/manifest.json`；模板从 manifest 取 URL，禁止写死 bundle 文件名。
- `pnpm run build` 会清理发布目录，但必须保留手写 `httpd/static/css/style.css`；其版本由内容指纹自动生成，不手改 query version。
- 前端 build 必须先于 Go build。改 import/依赖后立即比较主 bundle 与 editor chunk；静态 entry 渲染不能把 Plate editor runtime 拉进主 bundle。

## rawBody 与安全

- `plate-plugin-keys.ts` 中的节点/mark 字符串是持久化格式，只能兼容新增，不能改值或复用旧 key。
- entry 优先通过 `entry-body.jsx` 的静态组件表渲染 rawBody；rawBody 是客户端输入，必须递归验证并由 entry 级错误边界回退到服务端消毒 HTML。
- Feed 列表正文按 300 字符截断并提供 “Read more...”；entry 独立页（`onpage`）显示全文，不得再次绕过。
- 不把未经消毒的 `Body` 交给 `dangerouslySetInnerHTML`。服务端 feed body 使用 `util.DefaultSanitize`（保留合法富文本），标题使用严格纯文本策略。
- 链接、图片和 embed 必须经过协议白名单；调用 `js-video-url-parser` 前保留 2048 字节限制。不得允许 `javascript:`、HTML data URL 或任意 iframe。
- 编辑器仍需保留旧 rawBody 的节点与 mark round-trip；“工具栏没有按钮”不等于历史格式可以删除。

## Plate 与组件

- Plate/Slate 按兼容版本整体升级，不拆开碰运气；以锁文件、peer 依赖图和契约测试为准。
- Shift+Enter 软换行、图片 Backspace 先选中、H1-H6 exit-break、reset rules 都已有行为测试，不得静默回退。
- Plate 53 的 Markdown 快捷输入由各功能插件的 `inputRules` 和本地 `MarkdownShortcutsPlugin` 提供；不要重新引入已经失效的 `@platejs/autoformat`。
- blockquote 的新持久化形状是 `blockquote > p`；必须继续读取、渲染并序列化旧的扁平 blockquote，首次编辑可规范化为新形状。
- `cn`/`withProps` 来自 `components/cn`；不重新引入 `@udecode/*`。
- plate-ui 组件采用当前官方 registry 风格：普通函数组件 + `PlateElement`/`PlateLeaf` + `cn`（React 19 ref-as-prop）。新组件照此写法，参考 `https://platejs.org/r/<name>.json`；不再使用 `withCn`/`withVariants` 包装。
- 稳定组件不做无收益的机械改写；组件改动必须先补当前实现的行为测试。

## 类型、测试与 E2E

- `tsconfig` 不使用 `baseUrl`，保持 TypeScript 5.9 与 tsgo 双兼容；新增/修改 JSX 应启用 `// @ts-check` 并补 JSDoc 契约。
- ESLint 必须零 warning。Playwright 文件只放 `e2e/**`，Vitest 必须排除该目录。
- E2E 从 `httpd/app` 执行 `pnpm run test:e2e`；脚本使用随机端口、临时 DB，通过 `ForceArchiveFeed` 播种，并只清理本次临时进程。
- 前端完整门禁：`pnpm lint && pnpm run typecheck && CI=true pnpm test && pnpm run build`；涉及 Go、模板、嵌入资源时再跑根目录 Go 门禁。

## OAuth、配置与日志

- OAuth identity 必须按稳定的 `provider:user-id` key 查找；不能以“找不到”作为自动创建重复 profile 的理由。
- `~` 路径不能假定由 systemd 或底层库自动展开；配置加载处应使用明确的绝对路径或统一展开策略，并保留原始 OS error。
- debug 日志不得在非 debug 模式输出 OAuth key/token/secret；生产日志写 stdout/stderr，由 journald 管理。
