package changes

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestMayAlsoUse is the one "may also be used by" rule of change impact
// and the MCP table tool (T030 review): entries naming the table, and
// unbound entries (candidates null) that may reach it, without the owners
// found; an unbound entry of a model-bound tool reaches only a model table
// of that tool, any other tool's reaches every table.
func TestMayAlsoUse(t *testing.T) {
	u := newUnknownUses()
	owner := func(id string, entries ...map[string]any) *contract.Node {
		list := make([]any, len(entries))
		for i, e := range entries {
			list[i] = e
		}
		return &contract.Node{Identity: id, Attributes: map[string]any{"table_access_unknown": list}}
	}
	u.add(owner("function:a.ts#named", map[string]any{"row": "CT-R1", "line": 3.0, "tool": "prisma", "reason": "r", "candidates": []any{"table:schema.prisma#Tag"}}))
	u.add(owner("function:a.ts#prismaUnbound", map[string]any{"row": "CT-R2", "line": 4.0, "tool": "prisma", "reason": "r", "candidates": nil}))
	u.add(owner("function:a.ts#sqlUnbound", map[string]any{"row": "CT-R3", "line": 5.0, "tool": "sql", "reason": "r"}))
	u.add(owner("function:a.ts#found", map[string]any{"row": "CT-R1", "line": 6.0, "tool": "prisma", "reason": "r", "candidates": []any{"table:schema.prisma#Tag"}}))
	u.add(owner("function:b.py#sqlmodelUnbound", map[string]any{"row": "CT-P1", "line": 7.0, "tool": "sqlmodel", "reason": "r", "candidates": nil}))
	prismaTable := &contract.Node{Identity: "table:schema.prisma#Tag", Kind: "table", Attributes: map[string]any{"source": "model", "tool": "prisma"}}
	migration := &contract.Node{Identity: "table:m/0001.sql#tag", Kind: "table", Attributes: map[string]any{"dialect": "postgres"}}
	owners := func(list []UnknownUse) []string {
		var out []string
		for _, e := range list {
			out = append(out, e.Owner)
		}
		return out
	}
	got := owners(u.MayAlsoUse(prismaTable, map[string]bool{"function:a.ts#found": true}))
	want := []string{"function:a.ts#named", "function:a.ts#prismaUnbound", "function:a.ts#sqlUnbound"}
	if len(got) != len(want) {
		t.Fatalf("prisma table: %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prisma table: %v, want %v", got, want)
		}
	}
	if got := owners(u.MayAlsoUse(migration, nil)); len(got) != 1 || got[0] != "function:a.ts#sqlUnbound" {
		t.Errorf("migration table: %v, want only the sql site", got)
	}
	for _, e := range u.MayAlsoUse(prismaTable, nil) {
		if e.Unbound != (e.Owner == "function:a.ts#prismaUnbound" || e.Owner == "function:a.ts#sqlUnbound") {
			t.Errorf("%s: unbound %v", e.Owner, e.Unbound)
		}
	}
}
