package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

func assertAttributeControls(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case string:
		for _, c := range v {
			if c <= 31 {
				t.Fatalf("control character in attribute %q", v)
			}
		}
	case map[string]any:
		for key, value := range v {
			assertAttributeControls(t, key)
			assertAttributeControls(t, value)
		}
	case []any:
		for _, value := range v {
			assertAttributeControls(t, value)
		}
	}
}

func TestScopeGoldenAndConfigReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/test-scope")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	_, _, _ = emit(t, root, s, opt)
	first, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	projection := store.Snapshot{}
	byID := map[string]contract.Node{}
	links := map[string][]contract.Edge{}
	for _, n := range first.Nodes {
		byID[n.Identity] = n
		if n.Kind == "route" || n.Kind == "client_call" {
			projection.Nodes = append(projection.Nodes, n)
			assertAttributeControls(t, n.Attributes)
		} else if _, exists := n.Attributes["scope"]; exists {
			t.Fatal("scope on other kind", n)
		}
	}
	for _, e := range first.Edges {
		if e.Kind == "calls_route" {
			projection.Edges = append(projection.Edges, e)
			links[e.Source] = append(links[e.Source], e)
		}
	}
	for _, d := range first.Diagnostics {
		if strings.HasPrefix(d.Code, "extract_") || d.Code == "parse_failed" {
			t.Fatal(d)
		}
	}
	wanted := map[int]string{2: "linked", 3: "test_only", 4: "ambiguous", 5: "test_only", 6: "test_only", 7: "linked", 8: "test_only", 9: "test_only", 10: "test_only", 11: "linked", 12: "dynamic", 16: "test_only"}
	seen := map[int]bool{}
	for _, n := range projection.Nodes {
		a := n.Attributes
		if n.Kind == "client_call" && n.File == "frontend.ts" {
			line := n.Span.StartLine
			seen[line] = true
			if a["status"] != wanted[line] {
				t.Fatalf("line %d: %v, want %s", line, a, wanted[line])
			}
			if a["scope"] != nil {
				t.Fatal("production wrapper invocation inherited body scope", n)
			}
			p := a["paths"].([]any)[0].(map[string]any)
			if line == 4 && fmt.Sprint(p["candidate_count"]) != "2" {
				t.Fatal("production-only ambiguity", p)
			}
			if a["status"] == "test_only" && len(links[n.Identity]) != 0 {
				t.Fatal("test_only linked", n)
			}
			for _, id := range p["candidates"].([]any) {
				r := byID[id.(string)]
				if a["status"] == "test_only" && r.Attributes["scope"] != "test" && line != 6 {
					t.Fatal("wrong test-only target", r)
				}
				if line == 4 && r.Attributes["scope"] == "test" {
					t.Fatal("test candidate kept", r)
				}
			}
			if len(links[n.Identity]) > 0 && links[n.Identity][0].Confidence != .9 {
				t.Fatal("confidence changed")
			}
		}
		if n.Kind == "client_call" && n.File == "check.spec.ts" {
			p := a["paths"].([]any)[0].(map[string]any)
			if a["scope"] != "test" || n.Span.StartLine == 2 && (a["status"] != "ambiguous" || fmt.Sprint(p["candidate_count"]) != "2") || n.Span.StartLine == 3 && a["status"] != "linked" {
				t.Fatal("test call scope filtering", n)
			}
		}
		if n.Kind == "route" {
			if n.File == "kept.test.ts" && a["scope"] != nil {
				t.Fatal("exclude did not win")
			}
			if n.File == "checks/config.ts" && a["scope"] != "test" {
				t.Fatal("include missing")
			}
			if n.File == "t011g_backend.py" && n.QualifiedName == "GET /child" {
				paths := a["paths"].([]any)
				if len(paths) != 4 {
					t.Fatal("child paths", paths)
				}
				for _, v := range paths {
					p := v.(map[string]any)
					if (p["scope"] == "test") != (p["template"] == "/t011g/test/child" || p["template"] == "/t011g/body/child") {
						t.Fatal("chain scope merge", p)
					}
				}
			}
		}
	}
	if len(seen) != len(wanted) {
		t.Fatal("missing client sites", seen)
	}
	for _, command := range []string{"callers", "impact"} {
		term := "route:tests/test_apps.py#GET%20%2Ft011g%2Fonly"
		if command == "impact" {
			term = "function:tests/test_apps.py#only"
		}
		response, err := query.Execute(context.Background(), s, query.Options{Command: command, Term: term, Root: root, Repository: "fixture", Depth: 4})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, r := range response.Results {
			found = found || r.Node.Identity == "function:check.spec.ts#testSend"
		}
		if !found {
			t.Fatal(command, "omits test caller")
		}
	}
	golden.Check(t, "../../testdata/fixtures/test-scope.golden.json", projection)
	_, replay, writes := emit(t, root, s, opt)
	if replay.Parsed != 0 || writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatalf("warm replay: %+v %+v", replay, writes)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\ntests:\n  include: ['checks/**']\n  exclude: ['kept.test.ts', 'check.spec.ts', 'tests/**', '__tests__/**']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 0 {
		t.Fatalf("config edit parsed %d files", changed.Parsed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind != "route" && n.Kind != "client_call" {
			continue
		}
		old := byID[n.Identity]
		if old.ContentHash != n.ContentHash || !reflect.DeepEqual(old.Span, n.Span) {
			t.Fatal("identity/source changed", n)
		}
		if n.Kind == "client_call" && n.File == "frontend.ts" && n.Span.StartLine == 3 && n.Attributes["status"] != "linked" {
			t.Fatal("config did not relink", n)
		}
	}
	// Exercise recursive controls over every existing fixture, after JSON transport.
	for _, name := range []string{"clients", "url-evaluation", "express", "fastapi", "axum", "fastapi-helpers"} {
		t.Run(name, func(t *testing.T) {
			data, _, _ := emit(t, "../../testdata/fixtures/"+name, open(t), options(t))
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var n contract.Node
				if err := json.Unmarshal([]byte(line), &n); err != nil {
					t.Fatal(err)
				}
				if n.Type == "node" && (n.Kind == "route" || n.Kind == "client_call") {
					assertAttributeControls(t, n.Attributes)
				}
			}
		})
	}
}
