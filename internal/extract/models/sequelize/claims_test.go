package sequelize

import (
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// claimFactRows are the rows that may claim each claim fact: the
// attribute and option rows, and the association rows for the
// attributes an association adds (SQ-R1, SQ-R2) or the allowNull it
// merges.
var claimFactRows = map[string][]string{
	"name":           {"SQ-K2", "SQ-O1", "SQ-R1", "SQ-R2"},
	"type":           {"SQ-A1", "SQ-O1", "SQ-R1", "SQ-R2"},
	"nullable":       {"SQ-A2", "SQ-O1", "SQ-R1", "SQ-R2"},
	"primary_key":    {"SQ-A2", "SQ-O1", "SQ-R1", "SQ-R2"},
	"unique":         {"SQ-A2", "SQ-O1", "SQ-O2", "SQ-R1", "SQ-R2"},
	"index":          {"SQ-A2", "SQ-O1", "SQ-O2", "SQ-R1", "SQ-R2"},
	"default":        {"SQ-A2", "SQ-O1", "SQ-R1", "SQ-R2"},
	"server_default": {"SQ-A2", "SQ-O1", "SQ-R1", "SQ-R2"},
	"foreign_keys":   {"SQ-A3", "SQ-O1", "SQ-R1", "SQ-R2"},
}

// oracleForms are the claim forms the investigation oracle compared on postgres
// and sqlite (the T018g3 task record (internal): the SQ-A1 run list,
// SQ-A2 and SQ-O1) and that the oracle
// compares over the fixture (TestSequelizeOracle fails when a fixture run
// leaves one uncompared): a claim labelled oracle must have one of them
// (claimHook's form). Name, primary_key, unique and index
// claims are compared by value for every form; the forms are their
// values.
var oracleForms = map[string][]string{
	"type": {
		"STRING", "STRING(n)", "CHAR(n)", "TEXT", "INTEGER", "BIGINT", "SMALLINT", "FLOAT", "DOUBLE",
		"DECIMAL", "DECIMAL(p,s)", "BOOLEAN", "DATE", "DATEONLY", "TIME", "UUID", "JSON", "JSONB", "BLOB",
		"ENUM", "ARRAY(STRING)", "ARRAY(INTEGER)", "ARRAY(ENUM)", "RANGE(DATE)",
		"autoIncrement INTEGER", "autoIncrement BIGINT",
		"sqlite integer primary key INTEGER", "sqlite integer primary key BIGINT",
	},
	"nullable": {"autoIncrement", "sqlite integer primary key", "primary key", "allowNull true", "allowNull false", "allowNull absent"},
	"default": {
		"absent", "autoIncrement", "null on *", "literal on *", "fn on *",
		"string on STRING", "string on CHAR", "string on TEXT", "string on ENUM", "string on JSON", "string on JSONB",
		"number on INTEGER", "number on BIGINT", "number on SMALLINT", "number on FLOAT", "number on DOUBLE", "number on DECIMAL",
		"number on JSON", "number on JSONB", "boolean on BOOLEAN", "boolean on JSON", "boolean on JSONB",
		"json on JSON", "json on JSONB",
	},
}

func init() {
	// server_default is claimed for the default forms and, false, for a
	// client-side default (NOW, UUIDV4, UUIDV1, a function).
	oracleForms["server_default"] = append(append([]string{}, oracleForms["default"]...), "client on *")
}

func formAllowed(list []string, form string) bool {
	for _, f := range list {
		if f == form || strings.HasSuffix(f, " on *") && strings.HasPrefix(form, strings.TrimSuffix(f, "*")) {
			return true
		}
	}
	return false
}

// TestSequelizeClaims checks the claims of the fixture (T018g0 Revision
// 1): each claim's row is one claimFactRows allows; a claim is oracle only
// in a postgres or sqlite group and only for a form the investigation oracle
// compared (oracleForms), mapped exactly in the mysql, mariadb and mssql
// groups; each table's verification is its group's; and every listed
// type, nullability and default form is reached by the fixture on an
// oracle dialect, which step SQB5's oracle run compares.
func TestSequelizeClaims(t *testing.T) {
	forms := map[*models.Column]map[string]string{}
	claimHook = func(col *models.Column, fact, row, form string) {
		if forms[col] == nil {
			forms[col] = map[string]string{}
		}
		forms[col][fact] = form
	}
	defer func() { claimHook = nil }()
	nodes, _, _ := linkFixture(t)
	dialects := map[string]string{}
	for _, n := range nodes {
		if n.Kind == "table" {
			d, _ := n.Attributes["dialect"].(string)
			dialects[n.File+"#"+n.QualifiedName] = d
			want := models.VerificationOracle
			if d != "postgres" && d != "sqlite" {
				want = models.VerificationMapped
			}
			if n.Attributes["verification"] != want {
				t.Errorf("%s: verification %v on %s", n.Identity, n.Attributes["verification"], d)
			}
		}
	}
	// Forms by column: the hook sees records, the nodes carry identities;
	// match them through the defining file, line and attribute key.
	byKey := map[string]map[string]string{}
	for col, f := range forms {
		byKey[fmt.Sprintf("%s:%d:%s", col.File, col.Line, col.Attribute)] = f
	}
	reached := map[string]bool{}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		table := n.File + "#" + strings.SplitN(n.QualifiedName, ".", 2)[0]
		d := dialects[table]
		list, _ := n.Attributes["claims"].([]models.Claim)
		def, _ := n.Attributes["defined_in"].(map[string]any)
		f := byKey[fmt.Sprintf("%s:%v:%s", def["file"], def["line"], n.Attributes["attribute"])]
		for _, c := range list {
			if !containsString(claimFactRows[c.Fact], c.Row) {
				t.Errorf("%s: fact %s claimed by row %s", n.Identity, c.Fact, c.Row)
			}
			switch {
			case d == "postgres" || d == "sqlite":
				if c.Verification != models.VerificationOracle {
					t.Errorf("%s: %s claim %s on %s", n.Identity, c.Fact, c.Verification, d)
				}
				if allowed, ok := oracleForms[c.Fact]; ok {
					form := f[c.Fact]
					if !formAllowed(allowed, form) {
						t.Errorf("%s: %s claim of form %q is labelled oracle; the oracle did not compare that form", n.Identity, c.Fact, form)
					}
					reached[c.Fact+"/"+form] = true
				}
			default:
				if c.Verification != models.VerificationMapped {
					t.Errorf("%s: %s claim %s on %s", n.Identity, c.Fact, c.Verification, d)
				}
			}
		}
	}
	for fact, list := range oracleForms {
		for _, form := range list {
			ok := false
			for r := range reached {
				ok = ok || r == fact+"/"+form || strings.HasSuffix(form, " on *") && strings.HasPrefix(r, fact+"/"+strings.TrimSuffix(form, "*"))
			}
			if !ok && !optionalForms[fact+"/"+form] {
				t.Errorf("oracle form %s/%s is not reached by the fixture", fact, form)
			}
		}
	}
	_ = contract.Node{}
}

// optionalForms are listed oracle forms the first part of the fixture
// does not reach yet (step SQB4 completes it).
var optionalForms = map[string]bool{}
