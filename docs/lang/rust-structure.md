# Rust structure: claim contract

Status: contract of the T020 implementation (2026-11-17, `extractors-42`), ADR
0071, revised by the review fixes of step S4F (the T020 task record (internal),
ADR 0071 "Review fixes") and the coverage recovery of step S4X (rows RS-D9,
RS-D10 claim, RS-D12 added, raw-identifier `mod` names in RS-M2; ADR 0071
"Coverage recovery"), from the investigation draft (the T020 task record (internal), read at
`5b48ad4`, `extractors-39`). The rows are kept 1:1 with `structureRows` in
`internal/lang/rust/structure.go` by `TestRustStructureContract`, which also
checks that the fixture `testdata/fixtures/rust-structure` reaches every row
and the default branch D, that no branch reports an id outside the table,
and that every site a branch refuses (D and the refusals of RS-M2, RS-M3,
RS-I5) emits no node and no edge. `TestRustStructureOracle` (opt-in) compares the claimed facts with
an oracle built from the Rust toolchain's own logic over the raw bytes:
`syn` 2 for declarations, `cargo metadata` for crate roots and
`RUSTC_BOOTSTRAP=1 rustc -Zunpretty=hir-tree` (rustc's module loader and
resolver, every dependency an empty stub crate) for `mod` and `use` edges,
under a fixed set of cfg configurations (ADR 0071, "cfg coverage"). Rows
change only through a revision of task T020 (or a later task) with a
version bump. Label rule: row PL-L4 of `docs/extract/precision.md` (added after the independent review, its fixes S4F and the
developer decision O-S4-1 (a)): every Rust file a crate root reaches,
tests, examples, benches and build scripts included, is labelled
`precise`, except a file whose structure claims nothing (rows RS-F2 and
RS-F3, a file Graphvein could not parse or extract, or one dropped by
validation), which the indexer stores as `best_effort` and the server
serves with reason `structure_refused`. A file no crate root of a read
`Cargo.toml` reaches (no package, an unread manifest, no `mod` chain from
a target) keeps its claimed declarations but is stored `best_effort` with
an info diagnostic `structure_unrooted` and served with reason
`structure_unrooted`: its imports are not resolved as a crate member and no
oracle configuration compiles it. The label covers the rows below, never
calls (`code_calls`, row D).

Scope (T020 decision O5 (a)): Rust files, declarations and `use` / `mod`
import edges, as emitted by `internal/lang/rust` (`Extract`, `Refuse`,
`Link`, `NewModuleResolver`, `ResolveImport`). Calls are a separate scope
with no contract and stay Best effort (row D of the label table); they
resolve with the resolver's non-import mode, which this contract does not
change. Tauri commands, Axum routes, sqlx queries and Diesel models in Rust
files are other families' scopes.

**Every form no row below names goes to the single default branch D: it
claims nothing (no node, no edge). Forms inside the scope that D receives
are counted (one `rust_structure_unread` diagnostic per file); forms
outside the scope ("Not modelled") are not counted.**

Input text: the source as Rust reads it. Rust ends a line only at LF (CRLF
is LF; a lone CR is white space, and inside a `//` comment it does not end
the comment), and skips a leading byte order mark and a first-line shebang.
A file whose raw bytes hold a lone CR is read beyond its file node by no
row (RS-F3), because the scanner's text (CR as a line end) and the text
Rust reads have different lines.

Status column: **oracle** = `TestRustStructureOracle` reports 0
disagreements on every compared fact of the row on the fixture and the
corpora axum, axum-ddd, kftray, crates.io, hoppscotch, localsend and the
self-index (numbers in ADR 0071); **oracle (cfg)** = as oracle, for the
items rustc keeps in at least one oracle configuration; the entries no
configuration reaches are listed per corpus in ADR 0071 and rest on this
contract, the fixture and review.

## Crates and modules (input to the import rows)

| row | form | claim | refusal (to D) | status |
|---|---|---|---|---|
| RS-M1 | a scanned `Cargo.toml` with a `[package]` table that Graphvein reads | crate roots, as cargo 1.97 discovers targets: the library (`[lib] path` or `src/lib.rs`, unless `autolib = false` and no `[lib]` table; named `[lib] name` or the package name with `-` as `_`); explicit `[[bin]]`, `[[test]]`, `[[example]]`, `[[bench]]` targets (`path` as written, or a `name` found among the files cargo discovers for that kind); auto-discovered targets (`src/main.rs`, `src/bin/*.rs`, `src/bin/*/main.rs`, and `*.rs`, `*/main.rs` under `tests/`, `examples/`, `benches/`) unless the kind's `autobins`/`autotests`/`autoexamples`/`autobenches` key is `false`, or, unset, the package is edition 2015 (or of an unknown edition) and declares targets of that kind; an auto-discovered target whose name or path an explicit target takes is dropped; the build script (`build` as written, else `build.rs`, none for `build = false`), which sees only `[build-dependencies]` and not the package's library. Only scanned files are roots. Also the package's edition (`edition`, `edition.workspace = true` from the workspace root's `[workspace.package]`, 2015 when absent, as cargo) and its dependency tables (`[dependencies]`, `[dev-dependencies]`, `[build-dependencies]` and their `[target.*]` forms, `workspace = true` entries from `[workspace.dependencies]`). The manifest is read as TOML statements: strings of all four forms (multi-line ones included), arrays and inline tables over several lines (TOML 1.1, as cargo 1.97), comments. No node or edge of its own | a manifest Graphvein cannot read (`config_parse_error`: a crate root, target or discovery key of the wrong type, a line that is neither a header nor an assignment, an unclosed table, string, array or inline table, a structural table defined by a top-level key such as `lib.path = ...`, a dotted form of a read key) gives no crate root and no package (open point O-S4-5 (a)); so does a package with a target cargo cannot locate (a `name` with no file or two files, two discovered targets with one name: cargo refuses the manifest); a manifest without `[package]` (a virtual workspace, an empty file) gives none either; a dependency value Graphvein does not read (a TOML 1.1 `\x` or `\e` escape) is `unknown` (RS-I2, RS-I3) | oracle (cargo metadata: the oracle compares Graphvein's roots with the targets `cargo metadata` reports, R-EXTRA / R-MISSING) |
| RS-M2 | `mod x;` (outlined, not inside a block) in a file reached from a root, without `#[path]` | `imports` edge file → the module's file: `x.rs` or `x/mod.rs` in the module directory (a raw identifier `mod r#impl;` names `impl.rs` / `impl/mod.rs`, as rustc; paths follow the name as written, `use r#impl::X`, and a path through the other spelling resolves nothing) (the file's directory for crate roots, `mod.rs` files and files loaded through `#[path]`, `<dir>/<stem>/` for other files, plus inline module names); evidence: the `mod` item span. Each `mod` item resolves from its own attributes and the module instance holding it: several `mod` items with one name in one scope (cfg alternatives, an inline module next to an outlined one) are all kept, each with its own file | both candidates present (`ambiguous_module`, no edge; rustc E0761); neither present (`unresolved_module`); crates (or module instances) holding the file that disagree on the file (`unresolved_module`); a file reached from no root resolves no outlined module | oracle (cfg) |
| RS-M3 | `#[path = "..."] mod x;` (comments and doc comments between the attribute and the item do not detach it) | as RS-M2, the path taken relative to the file's directory, or, inside inline modules, to the inline module directory; the loaded file is a mod-rs file for its own `mod` items (as rustc) | as RS-M2 | oracle (cfg) |
| RS-M4 | inline `mod x { ... }` (outside blocks), with or without `#[path]` | no node or edge; adds `x` to the qualified names inside it and to the directory of outlined modules inside it | - | oracle (cfg) |
| RS-M5 | `mod` inside a function or other block body | none (Rust: a block's items are not module items): the module, its items and its `use` declarations claim nothing; counted (`block_module` for the module and each of its `use` declarations, `local_item` for its items) | - | oracle (no edge, no node) |

Cfg union: `#[cfg(...)]` is never evaluated; a module under any cfg is
linked (as Go's union over build configurations). An edge states that the
`mod` item names that file, not that one build compiles both.

## Files

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| RS-F1 | a scanned file with extension `.rs` (any crate, test, example, bench, build script, unreachable from any root) | file record and level-3 `file` node, `language: rust`; the file exists at this path with this content hash | - (the scanner decides the inventory) | oracle (inventory equals `git ls-files '*.rs'` minus the scanner's ignore rules) |
| RS-F2 | a file in which tree-sitter reports a syntax error (an error or missing node) | the file node only, labelled `best_effort`, plus the indexer's `syntax_error` warning | every declaration, `mod` item and `use` leaf tree-sitter recovered goes to D, counted (`syntax_error`); the file's calls, Tauri, route and query payloads are dropped with them | oracle (every file syn rejects claims no declaration and no edge) |
| RS-F3 | a file whose raw bytes hold a CR not followed by LF | the file node only, labelled `best_effort` | everything to D, counted (`lone_cr`; it wins over `syntax_error`), other payloads dropped as for RS-F2 | oracle (the fixture's lone-CR files) |

## Declarations

Spans: contract v1 spans of the item without its outer attributes and doc
comments (from the visibility or first keyword to the closing brace or
semicolon); the content hash covers exactly the span. Qualified names
follow `docs/lang/identity.md` ("Rust"): inline module names joined with
`.`; an item in a function body is named under that function's qualified
name without its `@n` (`run.helper`, `S.m.inner`, `run.L.new`) (RS-D10).
`@2`, `@3` number repeated qualified names among the items these rows
claim in one file in two passes: first the module items (RS-D1..RS-D8) in
source order, then the impl associated items (RS-D9) and the items in
function bodies (RS-D10) in source order, each taking the next number of
its qualified name, so they never change a module item's identity (items
D receives never take a number). Every row emits one `contains` edge
(`provenance: fact`, confidence 1, evidence = the span).

| row | form | node kind, qualified name | contained by | refusal (to D) | status |
|---|---|---|---|---|---|
| RS-D1 | `fn` item in a module body (file or inline module, outside blocks) | `function`, `Name` | file | RS-D11, RS-D12 | oracle |
| RS-D2 | `struct`, `enum`, `union`, `trait`, `type` alias item in a module body | `struct` / `enum` / `union` / `trait` / `type_alias`, `Name` | file | RS-D11, RS-D12 | oracle |
| RS-D3 | `macro_rules! name` in a module body | `macro`, `name` | file | RS-D11, RS-D12 | oracle |
| RS-D4 | `const` / `static` item with a visibility modifier (`pub`, `pub(crate)`, ...) in a module body, name not `_` | `constant` / `variable`, `Name` | file | private ones and `_` are not modelled | oracle |
| RS-D5 | `fn` in an `impl` block in a module body | `method`, `Type.name` (inherent) or `Type.name@Trait` (trait impl); `Type` by the self-type rule of `identity.md` | file | RS-D11, RS-D12 | oracle |
| RS-D6 | `fn` (with or without default body) in a `trait` in a module body | `method`, `Trait.name` | the trait node | RS-D11, RS-D12 | oracle |
| RS-D7 | `fn` in an `extern` block in a module body | `function`, `name` | file | - | oracle |
| RS-D8 | `static` with a visibility modifier in an `extern` block in a module body | `variable`, `NAME` | file | private ones are not modelled | oracle |
| RS-D9 | `const` / `type` inside an `impl` block in a module body (or, under RS-D10, in a function body) | `constant` / `type_alias`, `Type.NAME` (inherent) or `Type.NAME@Trait` (trait impl), with any visibility, as RS-D5 names methods; attributes `receiver`, `trait`, `receiver_path` as methods; no scope binding (calls and imports never resolve to it) | file (as RS-D5) | `const _` (rustc: "`const` items in this context need a name") and a `static` in an impl: D, counted (`impl_assoc_item`); items in the initializer: RS-D12 | oracle |
| RS-D10 | an item of a form RS-D1..RS-D9 inside the body of a function or method these rows claim, at any depth of blocks, closures and `match` arms, not inside a block's `mod` (RS-M5) or a `const`/`static` initializer | the kind its row gives, named under the enclosing function: `Fn.Name` (`run.helper`), impl methods and associated items `Fn.Type.name[@Trait]`, trait methods `Fn.Trait.name`; `Fn` is the function's qualified name without `@n`; numbered in the second pass; attribute `local: true`. It binds no scope name (calls and `use` leaves never resolve to it, the extern prelude's `macro_name` check reads module items only) and owns no calls: calls in its body stay with the enclosing module item, and calls to it stay unknown (a `let` or pattern binding may shadow it; Graphvein does not model value bindings) | the enclosing function or method; a method of a trait in a function body: that trait | private `const`/`static` are not modelled (as RS-D4); the forms RS-D12 refuses | oracle |
| RS-D11 | items generated by macros (`macro_rules!` invocations, derives, attribute macros, `include!`) | none (not modelled; Graphvein never expands macros) | - | - | D (not counted) |
| RS-D12 | any item inside a block outside a claimed function body: a `const` or `static` initializer (at module level, in an impl or in a function body), a block's `mod` item and its items (RS-M5), the body of an item that claims nothing | none: D, counted (`local_item`; mod items `block_module`, RS-M5); calls inside such functions stay with the enclosing function | - | - | oracle (no node) |

Not modelled (outside the scope; claim nothing, not counted): private
`const`/`static`, trait associated `const`/`type`, struct fields, enum
variants, generic parameters, `extern crate` items, `use` items as nodes,
attributes and doc comments, `macro` 2.0 items, `global_asm!`.

## Imports

Each `use` declaration (outside blocks or inside a function body, not
inside a block's mod item) is one claim site; the evidence span of every
edge is the whole declaration (`pub use a::{b, c};` included). Several
leaves resolving to the same target merge into one edge with several
evidence entries; the edge says "this declaration names something in that
file", the terminal declaration reached through re-export chains.

A leaf is resolved in every crate that holds the importer (a file reached
from several roots); a refusal in any of them refuses the leaf. Import
resolution never lets the glob of an unscanned crate bind a name, reads
first segments only from the extern prelude below, and reports refusals;
calls and the framework consumers keep the resolver's other mode.

**Extern prelude** (rows RS-I2, RS-I3): a first segment (or `::`-prefixed
first segment) that names no local item, module or import is, in this
order: refused when an `extern crate x as y;` item of the crate binds it
(`extern_crate_alias`, precision stop PS-S4-7) or a module-level
`macro_rules!` of the file has that name (`macro_name`, PS-S4-5); an
external crate for `std`, `core`, `alloc`, `proc_macro` and `test`; the
package's own library for its binaries, tests, examples and benches; a
declared dependency (extern name: the key of a renamed dependency, the
library name of the package a path dependency names, else the key; `-` as
`_`): a path dependency whose directory holds a scanned package with a
scanned library resolves into that library (RS-I2), any other source
(registry, git, a path outside the scan) is an external crate (RS-I3),
never a scanned library of the same name (PS-S4-6); a dependency value
Graphvein does not read, or one name declared with different sources, is
an external crate unless a scanned library has that name (then
`undecided_crate`); any other name is refused (`undeclared_crate`, open
point O-S4-4 (a)). A file no root reaches uses the nearest read package
above it, never that package's own library.

| row | form | facts claimed | refusal (to D) | status |
|---|---|---|---|---|
| RS-I1 | a leaf of an edition 2018, 2021 or 2024 crate whose path resolves (through `crate::`, `self::`, `super::`, lexical names, explicit and glob re-exports) to a declaration or module of the same crate; through a module name with several cfg alternatives (RS-M2), the rest of the path resolves in each, and the leaf claims only when every alternative reaches the same file | `imports` edge file → the file holding the terminal declaration (a module: its file) | ambiguous resolution (several globs naming different items, repeated names), cycles: `unresolved_import` warning, no edge (RS-I5); a name only the glob of an unscanned crate could supply (`external_glob_name`, PS-S4-4); `crate::` in a file no root reaches (`unrooted_crate_path`); cfg alternatives reaching different files (`cfg_module_alternatives`) | oracle (cfg) |
| RS-I2 | a leaf whose first segment names, through the extern prelude, the library of another scanned package declared as a path dependency, or the package's own library | as RS-I1, resolved in that crate | duplicate library names (`duplicate_crate_name`, no edge); refusals of the extern prelude | oracle (cfg; finished by the oracle's table walk) |
| RS-I3 | a leaf whose first segment names, through the extern prelude, a sysroot crate or a declared dependency that is no scanned path library | `imports` edge to `external:.#cargo:<crate>`, `<crate>` the dependency's package name (the `package` key of a renamed dependency, else its key; `-` as `_`), so `ser = { package = "serde" }` gives `cargo:serde`; a dependency value Graphvein does not read, or one extern name with different sources, gives the extern name: the file imports from that crate, nothing about which version or source | refusals of the extern prelude | oracle (cfg; crate name only) |
| RS-I4 | a leaf in a crate of edition 2015 (`edition = "2015"` or no edition key), of an edition Graphvein cannot read, or in a file under no read package; a leaf of any crate whose resolution follows a `use` declaration (explicit or glob) of such a crate | none: D, counted (`edition_2015_path`, or `edition_unknown_path`) until 2015 paths are modelled (open point O-S4-3 (a)) | - | oracle (no edge) |
| RS-I5 | any other leaf (enum variants, `#[macro_export]` macros, `self as x` in groups, unresolvable paths) | no edge; `unresolved_import` warning | - | oracle (no edge) |

Cfg union: as for modules, a `use` under any cfg is resolved, and the
declarations it reaches are those of every cfg branch; a path through a
module name with several cfg alternatives resolves in each and claims only
when they agree (RS-I1).

## Default branch D

Claims nothing. Counted per file (when the form is in the scope) in one
`info` diagnostic `rust_structure_unread`, whose message ends with the
count per form, sorted: `(form=n, form=n)`. Forms:

- `syntax_error`: declarations, outlined `mod` items, `use` leaves and the
  extractor's own counts of a file in which tree-sitter reports a syntax
  error (RS-F2); such a file counts no other form.
- `lone_cr`: the same for a file whose raw bytes hold a lone CR (RS-F3).
- `local_item`: items inside blocks outside claimed function bodies (RS-D12).
- `block_module`: mod items inside blocks and each `use` declaration
  inside them (RS-M5).
- `impl_assoc_item`: an impl's associated `const _` and `static` items
  (RS-D9 refusals).
- `edition_2015_path`, `edition_unknown_path`: `use` leaves RS-I4 refuses.
- `external_glob_name`: `use` leaves needing a name only the glob of an
  unscanned crate could supply (RS-I1).
- `undeclared_crate`, `undecided_crate`, `extern_crate_alias`,
  `macro_name`: first segments the extern prelude refuses.
- `unrooted_crate_path`: `crate::` leaves in a file no root reaches.
- `cfg_module_alternatives`: `use` leaves through a module name with
  several cfg alternatives that reach different files (RS-I1).

The extractor's counts are stored in `facts.RustFacts.Unread` (cached with
the file's facts); the linker adds its own and emits the diagnostic.

## Precision stops of the investigation (all closed in the implementation)

| stop | row | observed in the investigation | The implementation change | oracle after |
|---|---|---|---|---|
| PS-S4-1 | RS-D9 | D-ASSOC: 101 nodes on 4 corpora | impl `const`/`type` go to D (`impl_assoc_item`); no scope binding | 0 |
| PS-S4-2 | RS-D7 | D-FOREIGN (probe) | foreign fns are `function` | 0 |
| PS-S4-3 | RS-D10, RS-M5 | D-LOCAL: 137 nodes on 6 corpora; D-SUFFIX; block modules (probe) | block items and block modules to D, never in the `@n` counter or the module tree | 0 |
| PS-S4-4 | RS-I1, RS-I3 | I-1: 1034 wrong pairs | an unscanned crate's glob binds no name in import resolution | 0 |
| PS-S4-5 | RS-I3 | I-2: 4 wrong pairs | `macro_rules!` names and undeclared names never become crates | 0 |
| PS-S4-6 | RS-I2 | I-3: 3 wrong pairs (hoppscotch) | a scanned library only through a declared path dependency | 0 |
| PS-S4-7 | RS-I3 | I-4 (probe) | `extern crate x as y` aliases go to D | 0 |
| PS-S4-8 | RS-I4 | I-5 (probe) | edition 2015 leaves to D | 0 |
| PS-S4-9 | RS-F2 | D-REJECTED (probe) | a file with a tree-sitter syntax error claims only its file node | 0 |
| PS-S4-10 | RS-F3 | D-LONECR (probe) | a lone-CR file claims only its file node | 0 |

Step T020-S4X (coverage recovery) claims the forms PS-S4-1 and PS-S4-3
sent to D: impl associated `const`/`type` as members (RS-D9) and items in
function bodies under their function (RS-D10), numbered after the module
items so no module item's identity moves; the oracle compares them (kind,
name, qualified name with `@n`, span, hash, container). Block modules and
items in `const`/`static` initializers stay D (RS-D12).
