package drizzle

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// drizzle-kit loads each schema file with require and collects the table
// values among Object.values(exports) (prepareFromExports,
// bin.cjs:16727-16770). The exports include `export *` and `export { x }
// from` re-exports, so the modules they reach contribute tables; a
// namespace re-export (`export * as ns from`) is one object, never a
// table, and type-only re-exports are erased. Re-exports are followed
// through the shared TypeScript resolver (relative paths, tsconfig paths
// such as dokploy's `@dokploy/server/*`, workspace packages).

// Module is one module whose exports drizzle-kit sees for a config: a
// schema file (From empty, All set), or a module a re-export of an
// already reached module reaches.
type Module struct {
	File string
	// From and Line locate the re-export that reaches File.
	From string
	Line int
	// All means every named export of File is visible; otherwise Names
	// are the exported names of File that are visible.
	All   bool
	Names []string
}

// reach fills c.Modules from c.Files.
func reach(in Input, c *Config) {
	type key struct{ file, name string }
	all := map[string]bool{}
	names := map[key]bool{}
	var queue []Module
	push := func(m Module) {
		if all[m.File] {
			return
		}
		if m.All {
			all[m.File] = true
		} else {
			var fresh []string
			for _, n := range m.Names {
				if !names[key{m.File, n}] {
					names[key{m.File, n}] = true
					fresh = append(fresh, n)
				}
			}
			if len(fresh) == 0 {
				return
			}
			m.Names = fresh
		}
		queue = append(queue, m)
		c.Modules = append(c.Modules, m)
	}
	for _, f := range c.Files {
		push(Module{File: f, All: true})
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		visible := func(alias string) bool {
			if m.All {
				return true
			}
			for _, n := range m.Names {
				if n == alias {
					return true
				}
			}
			return false
		}
		f := in.TypeScript[m.File]
		for _, ec := range f.ExportClauses {
			if ec.TypeOnly || ec.Namespace {
				continue
			}
			module := ec.Module
			name := ec.Name
			if module == "" {
				// `export { x }` of an imported binding re-exports the
				// imported module's value.
				if ec.Star || !visible(ec.Alias) {
					continue
				}
				module, name = importOf(f, ec.Name)
				if module == "" || name == "*" {
					continue
				}
			} else if !ec.Star && !visible(ec.Alias) {
				continue
			}
			res := in.Resolver.Resolve(m.File, module)
			switch {
			case res.Package():
				continue
			case res.File == "":
				c.Unresolved = append(c.Unresolved, fmt.Sprintf("re-export of %q at %s:%d resolves to no file of the repository; the tables it exports are not read", module, m.File, ec.Span.StartLine))
				continue
			}
			if !strings.HasPrefix(module, ".") {
				in.tag("DR-K1/tsconfig `paths`")
			}
			next := Module{File: res.File, From: m.File, Line: ec.Span.StartLine}
			if ec.Star {
				in.tag("DR-K1/`export *`")
				next.All = m.All
				next.Names = m.Names
			} else {
				in.tag("DR-K1/`export { } from`")
				next.Names = []string{name}
			}
			push(next)
		}
	}
}

// importOf returns the module and exported name a local binding imports,
// or "".
func importOf(f typescript.Facts, local string) (module, name string) {
	for _, imp := range f.Imports {
		if n, ok := imp.Bindings[local]; ok {
			return imp.Module, n
		}
	}
	return "", ""
}

// tableBuilders are the drizzle-orm table builders of each dialect module
// (row DR-T1 and the builders D names); a file whose module-level
// declarations call one is reported when no group reads it.
var tableBuilders = map[string]bool{
	"pgTable": true, "sqliteTable": true, "mysqlTable": true, "singlestoreTable": true, "gelTable": true,
	"pgTableCreator": true, "sqliteTableCreator": true, "mysqlTableCreator": true,
}

// schemaBuilders make `<s>.table(...)` a table builder call.
var schemaBuilders = map[string]bool{"pgSchema": true, "mysqlSchema": true}

// builderFiles counts, per file with a model payload, the module-level
// declarations whose value is a table builder call (a call chain such as
// `pgTable(...).enableRLS()` included). Commented-out code is not in the
// payload.
func builderFiles(in Input) map[string]int {
	out := map[string]int{}
	for file, m := range in.Models {
		if m == nil {
			continue
		}
		f := in.TypeScript[file]
		schemas := map[string]bool{}
		for _, d := range m.Declarations {
			if callee := chainRoot(d.Value); callee != nil && callee.Kind == typescript.ModelIdentifier && schemaBuilders[dialectImport(f, callee.Name)] {
				schemas[d.Name] = true
			}
		}
		for _, d := range m.Declarations {
			if isTableCall(f, d.Value, schemas) {
				out[file]++
			}
		}
	}
	return out
}

// chainRoot follows a call chain to its first call and returns its callee.
func chainRoot(e typescript.ModelExpr) *typescript.ModelExpr {
	for e.Kind == typescript.ModelCall && e.Callee.Kind == typescript.ModelMember && e.Callee.Object.Kind == typescript.ModelCall {
		e = *e.Callee.Object
	}
	if e.Kind != typescript.ModelCall {
		return nil
	}
	return e.Callee
}

func isTableCall(f typescript.Facts, e typescript.ModelExpr, schemas map[string]bool) bool {
	callee := chainRoot(e)
	if callee == nil {
		return false
	}
	switch callee.Kind {
	case typescript.ModelIdentifier:
		return tableBuilders[dialectImport(f, callee.Name)]
	case typescript.ModelMember:
		obj := callee.Object
		if obj.Kind != typescript.ModelIdentifier {
			return false
		}
		if callee.Name == "table" {
			// A pgSchema const of this file, or one imported from
			// another file (a marker counts it either way).
			if schemas[obj.Name] {
				return true
			}
			module, _ := importOf(f, obj.Name)
			return module != "" && !strings.HasPrefix(module, "drizzle-orm")
		}
		// A namespace import of a dialect module: pg.pgTable(...).
		return dialectImport(f, obj.Name) == "*" && tableBuilders[callee.Name]
	}
	return false
}

// dialectImport returns the name a local identifier imports from a
// drizzle-orm/<dialect>-core module ("*" for a namespace import), or "".
func dialectImport(f typescript.Facts, local string) string {
	for _, imp := range f.Imports {
		if !strings.HasPrefix(imp.Module, "drizzle-orm/") || !strings.Contains(imp.Module, "-core") {
			continue
		}
		if n, ok := imp.Bindings[local]; ok {
			return n
		}
	}
	return ""
}

// owner is the directory of the nearest package.json above file (manifests
// sorted), "." when none.
func owner(manifests []string, file string) string {
	best, n := ".", 0
	for _, m := range manifests {
		if d := path.Dir(m); d != "." && strings.HasPrefix(file, d+"/") && len(d) > n {
			best, n = d, len(d)
		}
	}
	return best
}

// ownedBuilders lists the builder files whose package (owner) is one of
// owners and that are not in skip, sorted.
func ownedBuilders(builders map[string]int, manifests []string, owners, skip map[string]bool) []string {
	var list []string
	for f := range builders {
		if !skip[f] && owners[owner(manifests, f)] {
			list = append(list, f)
		}
	}
	sort.Strings(list)
	return list
}

// describeBuilders names builder files with their table-builder counts.
func describeBuilders(list []string, builders map[string]int) string {
	parts := make([]string, len(list))
	total := 0
	for i, f := range list {
		parts[i] = fmt.Sprintf("%s (%d)", f, builders[f])
		total += builders[f]
	}
	return fmt.Sprintf("%d file(s) with %d table builder call(s): %s", len(list), total, strings.Join(parts, ", "))
}
