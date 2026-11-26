package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/scan"
)

// goCensusRow is the part of a T023 census row (docs/contract/claims/data/go-http-routers-census.jsonl)
// the registration check reads.
type goCensusRow struct {
	Corpus      string   `json:"corpus"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Kind        string   `json:"kind"`
	Call        string   `json:"call"`
	Methods     []string `json:"methods"`
	Row         string   `json:"row"`
	HandlerRow  string   `json:"handler_row"`
	HandlerForm string   `json:"handler_form"`
	DForm       string   `json:"d_form"`
	Scope       string   `json:"scope"`
	// Paths are the path entries the investigation's precise rules give the site
	// (the oracle-checked composition of the census).
	Paths []goPathEntry `json:"paths"`
}

// goCorpusLink scans and links one corpus checkout as an index run would.
func goCorpusLink(t *testing.T, root string) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	t.Helper()
	sources := goCorpusSources(t, root)
	goSources := map[string][]byte{}
	for p, s := range sources {
		if strings.HasSuffix(p, ".go") {
			goSources[p] = s
		}
	}
	files := goParse(t, goSources)
	golang.Link(files, golang.ModulePaths(sources))
	return LinkGo(files, sources)
}

// goCorpusSources scans one corpus checkout as an index run would.
func goCorpusSources(t *testing.T, root string) map[string][]byte {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
	}
	return sources
}

// goCensusChange is a row of docs/contract/claims/data/go-http-routers-census-gr5.jsonl: a census
// route site whose expectation step GR5's rules change. Kind compose: the
// registration's handler is a sub-mux behind a proven wrapper (GR-O7), so
// the site is a composition, no route node; kind route: the site's path
// entries (each checked by TestGoRoutesMuxOracle on the corpora).
//
// Rows of docs/contract/claims/data/go-http-routers-census-gr6.jsonl (row GR-G1) also set
// Row and HandlerRow: a census D site that is now a route of a generated
// server, with the handler row its node has (GR-H2 for a resolved
// implementing method, GR-H5 otherwise).
//
// Rows of docs/contract/claims/data/go-http-routers-census-gr7.jsonl (rows GR-O8, GR-O9 and
// GR-C7) give owncast's sites their paths through the ServeMux -> chi hop
// (each checked by both oracles) and mark the forwarding closure's
// registration as a composition.
type goCensusChange struct {
	Corpus     string        `json:"corpus"`
	File       string        `json:"file"`
	Line       int           `json:"line"`
	Kind       string        `json:"kind"`
	Paths      []goPathEntry `json:"paths"`
	Row        string        `json:"row"`
	HandlerRow string        `json:"handler_row"`
}

// goCensusChanges reads docs/contract/claims/data/go-http-routers-census-gr5.jsonl,
// go-http-routers-census-gr6.jsonl and go-http-routers-census-gr7.jsonl by corpus and file:line
// (a later file's row replaces an earlier one's).
func goCensusChanges(t *testing.T) map[string]map[string]goCensusChange {
	t.Helper()
	out := map[string]map[string]goCensusChange{}
	for _, name := range []string{"go-http-routers-census-gr5.jsonl", "go-http-routers-census-gr6.jsonl", "go-http-routers-census-gr7.jsonl"} {
		f, err := os.Open("../../../docs/contract/claims/data/" + name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var c goCensusChange
			if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
				t.Fatal(err)
			}
			if out[c.Corpus] == nil {
				out[c.Corpus] = map[string]goCensusChange{}
			}
			out[c.Corpus][fmt.Sprintf("%s:%d", c.File, c.Line)] = c
		}
		f.Close()
	}
	return out
}

// TestGoRoutesCorpusRegistrations checks steps GR2-GR5 against the investigation's
// census of the five Go corpora: every census route row of a claim row
// (GR-R1-GR-R4) is a route node at the row's line with the row's methods,
// handler row and path entries (the census's oracle-checked composition,
// with step GR5's changes of docs/contract/claims/data/go-http-routers-census-gr5.jsonl applied),
// every D row is no route node and its form is counted, and no other route
// node is emitted. Set
// GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS to the directory holding the
// checkouts (scripts/cloud-corpus.sh realworld-gin gotify dozzle miniflux
// owncast).
func TestGoRoutesCorpusRegistrations(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS is not set")
	}
	f, err := os.Open("../../../docs/contract/claims/data/go-http-routers-census.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string][]goCensusRow{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var r goCensusRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Kind == "route" {
			rows[r.Corpus] = append(rows[r.Corpus], r)
		}
	}
	changes := goCensusChanges(t)
	for _, corpus := range []string{"realworld-gin", "gotify", "dozzle", "miniflux", "owncast"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, corpus)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%s: %v", corpus, err)
			}
			nodes, _, diagnostics := goCorpusLink(t, root)
			byLine := map[string][]contract.Node{}
			for _, n := range nodes {
				key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
				byLine[key] = append(byLine[key], n)
			}
			seen := map[string]bool{}
			claimed, upgraded, withPath, refused := 0, 0, 0, map[string]int{}
			for _, r := range rows[corpus] {
				key := fmt.Sprintf("%s:%d", r.File, r.Line)
				seen[key] = true
				got := byLine[key]
				if c, ok := changes[corpus][key]; ok && c.Row != "" {
					r.Row, r.HandlerRow = c.Row, c.HandlerRow
				}
				if r.Row == "D" {
					refused[r.DForm]++
					if len(got) > 0 {
						t.Errorf("%s: %s: census D %s, got %d route nodes", key, r.Call, r.DForm, len(got))
					}
					continue
				}
				if c, ok := changes[corpus][key]; ok {
					if c.Kind == "compose" {
						if len(got) > 0 {
							t.Errorf("%s: %s: a composition since step GR5, got %d route nodes", key, r.Call, len(got))
						}
						continue
					}
					r.Paths = c.Paths
				}
				claimed++
				var methods []string
				for _, n := range got {
					methods = append(methods, n.Attributes["method"].(string))
				}
				sort.Strings(methods)
				want := append([]string(nil), r.Methods...)
				if len(want) > 1 && r.Row != "GR-R4" && r.Row != "GR-R3" {
					want = []string{"ANY"}
				}
				sort.Strings(want)
				if strings.Join(methods, ",") != strings.Join(want, ",") {
					t.Errorf("%s: %s: methods %v, census %v (%s)", key, r.Call, methods, want, r.Row)
					continue
				}
				siteWithPath := false
				for _, n := range got {
					if want, have := goPathLines(r.Paths), goPathLines(n.Attributes["paths"].([]goPathEntry)); want != have {
						t.Errorf("%s: %s %s: paths\n  %s\ncensus\n  %s", key, n.Attributes["method"], r.Call, have, want)
					}
					if len(n.Attributes["paths"].([]goPathEntry)) > 0 && n.Attributes["scope"] != "test" {
						siteWithPath = true
					}
					row := "GR-H5"
					switch {
					case n.Attributes["handler_inline"] == true:
						row = "GR-H4"
					case n.Attributes["handler"] != nil:
						row = "GR-H1|GR-H2|GR-H3"
					}
					if r.HandlerRow == "GR-H5" && r.HandlerForm == "method_value_untyped" && row != "GR-H5" {
						// The investigation's census did not type a receiver bound
						// to a call of a module function with one
						// declared result; GR-H2 does.
						upgraded++
						continue
					}
					if !strings.Contains(row, r.HandlerRow) {
						t.Errorf("%s: %s: handler %v / %v, census %s", key, r.Call, n.Attributes["handler"], n.Attributes["handler_expression"], r.HandlerRow)
					}
					if (n.Attributes["scope"] == "test") != (r.Scope == "test") {
						t.Errorf("%s: scope %v, census %q", key, n.Attributes["scope"], r.Scope)
					}
				}
				if siteWithPath {
					withPath++
				}
			}
			extra := 0
			for key, list := range byLine {
				if !seen[key] {
					extra += len(list)
					for _, n := range list {
						id, _ := url.PathUnescape(n.Identity)
						t.Errorf("route node not in the census: %s", id)
					}
				}
			}
			counts := map[string]int{}
			for _, d := range diagnostics {
				if d.Code == "go_routes_unread" {
					t.Logf("%s: %s", d.File, d.Message)
				}
				counts[d.Code]++
			}
			t.Logf("%s: %d route nodes for %d claimed census rows (%d production sites with a path), %d extra, %d handlers typed by GR-H2 where the census has none; census D %v; diagnostics %v", corpus, len(nodes), claimed, withPath, extra, upgraded, refused, counts)
		})
	}
}

// goPathLines renders path entries as one sorted line for comparison.
func goPathLines(entries []goPathEntry) string {
	var out []string
	for _, p := range entries {
		out = append(out, fmt.Sprintf("%s slash=%v cond=%v res=%s scope=%s", p.Template, p.TrailingSlash, p.Conditional, p.Resolution, p.Scope))
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}
