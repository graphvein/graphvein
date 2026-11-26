package sequelize

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file resolves names across modules from the cached model payload:
// ESM imports through the shared TypeScript resolver, and the narrow
// CommonJS reach of ADR 0061 for gated files: a module-level
// `require("<relative>")` initializer (whole, member or destructured) and
// the `module.exports = e`, `module.exports.x = e` and `exports.x = e`
// statements of the required module. Package specifiers are never
// followed; they resolve to an external value that names the package, so
// the discovery can recognise `sequelize`. It is not a general CommonJS
// resolver (ADR 0014's deviation stays for every other consumer).

// maxDepth bounds every chain of aliases, re-exports and requires.
const maxDepth = 12

// value is what a name or expression resolves to. Exactly one of the
// shapes is set: an external package value (External, Name: the exported
// name, "" for a CommonJS whole module, "*" for a namespace), a
// module-level declaration (File, Decl, Expr: its initializer), a class
// declaration (File, Class), or an expression of File (Expr). The zero
// value is unknown; Why may say why.
type value struct {
	External string
	Name     string
	File     string
	Decl     string
	Class    *typescript.ModelStatement
	Expr     *typescript.ModelExpr
	Why      string
}

func (v value) known() bool { return v.External != "" || v.Class != nil || v.Expr != nil }

// isSequelizeClass reports a value that is Sequelize's constructor: the
// default or `Sequelize` export of the `sequelize` package, or its
// CommonJS module object (module.exports is the class).
func (v value) isSequelizeClass() bool {
	return v.External == pkgSequelize && (v.Name == "" || v.Name == "default" || v.Name == "Sequelize")
}

// isSequelizeModule reports the `sequelize` package object itself (a
// namespace import or a CommonJS require), whose `Sequelize`, `DataTypes`
// and `Model` members are the package's exports.
func (v value) isSequelizeModule() bool {
	return v.External == pkgSequelize && (v.Name == "" || v.Name == "*")
}

// requireSpec returns the specifier of `require("<spec>")`, or "".
func requireSpec(e *typescript.ModelExpr) string {
	if e == nil || e.Kind != typescript.ModelCall || e.Callee == nil || e.Callee.Kind != typescript.ModelIdentifier || e.Callee.Name != "require" {
		return ""
	}
	if len(e.Args) != 1 {
		return ""
	}
	arg := &e.Args[0]
	switch {
	case arg.Kind == typescript.ModelString:
		return arg.Value
	case arg.Kind == typescript.ModelBinary && arg.Value == "+" && arg.Left.Kind == typescript.ModelIdentifier && arg.Left.Name == "__dirname" &&
		arg.Right.Kind == typescript.ModelString && strings.HasPrefix(arg.Right.Value, "/"):
		// `require(__dirname + "/../config/config.json")`, the
		// sequelize-cli template's config require: the
		// module's own directory, so a relative specifier.
		return "." + arg.Right.Value
	}
	return ""
}

// resolveRequire places a relative require specifier as Node's CommonJS
// loader does (the file as written, then .js, .json and .node, then the
// directory's index.js and index.json). A directory with its own
// package.json (a "main" field) and a match only through a TypeScript
// extension (a loader such as ts-node or babel-node) are not followed.
func (d *discovery) resolveRequire(from, spec string) (string, string) {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && spec != "." && spec != ".." {
		return "", "not a relative require"
	}
	base := path.Join(path.Dir(from), spec)
	if base == ".." || strings.HasPrefix(base, "../") {
		return "", "leaves the repository"
	}
	for _, c := range []string{base, base + ".js", base + ".json", base + ".node"} {
		if _, ok := d.in.Sources[c]; ok {
			return c, ""
		}
	}
	if _, ok := d.in.Sources[path.Join(base, "package.json")]; ok {
		return "", "a directory with its own package.json (its main field is not read)"
	}
	for _, c := range []string{path.Join(base, "index.js"), path.Join(base, "index.json")} {
		if _, ok := d.in.Sources[c]; ok {
			return c, ""
		}
	}
	for _, ext := range []string{".ts", ".cjs", ".mjs", ".cts", ".mts", ".tsx", ".jsx"} {
		if _, ok := d.in.Sources[base+ext]; ok {
			return "", "only " + base + ext + " exists, which Node's require does not load without a loader"
		}
	}
	return "", "no file of the repository"
}

// declaration returns the single module-level declaration of name in
// file; several declarations of one name are ambiguous (nil).
func declaration(m *typescript.ModelSyntax, name string) *typescript.ModelDeclaration {
	if m == nil {
		return nil
	}
	var found *typescript.ModelDeclaration
	for i := range m.Declarations {
		if m.Declarations[i].Name == name {
			if found != nil {
				return nil
			}
			found = &m.Declarations[i]
		}
	}
	return found
}

// classDecl returns the module-level class declaration named name.
func classDecl(m *typescript.ModelSyntax, name string) *typescript.ModelStatement {
	if m == nil {
		return nil
	}
	for i := range m.Statements {
		if s := &m.Statements[i]; s.Kind == typescript.StatementClass && s.Name == name {
			return s
		}
	}
	return nil
}

// functionDecl returns the single module-level function declaration named
// name (payload row S6); several declarations of one name are not read.
func functionDecl(m *typescript.ModelSyntax, name string) *typescript.ModelStatement {
	if m == nil {
		return nil
	}
	var found *typescript.ModelStatement
	for i := range m.Statements {
		if s := &m.Statements[i]; s.Kind == typescript.StatementFunction && s.Name == name {
			if found != nil {
				return nil
			}
			found = s
		}
	}
	return found
}

// local resolves a module-level name of file: a declaration (a require
// initializer is followed), a class, a destructured require, or an ESM
// import binding.
func (d *discovery) local(file, name string, depth int) value {
	if depth > maxDepth {
		return value{Why: "alias chain too long"}
	}
	m := d.in.Models[file]
	if decl := declaration(m, name); decl != nil {
		e := &decl.Value
		if spec := requireSpec(e); spec != "" {
			return d.required(file, spec, "", depth+1)
		}
		if e.Kind == typescript.ModelMember && e.Object != nil {
			if spec := requireSpec(e.Object); spec != "" {
				return d.required(file, spec, e.Name, depth+1)
			}
		}
		if e.Kind == typescript.ModelIdentifier && decl.Keyword == "const" {
			return d.local(file, e.Name, depth+1)
		}
		return value{File: file, Decl: name, Expr: e}
	}
	if c := classDecl(m, name); c != nil {
		return value{File: file, Class: c}
	}
	if fn := functionDecl(m, name); fn != nil {
		return value{File: file, Decl: name, Expr: fn.Value}
	}
	if m != nil {
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Kind != typescript.StatementDestructure {
				continue
			}
			for _, b := range s.Bindings {
				if b.Name != name || b.Key == "" {
					continue
				}
				if spec := requireSpec(s.Value); spec != "" {
					return d.required(file, spec, b.Key, depth+1)
				}
				return value{Why: "destructured from a value that is not a require"}
			}
		}
	}
	for _, imp := range d.in.TypeScript[file].Imports {
		exported, ok := imp.Bindings[name]
		if !ok {
			continue
		}
		res := d.in.Resolver.Resolve(file, imp.Module)
		switch {
		case res.Package() || res.File != "" && typeDeclaration(res.File) && !strings.HasPrefix(imp.Module, "."):
			// A bare specifier that tsconfig paths map to a type declaration
			// (opencollective-api maps `config` to node-config's typings) is
			// the package at run time. A refused bare import is not: it may
			// be a repository file (row TS-I5), unless it is refused only
			// because the options are unread and the package is declared
			// (ModuleResolution.Package).
			return value{External: imp.Module, Name: exported}
		case res.File == "" && res.Refusal != "":
			return value{Why: "import of " + imp.Module + " is not proven (" + res.Refusal + ")"}
		case res.File == "":
			return value{Why: "import of " + imp.Module + " resolves to no file of the repository"}
		}
		return d.exported(res.File, exported, depth+1)
	}
	return value{Why: name + " is not a module-level binding of " + file}
}

// required resolves `require(spec)` from file, then its member key ("" for
// the whole module.exports).
func (d *discovery) required(file, spec, key string, depth int) value {
	if !strings.HasPrefix(spec, ".") {
		// The package only when the resolver states one (row TS-I5, or a
		// declared package under unread options): a package.json of the
		// repository with this name, a workspace link or an undeclared
		// name may make it a repository file.
		if res := d.in.Resolver.Resolve(file, spec); !res.Package() {
			return value{Why: "require(" + quote(spec) + ") from " + file + " is not proven to be the package"}
		}
		if key == "" {
			return value{External: spec}
		}
		return value{External: spec, Name: key}
	}
	target, why := d.resolveRequire(file, spec)
	if target == "" {
		return value{Why: "require(" + quote(spec) + ") from " + file + ": " + why}
	}
	d.requires[file] = append(d.requires[file], target)
	return d.cjsExport(target, key, depth+1)
}

// moduleExports returns the module.exports value of a CommonJS module:
// the expression of its single module-level `module.exports = e`
// statement. Several such statements are not read.
func moduleExports(m *typescript.ModelSyntax) *typescript.ModelExpr {
	if m == nil {
		return nil
	}
	var found *typescript.ModelExpr
	for i := range m.Statements {
		s := &m.Statements[i]
		if s.Kind != typescript.StatementExpression || s.Value.Kind != typescript.ModelAssign {
			continue
		}
		if memberPath(s.Value.Left) == "module.exports" {
			if found != nil {
				return nil
			}
			found = s.Value.Right
		}
	}
	return found
}

// cjsExport resolves a CommonJS module's exports value (key "") or one of
// its members: `module.exports = {key: e}`, `module.exports.key = e` or
// `exports.key = e`. A module without CommonJS exports falls back to its
// ESM exports (a transpiled module's default export).
func (d *discovery) cjsExport(file, key string, depth int) value {
	if depth > maxDepth {
		return value{Why: "export chain too long"}
	}
	m := d.in.Models[file]
	if m == nil {
		return value{Why: file + " has no model payload (it passes no gate row)"}
	}
	whole := moduleExports(m)
	if key == "" {
		if whole == nil {
			return value{Why: file + " has no single module.exports assignment"}
		}
		return d.expr(file, whole, depth+1)
	}
	if whole != nil {
		if whole.Kind == typescript.ModelObject {
			if e := objectEntry(whole, key); e != nil {
				return d.expr(file, e, depth+1)
			}
			return value{Why: "module.exports of " + file + " has no literal entry " + key}
		}
		v := d.expr(file, whole, depth+1)
		if v.External != "" {
			return value{External: v.External, Name: key}
		}
		return value{Why: "module.exports of " + file + " is not an object literal"}
	}
	var found *typescript.ModelExpr
	for i := range m.Statements {
		s := &m.Statements[i]
		if s.Kind != typescript.StatementExpression || s.Value.Kind != typescript.ModelAssign {
			continue
		}
		if p := memberPath(s.Value.Left); p == "exports."+key || p == "module.exports."+key {
			if found != nil {
				return value{Why: file + " assigns exports." + key + " more than once"}
			}
			found = s.Value.Right
		}
	}
	if found == nil {
		return value{Why: file + " has no export " + key}
	}
	return d.expr(file, found, depth+1)
}

// exported resolves an ESM export of file: "default" (the export default
// expression), a named exported declaration or class, a local export
// clause, or a re-export from another module.
func (d *discovery) exported(file, name string, depth int) value {
	if depth > maxDepth {
		return value{Why: "export chain too long"}
	}
	m := d.in.Models[file]
	f := d.in.TypeScript[file]
	if name == "default" {
		if m != nil {
			// `export default function name() {...}` (row S6): the default
			// expression of the payload is unknown for a declaration.
			for i := range m.Statements {
				if s := &m.Statements[i]; s.Kind == typescript.StatementFunction && s.Default {
					return value{File: file, Expr: s.Value}
				}
			}
		}
		if m != nil && m.Default != nil {
			return d.expr(file, &m.Default.Value, depth+1)
		}
		if m != nil {
			for i := range m.Statements {
				if s := &m.Statements[i]; s.Kind == typescript.StatementClass && s.Default {
					return value{File: file, Class: s}
				}
			}
			if whole := moduleExports(m); whole != nil {
				return d.expr(file, whole, depth+1)
			}
		}
	}
	if m != nil {
		for i := range m.Declarations {
			if decl := &m.Declarations[i]; decl.Exported && decl.Name == name {
				return d.local(file, name, depth+1)
			}
		}
		for i := range m.Statements {
			if s := &m.Statements[i]; s.Kind == typescript.StatementClass && s.Exported && !s.Default && s.Name == name {
				return value{File: file, Class: s}
			}
			if s := &m.Statements[i]; s.Kind == typescript.StatementFunction && s.Exported && !s.Default && s.Name == name {
				return d.local(file, name, depth+1)
			}
		}
	}
	for _, ec := range f.ExportClauses {
		if ec.TypeOnly || ec.Star || ec.Alias != name {
			continue
		}
		if ec.Module == "" {
			return d.local(file, ec.Name, depth+1)
		}
		res := d.in.Resolver.Resolve(file, ec.Module)
		if res.Package() {
			return value{External: ec.Module, Name: ec.Name}
		}
		if res.File == "" && res.Refusal != "" {
			return value{Why: "re-export of " + ec.Module + " is not proven (" + res.Refusal + ")"}
		}
		if res.File == "" {
			return value{Why: "re-export of " + ec.Module + " resolves to no file of the repository"}
		}
		return d.exported(res.File, ec.Name, depth+1)
	}
	if m == nil {
		return value{Why: file + " has no model payload (it passes no gate row)"}
	}
	return value{Why: file + " has no export " + name}
}

// expr resolves an expression of file: identifiers through local, members
// of an external package value to that package's export, everything else
// to itself.
func (d *discovery) expr(file string, e *typescript.ModelExpr, depth int) value {
	if e == nil {
		return value{}
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		return d.local(file, e.Name, depth+1)
	case typescript.ModelMember:
		if spec := requireSpec(e.Object); spec != "" {
			return d.required(file, spec, e.Name, depth+1)
		}
		if e.Object.Kind == typescript.ModelIdentifier {
			if o := d.local(file, e.Object.Name, depth+1); o.External != "" && (o.Name == "" || o.Name == "*") {
				return value{External: o.External, Name: e.Name}
			}
		}
	case typescript.ModelCall:
		if spec := requireSpec(e); spec != "" {
			return d.required(file, spec, "", depth+1)
		}
	}
	return value{File: file, Expr: e}
}

// memberPath renders an identifier or a chain of non-computed members
// ("module.exports.x"), "" for anything else.
func memberPath(e *typescript.ModelExpr) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		return e.Name
	case typescript.ModelMember:
		if p := memberPath(e.Object); p != "" {
			return p + "." + e.Name
		}
	}
	return ""
}

// objectEntry returns the value of an object literal's last plain entry
// key, or nil when a later spread or unknown entry could override it.
func objectEntry(obj *typescript.ModelExpr, key string) *typescript.ModelExpr {
	var found *typescript.ModelExpr
	for i := range obj.Entries {
		en := &obj.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand:
			if en.Key == key {
				found = &en.Value
			}
		default:
			found = nil
		}
	}
	return found
}

// typeDeclaration reports a TypeScript declaration file, which has no
// run-time value.
func typeDeclaration(f string) bool {
	return strings.HasSuffix(f, ".d.ts") || strings.HasSuffix(f, ".d.mts") || strings.HasSuffix(f, ".d.cts")
}

func quote(s string) string { return `"` + s + `"` }
