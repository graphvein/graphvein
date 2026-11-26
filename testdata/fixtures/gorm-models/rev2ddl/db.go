// Package rev2ddl holds the T018g5 Revision 2 forms that leave facts
// unknown: Migrator DDL calls and DDL text (A3), receivers whose handle
// may be scoped (A4) and a type declared inside the function (A6).
package rev2ddl

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Account loses a column and an index after AutoMigrate (A3).
type Account struct {
	ID    uint
	Name  string `gorm:"index"`
	Old   string
	Email string
}

// Plain is not changed by a Migrator call (its sets stay complete).
type Plain struct {
	ID   uint
	Name string
}

// Scoped is migrated through a parameter one caller passes db.Table(...)
// (A4): the site is not read.
type Scoped struct {
	ID uint
}

// Returned is migrated through a handle a function returns scoped (A4).
type Returned struct {
	ID uint
}

// Repo keeps a handle another literal sets scoped (A4).
type Repo struct {
	db *gorm.DB
}

// Fielded is migrated through Repo.db (A4).
type Fielded struct {
	ID uint
}

// Box is the package's Box; Open declares its own (A6).
type Box struct {
	ID uint
}

// Open opens the database and migrates.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Account{}, &Plain{}); err != nil {
		return nil, err
	}
	db.Migrator().DropColumn(&Account{}, "Old")
	db.Migrator().DropIndex(&Account{}, "idx_accounts_name")
	db.Exec("CREATE TABLE audit (id bigint)")
	migrateScoped(db)
	migrateScoped(db.Table("scoped_archive"))
	archive(db).AutoMigrate(&Returned{})
	r := &Repo{db: db.Table("fielded_archive")}
	r.migrate()
	type Box struct {
		ID   uint
		Size int
	}
	db.AutoMigrate(&Box{})
	return db, nil
}

func migrateScoped(db *gorm.DB) {
	db.AutoMigrate(&Scoped{})
}

func archive(db *gorm.DB) *gorm.DB {
	return db.Table("returned_archive")
}

func (r *Repo) migrate() {
	r.db.AutoMigrate(&Fielded{})
}
