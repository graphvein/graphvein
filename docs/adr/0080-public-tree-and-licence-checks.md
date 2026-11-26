# ADR 0080 - Public tree guard and licence checks

**Status:** Accepted (delegated review, 2026-11-20)

## Context

Graphvein goes public as a new repository with a fresh history, started from
a snapshot of this repository (the project owner's decision for launch 1).
The private repository keeps internal material that must not enter the
public tree: the handoff notes for the reviewing assistant, task records,
research, the internal plan, the CI budget notes, profiling records with
developer machine details, and the internal agent instructions. Until the
launch, development continues here, so the split has to hold for every
later snapshot and every push to the public remote, not only once.

Two further things block a public release:

- Tests read claim contract tables and census files from task records. If
  those records stay private, the public test suite cannot run.
- The binary bundles third-party software (Go modules, tree-sitter grammars
  compiled to WebAssembly, the web UI's npm packages, editor logos). The
  project is licensed under the Apache License 2.0, so the bundled licences
  must be compatible and their notices must ship.

## Options considered

1. **Filter the history** (`git filter-repo`) and publish this repository.
   Keeps history but every past commit would need review for internal
   content; rejected by the fresh-history decision.
2. **Ignore rules only** (a `.gitignore`-like list in the public repository).
   Ignore rules do not stop a tracked file or a force-add, so they guard
   nothing on their own.
3. **An explicit exclusion list with a snapshot tool, a tree check and a
   push guard** (chosen). The list is data, read by three small scripts;
   tests prove the snapshot passes the check and that the check fails on
   each kind of leak.

For licences:

1. Rely on the web build's notice file (`third-party-licenses.txt`) only. It
   covers the npm packages, shadcn/ui, the Go standard library and the logos,
   but no Go module and no grammar.
2. **A Go generator with a reviewed table and an allowlist** (chosen), run as
   a test: offline, deterministic, on every platform CI uses.
3. An external licence scanner. A new dependency and network access in CI;
   rejected.

## Decision

**Public contract tables.** Claim contract tables and census data that tests
read live in `docs/contract/claims/` and `docs/contract/claims/data/`. The
tables are verbatim copies of the contract sections of the task records;
the census files moved there unchanged. Every test checks the same rows as
before.

**Public tree guard.**

- `scripts/public-exclude.txt` lists the private paths with a reason each.
  The root `AGENTS.md` is replaced, not removed: the public version is kept
  in `docs/public/AGENTS.md` until launch.
- `scripts/public-snapshot.sh OUTDIR [LISTFILE]` exports `HEAD` with
  `git archive`, removes the private paths, moves the public `AGENTS.md` to
  the root and writes the exact file list.
- `scripts/check-public-tree.sh [DIR]` fails when a private path exists,
  when the root `AGENTS.md` is the internal one, or when a text file names
  an internal record (the task and research records, the handoff notes, the
  plan, the CI budget, or a task file by name), except the files in
  `scripts/public-reference-exempt.txt` (the guard's own files, this ADR,
  and the CI plan).
- `scripts/hooks/pre-push-public` refuses a push to the public remote
  (matched by URL) when any pushed commit's tree holds a private path or the
  internal `AGENTS.md`.
- `TestPublicSnapshot` runs the snapshot and the check in `go test ./...`;
  `TestCheckPublicTreeFails` shows that each check fails on its leak.

**Licences.** `scripts/notices` (`go run ./scripts/notices`) writes
`THIRD_PARTY_NOTICES` from the modules linked into `cmd/graphvein` for
linux, darwin and windows (`go list` with `GOPROXY=off`), the asset table of
`grammars/NOTICES.md` and the runtime packages and bundled build tools of
`web/package-lock.json`. It reproduces the Go module and grammar licence
files verbatim and points to the web UI's own notice file for npm licence
texts. The allowlist is Apache-2.0 (also WITH LLVM-exception), MIT, MIT-0,
BSD-2-Clause, BSD-3-Clause, ISC, 0BSD, Unlicense, CC0-1.0, Python-2.0, Zlib,
blessing (SQLite's public domain dedication) and MPL-2.0 (with a note:
file-level copyleft). Every Go module has a reviewed licence expression; the
classifier must find each licence file's licence in it. A new module, a
licence outside the list, an unrecognised licence file or a stale
`THIRD_PARTY_NOTICES` fails `TestThirdPartyNotices`. Reviewed exceptions name
an exact version and an ADR (elkjs 0.12.0, ADR 0046).

**Notices in the binary.** `internal/server/ui` embeds copies of `NOTICE`
and `THIRD_PARTY_NOTICES` (`legal/`, written by `scripts/notices`, kept
equal to the originals by `TestLegalCopies`); the server serves them at
`/NOTICE.txt` and `/THIRD_PARTY_NOTICES.txt` next to the web UI's
`/third-party-licenses.txt`. Release archives ship `LICENSE`, `NOTICE` and
`THIRD_PARTY_NOTICES` next to the binary.

**Editor logos** stay under their owners' terms; `NOTICE` and
`web/src/assets/editor-logos/README.md` exclude them from the Apache
License.

## Consequences

- The public repository can run the whole test suite.
- A new dependency needs a reviewed table entry and a regenerated
  `THIRD_PARTY_NOTICES` in the same change.
- Every new file is checked for references to internal records once it is
  committed; public docs refer to task records only as "the T0xx task record
  (internal)".
- The public CI changes are planned in `docs/public/ci-plan.md`.

## Known gaps

- The snapshot exports committed content only (`HEAD`); uncommitted work is
  not checked by `TestPublicSnapshot`.
- The reference scan matches path and file-name patterns; prose that
  describes internal material without naming it is not caught.
- npm licence texts come from the web build's notice file, which needs
  `node_modules`; the Go check reads only the lock file's `license` fields.

## Review notes (2026-11-20)

- Results: gate run 44712169191 (Linux: `changes`, `postgres`, `parsing-spike` green; postgres log 0 FAIL, 226 `/postgres` PASS, only the environment- and corpus-gated opt-in skips). Windows runs on the final CI gate.
- Checked: 14 claim tables and 12 census files moved to `docs/contract/claims/` with rows unchanged; 33 tests repointed (a 34th through a constant); no golden change except the four structure fixture README `content_hash` lines (README text de-referenced); the public snapshot (5,569 files) passes the public-tree check, and the reference scan finds no internal references outside the listed exemptions.
- Licences: every bundled Go module and grammar licence is on the allowlist; elkjs is used under EPL-2.0 (ADR 0046). NOTICE and THIRD_PARTY_NOTICES are embedded and served by the binary and ship in release archives.
- Known gaps / before launch: CLA texts are drafts for the project owner's and a lawyer's review (CLA Assistant stays off until approved); the code of conduct contact address; editor logo permissions; the public CI plan (`docs/public/ci-plan.md`) is applied in the public repository at launch.

## Revision 1 (2026-11-26): export and import between the repositories ("Model B′")

**Status:** Accepted (delegated review, 2026-11-26)

The original decision above stays accepted; this revision adds the
workflow after the launch.

### Context

The launch decision started the public repository from a snapshot, but it
left open how later work gets there. The project owner decided: this
repository stays the maintainer's working repository; the public
repository receives each finished task as one commit on a branch
`export/<task-id>` with a pull request; contributor pull requests happen in
the public repository and are imported here with their author kept;
releases, tags and provenance attestations are built only by the public
repository's CI from public commits. Every export is a new chance to leak
private material, and the existing guard checks private paths and record
names but not personal details, developer paths or private corpus names.

### Options considered

1. **Push the working repository's history** (filtered per push). Every
   commit and message would need review; rejected with the fresh history.
2. **Cherry-pick task commits** onto the public branch. Commits that touch
   private and public paths together would need splitting, and the public
   tree could drift from the snapshot without anyone noticing.
3. **One snapshot commit per task** (chosen): the public tree is always the
   snapshot of the working repository's `main`, checked before the commit
   and compared after the merge.

### Decision

- `scripts/export-public.sh [--base REF | --initial] [--body FILE] TASK_ID
  PUBLIC_CLONE SUBJECT` refuses a working tree with uncommitted changes or
  untracked files, makes the public snapshot of `HEAD`, runs
  `scripts/check-public-tree.sh` and the privacy check on it and on the
  commit message and pull request text, and creates `export/<task-id>` in a
  local clone of the public repository: one commit whose tree is the
  snapshot (built from Git objects with a temporary index, so the whole
  tree is replaced and deletions propagate; the clone's working tree and
  index are not touched), parented on `REF` (default `origin/main`), or
  without a parent for `--initial`. It is authored by the configured
  identity and not signed. It never pushes; it prints the next steps and
  writes the pull request description from `scripts/export-pr-template.md`.
  The commit reaches the public repository from the maintainer's checkout,
  through `scripts/hooks/pre-push-public`.
- `scripts/export-public.sh --check-merged PUBLIC_REF [--private REV]
  PUBLIC_CLONE` compares the tree of a merged public commit with a fresh
  snapshot of `REV` and lists every differing path (exit 1).
- `scripts/check-public-privacy.sh DIR` fails on e-mail addresses outside a
  short allowlist (the project contact, GitHub's SSH user, and the reserved
  documentation domains of RFC 2606 and RFC 6761), on developer and
  container paths, and on private corpus names read from a patterns file
  in a private folder of the working repository (listed in
  `scripts/public-exclude.txt`), so the guard does not publish the names it
  guards; without that file the name check is skipped and the script says
  so.
  Reviewed exemptions, each with a reason, are in
  `scripts/public-privacy-exempt.txt` (third-party licence texts, two
  vendors' published brand contacts). `TestPublicSnapshot` runs it on the
  snapshot.
- `scripts/import-contrib.sh [--base REV] PUBLIC_CLONE COMMIT|FROM..TO
  PR_NUMBER` applies a merged public pull request to a new branch
  `import/pr-<N>` with the public author (name, e-mail, date), the
  configured committer and a final `Public-PR: #<N>` line; it refuses a
  diff that touches a private path, the root `AGENTS.md` or
  `docs/public/AGENTS.md`.
- `scripts/test-export-public.sh` (run by `scripts/test.sh`) rehearses the
  flow offline with a local bare repository, including the refusals.
- The flow is described in `docs/public/ci-plan.md` ("Export and import").

### Consequences

- The public `main` has one squashed commit per task; the working
  repository keeps the detailed history. `CHANGELOG.md` gets one entry per
  export, and `CONTRIBUTING.md` explains the history's shape.
- A public change made outside an export (a contributor patch, an urgent
  security fix) must be imported before the next export, or the next export
  reverts it; the export's diff against the public base shows this.
- Public text may refer to public issues (`Fixes #N`), never to records of
  the working repository.

### Known gaps

- The privacy check matches patterns: a personal detail written without an
  address or path (a name in prose) is not caught, and the address
  allowlist trusts the reserved test domains.
- The post-merge check is run by hand; a squash-merge whose tree differs
  (for example a conflict resolved in GitHub) is caught only then.
- Importing a pull request onto a working `main` that moved on can fail to
  apply; the script then stops and the change is applied by hand.
- The privacy check runs on file contents, not on file names.

### Review notes (2026-11-26)

- Results: gate run 45787767515 (Linux: `changes`, `postgres`, `parsing-spike`; `main (windows-2025)`; all green; postgres log 0 FAIL, 291 `/postgres` PASS, 90 skips, the same opt-in skips as before). The first gate run failed on Windows only: the offline rehearsal's throwaway repositories were checked out under the runner's `core.autocrlf=true` before the test turned it off; the scripts themselves compare Git objects only. The test now creates its repositories with the conversion off and keeps one deliberate `core.autocrlf=true` contributor case.
- Checked: export, post-merge tree check, contributor import (author kept, `Public-PR: #N` recorded, private paths and `AGENTS.md` refused) and the push guard (stops at the first commit with a private path; its walk is bounded only by the public remote's tracking refs, never by the working repository's) in the offline test and in a rehearsal of the real tree against local bare repositories; the privacy check on a fresh snapshot (developer paths it found were replaced; licence-text addresses are exempt by file).
- The contributor licence agreement texts are in force as version 1.0 since 2026-11-26 (the "before launch" item of the 2026-11-20 notes); the drafting questions for a later legal review are kept outside the public tree.
