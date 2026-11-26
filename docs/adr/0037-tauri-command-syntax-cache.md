# ADR 0037 — Tauri command syntax cache

**Status:** Accepted (delegated review, 2026-11-01)

## Context

T015c links Tauri `invoke` calls in TypeScript to Rust command functions. The
T015c1 census (kftray at e3116e0c: 115 commands, one `generate_handler!` with
115 entries, four builder bindings) found three gaps in the existing Rust
cache. Command attributes are not cached: the extractor keeps only `#[path]`
on modules and a `#[cfg(` prefix test. Macro invocations are skipped, so
`generate_handler!` entries have no structure, spans or hashes. The cfg-gated
`if`/`else` that rebinds kftray's builder is cached as an opaque
`if_expression` with text only. The TypeScript side needs no new capture: every
invoke field T015c2 needs is already in the client caches.

## Options considered

- Extend RustFacts (RustStatement, RustExpr) to keep attributes, macro token
  trees and branch tails for every file. This changes existing payloads and
  records syntax no consumer reads for most files.
- Prove Tauri at extraction time (resolve `tauri::command`, the handler path
  and the builder root while parsing). This caches decisions that go stale
  when imports or Cargo metadata change, and exceeds this cache-only task.
- Add a separate, versioned, syntax-only Tauri payload beside RustFacts,
  absent when a file has no record. Selected.

## Decision

Add `Tauri *TauriSyntax` (`json:",omitempty"`) to `facts.Facts`, beside the
Rust syntax, with Version 1, Commands, Handlers and Flows as described in
[Tauri commands](../extract/tauri-commands.md). Files without a record keep
byte-identical payloads; a payload of another version, or a structurally
incomplete one (empty required kinds, a handler reference out of range), is a
cache miss, as for the router payload (ADR 0035). The aggregate extractor
version becomes extractors-25; contract v1, the store interface and schema are
unchanged.

Capture filters are the cheapest syntactic tests that keep every real target:
a function attribute whose last path segment is `command` (or a single
identifier aliased by a file `use` to such a path, or either inside
`cfg_attr`, nested `cfg_attr` included), and a macro whose last path segment
is `generate_handler`. Every qualifying attribute of a function is recorded,
so a Tauri attribute next to poise's is not lost; cfg_attr predicates are
normalized to LF and the predicates of nested levels joined, outermost first,
in one flat `all(p1, p2, ..)`. A command's `Cfg` covers `#[cfg(..)]` and a
`cfg_attr` applying `cfg(..)` at any nesting depth.
Proof stays with the Rust resolver at link time: each attribute and handler
records the RustFacts scope index the resolver uses, so T015c2 rejects clap's
and poise's `command` attributes by resolution, never by text.

Handlers parse the macro input grammar `#![..]* (#[..]* path),* ,?` from the
token tree; anything else gets an invalid reason and no entries. Entries keep
the span and hash of the path alone. Chains keep every method from the root
outward, including methods after `invoke_handler`, because only the last
`invoke_handler` on a builder counts. A chain never drops a method silently:
parentheses are transparent, `?` and `.await` inside the chain are passed
through and mark it incomplete, and a root that is not a call or identifier
marks it incomplete. Each handler context records what consumes its chain's
value (`Use`: statement, let, assign, tail, return, argument, other; other is
incomplete; assign only for a plain identifier target, a field, deref or index
target is other), because a tail, return or argument value may receive a later
`invoke_handler` that replaces this one; T015c2 may treat only statement, let
and assign chains as final. An `invoke_handler` method whose argument is not a
captured handler is marked `Uncaptured`, so a replacement is never invisible.
Flows exist for functions that contain a captured handler and record `Calls`:
every identifier-rooted chain in the function that calls `invoke_handler`,
captured or not, with its Use. They keep the let and assignment bindings
reachable from those roots, with the tail chain of every branch of `if`,
`match` and block values, a completeness flag, `Cfg` (also inherited from a
cfg-gated enclosing statement or block) and `Conditional` (the binding is not
in the body's top-level statement list), and `Shadows`: the flow names that a
parameter, closure parameter, `if let` / `while let` / match arm / match guard
/ `for` pattern or destructuring let may bind at a root site (wide scopes,
over-inclusive names, following the router payload's Shadows), plus every flow
name that appears as an identifier in a macro invocation's token tree in the
function body (a macro may rebind it). T015c2 treats an incomplete chain or a
shadowed flow name as unproven, never as a registry; it treats a let or assign
binding's handler as final only if no later call in `Calls`, captured or not,
applies to it, and a Conditional binding as a possible, never a definite,
rebinding.

## Consequences

The kftray payload carries exactly the census rows: 115 commands (one
attribute each), one handler with 115 entries (context use `let`, chain
complete) and one flow with four bindings and one call; the Shadows and
Conditional values are reported in the T015c1 summary (Revision 2). No other corpus has a
record. The fixture `testdata/fixtures/tauri-command-syntax` has a
payload-only golden, independent assertions, a CRLF check (the index also
normalizes line endings in scan) and shared backend round-trip coverage;
Postgres runs only in CI. No nodes, edges, diagnostics,
identities or existing payload fields change. T015c2 adds command and invoke
nodes and links from this payload.

## Known gaps

- Argument invalid reasons are syntactic and conservative: Tauri ignores
  unknown or non-literal name-value items other than `rename`, which this
  payload still marks invalid.
- Handlers and command functions written inside other macros (including
  `macro_rules!` bodies) are not captured. Bindings through tuple or struct
  patterns are not recorded as bindings, only as Shadows.
- `Cfg` reflects only the function's own `#[cfg(..)]`, not an enclosing
  module's (`#[cfg(test)] mod`, `#![cfg(..)]`); T015c2 must derive that from the
  module structure or treat such commands as conditional.
- `Use` describes the chain's immediate consumer only: a let- or
  assign-bound chain whose binding later escapes (`let x = ..; helper(x)`) is
  not tracked beyond the flow bindings.
- Handler entries named like primitive types (`str`, `u8`) make the handler
  invalid (conservative), and `///` doc comments on entries are not kept.
- A let-bound handler records context `let`; the method call receiving the
  binding is `Uncaptured`, not linked to the binding.
- `invoke_handler` in path form (`Builder::invoke_handler(b, h)`) is not
  recorded in `Calls`. Macro Shadows over-report (any identifier in any macro
  of the body counts, including generate_handler! entries).
- Commands are recorded only on function items with a body; attributes on
  trait method declarations without a body and `extern` functions have no
  record.
- ACL inputs are outside the payload: build.rs calls stay in the existing Rust
  calls and `permissions/` files in the scanner inventory.
- A `macro_rules!` defined inside the function body that assigns an outer
  local and is invoked without tokens (`reg!()`) is invisible to Shadows and
  Calls, so a captured handler it replaces can read as final. T015c2 must
  treat any function body that contains a `macro_rules!` definition, or any
  macro invocation it cannot see into, as unproven for flows.
- `#[cfg]` on a match arm is not propagated to bindings in the arm: their
  `Cfg` stays false (`Conditional` is true, so they remain only possible
  rebindings).
- `Valid` accepts a flow with Bindings but no Calls. The extractor never
  produces one at the final revision, and no older cache holding one can exist
  because the payload was never released.

## Review notes (delegated review, 2026-11-01)

- Delegated review in place of developer review. Accepted after
  Revision 2 and a final re-check. Results:
  - The investigation: stops S1-S3 pass (kftray census: 115 commands, one
    generate_handler! with 115 entries, four builder bindings).
  - Review 1: 3 major, 5 minor, 3 nit findings, fixed in Revision 1.
  - Review 2: 2 major, 4 minor, 3 nit findings, fixed in Revision 2.
  - Final re-check: 1 minor and 2 nit findings, recorded as known gaps, not
    fixed.
  - Gates at 59113ca: snapshot bodies of all ten public checkouts and the
    self-index byte-identical to main apart from the version header; warm
    replay parses 0 files; kftray timing +1.24% fresh, -0.05% warm; fresh
    store Langflow +0.00%, kftray +0.09%; CI green, including the postgres
    job.
- Revision 1: chains keep every method (Incomplete marker), pattern-binder
  Shadows, chain Use, structural Valid, LF predicates, nested cfg_attr and
  every qualifying attribute per command.
- Revision 2: Uncaptured markers and flow Calls, plain-identifier assign
  targets, Conditional bindings, cfg inherited from enclosing statements and
  cfg_attr(.., cfg), match guard and macro Shadows.
- Gaps: see Known gaps above (the last three come from the final re-check).
