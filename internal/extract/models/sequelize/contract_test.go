package sequelize

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const (
	docsPath = "../../../../docs/extract/schema-sequelize.md"
	taskPath = "../../../../docs/contract/claims/sequelize-models.md"
)

// docsOrder returns the row ids of a Markdown file's "## Claim contract"
// table in document order.
func docsOrder(t *testing.T, path string) []string {
	t.Helper()
	text := readFile(t, path)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatalf("%s: no Claim contract section", path)
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var ids []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| id |") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	return ids
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assumptionEntries returns the "- **SQ-Vn Title.** text" entries of the
// section between start and end, whitespace-normalised, in order.
func assumptionEntries(t *testing.T, path, start, end string) []string {
	t.Helper()
	text := readFile(t, path)
	i, j := strings.Index(text, start), strings.Index(text, end)
	if i < 0 || j < i {
		t.Fatalf("%s: no section %q before %q", path, start, end)
	}
	section := strings.Join(strings.Fields(text[i+len(start):j]), " ")
	var out []string
	for _, part := range strings.Split(section, "- **")[1:] {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

var assumptionID = regexp.MustCompile(`SQ-V[0-9]+`)

// TestSequelizeContract is the table-equality half of the contract test
// (plan shared piece 4): the docs table equals the task file's row for
// row, the code table (contract.go) lists the same rows in the same order
// with the same assumptions, every form a row lists is named in that row,
// the row sides match the task file (eleven claim rows, SQ-P1 refusing
// only, SQ-U1 marking only, SQ-V without a branch, D last), and the docs'
// assumptions equal the task file's environment section. The tag half
// (every row with a branch and every listed form reached by the fixture,
// every tag a row) comes with the claim pass.
func TestSequelizeContract(t *testing.T) {
	docs := modeltest.TableRows(t, docsPath)
	task := modeltest.TableRows(t, taskPath)
	if !reflect.DeepEqual(docs, task) {
		for id, line := range task {
			if docs[id] != line {
				t.Errorf("row %s differs between the task file and the docs:\ntask: %s\ndocs: %s", id, line, docs[id])
			}
		}
		for id := range docs {
			if _, ok := task[id]; !ok {
				t.Errorf("docs row %s is not in the task file", id)
			}
		}
	}
	var ids []string
	for _, r := range Contract {
		ids = append(ids, r.ID)
	}
	if order := docsOrder(t, docsPath); !reflect.DeepEqual(order, ids) {
		t.Errorf("docs rows %v, code rows %v", order, ids)
	}
	if order := docsOrder(t, taskPath); !reflect.DeepEqual(order, ids) {
		t.Errorf("task rows %v, code rows %v", order, ids)
	}
	if len(Contract) != 15 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 14 plus D last", len(Contract))
	}
	sides := map[string][]string{}
	for _, r := range Contract {
		sides[r.Side] = append(sides[r.Side], r.ID)
		line, ok := docs[r.ID]
		if !ok {
			t.Errorf("row %s is not in the docs", r.ID)
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) != 7 {
			t.Errorf("row %s: %d cells, want 5", r.ID, len(cells)-2)
			continue
		}
		var got []string
		if strings.TrimSpace(cells[5]) != "-" {
			got = assumptionID.FindAllString(cells[5], -1)
		}
		if !reflect.DeepEqual(got, r.Assumptions) && !(len(got) == 0 && len(r.Assumptions) == 0) {
			t.Errorf("row %s: docs assumptions %v, code %v", r.ID, got, r.Assumptions)
		}
		for _, a := range r.Assumptions {
			found := false
			for _, b := range Assumptions {
				found = found || a == b
			}
			if !found {
				t.Errorf("row %s: assumption %s is not in Assumptions", r.ID, a)
			}
		}
		for _, f := range r.Forms {
			if !strings.Contains(line, f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if kind := r.Kind(); !regexp.MustCompile(`^(K|M|A|O|R|P|U|V|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
	}
	want := map[string][]string{
		SideClaim:   {"SQ-K1", "SQ-K2", "SQ-M1", "SQ-M2", "SQ-A1", "SQ-A2", "SQ-A3", "SQ-O1", "SQ-O2", "SQ-R1", "SQ-R2"},
		SideRefusal: {"SQ-P1"},
		SideMarker:  {"SQ-U1"},
		SideNone:    {"SQ-V"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	// The assumptions: the docs' section equals the task file's
	// environment section entry for entry, and lists SQ-V1 to SQ-V6.
	docsEnv := assumptionEntries(t, docsPath, "\n## Assumptions\n", "\n## Claim contract\n")
	taskEnv := assumptionEntries(t, taskPath, "\n## Environment (assumptions, part of the contract)\n", "\n## Claim contract\n")
	if !reflect.DeepEqual(docsEnv, taskEnv) {
		t.Errorf("docs assumptions differ from the task file's:\ndocs: %q\ntask: %q", docsEnv, taskEnv)
	}
	var listed []string
	for _, e := range docsEnv {
		if m := regexp.MustCompile(`^(SQ-V[0-9]+) [A-Z][^.]*\.\*\*`).FindStringSubmatch(e); m != nil {
			listed = append(listed, m[1])
		}
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestSequelizeContractTags is the tag half of the contract test, over
// the claim pass on the fixture: every row with a branch is reached
// (every row but SQ-V, which only names the assumptions, and is never
// tagged), every form a row lists is reached and every form tag is
// listed, every tag is a row, and the rows the emitted claims name are
// exactly the claim-side rows the fixture tags.
func TestSequelizeContractTags(t *testing.T) {
	nodes, _, tags := linkFixture(t)
	forms := map[string][]string{}
	for _, r := range Contract {
		switch {
		case r.Side == SideNone && tags.Rows[r.ID]:
			t.Errorf("row %s has no branch but was tagged", r.ID)
		case r.Side != SideNone && !tags.Rows[r.ID]:
			t.Errorf("row %s is never reached by the fixture", r.ID)
		}
		if len(r.Forms) > 0 {
			forms[r.ID] = r.Forms
		}
	}
	for id, list := range forms {
		for _, f := range list {
			if !tags.Forms[id+"/"+f] {
				t.Errorf("row %s: form %q is never reached by the fixture", id, f)
			}
		}
	}
	for f := range tags.Forms {
		row, form, _ := strings.Cut(f, "/")
		found := false
		for _, x := range forms[row] {
			found = found || x == form
		}
		if !found {
			t.Errorf("form tag %s is not listed in its row", f)
		}
	}
	sides := map[string]string{}
	for _, r := range Contract {
		sides[r.ID] = r.Side
	}
	for row := range tags.Rows {
		if _, ok := sides[row]; !ok {
			t.Errorf("tag %s is not a contract row", row)
		}
	}
	claimed := map[string]bool{}
	for _, n := range nodes {
		list, _ := n.Attributes["claims"].([]models.Claim)
		for _, c := range list {
			claimed[c.Row] = true
			if sides[c.Row] != SideClaim {
				t.Errorf("%s: claim %s by row %s, which is not a claim-side row", n.Identity, c.Fact, c.Row)
			}
			if !tags.Rows[c.Row] {
				t.Errorf("%s: claim %s by row %s, which no branch tagged", n.Identity, c.Fact, c.Row)
			}
		}
	}
	// SQ-K1 and SQ-M1/M2 claim tables and groups (records without a
	// claims list), every other claim-side row claims column facts.
	for _, r := range Contract {
		if r.Side != SideClaim || r.ID == "SQ-K1" || r.ID == "SQ-M1" || r.ID == "SQ-M2" {
			continue
		}
		if !claimed[r.ID] {
			t.Errorf("claim row %s is tagged but no emitted claim names it", r.ID)
		}
	}
}

// TestSequelizeCatalogue checks the registered catalogue against the docs:
// every row's claim cell as written, its assumptions, a branch that is a
// function of the package ("none" exactly for the rows without one), and
// the assumptions' titles and texts.
func TestSequelizeCatalogue(t *testing.T) {
	docs := modeltest.TableRows(t, docsPath)
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		source.Write(b)
	}
	registered := false
	for _, c := range models.Catalogues() {
		registered = registered || c.Tool == Tool
	}
	if !registered {
		t.Error("the Sequelize catalogue is not registered")
	}
	c := Catalogue()
	if len(c.Rows) != len(Contract) {
		t.Fatalf("%d catalogue rows, %d contract rows", len(c.Rows), len(Contract))
	}
	for i, e := range c.Rows {
		r := Contract[i]
		cells := strings.Split(docs[r.ID], "|")
		if e.ID != r.ID || len(cells) < 4 || strings.TrimSpace(cells[3]) != e.Summary {
			t.Errorf("%s: summary %q, docs %q", r.ID, e.Summary, docs[r.ID])
		}
		if e.Assumptions == nil || !reflect.DeepEqual(e.Assumptions, append([]string{}, r.Assumptions...)) {
			t.Errorf("%s: assumptions %v, contract %v", r.ID, e.Assumptions, r.Assumptions)
		}
		switch {
		case (e.Branch == "none") != (r.Side == SideNone):
			t.Errorf("%s: branch %q for a row of side %s", r.ID, e.Branch, r.Side)
		case e.Branch != "none" && !regexp.MustCompile(`\nfunc (\([^)]*\) )?`+e.Branch+`\(`).MatchString(source.String()):
			t.Errorf("%s: branch %s is not a function of the package", r.ID, e.Branch)
		}
	}
	text := string(data)
	section := text[strings.Index(text, "\n## Assumptions\n"):strings.Index(text, "\n## Claim contract\n")]
	section = strings.Join(strings.Fields(section), " ")
	if len(c.Assumptions) != len(Assumptions) {
		t.Errorf("%d catalogue assumptions, want %d", len(c.Assumptions), len(Assumptions))
	}
	for i, a := range c.Assumptions {
		if a.ID != Assumptions[i] || !strings.Contains(section, "- **"+a.ID+" "+a.Title+".** "+a.Text) {
			t.Errorf("assumption %s %q: %q is not the docs' entry", a.ID, a.Title, a.Text)
		}
	}
}

// TestSequelizeFailureKinds keeps SQ-P1's refusal column and the code's
// failure kinds 1:1: the numbered items of the refusal
// cell are exactly failureKinds' texts in order, and every kind is used by
// a failure call of the package.
func TestSequelizeFailureKinds(t *testing.T) {
	docs := modeltest.TableRows(t, docsPath)
	cells := strings.Split(docs["SQ-P1"], " | ")
	if len(cells) < 4 {
		t.Fatalf("SQ-P1 row: %q", docs["SQ-P1"])
	}
	item := regexp.MustCompile(`\(([0-9]+)\) ([^;]+)`)
	var got []string
	for i, m := range item.FindAllStringSubmatch(cells[3], -1) {
		if m[1] != strconv.Itoa(i+1) {
			t.Errorf("item %s is number %d", m[1], i+1)
		}
		got = append(got, strings.TrimSpace(m[2]))
	}
	var want []string
	for _, k := range failureKinds {
		want = append(want, k.info.text)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SQ-P1 refusal items:\n got %q\nwant %q", got, want)
	}
	var source strings.Builder
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "failures.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		source.Write(b)
	}
	names := regexp.MustCompile(`\b(fail[A-Z][A-Za-z]*)\b`).FindAllStringSubmatch(readFile(t, "failures.go"), -1)
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n[1]] || n[1] == "failureKind" || n[1] == "failureKinds" || n[1] == "failureInfo" {
			continue
		}
		seen[n[1]] = true
		if !strings.Contains(source.String(), n[1]) {
			t.Errorf("failure kind %s is never used", n[1])
		}
	}
	if len(seen) != len(failureKinds) {
		t.Errorf("%d kind constants, %d failureKinds entries", len(seen), len(failureKinds))
	}
}
