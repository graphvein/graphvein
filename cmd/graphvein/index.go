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
	"github.com/graphvein/graphvein/internal/storeloc"
)

// openStore opens the writer store of root, the folder named on this
// command line: Postgres when GRAPHVEIN_POSTGRES_URL is set, otherwise the
// SQLite file the store resolver chooses (ADR 0093), whose folder is
// created owner-only first when the store is new.
func openStore(ctx context.Context, root string) (store.Store, error) {
	return commandLine(root).openWriter(ctx)
}

// sqlitePath is the SQLite store the resolver chooses for root (ADR
// 0093); it is an error for Postgres, which has no file.
func sqlitePath(root string) (string, error) {
	loc, err := locate(root)
	if err != nil {
		return "", err
	}
	if loc.Origin == storeloc.OriginPostgres {
		return "", errors.New("the index is in Postgres (GRAPHVEIN_POSTGRES_URL); it has no SQLite file")
	}
	return loc.Path, nil
}

// storeDir is the directory of the store openStore opens for root, where
// an index run writes its temporary snapshot: the SQLite file's directory,
// or "" for Postgres, which has no local files (indexrun.Options.SnapshotDir).
// A store that cannot be located is "" too; its run fails at the lock.
func storeDir(root string) string {
	return commandLine(root).dir()
}

// storeName is the file name of the store openStore opens for root, which
// names an index run's temporary snapshot so the next run can remove one a
// killed run left (indexrun.Options.StoreName), or "" for Postgres.
func storeName(root string) string {
	return commandLine(root).name()
}

// openLocker returns the index lock of the store openStore opens, and how
// to name it in messages: the lock file, or the Postgres advisory lock.
func openLocker(root, repository string) (store.IndexLocker, string) {
	return commandLine(root).locker(repository)
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
	timeout := flags.Duration("timeout", indexrun.DefaultTimeout, "base per-file parsing timeout, scaled with file size up to 12 times (ADR 0090); a file that times out is parsed again alone with the 12 times cap (T074)")
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
	// The first line says where the index is (ADR 0093); a folder with
	// two candidate stores is refused before anything is read.
	loc, err := locate(absolute)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, indexLine(loc, "repository", *repository))
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
	// A store move may have finished while this run waited for the lock:
	// the run then stops instead of creating a store at the old place.
	if again, err := locate(absolute); err != nil || !sameStore(again, loc) {
		if err == nil {
			err = fmt.Errorf("the index of %s moved to %s while this run waited for its lock; run graphvein index again", loc.Folder, again.Path)
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	trailer := indexTrailer(loc, "graphvein serve "+quoteArg(loc.Folder))
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
		printLines(stdout, trailer)
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
		StoreName:   storeName(absolute),
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	summary, writes := result.Summary, result.Writes
	fmt.Fprintf(stdout, "Files: %d %v; nodes: %d; edges: %d; diagnostics: %d; dropped: %d\nParsed: %d; reused: %d; writes: files=%d nodes=%d edges=%d deleted=%d\nTimings: scan=%s parse/extract=%s index=%s ingest=%s\n", summary.Files, summary.Languages, summary.Nodes, summary.Edges, summary.Diagnostics, summary.Dropped, summary.Parsed, summary.Reused, writes.Files, writes.Nodes, writes.Edges, writes.Deleted, summary.Scan, summary.Parse, summary.Total, result.Ingest)
	for _, line := range parseLines(summary) {
		fmt.Fprintln(stdout, line)
	}
	printLines(stdout, trailer)
	return 0
}

func printLines(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// parseLines are graphvein index's lines on files it could not parse,
// printed after the serial retry of timed-out parses (T074): the retry when
// one ran, and the files still not read, which make every count of the run
// incomplete. A run that read every file prints none.
func parseLines(s index.Summary) []string {
	var out []string
	if s.Retried > 0 {
		out = append(out, fmt.Sprintf("Parse retry: %s timed out and parsed again alone; %d read", fileCount(s.Retried), s.Recovered))
	}
	if s.ParseFailed == 0 {
		return out
	}
	reason := "parse failed"
	switch s.ParseTimedOut {
	case s.ParseFailed:
		reason = "parse timed out"
	case 0:
	default:
		reason = fmt.Sprintf("parse failed, %d of them timed out", s.ParseTimedOut)
	}
	return append(out, fmt.Sprintf("%s not read: %s (parse_failed); counts are incomplete, see graphvein serve's Couldn't read page", fileCount(s.ParseFailed), reason))
}

// fileCount is n with "file" or "files".
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
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
