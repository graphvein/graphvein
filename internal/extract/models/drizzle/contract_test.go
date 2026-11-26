package drizzle

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const (
	docsPath = "../../../../docs/extract/schema-drizzle.md"
	taskPath = "../../../../docs/contract/claims/drizzle-models.md"
)

// docsOrder returns the row ids of a Markdown file's "## Claim contract"
// table in document order.
func docsOrder(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
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

var assumptionID = regexp.MustCompile(`DR-V[0-9]+`)

// TestDrizzleContract is the table-equality half of the contract test (plan
// shared piece 4): the docs table equals the task file's row for row, the
// code table (contract.go) lists the same rows in the same order with the
// same assumptions, every form a row lists is named in that row, and the
// row sides match the task file (ten claim rows, DR-P1 refusing only,
// DR-P2 and DR-V without a branch, D last). The tag half (every row with
// a branch and every listed form reached by the fixture, every tag a row)
// is TestDrizzleFixture.
func TestDrizzleContract(t *testing.T) {
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
	if len(Contract) != 14 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 13 plus D last", len(Contract))
	}
	sides := map[string][]string{}
	for _, r := range Contract {
		sides[r.Side] = append(sides[r.Side], r.ID)
		line, ok := docs[r.ID]
		if !ok {
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
			if !containsString(Assumptions, a) {
				t.Errorf("row %s: assumption %s is not in Assumptions", r.ID, a)
			}
		}
		for _, f := range r.Forms {
			if !strings.Contains(line, f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if kind := r.Kind(); !regexp.MustCompile(`^(K|T|C|X|E|R|P|V|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
	}
	want := map[string][]string{
		SideClaim:   {"DR-K1", "DR-K2", "DR-T1", "DR-C1", "DR-C2", "DR-C3", "DR-K3", "DR-X1", "DR-E1", "DR-R1"},
		SideRefusal: {"DR-P1"},
		SideNone:    {"DR-P2", "DR-V"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	// The assumptions section lists DR-V1 to DR-V6, each with a title.
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	section := text[strings.Index(text, "\n## Assumptions\n"):strings.Index(text, "\n## Claim contract\n")]
	var listed []string
	for _, m := range regexp.MustCompile(`(?m)^- \*\*(DR-V[0-9]+) [A-Z][^.]*\.\*\*`).FindAllStringSubmatch(section, -1) {
		listed = append(listed, m[1])
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestDrizzleCatalogue checks the registered catalogue against the docs:
// every row's claim cell as written, its assumptions, a branch that is a
// function of the package ("none" exactly for the rows without one), and
// the assumptions' titles and texts.
func TestDrizzleCatalogue(t *testing.T) {
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
		t.Error("the Drizzle catalogue is not registered")
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
