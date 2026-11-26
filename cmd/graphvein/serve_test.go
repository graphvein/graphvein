package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
)

func TestServeUsageErrors(t *testing.T) {
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "absent", "index.db"))
	root := t.TempDir()
	for _, args := range [][]string{
		{root, "extra"},
		{"--addr", "127.0.0.1:0", root, "extra"},
		{root, "--addr", "localhost:7420"},
		{root, "--addr", "0.0.0.0:7420"},
		{root, "--addr", "[::]:7420"},
		{root, "--addr", "127.0.0.1"},
		{root, "--bogus"},
	} {
		var out, errOut bytes.Buffer
		if code := serveCommand(context.Background(), args, &out, &errOut); code != 2 || out.Len() != 0 {
			t.Errorf("%v: exit %d, stdout %q", args, code, out.String())
		}
		if strings.Contains(strings.Join(args, " "), "7420") && !strings.Contains(errOut.String(), "--addr must be a loopback IP literal such as 127.0.0.1:7420 or [::1]:7420") {
			t.Errorf("%v: stderr %q", args, errOut.String())
		}
	}
	// A missing store fails like query (exit 1) and creates nothing.
	var out, errOut bytes.Buffer
	if code := serveCommand(context.Background(), []string{root, "--addr", "127.0.0.1:0"}, &out, &errOut); code != 1 {
		t.Fatalf("missing store: exit %d, %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Dir(os.Getenv("GRAPHVEIN_SQLITE_PATH"))); !os.IsNotExist(err) {
		t.Fatal("serve created the store directory", err)
	}
}

// lineWriter hands the first stdout line to the test while the server
// keeps running.
type lineWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines chan string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			w.buf.WriteString(line)
			return len(p), nil
		}
		w.lines <- strings.TrimSuffix(line, "\n")
	}
}

// TestServeCommand serves an indexed fixture, reads status over a real
// socket and stops cleanly. With an egress observer and the default
// resolver and HTTP transport tripwires of
// TestOfflineRunMakesNoNetworkAttempts it extends that offline guarantee to
// serve: the only network operation is one allowed loopback listen.
func TestServeCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nrepository: fixture\negress: {mode: offline}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	var out, errOut bytes.Buffer
	if code := run([]string{"index", root}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}

	var mu sync.Mutex
	var events []egress.Event
	stop := egress.Observe(func(e egress.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	defer stop()
	var dnsHits []string
	savedResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dnsHits = append(dnsHits, address)
		mu.Unlock()
		return nil, errors.New("tripwire: default resolver used")
	}}
	t.Cleanup(func() { net.DefaultResolver = savedResolver })
	recorder := &roundTripRecorder{}
	savedTransport := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = savedTransport })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	freshWorkspace(t)
	stdout := &lineWriter{lines: make(chan string, 4)}
	var stderr syncBuffer
	exit := make(chan int, 1)
	go func() { exit <- serveCommand(ctx, []string{root, "--addr", "127.0.0.1:0"}, stdout, &stderr) }()

	var line string
	select {
	case line = <-stdout.lines:
	case code := <-exit:
		t.Fatalf("serve exited %d: %s", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("no startup line")
	}
	base, ok := strings.CutPrefix(line, "Serving fixture at ")
	if !ok {
		t.Fatalf("startup line %q", line)
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Path != "/" || u.Port() == "0" || u.Hostname() != "127.0.0.1" {
		t.Fatalf("URL %q: %v", base, err)
	}

	client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
	resp, err := client.Get(base + "api/v0/status")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal(resp.StatusCode, err, string(data))
	}
	var status struct {
		Repository string `json:"repository"`
		Freshness  struct {
			State string `json:"state"`
		} `json:"freshness"`
	}
	if err := json.Unmarshal(data, &status); err != nil || status.Repository != "fixture" || status.Freshness.State != "fresh" {
		t.Fatalf("status %s: %v", data, err)
	}
	// The workspace list and a request naming the entry by ws= (ADR
	// 0087) add no network use either.
	resp, err = client.Get(base + "api/v0/workspace")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var list struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &list); err != nil || resp.StatusCode != http.StatusOK || len(list.Entries) != 1 {
		t.Fatalf("workspace %d %s: %v", resp.StatusCode, data, err)
	}
	resp, err = client.Get(base + "api/v0/api/endpoints?ws=" + url.QueryEscape(list.Entries[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("endpoints by ws: %d %s", resp.StatusCode, data)
	}

	// A re-index from the UI runs in this process under the index lock,
	// and adds no network use: the runtime egress checks below still see
	// exactly one listen.
	req, err := http.NewRequest(http.MethodPost, base+"api/v0/index/run", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST index/run: %d %s", resp.StatusCode, data)
	}
	var runDoc struct {
		Lock struct {
			State string `json:"state"`
		} `json:"lock"`
		Job struct {
			State   string  `json:"state"`
			EndedAt *string `json:"ended_at"`
		} `json:"job"`
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		resp, err = client.Get(base + "api/v0/index/run")
		if err != nil {
			t.Fatal(err)
		}
		data, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err := json.Unmarshal(data, &runDoc); err != nil {
			t.Fatal(err, string(data))
		}
		// The job records its end just before the run releases the
		// repository lock, so wait for both; checking the lock right
		// after the end raced on loaded machines.
		if runDoc.Job.EndedAt != nil && runDoc.Lock.State != "held_here" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("re-index did not end and release the lock: %s", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runDoc.Job.State != "committed" || runDoc.Lock.State != "free" {
		t.Fatalf("re-index: %s", data)
	}

	cancel()
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	for _, want := range []string{"server started", "server stopped"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %s", want, stderr.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Op != "listen" || !events[0].Allowed || events[0].Address != "127.0.0.1:0" {
		t.Fatalf("egress events %+v, want one allowed listen", events)
	}
	if len(dnsHits) != 0 {
		t.Fatalf("default resolver hits %v", dnsHits)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.hits) != 0 {
		t.Fatalf("default HTTP transport hits %v", recorder.hits)
	}
}

// TestServeListenError checks the message for a port in use: one line that
// names the address once, gives the system's reason and the way out.
func TestServeListenError(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	var out, errOut bytes.Buffer
	if code := run([]string{"index", root, "--repository", "fixture"}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	taken, err := egress.ListenLoopback(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	out.Reset()
	errOut.Reset()
	if code := serveCommand(context.Background(), []string{root, "--repository", "fixture", "--addr", addr}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	msg := errOut.String()
	if !strings.HasPrefix(msg, "cannot listen on "+addr+": bind: ") || strings.Count(msg, addr) != 1 || strings.Count(msg, "\n") != 1 ||
		!strings.HasSuffix(msg, "; if the port is in use, try --addr 127.0.0.1:0\n") {
		t.Fatalf("message %q", msg)
	}
}

// syncBuffer is a bytes.Buffer safe for the server goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestServeInvalidConfig checks that serve starts on an index whose
// graphvein.yaml does not load (ADR 0088): it says so on stderr, serves
// the Settings endpoint's config_error and status, and keeps the identity
// the file sets; a file whose repository cannot be read asks for
// --repository; graphvein index still refuses the file.
func TestServeInvalidConfig(t *testing.T) {
	// A registry of its own, so the startup line counts no entry another
	// test registered (T038 x T039 integration).
	freshWorkspace(t)
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "graphvein.yaml")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("version: 1\nrepository: fixture\negress: {mode: offline}\n")
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	var out, errOut bytes.Buffer
	if code := run([]string{"index", root}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	for _, c := range []struct{ name, text, message string }{
		{"decoder", "version: 1\nrepository: fixture\negress: {mode: offline}\nindex:\n  shape_depth: 99\n", "line 5: index.shape_depth must be an integer from 1 to 10"},
		{"syntax", "version: 1\nrepository: fixture\nindex: [\n", "yaml:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			write(c.text)
			// An index run keeps its strict refusal.
			out.Reset()
			errOut.Reset()
			if code := run([]string{"index", root}, &out, &errOut); code == 0 {
				t.Fatalf("index accepted an invalid graphvein.yaml: %s", out.String())
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stdout := &lineWriter{lines: make(chan string, 4)}
			var stderr syncBuffer
			exit := make(chan int, 1)
			go func() { exit <- serveCommand(ctx, []string{root, "--addr", "127.0.0.1:0"}, stdout, &stderr) }()
			var line string
			select {
			case line = <-stdout.lines:
			case code := <-exit:
				t.Fatalf("serve exited %d: %s", code, stderr.String())
			case <-time.After(10 * time.Second):
				t.Fatal("no startup line")
			}
			base, ok := strings.CutPrefix(line, "Serving fixture at ")
			if !ok {
				t.Fatalf("startup line %q", line)
			}
			if !strings.Contains(stderr.String(), c.message) || !strings.Contains(stderr.String(), "Serving anyway") {
				t.Errorf("stderr %q", stderr.String())
			}
			client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
			get := func(path string) []byte {
				resp, err := client.Get(base + path)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("%s: %d %s", path, resp.StatusCode, data)
				}
				return data
			}
			var cfg struct {
				ConfigError *string `json:"config_error"`
				Repository  string  `json:"repository"`
			}
			if err := json.Unmarshal(get("api/v0/config"), &cfg); err != nil || cfg.ConfigError == nil || !strings.Contains(*cfg.ConfigError, c.message) || strings.Count(*cfg.ConfigError, "line ") > 1 {
				t.Errorf("config: %+v %v", cfg, err)
			}
			var status struct {
				Repository string `json:"repository"`
			}
			if err := json.Unmarshal(get("api/v0/status"), &status); err != nil || status.Repository != "fixture" {
				t.Errorf("status %+v %v", status, err)
			}
			cancel()
			if code := <-exit; code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
		})
	}
	// A repository key that is there but cannot be read proves no
	// identity: serve asks for --repository rather than guess one, and
	// starts with it.
	write("version: 1\nrepository: [fixture\n")
	errOut.Reset()
	if code := serveCommand(context.Background(), []string{root, "--addr", "127.0.0.1:0"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "give it with --repository") {
		t.Fatalf("unknown identity: exit %d, %s", code, errOut.String())
	}
}
