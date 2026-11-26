// Package relignore sets IgnoreRelationshipsWhenMigrating
// (investigation relations-ignore probe): no auto-added table, no join table, no foreign
// key.
package relignore

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Company is not created.
type Company struct {
	ID   int
	Name string
}

// Employee belongs to Company.
type Employee struct {
	ID        int
	CompanyID int
	Company   Company
}

// Lang is not created.
type Lang struct {
	ID   uint
	Code string
}

// Speaker's join table is not created.
type Speaker struct {
	ID    uint
	Langs []Lang `gorm:"many2many:speaker_langs;"`
}

// Open opens the database and creates the tables.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{IgnoreRelationshipsWhenMigrating: true})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Employee{}, &Speaker{})
}
