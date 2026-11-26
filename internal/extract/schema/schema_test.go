package schema

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/scan"
)

const fixture = "../../../testdata/fixtures/sql-migrations"

type linked struct {
	sources     map[string][]byte
	nodes       map[string]contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
}

func linkRoot(t *testing.T, root string) linked {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	out := linked{sources: map[string][]byte{}, nodes: map[string]contract.Node{}}
	for _, f := range scanned.Files {
		out.sources[f.Record.Path] = f.Source
	}
	nodes, edges, diagnostics := Link(Input{Sources: out.sources, Root: root})
	for _, n := range nodes {
		out.nodes[n.Identity] = n
	}
	out.edges, out.diagnostics = edges, diagnostics
	return out
}

func (l linked) node(t *testing.T, kind, file, qn string) contract.Node {
	t.Helper()
	id, err := contract.Identity(kind, file, qn)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := l.nodes[id]
	if !ok {
		t.Fatalf("missing %s", id)
	}
	return n
}

// spanText slices the normalized source with a contract span.
func (l linked) spanText(t *testing.T, file string, s contract.Span) string {
	t.Helper()
	lines := strings.SplitAfter(string(l.sources[file]), "\n")
	offset := func(line, column int) int {
		at := 0
		for i := 0; i < line-1; i++ {
			at += len(lines[i])
		}
		return at + column
	}
	return string(l.sources[file])[offset(s.StartLine, s.StartColumn):offset(s.EndLine, s.EndColumn)]
}

func TestVersionsQ1(t *testing.T) {
	for _, tc := range []struct {
		name, version, direction, description string
		ok                                    bool
	}{
		{"01_b.sql", "1", "simple", "b", true},
		{"+7_plus_sign.sql", "7", "simple", "plus sign", true},
		{"-3_negative.sql", "-3", "simple", "negative", true},
		{"10_two.up.sql", "10", "up", "two", true},
		{"10_two.down.sql", "10", "down", "two", true},
		{"2_twice.sql.sql", "2", "simple", "twice", true},
		{"x1_init.sql", "", "simple", "init", false},
		{"99999999999999999999_big.sql", "", "simple", "big", false},
	} {
		m := newMigration("m/"+tc.name, tc.name, nil)
		if m.direction != tc.direction || m.description != tc.description || (m.version != nil) != tc.ok {
			t.Fatalf("%s: %+v", tc.name, m)
		}
		if tc.ok && fmt.Sprint(*m.version) != tc.version {
			t.Fatalf("%s: version %d", tc.name, *m.version)
		}
	}
	for _, name := range []string{"README.md", "1.sql", "1_x.sql.bak", "nounderscore.sql"} {
		if _, _, ok := sqlxName(name); ok {
			t.Fatalf("%s must be ignored", name)
		}
	}
	if _, _, ok := sqlxName("_leading.sql"); !ok {
		t.Fatal("an empty version is a migration name whose version fails to parse")
	}
}

func TestFixtureOrderingAndSpans(t *testing.T) {
	l := linkRoot(t, fixture)
	order := map[string]any{}
	for _, n := range l.nodes {
		if n.Kind == "migration" && strings.HasPrefix(n.File, "pg-app/") {
			order[n.Name] = n.Attributes["order"]
		}
	}
	want := map[string]any{"1_base.sql": 1, "2_relations.sql": 2, "3_alter.sql": 3, "4_recreate.sql": 4, "5_generated_name.sql": 5,
		"6_opaque.sql": 6, "7_unsupported.sql": 7, "8_unreadable_alter.sql": 8, "9_unterminated.sql": 9, "10_reversible.up.sql": 10, "10_reversible.down.sql": nil}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("Q1-Q3 order %v", order)
	}
	for _, n := range l.nodes {
		if strings.Contains(n.File, "nested/") || strings.HasSuffix(n.File, "README.md") || strings.HasPrefix(n.File, "no-sqlx/") {
			t.Fatalf("ignored file produced %s", n.Identity)
		}
		if strings.HasPrefix(n.File, "bad-version/") || strings.HasPrefix(n.File, "dup-version/") || strings.HasPrefix(n.File, "sqlite-app/") {
			if n.Kind != "migration" {
				t.Fatalf("set without schema produced %s", n.Identity)
			}
		}
	}
	dup := l.node(t, "migration", "dup-version/migrations/2_c.sql", "migration")
	if dup.Attributes["applied"] != nil || dup.Attributes["order"] != nil {
		t.Fatalf("after a duplicate version: %v", dup.Attributes)
	}
	bad := l.node(t, "migration", "bad-version/migrations/2_next.sql", "migration")
	if bad.Attributes["applied"] != false {
		t.Fatalf("after an invalid version: %v", bad.Attributes)
	}

	// Spans: statement, column definition and REFERENCES clause.
	projects := l.node(t, "table", "pg-app/migrations/2_relations.sql", "projects")
	if text := l.spanText(t, projects.File, *projects.Span); !strings.HasPrefix(text, "CREATE TABLE projects (") || !strings.HasSuffix(text, ");") {
		t.Fatalf("table span %q", text)
	}
	account := l.node(t, "column", "pg-app/migrations/1_base.sql", "members.account_id")
	if text := l.spanText(t, account.File, *account.Span); text != "account_id serial primary key" {
		t.Fatalf("column span %q", text)
	}
	var refs []string
	for _, e := range l.edges {
		if e.Kind == "foreign_key" && strings.Contains(e.Source, "projects.owner_id") {
			refs = append(refs, l.spanText(t, e.Evidence[0].File, e.Evidence[0].Span))
		}
		if e.Kind == "foreign_key" && strings.Contains(e.Source, "tasks.owner_id") {
			refs = append(refs, l.spanText(t, e.Evidence[0].File, e.Evidence[0].Span)[:41])
		}
	}
	if !reflect.DeepEqual(refs, []string{"REFERENCES accounts ON DELETE CASCADE", "CONSTRAINT tasks_project_fkey FOREIGN KEY"}) && !reflect.DeepEqual(refs, []string{"CONSTRAINT tasks_project_fkey FOREIGN KEY", "REFERENCES accounts ON DELETE CASCADE"}) {
		t.Fatalf("reference spans %q", refs)
	}
	// P2: a 70-byte unquoted name folds and truncates to 63 bytes; quoted
	// names keep their case.
	long := l.node(t, "table", "pg-app/migrations/1_base.sql", "seventy_byte_unquoted_table_name_that_postgres_truncates_to_63_")
	if len(long.Name) != 63 {
		t.Fatal(long.Name)
	}
	l.node(t, "column", "pg-app/migrations/1_base.sql", "MixedCase.Id")
	l.node(t, "column", "pg-app/migrations/1_base.sql", "MixedCase.label")
	// RENAME TO keeps the creating file and records the old name; a table
	// dropped and recreated takes the later file.
	members := l.node(t, "table", "pg-app/migrations/1_base.sql", "members")
	if !reflect.DeepEqual(members.Attributes["previous_names"], []string{"accounts"}) || members.Attributes["state"] != "partial" {
		t.Fatalf("members %v", members.Attributes)
	}
	l.node(t, "table", "pg-app/migrations/4_recreate.sql", "scratch")
	l.node(t, "db_index", "pg-app/migrations/2_relations.sql", "tasks.(title)@2")
	if _, ok := l.nodes["db_index:pg-app/migrations/2_relations.sql#tasks.tasks_owner_title"]; ok {
		t.Fatal("DROP COLUMN must drop the index that includes the column")
	}
}

func TestRevisionOneCases(t *testing.T) {
	l := linkRoot(t, fixture)
	attr := func(n contract.Node, k string) any { return n.Attributes[k] }
	// R1.
	for _, tc := range []struct{ file, col, resolved string }{
		{"1_serials.sql", "a", "smallint"}, {"1_serials.sql", "b", "smallint"}, {"1_serials.sql", "c", "integer"},
		{"1_serials.sql", "d", "integer"}, {"1_serials.sql", "e", "bigint"}, {"1_serials.sql", "f", "bigint"},
		{"1_serials.sql", "i", "integer"}, {"2_add_serial.sql", "j", "bigint"},
	} {
		c := l.node(t, "column", "pg-serial/migrations/"+tc.file, "counters."+tc.col)
		if attr(c, "type_resolved") != tc.resolved || attr(c, "default_implied") != "sequence" || attr(c, "default") != nil || attr(c, "nullable") != false {
			t.Fatalf("R1 %s: %v", tc.col, c.Attributes)
		}
	}
	for _, name := range []string{"g", "h"} {
		c := l.node(t, "column", "pg-serial/migrations/1_serials.sql", "counters."+name)
		if _, ok := c.Attributes["type_resolved"]; ok {
			t.Fatal("type_resolved only for serial aliases")
		}
		if _, ok := c.Attributes["default_implied"]; ok {
			t.Fatal("default_implied only for serial aliases")
		}
	}
	// R2.
	for _, tc := range []struct {
		file, qn, state string
		opaque          int
	}{
		{"1_before.sql", "early", "partial", 5}, {"1_before.sql", "untouched", "complete", 5},
		// Revision 8: 2_switch.sql may fail (unqualified names after the
		// search_path change), so app.qualified is partial too.
		{"2_switch.sql", "late", "partial", 4}, {"2_switch.sql", "app.qualified", "partial", 4},
		{"2_switch.sql", "pointer", "partial", 0},
	} {
		n := l.node(t, "table", "pg-search-path/migrations/"+tc.file, tc.qn)
		if attr(n, "state") != tc.state || attr(n, "opaque_after") != tc.opaque {
			t.Fatalf("R2 %s: %v", tc.qn, n.Attributes)
		}
	}
	pointer := l.node(t, "column", "pg-search-path/migrations/2_switch.sql", "pointer.early_id")
	if ref := attr(pointer, "references").(map[string]any); ref["status"] != "unresolved" {
		t.Fatalf("R2 reference %v", ref)
	}
	// R3. Revision 1.9 Y48: 1_opaque.sql's DO runs a constant CREATE
	// TABLE, which the pass does not analyse where it runs (H21 judges it),
	// so it may fail and plain, created after it, is partial too.
	for qn, state := range map[string]string{"made": "partial", "other": "partial", "plain": "partial"} {
		if n := l.node(t, "table", "pg-opaque-exists/migrations/2_if_not_exists.sql", qn); attr(n, "state") != state {
			t.Fatalf("R3 %s: %v", qn, n.Attributes)
		}
	}
	// R4.
	items := l.node(t, "table", "pg-set-opacity/migrations/1_extension.sql", "items")
	if attr(items, "state") != "complete" || attr(items, "opaque_after") != 5 {
		t.Fatalf("R4 items %v", items.Attributes)
	}
	m := l.node(t, "migration", "pg-set-opacity/migrations/2_function.sql", "migration")
	want := map[string]any{"total": 6, "by_kind": map[string]any{"extension": 1, "dml": 4, "function_call": 1}}
	if !reflect.DeepEqual(attr(m, "set_opaque_statements"), want) {
		t.Fatalf("R4 set opacity %v", attr(m, "set_opaque_statements"))
	}
	serial := l.node(t, "migration", "pg-serial/migrations/1_serials.sql", "migration")
	if attr(serial, "no_effect") != 1 || attr(serial, "opaque") != 0 {
		t.Fatalf("DML in a set without functions is no-effect: %v", serial.Attributes)
	}
}

func TestUnreadableMemberR5(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join(fixture, "pg-unreadable"))); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "migrations")
	if err := os.WriteFile(filepath.Join(dir, "2_unreadable.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	members := []string{"2_unreadable.sql"}
	if err := os.Symlink(filepath.Join(dir, "1_first.sql"), filepath.Join(dir, "4_linked.sql")); err == nil {
		members = append(members, "4_linked.sql")
	} else {
		t.Logf("symlinks unavailable: %v", err)
	}
	l := linkRoot(t, root)
	var got []string
	for _, d := range l.diagnostics {
		if d.Code == "schema_member_unreadable" {
			got = append(got, d.Message)
		}
	}
	if len(got) != len(members) {
		t.Fatalf("diagnostics %q", got)
	}
	for i, name := range members {
		if !strings.Contains(got[i], "member "+name) {
			t.Fatalf("diagnostic %q does not name %s", got[i], name)
		}
	}
	for _, file := range []string{"1_first.sql", "3_third.sql"} {
		for _, n := range l.nodes {
			if n.Kind == "table" && n.File == "migrations/"+file && n.Attributes["state"] != "partial" {
				t.Fatalf("R5 %s: %v", n.Identity, n.Attributes)
			}
		}
	}
}

// TestSetsWithoutRecords covers T018b1 review: a set whose every
// member is unreadable (not UTF-8, or excluded by .graphveinignore, which
// the test writes so no live ignore file is committed) emits no node and
// only schema_member_unreadable on its manifest; a manifest the pass cannot
// read gives schema_manifest_unreadable instead of silence.
func TestSetsWithoutRecords(t *testing.T) {
	onlyDiagnostics := func(t *testing.T, l linked, file, code string, n int) {
		t.Helper()
		if len(l.nodes) != 0 || len(l.edges) != 0 {
			t.Fatalf("records %v %v", l.nodes, l.edges)
		}
		got := 0
		for _, d := range l.diagnostics {
			if d.File != file || d.Code != code {
				t.Fatalf("unexpected diagnostic %+v", d)
			}
			got++
		}
		if got != n {
			t.Fatalf("%d %s diagnostics, want %d: %v", got, code, n, l.diagnostics)
		}
	}
	copyPackage := func(t *testing.T, name string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(filepath.Join(fixture, name))); err != nil {
			t.Fatal(err)
		}
		return root
	}
	t.Run("not utf-8", func(t *testing.T) {
		root := copyPackage(t, "pg-all-unreadable")
		if err := os.WriteFile(filepath.Join(root, "migrations", "1_only.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		onlyDiagnostics(t, linkRoot(t, root), "Cargo.toml", "schema_member_unreadable", 1)
	})
	t.Run("ignored", func(t *testing.T) {
		root := copyPackage(t, "pg-all-unreadable")
		if err := os.WriteFile(filepath.Join(root, "migrations", "1_only.sql"), []byte("CREATE TABLE t (id integer);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".graphveinignore"), []byte("migrations/1_only.sql\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		onlyDiagnostics(t, linkRoot(t, root), "Cargo.toml", "schema_member_unreadable", 1)
	})
	t.Run("manifest unreadable", func(t *testing.T) {
		l := linkRoot(t, copyPackage(t, "bad-manifest"))
		onlyDiagnostics(t, l, "Cargo.toml", "schema_manifest_unreadable", 1)
		if d := l.diagnostics[0]; d.Severity != "warning" || !strings.Contains(d.Message, "line 9: expected value") {
			t.Fatalf("diagnostic %+v", d)
		}
	})
}

// TestRevisionTwoCases covers the Revision 2 cases
// with the pg-rev2-* fixture packages and pg-app's failing files.
func TestRevisionTwoCases(t *testing.T) {
	l := linkRoot(t, fixture)
	attr := func(n contract.Node, k string) any { return n.Attributes[k] }
	has := func(kind, file, qn string) bool {
		id, err := contract.Identity(kind, file, qn)
		if err != nil {
			t.Fatal(err)
		}
		_, ok := l.nodes[id]
		return ok
	}
	state := func(file, qn string) any {
		if !has("table", file, qn) {
			return "absent"
		}
		return attr(l.node(t, "table", file, qn), "state")
	}
	expect := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", what, got, want)
		}
	}
	cols := "pg-rev2-columns/migrations/1_columns.sql"
	idx := "pg-rev2-columns/migrations/2_indexes.sql"

	// WF1: a column named exclude is a column, EXCLUDE USING is a constraint.
	expect("WF1 bookings.exclude", has("column", cols, "bookings.exclude"), true)
	expect("WF1 flags.exclude", has("column", cols, "flags.exclude"), true)
	expect("WF1 bookings state", state(cols, "bookings"), "complete")

	// G3: unique covers primary keys, UNIQUE constraints and plain unique
	// indexes; false on a complete table otherwise; null on a partial one.
	for qn, want := range map[string]any{"handles.id": true, "handles.nick": true, "handles.email": false, "handles.partial_key": false,
		"handles.pair_a": false, "handles.plain": false, "bookings.code": true} {
		expect("G3 "+qn, attr(l.node(t, "column", cols, qn), "unique"), want)
	}
	expect("G3 ledger.memo (partial table)", attr(l.node(t, "column", idx, "ledger.memo"), "unique"), nil)
	expect("G3 ledger.id", attr(l.node(t, "column", idx, "ledger.id"), "unique"), true)

	// WF7: CASCADE drops the generated column, its index and the CHECK.
	expect("WF7 prices.net", has("column", cols, "prices.net"), false)
	expect("WF7 prices.gross", has("column", cols, "prices.gross"), false)
	expect("WF7 prices index", has("db_index", cols, "prices.prices_gross_idx"), false)
	expect("WF7 prices state", state(cols, "prices"), "complete")
	// WF7 without CASCADE: refused, the migration may fail.
	refused := "pg-rev2-types/migrations/2_refused.sql"
	expect("WF7 totals.net kept", has("column", refused, "totals.net"), true)
	expect("WF7 totals state", state(refused, "totals"), "partial")
	expect("WF7 refused migration applied", attr(l.node(t, "migration", refused, "migration"), "applied"), nil)

	// G5: DEFAULT pick_default() runs a set-defined function.
	m := l.node(t, "migration", idx, "migration")
	byKind := attr(m, "set_opaque_statements").(map[string]any)["by_kind"].(map[string]any)
	expect("G5 function_call", byKind["function_call"], 1)
	expect("G5 handles.picked", has("column", idx, "handles.picked"), true)
	expect("G5 handles.picked in creating file", has("column", cols, "handles.picked"), false)

	// WF5: DROP INDEX public.x may mean the index of an unqualified table;
	// T018a2 P3: whether it still exists is unknown, so it is not stated.
	expect("WF5 ledger state", state(idx, "ledger"), "partial")
	expect("WF5 ledger index not stated (P3)", has("db_index", idx, "ledger.ledger_memo_idx"), false)
	// WF5: ALTER INDEX <table> RENAME renames the table; ALTER INDEX
	// <index> RENAME renames the index.
	expect("WF5 journal renamed", state(idx, "journal_v2"), "complete")
	expect("WF5 journal index renamed", has("db_index", idx, "journal_v2.journal_v2_id_idx"), true)
	if has("table", idx, "journal_v2") {
		expect("WF5 journal previous names", attr(l.node(t, "table", idx, "journal_v2"), "previous_names"), []string{"journal"})
	}

	// WF3: no table over another relation of the same name.
	rel := "pg-rev2-relations/migrations/1_relations.sql"
	for _, qn := range []string{"base_view", "counter_seq", "base_lookup", "pair", "base_totals", "selected", "reports.summary"} {
		expect("WF3 "+qn, state(rel, qn), "absent")
	}
	expect("WF3 base", state(rel, "base"), "complete")
	conflict := "pg-rev2-relations/migrations/2_type_conflict.sql"
	expect("WF3 status (type conflict)", state(conflict, "status"), "absent")
	expect("WF3/WF8 touched_here", state(conflict, "touched_here"), "partial")
	expect("WF8 conflict applied", attr(l.node(t, "migration", conflict, "migration"), "applied"), nil)
	after := "pg-rev2-relations/migrations/3_after.sql"
	expect("WF8 after applied", attr(l.node(t, "migration", after, "migration"), "applied"), nil)
	expect("WF8 after order", attr(l.node(t, "migration", after, "migration"), "order"), 3)
	expect("WF8 after_fail", state(after, "after_fail"), "partial")
	expect("WF8 dropped_later kept", state(rel, "dropped_later"), "partial")

	// WF8: pg-app 8 (an unreadable statement) and 9 (a lexing failure).
	expect("WF8 tail", state("pg-app/migrations/9_unterminated.sql", "tail"), "partial")
	expect("WF8 reversible", state("pg-app/migrations/10_reversible.up.sql", "reversible"), "partial")
	for _, f := range []string{"8_unreadable_alter.sql", "9_unterminated.sql", "10_reversible.up.sql"} {
		expect("WF8 applied "+f, attr(l.node(t, "migration", "pg-app/migrations/"+f, "migration"), "applied"), nil)
	}
	// T018a2 B1b (P5, R1b): 7's view names a column of members after 6's
	// opaque statements, so its query is not proven and 7 may fail.
	expect("WF8 7 applied", attr(l.node(t, "migration", "pg-app/migrations/7_unsupported.sql", "migration"), "applied"), nil)

	// WF9 and WF4: temporary tables are not emitted; uncertain names keep
	// the known objects.
	temp := "pg-rev2-shadow/migrations/1_temp.sql"
	expect("WF9 pg_temp table", state(temp, "pg_temp.scratch_tmp"), "absent")
	expect("WF4 kept (temp shadow)", state(temp, "kept"), "partial")
	expect("WF4 solid (search_path)", state(temp, "solid"), "partial")
	expect("WF4 trimmed.extra kept", has("column", temp, "trimmed.extra"), true)
	expect("WF4 trimmed", state(temp, "trimmed"), "partial")
	expect("WF4 renamed", state(temp, "renamed_v2"), "partial")

	// WF6: ALTER SCHEMA RENAME moves the schema's tables.
	sch := "pg-rev2-schema/migrations/1_schema.sql"
	expect("WF6 stock.items", state(sch, "stock.items"), "partial")
	expect("WF6 stock.items.qty", has("column", sch, "stock.items.qty"), true)
	expect("WF6 inv.items", state(sch, "inv.items"), "absent")
	expect("WF6 loose", state(sch, "loose"), "partial")
	expect("WF6 after_rename", state(sch, "after_rename"), "partial")

	// G1: DROP TYPE ... CASCADE makes the tables using the type partial.
	types := "pg-rev2-types/migrations/1_types.sql"
	expect("G1 feelings", state(types, "feelings"), "partial")
	expect("G1 unrelated", state(types, "unrelated"), "complete")

	// G2 and WF2.
	roles := "pg-rev2-roles/migrations/1_roles.sql"
	expect("G2 after_role", state(roles, "after_role"), "partial")
	expect("WF2 before_role", state(roles, "before_role"), "partial")
	var owned []string
	for _, r := range attr(l.node(t, "table", roles, "before_role"), "reasons").([]any) {
		owned = append(owned, r.(map[string]any)["reason"].(string))
	}
	expect("WF2 reason", owned, []string{"DROP OWNED may have dropped this table or its objects"})
}

// TestDuplicateRecordCode checks that a duplicate record has its own code.
func TestDuplicateRecordCode(t *testing.T) {
	e := newEmitter()
	n := contract.Node{Type: "node", Identity: "table:m/1.sql#t", Kind: "table", File: "m/1.sql"}
	e.node(n)
	e.node(n)
	if len(e.diagnostics) != 1 || e.diagnostics[0].Code != "schema_duplicate_record" {
		t.Fatalf("diagnostics %v", e.diagnostics)
	}
}

// TestLinkRecoversPerSet checks that a panic in one set becomes a
// diagnostic and the other sets are still emitted.
func TestLinkRecoversPerSet(t *testing.T) {
	saved := runSet
	t.Cleanup(func() { runSet = saved })
	runSet = func(set *migrationSet, e *emitter) {
		if set.dir == "pg-app/migrations" {
			e.node(contract.Node{Type: "node", Identity: "table:pg-app/migrations/1_base.sql#half", Kind: "table"})
			panic("boom")
		}
		saved(set, e)
	}
	l := linkRoot(t, fixture)
	found := false
	for _, d := range l.diagnostics {
		if d.Code == "schema_internal_error" && strings.Contains(d.Message, "pg-app/migrations") && strings.Contains(d.Message, "boom") {
			found = true
		}
		if strings.HasPrefix(d.File, "pg-app/") && d.Code != "schema_internal_error" {
			t.Fatalf("diagnostic of the failed set kept: %v", d)
		}
	}
	if !found {
		t.Fatal("no schema_internal_error diagnostic")
	}
	for _, n := range l.nodes {
		if strings.HasPrefix(n.File, "pg-app/") || strings.HasPrefix(n.Identity, "table:pg-app/") {
			t.Fatalf("record of the failed set emitted: %s", n.Identity)
		}
	}
	l.node(t, "table", "pg-serial/migrations/1_serials.sql", "counters")
}

// TestRevisionTwoDispatch covers statement forms found in the Revision 2
// pass over the dispatch: none of them may be no-effect.
func TestRevisionTwoDispatch(t *testing.T) {
	functions := map[string]bool{"f": true}
	for _, tc := range []struct {
		sql        string
		class      class
		searchPath bool
		mayFail    bool
		allTables  bool
		dependents int
		runs       bool
		// drops is a named DROP of another kind (Revision 3 class 1).
		drops bool
	}{
		// SET SESSION AUTHORIZATION was read as SET SESSION <name>.
		{sql: "SET SESSION AUTHORIZATION bob", class: classOpaque, searchPath: true},
		{sql: "SET session_authorization = 'bob'", class: classOpaque, searchPath: true},
		{sql: `SET "search_path" = app`, class: classOpaque, searchPath: true},
		{sql: "RESET session_authorization", class: classOpaque, searchPath: true},
		{sql: "ALTER DATABASE app SET search_path TO app", class: classOpaque, searchPath: true},
		{sql: "ALTER ROLE bob RENAME TO alice", class: classOpaque, searchPath: true},
		{sql: "ALTER ROLE bob WITH LOGIN", class: classNoEffect},
		{sql: "VACUUM", class: classUnsupported, mayFail: true},
		{sql: "REINDEX TABLE CONCURRENTLY t", class: classUnsupported, mayFail: true},
		{sql: "DROP DATABASE other", class: classUnsupported, mayFail: true},
		{sql: "ANALYZE t", class: classOpaque},
		{sql: "REASSIGN OWNED BY a TO b", class: classNoEffect},
		{sql: "DROP EXTENSION citext", class: classOpaque},
		{sql: "DROP EXTENSION citext CASCADE", class: classOpaque, allTables: true},
		{sql: "DROP OPERATOR === (integer, integer) CASCADE", class: classOpaque, allTables: true},
		{sql: "DROP FUNCTION f(integer) CASCADE", class: classOpaque, drops: true},
		{sql: "DROP SEQUENCE s CASCADE", class: classOpaque, drops: true},
		{sql: "DROP COLLATION c CASCADE", class: classOpaque, drops: true},
		{sql: "DROP TRIGGER tr ON t CASCADE", class: classOpaque},
		{sql: "DROP FROBNICATOR x", class: classError},
		{sql: "ALTER TYPE mood RENAME TO feeling", class: classUnsupported, dependents: 1},
		{sql: "ALTER TYPE mood ADD VALUE 'meh'", class: classModeled},
		{sql: "ALTER DOMAIN d SET NOT NULL", class: classUnsupported, dependents: 1},
		{sql: "ALTER COLLATION c RENAME TO d", class: classNoEffect, dependents: 1},
		{sql: "CREATE TEMP TABLE app.t (id int)", class: classUnsupported, mayFail: true},
		{sql: "CREATE TABLE t2 AS SELECT f() AS x", class: classUnsupported, runs: true},
		{sql: "CREATE INDEX ON t ((f(a)))", class: classModeled, runs: true},
		{sql: "ALTER TABLE t ADD COLUMN c int DEFAULT f()", class: classModeled, runs: true},
		{sql: "SELECT f() INTO t3", class: classUnsupported, runs: true},
	} {
		stmts, failure := lexStatements([]byte(tc.sql + ";"))
		if failure != nil || len(stmts) != 1 {
			t.Fatalf("%s: lexing %v", tc.sql, failure)
		}
		a := analyze(stmts[0], []byte(tc.sql+";"), functions, true)
		if a.class != tc.class || a.searchPath != tc.searchPath || (a.mayFail != "") != tc.mayFail || (a.allTables != "") != tc.allTables ||
			len(a.dependents) != tc.dependents || (a.runsFunction != "") != tc.runs || (a.dropOthers != nil) != tc.drops {
			t.Errorf("%s: %+v", tc.sql, a)
		}
	}
}
