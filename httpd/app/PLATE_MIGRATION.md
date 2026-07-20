# Plate 31 to 49 migration

Plate 49 is not package-compatible with Plate 31. The migration is split into reviewable checkpoints so stored `rawBody` data and the existing editor UI remain testable throughout.

## Checkpoint 1: security-compatible 36.x core

- Upgrade the 31.x plugin family to the last compatible 36.x release where its current UI API remains available.
- Combobox, emoji, code-block and floating have moved to 36.x. The global combobox store was replaced with an Ariakit-backed inline input element, the language list is UI-owned, and the floating toolbar supplies explicit editor/focus state.
- Upgrade `plate-common`, `plate-media`, `plate-link` and the remaining headless plugins. This removes both high-severity advisories affecting Plate 31.
- Protect JSON `rawBody` and legacy HTML loading plus JSON/HTML submission with tests before changing plugin construction.

## Checkpoint 2: plugin object API

The verified per-package and per-file import map is recorded in `PLATE_37_API_MAP.md`.

- [x] Replace the removed global combobox store with the inline input element model introduced in combobox/emoji 34, then align both packages to 36.x.
- [x] Upgrade the complete `@udecode/plate-*` family to 37.x in one change. The Slate family remains at the compatible migration boundary: `slate` 0.103.0, `slate-react` 0.110.3, `slate-history` 0.109.0 and `slate-hyperscript` 0.100.0.
- [x] Replace `createXPlugin()` registrations with the React plugin objects exported from each package's `/react` entrypoint. Keep non-React transforms and serializers imported from the package root.
- [x] Replace `createPlugins()` and its global component map with plugin composition using `.configure()`, `.extend()` and `.withComponent()`.
- [x] Replace the plugin registration's `ELEMENT_*` and `MARK_*` dependencies with app-owned persisted keys. The v36 values are locked by `plate-plugin-keys.test.ts` before the v37 code migration begins; do not change `p`, `h1`-`h6`, `blockquote`, `code_block`, `code_line`, `code_syntax`, `a`, `img`, `media_embed`, `ul`, `ol`, `li`, `action_item`, `emoji_input`, or the existing mark keys.
- [x] Move React-only UI imports (`Plate`, `PlateContent`, element/leaf components, hooks, floating controls and combobox controls) to `/react`; retain headless helpers at root imports.
- [x] Remove the discontinued `@udecode/plate-paragraph` package. Its v36 configuration is temporarily preserved by the app-owned `paragraph-plugin.ts`; replace that factory with `ParagraphPlugin` from `@udecode/plate-common/react` during the v37 object migration. Do not add `@udecode/plate-basic-elements` merely to replace this one plugin.
- [x] Remove the unused `@udecode/plate-horizontal-rule` package after confirming that horizontal-rule nodes are not registered or rendered. Keep the `hr` key only for the existing autoformat compatibility path.
- [x] Cross the HTML serializer transition boundary with the Plate 37 migration. The published `@udecode/plate-serializer-html` 37 package has no usable distribution, so serialization now uses `@udecode/plate-html/react`; the stored JSON/HTML round-trip test protects its output.
- [x] Update element/leaf props, placeholder composition, media/caption, link floating toolbar, code-block combobox and emoji input together with their owning plugins.
- [x] Preserve all current plugin options during migration: reset/break rules, valid alignment/indent/line-height node types, caption targets, selection behavior, trailing paragraph, tab handling and link toolbar rendering are captured by `plate-plugins.test.ts` before object migration.
- [x] Run the stored JSON/HTML round-trip tests after the registration migration, in addition to editor interaction and lazy-loading tests.
- [x] Apply the Plate 38 security patch line as one family after the object migration. Plate core 38.0.6 removes GHSA-73rg-f94j-xvhx; retain the resolved plugin contracts and persisted keys from checkpoint 2.

## Checkpoint 3: Plate 49 package layout

- [x] Rename `@udecode/plate-*` packages to `@platejs/*`; replace `@udecode/plate-common` with `platejs` and the relevant focused packages rather than keeping a compatibility barrel.
- [x] Move basic marks/nodes to `@platejs/basic-nodes`, classic lists to `@platejs/list-classic`, and indent lists to `@platejs/list`.
- [x] Replace the retired Plate 38 HTML package with the async `serializeHtml` exported by `platejs`. Plate 49 does not publish `@platejs/serializer-html`; serialization uses a static editor and app-owned static components so interactive React hooks are never invoked during server rendering.
- [x] Move editor creation/provider setup to the v49 APIs, including plugin overrides and editable rendering.
- [x] Upgrade the Slate package family as one unit to the versions required by Plate 49; remove the temporary v37 peer-version pins.
- [x] Recheck package exports rather than rewriting `/react` imports mechanically: v49 keeps headless and React entrypoints separate.
- [x] Compare the final plugin keys with the v36 compatibility fixture before accepting any serialized output.

Plate 49 core now owns three behaviors that previously required separate plugins:
Shift+Enter invokes `insertSoftBreak`, Backspace first selects an adjacent void
image, and `NodeIdPlugin` is enabled by default outside tests. The contract test
locks these behaviors so removal of the old `SoftBreakPlugin`,
`SelectOnBackspacePlugin` and explicit `NodeIdPlugin` registrations is not
mistaken for a UX regression.

Each checkpoint must pass the editor compatibility tests, the Home/Public/Feed lazy-loading tests, type checking, production build, frozen installation and peer checks.
