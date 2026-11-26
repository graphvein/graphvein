# Angular router syntax

ADR 0035, payload version 2 per ADR 0036 Revision 1; aggregate extractors-49.
This stage caches source syntax only. Angular routing nodes, edges, validation
diagnostics and path composition are linked from this payload by
[Angular routes](angular-routes.md) (ADR 0036). Earlier payloads and graph
behavior, contract v1 and store interfaces are unchanged.

`typescript.CachedFacts.router` holds RouterSyntax version 2 with required calls,
loaders, arrays, configs and methods. Empty arrays are valid. Missing, obsolete
(version 1) or incomplete payloads force reparsing through the existing
fact-cache interface.

Calls keep offset, full span, kind, compact callee, argument count, optional
first DeclarationExpr, scope and source shadows. Decorator calls qualify too:

- provide_router: a named import whose imported name is provideRouter
  (including aliases, any specifier), or namespace-import.provideRouter.
- for_root / for_child: forRoot/forChild members with an imported receiver root.
- bootstrap_module: any bootstrapModule member call; also retain receiver
  syntax and the callee of a called receiver.
- bootstrap_application / create_application (version 2): a named import whose
  imported name is bootstrapApplication / createApplication (aliases included),
  or namespace-import.bootstrapApplication / .createApplication. `Options`
  keeps the ApplicationConfig argument as a DeclarationExpr: the second argument
  of bootstrapApplication, the first of createApplication.

F5 found correct existing scopes for provideRouter and bootstrapModule, but no
client records for seven top-level NgModule decorator calls. Reuse an existing
scope after matching the full span/callee and checking source binding agreement;
offset alone cannot disambiguate chains. Otherwise use module scope plus sorted
relevant shadows from parameters, blocks, loops and catch clauses. Default
expressions are not bindings. Earlier client scope behavior stays unchanged.
Relevant local shadows use the fallback because older scopes lack declaration
spans to prove the nearest same-name local. Scope roots include called receivers
and subscript arguments. Type annotations on identifier callback parameters are
transparent.

For calls, relevant names are the callee root and every identifier or shorthand
property identifier anywhere in the first argument subtree (and, for the two
application kinds, the options subtree). Property keys,
member property names and type nodes are excluded. Both shadow intersection and
cached-scope reuse use this set. Nested function references may conservatively
over-include names, making resolution unresolved. Sync-loader shadows remain
root-only.

Every object-literal loadComponent/loadChildren pair (identifier, property
identifier or string key) keeps key, pair span and value span, and a form:

- import_default: non-async/non-generator arrow or function, with one expression
  or one return after parentheses, importing a string or substitution-free
  template literal without backslashes; keep raw specifier content.
- import_member: import(S).then(F), with one non-async/non-generator function,
  one identifier parameter and a return of parameter.Name (property identifier).
- sync: identifier/member-chain body as DeclarationExpr, with scope and relevant
  source shadows including callback parameters.
- inadmissible: reason not a function, async, generator, dynamic specifier,
  then destructuring, then arguments, string loadChildren, or other.

Additional arrays keep a name and DeclarationExpr with array span: default
export literal arrays named default (parentheses/as/satisfies transparent), and
module single-declarator const arrays wrapped in satisfies. Never duplicate a
declaration constant. Framework and mutation proof remain consumer work.

Version 2 adds:

- configs: module single-declarator const object literals (parentheses, `as`
  and `satisfies` transparent) with an own `providers` pair or shorthand key,
  as name plus DeclarationExpr: the ApplicationConfig shape.
- methods: for every object literal that owns a `path` or `matcher` pair key,
  each method or accessor entry whose name is an identifier, property
  identifier or string, as the object span, the entry index (DeclarationExpr
  `Entries` order) and the key. DeclarationExpr keeps such entries without a
  key; this lets the consumer tell a present-but-unevaluated key from an entry
  that may supply any key. Computed method names are not recorded.

The angular-router-syntax golden projects only router payloads (version 2:
its fixture has no application calls, configs or route-shaped methods, so those
lists are empty; `TestRouterApplicationSyntax` covers them). Independent
assertions cover filters, spans, shadows, loader forms/reasons, arrays,
invalidation, additivity, CRLF and byte-identical warm graph replay. Shared
backend round trips cover SQLite locally and Postgres in CI.
