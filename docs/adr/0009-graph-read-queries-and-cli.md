# ADR 0009 — Graph read queries and CLI traversal

**Status:** Accepted (developer review, 2026-10-06)
**Date:** 2026-10-06

## Context

T007b needs filtered node lookup and evidence-preserving edge reads for query,
callers and impact. The persistence boundary must continue to support both SQL
backends without recursive SQL or contract v1 changes.

## Options considered

- Dump the whole snapshot for every traversal: simple but repeatedly reads all
  files, diagnostics and unrelated edges.
- Backend-specific recursive SQL/name expressions: divergent semantics and
  driver-specific JSON and case matching.
- Extend existing node filters and add set-based edge reads in shared SQL;
  deterministic breadth-first traversal in Go.

## Decision

Extend `store.Query` with Term and Limit; `Nodes` runs in a read transaction,
filters exact identity/kind/file/level in SQL and matches name/qualified-name
exactly or qualified-name suffix in Go, with byte-sensitive spelling. Zero
limit means unlimited; negative limits are invalid. Add `Store.Edges` accepting
identity and edge-kind sets plus direction. Read bounded identity chunks inside
one transaction and return deduplicated edges in kind/source/target order.
Postgres uses repeatable read. Existing indexes suffice; no migration is added.

CLI graph reads open existing stores without migrations or directory creation.
SQLite uses its read-only URI; Postgres sets every transaction read-only.
Freshness uses the status scan. Go traversal deduplicates nodes, ignores cycles,
and chooses the first shortest path according to sorted edge order. File impact
targets seed contained symbols. JSON is documented in `docs/cli/queries.md` and
verified against language fixtures on each backend.

## Consequences

Queries remain offline except explicitly configured Postgres infrastructure
through egress. Opening a missing or incompatible store is an error. Node term
filtering reads the SQL-filtered payloads before applying the limit; adding name
columns/indexes remains a measured future optimization. Each store read is
consistent; the complete multi-step traversal does not pin one transaction
across calls, so concurrent indexing may change the graph between traversal
levels. Freshness and graph reads also are separate snapshots.

## Known gaps

No type checking, dynamic dispatch, inherited member resolution or evaluation of
cfg/build conditions. Unknown unresolved counts estimate possibly missing
callers rather than proving a complete impact boundary. Large-graph snapshot
sessions and name indexing require measurements before widening this interface.

## Review notes (developer, 2026-10-06)

- Postgres parity verified in CI: language, CLI and traversal fixtures all
  passed as `/postgres` subtests with `GRAPHVEIN_REQUIRE_POSTGRES=1`.
- The blind-spot count sums every unknown call in the files a result touches
  (299 for `Validate` on the self-index, against 12 results). It does not say
  which calls could reach the target. Replace it in T008 with unresolved call
  sites whose callee name matches the target's simple name, keeping the file
  sum only as context.
- Traversal levels and freshness are separate read snapshots. Acceptable for
  the CLI; revisit before the MCP server (M5) answers agents' impact questions.
- Name filtering happens in Go after SQL filters. Measure on the ~5k-file
  profiling repository (ADR 0006 note) before adding name columns or indexes.
- Go call resolution is the weakest (self-index: 1,995 unknown calls against
  1,651 edges). A type-checked Go extractor using the standard library's
  go/parser and go/types is a candidate for `precise` Go; it needs its own ADR
  before M5.

## Review notes (developer, 2026-10-07, after T008b)

- Blind-spot note resolved by T008b: impact reports repository-wide unresolved
  call sites whose callee simple name matches the target or a reached node;
  the file-level sum is kept as unknown_calls_in_result_files. Postgres parity
  verified in CI.
- Name-only matching overcounts common names: Validate on the self-index
  reports 51 sites against 12 results. Narrowing by receiver type or arity
  needs type information; revisit with the Go precision ADR before M5.

## Review notes (developer, 2026-10-08, after T009)

- On Langflow (67,635 nodes) every query/callers/impact CLI call takes
  10-11 s regardless of result size: each decodes every node before name
  filtering and runs a full freshness scan. Name columns/indexes and cheaper
  freshness are justified; schedule before M5 (MCP), not blocking M2.
