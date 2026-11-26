package store_test

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// TestNodesContains compares Query.Contains (SQL prefilter plus the Go
// test) with a brute-force scan of the dump on both backends: LIKE
// metacharacters, case variants, non-ASCII names (folded only in ASCII),
// NULL name columns and the language fixtures (ADR 0089).
func TestNodesContains(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		compared := 0
		for _, repository := range fixtureRepositories(t, s) {
			dump, err := s.Dump(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			queries := map[string]bool{"%": true, "_": true, "!": true, `\`: true, "a_b": true, "A%B": true, "BUILD_": true, "ünï": true, "ÜNÏ": true, "Ünï": true, "QQQQ": true, "eee": true, "nul": true, "\x00": true, "mod.F1": true, ".": true}
			for i, n := range dump.Nodes {
				if i%7 != 0 || len(n.Name) < 3 {
					continue
				}
				queries[n.Name[1:3]] = true
				queries[store.FoldASCII(n.Name[:2])] = true
			}
			for q := range queries {
				var want []string
				for _, n := range dump.Nodes {
					if store.ContainsFolded(n.Name, q) || store.ContainsFolded(n.QualifiedName, q) {
						want = append(want, n.Identity)
					}
				}
				got, err := s.Nodes(ctx, repository, store.Query{Contains: q})
				if err != nil {
					t.Fatal(err)
				}
				if ids := identityList(got); !reflect.DeepEqual(ids, want) {
					t.Fatalf("%s Contains %q = %v, want %v", repository, q, ids, want)
				}
				limited, err := s.Nodes(ctx, repository, store.Query{Contains: q, Kind: "function", Limit: 2})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range limited {
					if n.Kind != "function" || !(store.ContainsFolded(n.Name, q) || store.ContainsFolded(n.QualifiedName, q)) {
						t.Fatalf("Contains %q with kind returned %v", q, n.Identity)
					}
				}
				compared++
			}
		}
		if compared < 50 {
			t.Fatalf("compared only %d queries", compared)
		}
	})
}

// TestFoldASCII pins the fold: ASCII letters only.
func TestFoldASCII(t *testing.T) {
	for in, want := range map[string]string{"GetUser": "getuser", "ÜNÏ": "ÜnÏ", "K": "K", "A_B%": "a_b%"} {
		if got := store.FoldASCII(in); got != want {
			t.Errorf("FoldASCII(%q) = %q, want %q", in, got, want)
		}
	}
	if store.ContainsFolded("Kelvin", "kelvin") {
		t.Error("the Kelvin sign must not fold to k")
	}
}

// TestFactFiles compares FactFiles with the dump on both backends: node
// files other than file nodes, plus every edge evidence file.
func TestFactFiles(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		for _, repository := range fixtureRepositories(t, s) {
			dump, err := s.Dump(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{}
			for _, n := range dump.Nodes {
				if n.Kind != "file" && n.File != "." && n.File != "" {
					want[n.File] = true
				}
			}
			for _, e := range dump.Edges {
				for _, ev := range e.Evidence {
					want[ev.File] = true
				}
			}
			got, err := s.FactFiles(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s FactFiles = %v, want %v", repository, sortedSet(got), sortedSet(want))
			}
		}
		empty, err := s.FactFiles(ctx, "absent")
		if err != nil || len(empty) != 0 {
			t.Fatal(empty, err)
		}
	})
}

func identityList(nodes []contract.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Identity)
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
