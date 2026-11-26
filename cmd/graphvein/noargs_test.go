package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noArgsFakes replaces every environment check and side effect of the
// no-argument start for one test and restores them afterwards.
type noArgsFakes struct {
	cancel   context.CancelFunc
	opened   []string
	waited   bool
	desktop  bool
	terminal bool
	openErr  error
	graphv   func(ctx context.Context, addr string) bool
	// stopOnReady cancels the run once the banner is printed, as Ctrl+C
	// would, so tests that open no browser still end.
	stopOnReady bool
}

func useNoArgsFakes(t *testing.T, addrs []string, f *noArgsFakes) {
	t.Helper()
	freshWorkspace(t)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	saved := []any{noArgsAddrs, interruptContext, canOpenBrowser, openBrowser, isGraphvein, stdinIsTerminal, waitForEnter}
	t.Cleanup(func() {
		noArgsAddrs = saved[0].([]string)
		interruptContext = saved[1].(func() (context.Context, context.CancelFunc))
		canOpenBrowser = saved[2].(func() bool)
		openBrowser = saved[3].(func(string) error)
		isGraphvein = saved[4].(func(context.Context, string) bool)
		stdinIsTerminal = saved[5].(func() bool)
		waitForEnter = saved[6].(func())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	f.cancel = cancel
	noArgsAddrs = addrs
	interruptContext = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	canOpenBrowser = func() bool {
		if f.stopOnReady {
			cancel()
		}
		return f.desktop
	}
	openBrowser = func(url string) error {
		f.opened = append(f.opened, url)
		cancel()
		return f.openErr
	}
	isGraphvein = func(ctx context.Context, addr string) bool {
		if f.graphv != nil {
			return f.graphv(ctx, addr)
		}
		return false
	}
	stdinIsTerminal = func() bool { return f.terminal }
	waitForEnter = func() { f.waited = true }
}

// freeAddr returns a loopback address that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// busyAddr returns a loopback address held by a listener that is not a
// Graphvein server, until the test ends.
func busyAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().String()
}

func runNoArgs(t *testing.T) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(nil, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNoArgsServesWorkspaceAndOpensBrowser(t *testing.T) {
	addr := freeAddr(t)
	f := &noArgsFakes{desktop: true}
	useNoArgsFakes(t, []string{addr}, f)
	served := make(chan bool, 1)
	openBrowser = func(url string) error {
		f.opened = append(f.opened, url)
		// The browser is opened just before the server starts answering;
		// the URL answers as a Graphvein workspace server once it does.
		go func() {
			served <- probeGraphvein(context.Background(), addr)
			f.cancel()
		}()
		return nil
	}
	code, out, errOut := runNoArgs(t)
	url := "http://" + addr + "/"
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, out, errOut)
	}
	if len(f.opened) != 1 || f.opened[0] != url {
		t.Fatalf("opened %v, want [%s]", f.opened, url)
	}
	if !<-served {
		t.Fatal("no Graphvein server answered at the opened URL")
	}
	for _, want := range []string{
		"Serving the workspace (0 entries) at " + url,
		"Graphvein web UI: " + url,
		"Run `graphvein help` for the commands.",
		"Press Ctrl+C or close this window to stop.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Open "+url) || strings.Contains(out, "Commands:") {
		t.Errorf("stdout = %q", out)
	}
}

func TestNoArgsWithoutDesktopPrintsURL(t *testing.T) {
	addr := freeAddr(t)
	f := &noArgsFakes{desktop: false, stopOnReady: true}
	useNoArgsFakes(t, []string{addr}, f)
	code, out, errOut := runNoArgs(t)
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, out, errOut)
	}
	if len(f.opened) != 0 {
		t.Fatalf("opened %v without a desktop", f.opened)
	}
	if !strings.Contains(out, "Open http://"+addr+"/ in your browser.") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestNoArgsOpenerErrorPrintsURLAndServes(t *testing.T) {
	addr := freeAddr(t)
	f := &noArgsFakes{desktop: true, openErr: errors.New("no handler")}
	useNoArgsFakes(t, []string{addr}, f)
	code, out, errOut := runNoArgs(t)
	if code != 0 {
		t.Fatalf("exit = %d (a browser error must not fail)\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Open http://"+addr+"/ in your browser.") || !strings.Contains(errOut, "could not open the browser") {
		t.Fatalf("stdout = %q\nstderr = %q", out, errOut)
	}
}

func TestNoArgsOpensRunningGraphvein(t *testing.T) {
	freshWorkspace(t)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
	addr := freeAddr(t)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- serveCommand(ctx, []string{"--addr", addr}, io.Discard, io.Discard) }()
	t.Cleanup(func() { stop(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for !probeGraphvein(context.Background(), addr) {
		if time.Now().After(deadline) {
			t.Fatal("first graphvein serve did not answer")
		}
		time.Sleep(20 * time.Millisecond)
	}

	f := &noArgsFakes{desktop: true, graphv: probeGraphvein}
	useNoArgsFakes(t, []string{addr, busyAddr(t)}, f)
	code, out, errOut := runNoArgs(t)
	url := "http://" + addr + "/"
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Graphvein is already running at "+url+"; opened it.") || strings.Contains(out, "Serving") {
		t.Fatalf("stdout = %q", out)
	}
	if len(f.opened) != 1 || f.opened[0] != url {
		t.Fatalf("opened %v, want [%s]", f.opened, url)
	}
}

func TestNoArgsBusyPortFallsBackToNext(t *testing.T) {
	busy, free := busyAddr(t), freeAddr(t)
	var probed []string
	f := &noArgsFakes{desktop: true, graphv: func(_ context.Context, addr string) bool {
		probed = append(probed, addr)
		return false
	}}
	useNoArgsFakes(t, []string{busy, free}, f)
	code, out, errOut := runNoArgs(t)
	url := "http://" + free + "/"
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, out, errOut)
	}
	if len(probed) != 1 || probed[0] != busy {
		t.Fatalf("probed %v, want [%s]", probed, busy)
	}
	if len(f.opened) != 1 || f.opened[0] != url || !strings.Contains(out, "Graphvein web UI: "+url) {
		t.Fatalf("opened %v\nstdout = %q", f.opened, out)
	}
	if strings.Contains(errOut, "if the port is in use") {
		t.Fatalf("the serve message leaked: %q", errOut)
	}
}

func TestNoArgsAllPortsBusy(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		f := &noArgsFakes{desktop: true, terminal: terminal}
		addrs := []string{busyAddr(t), busyAddr(t), busyAddr(t)}
		useNoArgsFakes(t, addrs, f)
		code, out, errOut := runNoArgs(t)
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s\n%s", code, out, errOut)
		}
		if len(f.opened) != 0 {
			t.Fatalf("opened %v", f.opened)
		}
		for _, want := range []string{"every address from " + addrs[0] + " to " + addrs[2], "graphvein serve --addr 127.0.0.1:PORT"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("stderr lacks %q:\n%s", want, errOut)
			}
		}
		if f.waited != terminal || strings.Contains(errOut, "Press Enter") != terminal {
			t.Fatalf("terminal=%v: waited=%v\n%s", terminal, f.waited, errOut)
		}
	}
}

func TestProbeGraphvein(t *testing.T) {
	reply := func(status int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v0/workspace" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return srv.Listener.Addr().String()
	}
	for _, tc := range []struct {
		name string
		addr string
		want bool
	}{
		{"workspace reply", reply(200, `{"api_version":0,"registry":{"path":"x","error":null},"entries":[]}`), true},
		{"other JSON", reply(200, `{"status":"ok"}`), false},
		{"not JSON", reply(200, `<html>`), false},
		{"error status", reply(500, `{"api_version":0,"registry":{},"entries":[]}`), false},
		{"nothing listening", freeAddr(t), false},
		{"not an IP literal", "localhost:7420", false},
	} {
		if got := probeGraphvein(context.Background(), tc.addr); got != tc.want {
			t.Errorf("%s: probeGraphvein(%s) = %v, want %v", tc.name, tc.addr, got, tc.want)
		}
	}
}

func TestNoArgsDefaultAddresses(t *testing.T) {
	if len(noArgsAddrs) != 11 || noArgsAddrs[0] != defaultServeAddr || noArgsAddrs[10] != "127.0.0.1:7430" {
		t.Fatalf("noArgsAddrs = %v", noArgsAddrs)
	}
}
