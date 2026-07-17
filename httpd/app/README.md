# ffdb frontend

React 19 + Vite. The Go server in `../` serves the templates and static
assets; this app only produces the JS/CSS bundle.

## Commands

```
pnpm install
pnpm build     # vite build + publish to ../static (bundle.min.js/css)
pnpm dev       # vite build --watch (development mode, sourcemaps)
pnpm test      # vitest
```

## How it integrates

- `vite build` emits `build/static/js/index.js` and `build/static/css/index.css`
  (single self-contained ESM bundle; the Go templates load it with
  `<script type="module">`).
- `scripts/publish-build.mjs` copies `build/static/*` into `../static/` and
  renames the entry to `../static/js/bundle.min.js` and
  `../static/css/bundle.min.css`, which the Go binary embeds and serves in
  production.
- In Go debug mode (`httpd -d`) the templates instead serve the files under
  `build/static/` directly. `air` (see `.air.toml`) reruns `yarn build` on
  file changes.

For plate UI components:

```
pnpx @udecode/plate-ui@latest init
```
