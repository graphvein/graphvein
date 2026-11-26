package golang

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// gormCensusGates are the counts of the inventory's Gates table
// (the T018g5 task record (internal)) over non-test Go files: files, files
// passing G1, struct type specs, field declarations and tagged fields. The
// payload keeps package-level struct types only (row TD): realworld's
// inventory counts 32 structs and 110 fields, including the function-local
// `type result struct` of articles/models.go:94 with its 2 fields.
var gormCensusGates = map[string][5]int{
	"gotify":    {89, 7, 94, 353, 127},
	"realworld": {17, 6, 31, 108, 63},
}

// gormCensusDirs maps the inventory's corpus names to checkout names
// (scripts/cloud-corpus.sh).
var gormCensusDirs = map[string]string{"gotify": "gotify", "realworld": "realworld-gin"}

// gormRow is one inventory row (docs/contract/claims/data/gorm-models-inventory.jsonl).
type gormRow struct {
	Corpus string `json:"corpus"`
	Scope  string `json:"scope"`
	Row    string `json:"row"`
	Form   string `json:"form"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
}

func (r gormRow) key() string {
	return fmt.Sprintf("%s|%s|%s:%d|%s|%s", r.Row, r.Form, r.File, r.Line, r.ID, r.Name)
}

// TestGormCensus checks that the Go model payload holds exactly the syntax
// The investigation's inventory found in gotify and realworld (every `read` row: the
// gorm.Open sites with dialector and configuration form, the gorm.Config
// literals with their keys, the migrate sites with receiver and argument
// forms, the users.AutoMigrate() call statement, every reached struct and
// every field's Go type form and gorm tag setting), re-deriving each row
// from the payload alone with the inventory scanner's classification. It
// also reports the gate counts. It only parses; set
// GRAPHVEIN_GORM_CENSUS_CORPUS to the directory holding the checkouts
// (scripts/cloud-corpus.sh gotify realworld-gin) to run it.
func TestGormCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GORM_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_GORM_CENSUS_CORPUS is not set")
	}
	inventory := gormInventory(t)
	for _, corpus := range []string{"gotify", "realworld"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, gormCensusDirs[corpus])
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%s: %v (run scripts/cloud-corpus.sh %s)", corpus, err, gormCensusDirs[corpus])
			}
			files, sizes := gormCapture(t, root)
			gates := [5]int{}
			payloadBytes := 0
			for p, f := range files {
				if strings.HasSuffix(p, "_test.go") {
					continue
				}
				gates[0]++
				if f.Go.Gated {
					gates[1]++
				}
				gates[2] += len(f.Go.Structs)
				for _, s := range f.Go.Structs {
					gates[3] += len(s.Fields)
					for _, fl := range s.Fields {
						if fl.Tag != "" {
							gates[4]++
						}
					}
				}
				payloadBytes += sizes[p]
			}
			if gates != gormCensusGates[corpus] {
				t.Errorf("gates (files, G1, structs, fields, tagged) = %v, want %v", gates, gormCensusGates[corpus])
			}
			got := deriveGormRows(t, root, files)
			want := map[string]int{}
			for _, r := range inventory[corpus] {
				want[r.key()]++
			}
			for k, n := range got {
				if want[k] != n {
					t.Errorf("payload row %s: %d, inventory %d", k, n, want[k])
				}
			}
			for k, n := range want {
				if got[k] != n {
					t.Errorf("inventory row %s: %d, payload %d", k, n, got[k])
				}
			}
			// The order rule's expansion: realworld's Migrate calls
			// users.AutoMigrate() as a top-level statement before its own
			// AutoMigrate calls (hello.go:15-22).
			for _, r := range inventory[corpus] {
				if !strings.HasPrefix(r.Form, "package_function_call:") {
					continue
				}
				found := false
				for _, fn := range files[r.File].Go.Funcs {
					for _, st := range fn.Statements {
						if st.Line == r.Line && st.Kind == "call" && shape(st.Callee) == strings.TrimPrefix(r.Form, "package_function_call:") {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("no call statement for %s at %s:%d", r.Form, r.File, r.Line)
				}
			}
			t.Logf("%s: %d Go files, gates %v, %d payload bytes (non-test), %d inventory rows checked", corpus, len(files), gates, payloadBytes, len(inventory[corpus]))
		})
	}
}

func gormInventory(t *testing.T) map[string][]gormRow {
	t.Helper()
	f, err := os.Open("../../../docs/contract/claims/data/gorm-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]gormRow{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r gormRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Scope == "read" {
			out[r.Corpus] = append(out[r.Corpus], r)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// gormCapture parses every Go file the scanner finds and returns the facts
// with their payload's JSON size.
func gormCapture(t *testing.T, root string) (map[string]facts.Facts, map[string]int) {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var files []scan.File
	var jobs []parse.Job
	for _, f := range scanned.Files {
		if f.Record.Language == "go" {
			files = append(files, f)
			jobs = append(jobs, parse.Job{Language: "go", Source: f.Source})
		}
	}
	out := map[string]facts.Facts{}
	sizes := map[string]int{}
	err = parse.Consume(context.Background(), jobs, parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, result parse.Result) {
		if result.Err != nil {
			t.Errorf("%s: %v", files[i].Record.Path, result.Err)
			return
		}
		f := Extract(files[i].Record.Path, files[i].Source, result.Root)
		if f.Go == nil || !f.Go.ModelsValid() {
			t.Errorf("%s: missing or invalid payload", files[i].Record.Path)
			return
		}
		data, _ := json.Marshal(f.Go)
		sizes[files[i].Record.Path] = len(data)
		out[files[i].Record.Path] = f
	})
	if err != nil {
		t.Fatal(err)
	}
	return out, sizes
}

// gormFile is the census view of one non-test file: its import aliases
// (named as the inventory scanner names them) and package path.
type gormFile struct {
	path    string
	facts   facts.Facts
	imports map[string]string
	pkgPath string
}

// deriveGormRows rebuilds the inventory's read rows from the payload, with
// the inventory scanner's classification (the investigation harness `inv`).
func deriveGormRows(t *testing.T, root string, all map[string]facts.Facts) map[string]int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	modPath := ModulePaths(map[string][]byte{"go.mod": data})["."]
	var files []*gormFile
	for _, p := range facts.Paths(all) {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		g := &gormFile{path: p, facts: all[p], imports: map[string]string{}, pkgPath: modPath + "/" + path.Dir(p)}
		if path.Dir(p) == "." {
			g.pkgPath = modPath
		}
		for _, imp := range all[p].Imports {
			name := path.Base(imp.Module)
			if imp.ExplicitAlias {
				for alias := range imp.Bindings {
					name = alias
				}
			} else if strings.HasPrefix(name, "v") && len(name) <= 3 {
				name = path.Base(path.Dir(imp.Module))
			}
			g.imports[name] = imp.Module
		}
		files = append(files, g)
	}
	rows := map[string]int{}
	emit := func(row, form, file string, line int, id, name string) {
		rows[gormRow{Row: row, Form: form, File: file, Line: line, ID: id, Name: name}.key()]++
	}
	type structAt struct {
		file *gormFile
		s    facts.GoStruct
	}
	structs := map[string]structAt{}
	for _, f := range files {
		for _, s := range f.facts.Go.Structs {
			structs[f.pkgPath+"."+s.Name] = structAt{f, s}
		}
	}
	reached := map[string]bool{}
	for _, f := range files {
		configs := map[string]bool{}
		note := func(e facts.GoExpr, line int) {
			walkGoExpr(e, func(x facts.GoExpr) {
				if x.Kind == facts.GoComposite && x.Type != nil && x.Type.Kind == facts.GoSelector && x.Type.Name == "Config" && x.Type.Object.Kind == facts.GoIdentifier && f.imports[x.Type.Object.Name] == "gorm.io/gorm" {
					var keys []string
					for _, el := range x.Elements {
						if el.Key == nil {
							continue
						}
						v := "nonliteral"
						if el.Value.Kind == facts.GoBoolean {
							v = el.Value.Value
						}
						keys = append(keys, shape(*el.Key)+"="+v)
					}
					configs[fmt.Sprintf("%d|%s", line, strings.Join(keys, ","))] = true
				}
			})
		}
		for _, v := range f.facts.Go.Vars {
			for _, e := range v.Values {
				note(e, v.Line)
			}
		}
		for _, fn := range f.facts.Go.Funcs {
			for _, l := range fn.Locals {
				for _, e := range l.Values {
					note(e, l.Line)
				}
			}
			for _, s := range fn.Sites {
				note(s.Call, s.Line)
				callee := s.Call.Callee
				if callee.Kind != facts.GoSelector {
					continue
				}
				name := callee.Name
				if x := callee.Object; x.Kind == facts.GoIdentifier {
					p := f.imports[x.Name]
					switch {
					case p == "gorm.io/gorm" && name == "Open":
						drv := "nonliteral"
						if len(s.Call.Args) > 0 {
							if c := s.Call.Args[0]; c.Kind == facts.GoCall && c.Callee.Kind == facts.GoSelector && c.Callee.Object.Kind == facts.GoIdentifier {
								drv = f.imports[c.Callee.Object.Name] + "." + c.Callee.Name
							}
						}
						cfg := "none"
						if len(s.Call.Args) > 1 {
							cfg = configForm(s.Call.Args[1])
						}
						emit("GO-K2", "gorm.Open(dialector="+drv+", config="+cfg+")", f.path, s.Line, "", "")
						continue
					case p != "" && (name == "AutoMigrate" || name == "CreateTable"):
						emit("GO-K3", "package_function_call:"+x.Name+"."+name, f.path, s.Line, p, "")
						continue
					}
				}
				if name != "AutoMigrate" && name != "CreateTable" {
					continue
				}
				emit("GO-K3", "migrate_site:"+name+":receiver="+receiverForm(fn, *callee.Object), f.path, s.Line, fmt.Sprintf("%d args", len(s.Call.Args)), "")
				for _, a := range s.Call.Args {
					form, key := argForm(f, a)
					emit("GO-K3", "migrate_arg:"+form, f.path, s.Line, key, "")
					if key != "" {
						reached[key] = true
					}
				}
			}
		}
		for k := range configs {
			parts := strings.SplitN(k, "|", 2)
			line, _ := strconv.Atoi(parts[0])
			emit("GO-K2", "gorm.Config literal{"+parts[1]+"}", f.path, line, "", "")
		}
	}
	for key := range reached {
		s, ok := structs[key]
		if !ok {
			continue
		}
		emit("GO-M1", "struct:migrate_arg", s.file.path, s.s.Line, s.s.Name, "")
		for _, fl := range s.s.Fields {
			names := fl.Names
			if fl.Embedded {
				typ := fl.Type
				if fl.Pointer {
					typ = facts.GoExpr{Kind: facts.GoPointer, Object: &fl.Type}
				}
				names = []string{"(embedded " + typeForm(s.file, typ) + ")"}
			}
			for _, n := range names {
				id := s.s.Name + "." + n
				typ := fl.Type
				if fl.Pointer {
					typ = facts.GoExpr{Kind: facts.GoPointer, Object: &fl.Type}
				}
				emit("GO-T1", "go_type:"+typeForm(s.file, typ), s.file.path, fl.Line, id, "")
				g, ok := reflect.StructTag(fl.Tag).Lookup("gorm")
				if !ok {
					continue
				}
				emit("GO-F1", "gorm_tag", s.file.path, fl.Line, id, "")
				for _, part := range strings.Split(g, ";") {
					if strings.TrimSpace(part) == "" {
						continue
					}
					kv := strings.SplitN(part, ":", 2)
					key := strings.TrimSpace(strings.ToUpper(kv[0]))
					val := key
					if len(kv) == 2 {
						val = kv[1]
					}
					row := gormTagRows[key]
					if row == "" {
						row = "D"
					}
					name := ""
					if key == "INDEX" || key == "UNIQUEINDEX" || key == "COLUMN" || key == "MANY2MANY" {
						name = strings.Split(val, ",")[0]
					}
					emit(row, "tag:"+strings.ToLower(key)+"="+tagShape(key, val), s.file.path, fl.Line, id, name)
				}
			}
		}
	}
	return rows
}

// gormTagRows is the inventory scanner's tag key to contract row map.
var gormTagRows = map[string]string{
	"COLUMN": "GO-F2", "TYPE": "GO-T1", "SIZE": "GO-T1", "PRECISION": "GO-T1", "SCALE": "GO-T1", "SERIALIZER": "GO-T1",
	"PRIMARYKEY": "GO-C1", "PRIMARY_KEY": "GO-C1", "AUTOINCREMENT": "GO-C1", "AUTOINCREMENTINCREMENT": "GO-C1",
	"NOT NULL": "GO-C2", "NOTNULL": "GO-C2", "DEFAULT": "GO-C3", "UNIQUE": "GO-C4", "INDEX": "GO-I1", "UNIQUEINDEX": "GO-I1",
	"-": "GO-F1", "->": "GO-F1", "<-": "GO-F1", "EMBEDDED": "GO-F1", "EMBEDDEDPREFIX": "GO-F1", "COMMENT": "GO-F1", "AUTOCREATETIME": "GO-F1", "AUTOUPDATETIME": "GO-F1",
	"FOREIGNKEY": "GO-R1", "REFERENCES": "GO-R1", "BELONGSTO": "GO-R1", "CONSTRAINT": "GO-R3", "MANY2MANY": "GO-R2", "JOINFOREIGNKEY": "GO-R2", "JOINREFERENCES": "GO-R2", "JSON": "GO-T1",
}

func tagShape(k, v string) string {
	switch k {
	case "INDEX", "UNIQUEINDEX":
		if v == k {
			return "flag"
		}
		parts := strings.Split(v, ",")
		s := "name"
		if parts[0] == "" {
			s = "unnamed"
		}
		for _, p := range parts[1:] {
			s += "," + strings.ToLower(strings.SplitN(p, ":", 2)[0])
		}
		return s
	case "TYPE":
		return "type:" + strings.ToLower(v)
	}
	if v == k {
		return "flag"
	}
	if _, err := strconv.Atoi(v); err == nil {
		return "int"
	}
	return "value"
}

func walkGoExpr(e facts.GoExpr, visit func(facts.GoExpr)) {
	visit(e)
	for _, x := range []*facts.GoExpr{e.Object, e.Callee, e.Type, e.Left, e.Right} {
		if x != nil {
			walkGoExpr(*x, visit)
		}
	}
	for _, a := range e.Args {
		walkGoExpr(a, visit)
	}
	for _, el := range e.Elements {
		if el.Key != nil {
			walkGoExpr(*el.Key, visit)
		}
		walkGoExpr(el.Value, visit)
	}
}

func typeForm(f *gormFile, e facts.GoExpr) string {
	switch e.Kind {
	case facts.GoPointer:
		return "*" + typeForm(f, *e.Object)
	case facts.GoSlice, facts.GoArray:
		return "[]" + typeForm(f, *e.Object)
	case facts.GoMap:
		return "map"
	case facts.GoIdentifier:
		switch e.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "byte", "rune", "any":
			return e.Name
		}
		return "local:" + e.Name
	case facts.GoSelector:
		x := shape(*e.Object)
		p := f.imports[x]
		if p == "time" || p == "database/sql" || p == "gorm.io/gorm" {
			return x + "." + e.Name
		}
		return "pkg:" + p + "." + e.Name
	case facts.GoInterface:
		return "interface"
	}
	return e.Kind
}

func configForm(e facts.GoExpr) string {
	switch {
	case e.Kind == facts.GoUnary && e.Object.Kind == facts.GoComposite:
		return "&literal"
	case e.Kind == facts.GoComposite:
		return "literal"
	case e.Kind == facts.GoIdentifier:
		return "ident"
	}
	return e.Kind
}

func argForm(f *gormFile, a facts.GoExpr) (string, string) {
	typeKey := func(t *facts.GoExpr) (string, string) {
		switch {
		case t == nil:
		case t.Kind == facts.GoIdentifier:
			return "local", f.pkgPath + "." + t.Name
		case t.Kind == facts.GoSelector && t.Object.Kind == facts.GoIdentifier:
			if p := f.imports[t.Object.Name]; p != "" {
				return "qualified", p + "." + t.Name
			}
		}
		return "other", ""
	}
	switch {
	case a.Kind == facts.GoCall && a.Callee.Kind == facts.GoIdentifier && a.Callee.Name == "new" && len(a.Args) == 1:
		q, k := typeKey(&a.Args[0])
		return "new(" + q + ")", k
	case a.Kind == facts.GoUnary && a.Value == "&" && a.Object.Kind == facts.GoComposite:
		q, k := typeKey(a.Object.Type)
		if len(a.Object.Elements) > 0 {
			q += ",fields"
		}
		return "&" + q + "{}", k
	case a.Kind == facts.GoComposite:
		q, k := typeKey(a.Type)
		return q + "{}", k
	}
	return "other:" + a.Kind, ""
}

// receiverForm classifies a migrate receiver by the declarations of its
// enclosing function, as the inventory scanner does (the last matching
// declaration wins, parameters before the body).
func receiverForm(fn facts.GoFunc, e facts.GoExpr) string {
	switch e.Kind {
	case facts.GoIdentifier:
		form := "ident:undeclared(package-level or outer)"
		params := append([]facts.GoParam(nil), fn.Params...)
		if fn.Receiver != nil {
			params = append([]facts.GoParam{*fn.Receiver}, params...)
		}
		for _, p := range append(params, fn.Results...) {
			if p.Name == e.Name {
				form = "ident:param_typed(" + shape(p.Type) + ")"
			}
		}
		for _, l := range fn.Locals {
			for i, n := range l.Names {
				if n != e.Name {
					continue
				}
				switch {
				case l.Kind == "var" && l.Type != nil:
					form = "ident:var_typed(" + shape(*l.Type) + ")"
				case l.Kind == "define" && len(l.Values) == 1 && l.Values[0].Kind == facts.GoCall:
					form = fmt.Sprintf("ident:define_from_call(%s,result%d)", shape(*l.Values[0].Callee), i)
				}
			}
		}
		return form
	case facts.GoCall:
		return "chain:" + shape(*e.Callee)
	case facts.GoSelector:
		return "selector:" + shape(e)
	}
	return e.Kind
}
