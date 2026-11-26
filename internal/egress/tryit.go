package egress

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// This file holds the client of one "try it" request (T029, ADR 0081). It
// is the only place that builds the TLS configuration of a try-it send
// (system roots, an optional CA bundle, optional certificate pins) and that
// decides whether the send goes through the system proxy.

// TryItOptions configure the client of one try-it send. The zero value
// apart from Timeout is the strict default: system roots only, no proxy.
type TryItOptions struct {
	// Timeout bounds the whole exchange, body included.
	Timeout time.Duration
	// ServerName is the host of the environment's origin; certificates
	// are checked against it.
	ServerName string
	// PinnedSHA256 are lower-case hex SHA-256 fingerprints of leaf
	// certificates (their DER encoding) accepted for this origin even when
	// normal verification fails (self-signed, another name, expired).
	PinnedSHA256 []string
	// CAPEM is a PEM bundle trusted in addition to the system roots.
	CAPEM []byte
	// Proxy, when set, is the proxy every request of the client goes
	// through. It must be a destination of the policy, as must the
	// request's own origin.
	Proxy *url.URL
	// ProxyRootCAs verify an https proxy's certificate; nil means the
	// system roots. The environment's pins and CA file never apply to the
	// proxy. Tests set it to trust a proxy they start.
	ProxyRootCAs *x509.CertPool
}

// ProxyTLSError is a failed TLS handshake with an https proxy (its
// certificate, verified against the system roots for the proxy's own
// name, or the protocol). It is never a statement about the origin's
// certificate.
type ProxyTLSError struct {
	Proxy string
	Err   error
}

func (e *ProxyTLSError) Error() string {
	return fmt.Sprintf("TLS with the proxy %s failed: %v", e.Proxy, e.Err)
}

func (e *ProxyTLSError) Unwrap() error { return e.Err }

// ErrNoCertificates is wrapped when a CA bundle holds no PEM certificate.
var ErrNoCertificates = errors.New("no PEM certificate found")

// CertificateError is a server certificate that is neither trusted by the
// roots in use nor pinned. It carries the leaf certificate so the user can
// inspect it and decide whether to pin it; nothing is accepted silently.
type CertificateError struct {
	Leaf *x509.Certificate
	// Fingerprint is the lower-case hex SHA-256 of the leaf's DER.
	Fingerprint string
	// Pinned are the fingerprints configured for the origin; when there
	// are any, the certificate changed since one was pinned.
	Pinned []string
	// Err is why normal verification failed.
	Err error
}

func (e *CertificateError) Error() string {
	if len(e.Pinned) > 0 {
		return fmt.Sprintf("certificate changed: the server presented %s, pinned %s (%v)", e.Fingerprint, strings.Join(e.Pinned, ", "), e.Err)
	}
	return fmt.Sprintf("certificate %s is not trusted: %v", e.Fingerprint, e.Err)
}

func (e *CertificateError) Unwrap() error { return e.Err }

// Fingerprint returns the lower-case hex SHA-256 of a certificate's DER,
// the form a pin is written in.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// TryItClient returns the client of one "try it" request (T029): every
// connection is dialed through this policy, redirects are never followed
// (the 3xx response is returned as it is, so a Location on another origin
// is shown, never dialed), no connection is kept, and the whole exchange,
// body included, is bounded by o.Timeout. HTTP/1.1 and HTTP/2 over TLS
// only: never HTTP/2 cleartext (h2c).
//
// TLS always verifies: a certificate is accepted when it verifies against
// the system roots plus o.CAPEM for o.ServerName, or when its fingerprint
// is pinned (pins add trust for this one origin; they never disable
// verification of other certificates). Anything else fails with a
// *CertificateError.
//
// Proxy environment variables are read only by SystemProxyFor; without
// o.Proxy the client connects directly.
func (p *Policy) TryItClient(o TryItOptions) (*http.Client, error) {
	var roots *x509.CertPool // nil: the system roots
	if len(o.CAPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(o.CAPEM) {
			return nil, fmt.Errorf("reading the CA bundle: %w", ErrNoCertificates)
		}
		roots = pool
	}
	pins := map[string]bool{}
	for _, fp := range o.PinnedSHA256 {
		pins[fp] = true
	}
	tlsConfig := &tls.Config{
		ServerName: o.ServerName,
		MinVersion: tls.VersionTLS12,
		// Verification is not skipped: VerifyConnection below verifies
		// every certificate chain, with pins as the only addition. The
		// handshake still proves the server holds the leaf's private key.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			leaf := cs.PeerCertificates[0]
			fp := Fingerprint(leaf)
			if pins[fp] {
				return nil
			}
			intermediates := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				intermediates.AddCert(c)
			}
			_, err := leaf.Verify(x509.VerifyOptions{DNSName: o.ServerName, Roots: roots, Intermediates: intermediates})
			if err == nil {
				return nil
			}
			return &CertificateError{Leaf: leaf, Fingerprint: fp, Pinned: o.PinnedSHA256, Err: err}
		},
	}
	var proxy func(*http.Request) (*url.URL, error)
	dial := p.DialContext
	if o.Proxy != nil {
		if !p.allows(o.Proxy.Hostname(), proxyPort(o.Proxy)) {
			return nil, fmt.Errorf("%w: proxy %s is not a destination of this policy", ErrBlocked, o.Proxy.Host)
		}
		// Go's transport would verify an https proxy with TLSClientConfig,
		// the origin's configuration (its name, pins and CA file). The
		// proxy's TLS is made here instead, with its own name and the
		// system roots: the transport sees a plain http proxy whose
		// connection is already TLS.
		transportProxy := o.Proxy
		if o.Proxy.Scheme == "https" {
			proxyAddr := net.JoinHostPort(o.Proxy.Hostname(), strconv.Itoa(int(proxyPort(o.Proxy))))
			u := *o.Proxy
			u.Scheme, u.Host = "http", proxyAddr
			transportProxy = &u
			proxyTLS := &tls.Config{ServerName: o.Proxy.Hostname(), RootCAs: o.ProxyRootCAs, MinVersion: tls.VersionTLS12}
			dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := p.DialContext(ctx, network, address)
				if err != nil || address != proxyAddr {
					return conn, err
				}
				tc := tls.Client(conn, proxyTLS)
				if err := tc.HandshakeContext(ctx); err != nil {
					conn.Close()
					return nil, &ProxyTLSError{Proxy: o.Proxy.Scheme + "://" + proxyAddr, Err: err}
				}
				return tc, nil
			}
		}
		proxy = func(r *http.Request) (*url.URL, error) {
			// The proxy sees only requests to an allowed destination: the
			// allow-list applies to the final destination, not only to the
			// proxy that is dialed.
			if !p.allows(r.URL.Hostname(), urlPort(r.URL)) {
				return nil, fmt.Errorf("%w: %s through the proxy", ErrBlocked, r.URL.Host)
			}
			return transportProxy, nil
		}
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	return &http.Client{
		Transport: &http.Transport{
			Protocols:              protocols,
			DialContext:            dial,
			Proxy:                  proxy,
			TLSClientConfig:        tlsConfig,
			ForceAttemptHTTP2:      true,
			TLSHandshakeTimeout:    dialTimeout,
			ResponseHeaderTimeout:  o.Timeout,
			MaxResponseHeaderBytes: 64 << 10,
			// A policy serves one request, so no connection is kept.
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       o.Timeout,
	}, nil
}

// allows reports whether host and port are a TCP destination of p.
func (p *Policy) allows(host string, port uint16) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		_, ok := p.literals[addrKey(addr, port)]
		return ok
	}
	return p.hasName(host, port)
}

func urlPort(u *url.URL) uint16 {
	if n, err := strconv.ParseUint(u.Port(), 10, 16); err == nil {
		return uint16(n)
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

func proxyPort(u *url.URL) uint16 {
	if u.Port() == "" && strings.HasPrefix(u.Scheme, "socks5") {
		return 1080
	}
	return urlPort(u)
}

// ProxyDestination is the destination a policy needs to reach proxy.
func ProxyDestination(proxy *url.URL, purpose Purpose) Destination {
	return Destination{Host: proxy.Hostname(), Port: proxyPort(proxy), Purpose: purpose}
}

// SystemProxyFor returns the proxy the system's environment names for
// target, or nil: HTTPS_PROXY (or https_proxy) for https, HTTP_PROXY (or
// http_proxy) for http, unless NO_PROXY (or no_proxy) matches the target.
// It follows Go's documented rules (golang.org/x/net/http/httpproxy):
// "localhost" and loopback addresses are never proxied; a NO_PROXY entry
// is "*", an IP address, a CIDR range, or a domain ("example.com" matches
// it and its subdomains, ".example.com" only subdomains), each optionally
// with ":port". A value without a scheme is read as http://. Only http,
// https, socks5 and socks5h proxies are accepted.
//
// getenv reads one variable; the variables are read at every call, so an
// environment change applies to the next send.
func SystemProxyFor(target *url.URL, getenv func(string) string) (*url.URL, error) {
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if host == "localhost" {
		return nil, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil && addr.Unmap().IsLoopback() {
		return nil, nil
	}
	first := func(names ...string) string {
		for _, n := range names {
			if v := getenv(n); v != "" {
				return v
			}
		}
		return ""
	}
	raw := first("HTTP_PROXY", "http_proxy")
	if target.Scheme == "https" {
		raw = first("HTTPS_PROXY", "https_proxy")
	}
	if raw == "" || noProxyMatch(first("NO_PROXY", "no_proxy"), host, urlPort(target)) {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("the proxy variable does not hold a URL")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("proxy scheme %q is not supported (http, https, socks5)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("the proxy URL has no host")
	}
	return u, nil
}

// noProxyMatch reports whether a NO_PROXY list exempts host:port.
func noProxyMatch(list, host string, port uint16) bool {
	hostAddr, hostErr := netip.ParseAddr(host)
	for _, entry := range strings.Split(list, ",") {
		e := strings.ToLower(strings.TrimSpace(entry))
		switch {
		case e == "":
			continue
		case e == "*":
			return true
		case strings.Contains(e, "/"):
			if prefix, err := netip.ParsePrefix(e); err == nil && hostErr == nil && prefix.Contains(hostAddr.Unmap()) {
				return true
			}
			continue
		}
		eHost := e
		if h, p, err := net.SplitHostPort(e); err == nil {
			n, err := strconv.ParseUint(p, 10, 16)
			if err != nil || uint16(n) != port {
				continue
			}
			eHost = h
		}
		eHost = strings.TrimSuffix(strings.Trim(eHost, "[]"), ".")
		if addr, err := netip.ParseAddr(eHost); err == nil {
			if hostErr == nil && addr.Unmap() == hostAddr.Unmap() {
				return true
			}
			continue
		}
		if strings.HasPrefix(eHost, ".") {
			if strings.HasSuffix(host, eHost) {
				return true
			}
			continue
		}
		if host == eHost || strings.HasSuffix(host, "."+eHost) {
			return true
		}
	}
	return false
}
