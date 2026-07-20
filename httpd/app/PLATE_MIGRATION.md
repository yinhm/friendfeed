# Plate 31 to 49 migration

Plate 49 is not package-compatible with Plate 31. The migration is split into reviewable checkpoints so stored `rawBody` data and the existing editor UI remain testable throughout.

## Checkpoint 1: security-compatible 36.x core

- Upgrade the 31.x plugin family to the last compatible 36.x release where its current UI API remains available.
- Combobox, emoji, code-block and floating have moved to 36.x. The global combobox store was replaced with an Ariakit-backed inline input element, the language list is UI-owned, and the floating toolbar supplies explicit editor/focus state.
- Upgrade `plate-common`, `plate-media`, `plate-link` and the remaining headless plugins. This removes both high-severity advisories affecting Plate 31.
- Protect JSON `rawBody` and legacy HTML loading plus JSON/HTML submission with tests before changing plugin construction.

## Checkpoint 2: plugin object API

- [x] Replace the removed global combobox store with the inline input element model introduced in combobox/emoji 34, then align both packages to 36.x.
- [ ] Upgrade the complete `@udecode/plate-*` family to 37.x in one change. The Slate family is already staged at the compatible migration boundary: `slate` 0.103.0, `slate-react` 0.110.3, `slate-history` 0.109.0 and `slate-hyperscript` 0.100.0.
- [ ] Replace `createXPlugin()` registrations with the React plugin objects exported from each package's `/react` entrypoint. Keep non-React transforms and serializers imported from the package root.
- [ ] Replace `createPlugins()` and its global component map with plugin composition using `.configure()`, `.extend()` and `.withComponent()`.
- [x] Replace the plugin registration's `ELEMENT_*` and `MARK_*` dependencies with app-owned persisted keys. The v36 values are locked by `plate-plugin-keys.test.ts` before the v37 code migration begins; do not change `p`, `h1`-`h6`, `blockquote`, `code_block`, `code_line`, `code_syntax`, `a`, `img`, `media_embed`, `ul`, `ol`, `li`, `action_item`, `emoji_input`, or the existing mark keys.
- [ ] Move React-only UI imports (`Plate`, `PlateContent`, element/leaf components, hooks, floating controls and combobox controls) to `/react`; retain headless helpers at root imports.
- [ ] Migrate paragraph support from the removed `@udecode/plate-paragraph` package to `ParagraphPlugin` from `@udecode/plate-common/react`. Do not add `@udecode/plate-basic-elements` merely to replace this one plugin.
- [ ] Update element/leaf props, placeholder composition, media/caption, link floating toolbar, code-block combobox and emoji input together with their owning plugins.
- [ ] Preserve all current plugin options: reset/break rules, valid alignment/indent/line-height node types, caption targets, selection behavior, trailing paragraph, tab handling and link toolbar rendering.
- [ ] Run the stored JSON/HTML round-trip tests after the registration migration, in addition to editor interaction and lazy-loading tests.

## Checkpoint 3: Plate 49 package layout

- [ ] Rename `@udecode/plate-*` packages to `@platejs/*`; replace `@udecode/plate-common` with the relevant `@platejs/core` and utility packages rather than keeping a compatibility barrel.
- [ ] Move basic marks/nodes to `@platejs/basic-nodes`, classic lists to `@platejs/list-classic`, and indent lists to `@platejs/list`.
- [ ] Replace deprecated `@udecode/plate-serializer-html` with `@platejs/serializer-html` and verify serialization uses the editor-bound plugin components.
- [ ] Move editor creation/provider setup to the v49 APIs, including plugin overrides and editable rendering.
- [ ] Upgrade the Slate package family as one unit to the exact versions required by Plate 49; remove any temporary v37 peer-version pins.
- [ ] Recheck package exports rather than rewriting `/react` imports mechanically: v49 keeps headless and React entrypoints separate.
- [ ] Compare the final plugin keys with the v36 compatibility fixture before accepting any serialized output.

Each checkpoint must pass the editor compatibility tests, the Home/Public/Feed lazy-loading tests, type checking, production build, frozen installation and peer checks.
