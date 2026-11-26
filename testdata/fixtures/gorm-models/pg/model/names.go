package model

import (
	"gorm.io/gorm/schema"

	"example.com/pg/model/tables"
)

// Renamed has a TableName literal (GO-K4).
type Renamed struct {
	ID   uint
	Name string
}

// TableName names the table.
func (Renamed) TableName() string { return "custom_renamed" }

// RenamedPtr has a TableName on the pointer.
type RenamedPtr struct{ ID uint }

// TableName names the table.
func (*RenamedPtr) TableName() string { return "custom_ptr" }

// RenamedNamer's TableName takes the namer: the table name is unknown.
type RenamedNamer struct{ ID uint }

// TableName names the table through the namer.
func (RenamedNamer) TableName(namer schema.Namer) string { return namer.TableName("Other") }

const renamedTable = "custom_const"

// RenamedConst returns a package constant (Revision 1, O7).
type RenamedConst struct{ ID uint }

// TableName names the table.
func (RenamedConst) TableName() string { return renamedTable }

// RenamedConcat returns a constant of another module package plus a
// literal.
type RenamedConcat struct{ ID uint }

// TableName names the table.
func (RenamedConcat) TableName() string { return tables.Prefix + "concat" }

var renamedVar = "custom_var"

// RenamedVar returns a variable: the table name is unknown.
type RenamedVar struct{ ID uint }

// TableName names the table.
func (RenamedVar) TableName() string { return renamedVar }

// Promoted gets its TableName from an embedded struct.
type Promoted struct {
	Renamer
	Name string
}

// Renamer is embedded by Promoted.
type Renamer struct{ ID uint }

// TableName names the table of every struct that embeds Renamer.
func (Renamer) TableName() string { return "promoted_name" }

type Person struct{ ID uint }
type Category struct{ ID uint }
type Human struct{ ID uint }
type Data struct{ ID uint }
type News struct{ ID uint }
type Equipment struct{ ID uint }
type HTTPLog struct{ ID uint }
type APIKey struct{ ID uint }
type UserOIDCID struct{ ID uint }
type Leaf struct{ ID uint }
type Mouse struct{ ID uint }
type Cactus struct{ ID uint }
type V2Item struct{ ID uint }
type Item2B struct{ ID uint }
type Sheep struct{ ID uint }
type Salesman struct{ ID uint }

// AVeryLongModelNameUsedToCheckIdentifierTruncationOnPostgresX's table
// name is longer than 63 bytes: postgres truncates it.
type AVeryLongModelNameUsedToCheckIdentifierTruncationOnPostgresX struct {
	ID uint
}

// LongIndexes has a generated index name over 63 bytes (hashed at
// postgres' limit) and an explicit one postgres truncates.
type LongIndexes struct {
	ID                                        uint
	SomeIndexedNameThatIsLongEnoughToBeHashed string `gorm:"index"`
	Other                                     string `gorm:"index:idx_an_explicit_name_that_is_longer_than_sixty_three_bytes_abcdefgh"`
}

// Holder and Wallet: Wallet.OwnerID is an int32 that a belongs-to
// relation to Holder may turn into Holder.ID's uint (relationship.go:592-
// 604): its type is withheld until relations are read.
type Holder struct {
	ID   uint
	Name string
}

type Wallet struct {
	ID      uint
	OwnerID int32
	Owner   Holder
	Label   string
}
