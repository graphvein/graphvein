package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/traverse"
)

// changesArgs are the changes tool's arguments.
type changesArgs struct {
	Base       string `json:"base,omitempty" jsonschema:"previous (default), run:ID, pin:NAME or ref:BASE[..HEAD] with stored snapshots (never indexed here)"`
	Group      string `json:"group,omitempty" jsonschema:"one group of the summary (routes, client_calls, code, migration_tables, ...)"`
	Change     string `json:"change,omitempty" jsonschema:"one change (added, removed, changed, body_changed, link_lost, unknown_now, ...)"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default: test items hidden and counted) or all"`
	Detail     string `json:"detail,omitempty" jsonschema:"compact (default: changed field names) or full (each field's before and after)"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"items per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

// changeImpactArgs are the change_impact tool's arguments.
type changeImpactArgs struct {
	ID         string `json:"id,omitempty" jsonschema:"a changed item's identity, as the changes tool lists it (or give group)"`
	Group      string `json:"group,omitempty" jsonschema:"every seed of one summary group (or give id)"`
	Base       string `json:"base,omitempty" jsonschema:"previous (default), run:ID, pin:NAME or a stored ref: base; project mode refuses ref: bases"`
	Depth      int    `json:"depth,omitempty" jsonschema:"steps to walk (default 3, at most 10)"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member whose change set is read (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"paths per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
	unknownsArgs
}

// baseOf checks a base argument (D9, row MC-30): the bases the change
// views read without writing. Every form changes.ParseBase accepts is
// read only here; a ref: base whose snapshots are not stored is refused
// by the view, and changeRefusal says how to store it.
func baseOf(raw string) (string, error) {
	b, err := changes.ParseBase(raw)
	if err != nil {
		return "", badRequest("%v", err)
	}
	return b.String(), nil
}

// changeRefusal turns a change view's error into the refusal of MC-30,
// with the stored bases as candidates when the base is the problem.
func (s *Server) changeRefusal(ctx context.Context, err error, base string, q url.Values) error {
	var re *server.ReadError
	if !errors.As(err, &re) {
		return err
	}
	te := &toolError{code: re.Code, message: re.Message}
	switch re.Code {
	case "ref_unknown":
		te.code = "would_index"
		te.message = "base " + base + " needs a ref snapshot that is not stored: " + re.Message + ". The MCP server is read-only and never indexes; run graphvein changes --base " + base + " (or the UI's Changes view) once to store it, then ask again"
	case "base_unknown":
		te.message = re.Message + ". The stored bases are listed as candidates"
	case "not_found":
		if q.Get("id") == "" {
			return err
		}
		te.message = re.Message + ": " + q.Get("id") + " is not a changed item between " + base + " and the current index (the changes tool lists the items)"
		return te
	case "base_expired":
		te.message = re.Message + ": the journal no longer holds that run. The stored bases are listed as candidates"
	default:
		return err
	}
	tagRow("MC-30")
	te.candidates = s.storedBases(ctx, q)
	return te
}

// storedBases lists the bases the change views can read now: the
// previous run (when there is one), each journal run, pin and stored ref
// snapshot, from /api/v0/changes/bases.
func (s *Server) storedBases(ctx context.Context, q url.Values) []any {
	bq := url.Values{}
	if r := q.Get("repository"); r != "" {
		bq.Set("repository", r)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/changes/bases", bq)
	if err != nil {
		return nil
	}
	v, err := decode(raw)
	if err != nil {
		return nil
	}
	out := []any{}
	runs := listOf(v["runs"])
	if len(runs) > 1 {
		out = append(out, "previous")
	}
	for _, r := range runs {
		m, _ := r.(map[string]any)
		out = append(out, "run:"+str(m["run_id"]))
	}
	for _, p := range listOf(v["pins"]) {
		m, _ := p.(map[string]any)
		out = append(out, "pin:"+str(m["name"]))
	}
	for _, r := range listOf(v["refs"]) {
		m, _ := r.(map[string]any)
		out = append(out, map[string]any{"ref_snapshot": m})
	}
	return out
}

// changesTool answers what changed between a stored base and the current
// index (row MC-24): the summary of /api/v0/changes/summary and one page
// of /api/v0/changes/items, with each item's row, change, cause, reason
// and labels.
func changesTool(ctx context.Context, s *Server, in changesArgs, e *Envelope) error {
	tagRow("MC-24")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	_, n, err := pageParams(in.Limit, "")
	if err != nil {
		return err
	}
	base, err := baseOf(in.Base)
	if err != nil {
		return err
	}
	detail, err := detailParam(in.Detail)
	if err != nil {
		return err
	}
	scope := orDefault(in.Scope, "production")
	if scope != "production" && scope != "all" {
		return badRequest("scope must be production or all, not %q", in.Scope)
	}
	tagRow("MC-2")
	e.Request["base"], e.Request["group"], e.Request["change"], e.Request["scope"], e.Request["detail"], e.Request["limit"], e.Request["cursor"] = base, in.Group, in.Change, scope, detail, n, in.Cursor
	q := url.Values{"base": {base}}
	if err := s.memberParam(e, q, in.Repository); err != nil {
		return err
	}
	raw, err := s.reader.Read(ctx, "/api/v0/changes/summary", q)
	if err != nil {
		return s.changeRefusal(ctx, err, base, q)
	}
	sv, err := decode(raw)
	if err != nil {
		return err
	}
	sum, _ := sv["summary"].(map[string]any)
	e.Summary = changeSummary(e, sum)
	iq := url.Values{"base": {base}, "limit": {strconv.Itoa(n)}}
	for k, v := range map[string]string{"group": in.Group, "change": in.Change, "cursor": in.Cursor, "repository": q.Get("repository")} {
		if v != "" {
			iq.Set(k, v)
		}
	}
	if scope == "all" {
		iq.Set("tests", "true")
	}
	raw, err = s.reader.Read(ctx, "/api/v0/changes/items", iq)
	if err != nil {
		return s.changeRefusal(ctx, err, base, q)
	}
	iv, err := decode(raw)
	if err != nil {
		return err
	}
	var items []any
	for _, it := range listOf(iv["items"]) {
		items = append(items, changeItem(e, it, detail))
	}
	if items == nil {
		items = []any{}
	}
	e.Results = items
	total, _ := asInt(iv["total"])
	viewPage(e, len(items), total, str(iv["next_cursor"]))
	if h, _ := asInt(iv["hidden_tests"]); h > 0 {
		e.unknown("MC-24", "hidden_tests", fmt.Sprintf("%d test-scope items hidden (scope all lists them)", h), nil)
	}
	for _, k := range sortedKeys(iv) {
		switch k {
		case "api_version", "repository", "index", "freshness", "base", "total", "hidden_tests", "next_cursor", "items":
		default:
			e.notModelled("changes.items."+k, iv[k])
		}
	}
	if len(items) == 0 {
		e.unknown("MC-24", "no_item", "no changed item in this filter between the base and the current index (facts the extractors do not read cannot change here)", nil)
	}
	return nil
}

// viewPage records a page the change view cut itself (MC-4): its total
// and opaque cursor are the view's.
func viewPage(e *Envelope, returned, total int, next string) {
	tagRow("MC-4")
	p := &Page{Returned: returned, Total: total, NextCursor: next}
	if next != "" {
		p.More = total - returned
		p.Text = fmt.Sprintf("%d more not shown; call again with cursor set to next_cursor", p.More)
	}
	e.Page = p
}

// changeSummary compacts the comparison summary: both sides (run, time,
// extractor, partial and gaps), the group counts, and the flags; a
// partial head, gaps, an extractor change and a head commit that differs
// are unknowns.
func changeSummary(e *Envelope, sum map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range sortedKeys(sum) {
		val := sum[k]
		switch k {
		case "contract_version", "groups", "total", "locations", "file_metadata_only", "partial":
			out[k] = val
		case "base", "head":
			out[k] = val
			side, _ := val.(map[string]any)
			if gaps := listOf(side["gaps"]); len(gaps) > 0 {
				e.unknown("MC-24", k+"_gaps", fmt.Sprintf("the %s index has files with knowledge gaps: their facts may be missing on that side, so a change there may be a gap, not an edit", k), gaps)
			}
			if t, _ := side["gaps_truncated"].(bool); t {
				e.unknown("MC-24", k+"_gaps_truncated", "the "+k+" side's gap list is cut upstream", nil)
			}
		case "extractor_changed":
			out[k] = val
			if b, _ := val.(bool); b {
				e.unknown("MC-24", "extractor_changed", "the two sides were indexed by different extractor versions: some items may be extractor changes, not code changes", nil)
			}
		case "head_commit_differs":
			out[k] = val
			if b, _ := val.(bool); b {
				e.unknown("MC-24", "head_commit_differs", "the current index was not made at the head commit of the ref base", nil)
			}
		case "not_extracted":
			out[k] = val
			if len(listOf(val)) > 0 {
				e.unknown("MC-24", "not_extracted", "facts of these kinds are not compared (no change can be reported for them)", val)
			}
		case "extractor_recheck":
			out[k] = val
			if val != nil {
				e.unknown("MC-24", "extractor_recheck", "the comparison needs an extractor recheck", val)
			}
		default:
			e.notModelled("changes.summary."+k, val)
		}
	}
	return out
}

// changeItem maps one change item: identity and location, the row,
// change, cause, reason, wording, labels and precision as served; in
// compact detail the changed fields by name and change, in full detail
// with their before and after values.
func changeItem(e *Envelope, raw any, detail string) map[string]any {
	m, _ := raw.(map[string]any)
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		val := m[k]
		switch k {
		case "identity":
			if str(val) != str(m["id"]) {
				out[k] = val
			}
		case "id", "group", "change", "row", "record", "kind", "name", "labels", "reason", "wording", "cause", "test", "hints", "impact_count":
			out[k] = val
		case "provenance":
			out[k] = val
			e.label("provenance", str(val))
		case "confidence":
			if val != nil {
				out[k] = val
			}
		case "precision":
			out[k] = val
			if p, ok := val.(map[string]any); ok {
				labelPrecision(e, p["scope"])
			}
		case "before", "after":
			out[k] = changeSide(e, val, detail)
		case "fields":
			var fields []any
			for _, f := range listOf(val) {
				fm, _ := f.(map[string]any)
				if detail == detailFull {
					fields = append(fields, fm)
					continue
				}
				fields = append(fields, map[string]any{"name": fm["name"], "change": fm["change"]})
				e.omitHow("items.fields before and after values", "call changes again with detail full")
			}
			if fields == nil {
				fields = []any{}
			}
			out[k] = fields
		default:
			e.notModelled("changes.item."+k, val)
		}
	}
	e.label("change", str(m["change"]))
	return out
}

// changeSide is an item's before or after state: file and line, and in
// full detail its stored fields.
func changeSide(e *Envelope, val any, detail string) any {
	m, ok := val.(map[string]any)
	if !ok {
		return val
	}
	out := map[string]any{"file": m["file"]}
	if sp, ok := m["span"].(map[string]any); ok {
		out["line"] = sp["start_line"]
	}
	if _, ok := m["ide"]; ok {
		e.omit("items.ide")
	}
	if f, ok := m["fields"]; ok {
		if detail == detailFull {
			out["fields"] = f
		} else {
			e.omitHow("items.before/after fields (each side's stored fields)", "call changes again with detail full")
		}
	}
	return out
}

// changeImpactTool answers what a changed item reaches (row MC-25):
// /api/v0/changes/impact, or /api/v0/project/changes/impact in project
// mode, with the paths paged by the view, their claims ("depends on",
// "may depend on", never "breaks"), the unknowns, table uses, caps and,
// across repositories, link assumptions and possible dependents.
func changeImpactTool(ctx context.Context, s *Server, in changeImpactArgs, e *Envelope) error {
	tagRow("MC-25")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	_, n, err := pageParams(in.Limit, "")
	if err != nil {
		return err
	}
	base, err := baseOf(in.Base)
	if err != nil {
		return err
	}
	if (in.ID == "") == (in.Group == "") {
		return badRequest("give exactly one of id and group")
	}
	depth := in.Depth
	if depth == 0 {
		depth = changes.DefaultDepth
	}
	if depth < 1 || depth > changes.MaxDepth {
		return badRequest("depth must be 1 to %d, not %d", changes.MaxDepth, in.Depth)
	}
	tagRow("MC-2")
	e.Request["id"], e.Request["group"], e.Request["base"], e.Request["depth"], e.Request["limit"], e.Request["cursor"] = in.ID, in.Group, base, depth, n, in.Cursor
	if err := e.unknownsPaging(in.unknownsArgs); err != nil {
		return err
	}
	q := url.Values{"base": {base}, "depth": {strconv.Itoa(depth)}, "limit": {strconv.Itoa(n)}}
	if in.ID != "" {
		q.Set("id", in.ID)
	} else {
		q.Set("group", in.Group)
	}
	if in.Cursor != "" {
		q.Set("cursor", in.Cursor)
	}
	if err := s.memberParam(e, q, in.Repository); err != nil {
		return err
	}
	path := "/api/v0/changes/impact"
	if s.opt.Project != nil {
		path = "/api/v0/project/changes/impact"
	}
	raw, err := s.reader.Read(ctx, path, q)
	if err != nil {
		return s.changeRefusal(ctx, err, base, q)
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	var paths []any
	summary := map[string]any{}
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch k {
		case "api_version", "repository", "index", "freshness", "base", "depth", "total", "next_cursor":
		case "seeds", "project":
			summary[k] = val
		case "paths":
			for _, p := range listOf(val) {
				paths = append(paths, impactPath(e, p))
			}
		case "unknowns":
			impactUnknowns(e, "", val)
		case "table_uses":
			tableUses(e, "", val)
		case "members":
			for _, mi := range listOf(val) {
				m, _ := mi.(map[string]any)
				repo := str(m["repository"])
				impactUnknowns(e, repo, m["unknowns"])
				tableUses(e, repo, m["table_uses"])
				if missing := listOf(m["link_sources_missing"]); len(missing) > 0 {
					e.unknown("MC-18", "link_sources_missing", "member "+repo+": link sources no longer stored; the paths through them are not followed", missing)
				}
			}
		case "possible_dependents":
			for _, pd := range listOf(val) {
				e.unknown("MC-18", "possible_dependent", "may depend on a reached route without a project link (ambiguous or incomplete link outcome): never a path", pd)
			}
		case "caps":
			for _, c := range listOf(val) {
				e.Bounds = append(e.Bounds, impactCap(e, str(c), depth))
			}
		default:
			e.notModelled("change_impact."+k, val)
		}
	}
	e.Summary = summary
	if paths == nil {
		paths = []any{}
	}
	e.Results = paths
	total, _ := asInt(v["total"])
	viewPage(e, len(paths), total, str(v["next_cursor"]))
	if total == 0 {
		e.unknown("MC-25", "no_dependent", "no stored dependent within the depth: not proof that nothing depends on it (see bounds and unknowns)", nil)
	}
	return nil
}

// impactCap is one change impact cap as a bound. The traversal names the
// cap without a count, so beyond is null and the text says "may".
func impactCap(e *Envelope, c string, depth int) Bound {
	tagRow("MC-3")
	e.label("bound", c)
	limit := map[string]int{traverse.CapDepth: depth, traverse.CapReached: traverse.MaxReached, traverse.CapFanOut: traverse.MaxFanOut}[c]
	text := fmt.Sprintf("%s cap reached (%d): dependents may lie beyond, uncounted", c, limit)
	if c == "depth" {
		if depth < changes.MaxDepth {
			text += fmt.Sprintf("; call again with depth up to %d", changes.MaxDepth)
		} else {
			// At the maximum a larger depth is no way on (same branch as
			// the chain and the callers/impact tools).
			text += "; call again from a dependent at the last step to see further"
		}
	}
	return Bound{Cap: c, Limit: limit, Text: text, Row: "MC-3"}
}

// impactPath maps one impact path: its claim and wording as served, the
// provenance and confidence (weakest inferred step), and each step with
// its first evidence location, an I8 step's access and a project link's
// assumption and stale reasons.
func impactPath(e *Envelope, raw any) map[string]any {
	m, _ := raw.(map[string]any)
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		val := m[k]
		switch k {
		case "node", "kind", "seed", "distance", "relation", "wording", "route_paths", "repository", "stale":
			out[k] = val
		case "route_shadowed":
			// A reached route whose every composed path an earlier mount
			// takes first: mounted, its paths withheld (empty
			// route_paths does not mean unmounted).
			out[k] = val
			e.label("flag", "shadowed")
		case "route_withheld":
			// A reached route the Go linker found but whose path it
			// withholds (couldn't-read row CR-44): empty route_paths does
			// not mean unmounted.
			out[k] = val
			e.label("flag", "path_withheld")
		case "prefix_unknown_paths":
			// A reached route's paths whose prefix the code does not
			// fix (T033 row UP-36): never full paths.
			out[k] = val
			e.label("path", "prefix_unknown")
		case "claim":
			out[k] = val
			e.label("claim", str(val))
		case "provenance":
			out[k] = val
			e.label("provenance", str(val))
		case "confidence":
			out[k] = val
			if val == nil {
				e.unknown("MC-25", "confidence_rule_only", "path "+str(m["node"])+": inferred by a rule with no stored confidence", nil)
			} else if str(m["provenance"]) != "fact" {
				out["confidence_rule"] = "weakest_edge"
				e.label("confidence", "weakest_edge")
			}
		case "assumptions":
			out[k] = val
			for _, a := range listOf(val) {
				e.Assumptions = appendAssumption(e.Assumptions, str(a))
			}
		case "steps":
			var steps []any
			for _, st := range listOf(val) {
				steps = append(steps, impactStep(e, st))
			}
			if steps == nil {
				steps = []any{}
			}
			out[k] = steps
		default:
			e.notModelled("change_impact.path."+k, val)
		}
	}
	if b, _ := m["stale"].(bool); b {
		tagRow("MC-18")
	}
	return out
}

func impactStep(e *Envelope, raw any) map[string]any {
	m, _ := raw.(map[string]any)
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		val := m[k]
		switch k {
		case "evidence":
			if ev := listOf(val); len(ev) > 0 {
				first, _ := ev[0].(map[string]any)
				out["file"] = first["file"]
				if sp, ok := first["span"].(map[string]any); ok {
					out["line"] = sp["start_line"]
				}
				if len(ev) > 1 {
					e.omit("paths.evidence")
				}
			}
		case "access":
			out[k] = accessOf(e, val)
		case "assumption":
			out[k] = val
			tagRow("MC-18")
			e.Assumptions = appendAssumption(e.Assumptions, str(val))
		case "link_assumptions":
			// A calls_route step's link assumptions (an unproven
			// origin, UP-V1, UP-V2): never a plain link (MC-25).
			for ck, cv := range e.linkAssumed(val, "MC-25") {
				out[ck] = cv
			}
		case "stale_reasons":
			out[k] = val
			e.unknown("MC-18", "stale_link", "a project link on this path is stale", val)
		case "kind", "source", "target", "relation", "provenance", "confidence", "via", "removed", "model", "source_repository", "target_repository":
			out[k] = val
		default:
			e.notModelled("change_impact.step."+k, val)
		}
	}
	return out
}

// impactUnknowns maps the traversal's unknowns (MC-25): blind spots, near
// misses, gap nodes, untraversed edges and relations not extracted.
func impactUnknowns(e *Envelope, repo string, val any) {
	m, _ := val.(map[string]any)
	prefix := ""
	if repo != "" {
		prefix = "member " + repo + ": "
	}
	for _, k := range sortedKeys(m) {
		v := m[k]
		switch k {
		case "blind_spots":
			// One unknown per list, paged like results (R14).
			if l := listOf(v); len(l) > 0 {
				e.unknown("MC-25", "blind_spot", prefix+fmt.Sprintf("possibly missing dependents, never paths: %s naming a reached function", plural(len(l), "unresolved call site")), e.listPage(l))
			}
		case "near_misses":
			if l := listOf(v); len(l) > 0 {
				e.unknown("MC-25", "near_miss", prefix+fmt.Sprintf("may depend on a reached route, never paths: %s listing it among candidates without a stored link", plural(len(l), "call")), e.listPage(l))
			}
		case "gap_nodes":
			if len(listOf(v)) > 0 {
				e.unknown("MC-25", "gap_nodes", prefix+"reached nodes in files with knowledge gaps: their own dependents may be missing", v)
			}
		case "untraversed":
			if um, _ := v.(map[string]any); len(um) > 0 {
				e.unknown("MC-25", "untraversed", prefix+"stored edges of these kinds lead on from reached nodes but no relation follows them (counts by kind)", um)
			}
		case "not_extracted":
			for _, ne := range listOf(v) {
				nm, _ := ne.(map[string]any)
				e.unknown("MC-25", "not_extracted", prefix+"relation "+str(nm["relation"])+" has no extracted facts here: dependents through it are unknown", ne)
			}
		default:
			e.notModelled("change_impact.unknowns."+k, v)
		}
	}
}

// tableUses maps I8's per-table answers: uses found are a lower bound,
// and every "may also be used by" owner is an unknown.
func tableUses(e *Envelope, repo string, val any) {
	for _, raw := range listOf(val) {
		m, _ := raw.(map[string]any)
		table := str(m["table"])
		found := listOf(m["found"])
		where := ""
		if repo != "" {
			where = " (member " + repo + ")"
		}
		e.unknown("MC-25", "table_uses_lower_bound", fmt.Sprintf("%s%s: %d uses found (a lower bound)", table, where, len(found)), m["found"])
		if l := listOf(m["may_also_be_used_by"]); len(l) > 0 {
			e.unknown("MC-25", "may_also_be_used_by", fmt.Sprintf("%s%s: %s may also use it (not bound; unbound: true marks a site whose candidates are unknown)", table, where, plural(len(l), "site")), e.listPage(l))
		}
	}
}

// memberParam adds the repository parameter in project mode (and checks
// it in one-repository mode), echoing the member.
func (s *Server) memberParam(e *Envelope, q url.Values, repository string) error {
	_, repo, err := s.member(repository, false)
	if err != nil {
		return err
	}
	if s.opt.Project != nil {
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	return nil
}
