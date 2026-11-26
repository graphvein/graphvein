package typeorm

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads data sources and their options (rows TO-K1, TO-K6,
// TO-K7, TO-K8): the module-level `new DataSource(<options>)`, the
// candidate rule (an object literal, a const bound to one, a selector
// function of the file returning them, conditionals of them), options
// imported from another module (TO-K7), scope-neutral spreads (TO-K8),
// ormconfig files, the dialect set and the naming strategy.

// Options is one candidate options object, read.
type Options struct {
	// File is the file the object literal is written in; Span its span.
	File string
	Span contract.Span
	// From says how the group reaches it: "literal", "const", "imported"
	// (TO-K7), "registerAs" (TO-K5) or "ormconfig".
	From string
	// Config says a registerAs namespace supplied some of the keys
	// (TO-K5, assumption TO-V8).
	Config bool
	// keys are the last value written for each key, in the file the value
	// is written in; order lists the keys in first-write order.
	keys  map[string]optionValue
	order []string
}

// optionValue is an option's value expression and the file it is written
// in (an identifier resolves there; `__dirname` is that file's directory).
type optionValue struct {
	Expr *typescript.ModelExpr
	File string
}

// Value returns the option key's value, nil when absent.
func (o *Options) Value(key string) *typescript.ModelExpr {
	if v, ok := o.keys[key]; ok {
		return v.Expr
	}
	return nil
}

func (o *Options) set(key string, v optionValue) {
	if _, ok := o.keys[key]; !ok {
		o.order = append(o.order, key)
	}
	o.keys[key] = v
}

// scopeKeys are the options that decide the group's tables and names
// (TO-K8: a spread is read only when none of these can come from it).
var scopeKeys = map[string]bool{
	"type": true, "entities": true, "entityPrefix": true, "schema": true,
	"namingStrategy": true, "name": true, "autoLoadEntities": true,
}

// optionContext carries what an options reader may resolve besides
// module-level bindings: the parameter names of a NestJS useFactory that
// receive @nestjs/config's ConfigService (TO-K5), and whether the
// statements read are a forRootAsync useFactory's body (TO-K3, whose try
// statements are read; T018g6 Revision 3 addendum 2).
type optionContext struct {
	config  map[string]bool
	factory bool
	// scope is the reading function's parameters and locals (scope.go):
	// they shadow the file's module-level names.
	scope localScope
}

// dataSources finds the package's module-level `const x = new
// DataSource(<options>)` declarations and `export default new
// DataSource(<options>)` statements (TO-K1).
func (d *discovery) dataSources(p *Package) {
	for _, f := range d.packageFiles(p) {
		m := d.in.Models[f]
		if m == nil {
			continue
		}
		for i := range m.Declarations {
			decl := &m.Declarations[i]
			if !d.isDataSource(f, &decl.Value) {
				continue
			}
			if decl.Keyword != "const" {
				d.diag(f, codeUnread, fmt.Sprintf("%s:%d: the TypeORM data source %s is declared with %s, so it may be reassigned; it is not read", f, decl.Span.StartLine, decl.Name, decl.Keyword))
				continue
			}
			d.dataSource(p, f, decl.Name, decl.Span, &decl.Value)
		}
		if m.Default != nil && d.isDataSource(f, &m.Default.Value) {
			d.dataSource(p, f, "", m.Default.Span, &m.Default.Value)
		}
	}
}

// isDataSource reports `new DataSource(...)` with DataSource from typeorm.
func (d *discovery) isDataSource(file string, e *typescript.ModelExpr) bool {
	return e.Kind == typescript.ModelNew && d.isExternal(file, e.Callee, pkgTypeORM, "DataSource")
}

func (d *discovery) dataSource(p *Package, file, name string, span contract.Span, e *typescript.ModelExpr) {
	g := &Group{File: file, Root: file, Span: span, Package: p, Form: "datasource", Binding: name, Name: "default", NameKnown: true, Test: d.isTest(file)}
	d.in.tag("TO-K1/`new DataSource(<options>)`")
	p.Groups = append(p.Groups, g)
	if len(e.Args) != 1 {
		g.OptionsWhy = "new DataSource takes one options argument"
		return
	}
	g.Options, g.OptionsWhy = d.candidates(file, &e.Args[0], true, optionContext{})
	g.Selector = d.selector(file, &e.Args[0])
	if g.OptionsWhy == "" && g.Options[0].From == "imported" {
		d.in.tag("TO-K7/`new DataSource(X)`")
	}
}

// selector returns "<file>:<line>" of the selector function an options
// call names, "" for any other options expression.
func (d *discovery) selector(file string, e *typescript.ModelExpr) string {
	if e.Kind != typescript.ModelCall || e.Callee.Kind != typescript.ModelIdentifier {
		return ""
	}
	if b := d.local(file, e.Callee.Name, 0); b.Func != nil && !b.Imported {
		return b.File + ":" + itoa(b.Func.Span.StartLine)
	}
	return ""
}

// candidates reads an options expression by the candidate rule: an
// object literal, an identifier bound to a module-level const object
// literal (of the file, or imported from another module of the package:
// TO-K7), a conditional of candidates, or (when call is true) a call to a
// module-level non-async function of the file whose every return is a
// candidate inside if/else chains.
func (d *discovery) candidates(file string, e *typescript.ModelExpr, call bool, ctx optionContext) ([]*Options, string) {
	switch e.Kind {
	case typescript.ModelObject:
		o, why := d.object(file, e, "literal", ctx)
		if why != "" {
			return nil, why
		}
		return []*Options{o}, ""
	case typescript.ModelIdentifier:
		if _, ok := ctx.scope.shadows(e.Name); ok {
			return nil, "the options " + e.Name + " are a parameter or local of the function that returns them (not read)"
		}
		b := d.local(file, e.Name, 0)
		v, why := d.constValue(b)
		if why != "" {
			return nil, "the options " + e.Name + ": " + why
		}
		if v.Kind != typescript.ModelObject {
			return nil, "the options " + e.Name + " are not bound to an object literal"
		}
		from := "const"
		if b.Imported {
			if d.owner(b.File) != d.owner(file) {
				return nil, "the options " + e.Name + " are imported from another package"
			}
			from = "imported"
			d.in.tag("TO-K7")
		}
		o, why := d.object(b.File, v, from, ctx)
		if why != "" {
			return nil, why
		}
		return []*Options{o}, ""
	case typescript.ModelCond:
		a, why := d.candidates(file, e.Then, false, ctx)
		if why != "" {
			return nil, why
		}
		b, why := d.candidates(file, e.Else, false, ctx)
		if why != "" {
			return nil, why
		}
		d.in.tag("TO-K1/candidate rule")
		return append(a, b...), ""
	case typescript.ModelCall:
		if reg, ok := d.configGet(file, e, ctx); ok {
			if reg.why != "" {
				return nil, reg.why
			}
			d.in.tag("TO-K5")
			return []*Options{reg.opts}, ""
		}
		if !call || e.Callee.Kind != typescript.ModelIdentifier || len(e.Args) != 0 {
			break
		}
		b := d.local(file, e.Callee.Name, 0)
		if b.Func == nil || b.Imported || b.Func.Value == nil || b.Func.Value.Kind != typescript.ModelFunction {
			return nil, "the options call " + e.Callee.Name + "() is not a module-level non-async function of " + file
		}
		var out []*Options
		fctx := ctx
		fctx.scope = functionScope(b.Func.Value)
		if why := d.returns(file, b.Func.Value.Statements, &out, fctx); why != "" {
			return nil, "the options function " + e.Callee.Name + ": " + why
		}
		if len(out) == 0 {
			return nil, "the options function " + e.Callee.Name + " returns no candidate"
		}
		d.in.tag("TO-K1/candidate rule")
		return out, ""
	}
	return nil, "the options are not an object literal, a const object literal, a selector function or a conditional of them (TO-K1)"
}

// returns reads a selector function's statements: every return is a
// candidate, if statements are followed into both branches, and any
// statement that could hide a return is not read.
func (d *discovery) returns(file string, list []typescript.ModelStatement, out *[]*Options, ctx optionContext) string {
	for i := range list {
		s := &list[i]
		switch s.Kind {
		case typescript.StatementReturn:
			if s.Value == nil {
				return fmt.Sprintf("a bare return at line %d", s.Span.StartLine)
			}
			c, why := d.candidates(file, s.Value, false, ctx)
			if why != "" {
				return why
			}
			*out = append(*out, c...)
		case typescript.StatementIf:
			if why := d.returns(file, s.Body, out, ctx); why != "" {
				return why
			}
			if why := d.returns(file, s.Else, out, ctx); why != "" {
				return why
			}
		case typescript.StatementTry:
			if !ctx.factory {
				return fmt.Sprintf("a statement at line %d is not read (it could return other options)", s.Span.StartLine)
			}
			// T018g6 Revision 3 addendum 2: a try statement is read only
			// when no path through it returns, so every path reaches the
			// same later return.
			if line := returnIn(s.Body, s.Catch, s.Finally); line != 0 {
				return fmt.Sprintf("a return inside the try statement at line %d (line %d) is not read", s.Span.StartLine, line)
			}
			d.in.tag("TO-K3/`try`")
		case typescript.StatementDeclaration, typescript.StatementExpression:
		default:
			return fmt.Sprintf("a statement at line %d is not read (it could return other options)", s.Span.StartLine)
		}
	}
	return ""
}

// returnIn returns the line of the first return statement in the lists,
// nested statements included, or 0 when none returns. Nested functions are
// expressions, so their returns are not statements of these lists.
func returnIn(lists ...[]typescript.ModelStatement) int {
	for _, list := range lists {
		for i := range list {
			s := &list[i]
			switch s.Kind {
			case typescript.StatementReturn:
				return s.Span.StartLine
			case typescript.StatementUnknown:
				// An unread statement (a loop, a switch, a block) could
				// return.
				return s.Span.StartLine
			}
			if line := returnIn(s.Body, s.Else, s.Catch, s.Finally); line != 0 {
				return line
			}
		}
	}
	return 0
}

// object reads an options object literal: plain and shorthand entries,
// methods by key, spreads read through a registerAs namespace (TO-K5) or
// as scope-neutral (TO-K8); any other entry leaves the options unread.
func (d *discovery) object(file string, e *typescript.ModelExpr, from string, ctx optionContext) (*Options, string) {
	o := &Options{File: file, Span: e.Span, From: from, keys: map[string]optionValue{}}
	for i := range e.Entries {
		en := &e.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand:
			// A value naming a parameter or local of the reading function
			// is read from that function (OP2), never from the module.
			o.set(en.Key, optionValue{Expr: ctx.scope.substitute(&en.Value), File: file})
		case typescript.ModelMethod:
			if scopeKeys[en.Key] {
				return nil, fmt.Sprintf("the scope option %s is a method at %s:%d", en.Key, file, en.Span.StartLine)
			}
		case typescript.ModelSpreadIn:
			if reg, ok := d.configGet(file, &en.Value, ctx); ok {
				if reg.why != "" {
					return nil, reg.why
				}
				for _, k := range reg.opts.order {
					o.set(k, reg.opts.keys[k])
				}
				o.Config = true
				d.in.tag("TO-K5")
				continue
			}
			if why := d.neutralSpread(file, &en.Value, ctx); why != "" {
				return nil, fmt.Sprintf("the spread at %s:%d: %s", file, en.Span.StartLine, why)
			}
			d.in.tag("TO-K8")
		default:
			return nil, fmt.Sprintf("an options entry at %s:%d is not read (a computed key or an accessor)", file, en.Span.StartLine)
		}
	}
	return o, ""
}

// neutralSpread applies TO-K8 to a spread argument: an identifier bound
// to a module-level const whose initializer is an object literal, a
// conditional of object literals or an object literal with &&-guarded
// spreads of object literals, recursively, none with a scope key or a
// computed key. It returns why the spread is not read, "" when it adds no
// scope option.
func (d *discovery) neutralSpread(file string, e *typescript.ModelExpr, ctx optionContext) string {
	if e.Kind != typescript.ModelIdentifier {
		return "the spread argument is not an identifier bound to a module-level const"
	}
	if _, ok := ctx.scope.shadows(e.Name); ok {
		return e.Name + " is a parameter or local of the function that returns the options"
	}
	b := d.local(file, e.Name, 0)
	v, why := d.constValue(b)
	if why != "" {
		return e.Name + ": " + why
	}
	return d.neutral(b.File, v, 0)
}

func (d *discovery) neutral(file string, e *typescript.ModelExpr, depth int) string {
	if depth > maxDepth {
		return "the spread nests too deeply"
	}
	switch e.Kind {
	case typescript.ModelObject:
		for i := range e.Entries {
			en := &e.Entries[i]
			switch en.Kind {
			case typescript.ModelProperty, typescript.ModelShorthand, typescript.ModelMethod:
				if scopeKeys[en.Key] {
					return fmt.Sprintf("it can supply the scope option %s (%s:%d)", en.Key, file, en.Span.StartLine)
				}
			case typescript.ModelSpreadIn:
				v := &en.Value
				if v.Kind == typescript.ModelBinary && v.Value == "&&" {
					v = v.Right
				}
				if v.Kind != typescript.ModelObject && v.Kind != typescript.ModelCond {
					return fmt.Sprintf("a nested spread at %s:%d is not an object literal or an &&-guarded one", file, en.Span.StartLine)
				}
				if why := d.neutral(file, v, depth+1); why != "" {
					return why
				}
			default:
				return fmt.Sprintf("an entry at %s:%d is not read (a computed key or an accessor)", file, en.Span.StartLine)
			}
		}
		return ""
	case typescript.ModelCond:
		if why := d.neutral(file, e.Then, depth+1); why != "" {
			return why
		}
		return d.neutral(file, e.Else, depth+1)
	}
	return "its value is not an object literal or a conditional of object literals"
}

// ormconfig reads ormconfig.json, ormconfig.ts and ormconfig.js at the
// package root (TypeORM 0.3 reads them only for createConnection and
// getConnectionOptions, so they are read here only when the package has
// no data source); 1.x reads them nowhere (row D), and the .yml, .yaml,
// .xml and .env forms are D. An array of options gives one group per
// element with a literal name.
func (d *discovery) ormconfig(p *Package) {
	for _, ext := range []string{".yml", ".yaml", ".xml", ".env"} {
		f := path.Join(p.Dir, "ormconfig"+ext)
		if _, ok := d.in.Sources[f]; ok {
			d.in.tag("D")
			d.diag(f, codeUnread, f+": an ormconfig"+ext+" file is not read (row D)")
		}
	}
	for _, name := range []string{"ormconfig.json", "ormconfig.ts", "ormconfig.js"} {
		f := path.Join(p.Dir, name)
		if _, ok := d.in.Sources[f]; !ok {
			continue
		}
		if p.Line == Line1 {
			d.in.tag("D")
			d.diag(f, codeUnread, f+": TypeORM 1.x does not read ormconfig files; it is not read (row D)")
			continue
		}
		d.in.tag("TO-K1/ormconfig")
		var objects []*Options
		var array bool
		var why string
		if name == "ormconfig.json" {
			objects, array, why = d.ormconfigJSON(f)
		} else {
			objects, array, why = d.ormconfigScript(f)
		}
		if why != "" {
			p.Groups = append(p.Groups, &Group{File: f, Root: f, Span: contract.Span{StartLine: 1}, Package: p, Form: "ormconfig", Name: "default", NameKnown: true, OptionsWhy: why, Test: d.isTest(f)})
			return
		}
		for _, o := range objects {
			g := &Group{File: f, Root: f, Span: o.Span, Package: p, Form: "ormconfig", Name: "default", NameKnown: true, Options: []*Options{o}, Test: d.isTest(f)}
			if g.Span.StartLine == 0 {
				g.Span.StartLine = 1
			}
			if array {
				if n, ok := stringLiteral(o.Value("name")); ok {
					g.Name, g.Root = n, f+"#"+n
				} else {
					g.OptionsWhy = "an ormconfig array element has no literal name"
				}
			}
			p.Groups = append(p.Groups, g)
		}
		return
	}
}

// ormconfigScript reads `export default <object literal or array>` or
// `module.exports = <...>` of an ormconfig.ts or .js.
func (d *discovery) ormconfigScript(f string) ([]*Options, bool, string) {
	m := d.in.Models[f]
	if m == nil {
		return nil, false, "the file has no model payload"
	}
	var value *typescript.ModelExpr
	if m.Default != nil {
		value = &m.Default.Value
	}
	for i := range m.Statements {
		s := &m.Statements[i]
		if s.Kind == typescript.StatementExpression && s.Value.Kind == typescript.ModelAssign && memberPath(s.Value.Left) == "module.exports" {
			value = s.Value.Right
		}
	}
	if value == nil {
		return nil, false, "no `export default` or `module.exports` options"
	}
	list := []*typescript.ModelExpr{value}
	array := value.Kind == typescript.ModelArray
	if array {
		list = nil
		for i := range value.Elements {
			list = append(list, &value.Elements[i])
		}
	}
	var out []*Options
	for _, e := range list {
		if e.Kind != typescript.ModelObject {
			return nil, false, "the ormconfig options are not object literals"
		}
		o, why := d.object(f, e, "ormconfig", optionContext{})
		if why != "" {
			return nil, false, why
		}
		out = append(out, o)
	}
	return out, array, ""
}

// ormconfigJSON reads ormconfig.json: an object or an array of objects
// whose values become literal expressions.
func (d *discovery) ormconfigJSON(f string) ([]*Options, bool, string) {
	var raw any
	if err := json.Unmarshal(d.in.Sources[f], &raw); err != nil {
		return nil, false, "ormconfig.json does not parse: " + err.Error()
	}
	switch v := raw.(type) {
	case map[string]any:
		return []*Options{jsonOptions(f, v)}, false, ""
	case []any:
		var out []*Options
		for _, el := range v {
			m, ok := el.(map[string]any)
			if !ok {
				return nil, false, "an ormconfig.json element is not an object"
			}
			out = append(out, jsonOptions(f, m))
		}
		return out, true, ""
	}
	return nil, false, "ormconfig.json is not an object or an array"
}

func jsonOptions(f string, m map[string]any) *Options {
	o := &Options{File: f, From: "ormconfig", keys: map[string]optionValue{}}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := jsonExpr(m[k])
		o.set(k, optionValue{Expr: &e, File: f})
	}
	return o
}

func jsonExpr(v any) typescript.ModelExpr {
	switch x := v.(type) {
	case string:
		return typescript.ModelExpr{Kind: typescript.ModelString, Value: x}
	case bool:
		if x {
			return typescript.ModelExpr{Kind: typescript.ModelBoolean, Value: "true"}
		}
		return typescript.ModelExpr{Kind: typescript.ModelBoolean, Value: "false"}
	case nil:
		return typescript.ModelExpr{Kind: typescript.ModelNull}
	case float64:
		return typescript.ModelExpr{Kind: typescript.ModelNumber, Value: fmt.Sprint(x)}
	case []any:
		e := typescript.ModelExpr{Kind: typescript.ModelArray}
		for _, el := range x {
			e.Elements = append(e.Elements, jsonExpr(el))
		}
		return e
	}
	return typescript.ModelExpr{Kind: typescript.ModelUnknown}
}

// memberPath renders an identifier or a chain of non-computed members,
// "" for anything else.
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

// dialects maps TypeORM's `type` values to the dialect the claims use
// and the verification of that dialect's facts (dialect rule, Revision 1
// O8): postgres and sqlite-through-sqlite3-or-better-sqlite3 were run by
// the oracle; mysql, mariadb, mssql and the other SQLite-family drivers
// are mapped from the driver source.
var dialects = map[string][2]string{
	"postgres":       {"postgres", "oracle"},
	"sqlite":         {"sqlite", "oracle"},
	"better-sqlite3": {"sqlite", "oracle"},
	"sqljs":          {"sqlite", "mapped"},
	"capacitor":      {"sqlite", "mapped"},
	"cordova":        {"sqlite", "mapped"},
	"expo":           {"sqlite", "mapped"},
	"nativescript":   {"sqlite", "mapped"},
	"react-native":   {"sqlite", "mapped"},
	"mysql":          {"mysql", "mapped"},
	"mariadb":        {"mariadb", "mapped"},
	"mssql":          {"mssql", "mapped"},
}

// dialect reads the candidates' literal `type` (TO-V4): one dialect, or
// the dialect set of a run-time selection (Revision 1 O1). A type outside
// the read set is D; 1.x's removed `sqlite` type fails (TO-P1 (7)).
func (d *discovery) dialect(g *Group) {
	g.Verification = map[string]string{}
	set := map[string]bool{}
	for _, o := range g.Options {
		v := o.Value("type")
		t, ok := stringLiteral(v)
		switch {
		case v == nil:
			g.OptionsWhy = "an options object has no type"
			return
		case !ok:
			g.OptionsWhy = fmt.Sprintf("the type option at %s:%d is not a string literal", o.File, v.Span.StartLine)
			return
		}
		g.Types = append(g.Types, t)
		m, ok := dialects[t]
		if !ok {
			g.Refused = "the dialect " + t + " is not read"
			continue
		}
		dialect, verification := m[0], m[1]
		if old, ok := g.Verification[dialect]; !ok || old == "oracle" {
			g.Verification[dialect] = verification
		}
		set[dialect] = true
		if t == "sqlite" && g.Package.Line == Line1 {
			if g.Failures == nil {
				g.Failures = map[string]string{}
			}
			g.Failures[dialect] = "TypeORM 1.x has no sqlite driver (type \"sqlite\": MissingDriverError from the DataSource constructor, driver/DriverFactory.js:72 at 1.1.3)"
			d.in.tag("TO-P1/(7)")
		}
	}
	for k := range set {
		g.Dialects = append(g.Dialects, k)
	}
	sort.Strings(g.Dialects)
	if len(g.Dialects) == 1 {
		g.Dialect = g.Dialects[0]
	} else if len(g.Dialects) > 1 {
		d.in.tag("TO-K1/the dialect set")
	}
}

// naming applies TO-K6: `namingStrategy: new SnakeNamingStrategy()` with
// the class imported by name from typeorm-naming-strategies 4.x and no
// arguments; any other value is D.
func (d *discovery) naming(g *Group) {
	g.Naming = "default"
	o := g.Options[0]
	v, ok := o.keys["namingStrategy"]
	if !ok {
		return
	}
	why := ""
	switch {
	case v.Expr.Kind != typescript.ModelNew || v.Expr.Callee.Kind != typescript.ModelIdentifier:
		why = "it is not `new SnakeNamingStrategy()`"
	case len(v.Expr.Args) != 0:
		why = "new SnakeNamingStrategy is called with arguments"
	case !d.importsNamed(v.File, v.Expr.Callee.Name, pkgNaming, "SnakeNamingStrategy"):
		why = v.Expr.Callee.Name + " is not SnakeNamingStrategy imported by name from " + pkgNaming
	case lockfile.Major(g.Package.Versions[pkgNaming]) != 4:
		why = pkgNaming + " is not proven 4.x in the lockfile (TO-V10)"
	}
	if why != "" {
		g.Refused = fmt.Sprintf("the namingStrategy at %s:%d: %s (a custom naming strategy)", v.File, v.Expr.Span.StartLine, why)
		return
	}
	g.Naming = "snake"
	d.in.tag("TO-K6")
}

// importsNamed reports that file imports local name directly from module
// as its export exported (not through a project re-export).
func (d *discovery) importsNamed(file, local, module, exported string) bool {
	for _, imp := range d.in.TypeScript[file].Imports {
		if imp.Module == module && imp.Bindings[local] == exported {
			return true
		}
	}
	return false
}

// scope checks that the scope options are equal across the candidates
// (TO-K1) and reads entityPrefix and schema: string literals, else the
// names they decide are unknown (the group's options are then not read).
func (d *discovery) scope(g *Group) {
	for _, key := range []string{"entities", "entityPrefix", "schema", "namingStrategy", "name", "autoLoadEntities"} {
		first := d.canon(g.Options[0], key)
		for _, o := range g.Options[1:] {
			if d.canon(o, key) != first {
				g.OptionsWhy = "the scope option " + key + " differs between the candidate options objects"
				return
			}
		}
	}
	o := g.Options[0]
	for _, key := range []string{"entityPrefix", "schema"} {
		v := o.Value(key)
		if v == nil || isUndefined(v) {
			continue
		}
		s, ok := stringLiteral(v)
		if !ok {
			g.OptionsWhy = fmt.Sprintf("the %s option at %s:%d is not a string literal", key, o.File, v.Span.StartLine)
			return
		}
		if key == "entityPrefix" {
			g.EntityPrefix = s
		} else {
			g.Schema = s
		}
	}
	if v := o.Value("autoLoadEntities"); v != nil {
		b, ok := boolLiteral(v)
		if !ok {
			g.Incomplete = append(g.Incomplete, fmt.Sprintf("%s:%d: autoLoadEntities is not a boolean literal", o.File, v.Span.StartLine))
		}
		g.AutoLoad = b
	}
	for _, c := range g.Options {
		g.Synchronize = append(g.Synchronize, literalFlag(c.Value("synchronize")))
		g.MigrationsRun = append(g.MigrationsRun, literalFlag(c.Value("migrationsRun")))
	}
}

func literalFlag(e *typescript.ModelExpr) string {
	if e == nil {
		return ""
	}
	if b, ok := boolLiteral(e); ok {
		if b {
			return "true"
		}
		return "false"
	}
	return "non-literal"
}

func isUndefined(e *typescript.ModelExpr) bool {
	return e != nil && e.Kind == typescript.ModelIdentifier && e.Name == "undefined"
}

// canon renders an option's value for the equality check: identifiers by
// the declaration they resolve to, literals by value, spans ignored.
func (d *discovery) canon(o *Options, key string) string {
	v, ok := o.keys[key]
	if !ok {
		return "<absent>"
	}
	return d.render(v.File, v.Expr, 0)
}

func (d *discovery) render(file string, e *typescript.ModelExpr, depth int) string {
	if e == nil || depth > maxDepth {
		return "?"
	}
	list := func(xs []typescript.ModelExpr) string {
		var parts []string
		for i := range xs {
			parts = append(parts, d.render(file, &xs[i], depth+1))
		}
		return strings.Join(parts, ",")
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		b := d.local(file, e.Name, 0)
		switch {
		case b.Decl != nil:
			return "decl:" + b.File + "#" + b.Decl.Name
		case b.Class != nil:
			return "class:" + b.File + "#" + b.Class.Name + "@" + itoa(b.Class.Span.StartLine)
		case b.Func != nil:
			return "func:" + b.File + "#" + b.Func.Name
		case b.External != "":
			return "ext:" + b.External + "#" + b.Name
		}
		return "id:" + file + "#" + e.Name
	case typescript.ModelString, typescript.ModelNumber, typescript.ModelBoolean:
		return e.Kind + ":" + e.Value
	case typescript.ModelNull:
		return "null"
	case typescript.ModelArray:
		return "[" + list(e.Elements) + "]"
	case typescript.ModelCall, typescript.ModelNew:
		return e.Kind + "(" + d.render(file, e.Callee, depth+1) + ")(" + list(e.Args) + ")"
	case typescript.ModelMember:
		return d.render(file, e.Object, depth+1) + "." + e.Name
	case typescript.ModelTemplate:
		return "`" + strings.Join(e.Strings, "${}") + "`(" + list(e.Substitutions) + ")"
	case typescript.ModelBinary:
		return "(" + d.render(file, e.Left, depth+1) + e.Value + d.render(file, e.Right, depth+1) + ")"
	case typescript.ModelObject:
		var parts []string
		for i := range e.Entries {
			en := &e.Entries[i]
			parts = append(parts, en.Kind+":"+en.Key+"="+d.render(file, &en.Value, depth+1))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	// Anything else is compared by position: equal only when it is the
	// same expression.
	return fmt.Sprintf("%s@%s:%d:%d", e.Kind, file, e.Span.StartLine, e.Span.StartColumn)
}
