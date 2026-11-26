# ADR 0076 - Go HTTP routers: net/http ServeMux, chi and gin routes

**Status:** Accepted (delegated review, 2026-11-19)

**Date:** 2026-11-18 (draft from the T023 investigation, 2026-11-18)

**Progress:** implementation steps GR1 (payload, extractors-46) and GR2 (recognition, objects, registrations, handlers: route nodes without paths; contract change GR-K4 and `escaped_router`) and GR3 (composition: roots, chi mounts, gin groups, net/http `StripPrefix` compositions with the outer-pattern rule, conditional and scope, `route_unmounted`, the composition graph for the oracle, the net/http HEAD rule of the client matcher; contract wording of GR-C1, GR-C3 and GR-C4 refusals made explicit; Go payload version 4) and GR4 (oracle: the in-suite net/http oracle and the opt-in chi/gin harness, zero disagreements on the fixture and the five corpora; serving positions, contract row GR-C6, and assumption GR-V5 added on the decision on GR3's open points; Go payload version 5) and GR5 (developer decisions GO1 and GO4 of 2026-11-18: path-preserving wrappers, contract row GR-O7, proven from the wrapper's and its request helpers' syntax; `srv.Handler = h` assignments as serving positions; Go payload version 6; the precision row is blocked on the independent review) and GR6 (developer decision GO2 of 2026-11-18: oapi-codegen generated chi, gin and net/http servers, contract row GR-G1, accepted on the header and a token-for-token template check; Go payload version 7) and GR7 (decision of 2026-11-18: forwarding closures, methods of a call result and the ServeMux -> chi hop, contract rows GR-O8, GR-O9 and GR-C7; Go payload version 8) and GR8 (escaped routers, opaque gates, deciding wrappers and closures, sibling checks of GR-C1 and GR-C3, the source oracle, the matcher's host rule; Go payload version 9) are implemented (`docs/contract/claims/go-http-routers.md`, the T023 task record (internal)).

Task: `docs/contract/claims/go-http-routers.md`; inventory and census:
the T023 task record (internal), `docs/contract/claims/data/go-http-routers-census.jsonl`. Extends
ADR 0013 (route nodes and path composition), ADR 0016 (Axum: roots,
ordered bindings, factory returns), ADR 0019 (helper parameters, the
`trailing_slash` flag) and ADR 0068 (the Go package reader) without
editing them.

## Context

The project plan's launch 1 (`ROADMAP.md`) lists Go HTTP routers among the coverage additions:
readers stopped installing where their own stack had gaps. Go is a
Precise structural language (PL-L1), but Graphvein emits no route for Go
code: on the five Go corpora (realworld-gin, gotify, dozzle, miniflux,
owncast) main produces 0 route nodes for 617 production route
registration sites (638 with test files),
and owncast's 60 literal `/api/...` client calls stay unmatched.

Go routing has three dominant shapes, all covered by the corpora: the
standard library's `ServeMux` with Go 1.22 method and wildcard patterns
(miniflux), chi v5 with nested `Route`/`Group`/`With`/`Mount`
(dozzle, owncast) and gin v1 with nested groups and helper functions
taking a `*gin.RouterGroup` (realworld-gin, gotify). Real projects also
wrap routers in middleware calls, mount them under configurable base
paths and generate them (oapi-codegen); these decide most of the
coverage.

## Options considered

- **Receiver-name inference** (`r.Get` is a route): rejected; miniflux's
  Go API client calls `c.request.Get(ctx, "/v1/me")`.
- **Type-check with go/types or golang.org/x/tools/go/packages:**
  rejected; it needs dependencies and the module cache (network, build
  configuration) and adds a dependency to the indexer. The syntactic
  rules of this ADR are what the corpora need, and the oracle checks
  their results.
- **Run or build corpus code to list routes:** rejected (never run
  repository code).
- **Cache syntax per file, resolve and compose at link time** (as GORM,
  ADR 0063, and Axum, ADR 0016): chosen.

## Decision

1. **Payload.** A cached Go route payload in `facts.GoFacts` for gated
   files (imports chi or gin, or uses `net/http`'s mux API): router-typed
   parameters and results, ordered bindings, calls with argument shapes,
   string constants, conditional context, spans. Syntax only; every
   decision is made at link time. Aggregate extractor version bump.
2. **Recognition** from resolved imports of `net/http`,
   `github.com/go-chi/chi/v5` and `github.com/gin-gonic/gin` and from
   proven objects (constructors, chi callbacks and `With`, gin `Group`
   and `Use`, router-typed parameters bound at resolved calls, factory
   returns, `ServeHTTP` method values), never from names.
3. **Route nodes** in the shared shape of ADR 0013 and routes.md:
   identity `route:<file>#<METHOD> <pattern>` (pattern without its
   net/http method prefix, `@2` for repeats), framework `net/http`, `chi`
   or `gin`, handler edges for module functions and for method values
   whose receiver type the syntax states.
4. **Composition** per framework, matching the real routers (oracle):
   chi `TrimSuffix(prefix, "/") + child`, gin `joinPaths` with the
   engine base `/`, net/http `StripPrefix` prefixes with the outer
   pattern required to match the child route. Roots are proven
   constructors no resolved composition consumes (ADR 0016). A router
   reaching a mount only through another call or a closure gets no path
   (`route_mount_unresolved`), pending open point O1.
   Serving positions (row GR-C6): where a net/http server
   serves a root (`ListenAndServe`, `Serve`, the `Handler` of an
   `http.Server` literal), the root's paths start after the
   `http.StripPrefix` prefixes on the way (Go 1.27.1 semantics), and a
   wrapper call on the way is assumption GR-V5 ("assumed not to change
   the request path"), shown on every path entry it applies to; a root
   with serving positions is served only through them. An `http.Server`
   whose `Handler` is set by an assignment (`srv.Handler = h`, `srv`
   stated a server) serves `h` the same way.
   **Path-preserving wrappers** (row GR-O7, developer decision
   GO1): a call of a module function or method that takes an
   `http.Handler` and returns it, or returns an `http.HandlerFunc`
   literal that serves the request it receives (`h.ServeHTTP(w, r)` or
   `h.ServeHTTP(w, r.WithContext(c))`) after only reading it or passing
   it to module helpers that only read it (recursively), holds its
   router argument as `http.Handler(x)` does: compositions and serving
   positions read through it, without assumption GR-V5. The proof is Go
   1.27.1 semantics (ServeMux dispatches on the request's RequestURI,
   Proto, Method, Host and URL; `WithContext` shares the URL) over the
   syntax that may run before the serving call; a branch that ends in a
   terminating statement never reaches it. The capture records the local
   part (rows RW-wrapper and RQ-request of `docs/lang/go-routes.md`), the
   link pass resolves the calls. A wrapper serving `r.WithContext` passes
   no chi router (chi keeps its routing state in the context). Every
   other wrapper keeps decision 4's default: no path at a composition
   position, GR-V5 at a serving position.
5. **Templates:** gin and chi parameters to `{name}`/`{*name}`, chi
   regexps stripped (FastAPI precedent), net/http `{name...}` to
   `{*name}`, subtree patterns to a final `{*}`, `{$}` and trailing
   slashes to `trailing_slash: true` (ADR 0019's flag, for all three
   frameworks).
6. **Claim contract** of 27 rows plus the single default branch D,
   counted per file in `go_routes_unread`; contract test keeps code, Go
   table and docs 1:1.
7. **Oracle:** Go's own `ServeMux` in the normal test suite (pattern
   validity, conflicts, dispatch of a sample request per claimed path
   through the rebuilt tree); chi and gin at the corpora's pinned
   versions in an opt-in harness with its own `go.mod` under
   `scripts/oracle/go_routes` (listing via `chi.Walk` and
   `Engine.Routes()`, dispatch). The harness rebuilds the registration
   tree from the linker's composition graph, never from corpus code.
8. **Client matching:** the existing HEAD-matches-GET rule extends to
   `net/http` routes only (chi and gin answer 405 and 404).
9. **Generated servers** (row GR-G1, developer decision GO2).
   oapi-codegen's chi, gin and net/http servers register every route
   through a field read (`r := options.BaseRouter`) and a concatenated
   pattern (`options.BaseURL+"/path"`), which the general rules refuse.
   A file is read as a generated server only when its header names
   oapi-codegen v2.8.0, v2.8.1 or v2.8.2 (whose server templates were
   read from the Go module proxy, Apache License 2.0) and go/scanner
   finds its server declarations equal to the templates token for token,
   including each registered wrapper method's single call of the
   ServerInterface method of the same name (the capture's
   generated-server record). The link pass then reads the template's
   semantics from the calls of the entry functions (instances): the
   router is the instance's (a new router, or its `BaseRouter` name); the
   BaseURL must be one constant across instances, else the routes keep
   their nodes without a path (`route_prefix_unresolved`); the handler is
   the ServerInterface method, linked to the implementing struct type's
   method only where every instance states that type. Options considered:
   a shape check from the cached router payload alone (rejected: it
   cannot see the wrapper methods' delegation or the exact statements of
   the template), trusting the header alone (rejected: an edited file
   keeps its header), and linking `ServerInterface` methods to the
   OpenAPI `api_operation` nodes instead of routes (O2 (c), left to the
   OpenAPI work). Consequence: a router returned by a method of a call
   result (`handlers.New(h).Handler()`) is now also no root
   (`wrapped_router`, GR-C4), since the generated router it returns would
   otherwise be claimed at the root. Step GR7 (decision 10) reads that
   method's returns instead (GR-O9), so the router is mounted where the
   call appears.
10. **Forwarding closures and the ServeMux -> chi hop** (rows GR-O8,
   GR-O9 and GR-C7). A chi router registered at the pattern
   `/` of a root ServeMux keeps its paths: Go 1.27.1's
   `ServeMux.ServeHTTP` passes the request on, setting only `r.Pattern`
   and the wildcard matches, and chi routes on the URL path. Because `/`
   is the mux's least specific pattern, a path is claimed only when no
   other pattern of the mux, no `/tree` -> `/tree/` redirect of a subtree
   pattern and no shadow of a forwarding closure may match any of its
   requests (net/http's own pattern relations, the chi path read as a
   ServeMux pattern); a mux with a pattern no rule reads claims none; the
   route states GR-V2. A function literal at that `/` is read as the
   router it serves only under the forwarding proof (captured as syntax:
   one name served `X.ServeHTTP(w, r)` with the same request, outside any
   branch chosen by `r.URL`, the request only read or passed to proven
   helpers before, `r.URL` read only as `r.URL.Path` against string
   literals, which become shadows) and when `X` is written exactly once,
   since the closure reads the variable when a request arrives. `New(x).M()`
   with `New` declaring one module result type `T` is read through `T.M`'s
   returns (GR-O5). Options considered: no path for closures (the
   default of O1, (b); rejected for owncast's
   forwarding shape), treating any closure that mentions a router as a
   passthrough (rejected: a closure may serve only some paths or rewrite
   the request), and claiming the hop without sibling checks (rejected:
   the oracle shows sibling subtree patterns and the closure's own cases
   taking paths). Both oracles rebuild the hop.

11. **Escaped routers and deciding wrappers** (the independent
   review's A1-B5, the T023 task record (internal)). A constructor object is
   a root only when every use of every value that holds it is a use a
   row reads (registration and child receivers, a router's own methods,
   registration and composition handlers, serving positions, local
   bindings, GR-O4 arguments, GR-O7 wrappers and the net/http handler
   functions around such a use, returns GR-O5 and GR-O9 read, an
   `http.Server` literal's `Handler`, GR-G1 entry calls); any other use
   (an `http.Handler`, interface or `any` parameter, a field, element or
   package-level store, a channel send, a multi-result or field return,
   a method value or gin's `Handler()`, a handler closure in such a
   position) makes it escaped: no root, `escaped_router`. The payload
   records the escape positions of the syntax it does not keep (rows
   RU-escape and RU-context), so every use in a captured record is
   visible. A ServeMux that escapes, and the DefaultServeMux (packages
   register on it in `init`), make the GR-C7 gate opaque. A wrapper or
   forwarding closure that decides by the path (a branch that ends or
   serves chosen by `r.URL`, or by a helper that reads the URL or the
   method) is not path-preserving; one that answers a method itself
   (`if r.Method == M { ...; return }`) refuses the inner routes of M
   and ANY. GR-C3 hops refuse a composed path a more
   specific sibling on the outer mux takes entirely (or its subtree
   redirect, or a sibling of unknown pattern outside the other branch of
   an `if`); chi mounts refuse one a parent-tree route of the same method
   covers. An `http.Handler` parameter the callee only serves at its own
   paths (miniflux's `newHTTPServer`) and `httptest.NewServer` are serving
   uses. Options considered: tracking escapes only for the
   review's forms (rejected: the class is "a use no row reads"), keeping
   escaped routers as roots with an assumption (rejected: it states the
   unprefixed path as the router's), and modelling a wrapper's answered
   literals as shadows (deferred: the path a wrapper sees depends on its
   place among `StripPrefix` and mounts; refusing the wrapper is the
   precise default). A source oracle compiles the fixture's own module
   with the real routers and dispatches every claim.

No contract version change, no store schema change, no new dependency in
Graphvein's `go.mod`.

## Consequences

Go backends gain route nodes and handler edges; frontends in the same or
another repository link to them through the existing matcher. On the
default rules realworld-gin and gotify are almost fully composed (78 of
79 production routes with a path), while dozzle, miniflux and owncast
keep most routes without a path behind configurable prefixes and
middleware wrappers, each named by a diagnostic; owncast's generated
server, behind a forwarding closure, has its paths since step GR7 (272 of
273 routes). Every path
claimed is one the real router dispatches (oracle).

## Known gaps

- Wrappers the proof does not cover (a request passed to a method of
  another type, to a standard-library function or to a closure, a
  rebinding other than `r = r.WithContext(c)`, a wrapper factory
  returning `func(http.Handler) http.Handler`), closures other than GR-O8's
  forwarding closures and configurable base prefixes leave paths empty (open points
  O1 and O3 of the task); such wrappers at a serving position keep the
  paths under assumption GR-V5.
- Generated servers (GR-G1): other oapi-codegen versions, the
  strict-server wrapper (`si` is an interface value: handlers
  unresolved), options passed as a variable, echo, fiber, gorilla and
  iris outputs and other generators are not read.
- The ServeMux -> chi hop (GR-C7) is read only at a root mux's `/`
  pattern without `StripPrefix`; a chi router under another pattern, a
  gin engine under a mux, and a closure forwarding to a ServeMux are not
  composed. A path a sibling pattern overlaps only partly is refused
  whole. A forwarding closure's helper passes may still answer some
  requests themselves (as for GR-O7 wrappers, a terminating branch chosen
  by a request helper refuses the proof since step GR8); a write of the
  forwarded variable through a pointer or inside another closure is not
  seen (the known gap of local bindings).
- `New(x).M()` returning a field set in `New` is not read
  (`factory_method_chain`).
- Group-level middleware (`Use`, `Group(p, mw...)`) is not listed;
  `middleware` holds registration-site middleware only.
- Route precedence between overlapping patterns is the matcher's
  specificity score, not each router's own precedence.
- Routers in struct fields or package variables are escaped (no root,
  no path), even where the field is written once from a proven
  router and only served; methods called on call results other than
  GR-O9's and other libraries (gorilla/mux, echo, fiber, httprouter,
  routegroup) are not read.
- Escapes are read in captured records of gated files and, since unit
  GR11, through the result uses every Go file records for the calls
  outside them (row RN-results);
  a name declared inside a function literal the payload does not capture
  is not followed; a value an external function retains while its result
  is bound locally is read as a wrapper's (the uses of the local decide).
- A wrapper's answered path literals are not modelled as shadows: a
  wrapper that decides by the path passes nothing.
- A `GODEBUG=httpmuxgo121=1` set at run time cannot be seen (GR-V2).

## Review notes

- **Steps:** the investigation inventory, census and oracle baseline; GR1 payload;
  GR2 recognition, registrations and handlers; GR3 composition; GR4
  oracle and serving positions; GR5 wrappers; GR6 generated servers; GR7
  forwarding closures and the ServeMux -> chi hop; GR8 and GR9 the
  review's findings (the T023 task record (internal)).
- **Independent review (GR-R):** class A findings all fixed: escaped
  routers (A1) and gates that miss siblings through an escaping mux (A2); the confirmation pass's A1-r1 (a router returned by a
  method an interface may call) and A1-r2 (a router appended to a slice), with B1-B6 (helper guards, the DefaultServeMux, deciding
  wrappers, answered methods, sibling coverage, host-answering closures).
  Review: the T023 task record (internal) (GR-R and its confirmation pass of
  GR8); its probe forms are the fixture's `source/` module.
- **Results:** Corpus: realworld-gin 27 of 27 production routes with a
  path, gotify 55 of 56, miniflux 74 of 174, owncast 272 of 273, dozzle 0
  of 85 (behind a configurable base prefix); the review checked 85 corpus
  paths by hand (and 12 again in the confirmation pass), all correct.
  Oracles 0 disagreements: the in-suite net/http oracle on the fixture
  and the five corpora, the opt-in chi/gin graph oracle at chi v5.2.4 and
  v5.5.0 with gin v1.10.0 and v1.12.0, and the opt-in source oracle that
  compiles the fixture's own module with the real routers.
- **Precision row PL-F10** (developer decision GO4; scope narrowed in
  step GR10 to what the oracles compare): route existence, `method`,
  `pattern`, `host` and the path entries' `template`, `trailing_slash`
  and `resolution`, with the GR-V assumptions shown, are Precise. Handler
  identity (`handler`, `exposes_route`), `middleware`, `conditional` and
  `scope` are outside it: the label names them `unverified`, the views
  mark them, and change items of them are Best effort (`not_compared`).
  Client-call links stay best effort.
- **GR9 re-check (GR-R):** A1-r3 (a factory used as a value) fixed in
  step GR10 by the general rule: any use of a factory's name other than
  a call the linker reads makes its routers escape (payload row
  RN-names); the source oracle's V1-V9 variants check the family.
- **Trailing slashes (TOP-1 of 2026-11-18):** gin and chi serve a path with
  and without a trailing slash as two routes (gin's default
  `RedirectTrailingSlash` sends the other form a redirect, chi and
  net/http a 404 or another route), so the client matcher's slash-twin
  rule now covers net/http, chi and gin as it covered FastAPI:
  equal-template ties of one framework with mixed `trailing_slash` flags
  keep the route of the call's form (`docs/extract/clients.md`). A chi
  `/` route under a Route or Mount prefix written without a trailing
  slash also gets the bare prefix's entry (chi's Mount registers the bare
  prefix as a stub; row GR-C1). All three oracles now also send each
  claimed path's other slash form and require that it does not reach the
  route unless the route claims that form too. react + realworld-gin as a
  project: see the T023 task record (internal), "TOP-1 of 2026-11-18".
- **Other slash form (TOP-2 of 2026-11-18):** the tie rule above still let a
  sole candidate of the other slash form link (a call to `/api/articles`
  reaching a gin or chi route registered only as `/api/articles/`), which
  the real routers answer with a redirect (gin, a `net/http` subtree root)
  or a 404 (chi, `net/http`). The client matcher now skips a `net/http`,
  `chi` or `gin` path entry whose `trailing_slash` differs from the call's
  (a catch-all or the root excepted): the call is unmatched unless the
  route claims that form too (GR-C1's bare-prefix entry). A redirect is
  never a link: the client has to follow it, and a 301 may change the
  method. The oracles of TOP-1 already require that the unclaimed form
  does not reach the route (net/http 51, chi 57, gin 28, source 38 forms
  on the fixture; 0 disagreements); `TestSlashTwins` holds one case per
  framework. react + realworld-gin and react + express as projects keep
  their 46 links each (27 calls + 19 operations), and the five Go corpora's
  indexes are unchanged (the T023 task record (internal)).

## Acceptance notes (2026-11-19)

- Results: combined gate CI run 44533180619 on 0ef4343 (`[ci windows]`; T023 + T022 + T026 + T024 stacked): postgres raw log 0 FAIL, 226 `/postgres` PASS, skips only opt-in harness exports and environment-gated live cases; Windows 0 FAIL. Full check on the stacked head 4a7c645: 0 FAIL, 225 `/postgres` PASS. Timing gate (the T023 task record (internal)): all corpora within limits (owncast fresh +3.1%, self +2.4%; store +0.5..+10.3%).
- Revisions: GR1 payload; GR2 recognition, registrations, handlers; GR3 composition; GR4 oracles and serving positions; GR5 path-preserving wrappers; GR6 oapi-codegen servers; GR7 forwarding closures and the ServeMux -> chi hop; GR8-GR11 fixes for the independent review (class A escaped routers, opaque sibling gates, decision guards, sibling coverage, factory value and result uses) and its two re-checks; stack follow-ups TOP-1/TOP-2 (trailing-slash forms per router: a redirect or 404 is never a link; chi bare prefix).
- Coverage of production routes with a path: realworld-gin 27/27, gotify 55/56, miniflux 74/174 (UI routes behind an unproven middleware chain), owncast 272/273 (241 generated routes; 241 `documents_route` links), dozzle 0/85 (configured base prefix). 100+ corpus paths checked by hand by the reviewer: all correct.
- Verification labels: PL-F10 "Go HTTP routes" Precise covers exactly what the oracles compare (route existence, method, pattern, host, path template, trailing slash, resolution, GR-V assumptions shown); handler identity / `exposes_route`, middleware, `conditional` and `scope` are served `unverified`. Oracles: in-suite net/http (Go 1.27.1), opt-in chi v5.2.4/v5.5.0 and gin v1.10.0/v1.12.0 graph harness, and a source oracle compiling the fixture module with the real routers (37/37 claimed paths; refusals confirmed or conservative).
- Assumptions: GR-V1 verified router versions; GR-V2 Go 1.22+ mux semantics (a run-time GODEBUG is not visible); GR-V3 chi callbacks run once; GR-V4 code as written; GR-V5 unproven wrappers at serving positions keep the path (shown).
- Known gaps: configured base prefixes (dozzle; the launch-1 unknown-prefix task, GO3); wrappers that answer literal paths are refused rather than shadow-modelled; routers held in struct fields escape; other router libraries and oapi-codegen versions are not read; owncast's minified `_next` chunks can hit the parse deadline under load (pre-existing).
- After v1: shadow modelling for path-answering wrappers; a field-held router rule; Precise labels for Express, FastAPI and Axum routes.
