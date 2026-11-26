package typescript

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// This file reads tsconfig.json and jsconfig.json files as the TypeScript
// 6.0 compiler reads them (rows TS-C1 and TS-C2 of
// docs/lang/typescript-structure.md): `extends` chains, the compiler
// options module resolution depends on, the program's file list (`files`,
// `include`, `exclude`, `allowJs`) and `references`. A configuration whose
// meaning Graphvein cannot establish (a parse error, an `extends` it cannot
// resolve in the repository, an option value it does not know) is unread:
// the files it may govern resolve only what does not depend on it (row
// TS-C1's refusal).

// Compiler enum values (typescript.d.ts ModuleKind, ModuleResolutionKind,
// ScriptTarget, JsxEmit).
const (
	moduleCommonJS  = 1
	moduleES2015    = 5
	moduleES2020    = 6
	moduleES2022    = 7
	moduleESNext    = 99
	moduleNode16    = 100
	moduleNodeNext  = 199
	modulePreserve  = 200
	resolveClassic  = 1
	resolveNode10   = 2
	resolveNode16   = 3
	resolveNodeNext = 99
	resolveBundler  = 100
	targetESNext    = 99
	jsxPreserve     = 1
)

var moduleKinds = map[string]int{"none": 0, "commonjs": 1, "amd": 2, "system": 4, "umd": 3, "es6": 5, "es2015": 5, "es2020": 6, "es2022": 7, "esnext": 99, "node16": 100, "node18": 101, "node20": 102, "nodenext": 199, "preserve": 200}
var resolutionKinds = map[string]int{"node10": 2, "node": 2, "classic": 1, "node16": 3, "nodenext": 99, "bundler": 100}
var targetKinds = map[string]int{"es3": 0, "es5": 1, "es6": 2, "es2015": 2, "es2016": 3, "es2017": 4, "es2018": 5, "es2019": 6, "es2020": 7, "es2021": 8, "es2022": 9, "es2023": 10, "es2024": 11, "es2025": 12, "esnext": 99}
var jsxKinds = map[string]int{"preserve": 1, "react": 2, "react-native": 3, "react-jsx": 4, "react-jsxdev": 5}

// tsOptions are the compiler options Graphvein's resolution reads, with
// relative paths already resolved against the configuration that sets them.
// Pointers and -1 mean "not set" where the compiler computes a default.
type tsOptions struct {
	module, moduleResolution, target, jsx int
	baseURL                               string
	paths                                 *jsonValue
	pathsBase                             string
	// pathsTargets are the targets of each paths key, in order, with
	// ${configDir} substituted (pathsTarget).
	pathsTargets                        map[string][]pathsTarget
	rootDirs, typeRoots                 []string
	hasTypeRoots                        bool
	customConditions, moduleSuffixes    []string
	allowJs, checkJs, resolveJSONModule *bool
	resolveExports, resolveImports      *bool
	preserveSymlinks, noDtsResolution   bool
	outDir, rootDir, declarationDir     string
	outFile                             string
	declaration, composite              bool
	emitDeclarationOnly, noEmit         bool
	configFile, configDir               string
}

// inferredOptions are the options of the editor's inferred project in
// TypeScript 6.0 (row TS-C2): allowJs, resolveJsonModule, jsx preserve and
// the latest target, from which module computes to esnext and
// moduleResolution to bundler.
func inferredOptions() *tsOptions {
	t := true
	return &tsOptions{module: -1, moduleResolution: -1, target: targetESNext, jsx: jsxPreserve, allowJs: &t, resolveJSONModule: &t}
}

// emitModule is the compiler's computed `module` (computedOptions.module).
func (o *tsOptions) emitModule() int {
	if o.module >= 0 {
		return o.module
	}
	target := o.target
	if target < 0 || target == 0 {
		target = 12
	}
	switch {
	case target == targetESNext:
		return moduleESNext
	case target >= 9:
		return moduleES2022
	case target >= 7:
		return moduleES2020
	case target >= 2:
		return moduleES2015
	}
	return moduleCommonJS
}

// resolution is the compiler's computed `moduleResolution`.
func (o *tsOptions) resolution() int {
	if o.moduleResolution >= 0 {
		return o.moduleResolution
	}
	switch m := o.emitModule(); {
	case m == 0 || m == 2 || m == 3 || m == 4:
		return resolveClassic
	case m == moduleNodeNext:
		return resolveNodeNext
	case m >= moduleNode16 && m < moduleNodeNext:
		return resolveNode16
	}
	return resolveBundler
}

func supportsExportsAndImports(resolution int) bool {
	return resolution >= resolveNode16 && resolution <= resolveNodeNext || resolution == resolveBundler
}

// exportsEnabled and importsEnabled are resolvePackageJsonExports and
// resolvePackageJsonImports as computed by the compiler.
func (o *tsOptions) exportsEnabled() bool {
	if !supportsExportsAndImports(o.resolution()) {
		return false
	}
	if o.resolveExports != nil {
		return *o.resolveExports
	}
	return true
}

func (o *tsOptions) importsEnabled() bool {
	if !supportsExportsAndImports(o.resolution()) {
		return false
	}
	if o.resolveImports != nil {
		return *o.resolveImports
	}
	return true
}

// jsonModule is the computed resolveJsonModule.
func (o *tsOptions) jsonModule() bool {
	if o.resolveJSONModule != nil {
		return *o.resolveJSONModule
	}
	switch o.emitModule() {
	case 102, moduleNodeNext:
		return true
	}
	return o.resolution() == resolveBundler
}

// js is the computed allowJs.
func (o *tsOptions) js() bool {
	if o.allowJs != nil {
		return *o.allowJs
	}
	return o.checkJs != nil && *o.checkJs
}

// optionSource is one compilerOptions entry with the directory of the
// configuration that sets it, for relative paths.
type optionSource struct {
	value jsonValue
	dir   string
}

// specList is a files, include or exclude list with the directory its
// relative entries are relative to.
type specList struct {
	set   bool
	specs []string
	dir   string
}

// tsConfig is one configuration file as the compiler reads it.
type tsConfig struct {
	file, dir string
	// unread is why the configuration's meaning is not established; ""
	// when it is read.
	unread     string
	options    *tsOptions
	files      specList
	include    specList
	exclude    specList
	references []string
	// program is the set of inventory files the configuration's program
	// contains, computed on first use.
	program map[string]bool
	// raw keeps the merged option sources for the configurations that
	// extend this one.
	raw map[string]optionSource
}

// configRaw is one file's own content before extends is applied.
type configRaw struct {
	extends    []string
	hasExtends bool
	options    map[string]optionSource
	files      specList
	include    specList
	exclude    specList
	references []string
}

func readConfigRaw(file string, source []byte) (configRaw, error) {
	var out configRaw
	data, err := jsonc(source)
	if err != nil {
		return out, err
	}
	v, err := parseJSONValue(data)
	if err != nil {
		return out, err
	}
	if v.Kind != "object" {
		return out, fmt.Errorf("expected an object")
	}
	dir := path.Dir(file)
	out.options = map[string]optionSource{}
	if e, ok := v.get("extends"); ok {
		out.hasExtends = true
		switch e.Kind {
		case "string":
			out.extends = []string{e.Str}
		case "array":
			for _, item := range e.Items {
				if item.Kind != "string" {
					return out, fmt.Errorf("extends entries must be strings")
				}
				out.extends = append(out.extends, item.Str)
			}
		default:
			return out, fmt.Errorf("extends must be a string or an array")
		}
	}
	if o, ok := v.get("compilerOptions"); ok {
		if o.Kind != "object" {
			return out, fmt.Errorf("compilerOptions must be an object")
		}
		for i, key := range o.Keys {
			out.options[key] = optionSource{o.Values[i], dir}
		}
	}
	for _, field := range []struct {
		name string
		into *specList
	}{{"files", &out.files}, {"include", &out.include}, {"exclude", &out.exclude}} {
		list, ok := v.get(field.name)
		if !ok || list.Kind == "null" {
			continue
		}
		if list.Kind != "array" {
			return out, fmt.Errorf("%s must be an array", field.name)
		}
		field.into.set, field.into.dir = true, dir
		field.into.specs = []string{}
		for _, item := range list.Items {
			// The compiler drops entries that are not strings.
			if item.Kind == "string" {
				field.into.specs = append(field.into.specs, item.Str)
			}
		}
	}
	if refs, ok := v.get("references"); ok && refs.Kind == "array" {
		for _, ref := range refs.Items {
			p, ok := ref.str("path")
			if !ok {
				continue
			}
			p = path.Clean(path.Join(dir, p))
			if !strings.HasSuffix(p, ".json") {
				p = path.Join(p, "tsconfig.json")
			}
			out.references = append(out.references, p)
		}
	}
	return out, nil
}

// tsConfigAt reads and merges one configuration and its extends chain.
func (r *ModuleResolver) tsConfigAt(file string) *tsConfig {
	if c, ok := r.tsConfigs[file]; ok {
		return c
	}
	c := r.mergeConfig(file, map[string]bool{})
	if c.unread == "" {
		applySpecDefaults(c)
	}
	if c.unread == "" && path.Base(file) == "jsconfig.json" {
		// The compiler's defaults for a jsconfig.json (getDefaultCompilerOptions).
		if c.options.allowJs == nil {
			t := true
			c.options.allowJs = &t
		}
		if _, set := c.raw["noEmit"]; !set {
			c.options.noEmit = true
		}
	}
	r.tsConfigs[file] = c
	return c
}

func (r *ModuleResolver) mergeConfig(file string, stack map[string]bool) *tsConfig {
	c := &tsConfig{file: file, dir: path.Dir(file)}
	source, ok := r.sources[file]
	if !ok {
		c.unread = "configuration not in the inventory"
		return c
	}
	if stack[file] {
		c.unread = "circular extends"
		return c
	}
	stack[file] = true
	defer delete(stack, file)
	raw, err := readConfigRaw(file, source)
	if err != nil {
		c.unread = err.Error()
		return c
	}
	merged := map[string]optionSource{}
	var files, include, exclude specList
	for _, ext := range raw.extends {
		base := r.extendsTarget(c.dir, ext)
		if base == "" {
			c.unread = "extends " + ext + " is not a configuration in the repository"
			return c
		}
		parent := r.mergeConfig(base, stack)
		if parent.unread != "" {
			c.unread = "extends " + ext + ": " + parent.unread
			return c
		}
		for k, v := range parent.raw {
			merged[k] = v
		}
		if parent.files.set {
			files = parent.files
		}
		if parent.include.set {
			include = parent.include
		}
		if parent.exclude.set {
			exclude = parent.exclude
		}
	}
	for k, v := range raw.options {
		merged[k] = v
	}
	if raw.files.set {
		files = raw.files
	}
	if raw.include.set {
		include = raw.include
	}
	if raw.exclude.set {
		exclude = raw.exclude
	}
	c.raw, c.files, c.include, c.exclude, c.references = merged, files, include, exclude, raw.references
	o, err := optionsFrom(merged, c.dir)
	if err != nil {
		c.unread = err.Error()
		return c
	}
	o.configFile, o.configDir = file, c.dir
	c.options = o
	return c
}

// applySpecDefaults gives the configuration that is read (never one it
// extends) the compiler's default lists: exclude outDir and
// declarationDir when no exclude is given, include everything when
// neither files nor include is given.
func applySpecDefaults(c *tsConfig) {
	o := c.options
	if !c.exclude.set && (o.outDir != "" || o.declarationDir != "") {
		c.exclude = specList{set: true, dir: "."}
		for _, d := range []string{o.outDir, o.declarationDir} {
			if d != "" {
				c.exclude.specs = append(c.exclude.specs, d)
			}
		}
	}
	if !c.files.set && !c.include.set {
		c.include = specList{set: true, specs: []string{"**/*"}, dir: c.dir}
	}
}

// extendsTarget resolves an `extends` entry as the compiler does: a relative
// or rooted path is the file (with ".json" appended when that file does not
// exist); a package name is resolved as a configuration lookup through the
// workspace links. "" when the target is not a scanned file.
func (r *ModuleResolver) extendsTarget(dir, ext string) string {
	if strings.HasPrefix(ext, "./") || strings.HasPrefix(ext, "../") || ext == "." || ext == ".." || strings.HasPrefix(ext, "/") {
		p := path.Clean(path.Join(dir, ext))
		if !r.isFile(p) && !strings.HasSuffix(p, ".json") {
			p += ".json"
		}
		if r.isFile(p) {
			return p
		}
		return ""
	}
	o := &tsOptions{module: -1, moduleResolution: resolveNodeNext, target: -1, jsx: -1}
	s := &resolveState{r: r, o: o, features: featAll, conditions: []string{"require", "types", "node"}, configLookup: true, containingDir: dir}
	res := s.resolveNonRelativeOrRelative(ext, dir, extJSON)
	if res == nil || s.refusal != "" {
		return ""
	}
	p := r.realpath(res.path)
	if !r.isFile(p) {
		return ""
	}
	return p
}

// configDirTemplate is the template the compiler substitutes in path-valued
// options, `paths` targets and `files`/`include`/`exclude` entries.
const configDirTemplate = "${configDir}"

// configDirPath is the compiler's getSubstitutedPathWithConfigDirTemplate
// for a value that starts with the template (compared ignoring case, as
// startsWithConfigDirTemplate does): the first exact occurrence is replaced
// by "./" and the result is relative to the final configuration's
// directory. ok is false when the value does not start with the template.
func configDirPath(value, finalDir string) (string, bool) {
	if len(value) < len(configDirTemplate) || !strings.EqualFold(value[:len(configDirTemplate)], configDirTemplate) {
		return "", false
	}
	return path.Clean(path.Join(finalDir, strings.Replace(value, configDirTemplate, "./", 1))), true
}

// resolveOptionPath resolves a path-valued option against the configuration
// that sets it, or the final one for a ${configDir} value.
func resolveOptionPath(value, declaringDir, finalDir string) string {
	if p, ok := configDirPath(value, finalDir); ok {
		return p
	}
	return path.Clean(path.Join(declaringDir, value))
}

// pathsTarget is one `paths` target.
type pathsTarget struct {
	// text is the target as written, relative to the paths base, or for
	// a ${configDir} target the repository path the compiler substitutes.
	text string
	// rooted marks a substituted ${configDir} target: the compiler makes it
	// absolute, so the paths base does not apply.
	rooted bool
}

// optionsFrom converts the merged compilerOptions. An option Graphvein reads
// with a value of the wrong type or an unknown enum value makes the whole
// configuration unread: the compiler would report it and ignore the value,
// and a configuration with errors is not one whose meaning is established.
func optionsFrom(raw map[string]optionSource, finalDir string) (*tsOptions, error) {
	o := &tsOptions{module: -1, moduleResolution: -1, target: -1, jsx: -1}
	enum := func(key string, table map[string]int, into *int) error {
		v, ok := raw[key]
		if !ok || v.value.Kind == "null" {
			return nil
		}
		if v.value.Kind != "string" {
			return fmt.Errorf("compilerOptions.%s is not a string", key)
		}
		n, ok := table[strings.ToLower(v.value.Str)]
		if !ok {
			return fmt.Errorf("compilerOptions.%s value %q is not known", key, v.value.Str)
		}
		*into = n
		return nil
	}
	boolean := func(key string) (*bool, error) {
		v, ok := raw[key]
		if !ok || v.value.Kind == "null" {
			return nil, nil
		}
		if v.value.Kind != "bool" {
			return nil, fmt.Errorf("compilerOptions.%s is not a boolean", key)
		}
		b := v.value.Bool
		return &b, nil
	}
	str := func(key string, isPath bool) (string, error) {
		v, ok := raw[key]
		if !ok || v.value.Kind == "null" {
			return "", nil
		}
		if v.value.Kind != "string" {
			return "", fmt.Errorf("compilerOptions.%s is not a string", key)
		}
		if !isPath || v.value.Str == "" {
			return v.value.Str, nil
		}
		return resolveOptionPath(v.value.Str, v.dir, finalDir), nil
	}
	list := func(key string, isPath bool) ([]string, bool, error) {
		v, ok := raw[key]
		if !ok || v.value.Kind == "null" {
			return nil, false, nil
		}
		if v.value.Kind != "array" {
			return nil, false, fmt.Errorf("compilerOptions.%s is not an array", key)
		}
		out := []string{}
		for _, item := range v.value.Items {
			if item.Kind != "string" {
				return nil, false, fmt.Errorf("compilerOptions.%s has a value that is not a string", key)
			}
			s := item.Str
			if isPath {
				s = resolveOptionPath(s, v.dir, finalDir)
			}
			out = append(out, s)
		}
		return out, true, nil
	}
	var err error
	for _, e := range []struct {
		key   string
		table map[string]int
		into  *int
	}{{"module", moduleKinds, &o.module}, {"moduleResolution", resolutionKinds, &o.moduleResolution}, {"target", targetKinds, &o.target}, {"jsx", jsxKinds, &o.jsx}} {
		if err = enum(e.key, e.table, e.into); err != nil {
			return nil, err
		}
	}
	for _, b := range []struct {
		key  string
		into **bool
	}{{"allowJs", &o.allowJs}, {"checkJs", &o.checkJs}, {"resolveJsonModule", &o.resolveJSONModule}, {"resolvePackageJsonExports", &o.resolveExports}, {"resolvePackageJsonImports", &o.resolveImports}} {
		if *b.into, err = boolean(b.key); err != nil {
			return nil, err
		}
	}
	for _, b := range []struct {
		key  string
		into *bool
	}{{"preserveSymlinks", &o.preserveSymlinks}, {"noDtsResolution", &o.noDtsResolution}, {"declaration", &o.declaration}, {"composite", &o.composite}, {"emitDeclarationOnly", &o.emitDeclarationOnly}, {"noEmit", &o.noEmit}} {
		v, e := boolean(b.key)
		if e != nil {
			return nil, e
		}
		*b.into = v != nil && *v
	}
	for _, s := range []struct {
		key    string
		isPath bool
		into   *string
	}{{"baseUrl", true, &o.baseURL}, {"outDir", true, &o.outDir}, {"rootDir", true, &o.rootDir}, {"declarationDir", true, &o.declarationDir}, {"outFile", true, &o.outFile}} {
		if *s.into, err = str(s.key, s.isPath); err != nil {
			return nil, err
		}
	}
	if o.rootDirs, _, err = list("rootDirs", true); err != nil {
		return nil, err
	}
	if o.typeRoots, o.hasTypeRoots, err = list("typeRoots", true); err != nil {
		return nil, err
	}
	if o.customConditions, _, err = list("customConditions", false); err != nil {
		return nil, err
	}
	if o.moduleSuffixes, _, err = list("moduleSuffixes", false); err != nil {
		return nil, err
	}
	if p, ok := raw["paths"]; ok && p.value.Kind != "null" {
		if p.value.Kind != "object" {
			return nil, fmt.Errorf("compilerOptions.paths is not an object")
		}
		for _, targets := range p.value.Values {
			if targets.Kind != "array" {
				return nil, fmt.Errorf("compilerOptions.paths entries must be arrays")
			}
			for _, t := range targets.Items {
				if t.Kind != "string" {
					return nil, fmt.Errorf("compilerOptions.paths targets must be strings")
				}
			}
		}
		value := p.value
		o.paths, o.pathsBase = &value, p.dir
		o.pathsTargets = map[string][]pathsTarget{}
		for _, key := range value.Keys {
			targets, _ := value.get(key)
			list := make([]pathsTarget, 0, len(targets.Items))
			for _, t := range targets.Items {
				if p, ok := configDirPath(t.Str, finalDir); ok {
					list = append(list, pathsTarget{text: p, rooted: true})
				} else {
					list = append(list, pathsTarget{text: t.Str})
				}
			}
			o.pathsTargets[key] = list
		}
	}
	return o, nil
}

// inProgram reports whether a configuration's program contains a file.
func (r *ModuleResolver) inProgram(c *tsConfig, file string) bool {
	if c.program == nil {
		c.program = r.programFiles(c)
	}
	return c.program[file]
}

// Extension groups of the compiler's program file selection
// (allSupportedExtensions), in priority order.
var tsExtensionGroups = [][]string{{".ts", ".tsx", ".d.ts"}, {".cts", ".d.cts"}, {".mts", ".d.mts"}}
var allExtensionGroups = [][]string{{".ts", ".tsx", ".d.ts", ".js", ".jsx"}, {".cts", ".d.cts", ".cjs"}, {".mts", ".d.mts", ".mjs"}}

// fileExtension returns the extension of a group a file name has, the
// longest that matches (".d.ts" before ".ts").
func groupExtension(file string, groups [][]string) (group []string, ext string) {
	for _, g := range groups {
		for _, e := range g {
			if strings.HasSuffix(file, e) && len(e) > len(ext) {
				group, ext = g, e
			}
		}
	}
	return group, ext
}

// programFiles is the compiler's getFileNamesFromConfigSpecs over the
// scanned inventory: literal `files`, then the files `include` matches and
// `exclude` does not, with supported extensions, keeping for each base name
// the highest-priority extension of its group.
func (r *ModuleResolver) programFiles(c *tsConfig) map[string]bool {
	out := map[string]bool{}
	literal := map[string]bool{}
	if c.files.set {
		for _, spec := range c.files.specs {
			literal[specPath(spec, c.files.dir, c.dir)] = true
		}
	}
	for f := range literal {
		out[f] = true
	}
	if !c.include.set || len(c.include.specs) == 0 {
		return out
	}
	groups := tsExtensionGroups
	if c.options.js() {
		groups = allExtensionGroups
	}
	var includes, jsonIncludes, excludes [][]string
	for _, spec := range c.include.specs {
		if invalidIncludeSpec(spec) {
			continue
		}
		comps := specComponents(spec, c.include.dir, c.dir)
		if len(comps) == 0 || comps[len(comps)-1] == "**" {
			continue
		}
		includes = append(includes, comps)
		if strings.HasSuffix(spec, ".json") {
			jsonIncludes = append(jsonIncludes, comps)
		}
	}
	if c.exclude.set {
		for _, spec := range c.exclude.specs {
			if dotDotAfterRecursive(spec) {
				continue
			}
			excludes = append(excludes, specComponents(spec, c.exclude.dir, c.dir))
		}
	}
	wildcard := map[string]bool{}
	for _, f := range r.inventory {
		json := c.options.jsonModule() && strings.HasSuffix(f, ".json")
		if _, ext := groupExtension(f, groups); ext == "" && !json {
			continue
		}
		excluded := false
		for _, e := range excludes {
			if globMatch(e, f, "exclude") {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		set := includes
		if json {
			set = jsonIncludes
		}
		for _, inc := range set {
			if globMatch(inc, f, "files") {
				wildcard[f] = true
				break
			}
		}
	}
	for f := range wildcard {
		if literal[f] {
			continue
		}
		group, ext := groupExtension(f, groups)
		stem := strings.TrimSuffix(f, ext)
		shadowed := false
		for _, e := range group {
			if e == ext {
				break
			}
			if e == ".d.ts" && (ext == ".js" || ext == ".jsx") {
				continue
			}
			if literal[stem+e] || wildcard[stem+e] {
				shadowed = true
				break
			}
		}
		if !shadowed {
			out[f] = true
		}
	}
	return out
}

// specPath is a files entry as a repository path.
func specPath(spec, dir, finalDir string) string {
	return resolveOptionPath(spec, dir, finalDir)
}

// specComponents are the path components of an include or exclude entry,
// relative to the repository root ("." for the root itself is empty).
func specComponents(spec, dir, finalDir string) []string {
	p := specPath(spec, dir, finalDir)
	if p == "." {
		return []string{}
	}
	comps := strings.Split(p, "/")
	last := comps[len(comps)-1]
	if !strings.ContainsAny(last, ".*?") {
		comps = append(comps, "**", "*")
	}
	return comps
}

func invalidIncludeSpec(spec string) bool {
	return strings.HasSuffix(spec, "**") || dotDotAfterRecursive(spec)
}

func dotDotAfterRecursive(s string) bool {
	wildcard := -1
	if strings.HasPrefix(s, "**/") {
		wildcard = 0
	} else {
		wildcard = strings.Index(s, "/**/")
	}
	if wildcard == -1 {
		return false
	}
	lastDot := strings.LastIndex(s, "/../")
	if strings.HasSuffix(s, "/..") {
		lastDot = len(s)
	}
	return lastDot > wildcard
}

var commonPackageFolders = map[string]bool{"node_modules": true, "bower_components": true, "jspm_packages": true}

// globMatch is the compiler's wildcard matching (getSubPatternFromSpec with
// the files or exclude matcher) over path components. For "files", `**`
// and wildcard components never match a segment starting with "." or a
// package folder, and `*` never takes the "." of a final ".min.js". For
// "exclude" the pattern may match a leading directory of the path.
func globMatch(spec []string, file, usage string) bool {
	segs := strings.Split(file, "/")
	var match func(i, j int) bool
	match = func(i, j int) bool {
		if i == len(spec) {
			return j == len(segs) || usage == "exclude"
		}
		c := spec[i]
		if c == "**" {
			if match(i+1, j) {
				return true
			}
			for k := j; k < len(segs); k++ {
				s := segs[k]
				if usage != "exclude" && (strings.HasPrefix(s, ".") || commonPackageFolders[s] || s == "") {
					return false
				}
				if match(i+1, k+1) {
					return true
				}
			}
			return false
		}
		if j == len(segs) {
			return false
		}
		if !componentMatch(c, segs[j], usage, j == len(segs)-1) {
			return false
		}
		return match(i+1, j+1)
	}
	return match(0, 0)
}

func componentMatch(c, s, usage string, last bool) bool {
	if !strings.ContainsAny(c, "*?") {
		return c == s
	}
	if usage != "exclude" {
		if commonPackageFolders[s] {
			return false
		}
		if (c[0] == '*' || c[0] == '?') && strings.HasPrefix(s, ".") {
			return false
		}
	}
	pr, sr := []rune(c), []rune(s)
	var m func(i, j int) bool
	m = func(i, j int) bool {
		if i == len(pr) {
			return j == len(sr)
		}
		switch pr[i] {
		case '*':
			if m(i+1, j) {
				return true
			}
			for k := j; k < len(sr); k++ {
				if usage != "exclude" && sr[k] == '.' && last && string(sr[k+1:]) == "min.js" {
					return false
				}
				if m(i+1, k+1) {
					return true
				}
			}
			return false
		case '?':
			return j < len(sr) && m(i+1, j+1)
		}
		return j < len(sr) && sr[j] == pr[i] && m(i+1, j+1)
	}
	return m(0, 0)
}

// governing is the configuration whose options apply to a file (row
// TS-C1): from the file's directory up to the root, the first
// tsconfig.json, then jsconfig.json, whose program contains the file,
// directly or through its references. It returns nil options when the
// file's options cannot be established (an unread configuration is met
// first), and the inferred-project options (row TS-C2) when no
// configuration contains the file.
func (r *ModuleResolver) governing(file string) (*tsOptions, string) {
	if v, ok := r.governed[file]; ok {
		return v.options, v.config
	}
	o, cfg := r.findGoverning(file)
	switch {
	case o != nil && cfg != "":
		structureTag("TS-C1")
	case o != nil:
		structureTag("TS-C2")
	default:
		structureTag("D")
	}
	r.governed[file] = governedFile{o, cfg}
	return o, cfg
}

type governedFile struct {
	options *tsOptions
	config  string
}

func (r *ModuleResolver) findGoverning(file string) (*tsOptions, string) {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
			p := path.Join(dir, name)
			if !r.isFile(p) {
				continue
			}
			c, known := r.including(r.tsConfigAt(p), file, map[string]bool{})
			if !known {
				return nil, p
			}
			if c != nil {
				return c.options, c.file
			}
		}
		if dir == "." || dir == "/" || dir == ".." {
			break
		}
	}
	return inferredOptions(), ""
}

// including returns the configuration (c or one it references) whose program
// contains file; known is false when an unread configuration makes the
// answer unknown.
func (r *ModuleResolver) including(c *tsConfig, file string, seen map[string]bool) (*tsConfig, bool) {
	if c.unread != "" {
		return nil, false
	}
	if r.inProgram(c, file) {
		return c, true
	}
	if seen[c.file] {
		return nil, true
	}
	seen[c.file] = true
	for _, ref := range c.references {
		if !r.isFile(ref) {
			continue
		}
		hit, known := r.including(r.tsConfigAt(ref), file, seen)
		if !known {
			return nil, false
		}
		if hit != nil {
			return hit, true
		}
	}
	return nil, true
}

// sortedKeys returns a map's keys in order (deterministic diagnostics).
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
