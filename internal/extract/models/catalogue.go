package models

import (
	"slices"
	"sort"
	"sync"
)

// The assumption catalogue of the model contracts, per tool, for the
// schema API's /api/v0/schema/assumptions (T018f, ADR 0055 option 6A).
// Each front end registers its catalogue once (from an init function);
// the server lists every registered tool. Row and assumption ids of later
// tools carry a tool prefix (PR-, DR-, ...), so they never collide with
// SQLModel's unprefixed ids within the "models" source.

// Assumption is one environment assumption of a model contract.
// FoldedInto names the assumption that now covers it, or is empty while it
// is in force.
type Assumption struct {
	ID, Title, Text, FoldedInto string
}

// ContractEntry is one row of a model contract: Branch is the function
// that holds its claim branch ("none" for a row without one), Summary its
// claim as the docs table summarises it, and Assumptions the assumptions
// its claims rest on.
type ContractEntry struct {
	ID, Branch, Summary string
	Assumptions         []string
}

// Catalogue is one tool's contract catalogue.
type Catalogue struct {
	Tool        string
	Assumptions []Assumption
	Rows        []ContractEntry
}

var (
	cataloguesMu sync.Mutex
	catalogues   = map[string]Catalogue{}
)

// Register adds a tool's catalogue; registering a tool twice replaces it.
func Register(c Catalogue) {
	cataloguesMu.Lock()
	defer cataloguesMu.Unlock()
	catalogues[c.Tool] = c
}

// Catalogues returns the registered catalogues sorted by tool.
func Catalogues() []Catalogue {
	cataloguesMu.Lock()
	defer cataloguesMu.Unlock()
	out := make([]Catalogue, 0, len(catalogues))
	for _, c := range catalogues {
		out = append(out, Catalogue{Tool: c.Tool, Assumptions: slices.Clone(c.Assumptions), Rows: slices.Clone(c.Rows)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}
