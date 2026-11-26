package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/store"
)

// freshnessReuse is how long the change endpoints reuse one working-tree
// check. Every change response carries freshness, and the check walks and
// hashes the whole working tree, which on a large repository costs more
// than the response itself (T019 latency gate: an items page under 50 ms).
// Within this window a response repeats the last check together with that
// check's checked_at, so it never claims a later instant than the one
// checked; after it, the next request checks again.
const freshnessReuse = 2 * time.Second

// changesState caches, for the change endpoints, what depends only on the
// index state or on the last working-tree check: the stored file hashes of
// the latest run (read once per run through the store) and the freshness of
// the working tree against them. A new run (another latest run id or finish
// time) drops both.
type changesState struct {
	mu sync.Mutex
	// run identifies the index state the entry belongs to.
	run    string
	hashes map[string]string
	// fresh is the last freshness check, made at checked (monotonic, for
	// the reuse window); fresh.CheckedAt holds the reported instant.
	fresh   *freshnessStatus
	checked time.Time
}

// runKey identifies the index state by its latest run.
func runKey(runs []store.Run) string {
	last := latestStoreRun(runs)
	if last == nil {
		return ""
	}
	return last.ID + "\x00" + last.FinishedAt
}

// reset drops the entry when the index state changed. The caller holds mu.
func (c *changesState) reset(key string) {
	if c.run != key {
		c.run, c.hashes, c.fresh = key, nil, nil
	}
}

// fileHashes returns the stored content hash of every file of the latest
// run, read through the store once per index state. The map is shared:
// callers must not change it.
func (c *changesState) fileHashes(ctx context.Context, opt Options, runs []store.Run) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reset(runKey(runs))
	if c.hashes == nil {
		hashes, err := opt.Store.FileHashes(ctx, opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading file hashes: %w", err)
		}
		c.hashes = hashes
	}
	return c.hashes, nil
}

// freshness returns the working tree's freshness against the latest run,
// as /api/v0/status computes it, reusing a check younger than
// freshnessReuse made for the same index state. Concurrent requests wait
// for one check instead of walking the tree each.
func (c *changesState) freshness(ctx context.Context, opt Options, runs []store.Run) freshnessStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reset(runKey(runs))
	if c.fresh != nil && time.Since(c.checked) < freshnessReuse {
		return *c.fresh
	}
	checkedAt := opt.now()
	started := time.Now()
	changed, err := opt.freshness(ctx)
	out := freshnessFor(changed, err, len(runs) > 0)
	out.CheckedAt = checkedAt.UTC().Format(time.RFC3339Nano)
	if err != nil && ctx.Err() != nil {
		// A cancelled request's failed check says nothing about the tree.
		return out
	}
	c.fresh, c.checked = &out, started
	return out
}
