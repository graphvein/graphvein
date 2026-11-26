// Package database opens one of three databases chosen at run time, as
// gotify does (GO-V3, Revision 1 O1).
package database

import (
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Account is the runtime group's model.
type Account struct {
	ID        uint
	Email     string `gorm:"type:varchar(120);uniqueIndex"`
	Name      string
	Nick      string `gorm:"index"`
	Bio       string `gorm:"size:300"`
	Credits   int    `gorm:"default:10;not null"`
	Ratio     float64
	Active    bool   `gorm:"default:true"`
	Plan      string `gorm:"default:free"`
	Avatar    []byte
	Owner     uint
	LastSeen  *time.Time
	CreatedAt time.Time
}

// Token has a string primary key.
type Token struct {
	Value     string `gorm:"primaryKey;size:64"`
	AccountID uint   `gorm:"index"`
}

// New opens the database of the dialect named and creates the tables.
func New(dialect, dsn string) (*gorm.DB, error) {
	cfg := &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true}
	var db *gorm.DB
	var err error
	switch dialect {
	case "mysql":
		db, err = gorm.Open(mysql.New(mysql.Config{DSN: dsn, DefaultStringSize: 256}), cfg)
	case "postgres":
		db, err = gorm.Open(postgres.Open(dsn), cfg)
	default:
		db, err = gorm.Open(sqlite.Open(dsn), cfg)
	}
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(new(Account), new(Token)); err != nil {
		return nil, err
	}
	return db, nil
}
