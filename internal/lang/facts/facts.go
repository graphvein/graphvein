// Package facts holds source-derived candidates shared by the language extractors.
// It is an internal cache format; the application still consumes contract records.
package facts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/parse"
)

// Import retains source bindings for resolution against the current inventory.
type Import struct {
	Module        string
	Bindings      map[string]string
	Span          contract.Span
	From          bool
	Scope         string
	ModuleScope   string
	ExplicitAlias bool
}

// Call retains a callee and lexical scope without caching a cross-file edge.
type Call struct {
	StaticName          string
	Source, Scope, Name string
	ModuleScope         string
	Span                contract.Span
	Shadowed            bool
	// Lexical is the qualified name of the file's declaration the call's
	// lexical binding names, set by the TypeScript/JavaScript extractor
	// when link time's qualified-name lookup would pick another
	// declaration of the name (row TSX-22): link time resolves the call to
	// this declaration instead.
	Lexical string `json:",omitempty"`
	// LexicalImport marks a TypeScript/JavaScript call whose lexical
	// binding is an import although a declaration of the file has the
	// callee's qualified name (row TSX-23): link time resolves it through
	// the module resolver only.
	LexicalImport bool `json:",omitempty"`
	// MemberWritten marks a TypeScript/JavaScript member call that the
	// extractor leaves unresolved because code of the file writes the
	// member (row TSX-25), or a property of its name through a receiver of
	// unknown owner (row TSX-26): link time does not resolve it through the
	// static member resolver of ADR 0026 either.
	MemberWritten bool `json:",omitempty"`
}

// Facts caches declarations and candidates rather than potentially stale links.
type Facts struct {
	Rust   *RustFacts   `json:",omitempty"`
	Tauri  *TauriSyntax `json:",omitempty"`
	Python *PythonFacts `json:",omitempty"`
	// Go is the Go model payload (ADR 0063): type shapes of every Go file and
	// the GORM sites of gated files; absent for every other language.
	Go *GoFacts `json:",omitempty"`
	// BestEffort is the Java, C# and Dart payload (T021, ADR 0067);
	// absent for every other language.
	BestEffort *BestEffortFacts `json:",omitempty"`
	// TypeScript is the TypeScript/JavaScript structural payload (ADR
	// 0070); absent when the file has nothing to report.
	TypeScript    *TypeScriptFacts `json:",omitempty"`
	ExportClauses []ExportClause   `json:",omitempty"`
	Routes        *RouteFacts      `json:",omitempty"`
	// Shape is the route shape syntax (T029 SH-E): the parameter uses of
	// the file's functions and its top-level type declarations; absent
	// when the file has neither.
	Shape *ShapeSyntax `json:",omitempty"`
	// Next is the Next.js route payload (ADR 0077); absent when the file
	// has nothing for it.
	Next *NextFacts `json:",omitempty"`
	// Actions is the server action payload (T024b, ADR 0077); absent when
	// the file has neither a directive, an inline action nor a JSX
	// expression attribute.
	Actions *ActionFacts `json:",omitempty"`
	// Repeated keeps ambiguous callable groups so relinking cannot select a
	// different body merely because declarations now have unique identities.
	Repeated     map[string][]string
	UnknownSites []UnknownSite
	Nodes        []contract.Node
	Edges        []contract.Edge
	Imports      []Import
	Calls        []Call
	// MemberWrittenNames are the property names a TypeScript/JavaScript
	// file writes through receivers whose owner the call binding model
	// does not know (row TSX-26), "*" when such a key is not static. Link
	// time does not resolve a static member call (ADR 0026) of the file
	// whose key path has one of them, as the extractor does for the
	// calls it checks (MemberWritten).
	MemberWrittenNames []string `json:",omitempty"`
	Exports            map[string]string
	Diagnostics        []contract.Diagnostic
	Package            string
	Modules            []Module
}

// TypeScriptFacts carries what the TypeScript structural claim contract
// (docs/lang/typescript-structure.md) needs to survive the fact cache.
type TypeScriptFacts struct {
	// Unread counts, by form, the in-scope forms the extractor sent to the
	// default branch D (the ts_structure_unread diagnostic).
	Unread map[string]int `json:",omitempty"`
	// Rejected marks a file tree-sitter reports an error in (row TS-F2).
	Rejected bool `json:",omitempty"`
	// NotParsed marks a file the indexer did not parse or extract (row
	// TS-F3), so its structure label falls to best_effort. Such facts are
	// never written to the fact cache.
	NotParsed bool `json:",omitempty"`
	// Bridges is the syntax of the JSX render, passed-function and
	// react-router rows (T026, docs/lang/typescript-bridges.md);
	// the linker resolves it against the current inventory.
	Bridges *BridgeSyntax `json:",omitempty"`
}

// BridgeSyntax keeps the per-file sites the bridge rows read. Nothing in it
// is resolved: names are linked anew on every run, like calls.
type BridgeSyntax struct {
	// Sites are JSX element tags (Row "jsx") and function values passed to
	// a call or a JSX attribute (Rows "argument", "property", "attribute").
	Sites []BridgeSite `json:",omitempty"`
	// Lazy are module-level `const X = lazy(() => import("m"))` bindings.
	Lazy []LazyComponent `json:",omitempty"`
	// Routes are the react-router route candidates of the file: object
	// literals in router positions and <Route> elements, in source order.
	Routes []RouteCandidate `json:",omitempty"`
	// RouterCalls are calls whose callee is named like a react-router
	// router factory (createBrowserRouter, useRoutes, ...).
	RouterCalls []RouterCall `json:",omitempty"`
	// Consts are module-level const initializers a route position may
	// name: an object candidate or an array of route items.
	Consts map[string]RouteValue `json:",omitempty"`
}

// BridgeSite is one name at a source position with its lexical scope.
type BridgeSite struct {
	Row                 string `json:",omitempty"`
	Source, Scope, Name string
	Span                contract.Span
	Shadowed            bool `json:",omitempty"`
}

// LazyComponent is a module-level React.lazy binding of a dynamic import.
type LazyComponent struct {
	Name   string
	Callee BridgeSite
	Module string
}

// RouteValue is a route position's value: one object candidate
// (Candidate >= 0) or an array of items.
type RouteValue struct {
	Candidate int
	Items     []RouteItem `json:",omitempty"`
	// Other marks a value of any other form (a call, a conditional).
	Other bool `json:",omitempty"`
}

// RouteItem is one element of a route array: an object candidate, a name,
// a spread of a name, or another form.
type RouteItem struct {
	Candidate int
	Ref       *BridgeSite `json:",omitempty"`
	Spread    bool        `json:",omitempty"`
	Other     bool        `json:",omitempty"`
}

// RouterCall is a call that may create a react-router router from routes.
type RouterCall struct {
	Callee BridgeSite
	Routes RouteValue
}

// RouteCandidate is an object literal or <Route> element that is a
// react-router route when a proven router or Route reaches it.
type RouteCandidate struct {
	// Qualified and Display are the ui_route name (identity.md), final.
	Qualified, Display string
	Span               contract.Span
	ContentHash        string
	// Owner is the identity of the containing route or node (the file
	// when none).
	Owner string
	// Parent is the index of the literal parent route, -1 when none.
	Parent int
	// Tag is the <Route> element's tag; empty Name for an object literal.
	Tag BridgeSite
	// Path is the literal path; PathForm is "literal", "dynamic",
	// "index" or "absent".
	Path, PathForm string
	// Open marks a spread or computed key that may supply any key.
	Open bool `json:",omitempty"`
	// Keys holds the identifier values of Component, loader and action;
	// Present lists every route key written (static definitions win over
	// lazy ones).
	Keys    map[string]BridgeSite `json:",omitempty"`
	Present []string              `json:",omitempty"`
	// Element lists the JSX tags of a JSX `element` value (outside nested
	// functions); ElementOther marks an element of any other form.
	Element      []BridgeSite `json:",omitempty"`
	ElementOther bool         `json:",omitempty"`
	// Lazy is the `lazy` value: nil when absent.
	Lazy *RouteLazy `json:",omitempty"`
	// Children are the items of a literal `children` array (objects only:
	// <Route> children are candidates with Parent set).
	Children []RouteItem `json:",omitempty"`
}

// RouteLazy is a route's lazy function in one of the read forms: "module"
// (`() => import("m")`: the module's exports are the route's properties),
// "object" (a returned object literal whose properties are named exports
// of imported modules) or "unknown".
type RouteLazy struct {
	Form   string
	Module string                `json:",omitempty"`
	Props  map[string]LazyImport `json:",omitempty"`
	Span   contract.Span
}

// LazyImport is one property of an "object" lazy form: the named export
// of a dynamically imported module, or Unknown.
type LazyImport struct {
	Module, Export string
	Unknown        bool `json:",omitempty"`
}

// ExportClause keeps export syntax so barrels and imported relays can be relinked.
type ExportClause struct {
	Module, Name, Alias       string
	Star, Namespace, TypeOnly bool
	Span                      contract.Span
}

// UnknownSite preserves uncertain calls without inventing graph edges.
type UnknownSite struct {
	Name   *string       `json:"name"`
	Span   contract.Span `json:"span"`
	Caller *string       `json:"caller"`
}

// Unknown records the static final callee segment, if available.
func Unknown(call Call) UnknownSite {
	name := strings.ReplaceAll(call.Name, "::", ".")
	if call.StaticName != "" {
		name = call.StaticName
	}
	name = strings.TrimPrefix(strings.TrimPrefix(name, "@receiver:"), "@self:")
	parts := strings.Split(name, ".")
	name = parts[len(parts)-1]
	site := UnknownSite{Span: call.Span}
	if name != "" {
		site.Name = &name
	}
	kind, _, _, _ := contract.ParseIdentity(call.Source)
	if kind != "file" && call.Source != "" {
		caller := call.Source
		site.Caller = &caller
	}
	return site
}

// Module retains Rust declarations for inventory-only module resolution.
type Module struct {
	Path                    string
	Name, Scope             string
	Inline, UnsupportedPath bool
	Span                    contract.Span
}

// Span translates detached parser locations to contract byte columns.
func Span(n *parse.Node) contract.Span {
	return contract.Span{StartLine: int(n.Row) + 1, StartColumn: int(n.Column), EndLine: int(n.EndRow) + 1, EndColumn: int(n.EndColumn)}
}

// Text permits optional grammar fields without inventing names.
func Text(n *parse.Node) string {
	if n == nil {
		return ""
	}
	return n.Text
}

// ID uses the contract's canonical escaping for every language.
func ID(kind, file, name string) string { v, _ := contract.Identity(kind, file, name); return v }

// Edge records a syntax-backed relation with its exact source evidence.
func Edge(kind, source, target, file string, span contract.Span) contract.Edge {
	return contract.Edge{Type: "edge", Kind: kind, Source: source, Target: target, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: file, Span: span}}}
}

// Add records lexical containment and hashes the complete declaration span.
func Add(out *Facts, file string, source []byte, n *parse.Node, kind, name, qualified, owner string, attrs map[string]any) string {
	span := Span(n)
	identity := ID(kind, file, qualified)
	if owner == "" {
		owner = ID("file", file, file)
	}
	out.Nodes = append(out.Nodes, contract.Node{Type: "node", Identity: identity, Kind: kind, Level: 4, Name: name, QualifiedName: qualified, File: file, Span: &span, ContentHash: contract.ContentHash(source[n.Start:n.End]), Attributes: attrs})
	out.Edges = append(out.Edges, Edge("contains", owner, identity, file, span))
	return identity
}

// External deduplicates ecosystem nodes without treating third-party imports as errors.
func External(nodes map[string]contract.Node, ecosystem, name string) string {
	q := ecosystem + ":" + name
	identity := ID("external", ".", q)
	attrs := map[string]any{"ecosystem": ecosystem, "name": name}
	hash, _ := contract.SyntheticHash(attrs)
	nodes[identity] = contract.Node{Type: "node", Identity: identity, Kind: "external", Level: 1, Name: name, QualifiedName: q, File: ".", ContentHash: hash, Attributes: attrs}
	return identity
}

// Unresolved exposes the count rather than inventing call targets.
func Unresolved(file string, external, unknown int) []contract.Diagnostic {
	if external+unknown == 0 {
		return nil
	}
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "unresolved_calls", Message: fmt.Sprintf("%d calls could not be resolved without a type checker (external=%d, unknown=%d)", external+unknown, external, unknown)}}
}

// Builtin matches a fixed language vocabulary without executing user code.
func Builtin(name, vocabulary string) bool {
	return name != "" && strings.Contains(" "+vocabulary+" ", " "+name+" ")
}

// Finish deterministically merges evidence for contract edge keys.
func Finish(nodes map[string]contract.Node, edges []contract.Edge) ([]contract.Node, []contract.Edge) {
	result := []contract.Node{}
	for _, n := range nodes {
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Identity < result[j].Identity })
	merged := map[string]contract.Edge{}
	for _, e := range edges {
		key := e.Kind + "\x00" + e.Source + "\x00" + e.Target
		if old, ok := merged[key]; ok {
			old.Evidence = append(old.Evidence, e.Evidence...)
			merged[key] = old
		} else {
			merged[key] = e
		}
	}
	keys := []string{}
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	edges = nil
	for _, key := range keys {
		edges = append(edges, merged[key])
	}
	return result, edges
}

// Paths gives linkers stable source order independent of Go map iteration.
func Paths(files map[string]Facts) []string {
	paths := []string{}
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Join preserves lexical ownership with the contract's dot separator.
func Join(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

// CompactPython removes trivia outside literals from overload parameter text.
func CompactPython(s string) string {
	var b strings.Builder
	var quote byte
	triple := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == quote {
				if !triple {
					quote = 0
				} else if i+2 < len(s) && s[i+1] == quote && s[i+2] == quote {
					b.WriteString(s[i+1 : i+3])
					i += 2
					quote = 0
					triple = false
				}
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
			if i+2 < len(s) && s[i+1] == c && s[i+2] == c {
				b.WriteString(s[i+1 : i+3])
				i += 2
				triple = true
			}
			continue
		}
		if c == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' && c != '\f' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// BestEffortVersion identifies the best-effort payload shape (T021, ADR
// 0067). Readers treat any other version as a cache miss.
const BestEffortVersion = 1

// BestEffortFacts is the per-file syntax the best-effort linkers (Java, C#,
// Dart) read: the package, declared type paths and namespaces that
// import resolution looks up, and the import directives as written. It is
// syntax only; resolution happens at link time against the current
// inventory.
type BestEffortFacts struct {
	Version int
	// Refused marks a file whose tree was recovered (row BE-2): it declares
	// nothing and imports nothing.
	Refused bool `json:",omitempty"`
	// Package is the Java package declaration's name (row JV-I1).
	Package string `json:",omitempty"`
	// Types are the importable type paths the file declares below its
	// package: top-level types and their member types ("Order",
	// "Order.Line"), never local or anonymous classes.
	Types []string `json:",omitempty"`
	// Namespaces are the C# namespaces the file declares, block and
	// file-scoped, nested names joined with `.` (the CS-I1 inventory).
	Namespaces []string `json:",omitempty"`
	// GlobalTypes are the names of the C# types the file declares outside
	// every namespace, and "Program" for a file with top-level statements
	// (its implicit class): the repository's types in the global namespace
	// (CS-I1).
	GlobalTypes []string `json:",omitempty"`
	// Imports are the import directives (Java) or using directives (C#) in
	// source order.
	Imports []BestEffortImport `json:",omitempty"`
	// Scoped is set on a refused (BE-2) payload when a lexical scan of the
	// file's text proves where its importable declarations can sit: under
	// one of Scopes (Java packages, C# file-scoped namespaces), or nowhere
	// importable when Scopes is empty (a Java file without a package
	// declaration). Without it a refused file may declare any name (row
	// BE-8), so the zero value is the safe side. A change to what this
	// field means after a release must bump BestEffortVersion, or older
	// cached payloads would be read with the new meaning.
	Scoped bool     `json:",omitempty"`
	Scopes []string `json:",omitempty"`
	// Global is set with Scoped when the refused file may also declare
	// types in the global namespace (a C# file with block namespaces, or
	// without any namespace keyword): Scopes then bound only the
	// namespaces it declares, so a name that may be a type (a C# static or
	// alias using) may be declared there whatever it is (row BE-8). As for
	// Scoped, a change to its meaning after a release must bump
	// BestEffortVersion.
	Global bool `json:",omitempty"`
}

// BestEffortImport is one import or using directive as written.
type BestEffortImport struct {
	// Name is the imported name with whitespace and comments removed and
	// without `static` or a trailing `.*` (Java), or the using target with
	// `global::`, whitespace and comments removed (C#).
	Name   string
	Static bool `json:",omitempty"`
	Star   bool `json:",omitempty"`
	// Alias marks a C# alias using (`using A = B.C;`); Name is its target.
	Alias bool `json:",omitempty"`
	// Scope is the C# namespace enclosing the using directive, "" at
	// compilation-unit level or for a name written with `global::`; a
	// using inside a namespace may name a namespace relative to it.
	Scope string `json:",omitempty"`
	// Prefixes are the proper dotted prefixes of a C# Name, shortest first,
	// from the syntax ("A", "A.B" for "A.B.C<T.U>").
	Prefixes []string `json:",omitempty"`
	// Directive is the Dart directive keyword ("import", "export" or
	// "part"); Name is then one URI as written, without quotes. A
	// conditional directive gives one entry per URI, all with its span.
	Directive string `json:",omitempty"`
	Span      contract.Span
}

// Valid reports whether a cached payload has the current shape.
func (b *BestEffortFacts) Valid() bool { return b != nil && b.Version == BestEffortVersion }
