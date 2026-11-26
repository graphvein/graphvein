//go:build ignore

package main

// gotify/server at 5ce7f0ac (gorm 1.31.2): database/database.go opens
// mysql, postgres or sqlite3 at run time with foreign keys disabled and
// migrates the five models in one call. The oracle runs postgres and
// sqlite (mysql facts are mapped, GO-V5).

import (
	"gorm.io/gorm"

	"example.invalid/gormoracle/corpus/gotify/model"
)

func init() {
	register(run{Name: "gotify", Repo: "gotify", Group: "models/gorm/.", Dialects: []string{"postgres", "sqlite"},
		Config: func() gorm.Config { return gorm.Config{DisableForeignKeyConstraintWhenMigrating: true} },
		Steps: []step{{Site: "database/database.go:90", Models: []any{
			new(model.User), new(model.Application), new(model.Message), new(model.Client), new(model.PluginConf),
		}}}})
}
