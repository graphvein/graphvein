# Code-to-table linking

Task T018h (claim contract: `docs/contract/claims/table-access.md`; Revisions 1 to 5 in the internal task record), ADR
0060. Package `internal/extract/tableaccess`, called once from
`internal/index/index.go` after the Drizzle front end. Contract version
unchanged: the edges are contract v1 `reads_table` / `writes_table` edges
and everything else is an open node attribute.

Implemented so far: rows CT-S1 and CT-S2 (sqlx), the SQL subset reader
CT-Q1 with name resolution CT-Q2, and row D's execution records; rows CT-P1..P5 (SQLModel / SQLAlchemy) and row D's Python execution
records; rows CT-R1..R3 (Prisma) and row D's Prisma execution
records, and `site_partial` entries (Revision 3) for Prisma and Python
; rows CT-Z1..Z4 (Drizzle) and row D's Drizzle execution
records. Every row has its branch. T024c2 adds the Supabase
rows SB-K1 and SB-T1..SB-T5 ("Supabase rows" below). Revision 5
added class rules (below).

- **Isolation (R5-B3).** Every per-file step (the sqlx, SQLModel, Prisma
  and Drizzle passes over one file) runs in its own guard: a failure
  drops that file's sites and unknown records and becomes one
  `table_access_internal_error` diagnostic (warning, the file's path, the
  step); every other file is linked as before. The index call is guarded
  too: a failure outside the per-file steps (a group or the aggregation)
  drops the pass's edges and attributes as one such diagnostic, never the
  index run.

## What is emitted

- **Edges.** One `reads_table` or `writes_table` edge per (kind, owner,
  table) (W2), plain contract v1: `provenance` `fact` (confidence 1) when
  at least one of its sites is a fact, else `inferred` (confidence 0.9);
  `evidence` lists every site (file and the whole invocation's span),
  sorted. The target is the table node the proof names (O4): for SQL
  text, the table of the package's migration set that CT-Q2 resolved.
- **`table_access`** on the owner node (Revision 2): one entry per edge,
  sorted by (kind, target): `{kind, target, tool, rows, operations,
  assumes, role, verification}`. Consumers join an edge with its owner's
  entry by (kind, target).
  - `tool`: `sqlx`, `sqlmodel`, `prisma`, `drizzle`, or `sql` for SQL text
    in a non-sqlx carrier; tools of several sites sharing an edge are
    sorted and joined with `,`.
  - `rows`: sorted distinct contract rows of its sites (a sqlx site whose
    SQL was read and resolved is `CT-Q1`, `CT-Q2` and `CT-S1` or `CT-S2`).
  - `operations`: sorted distinct `select`, `get`, `refresh`, `find`,
    `count`, `aggregate`, `insert`, `update`, `delete`, `upsert`, `merge`,
    `add`.
  - `assumes`: sorted TA-V ids of its inferred sites; omitted when the
    edge is a fact.
  - `role` (Revision 1, O2): `executes` when a site of the edge is executed
    in the owner, else `builds` ("builds a query on <table>"). For sqlx, a
    site is executed when a call of a sqlx execution method (`fetch`,
    `fetch_one`, `fetch_optional`, `fetch_all`, `fetch_many`, `execute`,
    `execute_many`) of the same owner holds it (chained, or as its
    argument). A Prisma site always executes: the client call is the
    query (its promise runs when awaited or passed to `$transaction`).
  - `verification` (Revision 1, O6): `oracle` when every row of its sites
    is one whose forms an oracle compared, else `mapped` ("mapped, not
    verified"). The SQL reader's forms (CT-Q1, CT-Q2, and the SQL carriers
    through them) are compared by the Postgres analyzer oracle below.
- **`table_access_unknown`** on the owner node (W5): one entry per site a
  row recognises but cannot bind, sorted by span: `{row, line, span, tool,
  reason, partial, candidates}`. `partial` is true when the same site also
  claimed a table (W4). `candidates` is `null` (any table of the tool's
  sources in the package) or the table identities the row narrowed to.
  Absent when a node has none.

Owner (W1): the innermost node the language extractor emits around the
site (function or method; a closure belongs to its enclosing function;
else the file node); for Rust, the payload's `Owner`. Test files are read
like any code (W6); the owner node carries its own scope. A site resolves
only against the schema sources of its own package (W7): for sqlx, the
migration sets whose `package` is the Cargo.toml nearest to the site's
file.

The edges are a lower bound: triggers, views, database functions and
cascades also touch tables, and no row claims them.

## Assumptions

- **TA-V1** The SQL text a modelled carrier runs, runs against the database
  the resolved schema source describes. sqlx `query!` checks this at
  compile time; for the other carriers it is an assumption, so their SQL
  edges are `inferred`.
- **TA-V2** An ORM model class, delegate or table const names the table its
  schema source emits; runtime table renames are not followed.
- **TA-V3a** A Prisma client receiver is what its name says.
- **TA-V3b** A Drizzle database or transaction receiver is what its name
  says.
- **TA-V3c** A SQLAlchemy session or connection receiver is what its name
  says.
- **TA-V4** Postgres semantics for SQL text: search_path `public`,
  identifiers folded to lower case unless quoted.

## Claim contract

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| CT-P1 | `select(E, ...)` resolved to `sqlmodel.select` or `sqlalchemy.select` (also `sqlalchemy.future`, `sqlmodel.sql.expression`), with entities a model class, a model column `M.c`, `col(M.c)`, or `aliased(M)`; chained `.select_from(M)`, `.join(M[, on])`, `.outerjoin(M)`, `.join_from(A, B)` on the same chain | `reads_table` to each model table (`operation: select`); `fact` (no receiver involved) | an entity or join target that is not a resolved model (`func.count()` without `select_from`, `t.c.x` of a Core `Table`, a CTE or subquery `.c`, a parameter): no claim for it, unknown record (`partial` when another entity is claimed); no claimed entity: unknown; a model referenced only in `.where()`/`.order_by()` (implicit FROM): not claimed (follow-up) | TA-V2 |
| CT-P2 | `insert(M)`, `update(M)`, `delete(M)` resolved to `sqlmodel`/`sqlalchemy` (also `sqlalchemy.dialects.postgresql.insert`, `...sqlite.insert`), any chain (`.where`, `.values`, `.on_conflict_do_update`, `.returning`) | `writes_table` to `M` (`insert`/`update`/`delete`; `on_conflict_do_update`: `upsert`); `fact` | target not a resolved model (a Core `Table`, `sa.table("n", ...)`): unknown; `insert(M).from_select(cols, select(...))`: the inner select is its own CT-P1 site | TA-V2 |
| CT-P3 | `<r>.get(M, ...)`, `<r>.get_one(M, ...)` with `M` a resolved model class | `reads_table` to `M` (`get`); `fact` when `<r>` is a parameter annotated with a name resolving to `sqlmodel.Session`, `sqlalchemy.orm.Session`, `sqlalchemy.ext.asyncio.AsyncSession` or `sqlmodel.ext.asyncio.session.AsyncSession`, else `inferred` (`assumes: TA-V3c`) | first argument not a resolved model: no site (not recognised; `dict.get` and the like) | TA-V2, TA-V3c |
| CT-P4 | `<r>.add(o)`, `<r>.delete(o)`, `<r>.merge(o)`, `<r>.refresh(o)`, `<r>.add_all([o, ...])` (list literal), with the class of each `o` proven: an inline `M(...)`, `M.model_validate(...)`, `M.from_orm(...)` (deprecated alias), a local name with one unconditional assignment from one of those in the same function, or a parameter annotated `M`, `M \| None` or `Optional[M]` | `writes_table` to `M` (`add`, `delete`, `merge`), `reads_table` (`refresh`); fact/inferred as CT-P3 | the class of `o` not proven (a loop variable, a query result, another call, an outer variable, an unannotated parameter), `add_all` of anything but a list literal of proven objects: unknown (`object_class_unproven`) | TA-V2, TA-V3c |
| CT-P5 | `text("<literal>")` resolved to `sqlalchemy.text`/`sqlmodel.text`, the literal a plain string (concatenation of literals allowed) | the SQL subset reader (CT-Q1, CT-Q2) over the literal, against the SQLModel group's tables (`__tablename__`, else the class name lower-cased, as T018c2 names them); every edge `inferred` (`assumes: TA-V1`), `tool: "sql"` | an f-string, `%`/`.format` or a non-literal argument: unknown (`sql_not_literal`); the reader's refusals | TA-V1, TA-V4 |
| CT-R1 | `<r>.<delegate>.<op>(args)` in a package with exactly one Prisma group whose models include `Delegate` (delegate = model name with its first letter lower-cased); `op` read: `findUnique`, `findUniqueOrThrow`, `findFirst`, `findFirstOrThrow`, `findMany`, `count`, `aggregate`, `groupBy`; write: `create`, `createMany`, `createManyAndReturn`, `update`, `updateMany`, `updateManyAndReturn`, `upsert`, `delete`, `deleteMany` | `reads_table` or `writes_table` to the model table (`find`, `count`, `aggregate`, `insert`, `update`, `upsert`, `delete`); always `inferred` (`assumes: TA-V3a`; receivers are `this.prisma`, an imported module object, `tx`) | `<r>[expr].<op>` (dynamic delegate): unknown with the group's tables as `candidates`; an `op` outside the lists (`$extends` model methods): no site; a delegate that is not a model: no site | TA-V2, TA-V3a |
| CT-R2 | in a CT-R1 site's argument object: relation fields of the model (from the Prisma group) under `include`, `select` or a relation filter (`some`, `every`, `none`, `is`, `isNot`) in `where`, nested at any depth; under `data`, a relation field with `create`, `createMany`, `connectOrCreate`, `upsert`, `update`, `updateMany`, `delete`, `deleteMany` | `reads_table` to each related model read; `writes_table` to each related model nested-written; same provenance as its CT-R1 site | an object with a spread (`Spread`, payload 1): `partial` unknown; `connect`, `disconnect`, `set` (the table holding the key depends on the relation side; implicit many-to-many writes the join table): unknown for that relation (`relation_write_side`); a key that is not a relation field: nothing | TA-V2, TA-V3a |
| CT-R3 | `<r>.$queryRaw`...``, `<r>.$executeRaw`...`` (tagged; substitutions are bound parameters by Prisma's semantics), `<r>.$queryRawUnsafe("<literal>", ...)`, `<r>.$executeRawUnsafe("<literal>", ...)` | the SQL subset reader over the text (a substitution read as a parameter `$n`), against the package's Prisma group tables (`@@map` name, else the model name; `@@schema`, else `public`); `inferred` (`assumes: TA-V1, TA-V3a`), `tool: "sql"` | a non-literal argument, `Prisma.sql`/`Prisma.raw`/`Prisma.join` fragments (hoppscotch binds them to locals): unknown (`sql_not_literal`); the reader's refusals | TA-V1, TA-V3a, TA-V4 |
| CT-Z1 | `<r>.select(...)`, `.selectDistinct(...)`, `.selectDistinctOn(...)` followed on the chain by `.from(t)`; `.leftJoin(t, ...)`, `.innerJoin`, `.rightJoin`, `.fullJoin`, `.crossJoin` (and `...Lateral`); `<r>.$count(t)`; with `t` a table const of a Drizzle group of the package (W7), resolved as the Targets table says | `reads_table` to each `t` (`select`, `count`); `inferred` (`assumes: TA-V3b`) | `t` a subquery (`.as("x")`), a `$with` CTE, a view, a parameter of a generic helper, or anything else unresolved: no claim for it, unknown (`partial` when another table is claimed) | TA-V2, TA-V3b |
| CT-Z2 | `<r>.insert(t)`, `<r>.update(t)` (with `.from(u)`: also reads `u`), `<r>.delete(t)`, any chain (`.values`, `.set`, `.where`, `.onConflictDoUpdate`, `.onConflictDoNothing`, `.returning`) | `writes_table` to `t` (`insert`, `update`, `delete`; `onConflictDoUpdate`: `upsert`); `reads_table` to `u`; `inferred` (`assumes: TA-V3b`) | as CT-Z1; insert ... select is `.insert(t).select(qb)` (Revision 4): the inner chain is its own CT-Z1 site | TA-V2, TA-V3b |
| CT-Z3 | `<r>.query.<key>.findMany(cfg)`, `.findFirst(cfg)`, where `key` is the exported name of a table const in the schema modules of exactly one Drizzle group of the package (kaneo and dokploy pass `import * as schema`); `with: { rel: ... }` nested at any depth, `rel` an entry of the table's `orm_relations` (DR-R1) | `reads_table` to the key's table and to each `with` relation's target (`find`); `inferred` (`assumes: TA-V3b`) | a key that is no table const or is one in two groups: unknown; a `with` key not in `orm_relations`, a `cfg` with a spread: `partial` unknown; `defineRelations` (1.x): D | TA-V2, TA-V3b |
| CT-Z4 | `<r>.execute(sql`...`)`, and on sqlite `<r>.run`, `.all`, `.get`, `.values` with an `sql` tagged template resolved to `drizzle-orm`'s `sql` | the SQL subset reader over the template, where a substitution in a value position is a parameter and a substitution in a table position (CT-Q1) that resolves to a table const of the group is that table; against the package's Drizzle group tables (name, `pgSchema`, else `public`); `inferred` (`assumes: TA-V1, TA-V3b`), `tool: "sql"` | `sql.raw(...)`, `sql.identifier(...)`, a substitution in a table position that is no table const, any other structural substitution: unknown (`sql_dynamic`); the reader's refusals | TA-V1, TA-V3b, TA-V4 |
| CT-S1 | `sqlx::query!`, `query_as!`, `query_scalar!` and their `_unchecked` forms (path `sqlx::<m>!` or `<m>!` imported by `use sqlx::...`), first string argument a literal (after the record type for `query_as!`) | the SQL subset reader over the literal against the sqlx migration set of the same Cargo package (T018a discovery); `fact` (sqlx checks the text against the migrated database at compile time), `tool: "sqlx"` | the package has no readable migration set, or the set is wholly opaque: unknown (`no_schema_source`); the reader's refusals | TA-V1, TA-V4 |
| CT-S2 | functions `sqlx::query("<lit>")`, `query_as::<_, T>("<lit>")`, `query_scalar("<lit>")`, `query_with`/`query_as_with` with a literal (fixture-only: 0 corpus sites) | as CT-S1, but `inferred` (`assumes: TA-V1`): unlike the macros, the text is not checked at compile time | a non-literal argument (`&format!(...)`, a variable): unknown (`sql_not_literal`) | TA-V1, TA-V4 |
| CT-Q1 | SQL subset reader (no claim of its own; tagged with its carrier): one statement (an optional trailing `;`), comments ignored, kinds `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `WITH` (writable CTEs included); table references after `FROM`, `JOIN`, comma joins, `INSERT INTO t [(cols)]` (`VALUES` or `SELECT`), `UPDATE t ... [FROM ...]`, `DELETE FROM t [USING ...]`, in subqueries at any depth, `[ONLY] [schema.]name` with an optional alias; `ON CONFLICT ... DO NOTHING / DO UPDATE` (`EXCLUDED` is not a table), `RETURNING`, lock clauses `FOR UPDATE / NO KEY UPDATE / SHARE / KEY SHARE` | writes: the INSERT, UPDATE and DELETE targets; reads: every other reference; a lock clause reads; the UPDATE/DELETE target is not also read; function calls in `FROM` (`unnest(...)`, set-returning functions) are no tables | another statement kind (DDL, `COPY`, `CALL`, `SET`, `LOCK`, `DO`, `EXPLAIN`, `MERGE`), several statements, `LATERAL` function forms the reader cannot delimit, a parse failure: unknown (`sql_outside_subset`); a statement with no table reference (`select now()`): no claim, no unknown | TA-V4 |
| CT-Q2 | name resolution for CT-Q1 references | an unqualified name: a CTE of the statement shadows it (no claim); else `public.<name>` folded to lower case unless quoted; a qualified name: that schema; claimed when exactly one table of the carrier's schema source has that schema and name (migration set: the tables it holds at its end; model groups: their tables) | a name found in no table of the source (a view, a table created outside the set, a typo): unknown (`sql_name_unresolved`, the name in the reason); a migration set whose opaque statements may create tables (`set_opaque_statements` > 0) and a name not found: unknown; a name two sources of the package hold: unknown (`ambiguous_group`) | TA-V4 |
| D | anything else: legacy `session.query(M)` and Flask-SQLAlchemy `M.query` (0 corpus sites), SQLAlchemy Core `Table` objects, driver-level `cursor.execute("...")`/`conn.execute("...")` (sqlite3, asyncpg), app helpers that wrap SQL or models (umami `rawQuery`/`pagedQuery`), generic repository helpers whose table is a parameter, `query_file!`, Prisma `$extends` methods, Drizzle 1.x forms, Diesel, GORM, Sequelize, TypeORM | nothing; an SQL-shaped string literal (`^\s*(select\|insert\|update\|delete\|with)\b` with `from`, `into` or `set`) passed to any call in a captured file adds an unknown record (`sql_carrier_unmodelled`); every other D form is invisible | always | - |

Rows CT-Q1 and CT-Q2 have no branch of their own; their refusals are
reasons the carriers' unknown records name. Branches in the code
(`contract.go` `Branch`): CT-P1 `claimPythonSelect`, CT-P2
`claimPythonWrite`, CT-P3 `claimPythonGet`, CT-P4 `claimPythonObject`,
CT-P5 `claimPythonText`, CT-R1 `claimPrismaDelegate`, CT-R2
`claimPrismaRelations`, CT-R3 `claimPrismaRaw`, CT-Z1 `claimDrizzleSelect`,
CT-Z2 `claimDrizzleWrite`, CT-Z3 `claimDrizzleQuery`, CT-Z4
`claimDrizzleSQL`, CT-S1 `claimSqlxMacro`, CT-S2
`claimSqlxFunction`, CT-Q1 `readSQL`, CT-Q2 `resolveNames`, D
`unknownExecution` (its Python half is `unknownPythonExecution`; its
Prisma and Drizzle halves are in `linkPrismaFile` and `linkDrizzleFile`).

The contract test is complete: `TestTableAccessContract` (the three
tables equal, every listed form named in its row) and
`TestTableAccessTags` (the oracle fixtures emit a tag for every row with a
branch and each of its listed forms, no tag outside the contract, and
every claim row and D has a branch).

## Python rows (CT-P1..CT-P5)

Sites are the payload's `Queries` (`docs/lang/python-models.md`, "Query
sites"); names resolve through the shared Python resolver; a model is a
class binding (one stable `class` statement) whose class node is the
`class` of a SQLModel group table (T018c2). The group is the repository
(W7).

- **Statements.** A chain's first link whose callee resolves to a listed
  statement function (`sqlmodel.select`, `sqlalchemy.select`,
  `sqlalchemy.future.select`, `sqlmodel.sql.expression.select`,
  `sqlalchemy.sql.expression.select`; `insert`, `update`, `delete` of
  `sqlmodel` or `sqlalchemy`, `sqlalchemy.dialects.postgresql.insert`,
  `sqlalchemy.dialects.sqlite.insert`; `sqlalchemy.text`, `sqlmodel.text`)
  is a CT-P1, CT-P2 or CT-P5 site, through any import alias
  (`select as sm_select`, `import sqlalchemy as sa`).
- **CT-P1.** Entities are the first link's positional arguments: a model
  class, `M.c`, `col(M.c)` (`sqlmodel.col`), `aliased(M)`
  (`sqlalchemy.orm.aliased`, written inline; a local bound to `aliased(M)`
  is not followed: its argument is not cached). Join targets: the first
  argument of `.select_from`, `.join`, `.outerjoin`, the first two of
  `.join_from`. Any other entity or target is `entity_unresolved` (partial
  when another is claimed); a call entity (`func.count()`) is not
  unresolved when a `.select_from` on the chain names a model.
- **Implicit FROM (Revision 3).** A call entity whose reduced arguments
  name a model (`func.max(Item.id)`: `M` or `M.c` at any depth) adds that
  model to SQLAlchemy's FROM list, a form the row does not claim. When the
  site claims another table and nothing else is unresolved, the site keeps
  its claims and records `site_partial` (`partial: true`) with the named
  models it does not claim as `candidates` (`null` when an argument is a
  form the payload does not keep). A call entity naming no model keeps the
  rule above.
- **CT-P2.** The first argument must be a model class (`entity_unresolved`
  otherwise); `.on_conflict_do_update` on the chain makes the operation
  `upsert`. `.from_select(cols, select(...))`'s select is its own site.
- **Write predicates (R5-C3).** A `.where` or `.filter` argument of a
  CT-P2 chain that names another model where the payload keeps it (`M`,
  `M.c`, a column method `M.c.is_not(...)`, or such a name inside a call's
  arguments, `and_(Item.active, ...)`) can make the statement read that
  table (SQLAlchemy renders `UPDATE ... FROM`): the site keeps its claim
  and records `site_partial` (`partial: true`) with those models as
  `candidates`. A nested statement (`select(...)` in the predicate) is its
  own site. A model named only inside a comparison (`User.id ==
  Item.owner_id`) is not seen: the payload keeps a comparison as `other`
  (known gap, as for `.where` in selects).
- **Session methods (CT-P3, CT-P4).** The outermost link `<r>.<method>`.
  The receiver is proven (fact) when it is a single name that is a
  parameter of the site's function, or of an enclosing one with no nearer
  binding, annotated with a name resolving to a listed session class;
  otherwise it must be named as a session (a segment of `<r>`, or the
  annotation's name, contains `session`, case-insensitive: `self.session`,
  `db_session`, `db: SessionDep`) and the site is inferred (`TA-V3c`). Any
  other receiver (a dict, a set, a router) is no site. CT-P3: the first
  argument a model class, else no site. CT-P4: each object proven by
  `M(...)`, `M.model_validate(...)`, `M.from_orm(...)`, a local of the
  site's own scope with one unconditional assignment from one of those, or
  a parameter of the site's own scope annotated `M`, `M | None` or
  `Optional[M]`; one unproven object (or an `add_all` argument that is no
  list literal) makes the site `object_class_unproven` and claims nothing.
- **CT-P5.** A str literal argument (implicit concatenation is one
  literal in the payload) goes through the reader and CT-Q2 against the
  group's tables (`name`, `schema` of the T018c2 record; a table whose
  schema T018c2 could not prove is unresolved, as an `O3.P13` migration
  table is); anything else is `sql_not_literal`. A `text()` that is the
  An Alembic revision module (module-level `revision` and `down_revision`
  bindings) runs against the schema at its revision, not the models' end
  state, so its `text()` sites are `no_schema_source`. A
  `text()` that is the
  value of a DDL keyword argument (`server_default=`, `server_onupdate=`,
  `postgresql_where=`, `sqlite_where=`, `default=`, `onupdate=`, read from
  the source before the call) is a DDL fragment, not a site.
- **Role.** A session method executes. A statement site executes when a
  call of an execution method (`exec`, `execute`, `scalar`, `scalars`,
  `stream`, `stream_scalars`) of the same owner holds it (from the cached
  `Calls`), or (R5-A1) runs it through a local: the call's only argument
  is a bare name (read from the source) with one stable assignment in the
  site's own scope (`Count` 1, not conditional, no import) whose value is
  the site's chain (same start offset, a call on a name or attribute), the
  call has the site's owner and comes after the site
  (`statement = select(User).where(...); session.exec(statement)`); else
  it builds.
- **Row D, executions.** In a captured file, every call of an execution
  method whose span holds no statement site and that runs none through a
  local (R5-A1) is `statement_not_traced` (tool `sqlmodel`) on its owner:
  a statement bound to a local assigned twice, conditionally or in an
  outer scope, a parameter, a call result, a Core table's `update()`, or a
  driver-level `op.execute("...")` / `cursor.execute("...")`.
- **Shadowing (R5-B2).** The shared Python resolver binds the target of
  `except E as X`, `with f() as X` (and a `case P as X` pattern) in the
  enclosing scope, so a model name rebound there resolves to that local:
  `except Exception as User: session.exec(delete(User))` is
  `entity_unresolved`, never a claim on the model.

## Prisma rows (CT-R1..CT-R3)

Sites are the cached TypeScript calls (`facts.ClientCall`, every call with
its reduced callee chain, arguments, owner and scope;
`docs/lang/typescript-models.md`, "Call cache fidelity fields"). The
group is the Prisma schema whose discovery names the site's package
(nearest `package.json`, W7); discovery gives each package at most one
schema, so a package has at most one group. A file in a package without a
group has no Prisma site. Models and tables are the T018g1 records
(`model`, `name`, `schema`, `join_table`); relation fields come from the
Prisma front end's accessor `prisma.Relations` (what its claim pass
already parses: field name, related model, list, implicit many-to-many
side, relation attributes unread), no record change. Every site is
`inferred` with `TA-V3a` in `assumes` (Revision 1, O1) and `executes`.

- **CT-R1.** A call `<r>.<delegate>.<op>(args)` whose delegate is a model
  name with its first letter lower-cased (an `@@ignore`'d model has no
  delegate) and whose `op` is in the row's lists. Operations: `find`
  (`findUnique*`, `findFirst*`, `findMany`), `count`, `aggregate`
  (`aggregate`, `groupBy`), `insert` (`create*`), `update` (`update*`),
  `upsert`, `delete` (`delete*`). `<r>[expr].<op>` is `entity_unresolved`
  with every table of the group as `candidates`. Anything else (a name
  that is no model, an operation outside the lists, a delegate with no
  receiver) is no site.
- **CT-R2.** The first argument object of a CT-R1 site is walked by model:
  `include` and `select` (a relation field with `true` or an object, whose
  own `where`, `include`, `select` and `orderBy` are read for the related
  model; `false`, `null`, `undefined` select nothing); `where` (`AND`,
  `OR`, `NOT` nest, a relation field's `some`, `every`, `none`, `is`,
  `isNot` object reads the related model and nests its filter); `data`
  (and an upsert's `create` and `update`) of a write: a relation field's
  `create`, `createMany`, `connectOrCreate`, `upsert`, `update`,
  `updateMany`, `delete`, `deleteMany` write the related model (`insert`,
  `insert`, `upsert`, `upsert`, `update`, `update`, `delete`, `delete`) and
  their payloads are read for it. Related reads record `find`. The claims
  are a site of row CT-R2 with the CT-R1 site's span and owner.
- **CT-R2 refusals.** `connect`, `disconnect`, `set`: one
  `relation_write_side` entry per site (`partial: true`) naming each
  relation and operation, `candidates` the model's table, the related
  table and the join table (`null` when the side is not read).
- **Fluent API (R5-C1).** A relation field called on a single-record
  delegate call (`findUnique`, `findUniqueOrThrow`, `findFirst`,
  `findFirstOrThrow`, `create`, `update`, `upsert`, `delete`) or on such a
  fluent call (`prisma.user.findUnique(...).posts()`,
  `.author().profile()`) reads the related table, a form the rows do not
  claim: one `site_partial` entry (row CT-R2, `partial: true`, reason
  `site_partial: fluent relation <Model>.<field>`) at the fluent call,
  with the related table (and an implicit many-to-many's join table) as
  `candidates`, `null` when the related model has no table. The delegate
  call is its own CT-R1 site. Any other method on the result (`then`,
  `catch`) is nothing.
- **Partially read sites (Revision 3, `site_partial`).** One entry per
  site (`partial: true`, row CT-R2) lists every form whose tables the row
  cannot name, with `candidates` when every one narrows: an implicit
  many-to-many relation read or written (its join table), a list relation
  whose attributes the claim pass does not read, a relation to a model
  with no claimed table, `orderBy` over a relation, `_count` of relations,
  a to-one filter without `is`/`isNot` (`where: { author: { email } }`), a
  relation filter whose value is not an object (`is: null`), an object
  with a spread or an entry the reduction cannot name, and an argument,
  `where`, `include`, `select`, `orderBy` or `data` that is not an object
  literal (a variable may hold relation fields: `candidates` null). The
  site keeps its claims.
- **CT-R3.** `$queryRaw` and `$executeRaw` as tagged templates: the
  cooked template text (escape sequences decoded) with each substitution
  read as a bound parameter `$1`, `$2`, ... (Prisma's semantics); a call
  with parentheses (`$queryRaw(query)`) is `sql_not_literal`, and so is a
  substitution that is a `Prisma.sql`, `Prisma.raw`, `Prisma.join` or
  `Prisma.empty` fragment, a name imported from a `@prisma/` module, or a
  local bound to one of those. `$queryRawUnsafe` and `$executeRawUnsafe`:
  a string literal first argument (a template without substitutions
  included), else `sql_not_literal`. The text goes through the reader and
  CT-Q2 against the group's tables (`name`, `schema`; a table whose schema
  is not known is unresolved) when the group's provider is `postgresql`
  (TA-V4), else `no_schema_source`. `tool: "sql"`, `assumes: ["TA-V1",
  "TA-V3a"]`.
- **Row D, executions.** In a file of a package with a Prisma group, a
  `<r>.$transaction(...)` or `<r>.$queryRawTyped(...)` call whose span
  holds no Prisma site is `statement_not_traced` (tool `prisma`): a
  transaction over queries built elsewhere, a callback that calls helpers,
  a TypedSQL query whose `.sql` file is not read.

## Drizzle rows (CT-Z1..CT-Z4)

Sites are the cached TypeScript calls (`facts.ClientCall`); the groups
are the Drizzle table and column records (T018g2, ADR 0059) with the
configs `drizzle.Discover` proves (their schema modules). Every site is
`inferred` with `TA-V3b` in `assumes` (Revision 1, O1). The drizzle-orm
semantics are those of the pinned 0.45.4: `pg-core/db.js` (`select` 162,
`selectDistinct` 169, `selectDistinctOn` 177, `$count` 100, `update` 212,
`insert` 239, `delete` 266, `execute` 273), `pg-core/query-builders/
select.js` (`from` 47, the joins 198-347), `update.js` (`from` 52, joins
118-121), `insert.js` (`values` 29, `select` 53, `onConflictDoNothing`,
`onConflictDoUpdate`), `sql/sql.js` 104-206 (how a template renders its
substitutions).

- **Chains.** A chain's outermost cached call is read (every call of a
  chain is cached with its own arguments since the Revision 4 payload
  correction); its links are the method calls from the receiver out.
- **Table consts (Targets).** An argument resolves through the shared
  module resolver: an import (aliases, `export *` barrels, `export { a as
  b }`), a namespace member (`schema.users`), a property of a const
  object literal (kaneo's `export const schema = { userTable, ... }`), a
  module-level alias const (`export const user = userTable`), a local
  binding with one assignment, `alias(t, "a")` (drizzle-orm); it names a
  table when it reaches a module-level const whose identity is a Drizzle
  table record (`table:<file>#<const>`). W7 (Revision 4): such a proven
  table is claimed whatever package the site is in. Not tables, and the
  form recorded: a subquery (`.as("x")`), a `$with` CTE, a view
  (`pgView(...)`), a parameter, a destructured binding, anything else.
- **CT-Z1.** `<r>.select*(...)` (only `with(...)` before it) followed by
  `.from(t)`: `t` and every join's first argument are read (`select`);
  `<r>.$count(t)` reads `t` (`count`). A string, number, object or array
  literal argument is no site (another library's `from("users")`). An
  unresolved position is `entity_unresolved` (`partial` when the site
  claims a table).
- **CT-Z2.** `<r>.insert(t)`, `.update(t)`, `.delete(t)` (one argument):
  `t` is written (`insert`, `update`, `delete`; `onConflictDoUpdate`:
  `upsert`); an update's `.from(u)` reads `u`; a join on an update chain
  is no listed form (`site_partial`, the joined table as candidate);
  `.insert(t).select(qb)`: the inner chain is its own CT-Z1 site. An
  unresolved target is a site only when a builder link follows
  (`values`, `set`, `where`, `returning`, `onConflict*`, `from`, `select`):
  `cache.delete(key)` is no site.
- **CT-Z3.** `<r>.query.<key>.findMany(cfg)` and `findFirst`: `key` is an
  export name of the schema modules of a group of the site's package
  (the package's groups are the config's package and the
  packages holding its tables' files); its table is read (`find`), and
  each `with` entry that is an `orm_relations` entry reads its target,
  its own `with` read for that target. In a package without a group, or
  for a key no table const has, the site is `entity_unresolved`; a key two
  tables hold is `ambiguous_group`. `site_partial`: a spread, a `with` key
  not in `orm_relations`, a relation whose target is not claimed, a
  config or `with` that is not an object literal, a value other than
  `true`, `false` or an object.
- **CT-Z4.** `<r>.execute(sql`...`)` (and `run`, `all`, `get`, `values`
  in a package with a SQLite group) with `sql` imported from
  `drizzle-orm`: the template is rendered as drizzle-orm renders it (a
  table const as `"name"`, or `"schema"."name"` for a `pgSchema` table; a
  column as `"table"."column"`; any other value as a bound parameter
  `$n`) and read by CT-Q1/CT-Q2 against the package's postgresql groups
  (in a package without a group, the groups of the table consts the
  template names; none: `no_schema_source`; a SQLite or MySQL group:
  `no_schema_source`). `sql_dynamic`: `sql.raw(...)`, `sql.identifier`
  and any structural substitution (an `sql` fragment, a drizzle-orm
  operator, a query builder, an aliased table, a view), and a value in a
  table position (after `FROM`, `JOIN`, `INTO`, `UPDATE`, `USING`,
  `ONLY`, `TABLE`). `tool: "sql"`, `assumes: ["TA-V1", "TA-V3b"]`.
- **Verification (Revision 4).** CT-Z1 and CT-Z2 claims are `oracle` for
  postgresql groups only (the toSQL oracle compares pg-core builders);
  on a SQLite or MySQL group they are `mapped`. CT-Z3 is `mapped`.
- **Role.** A site executes, except a chain that ends without running
  (`.as`, `.toSQL`, `.getSQL`, `.prepare`, `$dynamic`) or that is passed
  directly to another call (a helper runs it): those build, unless their
  span lies inside a site that executes (a subquery of an executed
  statement). `execute(sql...)` executes.
- **W6.** The groups' schema module files are not sites (a view's
  definition reads tables in DDL).
- **W7 evidence.** In a package without a group, a site that claims
  nothing and an execution record are kept only in a file that imports
  drizzle-orm or holds a claimed Drizzle site; CT-Z3 sites are always
  kept.
- **Row D, executions.** In such a file, `<r>.transaction(fn)` whose span
  holds no Drizzle site, and `<r>.execute(x)` whose argument is an `sql`
  value that is not an inline template (a local bound to one, a generic
  `sql<T>`...``), are `statement_not_traced` (tool `drizzle`).

## sqlx rows (CT-S1, CT-S2)

- A site of the payload (`docs/lang/rust-sqlx.md`) is sqlx's when its path
  is `sqlx::<name>` or a single name that a `use` declaration of the site's
  module imports as `sqlx::<name>` (the walk stops at a nested module,
  which does not see its parent's `use`). Any other path is no site.
- The migration sets usable for a site are the Postgres sets of its
  package with at least one table. None: `no_schema_source` (a package
  without a set, or with only a SQLite or MySQL set or a set holding no
  table).
- A literal goes through the reader, then CT-Q2. A macro site is a `fact`;
  a function site is `inferred` with `assumes: ["TA-V1"]`.

## SQL subset reader (CT-Q1)

The reader lexes with Postgres rules (comments, nested block comments,
`'...'`, `E'...'`, `B`/`X`/`N` strings, `U&'...'`, dollar quotes, quoted
identifiers with `""`, `$n` parameters) and refuses an unterminated token
or a `U&"..."` identifier. It reads one statement (an optional trailing
`;`) starting with `SELECT`, `INSERT`, `UPDATE`, `DELETE` or `WITH`:

- References: every FROM item after `FROM`, `JOIN` or a comma, in
  parentheses (a parenthesised join), `[ONLY] [schema.]name` with an
  optional alias; the `INSERT INTO`, `UPDATE` and `DELETE FROM` targets;
  `UPDATE ... FROM` and `DELETE ... USING` lists; subqueries in FROM, in
  expressions (`(SELECT ...)`, `EXISTS`, `IN`) and in CTEs at any depth.
- Not references: function calls in FROM (`unnest(...)`, `ROWS FROM`,
  set-returning functions, and (R5-B1) the reserved words Postgres reads
  as functions without parentheses: `user`, `current_user`,
  `session_user`, `current_role`, `current_date`, `current_time`,
  `current_timestamp`, `localtime`, `localtimestamp`, `current_catalog`,
  `current_schema`, `system_user` (Postgres 16+); `FROM user` is `FROM current_user`, a one-row result,
  tagged `unnest(...)`), `EXCLUDED`, column references, `FROM` inside an
  expression's parentheses (`EXTRACT(x FROM y)`) and in `IS [NOT]
  DISTINCT FROM`. Quoted (`"user"`) or after a schema (`public.user`)
  such a word is a name like any other.
- Direction: targets are written, every other reference read; a target is
  not also read; a lock clause reads;
  `ON CONFLICT ... DO UPDATE` makes the INSERT's operation `upsert`.
- CTEs: a WITH list's names are visible to the main statement; in a
  non-recursive list each CTE sees the earlier ones, in a recursive list
  all of them. A writable CTE's target is written.
- Refused (`sql_outside_subset`, the detail names the form): another
  statement kind (DDL, `TRUNCATE`, `COPY`, `CALL`, `SET`, `LOCK`, `DO`,
  `EXPLAIN`, `MERGE`, a top-level `VALUES` or parenthesised query),
  several statements, `SELECT INTO`, `TABLESAMPLE`, `LATERAL` other than a
  subquery or function call, one of those keyword functions unquoted as
  a write target, after `ONLY` or as a schema (`INSERT INTO user`,
  `user.t`: syntax errors in Postgres), a WITH list inside a statement, a CTE
  `SEARCH`/`CYCLE` clause, a name of three or more parts, unbalanced
  parentheses, a lexing failure.
- A statement with no table reference (`select now()`) claims nothing and
  records nothing.

## Name resolution (CT-Q2)

- An unqualified name that a visible CTE has claims nothing (shadowed).
- Otherwise the name is `public.<name>` (TA-V4) when unqualified, else
  `<schema>.<name>`; folded to lower case unless quoted.
- It is claimed when exactly one table of the usable sets has that schema
  and name; a migration table with `schema: null` counts as `public`.
- A migration table with `schema: null` that carries a search_path reason
  (`O3.P13`) may live outside `public`: a match on it is unresolved
  (Revision 2).
- No table: `sql_name_unresolved: <schema>.<name>` (a view, a table created
  outside the set, an old name of a renamed table); the detail adds that
  the set has opaque statements when it has some. Two tables:
  `ambiguous_group` with both identities as `candidates`.
- A site with several unresolved references records one entry: the first
  reference's code, every reference's detail.

## Supabase rows (SB-K1, SB-T1..SB-T5)

Task T024c2 (`docs/contract/claims/nextjs-supabase.md`; decisions O5 revised
and O6; ADR 0077). Branches in `supabase.go`: SB-K1 `supabaseClient`,
SB-T1 `claimSupabaseTable`, SB-T2 `supabaseSchema`, SB-T3
`claimSupabaseEmbeds`, SB-T4 `supabaseDynamicTable`, SB-T5
`supabaseUnprovenClient`, D `supabaseDefault`. `TestSupabaseContract`
keeps this table, the task's and `SupabaseContract` 1:1 (forms named in
their rows, branches present) and `TestSupabaseTags` checks that
`testdata/fixtures/table-access-supabase` reaches every row and form.

- **SB-V1** The database the client talks to is the one the repository's Supabase migrations describe (as TA-V1): every SB edge is `inferred`.
- **SB-V6** (T024c3, served by the generated-types catalogue,
  `docs/extract/schema-supabase-types.md`) A package without a Supabase
  migration set resolves against its generated types; those edges carry
  `assumes: [SB-V6]` instead of SB-V1.

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| SB-K1 | a client receiver: a call of `createClient` (`@supabase/supabase-js`), `createBrowserClient` / `createServerClient` (`@supabase/ssr`), or the `@supabase/auth-helpers-nextjs` `create*Client` factories, resolved through the shared resolver; directly, through a const binding (module or function scope, imported consts included), or through one level of a function whose every return is a proven client (awaited when async) | the receiver is a Supabase client; its default schema is `public` | an options argument with a `db.schema` key: schema from a literal, else every site of that client goes to SB-T5; destructuring, parameters (also typed `SupabaseClient`), `let`, members: unproven | - |
| SB-T1 | `<client>.from('<t>')` with a string literal or bounded const, the first PostgREST verb on the chain `select`, `insert`, `update`, `upsert` or `delete`; (T024 review) a `const builder`: `const q = <client>.from('<t>')` (a const declared in the use's function, or both at module level, never reassigned) and a chain `q.<verb>(...)` in that function, read as the one chain | `reads_table` (`select`) or `writes_table` (`insert`, `update`, `upsert`, `delete`) to the table `<schema>.<t>` of the package's Supabase set (`tool: supabase`, `inferred`, `assumes: SB-V1`); `insert(...).select()` is the write only | a name the set does not hold as a table (a view, a table created outside the set): unknown (`sql_name_unresolved`); no Supabase set in the package: the package's generated types (SB-G1, T024c3; `assumes: SB-V6`), else unknown `no_schema_source` (table name in the reason); no verb on the chain and no const builder use that reads it (returned, passed on, a `let`): unknown `builder_unread` (the table in the reason; T024 review) | SB-V1 (a migration set), SB-V6 (generated types) |
| SB-T2 | `<client>.schema('<s>').from('<t>')...` | as SB-T1 with schema `s` | non-literal schema: unknown | SB-V1 (a migration set), SB-V6 (generated types) |
| SB-T3 | a `select` argument that is a string literal with embedded resources (`'*, prices(*, products(*))'`) | the base table (SB-T1); each embedded name `e` (alias `a:e` allowed, nested levels relative to their parent; (T024 review) a final join modifier `!inner` or `!left`, PostgREST's join type, leaves the resource as without it) that names a table of the set with exactly one relationship to its parent (one foreign key in either direction and no junction table joining the two): `reads_table` to `e`, `verification: mapped` | a `!hint` (any other name after `!`, also `e!hint!inner`), a spread `...e`, zero or several relationships, a name that is no table of the set, relationships not resolved (a set read from generated types, SB-G1): `partial` unknown (`embedded_resource`) naming the unresolved names; a non-literal select argument (T024c3): the base table and a `partial` unknown `embedded_resource` ("embedded tables not read") with the package's other tables as candidates | SB-V1 |
| SB-T4 | `.from(<non-literal>)` on a proven client with an SB-T1 verb | nothing | unknown `table_dynamic`, candidates = the set's tables | - |
| SB-T5 | `<x>.from('<lit>').<verb>(...)` where `<x>` is not a proven client | nothing | unknown `client_unproven` (structural chain only, never a receiver-name rule) | - |
| D | `.storage.from(bucket)`, `.rpc(fn)` (counted), `.auth.*`, `.channel(...)` realtime, `.functions.invoke`, `supabase-py` / `supabase-dart` | nothing | always | - |

How the rows are read:

- **Receivers (SB-K1).** The client is the chain's receiver expression,
  never its name: a call of a factory imported from its package (the
  import binding names the module and export), a `const` bound to one
  (function scope, or a module `const`, also imported through the shared
  module resolver), or a call of a repository function (one level) whose
  every return is such a client; an `async` wrapper counts only when its
  call is awaited (`const s = await createServer()`). A wrapper of a
  wrapper, `let` / `var`, destructuring, parameters (typed or not),
  members (`this.db`, `ctx.supabase`), namespace calls
  (`ssr.createBrowserClient`) and an options argument the caches do not
  read (not an object literal, a spread, a non-literal `db.schema`) leave
  the receiver unproven. The default schema is `public`, or the literal
  `db.schema` option (`options.db.schema` for the auth-helpers factories).
- **Package (W7).** A chain resolves against the Supabase sets whose
  `supabase/` directory belongs to the same npm package (nearest
  `package.json`) as the chain's file. SB-T5 records are made only in a
  package whose `package.json` declares `@supabase/supabase-js`,
  `@supabase/ssr` or `@supabase/auth-helpers-nextjs`, so another
  library's `from(...).select()` (knex) stays invisible.
- **Chains (SB-T1, SB-T2).** The outermost call of a chain is read:
  `<client>.from(t)` or `<client>.schema(s).from(t)`, then the next link
  must be the verb. Names are exact (PostgREST quotes identifiers, so no
  folding): a string literal (a template literal without substitutions
  included) or a module `const` bound to one ("bounded const"). The table
  is resolved like a CT-Q2 reference (`schema.name` against the set's
  tables), so a view or a missing table is `sql_name_unresolved` and two
  sets of the package holding it is `ambiguous_group`. The role is
  `executes` (the builder runs when awaited, as a Prisma call).
  (T024 review) A chain whose receiver is a const bound to
  `<client>.from(t)` (or `.schema(s).from(t)`) without a verb, declared in
  the use's function (or both at module level) and never reassigned (the
  shared route scopes: no `let` / `var`, no assignment), and whose first
  link is a verb, is read as the one chain `<client>.from(t).<verb>...`
  at the use's span ("const builder"); the binding's own chain then
  claims nothing. Every other chain without a verb on a proven client is
  an unknown `builder_unread` at its own site.
- **Embeds (SB-T3).** The first `select` link with a literal argument
  (the verb, or `.select(...)` after a write) is parsed as postgrest-js
  sends it (whitespace outside quotes removed). An item
  `[...][alias:]name[!x](...)` is an embedded resource; `col.sum()` and
  `count()` are aggregates. Relationships are counted from the set's
  `foreign_key` edges (one per referencing column, so a two-column key
  never counts as one) and only in a set whose tables are all complete
  and which has no opaque statement: elsewhere an unread statement may
  add a key or a junction table, so every embed is a partial unknown
  ("relationships not proven"). A junction is any third table with keys
  to both. A final `!inner` or `!left` is PostgREST's join type and is
  dropped (T024 review: the resource is resolved as without it); any
  other name after `!` (a hint, also `e!hint!inner`) refuses.
  Embedded claims are a site of their own (row SB-T3, `mapped`).
- **D.** `.rpc(fn)` on a proven client is a `statement_not_traced`
  record naming `rpc <schema>.<fn>` (the function runs statements whose
  tables are not traced); storage buckets, chains without a verb and
  everything else claim and record nothing.
- **Precision.** SB edges are `best_effort`: row PL-F9 covers
  code-to-table links into tables of the tools it lists, and a Supabase
  table is not one of them until the SB rows carry their own evidence
  (T024 "Precision label").

## Unknown records

A reason is a code from this list, optionally followed by `: ` and a
detail (the unresolved name, the refused form). The vocabulary is closed;
steps H-B4 to H-B6 use the codes their rows name.

- `sql_not_literal` (CT-S1, CT-S2, CT-P5, CT-R3): the SQL argument is not a
  string literal.
- `sql_outside_subset` (through CT-Q1): the reader refused the text.
- `sql_name_unresolved` (through CT-Q2): a reference names no table of
  the package's source.
- `ambiguous_group` (CT-Q2, CT-Z3, W7): two tables or groups hold the
  name or key.
- `no_schema_source` (CT-S1, CT-S2, CT-P5, CT-R3, CT-Z4): the package has
  no readable migration set (sqlx), no SQLModel table, the site is in an
  Alembic revision module, the Prisma group's provider is not
  `postgresql`, or no postgresql Drizzle group is the template's source.
- `statement_not_traced` (D, Revision 2): "runs a statement whose table is
  not traced here". For sqlx: in a file that imports sqlx, a call of a
  sqlx execution method whose span holds no captured query site (a
  statement built in another function, a QueryBuilder, a query bound to a
  local the caches do not keep, an `Executor` call on a string). For
  Prisma: a `$transaction` or `$queryRawTyped` call holding no Prisma site.
  For Drizzle: a `transaction(fn)` holding no Drizzle site, an
  `execute(x)` of an `sql` value that is not an inline template.
- `object_class_unproven` (CT-P4): the class of an object is not proven.
- `relation_write_side` (CT-R2): `connect`, `disconnect` or `set` on a
  relation: the table holding the key depends on the relation's side (an
  implicit many-to-many writes the join table).
- `sql_dynamic` (CT-Z4): the template's SQL depends on values the reader
  cannot see (`sql.raw`, `sql.identifier`, a structural substitution, a
  value in a table position).
- `sql_carrier_unmodelled` (D): an SQL-shaped literal passed to an
  unmodelled call. Not emitted yet: no cached Python or Rust call keeps
  its arguments with an owner (H-B4 open point).
- `entity_unresolved` (CT-P1, CT-P2, CT-R1, CT-Z1, CT-Z2, CT-Z3): an
  entity, join target,
  statement target or delegate that does not resolve to a model of the
  group (a Core `Table`, `sa.table(...)`, `func.count()` without
  `select_from`, a parameter, a local the caches do not follow, a dynamic
  Prisma delegate `client[model]`, a Drizzle subquery, CTE, view or
  parameter in a table position, a relational query key no table const
  has).
- `site_partial` (Revision 3; CT-P1, CT-P2, CT-R2, CT-Z2, CT-Z3): a site that keeps its claims
  while other tables it may touch are not named by the row (SQLAlchemy's
  implicit FROM, a model in a write's predicate (R5-C3), a Prisma fluent
  relation call (R5-C1), a Prisma join table, `orderBy` over a relation, a spread,
  an argument that is not an object literal, a join on a Drizzle update
  chain, a Drizzle `with` key outside `orm_relations`); `candidates` lists them when
  the row narrows them.
- `table_dynamic` (SB-T2, SB-T4): a Supabase chain's table or schema is
  not a literal or bounded const; `candidates` are the package's
  Supabase tables.
- `client_unproven` (SB-T5): a Supabase chain shape on a receiver SB-K1
  does not prove; the detail names the table and why.
- `embedded_resource` (SB-T3): embedded resources of a select string
  that resolve to no table under the rule (a hint, a spread, zero or
  several relationships, a junction table, no table of the set).
- `builder_unread` (SB-T1, T024 review): a Supabase query builder
  `<client>.from(...)` without a verb on its chain that no const builder
  use reads (returned, passed on, bound with `let`); the detail names the
  table, `candidates` are the package's Supabase tables.

## Oracle

`scripts/oracle/sql_tables.sh` with `TestSQLTablesOracle` (never run by
CI): for each migration set with a claimed site, a fresh database on the
scratch Postgres 17 cluster, the set's migrations applied in sqlx order
(down migrations skipped), and for each claimed site `PREPARE` under
`debug_print_parse`. The analyzer's `RTEPERMISSIONINFO` entries give each
relation's required permissions in every nested query: a relation with an
INSERT, UPDATE or DELETE bit is written, one with only SELECT read; the
UPDATE bit of a SELECT query is its lock clause and reads (A1); the
SELECT-only entry of the `EXCLUDED` range table entry (relkind `c`, alias
`excluded`) is not a read (A2). The claimed (reads, writes) sets must equal
the analyzer's per site; for a site with a `partial` unknown, each
relation the analyzer lists must be claimed or be no table of the set.

The SQLAlchemy fixture oracle (`scripts/oracle/sql_tables_sqlalchemy.sh`
with `TestSQLAlchemyOracle`, never run by CI) compares every claimed CT-P1
and CT-P2 site of `testdata/fixtures/table-access-sqlmodel/`: the statement's
source text is evaluated in its module with the pinned SQLModel 0.0.39 /
SQLAlchemy 2.0.51, compiled with the Postgres dialect, and its tables read
(a Select: every table under its final FROM list, joins and aliases
unwrapped; an Insert, Update or Delete: its target). The claimed (reads,
writes) must equal them; a site with a partial unknown must claim a subset.
The script also writes `SQLModel.metadata`'s DDL, which `sql_tables.sh
--model-ddl` applies as the schema of the CT-P5 sites (`:name` binds are
numbered `$n` for `PREPARE`). CT-P1 and CT-P2 are `oracle` because the
fixture reaches each of their forms (`TestTableAccessClaims`); CT-P3 and
CT-P4 stay `mapped`.

For a Prisma group with a claimed CT-R3 site, `sql_tables.sh` builds the
schema from the group's schema with the pinned Prisma CLI (the group's
`pins`, or `--prisma-version`), installed into the scratch directory from
npm: `prisma migrate diff --from-empty --to-schema <schema> --script`
(no database connection; Prisma 7 reads a datasource URL from a scratch
`prisma.config.ts`), and applies that SQL as the set's one migration. The
comparison is the same as for sqlx sites. CT-R3 is `oracle` because the
Prisma fixture reaches each of its forms; CT-R1 and CT-R2 stay `mapped`
(no ORM oracle, O6).

For a Drizzle group with a claimed site, `sql_tables.sh` builds the
schema with the group's pinned drizzle-kit (`drizzle_catalog.mjs prepare`
copies the schema modules into the scratch install; `drizzle-kit export`
prints the SQL, no database) and applies it as the set's one migration
(`--drizzle-extra` installs packages the modules import at load). A
CT-Z4 site's rendered SQL is compared as above. For every claimed CT-Z1
and CT-Z2 site of a postgresql group (the toSQL fixture oracle, Revision
1 O6), `sql_tables_drizzle.mjs` evaluates the builder's source text (a
trailing `then`, `catch`, `execute`, `as` or `prepare` dropped) with its
receiver bound to drizzle-orm's `pg-proxy` database at the same pin
(nothing connects), its table consts to the copied modules' exports and
drizzle-orm imports to the package, and writes `toSQL()`'s SQL; the
analyzer reads it like any SQL site. A builder's SQL holds the sites
nested in its span (a subquery that is its own site): their claims count,
and when one of them is unbound or partial, or the site itself is, the
claims must be a subset of the analyzer's relations. `--sql-only` skips
the toSQL sites (a corpus: the harness evaluates source text, so it is a
fixture oracle). CT-Z1, CT-Z2 and CT-Z4 are `oracle` because the Drizzle
fixture reaches each of their forms; CT-Z3 stays `mapped`.

The Supabase chain oracle (`TestSupabaseChainsOracle`, opt-in,
`scripts/oracle/nextjs_supabase/postgrest_chains.cjs`) builds every
claimed SB-T1 and SB-T2 chain with `@supabase/postgrest-js`'s query
builder offline and compares method (operation), URL path (table) and
schema. It runs on the fixture and (T024 review) on the checkouts of
`GRAPHVEIN_POSTGREST_ORACLE_CORPORA`: with postgrest-js 1.18.1 and 1.9.2,
fixture 29/29, chatbot-ui 130/130 and supabase-stripe 12/12 claimed
chains agree. So the `oracle` label of SB-T1 and SB-T2 edges means: the
row's forms are compared on the fixture, and on the corpora each edge's
own chain was compared. It compares the request the builder makes, not
whether the table exists (that is SB-V1's migration set, checked by
`TestSupabaseOracle`). SB-T3 embeds are `mapped`: postgrest-js passes the
select string through, so only a PostgREST server would resolve them.

## Known gaps

- Supabase: a client built in another way (a namespace import, a class
  field, a context value, a parameter typed `SupabaseClient`) gives
  SB-T5 records, never claims. A select argument that is not a literal
  (a const, a template with substitutions) claims the base table only and
  records nothing for its possible embeds. PostgREST computed
  relationships (functions taking the parent's row type) and
  relationships through views are not modelled: an embed can only claim
  a table with exactly one key to its parent. Edge functions under
  `supabase/functions` resolve against no set unless a `package.json`
  holds them.

- sqlx sites inside another macro's token tree, `query_file*` and
  `#[macro_use]` macros are not captured (`docs/lang/rust-sqlx.md`).
- A query bound to a local and executed later in the same function is
  `builds` and its execution is a `statement_not_traced` record: the Rust
  caches keep no bindings.
- The execution rule reads method names only: an `execute` or `fetch*`
  call on a non-sqlx receiver in a sqlx file is also recorded.
- A table created after `CREATE SCHEMA` in a set without a qualification
  carries an `O3.P13` reason and is not resolved (conservative).
- Python: a statement bound to a local is traced to its execution only
  under R5-A1 (one stable assignment in the same scope and owner, the
  local the call's only argument); any other local (assigned twice or
  conditionally, from an outer scope, passed with other arguments) is
  `builds` and its execution `statement_not_traced`. Rust keeps this gap
  in full (no bindings cached). The execution rule reads method
  names only (`execute` on any receiver in a captured file); a
  driver-level SQL literal has no `sql_carrier_unmodelled` record (no
  cached argument with an owner).
- Python: a session-method receiver whose name does not say session
  (`db.add(User())` with an unannotated `db`) is no site; a local bound to
  `aliased(M)` is unresolved.
- Python: a model referenced only in `.where()` or `.order_by()` (implicit
  FROM through a comparison) is neither claimed nor recorded: the payload
  keeps a comparison as `other`. Only a call entity's arguments are read
  for `site_partial`. The same holds for a write's predicate (R5-C3):
  `update(User).where(User.id == Item.owner_id)` claims `user` only and
  records nothing for `item`.
- Prisma fluent API (R5-C1): only relation fields called directly on the
  chain are read; a single-record call stored in a local and extended
  later (`const u = prisma.user.findUnique(...); u.posts()`) is not.
- Prisma: a generic tagged template (`prisma.$queryRaw<T>`...``) is
  parsed by the TypeScript grammar only through error recovery (no call,
  or a callee that is not a member chain), so it is no site. A delegate reached through a local (`const users =
  prisma.user; users.findMany()`), a `$extends` model method and a client
  whose schema lives in another package are no site. Relation reads by
  `orderBy`, `_count` and to-one filters without `is`/`isNot` are
  `site_partial`, not claims (they are not row forms).
- Drizzle: a destructured binding (`const { userTable: user } = schema`,
  `const { default: db, schema } = await import(...)`) is not cached, so a
  table position through it is `entity_unresolved`. A generic `sql<T>`
  template is not read (the grammar's error recovery; known gap as for
  Prisma); run by `execute` it is a `statement_not_traced` record.
  `sql` fragments inside builder arguments (`where(sql`...`)`, `set({ x:
  sql`...` })`) are not read: a table they name is neither claimed nor
  recorded (lower bound). A builder bound to a local and extended later
  (`$dynamic()`, `q.leftJoin(...)` on a separate statement) is read only
  as far as its own chain. A builder stored without running (a class
  field, a local kept for later) reads `executes` (rare).
  Executions are recognised by method name (`transaction`, `execute`) on
  any receiver in a Drizzle file.
