package typescript

import (
	"path"
	"sort"
	"strings"
)

// This file transcribes the TypeScript 6.0 compiler's module resolution
// (moduleNameResolver.ts: nodeModuleNameResolverWorker and the functions it
// calls, for moduleResolution node10, node16, nodenext and bundler) over
// Graphvein's inventory: the scanned files plus the workspace links of row
// TS-C3, never a node_modules directory on disk. Names follow the
// compiler's so the two can be read side by side; TestTypeScriptStructureOracle
// compares the result with ts.resolveModuleName. Where the compiler would
// read something Graphvein does not model (typesVersions, versioned
// `types@` conditions, the outDir input mapping of an importing project,
// classic resolution) the state records a refusal and the import claims
// nothing.

// Extension sets (Extensions in the compiler).
const (
	extTS   = 1
	extJS   = 2
	extDecl = 4
	extJSON = 8
	extImpl = extTS | extJS
)

// Resolution features (NodeResolutionFeatures).
const (
	featImports     = 2
	featSelfName    = 4
	featExports     = 8
	featTrailers    = 16
	featEsm         = 32
	featImportsRoot = 64
	featAll         = 94
	featNode16      = 30
)

// Resolution modes (ResolutionMode: undefined, CommonJS, ESNext).
const (
	modeNone = 0
	modeCJS  = 1
	modeESM  = 99
)

// Refusals the resolver records (default-branch forms of the import rows).
const (
	refuseUnsupportedPackage = "unsupported_package"
)

type resolvedPath struct {
	path, ext string
}

// searchResult mirrors the compiler's SearchResult: a nil *searchResult is
// "no result, keep looking"; a non-nil one with a nil value stops the
// search without a file.
type searchResult struct {
	value *resolvedPath
}

func found(v *resolvedPath) *searchResult {
	if v == nil {
		return nil
	}
	return &searchResult{v}
}

type resolveState struct {
	r                *ModuleResolver
	o                *tsOptions
	features         int
	conditions       []string
	configLookup     bool
	fromPackageField bool
	containingDir    string
	refusal          string
	resolvedPackage  bool
	// asset enables Graphvein's asset rule (tryAsset).
	asset bool
}

func (s *resolveState) refuse(form string) {
	if s.refusal == "" {
		s.refusal = form
	}
}

func isRelativeName(name string) bool {
	return name == "." || name == ".." || strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/")
}

// getConditions is the compiler's condition list for a mode.
func getConditions(o *tsOptions, mode int) []string {
	res := o.resolution()
	if mode == modeNone {
		if res == resolveBundler {
			mode = modeESM
		} else if res == resolveNode10 {
			return []string{}
		}
	}
	var out []string
	if mode == modeESM {
		out = []string{"import"}
	} else {
		out = []string{"require"}
	}
	if !o.noDtsResolution {
		out = append(out, "types")
	}
	if res != resolveBundler {
		out = append(out, "node")
	}
	return append(out, o.customConditions...)
}

func nodeResolutionFeatures(o *tsOptions) int {
	f := 0
	switch o.resolution() {
	case resolveNode16:
		f = featNode16
	case resolveNodeNext, resolveBundler:
		f = featAll
	}
	if o.resolveExports != nil {
		if *o.resolveExports {
			f |= featExports
		} else {
			f &^= featExports
		}
	}
	if o.resolveImports != nil {
		if *o.resolveImports {
			f |= featImports
		} else {
			f &^= featImports
		}
	}
	return f
}

// compilerResolve is ts.resolveModuleName for one import site: the
// resolved file (a virtual path; see realpath) or nil, and the refusal
// form when resolution reached something Graphvein does not model.
func (r *ModuleResolver) compilerResolve(name, containingFile string, o *tsOptions, mode int, asset bool) (*resolvedPath, string) {
	dir := path.Dir(containingFile)
	var ext int
	if o.noDtsResolution {
		ext = extImpl
	} else {
		ext = extTS | extJS | extDecl
	}
	if o.jsonModule() {
		ext |= extJSON
	}
	var s *resolveState
	switch o.resolution() {
	case resolveNode16, resolveNodeNext:
		f := featNode16
		if o.resolution() == resolveNodeNext {
			f = featAll
		}
		if mode == modeESM {
			f |= featEsm
		}
		s = &resolveState{r: r, o: o, features: f, containingDir: dir}
		s.conditions = getConditions(o, map[bool]int{true: modeESM, false: modeCJS}[f&featEsm != 0])
	case resolveNode10:
		s = &resolveState{r: r, o: o, features: 0, containingDir: dir, conditions: []string{}}
		if mode != modeNone {
			s.features = featAll
			s.conditions = getConditions(o, mode)
		}
	case resolveBundler:
		s = &resolveState{r: r, o: o, features: nodeResolutionFeatures(o), containingDir: dir}
		s.conditions = getConditions(o, mode)
	default:
		return nil, unreadConfig
	}
	s.asset = asset
	var res *resolvedPath
	if o.resolution() == resolveNode10 {
		priority, secondary := ext&(extTS|extDecl), ext&^(extTS|extDecl)
		if priority != 0 {
			res = s.tryResolve(name, priority)
		}
		if res == nil && secondary != 0 && s.refusal == "" {
			res = s.tryResolve(name, secondary)
		}
	} else {
		res = s.tryResolve(name, ext)
	}
	if s.refusal != "" {
		return nil, s.refusal
	}
	return res, ""
}

// resolveNonRelativeOrRelative runs one tryResolve pass (used for extends
// lookups, which resolve JSON only).
func (s *resolveState) resolveNonRelativeOrRelative(name, dir string, ext int) *resolvedPath {
	s.containingDir = dir
	return s.tryResolve(name, ext)
}

func (s *resolveState) tryResolve(name string, ext int) *resolvedPath {
	dir := s.containingDir
	loader := func(ext int, candidate string, trailing bool) *resolvedPath {
		return s.nodeLoadModuleByRelativeName(ext, candidate, trailing, true)
	}
	if v, matched := s.tryLoadModuleUsingOptionalResolutionSettings(ext, name, dir, loader); matched {
		return v
	}
	if !isRelativeName(name) {
		if s.features&featImports != 0 && strings.HasPrefix(name, "#") {
			if res := s.loadModuleFromImports(ext, name, dir); res != nil {
				return res.value
			}
		}
		if s.features&featSelfName != 0 {
			if res := s.loadModuleFromSelfNameReference(ext, name, dir); res != nil {
				return res.value
			}
		}
		if strings.Contains(name, ":") {
			return nil
		}
		res := s.loadModuleFromNearestNodeModulesDirectory(ext, name, dir)
		if res == nil && ext&extDecl != 0 {
			res = s.resolveFromTypeRoot(name)
		}
		if res == nil {
			return nil
		}
		return res.value
	}
	candidate, trailing := cjsCandidate(dir, name)
	return s.nodeLoadModuleByRelativeName(ext, candidate, trailing, true)
}

// cjsCandidate is normalizePathForCJSResolution: "." and ".." (and a
// trailing slash) name a directory.
func cjsCandidate(dir, name string) (string, bool) {
	combined := path.Join(dir, name)
	trailing := strings.HasSuffix(name, "/") || name == "." || name == ".." || strings.HasSuffix(name, "/.") || strings.HasSuffix(name, "/..")
	if strings.HasPrefix(name, "/") {
		combined = path.Clean(name)
	}
	return combined, trailing
}

// tryLoadModuleUsingOptionalResolutionSettings: paths, then baseUrl for
// non-relative names or rootDirs for relative ones. matched is true when
// the compiler stops here (a paths pattern matched, even without a file).
func (s *resolveState) tryLoadModuleUsingOptionalResolutionSettings(ext int, name, dir string, loader func(int, string, bool) *resolvedPath) (*resolvedPath, bool) {
	if s.o.paths != nil && !isRelativeName(name) {
		base := s.o.pathsBase
		if s.o.baseURL != "" {
			base = s.o.baseURL
		}
		if res, matched := s.tryLoadModuleUsingPaths(ext, name, base, *s.o.paths, loader); matched {
			if res != nil {
				return res, true
			}
			// A matched pattern whose targets all fail stops the optional
			// settings; the compiler continues with node_modules.
			return nil, false
		}
	}
	if !isRelativeName(name) {
		if s.o.baseURL == "" {
			return nil, false
		}
		v := loader(ext, path.Join(s.o.baseURL, name), strings.HasSuffix(name, "/"))
		return v, v != nil
	}
	if len(s.o.rootDirs) == 0 {
		return nil, false
	}
	candidate, trailing := cjsCandidate(dir, name)
	prefixOf := func(rd string) string {
		if rd == "." {
			return ""
		}
		return rd + "/"
	}
	matched, ok := "", false
	for _, rd := range s.o.rootDirs {
		prefix := prefixOf(rd)
		if strings.HasPrefix(candidate, prefix) && (!ok || len(prefix) > len(matched)) {
			matched, ok = prefix, true
		}
	}
	if !ok {
		return nil, false
	}
	if v := loader(ext, candidate, trailing); v != nil {
		return v, true
	}
	suffix := strings.TrimPrefix(candidate, matched)
	skipped := false
	for _, rd := range s.o.rootDirs {
		if prefixOf(rd) == matched && !skipped {
			skipped = true
			continue
		}
		if v := loader(ext, path.Join(rd, suffix), trailing); v != nil {
			return v, true
		}
	}
	return nil, false
}

// pathsPattern is one parsed `paths` key.
type pathsPattern struct {
	key            string
	prefix, suffix string
	star           bool
}

// matchPaths is the compiler's key selection in tryLoadModuleUsingPaths:
// an exact key wins, else the pattern with the longest prefix (the first on
// ties). captured is the text the pattern's `*` matched.
func matchPaths(paths jsonValue, name string) (matched *pathsPattern, captured string) {
	var best *pathsPattern
	var exact *pathsPattern
	for _, key := range paths.Keys {
		star := strings.Index(key, "*")
		if star < 0 {
			if key == name && exact == nil {
				exact = &pathsPattern{key: key}
			}
			continue
		}
		if strings.Count(key, "*") > 1 {
			continue
		}
		p := &pathsPattern{key: key, prefix: key[:star], suffix: key[star+1:], star: true}
		if (best == nil || len(p.prefix) > len(best.prefix)) && len(name) >= len(p.prefix)+len(p.suffix) && strings.HasPrefix(name, p.prefix) && strings.HasSuffix(name, p.suffix) {
			best = p
		}
	}
	matched = exact
	if matched == nil {
		matched = best
	}
	if matched != nil && matched.star {
		captured = name[len(matched.prefix) : len(name)-len(matched.suffix)]
	}
	return matched, captured
}

// pathsCandidate is the path the compiler tries for one `paths` target, the
// first `*` replaced by the captured text and the target taken relative to
// base (rooted ${configDir} targets as they are). It also returns the
// substituted target for the extension and trailing-slash checks.
func pathsCandidate(t pathsTarget, matched *pathsPattern, captured, base string) (candidate, substituted string) {
	p := t.text
	if matched.star {
		p = strings.Replace(p, "*", captured, 1)
	}
	switch {
	case t.rooted:
		return path.Clean(p), p
	case strings.HasPrefix(p, "/"):
		return path.Clean(p), p
	}
	return path.Join(base, p), p
}

// tryLoadModuleUsingPaths: the matching key's targets are tried in order
// with the first `*` replaced by the matched text.
func (s *resolveState) tryLoadModuleUsingPaths(ext int, name, base string, paths jsonValue, loader func(int, string, bool) *resolvedPath) (*resolvedPath, bool) {
	matched, captured := matchPaths(paths, name)
	if matched == nil {
		return nil, false
	}
	for _, t := range s.o.pathsTargets[matched.key] {
		candidate, p := pathsCandidate(t, matched, captured, base)
		if extensionFromPath(t.text) != "" {
			if s.tryFile(candidate) {
				return &resolvedPath{candidate, extensionFromPath(t.text)}, true
			}
		}
		if v := loader(ext, candidate, strings.HasSuffix(p, "/")); v != nil {
			return v, true
		}
	}
	return nil, true
}

// pathsCandidates reports whether a `paths` key of the options matches a
// non-relative specifier, with the path the compiler tries for each target
// in order (row TS-I5's refusal; Orval predicts generated files from them).
func pathsCandidates(module string, o *tsOptions) ([]string, bool) {
	if o == nil || o.paths == nil || isRelativeName(module) {
		return nil, false
	}
	matched, captured := matchPaths(*o.paths, module)
	if matched == nil {
		return nil, false
	}
	base := o.pathsBase
	if o.baseURL != "" {
		base = o.baseURL
	}
	var candidates []string
	for _, t := range o.pathsTargets[matched.key] {
		candidate, _ := pathsCandidate(t, matched, captured, base)
		candidates = append(candidates, candidate)
	}
	return candidates, true
}

// extensionsToRemove is the compiler's list, in its order.
var extensionsToRemove = []string{".d.ts", ".d.mts", ".d.cts", ".mjs", ".mts", ".cjs", ".cts", ".ts", ".js", ".tsx", ".jsx", ".json"}

// extensionFromPath is tryGetExtensionFromPath2.
func extensionFromPath(p string) string {
	for _, e := range extensionsToRemove {
		if strings.HasSuffix(p, e) {
			return e
		}
	}
	if strings.HasSuffix(p, ".d.json.ts") {
		return ".d.json.ts"
	}
	return ""
}

func removeFileExtension(p string) string {
	for _, e := range extensionsToRemove {
		if strings.HasSuffix(p, e) {
			return strings.TrimSuffix(p, e)
		}
	}
	return p
}

func (s *resolveState) nodeLoadModuleByRelativeName(ext int, candidate string, trailing, considerPackageJSON bool) *resolvedPath {
	if !trailing {
		if v := s.loadModuleFromFile(ext, candidate); v != nil {
			return v
		}
	}
	if s.features&featEsm == 0 {
		return s.loadNodeModuleFromDirectory(ext, candidate, considerPackageJSON)
	}
	return nil
}

func (s *resolveState) loadModuleFromFile(ext int, candidate string) *resolvedPath {
	if v := s.loadModuleFromFileNoImplicitExtensions(ext, candidate); v != nil {
		return v
	}
	if s.features&featEsm == 0 {
		return s.tryAddingExtensions(candidate, ext, "")
	}
	return nil
}

func (s *resolveState) loadModuleFromFileNoImplicitExtensions(ext int, candidate string) *resolvedPath {
	base := path.Base(candidate)
	if !strings.Contains(base, ".") {
		return nil
	}
	extensionless := removeFileExtension(candidate)
	if extensionless == candidate {
		extensionless = candidate[:strings.LastIndex(candidate, ".")]
	}
	return s.tryAddingExtensions(extensionless, ext, candidate[len(extensionless):])
}

var tsImplementationExtensions = []string{".ts", ".tsx", ".mts", ".cts"}
var declarationExtensions = []string{".d.ts", ".d.mts", ".d.cts"}

func hasAnySuffix(p string, suffixes []string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(p, x) {
			return true
		}
	}
	return false
}

func (s *resolveState) loadFileNameFromPackageJSONField(ext int, candidate string) *resolvedPath {
	if ext&extTS != 0 && hasAnySuffix(candidate, tsImplementationExtensions) || ext&extDecl != 0 && hasAnySuffix(candidate, declarationExtensions) {
		if s.tryFile(candidate) {
			return &resolvedPath{candidate, extensionFromPath(candidate)}
		}
		return nil
	}
	if s.configLookup && ext == extJSON && strings.HasSuffix(candidate, ".json") {
		if s.tryFile(candidate) {
			return &resolvedPath{candidate, ".json"}
		}
		return nil
	}
	return s.loadModuleFromFileNoImplicitExtensions(ext, candidate)
}

func (s *resolveState) tryAddingExtensions(candidate string, ext int, original string) *resolvedPath {
	try := func(e string) *resolvedPath {
		if s.tryFile(candidate + e) {
			return &resolvedPath{candidate + e, e}
		}
		return nil
	}
	first := func(es ...string) *resolvedPath {
		for _, e := range es {
			if v := try(e); v != nil {
				return v
			}
		}
		return nil
	}
	var v *resolvedPath
	switch original {
	case ".mjs", ".mts", ".d.mts":
		if ext&extTS != 0 {
			v = first(".mts")
		}
		if v == nil && ext&extDecl != 0 {
			v = first(".d.mts")
		}
		if v == nil && ext&extJS != 0 {
			v = first(".mjs")
		}
	case ".cjs", ".cts", ".d.cts":
		if ext&extTS != 0 {
			v = first(".cts")
		}
		if v == nil && ext&extDecl != 0 {
			v = first(".d.cts")
		}
		if v == nil && ext&extJS != 0 {
			v = first(".cjs")
		}
	case ".json":
		if ext&extDecl != 0 {
			v = s.tryAsset(candidate, ".json")
		}
		if v == nil && ext&extJSON != 0 {
			v = first(".json")
		}
	case ".tsx", ".jsx":
		if ext&extTS != 0 {
			v = first(".tsx", ".ts")
		}
		if v == nil && ext&extDecl != 0 {
			v = first(".d.ts")
		}
		if v == nil && ext&extJS != 0 {
			v = first(".jsx", ".js")
		}
	case ".ts", ".d.ts", ".js", "":
		if ext&extTS != 0 {
			v = first(".ts", ".tsx")
		}
		if v == nil && ext&extDecl != 0 {
			v = first(".d.ts")
		}
		if v == nil && ext&extJS != 0 {
			v = first(".js", ".jsx")
		}
		if v == nil && s.configLookup {
			v = first(".json")
		}
	default:
		if ext&extDecl != 0 && !hasAnySuffix(candidate+original, declarationExtensions) {
			v = s.tryAsset(candidate, original)
		}
	}
	return v
}

// tryAsset is the compiler's lookup of "<x>.d<ext>.ts" for an import of
// "<x><ext>" with an extension the compiler does not load (row TS-I2): an
// existing declaration file of that name is the result; otherwise, in
// Graphvein's asset rule, the scanned file "<x><ext>" itself stands for it
// (the oracle runs the compiler with a host on which "<x>.d<ext>.ts" exists
// when "<x><ext>" does).
func (s *resolveState) tryAsset(candidate, original string) *resolvedPath {
	decl := candidate + ".d" + original + ".ts"
	if s.tryFile(decl) {
		return &resolvedPath{decl, ".d" + original + ".ts"}
	}
	if s.asset && s.tryFile(candidate+original) {
		return &resolvedPath{candidate + original, original}
	}
	return nil
}

// tryFile looks a file up, with moduleSuffixes when set.
func (s *resolveState) tryFile(name string) bool {
	if len(s.o.moduleSuffixes) == 0 {
		return s.r.exists(name)
	}
	ext := extensionFromPath(name)
	stem := strings.TrimSuffix(name, ext)
	for _, suffix := range s.o.moduleSuffixes {
		if s.r.exists(stem + suffix + ext) {
			// The compiler returns the suffixed name; Graphvein refuses
			// rather than model which of several names wins downstream.
			if suffix != "" {
				s.refuse(refuseUnsupportedPackage)
			}
			return suffix == ""
		}
	}
	return false
}

// packageJSONInfo is a directory's package.json as the compiler reads it.
type packageJSONInfo struct {
	dir     string
	content jsonValue
}

func (s *resolveState) getPackageJSONInfo(dir string) *packageJSONInfo {
	p := path.Join(dir, "package.json")
	if !s.r.exists(p) {
		return nil
	}
	real, _ := s.r.unlink(p)
	v, ok := s.r.packageJSON(real)
	if !ok {
		// A package.json Graphvein cannot parse: refuse what depends on it.
		s.refuse(refuseUnsupportedPackage)
		return nil
	}
	return &packageJSONInfo{dir: dir, content: v}
}

func (s *resolveState) packagePathField(info *packageJSONInfo, field string) string {
	v, ok := info.content.get(field)
	if !ok || v.Kind != "string" || v.Str == "" {
		return ""
	}
	return path.Join(info.dir, v.Str)
}

func (s *resolveState) checkTypesVersions(info *packageJSONInfo) {
	if v, ok := info.content.get("typesVersions"); ok && v.Kind == "object" {
		s.refuse(refuseUnsupportedPackage)
	}
}

func (s *resolveState) loadNodeModuleFromDirectory(ext int, candidate string, considerPackageJSON bool) *resolvedPath {
	var info *packageJSONInfo
	if considerPackageJSON {
		info = s.getPackageJSONInfo(candidate)
	}
	return s.loadNodeModuleFromDirectoryWorker(ext, candidate, info)
}

func (s *resolveState) loadNodeModuleFromDirectoryWorker(ext int, candidate string, info *packageJSONInfo) *resolvedPath {
	packageFile := ""
	if info != nil && info.dir == candidate {
		s.checkTypesVersions(info)
		if s.configLookup {
			packageFile = s.packagePathField(info, "tsconfig")
		} else {
			if ext&extDecl != 0 {
				packageFile = s.packagePathField(info, "typings")
				if packageFile == "" {
					packageFile = s.packagePathField(info, "types")
				}
			}
			if packageFile == "" && ext&(extImpl|extDecl) != 0 {
				packageFile = s.packagePathField(info, "main")
			}
		}
	}
	loader := func(ext int, candidate string) *resolvedPath {
		if v := s.loadFileNameFromPackageJSONField(ext, candidate); v != nil {
			return v
		}
		expanded := ext
		if ext == extDecl {
			expanded = extTS | extDecl
		}
		features, fromField := s.features, s.fromPackageField
		s.fromPackageField = true
		if info == nil || !isModuleType(info) {
			s.features &^= featEsm
		}
		v := s.nodeLoadModuleByRelativeName(expanded, candidate, false, false)
		s.features, s.fromPackageField = features, fromField
		return v
	}
	if packageFile != "" {
		if v := loader(ext, packageFile); v != nil {
			return v
		}
	}
	if s.features&featEsm == 0 {
		index := "index"
		if s.configLookup {
			index = "tsconfig"
		}
		return s.loadModuleFromFile(ext, path.Join(candidate, index))
	}
	return nil
}

func isModuleType(info *packageJSONInfo) bool {
	t, _ := info.content.str("type")
	return t == "module"
}

func parsePackageName(name string) (pkg, rest string) {
	i := strings.Index(name, "/")
	if strings.HasPrefix(name, "@") && i >= 0 {
		j := strings.Index(name[i+1:], "/")
		if j < 0 {
			return name, ""
		}
		i += 1 + j
	}
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

func allKeysStartWithDot(v jsonValue) bool {
	if v.Kind != "object" {
		return false
	}
	for _, k := range v.Keys {
		if !strings.HasPrefix(k, ".") {
			return false
		}
	}
	return true
}

func noKeyStartsWithDot(v jsonValue) bool {
	for _, k := range v.Keys {
		if strings.HasPrefix(k, ".") {
			return false
		}
	}
	return true
}

// getPackageScopeForPath is the nearest package.json at or above dir.
func (s *resolveState) getPackageScopeForPath(dir string) *packageJSONInfo {
	for d := dir; ; d = path.Dir(d) {
		if info := s.getPackageJSONInfo(d); info != nil {
			return info
		}
		if s.refusal != "" {
			return nil
		}
		if d == "." || d == "/" || strings.HasPrefix(d, "..") {
			return nil
		}
	}
}

func (s *resolveState) loadModuleFromSelfNameReference(ext int, name, dir string) *searchResult {
	scope := s.getPackageScopeForPath(dir)
	if scope == nil {
		return nil
	}
	if exports, ok := scope.content.get("exports"); !ok || !truthy(exports) {
		return nil
	}
	pkgName, ok := scope.content.str("name")
	if !ok {
		return nil
	}
	parts := strings.Split(name, "/")
	nameParts := strings.Split(pkgName, "/")
	if len(parts) < len(nameParts) {
		return nil
	}
	for i, p := range nameParts {
		if parts[i] != p {
			return nil
		}
	}
	trailing := parts[len(nameParts):]
	subpath := "."
	if len(trailing) > 0 {
		subpath = "./" + strings.Join(trailing, "/")
	}
	if s.o.js() && !containsNodeModules(dir) {
		return s.loadModuleFromExports(scope, ext, subpath)
	}
	priority, secondary := ext&(extTS|extDecl), ext&^(extTS|extDecl)
	if res := s.loadModuleFromExports(scope, priority, subpath); res != nil {
		return res
	}
	return s.loadModuleFromExports(scope, secondary, subpath)
}

func containsNodeModules(p string) bool {
	return strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/") || p == "node_modules" || strings.HasSuffix(p, "/node_modules")
}

// loadModuleFromExports: the main export for ".", else a subpath of an
// export map whose keys all start with ".". (The compiler's
// toSearchResult(undefined) is undefined: every failure here is nil.)
func (s *resolveState) loadModuleFromExports(scope *packageJSONInfo, ext int, subpath string) *searchResult {
	exports, ok := scope.content.get("exports")
	if !ok || !truthy(exports) {
		return nil
	}
	if subpath == "." {
		var main jsonValue
		has := false
		if exports.Kind == "string" || exports.Kind == "array" || exports.Kind == "object" && noKeyStartsWithDot(exports) {
			main, has = exports, true
		} else if exports.Kind == "object" {
			main, has = exports.get(".")
		}
		if has && truthy(main) {
			return s.loadModuleFromTargetExportOrImport(ext, scope, main, "", false, ".", false)
		}
		return nil
	}
	if exports.Kind == "object" && allKeysStartWithDot(exports) || exports.Kind == "array" && len(exports.Items) == 0 {
		if exports.Kind != "object" {
			return nil
		}
		return s.loadModuleFromExportsOrImports(ext, subpath, exports, scope, false)
	}
	return nil
}

// truthy is JavaScript truthiness of a JSON value.
func truthy(v jsonValue) bool {
	switch v.Kind {
	case "null":
		return false
	case "bool":
		return v.Bool
	case "string":
		return v.Str != ""
	case "number":
		return v.Str != "0" && v.Str != "-0" && v.Str != "0.0"
	}
	return true
}

func (s *resolveState) loadModuleFromImports(ext int, name, dir string) *searchResult {
	if name == "#" || strings.HasPrefix(name, "#/") && s.features&featImportsRoot == 0 {
		return nil
	}
	scope := s.getPackageScopeForPath(dir)
	if scope == nil {
		return nil
	}
	imports, ok := scope.content.get("imports")
	if !ok || !truthy(imports) {
		return nil
	}
	return s.loadModuleFromExportsOrImports(ext, name, imports, scope, true)
}

// comparePatternKeys orders expanding keys as the compiler does.
func comparePatternKeys(a, b string) int {
	ai, bi := strings.Index(a, "*"), strings.Index(b, "*")
	baseA, baseB := len(a), len(b)
	if ai != -1 {
		baseA = ai + 1
	}
	if bi != -1 {
		baseB = bi + 1
	}
	switch {
	case baseA > baseB:
		return -1
	case baseB > baseA:
		return 1
	case ai == -1:
		return 1
	case bi == -1:
		return -1
	case len(a) > len(b):
		return -1
	case len(b) > len(a):
		return 1
	}
	return 0
}

func (s *resolveState) loadModuleFromExportsOrImports(ext int, name string, table jsonValue, scope *packageJSONInfo, isImports bool) *searchResult {
	if !strings.HasSuffix(name, "/") && !strings.Contains(name, "*") {
		if target, ok := table.get(name); ok {
			return s.loadModuleFromTargetExportOrImport(ext, scope, target, "", false, name, isImports)
		}
	}
	var expanding []string
	for _, k := range table.Keys {
		if strings.Count(k, "*") == 1 || strings.HasSuffix(k, "/") {
			expanding = append(expanding, k)
		}
	}
	sort.SliceStable(expanding, func(i, j int) bool { return comparePatternKeys(expanding[i], expanding[j]) < 0 })
	for _, key := range expanding {
		target, _ := table.get(key)
		star := strings.Index(key, "*")
		if s.features&featTrailers != 0 && !strings.HasSuffix(key, "*") && star != -1 && strings.HasPrefix(name, key[:star]) && strings.HasSuffix(name, key[star+1:]) {
			sub := name[star : len(name)-(len(key)-1-star)]
			return s.loadModuleFromTargetExportOrImport(ext, scope, target, sub, true, key, isImports)
		} else if strings.HasSuffix(key, "*") && strings.HasPrefix(name, key[:len(key)-1]) {
			return s.loadModuleFromTargetExportOrImport(ext, scope, target, name[len(key)-1:], true, key, isImports)
		} else if strings.HasPrefix(name, key) {
			return s.loadModuleFromTargetExportOrImport(ext, scope, target, name[len(key):], false, key, isImports)
		}
	}
	return nil
}

func pathParts(p string) []string {
	var out []string
	for _, x := range strings.Split(p, "/") {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func containsBadPart(parts []string) bool {
	for _, p := range parts {
		if p == ".." || p == "." || p == "node_modules" {
			return true
		}
	}
	return false
}

func (s *resolveState) loadModuleFromTargetExportOrImport(ext int, scope *packageJSONInfo, target jsonValue, subpath string, pattern bool, key string, isImports bool) *searchResult {
	switch target.Kind {
	case "string":
		t := target.Str
		if !pattern && len(subpath) > 0 && !strings.HasSuffix(t, "/") {
			return nil
		}
		if !strings.HasPrefix(t, "./") {
			if isImports && !strings.HasPrefix(t, "../") && !strings.HasPrefix(t, "/") {
				lookup := t + subpath
				if pattern {
					lookup = strings.ReplaceAll(t, "*", subpath)
				}
				inner := &resolveState{r: s.r, o: s.o, features: s.features, conditions: s.conditions, containingDir: scope.dir, configLookup: s.configLookup, asset: s.asset}
				v := inner.tryResolve(lookup, ext)
				if inner.refusal != "" {
					s.refuse(inner.refusal)
				}
				return found(v)
			}
			return nil
		}
		parts := pathParts(t)
		if len(parts) > 1 && containsBadPart(parts[1:]) {
			return nil
		}
		if containsBadPart(pathParts(subpath)) {
			return nil
		}
		resolvedTarget := path.Join(scope.dir, t)
		if strings.HasSuffix(t, "/") {
			resolvedTarget += "/"
		}
		final := resolvedTarget + subpath
		if pattern {
			final = strings.ReplaceAll(resolvedTarget, "*", subpath)
		}
		final = path.Clean(final)
		s.checkInputFileMapping(final, scope)
		return found(s.loadFileNameFromPackageJSONField(ext, final))
	case "object":
		for i, cond := range target.Keys {
			if strings.HasPrefix(cond, "types@") && containsString(s.conditions, "types") {
				s.refuse(refuseUnsupportedPackage)
				return &searchResult{}
			}
			if cond == "default" || containsString(s.conditions, cond) {
				if res := s.loadModuleFromTargetExportOrImport(ext, scope, target.Values[i], subpath, pattern, key, isImports); res != nil {
					return res
				}
			}
		}
		return nil
	case "array":
		if len(target.Items) == 0 {
			return nil
		}
		for _, item := range target.Items {
			if res := s.loadModuleFromTargetExportOrImport(ext, scope, item, subpath, pattern, key, isImports); res != nil {
				return res
			}
		}
		return nil
	case "null":
		return &searchResult{}
	}
	return nil
}

// checkInputFileMapping refuses where the compiler's tryLoadInputFileForPath
// could map an export or import target in the importing project's outDir or
// declarationDir back to an input file: Graphvein does not model that
// guess of the common source directory.
func (s *resolveState) checkInputFileMapping(final string, scope *packageJSONInfo) {
	if s.configLookup || containsNodeModules(final) {
		return
	}
	if s.o.outDir == "" && s.o.declarationDir == "" {
		return
	}
	if s.o.configFile != "" && !within(scope.dir, s.o.configFile) {
		return
	}
	for _, d := range []string{s.o.declarationDir, s.o.outDir} {
		if d != "" && within(d, final) {
			s.refuse(refuseUnsupportedPackage)
		}
	}
}

// within reports whether p is dir or below it.
func within(dir, p string) bool {
	return dir == "." || p == dir || strings.HasPrefix(p, dir+"/")
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (s *resolveState) loadModuleFromNearestNodeModulesDirectory(ext int, name, dir string) *searchResult {
	priority, secondary := ext&(extTS|extDecl), ext&^(extTS|extDecl)
	if priority != 0 {
		if res := s.lookupNodeModules(priority, name, dir); res != nil {
			return res
		}
	}
	if secondary != 0 {
		return s.lookupNodeModules(secondary, name, dir)
	}
	return nil
}

func (s *resolveState) lookupNodeModules(ext int, name, dir string) *searchResult {
	for d := dir; ; d = path.Dir(d) {
		if path.Base(d) != "node_modules" {
			if res := s.loadModuleFromImmediateNodeModulesDirectory(ext, name, d); res != nil {
				return res
			}
			if s.refusal != "" {
				return &searchResult{}
			}
		}
		if d == "." || d == "/" || strings.HasPrefix(d, "..") {
			return nil
		}
	}
}

func (s *resolveState) loadModuleFromImmediateNodeModulesDirectory(ext int, name, dir string) *searchResult {
	folder := path.Join(dir, "node_modules")
	if !s.r.linkFolder(folder) {
		return nil
	}
	if v := s.loadModuleFromSpecificNodeModulesDirectory(ext, name, folder); v != nil {
		return &searchResult{v}
	}
	if ext&extDecl != 0 {
		if v := s.loadModuleFromSpecificNodeModulesDirectory(extDecl, mangleScopedPackageName(name), path.Join(folder, "@types")); v != nil {
			return &searchResult{v}
		}
	}
	return nil
}

func mangleScopedPackageName(name string) string {
	if strings.HasPrefix(name, "@") {
		replaced := strings.Replace(name, "/", "__", 1)
		if replaced != name {
			return replaced[1:]
		}
	}
	return name
}

func (s *resolveState) loadModuleFromSpecificNodeModulesDirectory(ext int, name, folder string) *resolvedPath {
	candidate := path.Join(folder, name)
	pkg, rest := parsePackageName(name)
	packageDir := path.Join(folder, pkg)
	info := s.getPackageJSONInfo(candidate)
	var root *packageJSONInfo
	if rest != "" && info != nil {
		hasExports := false
		if s.features&featExports != 0 {
			root = s.getPackageJSONInfo(packageDir)
			if root != nil {
				_, hasExports = root.content.get("exports")
			}
		}
		if !hasExports {
			if v := s.loadModuleFromFile(ext, candidate); v != nil {
				return v
			}
			return s.loadNodeModuleFromDirectoryWorker(ext, candidate, info)
		}
	}
	loader := func(ext int, candidate string) *resolvedPath {
		var v *resolvedPath
		if rest != "" || s.features&featEsm == 0 {
			v = s.loadModuleFromFile(ext, candidate)
		}
		if v == nil {
			v = s.loadNodeModuleFromDirectoryWorker(ext, candidate, info)
		}
		if v == nil && rest == "" && info != nil && s.features&featEsm != 0 {
			if e, ok := info.content.get("exports"); !ok || e.Kind == "null" {
				v = s.loadModuleFromFile(ext, path.Join(candidate, "index.js"))
			}
		}
		return v
	}
	if rest != "" {
		if root != nil {
			info = root
		} else {
			info = s.getPackageJSONInfo(packageDir)
		}
	}
	if info != nil {
		s.resolvedPackage = true
		if e, ok := info.content.get("exports"); ok && truthy(e) && s.features&featExports != 0 {
			sub := "."
			if rest != "" {
				sub = "./" + rest
			}
			res := s.loadModuleFromExports(info, ext, sub)
			if res == nil {
				return nil
			}
			return res.value
		}
		if rest != "" {
			s.checkTypesVersions(info)
		}
	}
	return loader(ext, candidate)
}

func (s *resolveState) resolveFromTypeRoot(name string) *searchResult {
	if !s.o.hasTypeRoots {
		return nil
	}
	for _, root := range s.o.typeRoots {
		lookup := name
		if strings.HasSuffix(root, "/node_modules/@types") || root == "node_modules/@types" {
			lookup = mangleScopedPackageName(name)
		}
		candidate := path.Join(root, lookup)
		if v := s.loadModuleFromFile(extDecl, candidate); v != nil {
			return &searchResult{v}
		}
		if v := s.loadNodeModuleFromDirectory(extDecl, candidate, true); v != nil {
			return &searchResult{v}
		}
	}
	return nil
}
