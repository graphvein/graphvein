// Package rev2old pins a postgres driver older than v1.5.6, whose
// dialector does not set the 63-byte identifier limit, and outside the
// verified driver versions (T018g5 Revision 2, A7).
package rev2old

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Short has names under 63 bytes: claimed.
type Short struct {
	ID   uint
	Name string `gorm:"index;unique"`
}

// LongNames has a generated index name over 63 bytes: unknown.
type LongNames struct {
	ID                                             uint
	AVeryLongFieldNameThatMakesTheIndexNameLongerX string `gorm:"index"`
}

// Open opens the database and migrates.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&Short{}, &LongNames{})
}
