# Changelog

All notable changes to Graphvein are recorded here, in the style of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions follow
[Semantic Versioning](https://semver.org/).

Each export from the maintainer's working repository adds one entry under
Unreleased.

## [Unreleased]

## [0.1.2] - 2026-11-27

### Changed

- The release workflow skips its own test run when CI already tested the
  tagged tree (a successful run with the postgres, Windows and macOS jobs
  on the tag commit, or on the merged pull request's last head with the
  same tree), and builds the six targets in parallel jobs: a merge to a
  draft release takes about 5 minutes instead of about 13
  (`docs/public/ci-plan.md`).
- A release can also start from the Actions tab: the Tag release workflow
  creates the `v*` tag on a commit of `main` (after the release checks)
  and starts the release workflow on it, which treats a dispatch on a tag
  as a release and a dispatch on a branch as the dry run.
- `await f<T>(x)` is read as a call of `f`: the call gets its name and its
  `calls` edge, and an awaited generic `Object.assign` counts as a write.
- Dependencies: pgx 5.13.0, wazero 1.12.0, modernc.org/sqlite 1.63.0
  (SQLite 3.54.1), yaml 3.0.6, x/sys 0.50.0, x/text 0.44.0; the web UI's
  lucide-react, Tailwind CSS, Vite, React plugin and typescript-eslint
  minor updates; setup-python v7.1.0, dependency-review-action v5.0.1 and
  upload-artifact v7.0.2. The extractor version is `extractors-63`.

### Fixed

- Rust files whose only syntax errors are attributes on struct pattern
  fields (`#[cfg(...)]` before a field in a pattern, which rustc accepts
  and the grammar does not) are read from a verified second parse instead
  of being dropped (row RS-F4, ADR 0092). Any other syntax error still
  leaves the file unread.
- A `.rs` file holding a few raw NUL bytes in literals or comments is read
  as source instead of being skipped as binary.
- The architecture chain endpoint answers 500 when the store fails,
  instead of 200 with an empty chain.
- A remembered view whose entry id is a JavaScript object property name
  (such as `constructor`) no longer reads an inherited value.
- docs/known-gaps.md says that Pages Router API routes (`pages/api/`) are
  read.

### Security

- The copied `curl` command in Try it shell-escapes the configured auth
  header name and prefix, so a configured value cannot run a command when
  the copied text is pasted into a shell.
- The oracle tooling (development only, not shipped) takes the x/crypto,
  x/net, quic-go, joi, zod and next 15.5 security fixes. govulncheck
  reports nothing in the `graphvein` binary.

## [0.1.1] - 2026-11-26

### Changed

- `graphvein` with no arguments serves the workspace web UI (as
  `graphvein serve` with no path) and opens the default browser at it, on
  every platform, instead of printing the usage; double-clicking
  `graphvein.exe` on Windows now opens the web UI instead of a window that
  closes at once. It uses `127.0.0.1:7420`, opens a Graphvein already
  running there instead of starting a second one, and otherwise falls back
  to the next free port up to 7430. No browser is opened over SSH or
  without a display; the URL is printed instead. `graphvein help` lists
  the commands as before.

- The website's quick start runs `graphvein serve .` after
  `graphvein index`, which opens the indexed repository directly;
  `graphvein serve` without a path opens the workspace view.

### Fixed

- CI starts on a push to `main`, a pull request and a manual dispatch
  only (Grammar assets: the same, when its inputs change). A branch push
  no longer starts a second CI run beside its pull request's, whose
  cancelled jobs blocked the required checks, and a tag push starts only
  the release workflow.
- Release notes on GitHub no longer break lines mid-sentence: the release
  workflow joins the wrapped lines of `docs/releases/<tag>.md`, removes
  its comments and drops its title, which the release title repeats
  (`scripts/releasenotes`).
- `graphvein help` lists the `query`, `callers` and `impact` commands,
  and the README shows the command-line path to them.
- The benchmark results of 2026-11-24 no longer say a public commit holds
  the build the runs used: that build (`extractors-59`) predates the public
  history, which starts at v0.1.0 (`extractors-62`).
- The README says that Graphvein runs locally, uses no LLM and connects
  only to what the user configures; the website's home page lists the
  Angular `HttpClient`, hey-api, Orval and Supabase JS clients it reads.

## [0.1.0] - 2026-11-26

### Added

- First public release: developed privately since October 2026; this
  repository starts from a snapshot.

### Changed

- Recall is now reported for application code first: 93.0% (langflow)
  and 93.5% (seerr); owncast's 81.5% counts only the calls Graphvein
  recognises (its admin console's `fetchData` helper calls are not), so
  it is an upper bound. The all-calls figure and its reason stay beside
  it; `link_precision.py stats --areas` prints the figures from the
  evidence data.
- Dependabot groups major updates per ecosystem in their own weekly pull
  request, beside the grouped minor and patch updates.
- CI's Windows and macOS test jobs set up the reference CPython 3.14, as
  the Linux job does.

### Fixed

- The test suite on a machine whose `python3` is not CPython 3.14 (macOS
  runners with Python 3.15): the Python oracle tests compare with the
  reference CPython 3.14 only, the TOML reader test with a TOML 1.0
  `tomllib` (Python 3.11 to 3.14), and the structure oracle no longer
  depends on the platform's extension-module suffix. Indexing is
  unchanged.
- The web UI writes the viewed index into the page URL in the same update
  that shows it, so a link copied right after the page opened names the
  index.
- The Python module-list test on macOS and Windows with the reference
  CPython version: the built-in, start-up, shipped and codec lists differ
  by platform, so they are compared exactly only on the platform they were
  generated on, and as inclusions elsewhere. Indexing is unchanged.

[Unreleased]: https://github.com/graphvein/graphvein/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/graphvein/graphvein/releases/tag/v0.1.2
[0.1.1]: https://github.com/graphvein/graphvein/releases/tag/v0.1.1
[0.1.0]: https://github.com/graphvein/graphvein/releases/tag/v0.1.0
