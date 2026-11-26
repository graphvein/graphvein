package csharp

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Link forwards each file's containment edges and classifies its using
// directives against the current inventory of scanned C# files (rows CS-I1,
// CS-I2). A using never becomes a file edge: a namespace spans many files
// and assemblies (T021 open point O8 (a)). A using that may name something
// the repository declares claims nothing; any other becomes an external
// `dotnet:` node, unless it sits inside a namespace, where its name may be
// relative (CS-I3), a proper prefix of two or more segments of its name is
// the repository's, so a non-C# source may declare it (CS-I4, a warning), or
// a refused or failed C# file may declare it (row BE-8: any name for a
// failed file or a refused one whose text proves nothing; else names in,
// under or above the namespaces its text names, and for a file that may
// also declare global-namespace types, every static and alias using).
func Link(files map[string]facts.Facts) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return link(files, nil)
}

// inventory is what the repository's readable C# files declare.
type inventory struct {
	// namespaces holds every declared namespace and each dotted prefix of
	// one; declared holds the declared namespaces only.
	namespaces, declared map[string]bool
	// global holds the type names declared in the global namespace.
	global map[string]bool
}

func link(files map[string]facts.Facts, t tracer) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	inv := inventory{namespaces: map[string]bool{}, declared: map[string]bool{}, global: map[string]bool{}}
	paths := facts.Paths(files)
	for _, file := range paths {
		p := files[file].BestEffort
		if !p.Valid() || p.Refused {
			continue
		}
		for _, ns := range p.Namespaces {
			inv.declared[ns] = true
			for prefix := ns; ; {
				inv.namespaces[prefix] = true
				i := strings.LastIndexByte(prefix, '.')
				if i < 0 {
					break
				}
				prefix = prefix[:i]
			}
		}
		for _, name := range p.GlobalTypes {
			inv.global[name] = true
		}
	}
	hidden := besteffort.Hidden(files)
	for _, file := range paths {
		f := files[file]
		edges = append(edges, f.Edges...)
		p := f.BestEffort
		if !p.Valid() || p.Refused {
			continue
		}
		source := facts.ID("file", file, file)
		for _, imp := range p.Imports {
			if inv.repository(imp) {
				t.trace("CS-I1")
				continue
			}
			if imp.Scope != "" {
				// Inside a namespace the name may be relative to an
				// enclosing namespace that a referenced assembly declares,
				// so the written name is not provably the external one.
				t.trace("CS-I3")
				continue
			}
			if ns := inv.enclosing(imp); ns != "" {
				t.trace("CS-I4")
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: shownUsing(imp) + " is not declared by a scanned C# file, but the repository declares namespace " + ns + " or one under it"})
				continue
			}
			if hiding := besteffort.Hiding(hidden, imp.Name, true, imp.Static || imp.Alias); len(hiding) > 0 {
				t.trace("BE-8")
				diagnostics = append(diagnostics, besteffort.HiddenImport(file, shownUsing(imp), hiding))
				continue
			}
			t.trace("CS-I2")
			target := facts.External(nodes, "dotnet", imp.Name)
			edges = append(edges, facts.Edge("imports", source, target, file, imp.Span))
		}
	}
	external, merged := facts.Finish(nodes, edges)
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].File < diagnostics[j].File })
	return external, merged, diagnostics
}

// repository reports whether a using may name something the repository
// declares (row CS-I1). It tries the name as written and, for a using inside
// a namespace, relative to that namespace and each enclosing one (C# looks
// names up there first). A namespace target (a plain or alias using) is the
// repository's when it is a declared namespace or a prefix of one. A type
// target (a static or alias using) may be the repository's when the
// namespace it would sit in, any proper prefix of the name, is declared;
// when the name would sit in the global namespace, when its first segment
// is a type the repository declares there.
func (inv inventory) repository(imp facts.BestEffortImport) bool {
	scopes := []string{}
	for scope := imp.Scope; scope != ""; {
		scopes = append(scopes, scope)
		i := strings.LastIndexByte(scope, '.')
		if i < 0 {
			break
		}
		scope = scope[:i]
	}
	scopes = append(scopes, "")
	for _, scope := range scopes {
		if !imp.Static && inv.namespaces[facts.Join(scope, imp.Name)] {
			return true
		}
		if !imp.Static && !imp.Alias {
			continue
		}
		if scope == "" {
			first := imp.Name
			if len(imp.Prefixes) > 0 {
				first = imp.Prefixes[0]
			}
			if i := strings.IndexByte(first, '<'); i >= 0 {
				first = first[:i]
			}
			if inv.global[first] {
				return true
			}
		} else if inv.declared[scope] {
			return true
		}
		for _, prefix := range imp.Prefixes {
			if inv.declared[facts.Join(scope, prefix)] {
				return true
			}
		}
	}
	return false
}

// enclosing returns the longest proper dotted prefix of two or more segments
// of a using's name that is a declared namespace or a prefix of one (row
// CS-I4), or "". The repository may declare the name itself outside .cs
// files, which the inventory never sees: Razor components take their
// namespace from the project's root namespace and folder, and .proto files
// and source generators add namespaces at build time. A one-segment prefix
// (`Microsoft`, `System`) is too broad to say anything about the name.
func (inv inventory) enclosing(imp facts.BestEffortImport) string {
	for i := len(imp.Prefixes) - 1; i >= 1; i-- {
		if inv.namespaces[imp.Prefixes[i]] {
			return imp.Prefixes[i]
		}
	}
	return ""
}

// shownUsing is a using as diagnostics quote it.
func shownUsing(imp facts.BestEffortImport) string {
	if imp.Static {
		return "using static " + imp.Name
	}
	return "using " + imp.Name
}
