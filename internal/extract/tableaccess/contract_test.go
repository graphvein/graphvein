package tableaccess

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const (
	docsPath = "../../../docs/extract/table-access.md"
	taskPath = "../../../docs/contract/claims/table-access.md"
)

// tableOrder returns the row ids of a Markdown file's "## Claim contract"
// table in document order.
func tableOrder(t *testing.T, path string) []string {
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

var assumptionID = regexp.MustCompile(`TA-V[0-9]+[a-c]?`)

// TestTableAccessContract is the table-equality half of the contract test
// (T018 plan, shared piece 4): the docs table equals the task file's row
// for row; the code table lists the same 17 rows in the same order with
// the same assumptions; every form a row lists is named in that row; the
// sides are the task's (fourteen claim rows, Q1 and Q2 reached through the
// carriers, D last); every branch the code table names is a function of
// the package; and the reason vocabulary is the docs' list. The tag half
// is TestTableAccessTags: every claim row reached, each listed form.
func TestTableAccessContract(t *testing.T) {
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
	if order := tableOrder(t, docsPath); !reflect.DeepEqual(order, ids) {
		t.Errorf("docs rows %v, code rows %v", order, ids)
	}
	if order := tableOrder(t, taskPath); !reflect.DeepEqual(order, ids) {
		t.Errorf("task rows %v, code rows %v", order, ids)
	}
	if len(Contract) != 17 || Contract[len(Contract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 16 plus D last", len(Contract))
	}
	var source strings.Builder
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		source.Write(b)
	}
	sides := map[string][]string{}
	for _, r := range Contract {
		sides[r.Side] = append(sides[r.Side], r.ID)
		line := docs[r.ID]
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
		for _, f := range r.Forms {
			if !strings.Contains(line, f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if r.Branch != "" && !regexp.MustCompile(`\nfunc (\([^)]*\) )?`+r.Branch+`\(`).MatchString(source.String()) {
			t.Errorf("row %s: branch %s is not a function of the package", r.ID, r.Branch)
		}
		if r.Side == SideClaim && r.Tool == "" {
			t.Errorf("row %s: claim row without a tool", r.ID)
		}
	}
	want := map[string][]string{
		SideClaim:   {"CT-P1", "CT-P2", "CT-P3", "CT-P4", "CT-P5", "CT-R1", "CT-R2", "CT-R3", "CT-Z1", "CT-Z2", "CT-Z3", "CT-Z4", "CT-S1", "CT-S2"},
		SideReader:  {"CT-Q1", "CT-Q2"},
		SideDefault: {"D"},
	}
	if !reflect.DeepEqual(sides, want) {
		t.Errorf("row sides %v, want %v", sides, want)
	}
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	section := text[strings.Index(text, "\n## Unknown records\n"):]
	var listed []string
	for _, m := range regexp.MustCompile("(?m)^- `([a-z_]+)`").FindAllStringSubmatch(section, -1) {
		listed = append(listed, m[1])
	}
	if !reflect.DeepEqual(listed, Reasons) {
		t.Errorf("docs reasons %v, code %v", listed, Reasons)
	}
	section = text[strings.Index(text, "\n## Assumptions\n"):strings.Index(text, "\n## Claim contract\n")]
	listed = nil
	for _, m := range regexp.MustCompile(`(?m)^- \*\*(TA-V[0-9]+[a-c]?)\*\*`).FindAllStringSubmatch(section, -1) {
		listed = append(listed, m[1])
	}
	if !reflect.DeepEqual(listed, Assumptions) {
		t.Errorf("docs assumptions %v, code %v", listed, Assumptions)
	}
}

// TestTableAccessTags is the tag half of the contract test: the link pass
// over the oracle fixtures (sqlx, SQLModel, Prisma, Drizzle) emits a tag
// for every row with a branch and each of its listed forms, no tag outside
// the contract and none for a row without a branch. With every step
// landed, every claim row and D has a branch (checked below).
func TestTableAccessTags(t *testing.T) {
	tags := modeltest.NewTags()
	for _, root := range oracleFixtures {
		in := loadRoot(t, root)
		in.Tag = tags.Tag
		Link(in)
	}
	branches := map[string]string{}
	forms := map[string][]string{}
	for _, r := range Contract {
		branches[r.ID] = r.Branch
		if r.Branch != "" && len(r.Forms) > 0 {
			forms[r.ID] = r.Forms
		}
	}
	modeltest.CheckTags(t, tags, branches, forms)
	for _, r := range Contract {
		if r.Branch == "" && r.Side != SideReader {
			t.Errorf("row %s (%s) has no branch", r.ID, r.Side)
		}
	}
}

// TestTableAccessClaims checks the verification labels (Revision 1, O6):
// a row labelled oracle that is implemented must have every listed form
// reached by the oracle fixtures (oracleFixtures), which the oracles
// compare in full (every claimed fixture site), so oracle labels only forms
// an oracle compared; every implemented carrier row reaches the reader
// (CT-Q1) and its forms through the fixtures. Rows still to land keep their
// label for their unit to prove (H-B5, H-B6 add their fixture oracles).
func TestTableAccessClaims(t *testing.T) {
	tags := modeltest.NewTags()
	var outs []Output
	for _, root := range oracleFixtures {
		in := loadRoot(t, root)
		in.Tag = tags.Tag
		outs = append(outs, Link(in))
	}
	for _, r := range Contract {
		if r.Branch == "" || r.Verification != VerificationOracle {
			continue
		}
		for _, f := range r.Forms {
			if !tags.Forms[r.ID+"/"+f] {
				t.Errorf("row %s is labelled oracle but form %s is not in the oracle fixture", r.ID, f)
			}
		}
	}
	for _, out := range outs {
		for owner, attrs := range out.Attributes {
			list, _ := attrs["table_access"].([]Access)
			for _, a := range list {
				oracle := true
				for _, id := range a.Rows {
					oracle = oracle && rowByID(id).Verification == VerificationOracle
				}
				if (a.Verification == VerificationOracle) != oracle {
					t.Errorf("%s %s %s: verification %s for rows %v", owner, a.Kind, a.Target, a.Verification, a.Rows)
				}
			}
		}
	}
}

// oracleFixtures are the fixtures an oracle compares in full: the sqlx
// fixture (scripts/oracle/sql_tables.sh over its migration sets) and the
// SQLModel fixture (scripts/oracle/sql_tables_sqlalchemy.sh for every
// claimed CT-P1/CT-P2 site, scripts/oracle/sql_tables.sh --model-ddl for
// its CT-P5 sites) and the Prisma fixture (scripts/oracle/sql_tables.sh
// builds its group's schema with prisma migrate diff for its CT-R3 sites;
// its CT-R1 and CT-R2 sites are mapped).
//
// The Drizzle fixture: scripts/oracle/sql_tables.sh builds its group's
// schema with the pinned drizzle-kit export, runs every claimed CT-Z4
// site's SQL and the toSQL() SQL of every claimed CT-Z1/CT-Z2 site
// (scripts/oracle/sql_tables_drizzle.mjs) through the analyzer; its CT-Z3
// sites are mapped.
var oracleFixtures = []string{fixtureRoot, sqlmodelFixtureRoot, prismaFixtureRoot, drizzleFixtureRoot}
