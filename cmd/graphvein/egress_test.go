package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// roundTripRecorder replaces http.DefaultTransport to catch HTTP that
// bypasses egress.
type roundTripRecorder struct {
	mu   sync.Mutex
	hits []string
}

func (r *roundTripRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hits = append(r.hits, req.URL.Host)
	r.mu.Unlock()
	return nil, errors.New("tripwire: default HTTP transport used")
}

// TestOfflineRunMakesNoNetworkAttempts runs index, status and query in
// offline mode and asserts that nothing went through egress and nothing used
// the default resolver or HTTP transport. The static boundary test proves
// first-party code has no other dial path; this proves the composed run
// uses none.
func TestOfflineRunMakesNoNetworkAttempts(t *testing.T) {
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

	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nrepository: fixture\negress: {mode: offline}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	for _, args := range [][]string{
		{"index", root},
		{"status", root},
		{"query", "Help", root, "--json"},
		{"index", root},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatal(args, code, stderr.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 || len(dnsHits) != 0 {
		t.Fatalf("egress events %+v, default resolver hits %v", events, dnsHits)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.hits) != 0 {
		t.Fatalf("default HTTP transport hits %v", recorder.hits)
	}
}

// TestMCPSessionMakesNoNetworkAttempts runs a whole graphvein mcp session
// (initialize, tools/list and a call of every tool) over stdin and stdout
// with the same tripwires as TestOfflineRunMakesNoNetworkAttempts: nothing
// goes through egress (no dial, no lookup and, unlike serve, no listen at
// all) and nothing uses the default resolver or HTTP transport (ADR 0082).
func TestMCPSessionMakesNoNetworkAttempts(t *testing.T) {
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

	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"index", root, "--repository", "fixture"}, &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	saved := stdinOf
	stdinOf = func() io.Reader { return inR }
	t.Cleanup(func() { stdinOf = saved })
	done := make(chan int, 1)
	var errOut bytes.Buffer
	go func() {
		code := run([]string{"mcp", root, "--repository", "fixture"}, outW, &errOut)
		outW.Close()
		done <- code
	}()
	lines := bufio.NewScanner(outR)
	lines.Buffer(make([]byte, 1<<20), 16<<20)
	id := 0
	call := func(method string, params any) map[string]any {
		t.Helper()
		id++
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if _, err := inW.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
		for lines.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(lines.Bytes(), &msg); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(msg["id"]) == fmt.Sprint(id) {
				if msg["error"] != nil {
					t.Fatalf("%s: %v", method, msg["error"])
				}
				res, _ := msg["result"].(map[string]any)
				return res
			}
		}
		t.Fatalf("%s: no answer: %v", method, lines.Err())
		return nil
	}
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "egress", "version": "1"}})
	inW.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	tools, _ := call("tools/list", map[string]any{})["tools"].([]any)
	arguments := map[string]map[string]any{
		"find": {"term": "Help"}, "endpoint": {"target": "Help"}, "source": {"target": "Help"},
		"callers": {"target": "Help"}, "impact": {"target": "Help"},
		"chain": {"target": "Help"}, "table": {"target": "Help"}, "change_impact": {"group": "code"},
	}
	called := 0
	for _, tool := range tools {
		name, _ := tool.(map[string]any)["name"].(string)
		args := arguments[name]
		if args == nil {
			args = map[string]any{}
		}
		res := call("tools/call", map[string]any{"name": name, "arguments": args})
		if res["structuredContent"] == nil {
			t.Fatalf("%s: no structured content: %v", name, res)
		}
		called++
	}
	if called < 13 {
		t.Fatalf("called %d tools, want every MCP tool", called)
	}
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("mcp exited %d: %s", code, errOut.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 || len(dnsHits) != 0 {
		t.Fatalf("egress events %+v, default resolver hits %v", events, dnsHits)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.hits) != 0 {
		t.Fatalf("default HTTP transport hits %v", recorder.hits)
	}
}
