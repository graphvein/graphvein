package gorm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// The T018g5 Revision 2 rules (independent review,
// decisions): A1 raw type texts, A2 SetupJoinTable, A3 Migrator DDL and DDL
// text, A4 handle flows, A5 the added failures, A6 local types, A7 driver
// pins, C1 the discovery guard and the dialector local.

// hasMessage reports a diagnostic of code whose message contains part.
func hasMessage(diags []contract.Diagnostic, code, part string) bool {
	for _, d := range diags {
		if d.Code == code && strings.Contains(d.Message, part) {
			return true
		}
	}
	return false
}

// TestGormHandleFlows checks the receiver proof's handle flows (A4): a
// module function's returns, a parameter's calls, a field's literals and
// assignments, a local's assignments.
func TestGormHandleFlows(t *testing.T) {
	cases := []struct {
		name, body, reason string
	}{
		{"scoped return", "func scoped(db *gorm.DB) *gorm.DB {\n\treturn db.Table(\"x\")\n}\n\nfunc f() {\n\tdb := scoped(open())\n\tdb.AutoMigrate(&A{})\n}\n" + open,
			"returns a value at line"},
		{"proven return", "func wrap(db *gorm.DB) *gorm.DB {\n\tif db == nil {\n\t\treturn nil\n\t}\n\treturn db.Debug()\n}\n\nfunc f() {\n\tdb := wrap(open())\n\tdb.AutoMigrate(&A{})\n}\n" + open, ""},
		{"bare return", "func named() (db *gorm.DB) {\n\tdb = open()\n\treturn\n}\n\nfunc f() {\n\tdb := named()\n\tdb.AutoMigrate(&A{})\n}\n" + open,
			"has a bare return"},
		{"scoped argument", "func m(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n\nfunc f() {\n\tdb := open()\n\tm(db)\n\tm(db.Table(\"x\"))\n}\n" + open,
			"passes a value that is not a proven *gorm.DB (chain method Table"},
		{"scoped local argument", "func m(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n\nfunc f() {\n\tdb := open()\n\tx := db.Scopes()\n\tm(x)\n}\n" + open,
			"passes a value that is not a proven *gorm.DB"},
		{"proven argument", "func m(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n\nfunc f() {\n\tm(open().Debug())\n}\n" + open, ""},
		{"method parameter", "type S struct{}\n\nfunc (s S) m(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n" + open,
			"a method's calls are not resolved"},
		{"scoped field literal", "type Store struct{ db *gorm.DB }\n\nfunc (s *Store) f() {\n\ts.db.AutoMigrate(&A{})\n}\n\nfunc g() {\n\t_ = &Store{db: open().Table(\"x\")}\n}\n" + open,
			"the field Store.db is set at app/db.go:"},
		{"scoped positional literal", "type Store struct{ db *gorm.DB }\n\nfunc (s *Store) f() {\n\ts.db.AutoMigrate(&A{})\n}\n\nfunc g() {\n\t_ = Store{open().Table(\"x\")}\n}\n" + open,
			"the field Store.db is set at"},
		{"scoped field store", "type Store struct{ db *gorm.DB }\n\nfunc (s *Store) f() {\n\ts.db.AutoMigrate(&A{})\n}\n\nfunc (s *Store) g() {\n\ts.db = s.db.Model(&A{})\n}\n" + open,
			"the field Store.db is set at"},
		{"proven field", "type Store struct{ db *gorm.DB }\n\nfunc (s *Store) f() {\n\ts.db.AutoMigrate(&A{})\n}\n\nfunc g() {\n\t_ = &Store{db: open()}\n}\n" + open, ""},
		{"scoped assignment", "func f() {\n\tdb := open()\n\tdb = db.Table(\"x\")\n\tdb.AutoMigrate(&A{})\n}\n" + open,
			"an assignment of it at line"},
		{"scoped var value", "func f() {\n\tvar db *gorm.DB = open().Table(\"x\")\n\tdb.AutoMigrate(&A{})\n}\n" + open,
			"chain method Table"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mods, diags, _ := discover(t, module(c.body))
			m := one(t, mods)
			if len(m.Sites) != 1 {
				t.Fatalf("%d sites", len(m.Sites))
			}
			s := m.Sites[0]
			if c.reason == "" {
				if !s.Proven() {
					t.Errorf("site not proven: %s", s.Reason)
				}
				return
			}
			if s.Proven() || !strings.Contains(s.Reason, c.reason) {
				t.Errorf("reason %q, want %q", s.Reason, c.reason)
			}
			if !hasMessage(diags, codeUnread, ListMarker(m.SetKey())) {
				t.Errorf("no list marker: %v", messages(diags, codeUnread))
			}
		})
	}
	// A call of the parameter's function from a file outside gate G1.
	src := module("func M(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n"+open,
		"app/other.go", "package app\n\nfunc g() {\n\tM(nil)\n}\n")
	mods, _, _ := discover(t, src)
	if s := one(t, mods).Sites[0]; s.Proven() || !strings.Contains(s.Reason, "a file without a GORM import") {
		t.Errorf("ungated call: reason %q", s.Reason)
	}
}

// TestGormLocalType checks A6: a migrate argument naming a type declared
// inside the function is not the package's type.
func TestGormLocalType(t *testing.T) {
	mods, diags, _ := discover(t, module("func f() {\n\tdb := open()\n\ttype A struct{ ID, Size uint }\n\tdb.AutoMigrate(&A{}, &B{})\n}\n"+open))
	m := one(t, mods)
	if got := identities(m); len(got) != 1 || !strings.HasSuffix(got[0], "#B") {
		t.Errorf("models %v, want only B", got)
	}
	if !hasMessage(diags, codeUnread, "names A, a type declared inside the function") {
		t.Errorf("markers %v", messages(diags, codeUnread))
	}
}

// TestGormJoinSetup checks A2: SetupJoinTable's relation keeps no join
// table, its join model is not "not proven"; one that is not read keeps
// none.
func TestGormJoinSetup(t *testing.T) {
	body := "type P struct {\n\tID uint\n\tTags []T `gorm:\"many2many:p_tags\"`\n\tOthers []O `gorm:\"many2many:p_others\"`\n}\n\ntype T struct{ ID uint }\n\ntype O struct{ ID uint }\n\ntype PT struct {\n\tPID uint `gorm:\"primaryKey\"`\n\tTID uint `gorm:\"primaryKey\"`\n}\n"
	src := claimModule(openSQLite+"\n\tdb.SetupJoinTable(&P{}, \"Tags\", &PT{})", "&P{}", body)
	n, diags := linkModule(t, src)
	if _, ok := n["table:app.go#P.Tags"]; ok {
		t.Error("the replaced join table is claimed")
	}
	if _, ok := n["table:app.go#P.Others"]; !ok {
		t.Error("the other join table is not claimed")
	}
	if !hasMessage(diags, codeNotClaimed, "join table p_tags is not claimed: SetupJoinTable at app.go:") {
		t.Errorf("not claimed: %v", messages(diags, codeNotClaimed))
	}
	if hasMessage(diags, codeNotProven, "struct PT") {
		t.Error("the join model is reported not proven")
	}
	// A field name that is not a literal: every join table is row D.
	src = claimModule(openSQLite+"\n\tf := \"Tags\"\n\tdb.SetupJoinTable(&P{}, f, &PT{})", "&P{}", body)
	n, diags = linkModule(t, src)
	if _, ok := n["table:app.go#P.Others"]; ok {
		t.Error("a join table is claimed beside a SetupJoinTable that is not read")
	}
	if !hasMessage(diags, codeUnread, "SetupJoinTable call not read") {
		t.Errorf("markers %v", messages(diags, codeUnread))
	}
}

// TestGormSchemaChanges checks A3: Migrator DDL calls and DDL text.
func TestGormSchemaChanges(t *testing.T) {
	body := "type U struct {\n\tID uint\n\tName string `gorm:\"index\"`\n\tOld string\n}\n\ntype V struct{ ID uint }\n"
	src := claimModule(openPG+"\n\tdb.Migrator().DropColumn(&U{}, \"Old\")", "&U{}, &V{}", body)
	n, diags := linkModule(t, src)
	u := n["table:app.go#U"]
	if attr(u, "column_set") != `"partial"` || attr(u, "constraint_set") != `"partial"` {
		t.Errorf("U sets %s %s", attr(u, "column_set"), attr(u, "constraint_set"))
	}
	if attr(n["column:app.go#U.old"], "state") != `"unknown"` || attr(n["column:app.go#U.name"], "state") != `"read"` {
		t.Errorf("columns old %s, name %s", attr(n["column:app.go#U.old"], "state"), attr(n["column:app.go#U.name"], "state"))
	}
	if attr(n["table:app.go#V"], "column_set") != `"complete"` {
		t.Error("V is changed")
	}
	if !hasMessage(diags, codeUnread, "Migrator().DropColumn changes the schema of the table of U") {
		t.Errorf("markers %v", messages(diags, codeUnread))
	}
	// An index method: the index lists unknown.
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Migrator().DropIndex(&U{}, \"idx_us_name\")", "&U{}", body))
	if attr(n["table:app.go#U"], "indexes") != "-" || attr(n["column:app.go#U.name"], "index") != "-" {
		t.Errorf("indexes %s, name index %s", attr(n["table:app.go#U"], "indexes"), attr(n["column:app.go#U.name"], "index"))
	}
	// A model that is not read: every table.
	n, _ = linkModule(t, claimModule(openPG+"\n\tvar x any\n\tdb.Migrator().DropColumn(x, \"Old\")", "&U{}, &V{}", body))
	if attr(n["table:app.go#V"], "column_set") != `"partial"` {
		t.Error("V is not partial beside a DropColumn whose model is not read")
	}
	// A string table name.
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Migrator().RenameTable(\"vs\", \"ws\")", "&U{}, &V{}", body))
	if attr(n["table:app.go#V"], "column_set") != `"partial"` || attr(n["table:app.go#U"], "column_set") != `"complete"` {
		t.Error("RenameTable(\"vs\") does not change only V")
	}
	// DDL text: the table list may be incomplete; other text: nothing.
	mods, diags, _ := discover(t, claimModule(openPG+"\n\tdb.Exec(\"create index x on us (name)\")\n\tdb.Raw(\"SELECT 1\")", "&U{}", body))
	m := one(t, mods)
	if len(m.RawDDL) != 1 || !hasMessage(diags, codeUnread, "Exec runs DDL text (CREATE ...)"+"") || !hasMessage(diags, codeUnread, ListMarker(m.SetKey())) {
		t.Errorf("raw DDL %v, markers %v", m.RawDDL, messages(diags, codeUnread))
	}
	// A Migrator DDL name in a file without a GORM import.
	src = claimModule(openPG, "&U{}, &V{}", body)
	src["other.go"] = "package app\n\nfunc g(m interface{ DropTable(...any) error }) {\n\tm.DropTable(\"us\")\n}\n"
	n, _ = linkModule(t, src)
	if attr(n["table:app.go#V"], "column_set") != `"partial"` {
		t.Error("an ungated DropTable does not change every table")
	}
}

// TestGormRawTypes checks A1: a raw type text with more than a type name,
// and a raw serial type, on every dialect.
func TestGormRawTypes(t *testing.T) {
	body := "type R struct {\n\tID uint\n\tA string `gorm:\"type:varchar(100) NOT NULL\"`\n\tB int `gorm:\"type:int(11) NOT NULL AUTO_INCREMENT\"`\n\tS int64 `gorm:\"type:bigserial\"`\n\tP string `gorm:\"type:numeric(10, 2)\"`\n\tQ string `gorm:\"type:text[]\"`\n}\n"
	for _, open := range []string{openPG, openSQLite, openMySQL} {
		n, _ := linkModule(t, claimModule(open, "&R{}", body))
		for _, col := range []string{"a", "b"} {
			c := n["column:app.go#R."+col]
			for _, fact := range []string{"type", "nullable", "default", "unique", "foreign_keys"} {
				if v := attr(c, fact); v != "-" {
					t.Errorf("%s: %s %s claimed (%s)", col, fact, v, open)
				}
			}
		}
		s := n["column:app.go#R.s"]
		for _, fact := range []string{"nullable", "default", "autoincrement"} {
			if v := attr(s, fact); v != "-" {
				t.Errorf("serial: %s %s claimed (%s)", fact, v, open)
			}
		}
		if attr(n["column:app.go#R.p"], "nullable") != "true" || attr(n["column:app.go#R.q"], "nullable") != "true" {
			t.Errorf("one type name with modifiers is not read (%s)", open)
		}
		if attr(n["table:app.go#R"], "constraint_set") != `"partial"` {
			t.Errorf("constraint set %s (%s)", attr(n["table:app.go#R"], "constraint_set"), open)
		}
	}
}

// TestGormRevision2Failures checks A5: a postgres index class other than
// UNIQUE and a bare sqlite function default fail their call.
func TestGormRevision2Failures(t *testing.T) {
	body := "type F struct {\n\tID uint\n\tT string `gorm:\"index:,class:FULLTEXT\"`\n}\n\ntype G struct{ ID uint }\n\ntype D struct {\n\tID uint\n\tAt time.Time `gorm:\"default:datetime('now')\"`\n}\n\ntype W struct {\n\tID uint\n\tAt time.Time `gorm:\"default:(datetime('now'))\"`\n}\n"
	n, diags := linkModule(t, claimModule(openPG, "&F{}, &G{}", body))
	if _, ok := n["table:app.go#F"]; ok || n["table:app.go#G"].Identity != "" {
		t.Error("postgres: the failing call's tables are claimed")
	}
	if !hasMessage(diags, codeNotClaimed, "has class FULLTEXT") {
		t.Errorf("not claimed %v", messages(diags, codeNotClaimed))
	}
	n, diags = linkModule(t, claimModule(openSQLite, "&W{}, &D{}, &G{}", body))
	if _, ok := n["table:app.go#W"]; !ok {
		t.Error("sqlite: a wrapped default is not claimed")
	}
	if _, ok := n["table:app.go#D"]; ok || n["table:app.go#G"].Identity != "" {
		t.Error("sqlite: the bare default's call is claimed")
	}
	if !hasMessage(diags, codeNotClaimed, "is a function call not wrapped in parentheses") {
		t.Errorf("not claimed %v", messages(diags, codeNotClaimed))
	}
	// postgres accepts the bare call.
	if n, _ = linkModule(t, claimModule(openPG, "&D{}", body)); n["table:app.go#D"].Identity == "" {
		t.Error("postgres: the bare default is not claimed")
	}
}

// TestGormDriverPins checks A7: driver pins outside the verified versions
// are shown with GO-V1; an old postgres driver leaves long generated names
// unknown.
func TestGormDriverPins(t *testing.T) {
	body := "type L struct {\n\tID uint\n\tAVeryLongFieldNameThatMakesTheIndexNameLongerThanSixtyThree string `gorm:\"index\"`\n\tShort string `gorm:\"index\"`\n}\n"
	src := claimModule(openPG, "&L{}", body)
	src["go.mod"] = strings.Replace(src["go.mod"], "gorm.io/driver/postgres v1.6.3", "gorm.io/driver/postgres v1.5.4", 1)
	n, _ := linkModule(t, src)
	tb := n["table:app.go#L"]
	if !strings.Contains(attr(tb, "assumption_notes"), "gorm.io/driver/postgres v1.5.4 is not a verified driver version") {
		t.Errorf("notes %s", attr(tb, "assumption_notes"))
	}
	idx := attr(tb, "indexes")
	if !strings.Contains(idx, `"name":null`) || !strings.Contains(idx, `"name":"idx_ls_short"`) {
		t.Errorf("indexes %s", idx)
	}
	n, _ = linkModule(t, claimModule(openPG, "&L{}", body))
	if v := attr(n["table:app.go#L"], "assumption_notes"); v != "-" {
		t.Errorf("verified pins carry notes %s", v)
	}
}

// TestGormDialectorLocal checks the dialector local: the group's dialects
// are the assigned dialectors'; another value refuses the module.
func TestGormDialectorLocal(t *testing.T) {
	body := "func f(kind string) {\n\tvar d gorm.Dialector\n\tswitch kind {\n\tcase \"a\":\n\t\td = sqlite.Open(\"a\")\n\tdefault:\n\t\td = postgres.Open(\"b\")\n\t}\n\tdb, _ := gorm.Open(d, &gorm.Config{})\n\tdb.AutoMigrate(&A{})\n}\n"
	src := module(body)
	src["go.mod"] = goMod("v1.31.2", "\tgorm.io/driver/postgres v1.6.3\n")
	src["app/db.go"] = strings.Replace(src["app/db.go"], "\t\"gorm.io/driver/sqlite\"\n", "\t\"gorm.io/driver/postgres\"\n\t\"gorm.io/driver/sqlite\"\n", 1)
	mods, _, _ := discover(t, src)
	m := one(t, mods)
	if m.Refused != "" || strings.Join(m.Dialects, ",") != "postgres,sqlite" {
		t.Errorf("refused %q, dialects %v", m.Refused, m.Dialects)
	}
	src["app/db.go"] = strings.Replace(src["app/db.go"], "\t\td = postgres.Open(\"b\")", "\t\td = other()", 1) + "\nfunc other() gorm.Dialector { return nil }\n"
	mods, _, _ = discover(t, src)
	if m := one(t, mods); m.Refused == "" {
		t.Error("a dialector local with another value is read")
	}
}

// TestGormDiscoverIsolation checks C1: a failure while reading one module
// is a diagnostic; the other modules are read.
func TestGormDiscoverIsolation(t *testing.T) {
	src := module("func f() {\n\tdb := open()\n\tdb.AutoMigrate(&A{})\n}\n" + open)
	src["b/go.mod"] = strings.Replace(src["go.mod"], "example.com/app", "example.com/b", 1)
	src["b/db.go"] = strings.Replace(src["app/db.go"], "package app", "package b", 1)
	faultHook = func(step, subject string) {
		if step == "discover" && subject == "GORM module b" {
			panic("injected")
		}
	}
	defer func() { faultHook = nil }()
	mods, diags, _ := discover(t, src)
	if len(mods) != 1 || mods[0].Dir != "." {
		t.Fatalf("%d modules", len(mods))
	}
	if !hasMessage(diags, "schema_model_internal_error", "injected") {
		t.Errorf("diagnostics %v", diags)
	}
}

// TestGormRawDDLStatements checks how DDL text is read (Revision 2
// addendum): the statements that may change a claimed table
// and the tables they name (none: every table).
func TestGormRawDDLStatements(t *testing.T) {
	cases := []struct {
		text, word, want string
	}{
		{"ALTER TABLE users DROP COLUMN legacy", "ALTER", "users"},
		{`alter table if exists only "public"."Users" rename to people`, "ALTER", "Users,people"},
		{"ALTER TABLE `orders` ADD COLUMN note text DEFAULT 'a;b'", "ALTER", "orders"},
		{"ALTER TYPE mood ADD VALUE 'x'", "ALTER", "*"},
		{"DROP TABLE IF EXISTS a, public.b CASCADE", "DROP", "a,b"},
		{"DROP INDEX idx_users_name", "DROP", "*"},
		{"RENAME TABLE a TO b, c TO d", "RENAME", "a,b,c,d"},
		{"CREATE UNIQUE INDEX IF NOT EXISTS ix ON ONLY users (name)", "CREATE", "index users"},
		{"CREATE INDEX ix USING btree", "CREATE", "index *"},
		{"CREATE TABLE audit (id bigint)", "CREATE", "audit"},
		{`create temporary table if not exists "app"."Users" (id int)`, "CREATE", "Users"},
		{"CREATE UNLOGGED TABLE IF NOT EXISTS `orders` LIKE base", "CREATE", "orders"},
		{"CREATE OR REPLACE TABLE t (id int)", "CREATE", "t"},
		{"CREATE TABLE IF NOT EXISTS %s (id int)", "CREATE", "*"},
		{"CREATE VIEW v AS SELECT 1", "CREATE", ""},
		{"CREATE OR REPLACE VIEW users AS SELECT 1", "CREATE", ""},
		{"CREATE TRIGGER t BEFORE INSERT ON users FOR EACH ROW EXECUTE FUNCTION f()", "CREATE", ""},
		{"-- note\nSELECT 1; /* c */ drop table x", "DROP", "x"},
		{"SELECT 1", "", ""},
	}
	for _, c := range cases {
		word, changes := readRawDDL(c.text)
		var got []string
		for _, ch := range changes {
			s := strings.Join(ch.Tables, ",")
			if len(ch.Tables) == 0 {
				s = "*"
			}
			if ch.IndexOnly {
				s = "index " + s
			}
			got = append(got, s)
		}
		if word != c.word || strings.Join(got, ";") != c.want {
			t.Errorf("%q: word %q changes %q, want %q %q", c.text, word, strings.Join(got, ";"), c.word, c.want)
		}
	}
}

// TestGormRawDDLTables checks the claim pass beside DDL text (Revision 2
// addendum): a named table keeps only its names, a text whose target is
// not read leaves every table only its names (a CREATE TABLE as an ALTER,
// part 2), a CREATE INDEX leaves the named table's index lists unknown and
// a CREATE VIEW changes no claimed table.
func TestGormRawDDLTables(t *testing.T) {
	body := "type U struct {\n\tID uint\n\tName string `gorm:\"index\"`\n}\n\ntype V struct{ ID uint }\n"
	names := func(n map[string]contract.Node, table string) bool {
		tb := n["table:app.go#"+table]
		return attr(tb, "column_set") == `"partial"` && attr(tb, "constraint_set") == `"partial"` && attr(tb, "primary_key") == "-" &&
			attr(tb, "indexes") == "-" && attr(tb, "schema_known") == "false" && attr(n["column:app.go#"+table+".id"], "state") == `"unknown"`
	}
	n, _ := linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"ALTER TABLE \\\"us\\\" DROP COLUMN name\")", "&U{}, &V{}", body))
	if !names(n, "U") || attr(n["table:app.go#V"], "column_set") != `"complete"` || attr(n["column:app.go#V.id"], "state") != `"read"` {
		t.Errorf("ALTER TABLE us: U %v, V column_set %s", names(n, "U"), attr(n["table:app.go#V"], "column_set"))
	}
	r := n["table:app.go#U"].Attributes["reasons"]
	if raw, _ := json.Marshal(r); !strings.Contains(string(raw), `runs \"ALTER TABLE \\\"us\\\" DROP COLUMN name\"`) {
		t.Errorf("U reasons do not name the statement: %s", raw)
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"DROP INDEX idx_us_name\")", "&U{}, &V{}", body))
	if !names(n, "U") || !names(n, "V") {
		t.Error("a DROP whose table is not read does not leave every table only its names")
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Raw(\"CREATE INDEX ix ON vs (id)\")", "&U{}, &V{}", body))
	if attr(n["table:app.go#V"], "indexes") != "-" || attr(n["table:app.go#V"], "column_set") != `"complete"` || attr(n["table:app.go#U"], "indexes") == "-" {
		t.Errorf("CREATE INDEX ON vs: V indexes %s, U indexes %s", attr(n["table:app.go#V"], "indexes"), attr(n["table:app.go#U"], "indexes"))
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"CREATE TABLE audit (id bigint)\")", "&U{}, &V{}", body))
	if attr(n["table:app.go#U"], "indexes") == "-" || attr(n["table:app.go#U"], "column_set") != `"complete"` {
		t.Error("CREATE TABLE of an unclaimed table changes a claimed table")
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"CREATE TEMP TABLE IF NOT EXISTS public.\\\"US\\\" (id int)\")", "&U{}, &V{}", body))
	if !names(n, "U") || attr(n["table:app.go#V"], "column_set") != `"complete"` {
		t.Errorf("CREATE TABLE us: U %v, V column_set %s", names(n, "U"), attr(n["table:app.go#V"], "column_set"))
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"CREATE TABLE IF NOT EXISTS (id int)\")", "&U{}, &V{}", body))
	if !names(n, "U") || !names(n, "V") {
		t.Error("a CREATE TABLE whose target is not read does not leave every table only its names")
	}
	n, _ = linkModule(t, claimModule(openPG+"\n\tdb.Exec(\"CREATE VIEW us AS SELECT 1\")", "&U{}, &V{}", body))
	if attr(n["table:app.go#U"], "indexes") == "-" || attr(n["table:app.go#U"], "column_set") != `"complete"` {
		t.Error("CREATE VIEW changes a claimed table")
	}
}
