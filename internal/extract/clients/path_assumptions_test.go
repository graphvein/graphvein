package clients

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestLinkedPathAssumptions checks how a link carries the path
// assumptions of the route path entries it matched:
// the union of the winning entries' ids, none when one winning
// entry rests on none, test-scope entries read only for a test call, and
// the confidence unchanged.
func TestLinkedPathAssumptions(t *testing.T) {
	route := func(identity string, entries ...map[string]any) contract.Node {
		return contract.Node{Kind: "route", Identity: identity, Attributes: map[string]any{"method": "GET", "framework": "express", "paths": entries}}
	}
	entry := func(template string, assumptions ...string) map[string]any {
		e := map[string]any{"template": template}
		if len(assumptions) > 0 {
			e["assumptions"] = assumptions
		}
		return e
	}
	nodes := []contract.Node{
		// Two entries tie for /a/x: both rest on an assumption.
		route("route:both", entry("/a/{p}", "RO-V2"), entry("/{q}/x", "GR-V5", "RO-V2")),
		// Two entries tie for /b/x: one is plain, so the link needs none.
		route("route:plain", entry("/b/{p}", "RO-V1"), entry("/{q}/y")),
		// A test-scope entry with an assumption beside a plain production one.
		route("route:scoped", entry("/c/z"), map[string]any{"template": "/c/z", "scope": "test", "assumptions": []string{"RO-V1"}}),
	}
	m := NewStoredRouteMatcher(nodes)
	for _, c := range []struct {
		template, scope, target string
		assumes                 []string
	}{
		{"/a/x", "production", "route:both", []string{"RO-V2", "GR-V5"}},
		{"/b/y", "production", "route:plain", nil},
		{"/c/z", "production", "route:scoped", nil},
		{"/c/z", "test", "route:scoped", nil},
	} {
		got := m.Call(CallRequest{Method: "GET", Template: c.template, Scope: c.scope, Base: "none"})
		if got.Status != "linked" || got.Candidates[0] != c.target || got.Confidence != 0.9 {
			t.Fatalf("%s: %+v, want a link to %s at 0.9", c.template, got, c.target)
		}
		if !reflect.DeepEqual(got.Assumes, c.assumes) {
			t.Errorf("%s (%s): assumes %v, want %v", c.template, c.scope, got.Assumes, c.assumes)
		}
	}
}
