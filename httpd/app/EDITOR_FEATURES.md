# Editor feature inventory

This inventory records why each Plate 31 plugin remains in the production editor. It prevents dependency cleanup from breaking stored `rawBody` values merely because a toolbar button is absent.

## User-facing and stored-content features

- Paragraphs, headings, blockquotes, lists, todo items, links, basic marks and highlights are reachable from the floating toolbar, autoformat rules, or keyboard shortcuts.
- Code blocks are reachable through autoformat and must continue to render existing code nodes.
- Emoji is activated by its combobox trigger even though it has no fixed toolbar button.
- Images, media embeds and captions have no current insert button, but remain registered so existing `image` and `media_embed` nodes deserialize, render and serialize without data loss.
- Alignment, indentation, line height and font properties have no current toolbar controls, but remain registered for stored node and leaf properties.
- Break, reset, selection, tabbable, trailing-block and node-id plugins provide editor behavior rather than visible controls.
- Juice and HTML serialization/deserialization are used when editing legacy HTML entries and when posting HTML alongside `rawBody`.

## Removed features

- The comments plugin and its disconnected UI component set were never registered by `plate-plugins.ts` and had no application entry point.
- DOCX serialization, Plate selection helpers, toggle and Plate's package-level UI tooling had no imports.
- The `@udecode/plate` aggregate package was replaced by `plate-common` plus the directly used HTML serializer. This avoids installing unrelated table, Markdown, DOCX, comments and suggestion packages.

Any future removal from the first section needs a stored-content fixture proving old `rawBody` nodes still round-trip safely.
