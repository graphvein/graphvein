# Unknown-prefix paths fixture, Go routers (T033)

Hand-written Go (chi v5, gin v1, net/http) syntax; no external corpus
source, nothing is installed or executed. No inert-name mapping is needed.

Each case is a route whose prefix the code does not fix, or a negative
case next to one, for the route side of the unknown-prefix path contract
(rows UP-1..UP-4 and UP-8..UP-10 of
`docs/contract/claims/unknown-prefix-paths.md`). Routes below a hop store
the hop in `unresolved_mounts` (T029 FU-2) and the paths proven below it in
`unresolved_prefix_paths`, with `prefix: "unresolved"`.

- `app/chi.go`: `r.Mount(base, items())` (UP-1, chi Mount);
  `r.Route(base, ...)` inside the read prefix `/outer` (UP-1, chi Route,
  `above: "/outer"`); one router mounted below two configured bases (one
  entry, UP-8); one router mounted at `/k` and below a configured base
  (it has a path: no entry, UP-9); a `Route(base)` inside an `if` (the
  entry is conditional); a regexp prefix chi refuses (`regexp_param`: no
  hop, no entry, row D); a pattern with an unknown piece (no route node,
  UP-10); a router parameter no resolved call binds (`route_unmounted`: no
  entry, UP-10).
- `app/gin.go`: `e.Group(prefix)` in `Gin`, which returns the engine (UP-1, gin Group), with `GET("")`, the
  prefix itself, stored as the template `""` (UP-10: never matched).
- `app/mux.go`: a mux behind a wrapper that answers one path itself (UP-4,
  wrapped), a mux stored in a struct field (UP-4, escaped),
  `root.Handle(prefix+"/", http.StripPrefix(prefix, api))` (UP-2,
  StripPrefix prefix), `root.Handle(pattern, api)` (UP-2, outer pattern),
  `http.StripPrefix("/{tenant}", api)` (a constant holding template
  syntax: the hop is stored, no entry, UP-2's refusal) and a StripPrefix
  of a configured prefix at a serving position (no hop is stored there:
  no entry, row D).
- `app/mux.go` `TailMux`: `root.Handle(prefix+"/api/",
  http.StripPrefix(prefix, sub))`: the inner mux sees only `/api/...`, so
  `GET /api/items/list` has an entry and `GET /other/thing/one` none
  (GR-C3's tail check below the unread prefix; Go 1.27.1 answers 404 at
  `<prefix>/other/thing/one`).
- `app/mux_test.go`: a StripPrefix hop in a test file (the entry has
  `scope: "test"`); `app/scoped_test.go` mounts the production router of `Scoped`
  (`app/chi.go`, below a configured base) at `/t`: the route keeps its
  production entry beside the test-scope path (UP-9 per scope).

The generated-server form (UP-3) is in the `go-routes` fixture
(`oapi/basechi`, two instances with different BaseURLs).

`unknown-prefix-go.golden.json` holds the route nodes, handler edges and
route diagnostics (`internal/index`, `TestUnknownPrefixGoGolden`);
`internal/extract/routes` `TestUnknownPrefixContractTags` checks that the
fixture reaches every implemented row and form.
