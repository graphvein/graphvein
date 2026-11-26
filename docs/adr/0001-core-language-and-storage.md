# ADR 0001 — Core language, indexer boundary and storage

**Status:** Accepted
**Date:** 2026-10-05

## Context

Graphvein must be fast enough to index large repositories, easy to distribute (including to corporate Windows machines where installing software requires approval), readable to developers learning from its codebase, and usable fully offline. It must also allow a higher-performance indexer to be added later.

## Options considered

- **Python:** slowest; distribution requires managing Python versions and native wheels.
- **Swift:** weak Windows support.
- **Java/Kotlin, C#:** viable, but need a runtime or heavier native builds, and have a thinner ecosystem for this tool's needs.
- **Node/TypeScript:** familiar and one language end to end, but slower and requires Node to run.
- **Rust:** best performance and first-class tree-sitter bindings, but a steeper learning curve, slower edit-verify cycles, and the MSVC toolchain to build on Windows.
- **Go + Rust in one binary:** combines every toolchain problem (Rust, cgo, Go) and makes cross-compiling and debugging hard.
- **Go:** single binary, fast enough, plain and readable, good ecosystem coverage.

## Decision

- The core is **Go**. The UI is **TypeScript/React**, embedded in the binary.
- The indexer is a **replaceable stage** that communicates only through a versioned NDJSON fact stream. A future Rust (or other) indexer runs as a separate process emitting the same contract, rather than being linked in.
- Storage supports **SQLite** (default, pure-Go driver, no cgo) and **Postgres** (team mode) behind one interface.
- Rationale, dispositions and caches are keyed on **node identity + content hash**, not commit hashes, so teammates on different commits share them wherever code is identical.

## Consequences

- Two languages in the repository (Go and TypeScript).
- The contract must be documented, versioned and tested; this is the cost of a swappable indexer.
- Every storage feature must be implemented and tested twice.
- The tree-sitter integration method remains open; see ADR 0002.

## Known gaps

- Raw indexing performance on very large monorepos is lower than Rust's. Revisit with profiling data; the contract makes an external Rust indexer possible without changes to the application.
