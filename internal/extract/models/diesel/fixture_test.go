package diesel

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const fixtureDir = "../../../../testdata/fixtures/diesel-models"

// linkFixture runs the claim pass over the fixture with the tag hook set.
func linkFixture(t *testing.T) ([]contract.Node, []contract.Edge, []contract.Diagnostic, *modeltest.Tags) {
	t.Helper()
	tags := modeltest.NewTags()
	nodes, edges, diags := Link(Input{Sources: readTree(t, fixtureDir), Tag: tags.Tag})
	return nodes, edges, diags, tags
}

// TestDieselFixture is the tag half of the contract test (precision stop
// S2): over the fixture every row with a branch is reached, DI-V (no
// branch) never is, every tag is a row, every listed form of a multi-form
// row is reached and every reached form is listed; and the shared claim
// checks hold.
func TestDieselFixture(t *testing.T) {
	nodes, _, _, tags := linkFixture(t)
	modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	rowsByID := map[string]Row{}
	for _, r := range Contract {
		rowsByID[r.ID] = r
		switch {
		case r.Side == SideNone && tags.Rows[r.ID]:
			t.Errorf("row %s has no branch but was tagged", r.ID)
		case r.Side != SideNone && !tags.Rows[r.ID]:
			t.Errorf("row %s is never reached by the fixture", r.ID)
		}
		for _, f := range r.Forms {
			if !tags.Forms[r.ID+"/"+f] {
				t.Errorf("row %s: form %q is never reached by the fixture", r.ID, f)
			}
		}
	}
	for row := range tags.Rows {
		if _, ok := rowsByID[row]; !ok {
			t.Errorf("tag %s is not a contract row", row)
		}
	}
	for form := range tags.Forms {
		id, f, _ := strings.Cut(form, "/")
		found := false
		for _, listed := range rowsByID[id].Forms {
			found = found || listed == f
		}
		if !found {
			t.Errorf("tag %s is not a listed form", form)
		}
	}
	// Every claim names a claim row of the contract.
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		for _, c := range n.Attributes["claims"].([]models.Claim) {
			if r, ok := rowsByID[c.Row]; !ok || r.Side != SideClaim {
				t.Errorf("%s: claim %s by row %s, not a claim row", n.Identity, c.Fact, c.Row)
			}
		}
	}
}

func node(t *testing.T, nodes []contract.Node, id string) map[string]any {
	t.Helper()
	for _, n := range nodes {
		if n.Identity == id {
			return n.Attributes
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

func hasIdentity(nodes []contract.Node, prefix string) bool {
	for _, n := range nodes {
		if strings.HasPrefix(n.Identity, prefix) {
			return true
		}
	}
	return false
}

// TestDieselFixtureFacts spells out the facts the contract rows decide, so
// a golden rewrite cannot change them unnoticed.
func TestDieselFixtureFacts(t *testing.T) {
	nodes, edges, diags, _ := linkFixture(t)
	type want map[string]any
	check := func(id string, w want) {
		t.Helper()
		got := node(t, nodes, id)
		for k, v := range w {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %s = %#v, want %#v", id, k, got[k], v)
			}
		}
	}
	const pg = "pg/src/schema.rs"
	col := func(file, model, name string) string { return "column:" + file + "#" + model + "." + name }
	tbl := func(file, model string) string { return "table:" + file + "#" + model }
	// DI-C2 on postgres: the builtin spelling map.
	for name, typ := range map[string]any{
		"id": "integer", "small": "smallint", "big": "bigint", "f4": "real", "f8": "double precision", "flag": "boolean",
		"body": "text", "vc": "character varying", "vc5": "character varying(5)", "c3": "character(3)", "ch": `"char"`,
		"raw": "bytea", "day": "date", "j": "json", "jb": "jsonb", "u": "uuid", "o": "oid", "m": "money", "ip": "inet",
		"net": "cidr", "mac": "macaddr", "mac8": "macaddr8", "ci": "citext", "tv": "tsvector", "tq": "tsquery",
		"bits": "bit(3)", "vb": "bit varying", "vb8": "bit varying(8)", "x": "xml", "r4": "int4range", "r8": "int8range",
		"dr": "daterange", "nr": "numrange", "tr": "tsrange", "tzr": "tstzrange", "lsn": "pg_lsn",
		"tags": "text[]", "nums": "integer[]", "cie": "citext",
		// Families (Revision 1 O4; type unknown, below) and refused forms.
		"c": nil, "at": nil, "ts": nil, "tstz": nil, "span": nil, "amount": nil, "bit1": nil, "names": nil,
		"mood": nil, "moods": nil, "path": nil, "pt": nil, "lite": nil, "rng": nil, "q": nil, "bogus": nil, "qn": nil, "badlen": nil,
	} {
		check(col(pg, "scalars", name), want{"type": typ})
	}
	check(col(pg, "scalars", "big"), want{"nullable": true, "tool_type": "Nullable<Int8>", "primary_key": false})
	check(col(pg, "scalars", "nums"), want{"nullable": true})
	check(col(pg, "scalars", "id"), want{"nullable": false, "primary_key": true})
	check(col(pg, "scalars", "qn"), want{"nullable": nil})
	check(col(pg, "extras", "attr"), want{"nullable": nil, "type": nil, "primary_key": nil, "name": "attr"})
	check(tbl(pg, "extras"), want{"primary_key": nil, "column_set": "complete"})
	check(col(pg, "scalars", "qn"), want{"primary_key": false})
	check(col(pg, "scalars", "weird"), want{"name": "Weird Name", "attribute": "weird", "type": "text"})
	check(col(pg, "scalars", "type"), want{"name": "type", "attribute": "r#type", "type": "text"})
	check(tbl(pg, "scalars"), want{"name": "scalars", "schema": nil, "schema_known": true, "column_set": "complete", "primary_key": []string{"id"}, "dialect": "postgres", "verification": "oracle", "pins": map[string]any{"diesel": "2.3.14"}})
	check(tbl(pg, "scalars"), want{"assumptions": []string{"DI-V1", "DI-V2", "DI-V3", "DI-V4", "DI-V5", "DI-V6"}})
	// DI-T1: keys, sql_name, raw idents, the empty key list.
	check(tbl(pg, "pairs"), want{"primary_key": []string{"a", "b"}})
	check(tbl(pg, "nokey"), want{"primary_key": nil, "column_set": "complete"})
	check(tbl(pg, "lines"), want{"name": "order_lines"})
	check(tbl(pg, "match"), want{"name": "match"})
	// DI-G1: a patch-rewritten column keeps its key flag only; O-a; an added
	// column; a patch-added table.
	check(col(pg, "users", "email"), want{"type": nil, "nullable": nil, "primary_key": false, "tool_type": "Nullable<Varchar>"})
	check(tbl(pg, "users"), want{"column_set": "complete", "primary_key": []string{"id"}})
	check(tbl(pg, "legacy"), want{"column_set": "partial", "primary_key": nil})
	check(col(pg, "legacy", "kept"), want{"type": nil, "nullable": nil})
	check(tbl(pg, "tags"), want{"column_set": "partial", "primary_key": nil})
	check(col(pg, "tags", "label"), want{"type": "text"})
	check(tbl(pg, "view_counts"), want{"column_set": "partial", "primary_key": nil})
	check(col(pg, "view_counts", "n"), want{"type": nil, "nullable": nil, "tool_type": "Int8"})
	// D and DI-P1: not claimed.
	for _, id := range []string{tbl(pg, "odd"), tbl(pg, "broken_key"), tbl("pg/src/lexbad.rs", "unterminated"), tbl("worker/src/jobs.rs", "temp_ids"), tbl("v1/src/schema.rs", "users")} {
		if hasIdentity(nodes, id) {
			t.Errorf("%s is claimed", id)
		}
	}
	// DI-J1: generated joinable! lines to a single generated key.
	fks := map[string]string{}
	for _, e := range edges {
		if e.Kind == "foreign_key" {
			fks[e.Source] = e.Target
		}
	}
	for src, dst := range map[string]string{
		col(pg, "posts", "author_id"):                   col(pg, "users", "id"),
		col(pg, "notes", "post_id"):                     col(pg, "posts", "id"),
		col(pg, "lines", "post_id"):                     col(pg, "posts", "id"),
		col(pg, "pairs", "user_id"):                     col(pg, "users", "id"),
		col("mysql/src/schema.rs", "items", "owner_id"): col("mysql/src/schema.rs", "owners", "id"),
		// SQLite: no file proves diesel_cli 2.1, and none needs to
		// (Revision 2 addendum 2).
		col("sqlite/src/schema.rs", "posts", "author_id"): col("sqlite/src/schema.rs", "users", "id"),
	} {
		if fks[src] != dst {
			t.Errorf("foreign key %s -> %q, want %s", src, fks[src], dst)
		}
		delete(fks, src)
	}
	// sqlite users -> logs: the parent key may be the implicit rowid (O-b).
	if len(fks) != 0 {
		t.Errorf("other foreign keys %v", fks)
	}
	check(col(pg, "posts", "editor_id"), want{"foreign_keys": nil})
	check(col(pg, "posts", "id"), want{"foreign_keys": nil})
	// The audit section: schema, module, a custom type with a schema.
	check(tbl("pg/src/audit.rs", "audit.events"), want{"schema": "audit", "name": "events"})
	check(col("pg/src/audit.rs", "audit.events", "level"), want{"type": nil, "type_family": models.TypeFamily("audit.level", models.ModifiersUnknown)})
	// A pg_catalog struct names the type alone (format_type's spelling);
	// a schema-qualified #[sql_name] (diesel_cli 2.3) is D.
	check(col("pg/src/audit.rs", "audit.events", "search"), want{"type": "tsvector"})
	if hasIdentity(nodes, tbl("pg/src/audit.rs", "audit.Old_Names")) {
		t.Errorf("a table with a schema-qualified #[sql_name] is claimed")
	}
	// Hand-written files: names only.
	check(tbl("pg/src/broken.rs", "accounts"), want{"column_set": "partial", "assumptions": []string{"DI-V1", "DI-V4", "DI-V5"}, "verification": "mapped"})
	check(col("pg/src/broken.rs", "accounts", "balance"), want{"type": nil, "nullable": nil})
	check(col("pg/src/nohead.rs", "handmade", "note"), want{"type": nil, "nullable": nil})
	// DI-U2: the unconfigured schema.rs.
	check(tbl("worker/src/schema.rs", "background_jobs"), want{"schema_known": false, "assumptions": []string{"DI-V1", "DI-V4", "DI-V7"}, "column_set": "partial"})
	check(col("worker/src/schema.rs", "background_jobs", "created_at"), want{"type": nil, "nullable": nil, "tool_type": "Timestamp"})
	// Type families (Revision 1 O4, ADR 0065): the family, the dropped
	// modifiers unknown, claimed by DI-C2.
	family := func(name string, unknown ...string) want {
		w := want{"type": nil, "type_family": models.TypeFamily(name, unknown...)}
		for _, m := range unknown {
			w[m] = nil
		}
		return w
	}
	for name, w := range map[string]want{
		"c": family("character", "length"), "at": family("time without time zone", "precision"),
		"ts": family("timestamp without time zone", "precision"), "tstz": family("timestamp with time zone", "precision"),
		"span": family("interval", "precision"), "amount": family("numeric", "precision", "scale"),
		"bit1": family("bit", "length"), "names": family("character varying[]", "length"),
	} {
		check(col(pg, "scalars", name), w)
	}
	check(col(pg, "scalars", "amount"), want{"tool_type": "Numeric"})
	// Revision 2, A2b: a custom type of a 2.3 sql_types struct (an enum
	// or an extension type) is its family with the generic modifiers
	// unknown (TestDieselV24Output checks a pg_catalog struct).
	modifiers := func(name string) want {
		return want{"type": nil, "type_family": models.TypeFamily(name, models.ModifiersUnknown)}
	}
	check(col(pg, "scalars", "mood"), modifiers("mood"))
	check(col(pg, "scalars", "moods"), modifiers("mood[]"))
	check(col(pg, "scalars", "path"), modifiers("ext.ltree"))
	// Revision 2, A4 and A2a: diesel_cli 2.4 output. An enum_type struct
	// is the plain type, timetz its family, another custom type its
	// family; the rust_types module is no definition (no marker).
	const v24 = "pg/src/v24.rs"
	for name, w := range map[string]want{
		"mood": {"type": "mood"}, "moods": {"type": "mood[]"}, "level": {"type": "audit.level", "nullable": true},
		"opens": family("time with time zone", "precision"), "opens_all": family("time with time zone[]", "precision"),
		"outline": modifiers("shape"),
	} {
		check(col(v24, "people24", name), w)
	}
	check(tbl(v24, "people24"), want{"column_set": "complete", "verification": "oracle"})
	// Revision 2 addendum: a file counts as diesel_cli 2.1
	// or later output only when it shows it, whatever the pin (2.3.14
	// here). v24.rs does by a database comment as a table's doc comment
	// and schema.rs by #[max_length]: no DI-V9. audit.rs shows neither:
	// DI-V9, and its joinable! is no foreign key; patched.rs's only
	// #[max_length] is patch-added, which proves nothing.
	for _, id := range []string{tbl(v24, "people24"), tbl(pg, "users")} {
		if list, _ := node(t, nodes, id)["assumptions"].([]string); slices.Contains(list, AssumeOlderPin) {
			t.Errorf("%s: assumptions %v list DI-V9", id, list)
		}
	}
	for _, id := range []string{tbl("pg/src/audit.rs", "audit.events"), tbl("pg/src/patched.rs", "patched_users")} {
		if list, _ := node(t, nodes, id)["assumptions"].([]string); !slices.Contains(list, AssumeOlderPin) {
			t.Errorf("%s: assumptions %v do not list DI-V9", id, list)
		}
	}
	check(col("pg/src/audit.rs", "audit.events", "actor_id"), want{"foreign_keys": nil, "type": "bigint"})
	// Revision 2, A3: a patch-added #[max_length] above a generated column
	// makes the column hand-written (names only, mapped); a patch that
	// rewrites a struct's postgres_type name makes the types naming it
	// unknown.
	const patched = "pg/src/patched.rs"
	check(col(patched, "patched_users", "name"), want{"type": nil, "nullable": nil, "primary_key": false})
	check(col(patched, "patched_users", "kind"), want{"type": nil, "type_family": nil, "nullable": false})
	check(col(patched, "patched_users", "note"), want{"type": "text"})
	check(tbl(patched, "patched_users"), want{"column_set": "complete", "primary_key": []string{"id"}})
	// Revision 2, A1: a diesel 2.0 pin. Varchar and Varbit without
	// #[max_length] are their families, no joinable! is a foreign key,
	// and the group lists DI-V9.
	const old = "pg20/src/schema.rs"
	for name, w := range map[string]want{
		"name": family("character varying", "length"), "bits": family("bit varying", "length"),
		"tags": family("character varying[]", "length"), "body": {"type": "text"},
	} {
		check(col(old, "old_users", name), w)
	}
	check(col(old, "old_posts", "user_id"), want{"foreign_keys": nil, "type": "integer"})
	check(tbl(old, "old_users"), want{"assumptions": []string{"DI-V1", "DI-V2", "DI-V3", "DI-V4", "DI-V5", "DI-V6", "DI-V9"}})
	// mysql (mapped).
	const my = "mysql/src/schema.rs"
	for name, w := range map[string]want{
		"code": family("varchar", "length"), "price": family("decimal", "precision", "scale"), "seen": family("datetime", "precision"),
		"state": {"type": nil, "type_family": nil},
	} {
		check(col(my, "items", name), w)
	}
	for name, typ := range map[string]any{"id": "integer", "flag": "bool", "tiny": "tinyint", "big": "bigint unsigned", "name": "varchar(64)", "body": "text", "meta": "json", "made": "year", "code": nil, "price": nil, "seen": nil, "state": nil} {
		check(col(my, "items", name), want{"type": typ})
	}
	check(col(my, "owners", "login"), want{"type": "char(32)"})
	check(tbl(my, "items"), want{"dialect": "mysql", "verification": "mapped", "primary_key": []string{"id"}})
	// sqlite (mapped): types unknown, the implicit rowid (O-b, as Revision
	// 1 addendum 3 changed it: not claimed, so the column set is not
	// proven).
	const lite = "sqlite/src/schema.rs"
	check(tbl(lite, "logs"), want{"primary_key": nil, "column_set": "partial", "dialect": "sqlite"})
	if hasIdentity(nodes, col(lite, "logs", "rowid")) {
		t.Errorf("the implicit rowid is claimed as a column")
	}
	check(col(lite, "users", "id"), want{"type": nil, "nullable": true, "primary_key": true})
	check(tbl(lite, "users"), want{"primary_key": []string{"id"}})
	// An unknown dialect: no dialect, no types, nullability mapped.
	check(tbl("unknown/src/schema.rs", "things"), want{"verification": "mapped", "assumptions": []string{"DI-V1", "DI-V2", "DI-V5", "DI-V6", "DI-V9"}})
	check(col("unknown/src/schema.rs", "things", "label"), want{"type": nil, "nullable": true})
	if _, ok := node(t, nodes, tbl("unknown/src/schema.rs", "things"))["dialect"]; ok {
		t.Errorf("an unknown dialect is written")
	}
	// Markers.
	var msgs []string
	for _, d := range diags {
		msgs = append(msgs, d.Code+" "+d.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, w := range []string{
		"schema_model_migrations pg/diesel.toml: " + MigrationsMarker + "pg/migrations",
		"macro diesel::define_sql_function! (row D)",
		"schema_model_import_failure pg/src/schema.rs:",
		"diesel::table! odd with the attribute allow (row D)",
		"schema_model_unread pg/src/nohead.rs:3: macro infer_schema!",
		"schema_model_unread pg/src/filtered.rs:1: the diesel.toml section that configures this file sets filter",
		"table view_counts is added by the patch_file pg/src/schema.patch",
		"schema_model_unread pg/src/lexbad.rs: the schema file could not be read",
		"schema_model_not_proven pg/diesel.toml: [print_schema.missing] file pg/src/missing.rs does not exist",
		"schema_model_not_proven pg/diesel.toml: [print_schema.stdout] has no file",
		"schema_model_not_proven pg/src/broken.rs:",
		"schema_model_unread pg/src/audit.rs:36: diesel::table! audit.Old_Names with #[sql_name = \"audit.Old Names\"], a schema-qualified name",
		"schema_model_not_proven v1/diesel.toml: [print_schema] names v1/src/schema.rs, but Diesel 1",
	} {
		if !strings.Contains(all, w) {
			t.Errorf("no diagnostic %q in\n%s", w, all)
		}
	}
	if strings.Contains(all, "worker/src/jobs.rs") {
		t.Errorf("table! outside schema.rs is not silent")
	}
}
