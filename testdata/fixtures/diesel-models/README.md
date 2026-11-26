# diesel-models fixture (T018g4)

Small Cargo packages that exercise the Diesel claim contract
(`docs/extract/schema-diesel.md`). Each package has its own `Cargo.toml`
and `Cargo.lock` (diesel 2.3.14, `pg20` 2.0.4, `v1` 1.4.8). `TestDieselIndexedFixture`
indexes this directory and checks the golden
`testdata/fixtures/diesel-models.golden.json`; `TestDieselFixture` runs
the claim pass over it for the contract test and `TestDieselFixtureFacts`
spells out the decided facts. No file needs an inert name (no ignore
files, no `target` directory).

`pg/src/schema.rs` and `pg/src/schema.patch` are written by a generator
in the unit's scratch space: the patch is `diff -u` of the print-schema
text before the hand edits and the committed file.

| package | what it covers |
|---|---|
| `pg` | postgres (oracle). `diesel.toml`: the default section with a `patch_file` (DI-G1: a rewritten column type, a removed column (O-a), an added column, an added table and two added `joinable!`), `[print_schema.audit]` with `schema` (a `pub mod audit` file, `schema.name` tables, a `sql_types` struct with a schema, one with `schema = "pg_catalog"`, and diesel_cli 2.3's schema-qualified `#[sql_name]` on a renamed table: D), `broken` (a hunk that cannot be located), `nohead` (no header; `infer_schema!`), `filtered` (`filter`), `stdout` (no `file`), `missing` (no such file), `lexbad` (the lexer refuses it), `failures` (every DI-P1 form), `v24` (diesel_cli 2.4 output: `#[diesel(enum_type)]` structs, a `rust_types` module, a `pg_catalog` `timetz`, a custom composite type; Revision 2 A4, A2), `patched` (a patch that adds `#[max_length]` above a generated column and rewrites a struct's `postgres_type` name; Revision 2 A3). Revision 2 addendum: a file is diesel_cli 2.1 output only when it shows it; `schema.rs` does by `#[max_length]` and `v24.rs` by a database comment on `people24`, while `audit.rs` (no `#[max_length]`, no comment) and `patched.rs` (a patch-added `#[max_length]` only) take the 2.0 rules (DI-V9, `audit.rs`'s `joinable!` no foreign key); a migrations directory (DI-U1). `schema.rs`: every builtin type of DI-C2, `#[max_length]`, arrays, `sql_types` structs (an enum, a schema-qualified type, an oid-only type, a builtin name, a `sqlite_type`), the refused type forms, `#[sql_name]` on a table and a column, `r#` idents, a composite and an empty key list, the three `table!` paths, an unknown attribute on a table (D) and on a column, `joinable!` forms (bare, path form, to a composite key, from an O-a table), `allow_tables_to_appear_in_same_query!` and another macro (D) |
| `pg20` | diesel 2.0.4 (Revision 2 A1): `Varchar` and `Varbit` without `#[max_length]` are families, the `joinable!` is no foreign key, DI-V9 |
| `worker` | a `schema.rs` no `diesel.toml` names (DI-U2, names only) and a `table!` in another file (silent D) |
| `mysql` | mysql (mapped): the MySQL type rule, `Unsigned<...>`, lengths, families, a `mysql_type` enum struct, a `joinable!` |
| `sqlite` | sqlite (mapped): types unknown, the implicit rowid key (O-b), a nullable integer key; `posts -> users (author_id)` is a foreign key although the file shows no diesel_cli 2.1 output (Revision 2 addendum 2), `users -> logs` is none (the parent key may be the implicit rowid) |
| `unknown` | two Diesel backends: dialect unknown (no types, nullability mapped) |
| `v1` | Diesel 1.4.8: not read (marker) |

The Diesel oracle (never run by CI) compares the claims of the
`pg` and `pg20` schema files with `scripts/oracle/diesel_fixture.sql`, the database
they describe: `bash scripts/oracle/diesel_catalog.sh --pg <url> --out
<dump> --fixture`, then `TestDieselOracle` with
`GRAPHVEIN_DIESEL_ORACLE=<dump>` and `GRAPHVEIN_DIESEL_ORACLE_ROOT` set
to this directory.
