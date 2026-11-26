#!/usr/bin/env bash
# The TypeORM oracle of T018g6 (ADR 0064). Never run by CI.
#
# It installs the pinned typeorm (each corpus's own pin: 0.3.22, 0.3.31,
# 1.1.2; the fixture's 0.3.31 and 1.1.3), reflect-metadata, pg, the SQLite
# drivers, typeorm-naming-strategies and typescript (transpile only, with
# decorator metadata; no type check) from npm into scratch kits, copies
# the directories each run needs into a scratch tree (applying the run's
# type substitutions to the copy only), and runs
# scripts/oracle/typeorm_catalog.cjs once per run: the run's entity modules
# load in a Node process with every module outside the run's allow list
# replaced by a permissive stub (no network, process or file system access
# from corpus code), TypeORM's own `DataSource.initialize()` with
# `synchronize` and `dropSchema` creates the tables in a fresh database
# (postgres: typeorm_oracle_<n> on the scratch Postgres 17 cluster of
# scripts/cloud-postgres.sh; sqlite: a scratch file), or applies the
# corpus's migration classes (seerr's drift check, TO-V2), and the catalog
# is dumped as JSON. Then compare:
#
#   GRAPHVEIN_TYPEORM_ORACLE=<out.json> GRAPHVEIN_TYPEORM_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/typeorm -run TestTypeORMOracle -v
#
# Usage:
#   bash scripts/oracle/typeorm_catalog.sh --scratch DIR --pg URL --out FILE \
#     --root ROOT --binaries DIR (--fixture | --corpus)
#
#   --fixture  the runs of testdata/fixtures/typeorm-models (ROOT is that
#        directory): every group that claims postgres or sqlite, sqlite
#        through sqlite3 and better-sqlite3 under 0.3.31 and through
#        better-sqlite3 under 1.1.3, the SnakeNamingStrategy package, and
#        one run per provable TO-P1 failure, which must fail.
#   --corpus   seerr, nest-admin, Maintainerr and aqualink-app at their
#        pins (ROOT holds the checkouts seerr/, nest-admin/, maintainerr/
#        and aqualink-app/, as scripts/cloud-corpus.sh clones them).
#   --pg URL   a role that may create databases; each postgres run uses a
#        fresh database typeorm_oracle_<n>, dropped at the end.
#   --binaries DIR  the native SQLite bindings, built or fetched before:
#        DIR/sqlite3-5.1.7/node_sqlite3.node and
#        DIR/better-sqlite3-11.10.0/better_sqlite3.node. The packages are
#        installed with --ignore-scripts, so no install script, download
#        or compiler runs here; the bindings are copied into the kits.
#        better-sqlite3 11.10.0 is used under every TypeORM version (1.x
#        asks for ^12 and Maintainerr pins 13.0.3, whose prebuilt binaries
#        could not be fetched; TypeORM writes the DDL).
#
# Only the ORM's own packages, the drivers and typescript are installed
# (npm --ignore-scripts), and no package script of a corpus runs. Needs
# node 22, npm and psql. Writes only under --scratch and --out. Bash 3.2
# compatible.
set -euo pipefail

die() { printf 'typeorm_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
scratch="" pg="" out="" root="" preset="" binaries=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --pg) pg=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --binaries) binaries=$2; shift 2 ;;
    --fixture) preset=fixture; shift ;;
    --corpus) preset=corpus; shift ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$out" ] && [ -n "$root" ] && [ -n "$preset" ] && [ -n "$pg" ] && [ -n "$binaries" ] ||
  die "--scratch, --pg, --out, --root, --binaries and --fixture or --corpus are required"
root=$(cd "$root" && pwd)
binaries=$(cd "$binaries" && pwd)
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
out_dir=$(cd "$(dirname "$out")" && pwd)
out="$out_dir/$(basename "$out")"
sqlite3_node="$binaries/sqlite3-5.1.7/node_sqlite3.node"
bs3_node="$binaries/better-sqlite3-11.10.0/better_sqlite3.node"
[ -f "$sqlite3_node" ] || die "$sqlite3_node is missing"
[ -f "$bs3_node" ] || die "$bs3_node is missing"

# kit NAME PACKAGE@VERSION...: one scratch install per TypeORM pin.
kit() {
  local name=$1
  shift
  local dir="$scratch/kits/$name"
  if [ ! -f "$dir/.done" ]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    printf '{"private": true}\n' >"$dir/package.json"
    (cd "$dir" && npm install --no-audit --no-fund --silent --ignore-scripts --legacy-peer-deps --save-exact "$@" >/dev/null) ||
      die "npm install failed for kit $name"
    if [ -d "$dir/node_modules/sqlite3" ]; then
      mkdir -p "$dir/node_modules/sqlite3/build/Release"
      cp "$sqlite3_node" "$dir/node_modules/sqlite3/build/Release/node_sqlite3.node"
    fi
    if [ -d "$dir/node_modules/better-sqlite3" ]; then
      mkdir -p "$dir/node_modules/better-sqlite3/build/Release"
      cp "$bs3_node" "$dir/node_modules/better-sqlite3/build/Release/better_sqlite3.node"
    fi
    touch "$dir/.done"
  fi
  local p
  for p in "$@"; do
    node -e 'const [d, p] = process.argv.slice(1); const i = p.lastIndexOf("@"); const got = require(d + "/node_modules/" + p.slice(0, i) + "/package.json").version; if (got !== p.slice(i + 1)) { console.error(p + ": " + got + " installed"); process.exit(1); }' "$dir" "$p" ||
      die "kit $name does not hold $p"
  done
}

common="reflect-metadata@0.2.2 pg@8.23.0 better-sqlite3@11.10.0"
# shellcheck disable=SC2086
kit t0322 typeorm@0.3.22 sqlite3@5.1.7 typescript@5.8.3 $common
# shellcheck disable=SC2086
kit t0331 typeorm@0.3.31 typeorm-naming-strategies@4.1.0 sqlite3@5.1.7 typescript@5.4.5 $common
# shellcheck disable=SC2086
kit t112 typeorm@1.1.2 typescript@6.0.3 $common
# shellcheck disable=SC2086
kit t113 typeorm@1.1.3 typescript@6.0.3 $common

helper() { node "$here/typeorm_catalog.cjs" "$@"; }

export PGOPTIONS="-c client_min_messages=warning"
records=()
dbs=()
# drop_dbs drops the run's scratch databases; the EXIT trap runs it on
# success and on every failure (die, set -e, an interrupt), so a failed
# run leaves no typeorm_oracle_<n> database behind.
drop_dbs() {
  local db
  for db in ${dbs[@]+"${dbs[@]}"}; do
    psql "$pg" -X -q -c "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 ||
      printf 'typeorm_catalog: could not drop %s\n' "$db" >&2
  done
  dbs=()
}
trap drop_dbs EXIT
trap 'exit 130' INT TERM
lines=$(helper specs "$preset" "$root" "$scratch" "$pg")
while IFS='|' read -r n dialect db spec; do
  [ -n "$n" ] || continue
  record="$(dirname "$spec")/record.json"
  if [ "$dialect" = postgres ]; then
    psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
    dbs+=("$db")
  fi
  printf 'typeorm_catalog: run %s (%s)\n' "$n" "$dialect" >&2
  # Each run in its own process: TypeORM's metadata storage is global.
  (cd "$(dirname "$spec")" && helper run "$spec" "$record") 2>"$(dirname "$spec")/run.log" ||
    { cat "$(dirname "$spec")/run.log" >&2; die "run $n failed"; }
  tail -n 1 "$(dirname "$spec")/run.log" >&2
  records+=("$record")
done <<EOF
$lines
EOF
[ ${#records[@]} -gt 0 ] || die "no runs"
helper merge "$out" "${records[@]}"
drop_dbs
printf 'typeorm_catalog: %d runs written to %s\n' "${#records[@]}" "$out" >&2
