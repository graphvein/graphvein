package python

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// This file reads the lowest FastAPI version a project's manifest allows
// (T029 SH-F, docs/contract/claims/route-shape-fastapi.md rows SH-F20 and
// SH-F26): some FastAPI behaviour depends on the version (return
// annotations validated from 0.89.0, form models flattened from 0.113.0),
// and a claim that needs a version is stated only when every install the
// manifest allows has it.

// Version is a release number major.minor.patch.
type Version [3]int

// AtLeast reports v >= o.
func (v Version) AtLeast(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] > o[i]
		}
	}
	return true
}

func (v Version) String() string {
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2])
}

// FastAPIMinimum returns the lowest FastAPI version the nearest
// pyproject.toml enclosing file that requires fastapi (or fastapi-slim)
// allows, and that manifest's path. ok is false when no enclosing
// pyproject.toml requires it with a lower bound read: a `>=`, `==`, `~=`
// or `>` PEP 508 specifier in project.dependencies, or a Poetry
// constraint in tool.poetry.dependencies.
func (r *ModuleResolver) FastAPIMinimum(file string) (Version, string, bool) {
	return r.requirementMinimum(file, fastAPIName)
}

// PydanticMinimum returns the lowest Pydantic version the nearest
// pyproject.toml enclosing file that requires pydantic directly allows,
// read like FastAPIMinimum. Pydantic 1 and 2 disagree on whether a
// nullable field without a default must be sent (row SH-F22), so that
// claim needs a proven major version. A pydantic requirement reached only
// through another package (fastapi, sqlmodel) is not read: ok is false.
func (r *ModuleResolver) PydanticMinimum(file string) (Version, string, bool) {
	return r.requirementMinimum(file, pydanticName)
}

// requirementMinimum finds the nearest enclosing pyproject.toml that
// declares a package matched by isName and reads its lowest allowed
// version.
func (r *ModuleResolver) requirementMinimum(file string, isName func(string) bool) (Version, string, bool) {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if text, found := r.pyprojects[dir]; found {
			if v, declared, ok := packageRequirement(text, isName); declared {
				manifest := path.Join(dir, "pyproject.toml")
				return v, manifest, ok
			}
		}
		if dir == "." || dir == "/" || dir == "" {
			return Version{}, "", false
		}
	}
}

var fastapiRequirementName = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)\s*(\[[^\]]*\])?\s*(.*)$`)

// fastAPIRequirement reads one pyproject.toml for fastapi.
func fastAPIRequirement(text string) (Version, bool, bool) {
	return packageRequirement(text, fastAPIName)
}

// packageRequirement reads one pyproject.toml: declared is true when it
// requires a package matched by isName; ok when the lower bound is read. Within one source
// (project.dependencies, tool.poetry.dependencies) every requirement
// holds, so the highest bound applies; between the two sources the lower
// one does (either may be the one installed from). A tool.uv override or
// constraint naming the package replaces what the manifest says: not read.
func packageRequirement(text string, isName func(string) bool) (Version, bool, bool) {
	doc, err := parseTOML(text)
	if err != nil {
		return Version{}, false, false
	}
	declared, unread := false, false
	var sources [][]Version
	if project, ok := doc["project"].(map[string]any); ok {
		var found []Version
		deps, _ := project["dependencies"].([]any)
		for _, d := range deps {
			s, _ := d.(string)
			m := fastapiRequirementName.FindStringSubmatch(s)
			if m == nil || !isName(m[1]) {
				continue
			}
			declared = true
			spec := m[3]
			if strings.ContainsAny(spec, ";@|") {
				// An environment marker, a direct reference or an
				// alternative: the installed version is not bounded by
				// this line.
				unread = true
				continue
			}
			if v, ok := lowerBound(strings.Split(spec, ","), false); ok {
				found = append(found, v)
			} else {
				unread = true
			}
		}
		if len(found) > 0 {
			sources = append(sources, found)
		}
	}
	if tool, ok := doc["tool"].(map[string]any); ok {
		if poetry, ok := tool["poetry"].(map[string]any); ok {
			var found []Version
			deps, _ := poetry["dependencies"].(map[string]any)
			for name, d := range deps {
				if !isName(name) {
					continue
				}
				declared = true
				spec := ""
				switch v := d.(type) {
				case string:
					spec = v
				case map[string]any:
					spec, _ = v["version"].(string)
				}
				if v, ok := poetryBound(spec); ok {
					found = append(found, v)
				} else {
					unread = true
				}
			}
			if len(found) > 0 {
				sources = append(sources, found)
			}
		}
		if uv, ok := tool["uv"].(map[string]any); ok {
			for _, key := range []string{"override-dependencies", "constraint-dependencies"} {
				list, _ := uv[key].([]any)
				for _, d := range list {
					s, _ := d.(string)
					if m := fastapiRequirementName.FindStringSubmatch(s); m != nil && isName(m[1]) {
						declared, unread = true, true
					}
				}
			}
		}
	}
	if !declared || unread || len(sources) == 0 {
		return Version{}, declared, false
	}
	var result Version
	for i, found := range sources {
		best := found[0]
		for _, v := range found[1:] {
			if v.AtLeast(best) {
				best = v
			}
		}
		if i == 0 || result.AtLeast(best) {
			result = best
		}
	}
	return result, true, true
}

// poetryBound reads a Poetry constraint: the lowest bound of its `||` (or
// `|`) alternatives, each a comma or space separated specifier list.
func poetryBound(spec string) (Version, bool) {
	var lowest Version
	for i, alt := range strings.Split(strings.ReplaceAll(spec, "||", "|"), "|") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			return Version{}, false
		}
		v, ok := lowerBound(strings.FieldsFunc(alt, func(r rune) bool { return r == ',' || r == ' ' }), true)
		if !ok {
			return Version{}, false
		}
		if i == 0 || lowest.AtLeast(v) {
			lowest = v
		}
	}
	return lowest, true
}

func fastAPIName(name string) bool {
	n := normalizedName(name)
	return n == "fastapi" || n == "fastapi-slim"
}

func pydanticName(name string) bool {
	return normalizedName(name) == "pydantic"
}

// normalizedName is a distribution name in PEP 503 normal form.
func normalizedName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", "-"), ".", "-"))
}

// lowerBound reads the highest lower bound of a specifier list. poetry
// accepts Poetry's `^X` and a bare version (meaning ^X).
func lowerBound(specs []string, poetry bool) (Version, bool) {
	var best Version
	ok := false
	for _, s := range specs {
		s = strings.TrimSpace(s)
		var text string
		switch {
		case strings.HasPrefix(s, ">="), strings.HasPrefix(s, "=="), strings.HasPrefix(s, "~="):
			text = s[2:]
		case strings.HasPrefix(s, ">"):
			text = s[1:]
		case poetry && (strings.HasPrefix(s, "^") || strings.HasPrefix(s, "~")):
			text = s[1:]
		case poetry && s != "" && s[0] >= '0' && s[0] <= '9':
			text = s
		default:
			// <, <=, !=, an empty specifier or a URL bound nothing below.
			continue
		}
		v, read := parseVersion(strings.TrimSpace(text))
		if !read {
			return Version{}, false
		}
		if !ok || v.AtLeast(best) {
			best, ok = v, true
		}
	}
	return best, ok
}

// parseVersion reads a release number's first three parts; a trailing
// `.*` ends it. Any pre-release, post or dev tag (0.89.0rc1 sorts below
// 0.89.0) makes it not read.
func parseVersion(s string) (Version, bool) {
	var v Version
	n := 0
	for i, p := range strings.Split(s, ".") {
		if p == "*" && i > 0 {
			break
		}
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return Version{}, false
		}
		x, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		if n < 3 {
			v[n] = x
			n++
		}
	}
	return v, n > 0
}
