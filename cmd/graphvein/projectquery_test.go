package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

const projectImpactFixtureDir = "../../testdata/fixtures/projectimpact"

const usersManifest = "version: 1\nname: users\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n"

// Identities of the projectimpact fixture.
const (
	usersRoute   = "route:src/server.ts#GET%20%2Ft022b7%2Fusers"
	usersHandler = "function:src/server.ts#listUsers"
)

// copyProjectImpactFixture copies the projectimpact fixture, restores the
// inert OpenAPI name (fixture README) and writes the manifest.
func copyProjectImpactFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(projectImpactFixtureDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "frontend", "openapi.yaml.fixture"), filepath.Join(root, "frontend", "openapi.yaml")); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, root, usersManifest)
	return root
}

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestProjectQueryGoldens is T022 B7's CLI gate on the projectimpact
// fixture, on both backends: callers of the backend's route list the
// frontend's call sites across the project link (the reverse question
// "what calls this backend route across repositories"), impact of the
// backend handler reaches the frontend UI function, an ambiguous term
// names both repositories, and single-repository callers are unchanged
// (no project link followed). A frontend index after the pass leaves the
// links stale: followed and marked. changes --project lists the link
// journal diff. JSON outputs are golden.
func TestProjectQueryGoldens(t *testing.T) {
	projectBackends(t, func(t *testing.T, _ func() store.Store) {
		root := copyProjectImpactFixture(t)
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		outputs := map[string]query.ProjectResponse{}
		jsonQuery := func(name string, want int, args ...string) {
			t.Helper()
			code, out, errOut := runCLI(append(args, "--project", root, "--json")...)
			if code != want || errOut != "" {
				t.Fatalf("%s = %d\n%s\n%s", name, code, out, errOut)
			}
			var r query.ProjectResponse
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatal(err, out)
			}
			outputs[name] = r
		}
		jsonQuery("1-callers-of-backend-route", 0, "callers", usersRoute, "--repository", "backend")
		jsonQuery("2-impact-of-backend-handler", 0, "impact", usersHandler, "--repository", "backend", "--depth", "4")
		jsonQuery("3-ambiguous-term", 1, "callers", "listUsers")

		callers := outputs["1-callers-of-backend-route"]
		if len(callers.Results) != 2 || callers.Results[0].Node.Repository != "frontend" || !callers.Results[0].Path[0].ProjectLink || callers.Results[0].Assumptions[0] != "matched across project repositories; base URL not proven" {
			t.Errorf("callers of the backend route = %+v", callers.Results)
		}
		reached := false
		for _, r := range outputs["2-impact-of-backend-handler"].Results {
			reached = reached || r.Node.Repository == "frontend" && r.Node.Identity == "function:src/page.ts#UsersPage" && r.Provenance == "inferred"
		}
		if !reached {
			t.Error("impact of the backend handler does not reach the frontend UI function")
		}

		// The text form shows the link, its assumption and repositories.
		code, out, errOut := runCLI("callers", usersRoute, "--project", root, "--repository", "backend")
		if code != 0 || !strings.Contains(out, "Link pass 1: fresh, 3 links") || !strings.Contains(out, "calls_route (project link) frontend client_call:src/api.ts#listUsers.") || !strings.Contains(out, "    assumption: matched across project repositories; base URL not proven") {
			t.Errorf("callers text = %d\n%s\n%s", code, out, errOut)
		}
		// Single-repository callers in the backend's checkout are unchanged:
		// the store has no in-repository caller of the route.
		code, out, errOut = runCLI("callers", usersRoute, filepath.Join(root, "backend"), "--repository", "backend", "--json")
		var single query.Response
		if code != 0 || json.Unmarshal([]byte(out), &single) != nil || len(single.Results) != 0 {
			t.Errorf("single-repository callers = %d %s %s", code, out, errOut)
		}

		// A frontend index without a link pass: links stale, still followed.
		if code, out, errOut := runCLI("index", filepath.Join(root, "frontend"), "--repository", "frontend"); code != 0 {
			t.Fatalf("index frontend = %d %s %s", code, out, errOut)
		}
		jsonQuery("4-callers-stale-links", 0, "callers", usersRoute, "--repository", "backend")
		stale := outputs["4-callers-stale-links"]
		if len(stale.Results) != 2 || !stale.Results[0].Stale || stale.Results[0].Path[0].StaleReasons[0] != "frontend indexed again since the pass" {
			t.Errorf("stale links not marked: %+v", stale.Results)
		}
		code, out, _ = runCLI("callers", usersRoute, "--project", root, "--repository", "backend")
		if code != 0 || !strings.Contains(out, "Link pass 1: stale (frontend indexed again)") || !strings.Contains(out, "    stale: frontend indexed again since the pass") {
			t.Errorf("stale callers text:\n%s", out)
		}
		golden.CheckRoot(t, filepath.Join(projectImpactFixtureDir, "..", "projectimpact.cli-query.golden.json"), outputs, root)

		// changes --project: the link journal diff of an unchanged second
		// pass is empty; members report their own counts.
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("second index --project = %d %s %s", code, out, errOut)
		}
		code, out, errOut = runCLI("changes", "--project", root)
		if code != 0 || !strings.Contains(out, "Links between pass 1 and pass 2: 0 added, 0 removed, 0 changed") || !strings.Contains(out, "frontend: run ") {
			t.Errorf("changes --project = %d\n%s\n%s", code, out, errOut)
		}
		// A base that is the newest pass compares nothing: the text and the
		// JSON say same pass, never "0 added, 0 removed, 0 changed" (T044
		// review), as the API's state same_pass does.
		code, out, errOut = runCLI("changes", "--project", root, "--base", "pass:2")
		if code != 0 || !strings.Contains(out, "Links: the base is pass 2, the newest kept link pass: same pass, nothing compared") || strings.Contains(out, "0 added") {
			t.Errorf("changes --project --base pass:2 = %d\n%s\n%s", code, out, errOut)
		}
		var samePass struct {
			State     string          `json:"state"`
			Links     json.RawMessage `json:"links"`
			LinksNote string          `json:"links_note"`
		}
		code, out, errOut = runCLI("changes", "--project", root, "--base", "pass:2", "--json")
		if code != 0 || json.Unmarshal([]byte(out), &samePass) != nil || samePass.State != "same_pass" || string(samePass.Links) != "null" || !strings.Contains(samePass.LinksNote, "nothing compared") {
			t.Errorf("changes --project --base pass:2 --json = %d %+v\n%s\n%s", code, samePass, out, errOut)
		}
		code, out, errOut = runCLI("changes", "--project", root, "--base", "pass:1", "--json")
		if code != 0 || json.Unmarshal([]byte(out), &samePass) != nil || samePass.State != "compared" || string(samePass.Links) == "null" {
			t.Errorf("changes --project --base pass:1 --json = %d %+v\n%s\n%s", code, samePass, out, errOut)
		}
		// changes impact at project scope needs the member when there are
		// several, and refuses ref bases.
		if code, _, errOut := runCLI("changes", "impact", usersHandler, "--project", root); code != 2 || !strings.Contains(errOut, "--repository") {
			t.Errorf("changes impact without --repository = %d %s", code, errOut)
		}
		if code, _, errOut := runCLI("changes", "impact", usersHandler, "--project", root, "--repository", "backend", "--base", "ref:main"); code != 2 || !strings.Contains(errOut, changes.ErrProjectRef.Error()) {
			t.Errorf("changes impact with a ref base = %d %s", code, errOut)
		}
		// Usage: --project takes no path.
		if code, _, _ := runCLI("callers", usersRoute, root, "--project", root); code != 2 {
			t.Errorf("callers with a path and --project = %d", code)
		}
	})
}
