package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
)

// linkPrecisionTags collects the rows the link precision branches report
// (clients.SetLinkPrecisionTagHook) while fn runs, and fails the test for
// a tag that is no row of clients.LinkPrecisionRows.
func linkPrecisionTags(t *testing.T, fn func()) map[string]int {
	t.Helper()
	tags := map[string]int{}
	clients.SetLinkPrecisionTagHook(func(row string) { tags[row]++ })
	defer clients.SetLinkPrecisionTagHook(nil)
	fn()
	for row := range tags {
		if !slices.Contains(clients.LinkPrecisionRows, row) {
			t.Errorf("a branch reports %s, which is not a link precision row", row)
		}
	}
	return tags
}

// TestLinkPrecisionContract keeps the code table (clients.LinkPrecisionRows)
// and the table in docs/extract/clients.md ("Link precision rules") 1:1:
// the same rows in the same order, the default LF-U last.
func TestLinkPrecisionContract(t *testing.T) {
	data, err := os.ReadFile("../../docs/extract/clients.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "## Link precision rules")
	if start < 0 {
		t.Fatal("no Link precision rules section")
	}
	section := text[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var rows []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| LF-") {
			rows = append(rows, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	if !slices.Equal(rows, clients.LinkPrecisionRows) {
		t.Errorf("docs rows %v, code rows %v", rows, clients.LinkPrecisionRows)
	}
	if n := strings.Count(text, "| LF-"); n != len(clients.LinkPrecisionRows) {
		t.Errorf("docs/extract/clients.md has %d LF rows, the code table %d", n, len(clients.LinkPrecisionRows))
	}
	if last := clients.LinkPrecisionRows[len(clients.LinkPrecisionRows)-1]; last != "LF-U" {
		t.Errorf("the default branch must come last, got %s", last)
	}
}

// TestLinkPrecisionRules checks the link-precision-fl fixture (T034): each
// client call against its row (independent of the golden), the golden of
// every client call, calls_route edge and calls edge of shadow.tsx and
// invisible.js, and
// that the table rows in the docs are exactly the rows the fixture covers.
func TestLinkPrecisionRules(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/link-precision-fl")); err != nil {
		t.Fatal(err)
	}
	// Inert name (README): the catch-all routes stay out of the self-index,
	// where they would become candidates of other fixtures' calls.
	if err := os.Rename(filepath.Join(root, "fallback.ts.inert"), filepath.Join(root, "fallback.ts")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	tags := linkPrecisionTags(t, func() { emit(t, root, s, opt) })
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		row, status, target string
		confidence          float64
		candidates          int
	}
	const catchAll = "route:fallback.ts#GET%20%2Apath"
	// target "" means no edge; candidates counts an ambiguous call's ties.
	const items, genre = "route:backend.ts#GET%20%2Ft034-items%2F%3Aid", "route:backend.ts#GET%20%2Ft034-genres%2Fmovie"
	expected := map[string]want{
		// LF-1: an origin nothing proves to be this server never reaches a
		// literal-free route (a module const, a template, an ANY catch-all,
		// an assignment-derived origin).
		"calls.ts:4": {row: "LF-1", status: "unmatched"},
		"calls.ts:5": {row: "LF-1", status: "unmatched"},
		"calls.ts:6": {row: "LF-1", status: "unmatched"},
		"calls.ts:9": {row: "LF-1", status: "unmatched"},
		// Unchanged: a literal origin still links a route with a literal
		// segment at 0.7; a call without an origin still links a catch-all.
		"calls.ts:10": {row: "LF-U", status: "linked", target: items, confidence: 0.7},
		"calls.ts:11": {row: "LF-U", status: "linked", target: catchAll, confidence: 0.9},
		// LF-2: a call parameter covers literal segments of other routes,
		// so the catch-all is one candidate among them.
		"calls.ts:14": {row: "LF-2", status: "ambiguous", candidates: 3},
		"calls.ts:15": {row: "LF-2", status: "ambiguous", candidates: 5},
		// Unchanged: a literal call, a parameter route winning over a
		// literal sibling (not a catch-all), a catch-all nothing overlaps.
		"calls.ts:16": {row: "LF-U", status: "linked", target: genre, confidence: 0.9},
		"calls.ts:17": {row: "LF-U", status: "linked", target: items, confidence: 0.9},
		"calls.ts:18": {row: "LF-U", status: "linked", target: catchAll, confidence: 0.9},
		// Unchanged: the wrapper and its invocation where it is visible.
		"shadow.tsx:4":   {row: "LF-U", status: "wrapper"},
		"shadow.tsx:6":   {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
		"invisible.js:3": {row: "LF-U", status: "wrapper"},
		"invisible.js:4": {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
		// A parameter's default value and a computed key bind nothing
		// (T034 review): the module wrapper is invoked.
		"defaults.js:1": {row: "LF-U", status: "wrapper"},
		"defaults.js:2": {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
		"defaults.js:3": {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
		"defaults.js:4": {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
		"defaults.ts:1": {row: "LF-U", status: "wrapper"},
		"defaults.ts:2": {row: "LF-U", status: "linked", target: "route:backend.ts#DELETE%20%2Ft034-items%2F%3Aid", confidence: 0.9},
	}
	edges := map[string][]contract.Edge{}
	var projected []any
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" || e.Kind == "calls" && len(e.Evidence) > 0 && slices.Contains([]string{"shadow.tsx", "invisible.js", "defaults.js", "defaults.ts"}, e.Evidence[0].File) {
			projected = append(projected, e)
		}
		if e.Kind == "calls_route" {
			edges[e.Source] = append(edges[e.Source], e)
		}
	}
	seen := map[string]bool{}
	var keys []string
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		projected = append(projected, n)
		key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
		keys = append(keys, key)
		w, ok := expected[key]
		if !ok {
			t.Errorf("%s: call without a row (%s) %v %v", key, n.Identity, n.Attributes["status"], edges[n.Identity])
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
		if w.status == "ambiguous" {
			var paths []struct {
				CandidateCount int `json:"candidate_count"`
			}
			data, _ := json.Marshal(n.Attributes["paths"])
			count := -1
			if json.Unmarshal(data, &paths) == nil && len(paths) == 1 {
				count = paths[0].CandidateCount
			}
			if count != w.candidates {
				t.Errorf("%s (%s): candidate_count %v, want %d", key, w.row, count, w.candidates)
			}
		}
	}
	// LF-3: a destructured binding (array pattern, shorthand parameter,
	// object pattern) shadows the wrapper's name: those calls are no
	// invocation and the functions do not call the wrapper.
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && e.Target == "function:shadow.tsx#remove" && e.Source != "function:shadow.tsx#Items" {
			t.Errorf("LF-3: %s calls the wrapper through a shadowed name", e.Source)
		}
	}
	// The wrapper purge is declared inside an anonymous function: Footer
	// cannot see it, so the call is no invocation (and, since T046, no
	// calls edge names it either: row TSX-17).
	for _, owner := range []string{"Header", "Panel", "Toolbar", "Footer"} {
		for _, n := range snapshot.Nodes {
			if n.Kind == "client_call" && strings.HasPrefix(n.QualifiedName, owner+".") {
				t.Errorf("LF-3: %s is an invocation through a shadowed name", n.Identity)
			}
		}
	}
	// A pattern shadows in its own function only: a class and a function
	// of one name in different anonymous scopes do not share bindings.
	colorRead := false
	for _, e := range snapshot.Edges {
		colorRead = colorRead || e.Kind == "calls" && e.Source == "method:shadow.tsx#Color.parse" && e.Target == "function:shadow.tsx#Color.parse.read"
	}
	// An object literal's method is a property, not a nearer binding: the
	// destructured drop of Row still shadows it.
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && e.Source == "function:shadow.tsx#Row" && e.Target == "method:shadow.tsx#Row.drop" {
			t.Error("LF-3: Row calls its object method drop through the destructured name")
		}
	}
	if !colorRead {
		t.Error("LF-3: Color.parse no longer calls read")
	}
	// C4: a parameter binds its pattern, never its default value or a
	// computed key; a parameter named like the wrapper shadows it.
	callsWrapper := map[string]bool{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" && (e.Target == "function:defaults.js#del" || e.Target == "function:defaults.ts#erase") {
			callsWrapper[e.Source] = true
		}
	}
	for source, want := range map[string]bool{"function:defaults.js#T5": true, "function:defaults.js#T6": true, "function:defaults.js#T7": true,
		"function:defaults.js#T8": false, "function:defaults.ts#T9": true, "function:defaults.ts#T10": false} {
		if callsWrapper[source] != want {
			t.Errorf("C4: %s calls the wrapper %v, want %v", source, callsWrapper[source], want)
		}
	}
	sort.Strings(keys)
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted (have %v)", key, keys)
		}
	}
	// C1: an OpenAPI spec whose servers name a host nothing proves to be
	// this repository's server documents no literal-free route (GET *path);
	// a route with a literal segment is documented at 0.7.
	documented := map[string]string{}
	for _, e := range snapshot.Edges {
		if e.Kind == "documents_route" {
			documented[e.Source] = e.Target
			projected = append(projected, e)
		}
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "api_operation" {
			projected = append(projected, n)
		}
	}
	for op, want := range map[string]string{
		"api_operation:spec/openapi.yaml#GET%20%2Frepos%2F%7Bowner%7D%2F%7Brepo%7D": "",
		"api_operation:spec/openapi.yaml#GET%20%2Fuser":                             "",
		"api_operation:spec/openapi.yaml#GET%20%2Ft034-api%2Ft034-genres%2Fmovie":   genre,
	} {
		if documented[op] != want {
			t.Errorf("C1: %s documents %q, want %q (all: %v)", op, documented[op], want, documented)
		}
	}
	// Every row's branch runs on the fixture (the tags, not the
	// expectations above, show it).
	for _, row := range clients.LinkPrecisionRows {
		if tags[row] == 0 {
			t.Errorf("the fixture never reaches row %s", row)
		}
	}
	golden.Check(t, "../../testdata/fixtures/link-precision-fl.golden.json", projected)
}

// TestLinkPrecisionCatchAlls checks row LF-2 on the final catch-all of
// every other framework (T034 review): Next.js `[...rest]` and
// `[[...slug]]`, chi `*`, Axum `{*rest}` and FastAPI's `{name:path}`,
// which the routes extractor stores as `{*name}` like the others; and the
// shown origin assumption of a 0.7 link whose origin nothing proves.
func TestLinkPrecisionCatchAlls(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/link-precision-catchall")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	tags := linkPrecisionTags(t, func() { emit(t, root, s, opt) })
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const pyCatchAll = "route:pyapp/main.py#GET%20%2Ft035-py%2Fapi%2F%7B_path%3Apath%7D"
	type want struct {
		status, target string
		confidence     float64
		candidates     int
		assumption     string
	}
	expected := map[string]want{
		"web/lib/calls.ts:2": {status: "ambiguous", candidates: 2}, // Next.js [...rest]
		"web/lib/calls.ts:3": {status: "ambiguous", candidates: 2}, // Next.js [[...slug]]
		"web/lib/calls.ts:4": {status: "ambiguous", candidates: 2}, // chi *
		"web/lib/calls.ts:5": {status: "ambiguous", candidates: 2}, // Axum {*rest}
		"web/lib/calls.ts:6": {status: "ambiguous", candidates: 3}, // FastAPI {_path:path}
		// B1: the path convertor takes several segments, so the
		// catch-all serves a multi-segment call.
		"web/lib/calls.ts:7": {status: "linked", target: pyCatchAll, confidence: 0.9},
		// A literal origin nothing proves: linked at 0.7 to a route with
		// a literal segment, with the shown assumption.
		"web/lib/calls.ts:8": {status: "linked", target: pyCatchAll, confidence: 0.7, assumption: "origin https://cdn.example.org assumed to be this repository's server"},
		"web/lib/calls.ts:9": {status: "linked", target: "route:web/app/t035-next/items/featured/route.ts#GET%20%2Ft035-next%2Fitems%2Ffeatured", confidence: 0.9},
	}
	edges := map[string][]contract.Edge{}
	var projected []any
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			edges[e.Source] = append(edges[e.Source], e)
			projected = append(projected, e)
		}
	}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" && n.File == "pyapp/main.py" && n.Identity == pyCatchAll {
			data, _ := json.Marshal(n.Attributes["paths"])
			if !strings.Contains(string(data), `"template":"/t035-py/api/{*_path}"`) {
				t.Errorf("B1: FastAPI path convertor stored as %s, want /t035-py/api/{*_path}", data)
			}
		}
		if n.Kind == "route" {
			projected = append(projected, n.Identity, n.Attributes["paths"])
		}
		if n.Kind != "client_call" {
			continue
		}
		projected = append(projected, n)
		key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
		w, ok := expected[key]
		if !ok {
			t.Errorf("%s: unexpected call %s", key, n.Identity)
			continue
		}
		seen[key] = true
		if n.Attributes["status"] != w.status {
			t.Errorf("%s: status %v, want %s", key, n.Attributes["status"], w.status)
		}
		got, confidence := "", 0.0
		if len(edges[n.Identity]) == 1 {
			got, confidence = edges[n.Identity][0].Target, edges[n.Identity][0].Confidence
		}
		if len(edges[n.Identity]) > 1 || got != w.target || confidence != w.confidence {
			t.Errorf("%s: edges %+v, want %s at %v", key, edges[n.Identity], w.target, w.confidence)
		}
		data, _ := json.Marshal(n.Attributes)
		var attrs struct {
			Paths []struct {
				CandidateCount int `json:"candidate_count"`
			} `json:"paths"`
			Assumptions []string `json:"origin_assumptions"`
		}
		if err := json.Unmarshal(data, &attrs); err != nil || len(attrs.Paths) != 1 {
			t.Errorf("%s: paths %s", key, data)
			continue
		}
		if w.status == "ambiguous" && attrs.Paths[0].CandidateCount != w.candidates {
			t.Errorf("%s: candidate_count %d, want %d", key, attrs.Paths[0].CandidateCount, w.candidates)
		}
		if got := strings.Join(attrs.Assumptions, "; "); got != w.assumption {
			t.Errorf("%s: origin_assumptions %q, want %q", key, got, w.assumption)
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	if tags["LF-2"] < 5 {
		t.Errorf("LF-2 reached %d times, want one per catch-all form (5)", tags["LF-2"])
	}
	golden.Check(t, "../../testdata/fixtures/link-precision-catchall.golden.json", projected)
}
