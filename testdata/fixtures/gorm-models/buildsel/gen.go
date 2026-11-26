//go:build ignore

// A generator the go command never builds: its migrate site is not read.
package main

import "gorm.io/gorm"

// Ghost is migrated only by the ignored generator.
type Ghost struct{ ID uint }

func run(db *gorm.DB) error { return db.AutoMigrate(&Ghost{}) }
