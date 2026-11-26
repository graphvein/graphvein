package routes_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	expressPathFixture  = "../../../testdata/fixtures/express5-paths"
	expressPathContract = "../../../docs/contract/claims/express5-paths.md"
)

// expressPathRoute is the golden's view of one route: its pattern and
// path entries.
type expressPathRoute struct {
	Identity string `json:"identity"`
	Pattern  any    `json:"pattern"`
	Handler  any    `json:"handler_expression"`
	Paths    any    `json:"paths"`
}

type expressPathGolden struct {
	Routes      []expressPathRoute `json:"routes"`
	Diagnostics []string           `json:"diagnostics"`
}

// indexExpressPathFixture indexes a copy of the fixture (cold, then a
// warm replay) and returns the snapshot and the rows the linker reached.
func indexExpressPathFixture(t *testing.T) (store.Snapshot, map[string]bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(expressPathFixture)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	tags := map[string]bool{}
	routes.SetExpressPathTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { routes.SetExpressPathTag(nil) })
	snapshot, _ := indexAuthFixture(t, root)
	return snapshot, tags
}

func expressPathView(s store.Snapshot) expressPathGolden {
	out := expressPathGolden{Routes: []expressPathRoute{}, Diagnostics: []string{}}
	for _, n := range s.Nodes {
		if n.Kind == "route" {
			out.Routes = append(out.Routes, expressPathRoute{n.Identity, n.Attributes["pattern"], n.Attributes["handler_expression"], n.Attributes["paths"]})
		}
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Identity < out.Routes[j].Identity })
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_") {
			out.Diagnostics = append(out.Diagnostics, fmt.Sprintf("%s %s %s: %s", d.File, d.Severity, d.Code, d.Message))
		}
	}
	sort.Strings(out.Diagnostics)
	return out
}

// contractRows reads the claim table of the contract document: row ids in
// order.
func contractRows(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(expressPathContract)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| EX5-") {
			continue
		}
		ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
	}
	return ids
}

// TestExpressPathContract indexes the express5-paths fixture, checks the
// routes and route diagnostics against the golden, that the code table
// equals the contract document's table row for row, and that the fixture
// reaches every row.
func TestExpressPathContract(t *testing.T) {
	snapshot, tags := indexExpressPathFixture(t)
	golden.Check(t, expressPathFixture+".golden.json", expressPathView(snapshot))
	var code []string
	for _, r := range routes.ExpressPathContract {
		code = append(code, r.ID)
		if !tags[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	if docs := contractRows(t); !reflect.DeepEqual(code, docs) {
		t.Errorf("code rows %v, contract document rows %v", code, docs)
	}
	for tag := range tags {
		found := false
		for _, r := range routes.ExpressPathContract {
			found = found || r.ID == tag
		}
		if !found {
			t.Errorf("tag %s names no row", tag)
		}
	}
}

// claimRegexp reads a stored path entry as the requests it claims, keeping
// slashes as Express sees them (T028 review, harness 1): a literal segment
// as written (the oracle sends the written case), a `{x}` segment one or
// more characters other than `/`, a final `{*x}` or `{*}` a slash and one
// or more characters (path-to-regexp 8's `[^]+`; Express 4's `.*` is wider,
// the documented zero-segment gap), one optional trailing slash (both
// majors ignore it by default), and an optional entry's final parameter
// may be absent. A request with an empty segment matches only where a
// catch-all takes it, as in Express.
func claimRegexp(entry map[string]any) *regexp.Regexp {
	template, _ := entry["template"].(string)
	segments := strings.FieldsFunc(template, func(c rune) bool { return c == '/' })
	var body strings.Builder
	for i, s := range segments {
		last := i == len(segments)-1
		switch {
		case last && strings.HasPrefix(s, "{*"):
			body.WriteString("/.+")
		case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
			if last && entry["optional"] == true {
				body.WriteString("(?:/[^/]+)?")
			} else {
				body.WriteString("/[^/]+")
			}
		default:
			body.WriteString("/" + regexp.QuoteMeta(s))
		}
	}
	return regexp.MustCompile("^" + body.String() + "/?$")
}

// TestExpressPathOracle compares the stored path entries of the
// express5-paths fixture with what Express 4.21.1 and 5.1.0 matched
// (scripts/oracle/express_paths.sh). Opt-in, never run by CI:
// GRAPHVEIN_EXPRESS_PATH_ORACLE lists the oracle's JSON outputs, comma
// separated (one per Express version).
//
// For every request sent to an app and every route of that app's module
// with path entries (oracle.json names the routes of apps other than
// `app`), the route claims the request exactly when Express
// answered it with that route, except the requests of the plan's `gaps`,
// where the row states that Express also matches and Graphvein does not
// claim (zero catch-all segments, a skipped non-final optional parameter). A route without path entries (rows
// EX5-W10, EX5-W14) is not compared; its answers are counted. Every
// pattern of a `throws` list must make Express 5 throw and have no path.
func TestExpressPathOracle(t *testing.T) {
	outputs := os.Getenv("GRAPHVEIN_EXPRESS_PATH_ORACLE")
	if outputs == "" {
		t.Skip("set GRAPHVEIN_EXPRESS_PATH_ORACLE (scripts/oracle/express_paths.sh)")
	}
	snapshot, _ := indexExpressPathFixture(t)
	type route struct {
		file, handler, pattern string
		paths                  []map[string]any
	}
	var all []route
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" {
			continue
		}
		h, _ := n.Attributes["handler_expression"].(string)
		p, _ := n.Attributes["pattern"].(string)
		r := route{file: n.File, handler: h, pattern: p}
		entries, _ := n.Attributes["paths"].([]any)
		for _, e := range entries {
			m, _ := e.(map[string]any)
			r.paths = append(r.paths, m)
		}
		all = append(all, r)
	}
	data, err := os.ReadFile(expressPathFixture + "/oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Groups []struct {
			Dir    string
			Module string
			Gaps   [][]string
			// Others lists the routes of the module's apps other than
			// `app`, by handler.
			Others map[string][]string `json:"routes_of_other_apps"`
		}
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	gaps := map[string]string{}
	modules := map[string]string{}
	appOf := map[string]string{}
	for _, g := range plan.Groups {
		module := g.Dir + "/" + g.Module
		modules[module] = module
		for app, handlers := range g.Others {
			for _, h := range handlers {
				appOf[module+"\x00"+h] = app
			}
		}
		for _, gap := range g.Gaps {
			gaps[module+"\x00"+gap[0]+"\x00"+gap[1]+"\x00"+gap[2]] = gap[3]
		}
	}
	for _, file := range strings.Split(outputs, ",") {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var oracle struct {
			Versions map[string]string `json:"versions"`
			Results  []struct {
				Module, App, Path, Route string
				Status                   int
			} `json:"results"`
			Throws []struct {
				Module, Pattern string
				Threw           bool
				Error           string
			} `json:"throws"`
		}
		if err := json.Unmarshal(data, &oracle); err != nil {
			t.Fatal(err)
		}
		compared, agree, gapCount, unclaimed, disagree := 0, 0, 0, 0, 0
		seenGaps := map[string]bool{}
		for _, res := range oracle.Results {
			answered := ""
			if res.Status == 200 {
				answered = res.Route
			}
			for _, r := range all {
				app := appOf[res.Module+"\x00"+r.handler]
				if app == "" {
					app = "app"
				}
				if r.file != modules[res.Module] || app != res.App {
					continue
				}
				matched := answered == r.handler
				if len(r.paths) == 0 {
					if matched {
						unclaimed++
					}
					continue
				}
				claimed := false
				for _, p := range r.paths {
					claimed = claimed || claimRegexp(p).MatchString(res.Path)
				}
				compared++
				key := res.Module + "\x00" + res.App + "\x00" + res.Path + "\x00" + r.handler
				if row, ok := gaps[key]; ok {
					seenGaps[key] = true
					if matched && !claimed {
						gapCount++
						continue
					}
					t.Errorf("express %s: %s %s: gap %s for %s does not hold (express matched %t, claimed %t)", oracle.Versions["express"], res.Module, res.Path, row, r.handler, matched, claimed)
					disagree++
					continue
				}
				if claimed == matched {
					agree++
					continue
				}
				disagree++
				t.Errorf("express %s: %s app %s GET %s: route %s (%s) claimed %t, express matched %t (answered %d by %q)", oracle.Versions["express"], res.Module, res.App, res.Path, r.handler, r.pattern, claimed, matched, res.Status, res.Route)
			}
		}
		ran := map[string]bool{}
		for _, res := range oracle.Results {
			ran[res.Module] = true
		}
		for key, row := range gaps {
			if module, _, _ := strings.Cut(key, "\x00"); ran[module] && !seenGaps[key] {
				t.Errorf("express %s: gap %s (%q) was not exercised", oracle.Versions["express"], row, key)
			}
		}
		for _, th := range oracle.Throws {
			ok := th.Threw
			for _, r := range all {
				if r.file == th.Module && r.pattern == th.Pattern && len(r.paths) != 0 {
					ok = false
				}
			}
			if !ok {
				t.Errorf("express %s: pattern %s: threw %t (%s), or a path is stored", oracle.Versions["express"], th.Pattern, th.Threw, th.Error)
				disagree++
			}
		}
		t.Logf("express %s (path-to-regexp %s, router %s): %d requests, %d route/request pairs compared: %d agree, %d documented recall gaps, %d disagree; %d answers by routes with no path entry (not compared); %d patterns refused by Express as claimed",
			oracle.Versions["express"], oracle.Versions["path-to-regexp"], oracle.Versions["router"], len(oracle.Results), compared, agree, gapCount, disagree, unclaimed, len(oracle.Throws))
	}
}
