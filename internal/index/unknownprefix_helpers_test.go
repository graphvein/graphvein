package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
)

// TestUnknownPrefixHelpers indexes the unknown-prefix-helpers fixture (T033,
// call-side rows UP-39 to UP-41 of
// docs/contract/claims/unknown-prefix-paths.md, copied in
// docs/extract/clients.md): every client call is checked against its row
// independently of the golden (status, base, base expression, link
// target, confidence, assumptions); the golden holds the routes' entries,
// the client calls and the calls_route edges; the branches the fixture
// reaches cover every call-side row (clients.UnknownPrefixCallRows) and
// nothing else.
func TestUnknownPrefixHelpers(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/unknown-prefix-helpers")); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	clients.SetUnknownPrefixCallTagHook(func(row string) { tags[row]++ })
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	clients.SetUnknownPrefixCallTagHook(nil)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const (
		version = "route:server/api.go#GET%20%2Fupc%2Fapi%2Fversion"
		profile = "route:server/api.go#PATCH%20%2Fupc%2Fapi%2Fprofile"
		logs    = "route:server/api.go#GET%20%2Fupc%2Fhosts%2F%7Bhost%7D%2Flogs"
		known   = "route:server/api.go#GET%20%2Fupc%2Fknown%2Fitems"
	)
	type want struct {
		row, status, base, expression, target string
		confidence                            float64
		assumes                               []string
	}
	aligned := []string{clients.AssumeUnknownPrefix, clients.AssumeSamePrefix}
	expected := map[string]want{
		// UP-39: the proven shapes, linked by UP-15 (aligned).
		"web/calls.ts:4": {row: "UP-39", status: "linked", base: "unresolved", expression: "config.base", target: version, confidence: 0.6, assumes: aligned},
		"web/calls.ts:5": {row: "UP-39", status: "linked", base: "unresolved", expression: "config.base", target: profile, confidence: 0.6, assumes: aligned},
		"web/calls.ts:6": {row: "UP-39", status: "linked", base: "unresolved", expression: "config.base", target: logs, confidence: 0.6, assumes: aligned},
		"web/calls.ts:7": {row: "UP-39", status: "linked", base: "unresolved", expression: "config.base", target: version, confidence: 0.6, assumes: aligned},
		"web/calls.ts:8": {row: "UP-39", status: "linked", base: "unresolved", expression: "BASE", target: version, confidence: 0.6, assumes: aligned},
		// UP-39 read, then the matcher refuses: one literal segment (UP-16),
		// two routes (UP-17), an entry with above (UP-15).
		"web/calls.ts:9":  {row: "UP-39", status: "unmatched", base: "unresolved", expression: "config.base"},
		"web/calls.ts:10": {row: "UP-39", status: "ambiguous", base: "unresolved", expression: "config.base"},
		"web/calls.ts:11": {row: "UP-39", status: "unmatched", base: "unresolved", expression: "config.base"},
		// UP-39 read, and the existing suffix tier links the route with a
		// full path (UP-13: the unknown-prefix tier is not reached).
		"web/calls.ts:12": {row: "UP-39", status: "linked", base: "unresolved", expression: "config.base", target: known, confidence: 0.6},
		// UP-40: arguments the row does not read.
		"web/calls.ts:13": {row: "UP-40", status: "dynamic", base: "unresolved"},
		"web/calls.ts:14": {row: "UP-40", status: "dynamic", base: "unresolved"},
		"web/calls.ts:15": {row: "UP-40", status: "dynamic", base: "unresolved"},
		// UP-40: bases the row does not read (let, call, written, a known
		// object, a known object without the property).
		"web/calls.ts:16": {row: "UP-40", status: "dynamic", base: "unresolved"},
		"web/calls.ts:17": {row: "UP-40", status: "dynamic", base: "unresolved"},
		"web/calls.ts:18": {row: "UP-40", status: "dynamic", base: "unresolved"},
		"web/calls.ts:19": {row: "UP-40", status: "linked", base: "none", target: known, confidence: 0.9},
		"web/calls.ts:20": {row: "UP-40", status: "dynamic", base: "unresolved"},
		// D: other helper shapes keep the ordinary evaluation.
		"web/calls.ts:21": {row: "D", status: "dynamic", base: "unresolved"},
		"web/calls.ts:22": {row: "D", status: "dynamic", base: "unresolved"},
		"web/calls.ts:23": {row: "D", status: "unmatched", base: "none"},
		// UP-40: a helper inside a function.
		"web/calls.ts:25": {row: "UP-40", status: "dynamic", base: "unresolved"},
		// UP-41: helpers bound by the generated auto-import declaration
		// file (web/auto-imports.d.ts), read by UP-39 or refused by UP-40.
		"web/auto.ts:2": {row: "UP-41", status: "linked", base: "unresolved", expression: "config.base", target: version, confidence: 0.6, assumes: aligned},
		"web/auto.ts:3": {row: "UP-41", status: "linked", base: "unresolved", expression: "config.base", target: logs, confidence: 0.6, assumes: aligned},
		"web/auto.ts:4": {row: "UP-41", status: "dynamic", base: "unresolved"},
		// D: a declaration file without the generated header binds nothing.
		"web/plain/calls.ts:2": {row: "D", status: "dynamic", base: "unresolved"},
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
			t.Errorf("%s: call without a row (%s)", key, n.Identity)
			continue
		}
		seen[key] = true
		if n.Attributes["status"] != w.status || n.Attributes["base"] != w.base {
			t.Errorf("%s (%s): status %v base %v, want %s %s", key, w.row, n.Attributes["status"], n.Attributes["base"], w.status, w.base)
		}
		expression, _ := n.Attributes["base_expression"].(string)
		if expression != w.expression {
			t.Errorf("%s (%s): base_expression %q, want %q", key, w.row, expression, w.expression)
		}
		got, confidence := "", 0.0
		if len(edges[n.Identity]) == 1 {
			got, confidence = edges[n.Identity][0].Target, edges[n.Identity][0].Confidence
		}
		if len(edges[n.Identity]) > 1 || got != w.target || confidence != w.confidence {
			t.Errorf("%s (%s): edges %+v, want %s at %v", key, w.row, edges[n.Identity], w.target, w.confidence)
		}
		var assumes []string
		if raw, ok := n.Attributes["assumes"].([]any); ok {
			for _, v := range raw {
				assumes = append(assumes, v.(string))
			}
		}
		if !reflect.DeepEqual(assumes, w.assumes) {
			t.Errorf("%s (%s): assumes %v, want %v", key, w.row, assumes, w.assumes)
		}
		if w.row == "UP-39" || w.row == "UP-41" && w.status == "linked" {
			var helpers []string
			data, _ := json.Marshal(n.Attributes["helpers"])
			if json.Unmarshal(data, &helpers) != nil || len(helpers) != 1 {
				t.Errorf("%s (%s): helpers %s, want the one helper", key, w.row, data)
			}
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	golden.Check(t, "../../testdata/fixtures/unknown-prefix-helpers.golden.json", projected)
	var got []string
	for tag := range tags {
		got = append(got, tag)
		if !slices.Contains(clients.UnknownPrefixCallRows, tag) {
			t.Errorf("a branch reports %s, which is not a call-side row", tag)
		}
	}
	sort.Strings(got)
	for _, row := range clients.UnknownPrefixCallRows {
		if tags[row] == 0 {
			t.Errorf("the fixture does not reach row %s (tags %v)", row, got)
		}
	}
}
