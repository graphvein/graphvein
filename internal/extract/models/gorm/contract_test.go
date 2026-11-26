package gorm

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
	docsPath = "../../../../docs/extract/schema-gorm.md"
	taskPath = "../../../../docs/contract/claims/gorm-models.md"
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

// assumptionEntries returns the "- **GO-Vn Title.** text" entries of the
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

var assumptionID = regexp.MustCompile(`GO-V[0-9]+`)

// TestGormContract is the table-equality half of the contract test (plan
// shared piece 4): the docs table equals the task file's row for row, the
// code table (contract.go) lists the same rows in the same order with the
// same assumptions, every form a row lists is named in that row, the row
// sides match the task file (seventeen claim rows, GO-M2 and GO-U1 marking
// only, GO-P1 refusing only, GO-V without a branch, D last), and the docs'
// assumptions equal the task file's environment section. The tag half
// over the fixture's claim pass is TestGormClaimTags (the full
// per-form coverage comes with step GB4).
func TestGormContract(t *testing.T) {
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
	if len(Contract) != 22 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 21 plus D last", len(Contract))
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
		if kind := r.Kind(); !regexp.MustCompile(`^(K|M|F|T|C|I|R|P|U|V|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
	}
	want := map[string][]string{
		SideClaim:   {"GO-K1", "GO-K2", "GO-K3", "GO-K4", "GO-M1", "GO-F1", "GO-F2", "GO-T1", "GO-T2", "GO-C1", "GO-C2", "GO-C3", "GO-C4", "GO-I1", "GO-R1", "GO-R2", "GO-R3"},
		SideRefusal: {"GO-P1"},
		SideMarker:  {"GO-M2", "GO-U1"},
		SideNone:    {"GO-V"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	// The assumptions: the docs' section equals the task file's
	// environment section entry for entry, and lists GO-V1 to GO-V7.
	docsEnv := assumptionEntries(t, docsPath, "\n## Assumptions\n", "\n## Claim contract\n")
	taskEnv := assumptionEntries(t, taskPath, "\n## Environment (assumptions, part of the contract)\n", "\n## Claim contract\n")
	if !reflect.DeepEqual(docsEnv, taskEnv) {
		t.Errorf("docs assumptions differ from the task file's:\ndocs: %q\ntask: %q", docsEnv, taskEnv)
	}
	var listed []string
	for _, e := range docsEnv {
		if m := regexp.MustCompile(`^(GO-V[0-9]+) [A-Z][^.]*\.\*\*`).FindStringSubmatch(e); m != nil {
			listed = append(listed, m[1])
		}
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestGormDiscoveryTags checks the tags discovery emits over the inline
// modules of the discovery tests: every tag is a contract row, every form
// tag a listed form of its row, and every listed form of the rows
// discovery decides (GO-K2, GO-K3) is reached.
func TestGormDiscoveryTags(t *testing.T) {
	tags := modeltest.NewTags()
	for _, src := range []map[string]string{
		gotifyLike, realworldLike,
		module("func f(db *gorm.DB) {\n\tdb.Migrator().AutoMigrate(A{})\n\tdb.Migrator().CreateTable(new(B))\n}\n"),
		module("func f() {\n\tgorm.Open(sqlite.Open(\"x\"), &gorm.Config{NamingStrategy: schema.NamingStrategy{}, IgnoreRelationshipsWhenMigrating: true})\n}\n"),
		module("func f() {\n\tgorm.Open(sqlite.Open(\"x\"))\n}\n"),
		module("func f(x bool) {\n\tvar d gorm.Dialector\n\tif x {\n\t\td = sqlite.Open(\"a\")\n\t}\n\tgorm.Open(d, &gorm.Config{})\n}\n"),
		{"go.mod": goMod("v1.31.2", "\tgithub.com/glebarez/sqlite v1.11.0\n\tgorm.io/driver/mysql v1.6.0\n"), "db.go": "package db\n\nimport (\n\tglebarez \"github.com/glebarez/sqlite\"\n\t\"gorm.io/driver/mysql\"\n\t\"gorm.io/gorm\"\n)\n\nfunc f() {\n\tgorm.Open(glebarez.Open(\"x\"), &gorm.Config{})\n\tgorm.Open(mysql.New(mysql.Config{}), &gorm.Config{})\n}\n"},
	} {
		in := input(t, src)
		in.Tag = tags.Tag
		Discover(in)
	}
	forms := map[string][]string{}
	rows := map[string]bool{}
	for _, r := range Contract {
		rows[r.ID] = true
		forms[r.ID] = r.Forms
	}
	for row := range tags.Rows {
		if !rows[row] {
			t.Errorf("tag %s is not a contract row", row)
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
	for _, row := range []string{"GO-K2", "GO-K3"} {
		for _, f := range forms[row] {
			if !tags.Forms[row+"/"+f] {
				t.Errorf("row %s: form %q is never reached", row, f)
			}
		}
	}
}

// TestGormCatalogue checks the /api/v0/schema/assumptions catalogue: it is
// registered, its rows are the contract's in order with the docs table's
// claim cell, the row's assumptions and a branch function of the package,
// and its assumptions are the docs' entries.
func TestGormCatalogue(t *testing.T) {
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
		t.Error("the GORM catalogue is not registered")
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
