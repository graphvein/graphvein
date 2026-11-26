#!/usr/bin/env bash
# Decides which CI jobs run. Called by the `changes` job in
# .github/workflows/ci.yml and exercised offline by scripts/test-t005a.sh.
# Writes spike=true|false, skip_tests=true|false, web=true|false and
# windows=true|false to $GITHUB_OUTPUT. web selects the web UI drift check
# (scripts/check-web.sh) and the web dependency audit (scripts/vulncheck.sh
# web). windows selects the Windows leg of the main job.
#
# GITHUB_REPOSITORY (set by GitHub Actions) selects the rules. The same
# workflow runs in the public repository and in the maintainer's working
# repository, which receives no outside contributions and runs fewer jobs:
#
# - In the public repository (graphvein/graphvein) every pull request and
#   every push runs every test job: windows is always true and nothing is
#   skipped (docs/public/ci-plan.md).
# - In any other repository the Windows leg runs on main, on schedule and
#   dispatch, and on a branch push or pull request only when the head
#   commit message contains the marker [ci windows]. A push to main whose
#   code changes (outside docs and Markdown) are all under site/, the
#   website generator (ADR 0091), runs the Linux job only. A merge to main
#   whose code equals an already tested branch commit is not re-tested.
set -euo pipefail
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}"
event=${GITHUB_EVENT_NAME:-}
ref=${GITHUB_REF:-}
spike=false
skip_tests=false
web=false
windows=false
windows_marker='[ci windows]'
public=false
if [[ "${GITHUB_REPOSITORY:-}" == 'graphvein/graphvein' ]]; then
  public=true
fi
case "$event" in
  workflow_dispatch)
    web=true
    windows=true
    ;;
  schedule)
    web=true
    windows=true
    # Weekly full matrix (including macOS) only when code changed recently.
    recent=$(git log --since='8 days ago' --format=%H -- . ':(exclude)docs' ':(exclude,glob)**/*.md' || echo unknown)
    if [[ -z "$recent" ]]; then
      skip_tests=true
      echo 'No code changes in the last 8 days; skipping the scheduled run.'
    fi
    ;;
  *)
    if [[ "$public" == true || ( "$event" == 'push' && "$ref" == 'refs/heads/main' ) ]]; then
      windows=true
    else
      # A pull request checkout is a merge commit; its second parent is the
      # pushed head. A failed lookup leaves Windows off; main still runs it.
      head=HEAD
      if [[ "$event" == 'pull_request' ]] && git rev-parse -q --verify 'HEAD^2' >/dev/null; then
        head='HEAD^2'
      fi
      message=$(git log -1 --format=%B "$head" || true)
      case "$message" in
        *"$windows_marker"*)
          windows=true
          echo "Head commit requests the Windows leg ($windows_marker)."
          ;;
      esac
    fi
    base=${BEFORE:-}
    if [[ -z "$base" || "$base" =~ ^0+$ ]]; then
      # First branch push: compare only changes since main diverged.
      base=$(git merge-base HEAD origin/main || true)
    fi
    # A failed diff counts as a change, so the job runs.
    if [[ -z "$base" ]]; then
      spike=true
      web=true
    else
      if ! changed=$(git diff --name-only "$base" HEAD -- spike/) || [[ -n "$changed" ]]; then
        spike=true
      fi
      # internal/server/testdata/ holds the status goldens the web tests
      # import, so a golden change must rerun them. scripts/vulncheck.sh holds
      # the web dependency audit that runs with the web check.
      if ! changed=$(git diff --name-only "$base" HEAD -- web/ internal/server/ui/static/ internal/server/testdata/ scripts/check-web.sh scripts/vulncheck.sh .github/workflows/ci.yml) || [[ -n "$changed" ]]; then
        web=true
      fi
    fi
    # Site-only changes on main skip the Windows leg (ADR 0091).
    if [[ "$public" != true && "$event" == 'push' && "$ref" == 'refs/heads/main' && -n "$base" ]]; then
      if code=$(git diff --name-only "$base" HEAD -- . ':(exclude)docs' ':(exclude,glob)**/*.md') && [[ -n "$code" ]] && ! printf '%s\n' "$code" | grep -qv '^site/'; then
        windows=false
        echo 'Only site/ changed outside docs; skipping the Windows leg.'
      fi
    fi
    # A merge to main whose code equals an already CI-tested branch commit
    # (docs-only commits after it are allowed) is not re-tested. A branch run
    # counts only if its Linux test job (postgres) ran and succeeded; a run
    # whose postgres job was skipped (for example an earlier merge-skip run)
    # does not count. A Windows leg is not required: Windows coverage rests on
    # the last [ci windows] run before the merge. Any
    # lookup failure falls back to running everything, Windows included.
    # The public repository never skips: its pull requests are tested on the
    # full matrix, and a push to main re-tests the real main tree.
    if [[ "$public" != true && "$event" == 'push' && "$ref" == 'refs/heads/main' ]] && git rev-parse -q --verify 'HEAD^2' >/dev/null; then
      tested=''
      repo_api="repos/${GITHUB_REPOSITORY:-}/actions"
      for sha in $(git rev-list --max-count=20 'HEAD^1..HEAD^2' || true); do
        for run in $(gh api "$repo_api/workflows/ci.yml/runs?head_sha=$sha&status=success&per_page=10" --jq '.workflow_runs[].id' || true); do
          [[ "$run" =~ ^[0-9]+$ ]] || continue
          n=$(gh api "$repo_api/runs/$run/jobs?per_page=100" --jq '[.jobs[] | select(.name == "postgres" and .conclusion == "success")] | length' || echo 0)
          if [[ "$n" =~ ^[0-9]+$ && "$n" -gt 0 ]]; then
            tested="$sha"
            break 2
          fi
        done
      done
      if [[ -z "$tested" ]]; then
        echo 'No successful CI run with a successful postgres job found on the merged branch; running full jobs.'
      elif code=$(git diff --name-only "$tested" HEAD -- . ':(exclude)docs' ':(exclude,glob)**/*.md') && [[ -z "$code" ]]; then
        skip_tests=true
        echo "Merge tree matches CI-tested commit $tested outside docs; skipping repeated jobs."
      else
        echo "Code differs from CI-tested commit $tested; running full jobs."
      fi
    fi
    ;;
esac
echo "spike=$spike" >> "$GITHUB_OUTPUT"
echo "skip_tests=$skip_tests" >> "$GITHUB_OUTPUT"
echo "web=$web" >> "$GITHUB_OUTPUT"
echo "windows=$windows" >> "$GITHUB_OUTPUT"
