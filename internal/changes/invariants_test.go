package changes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestViolationsDetect makes sure the invariant check is not vacuous: an
// unknown_now item turned into a removal inside a gap, a lost link without
// a proven reason and a one-sided hint are each reported.
func TestViolationsDetect(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, v := range []string{"v1", "v2"} {
		f, err := os.Open(filepath.Join("..", "..", "testdata", "fixtures", "change-impact", "streams", v+".ndjson"))
		if err != nil {
			t.Fatal(err)
		}
		err = ingest.Stream(ctx, f, s)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	c, err := Compare(ctx, s, "streams", Base{Kind: BasePrevious}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if v := c.Violations(); len(v) != 0 {
		t.Fatalf("clean comparison has violations: %v", v)
	}
	var gamma, link, alpha int = -1, -1, -1
	for i, it := range c.items {
		switch {
		case it.Identity == "function:src/c.ts#gamma":
			gamma = i
		case it.Kind == "documents_route":
			link = i
		case it.Identity == "function:src/a.ts#alpha":
			alpha = i
		}
	}
	if gamma < 0 || link < 0 || alpha < 0 {
		t.Fatal("fixture items missing")
	}
	// R3-3: a structural edge claimed beside a classified item.
	owner := ""
	for _, it := range c.items {
		if classifiedRows[it.Row] && it.Record == store.RecordNode {
			owner = it.Identity
		}
	}
	if owner == "" {
		t.Fatal("fixture has no C3-C12 node item")
	}
	c.items = append(c.items, Item{Row: "C16", Record: store.RecordEdge, Kind: "contains", Change: Removed,
		Identity: store.EdgeKey("contains", "file:x#x", owner)})
	c.items[gamma].Change = Removed
	c.items[link].Change = LinkLost
	c.items[alpha].Hints = []Hint{{Row: "C14", Other: "function:src/c.ts#gamma"}}
	got := strings.Join(c.Violations(), "\n")
	for _, want := range []string{"gamma: removed although", "lost link", "is not a mutual", "structural edge claimed beside"} {
		if !strings.Contains(got, want) {
			t.Errorf("violations do not report %q:\n%s", want, got)
		}
	}
}
