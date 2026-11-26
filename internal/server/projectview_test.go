package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// TestProjectMemberWithoutCheckout is T022 B9's audit of the handlers that
// need a member's files: a member stored in the project store but without
// a checkout here answers every endpoint from the store, with no file
// state and no editor link, its freshness unknown ("no checkout here");
// only code peek, a ref base and a ref job answer 409 no_checkout.
func TestProjectMemberWithoutCheckout(t *testing.T) {
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
		// The frontend's checkout is not here (the facts come from the
		// shared store); the server's working directory has a
		// graphvein.yaml that must never be read in its place.
		project.Members[0].Path, project.Members[0].PathSource, project.Members[0].Checkout = "", "", false
		opt := projectOptions(project, s)
		// The architecture views read the built-in defaults for it, as the
		// MCP callers and impact scope does (T043 review): this
		// graphvein.yaml in the working directory does not parse, so a
		// view that read it would fail.
		cwd := t.TempDir()
		if err := os.WriteFile(filepath.Join(cwd, config.File), []byte("version: 1\ntests: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(cwd)
		for _, tc := range []struct {
			method, path, code string
			status             int
		}{
			{http.MethodGet, "/api/v0/source?repository=frontend&id=x", "no_checkout", 409},
			{http.MethodGet, "/api/v0/changes/summary?repository=frontend&base=ref:main", "no_checkout", 409},
			{http.MethodGet, "/api/v0/status?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/ide?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/ui?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/precision?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/api/endpoints?repository=frontend&transport=all", "", 200},
			{http.MethodGet, "/api/v0/schema/summary?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/changes/bases?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/changes/summary?repository=frontend&base=previous", "base_unknown", 404},
			{http.MethodGet, "/api/v0/changes/refs?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/architecture?repository=frontend", "", 200},
			{http.MethodGet, "/api/v0/architecture/component?repository=frontend&id=.", "", 200},
			{http.MethodGet, "/api/v0/architecture/chain?repository=frontend&id=" + url.QueryEscape("function:src/api.ts#listUsers"), "", 200},
		} {
			rec := get(t, opt, tc.method, tc.path, nil)
			if rec.Code != tc.status || tc.code != "" && bodyCode(t, rec.Body.Bytes()) != tc.code {
				t.Errorf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
			}
		}
		req := httptestPost(t, opt, "/api/v0/changes/refs?repository=frontend", `{"base":"main"}`)
		if req.Code != http.StatusConflict || bodyCode(t, req.Body.Bytes()) != "no_checkout" {
			t.Errorf("POST refs: %d %s", req.Code, req.Body.String())
		}
		var status statusResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/status?repository=frontend", nil), &status)
		if status.Freshness.State != stateUnknown || !strings.Contains(status.Freshness.Error, "no checkout here") || !status.Index.Indexed {
			t.Errorf("status without a checkout: %+v", status)
		}
		// Calls are presented from the store: no file state, no editor
		// link, and the ide status says why.
		var calls callsResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/calls?repository=frontend&transport=http&status=linked,unmatched&scope=all", nil), &calls)
		if len(calls.Calls) == 0 || calls.IDE.Available || calls.IDE.Error == nil || !strings.Contains(*calls.IDE.Error, "no checkout here") {
			t.Fatalf("calls without a checkout: %+v", calls)
		}
		for _, c := range calls.Calls {
			if c.Node.FileState != nil || c.Node.IDE != nil {
				t.Errorf("call presented with file state or editor link: %+v", c.Node)
			}
			if c.Project == nil || len(c.Project.Outcomes) == 0 {
				t.Errorf("call without project outcomes: %+v", c)
			}
		}
		// The backend route's project callers come from the frontend's
		// stored nodes, without a file state.
		var detail endpointResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+url.QueryEscape(usersRoute)+"&repository=backend", nil), &detail)
		if detail.Project == nil || len(detail.Project.Callers) != 1 || detail.Project.Callers[0].Node == nil || detail.Project.Callers[0].Node.FileState != nil {
			t.Errorf("project callers from a member without a checkout: %+v", detail.Project)
		}
	})
}

// TestProjectViewMonorepo is T022 B9's server gate for the Project view on
// the projectlink fixture's monorepo scenario (link scopes with
// declarations): the declarations, the pass's ambiguous sources grouped
// by row and candidate providers, and the one stored in-repository link a
// declaration contradicts ("conflicts with declaration": web's
// userFortyTwoFromOrders reaches the users server while its base's entry
// names monorepo/orders).
func TestProjectViewMonorepo(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/projectlink")); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{"frontend", "monorepo/admin"} {
			if err := os.Rename(filepath.Join(root, dir, "openapi.yaml.fixture"), filepath.Join(root, dir, "openapi.yaml")); err != nil {
				t.Fatal(err)
			}
		}
		manifest, err := os.ReadFile(filepath.Join(root, "scenarios", "monorepo.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, config.ProjectFile), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{"frontend", "monorepo"} {
			indexRepository(t, filepath.Join(root, m), w, m)
		}
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		var doc projectResponse
		rec := get(t, opt, http.MethodGet, "/api/v0/project", nil)
		decodeInto(t, rec, &doc)
		if len(doc.Conflicts) != 1 || doc.Conflicts[0].Consumer != "monorepo/web" || doc.Conflicts[0].TargetScope != "users" || doc.Conflicts[0].Call.Name == "" {
			t.Errorf("conflicts: %+v", doc.Conflicts)
		}
		if doc.Ambiguous.Total == 0 || len(doc.Declarations.Consumes) != 6 || len(doc.Declarations.Scopes) != 5 {
			t.Errorf("ambiguous %+v, declarations %+v", doc.Ambiguous, doc.Declarations)
		}
		// T022 review: the conflicting call is marked where it is
		// listed: on its target route's endpoint and in the calls list.
		if len(doc.Conflicts) == 1 {
			c := doc.Conflicts[0]
			var detail endpointResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+url.QueryEscape(c.Target.Identity)+"&repository=monorepo", nil), &detail)
			if detail.Project == nil || len(detail.Project.DeclarationConflicts) != 1 || detail.Project.DeclarationConflicts[0].Call != c.Call.Identity || detail.Project.DeclarationConflicts[0].TargetScope != "users" {
				t.Errorf("endpoint declaration conflicts: %+v", detail.Project)
			}
			var calls callsResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/calls?transport=http&status=linked&scope=all&repository=monorepo", nil), &calls)
			marked := 0
			for _, e := range calls.Calls {
				if e.Project != nil && e.Project.DeclarationConflict != nil {
					marked++
					if e.Node.Identity != c.Call.Identity || e.Project.DeclarationConflict.Consumer != "monorepo/web" {
						t.Errorf("marked call: %+v", e.Project.DeclarationConflict)
					}
				}
			}
			if marked != 1 {
				t.Errorf("%d calls marked as conflicting, want 1", marked)
			}
		}
		body := map[string]any{"project": normalizeProject(t, rec.Body.Bytes(), runLabels(t, s, "frontend", "monorepo"))}
		golden.CheckRoot(t, filepath.Join("testdata", "project-monorepo.golden.json"), body, root)
	})
}

// httptestPost sends a JSON POST through a handler built from opt.
func httptestPost(t *testing.T, opt Options, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = testHost
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h, runs := newHandler(opt, testHosts)
	t.Cleanup(runs.stop)
	h.ServeHTTP(rec, req)
	return rec
}
