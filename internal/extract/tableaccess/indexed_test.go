package tableaccess_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const sqlxFixture = "../../../testdata/fixtures/table-access-sqlx"

// TestTableAccessSqlxFixture indexes the sqlx fixture through the indexer
// (the one call in internal/index/index.go) and checks the stored graph
// against testdata/fixtures/table-access-sqlx.golden.json, then a warm
// replay that parses nothing and emits the same stream.
func TestTableAccessSqlxFixture(t *testing.T) {
	checkIndexedFixture(t, sqlxFixture)
}

const sqlmodelFixture = "../../../testdata/fixtures/table-access-sqlmodel"

// TestTableAccessSqlmodelFixture checks the SQLModel fixture (rows
// CT-P1..CT-P5 and the Python execution records) against
// testdata/fixtures/table-access-sqlmodel.golden.json, cold and warm.
func TestTableAccessSqlmodelFixture(t *testing.T) {
	checkIndexedFixture(t, sqlmodelFixture)
}

const prismaFixture = "../../../testdata/fixtures/table-access-prisma"

// TestTableAccessPrismaFixture checks the Prisma fixture (rows CT-R1..CT-R3
// and the Prisma execution records) against
// testdata/fixtures/table-access-prisma.golden.json, cold and warm.
func TestTableAccessPrismaFixture(t *testing.T) {
	checkIndexedFixture(t, prismaFixture)
}

const drizzleFixture = "../../../testdata/fixtures/table-access-drizzle"

// TestTableAccessDrizzleFixture checks the Drizzle fixture (rows
// CT-Z1..CT-Z4 and the Drizzle execution records) against
// testdata/fixtures/table-access-drizzle.golden.json, cold and warm.
func TestTableAccessDrizzleFixture(t *testing.T) {
	checkIndexedFixture(t, drizzleFixture)
}

func checkIndexedFixture(t *testing.T, fixture string) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-14T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0) {
			t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
		}
		first = data
		if run == 0 {
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, fixture+".golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
		}
	}
}

const supabaseFixture = "../../../testdata/fixtures/table-access-supabase"

// TestTableAccessSupabaseFixture checks the Supabase fixture (rows SB-K1,
// SB-T1..SB-T5 and the Supabase D records) against
// testdata/fixtures/table-access-supabase.golden.json, cold and warm.
func TestTableAccessSupabaseFixture(t *testing.T) {
	checkIndexedFixture(t, supabaseFixture)
}
