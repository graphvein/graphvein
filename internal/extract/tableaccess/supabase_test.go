package tableaccess

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const t024Path = "../../../docs/contract/claims/nextjs-supabase.md"

// supabaseTableRows returns the Supabase table rows of a Markdown file:
// the lines from `| SB-K1 |` to the end of that table, in order.
func supabaseTableRows(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| SB-K1 |") {
			in = true
		}
		if in {
			if !strings.HasPrefix(line, "| ") {
				break
			}
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("%s: no Supabase table", path)
	}
	return rows
}

var supabaseAssumptionID = regexp.MustCompile(`SB-V[0-9]+`)

// TestSupabaseContract is the table half of the Supabase contract test:
// the task's table (rows SB-K1..SB-T5 and its D) equals the docs copy
// line for line; SupabaseContract lists the same rows in the same order
// with the same assumptions; every listed form is named in its row; every
// branch is a function of supabase.go; and the SB-V1 text is the docs'.
func TestSupabaseContract(t *testing.T) {
	task := supabaseTableRows(t, t024Path)
	docs := supabaseTableRows(t, docsPath)
	if !reflect.DeepEqual(task, docs) {
		t.Errorf("the docs' Supabase rows differ from the task's:\ntask: %q\ndocs: %q", task, docs)
	}
	if len(task) != len(SupabaseContract) {
		t.Fatalf("task has %d rows, SupabaseContract %d", len(task), len(SupabaseContract))
	}
	code, err := os.ReadFile("supabase.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range SupabaseContract {
		line := task[i]
		cells := strings.Split(strings.ReplaceAll(line, `\|`, "\x00"), "|")
		if len(cells) != 7 {
			t.Errorf("row %d: %d cells, want 5", i, len(cells)-2)
			continue
		}
		if id := strings.TrimSpace(cells[1]); id != r.ID {
			t.Errorf("row %d is %s in the task, %s in the code", i, id, r.ID)
		}
		var got []string
		if strings.TrimSpace(cells[5]) != "-" {
			got = supabaseAssumptionID.FindAllString(cells[5], -1)
		}
		if !reflect.DeepEqual(got, r.Assumptions) && !(len(got) == 0 && len(r.Assumptions) == 0) {
			t.Errorf("row %s: task assumptions %v, code %v", r.ID, got, r.Assumptions)
		}
		for _, f := range r.Forms {
			if !strings.Contains(line, f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if !regexp.MustCompile(`\nfunc (\([^)]*\) )?` + r.Branch + `\(`).Match(code) {
			t.Errorf("row %s: branch %s is not a function of supabase.go", r.ID, r.Branch)
		}
		if r.Side == SideClaim && r.Tool != SupabaseTool {
			t.Errorf("row %s: claim row with tool %q", r.ID, r.Tool)
		}
	}
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	for _, a := range SupabaseAssumptionTexts {
		if !strings.Contains(text, "- **"+a.ID+"** "+a.Text) {
			t.Errorf("%s: %q is not the docs' entry", a.ID, a.Text)
		}
	}
	if len(SupabaseAssumptionTexts) != len(SupabaseAssumptions) {
		t.Errorf("%d texts for %d assumptions", len(SupabaseAssumptionTexts), len(SupabaseAssumptions))
	}
}

// TestSupabaseTags is the tag half: the link pass over the Supabase
// fixture reaches every row of SupabaseContract and each of its forms, and
// tags nothing outside it; each oracle-labelled row reaches all its forms
// there (the fixture is the postgrest-js oracle's).
func TestSupabaseTags(t *testing.T) {
	tags := modeltest.NewTags()
	in := loadRoot(t, supabaseFixtureRoot)
	in.Tag = tags.Tag
	Link(in)
	branches := map[string]string{}
	forms := map[string][]string{}
	for _, r := range SupabaseContract {
		branches[r.ID] = r.Branch
		if len(r.Forms) > 0 {
			forms[r.ID] = r.Forms
		}
	}
	modeltest.CheckTags(t, tags, branches, forms)
}

// TestSupabaseFixtureClaims pins the fixture's claims and unknowns that
// carry the rows' decisions (the golden holds all of them).
func TestSupabaseFixtureClaims(t *testing.T) {
	a := analyze(loadRoot(t, supabaseFixtureRoot))
	claims := map[string][]string{}
	for _, s := range a.sites {
		if s.tool != SupabaseTool {
			continue
		}
		key := s.file + ":" + itoa(s.span.StartLine) + ":" + s.row
		for id, ops := range s.reads {
			for op := range ops {
				claims[key] = append(claims[key], "read "+tableName(id)+" "+op)
			}
		}
		for id, ops := range s.writes {
			for op := range ops {
				claims[key] = append(claims[key], "write "+tableName(id)+" "+op)
			}
		}
		if s.unknown != nil {
			claims[key] = append(claims[key], "unknown "+reasonCode(s.unknown.Reason))
		}
	}
	want := map[string][]string{
		"src/db/writes.ts:8:SB-T1":   {"write profiles insert"},
		"src/db/embeds.ts:8:SB-T3":   {"read prices select", "read products select"},
		"src/db/embeds.ts:12:SB-T3":  {"read products select"},
		"src/db/embeds.ts:23:SB-T3":  {"unknown embedded_resource"},
		"src/db/embeds.ts:27:SB-T3":  {"unknown embedded_resource"},
		"src/db/embeds.ts:53:SB-T3":  {"read prices select", "read products select"},
		"src/db/embeds.ts:57:SB-T3":  {"unknown embedded_resource"},
		"src/db/reads.ts:42:SB-T1":   {"read invoices select"},
		"src/db/reads.ts:50:SB-T1":   {"unknown sql_name_unresolved"},
		"other/src/notes.ts:7:SB-T1": {"unknown no_schema_source"},
	}
	for key, w := range want {
		got := claims[key]
		if strings.Join(sortStrings(got), ";") != strings.Join(sortStrings(w), ";") {
			t.Errorf("%s: %v, want %v", key, got, w)
		}
	}
	if _, ok := claims["src/db/embeds.ts:39:SB-T3"]; ok {
		t.Errorf("aggregates count() and amount.sum() are no embeds")
	}
	for owner, list := range a.unknowns {
		for _, u := range list {
			if strings.HasPrefix(owner, "function:plain/") {
				t.Errorf("%s: %v in a package that declares no Supabase package", owner, u)
			}
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func tableName(identity string) string {
	return identity[strings.LastIndex(identity, ".")+1:]
}

func sortStrings(list []string) []string {
	out := append([]string(nil), list...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestParseSelect checks the select string reader.
func TestParseSelect(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"*", "*", true},
		{"*, prices(*, products(*))", "*,[prices:*,[products:*]]", true},
		{"id, item:products ( name )", "id,[products(item):name]", true},
		{"id, sent:messages!sender_id(*)", "id,[messages(sent)!:*]", true},
		{"id, ...prices(amount)", "id,[...prices:amount]", true},
		{"count(), amount.sum()", "count,amount.sum", true},
		{"a::text, b:c::int", "a::text,b:c::int", true},
		{`"odd name", "Emb"(*)`, `"odd name",[Emb:*]`, true},
		{"id, x(", "", false},
		{"id)", "", false},
	}
	for _, c := range cases {
		items, ok := parseSelect(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && renderItems(items) != c.want {
			t.Errorf("%q: %s, want %s", c.in, renderItems(items), c.want)
		}
	}
}

func renderItems(items []selectItem) string {
	var parts []string
	for _, it := range items {
		if !it.embed {
			parts = append(parts, it.name)
			continue
		}
		s := "["
		if it.spread {
			s += "..."
		}
		s += it.name
		if it.alias != "" {
			s += "(" + it.alias + ")"
		}
		if it.hint {
			s += "!"
		}
		parts = append(parts, s+":"+renderItems(it.children)+"]")
	}
	return strings.Join(parts, ",")
}

// TestSupabaseChainsOracle is the postgrest-js fixture oracle of rows SB-T1
// and SB-T2 (never run by CI): every claimed fixture chain is evaluated by
// @supabase/postgrest-js's query builder at the corpus pin (1.18.1,
// GRAPHVEIN_POSTGREST_ORACLE_DIR names the extracted npm package), which
// builds method, URL and schema without a request
// (scripts/oracle/nextjs_supabase/postgrest_chains.cjs). The claimed
// operation must match the method (select GET or HEAD, insert POST,
// upsert POST with merge-duplicates, update PATCH, delete DELETE), the
// claimed table's name the URL's last segment and its schema the
// builder's. Each checkout of GRAPHVEIN_POSTGREST_ORACLE_CORPORA (OS path
// list) is compared the same way (T024 review), so the corpus chains
// the label oracle covers are compared chains. A chain that starts with a
// const builder binding (T024 review) is evaluated from the binding's
// value (T024 review).
func TestSupabaseChainsOracle(t *testing.T) {
	pkg := os.Getenv("GRAPHVEIN_POSTGREST_ORACLE_DIR")
	if pkg == "" {
		t.Skip("set GRAPHVEIN_POSTGREST_ORACLE_DIR to an extracted @supabase/postgrest-js package")
	}
	roots := []string{supabaseFixtureRoot}
	if list := os.Getenv("GRAPHVEIN_POSTGREST_ORACLE_CORPORA"); list != "" {
		roots = append(roots, filepath.SplitList(list)...)
	}
	for _, root := range roots {
		t.Run(filepath.Base(root), func(t *testing.T) { supabaseChainsOracle(t, pkg, root) })
	}
}

// supabaseChainsOracle compares every claimed SB-T1 and SB-T2 chain of one
// checkout with postgrest-js.
func supabaseChainsOracle(t *testing.T, pkg, root string) {
	a := analyze(loadRoot(t, root))
	type chain struct {
		ID       string            `json:"id"`
		Receiver string            `json:"receiver"`
		Chain    string            `json:"chain"`
		Schema   string            `json:"schema"`
		Consts   map[string]string `json:"consts,omitempty"`
		Bound    *constBound       `json:"bound,omitempty"`
	}
	type result struct {
		ID, Method, Path, Schema, Prefer, Error string
	}
	var chains []chain
	claimed := map[string]site{}
	for _, s := range a.sites {
		if s.row != "SB-T1" && s.row != "SB-T2" || len(s.reads)+len(s.writes) == 0 {
			continue
		}
		id := s.file + ":" + itoa(s.span.StartLine)
		chains = append(chains, chain{ID: id, Receiver: s.receiver, Chain: s.builder, Schema: s.clientSchema, Consts: s.consts, Bound: s.bound})
		claimed[id] = s
	}
	input := filepath.Join(t.TempDir(), "chains.json")
	data, _ := json.Marshal(chains)
	if err := os.WriteFile(input, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", "../../../scripts/oracle/nextjs_supabase/postgrest_chains.cjs", pkg, input).Output()
	if err != nil {
		t.Fatalf("postgrest_chains.cjs: %v", err)
	}
	var results []result
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	methods := map[string]string{"select": "GET", "insert": "POST", "upsert": "POST", "update": "PATCH", "delete": "DELETE"}
	compared := 0
	for _, r := range results {
		s := claimed[r.ID]
		if r.Error != "" {
			t.Errorf("%s: %s", r.ID, r.Error)
			continue
		}
		ops := s.reads
		if len(s.writes) > 0 {
			ops = s.writes
		}
		for id, set := range ops {
			for op := range set {
				method := methods[op]
				if op == "select" && r.Method == "HEAD" {
					method = "HEAD"
				}
				merge := strings.Contains(r.Prefer, "resolution=merge-duplicates")
				if r.Method != method || (op == "upsert") != merge {
					t.Errorf("%s: claimed %s, postgrest-js %s (Prefer %q)", r.ID, op, r.Method, r.Prefer)
				}
			}
			qualified := s.tables[id]
			if want := "/rest/v1/" + qualified[strings.Index(qualified, ".")+1:]; r.Path != want {
				t.Errorf("%s: claimed %s, postgrest-js path %s", r.ID, qualified, r.Path)
			}
			if schema := qualified[:strings.Index(qualified, ".")]; r.Schema != schema {
				t.Errorf("%s: claimed schema %s, postgrest-js %s", r.ID, schema, r.Schema)
			}
		}
		compared++
	}
	if compared != len(chains) {
		t.Errorf("compared %d of %d chains", compared, len(chains))
	}
	t.Logf("%s: %d claimed chains compared with postgrest-js", root, compared)
}
