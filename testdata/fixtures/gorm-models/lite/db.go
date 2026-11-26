// Package lite opens the fixture's sqlite database through glebarez/sqlite.
package lite

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Note is a plain model with sqlite's forms.
type Note struct {
	ID        uint
	Title     string `gorm:"size:200;not null;index:idx_note_title,sort:desc"`
	Body      string `gorm:"default:'none'"`
	Pinned    bool   `gorm:"default:false"`
	Rank      float64
	Seen      *string   `gorm:"default:null"`
	Stamp     time.Time `gorm:"type:timestamp"`
	Slug      string    `gorm:"unique"`
	Score     int       `gorm:"check:score >= 0"`
	CreatedAt time.Time
}

// CompositeAI has an autoincrement field in a composite key: sqlite keeps
// only that field as the primary key.
type CompositeAI struct {
	A uint `gorm:"primaryKey;autoIncrement"`
	B uint `gorm:"primaryKey"`
	C string
}

// TwoAI has two autoincrement integers: sqlite fails the call (GO-P1).
type TwoAI struct {
	ID    uint
	Count uint `gorm:"autoIncrement"`
}

// Sibling is migrated in TwoAI's call, so it is not claimed either.
type Sibling struct {
	ID   uint
	Name string
}

// Tagged has a map without a type or serializer: GORM cannot parse it
// (GO-P1).
type Tagged struct {
	ID   uint
	Tags map[string]string
}

// BadDefault's default does not parse as an int (GO-P1).
type BadDefault struct {
	ID    uint
	Count int `gorm:"default:many"`
}

// Draft is listed only in a test file: not read (O10); it is a model not
// proven migrated (GO-M2).
type Draft struct {
	ID   uint
	Text string `gorm:"size:10"`
}

// Open opens the database and creates the tables.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Note{}, &CompositeAI{}); err != nil {
		return nil, err
	}
	db.AutoMigrate(&TwoAI{}, &Sibling{})
	db.AutoMigrate(&Tagged{})
	db.AutoMigrate(&BadDefault{})
	return db, nil
}
