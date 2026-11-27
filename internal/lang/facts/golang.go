package facts

import "github.com/graphvein/graphvein/internal/contract"

// GoModelsVersion identifies the Go payload shape (ADR 0063). Readers
// treat any other version as a cache miss, so a shape change never reuses
// stale syntax. Version 2 adds the structure fields (Build, Rejected,
// Unread) of the Go structural claim contract (docs/lang/go-structure.md);
// version 3 adds the router syntax of T023 (Routes, Signatures, Debug and
// the expression fields Text, Func and Uses; docs/lang/go-routes.md);
// version 4 keeps calls on new(http.ServeMux) and on chains of router child
// calls (T023 GR3); version 5 gates and keeps the serving syntax of
// net/http (serving calls, StripPrefix and http.Server bindings, T023 GR4);
// version 6 adds the request-path syntax of wrappers and request helpers
// (Wrappers and Requests, T023 GR5); version 7 adds the generated-server
// payload of oapi-codegen files and gates the files that call a generated
// server's entry functions (Routes.Generated, T023 GR6); version 8 adds the
// forwarding proof of a function literal in router syntax (GoExpr.Forward,
// T023 GR7); version 9 (T023 GR8, GR9) adds the interface method names
// of named interface types and the escape syntax of router values (the
// records' Escapes, GoRouteCall.Statement, GoExpr.Request and Uses of
// captured literals) and the method intercepts and guards of wrappers and
// forwarding closures with the request reads of request helpers (T023 GR8);
// version 10 adds the request and response syntax of handlers (Shapes and
// Marshalers, T029 SH-G; docs/contract/claims/route-shape-go.md);
// version 11 adds the local type names of handlers (GoShapeFunc.LocalTypes,
// T029 W2).
const GoModelsVersion = 11

// GoMaxDepth bounds a reduced Go expression tree, as the TypeScript model
// payload bounds its trees: deeper nodes are kind unknown.
const GoMaxDepth = 48

// GoFacts is the Go syntax that GORM model linking (T018g5) reads. It is
// syntax only, never a claim (rule 31): whether a struct is a model, a
// receiver is a *gorm.DB or a call opens a database is decided at link time
// through the module's own packages, under the claim contract in
// docs/contract/claims/gorm-models.md. The payload table in
// docs/lang/go-models.md names every capture branch.
//
// Every non-test Go file carries the type shapes (Structs, Types, Methods,
// Consts), because a model, an embedded struct or a table-name constant may
// live in a file that imports nothing from GORM. Only files that pass gate G1 (an
// import of GORM, a GORM driver, glebarez/sqlite, inflection or jinzhu/gorm)
// carry Vars and Funcs, the sites GORM discovery reads. A _test.go file
// carries only Version: GORM discovery never reads test files (row GO-K1).
type GoFacts struct {
	Version int
	// Structs are the package-level struct type declarations, in source order.
	Structs []GoStruct `json:",omitempty"`
	// Types are the package-level named non-struct types and every type alias.
	Types []GoNamedType `json:",omitempty"`
	// Methods are the methods whose name GORM's schema parse reads
	// (TableName, Value, Scan, GormDataType, GormDBDataType).
	Methods []GoMethod `json:",omitempty"`
	// Consts are the package-level constants whose value is string-shaped
	// (string literals, names, selectors and + of those), which a TableName
	// body may return (T018g5 Revision 1, O7).
	Consts []GoConst `json:",omitempty"`
	// Gated reports that the file passes gate G1; Vars and Funcs are captured
	// only then.
	Gated bool `json:",omitempty"`
	// Vars are the package-level var declarations of a gated file.
	Vars []GoVar `json:",omitempty"`
	// Funcs are the function and method declarations with a body of a gated
	// file, in source order.
	Funcs []GoFunc `json:",omitempty"`

	// The fields below belong to the Go structural claim contract
	// (docs/lang/go-structure.md, ADR 0068), not to GORM. Every Go file,
	// test files included, carries them.

	// Build are the file header's build constraint lines as go/build reads
	// them: every //go:build line of the header, or, when there is none,
	// the // +build lines that precede the header's last blank line. The
	// linker decides from them whether some build configuration selects
	// the file (contract row GS-I1).
	Build []string `json:",omitempty"`
	// Rejected reports that go/parser rejects the file (row GS-F2): it
	// claims no declaration, and the linker emits no import edge for it.
	Rejected bool `json:",omitempty"`
	// LoneCR reports that the file's raw bytes hold a CR not followed by
	// LF (row GS-F3). The scanner reads such a CR as a line end, Go reads
	// it as white space, so the text Graphvein parsed is not the text the
	// go command reads: the file claims no declaration, the linker emits
	// no import edge for it, and a directory holding it is no decided
	// package. The indexer reuses cached facts only when this bit equals
	// the scanned file's (the content hash cannot tell CR from LF).
	LoneCR bool `json:",omitempty"`
	// Unread counts, by form, the in-scope forms the extractor sent to the
	// default branch (rows GS-F2, GS-D4, GS-D6). The linker adds its own
	// forms and reports the total in one go_structure_unread diagnostic.
	Unread map[string]int `json:",omitempty"`

	// The fields below are the router syntax of task T023 (ADR 0076,
	// docs/lang/go-routes.md). They are syntax only: which object is a
	// router, which call registers a route and which path it serves are
	// decided at link time.

	// Routes is set on a file that passes the route gate (an import of chi
	// or gin, or an import of net/http with a mention of its mux API), test
	// files included; absent on every other file.
	Routes *GoRoutes `json:",omitempty"`
	// Signatures are the functions without receiver of a non-test file that
	// declare exactly one result of a named type (T, *T, pkg.T, *pkg.T):
	// the receiver type of a method value h.M where h holds such a call's
	// result (contract row GR-H2).
	Signatures []GoSignature `json:",omitempty"`
	// Debug are the file header's //go:debug lines that set httpmuxgo121
	// (assumption GR-V2), as written.
	Debug []string `json:",omitempty"`
	// Wrappers are the function and method declarations of a file that
	// imports net/http whose syntax passes the local part of the
	// path-preservation proof (docs/lang/go-routes.md, row RW-wrapper):
	// they take an http.Handler and return it, or an http.HandlerFunc
	// literal that serves the same request. Whether a call of one passes a
	// router through is decided at link time (contract row GR-O7).
	Wrappers []GoWrapper `json:",omitempty"`
	// Requests are the functions and methods of such a file whose one
	// *http.Request parameter passes the local part of the proof
	// (row RQ-request): the request is only read, or passed on to calls
	// named in Passes.
	Requests []GoRequestFunc `json:",omitempty"`
	// Names counts, in every Go file, the calls and the other uses of each
	// name a call may target (row RN-names, T023 GR10): F (a name), .M (a
	// selector of a value: a method) and imp:<path>.F (a function of an
	// imported package). A router factory is read only when every call of
	// its name is a call the router linker reads and no other use exists.
	Names map[string]GoNameUse `json:",omitempty"`

	// The fields below are the request and response syntax of task
	// T029 (docs/contract/claims/route-shape-go.md, section
	// "Payload"). They are syntax only: which function is a route's
	// handler, which call is proven and what a read claims are decided at
	// link time.

	// Shapes are the functions, methods and function literals of a
	// non-test file that imports net/http or gin and that declare a
	// parameter of type *gin.Context, *http.Request or
	// http.ResponseWriter, with the calls and local declarations of their
	// bodies.
	Shapes []GoShapeFunc `json:",omitempty"`
	// Marshalers are the methods of a non-test file that change how
	// encoding/json reads or writes a type: MarshalJSON, UnmarshalJSON,
	// MarshalText and UnmarshalText, and gin's UnmarshalParam, by receiver
	// base type name. A body
	// or response schema built from such a type (or one that embeds it)
	// is unknown.
	Marshalers []GoMarshaler `json:",omitempty"`
}

// Go carrier roles (GoShapeParam.Carrier): the request values a handler
// receives.
const (
	GoCarrierGin     = "gin"     // *gin.Context
	GoCarrierRequest = "request" // *http.Request
	GoCarrierWriter  = "writer"  // http.ResponseWriter
)

// GoShapeFunc is one function, method or function literal with a carrier
// parameter. Line (and Column for a literal) is where the declaration or
// literal starts, equal to the start of its structure node or route
// record span.
type GoShapeFunc struct {
	// Name is empty for a function literal.
	Name     string `json:",omitempty"`
	Receiver string `json:",omitempty"`
	// ReceiverName is the name a method's receiver binds.
	ReceiverName string         `json:",omitempty"`
	Line         int            `json:",omitempty"`
	Column       int            `json:",omitempty"`
	Params       []GoShapeParam `json:",omitempty"`
	// Blocks are the parents of the body's blocks: Blocks[i] is the parent
	// of block i+1 (block 0 is the body). Conditional blocks (branches,
	// loop bodies, cases, closures) are blocks of their own.
	Blocks []int `json:",omitempty"`
	// Terminal are the blocks whose statement list ends in a return
	// statement: a write in one never precedes a statement after it.
	Terminal []int `json:",omitempty"`
	// Locals are the local names the body declares, with their declared
	// type or the value of a single-name declaration.
	Locals []GoShapeLocal `json:",omitempty"`
	// Calls are the outermost calls of the body in source order (a call's
	// arguments are part of its tree), and the calls inside function
	// literals that declare no carrier parameter (marked Closure).
	Calls []GoShapeCall `json:",omitempty"`
	// Reassigned are the parameter names the body assigns or redeclares:
	// a read through one of them may not read the received value.
	Reassigned []string `json:",omitempty"` // LocalTypes are the type names declared in the body (any depth) or
	// in the body of a function enclosing a literal: a type expression of
	// the function naming one is not the package's type (T029 W2).
	LocalTypes []string `json:",omitempty"`
}

// GoShapeParam is one declared parameter; Carrier names its role when its
// type is a carrier type.
type GoShapeParam struct {
	Name    string `json:",omitempty"`
	Carrier string `json:",omitempty"`
	Type    GoExpr
}

// GoShapeLocal is one name a body declares: Type is the declared type of
// a var, Value the single value of a one-name declaration or definition
// (a composite literal, & of one, a call or a selector chain). Count is
// how many declarations or assignments of the name the body holds.
type GoShapeLocal struct {
	Name  string
	Type  *GoExpr `json:",omitempty"`
	Value *GoExpr `json:",omitempty"`
	Count int     `json:",omitempty"`
	Line  int
}

// GoShapeCall is one call with its whole tree. Results are the names its
// results are bound to by := or = ("" for _ or a non-name target).
// Checked reports that the call's last result is tested against nil in an
// if statement whose consequence ends in a return. Block is the call's
// block; Closure marks a call inside a function literal.
type GoShapeCall struct {
	Call    GoExpr
	Line    int
	Column  int      `json:",omitempty"`
	Results []string `json:",omitempty"`
	Checked bool     `json:",omitempty"`
	Block   int      `json:",omitempty"`
	Closure bool     `json:",omitempty"`
}

// GoMarshaler is one marshaling method of a type.
type GoMarshaler struct {
	Receiver string
	Name     string
	Line     int
}

// GoNameUse counts one name's calls and its other uses in a file.
type GoNameUse struct {
	Calls  int `json:",omitempty"`
	Values int `json:",omitempty"`
	// Results are how the results of the name's calls outside the
	// captured router syntax are used, where no rule reads them by
	// construction (row RN-results, T023 GR11). A call whose result is
	// only served directly (a net/http or httptest serving call, an
	// http.Server's Handler, the engine's own Run or ServeHTTP, a router's
	// own methods) or discarded records nothing.
	Results []GoResultUse `json:",omitempty"`
}

// Go result use forms (GoResultUse.Use).
const (
	GoResultArgument = "argument" // an argument of a module function or method call: Callee, Arg
	GoResultReturn   = "return"   // returned by the enclosing function: Callee names it (F or .M)
	GoResultOther    = "other"    // anything else (a store, an element, a standard-library argument, another method): Callee says what
)

// GoResultUse is one use of a call's result. Result is the result index
// the use reads (-1: every result, the call itself).
type GoResultUse struct {
	Use    string
	Result int    `json:",omitempty"`
	Callee string `json:",omitempty"`
	Arg    int    `json:",omitempty"`
	Line   int
}

// GoWrapper is a declaration whose syntax keeps a served request's path
// (row RW-wrapper): every use of its http.Handler parameter serves the
// request the returned handler receives, unchanged or through
// r.WithContext, and the request is only read before it is served.
type GoWrapper struct {
	Name string
	// Receiver is a method's receiver base type name; empty for a function.
	Receiver string `json:",omitempty"`
	// Param is the index of the http.Handler parameter.
	Param int `json:",omitempty"`
	// Context reports that some serving call passes r.WithContext(...),
	// which keeps the URL but replaces the context (chi reads its routing
	// state from the context).
	Context bool `json:",omitempty"`
	// Passes are the calls that receive the request before it is served.
	Passes []GoRequestPass `json:",omitempty"`
	// Methods are the request methods the wrapper answers itself in a
	// terminating branch chosen by r.Method == M (row
	// RW-wrapper): an inner route of one of these methods is never
	// reached through it.
	Methods []string `json:",omitempty"`
	// Guards are the passes made in the condition of a branch that decides
	// whether the request is served: a helper that reads the
	// request's URL or method there makes the wrapper choose by them.
	Guards []GoRequestPass `json:",omitempty"`
	Line   int
}

// GoRequestFunc is a function or method whose *http.Request parameter is
// only read or passed on (row RQ-request).
type GoRequestFunc struct {
	Name string
	// Receiver is a method's receiver base type name; empty for a function.
	Receiver string `json:",omitempty"`
	// Param is the index of the *http.Request parameter.
	Param int `json:",omitempty"`
	// Passes are the calls that receive the request.
	Passes []GoRequestPass `json:",omitempty"`
	// URL reports a read of r.URL or r.RequestURI in the body, Method a
	// read of r.Method: a helper that reads them, directly or
	// through its passes, may decide by the request's path or method.
	URL    bool `json:",omitempty"`
	Method bool `json:",omitempty"`
	Line   int
}

// GoRequestPass is one call that receives the request as an argument: the
// callee (a name f, a selector pkg.F, or recv.M on the declaring method's
// receiver; never a local name) and the argument index.
type GoRequestPass struct {
	Callee GoExpr
	Arg    int `json:",omitempty"`
	// Method reports recv.M: a method of the declaring method's receiver
	// type.
	Method bool `json:",omitempty"`
}

// GoRoutes is the router syntax of one gated file.
type GoRoutes struct {
	// Consts are the package-level string-shaped constants of a gated
	// _test.go file (a non-test file carries them in GoFacts.Consts).
	Consts []GoConst `json:",omitempty"`
	// Vars are the package-level var specs whose value or type mentions
	// router syntax or whose names a captured function uses.
	Vars []GoVar `json:",omitempty"`
	// Funcs are the captured function declarations and function literals.
	// A literal's record follows the record that encloses it.
	Funcs []GoRouteFunc `json:",omitempty"`
	// Generated is set on a file whose header names an oapi-codegen
	// version and whose server declarations match that generator's
	// templates token for token (docs/lang/go-routes.md, row RO-server).
	// It is syntax: whether a server's routes exist and where they are
	// served is decided at link time (contract row GR-G1).
	Generated *GoGenerated `json:",omitempty"`
}

// GoGenerated is a generated server file (row RO-server): the generator,
// its version from the file header and the server variant (chi, gin or
// net/http, the oapi-codegen chi-server, gin-server or std-http-server
// output).
type GoGenerated struct {
	Generator string
	Version   string
	Framework string
}

// GoRouteFunc is one function declaration or function literal that mentions
// router syntax (docs/lang/go-routes.md, rows RF-decl and RF-literal).
type GoRouteFunc struct {
	// Name is the declared name; empty for a function literal.
	Name     string    `json:",omitempty"`
	Receiver *GoParam  `json:",omitempty"`
	Params   []GoParam `json:",omitempty"`
	Results  []GoParam `json:",omitempty"`
	// Span is the declaration's or literal's span, equal to the span of the
	// structure node of a declaration.
	Span contract.Span
	// Parent is 1 + the index in GoRoutes.Funcs of the record enclosing a
	// literal (0 for a declaration or a literal in a package-level var), and
	// Block the index of the parent's block where the literal appears.
	Parent int `json:",omitempty"`
	Block  int `json:",omitempty"`
	// Blocks are the lexical scopes of the body; Blocks[0] is the body.
	Blocks   []GoBlock       `json:",omitempty"`
	Bindings []GoBinding     `json:",omitempty"`
	Calls    []GoRouteCall   `json:",omitempty"`
	Returns  []GoRouteReturn `json:",omitempty"`
	// Escapes are the positions outside the kept syntax where a value that
	// may hold a router flows somewhere no row reads (row
	// RU-escape): the link pass evaluates each value, and a router object
	// it holds escapes (contract row GR-C4).
	Escapes []GoEscape `json:",omitempty"`
	// Jumps is set when the body (outside nested literals) holds a goto or
	// a labeled break or continue (row RJ-jump): such a jump may skip any
	// statement of the body, so the router middleware rule (contract row
	// SH-S18) proves no Use of this body unconditional.
	Jumps bool `json:",omitempty"`
}

// Go escape forms (row RU-escape): where a value goes.
const (
	GoEscapeStore     = "store"     // an assignment to a field, element, dereference or a name the record does not declare
	GoEscapeComposite = "composite" // an element of a composite literal
	GoEscapeArgument  = "argument"  // an argument of a call the payload does not keep
	GoEscapeSend      = "send"      // a channel send
	GoEscapeMethod    = "method"    // the receiver of a method call or method value other than a router's own
	GoEscapeReturn    = "return"    // a return of a function literal the payload does not keep
)

// GoEscape is one escape position: Value is the value that flows there,
// Detail the callee, method or composite element key it flows into.
type GoEscape struct {
	Form   string
	Value  GoExpr
	Detail string `json:",omitempty"`
	// Type is a composite literal's type (escape form composite).
	Type  *GoExpr `json:",omitempty"`
	Block int     `json:",omitempty"`
	Span  contract.Span
}

// Go block kinds. body, block, if and switch run whenever the enclosing block
// runs; then, else, for, case, go and defer may run a different number of
// times (conditional context, contract row GR-C5).
const (
	GoBlockBody   = "body"   // the function body
	GoBlockPlain  = "block"  // a bare { } statement
	GoBlockIf     = "if"     // an if statement's initializer and condition
	GoBlockThen   = "then"   // an if statement's consequence
	GoBlockElse   = "else"   // an if statement's alternative (an else-if nests an if)
	GoBlockFor    = "for"    // a for statement: clause and body
	GoBlockSwitch = "switch" // a switch or type switch: initializer and tag
	GoBlockCase   = "case"   // one case or default clause of a switch, type switch or select
	GoBlockGo     = "go"     // a go statement
	GoBlockDefer  = "defer"  // a defer statement
)

// GoBlock is one lexical scope: Parent is the index of the enclosing block
// (0 for Blocks[0], which has no parent).
type GoBlock struct {
	Parent int `json:",omitempty"`
	Kind   string
}

// Go binding kinds.
const (
	GoBindVar         = "var"          // var x T = e
	GoBindDefine      = "define"       // x := e (also a select receive x := <-c)
	GoBindAssign      = "assign"       // x = e (also x = <-c)
	GoBindConst       = "const"        // const x = e
	GoBindRange       = "range"        // for k, v := range e
	GoBindRangeAssign = "range_assign" // for k, v = range e
	GoBindTypeSwitch  = "typeswitch"   // switch x := e.(type)
	// GoBindHandler is x.Handler = e: it declares and assigns no name;
	// Names holds x (T023 GR5: an http.Server's handler, contract row
	// GR-C6).
	GoBindHandler = "handler"
)

// GoBinding declares or assigns names in a block. Names pair with Values by
// position when the counts agree; an assignment target that is not an
// identifier is "" (a field, an index expression).
type GoBinding struct {
	Kind   string
	Names  []string
	Type   *GoExpr  `json:",omitempty"`
	Values []GoExpr `json:",omitempty"`
	Block  int      `json:",omitempty"`
	// Span is the statement's or spec's span; a declared name's scope
	// starts at its end.
	Span contract.Span
}

// Go route call kinds.
const (
	GoCallRouter = "router" // the callee is a selector named like a router method
	GoCallPlain  = "call"   // a call of a name or name chain passing a value on
)

// GoRouteCall is one captured call with its whole tree (receiver chain and
// arguments). Hash is the content hash of a router call's text (a route
// node's hash).
type GoRouteCall struct {
	Kind  string
	Call  GoExpr
	Block int `json:",omitempty"`
	Span  contract.Span
	Hash  string `json:",omitempty"`
	// Context says where the value of a call outside the other kept syntax
	// goes (row RU-context): GoCallStatement (an expression, go
	// or defer statement, or a condition operand: discarded) or
	// GoCallLocal (bound to a local name). Empty for a call inside a kept
	// binding, return or call tree, or one whose value an escape records.
	Context string `json:",omitempty"`
}

// Go call contexts (GoRouteCall.Context).
const (
	GoCallStatement = "statement"
	GoCallLocal     = "local"
)

// GoRouteReturn is one return statement of a captured function; Values is
// empty for a bare return.
type GoRouteReturn struct {
	Values []GoExpr `json:",omitempty"`
	Block  int      `json:",omitempty"`
	Span   contract.Span
}

// GoSignature is a function's single declared result type.
type GoSignature struct {
	Name   string
	Result GoExpr
	Line   int
}

// GoStruct is one package-level `type N struct {...}` declaration.
type GoStruct struct {
	Name          string
	Line, EndLine int
	// TypeParams are the type parameter names of a generic struct.
	TypeParams []string  `json:",omitempty"`
	Fields     []GoField `json:",omitempty"`
}

// GoField is one field declaration: named fields share one declaration
// (`A, B string`), an embedded field has no names.
type GoField struct {
	Names    []string `json:",omitempty"`
	Embedded bool     `json:",omitempty"`
	// Pointer marks an embedded `*T`; a named field's pointer is in Type.
	Pointer bool `json:",omitempty"`
	Type    GoExpr
	// Tag is the decoded struct tag literal (reflect.StructTag text).
	Tag  string `json:",omitempty"`
	Line int
}

// GoNamedType is a package-level `type N T` whose T is not a struct type, or
// any `type N = T` alias.
type GoNamedType struct {
	Name       string
	Alias      bool     `json:",omitempty"`
	TypeParams []string `json:",omitempty"`
	Type       GoExpr
	// Methods are the method names of an interface type (row
	// T-interface, T023 GR9: a module method of one of these names may be
	// called through the interface, where the router linker cannot read
	// the call).
	Methods []string `json:",omitempty"`
	Line    int
}

// GoMethod is a method GORM's schema parse may call, by its receiver's base
// type name.
type GoMethod struct {
	Receiver string
	Pointer  bool `json:",omitempty"`
	Name     string
	// Params and Results count the declared parameters and results.
	Params  int `json:",omitempty"`
	Results int `json:",omitempty"`
	// Return is the expression of a TableName body that is one `return e`.
	Return *GoExpr `json:",omitempty"`
	Line   int
}

// GoConst is one package-level constant with a string-shaped value.
type GoConst struct {
	Name  string
	Type  *GoExpr `json:",omitempty"`
	Value GoExpr
	Line  int
}

// GoVar is one package-level var spec of a gated file.
type GoVar struct {
	Names  []string
	Type   *GoExpr  `json:",omitempty"`
	Values []GoExpr `json:",omitempty"`
	Line   int
}

// GoFunc is one function or method declaration with a body in a gated file.
type GoFunc struct {
	Name string
	// Receiver is a method's receiver (its name may be empty).
	Receiver *GoParam `json:",omitempty"`
	Line     int
	EndLine  int
	Params   []GoParam `json:",omitempty"`
	Results  []GoParam `json:",omitempty"`
	// Locals are the declarations and plain assignments in the body of the
	// names the sites and mutations use, directly or through the values of
	// other kept locals; other locals are not kept.
	Locals []GoLocal `json:",omitempty"`
	// Sites are the calls GORM discovery reads (see GoSite).
	Sites []GoSite `json:",omitempty"`
	// Statements are the body's top-level call statements whose callee is a
	// name or `name.name` (a module function the order rule may expand).
	Statements []GoStatement `json:",omitempty"`
	// Mutations are assignments to a GORM configuration field.
	Mutations []GoMutation `json:",omitempty"`
	// LocalTypes are the names of the type declarations in the body (any
	// depth): a migrate argument T{...} that names one is not the package's
	// type T (T018g5 Revision 2, A6).
	LocalTypes []string `json:",omitempty"`
	// Returns are the return statements outside function literals of a
	// function whose first result is a pointer to a type named DB: the
	// receiver proof reads every one (Revision 2, A4).
	Returns []GoReturn `json:",omitempty"`
	// Calls, Stores and Literals are where the body may pass a *gorm.DB
	// on (Revision 2, A4): every call whose callee is a name or
	// `name.name` with an argument of handle shape (GoHandleShaped),
	// every `=` assignment to a selector with such a value, and every
	// composite literal of a named type with such an element value, each
	// with its own tree (nested ones repeated).
	Calls    []GoSite     `json:",omitempty"`
	Stores   []GoMutation `json:",omitempty"`
	Literals []GoSite     `json:",omitempty"`
}

// GoMigratorDDL are the gorm.Migrator methods that change a database
// schema other than AutoMigrate and CreateTable (gorm.io/gorm
// migrator.go's Migrator interface): the capture keeps their calls as
// sites and the GORM linker marks what they may change (T018g5 Revision 2,
// A3). The interface's other methods (HasTable, ColumnTypes, ...) only
// read.
var GoMigratorDDL = map[string]bool{
	"DropTable": true, "RenameTable": true, "AddColumn": true, "DropColumn": true, "AlterColumn": true,
	"MigrateColumn": true, "MigrateColumnUnique": true, "RenameColumn": true, "CreateView": true, "DropView": true,
	"CreateConstraint": true, "DropConstraint": true, "CreateIndex": true, "DropIndex": true, "RenameIndex": true,
}

// GoReturn is one return statement: Value its first value (nil for a bare
// return).
type GoReturn struct {
	Value   *GoExpr `json:",omitempty"`
	Line    int
	Nesting string
}

// GoHandleShaped reports an expression that may hold a *gorm.DB: a name,
// a selector, a call, an index expression, or & or * of one. Literals,
// composite literals and the like cannot.
func GoHandleShaped(e GoExpr) bool {
	switch e.Kind {
	case GoIdentifier, GoSelector, GoCall, GoIndex, GoSpread:
		return true
	case GoUnary:
		return (e.Value == "&" || e.Value == "*") && e.Object != nil && GoHandleShaped(*e.Object)
	}
	return false
}

// GoParam is one declared parameter or result; Name is empty when unnamed.
type GoParam struct {
	Name     string `json:",omitempty"`
	Type     GoExpr
	Variadic bool `json:",omitempty"`
}

// Go nesting marks: where a captured statement part sits relative to the
// function body's top-level statement Statement.
const (
	GoUnconditional = "unconditional" // directly in the top-level statement
	GoIfHeader      = "if-header"     // in a top-level if's initializer or condition
	GoConditional   = "conditional"   // in a nested block, branch, loop, switch, defer or go
	GoClosure       = "closure"       // inside a function literal
)

// GoLocal is a `var x T = e`, `x, y := e` or `x = e` (identifier targets
// only) anywhere in a function body.
type GoLocal struct {
	// Kind is var, define or assign.
	Kind      string
	Names     []string
	Type      *GoExpr  `json:",omitempty"`
	Values    []GoExpr `json:",omitempty"`
	Line      int
	Statement int `json:",omitempty"`
	Nesting   string
}

// GoSite is a call whose selector is AutoMigrate, CreateTable, Open, New,
// Migrator, Set, Session, Debug, WithContext, SetupJoinTable, Exec, Raw or
// a Migrator DDL method (GoMigratorDDL), or whose callee is a selector on
// the file's inflection import. A call inside a captured site's
// tree is not repeated, except inside a function literal.
type GoSite struct {
	Call      GoExpr
	Line      int
	Statement int `json:",omitempty"`
	Nesting   string
}

// GoStatement is a top-level statement of a function body that calls a name
// or `name.name`: an expression statement (kind call), a `:=` or `=` whose
// single value is the call (define, assign), `return f(...)` (return) or an
// if statement whose initializer is one of those (if).
type GoStatement struct {
	Index  int `json:",omitempty"`
	Line   int
	Kind   string
	Callee GoExpr
}

// GoMutation is an assignment whose target is a selector ending in Config,
// NamingStrategy or a configuration key GORM discovery reads.
type GoMutation struct {
	Target    GoExpr
	Value     *GoExpr `json:",omitempty"`
	Line      int
	Statement int `json:",omitempty"`
	Nesting   string
}

// Go expression kinds. Each kind fills only the fields its comment names;
// parentheses are transparent.
const (
	GoIdentifier = "identifier" // Name (also true, false and nil as literals below)
	GoSelector   = "selector"   // Object, Name: `x.Name`, `pkg.Type`
	GoCall       = "call"       // Callee, Args
	GoComposite  = "composite"  // Type (absent when elided), Elements
	GoUnary      = "unary"      // Value (operator), Object
	GoBinary     = "binary"     // Value (operator), Left, Right
	GoString     = "string"     // Value (decoded)
	GoNumber     = "number"     // Value (source text)
	GoBoolean    = "boolean"    // Value
	GoNil        = "nil"        // nothing
	GoSpread     = "spread"     // Object: a `x...` call argument
	GoIndex      = "index"      // Object, Args: `x[i]`, `T[A, B]`
	GoPointer    = "pointer"    // Object (the element type)
	GoSlice      = "slice"      // Object
	GoArray      = "array"      // Value (length source text), Object
	GoMap        = "map"        // Left (key type), Right (value type)
	GoChan       = "chan"       // nothing
	GoFuncType   = "func"       // nothing
	GoInterface  = "interface"  // nothing
	GoStructType = "struct"     // nothing (an anonymous struct type)
	GoUnknown    = "unknown"    // nothing
	// GoFuncLit is a function literal in router syntax (docs/lang/go-routes.md):
	// Func is 1 + the index of its record in GoRoutes.Funcs when it was
	// captured; Uses lists the identifiers its body uses (its own
	// parameters excluded; for a captured literal too since payload
	// version 9). The model payload reduces literals to unknown.
	GoFuncLit = "funclit"
)

// GoExpr is a reduced Go expression or type expression.
type GoExpr struct {
	Kind     string
	Name     string      `json:",omitempty"`
	Value    string      `json:",omitempty"`
	Object   *GoExpr     `json:",omitempty"`
	Callee   *GoExpr     `json:",omitempty"`
	Args     []GoExpr    `json:",omitempty"`
	Type     *GoExpr     `json:",omitempty"`
	Elements []GoElement `json:",omitempty"`
	Left     *GoExpr     `json:",omitempty"`
	Right    *GoExpr     `json:",omitempty"`
	// Text is the whitespace-free source text of an argument of a call
	// named like a router method (router syntax only; empty for a literal).
	Text string   `json:",omitempty"`
	Func int      `json:",omitempty"`
	Uses []string `json:",omitempty"`
	// Forward is set on a function literal whose syntax passes the
	// forwarding proof (docs/lang/go-routes.md, row RE-forward): it serves
	// the request it receives, unchanged, through one captured name.
	Forward *GoForward `json:",omitempty"`
	// Request is set on a function literal with a *http.Request parameter:
	// one that may serve a request through the routers it
	// uses.
	Request bool `json:",omitempty"`
}

// GoForward is the forwarding proof of a function literal (row RE-forward,
// T023): the literal func(w, r *http.Request) serves r unchanged
// through Name.ServeHTTP(w, r) statements, Name being a name the literal
// neither declares nor assigns, after only reading r or passing it to the
// calls in Passes (the wrapper proof of row RW-wrapper, without
// r.WithContext). Its only uses of r.URL are r.URL.Path as the tag of an
// expression switch whose cases are string literals, or as an operand of ==
// with a string literal; Shadows lists those literals (paths the literal may
// answer itself). Whether the name holds a router is decided at link time
// (contract row GR-O8).
type GoForward struct {
	Name    string
	Passes  []GoRequestPass `json:",omitempty"`
	Shadows []string        `json:",omitempty"`
	// Methods and Guards are as GoWrapper's.
	Methods []string        `json:",omitempty"`
	Guards  []GoRequestPass `json:",omitempty"`
}

// GoElement is one composite literal element; Key is absent for a
// positional element.
type GoElement struct {
	Key   *GoExpr `json:",omitempty"`
	Value GoExpr
}

var (
	goExprKinds = map[string]bool{GoIdentifier: true, GoSelector: true, GoCall: true, GoComposite: true, GoUnary: true, GoBinary: true, GoString: true, GoNumber: true, GoBoolean: true, GoNil: true, GoSpread: true, GoIndex: true, GoPointer: true, GoSlice: true, GoArray: true, GoMap: true, GoChan: true, GoFuncType: true, GoInterface: true, GoStructType: true, GoUnknown: true, GoFuncLit: true}
	goNestings  = map[string]bool{GoUnconditional: true, GoIfHeader: true, GoConditional: true, GoClosure: true}
	goLocals    = map[string]bool{"var": true, "define": true, "assign": true}
	goStmts     = map[string]bool{"call": true, "define": true, "assign": true, "return": true, "if": true}
	goBlocks    = map[string]bool{GoBlockBody: true, GoBlockPlain: true, GoBlockIf: true, GoBlockThen: true, GoBlockElse: true, GoBlockFor: true, GoBlockSwitch: true, GoBlockCase: true, GoBlockGo: true, GoBlockDefer: true}
	goBindings  = map[string]bool{GoBindVar: true, GoBindDefine: true, GoBindAssign: true, GoBindConst: true, GoBindRange: true, GoBindRangeAssign: true, GoBindTypeSwitch: true, GoBindHandler: true}
	goCalls     = map[string]bool{GoCallRouter: true, GoCallPlain: true}
	goEscapes   = map[string]bool{GoEscapeStore: true, GoEscapeComposite: true, GoEscapeArgument: true, GoEscapeSend: true, GoEscapeMethod: true, GoEscapeReturn: true}
)

// ModelsValid accepts an absent payload (a file of another language) or a
// structurally complete payload of the current version. Older or damaged
// payloads are a cache miss, so the file is re-extracted instead of trusted.
func (g *GoFacts) ModelsValid() bool {
	if g == nil {
		return true
	}
	if g.Version != GoModelsVersion || !g.Gated && (len(g.Vars) > 0 || len(g.Funcs) > 0) {
		return false
	}
	for _, s := range g.Structs {
		if s.Name == "" || s.Line <= 0 || s.EndLine < s.Line {
			return false
		}
		for _, f := range s.Fields {
			if f.Line <= 0 || f.Embedded != (len(f.Names) == 0) || f.Pointer && !f.Embedded || !f.Type.valid(0) {
				return false
			}
		}
	}
	for _, t := range g.Types {
		if t.Name == "" || t.Line <= 0 || !t.Type.valid(0) {
			return false
		}
	}
	for _, m := range g.Methods {
		if m.Receiver == "" || m.Name == "" || m.Line <= 0 || m.Params < 0 || m.Results < 0 || m.Return != nil && (m.Name != "TableName" || !m.Return.valid(0)) {
			return false
		}
	}
	for _, c := range g.Consts {
		if c.Name == "" || c.Line <= 0 || !c.Value.valid(0) || c.Type != nil && !c.Type.valid(0) {
			return false
		}
	}
	for form, n := range g.Unread {
		if form == "" || n <= 0 {
			return false
		}
	}
	for _, v := range g.Vars {
		if len(v.Names) == 0 || v.Line <= 0 || v.Type != nil && !v.Type.valid(0) || !validGoExprs(v.Values) {
			return false
		}
	}
	for _, f := range g.Funcs {
		if !f.valid() {
			return false
		}
	}
	for _, s := range g.Signatures {
		if s.Name == "" || s.Line <= 0 || !s.Result.valid(0) {
			return false
		}
	}
	for _, d := range g.Debug {
		if d == "" {
			return false
		}
	}
	for _, w := range g.Wrappers {
		if w.Name == "" || w.Line <= 0 || w.Param < 0 || !validPasses(w.Passes) || !validPasses(w.Guards) {
			return false
		}
	}
	for name, u := range g.Names {
		if name == "" || u.Calls < 0 || u.Values < 0 || u.Calls+u.Values == 0 {
			return false
		}
		for _, r := range u.Results {
			if r.Use != GoResultArgument && r.Use != GoResultReturn && r.Use != GoResultOther || r.Line <= 0 || r.Result < -1 || r.Arg < 0 {
				return false
			}
		}
	}
	for _, r := range g.Requests {
		if r.Name == "" || r.Line <= 0 || r.Param < 0 || !validPasses(r.Passes) {
			return false
		}
	}
	for _, f := range g.Shapes {
		if !f.valid() {
			return false
		}
	}
	for _, m := range g.Marshalers {
		if m.Receiver == "" || m.Name == "" || m.Line <= 0 {
			return false
		}
	}
	return g.Routes.valid()
}

// validPasses checks request passes: a name or a selector of a name, a
// non-negative argument index.
func validPasses(passes []GoRequestPass) bool {
	for _, p := range passes {
		c := p.Callee
		switch {
		case p.Arg < 0:
			return false
		case c.Kind == GoIdentifier && c.Name != "":
		case c.Kind == GoSelector && c.Name != "" && c.Object != nil && c.Object.Kind == GoIdentifier && c.Object.Name != "":
		default:
			return false
		}
	}
	return true
}

// valid checks the router payload: records, block trees, kinds, indexes and
// spans; a literal's parent precedes it.
func (r *GoRoutes) valid() bool {
	if r == nil {
		return true
	}
	if g := r.Generated; g != nil && (g.Generator == "" || g.Version == "" || g.Framework != "chi" && g.Framework != "gin" && g.Framework != "net/http") {
		return false
	}
	for _, c := range r.Consts {
		if c.Name == "" || c.Line <= 0 || !c.Value.valid(0) || c.Type != nil && !c.Type.valid(0) {
			return false
		}
	}
	for _, v := range r.Vars {
		if len(v.Names) == 0 || v.Line <= 0 || v.Type != nil && !v.Type.valid(0) || !validGoExprs(v.Values) {
			return false
		}
	}
	span := func(s contract.Span) bool {
		return s.StartLine > 0 && s.StartColumn >= 0 && s.EndColumn >= 0 && (s.EndLine > s.StartLine || s.EndLine == s.StartLine && s.EndColumn >= s.StartColumn)
	}
	for i, f := range r.Funcs {
		if !span(f.Span) || f.Parent < 0 || f.Parent > i || f.Block < 0 || f.Parent > 0 && f.Block >= len(r.Funcs[f.Parent-1].Blocks) || f.Parent == 0 && f.Block != 0 {
			return false
		}
		if f.Receiver != nil && !f.Receiver.Type.valid(0) {
			return false
		}
		for _, p := range append(append([]GoParam(nil), f.Params...), f.Results...) {
			if !p.Type.valid(0) {
				return false
			}
		}
		if len(f.Blocks) == 0 || f.Blocks[0].Kind != GoBlockBody || f.Blocks[0].Parent != 0 {
			return false
		}
		for j, b := range f.Blocks[1:] {
			if !goBlocks[b.Kind] || b.Kind == GoBlockBody || b.Parent < 0 || b.Parent > j {
				return false
			}
		}
		block := func(b int) bool { return b >= 0 && b < len(f.Blocks) }
		for _, b := range f.Bindings {
			if !goBindings[b.Kind] || len(b.Names) == 0 || !block(b.Block) || !span(b.Span) || b.Type != nil && !b.Type.valid(0) || !validGoExprs(b.Values) {
				return false
			}
		}
		for _, c := range f.Calls {
			if !goCalls[c.Kind] || c.Call.Kind != GoCall || !block(c.Block) || !span(c.Span) || !c.Call.valid(0) || (c.Kind == GoCallRouter) != (c.Hash != "") || c.Context != "" && c.Context != GoCallStatement && c.Context != GoCallLocal {
				return false
			}
		}
		for _, ret := range f.Returns {
			if !block(ret.Block) || !span(ret.Span) || !validGoExprs(ret.Values) {
				return false
			}
		}
		for _, x := range f.Escapes {
			if !goEscapes[x.Form] || !block(x.Block) || !span(x.Span) || !x.Value.valid(0) || x.Type != nil && !x.Type.valid(0) {
				return false
			}
		}
	}
	return true
}

func (f GoShapeFunc) valid() bool {
	if f.Line <= 0 || f.Column < 0 || f.Name == "" && f.Receiver != "" {
		return false
	}
	for i, p := range f.Blocks {
		if p < 0 || p > i {
			return false
		}
	}
	for _, p := range f.Params {
		if p.Carrier != "" && p.Carrier != GoCarrierGin && p.Carrier != GoCarrierRequest && p.Carrier != GoCarrierWriter || !p.Type.valid(0) {
			return false
		}
	}
	for _, l := range f.Locals {
		if l.Name == "" || l.Line <= 0 || l.Count < 0 || l.Type != nil && !l.Type.valid(0) || l.Value != nil && !l.Value.valid(0) {
			return false
		}
	}
	for _, c := range f.Calls {
		if c.Call.Kind != GoCall || c.Line <= 0 || c.Column < 0 || c.Block < 0 || c.Block > len(f.Blocks) || !c.Call.valid(0) {
			return false
		}
	}
	for _, t := range f.Terminal {
		if t < 0 || t > len(f.Blocks) {
			return false
		}
	}
	for _, n := range f.Reassigned {
		if n == "" {
			return false
		}
	}
	return true
}

func (f GoFunc) valid() bool {
	if f.Name == "" || f.Line <= 0 || f.EndLine < f.Line || f.Receiver != nil && !f.Receiver.Type.valid(0) {
		return false
	}
	for _, p := range append(append([]GoParam(nil), f.Params...), f.Results...) {
		if !p.Type.valid(0) {
			return false
		}
	}
	for _, l := range f.Locals {
		if !goLocals[l.Kind] || len(l.Names) == 0 || l.Line <= 0 || l.Statement < 0 || !goNestings[l.Nesting] || l.Type != nil && !l.Type.valid(0) || !validGoExprs(l.Values) {
			return false
		}
	}
	for _, s := range f.Sites {
		if s.Call.Kind != GoCall || s.Line <= 0 || s.Statement < 0 || !goNestings[s.Nesting] || !s.Call.valid(0) {
			return false
		}
	}
	for _, s := range f.Statements {
		if !goStmts[s.Kind] || s.Line <= 0 || s.Index < 0 || !s.Callee.valid(0) {
			return false
		}
	}
	for _, m := range append(append([]GoMutation(nil), f.Mutations...), f.Stores...) {
		if m.Target.Kind != GoSelector || m.Line <= 0 || m.Statement < 0 || !goNestings[m.Nesting] || !m.Target.valid(0) || m.Value != nil && !m.Value.valid(0) {
			return false
		}
	}
	for _, n := range f.LocalTypes {
		if n == "" {
			return false
		}
	}
	for _, r := range f.Returns {
		if r.Line <= 0 || !goNestings[r.Nesting] || r.Nesting == GoClosure || r.Value != nil && !r.Value.valid(0) {
			return false
		}
	}
	for _, s := range f.Calls {
		if s.Call.Kind != GoCall || s.Line <= 0 || s.Statement < 0 || !goNestings[s.Nesting] || !s.Call.valid(0) {
			return false
		}
	}
	for _, s := range f.Literals {
		if s.Call.Kind != GoComposite || s.Line <= 0 || s.Statement < 0 || !goNestings[s.Nesting] || !s.Call.valid(0) {
			return false
		}
	}
	return true
}

func validGoExprs(exprs []GoExpr) bool {
	for _, e := range exprs {
		if !e.valid(0) {
			return false
		}
	}
	return true
}

// valid checks the kind and the fields each kind requires; depth guards a
// damaged cache entry that nests deeper than the capture ever writes. The
// switch only checks that a required child is present; each child is
// validated once, after it, so the cost is linear in the tree (validating a
// required child twice doubled the cost per level of nesting, T076).
func (e GoExpr) valid(depth int) bool {
	if depth > GoMaxDepth || !goExprKinds[e.Kind] {
		return false
	}
	sub := func(x *GoExpr) bool { return x == nil || x.valid(depth+1) }
	switch e.Kind {
	case GoIdentifier:
		if e.Name == "" {
			return false
		}
	case GoSelector:
		if e.Name == "" || e.Object == nil {
			return false
		}
	case GoCall:
		if e.Callee == nil {
			return false
		}
	case GoUnary, GoBinary:
		if e.Value == "" {
			return false
		}
		if e.Kind == GoUnary && e.Object == nil || e.Kind == GoBinary && (e.Left == nil || e.Right == nil) {
			return false
		}
	case GoBoolean:
		if e.Value != "true" && e.Value != "false" {
			return false
		}
	case GoNumber:
		if e.Value == "" {
			return false
		}
	case GoSpread, GoIndex, GoPointer, GoSlice, GoArray:
		if e.Object == nil {
			return false
		}
	case GoMap:
		if e.Left == nil || e.Right == nil {
			return false
		}
	case GoFuncLit:
		if e.Func < 0 {
			return false
		}
	}
	if e.Kind != GoFuncLit && (e.Func != 0 || len(e.Uses) > 0 || e.Forward != nil || e.Request) {
		return false
	}
	if e.Forward != nil && (e.Forward.Name == "" || !validPasses(e.Forward.Passes) || !validPasses(e.Forward.Guards)) {
		return false
	}
	if !sub(e.Object) || !sub(e.Callee) || !sub(e.Type) || !sub(e.Left) || !sub(e.Right) {
		return false
	}
	for _, a := range e.Args {
		if !a.valid(depth + 1) {
			return false
		}
	}
	for _, el := range e.Elements {
		if el.Key != nil && !el.Key.valid(depth+1) || !el.Value.valid(depth+1) {
			return false
		}
	}
	return true
}
