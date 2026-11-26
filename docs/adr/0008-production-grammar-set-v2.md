# ADR 0008 — Production grammar set v2

**Status:** Accepted (developer review, 2026-10-06)
**Date:** 2026-10-06

## Context

T007a needs Go and Python extraction. Registering Rust now lets T007b use the same
audited asset set without another asset change. ADR 0006 currently registers
JS/TS/TSX and uses `wasitter-0.1.0-assets-v1`. ADR 0007 requires an explicit
namespace and extractor-version change whenever production assets change.

## Options considered

- Download/build new grammars: disallowed by the task and unnecessary.
- Use cgo/native parsers: violates the production cgo boundary.
- Copy the audited T001/T002 modules with existing checksums, pins and notices:
  preserves the offline pure-Go runtime and existing worker ownership model.
- Register every spike grammar: outside T007a and not justified by its fixtures.

## Decision

Adopt the six-grammar production set below through unchanged wasitter v0.1.0 /
wazero v1.10.1. This **amends ADR 0006's parsing section only for its asset list
and cache namespace**, now `wasitter-0.1.0-assets-v2`. Shared compilation,
independent worker modules, deadline/panic handling and corruption recovery stay
as specified there. ADR 0006 and ADR 0007 are not edited.

All modules are byte-identical copies of the audited spike release assets from
`spike/parsing/grammars/`, originally the wasitter v0.1.0 release built with
Zig 0.15.2. Each module embeds the matching WASI bridge/runtime. The loader
verifies each SHA-256 digest before compilation, including the three new modules.

| Asset | SHA-256 | Source pin |
|---|---|---|
| JavaScript | `7abe2b54935fc1bf33bdcb8f48434676b801bcf62eca104b19b1dd1af18bfa5b` | tree-sitter/tree-sitter-javascript v0.25.0 |
| TypeScript | `e0da1e255655f8e9c078b7f69a9c45198bb2d8e98d5d50e708204101e586346a` | tree-sitter/tree-sitter-typescript v0.23.2 |
| TSX | `764556f90021d52e4cabebf8019d9c729aeb05542f6722a6c818a7cbafb61a18` | tree-sitter/tree-sitter-typescript v0.23.2 |
| Go | `2812c281eeee4a59ab27ae71595a625c1e50619507431a8f85f03edd361ae508` | tree-sitter/tree-sitter-go v0.25.0 |
| Python | `0b4396e688a08f9d2c6a11de2b018bdda63eef7b59497ee1fd56481cc96f5423` | tree-sitter/tree-sitter-python v0.25.0 |
| Rust | `2cdd61ffe54efabf991b04f8ef9915e60822e8bd61e5f0fd677aa84061346ade` | tree-sitter/tree-sitter-rust v0.24.2 |

The existing production `grammar-registry.json` already retains these pins and
source archive digests. The existing complete `THIRD_PARTY_NOTICES.txt` already
includes their MIT grants and the bridge/runtime, Unicode/ICU, endian, wasi-libc
and Zig notices. The copy was verified identical; no notice text was removed.
`SHA256SUMS` adds the three module hashes. No HTML, Angular, C# or Dart production
grammar is registered. Rust remains file-level until T007b.

Use aggregate extractor version `extractors-2` for all supported source caches.
Scanner language names and new language semantics invalidate previous cached
facts even though contract v1, schema, dependencies and store interfaces remain
unchanged. Shared source facts/candidates move to `internal/lang/facts` rather
than coupling the new extractors to TypeScript; per-language linkers relink
against the current inventory. Go module declarations are read from scanned
go.mod files on every relink. No filesystem fact sidecar is introduced.

## Measurements

Windows/amd64, installed Go 1.27.1; no downloads, installs or cgo. The existing
measurement harness primes all six grammars with tiny sources. Cold/warm runs
use separate child processes and fresh SQLite stores; only the compilation cache
survives between them. Startup includes compilation, instantiation, tiny parses,
detachment and worker cleanup for all six grammars, not just loading asset bytes.
Individual samples have warm OS caches and are not throughput guarantees.

| Workers | Cache | Six-grammar startup ms |
|---:|---|---:|
| 1 | cold | 772.5029 |
| 1 | warm | 81.7619 |
| 4 | cold | 762.1531 |
| 4 | warm | 80.6624 |

Measured while indexing the pinned FastAPI backend; corpus counts/timings and
reproduction commands are recorded in the T007a task record (internal). Startup is
measured separately before corpus work, so fact-cache replay cannot skew it.

## Consequences

The Go binary grows by 2,474,779 embedded WASM bytes for Go/Python/Rust.
First compilation costs more than the three-grammar set; the disk cache reduces
subsequent process startup. Existing per-worker mutable state remains isolated.
Every new language remains `best_effort`; unresolved calls are counted rather
than assigned speculative endpoints. Standard/third-party imports use ecosystem
external nodes without diagnostics; failed Python relative imports and missing
Go in-module imports are diagnosed.

The identity document explicitly records Go init and Python redefinition
declaration-order suffixes. These exceptional repeated declarations cannot have
unique plain names, so `@N` preserves every declaration without using line
numbers or hashes; insertion/reordering can change their association.

## Known gaps

No Go type checker or Python execution/import machinery; dynamic dispatch,
assignment inference, inheritance, wildcard imports and re-exports remain
unresolved. Go build constraints are indexed without evaluation; test visibility
follows the importer. Anonymous Go functions' calls conservatively remain
unresolved. Postgres parity is exercised by the Linux CI service; local runs
have no configured database and skip, while `GRAPHVEIN_REQUIRE_POSTGRES=1`
fails for a missing/unreachable database. Cross-platform execution remains CI
work; these measurements are Windows only. Rust extraction and CLI graph queries
remain the implementation and were not started.

## Review notes (developer, 2026-10-06)

- Postgres parity verified in CI: the Linux job ran with
  `GRAPHVEIN_REQUIRE_POSTGRES=1` and logged no skipped tests.
- Python module-member calls (`crud.create_user()` after `from app import crud`)
  remain unresolved and dominate FastAPI's unresolved count. Resolve in T007b;
  M2 route-to-handler-to-data tracing depends on it.
- Unresolved-call counts mix standard-library/third-party callees with truly
  unknown ones (self-index: 2,941). Separate them in T007b so `callers`/`impact`
  can report real blind spots.
- The single aggregate extractor version invalidates every language's cache on
  any one change. Move to per-language versions before M7 adds languages.
- Six-grammar peak memory was not measured. ADR 0006's memory-profiling note
  (~5k-file repository, before M2) remains open and now covers this set.

## Review notes (developer, 2026-10-08, after T009)

- Six-grammar memory measured on the self-index: private bytes minus Go
  runtime total is 48-50 MiB across three samples. Compilation (<1 s) is
  negligible against index time. The six-grammar memory note is resolved;
  per-language extractor versions remain open.
