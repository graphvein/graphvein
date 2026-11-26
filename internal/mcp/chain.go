package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/traverse"
)

// chainArgs are the chain tool's arguments.
type chainArgs struct {
	Target     string `json:"target" jsonschema:"node identity, 'METHOD /path' as served, a component directory (for example web; component:web when a node has the same name), a name or a qualified-name suffix at a name boundary"`
	Direction  string `json:"direction,omitempty" jsonschema:"up (callers, client calls, UI), down (callees, routes, handlers, tables) or both (default)"`
	Depth      int    `json:"depth,omitempty" jsonschema:"steps each way (default 6, at most 10)"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default) or all"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"members per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

// chainMemberKeys are the keys of a chain member the tool passes on; any
// other key is MC-D.
var chainMemberKeys = map[string]bool{"id": true, "kind": true, "name": true, "qualified_name": true, "file": true, "line": true, "component": true,
	"method": true, "path": true, "full_path": true, "prefix_path": true, "command": true, "status": true, "database": true, "repository": true, "row": true,
	"tier": true, "tier_step": true, "parents": true}

// chainTool answers the architecture chain (row MC-20): the members of
// /api/v0/architecture/chain per direction and step, each with the edges
// that reach it (wording, row, tier, and a table edge's access), paged;
// the chain's own caps, ends and not-followed counts are bounds and
// unknowns (MC-21).
func chainTool(ctx context.Context, s *Server, in chainArgs, e *Envelope) error {
	tagRow("MC-20")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	direction := orDefault(in.Direction, "both")
	if direction != "up" && direction != "down" && direction != "both" {
		return badRequest("direction must be up, down or both, not %q", in.Direction)
	}
	depth := in.Depth
	if depth == 0 {
		depth = traverse.ChainDefaultDepth
	}
	if depth < 1 || depth > traverse.MaxDepth {
		return badRequest("depth must be 1 to %d, not %d", traverse.MaxDepth, in.Depth)
	}
	scope := orDefault(in.Scope, "production")
	tagRow("MC-2")
	e.Request["target"], e.Request["direction"], e.Request["depth"], e.Request["scope"], e.Request["limit"], e.Request["cursor"] = in.Target, direction, depth, scope, n, in.Cursor
	_, repo, err := s.member(in.Repository, false)
	if err != nil {
		return err
	}
	q := url.Values{"direction": {direction}, "depth": {strconv.Itoa(depth)}, "scope": {scope}}
	if s.opt.Project != nil {
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	r, err := s.resolve(ctx, e, repo, in.Target, targetLookup{what: "stored node", routes: true, components: true})
	if err != nil {
		return err
	}
	q.Set("id", r.id)
	if kind, _, _, err := contract.ParseIdentity(r.id); err == nil {
		specOperationNote(e, kind)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/architecture/chain", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	caps, _ := v["caps"].(map[string]any)
	summary := map[string]any{}
	var members []any
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch k {
		case "api_version", "repository", "index", "direction", "scope", "depth":
			// Echoed by the envelope (request, freshness).
		case "start", "caps":
			summary[k] = val
		case "map":
			e.omit("map")
		case "up", "down":
			side, ok := val.(map[string]any)
			if !ok {
				continue
			}
			list, sideSummary := chainSide(e, k, side, caps, depth)
			members = append(members, list...)
			summary[k] = sideSummary
		default:
			e.notModelled("chain."+k, val)
		}
	}
	e.Summary = summary
	if members == nil {
		members = []any{}
	}
	e.Results = page(e, members, offset, n)
	if len(members) == 0 {
		e.unknown("MC-21", "no_member", "no member beyond the start within the depth: not proof that nothing calls or is called by it (see bounds and ends)", nil)
	}
	return nil
}

// isComponent reports whether the architecture view lists a component
// with this directory. The view is read only when the target is neither
// an identity nor a path.
func (s *Server) isComponent(ctx context.Context, repo, dir string) bool {
	q := url.Values{"scope": {"all"}}
	if s.opt.Project != nil {
		q.Set("repository", repo)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/architecture", q)
	if err != nil {
		return false
	}
	v, err := decode(raw)
	if err != nil {
		return false
	}
	for _, c := range listOf(v["components"]) {
		if m, _ := c.(map[string]any); str(m["id"]) == dir {
			return true
		}
	}
	return false
}

// chainSide maps one direction of the chain: every member past the start
// with the edges that reach it, the step bounds (MC-3), the ends and the
// not-followed counts (MC-21), test members kept out and project steps
// (MC-18).
func chainSide(e *Envelope, dir string, side map[string]any, caps map[string]any, depth int) ([]any, map[string]any) {
	into := map[string][]any{}
	for _, raw := range listOf(side["edges"]) {
		ed, _ := raw.(map[string]any)
		into[str(ed["to"])] = append(into[str(ed["to"])], chainEdgeOf(e, ed))
	}
	var out []any
	for _, rawStep := range listOf(side["steps"]) {
		st, _ := rawStep.(map[string]any)
		step, _ := strconv.Atoi(str(st["step"]))
		for _, k := range sortedKeys(st) {
			switch k {
			case "step", "members", "map":
			case "hidden_test":
				if c := str(st[k]); c != "0" && c != "" {
					e.unknown("MC-21", "hidden_test", fmt.Sprintf("%s step %d: %s test-scope members kept out (scope all shows them)", dir, step, c), nil)
				}
			case "bounds":
				for _, b := range listOf(st[k]) {
					e.Bounds = append(e.Bounds, chainBound(e, dir, b, caps, depth))
				}
			default:
				e.notModelled("chain.step."+k, st[k])
			}
		}
		if step == 0 {
			continue
		}
		for _, rawMember := range listOf(st["members"]) {
			m, _ := rawMember.(map[string]any)
			r := map[string]any{"direction": dir, "step": step}
			for _, k := range sortedKeys(m) {
				if !chainMemberKeys[k] {
					e.notModelled("chain.member."+k, m[k])
					continue
				}
				if m[k] != nil {
					r[k] = m[k]
				}
			}
			e.label("tier", tierFamily(str(m["tier"])))
			if str(m["repository"]) != "" {
				tagRow("MC-18")
			}
			via := into[str(m["id"])]
			if via == nil {
				via = []any{}
			}
			r["via"] = via
			out = append(out, r)
		}
	}
	summary := map[string]any{}
	for _, k := range sortedKeys(side) {
		switch k {
		case "steps", "edges":
		case "members", "hidden_test", "caps":
			summary[k] = side[k]
		case "ends":
			// One unknown per distinct end line, with the members it
			// applies to (the line is long and repeats per member).
			var texts []string
			members := map[string][]any{}
			rows := map[string]string{}
			for _, raw := range listOf(side[k]) {
				end, _ := raw.(map[string]any)
				text := str(end["text"])
				if members[text] == nil {
					texts = append(texts, text)
				}
				members[text] = append(members[text], end["member"])
				rows[text] = str(end["row"])
			}
			for _, text := range texts {
				data := map[string]any{"chain_row": rows[text], "members": members[text]}
				if rows[text] == "CH13" {
					e.unknown("MC-18", "continues_in_member", dir+": "+text, data)
					continue
				}
				e.unknown("MC-21", "not_modelled_end", dir+": "+text, data)
			}
		case "not_followed":
			if nf, _ := side[k].(map[string]any); len(nf) > 0 {
				e.unknown("MC-21", "not_followed", dir+": stored edges of these kinds lead on from members but no chain row follows them (counts by kind)", nf)
			}
		default:
			e.notModelled("chain.side."+k, side[k])
		}
	}
	return out, summary
}

// chainEdgeOf maps one chain edge into the member it reaches: wording,
// row, tier, the first evidence location and, on a table edge, the
// owner's table access with its verification and assumptions (D7, G9).
func chainEdgeOf(e *Envelope, ed map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range sortedKeys(ed) {
		val := ed[k]
		switch k {
		case "to":
		case "from", "kind", "row", "wording", "dashed", "tier", "count":
			out[k] = val
		case "evidence":
			if ev := listOf(val); len(ev) > 0 {
				first, _ := ev[0].(map[string]any)
				out["file"] = first["file"]
				if sp, ok := first["span"].(map[string]any); ok {
					out["line"] = sp["start_line"]
				}
				if len(ev) > 1 {
					e.omit("via.evidence")
				}
			}
		case "access":
			out[k] = accessOf(e, val)
		case "assumptions":
			// A CH3 link's assumptions (an unproven origin, UP-V1,
			// UP-V2): never a plain link.
			for ck, cv := range e.linkAssumed(val, "MC-20") {
				out[ck] = cv
			}
		default:
			e.notModelled("chain.edge."+k, val)
		}
	}
	return out
}

// accessOf passes a table_access entry on and labels its verification
// (MC-23): "mapped" is mapped, not verified; anything else the legend
// lacks is MC-D.
func accessOf(e *Envelope, val any) any {
	a, ok := val.(map[string]any)
	if !ok {
		return val
	}
	e.label("verification", orDefault(str(a["verification"]), "unknown"))
	for _, id := range listOf(a["assumes"]) {
		e.assume(str(id), "MC-23")
	}
	return a
}

// chainBound maps a chain step bound (CH12) with its cap's limit; depth
// is the requested depth.
func chainBound(e *Envelope, dir string, b any, caps map[string]any, depth int) Bound {
	tagRow("MC-3")
	m, _ := b.(map[string]any)
	step, _ := strconv.Atoi(str(m["step"]))
	beyond, _ := strconv.Atoi(str(m["beyond"]))
	capName := str(m["cap"])
	// The chain names its member cap "members"; traverse calls it
	// reached_nodes.
	limitKey := map[string]string{traverse.CapDepth: "depth", traverse.CapReached: "members", "members": "members", traverse.CapFanOut: "fan_out"}[capName]
	limit, _ := strconv.Atoi(str(caps[limitKey]))
	if capName == traverse.CapDepth {
		// The depth cap binds at the walked depth; caps.depth is the
		// largest depth a request may name (T042: 10, while the default
		// stays 6).
		limit = depth
	}
	e.label("bound", capName)
	text := dir + ": " + str(m["text"])
	if capName == traverse.CapDepth {
		if depth < traverse.MaxDepth {
			text += fmt.Sprintf("; call again with a larger depth (at most %d) or start from a member at the last step", traverse.MaxDepth)
		} else {
			// R12: at the maximum a larger depth is no way on.
			text += "; start from a member at the last step to see further"
		}
	}
	return Bound{Cap: capName, Limit: limit, Step: &step, Beyond: &beyond, Text: text, Row: "MC-3"}
}

// tierFamily is a chain tier's legend key: "fact", or "inferred" for any
// "inferred 0.9" (the number is the weakest edge confidence).
func tierFamily(t string) string {
	if strings.HasPrefix(t, "inferred ") {
		return "inferred"
	}
	return t
}
