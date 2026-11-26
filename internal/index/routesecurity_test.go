package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// securityLines renders, per route of a snapshot, the security section
// of its shape (state, reason, requirements, schemes) and the router
// middleware attributes the Go linker stores (T029).
func securityLines(t *testing.T, s store.Snapshot) []string {
	t.Helper()
	var ids []string
	byID := map[string][]string{}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		id := unescape(n.Identity)
		lines := []string{id}
		for _, key := range []string{"middleware", "group_middleware", "group_middleware_unknown"} {
			if v, ok := n.Attributes[key]; ok {
				data, _ := json.Marshal(v)
				if string(data) != "[]" {
					lines = append(lines, "  "+key+": "+string(data))
				}
			}
		}
		v, ok := n.Attributes[routeshape.AttributeKey]
		if !ok {
			lines = append(lines, "  no shape")
		} else {
			shape, err := routeshape.FromAttribute(v)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			line := "  security: " + string(shape.Security.State)
			if shape.Security.Reason != "" {
				line += " (" + shape.Security.Reason + ")"
			}
			lines = append(lines, line)
			for _, r := range shape.Security.Items {
				var names []string
				for _, u := range r.Schemes {
					names = append(names, u.Name)
				}
				lines = append(lines, fmt.Sprintf("    requires %s | %s %s %s:%d", strings.Join(names, " + "), r.Provenance.Kind, r.Provenance.Rule, r.Provenance.File, r.Provenance.Line))
			}
			for _, sc := range shape.Schemes {
				str := func(p *string) string {
					if p == nil {
						return "-"
					}
					return *p
				}
				lines = append(lines, fmt.Sprintf("    scheme %s %s scheme=%s in=%s name=%s | %s %s:%d", sc.Name, sc.Type, str(sc.Scheme), str(sc.In), str(sc.ParamName), sc.Provenance.Rule, sc.Provenance.File, sc.Provenance.Line))
			}
		}
		ids = append(ids, id)
		byID[id] = lines
	}
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		out = append(out, byID[id]...)
	}
	return out
}

// TestRouteSecurityGolden indexes the route-shape-security fixture (T029,
// docs/contract/claims/route-shape-security.md) and checks the
// security sections and router middleware read back from the store, and
// that a warm run replays every file.
func TestRouteSecurityGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/route-shape-security")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/route-shape-security.golden.json", securityLines(t, snapshot))
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
}

// TestRouteSecurityExport writes the security claims of the
// route-shape-security fixture as JSON for the oracle
// (scripts/oracle/route_security). Opt-in: GRAPHVEIN_ROUTE_SECURITY_OUT
// names the output file; never run by CI.
func TestRouteSecurityExport(t *testing.T) {
	out := os.Getenv("GRAPHVEIN_ROUTE_SECURITY_OUT")
	if out == "" {
		t.Skip("GRAPHVEIN_ROUTE_SECURITY_OUT is not set")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/route-shape-security")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	type claim struct {
		Route   string                   `json:"route"`
		Method  string                   `json:"method"`
		Paths   []string                 `json:"paths"`
		State   string                   `json:"state"`
		Reason  string                   `json:"reason"`
		Items   []routeshape.Requirement `json:"requirements"`
		Schemes []routeshape.Scheme      `json:"schemes"`
		// Middleware counts: the route's own arguments, its router-level
		// middleware and the unknown entries (gin's handler count check).
		RouteMW int `json:"route_middleware"`
		Group   int `json:"group_middleware"`
		Unknown int `json:"group_middleware_unknown"`
	}
	var claims []claim
	for _, n := range snapshot.Nodes {
		v, ok := n.Attributes[routeshape.AttributeKey]
		if n.Kind != "route" || !ok {
			continue
		}
		shape, err := routeshape.FromAttribute(v)
		if err != nil {
			t.Fatal(err)
		}
		c := claim{Route: unescape(n.Identity), State: string(shape.Security.State), Reason: shape.Security.Reason, Items: shape.Security.Items, Schemes: shape.Schemes}
		c.Method, _ = n.Attributes["method"].(string)
		c.RouteMW, c.Group, c.Unknown = countOf(n.Attributes["middleware"]), countOf(n.Attributes["group_middleware"]), countOf(n.Attributes["group_middleware_unknown"])
		paths, _ := n.Attributes["paths"].([]any)
		for _, p := range paths {
			m, _ := p.(map[string]any)
			if tpl, _ := m["template"].(string); tpl != "" {
				c.Paths = append(c.Paths, tpl)
			}
		}
		claims = append(claims, c)
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].Route < claims[j].Route })
	data, err := json.MarshalIndent(claims, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// countOf is the length of a list attribute read back from the store.
func countOf(v any) int {
	list, _ := v.([]any)
	return len(list)
}
