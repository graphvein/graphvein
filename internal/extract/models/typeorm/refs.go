package typeorm

import (
	"strconv"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file resolves names of the cached model payload: a module-level
// binding of a file (a const, a class, a function, an import) and the
// export a module offers under a name. TypeORM projects are ES modules
// compiled by tsc, so only import and export syntax is followed; the
// shared TypeScript resolver maps module specifiers to files (relative
// paths, tsconfig paths, workspace packages).

// maxDepth bounds alias and re-export chains.
const maxDepth = 16

// binding is what a name resolves to. Exactly one of Decl, Class, Func or
// External is set when the name resolves; Why says why it does not.
type binding struct {
	// File is the file of the declaration.
	File string
	// Decl is a module-level variable declaration (its Keyword says const,
	// let or var).
	Decl *typescript.ModelDeclaration
	// Class is a module-level class declaration.
	Class *typescript.ModelStatement
	// Func is a module-level function declaration.
	Func *typescript.ModelStatement
	// Enum is a module-level enum declaration (payload row S8; TO-C4
	// enum-member defaults and TO-C5 enum columns).
	Enum *typescript.ModelStatement
	// External is the bare module specifier of a package import that the
	// resolver does not map to a repository file; Name is the exported
	// name ("*" for a namespace, "default" for a default import).
	External, Name string
	// Imported is true when the name was reached through an import or a
	// re-export (TO-K7 needs the options object to come from another
	// module of the package).
	Imported bool
	Why      string
}

func (b binding) external(module, name string) bool {
	return b.External == module && b.Name == name
}

// local resolves a module-level name of file.
func (d *discovery) local(file, name string, depth int) binding {
	if depth > maxDepth {
		return binding{Why: "alias chain too long"}
	}
	m := d.in.Models[file]
	if m != nil {
		var found *typescript.ModelDeclaration
		for i := range m.Declarations {
			if m.Declarations[i].Name == name {
				found = &m.Declarations[i]
			}
		}
		if found != nil {
			return binding{File: file, Decl: found}
		}
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Kind == typescript.StatementClass && s.Name == name {
				return binding{File: file, Class: s}
			}
			if s.Kind == typescript.StatementFunction && s.Name == name {
				return binding{File: file, Func: s}
			}
			if s.Kind == typescript.StatementEnum && s.Name == name {
				return binding{File: file, Enum: s}
			}
		}
	}
	for _, imp := range d.in.TypeScript[file].Imports {
		exported, ok := imp.Bindings[name]
		if !ok {
			continue
		}
		b := d.module(file, imp.Module, exported, depth+1)
		b.Imported = true
		return b
	}
	return binding{Why: name + " is not a module-level binding of " + file}
}

// module resolves the export name of the module spec imported by file.
func (d *discovery) module(file, spec, name string, depth int) binding {
	res := d.in.Resolver.Resolve(file, spec)
	switch {
	case res.Package():
		// Only an import the resolver states is an npm package (row TS-I5,
		// or a declared package under unread options) is the package: a
		// refused bare import may be a repository file.
		return binding{External: spec, Name: name}
	case res.File == "" && res.Refusal != "":
		return binding{Why: "import of " + spec + " is not proven (" + res.Refusal + ")"}
	case res.File == "":
		return binding{Why: "import of " + spec + " resolves to no file of the repository"}
	case name == "*":
		return binding{Why: "a namespace import of " + spec}
	}
	return d.exported(res.File, name, depth+1)
}

// exported resolves the export name of file.
func (d *discovery) exported(file, name string, depth int) binding {
	if depth > maxDepth {
		return binding{Why: "export chain too long"}
	}
	m := d.in.Models[file]
	if m != nil && name == "default" {
		for i := range m.Statements {
			s := &m.Statements[i]
			if (s.Kind == typescript.StatementClass || s.Kind == typescript.StatementFunction) && s.Default {
				b := binding{File: file}
				if s.Kind == typescript.StatementClass {
					b.Class = s
				} else {
					b.Func = s
				}
				return b
			}
		}
		if m.Default != nil && m.Default.Value.Kind == typescript.ModelIdentifier {
			b := d.local(file, m.Default.Value.Name, depth+1)
			return b
		}
	}
	if m != nil {
		for i := range m.Declarations {
			if decl := &m.Declarations[i]; decl.Exported && decl.Name == name {
				return binding{File: file, Decl: decl}
			}
		}
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Exported && !s.Default && s.Name == name {
				switch s.Kind {
				case typescript.StatementClass:
					return binding{File: file, Class: s}
				case typescript.StatementFunction:
					return binding{File: file, Func: s}
				case typescript.StatementEnum:
					return binding{File: file, Enum: s}
				}
			}
		}
	}
	for _, ec := range d.in.TypeScript[file].ExportClauses {
		if ec.TypeOnly || ec.Star || ec.Alias != name {
			continue
		}
		if ec.Module == "" {
			return d.local(file, ec.Name, depth+1)
		}
		b := d.module(file, ec.Module, ec.Name, depth+1)
		b.Imported = true
		return b
	}
	for _, ec := range d.in.TypeScript[file].ExportClauses {
		if !ec.Star || ec.TypeOnly || ec.Module == "" || name == "default" {
			continue
		}
		if b := d.module(file, ec.Module, name, depth+1); b.Why == "" && b.External == "" {
			return b
		}
	}
	return binding{Why: name + " is not exported by " + file}
}

// expr resolves an identifier expression or a member of a namespace or
// default import (`orm.Entity`, `path.join`) to its binding.
func (d *discovery) expr(file string, e *typescript.ModelExpr) binding {
	if e == nil {
		return binding{Why: "no expression"}
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		return d.local(file, e.Name, 0)
	case typescript.ModelMember:
		if e.Object.Kind == typescript.ModelIdentifier {
			o := d.local(file, e.Object.Name, 0)
			if o.External != "" && (o.Name == "*" || o.Name == "default") {
				return binding{External: o.External, Name: e.Name, Imported: true}
			}
		}
	}
	return binding{Why: "not an identifier or a namespace member"}
}

// isExternal reports an expression that resolves to the export name of
// the package module.
func (d *discovery) isExternal(file string, e *typescript.ModelExpr, module, name string) bool {
	return d.expr(file, e).external(module, name)
}

// constValue returns the initializer of a module-level const binding, the
// file it is written in and whether it was imported; why is set when the
// binding is not a const declaration.
func (d *discovery) constValue(b binding) (*typescript.ModelExpr, string) {
	switch {
	case b.Why != "":
		return nil, b.Why
	case b.Decl == nil:
		return nil, "it is not a variable declaration"
	case b.Decl.Keyword != "const":
		return nil, "it is declared with " + b.Decl.Keyword + ", so it may be reassigned"
	}
	if why := d.mutated(b.File, b.Decl.Name); why != "" {
		return nil, why
	}
	return &b.Decl.Value, ""
}

// mutated reports a module-level statement of file that assigns into the
// object bound to name (`name.key = v`, `name[k] = v`) or passes it to
// Object.assign as the target; the object literal is then not what the
// code uses.
func (d *discovery) mutated(file, name string) string {
	m := d.in.Models[file]
	if m == nil {
		return ""
	}
	root := func(e *typescript.ModelExpr) bool {
		for e != nil && (e.Kind == typescript.ModelMember || e.Kind == typescript.ModelIndex) {
			e = e.Object
		}
		return e != nil && e.Kind == typescript.ModelIdentifier && e.Name == name
	}
	for i := range m.Statements {
		s := &m.Statements[i]
		if s.Kind != typescript.StatementExpression || s.Value == nil {
			continue
		}
		v := s.Value
		if v.Kind == typescript.ModelAssign && v.Left != nil && v.Left.Kind != typescript.ModelIdentifier && root(v.Left) {
			return name + " is assigned into at " + file + ":" + itoa(s.Span.StartLine)
		}
		if v.Kind == typescript.ModelCall && v.Callee.Kind == typescript.ModelMember && v.Callee.Name == "assign" &&
			v.Callee.Object.Kind == typescript.ModelIdentifier && v.Callee.Object.Name == "Object" && len(v.Args) > 0 && root(&v.Args[0]) {
			return name + " is changed by Object.assign at " + file + ":" + itoa(s.Span.StartLine)
		}
	}
	return ""
}

// stringLiteral returns a string literal's value, or a template without
// substitutions.
func stringLiteral(e *typescript.ModelExpr) (string, bool) {
	if e == nil {
		return "", false
	}
	switch e.Kind {
	case typescript.ModelString:
		return e.Value, true
	case typescript.ModelTemplate:
		if len(e.Substitutions) == 0 {
			return e.Strings[0], true
		}
	}
	return "", false
}

// boolLiteral returns a boolean literal's value.
func boolLiteral(e *typescript.ModelExpr) (bool, bool) {
	if e == nil || e.Kind != typescript.ModelBoolean {
		return false, false
	}
	return e.Value == "true", true
}

func itoa(n int) string { return strconv.Itoa(n) }
