// Package desktop holds the little graphvein does for a user who runs it
// with no arguments: deciding whether a desktop is there to show a
// browser, and opening the web UI in the default browser.
//
// Opening the browser starts a process (the system URL handler: rundll32
// on Windows, open on macOS, xdg-open elsewhere); it opens no network
// connection, so this package stays outside internal/egress. The browser
// then connects to graphvein's loopback server like any other client.
package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CanOpenBrowser reports whether a browser opened now would appear in
// front of the user. It is false in an SSH session (SSH_CONNECTION or
// SSH_TTY set) on every platform, because the browser would open on the
// remote machine's desktop or not at all. Windows and macOS otherwise
// always have a desktop; other systems need DISPLAY or WAYLAND_DISPLAY.
// goos and getenv are parameters so the rule is tested on any platform.
func CanOpenBrowser(goos string, getenv func(string) string) bool {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return false
	}
	switch goos {
	case "windows", "darwin":
		return true
	default:
		return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
	}
}

// BrowserCommand returns the program and arguments that open url with the
// default browser on goos. The URL is always one argument of its own and
// never passes through a shell, so no quoting is involved. On Windows the
// handler is rundll32 url.dll,FileProtocolHandler, the one Explorer uses
// for links, taken from %SystemRoot%\System32 when getenv knows it.
func BrowserCommand(goos, url string, getenv func(string) string) (string, []string) {
	switch goos {
	case "windows":
		rundll := "rundll32.exe"
		if root := getenv("SystemRoot"); root != "" {
			rundll = filepath.Join(root, "System32", "rundll32.exe")
		}
		return rundll, []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

// OpenBrowser opens url with the default browser and returns without
// waiting for it. Only http and https URLs are accepted: graphvein passes
// its own loopback address and nothing else. Callers treat an error as
// "print the URL instead"; a missing browser never stops graphvein.
func OpenBrowser(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("opening %q: not an http URL", url)
	}
	name, args := BrowserCommand(runtime.GOOS, url, os.Getenv)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %s in the browser: %w", url, err)
	}
	// Reap the handler when it exits; xdg-open and open return quickly.
	go func() { _ = cmd.Wait() }()
	return nil
}
