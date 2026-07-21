# AGENTS.md

## Project layout

- `server/` — gRPC backend (pebble store, feed index, job queue, stock API).
- `httpd/` — Go web server (gin + pongo2 templates, embeds `static/`).
- `httpd/app/` — React frontend (Vite + Plate editor + Tailwind), see
  `httpd/app/AGENTS.md` for frontend rules.
- `model/`, `store/`, `pb/`, `search/`, `media/`, `util/` — shared packages.
- `cli/` — command-line tools; `twitter/` — Python crawlers (twikit).

## Verification gates

Run the FULL gate after every change, not only the tests covering what you
just touched — regressions hide in unrelated suites (e.g. adding files under
a new directory once broke Vitest collection, typecheck and lint at once).

- Go: `go build ./... && go vet ./... && go test ./...`
- Frontend (in `httpd/app`): `pnpm lint` (zero warnings allowed),
  `pnpm run typecheck`, `CI=true pnpm test`, `pnpm run build`
  (includes the Tailwind/asset verifier), `git diff --check`
- E2E: `pnpm run test:e2e` from `httpd/app` (spins up real servers).

## Build & deploy order

Frontend first, Go second: `pnpm run build` (emits content-hashed bundles and
`static/manifest.json`) must complete before `go build` (embeds `static/`).

Never commit generated artifacts: `httpd/static/js/*`, `httpd/static/css/*`,
`httpd/static/manifest.json`. Templates resolve hashed asset URLs from the
manifest at startup; a stale tracked manifest is worse than none.

## Change discipline

- Behavior tests first, then convert. To migrate a component, write tests
  against the CURRENT implementation, migrate, and require the same tests to
  pass unchanged (worked well for EntryCommentForm; skipping this caused
  regressions elsewhere).
- For persisted formats (DB keys, rawBody node types), lock the exact values
  with characterization tests BEFORE changing producers/consumers.
- For large mechanical moves, prove equivalence mechanically (extract old/new
  code and diff, compare bytes/hashes) instead of eyeballing.
- Dead-code removal: unexported + zero references repo-wide (grep incl.
  tests) is safe to delete; exported API is NOT deleted merely for zero
  in-repo callers — decide per API boundary and record the decision.
- Verify assumptions empirically before claiming them (library behavior,
  registry versions, runtime facts); say plainly when something was not run.

## Testing practices

- No fixed ports or shared temp dirs in tests: bind `127.0.0.1:0` or random
  high ports; zombie listeners from killed runs poison later runs (observed:
  repeated seeds accumulating in a stale backend). Orchestrators must kill
  only their own temp binaries, never pattern-match system processes.
- Assert user-visible behavior (DOM, wire output) over implementation detail.
- Check bundle-size impact of dependency/import changes immediately; a single
  import can pull an editor-sized tree into the main bundle.
