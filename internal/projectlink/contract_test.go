package projectlink_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

const docsPath = "../../docs/extract/project-links.md"

func jsonText(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// docsRows returns the cells of each row of the "## Claim contract" table.
func docsRows(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var rows [][]string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| id |") {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "|"), " | ") {
			cells = append(cells, strings.TrimSpace(c))
		}
		rows = append(rows, cells)
	}
	return rows
}

// TestContractTable keeps the documented claim contract and the code table
// 1:1: same IDs in the same order, the unknown branch last, and a claim
// cell that names the row's link kind exactly when the row claims one.
func TestContractTable(t *testing.T) {
	docs := docsRows(t)
	if len(docs) != len(projectlink.Rows) {
		t.Fatalf("docs table has %d rows, code table %d", len(docs), len(projectlink.Rows))
	}
	for i, r := range projectlink.Rows {
		cells := docs[i]
		if cells[0] != r.ID {
			t.Errorf("row %d: docs %s, code %s", i, cells[0], r.ID)
			continue
		}
		claim := cells[2]
		if r.Kind == "" {
			if !strings.HasPrefix(claim, "none") || !strings.Contains(claim, "`"+r.Status+"`") {
				t.Errorf("%s: claim cell %q must start with none and name outcome %s", r.ID, claim, r.Status)
			}
		} else if !strings.HasPrefix(claim, "`"+r.Kind+"`") || r.Status != projectlink.StatusLinked {
			t.Errorf("%s: claim cell %q must name %s", r.ID, claim, r.Kind)
		}
	}
	if last := projectlink.Rows[len(projectlink.Rows)-1]; last.ID != projectlink.RowUnknown || last.Kind != "" {
		t.Errorf("the unknown default branch must be the last row, got %s", last.ID)
	}
}

// Synthetic nodes for the per-row cases. Attribute shapes follow
// docs/extract/clients.md and docs/extract/openapi.md.

func span() *contract.Span { return &contract.Span{StartLine: 1, EndLine: 1, EndColumn: 5} }

func routeNode(identity, method, template, scope string) contract.Node {
	path := map[string]any{"template": template, "optional": false, "trailing_slash": false}
	if scope != "" {
		path["scope"] = scope
	}
	return contract.Node{Type: "node", Kind: "route", Identity: identity, File: "server.ts", Span: span(), ContentHash: "sha256:" + identity,
		Attributes: map[string]any{"method": method, "framework": "express", "paths": []any{path}}}
}

// prefixRoute is a chi route with no path whose path below an unknown
// prefix is template (T033, unresolved_prefix_paths).
func prefixRoute(identity, method, template string) contract.Node {
	entry := map[string]any{"template": template, "prefix": "unresolved", "prefix_reason": "route_prefix_unresolved", "hop": "mount:server.go#Serve r.Route(base)", "conditional": false, "resolution": "literal"}
	return contract.Node{Type: "node", Kind: "route", Identity: identity, File: "server.go", Span: span(), ContentHash: "sha256:" + identity,
		Attributes: map[string]any{"method": method, "framework": "chi", "paths": []any{}, "unresolved_prefix_paths": []any{entry}}}
}

// callNode is an unmatched single-alternative call; change modifies it.
func callNode(identity, method, template string, change func(attrs, path map[string]any)) contract.Node {
	path := map[string]any{"template": template, "origin": nil, "conditional": false, "resolution": "literal", "status": "unmatched", "match": nil, "candidates": []any{}, "candidate_count": 0}
	attrs := map[string]any{"library": "fetch", "method": method, "base": "unresolved", "base_expression": "import.meta.env.VITE_API", "status": "unmatched", "paths": []any{path}}
	if change != nil {
		change(attrs, path)
	}
	return contract.Node{Type: "node", Kind: "client_call", Identity: identity, File: "api.ts", Span: span(), ContentHash: "sha256:" + identity, Attributes: attrs}
}

func operationNode(identity, method, template, status string, origins ...string) contract.Node {
	if origins == nil {
		origins = []string{}
	}
	path := map[string]any{"template": template, "origins": origins, "status": status, "trailing_slash": false}
	return contract.Node{Type: "node", Kind: "api_operation", Identity: identity, File: "openapi.yaml", Span: span(), ContentHash: "sha256:" + identity,
		Attributes: map[string]any{"method": method, "path": template, "paths": []any{path}}}
}

func member(repository string, nodes ...contract.Node) projectlink.Member {
	return projectlink.Member{Repository: repository, Run: "run-" + repository, Nodes: nodes}
}

// rowCase is one source in a small project and the row it must get.
// For a call with several or conditional alternatives, template names the
// alternative (each alternative is its own source); "*" takes any.
type rowCase struct {
	name, row string
	members   []projectlink.Member
	source    string
	template  string
}

// alternatives makes c a call with two conditional alternatives, /users
// (path p) and /orders, each with a stored base (extractors-47) unless
// stored is false. The call-level base_expression is the last entry's, as
// the clients extractor writes it, and must not decide either alternative.
func alternatives(stored bool) func(a, p map[string]any) {
	return func(a, p map[string]any) {
		p["conditional"] = true
		orders := map[string]any{"template": "/orders", "origin": nil, "conditional": true, "resolution": "literal", "status": "unmatched", "match": nil, "candidates": []any{}, "candidate_count": 0}
		if stored {
			p["base"], p["base_expression"] = "unresolved", "import.meta.env.VITE_API"
			orders["base"], orders["base_expression"] = "unresolved", "import.meta.env.VITE_OTHER"
		}
		a["base_expression"] = "import.meta.env.VITE_OTHER"
		a["paths"] = []any{orders, p}
	}
}

// equivalent marks routes as one equivalent_registrations group.
func equivalent(routes ...contract.Node) []contract.Node {
	ids := []any{}
	for _, r := range routes {
		ids = append(ids, r.Identity)
	}
	out := []contract.Node{}
	for i, r := range routes {
		attrs := map[string]any{}
		for k, v := range r.Attributes {
			attrs[k] = v
		}
		attrs["equivalent_registrations"] = ids
		r.Attributes = attrs
		r.Span = &contract.Span{StartLine: i + 1, EndLine: i + 1, EndColumn: 5}
		out = append(out, r)
	}
	return out
}

func rowCases() []rowCase {
	users := routeNode("route:server.ts#GET /users", "GET", "/users", "")
	users2 := routeNode("route:server.ts#GET /users@2", "GET", "/users", "")
	testUsers := routeNode("route:server.ts#GET /users", "GET", "/users", "test")
	call := callNode("client_call:api.ts#GET /users", "GET", "/users", nil)
	literal := callNode("client_call:api.ts#GET literal", "GET", "/users", func(a, p map[string]any) {
		a["base"], p["origin"] = "none", "https://API.example.com"
		delete(a, "base_expression")
	})
	op := operationNode("api_operation:openapi.yaml#GET /users", "GET", "/users", "unmatched", "https://api.example.com")
	back := member("back", users)
	other := member("other", users)
	consumes := func(m projectlink.Member, c ...config.Consume) projectlink.Member {
		m.Consumes = c
		return m
	}
	cases := []rowCase{
		{"own linked call", projectlink.RowInRepository, []projectlink.Member{member("front", callNode("c:linked", "GET", "/users", func(a, p map[string]any) { a["status"], p["status"] = "linked", "linked" })), back}, "c:linked", "*"},
		{"wrapper site", projectlink.RowInRepository, []projectlink.Member{member("front", callNode("c:wrapper", "GET", "/users", func(a, p map[string]any) { a["status"] = "wrapper" })), back}, "c:wrapper", "*"},
		{"own ambiguous operation", projectlink.RowInRepository, []projectlink.Member{member("front", operationNode("o:amb", "GET", "/users", "ambiguous")), back}, "o:amb", "*"},
		{"dynamic call", projectlink.RowDynamic, []projectlink.Member{member("front", callNode("c:dyn", "GET", "/users", func(a, p map[string]any) { a["status"], p["status"], p["template"] = "dynamic", "dynamic", nil })), back}, "c:dyn", "*"},
		{"call without paths", projectlink.RowDynamic, []projectlink.Member{member("front", callNode("c:none", "GET", "/users", func(a, p map[string]any) { a["paths"] = []any{} })), back}, "c:none", "*"},
		{"unresolved operation", projectlink.RowDynamic, []projectlink.Member{member("front", operationNode("o:unres", "GET", "/users", "unresolved")), back}, "o:unres", "*"},
		{"own linked alternative", projectlink.RowInRepository, []projectlink.Member{member("front", callNode("c:altlinked", "GET", "/users", func(a, p map[string]any) {
			alternatives(true)(a, p)
			p["status"] = "linked"
		})), back}, "c:altlinked", "/users"},
		{"dynamic alternative", projectlink.RowDynamic, []projectlink.Member{member("front", callNode("c:altdyn", "GET", "/users", func(a, p map[string]any) {
			alternatives(true)(a, p)
			a["status"], p["status"], p["template"] = "dynamic", "dynamic", nil
			delete(p, "base")
		})), back}, "c:altdyn", ""},
		{"two alternatives without stored bases", projectlink.RowAlternatives, []projectlink.Member{member("front", callNode("c:two", "GET", "/users", alternatives(false))), back}, "c:two", "/users"},
		{"conditional alternative without a stored base", projectlink.RowAlternatives, []projectlink.Member{member("front", callNode("c:cond", "GET", "/users", func(a, p map[string]any) { p["conditional"] = true })), back}, "c:cond", "/users"},
		{"literal origin undeclared", projectlink.RowLiteralOrigin, []projectlink.Member{member("front", literal), back}, literal.Identity, "*"},
		{"literal origin not named", projectlink.RowLiteralOrigin, []projectlink.Member{consumes(member("front", literal), config.Consume{Repository: "back"}), back}, literal.Identity, "*"},
		{"not declared", projectlink.RowNotDeclared, []projectlink.Member{consumes(member("front", call), config.Consume{Repository: "back", Select: config.Selector{PathPrefixes: []string{"/orders"}}}), back}, call.Identity, "*"},
		{"provider unindexed", projectlink.RowProviderUnindexed, []projectlink.Member{member("front", call), back, {Repository: "ghost"}}, call.Identity, "*"},
		{"no candidate", projectlink.RowNoCandidate, []projectlink.Member{member("front", callNode("c:none2", "GET", "/orders", nil)), back}, "c:none2", "*"},
		// UP-25: the only candidate is a route whose prefix the code does
		// not fix (unresolved_prefix_paths): never linked across.
		{"unknown prefix", projectlink.RowUnknownPrefix, []projectlink.Member{member("front", callNode("c:prefix", "GET", "/api/up/items", nil)), member("back", prefixRoute("r:prefix", "GET", "/api/up/items"))}, "c:prefix", "*"},
		// UP-25: a consumes entry naming the source's own member is no
		// declaration of a provider.
		{"unknown prefix of the own member", projectlink.RowUnknownPrefix, []projectlink.Member{consumes(member("front", callNode("c:prefixown", "GET", "/api/up/items", nil), prefixRoute("r:prefixown", "GET", "/api/up/items")), config.Consume{Repository: "front"})}, "c:prefixown", "*"},
		// UP-26: an entry naming the provider links on its unknown-prefix
		// entries (PL-L7).
		{"declared unknown prefix", projectlink.RowCallUnknownPrefix, []projectlink.Member{consumes(member("front", callNode("c:prefixdecl", "GET", "/api/up/items", nil)), config.Consume{Repository: "back"}), member("back", prefixRoute("r:prefixdecl", "GET", "/api/up/items"))}, "c:prefixdecl", "*"},
		// UP-21: a literal origin the entry names.
		{"declared unknown prefix with a literal origin", projectlink.RowCallUnknownPrefix, []projectlink.Member{consumes(member("front", callNode("c:prefixorigin", "GET", "/v2/api/up/items", func(a, p map[string]any) {
			a["base"], p["origin"] = "none", "https://API.example.com"
			delete(a, "base_expression")
		})), config.Consume{Repository: "back", Select: config.Selector{Origins: []string{"https://api.example.com"}}}), member("back", prefixRoute("r:prefixorigin", "GET", "/api/up/items"))}, "c:prefixorigin", "*"},
		// UP-20: without an entry naming it, a literal origin is refused.
		{"undeclared literal origin and unknown prefix", projectlink.RowLiteralOrigin, []projectlink.Member{member("front", callNode("c:prefixliteral", "GET", "/api/up/items", func(a, p map[string]any) {
			a["base"], p["origin"] = "none", "https://api.example.com"
			delete(a, "base_expression")
		})), member("back", prefixRoute("r:prefixliteral", "GET", "/api/up/items"))}, "c:prefixliteral", "*"},
		// UP-17 across providers: two declared providers match.
		{"declared unknown prefix in two providers", projectlink.RowSeveralProviders, []projectlink.Member{consumes(member("front", callNode("c:prefixtwo", "GET", "/api/up/items", nil)), config.Consume{Repository: "back"}, config.Consume{Repository: "other"}), member("back", prefixRoute("r:prefixtwo", "GET", "/api/up/items")), member("other", users, prefixRoute("r:prefixtwo2", "GET", "/api/up/items"))}, "c:prefixtwo", "*"},
		{"test-only routes", projectlink.RowTestOnly, []projectlink.Member{member("front", call), member("back", testUsers)}, call.Identity, "*"},
		{"tie inside one provider", projectlink.RowProviderAmbiguous, []projectlink.Member{member("front", call), member("back", users, users2)}, call.Identity, "*"},
		{"two providers", projectlink.RowSeveralProviders, []projectlink.Member{member("front", call), back, other}, call.Identity, "*"},
		{"default call link", projectlink.RowCallDefault, []projectlink.Member{member("front", call), back}, call.Identity, "*"},
		{"alternative with a stored base", projectlink.RowCallDefault, []projectlink.Member{member("front", callNode("c:alt", "GET", "/users", alternatives(true))), back}, "c:alt", "/users"},
		{"equivalent registrations", projectlink.RowCallDefault, []projectlink.Member{member("front", call), member("back", equivalent(users2, users)...)}, call.Identity, "*"},
		{"alternative through its own base selector", projectlink.RowCallSelector, []projectlink.Member{consumes(member("front", callNode("c:altsel", "GET", "/users", alternatives(true))),
			config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}}), back}, "c:altsel", "/users"},
		{"alternative outside the base selector", projectlink.RowNotDeclared, []projectlink.Member{consumes(member("front", callNode("c:altsel2", "GET", "/users", alternatives(true))),
			config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}}), back}, "c:altsel2", "/orders"},
		{"declared call link", projectlink.RowCallDeclared, []projectlink.Member{consumes(member("front", call), config.Consume{Repository: "back"}), back, other}, call.Identity, "*"},
		{"selector call link", projectlink.RowCallSelector, []projectlink.Member{consumes(member("front", call),
			config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}},
			config.Consume{Repository: "other", Select: config.Selector{PathPrefixes: []string{"/orders"}}}), back, other}, call.Identity, "*"},
		{"literal origin through selector", projectlink.RowCallOrigin, []projectlink.Member{consumes(member("front", literal), config.Consume{Repository: "back", Select: config.Selector{Origins: []string{"https://api.example.com"}}}), back, other}, literal.Identity, "*"},
		{"default documents link", projectlink.RowDocumentsDefault, []projectlink.Member{member("front", op), back}, op.Identity, "*"},
		// T022 review: a backend's own spec documents its own server.
		{"server spec without consumes", projectlink.RowServerSpec, []projectlink.Member{member("be2", op, routeNode("route:server.ts#POST /login", "POST", "/login", "")), back}, op.Identity, "*"},
		{"server spec through consumes", projectlink.RowDocumentsDeclared, []projectlink.Member{consumes(member("be2", op, routeNode("route:server.ts#POST /login", "POST", "/login", "")), config.Consume{Repository: "back"}), back}, op.Identity, "*"},
		{"declared documents link", projectlink.RowDocumentsDeclared, []projectlink.Member{consumes(member("front", op), config.Consume{Repository: "other", Select: config.Selector{Origins: []string{"https://api.example.com"}}}), back, other}, op.Identity, "*"},
		// Selectors narrow client calls only (T022 B6c R1): an operation
		// follows the entry's provider as with an entry without select,
		// unless an entry's origins names its server origin.
		{"operation under a bases selector", projectlink.RowDocumentsDeclared, []projectlink.Member{consumes(member("front", op), config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}}), back, other}, op.Identity, "*"},
		{"operation under a path prefix selector", projectlink.RowDocumentsDeclared, []projectlink.Member{consumes(member("front", op), config.Consume{Repository: "back", Select: config.Selector{PathPrefixes: []string{"/orders"}}}), back, other}, op.Identity, "*"},
		{"operation under selectors of two providers", projectlink.RowSeveralProviders, []projectlink.Member{consumes(member("front", op),
			config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}},
			config.Consume{Repository: "other", Select: config.Selector{PathPrefixes: []string{"/orders"}}}), back, other}, op.Identity, "*"},
		{"operation origin named by another entry", projectlink.RowDocumentsDeclared, []projectlink.Member{consumes(member("front", op),
			config.Consume{Repository: "back", Select: config.Selector{Bases: []string{"VITE_API"}}},
			config.Consume{Repository: "other", Select: config.Selector{Origins: []string{"https://API.example.com"}}}), back, other}, op.Identity, "*"},
		{"undecodable attributes", projectlink.RowUnknown, []projectlink.Member{member("front", callNode("c:bad", "GET", "/users", func(a, p map[string]any) { a["paths"] = "nope" })), back}, "c:bad", "*"},
		{"unknown own status", projectlink.RowUnknown, []projectlink.Member{member("front", callNode("c:odd", "GET", "/users", func(a, p map[string]any) { a["status"] = "odd" })), back}, "c:odd", "*"},
	}
	return append(cases, scopeCases()...)
}

// at returns n moved to file, so it falls in the link scope holding file.
func at(n contract.Node, file string) contract.Node {
	n.File = file
	return n
}

// ambiguousCall is an in-repository ambiguous single-alternative call
// between the given route identities.
func ambiguousCall(identity, template string, candidates ...string) contract.Node {
	ids := []any{}
	for _, c := range candidates {
		ids = append(ids, c)
	}
	return callNode(identity, "GET", template, func(a, p map[string]any) {
		a["status"], p["status"], p["candidates"], p["candidate_count"] = "ambiguous", "ambiguous", ids, len(ids)
	})
}

// scopeCases are the link scope rows: a repository "mono" with
// two servers, apps/a and apps/b, both registering GET /users, and a web
// scope whose calls the in-repository linker leaves ambiguous between them.
func scopeCases() []rowCase {
	usersA := at(routeNode("route:apps/a/server.ts#GET /users", "GET", "/users", ""), "apps/a/server.ts")
	usersB := at(routeNode("route:apps/b/server.ts#GET /users", "GET", "/users", ""), "apps/b/server.ts")
	userA := at(routeNode("route:apps/a/server.ts#GET /users/{id}", "GET", "/users/{id}", ""), "apps/a/server.ts")
	meB := at(routeNode("route:apps/b/server.ts#GET /users/me", "GET", "/users/me", ""), "apps/b/server.ts")
	meC := at(routeNode("route:apps/c/server.ts#GET /users/me", "GET", "/users/me", ""), "apps/c/server.ts")
	webCall := at(ambiguousCall("client_call:web/api.ts#GET /users", "/users", usersA.Identity, usersB.Identity), "web/api.ts")
	toolCall := at(ambiguousCall("client_call:tools/api.ts#GET /users", "/users", usersA.Identity, usersB.Identity), "tools/api.ts")
	meCall := at(ambiguousCall("client_call:web/api.ts#GET /users/me", "/users/me", meB.Identity, meC.Identity), "web/api.ts")
	op := at(operationNode("api_operation:web/openapi.yaml#GET /users", "GET", "/users", "ambiguous"), "web/openapi.yaml")
	plain := at(callNode("client_call:web/api.ts#GET /users plain", "GET", "/users", nil), "web/api.ts")
	mono := func(web []config.Consume, nodes ...contract.Node) projectlink.Member {
		m := member("mono", nodes...)
		m.Scopes = []config.Scope{{Name: "a", Path: "apps/a"}, {Name: "b", Path: "apps/b"}, {Name: "c", Path: "apps/c"}, {Name: "web", Path: "web", Consumes: web}}
		return m
	}
	consumesA := []config.Consume{{Repository: "mono", Scope: "a"}}
	both := []config.Consume{{Repository: "mono", Scope: "a"}, {Repository: "mono", Scope: "b"}}
	front := member("front", plain)
	front.Nodes[0].File = "api.ts"
	return []rowCase{
		{"scope resolves an in-repository ambiguous call", projectlink.RowScopeCall, []projectlink.Member{mono(consumesA, usersA, usersB, webCall)}, webCall.Identity, "*"},
		{"scope resolves an in-repository ambiguous operation", projectlink.RowScopeDocuments, []projectlink.Member{mono(consumesA, usersA, usersB, op)}, op.Identity, "*"},
		{"two declared scopes keep the ambiguity", projectlink.RowScopeUnresolved, []projectlink.Member{mono(both, usersA, usersB, webCall)}, webCall.Identity, "*"},
		// The call is tied between b and c; the declared scope a holds only
		// a less specific route (/users/{id}): a scope may only narrow the
		// tied candidates, never pick a route outside them.
		{"declared scope outside the tied candidates", projectlink.RowScopeUnresolved, []projectlink.Member{mono(consumesA, userA, meB, meC, meCall)}, meCall.Identity, "*"},
		{"declared scope and another repository", projectlink.RowScopeUnresolved, []projectlink.Member{mono([]config.Consume{{Repository: "mono", Scope: "a"}, {Repository: "back"}}, usersA, usersB, webCall), member("back", routeNode("route:server.ts#GET /users", "GET", "/users", ""))}, webCall.Identity, "*"},
		{"call outside every declared consumer scope", projectlink.RowInRepository, []projectlink.Member{mono(consumesA, usersA, usersB, toolCall)}, toolCall.Identity, "*"},
		{"declared scope without the own repository", projectlink.RowInRepository, []projectlink.Member{mono([]config.Consume{{Repository: "back"}}, usersA, usersB, webCall), member("back", routeNode("route:server.ts#GET /users", "GET", "/users", ""))}, webCall.Identity, "*"},
		{"consumer scope without consumes keeps the default rules", projectlink.RowCallDefault, []projectlink.Member{func() projectlink.Member {
			m := member("front", at(callNode("c:legacy", "GET", "/users", nil), "legacy/api.ts"))
			m.Scopes = []config.Scope{{Name: "legacy", Path: "legacy"}}
			return m
		}(), member("back", routeNode("route:server.ts#GET /users", "GET", "/users", ""))}, "c:legacy", "*"},
		{"consumes names a scope", projectlink.RowCallDeclared, []projectlink.Member{func() projectlink.Member {
			m := member("front", plain)
			m.Consumes = consumesA
			return m
		}(), mono(nil, usersA, usersB)}, plain.Identity, "*"},
		{"scopes of one provider are separate endpoints", projectlink.RowSeveralProviders, []projectlink.Member{front, mono(nil, usersA, usersB)}, plain.Identity, "*"},
		{"tie inside one scope", projectlink.RowProviderAmbiguous, []projectlink.Member{front, mono(nil, usersA, at(routeNode("route:apps/a/server.ts#GET /users@2", "GET", "/users", ""), "apps/a/server.ts"))}, plain.Identity, "*"},
	}
}

// TestRowCases is the per-row half of the contract test: every row has at
// least one case, every case's source gets exactly its row, and a link
// exists exactly when the row claims one.
func TestRowCases(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range rowCases() {
		t.Run(c.name, func(t *testing.T) {
			result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: c.members})
			var found []projectlink.Outcome
			for _, o := range result.Outcomes {
				if o.Source.Identity == c.source && (c.template == "*" || o.Template == c.template) {
					found = append(found, o)
				}
			}
			if len(found) != 1 || found[0].Row != c.row {
				t.Fatalf("outcomes for %s: %+v, want one with row %s", c.source, found, c.row)
			}
			var want projectlink.Row
			for _, r := range projectlink.Rows {
				if r.ID == c.row {
					want = r
				}
			}
			if found[0].Status != want.Status {
				t.Fatalf("status %s, want %s", found[0].Status, want.Status)
			}
			links := 0
			for _, l := range result.Set.Links {
				if l.Source.Identity == c.source && (c.template == "*" || l.Attributes["template"] == c.template) {
					links++
					if l.Kind != want.Kind || l.Attributes["row"] != c.row || l.Assumption == "" || l.Confidence <= 0 || l.Source.Run == "" || l.Target.Run == "" {
						t.Fatalf("link %+v", l)
					}
				}
			}
			if (want.Kind != "") != (links == 1) || links > 1 {
				t.Fatalf("%d links for row %s", links, c.row)
			}
			covered[c.row] = true
		})
	}
	for _, r := range projectlink.Rows {
		if !covered[r.ID] {
			t.Errorf("row %s has no case", r.ID)
		}
	}
}

// TestLinkDetails checks the values a link carries for the default and the
// declared assumption, the literal-origin confidence and candidate lists.
func TestLinkDetails(t *testing.T) {
	cases := map[string]rowCase{}
	for _, c := range rowCases() {
		cases[c.name] = c
	}
	link := func(name string) (projectlink.Result, int) {
		c := cases[name]
		result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: c.members})
		for i, l := range result.Set.Links {
			if l.Source.Identity == c.source {
				return result, i
			}
		}
		t.Fatalf("%s: no link", name)
		return result, -1
	}
	r, i := link("default call link")
	l := r.Set.Links[i]
	if l.Assumption != projectlink.DefaultAssumption || l.Confidence != 0.9 || l.Target.Repository != "back" || l.Target.ContentHash == "" || l.Source.ContentHash == "" || len(l.Evidence) != 1 {
		t.Fatalf("default link %+v", l)
	}
	r, i = link("selector call link")
	if got := r.Set.Links[i].Assumption; got != "declared in graphvein-project.yaml: `front` consumes `back` (bases: VITE_API)" {
		t.Fatalf("selector assumption %q", got)
	}
	r, i = link("literal origin through selector")
	if l := r.Set.Links[i]; l.Confidence != 0.7 || !strings.Contains(l.Assumption, "origins: https://api.example.com") {
		t.Fatalf("origin link %+v", l)
	}
	r, i = link("operation under a bases selector")
	if l := r.Set.Links[i]; l.Target.Repository != "back" || l.Assumption != "declared in graphvein-project.yaml: `front` consumes `back`; origin https://api.example.com assumed to be back's server" {
		// The entry names no origin, so the spec's server is assumed (LF-1).
		t.Fatalf("operation under a selector %+v", l)
	}
	r, i = link("operation origin named by another entry")
	if l := r.Set.Links[i]; l.Target.Repository != "other" || !strings.Contains(l.Assumption, "origins: https://API.example.com") {
		t.Fatalf("operation origin link %+v", l)
	}
	r, i = link("default documents link")
	if l := r.Set.Links[i]; l.Confidence != 0.7 || l.Kind != projectlink.KindDocumentsRoute {
		t.Fatalf("documents link %+v", l)
	}
	// Equivalent registrations collapse to the first in source order (ADR
	// 0022), here the @2 identity, which the group places first.
	r, i = link("equivalent registrations")
	if l := r.Set.Links[i]; l.Target.Identity != "route:server.ts#GET /users@2" || l.Confidence != 0.9 {
		t.Fatalf("equivalent registrations link %+v", l)
	}
	// Groups that differ, or a group that does not name the route itself,
	// prove nothing: the tie stays ambiguous.
	users := routeNode("route:server.ts#GET /users", "GET", "/users", "")
	users2 := routeNode("route:server.ts#GET /users@2", "GET", "/users", "")
	users3 := routeNode("route:server.ts#GET /users@3", "GET", "/users", "")
	pair := equivalent(users, users3)
	other := equivalent(users2, users3)
	call := callNode("client_call:api.ts#GET /users", "GET", "/users", nil)
	for name, routes := range map[string][]contract.Node{"different groups": {pair[0], other[0]}, "group without the route": {pair[0], pair[1], equivalent(users3, users)[0]}} {
		if name == "group without the route" {
			routes[2].Identity = users2.Identity
		}
		result := projectlink.Link(projectlink.Input{Project: "p", Members: []projectlink.Member{member("front", call), member("back", routes...)}})
		if len(result.Set.Links) != 0 || result.Outcomes[0].Row != projectlink.RowProviderAmbiguous {
			t.Fatalf("%s: %+v", name, result.Outcomes)
		}
	}
	// A scope overlay links the route of the declared scope, names the
	// declaration and says it resolves an in-repository ambiguity.
	r, i = link("scope resolves an in-repository ambiguous call")
	if l := r.Set.Links[i]; l.Target != (store.ProjectEndpoint{Repository: "mono", Identity: "route:apps/a/server.ts#GET /users", Run: "run-mono", ContentHash: "sha256:route:apps/a/server.ts#GET /users"}) ||
		l.Confidence != 0.9 || l.Attributes["source_scope"] != "web" || l.Attributes["target_scope"] != "a" ||
		l.Assumption != "declared in graphvein-project.yaml: `mono/web` consumes `mono/a`; "+projectlink.ScopeOverlayNote {
		t.Fatalf("scope overlay link %+v", l)
	}
	r, i = link("consumes names a scope")
	if l := r.Set.Links[i]; l.Target.Identity != "route:apps/a/server.ts#GET /users" || l.Attributes["target_scope"] != "a" || l.Assumption != "declared in graphvein-project.yaml: `front` consumes `mono/a`" {
		t.Fatalf("scope provider link %+v", l)
	}
	// Unresolved overlays list the candidates the declarations left: both
	// declared scopes, or the in-repository ties when no declared scope
	// holds one of them.
	for name, want := range map[string][]string{
		"two declared scopes keep the ambiguity":     {"route:apps/a/server.ts#GET /users", "route:apps/b/server.ts#GET /users"},
		"declared scope outside the tied candidates": {"route:apps/b/server.ts#GET /users/me", "route:apps/c/server.ts#GET /users/me"},
	} {
		c := cases[name]
		result := projectlink.Link(projectlink.Input{Project: "p", Members: c.members})
		var got []string
		for _, o := range result.Outcomes {
			if o.Source.Identity == c.source {
				for _, e := range o.Candidates {
					got = append(got, e.Identity)
				}
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") || len(result.Set.Links) != 0 {
			t.Fatalf("%s: candidates %v, links %+v", name, got, result.Set.Links)
		}
	}

	c := cases["two providers"]
	result := projectlink.Link(projectlink.Input{Project: "p", ManifestHash: "sha256:m", Members: c.members})
	for _, o := range result.Outcomes {
		if o.Source.Identity == c.source {
			if o.CandidateCount != 2 || len(o.Candidates) != 2 || o.Candidates[0].Repository != "back" || o.Candidates[1].Repository != "other" {
				t.Fatalf("ambiguous outcome %+v", o)
			}
		}
	}
	if len(result.Set.Links) != 0 {
		t.Fatalf("ambiguity linked: %+v", result.Set.Links)
	}
}
