//go:build ignore

package main

// The fixture modules of testdata/fixtures/gorm-models at gorm 1.31.2
// (their go.mod pins). Each run is the module's gorm.Open configuration
// and its migrate calls in source order (file:line in Site). Not run:
// dynamic/ (nothing claimed), v1/ (GORM v1, not read), lite/ (gorm
// 1.25.12: runs_fixture125.go).

import (
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	bsmodel "example.invalid/gormoracle/corpus/fixture/buildsel/model"
	"example.invalid/gormoracle/corpus/fixture/ctlite"
	"example.invalid/gormoracle/corpus/fixture/dialvar"
	"example.invalid/gormoracle/corpus/fixture/named"
	"example.invalid/gormoracle/corpus/fixture/nocfg"
	"example.invalid/gormoracle/corpus/fixture/nolower"
	"example.invalid/gormoracle/corpus/fixture/pg/model"
	"example.invalid/gormoracle/corpus/fixture/rel"
	"example.invalid/gormoracle/corpus/fixture/relignore"
	"example.invalid/gormoracle/corpus/fixture/relorder"
	"example.invalid/gormoracle/corpus/fixture/relsplit"
	"example.invalid/gormoracle/corpus/fixture/rev2"
	"example.invalid/gormoracle/corpus/fixture/rev2alter"
	"example.invalid/gormoracle/corpus/fixture/rev2create"
	"example.invalid/gormoracle/corpus/fixture/rev2ddl"
	"example.invalid/gormoracle/corpus/fixture/rev2lite"
	database "example.invalid/gormoracle/corpus/fixture/runtime"
)

func init() {
	sqlite, pg := []string{"sqlite"}, []string{"postgres"}
	// buildsel's model package holds an ignored file with a TableName and
	// a cgo file excluding cgo with another (copytypes keeps their
	// constraints, so the go command leaves both out). Its migrate call
	// also names mixed.Thing, a directory with two packages the go
	// command cannot build; Graphvein does not read that argument, and the
	// run leaves it out.
	register(run{Name: "fixture-buildsel", Repo: ".", Group: "models/gorm/buildsel", Dialects: sqlite, Steps: []step{
		{Site: "buildsel/db.go:21", Models: []any{&bsmodel.Account{}, &bsmodel.Ledger{}}},
	}})
	register(run{Name: "fixture-ctlite", Repo: ".", Group: "models/gorm/ctlite", Dialects: sqlite, Steps: []step{
		{Site: "ctlite/db.go:54", CreateTable: true, Models: []any{&ctlite.Employee{}, &ctlite.Speaker{}, &ctlite.Post{}, &ctlite.PComment{}}},
	}})
	register(run{Name: "fixture-named", Repo: ".", Group: "models/gorm/named", Dialects: sqlite,
		Config: func() gorm.Config {
			return gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "t_", SingularTable: true}}
		},
		Steps: []step{{Site: "named/db.go:28", Models: []any{&named.Person{}, &named.HTTPLog{}}}}})
	// nocfg opens gorm.io/driver/sqlite (cgo); glebarez renders the same
	// DDL.
	register(run{Name: "fixture-nocfg", Repo: ".", Group: "models/gorm/nocfg", Dialects: sqlite, Steps: []step{
		{Site: "nocfg/db.go:22", Models: []any{&nocfg.Item{}}},
	}})
	register(run{Name: "fixture-nolower", Repo: ".", Group: "models/gorm/nolower", Dialects: pg,
		Config: func() gorm.Config {
			return gorm.Config{NamingStrategy: &schema.NamingStrategy{NoLowerCase: true, IdentifierMaxLength: 30}}
		},
		Steps: []step{{Site: "nolower/db.go:24", Models: []any{&nolower.NoLower{}}}}})
	register(run{Name: "fixture-pg", Repo: ".", Group: "models/gorm/pg", Dialects: pg,
		// CustomProbe.Blob's GormDataType is "blob", a type Postgres does
		// not have: the record claims the table under GO-V2
		// (the application started, open point O11), so the harness gives
		// the database the type the application needs. The column itself
		// is not claimed.
		Setup: map[string][]string{"postgres": {"CREATE DOMAIN blob AS bytea"}},
		Steps: []step{
			{Site: "pg/db.go:16", Models: []any{
				new(model.TypeProbe), new(model.IndexProbe), new(model.CompositePK), new(model.StringPK),
				new(model.NoIDTable), new(model.IDNotFirst), new(model.ExplicitPKNotID), new(model.ColumnID),
				new(model.StringID), new(model.PKNoAI), new(model.CompositeIntPK), new(model.Embeds),
				new(model.WithModel), new(model.EmbedTagged), new(model.PGOnly),
			}},
			{Site: "pg/db.go:24", Models: []any{&model.CustomProbe{}}},
			{Site: "pg/db.go:27", Models: []any{
				&model.Renamed{}, &model.RenamedPtr{}, &model.RenamedNamer{}, &model.RenamedConst{},
				&model.RenamedConcat{}, &model.RenamedVar{}, &model.Promoted{},
			}},
			{Site: "pg/db.go:33", Models: []any{
				model.Person{}, model.Category{}, model.Human{}, model.Data{}, model.News{}, model.Equipment{},
				model.HTTPLog{}, model.APIKey{}, model.UserOIDCID{}, model.Leaf{}, model.Mouse{}, model.Cactus{},
				model.V2Item{}, model.Item2B{}, model.Sheep{}, model.Salesman{},
			}},
			{Site: "pg/db.go:38", Models: []any{&model.AVeryLongModelNameUsedToCheckIdentifierTruncationOnPostgresX{}, &model.LongIndexes{}}},
			{Site: "pg/db.go:39", Models: []any{&model.Wallet{}, &model.Holder{}}},
		}})
	register(run{Name: "fixture-rel", Repo: ".", Group: "models/gorm/rel", Dialects: pg, Steps: []step{
		{Site: "rel/db.go:14", Models: []any{&rel.Employee{}, &rel.Owner{}, &rel.CreditCard{}, &rel.Post{}, &rel.PComment{}, &rel.Speaker{}, &rel.Speaker2{}, &rel.Self{}, &rel.Referrer{}, &rel.Cat{}, &rel.Toy{}, &rel.NoFK{}, &rel.Holder{}, &rel.Article{}, &rel.Branch{}}},
		{Site: "rel/db.go:17", Models: []any{&rel.Before{}, &rel.Broken{}, &rel.After{}}},
		{Site: "rel/db.go:18", CreateTable: true, Models: []any{&rel.Ticket{}}},
	}})
	register(run{Name: "fixture-relignore", Repo: ".", Group: "models/gorm/relignore", Dialects: sqlite,
		Config: func() gorm.Config { return gorm.Config{IgnoreRelationshipsWhenMigrating: true} },
		Steps:  []step{{Site: "relignore/db.go:42", Models: []any{&relignore.Employee{}, &relignore.Speaker{}}}}})
	// relorder's first call is conditional (order not proven); the run
	// takes the source order with the condition true.
	register(run{Name: "fixture-relorder", Repo: ".", Group: "models/gorm/relorder", Dialects: sqlite, Steps: []step{
		{Site: "relorder/db.go:29", Models: []any{&relorder.CreditCard{}}},
		{Site: "relorder/db.go:31", Models: []any{&relorder.Owner{}}},
	}})
	register(run{Name: "fixture-relsplit", Repo: ".", Group: "models/gorm/relsplit", Dialects: pg, Steps: []step{
		{Site: "relsplit/db.go:55", Models: []any{&relsplit.CreditCard{}}},
		{Site: "relsplit/db.go:56", Models: []any{&relsplit.PComment{}}},
		{Site: "relsplit/db.go:57", Models: []any{&relsplit.Owner{}}},
		{Site: "relsplit/db.go:58", Models: []any{&relsplit.Post{}}},
		{Site: "relsplit/db.go:59", Models: []any{&relsplit.Pet{}}},
		{Site: "relsplit/db.go:60", Models: []any{&relsplit.Holder{}}},
	}})
	// runtime chooses mysql, postgres or sqlite at run time: the oracle
	// runs postgres and sqlite (mysql facts are mapped, GO-V5).
	register(run{Name: "fixture-runtime", Repo: ".", Group: "models/gorm/runtime", Dialects: []string{"postgres", "sqlite"},
		Config: func() gorm.Config { return gorm.Config{DisableForeignKeyConstraintWhenMigrating: true} },
		Steps:  []step{{Site: "runtime/database.go:53", Models: []any{new(database.Account), new(database.Token)}}}})
	// T018g5 Revision 2 (independent review): the raw types of GO-T2's
	// list (B1), raw type texts (A1), a postgres index class and a bare
	// sqlite function default that fail their calls (A5), SetupJoinTable
	// (A2), Migrator DDL and DDL text (A3), handles through returns,
	// parameters and fields (A4), a local type (A6) and a dialector local.
	register(run{Name: "fixture-rev2", Repo: ".", Group: "models/gorm/rev2", Dialects: pg, Steps: []step{
		{Site: "rev2/db.go:86", Models: []any{&rev2.PGRaw{}, &rev2.RawText{}}},
		{Site: "rev2/db.go:89", Models: []any{&rev2.FullText{}, &rev2.AfterFullText{}}},
		{Site: "rev2/db.go:90", Call: "SetupJoinTable", Do: func(db *gorm.DB) error {
			return db.SetupJoinTable(&rev2.Person{}, "Addresses", &rev2.PersonAddress{})
		}},
		{Site: "rev2/db.go:93", Models: []any{&rev2.Person{}, &rev2.Address{}}},
		{Site: "rev2/db.go:94, 108 (migrate(debug(db)))", Call: "Debug().AutoMigrate", Do: func(db *gorm.DB) error {
			return db.Debug().AutoMigrate(&rev2.Viaparam{})
		}},
	}})
	register(run{Name: "fixture-rev2ddl", Repo: ".", Group: "models/gorm/rev2ddl", Dialects: pg, Steps: []step{
		{Site: "rev2ddl/db.go:57", Models: []any{&rev2ddl.Account{}, &rev2ddl.Plain{}}},
		{Site: "rev2ddl/db.go:60", Call: "Migrator().DropColumn", Do: func(db *gorm.DB) error {
			return db.Migrator().DropColumn(&rev2ddl.Account{}, "Old")
		}},
		{Site: "rev2ddl/db.go:61", Call: "Migrator().DropIndex", Do: func(db *gorm.DB) error {
			return db.Migrator().DropIndex(&rev2ddl.Account{}, "idx_accounts_name")
		}},
		{Site: "rev2ddl/db.go:62", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec("CREATE TABLE audit (id bigint)").Error
		}},
		{Site: "rev2ddl/db.go:63, 77 (migrateScoped(db))", Models: []any{&rev2ddl.Scoped{}}},
		{Site: "rev2ddl/db.go:64, 77 (migrateScoped(db.Table(...)))", Call: "Table().AutoMigrate", Do: func(db *gorm.DB) error {
			return db.Table("scoped_archive").AutoMigrate(&rev2ddl.Scoped{})
		}},
		{Site: "rev2ddl/db.go:65", Call: "Table().AutoMigrate", Do: func(db *gorm.DB) error {
			return db.Table("returned_archive").AutoMigrate(&rev2ddl.Returned{})
		}},
		{Site: "rev2ddl/db.go:67, 85", Call: "Table().AutoMigrate", Do: func(db *gorm.DB) error {
			return db.Table("fielded_archive").AutoMigrate(&rev2ddl.Fielded{})
		}},
		{Site: "rev2ddl/db.go:72 (a local type Box)", Call: "AutoMigrate", Do: func(db *gorm.DB) error {
			type Box struct {
				ID   uint
				Size int
			}
			return db.AutoMigrate(&Box{})
		}},
	}})
	// rev2alter and rev2create (Revision 2 addendum): the migrate call, then
	// every Exec of the source as written (db.Raw without a finisher runs
	// nothing, so it is not a step).
	register(run{Name: "fixture-rev2alter", Repo: ".", Group: "models/gorm/rev2alter", Dialects: pg, Steps: []step{
		{Site: "rev2alter/db.go:46", Models: []any{&rev2alter.Ledger{}, &rev2alter.Tagged{}, &rev2alter.Audit{}, &rev2alter.Kept{}}},
		{Site: "rev2alter/db.go:49", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec("ALTER TABLE ledgers DROP COLUMN legacy").Error
		}},
		{Site: "rev2alter/db.go:50", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec(`CREATE UNIQUE INDEX idx_taggeds_name ON "taggeds" (name)`).Error
		}},
		{Site: "rev2alter/db.go:51", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec("CREATE TABLE IF NOT EXISTS public.audits (id bigserial PRIMARY KEY, note text)").Error
		}},
		{Site: "rev2alter/db.go:52", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec("CREATE VIEW kept_names AS SELECT name FROM kepts").Error
		}},
	}})
	register(run{Name: "fixture-rev2create", Repo: ".", Group: "models/gorm/rev2create", Dialects: pg, Steps: []step{
		{Site: "rev2create/db.go:31", Models: []any{&rev2create.Draft{}, &rev2create.Note{}}},
		{Site: "rev2create/db.go:34", Call: "Exec", Do: func(db *gorm.DB) error {
			return db.Exec("CREATE TABLE IF NOT EXISTS ? (id bigint)", "drafts_copy").Error
		}},
	}})
	register(run{Name: "fixture-rev2lite", Repo: ".", Group: "models/gorm/rev2lite", Dialects: sqlite, Steps: []step{
		{Site: "rev2lite/db.go:41", Models: []any{&rev2lite.LiteRaw{}}},
		{Site: "rev2lite/db.go:44", Models: []any{&rev2lite.BareDefault{}, &rev2lite.AfterBare{}}},
	}})
	register(run{Name: "fixture-dialvar", Repo: ".", Group: "models/gorm/dialvar", Dialects: []string{"postgres", "sqlite"}, Steps: []step{
		{Site: "dialvar/db.go:32", Models: []any{&dialvar.Entry{}}},
	}})
}
