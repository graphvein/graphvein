package indexrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestChangeJournalOracle checks the raw change layer against snapshot diffs
// on real commit pairs (T019 "Oracle", steps 1-4). It runs only with
// GRAPHVEIN_CHANGE_ORACLE=1 and GRAPHVEIN_CHANGE_ORACLE_PAIRS naming a pairs
// file: one pair per line, tab-separated "id clone A B", where clone is a
// local clone (for example git clone --filter=blob:none into scratch) and A
// and B are full commit ids. Lines starting with # are skipped.
// GRAPHVEIN_CHANGE_ORACLE_SCRATCH names the directory for checkouts and
// stores (default: the test's temporary directory) and
// GRAPHVEIN_CHANGE_ORACLE_REPORT a JSON file the per-pair results are
// written to.
//
// For each pair: index A into store S (DA) and pin it; check B out in the
// same directory and index it into S incrementally (journal J, DB'); index B
// fresh into store S2 (DB); then DB' must equal DB, and J's change set, the
// pin's change set and a ref comparison A..B in S must each equal the
// key-wise diff of DA and DB, before and after payloads included. A ref of B
// must also compare equal with the current index of B. Any disagreement
// fails the test: the raw layer must be exact (precision stop). Step 5 (the
// classification invariants) belongs to internal/changes.
func TestChangeJournalOracle(t *testing.T) {
	if os.Getenv("GRAPHVEIN_CHANGE_ORACLE") != "1" {
		t.Skip("set GRAPHVEIN_CHANGE_ORACLE=1 and GRAPHVEIN_CHANGE_ORACLE_PAIRS to run the change journal oracle")
	}
	pairsFile := os.Getenv("GRAPHVEIN_CHANGE_ORACLE_PAIRS")
	if pairsFile == "" {
		t.Fatal("GRAPHVEIN_CHANGE_ORACLE_PAIRS is required")
	}
	pairs, err := readOraclePairs(pairsFile)
	if err != nil {
		t.Fatal(err)
	}
	scratch := os.Getenv("GRAPHVEIN_CHANGE_ORACLE_SCRATCH")
	if scratch == "" {
		scratch = t.TempDir()
	}
	var reports []oracleReport
	for _, p := range pairs {
		t.Run(p.id, func(t *testing.T) {
			r := runOraclePair(t, scratch, p)
			reports = append(reports, r)
			data, _ := json.MarshalIndent(r, "", "  ")
			t.Logf("oracle %s:\n%s", p.id, data)
			if r.Disagreements() > 0 {
				t.Errorf("precision stop: %d raw disagreements on %s", r.Disagreements(), p.id)
			}
		})
	}
	if path := os.Getenv("GRAPHVEIN_CHANGE_ORACLE_REPORT"); path != "" {
		data, _ := json.MarshalIndent(reports, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type oraclePair struct{ id, clone, a, b string }

func readOraclePairs(path string) ([]oraclePair, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading oracle pairs: %w", err)
	}
	defer f.Close()
	var out []oraclePair
	lines := bufio.NewScanner(f)
	for n := 1; lines.Scan(); n++ {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("%s:%d: want id, clone, A and B separated by tabs", path, n)
		}
		out = append(out, oraclePair{fields[0], fields[1], fields[2], fields[3]})
	}
	return out, lines.Err()
}

// oracleCheck is one comparison of a change set with the reference diff.
type oracleCheck struct {
	Name          string   `json:"name"`
	Changes       int      `json:"changes"`
	Disagreements int      `json:"disagreements"`
	Examples      []string `json:"examples,omitempty"`
}

type oracleReport struct {
	ID string `json:"id"`
	A  string `json:"a"`
	B  string `json:"b"`
	// Keys compared: the union of DA's and DB's keys per record.
	Keys      map[string]int     `json:"keys_compared"`
	DiffSize  map[string]int     `json:"diff"`
	Checks    []oracleCheck      `json:"checks"`
	Seconds   map[string]float64 `json:"seconds"`
	StoreSize map[string]int64   `json:"store_bytes"`
	Journal   store.ChangeRun    `json:"journal_run"`
}

func (r oracleReport) Disagreements() int {
	n := 0
	for _, c := range r.Checks {
		n += c.Disagreements
	}
	return n
}

func oracleGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "advice.detachedHead=false", "-c", "core.autocrlf=false", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func runOraclePair(t *testing.T, scratch string, p oraclePair) oracleReport {
	ctx := context.Background()
	report := oracleReport{ID: p.id, A: p.a, B: p.b, Seconds: map[string]float64{}, StoreSize: map[string]int64{}}
	dir := filepath.Join(scratch, p.id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(dir, "tree")
	if _, err := os.Stat(tree); err == nil {
		oracleGit(t, p.clone, "worktree", "remove", "--force", tree)
	}
	oracleGit(t, p.clone, "worktree", "add", "--detach", tree, p.a)
	t.Cleanup(func() {
		exec.Command("git", "-C", p.clone, "worktree", "remove", "--force", tree).Run()
	})
	paths := map[string]string{"S": filepath.Join(dir, "s.db"), "S2": filepath.Join(dir, "s2.db")}
	for _, path := range paths {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(path + suffix)
		}
	}
	timed := func(name string, fn func()) {
		started := time.Now()
		fn()
		report.Seconds[name] = time.Since(started).Seconds()
	}
	options := func(path string) (Options, func()) {
		locker := sqlite.IndexLocker(path)
		lease, err := locker.Acquire(ctx, LockWait)
		if err != nil {
			t.Fatal(err)
		}
		return Options{
			Root:        tree,
			Repository:  p.id,
			Lease:       lease,
			OpenStore:   func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) },
			Parser:      parse.Options{Timeout: DefaultTimeout},
			MaxFileSize: DefaultMaxFileSize,
		}, func() { lease.Release(); locker.Close() }
	}
	dump := func(path, repo string) store.Snapshot {
		s, err := sqlite.OpenRead(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		d, err := s.Dump(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	size := func(name, path string) {
		var total int64
		for _, suffix := range []string{"", "-wal"} {
			if info, err := os.Stat(path + suffix); err == nil {
				total += info.Size()
			}
		}
		report.StoreSize[name] = total
	}

	// 1. Index A into S, Dump, pin A.
	optS, releaseS := options(paths["S"])
	defer releaseS()
	var runA Result
	timed("index_a", func() {
		var err error
		if runA, err = Run(ctx, optS); err != nil {
			t.Fatal(err)
		}
	})
	da := dump(paths["S"], p.id)
	size("S_after_a", paths["S"])
	s, err := sqlite.Open(ctx, paths["S"])
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.ChangeRuns(ctx, p.id)
	if err != nil || len(runs) == 0 {
		t.Fatal("no journal run after A", err)
	}
	if _, err := s.Pin(ctx, p.id, runs[len(runs)-1].Seq, "A", 0); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// 2. Check B out in the same directory and index it incrementally.
	oracleGit(t, tree, "checkout", "-q", "--detach", p.b)
	timed("index_b_incremental", func() {
		if _, err := Run(ctx, optS); err != nil {
			t.Fatal(err)
		}
	})
	dbPrime := dump(paths["S"], p.id)
	size("S_after_b", paths["S"])

	// 3. Index B fresh into S2.
	optS2, releaseS2 := options(paths["S2"])
	timed("index_b_fresh", func() {
		if _, err := Run(ctx, optS2); err != nil {
			t.Fatal(err)
		}
	})
	releaseS2()
	db := dump(paths["S2"], p.id)
	size("S2_after_b", paths["S2"])

	// 4. Compare.
	reference, err := store.DiffSnapshots(da, db)
	if err != nil {
		t.Fatal(err)
	}
	report.Keys = unionKeys(da, db)
	report.DiffSize = map[string]int{}
	for _, c := range reference {
		report.DiffSize[c.Record]++
	}
	incremental, err := store.DiffSnapshots(dbPrime, db)
	if err != nil {
		t.Fatal(err)
	}
	check := oracleCheck{Name: "incremental_equals_fresh", Changes: len(incremental), Disagreements: len(incremental)}
	for i, c := range incremental {
		if i < 10 {
			check.Examples = append(check.Examples, c.Record+" "+c.Key)
		}
	}
	if !reflect.DeepEqual(dbPrime.Diagnostics, db.Diagnostics) {
		check.Disagreements++
		check.Examples = append(check.Examples, "diagnostics differ")
	}
	report.Checks = append(report.Checks, check)

	timed("ref_runs", func() {
		for _, commit := range []string{p.a, p.b} {
			if _, err := RunRef(ctx, RefOptions{Options: optS, Commit: commit}); err != nil {
				t.Fatal(err)
			}
		}
	})
	size("S_after_refs", paths["S"])
	s, err = sqlite.Open(ctx, paths["S"])
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runs, err = s.ChangeRuns(ctx, p.id)
	if err != nil {
		t.Fatal(err)
	}
	report.Journal = runs[len(runs)-1]
	report.Journal.Gaps = nil
	nsA, nsB := store.RefNamespace(p.id, p.a), store.RefNamespace(p.id, p.b)
	for _, c := range []struct {
		name string
		base store.ChangeBase
		want []store.RecordChange
	}{
		{"journal_since_a", store.ChangeBase{Run: runA.Run.ID}, reference},
		{"pin_a", store.ChangeBase{Pin: "A"}, reference},
		{"ref_a_to_b", store.ChangeBase{Ref: nsA, Head: nsB}, reference},
		{"ref_a_to_current", store.ChangeBase{Ref: nsA}, reference},
		{"ref_b_to_current", store.ChangeBase{Ref: nsB}, []store.RecordChange{}},
	} {
		started := time.Now()
		cs, err := s.ChangeSet(ctx, p.id, c.base)
		report.Seconds["changeset_"+c.name] = time.Since(started).Seconds()
		if err != nil {
			t.Fatal(c.name, err)
		}
		report.Checks = append(report.Checks, compareChanges(c.name, cs.Changes, c.want))
	}
	return report
}

func unionKeys(a, b store.Snapshot) map[string]int {
	keys := map[string]map[string]bool{store.RecordFile: {}, store.RecordNode: {}, store.RecordEdge: {}}
	for _, s := range []store.Snapshot{a, b} {
		for _, f := range s.Files {
			keys[store.RecordFile][f.Path] = true
		}
		for _, n := range s.Nodes {
			keys[store.RecordNode][n.Identity] = true
		}
		for _, e := range s.Edges {
			keys[store.RecordEdge][store.EdgeKey(e.Kind, e.Source, e.Target)] = true
		}
	}
	out := map[string]int{}
	for record, set := range keys {
		out[record] = len(set)
	}
	return out
}

// compareChanges counts the keys whose presence or payloads differ between
// got and want.
func compareChanges(name string, got, want []store.RecordChange) oracleCheck {
	check := oracleCheck{Name: name, Changes: len(got)}
	index := func(list []store.RecordChange) map[string]store.RecordChange {
		out := map[string]store.RecordChange{}
		for _, c := range list {
			out[c.Record+"\x00"+c.Key] = c
		}
		return out
	}
	g, w := index(got), index(want)
	add := func(text string) {
		check.Disagreements++
		if len(check.Examples) < 10 {
			check.Examples = append(check.Examples, text)
		}
	}
	for k, c := range w {
		other, ok := g[k]
		switch {
		case !ok:
			add("missing " + c.Record + " " + c.Key)
		case string(other.Before) != string(c.Before) || string(other.After) != string(c.After):
			add("payload differs " + c.Record + " " + c.Key)
		}
	}
	for k, c := range g {
		if _, ok := w[k]; !ok {
			add("extra " + c.Record + " " + c.Key)
		}
	}
	return check
}
