// Package ctlite creates tables with Migrator().CreateTable on sqlite
// (investigation createtable probe): no table is added, and sqlite accepts a
// foreign key to a table that does not exist.
package ctlite

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

// Post has many PComments.
type Post struct {
	ID       uint
	Comments []PComment `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
}

// PComment is created after Post in the same call.
type PComment struct {
	ID     uint
	PostID uint
}

// Open opens the database and creates the tables.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, db.Migrator().CreateTable(&Employee{}, &Speaker{}, &Post{}, &PComment{})
}
