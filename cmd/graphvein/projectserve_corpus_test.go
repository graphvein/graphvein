//go:build graphveincorpus

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/projectlink"
)

// TestCorpusProjectServe is T022 B8's corpus gate on RealWorld react +
// express (scratch copies, the pins of TestCorpusProjectIndex): served
// with serve --project, the API section's endpoint of every express route
// lists the react calls the project links to it, over all routes the 27
// linked Orval calls, each with the default assumption, inferred
// provenance and confidence 0.7; the endpoints list counts them per route.
func TestCorpusProjectServe(t *testing.T) {
	react := pinnedCorpus(t, "react", "969709a379b13935b4e1caae0ad8cad548e5879a")
	express := pinnedCorpus(t, "express", "94a864e4e458a5e0ae2b07d3cf762791769e8690")
	scratch := t.TempDir()
	copyCheckout(t, react, filepath.Join(scratch, "react"))
	copyCheckout(t, express, filepath.Join(scratch, "express"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "project.db"))
	dir := filepath.Join(scratch, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, "version: 1\nname: realworld-express\nmembers:\n  - repository: react\n    path: ../react\n  - repository: express\n    path: ../express\n")
	if code, out, errOut := runCLI("index", "--project", dir); code != 0 {
		t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &lineWriter{lines: make(chan string, 4)}
	var stderr syncBuffer
	freshWorkspace(t)
	exit := make(chan int, 1)
	go func() {
		exit <- serveCommand(ctx, []string{"--project", dir, "--addr", "127.0.0.1:0"}, stdout, &stderr)
	}()
	var line string
	select {
	case line = <-stdout.lines:
	case code := <-exit:
		t.Fatalf("serve exited %d: %s", code, stderr.String())
	case <-time.After(30 * time.Second):
		t.Fatal("no startup line")
	}
	base, ok := strings.CutPrefix(line, "Serving project realworld-express (2 members) at ")
	if !ok {
		t.Fatalf("startup line %q", line)
	}
	client := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Minute}
	fetch := func(path string, v any) {
		t.Helper()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %d %v %s", path, resp.StatusCode, err, data)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatal(err)
		}
	}
	var list struct {
		Transports []struct {
			NeverCalled          int  `json:"never_called"`
			NeverCalledInProject *int `json:"never_called_in_project"`
			Endpoints            []struct {
				ID                 string `json:"id"`
				ProjectCallerCount *int   `json:"project_caller_count"`
			} `json:"endpoints"`
		} `json:"transports"`
	}
	fetch("api/v0/api/endpoints?repository=express&transport=http&scope=all", &list)
	if len(list.Transports) != 1 || list.Transports[0].NeverCalledInProject == nil {
		t.Fatalf("endpoints list: %+v", list)
	}
	counted, routesWithCallers := 0, 0
	calls := map[string]bool{}
	for _, row := range list.Transports[0].Endpoints {
		counted += *row.ProjectCallerCount
		var detail struct {
			Project struct {
				Callers []struct {
					Repository string  `json:"repository"`
					Identity   string  `json:"identity"`
					Provenance string  `json:"provenance"`
					Confidence float64 `json:"confidence"`
					Assumption string  `json:"assumption"`
					Stale      bool    `json:"stale"`
					Node       *struct {
						IDE *struct {
							Link string `json:"link"`
						} `json:"ide"`
					} `json:"node"`
				} `json:"callers"`
			} `json:"project"`
		}
		fetch("api/v0/api/endpoint?repository=express&scope=all&id="+url.QueryEscape(row.ID), &detail)
		if len(detail.Project.Callers) != *row.ProjectCallerCount {
			t.Errorf("%s: %d project callers, list counts %d", row.ID, len(detail.Project.Callers), *row.ProjectCallerCount)
		}
		if len(detail.Project.Callers) > 0 {
			routesWithCallers++
		}
		for _, c := range detail.Project.Callers {
			calls[c.Identity] = true
			if c.Repository != "react" || c.Provenance != "inferred" || c.Confidence != 0.7 || c.Assumption != projectlink.DefaultAssumption || c.Stale || c.Node == nil {
				t.Errorf("%s: project caller %+v", row.ID, c)
			}
		}
	}
	t.Logf("serve --project: %d express routes, %d with react callers, %d project callers listed, %d distinct react calls; never_called %d, never_called_in_project %d",
		len(list.Transports[0].Endpoints), routesWithCallers, counted, len(calls), list.Transports[0].NeverCalled, *list.Transports[0].NeverCalledInProject)
	if len(calls) != 27 {
		t.Errorf("the express routes' endpoints list %d distinct react calls, want 27", len(calls))
	}
	cancel()
	if code := <-exit; code != 0 {
		t.Errorf("serve exit %d: %s", code, stderr.String())
	}
}
