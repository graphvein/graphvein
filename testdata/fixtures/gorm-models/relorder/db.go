// Package relorder migrates a has-one child and its parent in calls whose
// order is not proven (open point O6): the child's foreign key is unknown.
package relorder

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// CreditCard is the child of Owner's has-one.
type CreditCard struct {
	ID      uint
	OwnerID uint
}

// Owner has one CreditCard.
type Owner struct {
	ID   uint
	Card CreditCard
}

// Open opens the database; the child's call is conditional.
func Open(path string, cards bool) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if cards {
		db.AutoMigrate(&CreditCard{})
	}
	db.AutoMigrate(&Owner{})
	return db, nil
}
