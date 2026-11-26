// Package model holds the module's models.
package model

// Account is migrated by Open; its table name is GORM's default
// (accounts), not the one the ignored file below declares.
type Account struct {
	ID   uint
	Name string
}

// Ledger is migrated by Open; its table name is GORM's default (ledgers).
type Ledger struct {
	ID    uint
	Total int64
}
