# TypeScript and JavaScript bridges: claim contract

Status: contract of T026 (2026-11-18, decision O4 (b) of
`docs/contract/claims/architecture-graph.md`, ADR 0079 revision "AG-B5"),
`extractors-48` (one version for the T026 stack). The rows are kept 1:1
with `bridgeRows` in `internal/lang/typescript/bridges.go` by
`TestTypeScriptBridgesContract`, which also checks that the fixture
`testdata/fixtures/ts-bridges` reaches every row and the default branch D;
`TestTypeScriptBridgesFixture` keeps its records in
`testdata/fixtures/ts-bridges.golden.json`.

Scope: files of the TypeScript structure contract
(`docs/lang/typescript-structure.md`) that row TS-F2 does not refuse. The
syntax is collected during the structural walk (`bridges_extract.go`) and
cached with the file's facts; names are resolved anew on every link
(`LinkBridges`), never cached.

**Every form no row below names goes to the single default branch D: it
claims nothing (no node, no edge). D is not counted in a diagnostic.**

## Name resolution (all rows)

A name at a site (a JSX tag, a passed value, a route key) resolves the way
the calls scope resolves a callee: `this.x` inside a method of class `C`
to `C.x`; else the scope chain of the file's declarations (innermost
owner first, ADR 0070 qualified names); else the file's import binding of
the first segment through the Precise import graph (TS-I1..I4 resolution,
export clauses, barrels, `export *`, namespace members), so a member chain
`NS.Button` resolves through a namespace import. The result must be a
declaration of the row's kinds, not one of a repeated group (TS-D12), not
in a file TS-F2 refuses, and the binding not ambiguous.

A name is **shadowed** (D) when its first segment is bound locally at the
site: a parameter of an enclosing function (binding patterns included:
`function Row({ onClick })`), a non-function `const`/`let`/`var`, `catch`
or `for ... of/in` binding anywhere in an enclosing function's body (a
binding shadows throughout its scope), or a parameter of an enclosing
anonymous callback (`xs.map((Item) => <Item />)`).

**Source** of a JR or PF edge: the nearest enclosing named function or
method (TS-D1..D8; anonymous callbacks add no owner, like calls), else the
file node (module-level code, class field initializers). Evidence: the
JSX opening (or self-closing) element for JR, the passed expression for
PF. Several sites of one source and target merge into one edge with
several evidence entries. Every edge is `provenance: fact`, confidence 1.

## JSX renders (rows JR)

`renders` states that the source creates a React element of the target
(React's element owner): not that the element mounts (a parent may drop
its `children`) and not a call.

| row | form | claim |
|---|---|---|
| JR1 | `<X ...>` or `<X ... />` whose tag (an identifier with an upper-case first letter, or a member chain `A.B`) resolves to a function or class declared in the same file | `renders` source → that declaration |
| JR2 | the same tag form resolving through an import binding to a function or class of another repository file | `renders` source → that declaration |
| JR3 | the tag is a module-level `const X = lazy(() => import("m"))` (or `React.lazy`), where `lazy` is the `react` package's export (named import, or the default or namespace import of `react`), the function's only result is `import("m")` with a string literal, and m's default export resolves to a function or class | `renders` source → that default export (React's documented lazy contract) |

## Passed functions (rows PF)

`passes_function` states that the function is passed as a value; the call
(by React, the router, a library) is not modelled. The chain draws it as
"passed as callback, call not modelled".

| row | form | claim |
|---|---|---|
| PF1 | a call argument that is an identifier or member chain (`setTimeout(tick)`, `xs.map(render)`, `memo(CardImpl)`) resolving to a function or method | `passes_function` source → it |
| PF2 | a property value or shorthand property of an object literal argument, or an array literal element, at any depth of object and array literals of an argument (`useMutation({ mutationFn: createItem })`, `createFileRoute("/x")({ component: Items })`) resolving to a function or method | `passes_function` source → it |
| PF3 | a JSX attribute whose value is `{name}` with an identifier or member chain (`onClick={handleClick}`, `loader={rootLoader}`) resolving to a function or method | `passes_function` source → it |

## react-router routes (rows RR)

Read from react-router 7.13.0 and 7.18.2 (`dist/development`, the
versions of the react corpus and langflow; react-router-dom 7 re-exports
them; the same route keys exist since 6.4): `createRoutesFromChildren`
maps a `<Route>`'s props `element`, `Component`, `index`, `path`,
`loader`, `action`, `lazy` and nested `<Route>` children to a route
object; `mapRouteProperties` replaces `element` by `createElement(Component)`
when `Component` is set; `loadLazyRoute` assigns the properties a lazy
function's result holds, except a property the route defines statically
(the static value wins) and `lazy`, `caseSensitive`, `path`, `id`,
`index`, `middleware`, `children`.

A route is a `ui_route` node (level 4, `framework: "react-router"`;
identity as Angular routes, `docs/lang/identity.md`: `<owner>.ROUTE <p>`,
literal children nested under their parent, `@N` for repeats in source
order among the file's candidates) with a `contains` edge from its literal
parent route, else the nearest emitted declaration containing it, else the
file. Attributes: `path` (the literal path, `null` when it is an
expression; absent when not written), `index: true`, `lazy` (`module`,
`object` or `unknown`), `open: true` (a spread or computed key), and
`component`, `element`, `loader`, `action`: the linked targets. The path
is the route's own segment as written, not a composed URL and not an HTTP
route.

| row | form | claim |
|---|---|---|
| RR1 | an object literal reached from the first argument of a call whose callee is react-router's `createBrowserRouter`, `createHashRouter`, `createMemoryRouter` or `useRoutes` (named or namespace import of `react-router` or `react-router-dom`, not shadowed): array elements, `children` array elements, and names (or spreads of names) resolving in the file or through an import binding to a module-level `const` whose initializer (through parentheses, `as`, `satisfies`) is such an object or array | `ui_route` node and `contains` edge |
| RR2 | a JSX element whose tag is react-router's `Route` (named or namespace import of `react-router` or `react-router-dom`, not shadowed); `<Route>` elements among its JSX children (through fragments and expressions) are its literal children | `ui_route` node and `contains` edge |
| RR3 | a route without spread or computed keys: a static `Component` whose value resolves to a function or class (JR rules); a JSX `element` value's tags outside nested functions resolving as JR1..JR3, when the route has no static `Component` and no `lazy` that may supply one | `routes_to` route → each target |
| RR4 | a static `loader` or `action` whose value resolves to a function or method | `passes_function` route → it |
| RR5 | a `lazy` function of form `module` (its only result is `import("m")`: m's exports `Component`, `loader`, `action`) or `object` (it returns an object literal whose values are names bound by `const { E: x } = await import("m")` or `const [{ E: x }, ...] = await Promise.all([import("m"), ...])` in its body, or `(await import("m")).E`), for each of `Component`, `loader`, `action` the route does not define statically, when the export resolves to a function or class (`Component`) or function (`loader`, `action`) | `routes_to` (Component) or `passes_function` (loader, action) route → it |

## Default branch D

Claims nothing: intrinsic elements (`<div>`, a lower-case first letter or
a dash) and fragments; JSX namespace names and computed tags; tags and
values that resolve to nothing in the repository (packages: `<Link>`,
`<Outlet>`), to a variable (`export default memo(Card)`, `styled.div`), an
interface or a repeated group, or are shadowed; inline functions passed as
values (`onClick={() => f()}`: their calls are the calls scope's);
`new X(f)` arguments; spread arguments and properties; a `lazy` binding
whose callee is not React's; route objects no proven router reaches (an
unused const, a `<Route>` of another package or of the repository); route
keys with inline values (`loader: async () => ...`); a static `element`
when a static or lazy `Component` replaces it; a route with a spread or
computed key (`open`); `lazy` in any other form (an identifier, the
object form `lazy: { Component: ... }` of react-router 7.5, a result built
by `.then`), and its keys; `ErrorBoundary`, `errorElement`,
`HydrateFallback`, `middleware`, `handle`, `shouldRevalidate`; TanStack
Router and other routers' route definitions (their component values are
PF2 only).

## Consumers

The architecture chain (T026 row CH8) follows `renders` ("renders" /
"rendered by"), `passes_function` ("passed as callback, call not
modelled") and `routes_to` as dashed bridges. Change impact (ADR 0056, T019
Revision 7) lists react-router routes and React bridge edges under row C17
("React UI"), other `passes_function` edges under C16, and follows
`renders` and `routes_to` into function components (I14) and
`passes_function` (I15, always inferred: "passes this as callback, call
not modelled").
