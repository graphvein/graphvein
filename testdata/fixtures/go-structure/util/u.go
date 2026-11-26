package util

import (
	"example.test/ext"
	"example.test/structure/_hidden"
	"example.test/structure/badbuild"
	"example.test/structure/lib"
	"example.test/structure/missing"
	"example.test/structure/multi"
	"example.test/structure/nested"
	"example.test/structure/testdata/pkg"
	"example.test/structure/twobuild"
)

var _ = lib.T{}
var _ = nested.N
var _ = ext.Ext
var _ = pkg.P
var _ = hidden.H
var _ = multi.A
var _ = badbuild.Y
var _ = twobuild.Z
var _ = missing.M
