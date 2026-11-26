package typeorm

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Input is what the TypeORM front end reads at link time: the TypeScript
// facts of every file (imports, export clauses), the cached model payloads
// of the gated files, the shared module resolver (module specifiers and
// the tsconfig chain) and the retained sources (package.json, lockfiles,
// ormconfig.json).
type Input struct {
	TypeScript map[string]typescript.Facts
	Models     map[string]*typescript.ModelSyntax
	Resolver   *typescript.ModuleResolver
	Sources    map[string][]byte
	// Tests classifies test files (TO-K4: forFeature calls and data
	// sources in test files are not read).
	Tests testscope.Rules
	// Tag reports a contract row ("<row>" or "<row>/<form>") to the
	// contract test; nil outside tests.
	Tag func(row string)
	// Migrations and Edges are the migration pass's records, which the kit
	// compares model tables with (never merged).
	Migrations []contract.Node
	Edges      []contract.Edge
}

func (in Input) tag(row string) {
	if in.Tag != nil {
		in.Tag(row)
	}
}

// The packages discovery reads.
const (
	pkgTypeORM    = "typeorm"
	pkgTypeScript = "typescript"
	pkgNest       = "@nestjs/typeorm"
	pkgNestConfig = "@nestjs/config"
	pkgNaming     = "typeorm-naming-strategies"
)

// Major lines of TypeORM the contract reads (Revision 2 O12).
const (
	Line03 = "0.3"
	Line1  = "1"
)

// Diagnostic codes of discovery (info).
const (
	codeNotProven  = "schema_model_not_proven"
	codeUnread     = "schema_model_unread"
	codeMigrations = "schema_model_migrations"
	codeFailure    = "schema_model_import_failure"
)

// migrationsMarker separates the file from the migrations directory in a
// schema_model_migrations message ("<file>: <marker><dir>").
const migrationsMarker = "typeorm migrations (not read): "

// listMarker ends a message about a group whose table list may be
// incomplete.
func listMarker(set string) string {
	return "; the table list of model group " + set + " may be incomplete"
}

// Package is one directory with a package.json whose dependencies or
// devDependencies name typeorm.
type Package struct {
	// Dir is the repository-relative package directory ("." for the root);
	// Manifest its package.json; Spec the typeorm range it declares.
	Dir, Manifest, Spec string
	// Versions are the locked versions of typeorm, typescript,
	// @nestjs/typeorm and typeorm-naming-strategies ("" when not proven);
	// Lockfile is the lockfile typeorm's version was read from.
	Versions map[string]string
	Lockfile string
	// Pins is {"typeorm": v, "typescript": v} when both are proven, else
	// nil (TO-V1 is still listed).
	Pins map[string]any
	// Line is the TypeORM major line whose rules apply: Line03 or Line1;
	// "" when the package is refused (Refused says why).
	Line    string
	Refused string
	// Drivers are the TypeORM driver packages the package depends on,
	// sorted (recorded; the dialect comes from the literal `type`).
	Drivers []string
	// Entities are the module-level entity classes of the package's files
	// (`@Entity`, `@ViewEntity` and `@ChildEntity` from typeorm, and
	// `@Entity`-named decorators of other modules), by file then source
	// order.
	Entities []*Entity
	// Groups are the package's data sources, ormconfig groups and NestJS
	// root modules, in file then source order.
	Groups []*Group
	// Features are the TypeOrmModule.forFeature calls of the package's
	// non-test files (TO-K4).
	Features []*Feature
}

// NestMajor is the major version of the locked @nestjs/typeorm, -1 when
// not proven.
func (p *Package) NestMajor() int {
	return lockfile.Major(p.Versions[pkgNest])
}

// Entity is one module-level class with an entity decorator.
type Entity struct {
	File string
	// Name is the class name ("" for an anonymous default export).
	Name  string
	Class *typescript.ModelStatement
	// Decorator is the entity decorator's expression; Form names it:
	// "Entity", "ViewEntity" or "ChildEntity". Typeorm says the decorator
	// resolves to the typeorm package's export (else row D: an `@Entity`
	// of another module).
	Decorator *typescript.ModelExpr
	Form      string
	Typeorm   bool
	// Exported says a directory glob reaches the class: `export class`,
	// `export default class`, `export { X }` or `export default X`.
	Exported bool
	// Test says the class is declared in a test file.
	Test bool
	// Flags are the compiler options of the file (TO-V3).
	Flags typescript.CompilerFlags
}

// Key is "<file>#<class>".
func (e *Entity) Key() string { return e.File + "#" + e.Name }

// Group is one model group: a data source, an ormconfig options object or
// a NestJS root module's data source.
type Group struct {
	// File is the file of the declaration or call; Root the group root
	// (the file, or "<file>#<name>" when the file holds several groups).
	File, Root string
	Span       contract.Span
	Package    *Package
	// Form is "datasource" (TO-K1), "ormconfig" (TO-K1), "forRoot" or
	// "forRootAsync" (TO-K3).
	Form string
	// Binding is the declared name of a data source ("" for `export
	// default new DataSource(...)`); Name is the NestJS data source name
	// ("default" unless stated) or an ormconfig element's name.
	Binding, Name string
	// Test says the data source or ormconfig file is a test file: the
	// group is read like any other and its tables carry `scope: test`, as
	// other front ends' test-file models do (Revision 3 addendum 3).
	Test bool
	// NameKnown is false when a NestJS data source name is not a literal:
	// forFeature calls cannot be attributed.
	NameKnown bool
	// Options are the candidate option objects, read (TO-K1 candidate
	// rule); OptionsWhy says why the group's options are not read (the
	// group then claims nothing and its tables are markers).
	Options    []*Options
	OptionsWhy string
	// Selector is "<file>:<line>" of the module-level function whose
	// returns are the candidates ("" when the options are not a call).
	Selector string
	// EntitiesAt is "<file>:<line>" of the entities array literal read.
	EntitiesAt string
	// Types are the candidates' literal `type` values in candidate order;
	// Dialects the dialects they map to, sorted; Dialect the single
	// dialect when there is one; Verification the verification per
	// dialect ("oracle" or "mapped", the weakest when two types map to
	// one dialect).
	Types        []string
	Dialects     []string
	Dialect      string
	Verification map[string]string
	// Failures are TO-P1 failures that make a dialect of the group fail
	// (1.x `type: "sqlite"`, failure (7)): dialect -> reason.
	Failures map[string]string
	// Refused says why the group is row D (a dialect outside the read
	// set, a custom naming strategy, decorators off): no claim.
	Refused string
	// Naming is "default" (TO-K2) or "snake" (TO-K6).
	Naming string
	// EntityPrefix and Schema are the scope options (empty when absent).
	EntityPrefix, Schema string
	// Entities are the reached entity classes (TO-K1 elements, TO-K4
	// forFeature lists), in reach order without repeats.
	Entities []*Entity
	// Incomplete lists the sites that may add tables to the group without
	// being read (an unread `entities` element, a glob outside the subset,
	// an unattributed forFeature call): the group's table list is
	// may_be_incomplete.
	Incomplete []string
	// AutoLoad is the literal autoLoadEntities option (TO-K4).
	AutoLoad bool
	// Migrations are the migrations option's patterns and directories
	// (TO-U1); HasMigrations says the option is present in some candidate
	// (TO-X4 then leaves generated names unknown).
	Migrations    []Migration
	HasMigrations bool
	// Synchronize records the candidates' literal synchronize and
	// migrationsRun values ("true", "false", "non-literal"), scope
	// evidence only.
	Synchronize, MigrationsRun []string
	// Spreads are the option spreads read as scope-neutral (TO-K8) or
	// through a registerAs namespace (TO-K5), "<file>:<line>".
	Spreads []string
}

// SetKey is the group's model group key.
func (g *Group) SetKey() string { return models.SetKey(Tool, g.Root) }

// Claimable reports a group whose tables the claim pass may claim.
func (g *Group) Claimable() bool {
	return g.OptionsWhy == "" && g.Refused == ""
}

// Migration is one element of a migrations option.
type Migration struct {
	// Pattern is the resolved repository-relative pattern ("" when the
	// element is not read; Why says why); Dir the directory before the
	// first wildcard; Files the script files the pattern matches.
	Pattern, Dir, Why string
	Files             int
	File              string
	Line              int
}

// Feature is one TypeOrmModule.forFeature call (TO-K4).
type Feature struct {
	File string
	Span contract.Span
	// Name is the data source name ("default" unless a literal second
	// argument names another); NameKnown is false for a non-literal one.
	Name      string
	NameKnown bool
	// Entities are the resolved entity classes; Unread the elements that
	// are not an identifier of an `@Entity` class.
	Entities []*Entity
	Unread   []string
}

// manifest is the part of a package.json discovery reads.
type manifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (m manifest) spec(name string) (string, bool) {
	if v, ok := m.Dependencies[name]; ok {
		return v, true
	}
	v, ok := m.DevDependencies[name]
	return v, ok
}

// driverPackages are the TypeORM driver packages recorded per package.
var driverPackages = []string{
	"pg", "sqlite3", "better-sqlite3", "mysql", "mysql2", "mariadb", "mssql", "sql.js",
	"oracledb", "@sap/hana-client", "mongodb", "@google-cloud/spanner", "@capacitor-community/sqlite",
	"expo-sqlite", "react-native-sqlite-storage", "nativescript-sqlite", "cordova-sqlite-storage",
}

// discovery holds the state of one Discover run.
type discovery struct {
	in        Input
	files     []string
	manifests []string
	diags     []contract.Diagnostic
	// registered caches the registerAs calls per package directory.
	registered map[string]map[string][]registration
	// reaches caches the import reach of root module files (TO-K4).
	reaches map[string]*importReach
	// apps caches appReach per root file, entries the package entry files.
	apps    map[string]*importReach
	entries map[*Package][]string
	// steps isolates each package's discovery and, in Link, each group,
	// entity and table step: a panic becomes a diagnostic.
	steps *models.Steps
}

func (d *discovery) diag(file, code, msg string) {
	d.diags = append(d.diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
}

// Discover finds every TypeORM package of the repository, its pins and
// major line, its entity classes, its data sources, ormconfig groups and
// NestJS root modules with their options, dialect set, naming strategy
// and reached entities, and its forFeature calls. Packages are sorted by
// directory. Diagnostics: schema_model_not_proven for a package or group
// without claims, schema_model_unread markers for forms found but not read
// (row D, entities outside every group, incomplete table lists),
// schema_model_migrations for the migrations option's directories (row
// TO-U1) and schema_model_import_failure for TO-P1 failures discovery
// proves (1.x `type: "sqlite"`).
func Discover(in Input) ([]*Package, []contract.Diagnostic) {
	d, pkgs := runDiscovery(in)
	return pkgs, append(d.diags, d.steps.Diagnostics...)
}

// runDiscovery runs discovery and keeps its state, which the claim pass
// uses to resolve names.
func runDiscovery(in Input) (*discovery, []*Package) {
	d := &discovery{in: in, registered: map[string]map[string][]registration{}, steps: &models.Steps{Hook: faultHook}}
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
		spec, ok := man.spec(pkgTypeORM)
		if !ok {
			continue
		}
		in.tag("TO-K1/package dependency")
		p := &Package{Dir: path.Dir(m), Manifest: m, Spec: spec, Versions: map[string]string{}}
		out = append(out, p)
		// One package's discovery failing never stops the others (Revision 4
		// C1): its groups are dropped and the failure is a diagnostic.
		done := false
		d.steps.Guard(m, "discovery", "TypeORM package "+m, func() {
			d.discoverPackage(p, man)
			done = true
		})
		if !done {
			p.Refused, p.Groups = "its discovery failed", nil
		}
	}
	return d, out
}

// discoverPackage reads one package: pins, drivers, entities, data
// sources, ormconfig, NestJS forms and the groups' reach.
func (d *discovery) discoverPackage(p *Package, man manifest) {
	m := p.Manifest
	in := d.in
	d.pins(p, man)
	for _, name := range driverPackages {
		if _, ok := man.spec(name); ok {
			p.Drivers = append(p.Drivers, name)
		}
	}
	sort.Strings(p.Drivers)
	if p.Refused != "" {
		in.tag("D")
		d.diag(m, codeNotProven, m+": "+p.Refused)
		return
	}
	d.entities(p)
	d.dataSources(p)
	if len(p.Groups) == 0 {
		d.ormconfig(p)
	}
	d.features(p)
	d.nestRoots(p)
	d.roots(p)
	for _, g := range p.Groups {
		d.finish(g)
	}
	d.unreached(p)
	if len(p.Groups) == 0 {
		d.diag(m, codeNotProven, m+": no TypeORM data source, ormconfig or TypeOrmModule root is found in this package; its entities are not read")
	}
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
		return !strings.HasSuffix(f, ".d.ts") && !strings.HasSuffix(f, ".d.mts") && !strings.HasSuffix(f, ".d.cts")
	}
	return false
}

func (d *discovery) isTest(file string) bool {
	return testscope.Classify(file, d.in.Tests) == testscope.Test || nestTestFile(file)
}

// nestTestFile is TO-K4's test-file rule: `*.spec.*`,
// `*.test.*`, `*.e2e-spec.*`, or a file under `test/` or `__tests__/`.
func nestTestFile(file string) bool {
	base := path.Base(file)
	return strings.Contains(base, ".spec.") || strings.Contains(base, ".test.") || strings.Contains(base, ".e2e-spec.") ||
		strings.Contains("/"+file, "/test/") || strings.Contains("/"+file, "/__tests__/")
}

// pins reads the locked versions (TO-V1) and the major line: 0.3.x and
// 1.x are read with their per-major rules (Revision 2 O12, Revision 3
// O14), any other version is refused.
func (d *discovery) pins(p *Package, man manifest) {
	v, lf, _ := lockfile.Locked(d.in.Sources, p.Dir, pkgTypeORM, p.Spec)
	p.Versions[pkgTypeORM] = v
	p.Lockfile = lf
	for _, name := range []string{pkgTypeScript, pkgNest, pkgNaming} {
		if spec, ok := d.specFor(p, man, name); ok {
			p.Versions[name], _, _ = lockfile.Locked(d.in.Sources, p.Dir, name, spec)
		}
	}
	if v != "" && p.Versions[pkgTypeScript] != "" {
		p.Pins = map[string]any{pkgTypeORM: v, pkgTypeScript: p.Versions[pkgTypeScript]}
		d.in.tag("TO-K1/lockfile pins")
	}
	shown := v
	if shown == "" {
		shown = p.Spec
	}
	major, minor, ok := versionLine(shown)
	switch {
	case !ok:
		p.Refused = "typeorm " + shown + ": the major version is not proven; the claim contract is verified for typeorm 0.3.x and 1.x only"
	case major == 0 && minor == 3:
		p.Line = Line03
	case major == 1:
		p.Line = Line1
	default:
		p.Refused = "typeorm " + shown + ": the claim contract is verified for typeorm 0.3.x and 1.x only; no claim is made for another major (row D)"
	}
}

// specFor returns the range a dependency is declared with: in the package
// manifest, else in the nearest manifest above the package that declares
// it (a workspace root's devDependencies).
func (d *discovery) specFor(p *Package, man manifest, name string) (string, bool) {
	if s, ok := man.spec(name); ok {
		return s, true
	}
	if p.Dir == "." {
		return "", false
	}
	for dir := path.Dir(p.Dir); ; dir = path.Dir(dir) {
		var up manifest
		if json.Unmarshal(d.in.Sources[path.Join(dir, "package.json")], &up) == nil {
			if s, ok := up.spec(name); ok {
				return s, true
			}
		}
		if dir == "." {
			return "", false
		}
	}
}

// versionLine reads the major and minor of a version or of a simple range
// ("0.3.31", "^0.3.31", "~1.1.2"); ok is false for anything else (open
// or several ranges, tags, URLs).
func versionLine(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "^~=v")
	if v == "" || strings.ContainsAny(v, " |<>*xX") {
		return 0, 0, false
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

// entityDecorators are the class decorators that make an entity class
// (G11's names).
var entityDecorators = map[string]bool{"Entity": true, "ViewEntity": true, "ChildEntity": true}

// entities records the package's module-level entity classes.
func (d *discovery) entities(p *Package) {
	for _, f := range d.packageFiles(p) {
		m := d.in.Models[f]
		if m == nil {
			continue
		}
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Kind != typescript.StatementClass {
				continue
			}
			for j := range s.DecoratorList {
				dec := &s.DecoratorList[j]
				callee := dec
				if callee.Kind == typescript.ModelCall {
					callee = callee.Callee
				}
				name := ""
				switch callee.Kind {
				case typescript.ModelIdentifier:
					name = callee.Name
				case typescript.ModelMember:
					name = callee.Name
				}
				if !entityDecorators[name] {
					continue
				}
				e := &Entity{File: f, Name: s.Name, Class: s, Decorator: dec, Form: name, Test: d.isTest(f)}
				e.Typeorm = d.isExternal(f, callee, pkgTypeORM, name)
				e.Exported = s.Exported || s.Default || d.exportedName(f, s.Name)
				e.Flags = d.in.Resolver.CompilerFlags(f)
				p.Entities = append(p.Entities, e)
				break
			}
		}
	}
}

// exportedName reports an export clause `export { name }` or `export {
// name as x }`, or `export default name`, of file.
func (d *discovery) exportedName(file, name string) bool {
	if name == "" {
		return false
	}
	for _, ec := range d.in.TypeScript[file].ExportClauses {
		if ec.Module == "" && !ec.Star && !ec.TypeOnly && ec.Name == name {
			return true
		}
	}
	m := d.in.Models[file]
	return m != nil && m.Default != nil && m.Default.Value.Kind == typescript.ModelIdentifier && m.Default.Value.Name == name
}

// entityOf returns the package entity class an identifier of file
// resolves to, or nil with a reason.
func (d *discovery) entityOf(p *Package, file string, e *typescript.ModelExpr) (*Entity, string) {
	if e.Kind != typescript.ModelIdentifier {
		return nil, "it is not an identifier"
	}
	b := d.local(file, e.Name, 0)
	if b.Class == nil {
		if b.Why != "" {
			return nil, b.Why
		}
		return nil, e.Name + " does not resolve to a class of the repository"
	}
	for _, en := range p.Entities {
		if en.Class == b.Class {
			return en, ""
		}
	}
	return nil, e.Name + " resolves to a class without an entity decorator, or of another package"
}

// finish computes a group's dialect and naming from its options, reads
// its entities and migrations, and reports what discovery found.
func (d *discovery) finish(g *Group) {
	if g.OptionsWhy == "" {
		d.dialect(g)
		d.naming(g)
		d.scope(g)
	}
	if g.OptionsWhy != "" {
		d.in.tag("D")
		d.diag(g.File, codeNotProven, fmt.Sprintf("%s:%d: the options of TypeORM %s %s are not read (%s); its entities are not claimed%s", g.File, g.Span.StartLine, g.Form, g.label(), g.OptionsWhy, listMarker(g.SetKey())))
		return
	}
	d.reach(g)
	d.migrations(g)
	if g.Refused == "" {
		d.decoratorFlags(g)
	}
	if g.Refused != "" {
		d.in.tag("D")
		d.diag(g.File, codeUnread, fmt.Sprintf("%s:%d: TypeORM %s %s is not read: %s (row D)", g.File, g.Span.StartLine, g.Form, g.label(), g.Refused))
	}
	for _, dialect := range sortedKeys(g.Failures) {
		d.diag(g.File, codeFailure, fmt.Sprintf("%s:%d: TO-P1 (7): %s; no table of model group %s is claimed on %s", g.File, g.Span.StartLine, g.Failures[dialect], g.SetKey(), dialect))
	}
	for _, x := range g.Incomplete {
		d.diag(g.File, codeUnread, x+listMarker(g.SetKey()))
	}
}

// label names a group in messages.
func (g *Group) label() string {
	switch {
	case g.Binding != "":
		return g.Binding
	case g.Form == "forRoot" || g.Form == "forRootAsync" || g.Form == "ormconfig":
		return "data source " + strconv.Quote(g.Name)
	}
	return "(default export)"
}

// sortedKeys returns m's keys in order, so diagnostics are deterministic.
func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// decoratorFlags applies TO-V3: TypeORM 0.3 and 1.x need legacy
// decorators, so an entity file whose tsconfig chain does not prove
// experimentalDecorators true makes the group D.
func (d *discovery) decoratorFlags(g *Group) {
	d.in.tag("TO-K1/tsconfig decorator flags")
	for _, e := range g.Entities {
		v, ok := e.Flags.Values["experimentalDecorators"]
		if ok && v != nil && *v {
			continue
		}
		why := "experimentalDecorators is not set"
		switch {
		case e.Flags.Config == "":
			why = "no tsconfig.json is found above it"
		case ok && v == nil:
			why = "experimentalDecorators is not a boolean literal in " + e.Flags.Config
		case ok:
			why = "experimentalDecorators is false in " + e.Flags.Config
		case len(e.Flags.Unresolved) > 0:
			why = "experimentalDecorators may be set by " + strings.Join(e.Flags.Unresolved, ", ") + ", which is not read"
		default:
			why = "experimentalDecorators is not set in the tsconfig chain of " + e.Flags.Config
		}
		g.Refused = fmt.Sprintf("entity file %s: %s (TO-V3: TypeORM needs legacy decorators)", e.File, why)
		return
	}
}

// unreached marks the package's typeorm `@Entity` classes that no
// claimable group reaches (TO-K1: not claimed), and the entity forms of
// row D (`@ViewEntity`, `@ChildEntity`, an `@Entity` of another module).
func (d *discovery) unreached(p *Package) {
	reached := map[*Entity]bool{}
	for _, g := range p.Groups {
		if g.Claimable() {
			for _, e := range g.Entities {
				reached[e] = true
			}
		}
	}
	for _, e := range p.Entities {
		line := e.Class.Span.StartLine
		switch {
		case !e.Typeorm:
			d.in.tag("D")
			d.diag(e.File, codeUnread, fmt.Sprintf("%s:%d: class %s has an @%s decorator that does not resolve to typeorm's; it is not read (row D)", e.File, line, e.Name, e.Form))
		case e.Form != "Entity":
			d.in.tag("D")
			d.diag(e.File, codeUnread, fmt.Sprintf("%s:%d: @%s class %s is not read (row D)", e.File, line, e.Form, e.Name))
		case !reached[e]:
			d.diag(e.File, codeUnread, fmt.Sprintf("%s:%d: entity class %s is not claimed: not in the data source's entities", e.File, line, e.Name))
		}
	}
}
