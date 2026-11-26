// Package dynamic builds its naming strategy at run time: every derived
// name is unknown (GO-K2, GO-K4).
package dynamic

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Thing's table name depends on the prefix.
type Thing struct {
	ID uint
}

// Open opens the database and creates the table.
func Open(path, prefix string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Thing{})
}
