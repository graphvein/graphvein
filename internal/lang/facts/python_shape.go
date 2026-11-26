package facts

// PythonShapeVersion identifies the route shape payload format. Readers
// treat any other version as a cache miss, so a format change never reuses
// stale syntax.
const PythonShapeVersion = 3

// PythonShape is the reduced syntax the FastAPI route shape linker reads
// (rows SH-F of docs/contract/claims/route-shape-fastapi.md): function
// signatures and the few body statements that can add to a route's
// request or response shape, and the annotated members of classes that
// may be Pydantic models. It is captured only in files that import
// fastapi, starlette, pydantic or sqlmodel (gate SG); every other file has
// no payload, and a call into such a file stops with a recorded reason.
//
// It is syntax only: whether an annotation is a Pydantic model, a call is
// FastAPI's Query or a raised class is HTTPException is decided at link
// time through the module resolver.
type PythonShape struct {
	Version   int
	Functions []PythonShapeFunction `json:",omitempty"`
	Classes   []PythonShapeClass    `json:",omitempty"`
	// Handlers are the project-level registrations that can rewrite any
	// route's statuses or request handling: exception handlers, and
	// route_class and default_response_class assignments. They are captured in every Python file,
	// not only gate SG's (an app object can be configured anywhere).
	Handlers []PythonShapeHandler `json:",omitempty"`
}

// PythonShapeHandler is one exception handler registration
// (`@app.exception_handler(K)`, `app.add_exception_handler(K, h)`, a key
// of an `exception_handlers=` dictionary) or one `x.route_class = ...` or
// `x.default_response_class = ...` assignment.
type PythonShapeHandler struct {
	// Kind is "exception_handler", "route_class" or
	// "default_response_class".
	Kind  string
	Line  int
	Scope int
	// Key is the registered exception class or status code; nil when it
	// is not read (unpacking, a non-literal dictionary).
	Key *RouteExpr `json:",omitempty"`
}

// PythonShapeFunction is one function scope's signature and shape sites.
type PythonShapeFunction struct {
	// Scope is the PythonFacts.Scopes index of the function's own scope.
	Scope int
	// Line is the 1-based line of the def.
	Line   int
	Params []PythonShapeParam `json:",omitempty"`
	// Returns is the return annotation; nil when there is none.
	Returns *RouteExpr `json:",omitempty"`
	// Sites are the body statements the linker reads, in source order
	// (not those of nested functions or classes).
	Sites []PythonShapeSite `json:",omitempty"`
	// Generator is true when the body yields: FastAPI streams the
	// handler's values, and a dependency's code after the yield runs
	// after the response.
	Generator bool `json:",omitempty"`
}

// PythonShapeParam is one parameter as written: annotation and default
// reduced, never evaluated.
type PythonShapeParam struct {
	Name string
	// Kind is "" (positional or keyword), "positional" (before /),
	// "keyword" (after * or *args), "args" (*args) or "kwargs" (**kwargs).
	Kind       string     `json:",omitempty"`
	Annotation *RouteExpr `json:",omitempty"`
	// Default is the reduced default value; HasDefault is set even when
	// the default is not kept (a form the linker does not read).
	Default    *RouteExpr `json:",omitempty"`
	HasDefault bool       `json:",omitempty"`
	Line       int
}

// PythonShapeSite is one statement or expression of a function body that
// can add to the shape.
type PythonShapeSite struct {
	// Kind is one of:
	//   - raise: Expr is the raised call;
	//   - return: Expr is the returned call (await removed), or Name the
	//     returned local name, or Plain; none of them for any other value;
	//   - assign: Name = Expr, a local assigned a call's result (or a
	//     Plain value);
	//   - call: Expr is a call to a dotted name that is not a local value
	//     (a candidate repository function); Args say which arguments are
	//     the function's parameters;
	//   - read: Name is a watched parameter, Attrs the attributes read on
	//     it, Method the method called last (get, json, ...), Key the
	//     literal key read (nil when not a literal: Dynamic);
	//   - use: the first other use of a watched parameter (it escapes).
	Kind string
	Line int
	// Try is true inside a try body: an exception raised here (or by a
	// call here) may be caught.
	Try bool `json:",omitempty"`
	// Plain marks a returned or assigned value that is never a Response
	// object: a literal, a container, a comparison or a builtin
	// conversion (dict(...), str(...)), or a bare return.
	Plain bool `json:",omitempty"`
	// AfterYield marks a site after the function's first yield (in source
	// order): in a dependency it runs after the response is sent.
	AfterYield bool             `json:",omitempty"`
	Expr       *RouteExpr       `json:",omitempty"`
	Args       []PythonShapeArg `json:",omitempty"`
	Name       string           `json:",omitempty"`
	Attrs      []string         `json:",omitempty"`
	Method     string           `json:",omitempty"`
	Key        *string          `json:",omitempty"`
	Dynamic    bool             `json:",omitempty"`
}

// PythonShapeArg is one argument of a call site that passes a parameter
// of the calling function on (the value call following follows).
type PythonShapeArg struct {
	// Index is the positional index, or -1 for a keyword argument.
	Index   int
	Keyword string `json:",omitempty"`
	// Param is the caller's parameter passed as is; Attr is set when an
	// attribute of it is passed instead (request.headers).
	Param string
	Attr  string `json:",omitempty"`
}

// PythonShapeClass is a class with bases, and its annotated and assigned
// members as the Pydantic schema reads them.
type PythonShapeClass struct {
	// Scope is the PythonFacts.Scopes index of the class's own scope.
	Scope   int
	Line    int
	Members []PythonModelMember `json:",omitempty"`
}

// Valid accepts an absent payload or a structurally complete payload of
// the current version; anything else is a cache miss.
func (s *PythonShape) Valid(scopes int) bool {
	if s == nil {
		return true
	}
	if s.Version != PythonShapeVersion {
		return false
	}
	for _, f := range s.Functions {
		if f.Scope <= 0 || f.Scope >= scopes || f.Line <= 0 {
			return false
		}
		for _, p := range f.Params {
			if p.Name == "" || p.Line <= 0 {
				return false
			}
		}
		for _, site := range f.Sites {
			if site.Line <= 0 || site.Kind == "" {
				return false
			}
		}
	}
	for _, h := range s.Handlers {
		if h.Line <= 0 || h.Kind == "" || h.Scope < 0 || h.Scope >= scopes {
			return false
		}
	}
	for _, c := range s.Classes {
		if c.Scope <= 0 || c.Scope >= scopes || c.Line <= 0 || !validModelMembers(c.Members) {
			return false
		}
	}
	return true
}

// ShapeValid checks the shape payload against the file's scopes.
func (p *PythonFacts) ShapeValid() bool {
	if p == nil {
		return true
	}
	return p.Shape.Valid(len(p.Scopes))
}
