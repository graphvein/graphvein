// Package csharp extracts best-effort C# structure (T021, ADR 0067): types,
// methods, constructors, operators, local functions, properties and
// indexers with exact spans, and using directives, which never become file
// edges. Every fact comes from exactly one row of the C# claim contract in
// docs/lang/best-effort.md (rows CS-*); every other form is the default
// branch D and claims nothing.
//
// The package reads tree-sitter-c-sharp v0.23.5 trees. Calls, cross-file
// symbol resolution and project files (csproj) are out of scope, and there
// is no compiler oracle for C# (T021 open point O13 (a)).
package csharp

import (
	"github.com/graphvein/graphvein/internal/lang/besteffort"
)

// Rows is the C# claim contract, kept one to one with the "C#" table of
// docs/lang/best-effort.md by TestContractTableMatchesDocument. Every branch
// of Extract and Link names its row through trace.
var Rows = besteffort.Table{
	{ID: "CS-T1", Claim: "class"},
	{ID: "CS-T2", Claim: "struct"},
	{ID: "CS-T3", Claim: "interface"},
	{ID: "CS-T4", Claim: "record; attribute struct: true for record struct"},
	{ID: "CS-T5", Claim: "enum"},
	{ID: "CS-T6", Claim: "Declarations inside namespace_declaration bodies, after a file_scoped_namespace_declaration (a sibling node spanning only namespace X;), and inside every preproc_if/preproc_elif/preproc_else branch, without evaluating conditions"},
	{ID: "CS-F1", Claim: "method; explicit interface implementation M@IFoo"},
	{ID: "CS-F2", Claim: "constructor, name = type name; static modifier: T@static"},
	{ID: "CS-F3", Claim: "method named ~T"},
	{ID: "CS-F4", Claim: "function, owned by the enclosing callable or property; under a top-level statement (global_statement): no owner"},
	{ID: "CS-F5", Claim: "method named operator <op> / implicit operator <T> / explicit operator <T>"},
	{ID: "CS-P1", Claim: "property; explicit interface P@IFoo"},
	{ID: "CS-P2", Claim: "property named this (overloads per BE-6; explicit this@IList)"},
	{ID: "CS-G1", Claim: "nothing"},
	{ID: "CS-R1", Claim: "nothing"},
	{ID: "CS-I1", Claim: "nothing"},
	{ID: "CS-I2", Claim: "external node external:.#dotnet:<name> (alias target, global:: and whitespace removed) and an imports edge"},
	{ID: "CS-I3", Claim: "nothing"},
	{ID: "CS-I4", Claim: "unresolved_import warning naming that prefix"},
}

// tracer records which contract row a branch took; nil outside tests.
type tracer func(row string)

func (t tracer) trace(row string) {
	if t != nil {
		t(row)
	}
}
