package typescript

import (
	"context"
	"github.com/graphvein/graphvein/internal/contract"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

func TestSemanticIdentityAndShadowing(t *testing.T) {
	source := []byte("export /* note */ default function() { return 1; }\nfunction target() {}\nexport function caller() { target(); const target = 2; }\n")
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	facts := Extract("a.ts", source, results[0].Root)
	if facts.Exports["default"] != "function:a.ts#default.function" {
		t.Fatal(facts.Exports)
	}
	_, edges, diagnostics := Link(map[string]Facts{"a.ts": facts})
	for _, e := range edges {
		if e.Kind == "calls" {
			t.Fatal("forward lexical shadow incorrectly resolved", e)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "unresolved_calls" {
		t.Fatal(diagnostics)
	}
	shifted := append([]byte("\n\n"), source...)
	results, err = parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: shifted}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	after := Extract("a.ts", shifted, results[0].Root)
	if after.Exports["default"] != facts.Exports["default"] {
		t.Fatal("anonymous identity depends on line")
	}
}

func TestDiscriminatorPreservesLiteralWhitespace(t *testing.T) {
	if compact("(value: 'a b') /* comment */ : 1") == compact("(value:'ab'):1") {
		t.Fatal("distinct literal overloads collided")
	}
	if compact("(value: string) /* comment */ : 1") != "(value:string):1" {
		t.Fatal("signature trivia changed discriminator")
	}
}

func TestEmptyImportIsDiagnosed(t *testing.T) {
	nodes, edges, diagnostics := Link(map[string]Facts{"a.ts": {Imports: []Import{{Module: "", Span: contract.Span{StartLine: 1, EndLine: 1}}}}})
	if len(nodes) != 0 || len(edges) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "unresolved_import" {
		t.Fatal(nodes, edges, diagnostics)
	}
}

func TestRepeatedNamesAndImportedAmbiguity(t *testing.T) {
	extract := func(file, source string) Facts {
		t.Helper()
		results, err := parse.Batch(context.Background(), []parse.Job{{Language: "javascript", Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
		if err != nil || results[0].Err != nil {
			t.Fatal(results, err)
		}
		return Extract(file, []byte(source), results[0].Root)
	}
	source := "function helper() { function child() {} }\nfunction helper() { function child() {} }\nfunction helper() {}\nexport { helper };\n"
	f := extract("a.mjs", source)
	var names []string
	for _, node := range f.Nodes {
		names = append(names, node.QualifiedName)
	}
	want := []string{"helper", "helper.child", "helper@2", "helper@2.child", "helper@3"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names %v, want %v", names, want)
	}
	shifted := extract("a.mjs", "\n\n"+source)
	for i, node := range shifted.Nodes {
		if node.Identity != f.Nodes[i].Identity {
			t.Fatal("preceding line shift changed identity", node)
		}
	}
	other := extract("other.mjs", source)
	if other.Nodes[0].QualifiedName != "helper" || other.Nodes[3].QualifiedName != "helper@2.child" {
		t.Fatal("counts did not restart per file", other.Nodes)
	}
	caller := extract("caller.mjs", "import { helper } from './a.mjs';\nfunction run() { helper(); }\n")
	_, edges, diagnostics := Link(map[string]Facts{"a.mjs": f, "caller.mjs": caller})
	for _, e := range edges {
		if e.Kind == "calls" {
			t.Fatal("ambiguous imported helper resolved", e)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0].File != "caller.mjs" || diagnostics[0].Code != "unresolved_calls" {
		t.Fatal("missing ambiguous-call diagnostic", diagnostics)
	}
}

func TestLocalObjectMemberContainment(t *testing.T) {
	source := []byte(`const top = { save() {} };
function factory() {
  const local = { nested: { save() {} } };
}
class Service {
  value = (() => { const local = { save() {} }; })();
  run() { const local = { save() {} }; }
}
export const emitted = { save() {} };
`)
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	f := Extract("a.ts", source, results[0].Root)
	want := map[string]string{
		"method:a.ts#top.save":                  "file:a.ts#a.ts",
		"method:a.ts#factory.local.nested.save": "function:a.ts#factory",
		"method:a.ts#Service.local.save":        "class:a.ts#Service",
		"method:a.ts#Service.run.local.save":    "method:a.ts#Service.run",
		"method:a.ts#emitted.save":              "variable:a.ts#emitted",
	}
	for _, edge := range f.Edges {
		if owner, ok := want[edge.Target]; ok && edge.Kind == "contains" {
			if edge.Source != owner {
				t.Errorf("%s contained by %s, want %s", edge.Target, edge.Source, owner)
			}
			delete(want, edge.Target)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing containment edges: %v", want)
	}
}

func TestComputedObjectKeyKeepsCalls(t *testing.T) {
	source := []byte("function key() {}\nfunction run() { const value = { [key()]: { save() {} } }; }\n")
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: source}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	f := Extract("a.ts", source, results[0].Root)
	_, edges, _ := Link(map[string]Facts{"a.ts": f})
	found := false
	for _, edge := range edges {
		if edge.Kind == "calls" && edge.Source == "function:a.ts#run" && edge.Target == "function:a.ts#key" {
			found = true
		}
	}
	if !found {
		t.Fatal("computed property key lost its call", edges)
	}
	if len(f.Nodes) != 3 || f.Nodes[2].QualifiedName != "run.value.save" {
		t.Fatal("computed key added an owner", f.Nodes)
	}
}
