package golang

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"go/build/constraint"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds the Go structural claim contract (docs/lang/go-structure.md,
// ADR 0068): the row table, the build-constraint reader and the package file
// selection that import edges use. TestGoStructureContract keeps
// structureRows, the document's tables and the fixture
// testdata/fixtures/go-structure 1:1; TestGoStructureOracle compares every
// claimed fact with go/parser, go/ast and go/build.

// structureRow is one row of the Go structural claim contract.
type structureRow struct {
	// ID is the row id in docs/lang/go-structure.md.
	ID string
	// Claim says in a few words what the row claims ("nothing" for a
	// refusal row).
	Claim string
}

// structureRows is the Go side of the contract tables, in document order.
// "D" is the single default branch: every form no row reads.
var structureRows = []structureRow{
	{"GS-M1", "the importer's module: nearest scanned go.mod with a module directive"},
	{"GS-F1", "file record and file node"},
	{"GS-F2", "nothing beyond the file: declarations and imports go to D"},
	{"GS-F3", "nothing beyond the file: declarations and imports go to D (lone CR)"},
	{"GS-D1", "function node and contains edge"},
	{"GS-D2", "function init, init@2, ... and contains edge"},
	{"GS-D3", "function _, _@2, ... and contains edge"},
	{"GS-D4", "method node T.Name and contains edge"},
	{"GS-D5", "struct, interface, type or type_alias node and contains edge"},
	{"GS-D6", "nothing: both declarations go to D"},
	{"GS-D7", "exported variable node and contains edge"},
	{"GS-D8", "exported constant node and contains edge"},
	{"GS-I1", "imports edges to the imported package's files"},
	{"GS-I2", "no edge; unresolved_import warning"},
	{"GS-I3", "external edge only; the directory goes to D"},
	{"GS-I4", "imports edge to external:.#go:<path>"},
	{"GS-I5", "nothing: the import goes to D"},
	{"D", "nothing; in-scope forms counted in go_structure_unread"},
}

// Default-branch forms, the keys of facts.GoFacts.Unread and of the
// go_structure_unread diagnostic.
const (
	unreadSyntaxError    = "syntax_error"
	unreadReceiver       = "receiver"
	unreadRedeclaration  = "redeclaration"
	unreadNestedModule   = "nested_module_import"
	unreadUndecided      = "undecided_package_import"
	unreadSelfImport     = "self_import"
	unreadCaptureMissing = "payload_missing"
	unreadLoneCR         = "lone_cr"
	unreadMalformedPath  = "malformed_import_path"
)

// structureTagHook is a test-only hook: every branch of the contract reports
// its row id. It is nil in production.
var structureTagHook func(row string)

func structureTag(row string) {
	if structureTagHook != nil {
		structureTagHook(row)
	}
}

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// buildLines returns the file header's build constraint lines as go/build
// reads them (its parseFileHeader and shouldBuild): every //go:build line
// of the leading run of comments and blank lines, or, when there is none,
// the // +build lines before the last blank line of that run.
func buildLines(source []byte) []string {
	content := bytes.TrimPrefix(source, utf8BOM)
	var goBuild []string
	end, ended, inSlashStar := 0, false, false
	p := content
Lines:
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 && !ended {
			end = len(content) - len(p)
			continue
		}
		if !bytes.HasPrefix(line, []byte("//")) {
			ended = true
		}
		if !inSlashStar && constraint.IsGoBuild(string(line)) {
			goBuild = append(goBuild, string(line))
		}
		for len(line) > 0 {
			if inSlashStar {
				i := bytes.Index(line, []byte("*/"))
				if i < 0 {
					continue Lines
				}
				inSlashStar = false
				line = bytes.TrimSpace(line[i+2:])
				continue
			}
			if bytes.HasPrefix(line, []byte("//")) {
				continue Lines
			}
			if bytes.HasPrefix(line, []byte("/*")) {
				inSlashStar = true
				line = bytes.TrimSpace(line[2:])
				continue
			}
			break Lines
		}
	}
	if len(goBuild) > 0 {
		return goBuild
	}
	var plus []string
	for _, line := range strings.Split(string(content[:end]), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") && constraint.IsPlusBuild(line) {
			plus = append(plus, line)
		}
	}
	return plus
}

// goPorts are the GOOS/GOARCH pairs of `go tool dist list` (Go 1.27.1). A
// file is a package file when it builds on at least one of them.
var goPorts = strings.Fields(`aix/ppc64 android/386 android/amd64 android/arm android/arm64
darwin/amd64 darwin/arm64 dragonfly/amd64 freebsd/386 freebsd/amd64 freebsd/arm freebsd/arm64
illumos/amd64 ios/amd64 ios/arm64 js/wasm linux/386 linux/amd64 linux/arm linux/arm64
linux/loong64 linux/mips linux/mips64 linux/mips64le linux/mipsle linux/ppc64 linux/ppc64le
linux/riscv64 linux/s390x netbsd/386 netbsd/amd64 netbsd/arm netbsd/arm64 openbsd/386
openbsd/amd64 openbsd/arm openbsd/arm64 openbsd/ppc64 openbsd/riscv64 plan9/386 plan9/amd64
plan9/arm solaris/amd64 wasip1/wasm windows/386 windows/amd64 windows/arm64`)

// knownOS, unixOS and knownArch are go/build's lists (internal/syslist):
// the names a file name suffix or a build tag can mean.
var (
	knownOS   = wordSet("aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos")
	unixOS    = wordSet("aix android darwin dragonfly freebsd hurd illumos ios linux netbsd openbsd solaris")
	knownArch = wordSet("386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm")
)

// latestGoRelease is the newest go1.N release tag that is set: the release
// this extractor version models (Go 1.27). A newer tag is never set, so the
// result does not depend on the toolchain that built the binary.
const latestGoRelease = 27

// maxFreeTags bounds the custom tags a constraint may name; a constraint
// with more is not decided and the import goes to the default branch.
const maxFreeTags = 12

func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

// osMatches reports whether a tag or file name OS matches GOOS goos, with
// go/build's exceptions (android is linux, illumos is solaris, ios is darwin).
func osMatches(name, goos string) bool {
	return name == goos || name == "linux" && goos == "android" || name == "solaris" && goos == "illumos" || name == "darwin" && goos == "ios"
}

// fileNamePort returns the GOOS and GOARCH a file name requires (go/build's
// goodOSArchFile), each empty when the name requires none.
func fileNamePort(name string) (goos, goarch string) {
	name, _, _ = strings.Cut(name, ".")
	i := strings.Index(name, "_")
	if i < 0 {
		return "", ""
	}
	l := strings.Split(name[i:], "_")
	if n := len(l); n > 0 && l[n-1] == "test" {
		l = l[:n-1]
	}
	n := len(l)
	if n >= 2 && knownOS[l[n-2]] && knownArch[l[n-1]] {
		return l[n-2], l[n-1]
	}
	if n >= 1 && knownOS[l[n-1]] {
		return l[n-1], ""
	}
	if n >= 1 && knownArch[l[n-1]] {
		return "", l[n-1]
	}
	return "", ""
}

// releaseTag reports whether tag is a go1.N release tag and whether it is set.
func releaseTag(tag string) (isRelease, set bool) {
	minor, ok := strings.CutPrefix(tag, "go1.")
	if !ok {
		return false, false
	}
	n, err := strconv.Atoi(minor)
	if err != nil || strconv.Itoa(n) != minor {
		// go1.21.3 or go1.x: never a release tag, never set.
		return true, false
	}
	return true, n >= 1 && n <= latestGoRelease
}

// buildConstraint returns the file's effective constraint: its //go:build
// line, or the conjunction of its // +build lines (go/build drops a +build
// line that does not parse). ok is false when the constraint cannot be
// read: two //go:build lines or one that does not parse, which the go
// command reports as an error.
func buildConstraint(lines []string) (expr constraint.Expr, ok bool) {
	if len(lines) > 0 && constraint.IsGoBuild(lines[0]) {
		if len(lines) > 1 {
			return nil, false
		}
		x, err := constraint.Parse(lines[0])
		return x, err == nil
	}
	for _, line := range lines {
		x, err := constraint.Parse(line)
		if err != nil {
			continue
		}
		if expr == nil {
			expr = x
		} else {
			expr = &constraint.AndExpr{X: expr, Y: x}
		}
	}
	return expr, true
}

// fixedTag reports whether tag's value is decided by the port alone (an OS,
// an architecture, unix, the compiler, a release tag or ignore).
func fixedTag(tag string) bool {
	if release, _ := releaseTag(tag); release {
		return true
	}
	return knownOS[tag] || knownArch[tag] || tag == "unix" || tag == "gc" || tag == "gccgo" || tag == "ignore"
}

// buildSelected reports whether some build configuration selects the file
// name with the header build lines: some port of goPorts with the gc
// compiler, any value of every custom tag (cgo included), `ignore` never
// set, release tags up to latestGoRelease. With cgo set (the file imports
// "C"), only configurations with cgo enabled count: go/build lists such a
// file as ignored when cgo is off. known is false when the constraint
// cannot be decided.
func buildSelected(name string, lines []string, cgo bool) (selected, known bool) {
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return false, true
	}
	expr, ok := buildConstraint(lines)
	if !ok {
		return false, false
	}
	var free []string
	seen := map[string]bool{}
	if expr != nil {
		expr.Eval(func(tag string) bool {
			if !fixedTag(tag) && !seen[tag] {
				seen[tag] = true
				free = append(free, tag)
			}
			return false
		})
	}
	if len(free) > maxFreeTags {
		return false, false
	}
	if cgo && !seen["cgo"] {
		free = append(free, "cgo")
	}
	nameOS, nameArch := fileNamePort(name)
	for _, port := range goPorts {
		goos, goarch, _ := strings.Cut(port, "/")
		if nameOS != "" && !osMatches(nameOS, goos) || nameArch != "" && nameArch != goarch {
			continue
		}
		if expr == nil && !cgo {
			return true, true
		}
		for mask := 0; mask < 1<<len(free); mask++ {
			value := func(tag string) bool {
				if release, set := releaseTag(tag); release {
					return set
				}
				switch {
				case knownOS[tag]:
					return osMatches(tag, goos)
				case knownArch[tag]:
					return tag == goarch
				case tag == "unix":
					return unixOS[goos]
				case tag == "gc":
					return true
				case tag == "gccgo", tag == "ignore":
					return false
				}
				for i, f := range free {
					if f == tag {
						return mask&(1<<i) != 0
					}
				}
				return false
			}
			if cgo && !value("cgo") {
				continue
			}
			if expr == nil || expr.Eval(value) {
				return true, true
			}
		}
	}
	return false, true
}

// importsC reports whether the file imports "C" (a cgo file).
func importsC(f facts.Facts) bool {
	for _, imp := range f.Imports {
		if imp.Module == "C" {
			return true
		}
	}
	return false
}

// GoPackage is a package directory as go/build's Context.Import reads it,
// over every build configuration buildSelected models. Every Go fact that
// names "the package in this directory" reads it: import edges (rows
// GS-I1, GS-I2, GS-I5), the call bindings of Link and the GORM front end's
// package-name bindings and package files, so a file the go command never builds into the
// package (an ignored generator, a documentation file) names nothing.
type GoPackage struct {
	// Name is the package clause of the package (go/build's Package.Name),
	// empty when no file decides it.
	Name string
	// Files are the selected non-test files, cgo files included (GoFiles
	// and CgoFiles), sorted.
	Files []string
	// Tests are the selected _test.go files of package Name (TestGoFiles).
	Tests []string
	// XTests are the selected _test.go files of package Name_test
	// (XTestGoFiles).
	XTests []string
	// Refusal names the default-branch form when go/build would report an
	// error for the directory, or the files cannot be read as Go reads
	// them; the other fields are then empty.
	Refusal string
}

// dirGoFiles lists the scanned .go files directly in dir, sorted (the
// order go/build reads a directory in).
func dirGoFiles(dir string, files map[string]facts.Facts) []string {
	var out []string
	for p := range files {
		if path.Dir(p) == dir && strings.HasSuffix(p, ".go") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Package reads package directory dir as go/build does (build.go,
// Context.Import, Go 1.27.1), applied to the union of the configurations
// buildSelected models:
//   - names starting with _ or . are never read;
//   - a file no configuration matches is ignored; matching ignores the
//     file's imports (go/build's MatchFile);
//   - a matched file with package clause `documentation` is ignored;
//   - a matched _test.go file whose clause ends in _test, other than the
//     package's own name so far, is an external test file of the package
//     without the suffix;
//   - every other matched file must name the same package as the first
//     (MultiplePackageError);
//   - a matched _test.go file importing "C" is an error ("use of cgo in
//     test not supported");
//   - a matched file importing "C" is a package file only in
//     configurations with cgo enabled.
//
// The refusals (go/build errors and the files Graphvein cannot read as Go
// reads them) are: a matched file whose constraint cannot be read, a
// matched file go/parser rejects or without a package clause, a file whose
// raw bytes hold a lone CR (row GS-F3), a file without a Go payload,
// several packages, cgo in a test file.
func Package(dir string, files map[string]facts.Facts) GoPackage {
	var out GoPackage
	refuse := func() GoPackage { return GoPackage{Refusal: unreadUndecided} }
	for _, p := range dirGoFiles(dir, files) {
		name := path.Base(p)
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		f := files[p]
		g := f.Go
		if g == nil || g.LoneCR {
			return refuse()
		}
		matched, known := buildSelected(name, g.Build, false)
		if !known {
			return refuse()
		}
		if !matched {
			continue
		}
		pkg := f.Package
		if g.Rejected || pkg == "" {
			return refuse()
		}
		if pkg == "documentation" {
			continue
		}
		isTest := strings.HasSuffix(name, "_test.go")
		isXTest := false
		if isTest && strings.HasSuffix(pkg, "_test") && out.Name != pkg {
			isXTest = true
			pkg = strings.TrimSuffix(pkg, "_test")
		}
		if out.Name == "" {
			out.Name = pkg
		} else if pkg != out.Name {
			return refuse()
		}
		cgo := importsC(f)
		if cgo && isTest {
			return refuse()
		}
		if cgo {
			if member, _ := buildSelected(name, g.Build, true); !member {
				continue
			}
		}
		switch {
		case isXTest:
			out.XTests = append(out.XTests, p)
		case isTest:
			out.Tests = append(out.Tests, p)
		default:
			out.Files = append(out.Files, p)
		}
	}
	return out
}

// ImportPackage is what an import of the package in directory dir from
// file reads (contract rows GS-I1, GS-I2, GS-I5): the package name the
// import binds, the files it reads, and the refusal when the import goes to
// the default branch. Another directory's importer reads the package's
// non-test files; an external test file of the same directory (package
// <name>_test) also reads the package's internal test files. Refusals: the
// package's own (Package), a `package main` directory imported from
// another directory ("is a program, not an importable package"), and an
// importer in the directory that is not an external test file
// (self_import). No targets and no refusal is row GS-I2.
func ImportPackage(file, dir string, files map[string]facts.Facts) (name string, targets []string, refusal string) {
	pkg := Package(dir, files)
	if pkg.Refusal != "" {
		return "", nil, pkg.Refusal
	}
	if len(pkg.Files) == 0 {
		return "", nil, ""
	}
	if path.Dir(file) != dir {
		if pkg.Name == "main" {
			return "", nil, unreadUndecided
		}
		return pkg.Name, pkg.Files, ""
	}
	if !strings.HasSuffix(file, "_test.go") || files[file].Package != pkg.Name+"_test" {
		return "", nil, unreadSelfImport
	}
	targets = append(append([]string(nil), pkg.Files...), pkg.Tests...)
	sort.Strings(targets)
	return pkg.Name, targets, ""
}

// ImportFiles lists the scanned files an import of the package in
// directory dir from file reads (ImportPackage without the name).
func ImportFiles(file, dir string, files map[string]facts.Facts) (targets []string, refusal string) {
	_, targets, refusal = ImportPackage(file, dir, files)
	return targets, refusal
}

// nestedModule reports whether a scanned go.mod lies between the module
// root base and the package directory dir (row GS-I3): the directory then
// belongs to another module, which the go command reads from its required
// version, not from these files.
func nestedModule(base, dir string, modules map[string]string) bool {
	for d := dir; d != base && d != "." && d != "/" && d != ""; d = path.Dir(d) {
		if modules[d] != "" {
			return true
		}
	}
	return false
}

// unreadDiagnostic reports a file's default-branch forms (O-S1-4): one info
// diagnostic with the total and the count per form, sorted by form.
func unreadDiagnostic(file string, unread map[string]int) []contract.Diagnostic {
	total := 0
	var forms []string
	for form, n := range unread {
		if n > 0 {
			total += n
			forms = append(forms, form)
		}
	}
	if total == 0 {
		return nil
	}
	sort.Strings(forms)
	parts := make([]string, len(forms))
	for i, form := range forms {
		parts[i] = fmt.Sprintf("%s=%d", form, unread[form])
	}
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "go_structure_unread", Message: fmt.Sprintf("%d Go declarations or imports are outside the structural claim contract and claim nothing (%s)", total, strings.Join(parts, ", "))}}
}
