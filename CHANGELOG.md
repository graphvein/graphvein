# Changelog

All notable changes to Graphvein are recorded here, in the style of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions follow
[Semantic Versioning](https://semver.org/).

Each export from the maintainer's working repository adds one entry under
Unreleased.

## [Unreleased]

### Changed

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

[Unreleased]: https://github.com/graphvein/graphvein/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/graphvein/graphvein/releases/tag/v0.1.0
