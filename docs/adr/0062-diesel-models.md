# ADR 0062 - Diesel models read at link time from retained sources

**Status:** Accepted (delegated review, 2026-11-16)

Diesel 2.x models (`docs/contract/claims/diesel-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"). Written for the whole task: discovery, the token
lexer, provenance, node shapes, row and assumption ids and the oracle are
fixed by the task file's claim contract, the investigation results and Revision 1
(2026-11-15); steps DB2-DB4 implement the rest and add their
implementation notes here.

## Context

A Diesel project states its database schema in a Rust file that
`diesel print-schema` writes from a live database and that `diesel.toml`
names (`[print_schema] file`, and `[print_schema.<name>] file` for each
further configuration): `diesel::table!` blocks with the Diesel SQL type of
every column (`Int4`, `Nullable<Text>`, `Timestamptz`), the primary-key
list, `diesel::joinable!` lines for single-column foreign keys,
`diesel::allow_tables_to_appear_in_same_query!`, and a `sql_types` module
for custom types. A configured `patch_file` is applied after generation,
so a generated file can hold hand-written lines. The corpus
(rust-lang/crates.io at `4e6ac5a`, diesel 2.3.14) has one configured file
(36 tables, 209 columns, 33 `joinable!`, a 25-line patch), one
unconfigured and stale `schema.rs` in the worker crate, and 293
`up.sql`/`down.sql` migration directories.

Diesel compiles against any `table!` the developer writes (a subset of
columns, a view declared as a table, an imaginary primary key, a
`joinable!` with no constraint behind it). The investigation's oracle (the
repository's migrations applied to a scratch Postgres 17, catalog
compared) found every such case on hand-written lines
and zero disagreements on lines `print-schema` wrote. What
`print-schema` writes is a closed function of the catalog (diesel_cli
2.3.14, cited in the task file); reading a generated line back inverts it.

The Rust extractor keeps no macro token trees (ADR 0060: it returns at
every `macro_invocation` outside the sqlx gate), and on a cache hit there
is no tree, so the facts cannot come from today's cached Rust facts.

## Options considered

1. **Link time over the retained sources, with a small token lexer**
   (Prisma, ADR 0057; the SQL migration reader, ADR 0047). The scanner
   retains the normalized bytes of every UTF-8 file on fresh and warm runs
   (`internal/index/index.go`, `internal/scan/scan.go`), including `.rs`,
   `diesel.toml`, `Cargo.toml`, `Cargo.lock` and the patch file.
2. **A cached payload from the Rust extractor** (the route of ADRs 0059
   and 0060): widen `facts.RustFacts` with `table!` token trees, gated to
   Diesel files, with an aggregate extractor version bump.
3. **tree-sitter at link time** over the configured files: a second
   parse path outside the bounded workers, rejected by ADR 0059.
4. **Read the migrations instead** through the SQL migration reader:
   measured in the investigation (open point O1): every table ends `partial` because
   of the reader's may-fail cascade.

## Decision

**Option 1.** The Diesel front end (`internal/extract/models/diesel`)
reads, at link time, the schema files discovery names, from the retained
sources. No cached payload, no Rust extractor change, no aggregate
extractor version bump (model records are link-time and uncached, as in
ADR 0057; a warm index relinks). If a later unit finds a reason to cache,
the bump is the next free `extractors-N` at merge.

Why link time fits here where ADRs 0059 and 0060 rejected it: the input is
one or two declarative files per repository, named by `diesel.toml`, not
arbitrary code; the facts need no owner or scope from the extractor; and
the grammar is a token-tree subset.

### The token lexer

`lexer.go` lexes every Rust token so that a form outside the contract is
seen and refused instead of breaking the file: identifiers (Unicode
letters included) and raw identifiers (`r#type`, stored without the
prefix with a raw flag), `->`, `::` and `=>` joined and every other
punctuation character single (so `>>` closes two generics), string
literals with escapes decoded (normal, raw with hashes, byte and C
strings, line continuations, CRLF folded), integer and float literals with
suffixes, character and byte literals, lifetimes, plain line and nested
block comments (dropped), and doc comments (`///`, `/** */`, `//!`,
`/*! */`) kept as tokens because they are attributes. A character no Rust
token starts with is an `Other` token: the item holding it is unread. An
unterminated literal or comment, an invalid escape, text that is not
UTF-8, or an unbalanced delimiter refuses the whole file (one
`schema_model_unread` marker; every definition in it is row D). Tokens are
grouped into token trees as Rust macros see them.

The lexer is not a Rust parser. The item reader (`items.go`) recognises,
at the file level and inside inline `mod name { ... }` blocks: `table!`,
`diesel::table!` and `::diesel::table!` (any delimiter), `joinable!` and
`allow_tables_to_appear_in_same_query!` (bare or `diesel::` qualified),
structs (for the `sql_types` module's `#[diesel(postgres_type(...))]`,
`mysql_type` and `sqlite_type` attributes), `use` declarations, inner
attributes, and everything else as an "other" item (row D).

The `table!` reader follows `diesel_table_macro_syntax` 0.3.0
(`lib.rs:33-176`): leading `use` declarations, outer attributes (docs; the
first `#[sql_name = "..."]`, `r#` stripped from its value), `name` or
`schema.name`, an optional key list, the column block, each column
`[#[attr]]* name -> TypePath` with the first `#[sql_name]` and
`#[max_length = <int>]` read; columns split at commas outside generic
angle brackets. A shape `syn` refuses (a keyword as a name, a missing
`->`, a type that cannot start a type path, a non-string `sql_name`, a
non-integer `max_length`, tokens after the column block, ...) is a DI-P1
syntax failure, because diesel_derives turns every parse error into
"invalid `table!` syntax" (`lib.rs:1450-1462`). A shape `syn` may accept
but the reader does not model (a lifetime, const or associated-type
argument, a qualified self type, an attribute on a `use`, another table
or column attribute) is unread: D for the table or for the column's
facts other than its name. After parsing, the compile-time checks of
diesel_derives 2.3.10 apply (`table.rs:9-140`): more than 128 columns, no
key list and no `id` column, a key naming no column, a column named like
its table, two columns of one Rust name, and two `table!` of one Rust name
in one module (both fail). The 16/32/64-column limits depend on crate
features and are not modelled.

### Discovery (row DI-K1)

- **diesel.toml.** Every retained `diesel.toml`, read with the shared
  TOML reader. `Config` and `print_schema` are `deny_unknown_fields`
  (`config.rs:14-21`, `:448-477`): an unknown top-level, section or
  `migrations_directory` key, a value of the wrong shape or a TOML error
  refuses the file (one marker, no group). `RootPrintSchema`
  (`config.rs:403-437`) is followed exactly: PrintSchema fields written
  directly under `[print_schema]` are the `default` configuration, every
  other key is a named configuration (a non-table one gets a marker and no
  group), and a named `default` replaces the direct fields. Paths resolve
  against the `diesel.toml` directory (`config.rs:48-63`, `:496-508`); an
  absolute, Windows or repository-leaving path names no file. A section
  without `file` (print-schema writes to standard output) yields no group
  and, when written, a marker; two sections naming one file give one
  group (the first by config path and key) and a marker.
- **The shared TOML reader.** The Cargo-subset TOML reader moved from
  `internal/extract/schema` to `internal/extract/cargotoml` (with
  `DependencyTables` and `Features`), unchanged; `internal/extract/schema`
  keeps type aliases and a wrapper that puts "Cargo.toml" before the
  reader's `line N: ...` errors, so its diagnostics and goldens do not
  change. `cargotoml.ReadLock` reads every `[[package]]` of a Cargo.lock.
- **Package and pin (DI-V1).** The owning package is the nearest
  `Cargo.toml` with `[package]` above the schema file (an unreadable
  manifest on the way stops the search: not proven). It must declare
  `diesel` in `[dependencies]`, `[dev-dependencies]` or a `[target.*]`
  table: the key `diesel`, a `package = "diesel"` rename, or
  `workspace = true` into the root's `[workspace.dependencies]`. The pin
  is the workspace root's `Cargo.lock` entry: its only diesel version, or
  the only one of the requirement's major when several are locked; else
  `pins: null` with DI-V1 still listed. The major comes from the pin, else
  from a single-comparator requirement; Diesel 2 is read, any other or an
  unproven major is a marker with no group.
- **Dialect (DI-V4, Revision 1 O3).** Cargo unifies a crate's features
  over the build, so the backend features (`postgres`,
  `postgres_backend`, `mysql`, `mysql_backend`, `sqlite`,
  `returning_clauses_for_sqlite_3_35`; diesel 2.3.14 `Cargo.toml:107-145`)
  of every `diesel` entry of every package of the workspace count (the
  packages whose workspace root is the owning package's), with inherited
  root entries and `[features]` that can forward one (`diesel/sqlite`,
  `diesel?/sqlite`). The backends the known Diesel companion crates
  enable count too (task Revision 1 addendum, OP2): a crate
  whose `Cargo.lock` entry lists `diesel` among its dependencies can turn
  on a diesel backend, so `cargo.go` holds the feature maps of
  `diesel_migrations` (2.0.0-rc.0 to 2.4.0: none), `diesel-async` (0.1.0
  to 0.9.2: `postgres`, `mysql`, and since 0.5.0 `sqlite`) and
  `diesel_full_text_search` (2.0.0 to 2.3.1: postgres always), read from
  the crates.io index and, at the corpus pins, their sources; a member's
  entry for a companion counts its listed and default features and the
  `[features]` that forward one. Exactly one backend gives the dialect,
  stated as assumption DI-V4 ("dialect taken from the workspace's Diesel
  features"). None, several, an unreadable entry, `[features]` table or
  workspace manifest, a missing or unreadable `Cargo.lock`, a locked crate
  outside the workspace that depends on diesel and is not a listed
  companion version, or a companion with backend-enabling features that
  a crate outside the workspace depends on (or that is locked twice) give
  `unknown` with the reason (dialect-dependent facts unknown, or the
  per-dialect rule of ADR 0065 where it applies): a wrong single dialect
  would type every column wrongly, so the rule errs to unknown.
- **Unconfigured schema files (Revision 1 O2).** A file named `schema.rs`
  in a Diesel 2 package that no readable `diesel.toml` names and that holds
  a `table!` is its own group, **names only** (tables and columns), with
  assumption DI-V7 "not configured by diesel.toml; may be stale" (row
  DI-U2, which therefore claims names instead of only marking). `table!`
  in any other file stays silent D. A `schema.rs` named only by a
  `diesel.toml` that diesel_cli would refuse is unconfigured in this sense.
- **Migrations (row DI-U1).** The migrations directory
  (`[migrations_directory] dir`, else `migrations` beside the
  `diesel.toml`) with at least one migration (a direct subdirectory with
  `up.sql`, dot entries skipped, `migrations_internals` 2.3.0
  `lib.rs:71-83`) gets one `schema_model_migrations` marker
  "`<diesel.toml>`: Diesel migrations directory (not read): `<dir>`", the
  shape of the other front ends' markers; "found, not read yet" (O1).

### Provenance (row DI-G1)

A configured file is generated when its first line is print-schema's
header `// @generated automatically by Diesel CLI.`; within it, the lines
the configured `patch_file` adds are hand-written. diesel_cli applies the
patch with the diffy crate (0.4.x), whose search is `patch(1)`'s without
fuzz; Graphvein has the patched file, so each hunk's new side is located
exactly, at the only matching position, and the location is checked by
applying the patch forward to the text it implies (step DB2 notes below).
A missing, unreadable or non-unified patch, or a hunk that cannot be
located, makes the whole file hand-written; a hunk that removes lines in a
`table!` block (other than blank and comment lines and column lines it
adds back under the same name) makes every fact of that table other than
its table and column names unknown (task Revision 1 addendum 2, O-a). A
configured file without the header, and every unconfigured
file, is hand-written.

### Node shapes and claims

Model-defined `table` and `column` nodes (`source: "model"`,
`tool: "diesel"`) with `contains` and `foreign_key` edges in the T018c2
record shape plus the T018g0 kit (ADR 0058), group key
`models/diesel/<schema file>`, so the Schema view shows them through
T018f's model source group without view changes. Database facts come only
from generated lines: base-table kind, primary key, complete column set,
nullability (outermost `Nullable`), the Postgres type in `format_type`
spelling (DI-C2's closed list, `sql_types` structs through DI-S1), and a
foreign key per generated `joinable!` to the parent's single generated
primary-key column (`foreign_keys` never complete; actions and names
unknown). Hand-written lines claim names only and, with generated lines,
carry the declared Diesel type (`Nullable<Int4>`) as a Rust-side
annotation with no `claims` entry (Revision 1 O5). Types whose modifiers
print-schema drops (`Numeric`, `Time`, `Timestamp`, `Timestamptz`,
`Interval`, ...) are claimed as the type family with modifiers unknown
through the kit's `SetTypeFamily` of ADR 0065 (Revision 1 O4);
the declared Diesel type is the kit's `tool_type` (`SetToolType`). MySQL
`enum`/`set` structs stay unknown (their labels are no kit modifier, so
no family is claimed). Postgres facts are
`verification: "oracle"`; MySQL and SQLite facts are `mapped` from
diesel_cli's own mapping source.

### Rows and assumptions

Rows (14, the task file's claim contract): DI-K1 scope, DI-G1 provenance,
DI-T1 tables, DI-C1 columns, DI-C2 types, DI-S1 custom SQL types, DI-J1
`joinable!`, DI-J2 `allow_tables_to_appear_in_same_query!` (no fact),
DI-P1 provable failures (`schema_model_import_failure`), DI-U1 the
migrations marker, DI-U2 unconfigured `schema.rs` (names only, O2), DI-V
the assumptions, and D. Assumptions: DI-V1 pins, DI-V2 the generated file
is current, DI-V3 no domains (narrowed by O4), DI-V4 dialect from the
workspace features, DI-V5 the generating section as it stands, DI-V6
diesel_cli rules (verified at 2.3.14 and, before merge, 2.4.0), DI-V7
unconfigured and possibly stale, DI-V8 a pin newer than the verified
diesel_cli versions (Revision 1 addendum 4; listed with DI-V6 for such a
pin, its text names the verified versions). Discovery attaches
DI-V1, DI-V4 (when the dialect is known) and DI-V5 (configured) or DI-V7
(unconfigured); DI-G1 and the type rows add the rest.

### Oracle

The repository's own `up.sql` migrations applied in Diesel's order
(version = name up to the first `_`, dashes removed, sorted as strings;
`metadata.toml` `run_in_transaction = false` honoured) to a scratch
Postgres 17 through `scripts/cloud-postgres.sh`, then a catalog dump;
every claimed fact compared, differences that the patch explains
classified as repository drift, any other difference a precision stop.
Probe DDL with a print-schema emulation covers forms the corpus does not
use, as a test of the mapping table only. No diesel_cli build.

As built: `scripts/oracle/diesel_catalog.sh` (bash 3.2, psql
only; never run by CI) writes one JSON dump per preset, `--corpus` (the
migrations) or `--fixture` (`diesel_fixture.sql`, the database the
fixture's Postgres schema files describe, and `diesel_probe.sql`); the
catalog query is `diesel_catalog.sql`. `TestDieselOracle` emulates
print-schema over the probe's `information_schema` in Go and compares the
claim pass of each run with its catalog, types in `format_type`'s spelling
exactly, and checks that every form labelled `oracle` was compared. Its
probe found two forms print-schema writes that the DB3 rules spelled
wrongly: `sql_types` structs with `schema = "pg_catalog"` (tsvector,
tsquery, xml, varbit, point; now the name alone) and diesel_cli 2.3's
schema-qualified `#[sql_name]` on a renamed table of a schema section
(now D). diesel_cli 2.4.0 was checked against the cited rules
(`docs/extract/schema-diesel.md` "diesel_cli 2.4.0"); a newer pin lists
DI-V8 ("Rules verified at diesel_cli 2.3.14 and 2.4.0: ...",
which replaced DB4's diagnostic). Step DB5 added the type-family
comparison: a family agrees when the catalog type's base name
(`format_type(atttypid, NULL)`) is the family and the modifiers it lists
unknown are not asserted; the probe compares every family form
(`oracleForms`).

### Revision 2 (independent review, decisions)

Fewer or weaker claims only, except where the oracle compared the form:

- **Older pins (A1).** diesel_cli 2.0.1 never writes `#[max_length]` and
  writes a `joinable!` for one column pair of a composite foreign key; so
  on a `diesel` pin older than 2.1.0, or not proven, `Varchar` and
  `Varbit` without `#[max_length]` are their families and no `joinable!`
  is a foreign key. diesel_cli 2.0.1, 2.1.1 and 2.2.0 were compared with
  every cited rule (`docs/extract/schema-diesel.md` "Older diesel_cli";
  diffy 0.3.0, which they use, has 0.4.2's parser and hunk search); a
  generated file whose pin is older than 2.3.14, or not proven, lists the
  new assumption DI-V9 ("Older diesel pin", renamed "Older diesel_cli
  output" by the addendum below).
- **Types (A2).** `timetz` joins the spelling map for `pg_catalog`
  structs only, as the family `time with time zone` (`precision`
  unknown). A `sql_types` struct that is not a builtin name, not of
  `pg_catalog` (every `pg_catalog` type that takes a modifier is in the
  map: `SELECT typname FROM pg_type WHERE typmodin <> 0` on Postgres 17)
  and not diesel_cli 2.4's `#[diesel(enum_type)]` is claimed as its
  family with a generic modifier unknown: print-schema drops an
  extension type's modifiers (`geometry(Point, 4326)`,
  `vector(1536)`). **Small kit change:** `models.TypeModifiers` gains
  `ModifiersUnknown` (`"modifiers"`), listed alone, with no attribute of
  its own (`SetTypeFamily` clears no attribute for it; `checkTypeFamily`
  and `modeltest.CheckTypeFamilies` accept it only alone); the view shows
  "geometry (modifiers unknown)". ADR 0065 (accepted) is not edited: the
  change is recorded here and in `docs/extract/schema-models.md`. The
  oracle compares it with a probe type that takes a modifier
  (`probe_vc`, varchar's I/O and typmod functions under another name,
  `LANGUAGE internal`).
- **Provenance (A3).** A table, a column or a `sql_types` struct is
  hand-written when any line from its first attribute line to its name
  line is patch-added: a patch that adds `#[max_length]` above a
  generated column, or rewrites a struct's `postgres_type(name = ...)`,
  no longer yields a type the database does not have. A patch-added
  attribute above a key column makes the primary key unknown.
- **diesel_cli 2.4 default output (A4).** `#[diesel(enum_type)]` structs
  are read (the enum's plain type), and `pub mod rust_types { ... }` (its
  generated enums) is no definition: no table-list marker. Contract
  change: two DI-S1 forms.
- **Dialect union (A5).** Every dependency table counts, build
  dependencies included (`cargotoml.AllDependencyTables`; the sqlx reader
  keeps `DependencyTables`).
- **Labels (A6).** Names on hand-written lines are `mapped`: the oracle
  compared generated lines only.
- **Guards (C).** `Discover` and each group's `models.Link` run under
  `Steps.Guard`, and `internal/index/index.go` calls `diesel.Link`
  through `safeDiesel` (as `safeTableAccess`): an internal failure is a
  `schema_model_internal_error` diagnostic, never a failed index run.
- **View (C).** A Diesel table's header chip reads "table! <Rust path>",
  its declarations sit in "the table! block", and its table-list reasons
  speak of the schema file and `table!` blocks, not classes.

### Revision 2 addendum

The diesel_cli that wrote a file is not the `diesel` library pin: a
project can bump `diesel` to 2.1 or later without re-running
print-schema, leaving 2.0 output. A1's 2.0 rules are therefore keyed to
the file, not the pin (fewer claims only): a generated file is diesel_cli
2.1 or later output only when it shows output 2.0.1 never writes, a
`#[max_length = n]` on a generated column or a `///` doc comment on a
generated table or column other than 2.0.1's generated text (2.0.1
`inference.rs:198-224` writes only that text and reads no database
comment; 2.1.1 writes database comments, `pg.rs:74-149`). Otherwise
`Varchar` and `Varbit` without `#[max_length]` are their families, no
`joinable!` is a foreign key and DI-V9 (now "Older diesel_cli output") is
listed, whatever the pin; a proven file lists DI-V9 only for a pin below
2.3.14 or not proven. A patch-added `#[max_length]` and a `#[doc]`
attribute prove nothing. Options considered: `#[max_length]` alone (the
requested minimum; on crates.io, which has none, it withholds all
29 foreign keys and turns 38 `Varchar` types into families) and adding
the database-comment proof (crates.io's `with_docs = true` output
carries database comments, so its coverage is unchanged). Chosen: both.
Other 2.1-only output (a MySQL `mysql_type` struct, non-default
`custom_type_derives`) is not used. Rows DI-G1 (the proof), DI-C2 and
DI-J1 (the 2.0 rules) changed; `provesCLI21` in `records.go`.

### Revision 2 addendum 2

No SQLite file can show 2.1 output (no lengths, no comments), so the
addendum withheld every SQLite `joinable!`. The diesel_cli 2.0.1 and
2.1.1 sources show the SQLite `joinable!` set is the same in both: each
loads one single-column constraint per `PRAGMA foreign_key_list` row
(2.0.1 `sqlite.rs:65-108`, 2.1.1 `sqlite.rs:57-88`), so a composite
key's rows share a table pair and the duplicate-pair filter drops them in
both (2.0.1 `foreign_keys.rs:16-26`, `:40`; 2.1.1 `:13-23`, `:38`), and
the self-reference, outside-the-output and single-primary-key filters and
`get_primary_keys` are the same. Options considered: keep withholding
(safe, but no SQLite project ever gets a foreign key), or exempt SQLite
from DI-J1's 2.0 rule. Chosen: the exemption; a SQLite generated
`joinable!` is claimed under the 2.1 rules whatever the writer, DI-V9 is
still listed. Rows DI-G1 (claim cell names the exception) and DI-J1
changed; `readJoinable` in `joins.go`. The shape-by-shape comparison is
in the task file's "Revision 2 addendum 2".

## Consequences

- No extractor, cache, store schema or contract change; a warm index
  relinks the Diesel groups from retained sources.
- Lexing the crates.io schema file (1,348 lines) per link is small next to
  the SQL reader; the gate measures it.
- Generated files get database facts with the oracle's backing; files
  developers edit by hand get names only, so a stale or imaginary
  `table!` never states a wrong type, key or foreign key.
- The workspace dialect rule can name a dialect the owning package does
  not enable itself (crates.io: the database crate's own entry names no
  backend); the assumption DI-V4 says so on every record.

## Known gaps

- Companion crates other than the three listed, and companion versions
  outside the listed ones (`diesel-async` 0.10.0 with diesel 2.4), make
  the dialect unknown rather than being read; so does a workspace without
  a committed `Cargo.lock` (common for libraries).
- A companion's backend features turned on by a crate outside the
  workspace are not read (the crate's manifest is not retained): unknown.
- Cargo's `[workspace] members` and `exclude` globs are not read: the
  workspace is the packages whose nearest `[workspace]` manifest (or
  `package.workspace`) is the same, as in `internal/extract/schema`.
- diffy 0.5 (diesel_cli 2.4) rewrote the patch parser; the reader follows
  diffy 0.4.2's parser and was not compared with 0.5's line by line
  (Revision 1 addendum 4). A patch the two parsers read differently is
  not detected as such; a hunk placed differently is caught by the forward
  replay check (DI-G1), which makes the file hand-written.
- The diesel_cli that wrote a file is not known (DI-V6). Since the
  Revision 2 addendum the 2.0 rules apply unless the file shows 2.1
  output, so a 2.0-written file no longer gets 2.1 claims; the cost is
  that a 2.1 or later file with no `#[max_length]` and no database
  comment (no `with_docs`, or no comments) loses its `joinable!` foreign
  keys and its unbounded `Varchar` types. No SQLite file can show 2.1
  output (no lengths, no comments); its `joinable!` lines are claimed
  anyway, since 2.0.1 and 2.1.1 write the same SQLite set (Revision 2
  addendum 2; 2.0.0's `sqlite.rs`, `foreign_keys.rs`, `inference.rs`
  and `print_schema.rs` are 2.0.1's byte for byte).
- Build dependencies count in the dialect union although resolver 2
  builds them with their own features: a build script's backend makes
  the dialect unknown (Revision 2, A5; errs to fewer claims).
- A 2.3 `sql_types` struct of an enum (no `enum_type` marker) is a family
  with `modifiers` unknown, though an enum takes none: the file does not
  say it is an enum.
- An `interval` family lists `precision` unknown; its fields (`day`,
  `year to month`) have no kit modifier and are not stated (the family
  claims the base type only).
- `include!`d schema files, Diesel 1.x forms (`infer_schema!`), `table!`
  outside configured files and `schema.rs`, and the 16/32/64-column
  feature limits are not read.
- The lexer refuses a whole file for one unterminated literal; Diesel's
  own build would fail there too.
- Diesel migrations are found, not read (O1; follow-up task "Diesel
  migration sets in the SQL reader").
- The DI-U1 marker needs a server rule to appear in `unread_tools`
  (the server's Prisma, Drizzle and Sequelize rules read their own
  markers); the task's Revision 1 addendum (OP1) allows step DB3 to add
  that one rule (added: tool `diesel-migrations`).

## Implementation notes

### Records

- `link.go` (`Link`, called once from `internal/index/index.go`),
  `records.go` (DI-T1, DI-C1, DI-S1 lookup, DI-U2, D markers), `types.go`
  (DI-C2: the Postgres spelling map, the MySQL mapped rule, SQLite type
  unknown), `joins.go` (DI-J1, the table-list markers). Model names are
  the table's Rust path in the file with modules joined by `.` and `r#`
  dropped (a node identity allows neither `::` nor `#`); a column renamed
  by `#[sql_name]` keeps its Rust name in its identity.
- The declared Diesel type is the kit's `tool_type` (`SetToolType`) and
  type-family types go through `SetTypeFamily` (after ADR 0065
  reached main; DB3 wrote `diesel_type` and type unknown with a reason).
- A table list that is not proven complete (an unconfigured or
  hand-written file, a patch-added table, `filter`, a patch removing a
  `table!` line) gets a `schema_model_unread` marker on the schema file,
  which the server counts into the group's `table_list` reasons.
- Facts the schema file never holds (defaults, uniques, indexes, checks,
  other foreign keys) are explained by one table reason (`constraint_set`)
  per generated table; columns without a generated `joinable!` have
  `foreign_keys: null`.
- No assumption catalogue is registered yet (`models.Register`): it would
  change the server's `schema-assumptions` golden, which step DB3 leaves
  unchanged (open point).

### Discovery, lexer and item reader

- Package `internal/extract/models/diesel`: `lexer.go` (tokens, trees),
  `items.go` (items, `table!`, `joinable!`, allow lists, `sql_types`
  structs, DI-P1 checks), `file.go` (`ReadFile`: per-file markers and
  DI-P1 diagnostics), `config.go` (`ReadConfig`), `cargo.go` (manifests,
  diesel entries, pin, dialect), `discover.go` (`Discover`,
  `MigrationDirs`). No records yet; `internal/index/index.go` is not
  called until step DB3.
- Package `internal/extract/cargotoml`: the moved TOML reader
  (`Parse`, `ParseError`), `DependencyTables`, `Features`, `ReadLock`,
  `LockedVersions`.
- On the crates.io pin (scratch run): one configured file (36 tables, 209
  columns, 33 `joinable!`, one allow list of 36, the `Ltree` struct),
  pin 2.3.14, dialect postgres; the worker's `schema.rs` as an
  unconfigured group (1 table, 7 columns); DI-U1 with 293 migrations; no
  unread item and no DI-P1 failure.

### Provenance, contract table and companion crates

- `provenance.go`: `ParsePatch` follows diffy 0.4.2's parser
  (`src/patch/parse.rs`: preamble, one `---`/`+++` pair, hunks only, the
  `\ No newline at end of file` marker, counts and order checked), so a
  patch diesel_cli refuses is not read. `LocateHunks` runs diffy's search
  (`src/apply.rs:136-197`: the stated new start, then one line back, one
  forward, exact lines, never over lines an earlier hunk wrote) over the
  committed file with each hunk's new side, and requires exactly one
  match: two matching positions mean the added lines are not proven, so
  the hunk is not located. It then rebuilds the text before patching and
  applies the patch forward with diffy's algorithm; the result must be the
  file with every hunk where it was located. `ReadProvenance` combines the
  header, the patch file (missing, unplaceable, non-UTF-8, unreadable: an
  info `schema_model_not_proven` and the whole file hand-written) and the
  located hunks into the added lines and the removed runs (benign when
  only blank lines, `//` comments, or column lines the same hunk adds back
  under the same name). Sources are read with CRLF folded to LF, as the
  scanner retains them.
- On crates.io: 8 hunks located at offsets 0, 0, 0, +2, +2, -13, -13, -13
  from their stated new starts, 25 added lines, 5 removed runs of which 2
  not benign (`categories.path`, and the replaced `joinable!` outside
  every table); the configured file generated, the worker's `schema.rs`
  hand-written (unconfigured).
- `contract.go`: the 12 rows plus D of the task file's claim contract
  (the task said "14 rows"; the table has 12 plus D), with sides claim,
  select (DI-G1), no-fact (DI-J2), refusal, marker, none and default,
  their assumption columns, the DI-P1 failure forms, and the assumption
  list DI-V1 to DI-V7. `TestDieselContract` keeps it, the task file's
  table and `docs/extract/schema-diesel.md` 1:1. The task file's rows
  were brought in line with Revision 1 (O2 names-only DI-U2 with DI-V7,
  O4 type families and the narrowed DI-V3, O5 annotations, O7) and the
  DB2 provenance rules in the same change.
- `cargo.go`: the companion feature maps and the lock-based rule above
  (OP2); `cargotoml.LockedPackage` gained `Dependencies`.


## Review notes (delegated review, 2026-11-16)

- **Steps:** the investigation (DA), DB1 discovery/lexer, DB2 provenance and patch
  locator, DB3 records, DB4 oracle and catalogue, DB5 adoption of the
  ADR 0065 kit (type families, tool type), DF review fixes (Revision 2),
  DG writer-proof rule and gate, DS SQLite `joinable!` exemption.
- **Developer decisions (2026-11-15):** Revision 1 O1-O7 (migrations
  unread, unconfigured schema.rs names-only with DI-V7, workspace dialect
  union, type families, Diesel tool type, patch read, every 2.x pin).
- **Independent review (DR):** class A findings all fixed: 2.0 pins and
  unbounded varchar (A1, older-output rules + DI-V9), `Timetz` and
  extension types with dropped modifiers (A2, families incl. a generic
  `modifiers` unknown), patch-added attribute lines (A3), diesel_cli 2.4
  enum output (A4), build-dependencies in the dialect union (A5),
  hand-written names `mapped` (A6); guards and Diesel view wording (C).
  Follow-up: the CLI that wrote a file must be proven by the
  file itself (`#[max_length]` or database doc comments), else the older
  rules apply whatever the pin; SQLite `joinable!` exempt after a
  shape-by-shape source check of 2.0.1-2.3.14.
- **Results:** oracle (293 crates.io migrations on Postgres 17, fixture,
  probe; 75 forms): 0 disagreements on claimed facts; the oracle test fails
  on any uncompared `oracle` label. Corpus snapshot: 16 corpora identical
  to main; crates.io gains only Diesel records. Timing within 10 %
  (langflow +0.9 % fresh / +4.1 % warm; crates.io +2.4 % / +6.0 %); store
  crates.io +0.93 % (SQLite) / +1.75 % (Postgres). CI run 43757176135
  (`ci/s30-t018g4`): Windows and postgres green, postgres raw log 0 FAIL,
  only env-gated skips.
- **Coverage (crates.io @4e6ac5a4):** 36 tables (34 with kind, primary key
  and complete column set; `categories` names only after a non-benign
  patch removal), 209 columns, 198 nullability, 164 types + 34 families,
  29 foreign keys; worker schema.rs as an unconfigured names-only group;
  293 migrations listed as not read.
- **Assumptions shown:** DI-V1..DI-V9.
- **Known gaps / after v1:** Diesel migrations through the SQL reader
  (follow-up with the reader's may-fail cascade), interval fields, MySQL
  enum/set types, diffy 0.5's parser not compared line by line with 0.4,
  2.1-only output forms other than `#[max_length]` and doc comments not
  used as writer proof.
