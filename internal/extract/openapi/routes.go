package openapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
)

// LinkOptions supplies current route proofs and deployment overrides at link time,
// so configuration edits do not invalidate language syntax caches.
type LinkOptions struct {
	Matcher   clients.RouteMatcher
	BasePaths map[string]string
	// Routes are the route nodes the matcher links to. When ShapeDepth is
	// nonzero, each route linked by exactly one documents_route edge gets
	// the committed operation's mapped shape items on its shape attribute
	// (T029 SH-O, docs/contract/claims/route-shape-spec.md); ShapeDepth is
	// the run's call-following depth limit, stored on a shape created here.
	Routes     []contract.Node
	ShapeDepth int
}

func (p *pass) linkOperation(n contract.Node) {
	paths := n.Attributes["paths"].([]map[string]any)
	base, configured := "", false
	keyLength := -1
	for key, value := range p.options.BasePaths {
		length := len(key)
		if key == "." {
			length = 0
		}
		if (key == "." || n.File == key || strings.HasPrefix(n.File, key+"/")) && length > keyLength {
			base, configured, keyLength = value, true, length
		}
	}
	if configured {
		paths = []map[string]any{pathEntry(base, n.Attributes["path"].(string), []string{}, "configured")}
		n.Attributes["paths"], n.Attributes["path_count"] = paths, 1
	}
	scope, _ := n.Attributes["scope"].(string)
	status := "linked"
	rank := map[string]int{"linked": 0, "ambiguous": 1, "test_only": 2, "unmatched": 3, "unresolved": 4}
	byTarget := map[string]contract.Edge{}
	for _, entry := range paths {
		entry["status"], entry["match"] = "unresolved", nil
		entry["candidates"], entry["candidate_count"] = []string{}, 0
		if template, ok := entry["template"].(string); ok {
			// Row LF-1: the spec's servers are not proven to be this
			// repository's server, so a literal-free route is no candidate.
			origins, _ := entry["origins"].([]string)
			result := p.options.Matcher.Exact(clients.ExactRequest{Method: n.Attributes["method"].(string), Template: template, Scope: scope, TrailingSlash: entry["trailing_slash"] == true, UnprovenOrigin: len(origins) > 0})
			entry["status"] = result.Status
			entry["candidates"], entry["candidate_count"] = result.Candidates, result.CandidateCount
			if result.Match != "" {
				entry["match"] = result.Match
			}
			if result.Status == "linked" {
				confidence := 0.9
				if len(entry["origins"].([]string)) > 0 {
					confidence = 0.7
				}
				target := result.Candidates[0]
				if old, exists := byTarget[target]; !exists || confidence > old.Confidence {
					byTarget[target] = contract.Edge{Type: "edge", Kind: "documents_route", Source: n.Identity, Target: target, Provenance: "inferred", Confidence: confidence, Evidence: []contract.Evidence{{File: n.File, Span: *n.Span}}}
				}
			}
		}
		if rank[entry["status"].(string)] > rank[status] {
			status = entry["status"].(string)
		}
	}
	n.Attributes["status"] = status
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		p.edges = append(p.edges, byTarget[target])
	}
}

func (p *pass) coverage(file string, start int) {
	counts := map[string]int{}
	total := 0
	for _, n := range p.nodes[start:] {
		if n.Kind == "api_operation" {
			total++
			counts[n.Attributes["status"].(string)]++
		}
	}
	noun := "operations"
	if total == 1 {
		noun = "operation"
	}
	p.diagnostics = append(p.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "openapi_coverage", Message: fmt.Sprintf("OpenAPI coverage: %d %s; linked=%d, ambiguous=%d, unmatched=%d, test_only=%d, unresolved=%d", total, noun, counts["linked"], counts["ambiguous"], counts["unmatched"], counts["test_only"], counts["unresolved"])})
}
