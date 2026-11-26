package routes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	nextShapeFixture  = "../../../testdata/fixtures/nextjs-shape"
	nextShapeContract = "../../../docs/contract/claims/route-shape-nextjs.md"
)

// nextIndexShapeFixture indexes a copy of the fixture with the given shape
// depth (cold, then a warm replay that must parse nothing and emit the
// same stream) and returns the snapshot and the rows reached.
func nextIndexShapeFixture(t *testing.T, depth int) (store.Snapshot, map[string]bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(nextShapeFixture)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	tags := map[string]bool{}
	routes.SetShapeTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { routes.SetShapeTag(nil) })
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z", ShapeDepth: depth, Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	var snapshot store.Snapshot
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0) {
			t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
		}
		first = data
		if run == 0 {
			if snapshot, err = s.Dump(ctx, "fixture"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return snapshot, tags
}

type nextShapeGolden struct {
	Routes      []expressShapeRoute `json:"routes"`
	Actions     []expressShapeRoute `json:"server_actions"`
	Diagnostics []string            `json:"diagnostics"`
}

func nextShapeView(s store.Snapshot) nextShapeGolden {
	out := nextShapeGolden{Routes: []expressShapeRoute{}, Actions: []expressShapeRoute{}, Diagnostics: []string{}}
	for _, n := range s.Nodes {
		switch n.Kind {
		case "route":
			out.Routes = append(out.Routes, expressShapeRoute{n.Identity, n.Attributes[routeshape.AttributeKey]})
		case "server_action":
			out.Actions = append(out.Actions, expressShapeRoute{n.Identity, n.Attributes[routeshape.AttributeKey]})
		}
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Identity < out.Routes[j].Identity })
	sort.Slice(out.Actions, func(i, j int) bool { return out.Actions[i].Identity < out.Actions[j].Identity })
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_shape") {
			out.Diagnostics = append(out.Diagnostics, d.File+" "+d.Code+": "+d.Message)
		}
	}
	sort.Strings(out.Diagnostics)
	return out
}

// nextShapeDocRows reads the claim table of the contract document.
func nextShapeDocRows(t *testing.T) []routes.NextShapeRow {
	t.Helper()
	data, err := os.ReadFile(nextShapeContract)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var rows []routes.NextShapeRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-N") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		rows = append(rows, routes.NextShapeRow{ID: strings.TrimSpace(cells[1]), Title: strings.TrimSpace(cells[2])})
	}
	return rows
}

// TestNextShapeContract indexes the fixture, checks every route's and
// server action's shape against the golden and the model rules, keeps
// the code table equal to the document's, checks that the fixture
// reaches every row, and that no shared Express row is reported on a
// Next.js route (every shared form maps to its SH-N row).
func TestNextShapeContract(t *testing.T) {
	snapshot, tags := nextIndexShapeFixture(t, routeshape.DefaultDepth)
	view := nextShapeView(snapshot)
	golden.Check(t, nextShapeFixture+".golden.json", view)
	for _, r := range append(append([]expressShapeRoute{}, view.Routes...), view.Actions...) {
		if r.Shape == nil {
			t.Errorf("%s: no shape", r.Identity)
			continue
		}
		if _, err := routeshape.FromAttribute(r.Shape); err != nil {
			t.Errorf("%s: %v", r.Identity, err)
		}
	}
	if docs := nextShapeDocRows(t); !reflect.DeepEqual(docs, routes.NextShapeRows) {
		t.Errorf("code rows %v\ndocument rows %v", routes.NextShapeRows, docs)
	}
	known := map[string]bool{}
	for _, r := range routes.NextShapeRows {
		known[r.ID] = true
		if !tags[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for tag := range tags {
		if !known[tag] {
			t.Errorf("tag %s names no SH-N row", tag)
		}
	}
}

// TestNextShapeDepth indexes the fixture with depth limits 2 and 10: the
// eight-call request chain of GET /api/deep hits the cap at 2 and 6 (the
// golden) but not at 10, and the three-call response chain hits it at 2
// only.
func TestNextShapeDepth(t *testing.T) {
	caps := func(depth int) (int, bool) {
		snapshot, _ := nextIndexShapeFixture(t, depth)
		for _, n := range snapshot.Nodes {
			if n.Kind != "route" || n.QualifiedName != "GET /api/deep" {
				continue
			}
			s, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
			if err != nil {
				t.Fatalf("%s: %v", n.Identity, err)
			}
			if s.Follow.DepthLimit != depth {
				t.Errorf("depth limit %d, want %d", s.Follow.DepthLimit, depth)
			}
			has203 := false
			for _, r := range s.Responses.Items {
				has203 = has203 || r.Status == "203"
			}
			return routeshape.Summarize(s).DepthCap, has203
		}
		t.Fatal("GET /api/deep not found")
		return 0, false
	}
	low, lowResp := caps(2)
	high, highResp := caps(routeshape.MaxDepthLimit)
	if low == 0 || high != 0 {
		t.Errorf("GET /api/deep depth caps: %d at 2, %d at 10", low, high)
	}
	if lowResp || !highResp {
		t.Errorf("GET /api/deep 203 response: %t at 2, %t at 10", lowResp, highResp)
	}
}
