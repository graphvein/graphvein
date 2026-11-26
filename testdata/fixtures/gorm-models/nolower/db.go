// Package nolower keeps names as written and limits identifiers to 30
// characters (GO-K4).
package nolower

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// NoLower is named NoLowers; its index name is hashed at 30 characters.
type NoLower struct {
	ID       uint
	UserName string `gorm:"index"`
	HTTPCode int
}

// Open opens the database and creates the table.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NamingStrategy: &schema.NamingStrategy{NoLowerCase: true, IdentifierMaxLength: 30}})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&NoLower{})
}
