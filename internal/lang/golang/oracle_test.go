package golang

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestGoStructureOracle compares every fact the Go structural claim
// contract (docs/lang/go-structure.md) claims with an oracle built on the Go
// standard library, reading each scanned file's raw bytes from disk (never
// the scanner's normalised text): go/parser and go/ast for declarations,
// spans, hashes and import specs; go/build's Context.ImportDir, run once
// per port of `go tool dist list`, per subset of the directory's custom
// tags (go/build's own AllTags) and with cgo on and off, for the package an
// import reads, its name, its files and its errors; the go command's
// import path rules (x/mod's CheckImportPath, as the oracle transcribes
// them) and its "is a program" rule; and the scanned go.mod files for
// module roots. It never builds, lists or runs the code it reads. The
// differential test TestGoStructureGoList checks the oracle's package
// rules against `go list` itself on the fixture.
//
// By default it runs on the fixture testdata/fixtures/go-structure. Opt in:
// GRAPHVEIN_GO_STRUCTURE_ORACLE_CORPUS=<directory holding gotify and
// realworld-gin> and GRAPHVEIN_GO_STRUCTURE_ORACLE_SELF=1 (this repository).
// Pass is 0 disagreements.
func TestGoStructureOracle(t *testing.T) {
	t.Run("fixture", func(t *testing.T) {
		checkOracle(t, "fixture", copyStructureFixture(t))
	})
	if dir := os.Getenv("GRAPHVEIN_GO_STRUCTURE_ORACLE_CORPUS"); dir != "" {
		for _, name := range []string{"gotify", "realworld-gin"} {
			t.Run(name, func(t *testing.T) { checkOracle(t, name, filepath.Join(dir, name)) })
		}
	}
	if os.Getenv("GRAPHVEIN_GO_STRUCTURE_ORACLE_SELF") == "1" {
		t.Run("self", func(t *testing.T) { checkOracle(t, "self", filepath.Join("..", "..", "..")) })
	}
}

// oraclePorts is the oracle's own copy of `go tool dist list` (Go 1.27.1).
var oraclePorts = strings.Fields(`aix/ppc64 android/386 android/amd64 android/arm android/arm64 darwin/amd64 darwin/arm64 dragonfly/amd64 freebsd/386 freebsd/amd64 freebsd/arm freebsd/arm64 illumos/amd64 ios/amd64 ios/arm64 js/wasm linux/386 linux/amd64 linux/arm linux/arm64 linux/loong64 linux/mips linux/mips64 linux/mips64le linux/mipsle linux/ppc64 linux/ppc64le linux/riscv64 linux/s390x netbsd/386 netbsd/amd64 netbsd/arm netbsd/arm64 openbsd/386 openbsd/amd64 openbsd/arm openbsd/arm64 openbsd/ppc64 openbsd/riscv64 plan9/386 plan9/amd64 plan9/arm solaris/amd64 wasip1/wasm windows/386 windows/amd64 windows/arm64`)

// oracleSystemNames are go/build's operating system and architecture names
// (internal/syslist), never custom tags.
var oracleSystemNames = func() map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Fields("aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos 386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm") {
		out[n] = true
	}
	return out
}()

// oracleReleaseTags are the release tags the oracle sets: go1.1 to go1.27.
func oracleReleaseTags() []string {
	var tags []string
	for i := 1; i <= 27; i++ {
		tags = append(tags, "go1."+strconv.Itoa(i))
	}
	return tags
}

type oSpan struct{ sl, sc, el, ec int }

func (s oSpan) String() string { return fmt.Sprintf("%d:%d-%d:%d", s.sl, s.sc, s.el, s.ec) }

func fromContract(s contract.Span) oSpan {
	return oSpan{s.StartLine, s.StartColumn, s.EndLine, s.EndColumn}
}

type oDecl struct {
	file, kind, name, q, hash string
	span                      oSpan
}

// oracleSide is what the oracle expects for one tree.
type oracleSide struct {
	decls map[string]oDecl // kind \x00 file \x00 q
	// imports[file][span] is the sorted target set ("file:<path>" or
	// "external:<import path>") or empty for no edge.
	imports    map[string]map[oSpan][]string
	unresolved map[string]int
	unread     map[string]map[string]int
	rejected   map[string]bool
	loneCR     map[string]bool
	stats      map[string]int
}

type oracleTree struct {
	root    string
	modules map[string]string // dir -> module path, from scanned go.mod files
	// dirs lists the scanned files of each directory: the inventory is the
	// input of both sides (the scanner's ignore rules decide it, row GS-F1).
	dirs map[string][]string
	raw  map[string][]byte // scanned path -> raw bytes on disk
	pkgs map[string]oPackage
}

// oPackage is a directory as go/build reads it over every configuration.
type oPackage struct {
	name                string
	files, tests, xtest []string
	undecided           string
}

func checkOracle(t *testing.T, corpus, root string) {
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("%s: %v", corpus, err)
	}
	run := runStructure(t, root)
	want := oracleExpect(t, run)
	disagreements := compareOracle(run, want)
	keys := make([]string, 0, len(want.stats))
	for k := range want.stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%d", k, want.stats[k])
	}
	t.Logf("%s: %d Go files, %d disagreements;%s", corpus, len(run.goFiles), len(disagreements), b.String())
	for i, d := range disagreements {
		if i == 60 {
			t.Errorf("... %d more", len(disagreements)-i)
			break
		}
		t.Errorf("%s", d)
	}
}

// oracleHash is SHA-256 of the span's bytes with CRLF and CR read as LF
// (contract v1 content hash).
func oracleHash(b []byte) string {
	s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s)))
}

func oracleExported(n string) bool { r, _ := utf8.DecodeRuneInString(n); return unicode.IsUpper(r) }

func oracleExpect(t *testing.T, run *structureRun) *oracleSide {
	t.Helper()
	w := &oracleSide{decls: map[string]oDecl{}, imports: map[string]map[oSpan][]string{}, unresolved: map[string]int{}, unread: map[string]map[string]int{}, rejected: map[string]bool{}, loneCR: map[string]bool{}, stats: map[string]int{}}
	tree := &oracleTree{root: run.root, modules: map[string]string{}, dirs: map[string][]string{}, raw: map[string][]byte{}, pkgs: map[string]oPackage{}}
	for p := range run.sources {
		raw, err := os.ReadFile(filepath.Join(run.root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		tree.raw[p] = raw
		tree.dirs[path.Dir(p)] = append(tree.dirs[path.Dir(p)], path.Base(p))
		if path.Base(p) == "go.mod" {
			if m := oracleModulePath(raw); m != "" {
				tree.modules[path.Dir(p)] = m
			}
		}
	}
	count := func(file, form string, n int) {
		if w.unread[file] == nil {
			w.unread[file] = map[string]int{}
		}
		w.unread[file][form] += n
	}
	for _, rel := range run.goFiles {
		src := tree.raw[rel]
		w.stats["files"]++
		if oracleLoneCR(src) {
			// Row GS-F3: Go reads these bytes differently from the
			// scanner's text; nothing beyond the file is claimed.
			w.loneCR[rel] = true
			w.stats["files_lone_cr"]++
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			w.rejected[rel] = true
			w.stats["files_rejected"]++
			continue
		}
		span := func(n ast.Node) (oSpan, int, int) {
			s, e := fset.PositionFor(n.Pos(), false), fset.PositionFor(n.End(), false)
			return oSpan{s.Line, s.Column - 1, e.Line, e.Column - 1}, s.Offset, e.Offset
		}
		var decls []oDecl
		add := func(n ast.Node, kind, name, q string) {
			s, so, eo := span(n)
			decls = append(decls, oDecl{file: rel, kind: kind, name: name, q: q, span: s, hash: oracleHash(src[so:eo])})
		}
		inits, blanks := 0, 0
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					q := d.Name.Name
					switch q {
					case "init":
						inits++
						if inits > 1 {
							q += "@" + strconv.Itoa(inits)
						}
					case "_":
						blanks++
						if blanks > 1 {
							q += "@" + strconv.Itoa(blanks)
						}
					}
					add(d, "function", d.Name.Name, q)
					continue
				}
				base := ""
				if len(d.Recv.List) == 1 && len(d.Recv.List[0].Names) <= 1 {
					base = oracleReceiver(d.Recv.List[0].Type)
				}
				if base == "" {
					count(rel, unreadReceiver, 1)
					w.stats["receivers_unread"]++
					continue
				}
				add(d, "method", d.Name.Name, base+"."+d.Name.Name)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.ImportSpec:
						v, _ := strconv.Unquote(s.Path.Value)
						sp, _, _ := span(s)
						w.stats["imports"]++
						targets, unresolved, form := tree.resolve(rel, f.Name.Name, v)
						if w.imports[rel] == nil {
							w.imports[rel] = map[oSpan][]string{}
						}
						w.imports[rel][sp] = targets
						if unresolved {
							w.unresolved[rel]++
						}
						if form != "" {
							count(rel, form, 1)
							w.stats["imports_"+form]++
						}
					case *ast.TypeSpec:
						kind := "type"
						if s.Assign.IsValid() {
							kind = "type_alias"
						} else {
							switch s.Type.(type) {
							case *ast.StructType:
								kind = "struct"
							case *ast.InterfaceType:
								kind = "interface"
							}
						}
						add(s, kind, s.Name.Name, s.Name.Name)
					case *ast.ValueSpec:
						kind := "variable"
						if d.Tok == token.CONST {
							kind = "constant"
						}
						for _, n := range s.Names {
							if n.Name != "_" && oracleExported(n.Name) {
								add(s, kind, n.Name, n.Name)
							}
						}
					}
				}
			}
		}
		seen := map[string]int{}
		for _, d := range decls {
			seen[d.kind+"\x00"+d.q]++
		}
		for _, d := range decls {
			if n := seen[d.kind+"\x00"+d.q]; n > 1 {
				count(rel, unreadRedeclaration, 1)
				w.stats["redeclarations"]++
				continue
			}
			w.decls[d.kind+"\x00"+d.file+"\x00"+d.q] = d
			w.stats["decl_"+d.kind]++
		}
	}
	return w
}

// oracleReceiver is the base type name of a receiver of row GS-D4: T, *T,
// T[...] or *T[...]; "" for any other form.
func oracleReceiver(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	switch x := e.(type) {
	case *ast.IndexExpr:
		e = x.X
	case *ast.IndexListExpr:
		e = x.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

var moduleLine = regexp.MustCompile(`^\s*module\s+"?([^"\s]+)"?\s*(//.*)?$`)

func oracleModulePath(src []byte) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(src), "\r", ""), "\n") {
		if m := moduleLine.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// oracleLoneCR reports a CR byte not followed by LF.
func oracleLoneCR(raw []byte) bool {
	for i, b := range raw {
		if b == '\r' && (i+1 == len(raw) || raw[i+1] != '\n') {
			return true
		}
	}
	return false
}

var (
	oracleElem      = regexp.MustCompile(`^[A-Za-z0-9._~+-]+$`)
	oracleShortName = regexp.MustCompile(`~[0-9]+$`)
	oracleWindows   = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)
)

// oracleValidImportPath is the go command's import path check
// (golang.org/x/mod/module.CheckImportPath, Go 1.27.1's vendored copy, as
// cmd/go/internal/modload's importFromModules applies it), written from
// its documentation: slash-separated non-empty elements of ASCII letters,
// digits and - . _ ~ +, no leading dash, no element of dots only or ending
// in a dot, no reserved Windows name before the first dot, no Windows short
// name suffix (~ and digits).
func oracleValidImportPath(p string) bool {
	if p == "" || p[0] == '-' || !utf8.ValidString(p) {
		return false
	}
	for _, elem := range strings.Split(p, "/") {
		if !oracleElem.MatchString(elem) || strings.Trim(elem, ".") == "" || strings.HasSuffix(elem, ".") {
			return false
		}
		short, _, _ := strings.Cut(elem, ".")
		if oracleWindows.MatchString(short) || oracleShortName.MatchString(short) {
			return false
		}
	}
	return true
}

// resolve returns the targets an import spec of file (package clause
// clause) with path p must have, whether an unresolved_import warning is
// due, and the default-branch form it is counted as.
func (o *oracleTree) resolve(file, clause, p string) (targets []string, unresolved bool, form string) {
	if !oracleValidImportPath(p) {
		return nil, false, unreadMalformedPath
	}
	external := []string{"external:" + p}
	base, module := "", ""
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if m := o.modules[dir]; m != "" {
			base, module = dir, m
			break
		}
		if dir == "." || dir == "/" {
			break
		}
	}
	if module == "" || !oracleValidImportPath(module) || p != module && !strings.HasPrefix(p, module+"/") {
		return external, false, ""
	}
	dir := base
	if p != module {
		dir = strings.TrimPrefix(base+"/"+strings.TrimPrefix(p, module+"/"), "./")
	}
	for d := dir; d != base && d != "."; d = path.Dir(d) {
		if o.modules[d] != "" {
			return external, false, unreadNestedModule
		}
	}
	pkg := o.packageAt(dir)
	if pkg.undecided != "" {
		return nil, false, unreadUndecided
	}
	if len(pkg.files) == 0 {
		return nil, true, ""
	}
	if path.Dir(file) != dir {
		if pkg.name == "main" {
			// cmd/go: import "..." is a program, not an importable package.
			return nil, false, unreadUndecided
		}
		return prefixed(pkg.files), false, ""
	}
	if !strings.HasSuffix(file, "_test.go") || clause != pkg.name+"_test" {
		return nil, false, unreadSelfImport
	}
	return prefixed(append(append([]string(nil), pkg.files...), pkg.tests...)), false, ""
}

func prefixed(files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = "file:" + f
	}
	sort.Strings(out)
	return out
}

// oracleInfo is a scanned file as build.Context.ReadDir returns it.
type oracleInfo struct {
	name string
	size int64
}

func (i oracleInfo) Name() string       { return i.name }
func (i oracleInfo) Size() int64        { return i.size }
func (i oracleInfo) Mode() fs.FileMode  { return 0o644 }
func (i oracleInfo) ModTime() time.Time { return time.Time{} }
func (i oracleInfo) IsDir() bool        { return false }
func (i oracleInfo) Sys() any           { return nil }

// context is a go/build context for one configuration that reads the
// scanned inventory and its raw bytes.
func (o *oracleTree) context(goos, goarch string, cgo bool, tags []string) build.Context {
	abs := func(p string) string {
		rel, err := filepath.Rel(o.root, p)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	return build.Context{
		GOOS: goos, GOARCH: goarch, CgoEnabled: cgo, Compiler: "gc",
		ReleaseTags: oracleReleaseTags(), BuildTags: tags,
		ReadDir: func(dir string) ([]fs.FileInfo, error) {
			var out []fs.FileInfo
			for _, name := range o.dirs[abs(dir)] {
				out = append(out, oracleInfo{name: name, size: int64(len(o.raw[path.Join(abs(dir), name)]))})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
			return out, nil
		},
		OpenFile: func(p string) (io.ReadCloser, error) {
			raw, ok := o.raw[abs(p)]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		},
	}
}

// oracleFreeTags are the tags of go/build's AllTags whose value no port
// decides: every tag but the operating systems, architectures, release
// tags, unix, the compilers, cgo (CgoEnabled) and ignore (never set).
func oracleFreeTags(all []string) []string {
	var out []string
	for _, tag := range all {
		if tag == "ignore" || tag == "cgo" || tag == "gc" || tag == "gccgo" || tag == "unix" || strings.HasPrefix(tag, "go1.") || oracleSystemNames[tag] {
			continue
		}
		out = append(out, tag)
	}
	return out
}

// packageAt reads directory dir with go/build's Context.ImportDir in every
// configuration (each port, cgo on and off, each subset of the custom
// tags the directory's files name) and joins the results: the files of
// every list over all configurations, one package name, and undecided when
// some configuration reports an error other than "no Go files" or two
// configurations name different packages. Graphvein's own refusals beyond
// go/build are added: a file whose raw bytes hold a lone CR (row GS-F3), a
// file some configuration matches that go/parser rejects, more than 12
// custom tags.
func (o *oracleTree) packageAt(dir string) oPackage {
	if pkg, ok := o.pkgs[dir]; ok {
		return pkg
	}
	pkg := o.readPackage(dir)
	o.pkgs[dir] = pkg
	return pkg
}

func (o *oracleTree) readPackage(dir string) oPackage {
	abs := filepath.Join(o.root, filepath.FromSlash(dir))
	hasGo := false
	for _, name := range o.dirs[dir] {
		hasGo = hasGo || strings.HasSuffix(name, ".go")
	}
	if !hasGo {
		// No scanned Go file: nothing to import (go/build cannot read a
		// directory the inventory does not hold).
		return oPackage{}
	}
	for _, name := range o.dirs[dir] {
		if strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, "_") && !strings.HasPrefix(name, ".") && oracleLoneCR(o.raw[path.Join(dir, name)]) {
			return oPackage{undecided: name + ": lone CR"}
		}
	}
	all := map[string]bool{}
	for _, port := range oraclePorts {
		goos, goarch, _ := strings.Cut(port, "/")
		ctxt := o.context(goos, goarch, true, nil)
		p, _ := ctxt.ImportDir(abs, 0)
		if p != nil {
			for _, tag := range p.AllTags {
				all[tag] = true
			}
		}
	}
	var tags []string
	for tag := range all {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	free := oracleFreeTags(tags)
	if len(free) > 12 {
		return oPackage{undecided: fmt.Sprintf("%d custom tags", len(free))}
	}
	files, tests, xtests, names := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	// rejected are the files go/parser rejects; matched those of them some
	// configuration matches (go/build's MatchFile).
	var rejected []string
	for _, name := range o.dirs[dir] {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if _, err := parser.ParseFile(token.NewFileSet(), name, o.raw[path.Join(dir, name)], parser.SkipObjectResolution); err != nil {
			rejected = append(rejected, name)
		}
	}
	matched := map[string]bool{}
	for _, port := range oraclePorts {
		goos, goarch, _ := strings.Cut(port, "/")
		for _, cgo := range []bool{false, true} {
			for mask := 0; mask < 1<<len(free); mask++ {
				var set []string
				for i, tag := range free {
					if mask&(1<<i) != 0 {
						set = append(set, tag)
					}
				}
				ctxt := o.context(goos, goarch, cgo, set)
				p, err := ctxt.ImportDir(abs, 0)
				var noGo *build.NoGoError
				if err != nil && !errors.As(err, &noGo) {
					return oPackage{undecided: fmt.Sprintf("%s: %v", port, err)}
				}
				for _, name := range rejected {
					if ok, _ := ctxt.MatchFile(abs, name); ok {
						matched[name] = true
					}
				}
				if p == nil || p.Name == "" {
					continue
				}
				names[p.Name] = true
				for _, list := range [][]string{p.GoFiles, p.CgoFiles} {
					for _, f := range list {
						files[path.Join(dir, f)] = true
					}
				}
				for _, f := range p.TestGoFiles {
					tests[path.Join(dir, f)] = true
				}
				for _, f := range p.XTestGoFiles {
					xtests[path.Join(dir, f)] = true
				}
			}
		}
	}
	if len(names) > 1 {
		return oPackage{undecided: "several package names"}
	}
	for name := range matched {
		return oPackage{undecided: name + ": go/parser rejects it"}
	}
	pkg := oPackage{files: sortedSet(files), tests: sortedSet(tests), xtest: sortedSet(xtests)}
	for name := range names {
		pkg.name = name
	}
	return pkg
}

func sortedSet(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var unreadMessage = regexp.MustCompile(`\(([^)]*)\)$`)

// compareOracle lists every disagreement between the run and the oracle.
func compareOracle(run *structureRun, want *oracleSide) []string {
	var out []string
	report := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	declKinds := map[string]bool{"function": true, "method": true, "struct": true, "interface": true, "type": true, "type_alias": true, "variable": true, "constant": true}
	got := map[string]contract.Node{}
	for _, n := range run.nodes {
		if !declKinds[n.Kind] || !strings.HasSuffix(n.File, ".go") {
			continue
		}
		key := n.Kind + "\x00" + n.File + "\x00" + n.QualifiedName
		if _, dup := got[key]; dup {
			report("duplicate identity %s", n.Identity)
		}
		got[key] = n
	}
	contains := map[string][]contract.Edge{}
	for _, e := range run.edges {
		if e.Kind == "contains" {
			contains[e.Target] = append(contains[e.Target], e)
		}
	}
	for key, d := range want.decls {
		n, ok := got[key]
		if !ok {
			report("declaration missing: %s %s %s %s", d.file, d.kind, d.q, d.span)
			continue
		}
		identity, _ := contract.Identity(d.kind, d.file, d.q)
		if n.Identity != identity {
			report("identity %s, want %s", n.Identity, identity)
		}
		if n.Name != d.name {
			report("%s: name %q, want %q", identity, n.Name, d.name)
		}
		if n.Span == nil || fromContract(*n.Span) != d.span {
			report("%s: span %v, want %s", identity, n.Span, d.span)
		}
		if n.ContentHash != d.hash {
			report("%s: content hash differs", identity)
		}
		fileID, _ := contract.Identity("file", d.file, d.file)
		edges := contains[identity]
		if len(edges) != 1 || edges[0].Source != fileID || edges[0].Provenance != "fact" || edges[0].Confidence != 1 || len(edges[0].Evidence) != 1 || edges[0].Evidence[0].File != d.file || fromContract(edges[0].Evidence[0].Span) != d.span {
			report("%s: contains edges %+v, want one from %s with the declaration span", identity, edges, fileID)
		}
	}
	for key, n := range got {
		if _, ok := want.decls[key]; !ok {
			report("declaration not in the oracle: %s %s %s", n.File, n.Identity, fromContract(*n.Span))
		}
	}
	externals := map[string]string{}
	for _, n := range run.nodes {
		if n.Kind == "external" && n.Attributes["ecosystem"] == "go" {
			externals[n.Identity], _ = n.Attributes["name"].(string)
		}
	}
	gotImports := map[string]map[oSpan][]string{}
	for _, e := range run.edges {
		if e.Kind != "imports" {
			continue
		}
		target := ""
		if name, ok := externals[e.Target]; ok {
			target = "external:" + name
		} else if strings.HasPrefix(e.Target, "file:") {
			kind, file, _, err := contract.ParseIdentity(e.Target)
			if err != nil || kind != "file" {
				report("imports edge to %s", e.Target)
				continue
			}
			target = "file:" + file
		} else {
			report("imports edge to %s", e.Target)
			continue
		}
		for _, ev := range e.Evidence {
			if gotImports[ev.File] == nil {
				gotImports[ev.File] = map[oSpan][]string{}
			}
			s := fromContract(ev.Span)
			gotImports[ev.File][s] = append(gotImports[ev.File][s], target)
		}
	}
	for file, specs := range want.imports {
		for s, targets := range specs {
			have := append([]string(nil), gotImports[file][s]...)
			sort.Strings(have)
			if strings.Join(have, " ") != strings.Join(targets, " ") {
				report("%s %s: import targets %v, want %v", file, s, have, targets)
			}
		}
	}
	for file, specs := range gotImports {
		for s := range specs {
			if _, ok := want.imports[file][s]; !ok {
				report("%s %s: import evidence with no oracle import spec (rejected file: %v, lone CR: %v)", file, s, want.rejected[file], want.loneCR[file])
			}
		}
	}
	unresolved := map[string]int{}
	unread := map[string]map[string]int{}
	for _, d := range run.diagnostics {
		switch d.Code {
		case "unresolved_import":
			unresolved[d.File]++
		case "go_structure_unread":
			m := unreadMessage.FindStringSubmatch(d.Message)
			if m == nil || unread[d.File] != nil {
				report("%s: go_structure_unread %q", d.File, d.Message)
				continue
			}
			unread[d.File] = map[string]int{}
			for _, part := range strings.Split(m[1], ", ") {
				form, n, _ := strings.Cut(part, "=")
				unread[d.File][form], _ = strconv.Atoi(n)
			}
		}
	}
	for _, file := range run.goFiles {
		if unresolved[file] != want.unresolved[file] {
			report("%s: %d unresolved_import warnings, want %d", file, unresolved[file], want.unresolved[file])
		}
		have, expect := unread[file], want.unread[file]
		if want.loneCR[file] {
			for form := range have {
				if form != unreadLoneCR {
					report("%s: lone-CR file counts %s", file, form)
				}
			}
			continue
		}
		if want.rejected[file] {
			// The count of a rejected file is tree-sitter's recovery,
			// which the oracle cannot see; it may carry no other form.
			for form := range have {
				if form != unreadSyntaxError {
					report("%s: rejected file counts %s", file, form)
				}
			}
			continue
		}
		forms := map[string]bool{}
		for form := range have {
			forms[form] = true
		}
		for form := range expect {
			forms[form] = true
		}
		for form := range forms {
			if have[form] != expect[form] {
				report("%s: default branch %s=%d, want %d", file, form, have[form], expect[form])
			}
		}
	}
	sort.Strings(out)
	return out
}
