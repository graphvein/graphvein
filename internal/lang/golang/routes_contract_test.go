package golang

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// goRouteRows maps every row of the router payload table
// (docs/lang/go-routes.md) to the fields its capture branch fills; rows
// without fields capture nothing.
var goRouteRows = map[string][]string{
	"RG-chi":          {"Routes"},
	"RG-gin":          {"Routes"},
	"RG-http":         {"Routes"},
	"RG-generated":    {"Routes"},
	"RG-D":            nil,
	"RF-decl":         {"Name", "Receiver", "Params", "Results", "Span", "Blocks", "Bindings", "Calls", "Returns"},
	"RF-literal":      {"Parent", "Block", "Params", "Results", "Span", "Blocks", "Bindings", "Calls", "Returns"},
	"RF-D":            nil,
	"RB-body":         {"Kind"},
	"RB-block":        {"Parent", "Kind"},
	"RB-if":           {"Parent", "Kind"},
	"RB-then":         {"Parent", "Kind"},
	"RB-else":         {"Parent", "Kind"},
	"RB-for":          {"Parent", "Kind"},
	"RB-switch":       {"Parent", "Kind"},
	"RB-case":         {"Parent", "Kind"},
	"RB-go":           {"Parent", "Kind"},
	"RB-defer":        {"Parent", "Kind"},
	"RL-var":          {"Kind", "Names", "Type", "Values", "Block", "Span"},
	"RL-define":       {"Kind", "Names", "Values", "Block", "Span"},
	"RL-assign":       {"Kind", "Names", "Values", "Block", "Span"},
	"RL-const":        {"Kind", "Names", "Values", "Block", "Span"},
	"RL-range":        {"Kind", "Names", "Values", "Block", "Span"},
	"RL-range_assign": {"Kind", "Names", "Values", "Block", "Span"},
	"RL-typeswitch":   {"Kind", "Names", "Values", "Block", "Span"},
	"RL-handler":      {"Kind", "Names", "Values", "Block", "Span"},
	"RL-D":            nil,
	"RC-router":       {"Kind", "Call", "Block", "Span", "Hash", "Context"},
	"RC-call":         {"Kind", "Call", "Block", "Span", "Context"},
	"RC-serve":        {"Kind", "Call", "Block", "Span", "Context"},
	"RC-generated":    {"Kind", "Call", "Block", "Span", "Context"},
	"RC-excluded":     nil,
	"RC-D":            nil,
	"RR-return":       {"Values", "Block", "Span"},
	"RJ-jump":         {"Jumps"},
	"RE-funclit":      {"Kind", "Func"},
	"RE-closure":      {"Kind", "Uses"},
	"RE-forward":      {"Forward"},
	"RE-request":      {"Request"},
	"RU-escape":       {"Form", "Value", "Detail", "Type", "Block", "Span"},
	"RU-context":      {"Context"},
	"RE-forward-D":    nil,
	"RE-text":         {"Text"},
	"RV-var":          {"Names", "Type", "Values", "Line"},
	"RV-D":            nil,
	"RK-const":        {"Name", "Type", "Value", "Line"},
	"RK-D":            nil,
	"RS-signature":    {"Name", "Result", "Line"},
	"RS-D":            nil,
	"RD-debug":        {"Debug"},
	"RN-names":        {"Names"},
	"RN-results":      {"Results"},
	"RO-server":       {"Generator", "Version", "Framework"},
	"RO-D":            nil,
	"RW-wrapper":      {"Name", "Receiver", "Param", "Context", "Passes", "Methods", "Guards", "Line"},
	"RW-D":            nil,
	"RQ-request":      {"Name", "Receiver", "Param", "Passes", "URL", "Method", "Line"},
	"RQ-D":            nil,
}

const chiImport = "package p\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/go-chi/chi/v5\"\n)\n\n"

// routeIn wraps statements in a router function's if branch, so that every
// record they produce has a non-zero block.
func routeIn(stmts string) string {
	return chiImport + "func f(r chi.Router, rs []chi.Router, v any) {\n\tif true {\n" + stmts + "\n\t}\n}\n"
}

// goRouteSnippets holds one source per row; the first record the capture
// tags with the row must fill exactly the row's fields. Together they emit
// every row of the table.
var goRouteSnippets = map[string]string{
	"RG-chi":          chiImport,
	"RG-gin":          "package p\n\nimport \"github.com/gin-gonic/gin\"\n",
	"RG-http":         "package p\n\nimport \"net/http\"\n\nvar m *http.ServeMux\n",
	"RG-D":            "package p\n\nimport \"net/http\"\n\nfunc h(w http.ResponseWriter, r *http.Request) {}\n",
	"RF-decl":         chiImport + "func (s *S) Routes(r chi.Router) (chi.Router, error) {\n\tsub := chi.NewRouter()\n\tr.Mount(\"/x\", sub)\n\treturn sub, nil\n}\n",
	"RF-literal":      chiImport + "func f() {\n\tif true {\n\t\t_ = func(r chi.Router) error {\n\t\t\tsub := chi.NewRouter()\n\t\t\tr.Mount(\"/x\", sub)\n\t\t\treturn nil\n\t\t}\n\t}\n}\n",
	"RF-D":            chiImport + "func g() {}\n",
	"RB-body":         chiImport + "func f(r chi.Router) {}\n",
	"RB-block":        routeIn("\t\t{\n\t\t}"),
	"RB-if":           chiImport + "func f(r chi.Router) {\n\tfor {\n\t\tif true {\n\t\t}\n\t}\n}\n",
	"RB-then":         chiImport + "func f(r chi.Router) {\n\tif true {\n\t}\n}\n",
	"RB-else":         chiImport + "func f(r chi.Router) {\n\tif true {\n\t} else {\n\t}\n}\n",
	"RB-for":          routeIn("\t\tfor {\n\t\t}"),
	"RB-switch":       routeIn("\t\tswitch {\n\t\t}"),
	"RB-case":         routeIn("\t\tswitch {\n\t\tcase true:\n\t\t}"),
	"RB-go":           routeIn("\t\tgo g()"),
	"RB-defer":        routeIn("\t\tdefer g()"),
	"RL-var":          routeIn("\t\tvar x chi.Router = r\n\t\tx.Get(\"/\", h)"),
	"RL-define":       routeIn("\t\tx := r\n\t\tx.Get(\"/\", h)"),
	"RL-assign":       routeIn("\t\tx = r\n\t\tx.Get(\"/\", h)"),
	"RL-const":        routeIn("\t\tconst p = \"/x\"\n\t\tr.Get(p, h)"),
	"RL-range":        routeIn("\t\tfor _, x := range rs {\n\t\t\tx.Get(\"/\", h)\n\t\t}"),
	"RL-range_assign": routeIn("\t\tfor _, x = range rs {\n\t\t\tx.Get(\"/\", h)\n\t\t}"),
	"RL-typeswitch":   routeIn("\t\tswitch x := v.(type) {\n\t\tcase chi.Router:\n\t\t\tx.Get(\"/\", h)\n\t\t}"),
	"RL-D":            routeIn("\t\ty := 1\n\t\ty += 2"),
	"RC-router":       routeIn("\t\tr.Get(\"/\", h)"),
	"RC-call":         routeIn("\t\tregister(r)"),
	"RC-serve":        routeIn("\t\thttp.ListenAndServe(\":80\", http.StripPrefix(\"/x\", r))"),
	"RC-excluded":     "package p\n\nimport (\n\t\"log/slog\"\n\t\"net/http\"\n\n\t\"github.com/go-chi/chi/v5\"\n)\n\nfunc f(r chi.Router, w http.ResponseWriter, req *http.Request) {\n\t_ = req.URL.Query().Get(\"x\")\n\tslog.Any(\"k\", 1)\n}\n",
	"RC-D":            routeIn("\t\tfmt.Println(\"x\")"),
	"RR-return":       chiImport + "func f(r chi.Router) chi.Router {\n\tif true {\n\t\treturn r\n\t}\n\treturn nil\n}\n",
	"RJ-jump":         chiImport + "func f(r chi.Router) {\n\tgoto end\n\tr.Use(mw)\nend:\n\tr.Get(\"/\", h)\n}\n",
	"RE-funclit":      routeIn("\t\tr.Route(\"/a\", func(r chi.Router) {})"),
	"RE-closure":      routeIn("\t\tr.Get(\"/\", func(w http.ResponseWriter, q *http.Request) { r.ServeHTTP(w, q) })"),
	"RE-forward":      routeIn("\t\tm.HandleFunc(\"/\", func(w http.ResponseWriter, q *http.Request) {\n\t\t\tswitch q.URL.Path {\n\t\t\tcase \"/debug\":\n\t\t\t\treturn\n\t\t\t}\n\t\t\tr.ServeHTTP(w, q)\n\t\t})"),
	"RE-request":      routeIn("\t\tr.Get(\"/\", func(w http.ResponseWriter, q *http.Request) { r.ServeHTTP(w, q) })"),
	"RU-escape":       routeIn("\t\t_ = T{H: r}"),
	"RU-context":      routeIn("\t\tregister(r)"),
	"RE-forward-D":    routeIn("\t\tr.Route(\"/a\", func(r chi.Router) {})"),
	"RE-text":         routeIn("\t\tr.Get(\"/\", h)"),
	"RV-var":          chiImport + "var router chi.Router = chi.NewRouter()\n",
	"RV-D":            chiImport + "var n = 1\n",
	"RK-const":        chiImport + "const prefix string = \"/api\"\n",
	"RK-D":            chiImport + "const n = 1\n",
	"RS-signature":    "package p\n\nfunc New() *Handler { return nil }\n",
	"RS-D":            "package p\n\nfunc f() (int, error) { return 0, nil }\n",
	"RD-debug":        "//go:debug httpmuxgo121=1\n\npackage main\n",
	"RN-results":      "package p\n\nfunc f() any { return keep(mk()) }\n",
	"RN-names":        "package p\n\nvar ctors = map[string]func() int{\"a\": mk}\n\nfunc f() int { return mk() }\n",
	"RW-wrapper": "package p\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/m/req\"\n)\n\n" +
		"func (m *M) W(name string, next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n" +
		"\t\tif r.Method == \"OPTIONS\" {\n\t\t\tr.URL.Path = \"/\"\n\t\t\treturn\n\t\t}\n\t\tif !req.Allowed(r) {\n\t\t\treturn\n\t\t}\n\t\treq.Log(r)\n\t\tnext.ServeHTTP(w, r.WithContext(r.Context()))\n\t\tr.URL.Path = \"/after\"\n\t})\n}\n",
	"RW-D":         "package p\n\nimport \"net/http\"\n\nfunc W(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tr.URL.Path = \"/x\"\n\t\tnext.ServeHTTP(w, r)\n\t})\n}\n",
	"RQ-request":   "package p\n\nimport \"net/http\"\n\nfunc (s *S) h(w http.ResponseWriter, r *http.Request) {\n\t_ = r.URL.Query().Get(\"q\") + r.Header.Get(\"h\") + r.Method\n\ts.log(r)\n}\n",
	"RQ-D":         "package p\n\nimport \"net/http\"\n\nfunc h(w http.ResponseWriter, r *http.Request) {\n\tr.Method = \"POST\"\n}\n",
	"RG-generated": "package p\n\nimport \"example.com/m/api\"\n\nfunc (s *S) Handler() any {\n\treturn api.Handler(s)\n}\n",
	"RC-generated": "package p\n\nimport (\n\t\"github.com/go-chi/chi/v5\"\n\n\t\"example.com/m/api\"\n)\n\nfunc f(s *S, r chi.Router) {\n\tif true {\n\t\tapi.HandlerWithOptions(s, api.ChiServerOptions{BaseURL: \"/v1\"})\n\t}\n}\n",
	"RO-server":    oapiSnippet("v2.8.1"),
	"RO-D":         oapiSnippet("v2.7.0"),
	"RL-handler":   "package p\n\nimport \"net/http\"\n\nfunc serve(srv *http.Server, mux *http.ServeMux) {\n\tif true {\n\t\tsrv.Handler = mux\n\t}\n}\n",
}

// oapiSnippet is a chi server oapi-codegen writes for a spec without
// operations, with the given version in its header.
func oapiSnippet(version string) string {
	return "// Package p provides primitives to interact with the openapi HTTP API.\n//\n// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version " + version + " DO NOT EDIT.\npackage p\n\n" +
		"import (\n\t\"net/http\"\n\n\t\"github.com/go-chi/chi/v5\"\n)\n\n" +
		oapiChiHandler + "\n\n" + oapiChiOptions + "\n\n" + oapiChiFromMux + "\n\n" + oapiChiFromMuxBase + "\n\n" + oapiChiWithOptions + "\n\n\t" + oapiChiReturn + "\n"
}

// goRouteTestFiles names the rows whose snippet is a _test.go file.
var goRouteTestFiles = map[string]bool{"RK-const": true, "RK-D": true}

func captureRouteTags(t *testing.T, file, source string) []goTag {
	t.Helper()
	var tags []goTag
	goRouteTag = func(row string, record any) { tags = append(tags, goTag{row, record}) }
	defer func() { goRouteTag = nil }()
	parseGo(t, file, source)
	return tags
}

// goRouteTable parses the "## Payload table" rows of docs/lang/go-routes.md.
func goRouteTable(t *testing.T) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile("../../../docs/lang/go-routes.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Payload table\n")
	if start < 0 {
		t.Fatal("no Payload table section")
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	rows := map[string]string{}
	var order []string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| id |") {
			continue
		}
		id := strings.TrimSpace(strings.SplitN(line[1:], "|", 2)[0])
		if _, dup := rows[id]; dup {
			t.Fatalf("duplicate row %s", id)
		}
		rows[id] = line
		order = append(order, id)
	}
	return rows, order
}

// TestGoRoutePayloadContract keeps the router payload table, goRouteRows and
// the capture branches 1:1: every row has an entry whose fields the row's
// payload cell lists, a snippet that fills exactly those fields, every
// branch tags a row of the table, and the snippets together emit every row.
func TestGoRoutePayloadContract(t *testing.T) {
	docs, order := goRouteTable(t)
	if len(order) != len(goRouteRows) {
		t.Errorf("payload table rows = %d, Go table %d", len(order), len(goRouteRows))
	}
	for _, id := range order {
		fields, ok := goRouteRows[id]
		if !ok {
			t.Errorf("row %s has no goRouteRows entry", id)
			continue
		}
		cells := strings.Split(docs[id], "|")
		cell := strings.TrimSpace(cells[len(cells)-2])
		if (cell == "nothing") != (len(fields) == 0) {
			t.Errorf("row %s: payload cell %q, Go fields %v", id, cell, fields)
		}
		if cell != "nothing" {
			listed := strings.Split(cell[strings.Index(cell, ":")+1:], ",")
			var names []string
			for _, l := range listed {
				names = append(names, strings.TrimSpace(l))
			}
			if !reflect.DeepEqual(names, fields) {
				t.Errorf("row %s: payload cell lists %v, Go fields %v", id, names, fields)
			}
		}
	}
	emitted := map[string]bool{}
	for _, id := range order {
		snippet, ok := goRouteSnippets[id]
		if !ok {
			t.Errorf("row %s has no snippet", id)
			continue
		}
		file := "a.go"
		if goRouteTestFiles[id] {
			file = "a_test.go"
		}
		found := false
		for _, tag := range captureRouteTags(t, file, snippet) {
			emitted[tag.row] = true
			if tag.row != id || found {
				continue
			}
			found = true
			if fields := goRouteRows[id]; len(fields) > 0 {
				exp := append([]string(nil), fields...)
				sort.Strings(exp)
				if got := goFilledFields(tag.record); !reflect.DeepEqual(got, exp) {
					t.Errorf("row %s: filled %v, want %v (%+v)", id, got, exp, tag.record)
				}
			}
		}
		if !found {
			t.Errorf("row %s: snippet emitted no %s tag", id, id)
		}
	}
	for _, id := range order {
		if !emitted[id] {
			t.Errorf("snippets emit no %s tag", id)
		}
	}
	for id := range emitted {
		if _, ok := docs[id]; !ok {
			t.Errorf("capture emitted tag %s, which is not a payload-table row", id)
		}
	}
	for id := range goRouteRows {
		if _, ok := docs[id]; !ok {
			t.Errorf("goRouteRows has %s, which is not a payload-table row", id)
		}
	}
}

// TestGoRouteChains checks the receiver chain and argument rules: a chain's
// inner calls belong to the outer router call, a router call inside another
// call's arguments is listed on its own, and a plain call inside a router
// call's arguments is not.
func TestGoRouteChains(t *testing.T) {
	f := parseGo(t, "r.go", "package p\n\nimport \"github.com/gin-gonic/gin\"\n\n"+
		"func Setup(g *gin.Engine) {\n\tg.Group(\"/a\").Use(mw).POST(\"/x\", h.X)\n\tRegister(g.Group(\"/b\"))\n\tg.GET(\"/y\", wrap(h.Y))\n}\n")
	var got []string
	for _, c := range f.Go.Routes.Funcs[0].Calls {
		got = append(got, c.Kind+" "+shape(c.Call))
	}
	want := []string{
		`router g.Group("/a").Use(mw).POST("/x", h.X)`,
		`call Register(g.Group("/b"))`,
		`router g.Group("/b")`,
		`router g.GET("/y", wrap(h.Y))`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	post := f.Go.Routes.Funcs[0].Calls[0].Call
	if post.Args[1].Text != "h.X" || post.Callee.Object.Callee.Object.Args[0].Text != `"/a"` {
		t.Fatalf("argument texts = %+v", post)
	}
}

// TestGoRouteScopes checks blocks, shadowing bindings and the router
// filter: a receiver bound to a composite literal or to a stdlib result is
// excluded, a range variable stays (receiver_unproven at link time), and
// every binding of a used name is kept.
func TestGoRouteScopes(t *testing.T) {
	f := parseGo(t, "r.go", "package p\n\nimport (\n\t\"net/url\"\n\n\t\"github.com/go-chi/chi/v5\"\n)\n\n"+
		"func f(base chi.Router, opts Options) {\n"+
		"\tr := opts.BaseRouter\n"+
		"\tif r == nil {\n\t\tr = chi.NewRouter()\n\t}\n"+
		"\treq := &Request{}\n\treq.Get(\"/x\")\n"+
		"\tu, _ := url.Parse(\"x\")\n\tu.Query().Get(\"k\")\n"+
		"\tfor _, x := range items {\n\t\tx.Get(\"/i\", h)\n\t}\n"+
		"\tr.Get(\"/a\", h)\n}\n")
	fn := f.Go.Routes.Funcs[0]
	var calls, bindings, blocks []string
	for _, c := range fn.Calls {
		calls = append(calls, shape(c.Call))
	}
	for _, b := range fn.Bindings {
		bindings = append(bindings, b.Kind+" "+strings.Join(b.Names, ","))
	}
	for _, b := range fn.Blocks {
		blocks = append(blocks, b.Kind)
	}
	wantCalls := []string{`x.Get("/i", h)`, `r.Get("/a", h)`}
	wantBindings := []string{"define r", "assign r", "range _,x"}
	wantBlocks := []string{"body", "if", "then", "for"}
	if !reflect.DeepEqual(calls, wantCalls) || !reflect.DeepEqual(bindings, wantBindings) || !reflect.DeepEqual(blocks, wantBlocks) {
		t.Fatalf("calls %q bindings %q blocks %q", calls, bindings, blocks)
	}
	if fn.Bindings[1].Block != 2 || fn.Calls[0].Block != 3 {
		t.Fatalf("blocks of r = and x.Get: %d %d", fn.Bindings[1].Block, fn.Calls[0].Block)
	}
	var routes facts.GoRoutes = *f.Go.Routes
	if len(routes.Funcs) != 1 {
		t.Fatalf("records = %d", len(routes.Funcs))
	}
}

// TestGoRouteFilterNewAndChains checks two router filter forms (found by
// step GR2): a local bound to new(http.ServeMux) (GR-K4) and a local bound
// to a chain of router child calls (r.With(a).With(b), GR-O2) may hold a
// router, so the calls on them and their bindings are kept.
func TestGoRouteFilterNewAndChains(t *testing.T) {
	f := parseGo(t, "r.go", "package p\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/go-chi/chi/v5\"\n)\n\n"+
		"func Mux() {\n\talloc := new(http.ServeMux)\n\talloc.HandleFunc(\"GET /alloc\", h)\n\tother := new(T)\n\tother.Get(\"/no\")\n}\n\n"+
		"func Chain(r chi.Router) {\n\tadmin := r.With(a).With(b)\n\tadmin.Get(\"/admin\", h)\n\tg := r.Group(nil).With(a)\n\tg.Get(\"/g\", h)\n}\n")
	var got []string
	for _, fn := range f.Go.Routes.Funcs {
		for _, b := range fn.Bindings {
			got = append(got, fn.Name+" "+b.Kind+" "+strings.Join(b.Names, ",")+" = "+shape(b.Values[0]))
		}
		for _, c := range fn.Calls {
			got = append(got, fn.Name+" "+c.Kind+" "+shape(c.Call))
		}
	}
	want := []string{
		`Mux define alloc = new(http.ServeMux)`,
		`Mux router alloc.HandleFunc("GET /alloc", h)`,
		`Chain define admin = r.With(a).With(b)`,
		`Chain define g = r.Group(nil).With(a)`,
		`Chain router admin.Get("/admin", h)`,
		`Chain router r.Group(nil).With(a)`,
		`Chain router r.With(a).With(b)`,
		`Chain router g.Get("/g", h)`,
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestGoRouteServingSyntax checks the serving syntax of step GR4 (row
// RC-serve and the binding rule): a serving call inside a standard-library
// call is listed once (the outer call), a standard-library call holding a
// StripPrefix call is listed, one in a binding's value is part of the
// binding, a module call inside a serving call is still listed on its
// own, bindings holding StripPrefix or an http.Server literal are kept
// with the names they use, and a file whose only router syntax is serving
// syntax passes the gate.
func TestGoRouteServingSyntax(t *testing.T) {
	f := parseGo(t, "main.go", "package main\n\nimport (\n\t\"log\"\n\t\"net/http\"\n\n\t\"example.com/app/api\"\n)\n\n"+
		"func main() {\n\tr := api.NewRouter()\n\tlog.Fatal(http.ListenAndServe(\":80\", cors(r)))\n}\n\n"+
		"func strip() {\n\tm := api.NewRouter()\n\th := http.StripPrefix(\"/v1\", http.StripPrefix(\"/v0\", m))\n\tsrv := &http.Server{Handler: h}\n\t_ = srv\n\tlog.Println(http.StripPrefix(\"/v2\", m))\n}\n")
	if f.Go.Routes == nil {
		t.Fatal("serving syntax does not pass the gate")
	}
	var got []string
	for _, fn := range f.Go.Routes.Funcs {
		for _, b := range fn.Bindings {
			got = append(got, fn.Name+" "+b.Kind+" "+strings.Join(b.Names, ",")+" = "+shape(b.Values[0]))
		}
		for _, c := range fn.Calls {
			got = append(got, fn.Name+" "+c.Kind+" "+shape(c.Call))
		}
	}
	want := []string{
		`main define r = api.NewRouter()`,
		`main call log.Fatal(http.ListenAndServe(":80", cors(r)))`,
		`main call cors(r)`,
		`strip define m = api.NewRouter()`,
		`strip define h = http.StripPrefix("/v1", http.StripPrefix("/v0", m))`,
		`strip define srv = &http.Server{Handler: h}`,
		`strip call log.Println(http.StripPrefix("/v2", m))`,
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestGoRouteSignaturesAndDebug checks the ungated fields: single named
// results in every non-test file, the httpmuxgo121 directive lines, and
// nothing router-related in a file the gate refuses.
func TestGoRouteSignaturesAndDebug(t *testing.T) {
	f := parseGo(t, "main.go", "//go:debug httpmuxgo121=1\n//go:debug panicnil=1\n\npackage main\n\n"+
		"func NewH() *H { return nil }\nfunc NewP() pkg.T { return pkg.T{} }\nfunc Err() error { return nil }\nfunc Two() (a, b *H) { return }\nfunc (h *H) M() *H { return h }\n")
	if !reflect.DeepEqual(f.Go.Debug, []string{"//go:debug httpmuxgo121=1"}) || f.Go.Routes != nil {
		t.Fatalf("debug %q routes %+v", f.Go.Debug, f.Go.Routes)
	}
	var sigs []string
	for _, s := range f.Go.Signatures {
		sigs = append(sigs, s.Name+" "+shape(s.Result))
	}
	if !reflect.DeepEqual(sigs, []string{"NewH *H", "NewP pkg.T"}) {
		t.Fatalf("signatures = %q", sigs)
	}
	test := parseGo(t, "main_test.go", "package main\n\nfunc NewH() *H { return nil }\n")
	if test.Go.Signatures != nil {
		t.Fatalf("test file signatures = %+v", test.Go.Signatures)
	}
}

// TestGoRoutePayloadValidity checks that damaged router payloads are cache
// misses.
func TestGoRoutePayloadValidity(t *testing.T) {
	f := parseGo(t, "r.go", routeIn("\t\tr.Route(\"/a\", func(r chi.Router) { r.Get(\"/b\", h) })"))
	if len(f.Go.Routes.Funcs) != 2 || f.Go.Routes.Funcs[1].Parent != 1 {
		t.Fatalf("records = %+v", f.Go.Routes.Funcs)
	}
	damage := map[string]func(g *facts.GoFacts){
		"block kind":    func(g *facts.GoFacts) { g.Routes.Funcs[0].Blocks[1].Kind = "loop" },
		"block parent":  func(g *facts.GoFacts) { g.Routes.Funcs[0].Blocks[1].Parent = 5 },
		"no body":       func(g *facts.GoFacts) { g.Routes.Funcs[0].Blocks = nil },
		"parent order":  func(g *facts.GoFacts) { g.Routes.Funcs[1].Parent = 2 },
		"parent block":  func(g *facts.GoFacts) { g.Routes.Funcs[1].Block = 99 },
		"call kind":     func(g *facts.GoFacts) { g.Routes.Funcs[1].Calls[0].Kind = "x" },
		"router hash":   func(g *facts.GoFacts) { g.Routes.Funcs[1].Calls[0].Hash = "" },
		"call span":     func(g *facts.GoFacts) { g.Routes.Funcs[1].Calls[0].Span.StartLine = 0 },
		"funclit field": func(g *facts.GoFacts) { g.Routes.Funcs[0].Calls[0].Call.Args[0].Func = 1 },
		"signature":     func(g *facts.GoFacts) { g.Signatures = []facts.GoSignature{{Name: "F"}} },
		"debug":         func(g *facts.GoFacts) { g.Debug = []string{""} },
	}
	for name, change := range damage {
		g := cloneGoFacts(t, f.Go)
		change(g)
		if g.ModelsValid() {
			t.Errorf("%s: damaged payload is valid", name)
		}
	}
}

// cloneGoFacts deep-copies a payload through its JSON form, which also
// checks that it round-trips.
func cloneGoFacts(t *testing.T, g *facts.GoFacts) *facts.GoFacts {
	t.Helper()
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var out facts.GoFacts
	if err := json.Unmarshal(data, &out); err != nil || !reflect.DeepEqual(&out, g) {
		t.Fatalf("payload does not round-trip: %v", err)
	}
	return &out
}
