package egress

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestTryItPurposeEveryMode checks that a try-it destination is configured
// infrastructure: dialable in offline and llm mode alike (row TI-6), and
// that only its own host and port are.
func TestTryItPurposeEveryMode(t *testing.T) {
	_, port := listen(t)
	_, other := listen(t)
	d := Destination{Host: "127.0.0.1", Port: port, Purpose: TryIt}
	for _, mode := range []Mode{Offline, LLM} {
		p := mustPolicy(t, mode, d)
		conn, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
		if err != nil {
			t.Fatalf("%s: try-it destination blocked: %v", mode, err)
		}
		conn.Close()
		if _, err := p.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(other)))); !errors.Is(err, ErrBlocked) {
			t.Fatalf("%s: another port of the same host allowed: %v", mode, err)
		}
	}
	if _, err := NewPolicy(Offline, Destination{Host: "/tmp", Port: 1, Purpose: TryIt}); err == nil {
		t.Fatal("a socket directory accepted as a try-it destination")
	}
}

// TestTryItClient checks the try-it client: the configured origin answers,
// a redirect is returned instead of followed (also to the same origin),
// another origin is blocked, proxy variables are ignored, every dial is
// reported with purpose try-it, and the timeout bounds a slow server.
func TestTryItClient(t *testing.T) {
	var blockedHits atomic.Int32
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockedHits.Add(1)
	}))
	defer blocked.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, blocked.URL+"/", http.StatusFound)
		case "/here":
			http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
		case "/slow":
			time.Sleep(2 * time.Second)
		default:
			io.WriteString(w, "ok")
		}
	}))
	defer allowed.Close()
	t.Setenv("HTTP_PROXY", blocked.URL)
	t.Setenv("HTTPS_PROXY", blocked.URL)
	t.Setenv("NO_PROXY", "")
	events := record(t)
	addr := allowed.Listener.Addr().(*net.TCPAddr)
	client, err := mustPolicy(t, Offline, Destination{Host: "127.0.0.1", Port: uint16(addr.Port), Purpose: TryIt}).TryItClient(TryItOptions{Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	get := func(url string) (*http.Response, error) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
		}
		return resp, err
	}
	if resp, err := get(allowed.URL + "/"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("allowed: %v %v", resp, err)
	}
	for _, path := range []string{"/away", "/here"} {
		resp, err := get(allowed.URL + path)
		if err != nil || resp.StatusCode/100 != 3 || resp.Header.Get("Location") == "" {
			t.Fatalf("%s: redirect followed or failed: %v %v", path, resp, err)
		}
	}
	if _, err := get(blocked.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("unlisted origin: %v", err)
	}
	if _, err := get(allowed.URL + "/slow"); err == nil {
		t.Fatal("timeout not applied")
	}
	if blockedHits.Load() != 0 {
		t.Fatal("blocked server was reached")
	}
	if pr := client.Transport.(*http.Transport).Protocols; pr == nil || pr.UnencryptedHTTP2() || !pr.HTTP1() {
		t.Fatalf("protocols %v: want HTTP/1.1 and TLS HTTP/2 only, never h2c", pr)
	}
	dials := 0
	for _, e := range events() {
		if e.Op == "dial" && e.Allowed {
			dials++
			if e.Purpose != TryIt {
				t.Fatalf("dial reported with purpose %q", e.Purpose)
			}
		}
	}
	if dials == 0 {
		t.Fatal("no dial reported")
	}
}

// TestTryItPurposeUsers checks that only the server's try-it proxy builds
// try-it destinations or clients: no other package can make a configured
// environment dialable.
func TestTryItPurposeUsers(t *testing.T) {
	root := moduleRoot(t)
	var users []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
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
		if strings.Contains(string(src), "egress.TryIt") {
			rel, _ := filepath.Rel(root, path)
			users = append(users, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(users, ",") != "internal/server/tryit.go" {
		t.Fatalf("try-it destinations built in %v; only internal/server/tryit.go may", users)
	}
}
