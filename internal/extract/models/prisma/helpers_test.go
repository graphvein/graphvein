package prisma

import (
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// checkRecords runs the shared claim checks over emitted records and
// checks that every reason and claim names a row of the contract.
func checkRecords(t *testing.T, nodes []contract.Node) map[string][]modeltest.Claim {
	t.Helper()
	claims := modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	ids := map[string]bool{}
	for _, r := range Contract {
		ids[r.ID] = true
	}
	for id, list := range claims {
		for _, c := range list {
			if !ids[c.Row] {
				t.Errorf("%s: claim %s names row %q, not a contract row", id, c.Fact, c.Row)
			}
		}
	}
	for _, n := range nodes {
		reasons, _ := n.Attributes["reasons"].([]models.Reason)
		for _, r := range reasons {
			if !ids[r.Row] {
				t.Errorf("%s: reason names row %q, not a contract row", n.Identity, r.Row)
			}
		}
	}
	return claims
}

// countRecords summarises emitted records for the corpus and fixture
// checks.
func countRecords(nodes []contract.Node) string {
	var tables, columns, typed, defaults, complete int
	verification := map[string]int{}
	for _, n := range nodes {
		a := n.Attributes
		switch n.Kind {
		case "table":
			tables++
			if a["state"] == "complete" {
				complete++
			}
		case "column":
			columns++
			if a["type"] != nil && a["nullable"] != nil {
				typed++
			}
			if a["server_default"] != nil {
				defaults++
			}
			for _, c := range a["claims"].([]models.Claim) {
				verification[c.Verification]++
			}
		}
	}
	return fmt.Sprintf("tables %d columns %d typed %d defaults %d complete %d oracle %d", tables, columns, typed, defaults, complete, verification[models.VerificationOracle])
}

// baselineCounts counts the facts the investigation oracle baseline counts
// (the T018g1 task record (internal), "Oracle baseline"): NOT NULL
// columns, columns with a server default, tables with a primary key,
// unique and non-unique indexes, and enum-typed columns.
func baselineCounts(nodes []contract.Node) string {
	var notNull, defaults, pks, unique, indexes, enums, fks, emulated int
	constraints := map[string]bool{}
	for _, n := range nodes {
		a := n.Attributes
		switch n.Kind {
		case "table":
			if pk, ok := a["primary_key"].([]string); ok && len(pk) > 0 {
				pks++
			}
			rel, _ := a["emulated_relations"].([]map[string]any)
			emulated += len(rel)
			list, _ := a["indexes"].([]map[string]any)
			for _, ix := range list {
				if ix["unique"] == true {
					unique++
				} else {
					indexes++
				}
			}
		case "column":
			if a["nullable"] == false {
				notNull++
			}
			if a["server_default"] == true {
				defaults++
			}
			if a["enum"] != nil {
				enums++
			}
			// One count per constraint: a composite key is listed on each
			// of its columns.
			list, _ := a["foreign_keys"].([]map[string]any)
			for _, fk := range list {
				constraints[fmt.Sprint(n.Identity[:strings.LastIndex(n.Identity, ".")], fk["local_columns"], fk["table"])] = true
			}
		}
	}
	fks = len(constraints)
	return fmt.Sprintf("not_null %d defaults %d pks %d unique %d indexes %d enum_columns %d foreign_keys %d emulated %d", notNull, defaults, pks, unique, indexes, enums, fks, emulated)
}
