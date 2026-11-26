package schema

// Supabase oracle (T024c1, rows SB-M1 to SB-M3): each Supabase set is
// replayed on PostgreSQL 17 on top of the Supabase prelude
// (scripts/oracle/nextjs_supabase/supabase-prelude.sql, the platform
// objects of assumption SB-V2) with the T018a2 harness, and the pass's
// records are compared with the catalog: the harness's comparison of
// tables, columns, keys, foreign keys and indexes (compare), plus the
// table names, the row level security flags (pg_class.relrowsecurity,
// relforcerowsecurity), the policies (pg_policy name and command) and the
// foreign keys to platform tables. Any disagreement fails the test.
//
// It runs where GRAPHVEIN_TEST_POSTGRES_URL is set, on the fixture
// testdata/fixtures/supabase-migrations/app and rls-data and on the checkouts listed in
// GRAPHVEIN_SUPABASE_CORPORA (a path list, e.g. the chatbot-ui and
// supabase-stripe pins; a checkout without supabase/config.toml is read
// as if it had one, so its migrations are still checked).
//
// Shims, as the investigation's replay-supabase.sh (the scratch cluster has neither
// pgvector nor the http extension): CREATE EXTENSION vector or http is
// dropped, vector(N) becomes real[], and hnsw and ivfflat indexes are
// dropped. Both the pass and the server read the shimmed text.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

const supabaseFixture = "../../../testdata/fixtures/supabase-migrations/app"

// supabaseRLSFixture is the NX-F2 set of SB-M9 (a data statement in a set
// that defines code): its own project, so app's tables stay compared.
const supabaseRLSFixture = "../../../testdata/fixtures/supabase-migrations/rls-data"

var (
	shimExtension = regexp.MustCompile(`(?i)create extension (if not exists )?(vector|http)[^;]*;`)
	shimVector    = regexp.MustCompile(`(?i)vector\(([0-9]+)\)`)
	shimIndex     = regexp.MustCompile(`(?is)create index[^;]*using\s+(hnsw|ivfflat)[^;]*;`)
)

func supabaseShim(sql string) string {
	sql = shimExtension.ReplaceAllString(sql, "-- shim: extension dropped")
	sql = shimVector.ReplaceAllString(sql, "real[]")
	return shimIndex.ReplaceAllString(sql, "-- shim: index dropped")
}

// supabasePrelude splits the prelude file into statements.
func supabasePrelude(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../../scripts/oracle/nextjs_supabase/supabase-prelude.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements, failure := lexStatements(data)
	if failure != nil {
		t.Fatalf("prelude: %s", failure.reason)
	}
	var out []string
	for _, st := range statements {
		out = append(out, string(data[st.start:st.end]))
	}
	// The platform's search_path (replay-supabase.sh sets it on the
	// database): extension functions such as uuid_generate_v4() resolve
	// unqualified.
	return append(out, `SET search_path = "$user", public, extensions`)
}

// supabaseHistory reads a project's supabase/migrations as the CLI lists
// them (written here from the CLI's rule, independently of
// supabaseDiscover) and returns the oracle history and the pass's sources.
func supabaseHistory(t *testing.T, root string, prelude []string) (*oracleHistory, map[string][]byte) {
	t.Helper()
	dir := filepath.Join(root, "supabase", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := &oracleHistory{ID: "supabase:" + filepath.Base(root), Prelude: prelude, FreshDB: true}
	sources := map[string][]byte{"supabase/config.toml": []byte("")}
	pattern := regexp.MustCompile(`^([0-9]+)_(.*)\.sql$`)
	for i, e := range entries {
		if i == 0 && supabaseLegacyInit(e.Name()) || !pattern.MatchString(e.Name()) || !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := supabaseShim(strings.ReplaceAll(string(data), "\r\n", "\n"))
		h.Files = append(h.Files, oracleFile{Name: e.Name(), SQL: text})
		sources["supabase/migrations/"+e.Name()] = []byte(text)
	}
	// The harness applies sqlx's numeric order; the CLI's is lexical. The
	// histories checked here have one order.
	order, ok := sqlxOrder(h.Files, false)
	if !ok || len(order) != len(h.Files) {
		t.Fatalf("%s: the harness cannot order the set", h.ID)
	}
	for i, m := range order {
		if m.file.Name != h.Files[i].Name {
			t.Fatalf("%s: numeric and lexical order differ at %s", h.ID, m.file.Name)
		}
	}
	return h, sources
}

// catRowSecurity is the catalog's row level security of one table.
type catRowSecurity struct {
	Enabled  bool        `json:"enabled"`
	Forced   bool        `json:"forced"`
	Policies []catPolicy `json:"policies"`
}

type catPolicy struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// readRowSecurity reads relrowsecurity, relforcerowsecurity and pg_policy
// for every table outside the system schemas.
func readRowSecurity(ctx context.Context, conn *sql.Conn, v *verdict) error {
	v.RowSecurity = map[string]catRowSecurity{}
	rows, err := conn.QueryContext(ctx, `SELECT n.nspname || '.' || c.relname, c.relrowsecurity, c.relforcerowsecurity FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_toast%'`)
	if err != nil {
		return fmt.Errorf("reading row security: %w", err)
	}
	for rows.Next() {
		var key string
		var rs catRowSecurity
		if err := rows.Scan(&key, &rs.Enabled, &rs.Forced); err != nil {
			rows.Close()
			return err
		}
		rs.Policies = []catPolicy{}
		v.RowSecurity[key] = rs
	}
	rows.Close()
	// pg_policy.polcmd: r SELECT, a INSERT, w UPDATE, d DELETE, * ALL.
	rows, err = conn.QueryContext(ctx, `SELECT n.nspname || '.' || c.relname, p.polname, CASE p.polcmd WHEN 'r' THEN 'SELECT' WHEN 'a' THEN 'INSERT' WHEN 'w' THEN 'UPDATE' WHEN 'd' THEN 'DELETE' ELSE 'ALL' END
		FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid = p.polrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace ORDER BY 1, p.oid`)
	if err != nil {
		return fmt.Errorf("reading pg_policy: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var p catPolicy
		if err := rows.Scan(&key, &p.Name, &p.Command); err != nil {
			return err
		}
		rs := v.RowSecurity[key]
		rs.Policies = append(rs.Policies, p)
		v.RowSecurity[key] = rs
	}
	return rows.Err()
}

// supabaseCompare checks the SB-M records against the catalog: the set's
// tables are the catalog's tables outside the prelude, each known rls
// flag is the catalog's, the policies of a complete table are the
// catalog's (in creation order) and those of a partial table are among
// them, and every external reference is a catalog foreign key to that
// platform table's id. It returns the disagreements and what it compared.
func supabaseCompare(nodes []contract.Node, v *verdict) (out []string, compared map[string]int) {
	compared = map[string]int{}
	prelude := map[string]bool{}
	for _, p := range v.Prelude {
		prelude[p] = true
	}
	want := map[string]bool{}
	for _, t := range v.Tables {
		if !prelude[t.Key] {
			want[t.Key] = true
		}
	}
	catalogFKs := map[string][]catFK{}
	for _, t := range v.Tables {
		catalogFKs[t.Key] = t.FKs
	}
	got := map[string]bool{}
	tableKey := map[string]string{} // by qualified name
	for _, n := range nodes {
		if n.Kind != "table" {
			continue
		}
		schema := "public"
		if s, ok := n.Attributes["schema"].(string); ok {
			schema = s
		}
		key := schema + "." + n.Name
		got[key] = true
		tableKey[n.QualifiedName] = key
		rs, ok := v.RowSecurity[key]
		if !ok {
			continue
		}
		if rls, ok := n.Attributes["rls"].(map[string]any); ok {
			for name, catalog := range map[string]bool{"enabled": rs.Enabled, "forced": rs.Forced} {
				if b, ok := rls[name].(bool); ok {
					compared["rls"]++
					if b != catalog {
						out = append(out, fmt.Sprintf("%s: rls %s %v, catalog %v", key, name, b, catalog))
					}
				}
			}
		}
		list, ok := n.Attributes["policies"].([]any)
		if !ok {
			continue
		}
		var records, catalog []string
		for _, p := range list {
			m := p.(map[string]any)
			records = append(records, fmt.Sprint(m["name"], " ", m["command"]))
			// The policy's table is the catalog's (pg_policies follows a
			// table rename; T024 review).
			table, _ := m["table"].(string)
			if !strings.Contains(table, ".") {
				table = "public." + table
			}
			compared["policy_tables"]++
			if table != key {
				out = append(out, fmt.Sprintf("%s: policy %v names table %s", key, m["name"], table))
			}
		}
		for _, p := range rs.Policies {
			catalog = append(catalog, p.Name+" "+p.Command)
		}
		compared["policies"] += len(records)
		if n.Attributes["state"] == "complete" {
			if !slices.Equal(records, catalog) {
				out = append(out, fmt.Sprintf("%s: policies %v, catalog %v", key, records, catalog))
			}
			continue
		}
		for _, r := range records {
			if !slices.Contains(catalog, r) {
				out = append(out, fmt.Sprintf("%s: policy %s is not in the catalog %v", key, r, catalog))
			}
		}
	}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		for _, k := range []string{"references", "additional_references"} {
			var refs []any
			switch r := n.Attributes[k].(type) {
			case map[string]any:
				refs = []any{r}
			case []any:
				refs = r
			}
			for _, r := range refs {
				m := r.(map[string]any)
				if m["status"] != "external" {
					continue
				}
				compared["external"]++
				owner := tableKey[strings.TrimSuffix(n.QualifiedName, "."+qnPart(n.Name))]
				local := fmt.Sprint(m["local_columns"])
				found := slices.ContainsFunc(catalogFKs[owner], func(fk catFK) bool {
					return fmt.Sprint(fk.Columns) == local && fk.RefTable == m["table"] && slices.Equal(fk.RefColumns, []string{"id"})
				})
				if !found {
					out = append(out, fmt.Sprintf("%s: external reference %v -> %v(id) is not a catalog foreign key %v", owner, local, m["table"], catalogFKs[owner]))
				}
			}
		}
	}
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	for k := range got {
		if !want[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if want[k] != got[k] {
			out = append(out, fmt.Sprintf("table %s: records %v, catalog %v", k, got[k], want[k]))
		}
	}
	compared["tables"] = len(got)
	return out, compared
}

func TestSupabaseOracle(t *testing.T) {
	o := sharedOracle(t)
	ctx := context.Background()
	roots := []string{supabaseFixture, supabaseRLSFixture}
	if env := os.Getenv("GRAPHVEIN_SUPABASE_CORPORA"); env != "" {
		roots = append(roots, filepath.SplitList(env)...)
	}
	prelude := supabasePrelude(t)
	saved := catalogHook
	catalogHook = readRowSecurity
	defer func() { catalogHook = saved }()
	for _, root := range roots {
		h, sources := supabaseHistory(t, root, prelude)
		t.Run(path.Base(filepath.ToSlash(root)), func(t *testing.T) {
			nodes, edges, diagnostics := Link(Input{Sources: sources})
			x := readRecords(nodes, edges)
			x.diagnostics = diagnostics
			v, err := o.run(ctx, h)
			takeNotes(t, o, h.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, oc := range v.Outcomes {
				if oc.Status != "accepted" {
					t.Errorf("%s: %s %s %s", oc.File, oc.Status, oc.SQLState, oc.Message)
				}
			}
			j := judgeWith(h, x, v)
			for _, d := range j.all {
				t.Errorf("disagreement: %s %s: %s", d.Kind, d.Object, d.Detail)
			}
			extra, compared := supabaseCompare(nodes, v)
			for _, d := range extra {
				t.Errorf("disagreement: %s", d)
			}
			t.Logf("%s: %d migrations, tables %d (%d complete), compared facts %d, SB-M compared %v", h.ID, j.counts.Migrations, j.counts.Tables, j.counts.TablesComplete, j.counts.ComparedFacts, compared)
		})
	}
}
