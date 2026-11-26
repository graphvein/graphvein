# ADR 0016 - Rust module resolution and Axum routes

**Status:** Accepted (developer review, 2026-10-10)
**Date:** 2026-10-10

## Context

T011d's completed the investigation finds the pinned RealWorld binaries importing their own
libraries as cargo externals. Axum composition and handlers need the same current
declarations as import/call linking, including replayed source facts.

## Options considered

- Execute Cargo/Axum: requires dependencies and runs repository code.
- Infer routers from receiver/type names: creates false registrations.
- Cache terminal bindings or templates: leaves metadata-only edits stale.
- Cache syntax and resolve the current scan: chosen, offline and compatible with
  contract v1 and the existing store/fact cache.

## Decision

Use [rust-resolution.md](../lang/rust-resolution.md)'s shared resolver. Read current
Cargo metadata without adding a TOML dependency. Cache module attributes, lexical
uses/items, private literal constants and ordered router syntax. Rebuild crates,
module/import/call links, handler/prefix resolution and factory composition every
pass. Keep existing non-route source identities and symbol emission rules.
Aggregate version becomes extractors-10; contract v1, store interface/schema and
dependencies stay unchanged. This extends ADRs 0006 and 0013 without editing them.

Only externally resolved axum constructors/routing functions establish recognition.
A scanned library named axum shadows the external framework. Follow ordered let
shadowing, assignments and lexical blocks. Named factory calls reuse their returned
object, including conditional tail/return alternatives. Deferred mounts preserve
recursive factory composition, with per-chain traversal cycle guards; these
logical objects are not contract nodes. Inline method-router expressions expand explicit verbs; dynamic patterns,
methods, mounts and prefixes remain diagnostic. Literal const/static &str paths
may be imported across modules/crates.

Route identities use unprefixed patterns and declaration-order duplicate suffixes.
Each verb hashes its own call; chained verbs start at their method name. Service
handlers are null and retain source expressions. Named handlers get fact,
confidence-1 handler-to-route exposes_route edges. Composition stays in
internal/extract/routes; per-file syntax stays in internal/lang/rust. See
[routes.md](../extract/routes.md).

The task's five intended deviations are explicit:

1. Accept colon/star and brace spellings across Axum versions; convert :name to
   {name} and *name to {*name}, preserving existing braces.
2. Every unconsumed router chain is a root, including unserved code.
3. Merges/nests are order-insensitive compositions.
4. Ignore visibility/dependency declarations; scanned library names resolve across
   packages. Duplicate library names are ambiguous.
5. Keep cfg-gated code without evaluating cfg and mark paths conditional.

Real semantics: leading :: uses the extern prelude in Rust 2018/2021. Crate roots
are mod-rs files regardless of filename; ordinary modules named lib.rs/main.rs
are not automatically roots. #[path] outside inline modules is relative to the
source directory; inside inline modules it follows the Rust reference's directory
rules, including a path attribute on the inline module itself. Cargo's conventional
integration tests, examples and benches are independent crate roots. An import
cannot supply its own first segment (notably async_trait::async_trait); an external
first segment uses the extern prelude. Opaque external globs cannot hide their own
crate name or a scanned library name. Associated methods are not bare lexical
free functions; trait default bodies still contribute syntax.

## Consequences

Metadata and target-only edits relink cached Rust. Explicit bindings shadow globs;
agreeing terminal declarations through several globs resolve, differing ones stay
ambiguous. Per-name cycle guards retain no partial memoized traversal. Ambiguous
module files produce ambiguous_module with both paths and no guessed link.
Extractor-version invalidation applies once to every language.

## Known gaps

No macro expansion, cfg evaluation, arbitrary expression evaluation, method-router
variable flow or new type inference. Layers remain empty middleware attributes.
The manifest reader interprets relevant metadata; it is not a complete Cargo/TOML
validator. Postgres and execution on other operating systems belong to CI.

## Review notes (developer, 2026-10-10)

- Accepted. realworld-axum-sqlx: 19/19 RealWorld operations, 19
  exposes_route edges, no extras, no mount or prefix diagnostics.
  axum-ddd: 20 routes, 19 handler edges (GET /metrics has an inline
  closure). Express, FastAPI and Langflow unchanged. Binary-to-library
  imports now resolve (unresolved_import: axum 4 -> 0, axum-ddd 1 -> 0);
  no unresolved count rises on any corpus. Same-session interleaved
  Langflow index time: +3.9% against main.
- Deviation 2 risk: a router passed as an argument to a function that
  nests it (parameter flow) stays unconsumed and becomes a root with an
  unprefixed, unconditional path, while the nest reports
  route_mount_unresolved. Not present in the corpus. If it appears,
  treat routers passed to unresolved call arguments as consumed and
  report route_unmounted instead.
- Cargo test, example and bench targets are crate roots, so routers
  built in test code are roots under deviation 2 and look like served
  routes. Revisit when T012 reports routes that no client calls.
- #[path] modules are now supported; the old unsupported_module_path
  diagnostic is replaced by unresolved_module for missing targets.
