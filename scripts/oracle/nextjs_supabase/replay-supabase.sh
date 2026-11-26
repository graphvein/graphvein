#!/usr/bin/env bash
# The T024 investigation oracle: replay a checkout's supabase/migrations into a scratch
# PostgreSQL 17 database (scripts/cloud-postgres.sh, cloud sessions only) on
# top of supabase-prelude.sql, a stub of the platform objects Supabase
# provides, and print the resulting tables, views and functions as JSON.
#
# Usage: PGBASE=postgres://graphvein@127.0.0.1:54329 \
#   bash replay-supabase.sh <checkout> <database-name> <out.json> <work-dir>
#
# Shims (the scratch cluster has no pgvector or http extension): CREATE
# EXTENSION vector/http is commented out, vector(N) becomes real[], and
# hnsw/ivfflat indexes are dropped. With GV_REAL_EXTENSIONS=1 (a cluster
# that has pgvector and pgsql-http installed, e.g. the PGDG packages
# postgresql-17-pgvector and postgresql-17-http, run as a superuser
# standing in for Supabase's supautils) nothing is shimmed: the prelude's
# two stand-in http types are dropped and the migrations run as written
# (T024c1b, row SB-M4). Each migration runs in one transaction; the script
# prints ok/FAIL per file. Bash 3.2 compatible.
set -u
src=$1; db=$2; out=$3; work=$4
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
base=${PGBASE:?set PGBASE to the cluster URL without a database}
mkdir -p "$work" || exit 1
psql -q "$base/graphvein_test" -c "drop database if exists $db" -c "create database $db" >/dev/null || exit 1
url="$base/$db"
psql -q "$url" -c "alter database $db set search_path = \"\$user\", public, extensions" >/dev/null || exit 1
psql -q -v ON_ERROR_STOP=1 "$url" -f "$here/supabase-prelude.sql" >/dev/null 2>&1 || exit 1
real=${GV_REAL_EXTENSIONS:-0}
if [ "$real" = 1 ]; then
  psql -q -v ON_ERROR_STOP=1 "$url" -c "drop type extensions.http_request, extensions.http_header" >/dev/null || exit 1
fi
log=$work/$db.log
: > "$log"
for f in "$src"/supabase/migrations/*.sql; do
  if [ "$real" = 1 ]; then
    cp "$f" "$work/cur.sql"
  else
    sed -E 's/create extension (if not exists )?(vector|http)[^;]*;/-- shim: &/I; s/vector\(([0-9]+)\)/real[]/gI' "$f" |
      perl -0pe 's/(create index[^;]*using\s+(hnsw|ivfflat)[^;]*;)/-- shim: index dropped\n/gi' > "$work/cur.sql"
  fi
  if psql -q -v ON_ERROR_STOP=1 --single-transaction "$url" -f "$work/cur.sql" >>"$log" 2>&1; then
    echo "ok $(basename "$f")"
  else
    echo "FAIL $(basename "$f")"
  fi
done
if [ "$real" = 1 ]; then
  # T024c1b R1 (SB-M6): the objects each extension owns (pg_depend 'e'),
  # by catalog and name, to compare with supabaseExtensionObjects.
  psql -At "$url" -c "select coalesce(json_agg(json_build_object('extension', e.extname, 'catalog', d.classid::regclass::text, 'object', pg_describe_object(d.classid, d.objid, 0)) order by 1), '[]')
    from pg_depend d join pg_extension e on e.oid = d.refobjid
    where d.refclassid = 'pg_extension'::regclass and d.deptype = 'e' and e.extname in ('vector', 'http')" > "$out.members.json"
fi
psql -At "$url" -c "select json_build_object(
 'tables',(select coalesce(json_agg(table_schema||'.'||table_name order by 1),'[]') from information_schema.tables where table_type='BASE TABLE' and table_schema not in ('pg_catalog','information_schema')),
 'views',(select coalesce(json_agg(table_schema||'.'||table_name order by 1),'[]') from information_schema.views where table_schema not in ('pg_catalog','information_schema')),
 'functions',(select coalesce(json_agg(distinct n.nspname||'.'||p.proname),'[]') from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname not in ('pg_catalog','information_schema')))" > "$out"
