// Package v1 uses GORM v1, which is found but not read (GO-U1).
package v1

import "github.com/jinzhu/gorm"

// Legacy is a GORM v1 model.
type Legacy struct {
	gorm.Model
	Name string
}

// Migrate creates the table with GORM v1.
func Migrate(db *gorm.DB) {
	db.AutoMigrate(&Legacy{})
}
