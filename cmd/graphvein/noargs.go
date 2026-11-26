package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/graphvein/graphvein/internal/desktop"
	"github.com/graphvein/graphvein/internal/egress"
)

// noArgsPorts is how many loopback ports graphvein with no arguments tries,
// starting at the default 7420: 7420 to 7430.
const noArgsPorts = 11

// The no-argument start is the same on every platform and however
// graphvein was started (a terminal, a double-click on graphvein.exe, a
// desktop launcher): it serves the workspace as graphvein serve does with
// no path and opens the browser at its URL. Each environment check and
// side effect is a variable so tests can run the path without a desktop,
// a browser, a terminal or the fixed ports.
var (
	// noArgsAddrs are the addresses graphvein with no arguments tries in
	// order: the graphvein serve default first, then the next ports.
	noArgsAddrs = loopbackAddrs("127.0.0.1", 7420, noArgsPorts)
	// interruptContext ends when the user presses Ctrl+C (os.Interrupt on
	// every OS) or closes the console window.
	interruptContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	}
	// canOpenBrowser reports whether a desktop is there to show a browser
	// (not in an SSH session; on Linux, DISPLAY or WAYLAND_DISPLAY set).
	canOpenBrowser = func() bool { return desktop.CanOpenBrowser(runtime.GOOS, os.Getenv) }
	// openBrowser opens a URL with the system URL handler.
	openBrowser = desktop.OpenBrowser
	// isGraphvein asks whether a Graphvein server answers at addr.
	isGraphvein = probeGraphvein
	// stdinIsTerminal is true when someone can press Enter: a console
	// window (a double-clicked graphvein.exe included) or a terminal.
	stdinIsTerminal = func() bool {
		info, err := os.Stdin.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
	// waitForEnter blocks until the user presses Enter (or stdin ends).
	waitForEnter = func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n') }
)

// loopbackAddrs lists n consecutive host:port addresses from first.
func loopbackAddrs(host string, first, n int) []string {
	addrs := make([]string, n)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("%s:%d", host, first+i)
	}
	return addrs
}

// noArgsCommand runs graphvein with no arguments: the workspace web UI on
// the first free loopback address of noArgsAddrs, a short banner and, when
// a desktop is there, the default browser at the URL. When the first
// address already answers as a Graphvein server, that server is opened
// instead of starting a second one. A browser that cannot be opened is
// never an error: the URL is printed and the server keeps running.
func noArgsCommand(ctx context.Context, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	registry := openRegistry(logger)
	show := func(url string) {
		if !canOpenBrowser() {
			fmt.Fprintf(stdout, "Open %s in your browser.\n", url)
			return
		}
		if err := openBrowser(url); err != nil {
			logger.Warn("could not open the browser", "error", err)
			fmt.Fprintf(stdout, "Open %s in your browser.\n", url)
		}
	}
	for i, addr := range noArgsAddrs {
		var listenErr error
		hooks := &serveHooks{
			ready: func(url string) {
				fmt.Fprintf(stdout, "\nGraphvein web UI: %s\n", url)
				fmt.Fprintln(stdout, "Add a repository in the browser. Run `graphvein help` for the commands.")
				fmt.Fprintln(stdout, "Press Ctrl+C or close this window to stop.")
				show(url)
			},
			listenFailed: func(err error) { listenErr = err },
		}
		code := serveWorkspace(ctx, addr, registry, nil, stdout, stderr, logger, hooks)
		if listenErr == nil {
			return code
		}
		if i == 0 && isGraphvein(ctx, addr) {
			url := "http://" + addr + "/"
			fmt.Fprintf(stdout, "Graphvein is already running at %s; opened it.\n", url)
			show(url)
			return 0
		}
		if ctx.Err() != nil {
			return 1
		}
	}
	first, last := noArgsAddrs[0], noArgsAddrs[len(noArgsAddrs)-1]
	fmt.Fprintf(stderr, "Graphvein could not start its web UI: every address from %s to %s is in use or unavailable.\n", first, last)
	fmt.Fprintln(stderr, "Choose a free port with `graphvein serve --addr 127.0.0.1:PORT`, or let the system pick one with `graphvein serve --addr 127.0.0.1:0`.")
	if stdinIsTerminal() {
		// A double-clicked graphvein.exe's window closes when graphvein
		// exits: keep it open until the message has been read.
		fmt.Fprintln(stderr, "Press Enter to close.")
		waitForEnter()
	}
	return 1
}

// probeTimeout bounds the whole probe; a Graphvein server answers its
// workspace list at once.
const probeTimeout = 2 * time.Second

// probeGraphvein reports whether a Graphvein server answers at addr, a
// loopback IP literal and port. It asks GET /api/v0/workspace, which every
// graphvein serve answers, through an egress policy holding only addr
// (egress.LocalServer), and recognises the reply by its api_version,
// registry and entries fields. Any error, timeout or other reply is false.
func probeGraphvein(ctx context.Context, addr string) bool {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return false
	}
	policy, err := egress.NewPolicy(egress.Offline, egress.Destination{Host: ap.Addr().String(), Port: ap.Port(), Purpose: egress.LocalServer})
	if err != nil {
		return false
	}
	client := policy.HTTPClient()
	client.Timeout = probeTimeout
	done := make(chan bool, 1)
	go func() {
		resp, err := client.Get("http://" + ap.String() + "/api/v0/workspace")
		if err != nil {
			done <- false
			return
		}
		defer resp.Body.Close()
		var reply struct {
			APIVersion *int             `json:"api_version"`
			Registry   *json.RawMessage `json:"registry"`
			Entries    *json.RawMessage `json:"entries"`
		}
		ok := resp.StatusCode == 200 &&
			json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&reply) == nil &&
			reply.APIVersion != nil && reply.Registry != nil && reply.Entries != nil
		done <- ok
	}()
	select {
	case ok := <-done:
		return ok
	case <-ctx.Done():
		return false
	}
}
