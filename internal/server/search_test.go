package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

func TestSearchContractTable(t *testing.T) {
	documented := contractTableRows(t, "../../docs/contract/claims/jump-search.md", "JS")
	var code []string
	tests := map[string]string{}
	for _, r := range searchRows {
		code = append(code, r.ID+" "+r.Topic)
		tests[r.ID] = r.Test
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("jump-search.md rows %v\ncode rows %v", documented, code)
	}
	checkRowTests(t, tests, "search_test.go")
}

func decodeSearch(t *testing.T, body []byte) searchResponse {
	t.Helper()
	var out searchResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err, string(body))
	}
	return out
}

func searchGet(t *testing.T, opt Options, path string) searchResponse {
	t.Helper()
	rec := get(t, opt, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
	}
	return decodeSearch(t, rec.Body.Bytes())
}

// group returns a kind's group, failing when it is absent.
func group(t *testing.T, groups []searchGroup, kind string) searchGroup {
	t.Helper()
	for _, g := range groups {
		if g.Kind == kind {
			return g
		}
	}
	t.Fatalf("no %s group in %v", kind, groups)
	return searchGroup{}
}

func labels(items []searchItem) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label+"/"+it.Tier)
	}
	return strings.Join(out, ",")
}

// TestSearchParameters: JS-1..JS-3, JS-19.
func TestSearchParameters(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		opt := testOptions(t.TempDir(), s)
		for _, bad := range []string{"", "q=", "q=%20%20", "q=" + strings.Repeat("a", 201), "q=%ff", "q=a&q=b", "q=a&kinds=route,nope", "q=a&kinds=route,route", "q=a&kinds=", "q=a&limit=0", "q=a&limit=51", "q=a&limit=x", "q=a&editor=nope", "q=a&first=fixture"} {
			if rec := get(t, opt, http.MethodGet, "/api/v0/search?"+bad, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("%q: %d %s", bad, rec.Code, rec.Body.String())
			}
		}
		r := searchGet(t, opt, "/api/v0/search?q=%20user%20&kinds=symbol,route&limit=1&editor=vscode")
		if r.Query != "user" || strings.Join(r.Kinds, ",") != "route,symbol" || r.Limit != 1 || len(r.Groups) != 2 || r.IDE.Editor == nil || *r.IDE.Editor != "vscode" {
			t.Errorf("parsed %+v", r)
		}
		if _, ok := r.Totals["file"]; ok {
			t.Error("a kind not asked for is reported")
		}
		if g := group(t, r.Groups, searchSymbol); len(g.Items) != 1 || !g.Truncated || g.Total != 2 {
			t.Errorf("limit 1: %+v", g)
		}
		def := searchGet(t, opt, "/api/v0/search?q=u")
		if def.Limit != searchDefaultLimit || len(def.Kinds) != 5 {
			t.Errorf("defaults %d %v", def.Limit, def.Kinds)
		}
	})
}

// TestSearchTiers: JS-9.
func TestSearchTiers(t *testing.T) {
	for _, c := range []struct {
		key, query string
		want       int
	}{
		{"getUserName", "get", tierPrefix},
		{"GetUserName", "getuser", tierPrefix},
		{"getUserName", "user", tierWord},
		{"get_user", "user", tierWord},
		{"/api/users/:id", "users", tierWord},
		{"v2Users", "users", tierWord},
		{"v2users", "2", tierWord},
		{"busers", "users", tierSubstring},
		{"ab", "abc", -1},
		{"ussr user", "us", tierPrefix},
		{"xusxUs", "us", tierWord},
		{"Kelvin", "k", -1},
	} {
		if got := searchTierOf(c.key, strings.ToLower(c.query)); got != c.want {
			t.Errorf("searchTierOf(%q, %q) = %d, want %d", c.key, c.query, got, c.want)
		}
	}
}

// TestSearchSynthetic: JS-6, JS-7, JS-10..JS-13, JS-20, D on the crafted
// snapshot of the couldn't-read tests.
func TestSearchSynthetic(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		opt := testOptions(t.TempDir(), s)
		r := searchGet(t, opt, "/api/v0/search?q=user")
		// JS-13: what was searched.
		if !r.Searched.Indexed || r.Searched.Files != 13 || r.Searched.FilesNotRead != 3 || r.Searched.ScannerGaps != 3 || r.Absence != absenceNotProven {
			t.Errorf("searched %+v", r.Searched)
		}
		// JS-6: the table by name, with its source.
		tables := group(t, r.Groups, searchTable)
		if labels(tables.Items) != "users/prefix" || *tables.Items[0].Source != "migration" {
			t.Errorf("tables %s", labels(tables.Items))
		}
		// JS-7, JS-10: the best key decides (getUserName's qualified name
		// UserService.getUserName starts with the query), then the
		// shorter key; the column user_id is not a symbol (JS-20).
		symbols := group(t, r.Groups, searchSymbol)
		if got := labels(symbols.Items); got != "UserService/prefix,getUserName/prefix" || symbols.Items[1].MatchKey != "qualified_name" {
			t.Errorf("symbols %s", got)
		}
		if symbols.Total != 2 {
			t.Errorf("symbol total %d", symbols.Total)
		}
		// JS-4: routes by path, name or pattern; a route with no path
		// known is labelled by its name or pattern marked "(path not
		// known)" (ADR 0089 Revision 4).
		routes := group(t, r.Groups, searchRoute)
		if got := labels(routes.Items); got != "/users/word,GET /user-names (path not known)/word" {
			t.Errorf("routes %s", got)
		}
		// JS-11: groups by best tier, then kind order; JS-12 totals.
		var order []string
		for _, g := range r.Groups {
			order = append(order, g.Kind)
		}
		if got := strings.Join(order, ","); got != "table,symbol,route,operation,file" {
			t.Errorf("group order %s", got)
		}
		if r.Totals[searchFile] != 0 || r.Totals[searchRoute] != 2 {
			t.Errorf("totals %v", r.Totals)
		}
		// JS-20, D: calls, columns, diagnostics and file text are not
		// searched.
		for _, q := range []string{"dyn", "user_id", "permission", "boom"} {
			n := searchGet(t, opt, "/api/v0/search?q="+q)
			for _, g := range n.Groups {
				if g.Total != 0 {
					t.Errorf("%s found in %s: %v", q, g.Kind, labels(g.Items))
				}
			}
		}
		// JS-8: files by base name, the shorter key first.
		f := searchGet(t, opt, "/api/v0/search?q=app&kinds=file")
		if got := labels(group(t, f.Groups, searchFile).Items); got != "src/app.go/prefix,web/App.vue/prefix,config/app.json/prefix" {
			t.Errorf("files %s", got)
		}
	})
}

// TestSearchOperations: JS-5.
func TestSearchOperations(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		r := searchGet(t, opt, "/api/v0/search?q=items&kinds=operation")
		g := group(t, r.Groups, searchOperation)
		if !g.Available || g.Total != 3 || g.Items[0].Method == nil || g.Items[0].ID == nil || !strings.HasPrefix(*g.Items[0].ID, "route:") {
			t.Errorf("operations %+v", g)
		}
		if err := os.WriteFile(filepath.Join(root, config.File), []byte("version: 1\nnope: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bad := searchGet(t, opt, "/api/v0/search?q=items&kinds=operation,route")
		g = group(t, bad.Groups, searchOperation)
		if g.Available || len(g.Unavailable) != 1 || g.Unavailable[0].Repository != "fixture" || g.Total != 0 {
			t.Errorf("unavailable %+v", g)
		}
		if group(t, bad.Groups, searchRoute).Total != 3 {
			t.Error("other kinds must still be answered")
		}
	})
}

// TestSearchNoResult: JS-14, JS-18.
func TestSearchNoResult(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		empty := searchGet(t, testOptions(t.TempDir(), s), "/api/v0/search?q=billing")
		if empty.Index != nil || empty.Searched.Indexed || empty.Absence != absenceNotProven || len(empty.Groups) != 5 {
			t.Errorf("not indexed %+v", empty)
		}
		ingestGraph(t, w, unreadGraph(t, "fixture", builtinIndexer))
		r := searchGet(t, testOptions(t.TempDir(), s), "/api/v0/search?q=billing")
		for _, g := range r.Groups {
			if g.Total != 0 || g.BestTier != nil || len(g.Items) != 0 || g.Truncated {
				t.Errorf("group %+v", g)
			}
		}
		if r.Absence != absenceNotProven || !r.Searched.Indexed {
			t.Errorf("absence %s", r.Absence)
		}
		data, _ := json.Marshal(r)
		for _, word := range []string{`"none"`, "not_found", "absent"} {
			if bytes.Contains(data, []byte(word)) {
				t.Errorf("answer says %s", word)
			}
		}
	})
}

// TestSearchFixtureGolden: JS-4, JS-8 on the API fixture, both backends
// equal.
func TestSearchFixtureGolden(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		got := map[string]any{}
		raw := map[string]string{}
		for _, q := range []string{"items", "t016d-api", "client", "Main.rs"} {
			rec := get(t, opt, http.MethodGet, "/api/v0/search?limit=5&q="+q, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", q, rec.Code, rec.Body.String())
			}
			decodeSearch(t, rec.Body.Bytes())
			data, _ := json.Marshal(normalizeAPI(t, rec.Body.Bytes()))
			text := golden.ReplaceRoot(string(data), filepath.ToSlash(root))
			raw[q] = text
			var v any
			if err := json.Unmarshal([]byte(text), &v); err != nil {
				t.Fatal(err)
			}
			got[q] = v
		}
		golden.Check(t, filepath.Join("testdata", "search-fixture.golden.json"), got)
		mu.Lock()
		bodies[t.Name()] = raw
		mu.Unlock()
	})
	if a, b := bodies["TestSearchFixtureGolden/sqlite"], bodies["TestSearchFixtureGolden/postgres"]; a != nil && b != nil && !equalJSON(a, b) {
		t.Error("SQLite and Postgres answers differ")
	}
}

// TestSearchProject: JS-15..JS-17.
func TestSearchProject(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(root, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(root, "backend"), w, "backend")
		if _, _, err := projectlink.Run(ctx, w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		opt := projectOptions(project, s)
		decode := func(path string) searchProjectResponse {
			t.Helper()
			rec := get(t, opt, http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
			}
			var p searchProjectResponse
			dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				t.Fatal(err)
			}
			return p
		}
		p := decode("/api/v0/search?q=users&kinds=route,file,symbol")
		if p.Project != "users" || len(p.Members) != 2 || p.Members[0].Repository != "frontend" {
			t.Fatalf("project %+v", p)
		}
		// JS-15: project totals are the member sums.
		for kind, total := range p.Totals {
			sum := 0
			for _, m := range p.Members {
				sum += m.Totals[kind]
			}
			if sum != total {
				t.Errorf("%s total %d, members %d", kind, total, sum)
			}
		}
		// JS-16: tier first, then the first member's items.
		first := decode("/api/v0/search?q=users&kinds=file&first=backend")
		if first.Members[0].Repository != "backend" {
			t.Errorf("first: %v", first.Members[0].Repository)
		}
		rank := map[string]int{}
		for i, n := range tierNames {
			rank[n] = i
		}
		items := group(t, first.Groups, searchFile).Items
		for i := 1; i < len(items); i++ {
			a, b := items[i-1], items[i]
			if rank[a.Tier] > rank[b.Tier] || rank[a.Tier] == rank[b.Tier] && a.Repository == "frontend" && b.Repository == "backend" {
				t.Errorf("order: %s %s before %s %s", a.Repository, a.Tier, b.Repository, b.Tier)
			}
		}
		// The member's own search with repository=.
		member := searchGet(t, opt, "/api/v0/search?q=users&kinds=file&repository=backend")
		if member.Repository != "backend" || member.Totals[searchFile] != first.Members[0].Totals[searchFile] {
			t.Errorf("member %+v", member)
		}
		for _, bad := range []string{"q=users&first=nope", "q=users&first=backend&repository=backend", "q=users&repository=nope"} {
			if rec := get(t, opt, http.MethodGet, "/api/v0/search?"+bad, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: %d", bad, rec.Code)
			}
		}
		// JS-17: a member without a checkout is searched without links.
		noCheckout := project
		noCheckout.Members = append([]config.Member(nil), project.Members...)
		noCheckout.Members[1].Checkout = false
		opt = projectOptions(noCheckout, s)
		nc := decode("/api/v0/search?q=users&kinds=route,file,symbol")
		if nc.Members[1].Checkout {
			t.Error("checkout reported")
		}
		for _, g := range nc.Groups {
			for _, it := range g.Items {
				if it.Repository == "backend" && it.IDE != nil {
					t.Errorf("link on a member without a checkout: %+v", it)
				}
			}
		}
		if nc.Totals[searchRoute] != p.Totals[searchRoute] {
			t.Error("a member without a checkout must still be searched")
		}
	})
}

// TestSearchProjectOrder checks row JS-16 on the merge itself: a better
// match of another member comes before a weaker match of the first member,
// and within a tier the first member's items come first, each member's in
// its own JS-10 order; limit applies to the merged group.
func TestSearchProjectOrder(t *testing.T) {
	item := func(repo, label string, tier int) rankedItem {
		return rankedItem{match: searchMatch{tier: tier, key: searchKey{name: "path", text: label}}, item: searchItem{Kind: searchFile, Repository: repo, Label: label, Tier: tierNames[tier]}}
	}
	member := func(repo string, items ...rankedItem) memberSearch {
		best := -1
		if len(items) > 0 {
			best = items[0].match.tier
		}
		return memberSearch{repository: repo, groups: map[string]*memberGroup{searchFile: {total: len(items), best: best, items: items}}}
	}
	first := member("web", item("web", "a/users-list.ts", 1), item("web", "b/my-users.ts", 2), item("web", "c/allusers.ts", 2))
	other := member("api", item("api", "users.go", 0), item("api", "x/users_test.go", 1), item("api", "zusers.go", 2))
	for _, tc := range []struct {
		limit int
		want  string
	}{
		{10, "api:users.go,web:a/users-list.ts,api:x/users_test.go,web:b/my-users.ts,web:c/allusers.ts,api:zusers.go"},
		{3, "api:users.go,web:a/users-list.ts,api:x/users_test.go"},
	} {
		groups, totals := mergeGroups(searchRequest{kinds: []string{searchFile}, limit: tc.limit}, []memberSearch{first, other})
		var got []string
		for _, it := range groups[0].Items {
			got = append(got, it.Repository+":"+it.Label)
		}
		if strings.Join(got, ",") != tc.want || totals[searchFile] != 6 || groups[0].Truncated != (tc.limit < 6) {
			t.Errorf("limit %d: %v (total %d, truncated %v)", tc.limit, got, totals[searchFile], groups[0].Truncated)
		}
	}
}

// TestSearchReader checks row JS-21: the in-process reader answers the
// search view with the same JSON as HTTP.
func TestSearchReader(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		r := NewReader(opt)
		defer r.Close()
		q := url.Values{"q": {"user"}}
		got, err := r.Read(context.Background(), searchPath, q)
		if err != nil {
			t.Fatal(err)
		}
		want := get(t, opt, http.MethodGet, searchPath+"?"+q.Encode(), nil)
		var a, b any
		if json.Unmarshal(got, &a) != nil || json.Unmarshal(want.Body.Bytes(), &b) != nil || !jsonEqual(a, b) {
			t.Errorf("reader %s\nHTTP %s", got, want.Body.String())
		}
	})
}

// TestSearchUnknownPrefix: JS-4's unknown-prefix key. A route under a
// mount whose prefix is not a constant (dozzle's r.Route(base, ...)) is
// labelled "…/api/notifications/rules"; any part of that label without
// the "…" finds it, and an exact match still ranks first.
func TestSearchUnknownPrefix(t *testing.T) {
	route := func(qualified string, attrs map[string]any) contract.Node {
		id, err := contract.Identity("route", "src/app.go", qualified)
		if err != nil {
			t.Fatal(err)
		}
		span := &contract.Span{StartLine: 9, EndLine: 9, EndColumn: 1}
		return contract.Node{Type: "node", Identity: id, Kind: "route", Level: 4, Name: qualified, QualifiedName: qualified, File: "src/app.go", Span: span, ContentHash: contract.ContentHash(nil), Attributes: attrs}
	}
	extra := []any{
		route("GET …/api/notifications/rules", map[string]any{"method": "GET", "unresolved_prefix_paths": []any{map[string]any{"template": "/api/notifications/rules", "prefix_reason": "mount_prefix_not_constant"}}}),
		route("GET /api/notifications/rules/extra", map[string]any{"method": "GET", "paths": []any{map[string]any{"template": "/api/notifications/rules/extra", "resolution": "constant"}}}),
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestGraph(t, w, unreadGraphWith(t, "fixture", builtinIndexer, extra))
		opt := testOptions(t.TempDir(), s)
		for _, q := range []string{"notifications/rules", "api/notif", "rules", "/api/notifications/rules"} {
			g := group(t, searchGet(t, opt, "/api/v0/search?kinds=route&q="+url.QueryEscape(q)).Groups, searchRoute)
			if g.Total != 2 || g.Items[0].Label != "…/api/notifications/rules" || g.Items[0].PrefixUnknown == nil || g.Items[0].MatchKey != "prefix_unknown" {
				t.Errorf("%s: %s", q, labels(g.Items))
			}
		}
		// The "…" is not part of the key: only the stored name holds it.
		g := group(t, searchGet(t, opt, "/api/v0/search?kinds=route&q="+url.QueryEscape("…/api")).Groups, searchRoute)
		if g.Total != 1 || g.Items[0].MatchKey != "name" {
			t.Errorf("ellipsis: %s", labels(g.Items))
		}
	})
}
