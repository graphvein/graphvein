package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ReadPaths are the API v0 views an in-process Reader answers (T030 D3).
// Each is a GET view that only reads the store and the checkout: the
// re-index runner, the writing change endpoints (pins, ref jobs), the
// manifest edit, the explorer's POST and every try-it path are absent, so
// a Reader can neither write nor reach the network. The change views are
// GET only: a ref: base whose snapshot is not stored is refused by the
// view (ref_unknown), never indexed (the POST refs job is not here).
var ReadPaths = []string{
	"/api/v0/status",
	"/api/v0/precision",
	"/api/v0/api/endpoints",
	"/api/v0/api/endpoint",
	"/api/v0/api/calls",
	"/api/v0/source",
	"/api/v0/architecture",
	"/api/v0/architecture/chain",
	"/api/v0/schema/summary",
	"/api/v0/schema/table",
	"/api/v0/schema/assumptions",
	"/api/v0/changes/bases",
	"/api/v0/changes/summary",
	"/api/v0/changes/items",
	"/api/v0/changes/impact",
	"/api/v0/project",
	"/api/v0/project/callers",
	"/api/v0/project/impact",
	"/api/v0/project/changes/impact",
	"/api/v0/project/links/changes",
	// Jump-to search and the couldn't-read page (T040, rows JS-21 and
	// CR-39): readable in process; no MCP tool reads them.
	"/api/v0/search",
	"/api/v0/unread",
}

// ReadError is a view's own error answer (its HTTP status, code and
// message), returned by Reader.Read so a caller can tell a refusal such as
// not_found or bad_request from a failed read.
type ReadError struct {
	Status  int
	Code    string
	Message string
}

func (e *ReadError) Error() string { return e.Code + ": " + e.Message }

// Reader answers the read-only views of HTTP API v0 in process, without a
// listener and without the browser guards of the HTTP server (Host,
// Fetch Metadata, CSP), which protect a socket a Reader does not have.
// It runs the same handlers graphvein serve registers, with the same
// consistency check, so its JSON is byte for byte the UI's (T030 D3): the
// MCP server reuses every rule of the views instead of a second
// implementation.
type Reader struct {
	handlers map[string]http.Handler
	stop     shutdowner
}

// NewReader prepares a Reader for opt, a single repository or, with
// opt.Project, a project whose member views take the repository parameter
// as serve --project does. opt.Locker and opt.OpenWriter are ignored: a
// Reader never re-indexes. Close it when done.
func NewReader(opt Options) *Reader {
	opt.Locker, opt.OpenWriter = nil, nil
	var routes map[string]http.Handler
	var stop shutdowner
	if opt.Project != nil {
		var set changesSet
		routes, set = newProjectRoutes(opt, nil)
		stop = set
	} else {
		routes = newAPIRoutes(opt)
		routes["/api/v0/status"] = newStatusHandler(opt)
		api := newChangesAPI(opt)
		api.routes(routes)
		stop = changesSet{api}
	}
	out := map[string]http.Handler{}
	for _, p := range ReadPaths {
		if h, ok := routes[p]; ok {
			out[p] = h
		}
	}
	return &Reader{handlers: out, stop: stop}
}

// Close stops what the views' route set runs in the background.
func (r *Reader) Close() {
	if r.stop != nil {
		r.stop.shutdown()
	}
}

// Read answers GET path?q as the HTTP API would and returns the JSON
// body. A view's error answer is a *ReadError; a path outside ReadPaths
// is refused before anything is read.
func (r *Reader) Read(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	h, ok := r.handlers[path]
	if !ok {
		return nil, &ReadError{Status: http.StatusNotFound, Code: "not_found", Message: "no readable view at " + path}
	}
	target := "/" + path[1:]
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	// The request never leaves the process: the handler is called
	// directly, and no client or transport is involved.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	rec := &bodyRecorder{header: http.Header{}, status: http.StatusOK}
	h.ServeHTTP(rec, req)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body := bytes.TrimSpace(rec.body.Bytes())
	if rec.status != http.StatusOK {
		var env errorEnvelope
		if err := json.Unmarshal(body, &env); err != nil || env.Error.Code == "" {
			return nil, &ReadError{Status: rec.status, Code: "store_error", Message: fmt.Sprintf("%s answered %d", path, rec.status)}
		}
		return nil, &ReadError{Status: rec.status, Code: env.Error.Code, Message: env.Error.Message}
	}
	return json.RawMessage(body), nil
}

// bodyRecorder is the in-process http.ResponseWriter of Read.
type bodyRecorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (b *bodyRecorder) Header() http.Header { return b.header }

func (b *bodyRecorder) WriteHeader(status int) {
	if !b.wrote {
		b.status, b.wrote = status, true
	}
}

func (b *bodyRecorder) Write(p []byte) (int, error) {
	b.wrote = true
	return b.body.Write(p)
}
