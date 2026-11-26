package python

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file holds row PS-M2 of docs/lang/python-structure.md (ADR 0072):
// workspace members installed as packages. It follows uv 0.13.9's
// discovery (crates/uv-workspace/src/workspace.rs) to decide which
// environment a Python file runs in and which projects that environment
// has installed, and hatchling 1.31.0 to decide which import packages each
// installed project exposes:
//
//   - A file's project is the nearest directory above it with a scanned
//     pyproject.toml. Its environment is the workspace that project
//     declares, or else the workspace of the first pyproject.toml above it
//     when that one declares a workspace that has the project as a member
//     (uv's find_workspace stops at the first pyproject.toml); otherwise
//     the project is standalone.
//   - A workspace's members are the root (when it has [project]) and the
//     directories its `members` globs match on disk, minus `exclude`, as
//     uv's collect_members_only reads them (see workspaceMembers).
//   - Installed: every member uv packages (tool.uv.package, else a
//     [build-system] table), and every editable uv path source that a
//     member's project.dependencies, or the root's default dependency
//     groups, name without a marker.
//
// Each installed project's packages are found on one search path entry
// after the importer's ancestor roots, as an installed environment finds
// them (an editable install puts the package's parent directory on
// sys.path through a .pth file, a regular install copies the package into
// site-packages: both give the same answer for the package's own name).
// Anything the resolution cannot read exactly refuses the workspace or the
// member: it adds no entry and reports a python_workspace_member_unread
// diagnostic.

// workspaceState is row PS-M2's reading of the scanned pyproject.toml
// files, built once per resolver.
type workspaceState struct {
	docs   map[string]map[string]any // by directory
	broken map[string]string         // directory -> TOML error
	// workspaces caches workspaceMembers by root.
	workspaces map[string]*workspaceInfo
	// envs caches the installed packages (name -> package directories) of
	// a project's environment, by project directory.
	envs map[string]map[string][]string
	// packages caches memberPackages by directory.
	packages map[string]memberLayout
	// children lists the subdirectories (walked and skipped) of each
	// directory, for the member globs.
	children map[string][]string
	refused  map[string]string // pyproject.toml path -> reason
	diags    []contract.Diagnostic
}

type workspaceInfo struct {
	members []string // sorted member directories, the root included
	refused string
}

type memberLayout struct {
	packages  map[string]string
	reason    string
	installed bool
}

// hatchlingBackend is the one build backend whose package layout PS-M2
// reads (hatchling 1.31.0's documented options and default file
// selection; its editable build is the reference for the .pth entry).
const hatchlingBackend = "hatchling.build"

// readWorkspaces builds r.ws from the scanned pyproject.toml files, once
// per resolver: every project's environment is computed so the refusals
// are reported whichever files import what.
func (r *ModuleResolver) readWorkspaces() *workspaceState {
	if r.ws != nil {
		return r.ws
	}
	w := &workspaceState{docs: map[string]map[string]any{}, broken: map[string]string{}, workspaces: map[string]*workspaceInfo{}, envs: map[string]map[string][]string{}, packages: map[string]memberLayout{}, refused: map[string]string{}}
	r.ws = w
	dirs := sortedKeys(r.pyprojects)
	for _, dir := range dirs {
		doc, err := parseTOML(r.pyprojects[dir])
		if err != nil {
			w.broken[dir] = err.Error()
			continue
		}
		w.docs[dir] = doc
	}
	for _, dir := range dirs {
		r.environment(dir)
	}
	for _, file := range sortedKeys(w.refused) {
		w.diags = append(w.diags, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "python_workspace_member_unread", Message: "workspace member or declaration not modelled: " + w.refused[file] + "; its packages are not on the search path"})
	}
	return w
}

func (w *workspaceState) refuse(dir, reason string) {
	file := pyprojectPath(dir)
	if _, done := w.refused[file]; !done {
		w.refused[file] = reason
	}
}

// WorkspaceDiagnostics returns the PS-M2 refusals: one info diagnostic
// per pyproject.toml whose workspace declaration or member layout the
// resolution cannot read exactly.
func (r *ModuleResolver) WorkspaceDiagnostics() []contract.Diagnostic {
	return r.readWorkspaces().diags
}

// siteFor returns the installed packages of file's environment (nil when
// there is none).
func (r *ModuleResolver) siteFor(file string) map[string][]string {
	r.readWorkspaces()
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if _, ok := r.pyprojects[dir]; ok {
			return r.environment(dir)
		}
		if dir == "." {
			return nil
		}
	}
}

// environment returns the installed packages of the environment of the
// project in dir (uv's Workspace::discover and find_workspace).
func (r *ModuleResolver) environment(dir string) map[string][]string {
	w := r.ws
	if env, ok := w.envs[dir]; ok {
		return env
	}
	w.envs[dir] = nil
	doc, ok := w.docs[dir]
	if !ok || managedFalse(doc) {
		return nil
	}
	var env map[string][]string
	if _, declares := tomlTable(doc, "tool", "uv", "workspace"); declares {
		env = r.workspaceEnvironment(dir)
	} else {
		parent := ""
		for d := dir; d != "."; {
			d = path.Dir(d)
			if _, ok := r.pyprojects[d]; ok {
				parent = d
				break
			}
		}
		switch {
		case parent == "":
			env = r.standaloneEnvironment(dir)
		case w.broken[parent] != "":
			// uv fails to read the workspace candidate: no environment.
		default:
			if _, declares := tomlTable(w.docs[parent], "tool", "uv", "workspace"); declares {
				info := r.workspaceMembers(parent)
				if info.refused == "" && contains(info.members, dir) {
					env = r.workspaceEnvironment(parent)
				} else if info.refused == "" {
					// Not included, or excluded: a standalone project.
					env = r.standaloneEnvironment(dir)
				}
			} else {
				env = r.standaloneEnvironment(dir)
			}
		}
	}
	w.envs[dir] = env
	return env
}

func managedFalse(doc map[string]any) bool {
	uv, _ := tomlTable(doc, "tool", "uv")
	v, set := uv["managed"]
	return set && v == false
}

// workspaceEnvironment is the installed packages of the workspace rooted
// at root: its packaged members and the editable path sources their
// requirements name.
func (r *ModuleResolver) workspaceEnvironment(root string) map[string][]string {
	info := r.workspaceMembers(root)
	if info.refused != "" {
		r.ws.refuse(root, info.refused)
		return nil
	}
	env := map[string][]string{}
	for _, m := range info.members {
		r.install(env, m)
	}
	rootSources, _ := tomlTable(r.ws.docs[root], "tool", "uv", "sources")
	for _, m := range info.members {
		own, _ := tomlTable(r.ws.docs[m], "tool", "uv", "sources")
		for _, name := range projectRequirements(r.ws.docs[m]) {
			// A member's own source wins; the root's applies otherwise,
			// with its path relative to the root.
			if src, ok := sourceFor(own, name); ok {
				r.installSource(env, m, name, src)
			} else if src, ok := sourceFor(rootSources, name); ok {
				r.installSource(env, root, name, src)
			}
		}
	}
	for _, name := range groupRequirements(r.ws.docs[root]) {
		if src, ok := sourceFor(rootSources, name); ok {
			r.installSource(env, root, name, src)
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// sourceFor finds the [tool.uv.sources] entry of a normalised
// requirement name.
func sourceFor(sources map[string]any, name string) (any, bool) {
	for key, v := range sources {
		if normalizeProjectName(key) == name {
			return v, true
		}
	}
	return nil, false
}

// standaloneEnvironment is the installed packages of a project outside
// any workspace: the editable path sources its requirements name (the
// project itself is an ancestor root already).
func (r *ModuleResolver) standaloneEnvironment(dir string) map[string][]string {
	doc := r.ws.docs[dir]
	sources, _ := tomlTable(doc, "tool", "uv", "sources")
	env := map[string][]string{}
	for _, name := range append(projectRequirements(doc), groupRequirements(doc)...) {
		if src, ok := sourceFor(sources, name); ok {
			r.installSource(env, dir, name, src)
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// install adds the packages of the project in dir to env.
func (r *ModuleResolver) install(env map[string][]string, dir string) {
	layout := r.memberPackages(dir)
	if layout.reason != "" {
		r.ws.refuse(dir, layout.reason)
		return
	}
	if !layout.installed {
		return
	}
	structureTag("PS-M2")
	for name, pkg := range layout.packages {
		if !contains(env[name], pkg) {
			env[name] = append(env[name], pkg)
		}
	}
}

// installSource installs the uv source of requirement name declared for
// the project in dir, when it is an editable path source.
func (r *ModuleResolver) installSource(env map[string][]string, dir, name string, v any) {
	target, reason := pathSource(dir, v)
	if reason != "" {
		r.ws.refuse(dir, fmt.Sprintf("uv source %s: %s", name, reason))
		return
	}
	if target == "" {
		return
	}
	if _, scanned := r.pyprojects[target]; !scanned {
		r.ws.refuse(dir, fmt.Sprintf("uv source %s: %s has no scanned pyproject.toml", name, target))
		return
	}
	r.install(env, target)
}

// requirementName matches a PEP 508 requirement's name and what follows.
var requirementName = regexp.MustCompile(`^\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(.*)$`)

// requirementNames returns the PEP 503 normalised names of the
// requirements in list that are installed unconditionally: a requirement
// with an environment marker (`;`) or a direct reference (`@`) is left
// out (a marker depends on the platform; a direct reference takes no
// source).
func requirementNames(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			continue
		}
		m := requirementName.FindStringSubmatch(s)
		if m == nil || strings.ContainsAny(m[2], ";@") {
			continue
		}
		out = append(out, normalizeProjectName(m[1]))
	}
	return out
}

// projectRequirements returns a project's unconditional
// project.dependencies (optional dependencies are installed only with
// --extra, which the default sync does not pass).
func projectRequirements(doc map[string]any) []string {
	project, _ := tomlTable(doc, "project")
	return requirementNames(project["dependencies"])
}

// groupRequirements returns the requirements of the default dependency
// groups uv installs for a project (`dev`, or tool.uv.default-groups: a
// list of names or "all"). Included groups ({include-group = ...}) are
// not followed (left out).
func groupRequirements(doc map[string]any) []string {
	groups, _ := tomlTable(doc, "dependency-groups")
	uv, _ := tomlTable(doc, "tool", "uv")
	wanted := map[string]bool{"dev": true}
	if v, set := uv["default-groups"]; set {
		wanted = map[string]bool{}
		switch d := v.(type) {
		case string:
			if d == "all" {
				for name := range groups {
					wanted[normalizeProjectName(name)] = true
				}
			}
		case []any:
			for _, g := range d {
				if s, ok := g.(string); ok {
					wanted[normalizeProjectName(s)] = true
				}
			}
		}
	}
	var out []string
	for _, name := range sortedKeys(groups) {
		if wanted[normalizeProjectName(name)] {
			out = append(out, requirementNames(groups[name])...)
		}
	}
	return out
}

var nameSeparators = regexp.MustCompile(`[-_.]+`)

// normalizeProjectName is PEP 503 normalisation, as uv compares package
// and source names.
func normalizeProjectName(name string) string {
	return strings.ToLower(nameSeparators.ReplaceAllString(name, "-"))
}

func pyprojectPath(dir string) string { return path.Join(dir, "pyproject.toml") }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tomlTable returns the table at the dotted key path, if it is a table.
func tomlTable(doc map[string]any, keys ...string) (map[string]any, bool) {
	var v any = doc
	for _, k := range keys {
		t, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = t[k]; !ok {
			return nil, false
		}
	}
	t, ok := v.(map[string]any)
	return t, ok
}

// stringList reads an array of non-empty strings.
func stringList(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok || s == "" {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// relativeDir cleans a path written relative to base; it refuses absolute
// paths, Windows separators and paths leaving the repository.
func relativeDir(base, p string) (string, bool) {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return "", false
	}
	joined := path.Join(base, p)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// globMatch is the glob crate's (0.3.4) Pattern matching with the default
// MatchOptions for patterns of `*`, `?` and literal characters (callers
// refuse `[`, `]` and `**`): `*` matches any sequence and `?` any one
// character, both also `/` (require_literal_separator is false, so uv's
// exclusions cross directories), and a leading dot needs no literal dot
// (require_literal_leading_dot is false). Member globs call it per path
// component, as glob() does.
func globMatch(pattern, name string) bool {
	p, s := []rune(pattern), []rune(name)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		for i < len(p) {
			switch p[i] {
			case '*':
				for k := j; k <= len(s); k++ {
					if match(i+1, k) {
						return true
					}
				}
				return false
			case '?':
				if j == len(s) {
					return false
				}
			default:
				if j == len(s) || s[j] != p[i] {
					return false
				}
			}
			i++
			j++
		}
		return j == len(s)
	}
	return match(0, 0)
}

// globSyntaxModelled reports whether a member or exclude pattern uses
// only the glob syntax PS-M2 models exactly (`*`, `?`, literals; the glob
// crate gives no other character a meaning outside `[...]`).
func globSyntaxModelled(pattern string) bool {
	return !strings.ContainsAny(pattern, "[]\\") && !strings.Contains(pattern, "**")
}

// isDir reports whether p is a directory on disk as the scan saw it, and
// whether its contents are unknown (a skipped directory, or inside one).
func (r *ModuleResolver) isDir(p string) (dir, unknown bool) {
	if r.unreadDirs[p] {
		return true, true
	}
	for d := path.Dir(p); d != "."; d = path.Dir(d) {
		if r.unreadDirs[d] {
			return true, true
		}
	}
	return p == "." || r.directories[p], false
}

// subdirectories lists dir's subdirectories, walked or skipped.
func (r *ModuleResolver) subdirectories(dir string) []string {
	w := r.ws
	if w.children == nil {
		w.children = map[string][]string{}
		for d := range r.directories {
			w.children[path.Dir(d)] = append(w.children[path.Dir(d)], d)
		}
		for d := range r.unreadDirs {
			if !r.directories[d] {
				w.children[path.Dir(d)] = append(w.children[path.Dir(d)], d)
			}
		}
	}
	return w.children[dir]
}

// noFiles reports that no file lies under dir, scanned or skipped (uv's
// has_only_gitignored_files holds for it whatever the ignore rules).
func (r *ModuleResolver) noFiles(dir string) bool {
	under := func(p string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }
	for f := range r.files {
		if under(f) {
			return false
		}
	}
	for d, entries := range r.unread {
		if under(d) && len(entries) > 0 {
			return false
		}
	}
	for d := range r.unreadDirs {
		if under(d) {
			return false
		}
	}
	return true
}

// globDirectories expands a member glob (cleaned, relative to the
// workspace root, whose own name is literal) as the glob crate's glob()
// walks it: a literal component is kept when the
// path exists, a wildcard component lists the directory and matches each
// name (hidden names too). Files are dropped (uv skips a match that is not
// a directory). It returns the matched directories, or a reason when a
// walk would read a directory the scan skipped.
func (r *ModuleResolver) globDirectories(root, pattern string) ([]string, string) {
	current := []string{root}
	var parts []string
	if pattern != "." {
		parts = strings.Split(pattern, "/")
	}
	for _, comp := range parts {
		var next []string
		for _, c := range current {
			if _, unknown := r.isDir(c); unknown {
				return nil, "the member glob walks " + c + ", a directory the scan skipped"
			}
			if !strings.ContainsAny(comp, "*?") {
				p := path.Join(c, comp)
				if dir, _ := r.isDir(p); dir {
					next = append(next, p)
				}
				continue
			}
			for _, d := range r.subdirectories(c) {
				if globMatch(comp, path.Base(d)) {
					next = append(next, d)
				}
			}
		}
		current = next
	}
	sort.Strings(current)
	return current, ""
}

// workspaceMembers reads the members of the uv workspace declared in
// root, as uv's collect_members_only does (workspace.rs 0.13.9): the root
// when it has [project]; for each `members` glob (normalised, joined to
// the escaped root), every directory glob() yields that no `exclude`
// glob matches (Pattern::matches_path with the default MatchOptions over
// the path relative to the root: `*` crosses `/`). A match without a
// pyproject.toml is skipped when it is hidden or holds no file at all; a
// member with `tool.uv.managed = false` is left out. Everything uv
// rejects (a match without pyproject.toml holding files, a member without
// [project], a member declaring its own workspace, two members of one
// name, an unreadable member pyproject.toml), any glob syntax PS-M2 does
// not model, and any match whose contents the scan did not read refuse
// the whole workspace.
func (r *ModuleResolver) workspaceMembers(root string) *workspaceInfo {
	w := r.ws
	if info, ok := w.workspaces[root]; ok {
		return info
	}
	info := &workspaceInfo{}
	w.workspaces[root] = info
	fail := func(reason string) *workspaceInfo {
		info.refused, info.members = reason, nil
		return info
	}
	doc := w.docs[root]
	ws, _ := tomlTable(doc, "tool", "uv", "workspace")
	for key := range ws {
		if key != "members" && key != "exclude" {
			return fail("[tool.uv.workspace] key " + key + " is not modelled")
		}
	}
	if managedFalse(doc) {
		return fail("the workspace root sets tool.uv.managed = false")
	}
	patterns := func(key string) ([]string, string) {
		raw, present := ws[key]
		if !present {
			return nil, ""
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, "[tool.uv.workspace] " + key + " is not an array of strings"
		}
		var out []string
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, "[tool.uv.workspace] " + key + " is not an array of strings"
			}
			if !globSyntaxModelled(s) {
				return nil, "workspace pattern " + s + " is not modelled"
			}
			out = append(out, s)
		}
		return out, ""
	}
	members, reason := patterns("members")
	if reason != "" {
		return fail(reason)
	}
	excludes, reason := patterns("exclude")
	if reason != "" {
		return fail(reason)
	}
	var excluded []string
	for _, e := range excludes {
		clean := path.Clean(e)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fail("exclude pattern " + e + " is not modelled")
		}
		excluded = append(excluded, clean)
	}
	isExcluded := func(d string) bool {
		rel := d
		if root != "." {
			if !strings.HasPrefix(d, root+"/") {
				return false
			}
			rel = strings.TrimPrefix(d, root+"/")
		}
		for _, e := range excluded {
			if globMatch(e, rel) {
				return true
			}
		}
		return false
	}
	set := map[string]bool{}
	if _, ok := doc["project"].(map[string]any); ok {
		set[root] = true
	}
	for _, m := range members {
		// uv joins the normalised glob to the escaped root: the root's own
		// characters are literal, so the walk starts at the root.
		rest := path.Clean(m)
		if rest == ".." || strings.HasPrefix(rest, "../") || strings.HasPrefix(rest, "/") {
			return fail("workspace pattern " + m + " leaves the workspace root (not modelled)")
		}
		matches, reason := r.globDirectories(root, rest)
		if reason != "" {
			return fail(reason)
		}
		for _, d := range matches {
			if d == root || set[d] || isExcluded(d) {
				continue
			}
			if _, unknown := r.isDir(d); unknown {
				return fail("workspace member " + d + " is a directory the scan skipped")
			}
			if _, scanned := r.pyprojects[d]; scanned {
				set[d] = true
				continue
			}
			if r.fileState(pyprojectPath(d)) != fileAbsent {
				return fail("workspace member " + d + " has a pyproject.toml Graphvein does not read")
			}
			if strings.HasPrefix(path.Base(d), ".") || r.noFiles(d) {
				continue // uv skips a hidden or empty match
			}
			return fail("workspace member " + d + " has no pyproject.toml (uv fails, or skips it when every file is gitignored)")
		}
	}
	names := map[string]string{}
	for _, m := range sortedKeys(set) {
		if m != root {
			if bad, ok := w.broken[m]; ok {
				return fail("workspace member " + m + ": pyproject.toml is not TOML this reader reads (" + bad + ")")
			}
			mdoc := w.docs[m]
			if managedFalse(mdoc) {
				continue
			}
			if _, nested := tomlTable(mdoc, "tool", "uv", "workspace"); nested {
				return fail("workspace member " + m + " declares its own workspace (uv: NestedWorkspace)")
			}
		}
		project, ok := tomlTable(w.docs[m], "project")
		if !ok {
			return fail("workspace member " + m + " has no [project] table")
		}
		name, _ := project["name"].(string)
		key := normalizeProjectName(name)
		if prev, dup := names[key]; dup {
			return fail("workspace members " + prev + " and " + m + " have the same name")
		}
		names[key] = m
		info.members = append(info.members, m)
	}
	return info
}

// pathSource reads one [tool.uv.sources] entry of the project in dir: the
// directory of an editable path source, "" for any other source kind
// (workspace, git, url, index: not a path in the repository), or a reason
// when a path source cannot be read exactly. A path source that is not
// editable is refused: a regular install copies only what the build
// includes, which PS-M2 does not model beyond the package names.
func pathSource(dir string, v any) (string, string) {
	t, ok := v.(map[string]any)
	if !ok {
		return "", ""
	}
	raw, isPath := t["path"]
	if !isPath {
		return "", ""
	}
	for key := range t {
		if key != "path" && key != "editable" {
			return "", "key " + key + " is not modelled"
		}
	}
	p, ok := raw.(string)
	if !ok {
		return "", "path is not a string"
	}
	if editable, _ := t["editable"].(bool); !editable {
		return "", "a path source that is not editable is not modelled"
	}
	if strings.HasSuffix(p, ".whl") || strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".zip") {
		return "", "an archive path is not modelled"
	}
	full, ok := relativeDir(dir, strings.TrimSuffix(p, "/"))
	if !ok {
		return "", "path " + p + " leaves the repository"
	}
	return full, ""
}

// hatchKeys are the options of [tool.hatch.build] and
// [tool.hatch.build.targets.wheel] PS-M2 reads; any other option there
// (sources, only-include, force-include, exclude, dev-mode-dirs,
// dev-mode-exact, hooks, ...) could change what the build exposes, so it
// refuses the member.
var hatchKeys = map[string]bool{"packages": true, "include": true}

// identifierName matches an ASCII Python identifier; a package name with
// other characters is refused (NFKC and PEP 3131 are not applied here).
var identifierName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// memberPackages reads the import packages the project in dir exposes
// when installed. A project uv does not package (tool.uv.package false,
// or unset without a [build-system] table: uv's PyProjectToml::is_package)
// is not installed and not refused.
func (r *ModuleResolver) memberPackages(dir string) memberLayout {
	if layout, ok := r.ws.packages[dir]; ok {
		return layout
	}
	layout := r.readMemberPackages(dir)
	r.ws.packages[dir] = layout
	return layout
}

func (r *ModuleResolver) readMemberPackages(dir string) memberLayout {
	refused := func(reason string) memberLayout { return memberLayout{reason: reason} }
	if bad, ok := r.ws.broken[dir]; ok {
		return refused("pyproject.toml is not TOML this reader reads (" + bad + ")")
	}
	doc := r.ws.docs[dir]
	build, hasBuild := tomlTable(doc, "build-system")
	uv, _ := tomlTable(doc, "tool", "uv")
	if v, set := uv["package"]; set {
		if v == false {
			return memberLayout{}
		}
		if v != true {
			return refused("tool.uv.package is not a boolean")
		}
	} else if !hasBuild {
		return memberLayout{}
	}
	if backend, _ := build["build-backend"].(string); backend != hatchlingBackend {
		return refused(fmt.Sprintf("build backend %q is not modelled", build["build-backend"]))
	}
	if r.fileState(path.Join(dir, "hatch.toml")) != fileAbsent {
		return refused("the project has a hatch.toml, which hatchling merges over [tool.hatch] (not modelled)")
	}
	project, ok := tomlTable(doc, "project")
	if !ok {
		return refused("no [project] table")
	}
	name, ok := project["name"].(string)
	if !ok || name == "" {
		return refused("project.name is not a string")
	}
	if dynamic, ok := project["dynamic"].([]any); ok {
		for _, d := range dynamic {
			if d == "name" {
				return refused("project.name is dynamic")
			}
		}
	}
	if !validProjectName(name) {
		return refused(fmt.Sprintf("project.name %q is not a valid project name, which hatchling refuses to build", name))
	}
	hatch := map[string]any{}
	wheel := map[string]any{}
	if t, ok := tomlTable(doc, "tool", "hatch", "build"); ok {
		for key, v := range t {
			if key == "targets" {
				continue
			}
			if !hatchKeys[key] {
				return refused("hatch build option " + key + " is not modelled")
			}
			hatch[key] = v
		}
	}
	if t, ok := tomlTable(doc, "tool", "hatch", "build", "targets", "wheel"); ok {
		for key, v := range t {
			if !hatchKeys[key] {
				return refused("hatch wheel option " + key + " is not modelled")
			}
			wheel[key] = v
		}
	}
	option := func(key string) (any, bool) {
		if v, ok := wheel[key]; ok {
			return v, true
		}
		v, ok := hatch[key]
		return v, ok
	}
	rawPackages, hasPackages := option("packages")
	rawInclude, hasInclude := option("include")
	result := func(packages map[string]string, reason string) memberLayout {
		return memberLayout{packages: packages, reason: reason, installed: reason == ""}
	}
	if !hasPackages {
		if hasInclude {
			return refused("hatch include without packages is not modelled")
		}
		return result(r.hatchDefault(dir, name))
	}
	entries, ok := stringList(rawPackages)
	if !ok || len(entries) == 0 {
		if list, isList := rawPackages.([]any); isList && len(list) == 0 && !hasInclude {
			return result(r.hatchDefault(dir, name))
		}
		return refused("hatch packages is not a non-empty array of strings")
	}
	out := map[string]string{}
	var prefixes []string
	for _, entry := range entries {
		clean := strings.TrimSuffix(strings.TrimPrefix(entry, "./"), "/")
		pkg, ok := relativeDir(dir, clean)
		if !ok || pkg == dir {
			return refused("hatch package " + entry + " is not a directory inside the project")
		}
		base := path.Base(pkg)
		if !identifierName.MatchString(base) {
			return refused("hatch package " + entry + " is not a Python identifier")
		}
		if _, dup := out[base]; dup {
			return refused("hatch packages name " + base + " twice")
		}
		if _, scanned := r.files[path.Join(pkg, "__init__.py")]; !scanned {
			return refused("hatch package " + entry + " has no scanned __init__.py (a namespace package or an unread file)")
		}
		out[base] = pkg
		prefixes = append(prefixes, clean+"/")
	}
	if hasInclude {
		patterns, ok := stringList(rawInclude)
		if !ok {
			return refused("hatch include is not an array of strings")
		}
		for _, pattern := range patterns {
			p := strings.TrimPrefix(strings.TrimPrefix(pattern, "/"), "./")
			inside := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(p, prefix) {
					inside = true
				}
			}
			if !inside {
				return refused("hatch include pattern " + pattern + " is outside the packages")
			}
		}
	}
	return result(out, "")
}

// hatchDefault applies hatchling's default file selection
// (WheelBuilderConfig.default_file_selection_options, hatchling 1.31.0):
// for the project name as written and as normalised (each with every run
// of characters other than word characters and `.` replaced by `_`), the
// first of `<name>/__init__.py` and `src/<name>/__init__.py` that exists
// is the package; `<name>.py` or one `*/<name>/__init__.py` would select
// a module or a namespace layout, which PS-M2 does not model (refused).
func (r *ModuleResolver) hatchDefault(dir, raw string) (map[string]string, string) {
	normalized := normalizeProjectName(raw)
	var names []string
	for _, n := range []string{fileNameComponent(raw), fileNameComponent(normalized)} {
		if !contains(names, n) {
			names = append(names, n)
		}
	}
	for _, n := range names {
		for _, pkg := range []string{path.Join(dir, n), path.Join(dir, "src", n)} {
			switch r.fileState(path.Join(pkg, "__init__.py")) {
			case fileScanned:
				if !identifierName.MatchString(n) {
					return nil, "hatchling selects " + pkg + ", not a Python identifier"
				}
				return map[string]string{n: pkg}, ""
			case fileUnread:
				return nil, "hatchling selects " + pkg + ", whose __init__.py Graphvein does not read"
			}
		}
		if r.fileState(path.Join(dir, n+".py")) != fileAbsent {
			return nil, "hatchling selects the module " + n + ".py (not modelled)"
		}
		for d := range r.directories {
			if path.Dir(path.Dir(d)) == dir && path.Base(d) == n && r.fileState(path.Join(d, "__init__.py")) != fileAbsent {
				return nil, "hatchling may select a namespace layout */" + n + " (not modelled)"
			}
		}
	}
	return nil, "hatchling's default file selection finds no package for " + raw
}

// validProjectName is hatchling 1.31.0's is_valid_project_name read over
// ASCII: one ASCII letter or digit, or several of ASCII letters, digits,
// `.`, `_` and `-` that begin and end with a letter or digit (PEP 508
// names). hatchling's case-insensitive match also accepts U+017F, U+0131,
// U+0130 and U+212A (their case folding meets an ASCII letter) and a
// trailing newline (`$`); those are refused here too (an over-refusal), so
// fileNameComponent only ever sees ASCII and reads no Unicode tables.
func validProjectName(name string) bool {
	alnum := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	if name == "" || !alnum(name[0]) || !alnum(name[len(name)-1]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !alnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// fileNameComponent is hatchling's normalize_file_name_component: every
// run of characters other than word characters and `.` becomes `_`. Its
// input is a name validProjectName accepted (or its PEP 503 form), so
// only ASCII letters and digits are word characters here; `-` is the one
// character replaced.
func fileNameComponent(s string) string {
	var b strings.Builder
	run := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' {
			b.WriteByte(c)
			run = false
			continue
		}
		if !run {
			b.WriteByte('_')
		}
		run = true
	}
	return b.String()
}

type fileState int

const (
	fileAbsent fileState = iota
	fileScanned
	fileUnread
)

// fileState reports whether a file exists as a scanned Python file, as an
// entry Graphvein does not read (a scanned file of another kind or a
// skipped entry), or not at all.
func (r *ModuleResolver) fileState(p string) fileState {
	if _, ok := r.files[p]; ok {
		return fileScanned
	}
	if contains(r.unread[path.Dir(p)], path.Base(p)) {
		return fileUnread
	}
	return fileAbsent
}
