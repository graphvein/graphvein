# ADR 0071 - Rust structural claim contract: files, declarations, imports

**Status:** Accepted (delegated review, 2026-11-18)

## Context

T020 (ADR 0066) labels a language's structural scope (files, declarations,
imports) `precise` only through a rule-table row whose evidence includes a
claim contract with a 1:1 test, goldens, corpus checks, an oracle where
cheap and an independent review. The T020 investigation
(the T020 task record (internal)) inventoried every Rust form the
extractor (`internal/lang/rust`) emits on six corpora, the self-index and a
probe, and compared it with an oracle built from the Rust toolchain:
`syn` 2 over the raw bytes (declarations), `cargo metadata --no-deps
--offline` (crate roots) and `RUSTC_BOOTSTRAP=1 rustc -Zunpretty=hir-tree`
with every dependency an empty stub crate (module tree and `use`
resolution; `scripts/oracle/rust_structure/`).

Every module-level declaration and every `mod` edge agreed. Main still
stated wrong facts in ten classes (precision stops PS-S4-1..10): impl
associated `const`/`type` and items in function bodies emitted as module
items (101 and 137 nodes on the corpora, with a wrong `@n` for a later
module item), foreign functions as methods, 1034 `use` pairs captured by the
glob of an unscanned crate (`use diesel::prelude::*` binding every unknown
name, `std` included), `macro_rules!` re-exports read as crates, an in-repo
copy linked although the manifest takes the crate from git, `extern crate
x as y` aliases and 2015-edition paths read as crates, and tree-sitter
recovery declarations in files rustc rejects and lone-CR files.

## Options considered

1. **Narrow the scope** to declarations and `mod` edges. Loses `use`, the
   most useful import fact.
2. **Fix each stop as a class rule in the existing resolver**, with a
   separate import mode so calls and the framework consumers (Tauri, Axum,
   sqlx, Diesel), which have their own contracts, keep their behaviour.
3. **Run cargo or rust-analyzer at index time.** Needs the toolchain and
   dependencies on the user's machine and executes build metadata; rejected
   as in the investigation.

## Decision

Option 2, with the decisions of 2026-11-17 (all
precision-safe): every stop fixed to zero; O-S4-2 (a) the hir-tree oracle
stays an opt-in test pinned to rustc 1.97.0; O-S4-3 (a) edition 2015 leaves
claim nothing; O-S4-4 (a) external crates only for declared dependencies
and sysroot crates; O-S4-5 (a) unreadable manifests claim no crate roots;
O-S4-1 (developer decision 2026-11-17, option (a)): the multi-configuration
oracle below is the bar, and the entries no configuration reaches are a
known gap listed by count.

The contract is `docs/lang/rust-structure.md`, rows RS-M1..RS-M5,
RS-F1..RS-F3, RS-D1..RS-D11, RS-I1..RS-I5 and the default branch D,
mirrored by `structureRows` (`internal/lang/rust/structure.go`) and kept
1:1 by `TestRustStructureContract` (row ids, default forms, every row and
form reached by the fixture `testdata/fixtures/rust-structure`).

- **Declarations** (`Extract`): only module items are nodes. Items in
  function bodies, closures, `const`/`static` initializers and other blocks
  go to D (`local_item`), never take an `@n` and never enter the module
  tree; their calls stay with the enclosing function. A `mod` in a block
  and its `use` declarations go to D (`block_module`); its syntax scope
  keeps the function, so module lookups never select it. An impl's
  associated `const` and `type` go to D (`impl_assoc_item`) and bind no
  scope name. Extern block functions are `function`.
- **Whole-file refusals** (`Refuse`, called by the indexer): a file
  tree-sitter reports a syntax error in (RS-F2) or whose raw bytes hold a
  lone CR (RS-F3, `scan.File.LoneCR`) claims only its file node; every
  declaration, `mod` item and `use` leaf is counted and every other payload
  of the file (calls, Tauri, routes, queries) is dropped. `facts.RustFacts`
  gains `Unread`, `LoneCR`, `Rejected` and `ExternAliases`; the fact cache
  is reused only when `LoneCR` equals the scanned bit. Tree-sitter's error
  flag was chosen over a second Rust parser in Go: no corpus file has one,
  syn rejects every fixture file that has one, and a file tree-sitter
  accepts but rustc rejects keeps code-as-written facts that the oracle
  still checks (D-REJECTED counts them).
- **Manifests** (`readManifest`): the line reader also reads the edition
  (`edition`, `edition.workspace = true` from the nearest `[workspace]`
  root's `[workspace.package]`, 2015 when absent, as cargo) and every
  dependency table (`[dependencies]`, `[dev-dependencies]`,
  `[build-dependencies]`, `[target.*]` forms, `[dependencies.<key>]`
  sub-tables, dotted keys, one-line inline tables, `workspace = true` from
  `[workspace.dependencies]`). A value it does not read (a multi-line
  inline table, a multi-line string) makes that dependency's source
  `unknown`; one extern name with different sources is a `conflict`. A
  manifest it cannot read gives no root and no package (O-S4-5); a
  manifest without `[package]` gives none either (it used to give an
  unnamed package with conventional roots).
- **Import resolution** (`ResolveImport`, used only for `imports` edges):
  the resolver's strict mode, with its own memo. The glob of an unscanned
  crate binds no name (PS-S4-4); a name only it could supply goes to D
  (`external_glob_name`). First segments come from the extern prelude:
  `extern crate ... as` aliases (`extern_crate_alias`) and the file's
  `macro_rules!` names (`macro_name`) are refused; `std`, `core`, `alloc`,
  `proc_macro`, `test` are externals; the package's own library for its
  other targets; a declared dependency resolves into a scanned library only
  through a path dependency whose directory holds that package (PS-S4-6),
  else it is an external crate; an unknown or conflicting dependency is
  external unless a scanned library has that name (`undecided_crate`);
  anything else is refused (`undeclared_crate`). Leaves of an edition 2015
  crate (`edition_2015_path`), of a package whose edition cannot be read or
  of a file under no read package (`edition_unknown_path`) and `crate::`
  in a file no root reaches (`unrooted_crate_path`) claim nothing. A
  refusal in any crate holding the file refuses the leaf.
- **Default branch count**: one `info` diagnostic `rust_structure_unread`
  per file with the count per form (extractor forms cached in
  `facts.RustFacts.Unread`, linker forms added at link time).
- **Oracle** (`TestRustStructureOracle`, opt-in, never in CI): indexes the
  fixture (`GRAPHVEIN_RUST_STRUCTURE_ORACLE=1`) or the checkouts named in
  `GRAPHVEIN_RUST_STRUCTURE_ORACLE_CORPUS` in process, runs
  `scripts/oracle/rust_structure/run.sh` and fails on every disagreement
  about a claimed fact (D-REJECTED, D-LONECR, D-EXTRA, D-LOCAL, D-ASSOC,
  D-OTHERPOS, D-MISMATCH, D-FOREIGN, D-SUFFIX, I-WRONG). It skips with a
  reason unless rustc is 1.97.0 and the windows and macos rust-std
  components are installed. The comparator (`compare.go`) still starts no
  process; the test and `run.sh` do. Omissions (D-MISSING, I-MISSING),
  edges rustc resolved nothing for (I-UNPROVEN) and unreached entries are
  logged.
- Extractor version `extractors-42` (Python the implementation holds 40, TypeScript
  the implementation 41; re-checked at the merge).

No rule-table row is added and no label changes: every Rust file stays
`best_effort` until the independent review and the PL-L4 decision.

### cfg coverage (O-S4-1)

The oracle runs rustc for every cargo target in twelve configurations:
target OS linux (the host, `x86_64-unknown-linux-gnu`), windows
(`x86_64-pc-windows-msvc`) and macos (`x86_64-apple-darwin`), each with
`cfg(test)` on (`--test`) and off, and each with every feature of the
package on and with none (no default features). The windows and macos
standard libraries are the rust-std components of the pinned toolchain
(`rustup target add x86_64-pc-windows-msvc x86_64-apple-darwin`, 1.97.0,
from static.rust-lang.org, allowed by the developer on 2026-11-17; MIT OR
Apache-2.0), so `std` paths resolve as on those targets; dependency stubs
are built per target. A `mod` or `use` declaration is compared against the
union of rustc's targets over every configuration and cargo target that
reaches it (Graphvein's cfg union). Cost: twelve rustc runs per cargo
target (fixture 120 runs, axum 24, axum-ddd 192, kftray 324, crates.io 768,
hoppscotch 204, localsend 192, self 648); all seven checkouts took 37
minutes of wall time on the four-core container, about twelve times the investigation's
single configuration. Every comparison reports which configurations reached
each evidence entry (`graphvein_evidence_reached_in_<config>`).

Residual Graphvein evidence entries no configuration reaches (custom cfgs,
`debug_assertions` off, other architectures, files in no cargo target),
by corpus (the investigation's single configuration in brackets): axum 0 (0), axum-ddd 0
(0), kftray 2 (122: `crates/kftui/src/updater.rs` under
`cfg(not(debug_assertions))`), crates.io 0 (0), hoppscotch 22 (55: 9 under
Tauri's `desktop`/`mobile` cfgs, which tauri-build sets from a build script,
13 in the plugins' `desktop.rs`/`mobile.rs`, which no oracle configuration
compiles for the same reason), localsend 13 (13: `target_os = "android"`
code and flutter_rust_bridge's `target_family = "wasm"` section), self 11
(15: `testdata/repos/rust`, whose `broken.rs` stops rustc printing any
HIR). These 48 entries are the
known gap of O-S4-1 (a).

### Shared assumptions of the oracle

The oracle shares none of Graphvein's code. It shares: the file inventory
(the scanner's list of Rust files; the investigation checked it against `git ls-files`
and the differences are scanner ignore rules, an omission on both sides);
the identity and self-type rules of `docs/lang/identity.md`, which the syn
oracle transcribes to name declarations; the content hash normalisation
(CRLF and CR to LF, contract v1); and the evidence key (the start of a
`use`/`mod` declaration). The external crate behind a name is not compared:
both sides report the crate name only, and stubs give rustc no contents, so
a path into a workspace crate is finished by walking the module and `use`
tables rustc printed for that crate (`via_table`), as is a path through an
import that failed at a stub (`use header::X` after `use reqwest::header`).

## Consequences

- Corpus snapshots lose exactly the wrong nodes and edges of the stops and
  the refused leaves, and gain `rust_structure_unread` diagnostics (Review
  notes).
- Calls inside local functions are attributed to the enclosing function;
  a Tauri command declared inside a function body has no identity (the
  Tauri contract already allows an empty identity).
- Unresolved `use` leaves whose crate is undeclared no longer produce an
  external node.

## Known gaps

- (Closed by S4F: explicit `[[test]]`/`[[example]]`/`[[bench]]`/`[[bin]]`
  targets and the build script are crate roots; see "Review fixes".)
- Items are looked up before modules and without namespaces:
  `use crate::config::X` next to `fn config()` and `mod config;` resolves
  nothing (an omission, RS-I5). The `mod config;` edge itself resolves
  since S4F (each `mod` item resolves from its own attributes).
- (Closed by S4X: `mod r#impl;`, a raw identifier module name, resolves to
  `impl.rs`; see "Coverage recovery".)
- The scanner drops tracked files that match `.gitignore` (kftray
  `crates/kftui/src/core/*.rs`) and `vendor/` (hoppscotch): edges to and
  from them are omissions on both sides. Noted only.
- cfg entries no oracle configuration reaches (above) rest on the contract,
  the fixture and review.
- Edition 2015 crates claim no `use` edge (O-S4-3 (a)); `extern crate`
  aliases claim nothing.
- A registry dependency whose library name differs from its package name
  (`rust-ini` providing `ini`) is refused as undeclared.
- Items generated by macros (`include!`, `macro_rules!` invocations,
  derives) are never nodes (RS-D11).
- Calls stay Best effort with no contract; they still resolve through
  external globs and any scanned library of a name (the non-import mode).

## Review notes

Results (the Rust implementation, 2026-11-17, `extractors-42`, branch at the commit of
this ADR; counts are (declaration, target) pairs):

| checkout | declarations agreeing (syn) | `mod` / `use` declarations compared | Graphvein import pairs compared / agreeing | wrong | unproven | omissions (I-MISSING) | unreached |
|---|---|---|---|---|---|---|---|
| fixture | 60 | 21 / 36 | 40 / 40 | 0 | 0 | 13 | 0 |
| axum | 75 | 10 / 81 | 94 / 94 | 0 | 0 | 0 | 0 |
| axum-ddd | 244 | 43 / 199 | 274 / 274 | 0 | 0 | 0 | 0 |
| kftray | 4994 | 262 / 2393 | 2716 / 2714 | 0 | 2 | 20 | 2 |
| crates.io | 3819 | 468 / 4294 | 4187 / 4187 | 0 | 0 | 540 | 0 |
| hoppscotch | 680 | 85 / 391 | 536 / 536 | 0 | 0 | 14 | 22 |
| localsend | 1454 | 94 / 796 | 892 / 892 | 0 | 0 | 7 | 13 |
| self | 341 | 40 / 81 | 98 / 98 | 0 | 0 | 13 | 11 |

`TestRustStructureOracle` reports **0 disagreements on claimed facts**
(declarations: kind, name, qualified name, identity, span, hash, container,
extra nodes; imports: every Graphvein target of every compared `mod` and
`use` declaration) on all eight. The two unproven kftray pairs are `use
super::TunnelMessage;` inside `#[tokio::test]` function bodies
(`websocket_client.rs:1125`, `:1673`): rustc's error recovery records no
resolution for a function-body `use` through a failed stub import; by
inspection the parent module imports `TunnelMessage` from
`kftray_commons::models::tunnel_protocol`, the file Graphvein names. The
omissions are the refusals (crates.io: the `[[test]] path` target's
modules, `crate::` and the package's own library from files no root
reaches; `macro_rules!` and enum variant re-exports) and leaves Graphvein
leaves unresolved. The comparator's table walk now also finishes a path
through an import that failed at a stub (`use header::X` after `use
reqwest::{header, ...}`, two crates.io pairs that were unproven before).

Snapshot diff `extractors-39` (main `b1072bf`) → `extractors-42`, per
class (header and footer excluded):

- Declarations removed (PS-S4-1, PS-S4-3), each node with its `contains`
  edge: axum 9, axum-ddd 2, kftray 75, crates.io 140, hoppscotch 7,
  localsend 3 (exactly the investigation's D-ASSOC + D-LOCAL counts); self 14 (fixtures
  and the new fixture), plus `helper@2` renamed `helper` and `c_func` kind
  `method` → `function` in the new fixture.
- `use` evidence entries whose target changed (PS-S4-4, PS-S4-5): axum-ddd
  5 (`tracing_subscriber` → `std`, `tokio`, `metrics_exporter_prometheus`),
  kftray 3, crates.io 870 (`diesel` 896 and `googletest` 106 captures
  among the removed), hoppscotch 10 (`rayon`) plus 3 (`relay`,
  `tauri_plugin_appload`: in-repo copy → git external, PS-S4-6).
- `use` evidence entries removed by refusals (counted in
  `rust_structure_unread`): kftray 3 (`error`, `status`, `warning`
  macro re-exports), crates.io 288 (165 external, 123 file: files of the
  `[[test]] path` target and other unreached files, `unrooted_crate_path`
  and `undeclared_crate`; `external_glob_name`), self 25 (undeclared
  crates in fixtures without dependency tables, edition 2015 and alias
  cases of the new fixture).
- `use` evidence entries added: kftray 5 file edges (names an external glob
  used to make ambiguous), crates.io 58 external (names an external glob
  used to make ambiguous, now `std`, `insta`, ...); all agree with rustc.
- `unresolved_import` warnings removed where the leaf now resolves or is
  refused: kftray 8, crates.io 434, hoppscotch 1, self 2; external nodes
  with no edge left removed (kftray 3, crates.io 2, self 6) or added
  (crates.io 4, hoppscotch 4, self 1).
- `rust_structure_unread` diagnostics added: axum 4, axum-ddd 1, kftray 44,
  crates.io 242, hoppscotch 6, localsend 7, self 20.
- Calls (row D, no contract): calls of removed local functions now leave
  the enclosing function (kftray 32 removed / 18 added, crates.io 19 / 3,
  hoppscotch 3 / 0, localsend 1 / 0, self 1 / 0), with the matching
  `unresolved_calls` diagnostics and file-node `unresolved_*` attributes;
  where such calls stay unknown only their caller changes: axum
  `src/http/types.rs`, whose file node moves 4 unknown call sites from the
  removed `StrVisitor.*@Visitor` methods to
  `Timestamptz.deserialize@Deserialize` (corrected by S4F).

Nothing else changed.

## Review fixes (S4R)

The independent review (the T020 task record (internal)) found one class A
finding (A1), four class B findings (B1-B4) and seven follow-ups (C1-C7).
Step S4F fixed each as a class rule, kept `structureRows`, the contract
document and `TestRustStructureContract` 1:1, and added a fixture case per
fixed form (`app/src/alts.rs`, `targets/`, `old/src/sub.rs`,
`app/src/unreached.rs`; table-access-sqlx `local_fn`). Extractor version
stays `extractors-42` (the branch is not merged; the payload gained only
the optional `RustQuery.Local`).

- **A1, cfg alternatives of a module name.** The module tree keeps every
  `mod` item of a name in a scope as an alternative (`<name>`, `<name>#2`,
  ...; inline module instances by their item span), and each outlined `mod`
  item resolves from its own `#[path]` and the directory of the module
  instance holding it (`rustCrate.modItems`); the `mod` edge no longer
  resolves the name. Import resolution through a name with several
  alternatives resolves the rest of the path in each and claims only when
  all reach the same file or crate; otherwise the leaf goes to D, counted
  as the new form `cfg_module_alternatives` (RS-I1). Calls (no contract)
  keep the first alternative. A file reached as several modules resolves
  in each (agreement or no edge).
- **B1.** A file loaded through `#[path]` is a mod-rs file for its own
  `mod` items (RS-M2, RS-M3 text).
- **B2.** Comments and doc comments between an item's outer attributes and
  the item no longer reset the pending `#[path]` (the Tauri and cfg
  attribute walks already skipped them).
- **B3.** Import resolution that follows a `use` declaration (explicit or
  glob) of an edition 2015 or unknown-edition crate refuses the leaf
  (`edition_2015_path` / `edition_unknown_path`, RS-I4 text).
- **B4 and the decision on explicit targets** (contract change
  of RS-M1, precision-safe): crate roots follow cargo 1.97's target
  discovery, probed with `cargo metadata`: `autolib`, `autobins`,
  `autotests`, `autoexamples`, `autobenches`; edition 2015 packages with
  explicit targets of a kind discover none of it; explicit `[[bin]]`,
  `[[test]]`, `[[example]]`, `[[bench]]` targets by `path` or by `name`; a
  discovered target whose name or path an explicit one takes is dropped;
  the build script (`build` key, `build.rs`), whose extern prelude is its
  `[build-dependencies]` without the package's library. A target cargo
  cannot locate refuses the package (`config_parse_error`), as cargo
  refuses the manifest. The oracle now compares Graphvein's roots with
  `cargo metadata`'s targets per manifest (R-EXTRA fails, R-MISSING is
  logged; `StructureRoots` in the test package).
- **C1.** `readManifest` reads TOML statements: all four string forms
  (multi-line ones with their escapes and line-ending backslash), arrays
  and inline tables over several lines (TOML 1.1, which cargo 1.97
  accepts), comments. A value it does not decode (a TOML 1.1 `\x` or `\e`
  escape) is unknown; an unclosed value, a structural table defined by a
  top-level key (`lib.path = ...`), a dotted form of a read key or a key
  of the wrong type refuses the manifest.
- **C2.** An external crate node is named by the dependency's package
  (`ser = { package = "serde" }` gives `cargo:serde`; identity.md). The
  extern name is not kept as a node attribute: the node is shared by every
  importer, which may call it differently. The oracle maps rustc's extern
  name to the package name from `cargo metadata`.
- **C3.** `TestRustStructureContract` checks that every refused site emits
  nothing: no node for a refused declaration, no imports evidence for a
  refused `mod` item or for a `use` declaration none of whose leaves
  claimed, nothing beyond the file node for an RS-F2/RS-F3 file. An
  unresolved or ambiguous `mod` item reports its row (RS-M2 or RS-M3).
- **C4.** The calls bullet of the Review notes now names axum's
  `src/http/types.rs` attribution change.
- **C5.** Documented under Known gaps (coverage); the `mod` edge half is
  fixed by A1.
- **C6.** A sqlx site in a local function keeps the enclosing function as
  owner (RS-D10, table-access W1), and records the local function's span
  (`RustQuery.Local`): only an execution call inside that local function
  makes it `executes`. Without this rule the fixture's site under an
  enclosing `.execute({ fn pick() { sqlx::query(..) } .. })` read
  `executes` (mutation-checked); with it, `builds`, as before S4.
- **C7.** Compacted self types keep one space between two words:
  `impl dyn m::Tr` gives `dyn m::Tr.f` (identity.md; the syn oracle's
  transcription follows the same rule).

Mutation check: reverting each fix (alternatives, mod-rs `#[path]` files,
comment skipping, the edition refusal through imports, the discovery keys,
explicit targets, build roots, package naming, self-type spacing, the
multi-line string tracking, an edge emitted from a refused leaf) fails
`TestRustStructureFixture`, `TestRustStructureContract`, `TestPackageRoots`
or `TestManifestStatements`.

Results (S4F, `extractors-42`, 12 configurations; counts as in the Review
notes table; roots = Graphvein crate roots compared with `cargo metadata`
targets / agreeing):

| checkout | declarations agreeing (syn) | `mod` / `use` declarations compared | import pairs compared / agreeing | roots compared / agreeing | wrong (incl. R-EXTRA) | unproven | omissions (I-MISSING; R-MISSING) | unreached |
|---|---|---|---|---|---|---|---|---|
| fixture | 90 | 35 / 53 | 67 / 67 | 16 / 16 | 0 | 0 | 15; 0 | 0 |
| axum | 75 | 10 / 81 | 94 / 94 | 2 / 2 | 0 | 0 | 0; 0 | 0 |
| axum-ddd | 244 | 43 / 199 | 274 / 274 | 16 / 16 | 0 | 0 | 0; 0 | 0 |
| kftray | 4994 | 262 / 2393 | 2716 / 2714 | 27 / 27 | 0 | 2 | 20; 0 | 2 |
| crates.io | 3819 | 468 / 4294 | 4901 / 4901 | 64 / 64 | 0 | 0 | 28; 0 | 0 |
| hoppscotch | 680 | 85 / 391 | 536 / 536 | 17 / 17 | 0 | 0 | 14; 0 | 22 |
| localsend | 1454 | 94 / 796 | 892 / 892 | 16 / 16 | 0 | 0 | 7; 0 | 13 |
| self | 372 | 54 / 98 | 125 / 125 | 60 / 60 | 0 | 0 | 15; 0 | 11 |

The review's probe workspaces p1-p5 (the T020 task record (internal))
also report 0 disagreements (p1 14/14 import pairs, p2 13/13, p3 1/1, p4
2/2, p5 18/18; roots 11/11). crates.io's omissions fall from 540 to 28:
its `[[test]] path` target (`src/tests/...`) is a crate root, so 714 more
import pairs are claimed and agree. The residual unreached entries are
unchanged (48: kftray 2, hoppscotch 22, localsend 13, self 11). kftray's
numbers are from a re-run: the first run's macos+test+features
configuration lost three bin targets (`kftui`, `bump_version`,
`generate_icons`) to rustc internal compiler errors that were a full disk
("failed printing to stdout: No space left on device" while printing the
HIR), which left their ICE reports in the kftray checkout. The runner now
compresses the HIR (`gzip -1`, about fifty times smaller), sends rustc's ICE
reports to the scratch directory (`RUSTC_ICE`, one directory per run) and
reports them (`M-RUSTC-ICE`); the re-run had no ICE, no `M-RUSTC-FAILED`
and the S4B omission count (20).

Snapshot diff S4B (`833571d`) → S4F, per class (same inputs, self = the
S4F tree for both):

- axum, axum-ddd, hoppscotch, localsend: no change.
- kftray: 6 call sites in the second of two `#[cfg] mod imp { .. }` blocks
  of `crates/kftray-tauri/src/detached_stderr.rs` now resolve in their own
  inline module instance's scope (unknown → external; calls, row D).
- crates.io: the `[[test]] path` target's files are rooted: 513 `use`/`mod`
  evidence entries added (all files, agreeing with rustc), 1246 call edges
  added, `unresolved_module` −161, `rust_structure_unread` −132/+31,
  `unresolved_import` −11/+75 (leaves in the newly rooted files that resolve
  nothing, RS-I5); `dynAppError.is` renamed `dyn AppError.is` (C7).
- self: the fixture's new cases (A1, B1, B2, B3, B4, C2, C7, the build
  script root) and the sqlx `local_fn` case; plus one indexing-time Go
  parse timeout on `internal/extract/schema/pgcatalog.go` under load in
  that run (not a code change; also present in the `extractors-39` run).

Against `extractors-39` (main `b1072bf`), the S4B classes above stand, plus
the crates.io test-target class and the kftray inline-instance calls.

## Review notes (S4R, pending acceptance)

Independent review (the T020 task record (internal), on `833571d`).
Verdict, verbatim: "**The Rust structure flip (PL-L4) is not ready.** One
class A finding: a module name declared twice in one scope (the
cfg-selected `#[path] mod sys;` pair, or an inline stub next to an
outlined module) makes Graphvein link the second `mod` item to the first
item's file. The form is ordinary platform code (3 of 7 small crates
sampled from crates.io use it) and has a small class-level fix. Four class
B findings (rarer forms, one doc/status mismatch) and seven follow-ups."

Fixes (`8b1e46e`, results `ce8a6c1`, oracle runner
`cdaac7d`; section "Review fixes" above): Every class A and B finding is
fixed as a class rule, still at `extractors-42`: A1 cfg alternatives of a
module name (each `mod` item resolves from its own attributes and module
instance; imports through several alternatives claim only when they
agree, `cfg_module_alternatives`), B1 `#[path]` files are mod-rs files, B2
comments between attributes and the item, B3 the edition 2015 refusal
through imports, B4 cargo 1.97 target discovery (explicit targets, `auto*`
keys, build scripts) checked against `cargo metadata`; the follow-ups C1-C7
fixed or recorded under Known gaps. Each fix has a fixture case and a
mutation check that fails a test when it is reverted. No class A finding
is open.

Corpus and oracle results at the flip (S4F, `extractors-42`, the code of
this flip apart from labels): TestRustStructureOracle reports 0
disagreements on claimed facts under 12 configurations (target OS linux,
windows, macos; `cfg(test)` on and off; all features and none) on the
fixture (90 declarations, 67 import pairs, 16 crate roots), axum (75, 94,
2), axum-ddd (244, 274, 16), kftray (4994, 2714 of 2716 agreeing, 2
unproven, 27), crates.io (3819, 4901, 64), hoppscotch (680, 536, 17),
localsend (1454, 892, 16) and the self-index (372, 125, 60). The oracle
(syn 2, `cargo metadata` and rustc 1.97.0 `-Zunpretty=hir-tree`, 12
configurations) is opt-in and never runs in CI: CI runs the contract test,
the fixture goldens and the label tests; the oracle reruns by hand at a
contract change. Known gap of O-S4-1 (a): 48 Graphvein evidence entries no
oracle configuration reaches (kftray 2, hoppscotch 22, localsend 13, self
11) are claimed as the contract writes them and rest on the contract, the
fixtures and review only.

Flip (`extractors-42`): row PL-L4 of
`docs/extract/precision.md` labels Rust files `precise` (tests, examples,
benches and build scripts included); calls stay row D (`code_calls`). A
Rust file whose structure claims nothing (RS-F2, RS-F3, not parsed, not
extracted or dropped by validation) is stored `best_effort`, served with
reason `structure_refused`. A Rust file reached from no crate root of a
read `Cargo.toml` (an orphan) is stored `best_effort` with an info
diagnostic `structure_unrooted`, served with reason `structure_unrooted`:
its declarations are claimed, but its imports are not resolved as a crate
member (outlined `mod` items resolve nothing, `crate::` is refused) and no
cargo target compiles it, so no oracle configuration checked them
(precision-safe default of step S4L).

S4L checks (`extractors-42`, the flip commit): `TestRustStructureOracle`
re-run under the 12 configurations on the fixture (90 declarations, 67/67
import pairs, 16/16 roots), axum (75, 94/94, 2/2) and axum-ddd (244,
274/274, 16/16): 0 disagreements, the S4F numbers; the other corpora are
cited from S4F above (same code apart from labels). Snapshot diff against
`cdaac7d` on the seven checkouts, with the file `precision` (record and
file node) and the `structure_unrooted` diagnostics projected out:
identical. Rust file labels after the flip: axum 12 precise, axum-ddd 46,
kftray 270, crates.io 528 precise and 1 unrooted
(`crates/crates_io_trustpub/src/keystore/impl.rs`, reached only through
`mod r#impl;`, the raw-identifier known gap), hoppscotch 105, localsend
110, self 116 precise, 34 unrooted (fixtures without a reachable root:
`rust-structure` 16, `diesel-models` 15, `clients` 1, `repos/rust` 1,
`spike` 1) and 3 refused (`structure_refused`: the fixtures' syntax-error
files). No file of another language changed label.

This section was accepted with the ADR (delegated review, 2026-11-18;
label corrected 2026-11-28).

## Coverage recovery

**Status:** Accepted with the ADR (delegated review, 2026-11-18; see "Review notes (acceptance, 2026-11-18)"; label corrected 2026-11-28).

Developer principle of the session: maximum coverage with no confident
wrong fact; a correctly modelled form that went to D only for convenience
is recovered when a moderate change does it and the oracle can check it.
Three forms are recovered, still at `extractors-42` (the branch is not
merged; see open points):

- **RS-D9, impl associated `const` and `type`** (PS-S4-1's D-ASSOC):
  claimed as members exactly as methods: `constant` / `type_alias`,
  `Type.NAME` or `Type.NAME@Trait`, any visibility, contained by the file,
  attributes `module`, `receiver`, `trait`, `receiver_path`. They bind no
  scope name (the syntax scopes skip them as before), so calls and `use`
  leaves never resolve to them. `const _` (syn accepts it, rustc's parser
  rejects it) and a `static` in an impl stay D (`impl_assoc_item`).
- **RS-D10, items in function bodies** (PS-S4-3's D-LOCAL): every form of
  RS-D1..RS-D9 inside the body of a claimed function, method or trait
  default method, at any depth of blocks, closures and `match` arms, is
  claimed under that function: `run.helper`, `S.m.inner`,
  `run.L.new` (an impl in the body), `run.LT.req` (a trait in the body,
  contained by the trait), nested bodies recursively. The prefix is the
  owner's qualified name without its `@n`, the lexical-owner convention of
  Python and TypeScript (identity.md). The enclosing function contains the
  item; the node carries `local: true`.
- **RS-M2, `mod r#impl;`**: the file and directory of a module named by a
  raw identifier drop the `r#` (rustc); the module tree keeps the name as
  written, so `use r#impl::X` follows it. A path through the other
  spelling (`foo` for `mod r#foo;`) resolves nothing: the declared-module
  check compares file names, so it shadows the extern prelude instead of
  reaching a dependency of that name (fixture `mod r#ser;` with
  `use ser::Deserialize`, mutation-checked).

What stays D, as RS-D12 (`local_item`): items in `const`/`static`
initializers (module level, impls and function bodies), a block's `mod`
and its items (RS-M5), and the bodies of items that claim nothing. syn
checks these positions unambiguously too, but an initializer is no
function body and a block module is a module scope Graphvein does not
model, so naming them under an owner would be a new convention for little
coverage (fixture and corpora: under ten sites).

Decisions:

- **Numbering in two passes.** Module items (RS-D1..RS-D8) are numbered
  first in source order; associated items and body items then, in source
  order, take the next number of their qualified name. A body item can
  never move a module item's identity (`mod run { fn helper }` keeps
  `run.helper`; `fn helper` in `fn run` is `run.helper@2`). One counter per
  file, as before, so identities stay unique. Implementation: the
  extractor queues these items and adds them after the walk, after the
  syntax scopes and the Tauri capture.
- **No other consumer sees them.** The syntax scopes and the Tauri capture
  run before the queued nodes are added, so a body item binds no scope
  name and a Tauri command declared in a body keeps an empty identity; at
  link time the extern prelude's `macro_name` refusal and the method
  lookup of calls skip `local` nodes (fixture `shadow`, `caller`,
  mutation-checked).
- **Calls.** Calls inside a body item keep the enclosing module item as
  caller, and calls to a body function stay unknown. Binding them would
  need value-namespace scoping (a `let`, `match` arm or closure parameter
  of the same name shadows a body `fn`; Graphvein tracks only some), so the
  resolution is not lexically certain. sqlx sites keep their owner and
  `RustQuery.Local` rule (table-access-sqlx unchanged apart from two new
  body nodes).

Oracle (syn transcription): `main.rs` names a body item under its owner
site and records each site's parent (a trait for its methods, the
enclosing function for a body item); `compare.go` numbers module sites
first and the rest second, and checks every claimed fact of the new sites
as for module items (kind, name, qualified name with `@n`, identity, span,
hash, container = the parent's expected identity). Trait methods now also
check their container through the parent's numbered identity (before, the
trait's unsuffixed name). Mutation check: containing body items by the
file instead of their owner gives four D-MISMATCH on the fixture.

Results (S4X, `extractors-42`, 12 configurations; declarations agreeing,
of which recovered body items / associated items; `mod`/`use` declarations
compared; import pairs compared / agreeing; roots):

| checkout | declarations (body / assoc) | `mod` / `use` | import pairs | roots | wrong | unproven | omissions (I-MISSING) | unreached |
|---|---|---|---|---|---|---|---|---|
| fixture | 140 (27 / 9) | 38 / 56 | 72 / 72 | 16 / 16 | 0 | 0 | 16 | 0 |
| axum | 84 (5 / 4) | 10 / 81 | 94 / 94 | 2 / 2 | 0 | 0 | 0 | 0 |
| axum-ddd | 246 (0 / 2) | 43 / 199 | 274 / 274 | 16 / 16 | 0 | 0 | 0 | 0 |
| kftray | 5070 (47 / 29) | 262 / 2393 | 2716 / 2714 | 27 / 27 | 0 | 2 | 20 | 2 |
| crates.io | 4053 (84 / 150) | 468 / 4294 | 4906 / 4906 | 64 / 64 | 0 | 0 | 23 | 0 |
| hoppscotch | 687 (7 / 0) | 85 / 391 | 536 / 536 | 17 / 17 | 0 | 0 | 14 | 22 |
| localsend | 1457 (3 / 0) | 94 / 796 | 892 / 892 | 16 / 16 | 0 | 0 | 7 | 13 |
| self | 431 (32 / 9) | 57 / 101 | 130 / 130 | 60 / 60 | 0 | 0 | 16 | 11 |

`TestRustStructureOracle` reports 0 disagreements on claimed facts on all
eight; no D-MISSING (every recovered site syn finds is emitted). The two
kftray unproven pairs and the 48 unreached entries are S4F's. crates.io
gains 5 import pairs and loses 5 omissions: `keystore/impl.rs` is rooted
through `mod r#impl;`. The fixture and self gain the S4X cases (`mod
r#ser;` adds one omission each: rustc reports the ambiguity, Graphvein
resolves nothing).


Snapshot diff `9c0b3bc` (S4L) → S4X on the seven checkouts (self = the
S4X tree for both binaries), per class: nodes added with their `contains`
edge, body items (RS-D10) + associated items (RS-D9): axum 5 + 4,
axum-ddd 0 + 2, kftray 47 + 29, crates.io 84 + 150, hoppscotch 7 + 0,
localsend 3 + 0, self 32 + 9 (fixtures); `rust_structure_unread`
diagnostics removed or recounted where these were the file's only or some
of its forms (axum 4, axum-ddd 1, kftray 43, crates.io 105 removed / 16
recounted, hoppscotch 6, localsend 7, self 6 / 2). Raw identifier class,
crates.io only: `crates/crates_io_trustpub/src/keystore/impl.rs` is rooted
(label `best_effort` → `precise`, `structure_unrooted` removed), the `mod
r#impl;` edge and `pub use r#impl::RealOidcKeyStore` resolve (4 imports
edges, 5 evidence entries, all agreeing with rustc: `mod.rs`'s `mod` and
`pub use`, `impl.rs`'s two `super::` uses, `src/server.rs`'s
`crates_io_trustpub::keystore::RealOidcKeyStore`), `unresolved_module` −1,
`unresolved_import` −4, and 2
call edges (row D) newly resolved in `impl.rs` and `src/server.rs` with
their `unresolved_calls` counts. self: the S4X fixture cases only
(`locals/impl.rs`, `locals/ser.rs` rooted). Nothing else changed: no call,
Tauri, route or query fact moved (calls keep their enclosing module item;
the syntax scopes and Tauri capture never see the new nodes). Label flips:
crates.io 529 precise, 0 unrooted (was 528 and 1); the other corpora
unchanged.

## Review notes (acceptance, 2026-11-18)

Accepted after delegated review (2026-11-17). Steps: the investigation (the T020 task record (internal), oracle decision by the developer, O-S4-1), S4B implementation, one independent review (the T020 task record (internal): one class A, four class B, C1-C7), S4F fixes (A1 and B1-B4 fixed as class rules and mutation-checked; explicit cargo targets and `build.rs` as crate roots; C1-C4, C6, C7), S4L (row PL-L4, label flip, `structure_unrooted`), S4X (coverage recovery: RS-D9, RS-D10, RS-D12, raw-identifier `mod`), S4I (merge with the Python and TypeScript structure stacks).

- Results: the oracle (`TestRustStructureOracle`: syn 2.0.106 over raw bytes, `cargo metadata --offline`, rustc 1.97.0 HIR tree, 12 cfg configurations; opt-in, not in CI) reports 0 disagreements on the fixture and seven checkouts (axum, axum-ddd, kftray, crates.io, hoppscotch, localsend, self); after the merge it was re-run on the fixture, axum, crates.io and the self-index with 0 disagreements. Crate roots equal cargo metadata on every checkout.
- Coverage after the flip (precise files): axum 12, axum-ddd 46, kftray 270, crates.io 529, hoppscotch 105, localsend 110, self-index 116 (fixtures deliberately unrooted or refused stay best effort).
- Wrong facts removed from main: 1,041 wrong `use` targets found by the investigation (globs from unscanned crates, `macro_rules!` re-exports as fake crates, in-repository copies of git dependencies), 238 wrong declaration nodes (impl-associated items and items in function bodies as module items; both now claimed with correct owners after S4X), a duplicate cfg `mod` name linked to the wrong file (A1), a local-function sqlx fact (C6).
- Known gaps and after-v1 items: as listed above, including 48 unreached cfg entries; calls stay row D (`code_calls`); calls to function-body items are not bound (local shadowing is not modelled).
- Gates: local full check 0 FAIL, 172 `/postgres` PASS; CI run 44226882272 on 5905b7e (`[ci windows]`) green, postgres raw log 0 FAIL, 173 `/postgres` PASS, Windows 0 FAIL.
