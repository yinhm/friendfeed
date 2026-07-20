# Frontend Upgrade Plan

更新基线：2026-07-20。前端位于 `httpd/app`，当前运行环境为 Node 24、Corepack 0.35、pnpm 8.15.9、React 19、Vite 7、Plate 31 和 Tailwind CSS 3。

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
- [ ] 制定 Plate 31 → 当前稳定主线的 API migration 清单；当前 registry 主线为 Plate 49。
- [ ] 同步升级 Slate、slate-react、slate-history、slate-hyperscript，禁止单包错位。
- [ ] 迁移 plugin 创建、editor/provider、serializer、media、floating UI 和自定义 Plate components。
- [ ] 处理已 deprecated 的 `@udecode/plate-*` 包与新包结构。
- [ ] 验证旧 `rawBody` JSON 可以加载、编辑并再次保存，不能破坏存量 entry。
- [ ] 针对 `dangerouslySetInnerHTML`、链接、media embed 和序列化结果做安全测试。
- [ ] 重新运行审计，优先消除 Plate 31 带来的 runtime vulnerabilities。

完成条件：旧内容兼容、核心编辑行为通过、审计风险显著下降、bundle 变化有记录。

## 8. Tailwind CSS 4 迁移

- [ ] Tailwind 3 → 4 单独迁移。
- [ ] 按 Tailwind 4 方式调整 PostCSS、主题变量、content discovery 和插件配置。
- [ ] 验证 Plate UI、Radix UI、编辑器 toolbar、dialog、popover 的全部样式。
- [ ] 检查与手写 `static/css/style.css` 的 reset、优先级和选择器冲突。

完成条件：页面视觉对比通过，production CSS 没有异常膨胀或丢失动态 class。

## 9. TypeScript 升级与覆盖扩展

- [ ] TypeScript 已因 plugin-react 6 的语法要求从 5.4 更新到最小兼容的 5.6.3；本阶段再评估最新 5.x，之后单独评估 TypeScript 7。
- [ ] 修复新版本带来的类型错误，不使用扩大 `skipLibCheck` 或批量 `any` 规避。
- [ ] 为核心 JS/JSX 文件逐步启用类型检查，优先覆盖 `App.jsx`、`entry.jsx`、`editor.jsx` 和网络请求工具。
- [ ] 评估增加 ESLint；lint 规则落地应独立提交，避免与功能迁移混杂。

完成条件：类型覆盖提高，测试与构建通过，不改变服务端 JSON 契约。

## 10. 可选的组件现代化

- [ ] 在依赖升级稳定后，再评估将 class components 迁移为 function components/hooks。
- [ ] 优先处理状态同步复杂的 `App` 和 `Entry`，迁移前先增加行为测试。
- [ ] 不为追求写法统一而改动稳定组件；每次迁移应有明确收益。

完成条件：属于可选维护工作，不阻塞前述安全与依赖升级。
