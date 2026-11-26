package dart

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

const fixture = "../../../testdata/fixtures/dart-best-effort"

func TestContractTableMatchesDocument(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/lang/best-effort.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := besteffort.DocTable(string(doc), "Dart"); !reflect.DeepEqual(got, Rows) {
		t.Fatalf("docs/lang/best-effort.md Dart table differs from dart.Rows:\n doc: %q\n go:  %q", got, Rows)
	}
}

// TestBranchesMatchRows reads every row id string literal of the extractor
// and linker (parsed, so ids in comments do not count): each names a row of
// the Dart or shared table, and each Dart row has a branch. That every
// branch is also taken is TestFixtureCoversEveryRow's job.
func TestBranchesMatchRows(t *testing.T) {
	branches, err := contracttest.RowLiterals([]string{"extract.go", "link.go"}, "DA")
	if err != nil {
		t.Fatal(err)
	}
	for row := range branches {
		if !Rows.Has(row) && !besteffort.Shared.Has(row) {
			t.Errorf("branch tagged %s has no contract row", row)
		}
	}
	for _, row := range append(Rows.IDs(), "BE-2", "D") {
		if !branches[row] {
			t.Errorf("row %s has no branch", row)
		}
	}
}

func parseFile(t *testing.T, source []byte) parse.Result {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "dart", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err, results)
	}
	return results[0]
}

// TestFixtureCoversEveryRow extracts and links the fixture the index golden
// uses and checks that its cases take every Dart row and the shared rows
// the package implements.
func TestFixtureCoversEveryRow(t *testing.T) {
	seen := map[string]bool{}
	trace := tracer(func(row string) { seen[row] = true })
	files := map[string]facts.Facts{}
	sources := map[string][]byte{}
	err := filepath.WalkDir(fixture, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, path)
		rel = filepath.ToSlash(rel)
		sources[rel] = source
		if !strings.HasSuffix(path, ".dart") {
			return nil
		}
		result := parseFile(t, source)
		f := extract(rel, source, result.Root, trace)
		if result.SyntaxError {
			f = besteffort.Refuse(f, besteffort.Scope{})
		}
		files[rel] = f
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	link(files, PackageNames(sources), trace)
	var missing []string
	for _, row := range append(Rows.IDs(), "BE-2", "D") {
		if !seen[row] {
			missing = append(missing, row)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("fixture misses rows %v", missing)
	}
}

// TestRecoveredTreeClaimsNothing: a recovered tree claims no symbol and no
// directive (row BE-2), whether the extractor sees the ERROR node or only
// the parser's syntax flag (through Refuse).
func TestRecoveredTreeClaimsNothing(t *testing.T) {
	source := "import 'a.dart';\nclass X {\n  void keep() {}\n  void broken( {}\n}\n"
	result := parseFile(t, []byte(source))
	if !result.SyntaxError {
		t.Fatal("parsed without a syntax error")
	}
	f := besteffort.Refuse(Extract("x.dart", []byte(source), result.Root), besteffort.Scope{})
	if len(f.Nodes)+len(f.Edges) != 0 || f.BestEffort == nil || !f.BestEffort.Refused || len(f.BestEffort.Imports) != 0 {
		t.Fatalf("refused file claims %+v", f)
	}
}

func extractOne(t *testing.T, file, source string) facts.Facts {
	t.Helper()
	result := parseFile(t, []byte(source))
	if result.SyntaxError {
		t.Fatalf("%s: syntax error", file)
	}
	return Extract(file, []byte(source), result.Root)
}

// TestNames covers the Dart naming rules: accessors (`x`, `x=`), operators,
// unnamed (`new`) and named constructors in classic and Dart 3.13 forms,
// anonymous extensions, the DA-G1 and DA-G2 guards, and the DA-S spans.
func TestNames(t *testing.T) {
	source := `class A {
  A();
  A.b();
  new c();
  factory d() => A();
  factory A.e() => A();
  factory() => A();
  foo();
  bar() {}
  factory B.f() => A();
  int get g => 1;
  set g(int v) {}
  A operator -() => this;
  A operator -(A o) => this;
  void operator []=(int i, int v) {}
}
extension on Map<String, List<int>> {}
extension<T> on T Function(T) {}
@a
// c
@b
external int get top;
`
	f := extractOne(t, "a.dart", source)
	var got []string
	for _, n := range f.Nodes {
		line := n.Kind + " " + n.QualifiedName + " " + n.Name
		if n.Attributes["accessor"] != nil {
			line += " " + n.Attributes["accessor"].(string)
		}
		got = append(got, line)
	}
	want := []string{
		"class A A",
		"constructor A.new new",
		"constructor A.b b",
		"constructor A.c c",
		"constructor A.d d",
		"constructor A.e e",
		"constructor A.new@2 new",
		"method A.g g get",
		"method A.g= g= set",
		"method A.operator - operator -",
		"method A.operator -@2 operator -",
		"method A.operator []= operator []=",
		"extension extension on Map<String,List<int>> extension on Map<String,List<int>>",
		"extension extension on T Function(T) extension on T Function(T)",
		"function top top get",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	top := f.Nodes[len(f.Nodes)-1].Span
	if top.StartLine != 19 || top.StartColumn != 0 || top.EndLine != 22 || top.EndColumn != 21 {
		t.Fatalf("top-level getter span %+v (annotations, comment, external and ; included)", top)
	}
}

// TestDotNewConstructors: `<Type>.new` (Dart 2.15) declares the unnamed
// constructor, so it gets the identity `C()` gets (row DA-C1), with or
// without `const`; only a `new` token with no `.` before it is the concise
// form (DA-C2), and any other `X.new` shape is refused (DA-G1) (T021-F1,
// review).
func TestDotNewConstructors(t *testing.T) {
	source := `class C {
  final int x;
  C.new() : x = 0;
}
class K {
  const K.new();
}
class N {
  new();
  new named();
  X.new();
  factory N.make() => N();
}
`
	f := extractOne(t, "a.dart", source)
	var got []string
	for _, n := range f.Nodes {
		got = append(got, n.Kind+" "+n.QualifiedName+" "+n.Name)
	}
	want := []string{
		"class C C",
		"constructor C.new new",
		"class K K",
		"constructor K.new new",
		"class N N",
		"constructor N.new new",
		"constructor N.named named",
		"constructor N.make make",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	result := parseFile(t, []byte(source))
	seen := map[string]int{}
	extract("a.dart", []byte(source), result.Root, func(row string) { seen[row]++ })
	if seen["DA-C1"] != 3 || seen["DA-C2"] != 2 || seen["DA-G1"] != 1 {
		t.Fatalf("rows %v", seen)
	}
}

// TestPackageNames reads only an unindented name: line with a plain or
// quoted package name.
func TestPackageNames(t *testing.T) {
	got := PackageNames(map[string][]byte{
		"pubspec.yaml":        []byte("\uFEFF# c\nname: root_pkg\n"),
		"a/pubspec.yaml":      []byte("description: x\r\nname: 'quoted' # c\r\n"),
		"b/pubspec.yaml":      []byte("dependencies:\n  name: nested\n"),
		"c/pubspec.yaml":      []byte("name: not-a-name\n"),
		"d/pubspec.yaml":      []byte("name:\n  folded\n"),
		"e/pubspec.yaml":      []byte("name: \"dq\"\nname: second\n"),
		"f/other.yaml":        []byte("name: ignored\n"),
		"g/sub/pubspec.yaml":  []byte("name: Sub_9\n"),
		"h/pubspec.yaml.orig": []byte("name: ignored\n"),
	})
	want := map[string]string{".": "root_pkg", "a": "quoted", "b": "", "c": "", "d": "", "e": "dq", "g/sub": "Sub_9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

// TestLinkRows covers the directive rows: dart: libraries, relative URIs
// (found, missing, above the root, self), the own package, other packages
// of the repository, external packages, other schemes and malformed
// package URIs, conditional alternatives, and the refusal of every
// package: URI that could name a package whose pubspec name is unknown.
func TestLinkRows(t *testing.T) {
	use := `import 'dart:io';
import 'b.dart';
import 'use.dart';
import 'nope.dart';
import '../../../up.dart';
import '/abs.dart';
import 'package:app/x.dart';
import 'package:app/y.dart';
import 'package:app/../escape.dart';
import 'package:core/c.dart';
import 'package:http/http.dart';
import 'package:/bad.dart';
import 'package:bad';
import 'dart:not/a/lib';
import 'file:///etc/x.dart';
import 'b.dart?q';
import 'a%20b.dart';
import 'c.dart' if (dart.library.io) 'dart:io' if (dart.library.html) 'package:web/web.dart';
part 'p.dart';
`
	build := func(packages map[string]string) (map[string]int, []string, []string) {
		files := map[string]facts.Facts{
			"app/lib/use.dart":    extractOne(t, "app/lib/use.dart", use),
			"app/lib/b.dart":      extractOne(t, "app/lib/b.dart", "class B {}\n"),
			"app/lib/a b.dart":    extractOne(t, "app/lib/a b.dart", "class AB {}\n"),
			"app/lib/c.dart":      extractOne(t, "app/lib/c.dart", "class C {}\n"),
			"app/lib/p.dart":      extractOne(t, "app/lib/p.dart", "part of 'use.dart';\n"),
			"app/lib/x.dart":      extractOne(t, "app/lib/x.dart", "class X {}\n"),
			"core/lib/c.dart":     extractOne(t, "core/lib/c.dart", "class C {}\n"),
			"app/lib/broken.dart": besteffort.Refuse(facts.Facts{}, besteffort.Scope{}),
		}
		rows := map[string]int{}
		nodes, edges, diagnostics := link(files, packages, func(row string) { rows[row]++ })
		var targets, warnings []string
		for _, e := range edges {
			if e.Kind == "imports" {
				targets = append(targets, e.Target)
			}
		}
		for _, n := range nodes {
			if n.Kind != "external" {
				t.Errorf("node %v", n)
			}
		}
		for _, d := range diagnostics {
			warnings = append(warnings, d.Message)
		}
		sort.Strings(targets)
		return rows, targets, warnings
	}
	rows, targets, warnings := build(map[string]string{"app": "app", "core": "core"})
	wantTargets := []string{
		"external:.#dart%3Aio",
		"external:.#pub%3Ahttp",
		"external:.#pub%3Aweb",
		"file:app/lib/a%20b.dart#app%2Flib%2Fa%20b.dart",
		"file:app/lib/b.dart#app%2Flib%2Fb.dart",
		"file:app/lib/c.dart#app%2Flib%2Fc.dart",
		"file:app/lib/p.dart#app%2Flib%2Fp.dart",
		"file:app/lib/x.dart#app%2Flib%2Fx.dart",
	}
	wantWarnings := []string{
		"import 'nope.dart' does not name a scanned file",
		"import '../../../up.dart' resolves outside the repository",
		"import '/abs.dart' resolves outside the repository",
		"import 'package:app/y.dart' does not name a scanned file (package app is app)",
		"import 'package:app/../escape.dart' resolves outside package app's lib directory (app/lib)",
	}
	if !reflect.DeepEqual(targets, wantTargets) || !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("targets\n%s\nwarnings\n%s", strings.Join(targets, "\n"), strings.Join(warnings, "\n"))
	}
	if !reflect.DeepEqual(rows, map[string]int{"DA-I1": 2, "DA-I2": 8, "DA-I3": 3, "DA-I4": 1, "DA-I5": 2, "DA-I8": 5}) {
		t.Fatalf("rows %v", rows)
	}
	// A pubspec without a readable name: package: URIs that could name it
	// (all of them) claim nothing, and no pub: external is emitted.
	rows, targets, _ = build(map[string]string{"app": "app", "core": ""})
	if rows["DA-I5"] != 0 || rows["DA-I4"] != 3 || strings.Contains(strings.Join(targets, " "), "pub%3A") {
		t.Fatalf("unnamed pubspec: rows %v, targets %v", rows, targets)
	}
	rows, _, _ = build(map[string]string{"app": "", "core": "core"})
	if rows["DA-I3"] != 0 || rows["DA-I5"] != 0 || rows["DA-I4"] != 6 {
		t.Fatalf("unnamed own pubspec: rows %v", rows)
	}
}
