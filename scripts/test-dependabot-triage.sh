#!/usr/bin/env bash
# Offline test of scripts/dependabot-triage.sh with a mock `gh`: the mock
# serves canned REST pages (api/<key>.<page>.json under the test folder,
# key = the path after repos/OWNER/REPO/ with "/" as "_"), applies the
# caller's --jq filter to each page like gh does, serves only the first
# page without --paginate, can end its lines with CRLF (MOCK_CRLF=1, as gh
# on Windows does) and records every write. Also checks that the committed
# list (.github/dependabot-pinned.json) is well formed, names existing
# directories and that each has an ignore entry in docs/public/dependabot.yml.
# Run by scripts/test.sh.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
if ! command -v jq > /dev/null 2>&1; then
  # The script runs only in the public repository's workflow (Ubuntu, jq
  # preinstalled); a developer machine without jq skips this test.
  echo "test-dependabot-triage: skipped (jq not installed)"
  exit 0
fi
src=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$src/.." && pwd)
t=$(mktemp -d)
trap 'rm -rf -- "$t"' EXIT

fail() {
  echo "test-dependabot-triage: FAIL: $*" >&2
  if [ -s "$t/out.txt" ]; then
    sed 's/^/  | /' "$t/out.txt" >&2
  fi
  exit 1
}

mkdir -p "$t/bin" "$t/api"
cat > "$t/bin/gh" <<'EOF'
#!/usr/bin/env bash
[ "${1:-}" = api ] || { echo "mock gh: only api is supported" >&2; exit 1; }
shift
paginate=0 method=GET path= filter= fields=
while [ $# -gt 0 ]; do
  case "$1" in
    --paginate) paginate=1 ;;
    --method) method=$2; shift ;;
    --jq) filter=$2; shift ;;
    -f) fields="$fields $(printf '%s' "$2" | tr '\n' '~')"; shift ;;
    *) path=$1 ;;
  esac
  shift
done
case "$path" in
  repos/owner/public/*) ;;
  *) echo "mock gh: unexpected path $path" >&2; exit 1 ;;
esac
if [ "$method" != GET ]; then
  printf '%s %s%s\n' "$method" "$path" "$fields" >> "$MOCK_DIR/calls.txt"
  echo '{}'
  exit 0
fi
key=${path#repos/owner/public/}
key=${key%%\?*}
key=$(printf '%s' "$key" | tr '/' '_')
n=1
while [ -f "$MOCK_DIR/api/$key.$n.json" ]; do
  if [ "${MOCK_CRLF:-0}" = 1 ]; then
    jq -r "$filter" "$MOCK_DIR/api/$key.$n.json" | sed 's/$/\r/'
  else
    jq -r "$filter" "$MOCK_DIR/api/$key.$n.json"
  fi
  [ "$paginate" = 1 ] || break
  n=$((n + 1))
done
EOF
chmod +x "$t/bin/gh"
export MOCK_DIR=$t
export PATH="$t/bin:$PATH"
export GITHUB_REPOSITORY=owner/public

cat > "$t/list.json" <<'EOF'
[
  {"directory": "scripts/oracle/a/next14", "ecosystem": "npm", "reason": "Reason A: corpus version."},
  {"directory": "scripts/oracle/b", "ecosystem": "cargo", "reason": "Reason B: old axum."}
]
EOF
dep='{"login": "dependabot[bot]", "type": "Bot"}'
# 1: all files pinned (two directories, second files page, a rename from a
#    pinned directory); 2: one file outside; 3: a person's PR touching only
#    a pinned directory; 4: Dependabot, pinned, already commented (reopened);
#    5: a lookalike directory name (scripts/oracle/bx) is not pinned;
#    6: a rename into a pinned directory from shipped code.
cat > "$t/api/pulls.1.json" <<EOF
[
  {"number": 1, "user": $dep, "title": "Bump next in /scripts/oracle/a/next14"},
  {"number": 2, "user": $dep, "title": "Bump zod\teverywhere"},
  {"number": 3, "user": {"login": "someone", "type": "User"}, "title": "Update pinned next"}
]
EOF
cat > "$t/api/pulls.2.json" <<EOF
[
  {"number": 4, "user": $dep, "title": "Bump axum"},
  {"number": 5, "user": $dep, "title": "Bump other"},
  {"number": 6, "user": $dep, "title": "Move a file"}
]
EOF
echo '[{"filename": "scripts/oracle/a/next14/package.json"}, {"filename": "scripts/oracle/a/next14/package-lock.json"}]' > "$t/api/pulls_1_files.1.json"
echo '[{"filename": "scripts/oracle/b/Cargo.lock", "previous_filename": "scripts/oracle/b/Cargo.old"}]' > "$t/api/pulls_1_files.2.json"
echo '[{"filename": "scripts/oracle/a/next14/package.json"}]' > "$t/api/pulls_2_files.1.json"
echo '[{"filename": "web/package.json"}]' > "$t/api/pulls_2_files.2.json"
echo '[{"filename": "scripts/oracle/a/next14/package.json"}]' > "$t/api/pulls_3_files.1.json"
echo '[{"filename": "scripts/oracle/b/Cargo.lock"}]' > "$t/api/pulls_4_files.1.json"
echo '[{"filename": "scripts/oracle/bx/Cargo.lock"}]' > "$t/api/pulls_5_files.1.json"
echo '[{"filename": "scripts/oracle/b/x.rs", "previous_filename": "internal/x.go"}]' > "$t/api/pulls_6_files.1.json"
echo '[{"user": {"login": "dependabot[bot]"}, "body": "Bumps next."}]' > "$t/api/issues_1_comments.1.json"
echo '[{"user": {"login": "someone"}, "body": "<!-- graphvein-dependabot-triage --> copied"}]' > "$t/api/issues_1_comments.2.json"
echo '[{"user": {"login": "dependabot[bot]"}, "body": "Bumps axum."}]' > "$t/api/issues_4_comments.1.json"
printf '%s\n' '[{"user": {"login": "github-actions[bot]"}, "body": "<!-- graphvein-dependabot-triage -->\nClosed by the Dependabot triage workflow"}]' > "$t/api/issues_4_comments.2.json"

check_list_mode() {
  rm -f "$t/calls.txt"
  bash "$src/dependabot-triage.sh" list "$t/list.json" > "$t/out.txt" 2>&1 || fail "list mode exited non-zero"
  [ ! -e "$t/calls.txt" ] || fail "list mode sent a write"
  grep -q '^pinned    #1 Bump next in /scripts/oracle/a/next14 -> scripts/oracle/a/next14 scripts/oracle/b$' "$t/out.txt" || fail "list: #1 not pinned (both file pages)"
  grep -q '^untouched #2 Bump zod everywhere (changes web/package.json, not pinned)$' "$t/out.txt" || fail "list: #2 mixed not untouched"
  grep -q '^untouched #3 .*(author someone)$' "$t/out.txt" || fail "list: #3 (a person's PR) not untouched"
  grep -q '^untouched #4 .*already has the triage comment' "$t/out.txt" || fail "list: #4 commented PR not untouched"
  grep -q '^untouched #5 .*scripts/oracle/bx/Cargo.lock, not pinned' "$t/out.txt" || fail "list: #5 lookalike directory matched"
  grep -q '^untouched #6 .*(changes internal/x.go, not pinned)$' "$t/out.txt" || fail "list: #6 rename from shipped code matched"
  grep -q '6 open pull request(s), 2 changing only pinned directories, 0 closed, 6 left open' "$t/out.txt" || fail "list: wrong summary"
  if grep -q $'\r' "$t/out.txt"; then fail "list: carriage return in output"; fi
}

# List mode: reports, writes nothing; the same with CRLF output from gh.
check_list_mode
MOCK_CRLF=1 check_list_mode

# Close mode (with CRLF): only #1 is commented and closed, comment first.
rm -f "$t/calls.txt"
MOCK_CRLF=1 bash "$src/dependabot-triage.sh" close "$t/list.json" > "$t/out.txt" 2>&1 || fail "close mode exited non-zero"
[ "$(wc -l < "$t/calls.txt" | tr -d ' ')" = 2 ] || fail "close: expected 2 write calls"
sed -n 1p "$t/calls.txt" | grep -q '^POST repos/owner/public/issues/1/comments body=<!-- graphvein-dependabot-triage -->~Closed by the Dependabot triage workflow' || fail "close: #1 comment"
sed -n 1p "$t/calls.txt" | grep -qF -- '- `scripts/oracle/a/next14`: Reason A: corpus version.~- `scripts/oracle/b`: Reason B: old axum.' || fail "close: #1 comment reasons"
if grep -q $'\r' "$t/calls.txt"; then fail "close: carriage return in a write"; fi
[ "$(sed -n 2p "$t/calls.txt")" = 'PATCH repos/owner/public/pulls/1 state=closed' ] || fail "close: #1 not closed after the comment"
grep -q '^closed    #1 ' "$t/out.txt" || fail "close: #1 not reported closed"
grep -q '6 open pull request(s), 2 changing only pinned directories, 1 closed, 5 left open' "$t/out.txt" || fail "close: wrong summary"

# Idempotence: once #1 is closed it is no longer listed, and a run sends
# nothing; if it is open again with the triage comment it is left alone.
rm -f "$t/calls.txt"
jq '[.[] | select(.number != 1)]' "$t/api/pulls.1.json" > "$t/p1" && mv "$t/p1" "$t/api/pulls.1.json"
bash "$src/dependabot-triage.sh" close "$t/list.json" > "$t/out.txt" 2>&1 || fail "second close run exited non-zero"
[ ! -e "$t/calls.txt" ] || fail "second close run sent a write"
grep -q '5 open pull request(s), 1 changing only pinned directories, 0 closed, 5 left open' "$t/out.txt" || fail "second run: wrong summary"

# A malformed list stops the run before any call.
for bad in \
  '[{"directory": "internal/server", "ecosystem": "gomod", "reason": "r"}]' \
  '[{"directory": "scripts/oracle/../web", "ecosystem": "npm", "reason": "r"}]' \
  '[{"directory": "scripts/oracle/a/", "ecosystem": "npm", "reason": "r"}]' \
  '[{"directory": "scripts/oracle/a", "ecosystem": "npm", "reason": ""}]' \
  '[{"directory": "scripts/oracle/a", "ecosystem": "npm", "reason": "r"}, {"directory": "scripts/oracle/a", "ecosystem": "npm", "reason": "s"}]' \
  '[]'; do
  printf '%s\n' "$bad" > "$t/bad.json"
  if bash "$src/dependabot-triage.sh" close "$t/bad.json" > "$t/out.txt" 2>&1; then fail "bad list accepted: $bad"; fi
  [ ! -e "$t/calls.txt" ] || fail "bad list: a write was sent"
done

# Usage errors.
if bash "$src/dependabot-triage.sh" merge "$t/list.json" > "$t/out.txt" 2>&1; then fail "bad mode accepted"; fi

# The committed list is well formed, names existing directories, and each
# has its ignore entry in docs/public/dependabot.yml.
rm -f "$t"/api/*.json
bash "$src/dependabot-triage.sh" list "$root/.github/dependabot-pinned.json" > "$t/out.txt" 2>&1 || fail "committed list rejected"
yml=$root/docs/public/dependabot.yml
jq -r '.[] | [.directory, .ecosystem] | @tsv' "$root/.github/dependabot-pinned.json" | tr -d '\r' > "$t/committed.tsv"
while IFS=$(printf '\t') read -r d eco; do
  [ -d "$root/$d" ] || fail "committed list names a missing directory: $d"
  # The entry: ecosystem line, directory line, then ignore with "*".
  awk -v eco="$eco" -v dir="/$d" '
    { sub(/\r$/, "") }
    /^  - package-ecosystem: / { e = ($3 == eco); d = 0; next }
    e && $1 == "directory:" && $2 == dir { d = 1; next }
    e && d && /dependency-name: "\*"/ { found = 1 }
    END { exit found ? 0 : 1 }' "$yml" || fail "docs/public/dependabot.yml has no $eco ignore entry for /$d"
done < "$t/committed.tsv"

echo "test-dependabot-triage: ok"
