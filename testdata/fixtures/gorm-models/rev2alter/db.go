// Package rev2alter holds the T018g5 Revision 2 addendum forms: Exec and
// Raw DDL text that alters or drops a claimed table, a CREATE INDEX naming
// one, a CREATE TABLE IF NOT EXISTS of one (part 2) and a CREATE VIEW.
package rev2alter

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Ledger loses a column through DDL text: it keeps only its table and
// column names.
type Ledger struct {
	ID     uint
	Name   string `gorm:"uniqueIndex"`
	Legacy string
}

// Tagged gains an index through DDL text: its index lists are unknown.
type Tagged struct {
	ID   uint
	Name string
}

// Audit is created by DDL text as well as by AutoMigrate: the text decides
// its columns, types and indexes, so it keeps only its table and column
// names.
type Audit struct {
	ID   uint
	Note string `gorm:"size:40;index"`
}

// Kept is not named by the DDL text: its columns and index lists stay
// claimed (its foreign keys are unknown beside any DDL text).
type Kept struct {
	ID   uint
	Name string `gorm:"index"`
}

// Open opens the database and migrates.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Ledger{}, &Tagged{}, &Audit{}, &Kept{}); err != nil {
		return nil, err
	}
	db.Exec("ALTER TABLE ledgers DROP COLUMN legacy")
	db.Exec(`CREATE UNIQUE INDEX idx_taggeds_name ON "taggeds" (name)`)
	db.Exec("CREATE TABLE IF NOT EXISTS public.audits (id bigserial PRIMARY KEY, note text)")
	db.Exec("CREATE VIEW kept_names AS SELECT name FROM kepts")
	db.Raw("SELECT count(*) FROM kept")
	return db, nil
}
