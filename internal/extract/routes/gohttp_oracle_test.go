package routes

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

	"github.com/graphvein/graphvein/internal/lang/golang"
)

// The T023 oracle: the registration tree of the composition
// graph (goRouteGraph: objects, edges, registrations, never corpus code)
// is rebuilt in real routers and every claimed path is dispatched. The
// net/http part runs here with Go's own ServeMux; chi and gin run in the
// opt-in harness under scripts/oracle/go_routes, which reads the graphs
// TestGoRoutesGraphExport writes.

// goOracleCorpora are the T023 corpora (scripts/cloud-corpus.sh).
var goOracleCorpora = []string{"realworld-gin", "gotify", "dozzle", "miniflux", "owncast"}

// goOracleGraph is one graph to check: the fixture, and the corpora when
// GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS names their directory.
type goOracleGraph struct {
	name  string
	graph goRouteGraph
}

func goOracleGraphs(t *testing.T) []goOracleGraph {
	t.Helper()
	out := []goOracleGraph{{"fixture", goRun(t, goFixtureSources(t)).routeGraph()}}
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS")
	if dir == "" {
		t.Log("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS is not set: the fixture only")
		return out
	}
	for _, corpus := range goOracleCorpora {
		root := filepath.Join(dir, corpus)
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("%s: %v", corpus, err)
		}
		out = append(out, goOracleGraph{corpus, goCorpusRun(t, root).routeGraph()})
	}
	return out
}

// goOracleSample builds a sample request path from a raw composed path in
// framework syntax: a wildcard segment becomes a value no literal segment
// uses, a catch-all two segments, a chi regexp parameter a value matching
// it, a net/http {$} the end of the path.
func goOracleSample(framework, raw string) (string, error) {
	parts := strings.Split(raw, "/")
	for i, p := range parts {
		switch framework {
		case goNetHTTP:
			switch {
			case p == "{$}" && i == len(parts)-1:
				parts[i] = ""
			case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}"):
				parts[i] = "gvx1/gvx2"
			case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
				parts[i] = "gvx1"
			}
		case goChi:
			switch {
			case p == "*" && i == len(parts)-1:
				parts[i] = "gvx1/gvx2"
			case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
				name, re, hasRe := strings.Cut(p[1:len(p)-1], ":")
				_ = name
				if !hasRe {
					parts[i] = "gvx1"
					continue
				}
				v, err := goOracleRegexpValue(re)
				if err != nil {
					return "", err
				}
				parts[i] = v
			case strings.Contains(p, "{"):
				return "", fmt.Errorf("parameter inside a segment: %q", p)
			}
		case goGin:
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

// goOracleRegexpValue is a value matching a chi parameter regexp.
func goOracleRegexpValue(re string) (string, error) {
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

// goOracleHost is the host of a net/http pattern ("" without one).
func goOracleHost(pattern string) string {
	if _, rest, ok := strings.Cut(pattern, " "); ok {
		pattern = strings.TrimLeft(rest, " \t")
	}
	if i := strings.IndexByte(pattern, '/'); i > 0 {
		return pattern[:i]
	}
	return ""
}

// goOracleResult counts one oracle run.
type goOracleResult struct {
	registered, compared, agree, head, headAgree, slash int
	disagreements                                       []string
}

// goOracleAlt serves a request through the first of several muxes that has
// a pattern for it: one composition whose handler may hold one of several
// muxes (a binding in each branch) is one of them in a real run.
type goOracleAlt []*http.ServeMux

func (a goOracleAlt) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, m := range a {
		if _, p := m.Handler(r); p != "" {
			m.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

// goMuxOracle rebuilds the net/http part of a composition graph with Go's
// ServeMux and dispatches a sample request per claimed chain.
func goMuxOracle(g goRouteGraph) goOracleResult {
	var res goOracleResult
	objects := map[string]goGraphObject{}
	for _, o := range g.Objects {
		objects[o.ID] = o
	}
	muxes := map[string]*http.ServeMux{}
	same := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == "same" && e.Blocked == "" {
			same[e.Child] = append(same[e.Child], e.Parent)
		}
	}
	// base resolves an object to the real muxes it stands for: a
	// constructor, zero-value or default mux itself, a parameter the
	// objects bound to it.
	var base func(id string, seen map[string]bool) []string
	base = func(id string, seen map[string]bool) []string {
		if seen[id] {
			return nil
		}
		seen[id] = true
		switch objects[id].Kind {
		case "ctor", "zero", "default":
			if muxes[id] == nil {
				muxes[id] = http.NewServeMux()
			}
			return []string{id}
		}
		var out []string
		for _, p := range same[id] {
			out = append(out, base(p, seen)...)
		}
		return out
	}
	handle := func(m *http.ServeMux, pattern string, h http.Handler, what string) {
		defer func() {
			if v := recover(); v != nil {
				res.disagreements = append(res.disagreements, fmt.Sprintf("%s: ServeMux rejects %q: %v", what, pattern, v))
			}
		}()
		m.Handle(pattern, h)
		res.registered++
	}
	done := map[string]bool{}
	for _, r := range g.Registrations {
		if r.Framework != goNetHTTP {
			continue
		}
		identity := r.Identity
		tag := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Graphvein-Route", identity) })
		for _, id := range r.Objects {
			for _, b := range base(id, map[string]bool{}) {
				if key := b + "\x00" + identity; !done[key] {
					done[key] = true
					handle(muxes[b], r.Pattern, tag, identity)
				}
			}
		}
	}
	type slot struct{ parent, outer, prefix string }
	children := map[slot][]string{}
	var slots []slot
	for _, e := range g.Edges {
		if e.Kind != "mux" || e.Blocked != "" {
			continue
		}
		for _, p := range base(e.Parent, map[string]bool{}) {
			s := slot{p, e.Outer, e.Prefix}
			if children[s] == nil {
				slots = append(slots, s)
			}
			children[s] = append(children[s], base(e.Child, map[string]bool{})...)
		}
	}
	for _, s := range slots {
		var alt goOracleAlt
		seen := map[string]bool{}
		for _, c := range children[s] {
			if !seen[c] {
				seen[c] = true
				alt = append(alt, muxes[c])
			}
		}
		var h http.Handler = alt
		if len(alt) == 1 {
			h = alt[0]
		}
		if s.prefix != "" {
			h = http.StripPrefix(s.prefix, h)
		}
		handle(muxes[s.parent], s.outer, h, "composition "+s.outer)
	}
	// GR-C7: a chi router at a ServeMux's "/" (directly or through a
	// forwarding closure, whose shadow cases answer themselves) is a stub
	// here; the chi harness rebuilds the chi tree behind it.
	hops := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != "muxchi" || e.Blocked != "" {
			continue
		}
		shadows := map[string]bool{}
		for _, sh := range e.Shadows {
			shadows[sh] = true
		}
		stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shadows[r.URL.Path] {
				w.Header().Set("X-Graphvein-Route", goOracleShadow)
				return
			}
			w.Header().Set("X-Graphvein-Route", goOracleChi)
		})
		for _, p := range base(e.Parent, map[string]bool{}) {
			if !hops[p] {
				hops[p] = true
				handle(muxes[p], e.Outer, stub, "chi hop "+e.Outer)
			}
		}
	}
	for _, r := range g.Registrations {
		if r.Framework != goChi {
			continue
		}
		for _, c := range r.Chains {
			root := muxes[c.Root]
			if objects[c.Root].Framework != goNetHTTP {
				continue
			}
			res.compared++
			what := fmt.Sprintf("%s via %s (raw %q, strip %q)", r.Identity, c.Root, c.Raw, c.Strip)
			if root == nil {
				res.disagreements = append(res.disagreements, what+": the root is no mux")
				continue
			}
			var h http.Handler = root
			if c.Strip != "" {
				h = http.StripPrefix(c.Strip, root)
			}
			sample, err := goOracleSample(goChi, c.Raw)
			if err != nil {
				res.disagreements = append(res.disagreements, what+": "+err.Error())
				continue
			}
			method := r.Method
			if method == "ANY" {
				method = http.MethodGet
			}
			if got := goOracleDispatch(h, method, "", c.Strip+sample); got != goOracleChi {
				res.disagreements = append(res.disagreements, fmt.Sprintf("%s: %s %s reaches %q, not the chi hop", what, method, c.Strip+sample, got))
				continue
			}
			res.agree++
		}
	}
	for _, r := range g.Registrations {
		if r.Framework != goNetHTTP {
			continue
		}
		templates := map[string]bool{}
		for _, p := range r.Paths {
			templates[fmt.Sprintf("%s|%v", p.Template, p.TrailingSlash)] = true
		}
		for _, c := range r.Chains {
			res.compared++
			what := fmt.Sprintf("%s via %s (raw %q, strip %q)", r.Identity, c.Root, c.Raw, c.Strip)
			if !templates[fmt.Sprintf("%s|%v", c.Template, c.TrailingSlash)] {
				res.disagreements = append(res.disagreements, what+": chain template "+c.Template+" is not a path entry")
				continue
			}
			root := muxes[c.Root]
			if root == nil {
				res.disagreements = append(res.disagreements, what+": the root is no mux")
				continue
			}
			var h http.Handler = root
			if c.Strip != "" {
				h = http.StripPrefix(c.Strip, root)
			}
			sample, err := goOracleSample(goNetHTTP, c.Raw)
			if err != nil {
				res.disagreements = append(res.disagreements, what+": "+err.Error())
				continue
			}
			method := r.Method
			if method == "ANY" {
				method = http.MethodGet
			}
			got := goOracleDispatch(h, method, goOracleHost(r.Pattern), c.Strip+sample)
			if got != r.Identity {
				res.disagreements = append(res.disagreements, fmt.Sprintf("%s: %s %s reaches %q", what, method, c.Strip+sample, got))
				continue
			}
			res.agree++
			// The other slash form (TOP-1 of 2026-11-18): unless the route
			// claims it too, ServeMux answers it with a redirect, a 404
			// or another pattern, never this registration.
			if c.Template != "/" && !strings.Contains(c.Template, "{*") && !templates[fmt.Sprintf("%s|%v", c.Template, !c.TrailingSlash)] {
				other := sample + "/"
				if strings.HasSuffix(sample, "/") {
					other = strings.TrimSuffix(sample, "/")
				}
				res.slash++
				if goOracleDispatch(h, method, goOracleHost(r.Pattern), c.Strip+other) == r.Identity {
					res.disagreements = append(res.disagreements, fmt.Sprintf("%s: %s %s (the unclaimed slash form) reaches the registration", what, method, c.Strip+other))
				}
			}
			if method == http.MethodGet {
				// A GET pattern also answers HEAD (the client matcher's
				// net/http HEAD rule, decision 8).
				res.head++
				if goOracleDispatch(h, http.MethodHead, goOracleHost(r.Pattern), c.Strip+sample) == r.Identity {
					res.headAgree++
				} else {
					res.disagreements = append(res.disagreements, what+": HEAD does not reach the GET registration")
				}
			}
		}
	}
	return res
}

// The tags the GR-C7 stub answers: the chi router behind the hop, or a
// shadow case the forwarding closure answers itself.
const (
	goOracleChi    = "gv-chi-hop"
	goOracleShadow = "gv-closure-case"
)

// goOracleDispatch serves one request and returns the registration that
// answered it.
func goOracleDispatch(h http.Handler, method, host, path string) string {
	if host == "" {
		host = "example.org"
	}
	req := httptest.NewRequest(method, "http://"+host+path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Header().Get("X-Graphvein-Route")
}

// TestGoRoutesMuxOracle is the in-suite net/http oracle (task T023):
// the claimed net/http registrations and GR-C3 compositions of the
// fixture (and of the five corpora when GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS
// is set) are registered in real ServeMuxes (validity and conflicts), and
// a sample request for every claimed chain, through its root and the
// root's served StripPrefix prefix (GR-C6), reaches the claiming
// registration; a GET registration also answers HEAD. Wrappers (GR-V5),
// handlers, middleware, conditional and scope are not checked (mapped).
func TestGoRoutesMuxOracle(t *testing.T) {
	for _, c := range goOracleGraphs(t) {
		res := goMuxOracle(c.graph)
		for _, d := range res.disagreements {
			t.Errorf("%s: %s", c.name, d)
		}
		t.Logf("%s: net/http: %d registrations and compositions accepted, %d chains dispatched, %d agree, %d unclaimed slash forms checked, %d GET also HEAD of %d, %d disagreements", c.name, res.registered, res.compared, res.agree, res.slash, res.headAgree, res.head, len(res.disagreements))
	}
}

// TestGoMuxOracleSeesWrongFacts checks that the oracle catches the wrong
// paths it exists for: a path through a StripPrefix whose prefix ends
// with '/' (the outer mux redirects) and a path without the served
// prefix.
func TestGoMuxOracleSeesWrongFacts(t *testing.T) {
	g := goRouteGraph{
		Objects: []goGraphObject{{ID: "root", Framework: goNetHTTP, Kind: "ctor", Root: true}, {ID: "sub", Framework: goNetHTTP, Kind: "ctor"}},
		Edges:   []goGraphEdge{{Parent: "root", Child: "sub", Kind: "mux", Outer: "/v2/", Prefix: "/v2/"}},
		Registrations: []goGraphRegistration{{Identity: "r", Framework: goNetHTTP, Method: "GET", Pattern: "GET /status", Objects: []string{"sub"},
			Paths:  []goPathEntry{{fastapiPathEntry: fastapiPathEntry{pathEntry: pathEntry{Template: "/v2/status"}}}},
			Chains: []goGraphChain{{Root: "root", Raw: "/v2//status", Template: "/v2/status"}, {Root: "root", Raw: "/v2/status", Template: "/v2/status"}}}},
	}
	if res := goMuxOracle(g); len(res.disagreements) != 2 {
		t.Errorf("disagreements %q, want 2", res.disagreements)
	}
}

// TestGoMuxOracleSeesWrongChiHops checks the GR-C7 part of the oracle:
// through a chi router at a ServeMux's "/", a path a sibling
// subtree pattern takes, the /tree -> /tree/ redirect of that sibling and
// a closure's shadow case are caught; a free path agrees.
func TestGoMuxOracleSeesWrongChiHops(t *testing.T) {
	chain := func(raw string) goGraphRegistration {
		return goGraphRegistration{Identity: "chi " + raw, Framework: goChi, Method: "GET", Pattern: raw, Objects: []string{"r"},
			Chains: []goGraphChain{{Root: "m", Raw: raw, Template: raw}}}
	}
	g := goRouteGraph{
		Objects: []goGraphObject{{ID: "m", Framework: goNetHTTP, Kind: "ctor", Root: true}, {ID: "r", Framework: goChi, Kind: "ctor"}},
		Edges:   []goGraphEdge{{Parent: "m", Child: "r", Kind: "muxchi", Outer: "/", Shadows: []string{"/debug/vars"}}},
		Registrations: []goGraphRegistration{
			{Identity: "plugins", Framework: goNetHTTP, Method: "ANY", Pattern: "/plugins/", Objects: []string{"m"}},
			chain("/status"), chain("/plugins/x"), chain("/plugins"), chain("/debug/vars"),
		},
	}
	res := goMuxOracle(g)
	if len(res.disagreements) != 3 || res.agree != 1 {
		t.Errorf("agree %d, disagreements %q, want 1 and 3", res.agree, res.disagreements)
	}
}

// TestGoRoutesGraphExport writes the composition graphs of the fixture
// (and of the corpora when GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS is set) as
// JSON into GRAPHVEIN_GO_ROUTES_GRAPH_OUT, for the opt-in chi and gin
// oracle (scripts/oracle/go_routes).
func TestGoRoutesGraphExport(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_GRAPH_OUT")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_GRAPH_OUT is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range goOracleGraphs(t) {
		data, err := json.MarshalIndent(c.graph, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, c.name)
	}
	sort.Strings(names)
	t.Logf("wrote %s", strings.Join(names, ", "))
}

// goCorpusRun scans and links one corpus checkout as an index run would
// and returns the linker (for the composition graph).
func goCorpusRun(t *testing.T, root string) *goLinker {
	t.Helper()
	sources := goCorpusSources(t, root)
	goSources := map[string][]byte{}
	for p, s := range sources {
		if strings.HasSuffix(p, ".go") {
			goSources[p] = s
		}
	}
	files := goParse(t, goSources)
	golang.Link(files, golang.ModulePaths(sources))
	return runGo(files, sources, selectedRules(nil), nil)
}
