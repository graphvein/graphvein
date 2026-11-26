package routes

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// commonJSContract is the code half of the CommonJS claim table
// (docs/extract/routes.md, "CommonJS modules", T036): each row, the forms
// its text must name, and the fixture cases (testdata/fixtures/
// express-commonjs/README.md) that exercise it.
var commonJSContract = []struct {
	ID    string
	Forms []string
	Cases []string
}{
	{"CJ-U1", []string{"`require(\"spec\")`", "Node's CommonJS loader", "`index`", "TypeScript file required from JavaScript", "ES module", "exists on disk but was not scanned", "`route_mount_unresolved` names the entry"}, []string{"C1", "C2", "C6", "C8", "C9", "C10", "C11", "R1", "R2", "R5", "R8", "R9", "U1", "U2", "U3", "K1"}},
	{"CJ-U2", []string{"`require(\"spec\").name`"}, []string{"C3", "R7"}},
	{"CJ-U3", []string{"`const { name, key: local } = require(\"spec\")`", "default value", "`const { Router } = require(\"express\")`"}, []string{"C4", "C5", "R11"}},
	{"CJ-U4", []string{"`require(\"pkg\")`", "factory allowlist"}, nil},
	{"CJ-E1", []string{"`module.exports = E`", "once"}, []string{"C1", "C7", "C9", "C12"}},
	{"CJ-E2", []string{"`module.exports = { name, key: E }`", "spread"}, []string{"C3"}},
	{"CJ-E3", []string{"`exports.name = E`", "`module.exports.name = E`"}, []string{"C4", "C5"}},
	{"CJ-E4", []string{"inside a function, branch or loop", "`exports = E`", "`Object.assign`", "ESM `export`"}, []string{"R3", "R4", "R6", "R10"}},
	{"D", []string{"every other form"}, nil},
}

// TestCommonJSContract checks the CommonJS table and the code table list
// the same rows in order, every form is named in its row, and every case of
// the fixture README is claimed by a row.
func TestCommonJSContract(t *testing.T) {
	ids, rows := goTable(t, goDocsPath, "\n### CommonJS modules (T036)\n")
	var want []string
	claimed := map[string]bool{}
	for _, r := range commonJSContract {
		want = append(want, r.ID)
		for _, f := range r.Forms {
			if !strings.Contains(rows[r.ID], f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		for _, c := range r.Cases {
			claimed[c] = true
		}
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("docs rows %v, code rows %v", ids, want)
	}
	// The unscanned-candidate fixture names its cases U and K.
	for fixture, pattern := range map[string]string{"express-commonjs": `(?m)^- ([CR]\d+) `, "express-commonjs-unscanned": `(?m)^- ([UK]\d+) `} {
		readme, err := os.ReadFile("../../../testdata/fixtures/" + fixture + "/README.md")
		if err != nil {
			t.Fatal(err)
		}
		cases := regexp.MustCompile(pattern).FindAllStringSubmatch(string(readme), -1)
		if len(cases) == 0 {
			t.Fatalf("no fixture cases in the %s README", fixture)
		}
		for _, c := range cases {
			if !claimed[c[1]] {
				t.Errorf("%s: fixture case %s is claimed by no row", fixture, c[1])
			}
		}
	}
}
