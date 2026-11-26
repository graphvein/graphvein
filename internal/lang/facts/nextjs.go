package facts

import "github.com/graphvein/graphvein/internal/contract"

// NextFacts is the TypeScript/JavaScript payload the Next.js route linker
// (T024a, ADR 0077) reads at link time. It holds only syntax the shared
// export facts do not keep, so route files and next.config files are never
// parsed again after the cache.
type NextFacts struct {
	// DestructuredExports names the bindings of top-level destructuring
	// exports (`export const { GET, POST } = handlers`), which declare
	// exported names without a declaration node of their own.
	DestructuredExports []NamedSpan `json:",omitempty"`
	// DefaultExport is the span of the statement that sets the module's
	// default export (`export default ...` or a top-level
	// `module.exports = ...`), read for Pages Router API files (row NX-R3).
	DefaultExport *contract.Span `json:",omitempty"`
	// Config is the literal reading of a next.config.* file (rows NX-P2
	// and NX-P3); nil for every other file.
	Config *NextConfig `json:",omitempty"`
}

// NamedSpan is one exported name and the statement that declares it.
type NamedSpan struct {
	Name string
	Span contract.Span
}

// NextConfig keeps the literal values of the keys Graphvein reads from a
// next.config file. Each key has a state: "absent" (Next.js default),
// "literal" (the value fields hold it) or "unread" (any other form, which
// Graphvein never guesses).
type NextConfig struct {
	BasePath            string   `json:",omitempty"`
	BasePathState       string   `json:",omitempty"`
	TrailingSlash       bool     `json:",omitempty"`
	TrailingSlashState  string   `json:",omitempty"`
	PageExtensions      []string `json:",omitempty"`
	PageExtensionsState string   `json:",omitempty"`
	// Rewrites holds the rules when RewritesState is "literal".
	Rewrites      []NextRewrite `json:",omitempty"`
	RewritesState string        `json:",omitempty"`
	// Rules are the redirects, rewrites and headers rules the route shape
	// rows read (T029 SH-N24); RulesUnread names the keys whose rules are
	// not read (any of them may apply to every route). A key in neither
	// is absent.
	Rules       []NextConfigRule `json:",omitempty"`
	RulesUnread []string         `json:",omitempty"`
	// Shape is "object" when the default export is read as a config
	// object (directly, through a const or one plugin call) and "unread"
	// otherwise; with "unread" every key is unread.
	Shape string `json:",omitempty"`
}

// NextConfigRule is one next.config redirects, rewrites or headers rule
// as the route shape rows read it.
type NextConfigRule struct {
	// Key is "redirects", "rewrites" or "headers"; Phase is the rewrite
	// phase ("beforeFiles", "afterFiles" or "fallback").
	Key   string `json:"key"`
	Phase string `json:"phase,omitempty"`
	// Source is the literal source, "" when it is not read (the rule may
	// match any path).
	Source string `json:"source,omitempty"`
	// Conditional marks a rule with has or missing conditions (or a part
	// not read that may add them): it may not apply.
	Conditional bool `json:"conditional,omitempty"`
	// HeaderKeys are the header names a headers rule sets, as written,
	// when HeaderKeysRead says every name of its list is read (T029 NXH).
	// A rule whose names are not read may set any header, Content-Type
	// included. Only headers rules record them.
	HeaderKeys     []string `json:"header_keys,omitempty"`
	HeaderKeysRead bool     `json:"header_keys_read,omitempty"`
}

// NextRewrite is one literal rewrite rule of next.config `rewrites`.
type NextRewrite struct {
	// Phase is "beforeFiles", "afterFiles" or "fallback"; an array return
	// is "afterFiles", as in Next.js.
	Phase       string `json:"phase"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// ActionFacts is the server action payload (T024b, ADR 0077, rows SA of
// docs/extract/server-actions.md). It keeps file-local syntax only: the
// file's directive prologue, the top-level exports of a 'use server'
// module, the functions whose own prologue holds 'use server', and the
// JSX attribute values that may name an action. Which binding a use
// names is decided at link time through the route scopes and the module
// resolver, so nothing here depends on another file.
type ActionFacts struct {
	// Directives are the string-literal expression statements that open
	// the file (TypeScript compiler semantics), in order.
	Directives []string `json:",omitempty"`
	// Exports are the top-level exports of a file whose prologue holds
	// 'use server' (row SA-M1); empty for every other file.
	Exports []ActionExport `json:",omitempty"`
	// Inline are the functions whose body prologue holds 'use server'
	// (row SA-I1), in source order.
	Inline []InlineAction `json:",omitempty"`
	// Attributes are the JSX attributes whose value is an expression
	// container, with the route scope of the attribute (rows SA-C1, D).
	Attributes []ActionAttribute `json:",omitempty"`
	// Ternaries are the conditional expressions with at least one plain
	// identifier among their (nested) alternatives, by start byte, so a
	// conditional action value is read as its alternatives (R1).
	Ternaries []ActionTernary `json:",omitempty"`
	// Loads are the module specifiers of the file's dynamic import() and
	// require() calls with a string literal argument, in source order;
	// LoadsUnread is set when one has any other argument. Row SA-C3's
	// server graph follows them (T024 review).
	Loads       []string `json:",omitempty"`
	LoadsUnread bool     `json:",omitempty"`
}

// ActionTernary is one conditional expression `c ? a : b` (nested
// conditionals and parentheses flattened). Leaves are its alternatives
// that are plain identifiers; Complete is false when another expression
// is among them.
type ActionTernary struct {
	Offset   int
	Leaves   []string
	Complete bool `json:",omitempty"`
}

// ActionExport is one top-level export of a 'use server' module. Form is
// one of: async_function, const_async_arrow, const_async_function,
// const_call, clause (Local names the local binding), imported_clause
// (`export { a }` of an imported binding: Module and Local name the import),
// default_async_function, default_async_arrow, default_call,
// default_clause (`export default a` of a local action binding; Local
// names it), or a refused form (sync_function, non_function,
// class, reexport, star, default_other, let_binding).
type ActionExport struct {
	Name, Local, Form string
	// Module and Imported name the import of an imported_clause export.
	Module   string `json:",omitempty"`
	Imported string `json:",omitempty"`
	// Identity is the node the extractor emitted for the exported binding,
	// when it emitted one.
	Identity string `json:",omitempty"`
	Span     contract.Span
	Hash     string
}

// InlineAction is a function whose own body prologue holds 'use server'.
// Scope is the function's own route scope (the ReturnScope of its
// binding), so a use resolves to it through the route scopes.
type InlineAction struct {
	// Name is the bound name; empty for an anonymous function.
	Name string `json:",omitempty"`
	// Form is named_function, const_arrow, const_function, let_binding
	// (a let or var binding), anonymous, or a refused form (sync, method).
	Form  string
	Async bool `json:",omitempty"`
	// Reassigned marks a let or var binding whose name is assigned
	// somewhere in the file: a use of the name may then name another
	// value, so it is not linked.
	Reassigned bool `json:",omitempty"`
	Scope      int
	// Offset is the function's start byte, matching RouteExpr.Offset of
	// an attribute value that is this function.
	Offset int
	// Identity is the function's own node; Owner the nearest enclosing
	// emitted owner (the file for module level).
	Identity         string `json:",omitempty"`
	Owner, OwnerName string
	Span             contract.Span
	Hash             string
}

// ActionAttribute is one JSX attribute `name={value}`.
type ActionAttribute struct {
	Name string
	// Element is the name of the JSX element the attribute is written on
	// as written (`form`, `button`, `Form`, `ui.Form`); empty when unknown.
	Element          string `json:",omitempty"`
	Value            RouteExpr
	Scope            int
	Owner, OwnerName string
	Span             contract.Span
	Hash             string
}
