#!/usr/bin/env bash
# The Sequelize oracle of T018g3 (ADR 0061). Never run by CI.
#
# It installs the pinned sequelize, pg and sqlite3 (and typescript, to
# transpile TypeScript modules to CommonJS; no type check) from npm into
# scratch, copies the directories each run needs into a scratch tree, and
# runs scripts/oracle/sequelize_catalog.cjs once per run: the run's
# modules load in a Node process with every module outside the run's
# allow list replaced by a permissive stub (no network, process or file
# system access from corpus code), Sequelize's own `sync({force: true})`
# creates the tables in a fresh database (postgres: sequelize_oracle_<n> on
# the scratch Postgres 17 cluster of scripts/cloud-postgres.sh; sqlite: a
# scratch file), replayed model by model when it fails, and the catalog is
# dumped as JSON. Then compare:
#
#   GRAPHVEIN_SEQUELIZE_ORACLE=<out.json> GRAPHVEIN_SEQUELIZE_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/sequelize -run TestSequelizeOracle -v
#
# Usage:
#   bash scripts/oracle/sequelize_catalog.sh --scratch DIR --pg URL --out FILE \
#     --root ROOT (--fixture | --corpus)
#
#   --fixture  the runs of testdata/fixtures/sequelize-models (ROOT is that
#        directory): every package but decorators/ (sequelize-typescript,
#        not installed; nothing claimed), legacy/ (sequelize 5; nothing
#        claimed), mapped/ (mysql and mssql: facts mapped, SQ-V6; no
#        driver is installed) and pghooks/ (instance hooks: nothing
#        claimed).
#   --corpus   opencollective-api and Gladys at their pins (ROOT holds the
#        checkouts opencollective-api/ and gladys/).
#   --pg URL   a role that may create databases; each postgres run uses a
#        fresh database sequelize_oracle_<n>, dropped at the end.
#
# Only the ORM's own packages are installed (npm --ignore-scripts); the
# sqlite3 binding is fetched by its own prebuild-install step, never built,
# and no package script of a corpus runs. Needs node 22.13+ (node:sqlite),
# npm and psql. Writes only under --scratch and --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'sequelize_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
scratch="" pg="" out="" root="" preset=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --pg) pg=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --fixture) preset=fixture; shift ;;
    --corpus) preset=corpus; shift ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$out" ] && [ -n "$root" ] && [ -n "$preset" ] || die "--scratch, --out, --root and --fixture or --corpus are required"
root=$(cd "$root" && pwd)
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
out_dir=$(cd "$(dirname "$out")" && pwd)
out="$out_dir/$(basename "$out")"

# The pins of both corpora's lockfiles (sequelize 6.37.8, pg 8.23.1,
# sqlite3 5.1.7); typescript transpiles only.
kit="$scratch/kit"
if [ ! -f "$kit/node_modules/sqlite3/build/Release/node_sqlite3.node" ]; then
  mkdir -p "$kit"
  printf '{"private": true}\n' >"$kit/package.json"
  (cd "$kit" && npm install --no-audit --no-fund --silent --ignore-scripts --save-exact \
    sequelize@6.37.8 pg@8.23.1 sqlite3@5.1.7 typescript@5.9.3 >/dev/null) || die "npm install failed"
  (cd "$kit/node_modules/sqlite3" && ../.bin/prebuild-install -r napi >/dev/null 2>&1) ||
    die "the sqlite3 prebuilt binding could not be fetched"
fi
node -e 'const d = process.argv[1]; for (const [p, v] of [["sequelize", "6.37.8"], ["pg", "8.23.1"], ["sqlite3", "5.1.7"], ["typescript", "5.9.3"]]) { const got = require(d + "/node_modules/" + p + "/package.json").version; if (got !== v) { console.error(p + " " + got + " installed, " + v + " wanted"); process.exit(1); } }' "$kit" ||
  die "the scratch kit does not hold the pins"

helper() { node "$here/sequelize_catalog.cjs" "$@"; }

export PGOPTIONS="-c client_min_messages=warning"
records=()
dbs=()
lines=$(helper specs "$preset" "$root" "$scratch" "$kit" "${pg:-postgres://localhost/postgres}")
while IFS='|' read -r n dialect spec; do
  [ -n "$n" ] || continue
  record="$(dirname "$spec")/record.json"
  if [ "$dialect" = postgres ]; then
    [ -n "$pg" ] || die "run $n: --pg is required for postgres"
    db="sequelize_oracle_$n"
    psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
    dbs+=("$db")
  fi
  printf 'sequelize_catalog: run %s (%s)\n' "$n" "$dialect" >&2
  helper run "$spec" "$record" 2>"$(dirname "$spec")/run.log" || { cat "$(dirname "$spec")/run.log" >&2; die "run $n failed"; }
  records+=("$record")
done <<EOF
$lines
EOF
[ ${#records[@]} -gt 0 ] || die "no runs"
helper merge "$out" "${records[@]}"
for db in ${dbs[@]+"${dbs[@]}"}; do
  psql "$pg" -X -q -c "DROP DATABASE $db" >/dev/null
done
printf 'sequelize_catalog: %d runs written to %s\n' "${#records[@]}" "$out" >&2
