package index_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularDeclarations(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-declarations")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	first, summary, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/angular-declarations.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	classes := map[string]contract.Node{}
	attrs := map[string]map[string]any{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "class" {
			continue
		}
		classes[n.Name] = n
		if a, ok := n.Attributes["angular"].(map[string]any); ok {
			attrs[n.Name] = a
		}
	}
	for _, name := range []string{"Indirect", "Unrelated", "Plain", "TypeOnly", "ParameterShadow"} {
		if attrs[name] != nil {
			t.Errorf("unproven class %s gained Angular attributes", name)
		}
	}
	for _, name := range []string{"Conflict", "Repeated"} {
		if !reflect.DeepEqual(attrs[name], map[string]any{"role": nil, "conflict": true}) {
			t.Errorf("conflict %s: %v", name, attrs[name])
		}
	}
	for _, name := range []string{"Satisfies", "NonNull", "TypeAssertion", "NonObject", "TwoArguments", "MissingPipeName", "InvalidSecondaryAfter", "InvalidSecondaryBefore"} {
		if attrs[name]["metadata"] != "invalid" {
			t.Errorf("%s metadata: %v", name, attrs[name])
		}
	}
	for _, name := range []string{"Spread", "Computed", "Method"} {
		if attrs[name]["metadata"] != "partial" {
			t.Errorf("%s metadata: %v", name, attrs[name])
		}
	}
	for name, want := range map[string]any{"Modern": true, "Declared": false, "RangeVersion": false, "UnknownVersion": nil, "MultiMajor": nil, "Standalone": true} {
		if attrs[name]["standalone"] != want {
			t.Errorf("%s standalone: %v", name, attrs[name])
		}
	}
	if attrs["Attribute"]["selector"] != "[from-constant]" || !reflect.DeepEqual(attrs["Attribute"]["export_as"], []any{"a", "b"}) {
		t.Error("imported selector/exportAs", attrs["Attribute"])
	}
	if attrs["HelperSelector"]["selector"] != "[from-helper]" {
		t.Error("existing helper evaluator was not reused", attrs["HelperSelector"])
	}
	if attrs["InjectableComponent"]["injectable"] != true || attrs["InjectableComponent"]["role"] != "component" {
		t.Error(attrs["InjectableComponent"])
	}
	for _, name := range []string{"JitModule", "JitComponent"} {
		if attrs[name]["jit"] != true {
			t.Error(name, attrs[name])
		}
	}
	if attrs["Both"]["template"].(map[string]any)["kind"] != "external" || attrs["Escape"]["template"].(map[string]any)["status"] != "escape" {
		t.Error("template precedence or escape")
	}
	if attrs["Inline"]["template"].(map[string]any)["status"] != "parsed" {
		t.Error(attrs["Inline"])
	}
	edges := map[string]contract.Edge{}
	for _, e := range snapshot.Edges {
		if e.Source == classes["InvalidSecondaryAfter"].Identity || e.Source == classes["InvalidSecondaryBefore"].Identity {
			if e.Kind == "uses_template" || strings.HasPrefix(e.Kind, "scope_") {
				t.Error("invalid Injectable metadata emitted Angular edges", e)
			}
		}
		edges[e.Kind+"\x00"+e.Source+"\x00"+e.Target] = e
	}
	has := func(kind, from, to string) bool {
		_, ok := edges[kind+"\x00"+classes[from].Identity+"\x00"+classes[to].Identity]
		return ok
	}
	for name, keys := range map[string][]any{
		"UnknownStandalone": {"standalone"},
		"UnknownSelector":   {"selector", "standalone"},
	} {
		a := attrs[name]
		if a["standalone"] != nil || a["standalone_source"] != "unknown" || !reflect.DeepEqual(a["unknown_keys"], keys) {
			t.Errorf("explicit unknown metadata on %s: %v", name, a)
		}
	}
	for name, counts := range map[string]map[string]int{
		"RevisionModule":          {"declarations": 3, "imports": 1},
		"UnknownStandaloneModule": {"declarations": 1},
	} {
		for key, want := range counts {
			got := attrs[name][key].(map[string]any)
			if got["unresolved"] != json.Number(strconv.Itoa(want)) || got["invalid"] != json.Number("0") || got["linked"] != json.Number("0") {
				t.Errorf("%s %s counts: %v", name, key, got)
			}
		}
		for _, edge := range snapshot.Edges {
			if edge.Source == classes[name].Identity && (edge.Kind == "declares" || strings.HasPrefix(edge.Kind, "scope_")) {
				t.Errorf("unproven revision target linked: %v", edge)
			}
		}
		unresolved, nonClass := 0, 0
		for _, diagnostic := range snapshot.Diagnostics {
			if !strings.HasPrefix(diagnostic.Message, classes[name].Identity+" metadata ") {
				continue
			}
			if diagnostic.Severity != "info" || diagnostic.Code != "angular_scope_entry_unresolved" {
				t.Errorf("unexpected revision diagnostic: %v", diagnostic)
			}
			unresolved++
			if strings.HasSuffix(diagnostic.Message, "target is not a class declaration") {
				nonClass++
			}
		}
		want, wantNonClass := 1, 0
		if name == "RevisionModule" {
			want, wantNonClass = 4, 2
		}
		if unresolved != want || nonClass != wantNonClass {
			t.Errorf("%s unresolved/non-class diagnostics: %d/%d, want %d/%d", name, unresolved, nonClass, want, wantNonClass)
		}
	}
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.File == "v21/src/revision.ts" && diagnostic.Code == "angular_version_unknown" {
			t.Error("explicit unknown standalone looked up the version", diagnostic)
		}
	}
	for _, target := range []string{"Declared", "Attribute", "LocalPipe"} {
		if !has("declares", "Good", target) {
			t.Error("missing declaration", target)
		}
	}
	if attrs["Good"]["declarations"].(map[string]any)["unresolved"] != json.Number("1") {
		t.Error("forwardRef parameter shadow was not retained", attrs["Good"])
	}
	if attrs["Good"]["declarations"].(map[string]any)["linked"] != json.Number("10") {
		t.Error("bare/spread const and literal array expansion counts", attrs["Good"])
	}
	if attrs["Cyclic"]["declarations"].(map[string]any)["unresolved"] != json.Number("1") {
		t.Error("const array cycle did not terminate", attrs["Cyclic"])
	}
	if attrs["Good"]["imports"].(map[string]any)["external"] != json.Number("3") {
		t.Error("external entry counts", attrs["Good"])
	}
	if attrs["MutationCases"]["declarations"].(map[string]any)["unresolved"] != json.Number("2") {
		t.Error("cross-file alias/computed mutations were missed", attrs["MutationCases"])
	}
	if attrs["DynamicModule"]["declarations"].(map[string]any)["unresolved"] != json.Number("1") {
		t.Error("dynamic namespace mutation did not block its exported array", attrs["DynamicModule"])
	}
	for _, target := range []string{"JitModule", "Standalone", "StandalonePipe"} {
		if !has("scope_imports", "Good", target) {
			t.Error("missing import", target)
		}
	}
	if has("declares", "FirstOwner", "Duplicate") || has("declares", "SecondOwner", "Duplicate") || !has("declares", "TestOwner", "Duplicate") {
		t.Error("duplicate declaration scope handling")
	}
	if !has("declares", "SingleOwner", "Single") {
		t.Error("same-module repetition was rejected")
	}
	if !has("scope_imports", "InjectableComponent", "Good") || !has("scope_imports", "InjectableComponent", "StandalonePipe") || has("scope_imports", "NotStandalone", "Good") {
		t.Error("component imports")
	}
	for _, e := range snapshot.Edges {
		if strings.HasPrefix(e.Kind, "scope_") && e.Source == classes["InvalidEntries"].Identity {
			t.Error("invalid scope linked", e)
		}
		if e.Kind == "declares" && e.Source == classes["InvalidEntries"].Identity {
			t.Error("invalid declaration linked", e)
		}
	}
	for _, name := range []string{"Alias", "Namespace", "ParentPath", "Both"} {
		found := false
		for _, e := range snapshot.Edges {
			if e.Kind == "uses_template" && e.Source == classes[name].Identity {
				found = true
			}
		}
		if !found {
			t.Error("missing template edge", name)
		}
	}
	codes := map[string]int{}
	for _, d := range snapshot.Diagnostics {
		codes[d.Code]++
	}
	for _, code := range []string{"angular_decorator_conflict", "angular_decorator_indirect", "angular_decorator_invalid", "angular_metadata_partial", "angular_value_wrong_type", "angular_value_unevaluated", "angular_version_unknown", "angular_template_missing", "angular_template_unresolved", "angular_template_absent", "angular_template_inline_unparsed", "angular_scope_entry_invalid", "angular_scope_entry_unresolved", "angular_imports_not_standalone", "angular_duplicate_declaration"} {
		if codes[code] == 0 {
			t.Error("missing diagnostic", code)
		}
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(first) != string(again) || warm.Parsed != 0 || warm.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("replay differs", warm, writes)
	}
	// Link-time package, lockfile, ownership and template edits must not parse TS.
	changes := []struct {
		file, data string
		want       any
	}{
		{"v21/bun.lock", `{"packages":{"@angular/core":["@angular/core@18.2.0"],"@angular/compiler-cli":["@angular/compiler-cli@18.2.0"]}}`, false},
		{"v21/package.json", `{"name":"fixture-v21","dependencies":{"@angular/core":"^22.0.0"},"devDependencies":{"@angular/compiler-cli":"^22.0.0"}}`, false},
		{"v21/angular.json", `{"projects":{"app":{"sourceRoot":"elsewhere","root":""}}}`, false},
		{"v15/src/page.html", `<h1>changed</h1>`, false},
	}
	for _, change := range changes {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(change.file)), []byte(change.data), 0644); err != nil {
			t.Fatal(err)
		}
		_, sum, _ := emit(t, root, s, opt)
		wantParsed := 0
		if strings.HasSuffix(change.file, ".html") {
			wantParsed = 1
		}
		if sum.Parsed != wantParsed {
			t.Errorf("metadata edit %s reparsed %d files", change.file, sum.Parsed)
		}
		current, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range current.Nodes {
			if n.Name == "Modern" && n.Kind == "class" && n.Attributes["angular"].(map[string]any)["standalone"] != change.want {
				t.Error("version relink", change.file, n.Attributes)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "v15", "src", "missing.html"), []byte("<p>now present</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	_, resourceEdit, _ := emit(t, root, s, opt)
	if resourceEdit.Parsed != 1 {
		t.Fatal("resource addition reparsed unchanged TS", resourceEdit)
	}
	resourceSnapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range resourceSnapshot.Nodes {
		if n.Name == "Missing" && n.Kind == "class" && n.Attributes["angular"].(map[string]any)["template"].(map[string]any)["status"] != "linked" {
			t.Error("new resource did not relink", n)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "v21", "tsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@angular/core":["src/shim.ts"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, aliased, _ := emit(t, root, s, opt)
	if aliased.Parsed != 0 {
		t.Fatal("tsconfig edit parsed unchanged TS", aliased)
	}
	shadowed, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range shadowed.Nodes {
		if n.Name == "Modern" && n.Kind == "class" {
			if _, ok := n.Attributes["angular"]; ok {
				t.Fatal("stale role survived paths shadow", n)
			}
		}
	}
	// Truncated declaration payloads are misses even if ordinary graph facts survive.
	for _, f := range snapshot.Files {
		if f.Path != "v15/src/decorators.ts" {
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
		cached.Declarations.Mutations = nil
		payload, err = json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveFactCache(context.Background(), "fixture", f.Path, f.ContentHash, key, payload); err != nil {
			t.Fatal(err)
		}
		_, sum, _ := emit(t, root, s, opt)
		if sum.Parsed != 1 {
			t.Error("truncated payload reused", sum)
		}
	}
}

func TestAngularDeclarationsShadow(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-declarations-shadow")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "core", "_package.json"), filepath.Join(root, "core", "package.json")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/angular-declarations-shadow.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	for _, n := range snapshot.Nodes {
		if _, ok := n.Attributes["angular"]; ok {
			t.Fatal("scanned core assigned a role", n)
		}
	}
	count := 0
	for _, d := range snapshot.Diagnostics {
		if d.Code == "angular_decorator_unproven" {
			count++
		}
	}
	if count != 2 {
		t.Fatal("unproven decorators", count)
	}
}
