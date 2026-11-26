package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/server/ui"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/workspace"
)

const defaultServeAddr = "127.0.0.1:7420"

// serveCommand runs graphvein serve until ctx ends (run wires Ctrl+C). It
// takes the context so tests can stop the server without signals.
func serveCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if hasProjectFlag(args) {
		return projectServeCommand(ctx, args, stdout, stderr)
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", "", "logical repository ID (defaults to directory name)")
	addr := flags.String("addr", defaultServeAddr, "loopback IP address and port to listen on (port 0 picks a free port)")
	// Accept path-first and flag-first invocations, like status.
	root, pathFirst := ".", false
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		root, pathFirst = args[0], true
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && pathFirst) {
		fmt.Fprintln(stderr, "usage: graphvein serve [path] [--repository id] [--addr 127.0.0.1:7420]; without a path or --repository, serve serves the workspace")
		return 2
	}
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	if !loopbackLiteral(*addr) {
		fmt.Fprintln(stderr, "--addr must be a loopback IP literal such as 127.0.0.1:7420 or [::1]:7420")
		return 2
	}
	var override *string
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "repository" {
			override = repository
		}
	})
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	registry := openRegistry(logger)
	if !pathFirst && flags.NArg() == 0 && override == nil {
		// No path and no --repository: serve the workspace registry
		// (ADR 0087, D2).
		return serveWorkspace(ctx, *addr, registry, nil, stdout, stderr, logger, nil)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// A graphvein.yaml that does not load does not stop serve (unlike an
	// index run): the UI shows the decoder's message on every view and
	// the Settings page can fix the file (ADR 0088).
	var configErr error
	*repository, configErr, err = config.ResolveServeRepository(absolute, override)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if configErr != nil {
		fmt.Fprintf(stderr, "%v\nServing anyway: the web UI shows this error and its Settings page can fix the file; graphvein index refuses until it loads.\n", configErr)
	}
	if err := sharedStoreCheck(registry, absolute, *repository, server.RepositoryClaims(*repository, resolvedFolder(absolute))); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := openReadStore(ctx, absolute)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// Deferred so it runs after Serve returned, which happens only once the
	// last request handler finished with the store.
	defer s.Close()
	// Re-index from the UI uses the same lock and writer store as
	// graphvein index (ADR 0052). Serve returns only after a running job
	// released the lock, so closing the locker afterwards is safe.
	locker, _ := openLocker(absolute, *repository)
	defer locker.Close()
	current := &server.WorkspaceIndex{
		Entry:   register(registry, workspace.KindRepository, *repository, absolute, logger),
		Options: repositoryOptions(absolute, *repository, s, locker),
	}
	return serveWorkspace(ctx, *addr, registry, current, stdout, stderr, logger, nil)
}

// repositoryOptions are the options of a single-repository index: the
// read store, the index lock and the writer store of a re-index.
func repositoryOptions(root, repository string, s store.Store, locker store.IndexLocker) server.Options {
	return server.Options{
		Root:       root,
		Repository: repository,
		Store:      s,
		Locker:     locker,
		OpenWriter: func(ctx context.Context) (store.Store, error) { return openStore(ctx, root) },
		StoreDir:   storeDir(root),
	}
}

// openRegistry opens the workspace registry. Without a configuration
// directory the server still serves the index it was started on.
func openRegistry(logger *slog.Logger) *workspace.Registry {
	path, err := workspace.DefaultPath()
	if err != nil {
		logger.Warn("the workspace registry is unavailable", "error", err)
		return workspace.Unavailable(err)
	}
	return workspace.Open(path)
}

// resolvedFolder is folder as the registry stores it (links resolved),
// or cleaned when it cannot be resolved.
func resolvedFolder(folder string) string {
	if path, err := workspace.Resolve(folder); err == nil {
		return path
	}
	return filepath.Clean(folder)
}

// sharedStoreCheck refuses to serve an index whose repository or project
// name another registered entry, in another folder, uses in the one
// database GRAPHVEIN_POSTGRES_URL names (claim row WS-57): either folder's
// facts could be answered for the other. Without that variable every
// folder has its own store (GRAPHVEIN_SQLITE_PATH is used by the
// command-line index only), so nothing is shared. A registry that cannot
// be read refuses nothing here; the server reports it.
func sharedStoreCheck(registry *workspace.Registry, folder, name string, claims []server.StoreClaim) error {
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" {
		return nil
	}
	entries, err := registry.Entries()
	if err != nil {
		return nil
	}
	if c := server.FindStoreConflict(resolvedFolder(folder), claims, entries, server.EntryClaims); c != nil {
		return fmt.Errorf("refusing to serve (shared_store_conflict): %s", c.Message(name))
	}
	return nil
}

// register records the index named on the command line in the registry
// (ADR 0087) and returns its entry. A registry that cannot be written
// is logged; serving goes on.
func register(registry *workspace.Registry, kind workspace.Kind, name, folder string, logger *slog.Logger) workspace.Entry {
	path := resolvedFolder(folder)
	e := workspace.Entry{Kind: kind, Name: name, Path: path, ID: workspace.EntryID(kind, path, name)}
	stored, _, err := registry.Add(e)
	if err != nil {
		logger.Warn("could not add the index to the workspace registry", "error", err)
		return e
	}
	return stored
}

// serveWorkspace serves the registry, with current (the index named on
// the command line, owned by the caller) as the index of requests without
// ws=.
//
// hooks is nil for graphvein serve; graphvein with no arguments uses it to
// open the browser once the server listens and to explain a busy port.
func serveWorkspace(ctx context.Context, addr string, registry *workspace.Registry, current *server.WorkspaceIndex, stdout, stderr io.Writer, logger *slog.Logger, hooks *serveHooks) int {
	workDir, _ := os.Getwd()
	srv, err := server.Listen(ctx, addr, server.Options{
		Version:   version,
		Assets:    ui.Assets,
		Logger:    logger,
		ReportURL: reportIssueURL(logger),
		Workspace: &server.WorkspaceOptions{
			Registry:      registry,
			Current:       current,
			Open:          openWorkspaceIndex,
			StoreLocation: storeLocation,
			WorkDir:       workDir,
			SharedStore:   os.Getenv("GRAPHVEIN_POSTGRES_URL") != "",
		},
	})
	if err != nil {
		if hooks != nil && hooks.listenFailed != nil {
			hooks.listenFailed(err)
			return 1
		}
		fmt.Fprintf(stderr, "%v; if the port is in use, try --addr 127.0.0.1:0\n", err)
		return 1
	}
	entries, _ := registry.Entries()
	others := 0
	for _, e := range entries {
		if current == nil || e.ID != current.Entry.ID {
			others++
		}
	}
	if others > 0 && os.Getenv("GRAPHVEIN_SQLITE_PATH") != "" && os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" {
		logger.Warn("GRAPHVEIN_SQLITE_PATH applies only to the index named on the command line; the other workspace entries are not opened while it is set")
	}
	more := ""
	if others > 0 {
		more = fmt.Sprintf(" (+%d in the workspace)", others)
	}
	switch {
	case current == nil:
		fmt.Fprintf(stdout, "Serving the workspace (%d entries) at %s\n", others, srv.URL())
	case current.Options.Project != nil:
		fmt.Fprintf(stdout, "Serving project %s (%d members)%s at %s\n", current.Options.Project.Project.Name, len(current.Options.Project.Project.Members), more, srv.URL())
	default:
		// The repository resolved now, which a rename may have changed
		// since the folder was registered (its entry keeps the old name).
		fmt.Fprintf(stdout, "Serving %s%s at %s\n", current.Options.Repository, more, srv.URL())
	}
	if hooks != nil && hooks.ready != nil {
		hooks.ready(srv.URL())
	}
	if err := srv.Serve(ctx); err != nil {
		logger.Error("server failed", "error", err)
		return 1
	}
	return 0
}

// serveHooks lets a caller of serveWorkspace act on its two outcomes
// without changing what graphvein serve prints: ready runs once the server
// listens (with its URL), listenFailed replaces the generic message when
// the address cannot be bound.
type serveHooks struct {
	ready        func(url string)
	listenFailed func(err error)
}

// openWorkspaceIndex opens a registry entry's index as serve <path> or
// serve --project would, with the store located from the folder and the
// environment as the CLI locates it. Without create, a missing SQLite
// store is server.ErrNoIndex; with create (adding a folder) an empty store
// is created first, as graphvein index would create it.
//
// GRAPHVEIN_SQLITE_PATH names one SQLite file, so it applies only to the
// index named on the command line (serve <path> or serve --project, which
// the caller opens itself): every entry this function would open is
// refused with server.ErrSingleStore instead of being answered from that
// one file. The repository name is re-read from graphvein.yaml when it
// names one (a project's from its manifest), so an index renamed since it was
// registered answers under its new name while its entry keeps its id.
func openWorkspaceIndex(ctx context.Context, e workspace.Entry, create bool) (*server.WorkspaceIndex, error) {
	dir := e.Path
	if p := os.Getenv("GRAPHVEIN_SQLITE_PATH"); p != "" && os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" {
		return nil, fmt.Errorf("%w: GRAPHVEIN_SQLITE_PATH names one SQLite file (%s), which only the index named on the command line uses; unset it to open %s from its own store", server.ErrSingleStore, p, e.Name)
	}
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") == "" {
		if _, err := os.Stat(sqlitePath(dir)); errors.Is(err, fs.ErrNotExist) && !create {
			return nil, server.ErrNoIndex
		}
	}
	if create {
		w, err := openStore(ctx, dir)
		if err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	}
	var project config.Project
	if e.Kind == workspace.KindProject {
		var err error
		if project, err = config.LoadProject(dir); err != nil {
			return nil, err
		}
	}
	s, err := openReadStore(ctx, dir)
	if err != nil {
		return nil, err
	}
	if e.Kind != workspace.KindProject {
		// graphvein.yaml's repository, when it names one, is the
		// repository now; without one the registered name stays (it may
		// come from serve --repository). A file that does not load opens
		// the entry anyway, as serve <path> does: the UI gates its views
		// and Settings can fix the file (ADR 0088).
		repository, _, err := config.ResolveRegisteredRepository(dir, e.Name)
		if err != nil {
			s.Close()
			return nil, err
		}
		locker, _ := openLocker(dir, repository)
		return &server.WorkspaceIndex{Entry: e, Options: repositoryOptions(dir, repository, s, locker), Close: func() {
			locker.Close()
			s.Close()
		}}, nil
	}
	opt, closeLockers := projectOptions(project, s)
	return &server.WorkspaceIndex{Entry: e, Options: opt, Close: func() {
		closeLockers()
		s.Close()
	}}, nil
}

// storeLocation reports where graphvein index stores folder's index.
func storeLocation(folder string) server.StoreLocation {
	if os.Getenv("GRAPHVEIN_POSTGRES_URL") != "" {
		return server.StoreLocation{Kind: "postgres", Env: "GRAPHVEIN_POSTGRES_URL"}
	}
	p := sqlitePath(folder)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return server.StoreLocation{Kind: "sqlite", Path: p}
}

// loopbackLiteral mirrors egress.ListenLoopback's rule so a bad --addr is a
// usage error (exit 2) rather than a listen failure: an IP literal on the
// loopback network with a port, never a name such as localhost.
func loopbackLiteral(addr string) bool {
	ap, err := netip.ParseAddrPort(addr)
	return err == nil && ap.Addr().Unmap().IsLoopback()
}

// projectServeCommand is graphvein serve --project (ADR 0075 "Server and
// UI", T022 B8): one project per server, read from the project store.
// Every endpoint answers per member through its repository parameter; a
// re-index from the UI re-indexes the project (members, then the link
// pass) under the same locks as graphvein index --project.
func projectServeCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("project", "", "project manifest (graphvein-project.yaml) or its directory")
	addr := flags.String("addr", defaultServeAddr, "loopback IP address and port to listen on (port 0 picks a free port)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *manifest == "" {
		fmt.Fprintln(stderr, "usage: graphvein serve --project <manifest or its directory> [--addr 127.0.0.1:7420]; members come from the manifest, so no path or --repository")
		return 2
	}
	if !loopbackLiteral(*addr) {
		fmt.Fprintln(stderr, "--addr must be a loopback IP literal such as 127.0.0.1:7420 or [::1]:7420")
		return 2
	}
	dir, err := projectDir(*manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	registry := openRegistry(logger)
	if err := sharedStoreCheck(registry, project.Dir, project.Name, server.ProjectClaims(project)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s, err := openReadStore(ctx, project.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer s.Close()
	opt, closeLockers := projectOptions(project, s)
	defer closeLockers()
	current := &server.WorkspaceIndex{
		Entry:   register(registry, workspace.KindProject, project.Name, project.Dir, logger),
		Options: opt,
	}
	return serveWorkspace(ctx, *addr, registry, current, stdout, stderr, logger, nil)
}

// projectOptions are the options of a project index over its open read
// store: the project lock, each member's lock and the project re-index.
// closeLockers releases the lockers.
func projectOptions(project config.Project, s store.Store) (server.Options, func()) {
	locker, _ := openProjectLocker(project.Dir, project.Name)
	memberLockers := map[string]store.IndexLocker{}
	for _, m := range project.Members {
		l, _ := openLocker(project.Dir, m.Repository)
		memberLockers[m.Repository] = l
	}
	closeLockers := func() {
		for _, l := range memberLockers {
			l.Close()
		}
		locker.Close()
	}
	return server.Options{
		Store:      s,
		Locker:     locker,
		OpenWriter: func(ctx context.Context) (store.Store, error) { return openStore(ctx, project.Dir) },
		StoreDir:   storeDir(project.Dir),
		Project: &server.ProjectOptions{
			Project:       project,
			MemberLockers: memberLockers,
			// A member renamed from the UI (T022 B10) gets its lock here.
			OpenMemberLocker: func(repository string) store.IndexLocker {
				l, _ := openLocker(project.Dir, repository)
				return l
			},
			// project is the manifest the server serves now, which a UI
			// edit may have changed since start.
			Reindex: func(ctx context.Context, project config.Project, linkPassOnly bool, progress func(string, indexrun.Progress)) server.ProjectRun {
				if linkPassOnly {
					return projectRunOf(project, runProjectLinkPass(ctx, project, progress))
				}
				return projectRunOf(project, runProjectIndex(ctx, project, parse.Options{Timeout: indexrun.DefaultTimeout}, indexrun.DefaultMaxFileSize, nil, progress))
			},
		},
	}, closeLockers
}

// projectRunOf converts a project index outcome into the server's report.
func projectRunOf(project config.Project, o projectIndexOutcome) server.ProjectRun {
	out := server.ProjectRun{PassError: o.passErr}
	if o.passErr == nil {
		run := o.run
		out.Pass = &run
	}
	for _, m := range project.Members {
		r, indexed := o.results[m.Repository]
		out.Members = append(out.Members, server.ProjectMemberRun{Repository: m.Repository, Indexed: indexed, Run: r.run, Error: r.err})
	}
	return out
}

// reportIssueURL is the build-time wrong-fact issue form URL, or "" when
// none is set or it is not a plain https URL (logged; the flag then stays
// off, ADR 0089).
func reportIssueURL(logger *slog.Logger) string {
	if reportURL == "" {
		return ""
	}
	u, err := server.ReportURL(reportURL)
	if err != nil {
		logger.Warn("wrong-fact report link disabled", "error", err)
		return ""
	}
	return u
}
