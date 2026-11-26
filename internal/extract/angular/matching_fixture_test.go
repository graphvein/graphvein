package angular_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestTemplateMatchingFixture(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../testdata/fixtures/angular-template-matching")); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "matching.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-26T00:00:00Z", MaxFileSize: 2 << 20, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "wasm")}}
	run := func() ([]byte, index.Summary) {
		t.Helper()
		var out bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &out, opt)
		if err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, bytes.NewReader(out.Bytes()), s); err != nil {
			t.Fatal(err)
		}
		return out.Bytes(), summary
	}
	first, _ := run()
	snapshot, err := s.Dump(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]map[string]any{}
	names := map[string]string{}
	for _, n := range snapshot.Nodes {
		names[n.Identity] = n.Name
		if n.Kind == "class" {
			if a, ok := n.Attributes["angular"].(map[string]any); ok {
				attrs[n.Name], _ = a["template_matching"].(map[string]any)
			}
		}
	}
	checkScope := func(name, want string) {
		t.Helper()
		if attrs[name]["scope"] != want {
			t.Errorf("%s scope: %v, want %s", name, attrs[name], want)
		}
	}
	for _, name := range []string{"Closed", "Attributes", "Structural", "ModuleHost", "StandaloneModuleHost", "UnknownBlock", "OrphanEmpty", "OrphanElse", "ConnectedElse", "TextRecovery", "ExpressionRecovery"} {
		checkScope(name, "complete")
	}
	checkScope("Open", "open")
	checkScope("Deferred", "unknown")
	checkScope("UnknownStandalone", "none")
	checkScope("Duplicate", "none")
	checkScope("DuplicateModuleHost", "invalid")
	checkScope("CycleHost", "invalid")
	checkScope("InvalidExportsHost", "invalid")
	checkCount := func(host, kind string, want map[string]int) {
		t.Helper()
		counts, _ := attrs[host][kind].(map[string]any)
		for key, v := range counts {
			if fmt.Sprint(v) != fmt.Sprint(want[key]) {
				t.Errorf("%s %s.%s = %v, want %d", host, kind, key, v, want[key])
			}
		}
	}
	checkCount("Closed", "elements", map[string]int{"entries": 3, "linked": 2, "unmatched": 1})
	checkCount("Closed", "pipes", map[string]int{"entries": 2, "linked": 1, "unmatched": 1})
	checkCount("Open", "elements", map[string]int{"entries": 2, "linked": 1, "external_scope": 1})
	checkCount("Open", "pipes", map[string]int{"entries": 2, "linked": 1, "external_scope": 1})
	checkCount("Attributes", "elements", map[string]int{"entries": 4, "linked": 1, "unknown": 3})
	checkCount("SecondaryOnly", "structural", map[string]int{"entries": 1, "unknown": 1})
	checkCount("Structural", "structural", map[string]int{"entries": 2, "linked": 1, "invalid": 1})
	checkCount("Unmodeled", "elements", map[string]int{"entries": 1, "unknown": 1})
	checkCount("DuplicatePipes", "pipes", map[string]int{"entries": 1, "unknown": 1})
	checkCount("UnknownPipes", "pipes", map[string]int{"entries": 2, "shadowable": 1, "unknown": 1})
	checkCount("Pipes", "pipes", map[string]int{"entries": 7, "linked": 3, "invalid": 3, "unknown": 1})
	checkCount("UnknownBlock", "elements", map[string]int{"entries": 3, "linked": 1, "unknown": 2})
	checkCount("OrphanEmpty", "elements", map[string]int{"entries": 2, "linked": 1, "unknown": 1})
	checkCount("OrphanElse", "elements", map[string]int{"entries": 2, "linked": 1, "unknown": 1})
	checkCount("ConnectedElse", "elements", map[string]int{"entries": 2, "linked": 2})
	checkCount("TextRecovery", "elements", map[string]int{"entries": 1, "unknown": 1})
	checkCount("ExpressionRecovery", "elements", map[string]int{"entries": 1, "linked": 1})
	checkCount("OrphanSwitch", "elements", map[string]int{"entries": 2, "unknown": 2})
	checkCount("Multiple", "elements", map[string]int{"entries": 1, "invalid": 1})
	checkCount("Names", "elements", map[string]int{"entries": 3, "unknown": 2, "unmatched": 1})
	edges := map[string][]string{}
	for _, e := range snapshot.Edges {
		if e.Kind != "renders" && e.Kind != "uses_pipe" {
			continue
		}
		if e.Provenance != "fact" || e.Confidence != 1 || len(e.Evidence) == 0 {
			t.Error("invalid matching evidence", e)
		}
		edges[names[e.Source]] = append(edges[names[e.Source]], names[e.Target])
	}
	for host, want := range map[string][]string{"Closed": {"Child", "Closed", "Dir", "LocalPipe"}, "Attributes": {"ValueSelector", "Compound", "Bindings", "X"}, "ModuleHost": {"Public"}, "StandaloneModuleHost": {"Public"}, "Multiple": {"BothDirective"}, "NonBindable": {"Child"}, "SharedOne": {"Child"}, "SharedTwo": {"Child"}, "CycleHost": nil, "Duplicate": nil, "DuplicateModuleHost": nil} {
		got := edges[host]
		// Edge identity sorting is by kind then target; compare sets independently.
		set := map[string]bool{}
		for _, name := range got {
			set[name] = true
		}
		wantSet := map[string]bool{}
		for _, name := range want {
			wantSet[name] = true
		}
		if !reflect.DeepEqual(set, wantSet) {
			t.Errorf("%s edges %v, want %v", host, got, want)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind != "renders" || names[e.Source] != "Attributes" {
			continue
		}
		want := map[string]int{"ValueSelector": 1, "Compound": 4, "Bindings": 4, "X": 4}[names[e.Target]]
		if len(e.Evidence) != want {
			t.Errorf("attribute evidence for %s: %v, want %d parts", names[e.Target], e.Evidence, want)
		}
	}
	if attrs["DotPath"] != nil || attrs["RootedPath"] != nil {
		t.Error("unresolved resource gained matching")
	}
	diagnostics := map[string]int{}
	for _, d := range snapshot.Diagnostics {
		diagnostics[d.Code]++
	}
	if diagnostics["angular_template_structure_unknown"] != 5 {
		t.Error("structural recovery/orphan diagnostics", diagnostics)
	}
	if diagnostics["angular_template_multiple_components"] != 1 {
		t.Error("multiple components", diagnostics)
	}
	if diagnostics["angular_pipe_invalid_context"] != 3 {
		t.Error("invalid pipes", diagnostics)
	}
	golden.Check(t, "../../../testdata/fixtures/angular-template-matching.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	again, replay := run()
	if replay.Parsed != 0 || !bytes.Equal(first, again) {
		t.Fatal("warm template matching differed", replay)
	}
	// A template-only edit must relink cached host declarations and remove edges.
	if err := os.WriteFile(filepath.Join(root, "shared.html"), []byte("<app-missing/>\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, edit := run()
	if edit.Parsed != 1 {
		t.Error("template-only edit reparsed hosts", edit)
	}
	for _, host := range []string{"SharedOne", "SharedTwo"} {
		for _, e := range snapshot.Edges {
			if e.Kind == "renders" && names[e.Source] == host {
				now, err := s.Edges(ctx, "fixture", []string{e.Source}, []string{"renders"}, store.Outgoing)
				if err != nil || len(now) != 0 {
					t.Error("stale shared-template edge", host, now, err)
				}
			}
		}
	}
}
