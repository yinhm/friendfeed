# Editor feature inventory

This inventory records why each current plugin remains in the production editor. It prevents dependency cleanup from breaking stored `rawBody` values merely because a toolbar button is absent.

## User-facing and stored-content features

- Paragraphs, headings, blockquotes, lists, todo items, links, basic marks and highlights are reachable from the floating toolbar, input rules, or keyboard shortcuts.
- Code blocks are reachable through Markdown input rules and must continue to render existing code nodes.
- `MarkdownShortcutsPlugin` owns horizontal-rule fences and text substitutions that have no feature plugin. The old `@platejs/autoformat` compatibility package is inert and must not be restored.
- Blockquotes are stored as `blockquote > p`; legacy flat blockquotes remain a supported read format and normalize when edited.
- Emoji is activated by its combobox trigger even though it has no fixed toolbar button.
- Images, media embeds and captions have no current insert button, but remain registered so existing `image` and `media_embed` nodes deserialize, render and serialize without data loss.
- Alignment, indentation, line height and font properties have no current toolbar controls, but remain registered for stored node and leaf properties.
- Break, reset, selection, tabbable, trailing-block and node-id behavior is provided by the registered plugins and Plate core rather than visible controls.
- Juice and HTML serialization/deserialization are used when editing legacy HTML entries and when posting HTML alongside `rawBody`.

## Deliberately absent features

- The comments plugin and its disconnected UI component set were never registered by `plate-plugins.ts` and had no application entry point.
- DOCX serialization, Plate selection helpers, toggle and Plate's package-level UI tooling had no imports.
- Table, suggestion, DnD and AI plugins are not part of this editor. Do not add their registry components without a real consumer.

Any future removal from the first section needs a stored-content fixture proving old `rawBody` nodes still round-trip safely.
