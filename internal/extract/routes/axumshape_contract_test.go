package routes

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const axumShapeDoc = "../../../docs/contract/claims/route-shape-axum.md"

// TestAxumShapeContractTable keeps axumShapeRows and the claim contract
// document 1:1: ids, order and forms, with no form in two rows.
func TestAxumShapeContractTable(t *testing.T) {
	data, err := os.ReadFile(axumShapeDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	type row struct {
		id    string
		forms []string
	}
	var rows []row
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-X") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := row{id: strings.TrimSpace(cells[1])}
		for _, f := range strings.Split(cells[3], "`, `") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.forms = append(r.forms, f)
			}
		}
		rows = append(rows, r)
	}
	if len(rows) != len(axumShapeRows) {
		t.Fatalf("document has %d rows, code %d", len(rows), len(axumShapeRows))
	}
	owner := map[string]string{}
	for i, r := range axumShapeRows {
		if rows[i].id != r.ID || !reflect.DeepEqual(rows[i].forms, r.Forms) {
			t.Errorf("row %d: document %s %v, code %s %v", i, rows[i].id, rows[i].forms, r.ID, r.Forms)
		}
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	if axumShapeRows[len(axumShapeRows)-1].ID != "D" {
		t.Error("rows must end with D")
	}
}

// axumRowEvidence is, per row, text the fixture goldens must contain for
// the row to be exercised: the row id as a provenance rule, or a reason
// only that row writes.
var axumRowEvidence = map[string][]string{
	"SH-X1":  {`"rule": "SH-X1"`},
	"SH-X2":  {`"rule": "SH-X2"`},
	"SH-X3":  {`"rule": "SH-X3"`},
	"SH-X4":  {`"rule": "SH-X4"`, "of field big is not read from URL-encoded data", `"name": "limit"`},
	"SH-X5":  {`"rule": "SH-X5"`, "the handler accepts any query parameter"},
	"SH-X6":  {`"rule": "SH-X6"`},
	"SH-X7":  {`"rule": "SH-X7"`, "application/x-www-form-urlencoded", "Form on ANY reads the query on GET and the body", "HEAD /r/form-head", "HEAD /api/form-head"},
	"SH-X8":  {`"rule": "SH-X8"`},
	"SH-X9":  {"implements FromRequest and may read the body"},
	"SH-X10": {`"rule": "SH-X10"`},
	"SH-X11": {`"rule": "SH-X11"`, "headers read by iter", "takes the headers before a later extractor in axum before 0.5"},
	"SH-X12": {"GET /items"},
	"SH-X13": {`"rule": "SH-X13"`, "read_by_dependency"},
	"SH-X14": {"the handler takes the raw request", "the request value parts is used by extract", "the request value parts is used by macro:info", "depth cap reached (2) at src/review.rs"},
	"SH-X15": {"(a wrapped extractor) is not read"},
	"SH-X16": {`"name": "itemName"`, `"name": "type"`, `"name": "up-since"`, `"name": "foo"`, `"name": "barBaz"`, `"name": "userId"`},
	"SH-X17": {`"LONG_PLAY"`, `"tape"`, `"asc"`},
	"SH-X18": {"serde attribute untagged on Event is not modelled", "flattened fields", "attribute serde_as on AsStr", "attribute cfg_attr(serde) on CfgNamed"},
	"SH-X19": {`"type": "integer"`, `"type": "number"`, `"type": "boolean"`, `"nullable": true,` + "\n" + `                    "type": "number"`},
	"SH-X20": {`"name": "user"`},
	"SH-X21": {`"rule": "SH-X21"`},
	"SH-X22": {`"rule": "SH-X22"`, `"content_type": "application/problem+json"`},
	"SH-X23": {`"rule": "SH-X23"`},
	"SH-X24": {`"rule": "SH-X24"`, "redirect status of Redirect::to"},
	"SH-X25": {`"rule": "SH-X25"`, `"content_type": "text/plain; charset=utf-8"`, `"content_type": "text/plain",`, "the content type of text responses depends on the axum version"},
	"SH-X26": {`"rule": "SH-X26"`, "status of error GuardErr::Db", "status of error IfErr::Bad", `"status": "410"`},
	"SH-X27": {`"section": "responses"`, "values with different schemas answer 200"},
	"SH-X28": {`"stop": "cycle"`, `"stop": "depth_cap"`, `"stop": "external"`, "(respond); "},
	"SH-X29": {`"state": "complete"`, `"state": "provably_none"`, `"state": "partial"`},
}

// TestAxumShapeFixtureRows checks that the fixture goldens exercise every
// row of the contract.
func TestAxumShapeFixtureRows(t *testing.T) {
	var all strings.Builder
	for _, g := range []string{"route-shape-axum.golden.json", "route-shape-axum.depth2.golden.json", "route-shape-axum03.golden.json", "route-shape-axum-versions.golden.json"} {
		data, err := os.ReadFile("../../../testdata/fixtures/" + g)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	text := all.String()
	for _, r := range axumShapeRows {
		if r.ID == "D" {
			continue
		}
		evidence, ok := axumRowEvidence[r.ID]
		if !ok {
			t.Errorf("row %s has no fixture evidence", r.ID)
			continue
		}
		for _, e := range evidence {
			if !strings.Contains(text, e) {
				t.Errorf("row %s: fixtures do not contain %q", r.ID, e)
			}
		}
	}
}
