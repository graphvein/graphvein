# CI plan for the public repository

Status: the workflow, `ci-changes.sh` and release parts are implemented
(ADR 0080) and the CLA texts are in force (version 1.0); installing CLA
Assistant and branch protection are still to do. The private
repository's CI behaves as before: the public rules apply only where
`github.repository` is `graphvein/graphvein`.

## What changes when the repository is public

GitHub Actions minutes on standard runners are free for public repositories.
The private repository's CI was shaped by a minutes budget (its cost notes,
`docs/ci-budget.md`, stay private): Windows runs only on demand, macOS only
on manual dispatch, and a merge to `main` skips tests already run on the
branch. In the public repository those rules cost contributors coverage and
buy nothing, so the plan drops most of them. The Postgres job does not
change.

## Workflow changes (`.github/workflows/ci.yml`)

The same workflow files run in the public repository and in the
maintainer's working repository. They select the public rules by
`github.repository == 'graphvein/graphvein'` (in `ci.yml` and
`public-tree.yml`) and by `GITHUB_REPOSITORY` (in `scripts/ci-changes.sh`);
any other repository, a fork included, keeps the working repository's
rules: Windows only on `main` and on branch heads marked `[ci windows]`,
macOS only on manual dispatch, and a merge to `main` whose code was
already tested is not re-run. Implemented:

1. **Full matrix on every pull request and every push to `main`.** In the
   public repository the `main` job's matrix is always
   `["windows-2025", "macos-15"]` and `scripts/ci-changes.sh` sets
   `windows=true` for every event, so both legs run on every run that is
   not skipped, with no marker. Nothing is skipped there: no merge skip
   and no site-only skip of the Windows leg.
2. **Postgres job unchanged.** `postgres` stays the only Linux test job: the
   full suite with `GRAPHVEIN_TEST_POSTGRES_URL` and
   `GRAPHVEIN_REQUIRE_POSTGRES=1`, the Python structure oracle, the web
   check when web inputs change, build and smoke tests. Its name and the
   names `main (windows-2025)` and `main (macos-15)` stay as they are: they
   are required checks.
3. **Public tree check.** `.github/workflows/public-tree.yml`, job
   `public-tree`, without `paths-ignore` (it must run on documentation-only
   changes too), on `pull_request`, `push` to `main` and dispatch, in the
   public repository only: checkout, then
   `bash scripts/check-public-tree.sh`. It needs no Go. It fails when a
   private path (`scripts/public-exclude.txt`) or the internal `AGENTS.md`
   appears, or a file names an internal record. `TestPublicSnapshot` also
   runs in the normal suite; in the public repository it checks the
   checkout instead of making a snapshot.
4. **Documentation-only changes.** `ci.yml` keeps `paths-ignore:
   ['docs/**', '**/*.md']`: documentation does not change test results, and
   the public tree job covers documentation.
5. **Weekly full run (not adopted).** `ci.yml` has no `schedule`. With free
   minutes, a weekly full matrix run would catch toolchain and runner image
   drift; the schedule branch of `scripts/ci-changes.sh` exists for it (it
   skips when no code changed in eight days).
6. **Fork pull requests.** `permissions: contents: read`. The `changes`
   job's `gh api` calls happen only for the merge skip, which never runs in
   the public repository, so a fork's token is never used; the job keeps
   `actions: read` for the working repository. No secrets are needed for
   tests.
7. **CLA (to do).** Install CLA Assistant (the hosted GitHub App, or its
   GitHub Action on `pull_request_target`, which never checks out the pull
   request code) with the texts in `docs/public/cla/`, in force as
   version 1.0; make its status a required check.
8. **Branch protection on `main` (to do).** Required checks: `postgres`,
   `main (windows-2025)`, `main (macos-15)`, `public-tree` and the CLA
   check; require pull requests and up-to-date branches.
9. **Comments.** The workflow comments describe both sets of rules without
   cost or budget wording.

**Dependency review.** `.github/workflows/dependency-review.yml`, job `dependency-review`, runs on pull requests to `main` and acts only in the public repository: it fails a pull request that adds a dependency with a known high or critical vulnerability (ADR 0086).

`grammars.yml` and the `parsing-spike` job stay as they are.

**Pinned actions.** Every `uses:` in `.github/workflows/` names a full
commit SHA with the release tag as a comment (for example
`actions/checkout@<sha> # v7.0.1`), so a moved tag cannot change what runs.
Dependabot (`.github/dependabot.yml`, `github-actions` ecosystem) proposes
updates to these pins, and to the Go and npm dependencies, as weekly
grouped pull requests; it acts only in the public repository. Dependabot
runs in any repository whose default branch holds `.github/dependabot.yml`,
so the maintainer's working repository keeps the file as
`docs/public/dependabot.yml` and the public snapshot
(`scripts/public-snapshot.sh`, and the export's tree built the same way)
places it at `.github/dependabot.yml`; the import refuses a change to
either path, which goes into `docs/public/dependabot.yml` by hand.

## `scripts/ci-changes.sh` and its offline test

`scripts/ci-changes.sh` decides `spike`, `web`, `windows` and `skip_tests`;
`scripts/test-t005a.sh` exercises it offline with a mock `gh`, with
`GITHUB_REPOSITORY` pinned so it behaves the same in either repository.

- **Repository input.** `GITHUB_REPOSITORY` (set by GitHub Actions) equal
  to `graphvein/graphvein` selects the public rules; any other value keeps
  the working repository's rules unchanged.
- **`windows`: always true in the public repository.** The `[ci windows]`
  marker logic stays for the working repository. The offline test adds
  public cases: a branch push and a pull request without a marker select
  Windows, a site-only push to `main` keeps it, and a fork's repository
  name keeps the marker rule.
- **Merge skip: never in the public repository.** With required,
  up-to-date pull request checks, a push to `main` re-runs what the pull
  request already ran; that costs minutes, which are free, and buys a check
  of the real `main` tree. The offline test checks that a merge the
  working repository would skip runs in the public repository and calls
  `gh` not at all. If queue time on `main` becomes a problem, a public skip
  needs the condition "the merged pull request's head run concluded
  `success` for every required job" and its cases.
- **`spike` and `web` filters: unchanged**, with their cases.
- **Schedule branch: unchanged** (used only if item 5 is adopted).

## Release archives

Release binaries are built only by the public repository's
`.github/workflows/release.yml`, from a `v*` tag on a commit on `main`
(the job is skipped in any other repository). They are not code-signed;
they come with SHA-256 checksums and build provenance:

1. `go run ./scripts/notices -check` on the tag, then the full suite on
   Linux (`bash scripts/test.sh`, without Postgres), because a tag may
   point at a commit whose `main` run is still going.
2. `bash scripts/release-archives.sh VERSION dist` cross-builds with
   `CGO_ENABLED=0`, `-trimpath -buildvcs=false` and the tag commit's time
   as build date for the six targets of `go run ./scripts/release -list`
   (linux, darwin and windows, each amd64 and arm64), with `REPORT_URL`
   set to the public issue form. Each archive (`.zip` for Windows,
   `.tar.gz` otherwise) contains the binary, `LICENSE`, `NOTICE`,
   `THIRD_PARTY_NOTICES` and `README.md`; entries have fixed modes, no
   owner and the build date as time, so the same binaries give the same
   archive bytes. `SHA256SUMS` lists every archive.
3. A smoke test of the Linux binary (`version`, serve), an upload of the
   archives as a workflow artifact, `actions/attest-build-provenance` on
   the archives, and `gh release create --draft` with
   `docs/releases/<tag>.md` as notes: the maintainer reviews the draft and
   publishes it by hand.

A manual dispatch is a dry run: it builds and uploads the archives and
creates no attestation and no release. The binary also serves `NOTICE`
and `THIRD_PARTY_NOTICES` at `/NOTICE.txt` and `/THIRD_PARTY_NOTICES.txt`
(embedded copies, ADR 0080), so a binary copied out of its archive still
carries every notice. `scripts/release` tests the matrix, the archive
contents and `SHA256SUMS` with fake binaries; `scripts/test-release.sh`
(run by `scripts/test.sh`) builds the host system's archive end to end.

## Export and import (Model B′)

The repository on GitHub is the public repository. The maintainer develops
in a separate working repository; it holds the same public tree plus the
private paths of `scripts/public-exclude.txt`, which never leave it. The two
meet only through the scripts below (ADR 0080, revision 1).

**One export per task.** When a task is finished on the working
repository's `main`, `scripts/export-public.sh TASK_ID PUBLIC_CLONE
SUBJECT` takes the public snapshot of its committed `HEAD` (the same rules
as `scripts/public-snapshot.sh`), runs the public tree check, the reference
scan and the privacy check (`scripts/check-public-privacy.sh`) on it and on
the commit message, and creates branch `export/<task-id>` in a local clone
of the public repository with one commit whose tree is the snapshot. The
whole tree is replaced, so deletions propagate. The script never pushes: it
prints the next steps and the pull request description, which names only
the task id and public facts. The export commit reaches the public
repository from the maintainer's checkout, through the public pre-push
guard (`scripts/hooks/pre-push-public`), which refuses any commit holding a
private path; the pull request is opened from that branch. The first
public commit was made the same way with `--initial` (no parent).

**Full matrix on the pull request, squash-merge by the maintainer.** The
export pull request runs the whole CI matrix in the public repository. The
maintainer squash-merges it in GitHub, so `main` gets one commit per task.

**Post-merge tree check.** After the merge, `scripts/export-public.sh
--check-merged origin/main --private <exported commit> PUBLIC_CLONE`
compares the merged tree with a fresh snapshot of the exported commit and
lists every differing path; any difference is fixed by the next export.

**Contributor pull requests happen here.** They are reviewed and
squash-merged in the public repository like any other. The maintainer then
imports the merged commit with `scripts/import-contrib.sh PUBLIC_CLONE
COMMIT PR_NUMBER`: it applies the diff to a new branch `import/pr-<N>` of
the working repository, keeps the contributor as the author (name, e-mail
and date) and ends the message with `Public-PR: #<N>`. It refuses a diff
that touches a private path or the root `AGENTS.md`. The next export then
carries the change back with an unchanged path.

**What goes where first.** Urgent security fixes and contributor patches
may merge publicly first and are imported. Everything else goes into the
working repository first and reaches the public repository only through an
export.

**References go one way.** Public commits and pull requests may say
`Fixes #N` for a public issue. Public text never names a record of the
working repository (task records, notes or internal links); the public
tree check and the privacy check catch file and path names, and the
export checks its own commit message and pull request text the same way.

**Releases only from public CI.** Tags, release archives and provenance
attestations are built only by the public repository's CI from public
commits; the working repository makes none.

## Order of work

1. Done in the working repository: items 1, 3 and 9, the
   `ci-changes.sh` and `test-t005a.sh` changes, the release workflow and
   the pinned actions above. The public rules among them change nothing
   there, because they apply only in `graphvein/graphvein`. The licence check
   (`TestThirdPartyNotices`), the public tree test (`TestPublicSnapshot`,
   `TestCheckPublicTreeFails`, with the privacy check), the export
   rehearsal (`scripts/test-export-public.sh`) and the release rehearsal
   (`scripts/test-release.sh`) run in `scripts/test.sh`.
2. Initial public commit: `scripts/export-public.sh --initial launch ...`
   in a clone of the empty public repository, pushed to `main`; then
   `--check-merged` against it. It carries everything in step 1; its first
   run on the full matrix and the public tree job confirm the public rules.
   Every later task follows the export flow above.
3. With the CLA texts in force (version 1.0): item 7 and branch protection
   (item 8).
