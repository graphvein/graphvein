#!/usr/bin/env bash
# The route shape oracle of task T029. Opt-in, never run by CI.
#
# 1. Graphvein's TestGoShapeExport links the go-shape fixture
#    (testdata/fixtures/go-shape) and writes the claimed route shapes and
#    type schemas into OUT (shapes.json, types.json).
# 2. TestGoShapeTypes and TestGoShapeRoutes (this directory, its own Go
#    module: gin v1.12.0, chi v5.5.0, pinned in go.sum, from the Go module
#    proxy) compile the fixture and compare with encoding/json and with
#    real responses of the fixture's routers on 127.0.0.1.
#
# Usage: bash scripts/oracle/go_shape/run.sh OUT
# Writes only under OUT. Bash 3.2 compatible.
set -euo pipefail

die() { printf 'go_shape oracle: %s\n' "$*" >&2; exit 1; }

[ $# -ge 1 ] || die "usage: run.sh OUT"
out=$1
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)

(
	cd "$repo"
	GRAPHVEIN_GO_SHAPE_OUT="$out" go test ./internal/extract/routes -run '^TestGoShapeExport$' -count=1 -v
)
(
	cd "$here"
	GRAPHVEIN_GO_SHAPE="$out" go test -run '^TestGoShape(Types|Routes)$' -count=1 -v .
)
