package changes

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestImpactReusedPerComparison checks that a comparison answers a repeated
// traversal of the same item and depth from its first result, recomputes for
// another depth, and that concurrent traversals and item reads on one shared
// comparison (as the server's comparison cache shares it) are safe under
// the race detector.
func TestImpactReusedPerComparison(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, stream := range []string{callStream("a", false), callStream("e", false)} {
		if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Compare(ctx, s, "callgap", Base{Kind: BasePrevious}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	const seed = "function:src/a.ts#callee"
	first, err := Impact(ctx, s, c, ImpactRequest{ID: seed})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Impact(ctx, s, c, ImpactRequest{ID: seed, Depth: DefaultDepth})
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Error("the same item and depth was traversed again")
	}
	shallow, err := Impact(ctx, s, c, ImpactRequest{ID: seed, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if shallow == first || shallow.Depth != 1 || len(shallow.Paths) != len(first.Paths) {
		t.Errorf("depth 1: %+v", shallow)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(depth int) {
			defer wg.Done()
			if _, err := Impact(ctx, s, c, ImpactRequest{ID: seed, Depth: depth}); err != nil {
				errs <- err
			}
		}(1 + i%3)
		go func() {
			defer wg.Done()
			for _, it := range c.AllItems() {
				_ = it.ImpactCount
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
