# Schema from SQLModel models

ADR 0054 (T018c2, `docs/contract/claims/python-models-linking.md`) and ADR 0058
(T018g0, the T018g0 task record (internal)). The SQLModel front end
`internal/extract/models/sqlmodel` runs at link time, after the migration
pass (`docs/extract/schema.md`), over the model syntax the Python extractor
caches (ADR 0053, `docs/lang/python-models.md`), and hands its tables to the
tool-neutral model kit `internal/extract/models`, which resolves foreign
keys (L4), compares with migration sets (L5) and emits the records (L3) for
every ORM front end. It adds no cached payload; it ran from extractors-29 on
(aggregate extractors-30 since T018f, extractors-31 since T018g0,
extractors-32 since T018g2's TypeScript model payload, extractors-33 since
T018h's Python and sqlx query payloads, extractors-34 since T018h's binding
change, extractors-35 since T018g3's model payload version 2, extractors-36
since T018g5's Go model payload, extractors-37 since T018g6's model payload
version 5, extractors-38 since T021's best-effort payload, extractors-39
since T020 S1's Go structure payload, extractors-40 since T020 S2's Python
structure payload, extractors-41 since T020 S3's TypeScript structure
payload, extractors-42 since T020 S4's Rust structure payload, extractors-43
since T020 S2X's Python workspace members, extractors-44 since GORM's
package selection, extractors-45 since T020-TSG's patched grammars,
extractors-46 since T023's Go router payload, extractors-47 since T022
MR-B3b's stored Express equivalence and per-alternative client bases and
MR-B6c's build-time constant bases,
extractors-48 since T026's architecture payloads, ADR 0079,
extractors-49 since T024's Next.js and server action payloads, ADR 0077).

## What it claims

The claim contract is T018c1's table (`docs/lang/python-models.md`, "Claim
contract", read in its claim and refusal columns with Revision 2: I1 and I2
after v1, assumption V7, rule P1) plus T018c2's linking rows L1-L8
(`docs/contract/claims/python-models-linking.md`). Every fact comes from one row;
everything else is unknown (`null`) with a reason, and a library default is
claimed only where the class and its proven chain show the setting absent.

- **Resolution (L1, L2).** Bases, callees, annotation names and type
  expressions count as `sqlmodel.X`, `sqlalchemy.X`, `pydantic.X`, ... only
  through the Python resolver (ADR 0015, ADR 0017), including SQLModel
  0.0.39's re-exports of SQLAlchemy names. A builtin (`str`, ..., and
  **(Rev 3.3)** the K-dec decorators `classmethod`, `staticmethod` and
  `property`) counts only when no binding in the module or an enclosing
  scope shadows the name, an alias of the builtin included. A table is claimed only when
  every direct base is proven non-table (R-B2); a base outside the
  repository or without a captured record gives `schema_model_not_proven`.
- **Proven failures (L7, P1).** `sa_column` with a flag keyword, a `Field`
  keyword outside its 0.0.39 signature, `ondelete="SET NULL"` on a column
  proven not nullable, an annotation type with no SQLAlchemy mapping, a
  complete column set without a primary key whose constraint set is proven
  to hold none (no `__table_args__`, or only `UniqueConstraint`, `Index`
  and `CheckConstraint` calls), and a non-annotated literal class attribute
  (`VERSION = 3`, pydantic's check; not when a chain class annotates the
  name, for example as a `ClassVar`) give `schema_model_import_failure`
  and no table for that class. Every other import failure is assumption V7
  (the model modules import, as they do when the application runs).
- **Column type aliases (K1b, Rev 3.3).** `Column(JsonVariant, ...)` with
  `JsonVariant = JSON().with_variant(JSONB(), "postgresql")` bound once at
  module level (before the class in the same file) is read: the type is
  unknown with a K1b reason, the other `Column` parts follow their rows. A
  name bound to any other expression stays K6 (column D), since it may be a
  schema item (`ForeignKey`, `Sequence`, `Identity`, `Computed`) whose facts
  the column would otherwise miss.
- **Primary key in the constraint set.** When `__table_args__` may hold a
  primary key (a `PrimaryKeyConstraint`, an options dict or any other
  element, an unread `__table_args__`, several bases), the table's
  `primary_key`, every column's absent `primary_key: false` and a
  `Column`'s nullability derived from it are unknown.
- **Isolation.** Each class, site and table step runs on its own: a failure
  becomes `schema_model_internal_error` naming the step, and the other
  classes and tables are still linked.
- **Foreign keys (L4).** The target table must be exactly one claimed
  model table of the repository with that name (default schema, scope
  compatible) and the column one of its known columns: a `foreign_key`
  edge; otherwise `unresolved` or `ambiguous` and
  `schema_unresolved_reference`.
- **Migration sets (L5).** Model tables are never merged into migration
  sets. A model table with the name of a migration-set table gets
  `schema_model_migration_overlap`, with the column-name difference when
  both are complete.
- **Mutation sites (L8).** X1 sites and U5 hooks resolve through the site's
  scope bindings (`table = C.__table__`) or `C.__table__...` chains; an
  unresolved production site affects every production model table; a
  test-scope site affects only test-scope tables. **(Rev 3.1)** A fact a
  site may change is unknown on every table it affects: an added
  constraint that is not a resolved `UniqueConstraint`/`Index`/
  `CheckConstraint` call opens the primary key (table `primary_key`,
  columns' `primary_key: false`, K2's derived nullability) unless it is a
  `ForeignKeyConstraint`, and the columns' `foreign_keys` unless it is a
  `PrimaryKeyConstraint`; `append_foreign_key` withdraws its column's
  `foreign_keys`; `append_column` that may replace a column
  (`replace_existing` other than literal `False`) makes every column
  unknown; a table attribute assignment also makes the schema and the
  listed constraint elements unknown. **(Rev 3.2)** An attribute
  assignment keeps a claim only when its target is proven to leave the
  claim's identity unchanged (single table segments `comment`, `info`,
  `primary_key`, `indexes`, `constraints`, `foreign_keys`; column segments
  such as `nullable` or `type`, listed in row L8): one that may rename a
  column (`c.<col>.name`, `.key`, another segment) refuses that column
  claim; any other target (`name`, `schema`, `fullname`, a subscript)
  refuses the table claim (`schema_model_not_claimed` naming X1).
- **Never claimed:** column order (PF2), generated names (PF3),
  relationships (PF7), a schema other than the default.

## Records

- `table` (level 4, the class statement; identity
  `table:<file>#<Class>`, name = the table name): `source: "model"`,
  `tool: "sqlmodel"`, `set: "models/sqlmodel/."` (the group key
  `models/<tool>/<root>`, G3; **(T018g0)** SQLModel's root is `.`, the
  repository root: every SQLModel table registers in the one
  `SQLModel.metadata` (V4), foreign keys resolve across the repository (L4)
  and an unresolved production mutation site affects every production
  table of the repository (L8), so the group is the linker's scope; until
  extractors-31 the key was `models`), `verification` (**(T018g0)**
  `oracle`, below), `verification_counts` (**(ADR 0057)** the claims of
  the columns it contains counted by verification, `{"oracle": n}`; `{}`
  without claims; written at link time, so no version bump), `name`, `schema` (`null`) with
  `schema_known`, `class`, `state` (`complete` only when `column_set` and
  `constraint_set` are both `complete`), `reasons` (`[{row, fact, file, line,
  reason}]`; T018f `fact`: `column_set` or `constraint_set` for a table
  reason, `type`, `primary_key` or `foreign_keys` for a column reason, null
  when a column reason makes every attribute unknown; `file` is the file of
  `line`, which may differ from the table's), `primary_key` (`null` unless the column set is complete,
  every key flag known and the constraint set proven to hold no primary key), `unique_constraints`, `indexes`,
  `check_constraints` (the `__table_args__` elements read; complete only
  when `constraint_set` is), `ddl_hooks`, `scope`, `assumptions`
  (`["V1", "V4", "V6", "V7"]`: library pins, same metadata, import-free
  mutations outside G1, imports succeed; **(Rev 3.3)** V4 and V6 added) and `pins` (SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic
  2.13.4).
- `column` (level 4, identity `column:<table file>#<Class>.<column>`):
  `source`, `name`, `attribute`, `defined_in` (`{file, line}` of the
  member, which may be in another file for an inherited field), `state`
  (`read`/`unknown`), `reasons`, `annotation`, `type`, `length`,
  `timezone`, `precision`, `scale` (claimed only for the types that have
  them; `"none"` is a proven absence, `null` not claimed), `nullable`,
  `primary_key`, `index`, `unique`, `default`
  (`absent`/`scalar`/`callable`/`null`), `default_value`,
  `server_default`, `server_default_text`, `foreign_keys` (`[]` proven
  none, `null` unknown, else `[{table, schema, column, on_delete,
  status}]`; **(ADR 0057)** a front end that states a whole constraint,
  Prisma, adds `local_columns`, `columns`, `on_update` and `name`, which
  SQLModel never writes), `scope`, **(T018g0)** `claims`. No `position`
  (PF2). **(ADR 0057)** A front end may state the table's primary-key
  order; the kit writes it only when it names exactly the key columns.
  **(ADR 0057)** A front end may give a column an identity name, used in
  the column's identity and qualified name instead of its name (Prisma:
  the field name, while `name` is the `@map`'d column name).
- **(T018g0)** Column `claims`: one entry per known fact among `name`,
  `type`, `length`, `timezone`, `precision`, `scale`, `nullable`,
  `primary_key`, `index`, `unique`, `default`, `server_default`,
  `foreign_keys` (in that order): `{fact, row, file, span, verification}`,
  the row whose claim branch set the fact, the member's file and line (the
  defining class's file for an inherited field), and `verification`. A fact
  held as `null` has no claim (its why is in `reasons`); a column with
  `state: "unknown"` has none. Text kept beside a fact (`default_value`,
  `server_default_text`, `annotation`) is covered by that fact's claim.
  Rows per fact (`TestModelClaims`): `name` M2, M3, M4 (the member) or K1
  (a `Column` name); `type`, `length`, `timezone`, `precision`, `scale` F5
  or T1; `nullable` F2, K2, A1 or A2 (Revision 2: a nullability the
  annotation states is claimed by A1, nullable, or A2, not nullable; F2
  claims one a `nullable=` keyword or the primary key decides);
  `primary_key`, `index`, `unique` F1 or K2;
  `default` M3, M4, F3, F3b or K4; `server_default` M2, M3, M4 (a member
  without `sa_column` has none) or K4; `foreign_keys` M2, M3, M4 (proven
  none), F4, K1 (proven none) or K3.
- **(T018g0)** `verification` (developer decision 2026-11-09): `oracle`
  for a fact the model's oracle checked, `mapped` for one taken from the
  ORM's own per-dialect mapping source and not checked against that
  database. SQLModel's facts are SQLAlchemy `MetaData` facts, independent
  of the dialect (no `dialect` attribute); a claim is `oracle` when its
  fact is one `TestModelOracle` compares with the `MetaData` oracle (the
  column by name, `type`, `length`, `timezone`, `precision`, `scale`,
  `nullable`, `primary_key`, `index`, `unique`, `default`,
  `server_default`, `foreign_keys`; and the table's name, schema, primary
  key and constraint lists for the table's `verification`). Every SQLModel
  claim fact is such a fact, so every SQLModel claim and table is `oracle`;
  SQLModel has no `mapped` fact. A fact is `oracle` only for fact kinds
  and forms the oracle has compared at least once on a fixture or corpus
  (T018g0 Revision 1): `TestModelClaims` checks that every form a claim
  branch can write (each fact/row pair; for `type` each T1 class and each
  F5 source type, Revision 2: `str`, `EmailStr`, `Path` and the four
  `ipaddress` types each, though all map to `AutoString`, and an enum on
  each of `Enum`, `StrEnum`, `IntEnum`, `Flag`, `IntFlag`; for `length`, `timezone`, `precision` and `scale` each value
  form, `none`, an integer, `true`, `false`) is reached by the two
  oracle-compared fixtures (`sqlmodel-models`, `sqlmodel-link`), and that
  they reach no form outside that list. `t_types.py` holds the cases for
  T1's bare `Numeric`, `Numeric(10, 2)` (arguments: precision and scale
  unknown), F5's `Decimal`, every other T1 class and every F5 source type
  (0 disagreements with the pinned oracle, `email-validator` 2.3.0 for
  `EmailStr`). The core refuses a claim without a known verification (no
  claim, a `schema_model_internal_error` diagnostic) and writes a table
  without one `mapped` with that diagnostic, so an unknown verification
  never looks verified; `modeltest.CheckClaims` (every known fact of a
  read column has exactly one claim with a verification, and each table's
  `verification_counts` equals its columns' claims counted) runs in every
  front end's tests.
- **(ADR 0065, T018g0b)** Kit fields for other front ends, never written
  by SQLModel: a group whose dialect is chosen at run time writes table
  `dialects` (sorted, and no `dialect`); a column may state `type`,
  `type_family`, `length`, `timezone`, `precision`, `scale`,
  `autoincrement` and `server_default` (with `server_default_text`) per
  dialect in `by_dialect: {<dialect>: {<fact>: value}}` (every table
  dialect listed, null where unknown; the flat fact null; such a group
  states every one of these facts per dialect, and a flat value is
  written unknown with `schema_model_internal_error`), each with one
  claim per dialect whose value is known, carrying `dialect`;
  `type_family: {name, unknown}` (unknown modifiers among `length`,
  `precision`, `scale`, or the generic `modifiers` alone; `type` and
  those modifiers null) claims a type whose modifiers the source loses.
  The generic `modifiers` (`models.ModifiersUnknown`, ADR 0062 Revision
  2, a small kit change recorded there, not in ADR 0065) is for a type
  whose own modifiers are none of the three, such as an extension type
  (PostGIS `geometry(Point, 4326)`, pgvector `vector(1536)`) that
  Diesel's schema.rs writes without them; it has no attribute of its own
  and the view shows "geometry (modifiers unknown)"; `autoincrement` (true: the database
  generates the value) is a claim fact; `tool_type` is the ORM's own type
  as written, never claimed. All are omitted when unset, and a claim
  without a dialect has no `dialect` key. `modeltest.CheckClaims` checks
  them (`CheckDialectFacts`, `CheckTypeFamilies`).
- Edges (fact, confidence 1): `contains` class -> table and table ->
  column; `foreign_key` column -> column.
- Diagnostics (info unless noted): `schema_model_not_claimed`,
  `schema_model_flag_unknown`, `schema_model_not_proven`,
  `schema_model_name_unknown`, `schema_model_unread` (per file count),
  `schema_model_core_tables`, `schema_model_migration_overlap`,
  `schema_unresolved_reference`; warning `schema_model_import_failure`,
  `schema_model_internal_error` (one class failed; the others continue),
  `schema_duplicate_record`.

The schema read API (`/api/v0/schema/*`) serves model tables as model
groups beside the migration sets, grouped by their stored `set` and never
merged with a migration set (T018f, ADR 0055; `docs/cli/serve.md` "Schema
endpoints"); `/api/v0/schema/assumptions` lists this contract's rows and
assumptions (`catalogue.go`, checked by `TestCatalogue`).

## Claim contract

Row ids of T018c1's table and T018c2's linking rows, with the function
that holds each row's claim branch (`TestModelClaimContract` keeps this
table, the task files and the code 1:1).

| id | claim branch | claim (summary; the task files hold the full rows) |
|---|---|---|
| C1 | `class` | a table for a `table=True` class whose direct bases are proven non-table |
| C1b | `class` | no table |
| C1c | `class` | none; `schema_model_flag_unknown` |
| C2 | `buildTable` | chain members contribute columns, nearest definition wins |
| C3 | `class` | none; `schema_model_not_claimed` |
| C4 | `class` | none (D) |
| C5 | `buildTable` | nothing; outside K-cfg the class is D |
| C6 | `buildTable` | columns and constraints partial; name only from the class's own `__tablename__` |
| N1 | `buildTable` | table name as written |
| N2 | `buildTable` | `Class.lower()` when absence is proven |
| N3 | `buildTable` | no table node; `schema_model_name_unknown` |
| M0 | none | nothing (not captured) |
| M1 | `buildTable` | nothing; outside K-dec the class is D |
| M2 | `fieldColumn` | a column from `Field(...)` |
| M3 | `fieldColumn` | a required column without default |
| M4 | `fieldColumn` | a column with a literal default |
| M5 | `fieldColumn` | no column (removes an inherited field) |
| M6 | `fieldColumn` | nothing |
| M7 | `buildTable` | nothing |
| M8 | `buildTable` | nothing (C5) |
| M9 | `buildTable` | nothing (C5) |
| M10 | `buildTable` | nothing; other keys make the class D |
| M11 | `buildTable` | table constraints and schema |
| M12 | `buildTable` | class D, the bound column unknown; a literal value is P1 (L7) |
| A1 | `annotation` | nullable |
| A2 | `annotation` | not nullable |
| A3 | `annotation` | nullability and type unknown |
| A5 | `annotation` | the alias read in its module |
| A1m | `annotation` | non-plain metadata: column D |
| F1 | `fieldCall` | flags; absent: false |
| F2 | `fieldCall` | nullability |
| F3 | `fieldCall` | default absent or scalar |
| F3b | `fieldCall` | default callable |
| F4 | `fieldCall` | foreign key (last-dot split) |
| F5 | `setType` | type from the annotation |
| F6 | `fieldCall` | type from `sa_type` |
| F7 | `fieldCall` | the column is the `Column(...)` |
| F7r | `fieldCall` | P1 failure |
| F8 | `fieldCall` | column D |
| F9 | `fieldCall` | no fact; outside the signature P1 |
| K1 | `columnCall` | column name and type |
| K1b | `typeExpr` | type unknown; **(Rev 3.3)** form `alias` (a module-level name bound once to a T1 call followed only by `.with_variant(...)` calls): the column read, its type unknown with reason |
| K2 | `columnCall` | flags, nullable = not primary_key |
| K3 | `columnCall` | foreign key |
| K4 | `columnCall` | default, server default |
| K5 | `columnCall` | nothing |
| K6 | `columnCall` | column D |
| T1 | `typeExpr` | type class with length/timezone |
| TA1 | `tableArgs` | unique constraint |
| TA2 | `tableArgs` | index |
| TA3 | `tableArgs` | check constraint |
| TA-D | `tableArgs` | constraint set, schema and primary key unknown |
| U1 | `class` | none; `schema_model_not_claimed` (also for a subclass of a declarative class) |
| U2 | `buildTable` | class or column D |
| U3 | `Link` | count in `schema_model_core_tables` |
| U4 | `Link` | count in `schema_model_core_tables` |
| X1 | `site` | set or facts not complete; **(Rev 3.1)** forms `PK` (primary key open), `FK` (foreign keys unknown), `replace` (columns unknown), `table_attr` (schema and listed elements unknown); **(Rev 3.2)** forms `table_identity` (an attribute assignment that may rename the table: `schema_model_not_claimed`, no table) and `column_identity` (one that may rename a column: no column node) |
| U5 | `site` | `ddl_hooks: true` |
| I1 | none | after v1 (V7, P1) |
| I2 | none | after v1 |
| D | `class` | none; partial with reason D |
| L1 | `resolve` | resolution through the Python resolver; **(Rev 3.3)** the K-dec builtins `classmethod`, `staticmethod`, `property` only when unbound |
| L2 | `class` | cross-file chain and base proof |
| L3 | `emit` | table and column nodes, contains edges |
| L4 | `resolveForeignKeys` | foreign_key edges |
| L5 | `compareMigrations` | `schema_model_migration_overlap` |
| L6 | `Link` | `schema_model_unread`, `schema_model_core_tables` |
| L7 | `buildTable` | `schema_model_import_failure`, no table (forms F7r, F4, F9, F5, primary key, M12) |
| L8 | `site` | site receivers, test scope |

## Tests

`internal/extract/models/sqlmodel`: `TestModelClaimContract` (rows, docs and tags
1:1; each form of L7 reached; the kit's helpers in
`internal/extract/models/modeltest`), `TestModelClaims` (claims per known
fact, rows per fact, verification), unit tests per linking row,
`TestStepIsolation` (a failing step leaves the others linked), `TestModelLinkFixture` (graph golden of
`testdata/fixtures/sqlmodel-link`), and `TestModelOracle` (skipped unless
`GRAPHVEIN_MODEL_ORACLE` and `GRAPHVEIN_MODEL_ORACLE_ROOT` are set),
which compares the claims with `scripts/oracle/sqlmodel_metadata.py`'s
output. `TestSQLModelFixture` (`internal/lang/python`) holds the graph
golden of `testdata/fixtures/sqlmodel-models`.
The kit (`internal/extract/models`) has its own tests over a tool-neutral
record set: `TestLink` (group key, `dialect` only when stated, claims with
file, span and verification, no claim on a null fact or an unknown column,
schema-aware foreign keys), `TestForeignKeySchema`, `TestStepsGuard` and
`TestCatalogues`.
