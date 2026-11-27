# rust-structure fixture

Rust structural claim contract (`docs/lang/rust-structure.md`, ADR 0071,
task T020, the implementation), rebuilt from the investigation probe
(section 6 of the T020-S4 inventory, an internal record) with the cases the implementation added.
Nothing here is built or run by the tests: the manifests are read by
Graphvein's resolver and, in the opt-in oracle, by `cargo metadata` and
`rustc -Zunpretty=hir-tree` (every dependency an empty stub crate). Several
files are deliberately not valid Rust (`app/src/broken.rs`) or not valid
manifests (`bad/Cargo.toml`).

Tests:

- `TestRustStructureFixture` (`internal/lang/rust`) indexes this directory
  and checks `testdata/fixtures/rust-structure.golden.json`, then a warm
  replay.
- `TestRustStructureContract` checks that the fixture reaches every
  contract row and the default branch, and counts every default form.
- `TestRustStructureOracle` (opt-in, `GRAPHVEIN_RUST_STRUCTURE_ORACLE=1`)
  compares the claimed facts with syn, cargo metadata and rustc.

The repository stores every file with LF line endings, so every test
writes two more cases into its temporary copy (`structureExtraFiles` in
`internal/lang/rust/structure_harness_test.go`): `app/src/crlf.rs` (CRLF)
`app/src/lonecr.rs` (a lone CR ends a `//` comment for Graphvein, not
for Rust: RS-F3) and `app/src/nul.rs` (raw NUL bytes in a block comment
and a byte string, which Rust allows: the scanner reads the file as
source and RS-D1 claims `nul`). `app/src/bom.rs` starts with a byte order
mark.

No file uses an ignored name (no `target/`, `build/`, `dist/`), so no inert
names are needed.

| path | rows | case |
|---|---|---|
| `Cargo.toml` | RS-M1 | virtual workspace: no package; `[workspace.package] edition` and `[workspace.dependencies] serde` |
| `app/Cargo.toml` | RS-M1, RS-I2, RS-I3 | lib renamed `app_lib`; path dependency `util` renaming `probe-util`; `serde = { workspace = true }`; `relay` from git although `relay/` is scanned (PS-S4-6); `widget` with a TOML 1.1 `\x65` escape Graphvein does not read (`undecided_crate`; the schema reader reports `schema_manifest_unreadable` for it); `old` with a one-line multi-line literal string (read, S4F C1); `ser`, a renamed `serde` (external node `cargo:serde`, S4F C2) |
| `app/src/lib.rs` | RS-M2..RS-M5, RS-D1..RS-D11, RS-I1..RS-I5, D | every `mod` form (non-mod-rs `net.rs` + `net/client.rs`, `both.rs` with `both/mod.rs` (E0761), `#[path]` outlined, inline and inside inline, `cfg` modules), `pub use` and glob re-exports, `::util::Root`, raw identifiers, `include!`, `macro_rules!` and its invocation, impl associated `const`/`type` (RS-D9 since S4X: `Wrapper.ASSOC`, `Wrapper.Item@Iterator`), trait items, extern block fn and statics, a body `fn helper` before the module-level one (RS-D10 since S4X: `outer.helper`, contained by `outer`), a block `mod fnmod` with a `use` (D), `extern crate core as kore` (`extern_crate_alias`), a `macro_rules!` re-export (`macro_name`), a name only `serde`'s glob could supply (`external_glob_name`), an item in a `const _` initializer (`local_item`) |
| `app/src/main.rs`, `app/src/bin/tool.rs`, `app/tests/it.rs` | RS-M1, RS-I2 | binaries and a test using the package's own library; `probe_app` is no extern name (`undeclared_crate`) |
| `app/build.rs`, `app/helper.rs` | RS-M1, RS-M2, RS-I1, RS-I3 | the build script, a crate root since S4F: `mod helper;` → `helper.rs`, `crate::helper::run`, `std` external |
| `app/src/unreached.rs` | RS-I1, RS-D9, D | a file no root reaches: `crate::` refused (`unrooted_crate_path`); `impl Unnamed { const _: u8 = 0; }`, which syn accepts and rustc rejects (no root compiles it), refused (`impl_assoc_item`) |
| `app/src/locals.rs`, `app/src/locals/` | RS-D9, RS-D10, RS-D12, RS-M2, RS-I3, RS-I5 | S4X coverage recovery: every item form in a function body (`run.helper@2` after the module-level `mod run { fn helper }`, `run.helper@3` in an `if` block, `run.in_closure`, a body impl's `run.L.new` and `run.L.Z`, a body trait's methods `run.LT.req` contained by `run.LT`, `pub const`/`static`, extern block items, `macro_rules!`), items in a method body (`S.m.inner`, `S.m.inner.Deep`) and a trait default body (`Tr.d.in_default`), cfg alternatives `plat`/`plat@2` with `plat.h`/`plat.h@2`; items in `const` initializers stay D (`local_item`); associated items `S.A`, `S.T`, `S.B@Tr`, `S.Item@Iterator`; calls to body functions stay unknown; a body `macro_rules! serde` does not refuse `use serde::Serialize` (`macro_name` reads module items); `mod r#impl;` loads `locals/impl.rs` and `pub use r#impl::Raw` follows it; `mod r#ser;` with `use ser::Deserialize` (the plain spelling) resolves nothing instead of the dependency `ser` |
| `app/src/alts.rs`, `app/src/alts/` | RS-M2, RS-M3, RS-I1, RS-I3, RS-I4, D, RS-D5 | Later fixes: a cfg pair `#[cfg(unix)] #[path] mod sys;` / `#[cfg(windows)] #[path] mod sys;` (each item its own file, A1), `use sys::Shared` (both alternatives reach `common.rs`: edge) and `use sys::Handle` (different files: `cfg_module_alternatives`); an inline `mod backend {}` next to a cfg'd `mod backend;` (edge to `alts/backend.rs`, `use backend::run` to D); `#[path = "alts/foo/bar.rs"] mod bar;` whose `mod c;` loads `alts/foo/c.rs`, not the decoy `alts/foo/bar/c.rs` (B1); a comment and a doc comment between `#[path]` and its `mod` item, decoys at the default places (B2); `use probe_old::sub::X` through the edition 2015 crate's re-export (`edition_2015_path`, B3); `use ser::Deserialize` (`cargo:serde`, C2); `impl dyn m::Tr` (`dyn m::Tr.f`, C7) |
| `targets/` | RS-M1, RS-M2, RS-I1, RS-I2, D | S4F target discovery: `autobins = false` (`src/main.rs` and `src/bin/auto.rs` are no roots: their `mod helper;` unresolved), `autoexamples = false` with `[[example]] name = "demo"` (`examples/demo/main.rs` a root, `examples/other.rs` none), `[[bin]] path`, `[[test]] path = "checks/smoke.rs"`, discovered `tests/auto.rs`, `build = "tools/gen.rs"` with `[build-dependencies] util` (RS-I2) and no access to the package's library or `[dependencies]` (`undeclared_crate`); a multi-line `description` holding `[lib]` and `path = ...` lines (one value, C1) |
| `app/src/broken.rs` | RS-F2 | a syntax error: nothing beyond the file (`syntax_error`) |
| `app/src/patattr.rs`, `app/src/patattr/inner.rs` | RS-F4, RS-D1, RS-D2, RS-D10, RS-M2 | T054: `#[cfg]` and `#[allow]` attributes on struct pattern fields (two `let` patterns, one with a renamed field and `..`), which tree-sitter-rust 0.24.2 reports as an error; statement attributes near the error (blanked in the first round, restored in the second) and `#[...]` text in a string; read from the verified second parse, so `Services`, `split`, `split.local_helper`, `by_field`, `after` and `mod inner;` (`patattr/inner.rs` rooted) are claimed |
| `app/src/patbroken.rs` | RS-F2 | T054: a struct pattern field attribute and a real syntax error: RS-F4 does not read it, nothing beyond the file (`syntax_error`) |
| `app/src/globbed.rs` | RS-I3 | `pub use serde::de::*;` |
| `util/` | RS-M1, RS-I1 | `edition.workspace = true`; a private module re-exported |
| `relay/`, `widget/` | RS-M1 | scanned libraries that are not the declared dependency |
| `old/` | RS-I4 | edition 2015: every `use` leaf to D (`edition_2015_path`), `mod` edges kept; `sub.rs` re-exports `m::Thing` (crate-relative in 2015, `old/src/m.rs`) next to an inline `mod m` |
| `bad/` | RS-M1, RS-I4 | a manifest Graphvein cannot read (`name = 123`): no root, no package (`edition_unknown_path`) |
| `stray/orphan.rs` | RS-F1, RS-I4 | a file under no package |
