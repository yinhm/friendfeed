# Plate 31 to 49 migration

Plate 49 is not package-compatible with Plate 31. The migration is split into reviewable checkpoints so stored `rawBody` data and the existing editor UI remain testable throughout.

## Checkpoint 1: security-compatible 36.x core

- Upgrade the 31.x plugin family to the last compatible 36.x release where its current UI API remains available.
- Keep combobox and emoji on 31.0.0 temporarily while their inline-input UI is rebuilt. Code-block and floating have moved to 36.x: the language list is now UI-owned and the floating toolbar supplies explicit editor/focus state.
- Upgrade `plate-common`, `plate-media`, `plate-link` and the remaining headless plugins. This removes both high-severity advisories affecting Plate 31.
- Protect JSON `rawBody` and legacy HTML loading plus JSON/HTML submission with tests before changing plugin construction.

## Checkpoint 2: plugin object API

- Replace the removed global combobox store with the inline input element model introduced in combobox/emoji 34, then align both packages to 36.x.
- Migrate `createXPlugin()` registrations and `createPlugins()` to the plugin object/configuration API introduced after 36.
- Update element/leaf props and floating/combobox components together with their plugins.
- Keep node type strings compatible with stored content (`p`, headings, lists, code blocks, images and media embeds).

## Checkpoint 3: Plate 49 package layout

- Rename `@udecode/plate-*` packages to `@platejs/*`.
- Move basic marks/nodes to `@platejs/basic-nodes`, classic lists to `@platejs/list-classic`, and indent lists to `@platejs/list`.
- Move editor setup to the v49 editor/plugin APIs and update HTML serialization to use editor-bound components.
- Upgrade the Slate package family as one unit to the versions required by Plate 49.

Each checkpoint must pass the editor compatibility tests, the Home/Public/Feed lazy-loading tests, type checking, production build, frozen installation and peer checks.
