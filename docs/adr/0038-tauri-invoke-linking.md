# ADR 0038 — Tauri invoke linking

**Status:** Accepted (delegated review, 2026-11-02)

## Context

ADR 0037 caches Tauri syntax without proof: command attributes,
`generate_handler!` entries, builder chains and the let bindings that carry a
builder to `invoke_handler`. T015c2 turns it into graph facts: which Rust
functions a Tauri app exposes as commands, which TypeScript calls invoke them,
and the links between them. Tauri dispatches an invoke by exact,
case-sensitive string equality with the first matching arm of the handler
installed last before the builder is consumed (tauri-v2.12.5 app.rs:1723,
2408, 2616; handler.rs). The T015c1 and T015c2 censuses of kftray (e3116e0c:
115 commands, one handler, 105 invoke sites) are the corpus.

The investigation of T015c2 found that the rules as written picked the let whose own
initializer holds the root (`let app = app.…`), so kftray proved no registry
(stop P2). Revision 1 added the statement end to the payload (R1), conditional
markers on alternative names (R2), explicit builtin macro mapping (R3) and a
link-time crate root for the ACL check (R4).

## Options considered

- **Edge confidence: inferred 0.9 or fact-or-nothing.** The T015c1 design
  used inferred 0.9 because ACL capabilities, per-entry cfg and the hosting
  webview were unproven. Each of those is now either proven or blocks the
  edge: build-time command removal (the only ACL effect that changes routing)
  blocks it; runtime capability denial is authorization, not routing; entry
  and command cfg is kept on the ipc_command as `cfg_gated`; the host is the
  one proven app of the repository. Dispatch has no base URL, template or
  suffix tier, so a rule set that only yields a proven edge or no edge
  publishes fact edges at confidence 1. Selected.
- **Registry finality: flow obligations or consumption.** Proving that no
  later `invoke_handler` reaches a builder through escapes, returns, call
  arguments or path-form calls needs data the cache does not hold. Tauri's
  `Builder::build(mut self, ..)` and `run(self, ..)` (and the plugin builder's
  `build` / `try_build`) take the builder by value, so a chain that reaches
  one of them through by-value builder methods only is final whatever happens
  to its bindings. Selected: a registry must be consumed (G5); other chains
  stay unproven.
- **Macros in flow bodies: capture or treat every flow as unproven.** A
  `macro_rules!` defined in the body, or an opaque procedural macro, may
  assign a builder local without naming it in its tokens. Without capture
  every identifier-rooted chain (kftray's included) would be unproven.
  Selected: payload version 2 records every macro invocation (token-tree
  nesting included) and definition of flow bodies, and the proof allows only
  macros verified at the locked versions, matched by resolved path (G11).

## Decision

- Payload `TauriSyntax` version 2 adds `TauriFlow.Macros` (path, scope, span,
  kind) and `TauriBinding.End`; a version-1 entry is a cache miss. Aggregate
  extractors-26. Revision 2 makes it version 3 (extractors-27; see below). Store interface, schema and contract v1 are unchanged; the
  new node and edge kinds are extensible kinds.
- `internal/extract/tauri` proves commands (K1-K3) and registries (G1-G11)
  with the Rust resolver, applies repository app scope (A1: exactly one
  proven app registry and no unproven G1-valid handler) and ACL removal (A2:
  `permissions/` files or an app manifest in the app's Cargo package), and
  recognizes `@tauri-apps/api/core` invoke calls, namespace and `core` forms,
  wrappers for three levels and the two global forms with the TypeScript
  module resolver and the client evaluator. The rules and attributes are in
  [Tauri commands](../extract/tauri-commands.md#linking); identities in
  [identities](../lang/identity.md).
- Nodes: level-4 `ipc_command` (one per entry of every G1-valid handler) and
  `ipc_call` (one per intent, plus each wrapper's inner forwarding site),
  transport `tauri_ipc`, with `contains` edges. Edges: `exposes_command`
  (command function to ipc_command) and `invokes_command` (ipc_call to
  ipc_command), facts at confidence 1, the latter only when every part is
  proven. A call whose name evaluates to several alternatives carries
  `conditional: true` on the call and each alternative: its edges mean "may
  invoke, depending on a runtime condition". The ipc_command cfg flag is
  `cfg_gated`, so each attribute name has one meaning.
- Diagnostics: `tauri_command_invalid` and `tauri_handler_invalid`
  (warnings), `tauri_entry_unresolved`, `tauri_command_duplicate`,
  `tauri_registry_unproven`, `tauri_acl_possible`, `tauri_app_ambiguous`
  (info). Unmatched and dynamic invokes are statuses, not diagnostics.

## Consequences

- kftray: 115 ipc_command nodes (app, not cfg-gated), 115 exposes_command
  edges, 105 ipc_call nodes (101 linked, 2 wrapper, 2 dynamic), 103
  invokes_command edges, 0 tauri_* diagnostics, matching the hand truth
  (the T015c2 task record (internal)) at all 105 sites.
- Linking runs on every pass from cached syntax; a TypeScript edit relinks
  without reparsing Rust. The payload grows by one integer per flow binding
  and one record per macro of the few functions that hold a handler.
- The fixture `testdata/fixtures/tauri-invoke` covers each rule in separate
  scenario repositories (app scope is per repository); the
  `tauri-command-syntax` golden changes by the version-2 fields only.

## Known gaps

- Turbofish roots (`Builder::<R>::new()`) are unproven [generic].
- Chains that are not consumed by `build` / `run` are unproven, because the
  escapes of their bindings are not cached; a builder returned from a helper
  (`make().invoke_handler(..)`) is unproven [root].
- App scope is repository-wide: per-crate or per-`tauri.conf.json` scoping is
  not done, so two apps in one repository give ambiguous matches and one
  unproven handler anywhere blocks app matches (under-claims only).
- A frontend in the repository served by an app outside it still links to
  the in-repository app when the names match.
- Global forms (`window.__TAURI__`, `__TAURI_INTERNALS__`) never link:
  `withGlobalTauri` and internals injection are not proven from cached data.
- Plugin commands (`plugin:<name>|<cmd>`) are reported with status plugin and
  never linked, even for proven plugin registries in the repository (T015d).
- A cfg on the statement that holds a call-rooted chain is not cached, so such
  a registry is not marked conditional.
- `#[macro_use] extern crate` can bring a macro named like a builtin into
  scope; the resolver does not see it, so it would be allowed as the builtin.
- An assignment after the invoke site inside a loop is not a possible value
  of the root; by-value consumption means it can only feed the next
  iteration's builder of the same type.
- The ACL check reads `build.rs` at the package root only, not a `build`
  script configured elsewhere.
- G10 is defensive: destructuring lets are never recorded as bindings, so
  they surface through Shadows (G9) or a missing binding (G8).

## Revision 2 (review 1)

Review 1 found two confident wrong facts and five smaller gaps. The reviewer
decisions change the rules as follows (status stays Proposed).

- **Cfg-gated duplicate arms (M1).** Tauri emits each `generate_handler!`
  entry's attributes on its own match arm (tauri-macros `handler.rs`), so an
  earlier cfg-gated arm does not make a later arm of the same name
  unreachable. For a name, the arms are collected in entry order up to and
  including the first arm without a cfg; only later arms are `unreachable`
  (with `tauri_command_duplicate`). With two or more collected arms the call
  links to each (it is the target in some build): the call carries
  `conditional: true` and each alternative its `target` and `cfg_gated`
  ("may invoke, depending on build configuration"). Every linked alternative
  now lists its target, also for runtime alternatives (R2).
- **Application scope (M2, m5).** A1 is decided over the production scope.
  Registries in test-scope files (ADR 0021) or in cfg(test)-only regions, and
  registries rooted at `tauri::test::mock_builder()` (reason `mock_builder`),
  neither prove nor block it. The single proven app also fails when the
  production scope holds other Tauri app evidence: a Tauri `Builder::default`
  / `Builder::new` call (any alias) its proof did not go through, more than
  one Tauri `generate_context!`, or main config files (`tauri.conf.json`,
  `tauri.conf.json5`, `Tauri.toml`) in more than one directory (status
  `ambiguous`, `tauri_app_ambiguous` per site); or an `invoke_handler` call
  whose argument is not Tauri's `generate_handler!` (a closure, a binding,
  another crate's macro), which is an unproven handler (status
  `registry_unproven`, `tauri_registry_unproven [handler]`). tauri-utils
  `config/parse.rs` (tauri-utils-v2.10.2) reads one main config per
  directory, in that order, and merges platform files
  (`tauri.<platform>.conf.json[5]`, `Tauri.<platform>.toml`) into it, so
  those overlays and a second format in the same directory are not another
  app. Only when the production scope has no registry and no app site are
  test-scope registries matched (I5 unchanged).
- **Wrapper writes (m1).** A callable is a wrapper only when its source text
  (nested functions included) holds no write to the forwarded parameter:
  assignment of any kind, `++` / `--`, a destructuring target, a for-in/of
  head, a function or class declaration of the name, or `eval`, `arguments`
  or `with`. The client extractor's cache drops the binding only for plain
  and compound assignments, so the check reads the wrapper's source at link
  time. (Revision 3 corrects the claim made here that this "can only reject
  a real wrapper": the lexer could misread the source and accept a writing
  callable; see Revision 3, MA1.)
- **Test-only commands (m2).** A command whose own cfg, enclosing item or
  module cfg in the same file, or `cfg_attr` predicate is exactly `test` or an
  `all(..)` containing it (never `any` or `not`), or whose entry's cfg is, or
  whose handler sits in such a region, is `test_only: true`. Production calls
  never link to it (status `test_only` with candidates when only such arms
  carry the name); test-scope calls do.
- **Function attributes (m3).** Every attribute of a handler's function
  (cfg_attr contents included) must be a builtin `cfg`, `cfg_attr`,
  `allow`, `deny`, `doc` or `inline`, `tokio::main` (tokio-macros 2.7.2
  `entry.rs`: the body is re-emitted unchanged inside `async`) or
  `tauri::mobile_entry_point` (tauri-macros 2.7.4 `mobile.rs`: the item is
  re-emitted unchanged); any other attribute may rewrite the body, so the
  registry is unproven [macro]. This applies to call-rooted chains too.
  kftray's `fn main` carries no attribute.
- **Payload.** `TauriSyntax` version 3 adds `TauriCommand.Test`,
  `TauriHandler.Attributes`, `TauriHandler.Test` and `Apps` (app sites:
  `root`, `invoke_handler`, `context`, with a `Test` mark). A file that only
  has app sites now carries a payload, so files cached as payload-free by an
  extractors-26 build would hide app evidence: the aggregate version becomes
  extractors-27. Config files come from the scanned sources, not the payload.
- **Fixture.** The shadowing package manifest is stored as `_package.json`
  and restored only in the shadowed scenario, so it no longer hides
  `@tauri-apps/api` for the Graphvein self-index (m4).

Consequences: kftray is unchanged (115 / 115 / 105 / 103, 0 diagnostics); its
production scope holds one Tauri builder root (the proof's own), one
`generate_context!`, no other invoke_handler call and one config directory
(`tauri.conf.json` with a `tauri.linux.conf.json` overlay).

Known gaps added by Revision 2:

- App sites miss builders made through a type alias, `Default::default()`, a
  helper function or inside another macro's token tree; a second app built
  that way still lets the first one link.
- Two `generate_context!` calls of one app (for example behind
  `#[cfg(desktop)]` and `#[cfg(mobile)]`), or a test builder outside a test
  scope and outside a cfg(test) region, block every match (under-claim).
- An out-of-line `#[cfg(test)] mod x;` declared in another file does not make
  its commands test-only; a `#[test]` function outside a cfg(test) module is
  not a test region.
- Attribute macros on an enclosing module or impl, and inner attributes in a
  function body, are not checked.
- The wrapper write check reads source text with a small lexer. (This entry
  was incomplete: a regular expression after `)` or `}`, a division after
  `++`, identifier escapes and JSX text could all hide a write. Revision 3,
  MA1 makes the lexer fail closed.)

## Revision 3 (review 2)

Review 2 found two more confident wrong facts (MA1, MA2) and two gaps (mi1,
mi2). The reviewer decisions change the rules as follows (status stays
Proposed). Payload version 3 changes in place (still unmerged; extractors-27
unchanged).

- **Wrapper write check fails closed (MA1).** The TypeScript caches do not
  record update, destructuring or for-head writes to a parameter: RouteScope
  bindings are reset only for identifier assignment targets and
  `URLBinding.Mutated` only for member mutations at module scope (rule 32
  check), so the check stays a link-time lexer over the wrapper's source. It
  now treats the parameter as written (no wrapper) whenever its tokens may
  differ from the real ones: a string that reaches a line break or the end;
  a template, regular expression or block comment that reaches the end; a
  `/` after `)`, `]`, `}`, `++`, `--` or any keyword (reserved or
  contextual), where regex or division depends on the grammar; a backslash
  or non-ASCII byte outside literals and comments (escaped or non-ASCII
  identifiers, non-ASCII spaces); unbalanced or mismatched brackets; and,
  in files that may hold JSX (every extension but `.ts`, `.mts`, `.cts`,
  including `.js`), a `<` followed by an identifier, `>` or `/`. The
  guarantee is now: a writing callable is accepted only if the lexer's token
  stream is the real one in an input it does not flag; every construct
  above where it is known not to be is rejected. Correct wrappers using one
  of these constructs are rejected too (under-claim). Probes q1 (v1-v6, V7)
  and q2 (u1, u3) are no longer wrappers; u2 (an apostrophe inside a
  template, no write) still is. kftray's two wrappers (`lib/tauri.ts`
  `invoke` and the test's `reject`) are unaffected.
- **Second-app evidence (MA2).** Every production reference to Tauri's
  Builder outside the proof's own root is app evidence, so a second app in
  the same crate that shares the library's `generate_context!` and config
  is seen. The payload's app sites now record every value-position path
  through a Builder-like name (`Builder` or a file `use` alias, transitively)
  with generic arguments and qualified-path brackets removed (constructor
  calls, `<tauri::Builder<R>>::default()`, function-path values such as
  `tauri::Builder::<R>::default`), `default` / `new` values typed as a
  Builder-like path in a let or a function return, struct fields of that
  type, type aliases of it, visible re-exports of a Builder-like name or a
  glob (always evidence when they resolve to tauri, conservative), and
  macro_rules! bodies or macro invocation token trees naming a Builder-like
  identifier (with `tauri::Builder` added when the trees also name `tauri`).
  A site counts when its path resolves to `tauri` `Builder` or
  `app::Builder` (or is ambiguous); a glob counts for `tauri` or
  `tauri::app`. Linking also resolves production `default` / `new` calls
  from the Rust call cache, which covers renames through another module's
  private `use` (`use super::B as C; C::new()`). Type annotations in
  function signatures are not evidence. Probes b0-bl each give `ambiguous`
  on their own, without the probes' shared `pub use`.
- **Complementary cfg pairs (mi1).** When two arms of a name are each gated
  only by one entry `#[cfg(P)]` and `#[cfg(not(P))]` (whitespace outside
  string literals ignored, either order, test-only arms included), they
  exist in every build: the candidate list stops at the second, and later
  arms of the name are `unreachable` with `tauri_command_duplicate`. Probe
  e1 now links to `g1` and `g2` only. Entry attributes in the payload are
  compacted outside string literals only, so `"a b"` and `"ab"` differ.
- **Documentation (mi2).** docs/extract/tauri-commands.md lists the wrapper
  check limits and the app-site scope.

Consequences: kftray is unchanged (115 / 115 / 105 / 103, 0 diagnostics, both
wrappers recognized). Its payload grows to 36 of 270 Rust files with 30 app
sites (17 roots, 12 glob re-exports of local modules, 1 context); the
production evidence is its own root (main.rs:164) and one
`generate_context!`, so no other-app evidence.

Known gaps added or kept by Revision 3:

- Only exact `P` / `not(P)` pairs are recognized; general cfg
  satisfiability (`any(a, b)` with `all(not(a), not(b))`, three-way splits)
  is not evaluated, so later arms stay targets (an over-claim of "may
  invoke, depending on build configuration" edges).
- A Builder renamed in another module's private `use` and referenced other
  than as a `default` / `new` call; an ambiguous callee of such a call; a
  `default` / `new` value typed only through a type alias (the alias itself
  is a site); a builder built from macro parameters (`$t::Builder` with
  `m!(tauri)`); a re-export in a crate outside the repository. A call found
  only through the call cache counts even inside a cfg(test) region
  (under-claim).
- The wrapper lexer rejects correct wrappers that divide after `)`, use a
  keyword before `/`, contain non-ASCII text outside strings, or use type
  arguments or comparisons in JSX-capable files.

## Review 3

Review 3 found one more confident wrong fact (MA3) and one gap (mi3). Status
stays Proposed; the payload and contract are unchanged.

- **Non-null assertions fail closed (MA3).** The wrapper lexer did not model
  TypeScript's postfix `!`: `cmd! = 'o'`, `cmd! += 'x'`, `(cmd)! = 'o'`,
  `cmd!++` and `cmd! ??= 'o'` hid the write from both the lexer and the
  TypeScript extractor, and in `n! / 2, [cmd] = ['o'], n! / 1` the `/` after
  `!` was read as a regular expression that swallowed the write. A lone `!`
  (not `!=` / `!==`) after an identifier, `)` or `]` now makes the lexer
  unsure, so the callable is not a wrapper. After a keyword that starts an
  expression (`return !x`) the `!` is the prefix operator and stays allowed;
  after `of`, `yield` or `await` (possible identifiers) it is unsure.
  Probes q3 a5-a7 and q4 d1, d2, d4 are no longer wrappers. kftray's two
  wrappers contain no `!` and are unaffected.

Known gaps added by review 3:

- Second-app evidence does not cover a Builder made only through a trait
  impl on `tauri::Builder` returning `Self::default()` (called as
  `Make::make()`), a generic helper (`mk::<tauri::Builder<R>>()` with
  `B::default()` inside), or an inferred `let b = Default::default()` typed
  only by later use. A wrong edge needs all of: a second app built only
  through such a form, with no `generate_handler!` / `invoke_handler` of its
  own, sharing the first app's `generate_context!` and config, and frontend
  code loaded only by that second app. The edge stays true for the first app
  whenever the frontend is shared.
- Correct wrappers that use a non-null assertion (`invoke(cmd!)`, `x!.y`)
  are rejected (under-claim).

## Review notes (delegated review, 2026-11-02)

- Delegated review in place of developer review. Accepted after
  Revision 3 and the review 3 fix. Results:
  - The investigation: precision stop P2 (G8 self-binding) resolved by task Revision 1.
  - Review 1: 2 major (cfg-gated duplicate arms; second app invisible) and
    5 minor findings, fixed in Revision 2 (payload version 3,
    extractors-27).
  - Review 2: 2 major (wrapper lexer failed open; second-app evidence gaps)
    and 2 minor findings, fixed in Revision 3.
  - Review 3: 1 major (postfix `!` non-null assertion), fixed in 169d06d;
    1 minor, documented as a known gap.
  - Gates: records outside the new kinds unchanged on all ten public
    checkouts and the self-index; kftray 115 ipc_command, 105 ipc_call,
    103 invokes_command, 0 tauri_* diagnostics; warm replay parses 0 files;
    timing kftray +1.89% fresh, +5.72% warm, Langflow +0.86% fresh, +0.36%
    warm; fresh store Langflow +0.00%, kftray +0.83%; CI green, including
    the postgres job.
- Revision 1 (task): binding statement end (`End`) and flow macro records
  close the G8 self-binding stop.
- Revision 2: cfg-gated duplicate arms, production app scope, wrapper writes,
  test-only commands, every function attribute, payload version 3.
- Revision 3: fail-closed wrapper lexer, Builder reference evidence and
  complementary cfg arms.
- Gaps: see Known gaps above and the gap lists added by Revision 2,
  Revision 3 and Review 3.
