# Changelog

All notable changes to Graphvein are recorded here, in the style of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions follow
[Semantic Versioning](https://semver.org/) once the first one is tagged.

Each export from the maintainer's working repository adds one entry under
Unreleased, from now on.

## [Unreleased]

### Added

- First public release: developed privately since October 2026; this
  repository starts from a snapshot.

### Changed

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
