package rel

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open opens the database and creates the tables in one proven order.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Employee{}, &Owner{}, &CreditCard{}, &Post{}, &PComment{}, &Speaker{}, &Speaker2{}, &Self{}, &Referrer{}, &Cat{}, &Toy{}, &NoFK{}, &Holder{}, &Article{}, &Branch{}); err != nil {
		return nil, err
	}
	db.AutoMigrate(&Before{}, &Broken{}, &After{})
	db.Migrator().CreateTable(&Ticket{})
	return db, nil
}
