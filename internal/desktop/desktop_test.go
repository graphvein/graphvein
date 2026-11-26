package desktop

import (
	"path/filepath"
	"reflect"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestCanOpenBrowser(t *testing.T) {
	cases := []struct {
		goos string
		vars map[string]string
		want bool
	}{
		{"windows", nil, true},
		{"darwin", nil, true},
		{"linux", nil, false},
		{"linux", map[string]string{"DISPLAY": ":0"}, true},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"freebsd", map[string]string{"DISPLAY": ":0"}, true},
		{"linux", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 22"}, false},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "w", "SSH_TTY": "/dev/pts/1"}, false},
		{"darwin", map[string]string{"SSH_CONNECTION": "x"}, false},
		{"windows", map[string]string{"SSH_TTY": "x"}, false},
	}
	for _, c := range cases {
		if got := CanOpenBrowser(c.goos, env(c.vars)); got != c.want {
			t.Errorf("CanOpenBrowser(%s, %v) = %v, want %v", c.goos, c.vars, got, c.want)
		}
	}
}

func TestBrowserCommand(t *testing.T) {
	const url = "http://127.0.0.1:7420/"
	cases := []struct {
		goos string
		vars map[string]string
		name string
		args []string
	}{
		{"windows", map[string]string{"SystemRoot": `C:\Windows`}, filepath.Join(`C:\Windows`, "System32", "rundll32.exe"), []string{"url.dll,FileProtocolHandler", url}},
		{"windows", nil, "rundll32.exe", []string{"url.dll,FileProtocolHandler", url}},
		{"darwin", nil, "open", []string{url}},
		{"linux", nil, "xdg-open", []string{url}},
	}
	for _, c := range cases {
		name, args := BrowserCommand(c.goos, url, env(c.vars))
		if name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("BrowserCommand(%s) = %s %q, want %s %q", c.goos, name, args, c.name, c.args)
		}
	}
}

func TestOpenBrowserRejectsNonHTTP(t *testing.T) {
	for _, u := range []string{"", "file:///etc/passwd", "-h", "javascript:alert(1)"} {
		if err := OpenBrowser(u); err == nil {
			t.Errorf("OpenBrowser(%q) = nil, want an error", u)
		}
	}
}
