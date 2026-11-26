package sqlmodel

import (
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/python"
)

const header = "from typing import Optional\nfrom sqlmodel import Field, SQLModel\n"

func nodeByID(nodes []contract.Node, id string) (contract.Node, bool) {
	for _, n := range nodes {
		if n.Identity == id {
			return n, true
		}
	}
	return contract.Node{}, false
}

func hasDiag(diags []contract.Diagnostic, code, text string) bool {
	for _, d := range diags {
		if d.Code == code && strings.Contains(d.Message, text) {
			return true
		}
	}
	return false
}

// L4: a target name two claimed tables share is ambiguous (no edge); a
// missing table is unresolved.
func TestForeignKeyResolution(t *testing.T) {
	nodes, edges, diags := linkSources(t, map[string]string{
		"a.py": header + "class Owner(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n",
		"b.py": header + "class Owner(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n",
		"c.py": header + "class Pet(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n    owner_id: int = Field(foreign_key=\"owner.id\")\n    vet_id: int = Field(foreign_key=\"vet.id\")\n",
	})
	for _, e := range edges {
		if e.Kind == "foreign_key" {
			t.Fatalf("unexpected foreign_key edge %v", e)
		}
	}
	pet, _ := nodeByID(nodes, "column:c.py#Pet.owner_id")
	if fks := pet.Attributes["foreign_keys"].([]map[string]any); fks[0]["status"] != "ambiguous" {
		t.Fatalf("owner_id: %v", fks)
	}
	if !hasDiag(diags, "schema_unresolved_reference", "vet.id unresolved") {
		t.Fatalf("no unresolved diagnostic: %v", diags)
	}
}

// L8: an unresolved production site takes completeness from every
// production table; a resolved one only from its table.
func TestSiteResolution(t *testing.T) {
	src := header + "class A(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\nclass B(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n"
	nodes, _, _ := linkSources(t, map[string]string{"m.py": src + "def f(t):\n    t.append_constraint(1)\n"})
	for _, id := range []string{"table:m.py#A", "table:m.py#B"} {
		if n, _ := nodeByID(nodes, id); n.Attributes["constraint_set"] != "partial" || n.Attributes["column_set"] != "complete" {
			t.Fatalf("%s: %v", id, n.Attributes)
		}
	}
	nodes, _, _ = linkSources(t, map[string]string{"m.py": src + "A.__table__.append_column(1)\n"})
	if a, _ := nodeByID(nodes, "table:m.py#A"); a.Attributes["column_set"] != "partial" {
		t.Fatalf("A: %v", a.Attributes)
	}
	if b, _ := nodeByID(nodes, "table:m.py#B"); b.Attributes["state"] != "complete" {
		t.Fatalf("B: %v", b.Attributes)
	}
}

// L1: a rebound builtin name is not the builtin; A5: an alias bound after
// the class is A3.
func TestResolutionRefusals(t *testing.T) {
	nodes, _, _ := linkSources(t, map[string]string{"m.py": header + "from other import str\nclass A(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n    name: str\n    late: Later = 1\nLater = Optional[int]\n"})
	name, _ := nodeByID(nodes, "column:m.py#A.name")
	late, _ := nodeByID(nodes, "column:m.py#A.late")
	if name.Attributes["type"] != nil || late.Attributes["nullable"] != nil {
		t.Fatalf("name %v, late %v", name.Attributes, late.Attributes)
	}
}

// L5: an equal table name in a migration set is a diagnostic, never a merge.
func TestMigrationOverlap(t *testing.T) {
	files, raw := parseAll(t, map[string]string{"m.py": header + "class Team(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n    motto: str\n"})
	table := contract.Node{Identity: "table:migrations/1_a.sql#public.team", Kind: "table", Name: "team", Attributes: map[string]any{"set": "migrations", "state": "complete"}}
	col := contract.Node{Identity: "column:migrations/1_a.sql#public.team.id", Kind: "column", Name: "id"}
	nodes, _, diags := Link(Input{Python: files, Resolver: python.NewModuleResolver(files, raw), Sources: raw, Migrations: []contract.Node{table, col}, Edges: []contract.Edge{{Kind: "contains", Source: table.Identity, Target: col.Identity}}})
	if !hasDiag(diags, "schema_model_migration_overlap", "only in the model: [motto]; only in the migration: []") {
		t.Fatalf("diagnostics: %v", diags)
	}
	if n, _ := nodeByID(nodes, "table:m.py#Team"); n.Attributes["set"] != "models/sqlmodel/." {
		t.Fatalf("model table: %v", n.Attributes)
	}
}

// AGENTS.md: a failure in one step (class, site, refused, record, foreign
// keys, migration comparison, emit) becomes a diagnostic and never stops
// the others (refused and record: T018g0 Revision 2).
func TestStepIsolation(t *testing.T) {
	src := header + "class A(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n" +
		"class B(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n    a_id: int = Field(foreign_key=\"a.id\")\n" +
		"class C(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n" +
		"A.__table__.append_constraint(1)\n"
	for _, step := range []string{"class", "site", "refused", "record", "foreign keys", "migration comparison", "emit"} {
		t.Run(step, func(t *testing.T) {
			failing := map[string]string{"class": "class A", "site": "append_constraint site", "refused": "table:m.py#C", "record": "table:m.py#C", "foreign keys": "table:m.py#B", "migration comparison": "table:m.py#C", "emit": "table:m.py#C"}[step]
			// A failed refused, record or emit step drops its table; the
			// other steps keep it with what they proved.
			drops := step == "refused" || step == "record" || step == "emit"
			faultHook = func(s, subject string) {
				if s == step && strings.Contains(subject, failing) {
					panic("injected")
				}
			}
			defer func() { faultHook = nil }()
			nodes, _, diags := linkSources(t, map[string]string{"m.py": src, "other.py": header + "class D(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n"})
			if !hasDiag(diags, "schema_model_internal_error", "("+step+" step): injected") {
				t.Fatalf("no diagnostic for the failing %s step: %v", step, diags)
			}
			want := []string{"table:m.py#B", "table:other.py#D"}
			if step != "class" {
				want = append(want, "table:m.py#A")
			}
			if !drops {
				want = append(want, "table:m.py#C")
			}
			for _, id := range want {
				if _, ok := nodeByID(nodes, id); !ok {
					t.Errorf("%s: %s missing after a failure elsewhere", step, id)
				}
			}
			if drops {
				if _, ok := nodeByID(nodes, "table:m.py#C"); ok {
					t.Errorf("%s: the failed table was emitted", step)
				}
				if _, ok := nodeByID(nodes, "column:m.py#C.id"); ok {
					t.Errorf("%s: a failed table left its column behind", step)
				}
			}
		})
	}
}

// Rev 3, L7: a PrimaryKeyConstraint in __table_args__ is no P1 failure, and
// the primary key stays unknown (TA-D); TA1-TA3 elements only prove the
// constraint set holds no primary key.
func TestPrimaryKeyInTableArgs(t *testing.T) {
	src := header + "from sqlalchemy import PrimaryKeyConstraint, UniqueConstraint\n" +
		"class Pair(SQLModel, table=True):\n    __table_args__ = (PrimaryKeyConstraint(\"a\", \"b\"),)\n    a: int\n    b: int\n" +
		"class Loose(SQLModel, table=True):\n    __table_args__ = (UniqueConstraint(\"a\"),)\n    a: int\n"
	nodes, _, diags := linkSources(t, map[string]string{"m.py": src})
	pair, ok := nodeByID(nodes, "table:m.py#Pair")
	if !ok || pair.Attributes["primary_key"] != nil {
		t.Fatalf("Pair: %v %v", ok, pair.Attributes)
	}
	if a, _ := nodeByID(nodes, "column:m.py#Pair.a"); a.Attributes["primary_key"] != nil {
		t.Fatalf("Pair.a: %v", a.Attributes)
	}
	if !hasDiag(diags, "schema_model_import_failure", "model class Loose cannot be created (PK:") || hasDiag(diags, "schema_model_import_failure", "class Pair") {
		t.Fatalf("diagnostics: %v", diags)
	}
}

// TestSiteChangedFacts checks Revision 3.1: a mutation site's constraint
// kind decides which proven facts it withdraws, and append_column withdraws
// the class's column facts only when it may replace one.
func TestSiteChangedFacts(t *testing.T) {
	src := header + "from sqlalchemy import Column, Integer, PrimaryKeyConstraint, UniqueConstraint\n" +
		"class A(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    n: int\n" +
		"class B(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    n: int\n" +
		"class C(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    n: int\n" +
		"A.__table__.append_constraint(UniqueConstraint(\"n\"))\n" +
		"B.__table__.append_constraint(PrimaryKeyConstraint(\"id\", \"n\"))\n" +
		"C.__table__.append_column(Column(\"extra\", Integer), replace_existing=False)\n"
	nodes, _, _ := linkSources(t, map[string]string{"m.py": src})
	want := map[string][2]any{
		"m.py#A": {[]string{"id"}, false},
		"m.py#B": {nil, nil},
		"m.py#C": {nil, false},
	}
	for id, w := range want {
		tbl, ok := nodeByID(nodes, "table:"+id)
		if !ok {
			t.Fatalf("%s: no table", id)
		}
		if fmt.Sprint(tbl.Attributes["primary_key"]) != fmt.Sprint(w[0]) {
			t.Errorf("%s primary_key = %v, want %v", id, tbl.Attributes["primary_key"], w[0])
		}
		n, _ := nodeByID(nodes, "column:"+id+".n")
		if n.Attributes["primary_key"] != w[1] {
			t.Errorf("%s.n primary_key = %v, want %v", id, n.Attributes["primary_key"], w[1])
		}
	}
	if fks, _ := nodeByID(nodes, "column:m.py#B.n"); fks.Attributes["foreign_keys"] == nil {
		t.Errorf("B.n: a PrimaryKeyConstraint adds no foreign key: %v", fks.Attributes)
	}
}

// TestAssignTarget checks which claim an X1 attribute assignment target may
// change (Rev 3.2): only proven segments keep the table or column claimed.
func TestAssignTarget(t *testing.T) {
	cases := []struct{ dotted, target, column string }{
		{"T.__table__.comment", assignTableKeep, ""},
		{"T.__table__.info", assignTableKeep, ""},
		{"T.__table__.name", assignTableIdentity, ""},
		{"T.__table__.schema", assignTableIdentity, ""},
		{"T.__table__.fullname", assignTableIdentity, ""},
		{"T.__table__.description", assignTableIdentity, ""},
		{"T.__table__.primary_key.name", assignTableIdentity, ""},
		{"T.__table__.c", assignTableIdentity, ""},
		{"table.name", assignTableIdentity, ""},
		{"table.comment", assignTableKeep, ""},
		{"", assignTableIdentity, ""},
		{"T.__table__.c.x.nullable", assignColumnKeep, "x"},
		{"T.__table__.columns.x.type.length", assignColumnKeep, "x"},
		{"T.__table__.c.x.name", assignColumnIdentity, "x"},
		{"T.__table__.c.x.key", assignColumnIdentity, "x"},
		{"T.__table__.c.x.label", assignColumnIdentity, "x"},
		{"T.__table__.c.x", assignColumnIdentity, "x"},
		{"table.c.x.name", assignColumnIdentity, "x"},
		{"T.__table__.c.x.table.name", assignTableIdentity, ""},
	}
	for _, c := range cases {
		target, column := assignTarget(c.dotted)
		if target != c.target || column != c.column {
			t.Errorf("assignTarget(%q) = %s, %q; want %s, %q", c.dotted, target, column, c.target, c.column)
		}
	}
}

// TestRenamingAssignments checks that a table or column an X1 attribute
// assignment may rename is not claimed (Rev 3.2), and that the other
// tables and columns stay.
func TestRenamingAssignments(t *testing.T) {
	src := header +
		"class A(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    n: int\n" +
		"class B(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    n: int\n" +
		"class C(SQLModel, table=True):\n    id: int = Field(primary_key=True)\n    a_id: int = Field(foreign_key=\"a.id\")\n" +
		"A.__table__.name = \"a2\"\n" +
		"A.__table__.c.n.nullable = True\n" +
		"t = B.__table__\n" +
		"t.c.n.name = \"m\"\n"
	nodes, _, diags := linkSources(t, map[string]string{"m.py": src})
	if _, ok := nodeByID(nodes, "table:m.py#A"); ok {
		t.Errorf("A: renamed table still claimed")
	}
	if _, ok := nodeByID(nodes, "column:m.py#A.id"); ok {
		t.Errorf("A.id: column of a refused table still claimed")
	}
	found := false
	for _, d := range diags {
		if d.Code == "schema_model_not_claimed" && strings.Contains(d.Message, "class A") && strings.Contains(d.Message, "(X1: ") {
			found = true
		}
	}
	if !found {
		t.Errorf("no X1 schema_model_not_claimed diagnostic for A: %v", diags)
	}
	b, ok := nodeByID(nodes, "table:m.py#B")
	if !ok {
		t.Fatalf("B: no table")
	}
	if b.Attributes["column_set"] != "partial" || b.Attributes["unique_constraints"] != nil {
		t.Errorf("B: column set %v, unique constraints %v; want partial, unknown", b.Attributes["column_set"], b.Attributes["unique_constraints"])
	}
	if _, ok := nodeByID(nodes, "column:m.py#B.n"); ok {
		t.Errorf("B.n: renamed column still claimed")
	}
	if _, ok := nodeByID(nodes, "column:m.py#B.id"); !ok {
		t.Errorf("B.id: an unrenamed column was dropped")
	}
	c, ok := nodeByID(nodes, "column:m.py#C.a_id")
	if !ok {
		t.Fatalf("C.a_id: no column")
	}
	if fks := fmt.Sprint(c.Attributes["foreign_keys"]); !strings.Contains(fks, "unresolved") {
		t.Errorf("C.a_id: a foreign key to a refused table resolved: %s", fks)
	}
}

// L1 and M1 (Revision 3.3, G-1): classmethod, staticmethod and property
// resolve to the builtins when unbound, so K-dec reads the methods; a
// module-level binding of the name, even an alias of the builtin, keeps it
// unresolved and the method an M1 refusal.
func TestDecoratorBuiltins(t *testing.T) {
	cls := "class A(SQLModel, table=True):\n    __tablename__ = \"a\"\n    id: Optional[int] = Field(default=None, primary_key=True)\n" +
		"    @classmethod\n    def make(cls):\n        return cls()\n" +
		"    @staticmethod\n    def width():\n        return 1\n" +
		"    @property\n    def short(self):\n        return 1\n"
	for _, tc := range []struct {
		name, prefix, want string
	}{
		{"unbound", "", "complete"},
		{"alias", "import builtins\nclassmethod = builtins.classmethod\n", "partial"},
		{"def", "def property(f):\n    return f\n", "partial"},
		{"import", "from helpers import staticmethod\n", "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, _, _ := linkSources(t, map[string]string{"m.py": header + tc.prefix + cls})
			a, ok := nodeByID(nodes, "table:m.py#A")
			if !ok || a.Attributes["column_set"] != tc.want {
				t.Fatalf("A: %v", a.Attributes)
			}
		})
	}
}

// K1b (Revision 3.3, G-2): a module-level name bound to a T1 call chain is
// the column type, read as unknown; the column's other facts are read. A
// name bound to anything else may be a schema item and stays K6.
func TestColumnTypeChainAlias(t *testing.T) {
	src := header + "from sqlalchemy import JSON, Column, ForeignKey\nfrom sqlalchemy.dialects.postgresql import JSONB\n" +
		"JsonVariant = JSON().with_variant(JSONB(), \"postgresql\")\n" +
		"Plain = JSON()\n" +
		"Other = JSON().copy()\n" +
		"Ref = ForeignKey(\"a.id\")\n" +
		"class A(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n" +
		"    data: dict = Field(sa_column=Column(JsonVariant, nullable=False, server_default=\"{}\"))\n" +
		"    plain: dict = Field(sa_column=Column(Plain))\n" +
		"    other: dict = Field(sa_column=Column(Other))\n" +
		"    ref: int = Field(sa_column=Column(Ref))\n"
	nodes, _, _ := linkSources(t, map[string]string{"m.py": src})
	for _, tc := range []struct {
		col, state string
	}{{"data", "read"}, {"plain", "read"}, {"other", "unknown"}, {"ref", "unknown"}} {
		c, ok := nodeByID(nodes, "column:m.py#A."+tc.col)
		if !ok || c.Attributes["state"] != tc.state || c.Attributes["type"] != nil {
			t.Fatalf("%s: %v", tc.col, c.Attributes)
		}
	}
	data, _ := nodeByID(nodes, "column:m.py#A.data")
	if data.Attributes["nullable"] != false || data.Attributes["server_default"] != true || data.Attributes["primary_key"] != false {
		t.Fatalf("data: %v", data.Attributes)
	}
	if fmt.Sprint(data.Attributes["reasons"]) == "[]" {
		t.Fatalf("data has no K1b reason: %v", data.Attributes)
	}
}
