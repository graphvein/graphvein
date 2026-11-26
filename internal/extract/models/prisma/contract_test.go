package prisma

import (
	"fmt"
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
	docsPath = "../../../../docs/extract/schema-prisma.md"
	taskPath = "../../../../docs/contract/claims/prisma-models.md"
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

var assumptionID = regexp.MustCompile(`PR-V[0-9]+`)

// TestPrismaContract is the table-equality half of the contract test (plan
// shared piece 4): the docs table equals the task file's row for row, the
// code table (contract.go) lists the same rows in the same order with the
// same assumptions, and every form a row lists is named in that row. The
// tag half (every claim branch reached by the fixture, every tag a row)
// comes with the claim branches.
func TestPrismaContract(t *testing.T) {
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
	if len(Contract) != 26 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 25 plus D last", len(Contract))
	}
	claims := 0
	for _, r := range Contract {
		line, ok := docs[r.ID]
		if !ok {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) != 7 {
			t.Errorf("row %s: %d cells, want 5", r.ID, len(cells)-2)
			continue
		}
		got := assumptionID.FindAllString(cells[5], -1)
		if strings.TrimSpace(cells[5]) == "-" {
			got = nil
		}
		if !reflect.DeepEqual(got, r.Assumptions) && !(len(got) == 0 && len(r.Assumptions) == 0) {
			t.Errorf("row %s: docs assumptions %v, code %v", r.ID, got, r.Assumptions)
		}
		for _, a := range r.Assumptions {
			if !contains(Assumptions, a) {
				t.Errorf("row %s: assumption %s is not in Assumptions", r.ID, a)
			}
		}
		for _, f := range r.Forms {
			if !strings.Contains(line, f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if kind := r.Kind(); !regexp.MustCompile(`^(B|F|A|AA|R|E|P|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
		if r.Claims {
			claims++
		}
	}
	// 25 rows plus D; PR-B2, PR-B4, PR-A4 and PR-R2 have no claim side.
	if claims != 21 {
		t.Errorf("%d rows with a claim side, want 21", claims)
	}
	for _, id := range []string{"PR-B2", "PR-B4", "PR-A4", "PR-R2", "D"} {
		for _, r := range Contract {
			if r.ID == id && r.Claims {
				t.Errorf("row %s has a claim side in the code table", id)
			}
		}
	}
}

// TestPrismaCatalogue checks the catalogue registered for
// /api/v0/schema/assumptions against the docs: every contract row with its
// claim cell as written and its row's assumptions, a branch that is a
// function of this package ("none" exactly for the rows without a claim
// side), and the assumptions PR-V1 to PR-V6 with the docs' title and text.
func TestPrismaCatalogue(t *testing.T) {
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
		t.Error("the Prisma catalogue is not registered")
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
		if e.Assumptions == nil || len(e.Assumptions) != len(r.Assumptions) || len(r.Assumptions) > 0 && !reflect.DeepEqual(e.Assumptions, r.Assumptions) {
			t.Errorf("%s: assumptions %v, contract %v", r.ID, e.Assumptions, r.Assumptions)
		}
		switch {
		case (e.Branch == "none") != !r.Claims:
			t.Errorf("%s: branch %q for a row with claim side %v", r.ID, e.Branch, r.Claims)
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

// TestClaimGates checks that every version gate of the claim pass
// (schema.since) is listed in claimGates, so sharedSchemas compares two
// packages' versions on every gate that decides a fact.
func TestClaimGates(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	call := regexp.MustCompile(`\.since\((\d+), (\d+)\)`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllStringSubmatch(string(data), -1) {
			n++
			listed := false
			for _, g := range claimGates {
				listed = listed || fmt.Sprint(g[0]) == m[1] && fmt.Sprint(g[1]) == m[2]
			}
			if !listed {
				t.Errorf("%s: gate since(%s, %s) is not in claimGates", f, m[1], m[2])
			}
		}
	}
	if n == 0 {
		t.Error("no since call found")
	}
}
