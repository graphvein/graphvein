package routes

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/testscope"
)

// goParse parses every Go file of sources with the indexer's parser and
// extractor, as an index run would cache them.
func goParse(t testing.TB, sources map[string][]byte) map[string]facts.Facts {
	t.Helper()
	files := map[string]facts.Facts{}
	var names []string
	var jobs []parse.Job
	for f, s := range sources {
		if strings.HasSuffix(f, ".go") {
			names = append(names, f)
			jobs = append(jobs, parse.Job{Language: "go", Source: s})
		}
	}
	options := parse.Options{Workers: 4, Timeout: time.Minute, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err := parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("%s: %v", names[i], r.Err)
			return
		}
		files[names[i]] = golang.Extract(names[i], sources[names[i]], r.Root)
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// goLink links sources (file -> text) and returns the result with the
// contract tags the branches emitted.
func goLink(t testing.TB, sources map[string]string, tags map[string]int) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	t.Helper()
	raw := map[string][]byte{}
	for f, s := range sources {
		raw[f] = []byte(s)
	}
	files := goParse(t, raw)
	golang.Link(files, golang.ModulePaths(raw))
	return linkGo(files, raw, testscope.Rules{}, func(row string) {
		if tags != nil {
			tags[row]++
		}
	})
}

// goRouteLines renders route nodes, handler edges and diagnostics as
// sorted lines for comparisons.
func goRouteLines(nodes []contract.Node, edges []contract.Edge, diagnostics []contract.Diagnostic) []string {
	var out []string
	for _, n := range nodes {
		line := n.Identity + " fw=" + n.Attributes["framework"].(string)
		if h, ok := n.Attributes["handler"].(string); ok {
			line += " handler=" + h
		}
		if n.Attributes["handler_inline"] == true {
			line += " inline"
		}
		if e, ok := n.Attributes["handler_expression"].(string); ok {
			line += " expr=" + e
		}
		if m := n.Attributes["middleware"].([]string); len(m) > 0 {
			line += " mw=" + strings.Join(m, ",")
		}
		if h, ok := n.Attributes["host"].(string); ok {
			line += " host=" + h
		}
		if a, ok := n.Attributes["assumptions"].([]string); ok {
			line += " assumptions=" + strings.Join(a, ",")
		}
		if n.Attributes["scope"] == "test" {
			line += " scope=test"
		}
		out = append(out, line)
	}
	for _, e := range edges {
		if e.Kind == "exposes_route" {
			out = append(out, "exposes "+e.Source+" -> "+e.Target)
		}
	}
	for _, d := range diagnostics {
		out = append(out, "diag "+d.File+" "+d.Code+" "+d.Message)
	}
	sort.Strings(out)
	return out
}
