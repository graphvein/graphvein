package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
)

// testCert is a certificate generated for a test, with its key.
type testCert struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

func (c testCert) tlsCert() *tls.Certificate {
	return &tls.Certificate{Certificate: [][]byte{c.der}, PrivateKey: c.key, Leaf: c.cert}
}

func (c testCert) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.der})
}

// makeTestCert creates a certificate for names (IP literals become IP
// SANs) signed by parent, or self-signed when parent is nil.
func makeTestCert(t *testing.T, cn string, parent *testCert, ca bool, names ...string) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: cn, Organization: []string{"Graphvein test"}},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: ca,
	}
	if ca {
		tmpl.KeyUsage |= x509.KeyUsageCertSign
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{cert, der, key}
}

// tlsUpstream is an upstream on 127.0.0.1 serving the certificate cert
// holds at each handshake.
func tlsUpstream(t *testing.T, cert *atomic.Pointer[tls.Certificate]) (*upstream, string) {
	t.Helper()
	u := newUpstream(t)
	u.Server.Close()
	srv := httptest.NewUnstartedServer(u.Server.Config.Handler)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Listener = tls.NewListener(srv.Listener, &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return cert.Load(), nil }})
	srv.Start()
	t.Cleanup(srv.Close)
	return u, "https://" + srv.Listener.Addr().String()
}

// errorDetails decodes an error answer's code and details.
func errorDetails(t *testing.T, rec *httptest.ResponseRecorder, status int, code string, details any) {
	t.Helper()
	wantError(t, rec, status, code)
	var env struct {
		Error struct {
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(env.Error.Details)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(details); err != nil {
		t.Fatalf("details %s: %v", env.Error.Details, err)
	}
}

// setOptions previews and applies an options edit of environment name in
// graphvein.yaml and returns the applied answer.
func (f *tryFixture) setOptions(opts optionsEditDoc) envEditResponse {
	f.t.Helper()
	req := envEditRequest{Target: "repository", BaseHash: f.list("").Files[0].Hash, Options: &opts}
	var prev envEditResponse
	decodeStrict(f.t, f.do(http.MethodPost, tryItEnvironmentsPath, req), http.StatusOK, &prev)
	if !prev.Valid {
		f.t.Fatalf("options edit invalid: %+v", prev)
	}
	req.Apply, req.ResultHash = true, prev.ResultHash
	var done envEditResponse
	decodeStrict(f.t, f.do(http.MethodPost, tryItEnvironmentsPath, req), http.StatusOK, &done)
	return done
}

// TestTryItCertificatePin checks row TI-29: an untrusted certificate is
// answered with its description and fingerprint; once pinned through the
// options edit it is accepted for that origin whatever its name; a
// changed certificate fails again with the pins and the new fingerprint.
func TestTryItCertificatePin(t *testing.T) {
	self := makeTestCert(t, "dev box", nil, false, "other.test")
	var cert atomic.Pointer[tls.Certificate]
	cert.Store(self.tlsCert())
	up, base := tlsUpstream(t, &cert)
	f := newTryFixture(t, envConfig(envEntry("dev", "local", base)))
	f.selectEnv("repository:dev", false)
	var d certificateDetails
	errorDetails(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1")), http.StatusBadGateway, "upstream_certificate_untrusted", &d)
	fp := egress.Fingerprint(self.cert)
	if d.Certificate.SHA256 != fp || !d.Certificate.SelfSigned || d.Certificate.Subject != "CN=dev box,O=Graphvein test" || strings.Join(d.Certificate.SANs, ",") != "other.test" ||
		d.Origin != base || d.Environment != "repository:dev" || len(d.PinnedSHA256) != 0 || d.Reason == "" || d.Certificate.NotAfter == "" {
		t.Fatalf("%+v", d)
	}
	// Pinning (written with colons and upper case, as browsers show it)
	// changes the environment's key: it must be selected again.
	shown := strings.ToUpper(fp[:2]) + ":" + strings.ToUpper(fp[2:])
	done := f.setOptions(optionsEditDoc{Name: "dev", TLS: &tlsDoc{Pins: []pinDoc{{SHA256: shown}}}})
	if !strings.Contains(done.Diff, "+      tls:\n+        pinned_sha256:\n+          - "+fp+"\n") {
		t.Fatalf("diff %s", done.Diff)
	}
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1")), http.StatusConflict, "environment_not_selected")
	l := f.list("")
	if o := l.Environments[0].Options; o.TLS == nil || len(o.TLS.Pins) != 1 || o.TLS.Pins[0].SHA256 != fp || o.TLS.Pins[0].Origin != nil {
		t.Fatalf("%+v", o)
	}
	f.selectEnv("repository:dev", false)
	if got := f.send(getUser("repository:dev", "1")); got.Status != 200 {
		t.Fatalf("%+v", got)
	}
	// The server now presents another certificate (valid for the name, but
	// not trusted and not pinned): refused, never silently accepted.
	other := makeTestCert(t, "dev box", nil, false, "127.0.0.1")
	cert.Store(other.tlsCert())
	errorDetails(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "2")), http.StatusBadGateway, "upstream_certificate_changed", &d)
	if d.Certificate.SHA256 != egress.Fingerprint(other.cert) || strings.Join(d.PinnedSHA256, "") != fp {
		t.Fatalf("%+v", d)
	}
	if n := len(up.requests()); n != 1 {
		t.Fatalf("%d requests reached the server, want 1", n)
	}
}

// TestTryItCAFile checks row TI-42: a CA bundle named by tls.ca_file
// (relative to the repository) is trusted besides the system roots, read
// at send time; a missing or empty bundle is reported.
func TestTryItCAFile(t *testing.T) {
	ca := makeTestCert(t, "Test Root CA", nil, true)
	leaf := makeTestCert(t, "api", &ca, false, "127.0.0.1")
	var cert atomic.Pointer[tls.Certificate]
	cert.Store(leaf.tlsCert())
	up, base := tlsUpstream(t, &cert)
	f := newTryFixture(t, envConfig(envEntry("dev", "local", base, "tls: {ca_file: certs/root.pem}")))
	f.selectEnv("repository:dev", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1")), http.StatusConflict, "ca_file_error")
	// Not a regular file.
	if err := os.MkdirAll(filepath.Join(f.root, "certs", "root.pem"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.selectEnv("repository:dev", false)
	rec := f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1"))
	wantError(t, rec, http.StatusConflict, "ca_file_error")
	if !strings.Contains(rec.Body.String(), "not a regular file") {
		t.Fatal(rec.Body.String())
	}
	if err := os.Remove(filepath.Join(f.root, "certs", "root.pem")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "certs", "root.pem"), []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.selectEnv("repository:dev", false)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1")), http.StatusConflict, "ca_file_error")
	if err := os.WriteFile(filepath.Join(f.root, "certs", "root.pem"), ca.pem(), 0o644); err != nil {
		t.Fatal(err)
	}
	// The bundle's content is part of the environment's identity:
	// it changed since the selection, so it is selected again.
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "1")), http.StatusConflict, "environment_not_selected")
	f.selectEnv("repository:dev", false)
	if got := f.send(getUser("repository:dev", "1")); got.Status != 200 {
		t.Fatalf("%+v", got)
	}
	// A certificate from another CA is still refused.
	stranger := makeTestCert(t, "api", nil, false, "127.0.0.1")
	cert.Store(stranger.tlsCert())
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:dev", "2")), http.StatusBadGateway, "upstream_certificate_untrusted")
	if n := len(up.requests()); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// TestTryItSystemProxy checks row TI-43: an environment with
// use_system_proxy sends through the proxy HTTP_PROXY names (credentials
// in the proxy URL are used, never shown); prepare names the proxy; an
// environment without it ignores the variables (TI-18).
func TestTryItSystemProxy(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.RequestURI+" "+r.Header.Get("Proxy-Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "from proxy")
	}))
	defer proxy.Close()
	proxyAddr := strings.TrimPrefix(proxy.URL, "http://")
	var st *tryItState
	tryItTestHook = func(s *tryItState) { st = s }
	t.Cleanup(func() { tryItTestHook = nil })
	t.Setenv("HTTP_PROXY", "http://user:proxy-pass-7@"+proxyAddr)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	f := newTryFixture(t, envConfig(envEntry("remote", "staging", "http://api.example.test", "use_system_proxy: true")))
	f.selectEnv("repository:remote", false)
	p := f.prepare(getUser("repository:remote", "1"))
	if p.Proxy == nil || *p.Proxy != "http://"+proxyAddr {
		t.Fatalf("prepare proxy %v", p.Proxy)
	}
	got := f.send(getUser("repository:remote", "1"))
	if got.Status != 200 || got.Body != "from proxy" {
		t.Fatalf("%+v", got)
	}
	mu.Lock()
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "GET http://api.example.test/users/1 Basic ") {
		t.Fatalf("proxy saw %v", seen)
	}
	if strings.Contains(f.logs.String(), "proxy-pass-7") || strings.Contains(f.do(http.MethodGet, tryItEnvironmentsPath, nil).Body.String(), "proxy-pass-7") {
		t.Fatal("proxy credentials shown")
	}
	mu.Unlock()
	if !p.ProxyCleartext {
		t.Fatal("an http environment through a proxy not marked cleartext")
	}

	// An https proxy is verified with its own name and the proxy roots,
	// never with the environment's pins or CA file.
	ca := makeTestCert(t, "Proxy Root", nil, true)
	leaf := makeTestCert(t, "proxy", &ca, false, "127.0.0.1")
	tlsProxy := httptest.NewUnstartedServer(proxy.Config.Handler)
	tlsProxy.Config.ErrorLog = log.New(io.Discard, "", 0)
	tlsProxy.Listener = tls.NewListener(tlsProxy.Listener, &tls.Config{Certificates: []tls.Certificate{*leaf.tlsCert()}})
	tlsProxy.Start()
	defer tlsProxy.Close()
	if err := os.WriteFile(filepath.Join(f.root, "proxy-ca.pem"), ca.pem(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_PROXY", "https://"+tlsProxy.Listener.Addr().String())
	cfg := envConfig(envEntry("remote", "staging", "http://api.example.test", "use_system_proxy: true",
		"tls: {ca_file: proxy-ca.pem, pinned_sha256: ["+egress.Fingerprint(leaf.cert)+"]}"))
	if err := os.WriteFile(filepath.Join(f.root, "graphvein.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	f.selectEnv("repository:remote", false)
	rec := f.do(http.MethodPost, tryItSendPath, getUser("repository:remote", "2"))
	wantError(t, rec, http.StatusBadGateway, "proxy_tls_error")
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	st.proxyRoots = roots
	if got := f.send(getUser("repository:remote", "3")); got.Body != "from proxy" {
		t.Fatalf("%+v", got)
	}
	mu.Lock()
	if len(seen) != 2 || !strings.HasPrefix(seen[1], "GET http://api.example.test/users/3 ") {
		t.Fatalf("proxy saw %v", seen)
	}
	mu.Unlock()

	t.Setenv("HTTP_PROXY", "ftp://"+proxyAddr)
	wantError(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:remote", "1")), http.StatusConflict, "invalid_proxy")
}

// TestTryItEnvironmentLimits checks row TI-44: an environment's limits
// replace the defaults (listed as effective limits), within the ceilings;
// a cut or refused request names the limit and how to raise it.
func TestTryItEnvironmentLimits(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL, "limits: {request_bytes: 10, response_bytes: 100, timeout_seconds: 1}")))
	l := f.list("")
	if l.Environments[0].EffectiveLimits != (envLimitsDoc{10, 100, 1}) || l.LimitCeilings != (envLimitsDoc{64 << 20, 256 << 20, 600}) ||
		*l.Environments[0].Options.Limits.RequestBytes != 10 {
		t.Fatalf("%+v %+v", l.Environments[0], l.LimitCeilings)
	}
	f.selectEnv("repository:local", false)
	var ld limitDoc
	errorDetails(t, f.do(http.MethodPost, tryItPreparePath, postUser("repository:local", "12345678901")), http.StatusRequestEntityTooLarge, "body_too_large", &ld)
	if ld.Limit != "request_bytes" || ld.Value != 10 || ld.Ceiling != 64<<20 || !strings.Contains(ld.Hint, "limits.request_bytes") {
		t.Fatalf("%+v", ld)
	}
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, strings.Repeat("y", 150))
	}
	got := f.send(getUser("repository:local", "1"))
	if !got.Truncated || got.BodyBytes != 100 || got.TruncatedLimit == nil || got.TruncatedLimit.Limit != "response_bytes" || got.TruncatedLimit.Ceiling != 256<<20 {
		t.Fatalf("%+v", got)
	}
	up.reply = func(w http.ResponseWriter, r *http.Request) { time.Sleep(1500 * time.Millisecond) }
	errorDetails(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:local", "1")), http.StatusGatewayTimeout, "upstream_timeout", &ld)
	if ld.Limit != "timeout_seconds" || ld.Value != 1 || ld.Ceiling != 600 {
		t.Fatalf("%+v", ld)
	}
	// Raising a limit is an options edit; limits do not change the key.
	f.setOptions(optionsEditDoc{Name: "local", Limits: &limitsSetDoc{RequestBytes: newPtr(int64(2 << 20))}})
	big := strings.Repeat("z", 1<<20+5)
	p := f.prepare(postUser("repository:local", big))
	req := postUser("repository:local", big)
	req.Confirmation = *p.Confirmation
	up.reply = nil
	if got := f.send(req); got.Status != 200 {
		t.Fatalf("%+v", got)
	}
}

// TestTryItMultipart checks row TI-45: a multipart/form-data body built
// by the server from fields and base64 files, summarised by prepare (names,
// file names, sizes), bound by the confirmation, received intact, never
// logged.
func TestTryItMultipart(t *testing.T) {
	up := newUpstream(t)
	var got struct {
		fields map[string][]string
		file   string
		name   string
		ctype  string
	}
	up.reply = func(w http.ResponseWriter, r *http.Request) {}
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	f.selectEnv("repository:local", false)
	file := []byte("\x89PNG file-content-9\x00\xff")
	parts := []partDoc{
		{Name: "title", Value: newPtr("hello multipart-field-8")},
		{Name: "avatar", Filename: newPtr(`a "b".png`), ContentType: "image/png", DataBase64: newPtr(base64.StdEncoding.EncodeToString(file))},
	}
	req := tryRequest{Environment: "repository:local", Route: routePostUsers, Path: "/users", Method: "POST", Multipart: parts}
	p := f.prepare(req)
	if len(p.Multipart) != 2 || p.Multipart[1].Bytes != len(file) || *p.Multipart[1].Filename != `a "b".png` || p.Multipart[0].Filename != nil ||
		!strings.Contains(strings.Join(p.Headers, ","), "Content-Type") || !p.ConfirmationRequired {
		t.Fatalf("%+v", p)
	}
	req.Confirmation = *p.Confirmation
	f.send(req)
	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatal("not sent")
	}
	r, _ := http.NewRequest("POST", "/", strings.NewReader(reqs[0].body))
	r.Header.Set("Content-Type", reqs[0].header.Get("Content-Type"))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	got.fields = r.MultipartForm.Value
	fh := r.MultipartForm.File["avatar"]
	if len(fh) != 1 {
		t.Fatalf("files %+v", r.MultipartForm.File)
	}
	fr, _ := fh[0].Open()
	data, _ := io.ReadAll(fr)
	got.file, got.name, got.ctype = string(data), fh[0].Filename, fh[0].Header.Get("Content-Type")
	if strings.Join(got.fields["title"], "") != "hello multipart-field-8" || got.file != string(file) || got.name != `a "b".png` || got.ctype != "image/png" {
		t.Fatalf("%+v", got)
	}
	for _, v := range []string{"multipart-field-8", "file-content-9"} {
		if strings.Contains(f.logs.String(), v) {
			t.Fatalf("log holds %s", v)
		}
	}
	for _, bad := range [][]partDoc{
		{{Name: "x"}},
		{{Name: "x", Value: newPtr("a"), DataBase64: newPtr("YQ==")}},
		{{Name: "x", DataBase64: newPtr("not base64!")}},
		{{Name: "", Value: newPtr("a")}},
		{{Name: "x\r\ny", Value: newPtr("a")}},
	} {
		r := req
		r.Confirmation, r.Multipart = "", bad
		wantError(t, f.do(http.MethodPost, tryItPreparePath, r), http.StatusBadRequest, "bad_request")
	}
	withBody := req
	withBody.Confirmation, withBody.Body = "", newPtr("x")
	wantError(t, f.do(http.MethodPost, tryItPreparePath, withBody), http.StatusBadRequest, "bad_request")
	tooBig := req
	tooBig.Confirmation, tooBig.Multipart = "", []partDoc{{Name: "f", Filename: newPtr("f"), Value: newPtr(strings.Repeat("a", maxTryItBody))}}
	wantError(t, f.do(http.MethodPost, tryItPreparePath, tooBig), http.StatusRequestEntityTooLarge, "body_too_large")
}

// TestTryItFreeForm checks row TI-26: a path that does not match the
// operation is sent only as a free-form request after a one-time
// confirmation (a grant does not cover it), relative to the base URL, with
// the encoding and dot-segment rules of operation paths.
func TestTryItFreeForm(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL+"/api")))
	f.selectEnv("repository:local", false)
	free := tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/admin/a b/%41", Method: "GET", FreeForm: true}
	wantError(t, f.do(http.MethodPost, tryItSendPath, free), http.StatusConflict, "confirmation_required")
	p := f.prepare(free)
	if !p.FreeForm || p.URL != up.URL+"/api/admin/a%20b/%41" || strings.Join(p.ConfirmationReasons, ",") != "free_form_path" || p.Confirmation == nil {
		t.Fatalf("%+v", p)
	}
	free.Confirmation = *p.Confirmation
	f.send(free)
	// Another method than the route's, on an any-method route.
	anyRoute := tryRequest{Environment: "repository:local", Route: routeAny, Path: "/any", Method: "PATCH", FreeForm: true}
	p = f.prepare(anyRoute)
	if strings.Join(p.ConfirmationReasons, ",") != "free_form_path,method" {
		t.Fatalf("%+v", p.ConfirmationReasons)
	}
	// A grant for the environment does not cover a free-form path.
	anyRoute.Confirmation, anyRoute.Remember = *p.Confirmation, true
	f.send(anyRoute)
	anyRoute.Confirmation, anyRoute.Remember = "", false
	wantError(t, f.do(http.MethodPost, tryItSendPath, anyRoute), http.StatusConflict, "confirmation_required")
	for _, tc := range []struct {
		path, code string
	}{
		{"admin", "bad_request"},
		{"/a/../b", "invalid_path_value"},
		{"/a/%2e%2E/b", "invalid_path_value"},
		{"/a/./b", "invalid_path_value"},
		{"/a?x=1", "bad_request"},
		{"/a/..%2f..%2Fb", "invalid_path_value"},
		{"/a%5cb", "invalid_path_value"},
		{"/a\\b", "invalid_path_value"},
		{"/a#f", "bad_request"},
	} {
		r := tryRequest{Environment: "repository:local", Route: routeGetUser, Path: tc.path, Method: "GET", FreeForm: true}
		wantError(t, f.do(http.MethodPost, tryItPreparePath, r), http.StatusBadRequest, tc.code)
	}
	withValues := tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/x", Method: "GET", FreeForm: true, PathValues: map[string]string{"id": "1"}}
	wantError(t, f.do(http.MethodPost, tryItPreparePath, withValues), http.StatusBadRequest, "bad_request")
	wantError(t, f.do(http.MethodPost, tryItPreparePath, tryRequest{Environment: "repository:local", Route: "route:nope#GET%20%2F", Path: "/x", Method: "GET", FreeForm: true}), http.StatusNotFound, "unknown_operation")
	wantError(t, f.do(http.MethodPost, tryItPreparePath, tryRequest{Environment: "repository:local", Route: routeGetUser, Path: "/x", Method: "CONNECT", FreeForm: true}), http.StatusBadRequest, "bad_request")
	reqs := up.requests()
	if len(reqs) != 2 || reqs[0].uri != "/api/admin/a%20b/%41" || reqs[1].method != "PATCH" || reqs[1].uri != "/api/any" {
		t.Fatalf("%+v", reqs)
	}
	if strings.Contains(f.logs.String(), "admin") || !strings.Contains(f.logs.String(), "path=(free-form)") {
		t.Fatalf("log:\n%s", f.logs.String())
	}
}

// TestTryItRedirectFollow checks row TI-46: a redirect is never followed;
// the answer says how to follow it once, through an environment that holds
// the target or after adding the target's origin as an environment
// (guarded edit), as a free-form request with its own confirmation.
func TestTryItRedirectFollow(t *testing.T) {
	other := newUpstream(t)
	up := newUpstream(t)
	up.reply = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			http.Redirect(w, r, other.URL+"/elsewhere?x=1&y=a+b#frag", http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, "/api/users/2", http.StatusFound)
	}
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL+"/api")))
	f.selectEnv("repository:local", false)
	got := f.send(getUser("repository:local", "1"))
	rf := got.RedirectFollow
	if rf == nil || !rf.SameOrigin || rf.Environment == nil || *rf.Environment != "repository:local" || rf.Path != "/users/2" || rf.Method != "GET" || rf.SuggestedEnvironment != nil {
		t.Fatalf("same origin: %+v", rf)
	}
	p := f.prepare(postUser("repository:local", `{"a":1}`))
	req := postUser("repository:local", `{"a":1}`)
	req.Confirmation = *p.Confirmation
	got = f.send(req)
	rf = got.RedirectFollow
	otherHost := strings.TrimPrefix(other.URL, "http://")
	if got.Status != 307 || rf == nil || rf.SameOrigin || rf.Environment != nil || rf.Origin != other.URL || rf.Path != "/elsewhere" || rf.Method != "POST" || !rf.KeepsBody ||
		rf.URL != other.URL+"/elsewhere?x=1&y=a+b" || len(rf.Query) != 2 || rf.Query[1] != (pairDoc{"y", "a b"}) ||
		rf.SuggestedEnvironment == nil || rf.SuggestedEnvironment.BaseURL != other.URL || rf.SuggestedEnvironment.Name != strings.ReplaceAll(otherHost, ":", "-") {
		t.Fatalf("cross origin: %+v %+v", rf, rf.SuggestedEnvironment)
	}
	if len(other.requests()) != 0 {
		t.Fatal("followed")
	}
	// Add the target's origin as an environment through the guarded edit.
	edit := envEditRequest{Target: "repository", BaseHash: f.list("").Files[0].Hash, Environment: envEditDoc{Name: rf.SuggestedEnvironment.Name, Label: "local", BaseURL: rf.SuggestedEnvironment.BaseURL}}
	var prev envEditResponse
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, edit), http.StatusOK, &prev)
	edit.Apply, edit.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, edit), http.StatusOK, &prev)
	// Follow once: select it and send the target as a free-form request.
	f.selectEnv(prev.Environment, false)
	follow := tryRequest{Environment: prev.Environment, Route: routePostUsers, Path: rf.Path, Query: rf.Query, Method: rf.Method, FreeForm: true, Body: newPtr(`{"a":1}`), ContentType: "application/json"}
	wantError(t, f.do(http.MethodPost, tryItSendPath, follow), http.StatusConflict, "confirmation_required")
	p = f.prepare(follow)
	follow.Confirmation = *p.Confirmation
	f.send(follow)
	if reqs := other.requests(); len(reqs) != 1 || reqs[0].uri != "/elsewhere?x=1&y=a+b" || reqs[0].body != `{"a":1}` || reqs[0].method != "POST" {
		t.Fatalf("%+v", reqs)
	}
	// Now the environment exists, a redirect there names it.
	f.selectEnv("repository:local", false)
	p = f.prepare(postUser("repository:local", `{}`))
	req = postUser("repository:local", `{}`)
	req.Confirmation = *p.Confirmation
	if rf = f.send(req).RedirectFollow; rf.Environment == nil || *rf.Environment != prev.Environment || rf.SuggestedEnvironment != nil {
		t.Fatalf("%+v", rf)
	}
}

// TestTryItSendWithoutAuth checks row TI-47: when the auth variable is not
// set, prepare names what is missing and issues no confirmation; the
// request is sent without auth only with send_without_auth, GET included,
// and the choice is bound into the confirmation.
func TestTryItSendWithoutAuth(t *testing.T) {
	up := newUpstream(t)
	t.Setenv("GV_TRYIT_MISSING", "")
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL, "auth: {header: X-Api-Key, env: GV_TRYIT_MISSING}")))
	f.selectEnv("repository:local", false)
	var am authMissingDoc
	errorDetails(t, f.do(http.MethodPost, tryItSendPath, getUser("repository:local", "1")), http.StatusBadRequest, "auth_missing", &am)
	if am != (authMissingDoc{"X-Api-Key", "GV_TRYIT_MISSING"}) {
		t.Fatalf("%+v", am)
	}
	p := f.prepare(postUser("repository:local", "{}"))
	if p.AuthMissing == nil || p.Confirmation != nil || !p.ConfirmationRequired || p.AuthHeader != nil {
		t.Fatalf("%+v", p)
	}
	g := getUser("repository:local", "1")
	g.SendWithoutAuth = true
	f.send(g)
	post := postUser("repository:local", "{}")
	post.SendWithoutAuth = true
	p = f.prepare(post)
	if p.Confirmation == nil || !p.SendWithoutAuth {
		t.Fatalf("%+v", p)
	}
	// The confirmation is bound to the choice.
	without := postUser("repository:local", "{}")
	without.Confirmation = *p.Confirmation
	wantError(t, f.do(http.MethodPost, tryItSendPath, without), http.StatusBadRequest, "auth_missing")
	p = f.prepare(post)
	post.Confirmation = *p.Confirmation
	f.send(post)
	// Once the variable is set, the choice is refused and a confirmation
	// made without auth no longer applies.
	p = f.prepare(func() tryRequest { r := postUser("repository:local", "{}"); r.SendWithoutAuth = true; return r }())
	t.Setenv("GV_TRYIT_MISSING", "k")
	post.Confirmation = *p.Confirmation
	wantError(t, f.do(http.MethodPost, tryItSendPath, post), http.StatusBadRequest, "bad_request")
	reqs := up.requests()
	if len(reqs) != 2 || reqs[0].header.Get("X-Api-Key") != "" || reqs[1].header.Get("X-Api-Key") != "" {
		t.Fatalf("%+v", reqs)
	}
}

// TestTryItEnvironmentOptions checks row TI-48: the options edit replaces
// an environment's tls, use_system_proxy and limits through the guarded
// edit (preview, hash, apply), validates every value, and is refused for
// an unknown environment.
func TestTryItEnvironmentOptions(t *testing.T) {
	up := newUpstream(t)
	f := newTryFixture(t, envConfig(envEntry("local", "local", up.URL)))
	fp := strings.Repeat("0f", 32)
	hash := f.list("").Files[0].Hash
	req := envEditRequest{Target: "repository", BaseHash: hash, Options: &optionsEditDoc{Name: "local",
		TLS: &tlsDoc{Pins: []pinDoc{{SHA256: fp}}, CAFile: newPtr("ca.pem")}, UseSystemProxy: true,
		Limits: &limitsSetDoc{ResponseBytes: newPtr(int64(8 << 20)), TimeoutSeconds: newPtr(120)}}}
	var prev envEditResponse
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, req), http.StatusOK, &prev)
	want := "+      tls:\n+        pinned_sha256:\n+          - " + fp + "\n+        ca_file: ca.pem\n+      use_system_proxy: true\n+      limits:\n+        response_bytes: 8388608\n+        timeout_seconds: 120\n"
	if !prev.Valid || prev.Applied || !strings.Contains(prev.Diff, want) || prev.Environment != "repository:local" {
		t.Fatalf("%+v\n%s", prev, prev.Diff)
	}
	if l := f.list(""); l.Environments[0].Options.TLS != nil {
		t.Fatal("a preview wrote the file")
	}
	stale := req
	stale.BaseHash = "sha256:old"
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, stale), http.StatusConflict, "file_changed")
	req.Apply, req.ResultHash = true, prev.ResultHash
	decodeStrict(t, f.do(http.MethodPost, tryItEnvironmentsPath, req), http.StatusOK, &prev)
	e := f.list("").Environments[0]
	if !e.Options.UseSystemProxy || *e.Options.TLS.CAFile != "ca.pem" || e.EffectiveLimits != (envLimitsDoc{maxTryItBody, 8 << 20, 120}) {
		t.Fatalf("%+v", e)
	}
	// Clearing every option.
	f.setOptions(optionsEditDoc{Name: "local"})
	if e = f.list("").Environments[0]; e.Options.TLS != nil || e.Options.UseSystemProxy || e.Options.Limits != nil {
		t.Fatalf("%+v", e.Options)
	}
	hash = f.list("").Files[0].Hash
	for _, bad := range []optionsEditDoc{
		{Name: "local", TLS: &tlsDoc{Pins: []pinDoc{{SHA256: "abc"}}}},
		{Name: "local", TLS: &tlsDoc{Pins: []pinDoc{{SHA256: fp, Origin: newPtr("ftp://x")}}}},
		{Name: "local", TLS: &tlsDoc{CAFile: newPtr("")}},
		{Name: "local", Limits: &limitsSetDoc{RequestBytes: newPtr(int64(64<<20 + 1))}},
		{Name: "local", Limits: &limitsSetDoc{TimeoutSeconds: newPtr(0)}},
		{Name: "local", Limits: &limitsSetDoc{ResponseBytes: newPtr(int64(256<<20 + 1))}},
		{Name: "a b"},
	} {
		wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, envEditRequest{Target: "repository", BaseHash: hash, Options: &bad}), http.StatusBadRequest, "bad_request")
	}
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, envEditRequest{Target: "repository", BaseHash: hash, Options: &optionsEditDoc{Name: "nope"}}), http.StatusBadRequest, "edit_refused")
	both := envEditRequest{Target: "repository", BaseHash: hash, Options: &optionsEditDoc{Name: "local"}, Environment: envEditDoc{Name: "x"}}
	wantError(t, f.do(http.MethodPost, tryItEnvironmentsPath, both), http.StatusBadRequest, "bad_request")
}
