package gorm

import (
	"fmt"
	"path"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
)

// Input is what the GORM front end reads at link time: the facts of every
// file (Go files carry imports, calls and the Go model payload) and the
// retained sources (go.mod files).
type Input struct {
	Files   map[string]facts.Facts
	Sources map[string][]byte
	// Tag reports a contract row ("<row>" or "<row>/<form>") to the
	// contract test; nil outside tests.
	Tag func(row string)
	// Migrations and Edges are the migration pass's records, which the
	// kit compares model tables with (never merged).
	Migrations []contract.Node
	Edges      []contract.Edge
}

func (in Input) tag(row string) {
	if in.Tag != nil {
		in.Tag(row)
	}
}

// The import paths discovery reads.
const (
	pkgGorm       = "gorm.io/gorm"
	pkgSchema     = "gorm.io/gorm/schema"
	pkgDrivers    = "gorm.io/driver/"
	pkgGlebarez   = "github.com/glebarez/sqlite"
	pkgInflection = "github.com/jinzhu/inflection"
	pkgGormV1     = "github.com/jinzhu/gorm"
)

// The gorm versions the rules were verified at (open point O5, Revision 1):
// both ends of the range ran the oracle. A newer v1 version is read under
// GO-V1 with VersionAssumption shown; an older one is not read.
var (
	verifiedLow  = [3]int{1, 25, 12}
	verifiedHigh = [3]int{1, 31, 2}
)

// VersionAssumption is the note a module read above the verified range
// carries (Revision 1, O5), shown with GO-V1 on its records.
const VersionAssumption = "rules verified at gorm 1.25.12-1.31.2"

// Version status of a module's gorm.io/gorm pin.
const (
	VersionVerified = "verified" // inside the verified range
	VersionAssumed  = "assumed"  // a newer v1: read under VersionAssumption
	VersionRefused  = "refused"  // older v1, another major or no exact pin
)

// Diagnostic codes of discovery (info).
const (
	codeNotProven = "schema_model_not_proven"
	codeUnread    = "schema_model_unread"
)

// ListMarker is the suffix of a diagnostic about a site that may add tables
// to model group set without being read: the server then marks the group's
// table_list may_be_incomplete whatever file the diagnostic is on (the
// suffix the Drizzle and Sequelize front ends use).
func ListMarker(set string) string {
	return "; the table list of model group " + set + " may be incomplete"
}

// Module is one Go module whose go.mod requires gorm.io/gorm: one model
// group (row GO-K1).
type Module struct {
	// Dir is the module directory ("." for the repository root), GoMod its
	// go.mod, Path the module path.
	Dir, GoMod, Path string
	// Requires are the go.mod require lines of the pinned modules
	// (gorm.io/gorm, gorm.io/driver/*, glebarez/sqlite, inflection) in file
	// order.
	Requires []lockfile.GoRequire
	// Pins maps each pinned module to its version, or nil when the go.mod
	// does not prove it (missing, or replaced by another path).
	Pins map[string]any
	// Version is the gorm.io/gorm version; VersionStatus is
	// VersionVerified, VersionAssumed or VersionRefused.
	Version, VersionStatus string
	// Refused says why the module's group claims nothing (empty when it is
	// read): a refused version, an unsupported dialector, no gorm.Open site.
	Refused string
	// Files are the module's read files: the non-test Go files of its own
	// packages that golang.Package selects, sorted.
	Files []string
	// Opens are the gorm.Open sites of the read files in file order.
	Opens []*OpenSite
	// Dialects are the distinct dialects of the supported Open sites,
	// sorted; Dialect is the only one, or "" for a runtime group (several,
	// GO-V3) or when no site is supported.
	Dialects []string
	Dialect  string
	// Config is the configuration every Open site agrees on (a key the
	// sites disagree on, or that a mutation changes, is unknown); MySQL the
	// mysql dialector's DDL settings the mysql sites agree on.
	Config Config
	MySQL  MySQLConfig
	// InflectionChanged lists the inflection.Add* call sites
	// ("<file>:<line>"): plural-derived names are then unknown (GO-K4).
	InflectionChanged []string
	// Sites are the migrate sites of the read files in file order.
	Sites []*MigrateSite
	// Models are the structs the proven sites list (row GO-M1), first
	// listing first. Auto-added tables (GO-K3's ReorderModels) are the
	// claim pass's (GB4).
	Models []*Model
	// Expansions are the call statements the order rule expands.
	Expansions []Expansion
	// Sequences are the migrate sites in statement order, one per root
	// function (the order rule); OrderProven holds when there is one
	// sequence and every site is in it.
	Sequences   [][]*MigrateSite
	OrderProven bool
	// Incomplete lists the sites that may add tables without being read
	// ("<file>:<line>: <why>"): the group's table_list is then
	// may_be_incomplete.
	Incomplete []string
	// JoinSetups are the SetupJoinTable calls of the read files (T018g5
	// Revision 2, A2): the join table of each relation they name is not
	// claimed (row D).
	JoinSetups []JoinSetup
	// Changes are the Migrator DDL calls of the read files (Revision 2, A3):
	// the tables they may change keep partial sets.
	Changes []SchemaChange
	// DriverNotes lists the dialector driver pins outside the verified
	// driver versions (Revision 2, A7), shown with GO-V1; OldPostgresNaming
	// marks a postgres driver older than v1.5.6, whose dialector does not
	// set the 63-byte identifier limit (names longer than 63 bytes are
	// then unknown).
	DriverNotes       []string
	OldPostgresNaming bool
	// RawDDL lists the Exec and Raw calls whose literal text is DDL
	// ("<file>:<line>"; also in Incomplete).
	RawDDL []string
	// RawChanges are the statements of that text that may change a
	// claimed table (Revision 2 addendum).
	RawChanges []RawChange
	// res keeps the module's resolver for the claim pass.
	res *resolver
}

// SetKey is the module's model group key.
func (m *Module) SetKey() string { return models.SetKey(Tool, m.Dir) }

// Model is one struct a proven migrate site lists.
type Model struct {
	// File declares the struct; Dir is its package directory.
	File, Dir string
	Struct    *facts.GoStruct
}

// Identity is the table identity of the model (row GO-M1).
func (m *Model) Identity() string { return "table:" + m.File + "#" + m.Struct.Name }

// Discover finds every Go module that requires gorm.io/gorm, its pins and
// version, its gorm.Open sites with their dialect and configuration, its
// migrate sites with the receiver proof and order, and the models they
// list. Modules are sorted by directory. Diagnostics: schema_model_not_proven
// for a module that claims nothing and for a model no read migrate call
// lists (GO-M2), schema_model_unread for what is found but not read (row D,
// each with the group's ListMarker, and the GORM v1 import of row GO-U1).
func Discover(in Input) ([]*Module, []contract.Diagnostic) {
	d := &discovery{in: in, modules: golang.ModulePaths(in.Sources)}
	d.unreadV1()
	var out []*Module
	// A failure while reading one module never stops the others (C1 of
	// T018g5 Revision 2): it becomes a schema_model_internal_error
	// diagnostic on the module's go.mod, and the module is skipped.
	steps := &models.Steps{Hook: faultHook}
	for _, dir := range sortedKeys(d.modules) {
		goMod := path.Join(dir, "go.mod")
		var m *Module
		kept, done := len(d.diags), false
		steps.Guard(goMod, "discover", "GORM module "+dir, func() {
			mod := lockfile.ParseGoMod(in.Sources[goMod])
			m = d.module(dir, goMod, mod)
			done = true
		})
		if !done {
			// Drop what the failed module had emitted.
			d.diags, m = d.diags[:kept], nil
		}
		d.pending = nil
		if m == nil {
			continue
		}
		out = append(out, m)
	}
	return out, append(d.diags, steps.Diagnostics...)
}

// discovery holds the state of one Discover run.
type discovery struct {
	in      Input
	modules map[string]string
	diags   []contract.Diagnostic
	// pending are the current module's row D markers, emitted only when the
	// module is read (a refused module's group has no table list to mark).
	pending []contract.Diagnostic
}

func (d *discovery) diag(file, code, msg string) {
	d.diags = append(d.diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
}

// unread records a site that may add tables to m's group without being
// read (row D): a marker diagnostic with the group's list marker, and the
// group's table list may be incomplete.
func (d *discovery) unread(m *Module, file string, line int, why string) {
	at := fmt.Sprintf("%s:%d", file, line)
	m.Incomplete = append(m.Incomplete, at+": "+why)
	d.in.tag("D")
	d.pending = append(d.pending, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: codeUnread, Message: at + ": " + why + ListMarker(m.SetKey())})
}

// unreadV1 marks every non-test Go file that imports GORM v1 (row GO-U1):
// found, not read.
func (d *discovery) unreadV1() {
	for _, p := range facts.Paths(d.in.Files) {
		if !readable(p) {
			continue
		}
		for _, imp := range d.in.Files[p].Imports {
			if imp.Module == pkgGormV1 || strings.HasPrefix(imp.Module, pkgGormV1+"/") {
				d.in.tag("GO-U1")
				d.diag(p, codeUnread, fmt.Sprintf("%s:%d: %s import (GORM v1): found, not read", p, imp.Span.StartLine, imp.Module))
			}
		}
	}
}

// readable reports a Go file GORM discovery reads: not a test file, and in
// a directory the go command builds (not testdata, vendor, or a directory
// starting with "." or "_"), assumption GO-V7.
func readable(p string) bool {
	if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
		return false
	}
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "testdata" || seg == "vendor" || seg != "." && (strings.HasPrefix(seg, ".") || strings.HasPrefix(seg, "_")) {
			return false
		}
	}
	return true
}

// module reads one go.mod: nil when it does not require gorm.io/gorm.
func (d *discovery) module(dir, goMod string, mod lockfile.GoMod) *Module {
	m := &Module{Dir: dir, GoMod: goMod, Path: mod.Module, Pins: map[string]any{}}
	gorm := false
	for _, r := range mod.Requires {
		if pinned(r.Path) {
			m.Requires = append(m.Requires, r)
		}
		if r.Path == pkgGorm {
			gorm = true
		}
	}
	if !gorm {
		return nil
	}
	d.in.tag("GO-K1")
	d.pending = nil
	d.pins(m, mod)
	if !readable(path.Join(dir, "x.go")) {
		// A module inside testdata, vendor or a dot or underscore
		// directory is not built by the go command (GO-V7); its files are
		// not read (an index of a repository that keeps such modules as
		// fixtures, like this one's testdata).
		d.in.tag("D")
		m.Refused = "the module lies in a directory the go command does not build (testdata, vendor, or a name starting with . or _)"
		d.diag(goMod, codeNotProven, fmt.Sprintf("%s: GORM module %s is not read: %s", goMod, m.Path, m.Refused))
		return m
	}
	r := &resolver{d: d, m: m}
	m.res = r
	// The read files are the package files the go command builds, as the
	// Go structure contract selects them (golang.Package, ADR 0068): a file
	// no build configuration selects (`//go:build ignore`, another port's
	// file name, a cgo file without cgo, `package documentation`) is not
	// part of the package and is skipped, as go/build skips it. A package
	// that cannot be decided reads nothing (row D on each of its files).
	var refused []string
	for _, p := range facts.Paths(d.in.Files) {
		if base, _ := golang.ModuleAt(p, d.modules); base != dir || !readable(p) {
			continue
		}
		pkg := r.goPackage(path.Dir(p))
		switch {
		case pkg.Refusal != "":
			refused = append(refused, p)
		case slices.Contains(pkg.Files, p):
			m.Files = append(m.Files, p)
		}
	}
	for _, p := range refused {
		if d.in.Files[p].Go == nil {
			d.unread(m, p, 1, "the Go model payload of this file is missing: its models and migrate sites are not read")
			continue
		}
		d.unread(m, p, 1, "the go command's package in "+path.Dir(p)+" cannot be decided (a build constraint not read, a file Go rejects or without a payload, several packages): its models and migrate sites are not read")
	}
	for _, p := range m.Files {
		r.file(p)
	}
	r.mergeConfig()
	d.driverPins(m)
	r.order()
	d.notProven(m, r)
	switch {
	case m.Refused != "":
	case len(m.Opens) == 0:
		d.in.tag("D")
		m.Refused = "no gorm.Open site is read, so the dialect and the configuration are unknown"
	}
	if m.Refused != "" {
		d.diag(goMod, codeNotProven, fmt.Sprintf("%s: GORM module %s is not read: %s", goMod, m.Path, m.Refused))
	} else {
		d.diags = append(d.diags, d.pending...)
	}
	d.pending = nil
	return m
}

// pinned reports a module whose version the records pin (GO-V1).
func pinned(p string) bool {
	return p == pkgGorm || p == pkgInflection || p == pkgGlebarez || strings.HasPrefix(p, pkgDrivers)
}

// pins fills the module's pins and gorm version status from its require
// and replace lines.
func (d *discovery) pins(m *Module, mod lockfile.GoMod) {
	m.Pins[pkgGorm], m.Pins[pkgInflection] = nil, nil
	for _, r := range m.Requires {
		m.Pins[r.Path] = r.Version
	}
	replacedPath := map[string]bool{}
	for _, rep := range mod.Replaces {
		if !pinned(rep.Path) {
			continue
		}
		if v, ok := m.Pins[rep.Path]; ok && v != nil && rep.Version != "" && rep.Version != v {
			continue
		}
		if rep.NewPath == rep.Path && rep.NewVersion != "" {
			m.Pins[rep.Path] = rep.NewVersion
			continue
		}
		m.Pins[rep.Path] = nil
		replacedPath[rep.Path] = true
	}
	v, _ := m.Pins[pkgGorm].(string)
	m.Version = v
	switch {
	case replacedPath[pkgGorm]:
		m.VersionStatus = VersionRefused
		m.Refused = "gorm.io/gorm is replaced by another module path, so the gorm code that migrated the database is not the pinned one"
	default:
		m.VersionStatus = versionStatus(v)
		if m.VersionStatus == VersionRefused {
			m.Refused = "gorm.io/gorm " + v + " is outside the read range (v1.25.12 or later v1)"
		}
	}
}

// verifiedDrivers are the dialector driver versions the oracle ran (the
// investigation: postgres v1.5.11 and v1.6.3, gorm.io/driver/sqlite v1.5.7 and v1.6.0,
// glebarez/sqlite v1.11.0) or the mysql mapping was made from (v1.6.0,
// GO-V5). T018g5 Revision 2, A7.
var verifiedDrivers = map[string][]string{
	pkgDrivers + "postgres": {"v1.5.11", "v1.6.3"},
	pkgDrivers + "sqlite":   {"v1.5.7", "v1.6.0"},
	pkgGlebarez:             {"v1.11.0"},
	pkgDrivers + "mysql":    {"v1.6.0"},
}

// verifiedDriverText names the verified driver versions in notes.
const verifiedDriverText = "gorm.io/driver/postgres v1.5.11, v1.6.3; gorm.io/driver/sqlite v1.5.7, v1.6.0; github.com/glebarez/sqlite v1.11.0; gorm.io/driver/mysql v1.6.0"

// postgresApply is the first gorm.io/driver/postgres version whose
// dialector sets the 63-byte identifier limit (postgres.go Apply).
var postgresApply = [3]int{1, 5, 6}

// driverPins lists the dialector drivers of the module's gorm.Open sites
// whose pin is not a verified driver version (shown with GO-V1), and marks
// a postgres driver older than v1.5.6 (Revision 2, A7).
func (d *discovery) driverPins(m *Module) {
	used := map[string]bool{}
	for _, o := range m.Opens {
		if o.Dialect != "" {
			used[o.Driver] = true
		}
	}
	for _, p := range sortedKeys(map[string]string{pkgDrivers + "postgres": "", pkgDrivers + "sqlite": "", pkgGlebarez: "", pkgDrivers + "mysql": ""}) {
		v, pinned := m.Pins[p]
		if !used[p] && !pinned {
			continue
		}
		ver, _ := v.(string)
		switch {
		case ver == "":
			m.DriverNotes = append(m.DriverNotes, p+" is not pinned by the module's go.mod (verified driver versions: "+verifiedDriverText+")")
		case !slices.Contains(verifiedDrivers[p], ver):
			m.DriverNotes = append(m.DriverNotes, p+" "+ver+" is not a verified driver version ("+verifiedDriverText+")")
		}
		if p == pkgDrivers+"postgres" {
			n, pre, ok := semver(ver)
			if !ok || less(n, pre, postgresApply) {
				m.OldPostgresNaming = true
			}
		}
	}
}

// versionStatus classifies a gorm.io/gorm version (Revision 1, O5).
func versionStatus(v string) string {
	n, pre, ok := semver(v)
	switch {
	case !ok || n[0] != 1 || less(n, pre, verifiedLow):
		return VersionRefused
	case pre == "" && !lessPlain(verifiedHigh, n):
		return VersionVerified
	default:
		return VersionAssumed
	}
}

// semver splits "vMAJOR.MINOR.PATCH[-pre][+build]".
func semver(v string) ([3]int, string, bool) {
	var n [3]int
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return n, "", false
	}
	rest, _, _ = strings.Cut(rest, "+")
	core, pre, _ := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return n, "", false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || len(p) > 1 && p[0] == '0' {
			return n, "", false
		}
		n[i] = x
	}
	return n, pre, true
}

// less reports whether version n (with prerelease pre) is below b.
func less(n [3]int, pre string, b [3]int) bool {
	if n == b {
		return pre != ""
	}
	return lessPlain(n, b)
}

func lessPlain(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// notProvenMessage is the GO-M2 marker of a struct; the claim pass drops
// it for a struct a migrate call reaches through GO-K3's auto-add.
func notProvenMessage(file string, line int, name string) string {
	return fmt.Sprintf("%s:%d: struct %s has gorm tags or embeds gorm.Model, but no read migrate call lists it (model, not proven migrated)", file, line, name)
}

// notProven marks the structs of the module that carry gorm tags or embed
// gorm.Model but that no proven migrate site lists, directly or as an
// embedded struct of a listed model (row GO-M2: "model, not proven
// migrated"). The claim pass drops the marker of a struct GO-K3's
// auto-add reaches (Link).
func (d *discovery) notProven(m *Module, r *resolver) {
	reached := map[string]bool{}
	var embed func(file string, s *facts.GoStruct)
	embed = func(file string, s *facts.GoStruct) {
		key := file + "#" + s.Name
		if reached[key] {
			return
		}
		reached[key] = true
		for _, f := range s.Fields {
			if !f.Embedded {
				continue
			}
			if mf, ms := r.structOf(file, f.Type); ms != nil {
				embed(mf, ms)
			}
		}
	}
	for _, mo := range m.Models {
		embed(mo.File, mo.Struct)
	}
	// A SetupJoinTable join model is the join table's model (Revision 2,
	// A2): not "not proven migrated", though its table is not claimed.
	for _, j := range m.JoinSetups {
		if j.Join != nil {
			embed(j.Join.File, j.Join.Struct)
		}
	}
	for _, p := range m.Files {
		g := d.in.Files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Structs {
			s := &g.Structs[i]
			if reached[p+"#"+s.Name] || !r.gormModel(p, s) {
				continue
			}
			d.in.tag("GO-M2")
			d.diag(p, codeNotProven, notProvenMessage(p, s.Line, s.Name))
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// gormModel reports a struct with a gorm tag or a gorm.Model embedding.
func (r *resolver) gormModel(file string, s *facts.GoStruct) bool {
	for _, f := range s.Fields {
		if _, ok := reflect.StructTag(f.Tag).Lookup("gorm"); ok {
			return true
		}
		if f.Embedded && f.Type.Kind == facts.GoSelector && f.Type.Name == "Model" && r.importOf(file, f.Type.Object) == pkgGorm {
			return true
		}
	}
	return false
}
