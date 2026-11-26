#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
tracked=$(git ls-files -ci --exclude-standard -- testdata/)
ignored=$(git status --porcelain --ignored -- testdata/ | sed -n '/^!! /p')
if [[ -n "$tracked$ignored" ]]; then
  printf 'Ignored fixture inputs are forbidden:\n%s\n%s\n' "$tracked" "$ignored" >&2
  exit 1
fi
