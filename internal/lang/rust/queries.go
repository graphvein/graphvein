package rust

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the sqlx query sites that code-to-table linking
// (T018h, ADR 0060) reads: the path of a query macro or function, its SQL
// literal decoded, the site's owner and lexical scope. It records syntax
// only; whether the path is sqlx's and which tables the SQL names is decided
// at link time (docs/lang/rust-sqlx.md).

var (
	// sqlxMacros are the checked query macros (CT-S1); the value is the
	// index of the SQL argument (query_as! takes the record type first).
	sqlxMacros = map[string]int{"query": 0, "query_unchecked": 0, "query_scalar": 0, "query_scalar_unchecked": 0, "query_as": 1, "query_as_unchecked": 1}
	// sqlxFunctions are the unchecked query functions (CT-S2).
	sqlxFunctions = setOf("query", "query_as", "query_scalar", "query_with", "query_as_with")
)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// queryCapture collects candidate sites during Extract's walk; the file gate
// is applied once the walk has seen every use declaration.
type queryCapture struct {
	queries []facts.RustQuery
	offsets []uint32
}

// sqlxPath splits a macro or function path and reports whether its spelling
// can name a sqlx query: one segment (imported by a use) or a first segment
// sqlx. The last segment must be in names.
func sqlxPath(path string, known func(string) bool) ([]string, bool) {
	path = strings.TrimPrefix(path, "::")
	segments := strings.Split(path, "::")
	for _, s := range segments {
		if s == "" {
			return nil, false
		}
	}
	if len(segments) > 1 && segments[0] != "sqlx" || !known(segments[len(segments)-1]) {
		return nil, false
	}
	return segments, true
}

// macro records a query macro candidate. A macro body is a token tree, so
// arguments are split on the commas between its child tokens: comments and
// string literals are child nodes, so the text between children holds only
// punctuation.
func (q *queryCapture) macro(n *parse.Node, owner string, local *contract.Span) {
	path, ok := sqlxPath(rustPath(n.Fields["macro"]), func(s string) bool { _, ok := sqlxMacros[s]; return ok })
	if !ok {
		return
	}
	site := facts.RustQuery{Path: path, Macro: true, Span: facts.Span(n), Owner: owner, Local: local}
	var tree *parse.Node
	for _, c := range n.Children {
		if c.Kind == "token_tree" {
			tree = c
		}
	}
	if tree != nil {
		if args := macroArgs(tree); sqlxMacros[path[len(path)-1]] < len(args) {
			sqlLiteral(&site, args[sqlxMacros[path[len(path)-1]]], tree)
		}
	}
	q.queries = append(q.queries, site)
	q.offsets = append(q.offsets, n.Start)
}

// macroArg is one comma-separated argument of a token tree: its child
// nodes and the punctuation between them.
type macroArg struct {
	nodes []*parse.Node
	gaps  []string // gaps[i] is the text before nodes[i+1]
}

func macroArgs(tree *parse.Node) []macroArg {
	text := tree.Text
	var args []macroArg
	current := macroArg{}
	pos := tree.Start + 1 // after the opening delimiter
	gap := ""
	flush := func() {
		args = append(args, current)
		current = macroArg{}
	}
	for _, c := range tree.Children {
		if c.Start < pos || int(c.Start-tree.Start) > len(text) {
			return nil
		}
		between := text[pos-tree.Start : c.Start-tree.Start]
		pos = c.End
		if c.Kind == "line_comment" || c.Kind == "block_comment" {
			gap += between
			continue
		}
		gap += between
		parts := strings.Split(gap, ",")
		for range parts[1:] {
			flush()
		}
		if len(current.nodes) > 0 {
			current.gaps = append(current.gaps, parts[len(parts)-1])
		}
		current.nodes = append(current.nodes, c)
		gap = ""
	}
	if len(current.nodes) > 0 {
		flush()
	}
	return args
}

// sqlLiteral decodes an argument that is a string literal or, for sqlx
// macros, a + concatenation of string literals. Anything else (a constant,
// concat!, a format) leaves the site without SQL.
func sqlLiteral(site *facts.RustQuery, arg macroArg, tree *parse.Node) {
	var b strings.Builder
	for i, n := range arg.nodes {
		if i > 0 && strings.TrimSpace(arg.gaps[i-1]) != "+" {
			return
		}
		if n.Kind != "string_literal" && n.Kind != "raw_string_literal" {
			return
		}
		s, ok := StringLiteral(n.Text)
		if !ok {
			return
		}
		b.WriteString(s)
	}
	if len(arg.nodes) == 0 {
		return
	}
	first, last := arg.nodes[0], arg.nodes[len(arg.nodes)-1]
	span := contract.Span{StartLine: int(first.Row) + 1, StartColumn: int(first.Column), EndLine: int(last.EndRow) + 1, EndColumn: int(last.EndColumn)}
	site.Literal, site.SQL, site.SQLSpan = true, b.String(), &span
}

// call records a query function candidate (sqlx::query("..."),
// query_as::<_, T>("...")); its first argument is the SQL.
func (q *queryCapture) call(n *parse.Node, owner string, local *contract.Span) {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "identifier" && callee.Kind != "scoped_identifier" && callee.Kind != "generic_function" {
		return
	}
	if callee.Kind == "generic_function" {
		if inner := callee.Fields["function"]; inner == nil || inner.Kind != "identifier" && inner.Kind != "scoped_identifier" {
			return
		}
	}
	path, ok := sqlxPath(rustPath(callee), func(s string) bool { return sqlxFunctions[s] })
	if !ok {
		return
	}
	site := facts.RustQuery{Path: path, Span: facts.Span(n), Owner: owner, Local: local}
	if args := n.Fields["arguments"]; args != nil {
		for _, a := range args.Children {
			if a.Kind == "line_comment" || a.Kind == "block_comment" {
				continue
			}
			if a.Kind == "string_literal" || a.Kind == "raw_string_literal" {
				if s, ok := StringLiteral(a.Text); ok {
					span := facts.Span(a)
					site.Literal, site.SQL, site.SQLSpan = true, s, &span
				}
			}
			break
		}
	}
	q.queries = append(q.queries, site)
	q.offsets = append(q.offsets, n.Start)
}

// finish applies the file gate (a use of sqlx, or a site path starting with
// sqlx) and gives each site its innermost lexical scope.
func (q *queryCapture) finish(imports []facts.Import, rust *facts.RustFacts) {
	if rust == nil || len(q.queries) == 0 {
		return
	}
	gate := false
	for _, imp := range imports {
		gate = gate || strings.SplitN(strings.TrimPrefix(imp.Module, "::"), "::", 2)[0] == "sqlx"
	}
	for _, s := range q.queries {
		gate = gate || len(s.Path) > 1
	}
	if !gate {
		return
	}
	for i := range q.queries {
		best := 0
		for j, s := range rust.Scopes {
			if s.Start <= q.offsets[i] && q.offsets[i] < s.End && s.Start >= rust.Scopes[best].Start {
				best = j
			}
		}
		q.queries[i].Scope = best
	}
	rust.Queries = q.queries
}
