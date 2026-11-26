package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// MissingSite reports name-based uncertainty without claiming a graph edge.
type MissingSite struct {
	File    string        `json:"file"`
	Span    contract.Span `json:"span"`
	Caller  *string       `json:"caller"`
	Name    *string       `json:"name"`
	Matches []string      `json:"matches"`
}

func simpleName(q string) string {
	q = strings.SplitN(q, "@", 2)[0]
	parts := strings.Split(q, ".")
	return parts[len(parts)-1]
}

func blindSpots(ctx context.Context, s store.Store, repository string, files []contract.Node, out *Response) error {
	names := map[string][]string{}
	nodes := []Node{*out.Target}
	for _, r := range out.Results {
		nodes = append(nodes, r.Node)
	}
	for _, n := range nodes {
		name := simpleName(n.QualifiedName)
		names[name] = append(names[name], n.Identity)
	}
	for name := range names {
		sort.Strings(names[name])
	}
	for _, n := range files {
		value, ok := n.Attributes["unresolved_unknown_call_sites"]
		if !ok {
			continue
		}
		out.BlindSpotData = true
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("reading blind spots for %s: %w", n.File, err)
		}
		var sites []MissingSite
		if err := json.Unmarshal(raw, &sites); err != nil {
			return fmt.Errorf("reading blind spots for %s: %w", n.File, err)
		}
		for _, site := range sites {
			if site.Name == nil || len(names[*site.Name]) == 0 {
				continue
			}
			site.File = n.File
			site.Matches = names[*site.Name]
			out.PossiblyMissingCallSites = append(out.PossiblyMissingCallSites, site)
		}
	}
	sort.Slice(out.PossiblyMissingCallSites, func(i, j int) bool {
		a, b := out.PossiblyMissingCallSites[i], out.PossiblyMissingCallSites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Span.StartLine != b.Span.StartLine {
			return a.Span.StartLine < b.Span.StartLine
		}
		if a.Span.StartColumn != b.Span.StartColumn {
			return a.Span.StartColumn < b.Span.StartColumn
		}
		if a.Span.EndLine != b.Span.EndLine {
			return a.Span.EndLine < b.Span.EndLine
		}
		return a.Span.EndColumn < b.Span.EndColumn
	})
	callers := []string{}
	for _, site := range out.PossiblyMissingCallSites {
		if site.Caller != nil {
			callers = append(callers, *site.Caller)
		} else {
			callers = append(callers, fileIdentity(site.File))
		}
	}
	resolved, err := s.Edges(ctx, repository, callers, []string{"calls"}, store.Outgoing)
	if err != nil {
		return err
	}
	for _, site := range out.PossiblyMissingCallSites {
		for _, e := range resolved {
			for _, v := range e.Evidence {
				if site.File == v.File && site.Span == v.Span {
					return fmt.Errorf("blind spot in %s is already resolved", site.File)
				}
			}
		}
	}
	out.PossiblyMissingCallers = len(out.PossiblyMissingCallSites)
	return nil
}
