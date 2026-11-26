package facts

// Route shape syntax (T029 SH-E, rows SH-E1..SH-E39 of
// docs/contract/claims/route-shape-express.md). The TypeScript extractor
// records, per function, how the function uses its own parameters: which
// properties it reads, which methods it calls on them, and which calls it
// passes them to. Nothing in it is resolved: the Express and Next.js Pages
// shape linker resolves names against the current inventory on every run
// and follows calls at link time, so a new shape depth needs no re-parse.

// ShapeSyntax is one file's route shape syntax.
type ShapeSyntax struct {
	// Functions are the file's functions that use a parameter in a form
	// the rows read or declare a return type, in source order.
	Functions []ShapeFunction `json:",omitempty"`
	// Types are the file's top-level interfaces and type aliases.
	Types map[string]ShapeType `json:",omitempty"`
}

// ShapeFunction is the parameter-use syntax of one function (a declaration,
// a function expression, an arrow function or a method). It is recorded
// only when the function uses a parameter in a form the shape rows read or
// declares a return type.
type ShapeFunction struct {
	// Offset is the function node's start byte, equal to the Start of its
	// RouteScope; Line is its 1-based start line.
	Offset int
	Line   int
	// EndLine and EndColumn locate the function's end as the structural
	// node of a named function records it (1-based line, 0-based byte
	// column), so a node identity finds its syntax.
	EndLine, EndColumn int
	// Params are the function's parameters in order.
	Params []ShapeParam `json:",omitempty"`
	// Decl is the type annotation of the const that binds the function
	// (`const h: RequestHandler<P, R, B, Q> = (req, res) => ...`).
	Decl *ShapeType `json:",omitempty"`
	// Returns is the declared return type.
	Returns *ShapeType `json:",omitempty"`
	// Events are the uses of the parameters, in source order.
	Events []ShapeEvent `json:",omitempty"`
	// Locals names every binding declared inside the function body other
	// than its parameters (including nested blocks and functions), so a
	// callee name that a local shadows is never resolved at module level.
	Locals []string `json:",omitempty"`
	// Async marks an async function: an error it throws rejects the
	// promise it returns instead of reaching its caller.
	Async bool `json:",omitempty"`
	// ReturnFn is the start offset of the one function this function
	// returns (a middleware factory: `(opts) => (req, res, next) => ...`),
	// 0 when it returns none or several.
	ReturnFn int `json:",omitempty"`
}

// ShapeParam is one parameter as written: a name, or an object pattern
// whose keys are read (Open when it has a rest element or another form).
type ShapeParam struct {
	Name string   `json:",omitempty"`
	Keys []string `json:",omitempty"`
	Open bool     `json:",omitempty"`
	Rest bool     `json:",omitempty"`
	// Type is the parameter's type annotation.
	Type *ShapeType `json:",omitempty"`
}

// ShapeEvent is one use of a parameter (Param is its index) or of a const
// alias of a property path of it. Path is the literal property path from
// the parameter.
//
// Kinds:
//   - "read": the path is read (Whole: the object at the path is used in a
//     form that may read any key, such as a spread or a computed key);
//   - "keys": an object pattern destructures the path (Keys; Open for a
//     rest element or a computed key);
//   - "call": methods are called on the path (Methods, outermost last);
//     an empty Path and a single method without a name is a call of the
//     parameter itself (`next(err)`);
//   - "pass": the path is an argument (Arg) of a call whose callee is not
//     a parameter (Callee); Transformed when the argument is the value
//     changed (spread into an object literal);
//   - "cast": the path is asserted to a type (`as T`, or a const
//     annotation `const b: T = req.body`);
//   - "assign": the path is assigned to (`res.statusCode = 404`);
//   - "throw": a throw statement (Param -1).
type ShapeEvent struct {
	Kind    string        `json:"k"`
	Param   int           `json:"p"`
	Path    []string      `json:",omitempty"`
	Whole   bool          `json:",omitempty"`
	Keys    []string      `json:",omitempty"`
	Open    bool          `json:",omitempty"`
	Methods []ShapeMethod `json:",omitempty"`
	Callee  *ShapeExpr    `json:",omitempty"`
	Arg     int           `json:",omitempty"`
	// Argc is the number of arguments of a pass's call.
	Argc        int  `json:",omitempty"`
	Transformed bool `json:",omitempty"`
	// Bound marks a read whose path is bound to a const alias (`const q =
	// req.query`): the path itself is not used as a whole.
	Bound bool `json:",omitempty"`
	// Top marks a pass whose call is a statement of the function body's
	// top level: the whole expression of an expression statement, or the
	// whole initializer of a declarator, under parentheses and await only
	// (never inside an if, a loop, a switch, a try, a conditional or
	// logical expression, a callback, or with .then/.catch on it). An
	// arrow function's expression body is its top level.
	Top bool `json:",omitempty"`
	// Awaited marks a Top pass whose call is directly under await.
	Awaited bool `json:",omitempty"`
	// Test names the failure test of a Top pass whose result is a const
	// tested by a later if statement of the body's top level: "error"
	// (`r.error`, a destructured `error`) or "success" (`!r.success`, a
	// destructured `!success`), alone or as an operand of `||`, with a
	// consequence that always ends in return or throw. TestAt is the
	// number of events recorded before that if statement, so the linker
	// can see what runs between the call and the test.
	Test   string `json:",omitempty"`
	TestAt int    `json:",omitempty"`
	// Stmt numbers the top-level statement of the function body the event
	// is in (from 1; 0 for an arrow function's expression body); Arm is 1
	// inside the consequence and 2 inside the alternative of that
	// statement when it is an if statement, else 0; ArmExits marks an arm
	// that always ends in return or throw. The linker reads which events
	// can follow which.
	Stmt     int  `json:",omitempty"`
	Arm      int  `json:",omitempty"`
	ArmExits bool `json:",omitempty"`
	// InTry marks a pass inside the body of a try statement of the
	// function: a thrown validation error may be caught there.
	InTry bool `json:",omitempty"`
	// Nested marks a next/navigation call inside a nested function,
	// arrow function or callback: it does not run when the function
	// itself runs (T029 SH-N20).
	Nested bool       `json:",omitempty"`
	Type   *ShapeType `json:",omitempty"`
	Line   int        `json:"l"`
}

// ShapeMethod is one method call of a chain on a parameter, with its
// arguments; an argument that is a const declared in the function by
// `const x = <init>` carries the initializer in Inits (same index).
type ShapeMethod struct {
	Name  string       `json:",omitempty"`
	Args  []ShapeExpr  `json:",omitempty"`
	Inits []*ShapeExpr `json:",omitempty"`
}

// ShapeType is a TypeScript type as written, reduced to the forms the
// shape rows read: "string", "number", "boolean", "null", "undefined",
// "any", "unknown", "never", "object" (Props), "array" (Args[0]), "name"
// (Name, generic Args), "union" (Args), "literal" (Name holds the literal
// text) and "other" (any other form).
type ShapeType struct {
	Kind  string      `json:"k"`
	Name  string      `json:",omitempty"`
	Args  []ShapeType `json:",omitempty"`
	Props []ShapeProp `json:",omitempty"`
	// Open marks an object type with an index signature or another member
	// form, or an interface that extends another type.
	Open bool `json:",omitempty"`
}

// ShapeProp is one property of an object type.
type ShapeProp struct {
	Name     string    `json:"n"`
	Optional bool      `json:",omitempty"`
	Type     ShapeType `json:"t"`
}

// ShapeExpr is a compact expression as written: Kind is the syntax kind
// ("identifier", "member_expression", "call_expression", "literal" for a
// string without substitutions, "number", "object", ...), Name the
// identifier, property, literal value or number text, Object the object of
// a member or the callee of a call, Args a call's arguments, Props an
// object literal's literal keys (Open when it has a spread or a computed
// key).
type ShapeExpr struct {
	Kind   string               `json:"k"`
	Name   string               `json:"n,omitempty"`
	Object *ShapeExpr           `json:"o,omitempty"`
	Args   []ShapeExpr          `json:"a,omitempty"`
	Props  map[string]ShapeExpr `json:"p,omitempty"`
	Open   bool                 `json:"s,omitempty"`
}

// RouteExpr converts e to the route expression form the route resolver
// reads (Text is the name).
func (e ShapeExpr) RouteExpr() RouteExpr {
	out := RouteExpr{Kind: e.Kind, Name: e.Name, Text: e.Name, Spread: e.Open}
	if e.Object != nil {
		o := e.Object.RouteExpr()
		out.Object = &o
		if e.Kind == "member_expression" {
			out.Text = o.Text + "." + e.Name
		}
	}
	for _, a := range e.Args {
		out.Args = append(out.Args, a.RouteExpr())
	}
	if e.Props != nil {
		out.Properties = map[string]RouteExpr{}
		for k, v := range e.Props {
			out.Properties[k] = v.RouteExpr()
		}
	}
	return out
}
