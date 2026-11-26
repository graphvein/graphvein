# Claim contract: Python model linking

The claim contract of the Python model linker (ADR 0054) as recorded when it was
designed (task T018c2, internal record). `docs/extract/schema-models.md` and the code
table in `internal/extract/models/sqlmodel` must equal these rows.

## Claim contract

The rows of [python-models-syntax.md](python-models-syntax.md) "Claim contract" (61
rows, C1 ... D) are T018c2's claim rows, read in their claim and refusal
columns, with Revision 2 applied: I1 and I2 are after v1 (no claim, no
withdrawal), and every "I1" in a refusal cell reads "P1 when proven, else
V7". T018c2 adds the linking rows below. `docs/extract/schema-models.md`
copies T018c1's row ids and the linking rows; the contract test keeps the
code 1:1 with both.

| id | form | claim | refusal / unknown |
|---|---|---|---|
| L1 | resolution of a base, callee, annotation name or type expression | a name is `sqlmodel.X`, `sqlalchemy.X`, `pydantic.X`, `typing.X`, `enum.X`, `uuid.UUID`, `datetime.X`, `decimal.Decimal`, `pathlib.Path` or `ipaddress.X` only when the resolver's binding is the external module and name (SQLModel 0.0.39's re-exports of SQLAlchemy names count as `sqlalchemy.X`; `sqlalchemy.types`, `sqlalchemy.sql.sqltypes`, `sqlalchemy.schema` and `sqlalchemy.sql.schema` count as `sqlalchemy`); a builtin (`str`, `int`, `float`, `bool`, `bytes`, `object`, `dict`, `list`, `set`, `tuple`, `type`; **(Rev 3.3)** the K-dec decorators `classmethod`, `staticmethod`, `property`) only when the name is unbound in the module and its enclosing scopes (**(Rev 3.3)** a binding that aliases the builtin, `classmethod = builtins.classmethod`, also shadows it); a repository class only when it resolves to one unconditional class binding (count 1) | anything else: unresolved, and the row that asked gets its own unknown |
| L2 | the chain (C2) and the direct-base proof (R-B2) across files | a repository base is followed to its captured `PythonModelClass` record (same file, same enclosing scope, same name); proven non-table per R-B2 | a base without a captured record (file outside G1, members not captured): not proven, so no table claim (diagnostic `schema_model_not_proven`) |
| L3 | node shapes | one `table` node per claimed class, one `column` node per column the table's chain defines (known or unknown), `contains` class -> table and table -> column | a class without table claim: no node, a diagnostic with the row |
| L4 | foreign-key target resolution (F4, K3 targets) | the table part names exactly one claimed model table of the same repository with that name and schema (both default when the target has no schema) and production/test scope compatible; the column part names a column of it: one `foreign_key` edge column -> column, reference `status: "resolved"` | no such table, or that column not among its known columns: `unresolved`, no edge; more than one candidate table: `ambiguous`, no edge (diagnostic `schema_unresolved_reference`) |
| L5 | model tables and migration sets of the same repository (pending developer decision PD2) | never merged: model tables form their own group (`set` = `models`, `source: "model"`); a model table whose name equals a table of a migration set of the repository gets an info diagnostic `schema_model_migration_overlap` naming the set and, when both tables are `complete`, the columns only one side has | no diagnostic when no name matches; no field-by-field verdict (which database a model maps to is runtime configuration) |
| L6 | unread model forms | info diagnostics with counts, one per file: `schema_model_unread` for C1c, D classes, N3 and U1/U2 classes; `schema_model_core_tables` for G4 lines (candidates by spelling: Core `Table(...)` and imperative mapping, U3/U4) | always (markers only, so no view shows "no models") |
| L7 | P1 proven failures | diagnostic `schema_model_import_failure` naming the row and the library check, no table for that class | only these forms are proven from one class statement and its proven chain: F7r (`sa_column` with a listed flag keyword in a resolved `sqlmodel.Field`), F4's `ondelete="SET NULL"` with `nullable` proven false, F9's keyword outside `sqlmodel.Field`'s signature (pinned list), F5's mapping of a proven type outside the mapping (a builtin generic, `Literal`, a union with more than one non-`None` member, a builtin outside F5) without `sa_type`/`sa_column`, **(Rev 3)** a complete column set with no primary-key column whose constraint set is also proven to hold no primary key (no `__table_args__` in the chain, or a tuple whose elements are all `UniqueConstraint`/`Index`/`CheckConstraint` calls, TA1-TA3), and M12's unannotated non-underscore assignment of a literal (pydantic's non-annotated attribute check) when the name is bound once in its class, annotated in no chain class, not a SQLModel class variable (`metadata`), and every chain class has no unread statement and a readable config; every other import failure is V7 (a `PrimaryKeyConstraint`, an options dict or any other element may hold the primary key: the table is claimed, its `primary_key` and the columns' absent `primary_key: false` are unknown) |
| L8 | mutation sites and DDL hooks (X1, U5) across scopes | a receiver that resolves, through the site's scope bindings (`table = C.__table__`) or a dotted `C.__table__...` chain, to a claimed model class affects that table only; a site in a test-scope path affects only test-scope tables | an unresolved receiver or argument in a production path affects every production model table (constraint set or column set not complete; an attribute assignment makes the named column, or every column when unnamed, unknown); **(Rev 3.1)** on every affected table, a fact the site may change is unknown, never kept: an added constraint (`append_constraint`, set add, constructor call) whose kind is not proven a resolved `UniqueConstraint`/`Index`/`CheckConstraint` call makes the primary key open as in L7's V7 case (table `primary_key`, the columns' absent `primary_key: false` and K2's derived nullability unknown) unless proven a `ForeignKeyConstraint`, and the columns' `foreign_keys` unknown unless proven a `PrimaryKeyConstraint`; `append_foreign_key` makes the named column's (every column's when unnamed) `foreign_keys` unknown; `append_column` with any argument besides the column other than `replace_existing=False` makes every column unknown; an attribute assignment that names no column also makes the schema and the listed constraint elements unknown; **(Rev 3.2)** an attribute assignment keeps a claim only when its target is proven to leave the claim's identity (table name, schema, fullname; column name, key) unchanged: on the table only a single segment `comment`, `info`, `primary_key`, `indexes`, `constraints` or `foreign_keys` (`T.__table__.comment = ...`; the Rev 3.1 withdrawals apply); on a column (`....c.<col>.<attr>`) only a single segment `nullable`, `type`, `default`, `server_default`, `onupdate`, `server_onupdate`, `primary_key`, `index`, `unique`, `autoincrement`, `comment`, `doc`, `info` or `foreign_keys`, or a longer chain whose first segment is `type`, `info`, `default`, `server_default`, `onupdate` or `server_onupdate` (the column's facts unknown, as before); any other single column segment (`name`, `key`, ...) or the column itself (`....c.<col> = ...`) refuses that column claim on every affected table (no column node; column set and listed constraint elements not complete); any other target (`name`, `schema`, `fullname`, another table segment or a longer table chain, a column chain through another segment such as `c.x.table.name`, a target the capture cannot spell such as a subscript) refuses the table claim on every affected table (`schema_model_not_claimed` naming X1, no table or column node; foreign keys to it unresolved) |

### Pending developer decisions (conservative option implemented)

- **PD1 Read API and Schema view.** The schema read API (ADR 0048) groups
  by the stored `set` and derives set states from migration nodes; the
  Schema view (T018b2) labels every set as migrations. Showing model
  groups needs a presentation decision (D7: "switchable sources later").
  Implemented (conservative): the `/api/v0/schema/*` endpoints exclude
  nodes with `source: "model"` (their responses stay as before, so the
  view cannot mislabel a model table as a migration table, and "No
  migrations found" stays true), and model tables are readable through
  the graph (CLI `query`, node endpoints). ADR 0054 specifies the proposed
  API extension (a `source: "models"` group with model-specific states)
  for after the mockup decision. Follow-up: Schema view mockup for models.
- **PD2 Comparison with migration sets.** Implemented as L5: a name
  overlap diagnostic with the column-name difference of complete tables,
  no merge, no edge. Rejected for now: field-by-field verdicts (would
  assert that the model and the migration describe the same database).
  No corpus has both sqlx sets and SQLModel models, so the decision only
  matters for future corpora.
