package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/traverse"
)

// tableArgs are the table tool's arguments.
type tableArgs struct {
	Target     string `json:"target" jsonschema:"table identity, or a table's name or qualified-name suffix at a name boundary (tables only)"`
	Detail     string `json:"detail,omitempty" jsonschema:"compact (default: column facts with claim counts by verification) or full (every stored claim with its row and span)"`
	Include    string `json:"include,omitempty" jsonschema:"both (default), schema (the table's facts only) or users (the code using it only)"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default) or all: which code users are listed"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"users per page (default 20, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
	unknownsArgs
}

// tableTool answers a table's schema (row MC-22) from /api/v0/schema/table
// and the code that reads, writes or builds a query on it (row MC-23,
// D7): the architecture chain's first upstream step from the table (CH6),
// whose edges carry the owner's table access with its verification and
// assumptions, plus the table_access_unknown sites that may also use it.
func tableTool(ctx context.Context, s *Server, in tableArgs, e *Envelope) error {
	tagRow("MC-22")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	limit := in.Limit
	if limit == 0 {
		limit = DefaultTableUsers
	}
	offset, n, err := pageParams(limit, in.Cursor)
	if err != nil {
		return err
	}
	detail, err := detailParam(in.Detail)
	if err != nil {
		return err
	}
	include := orDefault(in.Include, "both")
	if include != "both" && include != "schema" && include != "users" {
		return badRequest("include must be both, schema or users, not %q", in.Include)
	}
	scope := orDefault(in.Scope, "production")
	tagRow("MC-2")
	e.Request["target"], e.Request["detail"], e.Request["include"], e.Request["scope"], e.Request["limit"], e.Request["cursor"] = in.Target, detail, include, scope, n, in.Cursor
	if err := e.unknownsPaging(in.unknownsArgs); err != nil {
		return err
	}
	_, repo, err := s.member(in.Repository, false)
	if err != nil {
		return err
	}
	q := url.Values{}
	if s.opt.Project != nil {
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	r, err := s.resolve(ctx, e, repo, in.Target, targetLookup{what: "stored table", kind: "table"})
	if err != nil {
		return err
	}
	id := r.id
	tq := url.Values{"id": {id}}
	for k, v := range q {
		tq[k] = v
	}
	raw, err := s.reader.Read(ctx, "/api/v0/schema/table", tq)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	out := map[string]any{}
	tool := ""
	schema := e
	if include == "users" {
		// The schema is read for the table's tool, then left out.
		schema = newEnvelope(e.Tool)
		e.omitHow("table schema (columns, indexes, facts, history, neighbours)", "call table with include schema or both")
	}
	if err := tableSchema(schema, v, out, detail, &tool); err != nil {
		return err
	}
	if include == "users" {
		out = map[string]any{"table": map[string]any{"id": id}}
	}
	if include == "schema" {
		e.omitHow("users", "call table with include users or both")
		e.Results = []any{out}
		return nil
	}
	users, err := s.tableUsers(ctx, e, id, repo, scope, tool, detail)
	if err != nil {
		return err
	}
	out["users"] = page(e, users, offset, n)
	e.Results = []any{out}
	return nil
}

// tableSchema maps the schema table view's answer v into out (MC-22) and
// reports the table's tool.
func tableSchema(e *Envelope, v map[string]any, out map[string]any, detail string, tool *string) error {
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch k {
		case "api_version", "repository", "index":
		case "ide":
			e.omit(k)
		case "table":
			m, _ := val.(map[string]any)
			t := compactNode(e, m)
			attrs, _ := m["attributes"].(map[string]any)
			*tool = str(attrs["tool"])
			t["facts"] = factsOf(e, "table", attrs, detail)
			if ver := str(attrs["verification"]); ver != "" {
				e.label("verification", ver)
			}
			out["table"] = t
		case "set", "dialect", "dialects", "history", "unresolved_references", "external_references", "opaque_sites", "overlaps":
			out[k] = stripLinks(e, val)
		case "model_routes":
			out[k] = stripLinks(e, val)
			if mr, ok := val.(map[string]any); ok {
				if c, _ := asInt(mr["routes_not_fully_read"]); c > 0 {
					e.unknown("MC-22", "model_routes_not_fully_read", fmt.Sprintf("%d routes' request or response is not fully read: they may accept or return this table's model without it being listed", c), nil)
				}
			}
		case "completeness":
			out[k] = val
			e.label("completeness", str(val))
			if c := str(val); c != "complete" && c != "complete_in_source" {
				e.unknown("MC-22", "completeness_"+c, "completeness "+c+": columns or constraints may be missing; no absence is claimed", nil)
			}
		case "reasons":
			out[k] = val
			if len(listOf(val)) > 0 {
				e.unknown("MC-22", "table_reasons", "the table's schema has reasons it may be partial", val)
			}
		case "precision":
			out[k] = val
			labelPrecision(e, val)
		case "columns":
			var cols []any
			for _, c := range listOf(val) {
				cols = append(cols, columnOf(e, c, detail))
			}
			if cols == nil {
				cols = []any{}
			}
			out[k] = cols
		case "indexes":
			var idx []any
			for _, c := range listOf(val) {
				idx = append(idx, columnOf(e, c, detail))
			}
			if idx == nil {
				idx = []any{}
			}
			out[k] = idx
		case "neighbours":
			out[k] = neighboursOf(e, val)
		case "truncated":
			out[k] = val
			if tr, ok := val.(map[string]any); ok {
				tagRow("MC-3")
				e.label("bound", "neighbours")
				tables, _ := asInt(tr["tables_omitted"])
				e.Bounds = append(e.Bounds, Bound{Cap: "neighbours", Limit: server.NeighboursCap, Beyond: &tables, Row: "MC-3",
					Text: fmt.Sprintf("neighbour cap reached (%d tables per direction): %d tables and %v column pairs not listed", server.NeighboursCap, tables, tr["pairs_omitted"])})
			}
		case "rests_on":
			out[k] = val
			for _, a := range listOf(val) {
				e.assume(str(a), "MC-22")
			}
		default:
			e.notModelled("table."+k, val)
		}
	}
	return nil
}

// tableUsers reads the chain's first upstream step from the table (CH6):
// every member is code that reads, writes or builds a query on it, with
// the edge's wording, tier and access (MC-23). The list is a lower bound:
// the table_access_unknown sites that may also use the table are
// unknowns, and the answer never says "no users".
func (s *Server) tableUsers(ctx context.Context, e *Envelope, id, repo, scope, tool, detail string) ([]any, error) {
	tagRow("MC-23")
	q := url.Values{"id": {id}, "direction": {"up"}, "depth": {"1"}, "scope": {scope}}
	if s.opt.Project != nil {
		q.Set("repository", repo)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/architecture/chain", q)
	if err != nil {
		return nil, err
	}
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	up, _ := v["up"].(map[string]any)
	caps, _ := v["caps"].(map[string]any)
	into := map[string][]any{}
	for _, rawEdge := range listOf(up["edges"]) {
		ed, _ := rawEdge.(map[string]any)
		use := chainEdgeOf(e, ed)
		if detail == detailCompact {
			use = compactUse(e, use)
		}
		into[str(ed["to"])] = append(into[str(ed["to"])], use)
	}
	users := []any{}
	for _, rawStep := range listOf(up["steps"]) {
		st, _ := rawStep.(map[string]any)
		if str(st["step"]) != "1" {
			continue
		}
		if c := str(st["hidden_test"]); c != "0" && c != "" {
			e.unknown("MC-23", "hidden_test", c+" test-scope users kept out (scope all lists them)", nil)
		}
		for _, b := range listOf(st["bounds"]) {
			// A fan-out bound at step 1: more users than the cap.
			if m, _ := b.(map[string]any); str(m["cap"]) != traverse.CapDepth {
				e.Bounds = append(e.Bounds, chainBound(e, "users", b, caps, 1))
			}
		}
		for _, rawMember := range listOf(st["members"]) {
			m, _ := rawMember.(map[string]any)
			// The chain member's keys, as the chain tool maps them (R7).
			u := map[string]any{}
			for _, k := range sortedKeys(m) {
				switch {
				case !chainMemberKeys[k]:
					e.notModelled("users."+k, m[k])
				case m[k] != nil:
					u[k] = m[k]
				}
			}
			e.label("tier", tierFamily(str(m["tier"])))
			via := into[str(m["id"])]
			if via == nil {
				via = []any{}
			}
			u["access"] = via
			users = append(users, u)
		}
	}
	e.omit("users.chain")
	nodes, err := s.opt.Store.NodesByIdentity(ctx, repo, []string{id})
	if err != nil {
		return nil, fmt.Errorf("reading table %s: %w", id, err)
	}
	var table *contract.Node
	if len(nodes) == 1 {
		table = &nodes[0]
	}
	if !changes.AccessCovered(table) {
		// R3: no code-to-table row reads this table's source, so its users
		// are not extracted: never "0 uses found".
		tagRow("MC-23")
		text := fmt.Sprintf("table access is not extracted for this table's source (tool %s, dialect %s): its users are unknown, not zero",
			orDefault(tool, "none"), orDefault(str(attrOf(table, "dialect")), "none"))
		if len(users) > 0 {
			text += fmt.Sprintf("; the %d users listed come from stored edges and are a lower bound", len(users))
		}
		e.unknown("MC-23", "users_not_extracted", text, nil)
		return users, nil
	}
	// R9: the one "may also be used by" rule of change impact (I8).
	index, err := changes.ReadUnknownUses(ctx, s.opt.Store, repo)
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, u := range users {
		found[str(u.(map[string]any)["id"])] = true
	}
	var named, unbound []any
	unboundSites := 0
	byOwner := map[string]map[string]any{}
	for _, u := range index.MayAlsoUse(table, found) {
		if !u.Unbound {
			named = append(named, u)
			continue
		}
		// Unbound sites grouped by owner: lines and reasons (the rows say
		// no more than the reason).
		unboundSites++
		o := byOwner[u.Owner]
		if o == nil {
			o = map[string]any{"owner": u.Owner, "tool": u.Tool, "lines": []int{}, "reasons": map[string]int{}}
			byOwner[u.Owner] = o
			unbound = append(unbound, o)
		}
		o["lines"] = append(o["lines"].([]int), u.Line)
		o["reasons"].(map[string]int)[u.Reason]++
	}
	if len(named) > 0 {
		e.unknown("MC-23", "may_also_be_used_by", fmt.Sprintf("may also use this table, not bound: %s naming it among candidates", plural(len(named), "site")), e.listPage(named))
	}
	if len(unbound) > 0 {
		e.unknown("MC-23", "unbound_sites", fmt.Sprintf("any of these may use this table: %s in %s binding no table (candidates unknown)", plural(unboundSites, "table access site"), plural(len(unbound), "function or file")), e.listPage(unbound))
	}
	text := fmt.Sprintf("%d uses found (a lower bound): calls the indexer did not resolve and table access forms no row covers are not listed", len(users))
	e.unknown("MC-23", "users_lower_bound", text, nil)
	return users, nil
}

// attrOf is a stored attribute of n; nil for a nil node.
func attrOf(n *contract.Node, key string) any {
	if n == nil {
		return nil
	}
	return n.Attributes[key]
}

// columnOf maps one column (or index) of the table view: its node's
// location, its stored facts, the view's default state and foreign keys,
// and its claims folded into counts by verification (compact) or listed
// (full).
func columnOf(e *Envelope, c any, detail string) map[string]any {
	m, _ := c.(map[string]any)
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		val := m[k]
		switch k {
		case "node":
			nm, _ := val.(map[string]any)
			for ck, cv := range compactNode(e, nm) {
				out[ck] = cv
			}
			attrs, _ := nm["attributes"].(map[string]any)
			out["facts"] = factsOf(e, "column", attrs, detail)
		case "default_state":
			out[k] = val
		case "foreign_keys":
			out[k] = stripLinks(e, val)
		case "claims":
			out["claims"] = claimsOf(e, val, detail)
		default:
			e.notModelled("column."+k, val)
		}
	}
	return out
}

// factsOf passes a table's or column's stored attributes on as its facts.
// The claim list is folded into claims (claimsOf); in compact detail,
// attributes whose value is null or empty are left out and named
// in omitted (they state "not set", which full detail shows).
func factsOf(e *Envelope, what string, attrs map[string]any, detail string) map[string]any {
	out := map[string]any{}
	for _, k := range sortedKeys(attrs) {
		val := attrs[k]
		if k == "claims" {
			continue
		}
		if detail == detailCompact && emptyValue(val) {
			e.omitHow(what+".facts (null or empty)", "call again with detail full")
			continue
		}
		out[k] = val
	}
	return out
}

// claimsOf folds stored model claims into counts by verification, listing
// every claim that is not oracle-verified by fact and row (compact), or
// passes the whole list without editor links (full).
func claimsOf(e *Envelope, val any, detail string) any {
	list := listOf(val)
	if val == nil {
		return nil
	}
	if detail == detailFull {
		var out []any
		for _, c := range list {
			m, _ := c.(map[string]any)
			cm := map[string]any{}
			for k, v := range m {
				if k == "link" {
					e.omit("claims.link")
					continue
				}
				cm[k] = v
			}
			e.label("verification", orDefault(str(m["verification"]), "unknown"))
			out = append(out, cm)
		}
		if out == nil {
			out = []any{}
		}
		return out
	}
	counts := map[string]int{}
	notOracle := []any{}
	for _, c := range list {
		m, _ := c.(map[string]any)
		ver := orDefault(str(m["verification"]), "unknown")
		counts[ver]++
		e.label("verification", ver)
		if ver != "oracle" {
			notOracle = append(notOracle, map[string]any{"fact": m["fact"], "row": m["row"], "verification": ver})
		}
	}
	e.omitHow("claims (each claim's row, file and span)", "call again with detail full")
	return map[string]any{"by_verification": counts, "not_oracle": notOracle}
}

// neighboursOf compacts the foreign-key neighbours: each table's identity,
// name, completeness and column pairs.
func neighboursOf(e *Envelope, val any) map[string]any {
	m, _ := val.(map[string]any)
	out := map[string]any{}
	for _, dir := range sortedKeys(m) {
		var list []any
		for _, raw := range listOf(m[dir]) {
			nb, _ := raw.(map[string]any)
			t, _ := nb["table"].(map[string]any)
			entry := compactNode(e, t)
			entry["completeness"] = nb["completeness"]
			entry["pairs"] = nb["pairs"]
			list = append(list, entry)
		}
		if list == nil {
			list = []any{}
		}
		out[dir] = list
	}
	return out
}

// stripLinks drops editor links ("link", "ide") from a view value, naming
// them in omitted; everything else is passed on as served.
func stripLinks(e *Envelope, val any) any {
	switch x := val.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			if k == "link" || k == "ide" {
				e.omit(k)
				continue
			}
			out[k] = stripLinks(e, v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = stripLinks(e, v)
		}
		return out
	}
	return val
}

func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case string:
		return x == ""
	}
	return false
}

func asInt(v any) (int, bool) {
	var n int
	_, err := fmt.Sscan(str(v), &n)
	return n, err == nil
}

// compactUse is one table use in compact detail: the edge from the table
// (always the table, never dashed) and the access entry without its
// wording (the verification label explains it) and a null column use.
func compactUse(e *Envelope, use map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range use {
		switch {
		case k == "from", k == "dashed", k == "count" && str(v) == "1":
			e.omitHow("users.access from, dashed and a count of 1", "the edge always starts at the table and is never dashed; call table again with detail full")
		case k == "access":
			a, ok := v.(map[string]any)
			if !ok {
				out[k] = v
				continue
			}
			ca := map[string]any{}
			for ak, av := range a {
				if ak == "verification_wording" || ak == "column_use" && av == nil {
					e.omitHow("users.access verification_wording and a null column_use", "the verification label explains the wording; call table again with detail full")
					continue
				}
				ca[ak] = av
			}
			out[k] = ca
		default:
			out[k] = v
		}
	}
	return out
}

// plural is "1 site" or "N sites".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, " or file") {
		return fmt.Sprintf("%d functions or files", n)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
