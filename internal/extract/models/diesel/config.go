package diesel

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/cargotoml"
)

// Config is one diesel.toml as diesel_cli 2.3.14 reads it
// (config.rs:14-21, :403-477, :553-563).
type Config struct {
	// Path is the repository-relative diesel.toml; Dir its directory,
	// against which relative paths resolve (config.rs:48-63).
	Path, Dir string
	// Problem is set when diesel_cli would refuse the whole file (a TOML
	// error, an unknown key under deny_unknown_fields, a value of the
	// wrong type): no section of it is read (row DI-K1).
	Problem string
	// Sections are the [print_schema] configurations by key ("default"
	// for the section-less form), sorted by key.
	Sections []*Section
	// Migrations is the migrations directory ([migrations_directory]
	// dir, else migrations beside the diesel.toml; main.rs:116,
	// :215-218), repository-relative; "" when it cannot be placed in the
	// repository. MigrationsWritten is true when the key is written.
	Migrations        string
	MigrationsWritten bool
}

// Section is one print-schema configuration.
type Section struct {
	// Key is "default" or the <name> of [print_schema.<name>].
	Key string
	// Written is false for the implicit default of a file with named
	// sections and no section-less keys, or with no [print_schema].
	Written bool
	// File is the literal `file` value; Path the repository-relative
	// file it names ("" when it cannot be placed in the repository).
	File, Path string
	// PatchFile and PatchPath are `patch_file`, the same way.
	PatchFile, PatchPath string
	// Schema is the literal `schema`, nil when absent.
	Schema *string
	// Filter is true when `filter` sets only_tables or except_tables.
	Filter bool
	// ColumnSorting is the literal column_sorting ("" when absent: the
	// ordinal position).
	ColumnSorting string
	// ImportTypes is import_types, nil when absent.
	ImportTypes []string
	// Keys are the keys written in the section, sorted.
	Keys []string
	// Problem is set when the section yields no group (it is not a
	// table).
	Problem string
}

// printSchemaKeys are the PrintSchema fields (config.rs:449-477) and the
// shape each value must have for diesel_cli to accept the file.
var printSchemaKeys = map[string]string{
	"file":       "string",
	"patch_file": "string",
	"schema":     "string",
	// DocConfig accepts a boolean or a string (print_schema.rs:27-32).
	"with_docs":                                   "bool-or-string",
	"column_sorting":                              "string",
	"filter":                                      "table",
	"import_types":                                "strings",
	"generate_missing_sql_type_definitions":       "bool",
	"except_custom_type_definitions":              "strings",
	"custom_type_derives":                         "strings",
	"sqlite_integer_primary_key_is_bigint":        "bool",
	"pg_domains_as_custom_types":                  "strings",
	"allow_tables_to_appear_in_same_query_config": "string",
}

// ReadConfig reads a diesel.toml from its source. It never fails: a file
// diesel_cli would refuse has Problem set and no sections.
func ReadConfig(file string, source []byte) *Config {
	c := &Config{Path: file, Dir: path.Dir(file)}
	root, err := cargotoml.Parse(string(source))
	if err != nil {
		var pe *cargotoml.ParseError
		if errors.As(err, &pe) {
			c.Problem = fmt.Sprintf("the file could not be read (diesel.toml line %d: %s)", pe.Line, pe.Message)
		} else {
			c.Problem = "the file could not be read (" + err.Error() + ")"
		}
		return c
	}
	for _, k := range sortedKeys(root) {
		if k != "print_schema" && k != "migrations_directory" {
			c.Problem = fmt.Sprintf("unknown key %q: diesel_cli refuses the file (deny_unknown_fields, config.rs:15)", k)
			return c
		}
	}
	c.Migrations = path.Join(c.Dir, "migrations")
	if raw, ok := root["migrations_directory"]; ok {
		md, ok := raw.(cargotoml.Table)
		if !ok {
			c.Problem = "migrations_directory is not a table"
			return c
		}
		for _, k := range sortedKeys(md) {
			if k != "dir" {
				c.Problem = fmt.Sprintf("unknown key migrations_directory.%s: diesel_cli refuses the file (config.rs:553-557)", k)
				return c
			}
		}
		dir, ok := md["dir"].(string)
		if !ok {
			c.Problem = "migrations_directory.dir is missing or not a string"
			return c
		}
		c.MigrationsWritten = true
		c.Migrations = resolve(c.Dir, dir)
	}
	ps := cargotoml.Table{}
	if raw, ok := root["print_schema"]; ok {
		t, ok := raw.(cargotoml.Table)
		if !ok {
			c.Problem = "print_schema is not a table"
			return c
		}
		ps = t
	}
	// RootPrintSchema (config.rs:403-437): the PrintSchema fields written
	// directly under [print_schema] are the default configuration; every
	// other key is a named configuration, and a named "default" replaces
	// the direct fields.
	direct := cargotoml.Table{}
	named := map[string]any{}
	for k, v := range ps {
		if _, field := printSchemaKeys[k]; field {
			direct[k] = v
		} else {
			named[k] = v
		}
	}
	var sections []*Section
	if _, replaced := named["default"]; !replaced {
		s, problem := readSection("default", "print_schema", direct, c.Dir)
		if problem != "" {
			c.Problem = problem
			return c
		}
		s.Written = len(direct) > 0
		sections = append(sections, s)
	}
	for _, k := range sortedKeys(named) {
		t, ok := named[k].(cargotoml.Table)
		if !ok {
			sections = append(sections, &Section{Key: k, Written: true, Problem: fmt.Sprintf("print_schema.%s is not a table", k)})
			continue
		}
		s, problem := readSection(k, "print_schema."+k, t, c.Dir)
		if problem != "" {
			c.Problem = problem
			return c
		}
		s.Written = true
		sections = append(sections, s)
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Key < sections[j].Key })
	c.Sections = sections
	return c
}

// readSection checks one PrintSchema table; a problem refuses the file.
// name is the table as written ("print_schema" or "print_schema.<key>").
func readSection(key, name string, t cargotoml.Table, dir string) (*Section, string) {
	s := &Section{Key: key, Keys: sortedKeys(t)}
	for _, k := range s.Keys {
		shape, known := printSchemaKeys[k]
		if !known {
			return nil, fmt.Sprintf("unknown key %s.%s: diesel_cli refuses the file (deny_unknown_fields, config.rs:449)", name, k)
		}
		if !hasShape(t[k], shape) {
			return nil, fmt.Sprintf("%s.%s is not a %s", name, k, strings.ReplaceAll(shape, "-", " "))
		}
	}
	if v, ok := t["file"].(string); ok {
		s.File, s.Path = v, resolve(dir, v)
	}
	if v, ok := t["patch_file"].(string); ok {
		s.PatchFile, s.PatchPath = v, resolve(dir, v)
	}
	if v, ok := t["schema"].(string); ok {
		s.Schema = &v
	}
	if v, ok := t["column_sorting"].(string); ok {
		s.ColumnSorting = v
	}
	if f, ok := t["filter"].(cargotoml.Table); ok {
		for _, k := range sortedKeys(f) {
			if k != "only_tables" && k != "except_tables" || !hasShape(f[k], "strings") {
				return nil, fmt.Sprintf("%s.filter is not only_tables or except_tables (config.rs:587-635)", name)
			}
		}
		if len(f) > 1 {
			return nil, fmt.Sprintf("%s.filter sets both only_tables and except_tables (config.rs:633)", name)
		}
		s.Filter = len(f) == 1
	}
	if list, ok := t["import_types"].([]any); ok {
		s.ImportTypes = []string{}
		for _, v := range list {
			s.ImportTypes = append(s.ImportTypes, v.(string))
		}
	}
	return s, ""
}

func hasShape(v any, shape string) bool {
	switch shape {
	case "string":
		_, ok := v.(string)
		return ok
	case "bool":
		return v == cargotoml.Scalar("true") || v == cargotoml.Scalar("false")
	case "bool-or-string":
		_, ok := v.(string)
		return ok || v == cargotoml.Scalar("true") || v == cargotoml.Scalar("false")
	case "table":
		_, ok := v.(cargotoml.Table)
		return ok
	case "strings":
		list, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range list {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// resolve places a diesel.toml path in the repository: relative to the
// file's directory; "" for an absolute path, a Windows path, or one that
// leaves the repository.
func resolve(dir, value string) string {
	if value == "" || path.IsAbs(value) || strings.Contains(value, `\`) || len(value) > 1 && value[1] == ':' {
		return ""
	}
	p := path.Join(dir, value)
	if p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
