// Package indexrun runs one built-in index of a repository into a store
// while the caller holds the index lock. graphvein index and graphvein
// serve's re-index both use it, so a run started from the UI writes exactly
// what graphvein index with default flags writes (ADR 0052).
package indexrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
	"github.com/graphvein/graphvein/internal/store"
)

// LockWait is how long graphvein index and the server wait for the index
// lock. It covers a SQLite state probe and a previous holder's close; a
// longer wait would make a held lock look like a hang (ADR 0052).
const LockWait = 2 * time.Second

// Defaults of graphvein index's --timeout and --max-file-size, which a run
// started from the server always uses.
const (
	DefaultTimeout     = 5 * time.Second
	DefaultMaxFileSize = 2 << 20
)

// Run's phases besides Emit's own, in run order: PhaseStarting, then
// index.PhaseScanning, PhaseCache, PhaseParsing and PhaseLinking, then
// PhaseWriting, PhaseCommitting and PhaseFinishing.
const (
	// PhaseStarting is opening the writer store (and migrating it), before
	// the scan.
	PhaseStarting = "starting"
	// PhaseWriting is ingest: the snapshot is written into the run
	// transaction.
	PhaseWriting = "writing"
	// PhaseCommitting is the lease check and the commit.
	PhaseCommitting = "committing"
	// PhaseFinishing is closing the writer store (SQLite: the WAL
	// checkpoint), after a commit or a failure, still under the lock.
	PhaseFinishing = "finishing"
)

// Units of counted progress.
const (
	UnitFiles   = "files"
	UnitRecords = "records"
)

// Progress is one report of a run. Done, Total and Unit are set only when
// Counted is true, and then both numbers come from the run itself: files in
// the cache and parsing phases, records in the writing phase. Every other
// phase has no known total.
type Progress struct {
	Phase       string
	Done, Total int
	Unit        string
	Counted     bool
}

// ErrLockLost reports that the lease could not be proven held right before
// the commit; the run was rolled back.
var ErrLockLost = errors.New("index lock lost before commit")

// StoreError reports that the writer store could not be opened, so nothing
// ran. Callers tell it apart from an indexing failure with errors.As.
type StoreError struct{ Err error }

func (e *StoreError) Error() string { return e.Err.Error() }

// Unwrap returns the opening error.
func (e *StoreError) Unwrap() error { return e.Err }

// EmitFunc writes the NDJSON snapshot of root; index.Emit in production.
// Tests inject faults through it.
type EmitFunc func(context.Context, string, store.Store, io.Writer, index.Options) (index.Summary, error)

// Options configures one run. Lease and OpenStore are required.
type Options struct {
	// Root is the absolute repository directory.
	Root string
	// Repository is the logical repository ID.
	Repository string
	// Lease is the held index lock. Run checks it before the commit and
	// never releases it: the caller releases it after Run returned, so the
	// writer store's close happens under the lock.
	Lease store.IndexLease
	// OpenStore opens the writer store; Run closes it.
	OpenStore func(context.Context) (store.Store, error)
	// Parser and MaxFileSize are graphvein index's --workers, --timeout and
	// --max-file-size.
	Parser      parse.Options
	MaxFileSize int64
	// ShapeDepth is graphvein index --shape-depth (0: graphvein.yaml or
	// the default); see index.Options.ShapeDepth.
	ShapeDepth int
	// EmitPath, when set, receives a copy of the snapshot after it
	// committed (graphvein index --emit).
	EmitPath string
	// Progress, when set, receives phase and count reports. It is called
	// from the running goroutine and must return quickly.
	Progress func(Progress)
	// OnCommit, when set, is called right after the run transaction
	// committed, before the writer store closes, with the committed run.
	OnCommit func(store.Run)
	// Emit replaces index.Emit; nil means index.Emit.
	Emit EmitFunc
	// SnapshotDir is the directory of the store's own files (for SQLite
	// the directory of index.db), where the run writes its temporary
	// snapshot. Empty means there is no file store (Postgres): the
	// snapshot goes to the OS temporary directory. A directory inside
	// Root but outside Root/.graphvein is not used either, so the scan
	// never sees the snapshot and an interrupted run cannot leave it in
	// the repository; see SnapshotLocation.
	SnapshotDir string
}

// Result describes a committed run.
type Result struct {
	Summary index.Summary
	Writes  store.Writes
	// Run is the committed run, the same ID and finish time as its
	// index_runs row.
	Run store.Run
	// Ingest is the time spent in ingest, commit included.
	Ingest time.Duration
}

// Run indexes opt.Root with the built-in indexer and commits the snapshot
// as one run transaction. It opens the writer store, writes the snapshot to
// a temporary file next to the store (see Options.SnapshotDir), ingests it, and closes the store before it returns. Any failure rolls the
// transaction back and leaves the previous run in place.
//
// Run also records the run's git state (HEAD's commit and whether the
// indexed files are exactly that commit) and applies the journal limits of
// the root's graphvein.yaml (ADR 0056).
func Run(ctx context.Context, opt Options) (Result, error) {
	// A configuration error is reported by Emit, inside the run, as before;
	// the journal limits then stay at their defaults.
	cfg, err := config.Load(opt.Root)
	if err != nil {
		cfg.Changes = config.DefaultChanges()
	}
	return run(ctx, opt, runExtra{
		runOptions: store.RunOptions{Journal: store.JournalLimits{Runs: cfg.Changes.JournalRuns, Budget: cfg.Changes.JournalBudget}},
		gitState:   true,
	})
}

// runExtra is what a ref run adds to Run.
type runExtra struct {
	runOptions    store.RunOptions
	cacheFallback string
	configRoot    string
	// diagnostics are added to the snapshot before its commit.
	diagnostics []contract.Diagnostic
	// gitState reads the root's git state into the run metadata.
	gitState bool
}

func run(ctx context.Context, opt Options, extra runExtra) (Result, error) {
	var result Result
	report := func(p Progress) {
		if opt.Progress != nil {
			opt.Progress(p)
		}
	}
	emit := opt.Emit
	if emit == nil {
		emit = index.Emit
	}
	err := WithStore(ctx, opt.Lease, opt.OpenStore, Hooks{Progress: opt.Progress, OnCommit: func(run store.Run) {
		result.Run = run
		if opt.OnCommit != nil {
			opt.OnCommit(run)
		}
	}}, func(ctx context.Context, s store.Store) error {
		stream, cleanup, err := createSnapshot(SnapshotLocation(opt.Root, opt.SnapshotDir))
		if err != nil {
			return err
		}
		defer cleanup()
		lines := &lineCounter{w: stream}
		result.Summary, err = emit(ctx, opt.Root, s, lines, index.Options{
			Repository:    opt.Repository,
			Parser:        opt.Parser,
			MaxFileSize:   opt.MaxFileSize,
			ShapeDepth:    opt.ShapeDepth,
			CacheFallback: extra.cacheFallback,
			ConfigRoot:    extra.configRoot,
			Progress: func(p index.Progress) {
				out := Progress{Phase: p.Phase}
				if p.Counted {
					out.Done, out.Total, out.Unit, out.Counted = p.Done, p.Total, UnitFiles, true
				}
				report(out)
			},
		})
		if err != nil {
			return err
		}
		if _, err = stream.Seek(0, io.SeekStart); err != nil {
			return err
		}
		runOptions := extra.runOptions
		// The index inputs the emitter read (empty from an emitter that
		// reports none: unknown).
		runOptions.Settings = result.Summary.Settings.Record()
		if extra.gitState {
			files, err := snapshotFiles(stream)
			if err != nil {
				return err
			}
			state := scan.ReadGitState(ctx, opt.Root, files)
			runOptions.Commit, runOptions.Dirty = state.Commit, state.Dirty
			if _, err = stream.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		target := store.WithRunOptions(s, runOptions)
		if len(extra.diagnostics) > 0 {
			target = &diagnosticStore{Store: target, diagnostics: extra.diagnostics}
		}
		total := lines.n
		report(Progress{Phase: PhaseWriting, Total: total, Unit: UnitRecords, Counted: true})
		started := time.Now()
		result.Writes, err = ingest.StreamWithOptions(ctx, stream, target, ingest.Options{Progress: func(records int) {
			if records > total {
				// A final line without a newline: the count is not proven.
				report(Progress{Phase: PhaseWriting})
				return
			}
			report(Progress{Phase: PhaseWriting, Done: records, Total: total, Unit: UnitRecords, Counted: true})
		}})
		result.Ingest = time.Since(started)
		if err != nil {
			return err
		}
		if opt.EmitPath != "" {
			if err := copySnapshot(stream, opt.EmitPath); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

// SnapshotLocation is the directory where a run of root writes its
// temporary snapshot, given the store's directory dir ("" for no file
// store): dir itself when it is outside root or inside root/.graphvein
// (the default layout, which Git and the scanner ignore), and "" (the OS
// temporary directory) otherwise. Next to the store keeps the snapshot on
// the disk the user chose for index data; never elsewhere in the
// repository, so indexing does not dirty the indexed tree and the scan
// cannot read a snapshot that is being written.
func SnapshotLocation(root, dir string) string {
	if dir == "" {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	// filepath.Rel compares case-insensitively on Windows and fails for
	// another volume, which is outside root.
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return absDir
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	if first == ".graphvein" {
		return absDir
	}
	return ""
}

// createSnapshot creates the temporary snapshot file in dir ("" is the OS
// temporary directory), creating dir when it is missing. cleanup closes and
// removes the file (closed first: Windows cannot remove an open file) and
// then removes, innermost first, every directory createSnapshot created,
// stopping at the first that is not empty, so a run never leaves an empty
// directory behind.
func createSnapshot(dir string) (stream *os.File, cleanup func(), err error) {
	var created []string
	if dir != "" {
		for d := dir; ; {
			if _, err := os.Lstat(d); !errors.Is(err, fs.ErrNotExist) {
				break
			}
			created = append(created, d)
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
		if len(created) > 0 {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, nil, fmt.Errorf("creating the snapshot directory %s: %w", dir, err)
			}
		}
	}
	removeCreated := func() {
		for _, d := range created {
			if os.Remove(d) != nil {
				return
			}
		}
	}
	stream, err = os.CreateTemp(dir, "snapshot-*.ndjson")
	if err != nil {
		removeCreated()
		return nil, nil, fmt.Errorf("creating the temporary snapshot: %w", err)
	}
	cleanup = func() {
		stream.Close()
		if err := os.Remove(stream.Name()); err != nil {
			slog.Warn("removing the temporary snapshot failed", "path", stream.Name(), "error", err)
		}
		removeCreated()
	}
	return stream, cleanup, nil
}

// copySnapshot copies the committed snapshot to path.
func copySnapshot(stream *os.File, path string) error {
	if _, err := stream.Seek(0, io.SeekStart); err != nil {
		return err
	}
	dest, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(dest, stream)
	if closeErr := dest.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Hooks observe a WithStore run.
type Hooks struct {
	// Progress receives the committing and finishing phases.
	Progress func(Progress)
	// OnCommit receives the committed run.
	OnCommit func(store.Run)
}

// WithStore opens the writer store, calls fn with it and closes it. The
// store passed to fn checks lease right before every commit and fails the
// commit with ErrLockLost when the lease cannot be proven, so the
// transaction rolls back. graphvein index --indexer uses it directly; Run
// uses it for the built-in indexer.
func WithStore(ctx context.Context, lease store.IndexLease, open func(context.Context) (store.Store, error), hooks Hooks, fn func(context.Context, store.Store) error) error {
	if lease == nil || open == nil {
		return errors.New("index run needs a held index lock and a store opener")
	}
	if hooks.Progress != nil {
		hooks.Progress(Progress{Phase: PhaseStarting})
	}
	s, err := open(ctx)
	if err != nil {
		return &StoreError{Err: err}
	}
	// Deferred so the store also closes, under the caller's lock, when fn
	// panics and the caller recovers.
	defer func() {
		if hooks.Progress != nil {
			hooks.Progress(Progress{Phase: PhaseFinishing})
		}
		if closeErr := s.Close(); closeErr != nil {
			// A committed run is durable already; a failed close only
			// matters for the files it leaves (SQLite -wal), so it is logged.
			slog.Warn("closing the writer store failed", "error", closeErr)
		}
	}()
	return fn(ctx, &leaseStore{Store: s, lease: lease, hooks: hooks})
}

// leaseStore is a writer store whose transactions check the lease before
// they commit.
type leaseStore struct {
	store.Store
	lease store.IndexLease
	hooks Hooks
}

func (s *leaseStore) BeginRun(ctx context.Context, h contract.Header, options ...store.RunOptions) (store.Transaction, error) {
	tx, err := s.Store.BeginRun(ctx, h, options...)
	if err != nil {
		return nil, err
	}
	return &leaseTransaction{Transaction: tx, store: s}, nil
}

type leaseTransaction struct {
	store.Transaction
	store *leaseStore
}

// Commit reports the committing phase, checks the lease and commits. The
// lease is checked before the commit only; a session lost during the
// commit itself is not detected (ADR 0052 known gap).
func (t *leaseTransaction) Commit(ctx context.Context) error {
	hooks := t.store.hooks
	if hooks.Progress != nil {
		hooks.Progress(Progress{Phase: PhaseCommitting})
	}
	if err := t.store.lease.Check(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrLockLost, err)
	}
	if err := t.Transaction.Commit(ctx); err != nil {
		return err
	}
	if hooks.OnCommit != nil {
		hooks.OnCommit(t.Transaction.Run())
	}
	return nil
}

// lineCounter counts the newline-terminated lines written through it: the
// snapshot's record count, known only once Emit returned.
type lineCounter struct {
	w io.Writer
	n int
}

func (c *lineCounter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += bytes.Count(p[:n], []byte{'\n'})
	return n, err
}

// snapshotFiles reads the file records of a snapshot stream: path to git
// blob object id, which ReadGitState compares with HEAD's tree.
func snapshotFiles(stream io.Reader) (map[string]string, error) {
	files := map[string]string{}
	reader := bufio.NewReaderSize(stream, 1<<16)
	prefix := []byte(`{"type":"file"`)
	for {
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Long lines are nodes or edges; skip the rest of the line.
			for err == bufio.ErrBufferFull {
				_, err = reader.ReadSlice('\n')
			}
			if err == io.EOF {
				return files, nil
			}
			if err != nil {
				return nil, err
			}
			continue
		}
		if bytes.HasPrefix(line, prefix) {
			var f contract.File
			if e := json.Unmarshal(line, &f); e != nil {
				return nil, fmt.Errorf("reading snapshot file record: %w", e)
			}
			files[f.Path] = f.GitBlobOID
		}
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// diagnosticStore adds diagnostics to every run right before its commit: a
// ref run's materialisation diagnostics (symlinks, submodules, unwritable
// paths), which the scanner of the temporary directory cannot see.
type diagnosticStore struct {
	store.Store
	diagnostics []contract.Diagnostic
}

func (s *diagnosticStore) BeginRun(ctx context.Context, h contract.Header, options ...store.RunOptions) (store.Transaction, error) {
	tx, err := s.Store.BeginRun(ctx, h, options...)
	if err != nil {
		return nil, err
	}
	return &diagnosticTransaction{Transaction: tx, diagnostics: s.diagnostics}, nil
}

type diagnosticTransaction struct {
	store.Transaction
	diagnostics []contract.Diagnostic
}

func (t *diagnosticTransaction) Commit(ctx context.Context) error {
	for _, d := range t.diagnostics {
		if err := t.Transaction.Diagnostic(ctx, d); err != nil {
			return err
		}
	}
	return t.Transaction.Commit(ctx)
}

// Config sources of a ref run (ADR 0056): the ref's own graphvein.yaml, the
// working tree's when the ref has none, or none.
const (
	ConfigFromRef         = "ref"
	ConfigFromWorkingTree = "working_tree"
	ConfigNone            = "none"
)

// RefOptions configures a ref run. Options.Root is the working tree of the
// repository the commit belongs to and Options.Repository its identifier;
// the caller holds that repository's index lock (Options.Lease).
type RefOptions struct {
	Options
	// Commit is the full commit id to index (gitref.Resolve or
	// gitref.MergeBase).
	Commit string
}

// RefResult describes a committed ref run.
type RefResult struct {
	Result
	// Namespace is the reserved repository identifier the ref's graph is
	// stored under.
	Namespace string
	// ConfigSource is ConfigFromRef, ConfigFromWorkingTree or ConfigNone.
	ConfigSource string
}

// RunRef indexes one commit into its ref namespace (ADR 0056): the commit's
// tree is materialised into a temporary directory (removed on every path),
// indexed with the built-in indexer reading the main repository's fact cache
// first, and committed as a ref run (no journal) that registers the ref
// snapshot. Settings come from the working tree's graphvein.yaml
// (changes.max_ref_snapshots); extraction settings from the ref's own
// graphvein.yaml when it has one.
func RunRef(ctx context.Context, opt RefOptions) (RefResult, error) {
	var result RefResult
	if store.ReservedRepository(opt.Repository) {
		return result, fmt.Errorf("ref run of %q: %w", opt.Repository, store.ErrReservedRepository)
	}
	cfg, err := config.Load(opt.Root)
	if err != nil {
		return result, err
	}
	tree, err := gitref.Materialise(ctx, opt.Root, opt.Commit)
	if err != nil {
		return result, err
	}
	defer tree.Remove()
	result.Namespace = store.RefNamespace(opt.Repository, opt.Commit)
	extra := runExtra{cacheFallback: opt.Repository, diagnostics: tree.Diagnostics}
	switch {
	case fileExists(filepath.Join(tree.Dir, "graphvein.yaml")):
		result.ConfigSource = ConfigFromRef
	case fileExists(filepath.Join(opt.Root, "graphvein.yaml")):
		result.ConfigSource = ConfigFromWorkingTree
		extra.configRoot = opt.Root
	default:
		result.ConfigSource = ConfigNone
	}
	extra.runOptions = store.RunOptions{Namespace: &store.Namespace{Repository: opt.Repository, Commit: opt.Commit, ConfigSource: result.ConfigSource, MaxSnapshots: cfg.Changes.MaxRefSnapshots}}
	inner := opt.Options
	// The rule is applied against the working tree, which may hold the
	// store, before Root becomes the materialised tree.
	inner.SnapshotDir = SnapshotLocation(opt.Root, opt.SnapshotDir)
	inner.Root = tree.Dir
	inner.Repository = result.Namespace
	inner.EmitPath = ""
	result.Result, err = run(ctx, inner, extra)
	return result, err
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
