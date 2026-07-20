# Plate 37 API map

This inventory was verified against the installed Plate 37 declarations. It is the execution map for the 36-to-37 code migration; it is not a generic search-and-replace list.

## Import boundaries

- Import React components, hooks and React plugin objects from each package's `/react` entrypoint.
- Keep transforms, node utilities, serializers and Slate-only types at the package root.
- Split mixed imports instead of moving the complete declaration to `/react`. Important mixed files are `autoformat-plugin.ts`, `inline-combobox.tsx`, `media-popover.tsx`, `turn-into-dropdown-menu.tsx` and `editor.jsx`.
- `@udecode/plate-floating` and `@udecode/plate-resizable` do not expose a `/react` subpath in 37; their existing root imports remain root imports.

## Plugin registration replacements

`plate-plugins.ts` must replace every `createXPlugin()` call with the corresponding plugin object. React-capable objects come from `/react`; headless-only objects stay at root.

- Nodes: `ParagraphPlugin`, `HeadingPlugin`, `BlockquotePlugin`, `CodeBlockPlugin`, `LinkPlugin`, `ImagePlugin`, `MediaEmbedPlugin`, `CaptionPlugin`, `TodoListPlugin`.
- Marks: `BoldPlugin`, `ItalicPlugin`, `UnderlinePlugin`, `StrikethroughPlugin`, `CodePlugin`, `FontColorPlugin`, `FontBackgroundColorPlugin`, `FontSizePlugin`, `HighlightPlugin`.
- Styles: `AlignPlugin`, `IndentPlugin`, `IndentListPlugin`, `LineHeightPlugin`.
- Behavior: `AutoformatPlugin`, `EmojiPlugin`, `ExitBreakPlugin`, `NodeIdPlugin`, `ResetNodePlugin`, `SelectOnBackspacePlugin`, `SoftBreakPlugin`, `TabbablePlugin`, `TrailingBlockPlugin`, `JuicePlugin`.
- Replace `createPlugins(..., { components })` with `.configure()`, `.extend()` and `.withComponent()` while preserving the assertions in `plate-plugins.test.ts`.

## Removed and renamed APIs

- `Plate` no longer accepts `plugins` and `initialValue`; create the editor with `usePlateEditor`/`createPlateEditor` and pass it through the v37 provider API.
- `PlatePlugin` and `PlateEditor` React types move to `@udecode/plate-common/react`; headless equivalents are named `SlatePlugin` and `SlateEditor`.
- The temporary v36 `createPluginFactory` paragraph implementation must become `ParagraphPlugin` from `@udecode/plate-common/react`.
- `ELEMENT_*`, `MARK_*`, `KEYS_HEADING` and `KEY_LIST_STYLE_TYPE` exports are removed. Production code already uses `plate-plugin-keys.ts`; the Plate 36 comparison imports in `plate-plugin-keys.test.ts` must be removed only after their literal-value assertions remain intact.
- `AutoformatPlugin` is a plugin value, not the old options type; type `autoformatPlugin` from the new plugin config/options shape.
- `usePlateId` is replaced by `useEditorId`.
- v37 media action APIs take a plugin object rather than `{ pluginKey }`.

## File groups

1. Migrate `plate-plugins.ts`, `paragraph-plugin.ts`, `plate-editor.tsx` and `autoformat-plugin.ts` together.
2. Move common element/leaf/editor hooks to `@udecode/plate-common/react`.
3. Move code-block, caption, link, list, media, emoji and combobox React hooks to their `/react` entrypoints while retaining their transforms at root.
4. Update `editor.jsx` temporary-editor creation and HTML serialization last, then run the raw JSON/HTML round-trip tests.

Do not accept the migration until the plugin contract, stored-key, editor interaction and lazy-loading tests all pass.
