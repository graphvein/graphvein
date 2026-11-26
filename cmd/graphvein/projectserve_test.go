package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/store"
)

// TestServeProject serves the projectimpact fixture with serve --project
// over a real socket on both backends (T022 B8): the project endpoint, a
// member endpoint through its repository parameter, the 400 without it,
// the backend route's cross-repository callers, and a re-index from the
// UI that indexes both members and runs the link pass under the same locks
// as graphvein index --project.
func TestServeProject(t *testing.T) {
	projectBackends(t, func(t *testing.T, _ func() store.Store) {
		root := copyProjectImpactFixture(t)
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stdout := &lineWriter{lines: make(chan string, 4)}
		var stderr syncBuffer
		freshWorkspace(t)
		exit := make(chan int, 1)
		go func() {
			exit <- serveCommand(ctx, []string{"--project", root, "--addr", "127.0.0.1:0"}, stdout, &stderr)
		}()
		var line string
		select {
		case line = <-stdout.lines:
		case code := <-exit:
			t.Fatalf("serve exited %d: %s", code, stderr.String())
		case <-time.After(10 * time.Second):
			t.Fatal("no startup line")
		}
		base, ok := strings.CutPrefix(line, "Serving project users (2 members) at ")
		if !ok {
			t.Fatalf("startup line %q", line)
		}
		client := &http.Client{Transport: &http.Transport{}, Timeout: 30 * time.Second}
		fetch := func(method, path string, want int, v any) {
			t.Helper()
			var body io.Reader
			if method == http.MethodPost {
				body = strings.NewReader("{}")
			}
			req, err := http.NewRequest(method, base+path, body)
			if err != nil {
				t.Fatal(err)
			}
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != want {
				t.Fatalf("%s %s: %d %v %s", method, path, resp.StatusCode, err, data)
			}
			if v != nil {
				if err := json.Unmarshal(data, v); err != nil {
					t.Fatal(err, string(data))
				}
			}
		}
		var project struct {
			Members []struct {
				Repository string `json:"repository"`
				Checkout   bool   `json:"checkout"`
			} `json:"members"`
			LinkPass struct {
				State string `json:"state"`
				Pass  int64  `json:"pass"`
				Links int    `json:"links"`
			} `json:"link_pass"`
		}
		fetch(http.MethodGet, "api/v0/project", 200, &project)
		if len(project.Members) != 2 || project.LinkPass.State != "fresh" || project.LinkPass.Pass != 1 || project.LinkPass.Links != 3 {
			t.Errorf("project = %+v", project)
		}
		fetch(http.MethodGet, "api/v0/status", 400, nil)
		var status struct {
			Repository string `json:"repository"`
		}
		fetch(http.MethodGet, "api/v0/status?repository=frontend", 200, &status)
		if status.Repository != "frontend" {
			t.Errorf("member status = %+v", status)
		}
		var endpoint struct {
			Project struct {
				Callers []struct {
					Repository string `json:"repository"`
					Assumption string `json:"assumption"`
				} `json:"callers"`
			} `json:"project"`
		}
		fetch(http.MethodGet, "api/v0/api/endpoint?repository=backend&id="+url.QueryEscape(usersRoute), 200, &endpoint)
		if len(endpoint.Project.Callers) != 1 || endpoint.Project.Callers[0].Repository != "frontend" || endpoint.Project.Callers[0].Assumption == "" {
			t.Errorf("endpoint project callers = %+v", endpoint.Project)
		}

		// Re-index from the UI: both members, then the link pass.
		fetch(http.MethodPost, "api/v0/index/run", 202, nil)
		type jobDoc struct {
			Project string `json:"project"`
			Job     *struct {
				State   string  `json:"state"`
				EndedAt *string `json:"ended_at"`
				Project struct {
					Members []struct {
						Repository string `json:"repository"`
						State      string `json:"state"`
					} `json:"members"`
					LinkPass *struct {
						Pass  int64 `json:"pass"`
						Links int   `json:"links"`
					} `json:"link_pass"`
				} `json:"project"`
			} `json:"job"`
		}
		var doc jobDoc
		deadline := time.Now().Add(60 * time.Second)
		for {
			doc = jobDoc{}
			fetch(http.MethodGet, "api/v0/index/run", 200, &doc)
			if doc.Job != nil && doc.Job.EndedAt != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("re-index did not end")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if doc.Project != "users" || doc.Job.State != "committed" || doc.Job.Project.LinkPass == nil || doc.Job.Project.LinkPass.Pass != 2 || doc.Job.Project.LinkPass.Links != 3 ||
			len(doc.Job.Project.Members) != 2 || doc.Job.Project.Members[0].State != "committed" || doc.Job.Project.Members[1].State != "committed" {
			t.Errorf("project re-index = %+v %+v", doc, doc.Job)
		}
		fetch(http.MethodGet, "api/v0/project", 200, &project)
		if project.LinkPass.State != "fresh" || project.LinkPass.Pass != 2 {
			t.Errorf("project after re-index = %+v", project)
		}
		cancel()
		if code := <-exit; code != 0 {
			t.Errorf("serve exit %d: %s", code, stderr.String())
		}
	})
}

func TestServeProjectUsage(t *testing.T) {
	root := copyProjectImpactFixture(t)
	for _, args := range [][]string{
		{"--project", root, root},
		{"--project", root, "--repository", "backend"},
		{"--project", root, "--addr", "0.0.0.0:7420"},
		{"--project", t.TempDir()},
	} {
		var out, errOut bytes.Buffer
		if code := serveCommand(context.Background(), args, &out, &errOut); code != 2 || out.Len() != 0 {
			t.Errorf("%v: exit %d, stdout %q stderr %q", args, code, out.String(), errOut.String())
		}
	}
}

// TestServeProjectManifestEdit is T022 B10 end to end on both backends:
// serve --project on the two-backend fixture (every call ambiguous),
// accept the env-file suggestion from the UI's endpoints (preview, then
// apply with the base and result hashes), and run the offered link pass
// only: the server serves the new declaration without a restart, no
// member is indexed again, and the calls link to the proposed backend.
func TestServeProjectManifestEdit(t *testing.T) {
	projectBackends(t, func(t *testing.T, open func() store.Store) {
		root := copyProjectFixture(t, threeManifest)
		for path, content := range map[string]string{
			"frontend/.env": "VITE_API=http://localhost:3100\n",
			"backend/.env":  "PORT=3100\n",
		} {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if code, out, errOut := runCLI("index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stdout := &lineWriter{lines: make(chan string, 4)}
		var stderr syncBuffer
		freshWorkspace(t)
		exit := make(chan int, 1)
		go func() {
			exit <- serveCommand(ctx, []string{"--project", root, "--addr", "127.0.0.1:0"}, stdout, &stderr)
		}()
		var line string
		select {
		case line = <-stdout.lines:
		case code := <-exit:
			t.Fatalf("serve exited %d: %s", code, stderr.String())
		case <-time.After(10 * time.Second):
			t.Fatal("no startup line")
		}
		base, ok := strings.CutPrefix(line, "Serving project shop-three (3 members) at ")
		if !ok {
			t.Fatalf("startup line %q", line)
		}
		client := &http.Client{Transport: &http.Transport{}, Timeout: 30 * time.Second}
		var token string
		fetch := func(method, path, body string, want int, v any) {
			t.Helper()
			req, err := http.NewRequest(method, base+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Graphvein-Token", token)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != want {
				t.Fatalf("%s %s: %d %v %s", method, path, resp.StatusCode, err, data)
			}
			if v != nil {
				if err := json.Unmarshal(data, v); err != nil {
					t.Fatal(err, string(data))
				}
			}
		}
		var manifest struct {
			Hash  string `json:"hash"`
			Token string `json:"token"`
		}
		fetch(http.MethodGet, "api/v0/project/manifest", "", 200, &manifest)
		token = manifest.Token
		var suggestions struct {
			Proposals []struct {
				Number   int    `json:"number"`
				Member   string `json:"member"`
				Provider string `json:"provider"`
				Evidence []struct {
					File string `json:"file"`
				} `json:"evidence"`
			} `json:"proposals"`
		}
		fetch(http.MethodGet, "api/v0/project/suggestions", "", 200, &suggestions)
		if len(suggestions.Proposals) != 1 || suggestions.Proposals[0].Provider != "backend" || len(suggestions.Proposals[0].Evidence) != 2 {
			t.Fatalf("suggestions = %+v", suggestions)
		}
		var project struct {
			Members []struct {
				Repository string `json:"repository"`
				Index      *struct {
					FinishedAt string `json:"finished_at"`
				} `json:"index"`
			} `json:"members"`
			LinkPass struct {
				State string `json:"state"`
				Pass  int64  `json:"pass"`
				Links int    `json:"links"`
			} `json:"link_pass"`
			Declarations struct {
				Consumes []struct {
					Provider string `json:"provider"`
				} `json:"consumes"`
			} `json:"declarations"`
		}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if len(project.Declarations.Consumes) != 0 {
			t.Fatalf("before the edit: %+v", project)
		}
		linksBefore := project.LinkPass.Links
		ops := `"operations":[{"op":"accept_suggestion","number":1}]`
		var preview struct {
			ResultHash string `json:"result_hash"`
			Diff       string `json:"diff"`
			Valid      bool   `json:"valid"`
			Reindex    struct {
				Kind string `json:"kind"`
			} `json:"reindex"`
		}
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`}`, 200, &preview)
		if !preview.Valid || preview.Reindex.Kind != "link_pass" || !strings.Contains(preview.Diff, "+    consumes:\n+      - repository: backend\n+        select:\n+          bases: [VITE_API]\n") {
			t.Fatalf("preview = %+v", preview)
		}
		data, _ := os.ReadFile(filepath.Join(root, "graphvein-project.yaml"))
		if string(data) != threeManifest {
			t.Fatal("a preview wrote the manifest")
		}
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`,"apply":true,"result_hash":"`+preview.ResultHash+`"}`, 200, nil)
		data, _ = os.ReadFile(filepath.Join(root, "graphvein-project.yaml"))
		if !strings.Contains(string(data), "# frontend calls through VITE_API") || !strings.Contains(string(data), "bases: [VITE_API]") {
			t.Fatalf("written manifest:\n%s", data)
		}
		// Served at once, links stale until the link pass.
		runsBefore := map[string]string{}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if len(project.Declarations.Consumes) != 1 || project.LinkPass.State != "stale" {
			t.Fatalf("after the edit: %+v", project)
		}
		for _, m := range project.Members {
			if m.Index != nil {
				runsBefore[m.Repository] = m.Index.FinishedAt
			}
		}
		fetch(http.MethodPost, "api/v0/index/run", `{"link_pass_only":true}`, 202, nil)
		var job struct {
			Job *struct {
				State   string  `json:"state"`
				EndedAt *string `json:"ended_at"`
				Project struct {
					Members []struct {
						State string `json:"state"`
					} `json:"members"`
				} `json:"project"`
			} `json:"job"`
		}
		deadline := time.Now().Add(60 * time.Second)
		for {
			job.Job = nil
			fetch(http.MethodGet, "api/v0/index/run", "", 200, &job)
			if job.Job != nil && job.Job.EndedAt != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("link pass did not end")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if job.Job.State != "committed" {
			t.Fatalf("link pass job = %+v", job.Job)
		}
		for _, m := range job.Job.Project.Members {
			if m.State != "linked_from_store" {
				t.Errorf("member state in a link-pass-only job: %+v", job.Job.Project.Members)
			}
		}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if project.LinkPass.State != "fresh" || project.LinkPass.Pass != 2 || project.LinkPass.Links <= linksBefore {
			t.Fatalf("after the link pass: %+v", project)
		}
		for _, m := range project.Members {
			if m.Index != nil && runsBefore[m.Repository] != m.Index.FinishedAt {
				t.Errorf("member %s was indexed again by a link-pass-only job", m.Repository)
			}
		}
		links, err := open().ProjectLinks(context.Background(), "shop-three")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range links.Links {
			if l.Target.Repository != "backend" {
				t.Errorf("link to %s after accepting backend: %+v", l.Target.Repository, l)
			}
		}

		// T022 B11: a rename retires the old name in the same write (no
		// rows stay under backend), and the offered project re-index links
		// the new name.
		fetch(http.MethodGet, "api/v0/project/manifest", "", 200, &manifest)
		ops = `"operations":[{"op":"rename_repository","member":"backend","to":"api"}]`
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`}`, 200, &preview)
		if !preview.Valid || preview.Reindex.Kind != "project" {
			t.Fatalf("rename preview = %+v", preview)
		}
		// T022 review: the preview names the stored index the write
		// deletes, and apply needs its explicit confirm.
		var retiring struct {
			Retiring []struct {
				Repository string   `json:"repository"`
				Stored     bool     `json:"stored"`
				Nodes      int      `json:"nodes"`
				Projects   []string `json:"projects"`
				Standalone bool     `json:"standalone"`
				Confirm    string   `json:"confirm"`
			} `json:"retiring"`
		}
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`}`, 200, &retiring)
		if len(retiring.Retiring) != 1 || retiring.Retiring[0].Repository != "backend" || !retiring.Retiring[0].Stored || retiring.Retiring[0].Nodes == 0 ||
			retiring.Retiring[0].Standalone || len(retiring.Retiring[0].Projects) != 1 || retiring.Retiring[0].Confirm != fmt.Sprintf("delete stored index of backend (%d nodes)", retiring.Retiring[0].Nodes) {
			t.Fatalf("rename preview retiring = %+v", retiring)
		}
		before, _ := os.ReadFile(filepath.Join(root, "graphvein-project.yaml"))
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`,"apply":true,"result_hash":"`+preview.ResultHash+`"}`, 409, nil)
		if after, _ := os.ReadFile(filepath.Join(root, "graphvein-project.yaml")); string(after) != string(before) {
			t.Fatal("a rename without the delete confirm wrote the manifest")
		}
		var renamed struct {
			Applied bool `json:"applied"`
			Retired []struct {
				Repository string  `json:"repository"`
				Removed    bool    `json:"removed"`
				Error      *string `json:"error"`
			} `json:"retired"`
		}
		fetch(http.MethodPost, "api/v0/project/manifest", `{"base_hash":"`+manifest.Hash+`",`+ops+`,"apply":true,"result_hash":"`+preview.ResultHash+`","confirm_delete":["backend"]}`, 200, &renamed)
		if !renamed.Applied || len(renamed.Retired) != 1 || renamed.Retired[0].Repository != "backend" || !renamed.Retired[0].Removed || renamed.Retired[0].Error != nil {
			t.Fatalf("rename = %+v", renamed)
		}
		bg := context.Background()
		st := open()
		repos, err := st.Repositories(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range repos {
			if r.ID == "backend" {
				t.Errorf("backend's stored index stays after the rename: %+v", repos)
			}
		}
		if runs, err := st.Runs(bg, "backend"); err != nil || len(runs) != 0 {
			t.Errorf("runs of backend after the rename = %d, %v", len(runs), err)
		}
		retired, err := st.ProjectLinks(bg, "shop-three")
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range retired.Members {
			if m.Repository == "backend" {
				t.Errorf("member backend stays in the project tables: %+v", retired.Members)
			}
		}
		for _, l := range retired.Links {
			if l.Source.Repository == "backend" || l.Target.Repository == "backend" {
				t.Errorf("link naming backend stays: %+v", l)
			}
		}
		for _, o := range retired.Outcomes {
			if o.Source.Repository == "backend" || (o.Target != nil && o.Target.Repository == "backend") {
				t.Errorf("outcome naming backend stays: %+v", o)
			}
		}
		fetch(http.MethodPost, "api/v0/index/run", `{}`, 202, nil)
		deadline = time.Now().Add(120 * time.Second)
		for {
			job.Job = nil
			fetch(http.MethodGet, "api/v0/index/run", "", 200, &job)
			if job.Job != nil && job.Job.EndedAt != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("project re-index did not end")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if job.Job.State != "committed" {
			t.Fatalf("project re-index job = %+v", job.Job)
		}
		links, err = open().ProjectLinks(bg, "shop-three")
		if err != nil {
			t.Fatal(err)
		}
		if len(links.Links) == 0 {
			t.Error("no links after re-indexing the renamed member")
		}
		for _, l := range links.Links {
			if l.Target.Repository != "api" {
				t.Errorf("link to %s after the rename: %+v", l.Target.Repository, l)
			}
		}

		// T022 B11: a manifest edited on disk is served by the next project
		// request. A comment changes no declaration (links stay fresh); a
		// removed consumes entry does (links stale until the link pass),
		// and an invalid file keeps the manifest served before.
		manifestPath := filepath.Join(root, "graphvein-project.yaml")
		onDisk, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if project.LinkPass.State != "fresh" || len(project.Declarations.Consumes) != 1 {
			t.Fatalf("after the re-index: %+v", project)
		}
		if err := os.WriteFile(manifestPath, append([]byte("# edited on disk\n"), onDisk...), 0o644); err != nil {
			t.Fatal(err)
		}
		fetch(http.MethodGet, "api/v0/project/manifest", "", 200, &manifest)
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if project.LinkPass.State != "fresh" || manifest.Hash == "" {
			t.Fatalf("after a comment edited on disk: %+v", project)
		}
		withoutConsumes := strings.Replace(string(onDisk), "    consumes:\n      - repository: api\n        select:\n          bases: [VITE_API]\n", "", 1)
		if withoutConsumes == string(onDisk) {
			t.Fatalf("no consumes entry to remove in:\n%s", onDisk)
		}
		if err := os.WriteFile(manifestPath, []byte(withoutConsumes), 0o644); err != nil {
			t.Fatal(err)
		}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if project.LinkPass.State != "stale" || len(project.Declarations.Consumes) != 0 {
			t.Fatalf("after a declaration edited on disk: %+v", project)
		}
		if err := os.WriteFile(manifestPath, []byte("version: 1\nname: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fetch(http.MethodGet, "api/v0/project", "", 200, &project)
		if project.LinkPass.State != "stale" || len(project.Members) != 3 {
			t.Fatalf("after an invalid manifest on disk: %+v", project)
		}
		cancel()
		if code := <-exit; code != 0 {
			t.Errorf("serve exit %d: %s", code, stderr.String())
		}
	})
}
