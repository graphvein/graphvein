package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// The projectimpact fixture (T022 B7) served as a project (T022 B8).
const (
	projectFixture = "../../testdata/fixtures/projectimpact"
	usersManifest  = "version: 1\nname: users\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n"
	usersRoute     = "route:src/server.ts#GET%20%2Ft022b7%2Fusers"
	usersHandler   = "function:src/server.ts#listUsers"
	defaultAssume  = "matched across project repositories; base URL not proven"
)

// copyProjectFixture copies the fixture, restores the inert OpenAPI name
// (fixture README) and writes the manifest.
func copyProjectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(projectFixture)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "frontend", "openapi.yaml.fixture"), filepath.Join(root, "frontend", "openapi.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(usersManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func projectOptions(project config.Project, s store.Store) Options {
	opt := testOptions("", s)
	opt.Root, opt.Repository = "", ""
	opt.Project = &ProjectOptions{Project: project}
	return opt
}

// runLabels replaces every stored run ID of the members with a stable
// label (repository and position by finish time), and every finish time,
// so project bodies can be golden.
func runLabels(t *testing.T, s store.Store, repos ...string) func(string) string {
	t.Helper()
	pairs := []string{}
	for _, repo := range repos {
		runs, err := s.Runs(context.Background(), repo)
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].FinishedAt < runs[j].FinishedAt })
		for i, r := range runs {
			pairs = append(pairs, r.ID, fmt.Sprintf("<run %s#%d>", repo, i+1))
		}
	}
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace
}

// normalizeProject decodes a body, labels its run IDs and replaces every
// finish time.
func normalizeProject(t *testing.T, body []byte, label func(string) string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(label(string(body))), &v); err != nil {
		t.Fatal(err, string(body))
	}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "finished_at" {
					if _, ok := val.(string); ok {
						x[k] = finishedAtPlaceholder
						continue
					}
				}
				walk(val)
			}
		case []any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(v)
	return v
}

// TestProjectServerGoldens is T022 B8's gate: graphvein serve --project on
// the projectimpact fixture, on both backends. The project endpoint lists
// members, ID sources, freshness and the link pass; the backend route's
// endpoint lists the frontend's calls as cross-repository callers with the
// default assumption and inferred provenance; project callers, impact,
// change impact and the link journal diff answer as the CLI does; a
// frontend index after the pass marks links stale everywhere. Every
// single-repository endpoint answers per member, and is ambiguous (400)
// without repository.
func TestProjectServerGoldens(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		frontend, backendDir := filepath.Join(root, "frontend"), filepath.Join(root, "backend")
		pass := func() {
			t.Helper()
			if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
				t.Fatal(err)
			}
		}
		indexRepository(t, frontend, w, "frontend")
		indexRepository(t, backendDir, w, "backend")
		pass()
		opt := projectOptions(project, s)
		got := map[string]string{}
		goldens := map[string]any{}
		check := func(name, path string, want int) any {
			t.Helper()
			rec := get(t, opt, http.MethodGet, path, nil)
			if rec.Code != want {
				t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
			}
			v := normalizeProject(t, rec.Body.Bytes(), runLabels(t, s, "frontend", "backend"))
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[name] = golden.ReplaceRoot(string(data), filepath.ToSlash(root))
			goldens[name] = v
			return v
		}
		route := url.QueryEscape(usersRoute)
		check("1-project", "/api/v0/project", 200)
		check("2-endpoints-backend", "/api/v0/api/endpoints?transport=http&repository=backend", 200)
		check("2a-endpoints-frontend", "/api/v0/api/endpoints?transport=http&scope=all&repository=frontend", 200)
		check("3-endpoint-backend-route", "/api/v0/api/endpoint?id="+route+"&repository=backend", 200)
		check("4-project-callers", "/api/v0/project/callers?term="+route+"&repository=backend", 200)
		check("5-project-impact", "/api/v0/project/impact?term="+url.QueryEscape(usersHandler)+"&repository=backend&depth=4", 200)
		check("6-project-callers-ambiguous", "/api/v0/project/callers?term=listUsers", 200)
		// T022 B9: the flow of the route and the frontend's calls carry the
		// project links too.
		check("6a-flow-backend-route", "/api/v0/api/flow?id="+route+"&repository=backend", 200)
		check("6b-calls-frontend", "/api/v0/api/calls?transport=http&status=linked,unmatched,ambiguous,dynamic,wrapper,test_only&scope=all&repository=frontend", 200)
		// T022 MR-UI: the same calls matched by their project-scope status.
		check("6c-calls-frontend-project-scope", "/api/v0/api/calls?transport=http&status_scope=project&status=linked_across,ambiguous_across,unmatched&scope=all&repository=frontend", 200)
		for _, bad := range []string{"status_scope=nope&transport=http", "status_scope=project&transport=tauri_ipc"} {
			if rec := get(t, opt, http.MethodGet, "/api/v0/api/calls?status=unmatched&repository=frontend&"+bad, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status %d", bad, rec.Code)
			}
		}

		// The endpoint's cross-repository callers carry the assumption,
		// inferred provenance and the frontend's own presentation.
		var detail endpointResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+route+"&repository=backend", nil), &detail)
		if detail.Project == nil || len(detail.Project.Callers) != 1 || len(detail.Project.Operations) != 1 {
			t.Fatalf("project callers of the backend route: %+v", detail.Project)
		}
		for _, c := range append(detail.Project.Callers, detail.Project.Operations...) {
			if c.Repository != "frontend" || c.Assumption != defaultAssume || c.Provenance != "inferred" || c.Stale || c.Node == nil {
				t.Errorf("project link: %+v", c)
			}
		}
		if len(detail.Callers) != 0 {
			t.Errorf("in-repository callers of the backend route: %+v", detail.Callers)
		}
		// A project caller's editor link follows the same precedence as
		// every other link and says so (ide_status): the browser's editor=
		// first, then the member's graphvein.yaml, then the built-in default.
		linkOf := func(query string) (string, *ideStatus) {
			t.Helper()
			var d endpointResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+route+"&repository=backend"+query, nil), &d)
			if d.Project == nil || len(d.Project.Callers) != 1 || d.Project.Callers[0].Node == nil || d.Project.Callers[0].Node.IDE == nil {
				t.Fatalf("project caller%s: %+v", query, d.Project)
			}
			c := d.Project.Callers[0]
			return c.Node.IDE.Link, c.IDEStatus
		}
		if link, st := linkOf(""); !strings.HasPrefix(link, "vscode://") || st == nil || st.Source != ideSourceBuiltin {
			t.Errorf("built-in default: %s %+v", link, st)
		}
		if link, st := linkOf("&editor=zed"); !strings.HasPrefix(link, "zed://") || st == nil || st.Source != ideSourceRequest || st.Editor == nil || *st.Editor != "zed" {
			t.Errorf("browser choice: %s %+v", link, st)
		}
		memberYAML := filepath.Join(frontend, "graphvein.yaml")
		if _, err := os.Stat(memberYAML); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the frontend fixture has a graphvein.yaml already: %v", err)
		}
		if err := os.WriteFile(memberYAML, []byte("version: 1\nrepository: frontend\nide:\n  editor: cursor\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if link, st := linkOf(""); !strings.HasPrefix(link, "cursor://") || st == nil || st.Source != ideSourceRepository || st.Editor == nil || *st.Editor != "cursor" {
			t.Errorf("member repository choice: %s %+v", link, st)
		}
		if link, st := linkOf("&editor=zed"); !strings.HasPrefix(link, "zed://") || st == nil || st.Source != ideSourceRequest {
			t.Errorf("browser choice over the member's: %s %+v", link, st)
		}
		if err := os.Remove(memberYAML); err != nil {
			t.Fatal(err)
		}
		// T022 B11: the links are never labelled precise, and each member's
		// /api/v0/precision counts the links with an end in it.
		if l := detail.Project.LinksPrecision; l.Label != "best_effort" || l.Row != "D" {
			t.Errorf("links_precision = %+v", l)
		}
		stored, err := s.ProjectLinks(ctx, project.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{"frontend", "backend"} {
			want := 0
			for _, l := range stored.Links {
				if l.Source.Repository == member || l.Target.Repository == member {
					want++
				}
			}
			var doc precisionResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/precision?repository="+member, nil), &doc)
			found := false
			for _, f := range doc.Default.Families {
				if f.Family != precision.FamilyCrossRepositoryLinks {
					continue
				}
				found = true
				if f.Label != precision.BestEffort || f.Facts == nil || f.Facts.Total != want || f.Facts.Unit != "links" || want == 0 {
					t.Errorf("%s cross_repository_links = %+v (facts %+v), want %d links", member, f, f.Facts, want)
				}
			}
			if !found {
				t.Errorf("%s: /api/v0/precision does not list cross_repository_links", member)
			}
		}

		// Revision 2 of the backend: the list query changes. Change
		// impact at project scope reaches the frontend through I12.
		data, err := os.ReadFile(filepath.Join(backendDir, "src", "server.ts"))
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(string(data), "db.select().from(users)", "db.select().from(users).limit(10)", 1)
		if edited == string(data) {
			t.Fatal("revision 2 edit did not apply")
		}
		if err := os.WriteFile(filepath.Join(backendDir, "src", "server.ts"), []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, backendDir, w, "backend")
		pass()
		impact := check("7-project-change-impact", "/api/v0/project/changes/impact?repository=backend&base=previous&depth=4&id="+url.QueryEscape(usersHandler), 200)
		if !strings.Contains(fmt.Sprint(impact), "I12") {
			t.Error("project change impact does not cross the project link (I12)")
		}
		single := get(t, opt, http.MethodGet, "/api/v0/changes/impact?repository=backend&base=previous&depth=4&id="+url.QueryEscape(usersHandler), nil)
		if single.Code != 200 || strings.Contains(single.Body.String(), "I12") {
			t.Errorf("single-repository change impact in project mode: %d %s", single.Code, single.Body.String())
		}
		check("8-link-changes", "/api/v0/project/links/changes", 200)
		// A base that is the newest pass compares nothing: same_pass, links
		// null, never an empty diff that reads as "no change".
		same, _ := check("8a-link-changes-same-pass", "/api/v0/project/links/changes?base=pass:2", 200).(map[string]any)
		if same["state"] != "same_pass" || same["links"] != nil || same["links_note"] == nil {
			t.Errorf("base = newest pass: %v", same)
		}

		// The frontend indexed again without a pass: links stale.
		indexRepository(t, frontend, w, "frontend")
		check("9-project-stale", "/api/v0/project", 200)
		check("10-endpoint-stale", "/api/v0/api/endpoint?id="+route+"&repository=backend", 200)
		golden.CheckRoot(t, filepath.Join("testdata", "project-users.golden.json"), goldens, root)

		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		for name, body := range bodies["sqlite"] {
			if bodies["postgres"][name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// TestProjectServerRequests checks the repository parameter rules, a
// member without a checkout, the method rule of the new paths and the
// project re-index job.
func TestProjectServerRequests(t *testing.T) {
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
		// A third member without a checkout (not in the manifest on disk:
		// the declarations differ, so links are stale).
		project.Members = append(project.Members, config.Member{Repository: "ghost", RepositorySource: config.RepositoryFromManifest})
		opt := projectOptions(project, s)
		for _, tc := range []struct {
			path, code string
			status     int
		}{
			{"/api/v0/status", "repository_required", 400},
			{"/api/v0/api/endpoints", "repository_required", 400},
			{"/api/v0/schema/summary?editor=zed", "repository_required", 400},
			{"/api/v0/changes/summary?base=previous", "repository_required", 400},
			{"/api/v0/status?repository=nobody", "bad_request", 400},
			{"/api/v0/status?repository=backend&repository=frontend", "bad_request", 400},
			{"/api/v0/source?repository=ghost&file=src/a.ts", "no_checkout", 409},
			{"/api/v0/project/callers", "bad_request", 400},
			{"/api/v0/project/callers?term=listUsers&repository=nobody", "bad_request", 400},
			{"/api/v0/project/impact?term=listUsers&depth=11", "bad_request", 400},
			{"/api/v0/project/changes/impact?id=x", "repository_required", 400},
			{"/api/v0/project/changes/impact?repository=backend&base=ref:main&id=x", "bad_request", 400},
			{"/api/v0/project/links/changes?base=run:x", "bad_request", 400},
		} {
			rec := get(t, opt, http.MethodGet, tc.path, nil)
			if rec.Code != tc.status || bodyCode(t, rec.Body.Bytes()) != tc.code {
				t.Errorf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
			}
		}
		if rec := get(t, opt, http.MethodGet, "/api/v0/status", nil); !strings.Contains(rec.Body.String(), "members frontend, backend, ghost; name one with repository=") {
			t.Errorf("ambiguous message: %s", rec.Body.String())
		}
		// Members answer as single-repository servers do; schema
		// endpoints keep their editor parameter.
		for _, path := range []string{"/api/v0/status?repository=backend", "/api/v0/schema/summary?repository=backend&editor=zed", "/api/v0/ide?repository=frontend"} {
			if rec := get(t, opt, http.MethodGet, path, nil); rec.Code != 200 {
				t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
			}
		}
		var status statusResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/status?repository=backend", nil), &status)
		if status.Repository != "backend" || status.Freshness.State != stateFresh {
			t.Errorf("member status: %+v", status)
		}
		// The ghost member is listed without a checkout; every link is
		// stale because the declarations differ from the pass's.
		var doc projectResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/project", nil), &doc)
		if len(doc.Members) != 3 || doc.Members[2].Checkout || doc.Members[2].Freshness.State != stateUnknown || doc.Members[2].Index != nil || doc.LinkPass.State != stateStale || doc.LinkPass.StaleLinks != doc.LinkPass.Links {
			t.Errorf("project with a ghost member: %+v", doc)
		}
		// The new paths are GET only: no write endpoint in this step.
		for _, path := range []string{projectPath, projectCallersPath, projectImpactPath, projectChangesImpactPath, projectLinkChangesPath} {
			if rec := get(t, opt, http.MethodPost, path, nil); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("POST %s: %d", path, rec.Code)
			}
		}
	})
}

// TestProjectReindex checks POST /api/v0/index/run under --project: the
// job runs the project re-index, reports the member being indexed and the
// link pass, refuses a repository parameter and fails (with each
// member's outcome) when a member fails.
func TestProjectReindex(t *testing.T) {
	project := config.Project{Name: "users", Dir: t.TempDir(), Members: []config.Member{
		{Repository: "frontend", Checkout: true, Path: t.TempDir()},
		{Repository: "backend", Checkout: true, Path: t.TempDir()},
		{Repository: "ghost"},
	}}
	backends(t, func(t *testing.T, w, s store.Store) {
		release := make(chan struct{})
		var fail error
		reindex := func(ctx context.Context, _ config.Project, _ bool, progress func(string, indexrun.Progress)) ProjectRun {
			progress("frontend", indexrun.Progress{Phase: "scanning"})
			<-release
			progress("", indexrun.Progress{})
			run := &store.Run{ID: "r1", FinishedAt: "2026-11-03T14:41:00Z"}
			return ProjectRun{Members: []ProjectMemberRun{
				{Repository: "frontend", Indexed: true, Run: run},
				{Repository: "backend", Indexed: true, Run: run, Error: fail},
				{Repository: "ghost"},
			}, Pass: &store.ProjectLinkRun{Seq: 2, Added: 1}}
		}
		opt := projectOptions(project, s)
		opt.Locker = freeLocker{}
		opt.Project.Reindex = reindex
		h, runs := newHandler(opt, testHosts)
		defer runs.stop()
		post := func(query string) (int, []byte) {
			req, _ := http.NewRequest(http.MethodPost, "http://"+testHost+indexRunPath+query, strings.NewReader("{}"))
			req.Host = testHost
			req.Header.Set("Content-Type", "application/json")
			rec := &recorder{header: http.Header{}}
			h.ServeHTTP(rec, req)
			return rec.status, rec.body.Bytes()
		}
		document := func() indexRunResponse {
			req, _ := http.NewRequest(http.MethodGet, "http://"+testHost+indexRunPath, nil)
			req.Host = testHost
			rec := &recorder{header: http.Header{}}
			h.ServeHTTP(rec, req)
			var doc indexRunResponse
			if err := json.Unmarshal(rec.body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			return doc
		}
		wait := func() indexRunResponse {
			for i := 0; i < 200; i++ {
				if doc := document(); doc.Job != nil && doc.Job.EndedAt != nil {
					return doc
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("job did not end")
			return indexRunResponse{}
		}
		if status, body := post("?repository=backend"); status != 400 || bodyCode(t, body) != "bad_request" {
			t.Errorf("POST with repository: %d %s", status, body)
		}
		if status, body := post(""); status != 202 {
			t.Fatalf("POST: %d %s", status, body)
		}
		for i := 0; i < 200; i++ {
			if doc := document(); doc.Job.Member != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		doc := document()
		if doc.Project != "users" || doc.Repository != "" || doc.Lock.State != lockHeldHere || *doc.Job.Member != "frontend" || doc.Job.Project.Members[0].State != memberRunning || doc.Job.Project.Members[2].State != memberNoCheckout {
			t.Errorf("running project job: %+v %+v", doc, doc.Job)
		}
		if status, body := post(""); status != 409 {
			t.Errorf("second POST while running: %d %s", status, body)
		}
		close(release)
		doc = wait()
		if doc.Job.State != jobCommitted || doc.Job.Project.LinkPass == nil || doc.Job.Project.LinkPass.Pass != 2 || doc.Job.Project.Members[1].State != memberCommitted || doc.Job.Project.Members[1].Run.ID != "r1" || doc.Job.Member != nil {
			t.Errorf("committed project job: %+v %+v", doc.Job, doc.Job.Project)
		}
		// A member failing: the job fails and says which.
		release = make(chan struct{})
		close(release)
		fail = errors.New("parse exploded")
		if status, body := post(""); status != 202 {
			t.Fatalf("POST: %d %s", status, body)
		}
		doc = wait()
		if doc.Job.State != jobFailed || doc.Job.Error.Code != "index_failed" || !strings.Contains(doc.Job.Error.Message, "backend: parse exploded") || doc.Job.Project.Members[1].State != memberFailed {
			t.Errorf("failed project job: %+v %+v", doc.Job, doc.Job.Error)
		}
	})
}

// freeLocker is a project lock that is always free; the project re-index
// takes its locks inside Reindex.
type freeLocker struct{}

func (freeLocker) Acquire(context.Context, time.Duration) (store.IndexLease, error) {
	return nil, errors.New("not used")
}
func (freeLocker) State(context.Context) (store.LockState, error) { return store.LockFree, nil }
func (freeLocker) Close() error                                   { return nil }

// TestSameRemoteCheck covers the remote URL check: separate repository
// roots with one remote are reported; folders of one repository are not.
func TestSameRemoteCheck(t *testing.T) {
	remotes := map[string]gitref.Remote{
		"a":    {URL: "https://GitHub.com/o/r.git", TopLevel: "/x/a"},
		"b":    {URL: "git@github.com:o/r", TopLevel: "/x/b"},
		"web":  {URL: "https://example.com/mono", TopLevel: "/m"},
		"api":  {URL: "https://example.com/mono/", TopLevel: "/m"},
		"solo": {URL: "ssh://git@other.example:2222/x/y.git", TopLevel: "/s"},
	}
	got := sameRemoteChecks([]string{"a", "b", "web", "api", "solo", "none"}, remotes)
	if len(got) != 1 || got[0].Check != checkSameRemote || strings.Join(got[0].Members, ",") != "a,b" {
		t.Errorf("same remote checks = %+v", got)
	}
	for in, want := range map[string]string{
		"https://user:tok3n@github.com/o/r.git": "https://github.com/o/r.git",
		"https://github.com/o/r":                "https://github.com/o/r",
		"git@github.com:o/r.git":                "git@github.com:o/r.git",
		"ssh://git@host:22/o/r":                 "ssh://host:22/o/r",
		"https://tok@host":                      "https://host",
	} {
		if got := gitref.StripCredentials(in); got != want {
			t.Errorf("StripCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProjectCallStatus is the project-scope status rule (T022 MR-UI): a
// call linked in its repository stays linked; else a project link makes
// it linked_across, an ambiguous outcome ambiguous_across; else the
// repository's status stays.
func TestProjectCallStatus(t *testing.T) {
	node := func(id, status string) contract.Node {
		return contract.Node{Identity: id, Attributes: map[string]any{"status": status}}
	}
	linked := map[string]bool{"a": true, "c": true}
	ambiguous := map[string]bool{"b": true, "c": true}
	for _, tc := range []struct{ id, status, want string }{
		{"a", "linked", "linked"},
		{"a", "unmatched", "linked_across"},
		{"b", "unmatched", "ambiguous_across"},
		{"c", "unmatched", "linked_across"},
		{"d", "unmatched", "unmatched"},
		{"d", "dynamic", "dynamic"},
		{"b", "dynamic", "ambiguous_across"},
	} {
		if got := projectCallStatus(node(tc.id, tc.status), linked, ambiguous); got != tc.want {
			t.Errorf("%s %s = %s, want %s", tc.id, tc.status, got, tc.want)
		}
	}
}

// TestProjectPossibleCallers is the T022 review gate for serve
// --project on the projectthree fixture (a frontend and two backends that
// serve the same paths), on both backends: the frontend's ambiguous calls
// are possible callers of both backends' routes ("may be called by", never
// callers), and those routes count as possibly called, not as never
// called in the project.
func TestProjectPossibleCallers(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", "fixtures", "projectthree"))); err != nil {
			t.Fatal(err)
		}
		manifest := "version: 1\nname: three\nmembers:\n  - repository: web\n    path: web\n  - repository: express\n    path: express\n  - repository: axum\n    path: axum\n"
		if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{"web", "express", "axum"} {
			indexRepository(t, filepath.Join(root, m), w, m)
		}
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		counts := func(repo string) (never, maybe int, rows map[string][2]int) {
			t.Helper()
			var doc endpointsResponse
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?transport=http&repository="+repo, nil), &doc)
			if len(doc.Transports) != 1 || doc.Transports[0].NeverCalledInProject == nil || doc.Transports[0].PossiblyCalledInProject == nil {
				t.Fatalf("%s endpoints: %+v", repo, doc.Transports)
			}
			rows = map[string][2]int{}
			var list []routeRow
			raw, _ := json.Marshal(doc.Transports[0].Endpoints)
			json.Unmarshal(raw, &list)
			for _, r := range list {
				rows[r.ID] = [2]int{*r.ProjectCallerCount, *r.ProjectPossibleCallerCount}
			}
			return *doc.Transports[0].NeverCalledInProject, *doc.Transports[0].PossiblyCalledInProject, rows
		}
		// express: user and articles possibly called, login linked, health
		// never called.
		never, maybe, rows := counts("express")
		if never != 1 || maybe != 2 || rows["route:src/server.ts#GET%20%2Ft022r%2Fuser"] != [2]int{0, 1} || rows["route:src/server.ts#POST%20%2Ft022r%2Flogin"] != [2]int{1, 0} {
			t.Errorf("express: never %d, possibly %d, rows %v", never, maybe, rows)
		}
		if never, maybe, _ := counts("axum"); never != 0 || maybe != 2 {
			t.Errorf("axum: never %d, possibly %d", never, maybe)
		}
		var detail endpointResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+url.QueryEscape("route:src/server.ts#GET%20%2Ft022r%2Fuser")+"&repository=express", nil), &detail)
		if detail.Project == nil || len(detail.Project.Callers) != 0 || len(detail.Project.PossibleCallers) != 1 {
			t.Fatalf("express user endpoint: %+v", detail.Project)
		}
		pc := detail.Project.PossibleCallers[0]
		if pc.Repository != "web" || pc.Status != projectlink.StatusAmbiguous || pc.Wording != projectlink.PossibleAmbiguous || pc.CandidateCount != 2 || strings.Join(pc.CandidateRepositories, ",") != "axum,express" {
			t.Errorf("possible caller = %+v", pc)
		}
	})
}
