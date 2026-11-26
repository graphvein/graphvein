package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// projectFixture indexes two member repositories and returns their runs.
func projectFixture(t *testing.T, s store.Store) (front, back store.Run) {
	t.Helper()
	front, _ = mustWrite(t, s, "front", baseGraph())
	back, _ = mustWrite(t, s, "back", baseGraph())
	return front, back
}

// projectLink links node source of front to node target of back, as the
// runs recorded them.
func projectLink(front, back store.Run, source, target contract.Node) store.ProjectLink {
	return store.ProjectLink{
		Kind:       "calls_route",
		Source:     store.ProjectEndpoint{Repository: "front", Identity: source.Identity, Run: front.ID, ContentHash: source.ContentHash},
		Target:     store.ProjectEndpoint{Repository: "back", Identity: target.Identity, Run: back.ID, ContentHash: target.ContentHash},
		Provenance: "inferred",
		Confidence: 0.7,
		Evidence:   []contract.Evidence{{File: source.File, Span: contract.Span{StartLine: 1, EndLine: 1, EndColumn: 3}}},
		Assumption: "matched across project repositories; base URL not proven",
		Attributes: map[string]any{"tier": "exact"},
	}
}

func projectSet(front, back store.Run, links ...store.ProjectLink) store.ProjectLinkSet {
	return store.ProjectLinkSet{
		Project:      "app",
		ManifestHash: "sha256:manifest",
		Members:      []store.ProjectMember{{Repository: "front", Run: front.ID}, {Repository: "back", Run: back.ID}},
		Links:        links,
	}
}

// projectRows counts the rows of the four project tables, so a refused
// write can be shown to have written nothing.
func projectRows(t *testing.T, db *sql.DB) string {
	t.Helper()
	counts := []string{}
	for _, table := range []string{"projects", "project_members", "project_links", "project_link_runs", "project_outcomes"} {
		counts = append(counts, fmt.Sprintf("%s=%d", table, tableCount(t, db, "SELECT COUNT(*) FROM "+table)))
	}
	return strings.Join(counts, " ")
}

// linkJSON compares link sets by their encoding: decoded attribute numbers
// are json.Number, so reflect.DeepEqual would compare types, not values.
func linkJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProjectRepositories(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		repos, err := s.Repositories(ctx)
		if err != nil || len(repos) != 0 {
			t.Fatalf("empty store repositories = %+v %v", repos, err)
		}
		front, back := projectFixture(t, s)
		// A ref snapshot is not a repository of its own.
		mustWrite(t, s, store.RefNamespace("back", "c1"), baseGraph(), refOptions("back", "c1", 0))
		time.Sleep(2 * time.Millisecond)
		again, _ := mustWrite(t, s, "front", baseGraph())
		repos, err = s.Repositories(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, r := range repos {
			if r.Latest == nil {
				t.Fatalf("%s has no latest run", r.ID)
			}
			got = append(got, r.ID+"@"+r.Latest.ID)
		}
		want := []string{"back@" + back.ID, "front@" + again.ID}
		if !reflect.DeepEqual(got, want) || front.ID == again.ID {
			t.Fatalf("repositories = %v, want %v", got, want)
		}
	})
}

func TestProjectLinksReplace(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		if _, err := s.ProjectLinks(ctx, "app"); !errors.Is(err, store.ErrProjectUnknown) {
			t.Fatalf("unknown project = %v", err)
		}
		if runs, err := s.ProjectLinkRuns(ctx, "app"); err != nil || len(runs) != 0 {
			t.Fatalf("unknown project runs = %+v %v", runs, err)
		}
		front, back := projectFixture(t, s)
		g := baseGraph()
		a, b, c := g.nodes[0], g.nodes[1], g.nodes[3]
		l1, l2 := projectLink(front, back, a, b), projectLink(front, back, a, c)
		l2.Evidence = nil
		// A link to a Next.js route keeps its row assumptions (2026-11-18
		// TOP-1): the payload round-trips them on both backends.
		l2.Assumes = []string{"NX-V1", "NX-V3"}

		// First pass: two links, given out of key order.
		set := projectSet(front, back, l2, l1)
		run, err := s.ReplaceProjectLinks(ctx, set)
		if err != nil {
			t.Fatal(err)
		}
		if run.Seq != 1 || run.Added != 2 || run.Removed+run.Changed != 0 || len(run.Links) != 2 || run.ManifestHash != "sha256:manifest" {
			t.Fatalf("first pass = %+v", run)
		}
		stored, err := s.ProjectLinks(ctx, "app")
		if err != nil {
			t.Fatal(err)
		}
		l2.Evidence = []contract.Evidence{}
		want := projectSet(front, back, l1, l2)
		if l2.Key().Less(l1.Key()) {
			want.Links = []store.ProjectLink{l2, l1}
		}
		want.Pass = 1
		if linkJSON(t, stored) != linkJSON(t, want) {
			t.Fatalf("stored set\n got %s\nwant %s", linkJSON(t, stored), linkJSON(t, want))
		}
		if !stored.Links[0].Key().Less(stored.Links[1].Key()) {
			t.Fatal("links are not in key order")
		}

		// The same pass again changes no link row but records a pass.
		if _, err := db.ExecContext(ctx, "UPDATE project_links SET payload=payload"); err != nil {
			t.Fatal(err)
		}
		run, err = s.ReplaceProjectLinks(ctx, set)
		if err != nil || run.Seq != 2 || run.Added+run.Removed+run.Changed != 0 {
			t.Fatalf("unchanged pass = %+v %v", run, err)
		}

		// Changed, removed and added links.
		l1.Confidence = 0.6
		l3 := projectLink(front, back, b, a)
		run, err = s.ReplaceProjectLinks(ctx, projectSet(front, back, l1, l3))
		if err != nil || run.Seq != 3 || run.Added != 1 || run.Removed != 1 || run.Changed != 1 {
			t.Fatalf("third pass = %+v %v", run, err)
		}
		stored, err = s.ProjectLinks(ctx, "app")
		if err != nil || len(stored.Links) != 2 || stored.Pass != 3 {
			t.Fatalf("after third pass = %+v %v", stored, err)
		}
		for _, l := range stored.Links {
			if l.Key() == l1.Key() && l.Confidence != 0.6 {
				t.Fatalf("changed link not stored: %+v", l)
			}
		}

		// Removing a member removes it from the project; a link pass with no
		// links (every call ambiguous) is a valid pass.
		lone := store.ProjectLinkSet{Project: "app", ManifestHash: "sha256:other", Members: []store.ProjectMember{{Repository: "front", Run: front.ID}}}
		run, err = s.ReplaceProjectLinks(ctx, lone)
		if err != nil || run.Removed != 2 || len(run.Links) != 0 {
			t.Fatalf("one-member pass = %+v %v", run, err)
		}
		stored, err = s.ProjectLinks(ctx, "app")
		if err != nil || len(stored.Links) != 0 || len(stored.Members) != 1 || stored.ManifestHash != "sha256:other" {
			t.Fatalf("one-member project = %+v %v", stored, err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM project_members"); n != 1 {
			t.Fatalf("project_members rows = %d", n)
		}

		runs, err := s.ProjectLinkRuns(ctx, "app")
		if err != nil || len(runs) != 4 || runs[0].Seq != 1 || runs[3].Seq != 4 || len(runs[2].Links) != 2 {
			t.Fatalf("link journal = %+v %v", runs, err)
		}
		if runs[0].Members[1].Run != back.ID || runs[0].Links[0].PayloadHash == "" {
			t.Fatalf("journal pass = %+v", runs[0])
		}

		// Member snapshots are untouched by everything above.
		for _, repo := range []string{"front", "back"} {
			d, err := s.Dump(ctx, repo)
			if err != nil || len(d.Nodes) != len(g.nodes) || len(d.Edges) != len(g.edges) {
				t.Fatalf("%s snapshot changed: %v", repo, err)
			}
		}
	})
}

func TestProjectLinksStale(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		g := baseGraph()
		a, b := g.nodes[0], g.nodes[1]
		if _, err := s.ReplaceProjectLinks(ctx, projectSet(front, back, projectLink(front, back, a, b))); err != nil {
			t.Fatal(err)
		}
		before := projectRows(t, db)
		refused := func(name string, set store.ProjectLinkSet) {
			t.Helper()
			if _, err := s.ReplaceProjectLinks(ctx, set); !errors.Is(err, store.ErrProjectStale) {
				t.Fatalf("%s = %v, want ErrProjectStale", name, err)
			}
			if now := projectRows(t, db); now != before {
				t.Fatalf("%s wrote rows: %s -> %s", name, before, now)
			}
		}

		// A link whose node is stored with another hash, or not at all.
		changed := projectLink(front, back, a, b)
		changed.Target.ContentHash = "sha256:old"
		refused("changed node", projectSet(front, back, changed))
		missing := projectLink(front, back, a, gnode("z.ts", "z", nil))
		refused("missing node", projectSet(front, back, missing))

		// A member indexed after the pass read it.
		time.Sleep(2 * time.Millisecond)
		g2 := baseGraph()
		g2.nodes[1] = gnode("b.ts", "b", map[string]any{"v": 2})
		newBack, _ := mustWrite(t, s, "back", g2)
		refused("re-indexed member", projectSet(front, back, projectLink(front, back, a, b)))
		// A member recorded as not indexed while it is.
		unindexed := projectSet(front, newBack)
		unindexed.Members[0].Run = ""
		refused("member recorded without run", unindexed)
		// A member recorded with a run while it has none.
		ghost := projectSet(front, newBack)
		ghost.Members = append(ghost.Members, store.ProjectMember{Repository: "ghost", Run: "R"})
		refused("member without index", ghost)

		// The pass that read the new run is accepted; a member without any
		// index takes part with an empty run. b's content hash is unchanged,
		// so the link differs only in its target's run: it is counted
		// unchanged (T022 B4) but rewritten with the new run.
		set := projectSet(front, newBack, projectLink(front, newBack, a, g2.nodes[1]))
		set.Members = append(set.Members, store.ProjectMember{Repository: "ghost"})
		run, err := s.ReplaceProjectLinks(ctx, set)
		if err != nil || run.Added+run.Removed+run.Changed != 0 {
			t.Fatalf("fresh pass = %+v %v", run, err)
		}
		stored, err := s.ProjectLinks(ctx, "app")
		if err != nil || len(stored.Links) != 1 || stored.Links[0].Target.Run != newBack.ID {
			t.Fatalf("link not rewritten with the new run: %+v %v", stored, err)
		}
		runs, err := s.ProjectLinkRuns(ctx, "app")
		if err != nil || len(runs) != 2 || runs[0].Links[0].PayloadHash != runs[1].Links[0].PayloadHash {
			t.Fatalf("journal hashes differ for a link changed only in its runs: %+v %v", runs, err)
		}
	})
}

func TestProjectLinksInvalid(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		g := baseGraph()
		a, b := g.nodes[0], g.nodes[1]
		ok := func() store.ProjectLink { return projectLink(front, back, a, b) }
		withLink := func(edit func(*store.ProjectLink)) store.ProjectLinkSet {
			l := ok()
			edit(&l)
			return projectSet(front, back, l)
		}
		cases := map[string]store.ProjectLinkSet{
			"empty project":       func() store.ProjectLinkSet { x := projectSet(front, back); x.Project = ""; return x }(),
			"ref project":         func() store.ProjectLinkSet { x := projectSet(front, back); x.Project = "a@ref:b"; return x }(),
			"no manifest hash":    func() store.ProjectLinkSet { x := projectSet(front, back); x.ManifestHash = ""; return x }(),
			"no members":          {Project: "app", ManifestHash: "h"},
			"duplicate member":    func() store.ProjectLinkSet { x := projectSet(front, back); x.Members[1].Repository = "front"; return x }(),
			"NUL member":          func() store.ProjectLinkSet { x := projectSet(front, back); x.Members[1].Repository = "b\x00"; return x }(),
			"duplicate link":      projectSet(front, back, ok(), ok()),
			"non-member endpoint": withLink(func(l *store.ProjectLink) { l.Target.Repository = "other" }),
			"empty identity":      withLink(func(l *store.ProjectLink) { l.Source.Identity = "" }),
			"endpoint without run": withLink(func(l *store.ProjectLink) {
				l.Source.Run = ""
			}),
			"endpoint run not the member's": withLink(func(l *store.ProjectLink) { l.Target.Run = front.ID }),
			"no content hash":               withLink(func(l *store.ProjectLink) { l.Target.ContentHash = "" }),
			"empty kind":                    withLink(func(l *store.ProjectLink) { l.Kind = "" }),
			"no provenance":                 withLink(func(l *store.ProjectLink) { l.Provenance = "" }),
			"zero confidence":               withLink(func(l *store.ProjectLink) { l.Confidence = 0 }),
			"confidence above 1":            withLink(func(l *store.ProjectLink) { l.Confidence = 1.5 }),
			"NaN confidence":                withLink(func(l *store.ProjectLink) { l.Confidence = math.NaN() }),
			"no assumption":                 withLink(func(l *store.ProjectLink) { l.Assumption = " " }),
		}
		for name, set := range cases {
			if _, err := s.ReplaceProjectLinks(ctx, set); !errors.Is(err, store.ErrInvalid) {
				t.Errorf("%s = %v, want ErrInvalid", name, err)
			}
		}
		if rows := projectRows(t, db); rows != "projects=0 project_members=0 project_links=0 project_link_runs=0 project_outcomes=0" {
			t.Fatalf("invalid sets wrote rows: %s", rows)
		}
	})
}

// TestProjectLinksAtomic makes the last write of a pass (the journal row)
// fail and checks that the links of the previous pass are all still there.
func TestProjectLinksAtomic(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		g := baseGraph()
		first := projectSet(front, back, projectLink(front, back, g.nodes[0], g.nodes[1]))
		if _, err := s.ReplaceProjectLinks(ctx, first); err != nil {
			t.Fatal(err)
		}
		want, err := s.ProjectLinks(ctx, "app")
		if err != nil {
			t.Fatal(err)
		}
		before := projectRows(t, db)
		refuse := []string{"CREATE TRIGGER refuse_journal BEFORE INSERT ON project_link_runs BEGIN SELECT RAISE(ABORT, 'journal refused'); END"}
		if strings.HasSuffix(t.Name(), "/postgres") {
			refuse = []string{
				"CREATE FUNCTION refuse_journal() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''journal refused''; END'",
				"CREATE TRIGGER refuse_journal BEFORE INSERT ON project_link_runs FOR EACH ROW EXECUTE FUNCTION refuse_journal()",
			}
		}
		for _, q := range refuse {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		second := projectSet(front, back, projectLink(front, back, g.nodes[1], g.nodes[0]))
		second.Members = second.Members[:1]
		second.Links = nil
		if _, err := s.ReplaceProjectLinks(ctx, second); err == nil || !strings.Contains(err.Error(), "journal refused") {
			t.Fatalf("refused journal = %v", err)
		}
		got, err := s.ProjectLinks(ctx, "app")
		if err != nil || linkJSON(t, got) != linkJSON(t, want) || projectRows(t, db) != before {
			t.Fatalf("a failed pass changed the project: %+v %v", got, err)
		}
	})
}

func TestProjectLinkJournalRetention(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		for i := 0; i < store.DefaultProjectLinkRuns+2; i++ {
			if _, err := s.ReplaceProjectLinks(ctx, projectSet(front, back)); err != nil {
				t.Fatal(err)
			}
		}
		runs, err := s.ProjectLinkRuns(ctx, "app")
		if err != nil || len(runs) != store.DefaultProjectLinkRuns {
			t.Fatalf("kept %d runs: %v", len(runs), err)
		}
		if runs[0].Seq != 3 || runs[len(runs)-1].Seq != int64(store.DefaultProjectLinkRuns+2) {
			t.Fatalf("kept runs %d to %d", runs[0].Seq, runs[len(runs)-1].Seq)
		}
	})
}

// tableReads counts tableRows' queries.
var tableReads int

// tableRows reads every row of every table except schema_version as text,
// so a migration can be shown to keep each one.
func tableRows(t *testing.T, db *sql.DB, dialect string) map[string][]string {
	t.Helper()
	ctx := context.Background()
	q := "SELECT name FROM sqlite_master WHERE type='table' AND name<>'schema_version' ORDER BY name"
	if dialect == "postgres" {
		q = "SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name<>'schema_version' ORDER BY table_name"
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	out := map[string][]string{}
	for _, table := range tables {
		// A query text of its own per call: the Postgres driver caches a
		// statement's plan by its text, and a migration between two reads
		// changes SELECT *'s columns (cached plan must not change result
		// type).
		tableReads++
		rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s ORDER BY 1,2 -- read %d", table, tableReads))
		if err != nil {
			t.Fatal(err)
		}
		columns, _ := rows.Columns()
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		out[table] = []string{}
		for rows.Next() {
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			cells := []string{}
			for _, v := range values {
				if bytes, ok := v.([]byte); ok {
					v = string(bytes)
				}
				cells = append(cells, fmt.Sprint(v))
			}
			out[table] = append(out[table], strings.Join(cells, "|"))
		}
		rows.Close()
	}
	return out
}

// TestProjectMigrationFromVersion4 rebuilds a version-4 store holding
// graphs, a journal, a pin, a ref snapshot and a fact cache, migrates it,
// and checks that every row of every existing table is kept and that the
// project tables start empty and work.
func TestProjectMigrationFromVersion4(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		dialect := "sqlite"
		if strings.HasSuffix(t.Name(), "/postgres") {
			dialect = "postgres"
		}
		mustWrite(t, s, "front", baseGraph())
		g := baseGraph()
		g.files[0] = gfile("a.ts", "a changed")
		g.diags = []contract.Diagnostic{{Type: "diagnostic", File: "a.ts", Severity: "warning", Code: "x", Message: "m"}}
		mustWrite(t, s, "front", g)
		mustWrite(t, s, "back", baseGraph())
		mustWrite(t, s, store.RefNamespace("back", "c1"), baseGraph(), refOptions("back", "c1", 0))
		runs, err := s.ChangeRuns(ctx, "front")
		if err != nil || len(runs) == 0 {
			t.Fatal(runs, err)
		}
		if _, err := s.Pin(ctx, "front", runs[0].Seq, "release", 0); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveFactCache(ctx, "front", "a.ts", "h", "v", []byte("facts")); err != nil {
			t.Fatal(err)
		}
		dumps := map[string]store.Snapshot{}
		for _, repo := range []string{"front", "back"} {
			if dumps[repo], err = s.Dump(ctx, repo); err != nil {
				t.Fatal(err)
			}
		}
		dropProjectTables(t, db)
		if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=4"); err != nil {
			t.Fatal(err)
		}
		before := tableRows(t, db, dialect)

		migrated, err := sqlstore.Open(ctx, db, dialect)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version); err != nil || version != migrations.Version {
			t.Fatalf("migrated version = %d %v", version, err)
		}
		after := tableRows(t, db, dialect)
		// Migration 6 adds index_runs.settings: NULL (unknown) on every
		// run from before it.
		for i, row := range before["index_runs"] {
			before["index_runs"][i] = row + "|<nil>"
		}
		for table, rows := range before {
			if !reflect.DeepEqual(after[table], rows) {
				t.Errorf("migration changed %s: %d rows -> %d", table, len(rows), len(after[table]))
			}
		}
		for _, table := range []string{"projects", "project_members", "project_links", "project_link_runs", "project_outcomes"} {
			if rows, ok := after[table]; !ok || len(rows) != 0 {
				t.Errorf("%s after migration = %v (exists %v)", table, rows, ok)
			}
		}
		for repo, want := range dumps {
			if got, err := migrated.Dump(ctx, repo); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s dump changed by the migration: %v", repo, err)
			}
		}
		repos, err := migrated.Repositories(ctx)
		if err != nil || len(repos) != 2 {
			t.Fatalf("repositories after migration = %+v %v", repos, err)
		}
		set := store.ProjectLinkSet{Project: "app", ManifestHash: "h"}
		for _, r := range repos {
			set.Members = append(set.Members, store.ProjectMember{Repository: r.ID, Run: r.Latest.ID})
		}
		if _, err := migrated.ReplaceProjectLinks(ctx, set); err != nil {
			t.Fatal(err)
		}
	})
}

// TestOpenReadVersion4 keeps OpenRead's refusal of an older schema for the
// project migration: query commands never migrate.
func TestOpenReadVersion4(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, s, "r", baseGraph())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	dropProjectTables(t, db)
	if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=4"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := sqlite.OpenRead(ctx, path); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("schema version 4 requires indexing with schema %d", migrations.Version)) {
		t.Fatalf("OpenRead on version 4 = %v", err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ProjectLinkRuns(ctx, "app"); err != nil {
		t.Fatal(err)
	}
}

// TestProjectLock checks the project lock (ADR 0075): two lockers of one
// project exclude each other on both backends. On Postgres the project
// lock has its own advisory class, so it never blocks the index lock of a
// repository with the same name or another project; on SQLite it is the
// store lock, so it does.
func TestProjectLock(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx := context.Background()
		postgresBackend := strings.HasSuffix(t.Name(), "/postgres")
		a, b := ls.project("fixture"), ls.project("fixture")
		defer a.Close()
		defer b.Close()
		lease, err := a.Acquire(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Acquire(ctx, 0); !errors.Is(err, store.ErrIndexLocked) {
			t.Fatalf("second project locker = %v", err)
		}
		if state, err := b.State(ctx); err != nil || state != store.LockHeldElsewhere {
			t.Fatalf("state seen by the second locker = %s %v", state, err)
		}
		if postgresBackend {
			if n := ls.projectGranted(t, "fixture"); n != 1 {
				t.Fatalf("granted project locks = %d", n)
			}
			if err := lease.Check(ctx); err != nil {
				t.Fatal(err)
			}
		}
		// The index lock of repository "fixture" and another project.
		for name, l := range map[string]store.IndexLocker{"index lock": ls.locker(), "other project": ls.project("other")} {
			other, err := l.Acquire(ctx, 0)
			if postgresBackend {
				if err != nil {
					t.Fatalf("%s blocked by the project lock: %v", name, err)
				}
				other.Release()
			} else if !errors.Is(err, store.ErrIndexLocked) {
				t.Fatalf("%s on SQLite = %v, want the store lock held", name, err)
			}
			l.Close()
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		lease, err = b.Acquire(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		lease.Release()
	})
}

// TestProjectOutcomes stores a pass's outcomes with its links (T022 B4):
// they read back in pass order, the next pass replaces them in full, a
// member's index error is kept, and invalid outcomes write nothing.
func TestProjectOutcomes(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		g := baseGraph()
		a, b := g.nodes[0], g.nodes[1]
		target := store.ProjectNodeRef{Repository: "back", Identity: b.Identity}
		linked := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: a.Identity}, Template: "/users", Row: "PL-L1", Status: "linked", Target: &target}
		ambiguous := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: a.Identity}, Template: "/orders", Row: "PL-R11", Status: "ambiguous",
			Candidates: []store.ProjectNodeRef{{Repository: "back", Identity: b.Identity}, {Repository: "front", Identity: b.Identity}}, CandidateCount: 2}
		incomplete := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "back", Identity: b.Identity}, Row: "PL-R7", Status: "incomplete", Reason: "ghost"}

		set := projectSet(front, back, projectLink(front, back, a, b))
		set.Members[1].IndexError = "parsing failed"
		set.Outcomes = []store.ProjectOutcome{linked, ambiguous, incomplete}
		if _, err := s.ReplaceProjectLinks(ctx, set); err != nil {
			t.Fatal(err)
		}
		stored, err := s.ProjectLinks(ctx, "app")
		if err != nil {
			t.Fatal(err)
		}
		linked.Candidates, incomplete.Candidates = []store.ProjectNodeRef{}, []store.ProjectNodeRef{}
		if got, want := linkJSON(t, stored.Outcomes), linkJSON(t, []store.ProjectOutcome{linked, ambiguous, incomplete}); got != want {
			t.Fatalf("stored outcomes\n got %s\nwant %s", got, want)
		}
		if stored.Members[1].IndexError != "parsing failed" || stored.Members[0].IndexError != "" {
			t.Fatalf("members = %+v", stored.Members)
		}

		// Invalid outcomes are refused and write nothing.
		before := projectRows(t, db)
		for name, o := range map[string]store.ProjectOutcome{
			"not a member": {Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "ghost", Identity: a.Identity}, Row: "PL-R8", Status: "unmatched"},
			"no identity":  {Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front"}, Row: "PL-R8", Status: "unmatched"},
			"no row":       {Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: a.Identity}, Status: "unmatched"},
		} {
			bad := projectSet(front, back)
			bad.Outcomes = []store.ProjectOutcome{o}
			if _, err := s.ReplaceProjectLinks(ctx, bad); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("%s = %v, want ErrInvalid", name, err)
			}
		}
		if now := projectRows(t, db); now != before {
			t.Fatalf("invalid outcomes wrote rows: %s -> %s", before, now)
		}

		// The next pass replaces the outcomes in full; dropping a member
		// drops its outcomes with it.
		next := store.ProjectLinkSet{Project: "app", ManifestHash: "sha256:manifest", Members: []store.ProjectMember{{Repository: "front", Run: front.ID}}, Outcomes: []store.ProjectOutcome{ambiguous}}
		next.Outcomes[0].Candidates = nil
		next.Outcomes[0].CandidateCount = 0
		if _, err := s.ReplaceProjectLinks(ctx, next); err != nil {
			t.Fatal(err)
		}
		stored, err = s.ProjectLinks(ctx, "app")
		if err != nil || len(stored.Outcomes) != 1 || stored.Outcomes[0].Template != "/orders" || stored.Outcomes[0].Candidates == nil {
			t.Fatalf("second pass outcomes = %+v %v", stored.Outcomes, err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM project_outcomes"); n != 1 {
			t.Fatalf("project_outcomes rows = %d", n)
		}
		// A pass without outcomes stores none.
		if _, err := s.ReplaceProjectLinks(ctx, projectSet(front, back)); err != nil {
			t.Fatal(err)
		}
		if stored, err = s.ProjectLinks(ctx, "app"); err != nil || stored.Outcomes != nil {
			t.Fatalf("outcomes after an empty pass = %+v %v", stored.Outcomes, err)
		}
	})
}

// TestRetireProjectMember renames member back away (T022 B11): every
// project row naming it and its stored index are removed in one
// transaction, the other member is untouched, and a repository another
// project still names is refused with nothing deleted.
func TestRetireProjectMember(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		front, back := projectFixture(t, s)
		// A second run and a pin give back a change journal with entries.
		g := baseGraph()
		back, _ = mustWrite(t, s, "back", g)
		if _, err := s.Pin(ctx, "back", 1, "before", 10); err != nil {
			t.Fatal(err)
		}
		a, b := g.nodes[0], g.nodes[1]
		frontOnly := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: a.Identity}, Template: "/x", Row: "PL-R8", Status: "unmatched"}
		candidate := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: b.Identity}, Template: "/y", Row: "PL-R11", Status: "ambiguous",
			Candidates: []store.ProjectNodeRef{{Repository: "back", Identity: b.Identity}, {Repository: "front", Identity: a.Identity}}, CandidateCount: 2}
		target := store.ProjectNodeRef{Repository: "back", Identity: b.Identity}
		linked := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "front", Identity: a.Identity}, Template: "/z", Row: "PL-L1", Status: "linked", Target: &target}
		fromBack := store.ProjectOutcome{Kind: "calls_route", Source: store.ProjectNodeRef{Repository: "back", Identity: a.Identity}, Row: "PL-R8", Status: "unmatched"}
		set := projectSet(front, back, projectLink(front, back, a, b))
		set.Outcomes = []store.ProjectOutcome{frontOnly, candidate, linked, fromBack}
		if _, err := s.ReplaceProjectLinks(ctx, set); err != nil {
			t.Fatal(err)
		}
		frontBefore, err := s.Dump(ctx, "front")
		if err != nil {
			t.Fatal(err)
		}
		repoTables := []string{"repositories", "index_runs", "files", "nodes", "edges", "diagnostics", "fact_cache", "change_runs", "change_entries", "change_pins", "change_pin_entries", "ref_snapshots"}
		rowsOf := func(repo string) string {
			counts := []string{}
			for _, table := range repoTables {
				counts = append(counts, fmt.Sprintf("%s=%d", table, tableCount(t, db, "SELECT COUNT(*) FROM "+table+" WHERE repository=$1", repo)))
			}
			return strings.Join(counts, " ")
		}
		if tableCount(t, db, "SELECT COUNT(*) FROM change_pins WHERE repository='back'") == 0 || tableCount(t, db, "SELECT COUNT(*) FROM nodes WHERE repository='back'") == 0 {
			t.Fatalf("fixture has no rows to retire: %s", rowsOf("back"))
		}

		// Another project naming back refuses the retirement: nothing moves.
		other := store.ProjectLinkSet{Project: "other", ManifestHash: "sha256:other", Members: []store.ProjectMember{{Repository: "back", Run: back.ID}}}
		if _, err := s.ReplaceProjectLinks(ctx, other); err != nil {
			t.Fatal(err)
		}
		beforeProject, beforeBack := projectRows(t, db), rowsOf("back")
		if err := s.RetireProjectMember(ctx, "app", "back"); !errors.Is(err, store.ErrRepositoryShared) {
			t.Fatalf("shared member = %v, want ErrRepositoryShared", err)
		}
		if projectRows(t, db) != beforeProject || rowsOf("back") != beforeBack {
			t.Fatal("a refused retirement deleted rows")
		}
		if err := s.RetireProjectMember(ctx, "app", "back"+store.RefMarker+"main"); !errors.Is(err, store.ErrReservedRepository) {
			t.Fatalf("ref namespace = %v, want ErrReservedRepository", err)
		}
		if _, err := db.ExecContext(ctx, "DELETE FROM project_members WHERE project='other'"); err != nil {
			t.Fatal(err)
		}

		if err := s.RetireProjectMember(ctx, "app", "back"); err != nil {
			t.Fatal(err)
		}
		none := []string{}
		for _, table := range repoTables {
			none = append(none, table+"=0")
		}
		if got := rowsOf("back"); got != strings.Join(none, " ") {
			t.Fatalf("rows of back after retiring: %s", got)
		}
		for _, q := range []string{
			"SELECT COUNT(*) FROM project_links WHERE source_repository='back' OR target_repository='back'",
			"SELECT COUNT(*) FROM project_members WHERE repository='back'",
			"SELECT COUNT(*) FROM project_outcomes WHERE source_repository='back'",
		} {
			if n := tableCount(t, db, q); n != 0 {
				t.Fatalf("%s = %d", q, n)
			}
		}
		stored, err := s.ProjectLinks(ctx, "app")
		if err != nil {
			t.Fatal(err)
		}
		if len(stored.Members) != 1 || stored.Members[0].Repository != "front" || len(stored.Links) != 0 {
			t.Fatalf("project after retiring = %+v", stored)
		}
		frontOnly.Candidates = []store.ProjectNodeRef{}
		if got, want := linkJSON(t, stored.Outcomes), linkJSON(t, []store.ProjectOutcome{frontOnly}); got != want {
			t.Fatalf("outcomes after retiring\n got %s\nwant %s", got, want)
		}
		repos, err := s.Repositories(ctx)
		if err != nil || len(repos) != 1 || repos[0].ID != "front" {
			t.Fatalf("repositories after retiring = %+v %v", repos, err)
		}
		frontAfter, err := s.Dump(ctx, "front")
		if err != nil || linkJSON(t, frontAfter) != linkJSON(t, frontBefore) {
			t.Fatalf("front changed: %v", err)
		}
		// The journal keeps its passes; retiring again finds nothing.
		if runs, err := s.ProjectLinkRuns(ctx, "app"); err != nil || len(runs) != 1 {
			t.Fatalf("link journal = %d passes, %v", len(runs), err)
		}
		if err := s.RetireProjectMember(ctx, "app", "back"); err != nil {
			t.Fatalf("retiring again = %v", err)
		}
		// The renamed member indexes and links under its new name.
		api, _ := mustWrite(t, s, "api", baseGraph())
		l := projectLink(front, api, a, b)
		l.Target.Repository = "api"
		renamed := store.ProjectLinkSet{Project: "app", ManifestHash: "sha256:renamed", Members: []store.ProjectMember{{Repository: "front", Run: front.ID}, {Repository: "api", Run: api.ID}}, Links: []store.ProjectLink{l}}
		if _, err := s.ReplaceProjectLinks(ctx, renamed); err != nil {
			t.Fatalf("pass after the rename = %v", err)
		}
	})
}

// TestRepositoryFootprint covers what a rename preview reads before it
// retires a repository (T022 review): nothing for an unknown name; for a
// stored one its node count and the projects whose stored pass lists it,
// none for an index made on its own.
func TestRepositoryFootprint(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		none, err := s.RepositoryFootprint(ctx, "ghost")
		if err != nil || none.Stored || none.Nodes != 0 || len(none.Projects) != 0 {
			t.Fatalf("unknown repository = %+v, %v", none, err)
		}
		front, back := projectFixture(t, s)
		alone, err := s.RepositoryFootprint(ctx, "back")
		nodes := tableCount(t, db, "SELECT COUNT(*) FROM nodes WHERE repository='back'")
		if err != nil || !alone.Stored || alone.Nodes != nodes || nodes == 0 || len(alone.Projects) != 0 {
			t.Fatalf("standalone index = %+v, %v (nodes %d)", alone, err, nodes)
		}
		for _, project := range []string{"zeta", "app"} {
			set := projectSet(front, back)
			set.Project = project
			if _, err := s.ReplaceProjectLinks(ctx, set); err != nil {
				t.Fatal(err)
			}
		}
		member, err := s.RepositoryFootprint(ctx, "back")
		if err != nil || !member.Stored || member.Nodes != nodes || strings.Join(member.Projects, ",") != "app,zeta" {
			t.Fatalf("member of two passes = %+v, %v", member, err)
		}
	})
}
