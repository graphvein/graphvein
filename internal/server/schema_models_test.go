package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/diesel"
	"github.com/graphvein/graphvein/internal/store"
)

// modelsRoot indexes a copy of the sqlmodel-link fixture as "fixture".
func modelsRoot(t *testing.T, w store.Store) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/sqlmodel-link")); err != nil {
		t.Fatal(err)
	}
	indexRoot(t, root, w)
	return root
}

// TestSchemaModelGroups checks T018f's model group (ADR 0055 option 3A,
// G3): model tables form their own group after the migration set, never
// merged into it; the summary counts the group; a model table answers
// /schema/table.
func TestSchemaModelGroups(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelsRoot(t, w)
		var body setsBody
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		if len(body.Sets) != 2 || body.Sets[0].Set == nil || *body.Sets[0].Set != "migrations" || len(body.Sets[0].Tables) != 1 {
			t.Fatalf("sets %+v", body.Sets)
		}
		if g := body.Sets[1]; g.Set == nil || *g.Set != "models/sqlmodel/." || g.Source == nil || *g.Source != "models" || len(g.Tables) != 19 || len(g.Migrations) != 0 {
			t.Fatalf("model group %+v", g)
		}
		var summary schemaSummaryResponse
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/summary", nil), &summary)
		if summary.Sets == nil || *summary.Sets != 1 || summary.ModelGroups == nil || *summary.ModelGroups != 1 || summary.Found != foundSets {
			t.Fatalf("summary %+v", summary)
		}
		if !reflect.DeepEqual(summary.SourcesRead, []string{"migrations", "models"}) {
			t.Fatalf("sources_read %v", summary.SourcesRead)
		}
		rec := get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/table?id="+url.QueryEscape("table:app/models.py#Team"), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("model table: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// overlapDiagnostic reads the table name and migration set of an L5
// diagnostic (oracle only: the server never parses messages).
var overlapDiagnostic = regexp.MustCompile(`^(\S+):\d+: model table (\S+) \(class \S+\) has the name of table (\S+) of migration set (\S+);`)

// TestSchemaOverlapOracle checks the server's model/migration pairs on the
// sqlmodel-link fixture against the stored schema_model_migration_overlap
// diagnostics (T018f 1.4) from the model side; the migration side is null
// here because the fixture's model list may be incomplete.
func TestSchemaOverlapOracle(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelsRoot(t, w)
		snapshot, err := w.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		for _, d := range snapshot.Diagnostics {
			if d.Code != "schema_model_migration_overlap" {
				continue
			}
			m := overlapDiagnostic.FindStringSubmatch(d.Message)
			if m == nil {
				t.Fatalf("diagnostic %q", d.Message)
			}
			want = append(want, m[2]+" "+m[3]+" "+m[4])
		}
		var body setsBody
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		got, back := []string{}, []string{}
		for _, g := range body.Sets {
			for _, tb := range g.Tables {
				// The fixture's model group is may_be_incomplete, so a
				// migration table's list is null (F6 decision a); the
				// other direction is checked by TestSchemaMigrationOverlapsNeedModelLists.
				if g.Source != nil && *g.Source == sourceMigrations && tb.Overlaps != nil {
					t.Errorf("migration table %s overlaps %v, want null", tb.Node.Name, tb.Overlaps)
				}
				for _, o := range tb.Overlaps {
					if o.Source == "migrations" {
						got = append(got, tb.Node.Name+" "+o.Table+" "+*o.Set)
					} else {
						back = append(back, tb.Node.Name+" "+tb.Node.Identity+" "+*g.Set)
					}
				}
			}
		}
		sort.Strings(want)
		sort.Strings(got)
		sort.Strings(back)
		if len(want) == 0 || !reflect.DeepEqual(got, want) || len(back) != 0 {
			t.Errorf("overlaps %v, back %v, diagnostics %v", got, back, want)
		}
	})
}

// TestSchemaPreBumpSnapshot checks the crafted snapshot, whose records
// predate the provenance attributes: claims, rests_on and opaque_sites are
// null, never [] (T018f compatibility rule).
func TestSchemaPreBumpSnapshot(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestSchemaStates(t, w)
		opt := testOptions(t.TempDir(), s)
		opt.Repository = "states"
		var body setsBody
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &body)
		for _, g := range body.Sets {
			if g.Set != nil && g.RestsOn != nil {
				t.Errorf("set %s: rests_on %v", *g.Set, g.RestsOn)
			}
			for _, tb := range g.Tables {
				if tb.RestsOn != nil || tb.OpaqueSites != nil {
					t.Errorf("%s: rests_on %v opaque_sites %v", tb.Node.Identity, tb.RestsOn, tb.OpaqueSites)
				}
			}
		}
		rec := get(t, opt, http.MethodGet, schemaPath("table", "table:a/migrations/2_more.sql#t_partial_empty"), nil)
		var table map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &table); err != nil {
			t.Fatal(err)
		}
		if table["rests_on"] != nil || table["opaque_sites"] != nil {
			t.Errorf("table rests_on %v opaque_sites %v", table["rests_on"], table["opaque_sites"])
		}
		for _, c := range table["columns"].([]any) {
			if claims, present := c.(map[string]any)["claims"]; !present || claims != nil {
				t.Errorf("column claims %v", claims)
			}
		}
	})
}

func span(line, column int) map[string]any {
	return map[string]any{"start_line": line, "start_column": column, "end_line": line, "end_column": column + 5}
}

// TestTableOpaqueSites covers the per-table list: sites after the CREATE,
// down migrations skipped, null on a length mismatch or a missing list.
func TestTableOpaqueSites(t *testing.T) {
	m1 := contract.Node{Identity: "m1", Attributes: map[string]any{"opaque_sites": []any{
		map[string]any{"span": span(1, 0), "row": "O1", "kind": "do"},
		map[string]any{"span": span(9, 0), "row": "O2", "kind": "extension"},
	}}}
	down := contract.Node{Identity: "m1d", Attributes: map[string]any{"applied": false, "opaque_sites": nil}}
	m2 := contract.Node{Identity: "m2", Attributes: map[string]any{"opaque_sites": []any{map[string]any{"span": span(2, 0), "row": "O1", "kind": "dml"}}}}
	creator := "m1"
	table := func(after int) contract.Node {
		return contract.Node{Span: &contract.Span{StartLine: 5}, Attributes: map[string]any{"opaque_after": after}}
	}
	sites := tableOpaqueSites(table(2), &creator, []contract.Node{m1, down, m2})
	if len(sites) != 2 || sites[0].Kind != "extension" || sites[1].Migration != "m2" {
		t.Fatalf("sites %+v", sites)
	}
	if got := tableOpaqueSites(table(3), &creator, []contract.Node{m1, down, m2}); got != nil {
		t.Errorf("length mismatch: %+v", got)
	}
	m2.Attributes["opaque_sites"] = nil
	if got := tableOpaqueSites(table(1), &creator, []contract.Node{m1, m2}); got != nil {
		t.Errorf("missing list: %+v", got)
	}
	if got := tableOpaqueSites(table(0), nil, []contract.Node{m1}); got != nil {
		t.Errorf("no creator: %+v", got)
	}
	if got := tableOpaqueSites(table(0), &creator, []contract.Node{{Identity: "m1", Attributes: map[string]any{"opaque_sites": []any{}}}}); got == nil || len(got) != 0 {
		t.Errorf("proven empty: %+v", got)
	}
}

// TestModelTableList covers the model table_list rule per reason.
func TestModelTableList(t *testing.T) {
	tables := []contract.Node{{File: "a.py", Attributes: map[string]any{"source": "model", "tool": "sqlmodel", "assumptions": []any{"V1"}, "pins": map[string]any{"sqlmodel": "0.0.39"}}}}
	diag := func(code, message string) contract.Diagnostic {
		return contract.Diagnostic{File: "b.py", Code: code, Message: message}
	}
	both := []string{sourceMigrations, sourceModels}
	for _, tc := range []struct {
		name    string
		diags   []contract.Diagnostic
		unfiled []contract.Diagnostic
		read    []string
		list    any
		reasons []string
	}{
		{"complete", nil, nil, both, "complete", []string{}},
		{"not read", nil, nil, []string{sourceMigrations}, nil, []string{}},
		{"unread", []contract.Diagnostic{diag(codeModelUnread, "b.py: 2 model classes present, not read")}, nil, both, "may_be_incomplete", []string{"model_unread"}},
		{"core tables", []contract.Diagnostic{diag(codeModelCoreTables, "b.py: 1 calls named Table")}, nil, both, "may_be_incomplete", []string{"model_core_tables"}},
		{"count unreadable", []contract.Diagnostic{diag(codeModelUnread, "b.py: some classes")}, nil, both, "may_be_incomplete", []string{"model_counts_unknown"}},
		{"not claimed", []contract.Diagnostic{diag(codeModelNotClaimed, "b.py:3: class X")}, nil, both, "may_be_incomplete", []string{"model_not_claimed"}},
		// T018f Revision 2 (B1): a .py file the index did not read, or a
		// scanner gap that may hide one, may define tables.
		{"python too large", []contract.Diagnostic{diag("file_too_large", "file exceeds 10 byte parsing limit")}, nil, both, "may_be_incomplete", []string{"python_unread"}},
		{"python parse failed", []contract.Diagnostic{diag("parse_failed", "parsing b.py: x")}, nil, both, "may_be_incomplete", []string{"python_unread"}},
		{"python extract failed", []contract.Diagnostic{diag("extract_failed", "x")}, nil, both, "may_be_incomplete", []string{"python_unread"}},
		{"python not read by scanner", nil, []contract.Diagnostic{{File: "c.py", Code: "file_read"}}, both, "may_be_incomplete", []string{"python_unread"}},
		{"unknown type not read", nil, []contract.Diagnostic{{File: "bin/run", Code: "file_read"}}, both, "may_be_incomplete", []string{"python_scan_gap"}},
		{"other type not read", nil, []contract.Diagnostic{{File: "a.rs", Code: "file_read"}}, both, "complete", []string{}},
		{"directory not listed", nil, []contract.Diagnostic{{File: "pkg", Code: "scan_read"}}, both, "may_be_incomplete", []string{"python_scan_gap"}},
		{"symlink", nil, []contract.Diagnostic{{File: "link", Code: "symlink"}}, both, "may_be_incomplete", []string{"python_scan_gap"}},
		{"ignore file", nil, []contract.Diagnostic{{File: ".gitignore", Code: "ignore_read"}}, both, "may_be_incomplete", []string{"python_scan_gap"}},
		{"invalid path", nil, []contract.Diagnostic{{Code: "invalid_path"}}, both, "may_be_incomplete", []string{"python_scan_gap"}},
		{"too large other type", []contract.Diagnostic{{File: "b.rs", Code: "file_too_large"}}, nil, both, "complete", []string{}},
		{"import failure", []contract.Diagnostic{diag(codeModelImportFailure, "b.py:3: class X")}, nil, both, "may_be_incomplete", []string{"model_not_claimed"}},
		// T018g2 review: a TypeScript linker's marker belongs to its own
		// group (list marker), never to SQLModel's.
		{"typescript marker", []contract.Diagnostic{{File: "s.ts", Code: codeModelUnread, Message: "s.ts: 1 exported value(s) not read" + modelListMarker("models/drizzle/drizzle.config.ts")}}, nil, both, "complete", []string{}},
	} {
		s := schemaSet{DiagnosticCounts: map[string]int{}, Conflicts: []string{}}
		modelGroupState(&s, tables, tc.diags, tc.unfiled, tc.read)
		var list any
		if s.TableList != nil {
			list = *s.TableList
		}
		if list != tc.list || !reflect.DeepEqual(s.TableListReasons, tc.reasons) {
			t.Errorf("%s: table_list %v %v", tc.name, list, s.TableListReasons)
		}
	}
}

// TestModelGroupKeys checks the G3 group keys (T018g0): model tables group
// by their stored set models/<tool>/<root>, one group per tool and root,
// each with its own tool and pins; a group whose tool is not sqlmodel does
// not take the Python files' schema_model_ diagnostics.
func TestModelGroupKeys(t *testing.T) {
	table := func(file, set, tool string) contract.Node {
		return contract.Node{File: file, Attributes: map[string]any{"source": "model", "set": set, "tool": tool, "assumptions": []any{"V1"}, "pins": map[string]any{tool: "1"}}}
	}
	tables := []contract.Node{
		table("app/models.py", "models/sqlmodel/.", "sqlmodel"),
		table("prisma/schema.prisma", "models/prisma/prisma/schema.prisma", "prisma"),
		table("web/prisma/schema.prisma", "models/prisma/web/prisma/schema.prisma", "prisma"),
		table("app/more.py", "models/sqlmodel/.", "sqlmodel"),
		{File: "migrations/1.sql", Attributes: map[string]any{"set": "migrations"}},
	}
	groups := modelGroupsOf(tables)
	if len(groups) != 3 || len(groups[2]) != 2 || groups[0][0].File != "prisma/schema.prisma" || groups[1][0].File != "web/prisma/schema.prisma" {
		t.Fatalf("groups %v", groups)
	}
	unread := []contract.Diagnostic{{File: "app/other.py", Code: codeModelUnread, Message: "app/other.py: 2 model classes present, not read"}}
	both := []string{sourceMigrations, sourceModels}
	for _, g := range groups {
		s := schemaSet{DiagnosticCounts: map[string]int{}, Conflicts: []string{}}
		modelGroupState(&s, g, unread, nil, both)
		tool := *s.Tool
		if !reflect.DeepEqual(s.schemaModelFields.Pins, map[string]any{tool: "1"}) {
			t.Errorf("%s: pins %v", tool, s.schemaModelFields.Pins)
		}
		if got := s.DiagnosticCounts[codeModelUnread]; tool == "sqlmodel" && got != 1 || tool != "sqlmodel" && got != 0 {
			t.Errorf("%s: %d unread diagnostics", tool, got)
		}
	}
}

// TestOverlapsOf covers both directions and the column lists, null when
// one side's columns are not proven.
func TestOverlapsOf(t *testing.T) {
	model := contract.Node{Identity: "table:m.py#Team", Attributes: map[string]any{"source": "model", "set": "models/sqlmodel/.", "name": "team", "column_set": "complete"}}
	migration := contract.Node{Identity: "table:m/1.sql#team", Attributes: map[string]any{"set": "m", "name": "team", "state": "complete"}}
	other := contract.Node{Identity: "table:m/1.sql#hero", Attributes: map[string]any{"set": "m", "name": "hero", "state": "complete"}}
	cols := map[string][]string{model.Identity: {"id", "label"}, migration.Identity: {"id", "created"}}
	all := []contract.Node{model, migration, other}
	names := func(id string) []string { return cols[id] }
	a := overlapsOf(model, all, names)
	b := overlapsOf(migration, all, names)
	if len(a) != 1 || a[0].Table != migration.Identity || a[0].Source != "migrations" || !reflect.DeepEqual(a[0].ColumnsOnlyHere, []string{"label"}) || !reflect.DeepEqual(a[0].ColumnsOnlyThere, []string{"created"}) {
		t.Errorf("model side %+v", a)
	}
	if len(b) != 1 || b[0].Table != model.Identity || b[0].Source != "models" || !reflect.DeepEqual(b[0].ColumnsOnlyHere, []string{"created"}) {
		t.Errorf("migration side %+v", b)
	}
	if got := overlapsOf(other, all, names); got == nil || len(got) != 0 {
		t.Errorf("no pair: %+v", got)
	}
	model.Attributes["column_set"] = "partial"
	all[0] = model
	for _, o := range append(overlapsOf(model, all, names), overlapsOf(migration, all, names)...) {
		if o.ColumnsOnlyHere != nil || o.ColumnsOnlyThere != nil {
			t.Errorf("partial side: %+v", o)
		}
	}
}

// TestSourcesRead covers sources_read per version.
func TestSourcesRead(t *testing.T) {
	run := func(name, version string) *lastRun {
		return &lastRun{Indexer: contract.Indexer{Name: name, Version: version}}
	}
	for _, tc := range []struct {
		run  *lastRun
		want []string
	}{
		{nil, nil},
		{run("crafted", "extractors-30"), nil},
		{run("graphvein", "dev"), nil},
		{run("graphvein", "extractors-26"), []string{}},
		{run("graphvein", "extractors-27"), []string{}},
		{run("graphvein", "extractors-28"), []string{"migrations"}},
		{run("graphvein", "extractors-29"), []string{"migrations", "models"}},
		{run("graphvein", "extractors-30"), []string{"migrations", "models"}},
		{run("graphvein", "extractors-31"), []string{"migrations", "models"}},
	} {
		if got := sourcesRead(tc.run); !reflect.DeepEqual(got, tc.want) || (got == nil) != (tc.want == nil) {
			t.Errorf("%+v: %v, want %v", tc.run, got, tc.want)
		}
	}
}

// TestUnreadTools covers the Alembic marker rule.
func TestUnreadTools(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	for _, tc := range []struct {
		name  string
		files []string
		want  []schemaUnreadTool
	}{
		{"default layout", []string{"backend/alembic.ini", "backend/app/alembic/env.py", "backend/app/alembic/script.py.mako", "backend/app/alembic/versions/1_a.py", "backend/app/alembic/versions/2_b.py", "backend/app/alembic/versions/__init__.py"},
			[]schemaUnreadTool{{Tool: "alembic", Path: str("backend/app/alembic"), Marker: str("backend/alembic.ini"), Files: n(2)}}},
		{"versions without mako, marker in the directory", []string{"db/env.py", "db/alembic.ini", "db/versions/1.py"},
			[]schemaUnreadTool{{Tool: "alembic", Path: str("db"), Marker: str("db/alembic.ini"), Files: n(1)}}},
		{"mako without versions, no marker", []string{"db/env.py", "db/script.py.mako"},
			[]schemaUnreadTool{{Tool: "alembic", Path: str("db"), Files: n(0)}}},
		{"alembic.ini only", []string{"alembic.ini", "app/main.py"},
			[]schemaUnreadTool{{Tool: "alembic", Marker: str("alembic.ini")}}},
		{"env.py alone", []string{"app/env.py", "app/main.py"}, []schemaUnreadTool{}},
		{"prisma migrate", []string{"prisma/migrations/1_init/migration.sql", "prisma/migrations/2_b/migration.sql", "prisma/migrations/migration_lock.toml", "prisma/schema.prisma"},
			[]schemaUnreadTool{{Tool: "prisma-migrate", Path: str("prisma/migrations"), Marker: str("prisma/migrations/migration_lock.toml"), Files: n(2)}}},
		{"prisma migrate without a lock file, multi-file schema", []string{"db/schema/migrations/1_init/migration.sql", "db/schema/a.prisma"},
			[]schemaUnreadTool{{Tool: "prisma-migrate", Path: str("db/schema/migrations"), Files: n(1)}}},
		{"prisma lock file only", []string{"prisma/migrations/migration_lock.toml", "prisma/schema.prisma"},
			[]schemaUnreadTool{{Tool: "prisma-migrate", Path: str("prisma/migrations"), Marker: str("prisma/migrations/migration_lock.toml"), Files: n(0)}}},
		{"migrations without a schema beside them", []string{"db/migrations/1/migration.sql", "db/migrations/migration_lock.toml", "other/schema.prisma"}, []schemaUnreadTool{}},
		{"a migration.sql directly in migrations", []string{"prisma/migrations/migration.sql", "prisma/schema.prisma"}, []schemaUnreadTool{}},
		{"nothing", nil, []schemaUnreadTool{}},
	} {
		if got := unreadTools(tc.files, nil, nil, nil, nil); !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
}

// TestSchemaPrismaConfigMigrations checks that a Prisma 7 config's literal
// migrations.path reaches unread_tools through the linker's marker
// (T018g1 G1G), on both backends.
func TestSchemaPrismaConfigMigrations(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := prismaRootWith(t, w, map[string]string{
			"prisma.config.ts":                                    "export default defineConfig({ migrations: { path: 'db/history' } })\n",
			"prisma/migrations/migration_lock.toml":               "",
			"prisma/migrations/20260101000000_init/migration.sql": "",
			"db/history/migration_lock.toml":                      "provider = \"postgresql\"\n",
			"db/history/20260101000000_init/migration.sql":        "CREATE TABLE \"User\" (\"id\" INTEGER NOT NULL);\n",
		})
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		path, marker, files := "db/history", "db/history/migration_lock.toml", 1
		want := []schemaUnreadTool{{Tool: "prisma-migrate", Path: &path, Marker: &marker, Files: &files}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}

// TestUnreadToolsDrizzle covers the drizzle-kit rule: a
// folder a Drizzle config names holding meta/_journal.json or a .sql
// file, and the markers it is read from.
func TestUnreadToolsDrizzle(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	drizzle := map[string]bool{"apps/api/drizzle": true, "db/out": true}
	for _, tc := range []struct {
		name  string
		files []string
		want  []schemaUnreadTool
	}{
		{"journal and migrations", []string{"apps/api/drizzle/0000_a.sql", "apps/api/drizzle/0001_b.sql", "apps/api/drizzle/meta/_journal.json", "apps/api/drizzle/meta/0000_snapshot.json"},
			[]schemaUnreadTool{{Tool: "drizzle-kit", Path: str("apps/api/drizzle"), Marker: str("apps/api/drizzle/meta/_journal.json"), Files: n(2)}}},
		{"sql without a journal", []string{"db/out/0000_a.sql"},
			[]schemaUnreadTool{{Tool: "drizzle-kit", Path: str("db/out"), Files: n(1)}}},
		{"journal only", []string{"db/out/meta/_journal.json"},
			[]schemaUnreadTool{{Tool: "drizzle-kit", Path: str("db/out"), Marker: str("db/out/meta/_journal.json"), Files: n(0)}}},
		{"named folder without migrations", []string{"db/out/readme.md", "db/out/nested/0000_a.sql"}, []schemaUnreadTool{}},
		{"a drizzle folder no config names", []string{"drizzle/0000_a.sql", "drizzle/meta/_journal.json"}, []schemaUnreadTool{}},
	} {
		var files []string
		for _, f := range tc.files {
			if alembicPath(f) || drizzlePath(f, drizzle) {
				files = append(files, f)
			}
		}
		if got := unreadTools(files, nil, drizzle, nil, nil); !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
	hashes := map[string]string{"apps/api/drizzle.config.ts": "h", "b/drizzle.config.ts": "h"}
	mark := func(file, msg string) contract.Diagnostic {
		return contract.Diagnostic{File: file, Code: codePrismaMigrations, Severity: "info", Message: msg}
	}
	got := namedMigrations([]contract.Diagnostic{
		mark("apps/api/drizzle.config.ts", "apps/api/drizzle.config.ts: "+drizzleMigrationsMarker+"apps/api/drizzle"),
		mark("b/drizzle.config.ts", "b/drizzle.config.ts: "+drizzleMigrationsMarker+"b/../../x"),
		mark("b/drizzle.config.ts", "b/drizzle.config.ts: "+prismaMigrationsMarker+"b/prisma"),
		mark("gone/drizzle.config.ts", "gone/drizzle.config.ts: "+drizzleMigrationsMarker+"gone/drizzle"),
	}, hashes, drizzleMigrationsMarker)
	if !reflect.DeepEqual(got, map[string]bool{"apps/api/drizzle": true}) {
		t.Errorf("named %v", got)
	}
}

// TestSchemaDrizzleMigrations checks that a Drizzle config's migrations
// folder reaches unread_tools through the linker's marker, on
// both backends: the literal out of one config, the default of another.
func TestSchemaDrizzleMigrations(t *testing.T) {
	schema := "import { integer, pgTable } from \"drizzle-orm/pg-core\";\nexport const team = pgTable(\"team\", { id: integer(\"id\").primaryKey() });\n"
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"apps/api/package.json":           `{"name": "api", "dependencies": {"drizzle-orm": "0.45.4"}, "devDependencies": {"drizzle-kit": "0.31.11"}}`,
			"apps/api/drizzle.config.ts":      "export default { dialect: \"postgresql\", schema: \"./src/schema.ts\", out: \"./sql\" };\n",
			"apps/api/src/schema.ts":          schema,
			"apps/api/sql/0000_init.sql":      "CREATE TABLE \"team\" (\"id\" integer PRIMARY KEY NOT NULL);\n",
			"apps/api/sql/meta/_journal.json": `{"version": "7", "dialect": "postgresql", "entries": []}`,
			"apps/web/package.json":           `{"name": "web", "dependencies": {"drizzle-orm": "0.45.4"}, "devDependencies": {"drizzle-kit": "0.31.11"}}`,
			"apps/web/drizzle.config.ts":      "export default { dialect: \"postgresql\", schema: \"./schema.ts\" };\n",
			"apps/web/schema.ts":              strings.ReplaceAll(schema, "team", "member"),
			"apps/web/drizzle/0000_a.sql":     "CREATE TABLE \"member\" (\"id\" integer PRIMARY KEY NOT NULL);\n",
			"apps/web/drizzle/0001_b.sql":     "SELECT 1;\n",
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		api, journal, web, one, two := "apps/api/sql", "apps/api/sql/meta/_journal.json", "apps/web/drizzle", 1, 2
		want := []schemaUnreadTool{{Tool: "drizzle-kit", Path: &api, Marker: &journal, Files: &one}, {Tool: "drizzle-kit", Path: &web, Files: &two}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}

// TestModelDiagnosticCount covers the count forms: "<file>: <n> ...", one
// declaration ("<file>:<line>: ...", TypeScript linkers) and other shapes.
func TestModelDiagnosticCount(t *testing.T) {
	for _, tc := range []struct {
		msg string
		n   int64
		ok  bool
	}{
		{"a.ts: 2 exported value(s) not read (x, y): the module has no payload", 2, true},
		{"a.ts:12: dynamic: the table name is not a string literal", 1, true},
		{"a.ts:x: odd", 0, false},
		{"b.ts: 3 values", 0, false},
	} {
		n, ok := modelDiagnosticCount(contract.Diagnostic{File: "a.ts", Message: tc.msg})
		if n != tc.n || ok != tc.ok {
			t.Errorf("%q: %d %v, want %d %v", tc.msg, n, ok, tc.n, tc.ok)
		}
	}
}

// TestSchemaDrizzleTableList checks that a Drizzle group's table_list
// reads the Drizzle linker's diagnostics (T018g2 review), on both
// backends: a schema module whose builders come through a local re-export
// has no model payload, and its marker, on a file with no claimed table,
// makes the group may_be_incomplete (model_unread) and lists the file; a
// group without such a marker is complete, and the other group's marker
// does not reach it.
func TestSchemaDrizzleTableList(t *testing.T) {
	schema := "import { integer, pgTable } from \"drizzle-orm/pg-core\";\nexport const team = pgTable(\"team\", { id: integer(\"id\").primaryKey() });\n"
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"apps/api/package.json":      `{"name": "api", "dependencies": {"drizzle-orm": "0.45.4"}, "devDependencies": {"drizzle-kit": "0.31.11"}}`,
			"apps/api/drizzle.config.ts": "export default { dialect: \"postgresql\", schema: \"./src/*.ts\" };\n",
			"apps/api/src/schema.ts":     schema,
			"apps/api/src/core.ts":       "export { integer, pgTable } from \"drizzle-orm/pg-core\";\n",
			"apps/api/src/more.ts":       "import { integer, pgTable } from \"./core\";\nexport const extra = pgTable(\"extra\", { id: integer(\"id\") });\n",
			"apps/web/package.json":      `{"name": "web", "dependencies": {"drizzle-orm": "0.45.4"}, "devDependencies": {"drizzle-kit": "0.31.11"}}`,
			"apps/web/drizzle.config.ts": "export default { dialect: \"postgresql\", schema: \"./schema.ts\" };\n",
			"apps/web/schema.ts":         strings.ReplaceAll(schema, "team", "member"),
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			Sets []struct {
				Set              *string           `json:"set"`
				TableList        *string           `json:"table_list"`
				TableListReasons []string          `json:"table_list_reasons"`
				Files            []schemaModelFile `json:"files"`
			} `json:"sets"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		got := map[string]string{}
		for _, set := range body.Sets {
			if set.Set == nil || set.TableList == nil {
				continue
			}
			files, _ := json.Marshal(set.Files)
			got[*set.Set] = *set.TableList + " " + strings.Join(set.TableListReasons, ",") + " " + string(files)
		}
		want := map[string]string{
			"models/drizzle/apps/api/drizzle.config.ts": `may_be_incomplete model_unread [{"file":"apps/api/src/more.ts","tables":0,"unread":1,"core_tables":0},{"file":"apps/api/src/schema.ts","tables":1,"unread":0,"core_tables":0}]`,
			"models/drizzle/apps/web/drizzle.config.ts": `complete  [{"file":"apps/web/schema.ts","tables":1,"unread":0,"core_tables":0}]`,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("sets %v", got)
		}
	})
}

// TestUnreadToolsNamed covers the directories a Prisma 7 config's literal
// migrations.path names (T018g1 G1G) and the markers they are read from.
func TestUnreadToolsNamed(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	named := map[string]bool{"db/history": true}
	for _, tc := range []struct {
		name  string
		files []string
		want  []schemaUnreadTool
	}{
		{"named directory away from the schema", []string{"db/history/1_init/migration.sql", "db/history/migration_lock.toml", "prisma/schema.prisma"},
			[]schemaUnreadTool{{Tool: "prisma-migrate", Path: str("db/history"), Marker: str("db/history/migration_lock.toml"), Files: n(1)}}},
		{"named directory and the default beside the schema", []string{"db/history/1_init/migration.sql", "prisma/migrations/1_init/migration.sql", "prisma/schema.prisma"},
			[]schemaUnreadTool{{Tool: "prisma-migrate", Path: str("db/history"), Files: n(1)}, {Tool: "prisma-migrate", Path: str("prisma/migrations"), Files: n(1)}}},
		{"named directory without migrations", []string{"db/history/readme.md", "prisma/schema.prisma"}, []schemaUnreadTool{}},
		{"a migration.sql directly in the named directory", []string{"db/history/migration.sql"}, []schemaUnreadTool{}},
	} {
		if got := unreadTools(tc.files, named, nil, nil, nil); !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
	hashes := map[string]string{"app/prisma.config.ts": "h", "b/prisma.config.ts": "h", "c/prisma.config.ts": "h"}
	mark := func(file, msg string) contract.Diagnostic {
		return contract.Diagnostic{File: file, Code: codePrismaMigrations, Severity: "info", Message: msg}
	}
	got := namedMigrations([]contract.Diagnostic{
		mark("app/prisma.config.ts", "app/prisma.config.ts: "+prismaMigrationsMarker+"app/db/migrations"),
		mark("b/prisma.config.ts", "b/prisma.config.ts: "+prismaMigrationsMarker+"../x"),
		mark("c/prisma.config.ts", "c/prisma.config.ts: another shape"),
		mark("gone/prisma.config.ts", "gone/prisma.config.ts: "+prismaMigrationsMarker+"gone/m"),
		{File: "app/prisma.config.ts", Code: "schema_model_unread", Message: "app/prisma.config.ts: " + prismaMigrationsMarker + "other"},
		mark("app/prisma.config.ts", "app/prisma.config.ts: "+drizzleMigrationsMarker+"app/drizzle"),
	}, hashes, prismaMigrationsMarker)
	if !reflect.DeepEqual(got, map[string]bool{"app/db/migrations": true}) {
		t.Errorf("named %v", got)
	}
}

// TestSchemaAssumptionsCatalogue checks the catalogue's coverage: every
// migration row and precondition, every model row, the folded entries, and
// no V5.
func TestSchemaAssumptionsCatalogue(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		var body schemaAssumptionsResponse
		decodeInto(t, get(t, testOptions(t.TempDir(), s), http.MethodGet, "/api/v0/schema/assumptions", nil), &body)
		ids := map[string]*string{}
		for _, a := range body.Assumptions {
			ids[a.ID] = a.FoldedInto
		}
		for _, id := range []string{"E1", "E2", "E3", "E4", "E5", "E6", "E8", "V1", "V2", "V4", "V6", "V7"} {
			if f, ok := ids[id]; !ok || f != nil {
				t.Errorf("%s: listed %v folded %v", id, ok, f)
			}
		}
		if f := ids["E7"]; f == nil || *f != "E8" {
			t.Errorf("E7 folded into %v", f)
		}
		if f := ids["V3"]; f == nil || *f != "V7" {
			t.Errorf("V3 folded into %v", f)
		}
		if _, ok := ids["V5"]; ok {
			t.Error("V5 is listed")
		}
		if r := body.Rows["migrations"]["M4"]; !reflect.DeepEqual(r.Assumptions, []string{"E1", "E2", "E4", "E5", "E6", "E8"}) {
			t.Errorf("M4 %+v", r)
		}
		if r := body.Rows["models/sqlmodel"]["K1b"]; !reflect.DeepEqual(r.Assumptions, []string{"V1", "V4", "V6", "V7"}) {
			t.Errorf("K1b %+v", r)
		}
		if _, ok := body.Rows["migrations"]["M5a.P1"]; !ok {
			t.Error("precondition M5a.P1 missing")
		}
	})
}

// TestSchemaAssumptionsPerTool checks that model rows are keyed per tool
// (T018g6 Revision 3 addendum 3, OP2): every registered tool's rows are
// served under models/<tool> with that tool named, and each tool's default
// row D keeps its own text, so SQLModel's D never replaces TypeORM's (or
// the reverse).
func TestSchemaAssumptionsPerTool(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		var body schemaAssumptionsResponse
		decodeInto(t, get(t, testOptions(t.TempDir(), s), http.MethodGet, "/api/v0/schema/assumptions", nil), &body)
		if _, ok := body.Rows["models"]; ok {
			t.Error("model rows are served under the shared models key")
		}
		for _, c := range models.Catalogues() {
			rows, ok := body.Rows["models/"+c.Tool]
			if !ok || len(rows) != len(c.Rows) {
				t.Errorf("%s: %d rows served, the catalogue has %d", c.Tool, len(rows), len(c.Rows))
				continue
			}
			for _, r := range c.Rows {
				got, ok := rows[r.ID]
				if !ok || got.Tool == nil || *got.Tool != c.Tool || got.Source != "models" || got.Summary != plainText(r.Summary) {
					t.Errorf("%s %s: served %+v", c.Tool, r.ID, got)
				}
			}
		}
		sq, to := body.Rows["models/sqlmodel"]["D"], body.Rows["models/typeorm"]["D"]
		if sq.Summary == "" || to.Summary == "" || sq.Summary == to.Summary || !strings.Contains(to.Summary, "typeorm") {
			t.Errorf("D rows: sqlmodel %q, typeorm %q", sq.Summary, to.Summary)
		}
	})
}

// TestSetsModelClaims checks T018g0 Revision 1: /schema/sets serves a model
// column without its claims and the table's claim counts by verification;
// a migration column keeps its claims. Since ADR 0057 the counts are the
// table record's stored verification_counts, served next to the node and
// not in it; a table record without verification (an index before
// extractors-31: Revision 2) or without well-formed counts (before ADR
// 0057) makes them null.
func TestSetsModelClaims(t *testing.T) {
	claim := func(v any) map[string]any { return map[string]any{"fact": "type", "row": "F5", "verification": v} }
	model := contract.Node{Identity: "column:a.py#A.x", Kind: "column", Attributes: map[string]any{"source": "model", "type": "Integer", "claims": []any{claim("oracle"), claim("mapped"), claim(nil)}}}
	migration := contract.Node{Identity: "column:m#t.x", Kind: "column", Attributes: map[string]any{"claims": []any{claim(nil)}}}

	stored := map[string]any{"oracle": json.Number("1"), "mapped": json.Number("2")}
	table := contract.Node{Identity: "table:a.py#A", Kind: "table", Attributes: map[string]any{"source": "model", "verification": "oracle", "verification_counts": stored}}
	if got, want := verificationCounts(table), map[string]int{"oracle": 1, "mapped": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("counts %v, want %v", got, want)
	}
	empty := contract.Node{Identity: "table:a.py#E", Kind: "table", Attributes: map[string]any{"source": "model", "verification": "oracle", "verification_counts": map[string]any{}}}
	if got := verificationCounts(empty); got == nil || len(got) != 0 {
		t.Errorf("no claims: counts %v, want {}", got)
	}
	for name, attrs := range map[string]map[string]any{
		"no verification (before extractors-31)": {"source": "model", "verification_counts": stored},
		"no counts (before ADR 0057)":            {"source": "model", "verification": "oracle"},
		"counts not an object":                   {"source": "model", "verification": "oracle", "verification_counts": []any{}},
		"count not an integer":                   {"source": "model", "verification": "oracle", "verification_counts": map[string]any{"oracle": json.Number("1.5")}},
		"negative count":                         {"source": "model", "verification": "oracle", "verification_counts": map[string]any{"oracle": json.Number("-1")}},
	} {
		if got := verificationCounts(contract.Node{Identity: "table:a.py#B", Kind: "table", Attributes: attrs}); got != nil {
			t.Errorf("%s: counts %v, want null", name, got)
		}
	}
	shown := servedTable(table)
	if _, has := shown.Attributes["verification_counts"]; has || shown.Attributes["verification"] != "oracle" {
		t.Errorf("served table %v", shown.Attributes)
	}
	if _, has := table.Attributes["verification_counts"]; !has {
		t.Error("servedTable changed the stored record")
	}

	served := setColumnNode(model)
	if _, has := served.Attributes["claims"]; has || served.Attributes["type"] != "Integer" {
		t.Errorf("served model column %v", served.Attributes)
	}
	if _, has := model.Attributes["claims"]; !has {
		t.Error("setColumnNode changed the stored record")
	}
	if got := setColumnNode(migration); !reflect.DeepEqual(got, migration) {
		t.Errorf("migration column %v", got.Attributes)
	}
}

// prismaRoot indexes a small Prisma repository as "fixture": a postgresql
// schema with relationMode "prisma" and a Prisma Migrate directory beside
// it (migration_lock.toml and one migration.sql) that no pass reads.
func prismaRoot(t *testing.T, w store.Store) string {
	t.Helper()
	return prismaRootWith(t, w, nil)
}

// prismaRootWith is prismaRoot with extra files (path to content; an empty
// content removes the file).
func prismaRootWith(t *testing.T, w store.Store, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"package.json":                                        `{"name": "app", "devDependencies": {"prisma": "7.10.0"}}`,
		"package-lock.json":                                   `{"name": "app", "lockfileVersion": 3, "packages": {"": {"name": "app"}, "node_modules/prisma": {"version": "7.10.0"}}}`,
		"prisma/schema.prisma":                                "datasource db {\n  provider     = \"postgresql\"\n  relationMode = \"prisma\"\n}\n\nmodel User {\n  id    Int    @id\n  posts Post[]\n}\n\nmodel Post {\n  id     Int  @id\n  userId Int\n  user   User @relation(fields: [userId], references: [id])\n}\n",
		"prisma/migrations/migration_lock.toml":               "provider = \"postgresql\"\n",
		"prisma/migrations/20260101000000_init/migration.sql": "CREATE TABLE \"User\" (\"id\" INTEGER NOT NULL);\n",
	}
	for name, body := range extra {
		if body == "" {
			delete(files, name)
		} else {
			files[name] = body
		}
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	indexRoot(t, root, w)
	return root
}

// TestSchemaPrismaGroup checks the T018g1 review fixes on the server: the
// Prisma group serves the schema's provider as its dialect (C3), and
// Prisma Migrate's directory is an unread tool in /sets and the summary
// (C5), not a migration set and not "no migrations".
func TestSchemaPrismaGroup(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := prismaRoot(t, w)
		var body struct {
			setsBody
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		if len(body.Sets) != 1 {
			t.Fatalf("sets %+v", body.Sets)
		}
		g := body.Sets[0]
		if g.Set == nil || *g.Set != "models/prisma/prisma/schema.prisma" || g.Dialect == nil || *g.Dialect != "postgresql" {
			t.Fatalf("prisma group %+v", g)
		}
		path, marker, files := "prisma/migrations", "prisma/migrations/migration_lock.toml", 1
		want := []schemaUnreadTool{{Tool: "prisma-migrate", Path: &path, Marker: &marker, Files: &files}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
		var summary schemaSummaryResponse
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/summary", nil), &summary)
		if !reflect.DeepEqual(summary.UnreadTools, want) || summary.Sets == nil || *summary.Sets != 0 {
			b, _ := json.Marshal(summary)
			t.Fatalf("summary %s", b)
		}
	})
}

// TestUnreadToolsSequelize covers the Sequelize rule (T018g3,
// row SQ-U1): a folder the linker marks, counted by its script files at
// any depth.
func TestUnreadToolsSequelize(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	named := map[string]bool{"server/migrations": true, "migrations": true}
	for _, tc := range []struct {
		name  string
		files []string
		want  []schemaUnreadTool
	}{
		{"scripts at any depth", []string{"server/migrations/1-a.js", "server/migrations/old/2-b.ts", "server/migrations/readme.md"},
			[]schemaUnreadTool{{Tool: "sequelize-migrations", Path: str("server/migrations"), Files: n(2)}}},
		{"two folders", []string{"migrations/1.cjs", "server/migrations/1.mjs"},
			[]schemaUnreadTool{{Tool: "sequelize-migrations", Path: str("migrations"), Files: n(1)}, {Tool: "sequelize-migrations", Path: str("server/migrations"), Files: n(1)}}},
		{"a folder without scripts", []string{"server/migrations/readme.md"}, []schemaUnreadTool{}},
		{"a folder no package marks", []string{"web/migrations/1.js"}, []schemaUnreadTool{}},
	} {
		var files []string
		for _, f := range tc.files {
			if sequelizeDir(f, named) != "" {
				files = append(files, f)
			}
		}
		if got := unreadTools(files, nil, nil, named, nil); !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
}

// TestSchemaSequelizeMigrations checks that a Sequelize package's
// migrations folder reaches unread_tools through the linker's marker,
// on both backends.
func TestSchemaSequelizeMigrations(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"server/package.json":                `{"name": "api", "dependencies": {"sequelize": "6.37.8", "sqlite3": "5.1.7"}}`,
			"server/models/index.js":             "const { Sequelize, DataTypes } = require('sequelize');\nconst sequelize = new Sequelize({ dialect: 'sqlite' });\nconst Team = sequelize.define('Team', { name: DataTypes.STRING });\nmodule.exports = { sequelize, Team };\n",
			"server/migrations/20240101-team.js": "module.exports = { up: async () => {}, down: async () => {} };\n",
			"server/migrations/20240102-user.js": "module.exports = { up: async () => {}, down: async () => {} };\n",
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		dir, two := "server/migrations", 2
		want := []schemaUnreadTool{{Tool: "sequelize-migrations", Path: &dir, Files: &two}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}

// TestUnreadToolsGormV1 checks the GORM v1 rule (T018g5, row
// GO-U1): the GORM linker's markers on stored files give one gorm-v1 entry
// per package directory with the count of its marked files; a marker of
// another shape, on another file or on a file that is not stored names
// nothing.
func TestUnreadToolsGormV1(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	diag := func(file, msg string) contract.Diagnostic {
		return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: codeModelUnread, Message: msg}
	}
	hashes := map[string]string{"legacy/a.go": "1", "legacy/b.go": "2", "main.go": "3", "other/c.go": "4"}
	marks := []contract.Diagnostic{
		diag("legacy/a.go", "legacy/a.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read"),
		diag("legacy/a.go", "legacy/a.go:4: github.com/jinzhu/gorm/dialects/postgres import (GORM v1): found, not read"),
		diag("legacy/b.go", "legacy/b.go:5: github.com/jinzhu/gorm import (GORM v1): found, not read"),
		diag("main.go", "main.go:7: github.com/jinzhu/gorm import (GORM v1): found, not read"),
		diag("other/c.go", "legacy/a.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read"),
		diag("other/c.go", "other/c.go:3: a polymorphic relation is not read"),
		diag("gone.go", "gone.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read"),
	}
	got := withGormV1([]schemaUnreadTool{{Tool: "alembic", Path: str("migrations"), Files: n(1)}}, gormV1Files(marks, hashes))
	want := []schemaUnreadTool{
		{Tool: "gorm-v1", Path: str("."), Files: n(1)},
		{Tool: "gorm-v1", Path: str("legacy"), Files: n(2)},
		{Tool: "alembic", Path: str("migrations"), Files: n(1)},
	}
	if !reflect.DeepEqual(got, want) {
		b, _ := json.Marshal(got)
		t.Errorf("unread_tools %s", b)
	}
	if got := withGormV1([]schemaUnreadTool{}, nil); len(got) != 0 {
		t.Errorf("no markers: %v", got)
	}
}

// TestSchemaGormV1 checks that a GORM v1 import reaches unread_tools
// through the GORM linker's marker, on both backends.
func TestSchemaGormV1(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"go.mod":         "module example.com/legacy\n\ngo 1.22\n\nrequire github.com/jinzhu/gorm v1.9.16\n",
			"store/store.go": "package store\n\nimport \"github.com/jinzhu/gorm\"\n\n// Item is a GORM v1 model.\ntype Item struct {\n\tgorm.Model\n\tName string\n}\n\n// Migrate creates the table.\nfunc Migrate(db *gorm.DB) { db.AutoMigrate(&Item{}) }\n",
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		dir, one := "store", 1
		want := []schemaUnreadTool{{Tool: "gorm-v1", Path: &dir, Files: &one}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}

// TestUnreadToolsDiesel covers the Diesel rule (T018g4, row
// DI-U1): a directory the linker marks, counted by its migration
// directories with an up.sql (dot entries skipped), and the marker text
// the server reads equals the linker's.
func TestUnreadToolsDiesel(t *testing.T) {
	if dieselMigrationsMarker != diesel.MigrationsMarker {
		t.Fatalf("server marker %q, linker marker %q", dieselMigrationsMarker, diesel.MigrationsMarker)
	}
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	named := map[string]bool{"migrations": true, "db/migrations": true}
	for _, tc := range []struct {
		name  string
		files []string
		want  []schemaUnreadTool
	}{
		{"migration directories", []string{"migrations/2024-01-01-000000_a/up.sql", "migrations/2024-01-01-000000_a/down.sql", "migrations/2024-01-02-000000_b/up.sql", "migrations/.keep/up.sql", "migrations/up.sql", "migrations/x/y/up.sql"},
			[]schemaUnreadTool{{Tool: "diesel-migrations", Path: str("migrations"), Files: n(2)}}},
		{"two directories", []string{"db/migrations/1_a/up.sql", "migrations/1_a/up.sql"},
			[]schemaUnreadTool{{Tool: "diesel-migrations", Path: str("db/migrations"), Files: n(1)}, {Tool: "diesel-migrations", Path: str("migrations"), Files: n(1)}}},
		{"a directory no config marks", []string{"other/1_a/up.sql"}, []schemaUnreadTool{}},
	} {
		var files []string
		for _, f := range tc.files {
			if dieselDir(f, named) != "" {
				files = append(files, f)
			}
		}
		if got := unreadTools(files, nil, nil, nil, named); !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
}

// TestSchemaDieselMigrations checks that a diesel.toml's migrations
// directory reaches unread_tools through the linker's marker (row DI-U1,
// Revision 1 addendum OP1), on both backends.
func TestSchemaDieselMigrations(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"Cargo.toml":    "[package]\nname = \"app\"\nversion = \"0.1.0\"\n\n[dependencies]\ndiesel = { version = \"2.3.14\", features = [\"postgres\"] }\n",
			"Cargo.lock":    "version = 4\n\n[[package]]\nname = \"app\"\nversion = \"0.1.0\"\ndependencies = [\n \"diesel\",\n]\n\n[[package]]\nname = \"diesel\"\nversion = \"2.3.14\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n",
			"diesel.toml":   "[print_schema]\nfile = \"src/schema.rs\"\n",
			"src/schema.rs": "// @generated automatically by Diesel CLI.\n\ndiesel::table! {\n    users (id) {\n        id -> Int4,\n    }\n}\n",
			"migrations/2024-01-01-000000_users/up.sql":   "CREATE TABLE users (id serial PRIMARY KEY);\n",
			"migrations/2024-01-01-000000_users/down.sql": "DROP TABLE users;\n",
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		dir, one := "migrations", 1
		want := []schemaUnreadTool{{Tool: "diesel-migrations", Path: &dir, Files: &one}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}
