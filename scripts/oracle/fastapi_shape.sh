#!/usr/bin/env bash
# The FastAPI route shape oracle of T029 (rows SH-F1..SH-F35;
# docs/extract/route-shape.md "FastAPI"). Opt-in, never run by CI.
#
# 1. Creates a scratch virtual environment with the pinned FastAPI 0.161.1
#    and Pydantic 2.14.2 (MIT) and their pinned dependencies from PyPI
#    (--no-deps, the exact list below), unless --python names one already
#    built from that list.
# 2. Copies testdata/fixtures/route-shape-fastapi to the scratch directory
#    and runs fastapi_shape.py there: FastAPI's own app.openapi() for the
#    fixture app. No corpus code is ever imported.
# 3. Runs TestFastAPIShapeOracle, which indexes the same copy and compares
#    every operation's parameters, request body, responses and security
#    with the stored route shapes.
# 4. Indexes the copy and runs fastapi_shape_runtime.py: FastAPI's
#    TestClient drives the app and checks the claimed statuses (every fact
#    status reached by a request), success bodies against the claimed
#    schemas, claimed required body fields, required and optional
#    parameters, and that lowered statuses are overridden at runtime.
# Steps 2-4 run for testdata/fixtures/route-shape-fastapi,
# testdata/fixtures/route-shape-fastapi-handlers,
# testdata/fixtures/route-shape-fastapi-unpack and
# testdata/fixtures/route-shape-fastapi-assign.
#
# Usage: bash scripts/oracle/fastapi_shape.sh --scratch DIR [--python VENV_PYTHON]
# Writes only under --scratch. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'fastapi_shape: %s\n' "$*" >&2; exit 1; }

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
  # Published at least 14 days before 2026-11-20; licences: MIT (fastapi,
  # pydantic, pydantic-core, annotated-doc, annotated-types, anyio,
  # typing-inspection), BSD-3-Clause (starlette, idna), PSF-2.0
  # (typing-extensions), Apache-2.0 (opentelemetry-api, python-multipart),
  # BSD-3-Clause (httpx, httpcore), MIT (h11), MPL-2.0 (certifi).
  "$scratch/venv/bin/python" -m pip install -q --no-deps \
    fastapi==0.161.1 starlette==1.10.0 pydantic==2.14.2 pydantic-core==2.50.2 \
    annotated-doc==0.0.5 annotated-types==0.8.0 anyio==4.16.0 idna==3.20 \
    typing-extensions==4.16.0 typing-inspection==0.4.4 opentelemetry-api==1.45.2 \
    python-multipart==0.0.33 httpx==0.28.1 httpcore==1.0.9 h11==0.16.0 certifi==2025.10.5
  py="$scratch/venv/bin/python"
fi

# The main fixture, the handlers fixture (a separate project: its
# exception handlers apply to every route) and the unpack fixture (its
# unpacked FastAPI may pass exception handlers).
for fixture in main handlers unpack assign; do
  name=route-shape-fastapi
  [ "$fixture" = handlers ] && name=route-shape-fastapi-handlers
  [ "$fixture" = unpack ] && name=route-shape-fastapi-unpack
  [ "$fixture" = assign ] && name=route-shape-fastapi-assign
  root="$scratch/$name"
  rm -rf "$root"
  cp -R "$repo_root/testdata/fixtures/$name" "$root"
  (cd "$scratch" && "$py" -I -B "$here/fastapi_shape.py" --root "$root" \
    --app "app.main:app" --out "$scratch/openapi-$fixture.json")
  (cd "$repo_root" && GRAPHVEIN_FASTAPI_SHAPE_ROOT="$root" GRAPHVEIN_FASTAPI_SHAPE_OPENAPI="$scratch/openapi-$fixture.json" \
    go test ./internal/extract/routes -run '^TestFastAPIShapeOracle$' -count=1 -v)
  (cd "$repo_root" && go run ./cmd/graphvein index "$root" --emit "$scratch/route-shape-$fixture.ndjson" >/dev/null)
  (cd "$scratch" && "$py" -I -B -W ignore "$here/fastapi_shape_runtime.py" --root "$root" \
    --app "app.main:app" --ndjson "$scratch/route-shape-$fixture.ndjson" --fixture "$fixture")
done
