package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// declarationFile reports whether file is a TypeScript declaration file,
// which holds types and ambient declarations but never values.
func declarationFile(file string) bool {
	for _, suffix := range []string{".d.ts", ".d.mts", ".d.cts"} {
		if strings.HasSuffix(file, suffix) {
			return true
		}
	}
	return false
}

// cacheGlobalNames records the two syntax facts of the clients extractor's
// build-time constant base rule (docs/extract/clients.md): the global
// `declare const NAME: string` declarations of a declaration file
// (RouteFacts.AmbientStrings) and, for every other file, the names it could
// define a value for (RouteFacts.GlobalDefinitions).
func cacheGlobalNames(file string, root *parse.Node, r *facts.RouteFacts) {
	if root == nil {
		return
	}
	names := map[string]bool{}
	if declarationFile(file) {
		module := false
		for _, c := range root.Children {
			module = module || c.Kind == "import_statement" || c.Kind == "export_statement"
		}
		for _, c := range root.Children {
			if c.Kind != "ambient_declaration" {
				continue
			}
			for _, d := range c.Children {
				switch {
				case d.Kind == "lexical_declaration" && !module:
					// A top-level declaration is global only in a script.
					ambientStrings(d, names)
				case d.Kind == "statement_block" && hasToken(c, "global"):
					for _, s := range d.Children {
						if s.Kind == "lexical_declaration" {
							ambientStrings(s, names)
						}
					}
				}
			}
		}
		r.AmbientStrings = sortedNames(names)
		r.AutoImports = autoImports(root)
		return
	}
	for _, c := range root.Children {
		topLevelValues(c, names)
	}
	var assignments func(*parse.Node)
	assignments = func(n *parse.Node) {
		if n.Kind == "assignment_expression" {
			if name := globalProperty(n.Fields["left"]); name != "" {
				names[name] = true
			}
		}
		for _, c := range n.Children {
			assignments(c)
		}
	}
	assignments(root)
	r.GlobalDefinitions = sortedNames(names)
}

// ambientStrings adds the names a `const NAME: string` declaration without
// an initializer declares. Any other type, `let`/`var` or an initializer
// is not the modelled form.
func ambientStrings(d *parse.Node, names map[string]bool) {
	if !hasToken(d, "const") {
		return
	}
	for _, v := range d.Children {
		if v.Kind != "variable_declarator" || v.Fields["value"] != nil {
			continue
		}
		name, annotation := v.Fields["name"], v.Fields["type"]
		if name == nil || name.Kind != "identifier" || annotation == nil {
			continue
		}
		var types []*parse.Node
		for _, t := range annotation.Children {
			if t.Kind != "comment" {
				types = append(types, t)
			}
		}
		if len(types) == 1 && types[0].Kind == "predefined_type" && types[0].Text == "string" {
			names[name.Text] = true
		}
	}
}

// topLevelValues adds the names a top-level statement declares a value for.
func topLevelValues(n *parse.Node, names map[string]bool) {
	switch n.Kind {
	case "export_statement":
		if d := n.Fields["declaration"]; d != nil {
			topLevelValues(d, names)
		}
	case "lexical_declaration", "variable_declaration":
		for _, v := range n.Children {
			if v.Kind == "variable_declarator" {
				patternNames(v.Fields["name"], names)
			}
		}
	case "function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration", "enum_declaration":
		if name := text(n.Fields["name"]); name != "" {
			names[name] = true
		}
	}
}

// patternNames adds every identifier a declarator's name or destructuring
// pattern binds.
func patternNames(n *parse.Node, names map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind == "identifier" || n.Kind == "shorthand_property_identifier_pattern" {
		names[n.Text] = true
		return
	}
	if strings.Contains(n.Kind, "type") || n.Kind == "property_identifier" {
		return
	}
	for _, c := range n.Children {
		patternNames(c, names)
	}
}

// globalProperty returns NAME for an assignment target globalThis.NAME,
// window.NAME, self.NAME or global.NAME (or the same with a string
// subscript), and "" for any other target.
func globalProperty(left *parse.Node) string {
	if left == nil || left.Kind != "member_expression" && left.Kind != "subscript_expression" {
		return ""
	}
	object := left.Fields["object"]
	if object == nil || object.Kind != "identifier" {
		return ""
	}
	switch object.Text {
	case "globalThis", "window", "self", "global":
	default:
		return ""
	}
	if left.Kind == "member_expression" {
		return text(left.Fields["property"])
	}
	index := left.Fields["index"]
	if index == nil || index.Kind != "string" {
		return ""
	}
	return strings.Trim(index.Text, "'\"")
}

func sortedNames(names map[string]bool) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// autoImportHeader is the comment unplugin-auto-import writes at the top of
// the declaration file it generates.
const autoImportHeader = "// Generated by unplugin-auto-import"

// autoImports reads the global bindings of a declaration file generated by
// unplugin-auto-import (RouteFacts.AutoImports): only a file whose leading
// comments carry the plugin's header, only `const` declarations without an
// initializer inside `declare global { }`, and only the form
// `const NAME: typeof import('./relative').NAME` (the export has the
// declared name). Every other declaration records nothing.
func autoImports(root *parse.Node) []facts.AutoImport {
	generated := false
	for _, c := range root.Children {
		if c.Kind != "comment" {
			break
		}
		generated = generated || strings.TrimSpace(c.Text) == autoImportHeader
	}
	if !generated {
		return nil
	}
	var out []facts.AutoImport
	for _, c := range root.Children {
		if c.Kind != "ambient_declaration" || !hasToken(c, "global") {
			continue
		}
		for _, d := range c.Children {
			if d.Kind != "statement_block" {
				continue
			}
			for _, s := range d.Children {
				if s.Kind != "lexical_declaration" || !hasToken(s, "const") {
					continue
				}
				for _, v := range s.Children {
					if v.Kind != "variable_declarator" || v.Fields["value"] != nil {
						continue
					}
					name := v.Fields["name"]
					if name == nil || name.Kind != "identifier" {
						continue
					}
					if module, ok := autoImportModule(v.Fields["type"], name.Text); ok {
						out = append(out, facts.AutoImport{Name: name.Text, Module: module})
					}
				}
			}
		}
	}
	return out
}

// autoImportModule returns the module of a type annotation
// `: typeof import('./module').name`, with a relative module specifier.
func autoImportModule(annotation *parse.Node, name string) (string, bool) {
	if annotation == nil {
		return "", false
	}
	var types []*parse.Node
	for _, t := range annotation.Children {
		if t.Kind != "comment" {
			types = append(types, t)
		}
	}
	if len(types) != 1 || types[0].Kind != "type_query" || len(types[0].Children) != 1 {
		return "", false
	}
	member := types[0].Children[0]
	if member.Kind != "member_expression" || text(member.Fields["property"]) != name {
		return "", false
	}
	call := member.Fields["object"]
	if call == nil || call.Kind != "call_expression" || call.Fields["function"] == nil || call.Fields["function"].Kind != "import" {
		return "", false
	}
	args := call.Fields["arguments"]
	if args == nil || len(args.Children) != 1 || args.Children[0].Kind != "string" {
		return "", false
	}
	module, ok := jsString(args.Children[0].Text)
	if !ok || !strings.HasPrefix(module, "./") && !strings.HasPrefix(module, "../") {
		return "", false
	}
	return module, true
}
