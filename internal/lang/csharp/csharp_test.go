package csharp

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

const (
	fixture = "../../../testdata/fixtures/csharp-best-effort"
	// externalFixture has no refused file, so its usings reach CS-I2, which
	// row BE-8 blocks in the main fixture.
	externalFixture = "../../../testdata/fixtures/csharp-best-effort-external"
	// hiddenFixture's refused files have block namespaces or none, so BE-8
	// hides plain usings related to their namespace names and every static
	// and alias using there.
	hiddenFixture = "../../../testdata/fixtures/csharp-best-effort-hidden"
)

func TestContractTableMatchesDocument(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/lang/best-effort.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := besteffort.DocTable(string(doc), "C#"); !reflect.DeepEqual(got, Rows) {
		t.Fatalf("docs/lang/best-effort.md C# table differs from csharp.Rows:\n doc: %q\n go:  %q", got, Rows)
	}
}

// TestBranchesMatchRows reads every row id string literal of the extractor
// and linker (parsed, so ids in comments do not count): each names a row of
// the C# or shared table, and each C# row has a branch. That every
// branch is also taken is TestFixtureCoversEveryRow's job.
func TestBranchesMatchRows(t *testing.T) {
	branches, err := contracttest.RowLiterals([]string{"extract.go", "link.go"}, "CS")
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
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "c_sharp", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err, results)
	}
	return results[0]
}

// TestFixtureCoversEveryRow extracts and links the fixtures the index goldens
// use (each root linked on its own, as the indexer does) and checks that
// their cases take every C# row and the shared rows the package implements.
func TestFixtureCoversEveryRow(t *testing.T) {
	seen := map[string]bool{}
	trace := tracer(func(row string) { seen[row] = true })
	for _, root := range []string{fixture, externalFixture, hiddenFixture} {
		files := map[string]facts.Facts{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".cs") {
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
		"error":   "using System;\nclass X : IA\n#if A\n, IB\n#endif\n{\n  void Keep() {}\n}\n",
		"missing": "using System;\nclass X {\n  void M() { int x = 1 }\n}\n",
	} {
		result := parseFile(t, []byte(source))
		if !result.SyntaxError {
			t.Fatalf("%s: parsed without a syntax error", name)
		}
		f := besteffort.Refuse(Extract("X.cs", []byte(source), result.Root), RefusedScope([]byte(source)))
		if len(f.Nodes)+len(f.Edges) != 0 || f.BestEffort == nil || !f.BestEffort.Refused || len(f.BestEffort.Imports)+len(f.BestEffort.Namespaces) != 0 {
			t.Fatalf("%s: refused file claims %+v", name, f)
		}
	}
}

// TestNames covers the C# naming rules: BE-6 type lists with parameter
// modifiers and a `params` parameter, explicit interface suffixes, the
// static constructor, operators and conversions, and the generic arity
// suffix.
func TestNames(t *testing.T) {
	source := []byte(`class A : I<int> {
  static A() {}
  A(ref int a, [X] string /* c */ b) {}
  A(params System.Collections.Generic.List<int> rest) {}
  void M(this int x, out int y, scoped ref int z) {}
  void M() {}
  void I<int>.M() {}
  int global::N.I.P => 1;
  int P => 1;
  int this[int i] => i;
  int this[string s, params object[] o] => 1;
  public static A operator checked -(A a, A b) => a;
  public static explicit operator int(A a) => 0;
  public static explicit operator checked int(A a) => 0;
}
class Box {}
class Box<T> {}
struct Box<T, U> {}
`)
	result := parseFile(t, source)
	if result.SyntaxError {
		t.Fatal("syntax error")
	}
	f := Extract("A.cs", source, result.Root)
	var got []string
	for _, n := range f.Nodes {
		got = append(got, n.Kind+" "+n.QualifiedName+" "+n.Name)
	}
	want := []string{
		"class A A",
		"constructor A.A@static A",
		"constructor A.A@overload(ref int,string) A",
		"constructor A.A@overload(params System.Collections.Generic.List<int>) A",
		"method A.M@overload(this int,out int,scoped ref int) M",
		"method A.M@overload() M",
		"method A.M@I M",
		"property A.P@I P",
		"property A.P P",
		"property A.this@overload(int) this",
		"property A.this@overload(string,params object[]) this",
		"method A.operator checked - operator checked -",
		"method A.explicit operator int explicit operator int",
		"method A.explicit operator checked int explicit operator checked int",
		"class Box Box",
		"class Box`1 Box",
		"struct Box`2 Box",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
}

func extractAll(t *testing.T, sources map[string]string) map[string]facts.Facts {
	t.Helper()
	files := map[string]facts.Facts{}
	for file, source := range sources {
		result := parseFile(t, []byte(source))
		if result.SyntaxError {
			t.Fatal(file)
		}
		files[file] = Extract(file, []byte(source), result.Root)
	}
	return files
}

// TestLinkRows covers the CS-I1 inventory: declared namespaces and their
// prefixes, relative names inside a namespace (CS-I3 when not the
// repository's), static and alias usings of nested types, global-namespace
// types, the CS-I4 warnings for names under a repository namespace of two
// or more segments, and the external CS-I2 names.
func TestLinkRows(t *testing.T) {
	files := extractAll(t, map[string]string{
		"a/Order.cs": "namespace Shop.Orders { public class Order { public class Line {} } namespace Deep { class D {} } }\n",
		"b/Top.cs":   "class Top { public class Inner {} }\n",
		"d/Api.cs":   "namespace Corp.Api.V1 { class X {} }\n",
		"c/Use.cs": `using Shop;
using Shop.Orders.Deep;
using static Shop.Orders.Order.Line;
using L = Shop.Orders.Order.Line;
using static Top.Inner;
using static Other.Thing;
using System.Text;
using T = global::System.Collections.Generic.List<Shop.Orders.Order>;
using Shop.Orders.Generated;
using Corp.Api.Components;
using static Corp.Api.Gen.Ids;
using Corp.Web;
namespace Shop.Use
{
    using Orders;
    using Logging;
    using global::Serilog;
    using static Helpers;
}
`,
	})
	nodes, edges, diagnostics := Link(files)
	var targets []string
	for _, e := range edges {
		if e.Kind == "imports" {
			targets = append(targets, e.Target)
		}
	}
	sort.Strings(targets)
	want := []string{
		"external:.#dotnet%3ACorp.Web",
		"external:.#dotnet%3AOther.Thing",
		"external:.#dotnet%3ASerilog",
		"external:.#dotnet%3ASystem.Collections.Generic.List%3CShop.Orders.Order%3E",
		"external:.#dotnet%3ASystem.Text",
	}
	var warned []string
	for _, d := range diagnostics {
		warned = append(warned, d.Code+": "+d.Message)
	}
	wantWarned := []string{
		"unresolved_import: using Shop.Orders.Generated is not declared by a scanned C# file, but the repository declares namespace Shop.Orders or one under it",
		"unresolved_import: using Corp.Api.Components is not declared by a scanned C# file, but the repository declares namespace Corp.Api or one under it",
		"unresolved_import: using static Corp.Api.Gen.Ids is not declared by a scanned C# file, but the repository declares namespace Corp.Api or one under it",
	}
	if !reflect.DeepEqual(targets, want) || len(nodes) != 5 || !reflect.DeepEqual(warned, wantWarned) {
		t.Fatalf("targets %q, nodes %d, diagnostics %q", targets, len(nodes), warned)
	}
	seen := map[string]int{}
	link(files, func(row string) { seen[row]++ })
	if seen["CS-I1"] != 7 || seen["CS-I2"] != 5 || seen["CS-I3"] != 1 || seen["CS-I4"] != 3 {
		t.Fatalf("rows %v", seen)
	}
}

// TestHiddenFileBlocksExternals: row BE-8. While a C# file is failed (BE-3)
// or refused (BE-2) with a `\u` escape in its text, a using that would be
// external is an unresolved_import warning, BCL names included; repository
// usings still claim nothing. A refused file whose only namespace keyword is
// one file-scoped declaration hides usings of that namespace, of names under
// it and of its dotted prefixes. A refused file with block namespaces hides
// plain usings related in the same way to any namespace name in its text
// (nested blocks included) and every static and alias using; one without the
// keyword hides only static and alias usings.
func TestHiddenFileBlocksExternals(t *testing.T) {
	use := "using System.Linq;\nusing Shop;\nusing Corp;\nusing Corp.Hidden;\nusing static Corp.Hidden.Gen.Inner;\nusing CorpX;\nusing Inner.Deep;\nusing static Util.Text;\nusing J = Json.Net;\nnamespace Shop;\nclass Use {}\n"
	broken := func(text string) facts.Facts {
		return besteffort.Refuse(facts.Facts{}, RefusedScope([]byte(text)))
	}
	all := []string{"using System.Linq", "using Corp", "using Corp.Hidden", "using static Corp.Hidden.Gen.Inner", "using CorpX", "using Inner.Deep", "using static Util.Text", "using Json.Net"}
	scoped := []string{"using Corp", "using Corp.Hidden", "using static Corp.Hidden.Gen.Inner"}
	typed := []string{"using static Corp.Hidden.Gen.Inner", "using static Util.Text", "using Json.Net"}
	block := []string{"using Corp", "using Corp.Hidden", "using static Corp.Hidden.Gen.Inner", "using Inner.Deep", "using static Util.Text", "using Json.Net"}
	for name, c := range map[string]struct {
		hidden facts.Facts
		warned []string
	}{
		"failed":       {facts.Facts{}, all},
		"refused, old": {besteffort.Refuse(facts.Facts{}, besteffort.Scope{}), all},
		"escape":       {broken("namespace Corp.Hidden {\nclass G\\u0041 : I\n#if A\n, J\n#endif\n{ }\n}\n"), all},
		"file-scoped":  {broken("namespace Corp . Hidden ;\nclass Gen : I\n#if A\n, J\n#endif\n{ }\n"), scoped},
		"block":        {broken("namespace Corp.Hidden {\nclass Gen : I\n#if A\n, J\n#endif\n{ }\n}\n"), []string{"using Corp", "using Corp.Hidden", "using static Corp.Hidden.Gen.Inner", "using static Util.Text", "using Json.Net"}},
		"nested block": {broken("namespace Corp {\nnamespace Hidden {\nclass Gen : I\n#if A\n, J\n#endif\n{ }\n}\n}\nnamespace Inner { }\n"), block},
		"no keyword":   {broken("class Gen : I\n#if A\n, J\n#endif\n{ }\n"), typed},
		"prose":        {broken("namespace Corp.Hidden;\n// the namespace is hidden\nclass Gen : I\n#if A\n, J\n#endif\n{ }\n"), []string{"using Corp", "using Corp.Hidden", "using static Corp.Hidden.Gen.Inner", "using static Util.Text", "using Json.Net"}},
	} {
		files := extractAll(t, map[string]string{"a/Use.cs": use})
		files["z/Hidden.cs"] = c.hidden
		nodes, _, diagnostics := Link(files)
		var warned []string
		for _, d := range diagnostics {
			if d.Code != "unresolved_import" || !strings.Contains(d.Message, "z/Hidden.cs") {
				t.Errorf("%s: diagnostic %v", name, d)
			}
			warned = append(warned, strings.SplitN(d.Message, " is not", 2)[0])
		}
		if !reflect.DeepEqual(warned, c.warned) || len(nodes) != 8-len(c.warned) {
			t.Errorf("%s: warned %q, nodes %d", name, warned, len(nodes))
		}
	}
}

// TestRefusedScope: the lexical namespace scan behind BE-8 proves a
// file-scoped scope when every namespace keyword is a file-scoped
// declaration, a Global scope (namespaces only) otherwise, and nothing for
// a `\u` escape or a verbatim name after the keyword.
func TestRefusedScope(t *testing.T) {
	for source, want := range map[string]besteffort.Scope{
		"using X;\nnamespace A.B;\nclass C {}":               {Known: true, Names: []string{"A.B"}},
		"namespace A /* c */ . B ;":                          {Known: true, Names: []string{"A.B"}},
		"#if X\nnamespace A;\n#else\nnamespace B;\n#endif":   {Known: true, Names: []string{"A", "B"}},
		"class @namespace {}\nnamespace A;":                  {Known: true, Names: []string{"A"}},
		"namespace A { class C {} }":                         {Known: true, Names: []string{"A"}, Global: true},
		"namespace A { namespace B.C { } }\nnamespace A { }": {Known: true, Names: []string{"A", "B.C"}, Global: true},
		"namespace A;\n/// see namespace docs":               {Known: true, Names: []string{"A", "docs"}, Global: true},
		"class C {}":                                         {Known: true, Global: true},
		"namespace":                                          {Known: true, Global: true},
		"namespace A\\u0042;":                                {},
		"namespace @class { }":                               {},
	} {
		if got := RefusedScope([]byte(source)); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %+v, want %+v", source, got, want)
		}
	}
}
