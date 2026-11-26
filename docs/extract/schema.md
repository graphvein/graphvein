# Schema from SQL migrations

ADR 0047 (T018a, Revisions 1, 2 and 3) and ADR 0051 (T018a2, claim
contract and Postgres oracle); aggregate extractors-49 (28 from T018b1,
ADR 0048, so that an index proves the schema pass ran; 29 from T018c1; 30 from
T018f, ADR 0055: provenance attributes `claims`, reason `row`, `opaque_sites`; 31 from T018g0, ADR 0058: model records only; 32 from T018g2, ADR 0059: the cached TypeScript model payload; 33 and 34 from T018h, ADR 0060: Python query sites and sqlx query payloads; 35 from T018g3, ADR 0061: TypeScript model payload version 2; 36 from T018g5, ADR 0063: the Go model payload; 37 from T018g6, ADR 0064: TypeScript model payload version 5; 51 from T024c1, ADR 0077: Supabase migration sets, a link-time change; 52 from T024c1b, ADR 0077: bare Supabase directories, platform extensions and the PL/pgSQL SELECT INTO subset, a link-time change; 53 from T024c1b R1: platform extension effects and storage.buckets inserts, a link-time change; 55 from T024c3, ADR 0077: Supabase generated types as a schema source, `docs/extract/schema-supabase-types.md`, a link-time change); contract v1
unchanged (new extensible kinds). `internal/extract/schema` runs at link
time over the sources the scanner retained. It adds no cached payload, so no
language file is reparsed when a migration changes.

## Discovery (sqlx)

- A **set** is the directory `migrations` directly inside a Cargo package
  root (a `Cargo.toml` with a `[package]` table) whose package depends on
  sqlx, when the directory holds at least one sqlx-format file. This is what
  `migrate!()` and `migrate!("./migrations")` resolve to and the sqlx CLI
  default. Other `migrate!` paths, `Migrator::new` and `sqlx.toml` settings
  are not read (a `sqlx.toml` in the package gives
  `schema_unsupported_config` and makes every table of the set partial).
- **sqlx dependency**: the key `sqlx` (without a `package` rename) or any key
  with `package = "sqlx"`, in `[dependencies]`, `[dev-dependencies]` and
  both under `[target.*]`; `workspace = true` inherits the entry of the
  workspace root's `[workspace.dependencies]` (the `[package] workspace`
  path, else the nearest ancestor manifest with `[workspace]`), and member
  features add to it.
- **Dialect** from declared features: `postgres`, `mysql`, `sqlite` or
  `sqlite-unbundled`, `mssql` (0.5); `all`/`all-databases` enable several.
  Exactly one dialect gives it; none, several or an unreadable entry gives
  `unknown`. A package `[features]` entry `<sqlx key>/<feature>` or
  `<sqlx key>?/<feature>` that can enable a dialect other than the declared
  one, or a `[features]` table that cannot be read, also gives `unknown`
  (Revision 3 G7). Only `postgres` sets get schema objects; others get
  migration nodes and `schema_unsupported_dialect`. Feature unification
  across the build graph is not modeled.
- **Files** (sqlx `source.rs` / 0.5.9 `sqlx-macros/src/migrate.rs`): only
  direct entries; the name splits at the first `_` and the rest must end in
  `.sql`, else it is ignored (`README.md`); the prefix must parse as an
  `i64` (sign and leading zeros allowed, `01` equals `1`), otherwise sqlx
  refuses the whole set; `.up.sql` / `.down.sql` set the direction;
  description = rest without the suffix (repeatedly trimmed), `_` as space;
  stable sort by version; down migrations are never applied.
- **Order failures**: an invalid version gives `schema_migration_order`,
  every migration `applied: false`, no schema objects. A duplicate version
  among applied migrations gives the diagnostic on each duplicate, no schema
  objects, `applied: true` with `order` for migrations below the duplicate
  version and `applied: null`, `order: null` (undefined) from it on.
- **Unreadable members** (Revision 1 R5): the set directory is listed; a
  sqlx-format entry that is a regular file (symlinks followed) but is not in
  the scanned sources (symlink, not UTF-8, ignored, unreadable) gives
  `schema_member_unreadable` and makes **every** table of the set partial
  (a non-UTF-8 member makes sqlx refuse the whole set). A set whose
  every member is unreadable has no migration node and no table: only its
  manifest's diagnostics (`schema_member_unreadable`, and
  `schema_migration_order` or `schema_unsupported_dialect` when they
  apply) show it; the read API lists it under `sets_without_records`.
  A set directory whose listing fails for a reason other than absence
  (T018b1 review) also gives `schema_member_unreadable` on the manifest
  ("the directory could not be listed") and makes every table partial; a
  set with no stored member is then a set without records.
- **Unreadable manifests** (T018b1 review): a `Cargo.toml` the TOML
  reader rejects gives `schema_manifest_unreadable` (warning, on that
  file); whether it declares a set is unknown, and it is not read further.
  So does a manifest whose `package`, `workspace`,
  `workspace.dependencies`, `dependencies`, `dev-dependencies`, `target`,
  `target.X` or `target.X.(dev-)dependencies` is not a table (`[[package]]`
  included) or whose `package.workspace` is not a string (T018b1 review:
  Cargo refuses it; it is never read as "no package" or "no sqlx").

## Supabase sets (T024c1)

ADR 0077, task `docs/contract/claims/nextjs-supabase.md` (rows SB-M1 to SB-M8,
`internal/extract/schema/supabase.go`, `TestSupabaseContract` keeps the
rows, `supabaseRows` and the branches 1:1 and checks that the fixture
`testdata/fixtures/supabase-migrations` reaches every row).

- **SB-M1 discovery.** Every directory named `supabase` that holds
  `config.toml` (a scanned source) is a Supabase project; its
  `supabase/migrations` is a set with `tool: supabase`, dialect `postgres`
  and `package` the `config.toml` path. Members follow the Supabase CLI
  (`pkg/migration` `ListLocalMigrations`): the direct entries of the
  directory in lexical (name) order, a first entry named `<14
  digits>_init.sql` older than `20211209000000` skipped (diagnostic
  `schema_migration_order`, info), names matching `^([0-9]+)_(.*)\.sql$`
  applied, every other name ignored. `version_text` is the first group,
  `description` the second as written, `direction` always `simple`. A
  `supabase/migrations` directory without `config.toml` is a set only by
  SB-M5 (below). A
  member the scanner did not keep is unreadable (`schema_member_unreadable`)
  and, like a second member whose version repeats an earlier one (its
  bookkeeping row in `supabase_migrations.schema_migrations` is refused,
  `schema_migration_order`, warning), makes it and every later member
  `applied: null`; every table of the set is partial then.
- **Execution.** The CLI sends a file's statements and its bookkeeping
  `INSERT` as one pgx batch, which PostgreSQL runs in one implicit
  transaction block: the reader's P6 table applies with the implicit-block
  column for every file (sqlx's `-- no-transaction` marker means nothing).
  The statements are read by the same PostgreSQL 17 reader as sqlx sets,
  under the same rows, preconditions and default branch D; messages name
  the Supabase CLI where they name sqlx. A statement that drops or alters
  `supabase_migrations` (or `schema_migrations`) may fail (Q1.P13).
- **SB-M2 platform objects (assumption SB-V2).** The set's migration
  records carry `platform: {name: "supabase", assumption: "SB-V2",
  objects: [...]}`, the objects of the oracle prelude
  (`scripts/oracle/nextjs_supabase/supabase-prelude.sql`). A foreign key of
  one column to `auth.users`, `storage.objects` (both `uuid`) or
  `storage.buckets` (`text`), written without columns or with `(id)`, from a
  column whose type is that key's, is a reference with `status:
  "external"`, `table`, `columns: ["id"]` and `assumes: ["SB-V2"]`: no
  `schema_unresolved_reference`, no partial table, no edge (the platform
  table is no node). With the search_path unchanged, an unqualified table
  of the set (for example `users`) is in the default schema and never the
  platform table of the same name. Any other reference outside the set is
  as in sqlx sets.
- **SB-M3 row level security.** `ALTER TABLE t ENABLE / DISABLE / FORCE /
  NO FORCE ROW LEVEL SECURITY` and `CREATE / ALTER / DROP POLICY` are
  schema-neutral: the table stays as it was (in sqlx sets the four words
  stay M5k-a, table partial). The policy's `USING` and `WITH CHECK` are
  not checked (N1.P5 does not apply); the name checks (N1.P2) and Y12's
  refusals still do. Tables of a Supabase set carry two attributes:
  `rls: {enabled, forced}`: `false` when the set creates the table, set by
  the four words as written; a flag is `null` when the table's prior state
  is unknown (`CREATE TABLE IF NOT EXISTS` that may have been skipped), the
  word's table name may mean another table, or code may alter the table,
  and (T024 review) in either direction whenever a statement the pass
  did not read may have changed it: an opaque statement after the flag was
  set (O2: it may run any `ALTER TABLE`; NX-F2: an SB-M9 data statement
  does not count), a statement on the table that
  could not be read, or a member of the set that could not be read.
  Statements the pass read change the flags only through the four words,
  so a table's other partial reasons (an unproven CHECK, a column a
  migration that may fail adds) leave them known; the former rule that a
  `false` flag is `null` on any partial table is gone (it kept a `true`
  an opaque `DISABLE` could have cleared). `policies: [{name, command,
  table}]`: the policies the set created, in written order, `command`
  upper case (`ALL` when `FOR` is absent), `table` the table that holds
  the policy under its current name (it follows `ALTER TABLE ... RENAME`,
  as `pg_policies` does; the oracle compares it); `DROP POLICY` removes one,
  `ALTER POLICY ... RENAME TO` renames it, any other `ALTER POLICY` keeps
  it. `policies` is `null` when a policy statement could not be read, its
  table name may mean another held table (`public.t` beside `t`), or code
  may create, rename or drop policies. On a partial table the list holds
  the set's policies and others may exist. Nothing is claimed about roles,
  `USING` or `WITH CHECK`. A policy on a table the set does not hold
  (`storage.objects`) is as in sqlx sets.
- **SB-M5 bare directories (T024c1b, assumption SB-V3).** A directory
  `supabase/migrations` without `config.toml` beside it, holding at least
  one entry, every direct entry named `^([0-9]+)_(.*)\.sql$`, and not a
  sqlx set's directory, is read as a Supabase set by SB-M1's member rule,
  with `package` the migrations directory. Any other entry (a README, a
  down file, a subdirectory) or a directory that cannot be listed leaves it
  no set. The set's `platform` attribute lists the assumption under
  `assumptions` (`{id: "SB-V3", text: "read as Supabase CLI migrations; no
  supabase/config.toml found"}`), which the set strip shows.
- **SB-M4 platform extensions (T024c1b, assumption SB-V4).** In a Supabase
  set, `CREATE EXTENSION [IF NOT EXISTS] x`, at top level or run by code,
  of `http` or `vector` is not a statement only a superuser may run
  (Supabase's supautils lets its `postgres` role create them), so E6's
  O2.P4 may-fail does not apply; the statement stays opaque (O2) and the
  other O2.P4 and O2.P7 checks apply. Each such extension the set creates
  adds `{id: "SB-V4", text: "Supabase platform: extension x is available
  to the migration role"}` to the `platform` attribute's `assumptions`.
  The list is `mapped`: Supabase's allow-list is not published on a host
  the pass's sources can be read from, so it holds the extensions the
  corpora create, each checked by replaying chatbot-ui on PostgreSQL 17
  with the PGDG builds of pgvector 0.8.7 and pgsql-http 1.7
  (`replay-supabase.sh` with `GV_REAL_EXTENSIONS=1`). Any other untrusted
  extension may fail as in sqlx sets.
- **SB-M6 platform extension effects (T024c1b R1, assumption SB-V4).**
  `CREATE EXTENSION [IF NOT EXISTS] x [WITH] SCHEMA s` at top level, `x`
  `http` or `vector`, without VERSION, FROM or CASCADE, `s` neither
  `public` nor `pg_catalog`, is no opaque statement when the set (and its
  code) creates none of `x`'s object names (`supabaseExtensionObjects`,
  read from the install scripts of pgvector 0.8.7 and pgsql-http 1.7, the
  PGDG builds the corpus was replayed with) and no operator, cast,
  operator class or family, access method or aggregate. The scripts
  create types, domains, functions, aggregates, operators, operator
  classes, casts and access methods in `s` and no relation; they declare
  no implicit cast from a pg_catalog type and no polymorphic argument, so
  no later expression over pg_catalog types resolves to one of their
  functions or operators. The replay oracle checks the lists against the
  catalog (`pg_depend` members of each extension: no relation, every
  named object listed). Any other form stays opaque (O2).
- **SB-M7 platform inserts (T024c1b R1, assumption SB-V5).** `INSERT INTO
  storage.buckets ...` in a Supabase set: its column references are not
  checked (N3.P1 does not apply), the insert succeeds on a fresh project
  and the platform's triggers change no object the set holds. When no
  statement of the set or of its code names `buckets` other than in such
  an insert or in `REFERENCES storage.buckets` (RI triggers on the
  referenced table fire on UPDATE and DELETE only), runs a dynamic
  `EXECUTE`, or creates a body in another language or one the pass cannot
  read, no trigger or rule of the set fires on it, so it is no opaque data
  change. Each such statement adds `{id: "SB-V5", text: "inserts into
  Supabase platform tables (storage.buckets) succeed on a fresh project,
  and the platform's triggers change no object of the set: <statement>"}`
  to the `platform` attribute's `assumptions`.
- **SB-M8 platform functions in analysed SQL bodies (T024c1c, assumption
  SB-V2).** With `check_function_bodies` on (E4's default),
  `fmgr_sql_validator` (pg_proc.c REL_17_6) parses a LANGUAGE sql
  function's AS string and parse-analyses and rewrites each statement
  against the catalog at creation (`pg_analyze_and_rewrite_withcb`,
  `sql_fn_parser_setup` for the parameters, `check_sql_fn_statements`,
  `check_sql_fn_retval`), unless an argument is polymorphic; the body is
  not executed. `R1b.P5` proves one form of such a body (`sqlbody.go`,
  T024c1c clause of the precondition): one SELECT of one output expression
  of the RETURNS type over at most one table held with known columns
  under a certain name, an optional WHERE, EXISTS of the same form,
  parameters by name (a column of the level's table wins, as in
  `sql_fn_post_column_ref`; an outer level's column is not proven), one
  integer subscript of a parenthesised array and pg_catalog functions the
  set does not overload. In a Supabase set, `storage.foldername(text)`
  (`text[]`) and `storage.filename(text)` (`text`) may appear in that
  form when the set creates no function of the name: their one signature
  is the Supabase storage API's (tenant migrations 0002, 0036, 0060, 0061
  of `supabase/storage` at 132a66ae, Apache-2.0; no overload). Every
  other body keeps R1b.P5's may-fail. A BEGIN ATOMIC body is not covered.
  `TestSupabaseC1cCasesPostgres` checks every case live.
- **SB-M9 row level security across data statements (NX-F2).** In a set
  that defines code, `INSERT`, `UPDATE` and `DELETE` are opaque (O1: a
  trigger they fire may run any statement), which would make every `rls`
  flag unknown (SB-M3, C6). PostgreSQL 17 runs, for a data statement, only
  code the set can name: the row and statement triggers of the target and
  of the tables its foreign key actions reach (`trigger.c`,
  `ri_triggers.c`), their rules, and the defaults, constraints, index and
  policy expressions of the rows it writes; event triggers fire on DDL
  only. The statement's P12 inventory reads a superset of that code (every
  trigger function of the set, every routine a statement names outside its
  own definition, closed over their calls and data changes). When it is
  bounded (every body read; no `EXECUTE`, `SET`, `RESET`, `set_config`,
  transaction control, nested `DO`, other language or routine created by
  code) and holds no statement at all (so no `ALTER TABLE`, no policy
  statement), the data statement does not count against the flags; it
  stays opaque for every other fact. This holds only while the pass has
  read every routine of the set: no earlier opaque statement other than
  such a data statement and no unread statement. Fixture
  `rls-data/.../20240101000000_settings.sql`; `TestSupabaseNXF2Cases` and
  `TestSupabaseNXF2CasesPostgres` (positive and negative cases, flags
  checked against `pg_class.relrowsecurity` live). T024 review: SB-M9
  applies only when the statement's target and every data target of its
  inventory is a table the set holds under a certain name (an unqualified
  `pg_` name means the `pg_catalog` relation; a qualified name must name a
  held table of its schema, or in `public` one the set created
  unqualified, and no other relation the pass records, such as a view).
  A superuser's `UPDATE pg_catalog.pg_class SET relrowsecurity = false` or `DELETE FROM
  pg_catalog.pg_policy` changes row level security with no statement
  SB-M3 reads, and a view or platform table may carry rules or triggers
  the set does not show: a data statement on any other relation (top
  level, in a set with or without code, or in the bounded inventory of an
  O1 statement; SB-M7's `storage.buckets` insert excepted) and an O1
  statement with an unbounded inventory make the `rls` flags and the
  `policies` of every held table unknown, and no later data statement is
  SB-M9's. The statement keeps its may-fail (the platform's `postgres`
  role is no superuser, so the catalog writes are refused there). Fixture
  `rls-catalog/.../20240101000000_notes.sql`; `TestSupabaseB5Cases` and
  `TestSupabaseB5CasesPostgres` (13 cases; the 10 that do not name
  `public` checked live as a superuser, flags and policy counts),
  `TestSupabaseB5FixturePostgres` (the fixture refused with 42501 for a role without superuser rights, and as a
  superuser leaving row level security off and no policy).
  Stacked gate NX-S: SB-M9 does not read rule actions, and PostgreSQL
  runs a rule's actions with every `INSERT`, `UPDATE` or `DELETE` on its
  table (`rewriteHandler.c`); an action may write `pg_catalog.pg_class`
  or `pg_policy`. A top-level `CREATE [OR REPLACE] RULE` in a Supabase set
  therefore makes the `rls` flags and the `policies` of every held table
  unknown, and so does every data statement after it (a table created
  later may be what an action writes); when code of the set (a `DO` block
  or routine body) creates a rule, every data statement of the set does.
  No later data statement is SB-M9's. Fixture
  `rls-rule/.../20240101000000_notes.sql`; `TestSupabaseB5Cases` and
  `TestSupabaseB5CasesPostgres` (rule cases, checked live as a superuser),
  `TestSupabaseRuleFixture` and `TestSupabaseRuleFixturePostgres`.
  TOP-1 of 2026-11-18: code the pass cannot read may create a rule, a
  trigger or an event trigger (whose function runs with every later DDL
  statement, `ALTER TABLE ... ROW LEVEL SECURITY` and `CREATE POLICY`
  among them), and none of them is visible in the set. So a statement that
  runs such code (an O1 statement whose P12 inventory is unbounded: a
  dynamic `EXECUTE`, a body in another language or that cannot be lexed,
  a routine created by code), a top-level `CREATE EVENT TRIGGER`, code of
  the set that creates an event trigger (from the set's start) and a file
  whose rest cannot be lexed end SB-M9 for the set: the `rls` flags and
  the `policies` of every table, held then or created later, are unknown
  to the end of the set. Fixture `rls-dynamic/.../20240101000000_notes.sql`;
  `TestSupabaseB5Cases` and `TestSupabaseB5CasesPostgres` (dynamic rule,
  trigger and event trigger cases and a top-level event trigger, checked
  live as a superuser), `TestSupabaseDynamicFixture` and
  `TestSupabaseDynamicFixturePostgres`.
- **SB-M10 platform extension types as column types (NX-F2, assumptions
  SB-V2, SB-V4).** After SB-M6's `CREATE EXTENSION x SCHEMA s`, the types
  of `x`'s install script (`vector`, `halfvec`, `sparsevec`; `http_method`,
  `content_type`, `http_header`, `http_response`, `http_request`; their
  arrays) are column types in `s` (`CREATE TABLE`, `ADD COLUMN`; M2.P1
  proven). A qualified name resolves only in `s`; an unqualified one by
  the search_path as `namespace.c` does, pg_catalog first, when the path
  is known and reaches `s` (E4's value on the platform is `"$user",
  public, extensions`, SB-V2) and the set and its code create no type or
  relation of the name. No opaque statement may have run since the
  extension, and no `ALTER`/`DROP TYPE`, `DOMAIN` or `SCHEMA` may name it.
  Modifiers follow each `typmod_in`, checked live on PostgreSQL 17.12 with
  pgvector 0.8.7 and pgsql-http 1.7: `vector` and `halfvec` take one
  integer 1 to 16000, `sparsevec` 1 to 1000000000 (otherwise refused,
  22023); the http types take none (refused, 42601); a non-integer
  modifier or one on `_vector` is not proven. Function parameter and
  result types, index access methods (`hnsw`, `ivfflat`) and operator
  classes of the extension are not covered.
- **Oracle.** `TestSupabaseOracle` (with `GRAPHVEIN_TEST_POSTGRES_URL`)
  replays each set on PostgreSQL 17 on top of the prelude and the
  platform's `search_path` (`"$user", public, extensions`) with the T018a2
  harness, and compares the harness's facts plus the table names,
  `pg_class.relrowsecurity` / `relforcerowsecurity`, `pg_policy` names and
  commands, and the catalog foreign keys behind external references. It
  runs on the fixture and on the checkouts in `GRAPHVEIN_SUPABASE_CORPORA`
  (shims for pgvector and http as the investigation's replay script). Precision
  label: `tool: supabase` matches no rule-table row, so its records are
  `best_effort` (row D) until a row PL-F10 joins.

## Statements (PostgreSQL 17)

Each applied migration is lexed per `scan.l` (line and nested block
comments, `'...'` with `''`, `E'...'`, `B/X/N/U&` strings, `$tag$` quotes,
`"..."` and `U&"..."` identifiers; `$` continues an identifier and `$1` is a
parameter) and split at top-level `;`, except inside `BEGIN ATOMIC ... END`
bodies. An escape the scanner refuses is a lexing failure (Revision 1.6
D2): in `E'...'` a `\u` or `\U` without four or eight hexadecimal digits of
a valid code point, and an octal or hexadecimal escape whose byte is 0 or
not ASCII (multi-byte sequences built from escapes are not verified); in
`U&'...'` and `U&"..."` a backslash not followed by `\`, four or `+` six
hexadecimal digits of a valid code point, or a UESCAPE clause.
Identifiers fold per `scansup.c`: ASCII lowercased unless quoted,
truncated to 63 bytes at a character boundary. Statement spans run from the
first token to just after `;`.

| class | statements |
|---|---|
| modeled | `CREATE [UNLOGGED] TABLE [IF NOT EXISTS]`, `CREATE [UNIQUE] INDEX [CONCURRENTLY] [IF NOT EXISTS] [name] ON [ONLY]`, `ALTER TABLE [IF EXISTS] [ONLY]` with the actions below, `DROP TABLE`, `DROP INDEX`, `ALTER INDEX [IF EXISTS] n RENAME TO m`, `ALTER SCHEMA s RENAME TO t` |
| no effect | `CREATE FUNCTION/PROCEDURE/TRIGGER/COLLATION/SEQUENCE/POLICY/ROLE/...`, `ALTER SEQUENCE/FUNCTION/PROCEDURE/ROUTINE/POLICY/GROUP/DEFAULT PRIVILEGES` (`ALTER FUNCTION/PROCEDURE/ROUTINE ... RENAME TO g` adds `g` to the set's functions, Revision 3), `ALTER COLLATION`, `ALTER ROLE/USER/DATABASE` other than the R2 forms, `ALTER SCHEMA ... OWNER TO`, `COMMENT`, `GRANT`, `REVOKE`, `REASSIGN OWNED`, `LOCK`, `NOTIFY`/`LISTEN`/`UNLISTEN`, `CHECKPOINT`, `SECURITY LABEL`, `BEGIN`/`COMMIT`, other `SET`/`RESET`/`DISCARD` (including `SET standard_conforming_strings` to `on` and `SET backslash_quote` to `on` or `safe_encoding`), DML (`INSERT/UPDATE/DELETE/COPY/TRUNCATE/MERGE`), `SELECT`/`VALUES`/`WITH`, `ANALYZE`, `CLUSTER`, `REINDEX` and `REFRESH` when the rules below do not make them opaque, `DROP` of other objects without `CASCADE` (except `DROP EXTENSION` and `DROP OWNED`) |
| opaque | `DO`, `CALL`, `EXECUTE`; `ROLLBACK`/`SAVEPOINT`/`RELEASE`; `CREATE EXTENSION`, `ALTER EXTENSION`, `DROP EXTENSION`; a call of a function or procedure the set defines; DML in a set that defines any function or trigger, and `ANALYZE`/`CLUSTER`/`REINDEX`/`REFRESH` there (they run index expressions or view queries); the R2 statements: `SET search_path`, `SET SCHEMA`, `SET ROLE`, `SET SESSION AUTHORIZATION`, `SET session_authorization`, `RESET search_path/ALL/ROLE/SESSION AUTHORIZATION/session_authorization`, `DISCARD ALL`, `set_config('search_path', ...)` or a `set_config` with a non-literal name, `ALTER ROLE/USER/DATABASE ... SET/RESET search_path/role/session_authorization` or `RESET ALL`, `ALTER ROLE/USER ... RENAME`; `CREATE EVENT TRIGGER`; `DROP OWNED`; `DROP ... CASCADE` of other objects; the lexing settings (Revision 3 G8): `SET standard_conforming_strings` or `SET backslash_quote` to another value, `set_config` of either, `ALTER ROLE/USER/DATABASE ... SET` of either (kind `lexing_setting`) |
| unsupported | `CREATE/ALTER/DROP TYPE` and `DOMAIN`, views, `CREATE SCHEMA`, `DROP SCHEMA` (applied to the tables it can name), `ALTER INDEX` other than RENAME, `CREATE TABLE AS`, `CREATE TABLE OF/PARTITION OF/LIKE/INHERITS` (a partial table node with the name and no columns, Revision 4 G-d), `SELECT INTO` (every `INTO` at any paren depth of a SELECT, VALUES, WITH, TABLE or parenthesised statement that is not `INSERT INTO`, `MERGE INTO` or a column label `AS into`/`x.into`; the leftmost SELECT's INTO, also inside parentheses or after WITH, creates the table; any other INTO may be refused by Postgres, so the migration may fail), temporary tables (`TEMP`/`TEMPORARY` or a `pg_temp`/`pg_temp_N` qualification, namespace.c 673-678: no node; they shadow later unqualified names); statements Postgres refuses inside the migration's transaction: `VACUUM`, `REINDEX ... CONCURRENTLY`, `DROP DATABASE`, `DROP TABLESPACE`, `CREATE TEMP TABLE s.t` with a schema that is not temporary (these make the migration one that may fail, below) |
| error | anything else (including `DROP` of an unknown object kind), a recognized statement that does not parse, a lexing failure (the rest of the file is one region) |

Revision 2 principle: a statement form that is not modeled is opaque,
unsupported or an error; it is no-effect only when it cannot drop, rename or
alter a table, column, index, default or nullability. `DROP OWNED` (with or
without `CASCADE`, gram.y 6784-6793) drops every object the roles own and
makes every table partial; `DROP EXTENSION` drops the extension's member
objects.

Dependency drops (Revision 3 class 1, replacing Revision 2's G1 rule).
Columns depend on their types, including domains and the row types of
tables, views, materialized views, foreign tables and composite types (so
views and foreign tables, and the servers and foreign data wrappers that
hold foreign tables, are not harmless kinds); defaults, generation
expressions, CHECK and EXCLUDE constraints and indexes depend on the
functions, operators, types, sequences and collations they use.

- `DROP <kind> n ... CASCADE` of a table (`DROP TABLE`, `DROP SCHEMA`) or of
  a type, domain, view, materialized view, foreign table, sequence,
  function, procedure, routine, aggregate, collation, server or foreign
  data wrapper makes partial every table that **could** depend on it: a
  column whose written type is not a built-in Postgres type (any word
  outside pg_catalog's type names and the grammar's type keywords, or a
  schema other than `pg_catalog`), a column with a default, generation
  expression, serial or identity sequence or explicit collation, any CHECK
  or EXCLUDE constraint, an index with an expression, predicate, `USING`,
  operator class, collation or ordering, a foreign key written to a
  dropped name, or any identifier naming a dropped object. Unsure is
  partial.
- `DROP EXTENSION ... CASCADE` and `CASCADE` of kinds whose uses source
  cannot name (operators, operator classes and families, casts, access
  methods, text search objects, languages, conversions, transforms, or
  names that cannot be read) make every table partial. Triggers,
  policies, publications, subscriptions, statistics, event triggers, user
  mappings and roles change no table.
- Without `CASCADE`, Postgres refuses to drop an object something depends
  on. When a known column's written type names the dropped type, domain or
  relation (a built-in name such as `date` written unqualified means
  pg_catalog's type), a column has the dropped collation, a default,
  generation expression, index or named CHECK names the dropped function,
  or a default names the dropped sequence in a literal (`'s'`) or it is
  the generated `<table>_<column>_seq` of a serial or identity column, the
  drop is refused and the migration may fail. Other drops without
  `CASCADE` are no-effect (Postgres refuses them if a dependent exists).
- `ALTER TYPE/DOMAIN` other than `ADD VALUE`, `RENAME VALUE` and `OWNER
  TO`, and `ALTER COLLATION ... RENAME/SET SCHEMA`, make the tables that use
  the object partial; their columns keep the written old `type` and
  `collation` text.

Column definitions: name, type (written text, gaps compacted; read with the
Typename grammar, gram.y 14294-14760: `int`/`integer`/`smallint`/`bigint`/
`real`/`boolean`/`json`, `float[(p)]`, `double precision`,
`decimal`/`dec`/`numeric`/`varchar [(...)]`, `bit`/`character`/`char`/
`nchar`/`national character|char [varying] [(n)]`, `time`/`timestamp
[(p)] [with|without time zone]`, `interval [(p)]` or with its fields, or a
generic `[schema.]name [(modifiers)]`, then `[...]` bounds or `ARRAY
[[n]]`; any other word after a complete type is a parse error, Revision 3
B1), then any of
`CONSTRAINT n`, `NOT NULL`, `NULL`, `DEFAULT expr`, `PRIMARY KEY`, `UNIQUE
[NULLS [NOT] DISTINCT]`, `REFERENCES t [(c)] [MATCH] [ON DELETE] [ON
UPDATE]`, `CHECK (...) [NO INHERIT]`, `COLLATE c`, `GENERATED {ALWAYS|BY
DEFAULT} AS IDENTITY`, `GENERATED ALWAYS AS (expr) STORED`, `[NOT]
DEFERRABLE`, `INITIALLY ...`, `STORAGE`, `COMPRESSION`. Table constraints:
`PRIMARY KEY`, `UNIQUE`, `FOREIGN KEY ... REFERENCES`, `CHECK`, `EXCLUDE`.
`CONSTRAINT`, `PRIMARY`, `UNIQUE`, `FOREIGN` and `CHECK` are reserved
keywords, so a table element starting with them is a constraint; `EXCLUDE`
is unreserved (gram.y 17616), so it starts a constraint only when followed
by `USING` or `(` (gram.y 4211), and is otherwise a column named `exclude`
(Revision 2 WF1, also for `ALTER TABLE ... ADD exclude ...`).
ALTER TABLE actions: `ADD [COLUMN] [IF NOT EXISTS]`, `DROP [COLUMN] [IF
EXISTS] [CASCADE|RESTRICT]`, `ALTER [COLUMN] [SET DATA] TYPE [COLLATE]
[USING]`, `SET/DROP NOT NULL`, `SET/DROP DEFAULT`, `ADD [CONSTRAINT n]
...`, `DROP CONSTRAINT [IF EXISTS] n`, `RENAME [COLUMN] a TO b`, `RENAME TO
n`, `SET SCHEMA s` (the table moves to `s` and is partial); the
`alter_table_cmd` words `OWNER`, `ENABLE`, `DISABLE`, `SET`,
`RESET`, `CLUSTER`, `INHERIT`, `NO`, `OF`, `NOT`, `REPLICA`, `FORCE`,
`ATTACH`, `DETACH`, `VALIDATE`, `ALTER CONSTRAINT` and `RENAME CONSTRAINT`
make the table partial; any other word is a parse error.

## State rules

- **Serial** (R1): a single unquoted (any case) or quoted lowercase type
  name `smallserial`/`serial2`, `serial`/`serial4`, `bigserial`/`serial8`
  gives `type_resolved` `smallint`/`integer`/`bigint`, `nullable: false`,
  `default: null` and `default_implied: "sequence"`; never a sequence name.
  SET/DROP DEFAULT removes `default_implied`; ALTER TYPE removes
  `type_resolved` (and to a serial name it is refused: partial).
- PRIMARY KEY and identity imply NOT NULL; dropping a primary key keeps the
  NOT NULL marks (only `DROP NOT NULL` clears them). ALTER TYPE without
  COLLATE resets the collation to the type default (`null`).
- DROP COLUMN removes the indexes (elements, INCLUDE) and table constraints
  (including named CHECK and EXCLUDE constraints that name it) that use the
  column; an expression element or partial-index predicate naming it drops
  the index and makes the table partial. Foreign keys from other tables and
  generated columns whose expression names it (pg_attrdef.c 187-195) need
  CASCADE: without it the drop is refused (involved tables partial, nothing
  changes, the migration may fail); with it they are dropped too, with the
  indexes and constraints of the dropped generated columns (WF7). ALTER
  TYPE of a column a generated column uses is refused (tablecmds.c
  13627-13634). After a view exists, DROP COLUMN/TABLE without CASCADE and
  ALTER TYPE make the table partial instead of applying, and the migration
  may fail.
- `DROP CONSTRAINT [IF EXISTS] n` of a name the set wrote on a table
  whose constraint names are exact follows C3 (row `M5g-1`, "Claim
  contract"): exactly that constraint goes. Otherwise Revision 7 applies
  (superseding the Revision 4-6 name rules): any DROP CONSTRAINT, a
  generated name or a name on a table whose names are not exact, makes
  every key fact of each
  table the name may mean (all same-name tables when the qualification or
  search_path is uncertain) unknown: the table is partial, `primary_key`,
  `unique_keys` and the columns' `primary_key` and `unique` are `null`,
  its unique indexes stop proving `unique`, its own references are
  `unresolved`, and every foreign key that may reference it is
  `unresolved` with its table partial. NOT NULL marks stay (Postgres 17
  clears `attnotnull` only in ALTER COLUMN DROP NOT NULL, tablecmds.c
  7666-7668); CHECK constraints are kept. Without IF EXISTS the name may
  be missing, which Postgres refuses (tablecmds.c 12644-12651), so the
  migration may fail, whatever the table (also an unknown one). With IF
  EXISTS it is not a possible failure on that account, but a foreign key
  that may use one of the table's keys makes it one without CASCADE
  (key dependencies, below). A later ADD PRIMARY KEY may be refused (the
  old key may remain).
- **Merged key constraints** (Revision 7 F1, parse_utilcmd.c 2084-2140):
  within one CREATE TABLE (in source order) or one ALTER TABLE subcommand
  (ADD COLUMN with inline keys), the primary key is kept first and a
  UNIQUE constraint equal to a kept key in ordered columns, INCLUDE
  columns, NULLS NOT DISTINCT, DEFERRABLE and INITIALLY DEFERRED is
  dropped; its written name goes to an unnamed survivor. `a int PRIMARY
  KEY UNIQUE` gives only the primary key. Separate ALTER TABLE
  subcommands are not merged (Postgres 17 transforms each alone,
  tablecmds.c 5567-5592). WITH and USING INDEX TABLESPACE are not
  compared. An INCLUDE list that cannot be read makes the table partial
  and `unique_keys` `null`.
- **Key dependencies** (Revision 4 WF1, WF2): a foreign key depends on the
  unique index of the key it uses (pg_constraint.c 312-324). DROP INDEX
  of a unique index (also after ALTER INDEX RENAME) that a known foreign
  key references by exactly its columns is refused without CASCADE; with
  CASCADE the foreign key is dropped when that index was its only
  possible key and `unresolved` otherwise, and the referencing table is
  partial. Any DROP CONSTRAINT on a table that a foreign key may
  reference (Revision 7), and a DROP INDEX of an unwritten name on a
  table with an unnamed unique index some foreign key uses, may be refused (without CASCADE) or leave
  those references `unresolved` (with CASCADE); such a DROP INDEX makes
  the table's unnamed indexes uncertain (they no longer prove `unique`
  or back a foreign key). DROP INDEX of the index of a primary key or
  UNIQUE constraint, by its written or generated (`<t>_pkey`,
  `<t>_<cols>_key`) name, is always refused, even with CASCADE
  (dependency.c 770-789). DROP COLUMN of a column that a unique index
  INCLUDEs needs CASCADE when a foreign key uses that index.
- **Partitions and inheritance** (Revision 4 WF4, G-a): DROP COLUMN and
  ALTER COLUMN TYPE of a column named in PARTITION BY are refused
  (tablecmds.c 9047-9057, 12871-12877); ALTER TABLE c INHERIT p is refused
  when p is c or inherits from it (tablecmds.c 15732). DROP TABLE of a
  partitioned table drops its partitions (PARTITION OF, ATTACH PARTITION;
  partial instead when the parent is partial); DROP TABLE ... CASCADE drops
  the tables that inherit from it; DROP TYPE of the type of a CREATE TABLE
  ... OF table is refused without CASCADE and drops the table with it.
- **Identity and retyping** (Revision 4 G-b, G-c): ALTER COLUMN ... ADD
  GENERATED AS IDENTITY on a complete table is refused when the column is
  nullable, already an identity or has a default (tablecmds.c 8064-8085);
  otherwise it stays unmodeled (partial). ALTER COLUMN TYPE of a column on
  either side of a known foreign key, to a type that is not the other
  side's type (aliases equated, see below), makes the reference `unresolved`, both tables partial
  and the migration one that may fail (Postgres rebuilds the foreign key
  and refuses incompatible types).
- **Certain refusals** (Revision 3 class 3). A table is *complete* here
  when it has no partial reason and no opaque statement ran since its
  creation. Postgres refuses (the migration may fail, WF8) a column
  defined twice in CREATE TABLE, always; and on a complete table whose
  name surely means it: DROP/ALTER/RENAME COLUMN, a constraint, an index
  element or INCLUDE naming a column the table lacks (on a partial table it
  stays a partial reason); DROP TABLE without CASCADE of a table that a
  known foreign key references, that a table INHERITS from (CREATE TABLE
  ... INHERITS, ALTER TABLE ... INHERIT), or whose row type a column uses.
- **Foreign-key targets** (Revision 3 class 3): a reference resolves
  (`linked`) only when its columns are, in any order, exactly the columns
  of a non-deferrable primary key, UNIQUE constraint or unique index
  without predicate or expression of the referenced table (REFERENCES t
  without columns: its non-deferrable primary key; tablecmds.c
  transformFkeyCheckAttrs). Otherwise it is `unresolved`: on a complete
  referenced table also a refusal; on a partial one (any reason, including
  forgotten keys, uncertain unique indexes, LIKE/OF/PARTITION OF/INHERITS
  tables and opaque statements since its creation) or under a name that
  may mean another table, a possible failure (Revision 8). A reference to
  a table the set never creates stays unresolved only. Postgres adds the keys of a CREATE TABLE
  or ALTER TABLE statement before its foreign keys, so a REFERENCES may use
  a key a later element or action of the same statement defines.
- **Possible failures from unknown facts** (Revision 8). When a
  statement's success depends on a fact the pass does not know, the
  migration may fail (WF8); IF EXISTS and IF NOT EXISTS forms that
  succeed whatever the fact are not. A table's facts are *known* when no
  opaque statement ran since its creation and its only partial reasons
  are name moves (ALTER SCHEMA RENAME, SET SCHEMA). On a table whose facts
  are not known: a column a statement names may not exist (constraints,
  index elements and INCLUDE, ALTER/DROP/RENAME COLUMN; DROP COLUMN IF
  EXISTS excepted); ADD COLUMN and RENAME COLUMN TO may meet an existing
  name; ADD PRIMARY KEY may meet an existing key; ALTER COLUMN TYPE, DROP
  NOT NULL, SET/DROP DEFAULT and ADD GENERATED AS IDENTITY may meet
  unknown dependents or attributes; DROP COLUMN and DROP TABLE without
  CASCADE may meet unknown dependents (also when an unresolved foreign key
  may reference the column or table). A name that may mean another table
  or none (search_path change, another qualification, a temporary table)
  makes ALTER TABLE and CREATE INDEX possible failures, except ALTER
  TABLE whose actions are all ADD COLUMN IF NOT EXISTS without
  constraints or DROP CONSTRAINT IF EXISTS (plus ALTER TABLE IF EXISTS
  after a search_path change); DROP TABLE may fail after a search_path
  change without IF EXISTS, or under another qualification without
  CASCADE (a temporary table of the name exists and has no permanent
  dependents). A new name that may be taken (an uncertain relation, or a
  table of that name whose qualification may be the same schema) makes
  CREATE TABLE/INDEX/VIEW, RENAME TO, ALTER INDEX ... RENAME TO and SET
  SCHEMA possible failures. DROP INDEX and ALTER INDEX without IF EXISTS
  of a name that may mean no index (search_path, another qualification, a
  never-written name that may be generated) may fail. A new relation,
  index or ADD CONSTRAINT name of the shape of a generated name of a
  known unnamed object (`<t>_pkey`, `<t>_<...>_pkey|key|idx|excl|seq`,
  for a constraint of the same table also `fkey|check`, optional
  collision digits, or any name of 63 bytes) may fail unless IF NOT
  EXISTS. A new foreign key whose column types are not the same type is
  `unresolved` and may fail. Types are compared as Postgres resolves them
  (Revision 9, gram.y REL_17_6): keyword spellings become their
  pg_catalog type (`int`/`integer` `int4`, `smallint` `int2`, `bigint`
  `int8`, `real`/`float(1..24)` `float4`, `double precision`/`float`/
  `float(25..53)` `float8`, `boolean` `bool`, `character varying(n)`/
  `varchar(n)` `varchar(n)`, `character(n)`/`char(n)` `bpchar(n)` with
  bare `char` `bpchar(1)`, `decimal`/`numeric` `numeric`, `timestamp with
  time zone` `timestamptz`, `time with time zone` `timetz`), serial
  aliases are their integer type, and other names (quoted or not) stand
  for themselves (`"int4"` is `int4`, `"integer"` is not). Modifiers and
  array dimensions must match; different types (`int` and `bigint`,
  `varchar` and `text`) stay unequal. CREATE VIEW/MATERIALIZED VIEW/SEQUENCE/TYPE/DOMAIN of a name
  recorded in the same pg_class or pg_type namespace (row types of
  tables, views, materialized views and composite types; not sequences or
  indexes) is refused, or may fail when the record is uncertain; IF NOT
  EXISTS skips only over a relation; CREATE OR REPLACE VIEW over another
  relation is refused and over a view may fail. Left out, as outside the
  model: tables, indexes and relations the set never creates, names an
  opaque statement or another tool may have created, row data (validation
  of keys, CHECK, NOT NULL, casts), schema existence and unmodeled ALTER
  TABLE actions.
- **Renames of indexes** (Revision 3 class 2): `ALTER TABLE i RENAME TO j`
  renames a known index `i` (tablecmds.c 17901-17906; `SET SCHEMA` of an
  index is refused, other ALTER TABLE actions on it make the table
  partial); `ALTER INDEX/TABLE c RENAME TO d` of a written primary key or
  UNIQUE constraint name renames the constraint; a generated
  `_pkey`/`_key`/`_idx`/`_seq` name makes the table partial; `ALTER
  VIEW/MATERIALIZED VIEW/SEQUENCE/TYPE` on an index, or on a recorded
  relation of another kind, is refused (tablecmds.c 17907-17930).
- **Lexing settings** (Revision 3 G8): Postgres lexes each migration's
  whole query string before running it, so a lexing setting changed in a
  migration takes effect from the next one; Graphvein lexes with
  `standard_conforming_strings = on` and `backslash_quote =
  safe_encoding`, so after a change every later migration may fail.
- **Qualification** (P3): unqualified tables keep `schema: null` (the
  database's search_path decides). A name used under another qualification
  than a known table of the same name (`public.users` vs `users`) makes the
  known tables partial and leaves the reference unresolved.
- **search_path** (R2): after a search-path statement, every unqualified
  table created or altered is partial, and unqualified references are
  unresolved. `ALTER SCHEMA s RENAME TO t` and `DROP SCHEMA` count as such
  a change too (unqualified names may have lived in that schema).
- **Uncertain targets** (Revision 2 WF4): when a name may resolve to
  another table (an R2 change, a temporary table of the same name, another
  qualification) or the statement may not apply (a migration that may
  fail), `DROP TABLE`, `DROP COLUMN`, `DROP CONSTRAINT` and `DROP INDEX`
  keep the object and make the table partial (unknown is not absent);
  renames and other changes are applied to the known table, which is
  partial. An index such a DROP INDEX names is no longer proven (Revision
  4): it stops proving `unique`; with CASCADE the foreign keys that may use
  it are `unresolved`. DROP CONSTRAINT follows its own rule (Revision 7,
  above) on every table the name may mean.
- **Index names** (WF5): an index lives in its table's schema. `DROP
  INDEX`/`ALTER INDEX` with a qualification that differs from the known
  index's table (`public.i` for an index of an unqualified table) keeps the
  index and makes the table partial. `ALTER INDEX n RENAME TO m` renames a
  written index; when `n` names a table it renames the table, because ALTER
  INDEX ... RENAME accepts any relation (tablecmds.c 17901-17906,
  17931-17937); other ALTER INDEX forms and DROP INDEX on a table name are
  refused.
- **Schema rename** (WF6): `ALTER SCHEMA s RENAME TO t` moves the tables
  written as `s.x` to `t.x` (partial, with a reason naming the old name),
  makes unqualified tables partial and counts as an R2 change.
- **Other relations** (WF3): views, materialized views, sequences, indexes,
  composite types, `CREATE TABLE AS` and `SELECT INTO`
  targets and `CREATE SCHEMA` elements (tables, views, sequences, indexes)
  share the relation namespace; enum, range, base types and domains share
  the type namespace with the row type of a table. `CREATE TABLE IF NOT
  EXISTS` of a relation name is skipped (parse_utilcmd.c 195-211:
  `schema_relation_exists`, no table); `CREATE TABLE` of a relation name
  without IF NOT EXISTS, or of a type name with or without it (heap.c
  1170-1180), is refused. When the relation may have gone since (an opaque
  statement, an ALTER of it, a search_path change, a possible failure, or
  only another qualification matches, or the name equals a generated
  `_pkey`/`_key`/`_idx`/`_seq` name), the table is created and partial.
  Creating such an object under a table's name, or `DROP`/`ALTER
  VIEW/SEQUENCE/TYPE/...` of a table's name, is refused.
- **Migrations that may fail** (Revision 2 WF8): Postgres parses a
  migration's whole query string before running it and runs it in one
  transaction, and sqlx stops the set at a failed migration (migrator.rs
  168-182 at 0.8.6). A statement that could not be read (a parse error or
  a lexing failure) or that Postgres refuses or may refuse (the refusals
  named in these rules: a second primary key, an existing name, a refused
  DROP, CONCURRENTLY, VACUUM, ...) makes the migration one that may fail:
  every table it created or altered (also before the statement) and every
  table a later migration creates or alters is partial with a reason naming
  the failure, tables it dropped are restored as partial, and the
  migration and every later one get `applied: null` (their `order` stays).
  A Graphvein parse error does not prove that Postgres fails, so the state
  is unknown, not "not applied". A statement on a table the set does not
  know is unresolved, not a possible failure (the table may exist outside
  the set).
- **Functions on existing rows** (G5): a modeled ALTER TABLE or CREATE
  INDEX, a CREATE TABLE AS / CREATE MATERIALIZED VIEW (WITH DATA) and a
  SELECT INTO that call a function the set defines also count as opaque
  (`function_call`; the statement stays in the `modeled` or `unsupported`
  count).
- **Opaque** statements increment `opaque_after` on every table existing at
  that point and the set's `set_opaque_statements`; `CREATE TABLE IF NOT EXISTS`
  after any opaque statement makes the table partial (R3). A statement that
  fails to parse after its target is known makes that table partial;
  otherwise it counts as opaque.

## Records

Identity per contract v1. Qualified names use folded names; a component
containing `.`, `"`, `(`, `)`, `,` or `@` is written in SQL double quotes.

- `migration` (level 3, whole file): `tool`, `set`, `package`, `dialect`,
  `version` (decimal string or null), `version_text`, `direction`,
  `applied` (true, false or null: undefined order, or a possible failure of
  it or an earlier migration), `order` (sqlx's apply position; null for
  down migrations, failed ordering and an undefined order),
  `description`, `statements`, `modeled`, `no_effect`, `unsupported`,
  `opaque`, `errors` (null when not analysed), `extensions`,
  `set_opaque_statements` (`{total, by_kind}` for analysed Postgres sets,
  else null; the same value on every migration of the set),
  `opaque_sites` (T018f: `[{span, row, kind}]`, one per statement counted
  in `opaque`, in file order; `kind` from the `by_kind` vocabulary; null
  when the counts are null), `scope`.
- `table` (level 4, the CREATE TABLE statement): `name`, `schema`,
  `dialect`, `set`, `state` (`complete`/`partial`), `reasons`
  (`[{migration, span, reason, row}]`, set-level reasons have null migration and
  span; T018f `row`: the precondition id the reason cites, else the contract
  row of the statement that added it, `D` for an unlexed file rest, null
  only for the set-level reasons no row produces: an unlisted set
  directory, an unreadable member, `sqlx.toml`), `opaque_after`, `primary_key` (the key's columns; `[]` when the
  table is complete and has none; `null`, unknown, on a partial table
  without a known key; Revision 3 N2), `unique_keys`
  (`[{constraint, columns}]`, after Postgres's merging of equal key
  constraints; `null`, unknown, after any DROP CONSTRAINT on the table,
  Revision 7),
  `previous_names`, `scope`. A table whose CREATE TABLE header was read but
  whose body was not, or whose columns come from LIKE, OF, PARTITION OF or
  INHERITS, has no columns and is partial (Revision 3 G4, Revision 4 G-d).
- `column` (level 4, the column definition): `name`, `position`, `type`,
  `type_resolved` (serial only), `nullable` (`false` when NOT NULL is
  known; `true` on a complete table otherwise; `null` on a partial table
  otherwise, Revision 3 N2), `default`, `default_implied`
  (serial only), `collation`, `unique` (Revision 2 G3: `true` when the
  column alone is the primary key, a UNIQUE constraint or the key of a
  unique index without predicate and expression; `false` when none exists
  and the table is complete; `null`, unknown, on a partial table),
  `primary_key` (`true` when in the known primary key; `false` on a
  complete table otherwise; `null` on a partial table otherwise),
  `identity`, `generated`, `references` (`{table, columns,
  local_columns, constraint, on_delete, on_update, status, reason?}` or
  null), `additional_references` (only when a column has several foreign
  keys), `previous_names`, `scope`, `claims` (below).
- `db_index` (level 4, the CREATE INDEX statement): qualified name
  `<table>.<name>` or `<table>.(<elements>)` with `@2`... for repeated
  unnamed elements; `name`, `unique`, `using`, `elements`, `include`,
  `where`, `scope`, `claims`. Constraint-backed indexes are not db_index nodes.

`claims` (T018f, ADR 0055; provenance, not claims of their own): one entry
`{fact, row, migration, span}` per attribute the record states as known
(not null), sorted by `fact`, naming the statement that last changed it.
Column facts: `exists` (always), `type`, `type_resolved`, `nullable`,
`default`, `collation`, `identity`, `generated`, `primary_key`, `unique`,
`references`, `additional_references` and `name` (only after a rename);
`db_index` facts: `exists`, `name`, `unique`, `using`, `elements`,
`include`, `where`. The pass fingerprints every held column and index
before and after each statement (by object, so a table rename does not
change its columns' `references`); a changed fact takes the statement's
row that fits it (`exists`: M1, M5a, M4, M5f-u; `type`: M2 on creation,
M5c later; `nullable`: M5d, M5f, M5f-u; `default`: M5e; `name`: M5h, M8,
M5i; `references`: M3; ...), else the statement's own row
(`claims.go` `claimRows`). A copy the state restores (ROLLBACK TO) keeps
the copied claims. `TestClaimProvenance` checks every claim's row against
the fact kinds `TestContractFacts` allows it, one claim per known fact,
`opaque_sites` against `opaque`, and the reason rows.

Edges (fact, confidence 1): `contains` file -> migration, creating
migration -> table, table -> column, table -> db_index; `alters` migration
-> table for every later migration whose modeled statements touched the
table (evidence: those statements); `foreign_key` column -> column per
column pair (evidence: the REFERENCES or FOREIGN KEY clause). Unresolved
references give no edge and `schema_unresolved_reference`.

Diagnostics: warning `schema_parse_error`, `schema_migration_order`,
`schema_duplicate_record`, `schema_internal_error` (the pass failed on one
set; nothing of that set is emitted, the other sets are),
`schema_manifest_unreadable` (a `Cargo.toml` the pass cannot read); info
`schema_unsupported_statement`, `schema_opaque_statement`,
`schema_partial_table`, `schema_unresolved_reference`,
`schema_unsupported_dialect`, `schema_unsupported_config`,
`schema_member_unreadable`, `schema_migration_may_fail`,
`schema_relation_exists`. Messages name the file, line and byte span.
`set_opaque_statements.by_kind` kinds: extension, function_call, do, call,
execute, dml, runs_code, search_path, transaction, event_trigger,
drop_cascade, drop_owned, parse_error, lexing_failure, lexing_setting.

On a partial table the facts that state an absence (`nullable: true`,
`primary_key: false`, `unique: false`, no primary key) are `null`: an
unread or uncertain statement may have added the constraint. The facts
source proves (`nullable: false`, a known key, `unique: true`, the known
columns) stay, as unknown is not absent, unless a later statement may
have removed them: any DROP CONSTRAINT forgets all keys and foreign keys
of the tables it may name (`primary_key`, `unique_keys`, the columns'
`primary_key` and `unique` become `null`, the references `unresolved`;
Revision 7), and a DROP
INDEX that may not apply or whose name was never written makes the
indexes it may be stop proving `unique`. NOT NULL stays: no DROP
CONSTRAINT or DROP INDEX clears it in Postgres 17. On a complete table with
`opaque_after > 0` every attribute is stated as the modeled statements
leave it; `opaque_after` says that an opaque statement may have changed
any of them (ADR 0047).

## Claim contract

T018a2 (ADR 0051). The pass claims a fact only when a row below covers the
statement form and the row's preconditions hold; every other form goes to
the default branch `D`, which claims nothing. `applied: true` means:
Postgres 17 accepts this migration and every earlier one, given the
environment assumptions E1-E6 and E8 below (Revision 2.1: under E8 a
refusal the pass missed is an E8 hit, not a wrong fact). A statement whose success depends on
a fact the pass does not hold makes the migration one that may fail
(`applied: null`, WF8); a precondition the records prove false is a
refusal, reported the same way. The Go table `contract.go`
(`contractRows`, `contractPreconditions`) and these tables are equal:
`TestContractIDs` and `TestContractDocument` check them, and every
diagnostic a precondition produces ends with `[contract <row>.<class>]`.
Every analysis carries the row it was produced under (`analysis.row`,
`rowOf`; ALTER TABLE subcommands, column types, references and queries add
their own rows), and `TestContractRows` checks that every row id the code
records is listed, that every row is exercised by a fixture or regression
case and by the generators, and that forms no row covers reach `D`;
`TestContractFacts` (Revision 1.4) checks every fact each statement adds
or changes against the fact kinds its rows may claim, and that every
precondition is met by at least one case.

Row kinds: **M** modelled (table, column, index and foreign-key facts
exact), **R** recorded name (pg_class or pg_type namespace record, no table
facts), **N** no effect, **O** opaque (counted; success assumed under E3),
**F** refused inside sqlx's transaction, **T** transaction control, **Q**
set reading, **D** default. Precondition classes (task Revision 1): P1
names, P2 shared namespaces and renames, P3 conditional forms (IF [NOT]
EXISTS, OR REPLACE), P4 untracked objects, kinds and options, P5 columns,
expressions and values, P6 transaction structure, P7 syntax (gram.y
REL_17_6), P8 dependents, P9 subcommand order, P10 set reading, P11 known
rows (Revision 1.4), P12 code effects and P13 session state (Revision 1.5),
P14 compile-time validation of routine and DO bodies (Revision 1.6), K2
enum labels.

**Environment assumptions** (the oracle harness classifies a refusal that
breaks one as an assumption hit, not a disagreement):

- **E1 objects outside the set**: roles, extensions, schemas, tables,
  functions and operators the set never creates in any migration exist
  with a signature that fits their use, as do server files `COPY` names. A
  name the set creates anywhere (later, earlier and dropped or renamed
  away, through RENAME TO, a generated-name shape of a set table, a
  CREATE TABLE AS / SELECT INTO target, a CREATE SCHEMA element) is not
  outside; a `pg_catalog` function is never E1. Revision 1.5 (U7): the
  installed extensions are those of REL_17_6 `contrib/`, and the types and
  operator classes their scripts create are E1 objects (the generated
  tables `e1Types`, `e1Opclasses`); any other type, operator class,
  collation, setting or index is not E1 (`M2.P1`, `M4.P4`, `N2.P1`,
  `M7.P1`). The oracle attributes a refusal to E1 only by the message
  templates of harness rule H12. Revision 1.5 (U2): a function outside
  the set and a `pg_catalog` function change no object the set creates or
  uses (their bodies are not read; an outside function that runs DDL on
  set objects is an assumption hit), and a name that code in a DO block
  or a set-defined routine body creates is a name the set creates.
- **E2 rows**: no existing row breaks a constraint, NOT NULL or cast that
  a later ALTER TABLE or CREATE INDEX checks, and the rows of a server
  file `COPY` reads are valid. The values the set's own DML writes are not
  E2 (Revision 1.3): they are predictions (`N3.P5`). Nor are the rows
  they leave (Revision 1.4 R5): a later statement that checks or
  rewrites them is not proven (P11), and the oracle judges its refusal
  (harness rule H9).
- **E3 code**: DO, CALL, function and trigger bodies and extension
  scripts finish without raising. E3 grants nothing about what the code
  does to objects: that is class P12 (Revision 1.5 U2, "Code effects"
  below).
- **E4 session**: Postgres's default settings
  (`standard_conforming_strings` on, `check_function_bodies` on) and the
  history's schema as search_path (`"$user", public`), in a UTF8 database
  (the built-in collations `ucs_basic` and `pg_c_utf8` of `M2.P1` exist
  only in UTF8 databases, pg_collation.dat `collencoding` 6). The current
  user's name is not known and names no schema outside the set; a schema
  the set creates may be named like it (T018a2 Revision 1.6 V6).
- **E5 built-in schemas** (T018a2 Revision 1.9, D-public): the database
  has the schemas initdb creates in every database, `public` (owned by
  `pg_database_owner`) and `information_schema`, besides `pg_catalog`
  and `pg_toast`; the pass already relies on `public` for unqualified
  names and sqlx's bookkeeping table. A schema the set's DROP SCHEMA
  removed is no longer granted. `CREATE SCHEMA public` is therefore
  refused (Y08).
- **E6 privileges short of superuser** (Revision 1.9, D-superuser): the
  migrating role is not a superuser. It owns the database and the E5
  schemas the set uses, has CREATEROLE, CREATEDB, REPLICATION and
  BYPASSRLS, is a member of `pg_read_server_files`,
  `pg_write_server_files` and `pg_execute_server_program`, holds ADMIN
  OPTION on the outside roles of E1 (ordinary roles, not superusers),
  may SET ROLE to the roles the set creates, and holds the privileges
  on E1 objects the set's statements need. A statement only a superuser
  can run is may-fail (Y51). The oracle replays as such a role (H23)
  and scores other permission refusals as E6 hits.
- **E8 history applies** (T018a2 Revision 2.1, D-E8): the committed
  migration history applies cleanly, in order, as it did in the
  project's own environments. Migration verdicts and the schema the pass
  derives from them rest on E8: `applied: true` and the facts the pass
  claims are the schema the history defines under E1-E6 and E8, the way
  code is shown without claiming that it compiles. A refusal that
  Postgres raises on a fresh database for a migration the pass claims is
  a missed refusal prediction (an E8 hit, harness rule H27), not a wrong
  fact. A refusal the pass proves stays reported: `applied: false` with
  its `[contract <id>]` diagnostic (the migration fails on a fresh
  database), and facts after it are not claimed. A statement whose
  effects are not modelled stays unknown. Wrong facts are a wrong effect
  for a history that applies cleanly, or a proven refusal of a migration
  Postgres applies; the merge bar applies to them unchanged. (E7, runtime
  resources, belongs to Revision 2.0 and is subsumed by E8 in v1.)

### Revision 1.9 pending contract changes

T018a2 Revision 1.9 (task file, rules Y01-Y52, harness H21-H24) changed
the tables below in three implementation units, U-a (statement
validators), U-b (state records, H24) and U-c (the dependency rule Y39
with Y40, Y41, Y44, Y45 and Y52, statements of code Y48-Y50, the
superuser-only forms Y51, and harness rules H21-H23). All three have
landed: their entries are in the tables, and this list is empty. Every
refusal or may-fail a rule adds ends with `[contract <id>]` of the id the
rule names, and the code keeps one branch per modelled form (forms no
rule covers stay in the default branch `D`).

- (none pending)

The last column of both tables names the environment assumptions (E1-E6
and E8 above) a row's claim or a precondition's verdict rests on, `-` for none;
it is metadata for showing the assumptions next to the facts and changes
no claim (`contractAssumptions` in `contract.go`, checked by
`TestContractAssumptions` and `TestContractDocument`).

### Revision 2.1 contract changes

T018a2 Revision 2.1 (task file, "Revision 2.1 (2026-11-09): wrap-up
under E8"; developer decision 2026-11-09) wraps the task up under the new
assumption E8 (above). Step A2 implemented it, and this list is empty:
E8 is in "Environment assumptions"; `contractAssumptions` (and the last
column of the rows table) names E8 on every row whose claim is a fact or
`applied: true` (every row but D, which claims nothing, and F1, whose
only verdict is a refusal) and on no precondition (a precondition's
verdict is a refusal or a may-fail); the oracle judges generated and real
histories under harness rule H27 and runs seeds 1-200 per generator by
default ("Oracle" below). No row, precondition or rule changed; Revision
2.0's demotions do not apply in v1 (each form's effects are modelled: no
fact, or the tables that use the object become partial).

- (none pending)

### Revision 2.0 pending contract changes

T018a2 Revision 2.0 (task file, the error-site audit) is the after-v1
plan (VISION "After v1"; Revision 2.1 above). It would change the
contract in steps U0-U11 and Z, none of which runs in v1; its changes
stay listed here, outside the tables:

- E1 is replaced by Revision 2.0's exact list: outside roles; outside
  objects that are neither built-in nor extension members; contrib
  extensions the set never creates or drops, whose members exist but are
  never E1 for statements that change them; plpgsql; server files.
  Tablespaces other than `pg_default` and `pg_global` are not E1 (U0).
- E6 is replaced. The role owns the database, and so `public` through
  `pg_database_owner`, but not `information_schema`, `pg_catalog`,
  `pg_toast`, built-in objects, extension members or plpgsql. It cannot
  change its own role attributes. Refusals Postgres raises for every
  role are never E6 hits (U0).
- E7 runtime resources is proposed for the developer's decision (U0).
- Precondition class P15 (privileges and ownership) is added, with the
  built-in ownership rule first (U0).
- The audit tables `docs/extract/schema-audit/<area>.tsv` and the claim
  gate are added. A claimed form must be `audited` in `audit.go` (U0,
  then each unit; the gate is enforced by Z).
- Proposed demotions to D: composite ALTER TYPE attributes and ALTER
  DOMAIN (U3); CREATE OPERATOR, CAST, AGGREGATE, PUBLICATION and
  COLLATION, and SECURITY LABEL (U1, U11); REASSIGN OWNED and DROP OWNED
  (U1); MERGE (U8).

### Rows

| id | kind | form | claim | preconditions and refusal conditions | assumptions |
|---|---|---|---|---|---|
| `M1` | M | CREATE [UNLOGGED] TABLE [IF NOT EXISTS] n (cols, table constraints) | table, columns, keys, foreign keys, CHECK presence | name taken in pg_class or the row-type namespace (IF NOT EXISTS skips only over a relation); duplicate column; key column missing; Revision 7 F1 merging; Revision 8 generated-name shapes, narrowed by C5; an unreadable element gives a name-only partial node (D); `M1.P1`, `M1.P2` (Revision 1.4 R2: one constraint namespace per table), `M1.P3`, `M1.P4` (R3: column definitions, key column lists, table options, partitioning), `M1.P5` (R1: DEFAULT, generation and CHECK/EXCLUDE expressions); `M1.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `M1b` | M | CREATE TABLE n (LIKE ...) / OF t / PARTITION OF p / INHERITS (...) | partial node with the name only | as M1 for the name; circular INHERIT; `M1b.P1`; `M1b.P12` (code effects, Revision 1.5 U2); `M1b.P5` (Revision 1.9 Y29: partition bounds) | E1, E4, E5, E6, E8 |
| `M2` | M | column type (Typename grammar) | written type; alias-normalised type for comparisons | a word after a complete type that is not a column constraint: parse error (D); `M2.P5` (Revision 1.4 R4: type modifiers) | E1, E4, E5, E6, E8 |
| `M3` | M | REFERENCES t [(c)] / FOREIGN KEY (...) REFERENCES in M1, M5f | foreign_key edge to a proven key | referenced columns not exactly a non-deferrable PK, UNIQUE or plain unique index; no key on a complete table; a type pair outside C2's verified pairs (same type, int2/int4/int8, float4/float8, text/varchar/varchar(n)/char(n), integer referencing numeric); partial referenced table without a proven key; a name that may mean another table; `M3.P1` (K1 and backward names), `M3.P4` (persistence; Revision 1.4 R3: MATCH PARTIAL, SET column lists, generated key columns) | E1, E4, E5, E6, E8 |
| `M4` | M | CREATE [UNIQUE] INDEX [CONCURRENTLY] [IF NOT EXISTS] [n] ON [ONLY] t ... | db_index, column unique | column missing on a table whose facts are known; name taken; generated-name shapes (C5); `M4.P1`, `M4.P3`, `M4.P5` (IMMUTABLE elements, Revision 1.1), `M4.P6` (CONCURRENTLY: refused in any block; C4: outside a block refused on a partitioned table and not proven on a table the pass does not hold), `M4.P7`, `M4.P11` (Revision 1.4 R5: rows the set wrote); `M4.P12` (code effects, Revision 1.5 U2) | E1, E2, E4, E5, E6, E8 |
| `M5a` | M | ALTER TABLE ... ADD [COLUMN] [IF NOT EXISTS] coldef | new column | column may exist (Revision 8); `M5a.P1`, `M5a.P2`, `M5a.P4`, `M5a.P5`, `M5a.P9` (pass order), `M5a.P11`; `M5a.P12` (code effects, Revision 1.5 U2) | E1, E2, E4, E5, E6, E8 |
| `M5b` | M | ALTER TABLE ... DROP [COLUMN] [IF EXISTS] c [CASCADE\|RESTRICT] | column removed with its dependents | column missing; partition key; dependents without CASCADE (Revision 4) | E1, E4, E5, E6, E8 |
| `M5c` | M | ALTER TABLE ... ALTER [COLUMN] c [SET DATA] TYPE t [COLLATE] [USING] | new type | partition key; FK pair types (C2); facts not known; `M5c.P5`, `M5c.P8`, `M5c.P11`; the new type's modifiers as `M2.P5` | E1, E2, E4, E5, E6, E8 |
| `M5d` | M | ALTER TABLE ... ALTER [COLUMN] c SET / DROP NOT NULL | nullable | column missing; DROP NOT NULL of a PK or identity column; facts not known; `M5d.P11` (SET NOT NULL over rows the set wrote) | E1, E2, E4, E5, E6, E8 |
| `M5e` | M | ALTER TABLE ... ALTER [COLUMN] c SET DEFAULT e / DROP DEFAULT | default | column missing; facts not known; `M5e.P5` | E1, E4, E5, E6, E8 |
| `M5f` | M | ALTER TABLE ... ADD [CONSTRAINT n] PRIMARY KEY / UNIQUE / FOREIGN KEY / CHECK / EXCLUDE | key, foreign key, CHECK presence | PK may exist; columns missing; name taken (relation namespace for index-backed kinds); `M5f.P2` (any constraint of the table), `M5f.P4`, `M5f.P5` (CHECK/EXCLUDE), `M5f.P11` | E1, E2, E4, E5, E6, E8 |
| `M5f-u` | M | ALTER TABLE [ONLY] t ADD [CONSTRAINT n] PRIMARY KEY \| UNIQUE USING INDEX i [DEFERRABLE ...] as the statement's only subcommand | the constraint over the index's key columns; the index renamed to n; primary-key columns NOT NULL (Revision 1.5 U1) | index held under a certain name with its properties known (`M5f-u.P1`); index properties and table kind (`M5f-u.P4`); constraint name free, pg_class name free when renamed (`M5f-u.P2`); NOT NULL over rows the set wrote (`M5f-u.P11`) | E1, E2, E4, E5, E6, E8 |
| `M5g` | M | ALTER TABLE ... DROP CONSTRAINT [IF EXISTS] n [CASCADE\|RESTRICT] | every key of each table the name may mean is forgotten (Revision 7) | without IF EXISTS: may fail (the name may be missing); foreign keys that may use a key: may fail without CASCADE, unresolved with it | E1, E4, E5, E6, E8 |
| `M5g-1` | M | DROP CONSTRAINT of a name the set wrote on this table, tracked exactly | that constraint removed | applies only when the target is certain, its constraint names are exact (facts known, keys not forgotten, no RENAME CONSTRAINT or D action since), no earlier subcommand of the statement adds a constraint (AT_PASS_DROP runs first), the name is not a generated shape the table may hold, and, for a primary key, the set creates no view, rule or SQL-standard body (functional dependency); `M5g-1.P1` (a name the table does not hold: refused without IF EXISTS), `M5g-1.P8` (a key a foreign key uses: refused without CASCADE; with CASCADE the foreign key goes when the key was its only candidate) | E1, E4, E5, E6, E8 |
| `M5h` | M | ALTER TABLE ... RENAME [COLUMN] a TO b | column name | `a` missing; `b` may exist; indexes and keys follow the column by attribute number | E1, E4, E5, E6, E8 |
| `M5i` | M | ALTER TABLE ... RENAME TO n | name | name taken | E1, E4, E5, E6, E8 |
| `M5j` | M | ALTER TABLE ... SET SCHEMA s | schema | name taken | E1, E4, E5, E6, E8 |
| `M5k-a` | M | ALTER TABLE words whose success depends only on the table and E1 | table partial; SET LOGGED/UNLOGGED records persistence | `M5k-a.P4` (SET LOGGED/UNLOGGED persistence pairs); the words are listed in parse.go `m5kWord`; the C5 table `unnamedFreeWords` lists the words that create no unnamed object | E1, E4, E5, E6, E8 |
| `M5k-b` | M | ALTER TABLE words that depend on an object or option | table partial | `M5k-b.P4` (may fail unless the records prove the object and option; refused when they prove it false), `M5k-b.P7` (RENAME, SET SCHEMA, ATTACH/DETACH PARTITION only as the whole statement) | E1, E4, E5, E6, E8 |
| `M6` | M | DROP TABLE [IF EXISTS] names [CASCADE\|RESTRICT] | tables removed | INHERITS parent, row type in use, FK users without CASCADE; uncertain name (Revision 8); `M6.P1`, `M6.P8`; `M6.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `M7` | M | DROP INDEX [CONCURRENTLY] [IF EXISTS] names [CASCADE] | index removed | a constraint's index (always refused); the name may mean no index without IF EXISTS; `M7.P1`, `M7.P3`, `M7.P6` (CONCURRENTLY as M4; C4: outside a block one index, no CASCADE, not a partitioned index); `M7.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `M8` | M | ALTER INDEX [IF EXISTS] n RENAME TO m | index name | name taken; `n` not a relation; `M8.P1`, `M8.P2`; renaming a key's index renames the key (C3); `M8.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `M9` | M | ALTER SCHEMA s RENAME TO t | schema of tables (partial, name move) | `M9.P1`, `M9.P2` (Revision 1.9 Y06, Y08: a rename onto a schema the set holds, an E5 or system schema, or a `pg_` name) | E1, E4, E5, E6, E8 |
| `R1` | R | CREATE [OR REPLACE] [TEMP] VIEW / MATERIALIZED VIEW / SEQUENCE / TYPE / DOMAIN, CREATE TABLE AS, SELECT INTO, CREATE SCHEMA | namespace record | Revision 8 (3) namespace conflicts; OR REPLACE VIEW over a non-view refused, over a view may fail; `R1.P5` (Revision 1.4: CREATE DOMAIN and composite types), `R1.P7`; `R1.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `R1b` | R | the query of a view, CREATE TABLE AS or SELECT INTO, and a LANGUAGE sql body | as R1 | `R1b.P5`: not proven unless the single-relation or constant reader proves every column (`N1.P1` token rule for names) | E1, E3, E4, E5, E6, E8 |
| `R2` | R | CREATE TYPE n AS ENUM ('a', ...) | the enum's labels, in order | as R1; `R1.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `R3` | R | ALTER TYPE n ADD VALUE [IF NOT EXISTS] 'v' [BEFORE\|AFTER 'w'] / RENAME VALUE | label added or renamed | `R3.P1`, `R3.K2` (existing label, missing BEFORE/AFTER label, unknown labels, unsafe use of a new label in the same transaction); `R3.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `R4` | R | ALTER TYPE/DOMAIN (other), ALTER VIEW/SEQUENCE/MATERIALIZED VIEW ... RENAME, ALTER COLLATION | tables that use it become partial | kind mismatch refusals (Revision 3); `R4.P4` (Revision 1.9 Y51: ALTER TYPE ... SET) | E1, E4, E5, E6, E8 |
| `R5` | R | DROP <other kind> [IF EXISTS] names [CASCADE\|RESTRICT], DROP SCHEMA | dependents partial with CASCADE | a known dependent without CASCADE; `R5.P1`, `R5.P4` (the object must be of the dropped kind); `R5.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `R6` | R | CREATE FUNCTION/PROCEDURE, ALTER FUNCTION/PROCEDURE/ROUTINE ... [RENAME TO g] | the set's function names | `R6.P4` (function or procedure kind); a LANGUAGE sql body is R1b; `R6.P12` (code effects, Revision 1.5 U2) | E1, E4, E5, E6, E8 |
| `N1` | N | COMMENT, GRANT, REVOKE, ..., CREATE TRIGGER/POLICY/STATISTICS/..., ALTER SEQUENCE/POLICY/ROLE/... | nothing changes; CREATE TRIGGER and CREATE POLICY record the name on their table, the other named CREATE forms in the set's record of names | `N1.P1` (token rule), `N1.P2` (names of triggers, policies and other N1 objects), `N1.P4` (SECURITY LABEL), `N1.P5` (columns of COMMENT ON COLUMN, UPDATE OF, GRANT (cols); WHEN and policy expressions not proven); `N1.P12` (code effects, Revision 1.5 U2); `N1.P8` (Revision 1.9 Y38: DROP ROLE with shared dependents) | E1, E4, E5, E6, E8 |
| `N2` | N | SET/RESET/DISCARD other than O3/O4 | nothing changes | `N2.P6`, `N2.P13` (Revision 1.9 Y03: transaction modes and session transaction defaults) | E4, E6, E8 |
| `N3` | N | DML, SELECT/VALUES/WITH/TABLE, ANALYZE/CLUSTER/REINDEX/REFRESH | nothing changes | `N3.P1` (column references, Revision 1.2), `N3.P5` (values V1-V6, write hazards, Revision 1.3 and B2), `N3.P7` (SELECT INTO syntax), `N3.P8` (TRUNCATE, CLUSTER); `N3.P4` (Revision 1.9 Y24: REFRESH ... CONCURRENTLY) | E1, E2, E4, E6, E8 |
| `O1` | O | DO, CALL, EXECUTE; a call of a set-defined function; DML in a set that defines functions | opaque count | E3; `O1.P1` (a string argument that spells a set relation name); `R6.P4` for `SELECT p()` of a procedure; `O1.P12` (the DDL inventory: what later statements may meet, or unbounded) | E1, E3, E4, E6, E8 |
| `O1t` | O | SELECT f('<relation>') of a set-defined trigger installer | as N1 CREATE TRIGGER | the closed body form (`BEGIN EXECUTE format('<const>', p); [RETURN;] END`, one `%s` of a regclass or text parameter or `%I` of a text one) whose substituted text is a CREATE TRIGGER; overloads, other bodies and any opaque statement or ALTER/DROP FUNCTION since forget the template (O1); `N1.P2`, P1 for the relation and the trigger function | E1, E3, E4, E6, E8 |
| `O2` | O | CREATE/ALTER/DROP EXTENSION | opaque count | E1 (available), E3; `O2.P4`, `O2.P8` (Revision 1.9 Y33, Y34: extension versions, members in use) | E1, E3, E6, E8 |
| `O3` | O | search_path, role and session-authorization statements, DISCARD ALL | the session's search_path when every change to it is read (U3); otherwise later names uncertain | - | E4, E8 |
| `O4` | O | lexing settings | later migrations may fail | - | E4, E8 |
| `O5` | O | CREATE EVENT TRIGGER, DROP OWNED, DROP ... CASCADE of untracked kinds | opaque; tables partial | `O5.P4` (Revision 1.9 Y51: event triggers are superuser-only) | E3, E6, E8 |
| `F1` | F | VACUUM, REINDEX ... CONCURRENTLY, DROP DATABASE, DROP TABLESPACE, ... | migration may fail | `F1.P6`: refused in any block | - |
| `T1` | T | BEGIN/START, COMMIT/END, ROLLBACK/ABORT, SAVEPOINT, RELEASE, ROLLBACK TO, PREPARE TRANSACTION, LOCK | nothing changes, or the P6 effect | `T1.P6`: a (statement, block state) pair the P6 table (transactions.go) does not list may fail; ROLLBACK and ROLLBACK TO make the migration may fail; C4: a marked migration runs outside sqlx's transaction only when the Cargo requirement allows only 0.8 versions, inside when it allows only earlier ones, both ways otherwise | E8 |
| `Q1` | Q | the set's members as sqlx reads them | applied of every migration | `Q1.P10` (unreadable member: every migration `applied: null`; non-UTF-8 member: the set is refused; an unreadable manifest the dialect depends on: no records) | E4, E5, E8 |
| `D` | D | every other form | tables it may name partial; the migration may fail | also: text a row's reader accepts but gram.y REL_17_6 rejects (P7; Revision 1.6 V7: a statement of a governed row that none of the row's listed gram.y productions reads whole, and a reserved or type_func_name keyword in an element or expression column position, V3), plain PREPARE, ALTER TABLE subcommands with no M5 or M5k word (Revision 1.5 U1: they may fail, and every index of the table stops being a fact; USING INDEX outside row `M5f-u`), and every M5k-b or T1 case the table does not list | - |

### Preconditions

Each id is `<row>.<class>`; a row that carries a class without its own id
(for example M5b, M5d and M5e under P1) cites the id of the row named in
the text.

| id | precondition | assumptions |
|---|---|---|
| `M1.P1` | CREATE TABLE: column types, defaults, checks and parents the set creates must be held | E1 |
| `M1b.P1` | CREATE TABLE LIKE/OF/PARTITION OF/INHERITS: the source the set creates must be held; Revision 1.6 V11: a WITH OPTIONS column list of PARTITION OF or OF is not read (row D, M1b.P7) | E1 |
| `M3.P1` | REFERENCES: a referenced table the set creates must be held; Revision 1.8 X3: a referenced name the pass holds as a view, materialized view, sequence, composite type or index is refused (42809), may fail when its kind is not certain, and a table that may hold another relation kind (CREATE TABLE IF NOT EXISTS) is not proven | E1 |
| `M4.P1` | CREATE INDEX: the table and the functions the elements call must be held; Revision 1.8 X3: a table the pass holds as a view, sequence, composite type, foreign table or index is refused (42809), may fail when its kind is not certain; an index on a materialized view (its columns not proven) or on a table that may hold another relation kind may fail | E1 |
| `M5a.P1` | ALTER TABLE (every M5 row): the table, and the types and expression objects the set creates, must be held; on a table the pass does not hold that the set changed earlier, the statement is not proven; on a name that is an index only RENAME, RENAME COLUMN, OWNER TO, SET/RESET (...) and SET TABLESPACE are accepted, other actions are refused (42809) or not proven (Revision 1.4); Revision 1.6 V4 (kinds, 2026-11-07): on a name held as a view, materialized view, sequence or foreign table, also with IF EXISTS, an action whose ATSimplePermissions mask excludes that kind is refused (42809) and only RENAME, SET SCHEMA, OWNER TO and (on a sequence) SET LOGGED/UNLOGGED are accepted without further proof; on a name held only as a type or domain the statement (without IF EXISTS) may fail | E1, E4, E6 |
| `M6.P1` | DROP TABLE: each name the set creates must be held (without IF EXISTS) | E1 |
| `M7.P1` | DROP INDEX: a set name or a generated-name shape of a set table must be held (without IF EXISTS); E1 does not cover indexes, so any other name is not proven (Revision 1.5 H12) | E1 |
| `M5f-u.P1` | USING INDEX: the index held under a certain name on the table, with its columns, method, predicate, expressions, ordering, NULLS NOT DISTINCT and validity known (Revision 1.5 U1) | E1 |
| `M2.P1` | a written type exists: a pg_catalog type of pg_type.dat or a gram.y SQL type name, an array of one, a type the set holds, or an E1 type of contrib; COLLATE names default, C, POSIX, ucs_basic, pg_c_utf8 or a collation the set holds; anything else may fail (Revision 1.5 U7); Revision 1.6 V13: serial and its aliases are a type only in a column definition of CREATE TABLE or ADD COLUMN, anywhere else (composite attributes, domains, ALTER COLUMN TYPE, routine signatures) refused (42704); the built-in collations ucs_basic and pg_c_utf8 are proven under E4's UTF8 database only; Revision 1.6 D2: every type of a routine signature (parameters, RETURNS [SETOF], RETURNS TABLE), a pg_catalog pseudo-type there included | E1, E4 |
| `N2.P1` | SET/RESET: the name is a parameter of guc_tables.c (unknown one-part names refused) or a two-part placeholder whose prefix no module reserves (Revision 1.5 U7) | E1 |
| `M8.P1` | ALTER INDEX: a set name or a generated-name shape of a set table must be held (without IF EXISTS); Revision 1.6 V4: every ALTER INDEX form of a name the pass does not hold as an index may fail (E1 does not cover indexes); Revision 1.6 V4 (kinds, 2026-11-07): a name held as a relation of another kind is refused also with IF EXISTS (42809; RENAME accepts any relation but a composite type), a name held only as a type or domain may fail without IF EXISTS, and SET LOGGED, UNLOGGED, WITHOUT CLUSTER, WITHOUT OIDS and ACCESS METHOD are refused on an index and, with IF EXISTS, may fail on a name not held | E1, E4 |
| `M9.P1` | ALTER SCHEMA: a schema the set creates must be held | E1 |
| `M9.P2` | ALTER SCHEMA s RENAME TO n: refused (42P06) when n is a schema the set holds, public or information_schema under E5 unless the set dropped it, pg_catalog or pg_toast; refused (42939) when n starts with pg_; may fail when code or CREATE SCHEMA AUTHORIZATION of an unknown role may have created n (Revision 1.9 Y06, Y08) | E3, E5 |
| `R5.P1` | DROP SCHEMA: a schema the set creates must be held (without IF EXISTS); Revision 1.6 V4: DROP SEQUENCE, VIEW, MATERIALIZED VIEW, TYPE and DOMAIN of a name the pass does not hold (without IF EXISTS) may fail; Revision 1.7 W5: DROP COLLATION of a name the N1.P2 record does not hold (or dropped since) and DROP AGGREGATE (without IF EXISTS) may fail; Revision 1.8 X2: after ALTER COLLATION a RENAME TO b the pass holds b and not a | E1 |
| `N3.P1` | DML and queries (Revision 1.2): every column reference proven on a relation whose columns are known; INSERT targets and VALUES widths, SET targets, COPY and ANALYZE column lists; anything the reader does not prove may fail; Revision 1.6 V4: REFRESH MATERIALIZED VIEW, REINDEX INDEX and CLUSTER ... USING of a name not held as that kind may fail | E1 |
| `N1.P1` | token rule for N1, N3, R1, R1b, R4, R5, R6 and O1 statements: an identifier equal to a name the set creates must be held; Revision 1.6 V4: COMMENT ON INDEX, SEQUENCE, VIEW or MATERIALIZED VIEW of a name not held, COMMENT ON CONSTRAINT and GRANT/REVOKE ON SEQUENCE of a name not held may fail; Revision 1.7 W1 (with the D1 doc fix): DROP TRIGGER, POLICY or RULE (without IF EXISTS), ALTER POLICY and COMMENT ON TRIGGER, POLICY or RULE may fail on a table outside the set always, and on a table the set holds unless the pass holds the name there (the table resolves certainly with its facts known and its record of names of the kind is exact: a name created by code is only possibly held; the pass holds no rule); W4: ALTER DATABASE, GRANT/REVOKE ON DATABASE and ALTER ROLE ... IN DATABASE may fail (E1 does not list databases, the database name is unknown; COMMENT ON DATABASE never refuses); W5: DROP PUBLICATION of a name the N1.P2 record does not hold, and DROP STATISTICS, OPERATOR and CAST (without IF EXISTS), may fail; Revision 1.8 X2: a collation name renamed away is no longer held; X4: CREATE SCHEMA AUTHORIZATION r without a schema name creates schema r, and after one with CURRENT_USER, SESSION_USER or CURRENT_ROLE (a schema named after the unknown role) a statement naming a schema the pass does not hold (other than public, information_schema and pg_ names) and CREATE SCHEMA without IF NOT EXISTS may fail | E1, E3, E4, E5 |
| `R3.P1` | ALTER TYPE: a type the set creates must be held; Revision 1.6 V4 (kinds, 2026-11-07): ADD VALUE and RENAME VALUE of a table's row type are refused, and of a name the pass does not hold as a type may fail (E1 does not cover enums) | E1, E4 |
| `R4.P1` | ALTER VIEW/SEQUENCE/MATERIALIZED VIEW/TYPE of a relation the set created: its existence and kind must still be proven (no opaque statement or CASCADE since); Revision 1.6 V4: ALTER VIEW, ALTER MATERIALIZED VIEW and ALTER SEQUENCE of a name the pass does not hold as that kind (without IF EXISTS) may fail; Revision 1.6 V4 (kinds, 2026-11-07): ALTER DOMAIN of a name held as another kind, ALTER TYPE ... ADD/DROP/ALTER/RENAME ATTRIBUTE of a name held as anything but a composite type, and other ALTER TYPE forms of a relation's row type are refused (a sequence or index: may fail); ALTER VIEW / MATERIALIZED VIEW ... RENAME [COLUMN] and ALTER TYPE ... RENAME ATTRIBUTE of a held name may fail (columns not tracked); ALTER DOMAIN ... DROP CONSTRAINT (without IF EXISTS), VALIDATE CONSTRAINT and RENAME CONSTRAINT may fail (E1 does not cover constraints); ALTER TYPE (other than ADD / RENAME VALUE) and ALTER DOMAIN of a name the pass does not hold as a type may fail (E1 does not cover types other than its own) | E1, E4, E6 |
| `M1.P2` | CREATE TABLE: index-backed constraint names need a free pg_class name; constraint names are unique per table across every kind, and a written name that may be the generated name of an unnamed constraint of the statement may fail (Revision 1.4 R2); a column named ctid, xmin, cmin, xmax, cmax or tableoid is refused (Revision 1.5 U10); Revision 1.9: a table created in pg_catalog or pg_toast refused (42501, Y07); a statement that creates a pg_type entry named like a multirange type the set holds refused (42710, Y21) | - |
| `M5a.P2` | ADD COLUMN: as M1.P2, and the column's constraint names must not be held on the table (Revision 1.4 R2); a system column name is refused, also under IF NOT EXISTS (Revision 1.5 U10) | - |
| `M5h.P2` | RENAME COLUMN to a system column name is refused (Revision 1.5 U10); Revision 1.9 Y27: RENAME COLUMN a TO b without ONLY refused (42701) when an inheritance child holds b, may fail when a child's columns are not held | - |
| `M5f-u.P2` | USING INDEX: the constraint name free among the table's constraints and, when it differs from the index name, free in the pg_class namespace (Revision 1.5 U1) | - |
| `M5f.P2` | ADD CONSTRAINT: an index-backed constraint name needs a free pg_class name; a name held by any constraint of the table is refused, one the table may hold may fail (Revision 1.4 R2) | - |
| `M8.P2` | ALTER INDEX RENAME TO: the new name needs a free pg_class name; generated names keep their creation-time form; the index of a constraint takes a name no other constraint of its table holds (Revision 1.4 R2) | - |
| `O1.P1` | a string argument that spells a relation name (a reg* parameter may read it) follows P1 (B2, g5-s80); Revision 1.7 W2: a string literal bound (by position, by name, or as a VARIADIC element) to a reg* parameter of a set-defined routine, or to a parameter whose type the pass does not know or that is outside pg_catalog, is proven only as a regclass argument naming a table, view, materialized view or sequence the pass holds under a certain name (search_path known); otherwise the statement may fail; Revision 1.9 Y49: the static SELECT, PERFORM, INSERT, UPDATE, DELETE, VALUES and WITH statements of a DO body and of a set PL/pgSQL routine the statement may call are checked where they run with the top-level rules for names (N1.P1), columns (N3.P1) and reg* arguments (W2), and a failure there makes the running statement may fail; Y50: a call of a set PL/pgSQL routine (also a data statement that may fire a set trigger) may fail when, since the routine's creation or last CREATE OR REPLACE, a statement the pass applied dropped, renamed or changed the type of an object or column a word of its body names | E1, E3, E4 |
| `N1.P2` | CREATE TRIGGER (and O1t) and CREATE POLICY: the name must be free on the table; exact on a table whose facts are known, not proven on another table the set holds, the set's own record of names on a table it does not hold; CREATE ROLE/USER/GROUP, PUBLICATION, COLLATION, AGGREGATE, OPERATOR, CAST: a name the set created earlier and no read DROP or RENAME freed is refused (roles, publications) or not proven (the others, and every kind an unread RENAME may have used) (Revision 1.4); Revision 1.9 Y06: CREATE ROLE, USER or GROUP of a pg_ name, and ALTER ROLE ... RENAME from or to a pg_ name, refused (42939); Revision 1.9 Y13: the membership edges the set creates (GRANT r TO m, CREATE ROLE ... IN ROLE, ROLE, USER, ADMIN, ALTER GROUP ... ADD USER) and removes (REVOKE, ALTER GROUP ... DROP USER, DROP ROLE), with the migrating role (E6) a member of every role the set creates: making m a member of r is refused (0LP01) when r = m or the edges make r a member of m; may fail when r's memberships and m's members both include a role outside the set, after GRANTED BY, or when code may change memberships; Revision 1.9 (E6): while SET ROLE puts a role the set created in force, every statement but SET, RESET, SHOW, DISCARD and transaction control may fail (the role's privileges are not the migrating role's) | E1, E3, E6 |
| `M1.P3` | CREATE TABLE IF NOT EXISTS whose skip depends on an unknown writes no known facts | - |
| `M4.P2` | CREATE INDEX on a table the pass does not hold that the set changed earlier (an index created on it, ALTER TABLE): its names and columns are not proven (Revision 1.4); Revision 1.9 Y07: an index on a relation in pg_catalog or pg_toast refused (42501) | E1 |
| `M5a.P3` | ADD COLUMN IF NOT EXISTS on a table whose facts are not known writes no known facts, and its constraints and expressions are not proven (Revision 1.4) | - |
| `M4.P3` | CREATE INDEX IF NOT EXISTS whose skip depends on an unknown writes no index | - |
| `M7.P3` | DROP INDEX IF EXISTS under an uncertain qualification: the index it may mean is no longer a fact | - |
| `M1.P4` | CREATE TEMP TABLE with a foreign key: the referenced table's persistence is not proven; Revision 1.4 R3: a column definition is applied only in the allowlisted clause combinations (one DEFAULT, serial or identity; identity on smallint, integer or bigint without options; no generation expression with a default or identity; one PRIMARY KEY and COLLATE; COLLATE on a collatable type; no STORAGE or COMPRESSION), key columns written once, no table options other than TABLESPACE and WITH storage parameters proven by reloptions.c for a heap table (refused on a partitioned one), a key's WITH (...) proven for btree (Revision 1.5 U9), ON COMMIT refused, PARTITION BY RANGE or LIST over plain btree columns that every key includes (Revision 1.5 U4: as every unique index on a partitioned table) | - |
| `M1b.P4` | CREATE TABLE ... PARTITION OF p ... PARTITION BY: the new partition receives p's unique indexes and keys, each of which must include its partition key (RANGE or LIST over plain columns); p's unique indexes not known: not proven (Revision 1.5 U4); Revision 1.9 Y28: PARTITION OF a table the pass holds under a certain name as not partitioned refused (42P17), may fail when the name or its partitioning is not certain | - |
| `M1b.P5` | Partition bounds (Revision 1.9 Y29; parse_utilcmd.c transformPartitionBound, partbounds.c check_new_partition_bound): for a partitioned table the set created, the bounds of its partitions created by PARTITION OF or ATTACH PARTITION, until DETACH or the partition's drop; a bound of the wrong kind for the strategy, DEFAULT on a hash-partitioned table, a hash remainder not below its modulus, FROM or TO with more than one value for a one-column key (42P16), a second DEFAULT, an empty range, a list value or a range another partition holds, a hash modulus that is not a factor or multiple of the others or a hash remainder another holds (42P17), a NULL range bound (42601) and a string without a digit for an integer key (22P02) refused, for one-column keys of integer types, text and varchar (lists), date (ISO literals) and boolean with constant bounds (MINVALUE, MAXVALUE); other key types, multi-column and expression keys, values not proven, a table whose partitioning is not held, partitions code may have created, and a new partition while the default partition may hold rows the set wrote may fail | E2, E3 |
| `M4.P4` | CREATE INDEX WITH (...) and ALTER INDEX SET/RESET (...): storage parameters proven by reloptions.c for the access method; unknown names, repeats and values out of bounds refused, fractions for integer options and other forms not proven (Revision 1.5 U9); CREATE UNIQUE INDEX on a partitioned table (or one with partitioned partitions): btree, every partition key column a bare key column (not INCLUDE), else refused; a column with an operator class or COLLATE, or a table whose partitioning is not known, not proven (Revision 1.5 U4); Revision 1.6 V9: a written operator class of pg_opclass.dat for the method must accept the column type (equal to opcintype, binary-coercible, or polymorphically matched), a proven pg_catalog type it does not accept refused (42804), an E1 class as its extension declares, otherwise may fail; a UNIQUE index of a method other than btree refused (0A000, amcanunique); Revision 1.9 Y25: the access method's capabilities (amcaps.go, generated from the REL_17_6 handlers): UNIQUE, more than one key column, INCLUDE and ASC, DESC or NULLS FIRST / LAST where the method lacks them (0A000), more than 32 key and INCLUDE columns (54011), in CREATE INDEX and in EXCLUDE (which also needs amgettuple); an access method outside the core six may fail | E1 |
| `M5f-u.P4` | USING INDEX: a unique, non-partial btree index over plain columns with default ordering, not built CONCURRENTLY, no constraint attributes, not NULLS NOT DISTINCT under PRIMARY KEY, no second primary key, the table not partitioned; a property the records prove false is refused, one they do not prove may fail (Revision 1.5 U1) | E3 |
| `M5a.P4` | ADD COLUMN: the column definition as M1.P4 (Revision 1.4 R3); a PRIMARY KEY or UNIQUE column on a partitioned table as M4.P4's unique indexes (Revision 1.5 U4) | - |
| `M5f.P4` | ADD CONSTRAINT: key columns written once (Revision 1.4 R3); PRIMARY KEY and UNIQUE on a partitioned table as M4.P4's unique indexes, EXCLUDE on one not proven (Revision 1.5 U4) | - |
| `M3.P4` | REFERENCES: a permanent table cannot reference an unlogged one; Revision 1.4 R3: MATCH PARTIAL and an ON UPDATE column list are refused, an ON DELETE SET column list names key columns only, a generated key column takes no action that writes it | - |
| `M5k-a.P4` | SET LOGGED/UNLOGGED: foreign-key persistence pairs must be proven; Revision 1.9 Y30: ALTER INDEX SET (...) or RESET (...) of a partitioned index refused (42809) | - |
| `M5k-b.P4` | ALTER TABLE words that depend on an object or option the pass does not track may fail; identity needs an integer type; Revision 1.9 Y51: ENABLE or DISABLE [REPLICA \| ALWAYS] TRIGGER ALL, or of a system trigger, may fail (a table's internal triggers need a superuser); E6: OWNER TO a role the set created may fail | E6 |
| `N2.P4` | SET/RESET: the parameter's context is user; superuser contexts may fail; postmaster, sighup, internal, backend and superuser-backend are refused (Revision 1.5 U7); Revision 1.9 Y04: SET CONSTRAINTS names {DEFERRED \| IMMEDIATE} (trigger.c AfterTriggerSetState): a name that is not a ColId or a statement that is not ALL or a name list followed by the mode refused (42601); an unqualified name held with its deferrability read on set tables whose constraint names are exact (C3), with E4's search_path, no temporary table and no domain, constraint trigger or code that may hold the name: DEFERRED refused (42809) when one held constraint of the name is not deferrable (FOREIGN KEY, UNIQUE, PRIMARY KEY or EXCLUDE without DEFERRABLE, any CHECK), claimed otherwise, IMMEDIATE claimed; any other name may fail; SET CONSTRAINTS ALL claimed; Revision 1.9 Y51: ALTER ROLE ... SET or RESET, a routine's SET clause and ALTER FUNCTION ... SET of a two-part placeholder parameter (a stored setting checks it as superuser-only), and SET SESSION AUTHORIZATION of a role other than the session's, may fail | E3, E4, E6 |
| `N1.P4` | SECURITY LABEL needs a loaded label provider (not granted by E1-E4); ALTER SEQUENCE options as R1.P4 over the sequence's current parameters (a sequence created with options the pass read, or a serial or identity column's sequence; otherwise only the checks that need no current value), bounds that narrow over an unknown last value not proven, OWNED BY of an identity sequence refused (Revision 1.5 U5); Revision 1.9: CREATE TRIGGER (and O1t): a set routine the pass read with one definition that is a procedure or takes arguments (42883) or does not return trigger (42P17) refused, several definitions or an unread header may fail (Y09; T024c1b: public.f names the set's unqualified f created under E4's search_path); the relation kind as CreateTriggerFiringOn (table and partitioned table BEFORE or AFTER; view INSTEAD OF FOR EACH ROW or BEFORE/AFTER FOR EACH STATEMENT, no TRUNCATE, no transition table; foreign table BEFORE or AFTER, no constraint trigger, no transition table; any other kind refused; 42809, 0A000), a kind not certain may fail (Y10); TRUNCATE FOR EACH ROW, INSTEAD OF without FOR EACH ROW or with WHEN or a column list, ROW variables, and transition tables on a constraint trigger, not AFTER, with TRUNCATE, more than one event, a column list, NEW without INSERT or UPDATE, OLD without DELETE or UPDATE, twice, or under one name, or FOR EACH ROW on a partitioned table, partition or inheritance child, refused (Y11); CREATE and ALTER POLICY: INSERT with USING, SELECT or DELETE with WITH CHECK (42601; ALTER by the command the pass recorded), a relation other than a table or partitioned table (42809) refused (Y12); CREATE and ALTER PUBLICATION: an unknown or repeated option or publish value (42601), FOR, ADD or SET TABLE of a relation other than a table or partitioned table or of an unlogged table (22023) refused, a possibly temporary table may fail (Y31); CREATE COLLATION: an unknown or repeated attribute, LOCALE with LC_COLLATE or LC_CTYPE, FROM with another option (42601), an unknown provider, a deterministic value that is no boolean, a missing locale (builtin, icu) or lc_collate or lc_ctype (libc), a nondeterministic collation or rules with a provider other than icu, refused (Y35); GRANT ... TO PUBLIC WITH GRANT OPTION, also in ALTER DEFAULT PRIVILEGES, refused (0LP01, Y37); COMMENT ON TABLE, VIEW, MATERIALIZED VIEW, SEQUENCE, FOREIGN TABLE, INDEX, TYPE or DOMAIN of a name the pass holds as a kind the word does not take (TYPE: a sequence or index) refused (42809, 42704), followed through renames and CREATE TABLE IF NOT EXISTS, a kind not certain may fail (Y42); Revision 1.9 Y51 (D-superuser, E6; superuserForms): CREATE ROLE, USER or GROUP with SUPERUSER, ALTER ROLE with SUPERUSER or NOSUPERUSER, ALTER ROLE ALL ... SET, CREATE CAST ... WITHOUT FUNCTION, CREATE LANGUAGE, CREATE OPERATOR CLASS or FAMILY, ALTER OPERATOR FAMILY, CREATE TEXT SEARCH PARSER or TEMPLATE, CREATE ACCESS METHOD, CREATE or ALTER FOREIGN DATA WRAPPER, CREATE PUBLICATION FOR ALL TABLES or TABLES IN SCHEMA and ALTER PUBLICATION ADD or SET TABLES IN SCHEMA may fail (only a superuser may run them); E6: CREATE CAST may fail unless the set created its source or target type or it is an E1 type (the migrating role must own one), and ALTER ... OWNER TO a role the set created may fail (the new owner needs CREATE on the schema) | E1, E4, E5, E6 |
| `R1.P4` | CREATE SEQUENCE options: each written once (42601), signed integer constants in int8 range, AS smallint, integer or bigint, and the parameters sequence.c init_params computes (INCREMENT not 0, MINVALUE < MAXVALUE within the type's range, START and RESTART within the bounds, CACHE >= 1; 22023); OWNED BY NONE or a column of a table held under a certain name in the sequence's schema (55000, 42703); other values not proven (Revision 1.5 U5); Revision 1.9 Y51: CREATE TYPE of a base or shell type (CREATE TYPE name, alone or with a parameter list) may fail (only a superuser may create one) | E6 |
| `R4.P4` | ALTER TYPE ... SET (...) may fail: it changes a base type's properties, which only a superuser may do (Revision 1.9 Y51; typecmds.c 4493); E6: ALTER TYPE or DOMAIN ... OWNER TO a role the set created may fail | E3, E6 |
| `R5.P4` | DROP <kind> of an object held as another kind is refused; Revision 1.6 V4 (kinds, 2026-11-07): DROP INDEX [IF EXISTS] of a name held as another relation kind is refused (42809) | E4 |
| `R6.P4` | functions and procedures: the kind a statement names must be the kind the set holds; Revision 1.6 D2: the options of CREATE FUNCTION/PROCEDURE at most once each except SET (42601), volatility, STRICT, LEAKPROOF, COST, ROWS and PARALLEL refused for a procedure (42P13), COST and ROWS positive constants, ROWS only for a set-returning function (22023), PARALLEL SAFE, RESTRICTED or UNSAFE, a SET clause proven as N2.P5 proves SET; TRANSFORM, WINDOW, SUPPORT and options not read may fail; Revision 1.9 Y16: the actions of ALTER FUNCTION, PROCEDURE and ROUTINE as the CREATE options (an option twice, function attributes on a procedure, COST and ROWS, PARALLEL), against the one held definition when the pass read it; ROWS, and a ROUTINE's function attributes, on a routine not read may fail; Revision 1.9 Y51: a function or procedure with LEAKPROOF or SUPPORT, or in a language other than plpgsql and sql (c, internal, the untrusted procedural languages), may fail (only a superuser may create it); E6: ALTER FUNCTION, PROCEDURE, ROUTINE or AGGREGATE ... OWNER TO a role the set created may fail | E6 |
| `R6.P5` | CREATE FUNCTION and PROCEDURE: a polymorphic result or OUT parameter that no input determines refused (42P13; check_valid_polymorphic_signature: anyrange and anymultirange need an anyrange or anymultirange input, the other anyelement-family types any of that family, anycompatiblerange and anycompatiblemultirange an input of those two, the other anycompatible-family types any of that family, internal an internal input; Y17); a type modifier on an argument, OUT, RETURNS or RETURNS TABLE type as M2.P5 (42601 when the type takes none; Y18); the SET values of ALTER FUNCTION, PROCEDURE and ROUTINE as N2.P1 and N2.P5 (Y15) (Revision 1.9) | - |
| `R6.P3` | CREATE OR REPLACE FUNCTION/PROCEDURE over a held routine of the same identity (schema, name, input types of pg_catalog): kind (42809), return type and SETOF, OUT/INOUT/TABLE columns, named input parameters, and at least its defaults kept (42P13), else refused; an identity the pass cannot decide (types outside pg_catalog, a schema not proven, a routine changed by a statement not read) not proven (Revision 1.5 U6) | E1 |
| `R6.P2` | ALTER FUNCTION, PROCEDURE or ROUTINE ... RENAME TO g: refused (42723) when the set holds g with the same argument types in the same schema; may fail when the argument types or the held routines are not proven (Revision 1.9 Y16) | - |
| `M1.P5` | CREATE TABLE: DEFAULT result types assignable to the column; generation expressions IMMUTABLE and typed; CHECK expressions proven boolean over held columns with proven functions and operators, EXCLUDE only btree WITH = or gist over ranges (Revision 1.4 R1); Revision 1.6 V3: a generation expression, partition key or EXCLUDE element holding a SQL value function written as a keyword refused (42P17); Revision 1.9 Y46: a DEFAULT that is NULL, (NULL), or NULL cast (::, CAST) any number of times to unknown or to the column's type, with or without COLLATE, on a column whose type is not a domain and has no modifier, is stored as no default (atthasdef false) | - |
| `M2.P5` | column types: modifiers that the type's typmodin accepts; a time precision above 6 is recorded as 6; other modifiers are not proven (Revision 1.4 R4) | - |
| `M4.P5` | CREATE INDEX: expression elements and the predicate IMMUTABLE and typed (Revision 1.1); Revision 1.6 V3: an element or the predicate holding a SQL value function written as a keyword (user, current_user, ...: STABLE) refused (42P17) | - |
| `M5a.P5` | ADD COLUMN: as M1.P5; Revision 1.9 Y46: the NULL defaults of M1.P5 | - |
| `M5f.P5` | ADD CONSTRAINT: CHECK and EXCLUDE as M1.P5 (Revision 1.4 R1) | - |
| `M5f.P7` | constraint attributes in CREATE TABLE and ALTER TABLE ADD: NOT DEFERRABLE with INITIALLY DEFERRED and conflicting attribute words refused (42601); DEFERRABLE or INITIALLY DEFERRED on CHECK, NOT VALID on UNIQUE, PRIMARY KEY or EXCLUDE, NO INHERIT on any but CHECK refused (0A000, processCASbits); a column constraint's DEFERRABLE, NOT DEFERRABLE and INITIALLY words only after PRIMARY KEY, UNIQUE or REFERENCES, once each (42601); NOT VALID on a column constraint, and NO INHERIT after one other than CHECK, refused (42601) (Revision 1.9 Y26) | - |
| `M5c.P5` | ALTER COLUMN TYPE: an assignment cast without USING, a proven USING result, the default re-coerced; Revision 1.9 Y01: the new type of an identity column must be smallint, integer or bigint after alias normalisation (22023; a domain over them too) | - |
| `M5e.P5` | SET DEFAULT: the result type assignable to the column; Revision 1.9 Y46: SET DEFAULT of a NULL form M1.P5 lists removes the default | - |
| `N1.P5` | COMMENT ON COLUMN, CREATE TRIGGER ... UPDATE OF and GRANT/REVOKE (cols): the columns held on a table whose facts are known; CREATE STATISTICS, a trigger's WHEN and a policy's USING/WITH CHECK (other than TRUE or FALSE) not proven (Revision 1.3); Revision 1.9: CREATE and ALTER ROLE VALID UNTIL proven only for 'infinity', '-infinity' and YYYY-MM-DD[ HH:MM[:SS[.frac]]][zone] with each field in range, otherwise may fail (Y14); ALTER ROLE [IN DATABASE] ... SET and RESET as N2.P1 and N2.P5 (Y15) | - |
| `N3.P5` | DML values (Revision 1.3): every written value, default, condition and RETURNING item in the V1-V4 allowlist (literals by the B2 grammars), and no constraint, trigger or per-row expression the write may meet whose outcome depends on rows or values the pass does not prove; B2: V5 a single row into a table proven empty meets its keys, ON CONFLICT DO NOTHING arbiters (no deferrable key), a MATCH SIMPLE foreign key with a NULL column; V6 a fresh sequence's first value; Revision 1.9 Y23: a VALUES list of two or more rows that is a query (a VALUES statement, a parenthesised VALUES, CREATE TABLE or VIEW ... AS VALUES; not INSERT's VALUES, whose rows are coerced to the target columns one by one): each column's common type is resolved as select_common_type does from constants (integers as int4, int8 or numeric by size, decimals as numeric, TRUE and FALSE as boolean, a :: cast to int2, int4, int8, numeric, text or boolean; untyped strings and NULL alone are text): types of different categories refused (42804), an untyped string the common type's input does not prove refused or may fail (22P02), rows of different lengths refused (42601), an entry that is not such a constant may fail | E3 |
| `N3.P4` | REFRESH MATERIALIZED VIEW CONCURRENTLY (Revision 1.9 Y24; matview.c RefreshMatViewByOid): with WITH NO DATA refused (42601); of a materialized view the set created WITH NO DATA or last refreshed WITH NO DATA refused (0A000); of one whose index record is exact (no index of a generated name dropped, no IF NOT EXISTS skip, no code that may create or drop an index) with no unique btree index without a predicate on plain columns refused (55000); claimed when it is populated and holds such an index; a materialized view the pass does not hold, or an inexact record, may fail | E1, E3 |
| `N3.P6` | REINDEX INDEX of a partitioned index and REINDEX TABLE of a partitioned table inside a transaction block refused (25001); a table whose kind is not proven may fail (Revision 1.9 Y30) | - |
| `R1.P5` | CREATE DOMAIN: the base type's modifiers as M2.P5, one DEFAULT assignable to the base type, not NULL with NOT NULL, COLLATE on a collatable base type, each CHECK proven boolean over VALUE; other clauses not proven; CREATE TYPE AS (...): attribute type modifiers and collations as for columns (Revision 1.4 R1, R3, R4); Revision 1.6 V12: CREATE VIEW WITH (...) checked against the view options of reloptions.c, CREATE MATERIALIZED VIEW WITH (...) against the heap ones; a check_option or WITH CHECK OPTION may fail (the pass does not prove a view automatically updatable); Revision 1.9 Y20: a composite type's attribute named twice (42701) and a domain's constraint name written twice (42710) refused | - |
| `M5k-a.P5` | ALTER TABLE SET/RESET (...): storage parameters proven by reloptions.c for a table whose kind is known (heap; any parameter on a partitioned table refused); RESET of any name without a value accepted (Revision 1.5 U9); Revision 1.6 V12: on a relation held as a view or materialized view, against the view or heap options | - |
| `N2.P5` | SET: the value proven for the parameter's type (bool words, integer and real constants within bounds, enum values; a parameter with a check hook only by its closed table; T024c1b: search_path any value, check_search_path checks only the list syntax, which flatten_set_variable_args's quoting makes valid), a certain violation refused (Revision 1.5 U7); Revision 1.6 V10: SET NAMES and SET client_encoding: DEFAULT and the UTF8 names of encnames.c (compared as clean_encoding_name) proven, an unknown name refused (22023), any other encoding may fail and makes later migrations O4 | E4 |
| `N2.P6` | Transaction modes of sqlx's transaction (Revision 1.9 Y03; variable.c check_transaction_read_only, check_transaction_isolation, check_transaction_deferrable; pquery.c PlannedStmtRequiresSnapshot; utility.c PreventCommandIfReadOnly): SET TRANSACTION ISOLATION LEVEL (and transaction_isolation) to a level other than the current one refused (25001) after a statement that took a snapshot (every statement but transaction control, LOCK, SET, RESET, SHOW, SET CONSTRAINTS, FETCH, MOVE, LISTEN, NOTIFY, UNLISTEN, CHECKPOINT) or inside a savepoint; [NOT] DEFERRABLE refused after a snapshot or inside a savepoint; READ WRITE in a read-only transaction refused after a snapshot or inside a savepoint; SET TRANSACTION SNAPSHOT refused after a snapshot (25001) or under READ COMMITTED (0A000), otherwise may fail; in a read-only transaction DDL, DML and every other statement PreventCommandIfReadOnly guards refused (25006), a query, CALL, DO, EXECUTE, EXPLAIN, DECLARE or COPY may fail; in a no-transaction migration these statements may fail | E3, E4 |
| `N2.P13` | Session transaction defaults (Revision 1.9 Y03): SET (not SET LOCAL) of default_transaction_read_only, default_transaction_isolation and default_transaction_deferrable, SET SESSION CHARACTERISTICS AS TRANSACTION, their RESET and RESET ALL (E4's values) set the modes every later transaction starts with, from the next transaction on (after a COMMIT in the string, and every later migration); a value not read makes them unknown, and a statement Y03 judges then may fail; in a no-transaction migration these statements may fail | E4 |
| `R1b.P5` | the query of a view, CREATE TABLE AS or SELECT INTO, and a LANGUAGE sql body, are not proven; a LANGUAGE sql AS body only while check_function_bodies is on or unknown (Revision 1.6 V1: off, the validator does not parse it); T024c1c: a LANGUAGE sql function's AS body that the validator parse-analyses at creation (fmgr_sql_validator) is proven in one form: named parameters and a result of pg_catalog base types, options LANGUAGE, volatility, STRICT, LEAKPROOF, SECURITY and PARALLEL only, one SELECT of one output expression of the RETURNS type over at most one table held with known columns under a certain name ([AS] alias) with an optional WHERE; a name is a column of its level's table before a parameter (an outer level's column is not proven); EXISTS of the same form, one integer subscript of a parenthesised array, pg_catalog functions the set does not overload and the platform functions of SB-M8; never after an opaque statement, an operator, cast, operator class or family, access method or aggregate of the set, or a statement naming row_security | E3, E4 |
| `T1.P6` | transaction control: the outcome in each block state (the P6 table in transactions.go) | - |
| `T1.P4` | LOCK [TABLE]: a name the pass holds as a materialized view, foreign table, sequence, index or composite type is refused (42809); only a table, a partitioned table and a view (with its base relations, whatever their kinds) are locked; a kind not certain may fail (Revision 1.9 Y05) | - |
| `M4.P6` | CREATE INDEX CONCURRENTLY is refused in any block; outside one (C4) it is refused on a partitioned table and not proven on a table the pass does not hold | E1 |
| `M7.P6` | DROP INDEX CONCURRENTLY is refused in any block; outside one (C4) it drops one index without CASCADE, is refused on a partitioned table and is not proven for an index the pass does not hold | E1 |
| `F1.P6` | statements PreventInTransactionBlock refuses in any block | - |
| `M4.P7` | CREATE INDEX IF NOT EXISTS needs a name; a keyword its name position does not accept (Revision 1.5 U8); Revision 1.6 V3: index elements, operator classes and expression column references follow the keyword categories of index_elem, opt_qualified_name and columnref | - |
| `M1.P7` | CREATE TABLE (also M1b): an unquoted keyword in a name position whose gram.y nonterminal (ColId, type_function_name, ColLabel) does not accept its kwlist.h category is a syntax error (Revision 1.5 U8); Revision 1.6 V3: partition keys, EXCLUDE elements, CHECK and generation expressions likewise (part_elem, ExclusionConstraintElem, columnref); Revision 1.9 Y26: constraint attributes of CREATE TABLE as M5f.P7 | - |
| `M5a.P7` | ALTER TABLE (every M5 row): a keyword a name position does not accept, and a storage parameter list item that is not name [= value] (Revision 1.5 U8, U9); Revision 1.6 V3: CHECK, generation and EXCLUDE expressions of ADD likewise | - |
| `M8.P7` | ALTER INDEX: a keyword a name position does not accept (Revision 1.5 U8); Revision 1.6 V7: ALTER INDEX forms other than RENAME TO and SET / RESET (...) are row D | - |
| `R2.P7` | CREATE TYPE: a keyword a name position does not accept (Revision 1.5 U8); the whole statement read as CREATE TYPE n AS ENUM ( [Sconst [, ...]] ), otherwise row D (Revision 1.6 V7) | - |
| `R2.P5` | CREATE TYPE AS ENUM: a label written twice (23505) or longer than 63 bytes (42602) refused; a label the pass does not decode may fail (Revision 1.9 Y19) | - |
| `R6.P7` | CREATE/ALTER FUNCTION/PROCEDURE: an unqualified routine name is a type_function_name (no col_name or reserved keyword) (Revision 1.5 U8); ALTER FUNCTION/PROCEDURE/ROUTINE read whole as RENAME TO, OWNER TO, SET SCHEMA, [NO] DEPENDS ON EXTENSION or alterfunc_opt_list [RESTRICT], otherwise row D (Revision 1.6 V7) | E1, E6 |
| `N1.P7` | N1 statements: a keyword a name position does not accept (Revision 1.5 U8); the whole statement read by one of the N1 productions of Revision 1.6 V7 (COMMENT, GRANT/REVOKE, ALTER DEFAULT PRIVILEGES, roles, CREATE TRIGGER/POLICY/COLLATION/STATISTICS/PUBLICATION/OPERATOR/CAST/AGGREGATE, NOTIFY/LISTEN/UNLISTEN, CHECKPOINT, SECURITY LABEL, REASSIGN OWNED, DROP TRIGGER/POLICY/RULE, ALTER POLICY/TRIGGER/GROUP/DATABASE/SEQUENCE, other DROP kinds), otherwise row D; Revision 1.9: CREATE and ALTER ROLE (USER, GROUP) options given twice or conflicting (42601) and CONNECTION LIMIT below -1 (22023) refused (Y14); ALTER DEFAULT PRIVILEGES with IN SCHEMA or FOR ROLE given twice refused (42601, Y36) | E6 |
| `N1.P8` | DROP ROLE, USER or GROUP of a role the set created (Revision 1.9 Y38; user.c DropRole, checkSharedDependencies): refused (2BP01) while the set gave it a live shared dependent: a table grant until REVOKE of the same privileges (REVOKE ALL included) or the table's drop, ownership by OWNER TO (a set table's until it is dropped or owned by another role), CREATE SCHEMA ... AUTHORIZATION, policies TO it until DROP POLICY or the table's drop, ALTER DEFAULT PRIVILEGES, any other grant, and any statement while SET ROLE it was in force; DROP OWNED BY ends them, REASSIGN OWNED BY the owned ones; refused (55006) while it is the current role; may fail after SET SESSION AUTHORIZATION, code that may create, own or grant, or a statement naming it that the pass does not read; memberships, settings and comments are not dependents | E1, E3, E4, E6 |
| `M5k-b.P7` | RENAME, SET SCHEMA and ATTACH/DETACH PARTITION are whole statements, not list items; the subcommand read whole by its alter_table_cmd or partition_cmd production, otherwise row D (Revision 1.6 V7) | - |
| `N3.P7` | SELECT INTO: no DISTINCT or target after the target; INTO is no alias; no INTO in data-changing statements; a keyword a name position of a column list does not accept (Revision 1.5 U8); CLUSTER, REINDEX, REFRESH MATERIALIZED VIEW, TABLE and COPY (name, columns, direction, file or STDIN/STDOUT/PROGRAM, the option list) read whole, otherwise row D (Revision 1.6 V7) | - |
| `R1.P7` | no SELECT INTO in a view's or CREATE TABLE AS's query; a keyword a name position does not accept (Revision 1.5 U8); CREATE SCHEMA read whole (name, AUTHORIZATION, elements by their own rows' readers) and the shell, composite and range forms of CREATE TYPE, otherwise row D (Revision 1.6 V7) | E6 |
| `M1b.P7` | CREATE TABLE ... LIKE / OF / PARTITION OF / INHERITS read whole (elements, bound, OptPartitionSpec, USING, WITH, ON COMMIT, TABLESPACE in gram.y's order), otherwise row D (Revision 1.6 V7) | - |
| `M5e.P7` | SET DEFAULT on a table the pass does not hold: the expression read whole by the syntax-only expression reader, otherwise may fail (Revision 1.6 V7) | E1 |
| `M5k-a.P7` | the subcommand read whole by its alter_table_cmd production, otherwise row D (Revision 1.6 V7) | - |
| `M9.P7` | ALTER SCHEMA s RENAME TO t / OWNER TO r read whole, otherwise row D (Revision 1.6 V7) | E6 |
| `R3.P7` | ALTER TYPE n ADD VALUE [IF NOT EXISTS] Sconst [{BEFORE \| AFTER} Sconst] and RENAME VALUE Sconst TO Sconst read whole, otherwise row D (Revision 1.6 V7) | - |
| `R4.P7` | ALTER TYPE (RENAME TO, RENAME ATTRIBUTE, OWNER TO, SET SCHEMA, ADD/DROP/ALTER ATTRIBUTE), ALTER DOMAIN (AlterDomainStmt, RENAME, OWNER, SET SCHEMA), ALTER VIEW / MATERIALIZED VIEW (RENAME, RENAME COLUMN, SET SCHEMA, OWNER TO, [NO] DEPENDS ON EXTENSION, SET/RESET (...), ALTER COLUMN SET/DROP DEFAULT), ALTER SEQUENCE RENAME TO and ALTER COLLATION read whole, otherwise row D (Revision 1.6 V7) | E1, E6 |
| `R5.P7` | DROP {TYPE \| DOMAIN \| VIEW \| MATERIALIZED VIEW \| SEQUENCE \| COLLATION \| ...} [IF EXISTS] names [CASCADE \| RESTRICT], DROP {FUNCTION \| PROCEDURE \| ROUTINE \| AGGREGATE \| OPERATOR} with argument types, DROP CAST read whole, otherwise row D (Revision 1.6 V7) | - |
| `N2.P7` | RESET {name \| ALL \| TIME ZONE \| TRANSACTION ISOLATION LEVEL \| SESSION AUTHORIZATION} and DISCARD {PLANS \| SEQUENCES \| TEMP} read whole; SET NAMES takes a string constant or DEFAULT (gram.y opt_encoding), otherwise row D (Revision 1.6 V7) | E4 |
| `T1.P7` | BEGIN [WORK \| TRANSACTION], START TRANSACTION, COMMIT / END / ROLLBACK / ABORT [WORK \| TRANSACTION] [AND NO CHAIN], SAVEPOINT, RELEASE [SAVEPOINT], ROLLBACK TO [SAVEPOINT], PREPARE TRANSACTION, COMMIT/ROLLBACK PREPARED and LOCK read whole; transaction modes and AND CHAIN are row D (Revision 1.6 V7) | - |
| `O1.P7` | DO [LANGUAGE l] body, CALL f(args read by the syntax-only expression reader) and EXECUTE name [(args)] read whole, otherwise row D (Revision 1.6 V7) | E3 |
| `O2.P7` | CREATE EXTENSION [IF NOT EXISTS] name [WITH] [SCHEMA s] [VERSION v] [CASCADE], DROP EXTENSION [IF EXISTS] names [CASCADE \| RESTRICT], ALTER EXTENSION name UPDATE [TO v] read whole; other ALTER EXTENSION forms are row D (Revision 1.6 V7); Revision 1.9 Y33: CREATE EXTENSION with SCHEMA, VERSION or CASCADE given twice refused (42601) | E1 |
| `O2.P4` | Extension versions (Revision 1.9 Y33; extension.c CreateExtension, find_install_path, ExecAlterExtensionStmt; the generated table e1ExtensionVersions of the REL_17_6 contrib control files and scripts): CREATE EXTENSION ... VERSION v claimed only for the control file's default version, refused (22023) when no install script reaches v through update scripts, otherwise may fail, and may fail for an extension outside the table; ALTER EXTENSION x UPDATE [TO v] claimed when v is the version the set installed (a notice) or the default version reached from it, refused (22023) when no update path leads from the installed version to v, otherwise may fail; CREATE EXTENSION of an extension the set installed (or plpgsql, which initdb installs) without IF NOT EXISTS refused (42710), with it skipped; Revision 1.9 Y51: CREATE EXTENSION of an extension whose REL_17_6 control file is not trusted = true (with CASCADE, or that requires one), and ALTER EXTENSION ... UPDATE of one, may fail (only a superuser may install it) | E1, E6 |
| `O5.P4` | CREATE EVENT TRIGGER and ALTER EVENT TRIGGER may fail: only a superuser may run them, and E6 grants no superuser (Revision 1.9 Y51; event_trigger.c 137) | E6 |
| `O2.P8` | DROP EXTENSION [IF EXISTS] names without CASCADE (Revision 1.9 Y34; dependency.c reportDependentObjects, 2BP01; the generated table e1ExtensionMembers of the names each REL_17_6 contrib extension's scripts create): refused while another extension the set installed requires it (control file requires) or a live entry of the dependency record (X1) names a member; may fail for an extension whose scripts create operators, or one outside the table, while any live entry exists, and while code may have created objects | E1, E3 |
| `O3.P7` | DISCARD ALL and ALTER ROLE ... SET read whole, otherwise row D (Revision 1.6 V7) | - |
| `O5.P7` | CREATE EVENT TRIGGER, DROP OWNED BY and DROP ... CASCADE read whole, otherwise row D (Revision 1.6 V7) | - |
| `R6.P1` | CREATE FUNCTION/PROCEDURE: the language must exist; sql and plpgsql (an identifier folded, or a string constant spelled exactly so) are proven, every other language may fail (Revision 1.6 V2) | E1 |
| `R6.P14` | CREATE FUNCTION/PROCEDURE ... LANGUAGE plpgsql: the validator refuses a pseudo-type result other than trigger, event_trigger, record, void and the polymorphic types and a pseudo-type argument other than record and the polymorphic types (0A000), LANGUAGE sql a pseudo-type result other than record, void and the polymorphic types and a pseudo-type input argument other than the polymorphic types (42P13), a function without RETURNS and OUT parameters is refused (42P13); with check_function_bodies on or unknown the body is compiled and applied only in the provable subset (one block [<<l>>] [DECLARE name type [:= expr];...] BEGIN ... [EXCEPTION WHEN cond [OR cond] THEN ...] END [l] [;]; assignments to declared variables, parameters and NEW/OLD.col of a trigger function, RETURN per make_return_stmt, IF/ELSIF/ELSE, NULL, RAISE [level] 'fmt' [, expr], EXECUTE expr, PERFORM query, INSERT/UPDATE/DELETE of the expression reader's subset, statements the V7 readers read completely, T024c1b: SELECT with at most one INTO [STRICT] at parenthesis depth 0 whose targets are parameters or declared variables (several only of a pg_catalog type that is not composite) and the rest a query of the syntax-only reader; declared names may be unreserved SQL keywords; expressions of the syntax-only reader, whose FROM items are relations or, T024c1b, function calls); a certain compile error inside it (end label, trailing text, duplicate declaration, unknown variable, NEW.x outside a trigger function, RETURN against the result, RAISE parameter count, unknown condition, bad SQLSTATE, serial declared type, a trigger function with parameters) refused, anything else may fail (Revision 1.6 V1) | E1, E3, E4 |
| `O1.P14` | DO: the LANGUAGE clause as R6.P1 (Revision 1.6 V2); a PL/pgSQL DO body is compiled before it runs and is applied only in the provable subset of R6.P14 (the routine kind of a DO block: RETURN without an expression), otherwise may fail; an O1 statement whose inventory (O1.P12) may call a set routine whose PL/pgSQL body is compiled at that call (check_function_bodies off or unknown at its creation, or a declared type outside pg_catalog) may fail unless that body is in the subset with pg_catalog types (Revision 1.6 V1); Revision 1.7 W3: likewise a set LANGUAGE sql routine whose AS body was created with check_function_bodies off or unknown is parsed at that call and may fail unless R1b.P5's constant-body reader proves it | E1, E3, E4 |
| `R4.P5` | ALTER VIEW / MATERIALIZED VIEW SET / RESET (...): checked against the view options (security_barrier, security_invoker, check_option) or the heap ones; a check_option may fail (Revision 1.6 V12) | - |
| `M2.P4` | a pg_type.dat pseudo-type (typtype p) as a column or composite attribute type refused (42P16), as a domain base refused (42804) (Revision 1.6 V13) | - |
| `M1b.P2` | CREATE TABLE ... LIKE / INHERITS: an explicit column named like a system column refused (42701); CREATE TABLE ... OF a composite type with such an attribute refused (Revision 1.6 V11); Revision 1.9 Y28 (and Y27's merge): LIKE of a held table whose columns are known: a column another LIKE source or a column definition also has refused (42701), LIKE of a source whose columns are not held may fail when the table has other columns, LIKE with INHERITS may fail; OF a name the pass holds as a type other than a composite type (an enum, domain or base type, or a table's row type) refused (42809); INHERITS of a temporary table from a permanent one, of a partitioned table, a partition or a relation that is not a table refused (42809), of the same parent twice refused (42P07), and inherited columns whose types or collations differ between parents or from the column definition refused (42804) | - |
| `R1.P2` | CREATE TABLE AS, SELECT INTO and CREATE MATERIALIZED VIEW: an output column name (alias, column list, or the name of a plain column reference, function call or cast) equal to a system column name refused (42701) (Revision 1.6 V11); Revision 1.9: CREATE SCHEMA of a pg_ name, also the role-named schema of AUTHORIZATION, refused (42939, Y06); a relation (table, view, materialized view, sequence, composite type, foreign table, index) created in pg_catalog or pg_toast refused (42501), any other object created in pg_catalog may fail (E6, Y07); CREATE SCHEMA, by name or by AUTHORIZATION, of a schema the set holds, or of public or information_schema under E5 unless the set dropped it, refused (42P06) and skipped with IF NOT EXISTS, and may fail when code may have created it (Y08, Y43); CREATE TYPE AS RANGE whose multirange type name (multirange_type_name, or makeMultirangeTypeName's) a pg_type entry the pass holds already takes refused (42710, Y21); a CREATE SCHEMA element qualified by another schema refused (42P15, Y32) | E3, E5, E6 |
| `R4.P2` | ALTER TYPE ... ADD ATTRIBUTE of a system column name on a type a typed table uses: refused with CASCADE (42701), may fail otherwise; any attribute change without CASCADE on a type a typed table uses refused (2BP01) (Revision 1.6 V11) | - |
| `M5c.P8` | ALTER COLUMN TYPE: indexes, keys, CHECK constraints and expression indexes that use the column; ALTER COLUMN TYPE and DROP COLUMN without CASCADE: a trigger the set created that depends on the column (UPDATE OF, WHEN other than OLD IS DISTINCT FROM NEW; Revision 1.4); Revision 1.9 Y02: any ALTER COLUMN TYPE of a table is refused (0A000) while a column of a table the pass holds has a written type that reaches the table's row type, under any name the table has had, through arrays, domains, composite attributes or a range subtype; it may fail while a live dependency entry of a statement that creates columns whose types the pass does not hold (LIKE, OF, INHERITS, CREATE TABLE AS, SELECT INTO, a materialized view) names the table; Revision 1.9 Y40: DROP COLUMN c and ALTER COLUMN c DROP IDENTITY without CASCADE may fail while a live dependency entry names a sequence c owns; Y41: DROP COLUMN and ALTER COLUMN TYPE of table t without CASCADE may fail while a live entry of a SQL-standard body (BEGIN ATOMIC, RETURN expr) names t, and no fact of the statement is claimed; with CASCADE, DROP COLUMN makes such routines, and the objects whose entries name the column's sequence, no longer held | E1, E3 |
| `M5a.P8` | ALTER TABLE ONLY ... ADD COLUMN, ALTER COLUMN TYPE, ADD CHECK (without NO INHERIT) or RENAME COLUMN is refused while the table has inheritance children (the last two: Revision 1.4); Revision 1.9 Y27: the columns of a CREATE TABLE ... INHERITS child are recorded (its parents' columns merged with its own) and follow the parent's ADD and RENAME COLUMN: ALTER TABLE p ADD COLUMN c t without ONLY refused (42804) when a child or grandchild holds c with another type or collation (the same merges), may fail when a child's columns are not held or the column has constraints, NOT NULL, identity or generation; ALTER COLUMN TYPE without ONLY on a table with inheritance children may fail; on a recorded child, RENAME, DROP or ALTER COLUMN TYPE of an inherited column refused (42P16) | E1, E3, E6 |
| `M6.P8` | DROP TABLE without CASCADE: a table created LIKE it may use its sequences; Revision 1.8 X1 as replaced by Revision 1.9 Y39: and any dependent of the table the dependency record (R5.P8) names, an occurrence at the ON position of an object of the table and a REFERENCES target the foreign-key record decides excepted; Y40: the sequences its columns own are its names; Y52: DROP ... CASCADE also drops the columns of other tables whose type is its row type (R5.P8) | E1, E3, E6 |
| `R5.P8` | Revision 1.9 Y39 (replaces Revision 1.8 X1's record): DROP FUNCTION, PROCEDURE, ROUTINE, AGGREGATE, TYPE, DOMAIN, VIEW, MATERIALIZED VIEW, SEQUENCE, FOREIGN TABLE, COLLATION, SCHEMA or EXTENSION (Y34; and DROP TABLE, M6.P8) without CASCADE, with or without IF EXISTS, may fail while a live entry of the pass's dependency record has an occurrence that names the object under any name it has had (ALTER ... RENAME TO; its array and multirange names): every statement that can create an object with a pg_depend dependency adds an entry with the identity of its object and every word it writes (strings included; a routine's or DO body's DDL, or the whole body when it is not read or dynamic; a reg* type with a numeric constant names every object), no word removed; three positions are proven no dependency: the entry's own created name, the ON-table of an object of a table when a relation of that name is dropped, and a REFERENCES target the foreign-key record decides (a held table with known facts); an entry ends only when the object of its identity is removed: DROP of it (a trigger or policy by name and table; a routine without an argument list only when it has one held identity), DROP of its table, CREATE OR REPLACE of the same identity; Y45: a DROP that removes nothing ends none, and a rename moves the identity to the new name; Y40: the sequences a table's columns own (serial, identity, OWNED BY; the generated name clipped as ChooseRelationName does, with any numeric suffix) are names of the table; Y44: a statement that copies columns (CREATE TABLE AS, SELECT INTO, LIKE, INHERITS, PARTITION OF, OF, ALTER TABLE ... INHERIT, and views and materialized views) names the copied columns' types as the pass holds them, or every type when it does not, so DROP TYPE and DROP DOMAIN may fail while it is live; DROP SCHEMA also while the search_path may resolve to it; after DROP ... CASCADE the objects such entries created are no longer held and their CREATE or use may fail, and their entries stay live; Y52: DROP ... CASCADE of a table, view, type or domain drops every held table's column whose type is a dropped type, its array, or a domain or range over it (under any name it has had) with M5b's CASCADE effects, the table partial when the column's type or the dropped type is not exact; a partition-key column takes its table (M6 CASCADE), and the statement may fail when that is not exact | E1, E3, E4, E6 |
| `N3.P8` | TRUNCATE: no known foreign key from a table not truncated (without CASCADE); CLUSTER: a proven clusterable index | E1, E3 |
| `M4.P11` | CREATE UNIQUE INDEX or an index with expressions or a predicate on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5a.P11` | ADD COLUMN with NOT NULL (no default), a key, CHECK, REFERENCES or a generation expression on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5c.P11` | ALTER COLUMN TYPE on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5d.P11` | SET NOT NULL on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5f-u.P11` | USING INDEX PRIMARY KEY over a nullable column on a table holding rows the set wrote (Revision 1.5 U1) (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5f.P11` | ADD CONSTRAINT other than NOT VALID (keys, CHECK, EXCLUDE, FOREIGN KEY) on a table holding rows the set wrote (Revision 1.6 V5: rows the set wrote include rows its code may write, O1.P12; Revision 1.7 W6: and rows of the table's known inheritance children and partitions, recursively) | E2, E3 |
| `M5g-1.P1` | DROP CONSTRAINT of a name the table does not hold, on a table whose constraint names are exact, is refused without IF EXISTS | E1 |
| `M5g-1.P8` | DROP CONSTRAINT of a key a foreign key uses is refused without CASCADE; with CASCADE the foreign key is dropped when the key was its only candidate | E1, E3 |
| `M5a.P9` | ALTER TABLE subcommands that name one column run in the P9 pass order, or may fail | - |
| `O1.P12` | every O1 statement has a DDL inventory read from the bodies it may run (the DO body; the set-defined routines it names; for DML, EXECUTE and statements that run index expressions or view queries also every trigger function and every routine the set names elsewhere; closed over the routines the bodies call); unbounded (EXECUTE of anything but one string constant, SET, RESET or set_config, transaction control, a statement not read, DROP ... CASCADE, another language, a routine created by code) the migration may fail; the tables it names are partial (Revision 1.5 U2); Revision 1.6 V5: the inventory also lists the data statements of the bodies (INSERT, UPDATE, MERGE, COPY ... FROM, WITH) with their targets resolved as U3 resolves names (a target not resolved, one with triggers, a set rule that writes, or an unbounded inventory: every table), which from the statement on may hold rows the set wrote (P11); V8: for every relation the inventory may create, every name Postgres may generate with it is possibly held (<table>_<suffix> in pg_class, after the 63-byte truncation; the array type _<table> in pg_type), and DROP of such a name without CASCADE may fail; Revision 1.9 Y48: a CREATE FUNCTION or PROCEDURE the code runs (from a constant EXECUTE or as a static statement) is checked as at top level (R6.P14's compilation, R1b.P5's SQL body); a refusal or possible failure there makes the running statement may fail | E1, E2, E3 |
| `M1.P12` | CREATE TABLE of a name code may have created or left behind may fail; with IF NOT EXISTS its facts are unknown (Revision 1.5 U2) | E1, E3 |
| `M1b.P12` | CREATE TABLE ... PARTITION OF, LIKE, OF or INHERITS of a source code names (partitions, columns or objects the pass does not see) may fail (Revision 1.5 U2) | E1, E3 |
| `M4.P12` | CREATE INDEX of a name code may have created or left behind may fail (Revision 1.5 U2) | E1, E3 |
| `M5a.P12` | ALTER TABLE (every M5 row): a constraint or new name code may have created, a name code may have dropped, and DROP COLUMN or DROP CONSTRAINT without CASCADE, ALTER COLUMN TYPE or RENAME on a table code names (dependents the pass does not see) may fail (Revision 1.5 U2) | E1, E3 |
| `M6.P12` | DROP TABLE without CASCADE of a table code names (dependents the pass does not see) may fail (Revision 1.5 U2) | E1, E3 |
| `M7.P12` | DROP INDEX without CASCADE of an index code names, or of an index of a table code names, may fail (Revision 1.5 U2) | E1, E3 |
| `M8.P12` | ALTER INDEX RENAME TO a name code may have created or left behind may fail (Revision 1.5 U2) | E1, E3 |
| `R1.P12` | CREATE VIEW/SEQUENCE/TYPE/DOMAIN/SCHEMA (also R2) of a name code may have created or left behind may fail; with IF NOT EXISTS or OR REPLACE its record is uncertain (Revision 1.5 U2) | E1, E3 |
| `R3.P12` | ALTER TYPE ADD VALUE or RENAME VALUE of a type code names (labels the pass does not see) may fail (Revision 1.5 U2) | E1, E3 |
| `R5.P12` | DROP <other kind> or DROP SCHEMA without CASCADE of an object code names, or of a schema code may have created objects in, may fail (Revision 1.5 U2) | E1, E3 |
| `R6.P12` | CREATE FUNCTION/PROCEDURE without OR REPLACE of a routine code may have created, and DROP of a routine code names, may fail; a routine code creates, replaces, alters or drops has unknown definitions (R6.P3) (Revision 1.5 U2) | E1, E3 |
| `N1.P12` | CREATE TRIGGER, POLICY, ROLE, PUBLICATION, COLLATION, AGGREGATE, OPERATOR or CAST of a kind code may have created a name of may fail; a kind code drops or renames leaves that kind's names unknown (N1.P2, M2.P1 collations) (Revision 1.5 U2) | E1, E3 |
| `Q1.P13` | sqlx's unqualified INSERT INTO _sqlx_migrations at the end of each migration (and its UPDATE after a commit, for later migrations): applied only when the path in force reaches the default schema with only pg_catalog or set schemas before it; a path no entry of which can hold it refused (42P01); an unknown path or entry, a set relation named _sqlx_migrations, or a statement that may drop or rename public or _sqlx_migrations not proven; Revision 1.6 V6: the "$user" entry counts as any set schema; Revision 1.9 Y03: the INSERT runs in the transaction in force at the end of the string (a new one after the string committed sqlx's) and is refused (25006) when that transaction is read-only; the UPDATE after the commit runs in a new transaction with the session's defaults, and a committed default_transaction_read_only on (SET or SET SESSION CHARACTERISTICS) refuses it, which stops the run: every later migration is not applied | E4, E5 |
| `O3.P13` | an unqualified CREATE under a search_path the pass reads lands in its first existing entry (the record carries that schema); no existing entry refused (3F000), pg_catalog first refused (42501); an unknown path or an unknown entry before the deciding one not proven, and the record keeps schema null with a search_path reason; Revision 1.6 V6: the "$user" entry of E4's path is unknown from the first statement that may create a schema (CREATE SCHEMA, AUTHORIZATION, ALTER SCHEMA RENAME, code) while such a schema may exist | E3, E4 |
| `Q1.P10` | a member the pass could not read: every migration applied null; a member not UTF-8: the set is refused; a manifest the dialect depends on could not be read: no records | - |
| `Q1.P7` | a numeric literal scan.l REL_17_6 refuses anywhere in a migration (an integer, numeric or real followed by an identifier character, an exponent sign without digits, 0x, 0o or 0b without digits) or two numeric constants written together (1.2.3) refuses the whole migration (42601): Postgres parses the whole string before it runs any statement (Revision 1.9 Y22) | - |
| `R3.K2` | ADD VALUE: an existing label (without IF NOT EXISTS) or a missing BEFORE/AFTER label is refused; RENAME VALUE of a missing label or to an existing one is refused; unknown labels may fail; a label added in this transaction to a type that existed before it is unsafe to use before the commit (55P04); Revision 1.7 W7: labels are the decoded string values, and a label written in a form the pass does not decode (octal, hexadecimal or Unicode escapes, U&'...') makes the type's labels unknown (and may be any unsafe label); Revision 1.9 Y19: ADD VALUE and RENAME VALUE of a label longer than 63 bytes refused (42602) | - |

### Code effects (Revision 1.5 U2, class P12)

Every O1 statement (DO, CALL, EXECUTE, a call of a set-defined routine,
a data statement or a statement that runs index expressions or view
queries in a set that defines functions or triggers, and a modelled
statement that runs a set-defined function) has a DDL inventory, computed
in `codeeffects.go` from the bodies it may run: the DO body; every
definition so far of each set-defined routine it names; for data
statements, EXECUTE and the index-expression and view-query statements
also every trigger function and every routine a statement of the set
names outside its own definition (defaults, checks, index expressions,
policies, rules and operators run them); closed over the routine names
of the bodies, and over that second set when a body changes rows. A
PL/pgSQL body is split into its statements with block, label, IF, CASE,
loop and exception headers removed (declarations only add routine names);
a LANGUAGE sql body is a statement list. Statements whose head is CREATE,
ALTER, DROP, COMMENT, GRANT, REVOKE, SECURITY LABEL, TRUNCATE, REINDEX or
CLUSTER are read by the statement analyser, as is the one statement of an
`EXECUTE '<constant>'`; queries, data statements, assignments, RAISE,
RETURN and control statements change no object. The inventory is
unbounded on any other EXECUTE, SET, RESET or set_config, transaction
control, a nested DO, a statement the reader does not know or the
analyser cannot read, DROP ... CASCADE, a DO block or routine in another
language or not read, or a call of a routine created by code; then the
migration may fail from the statement on (`O1.P12`: the code may have
changed search_path, so sqlx's bookkeeping INSERT and every later
statement may fail) and every table is partial.

A bounded inventory, from the statement on: the names it may create are
possibly held (a later CREATE of the name without IF NOT EXISTS or OR
REPLACE may fail, with them its record is uncertain or its table
partial: `M1.P12`, `M4.P12`, `M5a.P12`, `M8.P12`, `R1.P12`, `R6.P12`;
N1 kinds by kind: `N1.P12`; a later use is not proven by P1, since the
set creates the name and the pass does not hold it); the objects it may
drop, rename or move are no longer held (a later statement that names
one without IF [NOT] EXISTS may fail; a dropped table becomes an
uncertain relation, a renamed index may be gone, a routine's definitions
become unknown for U6, a collation's N1 record unknown for `M2.P1`); every
table it names is partial, and every name it writes may have dependents
the pass does not see (DROP TABLE, DROP INDEX of the name or of an index
of such a table, DROP of another kind, DROP SCHEMA, DROP COLUMN or DROP
CONSTRAINT without CASCADE, ALTER COLUMN TYPE, RENAME, ADD VALUE or
RENAME VALUE, and PARTITION OF, LIKE, OF or INHERITS of such a source
may fail: `M5a.P12`, `M6.P12`, `M7.P12`, `R3.P12`, `R5.P12`,
`M1b.P12`). An enum label the code adds is unsafe for the rest of the
migration (`R3.K2`); a schema it creates or drops is an unknown
search_path entry (U3). Names are compared by their last part: the
search_path a body runs under is not followed, which only makes more
names meet. A guarded creation (`IF NOT EXISTS (SELECT ... FROM pg_type
...) THEN CREATE TYPE ...`) is not special-cased.

Revision 1.6 D2 completes P12: the inventory also lists the bodies'
data statements (INSERT, UPDATE, MERGE, COPY ... FROM, WITH) with their
targets, which from the statement on may hold rows the set wrote (`P11`:
`M4.P11`, `M5a.P11`, `M5c.P11`, `M5d.P11`, `M5f.P11`, `M5f-u.P11`; a
target not resolved, one with triggers, a set rule that writes, and an
unbounded inventory make every table one that may: V5), and for every
relation it may create, every name Postgres may generate with it is
possibly held (`<table>_<suffix>` in pg_class after the 63-byte cut, the
array type `_<table>` in pg_type; DROP of such a name without CASCADE may
fail: V8).

### Compilation of routine and DO bodies (Revision 1.6 V1, class P14)

`CREATE FUNCTION / PROCEDURE ... LANGUAGE plpgsql` runs plpgsql_validator
(pl_handler.c 442-551) and a DO block is compiled before it runs
(plpgsql_compile_inline): the PL/pgSQL grammar of pl_gram.y, the raw
parser over every embedded statement and expression, declared types,
assignment targets, RETURN against the result, RAISE parameter counts,
block labels and exception conditions. E3 covers what a body does when it
runs, never its compilation, so `plpgsql.go` proves the compilation: the
body must be in the provable subset (one block with declarations
`name type [:= expr]`, assignments to declared variables, parameters and
NEW/OLD.col of a trigger function, RETURN as make_return_stmt allows it,
IF / ELSIF / ELSE, NULL, RAISE [level] 'format' [, expr], EXECUTE expr,
PERFORM, INSERT / UPDATE / DELETE of the expression reader's subset,
statements V7's whole-statement readers read completely in their strict
form, and handlers WHEN condition [OR condition] THEN); every expression
is read by the syntax-only expression reader (`exprsyntax.go`: scan.l's
operator tokens, gram.y's nonassociative levels, decimal numbers without
trailing junk, string forms without lexing checks). Anything else may
fail (`R6.P14`, `O1.P14`). T024c1b adds `SELECT ... [INTO [STRICT]
target [, target]...] ...` (stmt_execsql for the word SELECT,
make_execsql_stmt): at most one INTO, at parenthesis depth 0, its
targets the routine's parameters or declared variables, and in a list of
several only variables of a pg_catalog type that is not composite
(read_into_target and read_into_scalar_list refuse a row or record
variable there, 42601); the INTO clause is cut out and the rest must be a
query of the syntax-only reader, whose FROM items are relations or
function calls (func_table) with an optional alias. A declared variable
may be named like an unreserved SQL keyword (decl_varname is a T_WORD:
the PL/pgSQL scanner knows only its own keywords). `check_function_bodies` is tracked like
search_path (U3's scopes: SET [SESSION | LOCAL], RESET, set_config with
constants, DISCARD ALL); off, CREATE checks the signature only and the
body is compiled at the first call of an O1 statement whose inventory may
call it (`O1.P14`, then proven with pg_catalog declared types only), and a
LANGUAGE sql AS body is not parsed (`R1b.P5` does not apply). The
validators' pseudo-type rules, every signature type (`M2.P1`) and the
option list (`R6.P4`: each option once, procedure attributes, COST and
ROWS) apply whatever the setting.
Revision 1.7 W3: such a LANGUAGE sql AS body, created with
check_function_bodies off or unknown, is parsed and analysed at its first
call (functions.c init_sql_fcache, clauses.c inline_function), so a call
the inventory may reach is applied only when `R1b.P5`'s constant-body
reader proves the body (`O1.P14`).

### Names checked at use (Revision 1.7, class P1)

- **W1 per-table names** (`objectkinds.go`): DROP TRIGGER, POLICY or RULE
  (without IF EXISTS), ALTER POLICY and COMMENT ON TRIGGER, POLICY or
  RULE need the object on the named table (42704). On a table outside the
  set they may fail; on a table the set holds they are applied only when
  the table resolves certainly with its facts known and its per-table
  record holds the name exactly (a name code created, dropped or renamed
  is only possibly held; the pass never holds a rule). `N1.P1`.
- **W2 reg\* arguments** (`rev17.go`): a string literal bound to a reg\*
  parameter of a set-defined routine (by position, by name or as a
  VARIADIC element; also a parameter whose type is unknown or outside
  pg_catalog) is looked up when the call is analysed; it is proven only
  as a regclass naming a table, view, materialized view or sequence the
  pass holds under a certain name. `O1.P1`.
- **W4 databases**: ALTER DATABASE, GRANT / REVOKE ON DATABASE and ALTER
  ROLE ... IN DATABASE may fail (no database is an E1 object, and the
  name of the one the set runs in is unknown). `N1.P1`.
- **W5 drops**: DROP PUBLICATION and COLLATION of a name the N1.P2 record
  does not hold (or that a DROP named since), and DROP STATISTICS,
  OPERATOR, AGGREGATE and CAST, without IF EXISTS, may fail. `N1.P1`,
  `R5.P1`.
- **W6 rows of a hierarchy**: a statement that checks a table's rows
  (P11) also counts the rows the set may have written into its known
  inheritance children and partitions.
- **W7 enum labels** (`R3.K2`): labels are decoded string values; a label
  the pass does not decode makes the type's labels unknown.

### Coverage rules inside the rows (B2 and B3)

- **V5 empty tables** (`N3.P5`, `rows.go`): a table the pass created holds
  no rows until a statement may write one; a single-row INSERT into it
  under a certain name without a set trigger meets its primary key,
  unique keys and plain unique indexes. **V6**: on such a table the first
  value of a serial or identity sequence is proven, unless the set's text
  anywhere calls `nextval`/`setval` or has ALTER SEQUENCE. ON CONFLICT DO
  NOTHING lifts the key hazard for non-deferrable keys; a MATCH SIMPLE
  foreign key with a NULL column is met.
- **O1t** (`o1t.go`): a trigger installer of the closed form above runs
  exactly the substituted CREATE TRIGGER.
- **C2** (`fkTypePair`): the verified foreign-key type pairs, for new keys
  and ALTER COLUMN TYPE on either side.
- **C3 exact constraint names** (row `M5g-1`, `keys.go`): Postgres keeps
  constraint names unique per table and ATExecDropConstraint looks the
  name up on that table only, so while the pass holds every constraint
  name of a table (the conditions in the row) DROP CONSTRAINT of a name it
  wrote removes exactly that constraint and keeps the other keys, and a
  name it does not hold is refused without IF EXISTS. ALTER INDEX/TABLE
  RENAME of a key's index renames the key; an ALTER INDEX or DROP INDEX
  under another qualification that may mean a key's index makes the table
  partial (DROP INDEX of it may fail even with IF EXISTS). Generated names
  of unnamed constraints keep Revision 7.
- **C4 `-- no-transaction`** (`sqlxversion.go`, `transactions.go`): the
  Cargo reader keeps each sqlx entry's version requirement (a workspace
  entry's from the root). When every entry allows only versions in
  [0.8.0, 0.9.0) the marker is read (sqlx 0.8.0 #3181, source.rs 127; the
  0.8 series is what A1 verified), when every entry allows only versions
  before 0.8.0 it is not, and otherwise (open ranges, pre-releases, git
  or path entries, entries that disagree) a marked migration is followed
  both ways as before. A marked migration of one statement runs outside
  any block, so CREATE/DROP INDEX CONCURRENTLY is accepted there except on
  a partitioned table (0A000), for a table or index the pass does not
  hold (whether it is partitioned is not proven), and for DROP INDEX
  CONCURRENTLY of several names or with CASCADE. Two or more statements
  run in the implicit block (P6). A failing no-transaction statement
  leaves what earlier commits made; the migration and every later one
  are `applied: null` and their facts conditional, as for any possible
  failure.
- **C5 generated-name shapes on partial tables** (`unnamedUnknown`,
  `unnamedFreeWords` in contract.go): a name shaped like a generated name
  conflicts only with a table on which something the pass did not model
  may have created unnamed objects: an opaque statement since it was
  created, or a partial reason not on the closed "no" list (name moves,
  one index that may be gone, and the ALTER TABLE words OWNER TO, ROW
  LEVEL SECURITY, ENABLE/DISABLE TRIGGER and RULE, SET LOGGED/UNLOGGED,
  SET WITHOUT CLUSTER/OIDS, SET/RESET storage parameters, REPLICA
  IDENTITY, CLUSTER ON, VALIDATE CONSTRAINT, ALTER CONSTRAINT, NO
  INHERIT, OF/NOT OF; tablecmds.c REL_17_6: none adds a constraint, index
  or sequence). ADD GENERATED AS IDENTITY, INHERIT, ATTACH/DETACH
  PARTITION, RENAME CONSTRAINT and unclassified words stay "yes".

### Oracle

`oracle_*_test.go` (test files only) apply each history to a real
Postgres 17 with sqlx 0.8.6's semantics (a fresh schema or database, the
E1 prelude, one simple query per migration, in sqlx's transaction unless
the manifest allows only versions that read `-- no-transaction`) and
compare the catalog with the records. Zero disagreements is the precision
gate. Histories: every Postgres fixture set, every recorded repro under
`testdata/oracle/`, 11 real histories (the T018a2 task record (internal),
`GRAPHVEIN_ORACLE_HISTORIES`) and generated histories from generators
g1-g9 (`GRAPHVEIN_ORACLE_SEEDS`, `GRAPHVEIN_ORACLE_GENERATORS`; a grammar
change gets a new generator id). Since Revision 1.6 the assumption scoring
reads the error context and the statement (H14-H16): a refusal is E3 only
when its innermost context frame is code running (never the compilation
or validation of a body, never 2D000); a relation template is E1 only
when the statement names the relation in a table position (not an index,
sequence, view or materialized view, nor a name inside a string); and a
class 22 or 23 refusal of ALTER TABLE or CREATE INDEX is E2 only when a
checked table may hold rows of a COPY or the prelude (rows the set's code
writes are the set's own). Revision 1.9 (2026-11-07) narrows E3 and adds
E6 (H21-H23): a refusal raised inside code is E3 only when it is code
logic (RAISE, INTO STRICT, ASSERT, class 22 or 23) or the statement that
raised it is not fixed by the set's text (a string constant, a format()
of a constant with constant arguments, or a static statement of a body
the set wrote); every assumption hit on a migration the pass claims is
logged as a `claimed-hit` line for review; and the replay runs as the role
`gv_migrator` (not a superuser: CREATEROLE, CREATEDB, REPLICATION,
BYPASSRLS, the server-file roles, owner of the database or history
schema), with CREATE EXTENSION of the prelude as the superuser, so that a
superuser-only statement (Y51's `superuserForms`) is judged and any other
permission refusal is an E6 hit. The tests skip without
`GRAPHVEIN_TEST_POSTGRES_URL`; with it, `go test` runs the fixtures, the
regression cases and seeds 1-200 of each generator (Revision 2.1; 1-1000
before, which passed go test's 10-minute package timeout under load).
Coverage is reported per history as tables complete, migrations applied
and facts known (`GRAPHVEIN_ORACLE_REPORT`). Revision 2.1 (H27): the
generated and real histories are judged under E8. A history
Postgres applies is compared whole; for a history Postgres refuses at
migration k, the pass's verdict on k is an E8 hit when it claims k, and
the facts are compared on the prefix before k (the pass re-run on the
prefix, against that prefix's catalog: the catalog after the stop, or a
fresh replay of the prefix when k is `-- no-transaction` or controls
transactions itself). Each such claim is logged as an `e8-hit
<assumption or R> <sqlstate> <case> <file> <row tag> <statement>` line
and counted per class and SQLSTATE in the gate summary (`clean`,
`refused` with `refusal-agrees`, `may-fail` and `e8-hit`, `prefix
judged`, `stopped`). Roots are a wrong fact in a
compared history or prefix, an `applied: false` on a migration Postgres
applied, and a history the harness could not judge. The fixtures and
the regression cases keep the strict comparison (a case with `"gate":
"e8"` in `oracle.json` is judged under H27). Gate runs name the range,
one generator per process: `GRAPHVEIN_ORACLE_GENERATORS=gN
GRAPHVEIN_ORACLE_SEEDS=1-10000 go test -run 'TestOracleGenerated$'
-timeout 0 ./internal/extract/schema`. The harness serialises its
cluster-wide role setup with an advisory lock (H28), so such processes
can share one cluster, and cleans up the roles a history created with
REASSIGN OWNED and DROP OWNED before DROP ROLE; a cleanup failure is a
harness note, never an abort (H25). After a schema-mode history it drops
whatever the migrating role still owns in the oracle database, so an
object a history created outside its schema (a search_path that names a
missing schema) never reaches the next history (H29).

## Known gaps

The full list is in ADR 0047, Known gaps. Found at the Revision 9 gate:

- K1: a foreign key to a table the set creates in a later migration is
  `unresolved` and both migrations get `applied: true`, although one must
  fail.
- K2: `ALTER TYPE ... ADD VALUE` without IF NOT EXISTS of an existing
  value, or on a missing type, is not modelled.
- K3: objects that `CREATE EXTENSION` creates are not known.
- C2: ALTER COLUMN TYPE `int` to `bigint` of a primary key a foreign key
  uses may fail, although Postgres accepts it.
- C3: DROP CONSTRAINT of a name the set wrote forgets all keys of the
  table (Revision 7); Rails and Django drop named constraints routinely.
- C4: `-- no-transaction` is not read, so CONCURRENTLY may fail.
- C5: user index names shaped like generated names on partial tables may
  fail.

T018a2 (ADR 0051) closes K1 and K2 (classes P1 and K2) and recovers C2-C5
inside the claim contract (rows `M3`, `M5g-1`, `T1`/`M4.P6`/`M7.P6` and
`unnamedUnknown`). What remains, in coverage-backlog order (ADR 0051,
Known gaps):

- Revision 8 unknown facts: an ADD COLUMN or ALTER TYPE on a partial
  table is the first possible failure in most real histories.
- CHECK constraints are not evaluated, so a V5 row into a table with a
  CHECK is not proven; V6 is blocked by any `nextval`/`setval` text in the
  set; `now()` and other functions as DML values and `INSERT ... SELECT`
  are not proven.
- C3 covers DROP CONSTRAINT only: generated constraint names, RENAME
  CONSTRAINT, VALIDATE CONSTRAINT and ALTER CONSTRAINT of an exact name
  still make the table partial; a primary-key drop is not claimed in a
  set with any view, rule or SQL-standard function body.
- C4 knows only the 0.8 series; VACUUM and the other F1 statements stay
  may-fail in a no-transaction migration.
- K3 extension objects, defaults compared by presence only, and E2/E3 as
  assumptions.
- Revision 1.9 U-c: the dependency record is a superset by words (any
  occurrence outside Y39's three proven positions makes a drop may fail;
  an entry of code, and an entry whose object a CASCADE may have removed,
  stays live); Y44's "every type" entries (CREATE TABLE AS, SELECT INTO,
  views) make DROP TYPE / DOMAIN may fail while they live; Y41 makes every
  DROP COLUMN / ALTER COLUMN TYPE of a table a live SQL-standard body
  names may fail. A statement code runs (a constant EXECUTE or a static
  statement of a DO body or called routine) other than CREATE FUNCTION or
  PROCEDURE is not analysed where it runs, so the running statement may
  fail unless its body is one block whose EXCEPTION WHEN OTHERS handler
  only raises notices (Y48, row D); static statements are checked only for
  names, columns and regclass arguments (Y49). Superuser-only forms (Y51),
  OWNER TO a role the set created and statements under SET ROLE of such a
  role (E6) are never claimed.
- Revision 2.1 (E8): refusal prediction is not exhaustive. The pass may
  claim a migration that Postgres refuses on a fresh database (known:
  extension members, ADMIN granted back to its grantor, bookkeeping lost
  through DROP OWNED or REVOKE, whitespace in boolean settings, composite
  ALTER TYPE attributes, ALTER DOMAIN, a permanent foreign key to a
  temporary table, unknown tablespaces, ownership of built-in objects, a
  trigger installer on a quoted outside name, a call after its column
  was renamed away). Under E8 these are E8 hits, not wrong facts; the
  error-site audit that would close them is after v1 (task Revision
  2.0).

## Tests

`testdata/fixtures/sql-migrations/` (see its README) with golden
`sql-migrations.golden.json`; T024c1: `testdata/fixtures/supabase-migrations/`
with golden `supabase-migrations.golden.json`
(`internal/index/supabase_migrations_test.go`), `TestSupabaseContract`,
`TestSupabaseDiscovery`, `TestSupabaseRowSecurity` and `TestSupabaseOracle`,
the server goldens `schema-{summary,sets,table}-supabase`; `internal/extract/schema` unit tests (lexer,
Cargo reader, versions, spans, Revision 1 cases, R5 symlinks, Revision 2 cases and dispatch forms, Revision 3, Revision 4, Revision 5, Revision 6, Revision 7 and Revision 8 cases from case texts, the Revision 7 merged-name check,
the per-set recover guard; T018a2: `TestContractIDs`, `TestContractRows`,
`TestContractFacts` and `TestContractDocument` for the claim contract, the B1a-B3 case tables
`TestB1a*`, `TestB1b*`, `TestB1cColumns`, `TestB1dValues`, `TestB2Cases`,
`TestB2Literals`, `TestB3Cases`, `TestB3RequirementReading` and
`TestB3NoTransaction`, whose `*Postgres` twins check each verdict against a
live server, and the oracle tests above);
`internal/index/schema_test.go` (golden, warm replay, single-set edits,
CRLF); `internal/store/schema_test.go` (both backends, and the CI-only
Postgres catalog check of `pg-app`).
