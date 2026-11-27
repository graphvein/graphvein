package clients

import (
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routes"
)

type routePath struct {
	Template      string `json:"template"`
	Optional      bool   `json:"optional"`
	TrailingSlash bool   `json:"trailing_slash"`
	Scope         string `json:"scope"`
	Resolution    string `json:"resolution"`
	// Assumptions are the path assumptions the entry rests on (GR-V5,
	// RO-V1, RO-V2: routes.PathAssumptionText); a link through the entry
	// carries them (linkedPathAssumptions).
	Assumptions []string `json:"assumptions"`
}

// slashFrameworks are the frameworks whose routers serve a path with and
// without a trailing slash as two routes (the other form gets a redirect
// or a 404, never the handler): FastAPI (ADR 0019) and the Go routers
// net/http, chi and gin (ADR 0076). Equal-template ties of one of them
// with mixed trailing_slash flags keep the paths matching the call's flag.
var slashFrameworks = map[string]bool{"fastapi": true, "net/http": true, "chi": true, "gin": true}

// strictSlashFrameworks are the Go routers that never hand the other
// slash form of a path to the route (TOP-2 of 2026-11-18, ADR 0076): chi
// answers it with a 404, gin (RedirectTrailingSlash, the default) and
// net/http (a subtree pattern's root named without its slash) with a
// redirect the client has to follow, or another route takes it. A path
// entry of theirs matches only a call of its own slash form; a chi "/"
// route under a slash-less prefix claims the bare prefix as an entry of
// its own (row GR-C1), so that form still links. A final catch-all and
// the root have no other form.
var strictSlashFrameworks = map[string]bool{"net/http": true, "chi": true, "gin": true}

// otherSlashForm reports a path entry of a strict-slash router that the
// call names in the other slash form, so the router does not serve it.
func otherSlashForm(framework string, p routePath, callSlash bool) bool {
	if !strictSlashFrameworks[framework] || p.TrailingSlash == callSlash {
		return false
	}
	route := segments(p.Template)
	if len(route) == 0 || strings.HasPrefix(route[len(route)-1], "{*") {
		return false
	}
	return true
}

type route struct {
	identity, method, framework string
	// host is a net/http host pattern's host (T023 review): the route
	// answers only requests for that host.
	host       string
	equivalent string
	span       contract.Span
	paths      []routePath
	// Next.js only (rows NX-L1, NX-L3): the project's literal rewrites and
	// base path, as the route node carries them.
	rewrites *routes.NextRewrites
	basePath *string
	project  string
	// prefixPaths are the route's unresolved_prefix_paths entries (T033):
	// read only by the unknown-prefix tier (linkUnknownPrefix).
	prefixPaths []prefixPath
}

// nextFramework reports the frameworks whose routes are served from the
// application's own origin (ADR 0077 same-origin rule).
func nextFramework(framework string) bool {
	return framework == "nextjs" || framework == "nextjs_pages"
}

type matcher []route

// nextOriginFramework marks an entry of a matcher that is no route but a
// Next.js application directory (withNextOrigins): it names an origin a
// same-origin call may run against. Such an entry has no method and no
// paths, so no call ever matches it.
const nextOriginFramework = "nextjs_origin"

// withNextOrigins returns m with the repository's Next.js application
// directories (routes.NextProjectDirs), so link can tell which project's
// origin a same-origin call is served from (row NX-L1, T024 review).
func (m matcher) withNextOrigins(dirs []string) matcher {
	out := append(matcher(nil), m...)
	for _, dir := range dirs {
		out = append(out, route{framework: nextOriginFramework, project: dir})
	}
	return out
}

// nextOrigin returns the Next.js application whose directory holds file
// (the deepest one), and how many applications the repository has.
func (m matcher) nextOrigin(file string) (project string, count int) {
	found := false
	for _, r := range m {
		if r.framework != nextOriginFramework {
			continue
		}
		count++
		if r.project != "." && !strings.HasPrefix(file, r.project+"/") {
			continue
		}
		if !found || project == "." || len(r.project) > len(project) {
			project, found = r.project, true
		}
	}
	if !found {
		return "", count
	}
	return project, count
}

func newMatcher(nodes []contract.Node, proofs ...map[string]string) matcher {
	var out matcher
	for _, n := range nodes {
		if n.Kind != "route" {
			continue
		}
		r := route{identity: n.Identity}
		if len(proofs) > 0 {
			r.equivalent = proofs[0][n.Identity]
		}
		if n.Span != nil {
			r.span = *n.Span
		}
		r.method, _ = n.Attributes["method"].(string)
		r.framework, _ = n.Attributes["framework"].(string)
		r.host, _ = n.Attributes["host"].(string)
		data, _ := json.Marshal(n.Attributes["paths"])
		_ = json.Unmarshal(data, &r.paths)
		if v, ok := n.Attributes[routes.UnknownPrefixAttribute]; ok {
			data, _ := json.Marshal(v)
			_ = json.Unmarshal(data, &r.prefixPaths)
		}
		if nextFramework(r.framework) {
			if v, ok := n.Attributes["rewrites"]; ok {
				data, _ := json.Marshal(v)
				var rewrites routes.NextRewrites
				if json.Unmarshal(data, &rewrites) == nil {
					r.rewrites = &rewrites
				}
			}
			if base, ok := n.Attributes["base_path"].(string); ok {
				r.basePath = &base
			}
			r.project, _ = n.Attributes["project"].(string)
		}
		out = append(out, r)
	}
	return out
}

// headServesGet are the route frameworks whose GET routes also answer
// HEAD: Express, Axum, Go's net/http ServeMux (T023 decision 8; chi and
// gin answer 405 and 404, FastAPI needs an explicit HEAD) and Next.js App
// Router route handlers (T024a, row NX-L1).
var headServesGet = map[string]bool{"express": true, "axum": true, "net/http": true, "nextjs": true}

func segments(s string) []string { return strings.FieldsFunc(s, func(c rune) bool { return c == '/' }) }
func param(s string) bool        { return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") }
func pathScore(call, route []string) (int, bool) {
	count := 0
	for i, s := range route {
		if i >= len(call) {
			return 0, false
		}
		if i == len(route)-1 && strings.HasPrefix(s, "{*") {
			return count, true
		}
		if param(s) {
			continue
		}
		if param(call[i]) || s != call[i] {
			return 0, false
		}
		count++
	}
	return count, len(call) == len(route)
}
func (m matcher) link(identity, file string, span contract.Span, attrs map[string]any) []contract.Edge {
	method, ok := attrs["method"].(string)
	path, valid := attrs["path_template"].(string)
	if !ok || !valid {
		return nil
	}
	call := segments(path)
	suffix := attrs["base"] == "unresolved" && attrs["origin_resolution"] != "unresolved"
	// An assignment-derived origin is never proven. A literal origin is
	// proven to be the target's only in the project link pass, where a
	// consumes entry names it for the provider (rows PL-R5, PL-L4).
	// An OpenAPI operation's servers reach the matcher only as this flag
	// (Exact): only a consumes entry naming one of them proves them (row
	// LF-1).
	unprovenOrigin := attrs["origin_resolution"] == "unresolved" || attrs["origin"] != nil && attrs["project_link"] != true || attrs["operation_origin"] == "unproven"
	literal := false
	for _, s := range call {
		literal = literal || !param(s)
	}
	bestLiterals, bestMethod := -1, -1
	ties := map[string]string{}
	var tiedPaths []tiedPath
	// overlaps are the routes a call parameter keeps from matching
	// because it stands where the route has a literal segment: at run
	// time the parameter may hold that literal (row LF-2).
	type overlap struct{ identity, scope string }
	// refused records that row LF-1 removed a candidate; without it or
	// LF-2 the alternative took the default branch LF-U.
	refused, overlapped := false, false
	defer func() {
		if !refused && !overlapped {
			linkTag("LF-U")
		}
	}()
	var overlaps []overlap
	plans := map[string]nextPlan{}
	original := call
	// A same-origin call runs against the origin of the Next.js
	// application that serves the page it is in: only that project's
	// routes are candidates. A file outside every application (a shared
	// package) may run in any of them (T024 review).
	origin, origins := m.nextOrigin(file)
	for _, r := range m {
		call := original
		if r.framework == nextOriginFramework {
			continue
		}
		if r.host != "" && !sameHost(attrs["origin"], r.host) {
			// A host-pattern route answers only its host: a call whose
			// origin names another host, or no literal origin, is not
			// linked to it (T023 review).
			continue
		}
		methodScore := 1
		if r.method != method {
			if r.method != "ANY" && !(method == "HEAD" && r.method == "GET" && headServesGet[r.framework]) {
				continue
			}
			methodScore = 0
		}
		if nextFramework(r.framework) {
			// Same-origin rule (NX-L1): a call to a literal origin never
			// reaches a Next.js route, whose origin Graphvein does not know.
			// A project link's literal origin is one a consumes entry
			// declares for the provider (NewProjectRouteMatcher).
			if attrs["origin"] != nil && attrs["project_link"] != true {
				routes.NextTag("NX-L1")
				continue
			}
			if origin != "" && r.project != origin {
				routes.NextTag("NX-L1")
				continue
			}
			// NX-L3: a call a literal rewrite may redirect is withheld from
			// that project's routes, or matched at the rewritten path where
			// Next.js's own order proves the rewrite applies.
			plan, planned := plans[r.project]
			if !planned {
				plan = m.nextPlan(r, call, attrs)
				plans[r.project] = plan
			}
			switch plan.mode {
			case "withhold":
				routes.NextTag("NX-L3")
				attrs["nextjs_rewrite"] = plan.source
				continue
			case "reroute":
				routes.NextTag("NX-L3")
				attrs["nextjs_rewrite"] = plan.source
				attrs["nextjs_rewrite_destination"] = plan.destination
				attrs["nextjs_rewrite_phase"] = plan.phase
				call = plan.path
			}
		}
		for _, p := range r.paths {
			if p.Template == "" {
				continue
			}
			// A route whose base path depends on the deployment matches only
			// calls whose base is unknown too (NX-R4).
			if p.Resolution == "unresolved" && attrs["base"] != "unresolved" {
				continue
			}
			if otherSlashForm(r.framework, p, attrs["trailing_slash"] == true) {
				continue
			}
			variants := [][]string{segments(p.Template)}
			if p.Optional && len(variants[0]) > 0 && param(variants[0][len(variants[0])-1]) {
				variants = append(variants, variants[0][:len(variants[0])-1])
			}
			for _, route := range variants {
				score, match := pathScore(call, route)
				// With an unresolved base, an exact match needs the same
				// anchor as a suffix remainder: the route starts with a
				// literal, or a route parameter takes a call parameter.
				// Otherwise the call's leading literal becomes the value of
				// a route parameter, and the unknown base is free to have
				// supplied that segment instead (T017b Revision 1). The
				// suffix tier below may still match a remainder.
				if match && attrs["base"] == "unresolved" && len(route) > 0 && param(route[0]) && !param(call[0]) {
					score, match = 0, false
				}
				tier := "exact"
				if !match && suffix && literal {
					for at := 1; at < len(route); at++ {
						// A remainder must match at least one literal call
						// segment with a literal route segment (v > 0).
						// Parameters or a catch-all alone leave the unknown
						// base as the only evidence, so they never match.
						// The remainder must also be anchored: its first
						// segment is a literal (equal to call[0], which
						// pathScore checks) or a parameter matched by a call
						// parameter, so no call literal becomes the value of
						// a parameter the matcher placed after the base
						// (T017b Rule P).
						if param(route[at]) && !param(call[0]) {
							continue
						}
						if v, yes := pathScore(call, route[at:]); yes && v > 0 && (!match || v > score) {
							score = v
							match = true
							tier = "suffix"
						}
					}
				}
				if !match {
					if tier == "exact" && overlapsLiteral(call, route) {
						overlaps = append(overlaps, overlap{r.identity, p.Scope})
					}
					continue
				}
				// With an unresolved base, a literal-free route matches only
				// by segment count, which the unknown base makes no evidence:
				// such a route is never a candidate, whatever the origin
				// (T017b Rule E, Revision 0).
				if score == 0 && attrs["base"] == "unresolved" {
					continue
				}
				// A call with an origin nothing proves to be the
				// repository's own server (row LF-1) never reaches a
				// literal-free route either: no literal segment says the
				// request is meant for it, and a catch-all accepts every
				// path of every host. A host-pattern route that names the
				// call's host is that proof.
				if score == 0 && unprovenOrigin && r.host == "" {
					linkTag("LF-1")
					refused = true
					continue
				}
				if score > bestLiterals || score == bestLiterals && methodScore > bestMethod {
					bestLiterals = score
					bestMethod = methodScore
					ties = map[string]string{}
					tiedPaths = nil
				}
				if score == bestLiterals && methodScore == bestMethod {
					tiedPaths = append(tiedPaths, tiedPath{r.identity, r.framework, p.Template, p.TrailingSlash, p.Scope, tier, p.Assumptions})
					if ties[r.identity] != "exact" {
						ties[r.identity] = tier
					}
				}
			}
		}
	}
	if len(ties) > 1 && len(tiedPaths) > 0 {
		sameRouter, matches, differs := slashFrameworks[tiedPaths[0].framework], false, false
		slash := attrs["trailing_slash"] == true
		for _, p := range tiedPaths {
			sameRouter = sameRouter && p.framework == tiedPaths[0].framework && p.template == tiedPaths[0].template
			matches = matches || p.slash == slash
			differs = differs || p.slash != slash
		}
		if sameRouter && matches && differs {
			keep := map[string]bool{}
			var selected []tiedPath
			for _, p := range tiedPaths {
				if p.slash == slash {
					keep[p.identity] = true
					selected = append(selected, p)
				}
			}
			tiedPaths = selected
			for id := range ties {
				if !keep[id] {
					delete(ties, id)
				}
			}
		}
	}
	testOnly := false
	if attrs["scope"] != "test" && len(ties) > 0 {
		production := map[string]string{}
		for _, p := range tiedPaths {
			if p.scope != "test" && production[p.identity] != "exact" {
				production[p.identity] = p.tier
			}
		}
		if len(production) > 0 {
			ties = production
		} else {
			testOnly = true
		}
	}
	if len(ties) == 0 {
		// No candidate in the existing tiers: the unknown-prefix tier
		// (rows UP-13..UP-20) may still find one.
		return m.linkUnknownPrefix(identity, file, span, attrs, original, method)
	}
	ids := make([]string, 0, len(ties))
	for id := range ties {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if testOnly && m.productionPrefixRival(attrs, original, method) {
		// Precision correction (T057): only test-scope composed paths
		// match, but a production route below an unknown prefix may serve
		// the call, so "only test routes match" is not proven. The
		// alternative is unmatched, never linked to either.
		attrs["status"] = "unmatched"
		return nil
	}
	if testOnly {
		attrs["status"] = "test_only"
		attrs["candidate_count"] = 0
		if len(ids) > 1 {
			attrs["candidate_count"] = len(ids)
		}
		if len(ids) > 10 {
			ids = ids[:10]
		}
		attrs["candidates"] = ids
		return nil
	}
	if len(ids) > 1 {
		proof := ""
		first := route{}
		equivalent := true
		for _, r := range m {
			if _, tied := ties[r.identity]; !tied {
				continue
			}
			if r.framework != "express" || r.equivalent == "" || proof != "" && proof != r.equivalent {
				equivalent = false
				break
			}
			proof = r.equivalent
			if first.identity == "" || sourceBefore(r.span, first.span) {
				first = r
			}
		}
		template, scope := "", ""
		for _, p := range tiedPaths {
			if _, tied := ties[p.identity]; !tied || attrs["scope"] != "test" && p.scope == "test" {
				continue
			}
			if template != "" && (template != p.template || scope != p.scope) {
				equivalent = false
			}
			template, scope = p.template, p.scope
		}
		if equivalent && first.identity != "" {
			ids = []string{first.identity}
		}
	}
	// A final catch-all wins only by default: every more specific route
	// is tried or registered before it. When a call parameter covers a
	// literal segment of another eligible route, that route may serve the
	// request instead, so the catch-all is only one candidate (row LF-2).
	if len(ids) == 1 && len(overlaps) > 0 && catchAllOnly(ids[0], tiedPaths) {
		set := map[string]bool{ids[0]: true}
		for _, o := range overlaps {
			if attrs["scope"] == "test" || o.scope != "test" {
				set[o.identity] = true
			}
		}
		if len(set) > 1 {
			ids = ids[:0]
			for id := range set {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			attrs["catch_all_overlap"] = true
			overlapped = true
			linkTag("LF-2")
		}
	}
	// Row UP-22: every remaining candidate is a final
	// catch-all and a route below an unknown prefix matches the call under
	// row UP-16: ambiguous, the catch-alls and those routes as candidates.
	if attrs["catch_all_overlap"] != true && allCatchAll(ids, tiedPaths) {
		if rivals := m.catchAllRival(attrs, original, method); len(rivals) > 0 {
			set := map[string]bool{}
			for _, id := range append(append([]string(nil), ids...), rivals...) {
				set[id] = true
			}
			ids = ids[:0]
			for id := range set {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			attrs["catch_all_overlap"] = true
		}
	}
	if origin == "" && origins > 1 {
		for _, r := range m {
			if _, tied := ties[r.identity]; tied && nextFramework(r.framework) {
				// A shared module's call may run in any of the
				// applications: a route of one of them is only a
				// candidate.
				routes.NextTag("NX-L1")
				attrs["nextjs_origin"] = "unknown"
				break
			}
		}
	}
	if attrs["catch_all_overlap"] == true {
		delete(attrs, "catch_all_overlap")
		attrs["status"] = "ambiguous"
		attrs["candidate_count"] = len(ids)
		if len(ids) > 10 {
			ids = ids[:10]
		}
		attrs["candidates"] = ids
		return nil
	}
	if len(ids) > 1 || attrs["nextjs_origin"] == "unknown" {
		attrs["status"] = "ambiguous"
		attrs["candidate_count"] = len(ids)
		if len(ids) > 10 {
			ids = ids[:10]
		}
		attrs["candidates"] = ids
		return nil
	}
	tier := ties[ids[0]]
	pathAssumes := linkedPathAssumptions(ids[0], tier, tiedPaths, attrs["scope"] == "test")
	attrs["status"] = "linked"
	nextBase := false
	for _, r := range m {
		if r.identity == ids[0] && nextFramework(r.framework) {
			routes.NextTag("NX-L1")
			attrs["assumes"] = []string{"NX-V1"}
			if plan := plans[r.project]; plan.mode == "reroute" && plan.phase != "beforeFiles" {
				attrs["assumes"] = []string{"NX-V1", "NX-V3"}
			}
			nextBase = unresolvedBasePath(r)
		}
	}
	if nextBase {
		// Decision U6 of T033: a Next.js route whose basePath is not read
		// (row NX-R4) is served below a prefix the code does not fix, as
		// an unknown-prefix route is; only a call whose own base is
		// unresolved reaches it, taken to be that same prefix. The link is
		// aligned with the unknown-prefix tier (row UP-19): suffix, 0.6,
		// route_prefix unresolved, assumptions UP-V1 and UP-V2 after the
		// Next.js ones. Which route links does not change.
		tier = "suffix"
		attrs["route_prefix"] = "unresolved"
		attrs["assumes"] = append(attrs["assumes"].([]string), AssumeUnknownPrefix, AssumeSamePrefix)
	}
	if len(pathAssumes) > 0 {
		list, _ := attrs["assumes"].([]string)
		attrs["assumes"] = append(append([]string(nil), list...), pathAssumes...)
	}
	attrs["match"] = tier
	confidence := 0.9
	if attrs["origin"] != nil || attrs["origin_resolution"] == "unresolved" {
		confidence = 0.7
		if origin, assumed := assumedOrigin(m, ids[0], attrs); assumed {
			attrs["origin_assumption"] = OriginAssumption(origin, "this repository")
		}
	}
	if tier == "suffix" {
		confidence = 0.6
	}
	return []contract.Edge{{Type: "edge", Kind: "calls_route", Source: identity, Target: ids[0], Provenance: "inferred", Confidence: confidence, Evidence: []contract.Evidence{{File: file, Span: span}}}}
}

// assumedOrigin reports the origin a link of a call with an origin
// assumes to be the target's server, when nothing proves it (row LF-1's
// proofs: a host-pattern route naming the host, or a project link, where a
// consumes entry declares the literal origin). The origin is the stored
// literal, or "(not resolved statically)" for an assignment-derived one.
func assumedOrigin(m matcher, target string, attrs map[string]any) (string, bool) {
	if attrs["project_link"] == true && attrs["origin_resolution"] != "unresolved" {
		return "", false
	}
	for _, r := range m {
		if r.identity == target && r.host != "" {
			return "", false
		}
	}
	if origin, ok := attrs["origin"].(string); ok && origin != "" {
		return origin, true
	}
	return UnresolvedOrigin, true
}

// UnresolvedOrigin names an assignment-derived origin in an origin
// assumption.
const UnresolvedOrigin = "(not resolved statically)"

// OriginAssumption is the shown assumption of a 0.7 link whose origin is
// not proven (T034 follow-up): "origin <origin> assumed to be <server>'s
// server", server "this repository" inside one repository or the provider
// repository's name in the project link pass.
func OriginAssumption(origin, server string) string {
	return "origin " + origin + " assumed to be " + server + "'s server"
}

// overlapsLiteral reports whether route would match call if each call
// parameter could hold any literal, and at least one call parameter stands
// where the route has a literal segment (row LF-2).
func overlapsLiteral(call, route []string) bool {
	covered := false
	for i, s := range route {
		if i >= len(call) {
			return false
		}
		if i == len(route)-1 && strings.HasPrefix(s, "{*") {
			return covered
		}
		if param(s) {
			continue
		}
		if param(call[i]) {
			covered = true
			continue
		}
		if s != call[i] {
			return false
		}
	}
	return covered && len(call) == len(route)
}

// tiedPath is one route path entry among the best candidates of a call.
// unresolvedBasePath reports a Next.js route whose project's basePath is
// not read as a literal: its path entries hold the template without the
// base (row NX-R4, resolution "unresolved").
func unresolvedBasePath(r route) bool {
	for _, p := range r.paths {
		if p.Resolution == "unresolved" {
			return true
		}
	}
	return false
}

type tiedPath struct {
	identity, framework, template string
	slash                         bool
	scope, tier                   string
	// assumptions are the entry's path assumptions (routePath.Assumptions).
	assumptions []string
}

// linkedPathAssumptions are the path assumptions a link to identity rests
// on: the entries through which the route won (its
// tier, and production entries for a production call) are read; when one
// of them rests on none, the link needs none (a path that is served
// without an assumption reaches the route), otherwise it is the union of
// their ids in entry order. The ids are the route side's (GR-V5, RO-V1,
// RO-V2), carried on the link's assumes as UP-V1 and UP-V2 are.
func linkedPathAssumptions(identity, tier string, tied []tiedPath, test bool) []string {
	var out []string
	found := false
	for _, p := range tied {
		if p.identity != identity || p.tier != tier || !test && p.scope == "test" {
			continue
		}
		if len(p.assumptions) == 0 {
			return nil
		}
		found = true
		for _, id := range p.assumptions {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	if !found {
		return nil
	}
	return out
}

// allCatchAll reports whether every candidate in ids is tied only through
// paths ending in a final catch-all (row UP-22).
func allCatchAll(ids []string, tied []tiedPath) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if !catchAllOnly(id, tied) {
			return false
		}
	}
	return true
}

// catchAllOnly reports whether every path through which identity is tied
// ends in a final catch-all.
func catchAllOnly(identity string, tied []tiedPath) bool {
	found := false
	for _, p := range tied {
		if p.identity != identity {
			continue
		}
		route := segments(p.template)
		if len(route) == 0 || !strings.HasPrefix(route[len(route)-1], "{*") {
			return false
		}
		found = true
	}
	return found
}

// rewritten reports the first literal rewrite source of r's project that
// the call path may match. A call parameter may match any source segment;
// with an unknown base (of the call or the route) any suffix of the call
// is tried, since the unknown part may hold the prefix.
func rewritten(r route, call []string, unresolved bool) (string, bool) {
	if r.rewrites == nil || r.rewrites.Status != "literal" {
		return "", false
	}
	for _, rule := range r.rewrites.Rules {
		source := segments(rule.Source)
		if r.basePath != nil {
			source = append(segments(*r.basePath), source...)
		} else {
			unresolved = true
		}
		for start := 0; start <= len(call); start++ {
			if sourceMatches(source, call[start:]) {
				return rule.Source, true
			}
			if !unresolved {
				break
			}
		}
	}
	return "", false
}

// sourceMatches matches path-to-regexp segments (literal, :n, :n?, :n*,
// :n+) against call segments.
func sourceMatches(source, call []string) bool {
	if len(source) == 0 {
		return len(call) == 0
	}
	s := source[0]
	if !strings.HasPrefix(s, ":") {
		// Next.js matches sources case-insensitively by default.
		if len(call) == 0 || !param(call[0]) && !strings.EqualFold(call[0], s) {
			return false
		}
		return sourceMatches(source[1:], call[1:])
	}
	min, many := 1, false
	switch s[len(s)-1] {
	case '?':
		min = 0
	case '*':
		min, many = 0, true
	case '+':
		many = true
	}
	max := 1
	if many {
		max = len(call)
	}
	for n := min; n <= max && n <= len(call); n++ {
		if sourceMatches(source[1:], call[n:]) {
			return true
		}
	}
	return false
}

func sourceBefore(a, b contract.Span) bool {
	left := [4]int{a.StartLine, a.StartColumn, a.EndLine, a.EndColumn}
	right := [4]int{b.StartLine, b.StartColumn, b.EndLine, b.EndColumn}
	for i := range left {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

// nextPlan is how a call relates to one project's literal rewrites (row
// NX-L3): mode "" (no rule may match), "withhold" (a rule may match, but
// whether or where it rewrites is not proven) or "reroute" (the request
// is served at path, the rewrite's destination).
type nextPlan struct {
	mode, source, destination, phase string
	path                             []string
}

// nextPhases is the order Next.js applies rewrites in (resolve-routes.js):
// beforeFiles, then an exact file-system check, afterFiles, a file-system
// and dynamic route check, then fallback.
var nextPhases = map[string]int{"beforeFiles": 0, "afterFiles": 1, "fallback": 2}

// nextPlan decides row NX-L3 for the project of r. With an unknown base
// (of the call or of the project) any rule that may match withholds, as
// before. Otherwise the first rule in Next.js order that may match
// decides. An afterFiles rule first yields to a static route of the
// project that surely serves the call path, a fallback rule to any such
// route (the call is then matched as without the rule), whatever the
// rule's destination; a route that only may serve it withholds. Then the
// rule must match surely, with a same-origin destination of literal
// segments and source parameters, and no later rule may match the
// rewritten path (chains are not followed). Anything else withholds.
func (m matcher) nextPlan(r route, call []string, attrs map[string]any) nextPlan {
	if r.rewrites == nil || r.rewrites.Status != "literal" {
		return nextPlan{}
	}
	if attrs["base"] == "unresolved" || r.basePath == nil {
		if source, ok := rewritten(r, call, true); ok {
			return nextPlan{mode: "withhold", source: source}
		}
		return nextPlan{}
	}
	rules := append([]routes.NextRewriteRule(nil), r.rewrites.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return nextPhases[rules[i].Phase] < nextPhases[rules[j].Phase] })
	base := *r.basePath
	slash := attrs["trailing_slash"] == true
	for i, rule := range rules {
		destination := rule.Destination
		if strings.HasPrefix(destination, "/") {
			destination = withBase(base, destination)
		}
		path, outcome := routes.NextRewrite(withBase(base, rule.Source), destination, call, slash)
		if outcome == routes.RewriteNoMatch {
			continue
		}
		withhold := nextPlan{mode: "withhold", source: rule.Source}
		// Next.js checks the file system before afterFiles rules (static
		// routes) and every route before fallback rules, whatever the
		// rule's destination: a route that surely serves the call path
		// wins before any outcome of the rule is considered (T024 review);
		// one that only may serve it leaves the call withheld.
		if rule.Phase != "beforeFiles" {
			if served, sure := m.nextServes(r.project, call, rule.Phase == "afterFiles"); served {
				if sure {
					return nextPlan{}
				}
				return withhold
			}
		}
		if outcome != routes.RewriteApplied {
			return withhold
		}
		if !m.nextChainEnds(r.project, base, rules, i, path) {
			return withhold
		}
		return nextPlan{mode: "reroute", source: rule.Source, destination: rule.Destination, phase: rule.Phase, path: path}
	}
	return nextPlan{}
}

// nextChainEnds reports whether no rule after rules[i] can rewrite the
// rewritten path again. Next.js keeps going down its list after a
// rewrite: an afterFiles or fallback rule is followed by a check of every
// route, the beforeFiles rules by an exact check of static routes, and
// any route that surely serves the path ends the chain there. Otherwise a
// later rule that may match the path means the chain is not followed, so
// the plan withholds.
func (m matcher) nextChainEnds(project, base string, rules []routes.NextRewriteRule, i int, path []string) bool {
	served := func(static bool) bool {
		_, sure := m.nextServes(project, path, static)
		return sure
	}
	if rules[i].Phase != "beforeFiles" && served(false) {
		return true
	}
	after := rules[i].Phase != "beforeFiles"
	checkedFiles, checkedRoutes := after, after
	for _, later := range rules[i+1:] {
		if later.Phase != "beforeFiles" && !checkedFiles {
			checkedFiles = true
			if served(true) {
				return true
			}
		}
		if later.Phase == "fallback" && !checkedRoutes {
			checkedRoutes = true
			if served(false) {
				return true
			}
		}
		if routes.NextRewriteMayMatch(withBase(base, later.Source), path) {
			return false
		}
	}
	return true
}

// withBase prefixes a rewrite path with the project's base path the way
// Next.js does (load-custom-routes.js processRoutes).
func withBase(base, path string) string {
	if base != "" && path == "/" {
		return base
	}
	return base + path
}

// nextServes reports whether a Next.js route of project may serve the
// call path (served) and whether one surely does (sure): static routes
// only for the exact file-system check before afterFiles rewrites, every
// route for the check before fallback rewrites. Methods do not matter:
// Next.js resolves the file by path.
func (m matcher) nextServes(project string, call []string, static bool) (served, sure bool) {
	for _, r := range m {
		if !nextFramework(r.framework) || r.project != project {
			continue
		}
		for _, p := range r.paths {
			route := segments(p.Template)
			variants := [][]string{route}
			if p.Optional && len(route) > 0 {
				variants = append(variants, route[:len(route)-1])
			}
			for _, v := range variants {
				dynamic := false
				for _, s := range v {
					dynamic = dynamic || param(s)
				}
				if static && (dynamic || p.Optional) {
					continue
				}
				if looseMatch(v, call) {
					served = true
				}
				if _, match := pathScore(call, v); match {
					sure = true
				}
			}
		}
	}
	return served, sure
}

// looseMatch reports whether a route path may match the call: a call
// parameter may equal any literal, and literals compare without case.
func looseMatch(route, call []string) bool {
	for i, s := range route {
		if i == len(route)-1 && strings.HasPrefix(s, "{*") {
			return len(call) > i
		}
		if i >= len(call) {
			return false
		}
		if param(s) || param(call[i]) || strings.EqualFold(s, call[i]) {
			continue
		}
		return false
	}
	return len(call) == len(route)
}

// nextOrigins lists the repository's Next.js application directories
// (routes.NextProjectDirs); a helper because the linkers name their route
// list "routes", shadowing the package.
func nextOrigins(sources map[string][]byte) []string {
	return routes.NextProjectDirs(sources)
}

// sameHost reports a call origin (scheme://host[:port], a string) whose
// host is host, as Go's ServeMux compares a host pattern with the request's
// host without its port (ASCII case folded, as URL hosts are).
func sameHost(origin any, host string) bool {
	o, ok := origin.(string)
	if !ok || o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Hostname(), host)
}
