package golang

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const structureDoc = "../../../docs/lang/go-structure.md"

var structureRowID = regexp.MustCompile(`^\|\s*(GS-[A-Z][0-9]+)\s*\|`)

// TestGoStructureContract keeps the Go structural claim contract 1:1: the
// row ids of docs/lang/go-structure.md and structureRows in the same order,
// every default-branch form named in the document, and every row and the
// default branch D reached by the fixture (each branch reports its row
// through structureTag), with no branch reporting an id outside the table.
func TestGoStructureContract(t *testing.T) {
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
	_, section, ok := strings.Cut(doc, "\n## Default branch D\n")
	if !ok {
		t.Fatal("the document has no \"## Default branch D\" section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	for _, form := range []string{unreadSyntaxError, unreadReceiver, unreadRedeclaration, unreadNestedModule, unreadUndecided, unreadSelfImport, unreadCaptureMissing, unreadLoneCR, unreadMalformedPath} {
		if !strings.Contains(section, "`"+form+"`") {
			t.Errorf("default branch section does not name form %s", form)
		}
	}

	var mu sync.Mutex
	reached := map[string]bool{}
	structureTagHook = func(row string) {
		mu.Lock()
		defer mu.Unlock()
		reached[row] = true
	}
	defer func() { structureTagHook = nil }()
	runStructure(t, copyStructureFixture(t))
	for _, row := range structureRows {
		if !reached[row.ID] {
			t.Errorf("row %s: no fixture case reaches it", row.ID)
		}
	}
	for id := range reached {
		if !rows[id] {
			t.Errorf("a branch reports %s, which is not a contract row", id)
		}
	}
}
