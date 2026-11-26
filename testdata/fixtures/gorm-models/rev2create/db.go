// Package rev2create holds the T018g5 Revision 2 addendum, part 2 form
// whose target is not read: a CREATE TABLE IF NOT EXISTS with a
// placeholder for the table name.
package rev2create

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Draft and Note are migrated beside a CREATE TABLE whose table is not
// read from the text: it may create either, so both keep only their table
// and column names.
type Draft struct {
	ID    uint
	Title string `gorm:"index"`
}

// Note is the second table of the group.
type Note struct {
	ID   uint
	Body string `gorm:"size:200"`
}

// Open opens the database and migrates.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Draft{}, &Note{}); err != nil {
		return nil, err
	}
	db.Exec("CREATE TABLE IF NOT EXISTS ? (id bigint)", "drafts_copy")
	return db, nil
}
