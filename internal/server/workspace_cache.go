package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/config"
)

// The answer cache of a workspace server (ADR 0087, D3): an open index
// keeps the answers of the GET views below, so switching back to it is
// instant. An answer is served again only while the index's fingerprint
// (its latest runs, its graphvein.yaml and, for a project, the manifest
// files and the last link pass) is the one it was computed at, and only
// until this server's next run of the index ends. The views listed read
// nothing else: views that report file states, editor links or source
// lines (which change with the files, not the index) and freshness are
// never cached.

// cachedPaths are the GET views the cache may answer.
var cachedPaths = map[string]bool{
	"/api/v0/api/endpoints":      true,
	"/api/v0/precision":          true,
	"/api/v0/schema/summary":     true,
	"/api/v0/schema/sets":        true,
	"/api/v0/schema/assumptions": true,
	"/api/v0/architecture":       true,
	"/api/v0/architecture/chain": true,
}

// Cache bounds (D3).
const (
	cacheIndexLimit int64 = 64 << 20
	cacheTotalLimit int64 = 256 << 20
	cacheEntryLimit       = 4 << 20
)

// cacheHeader reports hit, miss or bypass (the fingerprint could not be
// read) on every answer of a cached path.
const cacheHeader = "X-Graphvein-Cache"

// cacheBudget is the bytes cached by every index of one server.
type cacheBudget struct {
	mu    sync.Mutex
	total int64
	limit int64
}

func (b *cacheBudget) add(n int64) {
	b.mu.Lock()
	b.total += n
	b.mu.Unlock()
}

func (b *cacheBudget) fits(n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total+n <= b.limit
}

// answerCache is one open index's cache.
type answerCache struct {
	ih     *indexHandler
	budget *cacheBudget
	limit  int64

	mu         sync.Mutex
	generation uint64
	entries    map[string]*cachedAnswer
	size       int64
	tick       uint64
}

type cachedAnswer struct {
	fingerprint string
	header      http.Header
	body        []byte
	used        uint64
}

func newAnswerCache(ih *indexHandler, budget *cacheBudget) *answerCache {
	return &answerCache{ih: ih, budget: budget, limit: cacheIndexLimit, entries: map[string]*cachedAnswer{}}
}

// invalidate drops every answer and starts a new generation, so an answer
// computed before it is not stored after it. A run of this server calls it
// when the run ends.
func (c *answerCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.budget.add(-c.size)
	c.entries, c.size = map[string]*cachedAnswer{}, 0
}

// serve answers r from the cache or through h, storing a 200 answer.
func (c *answerCache) serve(w http.ResponseWriter, r *http.Request, h http.Handler) {
	fp, err := c.ih.fingerprint(r.Context())
	if err != nil {
		w.Header().Set(cacheHeader, "bypass")
		h.ServeHTTP(w, r)
		return
	}
	key := r.URL.Path + "?" + r.URL.Query().Encode()
	c.mu.Lock()
	gen := c.generation
	if a := c.entries[key]; a != nil && a.fingerprint == fp {
		c.tick++
		a.used = c.tick
		body, header := a.body, a.header
		c.mu.Unlock()
		for k, v := range header {
			w.Header()[k] = v
		}
		w.Header().Set(cacheHeader, "hit")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
	}
	c.mu.Unlock()
	rec := &captureWriter{header: http.Header{}, status: http.StatusOK}
	h.ServeHTTP(rec, r)
	if rec.status == http.StatusOK && rec.body.Len() <= cacheEntryLimit && r.Context().Err() == nil {
		c.put(key, fp, gen, rec.header.Clone(), bytes.Clone(rec.body.Bytes()))
	}
	for k, v := range rec.header {
		w.Header()[k] = v
	}
	w.Header().Set(cacheHeader, "miss")
	w.WriteHeader(rec.status)
	w.Write(rec.body.Bytes())
}

// put stores an answer computed in generation gen, evicting this cache's
// least recently used answers to stay within the index and total limits.
func (c *answerCache) put(key, fp string, gen uint64, header http.Header, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.generation {
		return
	}
	n := int64(len(body))
	if old := c.entries[key]; old != nil {
		c.drop(key, old)
	}
	for c.size+n > c.limit || !c.budget.fits(n) {
		var victimKey string
		var victim *cachedAnswer
		for k, a := range c.entries {
			if victim == nil || a.used < victim.used {
				victimKey, victim = k, a
			}
		}
		if victim == nil {
			return
		}
		c.drop(victimKey, victim)
	}
	c.tick++
	c.entries[key] = &cachedAnswer{fingerprint: fp, header: header, body: body, used: c.tick}
	c.size += n
	c.budget.add(n)
}

func (c *answerCache) drop(key string, a *cachedAnswer) {
	delete(c.entries, key)
	n := int64(len(a.body))
	c.size -= n
	c.budget.add(-n)
}

// captureWriter records a handler's answer so it can be cached.
type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func (c *captureWriter) Header() http.Header { return c.header }

func (c *captureWriter) WriteHeader(status int) {
	if !c.wrote {
		c.status, c.wrote = status, true
	}
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.wrote = true
	return c.body.Write(p)
}

// fingerprint names what the cached views read: the latest runs, the
// configuration files and, for a project, the manifest and the last link
// pass. Any read error is returned, and the request is answered uncached.
func (ih *indexHandler) fingerprint(ctx context.Context) (string, error) {
	var b strings.Builder
	runs := func(repository string) error {
		rs, err := ih.opt.Store.Runs(ctx, repository)
		if err != nil {
			return fmt.Errorf("reading runs: %w", err)
		}
		b.WriteString(repository + "\x00" + strconv.Itoa(len(rs)))
		if last := latestStoreRun(rs); last != nil {
			b.WriteString("\x00" + last.ID + "\x00" + last.FinishedAt)
		}
		b.WriteString("\n")
		return nil
	}
	if ih.pr == nil {
		if err := runs(ih.opt.Repository); err != nil {
			return "", err
		}
		if !ih.opt.noCheckout {
			if err := fileHash(&b, filepath.Join(ih.opt.Root, config.File)); err != nil {
				return "", err
			}
		}
		return digest(b.String()), nil
	}
	// A manifest edited on disk is served from now on (as a /project
	// request would), so the routes and the hash agree.
	ih.pr.refreshFromDisk()
	ih.pr.editMu.Lock()
	b.WriteString(ih.pr.diskHash + "\n")
	ih.pr.editMu.Unlock()
	p := ih.opt.Project.current()
	for _, m := range p.Members {
		if err := runs(m.Repository); err != nil {
			return "", err
		}
		if m.Checkout {
			if err := fileHash(&b, filepath.Join(m.Path, config.File)); err != nil {
				return "", err
			}
		}
	}
	passes, err := ih.opt.Store.ProjectLinkRuns(ctx, p.Name)
	if err != nil {
		return "", fmt.Errorf("reading link passes: %w", err)
	}
	if n := len(passes); n > 0 {
		b.WriteString("pass\x00" + strconv.FormatInt(passes[n-1].Seq, 10) + "\x00" + passes[n-1].ID + "\n")
	}
	return digest(b.String()), nil
}

// fileHash adds the content hash of path ("none" when it does not exist).
func fileHash(b *strings.Builder, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b.WriteString(path + "\x00none\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	b.WriteString(path + "\x00" + digest(string(data)) + "\n")
	return nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
