package typescript

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the TypeScript and JavaScript syntax that ORM model
// linking reads (ADR 0059, T018g2; widened to version 2 by ADR 0061,
// T018g3): the module-level declarations, statements and the default
// export of Drizzle schema and config files and of Sequelize model, instance
// and config modules, as an ordered, reduced expression tree. It records
// syntax only (rule 31). Every decision that needs resolution (is this callee
// pgTable, is this receiver the Sequelize instance, does this import or
// require reach a schema file) stays at link time. The payload table in docs/lang/typescript-models.md
// names every capture branch below by its row id, and
// TestModelPayloadContract keeps code and table 1:1.

// ModelSyntaxVersion is the current model payload version. A cached payload
// of any other version is a cache miss. Version 2 (ADR 0061) added the
// Sequelize gate rows, module-level statements, function bodies and the new,
// function, assignment and index expressions. Version 3 (T018g3 Revision 2,
// open point O4) added gate row G9 (attribute helper modules) and
// module-level function declarations (row S6). Version 4 (T018g3
// Revision 3) added module-level for-of loops (row S7),
// if statements in bodies (row B-if) and classes' static members. Version 5
// (ADR 0064, T018g6) added the TypeORM gate rows G10-G12, class
// decorator lists, instance members with declared types (rows M-* and T-*),
// module-level enums (row S8), array initializers in row S1, else branches
// of B-if and conditional expressions (row E-conditional).
const ModelSyntaxVersion = 5

// modelMaxDepth bounds the reduced tree so a pathological expression cannot
// grow the cache without limit; deeper nodes become unknown. Drizzle schemas
// nest about ten levels (table, columns object, builder chain, arguments).
const modelMaxDepth = 48

// modelTag is a test-only hook: every capture branch reports the payload row
// it serves and the record it produced. It is nil in production.
var modelTag func(row string, record any)

func tagModel(row string, record any) {
	if modelTag != nil {
		modelTag(row, record)
	}
}

// ModelSyntax is the cached model syntax of one file. It exists only for
// files that pass the gate (ModelGate); other files carry no Models field.
type ModelSyntax struct {
	Version int `json:"version"`
	// Declarations are the module-level variable declarations whose
	// initializer is a call, a tagged template, an object literal, a `new`
	// expression, an index into a call's result (version 4), an array or a
	// string literal or a conditional expression (version 5), in source
	// order.
	Declarations []ModelDeclaration `json:"declarations"`
	// Default is the `export default` expression, when the file has one.
	Default *ModelDefault `json:"default,omitempty"`
	// Statements are the module-level expression statements (calls and
	// assignments), class declarations and destructuring declarations, in
	// source order (version 2). They are a separate list so the version 1
	// fields keep their meaning; spans order the two lists against each other.
	Statements []ModelStatement `json:"statements,omitempty"`
}

// ModelDeclaration is one module-level `const`, `let` or `var` declarator.
// Exported is true only for the `export const` form; `export { x }` clauses
// stay in Facts.ExportClauses and are resolved at link time.
type ModelDeclaration struct {
	Name     string        `json:"name"`
	Keyword  string        `json:"keyword"`
	Exported bool          `json:"exported,omitempty"`
	Span     contract.Span `json:"span"`
	Value    ModelExpr     `json:"value"`
}

// ModelDefault is the expression of an `export default` statement, with the
// statement's span. A default-exported function or class is unknown.
type ModelDefault struct {
	Span  contract.Span `json:"span"`
	Value ModelExpr     `json:"value"`
}

// Model expression kinds. Each kind fills only the fields its comment names;
// TypeScript wrappers (`as`, `satisfies`, `!`, `<T>x`, parentheses) are
// transparent and never appear.
const (
	ModelCall       = "call"        // Callee, Args
	ModelMember     = "member"      // Object, Name (the property)
	ModelIdentifier = "identifier"  // Name
	ModelString     = "string"      // Value (decoded)
	ModelNumber     = "number"      // Value (source text, with a leading - or +)
	ModelBoolean    = "boolean"     // Value ("true" or "false")
	ModelNull       = "null"        // nothing
	ModelTemplate   = "template"    // Strings (cooked), Substitutions
	ModelTagged     = "tagged"      // Tag, Strings (cooked), Substitutions
	ModelObject     = "object"      // Entries
	ModelArray      = "array"       // Elements
	ModelSpread     = "spread"      // Object (the spread operand)
	ModelArrow      = "arrow"       // Params, Body, Block, Statements (block bodies)
	ModelNew        = "new"         // Callee, Args (version 2)
	ModelFunction   = "function"    // Name (when named), Params, Statements (version 2)
	ModelAssign     = "assign"      // Left, Right: `left = right` (version 2)
	ModelIndex      = "index"       // Object, Index: `object[index]` (version 2)
	ModelBinary     = "binary"      // Value (the operator), Left, Right (version 2)
	ModelUnary      = "unary"       // Value (the operator), Object (the operand) (version 2)
	ModelCond       = "conditional" // Test, Then, Else: `test ? then : else` (version 5)
	ModelUnknown    = "unknown"     // nothing but the span
)

// ModelExpr is a reduced expression that keeps source order. Span is the
// source span of the expression (after removing transparent wrappers).
type ModelExpr struct {
	Kind          string        `json:"kind"`
	Span          contract.Span `json:"span"`
	Name          string        `json:"name,omitempty"`
	Value         string        `json:"value,omitempty"`
	Callee        *ModelExpr    `json:"callee,omitempty"`
	Args          []ModelExpr   `json:"args,omitempty"`
	Object        *ModelExpr    `json:"object,omitempty"`
	Tag           *ModelExpr    `json:"tag,omitempty"`
	Strings       []string      `json:"strings,omitempty"`
	Substitutions []ModelExpr   `json:"substitutions,omitempty"`
	Entries       []ModelEntry  `json:"entries,omitempty"`
	Elements      []ModelExpr   `json:"elements,omitempty"`
	// Params are the arrow's parameter names in order; "" marks a parameter
	// that is not a plain identifier (destructuring, default value, rest).
	Params []string   `json:"params,omitempty"`
	Body   *ModelExpr `json:"body,omitempty"`
	// Block is true when the arrow body is a block: Body is then its single
	// `return` expression, or unknown when the block holds anything else.
	Block bool `json:"block,omitempty"`
	// Statements are the statements of an arrow's block body or a function
	// expression's body, in order (version 2). Block arrows keep Body as in
	// version 1, so a single-return block appears in both.
	Statements []ModelStatement `json:"statements,omitempty"`
	// Left and Right are an assignment's target and value.
	Left  *ModelExpr `json:"left,omitempty"`
	Right *ModelExpr `json:"right,omitempty"`
	// Index is the computed key of `object[index]`.
	Index *ModelExpr `json:"index,omitempty"`
	// Test, Then and Else are a conditional expression's parts (version 5):
	// seerr's options selector returns `isPgsql ? postgresProdConfig :
	// prodConfig`.
	Test *ModelExpr `json:"test,omitempty"`
	Then *ModelExpr `json:"then,omitempty"`
	Else *ModelExpr `json:"else,omitempty"`
	// Async marks an async arrow or function expression (row E-async,
	// T018g6 Revision 3 addendum 2: NestJS's dataSourceFactory). It is kept
	// only in files of the TypeORM gate rows G10 and G11; every other
	// payload holds such a function as unknown, as before.
	Async bool `json:"async,omitempty"`
}

// Object entry kinds, in source order (duplicates kept).
const (
	ModelProperty  = "property"  // Key, Value
	ModelShorthand = "shorthand" // Key, Value (the identifier)
	ModelSpreadIn  = "spread"    // Value (the spread operand)
	ModelMethod    = "method"    // Key, Value (the single returned literal, else unknown)
	ModelOther     = "unknown"   // computed keys, accessors, async or generator methods
)

// ModelEntry is one object literal entry.
type ModelEntry struct {
	Kind  string        `json:"kind"`
	Key   string        `json:"key,omitempty"`
	Span  contract.Span `json:"span"`
	Value ModelExpr     `json:"value"`
}

// modelKinds and modelEntryKinds are the kinds Valid accepts.
var modelKinds = map[string]bool{
	ModelCall: true, ModelMember: true, ModelIdentifier: true, ModelString: true, ModelNumber: true,
	ModelBoolean: true, ModelNull: true, ModelTemplate: true, ModelTagged: true, ModelObject: true,
	ModelArray: true, ModelSpread: true, ModelArrow: true, ModelNew: true, ModelFunction: true,
	ModelAssign: true, ModelIndex: true, ModelBinary: true, ModelUnary: true, ModelCond: true, ModelUnknown: true,
}

var modelEntryKinds = map[string]bool{ModelProperty: true, ModelShorthand: true, ModelSpreadIn: true, ModelMethod: true, ModelOther: true}

// Valid rejects payloads of another version and structurally incomplete
// trees, so a damaged cache entry is a cache miss rather than a wrong fact.
func (s *ModelSyntax) Valid() bool {
	if s == nil || s.Version != ModelSyntaxVersion || s.Declarations == nil {
		return false
	}
	for _, d := range s.Declarations {
		if d.Name == "" || !modelKeyword(d.Keyword) || !d.Value.valid(0) {
			return false
		}
	}
	for i := range s.Statements {
		if !modelModuleStatements[s.Statements[i].Kind] || !s.Statements[i].valid(0) {
			return false
		}
	}
	return s.Default == nil || s.Default.Value.valid(0)
}

func modelKeyword(k string) bool { return k == "const" || k == "let" || k == "var" }

func (e *ModelExpr) valid(depth int) bool {
	if e == nil || !modelKinds[e.Kind] || depth > modelMaxDepth+1 {
		return false
	}
	all := func(list []ModelExpr) bool {
		for i := range list {
			if !list[i].valid(depth + 1) {
				return false
			}
		}
		return true
	}
	switch e.Kind {
	case ModelCall, ModelNew:
		return e.Callee.valid(depth+1) && all(e.Args)
	case ModelFunction:
		return modelStatementsValid(e.Statements, depth+1)
	case ModelAssign:
		return e.Left.valid(depth+1) && e.Right.valid(depth+1)
	case ModelBinary:
		return e.Value != "" && e.Left.valid(depth+1) && e.Right.valid(depth+1)
	case ModelUnary:
		return e.Value != "" && e.Object.valid(depth+1)
	case ModelIndex:
		return e.Object.valid(depth+1) && e.Index.valid(depth+1)
	case ModelCond:
		return e.Test.valid(depth+1) && e.Then.valid(depth+1) && e.Else.valid(depth+1)
	case ModelMember:
		return e.Name != "" && e.Object.valid(depth+1)
	case ModelIdentifier:
		return e.Name != ""
	case ModelNumber, ModelBoolean:
		return e.Value != ""
	case ModelTemplate:
		return len(e.Strings) == len(e.Substitutions)+1 && all(e.Substitutions)
	case ModelTagged:
		return len(e.Strings) == len(e.Substitutions)+1 && e.Tag.valid(depth+1) && all(e.Substitutions)
	case ModelArray:
		return all(e.Elements)
	case ModelSpread:
		return e.Object.valid(depth + 1)
	case ModelArrow:
		return e.Body.valid(depth+1) && (e.Block || len(e.Statements) == 0) && modelStatementsValid(e.Statements, depth+1)
	case ModelObject:
		for i := range e.Entries {
			if !modelEntryKinds[e.Entries[i].Kind] || !e.Entries[i].Value.valid(depth+1) {
				return false
			}
		}
	}
	return true
}

// modelConfigNames are drizzle-kit's default config file names
// (drizzleConfigFromFile tries .ts, .js, then .json; JSON is not TypeScript).
var modelConfigNames = map[string]bool{"drizzle.config.ts": true, "drizzle.config.js": true}

// ModelGate reports whether a file gets a model payload: it imports a
// `drizzle-orm/<dialect>-core` module (row G1, where table, enum, column and
// customType builders live), imports `drizzle-kit` (row G2, configs), has
// drizzle-kit's default config name (row G3), or passes one of the Sequelize
// rows G4-G9 (sequelizeGate) or one of the TypeORM rows G10-G12
// (typeormGate, version 5). It returns the first row that opened the gate,
// or "".
func ModelGate(file string, root *parse.Node, f Facts) string {
	for _, imp := range f.Imports {
		if modelDialectModule(imp.Module) {
			return "G1"
		}
	}
	for _, imp := range f.Imports {
		if imp.Module == "drizzle-kit" {
			return "G2"
		}
	}
	if modelConfigNames[path.Base(file)] {
		return "G3"
	}
	if gate := sequelizeGate(root, f); gate != "" {
		return gate
	}
	return typeormGate(file, root, f)
}

// modelDialectModule matches drizzle-orm/<dialect>-core and its subpaths.
func modelDialectModule(module string) bool {
	rest, ok := strings.CutPrefix(module, "drizzle-orm/")
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(rest, "/")
	dialect, ok := strings.CutSuffix(first, "-core")
	if !ok || dialect == "" {
		return false
	}
	for _, c := range dialect {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// CaptureModels returns the model payload of a file, or nil when the file
// does not pass ModelGate. It only reads the tree; no resolution happens here.
func CaptureModels(file string, root *parse.Node, f Facts) *ModelSyntax {
	gate := ModelGate(file, root, f)
	if gate == "" || root == nil {
		tagModel("GD", file)
		return nil
	}
	s := &ModelSyntax{Version: ModelSyntaxVersion, Declarations: []ModelDeclaration{}}
	tagModel(gate, *s)
	for _, n := range root.Children {
		if e := moduleEnum(n); e != nil {
			s.enum(e, n, n != e)
			continue
		}
		if gate == "G12" {
			// An enum module's payload holds only its enums (row G12).
			tagModel("SD", n.Kind)
			continue
		}
		switch n.Kind {
		case "lexical_declaration", "variable_declaration":
			s.declarations(n, false)
		case "expression_statement":
			s.expressionStatement(n)
		case "class_declaration", "abstract_class_declaration":
			s.class(n, n, false, false)
		case "function_declaration":
			s.function(n, n, false, false)
		case "for_in_statement":
			s.forOf(n)
		case "export_statement":
			if d := n.Fields["declaration"]; d != nil && (d.Kind == "lexical_declaration" || d.Kind == "variable_declaration") && !modelHasToken(n, "default") {
				s.declarations(d, true)
				continue
			}
			if d := n.Fields["declaration"]; d != nil && (d.Kind == "class_declaration" || d.Kind == "abstract_class_declaration") {
				s.class(d, n, true, modelHasToken(n, "default"))
				if !modelHasToken(n, "default") {
					continue
				}
			}
			if d := n.Fields["declaration"]; d != nil && d.Kind == "function_declaration" {
				s.function(d, n, true, modelHasToken(n, "default"))
				if !modelHasToken(n, "default") {
					continue
				}
			}
			if v := n.Fields["value"]; v != nil && v.Kind == "class" && modelHasToken(n, "default") {
				s.class(v, n, true, true)
			}
			if modelHasToken(n, "default") {
				value := n.Fields["value"]
				var e ModelExpr
				if value != nil {
					e = reduceModel(value, 0)
				} else {
					e = modelUnknown(n.Fields["declaration"], n)
				}
				s.Default = &ModelDefault{Span: span(n), Value: e}
				tagModel("S2", *s.Default)
				continue
			}
			tagModel("SD", n.Kind)
		default:
			tagModel("SD", n.Kind)
		}
	}
	if gate != "G10" && gate != "G11" {
		// Rows E-async, E-await and B-try are TypeORM's (G10, G11): every
		// other payload keeps those forms unknown, as before them.
		s.withoutTypeORMForms()
	}
	return s
}

// declarationKeyword is the keyword of a lexical or variable declaration.
func declarationKeyword(n *parse.Node) string {
	if n.Kind != "lexical_declaration" {
		return "var"
	}
	if modelHasToken(n, "let") {
		return "let"
	}
	return "const"
}

func modelHasToken(n *parse.Node, token string) bool {
	for _, t := range n.Tokens {
		if t == token {
			return true
		}
	}
	return false
}

// declarations keeps the declarators whose initializer, after transparent
// wrappers, is a call (including a tagged template), an object literal, a
// `new` expression, (version 4) an index into a call's result or (version
// 5) an array literal, a string literal or a conditional expression: the
// cheapest syntactic test that keeps every table, enum, customType,
// relations() and config value, every `new Sequelize(...)` instance, the
// sequelize-cli `require(<config>)[env]` options, a TypeORM `entities`
// array, a `registerAs` token const and an options object chosen by a
// conditional (aqualink-app's `dataSourceInfo`, row TO-K8) (row S1). A destructuring declarator is row S5; everything else is
// row SD.
func (s *ModelSyntax) declarations(n *parse.Node, exported bool) {
	keyword := declarationKeyword(n)
	for _, d := range n.Children {
		if d.Kind != "variable_declarator" {
			continue
		}
		name, value := d.Fields["name"], d.Fields["value"]
		inner := modelUnwrap(value)
		if name != nil && name.Kind == "object_pattern" && value != nil {
			s.Statements = append(s.Statements, destructure(d, keyword, exported, 0))
			tagModel("S5", s.Statements[len(s.Statements)-1])
			continue
		}
		if name == nil || name.Kind != "identifier" || inner == nil || (inner.Kind != "call_expression" && inner.Kind != "object" && inner.Kind != "new_expression" && inner.Kind != "array" && inner.Kind != "string" && inner.Kind != "ternary_expression" && !modelIndexedCall(inner)) {
			tagModel("SD", d.Kind)
			continue
		}
		decl := ModelDeclaration{Name: name.Text, Keyword: keyword, Exported: exported, Span: span(d), Value: reduceModel(value, 0)}
		s.Declarations = append(s.Declarations, decl)
		tagModel("S1", decl)
	}
}

// modelIndexedCall reports `<call>[<expression>]` (version 4): the
// sequelize-cli template's `require(<config file>)[env]` options.
func modelIndexedCall(n *parse.Node) bool {
	if n.Kind != "subscript_expression" || modelOptional(n) {
		return false
	}
	o := modelUnwrap(n.Fields["object"])
	return o != nil && o.Kind == "call_expression"
}

// modelUnwrap removes TypeScript-only wrappers and parentheses, which do not
// change the runtime value drizzle-kit reads.
func modelUnwrap(n *parse.Node) *parse.Node {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
			var next *parse.Node
			for _, c := range n.Children {
				if c.Kind != "comment" && !strings.Contains(c.Kind, "type") {
					next = c
					break
				}
			}
			if next == nil {
				return n
			}
			n = next
		default:
			return n
		}
	}
	return nil
}

// modelOptional reports an optional chain (`a?.b`, `f?.()`), whose value may
// be undefined at run time.
func modelOptional(n *parse.Node) bool {
	if n.Fields["optional_chain"] != nil {
		return true
	}
	for _, c := range n.Children {
		if c.Kind == "optional_chain" {
			return true
		}
	}
	return modelHasToken(n, "?.")
}

func modelChildren(n *parse.Node) []*parse.Node {
	var out []*parse.Node
	if n != nil {
		for _, c := range n.Children {
			if c.Kind != "comment" {
				out = append(out, c)
			}
		}
	}
	return out
}

func modelUnknown(n, fallback *parse.Node) ModelExpr {
	if n == nil {
		n = fallback
	}
	e := ModelExpr{Kind: ModelUnknown}
	if n != nil {
		e.Span = span(n)
	}
	tagModel("E-unknown", e)
	return e
}

// reduceModel reduces one expression. Comments are never children of the
// result: every list skips them.
func reduceModel(n *parse.Node, depth int) ModelExpr {
	if n == nil {
		return ModelExpr{Kind: ModelUnknown}
	}
	if unwrapped := modelUnwrap(n); unwrapped != n {
		e := reduceModel(unwrapped, depth)
		tagModel("E-wrap", e)
		return e
	}
	if depth > modelMaxDepth {
		return modelUnknown(n, nil)
	}
	e := ModelExpr{Span: span(n)}
	switch n.Kind {
	case "call_expression":
		callee, args := n.Fields["function"], n.Fields["arguments"]
		if callee == nil || args == nil || modelOptional(n) {
			return modelUnknown(n, nil)
		}
		if args.Kind == "template_string" {
			tag := reduceModel(callee, depth+1)
			e.Kind, e.Tag = ModelTagged, &tag
			if !modelTemplate(&e, args, depth) {
				return modelUnknown(n, nil)
			}
			tagModel("E-tagged", e)
			return e
		}
		callee2 := reduceModel(callee, depth+1)
		e.Kind, e.Callee, e.Args = ModelCall, &callee2, []ModelExpr{}
		for _, a := range modelChildren(args) {
			e.Args = append(e.Args, reduceModel(a, depth+1))
		}
		tagModel("E-call", e)
	case "member_expression":
		object, property := n.Fields["object"], n.Fields["property"]
		if object == nil || property == nil || property.Kind != "property_identifier" || modelOptional(n) {
			return modelUnknown(n, nil)
		}
		o := reduceModel(object, depth+1)
		e.Kind, e.Object, e.Name = ModelMember, &o, property.Text
		tagModel("E-member", e)
	case "identifier", "undefined":
		e.Kind, e.Name = ModelIdentifier, n.Text
		tagModel("E-identifier", e)
	case "string":
		v, ok := jsString(n.Text)
		if !ok {
			return modelUnknown(n, nil)
		}
		e.Kind, e.Value = ModelString, v
		tagModel("E-string", e)
	case "number":
		e.Kind, e.Value = ModelNumber, n.Text
		tagModel("E-number", e)
	case "unary_expression":
		arg := modelUnwrap(n.Fields["argument"])
		op := text(n.Fields["operator"])
		if op == "" && len(n.Tokens) == 1 {
			op = n.Tokens[0]
		}
		if arg == nil || op == "" {
			return modelUnknown(n, nil)
		}
		if arg.Kind != "number" || (op != "-" && op != "+") {
			// Version 2: typeof, !, void, delete and signs on non-numbers.
			o := reduceModel(arg, depth+1)
			e.Kind, e.Value, e.Object = ModelUnary, op, &o
			tagModel("E-unary", e)
			return e
		}
		e.Kind, e.Value = ModelNumber, op+arg.Text
		tagModel("E-number", e)
	case "binary_expression":
		left, right := n.Fields["left"], n.Fields["right"]
		op := text(n.Fields["operator"])
		if op == "" && len(n.Tokens) == 1 {
			op = n.Tokens[0]
		}
		if left == nil || right == nil || op == "" {
			return modelUnknown(n, nil)
		}
		l, r := reduceModel(left, depth+1), reduceModel(right, depth+1)
		e.Kind, e.Value, e.Left, e.Right = ModelBinary, op, &l, &r
		tagModel("E-binary", e)
	case "true", "false":
		e.Kind, e.Value = ModelBoolean, n.Kind
		tagModel("E-boolean", e)
	case "null":
		e.Kind = ModelNull
		tagModel("E-null", e)
	case "template_string":
		e.Kind = ModelTemplate
		if !modelTemplate(&e, n, depth) {
			return modelUnknown(n, nil)
		}
		tagModel("E-template", e)
	case "object":
		e.Kind, e.Entries = ModelObject, []ModelEntry{}
		for _, p := range modelChildren(n) {
			e.Entries = append(e.Entries, modelEntry(p, depth+1))
		}
		tagModel("E-object", e)
	case "array":
		e.Kind, e.Elements = ModelArray, []ModelExpr{}
		for _, c := range modelChildren(n) {
			e.Elements = append(e.Elements, reduceModel(c, depth+1))
		}
		tagModel("E-array", e)
	case "spread_element":
		children := modelChildren(n)
		if len(children) != 1 {
			return modelUnknown(n, nil)
		}
		o := reduceModel(children[0], depth+1)
		e.Kind, e.Object = ModelSpread, &o
		tagModel("E-spread", e)
	case "arrow_function":
		if !modelArrow(&e, n, depth) {
			return modelUnknown(n, nil)
		}
		if e.Async {
			tagModel("E-async", e)
			return e
		}
		tagModel("E-arrow", e)
	case "new_expression":
		callee := n.Fields["constructor"]
		if callee == nil {
			return modelUnknown(n, nil)
		}
		c := reduceModel(callee, depth+1)
		e.Kind, e.Callee, e.Args = ModelNew, &c, []ModelExpr{}
		for _, a := range modelChildren(n.Fields["arguments"]) {
			e.Args = append(e.Args, reduceModel(a, depth+1))
		}
		tagModel("E-new", e)
	case "function_expression", "function":
		if !modelFunction(&e, n, depth) {
			return modelUnknown(n, nil)
		}
		if e.Async {
			tagModel("E-async", e)
			return e
		}
		tagModel("E-function", e)
	case "await_expression":
		// Row E-await (T018g6 Revision 3 addendum 2): kept only in the
		// TypeORM rows, as Async is.
		children := modelChildren(n)
		if len(children) != 1 {
			return modelUnknown(n, nil)
		}
		o := reduceModel(children[0], depth+1)
		e.Kind, e.Value, e.Object = ModelUnary, "await", &o
		tagModel("E-await", e)
	case "assignment_expression":
		left, right := n.Fields["left"], n.Fields["right"]
		if left == nil || right == nil {
			return modelUnknown(n, nil)
		}
		l, r := reduceModel(left, depth+1), reduceModel(right, depth+1)
		e.Kind, e.Left, e.Right = ModelAssign, &l, &r
		tagModel("E-assign", e)
	case "ternary_expression":
		test, then, otherwise := n.Fields["condition"], n.Fields["consequence"], n.Fields["alternative"]
		if test == nil || then == nil || otherwise == nil {
			return modelUnknown(n, nil)
		}
		t, a, b := reduceModel(test, depth+1), reduceModel(then, depth+1), reduceModel(otherwise, depth+1)
		e.Kind, e.Test, e.Then, e.Else = ModelCond, &t, &a, &b
		tagModel("E-conditional", e)
	case "subscript_expression":
		object, index := n.Fields["object"], n.Fields["index"]
		if object == nil || index == nil || modelOptional(n) {
			return modelUnknown(n, nil)
		}
		o, i := reduceModel(object, depth+1), reduceModel(index, depth+1)
		e.Kind, e.Object, e.Index = ModelIndex, &o, &i
		tagModel("E-index", e)
	default:
		return modelUnknown(n, nil)
	}
	return e
}

// modelTemplate fills Strings (cooked, line terminators normalised to \n as
// JavaScript does) and Substitutions. An invalid escape fails the template.
func modelTemplate(e *ModelExpr, n *parse.Node, depth int) bool {
	e.Strings, e.Substitutions = []string{}, []ModelExpr{}
	cook := func(from, to uint32) bool {
		raw := n.Text[from-n.Start : to-n.Start]
		raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
		v, ok := jsString("`" + raw + "`")
		e.Strings = append(e.Strings, v)
		return ok
	}
	start := n.Start + 1
	for _, c := range n.Children {
		if c.Kind != "template_substitution" {
			continue
		}
		if !cook(start, c.Start) {
			return false
		}
		inner := modelChildren(c)
		if len(inner) != 1 {
			return false
		}
		e.Substitutions = append(e.Substitutions, reduceModel(inner[0], depth+1))
		start = c.End
	}
	if n.End < start+1 {
		return false
	}
	return cook(start, n.End-1)
}

// modelArrow fills an arrow function's parameter names and body. An async
// arrow is marked Async (row E-async, T018g6 Revision 3 addendum 2); only
// the TypeORM rows keep it (CaptureModels turns it back into unknown in
// every other gated file: it returns a promise, never a value drizzle-kit
// or Sequelize uses).
func modelArrow(e *ModelExpr, n *parse.Node, depth int) bool {
	e.Async = modelHasToken(n, "async")
	e.Kind, e.Params = ModelArrow, []string{}
	if p := n.Fields["parameter"]; p != nil {
		e.Params = append(e.Params, modelParam(p))
	} else {
		for _, p := range modelChildren(n.Fields["parameters"]) {
			e.Params = append(e.Params, modelParam(p))
		}
	}
	body := n.Fields["body"]
	if body == nil {
		return false
	}
	if body.Kind != "statement_block" {
		b := reduceModel(body, depth+1)
		e.Body = &b
		return true
	}
	e.Block = true
	e.Statements = bodyStatements(body, depth+1)
	statements := modelChildren(body)
	if len(statements) == 1 && statements[0].Kind == "return_statement" {
		if values := modelChildren(statements[0]); len(values) == 1 {
			b := reduceModel(values[0], depth+1)
			e.Body = &b
			return true
		}
	}
	b := modelUnknown(body, nil)
	e.Body = &b
	return true
}

// modelFunction fills a function expression's name, parameter names and body
// statements. Generator functions are not read; an async function is marked
// Async, as for arrows.
func modelFunction(e *ModelExpr, n *parse.Node, depth int) bool {
	body := n.Fields["body"]
	if modelHasToken(n, "*") || body == nil {
		return false
	}
	e.Async = modelHasToken(n, "async")
	e.Kind, e.Params = ModelFunction, []string{}
	if name := n.Fields["name"]; name != nil {
		e.Name = name.Text
	}
	for _, p := range modelChildren(n.Fields["parameters"]) {
		e.Params = append(e.Params, modelParam(p))
	}
	e.Statements = bodyStatements(body, depth+1)
	return true
}

// modelParam returns a parameter's identifier name, or "" for any other form.
func modelParam(p *parse.Node) string {
	switch p.Kind {
	case "identifier":
		return p.Text
	case "required_parameter", "optional_parameter":
		if pattern := p.Fields["pattern"]; pattern != nil && pattern.Kind == "identifier" && p.Fields["value"] == nil {
			return pattern.Text
		}
	}
	return ""
}

// modelEntry reduces one object literal entry.
func modelEntry(p *parse.Node, depth int) ModelEntry {
	entry := ModelEntry{Span: span(p)}
	switch p.Kind {
	case "pair":
		key, ok := modelKey(p.Fields["key"])
		if !ok || p.Fields["value"] == nil {
			break
		}
		entry.Kind, entry.Key, entry.Value = ModelProperty, key, reduceModel(p.Fields["value"], depth)
		tagModel("E-property", entry)
		return entry
	case "shorthand_property_identifier":
		entry.Kind, entry.Key = ModelShorthand, p.Text
		entry.Value = ModelExpr{Kind: ModelIdentifier, Span: span(p), Name: p.Text}
		tagModel("E-shorthand", entry)
		return entry
	case "spread_element":
		children := modelChildren(p)
		if len(children) != 1 {
			break
		}
		entry.Kind, entry.Value = ModelSpreadIn, reduceModel(children[0], depth)
		tagModel("E-spread-entry", entry)
		return entry
	case "method_definition":
		key, ok := modelKey(p.Fields["name"])
		if !ok || modelHasToken(p, "async") || modelHasToken(p, "get") || modelHasToken(p, "set") || modelHasToken(p, "*") {
			break
		}
		entry.Kind, entry.Key = ModelMethod, key
		entry.Value = modelUnknown(p.Fields["body"], p)
		if statements := modelChildren(p.Fields["body"]); len(statements) == 1 && statements[0].Kind == "return_statement" {
			if values := modelChildren(statements[0]); len(values) == 1 {
				if v := reduceModel(values[0], depth); modelLiteral(v) {
					entry.Value = v
				}
			}
		}
		tagModel("E-method", entry)
		return entry
	}
	entry.Kind, entry.Value = ModelOther, modelUnknown(p, nil)
	tagModel("E-entry-unknown", entry)
	return entry
}

// modelKey decodes an identifier, string or number property key; computed
// keys are not read.
func modelKey(k *parse.Node) (string, bool) {
	if k == nil {
		return "", false
	}
	switch k.Kind {
	case "property_identifier", "identifier":
		return k.Text, true
	case "string":
		return jsString(k.Text)
	case "number":
		return numericPropertyKey(k.Text)
	}
	return "", false
}

// modelLiteral reports whether a reduced value is a literal a method may
// return in the payload (row E-method).
func modelLiteral(e ModelExpr) bool {
	switch e.Kind {
	case ModelString, ModelNumber, ModelBoolean, ModelNull:
		return true
	case ModelTemplate:
		return len(e.Substitutions) == 0
	}
	return false
}
