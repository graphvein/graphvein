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

// TestUnknownPrefixLinks indexes the unknown-prefix fixture (T033,
// rows UP-13..UP-20 of docs/contract/claims/unknown-prefix-paths.md,
// copied in docs/extract/clients.md): every client call is checked against
// its row independently of the golden (status, link target, confidence,
// candidate count, assumptions); the golden holds the routes' entries, the
// client calls and the calls_route edges; the branches the fixture reaches
// cover every matcher row (clients.UnknownPrefixMatcherRows) but UP-21, a
// row of the project link pass, and nothing else.
func TestUnknownPrefixLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/unknown-prefix")); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	clients.SetUnknownPrefixTagHook(func(row string) { tags[row]++ })
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	clients.SetUnknownPrefixTagHook(nil)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const (
		orders   = "route:server/api.go#GET%20%2Fupb%2Forders%2F%7Bid%7D"
		post     = "route:server/api.go#POST%20%2Fupb%2Forders"
		settings = "route:server/api.go#GET%20%2F%7Borg%7D%2Fupb%2Fsettings"
		files    = "route:server/api.go#GET%20%2Fupb%2Ffiles%2F"
		known    = "route:server/api.go#GET%20%2Fupb%2Fknown%2Fitems%402"
		prefixed = "route:server/api.go#GET%20%2Fupb%2Fknown%2Fitems"
		reports  = "route:server/api.go#GET%20%2Fupb%2Freports%2F%7Bid%7D"
		inner    = "route:server/api.go#GET%20%2Fupb%2Finner%2F%7Bname%7D"
		catchAll = "route:server/api.go#GET%20%2Fapp%2F%2A"
	)
	type want struct {
		row, status, target string
		confidence          float64
		// candidates is an ambiguous or test-only alternative's candidate
		// list length.
		candidates int
		assumes    []string
	}
	up1 := []string{clients.AssumeUnknownPrefix}
	expected := map[string]want{
		// UP-14: known from its start, the call is V + template (V = /v1).
		"web/calls.ts:2": {row: "UP-14", status: "linked", target: orders, confidence: 0.6, assumes: up1},
		// UP-14 with an empty unknown run.
		"web/calls.ts:3": {row: "UP-14", status: "linked", target: post, confidence: 0.6, assumes: up1},
		// UP-16: one literal segment only.
		"web/calls.ts:4": {row: "UP-16", status: "unmatched"},
		// UP-17: two routes with the same tail below two bases.
		"web/calls.ts:5": {row: "UP-17", status: "ambiguous", candidates: 2},
		// UP-17: a second route matching with one literal segment (/daily)
		// makes the call ambiguous; no ranking by literal count.
		"web/calls.ts:6": {row: "UP-17", status: "ambiguous", candidates: 2},
		// UP-16: a call literal would fill the route's leading parameter.
		"web/calls.ts:7": {row: "UP-16", status: "unmatched"},
		// UP-16 anchored: a call parameter takes the leading parameter.
		"web/calls.ts:8": {row: "UP-14", status: "linked", target: settings, confidence: 0.6, assumes: up1},
		// UP-14 refusal: a call parameter inside the unknown run.
		"web/calls.ts:9": {row: "D", status: "unmatched"},
		// UP-18: chi's /upb/files/ is not served for /upb/files.
		"web/calls.ts:10": {row: "UP-18", status: "unmatched"},
		"web/calls.ts:11": {row: "UP-14", status: "linked", target: files, confidence: 0.6, assumes: up1},
		// UP-13: a call the exact tier links keeps its link; the
		// unknown-prefix route with the same tail is not considered.
		"web/calls.ts:12": {row: "UP-13", status: "linked", target: known, confidence: 0.9},
		"web/calls.ts:13": {row: "UP-14", status: "linked", target: prefixed, confidence: 0.6, assumes: up1},
		// UP-18: HEAD reaches a net/http GET route, not a chi one.
		"web/calls.ts:14": {row: "UP-18", status: "linked", target: reports, confidence: 0.6, assumes: up1},
		"web/calls.ts:15": {row: "UP-18", status: "unmatched"},
		// UP-18: only a test-scope route matches a production call.
		"web/calls.ts:16": {row: "UP-18", status: "test_only", candidates: 1},
		// UP-14 with above: /outer + V + template.
		"web/calls.ts:17": {row: "UP-14", status: "linked", target: inner, confidence: 0.6, assumes: up1},
		// UP-18: no route of the method.
		"web/calls.ts:18": {row: "UP-18", status: "unmatched"},
		// UP-15: unresolved base aligned with the unknown prefix.
		"web/calls.ts:21": {row: "UP-15", status: "linked", target: orders, confidence: 0.6, assumes: []string{clients.AssumeUnknownPrefix, clients.AssumeSamePrefix}},
		// UP-15 refusal: the call's template is not the route's template.
		"web/calls.ts:22": {row: "UP-15", status: "unmatched"},
		// UP-15 refusal: an entry with a known part above the unknown
		// prefix, which the call's unknown base cannot stand for.
		"web/calls.ts:23": {row: "UP-15", status: "unmatched"},
		// UP-20: a literal origin.
		"web/calls.ts:24": {row: "UP-20", status: "unmatched"},
		// UP-22: only a known final catch-all in the existing
		// tiers, and an unknown-prefix route matching the call.
		"web/calls.ts:27": {row: "UP-22", status: "ambiguous", candidates: 2},
		// UP-13: the catch-all alone; no unknown-prefix route matches.
		"web/calls.ts:28": {row: "UP-13", status: "linked", target: catchAll, confidence: 0.9},
		// UP-22: a rival with one literal segment (UP-17's competitor).
		"web/calls.ts:29": {row: "UP-22", status: "ambiguous", candidates: 2},
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
		if n.Attributes["status"] != w.status {
			t.Errorf("%s (%s): status %v, want %s", key, w.row, n.Attributes["status"], w.status)
		}
		got, confidence := "", 0.0
		if len(edges[n.Identity]) == 1 {
			got, confidence = edges[n.Identity][0].Target, edges[n.Identity][0].Confidence
		}
		if len(edges[n.Identity]) > 1 || got != w.target || confidence != w.confidence {
			t.Errorf("%s (%s): edges %+v, want %s at %v", key, w.row, edges[n.Identity], w.target, w.confidence)
		}
		var paths []struct {
			Candidates  []string `json:"candidates"`
			Match       *string  `json:"match"`
			RoutePrefix string   `json:"route_prefix"`
			Assumes     []string `json:"assumes"`
		}
		data, _ := json.Marshal(n.Attributes["paths"])
		if json.Unmarshal(data, &paths) != nil || len(paths) != 1 {
			t.Fatalf("%s: paths %s", key, data)
		}
		p := paths[0]
		if (w.status == "ambiguous" || w.status == "test_only") && len(p.Candidates) != w.candidates {
			t.Errorf("%s (%s): candidates %v, want %d", key, w.row, p.Candidates, w.candidates)
		}
		var assumes []string
		if raw, ok := n.Attributes["assumes"].([]any); ok {
			for _, v := range raw {
				assumes = append(assumes, v.(string))
			}
		}
		if !reflect.DeepEqual(assumes, w.assumes) || !reflect.DeepEqual(p.Assumes, w.assumes) {
			t.Errorf("%s (%s): assumes %v (path %v), want %v", key, w.row, assumes, p.Assumes, w.assumes)
		}
		prefixed := w.confidence == 0.6
		if prefixed != (p.RoutePrefix == "unresolved") || prefixed && (p.Match == nil || *p.Match != "suffix") {
			t.Errorf("%s (%s): match %v route_prefix %q", key, w.row, p.Match, p.RoutePrefix)
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	golden.Check(t, "../../testdata/fixtures/unknown-prefix.golden.json", projected)
	var got []string
	for tag := range tags {
		got = append(got, tag)
		if !slices.Contains(clients.UnknownPrefixMatcherRows, tag) {
			t.Errorf("a branch reports %s, which is not a matcher row", tag)
		}
	}
	sort.Strings(got)
	for _, row := range clients.UnknownPrefixMatcherRows {
		if row == "UP-21" {
			// A row of the project link pass only (a declared literal
			// origin): the projectlink fixture test
			// TestUnknownPrefixProjectScenarios reaches it.
			continue
		}
		if tags[row] == 0 {
			t.Errorf("the fixture does not reach row %s (tags %v)", row, got)
		}
	}
}

func compact(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
