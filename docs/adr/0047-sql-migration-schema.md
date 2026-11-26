# ADR 0047 - Schema from sqlx SQL migrations

**Status:** Accepted (delegated review, 2026-11-04)

## Context

Milestone M2 of the project plan asks for schema extraction from SQL migrations. The T018 plan
(the T018 task record (internal)) found raw SQL migrations in two corpora,
both sqlx sets on PostgreSQL (axum: 4 files, axum-ddd: 5), and alterations
only in the Alembic corpora. T018a (the T018a task record (internal)
with Revision 1) defines the first schema extractor. Its investigation stopped once
(serial columns would have stated "no default"); Revision 1 added R1-R6 and
its R-A assessment (the T018a task record (internal)) found no further stop.

The extractor must never state a wrong schema fact (VISION, principle 6): what
it cannot read exactly has to stay visible.

## Options considered

### Parsing

- **tree-sitter SQL** (DerekStride/tree-sitter-sql) as a WASM grammar: not
  in the audited registry, so it needs a grammar-set build (ADR 0007/0028),
  a new cache namespace and an extractor bump; its multi-dialect error
  recovery does not follow PostgreSQL lexing. Rejected.
- **pg_query_go** (the Postgres parser): cgo, forbidden outside ADR 0002's
  package. Rejected.
- **Pure-Go SQL parsers** (vitess is MySQL-only; cockroachdb's parser is a
  large dependency with its own dialect): new dependencies for a subset the
  extractor can recognize directly. Rejected.
- **Hand-written Go** lexer per `scan.l`, statement splitter and
  recursive-descent recognizer for the modeled subset. Selected: no new
  dependency, no cgo, exact control over what is recognized.

### Where it runs

- A per-file cache of SQL syntax, like the language extractors: an extractor
  bump and a payload for little gain (migrations are small).
- **Link time over the retained sources**, like the OpenAPI import (ADR 0023
  R2). Selected: no cached payload, no extractor version change, warm runs
  parse zero files, and an edited migration changes only its set's records.

### Discovery

- Reading `migrate!` invocations: macros are in no cache (the Rust
  extractor skips them) and would need one.
- **Convention**: the `migrations` directory directly inside a Cargo package
  root whose package depends on sqlx. Selected; this is what `migrate!()`
  and `migrate!("./migrations")` resolve to (sqlx 0.8.6 `src/macros/mod.rs`
  816-818, 0.5.9 `src/macros.rs` 789-791, `sqlx-macros-core/src/common.rs`
  28) and covers both corpora.
- Dialect from the sqlx dependency's declared features in the package
  manifest, including `workspace = true` inheritance, renames, dev and
  target dependencies; exactly one database feature gives the dialect.

### Identity of unqualified and unnamed objects

- Assume `public` for unqualified tables (the contract example
  `table:migrations/001.sql#public.users`). Rejected: the schema comes from
  the database's search_path (default `"$user", public`), which source does
  not show; sqlx runs the file text unchanged.
- **Lexical names**: `<schema>.<name>` only when written, otherwise `<name>`
  with attribute `schema: null`. Selected. A reviewer preferring `public`
  makes it an identity change before merge.
- Unnamed indexes and constraints get generated names in Postgres that
  depend on other relations (`ChooseRelationName`). Selected: no generated
  names; an unnamed index is identified by its elements
  (`<table>.(a,b)`, `@2` in apply order for repeats). Name components that
  contain `.`, `"`, `(`, `)`, `,` or `@` are written in SQL double quotes so
  qualified names stay unambiguous.

### Partial versus opaque

- Mark every table partial after any opaque statement. Rejected: axum's five
  `select trigger_updated_at(...)` calls add an UPDATE trigger and change no
  column; partial would hide correct facts.
- **Count opaque statements** on every table existing at that point
  (`opaque_after`) and per set (`set_opaque_statements`); mark a table
  partial only when a statement about it cannot be applied exactly.
  Selected, with Revision 1's exceptions: search_path changes (R2) and
  `CREATE TABLE IF NOT EXISTS` after an opaque statement (R3) make the
  affected tables partial.

## Decision

`internal/extract/schema` implements the rules in `docs/extract/schema.md`:
sqlx discovery and file order (Q1-Q5 verified against sqlx v0.5.9 and
v0.8.6), the PostgreSQL REL_17_6 statement subset (P1-P5 and R1-R5), a
schema state of dialect-neutral operations (unexported until T018c/T018d
need it) and emission of:

- `migration` (level 3), `table`, `column`, `db_index` (level 4) source
  nodes;
- `contains` (file -> migration -> table -> column/db_index), `alters`
  (later migration -> table) and `foreign_key` (column -> column) fact
  edges;
- `schema_*` diagnostics.

The index calls it once per link pass after the other linkers; no other
linker reads its nodes. `Summary.Schema` measures the pass. Contract v1 is
unchanged: the kinds are extensible (precedents ADR 0036, ADR 0038).

Decisions taken in the implementation (Revision 1 and the T018b1 review
questions):

- Nullability is one attribute, `nullable` (R1's "not_null: true" means
  `nullable: false`).
- `set_opaque_statements` (`{total, by_kind}`) is written on every migration
  node of the set; there is no set node kind.
- R4 counts `CREATE EXTENSION` as opaque, so the corpus outcome is axum 6
  (5 calls, 1 extension) and axum-ddd 1, not Revision 1's stated 5 / 0.
- After an ordering failure, an invalid version gives `applied: false`,
  `order: null` for every migration; a duplicate version gives `applied`
  and `order` below the duplicate and `applied: null`, `order: null` from
  it on. Neither derives schema objects.
- R5 makes every table of the set partial, not only those after the
  member's version, because a non-UTF-8 member makes sqlx refuse the whole
  set and the pass cannot tell that case from a symlinked or ignored member
  without reading the file. To find such members the pass lists each
  discovered set directory (no file contents are read).

Revision 2 (independent review, the T018a task record (internal)
"Revision 2 (review)"):

- **Migrations that may fail.** Postgres parses a migration's whole query
  string before running it and runs it in one transaction; sqlx stops the
  set at a failed migration (migrator.rs 168-182 at 0.8.6). A Graphvein
  parse or lexing failure does not prove that Postgres fails, and a
  statement Graphvein knows Postgres refuses proves only that this file
  fails if the model is right. Either way the outcome of that migration and
  of every later one is unknown: the tables they create or alter are
  partial with a reason naming the failure, tables the failing migration
  dropped are restored as partial, and their `applied` is `null` (their
  `order` stays, since sqlx's order is still defined).
- **Unknown is not absent, also for drops.** A destructive change whose
  target name is uncertain (search_path, temporary tables, other
  qualifications, a possible failure) keeps the object and marks the table
  partial.
- **Not modeled means not no-effect.** A statement form is no-effect only
  when it cannot drop, rename or alter a table, column, index, default or
  nullability; otherwise it is opaque, unsupported with a conservative
  effect on the tables it can affect, or a parse error (which now means a
  possible failure).
- **Relation namespace.** Views, sequences, indexes, composite types,
  materialized views, unmodeled tables and CREATE SCHEMA elements are
  remembered by name, so a later CREATE TABLE of such a name is skipped
  (IF NOT EXISTS) or refused; types and domains conflict with a table's row
  type.
- **Temporary tables** include `pg_temp`/`pg_temp_N` qualifications.
- **`unique`** on a column is true for a single-column primary key, UNIQUE
  constraint or plain unique index, false on a complete table otherwise,
  null on a partial table.
- A panic while linking one set becomes a `schema_internal_error`
  diagnostic and drops only that set's records; duplicate records have
  their own code `schema_duplicate_record`.

Revision 3 (re-review, the T018a task record (internal) "Revision 3
(review)"): each finding is closed as a class with a broad conservative
rule, preferring over-conservatism to tracking rare dependencies exactly.

- **Dependency drops.** `DROP ... CASCADE` of a table or of any object
  other than triggers, policies, publications, subscriptions, statistics,
  event triggers, user mappings and roles makes partial every table that
  could depend on it: a column whose written type is not a built-in
  Postgres type (enum, range, domain, base type, row type of any relation),
  a column with a default, generation expression, serial or identity
  sequence or explicit collation, a CHECK or EXCLUDE constraint, an index
  that is not a plain btree over columns, or any identifier naming a
  dropped object. Operators, casts, extensions and other kinds that source
  cannot name keep making every table partial. Without CASCADE, a drop of a
  type, domain, relation (row type), collation, function or sequence that a
  known column, default or index uses is a refusal (the migration may
  fail).
- **Renames.** ALTER TABLE on an index renames the index (tablecmds.c
  17901-17906); ALTER INDEX/TABLE on a written constraint name renames the
  constraint; a generated key or index name makes its table partial;
  ALTER VIEW/SEQUENCE/MATERIALIZED VIEW/TYPE on an index or on a relation
  of another kind is refused; ALTER FUNCTION/PROCEDURE/ROUTINE ... RENAME
  TO adds the new name to the set's functions (the old one stays).
- **Certain refusals.** On a complete table (no reason, no opaque
  statement since its creation) whose name surely means it, a duplicate
  column, a statement or index naming a column the table lacks, a foreign
  key whose referenced columns are not exactly a non-deferrable primary key,
  UNIQUE constraint or plain unique index (any order), a foreign key without
  columns to a table without a primary key, DROP TABLE of an INHERITS
  parent or of a table whose row type a column uses (without CASCADE) are
  refusals. On a partial referenced table the reference stays unresolved,
  never linked. Keys of the whole CREATE TABLE or ALTER TABLE statement are
  added before its foreign keys, as in Postgres.
- **Column types** are read with the Typename grammar (gram.y
  14294-14760); a word after a complete type that is not a column
  constraint is a parse error. A CREATE TABLE whose header parsed but whose
  body did not still gives a table node with its name only (partial).
- **Lexing settings.** Postgres parses each migration's whole query string
  before running it, so `SET standard_conforming_strings` (or
  `backslash_quote`) to a value other than the assumed default, through
  SET, `set_config` or ALTER ROLE/DATABASE, makes every later migration one
  that may fail (its tables partial, `applied: null`).
- **Unknown absent facts.** On a partial table, `nullable: true`, column
  `primary_key: false` and table "no primary key" become `null`, like
  `unique`; a complete table without a primary key now says `[]`. Facts
  source proves (`nullable: false`, a known key) stay on partial tables.
- **Dialect from package features.** A `[features]` entry
  `sqlx/<feature>` or `sqlx?/<feature>` (or a renamed key) that can enable
  another database dialect makes the dialect `unknown`; an unreadable
  `[features]` table does too.

Revision 4 (third review, the T018a task record (internal) "Revision
4 (review)"): the same approach, applied to what depends on a key, an
index or a column in Postgres.

- **Key dependencies.** A foreign key depends on the unique index of the
  primary key, UNIQUE constraint or unique index it uses (pg_constraint.c
  312-324). Dropping such a key or index (also after a rename) without
  CASCADE is a refusal; with CASCADE the foreign key is dropped when that
  key was its only possible one, `unresolved` otherwise, and its table is
  partial. DROP INDEX of a constraint's index (written or generated
  `<t>_pkey`/`<t>_<cols>_key` name) is a refusal even with CASCADE
  (dependency.c 770-789).
- **Unwritten constraint names.** DROP CONSTRAINT of a name the set never
  wrote, or one that may not apply, forgets the constraints it may be: on
  a complete table only the unnamed ones whose generated-name label
  (`pkey`, `key`, `fkey`) matches, on a partial table all. Their facts
  become `null` (`primary_key`, `unique_keys`, column `primary_key` and
  `unique`), foreign keys `unresolved`; known foreign keys that may use a
  forgotten key make the drop a possible refusal without CASCADE and are
  `unresolved` with it. NOT NULL stays. This replaces Revision 3's "facts
  source proves stay" for this case. DROP INDEX of an unwritten name keeps
  the keys (DROP INDEX cannot drop a constraint's index) and makes the
  table's unnamed indexes uncertain.
- **Partitions, inheritance, identity, retyping.** DROP or ALTER TYPE of a
  partition key column, circular INHERIT and ADD GENERATED AS IDENTITY on
  a nullable, identity or defaulted column of a complete table are
  refusals. ALTER COLUMN TYPE of a foreign key column to a type not written
  like the other side's makes the reference `unresolved`, both tables
  partial and the migration one that may fail (type compatibility is not
  decided).
- **Tables from elsewhere.** CREATE TABLE ... LIKE / OF / PARTITION OF /
  INHERITS gives a partial table node with the name and no columns (they
  were recorded as other relations before). Partitions go with a dropped
  partitioned table, inheriting tables with DROP TABLE ... CASCADE, typed
  tables with DROP TYPE ... CASCADE.

Revision 5 (re-check of Revision 4, the T018a task record (internal)
"Revision 5 (review)"):

- **Missing constraint names.** Postgres refuses DROP CONSTRAINT of a
  missing name without IF EXISTS (tablecmds.c 12644-12651). The pass now
  computes the generated name of each unnamed primary key, UNIQUE, foreign
  key, CHECK and EXCLUDE constraint when it is created (makeObjectName,
  ChooseRelationName, ChooseConstraintName; a CHECK takes its column only
  when it uses exactly one), and marks it exact only when no truncation,
  collision with a known name, or earlier statement that may have created
  unknown names can have changed it. On a complete table, an unwritten
  name without IF EXISTS that is not such an exact name makes the
  migration one that may fail; an exact one keeps the Revision 4
  handling, and unnamed keys with a different exact name are kept.
  Partial tables and IF EXISTS are unchanged. DROP INDEX of an unknown
  name stays an unresolved diagnostic, since indexes of unmodeled objects
  make "missing" uncertain.

Revision 6 (re-check of Revision 5, the T018a task record (internal)
"Revision 6 (review)"):

- **Exact generated names narrowed.** Only `<t>_pkey` of an unnamed
  primary key, `<t>_<cols>_key` of an unnamed UNIQUE constraint and
  `<t>_<cols>_fkey` of an unnamed foreign key, over plain columns, can be
  exact, in the Revision 5 conditions. The generated names of unnamed
  CHECK and EXCLUDE constraints are never exact: Postgres takes them from
  the columns its parsed expression or index uses (heap.c
  AddRelationNewConstraints, indexcmds.c ChooseIndexColumnNames), and
  keywords (`extract(year from d)`, `CASE ... END`), SQL value functions
  (`current_date`) and EXCLUDE INCLUDE columns cannot be told from column
  references in the text. Dropping such a name that the source never
  wrote, on a complete table, may fail. An ALTER INDEX or ALTER TABLE
  ... RENAME of a relation whose written name equals a generated key
  name, under any qualification, makes every generated name with that
  base uncertain (a qualified `public.t_pkey` was not matched before).
- **Partial tables** (follow-up, overrides the Revision 5 choice): on a
  partial table, too, DROP CONSTRAINT without IF EXISTS of a name that is
  neither a constraint the set wrote and the table still holds nor an
  exact generated name it still holds may fail, since its existence is
  unknown. Every statement that may create or rename names the pass does
  not see (opaque bodies, unsupported or unreadable statements and ALTER
  actions such as RENAME CONSTRAINT, constraint triggers, statements on
  unknown tables) now also clears the exactness of all generated names
  chosen before it. IF EXISTS stays non-failing. When the target table
  itself is uncertain (the written name may mean another table, for
  example after a search_path change), DROP CONSTRAINT without IF EXISTS
  may fail whatever the name.

Revision 7 (re-check of Revision 6, the T018a task record (internal)
"Revision 7 (review)"; supersedes the Revision 5 and 6 exact-name rules):

- **Constraint names are not tracked.** Six reviews showed that
  exact generated-name tracking does not converge (merged duplicates,
  renames through ALTER INDEX, RENAME CONSTRAINT and function bodies,
  collision suffixes). Any ALTER TABLE ... DROP CONSTRAINT, written or
  generated name, makes every key fact of each table the name may mean
  unknown: the table is partial, `primary_key`, `unique_keys` and the
  columns' `primary_key` and `unique` are `null`, its unique indexes no
  longer prove uniqueness (an unmodeled ADD CONSTRAINT ... USING INDEX may
  have made one a constraint's index), its own references are
  `unresolved`, and every foreign key that may reference it is
  `unresolved` with its table partial. NOT NULL and CHECK constraints
  stay. Without IF EXISTS the migration may fail (WF8), whatever the
  table, known or not. With IF EXISTS it is not a possible failure on that
  account; a foreign key that may use one of the table's keys still makes
  the drop a possible refusal without CASCADE (Revision 4 WF1). A later
  ADD PRIMARY KEY on the table may be refused. The generated-name
  machinery (exact names, collision and truncation checks, rename
  tracking) is removed. What remains of generated names serves DROP INDEX
  and ALTER INDEX/TABLE on index names: the `<t>_pkey`/`<t>_<cols>_key`
  index names of unnamed keys (DROP INDEX of a constraint's index is
  always refused, WF2) and the `_idx`/`_seq` names that unnamed indexes
  and sequences may have (P5); these never claim an exact name.
- **Merged key constraints (F1).** Postgres merges equal index
  constraints within one transformation (transformIndexConstraints,
  parse_utilcmd.c 2084-2140): the primary key is kept first; a later
  UNIQUE equal to a kept key in ordered columns, INCLUDE columns, NULLS
  NOT DISTINCT, DEFERRABLE and INITIALLY DEFERRED is dropped, and its
  written name goes to an unnamed survivor. The pass does the same within
  one CREATE TABLE (in source order) and within one ALTER TABLE
  subcommand (ADD COLUMN with inline keys). Separate ALTER TABLE
  subcommands are not merged: Postgres 17 transforms each alone
  (tablecmds.c ATParseTransformCmd 5567-5592), so `ADD UNIQUE (a), ADD
  PRIMARY KEY (a)` keeps both. WITH and USING INDEX TABLESPACE are not
  compared. When an INCLUDE list cannot be read, both keys are kept, the
  table is partial and `unique_keys` is `null`.

Revision 8 (re-check of Revision 7, the T018a task record (internal)
"Revision 8 (review)"):

- **Principle: a statement whose success depends on a fact the pass does
  not know makes the migration one that may fail (WF8).** `applied: true`
  claims that Postgres accepted every statement; the pass makes that claim
  only when each statement's success follows from what source proves.
  Forms that succeed whatever the fact (IF EXISTS, IF NOT EXISTS, CASCADE
  where it removes the dependency) are not possible failures on that
  account. A certain refusal still needs a complete table; a possible
  failure needs only an unknown.
- **Foreign keys (W1).** A reference whose referenced table is partial
  (any reason: forgotten keys, uncertain unique indexes, LIKE, OF,
  PARTITION OF or INHERITS, opaque statements since its creation) and
  that no proven key matches is `unresolved` and a possible failure; on a
  complete table it stays a refusal; a proven key links as before. A
  referenced name that may mean another table (search_path, another
  qualification, a temporary table) is a possible failure too.
- **Known facts.** A table's facts are known when no opaque statement ran
  since its creation and its only partial reasons are name moves (ALTER
  SCHEMA RENAME, SET SCHEMA); the rules below use this, so a moved table
  keeps its definite outcomes.
- **Audited handlers** (each a possible failure when its fact is
  unknown): a column a statement names on a table whose facts are not
  known or whose name is uncertain; ADD COLUMN and RENAME COLUMN TO a name
  that may exist; ADD PRIMARY KEY on a table whose key may exist; ALTER
  COLUMN TYPE, DROP NOT NULL, SET/DROP DEFAULT and ADD GENERATED AS
  IDENTITY on such a table; DROP COLUMN and DROP TABLE without CASCADE on
  such a table or under an unresolved foreign key that may reference it;
  ALTER TABLE and CREATE INDEX on an uncertain name (except ALTER TABLE
  with only ADD COLUMN IF NOT EXISTS without constraints and DROP
  CONSTRAINT IF EXISTS, under ALTER TABLE IF EXISTS after a search_path
  change); DROP TABLE after a search_path change without IF EXISTS or
  under another qualification without CASCADE; CREATE TABLE/INDEX/VIEW,
  RENAME TO, ALTER INDEX ... RENAME TO and SET SCHEMA to a name that may
  be taken; DROP INDEX and ALTER INDEX without IF EXISTS of a name that may
  mean no index (including a never-written name that may be generated,
  since generated names are never exact, Revision 7). Once a possible
  failure is recorded for the set, later unknown facts add a partial
  reason only.
- **Follow-up decisions** (same principle): (1) a new relation, index or
  constraint name (CREATE TABLE/INDEX/VIEW/SEQUENCE, ADD CONSTRAINT name,
  RENAME TO, SET SCHEMA) of the shape of a generated name of a known
  unnamed object (`<t>_pkey`, `<t>_<...>_pkey|key|idx|excl|seq` for
  relations in a schema the table may share, and also `fkey|check` for a
  constraint of the same table; collision digits allowed; a name of 63
  bytes may be a truncated one) may fail, unless IF NOT EXISTS makes it
  harmless; a table whose facts are not known may hold any unnamed
  object. (2) A new foreign key whose column types are not identical as
  written (case-folded, spaces normalized, serial aliases as their integer
  type) is `unresolved` and may fail, as for ALTER COLUMN TYPE. (3) CREATE
  VIEW/MATERIALIZED VIEW/SEQUENCE/TYPE/DOMAIN of a name already recorded
  in the same pg_class namespace (relations, indexes) or pg_type
  namespace (types, domains and the row types of tables, views,
  materialized views and composite types; not sequences or indexes,
  heap.c 1316-1324) is a refusal when the record is certain and may fail
  when it is not; IF NOT EXISTS skips only over a pg_class relation (over
  a type it still fails, heap.c 1164-1182); CREATE OR REPLACE VIEW over
  another relation is refused (view.c 110-116) and over a view may fail
  (the old column list must be a prefix of the new one, which the pass
  does not know).
- **Left outside the principle**, because the pass does not model the
  fact at all and applying it would mark nearly every set: tables,
  indexes and other relations the set never creates (also as foreign-key
  targets; "Tables outside the set"), names an opaque statement or
  another tool may have created (axum creates its tables after `create
  extension`), row data (validation of new keys, CHECK, NOT NULL, casts
  in ALTER TYPE), compatibility of types that differ as written (only
  identical ones are proven), schema existence and unmodeled ALTER TABLE
  actions.
- **Type aliases** (Revision 9 C1): the foreign-key type checks (new
  foreign keys and ALTER COLUMN TYPE) compare types as Postgres resolves
  them, not as written. The SQL-standard keyword spellings of gram.y
  REL_17_6 (Numeric, Character, Bit, ConstDatetime) become the pg_catalog
  type they stand for: `int`/`integer` are `int4`, `smallint` `int2`,
  `bigint` `int8`, `real` and `float(1..24)` `float4`, `double precision`,
  `float` and `float(25..53)` `float8`, `boolean` `bool`,
  `character varying(n)`/`char varying(n)`/`varchar(n)` `varchar(n)`,
  `character(n)`/`char(n)`/`nchar(n)` `bpchar(n)` (bare `char` is
  `bpchar(1)`), `decimal`/`dec`/`numeric` `numeric`, `timestamp [without
  time zone]` `timestamp`, `timestamp with time zone` `timestamptz`, and
  `time` likewise (`time`, `timetz`); serial aliases are their integer
  type. Any other name, quoted or not, is a GenericType that stands for
  itself, so `"int4"` (quoted) is `int4`, but `"integer"` (quoted) is not
  `int4` and compares unequal. Type modifiers (written without spaces)
  and the number of array dimensions must match; array bounds are
  ignored, as Postgres ignores them. Different types (`int` and `bigint`,
  `varchar` and `text`, `varchar(36)` and `varchar(40)`) still compare
  unequal, and a type text that cannot be read compares unequal to every
  type.

## Consequences

- axum gets 4 migrations, 5 tables, 31 columns, 2 indexes and 7 foreign-key
  pairs; axum-ddd 5, 7, 32, 10 and 9. All tables are complete; the serial
  columns carry `type_resolved` and `default_implied: "sequence"`.
  Revision 2 leaves these counts and states unchanged; `unique` becomes
  true on the single-column primary keys (axum 3, axum-ddd 4) and on
  axum-ddd's unique-index columns `users.username` and `users.email`.
- Revision 2 is more conservative on sets with unreadable statements: one
  parse error makes every later migration's effects partial.
- Revision 3 leaves the axum and axum-ddd counts, states and `applied`
  values unchanged (their foreign keys reference primary keys; no CASCADE
  drop, lexing setting or package feature). The golden changes only in
  N2's absent facts and one reason text.
- Revision 4 leaves axum and axum-ddd unchanged (no drops of keys or
  indexes, no partitioned, inherited or typed tables). The golden changes
  in one fact: `projects.unique_keys` becomes `null` after `DROP
  CONSTRAINT projects_owner_id_slug_key`, whose unwritten name is the
  generated name of that UNIQUE constraint (it was listed as kept before).
- Revision 5 changes no golden fact: the fixture's
  `projects_owner_id_slug_key` is an exact generated name. Sets that drop
  a constraint by a name the pass cannot compute exactly on a complete
  table now get `applied: null` from that migration on.
- Revision 6 changes no golden fact (`projects_owner_id_slug_key` is a
  plain-column UNIQUE name). A set that drops an unnamed CHECK or EXCLUDE
  constraint by its generated name now gets `applied: null` from that
  migration on, even when Postgres would accept the drop; so does a set
  that drops an unwritten name on a partial table (the fixture's two
  drops are a written name and an exact generated name).
- Revision 7 leaves axum and axum-ddd unchanged (no DROP CONSTRAINT in
  any corpus migration). The fixture's DROP CONSTRAINT statements move
  from `pg-app` to a new `pg-drops` package, so `pg-app`'s later
  migrations keep their facts (`members.email` is unique again,
  `projects` is complete with its UNIQUE key; `5_generated_name.sql` now
  creates an unnamed index). A set that drops any constraint without IF
  EXISTS gets `applied: null` from that migration on, even when Postgres
  would accept the drop, and loses the key facts of that table.
- Revision 8 leaves axum and axum-ddd unchanged (all tables complete, all
  foreign keys to proven primary keys, no search_path change). In the
  fixture, `pg-search-path/2_switch.sql` and `pg-rev2-shadow/2_path.sql`
  become `applied: null` (unqualified names after a search_path change),
  so `app.qualified` is partial; `pg-rev2-columns` writes `DROP INDEX IF
  EXISTS public.ledger_memo_idx` so that its WF5 case keeps the migration
  applied. Sets that change search_path, use LIKE/INHERITS tables as
  foreign-key targets, or alter partial tables get `applied: null` from
  that migration on more often, sometimes where Postgres would accept the
  statement (the uncertain name and the partial reason are not weighed
  against each candidate).
- No cached payload, no extractor-version change, no contract change; warm
  replays parse nothing.
- Discovery adds one directory listing per set (none on corpora without a
  sqlx package).

## Known gaps

- **Hash gap**: a column's content hash is its definition span; a later
  ALTER changes its attributes and adds an `alters` edge but not its hash,
  so rationale bound to identity + hash is not marked stale by it.
- **Convention-only discovery**: non-default `migrate!("<dir>")`,
  `Migrator::new(path)` and `sqlx.toml` settings are not read (0 uses in
  the corpora); a `sqlx.toml` only gives a diagnostic and partial tables.
- **Declared-feature dialect** ignores Cargo feature unification through
  other dependencies; package `[features]` that forward to sqlx are read
  (Revision 3 G7), features of other crates that enable sqlx's are not.
- **Function bodies, triggers and extensions** may run DDL; they are counted
  as opaque, not executed. A `SELECT` of a function that an extension (not
  the set) defines is no-effect, although some extension functions change
  tables (for example TimescaleDB `create_hypertable`).
- **DROP CONSTRAINT is never exact** (Revision 7). Every DROP CONSTRAINT
  forgets all keys of each table it may name, and without IF EXISTS makes
  the migration one that may fail, even when Postgres would accept the
  drop (a written name the table surely holds). Sets that drop and re-add
  constraints lose their key facts and `applied` from that migration on.
  A unique index of such a table no longer proves uniqueness either,
  though only an unmodeled ADD CONSTRAINT ... USING INDEX can tie it to a
  constraint. DROP INDEX of a generated `<t>_pkey`/`<t>_<cols>_key` name
  is a refusal even when a collision suffix means Postgres named the
  key's index otherwise. Merging of key constraints (F1) ignores EXCLUDE
  constraints, which are not keys here and only merge with each other.
- **Which key a foreign key uses** (Revision 4): Postgres picks one index
  when several keys cover the referenced columns; Graphvein does not, so
  dropping any of them without CASCADE is a refusal, and with CASCADE the
  reference becomes `unresolved` unless the dropped key was the only one.
  Unresolved references whose written table may be the table are counted
  as possible users.
- **Type compatibility** (Revision 4 G-c; Revision 8 for new foreign
  keys; Revision 9 for aliases): ALTER COLUMN TYPE of a foreign key
  column, and a new foreign key, is a possible failure unless both sides
  are the same type after alias normalization (`int` and `int4`, `serial`
  and `integer`, `timestamptz` and `timestamp with time zone`); different
  but compatible types (`int` and `bigint`) are not recognized, so the set
  is more conservative than Postgres. `pg_catalog.int4` is not equated
  with `int4`, and a user type named like a built-in one in a schema
  before `pg_catalog` on the search_path is not seen. Indexes,
  CHECK constraints and defaults that an ALTER TYPE rebuilds are not
  checked.
- **Tables from elsewhere** (Revision 4 G-d): LIKE, OF, PARTITION OF and
  INHERITS tables have no columns; inherited columns of an INHERITS child
  are not copied, and the child's own written columns are not listed.
  `CREATE TABLE AS` and `SELECT INTO` still record another relation, not a
  table node. DETACH PARTITION is unmodeled (the parent is partial, so a
  later DROP TABLE makes its partitions partial instead of dropping them).
  ALTER TABLE ... NO INHERIT is unmodeled, so a circular-INHERIT refusal
  can be reported for a link it removed.
- **Whole-file failure** (Revision 2): a migration that may fail keeps the
  readable statements' effects, now partial, instead of dropping them; a
  real failure therefore still lists that migration's tables and columns
  (partial, with the failure as reason) and later migrations' effects
  (partial). Tables a failing migration changed before the failure keep
  the changed form (columns it dropped are not restored, only dropped
  tables are).
- **Over-conservative parse errors**: valid statements the recognizer does
  not know (`CREATE FOREIGN TABLE`, `ALTER TRIGGER`, `CREATE RULE`,
  `EXPLAIN`, ...) are parse errors, so the rest of the set becomes partial.
- **Valid forms the parser cannot read** (Revision 3 G4): a CREATE TABLE
  element the recognizer does not know leaves a table node with the name
  only (partial, no columns), and the migration may fail. The re-review's
  example `PRIMARY KEY (id, valid WITHOUT OVERLAPS)` is not in REL_17_6's
  gram.y (Postgres 18), so Postgres 17 refuses it; the rule is the same
  for forms Postgres 17 accepts.
- **Tables outside the set**: ALTER/DROP/CREATE INDEX on a table the set
  does not know, and a foreign key to it, is unresolved, not a possible
  failure, because the table may exist outside the migrations (Revision 8
  keeps this; DROP CONSTRAINT without IF EXISTS is the exception,
  Revision 7).
- **Unknown facts outside the model** (Revision 8): a statement can still
  get `applied: true` when Postgres refuses it for a reason the pass does
  not model: existing rows (ADD PRIMARY KEY/UNIQUE/CHECK/FOREIGN KEY, SET
  NOT NULL, ALTER TYPE), a missing cast, a missing schema, a name an
  extension or DO body created, or an unmodeled ALTER TABLE action.
  Over-conservative the other way: a partial reason that does not touch
  the fact in question (for example "a type of this name may exist")
  still counts as unknown; foreign keys between different compatible
  types (`int` and `bigint`; aliases such as `int` and `integer` are
  equated since Revision 9) and names that
  only look generated (`t_b_key` beside an unnamed UNIQUE (a)) may fail;
  CHECK and EXCLUDE names are both checked against the relation
  namespace, since the pass does not tell them apart.
- **Refusals not detected**: drops without CASCADE whose dependents source
  does not name exactly (a domain over a dropped type, an operator or cast in an expression, a sequence named
  only through a non-literal), ALTER SCHEMA RENAME to an existing name, and
  DROP SCHEMA without CASCADE of a schema that holds only unmodeled
  relations are not treated as possible failures.
- **Broad dependency rule**: `DROP ... CASCADE` makes many unaffected
  tables partial (any default, CHECK or non-built-in type); built-in type
  names are matched as words, so a search_path that puts a schema before
  `pg_catalog` (or a `public.text` type) is not seen.
- **Opaque statements and absent facts**: on a complete table with
  `opaque_after > 0`, `nullable: true`, `primary_key: false` and
  `primary_key: []` stay definite, as do all other attributes; an opaque
  statement may have changed any of them, and `opaque_after` is the
  caveat (ADR decision "Partial versus opaque").
- **Generated names** for WF3 conflicts are computed without truncation or
  collision suffixes (`t_pkey`, `t_a_b_key`, `t_a_idx`, `t_a_seq`); a match
  only makes the table partial.
- **Texts after renames**: generation expressions, defaults, CHECK names
  and index texts stay as written after a column rename; DROP COLUMN
  matches them by identifier, so a function named like the column also
  matches. After `ALTER TYPE/DOMAIN ... RENAME` and `ALTER COLLATION ...
  RENAME`, a column's `type` and `collation` keep the written old name
  (the tables are partial); after a function rename, defaults keep the
  old function name.
- **Domains**: a domain's default, NOT NULL and constraints are not folded
  into the column's `default` and `nullable` (ALTER DOMAIN makes the tables
  partial).
- **search_path with pg_temp first** makes an unqualified CREATE TABLE
  temporary; this is covered only by the R2 partial state.
- **unique on partial tables**: `true` is kept when the covering key or
  index is known; an index that a DROP INDEX may have dropped no longer
  proves it (Revision 4), but its db_index node is kept.
- **CREATE SCHEMA** that creates a schema named like the connecting role
  changes what `"$user"` resolves to; it is not treated as a search_path
  change.
- `CONCURRENTLY` makes the table partial (it cannot run inside sqlx's
  transaction); `-- no-transaction` (sqlx 0.8 only) is not read.
- Index element texts and `where` stay as written after column renames.

Gaps found at the Revision 9 gate, the scope of follow-up task T018a2:

- **K1 Forward foreign keys**: a foreign key to a table that the set
  creates in a later migration is `unresolved` as a table outside the set,
  and both migrations get `applied: true`. One of them must fail: either
  the table does not exist when the foreign key runs, or it does and the
  later CREATE TABLE is refused.
- **K2 ALTER TYPE ... ADD VALUE** without IF NOT EXISTS is not modelled:
  adding a value the enum already has, or to a type that does not exist,
  is refused by Postgres but gets `applied: true`.
- **K3 CREATE EXTENSION objects**: the types, functions and tables an
  extension creates are not known (see "Function bodies, triggers and
  extensions"); later statements that use them are judged without them.
- **C2 Widening a referenced key**: ALTER COLUMN TYPE `int` to `bigint` of
  a primary key that a foreign key uses may fail (types differ), although
  Postgres accepts it.
- **C3 DROP CONSTRAINT of a set-written name** forgets all keys of the
  table (Revision 7 decision; see "DROP CONSTRAINT is never exact"). Rails
  and Django migrations drop named constraints routinely, so such sets
  lose key facts.
- **C4 `-- no-transaction`** (sqlx 0.8) is not read; CONCURRENTLY therefore
  makes the migration one that may fail even where sqlx runs it outside a
  transaction.
- **C5 User index names shaped like generated names** on partial tables
  (a table whose facts are not known may hold any unnamed object) may
  fail, although the name is usually the user's own.
- K1, K2 and C2-C5: see ADR 0051.

## Review notes (delegated review, 2026-11-04)

- Implemented with Revision 1's R-A first (no precision stop). Nine reviews by fresh reviewers found confident wrong facts in rarer forms;
  Revisions 2-9 closed them as classes: CASCADE dependency drops, renames,
  certain refusals, FK dependency on keys, DROP CONSTRAINT (any DROP CONSTRAINT
  forgets the table's keys; without IF EXISTS the migration may fail), Postgres
  index-constraint merging inside one statement, the principle "a statement
  whose success depends on an unknown fact makes the migration may-fail", and
  type-alias normalisation for FK type comparison. Decision WF8: a
  lexing or parse failure makes that file and every later migration's effects
  partial (`applied` null).
- Convergence rule for the last round: stop only on a confident wrong fact in a
  common migration form or a corpus migration; exotic wrong facts are known gaps
  (K1 forward FK to a later-created table, K2 ALTER TYPE ADD VALUE) for T018a2,
  with the coverage gaps C2-C5 listed above.
- Gates: the ten public corpora unchanged except new schema records on axum
  (5 tables, 31 columns, 2 indexes, 7 FK pairs) and axum-ddd (7/32/10/9), all
  complete, all migrations applied; self-index differs only by the fixture's
  records; Langflow timing fresh -1.4% / warm +0.1%, store size identical;
  axum-ddd +0.03-0.09 s on sub-second runs (+119 records; recorded, rule 17),
  store +4.2%. CI postgres: TestSchemaSnapshotParity/postgres and
  TestSchemaPostgresSemantics (B6, the fixture applied to Postgres 17 and the
  catalog compared) pass; only the four helper skips. the private corpus not run.

