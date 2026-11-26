package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/graphvein/graphvein/internal/config"
)

// record collects events until the test ends.
func record(t *testing.T) func() []Event {
	t.Helper()
	var mu sync.Mutex
	var events []Event
	stop := Observe(func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	t.Cleanup(stop)
	return func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), events...)
	}
}

// tripwire is a Go resolver whose DNS traffic is counted and refused.
func tripwire(count *atomic.Int32) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		count.Add(1)
		return nil, errors.New("tripwire: DNS traffic")
	}}
}

// listen starts a loopback server that accepts and closes connections,
// counting them.
func listen(t *testing.T) (*atomic.Int32, uint16) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			c.Close()
		}
	}()
	return &accepted, uint16(l.Addr().(*net.TCPAddr).Port)
}

func mustPolicy(t *testing.T, mode Mode, d ...Destination) *Policy {
	t.Helper()
	p, err := NewPolicy(mode, d...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModeNamesMatchConfig(t *testing.T) {
	for _, name := range []string{"offline", "llm", "Offline", "LLM", "", "online", "none", " offline"} {
		_, modeErr := ParseMode(name)
		root := t.TempDir()
		body := fmt.Sprintf("version: 1\negress: {mode: %q}\n", name)
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		c, configErr := config.Load(root)
		if (modeErr == nil) != (configErr == nil) || (configErr == nil && c.Egress.Mode != name) {
			t.Fatalf("%q: ParseMode %v, config %v", name, modeErr, configErr)
		}
	}
	if _, err := NewPolicy("none"); err == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestNewPolicyValidation(t *testing.T) {
	for _, d := range []Destination{
		{Host: "", Port: 1, Purpose: Database},
		{Host: "a\x00b", Port: 1, Purpose: Database},
		{Host: "db.example", Port: 0, Purpose: Database},
		{Host: "db.example", Port: 1, Purpose: "other"},
		// A local server is a loopback IP literal, never a name or a
		// routable address.
		{Host: "localhost", Port: 7420, Purpose: LocalServer},
		{Host: "10.0.0.1", Port: 7420, Purpose: LocalServer},
		{Host: "/var/run/postgresql", Port: 7420, Purpose: LocalServer},
	} {
		if _, err := NewPolicy(Offline, d); err == nil {
			t.Fatalf("accepted %+v", d)
		}
	}
	// A socket directory needs the port too: it names the socket file.
	if _, err := NewPolicy(Offline, Destination{Host: "/var/run/postgresql", Purpose: Database}); err == nil {
		t.Fatal("accepted socket directory without port")
	}
	mustPolicy(t, Offline, Destination{Host: "/var/run/postgresql", Port: 5432, Purpose: Database})
	mustPolicy(t, Offline, Destination{Host: "127.0.0.1", Port: 7420, Purpose: LocalServer})
	mustPolicy(t, Offline, Destination{Host: "::1", Port: 7420, Purpose: LocalServer})
}

func TestLookupHost(t *testing.T) {
	var dns atomic.Int32
	p := mustPolicy(t, Offline, Destination{Host: "DB.Example.", Port: 5432, Purpose: Database})
	p.resolver = tripwire(&dns)

	events := record(t)
	addrs, err := p.LookupHost(context.Background(), "192.0.2.7")
	if err != nil || len(addrs) != 1 || addrs[0] != "192.0.2.7" || dns.Load() != 0 {
		t.Fatal("literal", addrs, err, dns.Load())
	}
	// Returning a literal is not a network lookup: no event, so an audit
	// never shows an unlisted literal as a "blocked" lookup that succeeded.
	if got := events(); len(got) != 0 {
		t.Fatalf("literal lookup reported %+v", got)
	}
	if _, err := p.LookupHost(context.Background(), "other.example"); !errors.Is(err, ErrBlocked) || dns.Load() != 0 {
		t.Fatal("unlisted name", err, dns.Load())
	}
	// A listed name (case and trailing dot ignored) reaches the policy's own
	// resolver, here the tripwire, and never the default one.
	var defaults atomic.Int32
	saved := net.DefaultResolver
	net.DefaultResolver = tripwire(&defaults)
	t.Cleanup(func() { net.DefaultResolver = saved })
	_, err = p.LookupHost(context.Background(), "db.example")
	if err == nil || errors.Is(err, ErrBlocked) || dns.Load() == 0 || defaults.Load() != 0 {
		t.Fatal("listed name", err, dns.Load(), defaults.Load())
	}
	// The resolver error is returned as is, keeping the pre-egress text.
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || err.Error() != dnsErr.Error() {
		t.Fatalf("resolver error wrapped: %v", err)
	}
}

// TestPostgresResolverErrorText pins that an unresolvable database host
// reports exactly what the pre-egress setup (net.Dialer plus the resolver's
// LookupHost) reported. A stub resolver refuses DNS traffic, so the test
// sends no packets.
func TestPostgresResolverErrorText(t *testing.T) {
	var dns atomic.Int32
	stub := tripwire(&dns)
	url := "postgres://u:secret@db.example.test:5432/db?sslmode=disable&connect_timeout=2"

	before, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	before.DialFunc = (&net.Dialer{Timeout: dialTimeout}).DialContext
	before.LookupFunc = stub.LookupHost
	_, want := pgx.ConnectConfig(context.Background(), before)

	after, p, err := postgresConfig(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.resolver = stub
	_, got := pgx.ConnectConfig(context.Background(), after)

	if want == nil || got == nil || got.Error() != want.Error() {
		t.Fatalf("error text changed:\n got %v\nwant %v", got, want)
	}
	if !strings.Contains(got.Error(), "hostname resolving error: lookup db.example.test") {
		t.Fatalf("unexpected text: %v", got)
	}
	if dns.Load() == 0 {
		t.Fatal("stub resolver not used")
	}
}

func TestZoneMatchedExactly(t *testing.T) {
	p := mustPolicy(t, Offline, Destination{Host: "fe80::1%eth0", Port: 5432, Purpose: Database})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, address := range []string{"[fe80::1%eth1]:5432", "[fe80::1]:5432"} {
		if _, err := p.DialContext(ctx, "tcp", address); !errors.Is(err, ErrBlocked) {
			t.Fatal(address, err)
		}
	}
	// The configured zone itself is allowed (the dial may still fail where
	// eth0 does not exist).
	if conn, err := p.DialContext(ctx, "tcp", "[fe80::1%eth0]:5432"); errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	} else if err == nil {
		conn.Close()
	}
}

func TestObserverMayStopItself(t *testing.T) {
	var calls atomic.Int32
	var stop func()
	stop = Observe(func(Event) {
		calls.Add(1)
		stop()
	})
	t.Cleanup(stop)
	done := make(chan struct{})
	go func() {
		defer close(done)
		p := mustPolicy(t, Offline)
		p.DialContext(context.Background(), "udp", "127.0.0.1:1")
		p.DialContext(context.Background(), "udp", "127.0.0.1:1")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("observer stopping itself deadlocked")
	}
	if calls.Load() != 1 {
		t.Fatalf("observer called %d times after stopping", calls.Load())
	}
}

func TestLookupRemembersPortPerDestination(t *testing.T) {
	_, port := listen(t)
	p := mustPolicy(t, Offline, Destination{Host: "db.example", Port: port, Purpose: Database})
	// Simulate a resolver answer without DNS traffic.
	p.resolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no DNS in tests")
	}}
	p.resolved[netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)] = Database
	conn, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if _, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)+1))); !errors.Is(err, ErrBlocked) {
		t.Fatal("resolved address on another port", err)
	}
	if _, err := p.DialContext(context.Background(), "tcp", "db.example:1"); !errors.Is(err, ErrBlocked) {
		t.Fatal("listed name on another port", err)
	}
}

func TestDialContext(t *testing.T) {
	_, port := listen(t)
	events := record(t)
	p := mustPolicy(t, Offline, Destination{Host: "127.0.0.1", Port: port, Purpose: Database})
	ctx := context.Background()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	conn, err := p.DialContext(ctx, "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	// The mapped form names the same address; it must not be blocked (it may
	// still fail where IPv6 is unavailable).
	if conn, err := p.DialContext(ctx, "tcp", net.JoinHostPort("::ffff:127.0.0.1", strconv.Itoa(int(port)))); errors.Is(err, ErrBlocked) {
		t.Fatal("mapped address blocked", err)
	} else if err == nil {
		conn.Close()
	}
	_, otherPort := listen(t)
	blocked := [][2]string{
		{"tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(otherPort)))},
		{"tcp", net.JoinHostPort("127.0.0.2", strconv.Itoa(int(port)))},
		{"tcp", "localhost:" + strconv.Itoa(int(port))},
		{"udp", address},
		{"ip", "127.0.0.1"},
		{"unix", filepath.Join(t.TempDir(), ".s.PGSQL.5432")},
		{"tcp", "not an address"},
	}
	for _, b := range blocked {
		if _, err := p.DialContext(ctx, b[0], b[1]); !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), b[1]) {
			t.Fatal(b, err)
		}
	}
	got := events()
	if len(got) < 1+len(blocked) || !got[0].Allowed || got[0].Purpose != Database || got[0].Address != address {
		t.Fatalf("events %+v", got)
	}
	for _, e := range got[len(got)-len(blocked):] {
		if e.Allowed || e.Op != "dial" {
			t.Fatalf("blocked event %+v", e)
		}
	}
}

func TestUnixSocketDirectory(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, ".s.PGSQL.5432")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	defer l.Close()
	go func() {
		if c, err := l.Accept(); err == nil {
			c.Close()
		}
	}()
	p := mustPolicy(t, Offline, Destination{Host: dir + string(filepath.Separator), Port: 5432, Purpose: Database})
	conn, err := p.DialContext(context.Background(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	// Only the socket file pgx dials is allowed: not another directory, not
	// another file or port in the same directory.
	for _, other := range []string{
		filepath.Join(t.TempDir(), ".s.PGSQL.5432"),
		filepath.Join(dir, ".s.PGSQL.5433"),
		filepath.Join(dir, "other.sock"),
	} {
		if _, err := p.DialContext(context.Background(), "unix", other); !errors.Is(err, ErrBlocked) {
			t.Fatal(other, err)
		}
	}
}

func TestModeFiltersLLMDestinations(t *testing.T) {
	_, port := listen(t)
	d := Destination{Host: "127.0.0.1", Port: port, Purpose: LLMProvider}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	if _, err := mustPolicy(t, Offline, d).DialContext(context.Background(), "tcp", address); !errors.Is(err, ErrBlocked) {
		t.Fatal("offline allowed llm destination", err)
	}
	conn, err := mustPolicy(t, LLM, d).DialContext(context.Background(), "tcp", address)
	if err != nil {
		t.Fatal("llm mode blocked llm destination", err)
	}
	conn.Close()
}

func TestHTTPClient(t *testing.T) {
	var blockedHits atomic.Int32
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockedHits.Add(1)
		io.WriteString(w, "blocked")
	}))
	defer blocked.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, blocked.URL+"/", http.StatusFound)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer allowed.Close()
	t.Setenv("HTTP_PROXY", blocked.URL)
	t.Setenv("HTTPS_PROXY", blocked.URL)
	t.Setenv("NO_PROXY", "")
	addr := allowed.Listener.Addr().(*net.TCPAddr)
	client := mustPolicy(t, Offline, Destination{Host: "127.0.0.1", Port: uint16(addr.Port), Purpose: Database}).HTTPClient()
	client.Timeout = 10 * time.Second
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("client honours proxy settings")
	}
	resp, err := client.Get(allowed.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" {
		t.Fatal(string(body))
	}
	if _, err := client.Get(blocked.URL); !errors.Is(err, ErrBlocked) {
		t.Fatal("unlisted server", err)
	}
	if _, err := client.Get(allowed.URL + "/redirect"); !errors.Is(err, ErrBlocked) {
		t.Fatal("redirect to unlisted server", err)
	}
	if blockedHits.Load() != 0 {
		t.Fatal("blocked server was reached")
	}
}

func TestListenLoopback(t *testing.T) {
	ctx := context.Background()
	l, err := ListenLoopback(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if l, err := ListenLoopback(ctx, "[::1]:0"); err == nil {
		l.Close()
	} else if errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	} else {
		t.Log("IPv6 loopback unavailable:", err)
	}
	for _, address := range []string{"0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0", ":0", "127.0.0.1", "127.0.0.1:http"} {
		if l, err := ListenLoopback(ctx, address); !errors.Is(err, ErrBlocked) {
			if l != nil {
				l.Close()
			}
			t.Fatal(address, err)
		}
	}
	for _, address := range []string{"localhost:0", "192.0.2.1:0"} {
		if _, err := ListenLoopback(ctx, address); err == nil || !strings.Contains(err.Error(), "use 127.0.0.1 or [::1]") {
			t.Fatal(address, err)
		}
	}
}

func TestOpenPostgresDialsOnlyConfiguredHost(t *testing.T) {
	accepted, port := listen(t)
	events := record(t)
	db, err := OpenPostgres(fmt.Sprintf("postgres://u:p@127.0.0.1:%d/db?connect_timeout=2&sslmode=disable", port), map[string]string{"search_path": "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err == nil {
		t.Fatal("ping succeeded against a fake server")
	}
	if accepted.Load() == 0 {
		t.Fatal("fake server saw no connection")
	}
	want := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	got := events()
	if len(got) == 0 {
		t.Fatal("no events")
	}
	for _, e := range got {
		if !e.Allowed || e.Purpose != Database || e.Op != "dial" || e.Address != want {
			t.Fatalf("event %+v", e)
		}
	}
}

func TestOpenPostgresDestinations(t *testing.T) {
	// Building the handle connects nothing; the policy is inspected through
	// a dial of each host.
	config := "postgres://u:secret@127.0.0.1:1,127.0.0.2:2/db?sslmode=disable"
	db, err := OpenPostgres(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, p, err := parsePostgres(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, address := range []string{"127.0.0.1:1", "127.0.0.2:2"} {
		if _, err := p.DialContext(ctx, "tcp", address); errors.Is(err, ErrBlocked) {
			t.Fatal(address, err)
		}
	}
	for _, address := range []string{"127.0.0.3:3", "127.0.0.1:2"} {
		if _, err := p.DialContext(ctx, "tcp", address); !errors.Is(err, ErrBlocked) {
			t.Fatal(address, err)
		}
	}
	for _, bad := range []string{"postgres://u:secret@host:notaport/db", "host=a port=99999999 password=secret"} {
		_, err := OpenPostgres(bad, nil)
		if err == nil || err.Error() != "invalid Postgres configuration" || strings.Contains(err.Error(), "secret") {
			t.Fatal(bad, err)
		}
	}
}
