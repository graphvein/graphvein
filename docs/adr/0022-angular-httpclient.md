# ADR 0022 — Angular HttpClient and equivalent Express registrations

**Status:** Accepted (developer review, 2026-10-17)
**Date:** 2026-10-12

## Context

T012b Revision 2 authorizes Angular calls and injectable wrappers, interceptor
bases, dynamic authority rejection and equivalent Express registrations. The
corpus has one URL-prefix interceptor, three eligible wrappers and duplicate
favorites routes sharing their handler chain.

## Options considered

- Infer receiver names or assume an API base: cannot establish source facts.
- Execute configuration/replacements: exceeds deterministic offline evaluation.
- Merge duplicate routes: loses source registrations and stable identities.
- Cache syntax and prove tokens and equivalence against current inventory: chosen.

## Decision

Cache DI tokens, decorators, provider containers and bounded request-forwarding
syntax alongside TypeScript clients. Resolve external Angular exports through
ADR 0014's resolver; scanned packages shadow them. Derive eligible wrapper targets
from DI without adding general-purpose calls edges; production provider overrides
block proof. Preserve existing wrapper shape and three-level depth.

Read scanned angular.json each link pass. The deepest sourceRoot/root owns
registrations and calls. One unconditional, running URL-touching interceptor
supplies a prefix evaluated with ADR 0020's unchanged evaluator. Header/error
interceptors do not contribute bases. Multiple, conditional or unowned touching
registrations and unevaluable prefix expressions leave bases unresolved and
suffix-eligible only when all applicable URL-touching registrations have the
proven prefix shape. Any opaque URL rewrite makes every affected alternative
dynamic with null template/origin, unresolved base and no edge, even with explicit
base configuration (Revision 3, consistent with ADR 0018). Unowned calls consider
every production URL-touching registration in the repository. Class interceptors require
HttpClientModule or withInterceptorsFromDi in their provider container. Prefixes
concatenate, so prefixed absolute URLs are dynamic. Follow static environment
imports; deployment replacements remain inventory evidence.

Reject dynamic authorities for fetch, axios and Angular with null template/origin
and unresolved base. Preserve other path evaluation and leading-expression rules.

Retain every Express source route and warn on later duplicate method/raw-pattern
registrations on one proven app/router in one file. After all existing filters,
select the first tied registration only when routing object, method, raw pattern,
non-inline handler, middleware and matching path template/scope agree. Supply
router proof internally, without new public route attributes. Other frameworks
and non-equivalent registrations remain ambiguous.

Invalidate syntax caches with extractors-14. Contract v1, identities, persistence,
dependencies and confidence tiers remain unchanged. Calls represent source sites
even if their Observables are never subscribed.

## Consequences

Configuration/import/provider edits relink unchanged calls. Unresolved deployment
bases can use clients.base_paths. Distinct named handlers retain the twelve-way
tie fixture's candidate-cap purpose without changing Express equivalence.

## Known gaps

No constructor-body field assignments, inherited DI, arbitrary interceptor
execution or general injector hierarchy evaluation. Production provider syntax
represents registration evidence; application code is never executed to prove
bootstrap reachability. Existing URL-object, axios-interceptor and route matching
gaps remain. Other platforms/live Postgres require CI; storage is unchanged.

Registration semantics were checked against the official
[interceptor guide](https://angular.dev/guide/http/interceptors) and
[HttpClientModule](https://angular.dev/api/common/http/HttpClientModule).

## Review notes (developer, 2026-10-17)

- Accepted. Angular + Express composite: 24 Angular client nodes, of which
  21 link (15 direct sites, 6 DI wrapper invocations) and 3 are inner
  wrapper sites; 0 ambiguous. Every terminal link is exact at 0.7 (the
  interceptor base https://api.realworld.show/api has a literal origin).
  All 19 operations agree with realworld-api.txt and source. Zero false
  links in the composite and the Langflow sample.
- The Express corpus registers favorite/unfavorite twice with identical
  handlers and middleware; the first registration links, as Express
  dispatches, and two route_duplicate_registration warnings are emitted.
  The clients fixture's /api/tie registrations received distinct handlers
  to keep candidate-cap coverage.
- Dynamic authorities: 22 already-dynamic alternatives (21 Langflow, 1
  Graphvein) lost partial origins and templates, including the Langflow
  documentation template /{}/api/v1/responses that could have matched by
  coincidence.
- Revision 3: opaque URL rewrites make affected calls dynamic; only
  proven-prefix interceptors leave bases unresolved and suffix-eligible.
  Corpus results unchanged.
- The investigation stopped twice on task errors: the allowed-change lists missed the
  corpus's duplicate registrations, then Graphvein's own clients fixture.
  Both records are kept.
- Timing against main: fresh +8.04%, warm -0.37% (three pairs).
- Gaps: constructor-body assignments, inherited DI, the general injector
  hierarchy and bootstrap reachability are not modeled; fileReplacements
  are inventory only; FastAPI first-match duplicates are not modeled.

## Review notes (developer, 2026-10-25)

- Angular ignores parameter decorators not imported directly from the
  specifier '@angular/core' (isAngularCore uses specifier text) and falls back
  to the parameter type token. This ADR's resolver-based @Inject(HttpClient)
  proof also accepts re-exported Inject, which Angular would ignore. No
  corpus case exists. Backlog: align with the specifier rule. inject() is a
  runtime function, so resolver-based proof is correct there.
