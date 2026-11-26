package sqlmodel

import (
	"fmt"
	"slices"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// claimFactRows are the rows whose branch may set each claim fact (T018g0):
// a claim names the row whose branch wrote the value. M2-M4 claim the name
// and the absences of a Field or bare member that no F row states; K1 a
// Column's name and its proven-empty foreign keys; A1 and A2 a nullability
// the annotation states (Revision 2), F2 one a Field keyword or the
// primary key decides.
var claimFactRows = map[string][]string{
	"name":           {"M2", "M3", "M4", "K1"},
	"type":           {"F5", "T1"},
	"length":         {"F5", "T1"},
	"timezone":       {"F5", "T1"},
	"precision":      {"F5", "T1"},
	"scale":          {"F5", "T1"},
	"nullable":       {"F2", "K2", "A1", "A2"},
	"primary_key":    {"F1", "K2"},
	"index":          {"F1", "K2"},
	"unique":         {"F1", "K2"},
	"default":        {"M3", "M4", "F3", "F3b", "K4"},
	"server_default": {"M2", "M3", "M4", "K4"},
	"foreign_keys":   {"M2", "M3", "M4", "F4", "K1", "K3"},
	// ADR 0065's facts: SQLModel never claims them.
	"type_family":   nil,
	"autoincrement": nil,
}

// oracleCompared are the column facts TestModelOracle compares with
// SQLAlchemy's MetaData (the column's name by lookup, comparedFacts, and
// the type's length, timezone, precision and scale): the facts whose
// claims are verification oracle.
var oracleCompared = append([]string{"name", "length", "timezone", "precision", "scale"}, comparedFacts...)

// sizeForms are the value forms each row's branch writes for the type's
// size facts (columns.go setType and typeExpr): "none", "int", "true" or
// "false". Unknown (nil) values carry no claim.
var sizeForms = map[string][]string{
	"length/F5":    {"none", "int"},
	"length/T1":    {"none", "int"},
	"timezone/F5":  {"false"},
	"timezone/T1":  {"false", "true"},
	"precision/F5": {"none", "int"},
	"precision/T1": {"none"},
	"scale/F5":     {"none", "int"},
	"scale/T1":     {"none"},
}

// claimForm names the form of a claimed value (T018g0 Revision 1): the
// class for T1's type, the source type for F5's (Revision 2: source is the
// annotation's resolved type or enum base, so two source types that map
// to one class are two forms), the value form for the size facts, the
// fact/row pair for every other fact.
func claimForm(fact, row string, v any, source string) string {
	pair := fact + "/" + row
	switch {
	case pair == "type/F5":
		return pair + "/" + source
	case fact == "type":
		return fmt.Sprintf("%s/%v", pair, v)
	case fact == "length" || fact == "timezone" || fact == "precision" || fact == "scale":
		switch x := v.(type) {
		case string:
			return pair + "/" + x
		case bool:
			return fmt.Sprintf("%s/%t", pair, x)
		default:
			return pair + "/int"
		}
	}
	return pair
}

// claimForms is every form a claim branch can write: the fact/row pairs of
// claimFactRows, sizeForms, and for type each T1 class (tList) and each F5
// source type (the keys of f5Types and enumBases).
func claimForms() map[string]bool {
	out := map[string]bool{}
	for f, rows := range claimFactRows {
		for _, r := range rows {
			pair := f + "/" + r
			switch {
			case f == "type" && r == "T1":
				for _, c := range tList {
					out[pair+"/"+c] = true
				}
			case f == "type" && r == "F5":
				for source := range f5Types {
					out[pair+"/"+source] = true
				}
				for base := range enumBases {
					out[pair+"/"+base] = true
				}
			case sizeForms[pair] != nil:
				for _, form := range sizeForms[pair] {
					out[pair+"/"+form] = true
				}
			default:
				out[pair] = true
			}
		}
	}
	return out
}

// TestModelClaims checks the column claims over both fixtures: every known
// claim fact of a read column has exactly one claim, an unknown fact or an
// unknown column none, each claim's row is one claimFactRows allows (and a
// contract row with a claim branch), every claimed fact is one the oracle
// compares, and every claim and table is verification oracle in the
// group models/sqlmodel/. (T018g0, ADR 0058).
func TestModelClaims(t *testing.T) {
	if !slices.Equal(sortedKeys(claimFactRows), sortedCopy(models.ClaimFacts)) {
		t.Fatalf("claimFactRows %v, core ClaimFacts %v", sortedKeys(claimFactRows), models.ClaimFacts)
	}
	used, forms := map[string]bool{}, map[string]bool{}
	columns := 0
	sources := map[string]string{}
	f5SourceHook = func(identity, source string) { sources[identity] = source }
	defer func() { f5SourceHook = nil }()
	for _, dir := range []string{"../../../../testdata/fixtures/sqlmodel-models", "../../../../testdata/fixtures/sqlmodel-link"} {
		files, raw := parseAll(t, fixtureSources(t, dir))
		clear(sources)
		nodes, _, _ := Link(Input{Python: files, Resolver: python.NewModuleResolver(files, raw), Sources: raw})
		modeltest.CheckClaims(t, nodes, models.ClaimFacts)
		for _, n := range nodes {
			if n.Kind == "table" {
				if n.Attributes["set"] != "models/sqlmodel/." || n.Attributes["verification"] != models.VerificationOracle {
					t.Errorf("%s: set %v, verification %v", n.Identity, n.Attributes["set"], n.Attributes["verification"])
				}
				if _, has := n.Attributes["dialect"]; has {
					t.Errorf("%s: SQLModel states no dialect", n.Identity)
				}
				continue
			}
			columns++
			checkColumnClaims(t, n, sources[n.Identity], used, forms)
		}
	}
	if columns == 0 {
		t.Fatal("no columns")
	}
	// A fact is oracle only for a form the oracle has compared: every form
	// a claim branch can write is reached by the two fixtures, which
	// TestModelOracle compares with SQLAlchemy (0 disagreements, T018g0
	// Revision 1), and the fixtures reach no other form.
	all := claimForms()
	for form := range all {
		if !forms[form] {
			t.Errorf("claim form %s is not reached by an oracle-compared fixture case", form)
		}
	}
	for form := range forms {
		if !all[form] {
			t.Errorf("claim form %s is not in claimForms", form)
		}
	}
	for f, rows := range claimFactRows {
		for _, r := range rows {
			if !used[f+"/"+r] {
				t.Errorf("fact %s: row %s never claims it", f, r)
			}
			if modelClaimRows[r] == "" {
				t.Errorf("fact %s: row %s has no claim branch", f, r)
			}
		}
	}
}

// checkColumnClaims checks SQLModel's own claim rules on one column (the
// shared ones are modeltest.CheckClaims'): rows per fact, oracle-compared
// facts, verification oracle, and records each claim's row and form.
// source is the source type F5 mapped the column's type from.
func checkColumnClaims(t *testing.T, n contract.Node, source string, used, forms map[string]bool) {
	t.Helper()
	a := n.Attributes
	list, ok := a["claims"].([]models.Claim)
	if !ok {
		t.Errorf("%s: claims %T", n.Identity, a["claims"])
		return
	}
	for _, c := range list {
		if c.Fact == "type" && c.Row == "F5" {
			if source == "" {
				t.Errorf("%s: F5 type claim without a source type", n.Identity)
			} else if want := f5Types[source]; want != a["type"] && !(enumBases[source] && a["type"] == "Enum") {
				t.Errorf("%s: F5 maps %s to %v", n.Identity, source, a["type"])
			}
		}
		if !slices.Contains(claimFactRows[c.Fact], c.Row) {
			t.Errorf("%s: fact %s claimed by row %s", n.Identity, c.Fact, c.Row)
		}
		if !slices.Contains(oracleCompared, c.Fact) {
			t.Errorf("%s: fact %s is not compared by the oracle", n.Identity, c.Fact)
		}
		if c.Verification != models.VerificationOracle || c.File == "" || c.Span.StartLine < 1 {
			t.Errorf("%s: claim %+v", n.Identity, c)
		}
		used[c.Fact+"/"+c.Row] = true
		forms[claimForm(c.Fact, c.Row, a[c.Fact], source)] = true
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
