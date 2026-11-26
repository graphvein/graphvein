# ADR 0066 - Precision labels: a central rule table, file labels for language structure, scope labels for fact families

**Status:** Accepted (delegated review, 2026-11-16)

## Context

Contract v1 gives every file record a `precision` of `precise` or
`best_effort` (`docs/contract/v1.md` "File"), and the indexer copies it to
the file node's `attributes.precision`. The scanner writes `best_effort`
for every file (`internal/scan/scan.go:366`); no API view or UI reads it
(the T020 task record (internal) §4). Milestone M2 of the project plan asks for `precise` where a
language earns it, shown in the UI; VISION principle 6 asks for honesty
about precision per language, and "Target languages" says every file and
node carries its language's label.

The developer fixed the bar (2026-11-09): every extractor and linker ships
with a claim contract, golden fixtures, corpus checks, one independent
review and an oracle where cheap; unknowns, `oracle` / `mapped`
verification, assumptions and confidence carry the rest. A Java/C#/Dart
tier (T021) will be labelled `best_effort` throughout.

The inventory (§2) finds that the full bar is met today by fact families,
not by languages: SQL migration schema (ADR 0047, 0051), the seven ORM
front ends (ADR 0054, 0057, 0059, 0061-0064) and code-to-table links (ADR
0060) have claim contracts with 1:1 tests, goldens, corpus checks,
oracles and recorded independent reviews. The first-tier languages'
structural extraction (declarations, imports, calls) has goldens and
corpus checks but no claim contract and no recorded independent review;
routes, client calls, Angular, Tauri and OpenAPI have goldens and corpus
checks, some independent reviews, and no claim contract.

A single TypeScript file can hold facts from several scopes (structure,
Express routes, fetch calls, Drizzle tables, table-access edges), so "the
file's precision" and "the precision of a fact in it" differ.

## Options considered

**Definition of `precise`.**
1. The full v1 bar per scope, checked by a test from named evidence
   (contract test, goldens, corpus record, review entry, oracle or a stated
   reason none is cheap) and pinned to the extractor version. Selected.
2. "Has a dedicated extractor with goldens and corpus checks": would make
   the first-tier languages precise at once without the contract and the
   review the bar requires. Rejected as not precision-safe.

**Granularity.**
1. File label only (contract v1 as is). Cannot say that a Prisma table is
   precise while its schema file and the TypeScript that queries it are
   not.
2. File label for the language's structural scope, plus scope labels for
   fact families computed by the server from the same table and attached
   to the views that already know their family. No contract or store
   change. Selected.
3. Per node and edge, stamped by the indexer. Node `attributes` are open,
   so a node attribute needs no `contract_version` bump; edges have no
   attribute map, so an optional edge field changes the contract docs and
   schemas and makes older consumers report `unknown_field`. Stamped
   attributes also change synthetic node hashes, so a label flip would
   expire dispositions. Rejected for v1.
4. File label as the weakest scope found in the file: almost every
   application file would read best effort whatever its language earns,
   and the label would change with content. Rejected.

**Where the label is decided.**
1. The scanner switches per language: no link to evidence, nothing keeps
   it honest.
2. Each extractor declares its label: no single table to keep 1:1 with
   the task and docs.
3. A central package `internal/precision` with the rule table (rows that
   earn `precise`, the default branch `best_effort`), used by the scanner
   for file labels and by the server for scope labels, and a test that
   checks every row's evidence and keeps table, task file and docs 1:1.
   Selected.

## Decision

- `precise` for a scope means its facts on the forms its claim contract
  lists meet the v1 bar, and what it cannot read is shown as unknown. It
  never means complete and never replaces a fact-level label.
- The rule table in `internal/precision` lists one row per scope that
  earns `precise` with its evidence and refusal condition; everything else
  falls to the single default branch `best_effort`. `TestPrecisionRules`
  checks each row's evidence, keeps the table 1:1 with
  `docs/contract/claims/precision-labels.md` and `docs/extract/precision.md`,
  forbids rows naming java, c_sharp or dart in v1, and pins the table's
  digest to `index.Version`.
- The file label (contract v1 field) is the language's structural scope.
  Today no language has a row, so every file stays `best_effort`; each
  first-tier language earns a row through its own structural claim
  contract, oracle where cheap and independent review (T020).
- Fact-family scope labels are computed by the server (`ScopeLabel`) for
  schema groups, table-access edges, endpoints and the flow graph, and
  served with `/api/v0/precision`. Rows today: SQL migration schema
  (postgres sqlx sets), SQLModel/SQLAlchemy, Prisma, Drizzle, Sequelize,
  Diesel, GORM, TypeORM, code-to-table links.
- A label applies only to runs of the built-in indexer whose extractor
  version equals the binary's; otherwise the API serves `best_effort` with
  a reason, and an external producer's own file label is shown as
  reported by that producer.
- The UI shows the label beside, never instead of, provenance and
  confidence, `oracle` / `unverified`, unknown and partial; the variant is
  chosen in a mockup round.

## Consequences

- No contract v1 change, no `contract_version` bump, no store migration.
- The implementation's first units change no golden: every file label stays
  `best_effort` until a structural row exists. A later flip changes only
  `precision` values in file records and file nodes of that language,
  bumps `index.Version`, and appears in change impact as one C1 item per
  file with cause `extractor_or_relink`.
- Labels are not cached and not stored per fact; the fact cache stays
  keyed by content hash and extractor version, and the file node is a
  source node whose hash is the file's, so a flip expires no disposition.
- At the start the UI shows Precise on schema, ORM and code-to-table
  facts and Best effort on every file and on routes, clients, Angular,
  Tauri and OpenAPI. That is accurate to the evidence, and it states
  plainly how much of the first-tier language support has not yet met
  the bar.
- The T021 tier needs nothing from this ADR: its languages have no row
  and the guard keeps it so.

## Known gaps

- Structural scopes need four new claim contracts (S1-S4); until then,
  VISION's "precise for the languages that earn it" is met by none.
- Scope labels cover only views that know their family; a future generic
  node view or the architecture graph must call `ScopeLabel` too.
- A precise scope's mapped dialects (mysql, mariadb, mssql) rely on the
  per-fact "unverified" chip to show they were not oracle-compared.
- Calls are their own scope (recommended) and stay Best effort until a
  call contract exists, even in a language whose structure flips.
- Evidence checks in `TestPrecisionRules` verify that named artefacts
  exist and are cited, not that the review was good; the independent
  review of T020 (R1) reads every row by hand.

## Implementation notes (T020)

- `internal/precision`: `rules.go` holds the table (nine family rows
  PL-F1..PL-F9, no file row), `FileLabel(language)` for the scanner and
  `ScopeLabel(run, marker)` for the server. `ScopeLabel` applies the run
  refusals (`external_indexer`, `extractor_mismatch`) before any row, so a
  caller cannot skip them; it returns the label, the deciding row (`D` for
  the default branch) and the reason. `index` imports the scanner, which
  imports `precision`, so the caller passes the binary's `index.Version`
  in `Run`.
- Markers: `Family` (`schema_migrations`, `schema_models`, `table_access`;
  any other family falls to D), the group's `tool` and the migration set's
  `dialect`. PL-F1 needs tool `sqlx` and dialect `postgres`; model rows
  match on `tool` only (O13).
- Evidence: P3 and P4 are verbatim quotes from the ADRs' "Review notes"
  sections, compared after collapsing whitespace and dropping Markdown
  bold markers.
- P6: the test keeps an append-only pin list of (extractor version,
  table digest). A changed digest at the pinned version fails ("bump
  index.Version"); an unrelated version bump with an unchanged table
  passes, so other tasks do not touch the pin.
- The scanner calls `precision.FileLabel`; every label is still
  `best_effort`, no golden changed and `index.Version` stays
  `extractors-37`.

## Implementation notes (T020)

- B2 added two read-only methods to the `store` interface, `Files(ctx,
  repo)` and `FilesByPath(ctx, repo, paths)`, implemented once in
  `sqlstore` and tested against both SQLite and Postgres
  (`internal/store` `TestFiles`). The file label and per-language counts
  need the stored file record's `language`, which file nodes do not
  carry. No schema change, no migration, no new stored data; accepted in
  the delegated review of 2026-11-16.
- The API serves every label (`/precision`, `/source`, `/schema/sets`,
  `/schema/table`, `/api/endpoints` per transport (added in B3),
  `/api/endpoint`, `/api/flow`, `/changes/items`); the UI (B3, Variant A)
  shows exactly what it receives: no label, no chip; an unknown label word,
  no chip; an external run, no scope chip and the producer's file label
  only as "reported by <indexer>". The coverage table's languages and
  families come from `/precision`, never from a map in the UI.

## Implementation notes (T020 F1, review fixes)

- A sixth label reason, `marker_conflict`: `precision.Marker` carries
  `Conflict` when a schema group's records name more than one tool or
  dialect (or some name none), and `ScopeLabel` returns it instead of
  `default_branch` when no row matches. It never makes a row match. The
  UI uses the "no claim contract" wording only for `default_branch`.
- One list of reasons: `precision.Reasons()` plus the server's
  `stored_label_invalid` and `scope_unknown` (`labelReasons` in
  `internal/server/precision.go`), written to
  `internal/server/testdata/precision-reasons.golden.json`; the web tests
  keep the UI's words 1:1 with it.
- `/precision` `files` counts by Graphvein's label (`fileLabelOf`, run
  refusals first); an external producer's stored values are counted apart
  in `reported_by_label`.
- `/changes/items` `precision.scope_side` names the side whose run decided
  the item's scope and field labels (`base` when the base run is refused).

## Review notes (2026-11-16)

Accepted after delegated review on 2026-11-16.

- **Developer decisions:** open points O1-O14 took their recommended defaults (2026-11-16), under the core principle of maximum coverage with the best user experience and no confident wrong fact. The UI is Variant A with Variant B's coverage table in the Evidence popover, wording "Precise / Best effort".
- **Steps:** B1 rule table and scanner wiring; B2 API (`/api/v0/precision`, labels on `/source`, schema groups and tables, endpoints, flow and Changes items, per-language and per-family fact counts; two read-only store methods `Files`/`FilesByPath`, tested on both backends, no schema change); mockup round M; B3 UI; R1 independent review; F1 review fixes.
- **Review (R1, the T020 task record (internal)):** no class A finding. Every precise label passes through `precision.ScopeLabel` or `precision.FileLabel` after the run refusal. PL-F1..PL-F9 were checked against their evidence, and none is broader than it (a Prisma `dynamic` provider is covered by PR-B1, which makes provider-dependent facts unknown; mapped dialects keep `verification: mapped` and the `unverified` chip). The class B findings were fixed: wording by reason with one reason list shared by the server and the UI, a restored source-span warning, file counts by Graphvein's label after refusals, and refusal tests over eight views for external and mismatched runs and a refused base side. C1 and C2 were fixed.
- **Results:** no golden changed except additions of label fields and new `precision-*` goldens; extractors-37 is unchanged; the Langflow snapshot is byte-identical apart from `generated_at`. The full suite passed locally with Postgres 17. CI run 43922611178 on 4bb2a58 was green on Windows and postgres; the postgres raw log shows 0 FAIL, 168 `/postgres` PASS and only the environment-gated skips. UI screenshots were checked at 900, 1280, 1440 and 1920 px in light and dark, with no horizontal overflow.
- **Coverage:** every file is `best_effort` today; the schema (sqlx, postgres), the seven ORM front ends and code-to-table links show Precise on every corpus that has them; routes, client calls, calls, Angular, Tauri and OpenAPI are Best effort (row D).
- **Known gaps and after-v1 items:** the structural scopes S1-S4 (Go, Python, TypeScript/JavaScript, Rust) are T020 follow-on units; review follow-ups C3 (no dialect hint on a Precise group's source card), C4 (two review citations quote revision lines), C5 (an empty external indexer name has no reason of its own), C6 ("Partial · 0 reasons" on model tables, pre-existing), C7 (store interface change recorded here), C8 (the popover fetches `/precision` separately from its view) and C9 (schema edge change items carry no scope label, which under-claims).
