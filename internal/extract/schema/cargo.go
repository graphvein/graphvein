package schema

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/extract/cargotoml"
)

// tomlTable and scalar are the shared TOML reader's values
// (internal/extract/cargotoml); the aliases keep this package's code as it
// was before the reader moved (T018g4 DB1).
type (
	tomlTable = cargotoml.Table
	scalar    = cargotoml.Scalar
)

// parseTOML reads a Cargo manifest with the shared reader; an error names
// the file kind and line ("Cargo.toml line 9: expected value"), as the
// discovery diagnostics always have.
func parseTOML(src string) (tomlTable, error) {
	t, err := cargotoml.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("Cargo.toml %w", err)
	}
	return t, nil
}

// sqlxDialects maps sqlx feature names to dialects (sqlx Cargo.toml
// [features] at v0.5.9 and v0.8.6). all and all-databases enable several.
var sqlxDialects = map[string][]string{
	"postgres":         {"postgres"},
	"mysql":            {"mysql"},
	"sqlite":           {"sqlite"},
	"sqlite-unbundled": {"sqlite"},
	"mssql":            {"mssql"},
	"all-databases":    {"mysql", "sqlite", "postgres", "mssql"},
	"all":              {"mysql", "sqlite", "postgres", "mssql"},
}

// manifest is what discovery needs from one Cargo.toml.
type manifest struct {
	path string
	// readable is false when parseTOML failed or a table discovery reads
	// has another type (manifestShape); nothing else is known then, and
	// problem holds the reason.
	readable  bool
	problem   string
	pkg       bool
	workspace bool
	// workspacePath is [package] workspace, when written.
	workspacePath *string
	root          tomlTable
}

func readCargo(file string, source []byte) manifest {
	m := manifest{path: file}
	if !utf8.Valid(source) {
		// Cargo reads a manifest as UTF-8 and refuses it otherwise (P10).
		m.problem = "the manifest is not UTF-8 text"
		return m
	}
	t, err := parseTOML(string(source))
	if err != nil {
		m.problem = err.Error()
		return m
	}
	if problem := manifestShape(t); problem != "" {
		m.problem = problem
		return m
	}
	m.readable, m.root = true, t
	if pkg, ok := t["package"].(tomlTable); ok {
		m.pkg = true
		if w, ok := pkg["workspace"].(string); ok {
			m.workspacePath = &w
		}
	}
	_, m.workspace = t["workspace"].(tomlTable)
	return m
}

// manifestShape checks the types of every value discovery reads (T018b1
// review: no silent drops). A [package], [workspace],
// [workspace.dependencies], [dependencies], [dev-dependencies], [target] or
// [target.X] (with its dependency tables) that is not a table, an array of
// [[package]] tables included, or a [package] workspace that is not a
// string, would otherwise be skipped as "no package" or "no sqlx" without a
// trace. Cargo refuses such a manifest; whether it was meant to declare a
// sqlx set is unknown, so it is unreadable. It returns the problem, or "".
func manifestShape(t tomlTable) string {
	notTable := func(name string, v any) string {
		if _, ok := v.(tomlTable); ok || v == nil {
			return ""
		}
		return name + " is not a table"
	}
	if p := notTable("package", t["package"]); p != "" {
		return p
	}
	if pkg, ok := t["package"].(tomlTable); ok {
		if w, present := pkg["workspace"]; present {
			if _, ok := w.(string); !ok {
				return "package.workspace is not a string"
			}
		}
	}
	if p := notTable("workspace", t["workspace"]); p != "" {
		return p
	}
	if ws, ok := t["workspace"].(tomlTable); ok {
		if p := notTable("workspace.dependencies", ws["dependencies"]); p != "" {
			return p
		}
	}
	for _, name := range []string{"dependencies", "dev-dependencies"} {
		if p := notTable(name, t[name]); p != "" {
			return p
		}
	}
	if p := notTable("target", t["target"]); p != "" {
		return p
	}
	targets, _ := t["target"].(tomlTable)
	keys := make([]string, 0, len(targets))
	for k := range targets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := notTable("target."+k, targets[k]); p != "" {
			return p
		}
		target, _ := targets[k].(tomlTable)
		for _, name := range []string{"dependencies", "dev-dependencies"} {
			if p := notTable("target."+k+"."+name, target[name]); p != "" {
				return p
			}
		}
	}
	return ""
}

// sqlxDependency is the result of reading a package's sqlx entries.
type sqlxDependency struct {
	found bool
	// dialect is postgres, mysql, sqlite, mssql or unknown.
	dialect string
	// reason explains an unknown dialect.
	reason string
	// rootUnread names the workspace root an inherited dependency entry
	// comes from when that root could not be read or is missing: whether
	// the entry is sqlx, and with which features, is unknown (P10).
	rootUnread string
	// marker is whether the sqlx versions the entries allow read
	// `-- no-transaction` (C4, sqlxversion.go).
	marker markerReading
}

// dependencyTables lists the [dependencies]-like tables of a manifest
// (cargotoml.DependencyTables).
func dependencyTables(root tomlTable) []tomlTable { return cargotoml.DependencyTables(root) }

// isSqlx reports whether a dependency entry names the sqlx crate: the key
// sqlx without a package rename, or any key with package = "sqlx".
func isSqlx(key string, value any) bool {
	if t, ok := value.(tomlTable); ok {
		if pkg, ok := t["package"].(string); ok {
			return pkg == "sqlx"
		}
	}
	return key == "sqlx"
}

// features returns the string features of an entry (cargotoml.Features).
func features(entry any) ([]string, bool) { return cargotoml.Features(entry) }

// readSqlx finds the sqlx dependency of a package manifest and its dialect
// from the declared features, following workspace = true into the
// workspace root's [workspace.dependencies]. Feature unification across
// the build graph is not modelled (ADR 0047 known gaps).
func readSqlx(m manifest, workspaceRoot *manifest) sqlxDependency {
	var out sqlxDependency
	dialects := map[string]bool{}
	unreadable := false
	// keys are the dependency keys that name sqlx; [features] entries
	// "key/feature" or "key?/feature" forward to them.
	keys := map[string]bool{}
	var readings []markerReading
	for _, deps := range dependencyTables(m.root) {
		for key, entry := range deps {
			inherited := false
			if t, ok := entry.(tomlTable); ok {
				if w, ok := t["workspace"]; ok {
					inherited = w == scalar("true")
				}
			}
			if inherited {
				var rootEntry any
				if workspaceRoot != nil && workspaceRoot.readable {
					if ws, ok := workspaceRoot.root["workspace"].(tomlTable); ok {
						if wd, ok := ws["dependencies"].(tomlTable); ok {
							rootEntry = wd[key]
						}
					}
				}
				if rootEntry == nil {
					switch {
					case workspaceRoot == nil:
						// P10: any key may be a renamed sqlx.
						out.found, out.rootUnread = true, "no workspace root was found"
					case !workspaceRoot.readable:
						out.found, out.rootUnread = true, "the workspace root "+workspaceRoot.path+" could not be read"
					case key == "sqlx":
						out.found = true
						unreadable = true
					}
					continue
				}
				if !isSqlx(key, rootEntry) {
					continue
				}
				out.found, keys[key] = true, true
				// The version comes from the root entry (an inheriting
				// entry may only add features and optional).
				readings = append(readings, requirementReading(entryVersion(rootEntry)))
				for _, e := range []any{rootEntry, entry} {
					list, ok := features(e)
					if !ok {
						unreadable = true
					}
					addDialects(dialects, list)
				}
				continue
			}
			if !isSqlx(key, entry) {
				continue
			}
			out.found, keys[key] = true, true
			readings = append(readings, requirementReading(entryVersion(entry)))
			list, ok := features(entry)
			if !ok {
				unreadable = true
			}
			addDialects(dialects, list)
		}
	}
	// Revision 3 G7: a package feature such as lite = ["sqlx/sqlite"] may
	// switch on another dialect when the package is built with it; the
	// dialect is then not certain.
	forwarded, readable := forwardedDialects(m.root, keys)
	extra := ""
	for _, d := range forwarded {
		if !dialects[d] && extra == "" {
			extra = d
		}
	}
	switch {
	case unreadable:
		out.dialect, out.reason = "unknown", "a sqlx dependency entry could not be read"
	case out.found && !readable:
		out.dialect, out.reason = "unknown", "the package [features] table could not be read"
	case extra != "":
		out.dialect, out.reason = "unknown", fmt.Sprintf("a package feature can enable the sqlx %s feature", extra)
	case len(dialects) == 1:
		for d := range dialects {
			out.dialect = d
		}
	case len(dialects) == 0:
		out.dialect, out.reason = "unknown", "no database feature is declared"
	default:
		out.dialect, out.reason = "unknown", "several database features are declared"
	}
	out.marker = combineReadings(readings)
	return out
}

// forwardedDialects returns the dialects that the package's [features]
// can enable on its sqlx dependency (entries "key/feature" and
// "key?/feature" for a sqlx key), sorted; readable is false when the table
// is not a table of string arrays.
func forwardedDialects(root tomlTable, keys map[string]bool) ([]string, bool) {
	raw, present := root["features"]
	if !present {
		return nil, true
	}
	table, ok := raw.(tomlTable)
	if !ok {
		return nil, false
	}
	found := map[string]bool{}
	for _, v := range table {
		list, ok := v.([]any)
		if !ok {
			return nil, false
		}
		for _, item := range list {
			entry, ok := item.(string)
			if !ok {
				return nil, false
			}
			dep, feature, ok := strings.Cut(entry, "/")
			if !ok || !keys[strings.TrimSuffix(dep, "?")] {
				continue
			}
			addDialects(found, []string{feature})
		}
	}
	out := make([]string, 0, len(found))
	for d := range found {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, true
}

func addDialects(dialects map[string]bool, list []string) {
	for _, f := range list {
		for _, d := range sqlxDialects[f] {
			dialects[d] = true
		}
	}
}

// findWorkspaceRoot follows Cargo's search: [package] workspace when
// written, else the nearest directory (the package's own included) whose
// manifest has a [workspace] table.
func findWorkspaceRoot(m manifest, manifests map[string]manifest) *manifest {
	dir := path.Dir(m.path)
	if m.workspacePath != nil {
		p := path.Join(dir, *m.workspacePath, "Cargo.toml")
		if root, ok := manifests[p]; ok {
			return &root
		}
		return nil
	}
	for {
		p := path.Join(dir, "Cargo.toml")
		if root, ok := manifests[p]; ok && (root.workspace || !root.readable && p != m.path) {
			// An unreadable manifest may be the root: the search never
			// climbs past it (P10).
			return &root
		}
		if dir == "." || dir == "" {
			return nil
		}
		dir = path.Dir(dir)
	}
}
