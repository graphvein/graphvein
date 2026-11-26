package facts

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// TauriSyntaxVersion identifies the Tauri payload shape. Readers treat any other
// version as a cache miss, so a shape change never reuses stale syntax.
const TauriSyntaxVersion = 3

// TauriSyntax caches the Rust syntax Tauri command linking needs and no other
// cache holds: command attributes, generate_handler! token trees and the let
// bindings that carry a builder to invoke_handler. It is syntax only; proof
// that an attribute or macro is Tauri's stays with the Rust resolver at link
// time, so a cached record never asserts a Tauri link.
type TauriSyntax struct {
	Version  int
	Commands []TauriCommand `json:",omitempty"`
	Handlers []TauriHandler `json:",omitempty"`
	Flows    []TauriFlow    `json:",omitempty"`
	Apps     []TauriAppSite `json:",omitempty"`
}

// Valid accepts an absent payload (a file without Tauri syntax) or a
// structurally complete payload of the current version. Older, foreign or
// damaged payloads (missing required kinds, out-of-range handler references)
// are re-extracted instead of trusted, because consumers index into them.
func (t *TauriSyntax) Valid() bool {
	if t == nil {
		return true
	}
	if t.Version != TauriSyntaxVersion {
		return false
	}
	for _, c := range t.Commands {
		if c.Name == "" || !tauriSpanValid(c.Span) || len(c.Attributes) == 0 {
			return false
		}
		for _, a := range c.Attributes {
			if !a.valid() {
				return false
			}
		}
	}
	for _, h := range t.Handlers {
		if !h.valid(len(t.Handlers)) {
			return false
		}
	}
	for _, f := range t.Flows {
		if len(f.Bindings) == 0 || f.Shadows == nil {
			return false
		}
		for _, b := range f.Bindings {
			if !b.valid(len(t.Handlers)) {
				return false
			}
		}
		for _, call := range f.Calls {
			if !call.valid(len(t.Handlers)) {
				return false
			}
		}
		for _, m := range f.Macros {
			if !m.valid() || m.Kind == "attribute" {
				return false
			}
		}
	}
	for _, a := range t.Apps {
		if a.Path == "" || a.Scope < 0 || !tauriSpanValid(a.Span) || !tauriAppKinds[a.Kind] {
			return false
		}
	}
	return true
}

// tauriAppKinds are the TauriAppSite kinds.
var tauriAppKinds = map[string]bool{"root": true, "typed": true, "alias": true, "reexport": true, "macro": true, "invoke_handler": true, "context": true}

func (m TauriMacro) valid() bool {
	return m.Path != "" && m.Scope >= 0 && tauriSpanValid(m.Span) && (m.Kind == "invocation" || m.Kind == "definition" || m.Kind == "attribute")
}

// validUse checks a chain consumer; an unrecognized consumer must have made
// the chain incomplete.
func validUse(use string, chain TauriChain) bool {
	switch use {
	case "statement", "let", "assign", "tail", "return", "argument":
		return true
	case "other":
		return chain.Incomplete
	}
	return false
}

func (c TauriCall) valid(handlers int) bool {
	if c.Chain.Root.Kind != "identifier" || !c.Chain.valid(handlers) || !validUse(c.Use, c.Chain) {
		return false
	}
	for _, m := range c.Chain.Methods {
		if m.Name == "invoke_handler" {
			return true
		}
	}
	return false
}

func tauriSpanValid(s contract.Span) bool {
	return s.StartLine > 0 && s.StartColumn >= 0 && s.EndLine >= s.StartLine && s.EndColumn >= 0 && (s.EndLine > s.StartLine || s.EndColumn > s.StartColumn)
}

func (a TauriAttribute) valid() bool {
	if a.Path == "" || !tauriSpanValid(a.Span) || a.Scope < 0 {
		return false
	}
	switch a.Form {
	case "attribute":
		if a.Predicate != "" {
			return false
		}
	case "cfg_attr":
		if a.Predicate == "" {
			return false
		}
	default:
		return false
	}
	for _, arg := range a.Arguments {
		if arg.Key == "" || (arg.Kind == "string") != (arg.Value != nil) {
			return false
		}
		switch arg.Kind {
		case "string", "literal":
		case "expression", "path", "list":
			if a.Invalid == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (h TauriHandler) valid(handlers int) bool {
	if h.Path == "" || h.Owner == "" || h.Scope < 0 || !tauriSpanValid(h.Span) || h.Invalid != "" && (len(h.Entries) > 0 || h.Plugin != "") {
		return false
	}
	for _, a := range h.Attributes {
		if !a.valid() || a.Kind != "attribute" {
			return false
		}
	}
	for _, e := range h.Entries {
		if e.Path == "" || len(e.Segments) == 0 || strings.TrimPrefix(e.Path, "::") != strings.Join(e.Segments, "::") || !tauriSpanValid(e.Span) || e.Hash == "" {
			return false
		}
	}
	c := h.Context
	switch c.Kind {
	case "argument":
		if c.Method == "" || c.Chain == nil || !c.Chain.valid(handlers) || !validUse(c.Use, *c.Chain) {
			return false
		}
	case "let":
		if c.Binding == "" || c.Chain != nil || c.Use != "" {
			return false
		}
	case "other":
		if c.Chain != nil || c.Use != "" {
			return false
		}
	default:
		return false
	}
	return true
}

func (c TauriChain) valid(handlers int) bool {
	r := c.Root
	switch r.Kind {
	case "call":
		if r.Span == nil || !tauriSpanValid(*r.Span) {
			return false
		}
	case "identifier":
		if r.Name == "" {
			return false
		}
	case "other":
		if r.Syntax == "" || !c.Incomplete {
			return false
		}
	default:
		return false
	}
	for _, m := range c.Methods {
		if m.Name == "" || !tauriSpanValid(m.Span) || m.Handler != nil && (*m.Handler < 0 || *m.Handler >= handlers) {
			return false
		}
		// Every invoke_handler call is either a captured handler reference
		// or explicitly Uncaptured, so a replacement is never invisible.
		if m.Uncaptured && (m.Name != "invoke_handler" || m.Handler != nil) || m.Name == "invoke_handler" && m.Handler == nil && !m.Uncaptured {
			return false
		}
	}
	return true
}

func (b TauriBinding) valid(handlers int) bool {
	if b.Name == "" || !tauriSpanValid(b.Block) || b.End <= b.Offset {
		return false
	}
	if b.Kind != "let" && b.Kind != "assign" {
		return false
	}
	switch b.Value {
	case "plain":
		if len(b.Alternatives) != 1 || !b.Complete {
			return false
		}
	case "if", "match", "block":
	case "unsupported", "none":
		if len(b.Alternatives) != 0 || b.Complete {
			return false
		}
	default:
		return false
	}
	for _, a := range b.Alternatives {
		if !a.valid(handlers) {
			return false
		}
	}
	return true
}

// TauriCommand is a function item with at least one attribute that could be
// #[tauri::command]. Identity is the existing function or method node, empty
// when the extractor emitted none. Attributes holds every qualifying attribute
// in source order, so a Tauri attribute after poise's or clap's is not lost;
// consumers resolve each one. Cfg is set when the function's own attributes
// include #[cfg(..)] or a cfg_attr (at any nesting) that applies cfg(..).
// Test (version 3) is set when the function or an enclosing item or
// statement of the same file carries a cfg whose predicate holds only in test
// builds (CfgTestOnly), so production calls never link to it.
type TauriCommand struct {
	Identity   string `json:",omitempty"`
	Name       string
	Span       contract.Span
	Attributes []TauriAttribute
	AsyncFn    bool   `json:",omitempty"`
	Visibility string `json:",omitempty"`
	Cfg        bool   `json:",omitempty"`
	Test       bool   `json:",omitempty"`
}

// TauriAttribute keeps the attribute path and its arguments as written. Scope
// is the RustFacts scope index from which the resolver resolves Path, so
// consumers can reject clap's or poise's command attributes by resolution.
// Predicate is the cfg_attr predicate with line endings normalized to LF;
// for nested cfg_attr the predicates of every level, outermost first, are
// joined in one `all(p1, p2, ...)`, each kept as written.
type TauriAttribute struct {
	Path      string
	Span      contract.Span
	Form      string
	Predicate string          `json:",omitempty"`
	Arguments []TauriArgument `json:",omitempty"`
	Async     bool            `json:",omitempty"`
	Invalid   string          `json:",omitempty"`
	Scope     int
}

// TauriArgument is one attribute argument. Value is set only for a decoded
// string literal, so an unknown value is never mistaken for an absent one.
type TauriArgument struct {
	Key   string
	Value *string
	Kind  string
}

// TauriHandler is a generate_handler! invocation with its parsed entries.
// Invalid is set, and Entries is empty, when the token tree is not the
// macro's input grammar, so a malformed handler never yields guessed entries.
// Attributes (version 3) lists the outer attributes of the owner function,
// with the attributes inside each cfg_attr flattened after it, because an
// attribute macro may rewrite the body that holds the builder chain. Test
// (version 3) is set when the handler sits in an item or statement whose cfg
// holds only in test builds (CfgTestOnly).
type TauriHandler struct {
	Path       string
	Span       contract.Span
	Scope      int
	Owner      string
	Plugin     string       `json:",omitempty"`
	Entries    []TauriEntry `json:",omitempty"`
	Invalid    string       `json:",omitempty"`
	Context    TauriContext
	Attributes []TauriMacro `json:",omitempty"`
	Test       bool         `json:",omitempty"`
}

// TauriAppSite (version 3) is syntax that may belong to a Tauri app other
// than the one a proven registry builds. Builder-like names are `Builder`
// and the file's use aliases of it. Kind is
//   - "root": a value-position path through a Builder-like segment (a
//     constructor call, `<tauri::Builder<R>>::default()`, a function path
//     value such as `tauri::Builder::<R>::default`); Path is the path up to
//     that segment and Span the call when the path is a callee;
//   - "typed": a let, return or struct field whose type is directly a
//     Builder-like path, for a let or return only when the value is a
//     `default` / `new` call naming no Builder-like segment
//     (`Default::default()`); Path is the type's path;
//   - "alias": a type alias of a Builder-like path;
//   - "reexport": a visible use of a Builder-like name (Path is its full
//     path) or of a glob not rooted at crate, self or super (`prefix::*`);
//   - "macro": a macro_rules! body or macro invocation whose token trees
//     name a Builder-like identifier (Path is its adjacent path, plus
//     `tauri::Builder` when the trees also name `tauri`);
//   - "invoke_handler": an invoke_handler call (method or path form) whose
//     arguments hold no generate_handler! invocation;
//   - "context": a generate_context! invocation.
//
// Paths are resolved from Scope at link time; Test is set inside an item or
// statement whose cfg holds only in test builds. The filter is syntactic and
// wide (tempfile::Builder is kept); only the resolver decides that a site is
// Tauri's. Type positions in function signatures are not recorded: they
// build no app (T015c2 Revision 3, MA2).
type TauriAppSite struct {
	Kind  string
	Path  string
	Scope int
	Span  contract.Span
	Test  bool `json:",omitempty"`
}

// TauriEntry is one handler entry path. Span and Hash cover the path only,
// so a future command node keeps its identity when entry attributes change.
type TauriEntry struct {
	Path       string
	Segments   []string
	Span       contract.Span
	Hash       string
	Attributes []string `json:",omitempty"`
}

// TauriContext says where the macro sits: the sole argument of a method call
// (Method and the whole receiver Chain), a let initializer (Binding) or other.
// Use says what consumes the chain's value (statement, let, assign, tail,
// return, argument or other), because a builder whose value escapes (a tail
// or return value, a call argument) may receive a later invoke_handler that
// replaces this one; consumers may treat only statement, let and assign
// chains as final.
type TauriContext struct {
	Kind    string
	Method  string      `json:",omitempty"`
	Binding string      `json:",omitempty"`
	Chain   *TauriChain `json:",omitempty"`
	Use     string      `json:",omitempty"`
}

// TauriChain is a method chain from its root outward. Builder replacement
// semantics depend on order, so every method is kept, including later ones.
// Parentheses are transparent. Incomplete is set when the chain passes
// through `?` or `.await` between its root and its outermost method, when the
// root is not a call or identifier, or when its value is consumed by an
// unrecognized expression, so a consumer never reads such a chain as proof.
type TauriChain struct {
	Root       TauriRoot
	Methods    []TauriMethod `json:",omitempty"`
	Incomplete bool          `json:",omitempty"`
}

// TauriRoot is the receiver at the bottom of a chain: a call (Callee, Generic,
// a decoded string first Argument, Span), an identifier (Name) or other
// (Syntax names the node kind). Offset is the root's start byte.
type TauriRoot struct {
	Kind     string
	Callee   string         `json:",omitempty"`
	Generic  bool           `json:",omitempty"`
	Argument *string        `json:",omitempty"`
	Span     *contract.Span `json:",omitempty"`
	Name     string         `json:",omitempty"`
	Syntax   string         `json:",omitempty"`
	Offset   uint32
}

// TauriMethod is one method call in a chain. Handler indexes the file's
// Handlers when the call's sole argument is a captured generate_handler!.
// Uncaptured marks an invoke_handler call whose argument is anything else (a
// let-bound handler, a closure, a missing or extra argument): it still
// replaces the builder's handler, so consumers must see it.
type TauriMethod struct {
	Name       string
	Span       contract.Span
	Handler    *int `json:",omitempty"`
	Uncaptured bool `json:",omitempty"`
}

// TauriFlow keeps, for one function that contains a captured handler, the
// bindings reachable from the identifier roots of its invoke_handler chains,
// so consumers can trace a receiver through rebindings and cfg-gated branches
// to its builder. Calls lists every identifier-rooted chain in the function
// that calls invoke_handler, captured or not, in source order, because a later
// call replaces the handler a binding carries. Shadows lists, in sorted order,
// the flow names that a binder other than a recorded binding (function or
// closure parameter, if let, while let, match arm, match guard or for
// pattern, a let with a destructuring pattern) may bind at one of the flow's
// identifier root sites, and every flow name that appears as an identifier in
// a macro invocation in the function body (a macro may rebind it); a consumer
// treats such a name as unproven. Macros lists every macro invocation and
// macro_rules! definition in the body (version 2), because a macro the
// consumer cannot see into may assign a local without naming it in its tokens.
type TauriFlow struct {
	Function string
	Bindings []TauriBinding
	Calls    []TauriCall `json:",omitempty"`
	Shadows  []string
	Macros   []TauriMacro `json:",omitempty"`
}

// TauriMacro is one macro invocation or macro_rules! definition in a flow
// function body (closures included, nested function items excluded), in
// source order. Path is the compact macro path for an invocation and the
// defined name for a definition; Scope is the RustFacts scope index from
// which the resolver resolves Path. Invocations nested in another macro's
// token tree (`info!("{}", env!("X"))`) are recorded with the outer
// invocation's scope, because a token tree has no scopes of its own. Kind is
// "invocation" or "definition".
type TauriMacro struct {
	Path  string
	Scope int
	Span  contract.Span
	Kind  string
}

// TauriCall is one identifier-rooted chain that calls invoke_handler, with
// the consumer of its value (as TauriContext.Use).
type TauriCall struct {
	Chain TauriChain
	Use   string
}

// TauriBinding is one let or assignment of a reachable name. Alternatives hold
// one chain for a plain value and every branch tail for if, match and block
// values; Complete is false when a branch has no tail or the form is
// unsupported, so a missing alternative is never read as none. Cfg is set
// when the statement or an enclosing statement carries a cfg (directly or
// through cfg_attr). Conditional is set when the statement is not in the
// function body's top-level statement list (inside an if, match, loop,
// closure, async or plain block), so consumers treat it as a possible, never
// a definite, rebinding. Offset and End are the statement's start and end
// bytes (End since version 2): a let binds its name only after its own
// statement, so a root inside the initializer of `let x = x.m();` refers to
// the earlier x.
type TauriBinding struct {
	Name         string
	Kind         string
	Block        contract.Span
	Offset       uint32
	End          uint32
	Cfg          bool `json:",omitempty"`
	Conditional  bool `json:",omitempty"`
	Value        string
	Alternatives []TauriChain `json:",omitempty"`
	Complete     bool
}

// CfgTestOnly reports whether a cfg predicate holds only in test builds: it is
// exactly `test`, or an `all(..)` with an item that is itself test-only.
// `any(..)` and `not(..)` never qualify. The predicate is compared without
// whitespace; an attribute text `#[cfg(<predicate>)]` is accepted too.
func CfgTestOnly(predicate string) bool {
	p := strings.Join(strings.Fields(predicate), "")
	if inner, ok := strings.CutPrefix(p, "#[cfg("); ok {
		if !strings.HasSuffix(inner, ")]") {
			return false
		}
		p = strings.TrimSuffix(inner, ")]")
	}
	if p == "test" {
		return true
	}
	inner, ok := strings.CutPrefix(p, "all(")
	if !ok || !strings.HasSuffix(inner, ")") {
		return false
	}
	for _, item := range splitPredicates(strings.TrimSuffix(inner, ")")) {
		if CfgTestOnly(item) {
			return true
		}
	}
	return false
}

// splitPredicates splits a cfg predicate list at top-level commas, skipping
// string literals, so `feature = "a,b"` stays one item.
func splitPredicates(list string) []string {
	var out []string
	depth, start, quoted := 0, 0, false
	for i := 0; i < len(list); i++ {
		switch c := list[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, list[start:i])
			start = i + 1
		}
	}
	return append(out, list[start:])
}

// CfgApplies reports whether an attribute text (`#[..]`, whitespace allowed)
// applies a cfg: `#[cfg(..)]`, or a `#[cfg_attr(<predicate>, ..)]` whose
// attribute list holds cfg(..) directly or in a nested cfg_attr.
func CfgApplies(attribute string) bool {
	a := strings.Join(strings.Fields(attribute), "")
	if strings.HasPrefix(a, "#[cfg(") {
		return true
	}
	inner, ok := strings.CutPrefix(a, "#[cfg_attr(")
	if !ok || !strings.HasSuffix(inner, ")]") {
		return false
	}
	return cfgAttrApplies(strings.TrimSuffix(inner, ")]"))
}

func cfgAttrApplies(list string) bool {
	items := splitPredicates(list)
	for _, item := range items[1:] {
		if strings.HasPrefix(item, "cfg(") {
			return true
		}
		if nested, ok := strings.CutPrefix(item, "cfg_attr("); ok && strings.HasSuffix(nested, ")") && cfgAttrApplies(strings.TrimSuffix(nested, ")")) {
			return true
		}
	}
	return false
}
