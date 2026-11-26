#!/usr/bin/env bash
# The Express middleware oracle of T026 (row DA-E1, AA5;
# docs/extract/auth.md "Oracle"). Opt-in, never run by CI.
#
# 1. Installs Express 4.21.1 and 5.1.0 (both MIT) into the scratch
#    directory (--ignore-scripts), each in its own tools directory.
# 2. For each version, copies testdata/fixtures/auth-express to scratch,
#    links that version's node_modules, serves the fixture apps on
#    127.0.0.1 and records which middleware ran before each route
#    (express_middleware.mjs). libs.js and cjs.cjs, which import the auth
#    libraries, are not loaded.
# 3. Runs TestExpressMiddlewareOracle, which compares the traces with the
#    stored mounted_middleware attributes.
#
# Usage: bash scripts/oracle/express_middleware.sh --scratch DIR
# Writes only under --scratch. Needs node and npm on PATH. Bash 3.2
# compatible.
set -euo pipefail

die() { printf 'express_middleware: %s\n' "$*" >&2; exit 1; }

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
fixture="$repo_root/testdata/fixtures/auth-express"

outputs=""
for version in 4.21.1 5.1.0; do
  tools="$scratch/express-$version"
  if [ ! -d "$tools/node_modules/express" ]; then
    mkdir -p "$tools"
    printf '{"private": true}\n' > "$tools/package.json"
    (cd "$tools" && npm install --ignore-scripts --no-audit --no-fund --save-exact "express@$version" >/dev/null)
  fi
  work="$scratch/fixture-$version"
  rm -rf "$work"
  mkdir -p "$work"
  cp -R "$fixture/." "$work/"
  ln -s "$tools/node_modules" "$work/node_modules"
  node "$here/express_middleware.mjs" "$work" "$scratch/express-$version.json"
  outputs="$outputs${outputs:+,}$scratch/express-$version.json"
done
(cd "$repo_root" && GRAPHVEIN_EXPRESS_ORACLE="$outputs" \
  go test ./internal/extract/routes -run '^TestExpressMiddlewareOracle$' -count=1 -v)
