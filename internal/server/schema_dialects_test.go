package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// dialectsFile is the one source of the synthetic dialects repository.
const dialectsFile = "app/models.go"

var dialectsSource = []byte("type Order struct {\n\tID uint\n\tAmount decimal.Decimal\n\tCreated time.Time\n\tNote string\n}\n")

// dialectsSnapshot builds, through the model kit itself (no front end
// writes ADR 0065's fields yet), a model group whose dialect is chosen at
// run time: one table with a per-dialect type, autoincrement and server
// default (oracle on postgres and sqlite, mapped on mysql), a per-dialect
// type family with its precision unknown beside a known sqlite type, a
// tool-side type and a type every dialect agrees on (text; mapped on mysql
// only). A group with run-time dialects states every dialect-dependent fact
// per dialect (T018g0b Revision 1, A3). It returns the snapshot as NDJSON
// for repository "dialects".
func dialectsSnapshot(t *testing.T) []byte {
	t.Helper()
	oracle := func(row string) models.ClaimSource {
		return models.ClaimSource{Row: row, Verification: models.VerificationOracle}
	}
	mapped := func(row string) models.ClaimSource { return models.ClaimSource{Row: row} }
	base := func(name, attr string, line int) *models.Column {
		return &models.Column{Name: name, Attribute: attr, File: dialectsFile, Line: line,
			Attributes: map[string]any{"nullable": false, "primary_key": false, "default": "absent"},
			Claims:     map[string]models.ClaimSource{"name": mapped("SY-B1"), "nullable": mapped("SY-B1"), "primary_key": mapped("SY-B1"), "default": mapped("SY-B1"), "foreign_keys": mapped("SY-B1")}}
	}
	id := base("id", "ID", 2)
	id.Attributes["primary_key"] = true
	id.SetToolType("uint")
	for _, d := range []string{"postgres", "sqlite"} {
		id.SetDialectFact(d, "autoincrement", true, oracle("SY-T2"))
	}
	id.SetDialectFact("mysql", "autoincrement", true, mapped("SY-T2"))
	id.SetDialectFact("postgres", "type", "bigserial", oracle("SY-T1"))
	id.SetDialectFact("sqlite", "type", "integer", oracle("SY-T1"))
	id.SetDialectFact("mysql", "type", "bigint unsigned", mapped("SY-T1"))

	amount := base("amount", "Amount", 3)
	amount.SetToolType("decimal.Decimal")
	amount.SetDialectTypeFamily("postgres", "numeric", []string{"precision", "scale"}, oracle("SY-T1"))
	amount.SetDialectFact("mysql", "type", "decimal", mapped("SY-T1"))
	amount.SetDialectFact("mysql", "precision", 10, mapped("SY-T1"))
	amount.SetDialectFact("mysql", "scale", 0, mapped("SY-T1"))
	amount.SetDialectFact("postgres", "server_default", true, oracle("SY-T3"))
	amount.SetDialectFact("postgres", "server_default_text", "0", models.ClaimSource{})
	amount.SetDialectFact("mysql", "server_default", true, mapped("SY-T3"))
	amount.SetDialectFact("mysql", "server_default_text", "'0'", models.ClaimSource{})
	amount.SetDialectFact("sqlite", "server_default", false, oracle("SY-T3"))

	created := base("created", "Created", 4)
	created.SetDialectTypeFamily("postgres", "timestamp with time zone", []string{"precision"}, oracle("SY-T1"))
	created.SetDialectTypeFamily("mysql", "datetime", []string{"precision"}, mapped("SY-T1"))
	created.SetDialectFact("sqlite", "type", "datetime", oracle("SY-T1"))
	created.SetDialectFact("postgres", "timezone", true, oracle("SY-T1"))
	created.SetDialectFact("mysql", "timezone", false, mapped("SY-T1"))

	note := base("note", "Note", 5)
	note.SetDialectFact("mysql", "type", "text", mapped("SY-T1"))
	note.SetDialectFact("postgres", "type", "text", oracle("SY-T1"))
	note.SetDialectFact("sqlite", "type", "text", oracle("SY-T1"))

	table := &models.Table{Identity: "table:" + dialectsFile + "#Order", File: dialectsFile, Name: "orders", Model: "Order", ModelLine: 1,
		Span: contract.Span{StartLine: 1, EndLine: 6}, SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true,
		Columns: []*models.Column{id, amount, created, note}}
	group := models.Group{Tool: "synthetic", Root: "app", Pins: map[string]any{"synthetic": "1.0"}, Assumptions: []string{}, Dialects: []string{"postgres", "mysql", "sqlite"}, Verification: models.VerificationMapped}
	steps := &models.Steps{}
	nodes, edges := models.Link([]*models.Table{table}, models.LinkInput{Group: group, Rows: models.Rows{Emit: "SY-L3"}, Steps: steps, Sources: map[string][]byte{dialectsFile: dialectsSource}})
	if len(steps.Diagnostics) != 0 {
		t.Fatalf("diagnostics %v", steps.Diagnostics)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	write := func(v any) {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	write(contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "crafted", Version: "t018g0b"}, Repository: "dialects", GeneratedAt: "2026-11-15T00:00:00Z"})
	write(contract.File{Type: "file", Path: dialectsFile, Language: "go", Precision: "precise", Size: int64(len(dialectsSource)), ContentHash: contract.ContentHash(dialectsSource)})
	for _, n := range nodes {
		write(n)
	}
	for _, e := range edges {
		write(e)
	}
	write(contract.Footer{Type: "footer", Counts: contract.Counts{Records: 1 + len(nodes) + len(edges), File: 1, Node: len(nodes), Edge: len(edges)}})
	return buf.Bytes()
}

// TestSchemaDialectFacts checks how the schema API serves ADR 0065's
// records on both backends: /sets lists the group's run-time dialects and
// each column's by_dialect (without claims), /table serves the claims
// with their dialect and a link under the request's editor, and the
// verification counts count every per-dialect claim.
func TestSchemaDialectFacts(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		if err := ingest.Stream(context.Background(), bytes.NewReader(dialectsSnapshot(t)), w); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dialectsFile)), dialectsSource, 0o644); err != nil {
			t.Fatal(err)
		}
		opt := testOptions(root, s)
		opt.Repository = "dialects"
		for _, tc := range []struct{ name, path string }{
			{"schema-sets-model-dialects", "/api/v0/schema/sets"},
			{"schema-table-model-dialects", schemaPath("table", "table:"+dialectsFile+"#Order") + "&editor=zed"},
		} {
			rec := get(t, opt, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			normalizeLinks(v, root)
			golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, root)
		}
		var body struct {
			Sets []struct {
				Dialects  []string `json:"dialects"`
				Conflicts []string `json:"conflicts"`
				Tables    []struct {
					VerificationCounts map[string]int `json:"verification_counts"`
					Columns            []struct {
						Node contract.Node `json:"node"`
					} `json:"columns"`
				} `json:"tables"`
			} `json:"sets"`
		}
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/schema/sets", nil), &body)
		if len(body.Sets) != 1 || len(body.Sets[0].Tables) != 1 {
			t.Fatalf("sets %+v", body.Sets)
		}
		g := body.Sets[0]
		if len(g.Dialects) != 3 || g.Dialects[0] != "mysql" || len(g.Conflicts) != 0 {
			t.Errorf("dialects %v conflicts %v", g.Dialects, g.Conflicts)
		}
		for _, c := range g.Tables[0].Columns {
			if _, has := c.Node.Attributes["claims"]; has {
				t.Errorf("%s: /sets serves claims", c.Node.Identity)
			}
		}
		if want := (map[string]int{"oracle": 12, "mapped": 29}); !equalCounts(g.Tables[0].VerificationCounts, want) {
			t.Errorf("verification_counts %v, want %v", g.Tables[0].VerificationCounts, want)
		}
	})
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestCommonStringList checks the group dialects rule: the common list,
// nil when no table has one, a conflict when tables differ or one lacks it.
func TestCommonStringList(t *testing.T) {
	node := func(v any) contract.Node {
		attrs := map[string]any{}
		if v != nil {
			attrs["dialects"] = v
		}
		return contract.Node{Attributes: attrs}
	}
	for _, tc := range []struct {
		name     string
		nodes    []contract.Node
		want     []string
		conflict bool
	}{
		{"none", []contract.Node{node(nil), node(nil)}, nil, false},
		{"same", []contract.Node{node([]any{"mysql", "sqlite"}), node([]any{"mysql", "sqlite"})}, []string{"mysql", "sqlite"}, false},
		{"differ", []contract.Node{node([]any{"mysql"}), node([]any{"sqlite"})}, nil, true},
		{"one lacks it", []contract.Node{node([]any{"mysql"}), node(nil)}, nil, true},
		{"not strings", []contract.Node{node([]any{"mysql"}), node([]any{1})}, nil, true},
	} {
		got, conflict := commonStringList(tc.nodes, "dialects")
		if conflict != tc.conflict || len(got) != len(tc.want) || (got != nil && got[0] != tc.want[0]) {
			t.Errorf("%s: %v %t, want %v %t", tc.name, got, conflict, tc.want, tc.conflict)
		}
	}
}
