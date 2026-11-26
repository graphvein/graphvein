package lite

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDraft(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Draft{}); err != nil {
		t.Fatal(err)
	}
}
