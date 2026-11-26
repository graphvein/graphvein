package index_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func TestOrvalGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/orval")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/orval.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	checkOrvalTransport(t, snapshot)
	count, links, mismatches, overrides := 0, 0, 0, 0
	kinds := map[string]int{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" || n.Attributes["library"] != "orval" {
			continue
		}
		count++
		if strings.HasPrefix(n.File, "proven/") {
			kinds[n.Attributes["invocation"].(string)]++
		}
		if !strings.HasPrefix(n.File, "proven/") && !strings.HasPrefix(n.File, "nomutator/") && !strings.HasPrefix(n.File, "alias/") && !strings.HasPrefix(n.File, "path-write/") && !strings.HasPrefix(n.File, "arrow/") && !strings.HasPrefix(n.File, "function-expression/") && !strings.HasPrefix(n.File, "shadow-fetch/") {
			t.Fatalf("unproven project emitted node: %s", n.Identity)
		}
		if n.Attributes["operation"] == nil {
			mismatches++
			if n.Attributes["status"] != "unmatched" || len(n.Attributes["paths"].([]any)) != 0 || n.Attributes["wrapper"] != nil {
				t.Fatal("mismatch gained a transport claim", n)
			}
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_operation" {
			links++
			if e.Provenance != "inferred" || e.Confidence != .9 {
				t.Fatal(e)
			}
		}
	}
	unmodeled, unproven := 0, 0
	for _, d := range snapshot.Diagnostics {
		if d.Code == "generated_client_operation_unmodeled" {
			unmodeled++
		}
		if d.Code == "generated_client_model_unproven" {
			unproven++
		}
		if strings.Contains(d.Message, "api_operation:") {
			if d.Code != "generated_client_operation_unmodeled" || d.Severity != "info" ||
				!strings.Contains(d.Message, "multiple request content types") && !strings.Contains(d.Message, "unsupported or absent operationId") {
				t.Fatal("operation diagnostic", d)
			}
		} else if d.Code == "generated_client_operation_unmodeled" {
			t.Fatal("operation diagnostic lacks operation identity", d)
		}
		if d.Code == "generated_client_override" {
			overrides++
		}
	}
	// alias/ adds one project: two unmodeled operations,
	// one client call and one calls_operation edge.
	if unmodeled != 14 || unproven != 5 {
		t.Fatalf("operation diagnostics=%d project diagnostics=%d", unmodeled, unproven)
	}
	if count != 20 || links != 19 || mismatches != 1 || overrides != 4 || kinds["direct"] != 7 || kinds["query_options"] != 6 || kinds["hook"] != 1 {
		t.Fatalf("count=%d links=%d mismatches=%d overrides=%d kinds=%v", count, links, mismatches, overrides, kinds)
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("Orval replay differs", warm, writes)
	}
	pkg := filepath.Join(root, "proven", "package.json")
	before, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pkg, []byte(strings.ReplaceAll(string(before), "^8.3.0", "^7.0.0")), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 0 {
		t.Fatal("package metadata edit reparsed source", changed)
	}
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "client_call" && n.Attributes["library"] == "orval" && strings.HasPrefix(n.File, "proven/") {
			t.Fatal("stale model after package edit", n.Identity)
		}
	}
}

func checkOrvalTransport(t *testing.T, snapshot store.Snapshot) {
	t.Helper()
	links := map[string][]float64{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			links[e.Source] = append(links[e.Source], e.Confidence)
		}
	}
	positives := map[string]bool{"assigned": true, "hostAssigned": true, "literal": true, "localAlias": true, "moduleUse": true}
	urlCount := 0
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		if n.File == "url-objects.ts" {
			urlCount++
			owner := strings.Split(n.QualifiedName, ".")[0]
			path := n.Attributes["paths"].([]any)[0].(map[string]any)
			if positives[owner] {
				if n.Attributes["status"] != "linked" || len(links[n.Identity]) != 1 || links[n.Identity][0] != .7 || path["match"] != "exact" {
					t.Fatal("URL object proof", n)
				}
				if owner == "assigned" || owner == "hostAssigned" {
					if path["origin"] != nil || path["origin_resolution"] != "unresolved" {
						t.Fatal("assigned authority retained origin", n)
					}
				} else if path["origin"] != "https://fixture" {
					t.Fatal("literal URL origin lost", n)
				}
			} else if n.Attributes["status"] != "dynamic" || path["template"] != nil || len(links[n.Identity]) != 0 {
				t.Fatal("unsupported URL object linked", n)
			}
		}
		if n.Attributes["library"] != "orval" || n.Attributes["operation"] == nil {
			continue
		}
		path := n.Attributes["paths"].([]any)[0].(map[string]any)
		if n.File == "path-write/app.ts" {
			if path["template"] != nil || len(links[n.Identity]) != 0 {
				t.Fatal("pathname-writing mutator linked", n)
			}
			continue
		}
		template := "/t012e/articles/{slug}"
		// alias/ is nomutator/ imported through a tsconfig paths
		// alias.
		if n.File == "nomutator/app.ts" || n.File == "alias/app.ts" {
			template = "/t012e" + template
		}
		if strings.Contains(n.Attributes["operation"].(string), "%7Bslug%7D") && path["template"] != template {
			t.Fatal("generated path parameter name lost", n)
		}
		confidence := .7
		// alias/ is nomutator/ imported through a tsconfig paths
		// alias.
		if n.File == "nomutator/app.ts" || n.File == "alias/app.ts" {
			confidence = .9
		}
		if len(links[n.Identity]) != 1 || links[n.Identity][0] != confidence || path["resolution"] != "generated" {
			t.Fatal("Orval transport or confidence", n)
		}
	}
	if urlCount != 20 {
		t.Fatal("URL fixture coverage", urlCount)
	}
}
