package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// Synthetic schema size (task T018b1 section 7): one set with 2,000 tables,
// 30,000 columns, 4,000 indexes and 3,000 foreign-key pairs, 600 of them
// into one hub table from 600 different tables.
const (
	synthTables     = 2000
	synthColumns    = 15 // per table
	synthIndexes    = 2  // per table
	synthHubSources = 600
	synthFiles      = 20
)

// syntheticSchema writes the synthetic store as an external snapshot of
// repository "synthetic" and returns the hub table's identity. It is
// generated here rather than committed as data.
func syntheticSchema(t testing.TB, s store.Store) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	hash := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	span := &contract.Span{StartLine: 1, EndLine: 1, EndColumn: 1}
	write := func(v any) {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	counts := contract.Counts{}
	write(contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "synthetic", Version: "1"}, Repository: "synthetic", GeneratedAt: "2026-11-04T00:00:00Z"})
	file := func(i int) string { return fmt.Sprintf("db/migrations/%02d_part.sql", i/(synthTables/synthFiles)+1) }
	for f := 0; f < synthFiles; f++ {
		write(contract.File{Type: "file", Path: file(f * synthTables / synthFiles), Language: "sql", Precision: "precise", ContentHash: hash})
		counts.File++
	}
	write(contract.File{Type: "file", Path: "db/Cargo.toml", Language: "toml", Precision: "precise", ContentHash: hash})
	counts.File++
	node := func(kind, f, qn string, level int, attrs map[string]any) string {
		id, err := contract.Identity(kind, f, qn)
		if err != nil {
			t.Fatal(err)
		}
		write(contract.Node{Type: "node", Identity: id, Kind: kind, Level: level, Name: qn, QualifiedName: qn, File: f, Span: span, ContentHash: hash, Attributes: attrs})
		counts.Node++
		return id
	}
	var edges []contract.Edge
	edge := func(kind, source, target, f string) {
		edges = append(edges, contract.Edge{Type: "edge", Kind: kind, Source: source, Target: target, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: f, Span: *span}}})
	}
	record := map[string]any{"total": 0, "by_kind": map[string]any{}}
	migrations := map[string]string{}
	for f := 0; f < synthFiles; f++ {
		name := file(f * synthTables / synthFiles)
		migrations[name] = node("migration", name, "migration", 3, map[string]any{"tool": "sqlx", "set": "db/migrations", "package": "db/Cargo.toml", "dialect": "postgres", "order": f + 1, "applied": true, "set_opaque_statements": record})
	}
	tables := make([]string, synthTables)
	firstColumn := make([]string, synthTables)
	columnsOf := make([][]string, synthTables)
	for i := 0; i < synthTables; i++ {
		f := file(i)
		name := fmt.Sprintf("t%04d", i)
		tables[i] = node("table", f, name, 4, map[string]any{"name": name, "schema": nil, "dialect": "postgres", "set": "db/migrations", "state": "complete", "reasons": []any{}, "opaque_after": 0, "primary_key": []any{"c00"}})
		edge("contains", migrations[f], tables[i], f)
		for c := 0; c < synthColumns; c++ {
			col := node("column", f, fmt.Sprintf("%s.c%02d", name, c), 4, map[string]any{"name": fmt.Sprintf("c%02d", c), "position": c + 1, "type": "integer", "default": nil, "references": nil})
			edge("contains", tables[i], col, f)
			columnsOf[i] = append(columnsOf[i], col)
		}
		firstColumn[i] = columnsOf[i][0]
		for x := 0; x < synthIndexes; x++ {
			ix := node("db_index", f, fmt.Sprintf("%s.i%d", name, x), 4, map[string]any{"name": fmt.Sprintf("i%d", x), "unique": false, "elements": []any{"c01"}})
			edge("contains", tables[i], ix, f)
		}
	}
	// 600 pairs into the hub (table 0) from tables 1..600, then 2,400
	// pairs between other tables: 1,400 from column 2 of tables 600..1999
	// and 1,000 from column 3 of tables 600..1599.
	for i := 1; i <= synthHubSources; i++ {
		edge("foreign_key", columnsOf[i][1], firstColumn[0], file(i))
	}
	for i := synthHubSources; i < synthTables; i++ {
		edge("foreign_key", columnsOf[i][2], firstColumn[i-1], file(i))
	}
	for i := synthHubSources; i < synthHubSources+1000; i++ {
		edge("foreign_key", columnsOf[i][3], firstColumn[i-2], file(i))
	}
	for _, e := range edges {
		write(e)
		counts.Edge++
	}
	for f := 0; f < synthFiles; f++ {
		write(contract.Diagnostic{Type: "diagnostic", File: file(f * synthTables / synthFiles), Severity: "info", Code: "schema_opaque_statement", Message: "synthetic"})
		counts.Diagnostic++
	}
	counts.Records = counts.File + counts.Node + counts.Edge + counts.Diagnostic
	write(contract.Footer{Type: "footer", Counts: counts})
	if err := ingest.Stream(context.Background(), &buf, s); err != nil {
		t.Fatal(err)
	}
	return tables[0]
}

// median times f over five runs after one discarded warm-up when
// GRAPHVEIN_SCHEMA_TIMING=1 (the section 7 measurement); otherwise it runs
// f once, so the suite checks the shapes without paying for the timing.
func median(f func()) time.Duration {
	if os.Getenv("GRAPHVEIN_SCHEMA_TIMING") != "1" {
		start := time.Now()
		f()
		return time.Since(start)
	}
	f()
	var runs []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		f()
		runs = append(runs, time.Since(start))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
	return runs[2]
}

// TestSchemaSynthetic checks the caps and uncut counts on the synthetic
// store (SQLite; the shapes are covered on both backends by the fixture
// tests) and logs the section 7 figures: sets body size and latency, the
// hub table's latency, the summary's latency, and the per-file diagnostics
// read against Dump. Timings are logged, never asserted; set
// GRAPHVEIN_SCHEMA_TIMING=1 for medians of five warm runs.
func TestSchemaSynthetic(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now()
	hub := syntheticSchema(t, s)
	t.Logf("synthetic store written in %v", time.Since(start))
	opt := testOptions(t.TempDir(), s)
	opt.Repository = "synthetic"

	var setsBody []byte
	setsTime := median(func() {
		rec := get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("sets: %d %s", rec.Code, rec.Body.String())
		}
		setsBody = rec.Body.Bytes()
	})
	var sets struct {
		Sets []struct {
			Tables []struct {
				Node          contract.Node `json:"node"`
				Columns       []any         `json:"columns"`
				Indexes       []any         `json:"indexes"`
				ForeignKeysIn int           `json:"foreign_keys_in"`
			} `json:"tables"`
			ForeignKeys []any `json:"foreign_keys"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(setsBody, &sets); err != nil {
		t.Fatal(err)
	}
	if len(sets.Sets) != 1 || len(sets.Sets[0].Tables) != synthTables || len(sets.Sets[0].ForeignKeys) != 3000 {
		t.Fatalf("sets: %d groups", len(sets.Sets))
	}
	columns, indexes := 0, 0
	for _, tb := range sets.Sets[0].Tables {
		columns += len(tb.Columns)
		indexes += len(tb.Indexes)
		if tb.Node.Identity == hub && tb.ForeignKeysIn != synthHubSources {
			t.Errorf("hub foreign_keys_in %d", tb.ForeignKeysIn)
		}
	}
	if columns != synthTables*synthColumns || indexes != synthTables*synthIndexes {
		t.Errorf("%d columns, %d indexes", columns, indexes)
	}

	var detail schemaTableResponse
	tableTime := median(func() {
		rec := get(t, opt, http.MethodGet, schemaPath("table", hub), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("table: %d", rec.Code)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
	})
	if len(detail.Neighbours.Incoming) != NeighboursCap || detail.Truncated == nil || detail.Truncated.TablesOmitted != synthHubSources-NeighboursCap || detail.Truncated.PairsOmitted != synthHubSources-NeighboursCap {
		t.Errorf("hub: %d incoming, truncated %+v", len(detail.Neighbours.Incoming), detail.Truncated)
	}
	summaryTime := median(func() {
		if rec := get(t, opt, http.MethodGet, "/api/v0/schema/summary", nil); rec.Code != http.StatusOK {
			t.Fatalf("summary: %d", rec.Code)
		}
	})
	file := "db/migrations/01_part.sql"
	var viaDump, viaRead int
	dumpTime := median(func() {
		snap, err := s.Dump(ctx, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		viaDump = 0
		for _, d := range snap.Diagnostics {
			if d.File == file {
				viaDump++
			}
		}
	})
	readTime := median(func() {
		ds, err := s.Diagnostics(ctx, "synthetic", []string{file})
		if err != nil {
			t.Fatal(err)
		}
		viaRead = len(ds)
	})
	if viaDump != 1 || viaRead != 1 {
		t.Errorf("diagnostics of %s: Dump %d, Diagnostics %d", file, viaDump, viaRead)
	}
	t.Logf("sets: %d bytes, median %v; table (hub): median %v; summary: median %v; diagnostics of one file: Dump %v, Diagnostics %v", len(setsBody), setsTime, tableTime, summaryTime, dumpTime, readTime)
}
