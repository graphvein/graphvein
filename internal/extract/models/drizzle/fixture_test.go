package drizzle

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	fixtureDir    = "../../../../testdata/fixtures/drizzle-models"
	fixtureGolden = fixtureDir + "/link.golden.json"
)

// fixtureSources reads the fixture as repository-relative sources; the
// README and the golden are expectations, not inputs.
func fixtureSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	err := filepath.WalkDir(fixtureDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(fixtureDir, p)
		rel = filepath.ToSlash(rel)
		if rel == "README.md" || strings.HasSuffix(rel, ".golden.json") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// linkFixture runs the claim pass over the fixture with the tag hook set.
func linkFixture(t *testing.T) ([]contract.Node, []contract.Edge, []contract.Diagnostic, *modeltest.Tags) {
	t.Helper()
	in := input(t, fixtureSources(t))
	tags := modeltest.NewTags()
	in.Tag = tags.Tag
	nodes, edges, diags := Link(in)
	return nodes, edges, diags, tags
}

// TestDrizzleFixture is the tag half of the contract test, run over the
// claim pass on the fixture: the golden of its records and diagnostics,
// the kit's shared claim checks (modeltest.CheckClaims), and the tags:
// every row with a branch reached (all but DR-V, which only names the
// assumptions), every form a row lists reached, every tag a contract row,
// every claim by a row that claims.
func TestDrizzleFixture(t *testing.T) {
	nodes, edges, diags, tags := linkFixture(t)
	golden.Check(t, fixtureGolden, store.Snapshot{Nodes: nodes, Edges: edges, Diagnostics: diags})
	claims := modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	if len(claims) == 0 {
		t.Fatal("no column was emitted")
	}
	for _, r := range Contract {
		if r.ID != "DR-V" && !tags.Rows[r.ID] {
			t.Errorf("row %s is never reached by the fixture", r.ID)
		}
	}
	for _, r := range Contract {
		if r.ID == "DR-K1" {
			continue // discovery forms: TestDiscoverScopeForms
		}
		for _, f := range r.Forms {
			if !tags.Forms[r.ID+"/"+f] {
				t.Errorf("row %s: form %s is never reached by the fixture", r.ID, f)
			}
		}
	}
	for row := range tags.Rows {
		found := false
		for _, r := range Contract {
			found = found || r.ID == row
		}
		if !found {
			t.Errorf("tag %s is not a contract row", row)
		}
	}
	// Every claim names a row of the contract, and a row that claims.
	for id, list := range claims {
		for _, c := range list {
			ok := false
			for _, r := range Contract {
				ok = ok || r.ID == c.Row && (r.Side == SideClaim || r.ID == "D")
			}
			if !ok {
				t.Errorf("%s: claim %s by row %s, not a claim row", id, c.Fact, c.Row)
			}
		}
	}
}

func nodeAttrs(t *testing.T, nodes []contract.Node, id string) map[string]any {
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

// TestDrizzleFixtureFacts spells out the facts the rows decide, so a
// golden rewrite cannot change them unnoticed.
func TestDrizzleFixtureFacts(t *testing.T) {
	nodes, _, diags, _ := linkFixture(t)
	const pg = "pg/db/schema.ts"
	col := func(file, table, key string) map[string]any {
		return nodeAttrs(t, nodes, "column:"+file+"#"+table+"."+key)
	}
	type want map[string]any
	check := func(name string, got map[string]any, w want) {
		t.Helper()
		for k, v := range w {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %s = %#v, want %#v", name, k, got[k], v)
			}
		}
	}
	null := any(nil)
	// Types as the catalog shows them (DR-C1), DR-C3, DR-E1.
	types := map[string]string{
		"id": "integer", "small": "smallint", "big": "bigint", "name": "character varying(20)", "code": "character(3)",
		"flag": "character(1)", "label": "character varying", "note": "text", "ratio": "real", "score": "double precision",
		"price": "numeric(10,2)", "whole": "numeric(8,0)", "anyNumber": "numeric", "total": "bigint", "active": "boolean",
		"createdAt": "timestamp without time zone", "at3": "timestamp(3) with time zone", "day": "date",
		"clock": "time(2) without time zone", "span": "interval", "token": "uuid", "ip": "inet", "meta": "json",
		"doc": "jsonb", "tags": "text[]", "nums": "integer[]", "state": "status", "states": "status[]",
		"feeling": "mood", "role": "app.app_role", "data": "bytea", "rollback": "integer",
	}
	for key, typ := range types {
		check("kinds."+key, col(pg, "kinds", key), want{"type": typ})
	}
	check("kinds.addr", col(pg, "kinds", "addr"), want{"type": null, "name": "addr"})
	check("kinds.wide", col(pg, "kinds", "wide"), want{"type": null})
	check("kinds.id", col(pg, "kinds", "id"), want{"nullable": false, "primary_key": true, "default": "server", "server_default": true, "server_default_text": null})
	check("kinds.small", col(pg, "kinds", "small"), want{"nullable": false, "primary_key": false})
	check("kinds.name", col(pg, "kinds", "name"), want{"nullable": false, "unique": true, "default": "absent", "server_default": false, "index": false, "foreign_keys": []map[string]any{}})
	check("kinds.code", col(pg, "kinds", "code"), want{"nullable": true, "unique": false})
	check("kinds.rollback", col(pg, "kinds", "rollback"), want{"name": "rollback", "nullable": false})
	// Default texts (bin.cjs:17021-17045).
	texts := map[string]any{
		"count": "0", "neg": "-1", "ratio": "1.5", "active": "true", "createdAt": "now()", "token": "gen_random_uuid()",
		"meta": `'{"a":1,"b":[true,null]}'::json`, "doc": "'[1,2]'::jsonb", "prefs": `'{"theme":"dark","size":2}'::jsonb`,
		"quoted": "'it''s'", "tags": `'{"a","b"}'`, "stamped": "(now() AT TIME ZONE 'utc')", "raw": "'x'", "state": "'active'",
	}
	for key, text := range texts {
		check("kinds."+key, col(pg, "kinds", key), want{"default": "server", "server_default": true, "server_default_text": text})
	}
	check("kinds.client", col(pg, "kinds", "client"), want{"default": null, "server_default": false})
	check("kinds.touched", col(pg, "kinds", "touched"), want{"default": null, "server_default": false})
	check("kinds.typed", col(pg, "kinds", "typed"), want{"default": "absent", "type": "jsonb"})
	check("kinds.feeling", col(pg, "kinds", "feeling"), want{"enum": map[string]any{"name": "mood", "schema": "public", "values": []string{"sad", "ok", "happy"}}})
	check("kinds.ident", col(pg, "kinds", "ident"), want{"identity": "always", "nullable": false, "default": null})
	check("kinds.ident2", col(pg, "kinds", "ident2"), want{"identity": "by default", "nullable": false})
	kinds := nodeAttrs(t, nodes, "table:"+pg+"#kinds")
	check("kinds", kinds, want{"name": "kinds", "schema": "public", "state": "complete", "primary_key": []string{"id"}, "dialect": "postgresql", "verification": "oracle"})
	wantUniques := []map[string]any{
		{"name": "kinds_name_unique", "columns": []string{"name"}, "nulls_not_distinct": false},
		{"name": "kinds_uniq_key", "columns": []string{"uniq"}, "nulls_not_distinct": true},
	}
	if !reflect.DeepEqual(kinds["unique_constraints"], wantUniques) {
		t.Errorf("kinds unique constraints %#v", kinds["unique_constraints"])
	}
	// Unknown defaults and modifiers.
	check("unknowns.nul", col(pg, "unknowns", "nul"), want{"default": "absent", "server_default": false})
	for _, key := range []string{"late", "dyn", "when"} {
		check("unknowns."+key, col(pg, "unknowns", key), want{"default": null, "server_default": null, "nullable": true})
	}
	check("unknowns.odd", col(pg, "unknowns", "odd"), want{"nullable": null, "default": null, "type": "text"})
	check("unknowns.pre", col(pg, "unknowns", "pre"), want{"nullable": null, "type": "text[]"})
	check("unknowns.owner", col(pg, "unknowns", "owner"), want{"nullable": true})
	if fk := col(pg, "unknowns", "owner")["foreign_keys"].([]map[string]any); len(fk) != 1 || fk[0]["name"] != "unknowns_owner_kinds_id_fk" || fk[0]["status"] != "resolved" {
		t.Errorf("unknowns.owner foreign keys %v", fk)
	}
	check("unknowns.createdOn", col(pg, "unknowns", "createdOn"), want{"name": "created_on", "nullable": false, "server_default_text": "now()"})
	check("unknowns", nodeAttrs(t, nodes, "table:"+pg+"#unknowns"), want{"constraint_set": "partial", "column_set": "complete"})
	check("people.full", col(pg, "people", "full"), want{"generated": "stored", "generated_expression": `"people"."first" || ' ' || "people"."last"`})
	check("people.constant", col(pg, "people", "constant"), want{"generated": "stored", "generated_expression": "'c'"})
	check("indexed.a", col(pg, "indexed", "a"), want{"nullable": true, "primary_key": false, "index": true})
	check("indexed.b", col(pg, "indexed", "b"), want{"nullable": false, "index": false})
	check("indexed", nodeAttrs(t, nodes, "table:"+pg+"#indexed"), want{"constraint_set": "complete", "primary_key": []string{"id"}})
	check("appUsers", nodeAttrs(t, nodes, "table:"+pg+"#appUsers"), want{"name": "users", "schema": "app"})
	for _, absent := range []string{"secret", "usesHidden", "inHidden", "dupA", "dupB", "twice", "twoUniques", "dynamic", "callback", "rls", "mutable", "wrongDialect", "persons"} {
		if hasIdentity(nodes, "table:"+pg+"#"+absent) {
			t.Errorf("table %s is claimed", absent)
		}
	}
	check("partial", nodeAttrs(t, nodes, "table:"+pg+"#partial"), want{"column_set": "partial"})
	check("partial.named", col(pg, "partial", "named"), want{"state": "unknown"})
	// Casing (DR-K2, DR-K3, A12).
	check("cased.userID", col("cased/schema.ts", "casedTable", "userID"), want{"name": "user_id"})
	check("cased.HTMLParser", col("cased/schema.ts", "casedTable", "HTMLParser"), want{"name": "html_parser"})
	check("cased.ExplicitName", col("cased/schema.ts", "casedTable", "ExplicitName"), want{"name": "ExplicitName"})
	cased := nodeAttrs(t, nodes, "table:cased/schema.ts#casedTable")
	if u := cased["unique_constraints"].([]map[string]any); len(u) != 1 || u[0]["name"] != "cased_table_displayName_unique" {
		t.Errorf("cased unique constraints %v", u)
	}
	check("cased", cased, want{"assumptions": []string{"DR-V1", "DR-V2", "DR-V3", "DR-V4", "DR-V5"}})
	check("camel.display_name", col("camel/schema.ts", "camelTable", "display_name"), want{"name": "displayName"})
	check("camel.api_key_v2", col("camel/schema.ts", "camelTable", "api_key_v2"), want{"name": "apiKeyV2"})
	// SQLite (A10).
	const lite = "sqlite/src/schema.ts"
	notes := nodeAttrs(t, nodes, "table:"+lite+"#notes")
	check("notes", notes, want{"schema": null, "dialect": "sqlite", "unique_constraints": []map[string]any{}, "set": "models/drizzle/sqlite/config/sqlite.config.ts"})
	if ix := notes["indexes"].([]map[string]any); len(ix) != 1 || ix[0]["name"] != "notes_title_unique" || ix[0]["unique"] != true {
		t.Errorf("notes indexes %v", ix)
	}
	check("notes.id", col(lite, "notes", "id"), want{"type": "integer", "nullable": false, "default": null})
	check("notes.title", col(lite, "notes", "title"), want{"type": "text(40)"})
	check("notes.meta", col(lite, "notes", "meta"), want{"type": "text", "server_default_text": `'{"a":1}'`})
	check("notes.done", col(lite, "notes", "done"), want{"server_default_text": "false"})
	check("notes.stamp", col(lite, "notes", "stamp"), want{"server_default_text": "(unixepoch())"})
	for _, absent := range []string{"left", "right"} {
		if hasIdentity(nodes, "table:"+lite+"#"+absent) {
			t.Errorf("sqlite table %s is claimed", absent)
		}
	}
	// MySQL (mapped, DR-V6, A9).
	const my = "mysql/schema.ts"
	items := nodeAttrs(t, nodes, "table:"+my+"#items")
	check("items", items, want{"verification": "mapped", "assumptions": []string{"DR-V1", "DR-V2", "DR-V4", "DR-V5", "DR-V6"}})
	check("items.big", col(my, "items", "big"), want{"type": "bigint unsigned"})
	check("items.kind", col(my, "items", "kind"), want{"type": "enum('a','b')"})
	check("items.at", col(my, "items", "at"), want{"type": "datetime(3)"})
	// The catalog form MySQL reports: serial is bigint
	// unsigned, boolean tinyint(1), char char(1).
	check("items.seq", col(my, "items", "seq"), want{"type": "bigint unsigned", "nullable": null, "default": null})
	check("items.flag", col(my, "items", "flag"), want{"type": "tinyint(1)", "default": "server", "server_default_text": null})
	check("items.name", col(my, "items", "name"), want{"type": "varchar(50)"})
	// AUTO_INCREMENT is not a default; defaultNow() is a default
	// (presence); onUpdateNow() alone leaves only the default unknown.
	check("counters.id", col(my, "counters", "id"), want{"type": "int", "nullable": false, "primary_key": true, "default": null, "server_default": null})
	check("counters.createdAt", col(my, "counters", "createdAt"), want{"type": "timestamp", "nullable": false, "default": "server", "server_default": true, "server_default_text": null})
	check("counters.updatedAt", col(my, "counters", "updatedAt"), want{"type": "timestamp", "nullable": true, "unique": false, "default": null, "server_default": null})
	check("counters.touchedAt", col(my, "counters", "touchedAt"), want{"type": "timestamp(3)", "default": "server", "server_default": true})
	check("counters.code", col(my, "counters", "code"), want{"type": "char(1)"})
	// A foreign key to a column that is unique only through a unique
	// index is unknown: drizzle-kit writes foreign keys before indexes,
	// so from one generate Postgres rejects it (oracle run, review
	// DR-P1); one to a column that is neither a key nor unique is DR-P1
	// (byEmail, below).
	byCode := col("pg/db/review/refs.ts", "byCode", "code")
	if fks := fmt.Sprint(byCode["foreign_keys"], byCode["reasons"]); byCode["foreign_keys"] != nil || !strings.Contains(fks, "unique only through a unique index") {
		t.Errorf("byCode.code: foreign keys %s", fks)
	}
	if hasIdentity(nodes, "table:pg/db/review/refs.ts#byEmail") || hasIdentity(nodes, "table:pg/db/review/reexported.ts#") || hasIdentity(nodes, "table:pg/db/review/creator.ts#") {
		t.Error("a review table that is not claimed is claimed")
	}
	// An enum default in a non-public schema (oracle normalisation, C1).
	check("appUsers.role", col(pg, "appUsers", "role"), want{"server_default_text": "'member'"})
	if hasIdentity(nodes, "table:"+my+"#inOther") || hasIdentity(nodes, "table:pgschema/schema.ts#events") {
		t.Error("a schema table drizzle-kit does not create is claimed")
	}
	if !hasIdentity(nodes, "table:pgschema/schema.ts#plain") || hasIdentity(nodes, "table:noconfig/") || hasIdentity(nodes, "table:v1/") || hasIdentity(nodes, "table:pg/old-schema.ts") {
		t.Error("discovery scope")
	}
	for _, want := range []struct{ code, file, part string }{
		{codeImportFailure, pg, "enum hidden"},
		{codeImportFailure, pg, "pgSchema hiddenSchema"},
		{codeImportFailure, pg, "2 exported tables are named public.dup"},
		{codeImportFailure, pg, `both named "x"`},
		{codeImportFailure, pg, "unique constraint u"},
		{codeImportFailure, lite, "unique index name code_idx"},
		{codeUnread, pg, "dynamic: the table name is not a string literal"},
		{codeUnread, pg, "callback: the columns are not an object literal"},
		{codeUnread, pg, ".enableRLS()"},
		{codeUnread, pg, "held by let"},
		{codeUnread, pg, "a sqlite table builder"},
		{codeUnread, my, "bin.cjs:16271"},
		{codeNotClaimed, "pgschema/schema.ts", `schema "audit"`},
		{codeUnread, "pg/db/review/reexported.ts", "1 exported value(s) not read (viaCore)"},
		{codeUnread, "pg/db/review/creator.ts", `it builds the table(s) "created"`},
		{codeImportFailure, "pg/db/review/refs.ts", "authors(email) is neither the primary key nor unique"},
	} {
		if !hasDiag(diags, want.code, want.file, want.part) {
			t.Errorf("no %s diagnostic on %s with %q", want.code, want.file, want.part)
		}
	}
}

// TestDrizzleFixtureKeys spells out the key, extra-config and relation
// facts (rows DR-C2 references, DR-X1, DR-K3, DR-R1, DR-P1), checked
// against drizzle-kit 0.31.11's export (postgresql, sqlite) and generate
// (casing) output and a scratch Postgres 17.11 catalog in step G2B4.
func TestDrizzleFixtureKeys(t *testing.T) {
	nodes, edges, diags, _ := linkFixture(t)
	const keys = "pg/db/keys.ts"
	type want map[string]any
	check := func(name string, got map[string]any, w want) {
		t.Helper()
		for k, v := range w {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %s = %#v, want %#v", name, k, got[k], v)
			}
		}
	}
	col := func(file, table, key string) map[string]any {
		return nodeAttrs(t, nodes, "column:"+file+"#"+table+"."+key)
	}
	fk := func(file, table, key string) map[string]any {
		t.Helper()
		list, _ := col(file, table, key)["foreign_keys"].([]map[string]any)
		if len(list) != 1 {
			t.Fatalf("%s.%s: foreign keys %v", table, key, list)
		}
		return list[0]
	}
	null := any(nil)
	// Inline references (DR-C2): actions, default no action, self, a
	// target the group does not claim, a name truncated to 63 bytes.
	check("authors.kindId", fk(keys, "authors", "kindId"), want{"name": "authors_kind_id_kinds_id_fk", "table": "kinds", "schema": "public", "column": "id", "on_delete": "cascade", "on_update": "set null", "status": "resolved"})
	check("authors.mentorId", fk(keys, "authors", "mentorId"), want{"name": "authors_mentor_id_authors_id_fk", "on_delete": "no action", "on_update": "no action"})
	check("authors.rlsId", col(keys, "authors", "rlsId"), want{"foreign_keys": null})
	check("longChild", fk(keys, "longChild", "parentReferenceIdentifier"), want{"name": "a_very_long_child_table_name_for_truncation_parent_reference_id"})
	// Extra config (DR-X1).
	posts := nodeAttrs(t, nodes, "table:"+keys+"#posts")
	check("posts", posts, want{"state": "complete", "primary_key": []string{"author_id", "slug"}, "primary_key_name": "posts_author_id_slug_pk",
		"unique_constraints": []map[string]any{
			{"name": "posts_title_body_unique", "columns": []string{"title", "body"}, "nulls_not_distinct": true},
			{"name": "posts_slug_key", "columns": []string{"slug"}, "nulls_not_distinct": false},
		},
		"check_constraints": []map[string]any{{"name": "posts_score_check", "expression": `"posts"."score" >= 0`}},
	})
	ix := map[string]map[string]any{}
	for _, x := range posts["indexes"].([]map[string]any) {
		ix[x["name"].(string)] = x
	}
	check("posts_published_at_score_index", ix["posts_published_at_score_index"], want{"columns": []any{"published_at", "score"}, "orders": []any{"desc", "asc"}, "nulls": []any{"last", "first"}, "method": "btree", "unique": false})
	check("posts_lower_title_idx", ix["posts_lower_title_idx"], want{"columns": []any{null}, "expressions": []any{`lower("title")`}, "unique": true})
	check("posts_title_live_idx", ix["posts_title_live_idx"], want{"partial": true, "where": `"posts"."published_at" is not null`, "unique": true})
	check("posts_recent_idx", ix["posts_recent_idx"], want{"method": "brin", "partial": true})
	check("posts_body_idx", ix["posts_body_idx"], want{"opclasses": []any{"text_pattern_ops"}, "storage_parameters": null})
	check("posts.authorId", col(keys, "posts", "authorId"), want{"nullable": false, "primary_key": true, "unique": false, "index": false})
	check("posts.slug", col(keys, "posts", "slug"), want{"unique": true, "primary_key": true})
	check("posts.title", col(keys, "posts", "title"), want{"unique": null, "index": true, "nullable": true})
	check("posts.authorId fk", fk(keys, "posts", "authorId"), want{"name": "posts_author_id_authors_id_fk", "on_delete": "cascade", "on_update": "no action"})
	check("comments.postSlug", fk(keys, "comments", "postSlug"), want{"name": "comments_post_fk", "local_columns": []string{"post_author", "post_slug"}, "columns": []string{"author_id", "slug"}, "column": "slug", "on_update": "cascade"})
	check("oddExtra", nodeAttrs(t, nodes, "table:"+keys+"#oddExtra"), want{"constraint_set": "partial", "primary_key": null})
	check("oddExtra.b", col(keys, "oddExtra", "b"), want{"nullable": null, "primary_key": null})
	check("blockExtra.a", col(keys, "blockExtra", "a"), want{"index": null})
	// Casing (DR-K3, A12).
	child := nodeAttrs(t, nodes, "table:cased/schema.ts#casedChild")
	check("casedChild", child, want{"primary_key_name": "cased_child_parent_id_other_key_pk",
		"unique_constraints": []map[string]any{{"name": "cased_child_otherKey_createdAt_unique", "columns": []string{"other_key", "created_at"}, "nulls_not_distinct": false}}})
	if x := child["indexes"].([]map[string]any); len(x) != 1 || x[0]["name"] != "cased_child_created_at_index" {
		t.Errorf("casedChild indexes %v", x)
	}
	check("casedChild.parentId", fk("cased/schema.ts", "casedChild", "parentId"), want{"name": "cased_child_parent_id_cased_table_user_id_fk"})
	// SQLite (A10): unnamed composite key, columns stay nullable; unnamed
	// foreign key; a one-column primaryKey() is the column's key.
	const lite = "sqlite/src/schema.ts"
	check("tags", nodeAttrs(t, nodes, "table:"+lite+"#tags"), want{"primary_key": []string{"note_id", "name"}, "primary_key_name": null,
		"check_constraints": []map[string]any{{"name": "tags_weight_check", "expression": `"tags"."weight" >= 0`}}})
	check("tags.noteId", col(lite, "tags", "noteId"), want{"nullable": true, "primary_key": true})
	check("tags.noteId fk", fk(lite, "tags", "noteId"), want{"name": null, "on_delete": "cascade"})
	check("single.code", col(lite, "single", "code"), want{"primary_key": true, "nullable": true})
	// MySQL (mapped).
	check("orders", nodeAttrs(t, nodes, "table:mysql/schema.ts#orders"), want{"primary_key": []string{"item_id", "code"}, "primary_key_name": null})
	check("orders.code", col("mysql/schema.ts", "orders", "code"), want{"nullable": false, "unique": true, "index": true})
	// Relations (DR-R1): ORM-level only, never an edge.
	rels := nodeAttrs(t, nodes, "table:"+keys+"#authors")["orm_relations"].([]map[string]any)
	if len(rels) != 4 {
		t.Fatalf("authors relations %v", rels)
	}
	check("relation kind", rels[0], want{"name": "kind", "kind": "one", "target": "table:pg/db/schema.ts#kinds", "columns": []string{"kind_id"}, "references": []string{"id"}})
	check("relation posts", rels[1], want{"kind": "many", "table": "posts", "columns": null})
	check("relation hidden", rels[3], want{"target": null, "table": null})
	for _, e := range edges {
		if e.Kind == "foreign_key" && strings.HasPrefix(e.Source, "column:"+keys+"#authors.") && e.Target == "column:"+keys+"#posts.authorId" {
			t.Error("a relation became a foreign_key edge")
		}
	}
	// DR-P1 and the relations marker.
	for _, w := range []struct{ code, file, part string }{
		{codeImportFailure, keys, "badIndex: the extra config names nope"},
		{codeImportFailure, keys, "badRef: foreign key target: authors has no column nope"},
		{codeImportFailure, keys, "both have unique constraint du"},
		{codeUnread, "pg/drizzle.config.ts", "pg/db/relations.ts (1)"},
	} {
		if !hasDiag(diags, w.code, w.file, w.part) {
			t.Errorf("no %s diagnostic on %s with %q", w.code, w.file, w.part)
		}
	}
	for _, absent := range []string{"badIndex", "badRef", "dupUnique"} {
		if hasIdentity(nodes, "table:"+keys+"#"+absent) {
			t.Errorf("table %s is claimed", absent)
		}
	}
}
