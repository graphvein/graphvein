// Package golang extracts Go declarations and conservative calls without type checking.
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func exported(name string) bool { r, _ := utf8.DecodeRuneInString(name); return unicode.IsUpper(r) }

func namedType(n *parse.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "type_identifier":
		return n.Text
	case "pointer_type":
		if len(n.Children) == 1 {
			return namedType(n.Children[0])
		}
	case "generic_type":
		return namedType(n.Fields["type"])
	}
	return ""
}

func identifiers(n *parse.Node) []string {
	var names []string
	if n == nil {
		return names
	}
	for _, c := range n.Children {
		if c.Kind == "identifier" {
			names = append(names, c.Text)
		}
	}
	return names
}

// receiverName reads the receiver type forms of contract row GS-D4: T, *T,
// T[...] and *T[...] with T an identifier. Every other form (parenthesised,
// qualified, double pointer, variadic) returns "" and goes to the default
// branch, counted, instead of guessing a receiver name.
func receiverName(n *parse.Node) string {
	if n != nil && n.Kind == "pointer_type" && len(n.Children) == 1 {
		n = n.Children[0]
	}
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "type_identifier":
		return n.Text
	case "generic_type":
		if t := n.Fields["type"]; t != nil && t.Kind == "type_identifier" {
			return t.Text
		}
	}
	return ""
}

// methodReceiver returns the receiver base type name of a method whose
// receiver list holds exactly one parameter declaration of a GS-D4 form.
func methodReceiver(n *parse.Node) string {
	r := n.Fields["receiver"]
	if r == nil || len(r.Children) != 1 || r.Children[0].Kind != "parameter_declaration" {
		return ""
	}
	if len(identifiers(r.Children[0])) > 1 {
		// func (a, b T) M(): go/parser accepts it, the type checker reports
		// "method has multiple receivers".
		return ""
	}
	return receiverName(r.Children[0].Fields["type"])
}

// goParserRejects reports whether go/parser rejects the file. Contract row
// GS-F2 keys on it: tree-sitter's error recovery can give a declaration a
// span that swallows the declarations after it, so a file the Go parser
// rejects claims no declaration and no import edge.
func goParserRejects(file string, source []byte) bool {
	_, _, err := goParse(file, source)
	return err != nil
}

// goParse parses a file with go/parser, as row GS-F2 and the shape payload
// (shape.go) read it.
func goParse(file string, source []byte) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, source, parser.SkipObjectResolution)
	return f, fset, err
}

// Extract preserves source order for repeated init and blank functions and
// lexical spans. The structural claims follow docs/lang/go-structure.md;
// forms no row reads are counted in the payload's Unread map.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	out := facts.Facts{Exports: map[string]string{}}
	initCount, blankCount := 0, 0
	unread := map[string]int{}
	var declarations func(*parse.Node)
	declarations = func(n *parse.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "package_clause":
			for _, c := range n.Children {
				if c.Kind == "package_identifier" {
					out.Package = c.Text
				}
			}
			return
		case "import_spec":
			module, _ := strconv.Unquote(facts.Text(n.Fields["path"]))
			alias := facts.Text(n.Fields["name"])
			if alias == "" {
				alias = path.Base(module)
			}
			out.Imports = append(out.Imports, facts.Import{Module: module, Bindings: map[string]string{alias: "*"}, Span: facts.Span(n), ExplicitAlias: n.Fields["name"] != nil})
			return
		case "type_spec", "type_alias":
			name := facts.Text(n.Fields["name"])
			if name == "" {
				return
			}
			kind := "type"
			if n.Kind == "type_alias" {
				kind = "type_alias"
			} else if typ := n.Fields["type"]; typ != nil {
				if typ.Kind == "struct_type" {
					kind = "struct"
				}
				if typ.Kind == "interface_type" {
					kind = "interface"
				}
			}
			structureTag("GS-D5")
			identity := facts.Add(&out, file, source, n, kind, name, name, "", map[string]any{})
			out.Exports[name] = identity
			return
		case "var_spec", "const_spec":
			for _, name := range identifiers(n) {
				if exported(name) {
					kind, row := "variable", "GS-D7"
					if n.Kind == "const_spec" {
						kind, row = "constant", "GS-D8"
					}
					structureTag(row)
					out.Exports[name] = facts.Add(&out, file, source, n, kind, name, name, "", map[string]any{"exported": true})
				}
			}
			return
		case "function_declaration", "method_declaration":
			name := facts.Text(n.Fields["name"])
			if name == "" {
				return
			}
			q := name
			kind := "function"
			locals := map[string]string{}
			shadow := map[string]bool{}
			params := func(p *parse.Node) {
				if p == nil {
					return
				}
				for _, c := range p.Children {
					for _, v := range identifiers(c) {
						locals[v] = namedType(c.Fields["type"])
						shadow[v] = true
					}
				}
			}
			params(n.Fields["parameters"])
			params(n.Fields["receiver"])
			if n.Kind == "method_declaration" {
				kind = "method"
				receiver := methodReceiver(n)
				if receiver == "" {
					structureTag("D")
					unread[unreadReceiver]++
					return
				}
				structureTag("GS-D4")
				q = receiver + "." + name
			} else if name == "init" {
				structureTag("GS-D2")
				initCount++
				if initCount > 1 {
					q += "@" + strconv.Itoa(initCount)
				}
			} else if name == "_" {
				structureTag("GS-D3")
				blankCount++
				if blankCount > 1 {
					q += "@" + strconv.Itoa(blankCount)
				}
			} else {
				structureTag("GS-D1")
			}
			identity := facts.Add(&out, file, source, n, kind, name, q, "", map[string]any{})
			if name != "init" && name != "_" {
				out.Exports[q] = identity
			}
			// Bound local names are collected before resolving calls. Assignment
			// inference is deliberately absent; only explicit named types qualify.
			var collect func(*parse.Node)
			collect = func(c *parse.Node) {
				if c == nil || c.Kind == "func_literal" {
					return
				}
				switch c.Kind {
				case "var_spec":
					for _, v := range identifiers(c) {
						if shadow[v] {
							locals[v] = ""
						} else {
							locals[v] = namedType(c.Fields["type"])
						}
						shadow[v] = true
					}
				case "short_var_declaration", "range_clause":
					if left := c.Fields["left"]; left != nil {
						for _, v := range identifiers(left) {
							shadow[v] = true
						}
					}
				case "const_spec":
					for _, v := range identifiers(c) {
						shadow[v] = true
						locals[v] = ""
					}
				}
				for _, v := range c.Children {
					collect(v)
				}
			}
			collect(n.Fields["body"])
			var calls func(*parse.Node, bool)
			calls = func(c *parse.Node, inLiteral bool) {
				if c == nil {
					return
				}
				inLiteral = inLiteral || c.Kind == "func_literal"
				if c.Kind == "call_expression" {
					callee := c.Fields["function"]
					callName := ""
					staticName := ""
					blocked := false
					if callee != nil {
						if callee.Kind == "identifier" {
							callName = callee.Text
							blocked = shadow[callName]
						}
						if callee.Kind == "selector_expression" {
							operand := callee.Fields["operand"]
							member := facts.Text(callee.Fields["field"])
							staticName = member
							if operand != nil && operand.Kind == "identifier" {
								callName = operand.Text + "." + member
								if typ := locals[operand.Text]; typ != "" {
									callName = "@receiver:" + typ + "." + member
								} else {
									blocked = shadow[operand.Text]
								}
							}
						}
					}
					out.Calls = append(out.Calls, facts.Call{StaticName: staticName, Source: identity, Scope: q, Name: callName, Span: facts.Span(c), Shadowed: blocked || inLiteral})
				}
				for _, v := range c.Children {
					calls(v, inLiteral)
				}
			}
			calls(n.Fields["body"], false)
			return
		}
		for _, c := range n.Children {
			declarations(c)
		}
	}
	declarations(root)
	refuseRedeclarations(&out, unread)
	out.Go, out.Diagnostics = safeCaptureGo(file, source, root, out.Imports, out.Diagnostics)
	parsed, fset, perr := goParse(file, source)
	rejected := perr != nil
	if out.Go != nil && !rejected && !shapeTestFile(file) {
		out.Go.Shapes, out.Go.Marshalers, out.Diagnostics = safeCaptureShapes(file, parsed, fset, out.Diagnostics)
	}
	if rejected {
		// Row GS-F2: nothing structural is claimed. The imports stay in the
		// facts for GORM (row PL-F7's scope); Link emits no edge for them.
		structureTag("GS-F2")
		n := len(out.Nodes) + len(out.Imports)
		for _, c := range unread {
			n += c
		}
		unread = map[string]int{}
		if n > 0 {
			structureTag("D")
			unread[unreadSyntaxError] = n
		}
		out.Nodes, out.Edges, out.Calls = nil, nil, nil
		out.Exports = map[string]string{}
	}
	if out.Go != nil {
		out.Go.Build = buildLines(source)
		out.Go.Rejected = rejected
		if len(unread) > 0 {
			out.Go.Unread = unread
		}
	}
	return out
}

// refuseRedeclarations sends every declaration whose identity occurs more
// than once in the file to the default branch (row GS-D6): invalid Go that
// go/parser accepts, where keeping the last record would give one identity
// a span the other declaration owns. Their contains edges, exports and the
// calls they own go with them.
func refuseRedeclarations(out *facts.Facts, unread map[string]int) {
	count := map[string]int{}
	for _, n := range out.Nodes {
		count[n.Identity]++
	}
	dropped := map[string]bool{}
	for identity, n := range count {
		if n > 1 {
			dropped[identity] = true
			unread[unreadRedeclaration] += n
			structureTag("GS-D6")
			structureTag("D")
		}
	}
	if len(dropped) == 0 {
		return
	}
	nodes := out.Nodes[:0]
	for _, n := range out.Nodes {
		if !dropped[n.Identity] {
			nodes = append(nodes, n)
		}
	}
	out.Nodes = nodes
	edges := out.Edges[:0]
	for _, e := range out.Edges {
		if !dropped[e.Target] {
			edges = append(edges, e)
		}
	}
	out.Edges = edges
	calls := out.Calls[:0]
	for _, c := range out.Calls {
		if !dropped[c.Source] {
			calls = append(calls, c)
		}
	}
	out.Calls = calls
	for name, identity := range out.Exports {
		if dropped[identity] {
			delete(out.Exports, name)
		}
	}
}

// RefuseLoneCR applies row GS-F3 to a file whose raw bytes hold a CR not
// followed by LF (scan.File.LoneCR): in Go such a CR is white space, not a
// line end, so a // comment runs on past it and a build constraint after
// it is no constraint, while Graphvein's extractors read the scanner's
// text, where it is a line end. Rather than model Go's reading of those
// bytes, the file claims nothing beyond its file node: every declaration
// and import goes to the default branch, counted as lone_cr (a GS-F2 count
// included), its calls are dropped, and the payload is marked so that the
// linker emits no import edge for it and refuses its directory as a
// package. The imports and the model payload stay for GORM, as for GS-F2.
func RefuseLoneCR(f facts.Facts) facts.Facts {
	structureTag("GS-F3")
	n := len(f.Nodes) + len(f.Imports)
	if f.Go != nil {
		if f.Go.Rejected {
			n = 0
		}
		for _, c := range f.Go.Unread {
			n += c
		}
	}
	f.Nodes, f.Edges, f.Calls, f.UnknownSites = nil, nil, nil, nil
	f.Exports = map[string]string{}
	if f.Go != nil {
		g := *f.Go
		g.LoneCR = true
		g.Unread = nil
		if n > 0 {
			structureTag("D")
			g.Unread = map[string]int{unreadLoneCR: n}
		}
		f.Go = &g
	}
	return f
}

// safeCaptureGo captures the Go model payload (ADR 0063) and the router
// syntax (ADR 0076, routes.go). A capture failure keeps the file's other
// facts and every other file: the payload is absent, a warning names the
// file, and the indexer's cache check misses on a Go file without a
// payload, so the next run tries again.
func safeCaptureGo(file string, source []byte, root *parse.Node, imports []facts.Import, diagnostics []contract.Diagnostic) (payload *facts.GoFacts, out []contract.Diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			payload = nil
			out = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "model_syntax_capture_failed", Message: fmt.Sprintf("%s model syntax capture: %v", file, v)})
		}
	}()
	payload = goCaptureFunc(file, root, imports)
	if payload != nil {
		captureRouteSyntax(payload, file, source, root, imports)
	}
	return payload, diagnostics
}

// safeCaptureShapes captures the shape payload (shape.go); a failure keeps
// the file's other facts and records a warning, so the routes of that file
// get no shape and every other file is read.
func safeCaptureShapes(file string, parsed *ast.File, fset *token.FileSet, diagnostics []contract.Diagnostic) (shapes []facts.GoShapeFunc, marshalers []facts.GoMarshaler, out []contract.Diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			shapes, marshalers = nil, nil
			out = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "model_syntax_capture_failed", Message: fmt.Sprintf("%s shape syntax capture: %v", file, v)})
		}
	}()
	shapes, marshalers = captureShapes(parsed, fset)
	return shapes, marshalers, diagnostics
}

// goCaptureFunc is captureGo; tests replace it to check the recover guard.
var goCaptureFunc = captureGo

// ModulePaths reads module declarations from the scanned inventory, never outside it.
// Relinking reads it anew so editing go.mod does not require reparsing importers.
func ModulePaths(sources map[string][]byte) map[string]string {
	out := map[string]string{}
	for p, source := range sources {
		if path.Base(p) != "go.mod" {
			continue
		}
		for _, line := range strings.Split(string(source), "\n") {
			words := strings.Fields(line)
			if len(words) >= 2 && words[0] == "module" {
				out[path.Dir(p)] = strings.Trim(words[1], "\"")
				break
			}
		}
	}
	return out
}

func callable(paths []string, name string, files map[string]facts.Facts) string {
	identity := ""
	for _, p := range paths {
		for _, n := range files[p].Nodes {
			if (n.Kind == "function" || n.Kind == "method") && n.QualifiedName == name {
				if identity != "" {
					return ""
				}
				identity = n.Identity
			}
		}
	}
	return identity
}

// Link resolves only package functions and explicitly typed same-package
// methods. Every "package in this directory" it names is Package's: import
// edges, the files and name an import binds, and the files a call without
// a qualifier sees.
func Link(files map[string]facts.Facts, modules map[string]string) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	packages := map[string]GoPackage{}
	packageOf := func(dir string) GoPackage {
		pkg, ok := packages[dir]
		if !ok {
			pkg = Package(dir, files)
			packages[dir] = pkg
		}
		return pkg
	}
	for _, file := range facts.Paths(files) {
		f := files[file]
		f.UnknownSites = nil
		structureTag("GS-F1")
		edges = append(edges, f.Edges...)
		bindings := map[string][]string{}
		externalBindings := map[string]bool{}
		unread := map[string]int{}
		if f.Go != nil {
			for form, n := range f.Go.Unread {
				unread[form] += n
			}
		}
		// A file go/parser rejects (GS-F2), a lone-CR file (GS-F3) or one
		// whose payload capture failed emits no import edge; the extractor
		// already counted the first two.
		linkImports := f.Go != nil && !f.Go.Rejected && !f.Go.LoneCR
		if f.Go == nil && len(f.Imports) > 0 {
			structureTag("D")
			unread[unreadCaptureMissing] += len(f.Imports)
		}
		base, module := ModuleAt(file, modules)
		if module != "" && CheckImportPath(module) == nil {
			structureTag("GS-M1")
		}
		for _, imp := range f.Imports {
			if CheckImportPath(imp.Module) != nil {
				// The go command rejects the path before resolving it.
				if linkImports {
					structureTag("D")
					unread[unreadMalformedPath]++
				}
				continue
			}
			dir, inModule := ModuleImportDir(base, module, imp.Module)
			nested := inModule && nestedModule(base, dir, modules)
			if !inModule || nested {
				if linkImports {
					if nested {
						structureTag("GS-I3")
						structureTag("D")
						unread[unreadNestedModule]++
					} else {
						structureTag("GS-I4")
					}
					edges = append(edges, facts.Edge("imports", facts.ID("file", file, file), facts.External(nodes, "go", imp.Module), file, imp.Span))
				}
				for alias := range imp.Bindings {
					if alias != "_" && alias != "." {
						externalBindings[alias] = true
					}
				}
				continue
			}
			var packageName string
			var targets []string
			refusal := ""
			if pkg := packageOf(dir); pkg.Refusal != "" {
				refusal = pkg.Refusal
			} else {
				packageName, targets, refusal = ImportPackage(file, dir, files)
			}
			if linkImports {
				switch {
				case refusal != "":
					structureTag("GS-I5")
					structureTag("D")
					unread[refusal]++
				case len(targets) == 0:
					structureTag("GS-I2")
					diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "in-module import " + imp.Module + " is unavailable"})
				default:
					structureTag("GS-I1")
				}
				for _, target := range targets {
					edges = append(edges, facts.Edge("imports", facts.ID("file", file, file), facts.ID("file", target, target), file, imp.Span))
				}
			}
			if packageName == "" {
				// A refused or unresolved package binds nothing.
				continue
			}
			for alias := range imp.Bindings {
				// The default binding is the package clause, which need not match
				// the last import path segment (notably semantic version paths).
				if !imp.ExplicitAlias {
					alias = packageName
				}
				if alias != "_" && alias != "." {
					bindings[alias] = targets
				}
			}
		}
		own := OwnPackageFiles(file, packageOf(path.Dir(file)), files)
		external, unknown := 0, 0
		for _, call := range f.Calls {
			target := ""
			if !call.Shadowed && call.Name != "" {
				if !strings.Contains(call.Name, ".") || strings.HasPrefix(call.Name, "@receiver:") {
					target = callable(own, strings.TrimPrefix(call.Name, "@receiver:"), files)
				}
				if target == "" {
					parts := strings.Split(call.Name, ".")
					if len(parts) == 2 && exported(parts[1]) {
						if paths, ok := bindings[parts[0]]; ok {
							target = callable(paths, parts[1], files)
						}
					}
				}
			}
			if target == "" {
				base := strings.Split(call.Name, ".")[0]
				if !call.Shadowed && (externalBindings[base] || facts.Builtin(call.Name, "append cap clear close complex copy delete imag len make max min new panic print println real recover")) {
					external++
				} else {
					unknown++
					f.UnknownSites = append(f.UnknownSites, facts.Unknown(call))
				}
			} else {
				edges = append(edges, facts.Edge("calls", call.Source, target, file, call.Span))
			}
		}
		files[file] = f
		diagnostics = append(diagnostics, facts.Unresolved(file, external, unknown)...)
		diagnostics = append(diagnostics, unreadDiagnostic(file, unread)...)
	}
	result, edges := facts.Finish(nodes, edges)
	return result, edges, diagnostics
}
