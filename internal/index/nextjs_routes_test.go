package index_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	nextFixture  = "../../testdata/fixtures/nextjs-routes"
	nextGolden   = "../../testdata/fixtures/nextjs-routes.golden.json"
	nextTaskPath = "../../docs/contract/claims/nextjs-supabase.md"
	nextDocsPath = "../../docs/extract/routes.md"
)

// nextSnapshot keeps what T024a decides: Next.js route nodes, their
// handler edges, client calls with their links, and the counted refusals.
func nextSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"routes": {}, "exposes_route": {}, "client_calls": {}, "calls_route": {}, "diagnostics": {}}
	keep := func(attrs map[string]any, keys ...string) string {
		picked := map[string]any{}
		for _, k := range keys {
			if v, ok := attrs[k]; ok {
				picked[k] = v
			}
		}
		data, err := json.Marshal(picked)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, n := range s.Nodes {
		switch n.Kind {
		case "route":
			if f, _ := n.Attributes["framework"].(string); strings.HasPrefix(f, "nextjs") {
				out["routes"] = append(out["routes"], n.Identity+" "+keep(n.Attributes, "framework", "method", "pattern", "handler", "handler_expression", "paths", "base_path", "rewrites", "project", "scope"))
			}
		case "client_call":
			out["client_calls"] = append(out["client_calls"], n.Identity+" "+keep(n.Attributes, "library", "status", "paths", "assumes", "wrapper", "base"))
		}
	}
	for _, e := range s.Edges {
		switch e.Kind {
		case "exposes_route", "calls_route":
			out[e.Kind] = append(out[e.Kind], e.Source+" -> "+e.Target)
		}
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "nextjs_") || d.Code == "swr_call_unread" {
			out["diagnostics"] = append(out["diagnostics"], d.File+" "+d.Code+" "+d.Message)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// TestNextRoutesFixture indexes the fixture against its golden and checks
// that it reaches every row of the contract and D, and that every tag a
// branch reports is a row (the tag half of TestNextRoutesContract).
func TestNextRoutesFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(nextFixture)); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	routes.SetNextTagHook(func(row string) { tags[row]++ })
	defer routes.SetNextTagHook(nil)
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	routes.SetNextTagHook(nil)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, nextGolden, nextSnapshot(t, snapshot))
	// Decision U6 of T033: a link to a route whose basePath is not read
	// (NX-R4) is aligned with the unknown-prefix tier: suffix, 0.6; every
	// other Next.js link keeps its confidence.
	for _, e := range snapshot.Edges {
		if e.Kind != "calls_route" {
			continue
		}
		want := 0.9
		if strings.HasPrefix(e.Target, "route:docs/") {
			want = 0.6
		}
		if e.Confidence != want {
			t.Errorf("%s -> %s: confidence %v, want %v", e.Source, e.Target, e.Confidence, want)
		}
	}
	known := map[string]bool{}
	for _, r := range routes.NextContract {
		known[r.ID] = true
		if tags[r.ID] == 0 {
			t.Errorf("the fixture never reaches row %s", r.ID)
		}
	}
	for row := range tags {
		if !known[row] {
			t.Errorf("a branch reports %s, which is not a row", row)
		}
	}
}

// nextRows returns the rows of the Next.js claim contract table in a
// Markdown file: the table after heading, id to line, plus the order.
func nextRows(t *testing.T, file, heading string) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, heading)
	if start < 0 {
		t.Fatalf("%s: no %q section", file, heading)
	}
	section := text[start+len(heading):]
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}
	rows := map[string]string{}
	var order []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| id |") {
			id := strings.TrimSpace(strings.Split(line, "|")[1])
			rows[id] = line
			order = append(order, id)
		}
	}
	return rows, order
}

// TestNextRoutesContract keeps the code table (routes.NextContract), the
// docs table and the task file's table 1:1: same rows, same order, same
// assumptions, D last.
func TestNextRoutesContract(t *testing.T) {
	task, taskOrder := nextRows(t, nextTaskPath, "## Claim contract: Next.js routes (T024a, rows NX)")
	docs, docsOrder := nextRows(t, nextDocsPath, "### Claim contract (Next.js)")
	if !reflect.DeepEqual(task, docs) {
		for id, line := range task {
			if docs[id] != line {
				t.Errorf("row %s differs between the task file and the docs:\ntask: %s\ndocs: %s", id, line, docs[id])
			}
		}
		for id := range docs {
			if _, ok := task[id]; !ok {
				t.Errorf("docs row %s is not in the task file", id)
			}
		}
	}
	var ids []string
	for _, r := range routes.NextContract {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(taskOrder, ids) || !reflect.DeepEqual(docsOrder, ids) {
		t.Errorf("rows: task %v, docs %v, code %v", taskOrder, docsOrder, ids)
	}
	if ids[len(ids)-1] != "D" {
		t.Error("D is not the last row")
	}
	assumption := regexp.MustCompile(`NX-V[0-9]+`)
	for _, r := range routes.NextContract {
		cells := strings.Split(task[r.ID], "|")
		if len(cells) != 7 {
			t.Errorf("row %s: %d cells, want 5", r.ID, len(cells)-2)
			continue
		}
		got := assumption.FindAllString(cells[5], -1)
		if !reflect.DeepEqual(got, r.Assumptions) && !(len(got) == 0 && len(r.Assumptions) == 0) {
			t.Errorf("row %s: table assumptions %v, code %v", r.ID, got, r.Assumptions)
		}
		for _, a := range r.Assumptions {
			found := false
			for _, known := range routes.NextAssumptions {
				found = found || known == a
			}
			if !found {
				t.Errorf("row %s: assumption %s is not in NextAssumptions", r.ID, a)
			}
		}
	}
}
