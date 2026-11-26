package deploy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/deploy"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	fixture  = "../../../testdata/fixtures/deploy-compose"
	taskPath = "../../../docs/contract/claims/architecture-graph.md"
)

// inertNames maps the fixture's inert file names to the names the indexer
// reads (README.md lists them). The fixture is stored under inert names so
// that the repository's own index gains no deploy facts from it.
var inertNames = map[string]string{
	"_compose.yml":                 "compose.yml",
	"_compose.deploy.yml":          "compose.deploy.yml",
	"_compose.override.yml":        "compose.override.yml",
	"_compose.dup.yml":             "compose.dup.yml",
	"_compose.empty.yml":           "compose.empty.yml",
	"_docker-compose.broken.yml":   "docker-compose.broken.yml",
	"ops/_docker-compose.yaml":     "ops/docker-compose.yaml",
	"api/_Dockerfile":              "api/Dockerfile",
	"api/_Dockerfile.dockerignore": "api/Dockerfile.dockerignore",
	"web/_web.Dockerfile_":         "web/web.Dockerfile",
	"web/_Dockerfile.md":           "web/Dockerfile.md",
	"tools/_Dockerfile":            "tools/Dockerfile",
	"envset/_env":                  "envset/.env",
	"envset/_compose.yml":          "envset/compose.yml",
	"envset/_compose.override.yml": "envset/compose.override.yml",
}

// FixtureRoot copies the fixture to a temporary repository and restores
// its real names.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	for from, to := range inertNames {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// projection is what the golden keeps: deploy nodes and their refusals.
type projection struct {
	Nodes       []contract.Node
	Diagnostics []contract.Diagnostic
}

func project(s store.Snapshot) projection {
	p := projection{Nodes: []contract.Node{}, Diagnostics: []contract.Diagnostic{}}
	for _, n := range s.Nodes {
		if n.Kind == "deploy_service" || n.Kind == "dockerfile" {
			p.Nodes = append(p.Nodes, n)
		}
	}
	for _, d := range s.Diagnostics {
		if d.Code == deploy.ComposeUnreadCode || d.Code == deploy.DockerfileUnreadCode {
			p.Diagnostics = append(p.Diagnostics, d)
		}
	}
	return p
}

// TestDeployComposeFixture indexes the fixture through the indexer, checks
// the deploy nodes and refusals against the golden, replays warm (same
// stream), and requires every row and modelled form to be reached.
func TestDeployComposeFixture(t *testing.T) {
	ctx := context.Background()
	root := fixtureRoot(t)
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var mu sync.Mutex
	tags := map[string]bool{}
	deploy.SetClaimTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { deploy.SetClaimTag(nil) })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0) {
			t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
		}
		first = data
		if run == 0 {
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			p := project(snapshot)
			golden.Check(t, fixture+".golden.json", p)
			checkNoSecrets(t, p)
		}
	}
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range deploy.Contract {
		if !reached[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for _, form := range []string{"DC-S1/image", "DC-S1/build", "DC-S1/build-unresolved", "DC-S1/depends_on", "DC-S1/ports", "DC-S1/refused",
		"DC-S2/other", "DC-T1/interpolated", "DC-T1/enable", "DC-T2/exposedbydefault", "DC-S1/dotenv-compose-file", "DC-F1/stage", "DC-F1/expose", "DC-F1/refused",
		"DCD/include", "DCD/extends", "DCD/no-services", "DCD/label", "DCD/argument", "DCD/dockerignore"} {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
}

// checkNoSecrets is the DC-S2 gate: no environment value beyond scheme
// and host is stored (the fixture's passwords, user names, ports, paths
// and query strings never appear).
func checkNoSecrets(t *testing.T, p projection) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cret-never-stored", "hunter2", "app:", "sslmode", "pa/ss", "not-a-url-never-stored", "token=abc", "/ingest", "6379", "remote-host", "agent", "never-stored-dotenv", "compose.prod.yml"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("stored deploy facts contain %q", secret)
		}
	}
}

// TestDeployConfigContract keeps the code's rows equal to the task file's
// DC table, in order, with a form named per row.
func TestDeployConfigContract(t *testing.T) {
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract: deploy config")
	if start < 0 {
		t.Fatal("no deploy-config table")
	}
	var ids []string
	for _, line := range strings.Split(text[start+1:], "\n")[1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| id |") && !strings.HasPrefix(line, "|---") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	var code []string
	for _, r := range deploy.Contract {
		code = append(code, r.ID)
		if r.Form == "" {
			t.Errorf("row %s names no form", r.ID)
		}
	}
	if !reflect.DeepEqual(ids, code) {
		t.Errorf("task rows %v, code rows %v", ids, code)
	}
}

// TestFileNames checks the DC-S1 and DC-F1 name forms.
func TestFileNames(t *testing.T) {
	for p, want := range map[string]bool{"compose.yml": true, "a/compose.yaml": true, "docker-compose.yml": true, "docker-compose.prod.yaml": true,
		"compose.deploy.yml": true, "x/docker-compose.override.yml": true, "dev.docker-compose.yml": false, "podman-compose.yml": false,
		"compose.json": false, "deploy-docker-compose.yml": false} {
		if got := deploy.ComposeFile(p); got != want {
			t.Errorf("ComposeFile(%q) = %v", p, got)
		}
	}
	for p, want := range map[string]bool{"Dockerfile": true, "a/Dockerfile.dev": true, "b/web.Dockerfile": true, "Dockerfile.dockerignore": false,
		"dockerfile": false, "Dockerfile-old": false} {
		if got := deploy.DockerfileName(p); got != want {
			t.Errorf("DockerfileName(%q) = %v", p, got)
		}
	}
}

// TestRefusalIsolation checks that a refused file affects no other file.
func TestRefusalIsolation(t *testing.T) {
	nodes, diags := deploy.Link(map[string][]byte{
		"a/compose.yml":  []byte("services: [\n"),
		"b/compose.yml":  []byte("services:\n  s:\n    image: alpine\n"),
		"c/Dockerfile":   []byte("RUN x\n"),
		"d/Dockerfile":   []byte("FROM alpine\n"),
		"e/compose.yml":  []byte("services: 1\n"),
		"f/compose.yml":  []byte("a: 1\n---\nservices: {}\n"),
		"g/compose.yml":  []byte("services:\n  s: [1]\n"),
		"h/Dockerfile.x": []byte("RUN <<EOF\nnever closed\n"),
	})
	var ids []string
	for _, n := range nodes {
		ids = append(ids, n.Identity)
	}
	if want := []string{"deploy_service:b/compose.yml#s", "dockerfile:d/Dockerfile#Dockerfile"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("nodes %v, want %v", ids, want)
	}
	var files []string
	for _, d := range diags {
		files = append(files, d.File)
	}
	if want := []string{"a/compose.yml", "c/Dockerfile", "e/compose.yml", "f/compose.yml", "g/compose.yml", "h/Dockerfile.x"}; !reflect.DeepEqual(files, want) {
		t.Errorf("refused %v, want %v", files, want)
	}
}
