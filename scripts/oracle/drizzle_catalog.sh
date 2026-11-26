#!/usr/bin/env bash
# The Drizzle oracle of T018g2 (ADR 0059). Never run by CI.
#
# For each run it installs the pinned drizzle-kit and drizzle-orm from npm
# into scratch, copies the schema modules (and the modules their runtime
# imports reach) into a tree under that install, so drizzle-orm resolves to
# the instance drizzle-kit checks, writes a scratch config with the run's
# literal dialect, casing and schema paths, and has drizzle-kit print its
# SQL: `export` (no database), or `generate` into a scratch folder when the
# run has a casing, because export ignores casing. Empty
# output, a non-zero exit or anything on stderr is a harness failure, never
# "no tables" (export exits 0 with nothing on stdout when a module fails to
# load). postgresql SQL is applied to a fresh database on the scratch
# Postgres 17 cluster of scripts/cloud-postgres.sh, sqlite SQL to a scratch
# SQLite file; the catalog is dumped as JSON. mysql SQL is kept as the
# mapped reference and applied nowhere (developer decision: mapped).
# Then TestDrizzleOracle lists the claimed expression texts (defaults,
# generated expressions, checks, index predicates and expressions), and
# each is put through the run's database (pg_get_expr and friends) so both
# sides are compared in Postgres's rendering; the databases are dropped.
#
#   GRAPHVEIN_DRIZZLE_ORACLE=<out.json> GRAPHVEIN_DRIZZLE_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/drizzle -run TestDrizzleOracle -v
#
# Usage:
#   bash scripts/oracle/drizzle_catalog.sh --scratch DIR --pg URL --out FILE \
#     --root ROOT (--fixture | --corpus | RUN...)
#
#   RUN  REPO|PKG|CONFIG|SCHEMAS|DIALECT|CASING|KIT|ORM|DROPS|STUBS
#        REPO the repository the test links, relative to ROOT ("." for the
#        fixture); PKG the working directory drizzle-kit runs in
#        and CONFIG the config (both relative to REPO; the group is
#        models/drizzle/<CONFIG>); SCHEMAS the config's literal schema
#        patterns (comma separated, relative to PKG), DIALECT and CASING
#        ("-" for none) its literal keys; KIT and ORM the drizzle-kit and
#        drizzle-orm pins; DROPS the declarations and object entries removed
#        from the copy ("file:Decl;file:Decl.prop": forms that stop
#        drizzle-kit or Postgres with the whole schema, none claimed; the
#        dump lists them); STUBS the modules replaced by throwing stubs
#        ("file=name,name;...": imports called only inside functions).
#   --fixture  the runs of testdata/fixtures/drizzle-models (ROOT is that
#        directory).
#   --corpus   kaneo and dokploy at their pins (ROOT holds the checkouts
#        kaneo/ and dokploy/).
#   --pg URL   a role that may create databases; each postgresql run uses a
#        fresh database drizzle_oracle_<n>, dropped at the end.
#
# Packages the schema modules import at load beyond drizzle-orm are
# installed in the scratch kit at the corpus lockfile's versions (EXTRA
# below); nothing else of the corpus is installed and no package script
# runs (npm --ignore-scripts). Needs node 22.13+ (node:sqlite), npm, psql
# and go. Writes only under --scratch and --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'drizzle_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch="" pg="" out="" root="" preset=""
runs=()
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --pg) pg=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    --fixture | --corpus) preset=$1; shift ;;
    -*) die "unknown option $1" ;;
    *) runs+=("$1"); shift ;;
  esac
done
[ -n "$scratch" ] && [ -n "$out" ] && [ -n "$root" ] || die "--scratch, --out and --root are required"
root=$(cd "$root" && pwd)
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
out_dir=$(cd "$(dirname "$out")" && pwd)
out="$out_dir/$(basename "$out")"

# The fixture's runs. Dropped (each refused or unread by the claim pass,
# so nothing claimed is lost): in pg/db/schema.ts the DR-P1 tables
# (non-exported enum and pgSchema, duplicate table, column and unique
# names: drizzle-kit exits or Postgres rejects them), the spread of a
# string in the D table `partial` (its column set is partial), the D
# column `unknowns.odd` (`.using()` is not a function) and `unknowns.late`
# (drizzle-kit throws on a substitution in a default); in pg/db/keys.ts
# the DR-P1 tables badIndex, badRef and dupUnique and the entry
# `authors.rlsId`, whose foreign key (unknown: rls is not claimed) names a
# column without a unique constraint, which Postgres rejects; in
# sqlite/src/schema.ts the DR-P1 tables left and right (one index name
# twice); in pg/db/review/refs.ts the DR-P1 table byEmail (a foreign key
# to a column that is neither the primary key nor unique: Postgres
# rejects it) and the column byCode.code, whose foreign key (unknown)
# names a column that is unique only through a unique index, which
# drizzle-kit creates after the foreign keys, so Postgres rejects it too.
fixture_runs() {
  local s=pg/db/schema.ts k=pg/db/keys.ts r=pg/db/review/refs.ts
  local pgdrops="$s:usesHidden;$s:inHidden;$s:dupA;$s:dupB;$s:twice;$s:twoUniques;$s:partial....settings.theme;$s:unknowns.odd;$s:unknowns.late;$k:authors.rlsId;$k:badIndex;$k:badRef;$k:dupUnique;$r:byEmail;$r:byCode.code"
  printf '%s\n' \
    ".|pg|pg/drizzle.config.ts|./db/schema.ts,./db/enums/*.ts,./db/keys.ts,./db/review|postgresql|-|0.31.11|0.45.4|$pgdrops|" \
    ".|pgschema|pgschema/drizzle.config.ts|./schema.ts|postgresql|-|0.31.11|0.45.4||" \
    ".|cased|cased/drizzle.config.ts|./schema.ts|postgresql|snake_case|0.31.11|0.45.4||" \
    ".|camel|camel/drizzle.config.ts|./schema.ts|postgresql|camelCase|0.31.11|0.45.4||" \
    ".|sqlite|sqlite/config/sqlite.config.ts|./src/schema.ts|sqlite|-|0.31.11|0.45.4|sqlite/src/schema.ts:left;sqlite/src/schema.ts:right|" \
    ".|mysql|mysql/drizzle.config.ts|./schema.ts|mysql|-|0.31.11|0.45.4||"
}

# The corpora. dokploy's schema modules import three modules only inside
# functions (paths(), encryptValue/decryptValue, generatePassword); they
# are stubbed. Every other module the schema reaches was read before the
# first run: top-level declarations only (tables, enums, relations, zod
# and drizzle-zod schemas, constants). db/schema/dbml.ts (pgGenerate at
# load) is not reached.
corpus_runs() {
  local ds=packages/server/src
  printf '%s\n' \
    "kaneo|apps/api|apps/api/drizzle.config.ts|./src/database/schema.ts|postgresql|-|0.31.11|0.45.4||" \
    "dokploy|apps/dokploy|apps/dokploy/server/db/drizzle.config.ts|./server/db/schema/index.ts|postgresql|-|0.31.9|0.45.2||$ds/constants/index.ts=paths;$ds/lib/encryption.ts=encryptValue,decryptValue;$ds/templates/index.ts=generatePassword"
}

# extra KIT: the packages the corpus schema modules import at load, at the
# versions of the corpus lockfiles (kaneo apps/api; dokploy
# packages/server).
extra() {
  case "$1" in
    0.31.11) printf '%s\n' "@paralleldrive/cuid2@3.3.0" ;;
    0.31.9) printf '%s\n' "zod@4.6.7" "drizzle-zod@0.5.1" "nanoid@3.3.18" "@faker-js/faker@10.6.0" ;;
  esac
}

case "$preset" in
  --fixture) while IFS= read -r r; do runs+=("$r"); done <<EOF
$(fixture_runs)
EOF
    ;;
  --corpus) while IFS= read -r r; do runs+=("$r"); done <<EOF
$(corpus_runs)
EOF
    ;;
esac
[ ${#runs[@]} -gt 0 ] || die "no runs"

helper() { node "$here/drizzle_catalog.mjs" "$@"; }

# install KIT ORM: drizzle-kit, drizzle-orm and the corpus extras in
# scratch, exact versions, no install scripts.
install() {
  local dir="$scratch/kit-$1-orm-$2"
  if [ ! -x "$dir/node_modules/.bin/drizzle-kit" ]; then
    mkdir -p "$dir"
    printf '{"private": true}\n' >"$dir/package.json"
    local pkgs
    pkgs=$(extra "$1" | tr '\n' ' ')
    # shellcheck disable=SC2086
    (cd "$dir" && npm install --no-audit --no-fund --silent --ignore-scripts --save-exact "drizzle-kit@$1" "drizzle-orm@$2" $pkgs >/dev/null) ||
      die "npm install drizzle-kit@$1 drizzle-orm@$2 failed"
  fi
  node -e 'const [d, k, o] = process.argv.slice(1); for (const [p, v] of [["drizzle-kit", k], ["drizzle-orm", o]]) { const got = require(d + "/node_modules/" + p + "/package.json").version; if (got !== v) { console.error(p + " " + got + " installed, " + v + " wanted"); process.exit(1); } }' "$dir" "$1" "$2" ||
    die "scratch install $dir does not hold the pins"
  printf '%s\n' "$dir"
}

catalog_sql() {
  cat <<'SQL'
WITH t AS (
  SELECT c.oid, n.nspname, c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'drizzle') AND n.nspname NOT LIKE 'pg\_toast%')
SELECT json_build_object(
  'tables', (SELECT coalesce(json_agg(json_build_object(
    'schema', t.nspname, 'name', t.relname,
    'columns', (SELECT coalesce(json_agg(json_build_object(
        'name', a.attname, 'type', pg_catalog.format_type(a.atttypid, a.atttypmod), 'not_null', a.attnotnull,
        'default', CASE WHEN a.attgenerated = '' THEN pg_catalog.pg_get_expr(d.adbin, d.adrelid) END,
        'generated', CASE WHEN a.attgenerated = 's' THEN pg_catalog.pg_get_expr(d.adbin, d.adrelid) END,
        'identity', a.attidentity::text) ORDER BY a.attnum), '[]')
      FROM pg_catalog.pg_attribute a
      LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
      WHERE a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped),
    'constraints', (SELECT coalesce(json_agg(json_build_object(
        'name', con.conname, 'type', con.contype::text,
        'columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.conkey) WITH ORDINALITY k(n, ord)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n),
        'ref_schema', fn.nspname, 'ref_table', fc.relname,
        'ref_columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.confkey) WITH ORDINALITY k(n, ord)
                        JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.n),
        'on_delete', con.confdeltype::text, 'on_update', con.confupdtype::text, 'index', ci.relname,
        'definition', pg_catalog.pg_get_constraintdef(con.oid)) ORDER BY con.conname), '[]')
      FROM pg_catalog.pg_constraint con LEFT JOIN pg_catalog.pg_class fc ON fc.oid = con.confrelid
      LEFT JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace LEFT JOIN pg_catalog.pg_class ci ON ci.oid = con.conindid
      WHERE con.conrelid = t.oid),
    'indexes', (SELECT coalesce(json_agg(json_build_object(
        'name', ic.relname, 'unique', i.indisunique, 'primary', i.indisprimary, 'nulls_not_distinct', i.indnullsnotdistinct,
        'columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY k(n, ord)
                    LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n WHERE k.ord <= i.indnkeyatts),
        'keys', (SELECT json_agg(pg_catalog.pg_get_indexdef(i.indexrelid, k.ord::int, true) ORDER BY k.ord)
                 FROM generate_series(1, i.indnkeyatts) WITH ORDINALITY k(n, ord)),
        'options', (SELECT json_agg(o.v ORDER BY o.ord) FROM unnest(i.indoption::pg_catalog.int2[]) WITH ORDINALITY o(v, ord) WHERE o.ord <= i.indnkeyatts),
        'opclasses', (SELECT json_agg(json_build_object('name', oc.opcname, 'default', oc.opcdefault) ORDER BY o.ord)
                      FROM unnest(i.indclass::pg_catalog.oid[]) WITH ORDINALITY o(v, ord) JOIN pg_catalog.pg_opclass oc ON oc.oid = o.v WHERE o.ord <= i.indnkeyatts),
        'method', am.amname, 'partial', i.indpred IS NOT NULL, 'expressions', i.indexprs IS NOT NULL,
        'where', pg_catalog.pg_get_expr(i.indpred, i.indrelid), 'reloptions', ic.reloptions,
        'definition', pg_catalog.pg_get_indexdef(i.indexrelid)) ORDER BY ic.relname), '[]')
      FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid JOIN pg_catalog.pg_am am ON am.oid = ic.relam
      WHERE i.indrelid = t.oid)) ORDER BY t.nspname, t.relname), '[]') FROM t),
  'enums', (SELECT coalesce(json_agg(json_build_object('schema', n.nspname, 'name', ty.typname,
      'values', (SELECT json_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_catalog.pg_enum e WHERE e.enumtypid = ty.oid))
      ORDER BY n.nspname, ty.typname), '[]')
    FROM pg_catalog.pg_type ty JOIN pg_catalog.pg_namespace n ON n.oid = ty.typnamespace WHERE ty.typtype = 'e'));
SQL
}

export PGOPTIONS="-c client_min_messages=warning"
dburl() { printf '%s' "$pg" | sed -E "s#/[^/?]+(\\?|\$)#/$1\\1#"; }

records=()
pgruns=()
n=0
for r in "${runs[@]}"; do
  IFS='|' read -r repo pkg config schemas dialect casing kit orm drops stubs <<EOF
$r
EOF
  idx=$n
  n=$((n + 1))
  kitdir=$(install "$kit" "$orm")
  work="$kitdir/run-$n"
  rm -rf "$work"
  mkdir -p "$work"
  printf 'drizzle_catalog: run %d: %s/%s (drizzle-kit %s, drizzle-orm %s, %s)\n' "$n" "$repo" "$config" "$kit" "$orm" "$dialect" >&2
  helper prepare "$root/$repo" "$pkg" "$schemas" "$dialect" "$casing" "$drops" "$stubs" "$work/tree" >"$work/modules.txt"
  cfg="$work/tree/$pkg/oracle.config.ts"
  command=export
  [ "$casing" = "-" ] || command=generate
  set +e
  if [ "$command" = export ]; then
    (cd "$work/tree/$pkg" && "$kitdir/node_modules/.bin/drizzle-kit" export --config "$cfg" >"$work/out.sql" 2>"$work/kit.log")
  else
    (cd "$work/tree/$pkg" && "$kitdir/node_modules/.bin/drizzle-kit" generate --config "$cfg" --name oracle >"$work/kit.out" 2>"$work/kit.log")
  fi
  status=$?
  set -e
  [ "$status" -eq 0 ] || { cat "$work/kit.log" >&2; die "run $n: drizzle-kit $command exited $status"; }
  [ ! -s "$work/kit.log" ] || { cat "$work/kit.log" >&2; die "run $n: drizzle-kit $command wrote to stderr"; }
  if [ "$command" = generate ]; then
    gen=$(ls "$work/tree/$pkg/oracle-out"/*.sql 2>/dev/null || true)
    [ "$(printf '%s\n' "$gen" | grep -c .)" -eq 1 ] || die "run $n: drizzle-kit generate wrote $(printf '%s\n' "$gen" | grep -c .) SQL files"
    cp "$gen" "$work/out.sql"
  fi
  grep -q 'CREATE TABLE' "$work/out.sql" || { cat "$work/kit.log" >&2; die "run $n: drizzle-kit $command printed no CREATE TABLE"; }
  catalog="$work/catalog.json"
  case "$dialect" in
    postgresql)
      [ -n "$pg" ] || die "run $n: --pg is required for postgresql"
      db="drizzle_oracle_$n"
      psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
      psql "$(dburl "$db")" -X -q -v ON_ERROR_STOP=1 -f "$work/out.sql" >"$work/apply.log" 2>&1 ||
        { cat "$work/apply.log" >&2; die "run $n: applying the SQL failed"; }
      catalog_sql | psql "$(dburl "$db")" -X -q -At -v ON_ERROR_STOP=1 -f - >"$catalog"
      pgruns+=("$idx|$db")
      ;;
    sqlite) helper sqlite "$work/out.sql" "$work/oracle.db" "$catalog" ;;
    mysql) catalog=- ;;
    *) die "run $n: dialect $dialect has no oracle" ;;
  esac
  helper record "$work/record.json" "$repo" "$config" "$dialect" "$kit" "$orm" "$command" "$drops" "$work/out.sql" "$catalog"
  records+=("$work/record.json")
done
helper merge "$out" "${records[@]}"

# Normalise the claimed expression texts through each run's database.
requests="$scratch/requests.json"
rm -f "$requests"
(cd "$repo_root" && GRAPHVEIN_DRIZZLE_ORACLE="$out" GRAPHVEIN_DRIZZLE_ORACLE_ROOT="$root" GRAPHVEIN_DRIZZLE_ORACLE_REQUESTS="$requests" \
  go test ./internal/extract/models/drizzle -run '^TestDrizzleOracle$' -count=1 >"$scratch/requests.log" 2>&1) ||
  { cat "$scratch/requests.log" >&2; die "listing the claimed expressions failed"; }
[ -f "$requests" ] || die "TestDrizzleOracle wrote no requests file"
for p in ${pgruns[@]+"${pgruns[@]}"}; do
  idx=${p%%|*}
  db=${p#*|}
  helper normalize-sql "$out" "$requests" "$idx" >"$scratch/normalize-$idx.sql"
  psql "$(dburl "$db")" -X -q -At -f "$scratch/normalize-$idx.sql" >"$scratch/normalize-$idx.out" 2>"$scratch/normalize-$idx.log" || true
  helper normalized "$out" "$requests" "$idx" "$scratch/normalize-$idx.out"
  psql "$pg" -X -q -c "DROP DATABASE $db" >/dev/null
done
printf 'drizzle_catalog: %d runs written to %s\n' "${#records[@]}" "$out" >&2
