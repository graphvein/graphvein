package schema

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

const supabaseFixtureRoot = "../../../testdata/fixtures/supabase-migrations"

// TestSupabaseContract keeps the SB-M rows of the task's claim contract,
// supabaseRows and the code 1:1: the same ids in the same order, each
// row's branch is a function of supabase.go, and the fixture reaches
// every row.
func TestSupabaseContract(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/contract/claims/nextjs-supabase.md")
	if err != nil {
		t.Fatal(err)
	}
	var docIDs, ids []string
	for _, line := range strings.Split(string(doc), "\n") {
		if m := regexp.MustCompile(`^\| (SB-M[0-9]+) \|`).FindStringSubmatch(line); m != nil {
			docIDs = append(docIDs, m[1])
		}
	}
	for _, r := range supabaseRows {
		ids = append(ids, r.id)
	}
	if !slices.Equal(docIDs, ids) {
		t.Fatalf("task table rows %v, supabaseRows %v", docIDs, ids)
	}
	code, err := os.ReadFile("supabase.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range supabaseRows {
		if !regexp.MustCompile(`func (\([a-z]+ \*[a-zA-Z]+\) )?` + r.branch + `\(`).Match(code) {
			t.Errorf("row %s: branch %s is not a function of supabase.go", r.id, r.branch)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	saved := runSet
	runSet = func(set *migrationSet, e *emitter) {
		saved(set, e)
		mu.Lock()
		defer mu.Unlock()
		for r, n := range set.supabaseRows {
			seen[r] += n
		}
	}
	defer func() { runSet = saved }()
	linkRoot(t, supabaseFixtureRoot)
	for _, r := range supabaseRows {
		if seen[r.id] == 0 {
			t.Errorf("the fixture does not reach row %s", r.id)
		}
	}
	for r := range seen {
		if !slices.Contains(ids, r) {
			t.Errorf("the code counts row %s, which the contract does not list", r)
		}
	}
}

// supabaseLinked links an in-memory Supabase project.
func supabaseLinked(t *testing.T, files map[string]string) linked {
	t.Helper()
	out := linked{sources: map[string][]byte{"supabase/config.toml": nil}, nodes: map[string]contract.Node{}}
	for name, sql := range files {
		out.sources["supabase/migrations/"+name] = []byte(sql)
	}
	nodes, edges, diagnostics := Link(Input{Sources: out.sources})
	for _, n := range nodes {
		out.nodes[n.Identity] = n
	}
	out.edges, out.diagnostics = edges, diagnostics
	return out
}

func appliedOf(l linked) map[string]any {
	out := map[string]any{}
	for _, n := range l.nodes {
		if n.Kind == "migration" {
			out[n.Name] = n.Attributes["applied"]
		}
	}
	return out
}

func TestSupabaseDiscovery(t *testing.T) {
	l := linkRoot(t, supabaseFixtureRoot)
	got := map[string]bool{}
	for _, n := range l.nodes {
		if n.Kind == "migration" {
			got[n.File] = true
			// SB-M5: the bare no-config directory is its own package.
			pkg := "app/supabase/config.toml"
			if strings.HasPrefix(n.File, "no-config/") {
				pkg = "no-config/supabase/migrations"
			}
			if strings.HasPrefix(n.File, "rls-data/") {
				pkg = "rls-data/supabase/config.toml"
			}
			if strings.HasPrefix(n.File, "rls-catalog/") {
				pkg = "rls-catalog/supabase/config.toml"
			}
			if strings.HasPrefix(n.File, "rls-rule/") {
				pkg = "rls-rule/supabase/config.toml"
			}
			if strings.HasPrefix(n.File, "rls-dynamic/") {
				pkg = "rls-dynamic/supabase/config.toml"
			}
			if n.Attributes["tool"] != "supabase" || n.Attributes["package"] != pkg || n.Attributes["direction"] != "simple" {
				t.Errorf("%s: %v", n.File, n.Attributes)
			}
		}
		if n.Kind == "table" && (n.Name == "legacy_skipped" || n.Name == "never_applied" || n.Name == "stray") {
			t.Errorf("table %s is not applied by the Supabase CLI", n.Name)
		}
	}
	// Six app migrations (T024c1c added sql_functions, the T024 review
	// archive), four no-config ones (NX-F2 vector_columns), one of
	// rls-data (NX-F2), one of rls-catalog (T024 review) and one of
	// rls-rule (NX-S) and one of rls-dynamic (TOP-1 of 2026-11-18).
	if len(got) != 14 {
		t.Fatalf("migrations %v", got)
	}

	// The legacy init file is skipped only as the first entry, and only
	// when older than 20211209000000; the order is lexical.
	l = supabaseLinked(t, map[string]string{
		"1_a.sql":                  "create table a (id int primary key);",
		"20200101000000_init.sql":  "create table b (id int primary key);",
		"20220101000000_init.sql":  "create table c (id int primary key);",
		"010_late.sql":             "create table d (id int primary key);",
		"20240101000000_x.up.sql":  "create table e (id int primary key);",
		"notes.sql":                "create table f (id int primary key);",
		"20240101000001_x.sql.bak": "create table g (id int primary key);",
	})
	order := map[string]any{}
	for _, n := range l.nodes {
		if n.Kind == "migration" {
			order[n.Name] = n.Attributes["order"]
		}
	}
	want := map[string]any{"010_late.sql": 1, "1_a.sql": 2, "20200101000000_init.sql": 3, "20220101000000_init.sql": 4, "20240101000000_x.up.sql": 5}
	if len(order) != len(want) {
		t.Fatalf("order %v", order)
	}
	for k, v := range want {
		if order[k] != v {
			t.Errorf("order %v, want %v", order, want)
		}
	}
	l = supabaseLinked(t, map[string]string{
		"20200101000000_init.sql": "create table b (id int primary key);",
		"20240101000000_x.sql":    "create table c (id int primary key);",
	})
	if _, ok := appliedOf(l)["20200101000000_init.sql"]; ok {
		t.Error("the legacy first init file is a member")
	}

	// A repeated version: the second file's bookkeeping row is refused,
	// so it and the later files are of unknown applied state, and every
	// table is partial.
	l = supabaseLinked(t, map[string]string{
		"1_a.sql": "create table a (id int primary key);",
		"1_b.sql": "create table b (id int primary key);",
		"2_c.sql": "create table c (id int primary key);",
	})
	applied := appliedOf(l)
	if applied["1_a.sql"] != true || applied["1_b.sql"] != nil || applied["2_c.sql"] != nil {
		t.Errorf("applied %v", applied)
	}
	for _, n := range l.nodes {
		if n.Kind == "table" && (n.Name != "a" || n.Attributes["state"] != "partial") {
			t.Errorf("table %s %v", n.Name, n.Attributes["state"])
		}
	}

	// No config.toml and an entry the CLI does not apply: no set (a
	// directory whose entries all follow the pattern is SB-M5's set,
	// TestSupabaseBareDirectory).
	nodes, _, _ := Link(Input{Sources: map[string][]byte{"supabase/migrations/1_a.sql": []byte("create table a (id int);"), "supabase/migrations/notes.txt": []byte("x")}})
	if len(nodes) != 0 {
		t.Errorf("a migrations directory without config.toml is a set: %v", nodes)
	}
}

func TestSupabaseRowSecurity(t *testing.T) {
	l := supabaseLinked(t, map[string]string{
		"1_a.sql": `create table a (id uuid primary key references auth.users (id), b text references storage.buckets);
alter table a enable row level security;
alter table a force row level security;
create policy p on a using (id = auth.uid());
create policy q on a for insert with check (true);
alter policy p on a rename to r;
alter policy r on a to authenticated;
drop policy q on a;
drop policy if exists missing on a;
create table plain (id int primary key, owner text references auth.users (id));`,
	})
	a := l.node(t, "table", "supabase/migrations/1_a.sql", "a")
	if a.Attributes["state"] != "complete" {
		t.Fatalf("a: %v", a.Attributes["reasons"])
	}
	rls := a.Attributes["rls"].(map[string]any)
	if rls["enabled"] != true || rls["forced"] != true {
		t.Errorf("rls %v", rls)
	}
	policies := a.Attributes["policies"].([]any)
	if len(policies) != 1 || policies[0].(map[string]any)["name"] != "r" || policies[0].(map[string]any)["command"] != "ALL" {
		t.Errorf("policies %v", policies)
	}
	id := l.node(t, "column", "supabase/migrations/1_a.sql", "a.id")
	if ref := id.Attributes["references"].(map[string]any); ref["status"] != "external" || ref["table"] != "auth.users" {
		t.Errorf("references %v", ref)
	}
	b := l.node(t, "column", "supabase/migrations/1_a.sql", "a.b")
	if ref := b.Attributes["references"].(map[string]any); ref["status"] != "external" || ref["table"] != "storage.buckets" {
		t.Errorf("references %v", ref)
	}
	// A key of another type is not a platform key: as today (unresolved).
	owner := l.node(t, "column", "supabase/migrations/1_a.sql", "plain.owner")
	if ref := owner.Attributes["references"].(map[string]any); ref["status"] != "unresolved" {
		t.Errorf("references %v", ref)
	}
	plain := l.node(t, "table", "supabase/migrations/1_a.sql", "plain")
	if rls := plain.Attributes["rls"].(map[string]any); rls["enabled"] != false {
		t.Errorf("plain rls %v", rls)
	}

	// Messages name the Supabase CLI, not sqlx.
	for _, d := range l.diagnostics {
		if strings.Contains(d.Message, "sqlx") && !strings.Contains(d.Message, "_sqlx_migrations") {
			t.Errorf("diagnostic names sqlx: %s", d.Message)
		}
	}

	// sqlx sets keep today's behaviour: no rls attributes, ROW LEVEL
	// SECURITY makes the table partial.
	nodes, _, _ := Link(Input{Sources: map[string][]byte{"Cargo.toml": []byte(pgManifest), "migrations/1_m.sql": []byte("create table s (id int primary key);\nalter table s enable row level security;")}})
	for _, n := range nodes {
		if n.Kind != "table" {
			continue
		}
		if _, ok := n.Attributes["rls"]; ok || n.Attributes["state"] != "partial" {
			t.Errorf("sqlx table %v", n.Attributes)
		}
	}
}

// TestSupabaseRLSUnknownBothWays checks T024 review: an opaque
// statement after a flag was set makes it unknown whatever its value, and
// a policy's table follows a rename of its table.
func TestSupabaseRLSUnknownBothWays(t *testing.T) {
	l := linkRoot(t, supabaseFixtureRoot)
	seen := 0
	for _, n := range l.nodes {
		if n.Kind != "table" {
			continue
		}
		rls, _ := n.Attributes["rls"].(map[string]any)
		switch {
		case strings.HasPrefix(n.File, "no-config/"):
			seen++
			if rls["enabled"] != nil || rls["forced"] != nil {
				t.Errorf("%s: rls %v after a DO block, want both unknown", n.Name, rls)
			}
		case n.Name == "settings":
			// NX-F2 (SB-M9, rls-data): the UPDATE after it is opaque, but
			// its code cannot change row level security.
			seen++
			if rls["enabled"] != true || rls["forced"] != false {
				t.Errorf("settings: rls %v, want enabled and not forced", rls)
			}
		case n.Name == "archive_old":
			seen++
			if rls["enabled"] != true {
				t.Errorf("archive_old: rls %v, want enabled", rls)
			}
			list, _ := n.Attributes["policies"].([]any)
			if len(list) != 1 || list[0].(map[string]any)["table"] != "public.archive_old" {
				t.Errorf("archive_old: policies %v, want one on public.archive_old", list)
			}
		}
		if strings.HasPrefix(n.File, "rls-catalog/") {
			// T024 review: catalog writes after the flags were set.
			seen++
			if rls["enabled"] != nil || rls["forced"] != nil || n.Attributes["policies"] != nil {
				t.Errorf("%s: rls %v, policies %v, want unknown (B5)", n.Name, rls, n.Attributes["policies"])
			}
		}
		if strings.HasPrefix(n.File, "rls-data/") && (rls["enabled"] == nil || rls["forced"] == nil) {
			t.Errorf("%s: rls %v, want both known (SB-M9)", n.Name, rls)
		}
	}
	if seen != 7 {
		t.Errorf("saw %d of the 7 tables", seen)
	}
}
