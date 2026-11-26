package server

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/graphvein/graphvein/internal/server/ui"
)

// maxEmbeddedBytes bounds the UI shipped inside the binary. T016f2 raised
// it from 2 MiB to 3 MiB for the Flow tab: the unmodified ELK worker
// (1,595,334 bytes), the lazy Flow chunk and its CSS, and the licence
// notices bring the tree to about 2.5 MiB (ADR 0046). 3 MiB is what that
// needs with room for small changes, not the 4 MiB the design allowed.
// T026 AG-C1 raised it to 3.5 MiB for the Architecture section: its
// lazy map chunk, inspector, Links tab and CSS add about 97 KB, which put
// the tree at 3,191,743 bytes; the half MiB of room covers the chain view
// (AG-C2) still to come.
// T022 B11 raised it to 3.5 MiB, the limit the architecture stack (T026
// AG-C1) records for its own lazily loaded section, for the same reason:
// the project UI (T022 B9) and the manifest editor (B10) are lazily loaded
// features, and with them the tree was 3,145,647 bytes, 81 bytes under
// 3 MiB, leaving no room for the next change.
// The 2026-11-24 integration raised it to 4 MiB, the size the design
// allowed (see above): the workspace list, Settings, the Help page (with the guide's
// text), jump-to and the couldn't-read page, all merged, put the tree at
// 3,818,988 bytes (the 180 px touch icon is 29,683 of them).
const maxEmbeddedBytes = 4 << 20

// These tests run against the real embedded tree, so they keep checking
// the built UI after the placeholder is replaced.

func TestEmbeddedFilesServed(t *testing.T) {
	opt := handlerOptions(t)
	opt.Assets = ui.Assets
	total := 0
	err := fs.WalkDir(ui.Assets, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(ui.Assets, name)
		if err != nil {
			return err
		}
		total += len(data)
		if strings.HasSuffix(name, ".map") {
			t.Errorf("source map embedded: %s", name)
		}
		wantType, known := contentTypes[path.Ext(name)]
		if !known {
			t.Errorf("%s: extension %q missing from the content type table", name, path.Ext(name))
		}
		wantCache := cacheRevalidate
		if strings.HasPrefix(name, "assets/") {
			wantCache = cacheImmutable
		} else if strings.Contains(name, "/") {
			t.Errorf("%s: only root files and assets/ are served", name)
			return nil
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := get(t, opt, method, "/"+name, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("%s /%s: status %d", method, name, rec.Code)
				continue
			}
			if got := rec.Header().Get("Content-Type"); got != wantType {
				t.Errorf("/%s: Content-Type %q, want %q", name, got, wantType)
			}
			if got := rec.Header().Get("Cache-Control"); got != wantCache {
				t.Errorf("/%s: Cache-Control %q, want %q", name, got, wantCache)
			}
			if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(data)) {
				t.Errorf("/%s: Content-Length %s, want %d", name, got, len(data))
			}
			wantBody := string(data)
			if method == http.MethodHead {
				wantBody = ""
			}
			if rec.Body.String() != wantBody {
				t.Errorf("%s /%s: body differs from the embedded file", method, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("embedded UI: %d bytes", total)
	if total == 0 || total >= maxEmbeddedBytes {
		t.Fatalf("embedded UI is %d bytes; want 1 to %d", total, maxEmbeddedBytes-1)
	}
}

func TestIndexPage(t *testing.T) {
	opt := handlerOptions(t)
	opt.Assets = ui.Assets
	want, err := fs.ReadFile(ui.Assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/", "/index.html"} {
		rec := get(t, opt, http.MethodGet, p, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: headers %v", p, rec.Header())
		}
	}
	html := string(want)
	lower := strings.ToLower(html)
	// The CSP forbids inline code; the page must not need it.
	for _, m := range regexp.MustCompile(`<script\b[^>]*>`).FindAllString(lower, -1) {
		if !strings.Contains(m, " src=") {
			t.Errorf("inline script: %s", m)
		}
	}
	if strings.Contains(lower, "<style") {
		t.Error("inline <style> element")
	}
	if regexp.MustCompile(`\sstyle\s*=`).MatchString(lower) {
		t.Error("style= attribute")
	}
	if m := regexp.MustCompile(`\son[a-z]+\s*=`).FindString(lower); m != "" {
		t.Errorf("event handler attribute %q", m)
	}
	// Every absolute reference must be served: an embedded file, or an
	// API route (the placeholder links to the status endpoint).
	refs := regexp.MustCompile(`\s(?:src|href)\s*=\s*["']([^"']*)["']`).FindAllStringSubmatch(html, -1)
	for _, ref := range refs {
		target := ref[1]
		if !strings.HasPrefix(target, "/") {
			continue
		}
		if strings.HasPrefix(target, "/api/") {
			if target != "/api/v0/status" {
				t.Errorf("link to unknown API route %s", target)
			}
			continue
		}
		if _, err := fs.Stat(ui.Assets, strings.TrimPrefix(target, "/")); err != nil {
			t.Errorf("reference %s is not embedded: %v", target, err)
		}
	}
}

func TestStaticNotFound(t *testing.T) {
	opt := handlerOptions(t)
	opt.Assets = fstest.MapFS{
		"index.html":       {Data: []byte("<p>x</p>")},
		"notes.txt":        {Data: []byte("notes")},
		"assets/app.js":    {Data: []byte("export {}")},
		"assets/_x.js":     {Data: []byte("export {}")},
		"sub/deeper.txt":   {Data: []byte("not served")},
		"assets/sub/a.css": {Data: []byte("a{}")},
	}
	for path, code := range map[string]int{
		"/notes.txt":            200,
		"/assets/app.js":        200,
		"/assets/_x.js":         200,
		"/assets/sub/a.css":     200,
		"/nope":                 404,
		"/assets":               404,
		"/assets/":              404,
		"/assets/sub":           404,
		"/sub":                  404,
		"/sub/deeper.txt":       404,
		"/assets/../index.html": 404,
		"//index.html":          404,
		"/index.html/":          404,
		"/.":                    404,
	} {
		rec := get(t, opt, http.MethodGet, path, nil)
		if rec.Code != code {
			t.Errorf("%s: status %d, want %d", path, rec.Code, code)
		}
		if code == 404 && !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
			t.Errorf("%s: 404 Content-Type %q", path, rec.Header().Get("Content-Type"))
		}
	}
}

// TestLegalNoticesServed checks that the binary serves the project's
// NOTICE and THIRD_PARTY_NOTICES (ADR 0080) next to the web UI's
// third-party-licenses.txt, as plain text.
func TestLegalNoticesServed(t *testing.T) {
	opt := handlerOptions(t)
	opt.Assets = ui.Assets
	for _, name := range []string{"NOTICE.txt", "THIRD_PARTY_NOTICES.txt", "third-party-licenses.txt"} {
		want, err := fs.ReadFile(ui.Assets, name)
		if err != nil {
			t.Fatal(err)
		}
		rec := get(t, opt, http.MethodGet, "/"+name, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
			t.Errorf("/%s: status %d, body equal %v", name, rec.Code, rec.Body.String() == string(want))
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("/%s: Content-Type %q", name, got)
		}
	}
}
