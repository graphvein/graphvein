package config

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/testscope"
	"go.yaml.in/yaml/v3"
)

// Architecture holds the architecture view settings (T026, O1 (a)).
type Architecture struct {
	// Hide lists patterns, in the tests pattern syntax, matched against a
	// component's directory: a matching component is hidden by default
	// (row CV4), like the closed list of docs-site generators (CV2) hides
	// docs sites. Nothing else is hidden by configuration.
	Hide testscope.Rules
	// HideGiven is true when at least one pattern is configured.
	HideGiven bool
}

// decodeArchitecture accepts `architecture: {hide: [patterns]}`. Unknown keys
// are an error, like every other section.
func decodeArchitecture(n *yaml.Node) (Architecture, error) {
	var a Architecture
	if n.Kind != yaml.MappingNode {
		return a, fmt.Errorf("architecture must be a mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return a, fmt.Errorf("line %d: architecture keys must be strings", k.Line)
		}
		if seen[k.Value] {
			return a, fmt.Errorf("line %d: duplicate architecture key %s", k.Line, k.Value)
		}
		seen[k.Value] = true
		if k.Value != "hide" {
			return a, fmt.Errorf("line %d: unknown architecture key %s", k.Line, k.Value)
		}
		if v.Kind != yaml.SequenceNode {
			return a, fmt.Errorf("line %d: architecture.hide must be a list", v.Line)
		}
		var patterns []string
		for _, item := range v.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return a, fmt.Errorf("line %d: architecture.hide patterns must be strings", item.Line)
			}
			if _, err := testscope.Compile([]string{item.Value}, nil); err != nil {
				return a, fmt.Errorf("line %d: %w", item.Line, err)
			}
			patterns = append(patterns, item.Value)
		}
		rules, err := testscope.Compile(patterns, nil)
		if err != nil {
			return a, err
		}
		a.Hide, a.HideGiven = rules, len(patterns) > 0
	}
	return a, nil
}
