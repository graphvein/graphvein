# ADR 0004 — Project skeleton and golden harness

**Status:** Accepted (developer review, 2026-10-05)
**Date:** 2026-10-05

## Context

T003 establishes the application module, CLI interface and reusable fixture
helper. It must build with Go alone and compare identical snapshots on Windows,
macOS and Linux. ADR 0001 fixes Go as the core language and ADR 0002 confines
the native parser experiment to the separate spike module.

## Options considered

- A CLI framework and JSON diff library: richer commands and diffs, but extra
  dependencies for two commands and a small test helper.
- Standard-library command dispatch and a JSON comparison helper: sufficient
  for the skeleton, with no third-party application dependencies.
- Normalize every JSON string as a path: convenient, but corrupts source text
  and other strings containing literal backslashes.
- Explicit path fields: callers mark paths with a small JSON-marshalling type.

## Decision

Use the standard library for `help` and `version`, with build variables injected
through Go linker flags. Unknown commands and extra arguments exit with code 2.
No arguments show help. Pin Go 1.27.1, the installed and tested version, in the
root module; CI reads it rather than duplicating the version.

Use `github.com/graphvein/graphvein` as the placeholder module path requested by
T003. Do not import the separate `spike/parsing` module.

Provide `internal/golden.Check(testing.TB, path, value)` and `golden.Path` for
portable filesystem paths. Canonical JSON sorts all object keys, retains array
order and numeric precision, and uses two-space indentation and a final LF.
Comparison is byte-exact; mismatches identify the first differing JSON path.
Only the explicit test flag `-update` writes expectations, and only for files
reached by selected tests. Self-tests use subprocesses to verify actual test
failures and update-flag behavior without failing the parent suite.

## Consequences

- The application has no third-party dependencies and no cgo.
- Future CLI commands can reconsider a framework when its benefits are concrete.
- Fixture producers must provide deterministic array order and mark filesystem
  paths with `golden.Path`; ordinary source strings retain their exact contents.
- Golden updates are deliberate and must be reviewed. Formatting changes also
  count as mismatches, preventing host-specific line endings from slipping in.
- Bash scripts are invoked with `bash`, avoiding dependence on executable bits
  set by a Windows checkout. CI separately maintains the WASM spike on Windows.

## Known gaps

- The placeholder owner must change when the GitHub organization is chosen.
- Only the two skeleton commands exist; no indexer contract is introduced here.
- The harness diagnoses the first difference rather than computing an entire
  edit script for large JSON documents.
- GitHub-hosted execution still needs a developer push; local Windows checks
  cannot demonstrate execution on macOS or Linux.
