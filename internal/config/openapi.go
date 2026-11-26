package config

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/testscope"
	"go.yaml.in/yaml/v3"
)

func decodeOpenAPI(n *yaml.Node) (testscope.Rules, map[string]string, error) {
	if n.Kind != yaml.MappingNode {
		return testscope.Rules{}, nil, fmt.Errorf("openapi must be a mapping")
	}
	patterns := *n
	patterns.Content = nil
	bases := map[string]string{}
	seenBase := false
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Tag == "!!str" && k.Value == "base_paths" {
			if seenBase {
				return testscope.Rules{}, nil, fmt.Errorf("duplicate openapi key base_paths")
			}
			seenBase = true
			// Reuse clients' strict map decoder, including alias and path checks.
			mapping := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{k, v}}
			decoded, err := decodeClients(mapping)
			if err != nil {
				return testscope.Rules{}, nil, fmt.Errorf("openapi.base_paths: %w", err)
			}
			bases = decoded.BasePaths
		} else {
			patterns.Content = append(patterns.Content, k, v)
		}
	}
	rules, err := decodePatterns(&patterns, "openapi")
	return rules, bases, err
}
