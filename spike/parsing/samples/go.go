package sample
type Item struct { Name string }
func NewItem(name string) Item { return Item{Name: name} }
func (item Item) Label() string { return item.Name }
