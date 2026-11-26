package typescript

import (
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file builds the filesystem the resolver sees (row TS-C3): the scanned
// inventory, plus a link "<root>/node_modules/<name>" for every member of a
// declared workspace (package.json "workspaces", pnpm-workspace.yaml
// "packages"), as the package managers link them, plus the virtual outputs
// of O-S3-3: through a link, a file a member's own tsconfig.json would emit
// into its outDir or declarationDir exists when its source file does, and
// stands for that source file.

// workspaceLinks finds the declared workspace members and links them.
func (r *ModuleResolver) workspaceLinks() {
	r.links = map[string]string{}
	var roots []string
	patterns := map[string][]string{}
	for _, file := range r.inventory {
		switch path.Base(file) {
		case "package.json":
			v, ok := r.packageJSON(file)
			if !ok {
				continue
			}
			w, ok := v.get("workspaces")
			if !ok {
				continue
			}
			if w.Kind == "object" {
				w, ok = w.get("packages")
			}
			if !ok || w.Kind != "array" {
				continue
			}
			dir := path.Dir(file)
			if _, seen := patterns[dir]; !seen {
				roots = append(roots, dir)
			}
			for _, item := range w.Items {
				if item.Kind == "string" {
					patterns[dir] = append(patterns[dir], item.Str)
				}
			}
		case "pnpm-workspace.yaml":
			dir := path.Dir(file)
			if _, seen := patterns[dir]; !seen {
				roots = append(roots, dir)
			}
			patterns[dir] = append(patterns[dir], pnpmPackages(string(r.sources[file]))...)
		}
	}
	sort.Strings(roots)
	for _, root := range roots {
		var include, exclude [][]string
		for _, p := range patterns[root] {
			neg := strings.HasPrefix(p, "!")
			p = strings.TrimPrefix(strings.TrimPrefix(p, "!"), "./")
			p = strings.TrimSuffix(p, "/")
			if p == "" {
				continue
			}
			comps := strings.Split(p, "/")
			if neg {
				exclude = append(exclude, comps)
			} else {
				include = append(include, comps)
			}
		}
		names := map[string][]string{}
		for _, file := range r.inventory {
			if path.Base(file) != "package.json" {
				continue
			}
			dir := path.Dir(file)
			if dir == root || !within(root, dir) {
				continue
			}
			rel := strings.TrimPrefix(dir, root+"/")
			if root == "." {
				rel = dir
			}
			if !workspaceGlob(include, rel) || workspaceGlob(exclude, rel) {
				continue
			}
			v, ok := r.packageJSON(file)
			if !ok {
				continue
			}
			name, ok := v.str("name")
			if !ok || name == "" {
				continue
			}
			names[name] = append(names[name], dir)
		}
		for _, name := range sortedKeys(names) {
			dirs := names[name]
			if len(dirs) > 1 {
				var files []string
				for _, d := range dirs {
					files = append(files, path.Join(d, "package.json"))
				}
				for _, f := range files {
					r.diagnostics = append(r.diagnostics, contract.Diagnostic{Type: "diagnostic", File: f, Severity: "warning", Code: "duplicate_workspace_package", Message: "duplicate workspace package: " + strings.Join(files, ", ")})
				}
				continue
			}
			structureTag("TS-C3")
			r.links[path.Join(root, "node_modules", name)] = dirs[0]
		}
	}
	r.linkKeys = sortedKeys(r.links)
}

// pnpmPackages reads the "packages" list of a pnpm-workspace.yaml: the
// block-sequence form `packages:` followed by `- glob` lines.
func pnpmPackages(text string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "packages:") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			if line == trimmed {
				in = false
			}
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if i := strings.Index(item, " #"); i >= 0 {
			item = strings.TrimSpace(item[:i])
		}
		item = strings.Trim(item, `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// workspaceGlob matches a member directory against package-manager globs:
// `*` and `?` within a segment, `**` for any number of segments; wildcards
// never match a segment that starts with "." or a node_modules folder.
func workspaceGlob(patterns [][]string, dir string) bool {
	segs := strings.Split(dir, "/")
	for _, p := range patterns {
		var match func(i, j int) bool
		match = func(i, j int) bool {
			if i == len(p) {
				return j == len(segs)
			}
			if p[i] == "**" {
				if match(i+1, j) {
					return true
				}
				for k := j; k < len(segs); k++ {
					if strings.HasPrefix(segs[k], ".") || segs[k] == "node_modules" {
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
			s := segs[j]
			if strings.ContainsAny(p[i], "*?") && (strings.HasPrefix(s, ".") || s == "node_modules") {
				return false
			}
			if !componentMatch(p[i], s, "exclude", false) {
				return false
			}
			return match(i+1, j+1)
		}
		if match(0, 0) {
			return true
		}
	}
	return false
}

// workspaceOutputs maps the outputs of every linked member's tsconfig.json
// (O-S3-3) to their source files: only outputs inside outDir or
// declarationDir, never for declaration or JSON inputs, and only from a
// configuration Graphvein reads that emits (no noEmit, no outFile).
func (r *ModuleResolver) workspaceOutputs() {
	r.outputs = map[string]string{}
	for _, key := range r.linkKeys {
		member := r.links[key]
		cfgPath := path.Join(member, "tsconfig.json")
		if !r.isFile(cfgPath) {
			continue
		}
		c := r.tsConfigAt(cfgPath)
		if c.unread != "" {
			continue
		}
		o := c.options
		if o.noEmit || o.outFile != "" || o.outDir == "" && o.declarationDir == "" {
			continue
		}
		common := c.dir
		if o.rootDir != "" {
			common = o.rootDir
		}
		for _, f := range sortedKeys(r.programFilesOf(c)) {
			if hasAnySuffix(f, declarationExtensions) || strings.HasSuffix(f, ".json") || isDeclarationLike(f) {
				continue
			}
			if !hasAnySuffix(f, []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}) {
				continue
			}
			rel := relPath(common, f)
			stem := removeFileExtension(rel)
			if o.outDir != "" && !o.emitDeclarationOnly {
				r.addOutput(path.Join(o.outDir, stem+outputExtension(f, o)), f)
			}
			if o.declaration || o.composite {
				if d := o.declarationDir; d != "" || o.outDir != "" {
					if d == "" {
						d = o.outDir
					}
					r.addOutput(path.Join(d, stem+declarationOutputExtension(f)), f)
				}
			}
		}
	}
}

func (r *ModuleResolver) programFilesOf(c *tsConfig) map[string]bool {
	if c.program == nil {
		c.program = r.programFiles(c)
	}
	return c.program
}

func (r *ModuleResolver) addOutput(out, source string) {
	if strings.HasPrefix(out, "../") || out == ".." {
		return
	}
	if _, ok := r.outputs[out]; !ok {
		r.outputs[out] = source
	}
}

// isDeclarationLike is the compiler's arbitrary-extension declaration file
// name ("x.d.css.ts").
func isDeclarationLike(f string) bool {
	base := path.Base(f)
	if !strings.HasSuffix(base, ".ts") {
		return false
	}
	i := strings.Index(base, ".d.")
	return i > 0
}

// relPath is p relative to dir ("../x" when p is outside dir).
func relPath(dir, p string) string {
	if dir == "." {
		return p
	}
	if within(dir, p) {
		return strings.TrimPrefix(p, dir+"/")
	}
	up := ""
	d := dir
	for !within(d, p) {
		up += "../"
		d = path.Dir(d)
		if d == "." {
			return up + p
		}
	}
	return up + strings.TrimPrefix(p, d+"/")
}

// outputExtension is the compiler's getOutputExtension.
func outputExtension(f string, o *tsOptions) string {
	switch {
	case o.jsx == jsxPreserve && hasAnySuffix(f, []string{".jsx", ".tsx"}):
		return ".jsx"
	case hasAnySuffix(f, []string{".mts", ".mjs"}):
		return ".mjs"
	case hasAnySuffix(f, []string{".cts", ".cjs"}):
		return ".cjs"
	}
	return ".js"
}

// declarationOutputExtension is the compiler's getDeclarationEmitExtensionForPath.
func declarationOutputExtension(f string) string {
	switch {
	case hasAnySuffix(f, []string{".mjs", ".mts"}):
		return ".d.mts"
	case hasAnySuffix(f, []string{".cjs", ".cts"}):
		return ".d.cts"
	}
	return ".d.ts"
}

// unlink maps a path through the workspace links: the real repository path
// and whether a link was crossed; "" when the path is under a node_modules
// directory no link explains.
func (r *ModuleResolver) unlink(p string) (string, bool) {
	via := false
	for {
		segs := strings.Split(p, "/")
		i := -1
		for k, s := range segs {
			if s == "node_modules" {
				i = k
				break
			}
		}
		if i < 0 {
			return p, via
		}
		n := 1
		if i+1 < len(segs) && strings.HasPrefix(segs[i+1], "@") {
			// A scoped name takes two segments; a member named "@x"
			// (no npm name, but package managers link it) takes one.
			if _, ok := r.links[strings.Join(segs[:i+2], "/")]; !ok {
				n = 2
			}
		}
		if i+n >= len(segs) {
			return "", via
		}
		key := strings.Join(segs[:i+1+n], "/")
		target, ok := r.links[key]
		if !ok {
			return "", via
		}
		via = true
		p = path.Join(append([]string{target}, segs[i+1+n:]...)...)
	}
}

// isFile reports a scanned file.
func (r *ModuleResolver) isFile(p string) bool {
	_, ok := r.files[p]
	return ok
}

// exists is the resolver's fileExists.
func (r *ModuleResolver) exists(p string) bool {
	real, via := r.unlink(p)
	if real == "" {
		return false
	}
	if r.isFile(real) {
		return true
	}
	if via {
		_, ok := r.outputs[real]
		return ok
	}
	return false
}

// realpath maps a resolved path to the repository file it stands for.
func (r *ModuleResolver) realpath(p string) string {
	real, via := r.unlink(p)
	if real == "" {
		return ""
	}
	if via && !r.isFile(real) {
		if source, ok := r.outputs[real]; ok {
			return source
		}
	}
	return real
}

// linkFolder reports whether a node_modules folder holds a link.
func (r *ModuleResolver) linkFolder(folder string) bool {
	prefix := folder + "/"
	i := sort.SearchStrings(r.linkKeys, prefix)
	return i < len(r.linkKeys) && strings.HasPrefix(r.linkKeys[i], prefix)
}

// packageJSON parses a scanned package.json; false when it does not parse.
func (r *ModuleResolver) packageJSON(p string) (jsonValue, bool) {
	if v, ok := r.packageJSONs[p]; ok {
		return v.value, v.ok
	}
	entry := parsedJSON{}
	if source, ok := r.sources[p]; ok {
		if data, err := jsonc(source); err == nil {
			if v, err := parseJSONValue(data); err == nil && v.Kind == "object" {
				entry = parsedJSON{v, true}
			}
		}
	}
	r.packageJSONs[p] = entry
	return entry.value, entry.ok
}

type parsedJSON struct {
	value jsonValue
	ok    bool
}

// scannedPackageName reports whether any scanned package.json is named name.
func (r *ModuleResolver) scannedPackageName(name string) bool {
	if r.packageNames == nil {
		r.packageNames = map[string]bool{}
		for _, file := range r.inventory {
			if path.Base(file) != "package.json" {
				continue
			}
			if v, ok := r.packageJSON(file); ok {
				if n, ok := v.str("name"); ok && n != "" {
					r.packageNames[n] = true
				}
			}
		}
	}
	return r.packageNames[name]
}

// linkedPackage reports whether a package name is linked in a workspace root
// that is an ancestor of the importing file.
func (r *ModuleResolver) linkedPackage(file, name string) bool {
	for d := path.Dir(file); ; d = path.Dir(d) {
		if _, ok := r.links[path.Join(d, "node_modules", name)]; ok {
			return true
		}
		if d == "." || d == "/" {
			return false
		}
	}
}

// validPackageName is the npm rule for new package names (row TS-I5):
// lower case letters, digits, "-", "." and "_", not starting with "." or
// "_", at most 214 characters, optionally under a "@scope/"; and Node's
// "node:" built-in names.
func validPackageName(name string) bool {
	if strings.HasPrefix(name, "node:") {
		return validPackageName(strings.TrimPrefix(name, "node:")) && !strings.Contains(name, "/")
	}
	if len(name) == 0 || len(name) > 214 {
		return false
	}
	part := func(s string) bool {
		if s == "" || s[0] == '.' || s[0] == '_' {
			return false
		}
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_') {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(name, "@") {
		scope, rest, ok := strings.Cut(name[1:], "/")
		return ok && part(scope) && part(rest)
	}
	return part(name)
}
