package models

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

func str(s string) *string { return &s }

// fixtureGroup is a group with a stated dialect and mapped as the default,
// like a Prisma schema for a provider without an oracle.
func fixtureGroup() Group {
	return Group{Tool: "fake", Root: "db/schema.fake", Pins: map[string]any{"fake": "1.0"}, Assumptions: []string{"FK-V1"}, Dialect: "mysql", Verification: VerificationMapped}
}

func fixtureTables() []*Table {
	id := &Column{Name: "id", Attribute: "id", File: "db/schema.fake", Line: 2,
		Attributes: map[string]any{"type": "Int", "nullable": false, "primary_key": true, "default": nil},
		Claims: map[string]ClaimSource{
			"name": {Row: "FK-B1"}, "type": {Row: "FK-F1"}, "nullable": {Row: "FK-F1", Verification: VerificationOracle},
			"primary_key": {Row: "FK-A1", File: "db/other.fake", Line: 9}, "foreign_keys": {Row: "FK-R1"},
			// A claim on a fact the record holds as null is never emitted.
			"default": {Row: "FK-A3"},
		}}
	ref := &Column{Name: "user_id", Attribute: "userId", File: "db/schema.fake", Line: 7,
		Attributes:  map[string]any{"primary_key": false},
		ForeignKeys: []*ForeignKey{{Schema: str("auth"), Table: "users", Column: "id", File: "db/schema.fake", Line: 7}},
		Claims:      map[string]ClaimSource{"name": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-R1"}, "primary_key": {Row: "FK-A1"}}}
	lost := &Column{Name: "x", Attribute: "x", File: "db/schema.fake", Line: 8, Unknown: true,
		Attributes: map[string]any{"type": nil}, Claims: map[string]ClaimSource{"name": {Row: "FK-B1"}}}
	users := &Table{Identity: "table:db/schema.fake#User", File: "db/schema.fake", Name: "users", Model: "User", ModelLine: 1,
		Span: contract.Span{StartLine: 1, EndLine: 3}, Schema: str("auth"), SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true,
		Columns: []*Column{id}}
	posts := &Table{Identity: "table:db/schema.fake#Post", File: "db/schema.fake", Name: "posts", Model: "Post", ModelLine: 5,
		Span: contract.Span{StartLine: 5, EndLine: 9}, SchemaKnown: true, ColumnsComplete: false, ConstraintsComplete: true,
		Columns: []*Column{ref, lost}, Verification: VerificationOracle}
	return []*Table{users, posts}
}

// TestLink checks the core's record shapes: the group key, the dialect
// only when stated, per-fact claims with their file, span and
// verification, no claim on a null fact or an unknown column, and
// schema-aware foreign-key resolution within the group.
func TestLink(t *testing.T) {
	steps := &Steps{}
	var tags []string
	src := map[string][]byte{"db/schema.fake": []byte("model User {\n  id Int @id\n}\n\nmodel Post {\n  x\n  userId Int\n  x\n}\n")}
	nodes, edges := Link(fixtureTables(), LinkInput{Group: fixtureGroup(), Rows: Rows{Emit: "FK-L3", ForeignKeys: "FK-L4", Overlap: "FK-L5"}, Tag: func(r string) { tags = append(tags, r) }, Steps: steps, Sources: src})
	modeltest.CheckClaims(t, nodes, ClaimFacts)
	by := map[string]contract.Node{}
	for _, n := range nodes {
		by[n.Identity] = n
	}
	u := by["table:db/schema.fake#User"]
	if u.Attributes["set"] != "models/fake/db/schema.fake" || u.Attributes["dialect"] != "mysql" || u.Attributes["schema"] != "auth" || u.Attributes["verification"] != VerificationMapped || u.Attributes["tool"] != "fake" {
		t.Fatalf("users: %v", u.Attributes)
	}
	if p := by["table:db/schema.fake#Post"]; p.Attributes["verification"] != VerificationOracle || p.Attributes["primary_key"] != nil || p.Attributes["state"] != "partial" {
		t.Fatalf("posts: %v", p.Attributes)
	}
	claims := by["column:db/schema.fake#User.id"].Attributes["claims"].([]Claim)
	want := []Claim{
		{Fact: "name", Row: "FK-B1", File: "db/schema.fake", Span: contract.Span{StartLine: 2, EndLine: 2}, Verification: VerificationMapped},
		{Fact: "type", Row: "FK-F1", File: "db/schema.fake", Span: contract.Span{StartLine: 2, EndLine: 2}, Verification: VerificationMapped},
		{Fact: "nullable", Row: "FK-F1", File: "db/schema.fake", Span: contract.Span{StartLine: 2, EndLine: 2}, Verification: VerificationOracle},
		{Fact: "primary_key", Row: "FK-A1", File: "db/other.fake", Span: contract.Span{StartLine: 9, EndLine: 9}, Verification: VerificationMapped},
		{Fact: "foreign_keys", Row: "FK-R1", File: "db/schema.fake", Span: contract.Span{StartLine: 2, EndLine: 2}, Verification: VerificationMapped},
	}
	if len(claims) != len(want) {
		t.Fatalf("claims %+v", claims)
	}
	for i := range want {
		if claims[i] != want[i] {
			t.Errorf("claim %d: %+v, want %+v", i, claims[i], want[i])
		}
	}
	if c := by["column:db/schema.fake#Post.x"].Attributes["claims"].([]Claim); len(c) != 0 {
		t.Errorf("unknown column has claims %v", c)
	}
	ref := by["column:db/schema.fake#Post.user_id"].Attributes
	fks := ref["foreign_keys"].([]map[string]any)
	if fks[0]["status"] != "resolved" || fks[0]["schema"] != "auth" {
		t.Fatalf("foreign key %v", fks)
	}
	found := false
	for _, e := range edges {
		if e.Kind == "foreign_key" && e.Source == "column:db/schema.fake#Post.user_id" && e.Target == "column:db/schema.fake#User.id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no foreign_key edge: %v", edges)
	}
	if strings.Join(tags, ",") != "FK-L4,FK-L3,FK-L3" {
		t.Errorf("tags %v", tags)
	}
	if len(steps.Diagnostics) != 0 {
		t.Errorf("diagnostics %v", steps.Diagnostics)
	}
}

// TestVerificationRequired checks T018g0 Revision 2: a verification that is
// empty or not a known value never looks verified. A claim without one is
// refused (no claim, a schema_model_internal_error diagnostic), so a front
// end's modeltest.CheckClaims fails on it; a table without one is written
// mapped, with the diagnostic.
func TestVerificationRequired(t *testing.T) {
	tables := fixtureTables()
	tables[0].Columns[0].Claims["type"] = ClaimSource{Row: "FK-F1", Verification: "verified"}
	tables[1].Verification = ""
	steps := &Steps{}
	g := fixtureGroup()
	g.Verification = ""
	nodes, _ := Link(tables, LinkInput{Group: g, Steps: steps})
	by := map[string]contract.Node{}
	for _, n := range nodes {
		by[n.Identity] = n
	}
	var facts []string
	for _, c := range by["column:db/schema.fake#User.id"].Attributes["claims"].([]Claim) {
		facts = append(facts, c.Fact+"="+c.Verification)
	}
	// Only nullable states its own known verification.
	if strings.Join(facts, ",") != "nullable=oracle" {
		t.Errorf("claims %v", facts)
	}
	for _, id := range []string{"table:db/schema.fake#User", "table:db/schema.fake#Post"} {
		if v := by[id].Attributes["verification"]; v != VerificationMapped {
			t.Errorf("%s: verification %v, want mapped", id, v)
		}
	}
	refused := 0
	for _, d := range steps.Diagnostics {
		if d.Code != "schema_model_internal_error" {
			t.Errorf("diagnostic %+v", d)
		}
		if strings.Contains(d.Message, "refused") {
			refused++
		}
	}
	// User.id name, type, primary_key, foreign_keys; Post.user_id name,
	// foreign_keys, primary_key: seven refused claims, and two tables
	// written mapped.
	if refused != 7 || len(steps.Diagnostics) != 9 {
		t.Errorf("diagnostics %v", steps.Diagnostics)
	}
}

// TestForeignKeySchema checks that a foreign key resolves only to a table of
// the same schema: no written schema means the default schema, and a table
// whose schema is not proven is no candidate.
func TestForeignKeySchema(t *testing.T) {
	tables := fixtureTables()
	tables[0].Schema = nil
	steps := &Steps{}
	nodes, _ := Link(tables, LinkInput{Group: fixtureGroup(), Steps: steps})
	for _, n := range nodes {
		if n.Identity == "column:db/schema.fake#Post.user_id" {
			if fk := n.Attributes["foreign_keys"].([]map[string]any)[0]; fk["status"] != "unresolved" {
				t.Fatalf("foreign key %v", fk)
			}
		}
	}
	if len(steps.Diagnostics) != 1 || steps.Diagnostics[0].Code != "schema_unresolved_reference" {
		t.Fatalf("diagnostics %v", steps.Diagnostics)
	}
}

// TestStepsGuard checks the core's step isolation: a failing table step
// becomes schema_model_internal_error and the other tables are emitted.
func TestStepsGuard(t *testing.T) {
	steps := &Steps{Hook: func(step, subject string) {
		if step == "emit" && strings.HasSuffix(subject, "#User") {
			panic("boom")
		}
	}}
	nodes, _ := Link(fixtureTables(), LinkInput{Group: fixtureGroup(), Steps: steps})
	for _, n := range nodes {
		if n.Identity == "table:db/schema.fake#User" {
			t.Fatal("the failed table was emitted")
		}
	}
	if len(nodes) == 0 || len(steps.Diagnostics) != 1 || steps.Diagnostics[0].Code != "schema_model_internal_error" {
		t.Fatalf("nodes %d, diagnostics %v", len(nodes), steps.Diagnostics)
	}
}

// TestCatalogues checks that registered catalogues are listed by tool.
func TestCatalogues(t *testing.T) {
	Register(Catalogue{Tool: "zz-test", Rows: []ContractEntry{{ID: "ZZ-1", Branch: "none", Summary: "s"}}})
	defer func() {
		cataloguesMu.Lock()
		delete(catalogues, "zz-test")
		cataloguesMu.Unlock()
	}()
	list := Catalogues()
	if len(list) == 0 || list[len(list)-1].Tool != "zz-test" || list[len(list)-1].Rows[0].ID != "ZZ-1" {
		t.Fatalf("catalogues %+v", list)
	}
}

// TestForeignKeyConstraint checks the kit extension of ADR 0057 (T018g0
// review): a foreign key that states its whole constraint (Columns) is
// written with local_columns, columns, on_update and name, each local
// column pairs with the referenced column at its position, and the key
// resolves only when every referenced column is known. A key without
// Columns keeps the single-column shape (SQLModel's records unchanged).
func TestForeignKeyConstraint(t *testing.T) {
	file := "db/schema.fake"
	col := func(name string, line int) *Column {
		return &Column{Name: name, Attribute: name, File: file, Line: line, Attributes: map[string]any{"primary_key": true}}
	}
	parent := &Table{Identity: "table:" + file + "#Parent", File: file, Name: "parent", Model: "Parent", Span: contract.Span{StartLine: 1, EndLine: 4},
		SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true, Columns: []*Column{col("a", 2), col("b", 3)}, PrimaryKey: []string{"b", "a"}}
	mk := func(ref string, refs []string) *ForeignKey {
		return &ForeignKey{Table: "parent", Column: ref, Columns: []string{"pa", "pb"}, RefColumns: refs, OnDelete: "Restrict", OnUpdate: "Cascade", Name: str("child_fk"), File: file, Line: 9}
	}
	pa := &Column{Name: "pa", Attribute: "pa", File: file, Line: 6, Attributes: map[string]any{"primary_key": false}}
	pb := &Column{Name: "pb", Attribute: "pb", File: file, Line: 7, Attributes: map[string]any{"primary_key": false}}
	pa.ForeignKeys = []*ForeignKey{mk("a", []string{"a", "b"})}
	pb.ForeignKeys = []*ForeignKey{mk("b", []string{"a", "b"})}
	bad := &Column{Name: "pc", Attribute: "pc", File: file, Line: 8, Attributes: map[string]any{"primary_key": false},
		ForeignKeys: []*ForeignKey{{Table: "parent", Column: "a", Columns: []string{"pc"}, RefColumns: []string{"a", "zz"}, File: file, Line: 8}}}
	child := &Table{Identity: "table:" + file + "#Child", File: file, Name: "child", Model: "Child", Span: contract.Span{StartLine: 5, EndLine: 10},
		SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true, Columns: []*Column{pa, pb, bad}}
	steps := &Steps{}
	src := map[string][]byte{file: []byte(strings.Repeat("x\n", 10))}
	nodes, edges := Link([]*Table{parent, child}, LinkInput{Group: fixtureGroup(), Steps: steps, Sources: src})
	by := map[string]contract.Node{}
	for _, n := range nodes {
		by[n.Identity] = n
	}
	if pk := by[parent.Identity].Attributes["primary_key"]; strings.Join(pk.([]string), ",") != "b,a" {
		t.Errorf("primary key order %v", pk)
	}
	fk := by["column:"+file+"#Child.pb"].Attributes["foreign_keys"].([]map[string]any)[0]
	if fk["status"] != "resolved" || fk["column"] != "b" || fk["on_update"] != "Cascade" || fk["name"] != "child_fk" ||
		strings.Join(fk["local_columns"].([]string), ",") != "pa,pb" || strings.Join(fk["columns"].([]string), ",") != "a,b" {
		t.Errorf("composite foreign key %v", fk)
	}
	if fk := by["column:"+file+"#Child.pc"].Attributes["foreign_keys"].([]map[string]any)[0]; fk["status"] != "unresolved" || fk["name"] != nil {
		t.Errorf("foreign key to an unknown column %v", fk)
	}
	targets := map[string]string{}
	for _, e := range edges {
		if e.Kind == "foreign_key" {
			targets[e.Source] = e.Target
		}
	}
	if targets["column:"+file+"#Child.pa"] != "column:"+file+"#Parent.a" || targets["column:"+file+"#Child.pb"] != "column:"+file+"#Parent.b" || len(targets) != 2 {
		t.Errorf("foreign_key edges %v", targets)
	}
	// The single-column shape has no constraint keys.
	nodes, _ = Link(fixtureTables(), LinkInput{Group: fixtureGroup(), Steps: &Steps{}})
	for _, n := range nodes {
		if n.Identity == "column:db/schema.fake#Post.user_id" {
			fk := n.Attributes["foreign_keys"].([]map[string]any)[0]
			if len(fk) != 5 {
				t.Errorf("single-column foreign key keys %v", fk)
			}
		}
	}
}

// TestPrimaryKeyOrder checks that a stated primary-key order is written
// only when it names exactly the key columns; otherwise the column order
// stays, with a schema_model_internal_error diagnostic.
func TestPrimaryKeyOrder(t *testing.T) {
	for _, tc := range []struct {
		order []string
		want  string
		diags int
	}{
		{nil, "id", 0},
		{[]string{"id"}, "id", 0},
		{[]string{"other"}, "id", 1},
		{[]string{"id", "id"}, "id", 1},
	} {
		tables := fixtureTables()
		tables[0].PrimaryKey = tc.order
		steps := &Steps{}
		nodes, _ := Link(tables, LinkInput{Group: fixtureGroup(), Steps: steps})
		for _, n := range nodes {
			if n.Identity == "table:db/schema.fake#User" {
				if pk := strings.Join(n.Attributes["primary_key"].([]string), ","); pk != tc.want {
					t.Errorf("order %v: primary key %s", tc.order, pk)
				}
			}
		}
		if len(steps.Diagnostics) != tc.diags {
			t.Errorf("order %v: diagnostics %v", tc.order, steps.Diagnostics)
		}
	}
}

// TestColumnIdentityName checks the identity name (ADR 0057): it replaces
// the column name in the identity and qualified name only, while the
// record's name, the primary key and the verification counts keep the
// column name.
func TestColumnIdentityName(t *testing.T) {
	tables := fixtureTables()
	c := tables[0].Columns[0]
	old := c.Name
	c.IdentityName = old
	c.Name = "mapped_" + old
	tables[0].ByName = nil
	nodes, _ := Link(tables, LinkInput{Group: fixtureGroup(), Steps: &Steps{}})
	found := false
	for _, n := range nodes {
		if n.Identity == "column:db/schema.fake#User."+old {
			found = true
			if n.Name != "mapped_"+old || n.QualifiedName != "User."+old || n.Attributes["name"] != "mapped_"+old {
				t.Errorf("column node %s: name %q, qualified %q, attribute %v", n.Identity, n.Name, n.QualifiedName, n.Attributes["name"])
			}
		}
	}
	if !found {
		t.Fatalf("no column with identity name %s", old)
	}
	modeltest.CheckClaims(t, nodes, ClaimFacts)
}
