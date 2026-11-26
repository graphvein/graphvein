package typeorm

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads the NestJS forms (rows TO-K3, TO-K4, TO-K5):
// `TypeOrmModule.forRoot(<options>)` and `forRootAsync({useFactory, ...})`
// as model groups, `TypeOrmModule.forFeature([...], name?)` as entity
// reach under `autoLoadEntities: true`, and `configService.get(<token>)`
// read through the package's single `registerAs(<token>, () => options)`.
// `@nestjs/typeorm` is cited at 11.0.0 and 12.0.3 under `dist/`.

// nestCall is one TypeOrmModule call found in a payload.
type nestCall struct {
	file   string
	call   *typescript.ModelExpr
	method string
	// inFunction is set for a call inside a function or arrow body: it
	// runs only when the function is called, so it is not read.
	inFunction bool
}

// nestCalls returns the TypeOrmModule.forRoot, forRootAsync and
// forFeature calls of file's payload in source order: module-level
// declarations, statements (class decorators included) and the default
// export, and the bodies of the functions they hold.
func (d *discovery) nestCalls(file string) []nestCall {
	m := d.in.Models[file]
	if m == nil {
		return nil
	}
	var out []nestCall
	var expr func(e *typescript.ModelExpr, fn bool, depth int)
	var stmts func(list []typescript.ModelStatement, fn bool, depth int)
	expr = func(e *typescript.ModelExpr, fn bool, depth int) {
		if e == nil || depth > 64 {
			return
		}
		if e.Kind == typescript.ModelCall && e.Callee.Kind == typescript.ModelMember {
			switch e.Callee.Name {
			case "forRoot", "forRootAsync", "forFeature":
				if d.isExternal(file, e.Callee.Object, pkgNest, "TypeOrmModule") {
					out = append(out, nestCall{file: file, call: e, method: e.Callee.Name, inFunction: fn})
				}
			}
		}
		expr(e.Callee, fn, depth+1)
		expr(e.Object, fn, depth+1)
		expr(e.Tag, fn, depth+1)
		expr(e.Left, fn, depth+1)
		expr(e.Right, fn, depth+1)
		expr(e.Index, fn, depth+1)
		expr(e.Test, fn, depth+1)
		expr(e.Then, fn, depth+1)
		expr(e.Else, fn, depth+1)
		for i := range e.Args {
			expr(&e.Args[i], fn, depth+1)
		}
		for i := range e.Elements {
			expr(&e.Elements[i], fn, depth+1)
		}
		for i := range e.Substitutions {
			expr(&e.Substitutions[i], fn, depth+1)
		}
		for i := range e.Entries {
			expr(&e.Entries[i].Value, fn, depth+1)
		}
		if e.Kind == typescript.ModelArrow || e.Kind == typescript.ModelFunction {
			if len(e.Statements) > 0 {
				stmts(e.Statements, true, depth+1)
			} else {
				expr(e.Body, true, depth+1)
			}
		}
	}
	stmts = func(list []typescript.ModelStatement, fn bool, depth int) {
		for i := range list {
			s := &list[i]
			expr(s.Value, fn, depth+1)
			expr(s.Extends, fn, depth+1)
			for j := range s.DecoratorList {
				expr(&s.DecoratorList[j], fn, depth+1)
			}
			stmts(s.Body, fn, depth+1)
			stmts(s.Else, fn, depth+1)
			stmts(s.Catch, fn, depth+1)
			stmts(s.Finally, fn, depth+1)
			stmts(s.Statics, true, depth+1)
		}
	}
	type site struct {
		line int
		run  func()
	}
	var order []site
	for i := range m.Declarations {
		decl := &m.Declarations[i]
		order = append(order, site{decl.Span.StartLine, func() { expr(&decl.Value, false, 0) }})
	}
	for i := range m.Statements {
		s := m.Statements[i : i+1]
		order = append(order, site{s[0].Span.StartLine, func() { stmts(s, false, 0) }})
	}
	if m.Default != nil {
		order = append(order, site{m.Default.Span.StartLine, func() { expr(&m.Default.Value, false, 0) }})
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && order[j].line < order[j-1].line; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for _, s := range order {
		s.run()
	}
	return out
}

// features reads the package's TypeOrmModule.forFeature calls (TO-K4).
// Calls in test files are counted in one marker and never reach a
// table; a call inside a function body is not read and leaves
// every autoLoadEntities group incomplete.
func (d *discovery) features(p *Package) {
	tests := 0
	for _, f := range d.packageFiles(p) {
		for _, c := range d.nestCalls(f) {
			if c.method != "forFeature" {
				continue
			}
			if d.isTest(f) {
				tests++
				continue
			}
			ft := &Feature{File: f, Span: c.call.Span, Name: "default", NameKnown: true}
			if c.inFunction {
				ft.NameKnown = false
				ft.Unread = append(ft.Unread, fmt.Sprintf("%s:%d: a TypeOrmModule.forFeature call inside a function is not read", f, c.call.Span.StartLine))
				p.Features = append(p.Features, ft)
				continue
			}
			d.in.tag("TO-K4/`TypeOrmModule.forFeature(")
			if len(c.call.Args) >= 2 {
				if n, ok := stringLiteral(&c.call.Args[1]); ok {
					ft.Name = n
				} else {
					ft.NameKnown = false
					ft.Unread = append(ft.Unread, fmt.Sprintf("%s:%d: the data source of a TypeOrmModule.forFeature call is not a string literal", f, c.call.Span.StartLine))
				}
			}
			if len(c.call.Args) == 0 || c.call.Args[0].Kind != typescript.ModelArray {
				ft.Unread = append(ft.Unread, fmt.Sprintf("%s:%d: the entities of a TypeOrmModule.forFeature call are not an array literal", f, c.call.Span.StartLine))
				p.Features = append(p.Features, ft)
				continue
			}
			for i := range c.call.Args[0].Elements {
				el := &c.call.Args[0].Elements[i]
				e, why := d.entityOf(p, f, el)
				switch {
				case e == nil || !e.Typeorm:
					if why == "" {
						why = "its @Entity decorator is not typeorm's"
					}
					ft.Unread = append(ft.Unread, fmt.Sprintf("%s:%d: a TypeOrmModule.forFeature element is not read (%s)", f, el.Span.StartLine, why))
				case e.Form == "Entity":
					ft.Entities = append(ft.Entities, e)
				}
				// A @ViewEntity or @ChildEntity class adds no table of its
				// own; its class is a row D marker (unreached).
			}
			p.Features = append(p.Features, ft)
		}
	}
	if tests > 0 {
		d.diag(p.Manifest, codeUnread, fmt.Sprintf("%s: %d TypeOrmModule.forFeature calls in test files are not read", p.Manifest, tests))
	}
}

// nestRoots reads the package's TypeOrmModule.forRoot and forRootAsync
// calls as model groups (TO-K3), one per call; calls in test files or
// inside function bodies are markers.
func (d *discovery) nestRoots(p *Package) {
	for _, f := range d.packageFiles(p) {
		for _, c := range d.nestCalls(f) {
			if c.method == "forFeature" {
				continue
			}
			line := c.call.Span.StartLine
			switch {
			case d.isTest(f):
				d.diag(f, codeUnread, fmt.Sprintf("%s:%d: TypeOrmModule.%s in a test file is not read", f, line, c.method))
				continue
			case c.inFunction:
				d.in.tag("D")
				d.diag(f, codeUnread, fmt.Sprintf("%s:%d: TypeOrmModule.%s inside a function is not read (row D)", f, line, c.method))
				continue
			}
			g := &Group{File: f, Root: f, Span: c.call.Span, Package: p, Form: c.method, Name: "default", NameKnown: true}
			p.Groups = append(p.Groups, g)
			if c.method == "forRoot" {
				d.forRoot(g, c.call)
			} else {
				d.forRootAsync(g, c.call)
			}
		}
	}
}

// forRoot reads `TypeOrmModule.forRoot(<options>, <name>?)`: the options
// as TO-K1 reads a data source's, the data source name from a literal
// second argument (12.x, `dist/typeorm.module.js:25-26` at 12.0.3) or the
// options' `name` (default "default", `common/typeorm.utils.js:58-66`).
func (d *discovery) forRoot(g *Group, call *typescript.ModelExpr) {
	d.in.tag("TO-K3/`TypeOrmModule.forRoot(<options>)`")
	if len(call.Args) == 0 {
		d.in.tag("D")
		g.OptionsWhy = "TypeOrmModule.forRoot() without options reads the environment or ormconfig at run time (row D)"
		return
	}
	g.Options, g.OptionsWhy = d.candidates(g.File, &call.Args[0], true, optionContext{})
	g.Selector = d.selector(g.File, &call.Args[0])
	if g.OptionsWhy != "" {
		return
	}
	if g.Options[0].From == "imported" {
		d.in.tag("TO-K7/`TypeOrmModule.forRoot(X)`")
	}
	d.nameFromOptions(g)
	if len(call.Args) >= 2 {
		d.in.tag("TO-K3/`forRoot(options, name)`")
		n, ok := stringLiteral(&call.Args[1])
		switch major := g.Package.NestMajor(); {
		case major >= 0 && major < 12:
			// 11.x forRoot takes one argument; the second is ignored.
		case !ok || major < 0:
			g.NameKnown = false
		case g.Options[0].Value("name") != nil && n != g.Name:
			g.NameKnown = false
		default:
			g.Name = n
		}
	}
}

// nameFromOptions reads the options' literal `name`.
func (d *discovery) nameFromOptions(g *Group) {
	v := g.Options[0].Value("name")
	if v == nil {
		return
	}
	if n, ok := stringLiteral(v); ok {
		g.Name = n
	} else {
		g.NameKnown = false
	}
}

// forRootAsync reads `TypeOrmModule.forRootAsync({useFactory, inject?,
// imports?, name?, dataSourceFactory?})` whose useFactory is an arrow or
// function expression whose every return is an options object TO-K1 can
// read (with TO-K5 for ConfigService parameters) and whose
// dataSourceFactory is absent or `(o) => new DataSource(o)`, optionally
// `.initialize()`d (`typeorm-core.module.js:160-165` at 11.0.0).
func (d *discovery) forRootAsync(g *Group, call *typescript.ModelExpr) {
	d.in.tag("TO-K3/`TypeOrmModule.forRootAsync(")
	if len(call.Args) != 1 || call.Args[0].Kind != typescript.ModelObject {
		g.OptionsWhy = "the forRootAsync argument is not an object literal"
		return
	}
	var factory, inject *typescript.ModelExpr
	for i := range call.Args[0].Entries {
		en := &call.Args[0].Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand && en.Kind != typescript.ModelMethod {
			g.OptionsWhy = fmt.Sprintf("a forRootAsync entry at line %d is not read", en.Span.StartLine)
			return
		}
		switch en.Key {
		case "useFactory":
			if en.Kind == typescript.ModelMethod {
				g.OptionsWhy = "useFactory is a method"
				return
			}
			factory = &en.Value
		case "inject":
			inject = &en.Value
		case "imports":
		case "name":
			if n, ok := stringLiteral(&en.Value); ok {
				g.Name = n
			} else {
				g.NameKnown = false
			}
		case "dataSourceFactory":
			if why := d.defaultFactory(g.File, &en.Value); why != "" {
				d.in.tag("D")
				g.OptionsWhy = "the dataSourceFactory is not `(o) => new DataSource(o)`: " + why + " (row D)"
				return
			}
			d.in.tag("TO-K3/`dataSourceFactory`")
		case "useClass", "useExisting":
			d.in.tag("D")
			g.OptionsWhy = "forRootAsync with " + en.Key + ": the options come from createTypeOrmOptions() (row D, open point O9)"
			return
		default:
			g.OptionsWhy = "the forRootAsync key " + en.Key + " is not read"
			return
		}
	}
	if factory == nil {
		g.OptionsWhy = "forRootAsync without useFactory"
		return
	}
	if factory.Kind != typescript.ModelArrow && factory.Kind != typescript.ModelFunction || factory.Async {
		g.OptionsWhy = "useFactory is not a non-async arrow or function expression (an async factory is not read)"
		return
	}
	ctx := optionContext{config: map[string]bool{}, factory: true, scope: functionScope(factory)}
	for i, param := range factory.Params {
		if param == "" || inject == nil || inject.Kind != typescript.ModelArray || i >= len(inject.Elements) {
			continue
		}
		if d.isExternal(g.File, &inject.Elements[i], pkgNestConfig, "ConfigService") {
			ctx.config[param] = true
		}
	}
	if factory.Kind == typescript.ModelArrow && !factory.Block {
		g.Options, g.OptionsWhy = d.candidates(g.File, factory.Body, false, ctx)
	} else {
		var out []*Options
		if why := d.returns(g.File, factory.Statements, &out, ctx); why != "" {
			g.OptionsWhy = "useFactory: " + why
			return
		}
		if len(out) == 0 {
			g.OptionsWhy = "useFactory returns no options object"
			return
		}
		g.Options = out
	}
	if g.OptionsWhy == "" && g.NameKnown && g.Name == "default" {
		d.nameFromOptions(g)
	}
}

// defaultFactory checks a dataSourceFactory: an arrow with one parameter,
// async or not, whose body is `new DataSource(<param>)` or `new
// DataSource(<param>).initialize()`, or (T018g6 Revision 3 addendum 2,
// nest-admin) an async block whose statements are `const x = await <that>;
// return x` or `return <that>` (`await` optional before `<that>`).
func (d *discovery) defaultFactory(file string, e *typescript.ModelExpr) string {
	if e.Kind != typescript.ModelArrow || len(e.Params) != 1 || e.Params[0] == "" {
		return "it is not a one-parameter arrow"
	}
	param := e.Params[0]
	if !e.Block {
		if e.Body == nil || !d.newDataSource(file, e.Body, param, e.Async) {
			return "its body is not new DataSource(" + param + ")"
		}
		return ""
	}
	list := e.Statements
	switch {
	case len(list) == 1 && list[0].Kind == typescript.StatementReturn && list[0].Value != nil:
		if d.newDataSource(file, list[0].Value, param, e.Async) {
			return ""
		}
	case len(list) == 2 && list[0].Kind == typescript.StatementDeclaration && list[0].Keyword == "const" && list[0].Value != nil &&
		list[1].Kind == typescript.StatementReturn && list[1].Value != nil && list[1].Value.Kind == typescript.ModelIdentifier && list[1].Value.Name == list[0].Name:
		if d.newDataSource(file, list[0].Value, param, e.Async) {
			return ""
		}
	}
	return "its block is not `const x = await new DataSource(" + param + ").initialize(); return x` or `return new DataSource(" + param + ")`"
}

// newDataSource reports `new DataSource(param)`, optionally
// `.initialize()`d, optionally awaited when the factory is async.
func (d *discovery) newDataSource(file string, body *typescript.ModelExpr, param string, async bool) bool {
	if async && body.Kind == typescript.ModelUnary && body.Value == "await" {
		body = body.Object
	}
	if body.Kind == typescript.ModelCall && body.Callee.Kind == typescript.ModelMember && body.Callee.Name == "initialize" && len(body.Args) == 0 {
		body = body.Callee.Object
	}
	return d.isDataSource(file, body) && len(body.Args) == 1 && body.Args[0].Kind == typescript.ModelIdentifier && body.Args[0].Name == param
}

// registration is one `registerAs(<token>, <factory>)` of the package.
type registration struct {
	file string
	line int
	opts *Options
	why  string
}

// configResult is what a `configService.get(<token>)` reads to.
type configResult struct {
	opts *Options
	why  string
}

// configGet reads `<param>.get('<token>')` where param receives
// @nestjs/config's ConfigService (TO-K5): the options of the package's
// single registerAs of that token. ok is false when e is not such a call.
func (d *discovery) configGet(file string, e *typescript.ModelExpr, ctx optionContext) (configResult, bool) {
	if e.Kind != typescript.ModelCall || e.Callee.Kind != typescript.ModelMember || e.Callee.Name != "get" ||
		e.Callee.Object.Kind != typescript.ModelIdentifier || !ctx.config[e.Callee.Object.Name] {
		return configResult{}, false
	}
	if len(e.Args) != 1 {
		return configResult{why: "configService.get is called with other than one argument"}, true
	}
	arg := ctx.scope.substitute(&e.Args[0])
	token, ok := d.token(file, arg)
	if !ok {
		return configResult{why: "the configService.get token is not a string literal"}, true
	}
	regs := d.registrations(d.owner(file))[token]
	switch {
	case len(regs) == 0:
		return configResult{why: "no registerAs(" + token + ", ...) is found in the package"}, true
	case len(regs) > 1:
		return configResult{why: fmt.Sprintf("%d registerAs calls register %q", len(regs), token)}, true
	case regs[0].why != "":
		return configResult{why: fmt.Sprintf("registerAs(%q) at %s:%d: %s", token, regs[0].file, regs[0].line, regs[0].why)}, true
	}
	return configResult{opts: regs[0].opts}, true
}

// token reads a registerAs or configService.get token: a string literal
// or a module-level const bound to one.
func (d *discovery) token(file string, e *typescript.ModelExpr) (string, bool) {
	if s, ok := stringLiteral(e); ok {
		return s, true
	}
	if e.Kind != typescript.ModelIdentifier {
		return "", false
	}
	v, why := d.constValue(d.local(file, e.Name, 0))
	if why != "" {
		return "", false
	}
	return stringLiteral(v)
}

// registrations returns the registerAs calls of the package directory's
// files by token: module-level declarations, statements and default
// exports whose value is `registerAs(<token>, <factory>)` with registerAs
// from @nestjs/config and a factory that is one `return` of options TO-K1
// can read.
func (d *discovery) registrations(dir string) map[string][]registration {
	if regs, ok := d.registered[dir]; ok {
		return regs
	}
	regs := map[string][]registration{}
	d.registered[dir] = regs
	for _, f := range d.files {
		m := d.in.Models[f]
		if m == nil || underNodeModules(f) || d.owner(f) != dir {
			continue
		}
		var values []*typescript.ModelExpr
		for i := range m.Declarations {
			values = append(values, &m.Declarations[i].Value)
		}
		for i := range m.Statements {
			if s := &m.Statements[i]; s.Kind == typescript.StatementExpression {
				values = append(values, s.Value)
			}
		}
		if m.Default != nil {
			values = append(values, &m.Default.Value)
		}
		for _, v := range values {
			if v.Kind != typescript.ModelCall || len(v.Args) != 2 || !d.isExternal(f, v.Callee, pkgNestConfig, "registerAs") {
				continue
			}
			token, ok := d.token(f, &v.Args[0])
			if !ok {
				continue
			}
			r := registration{file: f, line: v.Span.StartLine}
			fn := &v.Args[1]
			var ret *typescript.ModelExpr
			switch {
			case fn.Async:
				r.why = "the factory is async"
			case fn.Kind == typescript.ModelArrow && !fn.Block:
				ret = fn.Body
			case (fn.Kind == typescript.ModelArrow || fn.Kind == typescript.ModelFunction) && len(fn.Statements) == 1 &&
				fn.Statements[0].Kind == typescript.StatementReturn && fn.Statements[0].Value != nil:
				ret = fn.Statements[0].Value
			default:
				r.why = "the factory is not one return of an options object"
			}
			if ret != nil {
				c, why := d.candidates(f, ret, false, optionContext{scope: functionScope(fn)})
				switch {
				case why != "":
					r.why = why
				case len(c) != 1:
					r.why = "the factory returns several options objects"
				default:
					r.opts = c[0]
				}
			}
			regs[token] = append(regs[token], r)
		}
	}
	return regs
}

// roots names the group roots: the file, or "<file>#<name>" when the file
// holds several groups.
func (d *discovery) roots(p *Package) {
	perFile := map[string]int{}
	for _, g := range p.Groups {
		perFile[g.File]++
	}
	for _, g := range p.Groups {
		if perFile[g.File] < 2 || g.Form == "ormconfig" {
			continue
		}
		name := g.Binding
		if name == "" {
			name = g.Name
		}
		g.Root = g.File + "#" + name
	}
}

// importReach is the set of repository files a root module file loads
// through static imports (Revision 4 A4): the file itself and, recursively,
// every file an import declaration or an `export ... from` clause of a
// reached file resolves to (the shared module resolver; a type-only export
// clause loads nothing). Package imports are not followed. open lists why
// the graph cannot be followed to its end: a relative specifier that
// resolves to no file of the repository, or a `require` call (a dynamic
// `import()` is not visible to the link-time facts: TO-V5).
type importReach struct {
	files map[string]bool
	open  []string
}

// reachFrom computes (and caches) the import reach of file.
func (d *discovery) reachFrom(file string) *importReach {
	if r, ok := d.reaches[file]; ok {
		return r
	}
	r := &importReach{files: map[string]bool{file: true}}
	queue := []string{file}
	follow := func(from, spec string, line int) {
		res := d.in.Resolver.Resolve(from, spec)
		switch {
		case res.Package():
		case res.File == "" && strings.HasPrefix(spec, "."):
			r.open = append(r.open, fmt.Sprintf("%s:%d: the import of %s resolves to no file", from, line, spec))
		case res.File == "":
			// A bare specifier the resolver does not find: a package.
		case !r.files[res.File]:
			r.files[res.File] = true
			queue = append(queue, res.File)
		}
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		facts := d.in.TypeScript[f]
		for _, imp := range facts.Imports {
			follow(f, imp.Module, imp.Span.StartLine)
		}
		for _, ex := range facts.ExportClauses {
			if ex.Module != "" && !ex.TypeOnly {
				follow(f, ex.Module, ex.Span.StartLine)
			}
		}
		for _, call := range facts.Calls {
			if call.Name == "require" && !call.Shadowed {
				r.open = append(r.open, fmt.Sprintf("%s:%d: a require call loads a module the static imports do not show", f, call.Span.StartLine))
			}
		}
	}
	if d.reaches == nil {
		d.reaches = map[string]*importReach{}
	}
	d.reaches[file] = r
	return r
}

// appReach is the import reach of the application a root module file
// belongs to: the root file's own reach joined with the reach of every
// entry file of the package (a non-test file no other package file
// imports, such as main.ts) whose reach holds the root file. A NestJS
// application loads every module its entry imports, so a `forFeature` in
// an AppModule's feature module registers for a forRoot that a separate
// DatabaseModule holds (nest-admin `src/shared/database/database.module.ts`,
// imported by `src/app.module.ts` next to the feature modules); two
// applications of one package keep their own reach.
func (d *discovery) appReach(p *Package, root string) *importReach {
	if r, ok := d.apps[root]; ok {
		return r
	}
	if d.entries == nil {
		d.entries = map[*Package][]string{}
	}
	entries, ok := d.entries[p]
	if !ok {
		imported := map[string]bool{}
		files := d.packageFiles(p)
		for _, f := range files {
			facts := d.in.TypeScript[f]
			mark := func(spec string) {
				if res := d.in.Resolver.Resolve(f, spec); res.File != "" && res.File != f {
					imported[res.File] = true
				}
			}
			for _, imp := range facts.Imports {
				mark(imp.Module)
			}
			for _, ex := range facts.ExportClauses {
				if ex.Module != "" && !ex.TypeOnly {
					mark(ex.Module)
				}
			}
		}
		for _, f := range files {
			if !imported[f] && !d.isTest(f) {
				entries = append(entries, f)
			}
		}
		d.entries[p] = entries
	}
	own := d.reachFrom(root)
	out := &importReach{files: map[string]bool{}, open: append([]string(nil), own.open...)}
	for f := range own.files {
		out.files[f] = true
	}
	for _, e := range entries {
		r := d.reachFrom(e)
		if !r.files[root] || e == root {
			continue
		}
		for f := range r.files {
			out.files[f] = true
		}
		out.open = append(out.open, r.open...)
	}
	if d.apps == nil {
		d.apps = map[string]*importReach{}
	}
	d.apps[root] = out
	return out
}
