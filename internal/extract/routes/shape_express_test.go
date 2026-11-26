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
	expressShapeFixture  = "../../../testdata/fixtures/express-shape"
	expressShapeContract = "../../../docs/contract/claims/route-shape-express.md"
)

// expressIndexShapeFixture indexes a copy of the fixture with the given shape
// depth (cold, then a warm replay that must parse nothing and emit the
// same stream) and returns the snapshot and the rows reached.
func expressIndexShapeFixture(t *testing.T, depth int) (store.Snapshot, map[string]bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(expressShapeFixture)); err != nil {
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

type expressShapeRoute struct {
	Identity string `json:"identity"`
	Shape    any    `json:"shape"`
}

type expressShapeGolden struct {
	Routes      []expressShapeRoute `json:"routes"`
	Diagnostics []string            `json:"diagnostics"`
}

func expressShapeView(s store.Snapshot) expressShapeGolden {
	out := expressShapeGolden{Routes: []expressShapeRoute{}, Diagnostics: []string{}}
	for _, n := range s.Nodes {
		if n.Kind == "route" {
			out.Routes = append(out.Routes, expressShapeRoute{n.Identity, n.Attributes[routeshape.AttributeKey]})
		}
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Identity < out.Routes[j].Identity })
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_shape") {
			out.Diagnostics = append(out.Diagnostics, d.File+" "+d.Code+": "+d.Message)
		}
	}
	sort.Strings(out.Diagnostics)
	return out
}

// expressShapeDocRows reads the claim table of the contract document.
func expressShapeDocRows(t *testing.T) []routes.ExpressShapeRow {
	t.Helper()
	data, err := os.ReadFile(expressShapeContract)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var rows []routes.ExpressShapeRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-E") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		rows = append(rows, routes.ExpressShapeRow{ID: strings.TrimSpace(cells[1]), Title: strings.TrimSpace(cells[2])})
	}
	return rows
}

// TestExpressShapeContract indexes the fixture, checks every route's shape
// against the golden and the model rules, keeps the code table equal to
// the document's, and checks that the fixture reaches every row.
func TestExpressShapeContract(t *testing.T) {
	snapshot, tags := expressIndexShapeFixture(t, routeshape.DefaultDepth)
	view := expressShapeView(snapshot)
	golden.Check(t, expressShapeFixture+".golden.json", view)
	for _, r := range view.Routes {
		if r.Shape == nil {
			t.Errorf("%s: no shape", r.Identity)
			continue
		}
		if _, err := routeshape.FromAttribute(r.Shape); err != nil {
			t.Errorf("%s: %v", r.Identity, err)
		}
	}
	if docs := expressShapeDocRows(t); !reflect.DeepEqual(docs, routes.ExpressShapeRows) {
		t.Errorf("code rows %v\ndocument rows %v", routes.ExpressShapeRows, docs)
	}
	known := map[string]bool{}
	for _, r := range routes.ExpressShapeRows {
		known[r.ID] = true
		if !tags[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for tag := range tags {
		// The security rows (SH-S) share the hook; their own test checks
		// them (TestSecurityShapeFixtureRows).
		if !known[tag] && !strings.HasPrefix(tag, "SH-S") {
			t.Errorf("tag %s names no row", tag)
		}
	}
}

// TestExpressShapeDepth indexes the fixture with depth limits 2 and 10:
// the order chain (three calls) hits the cap at 2 and ends at 10, and the
// eight-call chain hits the cap at both 6 (golden) and 2 but not at 10.
func TestExpressShapeDepth(t *testing.T) {
	caps := func(depth int) map[string]int {
		snapshot, _ := expressIndexShapeFixture(t, depth)
		out := map[string]int{}
		for _, n := range snapshot.Nodes {
			if n.Kind != "route" {
				continue
			}
			s, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
			if err != nil {
				t.Fatalf("%s: %v", n.Identity, err)
			}
			if s.Follow.DepthLimit != depth {
				t.Errorf("%s: depth limit %d, want %d", n.Identity, s.Follow.DepthLimit, depth)
			}
			out[n.QualifiedName] = routeshape.Summarize(s).DepthCap
		}
		return out
	}
	low, high := caps(2), caps(routeshape.MaxDepthLimit)
	if low["POST /orders"] == 0 || high["POST /orders"] != 0 {
		t.Errorf("POST /orders depth caps: %d at 2, %d at 10", low["POST /orders"], high["POST /orders"])
	}
	if low["POST /deep"] == 0 || high["POST /deep"] != 0 {
		t.Errorf("POST /deep depth caps: %d at 2, %d at 10", low["POST /deep"], high["POST /deep"])
	}
}
