// Package lockfile reads the version of a dependency that a package's
// lockfile locks (pnpm-lock.yaml, package-lock.json, yarn.lock) or that a
// Go module's go.mod requires (gomod.go). The model front ends (Prisma,
// Drizzle, Sequelize, GORM) state their pins (assumption "the locked
// versions created the database") from it, so they share one reader: a fix
// here (for example reading every YAML document of a pnpm lockfile) reaches
// every front end at once.
package lockfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Lockfiles read for pins, in the order they are reported.
var lockfileNames = []string{"pnpm-lock.yaml", "package-lock.json", "yarn.lock"}

var leadingMajor = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.|$)`)

var exactVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// Locked returns the version of dependency name that the lockfile
// governing package directory dir (repository-relative) locks for that
// package, with the lockfile's path. The governing lockfile is the one
// beside the package, else the nearest one above it (a workspace root,
// T018g1); when several lockfiles sit in that directory they must
// agree. spec is the package.json range, which yarn.lock keys by. The
// version is empty when it is missing, not an exact version, or ambiguous;
// why says which.
func Locked(sources map[string][]byte, dir, name, spec string) (version, lockfile, why string) {
	for d := dir; ; d = path.Dir(d) {
		var found []string
		var versions []string
		for _, lf := range lockfileNames {
			p := path.Join(d, lf)
			data, ok := sources[p]
			if !ok {
				continue
			}
			found = append(found, p)
			rel := relDir(d, dir)
			var v string
			switch lf {
			case "pnpm-lock.yaml":
				v = Pnpm(data, rel, name)
			case "package-lock.json":
				v = npmVersion(data, rel, name)
			case "yarn.lock":
				v = yarnVersion(data, name, spec)
			}
			versions = append(versions, v)
		}
		if len(found) > 0 {
			lockfile = strings.Join(found, ", ")
			for _, v := range versions {
				if v == "" || !exactVersion.MatchString(v) {
					return "", lockfile, name + " has no exact locked version in " + lockfile
				}
				if v != versions[0] {
					return "", lockfile, "the lockfiles in " + d + " lock different versions of " + name
				}
			}
			return versions[0], lockfile, ""
		}
		if d == "." || d == "/" || d == "" {
			return "", "", "no lockfile beside the package or above it"
		}
	}
}

// relDir is dir relative to base, both repository-relative; "." when equal.
func relDir(base, dir string) string {
	if base == "." {
		return dir
	}
	if dir == base {
		return "."
	}
	return strings.TrimPrefix(dir, base+"/")
}

// pnpmDeps is one pnpm importer's dependency lists.
type pnpmDeps struct {
	Dependencies    map[string]yaml.Node `yaml:"dependencies"`
	DevDependencies map[string]yaml.Node `yaml:"devDependencies"`
}

// pnpmLock is one YAML document of a pnpm-lock.yaml.
type pnpmLock struct {
	Dependencies    map[string]yaml.Node `yaml:"dependencies"`
	DevDependencies map[string]yaml.Node `yaml:"devDependencies"`
	Importers       map[string]pnpmDeps  `yaml:"importers"`
}

// Pnpm reads a pnpm-lock.yaml. Recent pnpm versions write two YAML
// documents (an environment document holding packageManagerDependencies,
// then the lockfile; kaneo's pin), so every document is read and the
// documents that lock the dependency must agree (kaneo, kftray). It is
// exported for the front ends' tests.
func Pnpm(data []byte, importer, name string) string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	version := ""
	for {
		var lock pnpmLock
		if err := dec.Decode(&lock); err != nil {
			if errors.Is(err, io.EOF) {
				return version
			}
			return ""
		}
		v := pnpmDocVersion(lock, importer, name)
		if v == "" {
			continue
		}
		if version != "" && version != v {
			return ""
		}
		version = v
	}
}

// pnpmDocVersion reads one lockfile document: the importer of the package
// (key "." for the lockfile's own directory), or for a single-project
// lockfile without importers its top-level dependency lists. Lockfile 6
// and 9 entries are {specifier, version}; lockfile 5 entries are the
// version string. The version loses pnpm's peer suffix
// ("7.10.0(react@19...)", "7.10.0_typescript@5").
func pnpmDocVersion(lock pnpmLock, importer, name string) string {
	d := pnpmDeps{Dependencies: lock.Dependencies, DevDependencies: lock.DevDependencies}
	if lock.Importers != nil {
		var ok bool
		if d, ok = lock.Importers[importer]; !ok {
			return ""
		}
	} else if importer != "." {
		return ""
	}
	var node yaml.Node
	found := false
	for _, m := range []map[string]yaml.Node{d.Dependencies, d.DevDependencies} {
		if n, ok := m[name]; ok {
			if found {
				return ""
			}
			node, found = n, true
		}
	}
	if !found {
		return ""
	}
	var v string
	switch node.Kind {
	case yaml.ScalarNode:
		v = node.Value
	case yaml.MappingNode:
		var entry struct {
			Version string `yaml:"version"`
		}
		if node.Decode(&entry) != nil {
			return ""
		}
		v = entry.Version
	}
	if i := strings.IndexAny(v, "(_"); i > 0 {
		v = v[:i]
	}
	return v
}

// npmVersion reads a package-lock.json. Lockfile 2 and 3 list installed
// packages by path; the package's dependency is found as Node resolves it
// from the package directory: <dir>/node_modules/<name>, then each parent
// directory's node_modules up to the lockfile's. Lockfile 1 lists only the
// root project's dependencies.
func npmVersion(data []byte, rel, name string) string {
	var lock struct {
		LockfileVersion int
		Packages        map[string]struct{ Version string }
		Dependencies    map[string]struct{ Version string }
	}
	if json.Unmarshal(data, &lock) != nil {
		return ""
	}
	if lock.LockfileVersion <= 1 {
		if rel != "." {
			return ""
		}
		return lock.Dependencies[name].Version
	}
	for d := rel; ; d = path.Dir(d) {
		key := path.Join(d, "node_modules", name)
		if d == "." {
			key = path.Join("node_modules", name)
		}
		if p, ok := lock.Packages[key]; ok {
			return p.Version
		}
		if d == "." {
			return ""
		}
	}
}

// yarnVersion reads a yarn.lock (classic or Berry): the entry whose key
// list holds name@spec (Berry: name@npm:spec) gives the version. Several
// entries with different versions give none.
func yarnVersion(data []byte, name, spec string) string {
	if spec == "" {
		return ""
	}
	want := map[string]bool{name + "@" + spec: true, name + "@npm:" + spec: true}
	version, match := "", false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line[0] != ' ' && strings.HasSuffix(line, ":") {
			match = false
			keys := strings.TrimSuffix(line, ":")
			// Berry quotes a key list as one string ("a@npm:^1, a@npm:~1");
			// classic quotes each key.
			if unq, err := strconv.Unquote(keys); err == nil {
				keys = unq
			}
			for _, key := range strings.Split(keys, ",") {
				key = strings.TrimSpace(key)
				if unq, err := strconv.Unquote(key); err == nil {
					key = unq
				}
				if want[key] {
					match = true
				}
			}
			continue
		}
		if !match {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "version ") && !strings.HasPrefix(trimmed, "version:") || strings.HasPrefix(line, "    ") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "version"), ":"))
		if unq, err := strconv.Unquote(v); err == nil {
			v = unq
		}
		if version != "" && version != v {
			return ""
		}
		version = v
	}
	return version
}

// Major returns the major version of an exact version or of a
// package.json range whose major is fixed (^7.1.0, ~7.1, 7.x, 7), else -1.
func Major(v string) int {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "^~=v")
	m := leadingMajor.FindStringSubmatch(v)
	if m == nil || strings.ContainsAny(v, " |<>*") {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// AtLeast reports whether exact version v is at least major.minor.
func AtLeast(v string, major, minor int) bool {
	m := exactVersion.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a > major || a == major && b >= minor
}
