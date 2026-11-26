package diesel

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/extract/cargotoml"
)

// manifest is what Diesel discovery needs from one Cargo.toml.
type manifest struct {
	path string
	// problem is set when the manifest cannot be read; nothing else is
	// known then.
	problem   string
	pkg       bool
	workspace bool
	// name is [package] name ("" when absent or not a string).
	name string
	// workspacePath is [package] workspace, when written.
	workspacePath *string
	root          cargotoml.Table
}

func readManifest(file string, source []byte) manifest {
	m := manifest{path: file}
	if !utf8.Valid(source) {
		m.problem = "the manifest is not UTF-8 text"
		return m
	}
	t, err := cargotoml.Parse(string(source))
	if err != nil {
		m.problem = "Cargo.toml " + err.Error()
		return m
	}
	m.root = t
	switch p := t["package"].(type) {
	case nil:
	case cargotoml.Table:
		m.pkg = true
		m.name, _ = p["name"].(string)
		if w, present := p["workspace"]; present {
			s, ok := w.(string)
			if !ok {
				m.problem = "package.workspace is not a string"
				return m
			}
			m.workspacePath = &s
		}
	default:
		m.problem = "package is not a table"
		return m
	}
	switch t["workspace"].(type) {
	case nil:
	case cargotoml.Table:
		m.workspace = true
	default:
		m.problem = "workspace is not a table"
	}
	return m
}

// workspaceRoot follows Cargo's search (as internal/extract/schema does):
// [package] workspace when written, else the nearest directory, the
// package's own included, whose manifest has a [workspace] table. An
// unreadable manifest on the way stops the search: it may be the root.
func workspaceRoot(m manifest, manifests map[string]manifest) *manifest {
	dir := path.Dir(m.path)
	if m.workspacePath != nil {
		if root, ok := manifests[path.Join(dir, *m.workspacePath, "Cargo.toml")]; ok {
			return &root
		}
		return nil
	}
	for {
		p := path.Join(dir, "Cargo.toml")
		if root, ok := manifests[p]; ok && (root.workspace || root.problem != "" && p != m.path) {
			return &root
		}
		if dir == "." || dir == "" {
			return nil
		}
		dir = path.Dir(dir)
	}
}

// dieselEntry is one dependency entry that names the diesel crate (or,
// for the dialect rule, a companion crate).
type dieselEntry struct {
	// requirement is the version requirement ("" for a git or path
	// dependency); features the features the entry enables, inherited
	// ones included.
	requirement string
	features    []string
	// defaultFeatures is false when the entry (or the workspace entry it
	// inherits) sets default-features = false.
	defaultFeatures bool
	// key is the dependency key (a rename when package = "diesel").
	key string
}

// dependency is the result of reading a package's diesel entries.
type dependency struct {
	entries []dieselEntry
	// problem is set when an entry could not be read (whether it is
	// diesel, or its features, is unknown).
	problem string
}

// isCrate reports a dependency entry naming the crate: the key without a
// package rename, or any key with package = "<crate>".
func isCrate(key string, value any, crate string) bool {
	if t, ok := value.(cargotoml.Table); ok {
		if pkg, ok := t["package"].(string); ok {
			return pkg == crate
		}
	}
	return key == crate
}

// defaultFeaturesOff reports default-features = false (or Cargo's older
// default_features spelling) on an entry.
func defaultFeaturesOff(entry any) bool {
	t, ok := entry.(cargotoml.Table)
	return ok && (t["default-features"] == cargotoml.Scalar("false") || t["default_features"] == cargotoml.Scalar("false"))
}

// requirementOf returns an entry's version requirement.
func requirementOf(entry any) string {
	switch e := entry.(type) {
	case string:
		return e
	case cargotoml.Table:
		if v, ok := e["version"].(string); ok {
			return v
		}
	}
	return ""
}

// readDiesel finds the diesel entries of a package manifest, following
// workspace = true into the workspace root's [workspace.dependencies].
func readDiesel(m manifest, root *manifest) dependency {
	return readCrate(m, root, "diesel")
}

// readCrate finds the entries of a package manifest that name the crate,
// in every dependency table: build-dependencies too, since a build
// script's diesel features count in the workspace's dialect union
// (Revision 2, A5).
func readCrate(m manifest, root *manifest, crate string) dependency {
	var out dependency
	for _, deps := range cargotoml.AllDependencyTables(m.root) {
		for _, key := range sortedKeys(deps) {
			entry := deps[key]
			inherited := false
			if t, ok := entry.(cargotoml.Table); ok {
				inherited = t["workspace"] == cargotoml.Scalar("true")
			}
			if inherited {
				var rootEntry any
				if root != nil && root.problem == "" {
					if ws, ok := root.root["workspace"].(cargotoml.Table); ok {
						if wd, ok := ws["dependencies"].(cargotoml.Table); ok {
							rootEntry = wd[key]
						}
					}
				}
				if rootEntry == nil {
					switch {
					case root == nil && key == crate:
						out.problem = "the " + crate + " entry inherits from a workspace root that was not found"
					case root != nil && root.problem != "":
						out.problem = "the workspace root " + root.path + " could not be read"
					case key == crate:
						out.problem = "the " + crate + " entry inherits an entry the workspace root does not declare"
					}
					continue
				}
				if !isCrate(key, rootEntry, crate) {
					continue
				}
				e := dieselEntry{key: key, requirement: requirementOf(rootEntry), defaultFeatures: !defaultFeaturesOff(rootEntry)}
				for _, src := range []any{rootEntry, entry} {
					list, ok := cargotoml.Features(src)
					if !ok {
						out.problem = "a " + crate + " dependency entry's features could not be read"
					}
					e.features = append(e.features, list...)
				}
				out.entries = append(out.entries, e)
				continue
			}
			if !isCrate(key, entry, crate) {
				continue
			}
			list, ok := cargotoml.Features(entry)
			if !ok {
				out.problem = "a " + crate + " dependency entry's features could not be read"
			}
			out.entries = append(out.entries, dieselEntry{key: key, requirement: requirementOf(entry), features: list, defaultFeatures: !defaultFeaturesOff(entry)})
		}
	}
	return out
}

// backendFeatures maps diesel 2.3.14's backend features to dialects
// (diesel Cargo.toml [features], lines 107-145): each enables its
// backend; returning_clauses_for_sqlite_3_35 enables sqlite. diesel 2.4.0
// adds mariadb and mariadb_backend (the MariaDB backend, a dialect the
// contract does not read: dialectMariaDB) and sqlite-no-std (sqlite)
// (diesel 2.4.0 Cargo.toml [features]).
var backendFeatures = map[string]string{
	"postgres":                          "postgres",
	"postgres_backend":                  "postgres",
	"mysql":                             "mysql",
	"mysql_backend":                     "mysql",
	"sqlite":                            "sqlite",
	"returning_clauses_for_sqlite_3_35": "sqlite",
	"sqlite-no-std":                     "sqlite",
	"mariadb":                           dialectMariaDB,
	"mariadb_backend":                   dialectMariaDB,
}

// dialectMariaDB is the backend of diesel 2.4's mariadb features: it
// counts toward the workspace's backends (so a second backend makes the
// dialect unknown), and alone it makes the dialect unknown too, since
// diesel_cli 2.4 writes mariadb_type structs and no mapped rule covers it.
const dialectMariaDB = "mariadb"

// forwardedFeatures returns the diesel features a package's [features]
// table can enable on its diesel keys ("key/feature", "key?/feature");
// ok is false when the table is not a table of string arrays.
func forwardedFeatures(root cargotoml.Table, keys map[string]bool) ([]string, bool) {
	raw, present := root["features"]
	if !present {
		return nil, true
	}
	table, ok := raw.(cargotoml.Table)
	if !ok {
		return nil, false
	}
	var out []string
	for _, name := range sortedKeys(table) {
		list, ok := table[name].([]any)
		if !ok {
			return nil, false
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			dep, feature, ok := strings.Cut(s, "/")
			if ok && keys[strings.TrimSuffix(dep, "?")] {
				out = append(out, feature)
			}
		}
	}
	return out, true
}

// companion is a crate other than diesel whose features can enable a
// Diesel backend feature (Revision 1 addendum, OP2): Cargo unifies
// diesel's features over the build, so what such a crate turns on counts
// like a backend feature the workspace writes itself.
type companion struct {
	// versions are the published versions this feature map was read for.
	versions []string
	// always is the dialect the crate's own diesel dependency enables
	// whatever features are on ("" for none).
	always string
	// features maps each feature of the crate that enables a diesel
	// backend feature to that backend's dialect; defaults are its default
	// features.
	features map[string]string
	defaults []string
}

// companions are the Diesel companion crates whose feature maps were read
// (crates.io index, index.crates.io, 2026-11-15; the pinned sources of
// The investigation and DB2 for the crates.io corpus: diesel_migrations 2.3.2
// Cargo.toml:37-49, diesel-async 0.9.2 Cargo.toml [features] and
// :133-136, diesel_full_text_search 2.3.1 Cargo.toml:27-29). Only the
// versions listed here are known; a locked crate that depends on diesel
// at any other version, or any other crate that depends on diesel, makes
// the dialect unknown.
var companions = map[string][]companion{
	// mysql, postgres and sqlite are empty features, and the diesel
	// dependency has default-features = false and no features (2.4.0
	// adds only diesel/std).
	"diesel_migrations": {{versions: []string{"2.0.0-rc.0", "2.0.0-rc.1", "2.0.0", "2.1.0", "2.2.0", "2.3.0", "2.3.1", "2.3.2", "2.4.0"}}},
	// postgres = ["diesel/postgres_backend", ...], mysql =
	// ["diesel/mysql_backend", ...], since 0.5.0 sqlite = ["diesel/sqlite",
	// ...]; the diesel dependency enables no backend. 0.10.0 (diesel 2.4,
	// mariadb_backend) is not listed: its feature map was not read.
	"diesel-async": {
		{versions: []string{"0.1.0", "0.1.1", "0.2.0", "0.2.1", "0.2.2", "0.3.0", "0.3.1", "0.3.2", "0.4.0", "0.4.1"},
			features: map[string]string{"postgres": "postgres", "mysql": "mysql"}},
		{versions: []string{"0.5.0", "0.5.1", "0.5.2", "0.6.0", "0.6.1", "0.7.0", "0.7.1", "0.7.2", "0.7.3", "0.7.4", "0.8.0", "0.9.0", "0.9.1", "0.9.2"},
			features: map[string]string{"postgres": "postgres", "mysql": "mysql", "sqlite": "sqlite"}},
	},
	// The diesel dependency itself enables postgres (2.0.0) or
	// postgres_backend (2.1.0 on); the default with-diesel-postgres adds
	// diesel/postgres.
	"diesel_full_text_search": {
		{versions: []string{"2.0.0"}, always: "postgres"},
		{versions: []string{"2.1.0", "2.1.1", "2.2.0", "2.3.0", "2.3.1"}, always: "postgres",
			features: map[string]string{"with-diesel-postgres": "postgres"}, defaults: []string{"with-diesel-postgres"}},
	},
}

// knownCompanion returns the feature map of a companion crate's version.
func knownCompanion(name, version string) (companion, bool) {
	for _, c := range companions[name] {
		for _, v := range c.versions {
			if v == version {
				return c, true
			}
		}
	}
	return companion{}, false
}

// conditional reports features that enable a backend other than the one
// the crate always enables: only then do its enabled features matter.
func (c companion) conditional() bool {
	for _, d := range c.features {
		if d != c.always {
			return true
		}
	}
	return false
}

// lockInfo is a workspace's Cargo.lock as the pin and dialect rules read
// it.
type lockInfo struct {
	path     string
	packages []cargotoml.LockedPackage
	// problem says why the lockfile is not known ("" when read).
	problem string
}

func readLock(sources map[string][]byte, lockPath string) lockInfo {
	l := lockInfo{path: lockPath}
	src, ok := sources[lockPath]
	if !ok {
		l.problem = lockPath + " does not exist"
		return l
	}
	packages, err := cargotoml.ReadLock(string(src))
	if err != nil {
		l.problem = lockPath + " could not be read (" + err.Error() + ")"
		return l
	}
	l.packages = packages
	return l
}

// workspaceDialect applies the dialect rule (row DI-K1, Revision 1 O3 and
// the Revision 1 addendum OP2): Cargo unifies a crate's features over the
// build, so the Diesel backend features of every package of the
// workspace count, the package [features] that can forward one included,
// and so do the backends the known companion crates enable (their own
// diesel dependency, and the features the members' entries for them turn
// on). Exactly one backend gives the dialect. None, several, a manifest
// that cannot be read, a Cargo.lock that is missing or unreadable (the
// crates that may enable a backend are then not known), a locked crate
// outside the workspace that depends on diesel and is not a known
// companion version, or a companion whose enabling features a crate
// outside the workspace may turn on, give "unknown" with the reason: a
// wrong single dialect would type columns wrongly.
func workspaceDialect(members []manifest, unread []string, manifests map[string]manifest, lock lockInfo) (string, string) {
	if len(unread) > 0 {
		return "unknown", fmt.Sprintf("the workspace manifest %s could not be read, so the Diesel backend features are not known", unread[0])
	}
	dialects := map[string]bool{}
	addFeatures := func(features []string, table map[string]string) {
		for _, f := range features {
			if d := table[f]; d != "" {
				dialects[d] = true
			}
		}
	}
	// readEntries adds the backends one member enables through its
	// entries for a crate (diesel, or a companion with its feature map).
	readEntries := func(m manifest, crate string, table map[string]string, defaults []string) string {
		dep := readCrate(m, workspaceRoot(m, manifests), crate)
		if dep.problem != "" {
			return m.path + ": " + dep.problem
		}
		keys := map[string]bool{}
		for _, e := range dep.entries {
			keys[e.key] = true
			addFeatures(e.features, table)
			if e.defaultFeatures {
				addFeatures(defaults, table)
			}
		}
		if len(keys) == 0 {
			return ""
		}
		forwarded, ok := forwardedFeatures(m.root, keys)
		if !ok {
			return m.path + ": the package [features] table could not be read"
		}
		addFeatures(forwarded, table)
		return ""
	}
	isMember := map[string]bool{}
	for _, m := range members {
		isMember[m.name] = true
		if why := readEntries(m, "diesel", backendFeatures, nil); why != "" {
			return "unknown", why
		}
	}
	if lock.problem != "" {
		return "unknown", lock.problem + ", so the crates that may enable a Diesel backend are not known"
	}
	member := func(p cargotoml.LockedPackage) bool { return p.Source == "" && isMember[p.Name] }
	for _, p := range lock.packages {
		if p.Name == "diesel" || member(p) {
			continue
		}
		c, known := knownCompanion(p.Name, p.Version)
		if !known {
			for _, d := range p.Dependencies {
				if d == "diesel" {
					return "unknown", fmt.Sprintf("%s locks %s %s, which depends on diesel; the Diesel backend features it enables are not known", lock.path, p.Name, p.Version)
				}
			}
			continue
		}
		if c.always != "" {
			dialects[c.always] = true
		}
		if !c.conditional() {
			continue
		}
		if n := len(cargotoml.LockedVersions(lock.packages, p.Name)); n > 1 {
			return "unknown", fmt.Sprintf("%s locks %d versions of %s; the Diesel backend features each enables are not known", lock.path, n, p.Name)
		}
		for _, q := range lock.packages {
			if member(q) {
				continue
			}
			for _, d := range q.Dependencies {
				if d == p.Name {
					return "unknown", fmt.Sprintf("%s locks %s %s, which depends on %s; the Diesel backend features it turns on there are not known", lock.path, q.Name, q.Version, p.Name)
				}
			}
		}
		for _, m := range members {
			if why := readEntries(m, p.Name, c.features, c.defaults); why != "" {
				return "unknown", why
			}
		}
	}
	list := sortedKeys(dialects)
	switch len(list) {
	case 1:
		if list[0] == dialectMariaDB {
			return "unknown", "the workspace enables diesel's MariaDB backend (diesel 2.4), a dialect the claim contract does not read"
		}
		return list[0], ""
	case 0:
		return "unknown", "no Diesel backend feature (postgres, mysql, sqlite) is enabled in the workspace"
	}
	return "unknown", "several Diesel backends are enabled in the workspace (" + strings.Join(list, ", ") + ")"
}

// requirementMajor returns the major version a single-comparator
// requirement fixes ("2", "2.3", "^2.3", "~2.3.1", "=2.3.14"), or -1 for
// any other requirement (ranges, wildcards, several comparators).
func requirementMajor(req string) int {
	s := strings.TrimSpace(req)
	if strings.Contains(s, ",") {
		return -1
	}
	for _, op := range []string{"=", "^", "~"} {
		if strings.HasPrefix(s, op) {
			s = strings.TrimSpace(s[len(op):])
			break
		}
	}
	return versionMajor(s)
}

// versionMajor returns the major number of a version "M[.m[.p]][-pre]",
// or -1. A 0.x requirement counts as major 0.
func versionMajor(v string) int {
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// lockPin picks the diesel version of a workspace's Cargo.lock: the only
// locked diesel version, or the only one of the requirement's major when
// several are locked. "" when the lockfile is missing or unreadable or the
// choice is ambiguous; why explains it.
func lockPin(lock lockInfo, requirement string) (version, why string) {
	if lock.problem != "" {
		return "", lock.problem
	}
	versions := cargotoml.LockedVersions(lock.packages, "diesel")
	if len(versions) == 0 {
		return "", lock.path + " locks no diesel version"
	}
	if len(versions) == 1 {
		return versions[0], ""
	}
	major := requirementMajor(requirement)
	var match []string
	for _, v := range versions {
		if major >= 0 && versionMajor(v) == major {
			match = append(match, v)
		}
	}
	if len(match) == 1 {
		return match[0], ""
	}
	sort.Strings(versions)
	return "", lock.path + " locks several diesel versions (" + strings.Join(versions, ", ") + ")"
}
