package supabasetypes

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/extract/schema"
)

const fixtureRoot = "../../../../testdata/fixtures/supabase-types"

// loadFixture reads the fixture's sources and links its migration sets as
// the indexer does.
func loadFixture(t *testing.T) Input {
	t.Helper()
	sources := map[string][]byte{}
	err := filepath.WalkDir(fixtureRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(fixtureRoot, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges, _ := schema.Link(schema.Input{Sources: sources, Root: fixtureRoot})
	return Input{Sources: sources, Migrations: nodes, Edges: edges}
}

// TestSupabaseTypesTags is the tag half of the contract test: the fixture
// reaches every row with a branch and every listed form, and nothing else.
func TestSupabaseTypesTags(t *testing.T) {
	tags := modeltest.NewTags()
	claimTag = tags.Tag
	t.Cleanup(func() { claimTag = nil })
	Link(loadFixture(t))
	branches := map[string]string{}
	forms := map[string][]string{}
	for _, r := range Contract {
		branches[r.ID] = r.Branch
		if len(r.Forms) > 0 {
			forms[r.ID] = r.Forms
		}
	}
	modeltest.CheckTags(t, tags, branches, forms)
}

// TestSupabaseTypesFixture checks the fixture's records and diagnostics:
// the tables of app/ and legacy/, none of migrated/ (SB-G2) with its
// disagreements, handwritten/ unread and kysely/ silent.
func TestSupabaseTypesFixture(t *testing.T) {
	nodes, _, diags := Link(loadFixture(t))
	var tables []string
	byID := map[string]contract.Node{}
	for _, n := range nodes {
		byID[n.Identity] = n
		if n.Kind == "table" {
			tables = append(tables, n.Identity)
		}
	}
	want := []string{"table:app/lib/database.types.ts#public.lists", "table:app/lib/database.types.ts#public.todos", "table:legacy/src/types.ts#public.customers"}
	if strings.Join(tables, " ") != strings.Join(want, " ") {
		t.Errorf("tables %v, want %v", tables, want)
	}
	lists := byID["table:app/lib/database.types.ts#public.lists"]
	if cf, _ := lists.Attributes["computed_fields"].([]string); len(cf) != 1 || cf[0] != "todo_count" {
		t.Errorf("computed fields %v", lists.Attributes["computed_fields"])
	}
	if lists.Attributes["set"] != "models/supabase_types/app/lib/database.types.ts" || lists.Attributes["label"] != Label || lists.Attributes["package"] != "app" {
		t.Errorf("lists attributes %v", lists.Attributes)
	}
	checks := map[string]any{
		"column:app/lib/database.types.ts#public.todos.list_id":          true,
		"column:app/lib/database.types.ts#public.todos.meta":             false,
		"column:app/lib/database.types.ts#public.todos.window":           nil,
		"column:app/lib/database.types.ts#public.lists.id":               false,
		"column:legacy/src/types.ts#public.customers.stripe_customer_id": true,
	}
	for id, nullable := range checks {
		n, ok := byID[id]
		if !ok {
			t.Errorf("%s missing", id)
			continue
		}
		if n.Attributes["nullable"] != nullable {
			t.Errorf("%s nullable %v, want %v", id, n.Attributes["nullable"], nullable)
		}
	}
	if _, ok := byID["column:app/lib/database.types.ts#public.lists.todo_count"]; ok {
		t.Error("a computed field is a column")
	}
	if v := byID["column:app/lib/database.types.ts#public.todos.priority"].Attributes["tool_type"]; v != `Database["public"]["Enums"]["priority"] | null` {
		t.Errorf("priority tool_type %v", v)
	}
	modeltest.CheckClaims(t, nodes, []string{"name", "nullable"})
	codes := map[string][]string{}
	for _, d := range diags {
		codes[d.Code] = append(codes[d.Code], d.File+": "+d.Message)
	}
	if len(codes[codeUnread]) != 1 || !strings.HasPrefix(codes[codeUnread][0], "handwritten/database.ts") {
		t.Errorf("unread %v", codes[codeUnread])
	}
	if len(codes[codeCompared]) != 1 || !strings.Contains(codes[codeCompared][0], "2 tables in both, 1 agree") || !strings.Contains(codes[codeCompared][0], "platform schemas not compared: [storage]") {
		t.Errorf("compared %v", codes[codeCompared])
	}
	got := codes[codeDisagreement]
	wantDis := []string{"column public.members.email: nullable false", "column public.members.nickname is in the generated types", "column public.members.role of the migration set", "relationship members_id_fkey", "table public.audit of the migration set", "table public.invites is in the generated types"}
	if len(got) != len(wantDis) {
		t.Fatalf("disagreements %q", got)
	}
	for _, w := range wantDis {
		found := false
		for _, g := range got {
			found = found || strings.Contains(g, w)
		}
		if !found {
			t.Errorf("no disagreement %q in %q", w, got)
		}
	}
	for _, d := range diags {
		if d.File == "kysely/db.ts" {
			t.Errorf("kysely: %v", d)
		}
	}
}
