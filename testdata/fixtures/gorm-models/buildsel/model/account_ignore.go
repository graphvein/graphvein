//go:build ignore

package model

// TableName is never built into the package.
func (Account) TableName() string { return "ignored_accounts" }

// Shadow is a struct only an ignored file declares.
type Shadow struct{ ID uint }
