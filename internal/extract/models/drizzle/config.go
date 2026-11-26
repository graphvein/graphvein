package drizzle

import (
	"encoding/json"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// A drizzle-kit config is `export default defineConfig({...})` or `export
// default {...}`, either possibly wrapped in `satisfies Config` or `as
// Config` (transparent in the payload), or a drizzle.config.json object.
// It is read, never run. Keys read when literal: dialect, schema, casing,
// schemaFilter; out, dbCredentials, migrations, strict and verbose are
// not facts.

// Dialects drizzle-kit accepts. postgresql, sqlite, turso (SQLite rules)
// and mysql are read; singlestore and gel go to D.
var (
	readDialects = map[string]bool{"postgresql": true, "sqlite": true, "turso": true, "mysql": true}
	dDialects    = map[string]bool{"singlestore": true, "gel": true}
)

// Casing values drizzle-kit accepts (drizzle-orm casing.js).
var casings = map[string]bool{"snake_case": true, "camelCase": true}

// configKeys holds a config's keys as reduced values: a literal string, a
// list of literal strings, or neither (non-literal).
type configValue struct {
	// Strings holds the value's literal strings; List is true for an
	// array. Literal is false for any other value.
	Strings []string
	List    bool
	Literal bool
}

// configObject is a config's top-level keys. Unread is set when the
// object cannot be read whole (a spread or a computed key could set any
// key).
type configObject struct {
	Keys   map[string]configValue
	Unread string
}

// readConfigPayload reads a TypeScript or JavaScript config's default
// export from its payload. f is the file's facts, for the defineConfig
// import.
func readConfigPayload(m *typescript.ModelSyntax, f typescript.Facts) configObject {
	if m == nil || m.Default == nil {
		return configObject{Unread: "it has no `export default` the model payload captured"}
	}
	e := m.Default.Value
	if e.Kind == typescript.ModelCall && e.Callee.Kind == typescript.ModelIdentifier && importedFrom(f, e.Callee.Name, "drizzle-kit") == "defineConfig" {
		if len(e.Args) != 1 {
			return configObject{Unread: "defineConfig is not called with one argument"}
		}
		e = e.Args[0]
	}
	if e.Kind != typescript.ModelObject {
		return configObject{Unread: "its default export is not `defineConfig({...})` or an object literal"}
	}
	obj := configObject{Keys: map[string]configValue{}}
	for _, en := range e.Entries {
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand, typescript.ModelMethod:
			obj.Keys[en.Key] = exprValue(en)
		default:
			return configObject{Unread: "its config object has a spread or a computed key"}
		}
	}
	return obj
}

func exprValue(en typescript.ModelEntry) configValue {
	if en.Kind != typescript.ModelProperty {
		return configValue{}
	}
	if s, ok := literalString(en.Value); ok {
		return configValue{Strings: []string{s}, Literal: true}
	}
	if en.Value.Kind == typescript.ModelArray {
		v := configValue{List: true, Literal: true, Strings: []string{}}
		for _, el := range en.Value.Elements {
			s, ok := literalString(el)
			if !ok {
				return configValue{}
			}
			v.Strings = append(v.Strings, s)
		}
		return v
	}
	return configValue{}
}

// literalString is a string literal or a template without substitutions.
func literalString(e typescript.ModelExpr) (string, bool) {
	switch {
	case e.Kind == typescript.ModelString:
		return e.Value, true
	case e.Kind == typescript.ModelTemplate && len(e.Substitutions) == 0 && len(e.Strings) == 1:
		return e.Strings[0], true
	}
	return "", false
}

// importedFrom returns the name a local identifier imports from module,
// or "".
func importedFrom(f typescript.Facts, local, module string) string {
	for _, imp := range f.Imports {
		if imp.Module == module {
			if name, ok := imp.Bindings[local]; ok {
				return name
			}
		}
	}
	return ""
}

// readConfigJSON reads a drizzle.config.json object.
func readConfigJSON(data []byte) configObject {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return configObject{Unread: "it is not a JSON object"}
	}
	obj := configObject{Keys: map[string]configValue{}}
	for k, v := range raw {
		var s string
		var list []string
		switch {
		case json.Unmarshal(v, &s) == nil:
			obj.Keys[k] = configValue{Strings: []string{s}, Literal: true}
		case json.Unmarshal(v, &list) == nil && list != nil:
			obj.Keys[k] = configValue{Strings: list, List: true, Literal: true}
		default:
			obj.Keys[k] = configValue{}
		}
	}
	return obj
}

// apply reads the config keys into c; it sets c.Reason when the config is
// unproven or goes to D.
func (obj configObject) apply(c *Config, tag func(string)) {
	if obj.Unread != "" {
		c.Reason = c.Path + " cannot be read: " + obj.Unread
		return
	}
	d, ok := obj.Keys["dialect"]
	switch {
	case !ok:
		c.Reason = c.Path + " has no dialect"
		return
	case !d.Literal || d.List:
		c.Reason = "the dialect of " + c.Path + " is not a string literal"
		return
	case dDialects[d.Strings[0]]:
		c.Dialect = d.Strings[0]
		c.Unsupported = true
		c.Reason = "dialect " + quote(d.Strings[0]) + " of " + c.Path + " is not read (row D)"
		return
	case !readDialects[d.Strings[0]]:
		c.Reason = "dialect " + quote(d.Strings[0]) + " of " + c.Path + " is not a drizzle-kit dialect"
		return
	}
	c.Dialect = d.Strings[0]
	tag("DR-K1/literal `dialect`")
	s, ok := obj.Keys["schema"]
	switch {
	case !ok:
		c.Reason = c.Path + " has no schema"
		return
	case !s.Literal:
		c.Reason = "the schema of " + c.Path + " is not a string literal or an array of string literals"
		return
	}
	c.Schema = s.Strings
	if s.List {
		tag("DR-K1/array")
	}
	c.CasingKnown = true
	if v, ok := obj.Keys["casing"]; ok {
		tag("DR-K1/`casing`")
		if v.Literal && !v.List && casings[v.Strings[0]] {
			c.Casing = v.Strings[0]
		} else {
			c.CasingKnown = false
		}
	}
	c.SchemaFilterKnown = true
	if v, ok := obj.Keys["schemaFilter"]; ok {
		tag("DR-K1/`schemaFilter`")
		if v.Literal {
			c.SchemaFilter = v.Strings
		} else {
			c.SchemaFilterKnown = false
		}
	}
}

// flagObject turns a drizzle-kit command's literal flags into config keys
// (a script with --schema and --dialect and no config).
func flagObject(flags map[string]string) configObject {
	obj := configObject{Keys: map[string]configValue{}}
	for k, v := range flags {
		obj.Keys[k] = configValue{Strings: []string{v}, Literal: true}
	}
	return obj
}

// isConfigName reports a drizzle-kit config file name (drizzle.config.*).
func isConfigName(base string) bool {
	return strings.HasPrefix(base, "drizzle.config.")
}
