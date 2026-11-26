package python

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const structureDoc = "../../../docs/lang/python-structure.md"

var structureRowID = regexp.MustCompile(`^\|\s*(PS-[A-Z][0-9]+)\s*\|`)

var (
	unreadFormLine = regexp.MustCompile("^- `([a-z_]+)` \\((PS-[A-Z][0-9]+)\\)")
	rejectFormLine = regexp.MustCompile("^\\|\\s*`([a-z_]+)`\\s*\\|")
)

// TestPythonStructureContract keeps the Python structural claim contract
// 1:1: the row ids of docs/lang/python-structure.md and structureRows in
// the same order; the default-branch forms the document lists, each with
// the one row that sends it to D, equal to unreadForms and unreadRows; the
// PS-F2 rejection forms the document's table lists equal to rejectForms;
// the reference Python version stated there; and on the fixture, every row
// and the default branch D reached (each branch reports its row through
// structureTag), every default-branch form counted at a site that reports
// exactly its mapped row, every rejection form reached, and no branch
// reporting an id outside the table.
func TestPythonStructureContract(t *testing.T) {
	data, err := os.ReadFile(structureDoc)
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(data), "\r\n", "\n")
	var ids []string
	for _, line := range strings.Split(doc, "\n") {
		if m := structureRowID.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	var code []string
	rows := map[string]bool{}
	for _, row := range structureRows {
		rows[row.ID] = true
		if row.ID != "D" {
			code = append(code, row.ID)
		}
		if row.Claim == "" {
			t.Errorf("row %s has no claim", row.ID)
		}
	}
	if strings.Join(ids, " ") != strings.Join(code, " ") {
		t.Errorf("document rows %v, structureRows %v", ids, code)
	}

	// Default-branch forms: document list = unreadForms, each with its row.
	_, section, ok := strings.Cut(doc, "\n## Default branch D\n")
	if !ok {
		t.Fatal("the document has no \"## Default branch D\" section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var docForms []string
	for _, line := range strings.Split(section, "\n") {
		if m := unreadFormLine.FindStringSubmatch(line); m != nil {
			docForms = append(docForms, m[1])
			if unreadRows[m[1]] != m[2] {
				t.Errorf("document maps form %s to %s, unreadRows to %q", m[1], m[2], unreadRows[m[1]])
			}
		}
	}
	if strings.Join(docForms, " ") != strings.Join(unreadForms, " ") {
		t.Errorf("document default-branch forms %v, unreadForms %v", docForms, unreadForms)
	}
	if len(unreadRows) != len(unreadForms) {
		t.Errorf("unreadRows has %d forms, unreadForms %d", len(unreadRows), len(unreadForms))
	}
	for form, row := range unreadRows {
		if !rows[row] || row == "D" {
			t.Errorf("form %s maps to %s, not a claim row", form, row)
		}
	}

	// Rejection forms: document table = rejectForms.
	_, rejected, ok := strings.Cut(doc, "\n### Rejection forms\n")
	if !ok {
		t.Fatal("the document has no \"### Rejection forms\" section")
	}
	rejected, _, _ = strings.Cut(rejected, "\n#")
	var docRejects []string
	for _, line := range strings.Split(rejected, "\n") {
		if m := rejectFormLine.FindStringSubmatch(line); m != nil {
			docRejects = append(docRejects, m[1])
		}
	}
	if strings.Join(docRejects, " ") != strings.Join(rejectForms, " ") {
		t.Errorf("document rejection forms %v, rejectForms %v", docRejects, rejectForms)
	}
	if !strings.Contains(doc, "Reference Python: CPython "+referencePython) {
		t.Errorf("the document does not state the reference Python %s", referencePython)
	}

	var mu sync.Mutex
	reached := map[string]bool{}
	structureTagHook = func(row string) {
		mu.Lock()
		defer mu.Unlock()
		reached[row] = true
	}
	defer func() { structureTagHook = nil }()
	run := runStructure(t, copyStructureFixture(t))
	for _, row := range structureRows {
		if !reached[row.ID] {
			t.Errorf("row %s: no fixture case reaches it", row.ID)
		}
	}
	for id := range reached {
		if form, ok := strings.CutPrefix(id, "D/"); ok {
			if _, known := unreadRows[form]; !known {
				t.Errorf("a branch counts form %s, which is not a default-branch form", form)
			}
			continue
		}
		if !rows[id] {
			t.Errorf("a branch reports %s, which is not a contract row", id)
		}
	}
	seen := map[string]bool{}
	for _, form := range unreadForms {
		seen[form] = reached["D/"+form]
	}
	for _, d := range run.diagnostics {
		if d.Code == "python_syntax_rejected" {
			for _, form := range rejectForms {
				if strings.Contains(d.Message, "("+form+")") {
					seen[form] = true
				}
			}
		}
	}
	seen[rejectTreeSitterError] = run.files["syntax_bad.py"].Python != nil && run.files["syntax_bad.py"].Python.Rejected
	for _, form := range append(append([]string{}, rejectForms...), unreadForms...) {
		if !seen[form] {
			t.Errorf("form %s: no fixture case reaches it", form)
		}
	}
}
