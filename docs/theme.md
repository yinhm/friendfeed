# Theme 设计规范

本文定义整个项目（SSR 模板 + React bundle）的主题体系。目标是：简洁、无新增构建
依赖、默认主题接近还原 FriendFeed 原始设计。UI 主题只影响展示，不承担任何权限或
数据语义。

## 现状与约束

- 页面样式来自两处（`httpd/templates/layout.html`）：先加载 Vite 打包的 React
  bundle CSS（Tailwind preflight + shadcn 风格 token），后加载手写的
  `httpd/static/css/style.css`。后者按 AGENTS.md 约定必须保持手写、不进构建。
- React 侧已有一套 shadcn 惯例 token（`httpd/app/src/styles/globals.css`）：
  `--background`、`--foreground` 等 **hsl 分量三元组**，经 `hsl(var(--x))` 使用，
  dark 变体挂在 `.dark` class 上。
- `style.css` 目前硬编码颜色（`#00c`、`#77c`、`#666`、`#eee`、`#bdbdbb` 等），
  仅 `body` 引用了 `hsl(var(--background, ...))`。

因此主题体系分两层，边界必须清晰：

```text
站点层  --ff-*        style.css 定义，最终颜色值（hex/named），服务 SSR 页面
组件层  --background 等  React bundle 定义，hsl 三元组，服务 shadcn 组件
```

不合并两层格式：站点层新 token 一律用最终颜色值，不继承 hsl 三元组这一历史惯例。
`style.css` 在 bundle CSS 之后加载，同名 `:root` 变量以 `style.css` 为准——主题
块如需影响 React 组件，在同一主题块内按需覆盖组件层三元组，而不是让 React 反向
定义站点层颜色。

## Token 清单

站点层 token 定义在 `style.css` 顶部的 `:root` 块，命名 `--ff-<语义>`，只按语义
命名、不按具体页面命名。首版 token 固定为：

```css
:root {
    /* 基础 */
    --ff-bg: #fff;
    --ff-fg: #000;
    --ff-font: Arial, sans-serif;

    /* 链接 */
    --ff-link: #00c;          /* 正文与导航链接 */
    --ff-link-light: #77c;    /* likes/comments 区内链接、action-link */

    /* 辅助文本与边线 */
    --ff-muted: #666;         /* info/likes/comment 弱化文本 */
    --ff-border: #bdbdbb;     /* 输入框、头像、按钮边框 */
    --ff-hairline: #eee;      /* entry/header 分隔线 */

    /* 控件 */
    --ff-button-bg: #eee;
    --ff-button-fg: #333;
    --ff-danger: #b03030;
    --ff-danger-border: #8f2727;
    --ff-danger-fg: #fff;

    /* FriendFeed 标志性侧栏蓝盒 */
    --ff-box-bg: #ebf3f8;
    --ff-box-border: #c7dbec;

    /* 引用块 */
    --ff-quote-border: #d1d5db;
    --ff-quote-fg: #4b5563;

    /* 代码块 */
    --ff-code-bg: #eeffcc;
    --ff-code-border: #ac9;
    --ff-code-fg: #333;
    --ff-code-inline-bg: #e9ecef;
}
```

大小写变体视为同一颜色（如 `#BDBDBB` 与 `#bdbdbb` 同归 `--ff-border`）。

迁移规则：`style.css` 中现有硬编码颜色逐一替换为上表 token；语义相同才共用一个
token，颜色值恰好相同但语义不同的（如 `#eee` 既是分隔线又是按钮底色）必须拆开。
新增样式不得再写裸颜色值；确属一次性的装饰色（如 smile/comment 图标）除外。

明确豁免：`style.css` 中整段 Bootstrap 风格 table 块（`--bs-table-*` 变量及其
`#212529`/`#dee2e6`/`rgba()` 取值）是待清理遗留，不纳入 `--ff-*` token，本次
不改动也不计入验收；它的去留由后续独立清理决定。

## 默认主题：friendfeed

默认主题即 `:root` 中的取值，目标是接近还原 FriendFeed 原始设计（白底、Arial、
经典蓝链接、浅蓝侧栏盒）。现有 `#00c`/`#77c`/`#666`/`#eee` 就是该风格的既有还原，
全部保留；相对现状的唯一视觉变更：

- 侧栏 `.menu` 由灰盒（`#eee` + silver 边框）改为 FriendFeed 标志性浅蓝盒：
  `background: var(--ff-box-bg); border: 1px solid var(--ff-box-border)`。

`--ff-box-*` 取值为按截图目测的近似值，允许在实现时微调，但必须停留在「白底 +
浅蓝盒」的方向上，不引入渐变、阴影或圆角堆砌。

## 主题切换机制

- 主题以 `data-theme` 属性挂在 `<html>` 上，由 layout.html 渲染：
  `<html data-theme="{{ theme }}">`。无值或未知值一律回退默认主题。注意
  layout.html 目前没有 `<html>` 标签（`<!doctype html>` 直接接 `<head>`），
  实施时须先补出该元素；后续 dark 主题的 `class="dark"` 也挂在它上面。
- 每个主题是 `style.css` 中一个覆盖块，只覆盖 token，不得覆盖布局规则：

```css
[data-theme="dark"] {
    --ff-bg: ...;
    /* 同一块内按需覆盖组件层三元组，并同步输出 .dark 语义 */
}
```

- 主题选择持久化在 cookie `theme`（一年有效，`SameSite=Lax`）。httpd 读 cookie
  注入模板即可，无需 JS 参与，天然无 FOUC。切换入口放在 account 设置页，首版
  可以不提供 UI——机制先行，默认主题唯一。
- dark 主题不属于首版范围。落地时要求：token 全量覆盖、同步覆盖组件层三元组、
  并让 `<html>` 同时携带 `class="dark"` 以命中 React 侧
  `@custom-variant dark (&:is(.dark *))`。

## 非目标

- 不引入 CSS-in-JS、预处理器或额外构建步骤；`style.css` 保持手写。
- 不重构 React 组件内部样式，不迁移 Tailwind 版本或改动其 token 命名。
- 不做用户自定义配色（原版 FriendFeed 的 custom theme 不在范围内）。

## 实施步骤

1. 在 `style.css` 顶部建立 `:root` token 块，将现有硬编码颜色按语义替换为
   `var(--ff-*)`，此步不产生任何视觉变化（`.menu` 除外，见步骤 2）。
2. 应用侧栏浅蓝盒（`--ff-box-*`），核对桌面与 <=600px 移动布局。
3. layout.html 增加 `data-theme` 渲染与 cookie 读取；未设 cookie 时不输出属性。
4. 后续（非首版）：account 设置页主题选择、dark 主题块。

验收：无 cookie 时页面渲染与现状逐像素一致（侧栏盒颜色除外）；`style.css` 中
除 token 块、既有图标引用与被豁免的 legacy table 块外不再有裸颜色值；
`pnpm run build` 与 Go 构建不因此变更。
