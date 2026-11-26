package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestNodesAttribute checks Query.Attribute on both backends: only nodes
// whose attributes hold the key come back (null values included), in
// identity order, never a node that merely mentions the key in a value or
// under another case; other filters still apply; a key the SQL prefilter
// cannot express is still filtered exactly.
func TestNodesAttribute(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		span := &contract.Span{StartLine: 1, EndLine: 1}
		node := func(kind, qualified string, attrs map[string]any) contract.Node {
			id := kind + ":a.py#" + qualified
			return contract.Node{Type: "node", Identity: id, Kind: kind, Level: 4, Name: qualified, QualifiedName: qualified, File: "a.py", Span: span, ContentHash: contract.ContentHash([]byte(id)), Attributes: attrs}
		}
		nodes := []contract.Node{
			node("function", "b", map[string]any{"table_access_unknown": []any{map[string]any{"tool": "sql"}}}),
			node("function", "a", map[string]any{"table_access_unknown": nil}),
			node("function", "c", map[string]any{"note": "table_access_unknown"}),
			node("function", "d", map[string]any{"TABLE_ACCESS_UNKNOWN": []any{}}),
			node("function", "e", map[string]any{"tableXaccess_unknown": []any{}}),
			node("function", "f", map[string]any{"nested": map[string]any{"table_access_unknown": 1}}),
			node("method", "g", map[string]any{"table_access_unknown": []any{}}),
			node("function", "h", map[string]any{"odd key%": true}),
		}
		tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "attr", GeneratedAt: "2026-11-18T00:00:00Z"})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: "a.py", Language: "python", Precision: "best_effort", ContentHash: contract.ContentHash([]byte("a"))}); err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			if err := tx.UpsertNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			q    store.Query
			want []string
		}{
			{store.Query{Kind: "function", Attribute: "table_access_unknown"}, []string{"a", "b"}},
			{store.Query{Attribute: "table_access_unknown"}, []string{"a", "b", "g"}},
			{store.Query{Attribute: "table_access_unknown", Limit: 1}, []string{"a"}},
			{store.Query{Attribute: "odd key%"}, []string{"h"}},
			{store.Query{Attribute: "absent"}, nil},
		} {
			got, err := s.Nodes(ctx, "attr", tc.q)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, n := range got {
				names = append(names, n.QualifiedName)
			}
			if len(names) != len(tc.want) {
				t.Errorf("%+v: %v, want %v", tc.q, names, tc.want)
				continue
			}
			for i := range names {
				if names[i] != tc.want[i] {
					t.Errorf("%+v: %v, want %v", tc.q, names, tc.want)
				}
			}
			without, err := s.NodesWithoutModelClaims(ctx, "attr", tc.q)
			if err != nil || len(without) != len(got) {
				t.Errorf("%+v: NodesWithoutModelClaims %d nodes, %v", tc.q, len(without), err)
			}
		}
	})
}
