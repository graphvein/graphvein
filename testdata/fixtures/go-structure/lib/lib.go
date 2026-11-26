package lib

import (
	"fmt" // trailing comment
	. "strings"
	_ "embed"
	str "strconv"
)

/*
#include <stdio.h>
*/
import "C"

// T is a type.
type T struct{ A int `json:"a"` }

type (
	G[K comparable, V any] struct{ m map[K]V }
	I interface{ ~int | ~string }
	Alias = T
	N int
)

type GA[T any] = []T

func (t T) Value() int { return t.A }
func (t *T) Ptr() {}
func (g *G[K, V]) Get(k K) V { return g.m[k] }
func (g G[_, _]) Len() int { return len(g.m) }
func (r (T)) Paren() {}
func (r (*T)) ParenPtr() {}

func init() {}
func init() {}
func _() {}
func _() {}

var A, B = 1, 2 // two names
var (
	C int
	d int
	_ = fmt.Sprint
)

const (
	X = iota
	Y
	z
)

func Body()

func Local() {
	type inner struct{}
	_ = inner{}
	_ = ToUpper("x")
	_ = str.Itoa(1)
}

var Ünï = 1
var ñ = 2
