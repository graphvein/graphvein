#!/usr/bin/env bash
# Fails when a tree that is meant to be public contains private material
# (ADR 0080). Run it in the public repository's CI on its checkout, or on a
# snapshot made by scripts/public-snapshot.sh.
#
# Usage: bash scripts/check-public-tree.sh [DIR]   (default: the repository root)
#
# Checks:
#   1. no path of scripts/public-exclude.txt exists in DIR;
#   2. the root AGENTS.md is the public one (it does not send agents to the
#      internal notes);
#   3. no text file names an internal record: docs/tasks, docs/research,
#      the handoff notes, the internal plan or CI budget, or a task file by
#      name (T0..-name.md/.jsonl), except the files listed in
#      scripts/public-reference-exempt.txt.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dir=${1:-$(cd "$here/.." && pwd)}
exclude=$here/public-exclude.txt
exempt=$here/public-reference-exempt.txt
fail=0

strip() {
  # Prints each list entry without its comment and surrounding blanks.
  while IFS= read -r line || [ -n "$line" ]; do
    entry=${line%%#*}
    entry=$(printf '%s' "$entry" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//')
    [ -n "$entry" ] && printf '%s\n' "$entry"
  done < "$1"
}

# 1. Private paths.
for entry in $(strip "$exclude"); do
  # AGENTS.md is replaced, not removed: check 2 covers it.
  [ "$entry" = AGENTS.md ] && continue
  if [ -e "$dir/${entry%/}" ]; then
    echo "check-public-tree: private path present: $entry" >&2
    fail=1
  fi
done

# 2. The public AGENTS.md.
if [ ! -f "$dir/AGENTS.md" ]; then
  echo "check-public-tree: AGENTS.md is missing" >&2
  fail=1
elif grep -q 'HANDOFF' "$dir/AGENTS.md"; then
  echo "check-public-tree: AGENTS.md is the internal one" >&2
  fail=1
fi

# 3. References to internal records.
pattern='docs/tasks|docs/research|HANDOFF|docs/handoff|handoff-check|PLAN\.md|ci-budget|\.\./tasks/|(^|[^A-Za-z0-9])T0[0-9]+[a-z0-9]*-[A-Za-z0-9_-]+\.(md|jsonl)'
exempt_list=$(strip "$exempt")
hits=$(cd "$dir" && grep -rIlE --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=.corpus "$pattern" . 2>/dev/null | sed 's|^\./||' | LC_ALL=C sort || true)
for f in $hits; do
  skip=0
  for e in $exempt_list; do
    if [ "$f" = "$e" ]; then
      skip=1
      break
    fi
  done
  [ "$skip" = 1 ] && continue
  echo "check-public-tree: $f names an internal record:" >&2
  (cd "$dir" && grep -nE "$pattern" "$f" | head -3 | sed 's/^/  /' >&2) || true
  fail=1
done

if [ "$fail" != 0 ]; then
  exit 1
fi
echo "check-public-tree: $dir is clean"
