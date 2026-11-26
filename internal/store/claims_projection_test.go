package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestNodesWithoutModelClaims checks the /schema/sets column read (ADR
// 0057) on both backends: a model record comes back exactly as Nodes reads
// it minus its claims attribute, whatever that holds (number text, a NUL
// and non-ASCII strings included: Postgres takes its Go path for a
// payload with a \u0000 escape); any other node verbatim; Query filters,
// order and repository isolation as in Nodes.
func TestNodesWithoutModelClaims(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		claim := func(v any) map[string]any {
			c := map[string]any{"fact": "type", "row": "F5", "file": "a.py", "span": map[string]any{"start_line": 3}}
			if v != nil {
				c["verification"] = v
			}
			return c
		}
		span := &contract.Span{StartLine: 1, EndLine: 1}
		node := func(kind, qualified, name string, attrs map[string]any) contract.Node {
			id := kind + ":a.py#" + qualified
			return contract.Node{Type: "node", Identity: id, Kind: kind, Level: 4, Name: name, QualifiedName: qualified, File: "a.py", Span: span, ContentHash: contract.ContentHash([]byte(id)), Attributes: attrs}
		}
		tricky := map[string]any{
			"source": "model", "name": "x", "big": json.Number("9007199254740993"), "exp": json.Number("1e+21"),
			"neg": json.Number("-0"), "frac": json.Number("1.50"), "nul": "a\x00b", "text": "é 😀 \"q\" \\u0000",
			"nested": map[string]any{"claims": []any{"kept"}, "n": json.Number("2E-7")}, "claims_note": "kept", "empty": map[string]any{}, "none": nil,
			"claims": []any{claim("oracle"), claim("mapped"), claim(nil), claim(json.Number("3")), "not an object", claim("mapped"), claim("")},
		}
		// Without the NUL and the escaped "\u0000" text, Postgres removes
		// and counts in SQL too; with them it leaves the payload to Go.
		plain := map[string]any{}
		for k, v := range tricky {
			if k != "nul" && k != "text" {
				plain[k] = v
			}
		}
		plain["text"] = "é 😀 \"q\" <&>"
		nodes := []contract.Node{
			node("column", "A.v", "v", plain),
			node("column", "A.x", "x", tricky),
			node("column", "A.y", "y", map[string]any{"source": "model", "claims": []any{}}),
			node("column", "A.z", "z", map[string]any{"source": "model"}),
			node("column", "A.w", "w", map[string]any{"source": "model", "claims": "not a list"}),
			node("column", "t.x", "x", map[string]any{"claims": []any{claim("oracle")}, "source": "migration"}),
			node("table", "A", "A", map[string]any{"source": "model", "verification": "oracle"}),
		}
		for _, repo := range []string{"one", "two"} {
			tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: repo, GeneratedAt: "2026-11-09T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: "a.py", Language: "python", Precision: "best_effort", ContentHash: contract.ContentHash([]byte("a"))}); err != nil {
				t.Fatal(err)
			}
			list := nodes
			if repo == "two" {
				list = nodes[1:2]
			}
			for _, n := range list {
				if err := tx.UpsertNode(ctx, n); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}

		for _, q := range []store.Query{{Kind: "column"}, {}, {Kind: "column", Term: "x"}, {Kind: "column", Limit: 2}, {Identity: "column:a.py#A.w"}} {
			stored, err := s.Nodes(ctx, "one", q)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.NodesWithoutModelClaims(ctx, "one", q)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(stored) || len(got) == 0 {
				t.Fatalf("%+v: %d nodes, Nodes reads %d", q, len(got), len(stored))
			}
			for i, n := range stored {
				if n.Attributes["source"] == "model" {
					attrs := map[string]any{}
					for k, v := range n.Attributes {
						if k != "claims" {
							attrs[k] = v
						}
					}
					n.Attributes = attrs
				}
				if !reflect.DeepEqual(got[i], n) {
					t.Errorf("%+v: node %d\n got %#v\nwant %#v", q, i, got[i], n)
				}
			}
		}
		if got, err := s.NodesWithoutModelClaims(ctx, "two", store.Query{}); err != nil || len(got) != 1 {
			t.Errorf("repository two: %v, %v", got, err)
		}
		if got, err := s.NodesWithoutModelClaims(ctx, "absent", store.Query{}); err != nil || got == nil || len(got) != 0 {
			t.Errorf("unknown repository: %v, %v", got, err)
		}
		if _, err := s.NodesWithoutModelClaims(ctx, "one", store.Query{Limit: -1}); err == nil {
			t.Error("negative limit accepted")
		}
	})
}
