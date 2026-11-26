package typescript

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// drizzleCensusGates are the investigation's gate counts per corpus (task file,
// "Payload decision", evidence 3): files that import a
// drizzle-orm/<dialect>-core module (G1) and configs that import drizzle-kit
// (G2). Neither corpus has a default-named config without the import (G3).
// The investigation counted dokploy's G1 files by text search (55); one of them,
// packages/server/auth-schema.ts, is entirely commented out, import
// included, so a parsed import list gives 54.
var drizzleCensusGates = map[string]map[string]int{
	"kaneo":   {"G1": 8, "G2": 1},
	"dokploy": {"G1": 54, "G2": 1},
}

// drizzleRow is the part of an investigation inventory row
// (docs/contract/claims/data/drizzle-models-inventory.jsonl) the census compares.
type drizzleRow struct {
	Corpus   string `json:"corpus"`
	Scope    string `json:"scope"`
	Row      string `json:"row"`
	Form     string `json:"form"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Exported *bool  `json:"exported"`
	Values   int    `json:"values"`
}

// TestDrizzleCensus checks that the payload captures exactly the syntax
// The investigation's inventory found in kaneo and dokploy: the gated files, every
// table, enum, customType, relations() and sql-const declaration at its line
// with its exported flag, every table's columns as ordered object
// properties with their literal names, and every extra-config element (with
// the sql template of each partial index). It only parses; set
// GRAPHVEIN_DRIZZLE_CENSUS_CORPUS to the .corpus directory to run it.
func TestDrizzleCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_DRIZZLE_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_DRIZZLE_CENSUS_CORPUS is not set")
	}
	rows := drizzleInventory(t)
	for _, corpus := range []string{"kaneo", "dokploy"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, corpus)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%s: %v (run scripts/cloud-corpus.sh %s)", corpus, err, corpus)
			}
			payloads, gates := drizzleCapture(t, root)
			for gate, want := range drizzleCensusGates[corpus] {
				if got := len(gates[gate]); got != want {
					t.Errorf("%s files = %d, want %d: %v", gate, got, want, gates[gate])
				}
			}
			if len(gates["G3"]) != 0 {
				t.Errorf("G3 files %v, want none", gates["G3"])
			}
			checkDrizzleRows(t, payloads, rows[corpus])
			declarations := 0
			for _, s := range payloads {
				declarations += len(s.Declarations)
			}
			t.Logf("%s: G1 %d, G2 %d files; %d declarations; %d inventory rows checked", corpus, len(gates["G1"]), len(gates["G2"]), declarations, len(rows[corpus]))
		})
	}
}

func drizzleInventory(t *testing.T) map[string][]drizzleRow {
	t.Helper()
	f, err := os.Open("../../../docs/contract/claims/data/drizzle-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]drizzleRow{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var r drizzleRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Scope == "read" || r.Scope == "not_read" {
			out[r.Corpus] = append(out[r.Corpus], r)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// drizzleCapture parses every TypeScript and JavaScript file the indexer
// would scan and returns the payloads by path and the gated files by row.
func drizzleCapture(t *testing.T, root string) (map[string]*ModelSyntax, map[string][]string) {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var files []scan.File
	var jobs []parse.Job
	for _, f := range scanned.Files {
		switch f.Record.Language {
		case "typescript", "tsx", "javascript":
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: f.Record.Language, Source: f.Source})
		}
	}
	payloads := map[string]*ModelSyntax{}
	gates := map[string][]string{}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err = parse.Consume(context.Background(), jobs, options, func(i int, result parse.Result) {
		if result.Err != nil {
			t.Errorf("%s: %v", files[i].Record.Path, result.Err)
			return
		}
		f := Extract(files[i].Record.Path, files[i].Source, result.Root)
		if gate := ModelGate(files[i].Record.Path, result.Root, f); gate != "" {
			gates[gate] = append(gates[gate], files[i].Record.Path)
		}
		if s := CaptureModels(files[i].Record.Path, result.Root, f); s != nil {
			if !s.Valid() {
				t.Errorf("%s: invalid payload", files[i].Record.Path)
			}
			payloads[files[i].Record.Path] = s
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range gates {
		sort.Strings(list)
	}
	return payloads, gates
}

// calleeName is the identifier a call's callee names, or "".
func calleeName(e ModelExpr) string {
	if e.Kind != ModelCall || e.Callee.Kind != ModelIdentifier {
		return ""
	}
	return e.Callee.Name
}

// innermostCall follows a builder chain (text("a").notNull().default(x)) to
// its first call.
func innermostCall(e ModelExpr) ModelExpr {
	for e.Kind == ModelCall && e.Callee.Kind == ModelMember && e.Callee.Object.Kind == ModelCall {
		e = *e.Callee.Object
	}
	return e
}

// chainMethods lists the method calls of a builder chain, outermost last.
func chainMethods(e ModelExpr) map[string]ModelExpr {
	out := map[string]ModelExpr{}
	for e.Kind == ModelCall && e.Callee.Kind == ModelMember {
		out[e.Callee.Name] = e
		e = *e.Callee.Object
	}
	return out
}

func checkDrizzleRows(t *testing.T, payloads map[string]*ModelSyntax, rows []drizzleRow) {
	t.Helper()
	decls := map[string]ModelDeclaration{}
	for file, s := range payloads {
		for _, d := range s.Declarations {
			decls[file+"#"+d.Name] = d
		}
	}
	find := func(r drizzleRow, name string) (ModelDeclaration, bool) {
		d, ok := decls[r.File+"#"+name]
		if !ok {
			t.Errorf("%s:%d %s %s: declaration %s not captured", r.File, r.Line, r.Row, r.Form, name)
		}
		return d, ok
	}
	columns := map[string][]drizzleRow{}
	elements := map[string][]drizzleRow{}
	extra := map[string]drizzleRow{}
	tables := map[string]bool{}
	read := map[string]bool{}
	for _, r := range rows {
		switch {
		case r.Row == "DR-T1":
			tables[r.File+"#"+r.ID] = true
			read[r.File+"#"+r.ID] = r.Scope == "read"
			d, ok := find(r, r.ID)
			if !ok {
				continue
			}
			args := d.Value.Args
			if calleeName(d.Value) != "pgTable" || len(args) < 2 || args[0].Kind != ModelString || args[0].Value != r.Name || args[1].Kind != ModelObject || d.Span.StartLine != r.Line || r.Exported == nil || d.Exported != *r.Exported {
				t.Errorf("%s:%d table %s: captured %s at %d exported %v", r.File, r.Line, r.ID, shape(d.Value)[:min(60, len(shape(d.Value)))], d.Span.StartLine, d.Exported)
			}
		case r.Row == "DR-E1" && strings.HasPrefix(r.Form, "pgEnum:"):
			d, ok := find(r, r.ID)
			if !ok {
				continue
			}
			args := d.Value.Args
			if calleeName(d.Value) != "pgEnum" || len(args) != 2 || args[0].Value != r.Name || args[1].Kind != ModelArray || len(args[1].Elements) != r.Values || d.Span.StartLine != r.Line || d.Exported != (r.Exported != nil && *r.Exported) {
				t.Errorf("%s:%d enum %s: captured %s", r.File, r.Line, r.ID, shape(d.Value))
			}
		case r.Row == "DR-C3" && strings.HasPrefix(r.Form, "customType:dataType_literal:"):
			d, ok := find(r, r.ID)
			if !ok {
				continue
			}
			want := strings.TrimPrefix(r.Form, "customType:dataType_literal:")
			found := false
			if calleeName(d.Value) == "customType" && len(d.Value.Args) == 1 && d.Value.Args[0].Kind == ModelObject {
				for _, e := range d.Value.Args[0].Entries {
					found = found || (e.Kind == ModelMethod && e.Key == "dataType" && e.Value.Kind == ModelString && e.Value.Value == want)
				}
			}
			if !found || d.Span.StartLine != r.Line {
				t.Errorf("%s:%d customType %s: captured %s", r.File, r.Line, r.ID, shape(d.Value))
			}
		case r.Row == "DR-R1":
			if d, ok := find(r, r.ID); ok && (calleeName(d.Value) != "relations" || d.Span.StartLine != r.Line) {
				t.Errorf("%s:%d relations %s: captured %s", r.File, r.Line, r.ID, shape(d.Value))
			}
		case r.Row == "DR-C2" && r.Form == "module_sql_const":
			if d, ok := find(r, r.ID); ok && (d.Value.Kind != ModelTagged || d.Value.Tag.Name != "sql" || d.Span.StartLine != r.Line) {
				t.Errorf("%s:%d sql const %s: captured %s", r.File, r.Line, r.ID, shape(d.Value))
			}
		case (r.Row == "DR-C1" && r.Form == "column_name_literal") || (r.Row == "DR-K2" && r.Form == "column_name_omitted"):
			table, _, _ := strings.Cut(r.ID, ".")
			columns[r.File+"#"+table] = append(columns[r.File+"#"+table], r)
		case r.Row == "DR-X1" && strings.HasPrefix(r.Form, "extra_"):
			extra[r.File+"#"+r.ID] = r
		case r.Row == "DR-X1":
			elements[r.File+"#"+r.ID] = append(elements[r.File+"#"+r.ID], r)
		}
	}
	// Columns: the table's object properties, in order, with literal names.
	for key, list := range columns {
		sort.Slice(list, func(i, j int) bool { return list[i].Line < list[j].Line })
		d, ok := decls[key]
		if !ok || len(d.Value.Args) < 2 {
			t.Errorf("%s: columns of a table that is not captured", key)
			continue
		}
		entries := d.Value.Args[1].Entries
		if len(entries) != len(list) {
			t.Errorf("%s: %d entries, inventory %d columns", key, len(entries), len(list))
			continue
		}
		for i, r := range list {
			e := entries[i]
			_, prop, _ := strings.Cut(r.ID, ".")
			first := innermostCall(e.Value)
			name := ""
			if first.Kind == ModelCall && len(first.Args) > 0 && first.Args[0].Kind == ModelString {
				name = first.Args[0].Value
			}
			if e.Kind != ModelProperty || e.Key != prop || e.Span.StartLine != r.Line || name != r.Name {
				t.Errorf("%s:%d column %s: entry %s %q line %d name %q", r.File, r.Line, r.ID, e.Kind, e.Key, e.Span.StartLine, name)
			}
		}
	}
	// Extra config: none, an arrow returning an array of elements, or the
	// deprecated arrow returning an object. The investigation records the extra config
	// of tables in the read scope only.
	for key := range tables {
		if !read[key] {
			continue
		}
		d := decls[key]
		r, ok := extra[key]
		if !ok {
			t.Errorf("%s: no extra-config inventory row", key)
			continue
		}
		args := d.Value.Args
		switch r.Form {
		case "extra_none":
			if len(args) != 2 {
				t.Errorf("%s: extra_none but %d arguments", key, len(args))
			}
			continue
		case "extra_object_deprecated":
			if len(args) != 3 || args[2].Kind != ModelArrow || args[2].Body.Kind != ModelObject {
				t.Errorf("%s: deprecated object extra config not captured", key)
			}
			continue
		}
		if len(args) != 3 || args[2].Kind != ModelArrow || args[2].Body.Kind != ModelArray {
			t.Errorf("%s: extra array not captured", key)
			continue
		}
		list := elements[key]
		sort.Slice(list, func(i, j int) bool { return list[i].Line < list[j].Line })
		got := args[2].Body.Elements
		if len(got) != len(list) {
			t.Errorf("%s: %d extra elements, inventory %d", key, len(got), len(list))
			continue
		}
		for i, r := range list {
			e := got[i]
			builder := calleeName(innermostCall(e))
			wantBuilder, _, _ := strings.Cut(r.Form, "(")
			where, hasWhere := chainMethods(e)["where"]
			subst := hasWhere && len(where.Args) == 1 && where.Args[0].Kind == ModelTagged && len(where.Args[0].Substitutions) > 0
			if e.Span.StartLine != r.Line || builder != wantBuilder || subst != strings.Contains(r.Form, ".where(sql_subst)") {
				t.Errorf("%s:%d element %s: captured %s at %d", r.File, r.Line, r.Form, fmt.Sprint(builder), e.Span.StartLine)
			}
		}
	}
	// Nothing extra: every captured pgTable call is an inventory table.
	for key, d := range decls {
		if calleeName(d.Value) == "pgTable" && !tables[key] {
			t.Errorf("%s: captured pgTable declaration not in the inventory", key)
		}
	}
}
