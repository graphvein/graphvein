package packages

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/cargotoml"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Each reader returns a manifest (a confirmed package), or a nonempty
// refusal reason (row CM-U1), or neither (not a package: tool
// configuration, a workspace root, a package.json without a name).

// readPackageJSON is row CM-M1 (and CM-W1 for a nameless workspaces root).
func readPackageJSON(file, source string) (*manifest, string) {
	var doc any
	if err := json.Unmarshal([]byte(source), &doc); err != nil {
		return nil, "invalid JSON"
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, "not a JSON object"
	}
	raw, present := obj["name"]
	name, isString := raw.(string)
	if present && raw != nil && !isString {
		return nil, "name is not a string"
	}
	if name == "" {
		if _, ok := obj["workspaces"]; ok {
			tag("CM-W1/npm")
			return nil, ""
		}
		tag("CM-M1/none")
		return nil, ""
	}
	tag("CM-M1")
	attrs := map[string]any{}
	if w, ok := npmWorkspaces(obj["workspaces"]); ok {
		attrs["workspaces"] = w
	}
	switch b := obj["bin"].(type) {
	case string:
		if b != "" {
			attrs["bins"] = []any{path.Base(name)}
		}
	case map[string]any:
		keys := make([]string, 0, len(b))
		for k := range b {
			keys = append(keys, k)
		}
		if len(keys) > 0 {
			attrs["bins"] = sortedSet(keys)
		}
	}
	var deps []string
	for _, section := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		if m, ok := obj[section].(map[string]any); ok {
			for k := range m {
				deps = append(deps, k)
			}
		}
	}
	attrs["dependencies"] = sortedSet(deps)
	return &manifest{file: file, ecosystem: "npm", name: name, attrs: attrs}, ""
}

// npmWorkspaces reads the two forms npm and Yarn accept: a string array or
// an object with a string array "packages".
func npmWorkspaces(v any) ([]any, bool) {
	if m, ok := v.(map[string]any); ok {
		v = m["packages"]
	}
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	var out []string
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return asWritten(out), true
}

// readCargo is row CM-M2 (and CM-W1 for a virtual manifest), read with the
// Cargo TOML reader every Cargo extractor shares.
func readCargo(file, source string) (*manifest, string) {
	doc, err := cargotoml.Parse(source)
	if err != nil {
		return nil, fmt.Sprintf("TOML the reader does not accept (%v)", err)
	}
	pkg, hasPackage := doc["package"].(cargotoml.Table)
	if _, present := doc["package"]; present && !hasPackage {
		return nil, "package is not a table"
	}
	if !hasPackage {
		if _, ok := doc["workspace"].(cargotoml.Table); ok {
			tag("CM-W1/cargo")
			return nil, ""
		}
		return nil, "neither [package] nor [workspace]"
	}
	name, _ := pkg["name"].(string)
	if name == "" {
		return nil, "[package] without a string name"
	}
	tag("CM-M2")
	attrs := map[string]any{}
	var bins []string
	if list, ok := doc["bin"].([]any); ok {
		for _, item := range list {
			if t, ok := item.(cargotoml.Table); ok {
				if n, ok := t["name"].(string); ok {
					bins = append(bins, n)
				}
			}
		}
	}
	attrs["bins"] = sortedSet(bins)
	_, lib := doc["lib"].(cargotoml.Table)
	attrs["lib"] = lib
	// autobins = false turns off src/main.rs and src/bin discovery.
	attrs["autobins"] = pkg["autobins"] != cargotoml.Scalar("false")
	var deps []string
	for _, t := range cargotoml.AllDependencyTables(doc) {
		for key, entry := range t {
			name := key
			if e, ok := entry.(cargotoml.Table); ok {
				if renamed, ok := e["package"].(string); ok && renamed != "" {
					name = renamed
				}
			}
			deps = append(deps, name)
		}
	}
	attrs["dependencies"] = sortedSet(deps)
	return &manifest{file: file, ecosystem: "cargo", name: name, attrs: attrs}, ""
}

// readPyproject is row CM-M3 (and CM-W1 for a uv workspace root without a
// project). It uses the same TOML reader as Cargo: it accepts multi-line
// inline tables, which uv and pip accept too.
func readPyproject(file, source string) (*manifest, string) {
	doc, err := cargotoml.Parse(source)
	if err != nil {
		return nil, fmt.Sprintf("TOML the reader does not accept (%v)", err)
	}
	tool, _ := doc["tool"].(cargotoml.Table)
	poetry, _ := tool["poetry"].(cargotoml.Table)
	name := ""
	if raw, present := doc["project"]; present {
		project, ok := raw.(cargotoml.Table)
		if !ok {
			return nil, "project is not a table"
		}
		name, _ = project["name"].(string)
		if name == "" {
			return nil, "[project] without a string name"
		}
	} else if poetry != nil {
		name, _ = poetry["name"].(string)
	}
	if name == "" {
		if uv, ok := tool["uv"].(cargotoml.Table); ok {
			if _, ok := uv["workspace"].(cargotoml.Table); ok {
				tag("CM-W1/uv")
				return nil, ""
			}
		}
		tag("CM-M3/none")
		return nil, ""
	}
	tag("CM-M3")
	project, _ := doc["project"].(cargotoml.Table)
	var scripts []string
	for _, t := range []cargotoml.Table{tableAt(project, "scripts"), tableAt(poetry, "scripts")} {
		for k := range t {
			scripts = append(scripts, k)
		}
	}
	var deps []string
	if list, ok := project["dependencies"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				deps = append(deps, requirementName(s))
			}
		}
	}
	for k := range tableAt(poetry, "dependencies") {
		if k != "python" {
			deps = append(deps, k)
		}
	}
	attrs := map[string]any{"scripts": sortedSet(scripts), "dependencies": sortedSet(deps)}
	return &manifest{file: file, ecosystem: "python", name: name, attrs: attrs}, ""
}

func tableAt(t cargotoml.Table, key string) cargotoml.Table {
	if t == nil {
		return nil
	}
	v, _ := t[key].(cargotoml.Table)
	return v
}

// requirementName is the distribution name of a PEP 508 requirement as
// written (before any version, extra, marker or URL).
func requirementName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " <>=!~;[(@"); i >= 0 {
		s = s[:i]
	}
	return s
}

// readGoMod is row CM-M4. mainFiles are the stored Go files of the module
// in package main that declare func main (row CK5's Go form); the server
// reads them as stored facts.
func readGoMod(file, source string, goFacts map[string]typescript.Facts, files []string) (*manifest, string) {
	module := ""
	for _, line := range strings.Split(source, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		words := strings.Fields(line)
		if len(words) == 2 && words[0] == "module" && words[1] != "(" {
			module = strings.Trim(words[1], "\"`")
			break
		}
	}
	if module == "" {
		return nil, "no module directive"
	}
	tag("CM-M4")
	dir := path.Dir(file)
	modules := map[string]bool{}
	for _, f := range files {
		if path.Base(f) == "go.mod" {
			modules[path.Dir(f)] = true
		}
	}
	var mains []string
	for f, fx := range goFacts {
		if fx.Package != "main" || nearest(modules, path.Dir(f)) != dir {
			continue
		}
		for _, n := range fx.Nodes {
			if n.Kind == "function" && n.QualifiedName == "main" {
				mains = append(mains, f)
				break
			}
		}
	}
	return &manifest{file: file, ecosystem: "go", name: module, attrs: map[string]any{"main_files": sortedSet(mains)}}, ""
}

// nearest is the deepest directory of set enclosing dir, or "" when none.
func nearest(set map[string]bool, dir string) string {
	for {
		if set[dir] {
			return dir
		}
		if dir == "." {
			return ""
		}
		dir = path.Dir(dir)
	}
}

// readSetupPy is row CM-M5's setup.py form: a module-level call of
// setuptools' or distutils' setup, found through the cached Python facts
// (imports and calls as the structure extractor read them; nothing runs).
func readSetupPy(file, source string, fx typescript.Facts) (*manifest, string) {
	if fx.Python == nil || fx.Python.Rejected {
		// Not read as Python (too large, parse failure, rejected syntax):
		// the reader refuses, so CM-U1 applies.
		return nil, "setup.py was not read as Python"
	}
	callables := map[string]bool{}
	for _, imp := range fx.Imports {
		if imp.Scope != "" {
			continue
		}
		for alias, name := range imp.Bindings {
			if imp.From {
				if (imp.Module == "setuptools" || imp.Module == "distutils.core") && name == "setup" {
					callables[alias] = true
				}
				continue
			}
			if name != "setuptools" && name != "distutils.core" {
				continue
			}
			prefix := name
			if imp.ExplicitAlias {
				prefix = alias
			}
			callables[prefix+".setup"] = true
		}
	}
	for _, call := range fx.Calls {
		if call.Scope != "" || call.Shadowed || !callables[call.Name] {
			continue
		}
		tag("CM-M5")
		name, _ := setupName(source, call.Span)
		return &manifest{file: file, ecosystem: "python", name: name, attrs: map[string]any{}}, ""
	}
	tag("CM-M5/none")
	return nil, ""
}

// readSetupCfg is row CM-M5's setup.cfg form: [metadata] name.
func readSetupCfg(file, source string) *manifest {
	section := ""
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		if section != "metadata" || line != strings.TrimLeft(line, " \t") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			key, value, ok = strings.Cut(trimmed, ":")
		}
		if ok && strings.TrimSpace(key) == "name" && strings.TrimSpace(value) != "" {
			tag("CM-M5")
			return &manifest{file: file, ecosystem: "python", name: strings.TrimSpace(value), attrs: map[string]any{}}
		}
	}
	tag("CM-M5/none")
	return nil
}

// setupName reads the keyword argument name= of the setup call at span
// when its value is one plain string literal; anything else names nothing.
func setupName(source string, span contract.Span) (string, bool) {
	start, end := offset(source, span.StartLine, span.StartColumn), offset(source, span.EndLine, span.EndColumn)
	if start < 0 || end < start || end > len(source) {
		return "", false
	}
	text := source[start:end]
	open := strings.IndexByte(text, '(')
	if open < 0 || !strings.HasSuffix(text, ")") {
		return "", false
	}
	for _, arg := range topLevelArgs(text[open+1 : len(text)-1]) {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || strings.TrimSpace(key) != "name" {
			continue
		}
		return stringLiteral(strings.TrimSpace(value))
	}
	return "", false
}

// offset converts a 1-based line and byte column to a byte offset.
func offset(source string, line, column int) int {
	at := 0
	for l := 1; l < line; l++ {
		i := strings.IndexByte(source[at:], '\n')
		if i < 0 {
			return -1
		}
		at += i + 1
	}
	return at + column
}

// topLevelArgs splits an argument list at commas outside brackets and
// strings.
func topLevelArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"':
			quote := string(c)
			if strings.HasPrefix(s[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			j := i + len(quote)
			for j < len(s) && !strings.HasPrefix(s[j:], quote) {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j + len(quote) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// stringLiteral accepts one single-line quoted literal without prefix or
// escapes.
func stringLiteral(s string) (string, bool) {
	if len(s) < 3 {
		return "", false
	}
	q := s[0]
	if q != '"' && q != '\'' || s[len(s)-1] != q {
		return "", false
	}
	inner := s[1 : len(s)-1]
	if strings.ContainsAny(inner, "\\\n\"'") {
		return "", false
	}
	return inner, true
}
