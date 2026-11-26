package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const projectThreeFixtureDir = "../../testdata/fixtures/projectthree"

// Identities of the projectthree fixture.
const (
	threeUserRoute   = "route:src/server.ts#GET%20%2Ft022r%2Fuser"
	threeUserHandler = "function:src/server.ts#currentUser"
	threeHealthRoute = "route:src/server.ts#GET%20%2Ft022r%2Fhealth"
	threeLoadUser    = "client_call:src/api.ts#loadUser.GET%20%60%24%7Bimport.meta.env.VITE_API_URL%7D%2Ft022r%2Fuser%60"
)

const threeRepoManifest = "version: 1\nname: three\nmembers:\n  - repository: web\n    path: web\n  - repository: express\n    path: express\n  - repository: axum\n    path: axum\n"

// TestProjectThreePossibleCallers is the T022 review gate on a frontend
// with two backends serving the same paths, on both backends: the link
// pass leaves the frontend's user call ambiguous (PL-R11), so callers and
// impact of the express route list it as a possible caller ("may be called
// by ... ambiguous across repositories"), counted in the blind-spot line
// and never as a result; change impact lists it as a "may depend on"
// possible dependent, not a path. A fourth member without a stored index
// makes the calls incomplete (PL-R7): they are listed the same way with
// the outcome's reason. JSON outputs are golden.
func TestProjectThreePossibleCallers(t *testing.T) {
	projectBackends(t, func(t *testing.T, _ func() store.Store) {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(projectThreeFixtureDir)); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, root, threeRepoManifest)
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		outputs := map[string]any{}
		jsonOut := func(name string, args ...string) map[string]any {
			t.Helper()
			code, out, errOut := runCLI(append(args, "--json")...)
			if code != 0 || errOut != "" {
				t.Fatalf("%s = %d\n%s\n%s", name, code, out, errOut)
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatal(err, out)
			}
			outputs[name] = v
			return v
		}
		callers := jsonOut("1-callers-of-express-user", "callers", threeUserRoute, "--project", root, "--repository", "express")
		if results, _ := callers["results"].([]any); len(results) != 0 {
			t.Errorf("an ambiguous call became a caller: %v", results)
		}
		possible, _ := callers["possible_callers"].([]any)
		if len(possible) != 1 || !strings.Contains(fmtJSON(possible[0]), threeLoadUser) || !strings.Contains(fmtJSON(possible[0]), `"status":"ambiguous"`) {
			t.Errorf("possible callers = %v", possible)
		}
		jsonOut("2-impact-of-express-handler", "impact", threeUserHandler, "--project", root, "--repository", "express", "--depth", "4")
		jsonOut("3-callers-of-axum-user", "callers", "route:src/main.rs#GET%20%2Ft022r%2Fuser", "--project", root, "--repository", "axum")
		health := jsonOut("4-callers-of-express-health", "callers", threeHealthRoute, "--project", root, "--repository", "express")
		if p, _ := health["possible_callers"].([]any); len(p) != 0 {
			t.Errorf("a route no call matches has possible callers: %v", p)
		}

		code, out, _ := runCLI("callers", threeUserRoute, "--project", root, "--repository", "express")
		if code != 0 || !strings.Contains(out, "Blind spot: 1 possible callers in other repositories (ambiguous or incomplete, not linked)") ||
			!strings.Contains(out, "may be called by web "+threeLoadUser+" (ambiguous across repositories; 2 candidates in axum, express)") {
			t.Errorf("callers text:\n%s", out)
		}

		// Change impact: the edited handler's route has the ambiguous call
		// as a possible dependent, never a path.
		server := filepath.Join(root, "express", "src", "server.ts")
		data, err := os.ReadFile(server)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(server, []byte(strings.Replace(string(data), "user: null", "user: 1", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("second index --project = %d\n%s\n%s", code, out, errOut)
		}
		code, out, errOut := runCLI("changes", "impact", threeUserHandler, "--project", root, "--repository", "express", "--base", "previous", "--json")
		var change struct {
			Impacts []struct {
				Impact map[string]any `json:"impact"`
			} `json:"impacts"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &change) != nil || len(change.Impacts) != 1 {
			t.Fatalf("changes impact = %d\n%s\n%s", code, out, errOut)
		}
		outputs["5-change-impact-of-express-handler"] = change.Impacts[0].Impact
		if strings.Contains(fmtJSON(change.Impacts[0].Impact["paths"]), "client_call") {
			t.Error("an ambiguous call became an impact path")
		}
		code, out, _ = runCLI("changes", "impact", threeUserHandler, "--project", root, "--repository", "express", "--base", "previous")
		if code != 0 || !strings.Contains(out, "2 I12 web "+threeLoadUser+" may depend on: calls this route from another project repository, inferred (not a link: ambiguous across repositories; 2 candidates in axum, express") ||
			!strings.Contains(out, "1 possible dependents in other repositories") {
			t.Errorf("changes impact text:\n%s", out)
		}

		// A fourth member without a stored index: every call is incomplete.
		writeManifest(t, root, threeRepoManifest+"  - repository: ghost\n    path: ghost\n")
		if code, out, errOut := runCLI("index", "--project", root); code == 0 && strings.Contains(errOut, "panic") {
			t.Fatalf("index --project with a ghost member = %d\n%s\n%s", code, out, errOut)
		}
		incomplete := jsonOut("6-callers-incomplete", "callers", threeUserRoute, "--project", root, "--repository", "express")
		p, _ := incomplete["possible_callers"].([]any)
		if len(p) != 1 || !strings.Contains(fmtJSON(p[0]), `"status":"incomplete"`) || !strings.Contains(fmtJSON(p[0]), "no stored index: ghost") {
			t.Errorf("incomplete possible callers = %v", p)
		}
		golden.CheckRoot(t, filepath.Join(projectThreeFixtureDir, "..", "projectthree.cli-query.golden.json"), outputs, root)
	})
}

func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
