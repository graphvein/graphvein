package routes

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	goTaskPath  = "../../../docs/contract/claims/go-http-routers.md"
	goDocsPath  = "../../../docs/extract/routes.md"
	goFixture   = "../../../testdata/fixtures/go-routes"
	goTaskTable = "\n## Claim contract (draft)\n"
	goDocsTable = "\n### Go claim contract\n"
)

// goImplemented are the implementation steps whose rows the linker implements.
var goImplemented = map[string]bool{"GR2": true, "GR3": true, "GR4": true, "GR5": true, "GR6": true, "GR7": true}

// goTable reads the first Markdown table after heading in path: the row
// ids in order and each row's line.
func goTable(t *testing.T, path, heading string) ([]string, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, heading)
	if i < 0 {
		t.Fatalf("%s: no %q", path, strings.TrimSpace(heading))
	}
	var ids []string
	rows := map[string]string{}
	started := false
	for _, line := range strings.Split(text[i+len(heading):], "\n") {
		if !strings.HasPrefix(line, "|") {
			if started {
				break
			}
			continue
		}
		started = true
		if strings.HasPrefix(line, "| id |") || strings.HasPrefix(line, "|---") {
			continue
		}
		id := strings.TrimSpace(strings.Split(line, "|")[1])
		if _, dup := rows[id]; dup {
			t.Fatalf("%s: duplicate row %s", path, id)
		}
		ids = append(ids, id)
		rows[id] = line
	}
	return ids, rows
}

// goDForms reads the D forms listed after marker in path, in order.
func goDForms(t *testing.T, path, marker, end string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("%s: no %q", path, marker)
	}
	section := text[i+len(marker):]
	section = section[:strings.Index(section, end)]
	var out []string
	for _, m := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(section, -1) {
		if m[1] != "go_routes_unread" {
			out = append(out, m[1])
		}
	}
	return out
}

// TestGoRoutesContract is the table half of the Go route contract test:
// the docs table equals the task file's row for row, the code table lists
// the same rows in the same order, every listed form is named in its row,
// the D forms of the code, the task and the docs agree, and every static
// tag in the linker's source names a contract row (one tagged branch per
// claim row of the implemented steps).
func TestGoRoutesContract(t *testing.T) {
	taskIDs, task := goTable(t, goTaskPath, goTaskTable)
	docsIDs, docs := goTable(t, goDocsPath, goDocsTable)
	if !reflect.DeepEqual(task, docs) || !reflect.DeepEqual(taskIDs, docsIDs) {
		for _, id := range taskIDs {
			if docs[id] != task[id] {
				t.Errorf("row %s differs:\ntask: %s\ndocs: %s", id, task[id], docs[id])
			}
		}
		t.Errorf("task rows %v, docs rows %v", taskIDs, docsIDs)
	}
	var ids []string
	for _, r := range goContract {
		ids = append(ids, r.ID)
		for _, f := range r.Forms {
			if !strings.Contains(task[r.ID], f) {
				t.Errorf("row %s: form %q is not named in the row", r.ID, f)
			}
		}
		if r.Side == goSideClaim && r.Unit == "" {
			t.Errorf("row %s: no unit", r.ID)
		}
	}
	if !reflect.DeepEqual(ids, taskIDs) {
		t.Errorf("code rows %v, task rows %v", ids, taskIDs)
	}
	if len(goContract) != 35 || goContract[len(goContract)-1].ID != "D" {
		t.Errorf("the contract has %d rows, want 34 plus D last", len(goContract))
	}
	var forms []string
	for _, f := range goUnreadForms {
		forms = append(forms, f.Form)
	}
	if got := goDForms(t, goTaskPath, "D forms (counted):", "Not modelled"); !reflect.DeepEqual(got, forms) {
		t.Errorf("task D forms %v, code %v", got, forms)
	}
	if got := goDForms(t, goDocsPath, "D forms (counted in one info diagnostic", "A refused"); !reflect.DeepEqual(got, forms) {
		t.Errorf("docs D forms %v, code %v", got, forms)
	}
	// Every static tag in the linker names a row; every claim row of an
	// implemented unit has one.
	rows := map[string]goRow{}
	for _, r := range goContract {
		rows[r.ID] = r
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
		for _, m := range regexp.MustCompile(`l\.tag\("((GR-[A-Z][0-9]+)|D)[/"]`).FindAllStringSubmatch(string(data), -1) {
			if _, ok := rows[m[1]]; !ok {
				t.Errorf("%s: tag %s is not a contract row", f, m[1])
			}
			tagged[m[1]] = true
		}
	}
	for _, r := range goContract {
		if r.Side == goSideClaim && goImplemented[r.Unit] && !tagged[r.ID] {
			t.Errorf("claim row %s has no tagged branch", r.ID)
		}
		if r.Side == goSideNone && tagged[r.ID] {
			t.Errorf("row %s has no branch but is tagged", r.ID)
		}
	}
}

// goFixtureSources reads the go-routes fixture.
func goFixtureSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(goFixture, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(goFixture, p)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGoRoutesContractTags is the tag half: linking the fixture (and the
// template cases of TestGoTemplate) reaches every row of the steps
// implemented so far, every listed form of those rows and every D form
// those steps count; every tag names a row, a listed form or a D form.
func TestGoRoutesContractTags(t *testing.T) {
	tags := map[string]int{}
	goLink(t, goFixtureSources(t), tags)
	l := &goLinker{tag: func(row string) { tags[row]++ }}
	for _, c := range goTemplateCases {
		l.template(c.framework, c.raw)
	}
	rows := map[string]goRow{}
	for _, r := range goContract {
		rows[r.ID] = r
	}
	forms := map[string]bool{}
	for _, f := range goUnreadForms {
		forms[f.Form] = true
	}
	var got []string
	for tag := range tags {
		if strings.HasPrefix(tag, upTagPrefix) {
			// The unknown-prefix contract's tags
			// (TestUnknownPrefixContractTags).
			continue
		}
		got = append(got, tag)
		row, form, hasForm := strings.Cut(tag, "/")
		r, ok := rows[row]
		if !ok {
			t.Errorf("tag %s is not a contract row", tag)
			continue
		}
		if !hasForm {
			continue
		}
		if row == "D" {
			if !forms[form] {
				t.Errorf("tag %s is not a D form", tag)
			}
			continue
		}
		listed := false
		for _, f := range r.Forms {
			listed = listed || f == form
		}
		if !listed {
			t.Errorf("tag %s is not a listed form of %s", tag, row)
		}
	}
	sort.Strings(got)
	for _, r := range goContract {
		if !goImplemented[r.Unit] {
			continue
		}
		if tags[r.ID] == 0 {
			t.Errorf("the fixture does not reach row %s", r.ID)
		}
		for _, f := range r.Forms {
			if tags[r.ID+"/"+f] == 0 {
				t.Errorf("the fixture does not reach form %s/%s", r.ID, f)
			}
		}
	}
	for _, f := range goUnreadForms {
		if goImplemented[f.Unit] && tags["D/"+f.Form] == 0 {
			t.Errorf("the fixture does not reach D form %s", f.Form)
		}
	}
	if t.Failed() {
		t.Logf("tags: %v", got)
	}
}
