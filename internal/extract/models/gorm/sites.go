package gorm

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
)

// MigrateSite is one AutoMigrate, Migrator().AutoMigrate or
// Migrator().CreateTable call of a read file (row GO-K3).
type MigrateSite struct {
	// File and Func locate the site ("Name" or "Recv.Name"); Statement is
	// the index of the function body's top-level statement holding it and
	// Nesting its nesting mark there.
	File, Func string
	Line       int
	Statement  int
	Nesting    string
	// Call is "AutoMigrate", "Migrator().AutoMigrate" or
	// "Migrator().CreateTable".
	Call string
	// Receiver is the proof that the chain starts at a *gorm.DB ("var",
	// "param", "gorm.Open", "module function", "package var", "field"), ""
	// when it is not proven; Chain the allowed methods between the start and
	// the call.
	Receiver string
	// ReceiverCall is the call a local receiver is defined from ("gorm.Open",
	// "common.GetDB"), for "gorm.Open" and "module function" proofs.
	ReceiverCall string
	Chain        []string
	// Reason says why the site is row D (an unproven receiver, another chain
	// method); its arguments are then not read.
	Reason string
	// Args are the site's arguments.
	Args []Arg
	// Sequence and Position place the site in Module.Sequences (its first
	// appearance); -1 when the order rule does not place it.
	Sequence, Position int
	fn                 *funcInfo
}

// Proven reports a site whose receiver is a proven *gorm.DB through the
// allowed chain.
func (s *MigrateSite) Proven() bool { return s.Reason == "" }

// Ordered reports a site the order rule places in statement order within
// its function (an unconditional top-level statement or an if header).
func (s *MigrateSite) Ordered() bool {
	return s.Nesting == facts.GoUnconditional || s.Nesting == facts.GoIfHeader
}

// Arg is one migrate-site argument: Form is "new(T)", "&T{...}" or
// "T{...}" with Model the struct, or "" with Reason saying why it is not
// read (row D).
type Arg struct {
	Form   string
	Model  *Model
	Reason string
}

// JoinSetup is one SetupJoinTable(model, field, joinModel) call (T018g5
// Revision 2, A2). Owner and Field name the many2many relation whose join
// table GORM then builds from Join; Reason says why they are not known
// (every join table of the group is then row D).
type JoinSetup struct {
	File        string
	Line        int
	Owner, Join *Model
	Field       string
	Reason      string
}

// SchemaChange is one Migrator DDL call (facts.GoMigratorDDL, Revision 2,
// A3): Method the Migrator method; Model or Table (a string table name)
// the table it changes, both empty when not proven (it may change every
// table of the group); Column the literal column or field argument of a
// column method.
type SchemaChange struct {
	File, Method string
	Line         int
	Model        *Model
	Table        string
	Column       string
	Reason       string
}

// columnMethods are the Migrator DDL methods whose second argument names
// a column or field; elementMethods change indexes or constraints, and
// tableMethods the table itself.
var (
	columnMethods  = map[string]bool{"AddColumn": true, "DropColumn": true, "AlterColumn": true, "MigrateColumn": true, "MigrateColumnUnique": true, "RenameColumn": true}
	elementMethods = map[string]bool{"CreateIndex": true, "DropIndex": true, "RenameIndex": true, "CreateConstraint": true, "DropConstraint": true, "MigrateColumnUnique": true}
	tableMethods   = map[string]bool{"DropTable": true, "RenameTable": true}
)

// binding is one import of a file.
type binding struct {
	Path string
	// Dir and Package are set for a package of the file's own module.
	Dir, Package string
}

// funcInfo is one captured function of a read file.
type funcInfo struct {
	file  string
	fn    *facts.GoFunc
	sites []*MigrateSite
	// calls are the top-level call statements that resolve to module
	// functions (key, statement index).
	calls []stmtCall
}

type stmtCall struct {
	key   string
	index int
}

type mutation struct{ name, at string }

// resolver reads the payload of one module's files.
type resolver struct {
	d *discovery
	m *Module
	// path is the file being read.
	path string
	// bindings caches each file's imports.
	bindings map[string]map[string]binding
	// funcs are the captured functions by key ("<file>#<Func>"), in file
	// order in keys.
	funcs     map[string]*funcInfo
	keys      []string
	mutations []mutation
	models    map[string]*Model
	// packages caches golang.Package by directory; read is the set of
	// m.Files, built once the files are listed.
	packages map[string]golang.GoPackage
	read     map[string]bool
}

// importsOf returns a file's import bindings by name (the package clause
// for a module-own package, as golang.Link binds it).
func (r *resolver) importsOf(file string) map[string]binding {
	if b, ok := r.bindings[file]; ok {
		return b
	}
	if r.bindings == nil {
		r.bindings = map[string]map[string]binding{}
	}
	out := map[string]binding{}
	base, module := golang.ModuleAt(file, r.d.modules)
	for _, imp := range r.d.in.Files[file].Imports {
		b := binding{Path: imp.Module}
		if dir, ok := golang.ModuleImportDir(base, module, imp.Module); ok {
			// The package name comes from the go command's reading of the
			// directory (golang.ImportPackage, as Link binds it): a file no
			// build configuration selects never names the package, and a
			// package go/build refuses binds nothing.
			name, _, refusal := golang.ImportPackage(file, dir, r.d.in.Files)
			if refusal != "" || name == "" {
				continue
			}
			b.Dir, b.Package = dir, name
		}
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias && b.Package != "" {
				alias = b.Package
			}
			if alias != "_" && alias != "." {
				out[alias] = b
			}
		}
	}
	r.bindings[file] = out
	return out
}

// importOf is the import path an identifier names in file ("" when it is
// not an import binding).
func (r *resolver) importOf(file string, e *facts.GoExpr) string {
	if e == nil || e.Kind != facts.GoIdentifier {
		return ""
	}
	return r.importsOf(file)[e.Name].Path
}

// isGormDB reports a type expression *gorm.DB in file.
func (r *resolver) isGormDB(file string, t *facts.GoExpr) bool {
	return t != nil && t.Kind == facts.GoPointer && t.Object != nil && t.Object.Kind == facts.GoSelector && t.Object.Name == "DB" && r.importOf(file, t.Object.Object) == pkgGorm
}

// goPackage is the package in directory dir as the Go structure contract
// decides it (golang.Package, cached per directory): the same file
// selection import edges and call bindings read.
func (r *resolver) goPackage(dir string) golang.GoPackage {
	if pkg, ok := r.packages[dir]; ok {
		return pkg
	}
	if r.packages == nil {
		r.packages = map[string]golang.GoPackage{}
	}
	pkg := golang.Package(dir, r.d.in.Files)
	r.packages[dir] = pkg
	return pkg
}

// packageFiles are the read files of package directory dir with package
// name pkg ("" for any): the non-test files golang.Package selects for the
// directory, within the module's read files. A package it refuses, or one
// of another name, has none, so nothing resolves into it.
func (r *resolver) packageFiles(dir, pkg string) []string {
	p := r.goPackage(dir)
	if p.Refusal != "" || pkg != "" && p.Name != pkg {
		return nil
	}
	if r.read == nil {
		r.read = map[string]bool{}
		for _, f := range r.m.Files {
			r.read[f] = true
		}
	}
	var out []string
	for _, f := range p.Files {
		if r.read[f] {
			out = append(out, f)
		}
	}
	return out
}

// scope returns the package directory and name a name resolves in: the
// file's own package for an identifier, the imported module package for
// pkg.Name; ok is false for anything else (external is the import path of
// a package outside the module).
func (r *resolver) scope(file string, e facts.GoExpr) (dir, pkg, name, external string, ok bool) {
	switch e.Kind {
	case facts.GoIdentifier:
		return path.Dir(file), r.d.in.Files[file].Package, e.Name, "", true
	case facts.GoSelector:
		if e.Object.Kind != facts.GoIdentifier {
			return "", "", "", "", false
		}
		b, bound := r.importsOf(file)[e.Object.Name]
		if !bound {
			return "", "", "", "", false
		}
		if b.Dir == "" {
			return "", "", "", b.Path, false
		}
		return b.Dir, b.Package, e.Name, "", true
	}
	return "", "", "", "", false
}

// structOf resolves a type name (T or pkg.T, a pointer of either) to a
// struct of the module.
func (r *resolver) structOf(file string, t facts.GoExpr) (string, *facts.GoStruct) {
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = *t.Object
	}
	dir, pkg, name, _, ok := r.scope(file, t)
	if !ok {
		return "", nil
	}
	for _, p := range r.packageFiles(dir, pkg) {
		g := r.d.in.Files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Structs {
			if g.Structs[i].Name == name {
				return p, &g.Structs[i]
			}
		}
	}
	return "", nil
}

func funcName(fn *facts.GoFunc) string {
	if fn.Receiver != nil {
		t := fn.Receiver.Type
		if t.Kind == facts.GoPointer && t.Object != nil {
			t = *t.Object
		}
		if t.Kind == facts.GoIndex && t.Object != nil {
			t = *t.Object
		}
		return t.Name + "." + fn.Name
	}
	return fn.Name
}

// migrateCalls are the migrate methods (the callee's selector name).
var migrateCalls = map[string]bool{"AutoMigrate": true, "CreateTable": true}

// file reads one read file's payload: its functions' sites, statements
// and mutations, or for a file outside gate G1 the migrate calls of its
// call cache (GB1 open point 2).
func (r *resolver) file(p string) {
	r.path = p
	f := r.d.in.Files[p]
	if r.funcs == nil {
		r.funcs, r.models = map[string]*funcInfo{}, map[string]*Model{}
	}
	if !f.Go.Gated {
		r.ungated(p, f)
		return
	}
	for i := range f.Go.Funcs {
		fn := &f.Go.Funcs[i]
		info := &funcInfo{file: p, fn: fn}
		key := p + "#" + funcName(fn)
		r.funcs[key] = info
		r.keys = append(r.keys, key)
		for _, mu := range fn.Mutations {
			r.mutations = append(r.mutations, mutation{name: mu.Target.Name, at: fmt.Sprintf("%s:%d", p, mu.Line)})
		}
		for _, s := range fn.Sites {
			r.site(info, s)
		}
	}
	// Package-level var initialisers hold no captured sites, but a
	// gorm.Open there still opens the module's database (its dialect and
	// configuration count), and a migrate call there runs at package
	// initialisation, outside the order rule.
	pkgLevel := &funcInfo{file: p, fn: &facts.GoFunc{}}
	for _, v := range f.Go.Vars {
		for _, e := range v.Values {
			r.nested(pkgLevel, facts.GoSite{Call: e, Line: v.Line, Nesting: "package"}, true)
		}
	}
}

// nested reads the gorm.Open and migrate calls inside a captured tree
// that the payload does not repeat as sites (a call inside another
// captured site's arguments or chain, a package-level initialiser): each
// is read as a site of the same statement. root says the tree's own call
// is not a site yet.
func (r *resolver) nested(info *funcInfo, s facts.GoSite, root bool) {
	var walk func(e *facts.GoExpr, top bool)
	walk = func(e *facts.GoExpr, top bool) {
		if e == nil {
			return
		}
		if e.Kind == facts.GoCall && (!top || root) && e.Callee != nil && e.Callee.Kind == facts.GoSelector && (migrateCalls[e.Callee.Name] || e.Callee.Name == "Open" && r.importOf(r.path, e.Callee.Object) == pkgGorm) {
			r.site(info, facts.GoSite{Call: *e, Line: s.Line, Statement: s.Statement, Nesting: s.Nesting})
			return
		}
		for _, x := range []*facts.GoExpr{e.Object, e.Callee, e.Type, e.Left, e.Right} {
			walk(x, false)
		}
		for i := range e.Args {
			walk(&e.Args[i], false)
		}
		for i := range e.Elements {
			walk(e.Elements[i].Key, false)
			walk(&e.Elements[i].Value, false)
		}
	}
	walk(&s.Call, true)
}

// ungated reads a file outside gate G1: it has no sites, so a call named
// AutoMigrate or CreateTable in its call cache that is not a module
// function call is a migrate call whose receiver the payload cannot prove
// (row D).
func (r *resolver) ungated(p string, f facts.Facts) {
	for _, c := range f.Calls {
		if !migrateCalls[c.StaticName] && c.StaticName != "SetupJoinTable" && !facts.GoMigratorDDL[c.StaticName] {
			continue
		}
		if base, _, ok := strings.Cut(c.Name, "."); ok && c.Name == base+"."+c.StaticName {
			if _, bound := r.importsOf(p)[base]; bound && !migrateCalls[c.StaticName] {
				continue
			}
		}
		switch {
		case c.StaticName == "SetupJoinTable":
			r.m.JoinSetups = append(r.m.JoinSetups, JoinSetup{File: p, Line: c.Span.StartLine, Reason: "a SetupJoinTable call in a file without a GORM import is not read"})
			r.changeMarker(p, c.Span.StartLine, "SetupJoinTable call in a file without a GORM import: every join table of the group is not claimed")
			continue
		case facts.GoMigratorDDL[c.StaticName]:
			r.m.Changes = append(r.m.Changes, SchemaChange{File: p, Line: c.Span.StartLine, Method: c.StaticName, Reason: "a call in a file without a GORM import"})
			r.changeMarker(p, c.Span.StartLine, c.StaticName+" call in a file without a GORM import: it may change every table of the group, whose column, index and constraint sets are partial")
			continue
		}
		if base, _, ok := strings.Cut(c.Name, "."); ok && c.Name == base+"."+c.StaticName {
			if b, bound := r.importsOf(p)[base]; bound {
				if b.Dir == "" {
					r.d.unread(r.m, p, c.Span.StartLine, fmt.Sprintf("call of %s.%s, a package outside the module, is not read", b.Path, c.StaticName))
				}
				continue
			}
		}
		r.d.unread(r.m, p, c.Span.StartLine, c.StaticName+" call in a file without a GORM import: its receiver is not proven to be a *gorm.DB")
	}
}

// site classifies one captured site of a gated file.
func (r *resolver) site(info *funcInfo, s facts.GoSite) {
	r.nested(info, s, false)
	callee := s.Call.Callee
	if callee == nil || callee.Kind != facts.GoSelector {
		return
	}
	obj := callee.Object
	local := obj.Kind == facts.GoIdentifier && declares(info.fn, obj.Name)
	bound := ""
	if obj.Kind == facts.GoIdentifier && !local {
		bound = r.importOf(r.path, obj)
	}
	switch {
	case bound == pkgGorm && callee.Name == "Open":
		r.openSite(info.fn, s)
		return
	case bound == pkgInflection:
		if strings.HasPrefix(callee.Name, "Add") {
			r.m.InflectionChanged = append(r.m.InflectionChanged, fmt.Sprintf("%s:%d", r.path, s.Line))
		}
		return
	case bound == "" && callee.Name == "SetupJoinTable":
		r.joinSetup(info.fn, s)
		return
	case bound == "" && facts.GoMigratorDDL[callee.Name]:
		r.schemaChange(info.fn, s, callee.Name)
		return
	case bound == "" && (callee.Name == "Exec" || callee.Name == "Raw"):
		r.rawSQL(s, callee.Name)
		return
	case !migrateCalls[callee.Name]:
		return
	case bound != "":
		// pkg.AutoMigrate(...): a package function, not a GORM migrate
		// call. A module function is read through the order rule; a
		// dependency's is not read.
		if b := r.importsOf(r.path)[obj.Name]; b.Dir == "" {
			r.d.unread(r.m, r.path, s.Line, fmt.Sprintf("call of %s.%s, a package outside the module, is not read", b.Path, callee.Name))
		}
		return
	}
	ms := &MigrateSite{File: r.path, Func: funcName(info.fn), Line: s.Line, Statement: s.Statement, Nesting: s.Nesting, Sequence: -1, Position: -1, fn: info}
	info.sites = append(info.sites, ms)
	r.m.Sites = append(r.m.Sites, ms)
	r.d.in.tag("GO-K3")
	start := *obj
	ms.Call = callee.Name
	if callee.Name == "CreateTable" {
		ms.Call = "Migrator().CreateTable"
	}
	if start.Kind == facts.GoCall && start.Callee != nil && start.Callee.Kind == facts.GoSelector && start.Callee.Name == "Migrator" && len(start.Args) == 0 {
		if callee.Name == "AutoMigrate" {
			ms.Call = "Migrator().AutoMigrate"
		}
		start = *start.Callee.Object
	} else if callee.Name == "CreateTable" {
		ms.Reason = "CreateTable is not called on Migrator()"
	}
	if ms.Reason == "" {
		ms.Chain, start, ms.Reason = r.chain(start)
	}
	if ms.Reason == "" {
		ms.Receiver, ms.ReceiverCall, ms.Reason = r.receiver(info.fn, start)
	}
	if ms.Reason != "" {
		r.d.unread(r.m, r.path, s.Line, ms.Call+" site not read: "+ms.Reason)
		return
	}
	r.d.in.tag("GO-K3/`" + ms.Call + "`")
	for _, a := range s.Call.Args {
		arg := r.argument(info.fn, a)
		ms.Args = append(ms.Args, arg)
		if arg.Model == nil {
			r.d.unread(r.m, r.path, s.Line, ms.Call+" argument not read: "+arg.Reason)
			continue
		}
		r.d.in.tag("GO-K3/`" + arg.Form + "`")
		if _, seen := r.models[arg.Model.Identity()]; !seen {
			r.models[arg.Model.Identity()] = arg.Model
			r.m.Models = append(r.m.Models, arg.Model)
			r.d.in.tag("GO-M1")
		}
	}
}

// declares reports a parameter, receiver or local of fn named name.
func declares(fn *facts.GoFunc, name string) bool {
	if fn.Receiver != nil && fn.Receiver.Name == name {
		return true
	}
	for _, p := range fn.Params {
		if p.Name == name {
			return true
		}
	}
	for _, l := range fn.Locals {
		if l.Kind == "assign" {
			continue
		}
		for _, n := range l.Names {
			if n == name {
				return true
			}
		}
	}
	return false
}

// chain strips the allowed chain methods (Debug(), WithContext(e),
// Set("gorm:table_options", e), Session(&gorm.Session{...}) without
// DryRun) from a receiver chain, outermost first in the result's reverse.
func (r *resolver) chain(e facts.GoExpr) ([]string, facts.GoExpr, string) {
	var chain []string
	for e.Kind == facts.GoCall && e.Callee != nil && e.Callee.Kind == facts.GoSelector {
		name, args := e.Callee.Name, e.Args
		ok := false
		switch name {
		case "Debug":
			ok = len(args) == 0
		case "WithContext":
			ok = len(args) == 1
		case "Set":
			ok = len(args) == 2 && args[0].Kind == facts.GoString && args[0].Value == "gorm:table_options"
		case "Session":
			ok = len(args) == 1 && r.session(args[0])
		}
		if !ok {
			return nil, e, "chain method " + name + " is not one the contract reads"
		}
		chain = append([]string{name}, chain...)
		e = *e.Callee.Object
	}
	return chain, e, ""
}

// session reports a &gorm.Session{...} literal that does not set DryRun.
func (r *resolver) session(e facts.GoExpr) bool {
	lit := e.Object
	if e.Kind != facts.GoUnary || e.Value != "&" || lit == nil || lit.Kind != facts.GoComposite || lit.Type == nil || lit.Type.Kind != facts.GoSelector || lit.Type.Name != "Session" || r.importOf(r.path, lit.Type.Object) != pkgGorm {
		return false
	}
	for _, el := range lit.Elements {
		if el.Key == nil || el.Key.Kind != facts.GoIdentifier {
			return false
		}
		if el.Key.Name == "DryRun" && (el.Value.Kind != facts.GoBoolean || el.Value.Value != "false") {
			return false
		}
	}
	return true
}

// receiver proves that the chain start is a *gorm.DB (row GO-K3): a local
// declared `var x *gorm.DB`, a *gorm.DB parameter, a local defined from
// gorm.Open or from a module function whose first result is *gorm.DB, a
// module package-level `var X *gorm.DB`, or a *gorm.DB field of a module
// struct reached through the receiver or a parameter of that struct's type.
// T018g5 Revision 2 (A4) also follows the handle: a module function counts
// only when every return proves a handle, a parameter only when every call
// of its function in the module passes one, a field only when every
// literal and assignment of it does, and a local only when every
// assignment of it does (handleFlows).
func (r *resolver) receiver(fn *facts.GoFunc, e facts.GoExpr) (proof, call, why string) {
	return r.receiverIn(fn, e, map[string]bool{})
}

func (r *resolver) receiverIn(fn *facts.GoFunc, e facts.GoExpr, seen map[string]bool) (proof, call, why string) {
	switch e.Kind {
	case facts.GoIdentifier:
		if declares(fn, e.Name) {
			return r.local(fn, e.Name, seen)
		}
		if r.packageVar(path.Dir(r.path), r.d.in.Files[r.path].Package, e.Name) {
			return "package var", "", ""
		}
		return "", "", "receiver " + e.Name + " is not a proven *gorm.DB"
	case facts.GoSelector:
		obj := e.Object
		if obj.Kind != facts.GoIdentifier {
			break
		}
		if declares(fn, obj.Name) {
			if ok, why := r.field(fn, obj.Name, e.Name, seen); ok {
				return "field", "", ""
			} else if why != "" {
				return "", "", "receiver " + obj.Name + "." + e.Name + ": " + why
			}
			return "", "", "receiver " + obj.Name + "." + e.Name + " is not a proven *gorm.DB field"
		}
		if b, ok := r.importsOf(r.path)[obj.Name]; ok && b.Dir != "" && r.packageVar(b.Dir, b.Package, e.Name) {
			return "package var", "", ""
		}
		return "", "", "receiver " + obj.Name + "." + e.Name + " is not a proven *gorm.DB"
	}
	return "", "", "the receiver chain does not start at a name"
}

// exprProof proves that expression e of function fn in file holds a
// *gorm.DB through the allowed chain (Revision 2, A4): a call of gorm.Open
// or of a module function whose returns all prove one, or a chain of the
// allowed methods on a proven start.
func (r *resolver) exprProof(file string, fn *facts.GoFunc, e facts.GoExpr, seen map[string]bool) (string, string) {
	saved := r.path
	r.path = file
	defer func() { r.path = saved }()
	if e.Kind == facts.GoNil {
		// A nil handle migrates nothing (the call panics).
		return "nil", ""
	}
	if e.Kind == facts.GoCall {
		if p, _, _ := r.firstResult(e, seen); p != "" {
			return p, ""
		}
	}
	_, start, why := r.chain(e)
	if why != "" {
		return "", why
	}
	if start.Kind == facts.GoCall {
		if p, _, why := r.firstResult(start, seen); p != "" {
			return p, ""
		} else if why != "" {
			return "", why
		}
		return "", "the value is a call whose first result is not a proven *gorm.DB"
	}
	p, _, why := r.receiverIn(fn, start, seen)
	return p, why
}

// local proves a name declared in fn: every declaration (parameter, var,
// define) must give it type *gorm.DB, and every assignment a proven
// handle (Revision 2, A4).
func (r *resolver) local(fn *facts.GoFunc, name string, seen map[string]bool) (proof, call, why string) {
	bad := "receiver " + name + " is not a proven *gorm.DB"
	if fn.Receiver != nil && fn.Receiver.Name == name {
		return "", "", bad
	}
	for i, p := range fn.Params {
		if p.Name == name {
			if p.Variadic || !r.isGormDB(r.path, &p.Type) {
				return "", "", bad
			}
			if why := r.paramFlows(r.path, fn, i, seen); why != "" {
				return "", "", "parameter " + name + ": " + why
			}
			proof = "param"
		}
	}
	for _, l := range fn.Locals {
		for i, n := range l.Names {
			if n != name {
				continue
			}
			var value *facts.GoExpr
			switch {
			case len(l.Values) == len(l.Names):
				value = &l.Values[i]
			case len(l.Values) == 1 && i == 0:
				value = &l.Values[0]
			}
			if l.Kind == "assign" {
				// An assignment must keep the handle proven (x = x.Table(...)
				// would not).
				if value == nil {
					return "", "", bad + ": it is assigned a value that is not read"
				}
				if p, why := r.exprProof(r.path, fn, *value, seen); p == "" {
					return "", "", bad + ": an assignment of it at line " + fmt.Sprint(l.Line) + " is not a proven *gorm.DB (" + why + ")"
				}
				continue
			}
			p, c := "", ""
			switch {
			case l.Type != nil:
				if !r.isGormDB(r.path, l.Type) {
					break
				}
				if value == nil && len(l.Values) == 0 {
					p = "var"
				} else if value != nil {
					if pv, why := r.exprProof(r.path, fn, *value, seen); pv != "" {
						p = "var"
					} else {
						return "", "", bad + ": " + why
					}
				}
			case value != nil && value.Kind == facts.GoCall:
				var why string
				p, c, why = r.firstResult(*value, seen)
				if p == "" && why != "" {
					return "", "", bad + ": " + why
				}
			}
			if p == "" {
				return "", "", bad
			}
			if proof == "" {
				proof, call = p, c
			}
		}
	}
	return proof, call, ""
}

// firstResult proves a call whose first result is a *gorm.DB: gorm.Open,
// or a module function (F or pkg.F) declared with that first result whose
// every return statement proves a handle (Revision 2, A4).
func (r *resolver) firstResult(call facts.GoExpr, seen map[string]bool) (proof, name, why string) {
	callee := *call.Callee
	if callee.Kind == facts.GoSelector && callee.Name == "Open" && r.importOf(r.path, callee.Object) == pkgGorm {
		return "gorm.Open", "gorm.Open", ""
	}
	dir, pkg, fname, _, ok := r.scope(r.path, callee)
	if !ok {
		return "", "", ""
	}
	fn, file := r.function(dir, pkg, fname)
	if fn == nil || len(fn.Results) == 0 || !r.isGormDB(file, &fn.Results[0].Type) {
		return "", "", ""
	}
	if why := r.returnsProven(file, fn, seen); why != "" {
		return "", "", "the module function " + fname + " " + why
	}
	if callee.Kind == facts.GoSelector {
		return "module function", callee.Object.Name + "." + fname, ""
	}
	return "module function", fname, ""
}

// returnsProven checks every return statement of a module function: its
// first value must prove a handle ("" when it does).
func (r *resolver) returnsProven(file string, fn *facts.GoFunc, seen map[string]bool) string {
	key := "return:" + file + "#" + funcName(fn)
	if seen[key] {
		return "returns a handle that depends on itself"
	}
	seen[key] = true
	defer delete(seen, key)
	if len(fn.Returns) == 0 {
		return "has no return statement read"
	}
	for _, ret := range fn.Returns {
		if ret.Value == nil {
			return fmt.Sprintf("has a bare return at line %d", ret.Line)
		}
		if p, why := r.exprProof(file, fn, *ret.Value, seen); p == "" {
			return fmt.Sprintf("returns a value at line %d that is not a proven *gorm.DB (%s)", ret.Line, why)
		}
	}
	return ""
}

// handleUse is one place a function passes a value on: the file and
// function it is in, and the call or literal tree.
type handleUse struct {
	file string
	line int
	fn   *facts.GoFunc
	tree facts.GoExpr
}

// uses lists every captured call (calls == true) or composite literal of
// the module's gated read files: the functions' Calls or Literals and the
// trees of package-level initialisers.
func (r *resolver) uses(calls bool) []handleUse {
	var out []handleUse
	for _, p := range r.m.Files {
		g := r.d.in.Files[p].Go
		if g == nil || !g.Gated {
			continue
		}
		for i := range g.Funcs {
			fn := &g.Funcs[i]
			list := fn.Literals
			if calls {
				list = fn.Calls
			}
			for _, s := range list {
				out = append(out, handleUse{file: p, line: s.Line, fn: fn, tree: s.Call})
			}
		}
		empty := &facts.GoFunc{}
		for _, v := range g.Vars {
			for i := range v.Values {
				walkExpr(&v.Values[i], func(e *facts.GoExpr) {
					if calls && e.Kind == facts.GoCall || !calls && e.Kind == facts.GoComposite {
						out = append(out, handleUse{file: p, line: v.Line, fn: empty, tree: *e})
					}
				})
			}
		}
	}
	return out
}

// walkExpr calls visit on e and every expression below it.
func walkExpr(e *facts.GoExpr, visit func(*facts.GoExpr)) {
	if e == nil {
		return
	}
	visit(e)
	for _, x := range []*facts.GoExpr{e.Object, e.Callee, e.Type, e.Left, e.Right} {
		walkExpr(x, visit)
	}
	for i := range e.Args {
		walkExpr(&e.Args[i], visit)
	}
	for i := range e.Elements {
		walkExpr(e.Elements[i].Key, visit)
		walkExpr(&e.Elements[i].Value, visit)
	}
}

// names reports whether expression e of file names the module-level
// declaration name of package dir (an identifier of that package not
// declared in fn, or pkg.name with pkg bound to it).
func (r *resolver) names(file string, fn *facts.GoFunc, e *facts.GoExpr, dir, name string) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case facts.GoIdentifier:
		return e.Name == name && path.Dir(file) == dir && r.d.in.Files[file].Package == r.d.in.Files[r.funcFile(dir)].Package && !declares(fn, name) && !slices.Contains(fn.LocalTypes, name)
	case facts.GoSelector:
		if e.Name != name || e.Object.Kind != facts.GoIdentifier || declares(fn, e.Object.Name) {
			return false
		}
		b, ok := r.importsOf(file)[e.Object.Name]
		return ok && b.Dir == dir
	}
	return false
}

// funcFile is a read file of package directory dir (for its package
// name).
func (r *resolver) funcFile(dir string) string {
	for _, p := range r.m.Files {
		if path.Dir(p) == dir {
			return p
		}
	}
	return ""
}

// paramFlows checks that every call of fn (declared in file) in the
// module passes a proven handle as parameter i ("" when it does).
// A method's calls are not resolved; a call in a read file outside gate
// G1 cannot be read.
func (r *resolver) paramFlows(file string, fn *facts.GoFunc, i int, seen map[string]bool) string {
	if fn.Receiver != nil {
		return "a method's calls are not resolved, so what it is passed is not proven"
	}
	key := fmt.Sprintf("param:%s#%s#%d", file, fn.Name, i)
	if seen[key] {
		return "what the function is passed depends on itself"
	}
	seen[key] = true
	defer delete(seen, key)
	dir := path.Dir(file)
	for _, p := range r.m.Files {
		f := r.d.in.Files[p]
		if f.Go != nil && f.Go.Gated {
			continue
		}
		for _, c := range f.Calls {
			base, rest, sel := strings.Cut(c.Name, ".")
			if !sel && c.Name == fn.Name && path.Dir(p) == dir || sel && rest == fn.Name && r.importsOf(p)[base].Dir == dir {
				return fmt.Sprintf("it is called at %s:%d, a file without a GORM import, which is not read", p, c.Span.StartLine)
			}
		}
	}
	for _, u := range r.uses(true) {
		if !r.names(u.file, u.fn, u.tree.Callee, dir, fn.Name) {
			continue
		}
		at := fmt.Sprintf("%s:%d", u.file, u.line)
		if len(u.tree.Args) != len(fn.Params) || i >= len(u.tree.Args) {
			return "the call at " + at + " does not pass one argument per parameter"
		}
		if p, why := r.exprProof(u.file, u.fn, u.tree.Args[i], seen); p == "" {
			return "the call at " + at + " passes a value that is not a proven *gorm.DB (" + why + ")"
		}
	}
	return ""
}

// field reports that name (fn's receiver or a parameter, of a module struct
// type T or *T) has a field f of type *gorm.DB that every literal and
// assignment in the module sets to a proven handle (why says what is not).
func (r *resolver) field(fn *facts.GoFunc, name, f string, seen map[string]bool) (bool, string) {
	var typ *facts.GoExpr
	if fn.Receiver != nil && fn.Receiver.Name == name {
		typ = &fn.Receiver.Type
	}
	for i := range fn.Params {
		if fn.Params[i].Name == name {
			if typ != nil || fn.Params[i].Variadic {
				return false, ""
			}
			typ = &fn.Params[i].Type
		}
	}
	for _, l := range fn.Locals {
		for _, n := range l.Names {
			if n == name && l.Kind != "assign" {
				return false, ""
			}
		}
	}
	if typ == nil {
		return false, ""
	}
	file, s := r.structOf(r.path, *typ)
	if s == nil {
		return false, ""
	}
	index := 0
	for _, fd := range s.Fields {
		names := fd.Names
		if fd.Embedded {
			names = []string{""}
		}
		for _, n := range names {
			if n == f {
				if !r.isGormDB(file, &fd.Type) {
					return false, ""
				}
				if why := r.fieldFlows(file, s, f, index, seen); why != "" {
					return false, why
				}
				return true, ""
			}
			index++
		}
	}
	return false, ""
}

// fieldFlows checks that every composite literal of struct s (declared in
// file) and every assignment to a selector named f in the module's gated
// read files sets f to a proven handle (Revision 2, A4). A literal or
// assignment in a file outside gate G1 is not seen (known gap).
func (r *resolver) fieldFlows(file string, s *facts.GoStruct, f string, index int, seen map[string]bool) string {
	key := "field:" + file + "#" + s.Name + "." + f
	if seen[key] {
		return "the field's value depends on itself"
	}
	seen[key] = true
	defer delete(seen, key)
	dir := path.Dir(file)
	check := func(u handleUse, v facts.GoExpr) string {
		if p, why := r.exprProof(u.file, u.fn, v, seen); p == "" {
			return fmt.Sprintf("the field %s.%s is set at %s:%d to a value that is not a proven *gorm.DB (%s)", s.Name, f, u.file, u.line, why)
		}
		return ""
	}
	for _, u := range r.uses(false) {
		if !r.names(u.file, u.fn, u.tree.Type, dir, s.Name) {
			continue
		}
		for k, el := range u.tree.Elements {
			switch {
			case el.Key != nil && el.Key.Kind == facts.GoIdentifier && el.Key.Name == f, el.Key == nil && k == index:
				if why := check(u, el.Value); why != "" {
					return why
				}
			}
		}
	}
	for _, p := range r.m.Files {
		g := r.d.in.Files[p].Go
		if g == nil || !g.Gated {
			continue
		}
		for i := range g.Funcs {
			fn := &g.Funcs[i]
			for _, st := range fn.Stores {
				if st.Target.Name != f {
					continue
				}
				if st.Value == nil {
					return fmt.Sprintf("a field named %s is assigned at %s:%d a value that is not read", f, p, st.Line)
				}
				if why := check(handleUse{file: p, line: st.Line, fn: fn}, *st.Value); why != "" {
					return why
				}
			}
		}
	}
	return ""
}

// function finds a module package function (no receiver).
func (r *resolver) function(dir, pkg, name string) (*facts.GoFunc, string) {
	for _, p := range r.packageFiles(dir, pkg) {
		g := r.d.in.Files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Funcs {
			if g.Funcs[i].Receiver == nil && g.Funcs[i].Name == name {
				return &g.Funcs[i], p
			}
		}
	}
	return nil, ""
}

// packageVar reports a module package-level `var X *gorm.DB`.
func (r *resolver) packageVar(dir, pkg, name string) bool {
	for _, p := range r.packageFiles(dir, pkg) {
		g := r.d.in.Files[p].Go
		if g == nil {
			continue
		}
		for _, v := range g.Vars {
			for _, n := range v.Names {
				if n == name {
					return r.isGormDB(p, v.Type)
				}
			}
		}
	}
	return false
}

// argument reads one migrate-site argument: new(T), &T{...} or T{...} with
// T a struct of the module's own packages.
func (r *resolver) argument(fn *facts.GoFunc, a facts.GoExpr) Arg {
	var t *facts.GoExpr
	form := ""
	switch {
	case a.Kind == facts.GoCall && a.Callee.Kind == facts.GoIdentifier && a.Callee.Name == "new" && len(a.Args) == 1:
		form, t = "new(T)", &a.Args[0]
	case a.Kind == facts.GoUnary && a.Value == "&" && a.Object != nil && a.Object.Kind == facts.GoComposite:
		form, t = "&T{...}", a.Object.Type
	case a.Kind == facts.GoComposite:
		form, t = "T{...}", a.Type
	default:
		return Arg{Reason: "argument of kind " + a.Kind + " is not new(T), &T{...} or T{...}"}
	}
	if t == nil || t.Kind != facts.GoIdentifier && t.Kind != facts.GoSelector {
		return Arg{Reason: form + " does not name a type"}
	}
	if t.Kind == facts.GoIdentifier && fn != nil && slices.Contains(fn.LocalTypes, t.Name) {
		// A type declared in the function shadows the package's (Revision
		// 2, A6); the payload keeps no shape of it.
		return Arg{Reason: form + " names " + t.Name + ", a type declared inside the function"}
	}
	dir, pkg, name, external, ok := r.scope(r.path, *t)
	if !ok {
		if external != "" {
			r.d.in.tag("GO-K1")
			return Arg{Reason: "model package " + external + " is outside the module"}
		}
		return Arg{Reason: form + " does not name a module type"}
	}
	for _, p := range r.packageFiles(dir, pkg) {
		g := r.d.in.Files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Structs {
			if s := &g.Structs[i]; s.Name == name {
				if len(s.TypeParams) > 0 {
					return Arg{Reason: "struct " + name + " is generic"}
				}
				return Arg{Form: form, Model: &Model{File: p, Dir: dir, Struct: s}}
			}
		}
	}
	return Arg{Reason: "type " + name + " is not a struct declared in the module's read files"}
}

func sortStrings(s []string) { sort.Strings(s) }

// changeMarker records a call that changes the schema the migrate calls
// create (Revision 2, A2, A3; row GO-K3): a marker diagnostic without the
// list marker, emitted when the module is read.
func (r *resolver) changeMarker(file string, line int, why string) {
	r.d.in.tag("GO-K3")
	at := fmt.Sprintf("%s:%d", file, line)
	r.d.pending = append(r.d.pending, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: codeUnread, Message: at + ": " + why})
}

// joinSetup reads a SetupJoinTable(model, "Field", joinModel) call of any
// receiver (GORM keeps the join model in the schema cache the handle
// shares, Revision 2, A2).
func (r *resolver) joinSetup(fn *facts.GoFunc, s facts.GoSite) {
	j := JoinSetup{File: r.path, Line: s.Line}
	args := s.Call.Args
	switch {
	case len(args) != 3:
		j.Reason = "SetupJoinTable does not have three arguments"
	case args[1].Kind != facts.GoString:
		j.Reason = "the field argument of SetupJoinTable is not a string literal"
	default:
		owner, join := r.argument(fn, args[0]), r.argument(fn, args[2])
		j.Owner, j.Join, j.Field = owner.Model, join.Model, args[1].Value
		if owner.Model == nil {
			j.Reason = "the model argument of SetupJoinTable is not read: " + owner.Reason
		}
	}
	r.m.JoinSetups = append(r.m.JoinSetups, j)
	if j.Reason != "" {
		r.changeMarker(r.path, s.Line, "SetupJoinTable call not read ("+j.Reason+"): every join table of the group is not claimed")
		return
	}
	r.changeMarker(r.path, s.Line, "SetupJoinTable replaces the join table of "+j.Owner.Struct.Name+"."+j.Field+" with a custom join model: that join table is not claimed")
}

// schemaChange reads a Migrator DDL call: its first argument is the model
// (new(T), &T{...}, T{...}) or a string table name; anything else may name
// every table (Revision 2, A3).
func (r *resolver) schemaChange(fn *facts.GoFunc, s facts.GoSite, method string) {
	ch := SchemaChange{File: r.path, Line: s.Line, Method: method}
	args := s.Call.Args
	switch {
	case len(args) == 0:
		ch.Reason = method + " has no arguments"
	case args[0].Kind == facts.GoString:
		ch.Table = args[0].Value
	default:
		a := r.argument(fn, args[0])
		ch.Model = a.Model
		if a.Model == nil {
			ch.Reason = "its table argument is not read: " + a.Reason
		}
	}
	if columnMethods[method] && len(args) > 1 && args[1].Kind == facts.GoString {
		ch.Column = args[1].Value
	}
	r.m.Changes = append(r.m.Changes, ch)
	target := "every table of the group"
	switch {
	case ch.Model != nil:
		target = "the table of " + ch.Model.Struct.Name
	case ch.Table != "":
		target = "the table " + ch.Table
	}
	why := method + " changes the schema of " + target + " after the migrate calls: its column, index and constraint sets are partial"
	if ch.Reason != "" {
		why += " (" + ch.Reason + ")"
	}
	msg := "Migrator()." + why
	if method == "RenameTable" {
		r.d.unread(r.m, r.path, s.Line, msg)
		return
	}
	r.changeMarker(r.path, s.Line, msg)
}

// rawSQL reads an Exec or Raw call whose first argument is a string
// literal (or a + of them) with a statement starting with CREATE, ALTER,
// DROP or RENAME: the database may hold tables the migrate calls do not
// create (Revision 2, A3; GO-V2), so the group's table list may be
// incomplete; the statements that may change a claimed table are kept
// for the claim pass (Revision 2 addendum).
func (r *resolver) rawSQL(s facts.GoSite, method string) {
	if len(s.Call.Args) == 0 {
		return
	}
	text, ok := literalText(s.Call.Args[0])
	if !ok {
		return
	}
	word, changes := readRawDDL(text)
	if word == "" {
		return
	}
	r.m.RawDDL = append(r.m.RawDDL, fmt.Sprintf("%s:%d", r.path, s.Line))
	r.d.unread(r.m, r.path, s.Line, method+" runs DDL text ("+word+" ...): the database may hold tables, columns, indexes or constraints the migrate calls do not create (GO-V2)")
	for _, ch := range changes {
		ch.File, ch.Line, ch.Method = r.path, s.Line, method
		r.m.RawChanges = append(r.m.RawChanges, ch)
	}
}

// literalText is the text of a string literal or a + of string literals.
func literalText(e facts.GoExpr) (string, bool) {
	switch e.Kind {
	case facts.GoString:
		return e.Value, true
	case facts.GoBinary:
		if e.Value != "+" || e.Left == nil || e.Right == nil {
			return "", false
		}
		l, ok := literalText(*e.Left)
		if !ok {
			return "", false
		}
		rt, ok := literalText(*e.Right)
		return l + rt, ok
	}
	return "", false
}
