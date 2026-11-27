# Changelog

All notable changes to Graphvein are recorded here, in the style of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions follow
[Semantic Versioning](https://semver.org/).

Each export from the maintainer's working repository adds one entry under
Unreleased.

## [Unreleased]

## [0.2.0] - 2026-11-28

### Added

- Indexes are kept outside the repository by default, in a per-user
  stores folder: `%LocalAppData%\graphvein\stores` on Windows,
  `~/Library/Application Support/graphvein/stores` on macOS and
  `$XDG_DATA_HOME/graphvein/stores` (default
  `~/.local/share/graphvein/stores`) elsewhere, one owner-only folder per
  indexed folder. `graphvein index` no longer writes `.graphvein/` into
  the repository, so `git status` stays clean, and it prints first where
  the index is (`Index: <path>` and why it is there). An existing
  `<folder>/.graphvein/index.db` keeps working where it is and is never
  moved on its own; when a folder has two indexes, every command refuses
  and names both (ADR 0093).
- `graphvein store where [path]`, `graphvein store move <path> [<folder>]`
  and `graphvein store list`: where an index is kept and why, moving it
  (copied, checked, then the old files removed; asks first unless
  `--yes`) and every index Graphvein knows with its folder and size.
  `GRAPHVEIN_STORE_DIR` sets the stores folder (for example for scripts
  and tests); `stores.yaml`, beside `workspace.yaml`, records a chosen
  default folder and indexes kept elsewhere.
- Settings in the web UI starts with a Storage section, also without an
  index: where each index is, its size, Move… and Copy path, and the
  folder new indexes go to (Change…). The Add a folder confirmation shows
  where the new index will be kept, with Change….
- An Overview page is the home of the web UI: the repository's parts and
  libraries, its interfaces by transport, its data, what Graphvein could
  not read, and where to start. The logo links to it.
- The sidebar collapses to its icons with a button or `[`, and the choice
  is remembered in the browser; below 1100 px wide it opens over the page.
- A file whose parse runs past its deadline while other files are parsed
  in parallel is parsed again alone with a longer deadline (60 s by
  default). `graphvein index` prints how many files were parsed again and
  how many were read, and when files stay unread it says the counts are
  incomplete. The Overview, the Architecture section and What Graphvein
  couldn't read show the same notice; `/api/v0/status` gains
  `index.parse_failed` and `/api/v0/architecture` `parse_failed` (both
  left out when zero).
- `/api/v0/schema/summary` gains `refused_sources`: model sources
  Graphvein found and refused to read as a whole (for example a GORM
  module with a database driver it does not read, or a Prisma package
  without a schema it can prove), each with tool, file and reason.
- A Code scanning triage workflow, started by hand: it lists the open
  code scanning alerts, or dismisses the ones matching the reviewed list
  in `.github/codeql-dismissals.json` (rule, exact file, reason and the
  comment saying why); an alert without an entry stays open.
- A Dependabot triage workflow (daily, and by hand in list or close
  mode): it closes Dependabot pull requests whose changed files are all in
  oracle tooling directories pinned on purpose
  (`.github/dependabot-pinned.json`), with one comment giving each
  directory's reviewed reason; it never merges and leaves every other pull
  request alone. `dependabot.yml` also stops version and security-update
  pull requests for those directories (alerts stay).

### Changed

- The extractor version is `extractors-64`. Facts are unchanged, but the
  cached facts have a new layout, so the first index after upgrading
  parses every file again.
- Expression trees in TypeScript and JavaScript facts are shared instead
  of copied into every enclosing call: stores are up to about a third
  smaller (one measured store went from 944 MB to 640 MB) and memory for
  deeply nested code grows with the file instead of with its nesting.
- `GRAPHVEIN_SQLITE_PATH` still names the store file, but only for the
  folder named on the command line; the web UI opens every other
  workspace entry from its own store.
- Architecture shows libraries by default, as a band of library chips
  below the component cards with lines on a shared bus. Selecting a card
  or a library marks what it uses and what uses it, and chain chips are
  drawn above their lines. `show=nolibraries` in the page address hides
  the band.
- API section: one summary strip per transport, with the coverage cards
  behind a "Coverage and roots" disclosure (remembered in the browser); a
  transport with nothing found in the indexed files is a dashed "none
  found" tab. The endpoint list shows "0 linked" as text and a quieter
  "path assumed" mark, the route header is the path and one line of
  facts, an assumption shared by every caller is stated once above the
  callers, and the Flow tab's legend is one strip with a Legend button
  for the full legend. The "Test only" category reads "Only test
  endpoints match".
- Schema: the page shown when no tables are found lists what Graphvein
  reads and what it does not read yet, and names refused model sources
  with their file and reason ("Graphvein found GORM models but did not
  read them"); What Graphvein couldn't read lists them too.
- A production call whose only match is a route mounted under test code
  is no longer counted as test-only when a production route with an
  unknown prefix could also serve it: it is unmatched, and that route
  shows its path with "prefix unknown" instead of the path built only
  from test code (ADR 0021, revision 1).
- Development dependencies: jsdom 31.0.1; the release workflow uses
  upload-artifact v7.0.2 and download-artifact v8.0.2.

### Fixed

- Indexing a file with a very deep syntax tree, such as a large
  generated TypeScript union type, no longer copies the file's text into
  every node: one 717 KB file needed about 6.9 GB before.
- A temporary snapshot left by a killed `graphvein index` run is removed
  by the next run of the same index.
- The Couldn't read page's route paths card no longer says "Every
  production route has a full path" when no production route was found.
- Re-indexing a Go repository with deeply nested expressions (for
  example a long chain of selectors) no longer stalls: checking a cached
  expression took twice as long for every level of nesting, so a warm
  run could stop making progress.

### Known limits

- Very large TypeScript monorepos can still run out of memory during
  indexing. On one such repository (about 28,000 files, most of them
  vendored), excluding the vendored folder in `.graphveinignore` and
  setting `GOMEMLIMIT=5GiB` let indexing finish within 7 GB of memory;
  the whole repository still needs more.

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

[Unreleased]: https://github.com/graphvein/graphvein/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/graphvein/graphvein/releases/tag/v0.2.0
[0.1.2]: https://github.com/graphvein/graphvein/releases/tag/v0.1.2
[0.1.1]: https://github.com/graphvein/graphvein/releases/tag/v0.1.1
[0.1.0]: https://github.com/graphvein/graphvein/releases/tag/v0.1.0
