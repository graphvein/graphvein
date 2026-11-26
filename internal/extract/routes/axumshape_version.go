package routes

import (
	"path"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/cargotoml"
)

// This file reads the axum version a crate builds with (forms `version:*`
// of rows SH-X11 and SH-X25 of
// docs/contract/claims/route-shape-axum.md): some answers differ between
// the axum versions the contract covers (the charset of text responses,
// whether HeaderMap takes the headers), and those forms are claimed only
// when the version is proven.

// semver is a version without pre-release or build metadata.
type semver [3]int

func (a semver) less(b semver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// versionRange is the set of axum versions a crate may build with,
// [lo, hi). known is false when it is not proven; why says why.
type versionRange struct {
	known  bool
	lo, hi semver
	// source names what proved it (Cargo.lock or Cargo.toml) or why not.
	why string
}

// atLeast reports whether every version in the range is v or later.
func (r versionRange) atLeast(v semver) bool { return r.known && !r.lo.less(v) }

// below reports whether every version in the range is before v.
func (r versionRange) below(v semver) bool { return r.known && !v.less(r.hi) }

var (
	axum04 = semver{0, 4, 0}
	axum05 = semver{0, 5, 0}
)

// parseVersion reads "1", "1.2" or "1.2.3" (no pre-release); n is the
// number of parts written.
func parseVersion(s string) (semver, int, bool) {
	var v semver
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, 0, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || len(p) > 1 && p[0] == '0' {
			return v, 0, false
		}
		v[i] = n
	}
	return v, len(parts), true
}

// exactRange is the range of one locked or published version.
func exactRange(version, why string) versionRange {
	v, n, ok := parseVersion(version)
	if !ok || n != 3 {
		return versionRange{why: "axum version " + version + " is not read"}
	}
	hi := v
	hi[2]++
	return versionRange{known: true, lo: v, hi: hi, why: why}
}

// requirementRange reads a Cargo version requirement of one comparator:
// a bare or caret requirement ("0.8", "^0.8.8"), an exact one ("=0.8.8")
// or a tilde one ("~0.8"). Anything else (several comparators, `*`,
// `>=`, a pre-release) is not proven.
func requirementRange(req, why string) versionRange {
	req = strings.TrimSpace(req)
	op := ""
	for _, p := range []string{"=", "^", "~"} {
		if strings.HasPrefix(req, p) {
			op, req = p, strings.TrimSpace(req[1:])
			break
		}
	}
	v, n, ok := parseVersion(req)
	if !ok {
		return versionRange{why: "the axum requirement " + req + " is not read"}
	}
	hi := v
	switch {
	case op == "=":
		if n != 3 {
			return versionRange{why: "the axum requirement =" + req + " is not read"}
		}
		hi[2]++
	case op == "~":
		if n == 1 {
			hi = semver{v[0] + 1, 0, 0}
		} else {
			hi = semver{v[0], v[1] + 1, 0}
		}
	default:
		switch {
		case v[0] > 0 || n == 1:
			hi = semver{v[0] + 1, 0, 0}
		case v[1] > 0 || n == 2:
			hi = semver{0, v[1] + 1, 0}
		default:
			hi = semver{0, 0, v[2] + 1}
		}
	}
	return versionRange{known: true, lo: v, hi: hi, why: why}
}

// axumVersions reads and caches the axum version of each crate.
type axumVersions struct {
	sources map[string][]byte
	cache   map[string]versionRange
}

// of returns the axum version range of the crate holding file.
func (a *axumVersions) of(file string) versionRange {
	if a == nil || a.sources == nil {
		return versionRange{why: "the crate manifest was not read"}
	}
	manifest := ""
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		p := path.Join(dir, "Cargo.toml")
		if _, ok := a.sources[p]; ok {
			manifest = p
			break
		}
		if dir == "." || dir == "/" || dir == "" {
			break
		}
	}
	if manifest == "" {
		return versionRange{why: "no Cargo.toml holds " + file}
	}
	if r, ok := a.cache[manifest]; ok {
		return r
	}
	r := a.read(manifest)
	a.cache[manifest] = r
	return r
}

func (a *axumVersions) parse(p string) (cargotoml.Table, bool) {
	src, ok := a.sources[p]
	if !ok {
		return nil, false
	}
	t, err := cargotoml.Parse(string(src))
	return t, err == nil
}

// read proves the axum version of the crate whose manifest is at p: from
// the nearest Cargo.lock when there is one (the lock wins), else from
// the manifest's requirement (with workspace inheritance and path
// dependencies).
func (a *axumVersions) read(p string) versionRange {
	root, ok := a.parse(p)
	if !ok {
		return versionRange{why: p + " could not be read"}
	}
	pkg, ok := root["package"].(cargotoml.Table)
	if !ok {
		return versionRange{why: p + " has no [package]"}
	}
	name, _ := pkg["name"].(string)
	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		lock := path.Join(dir, "Cargo.lock")
		if _, ok := a.sources[lock]; ok {
			return a.locked(lock, name)
		}
		if dir == "." || dir == "/" || dir == "" {
			break
		}
	}
	var found []versionRange
	for _, deps := range cargotoml.DependencyTables(root) {
		for key, entry := range deps {
			if !namesAxum(key, entry) {
				continue
			}
			found = append(found, a.entry(p, key, entry))
		}
	}
	if len(found) == 0 {
		return versionRange{why: p + " declares no axum dependency"}
	}
	for _, f := range found[1:] {
		if f != found[0] {
			return versionRange{why: p + " declares axum more than once"}
		}
	}
	return found[0]
}

// namesAxum reports whether a dependency entry is the axum crate.
func namesAxum(key string, entry any) bool {
	if t, ok := entry.(cargotoml.Table); ok {
		if pkg, ok := t["package"].(string); ok {
			return pkg == "axum"
		}
	}
	return key == "axum"
}

// locked reads the version of axum the lockfile resolves for the local
// package name: the "axum [version]" entry of its dependencies.
func (a *axumVersions) locked(lock, name string) versionRange {
	packages, err := cargotoml.ReadLock(string(a.sources[lock]))
	if err != nil {
		return versionRange{why: lock + " could not be read"}
	}
	var own []cargotoml.LockedPackage
	for _, p := range packages {
		if p.Name == name && p.Source == "" {
			own = append(own, p)
		}
	}
	if len(own) != 1 {
		return versionRange{why: lock + " does not lock one package " + name}
	}
	var entries []string
	for _, e := range own[0].DependencyEntries {
		if f := strings.Fields(e); f[0] == "axum" {
			entries = append(entries, e)
		}
	}
	if len(entries) != 1 {
		return versionRange{why: lock + " does not lock one axum dependency of " + name}
	}
	if f := strings.Fields(entries[0]); len(f) >= 2 {
		return exactRange(f[1], "axum "+f[1]+" ("+lock+")")
	}
	versions := cargotoml.LockedVersions(packages, "axum")
	if len(versions) != 1 {
		return versionRange{why: lock + " locks several axum versions"}
	}
	return exactRange(versions[0], "axum "+versions[0]+" ("+lock+")")
}

// entry reads one dependency entry of the manifest at p.
func (a *axumVersions) entry(p, key string, entry any) versionRange {
	switch e := entry.(type) {
	case string:
		return requirementRange(e, "axum "+e+" ("+p+")")
	case cargotoml.Table:
		if e["workspace"] == cargotoml.Scalar("true") {
			ws := a.workspaceRoot(p)
			if ws == nil {
				return versionRange{why: p + " inherits axum from a workspace root that was not found"}
			}
			w, _ := ws["workspace"].(cargotoml.Table)
			deps, _ := w["dependencies"].(cargotoml.Table)
			inherited, ok := deps[key]
			if !ok {
				return versionRange{why: "the workspace root of " + p + " does not declare " + key}
			}
			if t, ok := inherited.(cargotoml.Table); ok && t["workspace"] != nil {
				return versionRange{why: "the workspace entry for " + key + " is not read"}
			}
			return a.entry(a.workspacePath(p), key, inherited)
		}
		if v, ok := e["version"].(string); ok {
			return requirementRange(v, "axum "+v+" ("+p+")")
		}
		if dep, ok := e["path"].(string); ok {
			return a.pathVersion(path.Join(path.Dir(p), dep, "Cargo.toml"))
		}
	}
	return versionRange{why: "the axum entry of " + p + " has no version"}
}

// pathVersion reads the published version of a path dependency.
func (a *axumVersions) pathVersion(p string) versionRange {
	p = path.Clean(p)
	root, ok := a.parse(p)
	if !ok {
		return versionRange{why: p + " could not be read"}
	}
	pkg, _ := root["package"].(cargotoml.Table)
	switch v := pkg["version"].(type) {
	case string:
		return exactRange(v, "axum "+v+" ("+p+")")
	case cargotoml.Table:
		if v["workspace"] == cargotoml.Scalar("true") {
			if ws := a.workspaceRoot(p); ws != nil {
				w, _ := ws["workspace"].(cargotoml.Table)
				wp, _ := w["package"].(cargotoml.Table)
				if s, ok := wp["version"].(string); ok {
					return exactRange(s, "axum "+s+" ("+a.workspacePath(p)+")")
				}
			}
		}
	}
	return versionRange{why: p + " has no version that is read"}
}

// workspacePath is the manifest of the workspace root of the manifest at
// p: the nearest one up with a [workspace] table, p's own included.
func (a *axumVersions) workspacePath(p string) string {
	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		m := path.Join(dir, "Cargo.toml")
		if t, ok := a.parse(m); ok {
			if _, ok := t["workspace"].(cargotoml.Table); ok {
				return m
			}
		}
		if dir == "." || dir == "/" || dir == "" {
			return ""
		}
	}
}

func (a *axumVersions) workspaceRoot(p string) cargotoml.Table {
	m := a.workspacePath(p)
	if m == "" {
		return nil
	}
	t, _ := a.parse(m)
	return t
}
