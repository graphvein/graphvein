package rust

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// manifest holds the parts of a Cargo.toml the Rust resolver reads: the
// crate roots (row RS-M1 of docs/lang/rust-structure.md), the edition (row
// RS-I4) and the dependency tables (rows RS-I2 and RS-I3). Everything else
// in the file is skipped.
type manifest struct {
	packageTable bool
	name         string
	// lib reports a [lib] table; libName and libPath are its keys.
	lib              bool
	libName, libPath string
	// auto holds the [package] target discovery keys that are set
	// (autolib, autobins, autotests, autoexamples, autobenches).
	auto map[string]bool
	// targets are the [[bin]], [[test]], [[example]] and [[bench]] entries,
	// keyed by kind ("bin", "test", "example", "bench").
	targets map[string][]manifestTarget
	// build is the [package] build key: "" (absent), "false", "true" or
	// the build script path as written.
	build string
	// edition is the [package] edition as written. editionWorkspace reports
	// `edition.workspace = true`; editionUnread an edition value this
	// reader does not read. A package without an edition key is edition
	// 2015, as for cargo.
	edition          string
	editionWorkspace bool
	editionUnread    bool
	// workspace reports a [workspace] table; workspaceEdition and
	// workspaceDeps are its [workspace.package] edition and
	// [workspace.dependencies] entries.
	workspace        bool
	workspaceEdition string
	workspaceDeps    map[string]dependency
	// deps are the entries of every dependency table ([dependencies],
	// [dev-dependencies], [build-dependencies] and their [target.*] forms),
	// keyed by table and dependency key.
	deps map[string]dependency
}

// manifestTarget is one [[bin]], [[test]], [[example]] or [[bench]] entry.
type manifestTarget struct {
	name, path string
}

// targetKinds are the target arrays of a manifest with their discovery key
// and the directory cargo discovers them in.
var targetKinds = []struct{ kind, auto, dir string }{{"bin", "autobins", "src/bin"}, {"test", "autotests", "tests"}, {"example", "autoexamples", "examples"}, {"bench", "autobenches", "benches"}}

// dependency is one dependency entry as written. Source is "path",
// "registry", "git", "workspace" (`workspace = true`: inherited from the
// workspace root's [workspace.dependencies]) or "unknown" (a value this
// reader does not read). Package is the `package` key of a renamed
// dependency.
type dependency struct {
	Source  string
	Path    string
	Package string
}

// dependencyTable reports whether a table header (its dotted parts) names
// a dependency table, whether it is a build-dependencies table, and the
// dependency key of the `[dependencies.<key>]` form.
func dependencyTable(parts []string) (isTable, build bool, key string) {
	if len(parts) >= 3 && parts[0] == "target" {
		parts = parts[2:]
	} else if len(parts) >= 2 && parts[0] == "workspace" {
		if parts[1] != "dependencies" || len(parts) > 3 {
			return false, false, ""
		}
		if len(parts) == 3 {
			return true, false, parts[2]
		}
		return true, false, ""
	}
	if len(parts) == 0 || len(parts) > 2 {
		return false, false, ""
	}
	switch parts[0] {
	case "dependencies", "dev-dependencies", "dev_dependencies":
	case "build-dependencies", "build_dependencies":
		build = true
	default:
		return false, false, ""
	}
	if len(parts) == 2 {
		return true, build, parts[1]
	}
	return true, build, ""
}

// splitDotted splits a TOML dotted key (bare or quoted parts) into its parts.
func splitDotted(key string) []string {
	var parts []string
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			b.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, b.String())
			b.Reset()
		case c == ' ' || c == '\t':
		default:
			b.WriteByte(c)
		}
	}
	return append(parts, b.String())
}

// tomlStatement is one TOML statement: a table header or a key/value pair
// whose raw value may span several lines (a multi-line string, an array, an
// inline table).
type tomlStatement struct {
	line   int
	header bool
	// array marks an array-of-tables header ([[bin]]).
	array bool
	// key holds the dotted parts of the header or of the assignment key.
	key   []string
	value string
}

// tomlStatements splits a manifest into statements, tracking every string
// form (basic, literal and their multi-line forms), comments and bracket
// nesting, so that no line inside a multi-line value is read as a header or
// an assignment. A value that does not end (an unclosed string, array or
// inline table) or a line that is neither a header nor an assignment is an
// error.
func tomlStatements(source string) ([]tomlStatement, error) {
	s := strings.TrimPrefix(source, "\ufeff")
	var out []tomlStatement
	line := 1
	i := 0
	skipComment := func() {
		for i < len(s) && s[i] != '\n' {
			i++
		}
	}
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\n':
			line++
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case c == '#':
			skipComment()
			continue
		}
		start := line
		if c == '[' {
			array := strings.HasPrefix(s[i:], "[[")
			open, closing := 1, "]"
			if array {
				open, closing = 2, "]]"
			}
			j := i + open
			quote := byte(0)
			for ; j < len(s) && s[j] != '\n'; j++ {
				if quote != 0 {
					if s[j] == quote {
						quote = 0
					}
					continue
				}
				if s[j] == '"' || s[j] == '\'' {
					quote = s[j]
				} else if s[j] == ']' {
					break
				}
			}
			if j >= len(s) || !strings.HasPrefix(s[j:], closing) {
				return nil, fmt.Errorf("Cargo.toml line %d: unclosed table", start)
			}
			name := s[i+open : j]
			i = j + len(closing)
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
				i++
			}
			if i < len(s) && s[i] == '#' {
				skipComment()
			}
			if i < len(s) && s[i] != '\n' {
				return nil, fmt.Errorf("Cargo.toml line %d: text after a table header", start)
			}
			out = append(out, tomlStatement{line: start, header: true, array: array, key: splitDotted(strings.TrimSpace(name))})
			continue
		}
		// key = value
		j := i
		quote := byte(0)
		for ; j < len(s); j++ {
			if s[j] == '\n' {
				return nil, fmt.Errorf("Cargo.toml line %d: expected assignment", start)
			}
			if quote != 0 {
				if s[j] == quote {
					quote = 0
				}
				continue
			}
			if s[j] == '"' || s[j] == '\'' {
				quote = s[j]
			} else if s[j] == '=' {
				break
			}
		}
		if j >= len(s) {
			return nil, fmt.Errorf("Cargo.toml line %d: expected assignment", start)
		}
		key := strings.TrimSpace(s[i:j])
		end, lines, err := valueEnd(s, j+1)
		if err != nil {
			return nil, fmt.Errorf("Cargo.toml line %d: %v", start, err)
		}
		value := strings.TrimSpace(stripComment(s[j+1 : end]))
		line += lines
		i = end
		out = append(out, tomlStatement{line: start, key: splitDotted(key), value: value})
	}
	return out, nil
}

// valueEnd returns the end of the raw value starting at i (the newline
// that ends it, or the end of the text) and the number of newlines inside
// it.
func valueEnd(s string, i int) (end, lines int, err error) {
	depth := 0
	for i < len(s) {
		c := s[i]
		switch {
		case strings.HasPrefix(s[i:], `"""`) || strings.HasPrefix(s[i:], "'''"):
			n, nl, ok := multiLineString(s[i:])
			if !ok {
				return 0, 0, fmt.Errorf("unclosed multi-line string")
			}
			i += n
			lines += nl
			continue
		case c == '"' || c == '\'':
			j := i + 1
			for ; j < len(s) && s[j] != c && s[j] != '\n'; j++ {
				if c == '"' && s[j] == '\\' {
					j++
				}
			}
			if j >= len(s) || s[j] != c {
				return 0, 0, fmt.Errorf("unclosed string")
			}
			i = j + 1
			continue
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '\n':
			if depth <= 0 {
				return i, lines, nil
			}
			lines++
		}
		i++
	}
	if depth > 0 {
		return 0, 0, fmt.Errorf("unclosed array or inline table")
	}
	return i, lines, nil
}

// multiLineString returns the length of the multi-line string starting at
// s[0] (with its delimiters) and the newlines inside it. The closing
// delimiter may be followed by up to two more quotes, which belong to the
// content.
func multiLineString(s string) (n, lines int, ok bool) {
	delim := s[:3]
	for i := 3; i < len(s); i++ {
		if s[i] == '\n' {
			lines++
		}
		if delim == `"""` && s[i] == '\\' {
			i++
			if i < len(s) && s[i] == '\n' {
				lines++
			}
			continue
		}
		if strings.HasPrefix(s[i:], delim) {
			end := i + 3
			for extra := 0; extra < 2 && end < len(s) && s[end] == delim[0]; extra++ {
				end++
			}
			return end, lines, true
		}
	}
	return 0, 0, false
}

// stripComment removes a trailing comment from a single-line value text.
// Multi-line values keep their inner comments; readValue skips them.
func stripComment(v string) string {
	if strings.Contains(v, "\n") {
		return v
	}
	quote := byte(0)
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return v[:i]
		}
	}
	return v
}

// tomlValue is a decoded TOML value: kind 's' (string), 'b' (boolean),
// 't' (inline table), 'a' (array) or 'o' (any other scalar: a number or a
// date, kept as written).
type tomlValue struct {
	kind  byte
	text  string
	table map[string]tomlValue
	array []tomlValue
}

// parseValue decodes a whole raw value; ok is false for anything this
// reader does not read (it never guesses).
func parseValue(raw string) (tomlValue, bool) {
	v, rest, ok := readValue(raw)
	if !ok || skipBlank(rest) != "" {
		return tomlValue{}, false
	}
	return v, true
}

// skipBlank skips white space, newlines and comments.
func skipBlank(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if !strings.HasPrefix(s, "#") {
			return s
		}
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return ""
		}
		s = s[i:]
	}
}

func readValue(s string) (tomlValue, string, bool) {
	s = strings.TrimLeft(s, " \t")
	switch {
	case s == "":
		return tomlValue{}, s, false
	case strings.HasPrefix(s, `"""`), strings.HasPrefix(s, "'''"):
		n, _, ok := multiLineString(s)
		if !ok {
			return tomlValue{}, s, false
		}
		text, ok := decodeMultiLine(s[:n])
		return tomlValue{kind: 's', text: text}, s[n:], ok
	case s[0] == '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 || strings.Contains(s[1:end+1], "\n") {
			return tomlValue{}, s, false
		}
		return tomlValue{kind: 's', text: s[1 : end+1]}, s[end+2:], true
	case s[0] == '"':
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '\n' {
				return tomlValue{}, s, false
			}
			if s[i] == '"' {
				text, ok := decodeBasic(s[1:i], false)
				return tomlValue{kind: 's', text: text}, s[i+1:], ok
			}
		}
		return tomlValue{}, s, false
	case s[0] == '[':
		v := tomlValue{kind: 'a'}
		rest := skipBlank(s[1:])
		for !strings.HasPrefix(rest, "]") {
			item, r, ok := readValue(rest)
			if !ok {
				return tomlValue{}, s, false
			}
			v.array = append(v.array, item)
			rest = skipBlank(r)
			if strings.HasPrefix(rest, ",") {
				rest = skipBlank(rest[1:])
			} else if !strings.HasPrefix(rest, "]") {
				return tomlValue{}, s, false
			}
		}
		return v, rest[1:], true
	case s[0] == '{':
		// Inline tables, including TOML 1.1's newlines and trailing
		// comma, which cargo 1.97 accepts.
		v := tomlValue{kind: 't', table: map[string]tomlValue{}}
		rest := skipBlank(s[1:])
		for !strings.HasPrefix(rest, "}") {
			key, value, ok := cutAssignment(rest)
			if !ok || strings.Contains(key, "\n") || strings.ContainsAny(key, ",{}[]#") {
				return tomlValue{}, s, false
			}
			item, r, ok := readValue(value)
			if !ok {
				return tomlValue{}, s, false
			}
			name := strings.Join(splitDotted(key), ".")
			if _, dup := v.table[name]; dup {
				return tomlValue{}, s, false
			}
			v.table[name] = item
			rest = skipBlank(r)
			if strings.HasPrefix(rest, ",") {
				rest = skipBlank(rest[1:])
			} else if !strings.HasPrefix(rest, "}") {
				return tomlValue{}, s, false
			}
		}
		return v, rest[1:], true
	}
	end := strings.IndexAny(s, ",]} \t\r\n#")
	if end < 0 {
		end = len(s)
	}
	word := s[:end]
	if word == "" {
		return tomlValue{}, s, false
	}
	if word == "true" || word == "false" {
		return tomlValue{kind: 'b', text: word}, s[end:], true
	}
	return tomlValue{kind: 'o', text: word}, s[end:], true
}

// decodeMultiLine decodes a multi-line basic ("""...""") or literal
// (”'...”') string with its delimiters: a newline right after the opening
// delimiter is dropped, and a basic string's escapes are decoded.
func decodeMultiLine(s string) (string, bool) {
	delim := s[:3]
	body := s[3 : len(s)-3]
	if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	} else if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	if delim == "'''" {
		return body, true
	}
	return decodeBasic(body, true)
}

// decodeBasic decodes the escapes of a TOML 1.0 basic string body. A
// multi-line body also takes the line-ending backslash. Any other escape
// (including TOML 1.1's \x and \e) is not read.
func decodeBasic(s string, multiLine bool) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", false
		}
		switch s[i] {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u', 'U':
			n := 4
			if s[i] == 'U' {
				n = 8
			}
			if i+n >= len(s) {
				return "", false
			}
			v, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if err != nil || !utf8.ValidRune(rune(v)) {
				return "", false
			}
			b.WriteRune(rune(v))
			i += n
		case ' ', '\t', '\r', '\n':
			if !multiLine {
				return "", false
			}
			// A line-ending backslash: only white space may follow it on
			// its line; it trims every white space and newline after it.
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r') {
				j++
			}
			if j >= len(s) || s[j] != '\n' {
				return "", false
			}
			for j < len(s) && strings.IndexByte(" \t\r\n", s[j]) >= 0 {
				j++
			}
			i = j - 1
		default:
			return "", false
		}
	}
	return b.String(), true
}

// cutAssignment splits `key = value` at the first `=` outside a quoted key.
func cutAssignment(line string) (key, value string, ok bool) {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
		case c == '"' || c == '\'':
			quote = c
		case c == '=':
			return strings.TrimSpace(line[:i]), line[i+1:], true
		}
	}
	return "", "", false
}

// tomlString decodes a whole raw value that is a TOML string (any of the
// four forms).
func tomlString(raw string) (string, bool) {
	v, ok := parseValue(raw)
	return v.text, ok && v.kind == 's'
}

// topLevelKeys are the manifest tables that a top-level dotted key or
// inline table could also define (`lib.path = ...`, `package = { ... }`).
// This reader only reads them as tables, so it refuses a manifest that
// defines one any other way rather than misread it.
var topLevelKeys = setOf("package", "lib", "bin", "test", "example", "bench", "workspace", "target",
	"dependencies", "dev-dependencies", "dev_dependencies", "build-dependencies", "build_dependencies")

// readManifest reads the resolution-relevant values of a Cargo.toml. A
// crate root key (package name, build, the target discovery keys, lib or
// target name or path) whose value has the wrong type, a line that is
// neither a header nor an assignment, a value that does not end, or a
// structural table defined by a top-level key is an error: the indexer
// then reads no crate root from the manifest (open point O-S4-5 (a)).
// Unrelated tables and values are skipped.
func readManifest(source string) (manifest, error) {
	out := manifest{deps: map[string]dependency{}, workspaceDeps: map[string]dependency{}, auto: map[string]bool{}, targets: map[string][]manifestTarget{}}
	statements, err := tomlStatements(source)
	if err != nil {
		return out, err
	}
	table := ""
	targetKind := ""
	depKey := ""
	// depTable keys the dependency entries of the current table: the
	// table name, prefixed "b\x01" for build-dependencies tables and
	// "n\x01" for the others; workspace tables keep their name.
	depTable := ""
	inDeps := false
	for _, st := range statements {
		if st.header {
			table = strings.Join(st.key, ".")
			targetKind = ""
			inDeps, depKey = false, ""
			if st.array {
				switch table {
				case "bin", "test", "example", "bench":
					targetKind = table
					out.targets[table] = append(out.targets[table], manifestTarget{})
				}
				table = "[[" + table + "]]"
				continue
			}
			switch table {
			case "package":
				out.packageTable = true
			case "workspace":
				out.workspace = true
			case "lib":
				out.lib = true
			}
			var build bool
			inDeps, build, depKey = dependencyTable(st.key)
			switch {
			case st.key[0] == "workspace":
				depTable = table
			case build:
				depTable = "b\x01" + table
			default:
				depTable = "n\x01" + table
			}
			if inDeps && depKey != "" {
				out.setDep(depTable, depKey, dependency{Source: "registry"})
			}
			continue
		}
		if table == "" {
			if topLevelKeys[st.key[0]] {
				return out, fmt.Errorf("Cargo.toml line %d: %s defined by a top-level key", st.line, st.key[0])
			}
			continue
		}
		value, readable := parseValue(st.value)
		if inDeps {
			out.readDep(depTable, depKey, st.key, value, readable)
			continue
		}
		key := strings.Join(st.key, ".")
		stringKey := func() (string, error) {
			if !readable || value.kind != 's' {
				return "", fmt.Errorf("Cargo.toml line %d: %s requires a string", st.line, key)
			}
			return value.text, nil
		}
		switch {
		case table == "package" && st.key[0] == "edition":
			switch {
			case key == "edition.workspace" && readable && value.kind == 'b':
				out.editionWorkspace = value.text == "true"
				out.editionUnread = value.text != "true"
			case key == "edition" && readable && value.kind == 't' && len(value.table) == 1 && value.table["workspace"].kind == 'b' && value.table["workspace"].text == "true":
				out.editionWorkspace = true
			case key == "edition" && readable && value.kind == 's':
				out.edition = value.text
			default:
				out.editionUnread = true
			}
		case table == "package" && key == "name":
			if out.name, err = stringKey(); err != nil {
				return out, err
			}
		case table == "package" && (key == "autolib" || key == "autobins" || key == "autotests" || key == "autoexamples" || key == "autobenches"):
			if !readable || value.kind != 'b' {
				return out, fmt.Errorf("Cargo.toml line %d: %s requires a boolean", st.line, key)
			}
			out.auto[key] = value.text == "true"
		case table == "package" && key == "build":
			switch {
			case readable && value.kind == 'b':
				out.build = value.text
			case readable && value.kind == 's' && value.text != "":
				out.build = value.text
			default:
				return out, fmt.Errorf("Cargo.toml line %d: build requires a string or a boolean", st.line)
			}
		case table == "package" && (st.key[0] == "name" || st.key[0] == "build" || strings.HasPrefix(st.key[0], "auto")):
			// A dotted form of a key read above: not read, so the
			// manifest is refused rather than misread.
			return out, fmt.Errorf("Cargo.toml line %d: %s is not read", st.line, key)
		case table == "workspace.package" && key == "edition":
			if readable && value.kind == 's' {
				out.workspaceEdition = value.text
			}
		case table == "lib" && (key == "name" || key == "path"):
			text, err := stringKey()
			if err != nil {
				return out, err
			}
			if key == "name" {
				out.libName = text
			} else {
				out.libPath = text
			}
		case targetKind != "" && (key == "name" || key == "path"):
			text, err := stringKey()
			if err != nil {
				return out, err
			}
			list := out.targets[targetKind]
			if key == "name" {
				list[len(list)-1].name = text
			} else {
				list[len(list)-1].path = text
			}
		}
	}
	return out, nil
}

// readDep reads one assignment inside a dependency table: `key = value` in
// [dependencies], or `field = value` in [dependencies.key].
func (m *manifest) readDep(table, depKey string, key []string, value tomlValue, readable bool) {
	if depKey != "" {
		d := m.dep(table, depKey)
		m.applyField(&d, strings.Join(key, "."), value, readable)
		m.setDep(table, depKey, d)
		return
	}
	name := key[0]
	if len(key) > 1 {
		d := m.dep(table, name)
		m.applyField(&d, strings.Join(key[1:], "."), value, readable)
		m.setDep(table, name, d)
		return
	}
	switch {
	case readable && value.kind == 's':
		m.setDep(table, name, dependency{Source: "registry"})
		return
	case !readable || value.kind != 't':
		m.setDep(table, name, dependency{Source: "unknown"})
		return
	}
	d := dependency{Source: "registry"}
	// git and unknown values win over path, whatever the field order.
	for _, k := range []string{"path", "workspace", "package", "git", "registry", "registry-index"} {
		if v, ok := value.table[k]; ok {
			m.applyField(&d, k, v, true)
		}
	}
	m.setDep(table, name, d)
}

// applyField applies one dependency field; readable is false for a value
// this reader does not read.
func (m *manifest) applyField(d *dependency, field string, value tomlValue, readable bool) {
	isString := readable && value.kind == 's'
	switch field {
	case "path":
		if !isString || value.text == "" {
			d.Source = "unknown"
			return
		}
		if d.Source == "registry" || d.Source == "path" {
			d.Source = "path"
		}
		d.Path = value.text
	case "git":
		if d.Source != "unknown" {
			d.Source = "git"
		}
	case "workspace":
		if readable && value.kind == 'b' && value.text == "true" && d.Source != "unknown" {
			d.Source = "workspace"
		}
	case "package":
		if !isString {
			d.Source = "unknown"
			return
		}
		d.Package = value.text
	case "registry", "registry-index":
		if d.Source == "path" {
			d.Source = "unknown"
		}
	}
}

func (m *manifest) dep(table, name string) dependency {
	if d, ok := m.deps[table+"\x00"+name]; ok {
		return d
	}
	if d, ok := m.workspaceDeps[name]; ok && strings.HasPrefix(table, "workspace.") {
		return d
	}
	return dependency{Source: "registry"}
}

// setDep records a dependency entry; the same key in several tables keeps
// one entry per table.
func (m *manifest) setDep(table, name string, d dependency) {
	if strings.HasPrefix(table, "workspace.") {
		m.workspaceDeps[name] = d
		return
	}
	m.deps[table+"\x00"+name] = d
}

// dependencies returns every dependency entry by key, over the package's
// build-dependencies tables (build) or over all its other dependency
// tables.
func (m *manifest) dependencies(build bool) map[string][]dependency {
	out := map[string][]dependency{}
	for k, d := range m.deps {
		table, name, _ := strings.Cut(k, "\x00")
		if strings.HasPrefix(table, "b\x01") == build {
			out[name] = append(out[name], d)
		}
	}
	return out
}
