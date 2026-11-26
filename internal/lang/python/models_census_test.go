package python

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// censusRow is the part of an investigation census row (docs/contract/claims/data/python-models-inventory.jsonl)
// that the census-equivalence test compares.
type censusRow struct {
	Kind   string   `json:"kind"`
	Corpus string   `json:"corpus"`
	File   string   `json:"file"`
	Form   string   `json:"form"`
	Forms  []string `json:"forms"`
	Class  string   `json:"cls"`
	Line   int      `json:"line"`
}

// censusSites are the investigation's X1/U5 site counts by kind (inventory, "Forms by
// contract row"); every other corpus and kind has none.
var censusSites = map[string]map[string]int{
	"langflow": {"append_constraint": 3, "append_column": 2, "constraint_call": 2, "ddl_hook": 4},
}

// censusUnresolvedCore lists the Core-table candidates that gate G4 keeps by
// spelling (callee's last segment Table) but whose callee is not imported
// from sqlalchemy or sqlmodel, so the investigation's census (resolved by origin) does
// not count them and T018c2's resolution drops them.
var censusUnresolvedCore = map[string]map[string]bool{
	"langflow": {"src/backend/base/langflow/__main__.py:354": true}, // rich.table.Table
}

// censusG3Subclasses are the classes that T018c1 Revision 3 adds to gate G3
// (a same-file subclass of a member-captured class) and that the investigation's census
// does not count: in both corpora, API schema classes such as
// `UserCreate(UserBase)` with `UserBase(SQLModel)`. None has a table keyword.
var censusG3Subclasses = map[string][]string{
	"fastapi": {
		"backend/app/models.py:22 UserCreate",
		"backend/app/models.py:63 UserPublic",
		"backend/app/models.py:80 ItemCreate",
		"backend/app/models.py:104 ItemPublic",
	},
	"langflow": {
		"src/backend/base/langflow/services/database/models/api_key/model.py:59 ApiKeyCreate",
		"src/backend/base/langflow/services/database/models/api_key/model.py:71 UnmaskedApiKeyRead",
		"src/backend/base/langflow/services/database/models/api_key/model.py:77 ApiKeyRead",
		"src/backend/base/langflow/services/database/models/flow/model.py:252 FlowCreate",
		"src/backend/base/langflow/services/database/models/flow/model.py:264 FlowRead",
		"src/backend/base/langflow/services/database/models/folder/model.py:69 FolderCreate",
		"src/backend/base/langflow/services/database/models/folder/model.py:79 FolderRead",
		"src/backend/base/langflow/services/database/models/folder/model.py:84 FolderListRead",
		"src/backend/base/langflow/services/database/models/folder/model.py:89 FolderReadWithFlows",
		"src/backend/base/langflow/services/database/models/memory_base/model.py:41 MemoryBaseCreate",
		"src/backend/base/langflow/services/database/models/memory_base/model.py:83 MemoryBaseRead",
		"src/backend/base/langflow/services/database/models/memory_base/model.py:135 MemoryBaseSessionRead",
		"src/backend/base/langflow/services/database/models/message/model.py:327 MessageRead",
		"src/backend/base/langflow/services/database/models/message/model.py:356 MessageCreate",
		"src/backend/base/langflow/services/database/models/transactions/model.py:162 TransactionReadResponse",
		"src/backend/base/langflow/services/database/models/variable/model.py:53 VariableCreate",
	},
}

// TestModelCensus checks that the Go capture finds exactly the investigation's model
// targets in the corpus checkouts (T018c1 B5): G1 files, member-captured
// classes, members of table and chain classes by kind, Core-table lines and
// mutation sites. It only parses; set GRAPHVEIN_MODEL_CENSUS_CORPUS to the
// .corpus directory to run it.
func TestModelCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_MODEL_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_MODEL_CENSUS_CORPUS is not set")
	}
	want := censusExpectation(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		corpus := e.Name()
		t.Run(corpus, func(t *testing.T) {
			got := censusCapture(t, filepath.Join(dir, corpus), censusSites[corpus])
			exp := want[corpus]
			if exp == nil {
				exp = newCensusSets()
			}
			exp["unresolved"] = censusUnresolvedCore[corpus]
			for _, k := range censusG3Subclasses[corpus] {
				exp["captured"][k] = true
			}
			for _, key := range []string{"files", "captured", "members", "core", "unresolved", "sites"} {
				missing, extra := diffSets(exp[key], got[key])
				if len(missing)+len(extra) > 0 {
					t.Errorf("%s %s: missing %d %v; extra %d %v", corpus, key, len(missing), head(missing), len(extra), head(extra))
				}
			}
			t.Logf("%s: G1 files %d, member-captured classes %d, table/chain members %d, Core-table lines %d, sites %d",
				corpus, len(got["files"]), len(got["captured"]), len(got["members"]), len(got["core"]), len(got["sites"]))
		})
	}
}

func newCensusSets() map[string]map[string]bool {
	return map[string]map[string]bool{"files": {}, "captured": {}, "members": {}, "core": {}, "unresolved": {}, "sites": {}}
}

// censusExpectation reads the investigation's census into comparable keys per corpus.
func censusExpectation(t *testing.T) map[string]map[string]map[string]bool {
	f, err := os.Open("../../../docs/contract/claims/data/python-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]map[string]map[string]bool{}
	sets := func(corpus string) map[string]map[string]bool {
		if out[corpus] == nil {
			out[corpus] = newCensusSets()
		}
		return out[corpus]
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var r censusRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		switch {
		case r.Kind == "census.file" && r.Form == "file.imports_orm":
			sets(r.Corpus)["files"][r.File] = true
		case r.Kind == "census.class" && (r.Form == "class.table_true" || r.Form == "class.model_ancestor" || r.Form == "class.sqlmodel_nontable"):
			sets(r.Corpus)["captured"][fmt.Sprintf("%s:%d %s", r.File, r.Line, r.Class)] = true
		case r.Kind == "census.member":
			sets(r.Corpus)["members"][fmt.Sprintf("%s:%d %s %s", r.File, r.Line, r.Class, censusMemberKind(r.Forms[0]))] = true
		case r.Kind == "census.module" && r.Form == "module.call.Table":
			sets(r.Corpus)["core"][fmt.Sprintf("%s:%d", r.File, r.Line)] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for corpus, kinds := range censusSites {
		for kind, n := range kinds {
			for i := 0; i < n; i++ {
				sets(corpus)["sites"][fmt.Sprintf("%s #%d", kind, i+1)] = true
			}
		}
	}
	return out
}

// censusMemberKind maps a census member's first form to the payload kind.
func censusMemberKind(form string) string {
	switch {
	case strings.HasPrefix(form, "annotation."):
		return "annotated"
	case strings.HasPrefix(form, "dunder."):
		return "assign"
	case strings.HasPrefix(form, "method"):
		return "method"
	case form == "nested_class":
		return "class"
	}
	return "unknown:" + form
}

// censusCapture parses every Python file of a checkout and reduces the
// captured payloads to the census keys.
func censusCapture(t *testing.T, root string, _ map[string]int) map[string]map[string]bool {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".py") || strings.HasSuffix(path, ".pyi")) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	sources := make([][]byte, len(paths))
	jobs := make([]parse.Job, len(paths))
	for i, p := range paths {
		if sources[i], err = os.ReadFile(p); err != nil {
			t.Fatal(err)
		}
		jobs[i] = parse.Job{Language: "python", Source: sources[i]}
	}
	models := map[string]*facts.PythonModels{}
	coreOrigin := map[string]bool{} // file:line -> callee imported from sqlalchemy/sqlmodel
	workers := runtime.NumCPU() / 2
	if workers < 1 {
		workers = 1
	}
	results := make([]*facts.PythonModels, len(paths))
	err = parse.Consume(context.Background(), jobs, parse.Options{Workers: workers, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, r parse.Result) {
		if r.Err != nil || r.Root == nil {
			return
		}
		rel, _ := filepath.Rel(root, paths[i])
		rel = filepath.ToSlash(rel)
		f := Extract(rel, sources[i], r.Root)
		if f.Python != nil && f.Python.Models != nil {
			if !f.Python.ModelsValid() {
				t.Errorf("%s: invalid payload", rel)
			}
			results[i] = f.Python.Models
			for _, line := range f.Python.Models.CoreTables {
				coreOrigin[fmt.Sprintf("%s:%d", rel, line)] = coreFromORM(f, line)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range paths {
		if results[i] != nil {
			rel, _ := filepath.Rel(root, p)
			models[filepath.ToSlash(rel)] = results[i]
		}
	}
	got := newCensusSets()
	tables := map[string]bool{}
	bases := map[string][]string{}
	for _, m := range models {
		for _, c := range m.Classes {
			for _, k := range c.Keywords {
				if k.Name == "table" && k.Value.Literal == "True" {
					tables[c.Name] = true
				}
			}
			for _, b := range c.Bases {
				if b.Name != "" {
					bases[c.Name] = append(bases[c.Name], lastSegment(b.Name))
				}
			}
		}
	}
	// Chain classes: named as a base, transitively by simple name, of a table class.
	chain := map[string]bool{}
	var follow func(string)
	follow = func(name string) {
		for _, b := range bases[name] {
			if !chain[b] {
				chain[b] = true
				follow(b)
			}
		}
	}
	for name := range tables {
		follow(name)
	}
	siteCount := map[string]int{}
	for file, m := range models {
		got["files"][file] = true
		for _, line := range m.CoreTables {
			key := fmt.Sprintf("%s:%d", file, line)
			if coreOrigin[key] {
				got["core"][key] = true
			} else {
				got["unresolved"][key] = true
			}
		}
		for _, s := range m.Sites {
			siteCount[s.Kind]++
			got["sites"][fmt.Sprintf("%s #%d", s.Kind, siteCount[s.Kind])] = true
		}
		for _, c := range m.Classes {
			if !c.MembersCaptured {
				continue
			}
			got["captured"][fmt.Sprintf("%s:%d %s", file, c.Line, c.Name)] = true
			if !tables[c.Name] && !chain[c.Name] {
				continue
			}
			for _, mem := range c.Members {
				if mem.Kind == "other" || mem.Line == 0 {
					got["members"][fmt.Sprintf("%s:%d %s %s/%s", file, mem.Line, c.Name, mem.Kind, mem.Detail)] = true
					continue
				}
				got["members"][fmt.Sprintf("%s:%d %s %s", file, mem.Line, c.Name, mem.Kind)] = true
			}
		}
	}
	return got
}

func diffSets(want, got map[string]bool) (missing, extra []string) {
	for k := range want {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func head(s []string) []string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// coreFromORM is the census's by-origin test for a Core-table line: the
// callee's first segment is bound by an import from sqlalchemy or sqlmodel.
func coreFromORM(f facts.Facts, line int) bool {
	for _, call := range f.Calls {
		last := lastSegment(call.Name)
		if call.Span.StartLine != line || last != "Table" && last != "map_imperatively" {
			continue
		}
		first := strings.SplitN(call.Name, ".", 2)[0]
		for _, imp := range f.Imports {
			if _, ok := imp.Bindings[first]; ok && modelGate([]facts.Import{imp}) {
				return true
			}
		}
	}
	return false
}
