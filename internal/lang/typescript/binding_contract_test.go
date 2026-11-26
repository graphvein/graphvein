package typescript_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	resolutionDoc        = "../../../docs/lang/typescript-resolution.md"
	callScopesFixture    = "../../../testdata/fixtures/ts-call-scopes"
	callScopesGoldenPath = "../../../testdata/fixtures/ts-call-scopes.golden.json"
)

var bindingRowID = regexp.MustCompile(`^\|\s*(TSX-[0-9]+)\s*\|`)

// resolvable and unresolvable are the two outcomes a row of the call
// binding model gives a call it decides: the call is left to link time, or
// marked unresolved.
func resolvable(typescript.BindingDecision) bool   { return true }
func unresolvable(typescript.BindingDecision) bool { return false }

// bindingOutcomes is each row's outcome, as the table of
// docs/lang/typescript-resolution.md states it, for every call the row
// decides. Rows whose outcome depends on the call's form say how.
var bindingOutcomes = map[string]func(typescript.BindingDecision) bool{
	"TSX-1": unresolvable,
	"TSX-2": unresolvable,
	"TSX-3": resolvable,
	"TSX-4": unresolvable,
	"TSX-5": unresolvable,
	"TSX-6": unresolvable,
	// `C.m()` resolves; calling the class itself (`C()`) does not.
	"TSX-7": func(d typescript.BindingDecision) bool { return strings.Contains(d.Name, ".") },
	"TSX-8": resolvable,
	// `o.m()` resolves; calling the object itself (`o()`) does not.
	"TSX-9":  func(d typescript.BindingDecision) bool { return strings.Contains(d.Name, ".") },
	"TSX-10": unresolvable,
	"TSX-11": resolvable,
	"TSX-12": unresolvable,
	"TSX-13": resolvable,
	"TSX-14": unresolvable,
	"TSX-15": unresolvable,
	"TSX-16": unresolvable,
	// Unresolved when link time would pick a declaration of the file.
	"TSX-17": func(d typescript.BindingDecision) bool { return !d.Declared },
	"TSX-18": resolvable,
	"TSX-19": unresolvable,
	"TSX-20": unresolvable,
	"TSX-21": unresolvable,
	"TSX-22": resolvable,
	"TSX-23": resolvable,
	"TSX-24": unresolvable,
	"TSX-25": unresolvable,
	"TSX-26": unresolvable,
}

// TestCallBindingContract keeps the call binding model 1:1: the row ids of
// the "Call binding model" table in docs/lang/typescript-resolution.md and
// typescript.BindingRows in the same order, every row reached when the
// indexer (index.Emit, the production path) indexes the fixture, every
// fixture call given its row's outcome (bindingOutcomes), no branch
// reporting an id outside the table, and the fixture's `calls` edges equal
// to the golden file (each call in the fixture is commented with its row
// and its expected edge).
func TestCallBindingContract(t *testing.T) {
	data, err := os.ReadFile(resolutionDoc)
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, section, ok := strings.Cut(doc, "\n## Call binding model\n")
	if !ok {
		t.Fatal(`the document has no "## Call binding model" section`)
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var ids []string
	for _, line := range strings.Split(section, "\n") {
		if m := bindingRowID.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	if strings.Join(ids, " ") != strings.Join(typescript.BindingRows, " ") {
		t.Errorf("document rows %v, BindingRows %v", ids, typescript.BindingRows)
	}
	rows := map[string]bool{}
	for _, row := range typescript.BindingRows {
		rows[row] = true
	}

	var mu sync.Mutex
	reached := map[string]bool{}
	var decisions []typescript.BindingDecision
	typescript.SetBindingTagHook(func(d typescript.BindingDecision) {
		mu.Lock()
		defer mu.Unlock()
		reached[d.Row] = true
		decisions = append(decisions, d)
	})
	defer typescript.SetBindingTagHook(nil)
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(callScopesFixture)); err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-25T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
		t.Fatal(err)
	}
	for _, row := range typescript.BindingRows {
		if !reached[row] {
			t.Errorf("row %s: no fixture case reaches it", row)
		}
	}
	for _, d := range decisions {
		want, ok := bindingOutcomes[d.Row]
		if !ok {
			continue
		}
		if expected := want(d); d.Resolved != expected {
			t.Errorf("%s %s at %d:%d (%s): resolved %v, the row's outcome is %v", d.Row, d.Name, d.Span.StartLine, d.Span.StartColumn, d.Source, d.Resolved, expected)
		}
	}
	for row := range rows {
		if _, ok := bindingOutcomes[row]; !ok {
			t.Errorf("row %s has no expected outcome in bindingOutcomes", row)
		}
	}
	for id := range reached {
		if !rows[id] {
			t.Errorf("a branch reports %s, which is not a binding row", id)
		}
	}
	var calls []contract.Edge
	scanner := bufio.NewScanner(&stream)
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		var e contract.Edge
		if json.Unmarshal(scanner.Bytes(), &e) == nil && e.Type == "edge" && e.Kind == "calls" {
			calls = append(calls, e)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, callScopesGoldenPath, calls)
}
