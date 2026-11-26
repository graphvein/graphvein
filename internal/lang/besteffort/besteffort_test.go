package besteffort

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func TestSharedTableMatchesDocument(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/lang/best-effort.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := DocTable(string(doc), "Shared"); !reflect.DeepEqual(got, Shared) {
		t.Fatalf("docs/lang/best-effort.md Shared table differs from besteffort.Shared:\n doc: %q\n go:  %q", got, Shared)
	}
}

func TestDocTable(t *testing.T) {
	doc := "# T\n\n## A\n\n| Row | Form | Claim |\n|---|---|---|\n| X-1 | `f` | `c` one |\n| X-2 | g | two |\n\nText.\n\n## B\n\n| Row | Claim |\n|---|---|\n| Y | three |\n"
	if got := DocTable(doc, "A"); !reflect.DeepEqual(got, Table{{ID: "X-1", Claim: "c one"}, {ID: "X-2", Claim: "two"}}) {
		t.Fatal(got)
	}
	if got := DocTable(doc, "B"); !reflect.DeepEqual(got, Table{{ID: "Y", Claim: "three"}}) {
		t.Fatal(got)
	}
	if got := DocTable(doc, "C"); got != nil {
		t.Fatal(got)
	}
}

func node(kind string, start, end uint32, children ...*parse.Node) *parse.Node {
	return &parse.Node{Kind: kind, Start: start, End: end, Children: children}
}

func TestRecovered(t *testing.T) {
	clean := node("program", 0, 10, node("class_declaration", 0, 10, node("identifier", 6, 7)))
	if Recovered(clean) || Recovered(nil) || Recovered(node("program", 0, 0)) {
		t.Fatal("clean tree refused")
	}
	if !Recovered(node("program", 0, 10, node("class_declaration", 0, 10, node("ERROR", 3, 5)))) {
		t.Fatal("ERROR node accepted")
	}
	if !Recovered(node("program", 0, 10, node("class_declaration", 0, 10, node("identifier", 6, 6)))) {
		t.Fatal("named MISSING node accepted")
	}
}

func TestRefuseKeepsDiagnosticsOnly(t *testing.T) {
	f := facts.Facts{Exports: map[string]string{"A": "class:A.java#A"}, BestEffort: &facts.BestEffortFacts{Version: facts.BestEffortVersion, Package: "p", Types: []string{"A"}}}
	f.Nodes = []contract.Node{{Type: "node", Identity: "class:A.java#A"}}
	f.Edges = []contract.Edge{{Type: "edge", Kind: "contains"}}
	f.Diagnostics = []contract.Diagnostic{{Type: "diagnostic", Code: "syntax_error"}}
	got := Refuse(f, Scope{})
	if len(got.Diagnostics) != 1 || len(got.Exports) != 0 || got.Nodes != nil || got.Edges != nil || !got.BestEffort.Valid() || !got.BestEffort.Refused || got.BestEffort.Package != "" || got.BestEffort.Types != nil || got.BestEffort.Scoped {
		t.Fatalf("%+v", got)
	}
	if got := Refuse(f, Scope{Known: true, Names: []string{"a.b"}}); !got.BestEffort.Scoped || !reflect.DeepEqual(got.BestEffort.Scopes, []string{"a.b"}) {
		t.Fatalf("scope not kept: %+v", got.BestEffort)
	}
}

// TestName covers BE-6 (an overload set gets type lists, a lone method
// keeps its name) and BE-5 (repeated identities per kind get @2, @3; owners
// carry their suffixes into their members' names).
func TestName(t *testing.T) {
	m := func(name, overload string, children ...*Decl) *Decl {
		return &Decl{Kind: "method", Name: name, Overloadable: true, Overload: overload, Children: children}
	}
	c := func(name string, children ...*Decl) *Decl {
		return &Decl{Kind: "class", Name: name, Children: children}
	}
	roots := []*Decl{c("A",
		m("run", "(int)", c("Local"), c("Local")),
		m("run", "(String)"),
		m("run", "(String)"),
		m("lone", "()"),
		c("run"),
	), c("A")}
	Name(roots)
	var got []string
	var visit func([]*Decl)
	visit = func(list []*Decl) {
		for _, d := range list {
			got = append(got, d.Kind+" "+d.Qualified())
			visit(d.Children)
		}
	}
	visit(roots)
	want := []string{
		"class A",
		"method A.run@overload(int)",
		"class A.run@overload(int).Local",
		"class A.run@overload(int).Local@2",
		"method A.run@overload(String)",
		"method A.run@overload(String)@2",
		"method A.lone",
		"class A.run",
		"class A@2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestTypeTextAndCover(t *testing.T) {
	source := []byte("java . util /* c */ . @A List < String >")
	n := node("t", 0, uint32(len(source)), node("block_comment", 12, 19), node("marker_annotation", 22, 24))
	if got := TypeText(source, n, n.Start, n.End); got != "java.util.List<String>" {
		t.Fatalf("%q", got)
	}
	first := &parse.Node{Kind: "annotation", Start: 2, End: 4, Row: 0, Column: 2}
	last := &parse.Node{Kind: "function_body", Start: 10, End: 12, EndRow: 1, EndColumn: 3}
	got := Cover(source, first, last)
	if got.Start != 2 || got.End != 12 || got.Text != string(source[2:12]) || got.EndRow != 1 || got.EndColumn != 3 {
		t.Fatalf("%+v", got)
	}
}

// TestNameSuffix: a discriminator is part of the segment, so it keeps a
// member out of a same-named overload set (BE-6) and owners carry it.
func TestNameSuffix(t *testing.T) {
	roots := []*Decl{{Kind: "class", Name: "R"}, {Kind: "class", Name: "R", Suffix: "`1", Children: []*Decl{
		{Kind: "method", Name: "M", Overloadable: true, Overload: "()"},
		{Kind: "method", Name: "M", Suffix: "@I", Overloadable: true, Overload: "()"},
		{Kind: "method", Name: "N", Suffix: "@I", Overloadable: true, Overload: "(int)"},
		{Kind: "method", Name: "N", Suffix: "@I", Overloadable: true, Overload: "()"},
	}}}
	Name(roots)
	var got []string
	for _, d := range append(roots, roots[1].Children...) {
		got = append(got, d.Qualified())
	}
	want := []string{"R", "R`1", "R`1.M", "R`1.M@I", "R`1.N@I@overload(int)", "R`1.N@I@overload()"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

// TestHidden: refused files and files without a valid payload are hidden
// (BE-8); Hiding keeps the unscoped ones and the scoped ones whose scope
// may declare the name, and for a name that may be a type the scoped ones
// that may also declare global-namespace types; HiddenImport names the
// first and counts the rest.
func TestHidden(t *testing.T) {
	files := map[string]facts.Facts{
		"a.cs": {BestEffort: &facts.BestEffortFacts{Version: facts.BestEffortVersion}},
		"b.cs": Refuse(facts.Facts{}, Scope{}),
		"c.cs": {},
		"d.cs": {BestEffort: &facts.BestEffortFacts{Version: facts.BestEffortVersion + 1}},
		"e.cs": Refuse(facts.Facts{}, Scope{Known: true, Names: []string{"A.B"}}),
		"f.cs": Refuse(facts.Facts{}, Scope{Known: true}),
		"g.cs": Refuse(facts.Facts{}, Scope{Known: true, Names: []string{"N"}, Global: true}),
	}
	hidden := Hidden(files)
	var paths []string
	for _, h := range hidden {
		paths = append(paths, h.File)
	}
	if !reflect.DeepEqual(paths, []string{"b.cs", "c.cs", "d.cs", "e.cs", "f.cs", "g.cs"}) {
		t.Fatal(paths)
	}
	for _, c := range []struct {
		name            string
		prefixes, typed bool
		want            []string
	}{
		{"A.B", false, false, []string{"b.cs", "c.cs", "d.cs", "e.cs"}},
		{"A.B.C", false, false, []string{"b.cs", "c.cs", "d.cs", "e.cs"}},
		{"A.BC", false, false, []string{"b.cs", "c.cs", "d.cs"}},
		{"A", false, false, []string{"b.cs", "c.cs", "d.cs"}},
		{"A", true, false, []string{"b.cs", "c.cs", "d.cs", "e.cs"}},
		{"A", true, true, []string{"b.cs", "c.cs", "d.cs", "e.cs", "g.cs"}},
		{"N.M", true, false, []string{"b.cs", "c.cs", "d.cs", "g.cs"}},
	} {
		if got := Hiding(hidden, c.name, c.prefixes, c.typed); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Hiding(%s, %v, %v) = %v", c.name, c.prefixes, c.typed, got)
		}
	}
	d := HiddenImport("a.cs", "using System", Hiding(hidden, "System", false, false))
	if d.File != "a.cs" || d.Severity != "warning" || d.Code != "unresolved_import" || !strings.HasSuffix(d.Message, ": b.cs and 2 more") || !strings.HasPrefix(d.Message, "using System ") {
		t.Fatalf("%+v", d)
	}
	if Hidden(map[string]facts.Facts{"a.cs": files["a.cs"]}) != nil {
		t.Fatal("readable file hidden")
	}
}

// TestOccurrences: whole words only (not after '@' or inside a longer
// identifier), the dotted name read across whitespace and comments, and the
// byte after it.
func TestOccurrences(t *testing.T) {
	source := "package a . /* c */ b ;\n@package x; repackage y; packages z;\n// package\n/* package q.r */\npackage\u00e9;\npackage s. ;"
	got := Occurrences([]byte(source), "package")
	// The comment's `package` reads past two comments into the next line
	// (an over-match, which only hides more); `package\u00e9` is one word.
	want := []Occurrence{{Name: "a.b", Next: ';'}, {Name: "package\u00e9", Next: ';'}, {Name: "q.r", Next: '*'}, {Name: "s", Next: '.'}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
