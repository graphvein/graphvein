#!/usr/bin/env bash
# The SQLAlchemy fixture oracle of T018h (rows CT-P1, CT-P2; ADR 0060,
# docs/extract/table-access.md "Oracle"). Never run by CI.
#
# 1. TestSQLAlchemyOracle (phase requests) links ROOT and writes every
#    claimed CT-P1/CT-P2 site's module, statement text and claimed tables.
# 2. sql_tables_sqlalchemy.py, in a scratch virtual environment with the
#    pinned SQLModel 0.0.39 / SQLAlchemy 2.0.51 / Pydantic 2.13.4, evaluates
#    and compiles each statement and compares its tables; it also writes the
#    SQLModel.metadata DDL (Postgres dialect) to SCRATCH/ddl.sql, which
#    scripts/oracle/sql_tables.sh --model-ddl applies for the CT-P5 sites.
#
# Usage:
#   bash scripts/oracle/sql_tables_sqlalchemy.sh --scratch DIR --python VENV_PYTHON --root ROOT
#
# ROOT is a scratch copy of the fixture (Python imports it). Writes only
# under --scratch. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'sql_tables_sqlalchemy: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch="" py="" root=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --python) py=$2; shift 2 ;;
    --root) root=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] && [ -n "$py" ] && [ -n "$root" ] || die "--scratch, --python and --root are required"
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)
root=$(cd "$root" && pwd)

(cd "$repo_root" && GRAPHVEIN_SQLALCHEMY_ORACLE_ROOT="$root" GRAPHVEIN_SQLALCHEMY_ORACLE_REQUESTS="$scratch/requests.json" \
  go test ./internal/extract/tableaccess -run '^TestSQLAlchemyOracle$' -count=1 -v) >"$scratch/requests.log" 2>&1 ||
  { cat "$scratch/requests.log" >&2; die "requests phase failed"; }
(cd "$scratch" && "$py" -I -B "$here/sql_tables_sqlalchemy.py" --root "$root" --requests "$scratch/requests.json" \
  --ddl "$scratch/ddl.sql" --out "$scratch/results.json")
