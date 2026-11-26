package schema

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// claimFactKinds maps a claimed attribute to the rowFacts kinds whose rows
// may set it (TestContractFacts' table): a column's own attributes are
// column facts; primary_key and unique follow from keys (and unique also
// from unique indexes); references from foreign keys; every db_index
// attribute is an index fact.
var claimFactKinds = map[string][]string{
	"exists": {factColumn}, "name": {factColumn}, "type": {factColumn}, "type_resolved": {factColumn},
	"nullable": {factColumn}, "default": {factColumn}, "collation": {factColumn},
	"identity": {factColumn}, "generated": {factColumn},
	"primary_key":           {factKey, factColumn},
	"unique":                {factKey, factIndex, factColumn},
	"references":            {factFK},
	"additional_references": {factFK},
}

// nullRowReasons are the set-level partial reasons no contract row
// produces (schema.go, after the statements): their row is null.
var nullRowReasons = []string{
	"the set directory could not be listed (",
	"a member of the set is not readable (",
	"sqlx.toml is not read; it may change the migration settings",
}

// checkProvenance checks T018f's provenance attributes on linked records:
// every known claimed fact of a column or db_index has exactly one claim
// whose row may set that fact; opaque_sites has one entry per opaque
// statement; every table reason has a row, null only for the set-level
// reasons listed in nullRowReasons.
func checkProvenance(t *testing.T, label string, nodes []contract.Node, stats map[string]int) {
	t.Helper()
	rowKinds := map[string]bool{}
	for _, r := range contractRows {
		rowKinds[r.id] = true
	}
	for _, n := range nodes {
		switch n.Kind {
		case "column", "db_index":
			facts := columnClaimFacts
			if n.Kind == "db_index" {
				facts = indexClaimFacts
			}
			list, ok := n.Attributes["claims"].([]any)
			if !ok {
				t.Errorf("%s: %s: claims %v", label, n.Identity, n.Attributes["claims"])
				continue
			}
			count := map[string]int{}
			for _, raw := range list {
				c, _ := raw.(map[string]any)
				fact, _ := c["fact"].(string)
				row, _ := c["row"].(string)
				count[fact]++
				stats[n.Kind+" "+fact+" "+row]++
				kinds := claimFactKinds[fact]
				if n.Kind == "db_index" {
					kinds = []string{factIndex}
				}
				allowed := false
				for _, k := range kinds {
					if slices.Contains(rowFacts[row], k) {
						allowed = true
					}
				}
				if !rowKinds[row] || !allowed {
					t.Errorf("%s: %s: fact %s claimed by row %q, which may not set it", label, n.Identity, fact, row)
				}
				if m, _ := c["migration"].(string); m == "" || c["span"] == nil {
					t.Errorf("%s: %s: claim without a statement: %v", label, n.Identity, c)
				}
			}
			for _, fact := range facts {
				known := fact == "exists" || n.Attributes[fact] != nil
				if fact == "name" && n.Kind == "column" {
					prev, _ := n.Attributes["previous_names"].([]string)
					known = len(prev) > 0 || count[fact] > 0
				}
				want := 0
				if known {
					want = 1
				}
				if count[fact] != want {
					t.Errorf("%s: %s: %d claims of %s (want %d)", label, n.Identity, count[fact], fact, want)
				}
			}
		case "migration":
			sites, isList := n.Attributes["opaque_sites"].([]any)
			opaque, isInt := n.Attributes["opaque"].(int)
			switch {
			case isInt && (!isList || len(sites) != opaque):
				t.Errorf("%s: %s: opaque %d, opaque_sites %v", label, n.Identity, opaque, n.Attributes["opaque_sites"])
			case !isInt && n.Attributes["opaque_sites"] != nil:
				t.Errorf("%s: %s: opaque_sites without counts", label, n.Identity)
			}
			for _, raw := range sites {
				site, _ := raw.(map[string]any)
				row, _ := site["row"].(string)
				stats["opaque_site "+row]++
				if !rowKinds[row] {
					t.Errorf("%s: %s: opaque site row %q", label, n.Identity, row)
				}
			}
		case "table":
			reasons, _ := n.Attributes["reasons"].([]any)
			for _, raw := range reasons {
				r, _ := raw.(map[string]any)
				row, hasRow := r["row"]
				if !hasRow {
					t.Errorf("%s: %s: reason without row: %v", label, n.Identity, r)
					continue
				}
				id, isString := row.(string)
				text, _ := r["reason"].(string)
				switch {
				case row == nil:
					if !slices.ContainsFunc(nullRowReasons, func(p string) bool { return strings.HasPrefix(text, p) }) {
						t.Errorf("%s: %s: reason with a null row: %q", label, n.Identity, text)
					}
				case !isString || !contractID(id):
					t.Errorf("%s: %s: reason row %v is not a contract id", label, n.Identity, row)
				default:
					stats["reason "+id]++
				}
			}
		}
	}
}

// TestClaimProvenance links the fixtures, the regression cases and the SQL
// cases TestContractFacts runs and checks every provenance attribute
// (checkProvenance).
func TestClaimProvenance(t *testing.T) {
	stats := map[string]int{}
	link := func(label string, sources map[string][]byte, root string) {
		nodes, _, _ := Link(Input{Sources: sources, Root: root})
		checkProvenance(t, label, nodes, stats)
	}
	history := func(h *oracleHistory) {
		root := t.TempDir()
		if err := h.writeTo(root); err != nil {
			t.Fatal(err)
		}
		pkg := ""
		sources := map[string][]byte{}
		if h.Workspace != "" {
			pkg = "pkg/"
			sources["Cargo.toml"] = []byte(h.Workspace)
		}
		sources[pkg+"Cargo.toml"] = []byte(h.cargo())
		for _, f := range h.Files {
			sources[pkg+"migrations/"+f.Name] = []byte(strings.ReplaceAll(f.SQL, "\r\n", "\n"))
		}
		link(h.ID, sources, root)
	}
	for _, h := range fixtureHistories(t) {
		history(h)
	}
	for _, c := range readCases(t) {
		history(c.History)
	}
	for _, cases := range [][]b1dCase{b1dCases, b2Cases, b3Cases, rev15Cases, rev15C2Cases, rev15C3Cases, rev16Cases, rev16D2Cases, rev17Cases, rev18Cases, rev19Cases, rev19bCases, rev19bTxCases, rev19cCases} {
		for _, tc := range cases {
			sources := map[string][]byte{"Cargo.toml": []byte(pgManifest), filepath.ToSlash(filepath.Join("migrations", "1_m.sql")): []byte(tc.sql)}
			link(tc.name, sources, "")
		}
	}
	for _, want := range []string{"column exists M1", "column type M2", "column references M3", "db_index exists M4", "column name M5h", "column nullable M5d", "column default M5e", "column type M5c", "opaque_site O1"} {
		if stats[want] == 0 {
			t.Errorf("no claim %q reached", want)
		}
	}
}
