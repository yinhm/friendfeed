# AGENTS.md

## Conventions

- Keep JavaScript out of the HTML templates (`templates/*.html`): no inline
  `<script>` blocks and no inline event handlers. Page behavior belongs in the
  React app (`app/src/`), which pages load as an external bundle via
  `{% block scripts %}`. Server-rendered data bootstrapping (e.g. the
  `window.appData` assignment in `feed.html`) is the existing exception.

## Frontend rules (httpd/app)

### Verification gate (all must pass, run the whole set)

- `pnpm lint` (zero warnings), `pnpm run typecheck` (tsc; `typecheck:tsgo`
  optional), `CI=true pnpm test`, `pnpm run build` (runs the Tailwind/asset
  verifier), and `go test ./...` when Go or templates changed.
- `vite.config.js` test config excludes `e2e/**` — Playwright specs live
  there and must never be collected by Vitest; run them via
  `pnpm run test:e2e`.

### Lazy-loading invariant

The editor chunk must only load on pages with the editor (Home, entry edit).
Public/Feed/logged-out pages must not download it. Rendered entry content
uses `src/entry-body.jsx` (static component map), never the editor runtime —
check main-bundle size after any import change (~230 kB today).

### Asset pipeline

- Vite emits `bundle-[hash].min.js/css` + `static/manifest.json`; the Go
  server injects hashed URLs into templates. Never hardcode bundle URLs.
- Generated files are untracked: `static/js/*`, `static/css/*`,
  `static/manifest.json`. Build frontend before `go build`.
- `style.css` gets an automatic content fingerprint (`?v=md5[:8]`); do not
  hand-edit a version number.

### Imports & typing

- tsconfig has NO `baseUrl` (TS 5.9/TS7 dual compatible); bare
  `components/...` imports resolve via `paths` — keep them working.
- `cn`/`withProps`/`withCn`/`withVariants` come from local
  `components/cn`; `withRef`/`createPrimitiveElement` from `platejs/react`.
  No `@udecode/*` imports remain.
- New/changed JSX files should carry `// @ts-check` with JSDoc contracts.

### Entry content model (rawBody)

- Persisted node/mark key strings are sacred — they live in
  `components/plate-plugin-keys.ts` with literal lock tests; never change a
  value, only add.
- `entry-body.jsx` renders rawBody; malformed rawBody (client-supplied) must
  fall back to the server-sanitized HTML body (recursive validation + error
  boundary).
- Feed lists truncate at 300 chars of text + "Read more..." link; the entry
  page (`onpage`) renders full. Do not bypass this again.
- Sanitizing: feed bodies use `util.DefaultSanitize` (UGCPolicy, keeps
  a/img/ul/li) server-side; page titles use the strict strip-all sanitizer.

### E2E pattern (scripts/e2e)

Boot throwaway backend+web on RANDOM ports, seed via `ForceArchiveFeed`,
assert React-rendered (`[data-eid]`) content. Cleanup kills only the run's
own temp binaries (aborted runs must never leave listeners behind).
