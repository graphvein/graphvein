#!/usr/bin/env bash
# The Express path oracle of T028 (docs/contract/claims/express5-paths.md,
# rows EX5-W1..EX5-W14). Opt-in, never run by CI.
#
# 1. Installs Express 4.21.1 (path-to-regexp 0.1.10) and 5.1.0 (router
#    2.2.0, path-to-regexp 8.2.0 through npm overrides), all MIT, into the
#    scratch directory (--ignore-scripts), each in its own tools directory.
# 2. For each version, copies testdata/fixtures/express5-paths to scratch,
#    links that version's node_modules into the directory of every module
#    whose group's majors include it, serves the fixture
#    apps on 127.0.0.1 and records which route answered each request of
#    oracle.json; under Express 5 it also checks that each pattern of the
#    `throws` list makes Express throw (express_paths.mjs).
# 3. Runs TestExpressPathOracle, which compares the answers with the
#    stored path entries of every route.
#
# Usage: bash scripts/oracle/express_paths.sh --scratch DIR
# Writes only under --scratch. Needs node and npm on PATH. Bash 3.2
# compatible.
set -euo pipefail

die() { printf 'express_paths: %s\n' "$*" >&2; exit 1; }

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
fixture="$repo_root/testdata/fixtures/express5-paths"

outputs=""
for version in 4.21.1 5.1.0; do
  tools="$scratch/express-$version-pinned"
  if [ ! -d "$tools/node_modules/express" ]; then
    mkdir -p "$tools"
    # Pinned to the versions Express resolved at its release: 4.21.1 pins
    # path-to-regexp 0.1.10 itself; 5.1.0 gets router 2.2.0 and
    # path-to-regexp 8.2.0 (npm overrides).
    case "$version" in
      5.*) printf '{"private": true, "overrides": {"router": "2.2.0", "path-to-regexp": "8.2.0"}}\n' > "$tools/package.json" ;;
      *) printf '{"private": true}\n' > "$tools/package.json" ;;
    esac
    (cd "$tools" && npm install --ignore-scripts --no-audit --no-fund --save-exact "express@$version" >/dev/null)
  fi
  major=${version%%.*}
  work="$scratch/paths-$version"
  rm -rf "$work"
  mkdir -p "$work"
  cp -R "$fixture/." "$work/"
  # Link node_modules into the directory of every module run under this
  # major (oracle.json `groups[].majors`).
  for dir in $(node -e '
    const plan = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
    const path = require("path");
    const dirs = new Set(plan.groups.filter((g) => g.majors.includes(Number(process.argv[2])))
      .map((g) => path.posix.dirname(`${g.dir}/${g.module}`)));
    console.log([...dirs].join(" "));' "$fixture/oracle.json" "$major"); do
    ln -s "$tools/node_modules" "$work/$dir/node_modules"
  done
  node "$here/express_paths.mjs" "$work" "$scratch/paths-$version.json"
  outputs="$outputs${outputs:+,}$scratch/paths-$version.json"
done
(cd "$repo_root" && GRAPHVEIN_EXPRESS_PATH_ORACLE="$outputs" \
  go test ./internal/extract/routes -run '^TestExpressPathOracle$' -count=1 -v)
