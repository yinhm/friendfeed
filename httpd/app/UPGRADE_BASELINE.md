# Frontend Upgrade Baseline

Captured on 2026-07-20 before the staged frontend upgrade.

## Runtime and package manager

- Node: 24.18.0
- Corepack: 0.35.0
- pnpm: 8.15.9
- Direct dependencies: 72 runtime, 9 development
- React / React DOM: 19.2.7
- Vite: 7.3.6
- Vitest: 3.2.7
- Plate packages: 31.0.0
- Tailwind CSS: 3.4.1
- TypeScript: 5.4.3

## Production bundle

| Asset | Bytes | Gzip bytes |
| --- | ---: | ---: |
| `static/js/bundle.min.js` | 210,152 | 65,321 |
| `static/js/editor-yhGdv_zq.js` | 1,887,350 | 545,292 |
| `static/css/bundle.min.css` | 57,387 | 11,898 |

The editor is a lazy chunk. It must remain absent from Public, Feed, and logged-out page loads.

## Audit snapshot

`pnpm audit --json` reported:

- Critical: 0
- High: 16
- Moderate: 12
- Low: 2

The vulnerable dependency paths are concentrated in the Plate 31 and Tailwind 3 dependency trees. Notable packages include `@udecode/plate-media`, `@udecode/plate-core`, `lodash`, `lodash.template`, `js-yaml`, `prismjs`, `braces`, `micromatch`, `minimatch`, `sucrase`, and `js-video-url-parser`.

Do not suppress these findings with broad overrides. Upgrade their owning direct dependencies, and rerun the audit after every stage.

## Major-version backlog

The 2026-07-20 registry snapshot showed these major migrations:

- Plate 31 → 49, together with the matching Slate packages
- Vite 7 → 8
- Vitest 3 → 4
- `@vitejs/plugin-react` 4 → 6
- jsdom 26 → 29
- Tailwind CSS 3 → 4
- TypeScript 5 → 7
- pnpm 8 → 11

These are intentionally split into separate stages in the repository `TODO.md`.

## Editor behavior baseline

- Home with `show_share=true` lazy-loads the editor, renders a content-editable input, and shows the publish control.
- Public and Feed use `show_share=false` and do not render the editor.
- The active Home editor has no image-upload control. Image plugins exist in the source tree, but no upload entry is wired into the rendered floating toolbar. This must be treated as a product decision, not silently assumed by upgrade tests.
