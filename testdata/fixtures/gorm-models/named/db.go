// Package named uses a literal naming strategy (GO-K2, GO-K4).
package named

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Person is named t_person (prefix, singular).
type Person struct {
	ID       uint
	FullName string `gorm:"uniqueIndex"`
}

// HTTPLog is named t_http_log.
type HTTPLog struct {
	ID   uint
	Path string `gorm:"index"`
}

// Open opens the database and creates the tables.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "t_", SingularTable: true}})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Person{}, &HTTPLog{})
}
