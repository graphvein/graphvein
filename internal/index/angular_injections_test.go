package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/graphvein/graphvein/internal/index"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularInjections(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-injects")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	first, _, _ := emit(t, root, s, opt)
	snap, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/angular-injects.golden.json", store.Snapshot{Nodes: snap.Nodes, Edges: snap.Edges, Diagnostics: snap.Diagnostics})
	nodes := map[string]contract.Node{}
	attrs := map[string]map[string]any{}
	for _, n := range snap.Nodes {
		if n.Kind == "class" {
			nodes[n.Name] = n
			if a, ok := n.Attributes["angular"].(map[string]any); ok {
				attrs[n.Name] = a
			}
		}
	}
	for _, name := range []string{"Conflict", "Jit", "Invalid", "Plain", "C1Parentheses", "C1Reference", "C1Assertion", "C1CombinedAfter", "C1CombinedBefore"} {
		if attrs[name]["injects"] != nil {
			t.Errorf("ineligible %s gained injects", name)
		}
	}
	for _, name := range []string{"C1Parentheses", "C1Reference", "C1Assertion", "C1CombinedAfter", "C1CombinedBefore"} {
		if attrs[name]["metadata"] != "invalid" {
			t.Errorf("%s C1: %v", name, attrs[name])
		}
	}
	want := map[string]map[string]int{
		"Types": {"entries": 36, "linked": 13, "external": 2, "unresolved": 13, "invalid": 5, "injection_token": 2, "attribute": 1, "overridden": 4},
		"Sites": {"entries": 8, "linked": 4, "external": 1, "unresolved": 1, "injection_token": 1, "attribute": 1, "overridden": 1},
		"Merge": {"entries": 2, "linked": 2}, "Overloads": {"entries": 1, "linked": 1},
		"TypeShadow":       {"entries": 1, "unresolved": 1},
		"NestedTypeShadow": {"entries": 1, "unresolved": 1},
		"FieldScope":       {"entries": 1, "unresolved": 1},
		"ConstructorBlock": {"entries": 1, "unresolved": 1},
		"UsesOverrides":    {"entries": 5, "linked": 4, "external": 1, "overridden": 3},
		"LocalDecorator":   {"entries": 1, "linked": 1}, "Shadow": {"entries": 0}, "Partial": {"entries": 1, "linked": 1},
	}
	for name, expected := range want {
		c, ok := attrs[name]["injects"].(map[string]any)
		if !ok {
			t.Errorf("%s missing injects", name)
			continue
		}
		for _, key := range []string{"entries", "linked", "external", "unresolved", "invalid", "injection_token", "attribute", "overridden"} {
			if c[key] != json.Number(strconv.Itoa(expected[key])) {
				t.Errorf("%s %s: got %v want %d", name, key, c[key], expected[key])
			}
		}
	}
	if attrs["Inherited"]["injects"].(map[string]any)["inherited"] != true {
		t.Error("inherited factory not recorded")
	}
	mergeEdges := 0
	functionEdges := 0
	for _, e := range snap.Edges {
		if e.Kind != "injects" {
			continue
		}
		if strings.Contains(e.Source, "lexical.ts") && !strings.HasSuffix(e.Source, "#unaffected") {
			t.Error("lexical shadow or nested arrow gained an edge", e)
		}
		if e.Source == nodes["Merge"].Identity {
			mergeEdges++
			if len(e.Evidence) != 2 {
				t.Error("merge evidence", e)
			}
		}
		if strings.HasPrefix(e.Source, "function:") {
			functionEdges++
			if strings.Contains(e.Source, "nested") {
				t.Error("nested function gained injection", e)
			}
		}
		if e.Source == nodes["Types"].Identity && (strings.HasPrefix(e.Target, "interface:") || strings.HasPrefix(e.Target, "type_alias:") || strings.HasPrefix(e.Target, "enum:")) {
			t.Error("value-less token gained edge", e)
		}
	}
	if mergeEdges != 1 || functionEdges != 4 {
		t.Errorf("merge=%d functions=%d", mergeEdges, functionEdges)
	}
	shadowDiagnostics := map[string]bool{}
	for _, d := range snap.Diagnostics {
		if strings.HasPrefix(d.Code, "angular_injection_") && strings.Contains(d.Message, "token :") {
			t.Error("type annotation colon in diagnostic", d)
		}
		if d.Code == "angular_injection_invalid" {
			want := "DECORATOR_ARITY_WRONG"
			if strings.Contains(d.Message, "parameter invalidDecorator ") {
				want = "DECORATOR_UNEXPECTED"
			}
			if !strings.HasSuffix(d.Message, ": "+want) {
				t.Error("non-specific Angular error", d)
			}
		}
		if d.File == "lexical.ts" && d.Code == "angular_injection_unresolved" {
			for _, name := range []string{"ConstructorBlock", "loopToken", "catchToken", "namespaceToken"} {
				if strings.Contains(d.Message, "#"+name+" ") && strings.HasSuffix(d.Message, ": locally shadowed") {
					shadowDiagnostics[name] = true
				}
			}
		}
	}
	if len(shadowDiagnostics) != 4 {
		t.Error("missing precise lexical diagnostics", shadowDiagnostics)
	}
	second, summary, _ := emit(t, root, s, opt)
	if !bytes.Equal(first, second) || summary.Parsed != 0 {
		t.Fatal("injection replay changed bytes or parsed files", summary)
	}
	// Provider-only edits relink unchanged callers, and test providers stay ignored.
	provider := filepath.Join(root, "providers.ts")
	data, err := os.ReadFile(provider)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "{provide: Renamed, useClass: DefaultToken}", "{provide: DefaultToken, useClass: Renamed}"))
	if err := os.WriteFile(provider, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 {
		t.Errorf("provider edit parsed %d files", summary.Parsed)
	}
	changed, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range changed.Nodes {
		if n.Name == "UsesOverrides" {
			c := n.Attributes["angular"].(map[string]any)["injects"].(map[string]any)
			if c["overridden"] != json.Number("2") {
				t.Error("unchanged caller retained provider proof", c)
			}
		}
	}
	configPath := filepath.Join(root, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(`{"compilerOptions":{"paths":{"@angular/core":["./shadow-core.ts"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 0 {
		t.Error("config edit reparsed TS", summary)
	}
	shadowed, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range shadowed.Edges {
		if e.Kind == "injects" {
			t.Error("paths shadow retained injects", e)
		}
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 0 {
		t.Error("config removal reparsed TS", summary)
	}
	packagePath := filepath.Join(root, "package.json")
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, []byte(strings.Replace(string(packageData), "angular-injects-fixture", "@angular/core", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 0 {
		t.Error("package edit reparsed TS", summary)
	}
	shadowed, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range shadowed.Edges {
		if e.Kind == "injects" {
			t.Error("package shadow retained injects", e)
		}
	}
	if err := os.WriteFile(packagePath, packageData, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _ = emit(t, root, s, opt)
	// A truncated payload must retry the host file instead of silently dropping DI.
	updated, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range updated.Files {
		if f.Path != "main.ts" {
			continue
		}
		key := index.Version + "/max=2097152"
		payload, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, key)
		if err != nil {
			t.Fatal(err)
		}
		var cached typescript.CachedFacts
		if err := json.Unmarshal(payload, &cached); err != nil {
			t.Fatal(err)
		}
		cached.Injections.Sites = nil
		payload, err = json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveFactCache(context.Background(), "fixture", f.Path, f.ContentHash, key, payload); err != nil {
			t.Fatal(err)
		}
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 {
		t.Errorf("truncated injections parsed %d", summary.Parsed)
	}
}
