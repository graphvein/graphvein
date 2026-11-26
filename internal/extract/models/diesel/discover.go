// Package diesel is the Diesel 2.x front end of the model-defined schema
// (T018g4, ADR 0062). It reads, at link time and from the sources the
// scanner retained, the schema files that diesel.toml configures (and,
// names only, the schema.rs files no diesel.toml names), with a small
// token lexer: `table!`, `joinable!`, `allow_tables_to_appear_in_same_query!`
// and the `sql_types` module. No Rust extractor output and no cached
// payload are involved.
//
// This file holds discovery (row DI-K1): the diesel.toml sections, the
// Cargo package that owns each schema file, its Diesel pin and the
// workspace dialect, and the migrations marker (row DI-U1).
package diesel

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// Tool is the model group tool name ("models/diesel/<schema file>").
const Tool = "diesel"

// Assumption ids (T018g4 "Environment", Revision 1). Discovery attaches
// the ones row DI-K1 names; later rows add theirs.
const (
	// AssumePins: the Cargo.lock diesel version is the one the code is
	// built with.
	AssumePins = "DI-V1"
	// AssumeGenerated: a generated schema file is current print-schema
	// output with only its patch applied.
	AssumeGenerated = "DI-V2"
	// AssumeModifiers: no column's type is a domain (narrowed by O4).
	AssumeModifiers = "DI-V3"
	// AssumeDialect: the database is of the dialect taken from the
	// workspace's Diesel backend features (O3).
	AssumeDialect = "DI-V4"
	// AssumeSettings: the file was generated with the [print_schema]
	// section that names it as it stands.
	AssumeSettings = "DI-V5"
	// AssumeCLI: the diesel_cli that wrote the file follows the
	// print-schema rules verified at the versions the contract names.
	AssumeCLI = "DI-V6"
	// AssumeUnconfigured: a schema.rs no diesel.toml names is not
	// configured by diesel.toml and may be stale (O2).
	AssumeUnconfigured = "DI-V7"
	// AssumeNewerPin: the diesel pin is newer than the diesel_cli versions
	// whose print-schema rules are verified (CLIVerified), and the newer
	// diesel_cli is assumed to write the same forms (Revision 1 O7,
	// addendum 4).
	AssumeNewerPin = "DI-V8"
	// AssumeOlderPin: the diesel pin is older than the diesel_cli versions
	// whose print-schema rules are verified, and the older diesel_cli
	// output was checked for the rules OlderCLIChecked lists (Revision 2,
	// A1).
	AssumeOlderPin = "DI-V9"
)

// Diagnostic codes of the Diesel front end.
const (
	codeNotProven     = "schema_model_not_proven"
	codeUnread        = "schema_model_unread"
	codeMigrations    = "schema_model_migrations"
	codeImportFailure = "schema_model_import_failure"
)

// MigrationsMarker separates the diesel.toml path from the directory in a
// schema_model_migrations message ("<diesel.toml>: <marker><dir>"), the
// shape the other front ends' markers have, so a server rule can read the
// directory back (row DI-U1).
const MigrationsMarker = "Diesel migrations directory (not read): "

// Input is what the Diesel front end reads: the retained sources
// (repository-relative paths).
type Input struct {
	Sources map[string][]byte
	// Tag reports a contract row ("<row>" or "<row>/<form>") to the
	// contract test; nil outside tests.
	Tag func(row string)
	// Migrations and Edges are the migration pass's records, for the
	// model kit's comparison with migration sets (Link only).
	Migrations []contract.Node
	Edges      []contract.Edge
}

func (in Input) tag(row string) {
	if in.Tag != nil {
		in.Tag(row)
	}
}

// Discovery is what discovery found.
type Discovery struct {
	// Configs are every diesel.toml, sorted by path.
	Configs []*Config
	// Files are the schema files to read, one model group each, sorted
	// by path.
	Files []*File
}

// File is one schema file to read: a file a diesel.toml section names
// (Configured), or a schema.rs in a Diesel package that none names
// (names only, O2).
type File struct {
	Path       string
	Configured bool
	// Config and Section are the diesel.toml section naming the file
	// (the first by config path and key when several do); nil when not
	// configured.
	Config  *Config
	Section *Section
	// Manifest is the owning package's Cargo.toml; Requirement its
	// diesel requirement; Version the locked diesel version ("" when not
	// proven, with PinReason).
	Manifest, Requirement, Version, PinReason string
	// Pins is {"diesel": Version}, nil when the version is not proven
	// (DI-V1 is listed all the same).
	Pins map[string]any
	// Dialect is postgres, mysql, sqlite or unknown (DialectReason says
	// why).
	Dialect, DialectReason string
	// Assumptions are the discovery assumptions of the group.
	Assumptions []string
}

// MigrationDirs counts the migration directories of a config's
// migrations directory: direct subdirectories holding an up.sql, dot
// entries skipped (migrations_internals 2.3.0 lib.rs:71-83).
func MigrationDirs(sources map[string][]byte, dir string) int {
	if dir == "" {
		return 0
	}
	prefix := dir + "/"
	if dir == "." {
		prefix = ""
	}
	seen := map[string]bool{}
	for f := range sources {
		rest, ok := strings.CutPrefix(f, prefix)
		if !ok {
			continue
		}
		name, file, ok := strings.Cut(rest, "/")
		if ok && file == "up.sql" && !strings.HasPrefix(name, ".") {
			seen[name] = true
		}
	}
	return len(seen)
}

// Discover finds the diesel.toml files, their sections and the schema
// files to read, with each file's package, pin and dialect. Every config
// or section that yields no group gets an info diagnostic (row DI-K1);
// each migrations directory with migrations gets the DI-U1 marker.
func Discover(in Input) (*Discovery, []contract.Diagnostic) {
	var diags []contract.Diagnostic
	diag := func(file, code, msg string) {
		diags = append(diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: msg})
	}
	paths := make([]string, 0, len(in.Sources))
	for p := range in.Sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	manifests := map[string]manifest{}
	for _, p := range paths {
		if path.Base(p) == "Cargo.toml" {
			manifests[p] = readManifest(p, in.Sources[p])
		}
	}
	d := &Discovery{}
	named := map[string]*File{}
	for _, p := range paths {
		if path.Base(p) != "diesel.toml" {
			continue
		}
		c := ReadConfig(p, in.Sources[p])
		d.Configs = append(d.Configs, c)
		if c.Problem != "" {
			in.tag("DI-K1")
			diag(p, codeNotProven, fmt.Sprintf("%s: %s; no schema file of it is read", p, c.Problem))
			continue
		}
		if n := MigrationDirs(in.Sources, c.Migrations); n > 0 {
			in.tag("DI-U1")
			diag(p, codeMigrations, p+": "+MigrationsMarker+c.Migrations)
		} else if c.MigrationsWritten && c.Migrations == "" {
			diag(p, codeUnread, p+": migrations_directory.dir cannot be placed in the repository; it is not read")
		}
		for _, s := range c.Sections {
			label := sectionLabel(s)
			switch {
			case s.Problem != "":
				diag(p, codeNotProven, fmt.Sprintf("%s: %s; it configures no schema file", p, s.Problem))
				continue
			case s.File == "":
				if s.Written {
					diag(p, codeNotProven, fmt.Sprintf("%s: %s has no file (print-schema writes to standard output); it configures no schema file", p, label))
				}
				continue
			case s.Path == "":
				diag(p, codeNotProven, fmt.Sprintf("%s: %s file %q cannot be placed in the repository", p, label, s.File))
				continue
			case in.Sources[s.Path] == nil:
				diag(p, codeNotProven, fmt.Sprintf("%s: %s file %s does not exist", p, label, s.Path))
				continue
			}
			if f := named[s.Path]; f != nil {
				diag(p, codeUnread, fmt.Sprintf("%s: %s names %s, which %s %s already configures; the file is read once, with the first section", p, label, s.Path, f.Config.Path, sectionLabel(f.Section)))
				continue
			}
			named[s.Path] = &File{Path: s.Path, Configured: true, Config: c, Section: s}
		}
	}
	for _, p := range sortedKeys(named) {
		f := named[p]
		if why := ownPackage(in.Sources, manifests, f); why != "" {
			in.tag("DI-K1")
			diag(f.Config.Path, codeNotProven, fmt.Sprintf("%s: %s names %s, but %s; it is not read", f.Config.Path, sectionLabel(f.Section), f.Path, why))
			continue
		}
		in.tag("DI-K1")
		d.Files = append(d.Files, f)
	}
	// O2: schema.rs files in Diesel packages that no diesel.toml names.
	for _, p := range paths {
		if path.Base(p) != "schema.rs" || named[p] != nil {
			continue
		}
		hasTable, lexErr := holdsTable(in.Sources[p])
		if !hasTable {
			continue
		}
		f := &File{Path: p}
		why := ownPackage(in.Sources, manifests, f)
		switch {
		case f.Manifest == "" || strings.HasPrefix(why, "the package does not depend"):
			// Not a Diesel package: the file is silent D.
			continue
		case why != "":
			diag(p, codeUnread, fmt.Sprintf("%s: a schema.rs with table! that no diesel.toml names; it is not read: %s", p, why))
			continue
		case lexErr != nil:
			diag(p, codeUnread, fmt.Sprintf("%s: a schema.rs with table! that no diesel.toml names could not be read (%v)", p, lexErr))
			continue
		}
		f.Assumptions = append(f.Assumptions, AssumeUnconfigured)
		in.tag("DI-U2")
		d.Files = append(d.Files, f)
	}
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Path < d.Files[j].Path })
	return d, diags
}

func sectionLabel(s *Section) string {
	if s.Key == "default" && s.Written {
		return "[print_schema]"
	}
	return "[print_schema." + s.Key + "]"
}

// holdsTable reports a file with at least one table! item at any module
// depth. A file the lexer refuses holds one when its text contains
// "table!" (the error is returned).
func holdsTable(src []byte) (bool, error) {
	trees, err := LexTrees(string(src))
	if err != nil {
		return strings.Contains(string(src), "table!"), err
	}
	var walk func(items []Item) bool
	walk = func(items []Item) bool {
		for _, it := range items {
			if it.Kind == ItemTable || it.Kind == ItemModule && walk(it.Items) {
				return true
			}
		}
		return false
	}
	return walk(ReadItems(trees)), nil
}

// ownPackage fills the file's package, pin, dialect and discovery
// assumptions, or returns why the file is not read: no owning package,
// a package that does not declare diesel, a Diesel major other than 2.
func ownPackage(sources map[string][]byte, manifests map[string]manifest, f *File) string {
	var m manifest
	found := false
	for dir := path.Dir(f.Path); ; dir = path.Dir(dir) {
		p := path.Join(dir, "Cargo.toml")
		if cand, ok := manifests[p]; ok {
			if cand.problem != "" {
				f.Manifest = p
				return "the manifest " + p + " could not be read (" + cand.problem + ")"
			}
			if cand.pkg {
				m, found = cand, true
				break
			}
		}
		if dir == "." || dir == "/" || dir == "" {
			break
		}
	}
	if !found {
		return "no Cargo package owns it"
	}
	f.Manifest = m.path
	root := workspaceRoot(m, manifests)
	dep := readDiesel(m, root)
	if dep.problem != "" {
		return "the diesel dependency of " + m.path + " could not be read (" + dep.problem + ")"
	}
	if len(dep.entries) == 0 {
		return "the package does not depend on diesel (" + m.path + ")"
	}
	for _, e := range dep.entries {
		if e.requirement != "" {
			f.Requirement = e.requirement
			break
		}
	}
	lockDir := path.Dir(m.path)
	if root != nil {
		lockDir = path.Dir(root.path)
	}
	lock := readLock(sources, path.Join(lockDir, "Cargo.lock"))
	f.Version, f.PinReason = lockPin(lock, f.Requirement)
	major := requirementMajor(f.Requirement)
	if f.Version != "" {
		major = versionMajor(f.Version)
		f.Pins = map[string]any{"diesel": f.Version}
	}
	switch {
	case major < 0:
		return "the Diesel major version is not proven (" + f.PinReason + "; requirement " + fmt.Sprintf("%q", f.Requirement) + ")"
	case major != 2:
		return fmt.Sprintf("Diesel %d: the claim contract is verified for Diesel 2.x only", major)
	}
	// The workspace: every package whose workspace root is the same.
	key := m.path
	if root != nil {
		key = root.path
	}
	var members []manifest
	var unread []string
	for _, p := range sortedKeys(manifests) {
		cand := manifests[p]
		if cand.problem != "" {
			if root != nil && within(p, path.Dir(root.path)) {
				unread = append(unread, p)
			}
			continue
		}
		if !cand.pkg {
			continue
		}
		candKey := cand.path
		if r := workspaceRoot(cand, manifests); r != nil {
			candKey = r.path
		}
		if candKey == key {
			members = append(members, cand)
		}
	}
	f.Dialect, f.DialectReason = workspaceDialect(members, unread, manifests, lock)
	f.Assumptions = []string{AssumePins}
	if f.Dialect != "unknown" {
		f.Assumptions = append(f.Assumptions, AssumeDialect)
	}
	if f.Configured {
		f.Assumptions = append(f.Assumptions, AssumeSettings)
	}
	return ""
}

// within reports a path inside dir ("." holds every path).
func within(p, dir string) bool {
	return dir == "." || strings.HasPrefix(p, dir+"/")
}
