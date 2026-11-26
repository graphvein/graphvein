#!/usr/bin/env bash
# Offline regression checks; all repositories and mock artifacts are temporary.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
repo="$temporary/repo"
mkdir -p "$repo/scripts" "$repo/testdata"
cp scripts/check-fixture-ignores.sh scripts/fetch-grammars.sh "$repo/scripts/"
git init -q -b main "$repo"
git -C "$repo" config core.autocrlf false
git -C "$repo" config user.email fixture@example.invalid
git -C "$repo" config user.name Fixture
echo 'ignored.ts' > "$repo/.gitignore"
echo baseline > "$repo/testdata/kept.ts"
git -C "$repo" add .
git -C "$repo" commit -qm baseline
bash "$repo/scripts/check-fixture-ignores.sh"
echo ignored > "$repo/testdata/ignored.ts"
if bash "$repo/scripts/check-fixture-ignores.sh" >/dev/null 2>&1; then echo 'Guard missed ignored fixture' >&2; exit 1; fi
# Force-add only in this disposable repository to prove the guard catches it.
git -C "$repo" add -f testdata/ignored.ts
if bash "$repo/scripts/check-fixture-ignores.sh" >/dev/null 2>&1; then echo 'Guard missed tracked ignored fixture' >&2; exit 1; fi
git -C "$repo" rm -q -f testdata/ignored.ts
git -C "$repo" update-ref refs/remotes/origin/main HEAD
git -C "$repo" checkout -qb feature
echo change > "$repo/ordinary.txt"
git -C "$repo" add ordinary.txt
git -C "$repo" commit -qm ordinary
# Exercise the actual script the workflow's changes job runs.
cp scripts/ci-changes.sh "$temporary/changes.sh"
expect_output() {
  local actual
  actual=$(cat "$GITHUB_OUTPUT")
  if [[ "$actual" != "$1" ]]; then
    printf 'ci-changes output was:\n%s\nexpected:\n%s\n' "$actual" "$1" >&2
    exit 1
  fi
}
# Pin the event and the repository so the checks behave the same locally and
# inside any CI event, in the public repository too (its rules differ).
export GITHUB_REPOSITORY=owner/private
export GITHUB_EVENT_NAME=push
export GITHUB_REF=refs/heads/feature
export BEFORE=0000000000000000000000000000000000000000
export GITHUB_OUTPUT="$temporary/output"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=false'
# A Go-only change does not select the web UI check.
mkdir -p "$repo/internal/server"
echo 'package server' > "$repo/internal/server/server.go"
git -C "$repo" add internal
git -C "$repo" commit -qm go
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=false'
mkdir "$repo/spike"
echo change > "$repo/spike/sample.txt"
git -C "$repo" add spike
git -C "$repo" commit -qm spike
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=true\nskip_tests=false\nweb=false\nwindows=false'
# Changes to web/ or to the committed UI build select the web UI check.
mkdir "$repo/web"
echo '{}' > "$repo/web/package.json"
git -C "$repo" add web
git -C "$repo" commit -qm web
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=false'
git -C "$repo" checkout -qb static main
mkdir -p "$repo/internal/server/ui/static"
echo '<!doctype html>' > "$repo/internal/server/ui/static/index.html"
git -C "$repo" add internal
git -C "$repo" commit -qm static
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REF=refs/heads/static bash "$temporary/changes.sh")
expect_output $'spike=false\nskip_tests=false\nweb=true\nwindows=false'
# So do the server goldens the web tests import, the workflow and the
# check script and the vulnerability scan script (its web audit); each on
# its own branch from main.
for path in internal/server/testdata/status-fresh.golden.json .github/workflows/ci.yml scripts/check-web.sh scripts/vulncheck.sh; do
  branch="web-input-$(basename "$path")"
  git -C "$repo" checkout -qb "$branch" main
  mkdir -p "$repo/$(dirname "$path")"
  echo '{}' > "$repo/$path"
  git -C "$repo" add "$path"
  git -C "$repo" commit -qm "$path"
  : > "$GITHUB_OUTPUT"
  (cd "$repo" && GITHUB_REF="refs/heads/$branch" bash "$temporary/changes.sh")
  expect_output $'spike=false\nskip_tests=false\nweb=true\nwindows=false'
done
git -C "$repo" checkout -q feature
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_EVENT_NAME=workflow_dispatch bash "$temporary/changes.sh")
expect_output $'spike=false\nskip_tests=false\nweb=true\nwindows=true'
# A branch push runs the Windows leg only when the head commit message holds
# the [ci windows] marker; a marker on an earlier commit does not count.
git -C "$repo" commit -q --allow-empty -m 'Final revision' -m 'Ready for review [ci windows]'
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=true'
git -C "$repo" commit -q --allow-empty -m 'Checkpoint after the marker'
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=false'
# A similar but different marker does not count.
git -C "$repo" commit -q --allow-empty -m 'Checkpoint [ci win]'
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh")
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=false'
# A pull request checkout is a merge commit: the marker is read from the
# pushed head (second parent), not from the synthetic merge message.
feature_head=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" commit -q --allow-empty -m 'PR head [ci windows]'
pr_head=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" checkout -q --detach main
git -C "$repo" merge -q --no-ff "$pr_head" -m 'Merge pr_head into main'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/1/merge BEFORE=$(git rev-parse main) bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=true'
git -C "$repo" checkout -q feature
git -C "$repo" reset -q --hard "$feature_head"
git -C "$repo" update-ref -d refs/remotes/origin/main
: > "$GITHUB_OUTPUT"
(cd "$repo" && bash "$temporary/changes.sh" 2>/dev/null)
expect_output $'spike=true\nskip_tests=false\nweb=true\nwindows=false'

# A merge push to main skips repeated jobs only when the merge tree matches a
# CI-tested branch commit outside docs and that commit's successful run had a
# successful postgres job (the Linux test job; Windows is not required). The
# mock gh lists one successful run per commit in MOCK_TESTED ("sha:run"
# pairs). MOCK_JOBS holds one "run|job name|conclusion" line per job; the
# jobs lookup answers the script's own filter by reading the job name and
# conclusion it selects, so a filter for another job finds nothing.
mkdir "$temporary/ci-bin"
cat > "$temporary/ci-bin/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == api ]] || exit 1
case "$2" in
  */workflows/ci.yml/runs\?head_sha=*)
    sha=${2#*head_sha=}
    sha=${sha%%&*}
    for pair in $MOCK_TESTED; do
      if [[ "${pair%%:*}" == "$sha" ]]; then echo "${pair#*:}"; fi
    done
    ;;
  */runs/*/jobs\?*)
    [[ "${MOCK_JOBS_FAIL:-}" != 1 ]] || exit 1
    [[ "${3:-}" == --jq ]] || exit 1
    run=${2#*/runs/}
    run=${run%%/*}
    pattern='select\(\.name == "([^"]*)" and \.conclusion == "([^"]*)"\)'
    [[ "$4" =~ $pattern ]] || exit 1
    want="$run|${BASH_REMATCH[1]}|${BASH_REMATCH[2]}"
    n=0
    while IFS= read -r job; do
      if [[ "$job" == "$want" ]]; then n=$((n + 1)); fi
    done <<EOF_JOBS
${MOCK_JOBS:-}
EOF_JOBS
    echo "$n"
    ;;
  *) exit 1 ;;
esac
MOCK
chmod +x "$temporary/ci-bin/gh"
git -C "$repo" checkout -q main
main_before=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" checkout -qb merged
echo one > "$repo/code1.txt"
git -C "$repo" add code1.txt
git -C "$repo" commit -qm code1
code1=$(git -C "$repo" rev-parse HEAD)
echo two > "$repo/code2.txt"
git -C "$repo" add code2.txt
git -C "$repo" commit -qm code2
code2=$(git -C "$repo" rev-parse HEAD)
mkdir "$repo/docs"
echo note > "$repo/docs/note.md"
git -C "$repo" add docs
git -C "$repo" commit -qm docs
git -C "$repo" checkout -q main
git -C "$repo" merge -q --no-ff merged -m 'Merge branch merged'
run_merge_check() {
  : > "$GITHUB_OUTPUT"
  (cd "$repo" && PATH="$temporary/ci-bin:$PATH" GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_REPOSITORY=owner/repo BEFORE="$main_before" MOCK_TESTED="$1" MOCK_JOBS="$2" bash "$temporary/changes.sh" >/dev/null)
}
nl=$'\n'
# Tested commit (postgres succeeded, Windows ran too) followed only by docs:
# skip.
run_merge_check "$code2:12" "12|postgres|success${nl}12|main (windows-2025)|success"
expect_output $'spike=false\nskip_tests=true\nweb=false\nwindows=true'
# Linux-only tested commit (no [ci windows], Windows leg skipped): skip.
run_merge_check "$code2:12" "12|changes|success${nl}12|postgres|success${nl}12|main (windows-2025)|skipped"
expect_output $'spike=false\nskip_tests=true\nweb=false\nwindows=true'
# The run's postgres job was skipped (for example a merge-skip run): run.
run_merge_check "$code2:12" "12|changes|success${nl}12|postgres|skipped"
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# Only the Windows leg succeeded, postgres did not: run.
run_merge_check "$code2:12" "12|main (windows-2025)|success${nl}12|postgres|skipped"
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# Tested commit followed by a code commit: run.
run_merge_check "$code1:11" '11|postgres|success'
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# No successful run found: run.
run_merge_check '' ''
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# postgres passed only on an earlier commit; the later code commit is
# untested: run.
run_merge_check "$code2:12 $code1:11" '11|postgres|success'
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# The last code commit has two successful runs and only the second had a
# successful postgres job: skip.
run_merge_check "$code2:21 $code2:22" "21|postgres|skipped${nl}22|postgres|success"
expect_output $'spike=false\nskip_tests=true\nweb=false\nwindows=true'
# A gh failure while listing jobs counts as no tested run: run.
MOCK_JOBS_FAIL=1 run_merge_check "$code2:12" '12|postgres|success' 2>/dev/null
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# A malformed run id is ignored: run.
run_merge_check "$code2:bad" 'bad|postgres|success'
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'

# A push to main that changes only site/ (outside docs and Markdown) runs
# the Linux job only; a site change together with other code keeps Windows.
git -C "$repo" checkout -q main
site_before=$(git -C "$repo" rev-parse HEAD)
mkdir -p "$repo/site"
echo 'package main' > "$repo/site/main.go"
echo 'notes' > "$repo/site/README.md"
git -C "$repo" add site
git -C "$repo" commit -qm site
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REF=refs/heads/main BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=false'
echo 'package other' > "$repo/other.go"
git -C "$repo" add other.go
git -C "$repo" commit -qm 'site and other code'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REF=refs/heads/main BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# A branch push of a site-only change keeps the branch rule (no marker, no
# Windows).
git -C "$repo" checkout -qb site-branch "$site_before"
mkdir -p "$repo/site"
echo 'package main // edit' > "$repo/site/main.go"
git -C "$repo" add site
git -C "$repo" commit -qm 'site branch'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REF=refs/heads/site-branch BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=false'

# In the public repository (GITHUB_REPOSITORY=graphvein/graphvein) every
# pull request and push runs the Windows leg without a marker, a site-only
# push to main keeps it, and a merge to main is never skipped and asks gh
# nothing. Any other repository name, a fork's included, keeps the rules
# above.
public=graphvein/graphvein
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REPOSITORY=$public GITHUB_REF=refs/heads/site-branch BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REPOSITORY=someone/graphvein GITHUB_REF=refs/heads/site-branch BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=false'
site_branch_head=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" checkout -q --detach "$site_before"
git -C "$repo" merge -q --no-ff "$site_branch_head" -m 'Merge site-branch into main'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REPOSITORY=$public GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/2/merge BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REPOSITORY=$public GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main BEFORE="$site_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
# The same merge in a private repository, with the branch head tested, is
# skipped; in the public repository it runs and gh is never called.
git -C "$repo" checkout -q main
public_main_before=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" checkout -qb public-merge
echo three > "$repo/code3.txt"
git -C "$repo" add code3.txt
git -C "$repo" commit -qm code3
code3=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" checkout -q main
git -C "$repo" merge -q --no-ff public-merge -m 'Merge public-merge into main'
main_before=$public_main_before
run_merge_check "$code3:31" '31|postgres|success'
expect_output $'spike=false\nskip_tests=true\nweb=false\nwindows=true'
mkdir "$temporary/no-gh"
cat > "$temporary/no-gh/gh" <<'MOCK'
#!/usr/bin/env bash
echo called >> "$GH_CALLS"
exit 1
MOCK
chmod +x "$temporary/no-gh/gh"
: > "$GITHUB_OUTPUT"
(cd "$repo" && PATH="$temporary/no-gh:$PATH" GH_CALLS="$temporary/gh-calls" GITHUB_REPOSITORY=$public GITHUB_REF=refs/heads/main BEFORE="$public_main_before" bash "$temporary/changes.sh" >/dev/null)
expect_output $'spike=false\nskip_tests=false\nweb=false\nwindows=true'
if [[ -e "$temporary/gh-calls" ]]; then echo 'The public repository called gh' >&2; exit 1; fi
# Dispatch selects the same jobs in both repositories.
: > "$GITHUB_OUTPUT"
(cd "$repo" && GITHUB_REPOSITORY=$public GITHUB_EVENT_NAME=workflow_dispatch bash "$temporary/changes.sh")
expect_output $'spike=false\nskip_tests=false\nweb=true\nwindows=true'

mkdir "$temporary/mock-bin" "$temporary/artifacts"
export MOCK_ARTIFACTS="$temporary/artifacts"
for name in manifest.yaml NOTICES.md UPSTREAM_NOTICES.txt LLVM-LICENSE.txt WASI-SDK-NOTICES.txt; do echo mock > "$MOCK_ARTIFACTS/$name"; done
for language in html angular c_sharp dart javascript typescript tsx; do
  for name in "wasitter-$language.wasm" "$language-LICENSES.txt" "$language-build.json"; do echo 'mock test bytes, not WASM' > "$MOCK_ARTIFACTS/$name"; done
done
(cd "$MOCK_ARTIFACTS" && sha256sum * > SHA256SUMS)
cat > "$temporary/mock-bin/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1 $2" == 'run view' ]]; then
  echo 'success|Grammar assets'
elif [[ "$1 $2" == 'run download' ]]; then
  while [[ "$1" != --dir ]]; do shift; done
  cp "$MOCK_ARTIFACTS/"* "$2/"
else
  exit 1
fi
MOCK
chmod +x "$temporary/mock-bin/gh"
export PATH="$temporary/mock-bin:$PATH"
bash "$repo/scripts/fetch-grammars.sh" 101 >/dev/null
[[ -f "$repo/grammars/runs/run-101/wasitter-angular.wasm" ]]
if bash "$repo/scripts/fetch-grammars.sh" 101 >/dev/null 2>&1; then echo 'Fetch overwrote run' >&2; exit 1; fi
echo tampered >> "$MOCK_ARTIFACTS/wasitter-angular.wasm"
if bash "$repo/scripts/fetch-grammars.sh" 102 >/dev/null 2>&1; then echo 'Fetch accepted bad checksum' >&2; exit 1; fi
[[ ! -e "$repo/grammars/runs/run-102" ]]
printf 'T005a offline script regression checks passed.\n'
