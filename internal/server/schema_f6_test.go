package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// Tests of the decisions on T018f Revision 2's last two
// points: (a) a migration table's overlaps are null unless every model
// table is listed; (b) /sets serves the repository's B1 reasons in
// model_list_reasons for the "No model table claimed" card.

// overlapRepo writes a sqlx migration set creating table account and,
// with model, a SQLModel class Account; with big, a .py file over the
// index's file size limit.
func overlapRepo(t *testing.T, model, big bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Cargo.toml", "[package]\nname = \"f6\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n")
	write("migrations/0001_init.sql", "CREATE TABLE account (\n    id SERIAL PRIMARY KEY\n);\n")
	if model {
		write("app/__init__.py", "")
		write("app/models.py", `from typing import Optional

from sqlmodel import Field, SQLModel


class Account(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
`)
	}
	if big {
		write("app/big.py", "# "+strings.Repeat("x", 8192)+"\n")
	}
	return root
}

// TestSchemaMigrationOverlapsNeedModelLists checks decision (a) on /sets
// and /table, and decision (b)'s model_list_reasons, on both stores.
func TestSchemaMigrationOverlapsNeedModelLists(t *testing.T) {
	cases := []struct {
		name         string
		model, big   bool
		wantOverlaps int // -1: null
		wantReasons  []string
	}{
		{"pair", true, false, 1, []string{}},
		{"pair-unread", true, true, -1, []string{"python_unread"}},
		{"no-models", false, false, 0, []string{}},
		{"no-models-unread", false, true, -1, []string{"python_unread"}},
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		for _, tc := range cases {
			root := overlapRepo(t, tc.model, tc.big)
			indexWithLimit(t, root, w, tc.name, 4096)
			opt := testOptions(root, s)
			opt.Repository = tc.name
			var sets struct {
				ModelListReasons []string `json:"model_list_reasons"`
				Sets             []struct {
					Source *string `json:"source"`
					Tables []struct {
						Node     contract.Node    `json:"node"`
						Overlaps []map[string]any `json:"overlaps"`
					} `json:"tables"`
				} `json:"sets"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &sets)
			if !reflect.DeepEqual(sets.ModelListReasons, tc.wantReasons) {
				t.Errorf("%s: model_list_reasons %v, want %v", tc.name, sets.ModelListReasons, tc.wantReasons)
			}
			found := false
			for _, g := range sets.Sets {
				if g.Source == nil || *g.Source != sourceMigrations {
					continue
				}
				for _, tb := range g.Tables {
					found = true
					check := func(where string, got []map[string]any) {
						if tc.wantOverlaps < 0 && got != nil || tc.wantOverlaps >= 0 && (got == nil || len(got) != tc.wantOverlaps) {
							t.Errorf("%s: %s overlaps %v, want %d (-1 null)", tc.name, where, got, tc.wantOverlaps)
						}
					}
					check("/sets", tb.Overlaps)
					var detail struct {
						Overlaps []map[string]any `json:"overlaps"`
					}
					decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/table?id="+url.QueryEscape(tb.Node.Identity), nil), &detail)
					check("/table", detail.Overlaps)
				}
			}
			if !found {
				t.Fatalf("%s: no migration table in %+v", tc.name, sets.Sets)
			}
		}
	})
}

// TestModelListsComplete covers the rule's inputs one by one.
func TestModelListsComplete(t *testing.T) {
	both := []string{sourceMigrations, sourceModels}
	unread := []contract.Diagnostic{{Code: "file_too_large", File: "app/big.py"}}
	gap := []contract.Diagnostic{{Code: "symlink", File: "link"}}
	cases := []struct {
		name     string
		read     []string
		diags    []contract.Diagnostic
		unfiled  []contract.Diagnostic
		expected bool
	}{
		{"read", both, nil, nil, true},
		{"models not read", []string{sourceMigrations}, nil, nil, false},
		{"not indexed", nil, nil, nil, false},
		{"python unread", both, unread, nil, false},
		{"scan gap", both, nil, gap, false},
	}
	for _, tc := range cases {
		if got := modelListsComplete(tc.read, nil, tc.diags, tc.unfiled); got != tc.expected {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.expected)
		}
	}
	migration := contract.Node{Identity: "table:m.sql#t", Kind: "table", Attributes: map[string]any{"name": "t"}}
	if overlapsProven(migration, nil, false) || !overlapsProven(migration, nil, true) {
		t.Error("a migration table's overlaps follow modelsComplete only")
	}
}
