package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

// Tests of T018f Revision 2 (2026-11-09, after the independent review).

// TestColumnReferences covers A1's rule: every reader of a column's
// references dispatches on the record's source, so a model foreign key the
// linker did not resolve is unresolved, never "no foreign key".
func TestColumnReferences(t *testing.T) {
	model := func(fks any) contract.Node {
		return contract.Node{Attributes: map[string]any{"source": "model", "foreign_keys": fks}}
	}
	fk := func(status string) map[string]any {
		return map[string]any{"table": "t", "column": "id", "status": status}
	}
	for _, tc := range []struct {
		name   string
		col    contract.Node
		linked []bool
	}{
		{"model none proven", model([]any{}), nil},
		{"model unknown", model(nil), nil},
		{"model resolved", model([]any{fk("resolved")}), []bool{true}},
		{"model unresolved", model([]any{fk("unresolved")}), []bool{false}},
		{"model ambiguous", model([]any{fk("ambiguous")}), []bool{false}},
		{"model malformed", model("x"), []bool{false}},
		{"model linked is not a model status", model([]any{fk("linked")}), []bool{false}},
		{"migration linked", contract.Node{Attributes: map[string]any{"references": map[string]any{"status": "linked"}}}, []bool{true}},
		{"migration unresolved", contract.Node{Attributes: map[string]any{"references": map[string]any{"status": "unresolved"}, "additional_references": []any{map[string]any{"status": "linked"}}}}, []bool{false, true}},
		{"migration foreign_keys ignored", contract.Node{Attributes: map[string]any{"foreign_keys": []any{fk("unresolved")}}}, nil},
	} {
		got := columnReferences(tc.col)
		if len(got) != len(tc.linked) {
			t.Errorf("%s: %d entries, want %d", tc.name, len(got), len(tc.linked))
			continue
		}
		for i, r := range got {
			if r.Linked != tc.linked[i] {
				t.Errorf("%s: entry %d linked %v", tc.name, i, r.Linked)
			}
		}
	}
}

// revision2Repo writes a small SQLModel repository: a model table with a
// foreign key to an unclaimed table (A1's repro), and, with big, a .py
// file over the index's file size limit (B1's repro).
func revision2Repo(t *testing.T, big bool) string {
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
	write("app/__init__.py", "")
	write("app/models.py", `from typing import Optional

from sqlmodel import Field, SQLModel


class Account(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    legacy_id: Optional[int] = Field(default=None, foreign_key="legacy_users.id")
`)
	if big {
		write("app/big.py", "# "+strings.Repeat("x", 8192)+"\n")
	}
	return root
}

// indexWithLimit indexes root as repository repo with a file size limit.
func indexWithLimit(t *testing.T, root string, s store.Store, repo string, limit int64) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repo, MaxFileSize: limit, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Stream(ctx, &stream, s); err != nil {
		t.Fatal(err)
	}
}

// TestSchemaRevision2Models checks A1 (an unresolved model foreign key is
// counted and listed as unresolved on /sets and /table), B1 (a model file
// over the size limit makes the group's table list may_be_incomplete with
// python_unread) and C6 (overlaps stay [] with no unread migration tool).
func TestSchemaRevision2Models(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		for _, big := range []bool{false, true} {
			repo := "small"
			if big {
				repo = "big"
			}
			root := revision2Repo(t, big)
			indexWithLimit(t, root, w, repo, 4096)
			opt := testOptions(root, s)
			opt.Repository = repo
			var sets struct {
				Sets []struct {
					Source           *string  `json:"source"`
					TableList        *string  `json:"table_list"`
					TableListReasons []string `json:"table_list_reasons"`
					Tables           []struct {
						Node                 contract.Node    `json:"node"`
						UnresolvedReferences int              `json:"unresolved_references"`
						Overlaps             []map[string]any `json:"overlaps"`
					} `json:"tables"`
				} `json:"sets"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &sets)
			if len(sets.Sets) != 1 || len(sets.Sets[0].Tables) != 1 {
				t.Fatalf("%s: sets %+v", repo, sets.Sets)
			}
			g := sets.Sets[0]
			tb := g.Tables[0]
			if tb.UnresolvedReferences != 1 {
				t.Errorf("%s: unresolved_references %d, want 1", repo, tb.UnresolvedReferences)
			}
			if tb.Overlaps == nil || len(tb.Overlaps) != 0 {
				t.Errorf("%s: overlaps %v, want []", repo, tb.Overlaps)
			}
			wantList, wantReasons := "complete", "[]"
			if big {
				wantList, wantReasons = "may_be_incomplete", `["python_unread"]`
			}
			reasons, _ := json.Marshal(g.TableListReasons)
			if g.TableList == nil || *g.TableList != wantList || string(reasons) != wantReasons {
				t.Errorf("%s: table_list %v %s", repo, g.TableList, reasons)
			}
			var table struct {
				UnresolvedReferences []schemaUnresolved `json:"unresolved_references"`
			}
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/table?id="+url.QueryEscape(tb.Node.Identity), nil), &table)
			if len(table.UnresolvedReferences) != 1 || !strings.HasSuffix(table.UnresolvedReferences[0].Column, ".legacy_id") {
				t.Errorf("%s: /table unresolved_references %+v", repo, table.UnresolvedReferences)
			}
		}
	})
}

// TestSchemaRevision2UnreadToolOverlaps checks C6: with an Alembic script
// directory found but not read, a model table's overlaps are null (not
// proven), on /sets and /table.
func TestSchemaRevision2UnreadToolOverlaps(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := revision2Repo(t, false)
		for name, content := range map[string]string{"alembic.ini": "[alembic]\n", "alembic/env.py": "", "alembic/versions/0001_init.py": ""} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexWithLimit(t, root, w, "alembic", 0)
		opt := testOptions(root, s)
		opt.Repository = "alembic"
		var sets map[string]any
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &sets)
		if tools, _ := sets["unread_tools"].([]any); len(tools) != 1 {
			t.Fatalf("unread_tools %v", sets["unread_tools"])
		}
		group := sets["sets"].([]any)[0].(map[string]any)
		table := group["tables"].([]any)[0].(map[string]any)
		if v, present := table["overlaps"]; !present || v != nil {
			t.Errorf("/sets overlaps %v, want null", v)
		}
		id := table["node"].(map[string]any)["identity"].(string)
		var detail map[string]any
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/table?id="+url.QueryEscape(id), nil), &detail)
		if v, present := detail["overlaps"]; !present || v != nil {
			t.Errorf("/table overlaps %v, want null", v)
		}
	})
}

// TestSchemaRevision2Neighbours checks C4: a model table's neighbours use
// the model completeness rule (Team is complete in the sqlmodel-link
// fixture; the migration rule read its missing state as unknown).
func TestSchemaRevision2Neighbours(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelsRoot(t, w)
		var body schemaTableResponse
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/table?id="+url.QueryEscape("table:app/models.py#Hero"), nil), &body)
		found := false
		for _, n := range append(body.Neighbours.Outgoing, body.Neighbours.Incoming...) {
			if n.Completeness != tableCompleteness(n.Table) {
				t.Errorf("neighbour %s: completeness %s, model rule %s", n.Table.Identity, n.Completeness, tableCompleteness(n.Table))
			}
			if n.Table.Identity == "table:app/models.py#Team" {
				found = n.Completeness == completenessComplete
			}
		}
		if !found {
			t.Errorf("Team is not a complete outgoing neighbour: %+v", body.Neighbours)
		}
		unresolved := map[string]bool{}
		for _, u := range body.UnresolvedReferences {
			unresolved[u.Column] = true
		}
		if !unresolved["column:app/models.py#Hero.rival_id"] {
			t.Errorf("rival_id -> nowhere not unresolved: %+v", body.UnresolvedReferences)
		}
	})
}

// processMark is what no served catalogue string may hold (C2): Markdown
// emphasis or code marks, revision or session references and review
// finding pairs.
var processMark = regexp.MustCompile("[*`]|Rev(ision)? \\d|Session \\d|\\b[a-z]\\d+-s\\d+\\b")

// TestCataloguePlainText checks C2 and C3 over every string the
// /assumptions response serves: plain user text only, and no model row
// names a Go function as its form.
func TestCataloguePlainText(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		var body any
		decodeInto(t, get(t, testOptions(t.TempDir(), s), http.MethodGet, "/api/v0/schema/assumptions", nil), &body)
		n := 0
		var walk func(path string, v any)
		walk = func(path string, v any) {
			switch x := v.(type) {
			case string:
				n++
				if processMark.MatchString(x) {
					t.Errorf("%s: %q", path, x)
				}
			case []any:
				for i, e := range x {
					walk(path+"["+strconv.Itoa(i)+"]", e)
				}
			case map[string]any:
				for k, e := range x {
					walk(path+"."+k, e)
				}
			}
		}
		walk("$", body)
		if n < 500 {
			t.Errorf("walked %d strings", n)
		}
		for key, list := range body.(map[string]any)["rows"].(map[string]any) {
			if !strings.HasPrefix(key, "models/") {
				continue
			}
			for id, r := range list.(map[string]any) {
				if b := r.(map[string]any)["branch"]; b != nil {
					t.Errorf("model row %s %s: branch %v, want null", key, id, b)
				}
			}
		}
	})
}

// TestPlainText covers each process marker form of the contract documents.
func TestPlainText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a `Field(...)` column", "a Field(...) column"},
		{"type unknown; **(Rev 3.3)** form `alias`", "type unknown; form alias"},
		{"may fail (Revision 1.5 U2)", "may fail"},
		{"held; Revision 1.6 V4: ALTER VIEW", "held; ALTER VIEW"},
		{"held; Revision 1.6 V4 (kinds, 2026-11-07): DROP INDEX", "held; DROP INDEX"},
		{"x (Revision 1.9 Y51; typecmds.c 4493)", "x (typecmds.c 4493)"},
		{"x (the last two: Revision 1.4); y", "x (the last two); y"},
		{"follows P1 (B2, g5-s80); z", "follows P1; z"},
		{"a; Revision 1.8 X1 as replaced by Revision 1.9 Y39: and any", "a; and any"},
	} {
		if got := plainText(tc.in); got != tc.want {
			t.Errorf("plainText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"live; Y45: a DROP", "live; a DROP"},
		{"arguments (W2), and", "arguments, and"},
		{"superuser); E6: OWNER TO", "superuser); E6: OWNER TO"},
	} {
		if got := migrationText(tc.in); got != tc.want {
			t.Errorf("migrationText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
