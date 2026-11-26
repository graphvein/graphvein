#!/usr/bin/env bash
# The Express and Next.js Pages Router API shape oracle (T029 SH-E,
# docs/contract/claims/route-shape-express.md). Opt-in, never run by CI.
#
# 1. Installs the pinned packages of tools/, express5/ and next/ into the
#    scratch directory (npm ci --ignore-scripts --omit=optional).
# 2. Copies testdata/fixtures/express-shape to scratch, links the Express 4
#    packages beside src/ and the Express 5 packages beside v5/, and
#    compiles src/ and v5/src/ with the pinned TypeScript (type errors do
#    not stop the emit: the fixture imports packages it never runs, such
#    as express-openapi-validator, which the oracle replaces).
# 3. Runs oracle.cjs against the stored shapes: the golden
#    testdata/fixtures/express-shape.golden.json, or --snapshot FILE (an
#    NDJSON stream of `graphvein index --emit` on the fixture).
#
# Usage: bash scripts/oracle/express_shape/express_shape.sh --scratch DIR [--snapshot FILE]
# Writes only under --scratch (result.json there). Needs node and npm on
# PATH. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'express_shape: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../../.." && pwd)
scratch=""
claims="$repo_root/testdata/fixtures/express-shape.golden.json"
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --snapshot) claims=$(cd "$(dirname "$2")" && pwd)/$(basename "$2"); shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] || die "--scratch is required"
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)

for set in tools express5 next; do
  dir="$scratch/$set"
  mkdir -p "$dir"
  if ! cmp -s "$here/$set/package-lock.json" "$dir/package-lock.json" || [ ! -d "$dir/node_modules" ]; then
    cp "$here/$set/package.json" "$here/$set/package-lock.json" "$dir/"
    (cd "$dir" && npm ci --ignore-scripts --omit=optional --no-audit --no-fund >/dev/null)
  fi
done

work="$scratch/work"
rm -rf "$work"
mkdir -p "$work"
cp -R "$repo_root/testdata/fixtures/express-shape/." "$work/"
ln -s "$scratch/tools/node_modules" "$work/node_modules"
ln -s "$scratch/express5/node_modules" "$work/v5/node_modules"
cat > "$work/tsconfig.oracle.json" <<'JSON'
{"compilerOptions": {"target": "ES2022", "module": "commonjs", "esModuleInterop": true, "skipLibCheck": true,
  "strict": false, "allowJs": true, "outDir": "out", "rootDir": ".", "noEmitOnError": false},
 "include": ["src", "v5/src"]}
JSON
(cd "$work" && node "$scratch/tools/node_modules/typescript/bin/tsc" -p tsconfig.oracle.json >"$scratch/tsc.log" 2>&1) || true
[ -f "$work/out/src/app.js" ] && [ -f "$work/out/v5/src/server.js" ] || die "compilation failed: see $scratch/tsc.log"
ln -s "$scratch/express5/node_modules" "$work/out/v5/node_modules"

NODE_ENV=test node "$here/oracle.cjs" --work "$work" --tools "$scratch/tools" --next "$scratch/next" \
  --claims "$claims" --cases "$here/cases.json" --out "$scratch/result.json"
