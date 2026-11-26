package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/query"
)

func TestClientsGoldenAndConfigReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/clients")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var projection []map[string]any
	byID := map[string]contract.Node{}
	links := map[string]contract.Edge{}
	contains := map[string]bool{}
	for _, n := range snapshot.Nodes {
		byID[n.Identity] = n
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			links[e.Source] = e
		}
		if e.Kind == "contains" {
			contains[e.Target] = true
		}
	}
	for i := 2; i <= 12; i++ {
		routeID, _ := contract.Identity("route", "backend.ts", fmt.Sprintf("GET /api/tie@%d", i))
		handlerID, _ := contract.Identity("function", "backend.ts", fmt.Sprintf("handler%d", i))
		if byID[routeID].Attributes["handler"] != handlerID || byID[handlerID].Kind != "function" || !contains[handlerID] {
			t.Fatal("Revision 2 distinct handler proof", routeID, handlerID)
		}
		exposed := false
		for _, e := range snapshot.Edges {
			exposed = exposed || e.Kind == "exposes_route" && e.Source == handlerID && e.Target == routeID
		}
		if !exposed {
			t.Fatal("Revision 2 retargeted exposes_route", routeID)
		}
	}
	warnings := 0
	for _, d := range snapshot.Diagnostics {
		if d.Code == "route_duplicate_registration" && d.File == "backend.ts" && strings.HasPrefix(d.Message, "GET /api/tie ") {
			warnings++
			if !strings.Contains(d.Message, "handler identical=false, middleware identical=true") {
				t.Fatal("Revision 2 differing handler warning", d)
			}
		}
	}
	if warnings != 11 {
		t.Fatalf("Revision 2 warnings=%d, want 11", warnings)
	}
	firstID, _ := contract.Identity("client_call", "frontend.ts", "main.GET '/api/items'")
	repeatedID, _ := contract.Identity("client_call", "frontend.ts", "main.GET '/api/items'@2")
	if byID[firstID].Span == nil || byID[firstID].Span.StartLine != 13 || byID[repeatedID].Span == nil || byID[repeatedID].Span.StartLine != 63 {
		t.Fatalf("client declaration-order spans: first=%+v repeated=%+v", byID[firstID].Span, byID[repeatedID].Span)
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "extract_invalid_record" || d.Code == "extract_failed" || d.Code == "parse_failed" {
			t.Fatal(d)
		}
	}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		a := n.Attributes
		paths := a["paths"].([]any)
		p := paths[0].(map[string]any)
		if n.File == "frontend.ts" && n.Span.StartLine == 45 {
			if a["status"] != "ambiguous" || fmt.Sprint(p["candidate_count"]) != "12" || len(p["candidates"].([]any)) != 10 {
				t.Fatal("Revision 2 twelve-handler candidate cap", n)
			}
		}
		row := map[string]any{"file": n.File, "line": n.Span.StartLine, "library": a["library"], "method": a["method"], "paths": paths, "base": a["base"], "status": a["status"], "wrapper": a["wrapper"], "helpers": a["helpers"]}
		if e, ok := links[n.Identity]; ok {
			confidence := 0.9
			if p["origin"] != nil {
				confidence = 0.7
			}
			if p["match"] == "suffix" {
				confidence = 0.6
			}
			if e.Provenance != "inferred" || e.Confidence != confidence || len(e.Evidence) < 1 || e.Evidence[0].Span != *n.Span {
				t.Fatal("call evidence", e)
			}
			if byID[e.Target].Kind != "route" {
				t.Fatal("wrong target", e)
			}
		}
		if !contains[n.Identity] {
			t.Fatal("missing containment", n)
		}
		if n.File == "extras.ts" && n.Span.StartLine == 32 {
			found := false
			for _, e := range snapshot.Edges {
				if e.Kind == "contains" && e.Target == n.Identity && e.Source == "class:extras.ts#Owner" {
					found = true
				}
			}
			if !found {
				t.Fatal("class initializer has wrong nearest owner", n)
			}
		}
		if n.File == "frontend.ts" && !strings.HasPrefix(n.QualifiedName, "main.") && n.Span.StartLine < 64 {
			t.Fatal("wrong lexical owner", n)
		}
		source, err := os.ReadFile(filepath.Join(root, n.File))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(source), "\n")
		offset := func(line, col int) int {
			for _, s := range lines[:line-1] {
				col += len(s) + 1
			}
			return col
		}
		if n.ContentHash != contract.ContentHash(source[offset(n.Span.StartLine, n.Span.StartColumn):offset(n.Span.EndLine, n.Span.EndColumn)]) {
			t.Fatal("wrong span hash", n)
		}
		if n.File == "extras.ts" && n.Span.StartLine == 36 && strings.HasPrefix(string(source[offset(n.Span.StartLine, n.Span.StartColumn):]), "await") {
			t.Fatal("generic call span includes await", n)
		}
		if a["status"] == "ambiguous" {
			candidates := p["candidates"].([]any)
			if len(candidates) != 10 {
				t.Fatal("unbounded candidates", a)
			}
			ids := []string{}
			for _, v := range candidates {
				ids = append(ids, v.(string))
			}
			if !sort.StringsAreSorted(ids) {
				t.Fatal(ids)
			}
		}
		projection = append(projection, row)
	}
	sort.Slice(projection, func(i, j int) bool {
		if projection[i]["file"] != projection[j]["file"] {
			return projection[i]["file"].(string) < projection[j]["file"].(string)
		}
		if projection[i]["line"] != projection[j]["line"] {
			return projection[i]["line"].(int) < projection[j]["line"].(int)
		}
		return fmt.Sprint(projection[i]["method"]) < fmt.Sprint(projection[j]["method"])
	})
	golden.Check(t, "../../testdata/fixtures/clients.golden.json", projection)
	warmData, warm, _ := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed {
		t.Fatal("warm replay", warm)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  base_paths:\n    '.': /api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, configured, _ := emit(t, root, s, opt)
	if configured.Parsed != 0 {
		t.Fatal("config reparsed sources", configured)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind == "client_call" && n.File == "extras.ts" && n.Span.StartLine == 48 && (n.Attributes["status"] != "unmatched" || n.Attributes["base"] != "configured" || n.Attributes["paths"].([]any)[0].(map[string]any)["template"] != "/api/api/items") {
			t.Fatal("evaluated helper must use configured unresolved axios base", n)
		}
		if n.Kind == "client_call" && n.File == "frontend.ts" && n.Span.StartLine == 54 && n.Attributes["status"] != "linked" {
			t.Fatal("source-evaluated helper did not link", n)
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
		t.Fatal("config relink differs from fresh")
	}
	// Handler -> route -> call -> owner crosses the boundary with inferred labels.
	response, err := query.Execute(context.Background(), s, query.Options{Command: "impact", Term: "function:backend.ts#handler", Root: root, Repository: "fixture", Depth: 4})
	if err != nil {
		t.Fatal(err)
	}
	owner := false
	for _, r := range response.Results {
		if r.Node.Identity == "function:frontend.ts#main" {
			owner = true
			if r.Provenance != "inferred" || r.Confidence == nil || *r.Confidence != 0.9 {
				t.Fatal(r)
			}
		}
	}
	if !owner {
		t.Fatal("missing frontend owner")
	}
	response, err = query.Execute(context.Background(), s, query.Options{Command: "callers", Term: "route:backend.ts#GET%20%2Fapi%2Fitems", Root: root, Repository: "fixture", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	owner = false
	for _, r := range response.Results {
		owner = owner || r.Node.Identity == "function:frontend.ts#main"
	}
	if !owner {
		t.Fatal("route callers omit owner")
	}
	if err := os.Rename(filepath.Join(root, "_axios-package.json"), filepath.Join(root, "package.json")); err != nil {
		t.Fatal(err)
	}
	_, shadow, _ := emit(t, root, s, opt)
	if shadow.Parsed != 0 {
		t.Fatal("workspace metadata reparsed sources", shadow)
	}
	shadowed, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range shadowed.Nodes {
		if n.Kind == "client_call" && n.Attributes["library"] == "axios" {
			t.Fatal("scanned axios package did not shadow external transport", n)
		}
	}
}
