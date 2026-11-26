// Package buildsel reads only the files the go command builds into a
// package (golang.Package, ADR 0068): every other file of this module
// declares a model, a TableName method or a migrate site that the build
// never sees.
package buildsel

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"example.com/buildsel/mixed"
	"example.com/buildsel/model"
)

// Open opens the database and creates the tables.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&model.Account{}, &model.Ledger{}, &mixed.Thing{})
}
