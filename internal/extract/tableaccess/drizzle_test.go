package tableaccess

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTree writes inline files under a temporary root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestDrizzleSQLite checks a SQLite group: its CT-Z1/CT-Z2 claims are
// mapped (the toSQL oracle compares pg-core builders only, Revision 4),
// and an sql template run by the SQLite methods is a CT-Z4 site refused
// with no_schema_source (TA-V4 is Postgres semantics).
func TestDrizzleSQLite(t *testing.T) {
	root := writeTree(t, map[string]string{
		"package.json":      `{"name": "lite", "private": true, "dependencies": {"drizzle-orm": "0.45.4"}, "devDependencies": {"drizzle-kit": "0.31.11"}}`,
		"package-lock.json": `{"name": "lite", "lockfileVersion": 3, "packages": {"": {"name": "lite"}, "node_modules/drizzle-orm": {"version": "0.45.4"}, "node_modules/drizzle-kit": {"version": "0.31.11"}}}`,
		"drizzle.config.ts": "import { defineConfig } from \"drizzle-kit\";\nexport default defineConfig({ dialect: \"sqlite\", schema: \"./schema.ts\" });\n",
		"schema.ts":         "import { integer, sqliteTable, text } from \"drizzle-orm/sqlite-core\";\nexport const notes = sqliteTable(\"notes\", { id: integer(\"id\").primaryKey(), body: text(\"body\") });\n",
		"use.ts": "import { sql } from \"drizzle-orm\";\nimport { notes } from \"./schema\";\n" +
			"export function list(db) {\n  return db.select().from(notes);\n}\n" +
			"export function add(db) {\n  return db.insert(notes).values({ body: \"x\" });\n}\n" +
			"export function raw(db) {\n  return db.run(sql`delete from ${notes}`);\n}\n",
	})
	a := analyze(loadRoot(t, root))
	got := map[string]site{}
	for _, s := range a.sites {
		got[s.row] = s
	}
	for _, row := range []string{"CT-Z1", "CT-Z2"} {
		s, ok := got[row]
		if !ok || len(s.reads)+len(s.writes) != 1 || s.verification != VerificationMapped {
			t.Errorf("%s: %+v, want one claim, mapped", row, s)
		}
	}
	z4, ok := got["CT-Z4"]
	if !ok || z4.unknown == nil || reasonCode(z4.unknown.Reason) != ReasonNoSchemaSource {
		t.Errorf("CT-Z4: %+v, want no_schema_source", z4)
	}
}
