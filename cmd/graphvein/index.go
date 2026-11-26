package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/externalindex"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// openStore opens the writer store: Postgres when GRAPHVEIN_POSTGRES_URL is
// set, otherwise the SQLite file at sqlitePath.
func openStore(ctx context.Context, root string) (store.Store, error) {
	if url := os.Getenv("GRAPHVEIN_POSTGRES_URL"); url != "" {
		return postgres.Open(ctx, url)
	}
	return sqlite.Open(ctx, sqlitePath(root))
}

// sqlitePath is the SQLite store for root: GRAPHVEIN_SQLITE_PATH, or
// .graphvein/index.db under root.
func sqlitePath(root string) string {
	if p := os.Getenv("GRAPHVEIN_SQLITE_PATH"); p != "" {
		return p
	}
	return filepath.Join(root, sqlite.DefaultPath)
}

// storeDir is the directory of the store openStore opens for root, where
// an index run writes its temporary snapshot: the SQLite file's directory,
// or "" for Postgres, which has no local files (indexrun.Options.SnapshotDir).
func storeDir(root string) string {
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") != "" {
		return ""
	}
	p := sqlitePath(root)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Dir(p)
}

// openLocker returns the index lock of the store openStore opens, and how
// to name it in messages: the lock file, or the Postgres advisory lock.
func openLocker(root, repository string) (store.IndexLocker, string) {
	if url := os.Getenv("GRAPHVEIN_POSTGRES_URL"); url != "" {
		return postgres.IndexLocker(url, repository), "Postgres advisory lock"
	}
	p := sqlitePath(root)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return sqlite.IndexLocker(p), sqlite.LockPath(p)
}

func indexCommand(command string, args []string, stdout, stderr io.Writer) int {
	return indexCommandWithEmitter(command, args, stdout, stderr, index.Emit)
}

func indexCommandWithEmitter(command string, args []string, stdout, stderr io.Writer, emitSnapshot func(context.Context, string, store.Store, io.Writer, index.Options) (index.Summary, error)) int {
	if hasProjectFlag(args) {
		return projectIndexCommand(command, args, stdout, stderr, emitSnapshot)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	emit := flags.String("emit", "", "also write the complete NDJSON snapshot")
	repository := flags.String("repository", "", "logical repository ID (defaults to directory name)")
	workers := flags.Int("workers", 0, "parser workers (default min(4, CPUs))")
	timeout := flags.Duration("timeout", indexrun.DefaultTimeout, "base per-file parsing timeout, scaled with file size up to 12 times (ADR 0090)")
	maxSize := flags.Int64("max-file-size", indexrun.DefaultMaxFileSize, "maximum file size to parse")
	indexer := ""
	indexerTimeout := time.Duration(0)
	shapeDepth := 0
	if command == "index" {
		flags.IntVar(&shapeDepth, "shape-depth", 0, fmt.Sprintf("route shape call-following depth, %d to %d (default: graphvein.yaml index.shape_depth, else %d)", routeshape.MinDepth, routeshape.MaxDepthLimit, routeshape.DefaultDepth))
		flags.StringVar(&indexer, "indexer", "", "external indexer executable path")
		flags.DurationVar(&indexerTimeout, "indexer-timeout", 0, "external process timeout (0 disables)")
	}
	// Accept the documented path-first invocation as well as flag-first syntax.
	root := "."
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		root = args[0]
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: graphvein "+command+" [path] [flags]")
		return 2
	}
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	var override *string
	conflict := ""
	shapeDepthSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "shape-depth" {
			shapeDepthSet = true
		}
		if f.Name == "repository" {
			override = repository
		}
		if indexer != "" && (f.Name == "workers" || f.Name == "timeout" || f.Name == "max-file-size" || f.Name == "shape-depth") {
			conflict = f.Name
		}
	})
	if conflict != "" {
		fmt.Fprintf(stderr, "--%s cannot be used with --indexer\n", conflict)
		return 2
	}
	if indexerTimeout < 0 {
		fmt.Fprintln(stderr, "--indexer-timeout must be nonnegative")
		return 2
	}
	if shapeDepthSet && !routeshape.ValidDepth(shapeDepth) {
		fmt.Fprintf(stderr, "--shape-depth must be from %d to %d\n", routeshape.MinDepth, routeshape.MaxDepthLimit)
		return 2
	}
	if *workers < 0 || *timeout <= 0 || *maxSize <= 0 {
		fmt.Fprintln(stderr, "workers must be nonnegative; timeout and max-file-size must be positive")
		return 2
	}
	if indexer != "" {
		var err error
		indexer, err = externalindex.ResolveExecutable(indexer)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	*repository, err = config.ResolveRepository(absolute, override)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx := context.Background()
	if command == "status" {
		s, err := openStore(ctx, absolute)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer s.Close()
		changed, err := index.Freshness(ctx, absolute, *repository, s)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		runs, err := s.Runs(ctx, *repository)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(runs) == 0 {
			fmt.Fprintln(stdout, "Not indexed")
		} else {
			fmt.Fprintf(stdout, "%d files changed since last index\n", len(changed))
			record := ""
			if last := store.LatestRun(runs); last != nil {
				record = last.Settings
			}
			fmt.Fprintln(stdout, settingsLine(config.IndexSettingsAt(record, absolute)))
		}
		for _, p := range changed {
			fmt.Fprintln(stdout, p)
		}
		return 0
	}
	// Both index paths run under the index lock shared with graphvein serve
	// (ADR 0052): taken before the scan, released after the writer store
	// closed. Indexing without a provable lock is not allowed.
	locker, lockName := openLocker(absolute, *repository)
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		fmt.Fprintf(stderr, "another index run holds the lock for this store (%s); try again when it has finished\n", lockName)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "taking the index lock (%s): %v\n", lockName, err)
		return 1
	}
	defer lease.Release()
	open := func(ctx context.Context) (store.Store, error) { return openStore(ctx, absolute) }
	if indexer != "" {
		var writes store.Writes
		err := indexrun.WithStore(ctx, lease, open, indexrun.Hooks{}, func(ctx context.Context, s store.Store) error {
			var dest *os.File
			var copyStream io.Writer
			if *emit != "" {
				var err error
				dest, err = os.Create(*emit)
				if err != nil {
					return err
				}
				copyStream = dest
			}
			var runErr error
			writes, runErr = externalindex.Run(ctx, indexer, absolute, *repository, indexerTimeout, s, copyStream)
			if dest != nil {
				if closeErr := dest.Close(); runErr == nil {
					runErr = closeErr
				}
			}
			return runErr
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "External snapshot ingested; writes: files=%d nodes=%d edges=%d deleted=%d\n", writes.Files, writes.Nodes, writes.Edges, writes.Deleted)
		return 0
	}
	result, err := indexrun.Run(ctx, indexrun.Options{
		Root:        absolute,
		Repository:  *repository,
		Lease:       lease,
		OpenStore:   open,
		Parser:      parse.Options{Workers: *workers, Timeout: *timeout},
		MaxFileSize: *maxSize,
		ShapeDepth:  shapeDepth,
		EmitPath:    *emit,
		Emit:        emitSnapshot,
		SnapshotDir: storeDir(absolute),
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	summary, writes := result.Summary, result.Writes
	fmt.Fprintf(stdout, "Files: %d %v; nodes: %d; edges: %d; diagnostics: %d; dropped: %d\nParsed: %d; reused: %d; writes: files=%d nodes=%d edges=%d deleted=%d\nTimings: scan=%s parse/extract=%s index=%s ingest=%s\n", summary.Files, summary.Languages, summary.Nodes, summary.Edges, summary.Diagnostics, summary.Dropped, summary.Parsed, summary.Reused, writes.Files, writes.Nodes, writes.Edges, writes.Deleted, summary.Scan, summary.Parse, summary.Total, result.Ingest)
	return 0
}

// settingsLine is graphvein status's line on the settings the last index
// run read (ADR 0088, row ST-46): a changed key is applied by a re-index;
// unknown is never reported as unchanged.
func settingsLine(c config.IndexSettings) string {
	switch c.State {
	case config.IndexSettingsUnchanged:
		return "Settings unchanged since last index"
	case config.IndexSettingsChanged:
		line := "Settings changed since last index: " + strings.Join(c.ChangedKeys, ", ") + " (re-index to apply)"
		if len(c.UnknownKeys) > 0 {
			line += "; not recorded: " + strings.Join(c.UnknownKeys, ", ")
		}
		return line
	}
	line := "Settings since last index: unknown: " + c.Reason
	if len(c.UnknownKeys) > 0 {
		line += " (" + strings.Join(c.UnknownKeys, ", ") + ")"
	}
	return line
}
