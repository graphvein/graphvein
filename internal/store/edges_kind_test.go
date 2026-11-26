package store_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestEdgesOfKind checks EdgesOfKind on both backends: exactly the edges
// of the kind Dump holds, in Edges' order (kind, source, target) in byte
// order also where a database collation would differ, none of another
// kind or repository, and an empty list for a kind with no edge.
func TestEdgesOfKind(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		// Upper- and lower-case names: byte order puts "B" before "a".
		a, b, c := gnode("a.ts", "a", nil), gnode("B.ts", "B", nil), gnode("a.ts", "c", nil)
		// An edge's evidence is in its source's file.
		edge := func(kind string, source, target contract.Node) contract.Edge {
			return gedge(kind, source, target, source.File)
		}
		g := graph{
			files: []contract.File{gfile("a.ts", "a"), gfile("B.ts", "B")},
			nodes: []contract.Node{a, b, c},
			edges: []contract.Edge{edge("calls", a, b), edge("calls", b, c), edge("calls", a, c), edge("imports", a, b), edge("references", c, a)},
		}
		_, dump := mustWrite(t, s, "kinds", g)
		mustWrite(t, s, "other", graph{files: []contract.File{gfile("a.ts", "a")}, nodes: []contract.Node{a, c}, edges: []contract.Edge{edge("calls", c, a)}})

		got, err := s.EdgesOfKind(ctx, "kinds", "calls")
		if err != nil {
			t.Fatal(err)
		}
		want := []contract.Edge{}
		for _, e := range dump.Edges {
			if e.Kind == "calls" {
				want = append(want, e)
			}
		}
		if len(want) != 3 || !reflect.DeepEqual(got, want) {
			t.Fatalf("EdgesOfKind(calls) = %+v, want the dump's %+v", got, want)
		}
		if got[0].Source != b.Identity {
			t.Errorf("first calls edge from %s, want %s (byte order)", got[0].Source, b.Identity)
		}
		viaEdges, err := s.Edges(ctx, "kinds", []string{a.Identity, b.Identity, c.Identity}, []string{"calls"}, store.Outgoing)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, viaEdges) {
			t.Errorf("EdgesOfKind(calls) = %+v, Edges = %+v; want the same edges in the same order", got, viaEdges)
		}
		none, err := s.EdgesOfKind(ctx, "kinds", "contains")
		if err != nil || none == nil || len(none) != 0 {
			t.Errorf("EdgesOfKind(contains) = %v, %v; want an empty list", none, err)
		}
	})
}
