package routes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/testscope"
)

const goShapeFixture = "../../../testdata/fixtures/go-shape"

// goShapeSources reads the go-shape fixture.
func goShapeSources(t testing.TB) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(goShapeFixture, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".go") && filepath.Base(p) != "go.mod" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(goShapeFixture, p)
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// goShapeLink links the fixture at a depth limit, with the row tags.
func goShapeLink(t testing.TB, depth int, tags map[string]int) ([]contract.Node, []contract.Diagnostic) {
	t.Helper()
	return goShapeLinkSources(t, goShapeSources(t), depth, tags)
}

func goShapeLinkSources(t testing.TB, raw map[string][]byte, depth int, tags map[string]int) ([]contract.Node, []contract.Diagnostic) {
	t.Helper()
	files := goParse(t, raw)
	golang.Link(files, golang.ModulePaths(raw))
	if tags != nil {
		goShapeTag = func(row string) { tags[row]++ }
		defer func() { goShapeTag = nil }()
	}
	l := runGoDepth(files, raw, testscope.Rules{}, nil, depth)
	for _, d := range l.diagnostics {
		if d.Code == "route_shape_failed" {
			t.Errorf("%s: %s", d.File, d.Message)
		}
	}
	return l.nodes, l.diagnostics
}

const goShapeDoc = "../../../docs/contract/claims/route-shape-go.md"

// TestGoShapeContractTable keeps goShapeRows and the claim contract
// document 1:1 (ids, order, forms), checks that every form belongs to one
// row, and that the dispatch tables of the rules name exactly the forms of
// their rows.
func TestGoShapeContractTable(t *testing.T) {
	data, err := os.ReadFile(goShapeDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var docs []goShapeRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-G") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := goShapeRow{ID: strings.TrimSpace(cells[1]), Title: strings.TrimSpace(cells[2])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.Forms = append(r.Forms, f)
			}
		}
		docs = append(docs, r)
	}
	if len(docs) != len(goShapeRows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(goShapeRows))
	}
	owner := map[string]string{}
	for i, r := range goShapeRows {
		d := docs[i]
		if d.ID != r.ID || d.Title != r.Title || strings.Join(d.Forms, ",") != strings.Join(r.Forms, ",") {
			t.Errorf("row %d: document %s %q %v, code %s %q %v", i, d.ID, d.Title, d.Forms, r.ID, r.Title, r.Forms)
		}
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	if last := goShapeRows[len(goShapeRows)-1]; last.ID != "D" || len(goShapeRows) > 40 {
		t.Error("rows must end with D and stay within SH-G1..SH-G39")
	}
	check := func(prefix string, table map[string]string) {
		for m, row := range table {
			if owner[prefix+m] != row {
				t.Errorf("%s%s dispatches to %s, the table says %q", prefix, m, row, owner[prefix+m])
			}
		}
	}
	check("gin:", ginMethods)
	check("http:", httpRequestMethods)
	check("http:", httpWriterMethods)
	for m := range goConversions {
		if owner["strconv:"+m] != "SH-G4" {
			t.Errorf("strconv.%s is not a form of SH-G4", m)
		}
	}
	for m := range ginUnread {
		if owner["gin:"+m] != "" {
			t.Errorf("gin:%s is both a row form and unread", m)
		}
	}
	// Every gin: and http: form is dispatched by a table or read by a
	// named rule of goshape.go.
	elsewhere := map[string]bool{"gin:bind-400": true, "gin:H": true, "gin:Request": true, "gin:Writer": true,
		"http:URL.Query().Get": true, "http:URL.Query().Has": true, "http:Header.Get": true, "http:Header.Values": true,
		"http:Error": true, "http:NotFound": true, "http:Redirect": true, "http:Header().Set": true}
	for f := range owner {
		group, m, _ := strings.Cut(f, ":")
		switch group {
		case "gin":
			if _, ok := ginMethods[m]; !ok && !elsewhere[f] {
				t.Errorf("form %s is read by no rule", f)
			}
		case "http":
			_, req := httpRequestMethods[m]
			_, w := httpWriterMethods[m]
			if !req && !w && !elsewhere[f] {
				t.Errorf("form %s is read by no rule", f)
			}
		}
	}
}

// TestGoShapeFixtureRows links the go-shape fixture and the generated
// server of the go-routes fixture and checks that every row is reached
// and every stored shape is valid.
func TestGoShapeFixtureRows(t *testing.T) {
	tags := map[string]int{}
	nodes, _ := goShapeLink(t, 6, tags)
	raw := map[string][]byte{}
	for p, s := range goFixtureSources(t) {
		raw[p] = []byte(s)
	}
	routesNodes, _ := goShapeLinkSources(t, raw, 6, tags)
	for _, r := range goShapeRows {
		if r.ID != "D" && tags[r.ID] == 0 {
			t.Errorf("the fixtures do not reach row %s", r.ID)
		}
	}
	for tag := range tags {
		found := false
		for _, r := range goShapeRows {
			found = found || r.ID == tag
		}
		if !found {
			t.Errorf("tag %s is not a row", tag)
		}
	}
	shapes := 0
	for _, n := range append(nodes, routesNodes...) {
		if v, ok := n.Attributes[routeshape.AttributeKey]; ok {
			shapes++
			if _, err := routeshape.FromAttribute(v); err != nil {
				t.Errorf("%s: %v", n.Identity, err)
			}
		}
	}
	if shapes < 40 {
		t.Errorf("%d shapes, want at least 40", shapes)
	}
}

// goShapeOf returns the shape of the fixture route with identity suffix.
func goShapeOf(t *testing.T, nodes []contract.Node, suffix string) *routeshape.Shape {
	t.Helper()
	for _, n := range nodes {
		if strings.HasSuffix(n.Identity, suffix) {
			s, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
			if err != nil {
				t.Fatalf("%s: %v", n.Identity, err)
			}
			return s
		}
	}
	t.Fatalf("no route %s", suffix)
	return nil
}

// TestGoShapeDepthLimit checks the configured depth (SM-21, SM-23): the
// seven-call chain of GET /deep stops at the limit with depth_cap and is
// followed to its end at 10; reads beyond the cap are not claimed.
func TestGoShapeDepthLimit(t *testing.T) {
	for _, c := range []struct {
		depth   int
		stop    routeshape.Stop
		reached int
		q3      bool
	}{{2, routeshape.StopDepthCap, 2, false}, {6, routeshape.StopDepthCap, 6, true}, {10, routeshape.StopEnded, 7, true}} {
		nodes, _ := goShapeLink(t, c.depth, nil)
		s := goShapeOf(t, nodes, "#GET%20%2Fdeep")
		if s.Follow.DepthLimit != c.depth || len(s.Follow.Chains) != 1 {
			t.Fatalf("depth %d: %+v", c.depth, s.Follow)
		}
		ch := s.Follow.Chains[0]
		if ch.Stop != c.stop || ch.Depth != c.reached {
			t.Errorf("depth %d: chain %s at %d, want %s at %d", c.depth, ch.Stop, ch.Depth, c.stop, c.reached)
		}
		if got := len(s.Query.Items) == 1; got != c.q3 {
			t.Errorf("depth %d: q3 read %v, want %v", c.depth, got, c.q3)
		}
		if got := len(s.Responses.Items) == 1; got != (c.depth == 10) {
			t.Errorf("depth %d: level7 response read %v", c.depth, got)
		}
		if c.stop == routeshape.StopDepthCap && !strings.Contains(s.Query.Reason, "depth cap reached") {
			t.Errorf("depth %d: reason %q", c.depth, s.Query.Reason)
		}
	}
}

// goShapeExport is one route of shapes.json, the input of the opt-in
// oracle scripts/oracle/go_shape.
type goShapeExport struct {
	Identity  string            `json:"identity"`
	Framework string            `json:"framework"`
	Method    string            `json:"method"`
	Templates []string          `json:"templates"`
	Shape     *routeshape.Shape `json:"shape"`
}

// goTypeExport is the schema Graphvein claims for one fixture struct
// type, as a request body (encoding/json decode) and as a response body.
type goTypeExport struct {
	Package  string            `json:"package"`
	Name     string            `json:"name"`
	Request  routeshape.Schema `json:"request"`
	Response routeshape.Schema `json:"response"`
}

// TestGoShapeExport writes the fixture's route shapes and type schemas as
// JSON into GRAPHVEIN_GO_SHAPE_OUT (shapes.json, types.json) for the
// opt-in oracle (scripts/oracle/go_shape), which compiles the fixture
// with the real gin, chi and encoding/json and compares.
func TestGoShapeExport(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_SHAPE_OUT")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_SHAPE_OUT is not set")
	}
	raw := goShapeSources(t)
	files := goParse(t, raw)
	golang.Link(files, golang.ModulePaths(raw))
	l := runGoDepth(files, raw, testscope.Rules{}, nil, routeshape.MaxDepthLimit)
	var routes []goShapeExport
	for _, n := range l.nodes {
		e := goShapeExport{Identity: n.Identity, Framework: n.Attributes["framework"].(string), Method: n.Attributes["method"].(string)}
		data, _ := json.Marshal(n.Attributes["paths"])
		var paths []struct {
			Template string `json:"template"`
		}
		_ = json.Unmarshal(data, &paths)
		for _, p := range paths {
			e.Templates = append(e.Templates, p.Template)
		}
		if v, ok := n.Attributes[routeshape.AttributeKey]; ok {
			s, err := routeshape.FromAttribute(v)
			if err != nil {
				t.Fatal(err)
			}
			e.Shape = s
		}
		routes = append(routes, e)
	}
	var types []goTypeExport
	for _, file := range []string{"ginapp/types.go", "chiapp/chiapp.go", "review/review.go"} {
		for _, st := range files[file].Go.Structs {
			if len(st.TypeParams) > 0 {
				continue
			}
			ref := facts.GoExpr{Kind: facts.GoIdentifier, Name: st.Name}
			req := &goSchemaBuilder{l: l, mode: goJSONRequest, stack: map[string]bool{}}
			resp := &goSchemaBuilder{l: l, mode: goJSONResponse, stack: map[string]bool{}}
			types = append(types, goTypeExport{Package: pathDir(file), Name: st.Name, Request: req.schema(file, ref, 0), Response: resp.schema(file, ref, 0)})
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{"shapes.json": routes, "types.json": types} {
		data, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
