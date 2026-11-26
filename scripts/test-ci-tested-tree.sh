#!/usr/bin/env bash
# Offline test of scripts/ci-tested-tree.sh with a mock `gh`. The mock
# answers `gh api PATH` from canned files and ignores --jq: each canned
# answer is what the real filter prints, so this test covers the
# decisions; the filters were checked against the public repository's API
# (the T053 task record lists the commits). Run by scripts/test.sh.
#
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail
src=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
t=$(mktemp -d)
trap 'rm -rf -- "$t"' EXIT

fail() {
  echo "test-ci-tested-tree: FAIL: $*" >&2
  if [ -s "$t/out.txt" ]; then
    sed 's/^/  | /' "$t/out.txt" >&2
  fi
  exit 1
}

# The mock gh: `gh api PATH ...` prints $t/mock/<PATH with every character
# outside [A-Za-z0-9._-] replaced by _>, or fails like an API error.
mkdir -p "$t/bin" "$t/mock"
cat > "$t/bin/gh" <<'EOF'
#!/usr/bin/env bash
[ "${1:-}" = api ] || { echo "mock gh: only api is supported" >&2; exit 1; }
key=$(printf '%s' "$2" | tr -c 'A-Za-z0-9._-' '_')
[ -f "$MOCK_DIR/$key" ] || { echo "mock gh: HTTP 404 for $2" >&2; exit 1; }
cat "$MOCK_DIR/$key"
EOF
chmod +x "$t/bin/gh"
export MOCK_DIR=$t/mock
export PATH="$t/bin:$PATH"
export GITHUB_REPOSITORY=owner/public
respond() {
  # respond PATH TEXT: the mock prints TEXT (and a newline) for PATH.
  printf '%s\n' "$2" > "$MOCK_DIR/$(printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_')"
}
reset_mock() { rm -f "$MOCK_DIR"/*; }

# A small main: P, then T, the squash-merge commit to release.
repo=$t/repo
git init -q -b main "$repo"
git -C "$repo" config core.autocrlf false
git -C "$repo" config commit.gpgsign false
git -C "$repo" config user.name Fixture
git -C "$repo" config user.email fixture@example.invalid
echo one > "$repo/a.txt"
git -C "$repo" add a.txt
git -C "$repo" commit -qm P
P=$(git -C "$repo" rev-parse HEAD)
echo two > "$repo/a.txt"
git -C "$repo" commit -qam T
T=$(git -C "$repo" rev-parse HEAD)
TREE=$(git -C "$repo" rev-parse 'HEAD^{tree}')
H=1111111111111111111111111111111111111111
OTHER_TREE=2222222222222222222222222222222222222222
ALL_JOBS='changes
postgres
main (windows-2025)
main (macos-15)'

# check WANT_TESTED WANT_RUN: run the script on T, compare its outputs.
check() {
  : > "$t/output"
  (cd "$repo" && GITHUB_OUTPUT=$t/output bash "$src/ci-tested-tree.sh" "$T") > "$t/out.txt" 2>&1 ||
    fail "exit $? (want 0)"
  got=$(tr '\n' '|' < "$t/output")
  want="tested=$1|tested_run=$2|"
  [ "$got" = "$want" ] || fail "outputs $got, want $want"
}
url() { echo "https://github.com/owner/public/actions/runs/$1"; }
push_runs="repos/owner/public/actions/workflows/ci.yml/runs?head_sha=$T&event=push&status=success&per_page=20"
pulls_t="repos/owner/public/commits/$T/pulls"
pulls_h="repos/owner/public/commits/$H/pulls"
head_commit="repos/owner/public/git/commits/$H"
compare="repos/owner/public/compare/$P...$H"
pr_runs="repos/owner/public/actions/workflows/ci.yml/runs?head_sha=$H&event=pull_request&status=success&per_page=20"
jobs() { echo "repos/owner/public/actions/runs/$1/jobs?per_page=100"; }

# Every API call fails: not tested, and the script still succeeds.
check false ''

# 1. A push run on main whose required jobs all passed.
respond "$push_runs" 101
respond "$(jobs 101)" "$ALL_JOBS"
check true "$(url 101)"
# A push run whose Windows leg was skipped (only successful jobs are
# listed) does not count, nor one whose job list cannot be read.
respond "$(jobs 101)" 'changes
postgres
main (macos-15)'
check false ''
rm -f "$MOCK_DIR/$(jobs 101 | tr -c 'A-Za-z0-9._-' '_')"
check false ''
# A later successful run of the same commit counts.
respond "$push_runs" '101
102'
respond "$(jobs 102)" "$ALL_JOBS"
check true "$(url 102)"
# Garbage in a run id is skipped.
respond "$push_runs" '../x'
check false ''

# 2. The merged pull request's head.
reset_mock
respond "$pulls_t" "7 $H export/T001"
respond "$head_commit" "$TREE"
respond "$compare" 0
respond "$pulls_h" main
respond "$pr_runs" 201
respond "$(jobs 201)" "$ALL_JOBS"
check true "$(url 201)"
# The head's tree differs from the release commit's.
respond "$head_commit" "$OTHER_TREE"
check false ''
respond "$head_commit" "$TREE"
# The head does not contain main before the merge: its runs merged it
# with an older base, so the tested tree may differ.
respond "$compare" 1
check false ''
respond "$compare" 0
# The head is also the head of a pull request into another branch, or
# that cannot be read.
respond "$pulls_h" 'main
release'
check false ''
rm -f "$MOCK_DIR/$(printf '%s' "$pulls_h" | tr -c 'A-Za-z0-9._-' '_')"
check false ''
respond "$pulls_h" main
# The head's run lacks a required job.
respond "$(jobs 201)" 'postgres
main (windows-2025)'
check false ''
respond "$(jobs 201)" "$ALL_JOBS"
# A second merged pull request listed first that fails a condition does not
# hide the one that passes.
respond "$pulls_t" "6 3333333333333333333333333333333333333333 export/T000
7 $H export/T001"
check true "$(url 201)"
# A head that is not a commit id is skipped.
respond "$pulls_t" "7 not-a-sha export/T001"
check false ''

# A root commit has no main before it: only a push run can count.
reset_mock
root_commit=$(git -C "$repo" rev-list --max-parents=0 HEAD)
: > "$t/output"
(cd "$repo" && GITHUB_OUTPUT=$t/output bash "$src/ci-tested-tree.sh" "$root_commit") > "$t/out.txt" 2>&1 || fail "root commit: exit $?"
[ "$(tr '\n' '|' < "$t/output")" = 'tested=false|tested_run=|' ] || fail "root commit outputs"

# Usage errors exit 2.
set +e
(cd "$repo" && GITHUB_REPOSITORY='' bash "$src/ci-tested-tree.sh" "$T") > "$t/out.txt" 2>&1
[ $? = 2 ] || fail "no GITHUB_REPOSITORY: want exit 2"
(cd "$repo" && bash "$src/ci-tested-tree.sh" no-such-commit) > "$t/out.txt" 2>&1
[ $? = 2 ] || fail "bad commit: want exit 2"
set -e

echo "test-ci-tested-tree: ok"
