package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Output formats (row OG-52).
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
)

// Encode writes doc as JSON (two-space indent, LF, final newline, HTML
// characters not escaped) or YAML (the same document, keys in the same
// order). The bytes depend only on doc, so one store state always gives
// the same document.
func Encode(doc *Document, format string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding the OpenAPI document: %w", err)
	}
	switch format {
	case FormatJSON, "":
		return buf.Bytes(), nil
	case FormatYAML:
		return toYAML(buf.Bytes())
	}
	return nil, fmt.Errorf("unknown format %q (json or yaml)", format)
}

// toYAML re-encodes JSON as YAML. yaml.v3 reads JSON (a YAML subset) into
// nodes that keep key order; clearing the JSON quoting and flow styles lets
// the encoder write block YAML, quoting a string only where a plain scalar
// would read back as another type ("200", "true", "null").
func toYAML(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("converting the OpenAPI document to YAML: %w", err)
	}
	var plain func(n *yaml.Node)
	plain = func(n *yaml.Node) {
		n.Style = 0
		for _, c := range n.Content {
			plain(c)
		}
	}
	plain(&doc)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("converting the OpenAPI document to YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("converting the OpenAPI document to YAML: %w", err)
	}
	return out.Bytes(), nil
}
