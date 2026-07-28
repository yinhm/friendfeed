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
SESSION_KEY="e2e-session-key"
BACKEND_PID=""
WEB_PID=""

cleanup() {
  status=$?
  trap - EXIT
  if ((status != 0)); then
    echo "E2E backend log:" >&2
    sed 's/^/  /' "$TMP/backend.log" >&2 2>/dev/null || true
    echo "E2E web log:" >&2
    sed 's/^/  /' "$TMP/web.log" >&2 2>/dev/null || true
  fi
  [[ -z "$WEB_PID" ]] || kill "$WEB_PID" 2>/dev/null || true
  [[ -z "$BACKEND_PID" ]] || kill "$BACKEND_PID" 2>/dev/null || true
  [[ -z "$WEB_PID" ]] || wait "$WEB_PID" 2>/dev/null || true
  [[ -z "$BACKEND_PID" ]] || wait "$BACKEND_PID" 2>/dev/null || true
  rm -rf "$TMP"
  exit "$status"
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

# pnpm must run with cwd inside httpd/app: the repo root has no package.json,
# so corepack would fall back to the latest pnpm there, which hard-fails the
# version check against the packageManager pin in httpd/app/package.json.
(cd "$ROOT/httpd/app" && pnpm run build)
cd "$ROOT"
go build -o "$TMP/ffdb" .
go build -o "$TMP/ffweb" ./httpd
"$TMP/ffdb" -c "$TMP/config.json" >"$TMP/backend.log" 2>&1 &
BACKEND_PID=$!
"$TMP/ffweb" -rpc "$GRPC_ADDR" -p "$WEB_PORT" -s "$SESSION_KEY" -c "$TMP/config.json" >"$TMP/web.log" 2>&1 &
WEB_PID=$!

ready=0
for _ in $(seq 1 30); do
  if curl -fs "$E2E_BASE_URL/public" -o /dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if ((ready == 0)); then
  echo "E2E web server did not become ready within 30 seconds" >&2
  exit 1
fi

go run ./scripts/e2e/seed \
  -addr "$GRPC_ADDR" \
  -session-key "$SESSION_KEY" \
  -session-cookie-file "$TMP/session-cookie" \
  -rename-session-cookie-file "$TMP/rename-session-cookie"
export E2E_SESSION_COOKIE="$(<"$TMP/session-cookie")"
export E2E_RENAME_SESSION_COOKIE="$(<"$TMP/rename-session-cookie")"

cd "$ROOT/httpd/app"
pnpm exec playwright test "$@"
