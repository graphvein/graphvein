package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/store"
)

// countingStore counts FileHashes reads, the store read the change
// endpoints cache per index state.
type countingStore struct {
	store.Store
	hashReads int
}

func (c *countingStore) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	c.hashReads++
	return c.Store.FileHashes(ctx, repo)
}

// TestChangesStateCache checks the change endpoints' per-index-state cache
// on both backends: the stored file hashes are read once per latest run;
// a freshness check is reused, with its own checked_at, only within
// freshnessReuse and only for the same index state; after the window an
// edited file makes the next check stale, and a new run drops the entry.
func TestChangesStateCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	replaceTree(t, filepath.Join(changeFixture, "rev1"), root)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		ctx := context.Background()
		cs := &countingStore{Store: rb.read}
		opt := testOptions(root, cs)
		runs, err := cs.Runs(ctx, opt.Repository)
		if err != nil {
			t.Fatal(err)
		}
		var st changesState

		for i := 0; i < 3; i++ {
			if _, err := st.fileHashes(ctx, opt, runs); err != nil {
				t.Fatal(err)
			}
		}
		first := st.freshness(ctx, opt, runs)
		if first.State != stateFresh {
			t.Fatalf("freshness = %+v, want fresh", first)
		}
		// fileHashes three times read once; the check reads its own.
		if cs.hashReads != 2 {
			t.Fatalf("FileHashes reads = %d, want 2", cs.hashReads)
		}

		path := filepath.Join(root, "server", "items.ts")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(path, data, 0o644)
		if err := os.WriteFile(path, append(data, []byte("\n// edited\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		if again := st.freshness(ctx, opt, runs); again.State != stateFresh || again.CheckedAt != first.CheckedAt || cs.hashReads != 2 {
			t.Fatalf("within the reuse window: %+v (reads %d), want the first check repeated", again, cs.hashReads)
		}

		// After the window the next request checks again.
		st.checked = st.checked.Add(-freshnessReuse)
		if stale := st.freshness(ctx, opt, runs); stale.State != stateStale || stale.ChangedCount == nil || *stale.ChangedCount != 1 {
			t.Fatalf("after the window: %+v, want stale with one changed file", stale)
		}

		// A new latest run drops the cached hashes and the check.
		later := append(append([]store.Run{}, runs...), store.Run{ID: "next", FinishedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)})
		reads := cs.hashReads
		if _, err := st.fileHashes(ctx, opt, later); err != nil {
			t.Fatal(err)
		}
		if cs.hashReads != reads+1 || st.fresh != nil {
			t.Fatalf("new run: reads %d (want %d), cached check %+v (want none)", cs.hashReads, reads+1, st.fresh)
		}
	})
}
