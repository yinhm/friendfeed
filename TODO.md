# Frontend Upgrade Plan

更新基线：2026-07-20。前端位于 `httpd/app`，当前运行环境为 Node 24、Corepack 0.35、pnpm 11.15.1、React 19、Vite 8、Plate 38 和 Tailwind CSS 3。

## 执行原则

- 严格按下列顺序执行；一个阶段一个提交，不混合升级。
- 不使用无边界的 `pnpm update --latest`。
- Plate 与 Slate 必须作为一个整体迁移，不能分别升级。
- Tailwind 4、TypeScript 7、Vite 8 分开迁移，避免错误来源混杂。
- 每阶段至少运行：`pnpm test`、`pnpm run typecheck`、`pnpm run build`。
- 涉及发布产物时同时运行 `go test ./httpd/...`，确认嵌入资源和模板路径有效。
- 保持编辑器按需加载；Public、Feed 和未登录页面不得加载 editor chunk。
- 记录每阶段的 `bundle.min.js`、`bundle.min.css` 和 editor chunk 大小，不能无解释地显著增长。

## 0. 固化升级基线

- [x] 保存 `pnpm outdated` 与 `pnpm audit` 摘要；基线见 `httpd/app/UPGRADE_BASELINE.md`。
- [x] 为 Home 编辑器补充加载、输入区和提交入口 smoke test；确认当前没有启用图片上传入口并记录现状。
- [x] 为 Public 与 Feed 页面补充“不渲染编辑器”的回归测试。
- [x] 记录 production bundle 的原始及 gzip 字节数。

完成条件：现有行为有足够测试保护，后续升级失败时可以明确定位回归阶段。

## 1. 清理依赖声明，不升级运行时代码

- [x] 逐一确认并移除没有直接 import 的依赖：`install`、`immutable`、`@styled-icons/boxicons-regular`、`@styled-icons/material`、`styled-components`、`throttle-debounce`。
- [x] 将 `typescript`、`@types/node`、`@types/react`、`@types/react-dom` 移到 `devDependencies`。
- [x] 将 `@types/node` 从 20.x 对齐到 Node 24 对应版本。
- [x] 重新生成 lockfile，确认被删除的包若仍是传递依赖，会由真正的上游包声明。

完成条件：测试、类型检查和构建通过；运行时功能与 bundle 行为不变或体积下降。

## 2. 当前主版本内的补丁升级

- [x] React、React DOM 保持 19.x，确认当前 19.2.7 已是最新 19.x。
- [x] Vite 保持 7.x，确认当前 7.3.6 已是最新 7.x。
- [x] Tailwind CSS 保持 3.x，更新到 3.4.19。
- [x] 更新 PostCSS；确认 Autoprefixer、Testing Library、jest-dom 等已是当前 major 的最新兼容版本。
- [x] 更新 `class-variance-authority`、`cmdk`、`react-tweet`、`tailwind-merge` 等独立小型依赖；`react-lite-youtube-embed` 2.6 因缺失 source map 回退到 2.4。
- [x] 再次运行 `pnpm audit`：moderate 从 12 降至 11，16 个 high 仍集中于 Plate 31 及其旧依赖树，留待整体 migration。

完成条件：不修改业务组件 API；所有验证通过；审计问题数量不增加。

## 3. Radix UI 组件组升级

- [x] 在同一阶段统一升级 Avatar、Checkbox、Dialog、Dropdown Menu、Popover、Separator、Slot、Toolbar、Tooltip。
- [x] 检查 Dialog/Popover/Menu 的 focus trap、Portal、Esc 关闭、键盘导航和 z-index；增加 Radix wrapper 交互测试。
- [x] 检查编辑器固定工具栏、浮动工具栏、链接弹层、媒体弹层和 tooltip；Home 编辑器 smoke test 与 production build 均通过。

结果：Radix 依赖树减少 22 个包；主 bundle 基本不变（210.15 kB，gzip 65.44 kB），editor chunk 从基线 1,887.35 kB 降至 1,866.89 kB（gzip 从 545.29 kB 降至 541.76 kB）。审计数量保持 16 high、11 moderate、2 low，问题仍来自 Plate 31 和旧工具依赖树。

完成条件：交互测试通过，无 hydration、focus 或 Portal 回归。

## 4. pnpm 与 lockfile 升级

- [x] 将 `packageManager` 从 pnpm 8.15.9 升级到 pnpm 11.15.1；该版本要求 Node >=22.13，符合项目的 Node 24 约束。
- [x] 使用 Corepack 0.35.0 激活目标版本，将 lockfile 从 v6 转换为 v9。
- [x] 验证 `CI=true pnpm install --frozen-lockfile` 可复现依赖树，并显式允许 esbuild 安装脚本。
- [x] 将 pnpm 11 不再从 package.json 读取的 peer/override 设置迁至 `pnpm-workspace.yaml`；保留 React 19 单版本约束，无 peer dependency 警告。

结果：lockfile 转换期间固定原有直接依赖版本，未夹带 Plate、TypeScript 或其他后续阶段的升级。pnpm 11 的 supply-chain 检查所需 Radix 版本例外已显式记录。主 bundle 保持 210.15 kB（gzip 65.43 kB），editor chunk 为 1,847.99 kB（gzip 535.40 kB）；production audit 降至 2 high、1 moderate，均来自 Plate 31。

完成条件：本地和部署构建都能使用锁定版本复现依赖树。

## 5. 构建与测试工具 major 升级

- [x] Vite 7 → 8.1.5。
- [x] `@vitejs/plugin-react` 4 → 6.0.3；其声明文件使用 arbitrary module identifier export，最小兼容升级 TypeScript 5.4.3 → 5.6.3。
- [x] Vitest 3 → 4.1.10，jsdom 26 → 29.1.1。
- [x] 检查 Vite build 输出命名，继续生成固定的 `bundle.min.js`、`bundle.min.css` 和带 hash 的 lazy editor chunk。
- [x] 检查 `scripts/publish-build.mjs`，确认保留手写的 `static/css/style.css`。
- [x] 确认 production 不包含 source map，development 为主 bundle 和 editor chunk 生成 source map。

结果：8 项测试、类型检查和 production build 通过。主 bundle 从 210.15 kB 降至 207.43 kB（gzip 64.64 kB），CSS 从 57.61 kB 降至 54.86 kB（gzip 11.76 kB），editor chunk 从 1,847.99 kB 降至 1,845.83 kB（gzip 525.04 kB）。

完成条件：Go 模板无需改变资源 URL；production 嵌入资源完整；lazy editor 只执行一次。

## 6. 编辑器瘦身

- [x] 根据实际产品能力盘点 Plate plugins，结果记录在 `httpd/app/EDITOR_FEATURES.md`；删除从未注册的 comments 和无调用的 DOCX serializer。
- [x] 删除 comments 的断开 UI 组件，并移除未导入的 selection、toggle、plate-ui 直接依赖。
- [x] 用 `plate-common` 和直接依赖的 HTML serializer 替换 `@udecode/plate` 总包；安装依赖减少 129 个，editor chunk 基本不变（1,845.83 kB，gzip 525.22 kB），证明原总包代码已被 Vite tree-shake；删除 comments UI 后 CSS 从 54.86 kB 降至 54.00 kB。
- [x] 保留 code block、emoji、image/media、caption、font/layout 等已注册能力；它们分别具有输入触发路径或承担旧 `rawBody` 节点兼容，不能仅因缺少固定工具栏按钮而删除。

完成条件：现有编辑、序列化和历史内容回显保持兼容，editor chunk 明确下降或给出保留理由。

## 7. Plate 与 Slate 整体迁移

- [x] 建立 31 → 49 分段迁移清单，见 `httpd/app/PLATE_MIGRATION.md`；先完成安全兼容台阶，将 core/media 等升级至 36.x，保留四个必须与旧 UI 同步迁移的 31.x 包。
- [x] 为旧 `rawBody` JSON、旧 HTML fallback 以及再次提交 JSON/HTML 增加兼容测试；安全台阶后 production audit 从 2 high、1 moderate 降至 1 moderate。
- [x] 将 code-block 与 floating 升至 36.x：保留原语言值并迁到 UI 层，floating toolbar 改用显式 editor/focus 状态；editor chunk 降至 1,760.23 kB（gzip 491.99 kB）。
- [x] 将 combobox 与 emoji 升至 36.x：用 Ariakit inline input 替换已删除的全局 combobox store，并增加 `:` 触发生成 `emoji_input` 节点的回归测试；editor chunk 为 1,797.33 kB（gzip 504.65 kB）。
- [x] 制定 Plate 31 → 当前稳定主线的 API migration 清单；Plate 37 插件对象迁移与 Plate 49 包重组的逐项清单见 `httpd/app/PLATE_MIGRATION.md`。
- [x] 同步升级 Slate、slate-react、slate-history、slate-hyperscript：对齐到 Plate 37 迁移边界的 Slate 0.103、Slate React 0.110.3、History 0.109；Hyperscript 保持该时期最新的 0.100。
- [x] 迁移 plugin 创建、editor/provider、serializer、media、floating UI 和自定义 Plate components；Plate 全家桶升级到 37，并改用 plugin object 与 editor-instance API。
- [x] 处理已 deprecated 的 `@udecode/plate-*` 包与新包结构：迁移至 Plate 49 的 `platejs`/`@platejs/*`，移除全部旧 Plate 38 直接依赖，并为异步 HTML serializer 增加独立静态组件边界。
- [x] 验证旧 `rawBody` JSON 可以加载、编辑并再次保存，不能破坏存量 entry。
- [x] 针对 `dangerouslySetInnerHTML`、链接、media embed 和序列化结果做安全测试；修复 AJAX/JSON feed 未 sanitize 的 XSS 缺口，并锁定 `javascript:`、HTML data URL 与恶意 iframe 不得进入序列化结果。
- [x] 重新运行审计并将 Plate 全家桶统一升级至 38，消除 Plate core 的 high 漏洞；剩余 `js-video-url-parser` moderate 所声称的修复版本 0.5.2 尚未发布，应用层已在调用第三方 parser 前拒绝超过 2048 字节的 URL，锁定测试并保留审计记录。

完成条件：旧内容兼容、核心编辑行为通过、审计风险显著下降、bundle 变化有记录。

## 8. Tailwind CSS 4 迁移

- [x] Tailwind 3 → 4 单独迁移：升级至 Tailwind 4.3.3，动画插件替换为 v4 CSS 版本，并增加 production selector 验证门。
- [x] 按 Tailwind 4 方式调整 PostCSS、主题变量、content discovery 和插件配置：使用 `@tailwindcss/postcss`、CSS-first `@theme`/`@source`/dark variant，移除旧 JavaScript config 与独立 autoprefixer。
- [x] 验证 Plate UI、Radix UI、编辑器 toolbar、dialog、popover 的关键生成样式；production build 现在锁定主题色、focus/checked 状态、dialog/popover 动画、dark/print variant、响应式与复杂 arbitrary selector。
- [x] 检查与手写 `static/css/style.css` 的 reset、优先级和选择器冲突；收窄后加载的全局 form/button/link 规则，避免覆盖 Plate/Radix utilities，并由构建脚本阻止这些全局选择器回归。

完成条件：页面视觉对比通过，production CSS 没有异常膨胀或丢失动态 class。

## 9. TypeScript 升级与覆盖扩展

- [x] TypeScript 已因 plugin-react 6 的语法要求从 5.4 更新到最小兼容的 5.6.3；现已精确升级至最后的 5.x（5.9.3），TypeScript 7 保留为独立评估项。
- [x] TypeScript 5.9.3 未引入新的类型错误；未扩大既有 `skipLibCheck`，也未增加批量 `any` 规避。
- [x] 为核心 JS/JSX 文件逐步启用类型检查，优先覆盖 `App.jsx`、`entry.jsx`、`editor.jsx` 和网络请求工具。
  - [x] `App.jsx` 已启用 `@ts-check`，补齐 feed、分页、context、FormData 与 `window.appData` 的 JSDoc 契约，并消除全部严格模式错误。
  - [x] `entry.jsx` 已启用 `@ts-check`，以共享 JSDoc 模型覆盖 entry、feed、thumbnail、comment、like、命令回调及表单事件，保持现有服务端 JSON 字段和交互流程。
  - [x] `editor.jsx` 已启用 `@ts-check`，以 Plate 49 的公开 `Value`/`PlateEditor` 类型约束持久化内容、编辑器引用、组件参数与提交回调，并按新版 `onChange` 契约读取 `value`。
  - [x] `utils.js` 已启用 `@ts-check`，以泛型约束 JSON 请求响应，并补齐 URL、表单字段、FormData 与 `intersperse` 的输入输出契约。
- [x] 增加 ESLint 10 flat config 与 `pnpm lint` 门禁，覆盖 JS/JSX/TS/TSX、浏览器/Node/Vitest 全局变量及 React Hooks；Plate 迁移边界的显式 `any` 留给后续类型重构。

完成条件：类型覆盖提高，测试与构建通过，不改变服务端 JSON 契约。

## 10. 可选的组件现代化

- [x] 已评估 class components 迁移：无状态的 `App` 外壳可直接函数化；`Feed` 的轮询/发帖状态与 `Entry` 的 props/本地编辑状态同步必须先由回归测试锁定，避免机械改写生命周期。
  - `EntryCommand*`、`EntryCommentForm`、`EntryComment`、`EntryLike` 等叶子组件可独立迁移，但收益低于先解决 `App`/`Feed`/`Entry` 的状态边界。
  - 推荐顺序为：补 `Feed`/`Entry` 行为测试 → 迁移无状态 `App` → 迁移 `Feed` → 最后迁移 `Entry`；每步单独提交并验证。
- [x] 优先处理状态同步复杂的 `App` 和 `Entry`，迁移前先增加行为测试。
  - [x] 已增加迁移前行为测试，锁定 `Feed` 的 20 秒刷新/卸载清理/发帖置顶，以及 `Entry` 的父级刷新、展开 likes 和本地编辑状态同步语义。
  - [x] 无状态 `App` 外壳已迁移为 function component；它继续在渲染时读取当前 URL 与 `window.appData`，未引入多余 hook。
  - [x] `Feed` 已迁移为 function component；轮询使用 effect 清理并通过 ref 读取最新 URL，异步刷新保持 class `setState` 的局部合并语义，发帖使用不可变置顶更新。
  - [x] 主 `Entry` 容器已迁移为 function component；新 `props.entry` 到达时同步未展开的服务端数据，本地展开/编辑状态保持优先，异步操作使用函数式 state 更新。
- [x] 不为追求写法统一而改动稳定组件；本轮只迁移有明确状态收益的 `App`/`Feed`/主 `Entry`，有稳定局部状态的叶子 class 保持现状。
  - 明确保留 `EntryCommand*`、`EntryCommentForm`、`EntryComment` 与 `EntryLike`：转换前三个无状态命令只会统一写法，其余组件需先有独立交互收益与回归覆盖。

完成条件：属于可选维护工作，不阻塞前述安全与依赖升级。

## 11. 现代化收尾

- [x] 移除 `@udecode/cn`：`withRef`/`createPrimitiveElement` 改从 `platejs/react`（一等公民转出口）导入；`cn`/`withProps`/`withCn`/`withVariants` 无官方继任，内联为 `src/components/cn.tsx`；`@udecode/*` 依赖清零。
- [x] 类型覆盖补全：`content.jsx`、`search.jsx`、`entry-like.jsx`、`index.jsx` 启用 `@ts-check`，并修正 `LikeData` 为服务端真实形状（`from` 必需，`placeholder`/`body` 可选），stage 9 闭环。
- [x] 将 soft break 验证从 `Hotkeys.isSoftBreak` 映射级升级为真实 `insertSoftBreak` 执行断言（同块插入 `\n` 而非新块），与图片 `deleteBackward` 测试同级。
- [x] 清理 `entry-state.test.jsx` 的 `act()` 输出噪音（rerender 包裹 `act()`，警告从 3 降为 0）。
- [x] 跟踪 `js-video-url-parser@0.5.2`：2026-07-21 复查仍未发布（上游自 2021-11 休眠），2048 字节限制改为常驻缓解并在源码注释中记录验证日期，不再标记为临时方案；后续升级前复查 registry。

完成条件：旧组织名依赖清零，核心 JS 文件类型检查全覆盖，测试输出干净。

## 12. 结构演进（需逐项决策）

- [x] entry 读路径从 `dangerouslySetInnerHTML` 迁移到 rawBody 组件渲染：`EntryBody` 以共享静态组件表（`static-components.tsx`，URL 白名单 + vendor parser）渲染 rawBody，仅 HTML body 的存量 entry 回退到服务端消毒 HTML；主 bundle 206→230 kB，editor chunk 保持懒加载。
- [x] SSR/CSR 评估与死代码清理：确认双渲染现状（SSR 内容挂载即被 React 丢弃）；删除 `feed.html` 中带死 jQuery 的 SSR sharebox、修正 `entry.Via.name` 大小写；经用户决定保留 entry 的 SSR 首屏渲染，纯 CSR 迁移暂缓。
- [x] `EntryCommentForm` 迁移：先为 class 版建 4 例行为测试（初值、提交、空提交拦截、取消传值），再转换为 `useState` 函数组件，同一套测试零改动通过；其余叶子 class 组件继续保留。
- [x] editor chunk 第二轮瘦身（按 fixture 决策）：移除 font color/backgroundColor/size 注册（存量 mark 数据 round-trip 有 fixture 保护）；删除死文件 `plate-editor.tsx` 并从依赖移除 react-dnd（实证从未进入 bundle）；react-tweet 与 lite-youtube-embed 分属 twitter/youtube 嵌入，非重复依赖，保留。editor chunk 1,397,128→1,397,100 B，依赖数 -2。

完成条件：每项独立提交、独立验证；渲染安全模型与体积变化有记录。

## 13. 常态卫生

- [x] 已配置 Dependabot：前端 pnpm 与根目录 Go modules 每周分组更新 minor/patch，major 保持人工迁移；pnpm 全局启用 `minimumReleaseAge`，Radix 安装链例外显式记录在 `pnpm-workspace.yaml`。
- [x] 评估 TypeScript 7（tsgo）：tsconfig 迁移为无 `baseUrl` 的相对 `paths`（TS 5.9 与 TS7 双兼容）+ `*.css` 声明；`typecheck:tsgo` 可用且零错误，本项目实测 tsc 13.7s → tsgo 6.9s（约 2x）；正式门禁仍用 tsc，TS7 待稳定后切换。
- [x] 点名升级老次要依赖：`lucide-react` 0.359→1.25.0（上游移除 Twitter 品牌图标，本地无使用点随删）、`tailwind-merge` 2→3.6.0（Tailwind 4 官配）、`@ariakit/react` 0.4.6→0.4.34；`date-fns` 零引用删除；`react-dnd` 已于 12.4 移除。editor chunk 1,397,100→1,415,682 B（新依赖略大，已记录）。
- [x] Playwright E2E 覆盖读写主链路：`scripts/e2e/` 起临时后端与 web（随机端口防僵尸冲突），通过 `ForceArchiveFeed` 播种 public 内容；再经 `PutOAuth` 创建临时用户、注入正常签名 session，验证 Home 懒加载 editor、编辑、提交与回显，并断言 public 不请求 editor chunk。
- [x] production build 增加 bundle 体积门禁：分别限制主入口、静态 rawBody renderer、懒加载 editor 和 CSS 的原始及 gzip 字节数，显著增长必须显式审核并调整预算。
- [x] Vitest 增加 V8 coverage 基线：覆盖全部自有 `src` 生产源码，生成终端、JSON summary 和 HTML 报告；CI 执行 coverage 测试，先观察真实数据，不设置缺乏依据的全局阈值。
- [x] 完成 2026-07-22 同主版本卫生更新：PostCSS 8.5.20→8.5.21、typescript-eslint 8.64.0→8.65.0；其余候选均为需要独立验证的 major。
- [x] `@testing-library/jest-dom` 6.9.1→7.0.0：继续使用 Vitest 专用入口，现有 `@testing-library/dom` 10.4.1 满足新 peer 契约，测试断言行为保持不变。
- [x] `react-lite-youtube-embed` 2.6.0→3.6.0：锁定 wrapper、播放按钮和 privacy-enhanced iframe 契约；3.6.0 仍引用未随 npm 包发布的 source map，测试噪音是已确认的上游发布问题，不在本地静默屏蔽。
- [x] 补齐 `utils.js` 请求边界测试：锁定同源凭据、URL-encoded command、FormData 不覆盖 multipart boundary、JSON 解码、诊断输出和 `intersperse`，不为覆盖率改生产接口。
- [x] 补齐 Search 表单契约测试：锁定浏览器默认 GET、`/search` action、`q` 参数名和受控输入，保护服务端既有 `/search?q=` 接口。

完成条件：依赖更新有机制保障，E2E 冒烟纳入验证门。
