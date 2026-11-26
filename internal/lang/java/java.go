// Package java extracts best-effort Java structure (T021, ADR 0067): types,
// methods and constructors with exact spans, and import directives resolved
// to a file only where the claim contract proves it. Every fact comes from
// exactly one row of the Java claim contract in docs/lang/best-effort.md
// (rows JV-*); every other form is the default branch D and claims nothing.
//
// The package reads tree-sitter-java v0.23.5 trees. Calls, cross-file symbol
// resolution and build files are out of scope.
package java

import (
	"github.com/graphvein/graphvein/internal/lang/besteffort"
)

// Rows is the Java claim contract, kept one to one with the "Java" table of
// docs/lang/best-effort.md by TestContractTableMatchesDocument. Every branch
// of Extract and Link names its row through trace.
var Rows = besteffort.Table{
	{ID: "JV-T1", Claim: "class"},
	{ID: "JV-T2", Claim: "interface"},
	{ID: "JV-T3", Claim: "enum"},
	{ID: "JV-T4", Claim: "record"},
	{ID: "JV-T5", Claim: "annotation"},
	{ID: "JV-T6", Claim: "as its row, owned by the enclosing type or callable"},
	{ID: "JV-F1", Claim: "method"},
	{ID: "JV-F2", Claim: "constructor, name = type name"},
	{ID: "JV-F3", Claim: "method"},
	{ID: "JV-F4", Claim: "method with no owner"},
	{ID: "JV-R1", Claim: "nothing"},
	{ID: "JV-R2", Claim: "nothing"},
	{ID: "JV-I1", Claim: "cached package name (no node)"},
	{ID: "JV-I2", Claim: "imports file to the one scanned Java file declaring that fully qualified type (package + top-level type [+ member types])"},
	{ID: "JV-I3", Claim: "imports to the one file declaring type a.b.C"},
	{ID: "JV-I4", Claim: "imports to the one file declaring it"},
	{ID: "JV-I5", Claim: "ambiguous_import warning listing the files"},
	{ID: "JV-I6", Claim: "nothing"},
	{ID: "JV-I7", Claim: "unresolved_import warning"},
	{ID: "JV-I8", Claim: "external node external:.#java:<name> (static dropped, .* kept, whitespace removed) and an imports edge"},
}

// tracer records which contract row a branch took; nil outside tests.
type tracer func(row string)

func (t tracer) trace(row string) {
	if t != nil {
		t(row)
	}
}
