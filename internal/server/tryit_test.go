package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// Route identities of testdata/fixtures/tryit (fixture README).
const (
	routeGetUser    = "route:server.ts#GET%20%2Fusers%2F%3Aid"
	routePostUsers  = "route:server.ts#POST%20%2Fusers"
	routeDeleteUser = "route:server.ts#DELETE%20%2Fusers%2F%3Aid"
	routeAny        = "route:server.ts#ANY%20%2Fany"
	routeWildcard   = "route:server.ts#GET%20%2Ffiles%2F%2A"
	routeOptional   = "route:server.ts#GET%20%2Fopt%2F%3Ax%3F"
	routeSlash      = "route:server.ts#GET%20%2Fslash%2F"
)

// upstream is a local test API on 127.0.0.1 that records what it gets.
type upstream struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recorded
	// reply, when set, answers instead of the default 200 JSON.
	reply func(w http.ResponseWriter, r *http.Request)
}

type recorded struct {
	method, uri string
	header      http.Header
	body        string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.reqs = append(u.reqs, recorded{r.Method, r.RequestURI, r.Header.Clone(), string(body)})
		reply := u.reply
		u.mu.Unlock()
		if reply != nil {
			reply(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) requests() []recorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recorded(nil), u.reqs...)
}

// tryFixture is one indexed copy of the tryit fixture behind one handler,
// so session state persists across its requests.
type tryFixture struct {
	t     *testing.T
	root  string
	opt   Options
	h     http.Handler
	token string
	logs  *bytes.Buffer
	now   time.Time
}

// indexTryFixture copies and indexes the tryit fixture into s.
func indexTryFixture(t *testing.T, s store.Store) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tryit")); err != nil {
		t.Fatal(err)
	}
	indexRoot(t, root, s)
	return root
}

// newTryFixture indexes the fixture into a fresh SQLite store and writes
// config as its graphvein.yaml (after indexing, so the file list is the
// fixture's).
func newTryFixture(t *testing.T, cfg string) *tryFixture {
	t.Helper()
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return serveTryFixture(t, indexTryFixture(t, s), s, cfg)
}

func serveTryFixture(t *testing.T, root string, s store.Store, cfg string) *tryFixture {
	t.Helper()
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := &tryFixture{t: t, root: root, logs: new(bytes.Buffer), now: fixedNow()}
	f.opt = testOptions(root, s)
	f.opt.Logger = slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.opt.Now = func() time.Time { return f.now }
	f.h = NewHandler(f.opt, testHosts)
	f.token = f.list("").Token
	return f
}

// envConfig is a graphvein.yaml with the given environments.
func envConfig(envs ...string) string {
	return "version: 1\ntryit:\n  environments:\n" + strings.Join(envs, "")
}

func envEntry(name, label, url string, extra ...string) string {
	s := fmt.Sprintf("    - name: %s\n      label: %s\n      base_url: %s\n", name, label, url)
	for _, e := range extra {
		s += "      " + e + "\n"
	}
	return s
}

// do sends one request with the headers a page of this server sends.
func (f *tryFixture) do(method, path string, body any, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			f.t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = testHost
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://"+testHost)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(tokenHeader, f.token)
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func (f *tryFixture) list(repository string) environmentsResponse {
	f.t.Helper()
	path := tryItEnvironmentsPath
	if repository != "" {
		path += "?repository=" + repository
	}
	rec := f.do(http.MethodGet, path, nil)
	var out environmentsResponse
	decodeStrict(f.t, rec, http.StatusOK, &out)
	return out
}

func (f *tryFixture) selectEnv(id string, ack bool) selectResponse {
	f.t.Helper()
	var out selectResponse
	decodeStrict(f.t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Environment: id, AcknowledgeProduction: ack}), http.StatusOK, &out)
	return out
}

func (f *tryFixture) prepare(req tryRequest) prepareResponse {
	f.t.Helper()
	var out prepareResponse
	decodeStrict(f.t, f.do(http.MethodPost, tryItPreparePath, req), http.StatusOK, &out)
	return out
}

func (f *tryFixture) send(req tryRequest) sendResponse {
	f.t.Helper()
	var out sendResponse
	decodeStrict(f.t, f.do(http.MethodPost, tryItSendPath, req), http.StatusOK, &out)
	return out
}

// decodeStrict checks the status and decodes the body, refusing unknown
// fields so the documented shapes and the code cannot drift apart.
func decodeStrict(t *testing.T, rec *httptest.ResponseRecorder, status int, v any) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatal(err, rec.Body.String())
	}
}

// wantError checks an error answer's status and code.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var env errorEnvelope
	if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &env) != nil || env.Error.Code != code {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

func getUser(env, id string) tryRequest {
	return tryRequest{Environment: env, Route: routeGetUser, Path: "/users/{id}", Method: "GET", PathValues: map[string]string{"id": id}}
}

func postUser(env, body string) tryRequest {
	return tryRequest{Environment: env, Route: routePostUsers, Path: "/users", Method: "POST", Body: &body, ContentType: "application/json"}
}

// TestTryItMethodsPerPath checks row TI-1: the try-it paths and their
// methods.
func TestTryItMethodsPerPath(t *testing.T) {
	f := newTryFixture(t, "")
	for _, p := range []string{tryItSelectPath, tryItAuthPath, tryItPreparePath, tryItSendPath} {
		rec := f.do(http.MethodGet, p, nil)
		wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		if rec.Header().Get("Allow") != "POST" {
			t.Errorf("%s: Allow %q", p, rec.Header().Get("Allow"))
		}
		wantError(t, f.do(http.MethodPut, p, nil), http.StatusMethodNotAllowed, "method_not_allowed")
	}
	rec := f.do(http.MethodDelete, tryItEnvironmentsPath, nil)
	wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
	if rec.Header().Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("environments: Allow %q", rec.Header().Get("Allow"))
	}
	rec = f.do(http.MethodPost, tryItSuggestionsPath, map[string]any{})
	wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
	if rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("suggestions: Allow %q", rec.Header().Get("Allow"))
	}
}

// TestTryItRequestChecks checks row TI-2 on every try-it POST.
func TestTryItRequestChecks(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	for _, p := range []string{tryItEnvironmentsPath, tryItSelectPath, tryItAuthPath, tryItPreparePath, tryItSendPath} {
		body := map[string]any{"environment": "repository:local"}
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Header.Del(tokenHeader) }), http.StatusForbidden, "token_required")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Header.Set(tokenHeader, "wrong") }), http.StatusForbidden, "token_required")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.RemoteAddr = "192.0.2.7:4000" }), http.StatusForbidden, "not_loopback")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }), http.StatusUnsupportedMediaType, "unsupported_media_type")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }), http.StatusForbidden, "cross_site_request")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }), http.StatusForbidden, "cross_site_request")
		wantError(t, f.do(http.MethodPost, p, body, func(r *http.Request) { r.Host = "evil.example:7420" }), http.StatusForbidden, "host_not_allowed")
		wantError(t, f.do(http.MethodPost, p, map[string]any{"url": "http://example.com"}), http.StatusBadRequest, "bad_request")
		wantError(t, f.do(http.MethodPost, p, `{"environment":"a","environment":"b"}`), http.StatusBadRequest, "bad_request")
	}
	if len(up.requests()) != 0 {
		t.Fatal("a refused request reached the upstream")
	}
}

// TestTryItEnvironmentsList checks row TI-4: the listing, unknown names
// and an invalid file.
func TestTryItEnvironmentsList(t *testing.T) {
	f := newTryFixture(t, envConfig(
		envEntry("local", "local", "http://127.0.0.1:8000/api/"),
		envEntry("prod", "Production", "https://api.example.test", "auth: {header: Authorization, env: GV_TRYIT_TEST_TOKEN, prefix: 'Bearer '}")))
	t.Setenv("GV_TRYIT_TEST_TOKEN", "secret-value-1")
	out := f.list("")
	if out.Repository != "fixture" || out.Token == "" || out.Selected != nil || len(out.Environments) != 2 {
		t.Fatalf("%+v", out)
	}
	local, prod := out.Environments[0], out.Environments[1]
	if local.ID != "repository:local" || local.BaseURL != "http://127.0.0.1:8000/api" || local.Origin != "http://127.0.0.1:8000" || local.Production || local.Auth.Header != nil || local.Line != 4 {
		t.Errorf("local %+v", local)
	}
	if prod.ID != "repository:prod" || !prod.Production || *prod.Auth.Header != "Authorization" || *prod.Auth.Env != "GV_TRYIT_TEST_TOKEN" || !prod.Auth.EnvSet || prod.Auth.SessionValue {
		t.Errorf("prod %+v", prod)
	}
	if strings.Contains(f.do(http.MethodGet, tryItEnvironmentsPath, nil).Body.String(), "secret-value-1") {
		t.Fatal("the listing shows a secret value")
	}
	if len(out.Files) != 1 || out.Files[0].Target != "repository" || out.Files[0].File != "graphvein.yaml" || !out.Files[0].Exists || !strings.HasPrefix(out.Files[0].Hash, "sha256:") {
		t.Errorf("files %+v", out.Files)
	}
	if out.Limits != (limitsDoc{maxTryItBody, maxTryItResponse, 30, 120}) {
		t.Errorf("limits %+v", out.Limits)
	}
	wantError(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Environment: "repository:staging"}), http.StatusBadRequest, "unknown_environment")
	wantError(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Environment: "local"}), http.StatusBadRequest, "unknown_environment")
	if err := os.WriteFile(filepath.Join(f.root, "graphvein.yaml"), []byte(envConfig(envEntry("x", "l", "ftp://x"))), 0o644); err != nil {
		t.Fatal(err)
	}
	wantError(t, f.do(http.MethodGet, tryItEnvironmentsPath, nil), http.StatusConflict, "invalid_config")
}

// TestTryItOperationMatch checks rows TI-7, TI-26 and TI-D on both
// backends: the URL is built from the environment and the stored
// operation only, values are encoded, and anything else is refused with
// nothing sent.
func TestTryItOperationMatch(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		up := newUpstream(t)
		root := indexTryFixture(t, w)
		f := serveTryFixture(t, root, s, envConfig(envEntry("local", "local", up.URL+"/base/")))
		f.selectEnv("repository:local", false)
		req := getUser("repository:local", "a/b?c#d e%")
		req.Query = []pairDoc{{"z", "1"}, {"a b", "x&y=z"}, {"z", ""}}
		got := f.send(req)
		wantURI := "/base/users/a%2Fb%3Fc%23d%20e%25?z=1&a+b=x%26y%3Dz&z="
		if got.Status != 200 || got.URL != up.URL+wantURI || got.Body != `{"ok":true}` || got.BodyEncoding != "text" {
			t.Fatalf("%+v", got)
		}
		if reqs := up.requests(); len(reqs) != 1 || reqs[0].uri != wantURI || reqs[0].method != "GET" {
			t.Fatalf("upstream got %+v", reqs)
		}
		fast := tryRequest{Environment: "repository:local", Route: "route:api/main.py#GET%20%2Fitems%2F%7Bitem_id%7D", Path: "/items/{item_id}", Method: "GET", PathValues: map[string]string{"item_id": "7"}}
		if got := f.send(fast); got.URL != up.URL+"/base/items/7" {
			t.Errorf("fastapi: %s", got.URL)
		}
		slash := tryRequest{Environment: "repository:local", Route: routeSlash, Path: "/slash", Method: "GET"}
		if got := f.send(slash); got.URL != up.URL+"/base/slash" {
			t.Errorf("slash: %s", got.URL)
		}
		for _, tc := range []struct {
			name   string
			req    tryRequest
			status int
			code   string
		}{
			{"missing value", tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/users/{id}", Method: "GET"}, 400, "missing_path_value"},
			{"empty value", getUser("repository:local", ""), 400, "missing_path_value"},
			{"dot dot", getUser("repository:local", ".."), 400, "invalid_path_value"},
			{"dot", getUser("repository:local", "."), 400, "invalid_path_value"},
			{"extra value", tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/users/{id}", Method: "GET", PathValues: map[string]string{"id": "1", "x": "2"}}, 400, "bad_request"},
			{"free-form path", tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/admin/users/{id}", Method: "GET", PathValues: map[string]string{"id": "1"}}, 400, "path_mismatch"},
			{"filled path", tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/users/1", Method: "GET"}, 400, "path_mismatch"},
			{"other method", tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/users/{id}", Method: "DELETE", PathValues: map[string]string{"id": "1"}}, 400, "method_mismatch"},
			{"any method", tryRequest{Environment: "repository:local", Route: routeAny, Path: "/any", Method: "ANY"}, 400, "operation_unavailable"},
			{"wildcard", tryRequest{Environment: "repository:local", Route: routeWildcard, Path: "/files/{*}", Method: "GET", PathValues: map[string]string{"*": "a"}}, 400, "operation_unavailable"},
			{"optional", tryRequest{Environment: "repository:local", Route: routeOptional, Path: "/opt/{x}", Method: "GET", PathValues: map[string]string{"x": "a"}}, 400, "operation_unavailable"},
			{"unknown route", tryRequest{Environment: "repository:local", Route: "route:nope#GET%20%2F", Path: "/", Method: "GET"}, 404, "unknown_operation"},
			{"not a route", tryRequest{Environment: "repository:local", Route: "file:server.ts", Path: "/", Method: "GET"}, 404, "unknown_operation"},
			{"no operation", tryRequest{Environment: "repository:local"}, 400, "bad_request"},
		} {
			rec := f.do(http.MethodPost, tryItSendPath, tc.req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
				t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body.String())
			}
		}
		if len(up.requests()) != 3 {
			t.Fatalf("a refused request was sent: %d requests", len(up.requests()))
		}
	})
}

// TestTryItSelection checks rows TI-8 and TI-25: a send goes only to the
// selected environment, and a switch revokes grants and pending
// confirmations.
func TestTryItSelection(t *testing.T) {
	a, b := newUpstream(t), newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("a", "local", a.URL), envEntry("b", "staging", b.URL)))
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:a", "1")), http.StatusConflict, "environment_not_selected")
	f.selectEnv("repository:a", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:b", "1")), http.StatusConflict, "environment_not_selected")
	// A grant and a pending confirmation for a...
	p := f.prepare(postUser("repository:a", "{}"))
	req := postUser("repository:a", "{}")
	req.Confirmation, req.Remember = *p.Confirmation, true
	if got := f.send(req); !got.Remembered {
		t.Fatal("remember did not grant")
	}
	pending := f.prepare(tryRequest{Environment: "repository:a", Route: routeDeleteUser, Path: "/users/{id}", Method: "DELETE", PathValues: map[string]string{"id": "1"}})
	if pending.ConfirmationRequired {
		t.Fatal("a granted environment asked for confirmation")
	}
	p2 := f.prepare(postUser("repository:a", "{}"))
	_ = p2
	// ...are gone after a switch to b and back.
	f.selectEnv("repository:b", false)
	if s := f.selectEnv("repository:a", false); s.Remembered {
		t.Fatal("the grant survived a switch")
	}
	wantError(t, f.do(http.MethodPost, tryItSendPath, postUser("repository:a", "{}")), http.StatusConflict, "confirmation_required")
	// Selecting the same environment again keeps its grant.
	p = f.prepare(postUser("repository:a", "{}"))
	req.Confirmation = *p.Confirmation
	f.send(req)
	if s := f.selectEnv("repository:a", false); !s.Remembered {
		t.Fatal("reselecting the same environment revoked its grant")
	}
	// An empty selection clears everything.
	f.selectEnv("", false)
	if f.list("").Selected != nil {
		t.Fatal("selection not cleared")
	}
	if len(b.requests()) != 0 {
		t.Fatal("environment b was reached")
	}
}

// TestTryItConfirmation checks rows TI-9 and TI-24: GET and HEAD go
// directly on a non-production environment; every other method needs a
// one-time confirmation bound to the exact request, or a grant.
func TestTryItConfirmation(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	if p := f.prepare(getUser("repository:local", "1")); p.ConfirmationRequired || p.Confirmation != nil || p.Method != "GET" || p.URL != up.URL+"/users/1" {
		t.Fatalf("GET prepare %+v", p)
	}
	f.send(getUser("repository:local", "1"))
	// No confirmation, an unknown one, a changed request, an expired one:
	// refused and nothing sent.
	wantError(t, f.do(http.MethodPost, tryItSendPath, postUser("repository:local", `{"a":1}`)), http.StatusConflict, "confirmation_required")
	bad := postUser("repository:local", `{"a":1}`)
	bad.Confirmation = "made-up"
	wantError(t, f.do(http.MethodPost, tryItSendPath, bad), http.StatusConflict, "confirmation_required")
	p := f.prepare(postUser("repository:local", `{"a":1}`))
	if !p.ConfirmationRequired || p.Confirmation == nil || p.ExpiresAt == nil || p.BodyBytes != 7 || p.Method != "POST" || strings.Join(p.Headers, ",") != "Content-Type" {
		t.Fatalf("POST prepare %+v", p)
	}
	changed := postUser("repository:local", `{"a":2}`)
	changed.Confirmation = *p.Confirmation
	wantError(t, f.do(http.MethodPost, tryItSendPath, changed), http.StatusConflict, "confirmation_required")
	// The failed attempt used the id up.
	same := postUser("repository:local", `{"a":1}`)
	same.Confirmation = *p.Confirmation
	wantError(t, f.do(http.MethodPost, tryItSendPath, same), http.StatusConflict, "confirmation_required")
	p = f.prepare(postUser("repository:local", `{"a":1}`))
	f.now = f.now.Add(confirmationTTL)
	same.Confirmation = *p.Confirmation
	wantError(t, f.do(http.MethodPost, tryItSendPath, same), http.StatusConflict, "confirmation_required")
	if n := len(up.requests()); n != 1 {
		t.Fatalf("%d requests reached the upstream, want the one GET", n)
	}
	// A valid confirmation sends once.
	p = f.prepare(postUser("repository:local", `{"a":1}`))
	same.Confirmation = *p.Confirmation
	if got := f.send(same); got.Status != 200 || got.Remembered {
		t.Fatalf("%+v", got)
	}
	wantError(t, f.do(http.MethodPost, tryItSendPath, same), http.StatusConflict, "confirmation_required")
	reqs := up.requests()
	if len(reqs) != 2 || reqs[1].method != "POST" || reqs[1].body != `{"a":1}` || reqs[1].header.Get("Content-Type") != "application/json" {
		t.Fatalf("upstream %+v", reqs)
	}
	// Remember: later non-GET requests of this environment need none.
	p = f.prepare(postUser("repository:local", `{}`))
	r := postUser("repository:local", `{}`)
	r.Confirmation, r.Remember = *p.Confirmation, true
	f.send(r)
	del := tryRequest{Environment: "repository:local", Route: routeDeleteUser, Path: "/users/{id}", Method: "DELETE", PathValues: map[string]string{"id": "9"}}
	if got := f.send(del); !got.Remembered {
		t.Fatal("grant not applied")
	}
	// remember needs a confirmation; GET takes none.
	r = postUser("repository:local", `{}`)
	r.Remember = true
	wantError(t, f.do(http.MethodPost, tryItSendPath, r), http.StatusBadRequest, "bad_request")
	g := getUser("repository:local", "1")
	g.Confirmation = "x"
	wantError(t, f.do(http.MethodPost, tryItSendPath, g), http.StatusBadRequest, "bad_request")
}

// TestTryItProduction checks row TI-10: a production environment needs one
// acknowledgement per selection before any send, GET included; mutating
// methods still need their confirmation; nothing else is blocked.
func TestTryItProduction(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("prod", "prod", up.URL), envEntry("local", "local", up.URL)))
	s := f.selectEnv("repository:prod", false)
	if !s.Production || s.Acknowledged || !s.AcknowledgementRequired {
		t.Fatalf("%+v", s)
	}
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:prod", "1")), http.StatusConflict, "production_acknowledgement_required")
	if p := f.prepare(getUser("repository:prod", "1")); !p.AcknowledgementRequired || !p.Production {
		t.Fatalf("%+v", p)
	}
	if s = f.selectEnv("repository:prod", true); !s.Acknowledged || s.AcknowledgementRequired {
		t.Fatalf("%+v", s)
	}
	f.send(getUser("repository:prod", "1"))
	wantError(t, f.do(http.MethodPost, tryItSendPath, postUser("repository:prod", "{}")), http.StatusConflict, "confirmation_required")
	p := f.prepare(postUser("repository:prod", "{}"))
	req := postUser("repository:prod", "{}")
	req.Confirmation = *p.Confirmation
	f.send(req)
	wantError(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Environment: "repository:local", AcknowledgeProduction: true}), http.StatusBadRequest, "bad_request")
	f.selectEnv("repository:local", false)
	f.selectEnv("repository:prod", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:prod", "1")), http.StatusConflict, "production_acknowledgement_required")
	if n := len(up.requests()); n != 2 {
		t.Fatalf("%d requests, want 2", n)
	}
}

// TestTryItAuth checks rows TI-11 and TI-12: the header value comes from
// the named variable or a value typed for the session, is sent upstream
// and is never returned, listed or logged.
func TestTryItAuth(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL, "auth: {header: Authorization, env: GV_TRYIT_TEST_TOKEN, prefix: 'Bearer '}"), envEntry("open", "local", up.URL)))
	t.Setenv("GV_TRYIT_TEST_TOKEN", "")
	f.selectEnv("repository:local", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:local", "1")), http.StatusBadRequest, "auth_missing")
	t.Setenv("GV_TRYIT_TEST_TOKEN", "env-secret-41")
	p := f.prepare(getUser("repository:local", "1"))
	if *p.AuthHeader != "Authorization" || *p.AuthSource != "env" {
		t.Fatalf("%+v", p)
	}
	f.send(getUser("repository:local", "1"))
	// A typed value wins over the variable and is never echoed.
	rec := f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Value: "Bearer typed-secret-42"})
	var ar authResponse
	decodeStrict(t, rec, http.StatusOK, &ar)
	if !ar.SessionValue || *ar.Header != "Authorization" || strings.Contains(rec.Body.String(), "typed-secret-42") {
		t.Fatalf("%s", rec.Body.String())
	}
	f.send(getUser("repository:local", "2"))
	l := f.list("")
	if !l.Environments[0].Auth.SessionValue {
		t.Fatal("session value not listed")
	}
	// A typed value for an environment without auth uses Authorization
	// unless another header is named.
	decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:open", Header: "X-Api-Key", Value: "typed-secret-43"}), http.StatusOK, &ar)
	wantError(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:open", Header: "Host", Value: "x"}), http.StatusBadRequest, "header_not_allowed")
	wantError(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:open", Value: "a\r\nb"}), http.StatusBadRequest, "bad_request")
	f.selectEnv("repository:open", false)
	f.send(getUser("repository:open", "3"))
	// Clearing falls back to the variable.
	decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Clear: true}), http.StatusOK, &ar)
	f.selectEnv("repository:local", false)
	f.send(getUser("repository:local", "4"))
	reqs := up.requests()
	want := []string{"Bearer env-secret-41", "Bearer typed-secret-42", "", "Bearer env-secret-41"}
	for i, r := range reqs {
		if r.header.Get("Authorization") != want[i] {
			t.Errorf("request %d: Authorization %q, want %q", i, r.header.Get("Authorization"), want[i])
		}
	}
	if reqs[2].header.Get("X-Api-Key") != "typed-secret-43" {
		t.Error("typed X-Api-Key not sent")
	}
	for _, secret := range []string{"env-secret-41", "typed-secret-42", "typed-secret-43"} {
		if strings.Contains(f.logs.String(), secret) {
			t.Fatalf("log holds %s:\n%s", secret, f.logs.String())
		}
		if strings.Contains(f.do(http.MethodGet, tryItEnvironmentsPath, nil).Body.String(), secret) {
			t.Fatalf("listing holds %s", secret)
		}
	}
}

// TestTryItHeaders checks row TI-13.
func TestTryItHeaders(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL, "auth: {header: X-Api-Key, env: GV_TRYIT_TEST_KEY}")))
	t.Setenv("GV_TRYIT_TEST_KEY", "k")
	f.selectEnv("repository:local", false)
	for _, h := range []pairDoc{{"Host", "x"}, {"Transfer-Encoding", "chunked"}, {"Content-Length", "1"}, {"Connection", "close"}, {"Proxy-Authorization", "x"},
		{"Upgrade", "websocket"}, {"x-api-key", "mine"}, {"Bad Name", "x"}} {
		req := getUser("repository:local", "1")
		req.Headers = []pairDoc{h}
		wantError(t, f.do(http.MethodPost, tryItSendPath, req), http.StatusBadRequest, "header_not_allowed")
	}
	req := getUser("repository:local", "1")
	req.Headers = []pairDoc{{"X-Trace", "a\r\nInjected: 1"}}
	wantError(t, f.do(http.MethodPost, tryItSendPath, req), http.StatusBadRequest, "bad_request")
	dup := postUser("repository:local", "{}")
	dup.Headers = []pairDoc{{"content-type", "text/plain"}}
	wantError(t, f.do(http.MethodPost, tryItPreparePath, dup), http.StatusBadRequest, "bad_request")
	req.Headers = []pairDoc{{"X-Trace", "t1"}, {"Accept", "application/json"}, {"Cookie", "a=b"}}
	f.send(req)
	got := up.requests()
	if len(got) != 1 || got[0].header.Get("X-Trace") != "t1" || got[0].header.Get("Cookie") != "a=b" || got[0].header.Get("X-Api-Key") != "k" {
		t.Fatalf("%+v", got)
	}
}

// TestTryItRedirect checks row TI-14: a redirect is answered, never
// followed, to another origin or the same one.
func TestTryItRedirect(t *testing.T) {
	other := newUpstream(t)
	up := newUpstream(t)
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/elsewhere", http.StatusFound)
	}
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	got := f.send(getUser("repository:local", "1"))
	if got.Status != 302 || got.Redirect == nil || *got.Redirect != other.URL+"/elsewhere" {
		t.Fatalf("%+v", got)
	}
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/users/2", http.StatusMovedPermanently)
	}
	if got = f.send(getUser("repository:local", "1")); got.Status != 301 {
		t.Fatalf("%+v", got)
	}
	for _, code := range []int{http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		up.reply = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", code) }
		if got = f.send(getUser("repository:local", "1")); got.Status != code || got.Redirect == nil {
			t.Fatalf("%d: %+v", code, got)
		}
	}
	if len(other.requests()) != 0 || len(up.requests()) != 5 {
		t.Fatal("a redirect was followed")
	}
}

// TestTryItLimits checks rows TI-3 and TI-15: request sizes, the response
// cap and the timeout.
func TestTryItLimits(t *testing.T) {
	tryItTestHook = func(s *tryItState) {
		s.maxResponse, s.timeout, s.maxSendPost = 1024, 300*time.Millisecond, maxTryItRequest
	}
	t.Cleanup(func() { tryItTestHook = nil })
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	huge := strings.Repeat("x", maxTryItRequest)
	wantError(t, f.do(http.MethodPost, tryItSendPath, postUser("repository:local", huge)), http.StatusRequestEntityTooLarge, "too_large")
	wantError(t, f.do(http.MethodPost, tryItPreparePath, postUser("repository:local", strings.Repeat("x", maxTryItBody+1))), http.StatusRequestEntityTooLarge, "body_too_large")
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, strings.Repeat("y", 5000))
	}
	got := f.send(getUser("repository:local", "1"))
	if !got.Truncated || got.BodyBytes != 1024 || len(got.Body) != 1024 || got.TruncatedReason == nil || *got.TruncatedReason != "size" {
		t.Fatalf("truncation: %v %d", got.Truncated, got.BodyBytes)
	}
	// A body cut by the timeout: the status received, marked.
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
		time.Sleep(time.Second)
		io.WriteString(w, "rest")
	}
	got = f.send(getUser("repository:local", "1"))
	if got.Status != 200 || !got.Truncated || got.TruncatedReason == nil || *got.TruncatedReason != "timeout" || got.Body != "partial" {
		t.Fatalf("timeout cut: %+v", got)
	}
	up.reply = func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) }
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:local", "1")), http.StatusGatewayTimeout, "upstream_timeout")
}

// TestTryItConcurrency checks row TI-16.
func TestTryItConcurrency(t *testing.T) {
	tryItTestHook = func(s *tryItState) {
		for i := 0; i < maxTryItInFlight; i++ {
			s.inFlight <- struct{}{}
		}
	}
	t.Cleanup(func() { tryItTestHook = nil })
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:local", "1")), http.StatusTooManyRequests, "too_many_requests")
	if len(up.requests()) != 0 {
		t.Fatal("sent beyond the limit")
	}
}

// TestTryItTLS checks row TI-17: certificates are verified, so a server
// whose certificate the system does not trust is refused (with the
// certificate, row TI-29).
func TestTryItTLS(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	f := newTryFixture(t, envConfig(envEntry("tls", "local", srv.URL)))
	f.selectEnv("repository:tls", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:tls", "1")), http.StatusBadGateway, "upstream_certificate_untrusted")
	if hits != 0 {
		t.Fatal("an unverified server got the request")
	}
}

// TestTryItResponseRedaction checks row TI-19.
func TestTryItResponseRedaction(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Set-Cookie", "session=s1")
		h.Add("Set-Cookie", "csrf=s2")
		h.Set("X-Api-Key", "s3")
		h.Set("X-Session-Id", "s4")
		h.Set("Authorization", "s5")
		h.Set("WWW-Authenticate", "Bearer realm=s6")
		h.Set("X-Request-Id", "visible")
		h.Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 1, 2, 0xff})
	}
	got := f.send(getUser("repository:local", "1"))
	shown := map[string][]string{}
	for _, p := range got.Headers {
		shown[p.Name] = append(shown[p.Name], p.Value)
	}
	for _, name := range []string{"Set-Cookie", "X-Api-Key", "X-Session-Id", "Authorization", "Www-Authenticate"} {
		for _, v := range shown[name] {
			if v != "[redacted]" {
				t.Errorf("%s shown as %q", name, v)
			}
		}
		if len(shown[name]) == 0 {
			t.Errorf("%s missing", name)
		}
	}
	if len(shown["Set-Cookie"]) != 2 || shown["X-Request-Id"][0] != "visible" {
		t.Errorf("%+v", shown)
	}
	if got.BodyEncoding != "hex" || got.Body != "000102ff" || got.BodyBytes != 4 {
		t.Errorf("binary body: %+v", got)
	}
	data, _ := json.Marshal(got)
	for _, s := range []string{"s1", "s2", "s3", "s4", "s5", "s6"} {
		if strings.Contains(string(data), "="+s) || strings.Contains(string(data), `"`+s+`"`) {
			t.Errorf("response shows %s", s)
		}
	}
}

// TestTryItLogging checks rows TI-20 and TI-21: the log has the
// environment, method, path template, status, duration and sizes and no
// value, and nothing is written to disk or the store.
func TestTryItLogging(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	before := listTree(t, f.root)
	runs, _ := f.opt.Store.Runs(context.Background(), "fixture")
	f.selectEnv("repository:local", false)
	p := f.prepare(postUser("repository:local", `{"password":"body-value-1"}`))
	req := postUser("repository:local", `{"password":"body-value-1"}`)
	req.Confirmation = *p.Confirmation
	req.Headers = []pairDoc{{"X-Trace", "header-value-2"}}
	req.Confirmation = ""
	p = f.prepare(req)
	req.Confirmation = *p.Confirmation
	req.Query = nil
	f.send(req)
	g := getUser("repository:local", "path-value-3")
	g.Query = []pairDoc{{"q", "query-value-4"}}
	f.send(g)
	log := f.logs.String()
	for _, v := range []string{"body-value-1", "header-value-2", "path-value-3", "query-value-4", up.URL} {
		if strings.Contains(log, v) {
			t.Fatalf("log holds %q:\n%s", v, log)
		}
	}
	for _, want := range []string{`msg="try-it request sent" environment=repository:local label=local repository=fixture method=GET path=/users/{id} request_bytes=0 status=200 response_bytes=11 truncated=false duration_ms=`,
		`method=POST path=/users request_bytes=27 status=200`} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if after := listTree(t, f.root); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("files changed: %v -> %v", before, after)
	}
	if after, _ := f.opt.Store.Runs(context.Background(), "fixture"); len(after) != len(runs) {
		t.Error("the store changed")
	}
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s %d %s", p, info.Size(), info.ModTime()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestTryItDestinationOnly checks rows TI-5 and TI-23: a send dials only
// the selected environment's origin, reported with purpose try-it, and
// nothing but a send dials.
func TestTryItDestinationOnly(t *testing.T) {
	a, b := newUpstream(t), newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("a", "local", a.URL), envEntry("b", "local", b.URL)))
	var mu sync.Mutex
	var events []egress.Event
	stop := egress.Observe(func(e egress.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	defer stop()
	f.selectEnv("repository:a", false)
	f.list("")
	f.prepare(getUser("repository:a", "1"))
	mu.Lock()
	if len(events) != 0 {
		t.Fatalf("events before a send: %+v", events)
	}
	mu.Unlock()
	f.send(getUser("repository:a", "1"))
	mu.Lock()
	seen := append([]egress.Event(nil), events...)
	mu.Unlock()
	aAddr := strings.TrimPrefix(a.URL, "http://")
	dials := 0
	for _, e := range seen {
		if e.Op != "dial" {
			continue
		}
		dials++
		if !e.Allowed || e.Purpose != egress.TryIt || e.Address != aAddr {
			t.Errorf("event %+v", e)
		}
	}
	if dials != 1 || len(b.requests()) != 0 {
		t.Fatalf("dials %d, b requests %d", dials, len(b.requests()))
	}
	// The policy of a send holds only that origin: a redirect target on
	// another port of the same host is not dialable even if it were
	// followed (egress TestTryItClient).
	c, err := tryItClient(config.BaseURL{Scheme: "http", Host: "127.0.0.1", Port: uint16(a.Listener.Addr().(*net.TCPAddr).Port)}, egress.TryItOptions{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := http.NewRequest(http.MethodGet, b.URL, nil)
	if _, err := c.Do(other); err == nil || !strings.Contains(err.Error(), egress.ErrBlocked.Error()) {
		t.Fatalf("another origin: %v", err)
	}
}

// TestTryItEnvironmentEdit checks row TI-30: the guarded edit that writes
// a typed URL as an environment.
func TestTryItEnvironmentEdit(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, "")
	l := f.list("")
	if len(l.Environments) != 0 || len(l.Files) != 1 || l.Files[0].Exists || l.Files[0].Hash != "absent" {
		t.Fatalf("%+v", l)
	}
	edit := envEditRequest{Target: "repository", BaseHash: "absent", Environment: envEditDoc{Name: "local", Label: "local", BaseURL: up.URL + "/"}}
	var prev envEditResponse
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, edit), http.StatusOK, &prev)
	wantDiff := "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n@@ -0,0 +1,6 @@\n+version: 1\n+tryit:\n+  environments:\n+    - name: local\n+      label: local\n+      base_url: " + up.URL + "\n"
	if prev.Applied || !prev.Valid || prev.Diff != wantDiff || prev.Environment != "repository:local" || prev.File != "graphvein.yaml" {
		t.Fatalf("preview %+v\n%s", prev, prev.Diff)
	}
	if _, err := os.Stat(filepath.Join(f.root, "graphvein.yaml")); err == nil {
		t.Fatal("a preview wrote the file")
	}
	// Not dialable before it is written.
	wantError(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Environment: "repository:local"}), http.StatusBadRequest, "unknown_environment")
	bad := edit
	bad.Apply, bad.ResultHash = true, "sha256:other"
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, bad), http.StatusConflict, "preview_changed")
	stale := edit
	stale.BaseHash = "sha256:old"
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, stale), http.StatusConflict, "file_changed")
	noToken := edit
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, noToken, func(r *http.Request) { r.Header.Del(tokenHeader) }), http.StatusForbidden, "token_required")
	apply := edit
	apply.Apply, apply.ResultHash = true, prev.ResultHash
	var done envEditResponse
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, apply), http.StatusOK, &done)
	if !done.Applied {
		t.Fatal("not applied")
	}
	l = f.list("")
	if len(l.Environments) != 1 || l.Environments[0].BaseURL != up.URL || l.Files[0].Hash != done.ResultHash {
		t.Fatalf("%+v", l)
	}
	f.selectEnv("repository:local", false)
	f.send(getUser("repository:local", "1"))
	// A second environment keeps the first; a duplicate name does not
	// validate and is never written.
	dupe := envEditRequest{Target: "repository", BaseHash: done.ResultHash, Environment: envEditDoc{Name: "local", Label: "x", BaseURL: "http://127.0.0.1:1"}}
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, dupe), http.StatusOK, &prev)
	if prev.Valid || len(prev.Problems) != 1 || !strings.Contains(prev.Problems[0], "environment local is defined twice") || strings.Contains(prev.Problems[0], f.root) {
		t.Fatalf("%+v", prev)
	}
	dupe.Apply, dupe.ResultHash = true, prev.ResultHash
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, dupe), http.StatusUnprocessableEntity, "invalid_config")
	for _, tc := range []envEditDoc{
		{Name: "a", Label: "l", BaseURL: "http://u:p@x"},
		{Name: "a", Label: "l", BaseURL: "ftp://x"},
		{Name: "a b", Label: "l", BaseURL: "http://x"},
		{Name: "a", Label: "", BaseURL: "http://x"},
	} {
		wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, envEditRequest{Target: "repository", BaseHash: done.ResultHash, Environment: tc}), http.StatusBadRequest, "bad_request")
	}
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, envEditRequest{Target: "project", BaseHash: done.ResultHash, Environment: envEditDoc{Name: "a", Label: "l", BaseURL: "http://x"}}), http.StatusBadRequest, "bad_request")
	withAuth := envEditRequest{Target: "repository", BaseHash: done.ResultHash, Environment: envEditDoc{Name: "staging", Label: "staging", BaseURL: "https://staging.example.test", Auth: &authEditDoc{Header: "Authorization", Env: "STAGING_TOKEN", Prefix: "Bearer "}}}
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, withAuth), http.StatusOK, &prev)
	withAuth.Apply, withAuth.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, withAuth), http.StatusOK, &done)
	data, _ := os.ReadFile(filepath.Join(f.root, "graphvein.yaml"))
	if !strings.Contains(string(data), "      auth:\n        header: Authorization\n        env: STAGING_TOKEN\n        prefix: 'Bearer '\n") {
		t.Fatalf("%s", data)
	}
}

// TestTryItSuggestions checks rows TI-31..TI-34 and TI-36..TI-41 on both
// backends against the golden: every suggestion with its source kind,
// row, file, line and reason; image services, interpolated values,
// templated servers and test files give none.
func TestTryItSuggestions(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := indexTryFixture(t, w)
		f := serveTryFixture(t, root, s, "")
		var out suggestionsDoc
		decodeStrict(t, f.do(http.MethodGet, tryItSuggestionsPath, nil), http.StatusOK, &out)
		golden.Check(t, filepath.Join("testdata", "tryit-suggestions.golden.json"), out)
		rows := map[string]bool{}
		for _, sg := range out.Suggestions {
			rows[sg.Row] = true
			if strings.Contains(sg.URL, "9999") || strings.Contains(sg.URL, "5432") || strings.Contains(sg.URL, "templated") {
				t.Errorf("suggested %+v", sg)
			}
		}
		for _, r := range tryItRows {
			if r.Test == "TestTryItSuggestions" && !rows[r.ID] {
				t.Errorf("row %s has no suggestion in the fixture", r.ID)
			}
		}
	})
}

// TestTryItProject checks rows TI-22 and TI-35: in project mode a member's
// environments are the project environments with a base URL for it plus
// its own graphvein.yaml; a project environment's acknowledgement spans
// members; suggestions include addresses another member calls it at; a
// project edit writes the manifest.
func TestTryItProject(t *testing.T) {
	up := newUpstream(t)
	ctx := context.Background()
	root := copyProjectFixture(t)
	frontend, backend := filepath.Join(root, "frontend"), filepath.Join(root, "backend")
	if err := os.WriteFile(filepath.Join(frontend, ".env"), []byte("VITE_API=http://localhost:8000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backend, ".env"), []byte("PORT=8000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := usersManifest + "tryit:\n  environments:\n    - name: shared\n      label: production\n      base_urls:\n        backend: " + up.URL + "\n"
	if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backend, "graphvein.yaml"), []byte(envConfig(envEntry("own", "local", up.URL+"/own"))), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	indexRepository(t, frontend, s, "frontend")
	indexRepository(t, backend, s, "backend")
	project, err := config.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := projectlink.Run(ctx, s, project, projectlink.ManifestHash(project)); err != nil {
		t.Fatal(err)
	}
	f := &tryFixture{t: t, root: root, logs: new(bytes.Buffer), now: fixedNow()}
	f.opt = projectOptions(project, s)
	f.opt.Logger = slog.New(slog.NewTextHandler(f.logs, nil))
	f.h = NewHandler(f.opt, testHosts)
	wantError(t, f.do(http.MethodGet, tryItEnvironmentsPath, nil), http.StatusBadRequest, "repository_required")
	l := f.list("backend")
	f.token = l.Token
	if len(l.Environments) != 2 || l.Environments[0].ID != "project:shared" || l.Environments[0].Source != "project" || l.Environments[0].File != config.ProjectFile ||
		l.Environments[1].ID != "repository:own" || len(l.Files) != 2 || l.Files[1].Target != "project" {
		t.Fatalf("backend %+v", l)
	}
	if fe := f.list("frontend"); len(fe.Environments) != 0 {
		t.Fatalf("frontend has no environment: %+v", fe.Environments)
	}
	wantError(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Repository: "frontend", Environment: "project:shared"}), http.StatusBadRequest, "unknown_environment")
	sel := selectRequest{Repository: "backend", Environment: "project:shared", AcknowledgeProduction: true}
	var sr selectResponse
	decodeStrict(t, f.do(http.MethodPost, tryItSelectPath, sel), http.StatusOK, &sr)
	req := tryRequest{Repository: "backend", Environment: "project:shared", Route: usersRoute, Path: "/t022b7/users", Method: "GET"}
	got := f.send(req)
	if got.URL != up.URL+"/t022b7/users" {
		t.Fatalf("%+v", got)
	}
	// A member's own environment is per member.
	decodeStrict(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Repository: "backend", Environment: "repository:own"}), http.StatusOK, &sr)
	req.Environment = "repository:own"
	if got = f.send(req); got.URL != up.URL+"/own/t022b7/users" {
		t.Fatalf("%+v", got)
	}
	// Suggestions: the frontend's .env address of the backend.
	var sg suggestionsDoc
	decodeStrict(t, f.do(http.MethodGet, tryItSuggestionsPath+"?repository=backend", nil), http.StatusOK, &sg)
	found := false
	for _, x := range sg.Suggestions {
		if x.Row == rowSuggestFrontend {
			found = true
			if x.URL != "http://localhost:8000" || *x.File != "frontend/.env" || *x.Line != 1 || x.SourceKind != "frontend_env" {
				t.Errorf("%+v", x)
			}
		}
	}
	if !found {
		t.Fatalf("no frontend suggestion: %+v", sg.Suggestions)
	}
	// A project edit adds the frontend's base URL to the shared
	// environment, in the manifest.
	edit := envEditRequest{Repository: "frontend", Target: "project", BaseHash: l.Files[1].Hash, Environment: envEditDoc{Name: "shared", BaseURL: "http://127.0.0.1:5173"}}
	var prev envEditResponse
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, edit), http.StatusOK, &prev)
	if !prev.Valid || !strings.Contains(prev.Diff, "+        frontend: http://127.0.0.1:5173\n") || prev.File != config.ProjectFile {
		t.Fatalf("%+v", prev)
	}
	edit.Apply, edit.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, edit), http.StatusOK, &prev)
	if fe := f.list("frontend"); len(fe.Environments) != 1 || fe.Environments[0].BaseURL != "http://127.0.0.1:5173" {
		t.Fatalf("frontend after edit: %+v", fe.Environments)
	}
	// Session state is per member: the backend's typed value and
	// acknowledgement of the shared environment do not apply to the
	// frontend.
	var ar authResponse
	decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Repository: "backend", Environment: "project:shared", Value: "backend-only"}), http.StatusOK, &ar)
	if fe := f.list("frontend"); fe.Environments[0].Auth.SessionValue || fe.Environments[0].Acknowledged {
		t.Fatalf("backend state applies to frontend: %+v", fe.Environments[0])
	}
	decodeStrict(t, f.do(http.MethodPost, tryItSelectPath, selectRequest{Repository: "frontend", Environment: "project:shared"}), http.StatusOK, &sr)
	if !sr.AcknowledgementRequired {
		t.Fatal("the backend's acknowledgement covered the frontend")
	}
	// Changing the backend's base URL in the manifest drops its state.
	decodeStrict(t, f.do(http.MethodPost, tryItSelectPath, sel), http.StatusOK, &sr)
	move := envEditRequest{Repository: "backend", Target: "project", BaseHash: prev.ResultHash, Environment: envEditDoc{Name: "shared", BaseURL: up.URL + "/moved"}}
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, move), http.StatusOK, &prev)
	move.Apply, move.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, move), http.StatusOK, &prev)
	req.Environment = "project:shared"
	wantError(t, f.do(http.MethodPost, tryItSendPath, req), http.StatusConflict, "environment_not_selected")
	if be := f.list("backend"); be.Environments[0].Auth.SessionValue || be.Environments[0].Acknowledged {
		t.Fatalf("backend state survived the move: %+v", be.Environments[0])
	}
	relabel := envEditRequest{Repository: "frontend", Target: "project", BaseHash: prev.ResultHash, Environment: envEditDoc{Name: "shared", Label: "local", BaseURL: "http://127.0.0.1:5174"}}
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, relabel), http.StatusConflict, "environment_exists")
	// Options of a project environment are edited in the manifest and
	// apply to every member (row TI-48).
	opts := envEditRequest{Repository: "frontend", Target: "project", BaseHash: prev.ResultHash, Options: &optionsEditDoc{Name: "shared", Limits: &limitsSetDoc{TimeoutSeconds: newPtr(45)}}}
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, opts), http.StatusOK, &prev)
	opts.Apply, opts.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, opts), http.StatusOK, &prev)
	if fe, be := f.list("frontend"), f.list("backend"); fe.Environments[0].EffectiveLimits.TimeoutSeconds != 45 || be.Environments[0].EffectiveLimits.TimeoutSeconds != 45 {
		t.Fatalf("project options: %+v %+v", fe.Environments[0], be.Environments[0])
	}
}

// TestTryItContractTable keeps docs/contract/claims/try-it.md's TI rows and
// tryItRows 1:1, and checks that each row's test exists.
func TestTryItContractTable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/contract/claims/try-it.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented, code []string
	for _, m := range regexp.MustCompile(`(?m)^\| (TI-[0-9D]+) \|`).FindAllSubmatch(doc, -1) {
		documented = append(documented, string(m[1]))
	}
	for _, r := range tryItRows {
		code = append(code, r.ID)
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("try-it.md rows %v\ncode rows %v", documented, code)
	}
	var sources bytes.Buffer
	for _, pattern := range []string{"*_test.go", "../egress/*_test.go"} {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources.Write(data)
		}
	}
	for _, r := range tryItRows {
		if !strings.Contains(sources.String(), "func "+r.Test+"(t *testing.T)") {
			t.Errorf("row %s names test %s, which does not exist", r.ID, r.Test)
		}
	}
}

// TestTryItEnvironmentChanged checks that grants, acknowledgements
// and typed values are bound to the environment's destination, label and
// auth; when its file changes any of them, all are dropped as on a switch,
// and nothing typed or granted for the old destination reaches the new.
func TestTryItEnvironmentChanged(t *testing.T) {
	a, b := newUpstream(t), newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", a.URL), envEntry("prod", "production", a.URL)))
	write := func(cfg string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.root, "graphvein.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	del := tryRequest{Environment: "repository:local", Route: routeDeleteUser, Path: "/users/{id}", Method: "DELETE", PathValues: map[string]string{"id": "1"}}
	f.selectEnv("repository:local", false)
	var ar authResponse
	decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Value: "Bearer typed-for-a"}), http.StatusOK, &ar)
	p := f.prepare(postUser("repository:local", "{}"))
	req := postUser("repository:local", "{}")
	req.Confirmation, req.Remember = *p.Confirmation, true
	f.send(req)
	f.send(del)
	// The same name now points at b.
	write(envConfig(envEntry("local", "local", b.URL), envEntry("prod", "production", a.URL)))
	wantError(t, f.do(http.MethodPost, tryItSendPath, del), http.StatusConflict, "environment_not_selected")
	l := f.list("")
	if l.Selected != nil || l.Environments[0].Remembered || l.Environments[0].Auth.SessionValue {
		t.Fatalf("state survived the change: %+v", l.Environments[0])
	}
	if s := f.selectEnv("repository:local", false); s.Remembered {
		t.Fatal("grant survived the change")
	}
	wantError(t, f.do(http.MethodPost, tryItSendPath, del), http.StatusConflict, "confirmation_required")
	f.send(getUser("repository:local", "2"))
	if reqs := b.requests(); len(reqs) != 1 || reqs[0].header.Get("Authorization") != "" {
		t.Fatalf("b got %+v", reqs)
	}
	// A production acknowledgement does not survive a change of origin,
	// nor a change of label.
	f.selectEnv("repository:prod", true)
	f.send(getUser("repository:prod", "3"))
	write(envConfig(envEntry("local", "local", b.URL), envEntry("prod", "production", b.URL)))
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:prod", "4")), http.StatusConflict, "environment_not_selected")
	if s := f.selectEnv("repository:prod", false); !s.AcknowledgementRequired {
		t.Fatal("acknowledgement survived the change")
	}
	f.selectEnv("repository:prod", true)
	write(envConfig(envEntry("local", "local", b.URL), envEntry("prod", "prod", b.URL)))
	if s := f.selectEnv("repository:prod", false); !s.AcknowledgementRequired {
		t.Fatal("acknowledgement survived a label change")
	}
	// A typed value is bound to the auth configuration too.
	write(envConfig(envEntry("local", "local", b.URL, "auth: {header: Authorization, env: GV_TRYIT_A}"), envEntry("prod", "prod", b.URL)))
	decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Value: "typed-2"}), http.StatusOK, &ar)
	write(envConfig(envEntry("local", "local", b.URL, "auth: {header: Authorization, env: GV_TRYIT_B}"), envEntry("prod", "prod", b.URL)))
	if l := f.list(""); l.Environments[0].Auth.SessionValue {
		t.Fatal("typed value survived an auth change")
	}
	if n := len(a.requests()); n != 3 {
		t.Fatalf("a got %d requests, want 3", n)
	}
}

// TestTryItAuthInHash checks that a confirmation is bound to the auth
// in use (header, source, variable, prefix, typed value), so changing it
// after prepare refuses the send.
func TestTryItAuthInHash(t *testing.T) {
	up := newUpstream(t)
	t.Setenv("GV_TRYIT_TEST_TOKEN", "v")
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL, "auth: {header: Authorization, env: GV_TRYIT_TEST_TOKEN}")))
	f.selectEnv("repository:local", false)
	var ar authResponse
	for _, change := range []func(){
		func() {
			decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Value: "typed-1"}), http.StatusOK, &ar)
		},
		func() {
			decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Value: "typed-2"}), http.StatusOK, &ar)
		},
		func() {
			decodeStrict(t, f.do(http.MethodPost, tryItAuthPath, authRequest{Environment: "repository:local", Clear: true}), http.StatusOK, &ar)
		},
	} {
		p := f.prepare(postUser("repository:local", "{}"))
		change()
		req := postUser("repository:local", "{}")
		req.Confirmation = *p.Confirmation
		wantError(t, f.do(http.MethodPost, tryItSendPath, req), http.StatusConflict, "confirmation_required")
	}
	if len(up.requests()) != 0 {
		t.Fatal("a request confirmed with other auth was sent")
	}
}

// TestTryItUnsupported checks rows TI-27 and TI-28: a method override, a
// body or a content type makes a GET or HEAD need the confirmation of a
// mutating method; WebSocket and h2c upgrade headers are refused for every
// method.
func TestTryItUnsupported(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	body := "x"
	for _, tc := range []struct {
		reason string
		mutate func(*tryRequest)
	}{
		{"method_override", func(r *tryRequest) { r.Headers = []pairDoc{{"X-HTTP-Method-Override", "DELETE"}} }},
		{"method_override", func(r *tryRequest) { r.Headers = []pairDoc{{"x-http-method", "DELETE"}} }},
		{"method_override", func(r *tryRequest) { r.Headers = []pairDoc{{"X-Method-Override", "DELETE"}} }},
		{"method_override", func(r *tryRequest) { r.Query = []pairDoc{{"_Method", "DELETE"}} }},
		{"body_on_get_or_head", func(r *tryRequest) { r.Body = &body }},
		{"body_on_get_or_head", func(r *tryRequest) { r.ContentType = "text/plain" }},
	} {
		req := getUser("repository:local", "1")
		tc.mutate(&req)
		wantError(t, f.do(http.MethodPost, tryItSendPath, req), http.StatusConflict, "confirmation_required")
		p := f.prepare(req)
		if !p.ConfirmationRequired || strings.Join(p.ConfirmationReasons, ",") != tc.reason || p.Confirmation == nil {
			t.Fatalf("%s: %+v", tc.reason, p)
		}
		req.Confirmation = *p.Confirmation
		f.send(req)
	}
	reqs := up.requests()
	if len(reqs) != 6 || reqs[0].header.Get("X-HTTP-Method-Override") != "DELETE" || reqs[3].uri != "/users/1?_Method=DELETE" || reqs[4].body != "x" || reqs[4].method != "GET" {
		t.Fatalf("%+v", reqs)
	}
	// An empty body is no body: sent directly.
	empty := ""
	req := getUser("repository:local", "1")
	req.Body = &empty
	f.send(req)
	// A raw multipart body typed by the user is a body like any other.
	raw := postUser("repository:local", "--b\r\nContent-Disposition: form-data; name=\"a\"\r\n\r\n1\r\n--b--\r\n")
	raw.ContentType = "multipart/form-data; boundary=b"
	p := f.prepare(raw)
	raw.Confirmation = *p.Confirmation
	f.send(raw)
	for _, h := range []pairDoc{{"Sec-WebSocket-Key", "k"}, {"HTTP2-Settings", "AAA"}} {
		r := getUser("repository:local", "1")
		r.Headers = []pairDoc{h}
		rec := f.do(http.MethodPost, tryItPreparePath, r)
		wantError(t, rec, http.StatusBadRequest, "not_supported")
		if h.Name == "Sec-WebSocket-Key" && !strings.Contains(rec.Body.String(), "WebSocket endpoints are not supported in try-it yet") {
			t.Errorf("%s", rec.Body.String())
		}
	}
	if len(up.requests()) != 8 {
		t.Fatalf("%d requests sent, want 8", len(up.requests()))
	}
}

// TestTryItProxyIgnored checks row TI-18 at the server: proxy variables
// do not divert a send.
func TestTryItProxyIgnored(t *testing.T) {
	proxy, up := newUpstream(t), newUpstream(t)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	f.send(getUser("repository:local", "1"))
	if len(proxy.requests()) != 0 || len(up.requests()) != 1 {
		t.Fatalf("proxy %d, upstream %d", len(proxy.requests()), len(up.requests()))
	}
}

// TestTryItDotSegments checks that a percent-encoded dot segment in a
// template's literal part is refused like a plain one.
func TestTryItDotSegments(t *testing.T) {
	for _, key := range []string{"/a/%2e%2e/{x}", "/a/%2E/{x}", "/a/../{x}", "/a/..%2f/{x}", "/a%5C{x}"} {
		if _, err := fillPath(key, map[string]string{"x": "1"}); err == nil {
			t.Errorf("%s accepted", key)
		}
	}
	for _, v := range []string{"..", ".", "%2e%2e"} {
		p, err := fillPath("/a/{x}", map[string]string{"x": v})
		if v == "%2e%2e" {
			if err != nil || p != "/a/%252e%252e" {
				t.Errorf("%q: %q %v", v, p, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}
