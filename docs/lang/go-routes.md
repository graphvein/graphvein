# Go router syntax (net/http ServeMux, chi, gin)

ADR 0076; task T023 (step GR1 captures the syntax; units
GR2-GR6 recognise, compose and check it). Aggregate extractor version:
extractors-49 (49 from T024, ADR 0077, outside the Go payload; 48 from T026, ADR 0079, outside the Go payload; 47 from T022, ADR 0075, outside the Go payload; 46 from T023 GR1: cached Go facts gained the router
payload; Go payload version 9, `facts.GoModelsVersion`: version 4, from
T023 GR3, keeps the two router filter forms GR2 found; version 5, from
T023 GR4, gates and keeps the net/http serving syntax; version 6, from
T023 GR5, adds the wrappers and request helpers and keeps `x.Handler = v`
assignments; version 7, from T023 GR6, adds the generated-server record
and gates and keeps the calls of a generated server's entry functions;
version 8, from T023 GR7, adds the forwarding proof of a function literal
(`GoExpr.Forward`); version 9, from T023 GR8, adds the escape records
(`Escapes`, `GoRouteCall.Context`, `GoExpr.Request`, the `Uses` of
captured literals) and the deciding statements of wrappers and
forwarding closures (`Methods`, `Guards`) with the request reads of
request helpers (`URL`, `Method`); none bumps the aggregate version, since
extractors-46 is this unmerged branch's own). Contract version
unchanged; the payload adds no graph record.

The Go extractor captures the syntax that Go route extraction reads: in
files that can register routes (the route gate), the functions and
function literals that mention router syntax, with their signatures,
lexical blocks, the bindings of the names their calls use, the
router-named calls and the calls that may pass a router on, and their
returns; and, in every non-test file, each function's single named
result type. The payload is syntax, never a claim (rule 31): which object
is a router, which call registers a route, which path it serves and which
handler it names are decided at link time through the module's own
packages, under the claim contract of `docs/contract/claims/go-http-routers.md`.

## Why a cached payload

On a cache hit the indexer replays stored facts and never parses the
file, so the link pass has no tree. The Go facts cached before this
payload keep one node per declaration, the calls with their callee name
and the GORM payload of files that import GORM: no call arguments, no
receiver chains, no function literals, no lexical blocks and no bindings
outside GORM files.

## Gate and filters (rule 31)

Most Go files import `net/http`, and every gin handler file imports gin
for `*gin.Context`, so the payload is filtered at capture by the
cheapest syntactic tests that keep every site the claim contract reads:

1. **File gate** (task decision 10, rows RG-*): the file imports
   `github.com/go-chi/chi/v5` (or an older chi path, which GR-K1 refuses
   at link time, counted) or `github.com/gin-gonic/gin`, or it imports
   `net/http` and mentions `ServeMux`, `NewServeMux`, `DefaultServeMux`,
   `Handle` or `HandleFunc`, or (since step GR4, the serving syntax of
   contract rows GR-C3 and GR-C6) `StripPrefix`, `ListenAndServe`,
   `ListenAndServeTLS`, `Serve`, `ServeTLS` or `Server`, as a selector or
   qualified type of its `net/http` name, or (since step GR6, row
   RG-generated) it calls a generated server's entry function
   (`Handler`, `HandlerFromMux`, `HandlerFromMuxWithBaseURL`,
   `HandlerWithOptions`, `RegisterHandlers`, `RegisterHandlersWithOptions`)
   with an argument, as `F(...)` or `pkg.F(...)` with `pkg` neither a
   standard-library nor a router import. Test
   files are gated like any other file (test-scope routes). A dot import
   binds no name; the link pass counts it from the file's imports.
2. **Router functions** (rows RF-*): in a gated file, a function
   declaration or function literal is captured when its signature has a
   parameter or result of a router type (`X.ServeMux`, `X.Router`,
   `X.Mux`, `X.Engine`, `X.RouterGroup`, `X.IRouter`, `X.IRoutes`, a
   pointer to one, X a router import) or a result `X.Handler`, or its
   body holds a router call that is not excluded (row RC-excluded), a
   router import member `NewServeMux`, `DefaultServeMux`, `Handle`,
   `HandleFunc`, `NewRouter`, `NewMux`, `New` or `Default`, a router type,
   a `net/http` `StripPrefix`, `ListenAndServe`, `ListenAndServeTLS`,
   `Serve`, `ServeTLS` or `Server`, a generated server's entry
   call as in the gate, or a function literal that is captured
   itself. A literal's record
   follows its parent's.
3. **Router calls** (row RC-router): a call whose callee is a selector
   named `Handle`, `HandleFunc`, `Get`, `Post`, `Put`, `Patch`,
   `Delete`, `Head`, `Options`, `Connect`, `Trace`, `Method`,
   `MethodFunc`, `Route`, `Group`, `With`, `Mount`, `GET`, `POST`, `PUT`,
   `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, `Any`, `Match`, `Static`,
   `StaticFS`, `StaticFile` or `StaticFileFS`, whatever its receiver is
   (the receiver is proven at link time: miniflux's HTTP client calls
   `c.request.Get(...)` are captured and refused there), except row
   RC-excluded. A call in the receiver chain of a router call
   (`r.With(m)` of `r.With(m).Get(...)`, `Group(...).Use(...)`) is part of
   that call's tree and not listed on its own; a router call in another
   captured call's arguments is listed on its own too.
4. **Exclusion** (row RC-excluded): a router-named call whose receiver no
   contract row can prove a router: the receiver is the package name of a
   non-router import (`slog.Any(...)`: a package function); the receiver
   chain starts at a parameter declared with a qualified type that is no
   router type (`*gin.Context`, `*http.Request`, `*testing.T`), never
   rebound in the function; the receiver is a parameter of another type
   (`s *Server`), directly or through method calls, never rebound (a
   field of it is kept: a router in a struct field is
   `receiver_unproven`, counted); the receiver is a local whose every
   binding holds a value that cannot be a router (the router filter
   below: `query := location.Query()`, a composite literal). GR-O4 binds
   only parameters of the router types, and GR-K3 refuses fields,
   elements, type assertions and receives, so none of these is a proven
   router object.
5. **Plain calls** (row RC-call): a call of a name (not a predeclared
   function) or of `x.F` (x a name that is not a standard-library or
   router import) outside a captured call's arguments, with an argument
   that may hold a router by the router filter: the calls the Go linker's
   rules can resolve to a module function or method that may receive a
   router (GR-O4).
   **Serving calls** (row RC-serve): a call of a
   standard-library function (`x.F`, x a standard-library import,
   `net/http` included) outside a captured call's arguments, another
   serving call, a binding's value and a return (those are kept whole),
   that is `net/http`'s `ListenAndServe`, `ListenAndServeTLS`, `Serve` or
   `ServeTLS` or whose arguments hold a `net/http` `StripPrefix` or
   serving call (outside function literals):
   `http.ListenAndServe(addr, http.StripPrefix(p, mux))`,
   `log.Fatal(http.ListenAndServe(addr, cors(mux)))`. It is kept whatever
   its arguments are, and its arguments are walked as outside a captured
   call (a plain or router call inside is still listed on its own).
   **Generated-server calls** (row RC-generated): a call of a
   generated server's entry function (as in the gate) outside a captured
   call's arguments, a serving call, a binding's value and a return is
   kept whatever its arguments are (`api.HandlerWithOptions(s,
   api.ChiServerOptions{BaseURL: "/v1"})`); a binding whose value holds
   one is kept too (rule 7).
6. **Router filter** (rows RC-call, RC-excluded, RE-closure): per
   record, a value may hold a router when it is a name that may, a call
   of `f(...)` (not predeclared), of `pkg.F(...)` (not a standard-library
   package; a router package only for `NewServeMux`, `NewRouter`,
   `NewMux`, `New`, `Default`), of `x.M(...)` with x a parameter (not of a
   qualified non-router type), a free name, a local declared `var x T`
   or a local that may hold a router, a call of `Route`, `Group`, `With`
   or `Use` on a call that may (`r.With(a).With(b)`), `new(X.ServeMux)`
   (GR-K4), a conversion or `&` of such a value, or `X.DefaultServeMux`.
   A name may when it is a parameter of a
   router type, a name one of whose bindings may, or a name the record
   neither declares nor binds (a package-level or enclosing name). Every
   other value (literal, composite literal, field, index, slice, type
   assertion, receive, range or type-switch variable, function literal,
   a call through a field such as `w.Body.String()`) is no proven router
   object.
7. **Bindings** (rows RL-*): every declaration or `=` assignment in the
   record's body (outside nested literals) of a name that the record's
   kept calls and returns, its nested records or the kept values of other
   bindings use; every binding of such a name is kept, so shadowing and
   reassignment stay visible. A binding whose value holds a `net/http`
   `StripPrefix` or serving call or an `http.Server` composite literal (outside
   function literals) is kept too (step GR4: a serving position may read
   it), with the names its value uses. An assignment `x.Handler = v` (one
   target, `x` an identifier, one value) is kept always, as kind
   `handler` with `x` and the names `v` uses (row RL-handler:
   an `http.Server`'s handler set by an assignment is a serving position
   when `x` is a server); a function with a parameter of type
   `http.Server` or `*http.Server` is a router function.
8. **Package-level vars** (rows RV-*): var specs of the gated file whose
   value or type mentions router syntax, or whose names a captured record
   uses.

Signatures (rows RS-*) and `//go:debug` lines (row RD-debug) are not
gated: a handler's receiver type may come from a constructor in any file
(GR-H2), and the `httpmuxgo121` setting of a `main` package may sit in
any file (GR-V2).

## Wrappers and request helpers

Contract row GR-O7 passes a router through a call of a module wrapper
only where the wrapper provably leaves the request path unchanged. The
capture records the local part of that proof for every file that imports
`net/http` (test files included), outside the route gate (rows RW-* and
RQ-*); the link pass resolves the calls and decides the rest. The proof
follows Go 1.27.1: `ServeMux.ServeHTTP` and `findHandler` dispatch on the
request's `RequestURI`, `Proto`, `Method`, `Host` and `URL` (its
`EscapedPath`, `Host` and `RawQuery`); `r.WithContext(c)` returns a
shallow copy that shares the `URL` (`request.go`).

- **Request uses.** Inside a declaration, an identifier naming the
  request `r` is a read when it is: the operand of `r.F` with `F` a field
  of `http.Request` other than `URL` (`Method`, `Header`, `RemoteAddr`,
  ...), or one of the methods `Context`, `Cookie`, `Cookies`,
  `CookiesNamed`, `BasicAuth`, `UserAgent`, `Referer`, `FormValue`,
  `PostFormValue`, `FormFile`, `ParseForm`, `ParseMultipartForm`,
  `MultipartReader`, `PathValue` and `ProtoAtLeast` (called or as a
  method value; none writes the dispatch fields in Go 1.27.1); `r.URL.F`
  with `F` a field of `url.URL` or one of its methods `Query`, `String`,
  `EscapedPath`, `EscapedFragment`, `Hostname`, `Port`, `IsAbs`,
  `Redacted`, `RequestURI`; an operand of `==` or `!=`. A selector chain
  rooted at `r` that is an assignment target, incremented or decremented,
  or the operand of `&` is no read. `r = r.WithContext(c)` (one target,
  one value) is a rebinding: the name then holds a copy sharing the
  `URL`. `r` as a direct argument of a call is a **pass** when the
  callee is a name `f` (never bound, assigned or used as a value in the
  declaration, not predeclared), `pkg.F` (`pkg` an import name used only
  as a selector operand) or `recv.M` (`recv` the declaring method's
  receiver name, used only as a selector operand): the pass is recorded
  with the argument index. Every other use (a binding of `r`, `&r`,
  `*r`, `r.URL` as a value, `r.WithContext(c)` anywhere else, `r.Clone`,
  `r.SetPathValue`, a call through a field or a closure) is no proof.
- **Request helpers** (row RQ-request): a function or method (receiver
  `T`, `*T` or `T[...]`) with exactly one named `*http.Request`
  parameter whose every use in the whole body, function literals and
  deferred calls included, is a read, a rebinding or a pass. Recorded:
  `Name`, `Receiver`, `Param` (the parameter's index), `Passes`, and
 `URL` when the body reads `r.URL` or `r.RequestURI`,
  `Method` when it reads `r.Method`.
- **Wrappers** (row RW-wrapper): a function or method with exactly one
  named `http.Handler` parameter `h` and exactly one result of type
  `http.Handler`, the file's `net/http` name never bound locally, such
  that: every `return` of the body outside function literals returns `h`
  or `http.HandlerFunc(L)` with `L` a function literal
  `func(w, r *http.Request)`; `h` is used only in those returns and as
  the receiver of serving calls `h.ServeHTTP(w, R)`, expression
  statements directly in such an `L` (no function literal, `go` or
  `defer` between), with `R` the name `r` or `r.WithContext(c)`; every
  `L` holds at least one serving call and no `goto` or label; and every
  use of `r` in the region of each serving call is a read, a rebinding,
  a pass or the serving call's own `R`. Recorded: `Name`, `Receiver`,
  `Param`, `Context` (some `R` is `r.WithContext(c)` or `r` was rebound),
  `Passes` (of every region), `Methods` and `Guards` (below).
- **Deciding statements**: an `if` or
  expression `switch` of `L` (outside nested literals) with a branch or
  clause body that ends in a terminating statement or holds a serving
  call decides whether the request is served. Its conditions (the `if`'s
  initializer and condition, the switch's initializer, tag and case
  values) are read: a read of `r.URL` or `r.RequestURI` there means the
  wrapper answers some paths itself (a health check, a static prefix, a
  trailing-slash redirect) and refuses the proof (row RW-D); a call that
  passes `r` there is a **guard** (`Guards`, recorded as passes are); a
  read of `r.Method` is allowed only as `if r.Method == M { ...; return }`
  (either operand order, no initializer, no `else`, no serving call in
  the branch) or as a switch on `r.Method` (no initializer) whose clauses
  either end without serving or neither end nor serve and whose default
  clause does not end: each `M` (a string literal or `http.MethodX`) is a
  method the wrapper answers itself (`Methods`); any other deciding read
  of `r.Method` refuses the proof.
- **Region** of a serving call `S`: the syntax that may run before or
  during `S` in one run of `L`: the statements before `S` in every
  enclosing statement list, the initializer and condition of every
  enclosing `if`, the headers of every enclosing `switch` or `select` and
  of the clause holding `S` (the case lists of the other clauses, and
  the bodies of the clauses that fall through into it), every enclosing
  `for` statement whole, and `S`'s own arguments. Inside the region, an
  `if` branch or case body whose statement list ends in a terminating
  statement (a `return`, a call of the predeclared `panic`, a terminating
  block, an `if` with an `else` both of whose branches terminate) and
  holds no `break` or `continue` is left out (it never reaches `S`), at
  any depth outside function literals; a function literal in the region
  is read whole (its `return` ends only the literal). The body of a
  function literal that `L` itself defers runs after `S` and is left
  out; its arguments are read. Uses after `S` or in a branch that never
  reaches it do not change the path `S` dispatches (a data race on the
  request is outside Go's memory model and not considered).

At link time (contract row GR-O7) a call `f(x)`, `pkg.F(x)` or `v.M(x)`
of a recorded wrapper (resolved as GR-O5 resolves factories; `v` typed as
GR-H2 types a receiver; exactly one declaration of the name) passes the
router objects of its `http.Handler` argument through when every pass
resolves: `f` to the functions of the declaring package, `pkg.F` to a
module package's functions, `recv.M` to the methods of the receiver's
type, each target (every build variant of the name) a request helper
whose request parameter is the argument passed and whose own passes
resolve the same way (the greatest such set of helpers: a cycle of
helpers that only read keeps the path). A wrapper with `Context` passes
no chi router (chi reads its routing state from the context). Since unit
GR8 a wrapper whose `Guards` resolve to a request helper that reads the
URL or the method (`URL`, `Method`, directly or through its passes), or
to no helper, passes nothing (the helper may decide by the
path); its `Methods` travel with the routers it passes (on the
composition edge or the serving position) and refuse the paths of the
inner routes of those methods and of ANY routes (a CORS
preflight answered by the wrapper never reaches an inner `OPTIONS`
route).

## Forwarding closures

Contract row GR-O8 reads a function literal registered as a ServeMux's
`/` handler as the router it serves, where the literal provably serves
the request it receives unchanged. The capture records the local proof on
every function literal it reduces (rows RE-forward and RE-forward-D):
`Forward` (`Name`, `Passes`, `Shadows`) when the literal

- is `func(w, r *http.Request)` (two parameters, the second a named
  `*http.Request`), holds no `goto` or label, and the file's `net/http`
  name is never bound in it;
- holds at least one serving call `X.ServeHTTP(a, r)`, an expression
  statement directly in the literal (no function literal, `go` or `defer`
  between), all with the same name `X` and the request name `r` itself
  (never `r.WithContext(c)`, never a rebound `r`); `X` is neither a
  parameter of the literal nor an import name, and every use of `X` in
  the literal is such a call's receiver (the literal neither declares
  nor assigns it);
- keeps every use of `r` in the region of each serving call a read or a
  pass (the wrapper proof's request uses and region, above);
- serves outside every branch chosen by the request path: no serving
  call lies in a case clause of a switch whose tag reads `r.URL`, or in a
  branch of an `if` whose initializer or condition reads `r.URL` (the
  `default` clause of such a switch may serve: its case values are
  shadows);
- passes the deciding-statement rule of wrappers for `r.Method`
  (`Methods`) and records its `Guards`;
- reads `r.URL` anywhere in the literal only as `r.URL.Path`, either as
  the tag of an expression switch without initializer whose case values
  are all string literals, or as an operand of an `==` that is an `if`
  condition and whose other operand is a string literal; it never reads
  `r.RequestURI`. Those string literals are `Shadows`: paths the literal
  may answer itself (a superset: a case that also forwards is listed).

Otherwise the literal has no `Forward` (row RE-forward-D). The link pass
decides whether `X` holds a router, whether the passes keep the path,
whether a guard reads the URL or the method (then no proof),
which composed paths the shadows refuse and which methods are answered by
the closure (rows GR-O8 and GR-C7).

## Escapes

Contract row GR-C4 makes a router object a root only when every use of
every value that holds it is a use a row reads. The kept
syntax (bindings, calls, returns) shows the uses the payload keeps; the
capture adds the rest for every captured record (rows RU-escape and
RU-context), scanning the body, and the bodies of the function literals
it does not capture, outside the kept syntax:

- **Contexts.** Each expression is scanned with where its value goes: a
  statement or condition operand discards it; a `:=`, `var` or `=` to a
  name the scope declares binds it locally (`_` discards); a store to a
  field, element, dereference, package-level or enclosing name, a
  composite literal element, a channel send and the return of a function
  literal without a record make it escape.
- **Calls the payload does not keep.** An argument escapes into the call
  (row RU-escape, form `argument`, `Detail` the callee text), unless the
  call's own value is bound locally (it may wrap the argument; the uses
  of the local decide). A router-named call on a receiver no rule proves
  (row RC-excluded) always takes its arguments. The receiver of a method
  other than the router's own methods (registrations, children, `Use`,
  `NotFound`, `MethodNotAllowed`, `NoRoute`, `NoMethod`, `ServeHTTP`,
  `Routes`, `Middlewares`, `Find`, gin's `Run*`, `LoadHTML*`, ... but not
  gin's `Handler()`) and a router method value (`hf := m.HandleFunc`)
  escape too (form `method`).
- **Recorded values** are those that may hold a router (the router filter
  of RC-call: names, calls, conversions, `&`, `x.ServeHTTP`, router
  method values, composite literals holding one) and handler-shaped
  literals (`Request`: a `*http.Request` parameter). A kept call met in
  this syntax gets its `Context` (`statement` or `local`) instead, or is
  recorded as the escaping value.

The link pass (`gohttp_escape.go`) walks the kept bindings (by target),
the returns (a return GR-O5 or GR-O9 reads is a use; any other return
escapes), the kept calls with a `Context`, and the escape records. A
walked value goes into a call's arguments by the callee: a registration,
composition or child call on proven routers, a serving function and a
generated server's entry call read theirs; `StripPrefix`,
`TimeoutHandler`, `MaxBytesHandler`, `http.Handler` and
`http.HandlerFunc` and a proven wrapper carry their handler to the
call's destination; an argument bound to a router-typed parameter
(GR-O4) is read in the callee; an `http.Handler` argument of a module
function whose one result is an `http.Handler` goes with the call's value
when that is bound locally or read by a row; a router value passed to
an `http.Handler` parameter whose every use in the callee (and its
literals) serves it at its own paths (an `http.Server` literal's
`Handler`, a serving call's handler, `srv.Handler = h`, `h.ServeHTTP`),
never reassigned and named by no escape record, is served (miniflux's
`newHTTPServer`; the capture treats `http.Handler` parameters as values
that may hold a router, so their uses are kept or recorded); the handler
of `httptest.NewServer`, `NewTLSServer` and `NewUnstartedServer` named
directly is served; every other argument escapes. An escaping value's objects (directly, or inside the calls and
closures its bindings hold) are `escaped_router`, named in
`route_mount_unresolved`; a parameter's escape is that of every object
bound to it. A `ServeMux` that escapes makes a GR-C7 gate opaque.

## Generated servers

Contract row GR-G1 (developer decision GO2) reads the HTTP servers
oapi-codegen generates (`oapi-codegen -generate chi-server`, `gin-server`
or `std-http-server`). The capture (`internal/lang/golang/oapi.go`, row
RO-server) accepts a gated file only when:

- its header, before the package clause, holds the line `// Code
  generated by github.com/oapi-codegen/oapi-codegen/v2 version vX.Y.Z DO
  NOT EDIT.` (imports.tmpl) with version v2.8.0, v2.8.1 or v2.8.2;
- it imports `net/http` and either `github.com/go-chi/chi/v5` (chi),
  `github.com/gin-gonic/gin` (gin) or neither (net/http), without aliases;
- go/parser accepts it, and its server declarations, scanned by
  go/scanner (comments dropped, automatic and explicit semicolons alike),
  equal the templates' token for token: chi `Handler`, `ChiServerOptions`,
  `HandlerFromMux`, `HandlerFromMuxWithBaseURL` and `HandlerWithOptions`
  (whose body is the template's head, the `wrapper` binding and one
  `r.Group(func(r chi.Router) { r.M(options.BaseURL+"path",
  wrapper.N) })` per operation, `M` a chi method, then `return r`);
  net/http the same with `ServeMux`, `StdHTTPServerOptions` and one
  `m.HandleFunc(http.MethodX+" "+options.BaseURL+"path", wrapper.N)` (or
  a quoted method) per operation; gin `GinServerOptions`,
  `RegisterHandlers` and `RegisterHandlersWithOptions` with one
  `router.M(options.BaseURL+"path", wrapper.N)` per operation, `M` a gin
  method; and the `ServerInterfaceWrapper` struct;
- every registered `N` is a method `func (siw *ServerInterfaceWrapper)
  N(w http.ResponseWriter, r *http.Request)` (gin: `(c *gin.Context)`)
  whose body uses `siw.Handler` once, as the call `siw.Handler.N(w, r,
  ...)` (gin `siw.Handler.N(c, ...)`) with identifier arguments: the
  handler serves the request through the ServerInterface method `N`.

The templates are oapi-codegen v2.8.0's `pkg/codegen/templates`
(`chi/chi-handler.tmpl`, `chi/chi-middleware.tmpl`,
`gin/gin-register.tmpl`, `gin/gin-wrappers.tmpl`,
`stdhttp/std-http-handler.tmpl`, `stdhttp/std-http-middleware.tmpl`) and
the path helpers of `pkg/codegen/utils.go`, read from the Go module proxy
(github.com/oapi-codegen/oapi-codegen/v2, Apache License 2.0; nothing is
built or run, nothing added to Graphvein's `go.mod`). v2.8.1 and v2.8.2
have the same files byte for byte; v2.7.2 and earlier differ (the chi
middleware template and the path helpers), so their files are row RO-D.
The record says only that the shape matched; the link pass decides the
routers, BaseURL and handlers from the entry calls (GR-G1).

## Payload

`facts.GoFacts` (JSON `Go`), version 7, fields added by T023 GR1, GR5 and GR6 (the
GORM and structure fields are documented in `docs/lang/go-models.md` and
`docs/lang/go-structure.md`). Spans are contract v1 spans (1-based
lines, 0-based byte columns, end exclusive) of the scanner's text.

- `Debug`: the header's `//go:debug` lines (before the package clause)
  that mention `httpmuxgo121`, trimmed. Every Go file.
- `Wrappers` (every file importing `net/http`):
  `facts.GoWrapper` `Name`, `Receiver` (a method's receiver base type),
  `Param` (the `http.Handler` parameter's index), `Context`, `Passes`,
  `Line` (row RW-wrapper).
- `Requests` (every file importing `net/http`):
  `facts.GoRequestFunc` `Name`, `Receiver`, `Param` (the `*http.Request`
  parameter's index), `Passes`, `Line` (row RQ-request). A pass
  (`facts.GoRequestPass`) is `Callee` (a name `f` or a selector `x.F`),
  `Arg` (the argument index) and `Method` (`x` is the declaring method's
  receiver).
- `Signatures`: functions without receiver of a non-test file that
  declare exactly one result of type `T`, `*T`, `pkg.T` or `*pkg.T` (T
  not predeclared): `Name`, `Result` (reduced type), `Line`.
- `Routes` (gated files only): `Consts` (package-level string-shaped
  constants of a gated `_test.go` file, as `GoFacts.Consts` of non-test
  files), `Vars` (`Names`, `Type`, `Values`, `Line`), `Funcs`,
  `Generated` (row RO-server: `facts.GoGenerated` `Generator`
  `oapi-codegen`, `Version` from the header, `Framework` `chi`, `gin` or
  `net/http`; absent otherwise).
- `Funcs[i]` (`facts.GoRouteFunc`): `Name` (empty for a literal),
  `Receiver`, `Params`, `Results` (one per name, as the GORM payload's
  parameters), `Span` (the declaration's span equals its structure
  node's span; a literal's span), `Parent` (1 + index of the enclosing
  record of a literal; 0 for a declaration or a literal in a package
  var's value), `Block` (the parent's block holding the literal),
  `Blocks`, `Bindings`, `Calls`, `Returns`, `Jumps` (set when the body
  holds a `goto` or a labeled `break` or `continue`, row RJ-jump: such a
  jump may skip any statement of the body).
- `Blocks[j]` (`Parent`, `Kind`): `Blocks[0]` is the body (`body`);
  `block` (a bare `{ }`), `if` (an `if` statement's initializer and
  condition), `then`, `else` (an `else if` nests an `if` block in the
  `else` block), `for` (clause and body), `switch` (a switch or type
  switch's initializer and tag), `case` (a case or default clause of a
  switch, type switch or select), `go`, `defer`. `body`, `block`, `if` and
  `switch` run whenever their parent runs; the others are conditional
  context (GR-C5).
- `Bindings[k]` (`Kind`, `Names`, `Type`, `Values`, `Block`, `Span`):
  `var`, `define` (`:=`, also `x := <-c` in a select), `assign` (`=`,
  identifier targets by name, any other target `""`), `const`, `range`,
  `range_assign`, `typeswitch` (`switch x := e.(type)`; Values the
  asserted operand), `handler` (`x.Handler = v`: Names holds `x`, which
  it neither declares nor assigns). Names pair with Values by position when the counts
  agree, otherwise one Value is a multi-value call. A declared name's
  scope starts at the end of `Span`.
- `Calls[k]` (`Kind` `router` or `call`, `Call` the whole call's reduced
  tree with its receiver chain and arguments, `Block`, `Span`, `Hash`:
  the content hash of a router call's text, a route node's hash).
- `Returns[k]` (`Values`, empty for a bare return; `Block`; `Span`).
- Expressions are the GORM payload's reduced trees (rows E-* of
  `docs/lang/go-models.md`) with three router additions: a function
  literal is kind `funclit` with `Func` (1 + index of its captured
  record) or, when not captured, `Uses` (the sorted identifiers its body
  uses, its own parameters and names the router filter rules out
  excluded: the link pass reads them to find a router used in a closure,
  GR-C4); a function literal also carries `Forward` when it passes the
  forwarding proof ("Forwarding closures"); and every argument
  of a router-named call carries `Text`, its
  source text without white space (`handler_expression`, `middleware`),
  except a function literal.

Validity: `GoFacts.ModelsValid()` also checks the router payload (record
order, parent and block indexes, block trees, kinds, spans, router calls
with a hash and plain calls without, expression kinds and the `funclit`
fields); the indexer checks it on every cache read, so a payload of
another version or a damaged one is re-extracted. A capture failure (a
recovered panic) behaves as the GORM payload's: diagnostic
`model_syntax_capture_failed`, no payload, the next run tries again.

## Payload table

Every capture branch in `internal/lang/golang/routes.go` reports one row
id below; `TestGoRoutePayloadContract` checks that the Go table and this
table match 1:1, that a snippet per row fills exactly the row's fields,
and that the snippets together emit exactly the rows of the table.

| id | form | payload |
|---|---|---|
| RG-chi | a file importing `github.com/go-chi/chi/v5`, `github.com/go-chi/chi` or another `github.com/go-chi/chi/vN` | routes: Routes |
| RG-gin | a file importing `github.com/gin-gonic/gin` (and no chi path) | routes: Routes |
| RG-http | a file importing `net/http` (and neither chi nor gin) that mentions `ServeMux`, `NewServeMux`, `DefaultServeMux`, `Handle` or `HandleFunc` as a selector or qualified type of its `net/http` name | routes: Routes |
| RG-generated | a file passing none of the gates above that calls a generated server's entry function (`Handler`, `HandlerFromMux`, `HandlerFromMuxWithBaseURL`, `HandlerWithOptions`, `RegisterHandlers` or `RegisterHandlersWithOptions`) with at least one argument, as `F(...)` or `pkg.F(...)` with `pkg` an import that is neither a standard-library nor a router package (contract row GR-G1) | routes: Routes |
| RG-D | every other file | nothing |
| RF-decl | a function or method declaration with a body in a gated file that is a router function (see Gate and filters, 2) | func: Name, Receiver, Params, Results, Span, Blocks, Bindings, Calls, Returns |
| RF-literal | a function literal that is a router function, inside a captured record or a kept package var | func: Parent, Block, Params, Results, Span, Blocks, Bindings, Calls, Returns |
| RF-D | any other function declaration or literal of a gated file (a literal inside router syntax is row RE-closure) | nothing |
| RB-body | the body of a captured record | block: Kind |
| RB-block | a bare `{ }` block | block: Parent, Kind |
| RB-if | an `if` statement (initializer and condition) | block: Parent, Kind |
| RB-then | an `if` statement's consequence | block: Parent, Kind |
| RB-else | an `if` statement's alternative | block: Parent, Kind |
| RB-for | a `for` statement, any clause | block: Parent, Kind |
| RB-switch | an expression or type switch (initializer and tag) | block: Parent, Kind |
| RB-case | a case or default clause of a switch, type switch or select | block: Parent, Kind |
| RB-go | a `go` statement | block: Parent, Kind |
| RB-defer | a `defer` statement | block: Parent, Kind |
| RL-var | a `var` spec in a body, kept (see Gate and filters, 7) | binding: Kind, Names, Type, Values, Block, Span |
| RL-define | a `:=` declaration or a select receive `x := <-c`, kept | binding: Kind, Names, Values, Block, Span |
| RL-assign | an `=` assignment or receive `x = <-c` with at least one identifier target, kept | binding: Kind, Names, Values, Block, Span |
| RL-const | a `const` spec in a body, kept | binding: Kind, Names, Values, Block, Span |
| RL-range | a `for k, v := range e` clause, kept | binding: Kind, Names, Values, Block, Span |
| RL-range_assign | a `for k, v = range e` clause, kept | binding: Kind, Names, Values, Block, Span |
| RL-typeswitch | a type switch guard `switch x := e.(type)`, kept | binding: Kind, Names, Values, Block, Span |
| RL-handler | an `=` assignment `x.Handler = v` with one target, `x` an identifier, and one value (step GR5: a serving position when `x` is an `http.Server`, row GR-C6); kept always, with the bindings of the names it uses; Names holds `x` (no name is declared or assigned) | binding: Kind, Names, Values, Block, Span |
| RL-D | a binding no kept syntax uses; compound assignments (`+=`), increments, assignments without an identifier target (other than RL-handler) | nothing |
| RC-router | a router call (see Gate and filters, 3) | call: Kind, Call, Block, Span, Hash, Context |
| RC-call | a plain call that may pass a router on (see Gate and filters, 5) | call: Kind, Call, Block, Span, Context |
| RC-serve | a serving call (see Gate and filters, 5): a standard-library call outside a binding's value and a return that is a `net/http` serving function or holds a `net/http` `StripPrefix` or serving call | call: Kind, Call, Block, Span, Context |
| RC-generated | a call of a generated server's entry function as in RG-generated, outside a captured call's arguments, a serving call, a binding's value and a return (those keep it in their tree), kept whatever its arguments are | call: Kind, Call, Block, Span, Context |
| RC-excluded | a router-named call on a receiver no rule proves a router (see Gate and filters, 4) | nothing |
| RC-D | every other call (kept only inside a captured tree) | nothing |
| RR-return | a `return` statement of a captured record (outside nested literals) | return: Values, Block, Span |
| RJ-jump | a `goto` statement, or a `break` or `continue` naming a label, in a captured record's body (outside nested literals); a plain `break` or `continue` is not one | jump: Jumps |
| RU-escape | a position outside the record's kept syntax (kept bindings, calls and returns) where a value that may hold a router goes somewhere no contract row reads (see Escapes): a store to a field, element, dereference or a name the scope does not declare; a composite literal element; an argument of a call the payload does not keep (with the call's value bound locally, the arguments go with it); a channel send; the receiver of a method other than a router's own or a router method value; a return of a function literal with no record; a handler-shaped literal (`Request`) in one of those positions | escape: Form, Value, Detail, Type, Block, Span |
| RU-context | a kept call that lies in syntax the payload does not keep: an expression, go or defer statement or a condition operand (`statement`), or the value of a binding to a local the scope declares (`local`) | call: Context |
| RE-funclit | a function literal inside a kept tree that has a captured record | expression: Kind, Func |
| RE-closure | a function literal inside a kept tree that has no record | expression: Kind, Uses |
| RE-forward | a function literal inside a kept tree whose syntax passes the forwarding proof (see Forwarding closures), in addition to RE-funclit or RE-closure | expression: Forward |
| RE-forward-D | any other function literal inside a kept tree | nothing |
| RE-request | a function literal inside a kept tree with a `*http.Request` parameter of the file's `net/http` name, in addition to RE-funclit or RE-closure (a captured literal's `Uses` are filled too) | expression: Request |
| RE-text | an argument of a router-named call, other than a function literal | expression: Text |
| RV-var | a package-level var spec of a gated file that mentions router syntax or whose names a captured record uses | var: Names, Type, Values, Line |
| RV-D | every other package-level var spec of a gated file | nothing |
| RK-const | a package-level constant of a gated `_test.go` file whose value is string literals, names (not `iota`), selectors and `+` of those | const: Name, Type, Value, Line |
| RK-D | any other package-level constant of a gated `_test.go` file | nothing |
| RS-signature | a function without receiver of a non-test file that declares exactly one result of type `T`, `*T`, `pkg.T` or `*pkg.T`, T not predeclared | signature: Name, Result, Line |
| RS-D | any other function declaration of a non-test file | nothing |
| RO-server | a gated file whose header, before the package clause, holds the line `// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version vX.Y.Z DO NOT EDIT.` with version v2.8.0, v2.8.1 or v2.8.2, that imports `net/http` and either `github.com/go-chi/chi/v5` or `github.com/gin-gonic/gin` or neither (all unaliased, no other aliased import), and whose server declarations equal the templates token for token (see "Generated servers") | generated: Generator, Version, Framework |
| RO-D | a file with an oapi-codegen header of another version, other imports or declarations that differ from the templates | nothing |
| RN-names | every Go file: per name a call may target (an identifier `F`, a selector `.M` of a value, `imp:<path>.F` of an import), the count of its calls and of its other uses (not declaration names, assignment or definition targets, keyed-element keys, labels or type names) | names: Names |
| RN-results | every Go file: for each call of a module function or method outside the captured router syntax, each use of its result (directly, or through the names a binding gives it in the enclosing body) that no row reads by construction: an argument of a module call (`argument`: Callee, Arg), a return (`return`: Callee the enclosing function), anything else (`other`); a discarded call, a serving call's handler (`net/http`, `httptest`), an `http.Server`'s `Handler`, a router's own method or `ServeHTTP` record nothing | names: Results |
| RD-debug | a header line `//go:debug ...` that mentions `httpmuxgo121` | debug: Debug |
| RW-wrapper | a function or method declaration of a file importing `net/http` with exactly one `http.Handler` parameter and one `http.Handler` result whose syntax passes the wrapper proof (see Wrappers and request helpers) | wrapper: Name, Receiver, Param, Context, Passes, Methods, Guards, Line |
| RW-D | a declaration with that signature whose syntax fails the proof | nothing |
| RQ-request | any other function or method declaration of such a file with exactly one named `*http.Request` parameter whose every use is a read or a pass (see Wrappers and request helpers) | request: Name, Receiver, Param, Passes, URL, Method, Line |
| RQ-D | every other function or method declaration with a body of such a file | nothing |

## Census (the T023 investigation corpora)

`TestGoRoutesCensus` (set `GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS` to the
corpus directory holding `realworld-gin`, `gotify`, `dozzle`,
`miniflux` and `owncast`, cloned by `scripts/cloud-corpus.sh`) parses the
five corpora at their pins and checks every `route` and `compose` row of
`docs/contract/claims/data/go-http-routers-census.jsonl` (sites only): the row's file passes the
gate and the payload holds a router call at the row's position whose
receiver chain names the row's method. Captured router calls the census
does not list (HTTP client and test-helper calls named `Get`, `Handle`
or `Group` on receivers the filters cannot rule out) are the link pass's
to refuse; the test logs their count per corpus.

| corpus | census sites | Go files | gated | router calls not in the census |
|---|---|---|---|---|
| realworld-gin | 62 | 21 | 12 | 1 (`Get`) |
| gotify | 71 | 142 | 51 | 4 (`Get` 2, `Group`, `Handle`) |
| dozzle | 106 | 402 | 18 | 30 (`Get`) |
| miniflux | 181 | 441 | 8 | 0 |
| owncast | 519 | 472 | 6 | 2 (`Get`) |

Refreshed (Go payload version 5): the gate also passes files
that only serve a router (`StripPrefix`, `ListenAndServe`, `Serve`,
`Server`: gotify 50 to 51, miniflux 5 to 8 gated files). The unlisted
router calls fell from 40 to 30 (dozzle) and 5 to 4 (gotify) against
step GR1's table, after the filter forms of payload versions 4 and 5.

## Known gaps

- A router reached only through a file the gate does not pass (a file
  that registers on a mux obtained from a factory and names neither
  chi, gin nor the `net/http` mux API) is not captured; the census has
  none.
- A router function whose only router syntax is a call of a module
  factory (`r := newRouter(); register(r)` in a function that names no
  router type, member or router call) is not captured.
- Signatures are not captured in test files: a test-scope handler whose
  receiver comes from a constructor in a test file has no GR-H2 type.
- A router stored into a struct field, map, slice or channel is visible
  only as syntax of the kept bindings and calls that mention it; whether
  that blocks a path is the link pass's decision (contract row GR-C4).
