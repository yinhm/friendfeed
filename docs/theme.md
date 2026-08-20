# Theme 与样式架构

本文描述 ffdb **已经采用**的样式契约。目标不是把所有页面统一成一种写法，而是让
FriendFeed 站点样式与 React/Plate 组件样式拥有清晰、可预测的边界。

## 设计原则

1. 不新增 CSS-in-JS、预处理器或额外构建步骤。
2. `httpd/static/css/style.css` 继续是手写站点样式，并由 Go 静态资源管线发布。
3. Tailwind CSS v4 只负责 reset、React 应用/组件 utility 与 Plate UI；不把 SSR
   模板机械迁移成 utility class。
4. 样式边界按 **UI surface** 划分，而不是按 SSR/React 划分：Feed/Search 即使被
   React hydrate，仍使用 FriendFeed semantic class；Account/Plate 使用 Tailwind。
5. 颜色使用语义 token；新增站点规则不得散落裸颜色值。
6. 样式架构重构默认必须保持现有视觉。token 化、cascade layer、utility 化和 CSS
   cleanup 应尽量保持 computed style 等价；任何有意的视觉变化必须单独列出并独立评审。

## Cascade layer

全局 layer 顺序由 `httpd/app/src/styles/globals.css` 声明：

```css
@layer theme, base, site, components, utilities;
```

含义：

```text
Tailwind theme
    ↓
Tailwind Preflight (base)
    ↓
FriendFeed semantic CSS (site)
    ↓
component layer
    ↓
explicit Tailwind utilities
```

`style.css` 整体位于 `@layer site`。因此站点规则可以恢复 Preflight 去掉的列表、标题、
表单等浏览器默认样式；而 React/Plate 上显式写出的 utility 仍拥有更高 cascade
优先级。不要通过提高 selector specificity 或 `!important` 解决这两套样式之间的冲突。

## 两类 token

### 站点 token

`style.css` 使用最终 CSS 颜色值：

```text
--ff-bg / --ff-fg / --ff-font
--ff-link / --ff-link-light
--ff-muted / --ff-border / --ff-hairline
--ff-button-* / --ff-danger-*
--ff-box-*
--ff-quote-*
--ff-code-*
```

它们服务 FriendFeed surface（SSR templates、hydrated Feed、Search、Groups）。默认 theme
首先保持重构前 ffdb 的现有视觉：白底、Arial、经典蓝链接、灰色 sidebar box。

### 组件 token

`globals.css` 保留 Plate/shadcn 惯例的 HSL 三元组：

```text
--background / --foreground
--primary / --primary-foreground
--secondary / --muted / --accent
--destructive / --border / --input / --ring
```

格式不与 `--ff-*` 合并，但默认值与 FriendFeed theme 的语义保持一致。Plate UI、Radix
以及 Account React 页面使用 `bg-primary`、`text-muted-foreground`、`border-input` 等
语义 utility，而不是直接依赖 blue/gray/red palette。

## Surface 边界

```text
FriendFeed site surface                 Component/app surface
-----------------------                 ---------------------
SSR Feed / Groups / navigation          Account
hydrated Feed / Entry / Search          Plate editor / toolbar/dialog
semantic class names                    Tailwind + CVA + Radix
style.css / --ff-*                      globals.css component tokens
@layer site                             @layer utilities/components
```

不要因为一个区域由 React 渲染就自动改成 Tailwind；也不要让 `style.css` 使用宽泛的
`img`、`table`、`button` 等全局 selector 去影响组件树。

## Preflight

Tailwind Preflight 是全站唯一 reset，继续保留。站点层只恢复 FriendFeed surface 真正
需要的语义默认值。不要关闭 Preflight 后再自行维护第二套 reset。

## 响应式与媒体

- 主断点目前保持 `600px`，由 `style.css` 负责 Feed/sidebar 的窄屏布局。
- 正文与媒体图片必须 `max-width: 100%`，不能用 `.entry img` 一类 selector 给编辑器
  内部图片施加固定宽度或 display。
- Plate 自身的 image/resizable 样式由 utility class 决定，site layer 不应覆盖它。

## 新增样式规则

- FriendFeed surface：优先复用已有 semantic class 和 `--ff-*`；只有可复用的新语义才
  增加 selector/token。
- Component surface：优先使用现有 Plate UI/Radix primitive、CVA 和 semantic Tailwind
  token；避免重复粘贴长串物理颜色 utility。
- 不新增 inline `<style>`；React 中也避免同时用 utility 与 `style={{...}}` 描述同一
  CSS property。
- 不使用 `!important` 作为层级修复手段。

## Theme 扩展

当前只实现默认 `friendfeed` theme。未来若增加 dark 或用户主题：

1. 站点 token 通过根节点属性块整体覆盖；
2. 同一主题同步提供组件 HSL token；
3. dark theme 再在根节点添加 `.dark`，复用现有 Tailwind dark variant；
4. 服务端选择主题以避免 FOUC。

在真正增加第二套主题之前，不引入 cookie、主题切换 JS 或空的服务端状态。

## 验证

前端变更至少执行：

```bash
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build
```

`pnpm run build` 会额外验证：

- 关键 Tailwind utilities 确实生成；
- `style.css` 仍位于 `@layer site`；
- `style.css` 不重新引入会污染组件的全局 `button/input/img/table` selector；
- bundle 大小不突破已有预算。

涉及 Feed/Plate 交互时再执行 `pnpm run test:e2e`。