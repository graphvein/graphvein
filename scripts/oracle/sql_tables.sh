#!/usr/bin/env bash
# The SQL tables oracle of T018h (ADR 0060, docs/extract/table-access.md
# "Oracle"). Never run by CI.
#
# 1. TestSQLTablesOracle (phase requests) links ROOT with the code-to-table
#    pass and writes, under --scratch, each migration set that has a claimed
#    SQL site: its migration files in sqlx order and each claimed site's SQL.
# 2. For each set this script creates a fresh database on a scratch
#    Postgres 17 cluster (scripts/cloud-postgres.sh), applies the migrations
#    in order (each file in one transaction, as sqlx does), dumps pg_class,
#    and runs every site as
#      SET debug_print_parse = on; SET client_min_messages = log; PREPARE p AS <sql>
#    keeping psql's log and exit status. The database is dropped.
# 3. TestSQLTablesOracle (phase compare) reads the analyzer's permission
#    entries and compares them with the claimed (reads, writes) per site;
#    any disagreement fails it.
#
# Usage:
#   bash scripts/oracle/sql_tables.sh --scratch DIR --pg URL --root ROOT [--out FILE] [--model-ddl FILE]
#
#   --root ROOT  the repository to link (a fixture or a scratch copy of a
#                corpus; nothing is written into it)
#   --pg URL     a role that may create databases
#   --out FILE   the comparison's counts as JSON
#   --model-ddl FILE  the SQLModel.metadata DDL (sql_tables_sqlalchemy.sh
#                writes it): the schema of the SQLModel group's CT-P5 sites
#   --prisma-version V  the Prisma CLI for a Prisma group without pins
#   --sql-only   compare SQL text sites only, no toSQL builder (a corpus:
#                the toSQL harness evaluates builder text, a fixture oracle)
#   --drizzle-extra PKG@V  a package a Drizzle group's schema modules import
#                at load, installed next to drizzle-kit (repeatable; kaneo:
#                @paralleldrive/cuid2@3.3.0)
#
# A Prisma group with a claimed CT-R3 site has no migrations: its schema is
# the SQL `prisma migrate diff --from-empty` prints for the group's schema,
# with the group's pinned CLI installed from npm into --scratch (no
# database connection).
#
# A Drizzle group with a claimed site has no migrations either: its schema
# is the SQL the group's pinned drizzle-kit `export` prints (no database),
# from a scratch copy of its schema modules (drizzle_catalog.mjs prepare,
# as scripts/oracle/drizzle_catalog.sh runs it). A claimed CT-Z1/CT-Z2
# builder's SQL is its toSQL() output, which sql_tables_drizzle.mjs
# writes by evaluating the builder text against drizzle-orm's pg-proxy
# database at the same pin (Revision 4); nothing of the repository runs
# except the schema modules drizzle-kit itself loads.
#
# Writes only under --scratch and --out. Needs psql and go (and node 24 and
# npm for Prisma and Drizzle groups). Bash 3.2 compatible.
set -euo pipefail

die() { printf 'sql_tables: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch="" pg="" root="" out="" model_ddl="" prisma_version=""
drizzle_extra=()
sql_only=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --pg) pg=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --model-ddl) model_ddl=$2; shift 2 ;;
    --prisma-version) prisma_version=$2; shift 2 ;;
    --drizzle-extra) drizzle_extra+=("$2"); shift 2 ;;
    --sql-only) sql_only=1; shift ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$pg" ] && [ -n "$root" ] || die "--scratch, --pg and --root are required"
command -v psql >/dev/null || die "psql not found"
root=$(cd "$root" && pwd)
if [ -n "$model_ddl" ]; then
  model_ddl="$(cd "$(dirname "$model_ddl")" && pwd)/$(basename "$model_ddl")"
fi
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
exchange="$scratch/exchange"
rm -rf "$exchange"
mkdir -p "$exchange"
if [ -n "$out" ]; then
  out_dir=$(cd "$(dirname "$out")" && pwd)
  out="$out_dir/$(basename "$out")"
fi

run_test() {
  (cd "$repo_root" && GRAPHVEIN_SQL_TABLES_ORACLE_PHASE=$1 GRAPHVEIN_SQL_TABLES_ORACLE_ROOT="$root" \
    GRAPHVEIN_SQL_TABLES_ORACLE_DIR="$exchange" GRAPHVEIN_SQL_TABLES_ORACLE_OUT="$out" \
    GRAPHVEIN_SQL_TABLES_ORACLE_MODEL_DDL="$model_ddl" GRAPHVEIN_SQL_TABLES_ORACLE_SQL_ONLY="$sql_only" \
    go test ./internal/extract/tableaccess -run '^TestSQLTablesOracle$' -count=1 -v)
}

run_test requests >"$scratch/requests.log" 2>&1 || { cat "$scratch/requests.log" >&2; die "requests phase failed"; }

# dburl replaces the database name of the --pg URL.
dburl() {
  local base=${pg%%\?*} query=""
  case "$pg" in *\?*) query="?${pg#*\?}" ;; esac
  printf '%s/%s%s' "${base%/*}" "$1" "$query"
}

# prisma_cli VERSION: the Prisma CLI in scratch.
prisma_cli() {
  local dir="$scratch/prisma-cli-$1"
  if [ ! -x "$dir/node_modules/.bin/prisma" ]; then
    mkdir -p "$dir"
    printf '{"private": true}\n' >"$dir/package.json"
    (cd "$dir" && npm install --no-audit --no-fund --silent "prisma@$1" >/dev/null) || die "npm install prisma@$1 failed"
  fi
  printf '%s\n' "$dir/node_modules/.bin/prisma"
}

# prisma_ddl SET_DIR: the SQL Migrate would run for the set's schema.
prisma_ddl() {
  local set_dir=$1 schema version major cli work target
  schema=$(cat "$set_dir/prisma.schema")
  version=$(cat "$set_dir/prisma.version")
  [ -n "$version" ] || version=$prisma_version
  [ -n "$version" ] || die "$(cat "$set_dir/name"): no pinned Prisma version; pass --prisma-version"
  major=${version%%.*}
  cli=$(prisma_cli "$version")
  work="$set_dir/prisma"
  rm -rf "$work"
  mkdir -p "$work"
  target="$work/schema.prisma"
  if [ -d "$schema" ]; then
    target="$work/schema"
    cp -R "$schema" "$target"
  else
    cp "$schema" "$target"
  fi
  if [ "$major" -ge 7 ]; then
    # Prisma 7 starts the schema engine only with a datasource URL in a
    # config file, even from empty; nothing connects to it.
    printf 'export default { datasource: { url: "postgresql://oracle@127.0.0.1:1/oracle" } };\n' >"$work/prisma.config.ts"
    (cd "$work" && "$cli" migrate diff --from-empty --to-schema "$target" --script -o "$set_dir/prisma.sql" >"$work/diff.log" 2>&1) ||
      { cat "$work/diff.log" >&2; die "prisma migrate diff failed for $schema"; }
  else
    (cd "$work" && DATABASE_URL=postgresql://oracle@127.0.0.1:1/oracle PRISMA_HIDE_UPDATE_MESSAGE=1 "$cli" migrate diff --from-empty --to-schema-datamodel "$target" --script >"$set_dir/prisma.sql" 2>"$work/diff.log") ||
      { cat "$work/diff.log" >&2; die "prisma migrate diff failed for $schema"; }
  fi
}

# drizzle_kit KIT ORM: drizzle-kit, drizzle-orm and the extras in scratch,
# exact versions, no install scripts.
drizzle_kit() {
  local dir="$scratch/drizzle-kit-$1-orm-$2"
  if [ ! -x "$dir/node_modules/.bin/drizzle-kit" ]; then
    mkdir -p "$dir"
    printf '{"private": true}\n' >"$dir/package.json"
    (cd "$dir" && npm install --no-audit --no-fund --silent --ignore-scripts --save-exact "drizzle-kit@$1" "drizzle-orm@$2" ${drizzle_extra[@]+"${drizzle_extra[@]}"} >/dev/null) ||
      die "npm install drizzle-kit@$1 drizzle-orm@$2 failed"
  fi
  printf '%s\n' "$dir"
}

# drizzle_ddl SET_DIR: the group's drizzle-kit export SQL, and each
# toSQL request's SQL.
drizzle_ddl() {
  local set_dir=$1 repo pkg config schemas dialect casing kit orm dir work
  IFS='|' read -r repo pkg config schemas dialect casing kit orm <"$set_dir/drizzle.run"
  [ -n "$kit" ] && [ -n "$orm" ] || die "$(cat "$set_dir/name"): no pinned drizzle-kit and drizzle-orm"
  dir=$(drizzle_kit "$kit" "$orm")
  work="$dir/run-$(basename "$set_dir")"
  rm -rf "$work"
  mkdir -p "$work"
  node "$here/drizzle_catalog.mjs" prepare "$root/$repo" "$pkg" "$schemas" "$dialect" "$casing" "" "" "$work/tree" >"$work/modules.txt" ||
    die "preparing the schema modules of $config failed"
  (cd "$work/tree/$pkg" && "$dir/node_modules/.bin/drizzle-kit" export --config "$work/tree/$pkg/oracle.config.ts" >"$set_dir/drizzle.sql" 2>"$work/kit.log") ||
    { cat "$work/kit.log" >&2; die "drizzle-kit export failed for $config"; }
  [ ! -s "$work/kit.log" ] || { cat "$work/kit.log" >&2; die "drizzle-kit export wrote to stderr for $config"; }
  grep -q 'CREATE TABLE' "$set_dir/drizzle.sql" || die "drizzle-kit export printed no CREATE TABLE for $config"
  set -- "$set_dir"/site-*.request.json
  if [ -f "$1" ]; then
    cp "$here/sql_tables_drizzle.mjs" "$dir/sql_tables_drizzle.mjs"
    node "$dir/sql_tables_drizzle.mjs" "$work/tree" "$@" || die "the toSQL harness failed for $config"
  fi
}

if [ -d "$exchange/sets" ]; then
  for set_dir in "$exchange"/sets/*; do
    n=$(basename "$set_dir")
    if [ -f "$set_dir/prisma.schema" ]; then
      prisma_ddl "$set_dir"
    fi
    if [ -f "$set_dir/drizzle.run" ]; then
      drizzle_ddl "$set_dir"
    fi
    db="sql_tables_oracle_$n"
    psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
    url=$(dburl "$db")
    while IFS= read -r file; do
      [ -n "$file" ] || continue
      psql "$url" -X -q -1 -v ON_ERROR_STOP=1 -f "$file" >>"$set_dir/apply.log" 2>&1 ||
        die "applying $file failed (see $set_dir/apply.log)"
    done <"$set_dir/migrations.list"
    psql "$url" -X -q -At -F "$(printf '\t')" -c \
      "SELECT c.oid, n.nspname, c.relname, c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace" \
      >"$set_dir/relations.tsv"
    for sql_file in "$set_dir"/site-*.sql; do
      [ -f "$sql_file" ] || continue
      base=${sql_file%.sql}
      status=0
      psql "$url" -X -q -v ON_ERROR_STOP=1 -c "SET debug_print_parse = on" -c "SET client_min_messages = log" \
        -c "PREPARE p AS $(cat "$sql_file")" >/dev/null 2>"$base.log" || status=$?
      printf '%s\n' "$status" >"$base.status"
    done
    psql "$pg" -X -q -c "DROP DATABASE $db" >/dev/null
  done
fi

run_test compare >"$scratch/compare.log" 2>&1 || { cat "$scratch/compare.log"; die "compare phase failed"; }
grep -E 'oracle:|DISAGREEMENT' "$scratch/compare.log" || true
