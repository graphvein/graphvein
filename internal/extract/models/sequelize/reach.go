package sequelize

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file finds the models and association calls an instance reaches
// (row SQ-K1, task file "Discovery"): an `init` statement whose
// `sequelize` option, or a `define` call whose receiver, resolves to the
// instance; a factory's define reached by a call that passes the instance
// as the first argument; association calls at module level of a module
// the group reaches, and inside `associate` functions that the recognised
// loop runs. Everything else that could add a table or an attribute is a
// marker, and makes the group incomplete.

// walk visits e and every expression below it, in source order, with
// whether the visit is inside a function body.
func walk(e *typescript.ModelExpr, inFunction bool, visit func(e *typescript.ModelExpr, inFunction bool)) {
	if e == nil {
		return
	}
	visit(e, inFunction)
	walk(e.Callee, inFunction, visit)
	walk(e.Object, inFunction, visit)
	walk(e.Tag, inFunction, visit)
	walk(e.Left, inFunction, visit)
	walk(e.Right, inFunction, visit)
	walk(e.Index, inFunction, visit)
	for i := range e.Args {
		walk(&e.Args[i], inFunction, visit)
	}
	for i := range e.Substitutions {
		walk(&e.Substitutions[i], inFunction, visit)
	}
	for i := range e.Elements {
		walk(&e.Elements[i], inFunction, visit)
	}
	for i := range e.Entries {
		walk(&e.Entries[i].Value, inFunction, visit)
	}
	if e.Kind == typescript.ModelArrow || e.Kind == typescript.ModelFunction {
		if len(e.Statements) == 0 {
			walk(e.Body, true, visit)
		}
		walkStatements(e.Statements, true, visit)
	}
}

func walkStatements(list []typescript.ModelStatement, inFunction bool, visit func(e *typescript.ModelExpr, inFunction bool)) {
	for i := range list {
		walk(list[i].Extends, inFunction, visit)
		walk(list[i].Value, inFunction, visit)
	}
}

// walkModule visits every expression of a payload: declarations,
// statements and the default export.
func walkModule(m *typescript.ModelSyntax, visit func(e *typescript.ModelExpr, inFunction bool)) {
	for i := range m.Declarations {
		walk(&m.Declarations[i].Value, false, visit)
	}
	walkStatements(m.Statements, false, visit)
	if m.Default != nil {
		walk(&m.Default.Value, false, visit)
	}
}

// methodCall returns the method name of a call `<object>.<method>(...)`.
func methodCall(e *typescript.ModelExpr) string {
	if e == nil || e.Kind != typescript.ModelCall || e.Callee == nil || e.Callee.Kind != typescript.ModelMember {
		return ""
	}
	return e.Callee.Name
}

// siteKey identifies a call site by file and start position, so payload
// calls and the ungated call cache can be matched.
func siteKey(file string, s contract.Span) string {
	return fmt.Sprintf("%s:%d:%d", file, s.StartLine, s.StartColumn)
}

// defineSite is a define call found in a factory body.
type defineSite struct {
	file    string
	factory *typescript.ModelExpr
	call    *typescript.ModelExpr
	binding string
	span    contract.Span
}

// factoryDefines returns the define calls of a module's factory: the
// module.exports value is an arrow or function whose first parameter is a
// plain name p, and a statement of its body declares `const v =
// p.define(...)`, calls `p.define(...)` or returns it.
func factoryDefines(file string, m *typescript.ModelSyntax) []defineSite {
	fn := moduleExports(m)
	if fn == nil || (fn.Kind != typescript.ModelArrow && fn.Kind != typescript.ModelFunction) || len(fn.Params) == 0 || fn.Params[0] == "" {
		return nil
	}
	p := fn.Params[0]
	var out []defineSite
	for i := range fn.Statements {
		s := &fn.Statements[i]
		call := s.Value
		if call == nil || methodCall(call) != "define" || call.Callee.Object.Kind != typescript.ModelIdentifier || call.Callee.Object.Name != p {
			continue
		}
		site := defineSite{file: file, factory: fn, call: call, span: call.Span}
		if s.Kind == typescript.StatementDeclaration && s.Keyword == "const" {
			site.binding = s.Name
		}
		out = append(out, site)
	}
	return out
}

// reachModels fills g.Models from the payloads of the package's files.
func (d *discovery) reachModels(g *Instance) {
	p := g.Package
	for _, f := range d.packageFiles(p) {
		m := d.in.Models[f]
		if m == nil {
			continue
		}
		// Module-level init statements and define calls.
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Kind != typescript.StatementExpression {
				continue
			}
			d.moduleSite(g, f, m, s.Value, "")
		}
		for i := range m.Declarations {
			decl := &m.Declarations[i]
			d.moduleSite(g, f, m, &decl.Value, decl.Name)
		}
		// Factory calls with the instance as the first argument.
		d.factoryCalls(g, f, m)
	}
}

// moduleSite records a module-level init statement or define call of
// file that reaches g. bound is the declaration name the call is the
// initializer of ("" for a statement).
func (d *discovery) moduleSite(g *Instance, file string, m *typescript.ModelSyntax, e *typescript.ModelExpr, bound string) {
	switch methodCall(e) {
	case "init":
		if bound != "" || len(e.Args) < 2 || e.Args[1].Kind != typescript.ModelObject {
			return
		}
		opt := optionValue(&e.Args[1], "sequelize")
		if opt == nil || opt.Kind == typescript.ModelUnknown {
			return
		}
		if inst, _ := d.instanceOf(g.Package, file, opt); inst != g {
			return
		}
		d.in.tag("SQ-K1/imported instance binding")
		model := &Model{File: file, Form: "init", Context: "module", Span: e.Span, Call: e}
		if recv := e.Callee.Object; recv.Kind == typescript.ModelIdentifier {
			model.ClassName = recv.Name
			model.Class = classDecl(m, recv.Name)
		}
		g.Models = append(g.Models, model)
		d.reached[siteKey(file, e.Span)] = true
	case "define":
		if inst, _ := d.instanceOf(g.Package, file, e.Callee.Object); inst != g {
			return
		}
		if g.File != file {
			d.in.tag("SQ-K1/imported instance binding")
		}
		g.Models = append(g.Models, &Model{File: file, Form: "define", Context: "module", Span: e.Span, Call: e, Binding: bound})
		d.reached[siteKey(file, e.Span)] = true
	}
}

// factoryCalls finds module-level calls of file (outside function bodies)
// whose callee resolves to a factory module's exports and whose first
// argument resolves to g, and records one model per define in that
// factory. A call that is the value of a property of a module-level const
// object records the object and key (the associate loop's `models`).
func (d *discovery) factoryCalls(g *Instance, file string, m *typescript.ModelSyntax) {
	visit := func(object, key string) func(e *typescript.ModelExpr, inFunction bool) {
		return func(e *typescript.ModelExpr, inFunction bool) {
			if inFunction || e.Kind != typescript.ModelCall || len(e.Args) == 0 {
				return
			}
			var callee value
			switch {
			case e.Callee.Kind == typescript.ModelIdentifier:
				callee = d.local(file, e.Callee.Name, 0)
			case requireSpec(e.Callee) != "":
				// `require("./user")(sequelize, DataTypes)` (the sequelize-cli
				// form).
				callee = d.expr(file, e.Callee, 0)
			default:
				return
			}
			if callee.Expr == nil || callee.File == "" || callee.File == file {
				return
			}
			defines := factoryDefines(callee.File, d.in.Models[callee.File])
			if len(defines) == 0 || defines[0].factory != callee.Expr {
				return
			}
			if inst, _ := d.instanceOf(g.Package, file, &e.Args[0]); inst != g {
				return
			}
			d.in.tag("SQ-K1/factory's first parameter")
			if e.Callee.Kind != typescript.ModelIdentifier {
				d.in.tag("SQ-K1/`require(...)(...)` factory call")
			}
			for _, site := range defines {
				fac := &Factory{Expr: site.factory, Params: site.factory.Params, CallFile: file, Call: e, Object: object, Key: key}
				g.Models = append(g.Models, &Model{File: site.file, Form: "define", Context: "factory", Span: site.span, Call: site.call, Binding: site.binding, Factory: fac})
				d.reached[siteKey(site.file, site.span)] = true
			}
		}
	}
	for i := range m.Declarations {
		decl := &m.Declarations[i]
		if decl.Keyword == "const" && decl.Value.Kind == typescript.ModelObject {
			for j := range decl.Value.Entries {
				en := &decl.Value.Entries[j]
				key := ""
				if en.Kind == typescript.ModelProperty {
					key = en.Key
				}
				walk(&en.Value, false, visit(decl.Name, key))
			}
			continue
		}
		walk(&decl.Value, false, visit("", ""))
	}
	for i := range m.Statements {
		st := &m.Statements[i]
		// `db.User = <factory call>`: an entry of the models object db
		// (modelsObject).
		if st.Kind == typescript.StatementExpression && st.Value.Kind == typescript.ModelAssign {
			if l := st.Value.Left; l.Kind == typescript.ModelMember && l.Object.Kind == typescript.ModelIdentifier {
				walk(st.Value.Right, false, visit(l.Object.Name, l.Name))
				continue
			}
		}
		walkStatements(m.Statements[i:i+1], false, visit("", ""))
	}
	if m.Default != nil {
		walk(&m.Default.Value, false, visit("", ""))
	}
}

// unreachedModels reports the model definition sites of the package that
// no instance reaches, and refuses definitions that reach two instances:
// init statements and define calls at module level whose receiver does not
// resolve, factories never called with an instance, define calls in any
// other function (the call-site cache; open point O8's project wrappers)
// and directory loaders. Sites in non-test files make every instance of
// the package incomplete.
func (d *discovery) unreachedModels(p *Package) {
	// A definition reached by two instances is not claimed.
	owners := map[string][]*Instance{}
	for _, g := range p.Instances {
		for _, m := range g.Models {
			k := siteKey(m.File, m.Span)
			if len(owners[k]) == 0 || owners[k][len(owners[k])-1] != g {
				owners[k] = append(owners[k], g)
			}
		}
	}
	for _, g := range p.Instances {
		var kept []*Model
		for _, m := range g.Models {
			if len(owners[siteKey(m.File, m.Span)]) > 1 {
				continue
			}
			kept = append(kept, m)
		}
		g.Models = kept
	}
	var keys []string
	for k := range owners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if gs := owners[k]; len(gs) > 1 {
			file := strings.SplitN(k, ":", 2)[0]
			var names []string
			for _, g := range gs {
				names = append(names, g.Root)
			}
			msg := fmt.Sprintf("%s: the model definition reaches %d Sequelize instances (%s); it is not read", k, len(gs), strings.Join(names, ", "))
			for _, g := range gs {
				d.diag(file, codeUnread, msg+listMarker(g.SetKey()))
			}
		}
	}
	type wrapper struct {
		file, name string
		line       int
	}
	var wrappers []wrapper
	var markers []string
	tests := 0
	mark := func(file, msg string) {
		if d.isTest(file) {
			tests++
			return
		}
		markers = append(markers, msg)
		d.unreadDiag(p, file, msg)
	}
	for _, f := range d.packageFiles(p) {
		m := d.in.Models[f]
		if m != nil {
			check := func(e *typescript.ModelExpr, bound string) {
				switch methodCall(e) {
				case "init":
					if bound != "" || len(e.Args) < 2 || e.Args[1].Kind != typescript.ModelObject || d.reached[siteKey(f, e.Span)] {
						return
					}
					d.reported[siteKey(f, e.Span)] = true
					opt := optionValue(&e.Args[1], "sequelize")
					if opt == nil || opt.Kind == typescript.ModelUnknown {
						mark(f, fmt.Sprintf("%s:%d: init without a readable sequelize option; the model is not read", f, e.Span.StartLine))
						return
					}
					if _, why := d.instanceOf(p, f, opt); why != "" {
						mark(f, fmt.Sprintf("%s:%d: the init's sequelize option does not resolve to an instance (%s); the model is not read", f, e.Span.StartLine, why))
					}
				case "define":
					if d.reached[siteKey(f, e.Span)] || len(e.Args) == 0 {
						return
					}
					d.reported[siteKey(f, e.Span)] = true
					if _, why := d.instanceOf(p, f, e.Callee.Object); why != "" {
						mark(f, fmt.Sprintf("%s:%d: the define's receiver does not resolve to an instance (%s); the model is not read", f, e.Span.StartLine, why))
					}
				}
			}
			for i := range m.Statements {
				if s := &m.Statements[i]; s.Kind == typescript.StatementExpression {
					check(s.Value, "")
				}
			}
			for i := range m.Declarations {
				check(&m.Declarations[i].Value, m.Declarations[i].Name)
			}
			for _, site := range factoryDefines(f, m) {
				k := siteKey(f, site.span)
				if !d.reached[k] && len(owners[k]) == 0 {
					d.reported[k] = true
					mark(f, fmt.Sprintf("%s:%d: a factory define that no module-level call runs with an instance; the model is not read", f, site.span.StartLine))
				}
			}
			// A class + init inside a function (the sequelize-cli model
			// template: a CommonJS factory declaring the class and calling
			// init): not read, with or without associate.
			walkModule(m, func(e *typescript.ModelExpr, inFunction bool) {
				if !inFunction || methodCall(e) != "init" || len(e.Args) < 2 || e.Args[1].Kind != typescript.ModelObject {
					return
				}
				if opt := optionValue(&e.Args[1], "sequelize"); opt == nil {
					return
				}
				k := siteKey(f, e.Span)
				if d.reached[k] || d.reported[k] {
					return
				}
				d.reported[k] = true
				mark(f, fmt.Sprintf("%s:%d: %s.init(...) inside a function (a class model built in a CommonJS factory, the sequelize-cli model template); the model is not read (row D)", f, e.Span.StartLine, memberPath(e.Callee.Object)))
			})
			loaders := false
			walkModule(m, func(e *typescript.ModelExpr, _ bool) {
				if e.Kind == typescript.ModelCall && e.Callee.Kind == typescript.ModelIdentifier && e.Callee.Name == "require" && requireSpec(e) == "" {
					loaders = true
				}
			})
			if loaders {
				mark(f, fmt.Sprintf("%s: a require with a computed path (a directory loader of %s/); the models it loads are not read (row D)", f, path.Dir(f)))
			}
		}
		for _, c := range clientCalls(d.in.TypeScript[f]) {
			if c.Expression.Object.Name != "define" || len(c.Expression.Args) < 2 {
				continue
			}
			k := siteKey(f, c.Span)
			if d.reached[k] || d.reported[k] || len(owners[k]) > 0 {
				continue
			}
			if c.OwnerName != "" {
				wrappers = append(wrappers, wrapper{file: f, name: c.OwnerName, line: c.Span.StartLine})
			}
			mark(f, fmt.Sprintf("%s:%d: %s(...) inside a function (a model built in a function or a project wrapper); the table it may define is not read (row D)", f, c.Span.StartLine, c.Expression.Object.Text))
		}
	}
	// Calls of a function whose body defines a table (open point O8: a
	// project wrapper such as Temporal(X, sequelize)) name the model.
	for _, w := range wrappers {
		for _, f := range d.packageFiles(p) {
			f := f
			calls := d.in.TypeScript[f].Routes
			if calls == nil {
				continue
			}
			for _, c := range calls.Clients {
				callee := c.Expression.Object
				if c.Expression.Kind != "call_expression" || callee == nil || callee.Kind != "identifier" {
					continue
				}
				if !(f == w.file && callee.Name == w.name) && !d.importsWrapper(f, callee.Name, w.file, w.name) {
					continue
				}
				first := ""
				if len(c.Expression.Args) > 0 {
					first = c.Expression.Args[0].Text
				}
				mark(f, fmt.Sprintf("%s:%d: %s(%s, ...) calls the project wrapper %s (%s:%d), whose body defines a table; that table is not read (row D, open point O8)", f, c.Span.StartLine, callee.Name, first, w.name, w.file, w.line))
			}
		}
	}
	d.testSites[p] += tests
	for _, g := range p.Instances {
		g.Incomplete = append(g.Incomplete, markers...)
	}
}

// clientCalls returns the cached member calls of a file (every call site,
// gated or not, with its reduced callee chain and arguments).
func clientCalls(f typescript.Facts) []facts.ClientCall {
	if f.Routes == nil {
		return nil
	}
	var out []facts.ClientCall
	for _, c := range f.Routes.Clients {
		if c.Expression.Kind != "call_expression" || c.Expression.Object == nil || c.Expression.Object.Kind != "member_expression" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// importsWrapper reports a local name of file that an ESM import binds to
// the default or the named export name of target (the payload does not
// keep module-level arrow declarations, so a wrapper is matched by its
// export).
func (d *discovery) importsWrapper(file, local, target, name string) bool {
	for _, imp := range d.in.TypeScript[file].Imports {
		exported, ok := imp.Bindings[local]
		if !ok {
			continue
		}
		if res := d.in.Resolver.Resolve(file, imp.Module); res.File == target && (exported == "default" || exported == name) {
			return true
		}
	}
	return false
}
