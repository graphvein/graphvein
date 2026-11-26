package clients

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// RouteMatcher shares client selection rules with declarations that have no
// unresolved deployment base and therefore must never use suffix matching.
type RouteMatcher struct {
	routes matcher
	// project marks a matcher of the project link pass that holds another
	// repository's Next.js routes (NewProjectRouteMatcher).
	project bool
}

// NewRouteMatcher retains source proofs needed to select equivalent Express
// registrations without adding those proofs to public route attributes.
func NewRouteMatcher(files map[string]facts.Facts, modules *typescript.ModuleResolver, nodes []contract.Node) RouteMatcher {
	return RouteMatcher{routes: newMatcher(nodes, routesProof(files, modules, nodes))}
}

// ExactRequest preserves custom method spelling and FastAPI's slash distinction.
type ExactRequest struct {
	Method, Template, Scope string
	TrailingSlash           bool
	// UnprovenOrigin marks an operation path entry whose literal origins
	// (the spec's servers) nothing proves to be the routes' server: set by
	// the in-repository correspondence for every entry with origins, and by
	// the project link pass unless a consumes entry names one of them. Row
	// LF-1 then applies as to a call.
	UnprovenOrigin bool
}

// ExactResult carries the same bounded candidates and selection status as clients.
type ExactResult struct {
	Status, Match  string
	Candidates     []string
	CandidateCount int
	// Assumes lists the row assumptions a link rests on, as the
	// in-repository linker records them: a Next.js route's (NX-V1,
	// NX-V3), the unknown-prefix tier's (UP-V1, UP-V2) and the matched
	// route path entry's (GR-V5, RO-V1, RO-V2).
	Assumes []string
}

// Exact uses the existing matcher with a resolved base, disabling suffix fallback.
func (m RouteMatcher) Exact(request ExactRequest) ExactResult {
	routes := m.routes
	for _, c := range request.Method {
		if c >= 'a' && c <= 'z' {
			// llhttp rejects lowercase method tokens before these frameworks
			// dispatch, including registrations accepting ANY method.
			routes = make(matcher, 0, len(m.routes))
			for _, r := range m.routes {
				if r.framework != "express" && r.framework != "fastapi" && !nextFramework(r.framework) {
					routes = append(routes, r)
				}
			}
			break
		}
	}
	attrs := map[string]any{
		"method": request.Method, "path_template": request.Template,
		"scope": request.Scope, "trailing_slash": request.TrailingSlash,
		"base": "none", "origin": nil, "status": "unmatched",
		"candidates": []string{}, "candidate_count": 0,
	}
	if m.project {
		attrs["project_link"] = true
	}
	if request.UnprovenOrigin {
		attrs["operation_origin"] = "unproven"
	}
	// An operation path is no call alternative: the unknown-prefix tier
	// (rows UP-13..UP-20) never takes it.
	attrs["exact_only"] = true
	edges := routes.link("", "", contract.Span{}, attrs)
	result := ExactResult{Status: attrs["status"].(string), Candidates: attrs["candidates"].([]string), CandidateCount: attrs["candidate_count"].(int)}
	if len(edges) > 0 {
		result.Match = "exact"
		result.Candidates = []string{edges[0].Target}
		result.Assumes, _ = attrs["assumes"].([]string)
	}
	return result
}

// NewStoredRouteMatcher builds a matcher from stored route nodes alone, for
// the project link pass (ADR 0075), which reads routes from the store and
// has no parse facts. ADR 0022's Express equivalence proof comes from the
// stored equivalent_registrations attribute: two routes are equivalent
// exactly when they carry the same group, which the routes extractor
// writes only for registrations that share one proof. So ties collapse to
// the first registration as they do inside one repository.
//
// Next.js route handlers and Pages Router API routes are left out: this
// matcher replays a repository's own result (the scope overlay, PL-S1 to
// PL-S3), where the same-origin rule (NX-L1) needs the application
// directories of the calling file, which stored nodes do not carry.
// NewProjectRouteMatcher keeps them for calls from another repository.
func NewStoredRouteMatcher(nodes []contract.Node) RouteMatcher {
	return storedMatcher(nodes, false)
}

// NewProjectRouteMatcher is NewStoredRouteMatcher for the routes of another
// repository (the project link pass's provider endpoints, 2026-11-18
// TOP-1): Next.js routes join the candidates. The project rules stand in
// for the same-origin rule (NX-L1): a call from another repository is
// matched under the cross-repository assumption, and one with a literal
// origin reaches a provider only through a consumes entry that names that
// origin (rows PL-R5, PL-L4), so the literal-origin refusal does not apply.
// A link to a Next.js route carries NX-V1, and NX-V3 for a call an
// afterFiles or fallback rewrite re-routes (CallResult.Assumes). The caller
// passes every Next.js route of each application it includes, since
// NX-L3 asks which of the application's routes serve a path.
func NewProjectRouteMatcher(nodes []contract.Node) RouteMatcher {
	return storedMatcher(nodes, true)
}

func storedMatcher(nodes []contract.Node, next bool) RouteMatcher {
	if next {
		return RouteMatcher{routes: newMatcher(nodes, storedProof(nodes)), project: true}
	}
	kept := make([]contract.Node, 0, len(nodes))
	for _, n := range nodes {
		if framework, _ := n.Attributes["framework"].(string); n.Kind == "route" && nextFramework(framework) {
			continue
		}
		kept = append(kept, n)
	}
	return RouteMatcher{routes: newMatcher(kept, storedProof(kept))}
}

// storedProof turns equivalent_registrations into the matcher's proof map:
// each Express route of a group gets the group's identities as its key. A
// list that does not decode to strings, or does not name the route itself,
// proves nothing.
func storedProof(nodes []contract.Node) map[string]string {
	proof := map[string]string{}
	for _, n := range nodes {
		if n.Kind != "route" || n.Attributes["framework"] != "express" {
			continue
		}
		list, ok := n.Attributes["equivalent_registrations"].([]any)
		if !ok {
			if typed, isStrings := n.Attributes["equivalent_registrations"].([]string); isStrings {
				for _, id := range typed {
					list = append(list, id)
				}
			}
		}
		ids := make([]string, 0, len(list))
		self := false
		for _, v := range list {
			id, isString := v.(string)
			if !isString {
				ids = nil
				break
			}
			self = self || id == n.Identity
			ids = append(ids, id)
		}
		if len(ids) >= 2 && self {
			proof[n.Identity] = strings.Join(ids, "\x00")
		}
	}
	return proof
}

// CallRequest is one client_call path alternative as its stored attributes
// record it. Base is the call's base kind (none, literal, constant,
// configured or unresolved); Origin is the alternative's literal origin, empty
// when it has none; OriginUnresolved marks an assignment-derived origin
// (origin_resolution unresolved).
type CallRequest struct {
	Method, Template, Scope, Base, Origin string
	OriginUnresolved, TrailingSlash       bool
	// DeclaredOrigin marks, in the project link pass, a literal Origin that
	// a consumes entry of the call's consumer names for this matcher's
	// provider, another member (row UP-21): the unknown-prefix tier then
	// takes the call instead of refusing its origin (row UP-20). It changes
	// no other tier.
	DeclaredOrigin bool
}

// CallResult is the matcher's outcome for one alternative: the same status,
// match tier, bounded candidates and confidence the in-repository client
// linker records. Candidates name the target when Status is linked.
type CallResult struct {
	Status, Match  string
	Candidates     []string
	CandidateCount int
	Confidence     float64
	// Assumes lists the row assumptions a link rests on, as the
	// in-repository linker records them: a Next.js route's (NX-V1,
	// NX-V3), the unknown-prefix tier's (UP-V1, UP-V2) and the matched
	// route path entry's (GR-V5, RO-V1, RO-V2).
	Assumes []string
	// AssumedOrigin is the call's origin when the link assumes it to be the
	// target's server (nothing proves it), else "" (see OriginAssumption).
	AssumedOrigin string
	// UnknownPrefix marks an outcome (linked, ambiguous or test_only) that
	// rests on routes' unresolved_prefix_paths entries (T033, rows
	// UP-13..UP-20). The project link pass never links on one (row UP-25).
	UnknownPrefix bool
}

// Call runs the client selection rules (tiers, specificity, scope, slash
// twins) for one stored alternative, exactly as the in-repository linker
// runs them for a freshly evaluated one.
func (m RouteMatcher) Call(request CallRequest) CallResult {
	var origin any
	if request.Origin != "" {
		origin = request.Origin
	}
	attrs := map[string]any{
		"method": request.Method, "path_template": request.Template,
		"scope": request.Scope, "trailing_slash": request.TrailingSlash,
		"base": request.Base, "origin": origin, "status": "unmatched",
		"candidates": []string{}, "candidate_count": 0,
	}
	if request.OriginUnresolved {
		attrs["origin_resolution"] = "unresolved"
	}
	if request.DeclaredOrigin && request.Origin != "" {
		attrs["origin_declared"] = true
	}
	if m.project {
		attrs["project_link"] = true
	}
	edges := m.routes.link("", "", contract.Span{}, attrs)
	result := CallResult{Status: attrs["status"].(string), Candidates: attrs["candidates"].([]string), CandidateCount: attrs["candidate_count"].(int)}
	result.UnknownPrefix = attrs[unknownPrefixTier] == true
	if len(edges) > 0 {
		result.Match, _ = attrs["match"].(string)
		result.Candidates = []string{edges[0].Target}
		result.Confidence = edges[0].Confidence
		result.Assumes, _ = attrs["assumes"].([]string)
		if _, assumed := attrs["origin_assumption"]; assumed {
			result.AssumedOrigin, _ = assumedOrigin(m.routes, edges[0].Target, attrs)
		}
	}
	return result
}
