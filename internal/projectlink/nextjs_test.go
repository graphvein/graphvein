package projectlink_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
)

// nextRouteNode is a Next.js App Router route handler of application
// project (the route attribute), with the stored attributes LinkNext
// writes: base_path "" and, when given, literal rewrites.
func nextRouteNode(identity, file, project, method, template string, rewrites []map[string]any) contract.Node {
	attrs := map[string]any{"framework": "nextjs", "method": method, "pattern": template, "project": project, "base_path": "",
		"paths": []any{map[string]any{"template": template, "resolution": "literal", "optional": false}}}
	if rewrites != nil {
		rules := []any{}
		for _, r := range rewrites {
			rules = append(rules, r)
		}
		attrs["rewrites"] = map[string]any{"status": "literal", "rules": rules}
	}
	return contract.Node{Type: "node", Kind: "route", Identity: identity, File: file, Span: span(), ContentHash: "sha256:" + identity, Attributes: attrs}
}

// TestNextRouteTargets checks project links to another repository's
// Next.js routes (TOP-1 of 2026-11-18): a call links under the project rules
// with NX-V1 (and NX-V3 through an afterFiles rewrite) added to the
// cross-repository assumption; a literal origin needs a consumes entry
// naming it; an application the member's link scopes divide is no target.
func TestNextRouteTargets(t *testing.T) {
	chat := nextRouteNode("route:app/api/chat/route.ts#POST", "app/api/chat/route.ts", ".", "POST", "/api/chat", nil)
	for _, c := range []struct {
		name        string
		members     []projectlink.Member
		row         string
		assumes     []string
		assumption  string
		targetFound bool
	}{
		{"default rules", []projectlink.Member{member("front", callNode("call:a", "POST", "/api/chat", nil)), member("web", chat)},
			projectlink.RowCallDefault, []string{"NX-V1"}, projectlink.DefaultAssumption + "; Next.js route: assumes NX-V1", true},
		{"afterFiles rewrite re-routes", []projectlink.Member{
			member("front", callNode("call:a", "GET", "/old/{id}", func(attrs, _ map[string]any) { attrs["base"] = "none"; delete(attrs, "base_expression") })),
			member("web", nextRouteNode("route:app/api/new/[id]/route.ts#GET", "app/api/new/[id]/route.ts", ".", "GET", "/api/new/{id}",
				[]map[string]any{{"phase": "afterFiles", "source": "/old/:id", "destination": "/api/new/:id"}}))},
			projectlink.RowCallDefault, []string{"NX-V1", "NX-V3"}, projectlink.DefaultAssumption + "; Next.js route: assumes NX-V1, NX-V3", true},
		{"literal origin without a declaration", []projectlink.Member{
			member("front", callNode("call:a", "POST", "/api/chat", func(attrs, path map[string]any) {
				attrs["base"] = "literal"
				path["origin"] = "https://chat.example.org"
			})),
			member("web", chat)}, projectlink.RowLiteralOrigin, nil, "", false},
		{"literal origin declared", []projectlink.Member{
			func() projectlink.Member {
				m := member("front", callNode("call:a", "POST", "/api/chat", func(attrs, path map[string]any) {
					attrs["base"] = "literal"
					path["origin"] = "https://chat.example.org"
				}))
				m.Consumes = []config.Consume{{Repository: "web", Select: config.Selector{Origins: []string{"https://chat.example.org"}}}}
				return m
			}(), member("web", chat)},
			projectlink.RowCallOrigin, []string{"NX-V1"}, "", true},
		{"application divided by link scopes", []projectlink.Member{
			member("front", callNode("call:a", "POST", "/api/chat", nil)),
			func() projectlink.Member {
				m := member("web", chat, nextRouteNode("route:app/api/x/route.ts#GET", "app/api/x/route.ts", ".", "GET", "/api/x", nil))
				m.Scopes = []config.Scope{{Name: "x", Path: "app/api/x"}}
				return m
			}()}, projectlink.RowNoCandidate, nil, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: c.members})
			var outcome *projectlink.Outcome
			for i := range result.Outcomes {
				if result.Outcomes[i].Source.Identity == "call:a" {
					outcome = &result.Outcomes[i]
				}
			}
			if outcome == nil || outcome.Row != c.row {
				t.Fatalf("outcome %+v, want row %s", outcome, c.row)
			}
			if !c.targetFound {
				if len(result.Set.Links) != 0 {
					t.Fatalf("links %+v, want none", result.Set.Links)
				}
				return
			}
			if len(result.Set.Links) != 1 {
				t.Fatalf("links %+v, want one", result.Set.Links)
			}
			l := result.Set.Links[0]
			if !reflect.DeepEqual(l.Assumes, c.assumes) || !strings.HasSuffix(l.Assumption, projectlink.NextAssumptionNote(c.assumes)) {
				t.Fatalf("link assumes %v (%q), want %v", l.Assumes, l.Assumption, c.assumes)
			}
			if c.assumption != "" && l.Assumption != c.assumption {
				t.Fatalf("assumption %q, want %q", l.Assumption, c.assumption)
			}
		})
	}
}

// TestNextRoutesStayOwnOrigin checks that the same-origin rule within a
// repository is unchanged: the scope overlay of a member never takes its
// own Next.js routes as candidates, so a call its own index left
// ambiguous between two Express scopes is still resolved by a declaration
// that also names the scope holding a Next.js route matching the path.
func TestNextRoutesStayOwnOrigin(t *testing.T) {
	a := routeNode("route:apps/a/server.ts#GET /items", "GET", "/items", "")
	a.File = "apps/a/server.ts"
	b := routeNode("route:apps/b/server.ts#GET /items", "GET", "/items", "")
	b.File = "apps/b/server.ts"
	next := nextRouteNode("route:web/app/items/route.ts#GET", "web/app/items/route.ts", "web", "GET", "/items", nil)
	call := callNode("call:a", "GET", "/items", func(attrs, path map[string]any) {
		attrs["status"] = "ambiguous"
		path["status"] = "ambiguous"
		path["candidates"] = []any{a.Identity, b.Identity}
		path["candidate_count"] = 2
	})
	call.File = "apps/front/api.ts"
	m := member("mono", a, b, next, call)
	m.Scopes = []config.Scope{{Name: "a", Path: "apps/a"}, {Name: "b", Path: "apps/b"}, {Name: "front", Path: "apps/front", Consumes: []config.Consume{{Repository: "mono", Scope: "a"}, {Repository: "mono", Scope: "web"}}}, {Name: "web", Path: "web"}}
	result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: []projectlink.Member{m}})
	if len(result.Set.Links) != 1 || result.Set.Links[0].Target.Identity != a.Identity || len(result.Set.Links[0].Assumes) != 0 {
		t.Fatalf("links %+v, outcomes %+v", result.Set.Links, result.Outcomes)
	}
}
