package cargotoml

import (
	"fmt"
	"sort"
	"strings"
)

// LockedPackage is one [[package]] entry of a Cargo.lock.
type LockedPackage struct {
	Name, Version string
	// Source is the registry or git source; "" for a path or workspace
	// package.
	Source string
	// Dependencies are the names of the packages it depends on (the
	// first word of each "name [version [(source)]]" entry), in order.
	Dependencies []string
	// DependencyEntries are the same entries as written ("name",
	// "name version" or "name version (source)"): the version names the
	// locked package when the lockfile holds several of that name.
	DependencyEntries []string
}

// ReadLock reads every [[package]] entry of a Cargo.lock (format v1 to
// v4: the entries share the name, version, source and dependencies
// keys). A lockfile the TOML reader refuses, or a [[package]] value of
// another shape, is an error: which versions are locked is then unknown.
func ReadLock(src string) ([]LockedPackage, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, fmt.Errorf("Cargo.lock %w", err)
	}
	raw, present := root["package"]
	if !present {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("Cargo.lock: package is not an array of tables")
	}
	var out []LockedPackage
	for i, v := range list {
		t, ok := v.(Table)
		if !ok {
			return nil, fmt.Errorf("Cargo.lock: package %d is not a table", i+1)
		}
		name, okName := t["name"].(string)
		version, okVersion := t["version"].(string)
		if !okName || !okVersion {
			return nil, fmt.Errorf("Cargo.lock: package %d has no string name and version", i+1)
		}
		source, _ := t["source"].(string)
		var deps, entries []string
		if raw, present := t["dependencies"]; present {
			list, ok := raw.([]any)
			if !ok {
				return nil, fmt.Errorf("Cargo.lock: package %d dependencies is not an array", i+1)
			}
			for _, d := range list {
				s, ok := d.(string)
				fields := strings.Fields(s)
				if !ok || len(fields) == 0 {
					return nil, fmt.Errorf("Cargo.lock: package %d has a dependency that is not a package name", i+1)
				}
				deps = append(deps, fields[0])
				entries = append(entries, s)
			}
		}
		out = append(out, LockedPackage{Name: name, Version: version, Source: source, Dependencies: deps, DependencyEntries: entries})
	}
	return out, nil
}

// LockedVersions returns the distinct versions of the named package in
// the entries, sorted as strings.
func LockedVersions(packages []LockedPackage, name string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range packages {
		if p.Name == name && !seen[p.Version] {
			seen[p.Version] = true
			out = append(out, p.Version)
		}
	}
	sort.Strings(out)
	return out
}
