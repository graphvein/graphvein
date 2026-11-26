package clients_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	heyAuthFixture = "../../../testdata/fixtures/auth-heyapi"
	heyTaskPath    = "../../../docs/contract/claims/architecture-graph.md"
)

// heyAuthCall is the golden's view of one client call.
type heyAuthCall struct {
	Identity string `json:"identity"`
	Library  any    `json:"library"`
	Method   any    `json:"method"`
	URL      any    `json:"url_expression"`
	Security any    `json:"security,omitempty"`
}

// indexHeyAuth indexes root twice (the second run replays the cache) and
// returns the first snapshot and the reached claim tags.
func indexHeyAuth(t *testing.T, root string) (store.Snapshot, map[string]bool) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var mu sync.Mutex
	tags := map[string]bool{}
	clients.SetAuthTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { clients.SetAuthTag(nil) })
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

func heyAuthCalls(s store.Snapshot) []heyAuthCall {
	var out []heyAuthCall
	for _, n := range s.Nodes {
		if n.Kind == "client_call" {
			out = append(out, heyAuthCall{Identity: n.Identity, Library: n.Attributes["library"], Method: n.Attributes["method"], URL: n.Attributes["url_expression"], Security: n.Attributes["security"]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// TestAuthHeyAPIContract indexes the auth-heyapi fixture, checks the
// client calls' security attributes against the golden, replays warm, and
// checks that the fixture reaches DA-H1 and DAD and that the code's rows
// equal the task file's.
func TestAuthHeyAPIContract(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(heyAuthFixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, tags := indexHeyAuth(t, root)
	golden.Check(t, heyAuthFixture+".golden.json", heyAuthCalls(snapshot))
	for _, form := range []string{"DA-H1", "DA-H1/unknown", "DAD/absent"} {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
	data, err := os.ReadFile(heyTaskPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract: auth extractors")
	if start < 0 {
		t.Fatal("no auth extractors table")
	}
	var ids, code []string
	for _, line := range strings.Split(text[start+1:], "\n")[1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "| DA-H") || strings.HasPrefix(line, "| DAD ") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	for _, r := range clients.HeyAPIAuthContract {
		if r.Form == "" {
			t.Errorf("row %s names no form", r.ID)
		}
		code = append(code, r.ID)
	}
	if !reflect.DeepEqual(ids, code) {
		t.Errorf("task rows %v, code rows %v", ids, code)
	}
}

// TestHeyAPISecurityOracle compares the stored security attributes with
// the arrays @hey-api/openapi-ts 0.99.0 generates from the fixture spec,
// read with the TypeScript compiler (scripts/oracle/heyapi_security.sh).
// Opt-in, never run by CI: GRAPHVEIN_HEYAPI_ORACLE names the oracle's
// JSON output ({"METHOD url": security array}).
func TestHeyAPISecurityOracle(t *testing.T) {
	out := os.Getenv("GRAPHVEIN_HEYAPI_ORACLE")
	if out == "" {
		t.Skip("set GRAPHVEIN_HEYAPI_ORACLE (scripts/oracle/heyapi_security.sh)")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var oracle map[string]any
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(heyAuthFixture)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := indexHeyAuth(t, root)
	agree, disagree, seen := 0, 0, map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" || n.File != "client/sdk.gen.ts" {
			continue
		}
		method, _ := n.Attributes["method"].(string)
		var url string
		if paths, ok := n.Attributes["paths"].([]any); ok && len(paths) == 1 {
			url, _ = paths[0].(map[string]any)["template"].(string)
		}
		key := method + " " + url
		want, ok := oracle[key]
		seen[key] = true
		got := n.Attributes["security"]
		if !ok || !reflect.DeepEqual(normalize(want), normalize(got)) {
			disagree++
			t.Errorf("%s (%s): stored %v, generated %v", n.Identity, key, got, want)
			continue
		}
		agree++
	}
	for key := range oracle {
		if !seen[key] {
			t.Errorf("generated operation %s has no stored call", key)
		}
	}
	t.Logf("%d calls agree, %d disagree", agree, disagree)
}

// normalize makes a JSON value comparable (nil for an absent array).
func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}
