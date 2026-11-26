package config

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Egress holds the network mode. It is kept as a plain string so config does
// not depend on the egress package; egress.ParseMode accepts exactly the same
// names, which a test asserts.
type Egress struct {
	Mode string
}

// DefaultEgress is offline: absent configuration never grants network access.
func DefaultEgress() Egress { return Egress{Mode: "offline"} }

func decodeEgress(n *yaml.Node) (Egress, error) {
	c := DefaultEgress()
	if n.Kind != yaml.MappingNode {
		return c, fmt.Errorf("egress must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
			return c, fmt.Errorf("invalid or duplicate egress key")
		}
		seen[k.Value] = true
		switch k.Value {
		case "mode":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || (v.Value != "offline" && v.Value != "llm") {
				return c, fmt.Errorf("egress mode must be the string offline or llm")
			}
			c.Mode = v.Value
		default:
			return c, fmt.Errorf("unknown egress key %s", k.Value)
		}
	}
	return c, nil
}
