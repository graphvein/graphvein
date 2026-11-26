package projectlink_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const fixtureDir = "../../testdata/fixtures/projectlink"

// fixtureMembers are the fixture's member folders, each indexed as its own
// repository named after the folder.
var fixtureMembers = []string{"frontend", "backend", "backend2", "monorepo"}

// inertSpecs are the fixture's OpenAPI specs stored under an inert name
// (README.md: openapi.yaml.fixture -> openapi.yaml), so the repository's
// own self-index sees no spec outside the OpenAPI fixtures.
var inertSpecs = []string{"frontend", "monorepo/admin"}

// copyFixture copies the fixture to a temporary directory and restores the
// inert spec names.
func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixtureDir)); err != nil {
		t.Fatal(err)
	}
	for _, dir := range inertSpecs {
		if err := os.Rename(filepath.Join(root, dir, "openapi.yaml.fixture"), filepath.Join(root, dir, "openapi.yaml")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// indexMember indexes dir as repository into s, as graphvein index would.
func indexMember(t *testing.T, s store.Store, dir, repository string) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repository, GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
	if _, err := index.Emit(ctx, dir, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s); err != nil {
		t.Fatal(err)
	}
}

// loadScenario parses scenarios/<name>.yaml as a manifest in root.
func loadScenario(t *testing.T, root, name string) config.Project {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "scenarios", name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := config.ParseProject(root, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return project
}

// goldenView is what a scenario golden records: links with runs replaced
// by the member name (runs are random), and every outcome.
type goldenView struct {
	Members  []store.ProjectMember `json:"members"`
	Links    []store.ProjectLink   `json:"links"`
	Outcomes []projectlink.Outcome `json:"outcomes"`
}

func view(result projectlink.Result) goldenView {
	v := goldenView{Members: []store.ProjectMember{}, Links: []store.ProjectLink{}, Outcomes: result.Outcomes}
	runName := func(repository, run string) string {
		if run == "" {
			return ""
		}
		return "<run of " + repository + ">"
	}
	for _, m := range result.Set.Members {
		v.Members = append(v.Members, store.ProjectMember{Repository: m.Repository, Run: runName(m.Repository, m.Run)})
	}
	for _, l := range result.Set.Links {
		l.Source.Run = runName(l.Source.Repository, l.Source.Run)
		l.Target.Run = runName(l.Target.Repository, l.Target.Run)
		v.Links = append(v.Links, l)
	}
	return v
}

// TestFixtureScenarios indexes the three fixture members into one SQLite
// store, runs the link pass of each scenario manifest, checks the result
// against its golden and reads the stored set back through the B2 store
// methods.
func TestFixtureScenarios(t *testing.T) {
	ctx := context.Background()
	root := copyFixture(t)
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, m := range fixtureMembers {
		indexMember(t, s, filepath.Join(root, m), m)
	}
	rows := map[string]bool{}
	for _, name := range []string{"two", "three", "consumes", "selector", "unindexed", "scopes", "monorepo", "monorepo-default", "monorepo-default-scope"} {
		t.Run(name, func(t *testing.T) {
			project := loadScenario(t, root, name)
			result, run, err := projectlink.Run(ctx, s, project, "sha256:"+name)
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, filepath.Join(fixtureDir, "..", "projectlink."+name+".golden.json"), view(result))
			if run.Added != len(result.Set.Links) || run.Removed != 0 || run.Changed != 0 {
				t.Fatalf("first pass journal: %+v", run)
			}
			stored, err := s.ProjectLinks(ctx, project.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := jsonText(t, stored.Links), jsonText(t, result.Set.Links); got != want {
				t.Fatalf("stored links differ:\n%s\nwant\n%s", got, want)
			}
			// Outcomes are stored with the links (T022 B4).
			if got, want := jsonText(t, stored.Outcomes), jsonText(t, result.Outcomes); got != want {
				t.Fatalf("stored outcomes differ:\n%s\nwant\n%s", got, want)
			}
			// A second pass over unchanged members changes nothing.
			again, run2, err := projectlink.Run(ctx, s, project, "sha256:"+name)
			if err != nil {
				t.Fatal(err)
			}
			if jsonText(t, again.Set.Links) != jsonText(t, result.Set.Links) || run2.Added+run2.Removed+run2.Changed != 0 {
				t.Fatalf("second pass differs: %+v", run2)
			}
			for _, o := range result.Outcomes {
				rows[o.Row] = true
			}
		})
	}
	t.Logf("rows reached by the fixture: %d", len(rows))
}
