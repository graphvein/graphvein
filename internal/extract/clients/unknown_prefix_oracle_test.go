package clients_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
)

// The unknown-prefix tier's precision gate on the corpora (T033,
// from the investigation analysis). No framework oracle answers which route a call
// reaches when a prefix is unknown, so the corpora's own links stand in
// for one. The test is opt-in: GRAPHVEIN_UP_ORACLE_SNAPSHOTS names a
// directory of complete snapshots, one "<corpus>.ndjson" per corpus,
// written by `graphvein index -repository <corpus> -emit <dir>/<corpus>.ndjson`
// with a binary of this revision (docs/extract/clients.md, "Unknown-prefix
// tier"). Without it the test is skipped.
//
//   - Strip simulation: every route's known paths lose their first K = 1
//     or 2 segments, which become an unknown prefix (the route keeps no
//     path, only unresolved_prefix_paths entries for what is left). Each
//     call alternative the corpus links today is matched again; its
//     existing target is the truth. A link to any other route is wrong.
//     Every alternative unmatched today must stay unlinked.
//   - Cross-corpus control: every alternative a corpus leaves unmatched is
//     matched against another corpus's routes with real
//     unresolved_prefix_paths entries (only their entries, no paths): a
//     link is wrong by construction.
//
// Required: no wrong link at K = 1 and K = 2, no unmatched alternative
// linked, no would-link in the control. The recall is logged.

// oracleNode is the part of a snapshot node the oracle reads.
type oracleNode struct {
	Type       string         `json:"type"`
	Kind       string         `json:"kind"`
	Identity   string         `json:"identity"`
	File       string         `json:"file"`
	Attributes map[string]any `json:"attributes"`
}

// oracleSource is one client call alternative with its stored outcome.
type oracleSource struct {
	call     string
	request  clients.CallRequest
	status   string
	match    string
	target   string
	multiple bool
}

func readOracleSnapshot(t *testing.T, path string) (routes []contract.Node, sources []oracleSource) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), contract.MaxRecordBytes+1)
	for scanner.Scan() {
		var n oracleNode
		if err := json.Unmarshal(scanner.Bytes(), &n); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if n.Type != "node" {
			continue
		}
		switch n.Kind {
		case "route":
			routes = append(routes, contract.Node{Type: "node", Kind: "route", Identity: n.Identity, File: n.File, Attributes: n.Attributes})
		case "client_call":
			sources = append(sources, oracleSources(n)...)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return routes, sources
}

// oracleSources lists a call's alternatives with a template, each with the
// base kind the linker used for it (the stored alternative base on a call
// with several or conditional alternatives).
func oracleSources(n oracleNode) []oracleSource {
	a := n.Attributes
	method, ok := a["method"].(string)
	if !ok || a["status"] == "wrapper" || a["status"] == "generated" {
		return nil
	}
	paths, _ := a["paths"].([]any)
	multi := len(paths) > 1
	for _, raw := range paths {
		if p, ok := raw.(map[string]any); ok && p["conditional"] == true {
			multi = true
		}
	}
	scope, _ := a["scope"].(string)
	var out []oracleSource
	for _, raw := range paths {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		template, ok := p["template"].(string)
		if !ok {
			continue
		}
		base, _ := a["base"].(string)
		if multi {
			base, _ = p["base"].(string)
		}
		origin, _ := p["origin"].(string)
		s := oracleSource{call: n.Identity, multiple: multi, request: clients.CallRequest{Method: method, Template: template, Scope: scope, Base: base, Origin: origin, OriginUnresolved: p["origin_resolution"] == "unresolved", TrailingSlash: p["trailing_slash"] == true}}
		s.status, _ = p["status"].(string)
		s.match, _ = p["match"].(string)
		if s.status == "linked" {
			if list, ok := p["candidates"].([]any); ok && len(list) == 1 {
				s.target, _ = list[0].(string)
			}
		}
		out = append(out, s)
	}
	return out
}

// stripped returns routes whose known paths lose their first k segments
// to an unknown prefix: each route keeps no path and one
// unresolved_prefix_paths entry per path with segments left. Routes
// without a path keep their stored entries.
func stripped(routes []contract.Node, k int) []contract.Node {
	return strippedWhere(routes, k, false, nil)
}

// strippedWhere strips the routes only selects (all when only is nil). With
// above, the unknown part is the k segments after the first one, which
// stays known as the entry's above (the K=1 `above` variant: /a/b/c ->
// above /a, template /c), and a path needs k+2 segments.
func strippedWhere(routes []contract.Node, k int, above bool, only map[string]bool) []contract.Node {
	out := make([]contract.Node, 0, len(routes))
	for _, r := range routes {
		attrs := map[string]any{}
		for key, v := range r.Attributes {
			attrs[key] = v
		}
		paths, _ := attrs["paths"].([]any)
		if len(paths) > 0 && (only == nil || only[r.Identity]) {
			var entries []any
			for _, raw := range paths {
				p, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				template, _ := p["template"].(string)
				segs := strings.FieldsFunc(template, func(c rune) bool { return c == '/' })
				skip, head := k, ""
				if above {
					if len(segs) == 0 {
						continue
					}
					skip, head = k+1, "/"+segs[0]
				}
				if len(segs) <= skip {
					continue
				}
				entry := map[string]any{"template": "/" + strings.Join(segs[skip:], "/"), "prefix": "unresolved"}
				if head != "" {
					entry["above"] = head
				}
				for _, key := range []string{"optional", "trailing_slash", "scope"} {
					if v, ok := p[key]; ok {
						entry[key] = v
					}
				}
				entries = append(entries, entry)
			}
			attrs["paths"] = []any{}
			attrs["unresolved_prefix_paths"] = entries
		}
		out = append(out, contract.Node{Type: r.Type, Kind: r.Kind, Identity: r.Identity, File: r.File, Attributes: attrs})
	}
	return out
}

// prefixOnly returns the routes with unresolved_prefix_paths entries,
// without their paths (the control asks about the tier alone).
func prefixOnly(routes []contract.Node) []contract.Node {
	var out []contract.Node
	for _, r := range routes {
		entries, _ := r.Attributes["unresolved_prefix_paths"].([]any)
		if len(entries) == 0 {
			continue
		}
		attrs := map[string]any{}
		for key, v := range r.Attributes {
			attrs[key] = v
		}
		attrs["paths"] = []any{}
		out = append(out, contract.Node{Type: r.Type, Kind: r.Kind, Identity: r.Identity, File: r.File, Attributes: attrs})
	}
	return out
}

func TestUnknownPrefixStripOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_UP_ORACLE_SNAPSHOTS")
	if dir == "" {
		t.Skip("GRAPHVEIN_UP_ORACLE_SNAPSHOTS not set (opt-in corpus check)")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no snapshots in %s (%v)", dir, err)
	}
	sort.Strings(files)
	type corpus struct {
		name    string
		routes  []contract.Node
		sources []oracleSource
	}
	var corpora []corpus
	for _, f := range files {
		routes, sources := readOracleSnapshot(t, f)
		corpora = append(corpora, corpus{strings.TrimSuffix(filepath.Base(f), ".ndjson"), routes, sources})
	}
	for _, k := range []int{1, 2} {
		type tally struct{ truth, correct, ambiguous, testOnly, none, wrong, unmatched, unmatchedLinked, full, fullCorrect int }
		var total tally
		for _, c := range corpora {
			var got tally
			m := clients.NewStoredRouteMatcher(stripped(c.routes, k))
			for _, s := range c.sources {
				switch s.status {
				case "linked":
					if s.target == "" {
						continue
					}
					got.truth++
					r := m.Call(s.request)
					// The investigation's comparison set: exact links of calls whose
					// path is known from its start, without an origin.
					full := s.match == "exact" && s.request.Origin == "" && !s.request.OriginUnresolved && s.request.Base != "unresolved"
					if full {
						got.full++
						if r.Status == "linked" && r.Candidates[0] == s.target {
							got.fullCorrect++
						}
					}
					switch {
					case r.Status == "linked" && r.Candidates[0] == s.target:
						got.correct++
					case r.Status == "linked":
						got.wrong++
						t.Errorf("K=%d %s: %s %s %s (match %s) linked to %s, truth %s", k, c.name, s.call, s.request.Method, s.request.Template, s.match, r.Candidates[0], s.target)
					case r.Status == "ambiguous":
						got.ambiguous++
					case r.Status == "test_only":
						got.testOnly++
					default:
						got.none++
					}
				case "unmatched":
					got.unmatched++
					if r := m.Call(s.request); r.Status == "linked" {
						got.unmatchedLinked++
						t.Errorf("K=%d %s: unmatched %s %s %s linked to %s", k, c.name, s.call, s.request.Method, s.request.Template, r.Candidates[0])
					}
				}
			}
			if got.truth+got.unmatched > 0 {
				t.Logf("K=%d %s: linked today %d: correct %d, ambiguous %d, test_only %d, none %d, wrong %d; full-path exact %d, correct %d; unmatched today %d, linked %d", k, c.name, got.truth, got.correct, got.ambiguous, got.testOnly, got.none, got.wrong, got.full, got.fullCorrect, got.unmatched, got.unmatchedLinked)
			}
			total.truth += got.truth
			total.correct += got.correct
			total.ambiguous += got.ambiguous
			total.testOnly += got.testOnly
			total.none += got.none
			total.wrong += got.wrong
			total.unmatched += got.unmatched
			total.unmatchedLinked += got.unmatchedLinked
			total.full += got.full
			total.fullCorrect += got.fullCorrect
		}
		t.Logf("K=%d all: linked today %d: correct %d (recall %.3f), ambiguous %d, test_only %d, none %d, wrong %d; full-path exact %d, correct %d; unmatched today %d, linked %d", k, total.truth, total.correct, float64(total.correct)/float64(max(total.truth, 1)), total.ambiguous, total.testOnly, total.none, total.wrong, total.full, total.fullCorrect, total.unmatched, total.unmatchedLinked)
	}
	links, ambiguous, matched, allLinks, allAmbiguous := 0, 0, 0, 0, 0
	for _, b := range corpora {
		targets := prefixOnly(b.routes)
		if len(targets) == 0 {
			continue
		}
		perFramework := map[string]int{}
		for _, fw := range frameworkOf(targets) {
			perFramework[fw]++
		}
		t.Logf("control: %s has %d routes with unresolved_prefix_paths (by framework %v)", b.name, len(targets), perFramework)
		m := clients.NewStoredRouteMatcher(targets)
		for _, a := range corpora {
			if a.name == b.name {
				continue
			}
			for _, s := range a.sources {
				r := m.Call(s.request)
				switch r.Status {
				case "linked":
					allLinks++
				case "ambiguous":
					allAmbiguous++
				}
				if s.status != "unmatched" {
					continue
				}
				matched++
				switch r.Status {
				case "linked":
					links++
					t.Errorf("control: %s call %s %s %s would link %s's %s", a.name, s.call, s.request.Method, s.request.Template, b.name, r.Candidates[0])
				case "ambiguous":
					ambiguous++
				}
			}
		}
	}
	t.Logf("control: %d unmatched alternatives tried per corpus with entries: would-link %d, ambiguous %d; every alternative regardless of its own status (UP-13 not applied): linked %d, ambiguous %d", matched, links, ambiguous, allLinks, allAmbiguous)
}

// frameworkOf maps route identities to their stored framework.
func frameworkOf(routes []contract.Node) map[string]string {
	out := map[string]string{}
	for _, r := range routes {
		f, _ := r.Attributes["framework"].(string)
		out[r.Identity] = f
	}
	return out
}

// oracleTally counts one variant's outcomes, per target framework.
type oracleTally struct {
	truth, correct, ambiguous, testOnly, none, wrong, unmatched, unmatchedLinked int
	// kept, keptAmbiguous: links out of the stripped set that stay as
	// today, or become ambiguous (row UP-22: a catch-all beside a stripped
	// route); changedWrong: one that links elsewhere.
	kept, keptAmbiguous, changedWrong int
	// notReplayed: links out of the stripped set the stored matcher does
	// not reproduce even unstripped (not checked). noEntry: a call into the
	// stripped file whose truth route keeps no entry after the strip and
	// that the existing tiers link elsewhere (a catch-all).
	notReplayed, noEntry int
}

func (a *oracleTally) add(b oracleTally) {
	a.truth += b.truth
	a.correct += b.correct
	a.ambiguous += b.ambiguous
	a.testOnly += b.testOnly
	a.none += b.none
	a.wrong += b.wrong
	a.unmatched += b.unmatched
	a.unmatchedLinked += b.unmatchedLinked
	a.kept += b.kept
	a.keptAmbiguous += b.keptAmbiguous
	a.changedWrong += b.changedWrong
	a.notReplayed += b.notReplayed
	a.noEntry += b.noEntry
}

// TestUnknownPrefixPartialStripOracle is the risky case of the tier:
// a call the known routes miss because its true target
// lost its path, while the other routes stay known and compete in the
// existing tiers. For each file holding routes with paths, only that
// file's routes are stripped (K = 1 and 2: the first K segments; and the
// `above` variant: the K = 1 segment after the first one, which stays
// known as the entry's above); every other route keeps its paths. Truth:
// today's links into the stripped file; a link to any other route is
// wrong. Every link out of the stripped set that the stored matcher
// reproduces on the unstripped routes must stay as today (or become
// ambiguous through row UP-22), and every alternative unmatched today must
// stay unlinked. Counts are logged per target framework. Opt-in as
// TestUnknownPrefixStripOracle.
func TestUnknownPrefixPartialStripOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_UP_ORACLE_SNAPSHOTS")
	if dir == "" {
		t.Skip("GRAPHVEIN_UP_ORACLE_SNAPSHOTS not set (opt-in corpus check)")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no snapshots in %s (%v)", dir, err)
	}
	sort.Strings(files)
	variants := []struct {
		name  string
		k     int
		above bool
	}{{"K=1", 1, false}, {"K=2", 2, false}, {"above K=1", 1, true}}
	for _, v := range variants {
		total := oracleTally{}
		byFramework := map[string]*oracleTally{}
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".ndjson")
			routes, sources := readOracleSnapshot(t, f)
			framework := frameworkOf(routes)
			byFile := map[string]map[string]bool{}
			for _, r := range routes {
				if paths, _ := r.Attributes["paths"].([]any); len(paths) == 0 {
					continue
				}
				if byFile[r.File] == nil {
					byFile[r.File] = map[string]bool{}
				}
				byFile[r.File][r.Identity] = true
			}
			var fileNames []string
			for file := range byFile {
				fileNames = append(fileNames, file)
			}
			sort.Strings(fileNames)
			got := oracleTally{}
			// replayed are today's links the stored matcher reproduces on
			// the unstripped routes: only they are checked to stay.
			replayed := map[string]bool{}
			base := clients.NewStoredRouteMatcher(routes)
			for _, s := range sources {
				if s.status == "linked" && s.target != "" {
					if r := base.Call(s.request); r.Status == "linked" && r.Candidates[0] == s.target {
						replayed[s.call+"\x00"+s.request.Method+"\x00"+s.request.Template] = true
					}
				}
			}
			for _, file := range fileNames {
				set := byFile[file]
				strippedRoutes := strippedWhere(routes, v.k, v.above, set)
				// hasEntry: a stripped route that keeps an entry (a path
				// with fewer segments than the strip has none: it is as if
				// the route had no path at all, which the tier cannot see).
				hasEntry := map[string]bool{}
				for _, r := range strippedRoutes {
					if entries, _ := r.Attributes["unresolved_prefix_paths"].([]any); len(entries) > 0 && set[r.Identity] {
						hasEntry[r.Identity] = true
					}
				}
				m := clients.NewStoredRouteMatcher(strippedRoutes)
				for _, s := range sources {
					switch {
					case s.status == "linked" && s.target != "" && set[s.target]:
						one := oracleTally{truth: 1}
						r := m.Call(s.request)
						switch {
						case r.Status == "linked" && r.Candidates[0] == s.target:
							one.correct = 1
						case r.Status == "linked" && !hasEntry[s.target]:
							// The truth route keeps no entry: the existing
							// tiers' answer (a catch-all) does not rest on
							// the tier; logged, as the whole strip counts
							// such a call "none".
							one.noEntry = 1
							t.Logf("%s %s strip %s: %s %s %s linked to %s by the existing tiers; truth %s keeps no entry after the strip", v.name, name, file, s.call, s.request.Method, s.request.Template, r.Candidates[0], s.target)
						case r.Status == "linked":
							one.wrong = 1
							t.Errorf("%s %s strip %s: %s %s %s linked to %s, truth %s", v.name, name, file, s.call, s.request.Method, s.request.Template, r.Candidates[0], s.target)
						case r.Status == "ambiguous":
							one.ambiguous = 1
						case r.Status == "test_only":
							one.testOnly = 1
						default:
							one.none = 1
						}
						got.add(one)
						fw := framework[s.target]
						if byFramework[fw] == nil {
							byFramework[fw] = &oracleTally{}
						}
						byFramework[fw].add(one)
					case s.status == "linked" && s.target != "":
						if !replayed[s.call+"\x00"+s.request.Method+"\x00"+s.request.Template] {
							// The stored matcher alone does not replay
							// this link (a Next.js project or plan it
							// does not hold): no claim about it here.
							got.notReplayed++
							continue
						}
						r := m.Call(s.request)
						switch {
						case r.Status == "linked" && r.Candidates[0] == s.target:
							got.kept++
						case r.Status == "ambiguous":
							got.keptAmbiguous++
						default:
							got.changedWrong++
							t.Errorf("%s %s strip %s: link out of the stripped set %s %s %s changed: %s %v, today %s", v.name, name, file, s.call, s.request.Method, s.request.Template, r.Status, r.Candidates, s.target)
						}
					case s.status == "unmatched":
						got.unmatched++
						if r := m.Call(s.request); r.Status == "linked" {
							got.unmatchedLinked++
							t.Errorf("%s %s strip %s: unmatched %s %s %s linked to %s", v.name, name, file, s.call, s.request.Method, s.request.Template, r.Candidates[0])
						}
					}
				}
			}
			if got.truth+got.unmatched > 0 {
				t.Logf("%s %s (%d files stripped one at a time): into the stripped file %d: correct %d, ambiguous %d, test_only %d, none %d, wrong %d; out of it kept %d, ambiguous %d, changed %d, not replayed %d; unmatched today %d, linked %d", v.name, name, len(fileNames), got.truth, got.correct, got.ambiguous, got.testOnly, got.none, got.wrong, got.kept, got.keptAmbiguous, got.changedWrong, got.notReplayed, got.unmatched, got.unmatchedLinked)
			}
			total.add(got)
		}
		var fws []string
		for fw := range byFramework {
			fws = append(fws, fw)
		}
		sort.Strings(fws)
		for _, fw := range fws {
			c := byFramework[fw]
			t.Logf("%s framework %q: into the stripped file %d: correct %d, ambiguous %d, test_only %d, none %d, wrong %d, truth without entry linked by the existing tiers %d", v.name, fw, c.truth, c.correct, c.ambiguous, c.testOnly, c.none, c.wrong, c.noEntry)
		}
		t.Logf("%s all: into the stripped file %d: correct %d (recall %.3f), ambiguous %d, test_only %d, none %d, wrong %d, truth without entry linked by the existing tiers %d; out of it kept %d, ambiguous %d, changed %d, not replayed %d; unmatched today %d, linked %d", v.name, total.truth, total.correct, float64(total.correct)/float64(max(total.truth, 1)), total.ambiguous, total.testOnly, total.none, total.wrong, total.noEntry, total.kept, total.keptAmbiguous, total.changedWrong, total.notReplayed, total.unmatched, total.unmatchedLinked)
	}
}
