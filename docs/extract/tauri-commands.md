# Tauri commands

ADRs 0037 (syntax cache) and 0038 (linking); aggregate extractors-49;
contract v1 unchanged. The syntax cache stores Rust source syntax only and
proves nothing; [Linking](#linking) resolves attribute and macro paths
through the Rust resolver (ADR 0016) at link time and adds the command and
invoke graph facts.

## Syntax cache

`facts.Facts.Tauri` (JSON `Tauri`, omitted when absent) holds `TauriSyntax`
version 3 with Commands, Handlers, Flows and Apps (version 2 added
`TauriFlow.Macros` and `TauriBinding.End`; version 3, T015c2 Revision 2, adds
`TauriCommand.Test`, `TauriHandler.Attributes` and `TauriHandler.Test`, and
`Apps`; an older entry is a cache miss). A Rust file with no record has no
payload, so its cached facts stay byte-identical to extractors-24 apart from
the cache version. A payload with another version, or one that is
structurally incomplete (an empty required kind or name, a missing chain or
`Shadows`, a handler reference out of range, a `string` argument without a
value, an `invoke_handler` method that is neither a handler reference nor
`Uncaptured`, a binding whose `End` is not after its `Offset`, a macro record
without a path or with another kind, an app site of another kind), is a cache
miss (`TauriSyntax.Valid`).

### Commands

One record per `function_item` (any nesting: module level, inline modules,
nested functions, impl members and trait methods with a body) with at least one
qualifying outer
attribute. `Attributes` lists every qualifying attribute in source order (for
example `#[poise::command]` then `#[tauri::command]` gives two), and T015c2
resolves each. A qualifying attribute is:

- a path whose last segment is `command` (`tauri::command`,
  `::tauri::command`, `command`, `poise::command`, ...);
- a single identifier that a `use` anywhere in the file binds to a path whose
  last segment is `command` (`use tauri::command as cmd;` then `#[cmd]`);
- an entry of `cfg_attr(<predicate>, ...)`'s attribute list that is one of the
  above, including inside nested `cfg_attr` (form `cfg_attr`). Each qualifying
  entry is a separate attribute record. The predicate is the source text with
  CRLF normalized to LF (as rustc reads source). For nested cfg_attr the
  predicates of every enclosing level, outermost first, are joined in one
  flat `all(..)`, each kept as written: `#[cfg_attr(x, cfg_attr(y,
  tauri::command))]` records `all(x, y)`, and `#[cfg_attr(a, cfg_attr(b,
  cfg_attr(all(c, d), tauri::command)))]` records `all(a, b, all(c, d))`.

Attributes on items other than functions (clap's `#[command(...)]` on structs,
enums and fields) and on bodiless signatures (trait method declarations,
`extern` block functions) produce no record. A record keeps the function node identity
(empty when the extractor emitted none), the raw ident (with any `r#`), the
function span (which starts after the attributes, like the function node),
`async fn`, the visibility text, whether the function's own attributes apply
a cfg (`Cfg`: `#[cfg(..)]`, or a `cfg_attr` whose attribute list contains
`cfg(..)` at any nesting depth, such as `#[cfg_attr(x, cfg(y))]`; the cfg of an
enclosing module, `#[cfg(test)] mod` or an inner `#![cfg(..)]`, is not
reflected), and its attributes. Each attribute keeps the compact path, the attribute item span
(the whole `#[..]`, also for `cfg_attr`), the form and predicate, the
arguments, the bare `async` flag, an invalid reason, and `Scope`: the RustFacts
scope index from which the resolver resolves the path
(`ModuleResolver.Resolve(file, Scope, Path)`).

`Test` (version 3) is set when the function, or an enclosing item or
statement of the same file, carries an outer `#[cfg(p)]` (or the enclosing
module or file an inner `#![cfg(p)]`) whose predicate holds only in test
builds: `p` is exactly `test`, or an `all(..)` with an item that is itself
test-only (`facts.CfgTestOnly`; `any(..)` and `not(..)` never qualify). An
out-of-line `#[cfg(test)] mod x;` declared in another file is not seen.

Function items written inside a macro invocation or a `macro_rules!` body are
token trees to the parser and are not captured, so a command defined by a macro
has no record.

Arguments are kept in source order as `{Key, Value, Kind}`:

- `key = "string"` (or a raw string): Kind `string`, Value decoded (after CRLF
  normalization);
- `key = <other literal>` (integer, byte string, ...): Kind `literal`, Value null;
- `key = <non-literal>`: Kind `expression`, Value null, invalid;
- a bare path `key`: Kind `path`, invalid; a list `key(..)`: Kind `list`, invalid.

Bare `async` sets the async flag. Empty items other than one trailing comma and
unrecognized items are invalid. Only the first invalid reason is kept. Tauri
rejects only a non-literal or non-string rename, lists and bare paths; it ignores
other unknown or non-literal name-value items, so the invalid reason is
conservative. A repeated rename is kept in order; Tauri applies the last.

### Handlers

One record per `macro_invocation` whose macro path's last segment is
`generate_handler` (any prefix, any delimiter), in source order. Macro bodies
are opaque token trees, so an invocation written inside another macro or a
`macro_rules!` body is not captured. A record keeps the compact macro path, the
invocation span, its lexical `Scope`, the owner (nearest enclosing function or
method node, else the file), the first `#![plugin(<ident>)]` ident as written
(Tauri replaces `_` with `-`), and the entries.

The token tree must be `#![..]* (#[..]* path),* ,?`, where a path is
`::? ident (:: ident)*` (`crate`, `self`, `super` and raw idents allowed).
Anything else, including generic arguments in an entry, a missing comma or an
empty middle entry, records an invalid reason and no entries. An entry named
like a primitive type (`str`, `u8`) is lexed as another token and also makes
the handler invalid, conservatively, although such a function name is legal.
Each entry keeps
the compact path, its segments, the span and content hash of the path alone and
its outer attributes as compact text (for example `#[cfg(debug_assertions)]`).

`Attributes` (version 3) lists the owner function's outer attributes as
records of kind `attribute` (compact path, span, scope); the attributes a
`cfg_attr` would apply follow it, nested `cfg_attr` included, and an
attribute the parser cannot read is recorded with path `?`. `Test` (version
3) marks a handler in a cfg(test)-only region, as for commands.

Context says where the invocation sits:

- `argument`: the sole argument of a method call; `Method` is that method,
  `Chain` the whole method chain the call belongs to and `Use` what consumes
  the chain's value (below);
- `let`: a let initializer; `Binding` is the pattern;
- `other`: anything else.

`Use` is one of:

- `statement`: an expression statement ending in `;`;
- `let` / `assign`: the value of a let declaration, or the right side of an
  assignment whose left side is a plain identifier (`b = ..`); a field, deref
  or index target (`self.b = ..`, `*slot = ..`, `v[0] = ..`) is `other`;
- `tail`: the tail expression of a block (no `;`) or a closure body;
- `return`: the value of `return`;
- `argument`: an argument of a call or method call;
- `other`: anything else (a reference, a field access, an array element, a
  match arm value, ...); the chain is then marked `Incomplete`.

A chain whose value is a tail, return value or argument may receive a later
`invoke_handler` elsewhere (`fn make() -> Builder { ..invoke_handler(a) }`
then `make().invoke_handler(b)`), so T015c2 may treat only `statement`, `let`
and `assign` chains as final. A `let` or `assign` chain is final only if no
later `invoke_handler` call in the flow's `Calls`, captured or not, applies to
its binding (see Flows).

### Chains

A chain is a root and the method calls from the root outward, including methods
after `invoke_handler`, each with the span from the method name to the end of
its call. A method whose sole argument is a captured handler references it by
index into the file's Handlers (`Handler`). Any other `invoke_handler` method
(a let-bound handler, a closure, no or several arguments) has `Uncaptured`
set: it still replaces the builder's handler. Every `invoke_handler` method is
exactly one of the two. The chain is found by climbing from the call
that receives the handler to the outermost method call, and then descending to
the root, so no method is dropped silently:

- parentheses are transparent in both directions;
- `?` and `.await` are passed through and set `Incomplete` when they sit
  between the root and the outermost method (`b.invoke_handler(a).setup(f)?
  .invoke_handler(b)` keeps all three methods, incomplete); a trailing `?` or
  `.await` after the outermost method hides no method and does not;
- a consumer other than the `Use` forms above sets `Incomplete`.

The root is:

- `call`: a non-method call with its callee path (generic arguments dropped,
  `Generic` set when present), the decoded first argument when it is a string
  literal, its span;
- `identifier`: a plain identifier with its name;
- `other`: any other expression, named by its node kind (for example
  `field_expression` for `self.builder`); the chain is `Incomplete`.

Every root also keeps its start byte offset. T015c2 treats an incomplete chain
as unproven.

### Flows

For every function that contains a captured handler (in any context) and an
identifier-rooted chain calling `invoke_handler`, Flows record:

- `Calls`: every identifier-rooted chain in the function (closures included,
  nested function items excluded) that calls `invoke_handler`, captured or
  `Uncaptured`, once per chain (two calls on one chain are one record), in
  source order, each with its `Use` as for handler contexts (`other` makes it
  `Incomplete`). A captured handler's own chain appears here too.
- `Bindings`: the let and assignment bindings of plain identifiers reachable
  from the roots of `Calls`, transitively through identifier roots of their
  alternatives.

So in `let app = B::default().invoke_handler(generate_handler![a]); let h =
generate_handler![b]; app.invoke_handler(h)`, the flow holds the `app` binding
and a call `app.invoke_handler?` that replaces handler 0. T015c2 treats a
binding's handler as final only if no later call in `Calls` (captured or not)
applies to that binding.

Bindings inside closures count; nested function items do not. Each binding
keeps name, kind (`let` or `assign`), the span of its innermost block, its
statement's start and end byte offsets (`Offset`, `End`; a let binds its name
only after its own statement, so a root inside `let app = app.m();` refers to
the earlier `app`), `Cfg`, `Conditional`, the value form and its
alternatives. `Cfg` is set
when a cfg attribute (`#[cfg(..)]` or a `cfg_attr` applying `cfg(..)`)
precedes the statement or any enclosing statement, block or expression
statement in the function body (`#[cfg(x)] { b = ..; }`). `Conditional` is set
when the statement is not in the function body's top-level statement list:
inside an `if`, `match`, loop, closure, async block or plain block (plain
blocks too, conservatively). T015c2 treats a Conditional binding as a possible
rebinding, never as a definite replacement. Value forms:

- `plain`: one chain;
- `if`, `match`, `block`: the tail chain of every branch, nested branches
  flattened in source order; `Complete` is false when a branch has no tail
  expression (including an `if` without `else`);
- `unsupported` (`loop`, `while`, `for`) and `none` (`let x;`): no
  alternatives, `Complete` false.

Alternative chains follow the chain rules above (a `let b = make()?;` value is
incomplete).

`Shadows` lists, sorted and de-duplicated:

- every flow name that another binder may bind at one of the flow's identifier
  root sites (the roots of `Calls` and the identifier roots of the kept
  alternatives). Binders are the function's own parameters, closure
  parameters, `if let` / `while let` patterns, match arm patterns, match guard
  patterns (`Some(x) if let Some(b) = .. =>`, including let chains), `for`
  patterns and let declarations with a destructuring pattern (tuple, struct,
  `ref`). Their scope is taken wide (the whole function, closure, `if` /
  `while` expression, match arm, `for` expression or enclosing block), and
  pattern names over-include (unit variants and constants count), so
  shadowing may be over-reported but is never missed. A binder whose scope
  does not contain a root site (kftray's `.setup(move |app| ..)` inside
  `let app = ..`) does not count;
- every flow name that appears as an identifier token anywhere in the token
  tree of a macro invocation in the function body (nested token trees
  included, nested function items excluded), wherever it sits, because a
  macro may expand to a rebinding (`rebind!(builder)`). This is conservative:
  it also counts macros that only read the name, and the entries of the
  function's own `generate_handler!` invocations.

T015c2 treats a flow name in Shadows as unproven at that flow.

`Macros` (version 2) lists, in source order, every macro invocation and
`macro_rules!` definition in the function body (closures included, nested
function items excluded): `Kind` `invocation` with the compact macro path, or
`definition` with the defined name, its span, and `Scope`, the RustFacts scope
index from which the resolver resolves the path. Invocations written inside
another macro's token tree (`info!("{}", env!("X"))`, found by their
`path ! (..)` token shape at any nesting) are recorded too, with the outer
invocation's scope. Flows exist only for functions with a captured handler,
so this is the cheapest filter that keeps every macro a registry proof must
see. kftray's `fn main` records 30 invocations and no definition.

### App sites

`Apps` (version 3) lists, for every Rust file, the syntax that may build a
Tauri app other than a proven registry, so linking can refuse the single-app
proof when another app exists (Revision 2, M2):

Builder-like names are `Builder` and the file's `use` aliases of it
(transitively, `use tauri::Builder as A; use A as B;`). Kinds (Revision 3,
MA2):

- `root`: a value-position path through a Builder-like segment, generic
  arguments removed and qualified paths read through their type
  (`tauri::Builder::default()`, `B::<Wry>::new()`,
  `<tauri::Builder<Wry>>::default()`, the function value
  `tauri::Builder::<Wry>::default`). Path is the path up to that segment
  (`tauri::Builder`); the span is the call's when the path is a callee (the
  span a proof root has);
- `typed`: a struct field whose type is directly a Builder-like path, or a
  let or function return whose type is, with a `default` / `new` value call
  that names no Builder-like segment (`let b: tauri::Builder<Wry> =
  Default::default()`); Path is the type's path;
- `alias`: a type alias of a Builder-like path;
- `reexport`: a visible `use` (`pub`, `pub(crate)`, ..) of a Builder-like
  name (Path is the full path) or of a glob not rooted at `crate`, `self` or
  `super` (Path `prefix::*`);
- `macro`: a `macro_rules!` body or macro invocation whose token trees name
  a Builder-like identifier (Path is its adjacent path, plus `tauri::Builder`
  when the trees also name `tauri`);
- `invoke_handler`: a method-form or path-form `invoke_handler` call none of
  whose arguments is a `generate_handler!` invocation (a closure, a binding);
- `context`: a `generate_context!` invocation, also inside another macro's
  token tree.

Each keeps the compact path (`invoke_handler` for the method form), span,
scope and `Test` (a cfg(test)-only region). Type positions in function
signatures are not recorded (they create no app). The filter is syntactic and
wide (`tempfile::Builder` is kept; kftray records 30 sites in 36 payload
files: 17 roots, 12 glob re-exports and 1 context, of which 2 are Tauri's);
only the resolver decides at link time.

## Known gaps

- A handler passed through a let binding is recorded with context `let`; the
  method call that receives the binding is `Uncaptured`, not linked to the
  binding.
- `invoke_handler` called in path form (`Builder::invoke_handler(b, h)`) is not
  a method call and is not recorded in `Calls`.
- Handlers and command functions inside other macros (including
  `macro_rules!` bodies) are not captured. Bindings through tuple or struct
  patterns are not recorded as bindings; they appear in `Shadows` when they
  may hide a root.
- `Cfg` covers only the function's own `#[cfg(..)]`; an enclosing module's cfg
  (`#[cfg(test)] mod`, `#![cfg(..)]`) is not reflected. `Test` does reflect
  enclosing cfg(test)-only items and inner attributes of the same file, but
  not an out-of-line `#[cfg(test)] mod x;` in another file.
- App sites (Revision 3) do not see a Builder renamed in another file's
  private `use` and then referenced other than by a `default` / `new` call
  (linking reads those calls from the Rust call cache), a `default` / `new`
  value typed only through a type alias (`let b: App = Default::default()`;
  the alias itself is a site), a builder made from a macro's parameters
  (`$t::Builder` invoked as `m!(tauri)`), or a re-export in a crate outside
  the repository.
- Entry attributes are compacted outside string literals only (Revision 3),
  so `"a b"` and `"ab"` stay distinct.
- A chain bound by `let` or `assign` whose binding later escapes (passed to a
  function, returned) is not tracked beyond the flow bindings: `Use` describes
  only the chain's immediate consumer.
- Entry attributes keep `#[..]` items only; `///` doc comments on entries
  (which Tauri sees as `#[doc]`) are dropped, which does not affect cfg.
- A `macro_rules!` defined inside the function body that assigns an outer
  local and is invoked without tokens (`reg!()`) contributes nothing to
  `Shadows` or `Calls`; `Macros` records the definition and the invocation,
  and linking treats such a flow as unproven (G11 below).
- `#[cfg]` on a match arm is not propagated to the bindings in that arm:
  their `Cfg` stays false, but `Conditional` is true, so they remain possible
  rebindings only.
- `TauriSyntax.Valid` accepts a flow with `Bindings` but no `Calls`. The
  extractor never produces one, and no older cache can hold one because the
  payload was never released before this version.
- ACL inputs (build.rs `tauri_build` calls, `permissions/` files) are not part
  of this payload: build.rs calls stay in the existing Rust calls, and the
  scanner inventory lists `permissions/` files.

## Linking

`internal/extract/tauri` (ADR 0038) runs on every link pass after the client
linkers, from the cached payload, the Rust resolver, the TypeScript module
resolver and the client evaluator (ADR 0020). Nothing it decides is cached,
so an edit to a TypeScript file relinks without reparsing Rust. Every rule
yields a proven edge or no edge; whatever is not proven keeps its node with
the unproven part named (VISION, principle 6).

### Commands (K1-K3)

A command function is a command record with exactly one attribute that
resolves (`Resolve(file, attribute.Scope, attribute.Path)`) to `tauri` or
`tauri_macros` `command`, not ambiguous; clap, poise and unbound attributes
never qualify. Two such attributes, or one with an invalid argument, give a
`tauri_command_invalid` warning; two attributes make the function no command.
The invoke name is the last `rename` when it is a decoded string, else the
function name; an invalid argument, a non-string rename or a raw identifier
(`r#..`) gives an unknown name. A command is cfg-gated when its attribute is
in `cfg_attr`, its record has `Cfg`, or its RustFacts function is
`Conditional` (an inline `#[cfg] mod`). It is test-only (Revision 2, m2) when
its record has `Test` or its `cfg_attr` predicate holds only in test builds
(`#[cfg_attr(test, tauri::command)]`).

### Registries (G1-G11)

A handler is considered only when its macro path resolves to `tauri` or
`tauri_macros` `generate_handler` (G1); an invalid token tree gives a
`tauri_handler_invalid` warning and no entries. It is a proven registry only
if every rule holds; otherwise its entries get `registry: unproven` with the
first failing `reason` and one `tauri_registry_unproven` diagnostic:

| rule | reason | requires |
|---|---|---|
| G2 | `context` | the handler is the sole argument of a method-form `invoke_handler` (no let-bound handler, no path form) |
| G3 | `incomplete` | the chain passes through no `?` or `.await` and its root is a call or identifier |
| G4 | `not_last` | no later `invoke_handler` on the chain |
| G5 | `not_consumed` | after the handler, builder methods only, then `build` or `run` (plugin: `build` or `try_build`), which take the builder by value |
| G6 | `method` | every method before the handler is a by-value builder method returning `Self` (Tauri 2.12.5 `app.rs` 1694-2620; plugin `plugin.rs` 279-757); an extension-trait method is not proven |
| G7 | `root`, `generic` | the root resolves to `tauri` `Builder::default` / `Builder::new` (app) or `plugin::Builder::new` with a string argument (plugin, named by it); turbofish roots are a known gap |
| G8 | `flow_binding` | an identifier root resolves to the latest flow `let` of the name whose block contains the root and whose statement ends before it; its values, a cfg-gated let's shadowed binding and every assignment between the let and the site (an assignment's own right side excluded) are complete chains of builder methods with proven roots of one kind |
| G9 | `shadowed` | the name is not in the flow's `Shadows` |
| G10 | `use_let_destructure` | the binding name is a plain identifier (defensive: destructuring lets are never recorded as bindings and surface through G9 or G8) |
| G11 | `macro` | the flow body defines no `macro_rules!` and invokes only verified macros, matched by resolved path: std/core/alloc builtins and prelude macros (unbound single segments included), log 0.4, tracing 0.1, tauri `generate_handler` and `generate_context`; and (Revision 2, m3) every attribute of the handler's function, cfg_attr contents included, is a builtin `cfg`, `cfg_attr`, `allow`, `deny`, `doc` or `inline`, `tokio::main` (tokio-macros 2.7.2 `entry.rs` wraps the body unchanged in `async`) or `tauri::mobile_entry_point` (tauri-macros 2.7.4 `mobile.rs` re-emits the item); this applies to call-rooted chains too |
| G7 | `mock_builder` | the root is `tauri::test::mock_builder()` (in every branch of a flow value); such a registry hosts no webview and neither proves nor blocks an app (Revision 2) |
| - | `plugin_name` | a `#![plugin(x)]` (with `_` read as `-`) names the plugin builder's own name; on an app registry it is not proven |

A registry is conditional when its owner function is `Conditional` or the
consumed chain's own let has `Cfg`.

### ipc_command nodes

One level-4 `ipc_command` node per entry of every G1-valid handler, proven or
not: span and content hash of the entry path, qualified name
`<owner>.COMMAND <last entry segment>` (`@2`, `@3` for repeats in a file;
renames never enter identity, see [identities](../lang/identity.md)), a
`contains` edge from the handler's owner and an `exposes_command` fact edge
from the command function when the entry resolves to exactly one K1 command
(also for unproven registries). Attributes: `framework` `tauri`, `transport`
`tauri_ipc`, `command` (invoke name or null), `handler` (function identity or
null), `registry` (`app`, `plugin` or `unproven`), `reason` (unproven only),
`plugin` (plugin registries), `cfg_gated` (an entry attribute that applies a
cfg, a cfg-gated command or a conditional registry; an edge into a cfg-gated
command means the target exists only in some builds), `test_only` (Revision
2, m2: the command is test-only, the entry's own cfg holds only in test
builds, or the handler sits in a cfg(test)-only region; production calls
never link to it), `unreachable` and `scope: test`. Tauri emits each entry's
attributes on its own match arm (tauri-macros `handler.rs`), so an entry is
`unreachable` (with a `tauri_command_duplicate` info, "the first entry wins")
only when an earlier entry of the handler with the same known name exists in
every build that has this one: that earlier entry has no cfg and is not
test-only (Revision 2, M1). An entry that does not resolve to a command gets
`command: null` and a `tauri_entry_unresolved` info.

### Invoke sites and ipc_call nodes

Recognized calls bind through the TypeScript module resolver (scanned
packages shadow `@tauri-apps/api`; type-only imports never qualify):
`invoke` from `@tauri-apps/api/core` (named or aliased, form `direct`),
`<ns>.invoke` for a namespace import of it and `core.invoke` with `core` from
`@tauri-apps/api` (form `namespace`). `@tauri-apps/api/tauri` (v1) is not
recognized. A wrapper is an emitted callable whose recognized invoke (or
wrapper invocation) receives one of its own parameters, unchanged, as the
command, followed for three levels. Unchanged means that the callable's
source text (nested functions included) has no write to the parameter: no
plain, compound or logical assignment, no `++` / `--`, no destructuring
assignment target or for-in/of head holding it, no function or class
declaration of the name and no `eval`, `arguments` or `with` (Revision 2, m1;
a nested function's own binding of the same name also counts). The check
lexes the source and fails closed (Revision 3, MA1): the callable is not a
wrapper when the lexer is unsure of its tokens, that is a string reaching a
line break or the end, a template, regular expression or block comment
reaching the end, a `/` after `)`, `]`, `}`, `++`, `--` or a keyword
(regex or division depends on the grammar), a backslash (escaped
identifiers) or non-ASCII byte outside literals and comments, unbalanced or
mismatched brackets, a lone `!` after an identifier, `)` or `]` (a
TypeScript non-null assertion; review 3, MA3), and, in every file but `.ts` /
`.mts` / `.cts`, a `<` followed by an identifier, `>` or `/` (JSX). No TypeScript cache field
records update or destructuring writes (rule 32 check), so the source text
is the only input; wrapper callees resolve through the
module resolver or, for local callables, the same-file scope-0 binding and
the file's node with that name (a nearer declaration leaves the callee
unresolved). `window.__TAURI__.core.invoke` and
`window.__TAURI_INTERNALS__.invoke` with `window` unbound are form `global`
and never link.

Each intent (a direct call or a wrapper invocation) is one level-4
`ipc_call` node with the call span and hash, qualified name
`<owner>.INVOKE <compact command argument>` and a `contains` edge from the
nearest emitted owner; a wrapper's inner forwarding call is its own node with
`wrapper: true` and status `wrapper`. The command argument is evaluated with
the bounded client evaluator, keeping every alternative. Attributes:
`framework`, `transport`, `form`, `wrapper_identity` (wrapper invocations),
`command` (the single known name, else null), `conditional` and
`alternatives` (R2: with two or more alternatives the call and every
alternative carry `conditional: true`; a linked alternative also carries its
`target` and that command's `cfg_gated`), `status`, `candidates` (ambiguous,
test_only), `plugin` / `plugin_command` and `scope: test`. Alternatives with
different commands are chosen by a runtime condition: their edges mean "may
invoke, depending on a runtime condition". Alternatives with the same
command and different targets are match arms of different builds (M1): their
edges mean "may invoke, depending on build configuration", and the call keeps
that `command`. `status` is a summary by precedence (an alternative that is
linked may sit under an `unmatched` call, `x ? 'greet' : 'nope'`); a UI shows
the edges of each alternative from `alternatives`, not from `status`.

### Matching and statuses

Per alternative: an unknown name is `dynamic`; a `plugin:<name>|<cmd>` name is
`plugin` (no edge; plugin linking is T015d); otherwise the name is compared
exactly with the commands of the repository's one proven app registry, in
entry order. App scope (A1, Revision 2) is decided over the production
scope: registries in test-scope files (ADR 0021) or cfg(test)-only regions
and mock_builder registries neither prove nor block it. `registry_unproven`
when any production G1-valid handler is unproven (it could be the real
registry), or a production `invoke_handler` call takes anything but Tauri's
`generate_handler!` (a closure, a binding, another crate's macro; one
`tauri_registry_unproven` `[handler]` info each); `ambiguous` with two or
more proven app registries (one `tauri_app_ambiguous` per registry), or with
one proven app and other production app evidence: a Tauri
Builder reference that is not a root of its proof (any `root`, `typed`,
`alias`, `reexport` or `macro` app site resolving to `tauri` `Builder` or
`app::Builder`, a glob re-export of `tauri` or `tauri::app`, or a production
`default` / `new` call in the Rust call cache resolving to Tauri's Builder), more than one Tauri `generate_context!`, or main config files
(`tauri.conf.json`, `tauri.conf.json5`, `Tauri.toml`; tauri-utils
`config/parse.rs` reads one per directory, and platform files such as
`tauri.linux.conf.json` are overlays) in more than one directory (one
`tauri_app_ambiguous` for the app and one per site); `acl_possible` when the
app's Cargo package (nearest `Cargo.toml` with `[package]`) has a scanned
`permissions/` file or its `build.rs` calls `app_manifest` /
`AppManifest::new` (one `tauri_acl_possible` per registry). Only when the
production scope has no registry and no app site are test-scope registries
matched instead (production calls then get `test_only`). Then the entries
with the name are collected in order up to and including the first one
without a cfg (M1), or the second of two whose only cfgs are exactly `P` and
`not(P)` (whitespace outside strings ignored, either order, test-only
entries included; Revision 3, mi1), which together exist in every build; later
entries with that name are `unreachable`: each is the target in some build, so the call links to
every collected entry (one `invokes_command` fact edge, confidence 1, per
target; two or more make the call conditional). A production call skips
test-only entries; when only test-only entries carry the name it is
`test_only` (m2), while a test-scope call links to them. An entry with an
unknown name before a collected one makes it `shadow_possible`; no entry with
the name is `unknown_command` when some entry is unknown, else `unmatched`; a
production call whose targets are in a test-scope registry is `test_only`. Global calls with a known name are
`global_unlinked`. The call status is the first of `dynamic`,
`registry_unproven`, `acl_possible`, `ambiguous`, `shadow_possible`,
`unknown_command`, `test_only`, `unmatched`, `plugin`, `global_unlinked`,
`linked` present among its alternatives.

kftray at e3116e0c: 115 ipc_command nodes (registry app), 115
exposes_command edges, 105 ipc_call nodes (linked 101, wrapper 2, dynamic 2),
103 invokes_command edges, 0 tauri_* diagnostics.

### Linking known gaps

See ADR 0038: turbofish roots, chains that are not consumed (their binding
may escape), repository-level single-app scope (no per-crate or
per-`tauri.conf.json` scoping; any second app, closure handler or test
builder outside a test scope blocks every match), app sites the payload
misses (see Known gaps above), the wrapper check's under-claims (any lexer
uncertainty rejects a wrapper, also in correct code such as `(a) / 2` or
type arguments in a `.tsx` file; correctness assumes the lexer's tokens are
the real ones whenever it is not unsure; a non-null assertion such as
`invoke(cmd!)` also rejects), second apps whose Builder is made only through
a trait impl `Self::default()` on `tauri::Builder`, a generic helper
`mk::<tauri::Builder<..>>()` or an inferred `let b = Default::default()`
(not evidence; a wrong edge needs such a second app with no registry of its
own, sharing the first app's context and config, with frontend code loaded
only by it), cfg satisfiability beyond exact
`P` / `not(P)` pairs (`any(a, b)` and `all(not(a), not(b))` are not
recognized as complementary, so later arms stay targets),
global forms, a frontend served by an app outside the repository, a cfg on a
statement holding a call-rooted chain, `#[macro_use] extern crate` macros
named like builtins, attribute macros on enclosing modules or impls, loops
that reassign a builder after its use, a `build` script outside `build.rs`,
an out-of-line `#[cfg(test)] mod x;` (its commands are not test-only), and
plugin linking (T015d).
