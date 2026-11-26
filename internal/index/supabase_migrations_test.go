package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestSupabaseMigrationsGolden indexes the T024c1 fixture (Supabase sets,
// rows SB-M1 to SB-M3) and checks the schema records against its golden:
// the CLI's members and order, foreign keys to platform tables (SB-V2) and
// the rls and policies attributes.
func TestSupabaseMigrationsGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/supabase-migrations")); err != nil {
		t.Fatal(err)
	}
	s := open(t)
	emit(t, root, s, options(t))
	out, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range out.Diagnostics {
		if d.Code == "extract_invalid_record" || d.Code == "invalid_record" || d.Code == "invalid_reference" {
			t.Fatal(d)
		}
	}
	golden.Check(t, "../../testdata/fixtures/supabase-migrations.golden.json", schemaSnapshot(out))
}
