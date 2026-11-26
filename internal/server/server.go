// Package server serves Graphvein's local web UI and its HTTP API on a
// loopback address. It never opens a socket itself: the listener comes from
// egress.ListenLoopback, so the network boundary of ADR 0040 holds (ADR 0041).
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
)

// shutdownGrace bounds how long Serve waits for connections to finish after
// its context ends. Running requests are cancelled at once, so they normally
// end well within it.
const shutdownGrace = 5 * time.Second

// errStopping is the cancellation cause of every request context once
// Serve's context ends, so handlers can tell a stopping server (answered
// with 503) from a client that left (nobody to answer).
var errStopping = errors.New("server is stopping")

// Options configures one repository's server. Requests read Store only;
// the one exception is a re-index (POST /api/v0/index/run), which runs in
// a job with its own writer store from OpenWriter under the index lock
// (ADR 0052).
type Options struct {
	// Root is the absolute repository directory that freshness scans.
	Root string
	// Repository is the logical repository ID used for store reads.
	Repository string
	// Version is the binary's version, reported as server_version.
	Version string
	// Store is an open, read-only store; the caller closes it.
	Store store.Store
	// Locker is the index lock shared with graphvein index. With
	// OpenWriter it enables re-index; when either is nil, GET
	// /api/v0/index/run reports the lock as unknown and POST answers 503.
	Locker store.IndexLocker
	// OpenWriter opens a writer store for one re-index run, the same store
	// Store reads, located the way graphvein index locates it. The job
	// closes it.
	OpenWriter func(context.Context) (store.Store, error)
	// StoreDir is the directory of the writer store's files, where a
	// re-index writes its temporary snapshot (indexrun.Options.SnapshotDir;
	// "" for Postgres: the OS temporary directory).
	StoreDir string
	// Assets holds the UI files: ui.Assets in production, a MapFS in tests.
	Assets fs.FS
	// Logger receives request rejections and handler failures; nil discards.
	Logger *slog.Logger
	// Now returns the current time; nil means time.Now. Tests fix it so
	// checked_at is deterministic.
	Now func() time.Time
	// ReportURL is the public wrong-fact issue form (a build-time setting,
	// ReportURL validates it), served by GET /api/v0/ui as report_url; ""
	// keeps the UI's "Report a wrong fact" flag off (ADR 0089).
	ReportURL string
	// Project, when set, makes the server serve one project (graphvein
	// serve --project; ADR 0075 "Server and UI"): Root and Repository are
	// unused, every endpoint answers per member through its repository
	// parameter, and a re-index re-indexes the project.
	Project *ProjectOptions
	// Workspace, when set, makes the server a multi-index server (ADR
	// 0087): Root, Repository, Store, Locker, OpenWriter, StoreDir and
	// Project are unused, every index comes from Workspace (its current
	// entry answers requests without ws=), and the workspace endpoints
	// are served.
	Workspace *WorkspaceOptions
	// project is set on the per-member options of a project server, so
	// member endpoints can read the project's links.
	project *projectState
	// noCheckout marks a project member without a checkout here (T022
	// B9): Root is empty, and the endpoints answer from the store alone.
	// Code peek, ref bases and ref jobs answer 409 no_checkout; IDE
	// links, file states and freshness are reported as unavailable,
	// never read from the server's working directory.
	noCheckout bool
	// onStatus, when set (workspace servers), receives the freshness of
	// every GET /api/v0/status answered for Repository, with the folder it
	// checked (checkedPath), so the workspace list shows it at once instead
	// of after the next background check.
	onStatus func(repository, path string, f freshnessStatus)
}

// checkedPath is the folder a freshness check of these options is for:
// for a project member the path its manifest gives (also when the
// checkout is missing, "" when it gives none), else Root. The workspace
// list shows a member's check only while the manifest on disk still gives
// this path (T038 re-check W1).
func (o Options) checkedPath() string {
	if o.project != nil {
		for _, m := range o.project.project.Members {
			if m.Repository == o.Repository {
				return m.Path
			}
		}
	}
	return o.Root
}

// errNoCheckout is the freshness error of a project member without a
// checkout: freshness needs the member's files.
var errNoCheckout = errors.New("no checkout here: freshness needs the member's files")

// freshness walks the checkout as graphvein status does; a project member
// without a checkout is unknown with errNoCheckout.
func (o Options) freshness(ctx context.Context) ([]string, error) {
	if o.noCheckout {
		return nil, errNoCheckout
	}
	return index.Freshness(ctx, o.Root, o.Repository, o.Store)
}

// loadConfig reads the repository's graphvein.yaml. A project member
// without a checkout has none here and gets the defaults, as a repository
// without the file does: its settings are unknown, never another
// directory's.
func (o Options) loadConfig() (config.Config, error) {
	if o.noCheckout {
		return config.Defaults(), nil
	}
	return config.Load(o.Root)
}

// noCheckoutError is the 409 of an endpoint that needs the member's files.
func (o Options) noCheckoutError(what string) *apiError {
	return &apiError{http.StatusConflict, "no_checkout", fmt.Sprintf("member %s has no checkout here, and %s needs its files; the other endpoints answer from the stored index", o.Repository, what)}
}

func (o Options) logger() *slog.Logger {
	if o.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Logger
}

func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

// Server is one HTTP server bound to a loopback listener. Create it with
// Listen and run it with Serve.
type Server struct {
	listener net.Listener
	http     *http.Server
	url      string
	opt      Options
	// handlers counts running handler calls, so Serve returns only after
	// the last one finished and the caller may close the store.
	handlers *sync.WaitGroup
	// runs owns re-index jobs and refs the change endpoints' ref indexing
	// jobs and comparison preparation; Serve stops both before returning.
	runs *runner
	refs shutdowner
	// ws is the workspace state of a multi-index server (runs is nil
	// then: each open index has its own runner).
	ws *workspaceServer
}

// Listen opens the loopback listener for addr (an IP literal such as
// 127.0.0.1:7420; port 0 picks a free port) and prepares the server. The
// Host allow-list is derived from the address actually bound, so it also
// holds when the port was chosen by the system.
func Listen(ctx context.Context, addr string, opt Options) (*Server, error) {
	ln, err := egress.ListenLoopback(ctx, addr)
	if err != nil {
		return nil, listenError(addr, err)
	}
	bound, err := netip.ParseAddrPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("reading bound address of %s: %w", addr, err)
	}
	logger := opt.logger()
	handlers := new(sync.WaitGroup)
	var handler http.Handler
	var runs *runner
	var refs shutdowner
	var ws *workspaceServer
	if opt.Workspace != nil {
		ws = newWorkspaceServer(opt, allowedHosts(bound))
		handler, refs = ws.handler, ws
	} else {
		handler, runs, refs = newServerHandler(opt, allowedHosts(bound))
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true) // cleartext HTTP/1.1 only: no h2c, no TLS
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlers.Add(1)
			defer handlers.Done()
			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// A freshness walk on a large repository can take seconds; the
		// request context still cancels it when the client leaves.
		WriteTimeout:   2 * time.Minute,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 32 << 10,
		Protocols:      protocols,
		ErrorLog:       slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return &Server{listener: ln, http: srv, url: "http://" + bound.String() + "/", opt: opt, handlers: handlers, runs: runs, refs: refs, ws: ws}, nil
}

// listenError names the address once and keeps the operating system's
// reason (for example "bind: address already in use"): a *net.OpError
// would repeat the address in its own text.
func listenError(addr string, err error) error {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		err = op.Err
	}
	return fmt.Errorf("cannot listen on %s: %w", addr, err)
}

// allowedHosts lists the Host header values accepted for a server bound to
// bound: the loopback names a browser on this machine uses, plus the bound
// literal itself when it differs (for example 127.0.0.2:P).
func allowedHosts(bound netip.AddrPort) []string {
	port := bound.Port()
	hosts := []string{
		netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port).String(),
		fmt.Sprintf("localhost:%d", port),
		netip.AddrPortFrom(netip.IPv6Loopback(), port).String(),
	}
	literal := bound.String()
	for _, h := range hosts {
		if h == literal {
			return hosts
		}
	}
	return append(hosts, literal)
}

// URL is the base URL of the bound address, ending in a slash, with IPv6
// hosts in brackets and the actual port when port 0 was requested.
func (s *Server) URL() string {
	return s.url
}

// Serve answers requests until ctx ends, then shuts down: it cancels every
// running request (so freshness walks stop at once), waits up to five
// seconds for connections to finish, drops the rest, waits for the last
// handler to return and then for a running re-index to roll back and
// release the index lock. When Serve returns, nothing uses the store or the
// locker any more, so the caller may close them. It returns nil after a clean shutdown.
func (s *Server) Serve(ctx context.Context) error {
	logger := s.opt.logger()
	// Request contexts derive from this one, not from the background
	// context that http.Server uses by default, which Shutdown never
	// cancels.
	requests, stopRequests := context.WithCancelCause(context.Background())
	defer stopRequests(errStopping)
	s.http.BaseContext = func(net.Listener) context.Context { return requests }
	switch {
	case s.ws != nil:
		logger.Info("server started", "addr", s.listener.Addr().String(), "workspace", s.ws.ws.Registry.Path(), "current", s.ws.current(), "version", s.opt.Version)
		s.ws.start()
	case s.opt.Project != nil:
		logger.Info("server started", "addr", s.listener.Addr().String(), "project", s.opt.Project.current().Name, "version", s.opt.Version)
	default:
		logger.Info("server started", "addr", s.listener.Addr().String(), "repository", s.opt.Repository, "version", s.opt.Version)
	}
	served := make(chan error, 1)
	go func() { served <- s.http.Serve(s.listener) }()
	select {
	case err := <-served:
		// Serve only returns early on a listener failure.
		stopRequests(errStopping)
		s.http.Close()
		s.handlers.Wait()
		if s.runs != nil {
			s.runs.stop()
		}
		s.refs.shutdown()
		return fmt.Errorf("serving on %s: %w", s.listener.Addr(), err)
	case <-ctx.Done():
	}
	stopRequests(errStopping)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	err := s.http.Shutdown(shutdownCtx)
	if err != nil {
		// The grace period ran out; drop the remaining connections.
		s.http.Close()
	}
	// Close does not wait for handlers; their contexts are cancelled, so
	// they return promptly.
	s.handlers.Wait()
	// A running re-index is cancelled and rolls back; it closes its writer
	// store and releases the index lock before Serve returns, so the
	// caller can close the read store and the locker last. A workspace
	// server does the same for every open index.
	if s.runs != nil {
		s.runs.stop()
	}
	s.refs.shutdown()
	if serveErr := <-served; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && err == nil {
		err = serveErr
	}
	if err != nil {
		return fmt.Errorf("stopping server: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
