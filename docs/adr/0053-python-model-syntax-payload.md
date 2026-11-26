# ADR 0053 - Python model syntax payload

**Status:** Accepted (delegated review, 2026-11-09)

## Context

Milestone M2 of the project plan names SQLModel/SQLAlchemy models as a schema source, and the
T018 schema plan (the T018 task record (internal)) splits that work into
T018c1 (capture the syntax, cache only) and T018c2 (emit model-defined
`table`/`column` nodes). T018a needed nine reviews because its
claimed surface was open, so T018c1 closed the surface first (rule 33): a
finite claim contract (61 rows, `docs/lang/python-models.md`), an inventory
of every model form in the ten corpora (the investigation, the T018c1 task record (internal))
and an oracle (SQLAlchemy's own `MetaData` after import) against which the
contract's reference model had 0 disagreements. Revision 2 (2026-11-09)
replaced the prediction of import failures with the assumption that model
modules import, as they do when the application runs (V7), and keeps
failures the source proves as diagnostics (P1).

Model linking needs more than the existing cached Python facts hold: class
keywords (`table=True`), the class body (annotations, `Field(...)` and
`Column(...)` arguments, `__tablename__`, `__table_args__`), Core `Table(...)`
calls and the sites that mutate a table after its definition. The cache must
hold syntax only (rule 31: no cross-file claim in a per-file cache), the
smallest payload that keeps every real target (rule 32), and a versioned
shape (rule 28).

## Options considered

1. **Reuse `PythonBinding` values only.** Class bindings already keep bases
   and module bindings keep a trimmed value. They drop class keywords, the
   class body and every call argument model linking reads; widening them
   would change every Python file's cache and every consumer of bindings.
2. **A full class-body payload for every class.** Simple to describe, but
   it caches bodies of thousands of classes that are not models (langflow
   has far more classes than the 63 table and chain classes) and every
   expression's text, against rule 32.
3. **A gated, reduced payload (chosen).** A new additive field captured
   only in files that import `sqlmodel` or `sqlalchemy`, with bodies only for
   classes that can be models, and reduced expressions.

## Decision

- `facts.PythonFacts.Models` (JSON `Models`, omitted when absent), type
  `facts.PythonModels`, version 1 (`facts.PythonModelsVersion`). Existing
  Python facts are unchanged; resolution data (bindings, imports, scopes)
  stays where it is and is not duplicated.
- Gates, all syntactic and evaluated during the existing syntax walk (no
  second parse):
  - G1: the file has an absolute import whose module's first segment is
    `sqlmodel` or `sqlalchemy`; other files get no `Models` field.
  - G2: a header record for every class with a base, keyword or decorator.
  - G3: members only for a class with a `table` keyword, a base whose last
    segment is `SQLModel`, a base naming such a same-file class (T018c1
    Revision 3), or that such a same-file class names as a base
    (fixpoint).
  - G4: lines of calls whose callee's last segment is `Table` or
    `map_imperatively`.
  - G5: mutation sites (`append_*`, set adds on `indexes`/`constraints`/
    `foreign_keys`, attribute assignments on `__table__`/`.c.`/`.columns`
    chains, constraint constructors outside `__table_args__` and outside call
    arguments with a non-literal positional argument) and DDL event hooks.
- Reduced expressions: literals decoded, calls keep the dotted callee,
  keyword names and positional arguments to depth 4; `Field`, `Column` and
  `Index` keep keyword values only for the keywords the contract reads;
  `Relationship` keeps its callee only; annotations keep their compacted
  text, values keep text only in type positions.
- Validity: `PythonModels.Valid()` and `PythonFacts.ModelsValid()` (scope
  indexes in range) are checked on every cache read; anything else is a
  cache miss. A failure to capture one class drops that class's record with
  a diagnostic and keeps the file's other facts.
- Versioning: the aggregate extractor version moves to `extractors-29`
  (living documents only).
- The claim contract lives in `docs/lang/python-models.md` ("Claim
  contract"), copied row for row from the task file; every capture branch
  carries its row id, and `TestModelPayloadContract` keeps code and table
  1:1. `TestModelCensus` checks the capture against the investigation's census when
  `GRAPHVEIN_MODEL_CENSUS_CORPUS` names the corpus directory.

## Consequences

- Every Python file is re-extracted once (cache version bump). Files outside
  G1 cache the same facts as before; the corpus snapshots change only in
  their header. The census finds G1 in 21 fastapi and 391 langflow files and
  in none of the other eight corpora.
- The Langflow store grows by the payload of 391 files with 77
  member-captured classes; the budget is +1.5% (task Gates), measured
  outside this ADR's change.
- The payload adds no graph record and no contract change; T018c2 decides
  every claim at link time through the resolver.

## Known gaps

- Cross-file ancestors in files outside G1 (a mixin that reaches SQLModel
  only through a re-exporting module) are not captured: T018c2 sees an
  unresolved record and marks the table partial ("unknown ancestor"), never
  a wrong fact. Relative imports never pass G1.
- U1-U4 (declarative classes, `Mapped`/`mapped_column`, Core tables,
  imperative mapping) are recognised, not read: a marker or a line, so a
  view can say "models present, not read".
- Column order and database names derived by naming conventions are never
  claimed (PF2, PF3).
- Mutations in files outside G1 are not captured (assumption V6).
- G4 is a spelling test: one langflow line (`rich.table.Table`) is captured
  and dropped by resolution at link time.
- The contract states SQLModel 0.0.39, SQLAlchemy 2.0.51 and Pydantic
  2.13.4 behaviour; another locked version is an assumption T018c2 states as
  a diagnostic.
- Import failures are not predicted in v1 (assumption V7, rows I1 and I2
  after v1); only failures the source proves (P1) are diagnostics.

## Review notes (delegated review, 2026-11-09)

Accepted together with ADR 0054 after T018c1 Revisions 1-3 and T018c2
Revisions 3-3.3 (records: the T018c1 task record (internal),
the T018c2 task record (internal)).

- Results:
  - Fixture oracle (V1 pins, scratch venv, Python 3.14.6): `sqlmodel-models`
    52 oracle tables, 142 columns, 44 claimed, **0 disagreements** (the four
    V7 fixture classes below excepted by decision); `sqlmodel-link` 21
    tables, 47 columns, 19 claimed, **0 disagreements**.
  - Corpus oracle (T018c2 linker over this payload, final revision):
    fastapi 2/2 tables complete, 12/12 columns; langflow 45/45 tables
    named, 36 complete column sets, 399/412 columns read; **0
    disagreements**.
  - Census (`TestModelCensus`, parse only): fastapi 21 G1 files and 19
    member-captured classes; langflow 391 G1 files, 93 classes, 574
    members, 118 Core-table lines (one of them the `rich.table.Table`
    spelling match), 11 mutation sites; the other eight corpora 0. The 16
    and 4 classes over the investigation's census are G3's same-file subclasses
    (API schemas, none a table).
  - Snapshots: the eight corpora without models byte-identical except the
    `extractors-29` header; fastapi +29 and langflow +992 records, all
    T018c2 model records and diagnostics, 0 removed.
  - Self-index: +511 records, all in the two model fixtures (gate run on
    `7bfc3d5`).
  - Store size (Langflow, fresh SQLite, same repository ID): payload alone
    +0.04% (budget +1.5%), with T018c2's records +0.20%.
  - Timing (interleaved): Langflow fresh +4.5%, warm +2.6%; fastapi
    +0.02 s fresh (budget +10%, +1 s below 10 s).
  - CI: run 42625029527 on `7bbf974` green (Windows; postgres job 0 FAIL,
    82 distinct `/postgres` PASS, only expected skips). Run 42623110414
    failed in `TestIndexLockAcquireAndState/postgres` (a shared-database
    test-isolation fault, fixed in `7bbf974`; not this ADR's code).
- Revisions (T018c1):
  - Rev 1 (2026-11-07): the contract review's class-A finding (Annotated
    metadata) and six class-B findings fixed as class rules R1-R9, every
    reviewer case added to the synthetic set; reference model 0
    disagreements.
  - Rev 2 (2026-11-09): assumption V7 (model modules import, as when the
    application runs) replaces the I1/I2 import-failure prediction; rule
    P1 keeps source-proven failures as diagnostics.
  - Rev 3 (from T018c2's review): G3 downward fixpoint for same-file
    subclasses, M12 literal values kept, `examples`/`json_schema_extra`
    out of K-meta/K-len, I1's missing-key form conditioned on a proven
    constraint set; Rev 3.1 added reduced `Args`/`Keywords` on X1 append
    and set-add sites. No version bump (payload v1 not on main before).
- Independent review (T018c2's, covering this payload): no class-A
  finding; its B/C findings that touch the payload (G3, M12) are fixed in
  Rev 3. The earlier contract re-check (2026-11-07) also found no class A.
- Assumptions: V1 (library pins), V4 (one default metadata), V6
  (import-free mutations outside G1 are not captured), V7 (import
  succeeds). I1 and I2 stay in the contract marked after v1.
- Developer decisions (2026-11-09): V7 replaces import-failure
  prediction; the four synthetic V7 fixture classes
  (`fail_a5_later.py#Early`, `rv_c2_dup_name.py#Second`,
  `rv_c2_shared_sa_column.py#SecondShared`,
  `rv_c2_shared_table_args.py#SecondArgs`) keep claims the oracle
  contradicts, by decision (no corpus form). Linking decisions are in ADR
  0054's notes.
- Known gaps: as listed above (cross-file ancestors outside G1, U1-U4
  recognised not read, G4 by spelling, mutations outside G1, pins not
  detected, column order and naming-convention names never claimed).
- After v1: I1/I2 import-failure prediction and V5; resolving Core-table
  callees (needs the callee in the payload, an ADR change).
