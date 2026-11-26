#!/usr/bin/env bash
# Prints the Git tree id of the public snapshot of a Graphvein commit: the
# tree scripts/public-snapshot.sh exports from it. A public commit with the
# same tree id (git rev-parse <commit>^{tree}) holds the same code; see
# docs/evidence/benchmark/method.md, "Graphvein identity".
#
# Usage: bash scripts/benchmark/public-tree.sh [COMMIT]   (default HEAD)
#
# Bash 3.2 compatible; runs in Git Bash on Windows. Needs git and go.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"
commit=${1:-HEAD}
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
git -C "$repo" ls-tree -r -z "$commit" >"$tmp/ls-tree"
git -C "$repo" show "$commit:scripts/public-exclude.txt" >"$tmp/exclude"
cd "$repo"
go run ./scripts/benchmark/gvbench public-tree \
  --ls-tree "$(native_path "$tmp/ls-tree")" --exclude "$(native_path "$tmp/exclude")"
