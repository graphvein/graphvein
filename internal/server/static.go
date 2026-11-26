package server

import (
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// contentTypes maps every extension the UI ships to its media type. The
// table is explicit because mime.TypeByExtension reads the Windows registry,
// which can map .js to text/plain; under nosniff browsers then refuse to run
// the module scripts. A test fails for an embedded file missing here.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".svg":   "image/svg+xml",
	".json":  "application/json",
	".txt":   "text/plain; charset=utf-8",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
}

// Cache policies: hashed asset names never change content, everything else
// must be revalidated so a new binary's UI is picked up at once.
const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

// staticHandler serves exact files from the embedded UI. It never lists
// directories and has no index.html fallback: the UI uses hash routing, so
// an unknown path is an honest 404.
type staticHandler struct {
	assets fs.FS
}

func (h staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, cache, ok := assetName(r.URL.Path)
	if !ok || h.assets == nil {
		http.NotFound(w, r)
		return
	}
	data, err := readRegularFile(h.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType, known := contentTypes[path.Ext(name)]
	if !known {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data) // a write error means the client went away
	}
}

// assetName maps a request path to an embedded file name and its cache
// policy. Only "/", root files and files under /assets/ are served.
func assetName(urlPath string) (name, cache string, ok bool) {
	if urlPath == "/" {
		return "index.html", cacheRevalidate, true
	}
	name = strings.TrimPrefix(urlPath, "/")
	if name == urlPath || !fs.ValidPath(name) || name == "." {
		return "", "", false
	}
	if strings.HasPrefix(name, "assets/") {
		return name, cacheImmutable, true
	}
	if strings.Contains(name, "/") {
		return "", "", false
	}
	return name, cacheRevalidate, true
}

// readRegularFile reads name only if it is a regular file, so directory
// paths get a 404 instead of a listing.
func readRegularFile(fsys fs.FS, name string) ([]byte, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(fsys, name)
}
