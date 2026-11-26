# ADR 0002 — Tree-sitter integration through WASM

**Status:** Accepted (2026-10-05, after T001 and T002 review)
**Date:** 2026-10-05

## Context

Graphvein needs deterministic tree-sitter parsing while remaining buildable on
Windows machines with only Go installed. T001 compares upstream tree-sitter
compiled to WASM inside a pure-Go runtime with official native cgo bindings.
The experiment lives in its own module under `spike/parsing/`, separate from
future application dependencies and interfaces.

## Options considered

- **A — WASM via wasitter/wazero:** no C compiler for consumers or cross-builds;
  upstream C runtime and grammars run inside WASM. Adds runtime, boundary-call
  and embedded-asset overhead, with a relatively young Go wrapper.
- **B — Official cgo bindings:** direct upstream execution, expected to be faster;
  requires a C compiler on every build machine and target-specific compilers or
  native CI builds for releases.

## Measurements

Windows/amd64, Go 1.27.1, Git Bash, no GCC/Clang/cc. No system tools installed.
WASM uses wasitter v0.1.0 and wazero v1.10.1; native uses go-tree-sitter v0.25.0.
Both candidates pin TSX v0.23.2 and Rust v0.24.2 grammars.

- WASM builds and matches both hand-checked sample goldens, including the arrow
  `const` component at line 9, without crashing.
- A generated 6,000-line / 314,010-byte TSX file parses without syntax errors:
  **172.2395 ms median over five fresh-tree parses**, excluding runtime setup,
  traversal and cleanup. Its 3,000 function/class declarations are also verified.
- WASM binaries: Windows/amd64 12,368,896 bytes; Linux/amd64 12,250,884 bytes;
  macOS/arm64 11,951,394 bytes. Both cross-builds succeed with CGO disabled.
- Native builds fail for all three targets: GCC is missing for Windows/Linux;
  Clang is missing for macOS. Native correctness, time and size are unmeasured.
- Peak memory is unmeasured; cross-built binaries were not executed.

See [the report](../../spike/parsing/REPORT.md) for methodology, reproduction,
limitations and links to raw results. There is no measured WASM/native ratio.

## Decision

Use **Option A: upstream tree-sitter in WASM through a pure-Go runtime** for the
built-in parser. The validated initial adapter is wasitter/wazero, with matching,
pinned WASM releases. T001 explicitly prefers A when B needs extra system
installs; that branch applies here. This decision is grounded in correctness and
distribution evidence, not a claim that A meets the 3× native speed threshold.

Production parser integration belongs in `internal/parse`. No production cgo is
approved. The sole experimental exception is the official cgo dependencies
imported by `spike/parsing/internal/parse/native.go`, gated by `native && cgo`,
for this comparison. No other application package may introduce cgo.

## Consequences

- Normal application builds and cross-compilation need only Go; parsing runs
  offline with bundled version-matched WASM grammars.
- Pin wrapper, runtime and grammar assets together; retain checksums and release
  license notices. Re-run goldens when upgrading them.
- M1 must isolate parser failures per file and manage runtime/parser/tree
  lifetimes explicitly. A parser/runtime is reused sequentially per worker.
- WASM compilation and tree traversal can add costs beyond measured parse time.
  Profile these before setting production concurrency or throughput expectations.
- The spike interfaces are throwaway; neither the indexer contract nor application
  public interfaces are introduced or changed by this ADR.
- T002 loads seven compatible release grammars: JavaScript, TypeScript, TSX,
  Rust, Go, Python and Java (language ABI 14/15 within the runtime's 13–15 range).
  HTML, Angular, C# and Dart have documented source/build paths but no validated
  wasitter-compatible prebuilt assets. Upstream HTML/C# browser WASM releases
  do not instantiate in the pinned adapter. Build and verify WASI assets in CI
  before promising parsing for those formats; do not add client toolchains.
- The T002 real corpus and 1/4/8-worker runs measure process peaks using Windows
  `GetProcessMemoryInfo`, and cold starts in separate processes. Compilation
  repeats per worker/grammar; eight workers cost considerably more memory without
  improving this small corpus's wall time. Do not choose production concurrency
  solely from CPU count. The report includes methodology, raw results and limits.
- If wasitter is abandoned, retain the pinned offline artifacts initially, then
  own a thin wazero adapter around upstream tree-sitter compiled to WASI with a
  small C ABI bridge. It must cover allocation/source transfer, language ABI
  checks, parser/tree/cursor lifecycle, node fields and locations, queries and
  cancellation/error handling. A minimal implementation is estimated at 2–4k
  Go lines plus 300–700 C lines and 1–2k test lines (2–4 engineer-weeks), not full
  compatibility with wasitter's API. No fallback is implemented by T002.

## Known gaps

- Native candidate is implemented but uncompiled and untested here. Re-run the
  comparison on a machine with an existing toolchain; do not install one as part
  of T001. Target C toolchain compatibility remains unknown.
- Process-memory and cold-start measurements are now available in
  [REPORT-T002.md](../../spike/parsing/REPORT-T002.md). Incremental parsing,
  cancellation, query performance and long-running worker memory remain untested.
- Seven grammar goldens and the pinned real corpus complete without parser
  failures/panics using independent workers on Windows. Go/Java have smoke and
  cold-start coverage but no files in this corpus. This does not establish
  arbitrary large-file/large-repository robustness or sharing a runtime between
  workers. HTML/Angular/C#/Dart loading remains unvalidated pending CI builds.
- Cross-platform execution remains for later CI work; only cross-builds passed.
- wasitter's long-term maintenance and broader API behavior remain unverified;
  the fallback estimate is a planning estimate, not demonstrated portability.
- Dart's pinned source LICENSE is MIT-style while package metadata says ISC;
  clarify the discrepancy before redistributing a Dart grammar asset.
