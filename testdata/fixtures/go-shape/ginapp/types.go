package ginapp

import "time"

// Base is embedded in User: its fields are promoted (encoding/json
// flattens them).
type Base struct {
	ID        int       `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	note      string
}

// Audit is embedded beside Base.
type Audit struct {
	By string `json:"by"`
	ID int    `json:"audit_id"`
}

// CreateUser is a request body bound by gin.
type CreateUser struct {
	Name     string            `json:"name" binding:"required"`
	Email    string            `json:"email" binding:"required"`
	Age      int               `json:"age"`
	Nickname *string           `json:"nickname,omitempty"`
	Tags     []string          `json:"tags"`
	Count    int64             `json:"count,string"`
	Secret   string            `json:"-"`
	Hidden   string            `validate:"required"`
	Meta     map[string]string `json:"meta"`
	internal string
}

// User is a response body.
type User struct {
	Base
	Audit
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Roles []Role `json:"roles"`
}

// Role is an element of User.Roles.
type Role struct {
	Name  string `json:"name"`
	Level uint8  `json:"level"`
}

// A and B both declare X and shared at the same depth: encoding/json
// drops both names in Conflict; Only is kept.
type A struct {
	X      string
	Shared string `json:"shared"`
}

// B is embedded beside A.
type B struct {
	X      string
	Shared string `json:"shared"`
	Only   string
}

// C tags X, so it wins over A's untagged X in Tagged.
type C struct {
	X string `json:"X"`
}

// Conflict embeds A and B.
type Conflict struct {
	A
	B
}

// Tagged embeds A and C.
type Tagged struct {
	A
	C
}

// Money has its own JSON form.
type Money struct{ Cents int64 }

// MarshalJSON writes Money as a decimal string.
func (m Money) MarshalJSON() ([]byte, error) { return []byte(`"0.00"`), nil }

// Price embeds Money: the promoted MarshalJSON writes the whole Price.
type Price struct {
	Money
	Currency string `json:"currency"`
}

// Page is generic.
type Page[T any] struct {
	Items []T `json:"items"`
}

// Node is recursive.
type Node struct {
	Name     string `json:"name"`
	Children []Node `json:"children"`
}

// ListQuery is bound from the query string.
type ListQuery struct {
	Page   int      `form:"page" binding:"required"`
	Size   int      `form:"size,default=20"`
	Sort   string   `form:"sort"`
	Filter []string `form:"filter"`
	Raw    string
	skip   string
}

// ItemURI is bound from the path.
type ItemURI struct {
	ID      int    `uri:"id" binding:"required"`
	Missing string `uri:"missing"`
}

// Login has the same JSON and form names.
type Login struct {
	User string `json:"user" form:"user" binding:"required"`
	Pass string `json:"pass" form:"pass" binding:"required"`
}

// Mixed has different JSON and form names.
type Mixed struct {
	A string `json:"a" form:"aa"`
}

// Types lists the fixture's types for the encoding/json oracle
// (scripts/oracle/go_shape).
var Types = map[string]any{
	"Base": Base{}, "Audit": Audit{}, "CreateUser": CreateUser{}, "User": User{}, "Role": Role{},
	"Conflict": Conflict{}, "Tagged": Tagged{}, "Price": Price{}, "Node": Node{}, "Login": Login{}, "Mixed": Mixed{},
}
