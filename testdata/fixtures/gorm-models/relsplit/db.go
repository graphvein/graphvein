// Package relsplit migrates children before their has-one and has-many
// parents, one call each (investigation relations-split probe).
package relsplit

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// CreditCard is created before Owner is parsed: no fk_owners_card.
type CreditCard struct {
	ID      uint
	Number  string
	OwnerID uint
}

// Owner has one CreditCard.
type Owner struct {
	ID   uint
	Card CreditCard `gorm:"foreignKey:OwnerID"`
}

// PComment is created before Post is parsed: no fk_posts_comments.
type PComment struct {
	ID     uint
	PostID uint
}

// Post has many PComments.
type Post struct {
	ID       uint
	Comments []PComment `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
}

// Holder has many Pets.
type Holder struct {
	ID   uint
	Pets []Pet `gorm:"constraint:OnDelete:CASCADE"`
}

// Pet's parse reaches Holder through its belongs-to field, so Holder's
// has-many registers before pets is created (fk_holders_pets).
type Pet struct {
	ID       uint
	HolderID uint
	Holder   Holder
}

// Open opens the database and creates the tables, one call each.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db.AutoMigrate(&CreditCard{})
	db.AutoMigrate(&PComment{})
	db.AutoMigrate(&Owner{})
	db.AutoMigrate(&Post{})
	db.AutoMigrate(&Pet{})
	db.AutoMigrate(&Holder{})
	return db, nil
}
