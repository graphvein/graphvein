# ADR 0026 - Static class member calls

**Status:** Accepted (developer review, 2026-10-19)
**Date:** 2026-10-19

## Context

The FastAPI template has 18 application/test calls to generated SDK static
methods without upstream calls edges. ADR 0014 already proves their class
bindings, but namespace traversal alone cannot prove a constructor property.
T012f Revision 1 requires additive resolution: existing calls, including the
TypeScript fixture's this.label() edge, retain their current behavior.

## Options considered

- Name-only matching cannot distinguish instance methods, accessors, fields,
  inherited members or replaced constructor properties.
- Runtime TypeScript type checking adds a compiler dependency and still does
  not establish the absence of runtime mutations. The offline compiler is a
  verification oracle only.
- Own static declarations with mutation guards reuse the shared inventory
  resolver and admit a source-proven, bounded subset. Chosen.

## Decision

Amend ADR 0014 with an additive StaticCall path for unresolved call expressions
whose callee is C.m or ns.C.m. Resolve the lexical class binding through the
existing import/export/namespace rules. Require exactly one emitted class
declaration and an own ordinary static method implementation. Async/generator
methods qualify; overload signatures keep their separate identities and calls
target the implementation. Direct anonymous default class declarations qualify.

Cache class declarations and their own eligible methods in detached RouteExpr
records attached to lexical bindings. Their identities refer only to declarations
in the same file. Block ambient/classes with runtime declaration merges and
repeated static/instance method identities. Class expressions, private names,
fields/accessors and inherited members supply no new targets. Unknown computed
static keys block every method they might replace. String-named ordinary methods
use their decoded property key and existing emitted identity. Type-only imports
cannot establish a runtime class binding. Optional member/call syntax is
transparent, as in existing calls.

Rebuild cross-file member mutation guards on every link pass. Assignment,
deletion and update syntax blocks the named property; unknown element mutations
block all properties. Object.defineProperty blocks its literal property (all
properties for an unknown key); Object.defineProperties and Object.assign block
all properties on the resolved class target. These are conservative guards even
when a call to Object is shadowed. They follow the shared resolver's bindings,
including import aliases and namespace members, without adding assignment-alias
inference. Direct this-property writes and Object mutations in static bodies
also block the affected constructor properties conservatively. Changes to
mutation files withdraw proof from unchanged cached callers. Catch parameters,
loop bindings and function-hoisted var declarations shadow class imports.

Keep the existing resolver path first. Add static calls only after it fails;
never remove or retarget an existing edge. Module-evaluation calls use their
nearest emitted owner, otherwise the file, and do not introduce new unresolved
diagnostics. Existing counted sites lose their unresolved count/site when proven.
Client wrappers consume the same calls edges and keep their existing three-level
forwarding rules. Member value references, including route-handler arguments,
keep their existing handling; this task adds call-expression resolution only.

Bump aggregate extraction to extractors-16 because class/body/modifier/merge
proof and type-only import syntax must survive cache replay. Contract v1,
public node attributes, identities, dependencies and persistence are unchanged.

## Consequences

SDK callers become reachable from linked routes without inventing application
client_call nodes. Eligible static URL forwarding wrappers use existing client
extraction and matching. Every language's old syntax cache is invalidated once.
Mutation proof is rebuilt from scanned syntax, including unexecuted branches;
unknown writes may therefore reduce coverage conservatively.

## Known gaps

Inherited statics, this/super and instance inference, element-access calls,
method value references and Python classmethod/staticmethod calls receive no new
resolution. Existing best-effort edges in these categories are preserved under
Revision 1. Arbitrary runtime aliases, reflection, external/unscanned mutations,
decorator effects and constructor reassignment are not general dataflow analysis.
Computed method declarations remain outside this subset. Postgres and other
operating systems require CI; storage is unchanged.

## Review notes (developer, 2026-10-19)

- Accepted. 37 new calls edges: 18 FastAPI template app calls to hey-api SDK
  methods, 1 Langflow, 2 Graphvein, 16 in the static-calls fixture. All agree
  with the offline TypeScript 5.3.3 type checker. Route callers in the FastAPI
  template now reach application code (impact on GET /api/v1/users/me and
  GET /api/v1/items/ verified). No client or route link changes in any corpus;
  the fixture's static URL wrapper links through the new edge at 0.9.
- Revision 1: the task listed this/super/instance calls among forms not to
  resolve; read literally that removed an existing correct this.label() edge.
  The task now states the change is additive only. The pre-implementation audit
  found zero existing class-binding edges that R2 would reject.
- The fixture's rejection cases sit at file scope because the existing resolver
  runs first inside functions and performs some method-name matching. What that
  path does with C.m() forms inside functions is not fixture-covered; an
  inventory of it is in the backlog before M5.
- Timing against main (two pairs): fresh +1.63%, warm +1.38%.
- Gaps: inherited statics, this/super, element access, method value references
  (for example queryFn: UsersService.readUsers), Python classmethod and
  staticmethod calls, and runtime aliasing or reflection.
