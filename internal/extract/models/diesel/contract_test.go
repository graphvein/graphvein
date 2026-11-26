package diesel

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
	docsPath = "../../../../docs/extract/schema-diesel.md"
	taskPath = "../../../../docs/contract/claims/diesel-models.md"
)

// docsOrder returns the row ids of a Markdown file's "## Claim contract"
// table in document order.
func docsOrder(t *testing.T, path string) []string {
	t.Helper()
	text := readDoc(t, path)
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

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assumptionEntries returns the "- **DI-Vn Title.** text" entries of the
// section between start and end, whitespace-normalised, in order.
func assumptionEntries(t *testing.T, path, start, end string) []string {
	t.Helper()
	text := readDoc(t, path)
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

var assumptionID = regexp.MustCompile(`DI-V[0-9]+`)

// TestDieselContract is the table-equality half of the contract test
// (plan shared piece 4): the docs table equals the task file's row for
// row, the code table (contract.go) lists the same rows in the same order
// with the same assumptions, every form a row lists is named in that row,
// the row sides match the task file (seven claim rows, DI-G1 selecting,
// DI-J2 without a fact, DI-P1 refusing only, DI-U1 marking only, DI-V
// without a branch, D last), and the docs' assumptions equal the task
// file's environment section. The tag half (every row with a branch and
// every listed form reached by the fixture, every tag a row) comes with
// the claim pass.
func TestDieselContract(t *testing.T) {
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
	if len(Contract) != 13 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 12 plus D last", len(Contract))
	}
	known := map[string]bool{}
	for _, a := range Assumptions {
		known[a] = true
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
			if !known[a] {
				t.Errorf("row %s: assumption %s is not in Assumptions", r.ID, a)
			}
		}
		for _, f := range r.Forms {
			if !strings.Contains(line, "`"+f+"`") {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if kind := r.Kind(); !regexp.MustCompile(`^(K|G|T|C|S|J|P|U|V|D)$`).MatchString(kind) {
			t.Errorf("row %s: kind %q", r.ID, kind)
		}
	}
	want := map[string][]string{
		SideClaim:   {"DI-K1", "DI-T1", "DI-C1", "DI-C2", "DI-S1", "DI-J1", "DI-U2"},
		SideSelect:  {"DI-G1"},
		SideNoFact:  {"DI-J2"},
		SideRefusal: {"DI-P1"},
		SideMarker:  {"DI-U1"},
		SideNone:    {"DI-V"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	// The DI-V row names the whole assumption range.
	if v := docs["DI-V"]; !strings.Contains(v, "DI-V1..DI-V"+strings.TrimPrefix(Assumptions[len(Assumptions)-1], "DI-V")+" ") {
		t.Errorf("row DI-V does not name DI-V1..%s: %s", Assumptions[len(Assumptions)-1], v)
	}
	// The assumptions: the docs' section equals the task file's
	// environment section entry for entry, and lists DI-V1 to DI-V7.
	docsEnv := assumptionEntries(t, docsPath, "\n## Assumptions\n", "\n## Claim contract\n")
	taskEnv := assumptionEntries(t, taskPath, "\n## Environment (assumptions, part of the contract)\n", "\n## Claim contract\n")
	if !reflect.DeepEqual(docsEnv, taskEnv) {
		t.Errorf("docs assumptions differ from the task file's:\ndocs: %q\ntask: %q", docsEnv, taskEnv)
	}
	var listed []string
	for _, e := range docsEnv {
		if m := regexp.MustCompile(`^(DI-V[0-9]+) [^.]*\.\*\*`).FindStringSubmatch(e); m != nil {
			listed = append(listed, m[1])
		}
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestFailureFormsAreTagged keeps DI-P1's forms and the item reader's
// failure kinds 1:1: every failure ReadFile tags is a listed form.
func TestFailureFormsAreTagged(t *testing.T) {
	tags := modeltest.NewTags()
	src := "diesel::table! {\n    t (id) {\n        id -> Int4,\n        id -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    u (x) {\n        y -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    v {\n        y -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    w (id) {\n        id -> Int4,\n        w -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    z (id) {\n        id -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    z (id) {\n        id -> Int4,\n    }\n}\n" +
		"diesel::table! {\n    s (id) {\n        id Int4,\n    }\n}\n"
	ReadFile(Input{Tag: tags.Tag}, "src/schema.rs", []byte(src))
	listed := map[string]bool{}
	for _, f := range FailureForms {
		listed["DI-P1/"+f] = true
	}
	for f := range tags.Forms {
		if !listed[f] {
			t.Errorf("tag %s is not a DI-P1 form", f)
		}
	}
	for _, f := range []string{FailDuplicateCol, FailKeyNotColumn, FailNoKey, FailColumnLikeName, FailDuplicateTable, FailSyntax} {
		if !tags.Forms["DI-P1/"+f] {
			t.Errorf("form %s was not tagged", f)
		}
	}
}

// TestDieselCatalogue checks the registered catalogue against the docs
// (Revision 1 addendum 3): every row's claim cell as written, its
// assumptions, a branch that is a function of the package ("none" exactly
// for the row without one), and each assumption's title and text, a
// leading part of its docs entry.
func TestDieselCatalogue(t *testing.T) {
	docs := modeltest.TableRows(t, docsPath)
	var source strings.Builder
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		source.Write(b)
	}
	registered := false
	for _, c := range models.Catalogues() {
		registered = registered || c.Tool == Tool
	}
	if !registered {
		t.Error("the Diesel catalogue is not registered")
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
	entries := assumptionEntries(t, docsPath, "\n## Assumptions\n", "\n## Claim contract\n")
	if len(c.Assumptions) != len(Assumptions) || len(entries) != len(Assumptions) {
		t.Fatalf("%d catalogue assumptions, %d docs entries, want %d", len(c.Assumptions), len(entries), len(Assumptions))
	}
	for i, a := range c.Assumptions {
		if a.ID != Assumptions[i] || !strings.HasPrefix(entries[i], a.ID+" "+a.Title+".** "+strings.TrimSuffix(a.Text, ".")) {
			t.Errorf("assumption %s %q: %q does not begin its docs entry %q", a.ID, a.Title, a.Text, entries[i])
		}
		// DI-V9 names the older releases checked (Revision 2, A1).
		if a.ID == AssumeOlderPin && !strings.Contains(a.Text, "at "+strings.Join(OlderCLIChecked[:len(OlderCLIChecked)-1], ", ")+" and "+OlderCLIChecked[len(OlderCLIChecked)-1]) {
			t.Errorf("DI-V9 does not name the checked releases %v: %q", OlderCLIChecked, a.Text)
		}
		// DI-V8 names the verified diesel_cli versions (addendum 4).
		if a.ID == AssumeNewerPin && !strings.HasPrefix(a.Text, "Rules verified at diesel_cli "+strings.Join(CLIVerified, " and ")+":") {
			t.Errorf("DI-V8 does not name the verified versions %v: %q", CLIVerified, a.Text)
		}
	}
}
