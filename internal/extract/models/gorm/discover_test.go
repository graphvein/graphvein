package gorm

import (
	"reflect"
	"strings"
	"testing"
)

// gotifyLike is gotify's shape: three dialectors in one switch (runtime
// dialect), a once-defined configuration local disabling foreign keys, a
// `var db *gorm.DB` receiver and new(pkg.T) arguments naming structs in
// files without a GORM import.
var gotifyLike = map[string]string{
	"go.mod": "module example.com/app/v3\n\ngo 1.24\n\nrequire (\n\tgorm.io/driver/mysql v1.6.0\n\tgorm.io/driver/postgres v1.6.3\n\tgorm.io/driver/sqlite v1.6.0\n\tgorm.io/gorm v1.31.2\n)\n\nrequire github.com/jinzhu/inflection v1.0.0 // indirect\n",
	"database/database.go": `package database

import (
	"errors"

	"example.com/app/v3/model"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func New(dialect, connection string) (*gorm.DB, error) {
	gormConfig := &gorm.Config{
		Logger:                                   nil,
		DisableForeignKeyConstraintWhenMigrating: true,
		TranslateError:                           true,
	}
	var db *gorm.DB
	err := errors.New("unsupported dialect: " + dialect)
	switch dialect {
	case "mysql":
		db, err = gorm.Open(mysql.Open(connection), gormConfig)
	case "postgres":
		db, err = gorm.Open(postgres.Open(connection), gormConfig)
	case "sqlite3":
		db, err = gorm.Open(sqlite.Open(connection), gormConfig)
	}
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(new(model.User), new(model.Application)); err != nil {
		return nil, err
	}
	return db, nil
}
`,
	"model/user.go":        "package model\n\ntype User struct {\n\tID   uint `gorm:\"primaryKey;autoIncrement\"`\n\tName string `gorm:\"type:varchar(180);uniqueIndex:uix_users_name\"`\n\tApps []Application\n}\n",
	"model/application.go": "package model\n\nimport \"time\"\n\ntype Application struct {\n\tID     uint `gorm:\"primaryKey;autoIncrement\"`\n\tUserID uint `gorm:\"index\"`\n\tSeen   *time.Time\n}\n",
	"database/migration_test.go": `package database

import (
	"testing"

	"example.com/app/v3/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigration(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open("x.db"), &gorm.Config{})
	db.Migrator().CreateTable(new(model.User))
}
`,
}

func TestDiscoverRuntimeDialect(t *testing.T) {
	mods, diags, tags := discover(t, gotifyLike)
	m := one(t, mods)
	if m.Dir != "." || m.Path != "example.com/app/v3" || m.SetKey() != "models/gorm/." || m.Refused != "" {
		t.Fatalf("module %+v", m)
	}
	if m.Version != "v1.31.2" || m.VersionStatus != VersionVerified {
		t.Errorf("version %s %s", m.Version, m.VersionStatus)
	}
	wantPins := map[string]any{"gorm.io/gorm": "v1.31.2", "gorm.io/driver/mysql": "v1.6.0", "gorm.io/driver/postgres": "v1.6.3", "gorm.io/driver/sqlite": "v1.6.0", "github.com/jinzhu/inflection": "v1.0.0"}
	if !reflect.DeepEqual(m.Pins, wantPins) {
		t.Errorf("pins %v", m.Pins)
	}
	if len(m.Requires) != 5 || !m.Requires[4].Indirect || m.Requires[4].Line != 12 {
		t.Errorf("requires %+v", m.Requires)
	}
	if !reflect.DeepEqual(m.Dialects, []string{"mysql", "postgres", "sqlite"}) || m.Dialect != "" {
		t.Errorf("dialects %v %q", m.Dialects, m.Dialect)
	}
	if len(m.Opens) != 3 {
		t.Fatalf("%d open sites (the test file's is not read)", len(m.Opens))
	}
	c := m.Config
	if c.Form != "once-defined local" || c.Line != 14 || !c.Naming.Known || c.Naming.Naming != (Naming{}) || !c.DisableForeignKeys.Known || !c.DisableForeignKeys.Value || !c.IgnoreRelationships.Known || c.IgnoreRelationships.Value {
		t.Errorf("config %+v", c)
	}
	if !reflect.DeepEqual(c.Keys, []string{"Logger=nonliteral", "DisableForeignKeyConstraintWhenMigrating=true", "TranslateError=true"}) {
		t.Errorf("keys %v", c.Keys)
	}
	if m.MySQL["DefaultStringSize"] != (Setting{Known: true}) {
		t.Errorf("mysql %+v", m.MySQL)
	}
	if len(m.Sites) != 1 {
		t.Fatalf("sites %+v", m.Sites)
	}
	s := m.Sites[0]
	if s.Call != "AutoMigrate" || s.Receiver != "var" || !s.Proven() || s.Nesting != "if-header" || s.Position != 0 || len(s.Args) != 2 {
		t.Errorf("site %+v", s)
	}
	if got := identities(m); !reflect.DeepEqual(got, []string{"table:model/user.go#User", "table:model/application.go#Application"}) {
		t.Errorf("models %v", got)
	}
	if !m.OrderProven || len(m.Incomplete) != 0 || len(diags) != 0 {
		t.Errorf("order %v incomplete %v diags %v", m.OrderProven, m.Incomplete, diags)
	}
	for _, tag := range []string{"GO-K1", "GO-K2/mysql", "GO-K2/postgres", "GO-K2/sqlite", "GO-K2/once-defined local", "GO-K2/`DisableForeignKeyConstraintWhenMigrating`", "GO-K3/`AutoMigrate`", "GO-K3/`new(T)`", "GO-M1"} {
		if !tags.Rows[tag] && !tags.Forms[tag] {
			t.Errorf("tag %s not emitted", tag)
		}
	}
}

// realworldLike is realworld's shape: a *gorm.DB parameter, a local defined
// from a module function returning *gorm.DB, &T{} and &pkg.T{} arguments,
// and a package-function call statement the order rule expands.
var realworldLike = map[string]string{
	"go.mod": goMod("v1.25.12"),
	"hello.go": `package main

import (
	"example.com/app/articles"
	"example.com/app/common"
	"example.com/app/users"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) {
	users.AutoMigrate()
	db.AutoMigrate(&articles.ArticleModel{})
	db.AutoMigrate(&articles.TagModel{})
}

func main() {
	db := common.Init()
	Migrate(db)
}
`,
	"common/database.go": `package common

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Init() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("./data/gorm.db"), &gorm.Config{TranslateError: true})
	if err != nil {
		panic(err)
	}
	DB = db
	return DB
}

func GetDB() *gorm.DB {
	return DB
}
`,
	"users/models.go": `package users

import (
	"example.com/app/common"
	"gorm.io/gorm"
)

type UserModel struct {
	ID       uint   ` + "`gorm:\"primaryKey\"`" + `
	Username string ` + "`gorm:\"column:username\"`" + `
}

type FollowModel struct {
	gorm.Model
	Following   UserModel
	FollowingID uint
}

func AutoMigrate() {
	db := common.GetDB()

	db.AutoMigrate(&UserModel{})
	db.AutoMigrate(&FollowModel{})
}
`,
	"articles/models.go": `package articles

import (
	"example.com/app/users"
	"gorm.io/gorm"
)

type ArticleModel struct {
	gorm.Model
	Slug     string ` + "`gorm:\"uniqueIndex\"`" + `
	Tags     []TagModel ` + "`gorm:\"many2many:article_tags;\"`" + `
	Author   users.UserModel
	AuthorID uint
}

type TagModel struct {
	gorm.Model
	Tag string ` + "`gorm:\"uniqueIndex\"`" + `
}
`,
}

func TestDiscoverOrderExpansion(t *testing.T) {
	mods, diags, _ := discover(t, realworldLike)
	m := one(t, mods)
	if m.Dialect != "sqlite" || m.Config.Form != "config literal" || m.VersionStatus != VersionVerified || m.Refused != "" {
		t.Fatalf("module %+v", m)
	}
	if len(m.Sequences) != 1 || !m.OrderProven {
		t.Fatalf("sequences %d proven %v", len(m.Sequences), m.OrderProven)
	}
	want := []string{"users/models.go:22", "users/models.go:23", "hello.go:12", "hello.go:13"}
	if got := sequence(m.Sequences[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("sequence %v, want %v", got, want)
	}
	if len(m.Expansions) != 1 || m.Expansions[0].Call != "users.AutoMigrate" || m.Expansions[0].Line != 11 || m.Expansions[0].Callee != "users/models.go#AutoMigrate" {
		t.Errorf("expansions %+v", m.Expansions)
	}
	receivers := map[string]string{}
	for _, s := range m.Sites {
		receivers[s.File] = s.Receiver + "|" + s.ReceiverCall
	}
	if receivers["hello.go"] != "param|" || receivers["users/models.go"] != "module function|common.GetDB" {
		t.Errorf("receivers %v", receivers)
	}
	if got := identities(m); !reflect.DeepEqual(got, []string{"table:articles/models.go#ArticleModel", "table:articles/models.go#TagModel", "table:users/models.go#UserModel", "table:users/models.go#FollowModel"}) {
		t.Errorf("models %v (file order of the sites)", got)
	}
	if len(diags) != 0 {
		t.Errorf("diags %v", diags)
	}
}

// module builds a one-file sqlite module around body (the file imports
// gorm, the sqlite driver, gorm/schema and context) with model structs.
func module(body string, extra ...string) map[string]string {
	src := map[string]string{
		"go.mod": goMod("v1.31.2"),
		"app/db.go": `package app

import (
	"context"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var _ = schema.NamingStrategy{}
var _ context.Context

type A struct{ ID uint }
type B struct{ ID uint }
type Named int

` + body,
	}
	for i := 0; i+1 < len(extra); i += 2 {
		src[extra[i]] = extra[i+1]
	}
	return src
}

const open = "func open() *gorm.DB {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\treturn db\n}\n"

func TestDiscoverReceiversAndChains(t *testing.T) {
	cases := []struct {
		name, body string
		proof      string
		call       string
		chain      []string
		reason     string
	}{
		{"gorm.Open local", "func f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\tdb.AutoMigrate(&A{})\n}\n", "gorm.Open", "AutoMigrate", nil, ""},
		{"Migrator AutoMigrate", "func f(db *gorm.DB) {\n\tdb.Migrator().AutoMigrate(A{})\n}\n" + open, "param", "Migrator().AutoMigrate", nil, ""},
		{"Migrator CreateTable", "func f(db *gorm.DB) {\n\tdb.Migrator().CreateTable(new(A))\n}\n" + open, "param", "Migrator().CreateTable", nil, ""},
		{"allowed chain", "func f(ctx context.Context, db *gorm.DB) {\n\tdb.Debug().WithContext(ctx).Set(\"gorm:table_options\", \"ENGINE=InnoDB\").Session(&gorm.Session{NewDB: true}).AutoMigrate(&A{})\n}\n" + open, "param", "AutoMigrate", []string{"Debug", "WithContext", "Set", "Session"}, ""},
		{"package var", "var DB *gorm.DB\n\nfunc f() {\n\tDB.AutoMigrate(&A{})\n}\n" + open, "package var", "AutoMigrate", nil, ""},
		{"field", "type Store struct{ db *gorm.DB }\n\nfunc (s *Store) f() {\n\ts.db.AutoMigrate(&A{})\n}\n" + open, "field", "AutoMigrate", nil, ""},
		{"field of a parameter", "type Store struct{ db *gorm.DB }\n\nfunc f(s Store) {\n\ts.db.AutoMigrate(&A{})\n}\n" + open, "field", "AutoMigrate", nil, ""},
		{"module function", "func f() {\n\tdb := open()\n\tdb.AutoMigrate(&A{})\n}\n" + open, "module function", "AutoMigrate", nil, ""},
		{"Table chain", "func f(db *gorm.DB) {\n\tdb.Table(\"x\").AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "chain method Table is not one the contract reads"},
		{"other Set key", "func f(db *gorm.DB) {\n\tdb.Set(\"gorm:other\", 1).AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "chain method Set is not one the contract reads"},
		{"DryRun session", "func f(db *gorm.DB) {\n\tdb.Session(&gorm.Session{DryRun: true}).AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "chain method Session is not one the contract reads"},
		{"untyped local", "func f(x interface{ AutoMigrate(...any) error }) {\n\tx.AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "receiver x is not a proven *gorm.DB"},
		{"closure parameter", "func f(run func(func(*gorm.DB))) {\n\trun(func(tx *gorm.DB) { tx.AutoMigrate(&A{}) })\n}\n" + open, "", "AutoMigrate", nil, "receiver tx is not a proven *gorm.DB"},
		{"call start", "func f() {\n\topen().AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "the receiver chain does not start at a name"},
		{"CreateTable without Migrator", "func f(db *gorm.DB) {\n\tdb.CreateTable(&A{})\n}\n" + open, "", "Migrator().CreateTable", nil, "CreateTable is not called on Migrator()"},
		{"shadowed local", "func f(db *gorm.DB) {\n\t{\n\t\tdb := 3\n\t\t_ = db\n\t}\n\tdb.AutoMigrate(&A{})\n}\n" + open, "", "AutoMigrate", nil, "receiver db is not a proven *gorm.DB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mods, diags, _ := discover(t, module(c.body))
			m := one(t, mods)
			if m.Refused != "" {
				t.Fatalf("refused: %s", m.Refused)
			}
			if len(m.Sites) != 1 {
				t.Fatalf("sites %+v", m.Sites)
			}
			s := m.Sites[0]
			if s.Receiver != c.proof || s.Call != c.call || !reflect.DeepEqual(s.Chain, c.chain) || s.Reason != c.reason {
				t.Errorf("site %+v", s)
			}
			unread := messages(diags, codeUnread)
			if c.reason == "" {
				if len(m.Models) != 1 || len(unread) != 0 || len(m.Incomplete) != 0 {
					t.Errorf("models %v unread %v", identities(m), unread)
				}
				return
			}
			if len(m.Models) != 0 || len(unread) != 1 || !strings.HasSuffix(unread[0], ListMarker("models/gorm/.")) || len(m.Incomplete) != 1 {
				t.Errorf("models %v unread %v incomplete %v", identities(m), unread, m.Incomplete)
			}
		})
	}
}

func TestDiscoverArguments(t *testing.T) {
	ext := "package other\n\ntype C struct{ ID uint }\n"
	body := `func f(db *gorm.DB, xs []any, v any) {
	db.AutoMigrate(new(A), &B{}, A{}, v, xs, new(*A), &Named{}, &other.C{}, &ext.D{})
}
` + open
	src := module(body, "app/other/c.go", ext)
	src["app/db.go"] = strings.Replace(src["app/db.go"], "import (\n", "import (\n\t\"example.com/app/app/other\"\n\t\"example.com/ext\"\n", 1)
	mods, diags, tags := discover(t, src)
	m := one(t, mods)
	if len(m.Sites) != 1 {
		t.Fatalf("sites %+v", m.Sites)
	}
	var forms, reasons []string
	for _, a := range m.Sites[0].Args {
		forms = append(forms, a.Form)
		reasons = append(reasons, a.Reason)
	}
	wantForms := []string{"new(T)", "&T{...}", "T{...}", "", "", "", "", "&T{...}", ""}
	if !reflect.DeepEqual(forms, wantForms) {
		t.Errorf("forms %q", forms)
	}
	wantReasons := []string{"", "", "", "argument of kind identifier is not new(T), &T{...} or T{...}", "argument of kind identifier is not new(T), &T{...} or T{...}", "new(T) does not name a type", "type Named is not a struct declared in the module's read files", "", "model package example.com/ext is outside the module"}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Errorf("reasons %q", reasons)
	}
	if got := identities(m); !reflect.DeepEqual(got, []string{"table:app/db.go#A", "table:app/db.go#B", "table:app/other/c.go#C"}) {
		t.Errorf("models %v", got)
	}
	if n := len(messages(diags, codeUnread)); n != 5 || len(m.Incomplete) != 5 {
		t.Errorf("%d unread markers, incomplete %v", n, m.Incomplete)
	}
	for _, f := range []string{"GO-K3/`new(T)`", "GO-K3/`&T{...}`", "GO-K3/`T{...}`"} {
		if !tags.Forms[f] {
			t.Errorf("form %s not tagged", f)
		}
	}
}

func TestDiscoverConfiguration(t *testing.T) {
	cases := []struct {
		name, body string
		check      func(t *testing.T, m *Module)
	}{
		{"literal naming", "func f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: \"t_\", SingularTable: true, NoLowerCase: true, IdentifierMaxLength: 30}, IgnoreRelationshipsWhenMigrating: true})\n\tdb.AutoMigrate(&A{})\n}\n", func(t *testing.T, m *Module) {
			c := m.Config
			if !c.Naming.Known || c.Naming.Naming != (Naming{TablePrefix: "t_", SingularTable: true, NoLowerCase: true, IdentifierMaxLength: 30}) || !c.IgnoreRelationships.Value || !c.DisableForeignKeys.Known || c.DisableForeignKeys.Value {
				t.Errorf("config %+v", c)
			}
		}},
		{"pointer naming", "func f() {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{NamingStrategy: &schema.NamingStrategy{SingularTable: true}})\n}\n", func(t *testing.T, m *Module) {
			if !m.Config.Naming.Known || !m.Config.Naming.Naming.SingularTable {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"NameReplacer", "func f(r schema.Replacer) {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{NamingStrategy: schema.NamingStrategy{NameReplacer: r}})\n}\n", func(t *testing.T, m *Module) {
			if m.Config.Naming.Known || m.Config.Naming.Reason != "NamingStrategy sets NameReplacer" || !m.Config.DisableForeignKeys.Known {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"non-literal flag", "func f(off bool) {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: off})\n}\n", func(t *testing.T, m *Module) {
			if m.Config.DisableForeignKeys.Known || !m.Config.Naming.Known {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"sites disagree", "func f() {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})\n\tgorm.Open(sqlite.Open(\"y\"), &gorm.Config{})\n}\n", func(t *testing.T, m *Module) {
			if m.Config.DisableForeignKeys.Known || m.Config.DisableForeignKeys.Reason != "the gorm.Open sites disagree on DisableForeignKeyConstraintWhenMigrating" || m.Dialect != "sqlite" {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"mutation", "func f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\tdb.NamingStrategy = schema.NamingStrategy{}\n\tdb.Config.DisableForeignKeyConstraintWhenMigrating = true\n}\n", func(t *testing.T, m *Module) {
			if m.Config.Naming.Known || m.Config.DisableForeignKeys.Known || !m.Config.IgnoreRelationships.Known {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"assigned local", "func f() {\n\tcfg := &gorm.Config{}\n\tcfg = &gorm.Config{}\n\tgorm.Open(sqlite.Open(\"x\"), cfg)\n}\n", func(t *testing.T, m *Module) {
			if m.Config.Form != "" || m.Config.Naming.Known || m.Config.Naming.Reason != "the configuration cfg is assigned again" {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"no configuration", "func f() {\n\tgorm.Open(sqlite.Open(\"x\"))\n}\n", func(t *testing.T, m *Module) {
			// Revision 1 addendum 2 (P1): GORM's zero Config.
			if m.Config.Form != "no configuration argument" || !m.Config.DisableForeignKeys.Known || m.Config.DisableForeignKeys.Value || !m.Config.Naming.Known || m.Config.Naming.Naming != (Naming{}) || m.Refused != "" || m.Dialect != "sqlite" {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"value literal", "func f() {\n\tcfg := gorm.Config{}\n\tgorm.Open(sqlite.Open(\"x\"), &cfg)\n}\n", func(t *testing.T, m *Module) {
			if m.Config.Form != "" {
				t.Errorf("config %+v", m.Config)
			}
		}},
		{"DryRun", "func f() {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{DryRun: true})\n}\n", func(t *testing.T, m *Module) {
			if !strings.Contains(m.Refused, "DryRun") {
				t.Errorf("refused %q", m.Refused)
			}
		}},
		{"other dialector", "func f(d gorm.Dialector) {\n\tgorm.Open(d, &gorm.Config{})\n}\n", func(t *testing.T, m *Module) {
			if !strings.Contains(m.Refused, "the dialector is not a call of a driver package") || len(m.Dialects) != 0 {
				t.Errorf("refused %q", m.Refused)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mods, _, _ := discover(t, module(c.body))
			c.check(t, one(t, mods))
		})
	}
}

func TestDiscoverDialectors(t *testing.T) {
	src := map[string]string{
		"go.mod": goMod("v1.31.2", "\tgorm.io/driver/mysql v1.6.0\n\tgorm.io/driver/sqlserver v1.6.0\n\tgithub.com/glebarez/sqlite v1.11.0\n"),
		"db.go": `package db

import (
	glebarez "github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func f(dsn string, size uint) {
	gorm.Open(glebarez.Open(dsn), &gorm.Config{})
	gorm.Open(mysql.New(mysql.Config{DSN: dsn, DefaultStringSize: 256, DisableDatetimePrecision: true, DefaultDatetimePrecision: nil}), &gorm.Config{})
	gorm.Open(mysql.New(mysql.Config{DefaultStringSize: size}), &gorm.Config{})
}
`,
	}
	mods, diags, tags := discover(t, src)
	m := one(t, mods)
	if !reflect.DeepEqual(m.Dialects, []string{"mysql", "sqlite"}) || m.Dialect != "" || m.Refused != "" {
		t.Fatalf("module %+v", m)
	}
	if m.Opens[0].Driver != "github.com/glebarez/sqlite" || m.Opens[0].Dialect != "sqlite" {
		t.Errorf("glebarez %+v", m.Opens[0])
	}
	o := m.Opens[1].MySQL
	if o["DefaultStringSize"] != (Setting{Known: true, Set: true, Value: "256"}) || o["DisableDatetimePrecision"] != (Setting{Known: true, Set: true, Value: "true"}) || o["DefaultDatetimePrecision"].Known {
		t.Errorf("mysql site %+v", o)
	}
	if m.MySQL["DefaultStringSize"].Known || m.MySQL["DisableDatetimePrecision"].Reason != "the mysql sites disagree on DisableDatetimePrecision" {
		t.Errorf("mysql group %+v", m.MySQL)
	}
	if m.Pins["github.com/glebarez/sqlite"] != "v1.11.0" || m.Pins["gorm.io/driver/sqlserver"] != "v1.6.0" {
		t.Errorf("pins %v", m.Pins)
	}
	if len(diags) != 0 {
		t.Errorf("diags %v", diags)
	}
	if !tags.Forms["GO-K2/glebarez sqlite"] || !tags.Forms["GO-K2/`mysql.New(mysql.Config{...})`"] {
		t.Errorf("forms %v", tags.Forms)
	}

	src["db.go"] = strings.Replace(src["db.go"], "\t\"gorm.io/driver/mysql\"\n", "\t\"gorm.io/driver/mysql\"\n\t\"gorm.io/driver/sqlserver\"\n", 1)
	src["db.go"] = strings.Replace(src["db.go"], "func f(dsn string, size uint) {\n", "func f(dsn string, size uint) {\n\tgorm.Open(sqlserver.Open(dsn), &gorm.Config{})\n", 1)
	mods, diags, _ = discover(t, src)
	m = one(t, mods)
	if m.Refused != "db.go:11: dialector gorm.io/driver/sqlserver.Open is not one GORM discovery reads" {
		t.Errorf("refused %q", m.Refused)
	}
	if got := messages(diags, codeNotProven); len(got) != 1 || !strings.HasPrefix(got[0], "go.mod|go.mod: GORM module example.com/app is not read: db.go:11") {
		t.Errorf("not proven %v", got)
	}
}

func TestDiscoverVersions(t *testing.T) {
	cases := []struct {
		gomod, status, refused string
	}{
		{goMod("v1.25.12"), VersionVerified, ""},
		{goMod("v1.31.2"), VersionVerified, ""},
		{goMod("v1.32.0"), VersionAssumed, ""},
		{goMod("v1.31.3-0.20260101000000-abcdefabcdef"), VersionAssumed, ""},
		{goMod("v1.25.11"), VersionRefused, "gorm.io/gorm v1.25.11 is outside the read range (v1.25.12 or later v1)"},
		{goMod("v1.25.12-rc1"), VersionRefused, "gorm.io/gorm v1.25.12-rc1 is outside the read range (v1.25.12 or later v1)"},
		{goMod("v0.2.0"), VersionRefused, "gorm.io/gorm v0.2.0 is outside the read range (v1.25.12 or later v1)"},
		{goMod("v1.25.0") + "replace gorm.io/gorm => gorm.io/gorm v1.30.0\n", VersionVerified, ""},
		{goMod("v1.30.0") + "replace gorm.io/gorm v1.30.0 => ../gorm\n", VersionRefused, "gorm.io/gorm is replaced by another module path, so the gorm code that migrated the database is not the pinned one"},
	}
	for _, c := range cases {
		src := module("func f(db *gorm.DB) {\n\tdb.AutoMigrate(&A{})\n}\n" + open)
		src["go.mod"] = c.gomod
		mods, diags, _ := discover(t, src)
		m := one(t, mods)
		if m.VersionStatus != c.status || m.Refused != c.refused {
			t.Errorf("%q: %s %q", c.gomod, m.VersionStatus, m.Refused)
		}
		if c.refused != "" && (len(messages(diags, codeNotProven)) != 1 || len(messages(diags, codeUnread)) != 0) {
			t.Errorf("%q: diags %v", c.gomod, diags)
		}
	}
	src := module("")
	src["go.mod"] = "module example.com/app\n\ngo 1.22\n\nrequire gorm.io/gorm v1.31.2\n\nreplace github.com/jinzhu/inflection => ./inflection\n"
	m := one(t, func() []*Module { mods, _, _ := discover(t, src); return mods }())
	if m.Pins["github.com/jinzhu/inflection"] != nil || m.Pins["gorm.io/gorm"] != "v1.31.2" || len(m.Pins) != 2 {
		t.Errorf("pins %v", m.Pins)
	}
	if m.Refused != "no gorm.Open site is read, so the dialect and the configuration are unknown" {
		t.Errorf("refused %q", m.Refused)
	}
}

func TestDiscoverModules(t *testing.T) {
	src := map[string]string{
		"go.mod":                 "module example.com/root\n\ngo 1.22\n",
		"main.go":                "package main\n\nimport \"github.com/jinzhu/gorm\"\n\nvar _ gorm.DB\n",
		"svc/go.mod":             goMod("v1.31.2"),
		"svc/db.go":              "package svc\n\nimport (\n\t\"gorm.io/driver/sqlite\"\n\t\"gorm.io/gorm\"\n)\n\ntype A struct{ ID uint }\n\nfunc f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\tdb.AutoMigrate(&A{})\n}\n",
		"svc/testdata/x.go":      "package x\n\nimport \"gorm.io/gorm\"\n\nfunc g(db *gorm.DB) { db.AutoMigrate(1) }\n",
		"svc/vendor/v/v.go":      "package v\n\nimport \"gorm.io/gorm\"\n\nfunc g(db *gorm.DB) { db.AutoMigrate(1) }\n",
		"svc/x_test.go":          "package svc\n\nimport \"gorm.io/gorm\"\n\nfunc g(db *gorm.DB) { db.AutoMigrate(1) }\n",
		"svc/nested/go.mod":      "module example.com/nested\n\ngo 1.22\n",
		"svc/nested/n.go":        "package nested\n\nimport \"gorm.io/gorm\"\n\nfunc g(db *gorm.DB) { db.AutoMigrate(1) }\n",
		"svc/models/tagged.go":   "package models\n\nimport \"gorm.io/gorm\"\n\ntype Base struct {\n\tgorm.Model\n}\n\ntype Tagged struct {\n\tName string `gorm:\"size:10\"`\n}\n\ntype Plain struct{ Name string }\n",
		"svc/models/embedder.go": "package models\n\ntype Used struct {\n\tBase\n\tCode string `gorm:\"index\"`\n}\n",
		"svc/use.go":             "package svc\n\nimport (\n\t\"example.com/app/models\"\n\t\"gorm.io/gorm\"\n)\n\nfunc h(db *gorm.DB) {\n\tdb.AutoMigrate(&models.Used{})\n}\n",
	}
	mods, diags, tags := discover(t, src)
	m := one(t, mods)
	if m.Dir != "svc" || m.SetKey() != "models/gorm/svc" || m.Refused != "" {
		t.Fatalf("module %+v", m)
	}
	if !reflect.DeepEqual(m.Files, []string{"svc/db.go", "svc/models/embedder.go", "svc/models/tagged.go", "svc/use.go"}) {
		t.Errorf("files %v", m.Files)
	}
	if got := identities(m); !reflect.DeepEqual(got, []string{"table:svc/db.go#A", "table:svc/models/embedder.go#Used"}) {
		t.Errorf("models %v", got)
	}
	if len(m.Sequences) != 2 || m.OrderProven {
		t.Errorf("two roots: %d sequences, proven %v", len(m.Sequences), m.OrderProven)
	}
	want := []string{"svc/models/tagged.go|svc/models/tagged.go:9: struct Tagged has gorm tags or embeds gorm.Model, but no read migrate call lists it (model, not proven migrated)"}
	if got := messages(diags, codeNotProven); !reflect.DeepEqual(got, want) {
		t.Errorf("not proven %v", got)
	}
	if got := messages(diags, codeUnread); !reflect.DeepEqual(got, []string{"main.go|main.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read"}) {
		t.Errorf("unread %v", got)
	}
	if !tags.Rows["GO-M2"] || !tags.Rows["GO-U1"] {
		t.Errorf("tags %v", tags.Rows)
	}
}

func TestDiscoverUngatedAndOrder(t *testing.T) {
	src := module(`func f(db *gorm.DB, ok bool) {
	db.AutoMigrate(&A{})
	if ok {
		db.AutoMigrate(&B{})
	}
	helper.AutoMigrate(db)
	g(db)
}

func g(db *gorm.DB) {
	db.AutoMigrate(&B{})
}
`+open,
		"app/store/store.go", "package store\n\ntype Store struct{ db interface{ AutoMigrate(...any) error } }\n\nfunc (s *Store) Migrate() {\n\ts.db.AutoMigrate(1)\n}\n\nfunc Run(x *Store) { x.Migrate() }\n",
		"app/inflect.go", "package app\n\nimport \"github.com/jinzhu/inflection\"\n\nfunc init() {\n\tinflection.AddIrregular(\"person\", \"people\")\n}\n",
	)
	src["app/db.go"] = strings.Replace(src["app/db.go"], "import (\n", "import (\n\t\"example.com/helper\"\n", 1)
	mods, diags, _ := discover(t, src)
	m := one(t, mods)
	if len(m.Sequences) != 1 {
		t.Fatalf("sequences %d", len(m.Sequences))
	}
	if got := sequence(m.Sequences[0]); !reflect.DeepEqual(got, []string{"app/db.go:20", "app/db.go:29"}) {
		t.Errorf("sequence %v", got)
	}
	if m.OrderProven {
		t.Error("a conditional site leaves the order unproven")
	}
	var conditional *MigrateSite
	for _, s := range m.Sites {
		if s.Line == 22 {
			conditional = s
		}
	}
	if conditional == nil || conditional.Position != -1 || conditional.Nesting != "conditional" || !conditional.Proven() {
		t.Errorf("conditional site %+v", conditional)
	}
	want := []string{
		"app/db.go|app/db.go:24: call of example.com/helper.AutoMigrate, a package outside the module, is not read" + ListMarker("models/gorm/."),
		"app/store/store.go|app/store/store.go:6: AutoMigrate call in a file without a GORM import: its receiver is not proven to be a *gorm.DB" + ListMarker("models/gorm/."),
	}
	if got := messages(diags, codeUnread); !reflect.DeepEqual(got, want) {
		t.Errorf("unread %q", got)
	}
	if !reflect.DeepEqual(m.InflectionChanged, []string{"app/inflect.go:6"}) {
		t.Errorf("inflection %v", m.InflectionChanged)
	}
}

// TestDiscoverNestedSites: a gorm.Open in a package-level initialiser or
// inside another captured site's arguments still counts for the dialect,
// and a migrate call at package level is read but never ordered.
func TestDiscoverNestedSites(t *testing.T) {
	src := module(`var testDB, _ = gorm.Open(sqlite.Open("test.db"), &gorm.Config{})

var _ = testDB.AutoMigrate(&B{})

type wrapper struct{}

func (wrapper) New(db *gorm.DB, err error) *gorm.DB { return db }

func f() {
	w := wrapper{}
	db := w.New(gorm.Open(postgres.Open("x"), &gorm.Config{}))
	_ = db
}
`)
	src["app/db.go"] = strings.Replace(src["app/db.go"], "import (\n", "import (\n\t\"gorm.io/driver/postgres\"\n", 1)
	mods, diags, _ := discover(t, src)
	m := one(t, mods)
	if !reflect.DeepEqual(m.Dialects, []string{"postgres", "sqlite"}) || len(m.Opens) != 2 {
		t.Errorf("dialects %v, %d open sites", m.Dialects, len(m.Opens))
	}
	if len(m.Sites) != 1 || m.Sites[0].Receiver != "" || m.OrderProven {
		t.Fatalf("sites %+v, order %v", m.Sites, m.OrderProven)
	}
	// testDB is a package var without a declared type: not proven.
	if got := messages(diags, codeUnread); len(got) != 1 || !strings.Contains(got[0], "receiver testDB is not a proven *gorm.DB") {
		t.Errorf("unread %v", got)
	}
}

// TestGormModuleUnderTestdata checks that a module the go command does not
// build (under testdata, vendor or a . or _ directory, GO-V7) is refused
// with one marker on its go.mod and nothing read.
func TestGormModuleUnderTestdata(t *testing.T) {
	for _, dir := range []string{"testdata/fix", "vendor/x", ".hidden", "_old/m"} {
		src := map[string]string{}
		for k, v := range module("func f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\tdb.AutoMigrate(&A{})\n}\n\ntype A struct{ ID uint }\n") {
			src[dir+"/"+k] = v
		}
		mods, diags, _ := discover(t, src)
		if len(mods) != 1 || !strings.Contains(mods[0].Refused, "does not build") || len(mods[0].Files) != 0 || len(diags) != 1 || diags[0].File != dir+"/go.mod" {
			t.Errorf("%s: %d modules, %v", dir, len(mods), diags)
		}
	}
}

// TestDiscoverPackageBinding: the package name an import binds is the one
// the go command reads (golang.ImportPackage). A
// `//go:build ignore` generator with `package main` that sorts first in
// the model directory once named the binding `main`, so models.M was not
// found; a refused directory (two packages) binds nothing.
func TestDiscoverPackageBinding(t *testing.T) {
	src := map[string]string{
		"go.mod":          goMod("v1.31.2"),
		"models/a_gen.go": "//go:build ignore\n\npackage main\n\ntype M struct{ Gen int }\n\nfunc main() {}\n",
		"models/m.go":     "package models\n\ntype M struct {\n\tID uint `gorm:\"primaryKey\"`\n}\n",
		"broken/a.go":     "package broken\n\ntype B struct{ ID uint }\n",
		"broken/b.go":     "package other\n\ntype B struct{ ID uint }\n",
		"app/db.go":       "package app\n\nimport (\n\t\"example.com/app/broken\"\n\t\"example.com/app/models\"\n\t\"gorm.io/driver/sqlite\"\n\t\"gorm.io/gorm\"\n)\n\nfunc f() {\n\tdb, _ := gorm.Open(sqlite.Open(\"x\"), &gorm.Config{})\n\tdb.AutoMigrate(&models.M{}, &broken.B{})\n}\n",
	}
	mods, _, _ := discover(t, src)
	m := one(t, mods)
	if got := identities(m); !reflect.DeepEqual(got, []string{"table:models/m.go#M"}) {
		t.Errorf("models %v, want only models/m.go#M (never the ignored generator's M, nothing from the refused directory)", got)
	}
}
