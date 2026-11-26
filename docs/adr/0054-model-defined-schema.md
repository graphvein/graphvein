# ADR 0054 - Model-defined schema

**Status:** Accepted (delegated review, 2026-11-09)

## Context

Milestone M2 of the project plan accepts "fastapi's SQLModel schema after T018c2" (developer
decision D9). T018c1 (ADR 0053) caches the SQLModel/SQLAlchemy model syntax
of files that import `sqlmodel` or `sqlalchemy` and fixes the claim
contract (61 rows, `docs/lang/python-models.md`); Revision 2 replaced the
prediction of import failures with assumption V7 (the model modules import,
as they do when the application runs) and rule P1 (failures the pinned
source proves from one class stay diagnostics). T018c2
(`docs/contract/claims/python-models-linking.md`) turns the cached syntax into graph
facts. The migration pass (ADR 0047, `internal/extract/schema`) is being
changed in parallel by T018a2, and the schema read API (ADR 0048) and the
Schema view (T018b2) derive every set state from migration nodes.

## Options considered

1. **Feed model classes into the migration state** of
   `internal/extract/schema` as another front end (the plan's shared-state
   idea). Rejected for now: the state is built around ordered SQL
   statements and Postgres refusal rules that do not apply to a class
   chain, and T018a2 rewrites that package in parallel.
2. **Merge model tables into the migration sets of the same repository**
   by table name. Rejected: which database a model maps to is runtime
   configuration (the engine URL), so a merge would assert a fact source
   does not show; the plan asks to compare, never merge silently.
3. **A separate linker package that emits the same node kinds**
   (`table`, `column`, `contains`, `foreign_key`) with `source: "model"`,
   its own group key `set: "models"`, and a comparison diagnostic for
   equal names (chosen).

## Decision

- `internal/extract/models` runs at link time after the migration pass,
  reads `facts.PythonModels` and resolves every name through the shared
  Python resolver (ADR 0015, ADR 0017). It adds no cached payload, so the
  aggregate extractor version stays extractors-29.
- The claim contract is T018c1's table plus linking rows L1-L8 (resolution,
  cross-file chain and base proof, node shapes, foreign-key resolution,
  migration comparison, unread markers, proven failures, mutation-site
  receivers), kept 1:1 with the code by `TestModelClaimContract`
  (`docs/extract/schema-models.md`).
- Identity: `table:<file>#<Class>` and `column:<table file>#<Class>.<column>`
  (the class's lexical name, so renaming `__tablename__` keeps rationale
  attached); the table name is the node name. An inherited column carries
  `defined_in` with the member's file and line.
- Model tables carry `assumptions: ["V1", "V4", "V6", "V7"]` (T018c2
  Revision 3.3: V4 same metadata and V6 import-free mutations outside G1
  added) and the library pins, so a view can state what the facts rest on.
- Read API (developer decision PD1, 2026-11-09; T018f serves model groups
  under ADR 0055): the `/api/v0/schema/*`
  endpoints leave `source: "model"` records out, so the Schema view keeps
  showing migration sets only and never labels a model table as part of
  one, until T018f adds them. Proposed extension for after the mockup decision: model tables as
  their own group with `source: "models"` (the field ADR 0048 already
  reserves), `objects: "modeled"`, `table_list` from the per-file
  `schema_model_unread`/`schema_model_core_tables` diagnostics
  (`may_be_incomplete` when any exists), and a model-specific
  `completeness` from `state`; the summary's `found` would count model
  groups. No public interface changes in this ADR's implementation.
- Comparison with migration sets (developer decision PD2, 2026-11-09): an
  info diagnostic `schema_model_migration_overlap` for equal table names,
  with the column-name difference when both tables are complete; no edge,
  no field-by-field verdict.
- Oracle: `scripts/oracle/sqlmodel_metadata.py` dumps SQLAlchemy's
  `MetaData` in a scratch venv with the pins; `TestModelOracle` compares
  (zero disagreements on claimed facts).

## Consequences

- fastapi's two model tables become graph facts; the CLI and node
  endpoints show them, the Schema view does not until T018f (PD1).
- Fixtures with model classes gain table and column records
  (`sqlmodel-models.golden.json`); corpus snapshots of fastapi and langflow
  will change when the later gate re-indexes them.
- The ARCHITECTURE plan of one shared schema state for every front end is
  not followed for models; a later task may unify them if Alembic (T018d)
  needs both.

## Known gaps

- V7 forms the fixture holds on purpose: a module whose later class fails
  (an alias bound after its use, duplicate table names, a shared
  `sa_column` or `__table_args__` across two tables) still yields claims
  for classes the oracle lacks or builds differently; none occurs in the
  corpora (T018c1 Revision 1, R9).
- U1-U4 (declarative classes, `Mapped`, Core tables, imperative mapping)
  are counted, not read; Core-table candidates are counted by spelling.
- The `sa_column=<helper>()` rule (langflow `_tz_column()`, 12 columns) is
  not implemented: those columns are D.
- Multiple inheritance (C6) gives no table name unless the class writes its
  own `__tablename__`.
- Column order and generated constraint/index names are never claimed;
  `length`/`precision`/`scale` use `"none"` for a proven absence.
- Versions other than the pins are not detected; the pins are an
  assumption stated on every table.

## Review notes (delegated review, 2026-11-09)

Accepted after T018c2 Revisions 3-3.3 and the merge of main (records:
the T018c2 task record (internal), `docs/contract/claims/python-models-linking.md`).

- Results:
  - Fixture oracle (V1 pins, Python 3.14.6): `sqlmodel-models` 52 oracle
    tables, 142 columns; 44 claimed, 37 complete column sets, 106 columns
    read, 839/944 facts known, **0 disagreements** (four V7 classes by
    decision, below); `sqlmodel-link` 21 tables, 47 columns; 19 claimed,
    40 columns read, 300/344 facts known, **0 disagreements**.
  - Corpus oracle (Python 3.14.6, corpus locks as constraints): fastapi
    2 tables, 12 columns: 2/2 complete, 12/12 columns, 96/96 facts, **0
    disagreements**. langflow 45 tables, 412 columns: 45/45 claimed, 36
    complete column sets, 35 complete tables, 399/412 columns read,
    3144/3296 facts, **0 disagreements**. Targets met; the nine partial
    column sets are those the reference model also leaves partial (eight
    `_tz_column()` tables, `SSOConfig` A1m).
  - Census: as ADR 0053's notes (fastapi 19, langflow 93 member-captured
    classes).
  - Snapshots (merged main vs branch): fastapi +29 records (2 `table`,
    12 `column`, 14 `contains`, 1 `foreign_key`); langflow +992 (45
    `table`, 412 `column`, 457 `contains`, 49 `foreign_key`, 29
    `schema_model_core_tables`); 0 removed; the other eight corpora
    differ only in the header.
  - Self-index: +511 records, all in `testdata/fixtures/sqlmodel-models`
    and `sqlmodel-link` (gate run on `7bfc3d5`, before Revision 3.3's
    fixture files).
  - Store size (Langflow, fresh): +0.04% payload (ADR 0053), +0.20% total
    with model records; fastapi +0.37%. Budget +1.5%: pass.
  - Timing (interleaved): Langflow fresh +4.5%, warm +2.6%; fastapi
    +0.02 s fresh, +0.01 s warm. The Revision 3.3 rerun left the index
    phase within the earlier range, so timing was not rerun.
  - CI: run 42625029527 on `7bbf974` green (Windows; postgres job 0 FAIL,
    82 distinct `/postgres` PASS; only expected skips: four helpers,
    env-gated `TestGenerate*`, `TestOracleRealHistories`,
    `TestModelCensus`, `TestModelOracle`, two `TestB3CasesPostgres`
    subcases needing `search_path` public). Run 42623110414 failed in
    `TestIndexLockAcquireAndState/postgres`; `7bbf974` makes that test
    count only its own schema's lock key (other packages hold index locks
    in the same database).
- Revisions:
  - Rev 3 (after the independent review): primary-key failure only when
    the constraint set is proven key-free (else `primary_key` unknown);
    M12 literal P1 form; G3 same-file subclasses; K-meta text; per-step
    isolation; U1 counted transitively.
  - Rev 3.1: any fact an X1 site may change is unknown on every affected
    table (constraint kind decides primary/foreign keys; `append_column`
    with `replace_existing`; table attribute assignments).
  - Rev 3.2: X1 attribute assignments not proven to keep identity refuse
    the column or table claim (`schema_model_not_claimed`).
  - Rev 3.3: G-1 (`classmethod`/`staticmethod`/`property` resolve as
    unbound builtins), G-2 (K1b module-level `.with_variant` alias read,
    type unknown), O1 (`assumptions` = V1, V4, V6, V7); merge of main
    (T018a2) with docs-only conflicts.
- Independent review: no class-A finding. Two class-B findings (primary
  key declared in `__table_args__`; M12 literal claimed because the
  payload kept no value) and four class-C findings (same-file subclasses,
  K-meta text, step isolation, U1 count) are fixed as class rules in
  Revision 3 with fixtures; Revisions 3.1 and 3.2 close its X1
  follow-ups.
- Assumptions on every model table: V1 (pins), V4 (same metadata), V6
  (import-free mutations outside G1), V7 (import succeeds). I1 and I2
  are after v1.
- Developer decisions (2026-11-09):
  - V7: model modules are assumed to import; only source-proven failures
    (P1) are diagnostics.
  - PD1: model records stay out of `/api/v0/schema/*` until T018f
    (models group, assumptions endpoint); readable through the graph.
  - PD2: overlap with migration tables is only the
    `schema_model_migration_overlap` diagnostic; no merge, no edge, no
    field verdict.
  - A literal options dict in `__table_args__` keeps the strict rule
    (primary key unknown) until a corpus shows one.
  - X1 assignment targets with a subscript (`T.__table__.c["x"]...`)
    refuse every production table (unresolved receiver).
  - The four synthetic V7 fixture classes (`fail_a5_later.py#Early`,
    `rv_c2_dup_name.py#Second`, `rv_c2_shared_sa_column.py#SecondShared`,
    `rv_c2_shared_table_args.py#SecondArgs`) keep their claims as
    documented V7 cases (`oracleV7`); no corpus has the forms.
  - G-2 narrowing: only T1 calls chained through `.with_variant(...)` are
    read; any other module-level bound name stays K6.
- Known gaps: as listed above, plus constraint variables are not followed
  (`append_constraint(pk)` withdraws both keys); `PrimaryKeyConstraint`
  literal columns are not claimed; `declarative_base()` bindings and
  `@registry.mapped` are not counted under U1.
- After v1: I1/I2 import-failure prediction and V5; the `_tz_column()`
  helper rule; C6 plain-mixin reading; options-dict allowlist; spelling
  subscripts in X1 receivers; Core-table callee resolution.
