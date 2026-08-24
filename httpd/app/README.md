# ffdb frontend

React 19 + Vite. The Go server in `../` serves the templates and static
assets; this app only produces the JS/CSS bundle.

## Commands

```
pnpm install
pnpm run build # Vite build, verification and publish to ../static
pnpm dev       # vite build --watch (development mode, sourcemaps)
pnpm test      # vitest
```

## How it integrates

- Vite emits content-hashed entry, editor and CSS assets under
  `build/static/`, plus `build/static/manifest.json`. Go templates resolve
  asset URLs through that manifest.
- `scripts/publish-build.mjs` replaces generated files under `../static/`
  while preserving the hand-written `../static/css/style.css`. The Go binary
  embeds and serves the published directory in production.
- In Go debug mode (`httpd -d`) the templates instead serve the files under
  `build/static/` directly. `pnpm dev` keeps the development build updated.

Plate UI components follow the current registry structure documented in
`../AGENTS.md`. Use `https://platejs.org/r/<name>.json` as a reference and
preserve the local behavior and visual tokens instead of replacing files
blindly.
