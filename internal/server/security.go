package server

import (
	"log/slog"
	"net/http"
	"strings"
)

// securityHeaders go on every response, errors included. The policy allows
// only same-origin scripts, styles and connections, so injected markup
// cannot run code or send data elsewhere.
var securityHeaders = [][2]string{
	{"Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"},
	{"X-Content-Type-Options", "nosniff"},
	{"Referrer-Policy", "no-referrer"},
	{"X-Frame-Options", "DENY"},
	{"Cross-Origin-Opener-Policy", "same-origin"},
	{"Cross-Origin-Resource-Policy", "same-origin"},
}

// NewHandler returns the complete request handler: security headers, the
// Host allow-list against DNS rebinding, the per-path method rule (GET and
// HEAD everywhere, POST only on /api/v0/index/run, the change endpoints
// that write (pins, pins/delete and refs), the try-it endpoints (T029)
// and, under --project, the manifest edit), the cross-site checks
// for the API plus http.CrossOriginProtection for POST, then routing to the
// API or the UI assets.
// allowedHosts are exact host:port values; Listen derives them from the
// bound address.
func NewHandler(opt Options, allowedHosts []string) http.Handler {
	h, _, _ := newServerHandler(opt, allowedHosts)
	return h
}

// newHandler is NewHandler that also returns the runner of re-index jobs.
func newHandler(opt Options, allowedHosts []string) (http.Handler, *runner) {
	h, runs, _ := newServerHandler(opt, allowedHosts)
	return h, runs
}

// newServerHandler is NewHandler that also returns the runner of re-index
// jobs and the change endpoints (ref indexing jobs, comparison
// preparation), so Serve can stop what runs at shutdown.
func newServerHandler(opt Options, allowedHosts []string) (http.Handler, *runner, shutdowner) {
	ih := newIndexHandler(opt)
	return guard(opt, allowedHosts, ih.serve), ih.runs, ih.changes
}

// indexHandler routes the per-index endpoints of one repository or one
// project: everything a single-index server answers under /api/. A
// workspace server (ADR 0087) holds one per open index.
type indexHandler struct {
	opt Options
	// lookup finds the handler of an API path; tryIt comes first.
	lookup func(path string) (http.Handler, bool)
	tryIt  *tryItState
	runs   *runner
	// changes stops the change endpoints' background work (and, in
	// project mode, the manifest router's).
	changes shutdowner
	// pr is the project router in project mode, nil otherwise.
	pr *projectRouter
	// cache, when set (workspace servers), answers repeated GET views.
	cache *answerCache
}

func newIndexHandler(opt Options) *indexHandler {
	ih := &indexHandler{opt: opt, runs: newRunner(opt)}
	if opt.Project != nil {
		// A project server's routes are rebuilt when a manifest edit is
		// written (T022 B10).
		pr := newProjectRouter(opt, ih.runs)
		ih.lookup, ih.changes, ih.pr = pr.lookup, pr, pr
		ih.tryIt = newTryItState(opt, &pr.editMu)
		return ih
	}
	ih.tryIt = newTryItState(opt, nil)
	routes := newAPIRoutes(opt)
	routes["/api/v0/status"] = newStatusHandler(opt)
	routes[indexRunPath] = ih.runs
	api := newChangesAPI(opt)
	api.routes(routes)
	ih.changes = changesSet{api}
	ih.lookup = func(path string) (http.Handler, bool) {
		h, ok := routes[path]
		return h, ok
	}
	return ih
}

// serve answers one API request that passed the guard.
func (ih *indexHandler) serve(w http.ResponseWriter, r *http.Request) {
	if h, ok := ih.tryIt.handler(r.URL.Path); ok {
		h.ServeHTTP(w, r)
		return
	}
	h, ok := ih.lookup(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no API endpoint at "+r.URL.Path)
		return
	}
	if ih.cache != nil && r.Method == http.MethodGet && cachedPaths[r.URL.Path] {
		ih.cache.serve(w, r, h)
		return
	}
	h.ServeHTTP(w, r)
}

// busy reports whether the index runs a re-index or a ref indexing job,
// which closing it would cancel.
func (ih *indexHandler) busy() bool {
	if ih.runs.running() {
		return true
	}
	b, ok := ih.changes.(interface{ busy() bool })
	return ok && b.busy()
}

// shutdown stops a running re-index (it rolls back and releases its
// lock) and the change endpoints' background work, and waits for both.
func (ih *indexHandler) shutdown() {
	ih.runs.stop()
	ih.changes.shutdown()
}

// guard wraps api, the handler of every /api/ path, in the security
// pipeline of ADR 0041 and ADR 0052: security headers, the Host
// allow-list, the per-path method rule, the cross-site checks for the API
// and http.CrossOriginProtection for POST; other paths serve the UI
// assets.
func guard(opt Options, allowedHosts []string, api func(http.ResponseWriter, *http.Request)) http.Handler {
	hosts := map[string]bool{}
	// origins are the page origins served by this server. A browser that
	// lacks Fetch Metadata still sends Origin on cross-origin requests it
	// can be made to send, so a foreign Origin is rejected as well.
	origins := map[string]bool{}
	for _, h := range allowedHosts {
		hosts[asciiLower(h)] = true
		origins["http://"+asciiLower(h)] = true
	}
	// crossOrigin is Go's CSRF protection for the state-changing POST
	// (ADR 0041 section 6, ADR 0052): it rejects same-site and cross-site
	// Fetch Metadata and, without it, an Origin whose host is not the
	// request's Host. GET and HEAD never reach it.
	crossOrigin := http.NewCrossOriginProtection()
	static := staticHandler{assets: opt.Assets}
	logger := opt.logger()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range securityHeaders {
			w.Header().Set(h[0], h[1])
		}
		reject := func(status int, code, message string) {
			// Only the path is logged: query strings may carry user data.
			logger.Warn("request rejected", "reason", code, "host", r.Host, "path", r.URL.Path)
			writeError(w, status, code, message)
		}
		if !hosts[asciiLower(r.Host)] {
			reject(http.StatusForbidden, "host_not_allowed", "this server answers only requests addressed to its loopback address")
			return
		}
		// The method rule is per path: every path is GET and HEAD only,
		// except the re-index endpoint and the writing change endpoints,
		// which also take POST (ADR 0052, ADR 0056).
		post := postPaths[r.URL.Path]
		if r.Method != http.MethodGet && r.Method != http.MethodHead && (!post || r.Method != http.MethodPost) {
			if post {
				allow := "POST"
				if getAlso[r.URL.Path] {
					allow = "GET, HEAD, POST"
				}
				w.Header().Set("Allow", allow)
				reject(http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, allow))
				return
			}
			w.Header().Set("Allow", "GET, HEAD")
			reject(http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, "GET, HEAD"))
			return
		}
		if !isAPI(r.URL.Path) {
			static.ServeHTTP(w, r)
			return
		}
		switch asciiLower(r.Header.Get("Sec-Fetch-Site")) {
		case "cross-site", "same-site":
			reject(http.StatusForbidden, "cross_site_request", "API requests from other sites are not allowed")
			return
		}
		// An absent Origin (curl, scripts, same-origin GET) is allowed.
		if origin, ok := r.Header["Origin"]; ok && (len(origin) != 1 || !origins[asciiLower(origin[0])]) {
			reject(http.StatusForbidden, "cross_site_request", "API requests from other origins are not allowed")
			return
		}
		if r.Method == http.MethodPost {
			if err := crossOrigin.Check(r); err != nil {
				reject(http.StatusForbidden, "cross_site_request", "cross-origin requests may not change the index")
				return
			}
		}
		api(w, r)
	})
}

// isAPI reports whether path belongs to the HTTP API, which owns /api/.
func isAPI(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// asciiLower lower-cases ASCII letters only. strings.ToLower and EqualFold
// apply Unicode folding (the Kelvin sign folds to k), which a host check
// must not do.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// logFailure records a handler failure that produced a 500 response.
func logFailure(logger *slog.Logger, r *http.Request, err error) {
	logger.Error("request failed", "path", r.URL.Path, "error", err)
}

// methodText is the 405 message: the refused method, the path and the
// methods the path allows, as its Allow header lists them (R3-10).
func methodText(r *http.Request, allow string) string {
	return r.Method + " is not allowed on " + r.URL.Path + "; allowed: " + allow
}
