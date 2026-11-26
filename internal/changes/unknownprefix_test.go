package changes

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// prefixStream is one route without a full path (dozzle's form: a chi
// Route under a configured base) whose unresolved_prefix_paths attribute
// is entries (a JSON list), or absent when entries is "". The route's
// identity and content hash never change: entries depend on a hop in
// another function.
func prefixStream(entries string) string {
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	attrs := `"framework":"chi","method":"GET","pattern":"/version","handler":null,"handler_inline":false,"middleware":[],"paths":[]`
	if entries != "" {
		attrs += `,"unresolved_prefix_paths":` + entries
	}
	lines := []string{
		`{"type":"file","path":"web/routes.go","language":"go","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("a", 64) + `"}`,
		`{"type":"node","identity":"route:web/routes.go#GET%20%2Fversion","kind":"route","level":4,"name":"GET /version","qualified_name":"GET /version","file":"web/routes.go","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("c", 64) + `","attributes":{` + attrs + `}}`,
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"prefix","generated_at":"2026-11-23T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":1,"node":1,"edge":0,"diagnostic":0}}`, len(lines))
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestUnknownPrefixPathsField covers T033 row UP-31: the route's
// unresolved_prefix_paths is a C3 field, known_now when it appears,
// changed when it changes and unknown_now when it goes, while identity and
// content hash stay the same; a route with it on neither side has no such
// field.
func TestUnknownPrefixPathsField(t *testing.T) {
	entry := func(template string) string {
		return `[{"template":"` + template + `","prefix":"unresolved","prefix_reason":"route_prefix_unresolved","hop":"mount:web/routes.go#createRouter r.Route(base)","conditional":false,"resolution":"literal"}]`
	}
	for _, tc := range []struct {
		name         string
		before, head string
		want         string
	}{
		{"appears", "", entry("/api/version"), KnownNow},
		{"changes", entry("/api/version"), entry("/v2/version"), Changed},
		{"goes", entry("/api/version"), "", UnknownNow},
		{"absent", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, stream := range []string{prefixStream(tc.before), prefixStream(tc.head)} {
				if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
					t.Fatal(err)
				}
			}
			c, err := Compare(ctx, s, "prefix", Base{Kind: BasePrevious}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			var route *Item
			for i := range c.items {
				if c.items[i].Row == "C3" {
					route = &c.items[i]
				}
			}
			if tc.want == "" {
				if route != nil {
					t.Fatalf("route item = %+v, want none", route)
				}
				return
			}
			if route == nil || len(route.Fields) != 1 || route.Fields[0].Name != "unresolved_prefix_paths" || route.Fields[0].Change != tc.want {
				t.Fatalf("route item = %+v, want %s on unresolved_prefix_paths", route, tc.want)
			}
		})
	}
}

// impactStream is a handler in web/routes.go exposing a route below a
// configured base (unknown-prefix entries only), and a client call in
// web/app.ts linked to it by the unknown-prefix tier (assumes UP-V1 and
// UP-V2 on its linked path entry). handlerHash varies the handler's body.
func impactStream(handlerHash string) string {
	span := `{"start_line":1,"start_column":0,"end_line":1,"end_column":10}`
	route := "route:web/routes.go#GET%20%2Fversion"
	call := "client_call:web/app.ts#load.GET%20%2Fversion"
	handler := "function:web/routes.go#version"
	lines := []string{
		`{"type":"file","path":"web/app.ts","language":"typescript","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat("b", 64) + `"}`,
		`{"type":"file","path":"web/routes.go","language":"go","precision":"precise","size":4,"content_hash":"sha256:` + strings.Repeat(handlerHash, 64) + `"}`,
		`{"type":"node","identity":"` + handler + `","kind":"function","level":3,"name":"version","qualified_name":"version","file":"web/routes.go","span":` + span + `,"content_hash":"sha256:` + strings.Repeat(handlerHash, 64) + `","attributes":{}}`,
		`{"type":"node","identity":"` + route + `","kind":"route","level":4,"name":"GET /version","qualified_name":"GET /version","file":"web/routes.go","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("c", 64) + `","attributes":{"framework":"chi","method":"GET","pattern":"/version","handler":"` + handler + `","handler_inline":false,"middleware":[],"paths":[],"unresolved_prefix_paths":[{"template":"/api/version","prefix":"unresolved","prefix_reason":"route_prefix_unresolved","hop":"mount:web/routes.go#createRouter r.Route(base)","conditional":false,"resolution":"literal"}]}}`,
		`{"type":"node","identity":"` + call + `","kind":"client_call","level":4,"name":"GET /version","qualified_name":"load.GET /version","file":"web/app.ts","span":` + span + `,"content_hash":"sha256:` + strings.Repeat("d", 64) + `","attributes":{"method":"GET","status":"linked","assumes":["UP-V1","UP-V2"],"paths":[{"template":"/api/version","status":"linked","match":"suffix","route_prefix":"unresolved","assumes":["UP-V1","UP-V2"],"candidates":["` + route + `"]}]}}`,
		`{"type":"edge","kind":"exposes_route","source":"` + handler + `","target":"` + route + `","provenance":"fact","confidence":1,"evidence":[{"file":"web/routes.go","span":` + span + `}]}`,
		`{"type":"edge","kind":"calls_route","source":"` + call + `","target":"` + route + `","provenance":"inferred","confidence":0.6,"evidence":[{"file":"web/app.ts","span":` + span + `}]}`,
	}
	header := `{"type":"header","contract_version":1,"indexer":{"name":"fixture-indexer","version":"1"},"repository":"prefiximpact","generated_at":"2026-11-23T00:00:00Z"}`
	footer := fmt.Sprintf(`{"type":"footer","counts":{"records":%d,"file":2,"node":3,"edge":2,"diagnostic":0}}`, len(lines))
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer + "\n"
}

// TestUnknownPrefixImpact covers T033 row UP-32: an impact path that
// reaches a route below an unknown prefix carries its prefix_unknown_paths
// (never route_paths), and the calls_route step into its caller carries
// the link's UP assumptions.
func TestUnknownPrefixImpact(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, stream := range []string{impactStream("a"), impactStream("e")} {
		if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Compare(ctx, s, "prefiximpact", Base{Kind: BasePrevious}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Impact(ctx, s, c, ImpactRequest{ID: "function:web/routes.go#version", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	var route, call *Path
	for i := range r.Paths {
		switch r.Paths[i].Kind {
		case "route":
			route = &r.Paths[i]
		case "client_call":
			call = &r.Paths[i]
		}
	}
	if route == nil || call == nil {
		t.Fatalf("paths = %+v, want the route and its call", r.Paths)
	}
	if route.RoutePaths == nil || len(*route.RoutePaths) != 0 {
		t.Errorf("route_paths = %v, want an empty list (no full path)", route.RoutePaths)
	}
	if route.PrefixUnknownPaths == nil || len(*route.PrefixUnknownPaths) != 1 || (*route.PrefixUnknownPaths)[0].Path != "…/api/version" {
		t.Errorf("prefix_unknown_paths = %+v", route.PrefixUnknownPaths)
	}
	last := call.Steps[len(call.Steps)-1]
	if last.Kind != "calls_route" || len(last.LinkAssumptions) != 2 || last.LinkAssumptions[0].ID != "UP-V1" || last.LinkAssumptions[1].ID != "UP-V2" || last.LinkAssumptions[0].Text == "" {
		t.Errorf("calls_route step = %+v, want UP-V1 and UP-V2 with their texts", last)
	}
	for _, st := range call.Steps[:len(call.Steps)-1] {
		if len(st.LinkAssumptions) != 0 {
			t.Errorf("step %s carries link assumptions", st.Kind)
		}
	}
}

// TestShadowedRouteImpact: an impact path that reaches a route whose every
// composed path an earlier mount takes (shadowed_paths) carries
// route_shadowed beside its empty route_paths, so a reader does not call
// the mounted route "not mounted"; a route without it has no flag.
func TestShadowedRouteImpact(t *testing.T) {
	unknown := regexp.MustCompile(`"unresolved_prefix_paths":\[[^\]]*\]`)
	for _, tc := range []struct {
		name, attribute string
		want            bool
	}{
		{"shadowed", `"shadowed_paths":[{"path":"/version","by":"app.mount(/version) (web/app.py:3)"}]`, true},
		{"unmounted", `"other":null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, hash := range []string{"a", "e"} {
				stream := unknown.ReplaceAllString(impactStream(hash), tc.attribute)
				if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
					t.Fatal(err)
				}
			}
			c, err := Compare(ctx, s, "prefiximpact", Base{Kind: BasePrevious}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			r, err := Impact(ctx, s, c, ImpactRequest{ID: "function:web/routes.go#version", Depth: 2})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range r.Paths {
				if p.Kind != "route" {
					continue
				}
				found = true
				if p.RoutePaths == nil || len(*p.RoutePaths) != 0 || p.RouteShadowed != tc.want {
					t.Errorf("route path = %+v, want empty route_paths and route_shadowed %t", p, tc.want)
				}
			}
			if !found {
				t.Fatalf("paths = %+v, want the route", r.Paths)
			}
		})
	}
}

// TestWithheldRouteChanges: a route registration whose path the Go linker
// withholds (couldn't-read row CR-44: its route_mount_unresolved
// diagnostic names the route's own span) is marked path_withheld on the
// head side of its change item and route_withheld on an impact path that
// reaches it, so a reader says "path withheld", never "not mounted". The
// base side of a previous-run comparison keeps no diagnostics and is never
// marked; the same message at another span marks nothing.
func TestWithheldRouteChanges(t *testing.T) {
	unknown := regexp.MustCompile(`"unresolved_prefix_paths":\[[^\]]*\]`)
	footer := regexp.MustCompile(`"records":(\d+),(.*)"diagnostic":0`)
	stream := func(handlerHash, routeHash, at string) string {
		s := unknown.ReplaceAllString(impactStream(handlerHash), `"other":null`)
		s = strings.Replace(s, `"content_hash":"sha256:`+strings.Repeat("c", 64), `"content_hash":"sha256:`+strings.Repeat(routeHash, 64), 1)
		if at == "" {
			return s
		}
		diagnostic := `{"type":"diagnostic","file":"web/routes.go","severity":"info","code":"route_mount_unresolved","message":"GET \"/version\" (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it) at ` + at + `"}`
		i := strings.LastIndex(s, `{"type":"footer"`)
		s = s[:i] + diagnostic + "\n" + s[i:]
		m := footer.FindStringSubmatch(s)
		records, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		return footer.ReplaceAllString(s, `"records":`+strconv.Itoa(records+1)+`,${2}"diagnostic":1`)
	}
	for _, tc := range []struct {
		name, at string
		want     bool
	}{
		{"withheld", "1:0-1:10", true},
		{"other site", "2:0-2:10", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			// The base run states the same diagnostic: it is not kept
			// for a journal run, so the base side is never marked.
			for _, stream := range []string{stream("a", "c", tc.at), stream("e", "f", tc.at)} {
				if err := ingest.Stream(ctx, strings.NewReader(stream), s); err != nil {
					t.Fatal(err)
				}
			}
			c, err := Compare(ctx, s, "prefiximpact", Base{Kind: BasePrevious}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			page, err := c.Items(Filter{Kind: "route"}, Page{})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 || page.Items[0].After == nil || page.Items[0].Before == nil {
				t.Fatalf("items = %+v, want the changed route", page.Items)
			}
			if it := page.Items[0]; it.After.PathWithheld != tc.want || it.Before.PathWithheld {
				t.Errorf("path_withheld head %t base %t, want head %t, base false", it.After.PathWithheld, it.Before.PathWithheld, tc.want)
			}
			r, err := Impact(ctx, s, c, ImpactRequest{ID: "function:web/routes.go#version", Depth: 2})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range r.Paths {
				if p.Kind == "route" {
					found = true
					if p.RouteWithheld != tc.want || p.RouteShadowed {
						t.Errorf("route path = %+v, want route_withheld %t", p, tc.want)
					}
				}
			}
			if !found {
				t.Fatalf("paths = %+v, want the route", r.Paths)
			}
		})
	}
}
