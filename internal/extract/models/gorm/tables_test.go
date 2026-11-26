package gorm

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// claimModule is a one-package module opening dialect d (a gorm.Open
// line) and migrating the models listed, with body holding the types.
func claimModule(open, migrate, body string, extraRequire ...string) map[string]string {
	src := "package app\n\nimport (\n\t\"time\"\n\n\t\"github.com/glebarez/sqlite\"\n\t\"gorm.io/driver/mysql\"\n\t\"gorm.io/driver/postgres\"\n\t\"gorm.io/gorm\"\n)\n\nvar _ = time.Now\nvar _ = sqlite.Open\nvar _ = mysql.Open\nvar _ = postgres.Open\n\n" + body +
		"\nfunc Open(dsn string) {\n" + open + "\n\tdb.AutoMigrate(" + migrate + ")\n}\n"
	return map[string]string{
		"go.mod": goMod("v1.31.2", append([]string{"\tgithub.com/glebarez/sqlite v1.11.0\n\tgorm.io/driver/mysql v1.6.0\n\tgorm.io/driver/postgres v1.6.3\n"}, extraRequire...)...),
		"app.go": src,
	}
}

const (
	openPG     = "\tdb, _ := gorm.Open(postgres.Open(dsn), &gorm.Config{})"
	openSQLite = "\tdb, _ := gorm.Open(sqlite.Open(dsn), &gorm.Config{})"
	openMySQL  = "\tdb, _ := gorm.Open(mysql.Open(dsn), &gorm.Config{})"
)

// linkModule runs the claim pass over inline sources.
func linkModule(t *testing.T, sources map[string]string) (map[string]contract.Node, []contract.Diagnostic) {
	t.Helper()
	in := input(t, sources)
	nodes, _, diags := Link(in)
	modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	for _, d := range diags {
		if d.Code == "schema_model_internal_error" {
			t.Errorf("internal error: %s", d.Message)
		}
	}
	return byIdentity(nodes), diags
}

// TestGormMySQLRendering checks the mapped mysql renderings (GO-T2, GO-V5;
// gorm.io/driver/mysql v1.6.0 mysql.go:360-506).
func TestGormMySQLRendering(t *testing.T) {
	body := "type T struct {\n\tID uint\n\tS string\n\tSI string `gorm:\"index\"`\n\tSD string `gorm:\"default:x\"`\n\tSU string `gorm:\"uniqueIndex\"`\n\tBig string `gorm:\"size:70000\"`\n\tB bool\n\tI8 int8\n\tI32 int32\n\tU16 uint16\n\tF32 float32\n\tF float64\n\tD float64 `gorm:\"precision:10;scale:2\"`\n\tAt time.Time\n\tAt0 time.Time `gorm:\"precision:0\"`\n\tRaw []byte\n\tRawS []byte `gorm:\"size:100\"`\n\tN int `gorm:\"default:5\"`\n\tOk bool `gorm:\"default:true\"`\n}\n"
	n, _ := linkModule(t, claimModule(openMySQL, "&T{}", body))
	want := map[string]string{
		"id": `"bigint unsigned"`, "s": `"longtext"`, "si": `"varchar(191)"`, "sd": `"varchar(191)"`, "su": `"longtext"`,
		"big": `"mediumtext"`, "b": `"boolean"`, "i8": `"tinyint"`, "i32": `"int"`, "u16": `"smallint unsigned"`,
		"f32": `"float"`, "f": `"double"`, "d": `"decimal(10, 2)"`, "at": `"datetime(3)"`, "at0": `"datetime"`,
		"raw": `"longblob"`, "raw_s": `"varbinary(100)"`,
	}
	for col, typ := range want {
		c := n["column:app.go#T."+col]
		if got := attr(c, "type"); got != typ {
			t.Errorf("%s: type %s, want %s", col, got, typ)
		}
		for _, cl := range modeltestClaims(t, c) {
			if cl.Verification != models.VerificationMapped {
				t.Errorf("%s: claim %+v is not mapped", col, cl)
			}
		}
	}
	if attr(n["column:app.go#T.id"], "autoincrement") != "true" || attr(n["column:app.go#T.id"], "nullable") != "false" || attr(n["column:app.go#T.at"], "precision") != "3" {
		t.Errorf("id or at: %v %v", n["column:app.go#T.id"].Attributes, n["column:app.go#T.at"].Attributes)
	}
	if attr(n["column:app.go#T.n"], "server_default_text") != `"5"` || attr(n["column:app.go#T.ok"], "server_default_text") != "-" || attr(n["column:app.go#T.ok"], "server_default") != "true" {
		t.Errorf("defaults: n %s, ok %s", attr(n["column:app.go#T.n"], "server_default_text"), attr(n["column:app.go#T.ok"], "server_default_text"))
	}
	if got := attr(n["table:app.go#T"], "dialect"); got != `"mysql"` {
		t.Errorf("dialect %s", got)
	}
	// mysql.New with a non-literal DefaultStringSize: string types unknown,
	// datetime precision still read; a literal one replaces 191.
	open := "\tsize := uint(100)\n\tdb, _ := gorm.Open(mysql.New(mysql.Config{DefaultStringSize: size, DisableDatetimePrecision: true}), &gorm.Config{})"
	n, _ = linkModule(t, claimModule(open, "&T{}", body))
	if attr(n["column:app.go#T.s"], "type") != "-" || attr(n["column:app.go#T.at"], "type") != `"datetime"` {
		t.Errorf("non-literal size: s %s, at %s", attr(n["column:app.go#T.s"], "type"), attr(n["column:app.go#T.at"], "type"))
	}
	open = "\tdb, _ := gorm.Open(mysql.New(mysql.Config{DefaultStringSize: 256}), &gorm.Config{})"
	n, _ = linkModule(t, claimModule(open, "&T{}", body))
	if attr(n["column:app.go#T.si"], "type") != `"varchar(256)"` {
		t.Errorf("literal size: si %s", attr(n["column:app.go#T.si"], "type"))
	}
}

// TestGormColumnRules checks single rules of GO-T1, GO-T2, GO-C1-C3 and
// GO-F1 on postgres.
func TestGormColumnRules(t *testing.T) {
	body := "type Named string\n\ntype T struct {\n\tID uint\n\tRawAI uint `gorm:\"type:varchar(36);autoIncrement\"`\n\tHex int `gorm:\"default:0x10\"`\n\tNeg int `gorm:\"default:-1\"`\n\tQuote string `gorm:\"default:it's\"`\n\tOdd string `gorm:\"type:inet;default:'x'\"`\n\tN Named\n\tA string `gorm:\"column:dup\"`\n\tB string `gorm:\"column:dup\"`\n\tAny any\n\tPayload any `gorm:\"serializer:json\"`\n\tU32 uint32\n}\n"
	n, diags := linkModule(t, claimModule(openPG, "&T{}", body))
	for col, want := range map[string]string{
		"raw_ai.type": `"bigint"`, "raw_ai.autoincrement": "true", "id.type": `"bigint"`,
		"hex.server_default": "true", "hex.server_default_text": "-", "neg.server_default_text": "-",
		"quote.server_default_text": "-", "odd.type": "-", "odd.server_default_text": "-",
		"n.type": `"text"`, "dup.state": `"unknown"`, "any.state": `"unknown"`, "payload.type": `"text"`,
		"u32.type": `"bigint"`,
	} {
		id, key, _ := strings.Cut(col, ".")
		if got := attr(n["column:app.go#T."+id], key); got != want {
			t.Errorf("%s: %s, want %s", col, got, want)
		}
	}
	tab := n["table:app.go#T"]
	if attr(tab, "column_set") != `"partial"` {
		t.Errorf("the any column leaves the column set complete: %v", tab.Attributes["reasons"])
	}
	for _, d := range diags {
		if d.Code == codeNotClaimed {
			t.Errorf("unexpected: %s", d.Message)
		}
	}
}

// TestGormTableNames checks the table-name forms of GO-K4: constant
// folding across packages, ambiguous promotion, an embedded type outside
// the module, inflection changes, non-ASCII names and two models with one
// table name.
func TestGormTableNames(t *testing.T) {
	body := "type A struct{ ID uint }\n\nfunc (A) TableName() string { return \"a\" }\n\ntype B struct{ ID uint }\n\nfunc (B) TableName() string { return \"b\" }\n\n" +
		"type Both struct {\n\tA\n\tB\n}\n\ntype Ext struct {\n\ttime.Time\n\tID uint\n}\n\ntype Größe struct{ ID uint }\n\ntype Same1 struct{ ID uint }\n\nfunc (Same1) TableName() string { return \"same\" }\n\ntype Same2 struct{ ID uint }\n\nfunc (Same2) TableName() string { return \"same\" }\n\ntype Plain struct {\n\tID uint\n\tÄlter int\n}\n"
	n, diags := linkModule(t, claimModule(openPG, "&Both{}, &Ext{}, &Größe{}, &Same1{}, &Same2{}, &Plain{}", body))
	for _, id := range []string{"Both", "Ext", "Größe", "Same1", "Same2"} {
		if _, ok := n["table:app.go#"+id]; ok {
			t.Errorf("%s is claimed", id)
		}
	}
	if attr(n["table:app.go#Plain"], "column_set") != `"partial"` || attr(n["table:app.go#Plain"], "name") != `"plains"` {
		t.Errorf("Plain: %v", n["table:app.go#Plain"].Attributes)
	}
	got := strings.Join(messages(diags, codeNotClaimed), "\n")
	for _, want := range []string{"two embedded structs of Both", "embeds Time, a type whose TableName", "Größe is not ASCII", "2 models of the group have the table name same"} {
		if !strings.Contains(got, want) {
			t.Errorf("no not-claimed diagnostic %q in:\n%s", want, got)
		}
	}
	// inflection.Add*: plural names unknown, a singular strategy still
	// names the table.
	src := claimModule(openSQLite, "&Plain{}", "type Plain struct{ ID uint }\n")
	src["infl.go"] = "package app\n\nimport \"github.com/jinzhu/inflection\"\n\nfunc init() { inflection.AddIrregular(\"plain\", \"plainz\") }\n"
	n, diags = linkModule(t, src)
	if _, ok := n["table:app.go#Plain"]; ok || !strings.Contains(strings.Join(messages(diags, codeNotClaimed), ""), "inflection") {
		t.Errorf("inflection change: claimed %v, %v", ok, messages(diags, codeNotClaimed))
	}
	src["app.go"] = strings.Replace(src["app.go"], "&gorm.Config{}", "&gorm.Config{NamingStrategy: schema.NamingStrategy{SingularTable: true}}", 1)
	src["app.go"] = strings.Replace(src["app.go"], "\"gorm.io/gorm\"\n", "\"gorm.io/gorm\"\n\t\"gorm.io/gorm/schema\"\n", 1)
	n, _ = linkModule(t, src)
	if attr(n["table:app.go#Plain"], "name") != `"plain"` {
		t.Errorf("singular table with inflection change: %v", n["table:app.go#Plain"].Attributes)
	}
}

// TestGormIndexRules checks shared index names (postgres: list unknown;
// sqlite: GO-P1) and the per-dialect facts of a run-time group whose
// primary key differs on sqlite.
func TestGormIndexRules(t *testing.T) {
	body := "type P struct {\n\tID uint\n\tX string `gorm:\"index:idx_x\"`\n}\n\ntype Q struct {\n\tID uint\n\tX string `gorm:\"index:idx_x\"`\n}\n"
	n, _ := linkModule(t, claimModule(openPG, "&P{}, &Q{}", body))
	// P is created first (its own segment of the call): postgres keeps
	// idx_x on P and skips it on Q.
	if attr(n["column:app.go#P.x"], "index") != "true" || attr(n["table:app.go#Q"], "indexes") != "[]" || attr(n["column:app.go#Q.x"], "index") != "false" {
		t.Errorf("shared index on postgres: P %s, Q %s", attr(n["table:app.go#P"], "indexes"), attr(n["table:app.go#Q"], "indexes"))
	}
	// Two calls whose order is not proven: which table is first is not
	// known, so both index lists are unknown.
	src := claimModule(openPG, "&P{}", body)
	src["app.go"] = strings.Replace(src["app.go"], "db.AutoMigrate(&P{})", "db.AutoMigrate(&P{})\n\tgo db.AutoMigrate(&Q{})", 1)
	n, _ = linkModule(t, src)
	if attr(n["table:app.go#P"], "indexes") != "-" || attr(n["table:app.go#Q"], "indexes") != "-" {
		t.Errorf("shared index, order not proven: P %s, Q %s", attr(n["table:app.go#P"], "indexes"), attr(n["table:app.go#Q"], "indexes"))
	}
	// sqlite fails the second CREATE INDEX: Q is not claimed (GO-P1).
	n, diags := linkModule(t, claimModule(openSQLite, "&P{}, &Q{}", body))
	if _, ok := n["table:app.go#P"]; !ok || len(messages(diags, codeNotClaimed)) != 1 {
		t.Errorf("shared index on sqlite: %v", messages(diags, codeNotClaimed))
	}
	// A run-time group (postgres, sqlite): a composite key with an
	// autoincrement member is (a, b) on postgres and (a) on sqlite.
	open := "\tvar db *gorm.DB\n\tif dsn == \"\" {\n\t\tdb, _ = gorm.Open(sqlite.Open(dsn), &gorm.Config{})\n\t} else {\n\t\tdb, _ = gorm.Open(postgres.Open(dsn), &gorm.Config{})\n\t}"
	body = "type C struct {\n\tA uint `gorm:\"primaryKey;autoIncrement\"`\n\tB uint `gorm:\"primaryKey\"`\n\tName string `gorm:\"not null;default:'n'\"`\n}\n"
	n, _ = linkModule(t, claimModule(open, "&C{}", body))
	if attr(n["table:app.go#C"], "primary_key") != "-" || attr(n["column:app.go#C.a"], "primary_key") != "-" {
		t.Errorf("runtime composite key: %s", attr(n["table:app.go#C"], "primary_key"))
	}
	name := n["column:app.go#C.name"]
	e := modeltest.DialectEntries(name)
	if attr(name, "nullable") != "false" || e["postgres"]["server_default_text"] != "'n'::text" || e["sqlite"]["server_default_text"] != `"n"` || attr(name, "default") != `"server"` {
		t.Errorf("runtime name: %v %v", name.Attributes, e)
	}
}

// TestGormRelationOrder checks the order-dependent relation facts
// (GO-V6): a has-one parent's key type is copied onto the child's
// foreign key (its own size 64 kept, relationship.go:600-602), and its
// constraint registered on the child, only when the
// parent is parsed before the child's table is created; with the calls'
// order not proven, both are unknown.
func TestGormRelationOrder(t *testing.T) {
	body := "type Owner struct {\n\tID   string `gorm:\"primaryKey;size:20\"`\n\tCard CreditCard\n}\n\ntype CreditCard struct {\n\tID      uint\n\tOwnerID uint\n}\n"
	for _, tc := range []struct {
		name, calls, typ, fks string
	}{
		{"child first", "&CreditCard{})\n\tdb.AutoMigrate(&Owner{}", `"bigint"`, "[]"},
		{"parent first", "&Owner{})\n\tdb.AutoMigrate(&CreditCard{}", `"character varying(64)"`, `[{"column":"id","columns":["id"],"local_columns":["owner_id"],"name":"fk_owners_card","on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"owners"}]`},
		{"one call", "&CreditCard{}, &Owner{}", `"character varying(64)"`, `[{"column":"id","columns":["id"],"local_columns":["owner_id"],"name":"fk_owners_card","on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"owners"}]`},
		{"order not proven", "&CreditCard{})\n\tgo db.AutoMigrate(&Owner{}", "-", "-"},
	} {
		n, diags := linkModule(t, claimModule(openPG, tc.calls, body))
		c := n["column:app.go#CreditCard.owner_id"]
		if attr(c, "type") != tc.typ || attr(c, "foreign_keys") != tc.fks {
			t.Errorf("%s: type %s, foreign keys %s (%v)", tc.name, attr(c, "type"), attr(c, "foreign_keys"), messages(diags, codeNotClaimed))
		}
	}
}
