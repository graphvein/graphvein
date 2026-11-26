package typescript

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// ModuleResolution is the outcome of one import specifier (rows TS-I1..I7).
type ModuleResolution struct {
	// File is the repository file the import names.
	File string
	// External marks a bare specifier the compiler does not resolve in the
	// repository whose package name is a valid npm name (row TS-I5).
	External bool
	// Refusal is the default-branch form when the import claims nothing.
	Refusal string
	// Candidates is kept for messages; the compiler's lookups are not listed.
	Candidates []string
	// Row is the contract row of a resolved or external import.
	Row string
	// DeclaredPackage marks a bare specifier refused only because the
	// file's options are unread (row TS-C1, config_unread) whose exact
	// package name a scanned package.json in the file's directory or an
	// ancestor declares (row TS-I5, rule (ii)), and that no workspace link
	// or scanned package.json name may make a repository file. It never
	// makes an import edge (the structure claims nothing for the site);
	// family consumers that need a package identity read it as the package
	// through Package and the export bindings.
	DeclaredPackage bool
}

// Package reports whether family consumers (models, table access,
// clients, routes, Angular, Tauri) may read the specifier as the npm
// package of its name: an external import (row TS-I5), or a declared
// package under unread options (DeclaredPackage). Structural import edges
// read External only.
func (m ModuleResolution) Package() bool { return m.External || m.DeclaredPackage }

// ExportBinding identifies a declaration or namespace without creating new nodes.
type ExportBinding struct {
	File, Name, Identity string
	Namespace, External  bool
	Ambiguous            bool
}

// ModuleResolver shares inventory-only decisions across calls, imports and routes.
// Its memoization lasts one relink; source cache payloads retain syntax only.
type ModuleResolver struct {
	files           map[string]Facts
	sources         map[string][]byte
	inventory       []string
	configs         map[string]moduleConfig
	badConfigs      map[string]bool
	fileConfigs     map[string]moduleConfig
	tsConfigs       map[string]*tsConfig
	governed        map[string]governedFile
	links           map[string]string
	linkKeys        []string
	outputs         map[string]string
	packageJSONs    map[string]parsedJSON
	packageNames    map[string]bool
	modules         map[string]ModuleResolution
	exports         map[string]ExportBinding
	diagnostics     []contract.Diagnostic
	staticClasses   map[string]facts.RouteExpr
	staticMutations map[string]bool
	// unread holds the entries on disk the scanner did not read (ignored,
	// skipped or dropped files; directories with a trailing "/"), set by
	// WithUnread. Only the CommonJS loader rows read it (Unread).
	unread map[string]bool
}

// WithUnread records the entries on disk that are not in the inventory:
// the scanner's skipped entries (scan.Result.Skipped: ignored names,
// unreadable, binary or symlinked files, directories with a trailing "/")
// and files dropped after reading. A runtime loader that would take one
// of them before a scanned candidate loads code Graphvein never read
// (row CJ-U1 of docs/extract/routes.md). It returns r.
func (r *ModuleResolver) WithUnread(paths []string) *ModuleResolver {
	if r.unread == nil {
		r.unread = map[string]bool{}
	}
	for _, p := range paths {
		r.unread[p] = true
	}
	return r
}

// Unread reports the unread entry that stands for file p on disk: p
// itself, when it was skipped as a file, or the nearest skipped directory
// holding it (its contents were never listed). It reads only the
// scanner's own list, never the file system, so no symlink is followed.
func (r *ModuleResolver) Unread(p string) (string, bool) {
	if r == nil || len(r.unread) == 0 {
		return "", false
	}
	if r.unread[p] {
		return p, true
	}
	for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if r.unread[dir+"/"] {
			return dir + "/", true
		}
	}
	return "", false
}

// NewModuleResolver rereads the scanned metadata on every relink, including replay.
func NewModuleResolver(files map[string]Facts, sources map[string][]byte) *ModuleResolver {
	r := &ModuleResolver{files: files, sources: sources, configs: map[string]moduleConfig{}, badConfigs: map[string]bool{}, fileConfigs: map[string]moduleConfig{}, tsConfigs: map[string]*tsConfig{}, governed: map[string]governedFile{}, packageJSONs: map[string]parsedJSON{}, modules: map[string]ModuleResolution{}, exports: map[string]ExportBinding{}}
	r.inventory = facts.Paths(files)
	loaded := map[string]bool{}
	for _, file := range r.inventory {
		source, ok := sources[file]
		if !ok {
			continue
		}
		switch path.Base(file) {
		case "package.json":
			var fields map[string]json.RawMessage
			if err := decodeObject(source, &fields); err != nil {
				r.parseError(file, err)
			}
		case "tsconfig.json", "jsconfig.json":
			r.loadConfig(file, sources, loaded)
		}
	}
	r.workspaceLinks()
	r.workspaceOutputs()
	r.prepareStaticCalls()
	return r
}

func (r *ModuleResolver) loadConfig(file string, sources map[string][]byte, loaded map[string]bool) {
	if loaded[file] {
		return
	}
	loaded[file] = true
	c, err := parseConfig(file, sources[file])
	if err != nil {
		r.parseError(file, err)
		r.badConfigs[file] = true
		return
	}
	r.configs[file] = c
	for _, parent := range c.parents {
		if !strings.HasPrefix(parent, ".") {
			continue
		}
		base := path.Clean(path.Join(path.Dir(file), parent))
		for _, candidate := range []string{base, base + ".json", path.Join(base, "tsconfig.json")} {
			if _, ok := r.files[candidate]; !ok {
				continue
			}
			if _, ok := sources[candidate]; ok {
				r.loadConfig(candidate, sources, loaded)
				break
			}
		}
	}
}

func (r *ModuleResolver) parseError(file string, err error) {
	r.diagnostics = append(r.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "config_parse_error", Message: fmt.Sprintf("parsing %s: %v", file, err)})
}

// Diagnostics exposes metadata failures without preventing other files from linking.
func (r *ModuleResolver) Diagnostics() []contract.Diagnostic { return r.diagnostics }

// Source returns a retained file's bytes as scanned (nil without
// sources). The Express route linker reads package.json manifests through
// it to learn which Express major parses a route path (T028).
func (r *ModuleResolver) Source(file string) ([]byte, bool) {
	if r == nil || r.sources == nil {
		return nil, false
	}
	data, ok := r.sources[file]
	return data, ok
}

func declaration(file string) bool {
	return strings.HasSuffix(file, ".d.ts") || strings.HasSuffix(file, ".d.mts") || strings.HasSuffix(file, ".d.cts")
}

func packageName(module string) string {
	parts := strings.Split(module, "/")
	if strings.HasPrefix(module, "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// Resolve resolves one import specifier of a file as the TypeScript 6.0
// compiler does with the file's governing options (rows TS-C1..C3,
// TS-I1..I7), memoized for the relink.
func (r *ModuleResolver) Resolve(file, module string) ModuleResolution {
	key := file + "\x00" + module
	if v, ok := r.modules[key]; ok {
		return v
	}
	v := r.resolveSite(file, module)
	r.modules[key] = v
	return v
}

// siteOutcome is one resolution under one set of options: a file, a
// refusal form, or neither (unresolved).
type siteOutcome struct {
	file, refusal string
	link          bool
}

func (r *ModuleResolver) resolveSite(file, module string) ModuleResolution {
	if module == "" || strings.HasPrefix(module, "/") {
		return ModuleResolution{}
	}
	relative := strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../") || module == "." || module == ".."
	o, _ := r.governing(file)
	var out siteOutcome
	if o == nil {
		// Row TS-C1's refusal: the file's options are not established. A
		// relative import claims only what every resolution kind agrees on.
		if !relative {
			return ModuleResolution{Refusal: unreadConfig, DeclaredPackage: r.unreadDeclaredPackage(file, module)}
		}
		for i, kind := range []int{resolveNode10, resolveBundler, resolveNodeNext} {
			alt := inferredOptions()
			alt.moduleResolution = kind
			if kind == resolveNodeNext {
				alt.module = moduleNodeNext
			}
			got := r.resolveWith(file, module, alt)
			if i == 0 {
				out = got
			} else if got != out {
				return ModuleResolution{Refusal: unreadConfig}
			}
		}
	} else {
		out = r.resolveWith(file, module, o)
	}
	if out.refusal != "" {
		return ModuleResolution{Refusal: out.refusal}
	}
	if out.file != "" {
		row := "TS-I3"
		switch {
		case relative && (assetExtension(module) || !hasAnySuffix(out.file, []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"})):
			row = "TS-I2"
		case relative:
			row = "TS-I1"
		case out.link || strings.HasPrefix(module, "#"):
			row = "TS-I4"
		}
		return ModuleResolution{File: out.file, Row: row}
	}
	if relative {
		// The joined path is kept for generated-client prediction (Orval),
		// which matches an import of a file not yet generated.
		return ModuleResolution{Row: "TS-I7", Candidates: []string{path.Clean(path.Join(path.Dir(file), module))}}
	}
	name := packageName(module)
	if r.linkedPackage(file, name) {
		return ModuleResolution{Refusal: unreadWorkspaceEntry}
	}
	if candidates, matched := pathsCandidates(module, o); matched {
		// A `paths` key matched and none of its targets exists in the
		// inventory: the compiler goes on to node_modules, but the targets
		// are often generated or ignored files of this repository, so the
		// name proves no package. The targets are kept for generated-client
		// prediction (Orval), as for a relative import.
		return ModuleResolution{Refusal: unreadPathsTargetMissing, Candidates: candidates}
	}
	if !validPackageName(name) {
		return ModuleResolution{Refusal: unreadNotAPackage}
	}
	if r.scannedPackageName(name) {
		// A package.json of the repository carries the name without being a
		// declared workspace member: it may still be installed from the
		// repository (a `file:` or `link:` dependency), so the import
		// claims neither that package nor an external one.
		return ModuleResolution{Refusal: unreadUnlinkedPackage}
	}
	if nodeBuiltin(module) || r.declaredDependency(file, name) {
		return ModuleResolution{External: true, Row: "TS-I5"}
	}
	// Bundler aliases, virtual modules and baseUrl or test-runner paths of
	// files outside the scanned inventory look like package names too: no
	// package.json in reach declares this one, so nothing proves a package.
	return ModuleResolution{Refusal: unreadUndeclaredPackage}
}

// unreadDeclaredPackage reports whether a bare specifier of a file whose
// options are unread is still the package for family consumers: its
// package name is valid, no workspace link or scanned package.json name
// may make it a repository file, and rule (ii) of row TS-I5 holds (a
// scanned package.json at or above the file declares the name). Node
// built-ins (rule (i)) are not included: an unread base could map them.
func (r *ModuleResolver) unreadDeclaredPackage(file, module string) bool {
	name := packageName(module)
	if !validPackageName(name) || strings.HasPrefix(name, "node:") {
		return false
	}
	return !r.linkedPackage(file, name) && !r.scannedPackageName(name) && r.declaredDependency(file, name)
}

// dependencyFields are the package.json fields whose keys declare a
// package the importing project installs (row TS-I5).
var dependencyFields = []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"}

// declaredDependency reports whether a scanned package.json in the
// importing file's directory or one of its ancestors declares name in one
// of dependencyFields (row TS-I5).
func (r *ModuleResolver) declaredDependency(file, name string) bool {
	for d := path.Dir(file); ; d = path.Dir(d) {
		if v, ok := r.packageJSON(path.Join(d, "package.json")); ok {
			for _, field := range dependencyFields {
				if deps, ok := v.get(field); ok && deps.Kind == "object" {
					if _, ok := deps.get(name); ok {
						return true
					}
				}
			}
		}
		if d == "." || d == "/" {
			return false
		}
	}
}

// nodeBuiltins is Node's list of built-in modules (module.builtinModules
// of Node 24) without the internal "_" names; "node:"-only modules are
// listed with their prefix.
var nodeBuiltins = map[string]bool{}

func init() {
	for _, name := range strings.Fields(`assert assert/strict async_hooks buffer child_process cluster console constants crypto dgram diagnostics_channel dns dns/promises domain events fs fs/promises http http2 https inspector inspector/promises module net os path path/posix path/win32 perf_hooks process punycode querystring readline readline/promises repl stream stream/consumers stream/promises stream/web string_decoder sys timers timers/promises tls trace_events tty url util util/types v8 vm wasi worker_threads zlib node:sea node:sqlite node:test node:test/reporters`) {
		nodeBuiltins[name] = true
	}
}

// nodeBuiltin reports whether a whole specifier names a Node built-in
// module: a listed name, with or without the "node:" prefix (the
// prefix-only modules only with it).
func nodeBuiltin(module string) bool {
	if nodeBuiltins[module] {
		return true
	}
	rest, ok := strings.CutPrefix(module, "node:")
	return ok && nodeBuiltins[rest]
}

// assetExtension reports a specifier whose extension (before any query) is
// not one the compiler loads as a module.
func assetExtension(module string) bool {
	m := module
	if i := strings.IndexAny(m, "?#"); i > 0 {
		m = m[:i]
	}
	ext := path.Ext(m)
	if ext == "" || strings.Contains(ext, "/") {
		return false
	}
	switch strings.ToLower(ext) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		return false
	}
	return true
}

// resolveWith resolves under one set of options: the compiler's result,
// the asset rule (row TS-I2) when it finds nothing, and the
// declaration-or-implementation check of O-S3-2.
func (r *ModuleResolver) resolveWith(file, module string, o *tsOptions) siteOutcome {
	mode, ok := r.siteMode(file, o)
	if !ok {
		return siteOutcome{refusal: refuseUnsupportedPackage}
	}
	target, refusal, link := r.compilerTarget(file, module, o, mode)
	if refusal != "" || target == "" {
		return siteOutcome{file: target, refusal: refusal}
	}
	impl := *o
	impl.noDtsResolution = true
	other, refusal, _ := r.compilerTarget(file, module, &impl, mode)
	if refusal != "" {
		return siteOutcome{refusal: refusal}
	}
	if other != "" && other != target {
		return siteOutcome{refusal: unreadDeclarationOrImpl}
	}
	return siteOutcome{file: target, link: link}
}

// compilerTarget is the compiler's resolution mapped to a repository file,
// with the asset rule as a second pass.
func (r *ModuleResolver) compilerTarget(file, module string, o *tsOptions, mode int) (string, string, bool) {
	res, refusal := r.compilerResolve(module, file, o, mode, false)
	if refusal != "" {
		return "", refusal, false
	}
	asset := assetExtension(module)
	if res == nil && asset {
		if res, refusal = r.compilerResolve(module, file, o, mode, true); refusal != "" {
			return "", refusal, false
		}
	}
	if res == nil {
		if asset && (strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../")) {
			if p := path.Join(path.Dir(file), module); r.isFile(p) {
				return p, "", false
			}
		}
		return "", "", false
	}
	link := containsNodeModules(res.path)
	if o.preserveSymlinks && link {
		return "", refuseUnsupportedPackage, false
	}
	target := r.realpath(res.path)
	if !r.isFile(target) {
		return "", refuseUnsupportedPackage, false
	}
	return target, "", link
}

// siteMode is the compiler's getModeForUsageLocation for an import or
// export declaration (getEmitSyntaxForUsageLocation). ok is false when the
// file's package scope cannot be read.
func (r *ModuleResolver) siteMode(file string, o *tsOptions) (int, bool) {
	res := o.resolution()
	if !(res >= resolveNode16 && res <= resolveNodeNext || o.exportsEnabled() || o.importsEnabled()) {
		return modeNone, true
	}
	implied, scopeType, ok := r.impliedNodeFormat(file, o)
	if !ok {
		return modeNone, false
	}
	format := 0
	module := o.emitModule()
	switch {
	case module >= moduleNode16 && module <= moduleNodeNext:
		format = implied
	case implied == modeCJS && (scopeType == "commonjs" || hasAnySuffix(file, []string{".cjs", ".cts"})):
		format = modeCJS
	case implied == modeESM && (scopeType == "module" || hasAnySuffix(file, []string{".mjs", ".mts"})):
		format = modeESM
	}
	if format == 0 {
		format = module
	}
	switch {
	case format == moduleCommonJS:
		return modeCJS, true
	case format >= moduleES2015 && format <= moduleESNext || format == modulePreserve:
		return modeESM, true
	}
	return modeNone, true
}

// impliedNodeFormat is the compiler's getImpliedNodeFormatForFile, with the
// "type" of the package scope it read.
func (r *ModuleResolver) impliedNodeFormat(file string, o *tsOptions) (int, string, bool) {
	switch {
	case hasAnySuffix(file, []string{".d.mts", ".mts", ".mjs"}):
		return modeESM, "", true
	case hasAnySuffix(file, []string{".d.cts", ".cts", ".cjs"}):
		return modeCJS, "", true
	}
	res := o.resolution()
	if !(res >= resolveNode16 && res <= resolveNodeNext || containsNodeModules(file)) || !hasAnySuffix(file, []string{".d.ts", ".ts", ".tsx", ".js", ".jsx"}) {
		return modeNone, "", true
	}
	s := &resolveState{r: r, o: o}
	scope := s.getPackageScopeForPath(path.Dir(file))
	if s.refusal != "" {
		return modeNone, "", false
	}
	t := ""
	if scope != nil {
		t, _ = scope.content.str("type")
	}
	if t == "module" {
		return modeESM, t, true
	}
	return modeCJS, t, true
}

// Export resolves one exported name; recursion guards are per name and path.
// Only complete top-level results are memoized, so cycles cannot poison a diamond.
func (r *ModuleResolver) Export(file, name string) ExportBinding {
	key := file + "\x00" + name
	if b, ok := r.exports[key]; ok {
		return b
	}
	b := r.export(file, name, map[string]bool{})
	r.exports[key] = b
	return b
}

func (r *ModuleResolver) imported(file, module, name string, seen map[string]bool) ExportBinding {
	v := r.Resolve(file, module)
	if v.Package() {
		return ExportBinding{File: module, Name: name, External: true, Namespace: name == "*"}
	}
	if v.File == "" {
		return ExportBinding{}
	}
	if name == "*" {
		return ExportBinding{File: v.File, Namespace: true}
	}
	return r.export(v.File, name, seen)
}

func (r *ModuleResolver) local(file, name string, seen map[string]bool) ExportBinding {
	f := r.files[file]
	for _, imp := range f.Imports {
		if exported, ok := imp.Bindings[name]; ok {
			return r.imported(file, imp.Module, exported, seen)
		}
	}
	for _, n := range f.Nodes {
		if n.QualifiedName == name {
			return ExportBinding{File: file, Name: name, Identity: n.Identity}
		}
	}
	if f.Routes != nil && len(f.Routes.Scopes) > 0 {
		if _, ok := f.Routes.Scopes[0].Bindings[name]; ok {
			return ExportBinding{File: file, Name: name}
		}
	}
	return ExportBinding{}
}

func (r *ModuleResolver) export(file, name string, seen map[string]bool) ExportBinding {
	key := file + "\x00" + name
	if seen[key] {
		return ExportBinding{}
	}
	seen[key] = true
	defer delete(seen, key)
	if head, tail, member := strings.Cut(name, "."); member {
		b := r.export(file, head, seen)
		return r.member(b, tail, seen)
	}
	f := r.files[file]
	// Clauses take precedence even when an explicit target is unavailable.
	for _, c := range f.ExportClauses {
		if c.Star || c.Alias != name {
			continue
		}
		if c.TypeOnly {
			return ExportBinding{}
		}
		if c.Namespace {
			return r.imported(file, c.Module, "*", seen)
		}
		if c.Module != "" {
			return r.imported(file, c.Module, c.Name, seen)
		}
		return r.local(file, c.Name, seen)
	}
	if identity := f.Exports[name]; identity != "" {
		_, _, q, err := contract.ParseIdentity(identity)
		if err == nil {
			if f.Routes != nil && f.Routes.Exports[name] != "" {
				q = f.Routes.Exports[name]
			}
			return ExportBinding{File: file, Name: q, Identity: identity}
		}
	}
	if f.Routes != nil {
		if local := f.Routes.Exports[name]; local != "" {
			return r.local(file, local, seen)
		}
	}
	if name == "default" {
		return ExportBinding{}
	}
	var result ExportBinding
	// typeOnly holds what each `export type *` would bind the name to. The
	// compiler takes a name from a type-only star too (and reports TS2308
	// when a value star also supplies it), while the emitted JavaScript
	// keeps only the value star: a name both reach through different
	// modules has no single answer, so it binds nothing (T020-TSG B1).
	var typeOnly []ExportBinding
	for _, c := range f.ExportClauses {
		if !c.Star {
			continue
		}
		if c.TypeOnly {
			if next := r.imported(file, c.Module, name, seen); next.Ambiguous || !next.External && next.File != "" {
				typeOnly = append(typeOnly, next)
			}
			continue
		}
		next := r.imported(file, c.Module, name, seen)
		if next.Ambiguous {
			return next
		}
		// Export-star of an external package has no known name inventory.
		if next.External || next.File == "" {
			continue
		}
		if result.File != "" && result != next {
			return ExportBinding{Ambiguous: true}
		}
		result = next
	}
	if result.File != "" {
		for _, t := range typeOnly {
			if t.Ambiguous || t != result {
				return ExportBinding{Ambiguous: true}
			}
		}
	}
	return result
}

func (r *ModuleResolver) member(b ExportBinding, name string, seen map[string]bool) ExportBinding {
	if b.External {
		if b.Name == "*" {
			b.Name = name
		} else {
			b.Name += "." + name
		}
		b.Namespace = false
		return b
	}
	if b.Namespace {
		return r.export(b.File, name, seen)
	}
	return ExportBinding{}
}

// ImportBinding resolves an imported binding, including namespace member chains.
func (r *ModuleResolver) ImportBinding(file, module, exported, member string) ExportBinding {
	v := r.Resolve(file, module)
	var b ExportBinding
	if v.Package() {
		b = ExportBinding{File: module, Name: exported, External: true, Namespace: exported == "*"}
	} else if v.File != "" {
		if exported == "*" {
			b = ExportBinding{File: v.File, Namespace: true}
		} else {
			b = r.Export(v.File, exported)
		}
	}
	if member != "" {
		b = r.Member(b, member)
	}
	return b
}

// Member resolves namespace chains with complete-result memoization.
func (r *ModuleResolver) Member(b ExportBinding, name string) ExportBinding {
	if b.External {
		return r.member(b, name, nil)
	}
	if b.Namespace {
		return r.Export(b.File, name)
	}
	return ExportBinding{}
}
