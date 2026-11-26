package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// dropProjectTables removes migration 0005's tables and migration 0006's
// column, so a test can rebuild
// schema version 4.
func dropProjectTables(t *testing.T, db *sql.DB) {
	t.Helper()
	dropRunSettings(t, db)
	for _, table := range []string{"project_outcomes", "project_link_runs", "project_links", "project_members", "projects"} {
		if _, err := db.ExecContext(context.Background(), "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
}

// dropRunSettings removes the column of migration 0006, so a test can
// rebuild schema version 5 or older.
func dropRunSettings(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), "ALTER TABLE index_runs DROP COLUMN settings"); err != nil {
		t.Fatal(err)
	}
}

// dropChangeTables removes the tables of migrations 0004 and 0005, so a
// test can rebuild schema version 3 or older.
func dropChangeTables(t *testing.T, db *sql.DB) {
	t.Helper()
	dropProjectTables(t, db)
	for _, table := range []string{"change_pin_entries", "change_pins", "change_entries", "change_runs", "ref_snapshots"} {
		if _, err := db.ExecContext(context.Background(), "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
}

// graph is one complete snapshot written through the run transaction API,
// so a test controls every upsert, DeleteFile and diagnostic.
type graph struct {
	files []contract.File
	nodes []contract.Node
	edges []contract.Edge
	diags []contract.Diagnostic
	// deleteFirst names files DeleteFile removes before anything else is
	// written, as a changed duplicate file in one stream does.
	deleteFirst []string
}

func gfile(path, content string) contract.File {
	return contract.File{Type: "file", Path: path, Language: "typescript", Precision: "precise", Size: int64(len(content)), ContentHash: contract.ContentHash([]byte(content))}
}

func gnode(path, name string, attrs map[string]any) contract.Node {
	id, err := contract.Identity("function", path, name)
	if err != nil {
		panic(err)
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	return contract.Node{Type: "node", Identity: id, Kind: "function", Level: 4, Name: name, QualifiedName: name, File: path, Span: &contract.Span{StartLine: 1, EndLine: 1, EndColumn: 3}, ContentHash: contract.ContentHash([]byte(name)), Attributes: attrs}
}

func gedge(kind string, source, target contract.Node, evidence ...string) contract.Edge {
	e := contract.Edge{Type: "edge", Kind: kind, Source: source.Identity, Target: target.Identity, Provenance: "fact", Confidence: 1}
	for _, f := range evidence {
		e.Evidence = append(e.Evidence, contract.Evidence{File: f, Span: contract.Span{StartLine: 1, EndLine: 1, EndColumn: 1}})
	}
	return e
}

func header(repo string) contract.Header {
	return contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-13T00:00:00Z"}
}

func writeGraph(ctx context.Context, s store.Store, repo string, g graph, options ...store.RunOptions) (store.Run, error) {
	tx, err := s.BeginRun(ctx, header(repo), options...)
	if err != nil {
		return store.Run{}, err
	}
	defer tx.Rollback()
	for _, p := range g.deleteFirst {
		if err := tx.DeleteFile(ctx, p); err != nil {
			return store.Run{}, err
		}
	}
	for _, f := range g.files {
		if err := tx.UpsertFile(ctx, f); err != nil {
			return store.Run{}, err
		}
	}
	for _, n := range g.nodes {
		if err := tx.UpsertNode(ctx, n); err != nil {
			return store.Run{}, err
		}
	}
	for _, e := range g.edges {
		if err := tx.UpsertEdge(ctx, e); err != nil {
			return store.Run{}, err
		}
	}
	for _, d := range g.diags {
		if err := tx.Diagnostic(ctx, d); err != nil {
			return store.Run{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Run{}, err
	}
	return tx.Run(), nil
}

func mustWrite(t *testing.T, s store.Store, repo string, g graph, options ...store.RunOptions) (store.Run, store.Snapshot) {
	t.Helper()
	ctx := context.Background()
	run, err := writeGraph(ctx, s, repo, g, options...)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := s.Dump(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	return run, dump
}

// checkChangeSet is the mini oracle: the store's change set must equal the
// key-wise diff of the two Dumps, payloads byte for byte.
func checkChangeSet(t *testing.T, s store.Store, repo string, base store.ChangeBase, from, to store.Snapshot) store.ChangeSet {
	t.Helper()
	cs, err := s.ChangeSet(context.Background(), repo, base)
	if err != nil {
		t.Fatal(err)
	}
	want, err := store.DiffSnapshots(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rawChanges(cs.Changes), rawChanges(want)) {
		t.Fatalf("change set (%d) differs from the snapshot diff (%d):\n got %s\nwant %s", len(cs.Changes), len(want), rawChanges(cs.Changes), rawChanges(want))
	}
	return cs
}

func rawChanges(c []store.RecordChange) []string {
	out := []string{}
	for _, x := range c {
		out = append(out, fmt.Sprintf("%s %s %s -> %s", x.Record, x.Key, x.Before, x.After))
	}
	return out
}

func tableCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// baseGraph has three files, call edges between them, an edge whose evidence
// is in a third file, and a synthetic node.
func baseGraph() graph {
	a, b, c := gnode("a.ts", "a", nil), gnode("b.ts", "b", nil), gnode("c.ts", "c", nil)
	b2 := gnode("b.ts", "bb", map[string]any{"x": 1})
	ext := contract.Node{Type: "node", Identity: "external:.#npm%3Areact", Kind: "external", Level: 1, Name: "react", QualifiedName: "npm:react", File: ".", Attributes: map[string]any{}}
	hash, _ := contract.SyntheticHash(ext.Attributes)
	ext.ContentHash = hash
	return graph{
		files: []contract.File{gfile("a.ts", "a"), gfile("b.ts", "b"), gfile("c.ts", "c")},
		nodes: []contract.Node{a, b, b2, c, ext},
		edges: []contract.Edge{gedge("calls", a, b, "a.ts"), gedge("calls", b, c, "b.ts"), gedge("calls", a, c, "c.ts"), gedge("imports", a, ext, "a.ts"), gedge("calls", b2, a, "b.ts")},
	}
}

func TestChangeJournalWritePath(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		run1, d1 := mustWrite(t, s, "r", baseGraph())
		// The first run has nothing to compare with: a run row, no entries.
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries WHERE repository='r'"); n != 0 {
			t.Fatalf("initial run wrote %d entries", n)
		}
		// Run 2: b.ts changed (its nodes and their edges replaced), node a's
		// attributes changed, c.ts removed (its node and every incident or
		// evidence edge cascade), d.ts added.
		g := baseGraph()
		a := gnode("a.ts", "a", map[string]any{"changed": true})
		b := gnode("b.ts", "b", nil)
		d := gnode("d.ts", "d", nil)
		g2 := graph{
			files: []contract.File{gfile("a.ts", "a"), gfile("b.ts", "b changed"), gfile("d.ts", "d")},
			nodes: []contract.Node{a, b, d, g.nodes[4]},
			edges: []contract.Edge{gedge("calls", a, b, "a.ts"), gedge("calls", b, d, "b.ts"), gedge("imports", a, g.nodes[4], "a.ts")},
		}
		// The journal of this tiny graph is larger than the graph, so the
		// byte budget is raised to keep both runs here (retention has its
		// own test).
		run2, d2 := mustWrite(t, s, "r", g2, store.RunOptions{Commit: "c2", Journal: store.JournalLimits{Budget: 100}})
		cs := checkChangeSet(t, s, "r", store.ChangeBase{Run: run1.ID}, d1, d2)
		if len(cs.Changes) == 0 {
			t.Fatal("no changes")
		}
		// Raw entries equal the net change exactly (no duplicates, no
		// no-op rows).
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries WHERE repository='r'"); n != len(cs.Changes) {
			t.Fatalf("entries = %d, want %d", n, len(cs.Changes))
		}
		runs, err := s.ChangeRuns(ctx, "r")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 2 || !runs[0].Initial || runs[1].PreviousRun != run1.ID || runs[1].RunID != run2.ID || runs[1].Commit != "c2" || runs[1].Seq != runs[0].Seq+1 {
			t.Fatalf("journal runs = %+v", runs)
		}
		if runs[0].Counts[store.RecordFile].Added != 3 {
			t.Fatalf("initial counts = %+v", runs[0].Counts)
		}
		c := runs[1].Counts
		if c[store.RecordFile] != (store.ChangeCounts{Added: 1, Changed: 1, Removed: 1}) {
			t.Fatalf("file counts = %+v", c)
		}
		total := 0
		for _, x := range c {
			total += x.Added + x.Changed + x.Removed
		}
		if total != len(cs.Changes) || runs[1].EntryBytes <= 0 {
			t.Fatalf("counts %+v / bytes %d for %d changes", c, runs[1].EntryBytes, len(cs.Changes))
		}
		if cs.Base.Kind != store.SideRun || cs.Base.Run.RunID != run1.ID || cs.Head.Kind != store.SideCurrent || cs.Head.Run.RunID != run2.ID {
			t.Fatalf("sides = %+v / %+v", cs.Base, cs.Head)
		}
		// after_hash is the sha256 of the payload after the run.
		var hash sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT after_hash FROM change_entries WHERE repository='r' AND record='file' AND key='a.ts'").Scan(&hash); err != sql.ErrNoRows {
			t.Fatalf("unchanged file a.ts has an entry: %v %v", hash, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT after_hash FROM change_entries WHERE repository='r' AND record='file' AND key='c.ts'").Scan(&hash); err != nil || hash.Valid {
			t.Fatalf("removed file after_hash = %v %v", hash, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT after_hash FROM change_entries WHERE repository='r' AND record='file' AND key='d.ts'").Scan(&hash); err != nil || len(hash.String) != 64 {
			t.Fatalf("added file after_hash = %v %v", hash, err)
		}
		// Comparing the newest run with itself is empty.
		checkChangeSet(t, s, "r", store.ChangeBase{Run: run2.ID}, d2, d2)
	})
}

func TestChangeJournalNoOpAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		mustWrite(t, s, "r", baseGraph())
		run2, d2 := mustWrite(t, s, "r", baseGraph())
		runs, err := s.ChangeRuns(ctx, "r")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 2 || runs[1].RunID != run2.ID || runs[1].EntryBytes != 0 {
			t.Fatalf("no-op run journal = %+v", runs)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries"); n != 0 {
			t.Fatalf("unchanged re-index wrote %d entries", n)
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Run: runs[0].RunID}, d2, d2)
		// A rolled-back run writes nothing.
		tx, err := s.BeginRun(ctx, header("r"))
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.UpsertFile(ctx, gfile("z.ts", "z")); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_runs"); n != 2 {
			t.Fatalf("rolled-back run left journal rows: %d", n)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries"); n != 0 {
			t.Fatalf("rolled-back run left %d entries", n)
		}
	})
}

func TestChangeJournalDeleteFile(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		run1, d1 := mustWrite(t, s, "r", baseGraph())
		// DeleteFile then re-emitting the same graph is no net change.
		g := baseGraph()
		g.deleteFirst = []string{"b.ts", "c.ts"}
		run2, d2 := mustWrite(t, s, "r", g)
		if !reflect.DeepEqual(d1, d2) {
			t.Fatal("re-emitted graph differs")
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries"); n != 0 {
			t.Fatalf("delete and re-emit wrote %d entries", n)
		}
		// DeleteFile of c.ts without re-emitting it: c's node, its incident
		// edges and the edge with evidence in c.ts are removed. A changed
		// duplicate of a.ts in one stream goes through DeleteFile too.
		g = baseGraph()
		g.deleteFirst = []string{"c.ts"}
		a := g.nodes[0]
		g.files = []contract.File{gfile("a.ts", "a"), gfile("b.ts", "b"), gfile("a.ts", "a2")}
		g.nodes = []contract.Node{a, g.nodes[1], g.nodes[2], g.nodes[4]}
		g.edges = []contract.Edge{gedge("calls", a, g.nodes[1], "a.ts"), g.edges[3]}
		_, d3 := mustWrite(t, s, "r", g)
		checkChangeSet(t, s, "r", store.ChangeBase{Run: run2.ID}, d2, d3)
		checkChangeSet(t, s, "r", store.ChangeBase{Run: run1.ID}, d1, d3)
		for _, key := range []string{store.EdgeKey("calls", "function:a.ts#a", "function:c.ts#c"), store.EdgeKey("calls", "function:b.ts#b", "function:c.ts#c"), store.EdgeKey("calls", "function:b.ts#bb", "function:a.ts#a")} {
			if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries WHERE record='edge' AND key=$1 AND after_hash IS NULL", key); n != 1 {
				t.Fatalf("removed edge %s has %d entries", key, n)
			}
		}
	})
}

func TestChangeJournalRetention(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		limits := store.RunOptions{Journal: store.JournalLimits{Runs: 2}}
		var runs []store.Run
		var dumps []store.Snapshot
		for i := range 4 {
			g := baseGraph()
			g.files[0] = gfile("a.ts", fmt.Sprintf("a%d", i))
			g.nodes[0] = gnode("a.ts", "a", map[string]any{"i": i})
			g.edges = g.edges[1:3]
			run, dump := mustWrite(t, s, "r", g, limits)
			runs, dumps = append(runs, run), append(dumps, dump)
		}
		kept, err := s.ChangeRuns(ctx, "r")
		if err != nil {
			t.Fatal(err)
		}
		if len(kept) != 2 || kept[0].RunID != runs[2].ID || kept[1].RunID != runs[3].ID {
			t.Fatalf("kept runs = %+v", kept)
		}
		// Run 1 started run 2's entries, which were dropped: expired. Run 2
		// is where the oldest kept run starts, so it is still covered.
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Run: runs[0].ID}); !errors.Is(err, store.ErrBaseExpired) {
			t.Fatalf("expired base = %v", err)
		}
		cs := checkChangeSet(t, s, "r", store.ChangeBase{Run: runs[1].ID}, dumps[1], dumps[3])
		if !cs.Base.Run.GapsUnknown {
			t.Fatal("a base older than the journal must carry unknown gaps")
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Run: runs[2].ID}, dumps[2], dumps[3])
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Run: "no-such-run"}); !errors.Is(err, store.ErrBaseUnknown) {
			t.Fatalf("unknown base = %v", err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_entries WHERE seq<$1", kept[0].Seq); n != 0 {
			t.Fatalf("dropped runs left %d entries", n)
		}
		// By bytes: a tiny budget keeps only the newest run, which still
		// answers "since the previous run".
		g := baseGraph()
		run5, d5 := mustWrite(t, s, "r", g, store.RunOptions{Journal: store.JournalLimits{Budget: 1e-9}})
		kept, err = s.ChangeRuns(ctx, "r")
		if err != nil {
			t.Fatal(err)
		}
		if len(kept) != 1 || kept[0].RunID != run5.ID {
			t.Fatalf("byte budget kept %+v", kept)
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Run: runs[3].ID}, dumps[3], d5)
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Run: runs[2].ID}); !errors.Is(err, store.ErrBaseExpired) {
			t.Fatalf("expired base = %v", err)
		}
		// The default budget (graph payload bytes) keeps both small runs.
		mustWrite(t, s, "r", baseGraph())
		if kept, _ := s.ChangeRuns(ctx, "r"); len(kept) != 2 {
			t.Fatalf("default limits kept %d runs", len(kept))
		}
	})
}

func TestChangePins(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		step := func(i int) graph {
			g := baseGraph()
			g.files[0] = gfile("a.ts", fmt.Sprintf("a%d", i))
			g.nodes[0] = gnode("a.ts", "a", map[string]any{"i": i})
			if i%2 == 1 {
				g.files = g.files[:2]
				g.nodes = []contract.Node{g.nodes[0], g.nodes[1], g.nodes[2], g.nodes[4]}
				g.edges = []contract.Edge{gedge("calls", g.nodes[0], g.nodes[1], "a.ts"), g.edges[3], g.edges[4]}
			}
			return g
		}
		_, d1 := mustWrite(t, s, "r", step(0))
		_, d2 := mustWrite(t, s, "r", step(1))
		runs, err := s.ChangeRuns(ctx, "r")
		if err != nil {
			t.Fatal(err)
		}
		// Pinning the latest run creates an empty overlay.
		latest, err := s.Pin(ctx, "r", runs[1].Seq, "latest", 3)
		if err != nil {
			t.Fatal(err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_pin_entries WHERE pin=$1", latest.ID); n != 0 {
			t.Fatalf("latest pin overlay has %d entries", n)
		}
		if latest.Run.RunID != runs[1].RunID || latest.Seq != runs[1].Seq || latest.CreatedAt == "" {
			t.Fatalf("pin = %+v", latest)
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Pin: "latest"}, d2, d2)
		// Pinning an older run seeds the overlay from the later entries.
		if _, err := s.Pin(ctx, "r", runs[0].Seq, "older", 3); err != nil {
			t.Fatal(err)
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Pin: "older"}, d1, d2)
		// Three more runs change the same keys again (first write wins) and
		// shrink the journal to one run; the pins stay exact.
		var last store.Snapshot
		for i := 2; i < 5; i++ {
			_, last = mustWrite(t, s, "r", step(i), store.RunOptions{Journal: store.JournalLimits{Runs: 1}})
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Pin: "latest"}, d2, last)
		cs := checkChangeSet(t, s, "r", store.ChangeBase{Pin: "older"}, d1, last)
		if cs.Base.Kind != store.SidePin || cs.Base.Pin != "older" || cs.Base.Run.RunID != runs[0].RunID {
			t.Fatalf("pin side = %+v", cs.Base)
		}
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Run: runs[0].RunID}); !errors.Is(err, store.ErrBaseExpired) {
			t.Fatalf("run base under the pin = %v", err)
		}
		// Limits, names and coverage.
		if _, err := s.Pin(ctx, "r", runs[0].Seq, "older", 3); !errors.Is(err, store.ErrBaseExpired) {
			t.Fatalf("pin of a dropped run = %v", err)
		}
		kept, _ := s.ChangeRuns(ctx, "r")
		if _, err := s.Pin(ctx, "r", kept[0].Seq, "older", 3); !errors.Is(err, store.ErrPinExists) {
			t.Fatalf("duplicate name = %v", err)
		}
		if _, err := s.Pin(ctx, "r", kept[0].Seq, "third", 3); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pin(ctx, "r", kept[0].Seq, "fourth", 3); !errors.Is(err, store.ErrPinLimit) {
			t.Fatalf("pin limit = %v", err)
		}
		if _, err := s.Pin(ctx, "r", kept[0].Seq+100, "future", 0); !errors.Is(err, store.ErrBaseUnknown) {
			t.Fatalf("unknown run = %v", err)
		}
		list, err := s.Pins(ctx, "r")
		if err != nil || len(list) != 3 || list[0].Name != "latest" || list[1].Name != "older" {
			t.Fatalf("pins = %+v %v", list, err)
		}
		if err := s.Unpin(ctx, "r", list[1].ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Unpin(ctx, "r", list[1].ID); !errors.Is(err, store.ErrPinUnknown) {
			t.Fatalf("second unpin = %v", err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_pin_entries WHERE pin=$1", list[1].ID); n != 0 {
			t.Fatalf("unpinned overlay left %d entries", n)
		}
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Pin: "older"}); !errors.Is(err, store.ErrBaseUnknown) {
			t.Fatalf("unpinned base = %v", err)
		}
		// The default limit is five.
		for i := range 3 {
			if _, err := s.Pin(ctx, "r", kept[0].Seq, fmt.Sprintf("d%d", i), 0); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Pin(ctx, "r", kept[0].Seq, "d3", 0); !errors.Is(err, store.ErrPinLimit) {
			t.Fatalf("default pin limit = %v", err)
		}
	})
}

func refOptions(repo, commit string, max int) store.RunOptions {
	return store.RunOptions{Namespace: &store.Namespace{Repository: repo, Commit: commit, ConfigSource: "none", MaxSnapshots: max}}
}

func TestChangeRefNamespaces(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		_, main := mustWrite(t, s, "r", baseGraph())
		g := baseGraph()
		g.files[1] = gfile("b.ts", "b at c2")
		g.diags = []contract.Diagnostic{{Type: "diagnostic", File: "b.ts", Severity: "error", Code: "parse_error", Message: "x"}}
		nsA, nsB := store.RefNamespace("r", "c1"), store.RefNamespace("r", "c2")
		_, dA := mustWrite(t, s, nsA, baseGraph(), refOptions("r", "c1", 0))
		_, dB := mustWrite(t, s, nsB, g, refOptions("r", "c2", 0))
		// Isolation: the main graph and its journal are untouched.
		if now, err := s.Dump(ctx, "r"); err != nil || !reflect.DeepEqual(now, main) {
			t.Fatal("ref run changed the main repository", err)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM change_runs WHERE repository<>'r'"); n != 0 {
			t.Fatalf("ref runs wrote %d journal runs", n)
		}
		snaps, err := s.RefSnapshots(ctx, "r")
		if err != nil || len(snaps) != 2 || snaps[0].Namespace != nsA || snaps[1].Commit != "c2" || snaps[1].Extractor != "test/1" {
			t.Fatalf("snapshots = %+v %v", snaps, err)
		}
		if !reflect.DeepEqual(snaps[1].Run.Gaps, []string{"b.ts"}) || snaps[1].Run.Dirty == nil || *snaps[1].Run.Dirty {
			t.Fatalf("snapshot run = %+v", snaps[1].Run)
		}
		cs := checkChangeSet(t, s, "r", store.ChangeBase{Ref: nsA, Head: nsB}, dA, dB)
		if len(cs.Changes) == 0 || cs.Base.Kind != store.SideRef || cs.Head.Repository != nsB {
			t.Fatalf("ref change set = %+v", cs)
		}
		checkChangeSet(t, s, "r", store.ChangeBase{Ref: nsB}, dB, main)
		if _, err := s.ChangeSet(ctx, "r", store.ChangeBase{Ref: store.RefNamespace("r", "c9")}); !errors.Is(err, store.ErrRefUnknown) {
			t.Fatalf("unknown ref = %v", err)
		}
		// Reserved identifiers.
		if _, err := writeGraph(ctx, s, "x@ref:y", baseGraph()); !errors.Is(err, store.ErrReservedRepository) {
			t.Fatalf("reserved repository = %v", err)
		}
		if _, err := writeGraph(ctx, s, nsA, baseGraph(), refOptions("other", "c1", 0)); err == nil {
			t.Fatal("ref run into another repository's namespace was accepted")
		}
		// Least recently used snapshots beyond the limit are dropped with
		// everything under their namespace.
		nsC := store.RefNamespace("r", "c3")
		mustWrite(t, s, nsC, baseGraph(), refOptions("r", "c3", 2))
		snaps, _ = s.RefSnapshots(ctx, "r")
		if len(snaps) != 2 || snaps[0].Namespace != nsB || snaps[1].Namespace != nsC {
			t.Fatalf("after the limit: %+v", snaps)
		}
		for _, table := range []string{"files", "nodes", "edges", "diagnostics", "index_runs", "fact_cache", "repositories"} {
			if n := tableCount(t, db, "SELECT COUNT(*) FROM "+table+" WHERE repository=$1", nsA); n != 0 {
				t.Fatalf("dropped namespace left %d %s rows", n, table)
			}
		}
		if err := s.DropRefSnapshot(ctx, "r", nsB); err != nil {
			t.Fatal(err)
		}
		if err := s.DropRefSnapshot(ctx, "r", nsB); !errors.Is(err, store.ErrRefUnknown) {
			t.Fatalf("second drop = %v", err)
		}
		if d, err := s.Dump(ctx, nsB); err != nil || len(d.Files)+len(d.Nodes)+len(d.Edges)+len(d.Diagnostics) != 0 {
			t.Fatal("dropped namespace still has a graph", err)
		}
		if now, err := s.Dump(ctx, "r"); err != nil || !reflect.DeepEqual(now, main) {
			t.Fatal("dropping a ref changed the main repository", err)
		}
	})
}

func TestChangeGaps(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		g := baseGraph()
		g.diags = []contract.Diagnostic{
			{Type: "diagnostic", File: "b.ts", Severity: "error", Code: "parse_error", Message: "x"},
			{Type: "diagnostic", File: "b.ts", Severity: "error", Code: "other", Message: "y"},
			{Type: "diagnostic", File: "lib", Severity: "error", Code: "scan_read", Message: "z"},
			{Type: "diagnostic", File: "a.ts", Severity: "warning", Code: "w", Message: "w"},
			// A syntax_error warning (tree-sitter recovered) is a gap too
			// (store.GapWarningCodes).
			{Type: "diagnostic", File: "c.ts", Severity: "warning", Code: "syntax_error", Message: "recovered"},
		}
		mustWrite(t, s, "r", g)
		runs, _ := s.ChangeRuns(ctx, "r")
		if !reflect.DeepEqual(runs[0].Gaps, []string{"b.ts", "c.ts", "lib"}) || runs[0].GapsFileless || runs[0].GapsTruncated {
			t.Fatalf("gaps = %+v", runs[0])
		}
		g.diags = append(g.diags, contract.Diagnostic{Type: "diagnostic", Severity: "error", Code: "f", Message: "f"})
		mustWrite(t, s, "r", g)
		runs, _ = s.ChangeRuns(ctx, "r")
		if !runs[1].GapsFileless {
			t.Fatal("a file-less error diagnostic must make the run wholly partial")
		}
	})
}

// TestChangeCacheFallback indexes a tree as the main repository and then the
// same tree as a ref namespace: the ref run reads the main repository's fact
// cache, so it parses zero files, and writes cache entries only to its own
// namespace.
func TestChangeCacheFallback(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		for name, content := range map[string]string{"a.ts": "export function a() { return b() }\n", "b.ts": "export function b() { return 1 }\n", "m.py": "def f():\n    return 1\n"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		emit := func(options index.Options, runOptions store.RunOptions) index.Summary {
			var out strings.Builder
			options.Parser = parse.Options{}
			summary, err := index.Emit(ctx, root, s, &out, options)
			if err != nil {
				t.Fatal(err)
			}
			if err := ingest.Stream(ctx, strings.NewReader(out.String()), store.WithRunOptions(s, runOptions)); err != nil {
				t.Fatal(err)
			}
			return summary
		}
		first := emit(index.Options{Repository: "r"}, store.RunOptions{})
		if first.Parsed == 0 {
			t.Fatal("main run parsed nothing")
		}
		ns := store.RefNamespace("r", "c1")
		ref := emit(index.Options{Repository: ns, CacheFallback: "r"}, refOptions("r", "c1", 0))
		if ref.Parsed != 0 || ref.Reused != first.Parsed {
			t.Fatalf("ref run parsed %d, reused %d (main parsed %d)", ref.Parsed, ref.Reused, first.Parsed)
		}
		// Reused entries are not copied: the ref's own cache holds only what
		// it parsed itself.
		if n := tableCount(t, db, "SELECT COUNT(*) FROM fact_cache WHERE repository=$1", ns); n != 0 {
			t.Fatalf("ref cache entries = %d, want 0", n)
		}
		main, _ := s.Dump(ctx, "r")
		refDump, _ := s.Dump(ctx, ns)
		checkChangeSet(t, s, "r", store.ChangeBase{Ref: ns}, refDump, main)
		// A ref whose a.ts differs parses that file only and caches it in
		// its own namespace; the main repository's cache is untouched.
		mainCache := tableCount(t, db, "SELECT COUNT(*) FROM fact_cache WHERE repository='r'")
		if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function a() { return 2 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ns2 := store.RefNamespace("r", "c2")
		changed := emit(index.Options{Repository: ns2, CacheFallback: "r"}, refOptions("r", "c2", 0))
		if changed.Parsed != 1 {
			t.Fatalf("changed ref parsed %d files, want 1", changed.Parsed)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM fact_cache WHERE repository=$1 AND path='a.ts'", ns2); n != 1 {
			t.Fatalf("changed ref cached %d entries for a.ts", n)
		}
		if n := tableCount(t, db, "SELECT COUNT(*) FROM fact_cache WHERE repository='r'"); n != mainCache {
			t.Fatalf("main cache changed: %d entries, was %d", n, mainCache)
		}
		// A second index of the same ref reads its own namespace's entry.
		again := emit(index.Options{Repository: ns2, CacheFallback: "r"}, refOptions("r", "c2", 0))
		if again.Parsed != 0 {
			t.Fatalf("re-indexed ref parsed %d files", again.Parsed)
		}
	})
}

// TestChangeJournalFromVersion3 rebuilds a version-3 store: OpenRead keeps
// refusing it, Open migrates it, and the first run after the migration
// journals its changes from the last run before it.
func TestChangeJournalFromVersion3(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		old, d1 := mustWrite(t, s, "r", baseGraph())
		dropChangeTables(t, db)
		if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=3"); err != nil {
			t.Fatal(err)
		}
		dialect := "sqlite"
		if strings.HasSuffix(t.Name(), "/postgres") {
			dialect = "postgres"
		}
		reopened, err := sqlstore.Open(ctx, db, dialect)
		if err != nil {
			t.Fatal(err)
		}
		if migrations.Version < 4 {
			t.Fatalf("migrations.Version = %d", migrations.Version)
		}
		g := baseGraph()
		g.files[0] = gfile("a.ts", "a changed")
		g.nodes[0] = gnode("a.ts", "a", map[string]any{"v": 3})
		g.edges = g.edges[1:3]
		_, d2 := mustWrite(t, reopened, "r", g)
		runs, err := reopened.ChangeRuns(ctx, "r")
		if err != nil || len(runs) != 1 || runs[0].Initial || runs[0].PreviousRun != old.ID {
			t.Fatalf("journal after migration = %+v %v", runs, err)
		}
		cs := checkChangeSet(t, reopened, "r", store.ChangeBase{Run: old.ID}, d1, d2)
		if !cs.Base.Run.GapsUnknown || cs.Base.Run.Indexer.Name != "test" {
			t.Fatalf("pre-journal base side = %+v", cs.Base)
		}
	})
}

// TestOpenReadVersion3 keeps OpenRead's refusal of an older schema: the
// query commands never migrate, so a version-3 store needs one index run.
func TestOpenReadVersion3(t *testing.T) {
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
	dropChangeTables(t, db)
	if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=3"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := sqlite.OpenRead(ctx, path); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("schema version 3 requires indexing with schema %d", migrations.Version)) {
		t.Fatalf("OpenRead on version 3 = %v", err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ChangeRuns(ctx, "r"); err != nil {
		t.Fatal(err)
	}
}

// Ensure the JSON shapes used by the API (a2) stay stable for the fields the
// store fills.
func TestChangeRunJSON(t *testing.T) {
	dirty := true
	data, err := json.Marshal(store.ChangeRun{Seq: 2, RunID: "x", PreviousRun: "w", Indexer: store.Indexer{Name: "graphvein", Version: "1"}, Commit: "abc", Dirty: &dirty, FinishedAt: "t", Gaps: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"seq":2,"run_id":"x","previous_run":"w","indexer":{"name":"graphvein","version":"1"},"commit":"abc","dirty":true,"finished_at":"t","entry_bytes":0,"gaps":[],"gaps_truncated":false,"gaps_fileless":false}`
	if string(data) != want {
		t.Fatalf("ChangeRun JSON = %s", data)
	}
}
