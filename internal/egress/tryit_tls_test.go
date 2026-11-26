package egress

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCert is a certificate made for a test, with its key.
type testCert struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

func (c testCert) tls() tls.Certificate {
	return tls.Certificate{Certificate: [][]byte{c.der}, PrivateKey: c.key, Leaf: c.cert}
}

func (c testCert) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.der})
}

// makeCert creates a certificate for the names (IP literals become IP
// SANs), signed by parent, or self-signed when parent is nil; ca makes it
// a CA.
func makeCert(t *testing.T, cn string, parent *testCert, ca bool, names ...string) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: cn},
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

// tlsServer serves cert on 127.0.0.1; cert may change between requests.
func tlsServer(t *testing.T, cert *tls.Certificate) (*httptest.Server, uint16) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	// StartTLS would serve httptest's own certificate to clients without
	// SNI, so the TLS listener is set up here.
	srv.Listener = tls.NewListener(srv.Listener, &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return cert, nil }})
	srv.Start()
	srv.URL = strings.Replace(srv.URL, "http://", "https://", 1)
	t.Cleanup(srv.Close)
	return srv, uint16(srv.Listener.Addr().(*net.TCPAddr).Port)
}

// TestTryItCertificates checks the try-it TLS rules: an untrusted
// certificate fails with its leaf and fingerprint; a pinned one is accepted
// for the origin whatever its name; a changed one fails again naming the
// pins; a CA bundle adds a root; a bundle without certificates is refused.
func TestTryItCertificates(t *testing.T) {
	self := makeCert(t, "self", nil, false, "other.test")
	current := self.tls()
	srv, port := tlsServer(t, &current)
	policy := mustPolicy(t, Offline, Destination{Host: "127.0.0.1", Port: port, Purpose: TryIt})
	get := func(o TryItOptions) error {
		o.Timeout, o.ServerName = 2*time.Second, "127.0.0.1"
		c, err := policy.TryItClient(o)
		if err != nil {
			return err
		}
		resp, err := c.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	var ce *CertificateError
	if err := get(TryItOptions{}); !errors.As(err, &ce) || ce.Fingerprint != Fingerprint(self.cert) || len(ce.Pinned) != 0 || ce.Leaf.Subject.CommonName != "self" {
		t.Fatalf("untrusted: %v", err)
	}
	pin := []string{Fingerprint(self.cert)}
	if err := get(TryItOptions{PinnedSHA256: pin}); err != nil {
		t.Fatalf("pinned certificate (named other.test) refused: %v", err)
	}
	other := makeCert(t, "other", nil, false, "127.0.0.1")
	current = other.tls()
	if err := get(TryItOptions{PinnedSHA256: pin}); !errors.As(err, &ce) || ce.Fingerprint != Fingerprint(other.cert) || strings.Join(ce.Pinned, "") != pin[0] || !strings.Contains(err.Error(), "certificate changed") {
		t.Fatalf("changed certificate: %v", err)
	}
	ca := makeCert(t, "Test Root", nil, true)
	leaf := makeCert(t, "leaf", &ca, false, "127.0.0.1")
	current = leaf.tls()
	if err := get(TryItOptions{}); !errors.As(err, &ce) {
		t.Fatalf("CA-signed without the CA: %v", err)
	}
	if err := get(TryItOptions{CAPEM: ca.pem()}); err != nil {
		t.Fatalf("CA bundle: %v", err)
	}
	// A pin does not stop a certificate that verifies from being accepted.
	if err := get(TryItOptions{CAPEM: ca.pem(), PinnedSHA256: pin}); err != nil {
		t.Fatalf("CA bundle with an unrelated pin: %v", err)
	}
	if _, err := policy.TryItClient(TryItOptions{CAPEM: []byte("not pem")}); !errors.Is(err, ErrNoCertificates) {
		t.Fatalf("empty bundle: %v", err)
	}
}

// TestTryItSystemProxy checks the opt-in proxy: SystemProxyFor reads the
// variables per scheme and applies NO_PROXY and the loopback rule; a
// client with a proxy dials only the proxy, which must be a destination,
// and refuses requests to other destinations through it.
func TestTryItSystemProxy(t *testing.T) {
	vars := map[string]string{}
	getenv := func(k string) string { return vars[k] }
	target := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	vars["HTTP_PROXY"], vars["https_proxy"] = "proxy.test:3128", "socks5://socks.test"
	for _, tc := range []struct{ noProxy, url, want string }{
		{"", "http://api.test/x", "http://proxy.test:3128"},
		{"", "https://api.test", "socks5://socks.test"},
		{"", "http://localhost:8000", ""},
		{"", "http://127.0.0.1:8000", ""},
		{"", "http://[::1]:8000", ""},
		{"*", "http://api.test", ""},
		{"api.test", "http://api.test", ""},
		{"test", "http://api.test", ""},
		{".api.test", "http://api.test", "http://proxy.test:3128"},
		{".api.test", "http://x.api.test", ""},
		{"api.test:8080", "http://api.test", "http://proxy.test:3128"},
		{"api.test:80", "http://api.test", ""},
		{"10.0.0.0/8", "http://10.1.2.3", ""},
		{"10.0.0.0/8", "http://192.0.2.1", "http://proxy.test:3128"},
		{"192.0.2.1, other", "http://192.0.2.1", ""},
		{"pi.test", "http://api.test", "http://proxy.test:3128"},
	} {
		vars["NO_PROXY"] = tc.noProxy
		got, err := SystemProxyFor(target(tc.url), getenv)
		if err != nil || (got == nil) != (tc.want == "") || got != nil && got.String() != tc.want {
			t.Errorf("NO_PROXY=%q %s: %v %v, want %q", tc.noProxy, tc.url, got, err, tc.want)
		}
	}
	vars["NO_PROXY"], vars["HTTP_PROXY"] = "", "ftp://p"
	if _, err := SystemProxyFor(target("http://api.test"), getenv); err == nil {
		t.Error("ftp proxy accepted")
	}

	// A forward proxy that answers itself.
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.RequestURI)
		io.WriteString(w, "via proxy")
	}))
	defer proxy.Close()
	proxyURL := target(proxy.URL)
	events := record(t)
	api := Destination{Host: "api.test", Port: 80, Purpose: TryIt}
	if _, err := mustPolicy(t, Offline, api).TryItClient(TryItOptions{Timeout: time.Second, Proxy: proxyURL}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("a proxy outside the policy: %v", err)
	}
	c, err := mustPolicy(t, Offline, api, ProxyDestination(proxyURL, TryIt)).TryItClient(TryItOptions{Timeout: time.Second, Proxy: proxyURL})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get("http://api.test/x?y=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := c.Get("http://elsewhere.test/"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("another destination through the proxy: %v", err)
	}
	if strings.Join(seen, ",") != "GET http://api.test/x?y=1" {
		t.Fatalf("proxy saw %v", seen)
	}
	for _, e := range events() {
		if e.Op == "dial" && (!e.Allowed || e.Purpose != TryIt || e.Address != proxyURL.Host) {
			t.Errorf("dial event %+v", e)
		}
	}

	// An https proxy is verified on its own: its name and the proxy roots,
	// never the origin's pins or CA file.
	ca := makeCert(t, "Proxy Root", nil, true)
	leaf := makeCert(t, "proxy", &ca, false, "127.0.0.1")
	cert := leaf.tls()
	tlsProxy := httptest.NewUnstartedServer(proxy.Config.Handler)
	tlsProxy.Listener = tls.NewListener(tlsProxy.Listener, &tls.Config{Certificates: []tls.Certificate{cert}})
	tlsProxy.Start()
	defer tlsProxy.Close()
	secure := target("https://" + tlsProxy.Listener.Addr().String())
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	pol := mustPolicy(t, Offline, api, ProxyDestination(secure, TryIt))
	via := func(o TryItOptions) error {
		o.Timeout, o.Proxy = 2*time.Second, secure
		c, err := pol.TryItClient(o)
		if err != nil {
			return err
		}
		resp, err := c.Get("http://api.test/secure")
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if err := via(TryItOptions{ProxyRootCAs: roots, ServerName: "api.test", PinnedSHA256: []string{strings.Repeat("0", 64)}}); err != nil {
		t.Fatalf("https proxy: %v", err)
	}
	var pe *ProxyTLSError
	var ce *CertificateError
	err = via(TryItOptions{ServerName: "api.test", PinnedSHA256: []string{Fingerprint(leaf.cert)}, CAPEM: ca.pem()})
	if !errors.As(err, &pe) || errors.As(err, &ce) {
		t.Fatalf("the origin's pins or CA file trusted the proxy: %v", err)
	}
	if seen[len(seen)-1] != "GET http://api.test/secure" {
		t.Fatalf("proxy saw %v", seen)
	}
}

// TestTryItTLSConfigOnlyInEgress checks that no other package builds a TLS
// configuration: certificate trust for try-it is decided here only.
func TestTryItTLSConfigOnlyInEgress(t *testing.T) {
	root := moduleRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "egress" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "tls.Config") || strings.Contains(string(src), "InsecureSkipVerify") {
			t.Errorf("%s builds a TLS configuration; only internal/egress may", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
