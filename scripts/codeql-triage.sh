#!/usr/bin/env bash
# Lists the open code scanning alerts of $GITHUB_REPOSITORY and, in
# dismiss mode, dismisses those that match an entry of the reviewed
# dismissal list (.github/codeql-dismissals.json). An entry names a rule id
# and an exact file path, the dismissal reason ("false positive", "used in
# tests" or "won't fix") and a comment of at most 280 characters saying
# why. Alerts without a matching entry are printed and left open, so a new
# finding is never dismissed by an old entry for another rule or file.
#
# Usage: bash scripts/codeql-triage.sh list|dismiss [LIST]
#
# Run by .github/workflows/codeql-triage.yml with GH_TOKEN set to a token
# with security-events: write; needs gh and jq. Tested offline with a mock
# gh by scripts/test-codeql-triage.sh. Bash 3.2 compatible.
set -euo pipefail

mode=${1:-}
list=${2:-.github/codeql-dismissals.json}
case "$mode" in
  list|dismiss) ;;
  *) echo "usage: codeql-triage.sh list|dismiss [LIST]" >&2; exit 2 ;;
esac
repo=${GITHUB_REPOSITORY:-}
if [ -z "$repo" ]; then
  echo "codeql-triage: GITHUB_REPOSITORY is not set" >&2
  exit 2
fi
[ -f "$list" ] || { echo "codeql-triage: $list not found" >&2; exit 2; }

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

# The reviewed entries, one per line: rule TAB path TAB reason TAB comment.
# A malformed entry stops the run before anything is dismissed.
if ! jq -e 'type == "array" and all(.[];
      (.rule | type) == "string" and (.path | type) == "string" and
      (.reason | IN("false positive", "used in tests", "won'"'"'t fix")) and
      (.comment | type) == "string" and (.comment | length) > 0 and
      (.comment | length) <= 280)' "$list" > /dev/null; then
  echo "codeql-triage: $list has an entry without rule, path, a valid reason or a comment of 1-280 characters" >&2
  exit 1
fi
# jq and gh on Windows end lines with CRLF; the carriage returns are dropped.
jq -r '.[] | [.rule, .path, .reason, .comment] | @tsv' "$list" | tr -d '\r' > "$tmp/entries.tsv"

# Open alerts, one per line: number TAB rule TAB path TAB line.
gh api --paginate "repos/$repo/code-scanning/alerts?state=open&per_page=100" \
  --jq '.[] | [.number, .rule.id, .most_recent_instance.location.path, .most_recent_instance.location.start_line] | @tsv' \
  | tr -d '\r' > "$tmp/alerts.tsv"

total=0
matched=0
dismissed=0
while IFS=$(printf '\t') read -r number rule path line; do
  [ -n "$number" ] || continue
  total=$((total + 1))
  entry=$(awk -F'\t' -v r="$rule" -v p="$path" '$1 == r && $2 == p { print; exit }' "$tmp/entries.tsv")
  if [ -z "$entry" ]; then
    printf 'open      #%s %s %s:%s (no entry)\n' "$number" "$rule" "$path" "$line"
    continue
  fi
  matched=$((matched + 1))
  reason=$(printf '%s\n' "$entry" | cut -f3)
  comment=$(printf '%s\n' "$entry" | cut -f4-)
  if [ "$mode" = list ]; then
    printf 'matches   #%s %s %s:%s -> %s\n' "$number" "$rule" "$path" "$line" "$reason"
    continue
  fi
  gh api --method PATCH "repos/$repo/code-scanning/alerts/$number" \
    -f state=dismissed -f "dismissed_reason=$reason" -f "dismissed_comment=$comment" > /dev/null
  dismissed=$((dismissed + 1))
  printf 'dismissed #%s %s %s:%s -> %s\n' "$number" "$rule" "$path" "$line" "$reason"
done < "$tmp/alerts.tsv"

printf 'codeql-triage: %d open alert(s), %d matching an entry, %d dismissed, %d left open\n' \
  "$total" "$matched" "$dismissed" "$((total - matched))"
