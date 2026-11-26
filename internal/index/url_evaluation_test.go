package index_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
)

func TestURLEvaluationGoldenAndTableReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/url-evaluation")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var projection []map[string]any
	sites := map[int]contract.Node{}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "extract_failed" || d.Code == "extract_invalid_record" || d.Code == "parse_failed" {
			t.Fatal(d)
		}
	}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		projection = append(projection, map[string]any{"file": n.File, "line": n.Span.StartLine, "status": n.Attributes["status"], "base": n.Attributes["base"], "paths": n.Attributes["paths"], "helpers": n.Attributes["helpers"]})
		if n.File == "frontend.ts" {
			sites[n.Span.StartLine] = n
		}
	}
	sort.Slice(projection, func(i, j int) bool {
		if projection[i]["file"] != projection[j]["file"] {
			return projection[i]["file"].(string) < projection[j]["file"].(string)
		}
		return projection[i]["line"].(int) < projection[j]["line"].(int)
	})
	for _, line := range []int{3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 18, 24, 25, 27, 28, 29, 33, 34, 39, 40} {
		if sites[line].Attributes["status"] != "linked" {
			t.Errorf("line %d must link: %v", line, sites[line].Attributes)
		}
	}
	for _, line := range []int{14, 15, 16, 20, 21, 22, 23, 30, 32, 35, 36, 37, 38, 42} {
		if sites[line].Attributes["status"] != "dynamic" {
			t.Errorf("line %d must remain dynamic: %v", line, sites[line].Attributes)
		}
	}
	if sites[26].Attributes["status"] != "ambiguous" {
		t.Error("Express slash twins must stay ambiguous", sites[26])
	}
	if paths := sites[12].Attributes["paths"].([]any); len(paths) != 2 {
		t.Error("unknown ternary alternatives", sites[12])
	}
	if paths := sites[13].Attributes["paths"].([]any); len(paths) != 1 || paths[0].(map[string]any)["conditional"] != true {
		t.Error("query-only ternary dedup", sites[13])
	}
	for _, edge := range snapshot.Edges {
		if edge.Kind == "calls_route" && edge.Source == sites[5].Identity {
			if len(edge.Evidence) < 3 {
				t.Error("missing table property and return evidence", edge)
			}
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	golden.Check(t, "../../testdata/fixtures/url-evaluation.golden.json", projection)
	warmData, warm, _ := emit(t, root, s, opt)
	if string(warmData) != string(data) || warm.Parsed != 0 || warm.Reused != first.Parsed {
		t.Fatal("cached replay", warm)
	}
	file := filepath.Join(root, "urls.ts")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(source), "DOT: '/api/dot'", "DOT: '/api/b'", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 {
		t.Fatal("table edit reparsed unchanged callers", changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind == "client_call" && n.File == "frontend.ts" && (n.Span.StartLine == 3 || n.Span.StartLine == 4) {
			if n.Attributes["paths"].([]any)[0].(map[string]any)["template"] != "/api/b" {
				t.Fatal("stale table result", n)
			}
		}
	}
	fresh := open(t)
	emit(t, root, fresh, opt)
	full, err := fresh.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(full)
	if string(a) != string(b) {
		t.Fatal("table relink differs from fresh")
	}
}
