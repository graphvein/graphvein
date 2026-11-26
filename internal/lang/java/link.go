package java

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Link forwards each file's containment edges and resolves its import
// directives against the current inventory of scanned Java files (rows
// JV-I2 to JV-I8). Resolution assumes a compiling repository (assumption
// V1): a fully qualified type name declared by exactly one scanned file
// names that file. Refused (BE-2) and failed (BE-3) files declare nothing
// that resolution can see, so an import one of them may declare (any import
// for a failed file, one in or under a refused file's package otherwise)
// never becomes an external node (row BE-8).
func Link(files map[string]facts.Facts) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return link(files, nil)
}

func link(files map[string]facts.Facts, t tracer) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	declared := map[string][]string{}
	packages := map[string]bool{}
	paths := facts.Paths(files)
	hidden := besteffort.Hidden(files)
	for _, file := range paths {
		p := files[file].BestEffort
		if !p.Valid() || p.Refused || p.Package == "" {
			continue
		}
		packages[p.Package] = true
		for _, typ := range p.Types {
			fqn := p.Package + "." + typ
			if list := declared[fqn]; len(list) == 0 || list[len(list)-1] != file {
				declared[fqn] = append(list, file)
			}
		}
	}
	for _, file := range paths {
		f := files[file]
		edges = append(edges, f.Edges...)
		p := f.BestEffort
		if !p.Valid() || p.Refused {
			continue
		}
		source := facts.ID("file", file, file)
		for _, imp := range p.Imports {
			// The type an import names: the whole name, except a single
			// static import, which names a member of the type.
			typ := imp.Name
			if imp.Static && !imp.Star {
				if i := strings.LastIndexByte(typ, '.'); i > 0 {
					typ = typ[:i]
				}
			}
			written := imp.Name
			if imp.Star {
				written += ".*"
			}
			shown := written
			if imp.Static {
				shown = "static " + written
			}
			switch candidates := declared[typ]; {
			case len(candidates) == 1:
				switch {
				case imp.Static:
					t.trace("JV-I3")
				case imp.Star:
					t.trace("JV-I4")
				default:
					t.trace("JV-I2")
				}
				if candidates[0] != file {
					edges = append(edges, facts.Edge("imports", source, facts.ID("file", candidates[0], candidates[0]), file, imp.Span))
				}
			case len(candidates) > 1:
				t.trace("JV-I5")
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "ambiguous_import", Message: "import " + shown + " has candidates: " + strings.Join(candidates, ", ")})
			case imp.Star && !imp.Static && packages[typ]:
				t.trace("JV-I6")
			case repositoryPackage(typ, packages) != "":
				t.trace("JV-I7")
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "import " + shown + " is not declared by a scanned file, but package " + repositoryPackage(typ, packages) + " is"})
			case len(besteffort.Hiding(hidden, typ, false, false)) > 0:
				t.trace("BE-8")
				diagnostics = append(diagnostics, besteffort.HiddenImport(file, "import "+shown, besteffort.Hiding(hidden, typ, false, false)))
			default:
				t.trace("JV-I8")
				target := facts.External(nodes, "java", written)
				edges = append(edges, facts.Edge("imports", source, target, file, imp.Span))
			}
		}
	}
	external, merged := facts.Finish(nodes, edges)
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].File < diagnostics[j].File })
	return external, merged, diagnostics
}

// repositoryPackage returns the longest of name and its dotted prefixes that
// a scanned file declares as its package (JV-I7), or "".
func repositoryPackage(name string, packages map[string]bool) string {
	for {
		if packages[name] {
			return name
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			return ""
		}
		name = name[:i]
	}
}
