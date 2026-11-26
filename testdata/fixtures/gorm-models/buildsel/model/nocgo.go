//go:build !cgo

package model

import "C"

// TableName in a cgo file that no configuration builds (it needs cgo and
// excludes it): Ledger keeps GORM's default name.
func (*Ledger) TableName() string { return "cgo_ledgers" }
