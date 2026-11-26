//go:build graphveincorpus

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestCorpusProjectQuery is T022 B7's corpus gate on RealWorld react +
// express (scratch copies, the pins of TestCorpusProjectIndex): callers
// of every express route a project link reaches list react's call sites
// across the link with its assumption, and impact of an express handler
// change (getArticles, edited in the copy) reaches react UI code through
// the link, both as graphvein impact --project and as graphvein changes
// impact --project. Counts are logged and recorded in the T022 task record (internal).
func TestCorpusProjectQuery(t *testing.T) {
	ctx := context.Background()
	react := pinnedCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := pinnedCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	scratch := t.TempDir()
	copyCheckout(t, react, filepath.Join(scratch, "react"))
	copyCheckout(t, express, filepath.Join(scratch, "express"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	db := filepath.Join(t.TempDir(), "project.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", db)
	dir := filepath.Join(scratch, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, "version: 1\nname: realworld-express\nmembers:\n  - repository: react\n    path: ../react\n  - repository: express\n    path: ../express\n")
	if code, out, errOut := runCLI("index", "--project", dir); code != 0 {
		t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
	}
	s, err := sqlite.OpenRead(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	set, err := s.ProjectLinks(ctx, "realworld-express")
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Every route a calls_route link reaches, with the react calls linked
	// to it.
	linked := map[string]map[string]bool{}
	for _, l := range set.Links {
		if l.Kind != "calls_route" {
			continue
		}
		if linked[l.Target.Identity] == nil {
			linked[l.Target.Identity] = map[string]bool{}
		}
		linked[l.Target.Identity][l.Source.Identity] = true
	}
	projectJSON := func(args ...string) query.ProjectResponse {
		t.Helper()
		code, out, errOut := runCLI(append(args, "--project", dir, "--json")...)
		if code != 0 {
			t.Fatalf("%v = %d\n%s", args, code, errOut)
		}
		var r query.ProjectResponse
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	callSites, functions := 0, 0
	for route, calls := range linked {
		r := projectJSON("callers", route, "--repository", "express")
		got := map[string]bool{}
		for _, res := range r.Results {
			if res.Node.Repository != "react" {
				continue
			}
			if res.Node.Kind == "client_call" {
				got[res.Node.Identity] = true
				link := res.Path[len(res.Path)-1]
				if !link.ProjectLink || link.Assumption != projectlink.DefaultAssumption || res.Provenance != "inferred" || *res.Confidence != 0.7 {
					t.Errorf("callers of %s: %s without the link's assumption and confidence: %+v", route, res.Node.Identity, res)
				}
			} else {
				functions++
			}
		}
		for call := range calls {
			if !got[call] {
				t.Errorf("callers of %s miss the linked react call %s", route, call)
			}
		}
		callSites += len(got)
	}
	t.Logf("callers --project: %d express routes with react callers, %d react call sites (distance 1), %d react enclosing functions (distance 2)", len(linked), callSites, functions)
	if len(linked) == 0 || callSites != len(linkedCalls(set)) {
		t.Fatalf("callers listed %d react call sites, links name %d", callSites, len(linkedCalls(set)))
	}

	// The handler of GET /api/articles.
	s, err = sqlite.OpenRead(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := s.Nodes(ctx, "express", store.Query{Kind: "route"})
	if err != nil {
		t.Fatal(err)
	}
	var route string
	for _, n := range routes {
		if n.Name == "GET /api/articles" {
			route = n.Identity
		}
	}
	handlers, err := s.Edges(ctx, "express", []string{route}, []string{"exposes_route"}, store.Incoming)
	s.Close()
	if err != nil || len(handlers) != 1 {
		t.Fatalf("handler of %q: %v %v", route, handlers, err)
	}
	handler := handlers[0].Source
	// react's UI code: its pages (src/pages), whose route loaders and
	// actions make the API calls. react-router wires them to components
	// through lazy route objects, so no static call edge leads on to the
	// .tsx components (counted separately, expected 0).
	uiReached := func(repo, file string) bool { return repo == "react" && strings.HasPrefix(file, "src/pages/") }

	graph := projectJSON("impact", handler, "--repository", "express", "--depth", "6")
	reactNodes, ui, tsx := 0, 0, 0
	for _, r := range graph.Results {
		if r.Node.Repository == "react" {
			reactNodes++
			if uiReached(r.Node.Repository, r.Node.File) {
				ui++
			}
			if strings.HasSuffix(r.Node.File, ".tsx") {
				tsx++
			}
		}
	}
	t.Logf("impact --project of %s (depth 6): %d results, %d in react, %d in react src/pages, %d in .tsx files; %d possibly missing callers", handler, len(graph.Results), reactNodes, ui, tsx, graph.PossiblyMissingCallers)
	if ui == 0 {
		t.Error("impact --project of the express handler reaches no react UI code")
	}

	// A change of the handler: edit the copy, index the project again.
	file := filepath.Join(scratch, "express", "apps", "article", "controller", "getArticles.ts")
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), "  const { user } = req;\n") {
		t.Fatalf("getArticles.ts changed upstream: %v", err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(data), "  const { user } = req;\n", "  const { user } = req;\n  void user;\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI("index", "--project", dir); code != 0 {
		t.Fatalf("second index --project = %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut := runCLI("changes", "impact", handler, "--project", dir, "--repository", "express", "--depth", "6", "--json")
	if code != 0 {
		t.Fatalf("changes impact --project = %d\n%s", code, errOut)
	}
	var report changes.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	im := report.Impacts[0].Impact
	for _, v := range changes.ImpactViolations(im) {
		t.Errorf("changes impact: %s", v)
	}
	reactPaths, uiPaths, linkSteps := 0, 0, 0
	for _, p := range im.Paths {
		if p.Repository != "react" {
			continue
		}
		reactPaths++
		if p.Provenance != "inferred" || p.Claim != "may depend on" || len(p.Assumptions) == 0 || p.Confidence == nil || *p.Confidence > 0.7 {
			t.Errorf("react path %s: %s %s %v %v", p.Node, p.Provenance, p.Claim, p.Confidence, p.Assumptions)
		}
		if p.Relation == "I12" || p.Relation == "I13" {
			linkSteps++
		}
		if uiReached("react", identityFileOf(p.Node)) {
			uiPaths++
		}
	}
	t.Logf("changes impact --project of %s (depth 6): %d paths, %d in react (%d through I12/I13 at the hop, %d in src/pages), caps %v", handler, len(im.Paths), reactPaths, linkSteps, uiPaths, im.Caps)
	if uiPaths == 0 {
		t.Error("changes impact --project of the express handler reaches no react UI code")
	}
}

func linkedCalls(set store.ProjectLinkSet) map[string]bool {
	out := map[string]bool{}
	for _, l := range set.Links {
		if l.Kind == "calls_route" {
			out[l.Source.Identity] = true
		}
	}
	return out
}

// identityFileOf is the file part of an identity (kind:file#name).
func identityFileOf(id string) string {
	_, rest, _ := strings.Cut(id, ":")
	file, _, _ := strings.Cut(rest, "#")
	return file
}
