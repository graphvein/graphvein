package typeorm

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// corpusClaims are the claim-pass counts per corpus: tables and columns
// claimed, unknown columns, tables with a complete column set and with a
// complete constraint set, per-dialect type claims, and the foreign keys
// (constraints, counted once per table), unique constraints and indexes
// listed.
type corpusClaims struct {
	Tables, Columns, Unknown, Complete, ConsComplete int
	FKs, Uniques, Indexes                            int
	// Types counts the type claims per dialect.
	Types map[string]int
}

// The counts against the coverage targets (the T018g6 task record,
// internal): seerr 14 of 14 tables, 157 columns (the 16 join columns
// included), 132 types per dialect, 16 foreign keys with actions, 7
// unique sets, 25 indexes; nest-admin 17 tables (15 entities and 2
// junctions; sys_dept partial through @Tree, D), 144 columns, 143 MySQL
// types, 10 foreign keys (sys_dict_item.type_id has none,
// createForeignKeyConstraints: false; the catalog's 11th, @TreeParent's
// parentId, is D), 7 unique sets, 4 junction indexes and (Revision 4
// A3) 5 MySQL foreign key indexes (unnamed: nest-admin has migrations;
// deploy/sql/nest_admin.sql shows each as KEY FK_<hash>); Maintainerr 18
// tables (17 and the junction) in each of its two groups with 12 foreign
// keys, 1 unique set and 6 indexes each; aqualink-app 29 tables (27 and 2
// junctions), 5 partial (4 ApiPointProperty tables, site_application's
// @Generated), 39 foreign keys.
var corpusClaimWant = map[string]corpusClaims{
	"seerr":        {Tables: 14, Columns: 157, Unknown: 0, Complete: 14, ConsComplete: 14, FKs: 16, Uniques: 7, Indexes: 25, Types: map[string]int{"postgres/oracle": 132, "sqlite/oracle": 132}},
	"nest-admin":   {Tables: 17, Columns: 144, Unknown: 0, Complete: 16, ConsComplete: 16, FKs: 10, Uniques: 7, Indexes: 9, Types: map[string]int{"/mapped": 143}},
	"maintainerr":  {Tables: 36, Columns: 364, Unknown: 0, Complete: 36, ConsComplete: 36, FKs: 24, Uniques: 2, Indexes: 12, Types: map[string]int{"/oracle": 358}},
	"aqualink-app": {Tables: 29, Columns: 283, Unknown: 1, Complete: 24, ConsComplete: 22, FKs: 39, Uniques: 8, Indexes: 36, Types: map[string]int{"/oracle": 280}},
}

// TestTypeORMCorpusClaims runs the claim pass over the four corpora
// (scratch clones at the pins; GRAPHVEIN_TYPEORM_CORPUS names their
// parent directory) and checks the claim shape (modeltest.CheckClaims,
// CheckDialectFacts) and the counts above.
func TestTypeORMCorpusClaims(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TYPEORM_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_TYPEORM_CORPUS is not set")
	}
	for _, corpus := range sortedCorpora() {
		t.Run(corpus, func(t *testing.T) {
			in := corpusInput(t, filepath.Join(dir, corpus))
			nodes, _, diags := Link(in)
			claims := modeltest.CheckClaims(t, nodes, models.ClaimFacts)
			got := countClaims(nodes, claims)
			for _, l := range diagLines(diags) {
				if strings.Contains(l, "schema_model_import_failure") || strings.Contains(l, "internal_error") {
					t.Log(l)
				}
			}
			t.Logf("%s: %+v", corpus, got)
			want := corpusClaimWant[corpus]
			if !reflect.DeepEqual(got, want) {
				t.Errorf("claims %+v, want %+v", got, want)
			}
		})
	}
}

func sortedCorpora() []string {
	var out []string
	for k := range corpusClaimWant {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func countClaims(nodes []contract.Node, claims map[string][]modeltest.Claim) corpusClaims {
	out := corpusClaims{Types: map[string]int{}}
	fks := map[string]bool{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			out.Tables++
			if n.Attributes["column_set"] == "complete" {
				out.Complete++
			}
			if n.Attributes["constraint_set"] == "complete" {
				out.ConsComplete++
			}
			if list, ok := n.Attributes["unique_constraints"].([]map[string]any); ok {
				out.Uniques += len(list)
			}
			if list, ok := n.Attributes["indexes"].([]map[string]any); ok {
				out.Indexes += len(list)
			}
		case "column":
			out.Columns++
			if n.Attributes["state"] == "unknown" {
				out.Unknown++
			}
			if list, ok := n.Attributes["foreign_keys"].([]map[string]any); ok {
				table := strings.TrimSuffix(n.Identity, "."+n.Name)
				for _, fk := range list {
					fks[table+fmt.Sprint(fk["local_columns"])] = true
				}
			}
			for _, cl := range claims[n.Identity] {
				if cl.Fact == "type" {
					out.Types[cl.Dialect+"/"+cl.Verification]++
				}
			}
		}
	}
	out.FKs = len(fks)
	return out
}
