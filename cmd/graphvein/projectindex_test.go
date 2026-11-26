package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const projectFixtureDir = "../../testdata/fixtures/projectlink"

// copyProjectFixture copies the project link fixture to a temporary
// directory, restores the inert spec name (fixture README) and writes
// manifest as its graphvein-project.yaml.
func copyProjectFixture(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(projectFixtureDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "frontend", "openapi.yaml.fixture"), filepath.Join(root, "frontend", "openapi.yaml")); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, root, manifest)
	return root
}

func writeManifest(t *testing.T, root, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectBackends runs check with graphvein pointed at a fresh SQLite
// store and at a fresh Postgres schema (skipped without
// GRAPHVEIN_TEST_POSTGRES_URL); open opens that store for reading.
func projectBackends(t *testing.T, check func(t *testing.T, open func() store.Store)) {
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.db")
		t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
		t.Setenv("GRAPHVEIN_SQLITE_PATH", path)
		check(t, func() store.Store {
			s, err := sqlite.OpenRead(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			return s
		})
	})
	t.Run("postgres", func(t *testing.T) {
		base := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
		unavailable := func() {
			if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
				t.Fatal("GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
			}
			t.Skip("Postgres unavailable: GRAPHVEIN_TEST_POSTGRES_URL unset or unreachable")
		}
		if base == "" {
			unavailable()
		}
		admin, err := egress.OpenPostgres(base, nil)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = admin.PingContext(ctx)
		cancel()
		if err != nil {
			unavailable()
		}
		schema := "graphvein_cli_project_" + strings.ToLower(rand.Text())
		if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}()
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		withSchema := u.String()
		t.Setenv("GRAPHVEIN_POSTGRES_URL", withSchema)
		t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "unused.db"))
		check(t, func() store.Store {
			s, err := postgres.OpenRead(context.Background(), withSchema)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			return s
		})
	})
}

// runIndex runs graphvein with the given emitter (nil: the real one) and
// returns exit code, stdout and stderr.
func runIndex(t *testing.T, emit func(context.Context, string, store.Store, io.Writer, index.Options) (index.Summary, error), args ...string) (int, string, string) {
	t.Helper()
	if emit == nil {
		emit = index.Emit
	}
	var out, errOut bytes.Buffer
	code := indexCommandWithEmitter(args[0], args[1:], &out, &errOut, emit)
	return code, out.String(), errOut.String()
}

func lines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// failingFor is an emitter that fails for the member checkout named dir.
func failingFor(dir string) func(context.Context, string, store.Store, io.Writer, index.Options) (index.Summary, error) {
	return func(ctx context.Context, root string, s store.Store, w io.Writer, opt index.Options) (index.Summary, error) {
		if filepath.Base(root) == dir {
			return index.Summary{}, errors.New("injected failure")
		}
		return index.Emit(ctx, root, s, w, opt)
	}
}

const twoManifest = "version: 1\nname: shop-two\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n"

// TestProjectIndexAndStatus is T022 B4's CLI gate on the project link
// fixture: index --project indexes both members and stores the same links
// and outcomes as the fixture's link pass golden; an unchanged re-index
// changes no link; re-indexing one member, a manifest change and a member
// that fails to index mark links stale; members without a checkout are
// linked from the store or reported. status --project outputs are golden.
func TestProjectIndexAndStatus(t *testing.T) {
	projectBackends(t, func(t *testing.T, open func() store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t, twoManifest)
		status := map[string][]string{}
		runStatus := func(name string) {
			t.Helper()
			code, out, errOut := runIndex(t, nil, "status", "--project", root)
			if code != 0 || errOut != "" {
				t.Fatalf("status %s = %d %s", name, code, errOut)
			}
			status[name] = lines(out)
		}
		runStatus("1-not-indexed")

		code, out, errOut := runIndex(t, nil, "index", "--project", filepath.Join(root, config.ProjectFile))
		if code != 0 || errOut != "" {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		if !strings.Contains(out, "frontend  frontend  indexed: files ") || !strings.Contains(out, "Link pass 1: ") || !strings.Contains(out, "(added ") {
			t.Fatalf("index output:\n%s", out)
		}
		s := open()
		set, err := s.ProjectLinks(ctx, "shop-two")
		if err != nil {
			t.Fatal(err)
		}
		checkAgainstPassGolden(t, set, "two")
		runStatus("2-fresh")

		// An unchanged re-index gives every member a new run but changes no
		// link: the journal counts them unchanged, the stored runs are new.
		code, out, errOut = runIndex(t, nil, "index", "--project", root)
		if code != 0 || !strings.Contains(out, "Link pass 2: ") || !strings.Contains(out, "(added 0, removed 0, changed 0, unchanged ") {
			t.Fatalf("unchanged re-index = %d\n%s\n%s", code, out, errOut)
		}
		runs, err := s.ProjectLinkRuns(ctx, "shop-two")
		if err != nil || len(runs) != 2 || runs[1].Added+runs[1].Removed+runs[1].Changed != 0 || runs[0].Members[0].Run == runs[1].Members[0].Run {
			t.Fatalf("link journal after an unchanged re-index = %+v %v", runs, err)
		}
		again, err := s.ProjectLinks(ctx, "shop-two")
		if err != nil || len(again.Links) != len(set.Links) || again.Links[0].Source.Run != runs[1].Members[0].Run {
			t.Fatalf("links not rewritten with the new runs: %v", err)
		}

		// Re-indexing one member alone (single-repository index into the
		// same store) marks the links touching it stale.
		if code, out, errOut := runIndex(t, nil, "index", filepath.Join(root, "backend"), "--repository", "backend"); code != 0 {
			t.Fatalf("index backend = %d %s %s", code, out, errOut)
		}
		runStatus("3-member-reindexed")

		// A manifest change marks every link stale until the next pass.
		writeManifest(t, root, twoManifest+"    consumes: [frontend]\n")
		runStatus("4-manifest-changed")
		writeManifest(t, root, twoManifest)

		// A member failing to index stops nothing: the other is indexed,
		// the pass links the failed one from its previous index and stores
		// the failure, so status shows its links stale.
		code, out, errOut = runIndex(t, failingFor("backend"), "index", "--project", root)
		if code != 1 || !strings.Contains(out, "backend   backend   index failed: injected failure; linked from its previous index, its links are stale") || !strings.Contains(out, "frontend  frontend  indexed: ") || !strings.Contains(out, "Link pass 3: ") || !strings.Contains(errOut, "some members failed") {
			t.Fatalf("index with a failing member = %d\n%s\n%s", code, out, errOut)
		}
		runStatus("5-member-failed")

		// Members without a checkout: backend is linked from the store,
		// billing (never indexed) is reported and the calls that could
		// reach it are incomplete.
		writeManifest(t, root, "version: 1\nname: shop-two\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n  - repository: billing\n")
		code, out, errOut = runIndex(t, nil, "index", "--project", root)
		if code != 0 || !strings.Contains(out, "backend   (no path)  no checkout here; linked from the store") || !strings.Contains(out, "billing   (no path)  no checkout here and not indexed") || !strings.Contains(out, "incomplete ") {
			t.Fatalf("index without checkouts = %d\n%s\n%s", code, out, errOut)
		}
		runStatus("6-no-checkouts")

		golden.Check(t, filepath.Join(projectFixtureDir, "..", "projectlink.cli-status.golden.json"), status)
	})
}

// checkAgainstPassGolden compares the links and outcomes the CLI stored
// with the fixture's link pass golden of scenario (internal/projectlink):
// the CLI path must link exactly as the pass does.
func checkAgainstPassGolden(t *testing.T, set store.ProjectLinkSet, scenario string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectFixtureDir, "..", "projectlink."+scenario+".golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Links    json.RawMessage `json:"links"`
		Outcomes json.RawMessage `json:"outcomes"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	links := []store.ProjectLink{}
	for _, l := range set.Links {
		l.Source.Run = "<run of " + l.Source.Repository + ">"
		l.Target.Run = "<run of " + l.Target.Repository + ">"
		links = append(links, l)
	}
	if len(links) == 0 {
		t.Fatal("the CLI stored no links")
	}
	for _, c := range []struct {
		name      string
		got, want any
	}{{"links", links, want.Links}, {"outcomes", set.Outcomes, want.Outcomes}} {
		got, err := json.Marshal(c.got)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.want.(json.RawMessage), &b); err != nil {
			t.Fatal(err)
		}
		ga, _ := json.Marshal(a)
		gb, _ := json.Marshal(b)
		if string(ga) != string(gb) {
			t.Fatalf("CLI %s differ from the pass golden:\n%s\nwant\n%s", c.name, ga, gb)
		}
	}
}

// TestProjectIndexLocked holds the SQLite store lock: every member and the
// link pass report it, nothing is written, and the command exits 1.
func TestProjectIndexLocked(t *testing.T) {
	root := copyProjectFixture(t, twoManifest)
	path := filepath.Join(t.TempDir(), "index.db")
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", path)
	locker := sqlite.IndexLocker(path)
	defer locker.Close()
	lease, err := locker.Acquire(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	code, out, errOut := runIndex(t, nil, "index", "--project", root)
	if code != 1 || strings.Count(out, "another index run holds the lock") != 2 || !strings.Contains(out, "Link pass failed: another run holds the project lock") || !strings.Contains(errOut, "link pass of project shop-two failed") {
		t.Fatalf("locked index --project = %d\n%s\n%s", code, out, errOut)
	}
}

func TestProjectIndexUsage(t *testing.T) {
	root := copyProjectFixture(t, twoManifest)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	for _, args := range [][]string{
		{"index", "--project", root, "--repository", "x"},
		{"index", "--project", root, "--emit", "x.ndjson"},
		{"index", "--project", root, root},
		{"status", "--project", filepath.Join(root, "frontend", "api.ts")},
		{"status", "--project", filepath.Join(root, "missing")},
	} {
		if code, out, errOut := runIndex(t, nil, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d %q %q", args, code, out, errOut)
		}
	}
}

// TestProjectStatusSettings is row ST-47: status --project words each
// member's settings comparison: unchanged after an index, changed (with
// the key) after an index input changes in the member's graphvein.yaml,
// unchanged again when the change is undone; an at-once key changes
// nothing. (A member file that does not load stops status --project
// before any row; a member without a checkout is unknown, in the golden
// of TestProjectIndexAndStatus.)
func TestProjectStatusSettings(t *testing.T) {
	projectBackends(t, func(t *testing.T, _ func() store.Store) {
		root := copyProjectFixture(t, twoManifest)
		if code, out, errOut := runIndex(t, nil, "index", "--project", root); code != 0 {
			t.Fatalf("index --project = %d\n%s\n%s", code, out, errOut)
		}
		row := func(member string) string {
			t.Helper()
			code, out, errOut := runIndex(t, nil, "status", "--project", root)
			if code != 0 || errOut != "" {
				t.Fatalf("status = %d %s", code, errOut)
			}
			for _, l := range lines(out) {
				if strings.HasPrefix(strings.TrimSpace(l), member+" ") {
					return l
				}
			}
			t.Fatalf("no row for %s in\n%s", member, out)
			return ""
		}
		yaml := filepath.Join(root, "backend", config.File)
		write := func(text string) {
			t.Helper()
			if err := os.WriteFile(yaml, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		base, err := os.ReadFile(yaml)
		if os.IsNotExist(err) {
			base, err = []byte("version: 1\nrepository: backend\n"), nil
		}
		if err != nil {
			t.Fatal(err)
		}
		if r := row("backend"); !strings.Contains(r, "; settings unchanged since its last index") {
			t.Fatalf("after index: %q", r)
		}
		write(string(base) + "ui:\n  labels: technical\n")
		if r := row("backend"); !strings.Contains(r, "; settings unchanged since its last index") {
			t.Fatalf("an at-once key changed the comparison: %q", r)
		}
		write(string(base) + "index:\n  shape_depth: 3\n")
		if r := row("backend"); !strings.Contains(r, "; settings changed since its last index: index.shape_depth (re-index to apply)") {
			t.Fatalf("after a shape depth change: %q", r)
		}
		if r := row("frontend"); !strings.Contains(r, "; settings unchanged since its last index") {
			t.Fatalf("frontend: %q", r)
		}
		write(string(base))
		if r := row("backend"); !strings.Contains(r, "; settings unchanged since its last index") {
			t.Fatalf("after the change was undone: %q", r)
		}
	})
}
