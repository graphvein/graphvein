package facts

import "github.com/graphvein/graphvein/internal/contract"

// RustFacts retains syntax only; crate configuration and bindings are relinked.
type RustFacts struct {
	Scopes    []RustScope
	Functions []RustFunction
	// Queries holds the sqlx query macro invocations and query function
	// calls of a file that uses sqlx (ADR 0060); absent for every other
	// file, so their cached facts do not change.
	Queries []RustQuery `json:",omitempty"`
	// Unread counts the forms of the file that the Rust structural claim
	// contract (docs/lang/rust-structure.md) sends to the default branch at
	// extraction time, by form; the linker adds its own and reports them.
	Unread map[string]int `json:",omitempty"`
	// LoneCR and Rejected mark a file that claims nothing beyond its file
	// node (rows RS-F3 and RS-F2): its raw bytes hold a CR not followed by
	// LF, or tree-sitter reported a syntax error.
	LoneCR   bool `json:",omitempty"`
	Rejected bool `json:",omitempty"`
	// ExternAliases are the names `extern crate x as y;` items of the file
	// bind (y); import resolution claims nothing through them.
	ExternAliases []string `json:",omitempty"`
	// Shape is the Axum route shape payload (rust_shape.go); absent for
	// files outside its gate or without anything it records.
	Shape *RustShape `json:",omitempty"`
}

// RustQuery is one sqlx query macro invocation (query!, query_as!,
// query_scalar! and their _unchecked forms) or query function call (query,
// query_as, query_scalar, query_with, query_as_with). It is syntax: whether
// the path names sqlx is decided at link time through the scope's imports.
type RustQuery struct {
	// Path is the macro or function path without generic arguments, split
	// on :: ("sqlx", "query_as"); a single segment is a name the scope may
	// import.
	Path  []string
	Macro bool `json:",omitempty"`
	// Literal reports that the SQL argument (the first, or the second for
	// query_as! after its record type) is a string literal (normal or raw),
	// or for a macro a + concatenation of such literals. SQL is its decoded
	// text; both are unset for any other argument.
	Literal bool           `json:",omitempty"`
	SQL     string         `json:",omitempty"`
	SQLSpan *contract.Span `json:",omitempty"`
	// Span is the whole invocation or call.
	Span contract.Span
	// Owner is the identity of the innermost emitted function or method
	// around the site, else the file node (the call capture's Source).
	Owner string
	// Local is the span of the innermost function item around the site
	// when that function is local (declared in a block, row RS-D10 of
	// docs/lang/rust-structure.md, so it has no node and Owner is the
	// enclosing function): only a call inside it can execute the site.
	Local *contract.Span `json:",omitempty"`
	// Scope is the RustFacts.Scopes index of the innermost lexical scope,
	// for resolving a single-segment Path through RustScope.Imports.
	Scope int `json:",omitempty"`
}

// QueriesValid accepts absent queries or structurally complete ones whose
// scope indexes name cached scopes, because link-time resolution indexes
// Scopes with them. A damaged payload is a cache miss.
func (r *RustFacts) QueriesValid() bool {
	if r == nil {
		return true
	}
	for _, q := range r.Queries {
		if len(q.Path) == 0 || q.Owner == "" || q.Span.StartLine <= 0 || q.Scope < 0 || q.Scope >= len(r.Scopes) {
			return false
		}
		for _, s := range q.Path {
			if s == "" {
				return false
			}
		}
		if !q.Literal && (q.SQL != "" || q.SQLSpan != nil) || q.Literal && q.SQLSpan == nil {
			return false
		}
	}
	return true
}

// RustScope keeps lexical uses and items, including private literal constants.
type RustScope struct {
	Parent           int
	Module, Function string
	Start, End       uint32
	Span             contract.Span
	Items            map[string][]RustItem
	Imports          []Import
}

// RustItem points to an existing identity without changing symbol emission.
type RustItem struct {
	Identity string
	Value    RustExpr
	Constant bool
}

// RustExpr is the small expression vocabulary needed by Axum object flow.
type RustExpr struct {
	Kind, Text, Name string
	Scope            int
	Offset           uint32
	Object           *RustExpr
	Args             []RustExpr
	Span             contract.Span
	Hash             string
	Conditional      bool
}

// RustStatement preserves ordered bindings and lexical blocks for router flow.
type RustStatement struct {
	Kind, Name  string
	Scope       int
	Expr        RustExpr
	Children    []RustStatement
	Conditional bool
}

// RustFunction keeps a named function's router-producing body, never its result.
type RustFunction struct {
	Identity    string
	Scope       int
	Body        []RustStatement
	Conditional bool
}
