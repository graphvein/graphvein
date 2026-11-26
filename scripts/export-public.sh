#!/usr/bin/env bash
# Makes the export commit of one finished task for the public repository
# (ADR 0080, revision 1: "Model B'"), or checks a merged export.
#
# Usage:
#   bash scripts/export-public.sh [--base REF | --initial] [--body FILE] TASK_ID PUBLIC_CLONE SUBJECT
#   bash scripts/export-public.sh --check-merged PUBLIC_REF [--private REV] PUBLIC_CLONE
#
# Export mode. Exports the committed tree of HEAD of this (the maintainer's
# working) repository as the public snapshot (scripts/public-snapshot.sh),
# runs the public tree check, the reference scan
# (scripts/check-public-tree.sh) and the privacy check
# (scripts/check-public-privacy.sh) on it, and checks the commit message and
# pull request text the same way. Then, in PUBLIC_CLONE (a local clone of the
# public repository), it creates branch export/TASK_ID from REF (default
# origin/main; fetch first) holding ONE commit whose tree is the snapshot:
# the whole tree is replaced, so deletions propagate. --initial makes the
# first public commit, without a parent, in a clone that has no commit yet.
# FILE (--body) holds public notes for the commit message and the pull
# request description; SUBJECT is the commit subject and pull request title.
# TASK_ID is a task id (T050) or a short slug (launch).
#
# Check mode (after the maintainer squash-merged the pull request): compares
# the tree of PUBLIC_REF in PUBLIC_CLONE with a fresh public snapshot of the
# private commit REV (default HEAD), prints every differing path and exits 1
# when they differ.
#
# The script never pushes and never touches PUBLIC_CLONE's working tree or
# index: it builds the tree in a temporary index, copies the objects with
# git pack-objects, commits with git commit-tree and creates the branch with
# git update-ref. It writes the pull request description into PUBLIC_CLONE's
# .git directory (export-pr-TASK_ID.md) and prints the next steps: push the
# branch to the working repository's remote, then, from the maintainer's
# checkout with scripts/hooks/pre-push-public installed, push that commit to
# the public repository (main for --initial, else export/TASK_ID and a pull
# request). The commit is authored and committed by the clone's configured
# identity and is not signed.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/.." && pwd)

usage() {
  sed -n 's/^#   bash/bash/p' "${BASH_SOURCE[0]}" | head -2 | sed 's/^/usage: /' >&2
  exit 2
}
die() {
  echo "export-public: $*" >&2
  exit 1
}

mode=export
base=origin/main
initial=0
body=''
check_ref=''
private_rev=HEAD
while [ $# -gt 0 ]; do
  case $1 in
    --initial) initial=1; shift ;;
    --base) [ $# -ge 2 ] || usage; base=$2; shift 2 ;;
    --body) [ $# -ge 2 ] || usage; body=$2; shift 2 ;;
    --check-merged) [ $# -ge 2 ] || usage; mode=check; check_ref=$2; shift 2 ;;
    --private) [ $# -ge 2 ] || usage; private_rev=$2; shift 2 ;;
    -h | --help) usage ;;
    --) shift; break ;;
    -*) usage ;;
    *) break ;;
  esac
done

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
# Native Git on Windows reads C:/... paths, not Git Bash's /tmp/...
if command -v cygpath >/dev/null 2>&1; then
  tmp=$(cygpath -m "$tmp")
fi

# snapshot_tree REV prints the tree object of the public snapshot of REV,
# built in the working repository with a temporary index: the same rules as
# scripts/public-snapshot.sh (REV's own scripts/public-exclude.txt removed,
# docs/public/AGENTS.md moved to the root), from Git objects instead of an
# archive, so line endings and file modes are exactly the committed ones.
snapshot_tree() {
  local rev=$1 idx=$tmp/snapshot.index line entry info mode_sha
  rm -f "$idx"
  GIT_INDEX_FILE=$idx git -C "$root" read-tree "$rev"
  git -C "$root" show "$rev:scripts/public-exclude.txt" > "$tmp/exclude.txt" ||
    die "$rev has no scripts/public-exclude.txt"
  while IFS= read -r line || [ -n "$line" ]; do
    entry=${line%%#*}
    entry=$(printf '%s' "$entry" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//')
    [ -z "$entry" ] && continue
    case "$entry" in
      /* | *..*) die "bad entry in public-exclude.txt: $entry" ;;
    esac
    GIT_INDEX_FILE=$idx git -C "$root" ls-files -z -- ":(literal)${entry%/}" |
      GIT_INDEX_FILE=$idx git -C "$root" update-index -z --force-remove --stdin
  done < "$tmp/exclude.txt"
  info=$(GIT_INDEX_FILE=$idx git -C "$root" ls-files -s -- ':(literal)docs/public/AGENTS.md')
  [ -n "$info" ] || die "$rev has no docs/public/AGENTS.md"
  mode_sha=$(printf '%s' "$info" | awk '{print $1 "," $2}')
  GIT_INDEX_FILE=$idx git -C "$root" update-index --force-remove -- docs/public/AGENTS.md
  GIT_INDEX_FILE=$idx git -C "$root" update-index --add --cacheinfo "$mode_sha,AGENTS.md"
  GIT_INDEX_FILE=$idx git -C "$root" write-tree
}

# copy_tree TREE copies TREE and everything it holds from the working
# repository into the public clone (a pack, through git index-pack).
copy_tree() {
  if git -C "$clone" cat-file -e "$1^{tree}" 2>/dev/null; then
    return 0
  fi
  { echo "$1"; git -C "$root" ls-tree -r -t "$1" | awk '$2 != "commit" {print $3}'; } |
    git -C "$root" pack-objects --stdout -q |
    git -C "$clone" index-pack --stdin > /dev/null
}

open_clone() {
  [ -d "$clone" ] || die "$clone is not a directory"
  git -C "$clone" rev-parse --git-dir > /dev/null 2>&1 || die "$clone is not a Git repository"
  local mine theirs
  mine=$(cd "$root" && cd "$(git rev-parse --git-common-dir)" && pwd -P)
  theirs=$(cd "$clone" && cd "$(git rev-parse --git-common-dir)" && pwd -P)
  [ "$mine" != "$theirs" ] || die "$clone is this working repository, not a clone of the public repository"
}

short() { git -C "$1" rev-parse --short "$2"; }

if [ "$mode" = check ]; then
  [ $# -eq 1 ] || usage
  clone=$1
  open_clone
  pub=$(git -C "$clone" rev-parse -q --verify "$check_ref^{commit}") || die "$check_ref is not a commit in $clone"
  priv=$(git -C "$root" rev-parse -q --verify "$private_rev^{commit}") || die "$private_rev is not a commit here"
  tree=$(snapshot_tree "$priv")
  copy_tree "$tree"
  if [ "$(git -C "$clone" rev-parse "$pub^{tree}")" = "$tree" ]; then
    echo "export-public: $check_ref ($(short "$clone" "$pub")) has the tree of the public snapshot of $private_rev ($(short "$root" "$priv"))"
    exit 0
  fi
  echo "export-public: $check_ref ($(short "$clone" "$pub")) differs from the public snapshot of $private_rev ($(short "$root" "$priv")):" >&2
  echo "  (A: only in $check_ref, D: only in the snapshot, M: content or mode differs)" >&2
  git -C "$clone" -c core.quotePath=false diff-tree -r --no-renames --name-status "$tree" "$pub" | sed 's/^/  /' >&2
  exit 1
fi

[ $# -eq 3 ] || usage
task=$1
clone=$2
subject=$3
case "$task" in
  '' | [!A-Za-z0-9]* | *[!A-Za-z0-9._-]*) die "task id must be letters, digits, '.', '_' or '-' (for example T050 or launch): $task" ;;
esac
branch=export/$task
git check-ref-format "refs/heads/$branch" || die "export/$task is not a valid branch name"
[ -n "$subject" ] || die "the subject is empty"
case "$subject" in *"
"*) die "the subject must be one line" ;; esac
if [ -n "$body" ] && [ ! -f "$body" ]; then
  die "body file $body not found"
fi
if [ "$initial" = 1 ] && [ "$base" != origin/main ]; then
  die "--initial and --base exclude each other"
fi
open_clone

# 1. Only committed content is exported.
if [ -n "$(git -C "$root" status --porcelain)" ]; then
  git -C "$root" status --short | head -10 >&2
  die "the working repository has uncommitted changes or untracked files; commit or stash them first"
fi
head=$(git -C "$root" rev-parse HEAD)

# 2. The snapshot and its checks.
bash "$here/public-snapshot.sh" "$tmp/public" > /dev/null
bash "$here/check-public-tree.sh" "$tmp/public" || die "the snapshot fails the public tree check"
bash "$here/check-public-privacy.sh" "$tmp/public" || die "the snapshot fails the privacy check"
tree=$(snapshot_tree HEAD)
# The archive snapshot that was checked and the tree that is committed hold
# the same paths.
(cd "$tmp/public" && find . \( -type f -o -type l \) | sed 's|^\./||' | LC_ALL=C sort) > "$tmp/checked.txt"
git -C "$root" -c core.quotePath=false ls-tree -r --name-only "$tree" | LC_ALL=C sort > "$tmp/committed.txt"
if ! cmp -s "$tmp/checked.txt" "$tmp/committed.txt"; then
  diff "$tmp/checked.txt" "$tmp/committed.txt" | head -10 >&2 || true
  die "the checked snapshot and the export tree differ (scripts/public-snapshot.sh and this script disagree)"
fi

# 3. Commit message and pull request text, checked like the tree.
{
  printf '%s\n' "$subject"
  if [ -n "$body" ]; then
    printf '\n'
    sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' "$body"
  fi
  printf '\nTask: %s\n' "$task"
} > "$tmp/message.txt"
gitdir=$(cd "$clone" && cd "$(git rev-parse --git-dir)" && pwd)
pr=$gitdir/export-pr-$task.md
while IFS= read -r line || [ -n "$line" ]; do
  if [ "$line" = "@NOTES@" ]; then
    if [ -n "$body" ]; then cat "$body"; fi
  else
    printf '%s\n' "$line" | sed "s/@TASK@/$task/g"
  fi
done < "$here/export-pr-template.md" | cat -s > "$tmp/pr.md"
mkdir "$tmp/text"
cp "$tmp/public/AGENTS.md" "$tmp/text/AGENTS.md"
cp "$tmp/message.txt" "$tmp/pr.md" "$tmp/text/"
bash "$here/check-public-tree.sh" "$tmp/text" > /dev/null || die "the commit message or pull request text names an internal record"
bash "$here/check-public-privacy.sh" "$tmp/text" > /dev/null || die "the commit message or pull request text fails the privacy check"

# 4. The export commit in the public clone.
if git -C "$clone" rev-parse -q --verify "refs/heads/$branch" > /dev/null; then
  die "$clone already has branch $branch"
fi
parent=''
if [ "$initial" = 1 ]; then
  if [ -n "$(git -C "$clone" rev-list -n 1 --all)" ]; then
    die "$clone already has commits; --initial is only for the first public commit"
  fi
else
  parent=$(git -C "$clone" rev-parse -q --verify "$base^{commit}") ||
    die "base $base is not a commit in $clone (run git -C $clone fetch first)"
  if [ "$(git -C "$clone" rev-parse "$parent^{tree}")" = "$tree" ]; then
    die "nothing to export: the snapshot equals the tree of $base"
  fi
fi
copy_tree "$tree"
if [ -n "$parent" ]; then
  commit=$(git -C "$clone" -c commit.gpgsign=false commit-tree "$tree" -p "$parent" -F "$tmp/message.txt")
else
  commit=$(git -C "$clone" -c commit.gpgsign=false commit-tree "$tree" -F "$tmp/message.txt")
fi
git -C "$clone" update-ref -m "export-public: $task" "refs/heads/$branch" "$commit" ""
cp "$tmp/pr.md" "$pr"

files=$(wc -l < "$tmp/committed.txt" | tr -d ' ')
echo "export-public: created $branch ($(short "$clone" "$commit")) in $clone"
echo "  snapshot of $(short "$root" "$head"): $files files; public tree, reference and privacy checks passed"
if [ -n "$parent" ]; then
  echo "  against $base ($(short "$clone" "$parent")):$(git -C "$clone" diff --shortstat "$parent" "$commit")"
fi
echo
private_url=$(git -C "$root" remote get-url origin 2> /dev/null || echo '<working repository remote>')
public_url=$(git -C "$clone" remote get-url origin 2> /dev/null || echo 'https://github.com/graphvein/graphvein.git')
if [ "$initial" = 1 ]; then
  target=main
else
  target=$branch
fi
echo "Nothing was pushed. Next steps:"
echo "  1. Hand the commit to the working repository's remote:"
echo "     git -C $clone push $private_url refs/heads/$branch:refs/heads/$branch"
echo "  2. In the maintainer's checkout of the working repository, with scripts/hooks/pre-push-public"
echo "     installed as its pre-push hook (it refuses a commit holding a private path):"
echo "     git fetch origin $branch && git push $public_url FETCH_HEAD:refs/heads/$target"
if [ "$initial" = 1 ]; then
  echo "     (the first commit goes straight to main; there is no base for a pull request yet)"
  echo "  3. git -C $clone fetch origin && bash scripts/export-public.sh --check-merged origin/main --private $(short "$root" "$head") $clone"
else
  echo "  3. gh pr create --repo graphvein/graphvein --base main --head $branch --title \"$subject\" --body-file $pr"
  echo "  4. After CI passes, squash-merge it in GitHub, then:"
  echo "     git -C $clone fetch origin && bash scripts/export-public.sh --check-merged origin/main --private $(short "$root" "$head") $clone"
fi
echo
echo "Pull request title: $subject"
echo "Pull request description ($pr):"
sed 's/^/  | /' "$pr"
