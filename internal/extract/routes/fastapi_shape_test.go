package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	shapeFixture  = "../../../testdata/fixtures/route-shape-fastapi"
	shapeContract = "../../../docs/contract/claims/route-shape-fastapi.md"
	// handlersFixture is a separate project: its exception handlers (row
	// SH-F37) apply to every route of the index.
	handlersFixture = "../../../testdata/fixtures/route-shape-fastapi-handlers"
	// unpackFixture is a separate project too: keyword unpacking on a
	// FastAPI constructor may pass exception handlers (row SH-F37).
	unpackFixture = "../../../testdata/fixtures/route-shape-fastapi-unpack"
	// assignFixture: a default_response_class assignment applies to
	// every route of the project (row SH-F24).
	assignFixture = "../../../testdata/fixtures/route-shape-fastapi-assign"
)

var shapeTagMu sync.Mutex

// indexShapeFixture indexes root with the given shape depth (0: the
// default) and returns the snapshot and the reached claim tags. It
// indexes twice more on the same cache: once replaying (no file parsed,
// the same stream) and once with depth 2 when depth is 0 (no file parsed:
// following runs at link time), returning that snapshot too.
func indexShapeFixture(t *testing.T, root string, depth int) (store.Snapshot, map[string]bool) {
	t.Helper()
	s, _, tags := indexShapeRuns(t, root, depth)
	return s, tags
}

func indexShapeRuns(t *testing.T, root string, depth int) (store.Snapshot, store.Snapshot, map[string]bool) {
	t.Helper()
	ctx := context.Background()
	shapeTagMu.Lock()
	defer shapeTagMu.Unlock()
	tags := map[string]bool{}
	var mu sync.Mutex
	routes.SetShapeTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	defer routes.SetShapeTag(nil)
	cache := filepath.Join(t.TempDir(), "cache")
	var first []byte
	var snapshot, shallow store.Snapshot
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for run := 0; run < 3; run++ {
		opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z", ShapeDepth: depth, Parser: parse.Options{Workers: 2, CacheDir: cache}}
		if run == 2 {
			opt.ShapeDepth = 2
		}
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		switch run {
		case 0:
			first = data
			if snapshot, err = s.Dump(ctx, "fixture"); err != nil {
				t.Fatal(err)
			}
		case 1:
			if !bytes.Equal(first, data) || summary.Parsed != 0 {
				t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
			}
		case 2:
			if summary.Parsed != 0 {
				t.Fatalf("a new shape depth parsed %d files", summary.Parsed)
			}
			if shallow, err = s.Dump(ctx, "fixture"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return snapshot, shallow, tags
}

// goldenItem is one item of a section as the golden shows it.
type goldenItem struct {
	Key      string   `json:"key"`
	Kind     string   `json:"kind"`
	Rule     string   `json:"rule"`
	Access   string   `json:"access,omitempty"`
	Presence string   `json:"presence,omitempty"`
	Content  any      `json:"content_type,omitempty"`
	Schema   any      `json:"schema"`
	At       string   `json:"at"`
	Chain    []string `json:"chain,omitempty"`
}

type goldenSection struct {
	State    string       `json:"state"`
	Reason   string       `json:"reason,omitempty"`
	Evidence string       `json:"evidence,omitempty"`
	Items    []goldenItem `json:"items,omitempty"`
}

type goldenRoute struct {
	Identity string                   `json:"identity"`
	Sections map[string]goldenSection `json:"sections"`
	Chains   []string                 `json:"chains"`
	Models   []string                 `json:"models,omitempty"`
}

type goldenShapes struct {
	Routes  []goldenRoute  `json:"routes"`
	Models  map[string]any `json:"models"`
	Schemes map[string]any `json:"security_schemes"`
}

func at(p routeshape.Provenance) string { return fmt.Sprintf("%s:%d", p.File, p.Line) }

func chainOf(steps []routeshape.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, fmt.Sprintf("%s:%d %s", s.File, s.Line, s.Function))
	}
	return out
}

func schemaValue(t *testing.T, s *routeshape.Schema) any {
	t.Helper()
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// projectShapes projects the snapshot's route shapes for the golden.
func projectShapes(t *testing.T, s store.Snapshot) goldenShapes {
	t.Helper()
	out := goldenShapes{Models: map[string]any{}, Schemes: map[string]any{}}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		r := goldenRoute{Identity: n.Identity, Sections: map[string]goldenSection{}, Chains: []string{}}
		value, ok := n.Attributes[routeshape.AttributeKey]
		if !ok {
			out.Routes = append(out.Routes, r)
			continue
		}
		sh, err := routeshape.FromAttribute(value)
		if err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		section := func(name string, sec routeshape.Section, items []goldenItem) {
			g := goldenSection{State: string(sec.State), Reason: sec.Reason, Items: items}
			if sec.Evidence != nil {
				g.Evidence = sec.Evidence.Rule + " " + at(*sec.Evidence)
			}
			r.Sections[name] = g
		}
		for name, ps := range map[string]routeshape.ParamSection{"path": sh.Path, "query": sh.Query, "header": sh.Header, "cookie": sh.Cookie} {
			var items []goldenItem
			for _, p := range ps.Items {
				items = append(items, goldenItem{Key: p.Name, Kind: string(p.Provenance.Kind), Rule: p.Provenance.Rule, Access: string(p.Access), Presence: string(p.Presence), Schema: schemaValue(t, p.Schema), At: at(p.Provenance), Chain: chainOf(p.Provenance.Chain)})
			}
			section(name, ps.Section, items)
		}
		var items []goldenItem
		for _, c := range sh.Body.Items {
			items = append(items, goldenItem{Key: "body", Kind: string(c.Provenance.Kind), Rule: c.Provenance.Rule, Content: c.ContentType, Schema: schemaValue(t, c.Schema), At: at(c.Provenance), Chain: chainOf(c.Provenance.Chain)})
		}
		section("body", sh.Body.Section, items)
		items = nil
		for _, c := range sh.Responses.Items {
			items = append(items, goldenItem{Key: c.Status, Kind: string(c.Provenance.Kind), Rule: c.Provenance.Rule, Content: c.ContentType, Schema: schemaValue(t, c.Schema), At: at(c.Provenance), Chain: chainOf(c.Provenance.Chain)})
		}
		section("responses", sh.Responses.Section, items)
		items = nil
		for _, c := range sh.Security.Items {
			var names []string
			for _, u := range c.Schemes {
				names = append(names, u.Name)
			}
			items = append(items, goldenItem{Key: strings.Join(names, "+"), Kind: string(c.Provenance.Kind), Rule: c.Provenance.Rule, At: at(c.Provenance)})
		}
		section("security", sh.Security.Section, items)
		for _, c := range sh.Follow.Chains {
			line := fmt.Sprintf("%s depth %d %s", c.Section, c.Depth, c.Stop)
			if c.At != nil {
				line += fmt.Sprintf(" at %s:%d %s", c.At.File, c.At.Line, c.At.Function)
			}
			if c.Detail != "" {
				line += " (" + c.Detail + ")"
			}
			r.Chains = append(r.Chains, line)
		}
		for _, m := range sh.Models {
			r.Models = append(r.Models, m.Name)
			v := schemaValue(t, &m.Schema)
			if old, seen := out.Models[m.Name]; seen && !reflect.DeepEqual(old, v) {
				t.Errorf("model %s differs between routes", m.Name)
			}
			out.Models[m.Name] = v
		}
		for _, sc := range sh.Schemes {
			out.Schemes[sc.Name] = map[string]any{"type": sc.Type, "scheme": sc.Scheme, "bearer_format": sc.BearerFormat, "in": sc.In, "param_name": sc.ParamName, "token_url": sc.TokenURL, "openid_connect_url": sc.OpenIDConnectURL, "rule": sc.Provenance.Rule, "at": at(sc.Provenance)}
		}
		out.Routes = append(out.Routes, r)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Identity < out.Routes[j].Identity })
	return out
}

// TestFastAPIShapeFixture indexes the route-shape-fastapi fixture, checks
// the shapes against the golden (and the depth 2 run's chains against
// their own golden), and checks that the fixture reaches every SH-F row.
func TestFastAPIShapeFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(shapeFixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, shallow, tags := indexShapeRuns(t, root, 0)
	golden.Check(t, shapeFixture+".golden.json", projectShapes(t, snapshot))
	depth2 := projectShapes(t, shallow)
	var chains []goldenRoute
	for _, r := range depth2.Routes {
		chains = append(chains, goldenRoute{Identity: r.Identity, Sections: map[string]goldenSection{"query": r.Sections["query"], "header": r.Sections["header"], "responses": r.Sections["responses"]}, Chains: r.Chains})
	}
	golden.Check(t, shapeFixture+".depth2.golden.json", chains)
	handlersRoot := t.TempDir()
	if err := os.CopyFS(handlersRoot, os.DirFS(handlersFixture)); err != nil {
		t.Fatal(err)
	}
	handlers, _, handlerTags := indexShapeRuns(t, handlersRoot, 0)
	golden.Check(t, handlersFixture+".golden.json", projectShapes(t, handlers))
	for tag := range handlerTags {
		tags[tag] = true
	}
	unpackRoot := t.TempDir()
	if err := os.CopyFS(unpackRoot, os.DirFS(unpackFixture)); err != nil {
		t.Fatal(err)
	}
	unpack, _, unpackTags := indexShapeRuns(t, unpackRoot, 0)
	golden.Check(t, unpackFixture+".golden.json", projectShapes(t, unpack))
	for tag := range unpackTags {
		tags[tag] = true
	}
	assignRoot := t.TempDir()
	if err := os.CopyFS(assignRoot, os.DirFS(assignFixture)); err != nil {
		t.Fatal(err)
	}
	assign, _, assignTags := indexShapeRuns(t, assignRoot, 0)
	golden.Check(t, assignFixture+".golden.json", projectShapes(t, assign))
	for tag := range assignTags {
		tags[tag] = true
	}
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range routes.FastAPIShapeContract {
		if !reached[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for _, form := range []string{"SH-F35/depth_cap", "SH-F35/cycle", "SH-F35/joined", "SH-F35/unresolved", "SH-F35/ambiguous", "SH-F35/external",
		"SH-F35/transformed", "SH-F35/resolver_error", "SH-F23/complete", "SH-F23/provably_none", "SH-F3/complete", "SH-F3/provably_none",
		"SH-F24/no-body", "SH-F27/response_model=None", "SH-F27/-> None", "SH-F27/-> Response", "SH-F27/no annotation", "SH-F17/use",
		"SH-F15/header", "SH-F15/cookie", "SH-F15/query", "SH-F19/embed", "SH-F19/several", "SH-F20/multipart", "SH-F31/constructor",
		"SH-F8/enum", "SH-F8/literal", "SH-F14/Request", "SH-F14/Response",
		// The review's classes (SHFR-1..SHFR-16).
		"SH-F8/_missing_", "SH-F13/subclass", "SH-F20/list[UploadFile]", "SH-F20/form model", "SH-F22/metadata",
		"SH-F22/validator", "SH-F22/populate_by_name", "SH-F22/pydantic version", "SH-F24/class default", "SH-F24/cookie", "SH-F24/override",
		"SH-F26/validated", "SH-F29/after yield", "SH-F30/unreachable", "SH-F32/subclass", "SH-F32/token_url null",
		// Response defaults scoped to the routes an object serves (T029 FU-1).
		"SH-F24/default_response_class", "SH-F24/**options", "SH-F24/unread includer", "SH-F24/x.default_response_class = C", "SH-F37/**options", "SH-F38/**options"} {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
	// Every stored item's rule is a row of the table.
	rows := map[string]bool{}
	for _, r := range routes.FastAPIShapeContract {
		rows[r.ID] = true
	}
	all := append(projectShapes(t, snapshot).Routes, projectShapes(t, handlers).Routes...)
	all = append(all, projectShapes(t, unpack).Routes...)
	for _, r := range append(all, projectShapes(t, assign).Routes...) {
		for name, sec := range r.Sections {
			for _, it := range sec.Items {
				if !rows[it.Rule] {
					t.Errorf("%s %s: rule %s is not a row", r.Identity, name, it.Rule)
				}
			}
		}
	}
}

// TestFastAPIShapeContract keeps routes.FastAPIShapeContract and the
// claims document 1:1: the same rows in the same order with the same
// forms, every form listed once.
func TestFastAPIShapeContract(t *testing.T) {
	data, err := os.ReadFile(shapeContract)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	type row struct {
		id    string
		forms []string
	}
	var doc []row
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-F") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := row{id: strings.TrimSpace(cells[1])}
		for _, f := range strings.Split(cells[3], "`, `") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.forms = append(r.forms, f)
			}
		}
		doc = append(doc, r)
	}
	if len(doc) != len(routes.FastAPIShapeContract) {
		t.Fatalf("document has %d rows, code %d", len(doc), len(routes.FastAPIShapeContract))
	}
	seen := map[string]string{}
	for i, r := range routes.FastAPIShapeContract {
		if doc[i].id != r.ID || !reflect.DeepEqual(doc[i].forms, r.Forms) {
			t.Errorf("row %d: document %s %q, code %s %q", i, doc[i].id, doc[i].forms, r.ID, r.Forms)
		}
		for _, f := range r.Forms {
			if prev, dup := seen[f]; dup {
				t.Errorf("form %q in %s and %s", f, prev, r.ID)
			}
			seen[f] = r.ID
		}
	}
	if routes.FastAPIShapeContract[len(routes.FastAPIShapeContract)-1].ID != "D" {
		t.Error("the table must end with D")
	}
}

// TestFastAPIShapeRouteClassAssignment checks the second form of row
// SH-F38: a project that assigns route_class after constructing a router
// has every fact of its routes' request and response sections lowered.
func TestFastAPIShapeRouteClassAssignment(t *testing.T) {
	root := t.TempDir()
	app := `from fastapi import APIRouter, FastAPI
from fastapi.routing import APIRoute


class Logged(APIRoute):
    pass


router = APIRouter()
router.route_class = Logged


@router.get("/items/{item_id}")
def read_item(item_id: int, q: int):
    return {}


app = FastAPI()
app.include_router(router)
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(app), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, tags := indexShapeFixture(t, root, 0)
	if !tags["SH-F38"] {
		t.Error("row SH-F38 is not reached")
	}
	shapes := projectShapes(t, snapshot)
	if len(shapes.Routes) != 1 {
		t.Fatalf("%d routes, want 1", len(shapes.Routes))
	}
	for name, sec := range shapes.Routes[0].Sections {
		for _, it := range sec.Items {
			if it.Kind == "fact" {
				t.Errorf("%s %s: kind fact after a route_class assignment", name, it.Key)
			}
		}
		if name != "security" && sec.State != "partial" {
			t.Errorf("%s: state %s, want partial", name, sec.State)
		}
	}
}
