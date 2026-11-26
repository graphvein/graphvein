//go:build ignore

package main

// The fixture module testdata/fixtures/gorm-models/lite at its pin, gorm
// 1.25.12 (the scratch module is built at realworld's pins).

import "example.invalid/gormoracle/corpus/fixture/lite"

func init() {
	register(run{Name: "fixture-lite", Repo: ".", Group: "models/gorm/lite", Dialects: []string{"sqlite"}, Steps: []step{
		{Site: "lite/db.go:71", Models: []any{&lite.Note{}, &lite.CompositeAI{}}},
		{Site: "lite/db.go:74", Models: []any{&lite.TwoAI{}, &lite.Sibling{}}},
		{Site: "lite/db.go:75", Models: []any{&lite.Tagged{}}},
		{Site: "lite/db.go:76", Models: []any{&lite.BadDefault{}}},
	}})
}
