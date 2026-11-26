package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/query"
)

// TestCLIConfidenceText checks that the human-readable CLI output writes a
// confidence the way the web UI and the server do (contract.ConfidenceText,
// T041): a whole number as "1.0", never "1", a fraction in its shortest
// exact form.
func TestCLIConfidenceText(t *testing.T) {
	one, frac := 1.0, 0.875
	t.Run("changes impact", func(t *testing.T) {
		var b bytes.Buffer
		printChanges(&b, changes.Report{Impacts: []changes.NamedImpact{{Seed: "s", Impact: &changes.ImpactResult{Depth: 1, Paths: []changes.Path{
			{Node: "a", Distance: 1, Relation: "I1", Claim: "depends on", Wording: "calls", Provenance: "fact", Confidence: &one},
			{Node: "b", Distance: 1, Relation: "I1", Claim: "may depend on", Wording: "calls", Provenance: "inferred", Confidence: &frac},
		}}}}})
		for _, want := range []string{"fact confidence=1.0\n", "inferred confidence=0.875\n"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, b.String())
			}
		}
	})
	t.Run("project query", func(t *testing.T) {
		var b bytes.Buffer
		node := query.ProjectNode{Repository: "web", Node: query.Node{Node: contract.Node{Identity: "n"}}}
		printProjectQuery(&b, "callers", query.ProjectResponse{Project: "p", Results: []query.ProjectResult{{
			Node: node, Distance: 1, Provenance: "inferred", Confidence: &one,
			Path: []query.ProjectStep{{Edge: contract.Edge{Kind: "calls", Source: "a", Target: "n", Provenance: "inferred", Confidence: 1}, SourceRepository: "web", TargetRepository: "web"}},
		}}})
		for _, want := range []string{"1 web n inferred confidence=1.0\n", "calls web a -> web n inferred confidence=1.0\n"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, b.String())
			}
		}
		if strings.Contains(b.String(), "confidence=1\n") {
			t.Errorf("output writes a whole confidence as \"1\":\n%s", b.String())
		}
	})
}
