package routes

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	upClaimsPath   = "../../../docs/contract/claims/unknown-prefix-paths.md"
	upRouteTable   = "\n## Route side\n"
	upDefaultTable = "\n## Default\n"
	upDocsTable    = "\n### Unknown-prefix paths claim contract (route side)\n"
	upFixture      = "../../../testdata/fixtures/unknown-prefix-go"
)

// upImplemented are the T033 implementation steps whose route-side rows are
// implemented.
var upImplemented = map[string]bool{"UP-A1": true, "UP-A2": true}

// TestUnknownPrefixContract is the table half of the unknown-prefix route
// contract test: the public route-side rows and D, the docs copy and the
// code table list the same rows in the same order with the same text (docs
// and public), every listed form is named in its row, and every static tag
// in the Go linker names a row (one tagged branch per row of the
// implemented steps).
func TestUnknownPrefixContract(t *testing.T) {
	routeIDs, public := goTable(t, upClaimsPath, upRouteTable)
	defaultIDs, defaults := goTable(t, upClaimsPath, upDefaultTable)
	if !reflect.DeepEqual(defaultIDs, []string{"D"}) {
		t.Fatalf("default table rows %v, want [D]", defaultIDs)
	}
	publicIDs := append(append([]string(nil), routeIDs...), "D")
	public["D"] = defaults["D"]
	docsIDs, docs := goTable(t, goDocsPath, upDocsTable)
	if !reflect.DeepEqual(docsIDs, publicIDs) {
		t.Errorf("docs rows %v, public rows %v", docsIDs, publicIDs)
	}
	for _, id := range publicIDs {
		if docs[id] != public[id] {
			t.Errorf("row %s differs:\npublic: %s\ndocs:   %s", id, public[id], docs[id])
		}
	}
	var ids []string
	rows := map[string]upRow{}
	for _, r := range unknownPrefixContract {
		ids = append(ids, r.ID)
		rows[r.ID] = r
		for _, f := range r.Forms {
			if !strings.Contains(public[r.ID], f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if r.Unit == "" {
			t.Errorf("row %s: no unit", r.ID)
		}
	}
	if !reflect.DeepEqual(ids, publicIDs) {
		t.Errorf("code rows %v, public rows %v", ids, publicIDs)
	}
	for form, row := range upFormRows {
		listed := form == upFormBinding
		for _, f := range rows[row].Forms {
			listed = listed || f == form
		}
		if !listed {
			t.Errorf("hop form %q is not a listed form of %s", form, row)
		}
	}
	// Every D form the code tags is stated in a row's
	// refusal column.
	allForms := append(append([]string(nil), upDefaultForms...), upWebDefaultForms...)
	for _, form := range allForms {
		list, ok := upDefaultRefusals[form]
		if !ok {
			t.Errorf("D form %s: no refusal row names it", form)
		}
		for _, r := range list {
			cells := strings.Split(public[r.Row], " | ")
			if len(cells) < 3 || !strings.Contains(cells[len(cells)-1], r.Text) {
				t.Errorf("D form %s: row %s's refusal column does not state %q", form, r.Row, r.Text)
			}
		}
	}
	if len(upDefaultRefusals) != len(allForms) {
		t.Errorf("upDefaultRefusals has %d forms, the code tags %d", len(upDefaultRefusals), len(allForms))
	}
	tagged := map[string]bool{}
	files, _ := filepath.Glob("gohttp*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`l\.upTag\("((UP-[0-9]+)|D)[/"]`).FindAllStringSubmatch(string(data), -1) {
			if _, ok := rows[m[1]]; !ok {
				t.Errorf("%s: tag %s is not a contract row", f, m[1])
			}
			tagged[m[1]] = true
		}
		// A row tag from a variable (l.upTag(row)) is checked by the tag
		// half: upFormRows names UP-1, UP-2 and UP-4.
	}
	// The Express, FastAPI and Axum projection: its static
	// tags, and the row each linker passes it.
	for _, f := range []string{"unknown_prefix.go", "express.go", "fastapi.go", "axum.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`(?:tagUnknownPrefix|safeWebUnknownPrefix)\("((UP-[0-9]+)|D)[/"]`).FindAllStringSubmatch(string(data), -1) {
			if _, ok := rows[m[1]]; !ok {
				t.Errorf("%s: tag %s is not a contract row", f, m[1])
			}
			tagged[m[1]] = true
		}
	}
	// D is tagged through a variable form (tagUnknownPrefix("D/" + ...)).
	tagged["D"] = true
	for _, row := range upFormRows {
		tagged[row] = true
	}
	for _, r := range unknownPrefixContract {
		if upImplemented[r.Unit] && !tagged[r.ID] {
			t.Errorf("row %s has no tagged branch", r.ID)
		}
	}
}

// TestUnknownPrefixContractTags is the tag half: linking the
// unknown-prefix-go fixture and the go-routes fixture (its generated
// servers, UP-3) reaches every row and listed form of the implemented
// steps and every D form; every tag names a row, a listed form or a D
// form.
func TestUnknownPrefixContractTags(t *testing.T) {
	sources := map[string]string{}
	for f, s := range goFixtureSources(t) {
		sources["gr/"+f] = s
	}
	up := map[string]string{}
	err := filepath.WalkDir(upFixture, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(upFixture, p)
		up["up/"+filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f, s := range up {
		sources[f] = s
	}
	tags := map[string]int{}
	goLink(t, sources, tags)
	rows := map[string]upRow{}
	for _, r := range unknownPrefixContract {
		rows[r.ID] = r
	}
	forms := map[string]bool{}
	for _, f := range upDefaultForms {
		forms[f] = true
	}
	var got []string
	for tag := range tags {
		name, ok := strings.CutPrefix(tag, upTagPrefix)
		if !ok {
			continue
		}
		got = append(got, name)
		row, form, hasForm := strings.Cut(name, "/")
		r, ok := rows[row]
		if !ok {
			t.Errorf("tag %s is not a contract row", name)
			continue
		}
		if !hasForm {
			continue
		}
		if row == "D" {
			if !forms[form] {
				t.Errorf("tag %s is not a D form", name)
			}
			continue
		}
		listed := false
		for _, f := range r.Forms {
			listed = listed || f == form
		}
		if !listed {
			t.Errorf("tag %s is not a listed form of %s", name, row)
		}
	}
	sort.Strings(got)
	for _, r := range unknownPrefixContract {
		// The Go fixtures reach the Go rows; the shared rows UP-8..UP-10
		// and D carry step UP-A1. TestUnknownPrefixWebContractTags
		// checks step UP-A2's rows.
		if r.Unit != "UP-A1" {
			continue
		}
		if tags[upTagPrefix+r.ID] == 0 && r.ID != "D" {
			t.Errorf("the fixtures do not reach row %s", r.ID)
		}
		for _, f := range r.Forms {
			if tags[upTagPrefix+r.ID+"/"+f] == 0 {
				t.Errorf("the fixtures do not reach form %s/%s", r.ID, f)
			}
		}
	}
	for _, f := range upDefaultForms {
		if tags[upTagPrefix+"D/"+f] == 0 {
			t.Errorf("the fixtures do not reach D form %s", f)
		}
	}
	if t.Failed() {
		t.Logf("tags: %v", got)
	}
}
