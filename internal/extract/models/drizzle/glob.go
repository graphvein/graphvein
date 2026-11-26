package drizzle

import (
	"path"
	"sort"
	"strings"
)

// drizzle-kit resolves a config's schema setting in prepareFilenames
// (drizzle-kit 0.31.11 bin.cjs:19713-19745): each string is a glob (glob
// 8.1.0, bundled at bin.cjs:9573) matched against the working directory,
// with no base, not against the config file's directory. A
// matched file is read; a matched directory contributes only the files
// directly in it (readdirSync, no recursion). Graphvein supports literal
// paths, `*`, `?` and `**`, with glob's default `dot: false` (a wildcard
// never matches a name that starts with a dot); any other glob syntax
// makes the config unproven.

// schemaExtensions are the extensions prepareFilenames accepts
// (bin.cjs:19728); other matched files are reported and not read.
var schemaExtensions = []string{".ts", ".js", ".cjs", ".mjs", ".mts", ".cts"}

func schemaExtension(file string) bool {
	for _, e := range schemaExtensions {
		if strings.HasSuffix(file, e) {
			return true
		}
	}
	return false
}

// globError is why a schema pattern cannot be resolved; "" when it can.
func globError(pattern string) string {
	switch {
	case pattern == "":
		return "an empty schema path"
	case strings.HasPrefix(pattern, "/") || strings.Contains(pattern, `\`) || len(pattern) > 1 && pattern[1] == ':':
		return "schema path " + quote(pattern) + " cannot be placed in the repository"
	case strings.ContainsAny(pattern, "[]{}"):
		return "schema path " + quote(pattern) + " uses glob syntax Graphvein does not support (classes or braces)"
	}
	for _, seg := range strings.Split(pattern, "/") {
		if strings.HasPrefix(seg, "!") || strings.HasPrefix(seg, "#") {
			return "schema path " + quote(pattern) + " uses glob syntax Graphvein does not support (negation or comment)"
		}
	}
	for _, ext := range []string{"@(", "+(", "*(", "?(", "!("} {
		if strings.Contains(pattern, ext) {
			return "schema path " + quote(pattern) + " uses glob syntax Graphvein does not support (extglob)"
		}
	}
	// A `..` after a wildcard cannot be cleaned without changing what
	// the glob matches.
	magic := false
	for _, seg := range strings.Split(pattern, "/") {
		if seg == ".." && magic {
			return "schema path " + quote(pattern) + " has `..` after a wildcard"
		}
		magic = magic || hasMagic(seg)
	}
	return ""
}

func hasMagic(seg string) bool { return strings.ContainsAny(seg, "*?") }

func quote(s string) string { return "\"" + s + "\"" }

// globForm says which DR-K1 schema form a pattern is: "glob" when it has a
// wildcard, else "path".
func globForm(pattern string) string {
	if hasMagic(pattern) {
		return "glob"
	}
	return "path"
}

// resolveSchema returns the files prepareFilenames returns for patterns
// resolved against the working directory dir, from the sorted repository
// paths files: the matched files, and the files directly in every matched
// directory, sorted and deduplicated. why is set when a pattern cannot be
// resolved; the config is then unproven.
func resolveSchema(files []string, dir string, patterns []string) (matched []string, why string) {
	dirs := map[string]bool{}
	for _, f := range files {
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			if dirs[d] {
				break
			}
			dirs[d] = true
		}
	}
	dirs["."] = true
	byDir := map[string][]string{}
	for _, f := range files {
		byDir[path.Dir(f)] = append(byDir[path.Dir(f)], f)
	}
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			matched = append(matched, f)
		}
	}
	for _, p := range patterns {
		if e := globError(p); e != "" {
			return nil, e
		}
		joined := path.Join(dir, p)
		if joined == ".." || strings.HasPrefix(joined, "../") {
			return nil, "schema path " + quote(p) + " is outside the repository"
		}
		segs := strings.Split(joined, "/")
		if joined == "." {
			segs = nil
		}
		for _, f := range files {
			if matchSegments(segs, strings.Split(f, "/")) {
				add(f)
			}
		}
		for d := range dirs {
			var ds []string
			if d != "." {
				ds = strings.Split(d, "/")
			}
			if matchSegments(segs, ds) {
				for _, f := range byDir[d] {
					add(f)
				}
			}
		}
	}
	sort.Strings(matched)
	return matched, ""
}

// matchSegments matches path segments against pattern segments; a `**`
// segment matches zero or more segments that do not start with a dot.
func matchSegments(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		if matchSegments(p[1:], s) {
			return true
		}
		return len(s) > 0 && !strings.HasPrefix(s[0], ".") && matchSegments(p, s[1:])
	}
	if len(s) == 0 || !matchSegment(p[0], s[0]) {
		return false
	}
	return matchSegments(p[1:], s[1:])
}

func matchSegment(p, s string) bool {
	if !hasMagic(p) {
		return p == s
	}
	if strings.HasPrefix(s, ".") && !strings.HasPrefix(p, ".") {
		return false
	}
	// Classes and escapes were refused by globError, so path.Match sees
	// only `*` and `?`, which never cross a segment.
	ok, err := path.Match(p, s)
	return err == nil && ok
}
