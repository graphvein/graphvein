package typescript_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	bridgesFixture = "../../../testdata/fixtures/ts-bridges"
	bridgesDoc     = "../../../docs/lang/typescript-bridges.md"
)

var bridgeRowID = regexp.MustCompile(`^\|\s*((?:JR|PF|RR)[0-9]+)\s*\|`)

// bridgesProjection keeps the bridge records of a snapshot: ui_route
// nodes and renders, passes_function, routes_to and route contains edges.
type bridgesProjection struct {
	Nodes []contract.Node
	Edges []contract.Edge
}

// indexBridges indexes the fixture twice (cold, then a warm replay that
// parses nothing and must emit the same stream) and returns the projection.
func indexBridges(t *testing.T) bridgesProjection {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(bridgesFixture)); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	var p bridgesProjection
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
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			p = projectBridges(snapshot)
		}
	}
	return p
}

func projectBridges(snapshot store.Snapshot) bridgesProjection {
	p := bridgesProjection{Nodes: []contract.Node{}, Edges: []contract.Edge{}}
	for _, n := range snapshot.Nodes {
		if n.Kind == "ui_route" {
			p.Nodes = append(p.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "renders" || e.Kind == "passes_function" || e.Kind == "routes_to" || e.Kind == "contains" && strings.HasPrefix(e.Target, "ui_route:") {
			p.Edges = append(p.Edges, e)
		}
	}
	return p
}

// TestTypeScriptBridgesFixture checks the bridge records of the fixture
// against testdata/fixtures/ts-bridges.golden.json and a few cases by name.
func TestTypeScriptBridgesFixture(t *testing.T) {
	p := indexBridges(t)
	golden.Check(t, bridgesFixture+".golden.json", p)
	has := map[string]bool{}
	for _, e := range p.Edges {
		has[e.Kind+" "+e.Source+" -> "+e.Target] = true
	}
	for _, want := range []string{
		"renders function:src/items.tsx#Items -> function:src/items.tsx#ItemsTableContent",
		"renders file:src/main.tsx#src%2Fmain.tsx -> function:src/App.tsx#App",
		"renders function:src/components/layout.tsx#Layout -> function:src/components/ui.tsx#Button",
		"passes_function function:src/items.tsx#Items -> function:src/items.tsx#createItem",
		"passes_function file:src/items.tsx#src%2Fitems.tsx -> function:src/items.tsx#Items",
		"routes_to ui_route:src/pages/article/article.route.ts#articleRoute.ROUTE%20article%2F%3Aslug -> function:src/pages/article/article.ui.tsx#ArticlePage",
		"passes_function ui_route:src/pages/article/article.route.ts#articleRoute.ROUTE%20article%2F%3Aslug -> function:src/pages/article/article.loader.ts#articleLoader",
	} {
		if !has[want] {
			t.Errorf("missing edge %s", want)
		}
	}
	for key := range has {
		// Shadowed names and unproven lazy bindings claim nothing; a local
		// component named Route is rendered, never a route.
		for _, bad := range []string{"#List -> function:src/items.tsx#render", "#Callbacks -> function:src/items.tsx#ItemsTableContent", "ui_route:src/legacy-routes.tsx#ROUTE", "ui_route:src/legacy-routes.tsx#fake", "#NotReact", "#makeLazy"} {
			if strings.Contains(key, bad) {
				t.Errorf("unexpected edge %s", key)
			}
		}
	}
}

// TestTypeScriptBridgesContract keeps bridgeRows and the tables of
// docs/lang/typescript-bridges.md 1:1 and checks that indexing the
// fixture reaches every row and the default branch D, and no branch
// reports an id outside the table.
func TestTypeScriptBridgesContract(t *testing.T) {
	data, err := os.ReadFile(bridgesDoc)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if m := bridgeRowID.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	var code []string
	rows := map[string]bool{}
	for _, row := range typescript.BridgeRows {
		rows[row.ID] = true
		if row.ID != "D" {
			code = append(code, row.ID)
		}
	}
	if strings.Join(ids, " ") != strings.Join(code, " ") {
		t.Errorf("document rows %v, bridgeRows %v", ids, code)
	}
	if !strings.Contains(string(data), "\n## Default branch D\n") {
		t.Error("the document has no \"## Default branch D\" section")
	}
	var mu sync.Mutex
	reached := map[string]bool{}
	typescript.SetBridgeTagHook(func(row string) {
		mu.Lock()
		defer mu.Unlock()
		reached[row] = true
	})
	defer typescript.SetBridgeTagHook(nil)
	indexBridges(t)
	for _, row := range typescript.BridgeRows {
		if !reached[row.ID] {
			t.Errorf("row %s: no fixture case reaches it", row.ID)
		}
	}
	for id := range reached {
		if !rows[id] {
			t.Errorf("a branch reports %s, which is not a contract row", id)
		}
	}
}
