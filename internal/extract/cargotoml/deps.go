package cargotoml

import "sort"

// DependencyTables lists the [dependencies]-like tables of a manifest:
// dependencies, dev-dependencies and both under every [target.*].
func DependencyTables(root Table) []Table {
	return dependencyTables(root, "dependencies", "dev-dependencies")
}

// AllDependencyTables lists every dependency table of a manifest:
// dependencies, dev-dependencies and build-dependencies, at the top level
// and under every [target.*]. A reader that unions the features a build
// may enable on a crate (Diesel's backend features, ADR 0062) needs the
// build-dependencies too.
func AllDependencyTables(root Table) []Table {
	return dependencyTables(root, "dependencies", "dev-dependencies", "build-dependencies")
}

func dependencyTables(root Table, names ...string) []Table {
	var out []Table
	for _, name := range names {
		if t, ok := root[name].(Table); ok {
			out = append(out, t)
		}
	}
	if targets, ok := root["target"].(Table); ok {
		keys := make([]string, 0, len(targets))
		for k := range targets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			target, ok := targets[k].(Table)
			if !ok {
				continue
			}
			for _, name := range names {
				if t, ok := target[name].(Table); ok {
					out = append(out, t)
				}
			}
		}
	}
	return out
}

// Features returns the string features of an entry; ok is false when the
// features value is not an array of strings.
func Features(entry any) ([]string, bool) {
	t, isTable := entry.(Table)
	if !isTable {
		_, isString := entry.(string)
		return nil, isString
	}
	raw, present := t["features"]
	if !present {
		return nil, true
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	var out []string
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}
