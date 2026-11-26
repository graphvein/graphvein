package index_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

var schemaKinds = map[string]bool{"migration": true, "table": true, "column": true, "db_index": true}

func schemaKind(identity string) bool {
	kind, _, _ := strings.Cut(identity, ":")
	return schemaKinds[kind]
}

// schemaSnapshot projects the records the schema pass adds.
func schemaSnapshot(snapshot store.Snapshot) store.Snapshot {
	out := store.Snapshot{}
	for _, n := range snapshot.Nodes {
		if schemaKinds[n.Kind] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if schemaKind(e.Target) || e.Kind == "foreign_key" || e.Kind == "alters" {
			out.Edges = append(out.Edges, e)
		}
	}
	for _, d := range snapshot.Diagnostics {
		if strings.HasPrefix(d.Code, "schema_") {
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
	return out
}

// copySchemaFixture copies the fixture and adds the R5 member that Git
// cannot hold portably: a migration that is not valid UTF-8. The symlink
// case of R5 is covered by internal/extract/schema, where it can be skipped
// on platforms without symlinks.
func copySchemaFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/sql-migrations")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pg-unreadable/migrations/2_unreadable.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Every member of pg-all-unreadable is unreadable (T018b1 review).
	if err := os.WriteFile(filepath.Join(root, "pg-all-unreadable/migrations/1_only.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSchemaGoldenAndRelinking(t *testing.T) {
	root := copySchemaFixture(t)
	s, opt := open(t), options(t)
	_, first, _ := emit(t, root, s, opt)
	dump := func(s store.Store) store.Snapshot {
		t.Helper()
		out, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range out.Diagnostics {
			if d.Code == "extract_invalid_record" || d.Code == "invalid_record" || d.Code == "invalid_reference" {
				t.Fatal(d)
			}
		}
		return schemaSnapshot(out)
	}
	before := dump(s)
	golden.Check(t, "../../testdata/fixtures/sql-migrations.golden.json", before)
	counts := map[string]int{}
	for _, n := range before.Nodes {
		counts[n.Kind]++
	}
	// Counts follow the golden; they are asserted so a golden rewrite that
	// loses records is noticed. T018a2 B1a: pg-unreadable's non-UTF-8 member
	// makes sqlx refuse the set (P10: 2 tables, 2 columns fewer);
	// pg-opaque-exists's CREATE TABLE IF NOT EXISTS after a DO block and
	// pg-rev2-columns's DROP INDEX IF EXISTS public.x state no facts they
	// may not have written (P3: 3 columns and 1 index fewer). T018a2 B1b:
	// pg-select-into's SELECT INTO of a UNION is not proven (P5, R1b), so
	// its migration may fail and the uncertain targets that CREATE TABLE IF
	// NOT EXISTS names become partial tables (3 tables more). T018a2
	// Revision 1.9 Y52: pg-rev2-types's DROP TYPE mood CASCADE drops the
	// two columns of type mood and mood[] (2 columns fewer, which Postgres
	// drops).
	if counts["migration"] != 49 || counts["table"] != 52 || counts["column"] != 98 || counts["db_index"] != 11 {
		t.Fatalf("fixture counts %v", counts)
	}

	// B5: a second index with no change parses nothing and emits the same
	// records.
	_, warm, _ := emit(t, root, s, opt)
	if warm.Parsed != 0 || !reflect.DeepEqual(before, dump(s)) {
		t.Fatal("warm replay changed", first, warm)
	}

	// B5: editing one migration changes only that set's schema records and
	// reparses no language file.
	file := filepath.Join(root, "pg-serial/migrations/2_add_serial.sql")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, []byte("ALTER TABLE counters ADD COLUMN l text;\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, next, _ := emit(t, root, s, opt)
	if next.Parsed != 0 {
		t.Fatal("language files reparsed", next)
	}
	after := dump(s)
	changed := func(a, b store.Snapshot) map[string]bool {
		set := map[string]bool{}
		index := map[string]string{}
		for _, n := range a.Nodes {
			index[n.Identity] = n.ContentHash + n.File
		}
		for _, n := range b.Nodes {
			if old, ok := index[n.Identity]; !ok || old != n.ContentHash+n.File || !reflect.DeepEqual(n, nodeOf(a, n.Identity)) {
				set[n.File] = true
			}
		}
		return set
	}
	for file := range changed(before, after) {
		if !strings.HasPrefix(file, "pg-serial/") {
			t.Fatalf("editing pg-serial changed records of %s", file)
		}
	}
	if len(after.Nodes) != len(before.Nodes)+1 {
		t.Fatalf("expected one new column: %d -> %d", len(before.Nodes), len(after.Nodes))
	}
}

func nodeOf(s store.Snapshot, identity string) any {
	for _, n := range s.Nodes {
		if n.Identity == identity {
			return n
		}
	}
	return nil
}

// TestSchemaCRLF checks that a CRLF copy of a migration gives the same
// records: the scanner normalizes line endings before hashing and spans.
func TestSchemaCRLF(t *testing.T) {
	project := func(crlf bool) store.Snapshot {
		root := copySchemaFixture(t)
		if crlf {
			file := filepath.Join(root, "pg-app/migrations/3_alter.sql")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s := open(t)
		emit(t, root, s, options(t))
		out, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		return schemaSnapshot(out)
	}
	if !reflect.DeepEqual(project(false), project(true)) {
		t.Fatal("CRLF migration changed schema records")
	}
}
