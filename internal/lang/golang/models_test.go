package golang

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// parseGo parses one Go source and extracts its facts with the payload.
func parseGo(t *testing.T, file, source string) facts.Facts {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "go", Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err, results[0].Err)
	}
	f := Extract(file, []byte(source), results[0].Root)
	if f.Go == nil || !f.Go.ModelsValid() {
		t.Fatalf("%s: missing or invalid payload %+v", file, f.Go)
	}
	return f
}

// shape renders a reduced expression compactly, so tests can state it as Go
// source.
func shape(e facts.GoExpr) string {
	list := func(items []facts.GoExpr) string {
		var parts []string
		for _, i := range items {
			parts = append(parts, shape(i))
		}
		return strings.Join(parts, ", ")
	}
	switch e.Kind {
	case facts.GoIdentifier:
		return e.Name
	case facts.GoSelector:
		return shape(*e.Object) + "." + e.Name
	case facts.GoCall:
		return shape(*e.Callee) + "(" + list(e.Args) + ")"
	case facts.GoComposite:
		var parts []string
		for _, el := range e.Elements {
			if el.Key != nil {
				parts = append(parts, shape(*el.Key)+": "+shape(el.Value))
			} else {
				parts = append(parts, shape(el.Value))
			}
		}
		typ := ""
		if e.Type != nil {
			typ = shape(*e.Type)
		}
		return typ + "{" + strings.Join(parts, ", ") + "}"
	case facts.GoUnary:
		return e.Value + shape(*e.Object)
	case facts.GoBinary:
		return shape(*e.Left) + " " + e.Value + " " + shape(*e.Right)
	case facts.GoString:
		return `"` + e.Value + `"`
	case facts.GoNumber, facts.GoBoolean:
		return e.Value
	case facts.GoNil:
		return "nil"
	case facts.GoSpread:
		return shape(*e.Object) + "..."
	case facts.GoIndex:
		return shape(*e.Object) + "[" + list(e.Args) + "]"
	case facts.GoPointer:
		return "*" + shape(*e.Object)
	case facts.GoSlice:
		return "[]" + shape(*e.Object)
	case facts.GoArray:
		return "[" + e.Value + "]" + shape(*e.Object)
	case facts.GoMap:
		return "map[" + shape(*e.Left) + "]" + shape(*e.Right)
	}
	return "<" + e.Kind + ">"
}

const gormImport = "package p\n\nimport \"gorm.io/gorm\"\n\n"

// TestGoPayloadTypeShapes covers the type shapes every Go file carries:
// struct tags, embedding, named types, the methods GORM reads and TableName
// bodies, and string constants (T018g5 Revision 1, O7).
func TestGoPayloadTypeShapes(t *testing.T) {
	f := parseGo(t, "model/user.go", "package model\n\nimport \"time\"\n\n"+
		"const prefix = \"app_\"\nconst (\n\tusers, other = prefix + \"users\", `raw`\n\tn = 3\n\tk Kind = iota\n)\n\n"+
		"// User is a model.\ntype User struct {\n"+
		"\tID uint `gorm:\"primaryKey;autoIncrement\"`\n"+
		"\tName, Nick string `gorm:\"type:varchar(180);uniqueIndex:uix_users_name\" json:\"name\"`\n"+
		"\tBase\n\t*Audit `gorm:\"embedded;embeddedPrefix:a_\"`\n\tgorm.Model\n"+
		"\tSeen *time.Time\n\tApps []Application `gorm:\"foreignKey:UserID\"`\n"+
		"\tTags map[string][]byte\n\tKey [16]byte\n\tFn func()\n\tAny interface{}\n\tAnon struct{ X int }\n\tCh chan int\n\tBox Box[int]\n\tlower int\n}\n\n"+
		"type (\n\tKind int\n\tAlias = User\n\tBox[T any] struct{ V T }\n)\n\n"+
		"func (User) TableName() string { return users }\n"+
		"func (a *Audit) TableName() string {\n\tif a == nil {\n\t\treturn \"x\"\n\t}\n\treturn \"y\"\n}\n"+
		"func (k Kind) Value() (driver.Value, error) { return int(k), nil }\n"+
		"func (k *Kind) Scan(v any) error { return nil }\n"+
		"func (Kind) GormDataType() string { return \"int\" }\n"+
		"func (k Kind) String() string { return \"\" }\n"+
		"func helper() {}\n")
	g := f.Go
	if g.Gated || len(g.Funcs) > 0 || len(g.Vars) > 0 {
		t.Fatalf("ungated file has sites: %+v", g)
	}
	if len(g.Structs) != 2 || g.Structs[0].Name != "User" || g.Structs[0].Line != 13 || g.Structs[0].EndLine != 29 || g.Structs[1].Name != "Box" || !reflect.DeepEqual(g.Structs[1].TypeParams, []string{"T"}) {
		t.Fatalf("structs = %+v", g.Structs)
	}
	var fields []string
	for _, fl := range g.Structs[0].Fields {
		s := strings.Join(fl.Names, ",") + " " + shape(fl.Type)
		if fl.Embedded {
			s = "embedded"
			if fl.Pointer {
				s += " *"
			}
			s += " " + shape(fl.Type)
		}
		if fl.Tag != "" {
			s += " `" + fl.Tag + "`"
		}
		fields = append(fields, s)
	}
	want := []string{
		"ID uint `gorm:\"primaryKey;autoIncrement\"`",
		"Name,Nick string `gorm:\"type:varchar(180);uniqueIndex:uix_users_name\" json:\"name\"`",
		"embedded Base",
		"embedded * Audit `gorm:\"embedded;embeddedPrefix:a_\"`",
		"embedded gorm.Model",
		"Seen *time.Time",
		"Apps []Application `gorm:\"foreignKey:UserID\"`",
		"Tags map[string][]byte",
		"Key [16]byte",
		"Fn <func>",
		"Any <interface>",
		"Anon <struct>",
		"Ch <chan>",
		"Box Box[int]",
		"lower int",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields:\n%s\nwant:\n%s", strings.Join(fields, "\n"), strings.Join(want, "\n"))
	}
	var types []string
	for _, ty := range g.Types {
		types = append(types, ty.Name+" "+shape(ty.Type)+" alias="+map[bool]string{true: "yes", false: "no"}[ty.Alias])
	}
	if !reflect.DeepEqual(types, []string{"Kind int alias=no", "Alias User alias=yes"}) {
		t.Fatalf("types = %v", types)
	}
	var methods []string
	for _, m := range g.Methods {
		s := m.Receiver + "." + m.Name
		if m.Pointer {
			s = "*" + s
		}
		s += " " + strings.Repeat("p", m.Params) + strings.Repeat("r", m.Results)
		if m.Return != nil {
			s += " return " + shape(*m.Return)
		}
		methods = append(methods, s)
	}
	// String and helper are not read; the second TableName has two returns.
	if !reflect.DeepEqual(methods, []string{"User.TableName r return users", "*Audit.TableName r", "Kind.Value rr", "*Kind.Scan pr", "Kind.GormDataType r"}) {
		t.Fatalf("methods = %v", methods)
	}
	var consts []string
	for _, c := range g.Consts {
		consts = append(consts, c.Name+" = "+shape(c.Value))
	}
	if !reflect.DeepEqual(consts, []string{`prefix = "app_"`, `users = prefix + "users"`, `other = "raw"`}) {
		t.Fatalf("consts = %v", consts)
	}
}

// TestGoPayloadSites covers the gated capture on the forms the corpora use
// (gotify's switch over three dialectors and one AutoMigrate on a var-typed
// receiver; realworld's *gorm.DB parameter, a function result receiver and
// the users.AutoMigrate() call statement) and the receiver chains,
// configuration literals, mutations and nesting marks of the contract.
func TestGoPayloadSites(t *testing.T) {
	f := parseGo(t, "database/database.go", gormImport+
		"var DB *gorm.DB\nvar cfg, other = &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true}, 1\n\n"+
		"func New(dialect, conn string) (*GormDatabase, error) {\n"+
		"\tgormConfig := &gorm.Config{\n\t\tLogger: logger.Default,\n\t\tNamingStrategy: schema.NamingStrategy{TablePrefix: \"t_\", SingularTable: true},\n\t}\n"+
		"\tvar db *gorm.DB\n\tvar err error\n"+
		"\tswitch dialect {\n\tcase \"mysql\":\n\t\tdb, err = gorm.Open(mysql.Open(conn), gormConfig)\n\tcase \"sqlite3\":\n\t\tdb, err = gorm.Open(sqlite.Open(conn), gormConfig)\n\t}\n"+
		"\tif err != nil {\n\t\treturn nil, err\n\t}\n"+
		"\tif err := db.AutoMigrate(new(model.User), &model.Application{}, Client{}, models...); err != nil {\n\t\treturn nil, err\n\t}\n"+
		"\tdb.Debug().WithContext(ctx).Set(\"gorm:table_options\", \"ENGINE=InnoDB\").Session(&gorm.Session{DryRun: false}).Migrator().CreateTable(&User{})\n"+
		"\tusers.AutoMigrate()\n\tMigrate(db)\n\tdb.Config.NamingStrategy = nil\n\tgormConfig.TablePrefix, x = \"p_\", 2\n\tcount += 1\n"+
		"\tgo func(tx *gorm.DB) { tx.AutoMigrate(&Late{}) }(db)\n"+
		"\treturn &GormDatabase{DB: db}, common.GetDB().AutoMigrate(&Last{})\n}\n\n"+
		"func (d *GormDatabase) Close() { d.DB.Migrator() }\n"+
		"func GetDB() *gorm.DB { return DB }\n")
	g := f.Go
	if !g.Gated || len(g.Funcs) != 3 {
		t.Fatalf("gated payload = %+v", g)
	}
	var vars []string
	for _, v := range g.Vars {
		s := strings.Join(v.Names, ",")
		if v.Type != nil {
			s += " " + shape(*v.Type)
		}
		for _, val := range v.Values {
			s += " = " + shape(val)
		}
		vars = append(vars, s)
	}
	if !reflect.DeepEqual(vars, []string{"DB *gorm.DB", "cfg,other = &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true} = 1"}) {
		t.Fatalf("vars = %v", vars)
	}
	fn := g.Funcs[0]
	var params []string
	for _, p := range append(append([]facts.GoParam(nil), fn.Params...), fn.Results...) {
		params = append(params, p.Name+" "+shape(p.Type))
	}
	if fn.Name != "New" || fn.Receiver != nil || !reflect.DeepEqual(params, []string{"dialect string", "conn string", " *GormDatabase", " error"}) {
		t.Fatalf("New = %s %v", fn.Name, params)
	}
	var locals []string
	for _, l := range fn.Locals {
		s := l.Kind + " " + strings.Join(l.Names, ",")
		if l.Type != nil {
			s += " " + shape(*l.Type)
		}
		for _, v := range l.Values {
			s += " = " + shape(v)
		}
		locals = append(locals, s+" @"+l.Nesting)
	}
	wantLocals := []string{
		`define gormConfig = &gorm.Config{Logger: logger.Default, NamingStrategy: schema.NamingStrategy{TablePrefix: "t_", SingularTable: true}} @unconditional`,
		"var db *gorm.DB @unconditional",
		"assign db,err = gorm.Open(mysql.Open(conn), gormConfig) @conditional",
		"assign db,err = gorm.Open(sqlite.Open(conn), gormConfig) @conditional",
	}
	// err is used by no site, so its declarations are not kept.
	if !reflect.DeepEqual(locals, wantLocals) {
		t.Fatalf("locals:\n%s\nwant:\n%s", strings.Join(locals, "\n"), strings.Join(wantLocals, "\n"))
	}
	var sites []string
	for _, s := range fn.Sites {
		sites = append(sites, shape(s.Call)+" @"+s.Nesting)
	}
	wantSites := []string{
		"gorm.Open(mysql.Open(conn), gormConfig) @conditional",
		"gorm.Open(sqlite.Open(conn), gormConfig) @conditional",
		"db.AutoMigrate(new(model.User), &model.Application{}, Client{}, models...) @if-header",
		`db.Debug().WithContext(ctx).Set("gorm:table_options", "ENGINE=InnoDB").Session(&gorm.Session{DryRun: false}).Migrator().CreateTable(&User{}) @unconditional`,
		"users.AutoMigrate() @unconditional",
		"tx.AutoMigrate(&Late{}) @closure",
		"common.GetDB().AutoMigrate(&Last{}) @unconditional",
	}
	if !reflect.DeepEqual(sites, wantSites) {
		t.Fatalf("sites:\n%s\nwant:\n%s", strings.Join(sites, "\n"), strings.Join(wantSites, "\n"))
	}
	var stmts []string
	for _, s := range fn.Statements {
		stmts = append(stmts, s.Kind+" "+shape(s.Callee))
	}
	if !reflect.DeepEqual(stmts, []string{"if db.AutoMigrate", "call users.AutoMigrate", "call Migrate"}) {
		t.Fatalf("statements = %v", stmts)
	}
	if fn.Statements[1].Index+1 != fn.Statements[2].Index || fn.Sites[4].Statement != fn.Statements[1].Index {
		t.Fatalf("statement indexes: %+v, sites %+v", fn.Statements, fn.Sites[4])
	}
	var mutations []string
	for _, m := range fn.Mutations {
		s := shape(m.Target)
		if m.Value != nil {
			s += " = " + shape(*m.Value)
		}
		mutations = append(mutations, s)
	}
	if !reflect.DeepEqual(mutations, []string{"db.Config.NamingStrategy = nil", `gormConfig.TablePrefix = "p_"`}) {
		t.Fatalf("mutations = %v", mutations)
	}
	closeFn, getDB := g.Funcs[1], g.Funcs[2]
	if closeFn.Receiver == nil || shape(closeFn.Receiver.Type) != "*GormDatabase" || closeFn.Receiver.Name != "d" || len(closeFn.Sites) != 1 || shape(closeFn.Sites[0].Call) != "d.DB.Migrator()" {
		t.Fatalf("Close = %+v", closeFn)
	}
	if getDB.Name != "GetDB" || len(getDB.Results) != 1 || shape(getDB.Results[0].Type) != "*gorm.DB" {
		t.Fatalf("GetDB = %+v", getDB)
	}
}

// TestGoPayloadInflection covers inflection calls through the import's local
// name and the gate on every listed import path.
func TestGoPayloadInflection(t *testing.T) {
	f := parseGo(t, "a.go", "package p\n\nimport infl \"github.com/jinzhu/inflection\"\n\nfunc init() {\n\tinfl.AddIrregular(\"person\", \"people\")\n\tinflection.AddUncountable(\"x\")\n}\n")
	if len(f.Go.Funcs) != 1 || len(f.Go.Funcs[0].Sites) != 1 || shape(f.Go.Funcs[0].Sites[0].Call) != `infl.AddIrregular("person", "people")` {
		t.Fatalf("inflection sites = %+v", f.Go.Funcs)
	}
	for module, gated := range map[string]bool{"gorm.io/gorm": true, "gorm.io/gorm/schema": true, "gorm.io/driver/postgres": true, "github.com/glebarez/sqlite": true, "github.com/jinzhu/inflection": true, "github.com/jinzhu/gorm": true, "github.com/jinzhu/gorm/dialects/mysql": true, "gorm.io/datatypes": false, "gorm.io/gormx": false, "github.com/glebarez/go-sqlite": false} {
		if gormGated(module) != gated {
			t.Errorf("gate(%s) = %v, want %v", module, !gated, gated)
		}
	}
}

// TestGoPayloadComments checks that comments between elements, arguments,
// fields and statements never become payload entries.
func TestGoPayloadComments(t *testing.T) {
	f := parseGo(t, "a.go", gormImport+"type T struct {\n\t// c\n\tA int // c\n\t/* c */ B string\n}\n\nfunc f(db *gorm.DB /* c */) {\n\t// c\n\tdb.AutoMigrate(\n\t\t// c\n\t\t&T{ /* c */ A: 1, // c\n\t\t},\n\t)\n}\n")
	if len(f.Go.Structs[0].Fields) != 2 || shape(f.Go.Funcs[0].Sites[0].Call) != "db.AutoMigrate(&T{A: 1})" || len(f.Go.Funcs[0].Params) != 1 {
		t.Fatalf("payload = %+v", f.Go)
	}
}

// TestGoPayloadDepth checks the depth bound and that the payload survives a
// JSON round trip as a valid cache entry, while damaged entries are not.
func TestGoPayloadDepth(t *testing.T) {
	deep := strings.Repeat("f(", 60) + "x" + strings.Repeat(")", 60)
	f := parseGo(t, "a.go", gormImport+"var v = "+deep+"\n")
	e := f.Go.Vars[0].Values[0]
	depth := 0
	for e.Kind == facts.GoCall {
		e = e.Args[0]
		depth++
	}
	if e.Kind != facts.GoUnknown || depth != facts.GoMaxDepth {
		t.Fatalf("cut at depth %d to %s", depth, e.Kind)
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var back facts.Facts
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back.Go, f.Go) || !back.Go.ModelsValid() {
		t.Fatal("round trip changed the payload", err)
	}
	damage := map[string]func(*facts.GoFacts){
		"version":       func(g *facts.GoFacts) { g.Version++ },
		"ungated funcs": func(g *facts.GoFacts) { g.Gated = false },
		"bad kind":      func(g *facts.GoFacts) { g.Vars[0].Values[0].Kind = "lambda" },
		"no callee":     func(g *facts.GoFacts) { g.Vars[0].Values[0].Callee = nil },
		"struct line":   func(g *facts.GoFacts) { g.Structs = append(g.Structs, facts.GoStruct{Name: "S"}) },
		"site nesting": func(g *facts.GoFacts) {
			g.Funcs = []facts.GoFunc{{Name: "f", Line: 1, EndLine: 1, Sites: []facts.GoSite{{Call: g.Vars[0].Values[0], Line: 1}}}}
		},
	}
	for name, mutate := range damage {
		var g facts.GoFacts
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		g = *back.Go
		mutate(&g)
		if g.ModelsValid() {
			t.Errorf("%s: damaged payload is valid", name)
		}
	}
}

// TestGoCaptureFailure checks that a capture failure keeps the file's other
// facts and reports a warning; the indexer then misses on the cache entry.
func TestGoCaptureFailure(t *testing.T) {
	goCaptureFunc = func(string, *parse.Node, []facts.Import) *facts.GoFacts { panic("boom") }
	defer func() { goCaptureFunc = captureGo }()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "go", Source: []byte("package p\n\nfunc F() {}\n")}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	f := Extract("a.go", []byte("package p\n\nfunc F() {}\n"), results[0].Root)
	if f.Go != nil || len(f.Nodes) != 1 || len(f.Diagnostics) != 1 || f.Diagnostics[0].Code != "model_syntax_capture_failed" {
		t.Fatalf("facts after failure = %+v", f)
	}
}

// goPayloadRows maps every payload-table row id (docs/lang/go-models.md) to
// the fields its capture branch fills; rows without fields capture nothing.
var goPayloadRows = map[string][]string{
	"G1":              {"Version", "Gated"},
	"GD":              {"Version"},
	"GT":              {"Version"},
	"T-struct":        {"Name", "Line", "EndLine", "TypeParams", "Fields"},
	"T-field":         {"Names", "Type", "Tag", "Line"},
	"T-embedded":      {"Embedded", "Pointer", "Type", "Tag", "Line"},
	"T-type":          {"Name", "TypeParams", "Type", "Line"},
	"T-alias":         {"Name", "Alias", "Type", "Line"},
	"T-interface":     {"Name", "Type", "Methods", "Line"},
	"T-method":        {"Receiver", "Pointer", "Name", "Params", "Results", "Line"},
	"T-tablename":     {"Receiver", "Pointer", "Name", "Results", "Return", "Line"},
	"T-const":         {"Name", "Type", "Value", "Line"},
	"TD":              nil,
	"V1":              {"Names", "Type", "Values", "Line"},
	"F1":              {"Name", "Receiver", "Line", "EndLine", "Params", "Results", "Locals", "Sites", "Statements", "Mutations", "LocalTypes", "Returns", "Calls", "Stores", "Literals"},
	"F-param":         {"Name", "Type", "Variadic"},
	"L-var":           {"Kind", "Names", "Type", "Values", "Line", "Statement", "Nesting"},
	"L-define":        {"Kind", "Names", "Values", "Line", "Statement", "Nesting"},
	"L-assign":        {"Kind", "Names", "Values", "Line", "Statement", "Nesting"},
	"X-mutation":      {"Target", "Value", "Line", "Statement", "Nesting"},
	"X-site":          {"Call", "Line", "Statement", "Nesting"},
	"X-inflection":    {"Call", "Line", "Statement", "Nesting"},
	"X-statement":     {"Index", "Line", "Kind", "Callee"},
	"L-type":          {"Name"},
	"X-return":        {"Value", "Line", "Nesting"},
	"X-call":          {"Call", "Line", "Statement", "Nesting"},
	"X-store":         {"Target", "Value", "Line", "Statement", "Nesting"},
	"X-literal":       {"Call", "Line", "Statement", "Nesting"},
	"N-unconditional": {"Nesting"},
	"N-if-header":     {"Nesting"},
	"N-conditional":   {"Nesting"},
	"N-closure":       {"Nesting"},
	"FD":              nil,
	"E-identifier":    {"Kind", "Name"},
	"E-selector":      {"Kind", "Name", "Object"},
	"E-call":          {"Kind", "Callee", "Args"},
	"E-composite":     {"Kind", "Type", "Elements"},
	"E-keyed":         {"Key", "Value"},
	"E-positional":    {"Value"},
	"E-unary":         {"Kind", "Value", "Object"},
	"E-binary":        {"Kind", "Value", "Left", "Right"},
	"E-string":        {"Kind", "Value"},
	"E-number":        {"Kind", "Value"},
	"E-boolean":       {"Kind", "Value"},
	"E-nil":           {"Kind"},
	"E-spread":        {"Kind", "Object"},
	"E-index":         {"Kind", "Object", "Args"},
	"E-pointer":       {"Kind", "Object"},
	"E-slice":         {"Kind", "Object"},
	"E-array":         {"Kind", "Value", "Object"},
	"E-map":           {"Kind", "Left", "Right"},
	"E-chan":          {"Kind"},
	"E-func":          {"Kind"},
	"E-interface":     {"Kind"},
	"E-struct":        {"Kind"},
	"E-paren":         {"Kind", "Name"},
	"E-unknown":       {"Kind"},
}

// goSnippets holds one source per row; the first record the capture tags
// with the row must fill exactly the row's fields. Together they emit every
// row of the table (TD and FD included).
var goSnippets = map[string]string{
	"G1":              gormImport,
	"GD":              "package p\n",
	"GT":              gormImport + "type T struct{ A int }\n",
	"T-struct":        "package p\n\ntype S[T any] struct{ A int }\n",
	"T-field":         "package p\n\ntype S struct {\n\tA int `gorm:\"x\"`\n}\n",
	"T-embedded":      "package p\n\ntype S struct {\n\t*B `gorm:\"embedded\"`\n}\n",
	"T-type":          "package p\n\ntype N[T any] []T\n",
	"T-alias":         "package p\n\ntype A = string\n",
	"T-interface":     "package p\n\ntype P interface {\n\tRouter() any\n}\n",
	"T-method":        "package p\n\nfunc (n *N) Scan(v any) error { return nil }\n",
	"T-tablename":     "package p\n\nfunc (s *S) TableName() string { return \"s\" }\n",
	"T-const":         "package p\n\nconst c string = \"x\"\n",
	"TD":              "package p\n\nconst n = 1\nvar v = 1\n\nfunc f() {}\nfunc (s S) String() string { return \"\" }\n",
	"V1":              gormImport + "var db *gorm.DB = nil\n",
	"F1":              gormImport + "func (r *R) M(db *gorm.DB) *gorm.DB {\n\tx := 1\n\ttype T struct{}\n\tf(x)\n\tdb.AutoMigrate(x)\n\tr.Config = nil\n\tr.db = db\n\t_ = T{A: db}\n\treturn db\n}\n",
	"F-param":         gormImport + "func f(a ...int) {}\n",
	"L-var":           gormImport + "func f() {\n\tg()\n\tvar x int = 1\n\tdb.AutoMigrate(x)\n}\n",
	"L-define":        gormImport + "func f() {\n\tg()\n\tx := 1\n\tdb.AutoMigrate(x)\n}\n",
	"L-assign":        gormImport + "func f() {\n\tg()\n\tx = 1\n\tdb.AutoMigrate(x)\n}\n",
	"X-mutation":      gormImport + "func f() {\n\tg()\n\tc.NamingStrategy = nil\n}\n",
	"X-site":          gormImport + "func f() {\n\tg()\n\tdb.AutoMigrate()\n}\n",
	"X-inflection":    "package p\n\nimport \"github.com/jinzhu/inflection\"\n\nfunc f() {\n\tg()\n\tinflection.AddUncountable(\"x\")\n}\n",
	"X-statement":     gormImport + "func f() {\n\tx := 1\n\th(x)\n}\n",
	"L-type":          gormImport + "func f() {\n\ttype T struct{}\n}\n",
	"X-return":        gormImport + "func f() *gorm.DB {\n\treturn db\n}\n",
	"X-call":          gormImport + "func f() {\n\tg()\n\th(db)\n}\n",
	"X-store":         gormImport + "func f() {\n\tg()\n\tr.db = db\n}\n",
	"X-literal":       gormImport + "func f() {\n\tg()\n\t_ = T{A: db}\n}\n",
	"N-unconditional": gormImport + "func f() {\n\tdb.AutoMigrate()\n}\n",
	"N-if-header":     gormImport + "func f() {\n\tif err := db.AutoMigrate(); err != nil {\n\t}\n}\n",
	"N-conditional":   gormImport + "func f() {\n\tfor {\n\t\tdb.AutoMigrate()\n\t}\n}\n",
	"N-closure":       gormImport + "func f() {\n\tgo func() { db.AutoMigrate() }()\n}\n",
	"FD":              gormImport + "func f() {\n\tx += 1\n\ta.b = 1\n\ty := 2\n}\n\nfunc g()\n",
	"E-identifier":    gormImport + "var v = a\n",
	"E-selector":      gormImport + "var v = a.b\n",
	"E-call":          gormImport + "var v = f(a)\n",
	"E-composite":     gormImport + "var v = T{A: 1}\n",
	"E-keyed":         gormImport + "var v = T{A: 1}\n",
	"E-positional":    gormImport + "var v = T{1}\n",
	"E-unary":         gormImport + "var v = &a\n",
	"E-binary":        gormImport + "var v = a + b\n",
	"E-string":        gormImport + "var v = \"s\"\n",
	"E-number":        gormImport + "var v = 1.5\n",
	"E-boolean":       gormImport + "var v = true\n",
	"E-nil":           gormImport + "var v = nil\n",
	"E-spread":        gormImport + "var v = f(a...)\n",
	"E-index":         gormImport + "var v = a[1]\n",
	"E-pointer":       "package p\n\ntype S struct{ A *int }\n",
	"E-slice":         "package p\n\ntype S struct{ A []int }\n",
	"E-array":         "package p\n\ntype S struct{ A [2]int }\n",
	"E-map":           "package p\n\ntype S struct{ A map[int]int }\n",
	"E-chan":          "package p\n\ntype S struct{ A chan int }\n",
	"E-func":          "package p\n\ntype S struct{ A func() }\n",
	"E-interface":     "package p\n\ntype S struct{ A interface{} }\n",
	"E-struct":        "package p\n\ntype S struct{ A struct{} }\n",
	"E-paren":         gormImport + "var v = (a)\n",
	"E-unknown":       gormImport + "var v = func() {}\n",
}

type goTag struct {
	row    string
	record any
}

func captureGoTags(t *testing.T, file, source string) []goTag {
	t.Helper()
	var tags []goTag
	goModelTag = func(row string, record any) { tags = append(tags, goTag{row, record}) }
	defer func() { goModelTag = nil }()
	parseGo(t, file, source)
	return tags
}

// goPayloadTable parses the "## Payload table" rows (id -> row line).
func goPayloadTable(t *testing.T) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile("../../../docs/lang/go-models.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Payload table\n")
	if start < 0 {
		t.Fatal("no Payload table section")
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	rows := map[string]string{}
	var order []string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| id |") {
			continue
		}
		id := strings.TrimSpace(strings.SplitN(line[1:], "|", 2)[0])
		if _, dup := rows[id]; dup {
			t.Fatalf("duplicate row %s", id)
		}
		rows[id] = line
		order = append(order, id)
	}
	return rows, order
}

func goFilledFields(record any) []string {
	v := reflect.ValueOf(record)
	if v.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsZero() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	sort.Strings(out)
	return out
}

// TestGoPayloadContract keeps the payload table, goPayloadRows and the
// capture branches 1:1: every row has an entry and a snippet that fills
// exactly its fields, every branch tags a row of the table, and the snippets
// together emit every row.
func TestGoPayloadContract(t *testing.T) {
	docs, order := goPayloadTable(t)
	if len(order) != len(goPayloadRows) {
		t.Errorf("payload table rows = %d, Go table %d", len(order), len(goPayloadRows))
	}
	for _, id := range order {
		fields, ok := goPayloadRows[id]
		if !ok {
			t.Errorf("row %s has no goPayloadRows entry", id)
			continue
		}
		cells := strings.Split(docs[id], "|")
		nothing := strings.TrimSpace(cells[len(cells)-2]) == "nothing"
		if nothing != (len(fields) == 0) {
			t.Errorf("row %s: payload cell %q, Go fields %v", id, cells[len(cells)-2], fields)
		}
	}
	emitted := map[string]bool{}
	for _, id := range order {
		snippet, ok := goSnippets[id]
		if !ok {
			t.Errorf("row %s has no snippet", id)
			continue
		}
		found := false
		file := "a.go"
		if id == "GT" {
			file = "a_test.go"
		}
		for _, tag := range captureGoTags(t, file, snippet) {
			emitted[tag.row] = true
			if tag.row != id || found {
				continue
			}
			found = true
			if fields := goPayloadRows[id]; len(fields) > 0 {
				exp := append([]string(nil), fields...)
				sort.Strings(exp)
				if got := goFilledFields(tag.record); !reflect.DeepEqual(got, exp) {
					t.Errorf("row %s: filled %v, want %v (%+v)", id, got, exp, tag.record)
				}
			}
		}
		if !found {
			t.Errorf("row %s: snippet emitted no %s tag", id, id)
		}
	}
	for _, id := range order {
		if !emitted[id] {
			t.Errorf("snippets emit no %s tag", id)
		}
	}
	for id := range emitted {
		if _, ok := docs[id]; !ok {
			t.Errorf("capture emitted tag %s, which is not a payload-table row", id)
		}
	}
	for id := range goPayloadRows {
		if _, ok := docs[id]; !ok {
			t.Errorf("goPayloadRows has %s, which is not a payload-table row", id)
		}
	}
}
