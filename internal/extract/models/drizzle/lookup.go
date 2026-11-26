package drizzle

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// ref is what an identifier of a schema module names, as far as the
// payload and the TypeScript facts show it: a binding imported from an
// external package (Module and Export; Export "*" for a namespace
// import), or a module-level declaration captured in a file's model
// payload (File and Decl). The zero ref is "not known".
type ref struct {
	Module, Export string
	File           string
	Decl           *typescript.ModelDeclaration
}

func (r ref) external() bool { return r.Module != "" }

// lookup resolves a local identifier of file: an import binding (followed
// through the shared resolver to the exporting file's declaration, its
// `export { }` clauses and `export *` barrels), else a module-level
// declaration of the file itself. Identifiers the payload does not hold (a
// function, a class, a declaration whose initializer is a literal) are
// not known.
func (in Input) lookup(file, local string) ref {
	return in.lookupSeen(file, local, map[string]bool{})
}

func (in Input) lookupSeen(file, local string, seen map[string]bool) ref {
	key := file + "\x00" + local
	if seen[key] {
		return ref{}
	}
	seen[key] = true
	defer delete(seen, key)
	for _, imp := range in.TypeScript[file].Imports {
		if exported, ok := imp.Bindings[local]; ok {
			return in.imported(file, imp.Module, exported, seen)
		}
	}
	if m := in.Models[file]; m != nil {
		for i := range m.Declarations {
			if m.Declarations[i].Name == local {
				return ref{File: file, Decl: &m.Declarations[i]}
			}
		}
	}
	if target, ok := in.alias(file, local); ok {
		return in.lookupSeen(file, target, seen)
	}
	return ref{}
}

// alias returns the identifier a module-level `const local = other`
// holds. The payload does not capture an identifier initializer, so the
// TypeScript facts' module bindings answer it (kaneo's
// `export const user = userTable`, used by its relations).
func (in Input) alias(file, local string) (string, bool) {
	r := in.TypeScript[file].Routes
	if r == nil || len(r.Scopes) == 0 {
		return "", false
	}
	b, ok := r.Scopes[0].Bindings[local]
	if !ok || !b.ModuleConst || b.Value.Kind != "identifier" || b.Value.Name == "" || b.Value.Name == local {
		return "", false
	}
	return b.Value.Name, true
}

// imported resolves name exported by module as file imports it.
func (in Input) imported(file, module, name string, seen map[string]bool) ref {
	if module == "drizzle-orm" || strings.HasPrefix(module, "drizzle-orm/") {
		return ref{Module: module, Export: name}
	}
	if in.Resolver == nil {
		return ref{}
	}
	res := in.Resolver.Resolve(file, module)
	switch {
	case res.Package():
		return ref{Module: module, Export: name}
	case res.File == "" || name == "*" || name == "default":
		return ref{}
	}
	return in.exportOf(res.File, name, seen)
}

// exportOf resolves the value file exports under name: an export clause
// naming it, an exported declaration, or exactly one `export *` barrel
// that provides it (two different ones: not known).
func (in Input) exportOf(file, name string, seen map[string]bool) ref {
	key := file + "\x00export\x00" + name
	if seen[key] {
		return ref{}
	}
	seen[key] = true
	defer delete(seen, key)
	f := in.TypeScript[file]
	for _, c := range f.ExportClauses {
		if c.Star || c.Alias != name {
			continue
		}
		if c.TypeOnly || c.Namespace {
			return ref{}
		}
		if c.Module != "" {
			return in.imported(file, c.Module, c.Name, seen)
		}
		return in.lookupSeen(file, c.Name, seen)
	}
	if m := in.Models[file]; m != nil {
		for i := range m.Declarations {
			if d := &m.Declarations[i]; d.Name == name && d.Exported {
				return ref{File: file, Decl: d}
			}
		}
	}
	if r := f.Routes; r != nil && r.Exports[name] == name {
		if target, ok := in.alias(file, name); ok {
			return in.lookupSeen(file, target, seen)
		}
	}
	var found ref
	for _, c := range f.ExportClauses {
		if !c.Star || c.TypeOnly || c.Namespace {
			continue
		}
		r := in.imported(file, c.Module, name, seen)
		if r == (ref{}) {
			continue
		}
		if found != (ref{}) && found != r {
			return ref{}
		}
		found = r
	}
	return found
}

// calleeRef resolves the callee of a call: an identifier, or a member of a
// namespace import (`pg.text`).
func (in Input) calleeRef(file string, callee *typescript.ModelExpr) ref {
	if callee == nil {
		return ref{}
	}
	switch callee.Kind {
	case typescript.ModelIdentifier:
		return in.lookup(file, callee.Name)
	case typescript.ModelMember:
		if callee.Object.Kind != typescript.ModelIdentifier {
			return ref{}
		}
		if r := in.lookup(file, callee.Object.Name); r.external() && r.Export == "*" {
			return ref{Module: r.Module, Export: callee.Name}
		}
	}
	return ref{}
}

// coreDialect maps a drizzle-orm dialect module to the dialect whose rules
// apply (DR-V4).
var coreDialect = map[string]string{
	"drizzle-orm/pg-core":     "postgresql",
	"drizzle-orm/sqlite-core": "sqlite",
	"drizzle-orm/mysql-core":  "mysql",
}

// isSQLTag reports whether a tagged template's tag is drizzle-orm's sql.
func (in Input) isSQLTag(file string, tag *typescript.ModelExpr) bool {
	r := in.calleeRef(file, tag)
	return r.Module == "drizzle-orm" && r.Export == "sql"
}

// chain splits a call chain `base(...).m1(...).m2(...)` into its base call
// and its method calls in source order. ok is false when e is not a call
// or a link of the chain is not a method call.
type link struct {
	Name string
	Args []typescript.ModelExpr
	Line int
}

func chain(e typescript.ModelExpr) (base typescript.ModelExpr, links []link, ok bool) {
	for e.Kind == typescript.ModelCall && e.Callee != nil && e.Callee.Kind == typescript.ModelMember && e.Callee.Object.Kind == typescript.ModelCall {
		links = append([]link{{Name: e.Callee.Name, Args: e.Args, Line: e.Span.EndLine}}, links...)
		e = *e.Callee.Object
	}
	if e.Kind != typescript.ModelCall {
		return e, nil, false
	}
	return e, links, true
}
