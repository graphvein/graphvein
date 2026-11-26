package routes_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	authFixture = "../../../testdata/fixtures/auth-fastapi"
	taskPath    = "../../../docs/contract/claims/architecture-graph.md"
)

// authRoute is the golden's view of one route: identity, paths and the
// security attributes.
type authRoute struct {
	Identity string   `json:"identity"`
	Paths    []string `json:"paths"`
	Security any      `json:"security_dependencies,omitempty"`
	Unknown  any      `json:"security_dependencies_unknown,omitempty"`
}

// indexAuthFixture indexes root twice (the second run replays the cache)
// and returns the snapshot and the reached claim tags.
func indexAuthFixture(t *testing.T, root string) (store.Snapshot, map[string]bool) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var mu sync.Mutex
	tags := map[string]bool{}
	routes.SetAuthTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { routes.SetAuthTag(nil) })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	var snapshot store.Snapshot
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
			if snapshot, err = s.Dump(ctx, "fixture"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return snapshot, tags
}

// authRoutes projects the snapshot's routes for the golden.
func authRoutes(t *testing.T, s store.Snapshot) []authRoute {
	t.Helper()
	var out []authRoute
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		r := authRoute{Identity: n.Identity, Paths: []string{}, Security: n.Attributes["security_dependencies"], Unknown: n.Attributes["security_dependencies_unknown"]}
		paths, _ := n.Attributes["paths"].([]any)
		for _, p := range paths {
			m, _ := p.(map[string]any)
			template, _ := m["template"].(string)
			r.Paths = append(r.Paths, template)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// TestAuthFastAPIContract indexes the auth-fastapi fixture, checks the
// security attributes against the golden, replays warm, and checks that
// the fixture reaches every DA-F row and DAD and that the code's rows equal
// the task file's.
func TestAuthFastAPIContract(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(authFixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, tags := indexAuthFixture(t, root)
	golden.Check(t, authFixture+".golden.json", authRoutes(t, snapshot))
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range routes.FastAPIAuthContract {
		if !reached[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for _, form := range []string{"DA-F1/unresolved", "DA-F2/truncated", "DA-F3/subclass", "DA-F3/auto_error-unknown",
		"DAD/list", "DAD/splat", "DAD/item", "DAD/no-callable", "DAD/inline", "DAD/expression", "DAD/external", "DAD/cycle",
		"DAD/class", "DAD/instance", "DAD/ungated", "DAD/metadata", "DAD/chains"} {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
	checkTaskRows(t, "DA-F", contractIDs(routes.FastAPIAuthContract))
}

func contractIDs(rows []routes.AuthRow) []string {
	var out []string
	for _, r := range rows {
		if r.Form == "" {
			panic("row " + r.ID + " names no form")
		}
		out = append(out, r.ID)
	}
	return out
}

// checkTaskRows keeps the code rows equal to the task file's auth table
// rows with the given prefix, in order, followed by DAD.
func checkTaskRows(t *testing.T, prefix string, code []string) {
	t.Helper()
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract: auth extractors")
	if start < 0 {
		t.Fatal("no auth extractors table")
	}
	var ids []string
	for _, line := range strings.Split(text[start+1:], "\n")[1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "| "+prefix) || strings.HasPrefix(line, "| DAD ") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	if !reflect.DeepEqual(ids, code) {
		t.Errorf("task rows %v, code rows %v", ids, code)
	}
}
