package supabasetypes

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const (
	taskPath = "../../../../docs/contract/claims/nextjs-supabase.md"
	docsPath = "../../../../docs/extract/schema-supabase-types.md"
)

// sbgRows returns the rows of the SB-G table of a Markdown file: the lines
// from `| SB-G1 |` to the end of that table.
func sbgRows(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| SB-G1 |") {
			in = true
		}
		if in {
			if !strings.HasPrefix(line, "| ") {
				break
			}
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("%s: no SB-G table", path)
	}
	return rows
}

var assumptionID = regexp.MustCompile(`SB-V[0-9]+`)

// TestSupabaseTypesContract keeps the task's SB-G table, the docs copy and
// Contract 1:1: the same rows in the same order, the same assumptions,
// every listed form named in its row, every branch a function of this
// package, and SB-V6's text the docs' entry.
func TestSupabaseTypesContract(t *testing.T) {
	task, docs := sbgRows(t, taskPath), sbgRows(t, docsPath)
	if !reflect.DeepEqual(task, docs) {
		t.Errorf("the docs' SB-G rows differ from the task's:\ntask: %q\ndocs: %q", task, docs)
	}
	if len(task) != len(Contract) {
		t.Fatalf("task has %d rows, Contract %d", len(task), len(Contract))
	}
	var code strings.Builder
	for _, f := range []string{"link.go", "compare.go", "parse.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		code.Write(data)
	}
	for i, r := range Contract {
		cells := strings.Split(strings.ReplaceAll(task[i], `\|`, "\x00"), "|")
		if len(cells) != 7 {
			t.Errorf("row %d: %d cells, want 5", i, len(cells)-2)
			continue
		}
		if id := strings.TrimSpace(cells[1]); id != r.ID {
			t.Errorf("row %d is %s in the task, %s in the code", i, id, r.ID)
		}
		var got []string
		if strings.TrimSpace(cells[5]) != "-" {
			got = assumptionID.FindAllString(cells[5], -1)
		}
		if len(got) != len(r.Assumptions) || len(got) > 0 && !reflect.DeepEqual(got, r.Assumptions) {
			t.Errorf("row %s: task assumptions %v, code %v", r.ID, got, r.Assumptions)
		}
		for _, f := range r.Forms {
			if !strings.Contains(task[i], f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if r.Branch != "" && !regexp.MustCompile(`\nfunc (\([^)]*\) )?`+r.Branch+`\(`).MatchString(code.String()) {
			t.Errorf("row %s: branch %s is not a function of the package", r.ID, r.Branch)
		}
	}
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- **"+Assumption.ID+"** "+Assumption.Text) {
		t.Errorf("%s: %q is not the docs' entry", Assumption.ID, Assumption.Text)
	}
	if c := Catalogue(); c.Tool != Tool || len(c.Rows) != len(Contract) {
		t.Errorf("catalogue %+v", c)
	}
}
