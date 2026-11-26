package golang

import (
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the Go syntax that GORM model linking reads (ADR 0063,
// T018g5): the type shapes of every non-test Go file and, in files
// that pass gate G1, the functions, locals, calls and package-level variables that
// GORM discovery reads. It records syntax only (rule 31). Every decision that
// needs resolution (is this struct migrated, is this receiver a *gorm.DB,
// does this call open a database) stays at link time. The payload table in
// docs/lang/go-models.md names every capture branch below by its row id, and
// TestGoPayloadContract keeps code and table 1:1.

// goModelTag is a test-only hook: every capture branch reports the payload
// row it serves and the record it produced. It is nil in production.
var goModelTag func(row string, record any)

func tagGo(row string, record any) {
	if goModelTag != nil {
		goModelTag(row, record)
	}
}

// gormMethods are the method names GORM's schema parse calls on a model or a
// field type (TableName for the table name, the Valuer, Scanner and data
// type interfaces for a field's type). Other methods have no table fact.
var gormMethods = map[string]bool{"TableName": true, "Value": true, "Scan": true, "GormDataType": true, "GormDBDataType": true}

// gormSiteNames are the selectors of the calls GORM discovery reads: the
// migrate calls, gorm.Open and the dialector constructors, and the receiver
// chain methods of the claim contract's row GO-K3.
// T018g5 Revision 2 adds SetupJoinTable (A2), the Migrator DDL methods
// (facts.GoMigratorDDL) and Exec and Raw (A3), whose calls change the
// tables the migrate calls create.
var gormSiteNames = func() map[string]bool {
	names := map[string]bool{"AutoMigrate": true, "CreateTable": true, "Open": true, "New": true, "Migrator": true, "Set": true, "Session": true, "Debug": true, "WithContext": true, "SetupJoinTable": true, "Exec": true, "Raw": true}
	for n := range facts.GoMigratorDDL {
		names[n] = true
	}
	return names
}()

// gormMutationNames are the selector names whose assignment changes GORM's
// configuration after gorm.Open (contract row GO-K2, assumption GO-V4).
var gormMutationNames = map[string]bool{"Config": true, "NamingStrategy": true, "DisableForeignKeyConstraintWhenMigrating": true, "IgnoreRelationshipsWhenMigrating": true, "TablePrefix": true, "SingularTable": true, "NoLowerCase": true, "IdentifierMaxLength": true, "NameReplacer": true}

// gormGated reports gate G1: an import of gorm.io/gorm (or a package under
// it), a gorm.io/driver package, github.com/glebarez/sqlite,
// github.com/jinzhu/inflection or github.com/jinzhu/gorm (or a package under
// it).
func gormGated(module string) bool {
	under := func(root string) bool { return module == root || strings.HasPrefix(module, root+"/") }
	return under("gorm.io/gorm") || strings.HasPrefix(module, "gorm.io/driver/") || module == "github.com/glebarez/sqlite" || module == "github.com/jinzhu/inflection" || under("github.com/jinzhu/gorm")
}

func line(n *parse.Node) int    { return int(n.Row) + 1 }
func endLine(n *parse.Node) int { return int(n.EndRow) + 1 }

// goCapture holds the per-file state of one capture.
type goCapture struct {
	// inflection holds the file's local names of the inflection import.
	inflection map[string]bool
	// dbResult is set while capturing a function whose first result is a
	// pointer to a type named DB (its returns are kept, Revision 2 A4).
	dbResult bool
}

// dbPointer reports a type expression *x.DB.
func dbPointer(t facts.GoExpr) bool {
	return t.Kind == facts.GoPointer && t.Object != nil && t.Object.Kind == facts.GoSelector && t.Object.Name == "DB"
}

// captureGo builds the Go model payload of one file. imports are the file's
// already extracted import specs.
func captureGo(file string, root *parse.Node, imports []facts.Import) *facts.GoFacts {
	out := &facts.GoFacts{Version: facts.GoModelsVersion}
	if strings.HasSuffix(file, "_test.go") {
		// Test files are not part of the package GORM migrates from
		// (contract row GO-K1, open point O10): nothing is read from them.
		tagGo("GT", *out)
		return out
	}
	c := goCapture{inflection: map[string]bool{}}
	for _, imp := range imports {
		if gormGated(imp.Module) {
			out.Gated = true
		}
		if imp.Module == "github.com/jinzhu/inflection" {
			for alias := range imp.Bindings {
				c.inflection[alias] = true
			}
		}
	}
	if out.Gated {
		tagGo("G1", *out)
	} else {
		tagGo("GD", *out)
	}
	if root == nil {
		return out
	}
	for _, d := range root.Children {
		switch d.Kind {
		case "type_declaration":
			for _, spec := range d.Children {
				c.typeSpec(out, spec)
			}
		case "method_declaration":
			c.method(out, d)
			if out.Gated {
				c.function(out, d)
			}
		case "function_declaration":
			if out.Gated {
				c.function(out, d)
			} else {
				tagGo("TD", d.Kind)
			}
		case "const_declaration":
			for _, spec := range specs(d, "const_spec") {
				c.constSpec(out, spec)
			}
		case "var_declaration":
			for _, spec := range specs(d, "var_spec") {
				if out.Gated {
					v := facts.GoVar{Names: identifiers(spec), Type: typeOf(spec), Values: values(spec), Line: line(spec)}
					tagGo("V1", v)
					out.Vars = append(out.Vars, v)
				} else {
					tagGo("TD", spec.Kind)
				}
			}
		}
	}
	return out
}

// specs lists the specs of a declaration, through a grouping list node.
func specs(n *parse.Node, kind string) []*parse.Node {
	var out []*parse.Node
	for _, c := range n.Children {
		if c.Kind == kind {
			out = append(out, c)
		} else {
			out = append(out, specs(c, kind)...)
		}
	}
	return out
}

func typeOf(spec *parse.Node) *facts.GoExpr {
	if t := spec.Fields["type"]; t != nil {
		e := reduce(t, 0)
		return &e
	}
	return nil
}

func values(spec *parse.Node) []facts.GoExpr {
	list := spec.Fields["value"]
	if list == nil {
		return nil
	}
	var out []facts.GoExpr
	for _, v := range kids(list) {
		out = append(out, reduce(v, 0))
	}
	return out
}

func typeParams(spec *parse.Node) []string {
	list := spec.Fields["type_parameters"]
	if list == nil {
		return nil
	}
	var out []string
	for _, p := range kids(list) {
		out = append(out, identifiers(p)...)
	}
	return out
}

func (c *goCapture) typeSpec(out *facts.GoFacts, spec *parse.Node) {
	name := facts.Text(spec.Fields["name"])
	typ := spec.Fields["type"]
	if name == "" || typ == nil {
		return
	}
	if spec.Kind == "type_spec" && typ.Kind == "struct_type" {
		s := facts.GoStruct{Name: name, Line: line(spec), EndLine: endLine(spec), TypeParams: typeParams(spec)}
		for _, list := range typ.Children {
			if list.Kind != "field_declaration_list" {
				continue
			}
			for _, f := range kids(list) {
				if f.Kind == "field_declaration" {
					s.Fields = append(s.Fields, field(f))
				}
			}
		}
		tagGo("T-struct", s)
		out.Structs = append(out.Structs, s)
		return
	}
	t := facts.GoNamedType{Name: name, Alias: spec.Kind == "type_alias", TypeParams: typeParams(spec), Type: reduce(typ, 0), Line: line(spec)}
	if !t.Alias && typ.Kind == "interface_type" {
		for _, el := range kids(typ) {
			if el.Kind == "method_elem" || el.Kind == "method_spec" {
				if n := facts.Text(el.Fields["name"]); n != "" {
					t.Methods = append(t.Methods, n)
				}
			}
		}
		tagGo("T-interface", t)
		out.Types = append(out.Types, t)
		return
	}
	if t.Alias {
		tagGo("T-alias", t)
	} else {
		tagGo("T-type", t)
	}
	out.Types = append(out.Types, t)
}

func field(n *parse.Node) facts.GoField {
	f := facts.GoField{Line: line(n)}
	for _, c := range n.Children {
		if c.Kind == "field_identifier" {
			f.Names = append(f.Names, c.Text)
		}
	}
	if t := n.Fields["type"]; t != nil {
		f.Type = reduce(t, 0)
	} else {
		f.Type = facts.GoExpr{Kind: facts.GoUnknown}
	}
	if tag := n.Fields["tag"]; tag != nil {
		f.Tag, _ = strconv.Unquote(tag.Text)
	}
	if len(f.Names) == 0 {
		f.Embedded = true
		for _, tok := range n.Tokens {
			if tok == "*" {
				f.Pointer = true
			}
		}
		tagGo("T-embedded", f)
	} else {
		tagGo("T-field", f)
	}
	return f
}

// receiverType returns a method's receiver base type name and pointer flag.
func receiverType(n *parse.Node) (string, bool, *facts.GoParam) {
	r := n.Fields["receiver"]
	if r == nil {
		return "", false, nil
	}
	for _, p := range r.Children {
		t := p.Fields["type"]
		if t == nil {
			continue
		}
		pointer := t.Kind == "pointer_type"
		param := &facts.GoParam{Name: facts.Text(p.Fields["name"]), Type: reduce(t, 0)}
		return namedType(t), pointer, param
	}
	return "", false, nil
}

// count counts the declared names of a parameter or result list (an unnamed
// declaration counts once); a bare result type counts once.
func count(list *parse.Node) int {
	if list == nil {
		return 0
	}
	if list.Kind != "parameter_list" {
		return 1
	}
	n := 0
	for _, p := range kids(list) {
		if p.Kind != "parameter_declaration" && p.Kind != "variadic_parameter_declaration" {
			continue
		}
		if names := identifiers(p); len(names) > 0 {
			n += len(names)
		} else {
			n++
		}
	}
	return n
}

func (c *goCapture) method(out *facts.GoFacts, n *parse.Node) {
	name := facts.Text(n.Fields["name"])
	receiver, pointer, _ := receiverType(n)
	if !gormMethods[name] || receiver == "" {
		tagGo("TD", n.Kind)
		return
	}
	m := facts.GoMethod{Receiver: receiver, Pointer: pointer, Name: name, Params: count(n.Fields["parameters"]), Results: count(n.Fields["result"]), Line: line(n)}
	if name == "TableName" {
		if e := singleReturn(n.Fields["body"]); e != nil {
			m.Return = e
			tagGo("T-tablename", m)
			out.Methods = append(out.Methods, m)
			return
		}
	}
	tagGo("T-method", m)
	out.Methods = append(out.Methods, m)
}

// singleReturn reduces the expression of a body that is exactly one
// `return e` with one result.
func singleReturn(body *parse.Node) *facts.GoExpr {
	stmts := statements(body)
	if len(stmts) != 1 || stmts[0].Kind != "return_statement" || len(kids(stmts[0])) != 1 {
		return nil
	}
	list := kids(stmts[0])[0]
	if list.Kind != "expression_list" || len(kids(list)) != 1 {
		return nil
	}
	e := reduce(kids(list)[0], 0)
	return &e
}

// statements lists the statements of a block.
func statements(block *parse.Node) []*parse.Node {
	if block == nil {
		return nil
	}
	var out []*parse.Node
	for _, c := range kids(block) {
		if c.Kind == "statement_list" {
			out = append(out, kids(c)...)
		}
	}
	return out
}

// stringShaped reports a value built only from string literals, names,
// selectors and + (a constant TableName may return, O7); iota is not.
func stringShaped(e facts.GoExpr) bool {
	switch e.Kind {
	case facts.GoString, facts.GoSelector:
		return true
	case facts.GoIdentifier:
		return e.Name != "iota"
	case facts.GoBinary:
		return e.Value == "+" && stringShaped(*e.Left) && stringShaped(*e.Right)
	}
	return false
}

func (c *goCapture) constSpec(out *facts.GoFacts, spec *parse.Node) {
	names := identifiers(spec)
	vals := values(spec)
	if len(vals) != len(names) {
		tagGo("TD", spec.Kind)
		return
	}
	for i, name := range names {
		if !stringShaped(vals[i]) {
			tagGo("TD", spec.Kind)
			continue
		}
		k := facts.GoConst{Name: name, Type: typeOf(spec), Value: vals[i], Line: line(spec)}
		tagGo("T-const", k)
		out.Consts = append(out.Consts, k)
	}
}

func params(list *parse.Node) []facts.GoParam {
	if list == nil {
		return nil
	}
	if list.Kind != "parameter_list" {
		return []facts.GoParam{{Type: reduce(list, 0)}}
	}
	var out []facts.GoParam
	for _, p := range kids(list) {
		if p.Kind != "parameter_declaration" && p.Kind != "variadic_parameter_declaration" {
			continue
		}
		typ := facts.GoExpr{Kind: facts.GoUnknown}
		if t := p.Fields["type"]; t != nil {
			typ = reduce(t, 0)
		}
		variadic := p.Kind == "variadic_parameter_declaration"
		names := identifiers(p)
		if len(names) == 0 {
			names = []string{""}
		}
		for _, name := range names {
			param := facts.GoParam{Name: name, Type: typ, Variadic: variadic}
			tagGo("F-param", param)
			out = append(out, param)
		}
	}
	return out
}

func (c *goCapture) function(out *facts.GoFacts, n *parse.Node) {
	name := facts.Text(n.Fields["name"])
	body := n.Fields["body"]
	if name == "" || body == nil {
		tagGo("FD", n.Kind)
		return
	}
	f := facts.GoFunc{Name: name, Line: line(n), EndLine: endLine(n), Params: params(n.Fields["parameters"]), Results: params(n.Fields["result"])}
	if n.Kind == "method_declaration" {
		_, _, f.Receiver = receiverType(n)
	}
	c.dbResult = len(f.Results) > 0 && dbPointer(f.Results[0].Type)
	for i, s := range statements(body) {
		c.topStatement(&f, s, i)
	}
	f.Locals = usedLocals(f)
	tagGo("F1", f)
	out.Funcs = append(out.Funcs, f)
}

// topStatement captures one top-level statement of a function body: its
// call statement record, then its parts with their nesting mark.
func (c *goCapture) topStatement(f *facts.GoFunc, s *parse.Node, index int) {
	if callee, kind := callStatement(s); callee != nil {
		st := facts.GoStatement{Index: index, Line: line(s), Kind: kind, Callee: *callee}
		tagGo("X-statement", st)
		f.Statements = append(f.Statements, st)
	}
	switch s.Kind {
	case "if_statement":
		for _, part := range s.Children {
			nesting := facts.GoConditional
			if part == s.Fields["initializer"] || part == s.Fields["condition"] {
				nesting = facts.GoIfHeader
			}
			c.visit(f, part, index, nesting, false)
		}
	case "expression_statement", "short_var_declaration", "assignment_statement", "var_declaration", "return_statement", "inc_statement", "dec_statement", "send_statement", "const_declaration":
		c.visit(f, s, index, facts.GoUnconditional, false)
	default:
		c.visit(f, s, index, facts.GoConditional, false)
	}
}

// callStatement recognizes a top-level statement that calls a name or
// `name.name` (GoStatement).
func callStatement(s *parse.Node) (*facts.GoExpr, string) {
	single := func(list *parse.Node) *parse.Node {
		if list != nil && list.Kind == "expression_list" && len(kids(list)) == 1 {
			return kids(list)[0]
		}
		return nil
	}
	plain := func(call *parse.Node) *facts.GoExpr {
		if call == nil || call.Kind != "call_expression" {
			return nil
		}
		callee := call.Fields["function"]
		if callee == nil {
			return nil
		}
		if callee.Kind == "identifier" || callee.Kind == "selector_expression" && callee.Fields["operand"] != nil && callee.Fields["operand"].Kind == "identifier" {
			e := reduce(callee, 0)
			return &e
		}
		return nil
	}
	switch s.Kind {
	case "expression_statement":
		if len(kids(s)) == 1 {
			return plain(kids(s)[0]), "call"
		}
	case "short_var_declaration":
		return plain(single(s.Fields["right"])), "define"
	case "assignment_statement":
		if isAssign(s) {
			return plain(single(s.Fields["right"])), "assign"
		}
	case "return_statement":
		if len(kids(s)) == 1 {
			return plain(single(kids(s)[0])), "return"
		}
	case "if_statement":
		if init := s.Fields["initializer"]; init != nil {
			if e, _ := callStatement(init); e != nil {
				return e, "if"
			}
		}
	}
	return nil, ""
}

// isAssign reports a plain `=` assignment (not `+=` and the like).
func isAssign(s *parse.Node) bool {
	for _, tok := range s.Tokens {
		if tok == "=" {
			return true
		}
	}
	return false
}

// visit walks a statement part, recording locals, mutations and sites.
// inSite suppresses sites already contained in a captured site's tree; a
// function literal resets it and marks everything below as closure.
func (c *goCapture) visit(f *facts.GoFunc, n *parse.Node, index int, nesting string, inSite bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "func_literal":
		nesting, inSite = facts.GoClosure, false
	case "block":
		if nesting != facts.GoClosure {
			nesting = facts.GoConditional
		}
	case "var_spec":
		l := facts.GoLocal{Kind: "var", Names: identifiers(n), Type: typeOf(n), Values: values(n), Line: line(n), Statement: index, Nesting: nesting}
		f.Locals = append(f.Locals, l)
	case "short_var_declaration":
		l := facts.GoLocal{Kind: "define", Names: identifiers(n.Fields["left"]), Values: exprs(n.Fields["right"]), Line: line(n), Statement: index, Nesting: nesting}
		f.Locals = append(f.Locals, l)
	case "assignment_statement":
		c.assignment(f, n, index, nesting)
	case "type_declaration":
		for _, spec := range n.Children {
			if name := facts.Text(spec.Fields["name"]); name != "" && (spec.Kind == "type_spec" || spec.Kind == "type_alias") {
				tagGo("L-type", struct{ Name string }{name})
				f.LocalTypes = append(f.LocalTypes, name)
			}
		}
		return
	case "return_statement":
		if c.dbResult && nesting != facts.GoClosure {
			r := facts.GoReturn{Line: line(n), Nesting: nesting}
			if list := kids(n); len(list) == 1 {
				if vals := exprs(list[0]); len(vals) > 0 {
					r.Value = &vals[0]
				}
			}
			tagGo("X-return", r)
			f.Returns = append(f.Returns, r)
		}
	case "composite_literal":
		if t := n.Fields["type"]; t != nil && (t.Kind == "type_identifier" || t.Kind == "qualified_type") {
			lit := reduce(n, 0)
			for _, el := range lit.Elements {
				if facts.GoHandleShaped(el.Value) {
					s := facts.GoSite{Call: lit, Line: line(n), Statement: index, Nesting: nesting}
					tagGo("X-literal", s)
					f.Literals = append(f.Literals, s)
					break
				}
			}
		}
	case "call_expression":
		if callee := n.Fields["function"]; callee != nil && (callee.Kind == "identifier" || callee.Kind == "selector_expression" && callee.Fields["operand"] != nil && callee.Fields["operand"].Kind == "identifier") {
			call := reduce(n, 0)
			for _, a := range call.Args {
				if facts.GoHandleShaped(a) {
					s := facts.GoSite{Call: call, Line: line(n), Statement: index, Nesting: nesting}
					tagGo("X-call", s)
					f.Calls = append(f.Calls, s)
					break
				}
			}
		}
		if !inSite && c.site(n) {
			s := facts.GoSite{Call: reduce(n, 0), Line: line(n), Statement: index, Nesting: nesting}
			if c.inflectionCall(n) {
				tagGo("X-inflection", s)
			} else {
				tagGo("X-site", s)
			}
			tagNesting(nesting)
			f.Sites = append(f.Sites, s)
			inSite = true
		}
	}
	for _, child := range n.Children {
		c.visit(f, child, index, nesting, inSite)
	}
}

// usedLocals keeps the locals that declare or assign a name the function's
// sites or mutations use, directly or through the values of other kept
// locals (a receiver, a configuration variable, an argument slice). Other
// locals are row FD: no GORM discovery rule reads them.
func usedLocals(f facts.GoFunc) []facts.GoLocal {
	used := map[string]bool{}
	note := func(e *facts.GoExpr) {
		if e != nil {
			walkNames(*e, used)
		}
	}
	for i := range f.Sites {
		note(&f.Sites[i].Call)
	}
	for i := range f.Mutations {
		note(&f.Mutations[i].Target)
		note(f.Mutations[i].Value)
	}
	for i := range f.Stores {
		note(f.Stores[i].Value)
	}
	for i := range f.Returns {
		note(f.Returns[i].Value)
	}
	for i := range f.Calls {
		note(&f.Calls[i].Call)
	}
	for i := range f.Literals {
		note(&f.Literals[i].Call)
	}
	kept := make([]bool, len(f.Locals))
	for changed := true; changed; {
		changed = false
		for i, l := range f.Locals {
			if kept[i] {
				continue
			}
			for _, name := range l.Names {
				if used[name] {
					kept[i], changed = true, true
				}
			}
			if kept[i] {
				for j := range l.Values {
					note(&l.Values[j])
				}
			}
		}
	}
	var out []facts.GoLocal
	for i, l := range f.Locals {
		if !kept[i] {
			tagGo("FD", l.Kind)
			continue
		}
		tagGo("L-"+l.Kind, l)
		tagNesting(l.Nesting)
		out = append(out, l)
	}
	return out
}

// walkNames adds every identifier of a reduced tree to names.
func walkNames(e facts.GoExpr, names map[string]bool) {
	if e.Kind == facts.GoIdentifier {
		names[e.Name] = true
	}
	for _, x := range []*facts.GoExpr{e.Object, e.Callee, e.Type, e.Left, e.Right} {
		if x != nil {
			walkNames(*x, names)
		}
	}
	for _, a := range e.Args {
		walkNames(a, names)
	}
	for _, el := range e.Elements {
		if el.Key != nil {
			walkNames(*el.Key, names)
		}
		walkNames(el.Value, names)
	}
}

func tagNesting(nesting string) {
	tagGo("N-"+nesting, struct{ Nesting string }{nesting})
}

func exprs(list *parse.Node) []facts.GoExpr {
	if list == nil {
		return nil
	}
	var out []facts.GoExpr
	for _, v := range kids(list) {
		out = append(out, reduce(v, 0))
	}
	return out
}

// assignment records `x, y = e` as a local and an assignment to a GORM
// configuration selector as a mutation; other assignments are FD.
func (c *goCapture) assignment(f *facts.GoFunc, n *parse.Node, index int, nesting string) {
	left, right := n.Fields["left"], n.Fields["right"]
	if left == nil || !isAssign(n) {
		tagGo("FD", n.Kind)
		return
	}
	idents := true
	for _, l := range kids(left) {
		if l.Kind != "identifier" {
			idents = false
		}
	}
	if idents {
		l := facts.GoLocal{Kind: "assign", Names: identifiers(left), Values: exprs(right), Line: line(n), Statement: index, Nesting: nesting}
		f.Locals = append(f.Locals, l)
		return
	}
	vals := exprs(right)
	mutated := false
	targets := kids(left)
	for i, l := range targets {
		if l.Kind != "selector_expression" || !gormMutationNames[facts.Text(l.Fields["field"])] {
			continue
		}
		m := facts.GoMutation{Target: reduce(l, 0), Line: line(n), Statement: index, Nesting: nesting}
		if len(vals) == len(targets) {
			v := vals[i]
			m.Value = &v
		}
		tagGo("X-mutation", m)
		tagNesting(nesting)
		f.Mutations = append(f.Mutations, m)
		mutated = true
	}
	// Stores (Revision 2, A4): an assignment to a selector of a value that
	// may be a *gorm.DB; Value is nil when the sides do not pair up (the
	// second result of a call).
	for i, l := range targets {
		if l.Kind != "selector_expression" {
			continue
		}
		var v *facts.GoExpr
		switch {
		case len(vals) == len(targets):
			v = &vals[i]
		case len(vals) == 1 && i == 0:
			v = &vals[0]
		case len(vals) == 1:
		default:
			continue
		}
		if len(vals) != 1 && !facts.GoHandleShaped(*v) || len(vals) == 1 && !facts.GoHandleShaped(vals[0]) {
			continue
		}
		st := facts.GoMutation{Target: reduce(l, 0), Value: v, Line: line(n), Statement: index, Nesting: nesting}
		tagGo("X-store", st)
		f.Stores = append(f.Stores, st)
		mutated = true
	}
	if !mutated {
		tagGo("FD", n.Kind)
	}
}

func (c *goCapture) inflectionCall(n *parse.Node) bool {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "selector_expression" {
		return false
	}
	operand := callee.Fields["operand"]
	return operand != nil && operand.Kind == "identifier" && c.inflection[operand.Text]
}

func (c *goCapture) site(n *parse.Node) bool {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "selector_expression" {
		return false
	}
	return gormSiteNames[facts.Text(callee.Fields["field"])] || c.inflectionCall(n)
}

// reduce turns an expression or type node into a reduced tree (rows E-*).
func reduce(n *parse.Node, depth int) facts.GoExpr { return reducer{}.reduce(n, depth) }

// reducer reduces expressions. route is set while the router capture
// (routes.go) reduces its trees: it then keeps function literals (row
// RE-funclit) and the argument texts of router-named calls (row RE-text),
// which the model payload does not read.
type reducer struct{ route *routeCapture }

func (rd reducer) reduce(n *parse.Node, depth int) facts.GoExpr {
	if n == nil {
		return facts.GoExpr{Kind: facts.GoUnknown}
	}
	if depth >= facts.GoMaxDepth {
		e := facts.GoExpr{Kind: facts.GoUnknown}
		tagGo("E-unknown", e)
		return e
	}
	sub := func(x *parse.Node) *facts.GoExpr {
		e := rd.reduce(x, depth+1)
		return &e
	}
	var e facts.GoExpr
	row := ""
	switch n.Kind {
	case "parenthesized_expression", "parenthesized_type":
		if inner := kids(n); len(inner) == 1 {
			e = rd.reduce(inner[0], depth)
			tagGo("E-paren", e)
			return e
		}
		e, row = facts.GoExpr{Kind: facts.GoUnknown}, "E-unknown"
	case "literal_element", "type_elem":
		// Grammar wrappers around one element or type argument.
		if inner := kids(n); len(inner) == 1 {
			return rd.reduce(inner[0], depth)
		}
		e, row = facts.GoExpr{Kind: facts.GoUnknown}, "E-unknown"
	case "identifier", "type_identifier", "field_identifier", "package_identifier", "iota":
		e, row = facts.GoExpr{Kind: facts.GoIdentifier, Name: n.Text}, "E-identifier"
	case "true", "false":
		e, row = facts.GoExpr{Kind: facts.GoBoolean, Value: n.Kind}, "E-boolean"
	case "nil":
		e, row = facts.GoExpr{Kind: facts.GoNil}, "E-nil"
	case "selector_expression":
		e, row = facts.GoExpr{Kind: facts.GoSelector, Object: sub(n.Fields["operand"]), Name: facts.Text(n.Fields["field"])}, "E-selector"
	case "qualified_type":
		e, row = facts.GoExpr{Kind: facts.GoSelector, Object: sub(n.Fields["package"]), Name: facts.Text(n.Fields["name"])}, "E-selector"
	case "call_expression":
		e, row = facts.GoExpr{Kind: facts.GoCall, Callee: sub(n.Fields["function"])}, "E-call"
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range kids(args) {
				if a.Kind == "variadic_argument" {
					s := facts.GoExpr{Kind: facts.GoSpread, Object: sub(firstChild(a))}
					tagGo("E-spread", s)
					e.Args = append(e.Args, s)
					continue
				}
				e.Args = append(e.Args, rd.reduce(a, depth+1))
			}
		}
		if rd.route != nil {
			rd.route.argTexts(n, &e)
		}
	case "func_literal":
		if rd.route != nil {
			return rd.route.funcLiteral(n)
		}
		e, row = facts.GoExpr{Kind: facts.GoUnknown}, "E-unknown"
	case "type_conversion_expression":
		e, row = facts.GoExpr{Kind: facts.GoCall, Callee: sub(n.Fields["type"]), Args: []facts.GoExpr{rd.reduce(n.Fields["operand"], depth+1)}}, "E-call"
	case "composite_literal":
		e, row = facts.GoExpr{Kind: facts.GoComposite, Type: sub(n.Fields["type"]), Elements: rd.elements(n.Fields["body"], depth)}, "E-composite"
	case "literal_value":
		e, row = facts.GoExpr{Kind: facts.GoComposite, Elements: rd.elements(n, depth)}, "E-composite"
	case "unary_expression":
		e, row = facts.GoExpr{Kind: facts.GoUnary, Value: operator(n), Object: sub(n.Fields["operand"])}, "E-unary"
	case "binary_expression":
		e, row = facts.GoExpr{Kind: facts.GoBinary, Value: operator(n), Left: sub(n.Fields["left"]), Right: sub(n.Fields["right"])}, "E-binary"
	case "interpreted_string_literal", "raw_string_literal":
		if v, err := strconv.Unquote(n.Text); err == nil {
			e, row = facts.GoExpr{Kind: facts.GoString, Value: v}, "E-string"
		} else {
			e, row = facts.GoExpr{Kind: facts.GoUnknown}, "E-unknown"
		}
	case "int_literal", "float_literal", "imaginary_literal":
		e, row = facts.GoExpr{Kind: facts.GoNumber, Value: n.Text}, "E-number"
	case "index_expression":
		e, row = facts.GoExpr{Kind: facts.GoIndex, Object: sub(n.Fields["operand"]), Args: []facts.GoExpr{rd.reduce(n.Fields["index"], depth+1)}}, "E-index"
	case "generic_type":
		e, row = facts.GoExpr{Kind: facts.GoIndex, Object: sub(n.Fields["type"])}, "E-index"
		if args := n.Fields["type_arguments"]; args != nil {
			for _, a := range kids(args) {
				e.Args = append(e.Args, rd.reduce(a, depth+1))
			}
		}
	case "pointer_type":
		e, row = facts.GoExpr{Kind: facts.GoPointer, Object: sub(firstChild(n))}, "E-pointer"
	case "slice_type":
		e, row = facts.GoExpr{Kind: facts.GoSlice, Object: sub(n.Fields["element"])}, "E-slice"
	case "array_type":
		e, row = facts.GoExpr{Kind: facts.GoArray, Value: facts.Text(n.Fields["length"]), Object: sub(n.Fields["element"])}, "E-array"
	case "implicit_length_array_type":
		e, row = facts.GoExpr{Kind: facts.GoArray, Value: "...", Object: sub(n.Fields["element"])}, "E-array"
	case "map_type":
		e, row = facts.GoExpr{Kind: facts.GoMap, Left: sub(n.Fields["key"]), Right: sub(n.Fields["value"])}, "E-map"
	case "channel_type":
		e, row = facts.GoExpr{Kind: facts.GoChan}, "E-chan"
	case "function_type":
		e, row = facts.GoExpr{Kind: facts.GoFuncType}, "E-func"
	case "interface_type":
		e, row = facts.GoExpr{Kind: facts.GoInterface}, "E-interface"
	case "struct_type":
		e, row = facts.GoExpr{Kind: facts.GoStructType}, "E-struct"
	default:
		e, row = facts.GoExpr{Kind: facts.GoUnknown}, "E-unknown"
	}
	tagGo(row, e)
	return e
}

// firstChild returns a node's first child that is not a comment.
func firstChild(n *parse.Node) *parse.Node {
	if c := kids(n); len(c) > 0 {
		return c[0]
	}
	return nil
}

// kids lists a node's children without comments, which the grammar places
// anywhere between tokens.
func kids(n *parse.Node) []*parse.Node {
	if n == nil {
		return nil
	}
	out := make([]*parse.Node, 0, len(n.Children))
	for _, c := range n.Children {
		if c.Kind != "comment" {
			out = append(out, c)
		}
	}
	return out
}

func operator(n *parse.Node) string {
	if len(n.Tokens) > 0 {
		return n.Tokens[0]
	}
	return "?"
}

// elements reduces a literal_value's keyed and positional elements; an
// elided-type nested literal is a composite without Type.
func (rd reducer) elements(body *parse.Node, depth int) []facts.GoElement {
	if body == nil {
		return nil
	}
	var out []facts.GoElement
	for _, el := range kids(body) {
		switch el.Kind {
		case "keyed_element":
			key := rd.reduce(el.Fields["key"], depth+1)
			g := facts.GoElement{Key: &key, Value: rd.reduce(el.Fields["value"], depth+1)}
			tagGo("E-keyed", g)
			out = append(out, g)
		default:
			g := facts.GoElement{Value: rd.reduce(el, depth+1)}
			tagGo("E-positional", g)
			out = append(out, g)
		}
	}
	return out
}
