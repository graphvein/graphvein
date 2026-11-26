# Claim contract: Diesel models

The environment assumptions and claim contract of the Diesel model extractor (ADR 0062)
as recorded when it was designed (task T018g4, internal record).
`docs/extract/schema-diesel.md` and the code table in `internal/extract/models/diesel`
must equal them (`TestDieselContract`).

## Environment (assumptions, part of the contract)

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

Finalised from the inventory and the oracle (the investigation). Row kinds: **K**
scope; **G** provenance; **T** tables; **C** columns; **S** custom SQL
types; **J** joins; **P** provable failures; **U** unread; **V**
assumptions; **D** the single default branch. Every form the corpus uses
has exactly one row (counts per row: the T018g4 inventory (internal record));
forms only the fixture uses come from the probes. The contract test keeps
`docs/extract/schema-diesel.md`, the Go table and the code 1:1 (plan,
shared piece 4). **13 rows: 12 rows plus D** (step DB2: the count was
written as 14 before; the table always had 12 rows plus D); DI-P1 only
refuses, DI-U1 only marks, DI-V has no branch, DI-G1 selects, DI-J2
claims nothing.

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

Provider-independent facts (table and column names, schema, primary-key
columns, foreign-key target) are claimed for every supported dialect with
the dialect's verification, from generated lines only (names: every
line).

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

Rows DI-U1, DI-U2 and DI-V exist so the inventory can count every scope
form and the contract test can name the assumption row. The claim-side
rows are DI-K1, T1, C1, C2, S1, J1 and, since Revision 1 O2, DI-U2
(seven); DI-G1 selects, DI-J2 claims nothing.

Kit fit (step DB3 checks it): the T018c2/T018g0 record shape holds every
claim above (`name`, `schema`, `primary_key`, `column_set`, column `type`,
`nullable`, `foreign_keys` with `columns`/`on_delete: null`); no kit
extension is expected. `type: null` with a reason naming the Diesel type
(for example "Numeric: precision and scale are not in schema.rs") keeps
the Diesel spelling visible without a new attribute (open point O4 lists
the alternative).
