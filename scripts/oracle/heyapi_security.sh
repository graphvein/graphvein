#!/usr/bin/env bash
# The hey-api security oracle of T026 (row DA-H1, AA4;
# docs/extract/auth.md "Oracle"). Opt-in, never run by CI.
#
# 1. Installs @hey-api/openapi-ts 0.99.0 (MIT) and typescript 6.0.3
#    (Apache-2.0) into the scratch directory (--ignore-scripts).
# 2. Generates the client from testdata/fixtures/auth-heyapi/openapi.json
#    with the fixture's openapi-ts.config.ts and checks that the committed
#    generated files equal the generator's output.
# 3. Reads the generated security arrays with the TypeScript compiler
#    (heyapi_security.mjs) and runs TestHeyAPISecurityOracle, which
#    compares them with the stored security attributes.
#
# Usage: bash scripts/oracle/heyapi_security.sh --scratch DIR
# Writes only under --scratch. Needs node and npm on PATH. Bash 3.2
# compatible.
set -euo pipefail

die() { printf 'heyapi_security: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] || die "--scratch is required"
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
fixture="$repo_root/testdata/fixtures/auth-heyapi"

if [ ! -d "$scratch/tools/node_modules/@hey-api/openapi-ts" ]; then
  mkdir -p "$scratch/tools"
  printf '{"private": true}\n' > "$scratch/tools/package.json"
  (cd "$scratch/tools" && npm install --ignore-scripts --no-audit --no-fund --save-exact \
    @hey-api/openapi-ts@0.99.0 typescript@6.0.3 >/dev/null)
fi
# Inside tools/, so the config resolves @hey-api/openapi-ts.
work="$scratch/tools/generate"
rm -rf "$work"
mkdir -p "$work"
cp "$fixture/openapi.json" "$fixture/openapi-ts.config.ts" "$work/"
(cd "$work" && node "$scratch/tools/node_modules/@hey-api/openapi-ts/bin/run.js" -f openapi-ts.config.ts >/dev/null)
for f in sdk.gen.ts client.gen.ts index.ts types.gen.ts; do
  cmp -s "$work/client/$f" "$fixture/client/$f" || die "client/$f differs from the generator's output"
done
node "$here/heyapi_security.mjs" "$scratch/tools/node_modules" "$work/client/sdk.gen.ts" "$scratch/heyapi-security.json"
(cd "$repo_root" && GRAPHVEIN_HEYAPI_ORACLE="$scratch/heyapi-security.json" \
  go test ./internal/extract/clients -run '^TestHeyAPISecurityOracle$' -count=1 -v)
