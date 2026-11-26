#!/usr/bin/env bash
# Exports the public tree of Graphvein (ADR 0080): the committed tree of
# HEAD (git archive) minus the private paths of scripts/public-exclude.txt,
# with docs/public/AGENTS.md moved to the root as AGENTS.md.
#
# Usage: bash scripts/public-snapshot.sh OUTDIR [LISTFILE]
#
# OUTDIR must not exist or be empty. LISTFILE, when given, receives the
# exact file list of the snapshot (one path per line, sorted bytewise);
# docs/public/launch-file-list.txt is written this way at launch:
#   bash scripts/public-snapshot.sh "$(mktemp -d)/public" docs/public/launch-file-list.txt
# Only committed content is exported: commit first.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: bash scripts/public-snapshot.sh OUTDIR [LISTFILE]" >&2
  exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out=$1
list=${2:-}

if [ -e "$out" ] && [ -n "$(ls -A "$out")" ]; then
  echo "public-snapshot: $out exists and is not empty" >&2
  exit 1
fi
mkdir -p "$out"
git -C "$root" archive --format=tar HEAD | tar -x -f - -C "$out"

# Remove the private paths.
while IFS= read -r line || [ -n "$line" ]; do
  entry=${line%%#*}
  entry=$(printf '%s' "$entry" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//')
  [ -z "$entry" ] && continue
  case "$entry" in
    /* | *..*) echo "public-snapshot: bad entry in public-exclude.txt: $entry" >&2; exit 1 ;;
  esac
  rm -rf "${out:?}/${entry%/}"
done < "$root/scripts/public-exclude.txt"

# The public AGENTS.md takes the place of the internal one.
if [ ! -f "$out/docs/public/AGENTS.md" ]; then
  echo "public-snapshot: docs/public/AGENTS.md is missing" >&2
  exit 1
fi
mv "$out/docs/public/AGENTS.md" "$out/AGENTS.md"

if [ -n "$list" ]; then
  (cd "$out" && find . -type f | sed 's|^\./||' | LC_ALL=C sort) > "$list"
fi
echo "public-snapshot: wrote $(cd "$out" && find . -type f | wc -l | tr -d ' ') files to $out"
