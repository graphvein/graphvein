package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// dialectGroup is a fixture-free group whose dialect is chosen at run
// time (like GORM's gotify), listed unsorted to check that the core sorts.
func dialectGroup() Group {
	return Group{Tool: "fake", Root: "app", Pins: map[string]any{"fake": "1.0"}, Dialects: []string{"sqlite", "postgres", "mysql"}, Verification: VerificationMapped}
}

const dialectFile = "app/models.fake"

var dialectSource = map[string][]byte{dialectFile: []byte("model Order {\n  id\n  amount\n  created\n  note\n}\n")}

// dialectTables builds one table with a per-dialect column (amount), a
// per-dialect type family beside a known type and a per-dialect timezone
// (created), a per-dialect autoincrement with a tool type (id) and a type
// every dialect agrees on (note) through the one-call helpers. A group
// with run-time dialects states every dialect-dependent fact per dialect
// (T018g0b Revision 1, A3).
func dialectTables() []*Table {
	oracle := func(row string) ClaimSource { return ClaimSource{Row: row, Verification: VerificationOracle} }
	id := &Column{Name: "id", Attribute: "ID", File: dialectFile, Line: 2,
		Attributes: map[string]any{"nullable": false, "primary_key": true},
		Claims:     map[string]ClaimSource{"name": {Row: "FK-B1"}, "nullable": {Row: "FK-B1"}, "primary_key": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-B1"}}}
	id.SetToolType("uint")
	id.SetDialectFact("postgres", "type", "bigserial", oracle("FK-T1"))
	id.SetDialectFact("sqlite", "type", "integer", oracle("FK-T1"))
	id.SetDialectFact("mysql", "type", "bigint unsigned", ClaimSource{Row: "FK-T1"})
	id.SetDialectFact("postgres", "autoincrement", true, oracle("FK-T2"))
	id.SetDialectFact("sqlite", "autoincrement", true, oracle("FK-T2"))
	id.SetDialectFact("mysql", "autoincrement", true, ClaimSource{Row: "FK-T2"})

	amount := &Column{Name: "amount", Attribute: "Amount", File: dialectFile, Line: 3,
		Attributes: map[string]any{"nullable": true},
		Claims:     map[string]ClaimSource{"name": {Row: "FK-B1"}, "nullable": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-B1"}}}
	amount.SetDialectTypeFamily("postgres", "numeric", []string{"precision", "scale"}, oracle("FK-T1"))
	amount.SetDialectFact("mysql", "type", "decimal", ClaimSource{Row: "FK-T1"})
	amount.SetDialectFact("mysql", "precision", 10, ClaimSource{Row: "FK-T1"})
	amount.SetDialectFact("mysql", "scale", 0, ClaimSource{Row: "FK-T1"})
	// sqlite's type stays unknown: no value, no claim.
	amount.SetDialectFact("postgres", "server_default", true, oracle("FK-T3"))
	amount.SetDialectFact("postgres", "server_default_text", "0", ClaimSource{})
	amount.SetDialectFact("mysql", "server_default", true, ClaimSource{Row: "FK-T3"})
	amount.SetDialectFact("mysql", "server_default_text", "'0'", ClaimSource{})

	created := &Column{Name: "created", Attribute: "Created", File: dialectFile, Line: 4,
		Attributes: map[string]any{},
		Claims:     map[string]ClaimSource{"name": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-B1"}}}
	created.SetDialectTypeFamily("postgres", "timestamp with time zone", []string{"precision"}, oracle("FK-T1"))
	created.SetDialectTypeFamily("mysql", "datetime", []string{"precision"}, ClaimSource{Row: "FK-T1"})
	created.SetDialectFact("sqlite", "type", "datetime", oracle("FK-T1"))
	created.SetDialectFact("postgres", "timezone", true, oracle("FK-T1"))
	created.SetDialectFact("mysql", "timezone", false, ClaimSource{Row: "FK-T1"})

	note := &Column{Name: "note", Attribute: "Note", File: dialectFile, Line: 5,
		Attributes: map[string]any{"nullable": true},
		Claims:     map[string]ClaimSource{"name": {Row: "FK-B1"}, "nullable": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-B1"}}}
	note.SetDialectFact("mysql", "type", "text", ClaimSource{Row: "FK-T1"})
	note.SetDialectFact("postgres", "type", "text", oracle("FK-T1"))
	note.SetDialectFact("sqlite", "type", "text", oracle("FK-T1"))
	return []*Table{{Identity: "table:" + dialectFile + "#Order", File: dialectFile, Name: "orders", Model: "Order", ModelLine: 1,
		Span: contract.Span{StartLine: 1, EndLine: 6}, SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true,
		Columns: []*Column{id, amount, created, note}}}
}

func linkDialects(t *testing.T, g Group, tables []*Table) (map[string]contract.Node, *Steps) {
	t.Helper()
	steps := &Steps{}
	nodes, _ := Link(tables, LinkInput{Group: g, Steps: steps, Sources: dialectSource})
	by := map[string]contract.Node{}
	for _, n := range nodes {
		by[n.Identity] = n
	}
	return by, steps
}

func claimKeys(n contract.Node) []string {
	var out []string
	for _, c := range n.Attributes["claims"].([]Claim) {
		key := c.Fact + "=" + c.Verification
		if c.Dialect != "" {
			key = c.Fact + "@" + c.Dialect + "=" + c.Verification
		}
		out = append(out, key)
	}
	return out
}

// TestDialectFacts checks ADR 0065's per-dialect facts on a synthetic
// group: the table lists its dialects (sorted) and no single dialect; a
// per-dialect fact is written as by_dialect with its flat value null, one
// claim per dialect whose value is known (each with its own
// verification), at the fact's place in the claim order; an unknown value
// on one dialect has no claim; dialect-independent facts stay single.
func TestDialectFacts(t *testing.T) {
	by, steps := linkDialects(t, dialectGroup(), dialectTables())
	modeltest.CheckClaims(t, nodesOf(by), ClaimFacts)
	if len(steps.Diagnostics) != 0 {
		t.Fatalf("diagnostics %v", steps.Diagnostics)
	}
	table := by["table:"+dialectFile+"#Order"].Attributes
	if !reflect.DeepEqual(table["dialects"], []string{"mysql", "postgres", "sqlite"}) || table["dialect"] != nil {
		t.Fatalf("table dialects %v, dialect %v", table["dialects"], table["dialect"])
	}
	if !reflect.DeepEqual(table["verification_counts"], map[string]int{"oracle": 11, "mapped": 21}) {
		t.Errorf("verification_counts %v", table["verification_counts"])
	}
	id := by["column:"+dialectFile+"#Order.id"]
	if got := strings.Join(claimKeys(id), ","); got != "name=mapped,type@mysql=mapped,type@postgres=oracle,type@sqlite=oracle,nullable=mapped,primary_key=mapped,autoincrement@mysql=mapped,autoincrement@postgres=oracle,autoincrement@sqlite=oracle,foreign_keys=mapped" {
		t.Errorf("id claims %s", got)
	}
	if id.Attributes["type"] != nil || id.Attributes["tool_type"] != "uint" || id.Attributes["autoincrement"] != nil {
		t.Errorf("id flat facts %v", id.Attributes)
	}
	amount := by["column:"+dialectFile+"#Order.amount"]
	data, _ := json.Marshal(amount.Attributes["by_dialect"])
	want := `{"mysql":{"precision":10,"scale":0,"server_default":true,"server_default_text":"'0'","type":"decimal","type_family":null},` +
		`"postgres":{"precision":null,"scale":null,"server_default":true,"server_default_text":"0","type":null,"type_family":{"name":"numeric","unknown":["precision","scale"]}},` +
		`"sqlite":{"precision":null,"scale":null,"server_default":null,"server_default_text":null,"type":null,"type_family":null}}`
	if string(data) != want {
		t.Errorf("amount by_dialect\n got %s\nwant %s", data, want)
	}
	if got := strings.Join(claimKeys(amount), ","); got != "name=mapped,type@mysql=mapped,type_family@postgres=oracle,precision@mysql=mapped,scale@mysql=mapped,nullable=mapped,server_default@mysql=mapped,server_default@postgres=oracle,foreign_keys=mapped" {
		t.Errorf("amount claims %s", got)
	}
	for _, f := range []string{"type", "type_family", "precision", "scale", "server_default", "server_default_text"} {
		if v, has := amount.Attributes[f]; !has || v != nil {
			t.Errorf("amount flat %s: %v (present %t), want null", f, v, has)
		}
	}
	created := by["column:"+dialectFile+"#Order.created"]
	if got := strings.Join(claimKeys(created), ","); got != "name=mapped,type@sqlite=oracle,type_family@mysql=mapped,type_family@postgres=oracle,timezone@mysql=mapped,timezone@postgres=oracle,foreign_keys=mapped" {
		t.Errorf("created claims %s", got)
	}
	note := by["column:"+dialectFile+"#Order.note"]
	for _, k := range []string{"tool_type"} {
		if _, has := note.Attributes[k]; has {
			t.Errorf("note has %s: a column gains only the attributes it states", k)
		}
	}
	if note.Attributes["type"] != nil || strings.Join(claimKeys(note), ",") != "name=mapped,type@mysql=mapped,type@postgres=oracle,type@sqlite=oracle,nullable=mapped,foreign_keys=mapped" {
		t.Errorf("note %v claims %v", note.Attributes, claimKeys(note))
	}
}

// TestDialectFactsListMirrored checks that modeltest's copy of the
// per-dialect fact list (it does not import the core) equals DialectFacts,
// so CheckDialectFacts checks exactly the facts the core writes.
func TestDialectFactsListMirrored(t *testing.T) {
	if !reflect.DeepEqual(modeltest.DialectFacts, DialectFacts) {
		t.Errorf("modeltest.DialectFacts %v, models.DialectFacts %v", modeltest.DialectFacts, DialectFacts)
	}
}

// singleDialect makes a group state one dialect (no run-time dialects),
// where flat dialect-dependent facts are allowed.
func singleDialect(g *Group) { g.Dialects = nil; g.Dialect = "postgres" }

// TestTypeFamily checks a flat type family (ADR 0065) in a single-dialect
// group: type_family is claimed, type and the unknown modifier are null
// without claims, and a known modifier beside it (timezone) keeps its
// claim.
func TestTypeFamily(t *testing.T) {
	g := dialectGroup()
	singleDialect(&g)
	created := &Column{Name: "created", Attribute: "Created", File: dialectFile, Line: 4,
		Attributes: map[string]any{"timezone": true},
		Claims:     map[string]ClaimSource{"name": {Row: "FK-B1"}, "timezone": {Row: "FK-T1"}, "foreign_keys": {Row: "FK-B1"}}}
	created.SetTypeFamily("timestamp with time zone", []string{"precision"}, ClaimSource{Row: "FK-T1"})
	table := &Table{Identity: "table:" + dialectFile + "#Order", File: dialectFile, Name: "orders", Model: "Order", Span: contract.Span{StartLine: 1, EndLine: 6},
		SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true, Columns: []*Column{created}}
	by, steps := linkDialects(t, g, []*Table{table})
	modeltest.CheckClaims(t, nodesOf(by), ClaimFacts)
	if len(steps.Diagnostics) != 0 {
		t.Fatalf("diagnostics %v", steps.Diagnostics)
	}
	n := by["column:"+dialectFile+"#Order.created"]
	a := n.Attributes
	if !reflect.DeepEqual(a["type_family"], TypeFamily("timestamp with time zone", "precision")) || a["type"] != nil || a["precision"] != nil || a["timezone"] != true {
		t.Fatalf("created %v", a)
	}
	if got := strings.Join(claimKeys(n), ","); got != "name=mapped,type_family=mapped,timezone=mapped,foreign_keys=mapped" {
		t.Errorf("created claims %s", got)
	}
}

// TestTypeFamilyModifiers checks the generic unknown modifier (ADR 0062
// Revision 2): a family of an extension type lists "modifiers" alone, is
// claimed, and writes no "modifiers" attribute.
func TestTypeFamilyModifiers(t *testing.T) {
	g := dialectGroup()
	singleDialect(&g)
	loc := &Column{Name: "loc", Attribute: "loc", File: dialectFile, Line: 4,
		Claims: map[string]ClaimSource{"name": {Row: "FK-B1"}, "foreign_keys": {Row: "FK-B1"}}}
	loc.SetTypeFamily("geometry", []string{ModifiersUnknown}, ClaimSource{Row: "FK-T1"})
	table := &Table{Identity: "table:" + dialectFile + "#Place", File: dialectFile, Name: "places", Model: "Place", Span: contract.Span{StartLine: 1, EndLine: 6},
		SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true, Columns: []*Column{loc}}
	by, steps := linkDialects(t, g, []*Table{table})
	modeltest.CheckClaims(t, nodesOf(by), ClaimFacts)
	if len(steps.Diagnostics) != 0 {
		t.Fatalf("diagnostics %v", steps.Diagnostics)
	}
	a := by["column:"+dialectFile+"#Place.loc"].Attributes
	if !reflect.DeepEqual(a["type_family"], TypeFamily("geometry", ModifiersUnknown)) || a["type"] != nil {
		t.Fatalf("loc %v", a)
	}
	if _, has := a[ModifiersUnknown]; has {
		t.Errorf("loc has a %s attribute: %v", ModifiersUnknown, a)
	}
	if got := strings.Join(claimKeys(by["column:"+dialectFile+"#Place.loc"]), ","); got != "name=mapped,type_family=mapped,foreign_keys=mapped" {
		t.Errorf("loc claims %s", got)
	}
}

// TestSingleDialectUnchanged checks that a group without run-time
// dialects and columns without the new fields emit no new attribute: the
// existing front ends' records stay byte-identical.
func TestSingleDialectUnchanged(t *testing.T) {
	steps := &Steps{}
	nodes, _ := Link(fixtureTables(), LinkInput{Group: fixtureGroup(), Steps: steps, Sources: map[string][]byte{}})
	for _, n := range nodes {
		for _, k := range []string{"dialects", "by_dialect", "type_family", "tool_type", "autoincrement"} {
			if _, has := n.Attributes[k]; has {
				t.Errorf("%s has %s", n.Identity, k)
			}
		}
		if n.Kind != "column" {
			continue
		}
		data, _ := json.Marshal(n.Attributes["claims"])
		if strings.Contains(string(data), "dialect") {
			t.Errorf("%s: claims %s name a dialect", n.Identity, data)
		}
	}
}

// TestDialectFactsRefused checks that a front end's misuse never shows a
// fact as more than it is: each case writes the facts concerned unknown
// (no claim) with a schema_model_internal_error diagnostic.
func TestDialectFactsRefused(t *testing.T) {
	cases := []struct {
		name   string
		group  func(*Group)
		column func(*Column)
		check  func(t *testing.T, table, col map[string]any, claims []string)
	}{
		{"type beside family", singleDialect, func(c *Column) {
			c.SetTypeFamily("numeric", []string{"precision"}, ClaimSource{Row: "FK-T1"})
			c.Attributes["type"] = "numeric"
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			if col["type"] != nil || col["type_family"] != nil || strings.Contains(strings.Join(claims, ","), "type") {
				t.Errorf("type %v family %v claims %v", col["type"], col["type_family"], claims)
			}
		}},
		{"unknown modifier stated", singleDialect, func(c *Column) {
			c.SetTypeFamily("numeric", []string{"precision", "scale"}, ClaimSource{Row: "FK-T1"})
			c.Attributes["scale"] = 2
			c.Claims["scale"] = ClaimSource{Row: "FK-T1"}
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			if col["scale"] != nil || col["type_family"] == nil || strings.Contains(strings.Join(claims, ","), "scale") {
				t.Errorf("scale %v family %v claims %v", col["scale"], col["type_family"], claims)
			}
		}},
		{"family without unknown modifiers", singleDialect, func(c *Column) {
			c.SetTypeFamily("numeric", nil, ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, _ []string) {
			if col["type_family"] != nil {
				t.Errorf("family %v", col["type_family"])
			}
		}},
		{"family with another modifier", singleDialect, func(c *Column) {
			c.SetTypeFamily("numeric", []string{"precision", "timezone"}, ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, _ []string) {
			if col["type_family"] != nil {
				t.Errorf("family %v", col["type_family"])
			}
		}},
		{"generic modifiers beside another modifier", singleDialect, func(c *Column) {
			c.SetTypeFamily("geometry", []string{ModifiersUnknown, "length"}, ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, _ []string) {
			if col["type_family"] != nil {
				t.Errorf("family %v", col["type_family"])
			}
		}},
		{"dialect outside the group", nil, func(c *Column) {
			c.SetDialectFact("mssql", "type", "int", ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			data, _ := json.Marshal(col["by_dialect"])
			if col["type"] != nil || strings.Contains(string(data), "mssql") || strings.Contains(strings.Join(claims, ","), "type") {
				t.Errorf("type %v by_dialect %s claims %v", col["type"], data, claims)
			}
		}},
		{"not a per-dialect fact", nil, func(c *Column) {
			c.SetDialectFact("mysql", "nullable", false, ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, _ []string) {
			if _, has := col["by_dialect"]; has || col["nullable"] != true {
				t.Errorf("by_dialect %v nullable %v", col["by_dialect"], col["nullable"])
			}
		}},
		{"flat and per dialect", nil, func(c *Column) {
			c.SetDialectFact("mysql", "type", "longtext", ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			if col["type"] != nil || strings.Join(claims, ",") != "name=mapped,type@mysql=mapped,nullable=mapped,foreign_keys=mapped" {
				t.Errorf("type %v claims %v", col["type"], claims)
			}
		}},
		{"flat dialect fact in a dialects group", nil, nil, func(t *testing.T, _, col map[string]any, claims []string) {
			// The base column states type text once (Revision 1, A3).
			if v, has := col["type"]; !has || v != nil || strings.Contains(strings.Join(claims, ","), "type") {
				t.Errorf("type %v (present %t) claims %v", v, has, claims)
			}
			if _, has := col["by_dialect"]; has {
				t.Errorf("by_dialect %v", col["by_dialect"])
			}
		}},
		{"flat modifier beside a per-dialect family", func(g *Group) { g.Dialects = []string{"mysql"} }, func(c *Column) {
			// A4: the family's unknown precision covers the flat one, also
			// when the family is set without the one-call helper.
			delete(c.Attributes, "type")
			c.SetDialectFact("mysql", "type_family", TypeFamily("numeric", "precision"), ClaimSource{Row: "FK-T1"})
			c.Attributes["precision"] = 10
			c.Claims["precision"] = ClaimSource{Row: "FK-T1"}
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			data, _ := json.Marshal(col["by_dialect"])
			if v, has := col["precision"]; !has || v != nil || strings.Join(claims, ",") != "name=mapped,type_family@mysql=mapped,nullable=mapped,foreign_keys=mapped" {
				t.Errorf("precision %v (present %t) claims %v", v, has, claims)
			}
			if string(data) != `{"mysql":{"precision":null,"type":null,"type_family":{"name":"numeric","unknown":["precision"]}}}` {
				t.Errorf("by_dialect %s", data)
			}
		}},
		{"flat modifier beside a family, no group dialects", singleDialect, func(c *Column) {
			delete(c.Attributes, "type")
			c.SetDialectFact("postgres", "type_family", TypeFamily("numeric", "precision"), ClaimSource{Row: "FK-T1"})
			c.Attributes["precision"] = 10
			c.Claims["precision"] = ClaimSource{Row: "FK-T1"}
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			if col["precision"] != nil || strings.Contains(strings.Join(claims, ","), "precision") {
				t.Errorf("precision %v claims %v", col["precision"], claims)
			}
		}},
		{"per dialect in a single-dialect group", singleDialect, func(c *Column) {
			c.SetDialectFact("postgres", "type", "text", ClaimSource{Row: "FK-T1"})
		}, func(t *testing.T, table, col map[string]any, claims []string) {
			if _, has := col["by_dialect"]; has || col["type"] != nil || table["dialect"] != "postgres" || strings.Contains(strings.Join(claims, ","), "type") {
				t.Errorf("by_dialect %v type %v dialect %v claims %v", col["by_dialect"], col["type"], table["dialect"], claims)
			}
		}},
		{"dialect and dialects", func(g *Group) { g.Dialect = "mysql" }, nil, func(t *testing.T, table, _ map[string]any, _ []string) {
			if table["dialect"] != nil || table["dialects"] == nil {
				t.Errorf("dialect %v dialects %v", table["dialect"], table["dialects"])
			}
		}},
		{"per-dialect claim without verification", func(g *Group) { g.Verification = "" }, func(c *Column) {
			c.SetDialectFact("mysql", "autoincrement", true, ClaimSource{Row: "FK-T2"})
		}, func(t *testing.T, _, col map[string]any, claims []string) {
			if strings.Contains(strings.Join(claims, ","), "autoincrement") {
				t.Errorf("claims %v", claims)
			}
		}},
		{"tool type not a string", nil, func(c *Column) { c.Attributes["tool_type"] = 3 }, func(t *testing.T, _, col map[string]any, _ []string) {
			if _, has := col["tool_type"]; has {
				t.Errorf("tool_type %v", col["tool_type"])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := dialectGroup()
			if tc.group != nil {
				tc.group(&g)
			}
			col := &Column{Name: "x", Attribute: "X", File: dialectFile, Line: 5,
				Attributes: map[string]any{"type": "text", "nullable": true},
				Claims:     map[string]ClaimSource{"name": {Row: "FK-B1", Verification: VerificationMapped}, "type": {Row: "FK-T1", Verification: VerificationMapped}, "nullable": {Row: "FK-B1", Verification: VerificationMapped}, "foreign_keys": {Row: "FK-B1", Verification: VerificationMapped}}}
			if tc.column != nil {
				tc.column(col)
			}
			table := &Table{Identity: "table:" + dialectFile + "#Order", File: dialectFile, Name: "orders", Model: "Order", Span: contract.Span{StartLine: 1, EndLine: 6},
				SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true, Verification: VerificationMapped, Columns: []*Column{col}}
			by, steps := linkDialects(t, g, []*Table{table})
			n := by["column:"+dialectFile+"#Order.x"]
			tc.check(t, by[table.Identity].Attributes, n.Attributes, claimKeys(n))
			// A refused claim leaves a known fact without one, which is
			// exactly what a front end's CheckClaims must catch.
			if tc.name != "per-dialect claim without verification" {
				modeltest.CheckClaims(t, nodesOf(by), ClaimFacts)
			}
			if len(steps.Diagnostics) == 0 || steps.Diagnostics[0].Code != "schema_model_internal_error" {
				t.Errorf("diagnostics %v", steps.Diagnostics)
			}
		})
	}
}

// TestCheckDialectShapes checks that the modeltest helpers catch a record
// that breaks ADR 0065's shapes (a record not written by the core, such as
// one read back from an older or a hand-made snapshot).
func TestCheckDialectShapes(t *testing.T) {
	table := contract.Node{Identity: "table:f#M", Kind: "table", File: "f", QualifiedName: "M", Attributes: map[string]any{"dialects": []any{"mysql", "sqlite"}, "verification_counts": map[string]any{}}}
	col := func(attrs map[string]any) contract.Node {
		attrs["state"], attrs["claims"] = "read", []any{}
		return contract.Node{Identity: "column:f#M.c", Kind: "column", Name: "c", File: "f", QualifiedName: "M.c", Attributes: attrs}
	}
	cases := []struct {
		name  string
		attrs map[string]any
		want  string
	}{
		{"missing dialect", map[string]any{"by_dialect": map[string]any{"mysql": map[string]any{"type": nil}}}, "by_dialect dialects"},
		{"flat beside per dialect", map[string]any{"type": "int", "by_dialect": map[string]any{"mysql": map[string]any{"type": nil}, "sqlite": map[string]any{"type": nil}}}, "beside its per-dialect values"},
		{"type beside family", map[string]any{"type": "numeric", "type_family": map[string]any{"name": "numeric", "unknown": []any{"precision"}}}, "beside type_family"},
		{"modifier known", map[string]any{"precision": 10, "type_family": map[string]any{"name": "numeric", "unknown": []any{"precision"}}}, "lists precision unknown"},
		{"flat fact in a dialects table", map[string]any{"type": "int"}, "once in a table with dialects"},
		{"known per-dialect fact without claim", map[string]any{"by_dialect": map[string]any{"mysql": map[string]any{"type": "int"}, "sqlite": map[string]any{"type": nil}}}, "on mysql has no claim"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{TB: t}
			modeltest.CheckClaims(r, []contract.Node{table, col(tc.attrs)}, ClaimFacts)
			if !strings.Contains(strings.Join(r.errors, "\n"), tc.want) {
				t.Errorf("errors %q, want one with %q", r.errors, tc.want)
			}
		})
	}
}

// recorder collects a check's errors instead of failing the test.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func nodesOf(by map[string]contract.Node) []contract.Node {
	var out []contract.Node
	for _, n := range by {
		out = append(out, n)
	}
	return out
}
