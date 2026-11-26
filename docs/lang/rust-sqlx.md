# Rust sqlx query syntax

ADR 0060; task T018h (code-to-table linking) (capture). Aggregate
extractor version: extractors-49 (33 from T018h: cached Rust facts gained the
`Queries` payload; 34 to 39 from later TypeScript, binding and Go payload changes; 42 from T020 S4, ADR 0071: the Rust structure contract; 43 from T020 S2X, ADR 0072: Python workspace members; 45 from T020-TSG, ADR 0074: patched JavaScript/TypeScript/TSX grammars). Contract version unchanged; the payload adds no graph
record.

The Rust extractor captures the sqlx query sites that code-to-table linking
reads (rows CT-S1 and CT-S2 in `docs/contract/claims/table-access.md`, and `docs/extract/table-access.md`): the macro or function path,
the SQL literal decoded, the site's owner and its lexical scope. The payload
is syntax, never a claim. Whether a path names sqlx (a single segment
through the scope's `use` declarations), which migration set the package
has and which tables the SQL names are decided at link time. Before this
payload the extractor returned at every `macro_invocation` and cached calls
without arguments, so no SQL text was cached (T018h).

## Payload

`facts.RustFacts.Queries` (JSON `Queries`, omitted when empty), type
`[]facts.RustQuery`, in source order. Versioned by the aggregate extractor
version, like the rest of `RustFacts`.

- **File gate.** Only a file with a `use` declaration whose path starts with
  `sqlx` (`use sqlx::query;`, `use sqlx::{self, PgPool};`, `use ::sqlx::...`)
  or a captured site whose path starts with `sqlx::` keeps its sites. Every
  other Rust file has no `Queries` field, so its cached facts are unchanged.
  (A `sqlx::` path elsewhere in a file, such as a type, admits nothing more:
  a site with a one-segment path can only be claimed through a `use`, which
  passes the gate itself.)
- **Sites.**
  - Macros (CT-S1): `query!`, `query_as!`, `query_scalar!` and their
    `_unchecked` forms, spelt with one segment (`query!`) or a first segment
    `sqlx` (`sqlx::query!`, `::sqlx::query!`). `Macro` is true.
  - Functions (CT-S2): `query`, `query_as`, `query_scalar`, `query_with`,
    `query_as_with`, spelt the same way, called directly (generic arguments
    such as `query_as::<_, User>` are dropped from `Path`). Method calls
    (`db.query(...)`) and other crates' paths (`other::query`) are not
    captured.
  - `query_file!`, `query_file_as!` and every other form are row D and not
    captured.
- **Fields.** `Path` (segments without a leading `::`), `Macro`, `Literal`,
  `SQL`, `SQLSpan` (the literal argument), `Span` (the whole invocation or
  call), `Owner` (the innermost emitted function or method around the site,
  the call capture's `Source`; else the file node) and `Scope` (the innermost
  `RustFacts.Scopes` entry by byte range, for resolving a one-segment path
  through `RustScope.Imports` and its parents).
- **SQL argument.** The first argument, or the second for `query_as!` and
  `query_as_unchecked!` (the first is the record type). A macro body is a
  token tree, so arguments are split on the commas between its child
  tokens: comments and string literals are child nodes, so a comma inside a
  string or comment never splits. The argument is a literal when it is one
  string literal, normal or raw (`r"..."`, `r#"..."#`), or for a macro a
  `+` concatenation of such literals (sqlx's macros accept it); its decoded
  value is `SQL`. A function's argument is a literal only when it is one
  string literal. Anything else (a constant, `concat!`, `&format!(...)`, a
  variable, a byte string) leaves `Literal` false and no `SQL`: CT-S1 or
  CT-S2 refuses it (`sql_not_literal`), never guesses.
- **Validity.** `RustFacts.QueriesValid()` (a path without empty segments,
  an owner, a span with a line, a scope index in range, `SQL` and `SQLSpan`
  only with `Literal`) is checked on every cache read
  (`internal/index/index.go`); a damaged payload is a cache miss.

## Known gaps

- Sites inside another macro's token tree (`tokio::try_join!(sqlx::query!(...)
  ...)`, `assert!(...)`) are not parsed by the grammar and are not captured.
- A record type with a comma (`query_as!(Pair<A, B>, "...")`) splits wrongly;
  the SQL argument is then no literal, so the site is refused, not misread.
- `query_scalar_with` and the `query_file*` macros are not listed by the
  contract and not captured.
- `#[macro_use] extern crate sqlx;` with unqualified macros does not pass the
  gate (the contract claims `<m>!` only when a `use sqlx::...` imports it).

Tests: `internal/lang/rust/queries_test.go` (macros and functions, raw
strings, `+` concatenation, `query_as!` record types, comments never
captured, closures, the gate, `use sqlx::query` scope resolution) and
`internal/lang/facts/facts_test.go` (validity).
