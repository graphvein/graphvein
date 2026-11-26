package tableaccess

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// The Drizzle toSQL() fixture oracle (Revision 1, O6; Revision 4). Never
// run by CI. For every claimed CT-Z1/CT-Z2 site of a postgresql group,
// TestSQLTablesOracle (phase requests) writes the builder's source text
// and the bindings it may reference; scripts/oracle/sql_tables_drizzle.mjs
// evaluates the text against drizzle-orm's pg-proxy database (no
// connection) at the corpora's pin, writes builder.toSQL()'s SQL, and
// sql_tables.sh runs it through the Postgres analyzer like any SQL site,
// on the schema drizzle-kit export builds for the group.

// toSQLBinding is one name a builder text may reference: the database
// receiver ("db"), a table const of the group ("table": its declaring
// file and declaration name), a namespace import ("namespace"), a
// drizzle-orm import ("external"), a binding's source text ("expr"),
// anything else a placeholder value ("value").
type toSQLBinding struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	File   string `json:"file,omitempty"`
	Export string `json:"export,omitempty"`
	Module string `json:"module,omitempty"`
	Text   string `json:"text,omitempty"`
	// Properties are an object binding's table properties.
	Properties []toSQLBinding `json:"properties,omitempty"`
}

type toSQLRequest struct {
	Key      string         `json:"key"`
	Text     string         `json:"text"`
	Bindings []toSQLBinding `json:"bindings"`
}

// toSQLSite reports a site the toSQL oracle compares: a claimed CT-Z1 or
// CT-Z2 builder labelled oracle.
func toSQLSite(s site) bool {
	return (s.row == "CT-Z1" || s.row == "CT-Z2") && s.builder != "" && s.verification == VerificationOracle
}

// receiverRoot is the root identifier of a builder text's receiver.
func receiverRoot(e facts.RouteExpr) string {
	recv, _ := callChain(e)
	for recv.Kind == "member_expression" && recv.Object != nil {
		recv = *recv.Object
	}
	if recv.Kind == "identifier" {
		return recv.Name
	}
	return ""
}

// toSQLBindings lists every binding visible from a site's scope, nearest
// first.
func toSQLBindings(l *drizzleLinker, s site) []toSQLBinding {
	f := l.in.TypeScript[s.file]
	root := ""
	for _, c := range f.Routes.Clients {
		if c.Span == s.span {
			root = receiverRoot(c.Expression)
		}
	}
	seen := map[string]bool{}
	var out []toSQLBinding
	scopes := f.Routes.Scopes
	for sc := s.scope; sc >= 0 && sc < len(scopes); sc = scopes[sc].Parent {
		var names []string
		for name := range scopes[sc].Bindings {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			b := scopes[sc].Bindings[name]
			out = append(out, toSQLBindingOf(l, s.file, sc, name, b, root))
		}
		if scopes[sc].Parent == sc {
			break
		}
	}
	return out
}

func toSQLBindingOf(l *drizzleLinker, file string, scope int, name string, b facts.RouteBinding, root string) toSQLBinding {
	if name == root {
		return toSQLBinding{Name: name, Kind: "db"}
	}
	if drizzleORMModule(b.Module) {
		return toSQLBinding{Name: name, Kind: "external", Module: b.Module, Export: b.Export}
	}
	r := l.resolve(file, scope, facts.RouteExpr{Kind: "identifier", Name: name, Text: name}, 0)
	switch {
	case r.table != nil && r.column == "" && r.alias == "":
		return toSQLBinding{Name: name, Kind: "table", File: r.table.file, Export: r.table.decl}
	case r.namespace != "":
		return toSQLBinding{Name: name, Kind: "namespace", File: r.namespace}
	case r.object != nil:
		out := toSQLBinding{Name: name, Kind: "object"}
		var keys []string
		for k := range r.object.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := l.resolve(r.objectFile, r.objectScope, r.object.Properties[k], 0)
			if p.table != nil && p.column == "" && p.alias == "" {
				out.Properties = append(out.Properties, toSQLBinding{Name: k, Kind: "table", File: p.table.file, Export: p.table.decl})
			}
		}
		return out
	case b.Module == "" && b.Value.Kind != "" && b.Value.Kind != "parameter" && b.Value.Kind != "function" && b.Value.Text != "":
		return toSQLBinding{Name: name, Kind: "expr", Text: b.Value.Text}
	}
	return toSQLBinding{Name: name, Kind: "value"}
}

func writeToSQLRequest(t *testing.T, path string, l *drizzleLinker, s site) {
	t.Helper()
	data, err := json.MarshalIndent(toSQLRequest{Key: siteKey(s), Text: s.builder, Bindings: toSQLBindings(l, s)}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data)+"\n")
}

// nestedClaim is what the toSQL comparison expects of a builder site: its
// claims and those of the sites nested in its span, and whether the
// comparison is a subset one.
type nestedClaim struct {
	reads, writes []string
	subset        bool
}

// nestedClaims computes the expectations of every toSQL site.
func nestedClaims(a analysis) map[string]nestedClaim {
	out := map[string]nestedClaim{}
	for _, s := range a.sites {
		if !toSQLSite(s) {
			continue
		}
		reads, writes := map[string]bool{}, map[string]bool{}
		subset := s.unknown != nil && s.unknown.Partial || len(s.extra) > 0
		add := func(x site) {
			for id := range x.reads {
				reads[x.tables[id]] = true
			}
			for id := range x.writes {
				writes[x.tables[id]] = true
			}
		}
		add(s)
		for _, x := range a.sites {
			if x.file != s.file || x.span == s.span || !contains(s.span, x.span) {
				continue
			}
			add(x)
			subset = subset || x.unknown != nil || len(x.extra) > 0 || len(x.reads)+len(x.writes) == 0 || x.tables == nil
		}
		out[siteKey(s)] = nestedClaim{reads: sortedSet(reads), writes: sortedSet(writes), subset: subset}
	}
	return out
}

func subset(small, large []string) bool {
	for _, x := range small {
		if !contains2(large, x) {
			return false
		}
	}
	return true
}

// TestDrizzleToSQLRequests keeps the requests phase working on the
// fixture: every claimed CT-Z1/CT-Z2 site has a builder text, its
// receiver is bound to the database and every table it names is bound to
// the table's declaration.
func TestDrizzleToSQLRequests(t *testing.T) {
	in := loadRoot(t, filepath.FromSlash(drizzleFixtureRoot))
	a := analyze(in)
	l := newDrizzleLinker(in)
	n := 0
	for _, s := range a.sites {
		if !toSQLSite(s) {
			continue
		}
		n++
		kinds := map[string]bool{}
		for _, b := range toSQLBindings(l, s) {
			kinds[b.Kind] = true
			if b.Kind == "table" && (b.File == "" || b.Export == "") {
				t.Errorf("%s: table binding %+v", siteKey(s), b)
			}
		}
		if !strings.Contains(s.builder, "(") || !kinds["db"] {
			t.Errorf("%s: builder %q, binding kinds %v", siteKey(s), s.builder, kinds)
		}
	}
	if n == 0 {
		t.Fatal("no claimed CT-Z1/CT-Z2 site in the fixture")
	}
}
