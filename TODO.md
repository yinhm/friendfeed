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

- [ ] React、React DOM 保持 19.x，只更新当前 major 内版本。
- [ ] Vite 保持 7.x，更新到最新 7.x。
- [ ] Tailwind CSS 保持 3.x，更新到最新 3.4.x。
- [ ] 更新 PostCSS、Autoprefixer、Testing Library、jest-dom 等兼容补丁版本。
- [ ] 更新 `class-variance-authority`、`cmdk`、`react-tweet` 等独立小型依赖的兼容版本。
- [ ] 再次运行 `pnpm audit`，记录仍只能通过 major migration 解决的项目。

完成条件：不修改业务组件 API；所有验证通过；审计问题数量不增加。

## 3. Radix UI 组件组升级

- [ ] 在同一阶段统一升级 Avatar、Checkbox、Dialog、Dropdown Menu、Popover、Separator、Slot、Toolbar、Tooltip。
- [ ] 检查 Dialog/Popover/Menu 的 focus trap、Portal、Esc 关闭、键盘导航和 z-index。
- [ ] 检查编辑器固定工具栏、浮动工具栏、链接弹层、媒体弹层和 tooltip。

完成条件：交互测试通过，无 hydration、focus 或 Portal 回归。

## 4. pnpm 与 lockfile 升级

- [ ] 将 `packageManager` 从 pnpm 8 升级到当前受支持版本；pnpm 11 需作为独立提交评估。
- [ ] 使用 Corepack 激活目标版本并重新生成 lockfile。
- [ ] 验证 `pnpm install --frozen-lockfile` 可在干净环境复现。
- [ ] 检查 peer dependency 与 override 结果，不能通过忽略警告掩盖冲突。

完成条件：本地和部署构建都能使用锁定版本复现依赖树。

## 5. 构建与测试工具 major 升级

- [ ] Vite 7 → 8。
- [ ] `@vitejs/plugin-react` 4 → 6。
- [ ] Vitest 3 → 4，jsdom 26 → 29。
- [ ] 检查 Vite library/build 输出命名，继续生成固定的 `bundle.min.js`、`bundle.min.css` 和带 hash 的 lazy chunks。
- [ ] 检查 `scripts/publish-build.mjs`，确保不会覆盖手写的 `static/css/style.css`。
- [ ] 确认 production 不包含 source map，development 仍可生成 source map。

完成条件：Go 模板无需改变资源 URL；production 嵌入资源完整；lazy editor 只执行一次。

## 6. 编辑器瘦身

- [ ] 根据实际产品能力盘点 Plate plugins，删除未启用或无入口的 comments、DOCX、复杂 media、emoji、code block 等插件。
- [ ] 删除随废弃插件失去引用的 UI 组件和直接依赖。
- [ ] 对比 editor chunk 体积，并记录每项功能与体积变化。
- [ ] 不因“仓库内无引用”删除仍由动态配置、插件注册或外部内容格式使用的能力。

完成条件：现有编辑、序列化和历史内容回显保持兼容，editor chunk 明确下降或给出保留理由。

## 7. Plate 与 Slate 整体迁移

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

- [ ] 先将 TypeScript 5.4 更新到最新兼容 5.x，再单独评估 TypeScript 7。
- [ ] 修复新版本带来的类型错误，不使用扩大 `skipLibCheck` 或批量 `any` 规避。
- [ ] 为核心 JS/JSX 文件逐步启用类型检查，优先覆盖 `App.jsx`、`entry.jsx`、`editor.jsx` 和网络请求工具。
- [ ] 评估增加 ESLint；lint 规则落地应独立提交，避免与功能迁移混杂。

完成条件：类型覆盖提高，测试与构建通过，不改变服务端 JSON 契约。

## 10. 可选的组件现代化

- [ ] 在依赖升级稳定后，再评估将 class components 迁移为 function components/hooks。
- [ ] 优先处理状态同步复杂的 `App` 和 `Entry`，迁移前先增加行为测试。
- [ ] 不为追求写法统一而改动稳定组件；每次迁移应有明确收益。

完成条件：属于可选维护工作，不阻塞前述安全与依赖升级。
