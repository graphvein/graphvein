package rust

import (
	"os"

	"github.com/graphvein/graphvein/internal/contract"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const structureDoc = "../../../docs/lang/rust-structure.md"

var structureRowID = regexp.MustCompile(`^\|\s*(RS-[A-Z][0-9]+)\s*\|`)

// TestRustStructureContract keeps the Rust structural claim contract 1:1:
// the row ids of docs/lang/rust-structure.md and structureRows in the same
// order, every default-branch form named in the document, and every row and
// the default branch D reached by the fixture (each branch reports its row
// through structureTag), with no branch reporting an id outside the table.
func TestRustStructureContract(t *testing.T) {
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
	for _, form := range unreadForms {
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
	type site struct {
		kind, file string
		span       contract.Span
	}
	sites := map[site]int{}
	structureSiteHook = func(kind, file string, span contract.Span) {
		mu.Lock()
		defer mu.Unlock()
		sites[site{kind, file, span}]++
	}
	defer func() { structureTagHook, structureSiteHook = nil, nil }()
	run := runStructure(t, copyStructureFixture(t))
	checkSitesEmitNothing(t, run, func(kind, file string, span contract.Span) int { return sites[site{kind, file, span}] }, func(yield func(kind, file string, span contract.Span)) {
		for s := range sites {
			yield(s.kind, s.file, s.span)
		}
	})
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
	// Every default-branch form is counted somewhere in the fixture.
	counted := map[string]bool{}
	for _, d := range run.diagnostics {
		if d.Code == "rust_structure_unread" {
			for _, form := range unreadForms {
				if strings.Contains(d.Message, form+"=") {
					counted[form] = true
				}
			}
		}
	}
	for _, form := range unreadForms {
		if !counted[form] {
			t.Errorf("form %s: no fixture case counts it", form)
		}
	}
}

// checkSitesEmitNothing checks that every site a branch refused emits
// nothing: no node for a refused declaration, no imports edge evidence for a
// refused mod item or for a use declaration none of whose leaves claimed an
// edge, and nothing but the file for a file refused whole (RS-F2, RS-F3).
func checkSitesEmitNothing(t *testing.T, run *structureRun, count func(kind, file string, span contract.Span) int, all func(func(kind, file string, span contract.Span))) {
	t.Helper()
	evidence := map[string]map[contract.Span]bool{}
	for _, e := range run.edges {
		if e.Kind != "imports" {
			continue
		}
		for _, ev := range e.Evidence {
			if evidence[ev.File] == nil {
				evidence[ev.File] = map[contract.Span]bool{}
			}
			evidence[ev.File][ev.Span] = true
		}
	}
	checked := 0
	all(func(kind, file string, span contract.Span) {
		switch kind {
		case siteDeclaration:
			checked++
			for _, n := range run.files[file].Nodes {
				if n.Span != nil && n.Span.StartLine == span.StartLine && n.Span.StartColumn == span.StartColumn {
					t.Errorf("refused declaration %s:%d emits node %s", file, span.StartLine, n.Identity)
				}
			}
		case siteMod:
			checked++
			if evidence[file][span] {
				t.Errorf("refused mod item %s:%d emits an imports edge", file, span.StartLine)
			}
		case siteUse:
			checked++
			if count(siteClaimedUse, file, span) == 0 && evidence[file][span] {
				t.Errorf("refused use declaration %s:%d emits an imports edge", file, span.StartLine)
			}
		}
	})
	for file, f := range run.files {
		if f.Rust == nil || !f.Rust.Rejected && !f.Rust.LoneCR {
			continue
		}
		checked++
		if len(f.Nodes)+len(f.Imports)+len(f.Modules)+len(f.Calls) > 0 || len(evidence[file]) > 0 {
			t.Errorf("refused file %s emits more than its file node", file)
		}
	}
	if checked == 0 {
		t.Error("no refused site reported")
	}
}
