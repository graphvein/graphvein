package facts

import "github.com/graphvein/graphvein/internal/contract"

// RouteExpr is detached syntax for conservative route and client resolution.
//
// Within one file's RouteFacts, expressions may share their Object, Args,
// Parts, Keywords and Properties (a nested call is one value in its
// enclosing call and in its own client record, T069): readers copy before
// they change any of them, never write into them.
type RouteExpr struct {
	Kind, Text, Name string
	Offset           int
	Object           *RouteExpr
	Args             []RouteExpr
	Keywords         []RouteKeyword       `json:",omitempty"`
	Parts            []RouteExpr          `json:",omitempty"`
	Properties       map[string]RouteExpr `json:",omitempty"`
	Operator         string               `json:",omitempty"`
	Keys             []string             `json:",omitempty"`
	Invalid          bool                 `json:",omitempty"`
	// Spread marks a TypeScript object literal with at least one spread
	// element ({...criteria, include: ...}), so a reader of Properties knows
	// the map may be incomplete (T018h, ADR 0060).
	Spread bool `json:",omitempty"`
	// Ref is zero except inside the fact cache layout of RouteFacts
	// (exprtable.go), where an expression is written as a 1-based index
	// into the file's expression table.
	Ref  int32          `json:",omitempty"`
	Span *contract.Span `json:",omitempty"`
}

// RouteKeyword preserves named and unpacked Python call arguments.
type RouteKeyword struct {
	Name  string
	Value RouteExpr
}

// RouteBinding preserves lexical shadowing and one-level factory returns.
type RouteBinding struct {
	Value       RouteExpr
	Returns     []RouteExpr
	ReturnScope int
	Constant    bool
	ModuleConst bool `json:",omitempty"`
	// Mutable marks a let or var declaration at any scope, so a reader
	// that needs a const binding (a Supabase client, row SB-K1 of
	// docs/extract/table-access.md) can refuse it; Async marks an async
	// function declaration, whose call yields a promise.
	Mutable        bool `json:",omitempty"`
	Async          bool `json:",omitempty"`
	Module, Export string
	Evaluation     *URLBinding `json:",omitempty"`
}

// RouteScope separates anonymous and block bindings without changing identities.
type RouteScope struct {
	Parent   int
	Bindings map[string]RouteBinding
	Function bool `json:",omitempty"`
	// Async marks an async or generator function scope: its body may run
	// after the call returns (T026 AG-F, Express row DA-E3).
	Async bool `json:",omitempty"`
	// Params are a function scope's parameters in order, Exits the spans
	// of the return and throw statements of its own body (not of nested
	// functions); DA-E3 reads them.
	Params []RouteParam    `json:",omitempty"`
	Exits  []contract.Span `json:",omitempty"`
	// Start is a function scope's source offset (its node's start, the
	// Offset of the RouteExpr that writes it), so an inline function
	// argument finds its parameters (T026 AG-RF, Express error handlers).
	Start int `json:",omitempty"`
}

// RouteParam is one parameter as written: an identifier (Name), an object
// pattern of shorthand or key: identifier properties (Props, key to local
// name; Open when it has a rest element or another form), or neither (a
// form DA-E3 does not read).
type RouteParam struct {
	Name  string            `json:",omitempty"`
	Props map[string]string `json:",omitempty"`
	Open  bool              `json:",omitempty"`
	// Default and Rest mark a parameter with a default value and a rest
	// parameter: JavaScript's Function.length counts the parameters
	// before the first of either (Express reads it to tell error
	// handlers, T026 AG-RF).
	Default bool `json:",omitempty"`
	Rest    bool `json:",omitempty"`
}

// FunctionLength is JavaScript's Function.length of params: the count of
// parameters before the first one with a default value or the rest
// parameter.
func FunctionLength(params []RouteParam) int {
	n := 0
	for _, p := range params {
		if p.Default || p.Rest {
			break
		}
		n++
	}
	return n
}

// URLBinding retains source proof independently of route factory resolution.
type URLBinding struct {
	Value                     RouteExpr
	Constant, Single, Mutated bool
	Offset                    int
	Helper                    *URLHelper `json:",omitempty"`
}

// URLHelper is bounded executable syntax, not a resolved cross-file result.
type URLHelper struct {
	Identity   string
	Scope      int
	Eligible   bool
	BoundConst bool
	Parameters []ClientParameter
	Body       []URLStatement
}

// URLStatement preserves only the statement forms admitted by URL evaluation.
type URLStatement struct {
	Kind, Name, Operator string
	Constant             bool
	Expression           RouteExpr
	Body, Else           []URLStatement
	Span                 contract.Span
}

// RouteCall caches registration and mount syntax, never composed paths.
type RouteCall struct {
	Receiver         RouteExpr
	Method           string
	Args             []RouteExpr
	Pattern          *RouteExpr
	Scope            int
	Owner, OwnerName string
	Conditional      bool
	// Guarded marks a call under a branch, loop, try or short-circuit
	// operator of its own function body (or of the top level); DA-E3
	// reads it.
	Guarded bool `json:",omitempty"`
	Span    contract.Span
	Hash    string
}

// RouteFacts is internal extraction metadata invalidated by the extractor version.
type RouteFacts struct {
	Angular         *AngularFacts `json:",omitempty"`
	Scopes          []RouteScope
	Exports         map[string]string
	Calls           []RouteCall
	Clients         []ClientCall                 `json:",omitempty"`
	Defaults        []ClientDefault              `json:",omitempty"`
	Parameters      map[string][]ClientParameter `json:",omitempty"`
	ParameterScopes map[string]int               `json:",omitempty"`
	URLScopes       []RouteScope                 `json:",omitempty"`
	// AmbientStrings names the global `declare const NAME: string`
	// declarations of a .d.ts file (top level of a script declaration
	// file, or inside `declare global { }`). A build tool such as webpack's
	// DefinePlugin usually supplies the value, so the clients extractor
	// records such a name used as a URL prefix as an unresolved base
	// (docs/extract/clients.md "Build-time constant bases").
	AmbientStrings []string `json:",omitempty"`
	// GlobalDefinitions names what a file other than a .d.ts defines that
	// could give such a name a value in the repository: its top-level
	// value declarations and its assignments to a property of globalThis,
	// window, self or global. Any of them leaves the name an ordinary
	// unknown identifier.
	GlobalDefinitions []string `json:",omitempty"`
	// AutoImports are the global bindings a declaration file generated by
	// unplugin-auto-import declares (T033 UP-C, row UP-41 of
	// docs/contract/claims/unknown-prefix-paths.md): inside `declare global
	// { }`, `const NAME: typeof import('./module').NAME`. The bundler plugin
	// inserts the import into every module it transforms, so such a name is
	// the module's export, not an unbound global.
	AutoImports []AutoImport `json:",omitempty"`
	// CommonJS is what the file exports through `module.exports` and
	// `exports` (T036): a `require` of the file reads it as an ESM import
	// reads Exports. Nil when the file writes neither.
	CommonJS *CommonJSExports `json:",omitempty"`

	// source is the file's text from byte sourceAt on (SetSource): the
	// fact cache stores an expression's Text as its length when it is
	// source[Offset:], and recovers it from the stored covering text.
	source   string
	sourceAt int
}

// SetSource records the file text the expressions' Text values are slices
// of, from byte offset at on (normally the parse tree root's Text and
// Start), so the fact cache can store Text values as offsets (T069). Facts
// without it are cached with their Text written out.
func (r *RouteFacts) SetSource(text string, at int) {
	r.source, r.sourceAt = text, at
}

// CommonJSExports is the value a `require` of a file returns, when the file
// writes it at its top level only (T036, rows CJ-E1..CJ-E4 of
// docs/extract/routes.md): Module is `module.exports = E`, Members the
// `exports.n = E` and `module.exports.n = E` writes. Unread marks any other
// write of the exports object; a reader then claims nothing about the file.
type CommonJSExports struct {
	Module  *RouteExpr           `json:",omitempty"`
	Members map[string]RouteExpr `json:",omitempty"`
	Unread  bool                 `json:",omitempty"`
}

// AutoImport is one global binding of a generated auto-import declaration
// file: Name is the module Module's export of the same name (Module as
// written, relative to the declaration file).
type AutoImport struct {
	Name, Module string
}

// ClientCall retains call-site syntax for recognition after all files resolve.
type ClientCall struct {
	Injection        *AngularInjection `json:",omitempty"`
	Expression       RouteExpr
	Scope            int
	URLScope         int `json:",omitempty"`
	Owner, OwnerName string
	// Guarded: as RouteCall.Guarded.
	Guarded bool `json:",omitempty"`
	Span    contract.Span
	Hash    string
}

// ClientDefault retains unconditional module-level axios defaults assignments.
type ClientDefault struct {
	Receiver, Value RouteExpr
	Scope           int
}

// ClientParameter permits bounded wrapper substitution without local dataflow.
type ClientParameter struct {
	Name    string
	Default RouteExpr
}
