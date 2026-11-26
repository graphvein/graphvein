package config

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Explorer holds the API explorer settings of graphvein.yaml (T029, ADR
// 0081). They change only what the explorer, try-it and the generated
// OpenAPI document show; the index, the API section's other views and
// every link ignore them.
type Explorer struct {
	// PathValues are path parts Graphvein could not read from the code
	// and the user stated: a route's whole path, or a Next.js basePath
	// read from the environment. Each is marked "set by you" wherever it
	// is used, never shown as read from code.
	PathValues []PathValue
}

// PathValue is one user-supplied path part.
type PathValue struct {
	// Key names what the value stands for, in a form that survives
	// re-indexing (rows OG-90..OG-93 of
	// docs/contract/claims/openapi-generation.md):
	//   - a route identity ("route:app/api.py#GET%20%2Fitems"): the whole
	//     path template of a route stored without a usable path;
	//   - "nextjs_base_path:<project directory>": the basePath of the
	//     Next.js project in that directory ("." for the repository
	//     root), put before every route template of the project;
	//   - a mount hop ("mount:<file>#<function> <hop>"): the prefix an
	//     unresolved mount, prefix or wrapper passes requests on with,
	//     put before the path of every route stored below the hop.
	Key string
	// Value is the path template (route key), base path (basePath key)
	// or prefix (mount key; "/" passes the path on unchanged).
	Value string
	// Registration is the stored content hash of the route's registration
	// when the value was set (route keys only): an identity can move to
	// another registration of the same file when one with the same method
	// and pattern is added or removed above it, so the generator uses the
	// value only while the hash still matches (row OG-90). Empty when not
	// recorded.
	Registration string
	// Line is the line of the entry in graphvein.yaml.
	Line int
}

// Key prefixes of explorer.path_values.
const (
	// PathValueRoutePrefix starts a route identity key.
	PathValueRoutePrefix = "route:"
	// PathValueNextBasePrefix starts a Next.js basePath key.
	PathValueNextBasePrefix = "nextjs_base_path:"
	// PathValueMountPrefix starts an unresolved mount hop key.
	PathValueMountPrefix = "mount:"
	// MaxPathValues bounds explorer.path_values.
	MaxPathValues = 2000
	// maxPathValueLen bounds a key and a value.
	maxPathValueLen = 2048
)

// decodeExplorer accepts `explorer: {path_values: [{key: K, value: V}]}`.
// Unknown keys, duplicate keys and invalid values are errors, like every
// other section.
func decodeExplorer(n *yaml.Node) (Explorer, error) {
	var e Explorer
	if n.Kind != yaml.MappingNode {
		return e, fmt.Errorf("explorer must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return e, fmt.Errorf("line %d: explorer keys must be strings", k.Line)
		}
		if seen[k.Value] {
			return e, fmt.Errorf("line %d: duplicate explorer key %s", k.Line, k.Value)
		}
		seen[k.Value] = true
		if k.Value != "path_values" {
			return e, fmt.Errorf("line %d: unknown explorer key %s", k.Line, k.Value)
		}
		values, err := decodePathValues(v)
		if err != nil {
			return e, err
		}
		e.PathValues = values
	}
	return e, nil
}

func decodePathValues(n *yaml.Node) ([]PathValue, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: explorer.path_values must be a list", n.Line)
	}
	if len(n.Content) > MaxPathValues {
		return nil, fmt.Errorf("line %d: explorer.path_values has more than %d entries", n.Line, MaxPathValues)
	}
	var out []PathValue
	keys := map[string]int{}
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d: an explorer.path_values entry is a mapping with key and value", item.Line)
		}
		pv := PathValue{Line: item.Line}
		got := map[string]bool{}
		for j := 0; j < len(item.Content); j += 2 {
			k, v := item.Content[j], item.Content[j+1]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, fmt.Errorf("line %d: explorer.path_values keys must be strings", k.Line)
			}
			if got[k.Value] {
				return nil, fmt.Errorf("line %d: duplicate key %s in an explorer.path_values entry", k.Line, k.Value)
			}
			got[k.Value] = true
			if k.Value != "key" && k.Value != "value" && k.Value != "registration" {
				return nil, fmt.Errorf("line %d: unknown explorer.path_values key %s (key, value and registration only)", k.Line, k.Value)
			}
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
				return nil, fmt.Errorf("line %d: explorer.path_values %s must be a string", v.Line, k.Value)
			}
			switch k.Value {
			case "key":
				pv.Key = v.Value
			case "value":
				pv.Value = v.Value
			default:
				pv.Registration = v.Value
			}
		}
		if !got["key"] || !got["value"] {
			return nil, fmt.Errorf("line %d: an explorer.path_values entry needs key and value", item.Line)
		}
		if got["registration"] && !RegistrationHash(pv.Registration) {
			return nil, fmt.Errorf("line %d: explorer.path_values registration must be a sha256:<64 hex digits> content hash", item.Line)
		}
		if got["registration"] && !strings.HasPrefix(pv.Key, PathValueRoutePrefix) && !strings.HasPrefix(pv.Key, PathValueMountPrefix) {
			return nil, fmt.Errorf("line %d: explorer.path_values registration belongs to a route or mount key only", item.Line)
		}
		if err := CheckPathValue(pv.Key, pv.Value); err != nil {
			return nil, fmt.Errorf("line %d: %v", item.Line, err)
		}
		if prev, dup := keys[pv.Key]; dup {
			return nil, fmt.Errorf("line %d: explorer.path_values key %s is already set on line %d", item.Line, pv.Key, prev)
		}
		keys[pv.Key] = item.Line
		out = append(out, pv)
	}
	return out, nil
}

// CheckPathValue checks one explorer.path_values entry: a key of a known
// form and a value that is a path the generated document can carry. The
// server checks an edit from the UI with it before building the new file.
func CheckPathValue(key, value string) error {
	if len(key) > maxPathValueLen || len(value) > maxPathValueLen {
		return fmt.Errorf("explorer.path_values key and value are at most %d bytes", maxPathValueLen)
	}
	switch {
	case strings.HasPrefix(key, PathValueRoutePrefix) && len(key) > len(PathValueRoutePrefix) && plainText(key):
		if err := checkPathText(value, true); err != nil {
			return fmt.Errorf("explorer.path_values value for %s: %v", key, err)
		}
	case strings.HasPrefix(key, PathValueNextBasePrefix) && len(key) > len(PathValueNextBasePrefix) && plainText(key):
		if value == "" {
			return nil
		}
		if err := checkPathText(value, false); err != nil {
			return fmt.Errorf("explorer.path_values value for %s: %v", key, err)
		}
		if strings.HasSuffix(value, "/") {
			return fmt.Errorf("explorer.path_values value for %s: a basePath does not end with / (Next.js refuses it)", key)
		}
	case strings.HasPrefix(key, PathValueMountPrefix) && len(key) > len(PathValueMountPrefix) && plainText(key):
		if err := checkPathText(value, true); err != nil {
			return fmt.Errorf("explorer.path_values value for %s: %v", key, err)
		}
		if value != "/" && strings.HasSuffix(value, "/") {
			return fmt.Errorf("explorer.path_values value for %s: a prefix does not end with / (use / for none)", key)
		}
		if strings.Contains(value, "//") || strings.Contains(value+"/", "/./") || strings.Contains(value+"/", "/../") {
			return fmt.Errorf("explorer.path_values value for %s: a prefix has no empty, . or .. segment", key)
		}
	default:
		return fmt.Errorf("explorer.path_values key %q must be a route identity (route:...), nextjs_base_path:<project directory> or a mount hop (mount:...)", key)
	}
	return nil
}

// plainText reports whether s holds no control character or space.
func plainText(s string) bool {
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// checkPathText checks a path: it starts with /, holds no query, fragment,
// space or control character, and, when templates are allowed, every brace
// is one {name} parameter whose name holds no /, {, }, * or :; otherwise no
// brace at all.
func checkPathText(value string, template bool) error {
	if !strings.HasPrefix(value, "/") {
		return fmt.Errorf("%q must start with /", value)
	}
	if !plainText(value) || strings.ContainsAny(value, "?#") {
		return fmt.Errorf("%q must not hold spaces, control characters, ? or #", value)
	}
	if !template {
		if strings.ContainsAny(value, "{}") {
			return fmt.Errorf("%q must not hold { or }", value)
		}
		return nil
	}
	rest := value
	for {
		open := strings.IndexAny(rest, "{}")
		if open < 0 {
			return nil
		}
		if rest[open] == '}' {
			return fmt.Errorf("%q has a } without {", value)
		}
		end := strings.IndexAny(rest[open+1:], "{}")
		if end < 0 || rest[open+1+end] != '}' {
			return fmt.Errorf("%q has a { without its }", value)
		}
		name := rest[open+1 : open+1+end]
		if name == "" || strings.ContainsAny(name, "/*:") {
			return fmt.Errorf("%q: parameter {%s} must be a name without /, * or :", value, name)
		}
		rest = rest[open+1+end+1:]
	}
}

// RegistrationHash reports whether s is a content hash as route nodes
// store it: "sha256:" and 64 lower-case hexadecimal digits.
func RegistrationHash(s string) bool {
	hex, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// PathValueFor returns the value set for key and whether one is set.
func (e Explorer) PathValueFor(key string) (string, bool) {
	for _, pv := range e.PathValues {
		if pv.Key == key {
			return pv.Value, true
		}
	}
	return "", false
}
