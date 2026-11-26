package dart

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// scheme matches a URI that starts with a scheme (`http:`, `file:`, ...).
var scheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// Link forwards each file's containment edges and resolves its directives
// against the scanned Dart files and the package names of the scanned
// pubspec.yaml files (from PackageNames): `dart:` libraries become external
// nodes (DA-I1), relative URIs and the file's own package resolve to
// scanned files (DA-I2, DA-I3), other packages of the repository claim
// nothing (DA-I4) and every other package is an external `pub:` node
// (DA-I5). A file belongs to the package of its nearest enclosing
// pubspec.yaml (assumption V2). Row BE-8 does not apply: no Dart file
// declares a package or library name an import resolves through.
func Link(files map[string]facts.Facts, packages map[string]string) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return link(files, packages, nil)
}

func link(files map[string]facts.Facts, packages map[string]string, t tracer) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	names := map[string]bool{}
	unnamed := false
	for _, name := range packages {
		if name == "" {
			unnamed = true
		}
		names[name] = true
	}
	for _, file := range facts.Paths(files) {
		f := files[file]
		edges = append(edges, f.Edges...)
		p := f.BestEffort
		if !p.Valid() || p.Refused {
			continue
		}
		source := facts.ID("file", file, file)
		warn := func(imp facts.BestEffortImport, why string) {
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: imp.Directive + " '" + imp.Name + "' " + why})
		}
		edge := func(imp facts.BestEffortImport, target string) {
			if target != file {
				edges = append(edges, facts.Edge("imports", source, facts.ID("file", target, target), file, imp.Span))
			}
		}
		for _, imp := range p.Imports {
			uri := imp.Name
			switch {
			case strings.HasPrefix(uri, "dart:"):
				lib := strings.TrimPrefix(uri, "dart:")
				if !packageName.MatchString(lib) {
					t.trace("DA-I8")
					continue
				}
				t.trace("DA-I1")
				edges = append(edges, facts.Edge("imports", source, facts.External(nodes, "dart", lib), file, imp.Span))
			case strings.HasPrefix(uri, "package:"):
				rest := strings.TrimPrefix(uri, "package:")
				i := strings.IndexByte(rest, '/')
				if i <= 0 || !packageName.MatchString(rest[:i]) || i == len(rest)-1 || strings.ContainsAny(rest, "?#") {
					t.trace("DA-I8")
					continue
				}
				name, rel := rest[:i], rest[i+1:]
				dir, own, found := nearest(file, packages)
				switch {
				case found && own == name:
					decoded, err := url.PathUnescape(rel)
					if err != nil {
						t.trace("DA-I8")
						continue
					}
					t.trace("DA-I3")
					lib := path.Join(dir, "lib")
					target := path.Join(lib, decoded)
					if !strings.HasPrefix(target, lib+"/") {
						warn(imp, "resolves outside package "+name+"'s lib directory ("+lib+")")
						continue
					}
					if _, ok := files[target]; !ok {
						warn(imp, "does not name a scanned file (package "+name+" is "+dir+")")
						continue
					}
					edge(imp, target)
				case (found && own == "") || names[name] || unnamed:
					// Another package of the repository (a workspace member
					// or path dependency, not provable without reading its
					// dependencies), or a package whose name is unknown.
					t.trace("DA-I4")
				default:
					t.trace("DA-I5")
					edges = append(edges, facts.Edge("imports", source, facts.External(nodes, "pub", name), file, imp.Span))
				}
			case scheme.MatchString(uri) || strings.ContainsAny(uri, "?#"):
				t.trace("DA-I8")
			default:
				decoded, err := url.PathUnescape(uri)
				if err != nil || decoded == "" {
					t.trace("DA-I8")
					continue
				}
				t.trace("DA-I2")
				target := path.Join(path.Dir(file), decoded)
				if strings.HasPrefix(decoded, "/") || target == ".." || strings.HasPrefix(target, "../") {
					warn(imp, "resolves outside the repository")
					continue
				}
				if _, ok := files[target]; !ok {
					warn(imp, "does not name a scanned file")
					continue
				}
				edge(imp, target)
			}
		}
	}
	external, merged := facts.Finish(nodes, edges)
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].File < diagnostics[j].File })
	return external, merged, diagnostics
}

// nearest returns the directory and package name of the pubspec.yaml
// nearest above file (its own directory first), and whether there is one.
func nearest(file string, packages map[string]string) (dir, name string, found bool) {
	for dir = path.Dir(file); ; dir = path.Dir(dir) {
		if name, ok := packages[dir]; ok {
			return dir, name, true
		}
		if dir == "." || dir == "/" {
			return "", "", false
		}
	}
}
