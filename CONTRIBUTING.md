# Contributing to Graphvein

Thank you for helping. Graphvein maps a codebase from its architecture down
to the line, and its central promise is precision: it never states a fact it
cannot back, and it says how sure it is of every fact it shows. Contributions
are reviewed against that promise first.

Before larger changes, open an issue to agree on the approach. Read
[the vision](docs/VISION.md), [the architecture](docs/ARCHITECTURE.md), the
[roadmap](ROADMAP.md) and the ADRs in [docs/adr/](docs/adr/) for the area you
want to touch. If your coding agent helps you, point it at
[AGENTS.md](AGENTS.md).

## Contributor licence agreement

Every contribution needs a signed contributor licence agreement (CLA),
adapted from the Apache Software Foundation's individual and corporate CLAs:
you keep the copyright in your contribution and grant the project owner a
licence to use and relicense it, plus a patent licence. CLA Assistant asks
you to sign when you open your first pull request; a pull request cannot be
merged until it is signed. If you contribute as part of your job, your
employer may need to sign the corporate CLA.

CLA Assistant presents the agreement as a comment on your pull request
with a link to the text (the
[individual CLA](docs/public/cla/individual.md), version 1.0, with its
[privacy notice](docs/public/cla/privacy.md)); you sign by following the
link with your GitHub account, and its status check on the pull request
then passes. You sign once; later pull requests are covered.

## Setting up

- Go **1.27.4** (the `toolchain` line in `go.mod`) and Bash. With an older
  Go 1.21+ installed and the default `GOTOOLCHAIN=auto`, the first `go`
  command prints `go: downloading go1.27.4 (<os>/<arch>)` and then runs
  1.27.4 for this repository; `GOTOOLCHAIN=local` keeps your own Go, which
  still builds (the language version is 1.27.1) but lacks the standard
  library security fixes `scripts/vulncheck.sh` checks for. On Windows, use
  Git Bash; every command in this repository works there. No C compiler is
  needed: the build is pure Go (`CGO_ENABLED=0`).
- Node 24 LTS (pinned in `web/.nvmrc`) only if you change the web UI.

## Running the tests

```bash
bash scripts/test.sh          # vet, unit tests, golden fixtures, licence check
bash scripts/build.sh         # builds bin/graphvein (bin/graphvein.exe on Windows)
bash scripts/check-web.sh     # only after changing web/: lint, tests, build drift
bash scripts/vulncheck.sh     # known vulnerabilities in the binary and web dependencies (needs network)
```

`scripts/test.sh` must pass before you open a pull request. It runs offline
(run `go mod download` once first).

**Vulnerability scans.** CI runs `scripts/vulncheck.sh`: govulncheck in
binary mode on the built binary, and `npm audit` on the web UI's lockfile
when the web UI changes. A vulnerable symbol linked into the binary fails
unless a reviewed entry with an expiry date in
`scripts/vulncheck/allowlist.txt` covers it; a high or critical advisory in
a web UI production dependency fails. Fix a finding with a dependency bump
where possible; an allowlist entry needs a reason and a short expiry
([ADR 0086](docs/adr/0086-vulnerability-scanning.md)).

**Postgres.** Storage code is tested against both SQLite and Postgres. The
Postgres subtests (`.../postgres`) run when `GRAPHVEIN_TEST_POSTGRES_URL`
points at a disposable Postgres 17 database; without it they are skipped
locally. CI's `postgres` job runs the whole suite against a real Postgres
on every pull request, so you do not need a local database. If you add
storage code, register its Postgres coverage in the shared backend tests.

**Corpus and oracle tests.** Some tests compare against pinned open-source
repositories or a reference toolchain (a real Postgres, CPython, a
compiler). They skip unless their environment variable names a checkout or
tool; their data lives in [docs/contract/claims/data/](docs/contract/claims/data/).

## Rules that keep the graph honest

**Golden files.** Fixture tests compare against golden JSON. Never update a
golden file just to make a test pass. If an output must change, regenerate
only the affected fixtures
(`go test ./internal/golden -run '^TestFixture$' -update`, or the package's
own `-update` flag), review the diff, and explain in the pull request why
each golden changed.

**Claim contracts.** Extractors and linkers over a large language or
framework surface keep a claim contract: a table in which every modelled
form has exactly one row, every claim the code makes is named by a row, and
every other form goes to a single unknown default branch. The tables live in
[docs/contract/claims/](docs/contract/claims/) and the extractor's page under
`docs/extract/` or `docs/lang/`; contract tests keep code and tables 1:1.
Never add a claim the table does not list. A new form or claim is a
contract change: change the table, the docs, the code table and the tests
together and say so in the pull request. Where an oracle exists (a real
Postgres, compiler or framework), a disagreement with it is a precision
stop, not a detail.

**New behaviour needs a test.** Extractors and linkers always need a fixture
with a golden output.

**Failures stay local.** A failure while processing one file must never stop
the others; record it as a diagnostic.

**Architecture boundaries.** Only `internal/egress` may open network
connections. No cgo. All persistence goes through the store interface. The
application consumes indexer output only through the contract in
[docs/contract/](docs/contract/); changing it means bumping
`contract_version` and updating the docs and golden tests in the same
change.

**Design decisions.** A change to the architecture, the indexer contract,
the storage schema, dependencies or public interfaces needs an ADR in
`docs/adr/NNNN-short-title.md` (context, options, decision, consequences,
known gaps) with `**Status:** Proposed`; maintainers set the final status.

## Fixtures and ignored files

Every file you add must be tracked by Git without `git add -f`. Before
opening a pull request run:

```bash
git status --porcelain --ignored
git ls-files -ci --exclude-standard
```

Neither may list a file you added. Fixtures must not contain live ignore
files (`.gitignore`, `.graphveinignore`) or paths the repository ignores
(`node_modules`, `dist`, `build`, `target`, agent folders). Store them under
inert names (`_gitignore`, `_graphveinignore`, `_node_modules/`, ...); tests
copy the fixture to a temporary directory and restore the real names.
Document the mapping in the fixture's README.

## Shell scripts

Scripts must run in Git Bash on Windows and under **bash 3.2** (macOS's
default), because CI runs on Windows, macOS and Linux. Under `set -u`, never
expand a possibly empty array as `"${arr[@]}"`; use
`${arr[@]+"${arr[@]}"}`. Do not use associative arrays, `mapfile` or
`readarray`, `${var,,}` or `${var^^}`, `|&`, `&>>` or negative array
indices. Do not rely on PowerShell or `make`.

## Dependencies and licences

Graphvein is licensed under the Apache License 2.0. A new Go module, npm
runtime package or grammar must carry a licence on the allowlist in
`scripts/notices` (permissive licences; anything else needs a reviewed
decision). After changing dependencies run `go run ./scripts/notices` and
commit the regenerated `THIRD_PARTY_NOTICES`; the test suite fails when it is
stale. Web UI dependencies are also checked by `scripts/check-web.sh`.

## Code style

Plain, idiomatic Go that people learning Go can read. Wrap errors with
context (`fmt.Errorf("parsing %s: %w", path, err)`), no panics for expected
failures, `log/slog` for logging, and doc comments on exported identifiers
that explain why. The web UI is TypeScript in strict mode; no `any` without
a comment explaining it.

## Pull requests

**Why the history looks the way it does.** The maintainer works in a
separate working repository and brings each finished task here as one
squashed export commit, on a branch named `export/<task-id>` with its own
pull request that runs the full CI matrix. So `main` shows one commit per
task rather than the work in progress behind it. Contributor pull requests
are reviewed and squash-merged here, and the maintainer then imports them
into the working repository with their author kept, so later exports carry
them unchanged.

Keep a pull request small enough to review in one sitting and focused on one
change. Fill in the pull request template; note anything you noticed but did
not change as a follow-up. Report security problems privately (see
[SECURITY.md](SECURITY.md)), not in issues. Everyone taking part follows the
[code of conduct](CODE_OF_CONDUCT.md).
