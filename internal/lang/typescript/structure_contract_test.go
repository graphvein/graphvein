package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const structureDoc = "../../../docs/lang/typescript-structure.md"

var structureRowID = regexp.MustCompile(`^\|\s*(TS-[A-Z][0-9]+)\s*\|`)

// TestTypeScriptStructureContract keeps the TypeScript structural claim
// contract 1:1: the row ids of docs/lang/typescript-structure.md and
// structureRows in the same order, every default-branch form named in the
// document, and every row and the default branch D reached when the
// indexer (index.Emit, the production path) indexes the fixture: each
// branch reports its row through structureTag, and no branch reports an id
// outside the table.
func TestTypeScriptStructureContract(t *testing.T) {
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
	for _, row := range typescript.StructureRows {
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
	for _, form := range typescript.UnreadForms {
		if !strings.Contains(section, "`"+form+"`") {
			t.Errorf("default branch section does not name form %s", form)
		}
	}

	var mu sync.Mutex
	reached := map[string]bool{}
	typescript.SetStructureTagHook(func(row string) {
		mu.Lock()
		defer mu.Unlock()
		reached[row] = true
	})
	defer typescript.SetStructureTagHook(nil)
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var stream bytes.Buffer
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-17T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, copyFixture(t), s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	for _, row := range typescript.StructureRows {
		if !reached[row.ID] {
			t.Errorf("row %s: no fixture case reaches it", row.ID)
		}
	}
	for id := range reached {
		if !rows[id] {
			t.Errorf("a branch reports %s, which is not a contract row", id)
		}
	}
	// Every default-branch form is reached by the fixture too.
	counted := map[string]bool{}
	for _, line := range bytes.Split(stream.Bytes(), []byte("\n")) {
		var d struct{ Type, Code, Message string }
		if json.Unmarshal(line, &d) != nil || d.Type != "diagnostic" || d.Code != "ts_structure_unread" {
			continue
		}
		for _, form := range typescript.UnreadForms {
			if strings.Contains(d.Message, form+"=") {
				counted[form] = true
			}
		}
	}
	for _, form := range typescript.UnreadForms {
		if !counted[form] {
			t.Errorf("form %s: no fixture case counts it", form)
		}
	}
}
