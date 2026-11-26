package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dialTimeout bounds a single connection attempt. It matches the value the
// Postgres connection used before this package owned dialing.
const dialTimeout = 10 * time.Second

// Destination is one host and port a Policy may reach, and why.
type Destination struct {
	// Host is a DNS name, an IP literal, or the absolute directory of a
	// unix socket (as Postgres configures local sockets).
	Host string
	// Port is required. For a socket directory it selects the one socket
	// file Postgres uses, <Host>/.s.PGSQL.<Port>.
	Port    uint16
	Purpose Purpose
}

// Policy decides which lookups and dials are allowed. It resolves names
// itself and dials only IP literals, so a DNS answer can never widen the
// allow-list beyond the destination that asked for it.
type Policy struct {
	mode     Mode
	names    []Destination      // DNS-name destinations, host normalised
	sockets  map[string]Purpose // allowed unix-socket paths, cleaned
	literals map[netip.AddrPort]Purpose
	resolver *net.Resolver
	dialer   *net.Dialer

	mu       sync.Mutex
	resolved map[netip.AddrPort]Purpose // addresses learned through LookupHost
}

// NewPolicy validates destinations and keeps those the mode permits.
// Destinations for LLM providers are dropped in offline mode; database
// destinations are explicitly configured infrastructure and stay in every
// mode.
func NewPolicy(mode Mode, destinations ...Destination) (*Policy, error) {
	if _, err := ParseMode(string(mode)); err != nil {
		return nil, err
	}
	p := &Policy{
		mode:     mode,
		literals: map[netip.AddrPort]Purpose{},
		sockets:  map[string]Purpose{},
		resolved: map[netip.AddrPort]Purpose{},
		// A private resolver behaves like net.DefaultResolver but lets tests
		// replace the default one with a tripwire for bypass detection.
		resolver: &net.Resolver{},
		dialer:   &net.Dialer{Timeout: dialTimeout},
	}
	for _, d := range destinations {
		if d.Host == "" || strings.ContainsRune(d.Host, 0) {
			return nil, fmt.Errorf("egress destination host must be nonempty and contain no NUL")
		}
		switch d.Purpose {
		case Database, TryIt:
		case LocalServer:
			if addr, err := netip.ParseAddr(d.Host); err != nil || !addr.Unmap().IsLoopback() {
				return nil, fmt.Errorf("egress destination %s: a local-server destination must be a loopback IP literal", d.Host)
			}
		case LLMProvider:
			if mode != LLM {
				continue
			}
		default:
			return nil, fmt.Errorf("egress destination %s: unknown purpose %q", d.Host, d.Purpose)
		}
		if d.Port == 0 {
			return nil, fmt.Errorf("egress destination %s: port must be 1-65535", d.Host)
		}
		if d.Purpose == TryIt && isSocketDir(d.Host) {
			return nil, fmt.Errorf("egress destination %s: a try-it destination is a host, not a socket", d.Host)
		}
		if isSocketDir(d.Host) {
			p.sockets[socketPath(d.Host, d.Port)] = d.Purpose
			continue
		}
		if addr, err := netip.ParseAddr(d.Host); err == nil {
			p.literals[addrKey(addr, d.Port)] = d.Purpose
			continue
		}
		d.Host = normalizeName(d.Host)
		p.names = append(p.names, d)
	}
	return p, nil
}

// isSocketDir mirrors pgx's rule for absolute socket paths, so a Postgres
// host string is classified here exactly as the driver classifies it.
func isSocketDir(host string) bool {
	if strings.HasPrefix(host, "/") {
		return true
	}
	return len(host) >= 3 && host[0] >= 'A' && host[0] <= 'Z' && host[1] == ':' && host[2] == '\\'
}

// socketPath is the socket file pgx dials for a socket directory and port
// (pgconn.NetworkAddress). Only that file is allowed, not the whole directory.
func socketPath(dir string, port uint16) string {
	return filepath.Join(dir, ".s.PGSQL.") + strconv.Itoa(int(port))
}

// addrKey normalises an address for allow-list matching. IPv4-mapped IPv6
// addresses match their IPv4 form. An IPv6 zone is kept, so a configured
// fe80::1%eth0 does not admit fe80::1%eth1; pgx dials the literal exactly as
// LookupHost returned it, zone included.
func addrKey(addr netip.Addr, port uint16) netip.AddrPort {
	return netip.AddrPortFrom(addr.Unmap(), port)
}

// normalizeName compares DNS names case-insensitively and ignores the
// trailing root dot, as DNS itself does.
func normalizeName(host string) string {
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// LookupHost resolves host only if it is an allowed destination name. The
// addresses returned become dialable for the destination's port only.
//
// An IP literal is returned as it is. That is not a network lookup (no
// resolver is called, nothing leaves the process), so it reports no Event and
// is never blocked here; the dial that follows is gated and reported. pgx
// calls LookupFunc for every host, literals included.
//
// A resolver error is returned unwrapped: pgx adds its own context, and the
// net.DNSError already names the host, so the message stays exactly what the
// default resolver produced before this package existed.
func (p *Policy) LookupHost(ctx context.Context, host string) ([]string, error) {
	if _, err := netip.ParseAddr(host); err == nil {
		return []string{host}, nil
	}
	name := normalizeName(host)
	var matches []Destination
	for _, d := range p.names {
		if d.Host == name {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		report(Event{Op: "lookup", Address: host})
		return nil, fmt.Errorf("%w: lookup %s", ErrBlocked, host)
	}
	report(Event{Op: "lookup", Address: host, Purpose: matches[0].Purpose, Allowed: true})
	addrs, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err // unwrapped on purpose, see above
	}
	p.mu.Lock()
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		for _, d := range matches {
			p.resolved[addrKey(ip, d.Port)] = d.Purpose
		}
	}
	p.mu.Unlock()
	return addrs, nil
}

// DialContext connects only to allowed destinations. It has the signature of
// net.Dialer.DialContext so it plugs into pgx, net/http and future clients.
// Names are resolved through LookupHost and each address is tried in order.
func (p *Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "unix":
		if purpose, ok := p.sockets[filepath.Clean(address)]; ok {
			report(Event{Op: "dial", Network: network, Address: address, Purpose: purpose, Allowed: true})
			return p.dialer.DialContext(ctx, network, address)
		}
	case "tcp", "tcp4", "tcp6":
		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			break
		}
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil {
			break
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			key := addrKey(addr, uint16(port))
			purpose, ok := p.literals[key]
			if !ok {
				p.mu.Lock()
				purpose, ok = p.resolved[key]
				p.mu.Unlock()
			}
			if ok {
				report(Event{Op: "dial", Network: network, Address: address, Purpose: purpose, Allowed: true})
				return p.dialer.DialContext(ctx, network, address)
			}
			break
		}
		if !p.hasName(host, uint16(port)) {
			break
		}
		addrs, err := p.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		var errs []error
		for _, a := range addrs {
			conn, err := p.DialContext(ctx, network, net.JoinHostPort(a, portText))
			if err == nil {
				return conn, nil
			}
			errs = append(errs, err)
		}
		if len(errs) == 0 {
			return nil, fmt.Errorf("dialing %s: no addresses", address)
		}
		return nil, errors.Join(errs...)
	}
	report(Event{Op: "dial", Network: network, Address: address})
	return nil, fmt.Errorf("%w: %s %s", ErrBlocked, network, address)
}

func (p *Policy) hasName(host string, port uint16) bool {
	name := normalizeName(host)
	for _, d := range p.names {
		if d.Host == name && d.Port == port {
			return true
		}
	}
	return false
}

// HTTPClient returns a client whose every connection, including redirects
// and TLS connections, is dialed through this policy. It ignores proxy
// environment variables: a proxy would be an unlisted destination, so proxy
// support must add the proxy as a destination instead.
func (p *Policy) HTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext:         p.DialContext,
		Proxy:               nil,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: dialTimeout,
	}}
}
