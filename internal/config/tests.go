package config

import (
	"fmt"
	"github.com/graphvein/graphvein/internal/testscope"
	"go.yaml.in/yaml/v3"
)

func decodeTests(n *yaml.Node) (testscope.Rules, error) {
	return decodePatterns(n, "tests")
}

func decodePatterns(n *yaml.Node, section string) (testscope.Rules, error) {
	if n.Kind != yaml.MappingNode {
		return testscope.Rules{}, fmt.Errorf("%s must be a mapping", section)
	}
	seen := map[string]bool{}
	var include, exclude []string
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value != "include" && k.Value != "exclude" {
			return testscope.Rules{}, fmt.Errorf("line %d: unknown %s key %s", k.Line, section, k.Value)
		}
		if seen[k.Value] {
			return testscope.Rules{}, fmt.Errorf("line %d: duplicate %s key %s", k.Line, section, k.Value)
		}
		seen[k.Value] = true
		if v.Kind != yaml.SequenceNode {
			return testscope.Rules{}, fmt.Errorf("line %d: %s.%s must be a list", v.Line, section, k.Value)
		}
		for _, item := range v.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return testscope.Rules{}, fmt.Errorf("line %d: %s.%s patterns must be strings", item.Line, section, k.Value)
			}
			if _, err := testscope.Compile([]string{item.Value}, nil); err != nil {
				return testscope.Rules{}, fmt.Errorf("line %d: %w", item.Line, err)
			}
			if k.Value == "include" {
				include = append(include, item.Value)
			} else {
				exclude = append(exclude, item.Value)
			}
		}
	}
	return testscope.Compile(include, exclude)
}
