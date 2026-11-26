package fixture

import (
	"fmt"
	util "example.test/fixture/helper"
)

type User struct{ Name string }
type Saver interface{ Save() }
type Alias = User
type Count int
type List[T any] struct{ Items []T }

var Exported = 1
var private = 2
const Answer = 42

func init() { local() }
func init() { local() }

func local() { util.Help() }
func (u *User) Save() { local() }
func (l *List[T]) Len() int { return len(l.Items) }
func Run() {
	var u User
	u.Save()
	local()
	fmt.Println(Answer)
	TestOnly()
}
