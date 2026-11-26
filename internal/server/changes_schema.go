package server

import (
	"context"
	"fmt"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// ChangeSchema gives internal/changes the schema rules of the Schema view
// (ADR 0048, ADR 0055): each group's table_list and a table's
// completeness, computed by the same code as /api/v0/schema/sets, so a
// change item never disagrees with what the Schema view shows. The CLI
// passes it too.
type ChangeSchema struct{}

var _ changes.Schema = ChangeSchema{}

// TableLists reads the groups of the graph stored under repository (the
// repository itself or a ref namespace) as /schema/sets builds them.
func (ChangeSchema) TableLists(ctx context.Context, s store.Store, repository string) (map[string]string, error) {
	h := &apiHandler{opt: Options{Repository: repository, Store: s}}
	read, err := computeSchemaSets(h, nil)
	if err != nil {
		return nil, err
	}
	rd := &reader{ctx: ctx, s: s, repo: repository}
	runs, err := rd.runs()
	if err != nil {
		return nil, err
	}
	body, err := read(ctx, rd, runs)
	if err != nil {
		return nil, fmt.Errorf("reading schema groups of %s: %w", repository, err)
	}
	out := map[string]string{}
	for _, set := range body.(schemaSetsResponse).Sets {
		if set.Set == nil {
			continue
		}
		value := "unknown"
		if set.TableList != nil {
			value = *set.TableList
		}
		out[*set.Set] = value
	}
	return out, nil
}

// TableCompleteness applies the completeness rule of the table's source.
func (ChangeSchema) TableCompleteness(n contract.Node) string { return tableCompleteness(n) }
