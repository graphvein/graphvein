#!/usr/bin/env bash
# The chi and gin oracle of task T023 (ADR 0076). Opt-in, never
# run by CI.
#
# 1. Graphvein's TestGoRoutesGraphExport links the go-routes fixture (and
#    the five corpora when a corpus directory is given) and writes each
#    composition graph as JSON into OUT. No corpus code is built or run.
# 2. TestGoRoutesOracle (this directory, its own Go module) rebuilds the
#    chi and gin registration trees of every graph in the real routers at
#    both pinned version pairs: go.mod (chi v5.5.0, gin v1.12.0) and
#    go.b.mod (chi v5.2.4, gin v1.10.0). Modules come from the Go module
#    proxy (go.sum and go.b.sum pin them).
# 3. TestGoRoutesSourceOracle compiles the fixture's own module
#    testdata/fixtures/go-routes/source with the real routers at both
#    pairs and dispatches every path Graphvein claims for it
#    (source-routes.json, written in step 1).
#
# Usage:
#   bash scripts/oracle/go_routes/run.sh OUT [CORPUS_DIR]
#
# CORPUS_DIR holds the checkouts realworld-gin, gotify, dozzle, miniflux
# and owncast (scripts/cloud-corpus.sh). Writes only under OUT.
# Bash 3.2 compatible.
set -euo pipefail

die() { printf 'go_routes oracle: %s\n' "$*" >&2; exit 1; }

[ $# -ge 1 ] || die "usage: run.sh OUT [CORPUS_DIR]"
out=$1
corpus=${2:-}
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)

(
	cd "$repo"
	GRAPHVEIN_GO_ROUTES_GRAPH_OUT="$out" GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS="$corpus" \
		go test ./internal/extract/routes -run '^TestGoRoutesGraphExport$' -count=1 -v
	GRAPHVEIN_GO_ROUTES_SOURCE_OUT="$out/source" \
		go test ./internal/extract/routes -run '^TestGoRoutesSourceExport$' -count=1 -v
)
(
	cd "$here"
	printf 'pair a: chi v5.5.0, gin v1.12.0\n'
	GRAPHVEIN_GO_ROUTES_GRAPHS="$out" go test -run '^TestGoRoutesOracle$' -count=1 -v .
	GRAPHVEIN_GO_ROUTES_SOURCE="$out/source" go test -run '^TestGoRoutesSourceOracle$' -count=1 -v .
	printf 'pair b: chi v5.2.4, gin v1.10.0\n'
	GRAPHVEIN_GO_ROUTES_GRAPHS="$out" go test -modfile=go.b.mod -run '^TestGoRoutesOracle$' -count=1 -v .
	GRAPHVEIN_GO_ROUTES_SOURCE="$out/source" go test -modfile=go.b.mod -run '^TestGoRoutesSourceOracle$' -count=1 -v .
)
