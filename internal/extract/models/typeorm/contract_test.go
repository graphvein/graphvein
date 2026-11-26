package typeorm

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
	docsPath = "../../../../docs/extract/schema-typeorm.md"
	taskPath = "../../../../docs/contract/claims/typeorm-models.md"
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

// assumptionEntries returns the "- **TO-Vn Title.** text" entries of the
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

var assumptionID = regexp.MustCompile(`TO-V[0-9]+`)

// TestTypeORMContract is the table-equality half of the contract test
// (plan shared piece 4): the docs table equals the task file's row for
// row, the code table (contract.go) lists the same rows in the same order
// with the same assumptions, every form a row lists is named in that row,
// the row sides match the task file (twenty-three claim rows, TO-E3 and
// TO-R3 without a table fact, TO-P1 and TO-P2 refusing only, TO-U1
// marking only, TO-V without a branch, D last), and the docs' assumptions
// equal the task file's environment section. The tag half (every row
// with a branch and every listed form reached by the fixture, every tag a
// row) comes with the claim pass.
func TestTypeORMContract(t *testing.T) {
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
	if len(Contract) != 30 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 29 plus D last", len(Contract))
	}
	sides := map[string][]string{}
	for _, r := range Contract {
		sides[r.Side] = append(sides[r.Side], r.ID)
		line, ok := docs[r.ID]
		if !ok {
			t.Errorf("row %s is not in the docs", r.ID)
			continue
		}
		cells := strings.Split(strings.ReplaceAll(line, `\|`, "\x00"), "|")
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
		if kind := r.Kind(); !regexp.MustCompile(`^(K|E|C|X|R|P|U|V|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
	}
	want := map[string][]string{
		SideClaim: {"TO-K1", "TO-K2", "TO-K3", "TO-K4", "TO-K5", "TO-K6", "TO-K7", "TO-K8", "TO-E1", "TO-E2",
			"TO-C1", "TO-C2", "TO-C3", "TO-C4", "TO-C5", "TO-C6", "TO-X1", "TO-X2", "TO-X3", "TO-X4", "TO-R1", "TO-R2", "TO-R4"},
		SideNoFact:  {"TO-E3", "TO-R3"},
		SideRefusal: {"TO-P1", "TO-P2"},
		SideMarker:  {"TO-U1"},
		SideNone:    {"TO-V"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	// The task file names the twenty-three claim-side rows; the code
	// agrees.
	task23 := "The claim-side rows are TO-K1, K2,\nK3, K4, K5, K6, K7, K8, E1, E2, C1, C2, C3, C4, C5, C6, X1, X2, X3, X4,\nR1, R2, R4 (twenty-three"
	if !strings.Contains(readFile(t, taskPath), task23) {
		t.Error("the task file's list of claim-side rows changed")
	}
	// The assumptions: the docs' section equals the task file's
	// environment section entry for entry, and lists TO-V1 to TO-V10 in
	// the file's order.
	docsEnv := assumptionEntries(t, docsPath, "\n## Assumptions\n", "\n## Claim contract\n")
	taskEnv := assumptionEntries(t, taskPath, "\n## Environment (assumptions, part of the contract)\n", "\n## Claim contract\n")
	if !reflect.DeepEqual(docsEnv, taskEnv) {
		t.Errorf("docs assumptions differ from the task file's:\ndocs: %q\ntask: %q", docsEnv, taskEnv)
	}
	var listed []string
	for _, e := range docsEnv {
		if m := regexp.MustCompile(`^(TO-V[0-9]+) [A-Z][^*]*\*\*`).FindStringSubmatch(e); m != nil {
			listed = append(listed, m[1])
		}
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestTypeORMCatalogue checks the /schema/assumptions catalogue against
// the document: it is registered, lists every contract row in
// order with its claim cell and assumptions and a branch that is a
// function of the package ("none" only for TO-V), and every assumption's
// title and text as the docs' entry states them.
func TestTypeORMCatalogue(t *testing.T) {
	docs := modeltest.TableRows(t, docsPath)
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
		t.Error("the TypeORM catalogue is not registered")
	}
	c := Catalogue()
	// Every row is served, D included (the server keys rows per tool).
	if len(c.Rows) != len(Contract) || Contract[len(Contract)-1].ID != "D" {
		t.Fatalf("%d catalogue rows, %d contract rows", len(c.Rows), len(Contract))
	}
	if d := catalogueRows["D"]; !strings.Contains(docs["D"], d.Summary) || !regexp.MustCompile(`\nfunc (\([^)]*\) )?`+d.Branch+`\(`).MatchString(source.String()) {
		t.Errorf("D: entry %+v does not match the docs row %q or names no function", d, docs["D"])
	}
	for i, e := range c.Rows {
		r := Contract[i]
		cells := strings.Split(strings.ReplaceAll(docs[r.ID], `\|`, "\x00"), "|")
		if e.ID != r.ID || len(cells) < 4 || strings.ReplaceAll(strings.TrimSpace(cells[3]), "\x00", `\|`) != e.Summary {
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
	section := readFile(t, docsPath)
	section = section[strings.Index(section, "\n## Assumptions\n"):strings.Index(section, "\n## Claim contract\n")]
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
