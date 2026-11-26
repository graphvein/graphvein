package facts

// RustShapeVersion identifies the Axum route shape payload (task
// T029, docs/contract/claims/route-shape-axum.md). Readers treat any
// other version as a cache miss, so a payload change never reuses stale
// syntax.
const RustShapeVersion = 2

// RustShape caches the syntax the Axum route shape linker reads and no
// other Rust cache holds: the signatures and result expressions of
// functions, the serde-relevant parts of type declarations, and the reads
// and calls of request values. It is syntax only: whether a type is
// Axum's, serde's or the repository's is decided at link time through the
// scope's imports, so a cached record never asserts a shape.
//
// It is recorded only for files whose text mentions `axum`, `serde` or
// `http` (the gate), because only those can declare handlers, extractors,
// request helpers or serialized types; every other file has none.
type RustShape struct {
	Version int
	Types   []RustTypeDecl `json:",omitempty"`
	Fns     []RustFnShape  `json:",omitempty"`
}

// RustType is a type as written: a path with generic arguments, a
// reference, a tuple, an array or slice, `impl Trait`, or anything else
// (Kind "other", with its text).
type RustType struct {
	// Kind is "path", "ref", "tuple", "array", "impl", "unit" or "other".
	Kind string
	// Name is the path without generic arguments for "path" and the trait
	// path for "impl".
	Name string `json:",omitempty"`
	// Args are the generic type arguments ("path"), the elements
	// ("tuple"), or the single referenced or element type ("ref",
	// "array").
	Args []RustType `json:",omitempty"`
	// Text is the type's text without white space.
	Text string
}

// RustAttr is one serde attribute argument: `rename = "x"` (Name rename,
// Value x), `default` (Name default). Nested arguments such as
// `rename(serialize = "a")` keep Nested, which the linker refuses.
type RustAttr struct {
	Name   string
	Value  string `json:",omitempty"`
	Nested bool   `json:",omitempty"`
}

// RustTypeDecl is a struct, enum or type alias declaration.
type RustTypeDecl struct {
	Identity string
	Name     string
	// Kind is "struct", "tuple_struct", "unit_struct", "enum" or "alias".
	Kind  string
	Scope int
	Line  int
	// Generics are the declaration's type parameter names, in order;
	// Defaults holds the default of each (Kind "" when none), aliases only.
	Generics []string   `json:",omitempty"`
	Defaults []RustType `json:",omitempty"`
	// Derives are the derive macro paths as written (`serde::Serialize`).
	Derives []string `json:",omitempty"`
	// Serde are the container's serde attribute arguments.
	Serde []RustAttr `json:",omitempty"`
	// Attrs are the names of the item's other attributes (not derive,
	// not serde): `serde_as`, `skip_serializing_none`, `cfg`; a
	// `cfg_attr` is written `cfg_attr(a,b)` with the names of the
	// attributes it applies. The linker refuses the forms it does not
	// know (an attribute macro may rewrite serde's view).
	Attrs []string `json:",omitempty"`
	// Fields are the named fields of a struct, or the positional ones of a
	// tuple struct (Name "0", "1", ...).
	Fields   []RustField   `json:",omitempty"`
	Variants []RustVariant `json:",omitempty"`
	// Alias is the aliased type of a type alias.
	Alias *RustType `json:",omitempty"`
}

// RustField is one field of a struct or of a struct variant.
type RustField struct {
	Name  string
	Type  RustType
	Serde []RustAttr `json:",omitempty"`
	// Attrs are the field's other attributes, as RustTypeDecl.Attrs.
	Attrs []string `json:",omitempty"`
	Line  int
}

// RustVariant is one enum variant.
type RustVariant struct {
	Name string
	// Kind is "unit", "tuple" or "struct".
	Kind  string
	Serde []RustAttr `json:",omitempty"`
	// Attrs are the variant's other attributes, as RustTypeDecl.Attrs.
	Attrs []string `json:",omitempty"`
}

// RustFnShape is the shape-relevant syntax of one function or method.
type RustFnShape struct {
	Identity string
	Line     int
	// Scope is the RustFacts.Scopes index of the body, for resolving the
	// paths of the signature and the body.
	Scope    int
	Generics []string    `json:",omitempty"`
	Params   []RustParam `json:",omitempty"`
	// Return is the declared return type; nil for `()`.
	Return *RustType `json:",omitempty"`
	// SelfType and Trait are the impl's self type and trait as written,
	// for methods.
	SelfType string `json:",omitempty"`
	Trait    string `json:",omitempty"`
	// Results are the expressions the body evaluates to: the tail, every
	// `return`, through `match` arms, `if` branches and blocks.
	Results []RustValue `json:",omitempty"`
	// Lets are the `let name[: T] = value;` bindings of a single name.
	Lets []RustLet `json:",omitempty"`
	// Uses are the reads and calls of parameter values (and names bound
	// from them) in the body.
	Uses []RustUse `json:",omitempty"`
	// Try reports a `?` operator in the body (errors returned early).
	Try bool `json:",omitempty"`
	// Bindings are the names every pattern of the body binds, once per
	// binding: lets of any pattern, match arms, `if let`, `while let`,
	// `for`, closure parameters, and nested function items. Parameters
	// are not repeated here. A name bound more than once may hold
	// different values at different places, so the linker types it
	// only when it has a single binding.
	Bindings []string `json:",omitempty"`
}

// RustParam is one parameter. Name is the binding of an identifier
// pattern; Inner the single binding of a tuple struct pattern
// (`Json(input)`); Pattern the pattern text.
type RustParam struct {
	Name    string `json:",omitempty"`
	Inner   string `json:",omitempty"`
	Pattern string
	Type    RustType
	Line    int
	Self    bool `json:",omitempty"`
}

// RustValue is an expression in the small vocabulary the shape linker
// reads: "path" (Name), "call" (Name the callee path, Args), "method"
// (Name, Recv, Args), "struct" (Name the type path), "tuple" (Args),
// "array" (Args, the elements of an array literal), "literal" (Name the
// decoded string), "macro" (Name) and "other".
// References, `.await`, parentheses and `?` are removed (Try records a
// removed `?`).
type RustValue struct {
	Kind  string
	Name  string      `json:",omitempty"`
	Scope int         `json:",omitempty"`
	Line  int         `json:",omitempty"`
	Recv  *RustValue  `json:",omitempty"`
	Args  []RustValue `json:",omitempty"`
	Text  string      `json:",omitempty"`
	Try   bool        `json:",omitempty"`
	// Arms are the match arms a result was reached through, outermost
	// first (only on Results).
	Arms []RustArm `json:",omitempty"`
}

// RustArm is one match arm on the path to a result: the match it belongs
// to (Match, numbered per function in source order), its index in that
// match, the scrutinee and the pattern as written without white space,
// and whether the arm has a guard (`if ..`).
type RustArm struct {
	Match     int
	Index     int
	Scrutinee string
	Pattern   string
	Guard     bool `json:",omitempty"`
}

// RustLet is one single-name let binding.
type RustLet struct {
	Name  string
	Type  *RustType `json:",omitempty"`
	Value RustValue
	Line  int
}

// RustUse is one use of a parameter value: a method called on it (Method
// with Args, after Via: "headers()" or ".headers" when reached through the
// request's headers), or a call passing it as argument Arg (Callee).
type RustUse struct {
	// Value is the parameter's name (a name bound from it is traced back).
	Value  string
	Via    string      `json:",omitempty"`
	Method string      `json:",omitempty"`
	Args   []RustValue `json:",omitempty"`
	Callee *RustValue  `json:",omitempty"`
	Arg    int         `json:",omitempty"`
	Line   int
	Scope  int `json:",omitempty"`
}

// ShapeValid accepts an absent payload or a structurally complete one of
// the current version whose scope indexes name cached scopes; anything
// else is a cache miss.
func (r *RustFacts) ShapeValid() bool {
	if r == nil || r.Shape == nil {
		return true
	}
	if r.Shape.Version != RustShapeVersion {
		return false
	}
	ok := func(scope int) bool { return scope >= 0 && scope < len(r.Scopes) }
	for _, t := range r.Shape.Types {
		if t.Identity == "" || t.Name == "" || !ok(t.Scope) || t.Line <= 0 {
			return false
		}
	}
	for _, f := range r.Shape.Fns {
		if f.Identity == "" || !ok(f.Scope) || f.Line <= 0 {
			return false
		}
		for _, u := range f.Uses {
			if u.Value == "" || !ok(u.Scope) || (u.Method == "") == (u.Callee == nil) {
				return false
			}
		}
	}
	return true
}
