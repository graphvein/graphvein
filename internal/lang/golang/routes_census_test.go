package golang

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// routeCensusRow is the part of a T023 census row (docs/contract/claims/data/go-http-routers-census.jsonl)
// the payload census reads: the site's position and call text.
type routeCensusRow struct {
	Corpus string `json:"corpus"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Kind   string `json:"kind"`
	Call   string `json:"call"`
}

// routeSites indexes a file's captured router calls (row RC-router) by
// position, with the selector names of the call and of its receiver chain
// (a chain shares the outer call's start).
func routeSites(g *facts.GoFacts) map[string][]string {
	out := map[string][]string{}
	if g == nil || g.Routes == nil {
		return out
	}
	for _, f := range g.Routes.Funcs {
		for _, c := range f.Calls {
			if c.Kind != facts.GoCallRouter {
				continue
			}
			// census columns are go/token's: 1-based bytes.
			key := fmt.Sprintf("%d:%d", c.Span.StartLine, c.Span.StartColumn+1)
			for e := &c.Call; e != nil && e.Kind == facts.GoCall && e.Callee != nil && e.Callee.Kind == facts.GoSelector; e = e.Callee.Object {
				out[key] = append(out[key], e.Callee.Name)
			}
		}
	}
	return out
}

// TestGoRoutesCensus checks the router payload against the investigation's census of
// the five Go corpora: every route and compose row (a registration or a
// composition call on a router object the census proved) is a captured
// router call at the row's position, in a file that passes the route
// gate, with the row's method name. Captured router calls the census does
// not list (HTTP clients, Use chains, calls on unproven receivers) are the
// link pass's to refuse; the test reports their count per corpus. It only
// parses; set GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS to the directory holding
// the checkouts (scripts/cloud-corpus.sh realworld-gin gotify dozzle
// miniflux owncast).
func TestGoRoutesCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS is not set")
	}
	file, err := os.Open("../../../docs/contract/claims/data/go-http-routers-census.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows := map[string][]routeCensusRow{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var r routeCensusRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Kind == "route" || r.Kind == "compose" {
			rows[r.Corpus] = append(rows[r.Corpus], r)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, corpus := range []string{"realworld-gin", "gotify", "dozzle", "miniflux", "owncast"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, corpus)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%s: %v (run scripts/cloud-corpus.sh %s)", corpus, err, corpus)
			}
			files, sizes := gormCapture(t, root)
			sites := map[string]map[string][]string{}
			gated, routeBytes, total := 0, 0, 0
			for p, f := range files {
				sites[p] = routeSites(f.Go)
				total += sizes[p]
				if f.Go.Routes != nil {
					gated++
					data, _ := json.Marshal(f.Go.Routes)
					routeBytes += len(data)
				}
			}
			matched := map[string]bool{}
			for _, r := range rows[corpus] {
				f, ok := files[r.File]
				if !ok {
					t.Errorf("%s:%d: census file not scanned", r.File, r.Line)
					continue
				}
				if f.Go.Routes == nil {
					t.Errorf("%s:%d: %s: file not gated", r.File, r.Line, r.Call)
					continue
				}
				key := fmt.Sprintf("%d:%d", r.Line, r.Col)
				found := false
				for _, name := range sites[r.File][key] {
					if strings.Contains(r.Call, "."+name+"(") {
						found = true
					}
				}
				if !found {
					t.Errorf("%s:%s: %s %q: no captured router call (have %v)", r.File, key, r.Kind, r.Call, sites[r.File][key])
				}
				matched[r.File+":"+key] = true
			}
			extra := map[string]int{}
			for p, byPos := range sites {
				for key, names := range byPos {
					if !matched[p+":"+key] {
						extra[names[0]]++
						if os.Getenv("GV_EXTRA") != "" {
							t.Logf("extra %s:%s %v", p, key, names)
						}
					}
				}
			}
			var names []string
			for n, k := range extra {
				names = append(names, fmt.Sprintf("%s=%d", n, k))
			}
			sort.Strings(names)
			t.Logf("%s: %d census sites, %d Go files, %d gated, route payload %d of %d payload bytes; router calls the census does not list: %s", corpus, len(rows[corpus]), len(files), gated, routeBytes, total, strings.Join(names, ", "))
		})
	}
}
