# ADR 0058 - Model-record kit: tool-neutral core, group key, claims and verification

**Status:** Accepted (delegated review, 2026-11-09)

## Context

ADR 0054 put the SQLModel linker in `internal/extract/models`: one package
that resolves Python model classes and also builds the table and column
records, resolves foreign keys (row L4), compares with migration sets (L5),
isolates steps and emits the nodes (L3). The 2026-11-09 plan
(the T018 task record (internal), "ORMs in the other first-tier
languages") adds six more ORM front ends (Prisma first, T018g1), each of
which would otherwise copy the emitter. Three developer decisions of
2026-11-09 change the shared record:

- G3: one model group per ORM tool and schema root, keyed
  `set: "models/<tool>/<root>"` (ADR 0055 already groups by the stored
  `set`); SQLModel's tables stored `set: "models"` until this task.
- Model columns gain `claims` per known fact, as migration columns did in
  T018f (ADR 0055 option 1A), so the contract row behind every model fact
  is answerable.
- Every model fact gains `verification`: `oracle` (checked by the ORM's
  oracle on that dialect) or `mapped` (taken from the ORM's own per-dialect
  mapping source, not checked against that database). Prisma's MySQL, SQL
  Server and CockroachDB facts will be `mapped` (T018g1, PR-V6); the view
  marks them "unverified".

This ADR amends ADR 0054's package layout and record shape. It does not
change ADR 0054's decisions on what SQLModel claims.

## Options considered

1. **Package layout.**
   - A. Core in `internal/extract/models` (package `models`), SQLModel in
     `internal/extract/models/sqlmodel`, later tools beside it
     (`.../prisma`, ...) (chosen: the core keeps the package path ADR 0054
     named for "models"; every front end imports the core and never the
     other way round).
   - B. Keep SQLModel in `internal/extract/models` and put the core in a
     subpackage (`.../kit`) (rejected: Prisma would import a SQLModel
     package's child; the tool-specific package would hold the generic
     name).
   - C. One package for all tools (rejected: each tool's contract test
     hooks, row ids and resolver are package state; one package mixes
     them).
2. **How a front end hands over its tables.**
   - A. The front end keeps its own working structs and converts each
     claimed table into the core's `models.Table`/`models.Column` once,
     after its own steps (chosen: SQLModel's resolution, chain and
     mutation-site logic stay untouched; the core sees only final records).
   - B. The front ends work on the core's structs directly (rejected for
     now: SQLModel's structs carry Python-specific state, such as the class
     record, derived nullability and refused columns; moving them into the
     core leaks one tool's state into every tool).
3. **Claims.**
   - A. The front end records, per fact, the row whose branch set it; the
     core emits a claim only for a fact that is known in the final record
     (chosen: a fact held as `null` can never carry a claim, whatever a
     front end recorded).
   - B. Claims derived after the fact by fingerprinting (the migration
     pass's approach; rejected: a class is read in one pass with no
     statement order, so the branch knows its row directly).
4. **Verification granularity.**
   - A. Per claim (column facts) and one value per table for the table's
     own facts (name, schema, keys, constraint lists) (chosen: table facts
     carry no claims yet, ADR 0055's known gap; a front end whose table
     facts mix both values states `mapped`, the conservative side).
   - B. Per element of every list (each index, each foreign key)
     (rejected for now: no front end needs it before Prisma's contract
     review; Prisma may add it with its own ADR).
5. **SQLModel's root.**
   - A. The repository root, `.` (chosen: every SQLModel table registers
     in the one `SQLModel.metadata`, assumption V4; L4 resolves foreign
     keys across the repository; an unresolved production X1 site affects
     every production table of the repository, L8. The group is the
     linker's scope, so foreign keys, overlaps and the strip's diagnostics
     stay exactly as before).
   - B. The Python package root (the topmost directory with
     `__init__.py`) or the nearest `pyproject.toml` (rejected: it would
     split one `MetaData` into several groups, so a foreign key between
     two packages would cross groups, and the `.py` files' unread and
     not-claimed diagnostics would need a per-root assignment the server
     cannot make for a file outside every root).

## Decision

Options 1A, 2A, 3A, 4A and 5A.

- **Core** (`internal/extract/models`): `Group` (tool, root, pins,
  assumptions, optional dialect, default verification; `SetKey()` =
  `models/<tool>/<root>`), `Table`, `Column`, `ForeignKey`, `Reason`,
  `Claim`, `ClaimSource`, `ClaimFacts`; `Link` (foreign-key resolution by
  name and schema within the group, migration comparison, emit), `Steps`
  (diagnostics and step isolation, `schema_model_internal_error`), the
  per-tool assumption catalogue registry (`Register`, `Catalogues`), and
  `internal/extract/models/modeltest` (contract-test helpers: docs table
  reader, tag recorder, `CheckTags`, `CheckDocs`). Each front end names
  its own rows for the shared steps (`Rows{Emit, ForeignKeys, Overlap}`).
- **SQLModel front end** (`internal/extract/models/sqlmodel`): T018c2's
  code unchanged in what it claims, plus a row per written fact.
- **Records** (extractor change, aggregate extractor version
  extractors-31): model tables store `set: "models/<tool>/<root>"`
  (SQLModel `models/sqlmodel/.`) and `verification`; model columns store
  `claims: [{fact, row, file, span, verification}]`; `dialect` only when
  the source states one (never for SQLModel). Contract v1 attributes are
  open maps, so `contract_version` stays 1.
- **SQLModel verification**: `oracle` for every claim and table. Its facts
  are SQLAlchemy `MetaData` facts, independent of the dialect;
  `TestModelOracle` compares every claim fact kind, and
  `TestModelClaims` checks that every claimed fact is one of those kinds.
- **API and UI**: the server already groups by stored `set` (ADR 0055); it
  serves model claims on `/table` with a link to the claim's file; the
  `/assumptions` catalogue lists every registered tool under source
  `models`. The view shows an "unverified" chip on a model table and on a
  column with a `mapped` fact, with the popover line "From <tool>'s
  <dialect> mapping; not checked against a <dialect> database".

### Revision 1 (2026-11-09)

- **`/sets` payload**: serving every model column's `claims` in `/sets`
  made langflow's `/sets` 54 % slower (response 484 KB -> 1,256 KB). The
  developer chose option (b): `/sets` serves model columns without
  `claims` and each model table a `verification_counts` object (column
  claims counted by verification) next to the table's own
  `verification`, enough for the "unverified" mark on list rows; the
  claims stay on `/table` (with links), where the popovers read them.
  Rejected: (a) keep serving them (over the latency gate); (c) store spans
  more compactly (smaller, but still every claim in a list response).
- **Oracle rule**: a fact is labelled `oracle` only for fact kinds and
  forms the oracle has compared at least once on a fixture or corpus.
  `TestModelClaims` checks that every form a claim branch that sets
  `verification: "oracle"` can write (fact/row pair; for `type` the
  class; for the size facts the value form) is reached by an
  oracle-compared fixture case. A form the oracle disagrees with becomes
  unknown by a class rule, never a guess.

### Revision 2 (2026-11-09, after the independent review)

- **F5 forms by source type**: `TestModelClaims` keyed F5's `type` forms
  by the produced class, so a source type without an oracle case (an
  `EmailStr`, `Path` or `ipaddress` annotation: `AutoString`; an enum on
  `StrEnum`, `IntEnum`, `Flag`, `IntFlag`: `Enum`) counted as covered. F5
  forms are now keyed by the source type; `MappedTypes` holds one case
  each, and the pinned oracle agrees (0 disagreements).
- **Verification is required**: the core refuses a column claim whose
  verification (its own, else the group's) is not `oracle` or `mapped`:
  no claim, a `schema_model_internal_error` diagnostic. A table without a
  known verification is written `mapped` with the same diagnostic (its
  facts stay; dropping them would lose proven facts). Chosen over
  defaulting claims to `mapped` because a refusal makes the front-end bug
  fail that front end's claim check (`modeltest.CheckClaims`) instead of
  shipping. The view marks anything that is not `oracle` unverified, and
  `/sets` serves null counts for a table record without `verification`.
- **Shared claim check**: `modeltest.CheckClaims` (every known fact of a
  read column has exactly one claim, with a row, a file, a line and a
  verification) runs in the core's and every front end's tests.
- **Step isolation**: the per-table `refused` (X1 drop) and `record`
  (conversion to the core record) steps of SQLModel are isolated steps.
- **Nullability rows**: a nullability the annotation states is claimed by
  A1 or A2 (the rows that state it), not F2.

## Consequences

- Prisma (T018g1) and later front ends fill core records and get L4, L5,
  emit, isolation, catalogue and contract-test helpers without copying.
- Every index re-parses once (version bump). Stored model columns grow by
  their claims list (measured in the T018g0 task record (internal)).
- A repository has at most one SQLModel group; the strip shows it as
  `models/sqlmodel/.`.
- An index built before extractors-31 keeps `set: "models"` and no model
  claims: the server serves it unchanged (claims `null`, "not recorded").

## Known gaps

- Table-level facts carry one `verification` and no claims of their own
  (ADR 0055's gap stays).
- (Closed in Revision 1, 2026-11-09: T1's bare `Numeric` and every other
  claimed type form now have a fixture case the oracle compared, and a
  test keeps it so.)
- A claim's span is the line of the member (or of the source the front
  end names), not the span of the fact's own text: a fact written on a
  later line of a multi-line member points at the member's first line.
  Per-fact lines need an end line (or a per-argument position) in the
  front end's payload (Revision 2).
- The server still assigns every `.py` file's `schema_model_*`
  diagnostic to the sqlmodel group; a second Python ORM front end would
  need a per-tool rule.
- A repository with two separate SQLModel applications (two metadata
  objects at run time) is still one group; V4 states the assumption.

## Review notes (delegated review, 2026-11-09)

- **Results:** SQLModel output unchanged by the split (fastapi 3,350 and langflow 234,833 emitted records identical to main after stripping `set`, `claims`, `verification` and `verification_counts`; only the header version 30 -> 31 differs); fixture oracle 0 disagreements on both fixtures (sqlmodel-models 55 tables / 183 columns, 1167/1272 facts known; sqlmodel-link 21 tables / 47 columns) with SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic 2.13.4, email-validator 2.3.0; store size langflow +0.12 %, fastapi +0.37 %; Langflow index timing fresh -1.2 %, warm -0.3 %; langflow `/table` +6-8 %, `/sets` +28-33 % (+15-17 ms, response 487 KB vs 484 KB), accepted by the developer on 2026-11-09 with the store-side projection scheduled as Prisma's first unit (G1B0); full check and web check green; CI run 42684350635 (Windows and postgres green, 0 FAIL, only the expected skips).
- **Revisions:** Revision 1 (`/sets` serves `verification_counts` instead of per-fact claims; every claimable T1/F5 type form reached by an oracle-compared fixture case); Revision 2 (after the independent review: F5 forms keyed by source type with 10 new fixture members; claims without a verification refused; shared `modeltest.CheckClaims`; guarded `refused` and `record` steps; annotation nullability claimed by A1/A2; null counts before extractors-31).
- **Independent review:** no class A. Class B: `oracle` labels on F5 source types the oracle had not compared (fixed). Class C: C1-C3, C6, C7 fixed; C4 (composite foreign keys, `on_update`, constraint names, ordered primary key) moved to Prisma's G1B1 under ADR 0057; C5 (claim spans are a member's first line) is a known gap.
- **Developer decisions (2026-11-09):** per-fact `verification` (`oracle` / `mapped`) for every ORM; one models group per ORM and schema root (`models/<tool>/<root>`; SQLModel's root is the repository, one metadata); the `/sets` latency acceptance above.
- **Known gaps / follow-ups:** table-level claims and per-element verification; claim spans per fact need a payload end line; a second Python ORM needs its own diagnostic assignment; `Numeric(10, 2)` precision and scale stay unknown (T1 argument rule).
