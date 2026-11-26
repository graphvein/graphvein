//go:build ignore

// copytypes copies Go model packages into the scratch oracle module so
// that GORM can parse their structs without running any of their code
// (T018g5). Per file it keeps the type declarations, the constant
// declarations, the variables whose values are all literals, and the
// methods GORM's schema parse calls (TableName, Value, Scan, GormDataType,
// GormDBDataType); every other function and variable is dropped, and with
// them every side effect, init function and unrelated dependency. Imports
// are kept only when a kept declaration uses them; -rewrite maps an import
// path prefix (the module's own path) to the scratch one. Build constraint
// lines and an import of "C" are kept, so the go command selects the
// copies as it selects the originals.
//
// Usage:
//
//	copytypes -src DIR -dst DIR -rewrite old=new[,old=new] PKGDIR...
//
// PKGDIR is a package directory relative to -src, or one .go file of
// one (a model file whose package holds other, unrelated types); "..."
// copies every package below -src (skipping testdata, vendor and directories whose name
// starts with "." or "_", as the go command does). _test.go files are
// never copied.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// gormMethods are the methods GORM's schema parse reads.
var gormMethods = map[string]bool{"TableName": true, "Value": true, "Scan": true, "GormDataType": true, "GormDBDataType": true}

func main() {
	src := flag.String("src", "", "source directory")
	dst := flag.String("dst", "", "destination directory")
	rewrite := flag.String("rewrite", "", "old=new[,old=new] import path prefixes")
	flag.Parse()
	if *src == "" || *dst == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "copytypes: -src, -dst and at least one package directory are required")
		os.Exit(2)
	}
	rw := map[string]string{}
	for _, p := range strings.Split(*rewrite, ",") {
		if kv := strings.SplitN(p, "=", 2); len(kv) == 2 {
			rw[kv[0]] = kv[1]
		}
	}
	var dirs []string
	for _, a := range flag.Args() {
		if a != "..." {
			dirs = append(dirs, a)
			continue
		}
		err := filepath.WalkDir(*src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			name := d.Name()
			if p != *src && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(*src, p)
			dirs = append(dirs, rel)
			return nil
		})
		if err != nil {
			fail(err)
		}
	}
	n := 0
	for _, dir := range dirs {
		if strings.HasSuffix(dir, ".go") {
			if err := copyFile(filepath.Join(*src, dir), filepath.Join(*dst, dir), rw); err != nil {
				fail(err)
			}
			n++
			continue
		}
		entries, err := os.ReadDir(filepath.Join(*src, dir))
		if err != nil {
			fail(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if err := copyFile(filepath.Join(*src, dir, name), filepath.Join(*dst, dir, name), rw); err != nil {
				fail(err)
			}
			n++
		}
	}
	fmt.Fprintf(os.Stderr, "copytypes: %d files copied from %s\n", n, *src)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "copytypes:", err)
	os.Exit(1)
}

// literal reports whether every value of a var spec is a literal.
func literal(vs *ast.ValueSpec) bool {
	if len(vs.Values) == 0 {
		return vs.Type != nil
	}
	for _, v := range vs.Values {
		if u, ok := v.(*ast.UnaryExpr); ok && u.Op == token.SUB {
			v = u.X
		}
		if _, ok := v.(*ast.BasicLit); !ok {
			return false
		}
	}
	return true
}

var majorRE = regexp.MustCompile(`^v[0-9]+$`)

// localName is the name an import binds when it has no explicit name.
func localName(p string) string {
	base := path.Base(p)
	if majorRE.MatchString(base) {
		base = path.Base(path.Dir(p))
	}
	if i := strings.Index(base, ".v"); i > 0 {
		base = base[:i]
	}
	return strings.ReplaceAll(base, "-", "")
}

func copyFile(from, to string, rw map[string]string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, from, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", from, err)
	}
	var kept []ast.Decl
	used := map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE, token.CONST:
			case token.VAR:
				var specs []ast.Spec
				for _, s := range d.Specs {
					if literal(s.(*ast.ValueSpec)) {
						specs = append(specs, s)
					}
				}
				if len(specs) == 0 {
					continue
				}
				d.Specs = specs
			default:
				continue
			}
		case *ast.FuncDecl:
			if d.Recv == nil || !gormMethods[d.Name.Name] {
				continue
			}
		}
		kept = append(kept, d)
		ast.Inspect(d, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := s.X.(*ast.Ident); ok {
					used[id.Name] = true
				}
			}
			return true
		})
	}
	var buf bytes.Buffer
	// The file's build constraint lines are kept, and so is an import of
	// "C", so that the go command building the scratch module selects the
	// copied files as it selects the originals (an ignored generator, a
	// cgo file under CGO_ENABLED=0, another port's file stay out): the
	// oracle never shares Graphvein's file selection.
	header, err := parser.ParseFile(token.NewFileSet(), from, nil, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing the header of %s: %w", from, err)
	}
	for _, g := range header.Comments {
		if g.End() >= header.Package {
			break
		}
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "//go:build ") || strings.HasPrefix(c.Text, "// +build ") {
				buf.WriteString(c.Text + "\n")
			}
		}
	}
	if buf.Len() > 0 {
		buf.WriteString("\n")
	}
	fmt.Fprintf(&buf, "// Copied from %s for the scratch GORM oracle: types, constants,\n// literal variables and GORM's schema methods only.\n\npackage %s\n\n", filepath.ToSlash(from), f.Name.Name)
	var imports []string
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := localName(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		if !used[name] && p != "C" {
			continue
		}
		for old, repl := range rw {
			if p == old || strings.HasPrefix(p, old+"/") {
				p = repl + strings.TrimPrefix(p, old)
			}
		}
		line := strconv.Quote(p)
		if im.Name != nil {
			line = im.Name.Name + " " + line
		}
		imports = append(imports, line)
	}
	if len(imports) > 0 {
		buf.WriteString("import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n")
	}
	for _, d := range kept {
		if err := format.Node(&buf, fset, d); err != nil {
			return err
		}
		buf.WriteString("\n\n")
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("formatting the copy of %s: %w", from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, out, 0o644)
}
