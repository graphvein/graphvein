#!/usr/bin/env bash
# Imports a merged contributor pull request of the public repository into
# this (the maintainer's working) repository, keeping its author (ADR 0080,
# revision 1: "Model B'").
#
# Usage: bash scripts/import-contrib.sh [--base REV] PUBLIC_CLONE COMMIT|FROM..TO PR_NUMBER
#
# PUBLIC_CLONE is a local clone of the public repository (fetch first).
# COMMIT is the public squash-merge commit; its diff against its first
# parent is imported. FROM..TO imports the diff of a pull request's head
# range instead. The diff is applied, in a temporary index, to REV (default
# HEAD) and committed as one commit on the new branch import/pr-PR_NUMBER:
#   author     name, e-mail and date of COMMIT (or TO), unchanged;
#   committer  this repository's configured identity;
#   message    the public commit message, ending with "Public-PR: #PR_NUMBER";
#              other authors of a FROM..TO range become Co-authored-by lines.
# Refused when the diff touches a private path (scripts/public-exclude.txt),
# the root AGENTS.md (the public AGENTS.md is docs/public/AGENTS.md here) or
# docs/public/AGENTS.md (which the public tree never holds).
#
# It never pushes and never touches the working tree or the index: the
# branch is created with git update-ref. The next steps are printed.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/.." && pwd)

usage() {
  echo "usage: bash scripts/import-contrib.sh [--base REV] PUBLIC_CLONE COMMIT|FROM..TO PR_NUMBER" >&2
  exit 2
}
die() {
  echo "import-contrib: $*" >&2
  exit 1
}

base=HEAD
while [ $# -gt 0 ]; do
  case $1 in
    --base) [ $# -ge 2 ] || usage; base=$2; shift 2 ;;
    -h | --help) usage ;;
    --) shift; break ;;
    -*) usage ;;
    *) break ;;
  esac
done
[ $# -eq 3 ] || usage
clone=$1
spec=$2
pr=$3
case "$pr" in
  '' | *[!0-9]* | 0*) die "PR_NUMBER must be a positive number: $pr" ;;
esac
branch=import/pr-$pr

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
if command -v cygpath >/dev/null 2>&1; then
  tmp=$(cygpath -m "$tmp")
fi

[ -d "$clone" ] && git -C "$clone" rev-parse --git-dir > /dev/null 2>&1 || die "$clone is not a Git repository"
case "$spec" in
  *...*) die "use FROM..TO (two dots), not $spec" ;;
  *..*)
    from=$(git -C "$clone" rev-parse -q --verify "${spec%%..*}^{commit}") || die "${spec%%..*} is not a commit in $clone"
    to=$(git -C "$clone" rev-parse -q --verify "${spec##*..}^{commit}") || die "${spec##*..} is not a commit in $clone"
    ;;
  *)
    to=$(git -C "$clone" rev-parse -q --verify "$spec^{commit}") || die "$spec is not a commit in $clone"
    from=$(git -C "$clone" rev-parse -q --verify "$to^1") || die "$spec has no parent; nothing to import"
    ;;
esac
base_sha=$(git -C "$root" rev-parse -q --verify "$base^{commit}") || die "$base is not a commit here"
if git -C "$root" rev-parse -q --verify "refs/heads/$branch" > /dev/null; then
  die "branch $branch already exists"
fi

# 1. Paths: nothing private, no AGENTS.md.
git -C "$clone" -c core.quotePath=false diff --name-only --no-renames "$from" "$to" > "$tmp/paths.txt"
[ -s "$tmp/paths.txt" ] || die "the diff of $spec is empty"
# The entries are parsed once: a subshell per path and entry is slow in Git
# Bash on Windows.
git -C "$root" show "$base_sha:scripts/public-exclude.txt" |
  sed 's/#.*//; s/[[:space:]]*$//; s/^[[:space:]]*//; s|/$||; /^$/d' > "$tmp/exclude.txt"
refused=0
while IFS= read -r path; do
  case "$path" in
    AGENTS.md)
      echo "import-contrib: the diff changes the root AGENTS.md; apply that change to docs/public/AGENTS.md by hand" >&2
      refused=1
      continue
      ;;
    docs/public/AGENTS.md)
      echo "import-contrib: the diff touches docs/public/AGENTS.md, which the public tree never holds" >&2
      refused=1
      continue
      ;;
  esac
  while IFS= read -r entry || [ -n "$entry" ]; do
    case "$path" in
      "$entry" | "$entry"/*)
        echo "import-contrib: the diff touches private path $path (scripts/public-exclude.txt: $entry)" >&2
        refused=1
        ;;
    esac
  done < "$tmp/exclude.txt"
done < "$tmp/paths.txt"
[ "$refused" = 0 ] || die "refused: a public pull request must not change private paths"

# 2. Apply the diff to BASE in a temporary index.
git -C "$clone" diff --binary --full-index --no-renames "$from" "$to" > "$tmp/patch"
GIT_INDEX_FILE=$tmp/index git -C "$root" read-tree "$base_sha"
GIT_INDEX_FILE=$tmp/index git -C "$root" apply --cached --whitespace=nowarn "$tmp/patch" ||
  die "the diff of $spec does not apply to $base ($(git -C "$root" rev-parse --short "$base_sha")); import it onto an older base with --base, or apply it by hand"
tree=$(GIT_INDEX_FILE=$tmp/index git -C "$root" write-tree)

# 3. The commit, with the public author and the public PR number.
author_name=$(git -C "$clone" log -1 --format=%an "$to")
author_email=$(git -C "$clone" log -1 --format=%ae "$to")
author_date=$(git -C "$clone" log -1 --format=%ad --date=raw "$to")
git -C "$clone" log -1 --format=%B "$to" | grep -v '^Public-PR: ' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' > "$tmp/public-message.txt" || true
others=$(git -C "$clone" log --format='%an <%ae>' "$from..$to" | LC_ALL=C sort -u | grep -vxF "$author_name <$author_email>" || true)
{
  cat "$tmp/public-message.txt"
  # Join an existing trailer block (GitHub's Co-authored-by lines after a
  # blank line), else start one.
  if ! grep -q '^$' "$tmp/public-message.txt" || ! tail -n 1 "$tmp/public-message.txt" | grep -qE '^[A-Za-z][A-Za-z-]*: '; then
    printf '\n'
  fi
  if [ -n "$others" ]; then
    printf '%s\n' "$others" | sed 's/^/Co-authored-by: /'
  fi
  printf 'Public-PR: #%s\n' "$pr"
} > "$tmp/message.txt"
commit=$(GIT_AUTHOR_NAME=$author_name GIT_AUTHOR_EMAIL=$author_email GIT_AUTHOR_DATE=$author_date \
  git -C "$root" commit-tree "$tree" -p "$base_sha" -F "$tmp/message.txt")
git -C "$root" update-ref -m "import-contrib: public PR #$pr" "refs/heads/$branch" "$commit" ""

echo "import-contrib: created $branch ($(git -C "$root" rev-parse --short "$commit")) on $base"
echo "  author: $author_name <$author_email>; public pull request #$pr"
echo "  $(git -C "$root" diff --shortstat "$base_sha" "$commit")"
echo
echo "Nothing was pushed. Next steps:"
echo "  git switch $branch && bash scripts/test.sh"
echo "  merge it into main; the next export carries it (its paths already match the public main)."
echo "  If main matched the public parent of $(git -C "$clone" rev-parse --short "$to"), confirm with:"
echo "  bash scripts/export-public.sh --check-merged $(git -C "$clone" rev-parse --short "$to") --private $branch $clone"
