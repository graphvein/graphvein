package config

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"go.yaml.in/yaml/v3"
)

// Clients supplies explicit bases only where source syntax cannot resolve
// them, and the opt-in that links committed bundler output (ADR 0085).
type Clients struct {
	BasePaths map[string]string
	// LinkGeneratedOutput (clients.link_generated_output) links the client
	// calls of committed bundler output, which are not linked by default.
	LinkGeneratedOutput bool
}

func decodeClients(n *yaml.Node) (Clients, error) {
	c := Clients{BasePaths: map[string]string{}}
	if n.Kind != yaml.MappingNode {
		return c, fmt.Errorf("clients must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value != "base_paths" && k.Value != "link_generated_output" {
			return c, fmt.Errorf("unknown clients key %s", k.Value)
		}
		if seen[k.Value] {
			return c, fmt.Errorf("duplicate clients key %s", k.Value)
		}
		seen[k.Value] = true
		if k.Value == "link_generated_output" {
			if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" || v.Decode(&c.LinkGeneratedOutput) != nil {
				return c, fmt.Errorf("clients.link_generated_output must be true or false")
			}
			continue
		}
		if v.Kind != yaml.MappingNode {
			return c, fmt.Errorf("clients.base_paths must be a mapping")
		}
		for j := 0; j < len(v.Content); j += 2 {
			dir, base := v.Content[j], v.Content[j+1]
			if dir.Kind != yaml.ScalarNode || dir.Tag != "!!str" || dir.Value != "." && !contract.ValidPath(dir.Value, false) {
				return c, fmt.Errorf("invalid clients directory %q", dir.Value)
			}
			if _, ok := c.BasePaths[dir.Value]; ok {
				return c, fmt.Errorf("duplicate clients directory %s", dir.Value)
			}
			if base.Kind != yaml.ScalarNode || base.Tag != "!!str" || base.Value != "" && !strings.HasPrefix(base.Value, "/") || strings.ContainsAny(base.Value, "\x00\r\n") {
				return c, fmt.Errorf("clients base path must be a string starting with /, or empty")
			}
			c.BasePaths[dir.Value] = base.Value
		}
	}
	return c, nil
}
