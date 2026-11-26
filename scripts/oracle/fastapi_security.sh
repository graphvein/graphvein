#!/usr/bin/env bash
# The FastAPI security oracle of T026 (rows DA-F1..DA-F3, AA3;
# docs/extract/auth.md "Oracle"). Opt-in, never run by CI.
#
# 1. Creates a scratch virtual environment with the pinned FastAPI 0.161.1
#    (MIT) and its pinned dependencies from PyPI (--no-deps, the exact list
#    below), unless --python names one already built from that list.
# 2. Copies testdata/fixtures/auth-fastapi to the scratch directory and runs
#    fastapi_security.py there: FastAPI's own app.openapi() security per
#    operation for the fixture apps (app/unread.py is not imported: FastAPI
#    refuses its forms at startup). No corpus code is ever imported.
# 3. Runs TestFastAPISecurityOracle, which indexes the same copy and
#    compares every route's security_dependencies with the operations.
#
# Usage: bash scripts/oracle/fastapi_security.sh --scratch DIR [--python VENV_PYTHON]
# Writes only under --scratch. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'fastapi_security: %s\n' "$*" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
scratch="" py=""
while [ $# -gt 0 ]; do
  case "$1" in
    --scratch) scratch=$2; shift 2 ;;
    --python) py=$2; shift 2 ;;
    *) die "unknown argument $1" ;;
  esac
done
[ -n "$scratch" ] || die "--scratch is required"
mkdir -p "$scratch"
scratch=$(cd "$scratch" && pwd)

if [ -z "$py" ]; then
  python3 -m venv "$scratch/venv"
  # Published at least 14 days before 2026-11-18; licences: MIT (fastapi,
  # pydantic, pydantic-core, annotated-doc, annotated-types, anyio,
  # typing-inspection), BSD-3-Clause (starlette, idna), PSF-2.0
  # (typing-extensions), Apache-2.0 (opentelemetry-api, python-multipart).
  "$scratch/venv/bin/python" -m pip install -q --no-deps \
    fastapi==0.161.1 starlette==1.10.0 pydantic==2.14.2 pydantic-core==2.50.2 \
    annotated-doc==0.0.5 annotated-types==0.8.0 anyio==4.16.0 idna==3.20 \
    typing-extensions==4.16.0 typing-inspection==0.4.4 opentelemetry-api==1.45.2 \
    python-multipart==0.0.33
  py="$scratch/venv/bin/python"
fi

root="$scratch/auth-fastapi"
rm -rf "$root"
cp -R "$repo_root/testdata/fixtures/auth-fastapi" "$root"
(cd "$scratch" && "$py" -I -B "$here/fastapi_security.py" --root "$root" \
  --apps "app.main:app,app.partner:app,app.partner:sub" --out "$scratch/openapi-security.json")
(cd "$repo_root" && GRAPHVEIN_FASTAPI_ORACLE_ROOT="$root" GRAPHVEIN_FASTAPI_ORACLE_OPENAPI="$scratch/openapi-security.json" \
  go test ./internal/extract/routes -run '^TestFastAPISecurityOracle$' -count=1 -v)
