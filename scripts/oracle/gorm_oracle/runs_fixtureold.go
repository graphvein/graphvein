//go:build ignore

package main

// The fixture module rev2old of testdata/fixtures/gorm-models at its pins
// (gorm 1.31.2, gorm.io/driver/postgres v1.5.4: a driver older than
// v1.5.6, whose dialector does not set the 63-byte identifier limit;
// T018g5 Revision 2, A7).

import "example.invalid/gormoracle/corpus/fixture/rev2old"

func init() {
	register(run{Name: "fixture-rev2old", Repo: ".", Group: "models/gorm/rev2old", Dialects: []string{"postgres"}, Steps: []step{
		{Site: "rev2old/db.go:29", Models: []any{&rev2old.Short{}, &rev2old.LongNames{}}},
	}})
}
