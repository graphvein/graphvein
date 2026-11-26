package routes_test

import (
	"encoding/json"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	expressAuthFixture = "../../../testdata/fixtures/auth-express"
	axumAuthFixture    = "../../../testdata/fixtures/auth-axum"
)

// middlewareRoute is the golden's view of one Express or Axum route: its
// paths and the auth composition attributes.
type middlewareRoute struct {
	Identity   string   `json:"identity"`
	Paths      []string `json:"paths"`
	Middleware any      `json:"middleware,omitempty"`
	Declares   any      `json:"middleware_declares,omitempty"`
	Mounted    any      `json:"mounted_middleware,omitempty"`
	Unknown    any      `json:"mounted_middleware_unknown,omitempty"`
	Errors     any      `json:"mounted_error_handlers,omitempty"`
	Layers     any      `json:"layers,omitempty"`
	LayersUnk  any      `json:"layers_unknown,omitempty"`
}

func middlewareRoutes(s store.Snapshot) []middlewareRoute {
	var out []middlewareRoute
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		r := middlewareRoute{Identity: n.Identity, Paths: []string{}, Middleware: n.Attributes["middleware"], Declares: n.Attributes["middleware_declares"],
			Mounted: n.Attributes["mounted_middleware"], Unknown: n.Attributes["mounted_middleware_unknown"], Errors: n.Attributes["mounted_error_handlers"],
			Layers: n.Attributes["layers"], LayersUnk: n.Attributes["layers_unknown"]}
		paths, _ := n.Attributes["paths"].([]any)
		for _, p := range paths {
			m, _ := p.(map[string]any)
			template, _ := m["template"].(string)
			r.Paths = append(r.Paths, template)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// checkContract checks the golden, that the fixture reaches every row of
// rows and every listed form, and that the code rows equal the task's.
func checkContract(t *testing.T, fixture, prefix string, rows []routes.AuthRow, forms []string) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, tags := indexAuthFixture(t, root)
	golden.Check(t, fixture+".golden.json", middlewareRoutes(snapshot))
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range rows {
		if !reached[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for _, form := range forms {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
	checkTaskRows(t, prefix, contractIDs(rows))
}

// TestAuthExpressContract indexes the auth-express fixture, checks the
// composed middleware against the golden, replays warm, and checks that
// the fixture reaches every DA-E row, DAD and every refusal form.
func TestAuthExpressContract(t *testing.T) {
	checkContract(t, expressAuthFixture, "DA-E", routes.ExpressAuthContract, []string{
		"DA-E1/use", "DA-E1/parent", "DA-E1/prefix", "DA-E1/escape", "DA-E1/sibling", "DA-E1/prefix-dynamic",
		"DA-E1/first-argument", "DA-E1/order", "DA-E1/conditional", "DA-E1/spread", "DA-E1/chains", "DA-E1/unmounted",
		"DA-E1/prefix-match", "DA-E2/passport", "DA-E2/express-jwt", "DA-E2/express-oauth2-jwt-bearer",
		"DA-E2/express-openid-connect", "DA-E2/strategy-unknown", "DA-E2/option-unknown", "DAD/not-a-factory",
		"DA-E3/call", "DA-E3/use", "DA-E1/earlier-route", "DA-E4/all", "DA-E5/error-handler"})
}

// TestExpressMiddlewareOracle compares the stored middleware of the
// auth-express fixture with the middleware Express itself ran before each
// route (scripts/oracle/express_middleware.sh). Opt-in, never run by CI:
// GRAPHVEIN_EXPRESS_ORACLE lists the oracle's JSON outputs, comma
// separated (one per Express version).
//
// For each answered request, the route that answered (its
// handler_expression) states mounted_middleware then its own middleware.
// With no unknown entry the trace must equal that list; with unknown
// entries the list must be a subsequence of the trace (an unread form
// may add middleware, never remove a stated one). Every route of the
// oracle's files must answer at least one request.
func TestExpressMiddlewareOracle(t *testing.T) {
	outputs := os.Getenv("GRAPHVEIN_EXPRESS_ORACLE")
	if outputs == "" {
		t.Skip("set GRAPHVEIN_EXPRESS_ORACLE (scripts/oracle/express_middleware.sh)")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(expressAuthFixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := indexAuthFixture(t, root)
	byHandler := map[string]map[string]any{}
	handlerFile := map[string]string{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		if h, ok := n.Attributes["handler_expression"].(string); ok && strings.HasPrefix(h, "reply(") {
			if path.Ext(n.File) == ".js" && n.File != "libs.js" {
				byHandler[h] = n.Attributes
				handlerFile[h] = n.File
			}
		}
	}
	for _, file := range strings.Split(outputs, ",") {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var oracle struct {
			Express string   `json:"express"`
			Skipped []string `json:"skipped"`
			Results []struct {
				Root, Method, Path, Route string
				Status                    int
				Trace                     []string
			} `json:"results"`
		}
		if err := json.Unmarshal(data, &oracle); err != nil {
			t.Fatal(err)
		}
		answered := map[string]bool{}
		exact, bounded, disagree := 0, 0, 0
		for _, res := range oracle.Results {
			attrs, ok := byHandler[res.Route]
			if res.Status != 200 || !ok {
				t.Errorf("express %s: %s %s %s answered %d by %q, no stored route", oracle.Express, res.Root, res.Method, res.Path, res.Status, res.Route)
				disagree++
				continue
			}
			answered[res.Route] = true
			var stated []string
			mounted, _ := attrs["mounted_middleware"].([]any)
			for _, m := range mounted {
				entry, _ := m.(map[string]any)
				name, _ := entry["name"].(string)
				stated = append(stated, name)
			}
			own, _ := attrs["middleware"].([]any)
			for _, m := range own {
				name, _ := m.(string)
				stated = append(stated, name)
			}
			unknown := attrs["mounted_middleware_unknown"] != nil
			switch {
			case !unknown && reflect.DeepEqual(stated, append([]string{}, res.Trace...)):
				exact++
			case !unknown && len(stated) == 0 && len(res.Trace) == 0:
				exact++
			case unknown && subsequence(stated, res.Trace):
				bounded++
			default:
				disagree++
				t.Errorf("express %s: %s %s %s: stated %v (unknown entries: %t), ran %v", oracle.Express, res.Root, res.Method, res.Path, stated, unknown, res.Trace)
			}
		}
		skipped := map[string]bool{}
		for _, m := range oracle.Skipped {
			skipped[m] = true
		}
		for h := range byHandler {
			if !answered[h] && !skipped[handlerFile[h]] {
				t.Errorf("express %s: route %s answered no request", oracle.Express, h)
			}
		}
		t.Logf("express %s: %d requests agree exactly, %d within unknown entries, %d disagree", oracle.Express, exact, bounded, disagree)
	}
}

// subsequence reports whether every item of want occurs in got in order.
func subsequence(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && want[i] == g {
			i++
		}
	}
	return i == len(want)
}

// TestAuthAxumContract indexes the auth-axum fixture, checks the composed
// layers against the golden, replays warm, and checks that the fixture
// reaches every DA-X row, DAD and every form. Mapped: no oracle (the
// layer semantics are read in axum 0.7.9 and 0.8.8).
func TestAuthAxumContract(t *testing.T) {
	checkContract(t, axumAuthFixture, "DA-X", routes.AxumAuthContract, []string{
		"DA-X1/layer", "DA-X1/route_layer", "DA-X1/parent", "DA-X1/conditional", "DA-X1/chains",
		"DA-X2/tower-http", "DA-X2/from_fn", "DA-X2/axum-login", "DAD/layer-as-written",
		"DA-X3/layer", "DA-X4/stack", "DAD/stack-method"})
}
