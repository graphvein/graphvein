#!/usr/bin/env bash
# Local layout check of the website (site/, ADR 0091); not run in CI.
# Builds the site into a temporary folder and checks it with Playwright at
# 1440, 900 and 390 px in light and dark: no horizontal overflow, the
# primary call to action above the fold at 1440 x 900, screenshots in the
# theme opposite to the page's, the lightbox and the header theme switch.
#
# Usage: bash scripts/check-site.sh SHOTS_DIR
# SHOTS_DIR receives the screenshots; keep it outside the repository.
# Needs Go, Node and the playwright package with Chromium (set NODE_PATH
# when playwright is installed outside a node_modules on the search path).
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
if [ $# -ne 1 ]; then
  echo "usage: bash scripts/check-site.sh SHOTS_DIR" >&2
  exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
shots=$1
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
(cd "$root/site" && go run . -out "$tmp/dist")
node "$root/site/tools/check-layout.cjs" "$tmp/dist" "$shots"
