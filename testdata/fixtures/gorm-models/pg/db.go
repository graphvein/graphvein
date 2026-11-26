// Package db opens the fixture's postgres database and migrates its models.
package db

import (
	"example.com/pg/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open opens the database and creates the tables.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		new(model.TypeProbe), new(model.IndexProbe), new(model.CompositePK), new(model.StringPK),
		new(model.NoIDTable), new(model.IDNotFirst), new(model.ExplicitPKNotID), new(model.ColumnID),
		new(model.StringID), new(model.PKNoAI), new(model.CompositeIntPK), new(model.Embeds),
		new(model.WithModel), new(model.EmbedTagged), new(model.PGOnly),
	); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&model.CustomProbe{}); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&model.Renamed{}, &model.RenamedPtr{}, &model.RenamedNamer{}, &model.RenamedConst{},
		&model.RenamedConcat{}, &model.RenamedVar{}, &model.Promoted{},
	); err != nil {
		return nil, err
	}
	db.AutoMigrate(
		model.Person{}, model.Category{}, model.Human{}, model.Data{}, model.News{}, model.Equipment{},
		model.HTTPLog{}, model.APIKey{}, model.UserOIDCID{}, model.Leaf{}, model.Mouse{}, model.Cactus{},
		model.V2Item{}, model.Item2B{}, model.Sheep{}, model.Salesman{},
	)
	db.AutoMigrate(&model.AVeryLongModelNameUsedToCheckIdentifierTruncationOnPostgresX{}, &model.LongIndexes{})
	db.Migrator().AutoMigrate(&model.Wallet{}, &model.Holder{})
	return db, nil
}
