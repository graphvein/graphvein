//go:build ignore

// Command oracle compares Graphvein's emitted Go structure (declarations,
// spans, content hashes, import edges) with go/parser + go/ast and a
// go.mod/go.work-based package resolver. It parses corpus files only; it
// never builds, runs or lists corpus code.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type span struct{ SL, SC, EL, EC int }

func (s span) String() string { return fmt.Sprintf("%d:%d-%d:%d", s.SL, s.SC, s.EL, s.EC) }

type decl struct {
	File, Kind, Name, Q string
	Span                span
	Hash                string
}

func (d decl) key() string { return d.Kind + "\x00" + d.File + "\x00" + d.Q }

type record struct {
	Class  string `json:"class"`
	Corpus string `json:"corpus"`
	File   string `json:"file"`
	Detail string `json:"detail"`
}

var out []record
var corpus string

func report(class, file, detail string) {
	out = append(out, record{class, corpus, file, detail})
}

func hash(b []byte) string {
	s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s)))
}

func exported(n string) bool { r, _ := utf8.DecodeRuneInString(n); return unicode.IsUpper(r) }

// ---------- Graphvein side ----------

type gvNode struct {
	Type, Identity, Kind, Name, File, Language, Precision, Path, Code, Message string
	QualifiedName                                                              string         `json:"qualified_name"`
	ContentHash                                                                string         `json:"content_hash"`
	Span                                                                       *jspan         `json:"span"`
	Attributes                                                                 map[string]any `json:"attributes"`
}

type jspan struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

func (j jspan) s() span { return span{j.StartLine, j.StartColumn, j.EndLine, j.EndColumn} }

type gvEdge struct {
	Kind, Source, Target string
	Evidence             []struct {
		File string
		Span jspan
	}
}

var declKinds = map[string]bool{"function": true, "method": true, "struct": true, "interface": true, "type": true, "type_alias": true, "variable": true, "constant": true}

type graphvein struct {
	goFiles   []string
	precision map[string]string
	decls     map[string]decl
	dupDecl   map[string]int
	// imports[file][span] = targets ("file:<path>" or "external:<name>")
	imports    map[string]map[span][]string
	unresolved map[string]int
	syntaxErr  map[string]bool
	externals  map[string]string
}

func decodeIdentity(id string) (kind, file, q string) {
	k, rest, _ := strings.Cut(id, ":")
	f, n, _ := strings.Cut(rest, "#")
	f, _ = url.PathUnescape(f)
	n, _ = url.PathUnescape(n)
	return k, f, n
}

func readGraphvein(p string) *graphvein {
	g := &graphvein{precision: map[string]string{}, decls: map[string]decl{}, dupDecl: map[string]int{}, imports: map[string]map[span][]string{}, unresolved: map[string]int{}, syntaxErr: map[string]bool{}, externals: map[string]string{}}
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var edges []gvEdge
	for sc.Scan() {
		var head struct{ Type string }
		json.Unmarshal(sc.Bytes(), &head)
		switch head.Type {
		case "file":
			var n gvNode
			json.Unmarshal(sc.Bytes(), &n)
			if n.Language == "go" {
				g.goFiles = append(g.goFiles, n.Path)
				g.precision[n.Path] = n.Precision
			}
		case "node":
			var n gvNode
			json.Unmarshal(sc.Bytes(), &n)
			if n.Kind == "external" {
				if eco, _ := n.Attributes["ecosystem"].(string); eco == "go" {
					g.externals[n.Identity], _ = n.Attributes["name"].(string)
				}
			}
			if !declKinds[n.Kind] || !strings.HasSuffix(n.File, ".go") {
				continue
			}
			d := decl{File: n.File, Kind: n.Kind, Name: n.Name, Q: n.QualifiedName, Hash: n.ContentHash}
			if n.Span != nil {
				d.Span = n.Span.s()
			}
			if _, ok := g.decls[d.key()]; ok {
				g.dupDecl[d.key()]++
			}
			g.decls[d.key()] = d
		case "edge":
			var e gvEdge
			json.Unmarshal(sc.Bytes(), &e)
			if e.Kind == "imports" {
				edges = append(edges, e)
			}
		case "diagnostic":
			var n gvNode
			json.Unmarshal(sc.Bytes(), &n)
			if strings.HasSuffix(n.File, ".go") {
				switch n.Code {
				case "unresolved_import":
					g.unresolved[n.File]++
				case "syntax_error", "parse_failed", "extract_failed":
					g.syntaxErr[n.File] = true
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
	for _, e := range edges {
		_, src, _ := decodeIdentity(e.Source)
		if !strings.HasSuffix(src, ".go") {
			continue
		}
		tk, tf, tq := decodeIdentity(e.Target)
		target := ""
		switch tk {
		case "file":
			target = "file:" + tf
		case "external":
			if !strings.HasPrefix(tq, "go:") {
				continue
			}
			target = "external:" + strings.TrimPrefix(tq, "go:")
		default:
			continue
		}
		for _, ev := range e.Evidence {
			if g.imports[ev.File] == nil {
				g.imports[ev.File] = map[span][]string{}
			}
			g.imports[ev.File][ev.Span.s()] = append(g.imports[ev.File][ev.Span.s()], target)
		}
	}
	return g
}

// ---------- oracle side: declarations ----------

type parsed struct {
	file    *ast.File
	fset    *token.FileSet
	src     []byte
	err     error
	decls   []decl
	imports []oImport
	// unmodelled counts forms Graphvein's contract does not claim.
	unexportedValues int
	blankValues      int
	localTypes       int
}

type oImport struct {
	Path, Name string
	Span       span
}

func pos(fset *token.FileSet, p token.Pos) (int, int, int) {
	x := fset.PositionFor(p, false)
	return x.Line, x.Column - 1, x.Offset
}

func spanOf(fset *token.FileSet, n ast.Node) (span, int, int) {
	sl, sc, so := pos(fset, n.Pos())
	el, ec, eo := pos(fset, n.End())
	return span{sl, sc, el, ec}, so, eo
}

func recvBase(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// recvSimple is the receiver shape tree-sitter's namedType reads: optional
// one pointer, optional generic instantiation, a plain identifier.
func recvSimple(e ast.Expr) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	switch t := e.(type) {
	case *ast.IndexExpr:
		e = t.X
	case *ast.IndexListExpr:
		e = t.X
	}
	_, ok := e.(*ast.Ident)
	return ok
}

func parseFile(root, rel string) *parsed {
	src, err := os.ReadFile(filepath.Join(root, rel))
	p := &parsed{src: src, fset: token.NewFileSet()}
	if err != nil {
		p.err = err
		return p
	}
	p.file, p.err = parser.ParseFile(p.fset, rel, src, parser.SkipObjectResolution|parser.ParseComments)
	if p.file == nil || p.err != nil {
		// A file go/parser rejects is not compared: Graphvein's facts for it
		// come from tree-sitter error recovery (reported separately).
		return p
	}
	add := func(n ast.Node, kind, name, q string) {
		s, so, eo := spanOf(p.fset, n)
		p.decls = append(p.decls, decl{File: rel, Kind: kind, Name: name, Q: q, Span: s, Hash: hash(src[so:eo])})
	}
	inits := 0
	for _, d := range p.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				q := d.Name.Name
				if q == "init" {
					inits++
					if inits > 1 {
						q += "@" + strconv.Itoa(inits)
					}
				}
				add(d, "function", d.Name.Name, q)
			} else if len(d.Recv.List) == 1 {
				base := recvBase(d.Recv.List[0].Type)
				if base == "" {
					report("oracle_bad_receiver", rel, d.Name.Name)
					continue
				}
				if !recvSimple(d.Recv.List[0].Type) {
					report("receiver_form_unread", rel, base+"."+d.Name.Name)
				}
				add(d, "method", d.Name.Name, base+"."+d.Name.Name)
			}
			if d.Body != nil {
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if _, ok := n.(*ast.TypeSpec); ok {
						p.localTypes++
					}
					return true
				})
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ImportSpec:
					v, _ := strconv.Unquote(s.Path.Value)
					name := ""
					if s.Name != nil {
						name = s.Name.Name
					}
					sp, _, _ := spanOf(p.fset, s)
					p.imports = append(p.imports, oImport{v, name, sp})
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
						if n.Name == "_" {
							p.blankValues++
						} else if exported(n.Name) {
							add(s, kind, n.Name, n.Name)
						} else {
							p.unexportedValues++
						}
					}
				}
			}
		}
	}
	return p
}

// ---------- oracle side: build constraints and packages ----------

var knownOS = strings.Fields("aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos")
var knownArch = strings.Fields("386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm")
var unixOS = strings.Fields("aix android darwin dragonfly freebsd hurd illumos ios linux netbsd openbsd solaris")
var ports [][2]string

func in(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func loadPorts(p string) {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	for _, l := range strings.Fields(string(b)) {
		a, b, _ := strings.Cut(l, "/")
		ports = append(ports, [2]string{a, b})
	}
}

// buildable reports whether some real port and some assignment of custom
// tags (with "ignore" false) selects the file. why names the reason when not.
func buildable(name string, src []byte) (bool, string) {
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return false, "underscore_or_dot_name"
	}
	stem := strings.TrimSuffix(name, ".go")
	stem = strings.TrimSuffix(stem, "_test")
	parts := strings.Split(stem, "_")
	fileOS, fileArch := "", ""
	if n := len(parts); n >= 2 {
		last := parts[n-1]
		if in(knownArch, last) {
			fileArch = last
			if n >= 3 && in(knownOS, parts[n-2]) {
				fileOS = parts[n-2]
			}
		} else if in(knownOS, last) {
			fileOS = last
		}
	}
	var expr constraint.Expr
	var plus []constraint.Expr
	// Header: lines before the package clause.
	for _, line := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "package ") {
			break
		}
		if constraint.IsGoBuild(t) {
			expr, _ = constraint.Parse(t)
		} else if constraint.IsPlusBuild(t) && expr == nil {
			if e, err := constraint.Parse(t); err == nil {
				plus = append(plus, e)
			}
		}
	}
	if expr == nil && len(plus) > 0 {
		expr = plus[0]
		for _, e := range plus[1:] {
			expr = &constraint.AndExpr{X: expr, Y: e}
		}
	}
	free := map[string]bool{}
	if expr != nil {
		expr.Eval(func(tag string) bool {
			if !in(knownOS, tag) && !in(knownArch, tag) && tag != "unix" && tag != "ignore" && !strings.HasPrefix(tag, "go1.") && tag != "gc" && tag != "gccgo" {
				free[tag] = true
			}
			return true
		})
	}
	var freeTags []string
	for t := range free {
		freeTags = append(freeTags, t)
	}
	sort.Strings(freeTags)
	if len(freeTags) > 10 {
		freeTags = freeTags[:10]
	}
	for _, pt := range ports {
		if fileOS != "" && pt[0] != fileOS && !(fileOS == "linux" && pt[0] == "android") && !(fileOS == "solaris" && pt[0] == "illumos") && !(fileOS == "darwin" && pt[0] == "ios") {
			continue
		}
		if fileArch != "" && pt[1] != fileArch {
			continue
		}
		if expr == nil {
			return true, ""
		}
		for mask := 0; mask < 1<<len(freeTags); mask++ {
			ok := expr.Eval(func(tag string) bool {
				switch {
				case tag == pt[0] || tag == pt[1]:
					return true
				case tag == "linux" && pt[0] == "android", tag == "solaris" && pt[0] == "illumos", tag == "darwin" && pt[0] == "ios":
					return true
				case tag == "unix":
					return in(unixOS, pt[0])
				case tag == "ignore":
					return false
				case strings.HasPrefix(tag, "go1."), tag == "gc":
					return true
				case tag == "gccgo":
					return false
				}
				for i, t := range freeTags {
					if t == tag {
						return mask&(1<<i) != 0
					}
				}
				return false
			})
			if ok {
				return true, ""
			}
		}
	}
	if expr != nil && strings.Contains(expr.String(), "ignore") {
		return false, "build_ignore"
	}
	if fileOS != "" || fileArch != "" {
		if expr == nil {
			return false, "filename_port_impossible"
		}
	}
	return false, "constraint_unsatisfiable"
}

type module struct {
	Dir, Path string
	Replace   map[string]string // module path -> local dir (repo-relative)
	Requires  map[string]bool
}

func parseGoMod(root, rel string) *module {
	b, _ := os.ReadFile(filepath.Join(root, rel))
	m := &module{Dir: path.Dir(rel), Replace: map[string]string{}, Requires: map[string]bool{}}
	block := ""
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		w := strings.Fields(line)
		if len(w) == 0 {
			continue
		}
		if w[0] == ")" {
			block = ""
			continue
		}
		if len(w) >= 2 && w[1] == "(" {
			block = w[0]
			continue
		}
		verb := block
		if verb == "" {
			verb = w[0]
			w = w[1:]
		}
		switch verb {
		case "module":
			if len(w) > 0 {
				m.Path = strings.Trim(w[0], "\"`")
			}
		case "require":
			if len(w) > 0 {
				m.Requires[strings.Trim(w[0], "\"")] = true
			}
		case "replace":
			// a [v] => b [v]
			for i, x := range w {
				if x == "=>" && i+1 < len(w) {
					to := strings.Trim(w[i+1], "\"")
					if strings.HasPrefix(to, "./") || strings.HasPrefix(to, "../") || to == "." || to == ".." {
						m.Replace[strings.Trim(w[0], "\"")] = path.Clean(path.Join(m.Dir, to))
					}
				}
			}
		}
	}
	return m
}

type world struct {
	root    string
	modules map[string]*module // dir -> module
	work    map[string][]string
	pkgName map[string]string
	files   map[string][]string // dir -> buildable .go files on disk
	skipped map[string]string   // path -> reason
	inv     map[string]bool
}

func (w *world) moduleAt(dir string) *module {
	for {
		if m := w.modules[dir]; m != nil {
			return m
		}
		if dir == "." || dir == "/" || dir == "" {
			return nil
		}
		dir = path.Dir(dir)
	}
}

func (w *world) dirFiles(dir string) []string {
	if f, ok := w.files[dir]; ok {
		return f
	}
	entries, err := os.ReadDir(filepath.Join(w.root, dir))
	var list []string
	names := map[string]int{}
	if err == nil {
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") {
				continue
			}
			rel := path.Join(dir, n)
			src, _ := os.ReadFile(filepath.Join(w.root, rel))
			if ok, why := buildable(n, src); !ok {
				w.skipped[rel] = why
				continue
			}
			list = append(list, rel)
			if !strings.HasSuffix(n, "_test.go") {
				fset := token.NewFileSet()
				if f, err := parser.ParseFile(fset, rel, src, parser.PackageClauseOnly); err == nil {
					names[f.Name.Name]++
				}
			}
		}
	}
	best, bestN := "", 0
	for n, c := range names {
		if c > bestN || c == bestN && n < best {
			best, bestN = n, c
		}
	}
	if len(names) > 1 {
		report("dir_multiple_packages", dir, fmt.Sprint(names))
	}
	w.pkgName[dir] = best
	var kept []string
	for _, rel := range list {
		src, _ := os.ReadFile(filepath.Join(w.root, rel))
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.PackageClauseOnly)
		if err != nil {
			w.skipped[rel] = "no_package_clause"
			continue
		}
		pn := f.Name.Name
		if pn != best && pn != best+"_test" {
			w.skipped[rel] = "other_package_" + pn
			continue
		}
		kept = append(kept, rel)
	}
	w.files[dir] = kept
	return kept
}

func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// resolve returns the package dir for import p from file f, or a class.
func (w *world) resolve(f, p string) (dir, class string) {
	if p == "C" {
		return "", "cgo"
	}
	m := w.moduleAt(path.Dir(f))
	if m == nil {
		if isStd(p) {
			return "", "std"
		}
		return "", "no_module"
	}
	// build list of local modules: the main module, local replaces, go.work uses
	type cand struct{ path, dir string }
	cands := []cand{{m.Path, m.Dir}}
	for mp, d := range m.Replace {
		cands = append(cands, cand{mp, d})
	}
	for wdir, uses := range w.work {
		if wdir == "." || m.Dir == wdir || strings.HasPrefix(m.Dir, wdir+"/") {
			for _, u := range uses {
				if um := w.modules[u]; um != nil {
					cands = append(cands, cand{um.Path, um.Dir})
				}
			}
		}
	}
	best := cand{}
	for _, c := range cands {
		if c.path == "" {
			continue
		}
		if (p == c.path || strings.HasPrefix(p, c.path+"/")) && len(c.path) > len(best.path) {
			best = c
		}
	}
	if best.path == "" {
		if isStd(p) {
			return "", "std"
		}
		return "", "external"
	}
	d := path.Join(best.dir, strings.TrimPrefix(strings.TrimPrefix(p, best.path), "/"))
	// a nested go.mod between best.dir and d makes d another module
	for x := d; x != best.dir && x != "." && x != ""; x = path.Dir(x) {
		if w.modules[x] != nil {
			return d, "nested_module"
		}
	}
	class = "local"
	if best.dir != m.Dir {
		class = "local_other_module"
	}
	return d, class
}

func expected(w *world, importer, dir string) []string {
	var outFiles []string
	list := w.dirFiles(dir)
	pn := w.pkgName[dir]
	for _, rel := range list {
		test := strings.HasSuffix(rel, "_test.go")
		if test {
			// a test importer in the same directory sees the package under
			// test with its internal test files (package <pn>, not <pn>_test)
			if !(strings.HasSuffix(importer, "_test.go") && path.Dir(importer) == dir) {
				continue
			}
			src, _ := os.ReadFile(filepath.Join(w.root, rel))
			fset := token.NewFileSet()
			f, _ := parser.ParseFile(fset, rel, src, parser.PackageClauseOnly)
			if f == nil || f.Name.Name != pn {
				continue
			}
		} else {
			src, _ := os.ReadFile(filepath.Join(w.root, rel))
			fset := token.NewFileSet()
			f, _ := parser.ParseFile(fset, rel, src, parser.PackageClauseOnly)
			if f == nil || f.Name.Name != pn {
				continue
			}
		}
		outFiles = append(outFiles, rel)
	}
	return outFiles
}

// ---------- compare ----------

type stats map[string]int

func main() {
	// Usage (the T020 S1 investigation; never run by CI):
	//   go tool dist list > ports.txt
	//   graphvein index <root> --emit <snapshot.ndjson>   (GRAPHVEIN_SQLITE_PATH outside <root>)
	//   go run scripts/oracle/go_structure/main.go <name> <root> <snapshot.ndjson> ports.txt <out-prefix>
	// Writes <out-prefix>.stats.json and <out-prefix>.disagreements.jsonl.
	if len(os.Args) != 6 {
		fmt.Fprintln(os.Stderr, "usage: oracle <corpus-name> <repo-root> <graphvein.ndjson> <ports.txt> <out-prefix>")
		os.Exit(2)
	}
	corpus = os.Args[1]
	root := os.Args[2]
	loadPorts(os.Args[4])
	g := readGraphvein(os.Args[3])
	st := stats{}
	w := &world{root: root, modules: map[string]*module{}, work: map[string][]string{}, pkgName: map[string]string{}, files: map[string][]string{}, skipped: map[string]string{}, inv: map[string]bool{}}
	// module graph from the go.mod / go.work files on disk (inventory-independent)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.Name() == "go.mod" {
			m := parseGoMod(root, rel)
			w.modules[m.Dir] = m
		}
		if d.Name() == "go.work" {
			b, _ := os.ReadFile(p)
			for _, l := range strings.Split(string(b), "\n") {
				f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "use"))
				for _, x := range f {
					if x != "(" && x != ")" {
						w.work[path.Dir(rel)] = append(w.work[path.Dir(rel)], path.Clean(path.Join(path.Dir(rel), x)))
					}
				}
			}
		}
		return nil
	})
	st["modules_on_disk"] = len(w.modules)
	st["go_work_files"] = len(w.work)
	for _, f := range g.goFiles {
		w.inv[f] = true
	}
	st["go_files"] = len(g.goFiles)
	oracleDecls := map[string]decl{}
	for _, f := range g.goFiles {
		if strings.HasSuffix(f, "_test.go") {
			st["files_test"]++
		}
		if g.precision[f] != "best_effort" {
			st["files_precision_not_best_effort"]++
		}
		p := parseFile(root, f)
		src := p.src
		if strings.Contains(string(src), "\r") {
			st["files_with_cr"]++
		}
		if ok, why := buildable(path.Base(f), src); !ok {
			st["files_not_buildable_"+why]++
		}
		if p.file != nil {
			for _, c := range p.file.Comments {
				for _, l := range c.List {
					if strings.HasPrefix(l.Text, "// Code generated ") && strings.HasSuffix(l.Text, " DO NOT EDIT.") {
						st["files_generated"]++
					}
				}
			}
			for _, imp := range p.imports {
				if imp.Path == "C" {
					st["files_cgo"]++
				}
			}
		}
		if p.err != nil {
			st["files_parse_error"]++
			report("oracle_parse_error", f, fmt.Sprintf("%v; graphvein syntax_error=%v", p.err, g.syntaxErr[f]))
			if p.file == nil {
				continue
			}
		}
		if g.syntaxErr[f] && p.err == nil {
			report("graphvein_syntax_error_oracle_clean", f, "")
		}
		if p.err == nil {
			census(p, f, st)
		}
		st["unexported_values_not_modelled"] += p.unexportedValues
		st["blank_values_not_modelled"] += p.blankValues
		st["local_types_not_modelled"] += p.localTypes
		for _, d := range p.decls {
			if prev, ok := oracleDecls[d.key()]; ok {
				report("oracle_duplicate_identity", f, d.Kind+" "+d.Q+" at "+prev.Span.String()+" and "+d.Span.String())
				st["oracle_duplicate_identity"]++
				continue
			}
			oracleDecls[d.key()] = d
		}
		// imports
		gi := g.imports[f]
		seen := map[span]bool{}
		for _, imp := range p.imports {
			st["imports"]++
			seen[imp.Span] = true
			targets, ok := gi[imp.Span]
			if !ok {
				st["import_no_graphvein_edge"]++
				dir, class := w.resolve(f, imp.Path)
				detail := imp.Path + " (" + class + ")"
				if class == "local" {
					exp := expected(w, f, dir)
					var scanned []string
					for _, e := range exp {
						if w.inv[e] {
							scanned = append(scanned, e)
						}
					}
					detail += fmt.Sprintf(" expected=%d scanned=%d unresolved_import_diag=%d", len(exp), len(scanned), g.unresolved[f])
					if len(scanned) > 0 {
						report("import_edges_missing", f, detail)
						continue
					}
				}
				report("import_without_edge", f, detail)
				continue
			}
			var files []string
			ext := ""
			for _, t := range targets {
				if strings.HasPrefix(t, "file:") {
					files = append(files, strings.TrimPrefix(t, "file:"))
				} else {
					ext = strings.TrimPrefix(t, "external:")
				}
			}
			sort.Strings(files)
			dir, class := w.resolve(f, imp.Path)
			st["import_class_"+class]++
			switch class {
			case "std", "external", "cgo", "no_module":
				if len(files) > 0 {
					report("import_local_edge_for_nonlocal", f, imp.Path+" -> "+strings.Join(files, ","))
					st["import_disagree"]++
				} else if ext != imp.Path {
					report("import_external_name_mismatch", f, imp.Path+" vs "+ext)
					st["import_disagree"]++
				} else {
					st["import_agree_external"]++
				}
			case "local_other_module":
				if ext != "" {
					report("import_local_module_as_external", f, imp.Path+" -> module dir "+dir)
					st["import_underresolved"]++
				} else {
					st["import_other_module_files"]++
				}
			case "local", "nested_module":
				if ext != "" {
					report("import_local_as_external", f, imp.Path)
					st["import_disagree"]++
					continue
				}
				if class == "nested_module" {
					// The directory belongs to another module that the
					// importer's go.mod neither replaces locally nor shares
					// a go.work with: the go command would read the
					// published version, not these files.
					report("import_into_nested_module", f, imp.Path+" -> "+strings.Join(files, ","))
					st["import_unproven_nested"]++
				}
				exp := expected(w, f, dir)
				expSet := map[string]bool{}
				for _, e := range exp {
					expSet[e] = true
				}
				gotSet := map[string]bool{}
				agree := true
				for _, x := range files {
					gotSet[x] = true
					if !expSet[x] {
						agree = false
						why := w.skipped[x]
						if why == "" {
							if strings.HasSuffix(x, "_test.go") {
								why = "test_file_seen_from_other_dir"
								if path.Dir(f) == dir {
									why = "test_file_of_importer_own_test_package"
								}
							} else {
								why = "unknown"
							}
						}
						if class == "nested_module" {
							why = "nested_module/" + why
						}
						report("import_extra_edge:"+why, f, imp.Path+" -> "+x)
						st["import_extra_edges"]++
					}
				}
				for _, e := range exp {
					if !gotSet[e] {
						agree = false
						why := "missing"
						if !w.inv[e] {
							why = "missing_unscanned"
						}
						report("import_missing_edge:"+why, f, imp.Path+" -> "+e)
						st["import_missing_edges"]++
					}
				}
				st["import_edges_compared"] += len(files)
				if agree {
					st["import_agree_local"]++
				} else {
					st["import_disagree_local_specs"]++
				}
			}
		}
		for sp := range gi {
			if !seen[sp] {
				report("graphvein_import_no_oracle_spec", f, sp.String())
				st["import_disagree"]++
			}
		}
	}
	// declarations
	for k, d := range oracleDecls {
		st["decl_oracle_"+d.Kind]++
		gd, ok := g.decls[k]
		if !ok {
			report("decl_missing_in_graphvein", d.File, d.Kind+" "+d.Q+" "+d.Span.String())
			st["decl_missing"]++
			continue
		}
		st["decl_matched"]++
		if gd.Name != d.Name {
			report("decl_name_mismatch", d.File, d.Q+": "+gd.Name+" vs "+d.Name)
			st["decl_disagree_name"]++
		}
		if gd.Span != d.Span {
			report("decl_span_mismatch", d.File, d.Kind+" "+d.Q+": graphvein "+gd.Span.String()+" oracle "+d.Span.String())
			st["decl_disagree_span"]++
		}
		if gd.Hash != d.Hash {
			st["decl_disagree_hash"]++
			if gd.Span == d.Span {
				report("decl_hash_mismatch_same_span", d.File, d.Q)
			}
		}
		if gd.Name == d.Name && gd.Span == d.Span && gd.Hash == d.Hash {
			st["decl_agree_all"]++
		}
	}
	for k, d := range g.decls {
		if _, ok := oracleDecls[k]; !ok {
			report("decl_extra_in_graphvein", d.File, d.Kind+" "+d.Q+" "+d.Span.String())
			st["decl_extra"]++
		}
		st["decl_graphvein_"+d.Kind]++
	}
	for k, n := range g.dupDecl {
		report("graphvein_duplicate_identity", "", fmt.Sprintf("%q x%d", k, n+1))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Detail < out[j].Detail
	})
	prefix := os.Args[5]
	jf, _ := os.Create(prefix + ".disagreements.jsonl")
	enc := json.NewEncoder(jf)
	for _, r := range out {
		enc.Encode(r)
	}
	jf.Close()
	sf, _ := os.Create(prefix + ".stats.json")
	b, _ := json.MarshalIndent(st, "", " ")
	sf.Write(b)
	sf.Close()
	classes := map[string]int{}
	for _, r := range out {
		classes[r.Class]++
	}
	b2, _ := json.MarshalIndent(classes, "", " ")
	fmt.Printf("%s\nstats: %s\nclasses: %s\n", corpus, b, b2)
}

func census(p *parsed, f string, st stats) {
	src := string(p.src)
	if strings.HasPrefix(src, "\xef\xbb\xbf") {
		st["census_file_bom"]++
	}
	if strings.Contains(src, "\n//line ") || strings.Contains(src, "/*line ") {
		st["census_file_line_directive"]++
	}
	if strings.HasSuffix(p.file.Name.Name, "_test") {
		st["census_file_external_test_package"]++
	}
	if p.file.Name.Name == "main" {
		st["census_file_package_main"]++
	}
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "package ") {
			break
		}
		if constraint.IsGoBuild(t) {
			st["census_file_go_build_line"]++
		}
	}
	for _, d := range p.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				st["census_func_no_body"]++
			}
			if d.Type.TypeParams != nil {
				st["census_func_generic"]++
			}
			if d.Recv == nil {
				if d.Name.Name == "init" {
					st["census_func_init"]++
				} else if d.Name.Name == "_" {
					st["census_func_blank"]++
				} else {
					st["census_func_plain"]++
				}
				continue
			}
			t := d.Recv.List[0].Type
			ptr := false
			if s, ok := t.(*ast.StarExpr); ok {
				ptr, t = true, s.X
			}
			switch t.(type) {
			case *ast.Ident:
				if ptr {
					st["census_method_pointer_receiver"]++
				} else {
					st["census_method_value_receiver"]++
				}
			case *ast.IndexExpr, *ast.IndexListExpr:
				st["census_method_generic_receiver"]++
			default:
				st["census_method_other_receiver_form"]++
			}
		case *ast.GenDecl:
			grouped := d.Lparen.IsValid()
			for _, sp := range d.Specs {
				switch s := sp.(type) {
				case *ast.ImportSpec:
					switch {
					case s.Path.Value == "\"C\"":
						st["census_import_cgo_C"]++
					case s.Name == nil:
						st["census_import_plain"]++
					case s.Name.Name == "_":
						st["census_import_blank"]++
					case s.Name.Name == ".":
						st["census_import_dot"]++
					default:
						st["census_import_alias"]++
					}
					if grouped {
						st["census_import_in_group"]++
					}
				case *ast.TypeSpec:
					if s.TypeParams != nil {
						st["census_type_generic"]++
					}
					if grouped {
						st["census_type_in_group"]++
					}
				case *ast.ValueSpec:
					if len(s.Names) > 1 {
						st["census_value_multi_name_spec"]++
					}
					if grouped {
						st["census_value_in_group"]++
					}
				}
			}
		}
	}
}
