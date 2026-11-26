# ADR 0073 - A store read of every edge of one kind, for the code calls coverage count

**Status:** Accepted (delegated review, 2026-11-18)

## Context

`/api/v0/precision` counts the facts this index holds in each labelled
family (ADR 0066). The `code_calls` family (code `calls` edges, row D,
T020 O-S1-5 (a)) had no count: ADR 0068 lists "no `code_calls` column in
the coverage table" as a known gap because it "would need a store query
by edge kind".

The `store` interface reads edges only by endpoint (`Edges(ctx, repo,
identities, kinds, direction)`). A `calls` edge's source is not always a
function or method: the built-in indexer also writes them from files,
classes and variables (fastapi: 5 file and 1 variable sources among 418),
so a fixed list of source kinds could miss some. Reading every node and
asking `Edges` for their outgoing `calls` edges is exact, but on the
self-index (20,646 nodes, 44,884 edges, 14,348 calls) it took
`/api/v0/precision` from about 65 ms to about 1.1 s, for a popover.

## Options considered

1. Every node's outgoing edges through the existing interface. Exact, no
   interface change, about 1.1 s on the self-index. Rejected as too slow
   for a view the Evidence popover fetches on each open.
2. A fixed list of source kinds (function, method, file, class,
   variable). Faster, but a kind the list misses undercounts silently: a
   wrong count in a view that states what the store holds. Rejected.
3. A read-only store method `EdgesOfKind(ctx, repo, kind)` that selects by
   the edges table's primary key prefix `(repository, kind)`. Exact,
   about 190 ms on the self-index, no schema change. Selected.
4. A counting method (edges by kind and source file). Faster still, but a
   second, narrower read for one view; the edge list is enough today.

## Decision

Add `EdgesOfKind(ctx, repository, kind) ([]contract.Edge, error)` to the
`store` interface, implemented once in `sqlstore` for SQLite and Postgres:
`SELECT payload FROM edges WHERE repository=$1 AND kind=$2`, sorted in Go
by `(kind, source, target)` in byte order like `Edges`. It is tested on
both backends (`internal/store` `TestEdgesOfKind`: the same edges as
`Dump` and as `Edges`, byte order where a collation would differ, no edge
of another kind or repository, an empty list for a kind with none).

`/api/v0/precision` serves `code_calls` as a sixth `default.families`
entry with unit `edges`, counted by the language of the edge source's file
(as PL-F9's edges are). Its label is decided by `labelOf` from the family
marker alone, the same decision as `/flow`'s and `/source`'s
`code_calls_precision`, so it is `best_effort` from row D on every run and
never follows a file label (`TestCodeCallsNeverPrecise` checks the count
against `Dump` on a run whose TypeScript files are stood in as Precise).

## Consequences

- The coverage table gets a "Code calls" column (Best effort, row D)
  wherever the index holds a `calls` edge, so a Go or Python file counted
  Precise shows its call edges beside it as Best effort.
- No contract change, no `contract_version` bump, no extractor version
  bump, no migration, no new stored data.
- `/api/v0/precision` reads every `calls` edge payload per request
  (about 190 ms on the self-index against about 65 ms before).

## Known gaps

- The count decodes whole edge payloads; option 4 would avoid that if the
  popover's latency matters on much larger indexes.
- Only `calls` uses the new read; other views still read edges by
  endpoint.

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17). Step FU (T020 follow-ups: the `code_calls` family in `/api/v0/precision`, C3 dialect note on Precise model source cards, C5 resolved as the `no_run` reason) and one independent review (the T020 task record (internal)): no class A finding; B1 (a cross-language `code_calls` count test) and C5 (where `no_run` appears) fixed in the unit.

- `EdgesOfKind` is a read-only query on the edges primary-key prefix (repository, kind); no schema change; tested on SQLite and Postgres (`TestEdgesOfKind`). `code_calls` is always best_effort on row D and its total equals the stored `calls` edges (`TestCodeCallsNeverPrecise`, `TestCodeCallsCountByLanguage`).
- C5 as written (an empty indexer name) cannot occur: `header.schema.json` requires `indexer.name` with minLength 1 and `BeginRun` validates every header. The real defect was a repository with no run being described as `external_indexer`; it now reads `no_run`.
- Review notes kept as follow-ups: C1 the C3 note is on the source card only, not the set strip; C2 SQLModel (no stated dialect, every fact oracle-verified) deliberately gets no note; C3 `/precision` reads every `calls` edge per request (about 190 ms on the self-index; a count-only read is the known gap above); C4 the message of commit e49b8e5 still names this ADR by its earlier number 0070 (pushed history is not rewritten).
- Gates: local full check 0 FAIL, 175 `/postgres` PASS; check-web 1,130 tests; CI run 44240876687 (attempt 2) on fa63d8a green, postgres raw log 0 FAIL, 176 `/postgres` PASS, Windows 0 FAIL.
