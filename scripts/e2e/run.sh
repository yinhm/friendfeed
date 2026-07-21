#!/usr/bin/env bash
# E2E smoke orchestration: boot backend + web on a throwaway database,
# seed deterministic entries through the archive RPC, run Playwright.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
GRPC_PORT=$((20000 + RANDOM % 20000))
WEB_PORT=$((46000 + RANDOM % 10000))
GRPC_ADDR="localhost:$GRPC_PORT"
export E2E_BASE_URL="http://localhost:$WEB_PORT"

cleanup() {
  # kill by exact temp-binary path so stale servers from aborted runs
  # cannot accumulate (and never touch system/production ffdb).
  pkill -f "^$TMP/ffdb" 2>/dev/null || true
  pkill -f "^$TMP/ffweb" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

cat > "$TMP/config.json" <<EOF
{
  "address": "$GRPC_ADDR",
  "db_path": "$TMP/db",
  "media_path": "$TMP/files",
  "gauth_key_file": "$TMP/gauth.json",
  "twitter_api_key": "",
  "twitter_api_secret": "",
  "twitter_api_callback": ""
}
EOF

# httpd refuses to boot without a gauth file; dummy credentials are fine
# because the smoke test never exercises OAuth.
cat > "$TMP/gauth.json" <<EOF
{"web":{"client_id":"e2e-dummy","client_secret":"e2e-dummy","redirect_uris":["$E2E_BASE_URL/auth/google/callback"]}}
EOF

cd "$ROOT"
go build -o "$TMP/ffdb" . && "$TMP/ffdb" -c "$TMP/config.json" >"$TMP/backend.log" 2>&1 &
go build -o "$TMP/ffweb" ./httpd && "$TMP/ffweb" -rpc "$GRPC_ADDR" -p "$WEB_PORT" -c "$TMP/config.json" >"$TMP/web.log" 2>&1 &

for _ in $(seq 1 30); do
  if curl -fs "$E2E_BASE_URL/public" -o /dev/null 2>&1; then break; fi
  sleep 1
done

go run ./scripts/e2e/seed -addr "$GRPC_ADDR"

cd "$ROOT/httpd/app"
pnpm exec playwright test "$@"
