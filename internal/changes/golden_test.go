package changes_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const fixtureDir = "../../testdata/fixtures/change-impact"

// backend is one store under test: open opens a writer on the same data,
// locker returns its index lock.
type backend struct {
	name   string
	open   func(context.Context) (store.Store, error)
	locker func() store.IndexLocker
}

// backends runs check on SQLite and, when GRAPHVEIN_TEST_POSTGRES_URL is
// set, on a fresh Postgres schema (CI's postgres job requires it).
func backends(t *testing.T, check func(t *testing.T, b backend)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.db")
		check(t, backend{name: "sqlite",
			open:   func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) },
			locker: func() store.IndexLocker { return sqlite.IndexLocker(path) }})
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
		schema := "graphvein_changes_" + strings.ToLower(rand.Text())
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
		check(t, backend{name: "postgres",
			open:   func(ctx context.Context) (store.Store, error) { return postgres.Open(ctx, withSchema) },
			locker: func() store.IndexLocker { return postgres.IndexLocker(withSchema, "fixture") }})
	})
}

// copyTree replaces everything in dst except .git with src's files.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(dst)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func indexInto(t *testing.T, ctx context.Context, s store.Store, root, cache string) {
	t.Helper()
	var stream bytes.Buffer
	opt := index.Options{Repository: "fixture", Parser: parse.Options{Workers: 1, CacheDir: cache}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Stream(ctx, &stream, s); err != nil {
		t.Fatal(err)
	}
}

func ingestFile(t *testing.T, ctx context.Context, s store.Store, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := ingest.Stream(ctx, f, s); err != nil {
		t.Fatal(err)
	}
}

func dump(t *testing.T, ctx context.Context, s store.Store, repo string) store.Snapshot {
	t.Helper()
	d, err := s.Dump(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// checkRaw is the mini oracle: the comparison's raw change set must equal
// the key-wise diff of the two Dumps.
func checkRaw(t *testing.T, name string, c *changes.Comparison, before, after store.Snapshot) {
	t.Helper()
	want, err := store.DiffSnapshots(before, after)
	if err != nil {
		t.Fatal(err)
	}
	got := c.ChangeSet().Changes
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if !bytes.Equal(a, b) {
		t.Errorf("%s: raw change set (%d) differs from the Dump diff (%d)", name, len(got), len(want))
	}
}

var shaPattern = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// normalise replaces what differs between runs and backends: run ids,
// finish times and commit ids, numbered in order of appearance.
func normalise(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	label := func(kind, value string) string {
		if value == "" {
			return value
		}
		if n, ok := names[value]; ok {
			return n
		}
		count := 0
		for _, n := range names {
			if strings.HasPrefix(n, "<"+kind+"-") {
				count++
			}
		}
		names[value] = fmt.Sprintf("<%s-%d>", kind, count+1)
		return names[value]
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				s, isString := x[k].(string)
				switch {
				case isString && (k == "run" || k == "run_id" || k == "previous_run"):
					x[k] = label("run", s)
				case isString && k == "finished_at":
					x[k] = label("time", s)
				default:
					x[k] = walk(x[k])
				}
			}
			return x
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		case string:
			return shaPattern.ReplaceAllStringFunc(x, func(sha string) string { return label("commit", sha) })
		}
		return v
	}
	return walk(doc)
}

// report builds the golden document and checks the classification
// invariants (oracle step 5) on the comparison and every traversal.
func report(t *testing.T, ctx context.Context, s store.Store, c *changes.Comparison, seeds ...string) changes.Report {
	t.Helper()
	r, err := changes.BuildReport(ctx, s, c, changes.Filter{IncludeTests: true}, seeds)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range c.Violations() {
		t.Errorf("%s: %s", c.Base, v)
	}
	for _, im := range r.Impacts {
		for _, v := range changes.ImpactViolations(im.Impact) {
			t.Errorf("%s impact of %s: %s", c.Base, im.Seed, v)
		}
	}
	return r
}

func compare(t *testing.T, ctx context.Context, s store.Store, base string, root string) *changes.Comparison {
	t.Helper()
	b, err := changes.ParseBase(base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := changes.Compare(ctx, s, "fixture", b, changes.Options{Root: root, ExtractorVersion: index.Version, Schema: server.ChangeSchema{}})
	if err != nil {
		t.Fatalf("comparing with %s: %v", base, err)
	}
	return c
}

// goldenCases are the golden files of the fixture, by case name.
var goldenCases = []string{"previous-rev1-rev2", "previous-rev2-rev3", "pin-rev1-rev3", "ref-rev1-rev3", "extractor-upgrade"}

func goldenPath(name string) string {
	return filepath.Join("..", "..", "testdata", "fixtures", "change-impact."+name+".golden.json")
}

// TestChangeImpactGoldens indexes the fixture's three revisions (and the
// two NDJSON streams) on both backends and checks every comparison case
// against its golden, after checking the raw change set against a Dump
// diff (mini oracle).
func TestChangeImpactGoldens(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		got := map[string]any{}

		copyTree(t, filepath.Join(fixtureDir, "rev1"), root)
		indexInto(t, ctx, s, root, cache)
		d1 := dump(t, ctx, s, "fixture")
		runs, err := s.ChangeRuns(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pin(ctx, "fixture", runs[len(runs)-1].Seq, "rev1", 0); err != nil {
			t.Fatal(err)
		}

		copyTree(t, filepath.Join(fixtureDir, "rev2"), root)
		indexInto(t, ctx, s, root, cache)
		d2 := dump(t, ctx, s, "fixture")
		c := compare(t, ctx, s, "previous", root)
		checkRaw(t, "previous-rev1-rev2", c, d1, d2)
		got["previous-rev1-rev2"] = report(t, ctx, s, c,
			"function:server/items.ts#formatRow",
			"function:server/handlers.ts#listItems",
			"route:server/router.ts#GET%20%2Fitems%2F%3Aid",
			"ipc_command:src-tauri/src/main.rs#main.COMMAND%20farewell",
			"column:db/migrations/0003_item_price.sql#items.price",
			"column:models/app/models.py#Hero.age")

		copyTree(t, filepath.Join(fixtureDir, "rev3"), root)
		indexInto(t, ctx, s, root, cache)
		d3 := dump(t, ctx, s, "fixture")
		c = compare(t, ctx, s, "previous", root)
		checkRaw(t, "previous-rev2-rev3", c, d2, d3)
		got["previous-rev2-rev3"] = report(t, ctx, s, c, "class:ui/child.ts#ChildComponent", "table:db/migrations/0001_items.sql#items",
			"table:models/app/models.py#Hero", "function:react/src/article.tsx#ArticleContent",
			"function:react/src/loader.ts#articleLoader")
		c = compare(t, ctx, s, "pin:rev1", root)
		checkRaw(t, "pin-rev1-rev3", c, d1, d3)
		got["pin-rev1-rev3"] = report(t, ctx, s, c, "function:server/items.ts#formatRow")

		// A git repository of rev1 and rev3 in the working tree: ref
		// comparisons materialise both commits.
		copyTree(t, filepath.Join(fixtureDir, "rev1"), root)
		git(t, root, "init", "-q")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "rev1")
		git(t, root, "tag", "rev1")
		copyTree(t, filepath.Join(fixtureDir, "rev3"), root)
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "rev3")
		git(t, root, "tag", "rev3")
		locker := b.locker()
		defer locker.Close()
		lease, err := locker.Acquire(ctx, indexrun.LockWait)
		if err != nil {
			t.Fatal(err)
		}
		opt := indexrun.Options{Root: root, Repository: "fixture", Lease: lease, OpenStore: b.open,
			Parser: parse.Options{Workers: 1, CacheDir: cache}, MaxFileSize: indexrun.DefaultMaxFileSize}
		var namespaces []string
		for _, tag := range []string{"rev1", "rev3"} {
			r, err := indexrun.RunRef(ctx, indexrun.RefOptions{Options: opt, Commit: git(t, root, "rev-parse", tag)})
			if err != nil {
				t.Fatal(err)
			}
			namespaces = append(namespaces, r.Namespace)
		}
		lease.Release()
		c = compare(t, ctx, s, "ref:rev1..rev3", root)
		checkRaw(t, "ref-rev1-rev3", c, dump(t, ctx, s, namespaces[0]), dump(t, ctx, s, namespaces[1]))
		got["ref-rev1-rev3"] = report(t, ctx, s, c, "route:server/app.ts#DELETE%20%2Fapi%2Flegacy")

		// Two NDJSON streams of another repository with different indexer
		// versions: unchanged files with changed facts are
		// extractor_or_relink, changed files code.
		ingestFile(t, ctx, s, filepath.Join(fixtureDir, "streams", "v1.ndjson"))
		s1 := dump(t, ctx, s, "streams")
		ingestFile(t, ctx, s, filepath.Join(fixtureDir, "streams", "v2.ndjson"))
		s2 := dump(t, ctx, s, "streams")
		sb, _ := changes.ParseBase("previous")
		c, err = changes.Compare(ctx, s, "streams", sb, changes.Options{Schema: server.ChangeSchema{}})
		if err != nil {
			t.Fatal(err)
		}
		checkRaw(t, "extractor-upgrade", c, s1, s2)
		got["extractor-upgrade"] = report(t, ctx, s, c, "route:src/b.ts#GET%20%2Fpets", "api_operation:api/spec.yaml#GET%20%2Fpets")

		for _, name := range goldenCases {
			golden.Check(t, goldenPath(name), normalise(t, got[name]))
		}
	})
}

// TestLineEndingConversion: a working tree checked out with CRLF line
// endings (Windows core.autocrlf) indexes file records whose size and git
// blob id differ from the committed LF blob's, while the LF-normalised
// content hash is equal. A ref comparison of HEAD against such a current
// index lists no change: the file records are counted as
// file_metadata_only, never as changed files or as cause code (T019a2
// decision).
func TestLineEndingConversion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	backends(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		s, err := b.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		root := filepath.Join(t.TempDir(), "repo")
		cache := filepath.Join(t.TempDir(), "cache")
		lf := "export function one() {\n  return 1;\n}\n\nexport function two() {\n  return one();\n}\n"
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte(lf), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, root, "init", "-q")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "lf")
		crlf := strings.ReplaceAll(lf, "\n", "\r\n")
		if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte(crlf), 0o644); err != nil {
			t.Fatal(err)
		}
		// indexrun.Run records the commit ref:HEAD takes its merge base with.
		indexRun(t, ctx, b, root, cache)
		indexRefs(t, ctx, b, root, cache, git(t, root, "rev-parse", "HEAD"))
		c := compare(t, ctx, s, "ref:HEAD", root)
		raw := c.ChangeSet().Changes
		if len(raw) == 0 {
			t.Fatal("the raw change set has no file record difference; the test does not exercise the conversion")
		}
		for _, rc := range raw {
			if rc.Record != store.RecordFile {
				t.Errorf("raw change outside file records: %s %s", rc.Record, rc.Key)
			}
		}
		if items := c.AllItems(); len(items) != 0 || c.Summary.FileMetadataOnly != 1 {
			t.Errorf("items = %+v, file_metadata_only = %d; want none and 1", items, c.Summary.FileMetadataOnly)
		}
	})
}
