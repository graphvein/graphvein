package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// TestFiles checks Files and FilesByPath on both backends: every record in
// byte order of path (also where a database collation would differ), and
// FilesByPath across several chunks with duplicates and missing paths.
func TestFiles(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		var stream bytes.Buffer
		enc := json.NewEncoder(&stream)
		records := []any{contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "crafted", Version: "files"}, Repository: "files", GeneratedAt: "2026-11-16T00:00:00Z"}}
		hash := contract.ContentHash(nil)
		// Upper- and lower-case names: byte order puts "B" before "a".
		for i := 0; i < 300; i++ {
			for _, name := range []string{fmt.Sprintf("a/%03d.ts", i), fmt.Sprintf("B/%03d.py", i)} {
				records = append(records, contract.File{Type: "file", Path: name, Language: "typescript", Precision: "best_effort", ContentHash: hash})
			}
		}
		records = append(records, map[string]any{"type": "footer", "counts": map[string]int{"records": len(records) - 1, "file": len(records) - 1, "node": 0, "edge": 0, "diagnostic": 0}})
		for _, r := range records {
			if err := enc.Encode(r); err != nil {
				t.Fatal(err)
			}
		}
		if err := ingest.Stream(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "files")
		if err != nil {
			t.Fatal(err)
		}
		all, err := s.Files(ctx, "files")
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 600 || all[0].Path != "B/000.py" || all[599].Path != "a/299.ts" {
			t.Fatalf("Files returned %d records, first %q", len(all), all[0].Path)
		}
		byPath := map[string]contract.File{}
		for _, f := range dump.Files {
			byPath[f.Path] = f
		}
		for i := 1; i < len(all); i++ {
			if all[i-1].Path >= all[i].Path || !reflect.DeepEqual(all[i], byPath[all[i].Path]) {
				t.Fatalf("Files record %d (%q) out of order or differs from Dump", i, all[i].Path)
			}
		}
		var paths []string
		var want []contract.File
		for i := len(all) - 1; i >= 0; i-- {
			if i%3 != 0 {
				paths = append(paths, all[i].Path, all[i].Path, all[i].Path+".missing")
			}
		}
		for i, f := range all {
			if i%3 != 0 {
				want = append(want, f)
			}
		}
		got, err := s.FilesByPath(ctx, "files", paths)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("FilesByPath returned %d records, want %d", len(got), len(want))
		}
		if got, err := s.FilesByPath(ctx, "files", nil); err != nil || got == nil || len(got) != 0 {
			t.Fatalf("empty FilesByPath = %v, %v", got, err)
		}
		if got, err := s.Files(ctx, "other"); err != nil || len(got) != 0 {
			t.Fatalf("other repository Files = %v, %v", got, err)
		}
	})
}
