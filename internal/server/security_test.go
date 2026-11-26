package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// apiPaths are API requests that answer 200 on an empty store, one per
// route that does.
var apiPaths = []string{"/api/v0/status", "/api/v0/api/endpoints", "/api/v0/api/calls?transport=http&status=dynamic", "/api/v0/schema/summary", "/api/v0/schema/sets"}

// lookupPaths are API requests for one stored node, which answer 404 on an
// empty store once every security check has passed.
var lookupPaths = []string{"/api/v0/api/endpoint?id=route%3Aa.ts%23GET%2520%252F", "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", "/api/v0/source?id=function%3Aa.ts%23f", "/api/v0/schema/table?id=table%3Aa.sql%23t", "/api/v0/schema/migration?id=migration%3Aa.sql%23migration"}

// allowedStatus is the status a request on an empty store gets when the
// security checks let it through.
func allowedStatus(path string) int {
	for _, p := range lookupPaths {
		if p == path {
			return http.StatusNotFound
		}
	}
	return http.StatusOK
}

// handlerOptions returns options over an empty migrated SQLite store, enough
// for every route.
func handlerOptions(t *testing.T) Options {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return testOptions(t.TempDir(), s)
}

func TestAllowedHosts(t *testing.T) {
	for _, tc := range []struct {
		bound string
		want  []string
	}{
		{"127.0.0.1:7420", []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420"}},
		{"[::1]:8080", []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"}},
		{"127.0.0.2:7420", []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420", "127.0.0.2:7420"}},
	} {
		if got := allowedHosts(netip.MustParseAddrPort(tc.bound)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.bound, got, tc.want)
		}
	}
}

func TestHostAllowList(t *testing.T) {
	opt := handlerOptions(t)
	bound := allowedHosts(netip.MustParseAddrPort("127.0.0.2:7420"))
	for _, tc := range []struct {
		host    string
		allowed bool
		hosts   []string // nil means the default test hosts
	}{
		{"127.0.0.1:7420", true, nil},
		{"localhost:7420", true, nil},
		{"LOCALHOST:7420", true, nil},
		{"LocalHost:7420", true, nil},
		{"[::1]:7420", true, nil},
		{"127.0.0.2:7420", false, nil},
		{"127.0.0.2:7420", true, bound},
		{"localhost:7420", true, bound},
		{"127.0.0.1", false, nil},
		{"localhost", false, nil},
		{"127.0.0.1:7421", false, nil},
		{"localhost:80", false, nil},
		{"localhost.:7420", false, nil},
		{"[::ffff:127.0.0.1]:7420", false, nil},
		{"[0:0:0:0:0:0:0:1]:7420", false, nil},
		{"evil.example:7420", false, nil},
		{"evil.example", false, nil},
		{"localhost:7420.evil.example", false, nil},
		{"loca\u212Ahost:7420", false, nil}, // Kelvin sign folds to k under Unicode rules
		{"", false, nil},
	} {
		hosts := tc.hosts
		if hosts == nil {
			hosts = testHosts
		}
		for _, path := range append(append([]string{"/"}, apiPaths...), lookupPaths...) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			NewHandler(opt, hosts).ServeHTTP(rec, req)
			if tc.allowed {
				if rec.Code != allowedStatus(path) {
					t.Errorf("host %q %s: status %d, want %d", tc.host, path, rec.Code, allowedStatus(path))
				}
				continue
			}
			if rec.Code != http.StatusForbidden || errorCode(t, rec) != "host_not_allowed" {
				t.Errorf("host %q %s: status %d %s, want 403 host_not_allowed", tc.host, path, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestMethods(t *testing.T) {
	opt := handlerOptions(t)
	for _, path := range append(append([]string{"/", "/nope"}, apiPaths...), lookupPaths...) {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := get(t, opt, method, path, nil)
			want := allowedStatus(path)
			if path == "/nope" {
				want = http.StatusNotFound
			}
			if rec.Code != want {
				t.Errorf("%s %s: status %d, want %d", method, path, rec.Code, want)
			}
			if method == http.MethodHead && rec.Body.Len() != 0 && path == "/" {
				t.Errorf("HEAD %s wrote a body", path)
			}
		}
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			rec := get(t, opt, method, path, map[string]string{"Origin": "http://127.0.0.1:7420", "Access-Control-Request-Method": "GET"})
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" || errorCode(t, rec) != "method_not_allowed" {
				t.Errorf("%s %s: status %d, Allow %q", method, path, rec.Code, rec.Header().Get("Allow"))
			}
		}
	}
}

func TestSecFetchSite(t *testing.T) {
	opt := handlerOptions(t)
	for _, tc := range []struct {
		value   string
		allowed bool
	}{
		{"", true},
		{"same-origin", true},
		{"none", true},
		{"cross-site", false},
		{"Cross-Site", false},
		{"same-site", false},
	} {
		header := map[string]string{}
		if tc.value != "" {
			header["Sec-Fetch-Site"] = tc.value
		}
		for _, path := range append(append([]string{}, apiPaths...), lookupPaths...) {
			rec := get(t, opt, http.MethodGet, path, header)
			if tc.allowed && rec.Code != allowedStatus(path) {
				t.Errorf("%s, Sec-Fetch-Site %q: status %d", path, tc.value, rec.Code)
			}
			if !tc.allowed && (rec.Code != http.StatusForbidden || errorCode(t, rec) != "cross_site_request") {
				t.Errorf("%s, Sec-Fetch-Site %q: status %d %s", path, tc.value, rec.Code, rec.Body.String())
			}
		}
		// Pages are not checked: navigating to the UI from a link is fine,
		// and the same-origin policy hides the response from other sites.
		if rec := get(t, opt, http.MethodGet, "/", header); rec.Code != http.StatusOK {
			t.Errorf("page, Sec-Fetch-Site %q: status %d", tc.value, rec.Code)
		}
	}
}

// TestOrigin covers browsers without Fetch Metadata: a present Origin must
// be one of this server's own origins; an absent one is allowed.
func TestOrigin(t *testing.T) {
	opt := handlerOptions(t)
	for _, tc := range []struct {
		origin  string
		present bool
		allowed bool
	}{
		{"", false, true},
		{"http://127.0.0.1:7420", true, true},
		{"http://LOCALHOST:7420", true, true},
		{"http://[::1]:7420", true, true},
		{"", true, false},
		{"null", true, false},
		{"http://evil.example", true, false},
		{"http://127.0.0.1:7421", true, false},
		{"https://127.0.0.1:7420", true, false},
		{"http://127.0.0.1", true, false},
		{"http://127.0.0.1:7420/", true, false},
		{"http://localhost.:7420", true, false},
	} {
		header := map[string]string{}
		if tc.present {
			header["Origin"] = tc.origin
		}
		for _, path := range append(append([]string{}, apiPaths...), lookupPaths...) {
			rec := get(t, opt, http.MethodGet, path, header)
			if tc.allowed && rec.Code != allowedStatus(path) {
				t.Errorf("%s, Origin %q (present %v): status %d", path, tc.origin, tc.present, rec.Code)
			}
			if !tc.allowed && (rec.Code != http.StatusForbidden || errorCode(t, rec) != "cross_site_request") {
				t.Errorf("%s, Origin %q: status %d %s", path, tc.origin, rec.Code, rec.Body.String())
			}
		}
		// Pages are not checked, as for Sec-Fetch-Site.
		if rec := get(t, opt, http.MethodGet, "/", header); rec.Code != http.StatusOK {
			t.Errorf("page, Origin %q: status %d", tc.origin, rec.Code)
		}
	}
	// Two Origin headers are not a single allowed origin.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status", nil)
	req.Host = testHost
	req.Header["Origin"] = []string{"http://127.0.0.1:7420", "http://evil.example"}
	rec := httptest.NewRecorder()
	NewHandler(opt, testHosts).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("two Origin headers: status %d", rec.Code)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	opt := handlerOptions(t)
	failing := opt
	failing.Store = failingStore{Store: opt.Store, runs: true}
	for _, tc := range []struct {
		name   string
		opt    Options
		method string
		path   string
		header map[string]string
		code   int
	}{
		{"page", opt, http.MethodGet, "/", nil, 200},
		{"status", opt, http.MethodGet, "/api/v0/status", nil, 200},
		{"host", opt, http.MethodGet, "/", map[string]string{"Host": "evil.example:7420"}, 403},
		{"cross-site", opt, http.MethodGet, "/api/v0/status", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"api not found", opt, http.MethodGet, "/api/v1/status", nil, 404},
		{"file not found", opt, http.MethodGet, "/nope", nil, 404},
		{"method", opt, http.MethodPost, "/api/v0/status", nil, 405},
		{"options", opt, http.MethodOptions, "/", map[string]string{"Origin": "http://evil.example"}, 405},
		{"store error", failing, http.MethodGet, "/api/v0/status", nil, 500},
		{"endpoints", opt, http.MethodGet, "/api/v0/api/endpoints", nil, 200},
		{"endpoint not found", opt, http.MethodGet, "/api/v0/api/endpoint?id=route%3Aa.ts%23GET%2520%252F", nil, 404},
		{"endpoint bad request", opt, http.MethodGet, "/api/v0/api/endpoint", nil, 400},
		{"calls", opt, http.MethodGet, "/api/v0/api/calls?transport=tauri_ipc&status=dynamic", nil, 200},
		{"calls cross-site", opt, http.MethodGet, "/api/v0/api/calls?transport=http&status=dynamic", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"endpoints method", opt, http.MethodPost, "/api/v0/api/endpoints", nil, 405},
		{"endpoints store error", failing, http.MethodGet, "/api/v0/api/endpoints", nil, 500},
		{"flow not found", opt, http.MethodGet, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", nil, 404},
		{"flow bad request", opt, http.MethodGet, "/api/v0/api/flow", nil, 400},
		{"flow cross-site", opt, http.MethodGet, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"flow origin", opt, http.MethodGet, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", map[string]string{"Origin": "http://evil.example"}, 403},
		{"flow host", opt, http.MethodGet, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", map[string]string{"Host": "evil.example:7420"}, 403},
		{"flow method", opt, http.MethodPost, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", nil, 405},
		{"flow store error", failing, http.MethodGet, "/api/v0/api/flow?id=route%3Aa.ts%23GET%2520%252F", nil, 500},
		{"source not found", opt, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f", nil, 404},
		{"source bad request", opt, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f&context=99", nil, 400},
		{"source cross-site", opt, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"source origin", opt, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f", map[string]string{"Origin": "http://evil.example"}, 403},
		{"source host", opt, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f", map[string]string{"Host": "evil.example:7420"}, 403},
		{"source method", opt, http.MethodPost, "/api/v0/source?id=function%3Aa.ts%23f", nil, 405},
		{"source store error", failing, http.MethodGet, "/api/v0/source?id=function%3Aa.ts%23f", nil, 500},
		{"schema summary", opt, http.MethodGet, "/api/v0/schema/summary", nil, 200},
		{"schema summary method", opt, http.MethodPost, "/api/v0/schema/summary", nil, 405},
		{"schema summary cross-site", opt, http.MethodGet, "/api/v0/schema/summary", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"schema summary store error", failing, http.MethodGet, "/api/v0/schema/summary", nil, 500},
		{"schema sets", opt, http.MethodGet, "/api/v0/schema/sets", nil, 200},
		{"schema sets host", opt, http.MethodGet, "/api/v0/schema/sets", map[string]string{"Host": "evil.example:7420"}, 403},
		{"schema sets origin", opt, http.MethodGet, "/api/v0/schema/sets", map[string]string{"Origin": "http://evil.example"}, 403},
		{"schema sets method", opt, http.MethodPost, "/api/v0/schema/sets", nil, 405},
		{"schema sets store error", failing, http.MethodGet, "/api/v0/schema/sets", nil, 500},
		{"schema table not found", opt, http.MethodGet, "/api/v0/schema/table?id=table%3Aa.sql%23t", nil, 404},
		{"schema table bad request", opt, http.MethodGet, "/api/v0/schema/table", nil, 400},
		{"schema table cross-site", opt, http.MethodGet, "/api/v0/schema/table?id=table%3Aa.sql%23t", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"schema table method", opt, http.MethodPost, "/api/v0/schema/table?id=table%3Aa.sql%23t", nil, 405},
		{"schema table store error", failing, http.MethodGet, "/api/v0/schema/table?id=table%3Aa.sql%23t", nil, 500},
		{"schema migration not found", opt, http.MethodGet, "/api/v0/schema/migration?id=migration%3Aa.sql%23migration", nil, 404},
		{"schema migration bad request", opt, http.MethodGet, "/api/v0/schema/migration?id=table%3Aa.sql%23t", nil, 400},
		{"schema migration host", opt, http.MethodGet, "/api/v0/schema/migration?id=migration%3Aa.sql%23migration", map[string]string{"Host": "evil.example:7420"}, 403},
		{"schema migration method", opt, http.MethodPost, "/api/v0/schema/migration?id=migration%3Aa.sql%23migration", nil, 405},
		{"schema migration store error", failing, http.MethodGet, "/api/v0/schema/migration?id=migration%3Aa.sql%23migration", nil, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, tc.opt, tc.method, tc.path, tc.header)
			if rec.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
			for _, h := range securityHeaders {
				if got := rec.Header().Get(h[0]); got != h[1] {
					t.Errorf("%s = %q, want %q", h[0], got, h[1])
				}
			}
			if strings.HasPrefix(tc.path, "/api/") && rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
			}
			for name := range rec.Header() {
				if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
					t.Errorf("CORS header %s set", name)
				}
			}
		})
	}
	// The policy text is part of the documented contract (D3).
	if securityHeaders[0][1] != "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'" {
		t.Fatal("CSP changed")
	}
}

func TestAPINotFound(t *testing.T) {
	opt := handlerOptions(t)
	for _, path := range []string{"/api", "/api/", "/api/v0", "/api/v0/status/", "/api/v0/nope", "/api/v0/api", "/api/v0/api/", "/api/v0/api/endpoints/", "/api/v0/api/Endpoints", "/api/v0/api/flow/", "/api/v0/api/Flow", "/api/v0/source/", "/api/v0/Source", "/api/v0/source/..%2Fa.ts", "/api/v0/schema", "/api/v0/schema/", "/api/v0/schema/Sets", "/api/v0/schema/sets/", "/api/v0/schema/tables"} {
		rec := get(t, opt, http.MethodGet, path, nil)
		if rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: status %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

// errorCode decodes the error envelope and returns its code.
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("error Content-Type %q", ct)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Errorf("error body %q: %v", rec.Body.String(), err)
	}
	return env.Error.Code
}
