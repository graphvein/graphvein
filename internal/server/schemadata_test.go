package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func num(s string) json.Number { return json.Number(s) }

func TestCompleteness(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]any
		want  string
	}{
		{map[string]any{"state": "complete", "opaque_after": num("0")}, completenessComplete},
		{map[string]any{"state": "complete", "opaque_after": num("3")}, completenessInSource},
		{map[string]any{"state": "complete", "opaque_after": num("-1")}, completenessUnknown},
		{map[string]any{"state": "complete", "opaque_after": "3"}, completenessUnknown},
		{map[string]any{"state": "complete", "opaque_after": num("1.5")}, completenessUnknown},
		{map[string]any{"state": "complete", "opaque_after": nil}, completenessUnknown},
		{map[string]any{"state": "complete"}, completenessUnknown},
		{map[string]any{"state": "partial", "opaque_after": num("0")}, completenessPartial},
		{map[string]any{"state": "partial"}, completenessPartial},
		{map[string]any{"state": "weird", "opaque_after": num("0")}, completenessUnknown},
		{map[string]any{"state": "Complete", "opaque_after": num("0")}, completenessUnknown},
		{map[string]any{"state": true, "opaque_after": num("0")}, completenessUnknown},
		{map[string]any{"opaque_after": num("0")}, completenessUnknown},
		{nil, completenessUnknown},
	} {
		if got := completeness(tc.attrs); got != tc.want {
			t.Errorf("%v: %s, want %s", tc.attrs, got, tc.want)
		}
	}
}

func TestDefaultState(t *testing.T) {
	all := []string{completenessComplete, completenessInSource, completenessPartial, completenessUnknown}
	for _, tc := range []struct {
		attrs map[string]any
		want  map[string]string // by table completeness; missing keys use "*"
	}{
		{map[string]any{"default": "now()"}, map[string]string{"*": defaultWritten}},
		{map[string]any{"default": "now()", "default_implied": "sequence"}, map[string]string{"*": defaultWritten}},
		{map[string]any{"default": nil, "default_implied": "sequence"}, map[string]string{"*": defaultImplied}},
		{map[string]any{"default": nil, "default_implied": nil}, map[string]string{"*": defaultUnknown}},
		{map[string]any{"default": nil, "default_implied": num("1")}, map[string]string{"*": defaultUnknown}},
		{map[string]any{"default": nil}, map[string]string{completenessComplete: defaultNone, completenessInSource: defaultNone, "*": defaultUnknown}},
		{map[string]any{}, map[string]string{"*": defaultUnknown}},
		{map[string]any{"default": num("5")}, map[string]string{"*": defaultUnknown}},
		{map[string]any{"default": false}, map[string]string{"*": defaultUnknown}},
		{map[string]any{"default": []any{}}, map[string]string{"*": defaultUnknown}},
	} {
		for _, c := range all {
			want, ok := tc.want[c]
			if !ok {
				want = tc.want["*"]
			}
			if got := defaultState(tc.attrs, c); got != want {
				t.Errorf("%v on %s: %s, want %s", tc.attrs, c, got, want)
			}
		}
	}
}

// TestModelDefaultState covers the model default rule: a Prisma client
// default (T018g1: default "client", no server default) is its own state,
// never unknown and never a server default.
func TestModelDefaultState(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]any
		table string
		want  string
	}{
		{map[string]any{"server_default": true, "default": "server"}, completenessPartial, defaultServer},
		{map[string]any{"server_default": false, "default": "scalar"}, completenessPartial, defaultPython},
		{map[string]any{"server_default": false, "default": "callable"}, completenessComplete, defaultPython},
		{map[string]any{"server_default": false, "default": "client"}, completenessPartial, defaultClient},
		{map[string]any{"server_default": nil, "default": "client"}, completenessComplete, defaultUnknown},
		{map[string]any{"server_default": false, "default": "absent"}, completenessComplete, defaultNone},
		{map[string]any{"server_default": false, "default": "absent"}, completenessPartial, defaultUnknown},
		{map[string]any{"server_default": false, "default": nil}, completenessComplete, defaultUnknown},
	} {
		if got := modelDefaultState(tc.attrs, tc.table); got != tc.want {
			t.Errorf("%v on %s: %s, want %s", tc.attrs, tc.table, got, tc.want)
		}
	}
}

func migrationNode(id string, attrs map[string]any) contract.Node {
	return contract.Node{Identity: id, Kind: "migration", File: id, Attributes: attrs}
}

func TestDeriveSetState(t *testing.T) {
	pg := "postgres"
	lite := "sqlite"
	known := func(total string, byKind map[string]any) map[string]any {
		return map[string]any{"set_opaque_statements": map[string]any{"total": num(total), "by_kind": byKind}}
	}
	// counted adds the unsupported count and applied to a migration's
	// attributes.
	counted := func(id string, attrs map[string]any, unsupported, applied any) contract.Node {
		out := map[string]any{"unsupported": unsupported, "applied": applied}
		for k, v := range attrs {
			out[k] = v
		}
		return migrationNode(id, out)
	}
	for _, tc := range []struct {
		name       string
		dialect    *string
		migrations []contract.Node
		codes      map[string]int
		objects    string
		notModeled []string
		tableList  string
		reasons    []string
	}{
		{"complete", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("0"), true), counted("b", known("0", map[string]any{}), num("0"), true)}, nil, objectsModeled, []string{}, tableListComplete, []string{}},
		{"opaque", &pg, []contract.Node{counted("a", known("2", map[string]any{"do": num("2")}), num("0"), true)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"opaque_statements"}},
		{"unreadable and config", &pg, []contract.Node{counted("a", known("1", map[string]any{"do": num("1")}), num("0"), true)}, map[string]int{"schema_member_unreadable": 1, "schema_unsupported_config": 1}, objectsModeled, []string{}, tableListMaybe, []string{"member_unreadable", "opaque_statements", "unsupported_config"}},
		{"sqlite", &lite, []contract.Node{migrationNode("a", map[string]any{"set_opaque_statements": nil})}, nil, objectsNotModeled, []string{"dialect_unsupported"}, tableListNotModeled, []string{}},
		{"dialect conflict or absent", nil, []contract.Node{migrationNode("a", known("0", map[string]any{}))}, nil, objectsNotModeled, []string{"dialect_unsupported"}, tableListNotModeled, []string{}},
		{"order failed", &pg, []contract.Node{migrationNode("a", map[string]any{"set_opaque_statements": nil})}, map[string]int{"schema_migration_order": 1}, objectsNotModeled, []string{"order_failed"}, tableListNotModeled, []string{}},
		{"order failed, unknown dialect", nil, nil, map[string]int{"schema_migration_order": 2}, objectsNotModeled, []string{"dialect_unsupported", "order_failed"}, tableListNotModeled, []string{}},
		{"not analysed: null record", &pg, []contract.Node{migrationNode("a", map[string]any{"set_opaque_statements": nil})}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: missing record", &pg, []contract.Node{migrationNode("a", map[string]any{})}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: records differ", &pg, []contract.Node{migrationNode("a", known("1", map[string]any{"do": num("1")})), migrationNode("b", known("2", map[string]any{"do": num("2")}))}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: one record missing", &pg, []contract.Node{migrationNode("a", known("0", map[string]any{})), migrationNode("b", map[string]any{})}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: string total", &pg, []contract.Node{migrationNode("a", map[string]any{"set_opaque_statements": map[string]any{"total": "0", "by_kind": map[string]any{}}})}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: by_kind not an object", &pg, []contract.Node{migrationNode("a", map[string]any{"set_opaque_statements": map[string]any{"total": num("0"), "by_kind": nil}})}, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		{"not analysed: no migrations", &pg, nil, nil, objectsNotModeled, []string{"not_analysed"}, tableListNotModeled, []string{}},
		// T018b1 review: unsupported statements (SELECT INTO, CREATE
		// TABLE AS, CREATE SCHEMA ... CREATE TABLE) are not in the opaque
		// total but may create tables the API does not list.
		{"unsupported count", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("0"), true), counted("b", known("0", map[string]any{}), num("2"), true)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"unsupported_statements"}},
		{"unsupported diagnostic only", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("0"), true)}, map[string]int{"schema_unsupported_statement": 1}, objectsModeled, []string{}, tableListMaybe, []string{"unsupported_statements"}},
		{"zero counts and a down migration", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("0"), true), counted("b", known("0", map[string]any{}), nil, false)}, nil, objectsModeled, []string{}, tableListComplete, []string{}},
		{"zero counts, applied null", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("0"), nil)}, nil, objectsModeled, []string{}, tableListComplete, []string{}},
		{"null count on an applied migration", &pg, []contract.Node{counted("a", known("0", map[string]any{}), nil, true)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"null count, applied null", &pg, []contract.Node{counted("a", known("0", map[string]any{}), nil, nil)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"string count", &pg, []contract.Node{counted("a", known("0", map[string]any{}), "1", true)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"negative count", &pg, []contract.Node{counted("a", known("0", map[string]any{}), num("-1"), true)}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"missing count, applied missing", &pg, []contract.Node{migrationNode("a", known("0", map[string]any{}))}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"applied string false", &pg, []contract.Node{counted("a", known("0", map[string]any{}), nil, "false")}, nil, objectsModeled, []string{}, tableListMaybe, []string{"statement_counts_unknown"}},
		{"all reasons", &pg, []contract.Node{counted("a", known("1", map[string]any{"do": num("1")}), num("1"), true), counted("b", known("1", map[string]any{"do": num("1")}), nil, true)}, map[string]int{"schema_member_unreadable": 1, "schema_unsupported_config": 1}, objectsModeled, []string{}, tableListMaybe, []string{"member_unreadable", "opaque_statements", "statement_counts_unknown", "unsupported_config", "unsupported_statements"}},
	} {
		got := deriveSetState(tc.dialect, tc.migrations, tc.codes)
		if got.objects != tc.objects || !reflect.DeepEqual(got.notModeledReasons, tc.notModeled) || got.tableList != tc.tableList || !reflect.DeepEqual(got.tableListReasons, tc.reasons) {
			t.Errorf("%s: %+v", tc.name, got)
		}
		if got.objects == objectsNotModeled && (got.opaqueTotal != nil || got.opaqueByKind != nil) {
			t.Errorf("%s: opaque values on a set that is not modeled: %+v", tc.name, got)
		}
	}
}

func TestFoundState(t *testing.T) {
	run := func(name, version string) *lastRun {
		return &lastRun{Indexer: contract.Indexer{Name: name, Version: version}}
	}
	for _, tc := range []struct {
		name             string
		last             *lastRun
		sets, unreadable int
		gaps             int
		found            string
		reason           string
	}{
		{"no run", nil, 0, 0, 0, foundUnknown, "not_indexed"},
		{"external", run("scip-go", "extractors-28"), 0, 0, 0, foundUnknown, "external_indexer"},
		{"extractors-26", run("graphvein", "extractors-26"), 0, 0, 0, foundUnknown, "predates_schema"},
		{"extractors-1", run("graphvein", "extractors-1"), 0, 0, 0, foundUnknown, "predates_schema"},
		{"extractors-27", run("graphvein", "extractors-27"), 0, 0, 0, foundUnknown, "version_ambiguous"},
		{"extractors-28", run("graphvein", "extractors-28"), 0, 0, 0, foundNone, ""},
		{"extractors-130", run("graphvein", "extractors-130"), 0, 0, 0, foundNone, ""},
		{"extractors-028", run("graphvein", "extractors-028"), 0, 0, 0, foundUnknown, "version_unreadable"},
		{"overflow", run("graphvein", "extractors-99999999999999999999999"), 0, 0, 0, foundUnknown, "version_unreadable"},
		{"other text", run("graphvein", "dev"), 0, 0, 0, foundUnknown, "version_unreadable"},
		{"suffix", run("graphvein", "extractors-28-dirty"), 0, 0, 0, foundUnknown, "version_unreadable"},
		{"Graphvein", run("Graphvein", "extractors-28"), 0, 0, 0, foundUnknown, "external_indexer"},
		{"sets", run("graphvein", "extractors-27"), 2, 0, 0, foundSets, ""},
		{"sets and an unreadable manifest", run("graphvein", "extractors-28"), 1, 1, 0, foundSets, ""},
		{"unreadable manifest", run("graphvein", "extractors-28"), 0, 1, 0, foundUnknown, "manifest_unreadable"},
		{"unreadable manifest, extractors-27", run("graphvein", "extractors-27"), 0, 2, 0, foundUnknown, "version_ambiguous"},
		{"unreadable manifest, no run", nil, 0, 1, 0, foundUnknown, "not_indexed"},
		{"scan gap", run("graphvein", "extractors-28"), 0, 0, 1, foundUnknown, "scan_incomplete"},
		{"scan gap and an unreadable manifest", run("graphvein", "extractors-28"), 0, 1, 1, foundUnknown, "scan_incomplete"},
		{"scan gap and sets", run("graphvein", "extractors-28"), 1, 0, 1, foundSets, ""},
		{"scan gap, extractors-27", run("graphvein", "extractors-27"), 0, 0, 3, foundUnknown, "version_ambiguous"},
	} {
		found, reason := foundState(tc.last, tc.sets, tc.unreadable, tc.gaps)
		got := ""
		if reason != nil {
			got = *reason
		}
		if found != tc.found || got != tc.reason {
			t.Errorf("%s: %s %q, want %s %q", tc.name, found, got, tc.found, tc.reason)
		}
	}
}

func TestReferenceEntries(t *testing.T) {
	linked := map[string]any{"status": "linked"}
	unresolved := map[string]any{"status": "unresolved"}
	for _, tc := range []struct {
		attrs              map[string]any
		entries, unlinkedN int
	}{
		{map[string]any{"references": nil}, 0, 0},
		{map[string]any{}, 0, 0},
		{map[string]any{"references": linked}, 1, 0},
		{map[string]any{"references": unresolved}, 1, 1},
		{map[string]any{"references": map[string]any{"status": "LINKED"}}, 1, 1},
		{map[string]any{"references": "t"}, 1, 1},
		{map[string]any{"references": linked, "additional_references": []any{linked, unresolved}}, 3, 1},
		{map[string]any{"references": linked, "additional_references": "x"}, 2, 1},
		{map[string]any{"references": linked, "additional_references": []any{"x"}}, 2, 1},
	} {
		entries := referenceEntries(tc.attrs)
		n := 0
		for _, e := range entries {
			if !linkedEntry(e) {
				n++
			}
		}
		if len(entries) != tc.entries || n != tc.unlinkedN {
			t.Errorf("%v: %d entries, %d unlinked", tc.attrs, len(entries), n)
		}
	}
}

func TestCommonStringAndTestOnly(t *testing.T) {
	nodes := func(values ...any) []contract.Node {
		out := []contract.Node{}
		for _, v := range values {
			attrs := map[string]any{}
			if v != "-" {
				attrs["tool"] = v
				attrs["scope"] = v
			}
			out = append(out, contract.Node{Attributes: attrs})
		}
		return out
	}
	for _, tc := range []struct {
		nodes    []contract.Node
		value    any
		conflict bool
	}{
		{nodes("sqlx", "sqlx"), "sqlx", false},
		{nodes("sqlx", "other"), nil, true},
		{nodes("sqlx", "-"), nil, true},
		{nodes("sqlx", nil), nil, true},
		{nodes("-", nil), nil, false},
		{nil, nil, false},
	} {
		v, conflict := commonString(tc.nodes, "tool")
		var got any
		if v != nil {
			got = *v
		}
		if got != tc.value || conflict != tc.conflict {
			t.Errorf("%v: %v %v", tc.nodes, got, conflict)
		}
	}
	if testOnly(nil) != nil || !*testOnly(nodes("test", "test")) || *testOnly(nodes("test", "-")) || *testOnly(nodes("Test")) {
		t.Error("test_only rule")
	}
}

func TestSchemaOrdering(t *testing.T) {
	node := func(id string, attrs map[string]any) contract.Node {
		return contract.Node{Identity: id, File: id, QualifiedName: id, Attributes: attrs}
	}
	migrations := []contract.Node{
		node("m-null", map[string]any{"order": nil}),
		node("m-2", map[string]any{"order": num("2")}),
		node("m-str", map[string]any{"order": "1"}),
		node("m-1", map[string]any{"order": num("1")}),
		node("m-10", map[string]any{"order": num("10")}),
	}
	if got := sortedBy(migrations, lessByOrder); !reflect.DeepEqual(got, []string{"m-1", "m-2", "m-10", "m-null", "m-str"}) {
		t.Errorf("migrations %v", got)
	}
	columns := []contract.Node{
		node("c-none", map[string]any{}),
		node("c-3", map[string]any{"position": num("3")}),
		node("b-2", map[string]any{"position": num("2")}),
		node("a-2", map[string]any{"position": num("2")}),
		node("a-none", map[string]any{"position": "1"}),
	}
	if got := sortedBy(columns, lessByPosition); !reflect.DeepEqual(got, []string{"a-2", "b-2", "c-3", "a-none", "c-none"}) {
		t.Errorf("columns %v", got)
	}
	a, b := "a", "b"
	if !lessSetKey(&a, &b) || lessSetKey(&b, &a) || !lessSetKey(&b, nil) || lessSetKey(nil, &a) || lessSetKey(nil, nil) {
		t.Error("set key order")
	}
}

func sortedBy(nodes []contract.Node, less func(a, b contract.Node) bool) []string {
	list := append([]contract.Node(nil), nodes...)
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && less(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return identities(list)
}

// TestBuildSetsEdgesOnly checks grouping by edges alone: a column is
// attached only through a contains edge, a foreign key to a column without
// a containing table has target_table null, and records without a set or a
// table land in the set: null group, last.
func TestBuildSetsEdgesOnly(t *testing.T) {
	set := "s/migrations"
	g := schemaGraph{
		migrations: []contract.Node{{Identity: "migration:s/migrations/1_a.sql#migration", Kind: "migration", File: "s/migrations/1_a.sql", Attributes: map[string]any{"set": set, "dialect": "postgres", "order": num("1"), "set_opaque_statements": map[string]any{"total": num("0"), "by_kind": map[string]any{}}}}},
		tables: []contract.Node{
			{Identity: "table:s/migrations/1_a.sql#t", Kind: "table", QualifiedName: "t", Attributes: map[string]any{"set": set, "state": "complete", "opaque_after": num("0")}},
			{Identity: "table:x.sql#loose", Kind: "table", QualifiedName: "loose", Attributes: map[string]any{"state": "partial"}},
		},
		columns: []contract.Node{
			{Identity: "column:s/migrations/1_a.sql#t.a", Kind: "column", Attributes: map[string]any{"position": num("1"), "default": nil, "references": map[string]any{"status": "linked"}}},
			// Same file and a name under t, but no contains edge.
			{Identity: "column:s/migrations/1_a.sql#t.b", Kind: "column", Attributes: map[string]any{"position": num("2")}},
		},
		migrationEdges: []contract.Edge{{Kind: "contains", Source: "migration:s/migrations/1_a.sql#migration", Target: "table:s/migrations/1_a.sql#t"}},
		tableEdges:     []contract.Edge{{Kind: "contains", Source: "table:s/migrations/1_a.sql#t", Target: "column:s/migrations/1_a.sql#t.a"}},
		foreignKeys:    []contract.Edge{{Kind: "foreign_key", Source: "column:s/migrations/1_a.sql#t.a", Target: "column:s/migrations/1_a.sql#t.b", Provenance: "fact", Confidence: 1}},
	}
	sets := buildSets(g)
	if len(sets) != 2 || sets[0].Set == nil || *sets[0].Set != set || sets[1].Set != nil {
		t.Fatalf("sets %+v", sets)
	}
	s := sets[0]
	if len(s.Tables) != 1 || len(s.Tables[0].Columns) != 1 || s.Tables[0].Columns[0].DefaultState != defaultNone || s.Tables[0].ForeignKeysOut != 1 || s.Tables[0].ForeignKeysIn != 0 {
		t.Errorf("table %+v", s.Tables)
	}
	if len(s.ForeignKeys) != 1 || s.ForeignKeys[0].TargetTable != nil || s.LinkedWithoutEdge != 0 {
		t.Errorf("foreign keys %+v, linked without edge %d", s.ForeignKeys, s.LinkedWithoutEdge)
	}
	null := sets[1]
	if null.Unattached != (schemaUnattached{Columns: 1}) || len(null.Tables) != 1 || null.Objects != nil || null.TestOnly != nil || null.Source != nil {
		t.Errorf("null group %+v", null)
	}
	// Without the edge, the linked reference is counted as inconsistent.
	g.foreignKeys = nil
	if got := buildSets(g)[0]; got.LinkedWithoutEdge != 1 || len(got.ForeignKeys) != 0 {
		t.Errorf("without edge: %+v", got)
	}
}

func TestGroupNeighboursCap(t *testing.T) {
	pairs := map[string][]neighbourPair{}
	for i := 0; i < 201; i++ {
		id := fmt.Sprintf("table:t%03d.sql#t", i)
		pairs[id] = []neighbourPair{{SourceColumn: id + ".b", TargetColumn: "x"}, {SourceColumn: id + ".a", TargetColumn: "x"}}
	}
	kept, tables, omitted := groupNeighbours(pairs, NeighboursCap)
	if len(kept) != 200 || tables != 1 || omitted != 2 || kept[0].table != "table:t000.sql#t" || kept[199].table != "table:t199.sql#t" {
		t.Fatalf("kept %d, omitted %d tables %d pairs", len(kept), tables, omitted)
	}
	if kept[0].pairs[0].SourceColumn != "table:t000.sql#t.a" {
		t.Errorf("pairs not ordered: %v", kept[0].pairs)
	}
}

func TestSetFiles(t *testing.T) {
	got := setFiles([]contract.Node{
		{File: "app/migrations/1.sql", Attributes: map[string]any{"package": "app/Cargo.toml"}},
		{File: "app/migrations/2.sql", Attributes: map[string]any{"package": "app/Cargo.toml"}},
		{File: "migrations/1.sql", Attributes: map[string]any{"package": "Cargo.toml"}},
		{File: "other/1.sql", Attributes: map[string]any{"package": nil}},
	})
	want := []string{"Cargo.toml", "app/Cargo.toml", "app/migrations/1.sql", "app/migrations/2.sql", "app/sqlx.toml", "migrations/1.sql", "other/1.sql", "sqlx.toml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files %v", got)
	}
}

func TestManifestLists(t *testing.T) {
	diag := func(file, code, message string) contract.Diagnostic {
		return contract.Diagnostic{File: file, Code: code, Message: message}
	}
	manifests := []string{"a/Cargo.toml", "b/Cargo.toml", "c/Cargo.toml", "d/Cargo.toml", "e/Cargo.toml", "f/Cargo.toml", "Cargo.toml"}
	migrations := []contract.Node{
		migrationNode("m1", map[string]any{"package": "a/Cargo.toml"}),
		migrationNode("m2", map[string]any{"package": num("1")}),
	}
	diagnostics := []contract.Diagnostic{
		// a: a set with records; its manifest diagnostics belong to it.
		diag("a/Cargo.toml", "schema_member_unreadable", "x"),
		// b: every member unreadable, with a sqlx.toml.
		diag("b/Cargo.toml", "schema_member_unreadable", "x"),
		diag("b/Cargo.toml", "schema_member_unreadable", "y"),
		diag("b/sqlx.toml", "schema_unsupported_config", "z"),
		diag("b/Cargo.toml", "other_code", "w"),
		// c: a failed set, listed in failed_sets only.
		diag("c/Cargo.toml", "schema_member_unreadable", "x"),
		diag("c/Cargo.toml", "schema_internal_error", "boom"),
		// d: an unreadable manifest, not a set.
		diag("d/Cargo.toml", "schema_manifest_unreadable", "bad"),
		// e: diagnostics of other passes only.
		diag("e/Cargo.toml", "toml_parse", "x"),
		// f: no diagnostics. Root: an unknown future schema code.
		diag("Cargo.toml", "schema_future_code", "x"),
		diag("sqlx.toml", "schema_unsupported_config", "x"),
	}
	got := setsWithoutRecords(manifests, migrations, diagnostics)
	want := []schemaRecordlessSet{
		{Package: "Cargo.toml", DiagnosticCounts: map[string]int{"schema_future_code": 1, "schema_unsupported_config": 1}},
		{Package: "b/Cargo.toml", DiagnosticCounts: map[string]int{"other_code": 1, "schema_member_unreadable": 2, "schema_unsupported_config": 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sets without records %+v", got)
	}
	if got := failedSets(diagnostics); !reflect.DeepEqual(got, []schemaFailedSet{{Package: "c/Cargo.toml", Message: "boom"}}) {
		t.Errorf("failed sets %+v", got)
	}
	if got := unreadableManifests(append(diagnostics, diag("d/x.toml", "schema_manifest_unreadable", "not a manifest"))); !reflect.DeepEqual(got, []schemaUnreadableManifest{{File: "d/Cargo.toml", Code: "schema_manifest_unreadable", Message: "bad"}}) {
		t.Errorf("unreadable manifests %+v", got)
	}
	if got := setsWithoutRecords(nil, nil, diagnostics); len(got) != 0 || got == nil {
		t.Errorf("no manifests: %+v", got)
	}
}

// TestScanGaps checks the R4 P1 lists: every scanner not-read code on any
// path is a gap except file_read, which is one only on a Cargo.toml; the
// symlink and file_read diagnostics of a Cargo.toml are also unreadable
// manifests, with the scanner code as their source.
func TestScanGaps(t *testing.T) {
	diag := func(file, code string) contract.Diagnostic {
		return contract.Diagnostic{File: file, Code: code, Message: code + " " + file}
	}
	unfiled := []contract.Diagnostic{
		diag("vendor-link", "symlink"),
		diag("a/Cargo.toml", "symlink"),
		diag("b/Cargo.toml", "file_read"),
		diag("b/notes.md", "file_read"),
		diag("c", "scan_read"),
		diag("", "scan_read"),
		diag("", "invalid_path"),
		diag("d/.gitignore", "ignore_read"),
		diag("", "invalid_reference"),
		diag("e/Cargo.toml", "unresolved_import"),
	}
	gap := func(file, code string) schemaScanGap {
		return schemaScanGap{File: file, Code: code, Message: code + " " + file}
	}
	want := []schemaScanGap{gap("vendor-link", "symlink"), gap("a/Cargo.toml", "symlink"), gap("b/Cargo.toml", "file_read"), gap("c", "scan_read"), gap("", "scan_read"), gap("", "invalid_path"), gap("d/.gitignore", "ignore_read")}
	if got := scanGaps(unfiled); !reflect.DeepEqual(got, want) {
		t.Errorf("scan gaps %+v", got)
	}
	if got := scanGaps(nil); got == nil || len(got) != 0 {
		t.Errorf("no diagnostics: %+v", got)
	}
	wantManifests := []schemaUnreadableManifest{
		{File: "a/Cargo.toml", Code: "symlink", Message: "symlink a/Cargo.toml"},
		{File: "b/Cargo.toml", Code: "file_read", Message: "file_read b/Cargo.toml"},
	}
	if got := unreadableManifests(unfiled); !reflect.DeepEqual(got, wantManifests) {
		t.Errorf("unreadable manifests %+v", got)
	}
}

// TestScanNotReadCodes checks that scanNotRead lists exactly the codes the
// scanner writes, so a new scanner code cannot silently leave "none"
// unproven (T018b1 review).
func TestScanNotReadCodes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "scan", "scan.go"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:diagnostic|notice)\([^,]+, "([a-z0-9_]+)"`).FindAllStringSubmatch(string(src), -1) {
		found[m[1]] = true
	}
	listed := map[string]bool{}
	for code := range scanNotRead {
		listed[code] = true
	}
	if len(found) == 0 || !reflect.DeepEqual(found, listed) {
		t.Errorf("scanner codes %v, scanNotRead %v", found, listed)
	}
}
