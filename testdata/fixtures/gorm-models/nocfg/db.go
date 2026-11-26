// Package nocfg opens GORM without a configuration: GORM's zero Config
// (Revision 1 addendum 2, P1).
package nocfg

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Item is a plain model.
type Item struct {
	ID   uint
	Name string `gorm:"not null"`
}

// Open opens the database and creates the table.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Item{})
}
