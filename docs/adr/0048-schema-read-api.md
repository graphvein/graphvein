# ADR 0048: Schema read API

**Status:** Accepted (delegated review, 2026-11-04)

## Context

T018a (ADR 0047) stores the schema of sqlx SQL migrations as `migration`,
`table`, `column` and `db_index` nodes with `contains`, `alters` and
`foreign_key` edges and `schema_*` diagnostics. Many of its values are
deliberately unknown: partial tables, `null` for facts source does not
prove, `applied: null`, opaque statements counted per set in
`set_opaque_statements`. The VISION precision principle
require that a view never shows an unknown as a known value. The schema view
(T018b2) and the shell's Schema entry need an HTTP API for these records.
The developer decisions D1-D8 of the schema plan (placement, layout,
grouping, history, unknown states, column types, sources, drift) must all
remain possible.

The server's API conventions (ADR 0041 security chain, ADR 0042 identity
in `?id=`, raw values, runs-before/after check, error envelope, ADR 0045
caps with `truncated`) apply. The store had no read of diagnostics except
`Dump`, which decodes the whole repository.

T018a merged without bumping the aggregate extractor version, so an index
built with the schema pass and one built before it both carry
`extractors-27`.

## Options and decisions

1. **Endpoint granularity.** One `sets` response with every group, table,
   column, index and foreign-key pair, plus `table` and `migration` details
   (chosen: a diagram needs the whole set at one index state; details carry
   presentation and history). Rejected: per-table lists only (a diagram
   needs N requests that can mix index states); a generic graph query (every
   client would re-implement the state rules).
2. **Raw attributes plus a few derived fields** with exact rules in one
   place, `internal/server/schemadata.go` (chosen). Rejected: flattened
   renamed fields (would fix T018a's names into the API and hide unknown
   values); raw only (every client re-implements completeness and defaults,
   where precision errors creep in). Each derived field falls back to
   `unknown` or null, never to `false`, `0`, `none` or `complete`.
3. **Grouping by the stored `set` key** with a `source` field (chosen;
   serves D3 and D7, models can add `source: "models"` groups). Rejected:
   merging by table name (would merge different databases); grouping by
   dialect.
4. **No type normalisation** (chosen; D6 can add `type_normalized` later).
   Rejected: normalising Postgres aliases now (wrong under search_path,
   domains, extension types).
5. **Foreign keys only from stored edges**; `references` text is never
   resolved (chosen). Rejected: resolving `references.table` by name (a
   guess where T018a said unresolved).
6. **A new store read** `Diagnostics(ctx, repository, files)` in stored
   order, one read transaction, files chunked by 128, using the existing
   `diagnostics_file` index (chosen; no storage-schema change, no
   migration). Rejected: `Dump` per request (whole repository; 0.5-0.7 s on
   the synthetic store against 20-50 µs); no diagnostics (the `objects` and
   `table_list` rules need `schema_migration_order`,
   `schema_member_unreadable` and `schema_unsupported_config`, and inline
   reasons need the statement diagnostics).
7. **Consistency** by the existing runs-before/after check with one retry,
   then 503 `index_changed` (ADR 0042 §6).
8. **No scope filter** (chosen; D3 hides test-only sets in the UI by
   default, from the derived `test_only`). Rejected: a production default
   in the API.
9. **Caps:** 200 neighbour tables per direction (`truncated`), 500
   diagnostics per migration (`diagnostics_total` uncut), counts in `sets`
   uncut; no paging of `sets` (revisit above 2,000 tables or when the
   latency target fails).
10. **The D1 count:** a separate `GET /api/v0/schema/summary` that reads only
    runs, migration nodes, file paths and the manifests' diagnostics
    (chosen). Rejected: a field on `/api/v0/status` (polled with a freshness
    walk, documented shape, four status goldens); the shell reading `sets`
    (the whole schema at every load). `found` keeps `unknown` apart from
    `none`.
11. **Proving the schema pass ran** (decision, T018b1
    Revision 1 X20): bump the aggregate extractor version from
    `extractors-27` to `extractors-28` in `internal/index`, so
    `firstSchemaVersion` is 28 and `found: none` can be stated for a
    graphvein index without sets; `extractors-27` reads `unknown` with
    `version_ambiguous`. Rejected: leaving 27 (every index stays `unknown`
    until an unrelated bump); recording the pass in the run header (a
    contract change). The bump changes no contract, record or storage
    schema: its only effects are a one-time reparse (the fact cache key
    includes the version) and the status extractor badge asking for a
    re-index of older stores.
12. **Sets the index holds nothing complete for** (T018b1 review fixes
    R1-R3). T018a classes table-creating forms it does not model
    (`SELECT ... INTO t`, `CREATE TABLE t AS`, `CREATE SCHEMA s CREATE
    TABLE ...`) as unsupported, not opaque, so they are outside the set
    record: `table_list` gains the reasons `unsupported_statements` (a
    migration's integer `unsupported` above 0, or a
    `schema_unsupported_statement` diagnostic) and
    `statement_counts_unknown` (a migration not exactly `applied: false`
    without an integer count), chosen over a per-form rule because the
    stored counts do not say which form was unsupported (conservative: a
    bare `CREATE SCHEMA` or a view also gives `may_be_incomplete`). A set
    T018a found but emitted no record for (every member unreadable) is
    listed in `sets_without_records`, a list of its own rather than
    `failed_sets` because the pass did not fail on it; any `schema_*`
    diagnostic other than `schema_internal_error` and
    `schema_manifest_unreadable` on a stored `Cargo.toml` that no
    migration names identifies it, so a future T018a code is covered. T018a
    now reports a `Cargo.toml` it cannot read with
    `schema_manifest_unreadable` (listed in `unreadable_manifests`), and
    `summary` answers `unknown` with `manifest_unreadable` instead of
    `none` when one exists and no set is found. Both are counted in
    `summary`, and record-less sets make `found` `sets`.
13. **General principles of the review fixes R4** (T018b1, second review
    round), applied as rules rather than case patches:
    - **P1 "none must be proven".** `found: none` is stated only when the
      index proves that no Cargo manifest could have been missed. The
      scanner (`internal/scan` `Walk`) writes five diagnostic codes, each
      for something it did not read: `scan_read` (a directory could not be
      listed), `symlink` (not followed; the diagnostic does not say whether
      it points at a directory or is a `Cargo.toml`), `invalid_path` (an
      entry with an unsupported path was skipped; its file is empty and it
      may be a directory), `ignore_read` (an ignore file could not be
      read; a lost negation can exclude a manifest) and `file_read` (a
      regular file could not be read). Each of the first four on any path,
      and `file_read` on a path named `Cargo.toml`, is a *scan gap*:
      `/sets` lists it in `scan_gaps`, `summary` counts it, and `found`
      is `unknown` with `scan_incomplete` (checked before
      `manifest_unreadable`). A `symlink` or `file_read` of a `Cargo.toml`
      is also an `unreadable_manifests` entry, whose new `code` field names
      the diagnostic it comes from. `TestScanNotReadCodes` fails when the
      scanner gains a code the table does not list. Chosen: a new store
      read `DiagnosticsWithoutFile(ctx, repository)` (diagnostics whose
      file has no `files` row, one query with `NOT EXISTS`, no schema
      change), because scanner diagnostics sit on paths that are not
      stored and cannot be named in advance. Rejected: `Dump` (whole
      repository); a store filter on the diagnostic code (no code column;
      decoding every payload); having T018a re-emit scan diagnostics on a
      known file (T018a does not see them, and there may be no manifest to
      attach them to). A `Cargo.toml` excluded by ignore rules or the
      default exclusions is outside the index: `none` means none among the
      indexed manifests (docs/cli/serve.md).
    - **P2 "no silent drops in discovery".** Every T018a discovery path
      that skips something because it could not read it reports it. A set
      directory listing that fails for a reason other than absence gives
      `schema_member_unreadable` on the manifest and makes every table of
      the set partial (a set with no stored member becomes a set without
      records). A manifest whose `package`, `workspace`,
      `workspace.dependencies`, `dependencies`, `dev-dependencies`,
      `target`, `target.X` or its dependency tables is not a table, or
      whose `package.workspace` is not a string, is unreadable
      (`schema_manifest_unreadable`) instead of "no package" or "no sqlx".
      The remaining skips are not read failures: a manifest without
      `[package]` (a virtual workspace), a package without a sqlx
      dependency, a missing `migrations` directory, entries that are not
      sqlx file names or not regular files.
    - **P3 "INTO after SELECT is unsupported".** Every `INTO` keyword at
      any paren depth of a select-like statement that is not `INSERT
      INTO`, `MERGE INTO` or a column label (`AS into`, `x.into`) makes
      the statement unsupported (strings, quoted identifiers and comments
      are not keyword tokens). The leftmost SELECT's INTO (also inside
      parentheses or after WITH, analyze.c `transformOptionalSelectInto`)
      creates a table; every other INTO may be refused by Postgres ("INTO
      is only allowed on first SELECT", "SELECT ... INTO is not allowed
      here", syntax errors), so the migration may fail. Each of the 24
      forms of `TestSelectInto` and the fixture package `pg-select-into`
      was run on Postgres 17.11: zero disagreements.

## Consequences

- The schema view reads four endpoints and must follow the reading rules
  in docs/cli/serve.md ("Reading the schema states").
- Store implementations gain `Diagnostics` and `DiagnosticsWithoutFile`;
  test wrappers that fail or count reads override them.
- Every existing index reads `version_ambiguous` until it is rebuilt with
  this binary.
- The modeled-dialect list (`postgres`) is a server constant that must
  follow T018a's dialect support.

## Known gaps

- Dropped tables and intermediate states are not available (T018a emits the
  final state); a later ALTER changes a column's attributes without changing
  its hash, so `file_state` may say unchanged for a changed meaning.
- Diagnostics are message text without spans (contract v1 has none).
- Unresolved references into a table are not attached to it (no edge).
- `sets` is unpaged: on the synthetic store (2,000 tables, 30,000 columns)
  it answers in about 1 s with a 17.8 MB body, above the 500 ms target; the
  time is in decoding stored payloads (about 70 %), JSON encoding (about
  16 %) and grouping (about 7 %) (pprof of the in-process handler, T018b1
  summary: 0.97-1.22 s, 17,831,352 bytes). T018b2 must not fetch `sets`
  more often than it needs (once per index state, not per view change);
  a lean form without stored attributes or a `set=` filter is a future
  option if the view needs to load faster.
- `table_list` reads per-migration counts, not statement forms, so
  `unsupported_statements` also marks sets whose unsupported statements
  create no table (a bare `CREATE SCHEMA`, a view, `VACUUM`).
- Whether a set in `sets_without_records` is test-only is unknown (the
  server has no test-scope rules; T018a emits no migration node with
  `scope`).
- Reads are separate transactions (retry, then 503); `file_state` and IDE
  links read each presented file per request.
- Several sets of one package share its `Cargo.toml` and `sqlx.toml`, so a
  manifest diagnostic counts for each of them (conservative: it can only
  add `may_be_incomplete` or `not_modeled`).
- With no `migration` and no `table` nodes, `sets` reads no columns or
  indexes, so unattached columns of such an index are not reported.
- The scanner drops a file that is not UTF-8 or contains a NUL byte, and
  any entry that is not a regular file, directory or symlink, without a
  diagnostic, so a binary or special `Cargo.toml` leaves no trace and
  `none` cannot see it (Cargo refuses a non-UTF-8 manifest too).
- Every `symlink` diagnostic is a scan gap, also for a symlink to a file
  not named `Cargo.toml`, because the diagnostic does not record the
  target's type.
- The P3 leftmost rule is approximate: an INTO after a depth-0 `FROM`,
  `FETCH` or similar word inside the target list (`a IS DISTINCT FROM b`,
  a column named `fetch`) is treated as one Postgres may refuse
  (conservative). Statements that are not select-like are not covered:
  `INSERT ... SELECT ... INTO t` and `CREATE VIEW v AS SELECT ... INTO t`
  are refused by Postgres but classed as before.

## Review notes (2026-11-04)

Delegated review, 2026-11-04. Records: the T018b1 task record (internal)
(Revision 1 and its review-fix addenda) and the T018b1 task record (internal).

- **Results** (gates on the stacked head of 2026-11-04, the T018b1 task record (internal)
  "Gates (2026-11-04, stacked head)"): full suite with a scratch Postgres
  17.11 exit 0, 1,288 PASS lines, 98 `/postgres` PASS, only the four helper
  skips plus `TestSourceStates/{sqlite,postgres}/unreadable`; web checks ok
  (650 tests). Ten corpora: every body byte-identical, header only
  `extractors-27` -> `extractors-28`; the private corpus not run. Self-index differs
  only by the T018b1 fixtures (`bad-manifest` F3 diagnostic and the R4 P3
  SELECT INTO correction on `pg-select-into`). Langflow timing fresh +0.6%,
  warm +0.7% (warm parses 0); axum-ddd within +1 s; store size identical
  (Langflow 935,981,056 B, axum-ddd 3,317,760 B). Schema latency (SQLite,
  warm medians): axum summary 1.3 ms / sets 5.0 ms, axum-ddd 1.4 / 5.6,
  Langflow 25.7 / 33.1 (`found: none`), self-index 6.0 / 17.0, table and
  migration details at most 7.3 ms; all under target. The synthetic
  2,000-table `sets` stays about 1 s (Known gaps). Playwright: the Schema
  page equals `/schema/sets` on axum and axum-ddd, 0 console errors, 0 CSP
  violations. CI: [ci windows] batch run on dd42753 green.
- **Revisions.** R1-R3 (F1-F4): `unsupported_statements` and
  `statement_counts_unknown` as separate table-list reasons;
  `sets_without_records`; T018a reports an unreadable `Cargo.toml` as
  `schema_manifest_unreadable` (`unreadable_manifests`, `found: unknown`
  with `manifest_unreadable`); store diagnostics errors name the
  repository and chunk. R4 applied three principles as rules rather than
  case patches (option 13): P1 "none must be proven" (every scanner
  not-read code is a scan gap, `scan_incomplete`, new store read
  `DiagnosticsWithoutFile`); P2 "no silent drops in discovery" (set
  directory listing errors and wrong-typed manifest values are reported);
  P3 "INTO after SELECT is unsupported" (only the leftmost SELECT's INTO
  creates a table, any other makes the migration may-fail),
  oracle-checked on Postgres 17.11 with zero disagreements. A fresh re-check of R4
  converged with no finding reopening T018b1.
- **Known gaps** (above, plus the summary's follow-ups): `sets` unpaged
  and slow on very large schemas (paging, `set=` or a lean payload later);
  no dropped tables or intermediate states; diagnostics without spans;
  scanner drops binary and special files without a diagnostic and
  `symlink` does not record the target type. Backlog for T018a2 (claim
  contract and oracle): `SELECT INTO t DISTINCT`, `AS into`,
  PREPARE/EXECUTE of SELECT INTO, `WITH ... (SELECT ... INTO t)`,
  `INSERT ... SELECT ... INTO` and `CREATE VIEW ... SELECT INTO`, and B3
  (a renamed workspace-inherited sqlx key whose workspace root is missing
  or unreadable is skipped without a trace).
