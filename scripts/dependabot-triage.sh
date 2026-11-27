#!/usr/bin/env bash
# Lists the open pull requests of $GITHUB_REPOSITORY and, in close mode,
# closes the Dependabot pull requests that change only directories pinned
# on purpose, with one comment giving the reviewed reason of each touched
# directory. The reviewed list is .github/dependabot-pinned.json: one entry
# per directory (a path under scripts/oracle/, its Dependabot ecosystem and
# a public reason of at most 500 characters).
#
# A pull request is closed only when it is authored by dependabot[bot] and
# every file it changes (renames: both names) is inside a pinned directory.
# Every other pull request is printed and left untouched. Nothing is ever
# merged and no branch is deleted (Dependabot removes its own branch). A
# pull request that already carries this workflow's comment is not
# commented again and is left as it is: it was reopened after triage, or an
# earlier close failed, and either way a person decides.
#
# Usage: bash scripts/dependabot-triage.sh list|close [LIST]
#
# Run by .github/workflows/dependabot-triage.yml with GH_TOKEN set to a
# token with pull-requests: write; needs gh and jq. Tested offline with a
# mock gh by scripts/test-dependabot-triage.sh. Bash 3.2 compatible.
set -euo pipefail

mode=${1:-}
list=${2:-.github/dependabot-pinned.json}
case "$mode" in
  list|close) ;;
  *) echo "usage: dependabot-triage.sh list|close [LIST]" >&2; exit 2 ;;
esac
repo=${GITHUB_REPOSITORY:-}
if [ -z "$repo" ]; then
  echo "dependabot-triage: GITHUB_REPOSITORY is not set" >&2
  exit 2
fi
[ -f "$list" ] || { echo "dependabot-triage: $list not found" >&2; exit 2; }

marker='<!-- graphvein-dependabot-triage -->'
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

# A malformed list stops the run before anything is read or written. Only
# oracle tooling can be pinned, so a pull request touching shipped code is
# never closed whatever the list says.
if ! jq -e 'type == "array" and length > 0 and
      ([.[].directory] | length == (unique | length)) and
      all(.[];
        (.directory | type) == "string" and
        (.directory | test("^scripts/oracle/[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$")) and
        (.directory | test("(^|/)\\.\\.?(/|$)") | not) and
        (.ecosystem | type) == "string" and (.ecosystem | length) > 0 and
        (.reason | type) == "string" and (.reason | length) > 0 and
        (.reason | length) <= 500 and (.reason | test("[\t\n\r]") | not))' \
      "$list" > /dev/null; then
  echo "dependabot-triage: $list needs unique entries with a directory under scripts/oracle/, an ecosystem and a one-line reason of 1-500 characters" >&2
  exit 1
fi
# jq and gh on Windows end lines with CRLF; the carriage returns are dropped.
jq -r '.[] | [.directory, .reason] | @tsv' "$list" | tr -d '\r' > "$tmp/pinned.tsv"

# Open pull requests, one per line: number TAB author login TAB title.
gh api --paginate "repos/$repo/pulls?state=open&per_page=100" \
  --jq '.[] | [.number, .user.login, (.title | gsub("[\t\r\n]"; " "))] | @tsv' \
  | tr -d '\r' > "$tmp/pulls.tsv"

# pinned_dir FILE prints the pinned directory holding FILE, or nothing.
pinned_dir() {
  local f=$1 d
  while IFS=$(printf '\t') read -r d _; do
    case "$f" in
      "$d"/*) printf '%s\n' "$d"; return 0 ;;
    esac
  done < "$tmp/pinned.tsv"
  return 0
}

total=0
candidates=0
closed=0
while IFS=$(printf '\t') read -r number author title; do
  [ -n "$number" ] || continue
  total=$((total + 1))
  if [ "$author" != 'dependabot[bot]' ]; then
    printf 'untouched #%s %s (author %s)\n' "$number" "$title" "$author"
    continue
  fi
  gh api --paginate "repos/$repo/pulls/$number/files?per_page=100" \
    --jq '.[] | .filename, (.previous_filename // empty)' \
    | tr -d '\r' > "$tmp/files.txt"
  if [ ! -s "$tmp/files.txt" ]; then
    printf 'untouched #%s %s (no changed files)\n' "$number" "$title"
    continue
  fi
  : > "$tmp/dirs.txt"
  outside=
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    d=$(pinned_dir "$f")
    if [ -z "$d" ]; then
      outside=$f
      break
    fi
    grep -qxF -- "$d" "$tmp/dirs.txt" || printf '%s\n' "$d" >> "$tmp/dirs.txt"
  done < "$tmp/files.txt"
  if [ -n "$outside" ]; then
    printf 'untouched #%s %s (changes %s, not pinned)\n' "$number" "$title" "$outside"
    continue
  fi
  candidates=$((candidates + 1))
  dirs=$(tr '\n' ' ' < "$tmp/dirs.txt" | sed 's/ $//')
  gh api --paginate "repos/$repo/issues/$number/comments?per_page=100" \
    --jq '.[] | select(.user.login == "github-actions[bot]") | .body' \
    | tr -d '\r' > "$tmp/comments.txt"
  if grep -qF -- "$marker" "$tmp/comments.txt"; then
    printf 'untouched #%s %s (already has the triage comment; reopened or an earlier close failed)\n' "$number" "$title"
    continue
  fi
  if [ "$mode" = list ]; then
    printf 'pinned    #%s %s -> %s\n' "$number" "$title" "$dirs"
    continue
  fi
  body="$marker
Closed by the Dependabot triage workflow (\`.github/workflows/dependabot-triage.yml\`): every file this pull request changes is in a directory whose versions are pinned on purpose. This pull request is not merged here; a fix that keeps the pinned version is taken in the maintainer's repository and arrives with the next export.
"
  while IFS= read -r d; do
    reason=$(awk -F'\t' -v d="$d" '$1 == d { print $2; exit }' "$tmp/pinned.tsv")
    body="$body
- \`$d\`: $reason"
  done < "$tmp/dirs.txt"
  gh api --method POST "repos/$repo/issues/$number/comments" -f "body=$body" > /dev/null
  gh api --method PATCH "repos/$repo/pulls/$number" -f state=closed > /dev/null
  closed=$((closed + 1))
  printf 'closed    #%s %s -> %s\n' "$number" "$title" "$dirs"
done < "$tmp/pulls.tsv"

printf 'dependabot-triage: %d open pull request(s), %d changing only pinned directories, %d closed, %d left open\n' \
  "$total" "$candidates" "$closed" "$((total - closed))"
