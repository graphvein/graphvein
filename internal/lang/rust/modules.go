package rust

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

type rustCrate struct {
	tree       *crate
	name, root string
	// manifest is the Cargo.toml that declared a library crate; it gives
	// crate-level diagnostics a real repository file to attach to.
	manifest string
	// pkg is the package the crate belongs to: the declaring manifest for a
	// root it reads, the nearest read package above the file for a file no
	// root reaches (orphan), nil when there is none.
	pkg *cargoPackage
	// orphan marks a crate rooted at a file no manifest root reaches.
	orphan bool
	// library marks the package's library crate, build its build script.
	library, build bool
	// modItems records, per outlined `mod` item walked in this crate (keyed
	// by modItemKey), the file it loads: "" when no candidate exists,
	// ambiguousModule when several do (rows RS-M2, RS-M3).
	modItems map[string][]string
}

// ambiguousModule marks a `mod` item whose module file is ambiguous
// (rustc E0761) in crate.modItems.
const ambiguousModule = "\x00ambiguous"

// modItemKey keys a `mod` item by its file and span start.
func modItemKey(file string, span contract.Span) string {
	return fmt.Sprintf("%s:%d:%d", file, span.StartLine, span.StartColumn)
}

// cargoPackage is what import edges (rows RS-I2..RS-I4) read from one
// scanned Cargo.toml with a [package] table.
type cargoPackage struct {
	manifest, dir string
	// edition is "2015", "2018", "2021" or "2024"; "" when it cannot be read
	// (an unread value, or a workspace-inherited edition with no workspace
	// root edition).
	edition string
	// lib is the package's library crate, libName its extern name.
	lib     *rustCrate
	libName string
	// deps maps each declared dependency's extern name to its source, over
	// the package's dependency tables other than build-dependencies;
	// buildDeps does the same for its build script.
	deps, buildDeps map[string]crateDep
	raw             manifest
}

// crateDep is a declared dependency: Source "path" (Dir is the dependency's
// package directory), "registry", "git", "unknown" (a value Graphvein does
// not read) or "conflict" (one extern name declared with different sources).
// Crate names the external crate an import edge points to (row RS-I3): the
// package name (the `package` key of a renamed dependency, else the key),
// `-` read as `_`.
type crateDep struct {
	Source, Dir, Crate string
}

// crateRoot is one crate root Graphvein reads from a manifest (row RS-M1).
type crateRoot struct {
	// Manifest is the declaring Cargo.toml, File the root file, Kind the
	// cargo target kind ("lib", "bin", "test", "example", "bench",
	// "custom-build").
	Manifest, File, Kind string
}

// Binding retains the terminal scanned declaration or external crate path.
// It is rebuilt each pass and is shared by imports, calls and Axum consumers.
type Binding struct {
	File, Identity, Name, External string
	Scope                          int
	Constant, IsModule, Ambiguous  bool
	Value                          facts.RustExpr
	crate                          *rustCrate
	module                         string
	cycle                          bool
	declared                       bool
	// refused names the default-branch form (docs/lang/rust-structure.md)
	// of an import leaf that claims nothing; set only by import resolution.
	refused string
	// alternatives are the qualified names of the cfg alternatives of a
	// module name (several `mod` items with one name in one scope); set
	// only by import resolution, which resolves the rest of the path in
	// each.
	alternatives []string
}

// ModuleResolver resolves only current scanned inventory; no Cargo execution is used.
type ModuleResolver struct {
	files       map[string]facts.Facts
	crates      []*rustCrate
	libraries   map[string][]*rustCrate
	members     map[string][]*rustCrate
	memo        map[string]Binding
	seen        map[string]bool
	diagnostics []contract.Diagnostic
	// strict selects import-edge resolution (rows RS-I1..RS-I5): globs of
	// external crates bind no name, first segments reach only declared
	// dependencies and sysroot crates, and refusals are reported. Calls and
	// the framework consumers resolve with strict unset.
	strict     bool
	strictMemo map[string]Binding
	packages   map[string]*cargoPackage
	// aliases holds, per crate root file, the names `extern crate x as y`
	// items of the crate's files bind.
	aliases map[string]map[string]bool
	// roots are the crate roots read from manifests, in manifest order.
	roots []crateRoot
}

func (r *ModuleResolver) diagnostic(file, code, message string) {
	r.diagnostics = append(r.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: code, Severity: "warning", Message: message})
}

// NewModuleResolver rereads scanned Cargo metadata so metadata-only edits relink.
func NewModuleResolver(files map[string]facts.Facts, sources map[string][]byte) *ModuleResolver {
	r := &ModuleResolver{files: files, libraries: map[string][]*rustCrate{}, members: map[string][]*rustCrate{}, memo: map[string]Binding{}, seen: map[string]bool{}, strictMemo: map[string]Binding{}, packages: map[string]*cargoPackage{}, aliases: map[string]map[string]bool{}}
	var manifests []string
	for file := range sources {
		if path.Base(file) == "Cargo.toml" {
			manifests = append(manifests, file)
		}
	}
	sort.Strings(manifests)
	read := map[string]manifest{}
	unread := map[string]bool{}
	for _, file := range manifests {
		m, err := readManifest(string(sources[file]))
		if err != nil {
			// Row RS-M1, open point O-S4-5 (a): a manifest Graphvein cannot
			// read gives no crate root and no package.
			structureTag("RS-M1")
			r.diagnostic(file, "config_parse_error", err.Error())
			unread[path.Dir(file)] = true
			continue
		}
		read[path.Dir(file)] = m
	}
	// dropped holds packages whose targets cargo cannot locate: like an
	// unread manifest, they give no crate root and no package.
	dropped := map[string]bool{}
	for _, file := range manifests {
		dir := path.Dir(file)
		m, ok := read[dir]
		if !ok || !m.packageTable {
			continue
		}
		structureTag("RS-M1")
		root, _, hasRoot := workspaceRoot(dir, read, unread)
		pkg := &cargoPackage{manifest: file, dir: dir, raw: m, edition: packageEdition(m, root, hasRoot)}
		roots, err := packageRoots(dir, m, pkg.edition, files)
		if err != nil {
			r.diagnostic(file, "config_parse_error", err.Error())
			dropped[dir] = true
			continue
		}
		r.packages[dir] = pkg
		name := m.libName
		if name == "" {
			name = strings.ReplaceAll(m.name, "-", "_")
		}
		pkg.libName = name
		for _, root := range roots {
			root.Manifest = file
			r.roots = append(r.roots, root)
			if root.Kind == "lib" {
				c := r.build(root.File, name, true)
				c.manifest = file
				c.pkg, c.library = pkg, true
				pkg.lib = c
				if name != "" {
					r.libraries[name] = append(r.libraries[name], c)
				}
				continue
			}
			c := r.build(root.File, "", true)
			c.pkg, c.build = pkg, root.Kind == "custom-build"
		}
	}
	for _, pkg := range r.packages {
		pkg.deps = packageDeps(pkg, false, read, unread, r.packages)
		pkg.buildDeps = packageDeps(pkg, true, read, unread, r.packages)
	}
	var names []string
	for name := range r.libraries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(r.libraries[name]) > 1 {
			// One diagnostic per declaring manifest, in manifest order (the
			// crates were appended while walking sorted manifests), so each
			// crate's Cargo.toml carries the warning on a valid file path.
			var roots []string
			for _, c := range r.libraries[name] {
				roots = append(roots, c.root)
			}
			for _, c := range r.libraries[name] {
				r.diagnostic(c.manifest, "duplicate_crate_name", name+": "+strings.Join(roots, ", "))
			}
		}
	}
	for d := range dropped {
		unread[d] = true
	}
	for _, file := range facts.Paths(files) {
		if len(r.members[file]) == 0 {
			c := r.build(file, "", false)
			c.orphan = true
			c.pkg = enclosingPackage(path.Dir(file), r.packages, unread)
		}
	}
	for _, c := range r.crates {
		seen := map[string]bool{}
		for _, m := range c.tree.modules {
			if seen[m.file] {
				continue
			}
			seen[m.file] = true
			if f := r.files[m.file].Rust; f != nil {
				for _, alias := range f.ExternAliases {
					if r.aliases[c.root] == nil {
						r.aliases[c.root] = map[string]bool{}
					}
					r.aliases[c.root][alias] = true
				}
			}
		}
	}
	return r
}

// packageRoots returns a package's crate roots as cargo 1.97 discovers its
// targets (row RS-M1): the library ([lib] path or src/lib.rs, unless
// autolib = false and no [lib] table), the explicit [[bin]], [[test]],
// [[example]] and [[bench]] targets (a path as written, or a name found
// among the discoverable files of that kind), the auto-discovered targets
// of each kind (src/main.rs and src/bin for binaries, tests/, examples/,
// benches/: `<name>.rs` and `<name>/main.rs`) unless the kind's auto key is
// false, or, unset, the package is edition 2015 (or of an unknown edition)
// and declares targets of that kind; an auto-discovered target whose name
// or path an explicit one takes is dropped, as cargo drops it; and the
// build script (`build` as written, build.rs unless `build = false`). Only
// files in the scanned inventory are roots. A target cargo cannot locate (a
// name with no file, a name two files take) is an error: cargo refuses the
// manifest, and so does Graphvein.
func packageRoots(dir string, m manifest, edition string, files map[string]facts.Facts) ([]crateRoot, error) {
	exists := func(p string) bool { _, ok := files[p]; return ok }
	var out []crateRoot
	autolib, set := m.auto["autolib"]
	if m.lib || !set || autolib {
		lib := m.libPath
		if lib == "" {
			lib = "src/lib.rs"
		}
		if p := path.Join(dir, lib); exists(p) {
			out = append(out, crateRoot{File: p, Kind: "lib"})
		}
	}
	// discoverable lists, per target name, the files cargo would discover
	// for a kind.
	discoverable := func(kind, sub string) map[string][]string {
		found := map[string][]string{}
		if kind == "bin" && exists(path.Join(dir, "src/main.rs")) {
			found[m.name] = append(found[m.name], path.Join(dir, "src/main.rs"))
		}
		base := path.Join(dir, sub) + "/"
		for _, p := range facts.Paths(files) {
			rel := strings.TrimPrefix(p, base)
			if rel == p || !strings.HasSuffix(rel, ".rs") {
				continue
			}
			switch parts := strings.Split(rel, "/"); {
			case len(parts) == 1:
				name := strings.TrimSuffix(rel, ".rs")
				found[name] = append(found[name], p)
			case len(parts) == 2 && parts[1] == "main.rs":
				found[parts[0]] = append(found[parts[0]], p)
			}
		}
		return found
	}
	for _, k := range targetKinds {
		found := discoverable(k.kind, k.dir)
		explicitNames, explicitPaths := map[string]bool{}, map[string]bool{}
		var roots []string
		for _, t := range m.targets[k.kind] {
			p := ""
			switch {
			case t.path != "":
				p = path.Join(dir, t.path)
			case t.name != "" && len(found[t.name]) == 1:
				p = found[t.name][0]
			default:
				return nil, fmt.Errorf("%s: cannot locate %s target %q", path.Join(dir, "Cargo.toml"), k.kind, t.name)
			}
			explicitNames[t.name] = true
			explicitPaths[p] = true
			roots = append(roots, p)
		}
		auto, set := m.auto[k.auto]
		if !set {
			auto = edition != "2015" && edition != "" || len(m.targets[k.kind]) == 0
		}
		if auto {
			var names []string
			for name := range found {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if len(found[name]) > 1 {
					return nil, fmt.Errorf("%s: two %s targets named %q", path.Join(dir, "Cargo.toml"), k.kind, name)
				}
				if p := found[name][0]; !explicitNames[name] && !explicitPaths[p] {
					roots = append(roots, p)
				}
			}
		}
		sort.Strings(roots)
		for _, p := range roots {
			if exists(p) {
				out = append(out, crateRoot{File: p, Kind: k.kind})
			}
		}
	}
	build := "build.rs"
	switch m.build {
	case "false":
		build = ""
	case "", "true":
	default:
		build = m.build
	}
	if p := path.Join(dir, build); build != "" && exists(p) {
		out = append(out, crateRoot{File: p, Kind: "custom-build"})
	}
	return out, nil
}

// Roots returns the crate roots the resolver read from manifests (row
// RS-M1), in manifest order; the structure oracle compares them with the
// targets `cargo metadata` reports.
func (r *ModuleResolver) Roots() []crateRoot {
	return append([]crateRoot(nil), r.roots...)
}

// workspaceRoot returns the manifest of the workspace a package directory
// belongs to: the nearest read manifest at or above it with a [workspace]
// table. ok is false when there is none, or when an unread manifest lies
// on the way.
func workspaceRoot(dir string, read map[string]manifest, unread map[string]bool) (root manifest, rootDir string, ok bool) {
	for d := dir; ; d = path.Dir(d) {
		if unread[d] {
			return manifest{}, "", false
		}
		if m, ok := read[d]; ok && m.workspace {
			return m, d, true
		}
		if d == "." || d == "/" || d == "" {
			return manifest{}, "", false
		}
	}
}

// packageEdition reads a package's edition (row RS-I4): the [package]
// value, the workspace root's [workspace.package] value for
// `edition.workspace = true`, or 2015 when the key is absent, as cargo
// does. "" means it cannot be read.
func packageEdition(m manifest, root manifest, hasRoot bool) string {
	edition := m.edition
	switch {
	case m.editionUnread:
		return ""
	case m.editionWorkspace:
		if !hasRoot {
			return ""
		}
		edition = root.workspaceEdition
	case edition == "":
		return "2015"
	}
	switch edition {
	case "2015", "2018", "2021", "2024":
		return edition
	}
	return ""
}

// packageDeps maps every dependency of a package, over its build-dependencies
// tables (build) or all its other dependency tables, to its extern name and
// source (rows RS-I2, RS-I3). The extern name is the dependency key for a
// renamed dependency (`package = ...`), the library name of the package a
// path dependency names, else the key; `-` reads as `_`.
func packageDeps(pkg *cargoPackage, build bool, read map[string]manifest, unread map[string]bool, packages map[string]*cargoPackage) map[string]crateDep {
	root, rootDir, hasRoot := workspaceRoot(pkg.dir, read, unread)
	out := map[string]crateDep{}
	keys := pkg.raw.dependencies(build)
	var names []string
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, key := range names {
		for _, d := range keys[key] {
			base := pkg.dir
			if d.Source == "workspace" {
				w, ok := root.workspaceDeps[key]
				if !hasRoot || !ok || w.Source == "workspace" {
					d = dependency{Source: "unknown"}
				} else {
					d = w
					base = rootDir
				}
			}
			crateName := key
			if d.Package != "" {
				crateName = d.Package
			}
			dep := crateDep{Source: d.Source, Crate: strings.ReplaceAll(crateName, "-", "_")}
			extern := key
			if d.Source == "path" {
				dep.Dir = path.Join(base, d.Path)
				if target := packages[dep.Dir]; target != nil && d.Package == "" && target.libName != "" {
					extern = target.libName
				}
			}
			extern = strings.ReplaceAll(extern, "-", "_")
			if d.Source == "unknown" {
				// A value Graphvein does not read may hold a `package`
				// key: the crate is named by its extern name only.
				dep.Crate = extern
			}
			if prior, ok := out[extern]; ok && prior != dep {
				dep = crateDep{Source: "conflict", Crate: extern}
			}
			out[extern] = dep
		}
	}
	return out
}

// enclosingPackage returns the nearest read package at or above dir, or nil
// when there is none or an unread manifest comes first.
func enclosingPackage(dir string, packages map[string]*cargoPackage, unread map[string]bool) *cargoPackage {
	for d := dir; ; d = path.Dir(d) {
		if unread[d] {
			return nil
		}
		if p := packages[d]; p != nil {
			return p
		}
		if d == "." || d == "/" || d == "" {
			return nil
		}
	}
}

// build walks a crate's module tree from its root. Every `mod` item is
// resolved from its own `#[path]` attribute and the directory of the module
// instance holding it (rows RS-M2, RS-M3), and its result recorded in
// modItems. Several `mod` items with one name in one scope (cfg
// alternatives) are all kept: the first under the qualified name, the
// others under `<name>#2`, `#3`, ..., counted in tree.alts.
func (r *ModuleResolver) build(root, name string, outlined bool) *rustCrate {
	c := &rustCrate{tree: &crate{modules: map[string]module{}, alts: map[string][]string{}}, name: name, root: root, modItems: map[string][]string{}}
	r.crates = append(r.crates, c)
	var walk func(string, module, map[string]bool)
	walk = func(q string, m module, seen map[string]bool) {
		key := m.file + "#" + m.scope + "#" + modItemKey(m.file, m.span)
		if seen[key] {
			return
		}
		seen[key] = true
		defer delete(seen, key)
		if _, exists := c.tree.modules[q]; exists {
			n := len(c.tree.alts[q]) + 1
			if n == 1 {
				n = 2
				c.tree.alts[q] = []string{q}
			}
			alt := fmt.Sprintf("%s#%d", q, n)
			c.tree.alts[q] = append(c.tree.alts[q], alt)
			q = alt
		}
		c.tree.modules[q] = m
		found := false
		for _, prior := range r.members[m.file] {
			found = found || prior == c
		}
		if !found {
			r.members[m.file] = append(r.members[m.file], c)
		}
		for _, d := range r.files[m.file].Modules {
			if d.Scope != m.scope || m.inline && !within(d.Span, m.span) {
				continue
			}
			child := facts.Join(q, d.Name)
			if d.Inline {
				dir := path.Join(m.dir, fileName(d.Name))
				if d.Path != "" {
					base := m.dir
					if m.scope == "" {
						base = path.Dir(m.file)
					}
					dir = path.Join(base, d.Path)
				}
				walk(child, module{file: m.file, scope: facts.Join(m.scope, d.Name), dir: dir, inline: true, span: d.Span}, seen)
				continue
			}
			if !outlined {
				continue
			}
			item := modItemKey(m.file, d.Span)
			candidates := []string{path.Join(m.dir, fileName(d.Name)+".rs"), path.Join(m.dir, fileName(d.Name), "mod.rs")}
			if d.Path != "" {
				base := m.dir
				if m.scope == "" {
					base = path.Dir(m.file)
				}
				candidates = []string{path.Join(base, d.Path)}
			}
			var available []string
			for _, p := range candidates {
				if _, ok := r.files[p]; ok {
					available = append(available, p)
				}
			}
			if len(available) > 1 {
				r.diagnostic(m.file, "ambiguous_module", d.Name+": "+strings.Join(available, ", "))
				c.modItems[item] = append(c.modItems[item], ambiguousModule)
				continue
			}
			if len(available) == 0 {
				c.modItems[item] = append(c.modItems[item], "")
				continue
			}
			p := available[0]
			c.modItems[item] = append(c.modItems[item], p)
			// rustc treats a file loaded through #[path] as a mod-rs file
			// (row RS-M3): its own `mod` items load from its directory.
			dir := strings.TrimSuffix(p, ".rs")
			if path.Base(p) == "mod.rs" || d.Path != "" {
				dir = path.Dir(p)
			}
			walk(child, module{file: p, dir: dir}, seen)
		}
	}
	walk("", module{file: root, dir: path.Dir(root)}, map[string]bool{})
	return c
}

// fileName is a module name as rustc spells its file and directory: a raw
// identifier (`mod r#impl;`) without its `r#` prefix. The module tree keeps
// the name as written, so `use r#impl::X` follows it; a path that spells the
// other form (`foo` for `mod r#foo;`) finds no module, and the check of
// declared module names (scopeName) compares file names, so it shadows the
// extern prelude instead of reaching a crate (an omission, never an edge).
func fileName(name string) string { return strings.TrimPrefix(name, "r#") }

// within reports whether span inner lies inside span outer.
func within(inner, outer contract.Span) bool {
	after := inner.StartLine > outer.StartLine || inner.StartLine == outer.StartLine && inner.StartColumn >= outer.StartColumn
	before := inner.EndLine < outer.EndLine || inner.EndLine == outer.EndLine && inner.EndColumn <= outer.EndColumn
	return after && before
}

func (r *ModuleResolver) moduleBinding(c *rustCrate, q string) Binding {
	m, ok := c.tree.modules[q]
	if !ok {
		return Binding{}
	}
	scope := 0
	if f := r.files[m.file].Rust; f != nil {
		for i, s := range f.Scopes {
			if s.Module == m.scope && s.Function == "" && (!m.inline || within(s.Span, m.span)) {
				scope = i
				break
			}
		}
	}
	return Binding{File: m.file, Scope: scope, IsModule: true, crate: c, module: q}
}

func bindingKey(b Binding) string {
	if b.Identity != "" {
		return b.Identity
	}
	if b.External != "" {
		return "external:" + b.External + "::" + b.Name
	}
	if b.File != "" {
		return fmt.Sprintf("%s#%s:%d:%s", b.File, b.module, b.Scope, b.Name)
	}
	return ""
}

func agree(a, b Binding) Binding {
	if a.Ambiguous || b.Ambiguous {
		return Binding{Ambiguous: true}
	}
	if bindingKey(a) == "" {
		b.cycle = b.cycle || a.cycle
		return b
	}
	if bindingKey(b) == "" {
		a.cycle = a.cycle || b.cycle
		return a
	}
	if bindingKey(a) != bindingKey(b) {
		return Binding{Ambiguous: true, cycle: a.cycle || b.cycle}
	}
	a.cycle = a.cycle || b.cycle
	return a
}

func (r *ModuleResolver) library(name string) Binding {
	list := r.libraries[name]
	if len(list) > 1 {
		return Binding{Ambiguous: true}
	}
	if len(list) == 1 {
		return r.moduleBinding(list[0], "")
	}
	return Binding{External: name}
}

// externCrate resolves a first path segment from the extern prelude. Outside
// import resolution it is library: any scanned library of that name, else an
// external crate. For import edges (r.strict) it follows rows RS-I2 and
// RS-I3: a sysroot crate; the package's own library for its other targets
// (not its build script); a declared dependency (the build script's
// build-dependencies, the other targets' other dependencies), which is the
// scanned library of a path dependency and otherwise an external crate named
// by its package (never a scanned library of the same name: precision stop
// PS-S4-6). `extern crate ... as` aliases, macro_rules! names and undeclared
// names claim nothing.
func (r *ModuleResolver) externCrate(c *rustCrate, file, name string) Binding {
	if !r.strict {
		return r.library(name)
	}
	if r.aliases[c.root][name] {
		return Binding{refused: unreadExternAlias}
	}
	for _, n := range r.files[file].Nodes {
		if n.Kind == "macro" && n.Name == name && !localNode(n) {
			return Binding{refused: unreadMacroName}
		}
	}
	switch name {
	case "std", "core", "alloc", "proc_macro", "test":
		return Binding{External: name}
	}
	pkg := c.pkg
	if pkg == nil {
		return Binding{refused: unreadUndeclaredCrate}
	}
	if !c.library && !c.orphan && !c.build && pkg.lib != nil && name == pkg.libName {
		return r.moduleBinding(pkg.lib, "")
	}
	deps := pkg.deps
	if c.build {
		deps = pkg.buildDeps
	}
	dep, ok := deps[name]
	if !ok {
		return Binding{refused: unreadUndeclaredCrate}
	}
	switch dep.Source {
	case "path":
		if target := r.packages[dep.Dir]; target != nil && target.lib != nil {
			return r.moduleBinding(target.lib, "")
		}
		return Binding{External: dep.Crate}
	case "registry", "git":
		return Binding{External: dep.Crate}
	}
	if len(r.libraries[name]) > 0 {
		return Binding{refused: unreadUndecidedCrate}
	}
	return Binding{External: dep.Crate}
}

// editionRefusal returns the default-branch form for following a `use`
// declaration of crate c in import resolution (row RS-I4): edition 2015
// paths and paths of a crate whose edition cannot be read are not modelled,
// so a leaf whose resolution passes through such an import claims nothing.
func editionRefusal(c *rustCrate) string {
	switch {
	case c.pkg == nil || c.pkg.edition == "":
		return unreadEditionUnknown
	case c.pkg.edition == "2015":
		return unreadEdition2015
	}
	return ""
}

// ResolveImport resolves one `use` leaf for an import edge (rows
// RS-I1..RS-I5). It is Resolve with r.strict set, and a leaf that any crate
// holding the file refuses (an edition 2015 or unknown crate, row RS-I4, or
// a refusal on the way) returns only the refusal form.
func (r *ModuleResolver) ResolveImport(file string, scope int, text string) Binding {
	f := r.files[file].Rust
	if f == nil || scope < 0 || scope >= len(f.Scopes) {
		return Binding{}
	}
	r.strict = true
	defer func() { r.strict = false }()
	var result Binding
	refused := ""
	for _, c := range r.members[file] {
		for _, q := range moduleNames(c.tree, file, f.Scopes[scope].Module, f.Scopes[scope].Span) {
			if form := editionRefusal(c); form != "" {
				refused = form
				continue
			}
			b := r.path(c, q, scope, file, text)
			if b.refused != "" {
				refused = b.refused
				continue
			}
			result = agree(result, b)
		}
	}
	if refused != "" {
		return Binding{refused: refused}
	}
	return result
}

func (r *ModuleResolver) lookup(c *rustCrate, q, name string) Binding {
	key := fmt.Sprintf("%s#%s::%s", c.root, q, name)
	memo := r.memo
	if r.strict {
		memo = r.strictMemo
		key = "strict:" + key
	}
	if b, ok := memo[key]; ok {
		return b
	}
	if r.seen[key] {
		return Binding{cycle: true}
	}
	r.seen[key] = true
	defer delete(r.seen, key)
	b := r.moduleBinding(c, q)
	if b.File == "" {
		return Binding{}
	}
	result, found := r.scopeName(b.File, b.Scope, c, q, name, false)
	if !found {
		result = Binding{refused: result.refused}
	}
	result.declared = found
	if !result.cycle {
		memo[key] = result
	}
	return result
}

func (r *ModuleResolver) scopeName(file string, scope int, c *rustCrate, q, name string, local bool) (Binding, bool) {
	if local {
		key := fmt.Sprintf("local:%s:%d:%s", file, scope, name)
		if r.seen[key] {
			return Binding{cycle: true}, true
		}
		r.seen[key] = true
		defer delete(r.seen, key)
	}
	f := r.files[file].Rust
	if f == nil || scope < 0 || scope >= len(f.Scopes) {
		return Binding{}, false
	}
	s := f.Scopes[scope]
	if items, ok := s.Items[name]; ok {
		if len(items) != 1 {
			return Binding{Ambiguous: true}, true
		}
		v := items[0]
		return Binding{File: file, Scope: scope, Identity: v.Identity, Name: name, Constant: v.Constant, Value: v.Value, crate: c, module: q}, true
	}
	if !local {
		child := facts.Join(q, name)
		if m := r.moduleBinding(c, child); m.File != "" {
			if alts := c.tree.alts[child]; r.strict && len(alts) > 1 {
				// Several `mod` items with this name (cfg alternatives):
				// import resolution continues in each.
				m.alternatives = alts
			}
			return m, true
		}
		// Missing/ambiguous declared modules shadow external crate names.
		for _, d := range r.files[file].Modules {
			if d.Scope == s.Module && fileName(d.Name) == fileName(name) {
				return Binding{}, true
			}
		}
	}
	follow := ""
	if r.strict {
		// Row RS-I4: following an import of an edition 2015 or
		// unknown-edition crate claims nothing.
		follow = editionRefusal(c)
	}
	var explicit Binding
	found := false
	for _, imp := range s.Imports {
		if target, ok := imp.Bindings[name]; ok && name != "_" && name != "*" {
			if follow != "" {
				return Binding{refused: follow}, true
			}
			// The import currently being defined cannot provide its own first
			// segment (notably the macro import async_trait::async_trait).
			if target == name || strings.HasPrefix(target, name+"::") {
				target = "::" + target
			}
			explicit = agree(explicit, r.path(c, q, scope, file, target))
			found = true
		}
	}
	if found {
		return explicit, true
	}
	var glob Binding
	opaque := ""
	for _, imp := range s.Imports {
		if !strings.HasSuffix(imp.Module, "::*") && !strings.HasSuffix(imp.Module, "*") {
			continue
		}
		if follow != "" {
			return Binding{refused: follow}, true
		}
		base := r.path(c, q, scope, file, strings.TrimSuffix(strings.TrimSuffix(imp.Module, "*"), "::"))
		v := Binding{}
		if base.refused != "" && r.strict {
			opaque = base.refused
			continue
		}
		if base.IsModule {
			v = r.lookup(base.crate, base.module, name)
			if v.refused != "" && bindingKey(v) == "" {
				opaque = v.refused
			}
		} else if base.External != "" && r.strict {
			// Rows RS-I1, RS-I3 (precision stop PS-S4-4): the glob of an
			// unscanned crate has no export inventory, so it binds no name;
			// a name only it could supply goes to D.
			opaque = unreadExternalGlob
			continue
		} else if base.External != "" {
			// An opaque external glob has no scanned export inventory proving
			// that it shadows a crate name. Qualified crate paths must still
			// reach the extern prelude; scanned glob declarations do shadow it.
			if name == base.External || len(r.libraries[name]) != 0 {
				continue
			}
			v = base
			v.Name = strings.TrimPrefix(base.Name+"::"+name, "::")
		}
		glob = agree(glob, v)
	}
	if bindingKey(glob) != "" || glob.Ambiguous || glob.cycle {
		return glob, true
	}
	return Binding{refused: opaque}, false
}

func (r *ModuleResolver) path(c *rustCrate, q string, scope int, file, text string) Binding {
	text = strings.TrimSpace(text)
	if text == "" {
		return Binding{}
	}
	absoluteExtern := strings.HasPrefix(text, "::")
	parts := strings.Split(strings.TrimPrefix(text, "::"), "::")
	var b Binding
	if absoluteExtern {
		b = r.externCrate(c, file, parts[0])
		parts = parts[1:]
	} else {
		switch parts[0] {
		case "crate":
			if r.strict && c.orphan {
				// A file no manifest root reaches is not known to be a crate
				// root, so `crate::` names no known module.
				return Binding{refused: unreadUnrootedCrate}
			}
			b = r.moduleBinding(c, "")
			parts = parts[1:]
		case "self":
			b = r.moduleBinding(c, q)
			parts = parts[1:]
		case "super":
			for len(parts) > 0 && parts[0] == "super" {
				if q == "" {
					return Binding{}
				}
				if i := strings.LastIndex(q, "."); i >= 0 {
					q = q[:i]
				} else {
					q = ""
				}
				parts = parts[1:]
			}
			b = r.moduleBinding(c, q)
		default:
			found := false
			f := r.files[file].Rust
			if f != nil {
				for i := scope; i >= 0 && i < len(f.Scopes); i = f.Scopes[i].Parent {
					if f.Scopes[i].Module != f.Scopes[scope].Module {
						break
					}
					if f.Scopes[i].Function == "" {
						break
					}
					b, found = r.scopeName(file, i, c, q, parts[0], true)
					if found {
						break
					}
				}
			}
			if !found {
				b = r.lookup(c, q, parts[0])
				found = bindingKey(b) != "" || b.Ambiguous || b.cycle || b.declared
			}
			if !found {
				b = r.externCrate(c, file, parts[0])
			}
			parts = parts[1:]
		}
	}
	return r.follow(b, parts)
}

// follow resolves the remaining segments of a path from binding b.
func (r *ModuleResolver) follow(b Binding, parts []string) Binding {
	for i := 0; ; i++ {
		if len(b.alternatives) > 0 {
			return r.followAlternatives(b, parts[i:])
		}
		if i == len(parts) {
			return b
		}
		name := parts[i]
		if b.Ambiguous || b.cycle || b.refused != "" {
			return b
		}
		if b.External != "" {
			b.Name = strings.TrimPrefix(b.Name+"::"+name, "::")
			continue
		}
		if b.IsModule {
			b = r.lookup(b.crate, b.module, name)
			continue
		}
		// Retain existing method identities, without instance type inference.
		if b.Identity != "" {
			var inherent, traits []Binding
			for _, p := range facts.Paths(r.files) {
				for _, node := range r.files[p].Nodes {
					if node.Kind != "method" || node.Name != name || localNode(node) {
						continue
					}
					mqs := moduleNames(b.crate.tree, p, attribute(node, "module"), contract.Span{})
					if len(mqs) == 0 {
						continue
					}
					mq := mqs[0]
					ms := r.moduleBinding(b.crate, mq).Scope
					receiver := r.path(b.crate, mq, ms, p, attribute(node, "receiver_path"))
					if receiver.Identity != b.Identity {
						continue
					}
					v := Binding{File: p, Identity: node.Identity, crate: b.crate, module: mq, Scope: ms}
					if attribute(node, "trait") == "" {
						inherent = append(inherent, v)
					} else {
						traits = append(traits, v)
					}
				}
			}
			if len(inherent) == 1 {
				b = inherent[0]
			} else if len(inherent) == 0 && len(traits) == 1 {
				b = traits[0]
			} else {
				return Binding{}
			}
		} else {
			return Binding{}
		}
	}
}

// followAlternatives resolves the rest of a path in every cfg alternative
// of a module name (import resolution only). The alternatives agree when
// each reaches the same file or the same external crate: the first one's
// result stands for all. Otherwise the leaf claims nothing: a refusal on the
// way stays, anything else goes to D as cfg_module_alternatives (row RS-I1).
func (r *ModuleResolver) followAlternatives(b Binding, rest []string) Binding {
	var first Binding
	target := ""
	for i, q := range b.alternatives {
		v := r.follow(r.moduleBinding(b.crate, q), rest)
		if v.refused != "" {
			return Binding{refused: v.refused}
		}
		if v.Ambiguous || v.cycle {
			return v
		}
		t := ""
		switch {
		case v.External != "":
			t = "external:" + v.External
		case v.File != "":
			t = "file:" + v.File
		}
		if i == 0 {
			first, target = v, t
			continue
		}
		if t != target {
			return Binding{refused: unreadCfgModule}
		}
	}
	return first
}

// Resolve follows a Rust path from its lexical syntax scope to one declaration.
func (r *ModuleResolver) Resolve(file string, scope int, text string) Binding {
	f := r.files[file].Rust
	if f == nil || scope < 0 || scope >= len(f.Scopes) {
		return Binding{}
	}
	var result Binding
	for _, c := range r.members[file] {
		for _, q := range moduleNames(c.tree, file, f.Scopes[scope].Module, f.Scopes[scope].Span) {
			result = agree(result, r.path(c, q, scope, file, text))
		}
	}
	return result
}

// ScopeAt selects the smallest lexical block containing a source call.
func (r *ModuleResolver) ScopeAt(file string, span contract.Span) int {
	best := 0
	f := r.files[file].Rust
	if f == nil {
		return best
	}
	for i, s := range f.Scopes {
		start := s.Span.StartLine < span.StartLine || s.Span.StartLine == span.StartLine && s.Span.StartColumn <= span.StartColumn
		end := s.Span.EndLine > span.EndLine || s.Span.EndLine == span.EndLine && s.Span.EndColumn >= span.EndColumn
		if start && end && s.End-s.Start <= f.Scopes[best].End-f.Scopes[best].Start {
			best = i
		}
	}
	return best
}

// Prefix proves only literal strings or const/static &str literal initializers.
func (r *ModuleResolver) Prefix(file string, e facts.RustExpr) (string, string, bool) {
	if e.Kind == "literal" {
		return e.Name, "literal", true
	}
	if e.Kind == "path" {
		b := r.Resolve(file, e.Scope, e.Name)
		if b.Constant && !b.Ambiguous {
			return b.Value.Name, "constant", true
		}
	}
	return "", "", false
}

// LinkWithResolver gives calls and routes the same current Rust name resolution.
func LinkWithResolver(files map[string]facts.Facts, r *ModuleResolver) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes := map[string]contract.Node{}
	var edges []contract.Edge
	diagnostics := append([]contract.Diagnostic{}, r.diagnostics...)
	for _, file := range facts.Paths(files) {
		f := files[file]
		f.UnknownSites = nil
		edges = append(edges, f.Edges...)
		unread := map[string]int{}
		if f.Rust != nil {
			for form, n := range f.Rust.Unread {
				unread[form] += n
			}
		}
		for _, imp := range f.Imports {
			name := strings.TrimSuffix(imp.Module, "::*")
			b := r.ResolveImport(file, r.ScopeAt(file, imp.Span), name)
			switch {
			case b.refused != "":
				if b.refused == unreadEdition2015 || b.refused == unreadEditionUnknown {
					structureTag("RS-I4")
				}
				structureSite("D", siteUse, file, imp.Span)
				unread[b.refused]++
			case b.File != "" && !b.Ambiguous:
				if r.sameCrate(file, b.crate) {
					structureTag("RS-I1")
				} else {
					structureTag("RS-I2")
				}
				structureSite("", siteClaimedUse, file, imp.Span)
				edges = append(edges, facts.Edge("imports", facts.ID("file", file, file), facts.ID("file", b.File, b.File), file, imp.Span))
			case b.External != "" && !b.Ambiguous:
				structureTag("RS-I3")
				structureSite("", siteClaimedUse, file, imp.Span)
				edges = append(edges, facts.Edge("imports", facts.ID("file", file, file), facts.External(nodes, "cargo", b.External), file, imp.Span))
			default:
				structureSite("RS-I5", siteUse, file, imp.Span)
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "in-crate import " + imp.Module + " is unavailable"})
			}
		}
		diagnostics = append(diagnostics, unreadDiagnostic(file, unread)...)
		for _, d := range f.Modules {
			if d.Inline {
				structureTag("RS-M4")
				continue
			}
			row := "RS-M2"
			if d.Path != "" {
				row = "RS-M3"
			}
			// Each `mod` item resolves from its own attributes and
			// directory in every crate (and module instance) holding it;
			// an edge only when they all name one file.
			targets := map[string]bool{}
			for _, c := range r.members[file] {
				for _, t := range c.modItems[modItemKey(file, d.Span)] {
					targets[t] = true
				}
			}
			target := ""
			for t := range targets {
				target = t
			}
			switch {
			case len(targets) == 1 && target != "" && target != ambiguousModule:
				structureTag(row)
				edges = append(edges, facts.Edge("imports", facts.ID("file", file, file), facts.ID("file", target, target), file, d.Span))
			case len(targets) == 1 && target == ambiguousModule:
				// Both candidates exist (rustc E0761): the ambiguous_module
				// warning was reported while building the tree.
				structureSite(row, siteMod, file, d.Span)
			default:
				structureSite(row, siteMod, file, d.Span)
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_module", Message: "module " + d.Name + " is unavailable"})
			}
		}
		ext, unknown := 0, 0
		for _, call := range f.Calls {
			b := Binding{}
			if call.Name != "" && !call.Shadowed {
				b = r.Resolve(file, r.ScopeAt(file, call.Span), call.Name)
			}
			kind, _, _, _ := contract.ParseIdentity(b.Identity)
			if (kind == "function" || kind == "method") && !b.Ambiguous {
				edges = append(edges, facts.Edge("calls", call.Source, b.Identity, file, call.Span))
			} else if b.External != "" && (b.External != call.Name || strings.Contains(call.Name, "::")) || (!call.Shadowed && facts.Builtin(strings.Split(call.Name, "::")[0], "std core alloc drop size_of size_of_val align_of align_of_val Some Ok Err Vec String Box Option Result")) {
				ext++
			} else {
				unknown++
				f.UnknownSites = append(f.UnknownSites, facts.Unknown(call))
			}
		}
		files[file] = f
		diagnostics = append(diagnostics, facts.Unresolved(file, ext, unknown)...)
	}
	n, edges := facts.Finish(nodes, edges)
	return n, edges, diagnostics
}
