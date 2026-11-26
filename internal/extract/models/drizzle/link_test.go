package drizzle

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestLinkStepIsolation: a failure in one table's step becomes a
// diagnostic and the other tables are still claimed (AGENTS.md).
func TestLinkStepIsolation(t *testing.T) {
	in := input(t, map[string]string{
		"package.json":      manifestJSON("", ormDep, kitDep),
		"drizzle.config.ts": `export default { dialect: "postgresql", schema: "./db/*.ts" };`,
		"db/a.ts":           "import { pgTable, text } from \"drizzle-orm/pg-core\";\nexport const a = pgTable(\"a\", { id: text() });\nexport const b = pgTable(\"b\", { id: text() });\n",
	})
	faultHook = func(step, subject string) {
		if step == "table" && strings.HasSuffix(subject, "#a") {
			panic("boom")
		}
	}
	defer func() { faultHook = nil }()
	nodes, _, diags := Link(in)
	if hasIdentity(nodes, "table:db/a.ts#a") || !hasIdentity(nodes, "table:db/a.ts#b") {
		t.Errorf("isolation: %v", nodes)
	}
	if !hasDiag(diags, "schema_model_internal_error", "db/a.ts", "boom") {
		t.Errorf("no internal error diagnostic: %v", diagLines(diags))
	}
}

// TestLinkSharedSchema: two configs whose schemas reach the same module
// (T018g2 review). When they agree on dialect, casing and schemaFilter
// the table is claimed once, in the group of the first config by root,
// and the other group gets a diagnostic with its list marker; when they
// differ it is claimed in neither, and the message says in what.
func TestLinkSharedSchema(t *testing.T) {
	files := func(other string) map[string]string {
		return map[string]string{
			"package.json":      manifestJSON(`"a": "drizzle-kit generate --config other.config.ts"`, ormDep, kitDep),
			"drizzle.config.ts": `export default { dialect: "postgresql", schema: "./db/a.ts" };`,
			"other.config.ts":   "import { defineConfig } from \"drizzle-kit\";\nexport default defineConfig({ dialect: \"postgresql\", schema: [\"./db/a.ts\", \"./db/b.ts\"]" + other + " });",
			"db/a.ts":           tableSrc,
			"db/b.ts":           strings.ReplaceAll(tableSrc, "\"t\"", "\"u\""),
		}
	}
	nodes, _, diags := Link(input(t, files("")))
	if a := nodeAttrs(t, nodes, "table:db/a.ts#t"); a["set"] != "models/drizzle/drizzle.config.ts" {
		t.Errorf("agreeing configs: the shared table is not claimed in the first group: %v", a)
	}
	if a := nodeAttrs(t, nodes, "table:db/b.ts#t"); a["set"] != "models/drizzle/other.config.ts" {
		t.Errorf("the other config's own table: %v", a)
	}
	if !hasDiag(diags, codeNotClaimed, "db/a.ts", "configs drizzle.config.ts and other.config.ts, which agree", "claimed in model group models/drizzle/drizzle.config.ts only", ListMarker("models/drizzle/other.config.ts")) {
		t.Errorf("no shared-schema diagnostic for the other group: %v", diagLines(diags))
	}
	nodes, _, diags = Link(input(t, files(`, casing: "snake_case"`)))
	if hasIdentity(nodes, "table:db/a.ts#") || !hasIdentity(nodes, "table:db/b.ts#t") {
		t.Errorf("configs that differ: %v", nodes)
	}
	for _, set := range []string{"models/drizzle/drizzle.config.ts", "models/drizzle/other.config.ts"} {
		if !hasDiag(diags, codeNotClaimed, "db/a.ts", "configs drizzle.config.ts and other.config.ts, which differ in casing", "it is not claimed", ListMarker(set)) {
			t.Errorf("no differing-config diagnostic for %s: %v", set, diagLines(diags))
		}
	}
}

// TestLinkSameConfigTwoPackages: one config file that two packages run (a
// default config another package names with --config) is one group, and
// the message never names it twice; when the two working directories
// resolve its schema to different files, no table is claimed (DR-V5).
func TestLinkSameConfigTwoPackages(t *testing.T) {
	run := func(schema string, extra map[string]string) ([]contract.Node, []contract.Diagnostic) {
		src := map[string]string{
			"a/package.json":      manifestJSON("", ormDep, kitDep),
			"a/drizzle.config.ts": `export default { dialect: "postgresql", schema: "` + schema + `" };`,
			"a/db/s.ts":           tableSrc,
			"b/package.json":      manifestJSON(`"m": "drizzle-kit migrate --config ../a/drizzle.config.ts"`, ormDep, kitDep),
		}
		for k, v := range extra {
			src[k] = v
		}
		nodes, _, diags := Link(input(t, src))
		return nodes, diags
	}
	nodes, diags := run("../a/db/s.ts", nil)
	if a := nodeAttrs(t, nodes, "table:a/db/s.ts#t"); a["set"] != "models/drizzle/a/drizzle.config.ts" {
		t.Errorf("one config, one group: %v (%v)", a, diagLines(diags))
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "a/drizzle.config.ts and a/drizzle.config.ts") {
			t.Errorf("config named twice: %s", d.Message)
		}
	}
	nodes, diags = run("./db/s.ts", map[string]string{"b/db/s.ts": strings.ReplaceAll(tableSrc, "\"t\"", "\"v\"")})
	if hasIdentity(nodes, "table:") {
		t.Errorf("directory-dependent schema claimed: %v", nodes)
	}
	if !hasDiag(diags, codeNotProven, "a/drizzle.config.ts", "package directories a and b", "no table is claimed") {
		t.Errorf("no directory diagnostic: %v", diagLines(diags))
	}
}

// TestLinkModuleWithoutPayload: a schema module that builds its tables
// with builders imported through a local re-export has no model payload
// (gate G1); its exported values get one marker naming the file with the
// group's list marker, never silence (T018g2 review). Functions are not
// counted.
func TestLinkModuleWithoutPayload(t *testing.T) {
	in := input(t, map[string]string{
		"package.json":      manifestJSON("", ormDep, kitDep),
		"drizzle.config.ts": `export default { dialect: "postgresql", schema: "./db/*.ts" };`,
		"db/core.ts":        "export { pgTable, text } from \"drizzle-orm/pg-core\";\n",
		"db/users.ts":       "import { pgTable, text } from \"./core\";\nexport const users = pgTable(\"users\", { id: text() });\nexport const other = pgTable(\"other\", { id: text() });\nexport function helper() { return 1; }\n",
		"db/plain.ts":       tableSrc,
	})
	nodes, _, diags := Link(in)
	if !hasIdentity(nodes, "table:db/plain.ts#t") || hasIdentity(nodes, "table:db/users.ts#") {
		t.Errorf("nodes: %v", nodes)
	}
	if !hasDiag(diags, codeUnread, "db/users.ts", "db/users.ts: 2 exported value(s) not read (other, users)", "no Drizzle model payload", ListMarker("models/drizzle/drizzle.config.ts")) {
		t.Errorf("no payload marker: %v", diagLines(diags))
	}
	if hasDiag(diags, codeUnread, "db/core.ts") {
		t.Errorf("a module of re-exports only is marked: %v", diagLines(diags))
	}
	n := 0
	for _, d := range diags {
		if d.File == "db/users.ts" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d markers on db/users.ts, want 1: %v", n, diagLines(diags))
	}
}

// TestLinkTableCreator: a table creator's marker names the tables its
// calls build (T018g2 review), also when the creator is not exported.
func TestLinkTableCreator(t *testing.T) {
	for _, exported := range []string{"export ", ""} {
		in := input(t, map[string]string{
			"package.json":      manifestJSON("", ormDep, kitDep),
			"drizzle.config.ts": `export default { dialect: "postgresql", schema: "./s.ts" };`,
			"s.ts":              "import { pgTableCreator, text } from \"drizzle-orm/pg-core\";\n" + exported + "const createTable = pgTableCreator((n) => `app_${n}`);\nexport const users = createTable(\"users\", { id: text() });\nexport const posts = createTable(\"posts\", { id: text() });\n",
		})
		nodes, _, diags := Link(in)
		if hasIdentity(nodes, "table:") {
			t.Errorf("%q: creator tables claimed: %v", exported, nodes)
		}
		if !hasDiag(diags, codeUnread, "s.ts", "s.ts:2: createTable: pgTableCreator is not read (row D); it builds the table(s) \"users\", \"posts\"", ListMarker("models/drizzle/drizzle.config.ts")) {
			t.Errorf("%q: creator marker: %v", exported, diagLines(diags))
		}
	}
}

// TestLinkUnprovenCasing: an omitted name under a casing that is not
// literal is unknown (DR-K2); the explicit name is claimed.
func TestLinkUnprovenCasing(t *testing.T) {
	in := input(t, map[string]string{
		"package.json":      manifestJSON("", ormDep, kitDep),
		"drizzle.config.ts": "const c = process.env.C;\nexport default { dialect: \"postgresql\", schema: \"./s.ts\", casing: c };",
		"s.ts":              "import { pgTable, text } from \"drizzle-orm/pg-core\";\nexport const t = pgTable(\"t\", { someKey: text(), named: text(\"named\") });\n",
	})
	nodes, _, _ := Link(in)
	if a := nodeAttrs(t, nodes, "column:s.ts#t.someKey"); a["state"] != "unknown" {
		t.Errorf("omitted name under unknown casing: %v", a)
	}
	if a := nodeAttrs(t, nodes, "column:s.ts#t.named"); a["name"] != "named" || a["state"] != "read" {
		t.Errorf("explicit name: %v", a)
	}
}

// TestLinkMigrationOverlap: a Drizzle table whose name a migration set
// also defines gets the kit's schema_model_migration_overlap diagnostic,
// with the column difference when both are complete; the sources are not
// merged (as SQLModel and Prisma).
func TestLinkMigrationOverlap(t *testing.T) {
	in := input(t, map[string]string{
		"package.json":      manifestJSON("", ormDep, kitDep),
		"drizzle.config.ts": `export default { dialect: "postgresql", schema: "./db/a.ts" };`,
		"db/a.ts":           "import { integer, pgTable, text } from \"drizzle-orm/pg-core\";\nexport const team = pgTable(\"team\", { id: integer(\"id\").primaryKey(), name: text(\"name\") });\n",
	})
	table := contract.Node{Identity: "table:migrations/1_a.sql#public.team", Kind: "table", Name: "team", Attributes: map[string]any{"set": "migrations", "state": "complete"}}
	col := contract.Node{Identity: "column:migrations/1_a.sql#public.team.id", Kind: "column", Name: "id"}
	in.Migrations = []contract.Node{table, col}
	in.Edges = []contract.Edge{{Kind: "contains", Source: table.Identity, Target: col.Identity}}
	nodes, _, diags := Link(in)
	if !hasIdentity(nodes, "table:db/a.ts#team") || hasIdentity(nodes, "column:migrations/1_a.sql#public.team.name") {
		t.Fatalf("nodes: %v", nodes)
	}
	if !hasDiag(diags, "schema_model_migration_overlap", "db/a.ts", "model table team", "table:migrations/1_a.sql#public.team", "columns only in the model: [name]; only in the migration: []") {
		t.Errorf("no overlap diagnostic: %v", diagLines(diags))
	}
}
