# Rust module resolution

ADR 0016 defines one current-inventory resolver for imports, calls, Axum objects,
handlers and prefixes. Cache syntax only; rebuild crate/module inventory and
completed lookup memo tables every link pass. Aggregate version: extractors-49.
Import edges follow the Rust structural claim contract
([rust-structure.md](rust-structure.md), ADR 0071) through the resolver's import
mode (`ResolveImport`); calls and the route, Tauri, sqlx and Diesel consumers use
the mode described here.

## Crates and roots

Each scanned Cargo.toml package contributes its library and binary targets. Read
package.name, lib.name/lib.path and bin.path string values. Library defaults are
src/lib.rs and the package name with hyphens changed to underscores. Binary roots
are explicit paths, src/main.rs, src/bin/*.rs and src/bin/*/main.rs. Conventional
tests/*.rs/tests/*/main.rs, examples and benches are independent Cargo roots too.
Workspace tables alone do not define a crate, nor does an empty manifest. Invalid
relevant metadata emits config_parse_error and gives no root (ADR 0071, O-S4-5).
The reader also reads the edition and the dependency tables, which only the
import mode uses. Unrelated dependency tables are
never executed or fetched. Duplicate library names emit duplicate_crate_name on
each declaring Cargo.toml (one per manifest, in path order, listing every root)
and cannot resolve. Visibility is intentionally unchecked; declared dependencies
are checked by the import mode only.

## Modules

Crate roots and mod.rs are mod-rs files. Ordinary modules probe x.rs and x/mod.rs
under the module directory; in non-mod-rs foo.rs the directory is foo/. Both
candidates present emits ambiguous_module with both paths and resolves neither.
Inline modules add directory components. An outlined #[path] outside inline
modules is relative to the source directory; inside inline modules it uses the
inline directory, including foo/ in a non-mod-rs file. A #[path] on an inline
module changes that directory. String decoding preserves spaces. Files unreachable
from roots retain file-local and inline resolution. A mod item inside a function
body is no module of the file (its scope keeps the function).

## Paths, scopes and re-exports

crate::, self:: and repeated super:: traverse current modules. Leading :: searches
the extern prelude, bypassing lexical module names. Bare paths search lexical
items, declared modules and explicit use bindings, then globs, scanned libraries
and external paths. Groups, grouped self, aliases, as _, pub use re-exports and
function/block-scoped uses are retained. An import cannot provide its own first
segment; external crate names use the extern prelude. An opaque external glob
does not hide its own crate name or a scanned library name. as _ contributes an import dependency
without a binding. Explicit names shadow globs. Several globs resolve only when
they agree on one terminal declaration. Per-name cycle guards terminate lookups;
only completed traversals are memoized. Repeated callables remain ambiguous.
Unavailable explicit bindings do not fall through to external dependencies.

Import edges target scanned files or existing cargo external identities. Calls
target emitted free functions and existing method identities where a named type
resolves; no instance/type inference is added. Associated methods are not bare
lexical free functions. Trait default bodies still contribute syntax. Bare unavailable calls retain
unknown-call accounting. Symbol identities remain unchanged, including outlined
modules' file-local names and inline modules' lexical names.

## Route consumers and limitations

Private constants are syntax bindings without new nodes; an impl's associated
constants are not module bindings, and items inside function bodies have no node
(their lexical binding shadows outer names without an identity). Literal strings and
const/static &str items initialized directly with literals prove paths, including
imported items. No concatenation, format!/concat!, variable or arbitrary initializer
evaluation occurs. Router bindings obey source order and block shadowing. Named
factory tail/return expressions reuse one internal logical object across call sites,
including conditional alternatives. Deferred mounts retain recursive factories;
composition traversal uses per-chain cycle guards. These objects are not contract nodes.
Method-router variables stay dynamic. See [routes.md](../extract/routes.md).

Macros and cfg are not evaluated. The manifest reader handles relevant string
metadata rather than validating every TOML/Cargo option. No network, compiler
invocation, external dependency inventory or persistence bypass is used.
