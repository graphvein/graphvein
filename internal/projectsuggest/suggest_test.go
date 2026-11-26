package projectsuggest

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const fixtureDir = "../../testdata/fixtures/projectsuggest"

// CopyFixture copies a suggestion fixture to a temporary directory and
// restores the inert env file names (`_env*` to `.env*`, fixture README).
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join(fixtureDir, name))); err != nil {
		t.Fatal(err)
	}
	RestoreEnvNames(t, root)
	return root
}

// RestoreEnvNames renames every `_env*` file under root to `.env*`.
func RestoreEnvNames(t *testing.T, root string) {
	t.Helper()
	var renames [][2]string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "_env") {
			renames = append(renames, [2]string{path, filepath.Join(filepath.Dir(path), "."+strings.TrimPrefix(d.Name(), "_"))})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range renames {
		if err := os.Rename(r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
}

// fixtureCases are the suggestion fixtures, one per reader family (B6)
// plus the define, service-name and stored-call checks (B6b).
var fixtureCases = []string{"env", "compose", "devserver", "define"}

// TestFixtureProposals is B6's reader gate: one fixture per reader family
// with a golden of every proposal, unmatched address and unread form.
// Every member checkout is indexed first, so proposals are checked against
// the consumers' stored calls (B6b). Suggest writes nothing: the fixture
// copy is byte-identical afterwards.
func TestFixtureProposals(t *testing.T) {
	for _, name := range fixtureCases {
		t.Run(name, func(t *testing.T) {
			root := copyFixture(t, name)
			p, err := config.LoadProject(root)
			if err != nil {
				t.Fatal(err)
			}
			calls := indexProject(t, p)
			before := snapshot(t, root)
			got, err := Suggest(context.Background(), p, calls)
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, filepath.Join(fixtureDir, "..", "projectsuggest."+name+".golden.json"), got)
			if after := snapshot(t, root); !equalSnapshots(before, after) {
				t.Fatal("Suggest changed files in the project")
			}
			// Every proposal's entry is a valid consumes entry: accepting it
			// yields a manifest that validates (unless already declared).
			manifest, err := os.ReadFile(filepath.Join(root, config.ProjectFile))
			if err != nil {
				t.Fatal(err)
			}
			for _, prop := range got.Proposals {
				data, err := config.AddProjectConsume(manifest, prop.Member, prop.Scope, prop.Consume())
				if err != nil {
					t.Fatalf("proposal %d: %v", prop.Number, err)
				}
				_, err = config.ParseProject(root, data, nil)
				if prop.Declared != (err != nil) {
					t.Errorf("proposal %d (declared %v): validation %v", prop.Number, prop.Declared, err)
				}
			}
		})
	}
}

// indexProject indexes every member checkout of p into a temporary SQLite
// store and reads the members' calls back, as graphvein index --project
// and project suggest do.
func indexProject(t *testing.T, p config.Project) Calls {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, m := range p.Members {
		if !m.Checkout {
			continue
		}
		var stream bytes.Buffer
		opt := index.Options{Repository: m.Repository, GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
		if _, err := index.Emit(ctx, m.Path, s, &stream, opt); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s); err != nil {
			t.Fatal(err)
		}
	}
	calls, err := ReadCalls(ctx, s, p)
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

// TestNoStoredIndex: without a stored index no call is known, so every
// matched address with a base is unmatched evidence (row SG-M5) that says
// to index the project first; proxy proposals are not checked.
func TestNoStoredIndex(t *testing.T) {
	root := copyFixture(t, "define")
	p, err := config.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Suggest(context.Background(), p, Calls{})
	if err != nil {
		t.Fatal(err)
	}
	for _, prop := range got.Proposals {
		if len(prop.Select.Bases) > 0 || len(prop.Select.Origins) > 0 {
			t.Errorf("proposal %d with a base or origin without a stored index: %+v", prop.Number, prop)
		}
	}
	n := 0
	for _, u := range got.Unmatched {
		if u.Row == RowNoCalls {
			n++
			if !strings.Contains(u.Reason, "has no stored index") {
				t.Errorf("reason %q", u.Reason)
			}
		}
	}
	if n == 0 {
		t.Fatal("no SG-M5 entry without a stored index")
	}
}

func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func equalSnapshots(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// TestContractTable keeps Rows, the table in docs/cli/project.md
// ("Suggestions") and the fixture goldens 1:1: every row is documented
// once, and every row except the default is exercised by a golden.
func TestContractTable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/cli/project.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, m := range regexp.MustCompile("(?m)^\\| `(SG-[A-Z0-9]+)` \\|").FindAllSubmatch(doc, -1) {
		documented = append(documented, string(m[1]))
	}
	var code []string
	for _, r := range Rows {
		code = append(code, r.ID)
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("docs/cli/project.md rows %v, code rows %v", documented, code)
	}
	used := map[string]bool{}
	for _, name := range fixtureCases {
		data, err := os.ReadFile(filepath.Join(fixtureDir, "..", "projectsuggest."+name+".golden.json"))
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		collectRows(v, used)
	}
	var missing []string
	for _, r := range Rows {
		if !used[r.ID] {
			missing = append(missing, r.ID)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("rows without a fixture: %v", missing)
	}
}

func collectRows(v any, used map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			if s, ok := item.(string); ok && k == "row" {
				used[s] = true
			}
			if list, ok := item.([]any); ok && k == "matches" {
				for _, m := range list {
					used[m.(string)] = true
				}
			}
			collectRows(item, used)
		}
	case []any:
		for _, item := range x {
			collectRows(item, used)
		}
	}
}
