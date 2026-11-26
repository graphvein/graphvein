// Package config resolves the logical repository shared by different checkouts.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/graphvein/graphvein/internal/testscope"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config contains only the project settings supported by schema version 1.
type Config struct {
	OpenAPIBasePaths map[string]string
	OpenAPI          testscope.Rules
	Tests            testscope.Rules
	IDE              IDE
	// IDEGiven is true when graphvein.yaml has a nonempty ide setting (an
	// editor name or a mapping with at least one key). IDE is DefaultIDE()
	// either way, so only this flag tells the team's choice from
	// Graphvein's built-in default (ADR 0050).
	IDEGiven bool
	// IDECustom is true when the ide setting has its own link_template:
	// IDE.Editor then names no editor whose format builds the links.
	IDECustom bool
	Egress    Egress
	Clients   Clients
	// Changes holds the change journal limits.
	Changes Changes
	// UI holds the web UI defaults (T025b); a browser's choice wins.
	UI UI
	// Architecture holds the architecture view settings (T026).
	Architecture Architecture
	// Index holds the settings that change link output (route shape
	// call-following depth).
	Index Index
	// Explorer holds the API explorer's user-supplied path values
	// (T029): they affect only the explorer, try-it and the generated
	// OpenAPI document, never the index.
	Explorer Explorer
	// TryIt holds the "try it" environments (T029, ADR 0081).
	TryIt      TryIt
	Version    int
	Repository string
}

// Defaults is the configuration of a repository without graphvein.yaml.
// A project member served without a checkout uses it too (T022 B9).
func Defaults() Config {
	return Config{IDE: DefaultIDE(), Egress: DefaultEgress(), Changes: DefaultChanges(), Index: DefaultIndex()}
}

// Load reads only the supplied root, so the caller's working directory cannot
// accidentally select another project's identity.
func Load(root string) (Config, error) {
	path := filepath.Join(root, File)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return Parse(path, data)
}

// File is the name of the repository configuration file.
const File = "graphvein.yaml"

// Parse validates data as the graphvein.yaml at path, as Load would read
// it. The server's guarded edits (the explorer's path values, try-it
// environments) check an edited file with it before writing (T029).
func Parse(path string, data []byte) (Config, error) {
	return decode(path, bytes.NewReader(data))
}

func decode(path string, f io.Reader) (Config, error) {
	fail := func(line int, message string) (Config, error) {
		if linePrefixed(message) {
			// A section decoder already named the exact line.
			return Config{}, fmt.Errorf("%s: %s", path, message)
		}
		return Config{}, fmt.Errorf("%s: line %d: %s", path, line, message)
	}
	d := yaml.NewDecoder(f)
	var doc yaml.Node
	if err := d.Decode(&doc); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		return fail(extra.Line, "multiple YAML documents are not allowed")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fail(doc.Line, "expected a mapping with version and repository")
	}
	c := Config{IDE: DefaultIDE(), Egress: DefaultEgress(), Changes: DefaultChanges(), Index: DefaultIndex()}
	seen := map[string]bool{}
	m := doc.Content[0]
	for i := 0; i < len(m.Content); i += 2 {
		key, value := m.Content[i], m.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return fail(key.Line, "keys must be strings")
		}
		if seen[key.Value] {
			return fail(key.Line, "duplicate key "+key.Value)
		}
		seen[key.Value] = true
		switch key.Value {
		case "openapi":
			var err error
			c.OpenAPI, c.OpenAPIBasePaths, err = decodeOpenAPI(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "tests":
			var err error
			c.Tests, err = decodeTests(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "clients":
			var err error
			c.Clients, err = decodeClients(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "ide":
			var err error
			c.IDE, err = decodeIDE(value)
			c.IDEGiven = value.Kind != yaml.MappingNode || len(value.Content) > 0
			c.IDECustom = hasKey(value, "link_template")
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "changes":
			var err error
			c.Changes, err = decodeChanges(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "ui":
			var err error
			c.UI, err = decodeUI(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "architecture":
			var err error
			c.Architecture, err = decodeArchitecture(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "index":
			var err error
			c.Index, err = decodeIndex(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "explorer":
			var err error
			c.Explorer, err = decodeExplorer(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "tryit":
			var err error
			c.TryIt, err = decodeTryIt(value)
			var le *lineError
			if errors.As(err, &le) {
				return fail(le.line, le.msg)
			}
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "egress":
			var err error
			c.Egress, err = decodeEgress(value)
			if err != nil {
				return fail(value.Line, err.Error())
			}
		case "version":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Decode(&c.Version) != nil || c.Version != 1 {
				return fail(value.Line, "version must be integer 1")
			}
		case "repository":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" || strings.ContainsRune(value.Value, 0) {
				return fail(value.Line, "repository must be a nonempty string without NUL")
			}
			if strings.Contains(value.Value, RefMarker) {
				return fail(value.Line, "repository must not contain "+RefMarker+" (reserved for ref snapshots)")
			}
			c.Repository = value.Value
		default:
			return fail(key.Line, "unknown key "+key.Value)
		}
	}
	if !seen["version"] {
		return fail(m.Line, "version is required")
	}
	return c, nil
}

// linePrefixed reports whether message starts with "line N: ", as the
// section decoders' messages do, so decode does not name a line twice.
func linePrefixed(message string) bool {
	rest, ok := strings.CutPrefix(message, "line ")
	if !ok {
		return false
	}
	digits := len(rest) - len(strings.TrimLeft(rest, "0123456789"))
	return digits > 0 && strings.HasPrefix(rest[digits:], ": ")
}

// RefMarker is reserved in repository identifiers: ref snapshots are stored
// under "<repository>@ref:<commit>" (ADR 0056). It equals store.RefMarker,
// which a test asserts.
const RefMarker = "@ref:"

// ResolveRepository validates config even when a flag overrides its identity.
// A nil override means no flag was supplied; an explicitly empty flag is invalid.
func ResolveRepository(root string, override *string) (string, error) {
	c, err := Load(root)
	if err != nil {
		return "", err
	}
	id := c.Repository
	if id == "" {
		id = filepath.Base(root)
	}
	if override != nil {
		id = *override
	}
	if id == "" || strings.ContainsRune(id, 0) {
		return "", fmt.Errorf("repository must be nonempty and contain no NUL")
	}
	if strings.Contains(id, RefMarker) {
		return "", fmt.Errorf("repository %q must not contain %s (reserved for ref snapshots)", id, RefMarker)
	}
	return id, nil
}

// ResolveServeRepository is ResolveRepository for graphvein serve, which
// starts even when graphvein.yaml does not load so the Settings page can
// show the decoder's message and fix the file (ADR 0088). loadErr is that
// message (nil when the file loads); err is a failure that stops serve as
// before (an invalid identity, a file that cannot be read).
//
// Without a loading file the identity is, in order: the override, the
// file's own repository key when the YAML parses and the key is a valid
// string, and the directory name only when the YAML parses and sets no
// repository key. A file whose YAML does not parse, or whose repository
// key is not a valid string, proves no identity: err asks for
// --repository instead of guessing one.
func ResolveServeRepository(root string, override *string) (id string, loadErr error, err error) {
	path := filepath.Join(root, File)
	data, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", nil, fmt.Errorf("reading %s: %w", path, rerr)
	}
	if rerr != nil {
		// No file: the defaults load.
		id, err := ResolveRepository(root, override)
		return id, nil, err
	}
	if _, loadErr = Parse(path, data); loadErr == nil {
		id, err := ResolveRepository(root, override)
		return id, nil, err
	}
	if override == nil {
		var ok bool
		if id, ok = lenientRepository(data); !ok {
			return "", loadErr, fmt.Errorf("%w; its repository is not known, so give it with --repository", loadErr)
		}
		if id == "" {
			id = filepath.Base(root)
		}
		override = &id
	}
	id = *override
	if id == "" || strings.ContainsRune(id, 0) {
		return "", loadErr, fmt.Errorf("repository must be nonempty and contain no NUL")
	}
	if strings.Contains(id, RefMarker) {
		return "", loadErr, fmt.Errorf("repository %q must not contain %s (reserved for ref snapshots)", id, RefMarker)
	}
	return id, loadErr, nil
}

// ResolveRegisteredRepository is the identity of a workspace entry's
// folder when graphvein serve opens it from the registry (ADR 0087): the
// repository key of its graphvein.yaml when the file names one, else the
// name the entry was registered under (which may come from serve
// --repository). Like ResolveServeRepository, a file that does not load
// does not stop the entry from opening: loadErr is the decoder's message,
// so the UI gates its views and Settings can fix the file (ADR 0088). When
// such a file's repository key cannot be read, the registered name is
// kept: it is the name the entry's facts were stored under, so nothing is
// guessed. err is a failure that stops the entry opening (a file that
// cannot be read, an invalid identity).
func ResolveRegisteredRepository(root, registered string) (id string, loadErr error, err error) {
	path := filepath.Join(root, File)
	data, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", nil, fmt.Errorf("reading %s: %w", path, rerr)
	}
	id = registered
	if rerr == nil {
		var c Config
		if c, loadErr = Parse(path, data); loadErr == nil {
			if c.Repository != "" {
				id = c.Repository
			}
		} else if key, ok := lenientRepository(data); ok && key != "" {
			id = key
		}
	}
	if id == "" || strings.ContainsRune(id, 0) {
		return "", loadErr, fmt.Errorf("repository must be nonempty and contain no NUL")
	}
	if strings.Contains(id, RefMarker) {
		return "", loadErr, fmt.Errorf("repository %q must not contain %s (reserved for ref snapshots)", id, RefMarker)
	}
	return id, loadErr, nil
}

// lenientRepository reads the repository key of a graphvein.yaml that
// does not load: from the parsed mapping when the YAML parses, else from
// the lines that start a root key (column 0) when exactly one of them is
// `repository:` and that line alone parses. id is "" when the file sets
// no repository key; ok is false when the key is there but not a valid
// string, or cannot be read.
func lenientRepository(data []byte) (id string, ok bool) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) == nil {
		if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
			return "", false
		}
		_, v := mappingEntry(doc.Content[0], "repository")
		if v == nil {
			return "", true
		}
		return v.Value, v.Kind == yaml.ScalarNode && v.Tag == "!!str" && v.Value != ""
	}
	var found []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "repository:") || strings.HasPrefix(line, "repository ") || strings.HasPrefix(line, "'repository'") || strings.HasPrefix(line, `"repository"`) {
			found = append(found, line)
		}
	}
	switch len(found) {
	case 0:
		return "", true
	case 1:
		var m map[string]any
		if yaml.Unmarshal([]byte(found[0]), &m) != nil {
			return "", false
		}
		s, isString := m["repository"].(string)
		return s, isString && s != ""
	}
	return "", false
}

// hasKey reports whether the mapping n has the string key name.
func hasKey(n *yaml.Node, name string) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Kind == yaml.ScalarNode && n.Content[i].Value == name {
			return true
		}
	}
	return false
}
