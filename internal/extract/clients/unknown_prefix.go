package clients

import (
	"slices"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
)

// The unknown-prefix tier (task T033, ADR 0084; rows UP-13..UP-20 of
// docs/contract/claims/unknown-prefix-paths.md, copied in
// docs/extract/clients.md). A route whose prefix the code does not fix has
// no "paths" entry, only unresolved_prefix_paths entries: the path proven
// below the unknown part. This tier is a fallback: it runs only for an
// alternative the existing tiers (exact, origin, suffix) leave without any
// candidate, so no existing edge or status changes. It never ranks its
// candidates: any second route that matches with a literal segment makes
// the alternative ambiguous, because each candidate implies a different
// unknown prefix and the literal count is no evidence between them.

// prefixPath is one stored unresolved_prefix_paths entry, as the matcher
// reads it.
type prefixPath struct {
	Template      string `json:"template"`
	Above         string `json:"above"`
	Optional      bool   `json:"optional"`
	TrailingSlash bool   `json:"trailing_slash"`
	Scope         string `json:"scope"`
	// Assumptions are the path assumptions the entry rests on (GR-V5,
	// RO-V1, RO-V2), carried by a link through it.
	Assumptions []string `json:"assumptions"`
}

// Assumption ids of the tier (rows UP-V1 and UP-V2).
const (
	// AssumeUnknownPrefix: the route is served below a prefix the code
	// does not fix, and the call is taken to reach it because its path
	// ends with the route's proven path below that prefix.
	AssumeUnknownPrefix = "UP-V1"
	// AssumeSamePrefix: the call's unknown base and the route's unknown
	// prefix are the same value.
	AssumeSamePrefix = "UP-V2"
	// AssumeDeclaredProvider: a consumes declaration of the project
	// manifest is taken as the proof that the call reaches the provider
	// (rows UP-21 and UP-26); added by the project link pass.
	AssumeDeclaredProvider = "UP-V3"
)

// UnknownPrefixAssumptions are the shown texts of the tier's assumptions,
// by id, as the public claim contract states them.
var UnknownPrefixAssumptions = map[string]string{
	AssumeUnknownPrefix:    "The route is served below a prefix the code does not fix (configuration or an unproven hop); the call is taken to reach it because its path ends with the route's proven path below that prefix.",
	AssumeSamePrefix:       "The call's unknown base and the route's unknown prefix are the same value (for example both read from the same configuration).",
	AssumeDeclaredProvider: "A consumes entry of the project manifest names the provider (and, for a literal origin, that origin): the call is taken to reach that provider's server, below a prefix its code does not fix.",
}

// unknownPrefixTier marks, on an alternative's attributes during linking,
// an outcome (linked, ambiguous or test_only) that rests on unknown-prefix
// entries. It is never stored: linkPaths removes it; RouteMatcher.Call
// reports it as CallResult.UnknownPrefix.
const unknownPrefixTier = "unknown_prefix_tier"

// upMatch is how one route relates to a call in this tier.
type upMatch struct {
	// literal: some entry matches with at least one literal route segment
	// (a competitor for row UP-17).
	literal bool
	// qualified: some entry matches under row UP-16 (two literal segments,
	// anchored first template segment).
	qualified bool
	// head: the match needed a client HEAD to a GET route (row UP-18).
	head bool
	// plain: some entry matching under row UP-16 rests on no path
	// assumption; assumes: the union of the path assumptions of the
	// entries matching under UP-16 (the link carries them
	// unless plain).
	plain   bool
	assumes []string
}

// upFound is what the unknown-prefix tier finds for one alternative: the
// routes whose entries match it (production ones, or test-scope ones only
// when isTest), whether one matches under row UP-16 and whether the call's
// base is unresolved (row UP-15).
type upFound struct {
	ids                        []string
	qualified, isTest, aligned bool
	// assumes are the path assumptions of the single route's qualifying
	// entries (upMatch.assumes; none when one of them rests on none).
	assumes []string
}

// linkUnknownPrefix runs the tier for one alternative that the existing
// tiers left without a candidate (row UP-13). It sets the alternative's
// status and returns the link, if any. Every other form takes the single
// default branch D: no candidate, the alternative stays unmatched.
func (m matcher) linkUnknownPrefix(identity, file string, span contract.Span, attrs map[string]any, call []string, method string) []contract.Edge {
	attrs["status"] = "unmatched"
	found, ok := m.upCandidates(attrs, call, method, upTag)
	if !ok {
		return nil
	}
	ids := found.ids
	if found.aligned {
		upTag("UP-15")
	} else {
		upTag("UP-14")
	}
	attrs[unknownPrefixTier] = true
	if found.isTest {
		upTag("UP-18")
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
		upTag("UP-17")
		attrs["status"] = "ambiguous"
		attrs["candidate_count"] = len(ids)
		if len(ids) > 10 {
			ids = ids[:10]
		}
		attrs["candidates"] = ids
		return nil
	}
	upTag("UP-19")
	attrs["status"] = "linked"
	attrs["match"] = "suffix"
	attrs["route_prefix"] = "unresolved"
	attrs["assumes"] = []string{AssumeUnknownPrefix}
	if found.aligned {
		attrs["assumes"] = []string{AssumeUnknownPrefix, AssumeSamePrefix}
	}
	// Step UP-G: the path assumptions of the entries the
	// link rests on follow the tier's own.
	attrs["assumes"] = append(attrs["assumes"].([]string), found.assumes...)
	return []contract.Edge{{Type: "edge", Kind: "calls_route", Source: identity, Target: ids[0], Provenance: "inferred", Confidence: 0.6, Evidence: []contract.Evidence{{File: file, Span: span}}}}
}

// catchAllRival is row UP-22: an alternative whose
// only candidates in the existing tiers are final catch-alls (a SPA or
// root fallback) while an unknown-prefix entry of a production route
// matches it with at least one literal segment (UP-17's competitor) is
// ambiguous, as row LF-2 makes a
// catch-all ambiguous beside a more specific route: the call may be meant
// for the route below the unknown prefix, which a router tries first. It
// returns the unknown-prefix routes to add to the candidates, nil when the
// row does not apply. It reads attrs and never changes them.
func (m matcher) catchAllRival(attrs map[string]any, call []string, method string) []string {
	// A rival needs one literal segment matched (UP-17's competitor), not
	// UP-16's two: the outcome is ambiguous, never a link to it.
	found, _ := m.upCandidates(attrs, call, method, func(string) {})
	if found.isTest || len(found.ids) == 0 {
		return nil
	}
	upTag("UP-22")
	return found.ids
}

// upCandidates matches one alternative against every route's
// unresolved_prefix_paths entries (rows UP-14..UP-18, UP-20, UP-21),
// reporting each branch's row to tag. ok is false when the alternative
// takes no candidate here (a refused form or none qualifying under row
// UP-16).
func (m matcher) upCandidates(attrs map[string]any, call []string, method string, tag func(string)) (upFound, bool) {
	if attrs["exact_only"] == true {
		// OpenAPI operation correspondence (exact only): no call
		// alternative.
		tag("D")
		return upFound{}, false
	}
	tag("UP-13")
	if attrs["nextjs_rewrite"] != nil {
		// A Next.js rewrite may send the call elsewhere (row NX-L3): its
		// withholding is no absence of candidates.
		return upFound{}, false
	}
	if attrs["origin_resolution"] == "unresolved" {
		// An assignment-derived origin is never proven.
		tag("UP-20")
		return upFound{}, false
	}
	if attrs["origin"] != nil {
		if attrs["origin_declared"] != true {
			tag("UP-20")
			return upFound{}, false
		}
		// The project link pass found a consumes entry of the call's
		// consumer that names this literal origin and another member as
		// its provider (row UP-21): the call's path is known from its
		// start, so it is matched as row UP-14 reads such a call.
		tag("UP-21")
	}
	aligned := false
	switch attrs["base"] {
	case "none", "literal", "constant", "configured":
	case "unresolved":
		aligned = true
	default:
		tag("D")
		return upFound{}, false
	}
	slash := attrs["trailing_slash"] == true
	test := attrs["scope"] == "test"
	// Matches by route identity, production entries and test-scope entries
	// apart (row UP-18: a production call prefers production entries).
	production, testOnly := map[string]upMatch{}, map[string]upMatch{}
	slashRefused, aboveRefused := false, false
	for _, r := range m {
		if len(r.prefixPaths) == 0 || nextFramework(r.framework) || r.host != "" {
			// A Next.js route is never a candidate here (UP-20); a
			// host-pattern route answers only a call naming its host, and
			// this tier takes no call with an origin.
			continue
		}
		head := false
		if r.method != method && r.method != "ANY" {
			if !(method == "HEAD" && r.method == "GET" && headServesGet[r.framework]) {
				continue
			}
			head = true
		}
		for _, p := range r.prefixPaths {
			if p.Template == "" {
				continue
			}
			if otherSlashForm(r.framework, routePath{Template: p.Template, TrailingSlash: p.TrailingSlash}, slash) {
				slashRefused = true
				continue
			}
			above := segments(p.Above)
			if aligned && len(above) > 0 {
				// The call's unknown base cannot stand for an unknown part
				// that lies after a known one.
				aboveRefused = true
				continue
			}
			template := segments(p.Template)
			variants := [][]string{template}
			if p.Optional && len(template) > 0 && param(template[len(template)-1]) {
				variants = append(variants, template[:len(template)-1])
			}
			for _, v := range variants {
				literal, qualified := matchPrefixed(call, above, v, aligned)
				if !literal {
					continue
				}
				set := production
				if p.Scope == "test" && !test {
					set = testOnly
				}
				got := set[r.identity]
				got.literal = true
				got.qualified = got.qualified || qualified
				got.head = got.head || head
				if qualified {
					got.plain = got.plain || len(p.Assumptions) == 0
					for _, id := range p.Assumptions {
						if !slices.Contains(got.assumes, id) {
							got.assumes = append(got.assumes, id)
						}
					}
				}
				set[r.identity] = got
			}
		}
	}
	if slashRefused {
		tag("UP-18")
	}
	if aboveRefused {
		tag("UP-15")
	}
	chosen, isTest := production, false
	if len(production) == 0 {
		chosen, isTest = testOnly, true
	}
	if len(chosen) == 0 {
		tag("D")
		return upFound{}, false
	}
	qualified := false
	ids := make([]string, 0, len(chosen))
	for id, got := range chosen {
		ids = append(ids, id)
		qualified = qualified || got.qualified
		if got.head {
			tag("UP-18")
		}
	}
	sort.Strings(ids)
	if !qualified {
		// No route matches with two literal segments and an anchored
		// template: no candidate (but the routes stay rivals of a
		// catch-all, row UP-22).
		tag("UP-16")
		return upFound{ids: ids, isTest: isTest, aligned: aligned}, false
	}
	found := upFound{ids: ids, qualified: true, isTest: isTest, aligned: aligned}
	if got := chosen[ids[0]]; len(ids) == 1 && !got.plain {
		found.assumes = got.assumes
	}
	return found, true
}

// matchPrefixed matches a call against one entry: literal reports a match
// with at least one literal route segment (of above and template) matched
// by a literal call segment, qualified a match with at least two and an
// anchored template (row UP-16).
//
// Known from its start (row UP-14), the call is above + V + template: above
// matches the call's first segments (a route parameter takes any call
// segment), V is a run of zero or more literal call segments (the unknown
// prefix; a call parameter inside it ends the run), and the rest matches
// template by the existing segment rules. With an unresolved base (row
// UP-15), the call must match template entirely (the two unknown parts are
// taken to be the same value; the caller refuses an entry with above).
// Anchored: template's first segment is a literal, or a parameter matched
// by a call parameter, so no call literal becomes the value of a route
// parameter right after the unknown part.
func matchPrefixed(call, above, template []string, aligned bool) (literal, qualified bool) {
	if len(template) == 0 {
		return false, false
	}
	count := 0
	for i, s := range above {
		if i >= len(call) {
			return false, false
		}
		if param(s) {
			continue
		}
		if param(call[i]) || s != call[i] {
			return false, false
		}
		count++
	}
	rest := call[len(above):]
	try := func(tail []string) {
		score, match := pathScore(tail, template)
		if !match || count+score == 0 {
			return
		}
		literal = true
		anchored := !param(template[0]) || param(tail[0])
		qualified = qualified || anchored && count+score >= 2
	}
	if aligned {
		try(rest)
		return literal, qualified
	}
	for v := 0; v < len(rest); v++ {
		try(rest[v:])
		if param(rest[v]) {
			// A call parameter inside the unknown run: the run ends here.
			break
		}
	}
	return literal, qualified
}

var upTagHook func(row string)

// SetUnknownPrefixTagHook installs (or, with nil, removes) the hook every
// branch of the unknown-prefix tier reports its row to (rows UP-13..UP-20
// and D). Tests only; linking is single-threaded.
func SetUnknownPrefixTagHook(hook func(row string)) {
	upTagHook = hook
}

func upTag(row string) {
	if upTagHook != nil {
		upTagHook(row)
	}
}
