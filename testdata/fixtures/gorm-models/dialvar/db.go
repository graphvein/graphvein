// Package dialvar assigns the dialector to a gorm.Dialector variable in a
// switch before one gorm.Open: a runtime group of the assigned dialects
// (T018g5 Revision 2, coverage).
package dialvar

import (
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Entry is the group's model.
type Entry struct {
	ID    uint
	Title string `gorm:"size:80;index"`
	Done  bool   `gorm:"default:false"`
}

// Open opens the database of the kind named and migrates.
func Open(kind, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch kind {
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		dialector = sqlite.Open(dsn)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Entry{})
}
