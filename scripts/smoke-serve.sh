#!/usr/bin/env bash
# Smoke test for graphvein serve: index a fixture copy, serve it on a free
# loopback port, check the status, API section, schema and source endpoints, the page, the security headers
# and the Host and Origin checks, then stop the server. Runs in Git Bash and bash 3.2.
# Usage: bash scripts/smoke-serve.sh [path/to/graphvein]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
bin=${1:-./bin/graphvein$(go env GOEXE)}
case $bin in
  /*) ;;
  *) bin="$PWD/$bin" ;;
esac
tmp=$(mktemp -d)
pid=""
cleanup() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT
fail() {
  printf 'smoke-serve: %s\n' "$1" >&2
  if [ -f "$tmp/stderr" ]; then
    cat "$tmp/stderr" >&2
  fi
  exit 1
}

# A small fixture without inert ignore-file names, so a plain copy indexes
# exactly as committed.
cp -R testdata/fixtures/env-wrappers "$tmp/repo"
export GRAPHVEIN_POSTGRES_URL=""
export GRAPHVEIN_SQLITE_PATH="$tmp/index.db"
# serve registers its index in the workspace registry (ADR 0087): keep the
# runner's own registry untouched.
export GRAPHVEIN_WORKSPACE_FILE="$tmp/workspace.yaml"
"$bin" index "$tmp/repo" --repository smoke >/dev/null

"$bin" serve "$tmp/repo" --repository smoke --addr 127.0.0.1:0 >"$tmp/stdout" 2>"$tmp/stderr" &
pid=$!
url=""
tries=0
while [ -z "$url" ]; do
  tries=$((tries + 1))
  if [ "$tries" -gt 150 ]; then
    fail "server did not print its URL within 30 s"
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    fail "server exited early"
  fi
  sleep 0.2
  url=$(sed -n 's/^Serving smoke at \(http:[^ ]*\)$/\1/p' "$tmp/stdout" | tr -d '\r')
done
printf 'serving at %s\n' "$url"

curl -fsS -D "$tmp/headers" -o "$tmp/status" "${url}api/v0/status" || fail "status request failed"
grep -q '"state":"fresh"' "$tmp/status" || fail "status is not fresh: $(cat "$tmp/status")"
grep -qi "^content-security-policy: default-src 'self'" "$tmp/headers" || fail "status response lacks the CSP header"
grep -qi '^x-content-type-options: nosniff' "$tmp/headers" || fail "status response lacks nosniff"

curl -fsS -o "$tmp/endpoints" "${url}api/v0/api/endpoints" || fail "endpoints request failed"
grep -q '"api_version":0' "$tmp/endpoints" || fail "endpoints body lacks api_version 0: $(cat "$tmp/endpoints")"

# Schema endpoints (ADR 0048): the fixture has no sqlx migration set.
curl -fsS -o "$tmp/schema-sets" "${url}api/v0/schema/sets" || fail "schema sets request failed"
grep -q '"api_version":0' "$tmp/schema-sets" || fail "schema sets body lacks api_version 0: $(cat "$tmp/schema-sets")"
curl -fsS -o "$tmp/schema-summary" "${url}api/v0/schema/summary" || fail "schema summary request failed"
grep -q '"sets":0' "$tmp/schema-summary" || fail "schema summary does not count zero sets: $(cat "$tmp/schema-summary")"

# The source endpoint answers for the first listed route of the unchanged
# fixture with an exact highlight.
route=$(sed -n 's/.*"id":"\(route:[^"]*\)".*/\1/p' "$tmp/endpoints" | head -n 1)
[ -n "$route" ] || fail "endpoints body lists no route: $(cat "$tmp/endpoints")"
curl -fsS -G --data-urlencode "id=$route" -o "$tmp/source" "${url}api/v0/source" || fail "source request failed for $route"
grep -q '"state":"exact"' "$tmp/source" || fail "source state is not exact: $(cat "$tmp/source")"
curl -fsS -G --data-urlencode "id=$route" -o "$tmp/flow" "${url}api/v0/api/flow" || fail "flow request failed for $route"
grep -q '"api_version":0' "$tmp/flow" || fail "flow body lacks api_version 0: $(cat "$tmp/flow")"

# The editor presets, and a link built for the editor a browser chose.
curl -fsS -o "$tmp/ide" "${url}api/v0/ide" || fail "ide request failed"
grep -q '"editor":"zed"' "$tmp/ide" || fail "ide body lacks the zed preset: $(cat "$tmp/ide")"
curl -fsS -G --data-urlencode "id=$route" --data-urlencode "editor=zed" -o "$tmp/source-zed" "${url}api/v0/source" || fail "source request with editor=zed failed"
grep -q '"link":"zed://file' "$tmp/source-zed" || fail "editor=zed did not build a zed link: $(cat "$tmp/source-zed")"
grep -q '"source":"request"' "$tmp/source-zed" || fail "editor=zed response lacks ide.source request: $(cat "$tmp/source-zed")"

curl -fsS -D "$tmp/page-headers" -o "$tmp/page" "$url" || fail "page request failed"
grep -q 'Graphvein' "$tmp/page" || fail "page does not mention Graphvein"
grep -qi "^content-security-policy: default-src 'self'" "$tmp/page-headers" || fail "page lacks the CSP header"

port=${url##*:}
port=${port%/}
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Host: evil.example:$port" "${url}api/v0/status") || fail "foreign Host request failed"
[ "$code" = "403" ] || fail "foreign Host answered $code, want 403"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Origin: http://evil.example" "${url}api/v0/status") || fail "foreign Origin request failed"
[ "$code" = "403" ] || fail "foreign Origin answered $code, want 403"

printf 'smoke-serve: ok\n'
