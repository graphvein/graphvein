package changes

import (
	"context"
	"fmt"

	"github.com/graphvein/graphvein/internal/store"
)

// Report is the document graphvein changes --json prints: the summary and
// every item a filter selects, in the HTTP items shape, plus the impact of
// the requested seeds.
type Report struct {
	Repository string        `json:"repository"`
	Base       string        `json:"base"`
	Summary    Summary       `json:"summary"`
	Items      []Item        `json:"items"`
	Hidden     int           `json:"hidden_tests"`
	Impacts    []NamedImpact `json:"impacts,omitempty"`
}

// NamedImpact is the traversal from one seed item.
type NamedImpact struct {
	Seed   string        `json:"seed"`
	Impact *ImpactResult `json:"impact"`
}

// BuildReport pages through every item f selects and runs Impact for each
// of seeds (item ids) at the default depth.
func BuildReport(ctx context.Context, s store.Store, c *Comparison, f Filter, seeds []string) (Report, error) {
	out := Report{Repository: c.Repository, Base: c.Base.String(), Summary: c.Summary, Items: []Item{}}
	for _, id := range seeds {
		r, err := Impact(ctx, s, c, ImpactRequest{ID: id})
		if err != nil {
			return out, fmt.Errorf("impact of %s: %w", id, err)
		}
		out.Impacts = append(out.Impacts, NamedImpact{Seed: id, Impact: r})
	}
	page := Page{Limit: MaxLimit}
	for {
		p, err := c.Items(f, page)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, p.Items...)
		out.Hidden = p.Hidden
		if p.Next == nil {
			return out, nil
		}
		page.Cursor = *p.Next
	}
}
