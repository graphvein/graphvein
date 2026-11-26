package mcp

import (
	"context"
	"fmt"
	"net/url"
)

type linkChangesArgs struct {
	Base   string `json:"base,omitempty" jsonschema:"previous (default: the pass before the newest kept one) or pass:N, a kept link pass"`
	Limit  int    `json:"limit,omitempty" jsonschema:"changed links per page (default 50, at most 500)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

// linkChangeKeys are the keys of one changed link (store.ProjectLinkKey).
var linkChangeKeys = keySet("kind", "source_repository", "source_identity", "target_repository", "target_identity")

// The states of GET /api/v0/project/links/changes (its "state" key).
const (
	stateCompared     = "compared"
	stateSamePass     = "same_pass"
	stateNoComparison = "no_comparison"
)

// linkChangesTool answers which project links changed between two kept
// link passes (row MC-32) from GET /api/v0/project/links/changes through
// server.Reader, the view graphvein serve --project registers. It only
// reads the link journal: it never links and never indexes. Outside
// project mode there are no project links, and it says so.
func linkChangesTool(ctx context.Context, s *Server, in linkChangesArgs, e *Envelope) error {
	tagRow("MC-32")
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	base := orDefault(in.Base, "previous")
	tagRow("MC-2")
	e.Request["base"], e.Request["limit"], e.Request["cursor"] = base, n, in.Cursor
	if s.opt.Project == nil {
		return &toolError{code: "project_only", message: fmt.Sprintf("link changes compare the link passes of a project; this server serves repository %s alone, which has no project links: start the server with graphvein mcp --project MANIFEST", s.opt.Repository)}
	}
	raw, err := s.reader.Read(ctx, "/api/v0/project/links/changes", url.Values{"base": {base}})
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	// The view's state says whether it compared two passes (T044):
	// "compared" with a diff in links, "same_pass" when the base is the
	// newest pass, "no_comparison" with fewer than two passes. Only
	// "compared" lists changes; the other two are never "no change".
	state := str(v["state"])
	summary := map[string]any{"base": v["base"], "state": v["state"], "compared": state == stateCompared}
	note := str(v["links_note"])
	switch state {
	case stateCompared:
	case stateSamePass:
		// The view's note already says nothing was compared.
		e.unknown("MC-32", "same_pass", note, nil)
	case stateNoComparison:
		e.unknown("MC-32", "no_comparison", note+": nothing compared, not proof that no link changed", nil)
	default:
		// A state this tool does not know: list nothing and say so.
		summary["compared"] = false
		e.notModelled("link_changes.state", v["state"])
	}
	var items []any
	for _, k := range sortedKeys(v) {
		switch k {
		case "api_version", "project", "base", "state", "links_note":
		case "links":
			links, ok := v[k].(map[string]any)
			if !ok || state != stateCompared {
				continue
			}
			for _, lk := range sortedKeys(links) {
				switch lk {
				case "project":
				case "from_pass", "to_pass", "manifest_changed":
					summary[lk] = links[lk]
				case "members":
					// The members and runs each pass linked, as stored.
					pair := listOf(links[lk])
					if len(pair) == 2 {
						summary["members"] = map[string]any{"from_pass": pair[0], "to_pass": pair[1]}
					} else {
						e.notModelled("links.members", links[lk])
					}
				case "added", "removed", "changed":
				default:
					e.notModelled("links."+lk, links[lk])
				}
			}
			// The view lists added, removed and changed links each in key
			// order; the results keep that order, one change per item.
			for _, change := range []string{"added", "removed", "changed"} {
				list := listOf(links[change])
				summary[change] = len(list)
				for _, item := range list {
					m, _ := item.(map[string]any)
					out := e.mapKeys("link", m, linkChangeKeys, nil)
					out["change"] = change
					items = append(items, out)
				}
			}
		default:
			e.notModelled("link_changes."+k, v[k])
		}
	}
	if lp := e.Freshness.LinkPass; lp != nil && len(lp.StaleReasons) > 0 {
		e.unknown("MC-18", "link_pass_stale", "the newest link pass is stale: the links would differ if linked now, and this comparison does not show that", lp.StaleReasons)
	}
	e.Summary = summary
	if items == nil {
		items = []any{}
	}
	e.Results = page(e, items, offset, n)
	// Only the labels this page uses (MC-5).
	for _, r := range e.Results {
		change := str(r.(map[string]any)["change"])
		e.label("link_change", change)
		if change == "changed" {
			e.omitHow("link.payload", "the link journal keeps a payload hash per link, not the payload: the endpoint tool shows a route's current project callers with their confidence, evidence and assumption")
		}
	}
	return nil
}
