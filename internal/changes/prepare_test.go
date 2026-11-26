package changes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// gatedStore counts file-node reads and, while gate is open, holds each one
// until gate is closed or the read's context ends, so a test can start a
// traversal while a background preparation is still reading.
type gatedStore struct {
	store.Store
	mu      sync.Mutex
	reads   int
	started chan struct{}
	gate    chan struct{}
	// kinds counts every Nodes read by queried kind.
	kinds map[string]int
}

func (g *gatedStore) Nodes(ctx context.Context, repository string, q store.Query) ([]contract.Node, error) {
	g.mu.Lock()
	if g.kinds == nil {
		g.kinds = map[string]int{}
	}
	g.kinds[q.Kind]++
	g.mu.Unlock()
	if q.Kind == "file" {
		g.mu.Lock()
		g.reads++
		gate, started := g.gate, g.started
		g.mu.Unlock()
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return g.Store.Nodes(ctx, repository, q)
}

func (g *gatedStore) fileReads() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads
}

func (g *gatedStore) kindReads(kind string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kinds[kind]
}

func preparedComparison(t *testing.T) (*gatedStore, *Comparison) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, stream := range []string{callStream("a", false), callStream("e", false)} {
		if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Compare(ctx, s, "callgap", Base{Kind: BasePrevious}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return &gatedStore{Store: s}, c
}

// TestPrepareBuildsBlindIndexOnce checks that a traversal after Prepare
// reads no file nodes, and that a traversal started while Prepare is still
// reading waits for it instead of reading again.
func TestPrepareBuildsBlindIndexOnce(t *testing.T) {
	ctx := context.Background()
	const seed = "function:src/a.ts#callee"

	g, c := preparedComparison(t)
	if err := c.Prepare(ctx, g); err != nil {
		t.Fatal(err)
	}
	if _, err := Impact(ctx, g, c, ImpactRequest{ID: seed}); err != nil {
		t.Fatal(err)
	}
	if n := g.fileReads(); n != 1 {
		t.Errorf("file-node reads after Prepare and a traversal = %d, want 1", n)
	}

	g, c = preparedComparison(t)
	g.started, g.gate = make(chan struct{}, 1), make(chan struct{})
	prepared := make(chan error, 1)
	go func() { prepared <- c.Prepare(ctx, g) }()
	<-g.started
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(depth int) {
			defer wg.Done()
			if _, err := Impact(ctx, g, c, ImpactRequest{ID: seed, Depth: depth}); err != nil {
				errs <- err
			}
		}(1 + i%3)
	}
	close(g.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if n := g.fileReads(); n != 1 {
		t.Errorf("file-node reads with traversals waiting on Prepare = %d, want 1", n)
	}
}

// TestPrepareCancelled checks that a traversal waiting on a preparation
// whose context ends (a newer run, the server stopping) builds the index
// itself, and that Prepare reports the cancellation.
func TestPrepareCancelled(t *testing.T) {
	g, c := preparedComparison(t)
	g.started, g.gate = make(chan struct{}, 1), make(chan struct{})
	prepareCtx, cancel := context.WithCancel(context.Background())
	prepared := make(chan error, 1)
	go func() { prepared <- c.Prepare(prepareCtx, g) }()
	<-g.started

	traversed := make(chan error, 1)
	go func() {
		_, err := Impact(context.Background(), g, c, ImpactRequest{ID: "function:src/a.ts#callee"})
		traversed <- err
	}()
	cancel()
	if err := <-prepared; !errors.Is(err, context.Canceled) {
		t.Errorf("Prepare after cancel = %v, want context.Canceled", err)
	}
	// The traversal now reads itself; let its read through.
	close(g.gate)
	if err := <-traversed; err != nil {
		t.Fatalf("traversal after a cancelled preparation: %v", err)
	}
	if n := g.fileReads(); n != 2 {
		t.Errorf("file-node reads = %d, want 2 (the cancelled one and the traversal's)", n)
	}
}

// tableStream is a SQLModel table, an owner that reads it (an edge with its
// table_access entry) and an owner whose table_access_unknown entry names
// it; tableHash changes the table between runs so it is an item.
func tableStream(tableHash string) string {
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	table := "table:models/m.py#Hero"
	reader := "function:models/q.py#heroes"
	maybe := "function:models/q.py#maybe"
	lines := []string{
		`{"type":"file","path":"models/m.py","language":"python","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat(tableHash, 64) + `"}`,
		`{"type":"file","path":"models/q.py","language":"python","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("b", 64) + `"}`,
		`{"type":"node","identity":"` + table + `","kind":"table","level":4,"name":"Hero","qualified_name":"Hero","file":"models/m.py","span":` + span + `,"content_hash":"sha256:` + strings.Repeat(tableHash, 64) + `","attributes":{"source":"model","tool":"sqlmodel"}}`,
		`{"type":"node","identity":"` + reader + `","kind":"function","level":4,"name":"heroes","qualified_name":"heroes","file":"models/q.py","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("d", 64) + `","attributes":{"table_access":[{"kind":"reads_table","target":"` + table + `","tool":"sqlmodel","role":"runs","operations":["select"],"rows":["CT-P1"],"verification":"mapped","assumes":[]}]}}`,
		`{"type":"node","identity":"` + maybe + `","kind":"function","level":4,"name":"maybe","qualified_name":"maybe","file":"models/q.py","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("e", 64) + `","attributes":{"table_access_unknown":[{"row":"CT-P1","line":1,"tool":"sqlmodel","reason":"site_partial","partial":true,"candidates":["` + table + `"]}]}}`,
		`{"type":"edge","kind":"reads_table","source":"` + reader + `","target":"` + table + `","provenance":"inferred","confidence":0.8,"evidence":[{"file":"models/q.py","span":` + span + `}]}`,
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"tables","generated_at":"2026-11-14T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":2,"node":3,"edge":1,"diagnostic":0}}`, len(lines))
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestPrepareBuildsTableUseIndex checks that Prepare builds the I8
// candidate index when an item is a table, reading the file nodes once
// for both indexes, so a table traversal after it reads no code symbol or
// file nodes; that concurrent traversals and Prepare build it once; and that a comparison without table or column
// items does not read code symbols in Prepare.
func TestPrepareBuildsTableUseIndex(t *testing.T) {
	ctx := context.Background()
	const seed = "table:models/m.py#Hero"
	open := func(t *testing.T) (*gatedStore, *Comparison) {
		t.Helper()
		s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		for _, stream := range []string{tableStream("a"), tableStream("c")} {
			if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
				t.Fatal(err)
			}
		}
		c, err := Compare(ctx, s, "tables", Base{Kind: BasePrevious}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !c.hasTableItems() {
			t.Fatalf("the fixture has no table item")
		}
		return &gatedStore{Store: s}, c
	}

	g, c := open(t)
	if err := c.Prepare(ctx, g); err != nil {
		t.Fatal(err)
	}
	if n := g.kindReads("function"); n != 1 {
		t.Errorf("function-node reads in Prepare = %d, want 1", n)
	}
	if n := g.kindReads("file"); n != 1 {
		t.Errorf("file-node reads in Prepare = %d, want 1 (shared by both indexes)", n)
	}
	r, err := Impact(ctx, g, c, ImpactRequest{ID: seed})
	if err != nil {
		t.Fatal(err)
	}
	if n := g.kindReads("function"); n != 1 {
		t.Errorf("function-node reads after Prepare and a traversal = %d, want 1", n)
	}
	if n := g.kindReads("file"); n != 1 {
		t.Errorf("file-node reads after Prepare and a traversal = %d, want 1", n)
	}
	if len(r.TableUses) != 1 || len(r.TableUses[0].MayAlsoBeUsedBy) != 1 || r.TableUses[0].MayAlsoBeUsedBy[0].Owner != "function:models/q.py#maybe" {
		t.Errorf("table uses = %+v, want one entry with function:models/q.py#maybe as a possible user", r.TableUses)
	}

	g, c = open(t)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.Prepare(ctx, g); err != nil {
			errs <- err
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(depth int) {
			defer wg.Done()
			if _, err := Impact(ctx, g, c, ImpactRequest{ID: seed, Depth: depth}); err != nil {
				errs <- err
			}
		}(1 + i%3)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if f, n := g.kindReads("file"), g.kindReads("function"); f != 1 || n != 1 {
		t.Errorf("file- and function-node reads with Prepare and traversals at once = %d and %d, want 1 and 1", f, n)
	}

	g, c = preparedComparison(t)
	if err := c.Prepare(ctx, g); err != nil {
		t.Fatal(err)
	}
	if n := g.kindReads("function"); n != 0 {
		t.Errorf("function-node reads in Prepare without table items = %d, want 0", n)
	}
}
