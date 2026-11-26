// Package rev2 holds the postgres forms of T018g5 Revision 2: the raw types
// of GO-T2's closed list (B1), raw type texts with more than a type name
// (A1), a postgres index class (A5), SetupJoinTable (A2) and handles
// proven through a function's returns and its callers (A4).
package rev2

import (
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PGRaw uses every raw type of GO-T2's closed postgres list the other
// modules do not (B1).
type PGRaw struct {
	ID    uint
	At    time.Time `gorm:"type:timestamptz"`
	Doc   string    `gorm:"type:json"`
	Day   time.Time `gorm:"type:date"`
	Flag  bool      `gorm:"type:boolean"`
	Big   int64     `gorm:"type:bigint"`
	Mid   int32     `gorm:"type:integer"`
	Small int16     `gorm:"type:smallint"`
	Bin   []byte    `gorm:"type:bytea"`
	Upper string    `gorm:"type:VARCHAR(12)"`
}

// RawText has raw type texts that are more than one type name (A1).
type RawText struct {
	ID   uint
	Name string `gorm:"type:varchar(100) NOT NULL"`
	Bio  string `gorm:"type:text NOT NULL DEFAULT ''"`
	Code string `gorm:"type:varchar(20) UNIQUE"`
	Seq  int64  `gorm:"type:bigserial"`
	Note string `gorm:"type:varchar (30)"`
}

// FullText has an index class postgres does not accept: its call fails at
// this model (A5).
type FullText struct {
	ID    uint
	Title string `gorm:"index:,class:FULLTEXT"`
}

// AfterFullText follows FullText in its call: not created.
type AfterFullText struct {
	ID uint
}

// Person's join table to Address is built from PersonAddress
// (SetupJoinTable, A2).
type Person struct {
	ID        int
	Name      string
	Addresses []Address `gorm:"many2many:person_addresses;"`
}

// Address is Person's many2many target.
type Address struct {
	ID   uint
	Name string
}

// PersonAddress is the custom join model.
type PersonAddress struct {
	PersonID  int `gorm:"primaryKey"`
	AddressID int `gorm:"primaryKey"`
	CreatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// Viaparam is migrated through a parameter every caller passes a proven
// handle (A4).
type Viaparam struct {
	ID   uint
	Name string
}

// Open opens the database and migrates.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&PGRaw{}, &RawText{}); err != nil {
		return nil, err
	}
	db.AutoMigrate(&FullText{}, &AfterFullText{})
	if err := db.SetupJoinTable(&Person{}, "Addresses", &PersonAddress{}); err != nil {
		return nil, err
	}
	db.AutoMigrate(&Person{}, &Address{})
	migrate(debug(db))
	return db, nil
}

// debug returns a proven handle on every return.
func debug(db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.Debug()
}

// migrate is called only with a proven handle.
func migrate(db *gorm.DB) {
	db.AutoMigrate(&Viaparam{})
}
