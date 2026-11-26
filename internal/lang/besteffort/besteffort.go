// Package besteffort holds the rules the best-effort languages (Java, C#,
// Dart; T021, ADR 0067) share: the whole-file refusal of recovered trees
// (row BE-2), the lexical owner chain and its naming rules (BE-4 to BE-6),
// the span and type-text helpers, and the claim-contract table type that
// keeps docs/lang/best-effort.md and the code one to one.
//
// The rows themselves live in docs/lang/best-effort.md. A language package
// lists its rows as a Table and tags every branch with a row id; the shared
// rows are in Shared.
package besteffort

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Recovered reports whether the detached tree holds an ERROR node or a named
// MISSING node (a zero-width named node below the root). tree-sitter can
// re-parent a member outside its type without leaving it under an ERROR node
// (T021), so one such node refuses the whole file (row BE-2).
//
// The detached tree drops unnamed nodes, so an unnamed MISSING token (a
// missing `;`, say) is invisible here: callers must also honour the parser's
// own syntax flag through Refuse.
func Recovered(root *parse.Node) bool {
	if root == nil {
		return false
	}
	stack := []*parse.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == "ERROR" {
			return true
		}
		if n != root && n.Start == n.End {
			return true
		}
		stack = append(stack, n.Children...)
	}
	return false
}

// Refuse applies row BE-2 to facts already extracted from a file whose tree
// was recovered: every symbol node, containment edge and import directive is
// dropped, and only the diagnostics (the syntax_error gap warning among them)
// and an empty, refused payload stay. scope is what a lexical scan of the
// file's text proves about the names it can declare (row BE-8); the zero
// Scope proves nothing. The file node and file record are made by the
// indexer and are unaffected (row BE-1).
func Refuse(f facts.Facts, scope Scope) facts.Facts {
	payload := &facts.BestEffortFacts{Version: facts.BestEffortVersion, Refused: true}
	if scope.Known {
		payload.Scoped = true
		payload.Scopes = scope.Names
		payload.Global = scope.Global
	}
	return facts.Facts{
		Exports:     map[string]string{},
		Diagnostics: f.Diagnostics,
		BestEffort:  payload,
	}
}

// Scope is what a refused file can declare, read from its raw text because
// its tree cannot be trusted (row BE-8). Known is set only when the scan
// proves that every name the file can declare for an import sits under one
// of Names (Java packages, C# file-scoped namespaces) or, with Names empty,
// that it declares nothing importable. Global, set only with Known, weakens
// that proof to namespaces: the file may also declare types in the global
// namespace (a C# file with block namespaces or without a namespace
// keyword), so Names bound only the namespace names it declares, and a name
// that may be a type is not bounded at all. The scan may over-match (a
// keyword in a comment adds a name, which only hides more) but never
// under-match.
type Scope struct {
	Known  bool
	Names  []string
	Global bool
}

// Occurrence is one place where a keyword stands as a whole word in a text:
// the qualified name read after it and the first byte after that name.
type Occurrence struct {
	// Name is the dotted name after the keyword, whitespace and comments
	// between its parts removed; "" when no identifier follows.
	Name string
	// Next is the first byte after the name, past whitespace and comments;
	// 0 at the end of the text.
	Next byte
}

// Occurrences finds every whole-word keyword in source, in comments and
// string literals too, and reads the qualified name after each one. It is
// the lexical scan behind Scope: since it cannot tell code from comments,
// it over-matches by design. An identifier is a run of Unicode letters,
// digits, '_' and '$'; a keyword directly after '@' (a C# verbatim
// identifier) is not counted.
func Occurrences(source []byte, keyword string) []Occurrence {
	text := string(source)
	var out []Occurrence
	for from := 0; ; {
		i := strings.Index(text[from:], keyword)
		if i < 0 {
			return out
		}
		start := from + i
		end := start + len(keyword)
		from = end
		if start > 0 {
			if r, _ := utf8.DecodeLastRuneInString(text[:start]); identRune(r) || r == '@' {
				continue
			}
		}
		if end < len(text) {
			if r, _ := utf8.DecodeRuneInString(text[end:]); identRune(r) {
				continue
			}
		}
		var name strings.Builder
		pos := skipTrivia(text, end)
		for {
			ident := identEnd(text, pos)
			if ident == pos {
				break
			}
			name.WriteString(text[pos:ident])
			pos = skipTrivia(text, ident)
			if pos >= len(text) || text[pos] != '.' {
				break
			}
			after := skipTrivia(text, pos+1)
			if identEnd(text, after) == after {
				break
			}
			name.WriteByte('.')
			pos = after
		}
		o := Occurrence{Name: name.String()}
		if pos < len(text) {
			o.Next = text[pos]
		}
		out = append(out, o)
	}
}

func identRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// identEnd returns the end of the identifier starting at pos (pos itself
// when none starts there).
func identEnd(text string, pos int) int {
	for pos < len(text) {
		r, size := utf8.DecodeRuneInString(text[pos:])
		if !identRune(r) {
			break
		}
		pos += size
	}
	return pos
}

// skipTrivia skips whitespace and `//` and `/* */` comments from pos.
func skipTrivia(text string, pos int) int {
	for pos < len(text) {
		switch {
		case text[pos] == ' ' || text[pos] == '\t' || text[pos] == '\n' || text[pos] == '\r' || text[pos] == '\f':
			pos++
		case strings.HasPrefix(text[pos:], "//"):
			i := strings.IndexByte(text[pos:], '\n')
			if i < 0 {
				return len(text)
			}
			pos += i + 1
		case strings.HasPrefix(text[pos:], "/*"):
			i := strings.Index(text[pos+2:], "*/")
			if i < 0 {
				return len(text)
			}
			pos += i + 4
		default:
			return pos
		}
	}
	return pos
}

// Decl is one declaration a language extractor decided to emit, before
// naming. Children are the declarations it lexically owns (BE-4).
type Decl struct {
	// Row is the contract row that claims the declaration.
	Row string
	// Kind is the node kind (class, method, constructor, ...).
	Kind string
	// Name is the declared name, the node's `name`.
	Name string
	// Suffix is a discriminator appended to Name in the qualified name
	// before BE-6 and BE-5 run, for declarations a language names apart
	// from same-named siblings (C# `@IFoo` explicit interface members,
	// `@static` constructors, generic arity `` `1 ``). It is not part of
	// the node's `name`.
	Suffix string
	// Node is the declaration's syntax node; its span is the claimed span
	// unless Span is set.
	Node *parse.Node
	// Overload is the parameter type list used for BE-6, for example
	// "(String,int)"; empty for declarations that cannot be overloaded.
	Overload string
	// Overloadable marks callables that take part in BE-6.
	Overloadable bool
	// Attributes are the row's node attributes; nil when the row lists none.
	Attributes map[string]any
	Children   []*Decl

	qualified string
}

// Qualified is the decoded qualified name after Name ran.
func (d *Decl) Qualified() string { return d.qualified }

// Name assigns qualified names to a file's declaration forest: lexical owners
// joined with `.`, each segment the declared name plus its Suffix, then
// `@overload(<types>)` for members of a same-owner, same-kind, same-segment
// set (BE-6), then `@2`, `@3` in source order for identities still repeated
// within the file (BE-5). Roots and children must be in source order.
func Name(roots []*Decl) {
	counts := map[string]int{}
	var visit func(owner string, siblings []*Decl)
	visit = func(owner string, siblings []*Decl) {
		group := map[string]int{}
		for _, d := range siblings {
			if d.Overloadable {
				group[d.Kind+"\x00"+d.Name+d.Suffix]++
			}
		}
		for _, d := range siblings {
			q := facts.Join(owner, d.Name+d.Suffix)
			if d.Overloadable && group[d.Kind+"\x00"+d.Name+d.Suffix] > 1 {
				q += "@overload" + d.Overload
			}
			key := d.Kind + "\x00" + q
			counts[key]++
			if counts[key] > 1 {
				q += "@" + strconv.Itoa(counts[key])
			}
			d.qualified = q
			visit(q, d.Children)
		}
	}
	visit("", roots)
}

// Emit adds the named forest to out: one level-4 node per declaration with
// its span and the content hash of exactly that span (BE-7), and a contains
// edge from its nearest emitted owner, else the file (BE-4). Name must have
// run first.
func Emit(out *facts.Facts, file string, source []byte, roots []*Decl) {
	var visit func(owner string, list []*Decl)
	visit = func(owner string, list []*Decl) {
		for _, d := range list {
			attrs := d.Attributes
			if attrs == nil {
				attrs = map[string]any{}
			}
			identity := facts.Add(out, file, source, d.Node, d.Kind, d.Name, d.qualified, owner, attrs)
			out.Exports[d.qualified] = identity
			visit(identity, d.Children)
		}
	}
	visit("", roots)
}

// trivia are the node kinds TypeText leaves out: annotations (Java) and
// comments. Languages with other annotation node kinds pass them in extra.
var trivia = map[string]bool{"annotation": true, "marker_annotation": true, "line_comment": true, "block_comment": true, "comment": true}

// TypeText returns the source text of n from byte offset start to end with
// whitespace and every descendant of a trivia kind (annotations, comments)
// removed. It is the "type as written" of the overload rule (BE-6) and the
// import name as written (JV-I8).
func TypeText(source []byte, n *parse.Node, start, end uint32) string {
	if n == nil || start >= end || int(end) > len(source) {
		return ""
	}
	type cut struct{ start, end uint32 }
	var cuts []cut
	var collect func(*parse.Node)
	collect = func(c *parse.Node) {
		if trivia[c.Kind] {
			cuts = append(cuts, cut{c.Start, c.End})
			return
		}
		for _, child := range c.Children {
			collect(child)
		}
	}
	collect(n)
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].start < cuts[j].start })
	var b strings.Builder
	for i := start; i < end; i++ {
		skipped := false
		for _, c := range cuts {
			if i >= c.start && i < c.end {
				i = c.end - 1
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		switch source[i] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		}
		b.WriteByte(source[i])
	}
	return b.String()
}

// Row is one claim-contract row: its id and the text of its claim column, as
// docs/lang/best-effort.md prints them. Branches name rows by ID.
type Row struct {
	ID, Claim string
}

// Table is a contract section in document order.
type Table []Row

// IDs lists the row ids in order.
func (t Table) IDs() []string {
	ids := make([]string, len(t))
	for i, r := range t {
		ids[i] = r.ID
	}
	return ids
}

// Has reports whether id is a row of the table.
func (t Table) Has(id string) bool {
	for _, r := range t {
		if r.ID == id {
			return true
		}
	}
	return false
}

// Shared are the rows every best-effort language applies (the "Shared"
// section of docs/lang/best-effort.md). The claim text is the document's,
// with Markdown code marks removed.
var Shared = Table{
	{ID: "BE-1", Claim: "File record with scanner language java, c_sharp, dart and precision: \"best_effort\"; file node"},
	{ID: "BE-2", Claim: "Nothing beyond BE-1; the existing syntax_error warning (a knowledge gap)"},
	{ID: "BE-3", Claim: "Existing parse_failed / extract_failed error diagnostic for that file only"},
	{ID: "BE-4", Claim: "contains edge from the nearest emitted lexical owner, else the file; evidence is the symbol's span"},
	{ID: "BE-5", Claim: "@2, @3 in source order per file and kind"},
	{ID: "BE-6", Claim: "@overload(<types>) per member"},
	{ID: "BE-7", Claim: "Span per its row; content_hash of exactly that span"},
	{ID: "BE-8", Claim: "unresolved_import warning naming a hidden file; no external node, no edge"},
	{ID: "D", Claim: "Nothing"},
}

// Hider is a file of one language whose declarations import resolution
// cannot see (row BE-8): a file refused by BE-2, or a file without a valid
// payload because parsing or extraction failed (BE-3).
type Hider struct {
	File string
	// Scoped is set when the refused file's text proves that the names it
	// can declare sit under Scopes (none at all when Scopes is empty);
	// otherwise it may declare any name.
	Scoped bool
	Scopes []string
	// Global is set with Scoped when Scopes bound only the namespaces the
	// file declares: it may also declare types in the global namespace.
	Global bool
}

// Hidden lists, sorted by path, the hidden files of one language (row BE-8).
func Hidden(files map[string]facts.Facts) []Hider {
	var hidden []Hider
	for _, file := range facts.Paths(files) {
		p := files[file].BestEffort
		switch {
		case !p.Valid():
			hidden = append(hidden, Hider{File: file})
		case p.Refused:
			hidden = append(hidden, Hider{File: file, Scoped: p.Scoped, Scopes: p.Scopes, Global: p.Global})
		}
	}
	return hidden
}

// Hiding returns, in order, the hidden files that may declare name (row
// BE-8): every unscoped one, and every scoped one with a scope equal to name
// or a dotted prefix of it. With prefixes set, a scope that name is a dotted
// prefix of also counts (a C# file declaring namespace A.B makes namespace A
// exist). With typed set, name may be a type in the global namespace (a C#
// static or alias using), so every file whose scopes bound only its
// namespaces (Global) counts too.
func Hiding(hidden []Hider, name string, prefixes, typed bool) []string {
	var files []string
	for _, h := range hidden {
		if !h.Scoped || (h.Global && typed) {
			files = append(files, h.File)
			continue
		}
		for _, scope := range h.Scopes {
			if name == scope || strings.HasPrefix(name, scope+".") || (prefixes && strings.HasPrefix(scope, name+".")) {
				files = append(files, h.File)
				break
			}
		}
	}
	return files
}

// HiddenImport is the row BE-8 warning for a directive (written as shown,
// for example "import java.util.List" or "using System.Linq") that would
// otherwise become an external node while hiding (from Hiding) is not
// empty: the name may be declared in one of those files, so it is reported
// as unresolved instead of being labelled external.
func HiddenImport(file, shown string, hiding []string) contract.Diagnostic {
	where := hiding[0]
	if len(hiding) > 1 {
		where += fmt.Sprintf(" and %d more", len(hiding)-1)
	}
	return contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: shown + " is not declared by a readable scanned file, but may be declared in a file refused for invalid syntax or a failed parse: " + where}
}

// DocTable reads the table under the Markdown heading named heading (the
// first table after it) from doc and returns its rows: the first column as
// the id and the column named "Claim" as the claim, with code marks removed.
// Tests compare it with the Go table, keeping both one to one.
func DocTable(doc, heading string) Table {
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	var table Table
	inSection, claim := false, -1
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			if inSection && claim >= 0 {
				break
			}
			inSection = strings.TrimSpace(strings.TrimLeft(line, "#")) == heading
			continue
		}
		if !inSection || !strings.HasPrefix(line, "|") {
			if inSection && claim >= 0 && strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], "`", ""))
		}
		if claim < 0 {
			for i, c := range cells {
				if c == "Claim" {
					claim = i
				}
			}
			continue
		}
		if strings.HasPrefix(cells[0], "---") || strings.HasPrefix(cells[0], ":--") {
			continue
		}
		if claim < len(cells) {
			table = append(table, Row{ID: cells[0], Claim: cells[claim]})
		}
	}
	return table
}

// Cover returns a node spanning from the start of first to the end of last,
// for rows whose span is wider than one syntax node (a declaration with its
// preceding annotation siblings, say). Its Text is the covered source and it
// has no children; Emit hashes exactly that span (BE-7).
func Cover(source []byte, first, last *parse.Node) *parse.Node {
	n := &parse.Node{Kind: first.Kind, Start: first.Start, End: last.End, Row: first.Row, Column: first.Column, EndRow: last.EndRow, EndColumn: last.EndColumn, Fields: map[string]*parse.Node{}}
	if n.Start <= n.End && int(n.End) <= len(source) {
		n.Text = string(source[n.Start:n.End])
	}
	return n
}
