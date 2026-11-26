package sequelize

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Input is what the Sequelize front end reads at link time: the
// TypeScript facts of every file (imports, export clauses and the ungated
// call-site cache), the cached model payloads of the gated files, the
// shared module resolver and the retained sources (package.json,
// lockfiles, node-config JSON).
type Input struct {
	TypeScript map[string]typescript.Facts
	Models     map[string]*typescript.ModelSyntax
	Resolver   *typescript.ModuleResolver
	Sources    map[string][]byte
	// Tests classifies test files: their define and association call
	// sites are counted in one marker and never make a group incomplete.
	Tests testscope.Rules
	// Migrations and Edges are the migration pass's records (for the
	// kit's overlap diagnostic in the claim pass, SQB4).
	Migrations []contract.Node
	Edges      []contract.Edge
	// Tag reports a contract row ("<row>" or "<row>/<form>") to the
	// contract test; nil outside tests.
	Tag func(row string)
}

func (in Input) tag(row string) {
	if in.Tag != nil {
		in.Tag(row)
	}
}

// The packages discovery reads.
const (
	pkgSequelize  = "sequelize"
	pkgInflection = "inflection"
	pkgTypescript = "sequelize-typescript"
)

// Package is one directory with a package.json whose dependencies or
// devDependencies name sequelize.
type Package struct {
	// Dir is the repository-relative package directory ("." for the root);
	// Manifest its package.json; Spec the sequelize range it declares.
	Dir, Manifest, Spec string
	// Versions are the locked versions of sequelize and inflection (""
	// when not proven); Lockfile the lockfile read.
	Versions map[string]string
	Lockfile string
	// Pins is {"sequelize": v, "inflection": v} when both are proven, else
	// nil (SQ-V1 is still listed).
	Pins map[string]any
	// Refused says why no claim is made for the package (another major).
	Refused string
	// Drivers are the Sequelize driver packages the package depends on,
	// sorted.
	Drivers []string
	// Migrations are the migrations folders found (SQ-U1, never read);
	// MigrationFiles counts the script files under each.
	Migrations     []string
	MigrationFiles map[string]int
	// Instances are the package's module-level Sequelize instances; each
	// is one model group.
	Instances []*Instance
}

// Instance is one module-level `const x = new Sequelize(...)`: a model
// group.
type Instance struct {
	// File and Name locate the declaration; Root is the group root (the
	// file, or "<file>#<name>" when the file declares several instances).
	File, Name, Root string
	Span             contract.Span
	Package          *Package
	Options          Options
	// Dialect is the dialect the claims use ("" when unknown); DialectFrom
	// is "literal" or "driver"; DialectReason says why the dialect is
	// unknown or unsupported (dialect-dependent facts are then unknown,
	// or the group is row D).
	Dialect, DialectFrom, DialectReason string
	// Models are the model definitions that reach this instance, in
	// discovery order (file, then source order).
	Models []*Model
	// Associations are the reached association calls in execution order
	// as far as it is proven (OrderProven).
	Associations []*Association
	// OrderProven is false when reached module-level association calls
	// sit in more than one module (the order between modules depends on
	// the entry point), so the claim pass does not rely on first-wins
	// across them.
	OrderProven bool
	// SyncSites are the `<instance>.sync(...)` call sites found
	// ("<file>:<line>"); none means the database is built another way
	// (SQ-V2). No claim depends on them.
	SyncSites []string
	// Incomplete lists the sites that may add tables or attributes to the
	// group without being read (a define in a function, a directory
	// loader, an association call outside the reach rule, an associate
	// function that is not run by a recognised loop or holds statements
	// this pass does not read). The group's table list and the affected
	// tables' column sets are then not complete.
	Incomplete []string
	// AttributesIncomplete are the Incomplete entries about association
	// calls (an associate function no recognised loop runs or with
	// statements that are not read, an association call outside the reach
	// rule): any model's attributes may then change, so every table's
	// column set is partial.
	AttributesIncomplete []string
}

// SetKey is the instance's model group key.
func (g *Instance) SetKey() string { return models.SetKey(Tool, g.Root) }

// Model is one model definition reached by an instance: a module-level
// `X.init({...}, {sequelize, ...})` statement (row SQ-M2) or a `define`
// call (row SQ-M1) at module level or in a factory's body.
type Model struct {
	File string
	// Form is "init" or "define"; Context is "module" or "factory".
	Form, Context string
	Span          contract.Span
	// Call is the init or define call.
	Call *typescript.ModelExpr
	// Class is the class declaration an init's receiver names (nil when the
	// receiver is not a class of the file); ClassName the receiver name.
	Class     *typescript.ModelStatement
	ClassName string
	// Binding is the declared name of a define's result (`const area =
	// sequelize.define(...)`), "" when the result is not bound.
	Binding string
	// Factory is set for a define in a factory body: the factory, the call
	// that runs it with the instance, and the models object key the call
	// is the value of.
	Factory *Factory
}

// Factory is a CommonJS factory `module.exports = (sequelize, DataTypes)
// => {...}` called with the instance.
type Factory struct {
	// Expr is the factory function; Params its parameter names.
	Expr   *typescript.ModelExpr
	Params []string
	// CallFile and Call locate the call `XModel(sequelize, Sequelize)`;
	// Object and Key name the module-level object entry it is the value
	// of ("" when it is not one).
	CallFile    string
	Call        *typescript.ModelExpr
	Object, Key string
}

// Association is one reached association call.
type Association struct {
	File string
	Span contract.Span
	// Method is belongsTo, hasOne, hasMany or belongsToMany; Call the call.
	Method string
	Call   *typescript.ModelExpr
	// Context is "module" (a module-level statement) or "associate" (a
	// statement of an associate function the recognised loop runs);
	// LoopFile and LoopSpan locate that loop's module-level statement.
	Context  string
	LoopFile string
	LoopSpan contract.Span
	// Source and Target are the models the receiver and the first argument
	// resolve to (nil when they do not resolve; the claim pass leaves the
	// facts the call may add unknown).
	Source, Target *Model
}

// Diagnostic codes of discovery (info).
const (
	codeNotProven = "schema_model_not_proven"
	codeUnread    = "schema_model_unread"
	// codeMigrations marks a package's migrations folder (the code the
	// Prisma and Drizzle front ends use); its message ends with
	// migrationsMarker and the folder.
	codeMigrations = "schema_model_migrations"
)

// migrationsMarker separates the manifest from the folder in a
// schema_model_migrations message ("<package.json>: <marker><dir>").
const migrationsMarker = "sequelize-cli or umzug migrations folder (not read): "

// associationMethods are Sequelize's association calls (rows SQ-R1,
// SQ-R2).
var associationMethods = map[string]bool{"belongsTo": true, "hasOne": true, "hasMany": true, "belongsToMany": true}

type manifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// discovery holds the state of one Discover run.
type discovery struct {
	in        Input
	files     []string
	manifests []string
	diags     []contract.Diagnostic
	// requires records the relative requires followed, per requiring file.
	requires map[string][]string
	// current is the file whose instance options are being read.
	current string
	// reached and reported are the definition and association call sites
	// already placed (siteKey), so the call-site cache does not report
	// them again; testSites counts the test-file sites per package.
	reached, reported map[string]bool
	testSites         map[*Package]int
}

// unreadDiag reports a definition found but not read (row D) once per
// instance of the package, the message ending with that group's
// table-list marker (listMarker): the server then marks the group's
// table_list may_be_incomplete whatever file the marker is on.
func (d *discovery) unreadDiag(p *Package, file, msg string) {
	if len(p.Instances) == 0 {
		d.diag(file, codeUnread, msg)
		return
	}
	for _, g := range p.Instances {
		d.diag(file, codeUnread, msg+listMarker(g.SetKey()))
	}
}

func (d *discovery) diag(file, code, msg string) {
	d.diags = append(d.diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
}

// Discover finds every Sequelize package of the repository, its pins,
// drivers and migrations folders, its module-level instances with their
// options and dialect, and for each instance the models and association
// calls that reach it. Packages are sorted by directory. Diagnostics:
// schema_model_not_proven for a package or instance without claims,
// schema_model_unread markers for definitions found but not read (row D:
// sequelize-typescript, defines in functions, directory loaders, models
// whose receiver does not resolve or that reach two instances,
// association calls outside the reach rule), schema_model_migrations for
// each migrations folder (row SQ-U1).
func Discover(in Input) ([]*Package, []contract.Diagnostic) {
	d, pkgs := runDiscovery(in)
	return pkgs, d.diags
}

// runDiscovery runs discovery and keeps its state, which the claim pass uses
// to resolve names.
func runDiscovery(in Input) (*discovery, []*Package) {
	d := &discovery{in: in, requires: map[string][]string{}, reached: map[string]bool{}, reported: map[string]bool{}, testSites: map[*Package]int{}}
	for p := range in.Sources {
		d.files = append(d.files, p)
	}
	sort.Strings(d.files)
	for _, p := range d.files {
		if path.Base(p) == "package.json" && !underNodeModules(p) {
			d.manifests = append(d.manifests, p)
		}
	}
	var out []*Package
	for _, m := range d.manifests {
		var man manifest
		if json.Unmarshal(in.Sources[m], &man) != nil {
			continue
		}
		spec, ok := man.Dependencies[pkgSequelize]
		if !ok {
			spec, ok = man.DevDependencies[pkgSequelize]
		}
		if !ok {
			continue
		}
		in.tag("SQ-K1/package dependency")
		p := &Package{Dir: path.Dir(m), Manifest: m, Spec: spec, Versions: map[string]string{}}
		out = append(out, p)
		d.pins(p)
		for name := range driverDialects {
			_, a := man.Dependencies[name]
			_, b := man.DevDependencies[name]
			if a || b {
				p.Drivers = append(p.Drivers, name)
			}
		}
		sort.Strings(p.Drivers)
		d.migrations(p)
		_, tsDep := man.Dependencies[pkgTypescript]
		_, tsDev := man.DevDependencies[pkgTypescript]
		decorated := m + ": depends on sequelize-typescript; its decorated models are not read (row D)"
		if p.Refused != "" {
			if tsDep || tsDev {
				d.diag(m, codeUnread, decorated)
			}
			d.diag(m, codeNotProven, m+": "+p.Refused)
			continue
		}
		d.instances(p)
		d.instanceHooks(p)
		if tsDep || tsDev {
			d.unreadDiag(p, m, decorated)
		}
		if len(p.Instances) == 0 {
			d.diag(m, codeNotProven, m+": no module-level `new Sequelize(...)` instance is found in this package; its models are not read")
			continue
		}
		for _, g := range p.Instances {
			d.reachModels(g)
		}
		d.unreachedModels(p)
		for _, g := range p.Instances {
			d.reachAssociations(g)
		}
		d.unreachedCalls(p)
		d.syncSites(p)
	}
	d.otherPackageModels(out)
	return d, out
}

// otherPackageModels marks the groups that a model of another package
// reaches: a module-level define whose receiver, or an
// init whose sequelize option, resolves to an instance of a package that
// does not own the file. Such a model is not read; the instance's group is
// incomplete, and when the file's package has association calls the
// attributes of every model of the group may change too.
func (d *discovery) otherPackageModels(pkgs []*Package) {
	var all []*Instance
	for _, p := range pkgs {
		all = append(all, p.Instances...)
	}
	if len(all) == 0 {
		return
	}
	instanceOf := func(file string, e *typescript.ModelExpr) *Instance {
		v := d.expr(file, e, 0)
		for _, g := range all {
			if v.Decl != "" && g.File == v.File && g.Name == v.Decl {
				return g
			}
		}
		return nil
	}
	for _, f := range d.files {
		m := d.in.Models[f]
		if m == nil || underNodeModules(f) || !scriptFile(f) {
			continue
		}
		owner := d.owner(f)
		check := func(e *typescript.ModelExpr) {
			var g *Instance
			switch methodCall(e) {
			case "define":
				if len(e.Args) > 0 {
					g = instanceOf(f, e.Callee.Object)
				}
			case "init":
				if len(e.Args) >= 2 && e.Args[1].Kind == typescript.ModelObject {
					if opt := optionValue(&e.Args[1], "sequelize"); opt != nil && opt.Kind != typescript.ModelUnknown {
						g = instanceOf(f, opt)
					}
				}
			}
			if g == nil || g.Package.Dir == owner || d.isTest(f) {
				return
			}
			msg := fmt.Sprintf("%s:%d: a model of another package (%s) reaches Sequelize instance %s; it is not read", f, e.Span.StartLine, owner, g.Root)
			d.diag(f, codeUnread, msg+listMarker(g.SetKey()))
			if d.packageHasAssociations(owner) {
				g.addAttributesIncomplete(msg)
			} else {
				g.Incomplete = append(g.Incomplete, msg)
			}
		}
		for i := range m.Statements {
			if s := &m.Statements[i]; s.Kind == typescript.StatementExpression {
				check(s.Value)
			}
		}
		for i := range m.Declarations {
			check(&m.Declarations[i].Value)
		}
	}
}

// packageHasAssociations reports an association call in a file owned by
// the package directory dir.
func (d *discovery) packageHasAssociations(dir string) bool {
	for _, f := range d.files {
		if d.owner(f) != dir || underNodeModules(f) {
			continue
		}
		for _, c := range clientCalls(d.in.TypeScript[f]) {
			if associationMethods[c.Expression.Object.Name] && len(c.Expression.Args) > 0 {
				return true
			}
		}
	}
	return false
}

func underNodeModules(p string) bool {
	return strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/")
}

// owner is the directory of the nearest package.json above file, "." when
// none.
func (d *discovery) owner(file string) string {
	best, n := ".", 0
	for _, m := range d.manifests {
		if dir := path.Dir(m); dir != "." && strings.HasPrefix(file, dir+"/") && len(dir) > n {
			best, n = dir, len(dir)
		}
	}
	return best
}

// packageFiles are the TypeScript and JavaScript files the package owns
// (not under node_modules), sorted.
func (d *discovery) packageFiles(p *Package) []string {
	var out []string
	for _, f := range d.files {
		if _, ok := d.in.TypeScript[f]; !ok || underNodeModules(f) || !scriptFile(f) {
			continue
		}
		if d.owner(f) == p.Dir {
			out = append(out, f)
		}
	}
	return out
}

func scriptFile(f string) bool {
	switch path.Ext(f) {
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// pins reads the locked versions (SQ-V1) and refuses any major but 6
// (developer decision 2026-11-13).
func (d *discovery) pins(p *Package) {
	v, lf, _ := lockfile.Locked(d.in.Sources, p.Dir, pkgSequelize, p.Spec)
	p.Versions[pkgSequelize] = v
	p.Lockfile = lf
	if v != "" {
		if iv := d.inflection(p, lf); iv != "" {
			p.Versions[pkgInflection] = iv
			p.Pins = map[string]any{pkgSequelize: v, pkgInflection: iv}
			d.in.tag("SQ-K1/lockfile pins")
		}
	}
	shown := v
	if shown == "" {
		shown = p.Spec
	}
	if major := lockfile.Major(shown); major != 6 {
		if major < 0 {
			p.Refused = "sequelize " + shown + ": the major version is not proven; the claim contract is verified for sequelize 6 only"
		} else {
			p.Refused = "sequelize " + shown + ": the claim contract is verified for sequelize 6 only; no claim is made until a corpus pins this major"
		}
	}
}

// inflection reads the inflection version Sequelize loads from a
// package-lock.json (lockfile 2 or 3): Node resolves `inflection` from the
// installed sequelize directory, so its own node_modules first, then each
// node_modules above it. Other lockfiles do not list transitive
// dependencies by install path; the pin is then not proven.
func (d *discovery) inflection(p *Package, lf string) string {
	if path.Base(lf) != "package-lock.json" {
		return ""
	}
	var lock struct {
		LockfileVersion int
		Packages        map[string]struct{ Version string }
	}
	if json.Unmarshal(d.in.Sources[lf], &lock) != nil || lock.LockfileVersion < 2 {
		return ""
	}
	base := path.Dir(lf)
	rel := p.Dir
	if base != "." {
		rel = strings.TrimPrefix(strings.TrimPrefix(p.Dir, base), "/")
		if rel == "" {
			rel = "."
		}
	}
	installed := ""
	for dir := rel; ; dir = path.Dir(dir) {
		key := path.Join(dir, "node_modules", pkgSequelize)
		if _, ok := lock.Packages[key]; ok {
			installed = key
			break
		}
		if dir == "." {
			return ""
		}
	}
	candidates := []string{path.Join(installed, "node_modules", pkgInflection)}
	for dir := path.Dir(installed); ; dir = path.Dir(dir) {
		if path.Base(dir) != "node_modules" {
			candidates = append(candidates, path.Join(dir, "node_modules", pkgInflection))
		}
		if dir == "." {
			break
		}
	}
	for _, key := range candidates {
		if e, ok := lock.Packages[key]; ok {
			return e.Version
		}
	}
	return ""
}

// migrations marks the package's migrations folders (SQ-U1): the default
// `migrations/` of sequelize-cli and umzug 2 (both relative to the
// working directory, assumed to be the package directory) when it holds
// script files. A `.sequelizerc` may name another folder; it is not read,
// and says so.
func (d *discovery) migrations(p *Package) {
	dir := path.Join(p.Dir, "migrations")
	n := 0
	for _, f := range d.files {
		if strings.HasPrefix(f, dir+"/") && scriptFile(f) {
			n++
		}
	}
	if n > 0 {
		p.Migrations = append(p.Migrations, dir)
		p.MigrationFiles = map[string]int{dir: n}
		d.in.tag("SQ-U1")
		d.diag(p.Manifest, codeMigrations, p.Manifest+": "+migrationsMarker+dir)
	}
	if rc := path.Join(p.Dir, ".sequelizerc"); d.in.Sources[rc] != nil {
		d.diag(rc, codeUnread, rc+": a .sequelizerc may set sequelize-cli's migrations-path; it is not read")
	}
}

// instances finds the package's module-level `const x = new
// Sequelize(...)` declarations and reads their options and dialect.
func (d *discovery) instances(p *Package) {
	perFile := map[string]int{}
	for _, f := range d.packageFiles(p) {
		m := d.in.Models[f]
		if m == nil {
			continue
		}
		for i := range m.Declarations {
			decl := &m.Declarations[i]
			if decl.Value.Kind != typescript.ModelNew {
				continue
			}
			if !d.expr(f, decl.Value.Callee, 0).isSequelizeClass() && !d.sequelizeMember(f, decl.Value.Callee) {
				continue
			}
			if decl.Keyword != "const" {
				d.diag(f, codeUnread, fmt.Sprintf("%s:%d: the Sequelize instance %s is declared with %s, so it may be reassigned; its models are not read", f, decl.Span.StartLine, decl.Name, decl.Keyword))
				continue
			}
			d.in.tag("SQ-K1/`new Sequelize(...)`")
			g := &Instance{File: f, Name: decl.Name, Root: f, Span: decl.Span, Package: p}
			perFile[f]++
			d.current = f
			g.Options = d.readOptions(f, &decl.Value, p)
			g.Dialect, g.DialectFrom, g.DialectReason = dialectRule(g.Options, p, d.in.tag)
			p.Instances = append(p.Instances, g)
			if !g.Options.Known {
				d.diag(f, codeNotProven, fmt.Sprintf("%s:%d: the options of Sequelize instance %s are not read (%s); every option-derived fact of its models is unknown", f, decl.Span.StartLine, decl.Name, g.Options.Reason))
			}
			if g.DialectReason != "" {
				d.diag(f, codeNotProven, fmt.Sprintf("%s:%d: dialect of Sequelize instance %s: %s", f, decl.Span.StartLine, decl.Name, g.DialectReason))
			}
		}
	}
	for _, g := range p.Instances {
		if perFile[g.File] > 1 {
			g.Root = g.File + "#" + g.Name
		}
	}
}

// hookMethods are the methods that register a Sequelize hook: addHook and
// one method per hook type (lib/hooks.js:5-51, applyTo :87-97). On an
// instance, beforeDefine and afterDefine run inside every define and init
// and may change any attribute or option; the class rule
// covers every hook type.
var hookMethods = map[string]bool{"addHook": true}

func init() {
	for _, h := range []string{
		"beforeValidate", "afterValidate", "validationFailed", "beforeCreate", "afterCreate", "beforeDestroy", "afterDestroy",
		"beforeRestore", "afterRestore", "beforeUpdate", "afterUpdate", "beforeSave", "afterSave", "beforeUpsert", "afterUpsert",
		"beforeBulkCreate", "afterBulkCreate", "beforeBulkDestroy", "afterBulkDestroy", "beforeBulkRestore", "afterBulkRestore",
		"beforeBulkUpdate", "afterBulkUpdate", "beforeFind", "beforeFindAfterExpandIncludeAll", "beforeFindAfterOptions", "afterFind",
		"beforeCount", "beforeDefine", "afterDefine", "beforeInit", "afterInit", "beforeAssociate", "afterAssociate",
		"beforeConnect", "afterConnect", "beforeDisconnect", "afterDisconnect", "beforePoolAcquire", "afterPoolAcquire",
		"beforeSync", "afterSync", "beforeBulkSync", "afterBulkSync", "beforeQuery", "afterQuery",
	} {
		hookMethods[h] = true
	}
}

// instanceHooks refuses the instances on which a file of the package
// registers a hook (row D): a call `<receiver>.addHook(...)`
// or `<receiver>.<hook type>(...)` anywhere (module level or in a
// function) whose receiver resolves to the instance, or to Sequelize's
// class (every instance of the package). Every option- and
// attribute-derived fact of the group is then unknown: the group is not
// claimed, as with options that are not read.
func (d *discovery) instanceHooks(p *Package) {
	for _, f := range d.packageFiles(p) {
		for _, c := range clientCalls(d.in.TypeScript[f]) {
			recv := c.Expression.Object.Object
			if !hookMethods[c.Expression.Object.Name] || recv == nil || recv.Kind != "identifier" {
				continue
			}
			e := &typescript.ModelExpr{Kind: typescript.ModelIdentifier, Name: recv.Name}
			var hit []*Instance
			if g, _ := d.instanceOf(p, f, e); g != nil {
				hit = append(hit, g)
			} else if d.expr(f, e, 0).isSequelizeClass() {
				hit = p.Instances
			}
			for _, g := range hit {
				if !g.Options.Known {
					continue
				}
				d.in.tag("D")
				g.Options = Options{Reason: fmt.Sprintf("%s:%d: %s.%s(...) registers a hook (instance hooks such as beforeDefine may change every model's attributes and options; row D)", f, c.Span.StartLine, recv.Name, c.Expression.Object.Name)}
				d.diag(f, codeNotProven, fmt.Sprintf("%s:%d: the options of Sequelize instance %s are not read (%s); every option-derived fact of its models is unknown", g.File, g.Span.StartLine, g.Name, g.Options.Reason))
			}
		}
	}
}

// sequelizeMember reports `S.Sequelize` or `S.default` where S is the
// sequelize package object.
func (d *discovery) sequelizeMember(file string, e *typescript.ModelExpr) bool {
	if e.Kind != typescript.ModelMember || e.Object.Kind != typescript.ModelIdentifier || (e.Name != "Sequelize" && e.Name != "default") {
		return false
	}
	return d.local(file, e.Object.Name, 0).isSequelizeModule()
}

// instanceOf returns the instance an expression of file resolves to, or
// nil with a reason.
func (d *discovery) instanceOf(p *Package, file string, e *typescript.ModelExpr) (*Instance, string) {
	v := d.expr(file, e, 0)
	if v.Decl == "" {
		if v.Why != "" {
			return nil, v.Why
		}
		return nil, "it is not a module-level declaration"
	}
	for _, g := range p.Instances {
		if g.File == v.File && g.Name == v.Decl {
			return g, ""
		}
	}
	return nil, fmt.Sprintf("it resolves to %s in %s, which is not a Sequelize instance of the package", v.Decl, v.File)
}

func (d *discovery) isTest(file string) bool {
	return testscope.Classify(file, d.in.Tests) == testscope.Test
}

// syncSites lists the calls `<receiver>.sync(...)` of the package whose
// receiver identifier resolves to an instance.
func (d *discovery) syncSites(p *Package) {
	for _, f := range d.packageFiles(p) {
		for _, c := range clientCalls(d.in.TypeScript[f]) {
			recv := c.Expression.Object.Object
			if c.Expression.Object.Name != "sync" || recv == nil || recv.Kind != "identifier" {
				continue
			}
			if g, _ := d.instanceOf(p, f, &typescript.ModelExpr{Kind: typescript.ModelIdentifier, Name: recv.Name}); g != nil {
				g.SyncSites = append(g.SyncSites, fmt.Sprintf("%s:%d", f, c.Span.StartLine))
			}
		}
	}
}
