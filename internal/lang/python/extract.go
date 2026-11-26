// Package python extracts Python syntax and conservative module links offline.
package python

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"golang.org/x/text/unicode/norm"
)

// identifier returns a Python identifier as the parser stores it: CPython
// normalises every identifier to NFKC (PEP 3131), so `def \u210c()` defines
// `H`. ASCII names are already normal.
func identifier(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			return norm.NFKC.String(name)
		}
	}
	return name
}

// moduleName returns the module name of a dotted_name or relative_import
// as Python's tokenizer reads it: the NAME tokens joined by ".", each
// NFKC-normalised; white space, comments and line continuations between
// the tokens are not part of the name. A relative import keeps its dots.
func moduleName(n *parse.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "dotted_name":
		var parts []string
		for _, c := range n.Children {
			if c.Kind == "identifier" {
				parts = append(parts, identifier(c.Text))
			}
		}
		return strings.Join(parts, ".")
	case "relative_import":
		dots, rest := "", ""
		for _, c := range n.Children {
			switch c.Kind {
			case "import_prefix":
				dots = strings.Repeat(".", strings.Count(c.Text, "."))
			case "dotted_name":
				rest = moduleName(c)
			}
		}
		return dots + rest
	}
	return identifier(n.Text)
}

func upper(name string) bool {
	letter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			letter = true
			if !unicode.IsUpper(r) {
				return false
			}
		} else if r != '_' && !unicode.IsDigit(r) {
			return false
		}
	}
	return letter
}

func bindingNames(n *parse.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == "identifier" {
		return []string{identifier(n.Text)}
	}
	switch n.Kind {
	case "pattern_list", "tuple_pattern", "list_pattern", "tuple", "list":
		var names []string
		for _, c := range n.Children {
			names = append(names, bindingNames(c)...)
		}
		return names
	}
	return nil
}

func decorator(n *parse.Node) string {
	if n == nil {
		return ""
	}
	for _, c := range n.Children {
		if c.Kind == "call" {
			return facts.Text(c.Fields["function"])
		}
		if c.Kind != "comment" {
			return c.Text
		}
	}
	return ""
}

// Extract records decorators, lexical ownership and positional redefinition names.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	out := facts.Facts{Exports: map[string]string{}}
	counts := map[string]int{}
	shadows := map[string]map[string]bool{}
	classScopes := map[string]bool{}
	unread := map[string]int{}
	selfBindings := map[string]bool{}
	selfAssignments := map[string]bool{}
	var visit func(*parse.Node, string, string, string, string, bool, *parse.Node, []string)
	visit = func(n *parse.Node, scope, ownerID, caller, class string, inClass bool, prefix *parse.Node, decorators []string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "decorated_definition":
			var names []string
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					names = append(names, decorator(c))
				}
			}
			visit(n.Fields["definition"], scope, ownerID, caller, class, inClass, n, names)
			return
		case "function_definition", "class_definition":
			name := identifier(facts.Text(n.Fields["name"]))
			if name == "" {
				return
			}
			kind := "function"
			if n.Kind == "class_definition" {
				kind = "class"
			} else if inClass {
				kind = "method"
			}
			q := facts.Join(scope, name)
			overload := false
			for _, d := range decorators {
				if d == "overload" || d == "typing.overload" {
					overload = true
				}
			}
			switch kind {
			case "function":
				structureTag("PS-D1")
			case "method":
				structureTag("PS-D2")
			default:
				structureTag("PS-D3")
			}
			if overload {
				structureTag("PS-D4")
				q += "@overload" + facts.CompactPython(facts.Text(n.Fields["parameters"]))
			}
			key := q
			counts[key]++
			if counts[key] > 1 {
				structureTag("PS-D5")
				q += "@" + strconv.Itoa(counts[key])
			}
			location := n
			if prefix != nil {
				location = prefix
			}
			attrs := map[string]any{}
			if len(decorators) > 0 {
				structureTag("PS-D6")
				attrs["decorators"] = decorators
			}
			identity := addDecl(&out, file, source, location, n, kind, name, q, ownerID, attrs)
			out.Exports[q] = identity
			shadows[q] = map[string]bool{}
			// Parameters and local assignments shadow module/imported callables.
			var parameters func(*parse.Node)
			parameters = func(c *parse.Node) {
				if c == nil {
					return
				}
				if c.Kind == "identifier" {
					shadows[q][c.Text] = true
					return
				}
				switch c.Kind {
				case "typed_parameter", "typed_default_parameter", "default_parameter":
					name := c.Fields["name"]
					if name == nil {
						for _, v := range c.Children {
							if v.Kind == "identifier" {
								name = v
								break
							}
						}
					}
					parameters(name)
					return
				}
				for _, v := range c.Children {
					parameters(v)
				}
			}
			parameters(n.Fields["parameters"])
			selfBindings[q] = kind == "method" && shadows[q]["self"]
			if kind == "class" {
				classScopes[q] = true
				class = q
				inClass = true
				caller = identity
			} else {
				inClass = false
				caller = identity
			}
			visit(n.Fields["body"], q, identity, caller, class, inClass, nil, nil)
			return
		case "import_statement", "import_from_statement":
			module := moduleName(n.Fields["module_name"])
			from := n.Kind == "import_from_statement"
			for _, c := range n.Children {
				if c == n.Fields["module_name"] {
					continue
				}
				if c.Kind != "dotted_name" && c.Kind != "aliased_import" && c.Kind != "wildcard_import" {
					continue
				}
				name := c.Text
				if c.Kind == "dotted_name" {
					name = moduleName(c)
				}
				alias := ""
				if c.Kind == "aliased_import" {
					name = moduleName(c.Fields["name"])
					alias = identifier(facts.Text(c.Fields["alias"]))
				}
				if alias == "" {
					alias = name
					if !from {
						alias = strings.Split(name, ".")[0]
					}
				}
				impModule := module
				if !from {
					impModule = name
				}
				out.Imports = append(out.Imports, facts.Import{Module: impModule, Bindings: map[string]string{alias: name}, Span: facts.Span(n), From: from, Scope: scope, ExplicitAlias: c.Kind == "aliased_import"})
			}
			return
		case "future_import_statement":
			// Row D: __future__ imports claim no edge (open point O-S2-8).
			for _, c := range n.Children {
				if c.Kind == "dotted_name" || c.Kind == "aliased_import" {
					count(unread, unreadFutureImport, 1)
				}
			}
			return
		case "return_statement":
			if ownerID == "" {
				// Row PS-F3: a file only compile() rejects is claimed as written.
				structureTag("PS-F3")
			}
		case "assignment":
			left := n.Fields["left"]
			annotationOnly := n.Fields["right"] == nil
			if scope == "" {
				for _, name := range starredNames(left) {
					if upper(name) {
						count(unread, unreadStarredTarget, 1)
					}
				}
			}
			for _, name := range bindingNames(left) {
				if name == "self" {
					selfAssignments[scope] = true
				}
				if scope == "" && upper(name) && annotationOnly {
					count(unread, unreadAnnotationOnly, 1)
					continue
				}
				if scope == "" && upper(name) {
					structureTag("PS-D7")
					q := name
					counts[q]++
					if counts[q] > 1 {
						q += "@" + strconv.Itoa(counts[q])
					}
					out.Exports[q] = facts.Add(&out, file, source, n, "constant", name, q, "", map[string]any{})
				} else {
					if shadows[scope] == nil {
						shadows[scope] = map[string]bool{}
					}
					shadows[scope][name] = true
				}
			}
		case "for_statement", "for_in_clause", "augmented_assignment", "named_expression":
			left := n.Fields["left"]
			if left == nil {
				left = n.Fields["name"]
			}
			for _, name := range bindingNames(left) {
				if shadows[scope] == nil {
					shadows[scope] = map[string]bool{}
				}
				shadows[scope][name] = true
				if name == "self" {
					selfAssignments[scope] = true
				}
			}
		case "call":
			callee := n.Fields["function"]
			name := ""
			staticName := ""
			if callee != nil && (callee.Kind == "identifier" || callee.Kind == "attribute") {
				name = callee.Text
				if callee.Kind == "attribute" {
					staticName = facts.Text(callee.Fields["attribute"])
				}
			}
			if class != "" && strings.HasPrefix(name, "self.") {
				name = "@self:" + class + strings.TrimPrefix(name, "self")
			}
			out.Calls = append(out.Calls, facts.Call{StaticName: staticName, Source: caller, Scope: scope, Name: name, Span: facts.Span(n)})
		}
		for _, c := range n.Children {
			visit(c, scope, ownerID, caller, class, inClass, nil, nil)
		}
	}
	visit(root, "", "", facts.ID("file", file, file), "", false, nil, nil)
	for i := range out.Calls {
		call := &out.Calls[i]
		base := strings.Split(call.Name, ".")[0]
		scope := call.Scope
		if strings.HasPrefix(call.Name, "@self:") {
			bound := false
			for s := scope; ; {
				if selfAssignments[s] {
					break
				}
				if shadows[s]["self"] {
					bound = selfBindings[s]
					break
				}
				if s == "" {
					break
				}
				if j := strings.LastIndex(s, "."); j >= 0 {
					s = s[:j]
				} else {
					s = ""
				}
			}
			call.Shadowed = !bound
		}
		for {
			if !classScopes[scope] {
				call.Shadowed = call.Shadowed || shadows[scope][base]
			}
			if scope == "" {
				break
			}
			if j := strings.LastIndex(scope, "."); j >= 0 {
				scope = scope[:j]
			} else {
				scope = ""
			}
		}
	}
	extractSyntax(file, source, root, &out)
	if len(unread) > 0 {
		out.Python.Unread = unread
	}
	if form := rejection(root, source); form != "" {
		return refuse(out, file, form)
	}
	return out
}

// starredNames returns the names of starred targets (`*E`) in an
// assignment's left side; row PS-D7 claims none of them.
func starredNames(n *parse.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "list_splat_pattern":
		return bindingNames(firstNamed(n))
	case "pattern_list", "tuple_pattern", "list_pattern", "tuple", "list":
		var names []string
		for _, c := range n.Children {
			names = append(names, starredNames(c)...)
		}
		return names
	}
	return nil
}

func firstNamed(n *parse.Node) *parse.Node {
	for _, c := range n.Children {
		if c.Kind != "comment" {
			return c
		}
	}
	return nil
}

func callable(file, name string, files map[string]facts.Facts) string {
	identity := files[file].Exports[name]
	kind, _, _, _ := contract.ParseIdentity(identity)
	if kind == "function" || kind == "method" || kind == "class" {
		return identity
	}
	return ""
}

// Link resolves Python links with repository and implicit roots without metadata.
func Link(files map[string]facts.Facts) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkWithResolver(files, NewModuleResolver(files, nil))
}

// LinkWithResolver shares Python import bindings with calls and route composition.
func LinkWithResolver(files map[string]facts.Facts, resolver *ModuleResolver) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	for _, file := range facts.Paths(files) {
		f := files[file]
		f.UnknownSites = nil
		edges = append(edges, f.Edges...)
		structureTag("PS-F1")
		structureTag("PS-M1")
		unread := map[string]int{}
		if f.Python != nil {
			for form, n := range f.Python.Unread {
				unread[form] += n
			}
		}
		if f.Python == nil || !f.Python.Rejected {
			importEdges, importDiagnostics := linkImports(file, f, resolver, nodes, unread)
			edges = append(edges, importEdges...)
			diagnostics = append(diagnostics, importDiagnostics...)
		}
		diagnostics = append(diagnostics, unreadDiagnostic(file, unread)...)
		external, unknown := 0, 0
		for _, call := range f.Calls {
			target := ""
			isExternal := false
			if !call.Shadowed && call.Name != "" {
				if strings.HasPrefix(call.Name, "@self:") {
					target = callable(file, strings.TrimPrefix(call.Name, "@self:"), files)
				} else {
					b := resolver.Callable(file, resolver.Scope(file, call.Scope), call.Name)
					kind, _, _, _ := contract.ParseIdentity(b.Identity)
					if kind == "function" || kind == "method" || kind == "class" {
						target = b.Identity
					}
					isExternal = b.External
				}
			}
			if target != "" {
				edges = append(edges, facts.Edge("calls", call.Source, target, file, call.Span))
			} else if !call.Shadowed && (isExternal || facts.Builtin(call.Name, pythonBuiltins)) {
				external++
			} else {
				unknown++
				f.UnknownSites = append(f.UnknownSites, facts.Unknown(call))
			}
		}
		files[file] = f
		diagnostics = append(diagnostics, facts.Unresolved(file, external, unknown)...)
	}
	// Row PS-M2's refusals: workspace members and declarations the
	// resolution could not read exactly.
	diagnostics = append(diagnostics, resolver.WorkspaceDiagnostics()...)
	result, edges := facts.Finish(nodes, edges)
	return result, edges, diagnostics
}

const pythonBuiltins = "abs aiter all anext any ascii bin bool breakpoint bytearray bytes callable chr classmethod compile complex delattr dict dir divmod enumerate eval exec filter float format frozenset getattr globals hasattr hash help hex id input int isinstance issubclass iter len list locals map max memoryview min next object oct open ord pow print property range repr reversed round set setattr slice sorted staticmethod str sum super tuple type vars zip __import__ BaseException Exception ArithmeticError AssertionError AttributeError EOFError ImportError ModuleNotFoundError LookupError IndexError KeyError NameError UnboundLocalError OSError IOError FileNotFoundError PermissionError NotImplementedError RuntimeError RecursionError StopIteration StopAsyncIteration SyntaxError IndentationError TabError TypeError ValueError UnicodeError UnicodeDecodeError UnicodeEncodeError UnicodeTranslateError ZeroDivisionError OverflowError FloatingPointError MemoryError SystemError SystemExit KeyboardInterrupt GeneratorExit Warning UserWarning DeprecationWarning ResourceWarning RuntimeWarning FutureWarning"
