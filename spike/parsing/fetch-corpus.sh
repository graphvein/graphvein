#!/usr/bin/env bash
# Fetch sources only: never install dependencies or run code from these repos.
set -euo pipefail
cd "$(dirname "$0")"
command -v git >/dev/null || { echo "Missing git" >&2; exit 1; }
mkdir -p .corpus
while read -r name repository commit; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  target=".corpus/$name"
  if [[ ! -d "$target" ]]; then
    git init -q "$target"
    git -C "$target" remote add origin "$repository"
  fi
  # Refuse to discard changes in a pre-existing checkout.
  if [[ -n "$(git -C "$target" status --porcelain)" ]]; then
    echo "Corpus checkout has local changes: $name" >&2
    exit 1
  fi
  if [[ "$(git -C "$target" rev-parse --verify HEAD 2>/dev/null || true)" != "$commit" ]]; then
    git -C "$target" fetch -q --depth=1 origin "$commit"
    git -C "$target" -c core.autocrlf=false checkout -q --detach FETCH_HEAD
  fi
  [[ "$(git -C "$target" rev-parse HEAD)" == "$commit" ]] || exit 1
  printf '%s %s\n' "$name" "$commit"
done < corpus.tsv
if [[ $# -gt 1 ]]; then
  echo "Usage: bash fetch-corpus.sh [optional-local-repository]" >&2
  exit 1
fi
if [[ $# -eq 1 ]]; then
  [[ -d "$1" ]] || { echo "Local repository is missing" >&2; exit 1; }
  # Keep the private path in ignored local configuration, never corpus.tsv.
  if [[ "$OSTYPE" == msys* ]]; then
    (cd "$1" && pwd -W) > .corpus/local-path.txt
  else
    (cd "$1" && pwd) > .corpus/local-path.txt
  fi
  echo "Optional private corpus configured (contents remain in place)."
else
  # An old private configuration must not silently affect a new public run.
  : > .corpus/local-path.txt
fi
