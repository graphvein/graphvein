// Command graphvein exposes the local codebase mapping tool.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
	// reportURL is the public wrong-fact issue form, set at build time
	// (scripts/build.sh REPORT_URL, -X main.reportURL=...). It is the
	// public-repository switch of ADR 0089: empty, the default, means the
	// UI renders no "Report a wrong fact" flag.
	reportURL = ""
)

func main() {
	version, commit, buildDate = resolveVersion(version, commit, buildDate, debug.ReadBuildInfo)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// No arguments: the web UI, on every platform (graphvein help
		// lists the commands).
		ctx, stop := interruptContext()
		defer stop()
		return noArgsCommand(ctx, stdout, stderr)
	}
	if args[0] == "query" || args[0] == "callers" || args[0] == "impact" {
		return queryCommand(args[0], args[1:], stdout, stderr)
	}
	if args[0] == "project" {
		return projectCommand(args[1:], stdout, stderr)
	}
	if args[0] == "openapi" {
		return openapiCommand(args[1:], stdout, stderr)
	}
	if args[0] == "changes" {
		return changesCommand(args[1:], stdout, stderr)
	}
	if args[0] == "index" || args[0] == "status" {
		return indexCommand(args[0], args[1:], stdout, stderr)
	}
	if args[0] == "store" {
		return storeCommand(args[1:], stdinOf(), stdout, stderr)
	}
	if args[0] == "serve" {
		// Ctrl+C (os.Interrupt on every OS) stops the server gracefully.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return serveCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] == "mcp" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return mcpCommand(ctx, args[1:], stdinOf(), stdout, stderr)
	}
	if args[0] == "ingest" {
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: graphvein ingest <file.ndjson>")
			return 2
		}
		var locked *lockedError
		if err := ingestFile(args[1]); errors.As(err, &locked) {
			fmt.Fprintf(stderr, "another index run holds the lock for this store (%s); try again when it has finished\n", locked.name)
			return 1
		} else if err != nil {
			slog.New(slog.NewTextHandler(stderr, nil)).Error("ingest failed", "error", err)
			return 1
		}
		fmt.Fprintln(stdout, "Snapshot ingested")
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: graphvein <help|version|ingest>")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "graphvein %s\ncommit: %s\nbuild date: %s\n", version, commit, buildDate)
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, "Usage: graphvein [command]\n\nWithout a command, graphvein serves the workspace web UI at 127.0.0.1:7420 (or the next free port up to 7430) and opens the browser.\n\nCommands:\n  version  Print version, commit and build date\n  help     Show this help\n  index [path] [--emit file.ndjson] [--indexer executable] [--indexer-timeout duration]  Index a repository\n  status [path]  Report index freshness\n  query <term> [path] [--kind KIND] [--file PATH] [--limit N] [--json]  Find nodes by identity, name or qualified-name suffix\n  callers <target> [path] [--depth N] [--project DIR] [--json]  Show what calls a function or route\n  impact <target> [path] [--depth N] [--project DIR] [--json]  Show what depends on a target through calls, imports, references and routes\n  index --project DIR  Index every project member, then link them (status --project: members and link freshness)\n  changes [path] [--base previous|run:ID|pin:NAME|ref:BASE[..HEAD]] [--json]  Show what changed and what depends on it (also: changes impact ID, changes pin NAME, changes unpin NAME)\n  serve [path] [--addr 127.0.0.1:7420]  Serve the local web UI\n  mcp [path] [--project DIR] [--structured-only]  Serve the read-only MCP server on stdin/stdout\n  openapi [path] [--format json|yaml] [--out file]  Write an OpenAPI 3.1 document of the stored routes (--project DIR --repository MEMBER: one member)\n  ingest <file.ndjson>  Ingest a complete repository snapshot\n  project init --name NAME [--dir DIR] [REPOSITORY=]PATH...  Write graphvein-project.yaml for several repositories\n  project add [--manifest DIR] [REPOSITORY=]PATH  Add a repository to the project manifest\n  project suggest [--manifest DIR] [--json] [--accept N]  List consumes declarations proposed from env, Compose and dev-server proxy files; --accept N writes one\n  project forget [--manifest DIR] REPOSITORY  Remove a name the manifest no longer has (an old member name) from the project store\n  store where [path]  Show where a folder's index is kept\n  store move <path> [<folder>] [--yes]  Move a folder's index (default: to the per-user stores folder)\n  store list  List the indexes Graphvein knows, including ones whose folder is gone\n\nRepository: --repository, graphvein.yaml, then directory basename\nStorage: GRAPHVEIN_POSTGRES_URL; else GRAPHVEIN_SQLITE_PATH for the folder named on the command line; else <folder>/.graphvein/index.db when it exists; else the per-user stores folder (GRAPHVEIN_STORE_DIR replaces it; graphvein store where shows the place)")
	default:
		fmt.Fprintf(stderr, "unknown command %q; run graphvein help\n", args[0])
		return 2
	}
	return 0
}

// stdinOf is the standard input graphvein mcp reads; tests replace it to
// drive a session without a terminal.
var stdinOf = func() io.Reader { return os.Stdin }

// lockedError reports that another holder has the index lock; name is the
// lock file or "Postgres advisory lock", as graphvein index prints it.
type lockedError struct{ name string }

func (e *lockedError) Error() string { return "index lock held: " + e.name }

// ingestFile ingests a snapshot under the index lock shared with graphvein
// index and serve (ADR 0052), so an ingest never writes while another run
// does. path may be any file os.Open accepts, including a pipe such as
// /dev/stdin, so the stream is read exactly once (see ingestInput).
func ingestFile(path string) error {
	input, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening stream %s: %w", path, err)
	}
	defer input.Close()
	return ingestInput(path, input)
}

// ingestInput ingests the stream read from input; name labels errors. The
// Postgres lock is per repository, so the stream header names it: the
// header line is read through a buffer and handed back in front of the rest
// of input, so ingest consumes the very bytes whose header gave the lock
// key, without a second read (input may be a pipe).
func ingestInput(name string, input io.Reader) error {
	ctx := context.Background()
	var locker store.IndexLocker
	var lockName, lockRepository string
	var open func(context.Context) (store.Store, error)
	stream := input
	if url := os.Getenv("GRAPHVEIN_POSTGRES_URL"); url != "" {
		header, rest, err := readStreamHeader(input)
		if err != nil {
			return fmt.Errorf("reading the stream header of %s: %w", name, err)
		}
		stream, lockRepository = rest, header.Repository
		locker, lockName = postgres.IndexLocker(url, lockRepository), "Postgres advisory lock"
		open = func(ctx context.Context) (store.Store, error) { return postgres.Open(ctx, url) }
	} else {
		// The store of the current folder, located as graphvein index
		// locates it (ADR 0093).
		locker, lockName = openLocker(".", "")
		open = func(ctx context.Context) (store.Store, error) { return openStore(ctx, ".") }
	}
	defer locker.Close()
	lease, err := locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return &lockedError{name: lockName}
	}
	if err != nil {
		return fmt.Errorf("taking the index lock (%s): %w", lockName, err)
	}
	defer lease.Release()
	return indexrun.WithStore(ctx, lease, open, indexrun.Hooks{}, func(ctx context.Context, s store.Store) error {
		return ingestLocked(ctx, s, lockRepository, stream)
	})
}

// ingestLocked writes stream to s. When lockRepository is set (the
// Postgres lock is per repository), a stream whose header names another
// repository is refused before anything is written: the lock held must
// cover the repository the snapshot replaces.
func ingestLocked(ctx context.Context, s store.Store, lockRepository string, stream io.Reader) error {
	_, err := ingest.StreamWithOptions(ctx, stream, s, ingest.Options{Repository: lockRepository})
	return err
}

// readStreamHeader parses the first line of input as the stream header and
// returns it with a reader that yields input from its first byte: the
// consumed header line, then the rest through the same buffer. The line is
// bounded like any contract record, so an oversized first line fails here
// as it would in ingest.
func readStreamHeader(input io.Reader) (*contract.Header, io.Reader, error) {
	buffered := bufio.NewReader(input)
	var line []byte
	for {
		chunk, err := buffered.ReadSlice('\n')
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull && len(line) <= contract.MaxRecordBytes+1 {
			continue
		}
		if err != nil && err != bufio.ErrBufferFull && err != io.EOF {
			return nil, nil, err
		}
		break
	}
	event, err := contract.NewReader(bytes.NewReader(line)).Next()
	if err != nil {
		return nil, nil, err
	}
	header, ok := event.Record.(*contract.Header)
	if !ok || header.Repository == "" {
		return nil, nil, fmt.Errorf("line 1: invalid stream header")
	}
	return header, io.MultiReader(bytes.NewReader(line), buffered), nil
}
