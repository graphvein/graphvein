package java

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/besteffort/contracttest"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

const fixture = "../../../testdata/fixtures/java-best-effort"

func TestContractTableMatchesDocument(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/lang/best-effort.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := besteffort.DocTable(string(doc), "Java"); !reflect.DeepEqual(got, Rows) {
		t.Fatalf("docs/lang/best-effort.md Java table differs from java.Rows:\n doc: %q\n go:  %q", got, Rows)
	}
}

// TestBranchesMatchRows reads every row id string literal of the extractor
// and linker (parsed, so ids in comments do not count): each names a row of
// the Java or shared table, and each Java row has a branch. That every
// branch is also taken is TestFixtureCoversEveryRow's job.
func TestBranchesMatchRows(t *testing.T) {
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range all {
		if !strings.HasSuffix(f, "_test.go") && f != "java.go" {
			files = append(files, f)
		}
	}
	branches, err := contracttest.RowLiterals(files, "JV")
	if err != nil {
		t.Fatal(err)
	}
	for row := range branches {
		if !Rows.Has(row) && !besteffort.Shared.Has(row) {
			t.Errorf("branch tagged %s has no contract row", row)
		}
	}
	for _, row := range append(Rows.IDs(), "BE-2", "BE-8", "D") {
		if !branches[row] {
			t.Errorf("row %s has no branch", row)
		}
	}
}

func parseFile(t *testing.T, source []byte) parse.Result {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "java", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err, results)
	}
	return results[0]
}

// externalFixture is the second fixture root: it has no refused file, so its
// imports reach JV-I8, which row BE-8 blocks in the main fixture.
const externalFixture = "../../../testdata/fixtures/java-best-effort-external"

// TestFixtureCoversEveryRow extracts and links the fixtures the index goldens
// use (each root linked on its own, as the indexer does) and checks that
// their cases take every Java row and the shared rows the package
// implements.
func TestFixtureCoversEveryRow(t *testing.T) {
	seen := map[string]bool{}
	trace := tracer(func(row string) { seen[row] = true })
	for _, root := range []string{fixture, externalFixture} {
		files := map[string]facts.Facts{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".java") {
				return err
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			result := parseFile(t, source)
			f := extract(rel, source, result.Root, trace)
			if result.SyntaxError {
				f = besteffort.Refuse(f, RefusedScope(source))
			}
			files[rel] = f
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		link(files, trace)
	}
	var missing []string
	for _, row := range append(Rows.IDs(), "BE-2", "BE-8", "D") {
		if !seen[row] {
			missing = append(missing, row)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("fixtures miss rows %v", missing)
	}
}

// TestRecoveredTreeClaimsNothing: a member re-parented by recovery must not
// survive (row BE-2), and a MISSING-only tree is refused through Refuse.
func TestRecoveredTreeClaimsNothing(t *testing.T) {
	for name, source := range map[string]string{
		"error":   "package a;\nimport b.C;\nclass X extends {\n  void keep() {}\n}\n",
		"missing": "package a;\nimport b.C;\nclass X {\n  void m() { int x = 1 }\n}\n",
	} {
		result := parseFile(t, []byte(source))
		if !result.SyntaxError {
			t.Fatalf("%s: parsed without a syntax error", name)
		}
		f := Extract("X.java", []byte(source), result.Root)
		f = besteffort.Refuse(f, RefusedScope([]byte(source)))
		if len(f.Nodes)+len(f.Edges) != 0 || f.BestEffort == nil || !f.BestEffort.Refused || len(f.BestEffort.Imports) != 0 {
			t.Fatalf("%s: refused file claims %+v", name, f)
		}
	}
}

func TestOverloadTypes(t *testing.T) {
	source := []byte(`class A {
  A(final @X int a[], String ... rest) {}
  void m(@NonNull java.util.List<@N ? extends /* c */ T> x, int @A [] y) {}
  void m(A this) {}
  <U> void g(Map.Entry<K, V>[] e) {}
}
record R(int x, java.lang.String y) { R {} }
`)
	result := parseFile(t, source)
	if result.SyntaxError {
		t.Fatal("syntax error")
	}
	f := Extract("A.java", source, result.Root)
	names := map[string]bool{}
	for _, n := range f.Nodes {
		names[n.QualifiedName] = true
	}
	for _, want := range []string{"A", "A.A", "A.m@overload(java.util.List<?extendsT>,int[])", "A.m@overload()", "A.g", "R", "R.R"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	if got := parameters(source, findKind(result.Root, "constructor_declaration").Fields["parameters"]); got != "(int[],String...)" {
		t.Errorf("constructor types %q", got)
	}
	if got := parameters(source, findKind(result.Root, "record_declaration").Fields["parameters"]); got != "(int,java.lang.String)" {
		t.Errorf("record types %q", got)
	}
}

func findKind(n *parse.Node, kind string) *parse.Node {
	if n.Kind == kind {
		return n
	}
	for _, c := range n.Children {
		if found := findKind(c, kind); found != nil {
			return found
		}
	}
	return nil
}

func TestLinkRows(t *testing.T) {
	sources := map[string]string{
		"a/Order.java":   "package com.x;\npublic class Order { public static class Line {} }\n",
		"b/Twin.java":    "package com.d;\nclass Twin {}\n",
		"c/Twin.java":    "package com.d;\nclass Twin {}\n",
		"d/Local.java":   "package com.x;\nclass Local { void m() { class Hidden {} } }\n",
		"e/Use.java":     "package com.x;\nimport com.x.Order.Line;\nimport com.x.Local.Hidden;\nimport static com.x.Order.Line.*;\nimport com.d.Twin;\nimport com.x.*;\nimport com.x.y.Gen;\nimport java.util.List;\nimport com.x.Use;\nclass Use {}\n",
		"f/Default.java": "class Default {}\n",
	}
	files := map[string]facts.Facts{}
	for file, source := range sources {
		result := parseFile(t, []byte(source))
		if result.SyntaxError {
			t.Fatal(file)
		}
		files[file] = Extract(file, []byte(source), result.Root)
	}
	nodes, edges, diagnostics := Link(files)
	imports := map[string]int{}
	for _, e := range edges {
		if e.Kind == "imports" {
			imports[e.Target] = len(e.Evidence)
		}
	}
	want := map[string]int{"file:a/Order.java#a%2FOrder.java": 2, "external:.#java%3Ajava.util.List": 1}
	if !reflect.DeepEqual(imports, want) || len(nodes) != 1 {
		t.Fatalf("imports %v, nodes %v", imports, nodes)
	}
	codes := map[string]int{}
	for _, d := range diagnostics {
		codes[d.Code]++
	}
	// Hidden is a local class (not importable) under package com.x, and
	// com.x.y.Gen sits under com.x: both unresolved.
	if !reflect.DeepEqual(codes, map[string]int{"ambiguous_import": 1, "unresolved_import": 2}) {
		t.Fatalf("diagnostics %v", diagnostics)
	}
}

// TestHiddenFileBlocksExternals: row BE-8. While a Java file is failed
// (BE-3) or refused (BE-2) without a provable scope, an import that would be
// external is an unresolved_import warning, JDK names included; resolvable
// imports still resolve. A refused file with a package declaration hides
// only names in or under that package; one without hides nothing.
func TestHiddenFileBlocksExternals(t *testing.T) {
	use := "package com.x;\nimport com.x.Order;\nimport java.util.List;\nimport com.hidden.Gen;\nimport com.hidden.sub.Deep.Inner;\nimport com.hiddenx.Other;\nclass Use {}\n"
	order := "package com.x;\nclass Order {}\n"
	broken := func(text string) facts.Facts {
		return besteffort.Refuse(facts.Facts{}, RefusedScope([]byte(text)))
	}
	all := []string{"import java.util.List", "import com.hidden.Gen", "import com.hidden.sub.Deep.Inner", "import com.hiddenx.Other"}
	for name, c := range map[string]struct {
		hidden facts.Facts
		warned []string
	}{
		"failed":        {facts.Facts{}, all},
		"refused, old":  {besteffort.Refuse(facts.Facts{}, besteffort.Scope{}), all},
		"unicode":       {broken("package com.hidden;\nclass A { String s = \"\\u00e9\"; class {}\n"), all},
		"package":       {broken("/* see package com.hidden.sub */\npackage com . /* c */ hidden;\nclass A extends {}\n"), []string{"import com.hidden.Gen", "import com.hidden.sub.Deep.Inner"}},
		"default":       {broken("// the default package-private helper\nclass A extends {}\n"), nil},
		"comment words": {broken("// a package com.hiddenx note\nclass A extends {}\n"), []string{"import com.hiddenx.Other"}},
	} {
		files := map[string]facts.Facts{"z/Hidden.java": c.hidden}
		for file, source := range map[string]string{"a/Use.java": use, "a/Order.java": order} {
			result := parseFile(t, []byte(source))
			files[file] = Extract(file, []byte(source), result.Root)
		}
		nodes, edges, diagnostics := Link(files)
		imports := 0
		for _, e := range edges {
			if e.Kind == "imports" {
				imports++
			}
		}
		var warned []string
		for _, d := range diagnostics {
			if d.Code != "unresolved_import" || !strings.Contains(d.Message, "z/Hidden.java") {
				t.Errorf("%s: diagnostic %v", name, d)
			}
			warned = append(warned, strings.SplitN(d.Message, " is not", 2)[0])
		}
		if !reflect.DeepEqual(warned, c.warned) || len(nodes) != 4-len(c.warned) || imports != 1+len(nodes) {
			t.Errorf("%s: warned %q, nodes %d, imports %d", name, warned, len(nodes), imports)
		}
	}
}

// TestRefusedScope: the lexical package scan behind BE-8 over-matches
// (comments, strings) and gives up on unicode escapes.
func TestRefusedScope(t *testing.T) {
	for source, want := range map[string]besteffort.Scope{
		"package a.b;\nclass X extends {}":                         {Known: true, Names: []string{"a.b"}},
		"@Deprecated package a . b /* c */ ;":                      {Known: true, Names: []string{"a.b"}},
		"// package-private, see package x.y\npackage a;\n":        {Known: true, Names: []string{"x.y", "a"}},
		"class X { String s = \"package q\"; void packaged() {} }": {Known: true, Names: []string{"q"}},
		"class X {}":                      {Known: true},
		"package a;\nchar c = '\\u0041';": {},
	} {
		if got := RefusedScope([]byte(source)); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %+v, want %+v", source, got, want)
		}
	}
}
