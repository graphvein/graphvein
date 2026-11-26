# Schema from Diesel models

ADR 0062 (T018g4, `docs/contract/claims/diesel-models.md`; inventory and
oracle baseline in the T018g4 task record (internal)). The Diesel front end
`internal/extract/models/diesel` reads, at link time and from the sources
the scanner retained, the Diesel 2.x schema files that `diesel.toml`
configures (and, names only, the `schema.rs` files no `diesel.toml`
names), with a small token lexer, and will hand its tables to the
tool-neutral model kit `internal/extract/models` (ADR 0058,
`docs/extract/schema-models.md`), which emits the records every ORM front
end shares (`source: "model"`, `tool: "diesel"`, group
`models/diesel/<schema file>`).

**Status (steps DB1 to DB5 and the Revision 2 fixes, 2026-11-15):** discovery (`discover.go`,
`config.go`, `cargo.go`; the shared Cargo TOML and `Cargo.lock` reader
`internal/extract/cargotoml`), the token lexer and item reader
(`lexer.go`, `items.go`, `file.go`; row DI-P1), provenance (`provenance.go`,
row DI-G1), the contract table (`contract.go`), the claim pass
(`link.go`, `records.go`, `types.go`, `joins.go`; "Records" below), the
indexer call and the fixture `testdata/fixtures/diesel-models/` are in
place, with the oracle and the type families.

Line numbers below are from the published crates the corpus locks:
diesel 2.3.14, diesel_derives 2.3.10, diesel_table_macro_syntax 0.3.0,
diesel_migrations 2.3.2, migrations_internals 2.3.0, diesel-async 0.9.2,
diesel_full_text_search 2.3.1; and from diesel_cli 2.3.14 (the release
matching the pin; not in `Cargo.lock`) with the diffy 0.4.2 crate it
applies patches with (`diesel_cli` requires `diffy = "0.4.0"`). Paths
without a crate are diesel_cli's `src/`.

## Discovery

`Discover` returns every `diesel.toml` and the schema files to read, each
with its owning package, pin and dialect. Diagnostics are `info`:
`schema_model_not_proven` (a config, section or file without a group),
`schema_model_unread` (row D markers) and `schema_model_migrations` (row
DI-U1).

- **Configuration (DI-K1).** Every retained `diesel.toml`. `Config` and
  `PrintSchema` are `deny_unknown_fields` (`config.rs:14-21`,
  `:448-477`): an unknown key, a value of the wrong shape or a TOML error
  refuses the file (one marker, no group). `RootPrintSchema`
  (`config.rs:403-437`): the PrintSchema fields directly under
  `[print_schema]` are the `default` configuration, every other key is a
  named configuration `[print_schema.<name>]`, and a named `default`
  replaces the direct fields. `file` and `patch_file` resolve against the
  `diesel.toml` directory (`config.rs:48-63`, `:496-508`); an absolute,
  Windows or repository-leaving path names no file. A section without
  `file` (print-schema writes to standard output) yields no group; two
  sections naming one file give one group (the first by config path and
  key) and a marker.
- **Package and pin (DI-K1, DI-V1).** The owning package is the nearest
  `Cargo.toml` with `[package]` above the schema file; it must declare
  `diesel` in `[dependencies]`, `[dev-dependencies]` or a `[target.*]`
  table (the key, a `package = "diesel"` rename, or `workspace = true`).
  The pin is the workspace root's `Cargo.lock` entry: its only diesel
  version, or the only one of the requirement's major; else `pins: null`
  with DI-V1 still listed. Diesel 2 is read; another or an unproven major
  is a marker with no group. crates.io: `crates/crates_io_database/Cargo.toml:16`
  (`=2.3.14`), `Cargo.lock` 2.3.14.
- **Migrations (DI-U1).** `[migrations_directory] dir`
  (`config.rs:553-563`), else `migrations` beside the `diesel.toml`
  (`main.rs:116`, `:215-218`), with at least one direct subdirectory
  holding an `up.sql` (dot entries skipped, migrations_internals
  `lib.rs:71-83`): one `schema_model_migrations` marker, found, not read
  yet (open point O1). crates.io: 293.
- **Unconfigured schema files (DI-U2, Revision 1 O2).** A `schema.rs` in a
  Diesel 2 package that no readable `diesel.toml` names and that holds a
  `table!` is its own group, names only, with assumption DI-V7. `table!`
  in any other file is silent D. crates.io: `crates/crates_io_worker/src/schema.rs`.

### Dialect (DI-K1, DI-V4)

Cargo unifies a crate's features over the build, so the dialect is the
union of the Diesel backends enabled anywhere in the workspace (open
point O3, Revision 1 O3 and addendum OP2): exactly one gives the dialect;
none or several give `unknown` (dialect-dependent facts unknown).

- **diesel's own features** (diesel `Cargo.toml:107-145`): `postgres`,
  `postgres_backend` (postgres), `mysql`, `mysql_backend` (mysql),
  `sqlite`, `returning_clauses_for_sqlite_3_35` (sqlite), on every
  `diesel` entry of every package of the workspace, in every dependency
  table (`[dependencies]`, `[dev-dependencies]`, `[build-dependencies]`
  and each under `[target.*]`; `cargotoml.AllDependencyTables`; Revision
  2, A5: a build script's backend counts, though resolver 2 builds
  build-dependencies with their own features, so the rule errs to
  unknown), with inherited root
  entries and the `[features]` that forward one (`diesel/sqlite`,
  `diesel?/sqlite`). diesel 2.4.0 adds `sqlite-no-std` (sqlite) and
  `mariadb`, `mariadb_backend` (its MariaDB backend): MariaDB counts as a
  backend, so with another one the dialect is `unknown`, and alone it is
  `unknown` too (no rule maps MariaDB; diesel_cli 2.4 writes
  `mariadb_type` structs for it). Step DB4, from the diesel 2.4.0
  `Cargo.toml` `[features]`.
- **Companion crates** (OP2). A crate whose `Cargo.lock` entry lists
  `diesel` among its dependencies can turn on a diesel backend. The
  feature maps of these crates were read from the crates.io index
  (`index.crates.io`, 2026-11-15) and, at the corpus pins, from their
  sources:

| crate | versions | backend always on | features that enable a backend |
|---|---|---|---|
| `diesel_migrations` | 2.0.0-rc.0 to 2.4.0 (every published 2.x) | none (`Cargo.toml:47-49`: `default-features = false`, no features) | none (`Cargo.toml:37-41`: `mysql`, `postgres`, `sqlite` are empty) |
| `diesel-async` | 0.1.0 to 0.4.1 | none (its diesel dependency enables only `i-implement-a-third-party-backend-and-opt-into-breaking-changes`) | `postgres` (`diesel/postgres_backend`), `mysql` (`diesel/mysql_backend`) |
| `diesel-async` | 0.5.0 to 0.9.2 | none (0.9.2 `Cargo.toml:133-136`) | the same plus `sqlite` (`diesel/sqlite`) |
| `diesel_full_text_search` | 2.0.0 | postgres (`features = ["postgres"]`) | - |
| `diesel_full_text_search` | 2.1.0 to 2.3.1 | postgres (`features = ["postgres_backend"]`) | `with-diesel-postgres` (`diesel/postgres`, a default feature; 2.3.1 `Cargo.toml:27-29`) |

A member's entry for a companion counts its listed features, its default
features unless `default-features = false`, and the member's `[features]`
that forward one (`diesel-async/mysql`). `diesel-async` 0.10.0 (diesel
2.4, `mariadb_backend`) is not listed: its feature map was not read.

- **Unknown instead of a guess.** A wrong single dialect would type every
  column wrongly, so the dialect is `unknown` when the workspace's
  `Cargo.lock` is missing or unreadable; when it locks a crate outside the
  workspace that depends on diesel and is not a listed companion version
  (`diesel-derive-enum`, a path package outside the workspace,
  `diesel-async` 0.10.0); when a companion with backend-enabling features
  is a dependency of a crate outside the workspace (whose features for it
  are not read) or is locked in two versions; or when a workspace
  manifest, entry or `[features]` table cannot be read.
- crates.io: the database crate's own entry names no backend
  (`["serde_json", "chrono"]`); the root package (`Cargo.toml:125`) and
  `crates_io_worker` (`Cargo.toml:12`) add `postgres`;
  `diesel_full_text_search` 2.3.1 enables `postgres_backend`,
  `diesel-async` 0.9.2 with `postgres` adds `postgres_backend`,
  `diesel_migrations` 2.3.2 none: dialect `postgres`.

## Provenance (DI-G1)

`ReadProvenance` decides which lines of a schema file `diesel
print-schema` wrote. Database facts come only from generated lines; a
hand-written line claims names only (governing
principle).

- **Header.** print-schema writes `// @generated automatically by Diesel
  CLI.` as the first line (`print_schema.rs:12`, `:203`). A configured
  file whose first line is not exactly that, and every unconfigured file,
  is hand-written throughout.
- **The patch as diesel_cli applies it.** After formatting, print-schema
  reads `patch_file` as text and applies it with diffy
  (`print_schema.rs:312-331`: `diffy::Patch::from_str`, `diffy::apply`);
  a patch it cannot read or apply makes print-schema fail. `ParsePatch`
  follows diffy 0.4.2's parser (`src/patch/parse.rs`): preamble lines are
  skipped up to the first `--- `, `+++ ` or `@@ ` line; at most one `---`
  and one `+++` filename line (quoted names with the escapes `\n \t \0
  \r \" \\`); then only hunks: `@@ -a[,b] +c[,d] @@`, body lines ` `, `+`,
  `-`, an empty line as context, and `\ No newline at end of file`; the
  counts must match the header and hunks must be in order without
  overlap. A second file's header, a stray line, a count mismatch or a
  non-UTF-8 file is not a patch diesel_cli reads.
- **Locating hunks.** diffy searches each hunk's old side in the text
  being patched from the stated new start (clamped), then one line back,
  one forward, and so on, exact lines only, never over lines an earlier
  hunk wrote (`src/apply.rs:120-197`, GNU patch's search without fuzz).
  Graphvein has the committed file, after the patch, so `LocateHunks`
  runs that search backwards: each hunk's new side (context and added
  lines) must match the file exactly at exactly one position not taken by
  an earlier hunk. Then the text before patching is rebuilt from the
  located hunks and the patch is applied forward with diffy's algorithm:
  it must give the file back with every hunk where it was located. A hunk
  with an empty new side, no match, more than one match, or a replay that
  places it elsewhere is not located.
- **Result.** Every hunk located: the file is generated and the patch's
  added lines are hand-written. A `patch_file` that names no retained
  file, is not UTF-8, is not a patch diffy reads, adds the header line
  itself, or has a hunk that is not located: the whole file is
  hand-written (an info `schema_model_not_proven` diagnostic names the
  reason).
- **Removals.** Each run of removed lines is recorded after the committed
  line it was removed after. A run is benign when every removed line is
  blank, a `//` comment (doc comments included), or a column definition
  the same hunk adds back under the same column name (a changed type, as
  crates.io's three `Array<Nullable<Text>>` lines); a run that is not
  benign inside a `table!` item makes that table's column set not
  complete (crates.io: `categories.path`), and a removed line holding
  `table!` makes the group's table list not complete.
- **Which line counts.** A table, a column or a `sql_types` struct is
  hand-written when any line from its first attribute (or doc comment)
  line to its name line (`name (keys) {`, the column's definition line,
  `pub struct Name;`) is added (Revision 2, A3): a patch that adds
  `#[max_length = 10]` above a generated column, or rewrites a struct's
  `postgres_type(name = ...)`, changes what the item states. A
  patch-added attribute above a key column leaves the table's primary key
  unknown. The macro line does not count: a hunk that adds a table ends
  with the next table's `diesel::table! {` line (crates.io's fifth hunk),
  so the macro line of a generated table can be an added line.
- **Line endings.** The patch and the schema file are read with CRLF and
  lone CR folded to LF, as the scanner retains every source; a CRLF patch
  therefore locates like the committed LF text.
- crates.io: header present; 8 hunks, 25 added lines, all located; the
  new sides sit at offsets 0, 0, 0, +2, +2, -13, -13, -13 from their
  stated new starts; 5 removed runs: 3 benign (the type changes), 2 not
  (`categories.path`, inside its table; the replaced `joinable!`, outside
  every table); no table removed.

## Assumptions

Copied from the task file's "Environment" (ids and text identical);
each group's records list the ones that apply (`assumptions`) with its
`pins`:

- **DI-V1 Pins.** The `diesel` version in the workspace's `Cargo.lock` is
  the one the code is built with (`pins: {"diesel": "<v>"}`); a missing or
  ambiguous pin gives `pins: null` with DI-V1 still listed.
- **DI-V2 Generated file is current.** A generated schema file is
  print-schema output from the deployed database, with only the configured
  `patch_file` applied afterwards, regenerated after the latest migration
  and not edited by hand outside the patch. (crates.io at the pin: true,
  oracle 0 disagreements; the developer workflow in
  `crates/crates_io_database/README.md:13-29`.)
- **DI-V3 No domains.** No column's type is a domain (print-schema writes
  a domain's base type unless `pg_domains_as_custom_types` matches it;
  `pg.rs:136-147`). Probe P3 shows the failure case (a domain over
  `integer`). Narrowed by Revision 1 O4: `Time`, `Timestamp`,
  `Timestamptz`, `Interval`, `Numeric` and every other type whose
  modifiers print-schema drops are claimed as the type family with
  modifiers unknown, so no assumption covers their modifiers (probe P3's
  `timestamp(3)`, `timestamptz(0)`, `time(3)`).
- **DI-V4 Dialect.** The database is of the dialect the workspace's Diesel
  backend features name, the backends the known companion crates enable
  included (Discovery; open point O3, Revision 1 O3 and addendum OP2);
  shown as "dialect taken from the workspace's Diesel features".
- **DI-V5 Generation settings.** The schema file was generated with the
  `[print_schema]` section that names it as it stands (its `schema`,
  `filter`, `import_types`, `pg_domains_as_custom_types`,
  `sqlite_integer_primary_key_is_bigint`).
- **DI-V6 diesel_cli rules.** The diesel_cli that generated the file
  follows the print-schema rules the claim contract cites, verified at
  diesel_cli 2.3.14 and 2.4.0. diesel_cli is a separate binary, not in `Cargo.lock`; the
  `diesel` library pin does not pin it. (2.4.0 was published 2026-11-13; open point O7. Revision 1
  O7: the rules are verified at diesel_cli 2.3.14 and, before merge,
  2.4.0; for a pin newer than the verified versions the assumption says
  "rules verified at diesel_cli <versions>". Step DB4 checked 2.4.0;
  Revision 1 addendum 4 makes that a separate assumption, DI-V8.)
- **DI-V7 Unconfigured schema file.** A `schema.rs` that no `diesel.toml`
  names is not configured by diesel.toml and may be stale (Revision 1 O2);
  its group claims names only.
- **DI-V8 Newer diesel pin.** Rules verified at diesel_cli 2.3.14 and
  2.4.0: the `diesel` pin is newer than these versions, and the diesel_cli
  that generated the file is assumed to write the forms the claim
  contract reads as they do. (Revision 1 addendum 4: listed in the group's
  assumptions, beside DI-V6, for a generated file whose pin is newer than
  2.4.0, compared by the numeric core: 2.4.1 and 2.5.0-rc.1 are newer;
  the versions are `CLIVerified` in `records.go`.)
- **DI-V9 Older diesel_cli output.** Rules verified at diesel_cli 2.3.14 and
  2.4.0; older diesel_cli output checked for the header, the `pub mod`
  wrap, `Nullable`, the type idents, `#[sql_name]`, the key list, base
  tables only, `sql_types` structs, SQLite nullability, the MySQL type rule and
  the patch (diffy 0.3, whose parser and hunk search are 0.4's) at
  2.0.1, 2.1.1 and 2.2.0, and for `#[max_length]` and the `joinable!`
  filters at 2.1.1 and 2.2.0 (SQLite's `joinable!` set also at 2.0.1):
  the `diesel` pin is older than 2.3.14 (or
  not proven), or the file shows no diesel_cli 2.1 or later output (no
  `#[max_length]` and no database comment on a generated line), and the
  diesel_cli that generated the file is assumed to write those forms as
  the checked releases do. (Revision 2, A1 and addendum:
  listed in the group's assumptions beside DI-V6 for a generated file
  whose pin is older than 2.3.14 or not proven, or that shows no
  diesel_cli 2.1 or later output whatever the pin (DI-G1); in a file that
  shows none, `Varchar` and `Varbit` without `#[max_length]` are their
  family and no `joinable!` outside SQLite is a foreign key (Revision 2
  addendum 2); the releases are
  `OlderCLIChecked` in `records.go`, the comparison is "Older diesel_cli"
  in `docs/extract/schema-diesel.md`.)

## Claim contract

Copied from `docs/contract/claims/diesel-models.md` (ids and text
identical); `contract.go` holds the same rows, and `TestDieselContract`
checks the three against each other. Row kinds: **K** scope; **G**
provenance; **T** tables; **C** columns; **S** custom SQL types; **J**
joins; **P** provable failures; **U** unread or unconfigured; **V**
assumptions; **D** the single default branch.

Dialect rule (developer decision 2026-11-13):
- **postgres:** facts verified by the oracle (`verification: "oracle"`):
  the repository's migrations (corpus) or probe DDL (fixture forms) on a
  scratch Postgres 17, catalog read, compared with the generated lines;
  names on hand-written lines (a hand-written or unconfigured file, a
  patch-added line) are `mapped` (Revision 2, A6).
- **mysql:** generated-line facts `mapped` from diesel_cli 2.3.14
  `infer_schema_internals/mysql.rs:219-280` (type ident =
  `UpperCamelCase` of the type name before `(`, `tinyint(1)` as `Bool`,
  `Unsigned<...>` for unsigned) and `information_schema.rs:143-190`.
  Type: the lower-cased ident as MySQL's base type name, `#[max_length]`
  as its length, every other type parameter unknown.
- **sqlite:** generated-line facts `mapped` from
  `infer_schema_internals/sqlite.rs:349-409`: print-schema maps the
  declared type to an affinity class (`Text`, `Integer`, `BigInt`,
  `Double`, ...), so the declared type is never recoverable: type unknown
  on sqlite; name, nullability, primary key and DI-J1 foreign keys
  `mapped`.
- **Unknown in every dialect:** defaults, identity and generated
  expressions, unique constraints and indexes, check constraints,
  foreign-key actions and names, enum labels, comments, column order
  (`column_sorting = "name"` reorders columns; crates.io uses it).


| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| DI-K1 | scope (Discovery above): a retained `diesel.toml`, its `[print_schema]` and `[print_schema.<name>]` sections with a literal `file`, the owning Cargo package's `diesel` dependency (direct, renamed, workspace-inherited), the `Cargo.lock` pin, the workspace backend-feature dialect rule (with the backends the known companion crates enable, Revision 1 addendum OP2; every dependency table counts, `[build-dependencies]` and `[target.*]` tables included, Revision 2 A5) | one group per configured file (`models/diesel/<file>`), `dialect`, `pins`, the section's literal `schema` | another major: marker, no claims; `file` missing or naming no retained file, a section that is not a table, an unknown key (`deny_unknown_fields`), a TOML error: marker, no group for that section (unknown key or TOML error: none for the file); dialect unknown: dialect-dependent facts unknown; two sections naming one file: one group, marker | DI-V1, DI-V4, DI-V5 |
| DI-G1 | provenance of each line of a configured file: generated (header on line 1, not added by a located patch hunk) or hand-written; the `patch_file` read as diesel_cli applies it (diffy 0.4, `print_schema.rs:312-331`), each hunk's new side located exactly, without fuzz | no fact of its own; selects which rows' generated claims apply; a table, a column or a `sql_types` struct is hand-written when any line from its first attribute (or doc comment) line to its name line is added by a located hunk (Revision 2 A3: a patch that adds `#[max_length]` above a column or rewrites a struct's `postgres_type(name = ...)` makes it hand-written); a generated file is diesel_cli 2.1 or later output only when it shows it: a `#[max_length = <int>]` on a generated column, or a `///` doc comment on a generated table or column that is not diesel_cli 2.0's generated text (2.0.1 writes no `max_length` and reads no database comment, `inference.rs:198-224`; 2.1.1 writes database comments, `pg.rs:74-149`); otherwise DI-C2 and DI-J1 (except on sqlite: Revision 2 addendum 2) take the diesel_cli 2.0 rules and the group lists DI-V9, whatever the pin (Revision 2 addendum) | header missing (or added by the patch), patch missing, unreadable or not a unified diff diffy reads, a hunk not located (no exact match, more than one, or a forward replay that places it elsewhere): the whole file hand-written; a hunk that removes lines inside a `table!` block (other than blank lines, `//` comment lines and column lines the same hunk adds back under the same name): every fact of that table other than its Rust-visible names (table and column names, schema) unknown, with a reason naming the patch (Revision 1 addendum 2 O-a); a patch-added attribute line above a key column: the table's primary key unknown | DI-V2, DI-V6, DI-V8, DI-V9 |
| DI-T1 | `table!` or `diesel::table!` (also `::diesel::table!`) as an item of the file or of a `pub mod <ident> { ... }` (print-schema's `schema` key wraps output in one, `print_schema.rs:284-285`): leading `use` items, outer attributes (`///` docs; `#[sql_name = "<s>"]`, `r#` prefix stripped, `diesel_table_macro_syntax` 0.3.0 `lib.rs:115-137`), `name` or `schema.name` (`lib.rs:44-51`), optional `(k, ...)` key list, `{ columns }` | every line: table `name` (`#[sql_name]` else the ident, `r#` stripped), `schema` (written `schema.` prefix, else the section's `schema`, else none: the dialect default); generated: base table (print-schema writes only `BASE TABLE`s), `primary_key` = the key list in order (DB names through each column's `#[sql_name]`), `column_set: complete` unless a patch hunk adds a column line that replaces no removed column of the same name (a patch removal in the block: DI-G1); group `table_list` complete when every table is generated, no `filter` is set, and no patch hunk removes a `table!` block | hand-written: kind, primary key and completeness unknown (a view, temporary or imaginary table, or a subset of columns is valid Diesel); an empty key list `()` or no key list: primary key unknown (probe P1 `nopk`); on sqlite a single key column `rowid`, `oid` or `_rowid_` written `Integer` or `BigInt` (the implicit rowid print-schema prepends, diesel_cli `infer_schema_internals/sqlite.rs:171-190`, `:330-343`): not claimed as a column, the column set not proven complete (it may be a declared column of that name), primary key unknown (Revision 1 addendum 2 O-b, addendum 3); a key list without a column of that name: DI-P1; a written `schema.` prefix with a `#[sql_name]` that starts with that schema and a dot (diesel_cli 2.3 writes a renamed table of a schema section so, `print_schema.rs:805-811`; 2.4.0 writes the name alone): D for the table (Revision 1 addendum 3); any other attribute or token form: D for the table | DI-V2, DI-V5 |
| DI-C1 | column definition `[#[attr]]* name -> TypePath,` (`lib.rs:85-107`) with `///` docs, `#[sql_name = "<s>"]`, `#[max_length = <int>]` | every line: column `name` (`#[sql_name]` else the ident, `r#` stripped); generated: nullability (outermost `Nullable<...>`: nullable; otherwise not null; `pg.rs:69`, `infer_schema_internals/data_structures.rs:137-150`), and the type per DI-C2; every line also carries the declared Diesel type (`Nullable<Int4>`) as a Rust-side annotation, not a database fact (no `claims` entry, Revision 1 O5; attribute `tool_type`, ADR 0065) | hand-written (DI-G1, from its first attribute line to its name line): nullability and type unknown, the name `mapped` (Revision 2 A6); any other attribute: that column's facts other than its name unknown | DI-V2 |
| DI-C2 | the column's type path on a generated line, after removing `Nullable<...>`: `Array<T>` (element `Nullable` dropped: Postgres stores no element nullability), a bare ident that names a struct of the file's `sql_types` module (DI-S1), or a bare ident whose lower-case spelling is in the closed builtin list (Postgres: `int2 int4 int8 float4 float8 bool text varchar bpchar char bytea date time timestamp timestamptz interval json jsonb uuid oid money inet cidr macaddr macaddr8 citext tsvector tsquery bit varbit xml int4range int8range daterange numrange tsrange tstzrange pg_lsn`; the inverse of `UpperCamelCase(udt_name)`, `pg.rs:66-67`) | Postgres `type` in `format_type` spelling: `integer`, `bigint`, `smallint`, `real`, `double precision`, `boolean`, `text`, `character varying`, `character`, `"char"`, `bytea`, `date`, `time without time zone`, `timestamp without time zone`, `timestamp with time zone`, `interval`, `bit`, `bit varying`, ... ; `#[max_length = n]` on `varchar`, `bpchar`, `bit`, `varbit` adds `(n)`; `T[]` for arrays; a `sql_types` struct: its `postgres_type` name (a builtin name through the same spelling map, else as written, `schema.name` with its `schema` unless that is `pg_catalog`, which print-schema writes and `format_type` never does: Revision 1 addendum 3), as the plain type when the struct is `#[diesel(enum_type)]` (an enum takes no type modifiers) or its schema is `pg_catalog` (every `pg_catalog` type that takes a modifier is in the spelling map), else as its family with the generic `modifiers` unknown (an extension type such as PostGIS `geometry` or pgvector `vector` loses its modifiers in schema.rs: Revision 2 A2b); the spelling map also holds `timetz` (`time with time zone`, a family with `precision` unknown), which print-schema writes only as a `pg_catalog` struct (Revision 2 A2a); `Numeric`, `Time`, `Timestamp`, `Timestamptz`, `Interval` and any other type whose modifiers print-schema drops (also `Bpchar` and `Bit` without `#[max_length]`, and `Array<T>` of `Varchar`, `Bpchar`, `Bit`, `Varbit`: information_schema has no length for an array column; in a file that shows no diesel_cli 2.1 or later output (DI-G1), whatever the pin, `Varchar` and `Varbit` without `#[max_length]` too, since diesel_cli 2.0 never writes it: Revision 2 A1 and addendum): the type family with the dropped modifiers unknown (Revision 1 O4, the kit's `type_family` of ADR 0065; `interval`'s fields have no kit modifier and are not stated); mysql and sqlite per the dialect rule (`mapped`) | an ident outside the list and the module, a qualified path, generic arguments other than `Nullable`/`Array`, `Range<T>`/`Multirange<T>` written generically: type unknown; hand-written lines: DI-C1 | DI-V3, DI-V4 |
| DI-S1 | a struct item of a module named `sql_types` in the file with `#[diesel(postgres_type(name = "<n>"[, schema = "<s>"]))]` (`print_schema.rs:440-455`); its `#[derive(...)]`; diesel_cli 2.4's `#[diesel(enum_type)]` on an enum's struct (2.4.0 `print_schema.rs:1314-1316`); `use super::sql_types::X` / `use super::sql_types::*` in a `table!`; diesel_cli 2.4's `pub mod rust_types { ... }` beside the `sql_types` module (its generated Rust enums, 2.4.0 `print_schema.rs:1333-1349`) | the type name used by DI-C2 (an `enum_type` struct: an enum, the plain type); a `rust_types` module's `enum` items: no definition and no marker (Revision 2 A4) | `postgres_type(oid = ..., array_oid = ...)` (no name): types naming it unknown; `mysql_type`/`sqlite_type` structs: name used on that dialect only; a struct hand-written by DI-G1 (a patch line from its first attribute to its name): types naming it unknown (Revision 2 A3); enum labels: never claimed (not in the file); any other item in a `rust_types` module: read as anywhere else | DI-V2 |
| DI-J1 | `joinable!(child -> parent (column));` or `diesel::joinable!(...)` (`diesel` 2.3.14 `macros/mod.rs:81-86`; child and parent single idents naming tables of the group) | generated: a foreign key from the child's column to the parent's single primary-key column (print-schema emits one only for such a constraint, `foreign_keys.rs:8-71`); `on_delete`/`on_update` unknown; constraint name unknown | hand-written: no foreign key (a Rust-level join hint; crates.io's patch adds two on a polymorphic `owner_id` and one from a materialized view); a file that shows no diesel_cli 2.1 or later output (DI-G1), whatever the pin: no foreign key, with a reason (diesel_cli 2.0 also writes a `joinable!` for one column pair of a composite foreign key: 2.0.1 `foreign_keys.rs` has no single-column filter; Revision 2 A1 and addendum), except on sqlite, where the generated claim stands: 2.0.1 and 2.1.1 load one constraint per `PRAGMA foreign_key_list` row (2.0.1 `sqlite.rs:65-108`, 2.1.1 `sqlite.rs:57-88`), so a composite key's rows share a table pair and both drop them with the duplicate-pair filter (2.0.1 `foreign_keys.rs:16-26`, `:40`; 2.1.1 `foreign_keys.rs:13-23`, `:38`), and the other filters are the same (Revision 2 addendum 2); a parent whose primary key is not a single generated column, a child or parent not in the group, path forms (`a::b`): no foreign key; the column's `foreign_keys` list is never complete (self references, duplicate table pairs, composite keys and keys to non-key columns are not emitted) | DI-V2 |
| DI-J2 | `allow_tables_to_appear_in_same_query!(t, ...)` (`macros/mod.rs:210`) | nothing (Rust-level: which tables may share a query) | always no fact; read so the contract test reaches the form | - |
| DI-P1 | failures the Diesel macros prove at compile time, from the file (`diesel_derives` 2.3.10 `table.rs:9-29` more than 128 columns, more than 16/32/64 without the matching feature not modelled; `table.rs:46-87` no key list and no `id` column; `table.rs:130-140` a column named like its table; `lib.rs:1450-1462` invalid `table!` syntax; a key list naming no column; two columns with one Rust name; two `table!` with one Rust name in one module; failure forms `syntax`, `columns`, `no-key`, `column-named-like-table`, `key-not-a-column`, `duplicate-column`, `duplicate-table`) | diagnostic `schema_model_import_failure` (row named); no claim for the table involved; on duplicate table names, for both | other failures (a `joinable!` to an undeclared table, which also fails to compile): its foreign key not claimed | - |
| DI-U1 | the migrations directory of a `diesel.toml` (`[migrations_directory] dir`, else `migrations` beside it) | `unread_tools` entry (found, not read yet), count of migration directories | always unread (open point O1) | - |
| DI-U2 | a Rust file named `schema.rs` in a Diesel 2 package, not named by any readable `diesel.toml`, holding at least one `table!` (open point O2; Revision 1 O2) | its own group (`models/diesel/<file>`), names only: table and column names as DI-T1 and DI-C1 read them on hand-written lines, `pins`, `dialect` | every other fact unknown (hand-written throughout, DI-G1; the corpus's case is stale); a file the lexer refuses: marker `schema_model_unread` (tool `diesel`) | DI-V1, DI-V4, DI-V7 |
| DI-V | assumptions DI-V1..DI-V9 attached to the records of the group (discovery attaches DI-V1, DI-V4 when the dialect is known, DI-V5 or DI-V7; DI-G1 and the type rows add the rest) | stated, never claimed as facts | - | - |
| D | anything else: Diesel 1.x forms (`infer_schema!`, `infer_table_from_schema!`), `table!` in any other file or function (crates.io: temporary and imaginary query tables, `information_schema.columns`), `include!`d schema files, `#[derive(Queryable, Insertable, Selectable)]` structs and `#[diesel(table_name = ...)]`, `define_sql_function!`, `sql_query`, other macros in the file, attributes and tokens no row lists | nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `diesel`) for a whole unread definition in a configured file | always | - |

## Mapped dialects

MySQL and SQLite facts are `verification: "mapped"`: taken from
diesel_cli 2.3.14's print-schema source, never from a database (the
oracle runs Postgres only). They apply to generated lines only, as on
Postgres. The tables spell out the dialect rule above for step DB3; a
form not listed is type unknown.

### MySQL

print-schema reads `information_schema.columns` (`column_type`,
nullability, `character_maximum_length`;
`infer_schema_internals/mysql.rs:22-65`, which clears the length when
`column_type` has no `(`), then `determine_column_type`
(`mysql.rs:219-233`) and `determine_type_name` (`mysql.rs:259-280`):
`tinyint(1)` becomes `bool`, any type starting with `int` becomes
`integer`, otherwise the text before `(`; `unsigned` is removed and
marked; a remaining space is an unsupported type (print-schema fails).
The ident is the name in `UpperCamelCase`, inside `Unsigned<...>` when
unsigned. Enum and set columns get a `sql_types` struct with
`#[diesel(mysql_type(name = "Enum"))]` (`print_schema.rs:525-548`, DI-S1).

| `column_type` | print-schema writes | type claimed (`mapped`) |
|---|---|---|
| `tinyint(1)` | `Bool` | `bool` (MySQL's synonym for `tinyint(1)`) |
| `int`, `int(11)`, `integer` | `Integer` | `integer` (synonym for `int`; display width not claimed) |
| `tinyint`, `smallint`, `mediumint`, `bigint` (any display width) | `Tinyint`, `Smallint`, `Mediumint`, `Bigint` | the lower-cased ident (display width and `zerofill`, which print-schema drops, are display attributes and not claimed) |
| `... unsigned` | `Unsigned<X>` | X's type followed by `unsigned` |
| `decimal(p,s)`, `float`, `float(m,d)`, `double`, `double(m,d)`, `bit(n)`, `datetime(n)`, `timestamp(n)`, `time(n)` and their unmodified forms | `Decimal`, `Float`, `Double`, `Bit`, `Datetime`, `Timestamp`, `Time` | the type family with modifiers unknown (Revision 1 O4): print-schema drops the parameters, so `Datetime` may be `datetime` or `datetime(6)` |
| `char(n)`, `varchar(n)`, `binary(n)`, `varbinary(n)` | `Char`, `Varchar`, `Binary`, `Varbinary` with `#[max_length = n]` | the lower-cased ident with `(n)`; without `#[max_length]` the length is unknown (family) |
| `tinytext`, `text`, `mediumtext`, `longtext`, `tinyblob`, `blob`, `mediumblob`, `longblob`, `date`, `year`, `json` | the name in `UpperCamelCase` (`Tinytext`, `Text`, ..., `Json`) | the lower-cased ident |
| `enum(...)`, `set(...)` | a `sql_types` struct, `mysql_type(name = "Enum")` / `"Set"` | type unknown: the labels are not in the file and a type family has no labels modifier (ADR 0065), so no family is claimed |

Names, nullability (`Nullable<...>`), primary keys and DI-J1 foreign keys
follow the same rules as on Postgres (`information_schema.rs:143-190`
for tables and keys).

### SQLite

print-schema reads `PRAGMA table_xinfo` (`sqlite.rs:145-215`; nullability
is `notnull` inverted, so an `INTEGER PRIMARY KEY` without `NOT NULL` is
written `Nullable<Integer>`) and maps the lower-cased declared type to a
class (`determine_column_type`, `sqlite.rs:349-409`; the tests
`sqlite.rs:433-461`). Many declared types give one class, so the declared
type is never recoverable: **type unknown on sqlite** (the class is kept
as the Diesel-side annotation, Revision 1 O5).

| declared type (lower-cased) | print-schema writes |
|---|---|
| `boolean`, `bool`, anything with `tiny` and `int` | `Bool` |
| `int2`, anything with `small` and `int` | `SmallInt` |
| `int8`, anything with `big` and `int` | `BigInt` |
| anything else with `int` | `Integer` (`BigInt` for a rowid column when `sqlite_integer_primary_key_is_bigint`) |
| anything with `char`, `clob` or `text` | `Text` |
| anything with `blob` or `binary`, or no declared type | `Binary` |
| anything with `float` or `real` | `Float` |
| anything with `double`, `num` or `dec` | `Double` |
| `datetime`, `timestamp` / `date` / `time` | `Timestamp` / `Date` / `Time` |
| anything else | print-schema fails (unsupported type) |

A table whose only primary key is the implicit rowid gets a prepended
`rowid` column that `PRAGMA table_xinfo` does not list
(`sqlite.rs:171-190`): it is not claimed as a column and the table's
primary key is unknown (task Revision 1 addendum 2, O-b). Names, nullability, primary keys and DI-J1
foreign keys are `mapped`.

## diesel_cli 2.4.0

Revision 1 O7: the print-schema rules cited here were checked against
the diesel_cli 2.4.0 sources (crates.io tarball, published 2026-11-13),
file by file against 2.3.14. Each rule the contract reads holds at 2.4.0
for the forms the reader accepts; new 2.4 output forms fall to an
existing refusal or to D, so no 2.4 pin follows a changed rule:

| rule (2.3.14) | 2.4.0 | effect on the contract |
|---|---|---|
| base tables only (`information_schema.rs:143-190`) | views too with `include_views = true`, written `diesel::view!` (`print_schema.rs:1595-1599`); tables still `diesel::table!` | none: `include_views` is a key 2.3.14 does not know, so the section is a marker (DI-K1); `view!` is D |
| `Nullable<...>` exactly when nullable; type ident `UpperCamelCase(udt_name)` (`pg.rs:31-74`) | same, the ident through `make_valid_identifier` (a leading digit or reserved name changes the ident only) | none: builtin idents are unchanged; a struct is read by its `postgres_type` name |
| `#[max_length = n]` from `character_maximum_length`; `#[sql_name]` on a renamed column | same (`print_schema.rs:1752-1764`); new `#[auto_increment]` column attribute | `#[auto_increment]`: any other attribute, the column's facts other than its name unknown (DI-C1) |
| table `#[sql_name]` = `full_sql_name` (schema-qualified in a schema section, `print_schema.rs:805-811`) | the name alone | the 2.3 form is D for the table (DI-T1, Revision 1 addendum 3); the 2.4 form is read as written |
| primary-key columns as the key list | same; views have none | none |
| `joinable!` filters (`foreign_keys.rs:8-71`) | same filters, split into functions; `joinable!` paths may carry a schema prefix with several schemas | a path form gives no foreign key (DI-J1) |
| `sql_types` structs `postgres_type(name, schema)` (`print_schema.rs:440-455`) | same, plus `#[diesel(enum_type)]` on enums and a generated Rust enum module (`generate_rust_enum_definitions`, default on) | `enum_type`: the struct is not read, types naming it unknown (DI-S1); the enum module is D |
| SQLite implicit rowid, affinity map (`sqlite.rs`) | same | none |
| MySQL type rule (`mysql.rs:219-280`) | moved to `mysql_like.rs:196-243`, same rule; MariaDB backend added (`mariadb_type`) | MariaDB: dialect unknown (Dialect above) |
| header `// @generated automatically by Diesel CLI.`; `pub mod <schema>` wrap | same | none |
| patch applied with diffy 0.4 (`print_schema.rs:312-331`) | diffy 0.5.0; `apply`'s exact hunk search is unchanged, the patch parser was rewritten | none known: a patch the reader's diffy 0.4 rules refuse makes the file hand-written; a parser difference in 0.5 is a known gap |
| new `diesel.toml` keys (`include_views`, `experimental_infer_nullable_for_views`, `custom_enum_derives`, `generate_rust_enum_definitions`) | - | a section using one is a marker (unknown key, DI-K1) |

The verified versions are `CLIVerified` in `records.go`. A generated
file whose `diesel` pin is newer than 2.4.0 keeps its claims and its
group lists DI-V8 ("Rules verified at diesel_cli 2.3.14 and 2.4.0: ...")
beside DI-V6 (Revision 1 addendum 4; DB4's `schema_model_not_proven`
diagnostic is dropped: the pin is in `pins`, so it added nothing).

## Older diesel_cli (Revision 2, A1)

The diesel_cli 2.0.1, 2.1.1 and 2.2.0 sources (crates.io tarballs) were
compared with 2.3.14 for every rule the contract cites. A `diesel` pin
says nothing certain about the diesel_cli that wrote the file (DI-V6),
so a generated file whose pin is older than 2.3.14, or not proven, lists
DI-V9; a rule that differs makes the facts it decides unknown below the
release that changed it. Revision 2 addendum: for the 2.0
to 2.1 differences the pin decides nothing, since a project can bump
`diesel` to 2.1 or later without re-running print-schema. A generated
file is diesel_cli 2.1 or later output only when it shows output 2.0.1
never writes: a `#[max_length = n]` on a generated column, or a `///`
doc comment on a generated table or column other than 2.0.1's generated
text (2.0.1 `infer_schema_internals/inference.rs:198-224` writes only
"Representation of the `<t>` table." and "The `<c>` column of the `<t>`
table. / Its SQL type is `<T>`.", each with "(Automatically generated by
Diesel.)", and reads no database comment; 2.1.1 reads `col_description`
and `obj_description`, `pg.rs:74-149`, and writes them, `print_schema.rs`
`write_doc_comments`). A `#[doc = "..."]` attribute and a patch-added
line prove nothing. A file that shows neither takes the 2.0 rules below
and lists DI-V9, whatever the pin (row DI-G1; `provesCLI21` in
`records.go`). Other 2.1-only forms (a MySQL `sql_types` struct with
`mysql_type`, `custom_type_derives` beyond the default) are not used as
proof. SQLite gets neither (2.1.1 `sqlite.rs:149-162` sets no length,
`inference.rs:137-143` reads no SQLite comment), so no SQLite file
proves 2.1. It does not need to for `joinable!` (Revision 2 addendum 2):
both 2.0.1 and 2.1.1 load one constraint per `PRAGMA foreign_key_list`
row (2.0.1 `sqlite.rs:65-108`, one column per constraint; 2.1.1
`sqlite.rs:57-88`, `foreign_key_columns: vec![row.foreign_key]`), so
2.1.1's single-column filter (`foreign_keys.rs:30`) never fires on
SQLite and a composite key's rows, which share a table pair, are dropped
by the duplicate-pair filter in both (2.0.1 `foreign_keys.rs:16-26`,
`:40`; 2.1.1 `:13-23`, `:38`). The other filters are the same: self
reference (2.0.1 `:30`, 2.1.1 `:27`), tables outside the output (`:31-32`,
`:28-29`), a parent key that is not its single primary-key column
(`:33-39`, `:31-37`, with the same `get_primary_keys`, 2.0.1
`sqlite.rs:189-204`, 2.1.1 `:192-207`), and the written column name
(the raw name with the reserved-word suffix, 2.0.1 `inference.rs:170-177`,
2.1.1 `:238-248`). An implicit reference (`REFERENCES parent` without a
column) takes the parent's primary key in both; with other than one key
column 2.0.1 fails print-schema (`sqlite.rs:79-95`), so no file differs.
A SQLite generated `joinable!` is therefore claimed under the 2.1 rules
whatever the writer; the file still lists DI-V9.

| rule (2.3.14) | 2.0.1 | 2.1.1, 2.2.0 | effect on the contract |
|---|---|---|---|
| header `// @generated automatically by Diesel CLI.`, `pub mod <schema>` wrap | same (`print_schema.rs:13`, `:330`) | same | none |
| `Nullable<...>` exactly when nullable; ident `UpperCamelCase(udt_name)`; `Array<Nullable<T>>` | same (`pg.rs:6-46`, `data_structures.rs:31-52`) | same (`pg.rs` `determine_column_type` differs only in error types and `record`) | none |
| `#[max_length = n]` from `character_maximum_length` | never written (no `max_length` anywhere in the 2.0.1 sources) | same as 2.3.14 | in a file that shows no 2.1 output: `Varchar` and `Varbit` without `#[max_length]` are their families (`length` unknown); `Bpchar`, `Bit` and arrays already were |
| `#[sql_name]` on renamed columns and tables (`full_sql_name`) | same (`print_schema.rs:447-451`, `:494-497`) | same | none (the schema-qualified table form is D, DI-T1) |
| base tables only | same (`information_schema.rs:312`) | same | none |
| primary-key columns as the key list | same; a table without a key fails print-schema (no rowid prepended until 2.2.0) | same; 2.2.0 prepends SQLite's implicit rowid | none: the rowid rule (O-b) withholds the column either way |
| `sql_types` structs `postgres_type(name, schema)` | same attribute; derive `diesel::sql_types::SqlType` only (`print_schema.rs:278-286`) | configurable derives | none (the derive is not read) |
| `joinable!` filters (`foreign_keys.rs:8-71`: self, outside the output, composite keys, non-key targets, duplicate pairs) | no composite-key filter, and each constraint's columns are the first `key_column_usage` row (`information_schema.rs:360-369`): a composite foreign key can be written as one `joinable!`; on SQLite every constraint is one `PRAGMA foreign_key_list` row in 2.0.1 and 2.1.1 alike, and the duplicate-pair filter drops a composite key's rows | same filters as 2.3.14 | in a file that shows no 2.1 output: no `joinable!` is a foreign key (a column reason says why), except on SQLite (Revision 2 addendum 2) |
| MySQL type rule (`mysql.rs:219-280`) | same `determine_type_name` (`mysql.rs:104-130`) | same | none |
| SQLite nullability (`!notnull`) | same | same | none |
| patch applied with diffy 0.4 (`print_schema.rs:312-331`) | diffy 0.3.0 | diffy 0.3.0 | none: diffy 0.3.0's patch parser (`src/patch/parse.rs`) is 0.4.2's and its hunk search (`src/apply.rs`) differs only in style |

The checked releases are `OlderCLIChecked` in `records.go`; DI-V9's text
names them, and `TestDieselCatalogue` checks that it does.

## Records

`Link` (called once from `internal/index/index.go`) emits one model group
per discovered file through the model kit; `verification` is `oracle` on
postgres and `mapped` on mysql, sqlite and an unknown dialect (no
`dialect` attribute then). On postgres, names on hand-written lines are
`mapped` (Revision 2, A6): a hand-written or unconfigured file's group,
a hand-written table, and the name claim of a hand-written column.
Group `assumptions`: discovery's (DI-V1, DI-V4
when the dialect is known, DI-V5 or DI-V7), DI-V2 and DI-V6 for a
generated file (and DI-V8 when its pin is newer than 2.4.0, DI-V9 when
it is older than 2.3.14 or not proven), DI-V3 once a
Postgres type or type family is claimed.

- **Tables.** Identity `table:<file>#<model>`, the model being the Rust
  path in the file with modules joined by `.` and `r#` dropped (`audit.events`,
  `match`). `name` (`#[sql_name]` else the ident), `schema`
  (`schema_known` false only in an unconfigured file without a written
  prefix). `primary_key` in key-list order, `column_set: complete` and the
  columns' `primary_key` only on a generated table whose facts are known;
  `constraint_set` is always `partial` (one `constraint_set` reason says
  why) and `unique_constraints`, `indexes`, `check_constraints` are null.
  A table with an attribute other than docs and `#[sql_name]`, or a token
  form the reader does not model, is not emitted (marker, row D).
- **Columns.** Identity by the Rust name (renamed by `#[sql_name]`, the
  name is the database name and the identity keeps the Rust name);
  `attribute` is the Rust ident as written (`r#type`). `tool_type` (ADR
  0065, `SetToolType`) holds the declared Diesel type on every line
  (Revision 1 O5; never claimed, shown labelled as Diesel's type).
  Generated lines claim `nullable` (DI-C1), `type` or `type_family`
  (DI-C2) and `primary_key` (DI-T1); hand-written lines claim the name
  only, with a reason (`fact: null`). Type-family types (`Numeric`,
  `Time`, `Timestamp`, `Timestamptz`, `Interval`, `Bpchar` and `Bit`
  without `#[max_length]`, arrays of those and of `Varchar` and `Varbit`,
  MySQL's `Decimal`, `Float`, `Double`, `Bit`, `Datetime`, `Timestamp`,
  `Time` and the length types without `#[max_length]`) are claimed through
  the kit's `SetTypeFamily`: `type_family` `{name, unknown}` with `name`
  in `format_type`'s spelling without modifiers (`timestamp with time
  zone`, `character varying[]`) and `unknown` the dropped modifiers
  (`length`; `precision`; `precision`, `scale`), `type` and those
  modifiers null. `interval`'s fields have no kit modifier: its family
  lists `precision` and says nothing about the fields. `timetz` (a
  `pg_catalog` struct) is the family `time with time zone` with
  `precision` unknown; a `sql_types` struct that is not an
  `#[diesel(enum_type)]` enum, not a builtin name and not of `pg_catalog`
  is its family with the kit's generic `modifiers` unknown (Revision 2,
  A2b: PostGIS `geometry(Point, 4326)`, pgvector `vector(1536)`), shown
  "geometry (modifiers unknown)"; in a file that shows no diesel_cli 2.1
  or later output (DI-G1), whatever the pin, `Varchar` and `Varbit`
  without `#[max_length]` are families too.
- **Foreign keys.** A claimed `joinable!` writes one entry (`table`,
  `schema`, `column`, `local_columns`, `columns`, `on_delete: null`,
  `on_update: null`, `name: null`) with a `foreign_key` edge; every other
  column has `foreign_keys: null` (the list is never complete, F5).
- **Table list.** A `schema_model_unread` diagnostic on the schema file
  (`<file>:<line>: ...`) marks each reason the table list is not proven
  complete: an unconfigured or hand-written file, a patch-added table, a
  `filter`, a patch removing a `table!` line; and every row-D definition.
  The server counts them into the group's `table_list` reasons.
- **Unread tools.** The DI-U1 marker reaches `unread_tools` as
  `{"tool": "diesel-migrations", "path": <dir>, "marker": null, "files":
  <migration directories with an up.sql>}` (server rule, Revision 1
  addendum OP1).

## Tests

- `TestDieselContract` (`contract_test.go`): this table equals the task
  file's row for row; `contract.go` lists the same rows in the same order
  with the same assumptions and sides (seven claim rows, DI-G1 selecting,
  DI-J2 without a fact, DI-P1 refusal only, DI-U1 marker only, DI-V
  without a branch, D last); every listed form is named in its row; the
  assumptions section equals the task file's environment section.
  `TestDieselFixture` is the tag half: over the fixture every row with a
  branch and every listed form is reached, every tag is a row or a listed
  form, every claim names a claim row; `TestDieselFixtureFacts` spells out
  the decided facts; `TestDieselIndexedFixture` checks the golden
  `testdata/fixtures/diesel-models.golden.json` and the warm replay;
  `TestDieselCorpus` (skipped unless `GRAPHVEIN_DIESEL_CORPUS` names a
  crates.io checkout at the pin) checks the coverage numbers.
  `TestDieselCatalogue`: the catalogue registered for
  `/api/v0/schema/assumptions` (DI-V1..DI-V7 and the rows' claim cells)
  equals this document.
- `TestDieselOracle` (`oracle_test.go`; skipped unless
  `GRAPHVEIN_DIESEL_ORACLE` names a dump of
  `scripts/oracle/diesel_catalog.sh` and `GRAPHVEIN_DIESEL_ORACLE_ROOT`
  the directory its runs are relative to; never run by CI): every claimed
  fact (table name and schema, base kind and column set when complete,
  primary key, column name, nullability, type in `format_type`'s spelling
  exactly, key membership, foreign keys) against the catalog of a scratch
  Postgres 17. Runs: crates.io (its 293 migrations in Diesel's order), the
  fixture (`scripts/oracle/diesel_fixture.sql`, the database the fixture's
  Postgres schema files describe) and the probe
  (`scripts/oracle/diesel_probe.sql`, compared through an emulation of
  print-schema's cited rules over the probe's `information_schema`: a
  test of the mapping table only). With the fixture and probe runs every
  form the fixture labels `oracle` and every form of `oracleForms` (each
  claimed builtin type with and without `#[max_length]`, arrays,
  `sql_types` structs with and without a schema, table `#[sql_name]`,
  keyword names, schema-qualified tables, keys, foreign keys) must be
  compared. Facts withheld on hand-written lines that a naive rule would
  get wrong are logged as drift. A type family agrees when the catalog
  type's base name (`format_type` without modifiers) is the family and
  the modifiers it lists unknown are not asserted; the family forms
  (each family type and its array, the arrays of length types, `Bpchar`
  and `Bit` without a length) are in `oracleForms`, compared by the probe
  run. `TestDieselCLIVerified`: DI-V8 for a pin newer than the verified
  diesel_cli versions. Revision 2 (`revision2_test.go`):
  `TestDieselOlderPins` (A1 and addendum: families and no foreign key in
  a file that shows no diesel_cli 2.1 output whatever the pin, from 2.0.4
  to 2.4.1; `#[max_length]` and a database comment prove 2.1, 2.0's
  generated docs and `#[doc]` do not; DI-V9 below 2.3.14, for an
  unproven pin and for an unproven file), `TestDieselPatchSpans`
  (A3: the review's two probes, a patch-added attribute above a key
  column), `TestDieselV24Output` (A4, A2: an `enum_type` struct, the
  `rust_types` module, `timetz`, a custom type, a `pg_catalog` struct),
  `TestDieselVerificationLabels` (A6), `TestDieselLinkGuards` (C: a
  failure in discovery or in one group's emit is a diagnostic); the
  probe adds `timetz`, `timetz(2)[]` and `probe_vc(12)`, a custom type
  that takes a modifier, and the fixture run adds `v24.rs`,
  `patched.rs` and the diesel 2.0 package `pg20`.
- `provenance_test.go`: the header, a patch with a changed and a removed
  column and an added `joinable!`, offsets both ways, unlocatable hunks
  (no match, a changed context line: no fuzz, two matches, no new side),
  a missing, unplaceable, non-UTF-8 and non-diff patch file, two files in
  one patch, a hunk adding the header, CRLF in the patch, the file or
  both, a last line without a line break, a removed table, the forward
  replay, and diffy's parser rules (`TestParsePatch`).
- `cargo_test.go`: the companion crates of the dialect rule (OP2) and the
  well-formedness of their table; `discover_test.go`: configuration,
  package, pin and the workspace dialect; `lexer_test.go`,
  `items_test.go`: the token and item forms and the DI-P1 failures.
