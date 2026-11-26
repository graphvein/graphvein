# go-routes fixture

Go HTTP routes (task T023, ADR 0076): net/http ServeMux, chi v5 and gin
v1 registrations, objects and handlers, their composed paths,
serving positions, path-preserving wrappers,
oapi-codegen generated servers and the ServeMux ->
chi hop with forwarding closures and methods of a call result,
with the refusals of the claim contract's default branch D.

- `nethttp/mux.go`: ServeMux constructors, zero values (including
  `new(http.ServeMux)`), the DefaultServeMux, method, host, subtree and
  `{$}` patterns, handler forms, an invalid and a conflicting pattern, a
  dynamic pattern; compositions whose outer pattern does not match the
  child route (`/ptr/`, `/alloc/`: no path) and a wrapped mux.
- `nethttp/compose.go`: `StripPrefix` compositions (nested, constant
  prefix, method and host outer patterns, a dynamic prefix, a conflicting
  composition, a conditional one, a mux binding mixed with an unproven
  value), an `http.Server` root, a `StripPrefix` outside any mux
  registration (served under its prefix), a mux wrapped in
  a factory's return and one forwarded from a closure.
- `nethttp/serve.go`, `nethttp/serve_test.go` (row GR-C6):
  serving positions: `StripPrefix` inside `log.Fatal(http.ListenAndServe(...))`,
  nested prefixes through a binding and an `http.Server` literal with
  `http.TimeoutHandler`, a wrapper with `http.MaxBytesHandler`
  (assumption GR-V5), a mux served directly and through a wrapper (no
  assumption), a dynamic prefix, a prefix ending with `/`, a closure, a
  helper serving its parameter, and a production mux that only a test
  serves under a prefix.
- `chiapp/serve.go`, `ginapp/serve.go`: a chi router and a gin engine
  under `StripPrefix`, a gin engine through a wrapper in an
  `http.Server` literal (an identity wrapper, so no
  assumption), a chi router served through a wrapper that serves
  `r.WithContext` (GR-V5: chi keeps its routing state in the context).
- `nethttp/wrap.go`, `mw/mw.go` (rows GR-O7 and GR-C6):
  sub-muxes composed through proven wrappers (a package function with a
  terminating branch, a method that rebinds `r = r.WithContext(c)` and
  calls a reading method, an identity wrapper, `r.WithContext` around a
  ServeMux) and through refused ones (a path write, a request passed to a
  helper that writes the method); roots served through a proven wrapper
  (no assumption), a refused one and a proven one around a refused one
  (GR-V5 naming only the refused one); `srv.Handler = h` on an
  `http.Server` literal's variable and on a parameter, and on a struct
  that is no server; a mux that maintenance mode may replace with a
  plain handler (conditional) or with a closure forwarding to it (no
  path). `nethttp/mux.go`'s `logging` passes `r.URL` to `log.Println`, so
  it stays an unproven wrapper; `chiapp/compose.go` mounts a chi router
  through `mw.CORS` (composed) and through `cors`, which serves
  `r.WithContext` (no path).
- `chiapp/router.go`: chi verbs, `Handle` with a method prefix, `Method`,
  `Route`/`Group`/`With`, callbacks, helpers, factories, regexp
  parameters, `Mount` and the refused receivers; `Chain` registers on a
  method of a call result that builds its router (GR-O9) and on
  one that returns a field (`factory_method_chain`).
- `chiapp/compose.go`: `Route` and `Mount` (trailing slashes, a
  factory), a wrapped mount, mounts of an unproven value and of a
  ServeMux, a dynamic, an invalid and a regexp prefix, a chained `With`
  bound to a name, a conditional `Route`.
- `ginapp/engine.go`, `ginapp/groups.go`: gin verbs, groups (nested,
  trailing slashes, empty patterns, a dynamic prefix, a conditional
  binding), `Use`, `Any`, `Handle`, `Match`, the static forms, a helper
  bound from production, test and conditional calls, an unbound
  parameter; `ginapp/engine_test.go` a test file.
- `legacy/`: a nested Go 1.21 module (legacy_mux_patterns).
- `oldchi/`: chi before v5 and a dot import of gin.
- `oapi/` (row GR-G1): four servers written by hand in the
  exact shape of oapi-codegen v2.8.x's templates, headers included
  (`petchi` chi, `petgin` gin, `petstd` net/http, `basechi` chi; no
  generator was run). `oapi/app/app.go` instantiates them: `petchi`
  through `Handler`, `HandlerWithOptions` with a `BaseRouter` and
  `HandlerFromMux` under a chi mount (BaseURL `""`, handlers on `Pets`),
  `petgin` with BaseURL `/g`, `petstd` with a constant BaseURL, and
  `basechi` with two different BaseURLs (one a parameter) and an
  interface value as `si` (no path, `route_prefix_unresolved`, handlers
  unresolved); `oapi/basechi/chain.go` reaches its router through a
  method of a call result, as owncast does (GR-O9: the
  generated router is mounted under `/api/`; no path, since `basechi`
  has no one constant BaseURL).
- `muxchi/` (rows GR-C7 and GR-O8): owncast's shape (a chi
  root forwarded from a closure at a ServeMux's `/` beside a `/plugins/`
  subtree pattern, the closure answering `/debug/vars` and `/embed/chat`
  itself: those, `/plugins/{name}` and the redirected `/plugins` get no
  path), a chi router registered at `/` directly with a disjoint `POST`
  and an overlapping `GET` sibling, and the refused forms (a forwarded
  variable written twice, a closure serving one path only, a chi router
  at `/sub/`, a mux with a dynamic pattern).
- `source/`: a nested module that compiles with
  the real `net/http`, chi v5 and gin v1 (`go.mod` requires chi v5.2.4
  and gin v1.10.0; the source oracle of `scripts/oracle/go_routes`
  builds it at both version pairs). Package `cases` holds one function
  per case returning the router the oracle serves (`R11` serves through
  `cases.Listen`, which the oracle replaces); every handler writes its tag
  (`tg.T("Case/label")`, `tg.G(...)`, or a named handler's own name) in
  the `X-Route` header. `escape.go`: escaped routers (A1:
  `R1` a mount helper with an `http.Handler` parameter, `R2` a ServeMux
  mount helper, `R6` a `(router, error)` constructor, `R9` a struct
  field, `R10` a field returned by a method, `R11` a serving helper,
  `R12` a package variable; every route there has no path). `hop.go`:
  GR-C7 hops beside siblings the gate cannot see (A2: `H1` an interface
  helper, `H2` a struct field, `H7` a method value, `H8` a function
  value), the DefaultServeMux with `net/http/pprof` (B2: `H3`),
  forwarding closures deciding through helpers (B1: `H9`, `H10`) and by
  method (B4: `H4`), and the hops that keep their paths (`H5` a shadow,
  `H6` a disjoint sibling). `mux.go`: net/http compositions with a more
  specific sibling (B5: `P2`), a sibling subtree's redirect (`P18`), an
  escaping mux (`P13`), `{$}` and wildcards (`P6`), host and method
  patterns (`P12`) and named handlers (`P19`, rows GR-H1-GR-H3).
  `chi.go`: a static and a parameter parent route taking a mounted path
  (B5: `P3`, `P4`), trailing slashes, nesting, refused `Handle` and
  `StripPrefix` mounts, a helper and a factory. `gin.go`: groups, `Any`
  and `Static`. `wrap.go`: wrappers answering a path, a prefix, a
  trailing-slash redirect (B3: `PW1`, `PW5`, `PW2`), rewriting the path
  (`PW3`, `PW4`), answering a method (B4: `PW7`), a proven wrapper
  (`PW8`) and chi mounted through a path-answering wrapper (`PC1`).
  `confirm.go`: confirmation-pass forms, a
  router returned through a module interface method (`R13`), one
  appended to a slice (`R14`) and a closure answering one host (`H14`).
- `consts/`, `handlers/`: imported constants and handlers.

`nethttp/compose.go` keeps its dynamic-pattern composition on a mux of
its own (`droot`): a sibling of unknown pattern refuses the
other compositions of its mux (`fixed`'s `GET /f`).

No file is stored under an inert name.
