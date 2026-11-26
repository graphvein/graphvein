package changes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
)

const projectFixtureDir = "../../testdata/fixtures/projectimpact"

// projectManifest names the fixture's two members.
const projectManifest = "version: 1\nname: users\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n"

// copyProjectImpactFixture copies the fixture to a temporary directory,
// restores the inert OpenAPI name (fixture README) and writes the
// manifest.
func copyProjectImpactFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(projectFixtureDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "frontend", "openapi.yaml.fixture"), filepath.Join(root, "frontend", "openapi.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(projectManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func indexRepository(t *testing.T, ctx context.Context, s store.Store, root, repo, cache string) {
	t.Helper()
	var stream bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: repo, Parser: parse.Options{Workers: 1, CacheDir: cache}}); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Stream(ctx, &stream, s); err != nil {
		t.Fatal(err)
	}
}

// edit replaces old with new in a fixture copy's file; the test fails when
// old is not there, so a fixture change cannot silently skip a revision.
func edit(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s has no %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectGolden is the golden document: the backend's comparison, the
// project-scoped traversals and the link journal diff. Summary, items and
// impacts have the shape TestChangeClaimContract reads.
type projectGolden struct {
	Summary changes.Summary         `json:"summary"`
	Items   []changes.Item          `json:"items"`
	Impacts []changes.NamedImpact   `json:"impacts"`
	Links   changes.ProjectLinkDiff `json:"project_links"`
}

// TestProjectImpactGolden is T022 B7's change impact gate on the
// projectimpact fixture, on both backends: a change of the backend's
// users table reaches the frontend's UI functions through the handler,
// its route and the project link (table -> handler -> route -> client
// call -> api function -> UI function), and the frontend's OpenAPI
// operation through a documents_route link; every cross-repository step
// is inferred with the link's assumption; a link left stale by a later
// frontend index is still followed and marked; the link journal diff
// lists the link the backend's removed route took with it.
func TestProjectImpactGolden(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := copyProjectImpactFixture(t)
		cache := filepath.Join(t.TempDir(), "cache")
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		pass := func() {
			t.Helper()
			if _, _, err := projectlink.Run(ctx, s, project, projectlink.ManifestHash(project)); err != nil {
				t.Fatal(err)
			}
		}
		frontend, backendDir := filepath.Join(root, "frontend"), filepath.Join(root, "backend")
		indexRepository(t, ctx, s, frontend, "frontend", cache)
		indexRepository(t, ctx, s, backendDir, "backend", cache)
		pass()

		// Revision 2 of the backend: the table gains a column, the list
		// handler's query changes, the POST route is no longer registered.
		edit(t, filepath.Join(backendDir, "src", "db", "schema.ts"), "  email: text(\"email\").notNull(),\n", "  email: text(\"email\").notNull(),\n  name: text(\"name\"),\n")
		edit(t, filepath.Join(backendDir, "src", "server.ts"), "db.select().from(users)", "db.select().from(users).limit(10)")
		edit(t, filepath.Join(backendDir, "src", "server.ts"), "app.post('/t022b7/users', createUser);\n", "")
		indexRepository(t, ctx, s, backendDir, "backend", cache)
		pass()

		scope := func() *changes.ProjectScope {
			t.Helper()
			view, err := projectlink.ReadView(ctx, s, project)
			if err != nil {
				t.Fatal(err)
			}
			return &changes.ProjectScope{Project: project.Name, Members: []string{"frontend", "backend"}, Links: view}
		}
		c, err := changes.Compare(ctx, s, "backend", changes.Base{Kind: changes.BasePrevious}, changes.Options{Root: backendDir, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
		if err != nil {
			t.Fatal(err)
		}
		doc := projectGolden{Summary: c.Summary, Items: []changes.Item{}}
		r, err := changes.BuildReport(ctx, s, c, changes.Filter{IncludeTests: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		doc.Items = r.Items
		fresh := scope()
		impact := func(id string, depth int, sc *changes.ProjectScope) *changes.ImpactResult {
			t.Helper()
			im, err := changes.Impact(ctx, s, c, changes.ImpactRequest{ID: id, Depth: depth, Project: sc})
			if err != nil {
				t.Fatalf("impact of %s: %v", id, err)
			}
			for _, v := range changes.ImpactViolations(im) {
				t.Errorf("impact of %s: %s", id, v)
			}
			doc.Impacts = append(doc.Impacts, changes.NamedImpact{Seed: id, Impact: im})
			return im
		}
		table := impact("table:src/db/schema.ts#users", changes.MaxDepth, fresh)
		handler := impact("function:src/server.ts#listUsers", 4, fresh)

		// The single-repository traversal of the same item is unchanged by
		// project mode: it stops at the backend's route.
		single, err := changes.Impact(ctx, s, c, changes.ImpactRequest{ID: "function:src/server.ts#listUsers", Depth: 4})
		if err != nil {
			t.Fatal(err)
		}
		if single.Project != "" || single.Members != nil {
			t.Errorf("single-repository impact carries project fields: %+v", single)
		}
		for _, p := range single.Paths {
			if p.Repository != "" || p.Relation == "I12" || p.Relation == "I13" {
				t.Errorf("single-repository impact crossed into the project: %+v", p)
			}
		}

		reachesUI := func(im *changes.ImpactResult) bool {
			for _, p := range im.Paths {
				if p.Repository == "frontend" && p.Node == "function:src/page.ts#UsersPage" {
					return p.Provenance == "inferred" && p.Claim == "may depend on" && len(p.Assumptions) == 1 && p.Assumptions[0] == projectlink.DefaultAssumption && !p.Stale
				}
			}
			return false
		}
		if !reachesUI(table) || !reachesUI(handler) {
			t.Errorf("the backend's table and handler change do not reach the frontend UI through the project link")
		}

		// The frontend indexed again without a link pass leaves the links
		// stale (the comparison of the backend is unaffected): still
		// followed, marked with the reason.
		indexRepository(t, ctx, s, frontend, "frontend", cache)
		stale, err := changes.Impact(ctx, s, c, changes.ImpactRequest{ID: "function:src/server.ts#listUsers", Depth: 4, Project: scope()})
		if err != nil {
			t.Fatal(err)
		}
		staleSeen := false
		for _, p := range stale.Paths {
			if p.Repository == "frontend" {
				staleSeen = p.Stale
				for _, st := range p.Steps {
					if st.Relation == "I12" && (len(st.StaleReasons) != 1 || st.StaleReasons[0] != "frontend "+projectlink.ReasonIndexedAgain) {
						t.Errorf("stale link step reasons = %v", st.StaleReasons)
					}
				}
			}
		}
		if !staleSeen {
			t.Errorf("a stale project link is not followed and marked: %+v", stale.Paths)
		}
		doc.Impacts = append(doc.Impacts, changes.NamedImpact{Seed: "function:src/server.ts#listUsers (stale links)", Impact: stale})

		runs, err := s.ProjectLinkRuns(ctx, project.Name)
		if err != nil || len(runs) != 2 {
			t.Fatalf("link journal = %d passes, %v", len(runs), err)
		}
		doc.Links, err = changes.DiffProjectLinks(project.Name, runs, runs[0].Seq, runs[1].Seq)
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, filepath.Join("..", "..", "testdata", "fixtures", "change-impact.project.golden.json"), normalise(t, doc))
	})
}

// TestDiffProjectLinks covers the link journal diff on hand-made passes:
// added, removed, changed (payload hash), unchanged, and an unknown pass.
func TestDiffProjectLinks(t *testing.T) {
	key := func(id string) store.ProjectLinkKey {
		return store.ProjectLinkKey{Kind: "calls_route", SourceRepository: "web", SourceIdentity: id, TargetRepository: "api", TargetIdentity: "route:r"}
	}
	entry := func(id, hash string) store.ProjectLinkEntry {
		return store.ProjectLinkEntry{ProjectLinkKey: key(id), PayloadHash: hash}
	}
	runs := []store.ProjectLinkRun{
		{Seq: 4, ManifestHash: "m1", Links: []store.ProjectLinkEntry{entry("a", "1"), entry("b", "1"), entry("c", "1")}},
		{Seq: 5, ManifestHash: "m2", Links: []store.ProjectLinkEntry{entry("a", "1"), entry("c", "2"), entry("d", "1")}},
	}
	d, err := changes.DiffProjectLinks("p", runs, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 1 || d.Added[0] != key("d") || len(d.Removed) != 1 || d.Removed[0] != key("b") || len(d.Changed) != 1 || d.Changed[0] != key("c") || !d.Manifest {
		t.Errorf("diff = %+v", d)
	}
	if _, err := changes.DiffProjectLinks("p", runs, 3, 5); err == nil {
		t.Error("a pass the journal no longer keeps is compared")
	}
}
