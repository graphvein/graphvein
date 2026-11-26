package routes

import (
	"sort"
	"strings"
	"testing"
)

// goTemplateCases are GR-P2's template normalizations.
var goTemplateCases = []struct {
	framework, raw, template string
	trailing, ok             bool
}{
	{goGin, "/api/users/:id", "/api/users/{id}", false, true},
	{goGin, "/static/*filepath", "/static/{*filepath}", false, true},
	{goGin, "/api//version/", "/api/version", true, true},
	{goChi, "/files/{id}", "/files/{id}", false, true},
	{goChi, "/files/{id:[0-9]+}/raw", "/files/{id}/raw", false, true},
	{goChi, "/date/{y:\\d{4}}-{m}", "/date/{y}-{m}", false, true},
	{goChi, "/assets/*", "/assets/{*}", false, true},
	{goChi, "/raw/{path:.*}", "", false, false},
	{goChi, "/x/{p:[^/]+}", "/x/{p}", false, true},
	{goChi, "/y/{p:a|b/c}", "", false, false},
	{goChi, "/api/nested/", "/api/nested", true, true},
	{goNetHTTP, "/files/{path...}", "/files/{*path}", false, true},
	{goNetHTTP, "/a/{$}", "/a", true, true},
	{goNetHTTP, "/{$}", "/", false, true},
	{goNetHTTP, "/tree/", "/tree/{*}", false, true},
	{goNetHTTP, "/", "/{*}", false, true},
	{goNetHTTP, "/items/{id}", "/items/{id}", false, true},
}

func TestGoTemplate(t *testing.T) {
	for _, c := range goTemplateCases {
		template, trailing, ok := goTemplate(c.framework, c.raw)
		if template != c.template || trailing != c.trailing || ok != c.ok {
			t.Errorf("%s %q: %q %v %v, want %q %v %v", c.framework, c.raw, template, trailing, ok, c.template, c.trailing, c.ok)
		}
	}
}

func TestGoModuleSettings(t *testing.T) {
	for text, legacy := range map[string]bool{
		"module m\n\ngo 1.22\n":   false,
		"module m\n\ngo 1.21.5\n": true,
		"module m\n":              true,
		"module m\n\ngo 1.24\n\ngodebug httpmuxgo121=1\n":         true,
		"module m\n\ngo 1.24\n\ngodebug (\n\tdefault=go1.21\n)\n": true,
		"module m\n\ngo 1.24\n\ngodebug (\n\tpanicnil=1\n)\n":     false,
		"module m\n\ngo 1.100\n":                                  false,
		"module m\n\ngo 1.3\n":                                    true,
	} {
		if got := readGoMod(text).legacy; got != legacy {
			t.Errorf("%q: legacy %v, want %v", text, got, legacy)
		}
	}
	info := readGoMod("module m\n\ngo 1.24\n\nrequire (\n\tgithub.com/go-chi/chi/v5 v5.2.4 // indirect\n\tgithub.com/gin-gonic/gin v1.9.1\n)\n")
	if info.versions[goChi] != "v5.2.4" || info.versions[goGin] != "v1.9.1" {
		t.Errorf("versions %v", info.versions)
	}
}

func TestGoRegexpSlash(t *testing.T) {
	for re, slash := range map[string]bool{"[0-9]+": false, ".*": true, "[^/]+": false, "[^a]": true, "[a-z-]+": false, "a/b": true, "\\d{4}": false, "(": true} {
		if got := goRegexpSlash(re); got != slash {
			t.Errorf("%q: %v, want %v", re, got, slash)
		}
	}
}

// TestGoRoutesMainModuleDebug checks GR-V2's //go:debug reading: a main
// package of the module that sets httpmuxgo121=1 refuses net/http routes.
func TestGoRoutesMainModuleDebug(t *testing.T) {
	src := map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.24\n",
		"cmd/main.go": "//go:debug httpmuxgo121=1\n\npackage main\n\nfunc main() {}\n",
		"web/web.go":  "package web\n\nimport \"net/http\"\n\nfunc h(w http.ResponseWriter, r *http.Request) {}\n\nfunc Routes() {\n\tmux := http.NewServeMux()\n\tmux.HandleFunc(\"GET /x\", h)\n}\n",
	}
	nodes, _, diagnostics := goLink(t, src, nil)
	if len(nodes) != 0 {
		t.Errorf("legacy module: %d routes", len(nodes))
	}
	found := false
	for _, d := range diagnostics {
		found = found || d.Code == "go_routes_unread" && strings.Contains(d.Message, "legacy_mux_patterns=1")
	}
	if !found {
		t.Errorf("no legacy_mux_patterns count: %v", diagnostics)
	}
	delete(src, "cmd/main.go")
	if nodes, _, _ := goLink(t, src, nil); len(nodes) != 1 {
		t.Errorf("module without the setting: %d routes, want 1", len(nodes))
	}
}

// TestGoRoutesShadowedImport checks GR-K1's shadowing: a scanned module
// with chi's import path is the module's own package, not chi.
func TestGoRoutesShadowedImport(t *testing.T) {
	src := map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.24\n",
		"chi/go.mod": "module github.com/go-chi/chi/v5\n\ngo 1.24\n",
		"chi/chi.go": "package chi\n\ntype Mux struct{}\n\nfunc NewRouter() *Mux { return &Mux{} }\n\nfunc (m *Mux) Get(p string, h any) {}\n",
		"web/web.go": "package web\n\nimport \"github.com/go-chi/chi/v5\"\n\nfunc Routes() {\n\tr := chi.NewRouter()\n\tr.Get(\"/x\", nil)\n}\n",
	}
	if nodes, _, _ := goLink(t, src, nil); len(nodes) != 0 {
		t.Errorf("shadowed chi: %d routes", len(nodes))
	}
}

// TestGoRoutesBindings checks GR-K3's reaching bindings: shadowing in a
// nested block, a loop-carried reassignment and a conditional
// reassignment with an unproven value.
func TestGoRoutesBindings(t *testing.T) {
	src := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.24\n",
		"web/web.go": `package web

import "net/http"

func h(w http.ResponseWriter, r *http.Request) {}

func other() *http.ServeMux { return nil }

func Routes(ok bool) {
	mux := http.NewServeMux()
	{
		mux := other()
		mux.HandleFunc("/shadowed", h)
	}
	mux.HandleFunc("/outer", h)
	for i := 0; i < 2; i++ {
		mux.HandleFunc("/loop", h)
		mux = other()
	}
	m2 := http.NewServeMux()
	if ok {
		m2 = http.NewServeMux()
	}
	m2.HandleFunc("/both", h)
}
`,
	}
	nodes, _, _ := goLink(t, src, nil)
	var got []string
	for _, n := range nodes {
		got = append(got, n.QualifiedName)
	}
	sort.Strings(got)
	want := "ANY /both,ANY /outer"
	if strings.Join(got, ",") != want {
		t.Errorf("routes %v, want %s", got, want)
	}
}
