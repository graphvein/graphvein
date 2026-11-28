package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

// TestMain points the workspace registry at a temporary file, so no test
// of this package writes the user's own registry (ADR 0087).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "graphvein-workspace-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv(workspace.EnvFile, filepath.Join(dir, "workspace.yaml"))
	// New indexes go to the per-user stores folder (ADR 0093): every test
	// keeps them, and stores.yaml beside the registry above, in this
	// temporary folder, never in the user's own data folder.
	os.Setenv(storeloc.EnvStoreDir, filepath.Join(dir, "stores"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// freshWorkspace gives one test an empty registry of its own, so its
// startup line does not depend on what other tests registered.
func freshWorkspace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	t.Setenv(workspace.EnvFile, path)
	return path
}

// startServe runs serveCommand until the test ends and returns its first
// stdout line.
func startServe(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stdout := &lineWriter{lines: make(chan string, 4)}
	var stderr syncBuffer
	exit := make(chan int, 1)
	go func() { exit <- serveCommand(ctx, append(args, "--addr", "127.0.0.1:0"), stdout, &stderr) }()
	t.Cleanup(func() {
		cancel()
		select {
		case code := <-exit:
			if code != 0 {
				t.Errorf("serve exit %d: %s", code, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("serve did not stop")
		}
	})
	select {
	case line := <-stdout.lines:
		return line
	case code := <-exit:
		t.Fatalf("serve exited %d: %s", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("no startup line")
	}
	return ""
}

// TestServeWorkspace checks who registers and the serve forms (WS-6,
// D2): serve <path> registers its index and keeps its startup line;
// serve without a path serves the registry, answers per-index requests
// by ws= and refuses them without it; an entry added through the typed
// path flow is listed, and the next serve <path> counts it.
func TestServeWorkspace(t *testing.T) {
	registry := freshWorkspace(t)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	repo := func(name string) string {
		root := filepath.Join(t.TempDir(), name)
		if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
			t.Fatal(err)
		}
		if code, out, errOut := runCLI("index", root); code != 0 {
			t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
		}
		return root
	}
	alpha, beta := repo("alpha"), repo("beta")
	if _, err := os.Stat(registry); !os.IsNotExist(err) {
		t.Fatalf("graphvein index wrote the registry: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{}, Timeout: 30 * time.Second}
	call := func(method, url, token, body string, want int) []byte {
		t.Helper()
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, url, r)
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
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, url, resp.StatusCode, data)
		}
		return data
	}
	type list struct {
		Token   string  `json:"token"`
		Current *string `json:"current"`
		Entries []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Current bool   `json:"current"`
		} `json:"entries"`
	}
	read := func(base string) list {
		t.Helper()
		var l list
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/workspace", "", "", 200), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}

	t.Run("serve path registers", func(t *testing.T) {
		line := startServe(t, alpha)
		base, ok := strings.CutPrefix(line, "Serving alpha at ")
		if !ok {
			t.Fatalf("startup line %q", line)
		}
		l := read(base)
		if len(l.Entries) != 1 || l.Entries[0].Name != "alpha" || !l.Entries[0].Current || l.Current == nil || *l.Current != l.Entries[0].ID {
			t.Fatalf("workspace %+v", l)
		}
		data, err := os.ReadFile(registry)
		if err != nil || !strings.Contains(string(data), "name: alpha") {
			t.Fatalf("registry %s: %v", data, err)
		}
	})

	t.Run("serve without a path serves the registry", func(t *testing.T) {
		line := startServe(t)
		base, ok := strings.CutPrefix(line, "Serving the workspace (1 entries) at ")
		if !ok {
			t.Fatalf("startup line %q", line)
		}
		l := read(base)
		if l.Current != nil || len(l.Entries) != 1 {
			t.Fatalf("workspace %+v", l)
		}
		call(http.MethodGet, base+"api/v0/status", "", "", http.StatusBadRequest)
		var status struct {
			Repository string `json:"repository"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/status?ws="+l.Entries[0].ID, "", "", 200), &status); err != nil || status.Repository != "alpha" {
			t.Fatalf("status %+v %v", status, err)
		}
		var check struct {
			Outcome string `json:"outcome"`
			Confirm string `json:"confirm"`
			Index   struct {
				State string `json:"state"`
			} `json:"index"`
			Resolved string `json:"resolved"`
		}
		body, _ := json.Marshal(map[string]string{"path": beta})
		if err := json.Unmarshal(call(http.MethodPost, base+"api/v0/workspace/check", l.Token, string(body), 200), &check); err != nil {
			t.Fatal(err)
		}
		if check.Outcome != "ready" || check.Confirm == "" || check.Index.State != "present" {
			t.Fatalf("check %+v", check)
		}
		body, _ = json.Marshal(map[string]any{"path": check.Resolved, "confirm": check.Confirm, "index": false})
		call(http.MethodPost, base+"api/v0/workspace/add", l.Token, string(body), 200)
		if l := read(base); len(l.Entries) != 2 {
			t.Fatalf("after add %+v", l)
		}
	})

	t.Run("serve path counts the others", func(t *testing.T) {
		line := startServe(t, alpha)
		if !strings.HasPrefix(line, "Serving alpha (+1 in the workspace) at ") {
			t.Fatalf("startup line %q", line)
		}
	})
	t.Run("GRAPHVEIN_SQLITE_PATH applies only to the command-line index", func(t *testing.T) {
		// WS-70 (ADR 0087 revision 5): the variable names the store of
		// the folder on the command line only; every other entry opens
		// from its own store through the resolver, never from that file.
		single := filepath.Join(t.TempDir(), "single.db")
		t.Setenv("GRAPHVEIN_SQLITE_PATH", single)
		if code, out, errOut := runCLI("index", alpha); code != 0 {
			t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
		}
		before, err := os.Stat(single)
		if err != nil {
			t.Fatal(err)
		}
		line := startServe(t, alpha)
		base := line[strings.LastIndex(line, " ")+1:]
		l := read(base)
		var alphaID, betaID string
		for _, e := range l.Entries {
			if e.Name == "alpha" {
				alphaID = e.ID
			} else {
				betaID = e.ID
			}
		}
		var status struct {
			Repository string `json:"repository"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/status?ws="+alphaID, "", "", 200), &status); err != nil || status.Repository != "alpha" {
			t.Fatalf("command-line index: %+v %v", status, err)
		}
		var other struct {
			Repository string `json:"repository"`
			Index      struct {
				Indexed bool `json:"indexed"`
			} `json:"index"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/status?ws="+betaID, "", "", 200), &other); err != nil || other.Repository != "beta" || !other.Index.Indexed {
			t.Fatalf("another entry under GRAPHVEIN_SQLITE_PATH: %+v %v", other, err)
		}
		// Settings' Storage section says which entry uses the variable
		// and that beta's store is its own.
		var storage struct {
			SQLitePath struct {
				Set   bool    `json:"set"`
				Entry *string `json:"entry"`
			} `json:"sqlite_path"`
			Entries []struct {
				ID    string `json:"id"`
				Store struct {
					Path   *string `json:"path"`
					Origin *string `json:"origin"`
				} `json:"store"`
				Movable bool `json:"movable"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/store", "", "", 200), &storage); err != nil {
			t.Fatal(err)
		}
		if !storage.SQLitePath.Set || storage.SQLitePath.Entry == nil || *storage.SQLitePath.Entry != alphaID {
			t.Fatalf("sqlite_path %+v", storage.SQLitePath)
		}
		for _, e := range storage.Entries {
			switch e.ID {
			case alphaID:
				if e.Store.Path == nil || *e.Store.Path != single || e.Movable {
					t.Fatalf("alpha store %+v", e)
				}
			case betaID:
				if e.Store.Path == nil || *e.Store.Path == single || !e.Movable {
					t.Fatalf("beta store %+v", e)
				}
			}
		}
		if after, err := os.Stat(single); err != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
			t.Fatalf("the variable's file changed: %v", err)
		}
	})

	t.Run("serve path after a rename keeps one entry", func(t *testing.T) {
		// T038 review follow-up: identity is the folder; the name shown
		// is re-read from graphvein.yaml when the index opens.
		first := startServe(t, alpha)
		before := read(first[strings.LastIndex(first, " ")+1:])
		if err := os.WriteFile(filepath.Join(beta, "graphvein.yaml"), []byte("version: 1\nrepository: gamma\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, out, errOut := runCLI("index", beta); code != 0 {
			t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
		}
		line := startServe(t, beta)
		base, ok := strings.CutPrefix(line, "Serving gamma (+1 in the workspace) at ")
		if !ok {
			t.Fatalf("startup line %q", line)
		}
		l := read(base)
		if len(l.Entries) != len(before.Entries) {
			t.Fatalf("a second entry for one folder: %+v", l.Entries)
		}
		for _, e := range l.Entries {
			if e.Current && (e.Name != "gamma" || !strings.HasPrefix(e.ID, "beta~")) {
				t.Fatalf("renamed entry %+v: want name gamma under the registered id", e)
			}
		}
		var status struct {
			Repository string `json:"repository"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/status", "", "", 200), &status); err != nil || status.Repository != "gamma" {
			t.Fatalf("status %+v %v", status, err)
		}
	})

	t.Run("an entry whose graphvein.yaml does not load opens", func(t *testing.T) {
		// T038 x T039 integration: serve without a path opens such an
		// entry as serve <path> does (ADR 0088): its views answer and
		// GET /api/v0/config carries the decoder's message, so the UI
		// gates them and Settings can fix the file.
		file := filepath.Join(alpha, "graphvein.yaml")
		if err := os.WriteFile(file, []byte("version: 1\nindex: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(file)
		line := startServe(t)
		base := line[strings.LastIndex(line, " ")+1:]
		var alphaID string
		for _, e := range read(base).Entries {
			if e.Name == "alpha" {
				alphaID = e.ID
			}
		}
		if alphaID == "" {
			t.Fatal("no alpha entry")
		}
		var status struct {
			Repository string `json:"repository"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/status?ws="+alphaID, "", "", 200), &status); err != nil || status.Repository != "alpha" {
			t.Fatalf("status %+v %v", status, err)
		}
		var cfg struct {
			ConfigError *string `json:"config_error"`
		}
		if err := json.Unmarshal(call(http.MethodGet, base+"api/v0/config?ws="+alphaID, "", "", 200), &cfg); err != nil || cfg.ConfigError == nil {
			t.Fatalf("config %+v %v", cfg, err)
		}
	})

	var out bytes.Buffer
	if data, err := os.ReadFile(registry); err != nil || strings.Count(string(data), "kind: repository") != 2 {
		out.Write(data)
		t.Fatalf("registry %s: %v", out.String(), err)
	}
}
