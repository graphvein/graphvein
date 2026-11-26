package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// archProjectBodies serves the architecture endpoints of the projectimpact
// fixture as a project (TOP-2 of 2026-11-18, rows CE8 and CH13) on both
// backends, checks the golden and returns the bodies by backend and name;
// tags collects the rows the requests reached.
func archProjectBodies(t *testing.T, tags map[string]bool, check bool) map[string]map[string]any {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	archTag = func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	}
	t.Cleanup(func() { archTag = nil })
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(root, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(root, "backend"), w, "backend")
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		got := map[string]any{}
		for _, c := range []struct{ name, path string }{
			{"frontend-overview", "/api/v0/architecture?repository=frontend"},
			{"backend-overview", "/api/v0/architecture?repository=backend"},
			{"frontend-chain-call", "/api/v0/architecture/chain?repository=frontend&direction=down&id=" + url.QueryEscape("client_call:src/api.ts#listUsers.GET%20%60%24%7Bimport.meta.env.VITE_API%7D%2Ft022b7%2Fusers%60")},
			{"backend-chain-route", "/api/v0/architecture/chain?repository=backend&direction=up&id=" + url.QueryEscape(usersRoute)},
			{"frontend-chain-component", "/api/v0/architecture/chain?repository=frontend&id=."},
		} {
			rec := get(t, opt, http.MethodGet, c.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", c.path, rec.Code, rec.Body.String())
			}
			got[c.name] = normalizeProject(t, rec.Body.Bytes(), runLabels(t, s, "frontend", "backend"))
		}
		if check {
			golden.CheckRoot(t, filepath.Join("testdata", "architecture-project.golden.json"), got, root)
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	return bodies
}

// TestArchitectureProject checks the architecture view of project members
// (rows CE8, CH13): the frontend's calls the pass links to the backend are
// a link to the backend's muted card with the assumption, counted as
// linked across repositories and never in the "not linked" ghost; the
// backend shows the incoming link; a chain crosses into the other card as
// far as the stored link and ends there; both backends agree.
func TestArchitectureProject(t *testing.T) {
	bodies := archProjectBodies(t, map[string]bool{}, true)
	for backend, got := range bodies {
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]struct {
			Ghosts    []archGhost     `json:"ghosts"`
			NotProven []archNotProven `json:"not_proven"`
			Links     []archLink      `json:"links"`
			Project   *struct {
				Members []archRemoteMember `json:"members"`
				Across  []archAcross       `json:"across"`
			} `json:"project"`
			Down *struct {
				Ends []chainEnd `json:"ends"`
			} `json:"down"`
			Up *struct {
				Ends []chainEnd `json:"ends"`
			} `json:"up"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		front := doc["frontend-overview"]
		if front.Project == nil || len(front.Project.Across) != 1 || front.Project.Across[0].LinkedAcross == 0 {
			t.Fatalf("%s: frontend project block %+v", backend, front.Project)
		}
		for _, g := range front.Ghosts {
			if g.Family == familyHTTP && g.Component == front.Project.Across[0].Component && g.Total+front.Project.Across[0].LinkedAcross != front.Project.Across[0].RepositoryNotLinked {
				t.Errorf("%s: ghost %+v and across %+v disagree", backend, g, front.Project.Across[0])
			}
		}
		// R1: the frontend's operation the pass links to a backend route is
		// documented there, not "documented, no route".
		if a := front.Project.Across[0]; a.OperationsAcross != 1 || a.RepositoryNoRoute != 1 {
			t.Errorf("%s: frontend operations across %+v", backend, a)
		}
		// T044: the backend card counts the frontend's operation the pass
		// links to its routes beside the calls (CE8 R1), never fewer links
		// than the pass stored; the backend's view counts it as incoming.
		if len(front.Project.Members) != 1 || front.Project.Members[0].OperationsOut != front.Project.Across[0].OperationsAcross || front.Project.Members[0].OperationsIn != 0 {
			t.Errorf("%s: frontend card %+v, across %+v", backend, front.Project.Members, front.Project.Across[0])
		}
		linked := 0
		for _, l := range front.Links {
			if l.Row == "CE8" {
				linked += l.Count
				if l.Target != remoteCard("backend") || len(l.Assumptions) != 1 || l.Assumptions[0] != defaultAssume || l.Cells.Protocol.Value != "HTTP" || l.Cells.TLS.Value != "TLS ?" {
					t.Errorf("%s: frontend CE8 link %+v", backend, l)
				}
			}
		}
		if linked != front.Project.Across[0].LinkedAcross {
			t.Errorf("%s: CE8 links count %d calls, across says %d", backend, linked, front.Project.Across[0].LinkedAcross)
		}
		for _, np := range front.NotProven {
			if np.Kind == "documented_no_route" {
				t.Errorf("%s: frontend still lists %q", backend, np.Text)
			}
		}
		back := doc["backend-overview"]
		incoming := false
		for _, l := range back.Links {
			incoming = incoming || l.Row == "CE8" && l.Source == remoteCard("frontend")
		}
		if !incoming {
			t.Errorf("%s: backend overview has no incoming CE8 link: %+v", backend, back.Links)
		}
		// The frontend's card must exist on the backend's view: a missing
		// project block or card fails here instead of skipping the checks.
		if back.Project == nil || len(back.Project.Members) != 1 {
			t.Fatalf("%s: backend project block %+v", backend, back.Project)
		}
		if card := back.Project.Members[0]; card.ID != remoteCard("frontend") || card.CallsIn == 0 || card.CallsOut != 0 || card.OperationsIn != 1 || card.OperationsOut != 0 {
			t.Errorf("%s: backend card %+v", backend, card)
		}
		for name, ends := range map[string][]chainEnd{"frontend-chain-call": downEnds(doc["frontend-chain-call"].Down), "backend-chain-route": upEnds(doc["backend-chain-route"].Up), "frontend-chain-component": downEnds(doc["frontend-chain-component"].Down)} {
			found := false
			for _, e := range ends {
				found = found || e.Row == "CH13" && strings.HasPrefix(e.Text, "continues in ")
			}
			if !found {
				t.Errorf("%s: %s has no CH13 end line: %+v", backend, name, ends)
			}
		}
	}
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		a, _ := json.Marshal(bodies["sqlite"])
		b, _ := json.Marshal(bodies["postgres"])
		if string(a) != string(b) {
			t.Error("SQLite and Postgres bodies differ")
		}
	}
}

func downEnds(s *struct {
	Ends []chainEnd `json:"ends"`
}) []chainEnd {
	if s == nil {
		return nil
	}
	return s.Ends
}

func upEnds(s *struct {
	Ends []chainEnd `json:"ends"`
}) []chainEnd {
	return downEnds(s)
}

// TestProjectCardOperations checks the CE8 card counts of documents_route
// links (T044 review): an operation documenting two routes of a member
// counts once on each side, and a member that only documents links reach
// still gets its card, with no calls.
func TestProjectCardOperations(t *testing.T) {
	const (
		operation  = "openapi_operation:api/openapi.yaml#GET%20%2Fusers"
		listRoute  = "route:src/server.ts#GET%20%2Fusers"
		aliasRoute = "route:src/server.ts#GET%20%2Fpeople"
	)
	model := &archModel{fileOf: map[string]string{"api/openapi.yaml": "api", "src/server.ts": "src"}}
	documents := func(from, to, route string) store.ProjectLink {
		return store.ProjectLink{Kind: projectlink.KindDocumentsRoute,
			Source: store.ProjectEndpoint{Repository: from, Identity: operation},
			Target: store.ProjectEndpoint{Repository: to, Identity: route}}
	}
	cards := func(px *archProjectIn) map[string]archRemoteMember {
		g := &archGraph{testEdges: map[string]int{}}
		if links := projectLinks(px, model, g, map[string]contract.Node{}, &documentation{}, func(string) bool { return false }, scopeProduction); len(links) != 0 {
			t.Errorf("documents links drew CE8 call links: %+v", links)
		}
		out := map[string]archRemoteMember{}
		for _, m := range px.doc.Members {
			out[m.Repository] = m
		}
		return out
	}

	// The documenting member's view: its one operation documents two
	// routes of "backend"; "backend" has no call link at all.
	got := cards(&archProjectIn{doc: &archProjectDoc{}, docsOut: []store.ProjectLink{
		documents("spec", "backend", listRoute), documents("spec", "backend", aliasRoute),
	}})
	if c, ok := got["backend"]; !ok || c.ID != remoteCard("backend") || c.OperationsOut != 1 || c.OperationsIn != 0 || c.CallsOut != 0 || c.CallsIn != 0 {
		t.Errorf("spec view: backend card %+v (present %t), want operations_out 1 and no calls", c, ok)
	}

	// The documented member's view: the same two links come in.
	got = cards(&archProjectIn{doc: &archProjectDoc{}, docsIn: []store.ProjectLink{
		documents("spec", "backend", listRoute), documents("spec", "backend", aliasRoute),
	}})
	if c, ok := got["spec"]; !ok || c.ID != remoteCard("spec") || c.OperationsIn != 1 || c.OperationsOut != 0 || c.CallsOut != 0 || c.CallsIn != 0 {
		t.Errorf("backend view: spec card %+v (present %t), want operations_in 1 and no calls", c, ok)
	}
	if len(got) != 1 {
		t.Errorf("backend view cards %+v, want spec only", got)
	}
}
