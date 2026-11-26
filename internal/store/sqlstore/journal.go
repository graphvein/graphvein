package sqlstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// The change journal (ADR 0056). A run transaction already holds every
// stored payload in its old maps (ADR 0006). The first time the run changes,
// removes or adds a key, remember copies the key's pre-run state into the
// journal state; at Commit the net change per remembered key becomes one
// change_entries row, so memory stays O(changed keys).

// prior is a key's state before the run: its payload, or absent.
type prior struct {
	data    string
	present bool
}

// journalState collects what Commit writes into the journal. For a ref run
// (enabled false) only the knowledge gaps are kept, for the ref snapshot.
type journalState struct {
	enabled bool
	before  map[string]map[string]prior
	// gaps counts error diagnostics per path; fileless counts those without
	// a path.
	gaps     map[string]int
	fileless int
}

func newJournalState(enabled bool) journalState {
	j := journalState{enabled: enabled, gaps: map[string]int{}}
	if enabled {
		j.before = map[string]map[string]prior{"files": {}, "nodes": {}, "edges": {}}
	}
	return j
}

// remember records key's pre-run state the first time the run modifies it.
// Every modification of the old maps calls it before the change, so the
// first call always sees the state loaded at BeginRun.
func (t *transaction) remember(table, key string) {
	if !t.journal.enabled {
		return
	}
	if _, done := t.journal.before[table][key]; done {
		return
	}
	data, ok := t.old[table][key]
	t.journal.before[table][key] = prior{data: data, present: ok}
}

// noteDiagnostic records a knowledge gap: an error-severity diagnostic, or
// a warning store.GapWarning names.
func (j *journalState) noteDiagnostic(d contract.Diagnostic) {
	if !store.GapDiagnostic(d) {
		return
	}
	if d.File == "" {
		j.fileless++
		return
	}
	j.gaps[d.File]++
}

// dropGaps mirrors DeleteFile, which deletes the file's stored diagnostics.
func (j *journalState) dropGaps(path string) { delete(j.gaps, path) }

// gapList returns the sorted gap paths, capped at store.MaxGapPaths.
func (j *journalState) gapList() (paths []string, truncated bool) {
	paths = []string{}
	for p := range j.gaps {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > store.MaxGapPaths {
		return paths[:store.MaxGapPaths], true
	}
	return paths, false
}

// recordNames maps the graph tables to journal record names.
var recordNames = map[string]string{"files": store.RecordFile, "nodes": store.RecordNode, "edges": store.RecordEdge}

// journalKey converts a transaction map key to the stored journal key.
func journalKey(table, key string) string {
	if table != "edges" {
		return key
	}
	parts := strings.SplitN(key, "\x00", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return store.EdgeKey(parts[0], parts[1], parts[2])
}

// entry is one net change of the run.
type entry struct {
	record, key string
	before      *string
	afterHash   *string
}

func (e entry) bytes() int64 {
	n := len(e.record) + len(e.key)
	if e.before != nil {
		n += len(*e.before)
	}
	if e.afterHash != nil {
		n += len(*e.afterHash)
	}
	return int64(n)
}

func payloadHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// netEntries compares each remembered key's pre-run state with its final
// state. Equal states (a file deleted and re-emitted unchanged) produce no
// entry.
func (t *transaction) netEntries() ([]entry, map[string]store.ChangeCounts) {
	var out []entry
	counts := map[string]store.ChangeCounts{}
	for _, table := range []string{"files", "nodes", "edges"} {
		record := recordNames[table]
		c := counts[record]
		for key, p := range t.journal.before[table] {
			final, ok := t.old[table][key]
			present := ok && t.seen[table][key]
			if p.present == present && (!present || p.data == final) {
				continue
			}
			e := entry{record: record, key: journalKey(table, key)}
			if p.present {
				data := p.data
				e.before = &data
			}
			if present {
				h := payloadHash(final)
				e.afterHash = &h
			}
			switch {
			case !p.present:
				c.Added++
			case !present:
				c.Removed++
			default:
				c.Changed++
			}
			out = append(out, e)
		}
		counts[record] = c
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].record != out[j].record {
			return out[i].record < out[j].record
		}
		return out[i].key < out[j].key
	})
	return out, counts
}

// graphBytes is the payload size of the graph the run leaves behind: the
// seen keys of the old maps, which hold exactly the stored payloads.
func (t *transaction) graphBytes() int64 {
	var n int64
	for _, table := range []string{"files", "nodes", "edges"} {
		for key, data := range t.old[table] {
			if t.seen[table][key] {
				n += int64(len(data))
			}
		}
	}
	return n
}

// runFacts describes the committed run for the journal and ref snapshots.
func (t *transaction) runFacts() store.ChangeRun {
	gaps, truncated := t.journal.gapList()
	r := store.ChangeRun{
		RunID:         t.run.ID,
		Indexer:       store.Indexer{Name: t.run.Header.Indexer.Name, Version: t.run.Header.Indexer.Version},
		Commit:        t.options.Commit,
		Dirty:         t.options.Dirty,
		FinishedAt:    t.run.FinishedAt,
		Gaps:          gaps,
		GapsTruncated: truncated,
		GapsFileless:  t.journal.fileless > 0,
	}
	if t.run.Header.Commit != "" {
		r.Commit = t.run.Header.Commit
	}
	return r
}

// previousRun finds the run whose state the old maps were loaded from: the
// newest journal run, or for a store migrated from version 3 the newest
// index run before this one. Empty means the repository's first run.
func (t *transaction) previousRun(ctx context.Context) (string, error) {
	repo := t.run.Header.Repository
	var id string
	err := t.tx.QueryRowContext(ctx, "SELECT run_id FROM change_runs WHERE repository=$1 ORDER BY seq DESC LIMIT 1", repo).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("reading the newest journal run: %w", err)
	}
	runs, err := read[store.Run](ctx, t.tx, "SELECT payload FROM index_runs WHERE repository=$1 AND id<>$2", repo, t.run.ID)
	if err != nil {
		return "", fmt.Errorf("reading earlier runs: %w", err)
	}
	var newest time.Time
	for _, r := range runs {
		finished, err := time.Parse(time.RFC3339Nano, r.FinishedAt)
		if err != nil {
			continue
		}
		if id == "" || finished.After(newest) {
			id, newest = r.ID, finished
		}
	}
	return id, nil
}

// writeJournal writes the run's journal row, its entries and the pin
// overlays, then applies retention, all inside the run transaction.
func (t *transaction) writeJournal(ctx context.Context) error {
	repo := t.run.Header.Repository
	previous, err := t.previousRun(ctx)
	if err != nil {
		return err
	}
	entries, counts := t.netEntries()
	run := t.runFacts()
	run.PreviousRun = previous
	run.Initial = previous == ""
	run.Counts = counts
	if run.Initial {
		// No base precedes the first run, so its entries could never be
		// read; the counts still say what it added.
		entries = nil
	}
	for _, e := range entries {
		run.EntryBytes += e.bytes()
	}
	var seq int64
	if err := t.tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq),0)+1 FROM change_runs WHERE repository=$1", repo).Scan(&seq); err != nil {
		return fmt.Errorf("allocating journal run: %w", err)
	}
	run.Seq = seq
	data, err := payload(run)
	if err != nil {
		return err
	}
	if err := t.exec(ctx, "INSERT INTO change_runs(repository,seq,run_id,payload) VALUES($1,$2,$3,$4)", repo, seq, t.run.ID, data); err != nil {
		return err
	}
	const batch = 100
	for start := 0; start < len(entries); start += batch {
		end := min(start+batch, len(entries))
		args := []any{repo, seq}
		values := make([]string, 0, end-start)
		for _, e := range entries[start:end] {
			args = append(args, e.record, e.key, nullable(e.before), nullable(e.afterHash))
			n := len(args)
			values = append(values, fmt.Sprintf("($1,$2,$%d,$%d,$%d,$%d)", n-3, n-2, n-1, n))
		}
		if err := t.exec(ctx, "INSERT INTO change_entries(repository,seq,record,key,before,after_hash) VALUES "+strings.Join(values, ","), args...); err != nil {
			return err
		}
	}
	// Every pin keeps the payload a key had at the pin: the first change
	// after the pin writes it, later changes leave it (first write wins).
	if len(entries) > 0 {
		if err := t.exec(ctx, "INSERT INTO change_pin_entries(repository,pin,record,key,before) SELECT e.repository,p.id,e.record,e.key,e.before FROM change_entries e JOIN change_pins p ON p.repository=e.repository WHERE e.repository=$1 AND e.seq=$2 ON CONFLICT(repository,pin,record,key) DO NOTHING", repo, seq); err != nil {
			return err
		}
	}
	return t.retain(ctx, seq)
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// retain drops the oldest journal runs beyond the run limit or the byte
// budget, always keeping the newest run.
func (t *transaction) retain(ctx context.Context, newest int64) error {
	repo := t.run.Header.Repository
	limits := t.options.Journal.WithDefaults()
	runs, err := changeRuns(ctx, t.tx, repo)
	if err != nil {
		return err
	}
	budget := limits.Budget * float64(t.graphBytes())
	var total int64
	var drop int64 = -1
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		kept := len(runs) - 1 - i
		total += r.EntryBytes
		if r.Seq != newest && (kept >= limits.Runs || float64(total) > budget) {
			drop = r.Seq
			break
		}
	}
	if drop < 0 {
		return nil
	}
	if err := t.exec(ctx, "DELETE FROM change_entries WHERE repository=$1 AND seq<=$2", repo, drop); err != nil {
		return err
	}
	return t.exec(ctx, "DELETE FROM change_runs WHERE repository=$1 AND seq<=$2", repo, drop)
}

// checkNamespace refuses reserved repository identifiers outside ref runs and
// ref runs whose header repository is not their namespace.
func checkNamespace(repository string, ns *store.Namespace) error {
	if ns == nil {
		if store.ReservedRepository(repository) {
			return fmt.Errorf("repository %q: %w", repository, store.ErrReservedRepository)
		}
		return nil
	}
	if ns.Repository == "" || store.ReservedRepository(ns.Repository) {
		return fmt.Errorf("ref run of repository %q: %w", ns.Repository, store.ErrReservedRepository)
	}
	if ns.Commit == "" || strings.ContainsAny(ns.Commit, "@:/\x00") {
		return fmt.Errorf("ref run needs a commit id, got %q", ns.Commit)
	}
	if want := store.RefNamespace(ns.Repository, ns.Commit); repository != want {
		return fmt.Errorf("ref run header repository %q, want %q", repository, want)
	}
	return nil
}

// registerRefSnapshot records a committed ref run as a ref snapshot of its
// repository and drops the least recently used snapshots beyond the limit.
func (t *transaction) registerRefSnapshot(ctx context.Context) error {
	ns := t.options.Namespace
	namespace := t.run.Header.Repository
	now := t.run.FinishedAt
	snap := store.RefSnapshot{
		Namespace:    namespace,
		Commit:       ns.Commit,
		Extractor:    t.run.Header.Indexer.Name + "/" + t.run.Header.Indexer.Version,
		CreatedAt:    now,
		LastUsedAt:   now,
		ConfigSource: ns.ConfigSource,
		Run:          t.runFacts(),
	}
	clean := false
	snap.Run.Commit, snap.Run.Dirty = ns.Commit, &clean
	existing, err := refSnapshots(ctx, t.tx, ns.Repository)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Namespace == namespace {
			snap.CreatedAt = e.CreatedAt
		}
	}
	data, err := payload(snap)
	if err != nil {
		return err
	}
	if err := t.exec(ctx, "INSERT INTO repositories(repository) VALUES($1) ON CONFLICT(repository) DO NOTHING", ns.Repository); err != nil {
		return err
	}
	if err := t.exec(ctx, "INSERT INTO ref_snapshots(repository,namespace,commit_id,extractor,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(repository,namespace) DO UPDATE SET commit_id=excluded.commit_id,extractor=excluded.extractor,payload=excluded.payload", ns.Repository, namespace, snap.Commit, snap.Extractor, data); err != nil {
		return err
	}
	limit := ns.MaxSnapshots
	if limit <= 0 {
		limit = store.DefaultMaxRefSnapshots
	}
	others := []store.RefSnapshot{}
	for _, e := range existing {
		if e.Namespace != namespace {
			others = append(others, e)
		}
	}
	// Newest use first; the run's own snapshot is the newest and counts one.
	sort.Slice(others, func(i, j int) bool { return usedAfter(others[i], others[j]) })
	for i, e := range others {
		if i+1 >= limit {
			if err := dropNamespace(ctx, t.tx, ns.Repository, e.Namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

// usedAfter orders snapshots by last use, newest first, then by namespace.
func usedAfter(a, b store.RefSnapshot) bool {
	ta, _ := time.Parse(time.RFC3339Nano, a.LastUsedAt)
	tb, _ := time.Parse(time.RFC3339Nano, b.LastUsedAt)
	if !ta.Equal(tb) {
		return ta.After(tb)
	}
	return a.Namespace < b.Namespace
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// dropNamespace deletes a ref snapshot and every row stored under its
// namespace, children before parents.
func dropNamespace(ctx context.Context, tx execer, repository, namespace string) error {
	for _, q := range []string{
		"DELETE FROM edges WHERE repository=$1",
		"DELETE FROM nodes WHERE repository=$1",
		"DELETE FROM diagnostics WHERE repository=$1",
		"DELETE FROM files WHERE repository=$1",
		"DELETE FROM fact_cache WHERE repository=$1",
		"DELETE FROM index_runs WHERE repository=$1",
		"DELETE FROM repositories WHERE repository=$1",
	} {
		if _, err := tx.ExecContext(ctx, q, namespace); err != nil {
			return fmt.Errorf("dropping ref snapshot %s: %w", namespace, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ref_snapshots WHERE repository=$1 AND namespace=$2", repository, namespace); err != nil {
		return fmt.Errorf("dropping ref snapshot %s: %w", namespace, err)
	}
	return nil
}

// changeRuns reads the kept journal runs oldest first; the seq column is
// authoritative.
func changeRuns(ctx context.Context, db queryer, repo string) ([]store.ChangeRun, error) {
	rows, err := db.QueryContext(ctx, "SELECT seq,payload FROM change_runs WHERE repository=$1 ORDER BY seq", repo)
	if err != nil {
		return nil, fmt.Errorf("reading journal runs of %s: %w", repo, err)
	}
	defer rows.Close()
	out := []store.ChangeRun{}
	for rows.Next() {
		var seq int64
		var data string
		if err := rows.Scan(&seq, &data); err != nil {
			return nil, fmt.Errorf("reading journal run: %w", err)
		}
		var r store.ChangeRun
		if err := decodeStored(data, &r); err != nil {
			return nil, err
		}
		r.Seq = seq
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading journal runs of %s: %w", repo, err)
	}
	return out, nil
}

func refSnapshots(ctx context.Context, db queryer, repo string) ([]store.RefSnapshot, error) {
	out, err := read[store.RefSnapshot](ctx, db, "SELECT payload FROM ref_snapshots WHERE repository=$1 ORDER BY namespace", repo)
	if err != nil {
		return nil, fmt.Errorf("reading ref snapshots of %s: %w", repo, err)
	}
	return out, nil
}

func pins(ctx context.Context, db queryer, repo string) ([]store.Pin, error) {
	out, err := read[store.Pin](ctx, db, "SELECT payload FROM change_pins WHERE repository=$1 ORDER BY name", repo)
	if err != nil {
		return nil, fmt.Errorf("reading pins of %s: %w", repo, err)
	}
	return out, nil
}

// ChangeRuns lists the kept journal runs, oldest first.
func (s *DB) ChangeRuns(ctx context.Context, repo string) ([]store.ChangeRun, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return changeRuns(ctx, tx, repo)
}

// Pins lists the repository's pins by name.
func (s *DB) Pins(ctx context.Context, repo string) ([]store.Pin, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return pins(ctx, tx, repo)
}

// RefSnapshots lists the repository's ref snapshots by namespace.
func (s *DB) RefSnapshots(ctx context.Context, repo string) ([]store.RefSnapshot, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return refSnapshots(ctx, tx, repo)
}

// Pin pins a kept journal run and seeds its overlay from the entries after
// it: for each key, the before payload of its earliest later entry.
func (s *DB) Pin(ctx context.Context, repo string, seq int64, name string, limit int) (store.Pin, error) {
	var pin store.Pin
	if name == "" || strings.ContainsRune(name, 0) || len(name) > 200 {
		return pin, fmt.Errorf("pin name must be 1 to 200 bytes without NUL")
	}
	if limit <= 0 {
		limit = store.DefaultMaxPins
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pin, fmt.Errorf("pinning %s: %w", name, err)
	}
	defer tx.Rollback()
	runs, err := changeRuns(ctx, tx, repo)
	if err != nil {
		return pin, err
	}
	var run *store.ChangeRun
	for i := range runs {
		if runs[i].Seq == seq {
			run = &runs[i]
		}
	}
	if run == nil {
		if len(runs) > 0 && seq > 0 && seq < runs[0].Seq {
			return pin, fmt.Errorf("pinning run %d: %w", seq, store.ErrBaseExpired)
		}
		return pin, fmt.Errorf("pinning run %d: %w", seq, store.ErrBaseUnknown)
	}
	existing, err := pins(ctx, tx, repo)
	if err != nil {
		return pin, err
	}
	for _, p := range existing {
		if p.Name == name {
			return pin, fmt.Errorf("pinning %s: %w", name, store.ErrPinExists)
		}
	}
	if len(existing) >= limit {
		return pin, fmt.Errorf("pinning %s (%d pins): %w", name, len(existing), store.ErrPinLimit)
	}
	pin = store.Pin{ID: rand.Text(), Name: name, Seq: seq, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Run: *run}
	data, err := payload(pin)
	if err != nil {
		return store.Pin{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO change_pins(repository,id,name,seq,payload) VALUES($1,$2,$3,$4,$5)", repo, pin.ID, name, seq, data); err != nil {
		return store.Pin{}, fmt.Errorf("pinning %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO change_pin_entries(repository,pin,record,key,before) SELECT e.repository,$2,e.record,e.key,e.before FROM change_entries e WHERE e.repository=$1 AND e.seq>$3 AND e.seq=(SELECT MIN(x.seq) FROM change_entries x WHERE x.repository=e.repository AND x.record=e.record AND x.key=e.key AND x.seq>$3)", repo, pin.ID, seq); err != nil {
		return store.Pin{}, fmt.Errorf("seeding pin %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return store.Pin{}, fmt.Errorf("pinning %s: %w", name, err)
	}
	return pin, nil
}

// Unpin deletes a pin and its overlay.
func (s *DB) Unpin(ctx context.Context, repo, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("unpinning %s: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM change_pin_entries WHERE repository=$1 AND pin=$2", repo, id); err != nil {
		return fmt.Errorf("unpinning %s: %w", id, err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM change_pins WHERE repository=$1 AND id=$2", repo, id)
	if err != nil {
		return fmt.Errorf("unpinning %s: %w", id, err)
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return fmt.Errorf("unpinning %s: %w", id, store.ErrPinUnknown)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("unpinning %s: %w", id, err)
	}
	return nil
}

// DropRefSnapshot deletes a ref snapshot and its namespace.
func (s *DB) DropRefSnapshot(ctx context.Context, repo, namespace string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dropping ref snapshot %s: %w", namespace, err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ref_snapshots WHERE repository=$1 AND namespace=$2", repo, namespace).Scan(&n); err != nil {
		return fmt.Errorf("dropping ref snapshot %s: %w", namespace, err)
	}
	if n == 0 {
		return fmt.Errorf("dropping ref snapshot %s: %w", namespace, store.ErrRefUnknown)
	}
	if err := dropNamespace(ctx, tx, repo, namespace); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dropping ref snapshot %s: %w", namespace, err)
	}
	return nil
}
