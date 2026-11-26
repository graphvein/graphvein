# Server actions

ADR 0077 (T024b, task `docs/contract/claims/nextjs-supabase.md`); extractors-49,
contract v1 unchanged (the new kinds are extensible, `docs/contract/v1.md`).
React server actions in Next.js projects are a third API transport,
`server_action`, beside HTTP and Tauri IPC (decision O1 (a)): an action is
a POST to whichever page renders it, identified by an action ID header, so
it has no route of its own.

## Model

- One level-4 `server_action` node per action, `contains` from the
  declaring file (module actions) or the nearest enclosing emitted owner
  (inline actions), and an `exposes_action` fact edge from the function or
  variable node that implements it (none for an anonymous inline action).
  Attributes: `framework` `nextjs`, `transport` `server_action`, `action`
  (the export name, `default` for the default export, the bound name of an
  inline action, null when anonymous), `action_scope` (`module` or
  `inline`), `form` (the row form below), `handler` (the implementing node
  or null), `reassigned` (a `let` / `var` inline action whose name is
  assigned in the file), `unread_uses` (R1: the number of counted unread
  uses that name it) and `scope: test` in test scope. Identities: see
  [identities](../lang/identity.md#server-actions).
- One level-4 `action_call` node per linked use, `contains` from the
  nearest emitted owner of the use, and a `calls_action` fact edge to the
  action. Attributes: `framework`, `transport`, `status` (always `linked`:
  unproven uses are counted, never stored), `action` (the target identity),
  `form` (`form_action`, `action_state` or `direct`), `attribute`
  (`action` or `formAction`, form_action only), `hook` (`useActionState` or
  `useFormState`, action_state only), `boundary` (`client` or `unknown`,
  direct only), `client_entry` (the `'use client'` file a module of the
  client module graph is reached from, when it is not that file itself),
  `shared_module` (R1: the module is in both module graphs, boundary
  unknown), `server_graph_incomplete` (T024 review: a server-graph
  file with a dynamic `import()` or `require()`, boundary unknown),
  `conditional` and `alternatives` (R1: a conditional value;
  `action` is then null unless one action is named, and there is one
  `calls_action` edge per alternative) and `scope: test`.
- A direct call keeps its ordinary `calls` edge (code calls, row D of the
  structure contract); the `action_call` is added beside it.
- Only files under a directory whose `package.json` declares `next`
  (dependencies or devDependencies) declare actions, as row NX-P1 finds
  Next.js projects. A use may be anywhere in the repository.

## Syntax cache

`facts.Facts.Actions` (`facts.ActionFacts`, omitted when empty) holds the
file's directive prologue (the leading string-literal expression
statements, TypeScript compiler semantics: comments are skipped, a
parenthesized string ends the prologue, a string with an escape sequence
keeps its raw text), the top-level exports of a `'use server'` module with
their form, the functions whose own body prologue holds `'use server'`
with their route scope, and the JSX attributes `action={...}` and
`formAction={...}` and every attribute whose value is a plain identifier,
with the route scope they read in. The route walker
(`internal/lang/typescript/routes.go`) records the scope-bound parts, so a
use resolves through the same lexical scopes and module resolver as the
client calls (ADR 0020); nothing in the cache depends on another file.

## Claim contract

The rows are those of the task file (`TestServerActionsContract` keeps the
task table, this table and `actions.Contract` 1:1, and
`TestServerActionsFixture` checks that the fixture
`testdata/fixtures/nextjs-actions` reaches every row and D).

| id | form | claim | refusal / unknown |
|---|---|---|---|
| SA-M1 | a file whose directive prologue holds `'use server'` (TypeScript compiler semantics: leading string-literal expression statements), not `'use client'`; each export: `export async function`, `export const a = async (...) => ...` / `async function`, `export const a = <call>` (a wrapper's result), `export { a }` of a local binding of those forms, (R1) `export { a }` of an imported binding (`imported_clause`: Next.js registers it, next-custom-transforms `server_actions.rs`; only an import of this file names the action), `export default async function`; (T024 review, `server_actions.rs` `ExportDefaultExpr` at next 14.1.0 to 16.4.2) `export default async (...) => ...` (`default_async_arrow`), `export default <call>` (`default_call`), `export default a` of a local binding of the forms above (`default_clause`) or of an imported binding (`imported_clause`) | a server action named by the export (`scope: module`), its node pointing at the exported binding (for `imported_clause` the imported function, when resolved) | a non-function export (`export const x = 1`, a class): D (a load error, `ensureServerEntryExports`); a sync function written in the file and exported directly, through `export { a }` or through `export default`: D (a build error at next 15.1.4 to 16.4.2, where every exported function inherits the file directive and must be async; at 14.1.0 only a sync `function` exported directly and a sync default arrow are build errors, while a sync arrow and a sync function exported by name are registered, since the runtime check tests only `typeof === 'function'`: there D misses an action, never claims one); `export { a } from` and `export *`: D (build errors in `server_actions.rs`) |
| SA-I1 | a named async function, or an async arrow / function expression bound to a `const`, (R1) `let` or `var`, whose body's directive prologue holds `'use server'`, in a file without `'use client'` | a server action (`scope: inline`); a `let` / `var` binding assigned anywhere in the file is `reassigned`: its uses are not linked | anonymous inline actions (`action={async () => { 'use server' ... }}`): the action node with the enclosing owner, no name (R1: linked as the form's action by SA-C1); a sync function or a class method: D; in a `'use client'` file: D (build error) |
| SA-C1 | JSX attribute `action={a}` on an intrinsic `<form>` or on next/form's `Form` (its default import), or `formAction={a}` on an intrinsic `<button>` or `<input>` (T024 review: React's form actions), `a` resolving (shared resolver, through imports) to an SA-M1/SA-I1 action; (R1) an inline SA-I1 action written as the value; (R1) a conditional `c ? a : b` (nested, parenthesized) whose alternatives include such bindings | an `action_call` node contained by the owner with a `calls_action` edge to the action; `form: form_action`; a conditional value is one use with `conditional: true`, `alternatives` and one `calls_action` edge per action alternative (each a possible submit target) | any other expression (call, member), an unresolved binding: D (counted); `action` / `formAction` on any other element (a component's prop, which the component may use as it likes): D (counted, as a prop) |
| SA-C2 | `useActionState(a, ...)` from `react` or `useFormState(a, ...)` from `react-dom`, `a` as in SA-C1 | `calls_action`, `form: action_state` (conditional as SA-C1) | as SA-C1 |
| SA-C3 | a direct call `a(...)`, `a` resolving to an SA-M1/SA-I1 action | `calls_action`, `form: direct`; `boundary: client` when the call's file has `'use client'` in its prologue, or (R1) it is reached from such a file through resolved value imports and value re-export clauses (`export { a } from`, `export *`, `export * as ns`; the client module graph, `mapped`) and no server entry point (a file under `app/` or `pages/`, middleware, instrumentation) reaches it through imports or re-export clauses that may load it (any that is not type-only; T024 review) without a `'use client'` boundary, following also `import()` and `require()` of a string literal, and no file of the server graph holds one the resolver cannot follow (a non-literal or unresolved specifier; else `server_graph_incomplete` names the first); otherwise `boundary: unknown` (a module in both graphs also gets `shared_module: true`; never `server`, never `client` without that proof) | an unresolved callee: D; the ordinary `calls` edge stays as today |
| D | props passing an action to a component, `.bind(...)`, actions read through objects, `startTransition` callbacks other than a direct call, dynamic import | nothing linked; counted (`server_action_use_unread`); (R1) each action such a use names gets `unread_uses` and is never reported as not called | always |

Every action fact is `mapped` except the directive and the export names,
which the TypeScript compiler oracle checks.

Forms (`form` attribute, and the reason of a `server_action_unread`
diagnostic): SA-M1 `async_function`, `const_async_arrow`,
`const_async_function`, `const_call`, `clause` (`export { a }` of a local
async function or `const` of one of those forms), `imported_clause`
(`export { a }` or `export default a` of an imported binding),
`default_async_function`, `default_async_arrow`, `default_call`,
`default_clause` (`export default a` of a local binding of the forms
above; T024 review); refused `sync_function`, `non_function`, `class`, `let_binding`,
`reexport` (`export { a } from`), `star`, `default_other`. SA-I1
`named_function`, `const_arrow`, `const_function`, `let_binding` (a `let`
or `var` binding; `reassigned: true` when the name is assigned anywhere
in the file), `anonymous`; refused `sync`, `method`, and any inline
`'use server'` function in a `'use client'` file. A file whose prologue
holds both directives declares no action. The Next.js semantics come from
`next-custom-transforms` 0.1.0 (`src/transforms/server_actions.rs`, crates.io,
January 2025, between the corpora's Next.js 14.1 and 16.2), re-read for
the T024 review at the corpora's locked versions (`next` 14.1.0, 15.1.4,
15.6.0-canary.59, 16.2.10, 16.4.1, 16.4.2:
`crates/next-custom-transforms/src/transforms/server_actions.rs`):
`export { a }` registers any local or imported binding, `export { a }
from` and `export *` are build errors, inline actions are any async
function or arrow with the directive whatever binds it, a sync one is a
build error, `export default` registers an async arrow, a call and an
identifier (a sync default arrow is a build error), and exported values
are checked to be functions when the module loads
(`ensureServerEntryExports` tests `typeof === 'function'` only). From
15.1.4 every exported function inherits the file directive, so a sync one
is a build error however it is exported; at 14.1.0 only a sync `function`
exported directly is one, and a sync arrow or a sync function exported by
name is registered (row SA-M1 sends both to D: a missed action, never a
wrong one).

### Resolution

An identifier at a use resolves through the route scopes of its file
(lexical shadowing, parameters, type-only imports name no value) and, for
an import, through the module resolver's export binding (re-exports,
barrels, aliases, default exports); the binding is an action when it is
the implementing node of an action, an inline action's own function scope,
or a top-level name a module action binds. Anything ambiguous or not
followed is unresolved; a resolved binding that is not an action is not a
use at all. `useActionState` / `useFormState` are recognised imported by
name or read from a namespace or default import of `react` / `react-dom`
(the module resolver decides that the specifier is the package).

### Client and server module graphs (row SA-C3)

The client module graph is every file whose prologue holds `'use client'`
and every file reached from one through imports that load a module at
runtime: a side-effect import, or an import with a binding that is not
type-only and names a function, method, class or variable node or a
namespace, resolved to a scanned file by the module resolver, and (T024 review) a re-export clause with a module (`export * from`,
`export * as ns from`, `export { a } from` naming such a node), which
loads that module whenever the barrel loads. A
`'use server'` module is never entered (its code runs on the server; a
client import of it references its actions).

The server module graph (R1) starts at every server entry point of a
Next.js project that does not open with `'use client'`: the scripts under
`app/`, `src/app/`, `pages/` or `src/pages/` (server components, route
handlers, Pages Router pages, which also render on the server) and the
project's `middleware`, `proxy` and `instrumentation` files; it follows
every import and re-export clause that may load a module (any that is not
type-only, also one whose bindings the resolver cannot prove to be values:
the compiler may or may not elide it), so it may hold more modules than
load, never fewer of those it can resolve, and it never enters a
`'use client'` file (the server renders a reference to it). It also
follows `import()` and `require()` of a string literal (a template literal
without substitutions included; `facts.ActionFacts.Loads`). One the
resolver cannot follow (a non-literal specifier, a path it does not
resolve) may load any module: no module is then proven client-only, and
every call that would read `client` through the client graph reads
`boundary: unknown` with `server_graph_incomplete` naming the first such
file (path order).

`boundary: client` when the calling file opens with `'use client'`, or is
in the client graph and not in the server graph (`client_entry` names the
`'use client'` file it is reached from). A module in both graphs is in
both bundles, so its call may run on the server: `boundary: unknown` with
`shared_module: true`. Everything else is `boundary: unknown`, never
`server`. The client graph may miss a module (dynamic `import()`,
`require`, names the resolver does not follow): that module stays
`unknown`. The server graph misses only imports the module resolver
cannot resolve to a scanned file (a path alias it does not read, a
generated file): such a miss could leave a shared module labelled
`client`, a known gap.

### Counted refusals

`server_action_unread` (info): a declaration form no row reads.
`server_action_use_unread` (info): an `action` / `formAction` attribute
on an element other than the intrinsic `form` (action), `button` or
`input` (formAction) and next/form's `Form` (action), counted as a prop
(T024 review); an `action` / `formAction` value that
is not an identifier (a conditional, call or member) other than a plain
client function, or an unresolved identifier there; a `useActionState` /
`useFormState` first argument of those kinds; an action passed as a JSX
prop or as a call argument; `a.bind(...)` and other member reads of an
action; an action read from a namespace object. Each action such a use names
gets the node attribute `unread_uses` (the count): the API section shows it
as "may be called (unread use)" and never counts it as not called (R1).
A conditional whose alternatives name actions is linked (SA-C1, SA-C2); one
with an unresolved alternative and no action is counted.

## Oracle

`TestServerActionsOracle` (`internal/index`, opt-in with
`GRAPHVEIN_TS_ORACLE_DIR` as the TypeScript structure oracle, corpora
through `GRAPHVEIN_ACTIONS_ORACLE_CORPORA`) runs
`scripts/oracle/nextjs_supabase/server_actions.cjs` (TypeScript 6.0.3:
directive prologues, export names, checker alias resolution of every
identifier) and fails when Graphvein claims an action the compiler does not
prove or links a use the checker does not confirm with the same form.
The checker follows aliases one step at a time, so an import through a
`'use server'` file's `export { a }` names that file's action, and an
alternative of a conditional value takes the form of the whole value.
Results (T024b R1): fixture 12 actions and 20 linked uses (each
alternative counted) agree; on the six T024 corpora 25/25 actions and
21/21 linked uses agree, 0 disagreements; the compiler's 6 further uses
(props in supabase-stripe) are counted D.

Boundary (T024 review): the script also computes row SA-C3's module
graphs with the compiler's own module resolution (`ts.resolveModuleName`)
and import elision (an import binding is kept when its symbol is a
value), the server graph over every import and re-export that is not
type-only plus `import()` and `require()` of a string literal; a direct
call Graphvein labels `client` must be one those graphs prove (`unknown`
claims nothing). Results (T024 review fixes): fixture 15 actions, 26
linked uses, 9 direct calls, 3 `client` labels proven; corpora 25/25
actions, 20/20 linked uses, 4/4 `client` labels proven, 0 disagreements.

## Shape

Each `server_action` node also carries the route shape attribute `shape`
(T029, rows SH-N27..SH-N29 of the [Next.js route shape claim
contract](../contract/claims/route-shape-nextjs.md)): the form fields its
`FormData` argument reads, the type of a single typed argument, and its
results. See [route shape](route-shape.md#nextjs-app-router-route-handlers-and-server-actions).

## Known gaps

Actions passed through props, `.bind`, namespace objects and other value
flow (counted, `unread_uses`); uses of a reassigned `let` / `var` action;
object method
and static class method actions; `'use server'` outside Next.js projects;
the boundary of calls in modules reached only through dynamic imports or
`require` (unknown), server imports the module resolver cannot resolve (a
shared module could then read `client`), and the server graph is one for
the whole repository (a dynamic import in one project makes the client
labels of every project unknown); impact traversal from server actions in Changes (row C18
classifies them; no relation row follows them yet).
