#!/usr/bin/env bash
# Offline test of scripts/codeql-triage.sh with a mock `gh`: the mock
# prints canned open alerts (what the real --jq filter prints) and records
# every PATCH it receives. Also checks that the committed dismissal list
# (.github/codeql-dismissals.json) is well formed. Run by scripts/test.sh.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
if ! command -v jq > /dev/null 2>&1; then
  # The script runs only in the public repository's workflow (Ubuntu, jq
  # preinstalled); a developer machine without jq skips this test.
  echo "test-codeql-triage: skipped (jq not installed)"
  exit 0
fi
src=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$src/.." && pwd)
t=$(mktemp -d)
trap 'rm -rf -- "$t"' EXIT

fail() {
  echo "test-codeql-triage: FAIL: $*" >&2
  if [ -s "$t/out.txt" ]; then
    sed 's/^/  | /' "$t/out.txt" >&2
  fi
  exit 1
}

mkdir -p "$t/bin"
cat > "$t/bin/gh" <<'EOF'
#!/usr/bin/env bash
[ "${1:-}" = api ] || { echo "mock gh: only api is supported" >&2; exit 1; }
shift
if [ "${1:-}" = --method ]; then
  printf '%s\n' "$*" >> "$MOCK_DIR/calls.txt"
  echo '{}'
  exit 0
fi
cat "$MOCK_DIR/alerts.tsv"
EOF
chmod +x "$t/bin/gh"
export MOCK_DIR=$t
export PATH="$t/bin:$PATH"
export GITHUB_REPOSITORY=owner/public

cat > "$t/list.json" <<'EOF'
[
  {"rule": "go/path-injection", "path": "internal/a.go", "reason": "false positive", "comment": "False positive: loopback only."},
  {"rule": "js/reflected-xss", "path": "testdata/x.ts", "reason": "used in tests", "comment": "Used in tests: fixture input."}
]
EOF
tab=$(printf '\t')
{
  printf '11%sgo/path-injection%sinternal/a.go%s40\n' "$tab" "$tab" "$tab"
  printf '12%sgo/path-injection%sinternal/b.go%s7\n' "$tab" "$tab" "$tab"
  printf '13%sjs/reflected-xss%stestdata/x.ts%s3\n' "$tab" "$tab" "$tab"
  printf '14%sgo/command-injection%sinternal/a.go%s9\n' "$tab" "$tab" "$tab"
} > "$t/alerts.tsv"

# List mode: reports matches, dismisses nothing.
bash "$src/codeql-triage.sh" list "$t/list.json" > "$t/out.txt" 2>&1 || fail "list mode exited non-zero"
[ ! -e "$t/calls.txt" ] || fail "list mode sent a PATCH"
grep -q '^matches   #11 go/path-injection internal/a.go:40 -> false positive$' "$t/out.txt" || fail "list: #11 not matched"
grep -q '^open      #12 ' "$t/out.txt" || fail "list: #12 (same rule, other file) matched"
grep -q '^open      #14 ' "$t/out.txt" || fail "list: #14 (same file, other rule) matched"
grep -q '4 open alert(s), 2 matching an entry, 0 dismissed, 2 left open' "$t/out.txt" || fail "list: wrong summary"

# Dismiss mode: exactly the two matching alerts, with reason and comment.
bash "$src/codeql-triage.sh" dismiss "$t/list.json" > "$t/out.txt" 2>&1 || fail "dismiss mode exited non-zero"
[ "$(wc -l < "$t/calls.txt" | tr -d ' ')" = 2 ] || fail "dismiss: expected 2 PATCH calls"
grep -q 'repos/owner/public/code-scanning/alerts/11 -f state=dismissed -f dismissed_reason=false positive -f dismissed_comment=False positive: loopback only.' "$t/calls.txt" || fail "dismiss: #11 call"
grep -q 'code-scanning/alerts/13 .*dismissed_reason=used in tests' "$t/calls.txt" || fail "dismiss: #13 call"
grep -q 'alerts/12\|alerts/14' "$t/calls.txt" && fail "dismiss: an unmatched alert was dismissed"
grep -q '4 open alert(s), 2 matching an entry, 2 dismissed, 2 left open' "$t/out.txt" || fail "dismiss: wrong summary"

# A malformed entry stops the run before any call.
rm -f "$t/calls.txt"
echo '[{"rule": "x", "path": "y", "reason": "not a reason", "comment": "c"}]' > "$t/bad.json"
if bash "$src/codeql-triage.sh" dismiss "$t/bad.json" > "$t/out.txt" 2>&1; then fail "bad reason accepted"; fi
[ ! -e "$t/calls.txt" ] || fail "bad list: a PATCH was sent"

# Usage errors.
if bash "$src/codeql-triage.sh" frobnicate "$t/list.json" > "$t/out.txt" 2>&1; then fail "bad mode accepted"; fi

# The committed list is well formed and names files that exist.
: > "$t/alerts.tsv"
bash "$src/codeql-triage.sh" list "$root/.github/codeql-dismissals.json" > "$t/out.txt" 2>&1 || fail "committed list rejected"
missing=$(jq -r '.[].path' "$root/.github/codeql-dismissals.json" | tr -d '\r' | while read -r p; do [ -f "$root/$p" ] || echo "$p"; done)
[ -z "$missing" ] || fail "committed list names missing files: $missing"

echo "test-codeql-triage: ok"
