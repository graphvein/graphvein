package prisma

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const fixtureDir = "../../../../testdata/fixtures/prisma-models"

// fixtureSources reads every file of the fixture as the indexer's
// retained sources (repository-relative paths).
func fixtureSources(t *testing.T) map[string][]byte {
	t.Helper()
	sources := map[string][]byte{}
	err := filepath.WalkDir(fixtureDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixtureDir, p)
		sources[filepath.ToSlash(rel)] = data
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
	tags := modeltest.NewTags()
	claimTag = tags.Tag
	defer func() { claimTag = nil }()
	nodes, edges, diags := Link(Input{Sources: fixtureSources(t)})
	return nodes, edges, diags, tags
}

// TestPrismaFixture runs the claim pass over the fixture (the indexed
// golden is TestPrismaIndexedFixture's) and checks the shared claim
// checks, that the claim branches reach every row and every listed form,
// and that every tag is a row.
func TestPrismaFixture(t *testing.T) {
	nodes, edges, diags, tags := linkFixture(t)
	checkRecords(t, nodes)
	if len(edges) == 0 || len(diags) == 0 {
		t.Errorf("%d edges, %d diagnostics", len(edges), len(diags))
	}
	// Every form of the multi-form rows is reached.
	for _, r := range Contract {
		for _, f := range r.Forms {
			if !tags.Forms[r.ID+"/"+f] {
				t.Errorf("row %s: form %s is never reached by the fixture", r.ID, f)
			}
		}
	}
	// The full contract test (precision stop S2): every row's branch is
	// reached by the fixture and every tag is a row, so the tags emitted
	// equal the contract's rows.
	for _, r := range Contract {
		if !tags.Rows[r.ID] {
			t.Errorf("row %s is never reached by the fixture", r.ID)
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
}

func nodeByID(t *testing.T, nodes []contract.Node, id string) map[string]any {
	t.Helper()
	for _, n := range nodes {
		if n.Identity == id {
			return n.Attributes
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

func hasNode(nodes []contract.Node, prefix string) bool {
	for _, n := range nodes {
		if strings.HasPrefix(n.Identity, prefix) {
			return true
		}
	}
	return false
}

// TestPrismaFixtureFacts spells out the facts the contract rows decide,
// so a golden rewrite cannot change them unnoticed.
func TestPrismaFixtureFacts(t *testing.T) {
	nodes, _, diags, _ := linkFixture(t)
	const pg = "pg/prisma/schema.prisma"
	col := func(file, model, field string) map[string]any {
		return nodeByID(t, nodes, "column:"+file+"#"+model+"."+field)
	}
	tbl := func(file, model string) map[string]any { return nodeByID(t, nodes, "table:"+file+"#"+model) }
	type want map[string]any
	check := func(name string, got map[string]any, w want) {
		t.Helper()
		for k, v := range w {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %s = %#v, want %#v", name, k, got[k], v)
			}
		}
	}
	// PR-F1, PR-F2 (lists are nullable), PR-F3, PR-F4, PR-E1.
	check("Scalars.text", col(pg, "Scalars", "text"), want{"type": "String", "sql_type": "text", "nullable": false, "default": "absent", "server_default": false, "unique": false, "primary_key": false})
	check("Scalars.decimal", col(pg, "Scalars", "decimal"), want{"sql_type": "numeric(65,30)", "precision": 65, "scale": 30})
	check("Scalars.at", col(pg, "Scalars", "at"), want{"sql_type": "timestamp(3) without time zone", "precision": 3, "timezone": false})
	check("Scalars.list", col(pg, "Scalars", "list"), want{"sql_type": "text[]", "nullable": true})
	check("Scalars.roles", col(pg, "Scalars", "roles"), want{"type": "role_kind", "sql_type": "role_kind[]", "nullable": true})
	check("Scalars.role", col(pg, "Scalars", "role"), want{"enum": map[string]any{"name": "Role", "database_name": "role_kind", "values": []string{"admin", "user"}, "schema": nil}})
	check("Scalars.circle", col(pg, "Scalars", "circle"), want{"type": "circle", "sql_type": "circle", "nullable": true})
	// PR-A6.
	check("Natives.vc", col(pg, "Natives", "vc"), want{"sql_type": "character varying(255)", "native_type": "VarChar(255)", "length": 255})
	check("Natives.tstz", col(pg, "Natives", "tstz"), want{"sql_type": "timestamp(3) with time zone", "timezone": true, "precision": 3})
	check("Natives.bit", col(pg, "Natives", "bit"), want{"sql_type": "bit(8)", "length": 8})
	check("Natives.wrong", col(pg, "Natives", "wrong"), want{"type": nil, "sql_type": nil})
	check("Natives.unknown", col(pg, "Natives", "unknown"), want{"type": nil})
	// PR-A3 forms (a)-(e), PR-A4 and the unknown defaults.
	// server_default_text is the database's text (developer decision
	// 2026-11-09): rendered where the mapping proves it, else presence only.
	check("Defaults.id", col(pg, "Defaults", "id"), want{"server_default": true, "server_default_text": nil, "sql_type": "integer"})
	check("Defaults.label", col(pg, "Defaults", "label"), want{"default": "server", "server_default_text": `'it''s "quoted"'`})
	check("Defaults.n", col(pg, "Defaults", "n"), want{"server_default_text": "42"})
	check("Defaults.flag", col(pg, "Defaults", "flag"), want{"server_default_text": "true"})
	check("Defaults.role", col(pg, "Defaults", "role"), want{"server_default_text": "'user'"})
	check("Defaults.ratio", col(pg, "Defaults", "ratio"), want{"server_default": true, "server_default_text": nil})
	check("Defaults.tags", col(pg, "Defaults", "tags"), want{"server_default": true, "server_default_text": nil})
	check("Defaults.day", col(pg, "Defaults", "day"), want{"server_default_text": "CURRENT_TIMESTAMP", "sql_type": "date"})
	check("Defaults.touched", col(pg, "Defaults", "touched"), want{"server_default": true})
	check("Defaults.updated", col(pg, "Defaults", "updated"), want{"server_default": false, "default": nil})
	check("Defaults.gen", col(pg, "Defaults", "gen"), want{"server_default_text": "gen_random_uuid()"})
	check("Defaults.u7", col(pg, "Defaults", "u7"), want{"default": "client", "server_default": false})
	check("Defaults.seq", col(pg, "Defaults", "seq"), want{"default": nil, "server_default": nil})
	check("Defaults.bare", col(pg, "Defaults", "bare"), want{"default": nil, "server_default": nil})
	check("Defaults.named", col(pg, "Defaults", "named"), want{"server_default": nil})
	check("Ticket.title", col("sqlserver/schema.prisma", "Ticket", "title"), want{"server_default": true, "server_default_text": nil})
	// PR-A1, PR-A2, PR-A5, PR-A7, PR-AA4.
	check("Mapped", tbl(pg, "Mapped"), want{"name": "mapped_people", "primary_key": []string{"mapped_id"}, "primary_key_name": "mapped_pk", "unique_constraints": []map[string]any{}})
	check("Mapped.firstName", col(pg, "Mapped", "firstName"), want{"name": "first_name", "unique": true})
	check("Mapped.hidden", col(pg, "Mapped", "hidden"), want{"ignored": true})
	// PR-AA1 key order, PR-AA2, PR-AA3.
	comp := tbl(pg, "Composite")
	check("Composite", comp, want{"primary_key": []string{"b", "a"}, "primary_key_name": "composite_pk", "state": "complete"})
	check("Composite.e", col(pg, "Composite", "e"), want{"name": "e_col", "unique": true})
	check("Composite.c", col(pg, "Composite", "c"), want{"index": true, "unique": false})
	// @@ignore keeps the table; no @id: no primary key, proven.
	check("NoKey", tbl(pg, "NoKey"), want{"ignored": true, "primary_key": []string{}, "state": "complete"})
	// Unread attributes (D): absences withheld.
	check("Unread", tbl(pg, "Unread"), want{"constraint_set": "partial"})
	check("Unread.odd", col(pg, "Unread", "odd"), want{"unique": nil, "default": nil, "foreign_keys": nil, "type": "String"})
	check("Partial", tbl(pg, "Partial"), want{"constraint_set": "partial", "primary_key": []string{"id"}})
	// A partial unique index (where:) is D, as @@index(where:):
	// no unique claim, no unconditional unique index.
	check("Partial.name", col(pg, "Partial", "name"), want{"unique": nil, "index": nil})
	check("Partial.slug", col(pg, "Partial", "slug"), want{"unique": nil})
	check("Partial", tbl(pg, "Partial"), want{"indexes": []map[string]any{}, "unique_constraints": []map[string]any{}})
	// The unique kind is provider-dependent: SQL Server's
	// UNIQUE constraints, mysql's and cockroachdb's unique indexes
	// (mapped), unknown with a non-literal provider.
	const mss, crdb = "sqlserver/schema.prisma", "cockroach/schema.prisma"
	check("Ticket", tbl(mss, "Ticket"), want{
		"unique_constraints": []map[string]any{{"name": nil, "columns": []string{"code"}}, {"name": "UQ_ticket_flag_amount", "columns": []string{"flag", "amount"}}},
		"indexes":            []map[string]any{}, "verification": "mapped",
		"assumptions": []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4", "PR-V6"},
	})
	check("Ticket.code", col(mss, "Ticket", "code"), want{"unique": true})
	check("_LabelToTicket", tbl(mss, "_LabelToTicket"), want{
		"unique_constraints": []map[string]any{{"name": nil, "columns": []string{"A", "B"}}},
		"indexes":            []map[string]any{{"name": nil, "columns": []string{"B"}, "unique": false, "method": nil}},
		"primary_key":        []string{}, "constraint_set": "partial",
	})
	check("Member", tbl(crdb, "Member"), want{
		"unique_constraints": []map[string]any{},
		"indexes":            []map[string]any{{"name": nil, "columns": []string{"email"}, "unique": true, "method": nil}, {"name": nil, "columns": []string{"rank", "ratio"}, "unique": true, "method": nil}},
	})
	check("_MemberToTeam", tbl(crdb, "_MemberToTeam"), want{
		"unique_constraints": []map[string]any{},
		"indexes":            []map[string]any{{"name": nil, "columns": []string{"A", "B"}, "unique": true, "method": nil}, {"name": nil, "columns": []string{"B"}, "unique": false, "method": nil}},
	})
	check("Thing", tbl("dynamic/schema.prisma", "Thing"), want{"indexes": []map[string]any{}, "unique_constraints": []map[string]any{}, "constraint_set": "partial"})
	check("Thing.code", col("dynamic/schema.prisma", "Thing", "code"), want{"unique": true})
	// One schema, two packages: claimed once when the versions
	// agree on every gate (pins null: 7.10.0 and 7.9.1), else not at all.
	check("Shared", tbl("twin/schema.prisma", "Shared"), want{"pins": map[string]any(nil), "state": "complete"})
	shared := 0
	for _, n := range nodes {
		if n.Identity == "table:twin/schema.prisma#Shared" {
			shared++
		}
	}
	if shared != 1 || hasNode(nodes, "table:split/") {
		t.Errorf("twin/schema.prisma claimed %d times; split claimed: %v", shared, hasNode(nodes, "table:split/"))
	}
	// Prisma 7 rejects a datasource url.
	if hasNode(nodes, "table:urlv7/") {
		t.Error("urlv7: a schema Prisma 7 rejects emitted a table")
	}
	rejected, split := false, false
	for _, d := range diags {
		rejected = rejected || d.File == "urlv7/schema.prisma" && strings.Contains(d.Message, "schema rejected by the pinned Prisma version")
		split = split || d.File == "split/schema.prisma" && strings.Contains(d.Message, "split/legacy/package.json and split/package.json")
	}
	if !rejected || !split {
		t.Errorf("urlv7 rejection diagnostic %v, split diagnostic %v", rejected, split)
	}
	// PR-R1: explicit actions and map:, default actions, a
	// composite key, a one-to-one; D for an unknown referenced field.
	fkIn := func(schema any, table string, local, refs []string, onDelete, onUpdate, name any) []map[string]any {
		return []map[string]any{{"table": table, "schema": schema, "column": refs[0], "local_columns": local, "columns": refs, "on_delete": onDelete, "on_update": onUpdate, "name": name, "status": "resolved"}}
	}
	fk := func(table string, local, refs []string, onDelete, onUpdate, name any) []map[string]any {
		return fkIn("public", table, local, refs, onDelete, onUpdate, name)
	}
	check("Post", tbl(pg, "Post"), want{"constraint_set": "complete"})
	check("Post.authorId", col(pg, "Post", "authorId"), want{"foreign_keys": fk("Author", []string{"authorId"}, []string{"id"}, "Cascade", "NoAction", "post_author_fk")})
	check("Post.editorId", col(pg, "Post", "editorId"), want{"foreign_keys": fk("Author", []string{"editorId"}, []string{"id"}, "SetNull", "Cascade", nil)})
	check("Profile.userId", col(pg, "Profile", "userId"), want{"foreign_keys": fk("Author", []string{"userId"}, []string{"id"}, "Restrict", "Cascade", nil), "unique": true})
	childFK := fk("Parent", []string{"pa", "pb"}, []string{"pa", "pb"}, "Restrict", "Cascade", "child_parent_fk")
	check("Child.pa", col(pg, "Child", "pa"), want{"foreign_keys": childFK})
	childFK[0]["column"] = "pb"
	check("Child.pb", col(pg, "Child", "pb"), want{"foreign_keys": childFK, "nullable": true})
	check("Author.id", col(pg, "Author", "id"), want{"foreign_keys": []map[string]any{}})
	check("Broken", tbl(pg, "Broken"), want{"constraint_set": "partial"})
	check("Broken.aid", col(pg, "Broken", "aid"), want{"foreign_keys": nil})
	// PR-R3: generated and explicit names, @@map'd model, self-relation.
	join := func(name string) map[string]any { return tbl(pg, "_"+name) }
	check("_PostToTag", join("PostToTag"), want{"primary_key": []string{"A", "B"}, "indexes": []map[string]any{{"name": nil, "columns": []string{"B"}, "unique": false, "method": "BTree"}}, "schema": "public", "state": "complete"})
	check("_PostToTag.A", col(pg, "_PostToTag", "A"), want{"type": "Int", "nullable": false, "primary_key": true, "index": false, "foreign_keys": fk("Post", []string{"A"}, []string{"id"}, "Cascade", "Cascade", nil)})
	check("_PostToTag.B", col(pg, "_PostToTag", "B"), want{"index": true, "foreign_keys": fk("Tag", []string{"B"}, []string{"id"}, "Cascade", "Cascade", nil)})
	check("_post_labels.A", col(pg, "_post_labels", "A"), want{"sql_type": "uuid", "foreign_keys": fk("labels", []string{"A"}, []string{"id"}, "Cascade", "Cascade", nil)})
	check("_friends.B", col(pg, "_friends", "B"), want{"foreign_keys": fk("Person", []string{"B"}, []string{"id"}, "Cascade", "Cascade", nil)})
	// PR-P1 at model level: no table for the model.
	for _, m := range []string{"Keyless", "TwoIds", "IdBoth", "_KeylessToPost"} {
		if hasNode(nodes, "table:"+pg+"#"+m) {
			t.Errorf("model %s breaks a PR-P1 rule but has a table", m)
		}
	}
	// relationMode "prisma": no foreign keys, emulated_relations listed,
	// the join table without foreign keys.
	const em = "emulated/schema.prisma"
	check("emulated Post", tableAttrs(t, nodes, em, "Post"), want{"emulated_relations": []map[string]any{{"field": "user", "model": "User", "table": "User", "columns": []string{"userId"}, "references": []string{"id"}}}, "state": "complete"})
	check("emulated Post.userId", col(em, "Post", "userId"), want{"foreign_keys": []map[string]any{}, "index": true})
	check("emulated _GroupToUser.A", col(em, "_GroupToUser", "A"), want{"foreign_keys": []map[string]any{}, "primary_key": true})
	// Mapped providers: SQL Server's default action, MySQL's index.
	check("Note.ticketId", col("sqlserver/schema.prisma", "Note", "ticketId"), want{"foreign_keys": []map[string]any{{"table": "Ticket", "schema": nil, "column": "id", "local_columns": []string{"ticketId"}, "columns": []string{"id"}, "on_delete": "NoAction", "on_update": "Cascade", "name": nil, "status": "resolved"}}})
	check("Purchase.itemId", col("mysql/schema.prisma", "Purchase", "itemId"), want{"index": nil})
	check("Purchase", tableAttrs(t, nodes, "mysql/schema.prisma", "Purchase"), want{"constraint_set": "partial"})
	// Prisma 5 and 6 (developer decision 2026-11-09).
	check("v5 Invoice", tableAttrs(t, nodes, "v5/db/schema.prisma", "Invoice"), want{"schema": "billing", "pins": map[string]any{"prisma": "5.22.0"}, "set": "models/prisma/v5/db/schema.prisma"})
	check("v5 Invoice.code", col("v5/db/schema.prisma", "Invoice", "code"), want{"default": nil, "server_default": nil})
	// Prisma 5's join table key is a unique index on (A, B) (the
	// oracle).
	check("v5 join", tableAttrs(t, nodes, "v5/db/schema.prisma", "_InvoiceToLine"), want{"primary_key": []string{}, "constraint_set": "complete", "indexes": []map[string]any{
		{"name": nil, "columns": []string{"A", "B"}, "unique": true, "method": nil},
		{"name": nil, "columns": []string{"B"}, "unique": false, "method": "BTree"},
	}})
	check("v5 join.A", col("v5/db/schema.prisma", "_InvoiceToLine", "A"), want{"primary_key": false, "unique": false, "index": false, "foreign_keys": fkIn("billing", "Invoice", []string{"A"}, []string{"id"}, "Cascade", "Cascade", nil)})
	check("v6 Shop.code", col("v6/models/shop.prisma", "Shop", "code"), want{"default": "client", "server_default": false})
	check("v6 join", tableAttrs(t, nodes, "v6/models/shop.prisma", "_ShopToTag"), want{"primary_key": []string{"A", "B"}, "schema": "shop", "set": "models/prisma/v6/models"})
	check("NonLiteralMap.name", col(pg, "NonLiteralMap", "name"), want{"state": "unknown"})
	if hasNode(nodes, "table:"+pg+"#Refused") || hasNode(nodes, "table:"+pg+"#Summary") || hasNode(nodes, "table:"+pg+"#Address") {
		t.Error("a refused table, a view or a composite type was emitted")
	}
	// PR-AA5 on the multi-file schema, PR-V5.
	check("Account", tableAttrs(t, nodes, "multi/prisma/schema/base.prisma", "Account"), want{"schema": "public", "set": "models/prisma/multi/prisma/schema"})
	check("AuditEntry", tableAttrs(t, nodes, "multi/prisma/schema/audit.prisma", "AuditEntry"), want{"schema": "audit", "assumptions": []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4"}})
	if hasNode(nodes, "table:multi/prisma/schema/audit.prisma#Stray") || hasNode(nodes, "table:multi/prisma/schema/audit.prisma#Unplaced") {
		t.Error("a model breaking the @@schema rules (PR-P1) has a table")
	}
	check("Scalars", tbl(pg, "Scalars"), want{"schema": "public", "assumptions": []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4", "PR-V5"}, "pins": map[string]any{"prisma": "7.10.0"}, "dialect": "postgresql", "verification": "oracle"})
	// Mapped providers (PR-V6).
	check("Item", tableAttrs(t, nodes, "mysql/schema.prisma", "Item"), want{"schema": nil, "verification": "mapped", "assumptions": []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4", "PR-V6"}})
	check("Item.name", col("mysql/schema.prisma", "Item", "name"), want{"sql_type": "VARCHAR(191)", "length": nil})
	check("Item.size", col("mysql/schema.prisma", "Item", "size"), want{"sql_type": "ENUM('s', 'l')"})
	check("Item.created", col("mysql/schema.prisma", "Item", "created"), want{"server_default_text": "CURRENT_TIMESTAMP(3)"})
	// AUTO_INCREMENT and IDENTITY are column properties, not DEFAULTs
	// (decision 2026-11-09): no default claim.
	check("Item.id", col("mysql/schema.prisma", "Item", "id"), want{"default": nil, "server_default": nil, "server_default_text": nil})
	check("Item.data", col("mysql/schema.prisma", "Item", "data"), want{"server_default": nil})
	check("Item.short", col("mysql/schema.prisma", "Item", "short"), want{"type": nil})
	check("Ticket.id", col("sqlserver/schema.prisma", "Ticket", "id"), want{"sql_type": "INT", "default": nil, "server_default": nil, "server_default_text": nil})
	check("Ticket.body", col("sqlserver/schema.prisma", "Ticket", "body"), want{"sql_type": "NVARCHAR(1000)"})
	check("Member.id", col("cockroach/schema.prisma", "Member", "id"), want{"sql_type": "INT8", "server_default_text": "unique_rowid()"})
	check("Member.tags", col("cockroach/schema.prisma", "Member", "tags"), want{"sql_type": "STRING[]"})
	check("Member", tableAttrs(t, nodes, "cockroach/schema.prisma", "Member"), want{"schema": "public"})
	for _, n := range nodes {
		if !strings.HasPrefix(n.Identity, "column:mysql/") || n.Attributes["state"] != "read" {
			continue
		}
		for _, c := range checkRecords(t, []contract.Node{n})[n.Identity] {
			// A foreign-key list with constraints carries the connector's
			// default actions (mapped); an empty list is provider-independent.
			fks, _ := n.Attributes["foreign_keys"].([]map[string]any)
			mapped := c.Fact == "type" || c.Fact == "default" || c.Fact == "server_default" || c.Fact == "foreign_keys" && len(fks) > 0
			if mapped != (c.Verification == "mapped") {
				t.Errorf("%s: claim %s verification %s", n.Identity, c.Fact, c.Verification)
			}
		}
	}
	// sqlite (the SQLite oracle): the declared types, enums as
	// TEXT, the database's default text, no default claim for an Int id
	// (INTEGER PRIMARY KEY AUTOINCREMENT) or autoincrement() or a Json
	// literal, foreign keys with the default actions, the join table's
	// unique index.
	const lite = "sqlite/schema.prisma"
	check("sqlite Note", tableAttrs(t, nodes, lite, "Note"), want{"verification": "oracle", "dialect": "sqlite", "schema": nil, "assumptions": []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4"}})
	check("Note.text", col(lite, "Note", "text"), want{"type": "String", "sql_type": "TEXT", "default": "absent", "server_default": false, "nullable": true})
	check("Note.id", col(lite, "Note", "id"), want{"sql_type": "INTEGER", "default": nil, "server_default": nil, "primary_key": true})
	check("Note.data", col(lite, "Note", "data"), want{"sql_type": "JSONB"})
	check("Note.mood", col(lite, "Note", "mood"), want{"type": "TEXT", "sql_type": "TEXT"})
	check("Note.shape", col(lite, "Note", "shape"), want{"sql_type": "CIRCLE"})
	check("sqlite Defaults.id", col(lite, "Defaults", "id"), want{"sql_type": "BIGINT", "default": nil, "server_default": nil})
	check("sqlite Defaults.label", col(lite, "Defaults", "label"), want{"server_default": true, "server_default_text": "'it''s'"})
	check("sqlite Defaults.n", col(lite, "Defaults", "n"), want{"server_default_text": "-1"})
	check("sqlite Defaults.mood", col(lite, "Defaults", "mood"), want{"server_default_text": "'sad'"})
	check("sqlite Defaults.created", col(lite, "Defaults", "created"), want{"server_default_text": "CURRENT_TIMESTAMP"})
	check("sqlite Defaults.ratio", col(lite, "Defaults", "ratio"), want{"server_default": true, "server_default_text": nil})
	check("sqlite Defaults.doc", col(lite, "Defaults", "doc"), want{"default": nil, "server_default": nil})
	check("sqlite Defaults.u", col(lite, "Defaults", "u"), want{"default": "client", "server_default": false})
	check("sqlite Kid.pid", col(lite, "Kid", "pid"), want{"foreign_keys": fkIn(nil, "Parent", []string{"pid"}, []string{"id"}, "Restrict", "Cascade", nil)})
	check("sqlite Kid.oid", col(lite, "Kid", "oid"), want{"foreign_keys": fkIn(nil, "Parent", []string{"oid"}, []string{"id"}, "SetNull", "Cascade", nil)})
	check("sqlite join", tableAttrs(t, nodes, lite, "_KidToTag"), want{"primary_key": []string{}, "constraint_set": "complete", "verification": "oracle"})
	// Postgres keeps a negative or non-int4 integer default as a cast
	// string: presence only.
	check("Defaults.neg", col(pg, "Defaults", "neg"), want{"server_default": true, "server_default_text": nil})
	check("Defaults.huge", col(pg, "Defaults", "huge"), want{"server_default": true, "server_default_text": nil})
	// A non-literal provider or relation mode leaves provider-dependent
	// and foreign-key facts unknown.
	check("Thing", tableAttrs(t, nodes, "dynamic/schema.prisma", "Thing"), want{"constraint_set": "partial", "dialect": nil})
	check("Thing.label", col("dynamic/schema.prisma", "Thing", "label"), want{"type": nil, "foreign_keys": nil, "nullable": true})
	if hasNode(nodes, "table:mongo/") || hasNode(nodes, "table:nodep/") || hasNode(nodes, "table:nonliteral/") || hasNode(nodes, "table:dupe/") || hasNode(nodes, "table:noview/") || hasNode(nodes, "table:mysql/schema.prisma#Tagged") {
		t.Error("mongodb, a package without the dependency, an unproven schema or a PR-P1 failure emitted a table")
	}
	codes := map[string]int{}
	for _, d := range diags {
		codes[d.Code]++
	}
	if codes[codeNotProven] != 1 || codes[codeUnread] < 4 || codes[codeNotClaimed] != 2 || codes[codeImportFailure] != 9 || codes[codeMigrations] != 1 {
		t.Errorf("diagnostics %v", codes)
	}
}

func tableAttrs(t *testing.T, nodes []contract.Node, file, model string) map[string]any {
	t.Helper()
	return nodeByID(t, nodes, "table:"+file+"#"+model)
}
