package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/shapediff"
	"github.com/graphvein/graphvein/internal/store"
)

// The shape-differences fixtures (T029 W4, rows SD-1..SD-12 of
// docs/contract/claims/shape-differences.md; fixture READMEs).
const (
	shapeDiffFixture        = "../../testdata/fixtures/shape-differences"
	shapeDiffProjectFixture = "../../testdata/fixtures/shape-differences-project"
	shapeDiffManifest       = "version: 1\nname: w4p\nmembers:\n  - repository: web\n    path: web\n  - repository: api\n    path: api\n"
	shapeDiffProjectRoute   = "route:app/main.py#POST%20%2Fw4p%2Fusers"
)

// shapeDiffView is the golden of the single-repository fixture: the
// stored sends attribute of every client call, and the comparison each
// route's endpoint detail serves per caller (null: nothing compared).
type shapeDiffView struct {
	Sends     map[string]any            `json:"sends"`
	Endpoints map[string]map[string]any `json:"endpoints"`
}

// TestShapeDifferences indexes the fixture on both backends and checks the
// sends attributes and the served comparisons against the golden, that
// GET /api/v0/api/flow serves the same comparison on each calls_route
// edge as the endpoint detail on its caller, and that the fixture reaches
// every row of the claim table.
func TestShapeDifferences(t *testing.T) {
	var mu sync.Mutex
	rows := map[string]bool{}
	reach := func(id string) {
		mu.Lock()
		rows[id] = true
		mu.Unlock()
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(shapeDiffFixture)); err != nil {
			t.Fatal(err)
		}
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		snapshot, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		view := shapeDiffView{Sends: map[string]any{}, Endpoints: map[string]map[string]any{}}
		var routes []string
		for _, n := range snapshot.Nodes {
			switch n.Kind {
			case "client_call":
				v, ok := n.Attributes[shapediff.AttributeKey]
				view.Sends[n.Identity] = v
				if !ok {
					reach("D")
					continue
				}
				sends, err := shapediff.FromAttribute(v)
				if err != nil {
					t.Errorf("%s: stored sends refused: %v", n.Identity, err)
					continue
				}
				reach("SD-1")
				for _, p := range []*shapediff.Part{sends.Body, sends.Query} {
					if p != nil {
						reach(p.Rule)
					}
				}
			case "route":
				routes = append(routes, n.Identity)
			}
		}
		sort.Strings(routes)
		for _, id := range routes {
			var detail struct {
				Callers []struct {
					Node struct {
						Identity string `json:"identity"`
					} `json:"node"`
					ShapeDifferences any `json:"shape_differences"`
				} `json:"callers"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(id), nil), &detail)
			per := map[string]any{}
			for _, c := range detail.Callers {
				per[c.Node.Identity] = c.ShapeDifferences
				reachedRows(c.ShapeDifferences, reach)
			}
			view.Endpoints[id] = per
			var flow struct {
				Edges []map[string]any `json:"edges"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/flow?scope=all&id="+url.QueryEscape(id), nil), &flow)
			for _, e := range flow.Edges {
				if e["kind"] != "calls_route" || e["target"] != id {
					continue
				}
				source, _ := e["source"].(string)
				if !reflect.DeepEqual(e["shape_differences"], per[source]) {
					t.Errorf("%s -> %s: flow edge %v, endpoint caller %v", source, id, e["shape_differences"], per[source])
				}
			}
		}
		golden.CheckRoot(t, shapeDiffFixture+".golden.json", view, root)
	})
	t.Run("project", func(t *testing.T) { shapeDiffProject(t, reach) })
	for _, r := range shapediff.Rows {
		if !rows[r.ID] {
			t.Errorf("the fixtures reach no case of row %s (%s)", r.ID, r.Title)
		}
	}
}

// reachedRows records the rows a served comparison shows.
func reachedRows(v any, reach func(string)) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	reach("SD-12")
	for _, c := range m["compared"].([]any) {
		reach(map[string]string{"body": "SD-6", "query": "SD-7"}[c.(string)])
	}
	diffs := m["differences"].([]any)
	if len(diffs) == 0 {
		reach("SD-10")
	}
	for _, d := range diffs {
		reach(d.(map[string]any)["row"].(string))
	}
	if m["cross_repository"] == true {
		reach("SD-11")
	}
}

// shapeDiffProject serves the project fixture: the web member's call to
// the api member's route is a cross-repository link, and its comparison
// is labelled as one (row SD-11).
func shapeDiffProject(t *testing.T, reach func(string)) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(shapeDiffProjectFixture)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(shapeDiffManifest), 0o644); err != nil {
			t.Fatal(err)
		}
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(root, "web"), w, "web")
		indexRepository(t, filepath.Join(root, "api"), w, "api")
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		var detail struct {
			Callers []any `json:"callers"`
			Project struct {
				Callers []struct {
					Identity         string `json:"identity"`
					ShapeDifferences any    `json:"shape_differences"`
				} `json:"callers"`
			} `json:"project"`
		}
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?repository=api&id="+url.QueryEscape(shapeDiffProjectRoute), nil), &detail)
		if len(detail.Callers) != 0 || len(detail.Project.Callers) != 1 {
			t.Fatalf("callers %v, project callers %v", detail.Callers, detail.Project.Callers)
		}
		c := detail.Project.Callers[0]
		reachedRows(c.ShapeDifferences, reach)
		got := map[string]any{c.Identity: c.ShapeDifferences}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"cross_repository":true`) {
			t.Errorf("project comparison not labelled as cross-repository: %s", data)
		}
		golden.CheckRoot(t, shapeDiffProjectFixture+".golden.json", got, root)
	})
}
