package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
)

// TestLinkPathAssumptions indexes the link-path-assumptions fixture
// (T033 review): a client call linked to a route path entry that
// rests on a path assumption (RO-V1, RO-V2, GR-V5) carries those ids in
// the path entry's and the call's assumes, after any tier assumption
// (UP-V1), with its confidence unchanged, and clients.LinkAssumptions
// shows each with its text; a call linked through a plain entry carries
// none.
func TestLinkPathAssumptions(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/link-path-assumptions")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		target     string
		confidence float64
		assumes    []string
	}
	expected := map[string]want{
		// A1: /z/b behind a router with a 404 middleware (RO-V2).
		"client/calls.ts:2": {"route:web/app.js#GET%20%2Fz%2Fb", 0.9, []string{"RO-V2"}},
		// A2: a router mounted twice; the route side marks RO-V2 per route
		// (every entry of the route), so both links carry it.
		"client/calls.ts:3": {"route:web/app.js#GET%20%2Fc", 0.9, []string{"RO-V2"}},
		"client/calls.ts:4": {"route:web/app.js#GET%20%2Fc", 0.9, []string{"RO-V2"}},
		// A3: a plain route.
		"client/calls.ts:5": {"route:web/app.js#GET%20%2Fplain%2Fone", 0.9, nil},
		// F1: a mount at an unknown prefix added before the route (RO-V1).
		"client/calls.ts:6": {"route:api/main.py#GET%20%2Fhealth%2Flive", 0.9, []string{"RO-V1"}},
		// G1: a net/http mux served through a path-changing wrapper.
		"client/calls.ts:7": {"route:server/main.go#GET%20%2Fgw%2Fitems%2Fall", 0.9, []string{"GR-V5"}},
		// G2: the same below a prefix read from the environment: the
		// unknown-prefix tier's assumption first, then GR-V5.
		"client/calls.ts:8": {"route:server/main.go#GET%20%2Fgp%2Forders%2Flist", 0.6, []string{clients.AssumeUnknownPrefix, "GR-V5"}},
		// G3: one route, its wrapped entry (GR-V5) and its plain /direct
		// entry: each link carries what its own entry rests on.
		"client/calls.ts:9":  {"route:server/main.go#GET%20%2Fgb%2Fitems%2Fall", 0.9, []string{"GR-V5"}},
		"client/calls.ts:10": {"route:server/main.go#GET%20%2Fgb%2Fitems%2Fall", 0.9, nil},
	}
	edges := map[string][]contract.Edge{}
	var projected []any
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			projected = append(projected, fmt.Sprintf("%s paths=%s entries=%s", n.Identity, compact(t, n.Attributes["paths"]), compact(t, n.Attributes["unresolved_prefix_paths"])))
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			edges[e.Source] = append(edges[e.Source], e)
			projected = append(projected, e)
		}
	}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		projected = append(projected, n)
		key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
		w, ok := expected[key]
		if !ok {
			t.Errorf("%s: call without a case (%s)", key, n.Identity)
			continue
		}
		seen[key] = true
		if len(edges[n.Identity]) != 1 || edges[n.Identity][0].Target != w.target || edges[n.Identity][0].Confidence != w.confidence {
			t.Errorf("%s: edges %+v, want %s at %v", key, edges[n.Identity], w.target, w.confidence)
			continue
		}
		var paths []struct {
			Assumes []string `json:"assumes"`
		}
		data, _ := json.Marshal(n.Attributes["paths"])
		if json.Unmarshal(data, &paths) != nil || len(paths) != 1 {
			t.Fatalf("%s: paths %s", key, data)
		}
		var assumes []string
		data, _ = json.Marshal(n.Attributes["assumes"])
		_ = json.Unmarshal(data, &assumes)
		if !reflect.DeepEqual(assumes, w.assumes) || !reflect.DeepEqual(paths[0].Assumes, w.assumes) {
			t.Errorf("%s: assumes %v (path %v), want %v", key, assumes, paths[0].Assumes, w.assumes)
		}
		var shown []string
		for _, a := range clients.LinkAssumptions(n.Attributes, w.target) {
			if a.Text == "" {
				t.Errorf("%s: assumption %s has no text", key, a.ID)
			}
			shown = append(shown, a.ID)
		}
		if !reflect.DeepEqual(shown, w.assumes) {
			t.Errorf("%s: shown link assumptions %v, want %v", key, shown, w.assumes)
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	golden.Check(t, "../../testdata/fixtures/link-path-assumptions.golden.json", projected)
}
