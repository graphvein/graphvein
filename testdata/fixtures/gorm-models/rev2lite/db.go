// Package rev2lite holds the sqlite forms of T018g5 Revision 2: raw type
// texts (A1) and a raw function default not wrapped in parentheses, which
// fails its call (A5).
package rev2lite

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// LiteRaw has raw type texts with more than a type name (A1) and a
// wrapped function default (claimed: presence only).
type LiteRaw struct {
	ID    uint
	Name  string    `gorm:"type:varchar(100) NOT NULL"`
	Seq   int64     `gorm:"type:bigserial"`
	Stamp time.Time `gorm:"default:(datetime('now'))"`
	Plain string    `gorm:"type:varchar(10)"`
}

// BareDefault's default is a function call without parentheses: sqlite
// rejects the CREATE TABLE (A5).
type BareDefault struct {
	ID    uint
	Stamp time.Time `gorm:"default:datetime('now')"`
}

// AfterBare follows BareDefault in its call: not created.
type AfterBare struct {
	ID uint
}

// Open opens the database and migrates.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&LiteRaw{}); err != nil {
		return nil, err
	}
	db.AutoMigrate(&BareDefault{}, &AfterBare{})
	return db, nil
}
