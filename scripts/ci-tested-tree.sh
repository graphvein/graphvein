#!/usr/bin/env bash
# Says whether the tree of a commit on main already passed the full CI
# matrix, so the release workflow (.github/workflows/release.yml) can skip
# its own test run. Exercised offline by scripts/test-ci-tested-tree.sh.
#
# Usage: bash scripts/ci-tested-tree.sh [COMMIT]   (default HEAD)
#
# A tree counts as tested when a run of ci.yml concluded success and its
# jobs named in REQUIRED_JOBS below each concluded success (a skipped job
# does not count), and that run tested exactly the tree of COMMIT. Two
# kinds of run qualify:
#
# 1. A push run on main whose head is COMMIT.
# 2. A pull_request run of the pull request that was merged as COMMIT
#    (its merge_commit_sha is COMMIT, its base is main), on the pull
#    request's last head H, when
#    - the tree of H equals the tree of COMMIT (a squash-merge of an
#      up-to-date branch gives the same tree),
#    - COMMIT's first parent (main before the merge) is an ancestor of H,
#      so the base every earlier run of H merged with was in H already and
#      the tested merge tree is the tree of H,
#    - every pull request whose head is H has main as its base (a run of H
#      against another base would have tested another merge tree), and
#    - the run's head branch is the pull request's head branch.
#
# Anything else, including any failed API call, answers "not tested", and
# the release runs the tests itself. The script never fails because of the
# answer: it exits 0 and writes tested=true|false and tested_run=<run URL
# or empty> to $GITHUB_OUTPUT when that is set, and says why on stdout.
# Exit 2 is a usage error (no GITHUB_REPOSITORY, COMMIT not a commit).
#
# It reads the API through `gh api` with GH_TOKEN (the job's GITHUB_TOKEN
# with actions: read and contents: read is enough for a public repository).
# Bash 3.2 compatible; runs in Git Bash on Windows.
set -euo pipefail

# The required checks of the full matrix (docs/public/ci-plan.md).
REQUIRED_JOBS='postgres
main (windows-2025)
main (macos-15)'

repo=${GITHUB_REPOSITORY:-}
if [ -z "$repo" ]; then
  echo "ci-tested-tree: GITHUB_REPOSITORY is not set" >&2
  exit 2
fi
if ! commit=$(git rev-parse -q --verify "${1:-HEAD}^{commit}"); then
  echo "ci-tested-tree: ${1:-HEAD} is not a commit" >&2
  exit 2
fi
tree=$(git rev-parse "$commit^{tree}")
api="repos/$repo"

answer() {
  # answer true|false RUN_URL MESSAGE
  echo "ci-tested-tree: $3"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "tested=$1" >> "$GITHUB_OUTPUT"
    echo "tested_run=$2" >> "$GITHUB_OUTPUT"
  fi
  exit 0
}

# run_passed RUN_ID: every required job of the run's latest attempt
# concluded success.
run_passed() {
  local names
  names=$(gh api "$api/actions/runs/$1/jobs?per_page=100" \
    --jq '.jobs[] | select(.conclusion == "success") | .name') || return 1
  while IFS= read -r job; do
    printf '%s\n' "$names" | grep -qxF -- "$job" || return 1
  done <<EOF
$REQUIRED_JOBS
EOF
  return 0
}

run_url() {
  echo "https://github.com/$repo/actions/runs/$1"
}

# 1. A push run on main of COMMIT itself.
runs=$(gh api "$api/actions/workflows/ci.yml/runs?head_sha=$commit&event=push&status=success&per_page=20" \
  --jq '.workflow_runs[] | select(.head_branch == "main") | .id' || true)
for run in $runs; do
  case $run in '' | *[!0-9]*) continue ;; esac
  if run_passed "$run"; then
    answer true "$(run_url "$run")" "CI run $run on main tested $commit"
  fi
done

# 2. The merged pull request's head.
parent=$(git rev-parse -q --verify "$commit^1" || true)
if [ -z "$parent" ]; then
  answer false '' "no tested run found: $commit has no parent"
fi
prs=$(gh api "$api/commits/$commit/pulls" \
  --jq ".[] | select(.merged_at != null and .merge_commit_sha == \"$commit\" and .base.ref == \"main\") | \"\(.number) \(.head.sha) \(.head.ref)\"" || true)
while read -r number head ref; do
  [ -n "${number:-}" ] || continue
  case $head in *[!0-9a-f]*) continue ;; esac
  head_tree=$(gh api "$api/git/commits/$head" --jq '.tree.sha' || true)
  if [ "$head_tree" != "$tree" ]; then
    echo "ci-tested-tree: pull request #$number head $head has another tree"
    continue
  fi
  behind=$(gh api "$api/compare/$parent...$head" --jq '.behind_by' || true)
  if [ "$behind" != 0 ]; then
    echo "ci-tested-tree: pull request #$number head $head does not contain main before the merge ($parent)"
    continue
  fi
  bases=$(gh api "$api/commits/$head/pulls" --jq ".[] | select(.head.sha == \"$head\") | .base.ref" || echo '?')
  if [ -z "$bases" ] || printf '%s\n' "$bases" | grep -qvxF main; then
    echo "ci-tested-tree: head $head is (or may be) the head of a pull request into another branch"
    continue
  fi
  runs=$(gh api "$api/actions/workflows/ci.yml/runs?head_sha=$head&event=pull_request&status=success&per_page=20" \
    --jq ".workflow_runs[] | select(.head_branch == \"$ref\") | .id" || true)
  for run in $runs; do
    case $run in '' | *[!0-9]*) continue ;; esac
    if run_passed "$run"; then
      answer true "$(run_url "$run")" "CI run $run of pull request #$number tested head $head, whose tree equals $commit's"
    fi
  done
done <<EOF
$prs
EOF

answer false '' "no successful CI run with every required job found for the tree of $commit"
