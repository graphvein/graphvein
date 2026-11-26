#!/usr/bin/env bash
# The Prisma oracle of T018g1 (ADR 0057). Never run by CI.
#
# For each run it installs the pinned Prisma CLI from npm into scratch, has
# it print the SQL Migrate would run for the schema (`migrate diff
# --from-empty`, no database connection), applies that SQL to a fresh
# database (postgresql: on the scratch Postgres 17 cluster of
# scripts/cloud-postgres.sh; sqlite: a scratch SQLite file) and dumps the
# catalog as JSON. TestPrismaOracle (internal/extract/models/prisma)
# compares every claimed fact with it:
#
#   GRAPHVEIN_PRISMA_ORACLE=<out.json> GRAPHVEIN_PRISMA_ORACLE_ROOT=<root> \
#     go test ./internal/extract/models/prisma -run TestPrismaOracle -v
#
# Usage:
#   bash scripts/oracle/prisma_catalog.sh --scratch DIR --pg URL --out FILE \
#     --root ROOT (--fixture | --corpus | RUN...)
#
#   RUN      DIR|SCHEMA|VERSION|DROPS: DIR is the directory (relative to
#            ROOT) the test links, SCHEMA the schema file or folder relative
#            to DIR (the model group's `set` is "models/prisma/<SCHEMA>"),
#            VERSION the Prisma CLI, DROPS the forms removed before the run
#            ("Model;Model.field;Model:<line as written>"): forms Prisma or
#            Postgres rejects with the whole schema, whose claims are then
#            not compared; the dump lists them.
#   --fixture  the runs of testdata/fixtures/prisma-models (ROOT is that
#            directory).
#   --corpus   umami and hoppscotch at their locked versions (ROOT holds
#            the checkouts umami/ and hoppscotch/).
#   The fixture's postgresql and sqlite schemas also run with Prisma 6.19.3
#   and 5.22.0: the test then links with the lockfile's pin rewritten to
#   the run's version, so each run compares the claims for that version.
#   --pg URL   a role that may create databases; each postgresql run uses a
#            fresh database prisma_oracle_<n> on that server, dropped after.
#
# Needs node/npm (the CLI), psql and python3. Writes only under --scratch
# and --out. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'prisma_catalog: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
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

# The fixture's runs. DROPS are the forms Prisma or Postgres rejects with
# the whole schema (PR-P1 models, a relation naming an unknown field, an
# unknown attribute, refused native types and defaults, a composite type,
# @@fulltext and the partial indexes and unique indexes (where:, a preview
# feature) on postgresql): Prisma validates the schema as a whole, so the
# rest is compared only once they are gone. None of them is claimed (each
# is PR-P1 or D).
fixture_runs() {
  local pgdrops='Broken;Author.broken;Unread.odd;TwoIds;IdBoth;NonLiteralMap;Refused;Keyless;Post.keyless;Address;WithView.summary;WithView.addr;Defaults.seq;Defaults.named;Natives.wrong;Natives.badArg;Natives.unknown;Unread:@@fulltext([plain]);Partial:@@index([name], where: raw("name IS NOT NULL"));Partial.slug;Partial:@@unique([a, b], where: raw("a > 0"))'
  # Prisma 5.22.0 also rejects cuid(2) and ulid(), which the claim pass
  # leaves unknown before 6.0.0 and 6.2.0.
  local v5drops='Defaults.c2;Defaults.ulid'
  # SQLite: a Json literal default renders as invalid SQL (DEFAULT {}), so
  # its default is unknown; Prisma 5.22.0 has no Json or enums on sqlite
  # (both from 6.2.0).
  local litedrops='Defaults.doc'
  printf '%s\n' \
    ".|pg/prisma/schema.prisma|7.10.0|$pgdrops" \
    ".|pg/prisma/schema.prisma|6.19.3|$pgdrops" \
    ".|pg/prisma/schema.prisma|5.22.0|$pgdrops;$v5drops" \
    ".|sqlite/schema.prisma|7.10.0|$litedrops" \
    ".|sqlite/schema.prisma|6.19.3|$litedrops" \
    ".|sqlite/schema.prisma|5.22.0|$litedrops;Mood;Note.mood;Note.data;Defaults.mood" \
    ".|multi/prisma/schema|7.10.0|Stray;Unplaced" \
    ".|emulated/schema.prisma|7.10.0|" \
    ".|v5/db/schema.prisma|5.22.0|Invoice.code" \
    ".|v6/models|6.19.3|"
}

corpus_runs() {
  printf '%s\n' \
    "umami|prisma/schema.prisma|7.10.0|" \
    "hoppscotch|packages/hoppscotch-backend/prisma/schema.prisma|7.9.1|"
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

# install VERSION: the CLI in scratch (the schema engine downloads from
# binaries.prisma.sh on install).
install() {
  local dir="$scratch/cli-$1"
  if [ ! -x "$dir/node_modules/.bin/prisma" ]; then
    mkdir -p "$dir"
    printf '{"private": true}\n' >"$dir/package.json"
    (cd "$dir" && npm install --no-audit --no-fund --silent "prisma@$1" >/dev/null) || die "npm install prisma@$1 failed"
  fi
  printf '%s\n' "$dir/node_modules/.bin/prisma"
}

catalog_sql() {
  cat <<'SQL'
WITH t AS (
  SELECT c.oid, n.nspname, c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_toast%')
SELECT json_build_object(
  'tables', (SELECT coalesce(json_agg(json_build_object(
    'schema', t.nspname, 'name', t.relname,
    'columns', (SELECT coalesce(json_agg(json_build_object(
        'name', a.attname, 'type', pg_catalog.format_type(a.atttypid, a.atttypmod), 'not_null', a.attnotnull,
        'default', pg_catalog.pg_get_expr(d.adbin, d.adrelid),
        'type_schema', (SELECT tn.nspname FROM pg_catalog.pg_type ty JOIN pg_catalog.pg_namespace tn ON tn.oid = ty.typnamespace
                        WHERE ty.oid = CASE WHEN et.typelem <> 0 AND et.typcategory = 'A' THEN et.typelem ELSE a.atttypid END),
        'type_name', (SELECT ty.typname FROM pg_catalog.pg_type ty
                      WHERE ty.oid = CASE WHEN et.typelem <> 0 AND et.typcategory = 'A' THEN et.typelem ELSE a.atttypid END),
        'array', et.typcategory = 'A') ORDER BY a.attnum), '[]')
      FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_type et ON et.oid = a.atttypid
      LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
      WHERE a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped),
    'constraints', (SELECT coalesce(json_agg(json_build_object(
        'name', con.conname, 'type', con.contype::text,
        'columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.conkey) WITH ORDINALITY k(n, ord)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n),
        'ref_schema', fn.nspname, 'ref_table', fc.relname,
        'ref_columns', (SELECT json_agg(a.attname ORDER BY k.ord) FROM unnest(con.confkey) WITH ORDINALITY k(n, ord)
                        JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.n),
        'on_delete', con.confdeltype::text, 'on_update', con.confupdtype::text, 'index', ci.relname) ORDER BY con.conname), '[]')
      FROM pg_catalog.pg_constraint con LEFT JOIN pg_catalog.pg_class fc ON fc.oid = con.confrelid
      LEFT JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace LEFT JOIN pg_catalog.pg_class ci ON ci.oid = con.conindid
      WHERE con.conrelid = t.oid),
    'indexes', (SELECT coalesce(json_agg(json_build_object(
        'name', ic.relname, 'unique', i.indisunique, 'primary', i.indisprimary,
        'columns', (SELECT json_agg(coalesce(a.attname, '') ORDER BY k.ord) FROM unnest(i.indkey::pg_catalog.int2[]) WITH ORDINALITY k(n, ord)
                    LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n WHERE k.ord <= i.indnkeyatts),
        'method', am.amname, 'partial', i.indpred IS NOT NULL, 'expressions', i.indexprs IS NOT NULL,
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

py() { python3 -I -B "$here/prisma_catalog.py" "$@"; }

records=()
n=0
for r in "${runs[@]}"; do
  n=$((n + 1))
  IFS='|' read -r dir schema version drops <<EOF
$r
EOF
  work="$scratch/run-$n"
  rm -rf "$work"
  mkdir -p "$work"
  src="$root/$dir/$schema"
  [ -e "$src" ] || die "run $n: $src does not exist"
  target="$work/schema"
  [ -d "$src" ] || target="$work/schema.prisma"
  major=${version%%.*}
  provider=$(py prepare "$src" "$target" "$drops" "$major")
  case "$provider" in
    postgresql) url="postgresql://oracle@127.0.0.1:1/oracle" ;;
    sqlite) url="file:./oracle.db" ;;
    *) die "run $n: provider $provider has no oracle" ;;
  esac
  cli=$(install "$version")
  printf 'prisma_catalog: run %d: %s/%s (prisma %s, %s)\n' "$n" "$dir" "$schema" "$version" "$provider" >&2
  if [ "$major" -ge 7 ]; then
    # Prisma 7 needs a datasource URL in a config file to start the schema
    # engine, even from empty; the project's own config is not loaded.
    printf 'export default { datasource: { url: "%s" } };\n' "$url" >"$work/prisma.config.ts"
    (cd "$work" && "$cli" migrate diff --from-empty --to-schema "$target" --script -o "$work/out.sql" >"$work/diff.log" 2>&1) ||
      { cat "$work/diff.log" >&2; die "run $n: migrate diff failed"; }
  else
    (cd "$work" && DATABASE_URL=$url PRISMA_HIDE_UPDATE_MESSAGE=1 "$cli" migrate diff --from-empty --to-schema-datamodel "$target" --script >"$work/out.sql" 2>"$work/diff.log") ||
      { cat "$work/diff.log" >&2; die "run $n: migrate diff failed"; }
  fi
  if [ "$provider" = postgresql ]; then
    [ -n "$pg" ] || die "run $n: --pg is required for postgresql"
    db="prisma_oracle_$n"
    psql "$pg" -X -q -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
    dburl=$(printf '%s' "$pg" | sed -E "s#/[^/?]+(\\?|\$)#/$db\\1#")
    # Extensions are the environment's (PR-V2 covers only what Prisma
    # creates); the fixture's citext column and its gin_trgm_ops and text
    # Gist indexes need three.
    psql "$dburl" -X -q -v ON_ERROR_STOP=1 -c "CREATE EXTENSION IF NOT EXISTS citext" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm" -c "CREATE EXTENSION IF NOT EXISTS btree_gist" >/dev/null
    psql "$dburl" -X -q -v ON_ERROR_STOP=1 -f "$work/out.sql" >"$work/apply.log" 2>&1 ||
      { cat "$work/apply.log" >&2; die "run $n: applying the SQL failed"; }
    catalog_sql | psql "$dburl" -X -q -At -v ON_ERROR_STOP=1 -f - >"$work/catalog.json"
    psql "$pg" -X -q -c "DROP DATABASE $db" >/dev/null
  else
    py sqlite "$work/out.sql" "$work/oracle.db" "$work/catalog.json"
  fi
  py run "$work/record.json" "$dir" "$version" "$provider" "$schema" "$drops" "$work/out.sql" "$work/catalog.json"
  records+=("$work/record.json")
done
py merge "$out" "${records[@]}"
printf 'prisma_catalog: %d runs written to %s\n' "${#records[@]}" "$out" >&2
