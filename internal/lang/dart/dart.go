// Package dart extracts best-effort Dart structure (T021, ADR 0067): types,
// functions, methods, accessors, operators and constructors with exact
// spans, and import, export and part directives resolved to a file only
// where the claim contract proves it. Every fact comes from exactly one row
// of the Dart claim contract in docs/lang/best-effort.md (rows DA-*); every
// other form is the default branch D and claims nothing.
//
// The package reads UserNobody14/tree-sitter-dart 0038280 trees. Calls,
// cross-file symbol resolution and build files are out of scope, except the
// one top-level `name:` line of each pubspec.yaml (PackageNames), without
// which a `package:` URI of the repository's own package cannot be told
// from a third-party one. There is no Dart oracle (T021 open point O13 (a)).
package dart

import (
	"github.com/graphvein/graphvein/internal/lang/besteffort"
)

// Rows is the Dart claim contract, kept one to one with the "Dart" table of
// docs/lang/best-effort.md by TestContractTableMatchesDocument. Every branch
// of Extract and Link names its row through trace.
var Rows = besteffort.Table{
	{ID: "DA-T1", Claim: "class; attribute mixin: true for mixin class"},
	{ID: "DA-T2", Claim: "mixin"},
	{ID: "DA-T3", Claim: "extension; anonymous: extension on <T>"},
	{ID: "DA-T4", Claim: "extension_type"},
	{ID: "DA-T5", Claim: "enum"},
	{ID: "DA-F1", Claim: "function"},
	{ID: "DA-F2", Claim: "method"},
	{ID: "DA-F3", Claim: "function at top level, method in a type; attribute accessor: \"get\""},
	{ID: "DA-F4", Claim: "as DA-F3, name x=, accessor: \"set\""},
	{ID: "DA-F5", Claim: "method named operator <op>"},
	{ID: "DA-F6", Claim: "function, owned by the nearest emitted owner"},
	{ID: "DA-C1", Claim: "constructor; one identifier: unnamed new; two: named <second identifier>"},
	{ID: "DA-C2", Claim: "constructor; without a name field: unnamed new; with one: named <name>"},
	{ID: "DA-C3", Claim: "constructor; without an identifier: unnamed new; with one: named <identifier>"},
	{ID: "DA-S", Claim: "Span from the first annotation of the contiguous run of annotation siblings directly before the declaration (comments between them included; at top level also a directly preceding external keyword) to the end of the following function_body; without a body, to the end of the wrapping declaration node (at top level the signature) extended over a directly following ; (spaces and tabs only between)"},
	{ID: "DA-G1", Claim: "nothing"},
	{ID: "DA-G2", Claim: "nothing"},
	{ID: "DA-R1", Claim: "nothing"},
	{ID: "DA-I1", Claim: "external external:.#dart:<lib> and imports"},
	{ID: "DA-I2", Claim: "imports to the scanned file at the URI resolved against the importing file's directory"},
	{ID: "DA-I3", Claim: "imports to <pubspec dir>/lib/<p>"},
	{ID: "DA-I4", Claim: "nothing"},
	{ID: "DA-I5", Claim: "external external:.#pub:<n> and imports"},
	{ID: "DA-I6", Claim: "Each URI (default and every alternative) handled by DA-I1-I5 and DA-I8 independently, conditions not evaluated"},
	{ID: "DA-I7", Claim: "nothing"},
	{ID: "DA-I8", Claim: "nothing"},
}

// tracer records which contract row a branch took; nil outside tests.
type tracer func(row string)

func (t tracer) trace(row string) {
	if t != nil {
		t(row)
	}
}
