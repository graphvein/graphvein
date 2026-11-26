#!/usr/bin/env bash
# The Diesel oracle of T018g4 (ADR 0062). Never run by CI.
#
# It builds scratch Postgres databases and dumps what each holds as JSON,
# one run per database:
#
#   corpus     the repository's own Diesel migrations, applied in
#              diesel_migrations' order (version = the directory name up to
#              the first `_` with every `-` removed, migrations_internals
#              2.3.0 lib.rs:71-73, sorted as strings, migration_harness.rs:128;
#              dot entries and directories without an up.sql skipped,
#              lib.rs:75-83), each up.sql in one transaction (psql -1) unless
#              its metadata.toml says run_in_transaction = false.
#   fixture    scripts/oracle/diesel_fixture.sql: the database the fixture's
#              generated Postgres schema files describe.
#   probe      scripts/oracle/diesel_probe.sql: forms neither the corpus nor
#              the fixture has; TestDieselOracle writes the schema files
#              print-schema would write from the run's information_schema
#              (an emulation of the cited rules, a test of the mapping table
#              only) and compares the claim pass over them.
#
# Each run holds the catalog (relations, columns with format_type, type
# name and schema, not null; p/u/f constraints with ordered columns) and,
# for the probe, the information_schema rows print-schema reads. Then
# compare:
#
#   GRAPHVEIN_DIESEL_ORACLE=<out.json> GRAPHVEIN_DIESEL_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/diesel -run TestDieselOracle -v
#
# Usage:
#   bash scripts/oracle/diesel_catalog.sh --pg URL --out FILE \
#     (--fixture | --corpus ROOT)
#
#   --fixture      the fixture and probe runs (ROOT for the test:
#                  testdata/fixtures/diesel-models).
#   --corpus ROOT  rust-lang/crates.io at its pin
#                  (4e6ac5a46387d41656746507e0b6d7b5d863cddf): ROOT is the
#                  checkout, also ROOT for the test.
#   --pg URL       a role that may create databases; each run uses a fresh
#                  database diesel_oracle_<run>, dropped at the end.
#
# Needs psql (Postgres 17). Writes only --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'diesel_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
pg="" out="" preset="" root=""
while [ $# -gt 0 ]; do
  case "$1" in
    --pg) pg=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --fixture) preset=fixture; shift ;;
    --corpus) preset=corpus; root=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$pg" ] && [ -n "$out" ] && [ -n "$preset" ] || die "--pg, --out and --fixture or --corpus are required"
out_dir=$(cd "$(dirname "$out")" && pwd)
out="$out_dir/$(basename "$out")"
export PGOPTIONS="-c client_min_messages=warning"

# dburl URL DB replaces the database of a postgres:// URL.
dburl() {
  local url=$1 db=$2 base query=""
  base=${url%%\?*}
  case "$url" in *\?*) query="?${url#*\?}" ;; esac
  printf '%s/%s%s' "${base%/*}" "$db" "$query"
}

# order DIR prints the migration directories of DIR in Diesel's order.
order() {
  local dir=$1 d name version tab
  tab=$(printf '\t')
  for d in "$dir"/*/; do
    [ -d "$d" ] || continue
    name=$(basename "$d")
    case "$name" in .*) continue ;; esac
    [ -f "$d/up.sql" ] || continue
    version=${name%%_*}
    printf '%s\t%s\n' "${version//-/}" "$name"
  done | LC_ALL=C sort -s -t "$tab" -k1,1 | cut -f2-
}

# create DB makes a fresh database.
create() {
  psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $1" -c "CREATE DATABASE $1" >/dev/null
}

applied=0
failed=0
failures=""
# apply URL DIR applies DIR's migrations to URL in Diesel's order.
apply() {
  local url=$1 dir=$2 m single err
  err=$(mktemp)
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    single=-1
    if [ -f "$dir/$m/metadata.toml" ] && grep -Eq '^[[:space:]]*run_in_transaction[[:space:]]*=[[:space:]]*false' "$dir/$m/metadata.toml"; then
      single=""
    fi
    if psql "$url" -X -q -v ON_ERROR_STOP=1 ${single:+"$single"} -f "$dir/$m/up.sql" >/dev/null 2>"$err"; then
      applied=$((applied + 1))
    else
      failed=$((failed + 1))
      failures="$failures$m: $(tr '\n' ' ' <"$err")
"
    fi
  done <<EOF
$(order "$dir")
EOF
  rm -f "$err"
}

runs=()
dbs=()
# dump NAME KIND REPO SETS URL appends a run: SETS is a comma-separated
# list of model groups the run compares.
dump() {
  local name=$1 kind=$2 repo=$3 sets=$4 url=$5 json
  json=$(psql "$url" -X -q -A -t -v ON_ERROR_STOP=1 \
    -v name="$name" -v kind="$kind" -v repo="$repo" -v sets="$sets" \
    -v applied="$applied" -v failed="$failed" -v failures="$failures" \
    -f "$here/diesel_catalog.sql") || die "run $name: the catalog dump failed"
  runs+=("$json")
}

case "$preset" in
  fixture)
    create diesel_oracle_fixture
    dbs+=(diesel_oracle_fixture)
    url=$(dburl "$pg" diesel_oracle_fixture)
    psql "$url" -X -q -v ON_ERROR_STOP=1 -f "$here/diesel_fixture.sql" >/dev/null || die "diesel_fixture.sql failed"
    dump fixture fixture . "pg/src/schema.rs,pg/src/audit.rs,pg/src/filtered.rs,pg/src/broken.rs,pg/src/nohead.rs,pg/src/v24.rs,pg/src/patched.rs,pg20/src/schema.rs" "$url"
    create diesel_oracle_probe
    dbs+=(diesel_oracle_probe)
    url=$(dburl "$pg" diesel_oracle_probe)
    psql "$url" -X -q -v ON_ERROR_STOP=1 -f "$here/diesel_probe.sql" >/dev/null || die "diesel_probe.sql failed"
    dump probe probe "" "" "$url"
    ;;
  corpus)
    [ -d "$root/migrations" ] || die "$root/migrations is not a directory"
    create diesel_oracle_crates_io
    dbs+=(diesel_oracle_crates_io)
    url=$(dburl "$pg" diesel_oracle_crates_io)
    apply "$url" "$root/migrations"
    printf 'diesel_catalog: crates.io: %d migrations applied, %d failed\n' "$applied" "$failed" >&2
    dump crates.io corpus . "crates/crates_io_database/src/schema.rs,crates/crates_io_worker/src/schema.rs" "$url"
    ;;
esac

{
  printf '{"runs": ['
  sep=""
  for r in ${runs[@]+"${runs[@]}"}; do
    printf '%s%s' "$sep" "$r"
    sep=","
  done
  printf ']}\n'
} >"$out"
for db in ${dbs[@]+"${dbs[@]}"}; do
  psql "$pg" -X -q -c "DROP DATABASE $db" >/dev/null
done
printf 'diesel_catalog: %d runs written to %s\n' "${#runs[@]}" "$out" >&2
