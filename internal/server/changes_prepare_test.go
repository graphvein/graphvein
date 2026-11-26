package server

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// slowFileStore holds every file-node read until the read's context ends,
// standing in for a slow preparation, and counts the reads it cancelled.
type slowFileStore struct {
	store.Store
	mu        sync.Mutex
	started   chan struct{}
	cancelled int
}

func (b *slowFileStore) Nodes(ctx context.Context, repository string, q store.Query) ([]contract.Node, error) {
	if q.Kind != "file" {
		return b.Store.Nodes(ctx, repository, q)
	}
	b.started <- struct{}{}
	<-ctx.Done()
	b.mu.Lock()
	b.cancelled++
	b.mu.Unlock()
	return nil, ctx.Err()
}

// TestComparisonPreparedInBackground checks that a computed comparison's
// blind-spot preparation starts only once the response is written, that a comparison
// computed at a newer run stops the older one's preparation, and that
// shutdown stops and waits for the rest, so nothing reads the store after
// Serve returns.
func TestComparisonPreparedInBackground(t *testing.T) {
	backends(t, func(t *testing.T, write, read store.Store) {
		ctx := context.Background()
		root := fixtureRoot(t)
		indexRoot(t, root, write)
		indexRoot(t, root, write)
		slow := &slowFileStore{Store: read, started: make(chan struct{}, 4)}
		a := newChangesAPI(testOptions(root, slow))
		runs, err := read.Runs(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		q := url.Values{"base": {"previous"}}
		first, err := a.comparison(ctx, q, runs)
		if err != nil {
			t.Fatal(err)
		}
		// Nothing reads the file nodes until the response is written.
		select {
		case <-slow.started:
			t.Fatal("the preparation started before the response")
		default:
		}
		a.startPreparations()
		waitStarted(t, slow)

		// The same run answers from the cache; a newer run computes again
		// and stops the first comparison's preparation.
		if again, err := a.comparison(ctx, q, runs); err != nil || again != first {
			t.Fatalf("cached comparison = %p, %v; want %p", again, err, first)
		}
		newer := append([]store.Run{}, runs...)
		last := *latestStoreRun(newer)
		at, err := time.Parse(time.RFC3339Nano, last.FinishedAt)
		if err != nil {
			t.Fatal(err)
		}
		last.ID, last.FinishedAt = last.ID+"-newer", at.Add(time.Minute).Format(time.RFC3339Nano)
		newer = append(newer, last)
		if _, err := a.comparison(ctx, q, newer); err != nil {
			t.Fatal(err)
		}
		a.startPreparations()
		waitStarted(t, slow)
		deadline := time.Now().Add(10 * time.Second)
		for cancelledReads(slow) < 1 {
			if time.Now().After(deadline) {
				t.Fatal("the older comparison's preparation was not cancelled by a newer run")
			}
			time.Sleep(5 * time.Millisecond)
		}
		a.mu.Lock()
		cached := len(a.cache)
		a.mu.Unlock()
		if cached != 1 {
			t.Errorf("cached comparisons after a newer run = %d, want 1", cached)
		}

		a.shutdown()
		if n := cancelledReads(slow); n != 2 {
			t.Errorf("cancelled preparations after shutdown = %d, want 2", n)
		}
		// After shutdown no preparation starts.
		a.cache = nil
		if _, err := a.comparison(ctx, q, runs); err != nil {
			t.Fatal(err)
		}
		a.startPreparations()
		a.preparing.Wait()
		select {
		case <-slow.started:
			t.Error("a preparation started after shutdown")
		default:
		}
	})
}

func waitStarted(t *testing.T, b *slowFileStore) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(10 * time.Second):
		t.Fatal("no background preparation started")
	}
}

func cancelledReads(b *slowFileStore) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancelled
}
