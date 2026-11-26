package python

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file holds the Python structural claim contract
// (docs/lang/python-structure.md, ADR 0069): the row table, the forms of
// the default branch, the reference-Python rejection forms, declaration
// span ends and the structural import resolution. TestPythonStructureContract
// keeps structureRows, the document's tables and the fixture
// testdata/fixtures/python-structure 1:1; TestPythonStructureOracle compares
// every claimed fact with scripts/oracle/python_structure/oracle.py, which
// reads the raw bytes with CPython's own ast, tokenize and importlib rules.

// structureRow is one row of the Python structural claim contract.
type structureRow struct {
	// ID is the row id in docs/lang/python-structure.md.
	ID string
	// Claim says in a few words what the row claims ("nothing" for a
	// refusal row).
	Claim string
}

// structureRows is the Go side of the contract tables, in document order.
// "D" is the single default branch: every form no row reads.
var structureRows = []structureRow{
	{"PS-M1", "the search path: Graphvein's documented root policy"},
	{"PS-M2", "a workspace member's declared packages, one search path entry after the ancestor roots"},
	{"PS-F1", "file record and file node"},
	{"PS-F2", "nothing beyond the file: declarations and imports go to D"},
	{"PS-F3", "as PS-F1 plus declarations and imports (code as written)"},
	{"PS-D1", "function node and contains edge"},
	{"PS-D2", "method node and contains edge"},
	{"PS-D3", "class node and contains edge"},
	{"PS-D4", "overload signature in the qualified name"},
	{"PS-D5", "positional @2, @3 suffixes"},
	{"PS-D6", "attributes.decorators"},
	{"PS-D7", "constant node and contains edge"},
	{"PS-I1", "imports edge to the leaf module's file"},
	{"PS-I2", "imports edges to the module's file and to the modules the names bind"},
	{"PS-I3", "nothing: relative import beyond the top-level package"},
	{"PS-I4", "imports edge to external:.#pypi:<top-level name>"},
	{"PS-I5", "imports edge to external:.#python-stdlib:<top-level name>"},
	{"PS-I6", "edges only to submodules a from-name binds (namespace package)"},
	{"PS-I7", "no edge; unresolved_import or ambiguous_import warning"},
	{"D", "nothing; in-scope forms counted in python_structure_unread"},
}

// Default-branch forms, the keys of facts.PythonFacts.Unread and of the
// python_structure_unread diagnostic.
const (
	unreadSyntaxError        = "syntax_error"
	unreadAnnotationOnly     = "annotation_only"
	unreadStarredTarget      = "starred_target"
	unreadShadowed           = "shadowed_module"
	unreadUndecidedName      = "undecided_name"
	unreadSelfImport         = "self_import"
	unreadRelativeBeyondTop  = "relative_beyond_top"
	unreadUnresolvedInRepo   = "unresolved_in_repository"
	unreadFutureImport       = "future_import"
	unreadSelfBoundSubmodule = "submodule_after_self_binding"
	unreadDottedParents      = "dotted_parent_packages"
	unreadNamespaceHides     = "namespace_hides_concrete"
	unreadScriptDirectory    = "script_directory_module"
	unreadInterpreterModule  = "interpreter_module"
	unreadPlatformModule     = "platform_module"
)

// unreadRows maps every default-branch form to the one contract row whose
// refusal (or, for the counted-only forms, whose claim) sends it to D. The
// document's "Default branch D" list names each form with this row;
// TestPythonStructureContract checks both, and that every counting site
// reports this row.
var unreadRows = map[string]string{
	unreadSyntaxError:        "PS-F2",
	unreadAnnotationOnly:     "PS-D7",
	unreadStarredTarget:      "PS-D7",
	unreadFutureImport:       "PS-I2",
	unreadShadowed:           "PS-I1",
	unreadSelfImport:         "PS-I2",
	unreadUndecidedName:      "PS-I2",
	unreadSelfBoundSubmodule: "PS-I2",
	unreadRelativeBeyondTop:  "PS-I3",
	unreadUnresolvedInRepo:   "PS-I4",
	unreadDottedParents:      "PS-I1",
	unreadNamespaceHides:     "PS-I6",
	unreadScriptDirectory:    "PS-M1",
	unreadInterpreterModule:  "PS-I4",
	unreadPlatformModule:     "PS-I5",
}

// unreadForms lists every default-branch form in document order.
var unreadForms = []string{unreadSyntaxError, unreadAnnotationOnly, unreadStarredTarget, unreadFutureImport, unreadShadowed, unreadSelfImport, unreadUndecidedName, unreadSelfBoundSubmodule, unreadRelativeBeyondTop, unreadUnresolvedInRepo, unreadDottedParents, unreadNamespaceHides, unreadScriptDirectory, unreadInterpreterModule, unreadPlatformModule}

// External ecosystems of unresolved absolute imports (rows PS-I4, PS-I5):
// the one place the ecosystem is chosen.
const (
	ecosystemPyPI   = "pypi"
	ecosystemStdlib = "python-stdlib"
)

// referencePython is the Python version whose grammar decides which files
// claim their structure (row PS-F2). Syntax newer than it is rejected;
// syntax it removed is rejected. Version-dependent forms below test it.
const referencePython = "3.14"

// referenceAtLeast reports whether the reference Python is at least
// major.minor (both versions are "3.N").
func referenceAtLeast(minor int) bool {
	var major, have int
	fmt.Sscanf(referencePython, "%d.%d", &major, &have)
	return major > 3 || major == 3 && have >= minor
}

// structureTagHook is a test-only hook: every branch of the contract reports
// its row id, and every counting site of D also "D/<form>". It is nil in
// production.
var structureTagHook func(row string)

func structureTag(row string) {
	if structureTagHook != nil {
		structureTagHook(row)
	}
}

// declEnd returns where the reference parser's node ends for a def or class
// (rows PS-D1..PS-D3 spans): the end of its last statement, including a
// `;` after it on the same line, without the comments tree-sitter's block
// keeps after it.
func declEnd(n *parse.Node, source []byte) (row, column, offset uint32) {
	for {
		j := len(n.Children) - 1
		for j >= 0 && n.Children[j].Kind == "comment" {
			j--
		}
		if j < 0 {
			return n.EndRow, n.EndColumn, n.End
		}
		c := n.Children[j]
		if c.End == n.End {
			n = c
			continue
		}
		semicolon, ok := trivia(source, c.End, n.End)
		if !ok {
			return n.EndRow, n.EndColumn, n.End
		}
		if semicolon > 0 {
			// The statement's `;` belongs to the node; it is on c's last
			// line (a line end before it would end the statement).
			return c.EndRow, c.EndColumn + semicolon, c.End + semicolon
		}
		n = c
	}
}

// trivia reports that source[from:to] holds only white space, line ends,
// comments and semicolons, and returns the length up to and including a
// `;` on the first line (0 when there is none).
func trivia(source []byte, from, to uint32) (semicolon uint32, ok bool) {
	if int(to) > len(source) || from > to {
		return 0, false
	}
	comment, firstLine := false, true
	for i, c := range source[from:to] {
		switch {
		case comment:
			if c == '\n' {
				comment = false
				firstLine = false
			}
		case c == '#':
			comment = true
		case c == ';':
			if !firstLine {
				return 0, false
			}
			semicolon = uint32(i) + 1
		case c == '\n' || c == '\r':
			firstLine = false
		case c == ' ' || c == '\t' || c == '\f':
		default:
			return 0, false
		}
	}
	return semicolon, true
}

// addDecl records a declaration node from start's first byte to the end of
// def (declEnd), with its contains edge.
func addDecl(out *facts.Facts, file string, source []byte, start, def *parse.Node, kind, name, qualified, owner string, attrs map[string]any) string {
	row, column, offset := declEnd(def, source)
	span := contract.Span{StartLine: int(start.Row) + 1, StartColumn: int(start.Column), EndLine: int(row) + 1, EndColumn: int(column)}
	identity := facts.ID(kind, file, qualified)
	if owner == "" {
		owner = facts.ID("file", file, file)
	}
	out.Nodes = append(out.Nodes, contract.Node{Type: "node", Identity: identity, Kind: kind, Level: 4, Name: name, QualifiedName: qualified, File: file, Span: &span, ContentHash: contract.ContentHash(source[start.Start:offset]), Attributes: attrs})
	out.Edges = append(out.Edges, facts.Edge("contains", owner, identity, file, span))
	return identity
}

// refuse applies row PS-F2: the file claims nothing beyond its file node.
// Its declarations and imports go to D, counted as syntax_error; its calls,
// routes, models and bindings go with them, so no other file resolves a
// name through it.
func refuse(f facts.Facts, file, form string) facts.Facts {
	structureTag("PS-F2")
	n := len(f.Nodes) + len(f.Imports)
	if f.Python != nil {
		if f.Python.Rejected {
			return f
		}
		for _, c := range f.Python.Unread {
			n += c
		}
	}
	unread := map[string]int{}
	if n > 0 {
		count(unread, unreadSyntaxError, n)
	}
	var diagnostics []contract.Diagnostic
	for _, d := range f.Diagnostics {
		if d.Code == "python_syntax_rejected" {
			continue
		}
		diagnostics = append(diagnostics, d)
	}
	if form != rejectTreeSitterError {
		diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "python_syntax_rejected", Message: fmt.Sprintf("Python %s rejects this file (%s); its structure claims nothing", referencePython, form)})
	}
	return facts.Facts{
		Exports:     map[string]string{},
		Diagnostics: diagnostics,
		Python:      &facts.PythonFacts{Scopes: []facts.PythonScope{{Parent: -1, Bindings: map[string]facts.PythonBinding{}}}, Rejected: true, Unread: unread},
	}
}

// RefuseSyntaxError applies row PS-F2 to a file whose tree tree-sitter
// reports an error in (the indexer's parse.Result.SyntaxError, which also
// sees MISSING tokens the detached tree omits).
func RefuseSyntaxError(f facts.Facts, file string) facts.Facts {
	return refuse(f, file, rejectTreeSitterError)
}

// unreadDiagnostic reports a file's default-branch forms: one info
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
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "python_structure_unread", Message: fmt.Sprintf("%d Python declarations or imports are outside the structural claim contract and claim nothing (%s)", total, strings.Join(parts, ", "))}}
}

func nameSet(lists ...string) map[string]bool {
	out := map[string]bool{}
	for _, list := range lists {
		for _, name := range strings.Fields(list) {
			out[name] = true
		}
	}
	return out
}

var (
	// stdlibSet holds the python-stdlib names (row PS-I5): the reference
	// Python's sys.stdlib_module_names, the meta-path names and the
	// packages CPython ships beside them (metapath.go).
	stdlibSet = nameSet(stdlibNames, metaPathNames, shippedModuleNames)
	// metaPathSet: names the meta path serves before any sys.path entry.
	metaPathSet = nameSet(metaPathNames)
	// platformSet: names whose loading depends on the build.
	platformSet = nameSet(platformModuleNames)
	// interpreterSet: modules the interpreter itself provides or loads at
	// start-up from a place the repository cannot show: __main__ is the
	// running program; site imports sitecustomize and usercustomize from
	// sys.path before the script directory is on it.
	interpreterSet = nameSet("__main__ sitecustomize usercustomize")
)

// externalEcosystem is the ecosystem of an unresolved absolute import's
// top-level name (rows PS-I4, PS-I5).
func externalEcosystem(top string) string {
	if stdlibSet[top] {
		return ecosystemStdlib
	}
	return ecosystemPyPI
}

// entryKind is what one path entry holds for a name, in FileFinder's order.
type entryKind int

const (
	foundNone entryKind = iota
	foundPackage
	foundModule
	foundNamespace
)

// target is a module the structural resolution found: its file (empty for
// a namespace package) and its __path__ (empty for a plain module).
type target struct {
	file string
	dirs []string
}

func (t target) ok() bool { return t.file != "" || len(t.dirs) > 0 }

// classify returns what directory dir holds for name as Graphvein reads it
// (scanned sources only): a regular package, a source module, a namespace
// directory (any directory: Python needs no Python file in it) or nothing.
func (r *ModuleResolver) classify(dir, name string) entryKind {
	p := path.Join(dir, name)
	if _, ok := r.files[path.Join(p, "__init__.py")]; ok {
		return foundPackage
	}
	if _, ok := r.files[p+".py"]; ok {
		return foundModule
	}
	if r.directories[p] {
		return foundNamespace
	}
	return foundNone
}

// loaderKind sorts a file name next to a module name into the loader
// suffix classes of FileFinder: 1 extension module, 2 source, 3 bytecode,
// 0 none.
func loaderKind(file, name string) int {
	if !strings.HasPrefix(file, name+".") {
		return 0
	}
	rest := file[len(name):]
	switch {
	case strings.HasSuffix(rest, ".so") || strings.HasSuffix(rest, ".pyd"):
		return 1
	case rest == ".py" || rest == ".pyw":
		return 2
	case rest == ".pyc":
		return 3
	}
	return 0
}

// shadowed reports that dir holds an entry Graphvein does not read (a
// skipped file or directory, a symlink to a directory included, or a
// scanned file that is no Python source) that FileFinder would try for
// name before what Graphvein found there (row PS-I1 refusal): an extension
// or bytecode `__init__`, an unread directory, an extension module before a
// source module, or any candidate where Graphvein found nothing concrete.
func (r *ModuleResolver) shadowed(dir, name string, f entryKind) bool {
	if f != foundPackage {
		if r.unreadDirs[path.Join(dir, name)] {
			return true
		}
		for _, entry := range r.unread[path.Join(dir, name)] {
			if loaderKind(entry, "__init__") != 0 {
				return true
			}
		}
	} else {
		for _, entry := range r.unread[path.Join(dir, name)] {
			if loaderKind(entry, "__init__") == 1 {
				return true
			}
		}
		return false
	}
	for _, entry := range r.unread[dir] {
		switch kind := loaderKind(entry, name); {
		case kind == 0:
		case f == foundModule && kind == 1:
			return true
		case f == foundNone || f == foundNamespace:
			return true
		}
	}
	return false
}

// found is the outcome of find.
type found struct {
	target target
	// shadow: an unread candidate comes first (shadowed).
	shadow bool
	// top: the first component is a concrete module or package;
	// topNamespace: it is only namespace portions.
	top, topNamespace bool
	// member: the first component came from the installed workspace
	// packages (row PS-M2); ambiguous: several members install it (their
	// __init__.py files, sorted).
	member    bool
	ambiguous []string
}

// find is importlib's PathFinder over the entries for a dotted name, one
// component at a time in the parent's __path__: per component, the first
// entry holding a regular package or a module wins and namespace portions
// accumulate; a module parent ends the search.
func (r *ModuleResolver) find(entries []string, parts []string) found {
	return r.findWithSite(entries, nil, parts)
}

// findWithSite is find with one more path entry after entries: the
// installed workspace packages of row PS-M2 (name to package
// directories), which hold only those packages, as a site-packages
// directory holds only what was installed. A package there wins over
// namespace portions found earlier, as in PathFinder; one installed by
// several members is ambiguous (the order of their .pth entries decides).
func (r *ModuleResolver) findWithSite(entries []string, site map[string][]string, parts []string) found {
	var out found
	locations := entries
	for i, comp := range parts {
		var hit target
		var portions []string
		for _, entry := range locations {
			kind := r.classify(entry, comp)
			if r.shadowed(entry, comp, kind) {
				return found{shadow: true, top: out.top}
			}
			p := path.Join(entry, comp)
			if kind == foundPackage {
				hit = target{file: path.Join(p, "__init__.py"), dirs: []string{p}}
				break
			}
			if kind == foundModule {
				hit = target{file: p + ".py"}
				break
			}
			if kind == foundNamespace {
				portions = append(portions, p)
			}
		}
		// A member package named like a standard-library, built-in or
		// frozen module is never imported from site-packages: the
		// standard library comes first on sys.path (row PS-M2).
		if i == 0 && !hit.ok() && len(site[comp]) > 0 && !stdlibSet[comp] {
			pkgs := site[comp]
			if len(pkgs) > 1 {
				var inits []string
				for _, pkg := range pkgs {
					inits = append(inits, path.Join(pkg, "__init__.py"))
				}
				sort.Strings(inits)
				return found{ambiguous: inits}
			}
			if r.shadowed(path.Dir(pkgs[0]), comp, foundPackage) {
				return found{shadow: true}
			}
			hit = target{file: path.Join(pkgs[0], "__init__.py"), dirs: []string{pkgs[0]}}
			out.member = true
		}
		if i == 0 {
			out.top = hit.file != ""
			out.topNamespace = hit.file == "" && len(portions) > 0
		}
		if !hit.ok() {
			if len(portions) == 0 {
				return out
			}
			hit = target{dirs: portions}
		}
		if i == len(parts)-1 {
			out.target = hit
			return out
		}
		if len(hit.dirs) == 0 {
			return out
		}
		locations = hit.dirs
	}
	return out
}

// resolution is the outcome of one import's module under the structural
// rows.
type resolution struct {
	target    target
	ambiguous []string
	refusal   string // a default-branch form
	external  bool   // not found: an external node (PS-I4, PS-I5)
	missing   bool   // relative and not found (PS-I7)
	hides     bool   // a namespace on the ancestor path while a sibling root holds a concrete candidate
}

// relativeBase returns the directory a relative import of level dots
// searches, and whether the import goes beyond the top-level package
// (row PS-I3) or above the repository (PS-I7).
func (r *ModuleResolver) relativeBase(file string, level int) (base string, beyondTop, above bool) {
	dir := path.Dir(file)
	depth := 0
	if dir != "." {
		depth = len(strings.Split(dir, "/"))
	}
	if level > depth {
		if _, rootPackage := r.files["__init__.py"]; !rootPackage {
			return "", true, false
		}
	}
	for i := 1; i < level; i++ {
		if dir == "." {
			return "", false, true
		}
		dir = path.Dir(dir)
	}
	return dir, false, false
}

// scriptDirectory reports a directory that is neither a regular package
// (no `__init__.py`, read or not) nor a root of PS-M1: a file there runs as
// a script (`python dir/run.py`) or as a pytest module in the default
// `prepend` import mode, and both put the directory first on sys.path.
func (r *ModuleResolver) scriptDirectory(dir string) bool {
	if r.rootSet[dir] || r.implicitSet[dir] {
		return false
	}
	return !r.regularPackage(dir)
}

// implicitScriptDirectory reports a directory that is an implicit root of
// PS-M1 but neither an explicit root nor a regular package: a file there
// also runs as a script with the directory first on sys.path, while the
// policy searches the explicit ancestor roots before the implicit ones.
func (r *ModuleResolver) implicitScriptDirectory(dir string) bool {
	return r.implicitSet[dir] && !r.rootSet[dir] && !r.regularPackage(dir)
}

// regularPackage reports that dir holds an `__init__.py`, read or not.
func (r *ModuleResolver) regularPackage(dir string) bool {
	if _, ok := r.files[path.Join(dir, "__init__.py")]; ok {
		return true
	}
	for _, entry := range r.unread[dir] {
		if entry == "__init__.py" {
			return true
		}
	}
	return false
}

// sameTarget reports that two targets name the same module or the same
// namespace portions in the same order.
func sameTarget(a, b target) bool {
	if a.file != b.file || len(a.dirs) != len(b.dirs) {
		return false
	}
	for i := range a.dirs {
		if a.dirs[i] != b.dirs[i] {
			return false
		}
	}
	return true
}

// holds reports that FileFinder finds something for name in dir: a
// package, a module, a namespace directory or an unread candidate.
func (r *ModuleResolver) holds(dir, name string) bool {
	kind := r.classify(dir, name)
	return kind != foundNone || r.shadowed(dir, name, kind)
}

// structural resolves an import's module for the structural rows (and,
// through Resolve, for every other Python consumer). Before the search path
// (PS-M1): a module the interpreter provides (__main__, site hooks) is
// refused; a meta-path name (built in, frozen, loaded at start-up) is
// python-stdlib; a name the importer's script directory holds is refused
// (Python would search that directory first). When the importer's
// directory is an implicit root, the policy's answer is refused only when
// a search of that directory alone answers differently. Search path: ancestor
// explicit roots deepest first as one path, then each other explicit root
// alone (several hits are ambiguous), then the implicit ancestor roots as
// one path. After it: a top-level name found only as namespace portions
// that the standard library holds concretely, and a build-dependent
// standard-library name found in the repository, are refused.
func (r *ModuleResolver) structural(file, name string) resolution {
	if strings.HasPrefix(name, ".") {
		level := len(name) - len(strings.TrimLeft(name, "."))
		base, beyond, above := r.relativeBase(file, level)
		if beyond {
			return resolution{refusal: unreadRelativeBeyondTop}
		}
		if above {
			return resolution{missing: true}
		}
		rest := name[level:]
		if rest == "" {
			t := target{dirs: []string{base}}
			if _, ok := r.files[path.Join(base, "__init__.py")]; ok {
				t.file = path.Join(base, "__init__.py")
			}
			return resolution{target: t}
		}
		f := r.find([]string{base}, strings.Split(rest, "."))
		if f.shadow {
			return resolution{refusal: unreadShadowed}
		}
		if !f.target.ok() {
			return resolution{missing: true}
		}
		return resolution{target: f.target}
	}
	parts := strings.Split(name, ".")
	top := parts[0]
	if interpreterSet[top] {
		return resolution{refusal: unreadInterpreterModule}
	}
	if metaPathSet[top] {
		return resolution{external: true}
	}
	dir := path.Dir(file)
	if r.scriptDirectory(dir) && r.holds(dir, top) {
		return resolution{refusal: unreadScriptDirectory}
	}
	res, f := r.policy(file, parts)
	if res.target.ok() && r.implicitScriptDirectory(dir) && r.holds(dir, top) {
		// An implicit root is searched after the explicit ancestors, but
		// `python dir/run.py` searches the directory first: refuse only
		// when that order would give another answer.
		if own := r.find([]string{dir}, parts); own.shadow || !sameTarget(own.target, res.target) {
			return resolution{refusal: unreadScriptDirectory}
		}
	}
	inRepository := res.target.ok() || res.refusal == unreadUnresolvedInRepo || res.hides || len(res.ambiguous) > 0
	if stdlibSet[top] && f.topNamespace && res.target.ok() {
		// The standard library holds the name concretely on a later
		// sys.path entry, and PathFinder takes it over namespace portions.
		return resolution{refusal: unreadNamespaceHides}
	}
	if platformSet[top] && inRepository {
		return resolution{refusal: unreadPlatformModule}
	}
	return res
}

// policy is the search over the PS-M1 roots; it also returns the find
// whose answer it took.
func (r *ModuleResolver) policy(file string, parts []string) (resolution, found) {
	var ancestors, others, implicit []string
	for _, root := range r.roots {
		if ancestor(root, file) {
			ancestors = append(ancestors, root)
		} else {
			others = append(others, root)
		}
	}
	deepestFirst(ancestors)
	for _, root := range r.implicit {
		if ancestor(root, file) {
			implicit = append(implicit, root)
		}
	}
	f := r.findWithSite(ancestors, r.siteFor(file), parts)
	if f.shadow {
		return resolution{refusal: unreadShadowed}, f
	}
	if len(f.ambiguous) > 0 {
		structureTag("PS-M2")
		return resolution{ambiguous: f.ambiguous}, f
	}
	if f.member {
		structureTag("PS-M2")
	}
	if f.target.ok() {
		res := resolution{target: f.target}
		if f.target.file == "" {
			for _, root := range others {
				if c := r.find([]string{root}, parts); c.target.file != "" {
					res.hides = true
				}
			}
		}
		return res, f
	}
	if f.top {
		// A concrete top-level module or package on the ancestor path
		// binds the name; no later entry can supply the rest.
		return resolution{refusal: unreadUnresolvedInRepo}, f
	}
	inRepository := false
	var hits []found
	for _, root := range others {
		c := r.find([]string{root}, parts)
		if c.shadow {
			return resolution{refusal: unreadShadowed}, c
		}
		inRepository = inRepository || c.top
		if c.target.ok() {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return resolution{target: hits[0].target}, hits[0]
	}
	if len(hits) > 1 {
		var candidates []string
		for _, c := range hits {
			p := c.target.file
			if p == "" {
				p = c.target.dirs[0]
			}
			candidates = append(candidates, p)
		}
		sort.Strings(candidates)
		return resolution{ambiguous: candidates}, found{}
	}
	g := r.find(implicit, parts)
	if g.shadow {
		return resolution{refusal: unreadShadowed}, g
	}
	if g.target.ok() {
		return resolution{target: g.target}, g
	}
	if g.top || inRepository {
		return resolution{refusal: unreadUnresolvedInRepo}, g
	}
	return resolution{external: true}, g
}

func deepestFirst(roots []string) {
	sort.Slice(roots, func(i, j int) bool {
		if len(roots[i]) != len(roots[j]) {
			return len(roots[i]) > len(roots[j])
		}
		return roots[i] < roots[j]
	})
}

// fromList decides what `from M import name` (and the attribute M.name of
// a package, for calls, routes and models) binds, as CPython's
// _handle_fromlist does: a submodule M.name when M is a package whose
// module has no binding of name, no star import and no module
// __getattr__ (PEP 562); else M's own binding (own): when M also has the
// submodule, only a single unconditional from-import out of that submodule
// itself decides it. form names a
// default-branch form when the answer is undecided (undecided_name), an
// unread candidate comes first (shadowed_module), or the binding is
// `from . import name` itself (submodule_after_self_binding, with own and
// sub both set).
func (r *ModuleResolver) fromList(m target, name string) (sub target, own bool, form string) {
	if len(m.dirs) > 0 {
		f := r.find(m.dirs, []string{name})
		if f.shadow {
			return target{}, false, unreadShadowed
		}
		sub = f.target
	}
	if m.file == "" {
		return sub, false, ""
	}
	mf := r.files[m.file]
	p := mf.Python
	if p == nil || p.Rejected || len(p.Scopes) == 0 {
		if sub.ok() {
			return target{}, false, unreadUndecidedName
		}
		return target{}, false, ""
	}
	b, bound := p.Scopes[0].Bindings[name]
	if !bound {
		if _, lazy := p.Scopes[0].Bindings["__getattr__"]; lazy {
			return target{}, false, unreadUndecidedName
		}
	}
	if len(m.dirs) > 0 && sub.ok() {
		if !bound {
			for _, imp := range mf.Imports {
				if imp.From && imp.Scope == "" && imp.Bindings["*"] == "*" {
					return target{}, false, unreadUndecidedName
				}
			}
			return sub, false, ""
		}
		if b.Import != nil && b.Import.From && b.Import.Module == "." && b.Imported == name && b.Count == 1 {
			return sub, true, unreadSelfBoundSubmodule
		}
		if b.Import != nil && b.Import.From && b.Count == 1 && b.Stable && !b.Guarded {
			// `from .name import x as name` (or the absolute spelling)
			// loads the submodule, setting the attribute, before the
			// binding; a later import of a loaded module sets no
			// attribute again, so the binding is final.
			if res := r.structural(m.file, b.Import.Module); res.refusal == "" && res.target.file == sub.file {
				return target{}, true, ""
			}
		}
		return target{}, false, unreadUndecidedName
	}
	return target{}, bound, ""
}

// fromName decides the module `from M import name` binds (From-names in
// the document): fromList's submodule, else the module a single,
// unconditional import binding of name in M re-exports. It returns the
// module's file, or a default-branch form when the name is undecided.
func (r *ModuleResolver) fromName(m target, name string, depth int) (file, form string) {
	if depth > 8 {
		return "", unreadUndecidedName
	}
	sub, own, form := r.fromList(m, name)
	if form != "" {
		return "", form
	}
	if !own {
		return sub.file, ""
	}
	b := r.files[m.file].Python.Scopes[0].Bindings[name]
	if b.Count > 1 || !b.Stable || b.Guarded {
		return "", unreadUndecidedName
	}
	if b.Import == nil {
		return "", ""
	}
	imp := *b.Import
	if !imp.From {
		module := imp.Module
		if !imp.ExplicitAlias {
			module = strings.Split(module, ".")[0]
		}
		res := r.structural(m.file, module)
		if res.refusal != "" || len(res.ambiguous) > 0 {
			return "", unreadUndecidedName
		}
		return res.target.file, ""
	}
	res := r.structural(m.file, imp.Module)
	if res.refusal != "" || len(res.ambiguous) > 0 {
		return "", unreadUndecidedName
	}
	if !res.target.ok() {
		return "", ""
	}
	return r.fromName(res.target, b.Imported, depth+1)
}

// count records n claim units of a default-branch form: the branch
// reports D, the form's row (unreadRows) and the form itself, and the
// form is counted for the file.
func count(unread map[string]int, form string, n int) {
	structureTag("D")
	structureTag(unreadRows[form])
	structureTag("D/" + form)
	unread[form] += n
}

// linkImports emits the import edges of one file under rows PS-I1..PS-I7,
// counting refused forms in unread.
func linkImports(file string, f facts.Facts, r *ModuleResolver, nodes map[string]contract.Node, unread map[string]int) ([]contract.Edge, []contract.Diagnostic) {
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	source := facts.ID("file", file, file)
	for _, imp := range f.Imports {
		res := r.structural(file, imp.Module)
		if res.refusal != "" {
			if res.refusal == unreadUnresolvedInRepo {
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "import " + imp.Module + " is not found, though its top-level name resolves in the repository"})
			}
			count(unread, res.refusal, 1)
			continue
		}
		if len(res.ambiguous) > 0 {
			structureTag("PS-I7")
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "ambiguous_import", Message: "import " + imp.Module + " has candidates: " + strings.Join(res.ambiguous, ", ")})
			continue
		}
		if res.missing {
			structureTag("PS-I7")
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "relative import " + imp.Module + " is unavailable"})
			continue
		}
		if res.external {
			top := strings.Split(imp.Module, ".")[0]
			ecosystem := externalEcosystem(top)
			if ecosystem == ecosystemStdlib {
				structureTag("PS-I5")
			} else {
				structureTag("PS-I4")
			}
			edges = append(edges, facts.Edge("imports", source, facts.External(nodes, ecosystem, top), file, imp.Span))
			continue
		}
		if !strings.HasPrefix(imp.Module, ".") && strings.Contains(imp.Module, ".") {
			count(unread, unreadDottedParents, 1)
		}
		if res.hides {
			count(unread, unreadNamespaceHides, 1)
		}
		targets := map[string]bool{}
		if res.target.file != "" {
			if res.target.file == file {
				count(unread, unreadSelfImport, 1)
			} else {
				targets[res.target.file] = true
			}
		}
		if imp.From {
			if res.target.file == "" {
				structureTag("PS-I6")
			} else {
				structureTag("PS-I2")
			}
			for _, name := range imp.Bindings {
				if name == "*" {
					continue
				}
				t, form := r.fromName(res.target, name, 0)
				if form != "" {
					count(unread, form, 1)
					continue
				}
				if t == file {
					count(unread, unreadSelfImport, 1)
					continue
				}
				if t != "" {
					targets[t] = true
				}
			}
		} else if res.target.file == "" {
			structureTag("PS-I6")
		} else {
			structureTag("PS-I1")
		}
		for t := range targets {
			edges = append(edges, facts.Edge("imports", source, facts.ID("file", t, t), file, imp.Span))
		}
	}
	return edges, diagnostics
}
