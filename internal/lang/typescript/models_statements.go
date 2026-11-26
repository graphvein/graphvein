package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file holds the version 2 parts of the model payload (ADR 0061,
// T018g3): the Sequelize gate rows, module-level statements, function body
// statements and their validity rules. Like the rest of the payload it is
// syntax only; the Sequelize linker decides at link time what a statement
// means.

// Model statement kinds. Each kind fills only the fields its comment names.
const (
	StatementExpression  = "expression"  // Value: a call or an assignment at module level, any expression in a body
	StatementClass       = "class"       // Name, Exported, Default, Abstract, Extends, Decorators
	StatementDestructure = "destructure" // Keyword, Exported, Bindings, Value (the initializer)
	StatementFunction    = "function"    // Name, Exported, Default, Value (a function expression); module level, or a static method in Statics (version 4)
	StatementForOf       = "for-of"      // Keyword, Name, Value (the iterable), Body; module level only (version 4)
	StatementIf          = "if"          // Value (the condition), Body (the consequence), Else (version 5); bodies only (version 4)
	StatementEnum        = "enum"        // Name, Exported, Const, EnumMembers; module level only (version 5)
	StatementTry         = "try"         // Body (the try block), Catch, Finally; bodies only, TypeORM gate rows only (version 5, T018g6 Revision 3 addendum 2)
	StatementDeclaration = "declaration" // Keyword, Name, Value (nil without an initializer); bodies only
	StatementReturn      = "return"      // Value (nil for a bare return); bodies only
	StatementUnknown     = "unknown"     // nothing but the span; bodies only
)

// modelModuleStatements are the kinds a module-level statement list may
// hold; module-level declarations with an identifier name stay in
// ModelSyntax.Declarations, and other module-level statements are not kept.
var modelModuleStatements = map[string]bool{StatementExpression: true, StatementClass: true, StatementDestructure: true, StatementFunction: true, StatementForOf: true, StatementEnum: true}

var modelBodyStatements = map[string]bool{
	StatementExpression: true, StatementClass: true, StatementDestructure: true,
	StatementDeclaration: true, StatementReturn: true, StatementUnknown: true, StatementIf: true, StatementTry: true,
}

// ModelStatement is one module-level statement of a gated file or one
// statement of a captured function body.
type ModelStatement struct {
	Kind     string        `json:"kind"`
	Span     contract.Span `json:"span"`
	Keyword  string        `json:"keyword,omitempty"`
	Name     string        `json:"name,omitempty"`
	Exported bool          `json:"exported,omitempty"`
	// Default and Abstract describe a class declaration: `export default
	// class` (Name is "" when the class is anonymous) and `abstract class`.
	Default  bool `json:"default,omitempty"`
	Abstract bool `json:"abstract,omitempty"`
	// Extends is the class's `extends` expression (type arguments dropped),
	// nil when the class extends nothing.
	Extends *ModelExpr `json:"extends,omitempty"`
	// Decorators counts the class decorators and the decorators of its
	// members (kept from version 2; the expressions are DecoratorList and
	// each member's Decorators from version 5).
	Decorators int `json:"decorators,omitempty"`
	// DecoratorList are the class decorators' expressions in source order,
	// decorators written before `export` included (version 5, ADR 0064).
	DecoratorList []ModelExpr `json:"decorator_list,omitempty"`
	// Members are a class's instance members in order (version 5): TypeORM
	// columns and relations are decorated instance properties.
	Members []ModelClassMember `json:"members,omitempty"`
	// Const and EnumMembers describe an enum declaration (version 5).
	Const       bool              `json:"const,omitempty"`
	EnumMembers []ModelEnumMember `json:"enum_members,omitempty"`
	// Bindings are the entries of a destructuring object pattern in order.
	Bindings []ModelBinding `json:"bindings,omitempty"`
	Value    *ModelExpr     `json:"value,omitempty"`
	// Body is the statement list of a for-of loop or of an if statement's
	// consequence (a block's statements, or the single statement), read
	// like a function body (version 4).
	Body []ModelStatement `json:"body,omitempty"`
	// Else is an if statement's alternative (version 5): a block's
	// statements or the single statement; an `else if` is a nested if.
	Else []ModelStatement `json:"else_body,omitempty"`
	// Catch and Finally are a try statement's catch and finally blocks
	// (row B-try; Body is the try block). A catch parameter is not kept.
	Catch   []ModelStatement `json:"catch_body,omitempty"`
	Finally []ModelStatement `json:"finally_body,omitempty"`
	// Statics are a class's static members in order (version 4): a plain
	// static method is a function statement (Name, Value); a static field,
	// accessor, async or generator method, computed name or static block
	// is an unknown statement with the Name when it has a plain one, so a
	// consumer knows a static member of that name exists and is not read.
	Statics []ModelStatement `json:"statics,omitempty"`
}

// ModelBinding is one entry of an object pattern: `key` binds Name = Key,
// `key: name` binds Name. Name is "" for an entry that is not a plain
// binding (a default value, a nested pattern); a rest entry `...r` has an
// empty Key and Name r.
type ModelBinding struct {
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

func modelStatementsValid(list []ModelStatement, depth int) bool {
	for i := range list {
		if !modelBodyStatements[list[i].Kind] || !list[i].valid(depth) {
			return false
		}
	}
	return true
}

// staticsValid checks a class's static members: plain methods as
// function statements, everything else unknown.
func staticsValid(list []ModelStatement, depth int) bool {
	for i := range list {
		s := &list[i]
		switch s.Kind {
		case StatementFunction:
			if s.Name == "" || s.Value == nil || s.Value.Kind != ModelFunction || !s.Value.valid(depth) {
				return false
			}
		case StatementUnknown:
		default:
			return false
		}
	}
	return true
}

func (s *ModelStatement) valid(depth int) bool {
	switch s.Kind {
	case StatementExpression:
		return s.Value.valid(depth)
	case StatementClass:
		if !(s.Name != "" || s.Default) || s.Decorators < 0 || (s.Extends != nil && !s.Extends.valid(depth)) || !staticsValid(s.Statics, depth) {
			return false
		}
		for i := range s.DecoratorList {
			if !s.DecoratorList[i].valid(depth + 1) {
				return false
			}
		}
		for i := range s.Members {
			if !s.Members[i].valid(depth) {
				return false
			}
		}
		return true
	case StatementEnum:
		return s.Name != "" && s.EnumMembers != nil && enumMembersValid(s.EnumMembers, depth)
	case StatementForOf:
		return modelKeyword(s.Keyword) && s.Name != "" && s.Value.valid(depth) && modelStatementsValid(s.Body, depth+1)
	case StatementIf:
		return s.Value.valid(depth) && modelStatementsValid(s.Body, depth+1) && modelStatementsValid(s.Else, depth+1)
	case StatementTry:
		return modelStatementsValid(s.Body, depth+1) && modelStatementsValid(s.Catch, depth+1) && modelStatementsValid(s.Finally, depth+1)
	case StatementFunction:
		return (s.Name != "" || s.Default) && s.Value != nil && s.Value.Kind == ModelFunction && s.Value.valid(depth)
	case StatementDestructure:
		return modelKeyword(s.Keyword) && s.Bindings != nil && s.Value.valid(depth)
	case StatementDeclaration:
		return modelKeyword(s.Keyword) && s.Name != "" && (s.Value == nil || s.Value.valid(depth))
	case StatementReturn:
		return s.Value == nil || s.Value.valid(depth)
	case StatementUnknown:
		return true
	}
	return false
}

// expressionStatement keeps a module-level expression statement whose
// expression, after wrappers, is a call or an `=` assignment (row S3):
// `X.init(...)`, `A.belongsTo(B)`, `module.exports = ...`,
// `x.associate = ...`. Everything else is row SD.
func (s *ModelSyntax) expressionStatement(n *parse.Node) {
	children := modelChildren(n)
	if len(children) != 1 {
		tagModel("SD", n.Kind)
		return
	}
	inner := modelUnwrap(children[0])
	if inner == nil || (inner.Kind != "call_expression" && inner.Kind != "assignment_expression") {
		tagModel("SD", n.Kind)
		return
	}
	v := reduceModel(children[0], 0)
	st := ModelStatement{Kind: StatementExpression, Span: span(n), Value: &v}
	s.Statements = append(s.Statements, st)
	tagModel("S3", st)
}

// class records a module-level class declaration (row S4). holder is the
// export statement when there is one: decorators written before `export`
// belong to it.
func (s *ModelSyntax) class(n, holder *parse.Node, exported, isDefault bool) {
	st := classStatement(n, holder, 0)
	st.Exported, st.Default = exported, isDefault
	s.Statements = append(s.Statements, st)
	tagModel("S4", st)
}

// function records a module-level function declaration, plain, `export`
// or `export default` (row S6, version 3): its name and its value as a
// function expression (parameters and body statements). Async and
// generator functions are not read (row SD), as for function expressions.
// holder is the export statement when there is one.
func (s *ModelSyntax) function(n, holder *parse.Node, exported, isDefault bool) {
	var v ModelExpr
	v.Span = span(n)
	if modelHasToken(n, "async") || !modelFunction(&v, n, 0) {
		tagModel("SD", n.Kind)
		return
	}
	st := ModelStatement{Kind: StatementFunction, Span: span(holder), Name: v.Name, Exported: exported, Default: isDefault, Value: &v}
	s.Statements = append(s.Statements, st)
	tagModel("S6", st)
}

// classStatement reads a class: name, abstract, the extends expression, the
// decorator count, the static members (version 4) and the class decorators
// and instance members (version 5).
func classStatement(n, holder *parse.Node, depth int) ModelStatement {
	st := ModelStatement{Kind: StatementClass, Span: span(holder), Abstract: n.Kind == "abstract_class_declaration"}
	if name := n.Fields["name"]; name != nil {
		st.Name = name.Text
	}
	countDecorators := func(p *parse.Node) {
		if p == nil {
			return
		}
		for _, c := range p.Children {
			if c.Kind == "decorator" {
				st.Decorators++
			}
		}
	}
	if holder != n {
		countDecorators(holder)
	}
	countDecorators(n)
	if holder != n {
		st.DecoratorList = append(st.DecoratorList, classDecorators(holder, depth)...)
	}
	st.DecoratorList = append(st.DecoratorList, classDecorators(n, depth)...)
	if body := n.Fields["body"]; body != nil {
		countDecorators(body)
		for _, member := range body.Children {
			countDecorators(member)
			st.Statics = append(st.Statics, staticMember(member, depth)...)
		}
		st.Members = classMembers(body, depth)
	}
	for _, c := range n.Children {
		if c.Kind != "class_heritage" {
			continue
		}
		var value *parse.Node
		for _, h := range modelChildren(c) {
			if h.Kind == "extends_clause" {
				value = h.Fields["value"]
				break
			}
			if h.Kind != "implements_clause" && value == nil {
				// JavaScript: class_heritage holds the expression itself.
				value = h
			}
		}
		if value != nil {
			e := reduceModel(value, depth+1)
			st.Extends = &e
		}
	}
	return st
}

// staticMember reads one class body member for Statics (version 4): a
// static method that is not async, a generator or an accessor, with a
// plain name, is a function statement; any other static member (a field,
// an accessor, a computed name, a static block) is an unknown statement
// carrying its plain name, if any. Instance members are not kept.
func staticMember(member *parse.Node, depth int) []ModelStatement {
	switch member.Kind {
	case "method_definition", "field_definition", "public_field_definition", "class_static_block":
	default:
		return nil
	}
	if member.Kind != "class_static_block" && !modelHasToken(member, "static") {
		return nil
	}
	name := ""
	if key := member.Fields["name"]; key != nil && key.Kind == "property_identifier" {
		name = key.Text
	} else if key := member.Fields["property"]; key != nil && key.Kind == "property_identifier" {
		name = key.Text
	}
	if member.Kind == "method_definition" && name != "" && !modelHasToken(member, "get") && !modelHasToken(member, "set") {
		v := ModelExpr{Span: span(member)}
		if !modelHasToken(member, "async") && modelFunction(&v, member, depth+1) {
			v.Name = name
			return []ModelStatement{{Kind: StatementFunction, Span: span(member), Name: name, Value: &v}}
		}
	}
	return []ModelStatement{{Kind: StatementUnknown, Span: span(member), Name: name}}
}

// forOf records a module-level `for (const|let|var <name> of <iterable>)
// <statement>` (row S7, version 4): the binding, the iterable and the
// body statements, read like a function body. `for await`, `for ... in`
// and loops whose binding is not an identifier are row SD.
func (s *ModelSyntax) forOf(n *parse.Node) {
	left, right, body := n.Fields["left"], n.Fields["right"], n.Fields["body"]
	keyword := ""
	for _, k := range []string{"const", "let", "var"} {
		if modelHasToken(n, k) {
			keyword = k
		}
	}
	if !modelHasToken(n, "of") || modelHasToken(n, "await") || keyword == "" || left == nil || left.Kind != "identifier" || right == nil || body == nil {
		tagModel("SD", n.Kind)
		return
	}
	v := reduceModel(right, 0)
	st := ModelStatement{Kind: StatementForOf, Span: span(n), Keyword: keyword, Name: left.Text, Value: &v, Body: loopBody(body, 1)}
	s.Statements = append(s.Statements, st)
	tagModel("S7", st)
}

// loopBody reads the body of a loop or an if statement: a block's
// statements, or the single statement.
func loopBody(body *parse.Node, depth int) []ModelStatement {
	if body.Kind == "statement_block" {
		return bodyStatements(body, depth)
	}
	return bodyStatement(body, depth)
}

// destructure reads an object-pattern declarator (rows S5 and B-destructure).
func destructure(d *parse.Node, keyword string, exported bool, depth int) ModelStatement {
	value := reduceModel(d.Fields["value"], depth+1)
	st := ModelStatement{Kind: StatementDestructure, Span: span(d), Keyword: keyword, Exported: exported, Bindings: []ModelBinding{}, Value: &value}
	for _, p := range modelChildren(d.Fields["name"]) {
		switch p.Kind {
		case "shorthand_property_identifier_pattern":
			st.Bindings = append(st.Bindings, ModelBinding{Key: p.Text, Name: p.Text})
		case "pair_pattern":
			key, _ := modelKey(p.Fields["key"])
			b := ModelBinding{Key: key}
			if v := p.Fields["value"]; v != nil && v.Kind == "identifier" && key != "" {
				b.Name = v.Text
			}
			st.Bindings = append(st.Bindings, b)
		case "object_assignment_pattern":
			st.Bindings = append(st.Bindings, ModelBinding{Key: text(p.Fields["left"])})
		case "rest_pattern":
			b := ModelBinding{}
			if inner := modelChildren(p); len(inner) == 1 && inner[0].Kind == "identifier" {
				b.Name = inner[0].Text
			}
			st.Bindings = append(st.Bindings, b)
		default:
			st.Bindings = append(st.Bindings, ModelBinding{})
		}
	}
	return st
}

// bodyStatements reads a function body block as an ordered statement list:
// declarations, destructuring declarations, expression statements, class
// declarations, returns and if statements (version 4; with their else
// branches from version 5);
// every other statement (other conditionals, loops, nested blocks,
// function declarations) is an unknown entry, so a consumer sees that
// something it cannot read sits between two statements.
func bodyStatements(block *parse.Node, depth int) []ModelStatement {
	out := []ModelStatement{}
	for _, n := range modelChildren(block) {
		out = append(out, bodyStatement(n, depth)...)
	}
	return out
}

// bodyStatement reads one statement of a captured body (a declaration
// yields one statement per declarator).
func bodyStatement(n *parse.Node, depth int) []ModelStatement {
	var out []ModelStatement
	if depth > modelMaxDepth {
		st := ModelStatement{Kind: StatementUnknown, Span: span(n)}
		tagModel("B-unknown", st)
		return append(out, st)
	}
	switch n.Kind {
	case "lexical_declaration", "variable_declaration":
		keyword := declarationKeyword(n)
		for _, d := range n.Children {
			if d.Kind != "variable_declarator" {
				continue
			}
			name, value := d.Fields["name"], d.Fields["value"]
			switch {
			case name != nil && name.Kind == "object_pattern" && value != nil:
				st := destructure(d, keyword, false, depth)
				out = append(out, st)
				tagModel("B-destructure", st)
			case name != nil && name.Kind == "identifier":
				st := ModelStatement{Kind: StatementDeclaration, Span: span(d), Keyword: keyword, Name: name.Text}
				if value != nil {
					v := reduceModel(value, depth+1)
					st.Value = &v
				}
				out = append(out, st)
				tagModel("B-declaration", st)
			default:
				st := ModelStatement{Kind: StatementUnknown, Span: span(d)}
				out = append(out, st)
				tagModel("B-unknown", st)
			}
		}
	case "expression_statement":
		children := modelChildren(n)
		if len(children) != 1 {
			st := ModelStatement{Kind: StatementUnknown, Span: span(n)}
			tagModel("B-unknown", st)
			return append(out, st)
		}
		v := reduceModel(children[0], depth+1)
		st := ModelStatement{Kind: StatementExpression, Span: span(n), Value: &v}
		out = append(out, st)
		tagModel("B-expression", st)
	case "return_statement":
		st := ModelStatement{Kind: StatementReturn, Span: span(n)}
		if values := modelChildren(n); len(values) == 1 {
			v := reduceModel(values[0], depth+1)
			st.Value = &v
		} else if len(values) > 1 {
			v := modelUnknown(n, nil)
			st.Value = &v
		}
		out = append(out, st)
		tagModel("B-return", st)
	case "class_declaration", "abstract_class_declaration":
		st := classStatement(n, n, depth)
		out = append(out, st)
		tagModel("B-class", st)
	case "if_statement":
		cond, then, alternative := n.Fields["condition"], n.Fields["consequence"], n.Fields["alternative"]
		var otherwise *parse.Node
		if alternative != nil {
			if inner := modelChildren(alternative); len(inner) == 1 {
				otherwise = inner[0]
			}
		}
		if cond == nil || then == nil || (alternative != nil && otherwise == nil) {
			st := ModelStatement{Kind: StatementUnknown, Span: span(n)}
			tagModel("B-unknown", st)
			return append(out, st)
		}
		v := reduceModel(cond, depth+1)
		st := ModelStatement{Kind: StatementIf, Span: span(n), Value: &v, Body: loopBody(then, depth+1)}
		if otherwise != nil {
			// Version 5: the else branch; `else if` is a nested if.
			st.Else = loopBody(otherwise, depth+1)
		}
		out = append(out, st)
		tagModel("B-if", st)
	case "try_statement":
		// Row B-try (T018g6 Revision 3 addendum 2): nest-admin's useFactory
		// parses a logger option in a try block before its return. Kept
		// only in the TypeORM gate rows, as Async is.
		body, handler, finalizer := n.Fields["body"], n.Fields["handler"], n.Fields["finalizer"]
		if body == nil || (handler != nil && handler.Fields["body"] == nil) || (finalizer != nil && finalizer.Fields["body"] == nil) {
			st := ModelStatement{Kind: StatementUnknown, Span: span(n)}
			tagModel("B-unknown", st)
			return append(out, st)
		}
		st := ModelStatement{Kind: StatementTry, Span: span(n), Body: bodyStatements(body, depth+1)}
		if handler != nil {
			st.Catch = bodyStatements(handler.Fields["body"], depth+1)
		}
		if finalizer != nil {
			st.Finally = bodyStatements(finalizer.Fields["body"], depth+1)
		}
		out = append(out, st)
		tagModel("B-try", st)
	default:
		st := ModelStatement{Kind: StatementUnknown, Span: span(n)}
		out = append(out, st)
		tagModel("B-unknown", st)
	}
	return out
}

// sequelizeDialects are the dialect names Sequelize 6 accepts (row G7).
var sequelizeDialects = map[string]bool{
	"postgres": true, "sqlite": true, "mysql": true, "mariadb": true, "mssql": true,
	"db2": true, "snowflake": true, "oracle": true,
}

// sequelizeModule matches `sequelize` and its subpaths, never
// `sequelize-typescript` or other packages that share the prefix.
func sequelizeModule(module string) bool {
	return module == "sequelize" || strings.HasPrefix(module, "sequelize/")
}

// sequelizeGate returns the first Sequelize gate row a file passes:
//
//   - G4 the file imports `sequelize` (value or type import);
//   - G5 it calls `require("sequelize")`;
//   - G6 it has a module-level statement `<e>.init(<object>, <object>)` or a
//     call `<e>.define(<string>, <object>, ...)` anywhere (CommonJS
//     factories import nothing from Sequelize);
//   - G7 it has an object property `dialect: "<Sequelize dialect>"` (the
//     options object a factory-style instance reads, Gladys's
//     `config/config.js`);
//   - G8 its `module.exports` or default export is a function that returns
//     `<binding>[<expression>]`, the binding a relative `require` or import
//     (the environment selector of open point O2, Gladys's
//     `utils/getConfig.js`).
//
// Each tree walk runs only when a substring precheck on the file text could
// succeed, so a file outside the gate costs a few string searches.
func sequelizeGate(root *parse.Node, f Facts) string {
	for _, imp := range f.Imports {
		if sequelizeModule(imp.Module) {
			return "G4"
		}
	}
	if root == nil {
		return ""
	}
	src := root.Text
	wantRequire := strings.Contains(src, "require") && strings.Contains(src, "sequelize")
	wantDefine := strings.Contains(src, "define")
	wantDialect := strings.Contains(src, "dialect")
	var require, define, dialect bool
	if wantRequire || wantDefine || wantDialect {
		var walk func(n *parse.Node)
		walk = func(n *parse.Node) {
			if require || n == nil {
				return
			}
			switch n.Kind {
			case "call_expression":
				callee, args := n.Fields["function"], modelChildren(n.Fields["arguments"])
				if wantRequire && callee != nil && callee.Kind == "identifier" && callee.Text == "require" && len(args) >= 1 && args[0].Kind == "string" {
					if v, ok := jsString(args[0].Text); ok && sequelizeModule(v) {
						require = true
						return
					}
				}
				if wantDefine && !define && modelMethodCall(n, "define") && len(args) >= 2 && modelUnwrap(args[0]).Kind == "string" && modelUnwrap(args[1]).Kind == "object" {
					define = true
				}
			case "pair":
				if wantDialect && !dialect {
					key, ok := modelKey(n.Fields["key"])
					if value := modelUnwrap(n.Fields["value"]); ok && key == "dialect" && value != nil && value.Kind == "string" {
						v, _ := jsString(value.Text)
						dialect = sequelizeDialects[v]
					}
				}
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(root)
	}
	switch {
	case require:
		return "G5"
	case define || (strings.Contains(src, "init") && moduleInitStatement(root)):
		return "G6"
	case dialect:
		return "G7"
	case strings.Contains(src, "[") && environmentSelector(root, f):
		return "G8"
	case strings.Contains(src, "type") && attributeHelperModule(root):
		return "G9"
	}
	return ""
}

// attributeHelperModule reports row G9 (T018g3 Revision 2, open point O4):
// a module-level function declaration (plain or exported), `export
// default F`, `module.exports = F` or `exports.<name> = F`, F a non-async,
// non-generator function or arrow, whose body is a single `return <object
// literal>` (or an arrow's object literal body), and that object or one of
// its property values is an object literal with a `type` property whose
// value is `<p>.<X>` or `<p>.<X>(...)`, p one of F's parameters: an
// attribute helper such as opencollective-api's
// `CustomDataTypes(DataTypes).currency`, which imports nothing from
// Sequelize. The gate is syntax only; the linker decides whether a model
// reaches the helper.
func attributeHelperModule(root *parse.Node) bool {
	var candidates []*parse.Node
	for _, n := range root.Children {
		switch n.Kind {
		case "function_declaration":
			candidates = append(candidates, n)
		case "export_statement":
			if d := n.Fields["declaration"]; d != nil && d.Kind == "function_declaration" {
				candidates = append(candidates, d)
			} else if v := modelUnwrap(n.Fields["value"]); v != nil && modelHasToken(n, "default") {
				candidates = append(candidates, v)
			}
		case "expression_statement":
			if children := modelChildren(n); len(children) == 1 {
				if a := modelUnwrap(children[0]); a != nil && a.Kind == "assignment_expression" {
					left := compact(text(a.Fields["left"]))
					if left == "module.exports" || strings.HasPrefix(left, "exports.") || strings.HasPrefix(left, "module.exports.") {
						candidates = append(candidates, modelUnwrap(a.Fields["right"]))
					}
				}
			}
		}
	}
	for _, fn := range candidates {
		if helperFunction(fn) {
			return true
		}
	}
	return false
}

// helperFunction reports a function or arrow node of G9's shape.
func helperFunction(fn *parse.Node) bool {
	if fn == nil || modelHasToken(fn, "async") || modelHasToken(fn, "*") {
		return false
	}
	switch fn.Kind {
	case "function_declaration", "function_expression", "function", "arrow_function":
	default:
		return false
	}
	params := map[string]bool{}
	if p := fn.Fields["parameter"]; p != nil {
		params[modelParam(p)] = true
	}
	for _, p := range modelChildren(fn.Fields["parameters"]) {
		params[modelParam(p)] = true
	}
	delete(params, "")
	body := fn.Fields["body"]
	if body == nil {
		return false
	}
	var returned *parse.Node
	if body.Kind == "statement_block" {
		statements := modelChildren(body)
		if len(statements) != 1 || statements[0].Kind != "return_statement" {
			return false
		}
		values := modelChildren(statements[0])
		if len(values) != 1 {
			return false
		}
		returned = modelUnwrap(values[0])
	} else {
		returned = modelUnwrap(body)
	}
	if returned == nil || returned.Kind != "object" {
		return false
	}
	typed := func(obj *parse.Node) bool {
		for _, p := range modelChildren(obj) {
			if p.Kind != "pair" {
				continue
			}
			key, ok := modelKey(p.Fields["key"])
			v := modelUnwrap(p.Fields["value"])
			if !ok || key != "type" || v == nil {
				continue
			}
			if v.Kind == "call_expression" {
				v = modelUnwrap(v.Fields["function"])
			}
			if v != nil && v.Kind == "member_expression" {
				o := v.Fields["object"]
				if o != nil && o.Kind == "identifier" && params[o.Text] {
					return true
				}
			}
		}
		return false
	}
	if typed(returned) {
		return true
	}
	for _, p := range modelChildren(returned) {
		if p.Kind != "pair" {
			continue
		}
		if v := modelUnwrap(p.Fields["value"]); v != nil && v.Kind == "object" && typed(v) {
			return true
		}
	}
	return false
}

// modelMethodCall reports a call `<e>.<method>(...)` (not optional).
func modelMethodCall(n *parse.Node, method string) bool {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "member_expression" || modelOptional(n) || modelOptional(callee) {
		return false
	}
	p := callee.Fields["property"]
	return p != nil && p.Kind == "property_identifier" && p.Text == method
}

// moduleInitStatement reports a module-level `<e>.init(<object>, <object>)`.
func moduleInitStatement(root *parse.Node) bool {
	for _, n := range root.Children {
		if n.Kind != "expression_statement" {
			continue
		}
		children := modelChildren(n)
		if len(children) != 1 {
			continue
		}
		call := modelUnwrap(children[0])
		if call == nil || call.Kind != "call_expression" || !modelMethodCall(call, "init") {
			continue
		}
		args := modelChildren(call.Fields["arguments"])
		if len(args) == 2 && modelUnwrap(args[0]).Kind == "object" && modelUnwrap(args[1]).Kind == "object" {
			return true
		}
	}
	return false
}

// environmentSelector reports row G8: `module.exports = F` or `export
// default F` at module level, F a non-async arrow or function expression
// that returns `<id>[<expression>]`, id bound at module level by
// `require("./...")` / `require("../...")` or imported from a relative module.
func environmentSelector(root *parse.Node, f Facts) bool {
	relative := func(m string) bool { return strings.HasPrefix(m, "./") || strings.HasPrefix(m, "../") }
	bound := map[string]bool{}
	for _, imp := range f.Imports {
		if relative(imp.Module) {
			for local := range imp.Bindings {
				bound[local] = true
			}
		}
	}
	var exported []*parse.Node
	for _, n := range root.Children {
		switch n.Kind {
		case "lexical_declaration", "variable_declaration":
			for _, d := range n.Children {
				name, value := d.Fields["name"], modelUnwrap(d.Fields["value"])
				if d.Kind != "variable_declarator" || name == nil || name.Kind != "identifier" || value == nil || value.Kind != "call_expression" {
					continue
				}
				callee, args := value.Fields["function"], modelChildren(value.Fields["arguments"])
				if callee != nil && callee.Kind == "identifier" && callee.Text == "require" && len(args) == 1 && args[0].Kind == "string" {
					if v, ok := jsString(args[0].Text); ok && relative(v) {
						bound[name.Text] = true
					}
				}
			}
		case "expression_statement":
			if children := modelChildren(n); len(children) == 1 {
				if a := modelUnwrap(children[0]); a != nil && a.Kind == "assignment_expression" && compact(text(a.Fields["left"])) == "module.exports" {
					exported = append(exported, modelUnwrap(a.Fields["right"]))
				}
			}
		case "export_statement":
			if modelHasToken(n, "default") && n.Fields["value"] != nil {
				exported = append(exported, modelUnwrap(n.Fields["value"]))
			}
		}
	}
	returnsIndex := func(e *parse.Node) bool {
		e = modelUnwrap(e)
		if e == nil || e.Kind != "subscript_expression" {
			return false
		}
		o := e.Fields["object"]
		return o != nil && o.Kind == "identifier" && bound[o.Text]
	}
	for _, fn := range exported {
		if fn == nil || (fn.Kind != "arrow_function" && fn.Kind != "function_expression" && fn.Kind != "function") || modelHasToken(fn, "async") {
			continue
		}
		body := fn.Fields["body"]
		if body == nil {
			continue
		}
		if body.Kind != "statement_block" {
			if returnsIndex(body) {
				return true
			}
			continue
		}
		for _, st := range modelChildren(body) {
			if values := modelChildren(st); st.Kind == "return_statement" && len(values) == 1 && returnsIndex(values[0]) {
				return true
			}
		}
	}
	return false
}
