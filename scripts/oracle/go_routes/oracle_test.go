// Package goroutes is the opt-in chi and gin oracle of task T023
// (ADR 0076). It is its own Go module: chi and gin are pinned here
// (go.mod: chi v5.5.0 and gin v1.12.0; go.b.mod: chi v5.2.4 and gin
// v1.10.0), never in Graphvein's go.mod. Never run by CI.
//
// TestGoRoutesOracle reads the composition graphs that Graphvein's
// TestGoRoutesGraphExport writes (objects, edges, registrations and the
// chains from each root: never corpus code, which is neither built nor
// run), rebuilds each chi and gin registration tree in the real router,
// and checks that
//
//   - every claimed registration and composition is accepted (chi and gin
//     panic on invalid or conflicting ones);
//   - chi.Walk and gin's Engine.Routes() list exactly the claimed
//     (method, raw path) set of each root (a claimed ANY covers the
//     methods the version registers for it);
//   - a sample request built from every claimed chain (through the root
//     and its served StripPrefix prefix) reaches the claiming
//     registration (static routes: listing only).
//   - since step GR7, a chi router at a root ServeMux's "/" (row GR-C7) is
//     rebuilt behind a real ServeMux holding that mux's other
//     registrations and compositions, and behind the forwarding closure's
//     shadow cases (chiHops); its chains are dispatched through the mux.
//
// Run it with scripts/oracle/go_routes/run.sh.
//
// Licences (read before use, recorded in the T023 task record (internal)):
// github.com/go-chi/chi/v5 MIT; github.com/gin-gonic/gin MIT; gin's
// dependencies as listed there.
package goroutes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
)

type graph struct {
	Objects       []object       `json:"objects"`
	Edges         []edge         `json:"edges"`
	Registrations []registration `json:"registrations"`
}

type object struct {
	ID        string  `json:"id"`
	Framework string  `json:"framework"`
	Kind      string  `json:"kind"`
	Root      bool    `json:"root"`
	Served    []serve `json:"served"`
}

type serve struct {
	Strip    string   `json:"strip"`
	Wrappers []string `json:"wrappers"`
}

type edge struct {
	Parent  string   `json:"parent"`
	Child   string   `json:"child"`
	Kind    string   `json:"kind"`
	Prefix  string   `json:"prefix"`
	Outer   string   `json:"outer"`
	Blocked string   `json:"blocked"`
	Shadows []string `json:"shadows"`
}

type registration struct {
	Identity  string   `json:"identity"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Framework string   `json:"framework"`
	Method    string   `json:"method"`
	Pattern   string   `json:"pattern"`
	Static    string   `json:"static"`
	Objects   []string `json:"objects"`
	Paths     []struct {
		Template      string `json:"template"`
		TrailingSlash bool   `json:"trailing_slash"`
	} `json:"paths"`
	Chains []chain `json:"chains"`
}

type chain struct {
	Root          string `json:"root"`
	Strip         string `json:"strip"`
	Raw           string `json:"raw"`
	Template      string `json:"template"`
	TrailingSlash bool   `json:"trailing_slash"`
	// Bare is a chi "/" route's bare prefix form (row GR-C1): served by
	// Mount's stub registration, which chi.Walk does not list.
	Bare bool `json:"bare"`
}

// result counts one framework's run on one graph.
type result struct {
	registered, roots, listed, unclaimed, compared, agree, static, duplicates, slashChecks int
	disagreements, unclaimedList                                                           []string
}

func (r *result) fail(format string, args ...any) {
	r.disagreements = append(r.disagreements, fmt.Sprintf(format, args...))
}

const routeHeader = "X-Graphvein-Route"

func tagHandler(identity string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.Header().Set(routeHeader, identity) }
}

// sample builds a request path from a raw composed path (framework
// syntax), as Graphvein's in-suite net/http oracle does.
func sample(framework, raw string) (string, error) {
	parts := strings.Split(raw, "/")
	for i, p := range parts {
		switch framework {
		case "chi":
			switch {
			case p == "*" && i == len(parts)-1:
				parts[i] = "gvx1/gvx2"
			case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
				_, re, hasRe := strings.Cut(p[1:len(p)-1], ":")
				if !hasRe {
					parts[i] = "gvx1"
					continue
				}
				v, err := regexpValue(re)
				if err != nil {
					return "", err
				}
				parts[i] = v
			case strings.Contains(p, "{") || strings.Contains(p, "*"):
				return "", fmt.Errorf("parameter inside a segment: %q", p)
			}
		case "gin":
			switch {
			case strings.HasPrefix(p, ":"):
				parts[i] = "gvx1"
			case strings.HasPrefix(p, "*"):
				parts[i] = "gvx1/gvx2"
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

func regexpValue(re string) (string, error) {
	rx, err := regexp.Compile("^(?:" + re + ")$")
	if err != nil {
		return "", err
	}
	for _, v := range []string{"1", "12", "a", "abc", "gvx1", "a1", "2024-01-01", "x.y", "A"} {
		if rx.MatchString(v) {
			return v, nil
		}
	}
	return "", fmt.Errorf("no sample value for regexp %q", re)
}

func dispatch(h http.Handler, method, path string) string {
	req := httptest.NewRequest(method, "http://example.org"+path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Header().Get(routeHeader)
}

// protect runs f and reports a panic of the real router as a
// disagreement.
func protect(res *result, what string, f func()) bool {
	ok := true
	func() {
		defer func() {
			if v := recover(); v != nil {
				res.fail("%s: the router rejects it: %v", what, v)
				ok = false
			}
		}()
		f()
	}()
	return ok
}

// tree resolves graph objects of one framework to real routers: a
// constructor is one router; a child made through a prefixed edge (chi
// Route, gin Group) is one router per real parent; a child through a
// "same" edge (chi Group and With, gin Use, a parameter binding) stands
// for its parents' routers.
type tree struct {
	g       graph
	objects map[string]object
	in      map[string][]edge
	memo    map[string][]any
	busy    map[string]bool
	make    func(o object) any
	child   func(parent any, e edge, res *result) any
	res     *result
}

func newTree(g graph, framework string, res *result) *tree {
	t := &tree{g: g, objects: map[string]object{}, in: map[string][]edge{}, memo: map[string][]any{}, busy: map[string]bool{}, res: res}
	for _, o := range g.Objects {
		if o.Framework == framework {
			t.objects[o.ID] = o
		}
	}
	for _, e := range g.Edges {
		if _, ok := t.objects[e.Child]; ok {
			t.in[e.Child] = append(t.in[e.Child], e)
		}
	}
	return t
}

func (t *tree) reals(id string) []any {
	if r, ok := t.memo[id]; ok {
		return r
	}
	if t.busy[id] {
		return nil
	}
	t.busy[id] = true
	defer delete(t.busy, id)
	o := t.objects[id]
	var out []any
	if o.Kind == "ctor" {
		out = []any{t.make(o)}
	} else {
		for _, e := range t.in[id] {
			if e.Kind == "same" {
				out = append(out, t.reals(e.Parent)...)
				continue
			}
			for _, p := range t.reals(e.Parent) {
				if c := t.child(p, e, t.res); c != nil {
					out = append(out, c)
				}
			}
		}
	}
	t.memo[id] = out
	return out
}

// listingCheck compares a root's listing (method, route -> the
// registration that answers it) with the claims of its chains: every
// claimed (method, raw path) is listed for the claiming registration (a
// claimed ANY on its path, checked with GET), and every listed route of a
// registration that claims a chain from this root is one of its claims. A
// listed route of a registration without a chain from this root is a path
// the rules do not claim (counted, no disagreement: no fact is stated).
func listingCheck(res *result, root string, g graph, framework string, listed map[[2]string]string, alias map[string]string) {
	claimed := map[[2]string]string{}
	anyPath := map[string]string{}
	chained := map[string]bool{}
	for _, r := range g.Registrations {
		if r.Framework != framework {
			continue
		}
		for _, c := range r.Chains {
			if c.Root != root || c.Bare {
				continue
			}
			chained[r.Identity] = true
			if r.Method == "ANY" {
				anyPath[c.Raw] = r.Identity
			} else {
				claimed[[2]string{r.Method, c.Raw}] = r.Identity
			}
		}
	}
	expect := func(id string) string {
		if a, ok := alias[id]; ok {
			return a
		}
		return id
	}
	var keys [][2]string
	for k := range listed {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][1]+keys[i][0] < keys[j][1]+keys[j][0] })
	for _, k := range keys {
		for _, id := range strings.Split(listed[k], "|") {
			res.listed++
			if !chained[id] {
				res.unclaimed++
				res.unclaimedList = append(res.unclaimedList, fmt.Sprintf("%s %s %s (%s)", root, k[0], k[1], id))
				continue
			}
			if c, ok := claimed[k]; ok && expect(c) == id {
				continue
			}
			if a, ok := anyPath[k[1]]; ok && expect(a) == id {
				continue
			}
			res.fail("root %s lists %s %s for %s, which it does not claim", root, k[0], k[1], id)
		}
	}
	for k, id := range claimed {
		if !listedFor(listed[k], expect(id)) {
			res.fail("root %s: claimed %s %s (%s) is listed for %q", root, k[0], k[1], id, listed[k])
		}
	}
	for p, id := range anyPath {
		if !listedFor(listed[[2]string{http.MethodGet, p}], expect(id)) {
			res.fail("root %s: claimed ANY %s (%s) is listed for %q", root, p, id, listed[[2]string{http.MethodGet, p}])
		}
	}
}

// answer is the registration a handler answers for.
func answer(h http.Handler) string {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.org/", nil))
	return w.Header().Get(routeHeader)
}

func checkDispatch(res *result, framework string, r registration, roots map[string]http.Handler, alias map[string]string) {
	if r.Static != "" {
		res.static += len(r.Chains)
		return
	}
	templates := map[string]bool{}
	for _, p := range r.Paths {
		templates[fmt.Sprintf("%s|%v", p.Template, p.TrailingSlash)] = true
	}
	for _, c := range r.Chains {
		res.compared++
		what := fmt.Sprintf("%s via %s (raw %q, strip %q)", r.Identity, c.Root, c.Raw, c.Strip)
		if !templates[fmt.Sprintf("%s|%v", c.Template, c.TrailingSlash)] {
			res.fail("%s: chain template %s is not a path entry", what, c.Template)
			continue
		}
		h := roots[c.Root]
		if h == nil {
			res.fail("%s: the root was not rebuilt", what)
			continue
		}
		if c.Strip != "" {
			h = http.StripPrefix(c.Strip, h)
		}
		path, err := sample(framework, c.Raw)
		if err != nil {
			res.fail("%s: %v", what, err)
			continue
		}
		method := r.Method
		if method == "ANY" {
			method = http.MethodGet
		}
		want := r.Identity
		if a, ok := alias[want]; ok {
			want = a
		}
		if got := dispatch(h, method, c.Strip+path); got != want {
			res.fail("%s: %s %s reaches %q", what, method, c.Strip+path, got)
			continue
		}
		res.agree++
		// The other slash form (TOP-1 of 2026-11-18): unless the route
		// claims it too, it must not reach the route (gin redirects,
		// chi and net/http answer 404 or another route).
		if c.Template == "/" || strings.Contains(c.Template, "{*") || templates[fmt.Sprintf("%s|%v", c.Template, !c.TrailingSlash)] {
			continue
		}
		other := path + "/"
		if strings.HasSuffix(path, "/") {
			other = strings.TrimSuffix(path, "/")
		}
		res.slashChecks++
		if got := dispatch(h, method, c.Strip+other); got == want {
			res.fail("%s: %s %s (the unclaimed slash form) reaches the route", what, method, c.Strip+other)
		}
	}
}

// chiOracle rebuilds the chi part of a graph.
func chiOracle(g graph) result {
	var res result
	t := newTree(g, "chi", &res)
	t.make = func(object) any { return chi.NewRouter() }
	mounted := map[string]bool{}
	t.child = func(p any, e edge, res *result) any {
		parent := p.(*chi.Mux)
		child := t.objects[e.Child]
		if child.Kind == "ctor" {
			return nil
		}
		sub := chi.NewRouter()
		if e.Blocked == "" {
			protect(res, "chi Route "+e.Prefix, func() { parent.Mount(e.Prefix, sub) })
		}
		_ = mounted
		return sub
	}
	// chi Mount compositions: a constructor child mounted on each real
	// parent.
	for _, e := range g.Edges {
		c, ok := t.objects[e.Child]
		if !ok || e.Kind != "chi" || c.Kind != "ctor" || e.Blocked != "" {
			continue
		}
		subs := t.reals(e.Child)
		for _, p := range t.reals(e.Parent) {
			for _, s := range subs {
				parent, sub := p.(*chi.Mux), s.(*chi.Mux)
				protect(&res, "chi Mount "+e.Prefix, func() { parent.Mount(e.Prefix, sub) })
			}
		}
	}
	alias := duplicates(g, "chi", &res)
	for _, r := range g.Registrations {
		if r.Framework != "chi" || alias[r.Identity] != "" {
			continue
		}
		h := tagHandler(r.Identity)
		pattern := r.Pattern
		if _, rest, ok := strings.Cut(pattern, " "); ok && !strings.HasPrefix(pattern, "/") {
			pattern = strings.TrimLeft(rest, " \t")
		}
		for _, id := range r.Objects {
			for _, x := range t.reals(id) {
				m := x.(*chi.Mux)
				if protect(&res, r.Identity, func() {
					if r.Method == "ANY" {
						m.Handle(pattern, h)
					} else {
						m.Method(r.Method, pattern, h)
					}
				}) {
					res.registered++
				}
			}
		}
	}
	roots := map[string]http.Handler{}
	for _, o := range g.Objects {
		if o.Framework != "chi" || !o.Root {
			continue
		}
		res.roots++
		rs := t.reals(o.ID)
		if len(rs) != 1 {
			res.fail("root %s: %d routers", o.ID, len(rs))
			continue
		}
		m := rs[0].(*chi.Mux)
		roots[o.ID] = m
		listed := map[[2]string]string{}
		if err := chi.Walk(m, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
			listWalk(listed, [2]string{method, route}, answer(h))
			return nil
		}); err != nil {
			res.fail("root %s: chi.Walk: %v", o.ID, err)
		}
		listingCheck(&res, o.ID, g, "chi", listed, alias)
	}
	chiHops(&res, g, t, roots, alias)
	for _, r := range g.Registrations {
		if r.Framework == "chi" {
			checkDispatch(&res, "chi", r, roots, alias)
		}
	}
	return res
}

// chiHops rebuilds the ServeMux -> chi hops (row GR-C7): each
// net/http root mux whose "/" pattern holds a chi router, directly or
// through a forwarding closure, is a real ServeMux with its other
// registrations (tag handlers) and compositions (a stub answering
// "gv-mux-child"), and at "/" a closure answering its shadow cases itself
// ("gv-closure-case") and serving the real chi router otherwise. The chi
// router's listing is compared with the chains claimed from the mux root,
// and the chains are dispatched through the mux (checkDispatch).
func chiHops(res *result, g graph, t *tree, roots map[string]http.Handler, alias map[string]string) {
	objects := map[string]object{}
	for _, o := range g.Objects {
		objects[o.ID] = o
	}
	// up resolves a net/http object to its root muxes through "same"
	// edges (a parameter bound to a mux).
	var up func(id string, seen map[string]bool) []string
	up = func(id string, seen map[string]bool) []string {
		if seen[id] {
			return nil
		}
		seen[id] = true
		o := objects[id]
		if o.Root && o.Framework == "net/http" {
			return []string{id}
		}
		var out []string
		for _, e := range g.Edges {
			if e.Kind == "same" && e.Child == id {
				out = append(out, up(e.Parent, seen)...)
			}
		}
		return out
	}
	muxes := map[string]*http.ServeMux{}
	mux := func(id string) *http.ServeMux {
		if muxes[id] == nil {
			muxes[id] = http.NewServeMux()
		}
		return muxes[id]
	}
	var hops []edge
	for _, e := range g.Edges {
		if e.Kind == "muxchi" && e.Blocked == "" {
			hops = append(hops, e)
		}
	}
	if len(hops) == 0 {
		return
	}
	hopRoots := map[string]bool{}
	for _, e := range hops {
		for _, r := range up(e.Parent, map[string]bool{}) {
			hopRoots[r] = true
		}
	}
	for _, r := range g.Registrations {
		if r.Framework != "net/http" {
			continue
		}
		for _, id := range r.Objects {
			for _, root := range up(id, map[string]bool{}) {
				if hopRoots[root] {
					m := mux(root)
					protect(res, r.Identity, func() { m.Handle(r.Pattern, tagHandler(r.Identity)) })
				}
			}
		}
	}
	for _, e := range g.Edges {
		if e.Kind != "mux" || e.Blocked != "" {
			continue
		}
		for _, root := range up(e.Parent, map[string]bool{}) {
			if hopRoots[root] {
				m := mux(root)
				protect(res, "composition "+e.Outer, func() { m.Handle(e.Outer, tagHandler("gv-mux-child")) })
			}
		}
	}
	for _, e := range hops {
		subs := t.reals(e.Child)
		if len(subs) != 1 {
			res.fail("chi hop %s -> %s: %d routers", e.Parent, e.Child, len(subs))
			continue
		}
		router := subs[0].(*chi.Mux)
		shadows := map[string]bool{}
		for _, sh := range e.Shadows {
			shadows[sh] = true
		}
		closure := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shadows[r.URL.Path] {
				w.Header().Set(routeHeader, "gv-closure-case")
				return
			}
			router.ServeHTTP(w, r)
		})
		for _, root := range up(e.Parent, map[string]bool{}) {
			m := mux(root)
			protect(res, "chi hop "+e.Outer, func() { m.Handle(e.Outer, closure) })
			roots[root] = m
			res.roots++
			listed := map[[2]string]string{}
			if err := chi.Walk(router, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
				listWalk(listed, [2]string{method, route}, answer(h))
				return nil
			}); err != nil {
				res.fail("hop root %s: chi.Walk: %v", root, err)
			}
			listingCheck(res, root, g, "chi", listed, alias)
		}
	}
}

// ginOracle rebuilds the gin part of a graph.
func ginOracle(g graph) result {
	var res result
	t := newTree(g, "gin", &res)
	engines := map[string]*gin.Engine{}
	t.make = func(o object) any {
		e := gin.New()
		engines[o.ID] = e
		return &e.RouterGroup
	}
	t.child = func(p any, e edge, res *result) any {
		var out *gin.RouterGroup
		if e.Blocked != "" {
			// No path through a blocked edge: a detached group.
			return gin.New().Group("/gv-detached")
		}
		protect(res, "gin Group "+e.Prefix, func() { out = p.(*gin.RouterGroup).Group(e.Prefix) })
		if out == nil {
			return nil
		}
		return out
	}
	// A gin static call registers GET and HEAD at once: one call per
	// site and group.
	statics := map[string]bool{}
	alias := duplicates(g, "gin", &res)
	for _, r := range g.Registrations {
		if r.Framework != "gin" || alias[r.Identity] != "" {
			continue
		}
		h := tagHandler(r.Identity)
		for _, id := range r.Objects {
			for i, x := range t.reals(id) {
				grp := x.(*gin.RouterGroup)
				if r.Static != "" {
					key := fmt.Sprintf("%s:%d:%s:%d:%p", r.File, r.Line, id, i, grp)
					if statics[key] {
						continue
					}
					statics[key] = true
				}
				if protect(&res, r.Identity, func() {
					switch {
					case r.Static == "Static":
						grp.Static(r.Pattern, ".")
					case r.Static == "StaticFS":
						grp.StaticFS(r.Pattern, gin.Dir(".", false))
					case r.Static == "StaticFile":
						grp.StaticFile(r.Pattern, "gv-oracle-file")
					case r.Static == "StaticFileFS":
						grp.StaticFileFS(r.Pattern, "gv-oracle-file", http.Dir("."))
					case r.Method == "ANY":
						grp.Any(r.Pattern, gin.HandlerFunc(func(c *gin.Context) { h(c.Writer, c.Request) }))
					default:
						grp.Handle(r.Method, r.Pattern, gin.HandlerFunc(func(c *gin.Context) { h(c.Writer, c.Request) }))
					}
				}) {
					res.registered++
				}
			}
		}
	}
	staticAt := map[[3]string]string{}
	for _, r := range g.Registrations {
		if r.Framework == "gin" && r.Static != "" {
			for _, c := range r.Chains {
				staticAt[[3]string{c.Root, r.Method, c.Raw}] = r.Identity
			}
		}
	}
	roots := map[string]http.Handler{}
	for _, o := range g.Objects {
		if o.Framework != "gin" || !o.Root {
			continue
		}
		res.roots++
		t.reals(o.ID)
		e := engines[o.ID]
		if e == nil {
			res.fail("root %s was not rebuilt", o.ID)
			continue
		}
		roots[o.ID] = e
		listed := map[[2]string]string{}
		for _, ri := range e.Routes() {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "http://example.org/", nil)
			id := ""
			func() {
				defer func() { _ = recover() }()
				ri.HandlerFunc(c)
				id = w.Header().Get(routeHeader)
			}()
			if id == "" {
				// A static handler answers no tag: the static
				// registration of that path.
				id = staticAt[[3]string{o.ID, ri.Method, ri.Path}]
			}
			listed[[2]string{ri.Method, ri.Path}] = id
		}
		listingCheck(&res, o.ID, g, "gin", listed, alias)
	}
	for _, r := range g.Registrations {
		if r.Framework == "gin" {
			checkDispatch(&res, "gin", r, roots, alias)
		}
	}
	return res
}

// duplicates maps each registration that repeats an earlier one of the
// same framework (same objects, method and pattern: identity suffix @2,
// @3) to the first: net/http and gin panic on such a repeat at start-up
// and chi replaces it, and the contract states no rule for it (task
// decision 3), so the oracle registers the first only and counts the
// repeats.
func duplicates(g graph, framework string, res *result) map[string]string {
	first := map[string]string{}
	alias := map[string]string{}
	for _, r := range g.Registrations {
		if r.Framework != framework {
			continue
		}
		objs := append([]string(nil), r.Objects...)
		sort.Strings(objs)
		key := strings.Join(objs, ",") + "\x00" + r.Method + "\x00" + r.Pattern + "\x00" + r.Static
		if f, ok := first[key]; ok {
			alias[r.Identity] = f
			res.duplicates++
			continue
		}
		first[key] = r.Identity
	}
	return alias
}

// TestGoRoutesOracle runs the chi and gin oracle on every graph in
// GRAPHVEIN_GO_ROUTES_GRAPHS (a directory of <name>.json files).
func TestGoRoutesOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_GRAPHS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_GRAPHS is not set")
	}
	gin.SetMode(gin.ReleaseMode)
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no graphs in %s: %v", dir, err)
	}
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var g graph
		if err := json.Unmarshal(data, &g); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		for _, run := range []struct {
			framework string
			f         func(graph) result
		}{{"chi", chiOracle}, {"gin", ginOracle}} {
			res := run.f(g)
			for _, d := range res.disagreements {
				t.Errorf("%s: %s: %s", name, run.framework, d)
			}
			for _, u := range res.unclaimedList {
				t.Logf("%s: %s: listed without a claim (no path stated): %s", name, run.framework, u)
			}
			t.Logf("%s: %s: %d roots, %d registrations accepted (%d repeats, decision 3), %d listed routes compared (%d of registrations without a chain from that root), %d chains dispatched, %d agree, %d unclaimed slash forms checked, %d static chains (listing only), %d disagreements", name, run.framework, res.roots, res.registered, res.duplicates, res.listed, res.unclaimed, res.compared, res.agree, res.slashChecks, res.static, len(res.disagreements))
		}
	}
}

// listWalk records one chi.Walk entry; chi lists a parent route and a
// mounted route of the same method and path both: the ids are
// joined with "|", and dispatch decides which one answers.
func listWalk(listed map[[2]string]string, k [2]string, id string) {
	if old, ok := listed[k]; ok && old != id {
		listed[k] = old + "|" + id
		return
	}
	listed[k] = id
}

// listedFor reports that a listing entry names id.
func listedFor(entry, id string) bool {
	for _, x := range strings.Split(entry, "|") {
		if x == id {
			return true
		}
	}
	return false
}
