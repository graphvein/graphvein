package supabasetypes

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// oracleFile is one file as scripts/oracle/nextjs_supabase/supabase_types.cjs
// reads it with the TypeScript checker.
type oracleFile struct {
	File        string `json:"file"`
	Diagnostics int    `json:"diagnostics"`
	Schemas     []struct {
		Name   string `json:"name"`
		Tables []struct {
			Name string `json:"name"`
			Row  []struct {
				Name     string `json:"name"`
				Nullable any    `json:"nullable"`
			} `json:"row"`
			Insert        []string `json:"insert"`
			Relationships []struct {
				Name       string   `json:"name"`
				Columns    []string `json:"columns"`
				RefRel     string   `json:"referenced_relation"`
				RefColumns []string `json:"referenced_columns"`
			} `json:"relationships"`
		} `json:"tables"`
	} `json:"schemas"`
}

// TestSupabaseTypesOracle compares the parser's reading of each generated
// types file with the TypeScript 6.0.3 compiler's (opt-in: set
// GRAPHVEIN_TS_ORACLE_DIR to the scratch install of
// scripts/oracle/ts_structure; Node from PATH or GRAPHVEIN_NODE). Files:
// the fixture's, plus GRAPHVEIN_SUPABASE_TYPES_FILES (a path list). Every
// table, column (Insert property), Row property, written nullability and
// relationship must agree; any disagreement fails (a precision stop).
func TestSupabaseTypesOracle(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TS_ORACLE_DIR")
	if dir == "" {
		t.Skip("GRAPHVEIN_TS_ORACLE_DIR is not set: install the TypeScript oracle as scripts/oracle/ts_structure/README.md says")
	}
	node := os.Getenv("GRAPHVEIN_NODE")
	if node == "" {
		found, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not on PATH and GRAPHVEIN_NODE is not set")
		}
		node = found
	}
	script, err := filepath.Abs("../../../../scripts/oracle/nextjs_supabase/supabase_types.cjs")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range []string{"app/lib/database.types.ts", "legacy/src/types.ts", "migrated/lib/types.ts"} {
		abs, err := filepath.Abs(filepath.Join(fixtureRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, abs)
	}
	if list := os.Getenv("GRAPHVEIN_SUPABASE_TYPES_FILES"); list != "" {
		files = append(files, filepath.SplitList(list)...)
	}
	cmd := exec.Command(node, append([]string{script}, files...)...)
	cmd.Env = append(os.Environ(), "TS_DIR="+dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("oracle: %v\n%s", err, stderr.String())
	}
	var oracle struct {
		TypeScript string       `json:"typescript"`
		Files      []oracleFile `json:"files"`
	}
	if err := json.Unmarshal(out, &oracle); err != nil {
		t.Fatal(err)
	}
	for _, of := range oracle.Files {
		t.Run(filepath.Base(filepath.Dir(of.File))+"/"+filepath.Base(of.File), func(t *testing.T) {
			compareOracle(t, of)
		})
	}
}

func compareOracle(t *testing.T, of oracleFile) {
	src, err := os.ReadFile(of.File)
	if err != nil {
		t.Fatal(err)
	}
	tf, _, err := parseFile(src)
	if err != nil {
		t.Fatalf("not read: %v", err)
	}
	if of.Diagnostics != 0 {
		t.Logf("%d compiler diagnostics (the file is checked on its own)", of.Diagnostics)
	}
	ours := map[string]schemaDecl{}
	for _, s := range tf.schemas {
		ours[s.name] = s
	}
	if len(ours) != len(of.Schemas) {
		t.Errorf("schemas: %d read, %d in the compiler", len(ours), len(of.Schemas))
	}
	tables, columns, rels := 0, 0, 0
	for _, osch := range of.Schemas {
		s, ok := ours[osch.Name]
		if !ok {
			t.Errorf("schema %s not read", osch.Name)
			continue
		}
		if len(s.tables) != len(osch.Tables) {
			t.Errorf("%s: %d tables read, %d in the compiler", osch.Name, len(s.tables), len(osch.Tables))
			continue
		}
		for i, ot := range osch.Tables {
			r := s.tables[i]
			if r.name != ot.Name {
				t.Errorf("%s: table %d is %s, compiler %s", osch.Name, i, r.name, ot.Name)
				continue
			}
			tables++
			var ins []string
			for _, f := range r.insert {
				ins = append(ins, f.name)
			}
			if !reflect.DeepEqual(ins, ot.Insert) && !(len(ins) == 0 && len(ot.Insert) == 0) {
				t.Errorf("%s.%s: columns %v, compiler Insert %v", osch.Name, r.name, ins, ot.Insert)
			}
			if len(r.row) != len(ot.Row) {
				t.Errorf("%s.%s: %d Row properties, compiler %d", osch.Name, r.name, len(r.row), len(ot.Row))
				continue
			}
			for j, oc := range ot.Row {
				f := r.row[j]
				var mine any
				if f.nullable != nil {
					mine = *f.nullable
				}
				if f.name != oc.Name || mine != oc.Nullable {
					t.Errorf("%s.%s: Row %s nullable %v, compiler %s %v", osch.Name, r.name, f.name, mine, oc.Name, oc.Nullable)
				}
				columns++
			}
			if len(r.rels) != len(ot.Relationships) {
				t.Errorf("%s.%s: %d relationships, compiler %d", osch.Name, r.name, len(r.rels), len(ot.Relationships))
				continue
			}
			for j, orl := range ot.Relationships {
				m := r.rels[j]
				if m.name != orl.Name || m.refRel != orl.RefRel || !reflect.DeepEqual(m.columns, orl.Columns) || !reflect.DeepEqual(m.refColumns, orl.RefColumns) {
					t.Errorf("%s.%s: relationship %+v, compiler %+v", osch.Name, r.name, m, orl)
				}
				rels++
			}
		}
	}
	t.Logf("agree: %d tables, %d Row properties, %d relationships", tables, columns, rels)
}
