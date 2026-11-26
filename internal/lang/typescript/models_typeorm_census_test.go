package typescript

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// typeormCorpus is one T018g6 corpus: its checkout name, the TypeScript
// package the inventory counted (a repository-relative prefix), and the
// gate counts of the payload decision and the TA2/TA3 sections
// (docs/contract/claims/typeorm-models.md, the T018g6 task record (internal)).
type typeormCorpus struct {
	name, dir, prefix string
	files             int            // TypeScript files in the package
	gates             map[string]int // files per gate row G10-G12
	enums             int            // enum declarations in the G12 files
	forFeature        int            // TypeOrmModule.forFeature calls in non-test files
}

var typeormCorpora = []typeormCorpus{
	{name: "seerr", dir: "seerr", files: 618, gates: map[string]int{"G10": 104, "G12": 18}, enums: 23},
	{name: "nest-admin", dir: "nest-admin", files: 237, gates: map[string]int{"G10": 58, "G12": 11}, enums: 12, forFeature: 12},
	{name: "maintainerr", dir: "maintainerr", prefix: "apps/server/", files: 499, gates: map[string]int{"G10": 135, "G12": 6}, enums: 7, forFeature: 10},
	{name: "maintainerr-contracts", dir: "maintainerr", prefix: "packages/contracts/", gates: map[string]int{"G12": 9}, enums: 13},
	{name: "aqualink-app", dir: "aqualink-app", prefix: "packages/api/", files: 394, gates: map[string]int{"G10": 233, "G12": 7}, enums: 7, forFeature: 19},
}

// typeormRow is the part of a T018g6 inventory row the census compares.
type typeormRow struct {
	Kind      string  `json:"kind"`
	Corpus    string  `json:"corpus"`
	Row       string  `json:"row"`
	File      string  `json:"file"`
	Line      int     `json:"line"`
	Class     string  `json:"class"`
	Member    *string `json:"member"`
	Decorator string  `json:"decorator"`
	Form      string  `json:"form"`
	DeclType  *string `json:"declType"`
	Status    string  `json:"status"`
}

// typeormFile is one parsed file of a corpus: its payload (nil outside
// the gate), its gate row, the ungated class-decorator cache and the
// source, for line numbers of RouteExpr offsets.
type typeormFile struct {
	gate      string
	payload   *ModelSyntax
	templates TemplateSyntax
	source    []byte
	nestOnly  bool // imports @nestjs/typeorm but not typeorm
}

// TestTypeORMCensus checks that the version 5 payload captures exactly the
// syntax T018g6's inventory (docs/contract/claims/data/typeorm-models-inventory.jsonl) found in
// seerr, nest-admin, Maintainerr and aqualink-app: the gate counts (G10,
// G11, G12 and the enums of the G12 files), every decorator site at its
// line on its class or member with its argument shapes, option keys and
// the member's declared type, no decorator in an inventoried file that the
// inventory lacks, and the payload side of the discovery rows (seerr's
// option objects, entities array and options selector; nest-admin's
// forRootAsync factory and registerAs namespace; Maintainerr's and
// aqualink's data sources; the forFeature calls, counted in the ungated
// class-decorator cache and, since G10 reaches `@nestjs/typeorm`
// imports, also found in the payload). It
// only parses; set GRAPHVEIN_TYPEORM_CENSUS_CORPUS to a directory holding
// the four checkouts (scripts/cloud-corpus.sh seerr nest-admin maintainerr
// aqualink-app) to run it.
func TestTypeORMCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_TYPEORM_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_TYPEORM_CENSUS_CORPUS is not set")
	}
	rows := typeormInventory(t)
	parsed := map[string]map[string]*typeormFile{}
	for _, corpus := range typeormCorpora {
		t.Run(corpus.name, func(t *testing.T) {
			root := filepath.Join(dir, corpus.dir)
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%v (run scripts/cloud-corpus.sh %s)", err, corpus.dir)
			}
			files := parsed[corpus.dir]
			if files == nil {
				var prefixes []string
				for _, other := range typeormCorpora {
					if other.dir == corpus.dir {
						prefixes = append(prefixes, other.prefix)
					}
				}
				files = typeormCapture(t, root, prefixes)
				parsed[corpus.dir] = files
			}
			c := &typeormCensus{t: t, corpus: corpus, files: files}
			c.gates()
			sites := 0
			for _, r := range rows[corpus.dir] {
				if r.Kind == "site" && strings.HasPrefix(r.File, corpus.prefix) {
					c.site(r)
					sites++
				}
			}
			c.nothingExtra(rows[corpus.dir])
			for _, r := range rows[corpus.dir] {
				if r.Kind == "discovery" && strings.HasPrefix(r.File, corpus.prefix) {
					c.discovery(r)
				}
			}
			c.forFeature()
			c.specific()
			size, gated, nestOnly := 0, 0, 0
			for path, f := range files {
				if !strings.HasPrefix(path, corpus.prefix) || !typeormSource(path) {
					continue
				}
				if f.payload != nil {
					gated++
					data, _ := json.Marshal(f.payload)
					size += len(data)
				} else if f.nestOnly {
					nestOnly++
				}
			}
			t.Logf("%s: %d gated files, %d payload bytes, %d decorator sites checked; %d files import @nestjs/typeorm without typeorm (G10 since TOB2)", corpus.name, gated, size, sites, nestOnly)
		})
	}
}

func typeormInventory(t *testing.T) map[string][]typeormRow {
	t.Helper()
	f, err := os.Open("../../../docs/contract/claims/data/typeorm-models-inventory.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]typeormRow{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var r typeormRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out[r.Corpus] = append(out[r.Corpus], r)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// typeormSource reports the TypeScript sources the inventory counted:
// .ts, .tsx, .mts and .cts files, not declaration files.
func typeormSource(path string) bool {
	if strings.HasSuffix(path, ".d.ts") || strings.HasSuffix(path, ".d.mts") || strings.HasSuffix(path, ".d.cts") {
		return false
	}
	switch filepath.Ext(path) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	}
	return false
}

// typeormCapture parses every TypeScript and JavaScript file of a checkout
// under one of the package prefixes and keeps its gate, payload and
// class-decorator cache. (Maintainerr's vendored Yarn release, outside its
// packages, is too large to parse within the parser's time limit.)
func typeormCapture(t *testing.T, root string, prefixes []string) map[string]*typeormFile {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var files []scan.File
	var jobs []parse.Job
	for _, f := range scanned.Files {
		inside := false
		for _, p := range prefixes {
			inside = inside || strings.HasPrefix(f.Record.Path, p)
		}
		switch f.Record.Language {
		case "typescript", "tsx", "javascript":
			if inside {
				files = append(files, f)
				jobs = append(jobs, parse.Job{Language: f.Record.Language, Source: f.Source})
			}
		}
	}
	out := map[string]*typeormFile{}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err = parse.Consume(context.Background(), jobs, options, func(i int, result parse.Result) {
		path := files[i].Record.Path
		if result.Err != nil {
			t.Errorf("%s: %v", path, result.Err)
			return
		}
		f := Extract(path, files[i].Source, result.Root)
		entry := &typeormFile{gate: ModelGate(path, result.Root, f), payload: CaptureModels(path, result.Root, f), templates: CaptureTemplates(result.Root, f.Nodes), source: files[i].Source}
		nest, orm := false, false
		for _, imp := range f.Imports {
			nest = nest || imp.Module == "@nestjs/typeorm"
			orm = orm || typeormModule(imp.Module)
		}
		entry.nestOnly = nest && !orm
		if entry.payload != nil && !entry.payload.Valid() {
			t.Errorf("%s: invalid payload", path)
		}
		out[path] = entry
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type typeormCensus struct {
	t      *testing.T
	corpus typeormCorpus
	files  map[string]*typeormFile
}

// gates checks the gate counts of the corpus's package.
func (c *typeormCensus) gates() {
	total, enums := 0, 0
	got := map[string]int{}
	for path, f := range c.files {
		if !strings.HasPrefix(path, c.corpus.prefix) || !typeormSource(path) {
			continue
		}
		total++
		if f.gate == "G10" || f.gate == "G11" || f.gate == "G12" {
			got[f.gate]++
		} else if f.gate != "" {
			c.t.Errorf("%s: gate %s, a Drizzle or Sequelize row", path, f.gate)
		}
		if f.gate == "G12" {
			for _, st := range f.payload.Statements {
				if st.Kind != StatementEnum {
					c.t.Errorf("%s: G12 payload holds a %s statement", path, st.Kind)
				}
				enums++
			}
			if len(f.payload.Declarations) != 0 || f.payload.Default != nil {
				c.t.Errorf("%s: G12 payload holds declarations or a default", path)
			}
		}
	}
	if c.corpus.files != 0 && total != c.corpus.files {
		c.t.Errorf("TypeScript files = %d, want %d", total, c.corpus.files)
	}
	for _, gate := range []string{"G10", "G11", "G12"} {
		if got[gate] != c.corpus.gates[gate] {
			c.t.Errorf("%s files = %d, want %d", gate, got[gate], c.corpus.gates[gate])
		}
	}
	if enums != c.corpus.enums {
		c.t.Errorf("enums in G12 files = %d, want %d", enums, c.corpus.enums)
	}
}

// decoratorName is the identifier a decorator expression names: `@X`,
// `@X(...)`, `@a.X(...)`.
func decoratorName(e ModelExpr) string {
	if e.Kind == ModelCall {
		e = *e.Callee
	}
	switch e.Kind {
	case ModelIdentifier:
		return e.Name
	case ModelMember:
		return e.Name
	}
	return ""
}

// moduleClasses lists a payload's module-level classes by name.
func moduleClasses(s *ModelSyntax) map[string]*ModelStatement {
	out := map[string]*ModelStatement{}
	for i := range s.Statements {
		if s.Statements[i].Kind == StatementClass {
			out[s.Statements[i].Name] = &s.Statements[i]
		}
	}
	return out
}

var (
	formShapes = regexp.MustCompile(`(?:^|\s)(\w+)\(((?:arrow|object|string|array|,)*)\)`)
	formKeys   = regexp.MustCompile(`keys ([A-Za-z_,]+)`)
	formType   = regexp.MustCompile(`explicit '([^']+)' \(positional\)`)
)

// site checks one decorator site row: the decorator at its line on its
// class or member, its argument shapes, its option keys in order, an
// explicit positional type, and the member's declared type.
func (c *typeormCensus) site(r typeormRow) {
	t := c.t
	f := c.files[r.File]
	if f == nil || f.payload == nil {
		t.Errorf("%s:%d: no payload (gate %q)", r.File, r.Line, gateOf(f))
		return
	}
	class := moduleClasses(f.payload)[r.Class]
	if class == nil {
		t.Errorf("%s:%d: no module-level class %s", r.File, r.Line, r.Class)
		return
	}
	var list []ModelExpr
	var member *ModelClassMember
	if r.Member == nil {
		list = class.DecoratorList
	} else {
		for i := range class.Members {
			if class.Members[i].Name == *r.Member {
				for _, d := range class.Members[i].Decorators {
					if d.Span.StartLine == r.Line {
						member = &class.Members[i]
					}
				}
			}
		}
		if member == nil {
			t.Errorf("%s:%d: no member %s.%s with a decorator at this line", r.File, r.Line, r.Class, *r.Member)
			return
		}
		list = member.Decorators
	}
	var d *ModelExpr
	for i := range list {
		if list[i].Span.StartLine == r.Line && decoratorName(list[i]) == r.Decorator {
			d = &list[i]
		}
	}
	if d == nil {
		t.Errorf("%s:%d: no @%s decorator on %s", r.File, r.Line, r.Decorator, r.Class)
		return
	}
	var args []ModelExpr
	if d.Kind == ModelCall {
		args = d.Args
	}
	if m := formShapes.FindStringSubmatch(r.Form); m != nil && m[1] == r.Decorator {
		var want []string
		if m[2] != "" {
			want = strings.Split(m[2], ",")
		}
		var got []string
		for _, a := range args {
			got = append(got, map[string]string{ModelArrow: "arrow", ModelObject: "object", ModelString: "string", ModelArray: "array"}[a.Kind])
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s:%d: @%s argument shapes %v, want %v", r.File, r.Line, r.Decorator, got, want)
		}
	}
	if m := formKeys.FindStringSubmatch(r.Form); m != nil {
		var got []string
		for _, a := range args {
			if a.Kind == ModelObject {
				for _, e := range a.Entries {
					got = append(got, e.Key)
				}
			}
		}
		if strings.Join(got, ",") != m[1] {
			t.Errorf("%s:%d: @%s option keys %v, want %s", r.File, r.Line, r.Decorator, got, m[1])
		}
	}
	if m := formType.FindStringSubmatch(r.Form); m != nil {
		if len(args) == 0 || args[0].Kind != ModelString || args[0].Value != m[1] {
			t.Errorf("%s:%d: @%s positional type, want %q", r.File, r.Line, r.Decorator, m[1])
		}
	}
	if r.DeclType != nil && member != nil {
		if got := shapeType(member.Type); !sameDeclType(got, *r.DeclType) {
			t.Errorf("%s:%d: %s.%s declared type %q, want %q", r.File, r.Line, r.Class, *r.Member, got, *r.DeclType)
		}
	}
}

// sameDeclType compares a rendered payload type with the inventory's
// source text. A reference whose arguments the payload does not keep
// renders its argument count (`Relation<1>`), so only the name is compared
// there; string literals use the source's quotes.
func sameDeclType(got, want string) bool {
	if got == strings.ReplaceAll(want, `"`, "'") {
		return true
	}
	name, rest, ok := strings.Cut(want, "<")
	return ok && strings.HasSuffix(rest, ">") && strings.HasPrefix(got, name+"<") && !strings.ContainsAny(name, " |&")
}

func gateOf(f *typeormFile) string {
	if f == nil {
		return "missing file"
	}
	return f.gate
}

// nothingExtra checks that every class and member decorator the payload
// captures in an inventoried file is an inventory site.
func (c *typeormCensus) nothingExtra(rows []typeormRow) {
	want := map[string]int{}
	for _, r := range rows {
		if r.Kind == "site" && strings.HasPrefix(r.File, c.corpus.prefix) {
			want[r.File]++
		}
	}
	for file, n := range want {
		f := c.files[file]
		if f == nil || f.payload == nil {
			continue
		}
		got := 0
		for _, st := range f.payload.Statements {
			if st.Kind != StatementClass {
				continue
			}
			got += len(st.DecoratorList)
			for _, m := range st.Members {
				got += len(m.Decorators)
			}
		}
		if got != n {
			c.t.Errorf("%s: %d decorators captured, the inventory has %d sites", file, got, n)
		}
	}
}

// discovery checks that a discovery row's TypeScript site is in the
// payload: some captured declaration, statement, expression, member or
// decorator starts at the row's line. forFeature rows are checked by
// forFeature; a refused row (a project helper) needs no payload.
func (c *typeormCensus) discovery(r typeormRow) {
	if !typeormSource(r.File) || r.Line == 0 || r.Status == "refused" || strings.Contains(r.Form, "forFeature") {
		return
	}
	f := c.files[r.File]
	if f == nil {
		c.t.Errorf("%s:%d: file not found", r.File, r.Line)
		return
	}
	if f.payload == nil {
		// G10 reaches @nestjs/typeorm imports since step TOB2
		// (decision OP1): every file a discovery row reads has a payload.
		c.t.Errorf("%s:%d: %s outside the gate (%s): no payload", r.File, r.Line, r.Row, r.Form)
		return
	}
	if !payloadStartsAt(f.payload, r.Line) {
		c.t.Errorf("%s:%d: %s: nothing captured at this line (%s)", r.File, r.Line, r.Row, r.Form)
	}
}

// payloadStartsAt reports whether a captured node starts at line.
func payloadStartsAt(s *ModelSyntax, line int) bool {
	found := false
	var expr func(e *ModelExpr)
	var stmts func(list []ModelStatement)
	expr = func(e *ModelExpr) {
		if e == nil || found {
			return
		}
		if e.Span.StartLine == line {
			found = true
			return
		}
		for _, x := range []*ModelExpr{e.Callee, e.Object, e.Tag, e.Body, e.Left, e.Right, e.Index, e.Test, e.Then, e.Else} {
			expr(x)
		}
		for _, list := range [][]ModelExpr{e.Args, e.Substitutions, e.Elements} {
			for i := range list {
				expr(&list[i])
			}
		}
		for i := range e.Entries {
			expr(&e.Entries[i].Value)
		}
		stmts(e.Statements)
	}
	stmts = func(list []ModelStatement) {
		for i := range list {
			st := &list[i]
			if st.Span.StartLine == line {
				found = true
				return
			}
			expr(st.Value)
			expr(st.Extends)
			for j := range st.DecoratorList {
				expr(&st.DecoratorList[j])
			}
			for _, m := range st.Members {
				if m.Span.StartLine == line {
					found = true
				}
				for j := range m.Decorators {
					expr(&m.Decorators[j])
				}
			}
			stmts(st.Body)
			stmts(st.Else)
			stmts(st.Statics)
		}
	}
	for i := range s.Declarations {
		if s.Declarations[i].Span.StartLine == line {
			return true
		}
		expr(&s.Declarations[i].Value)
	}
	if s.Default != nil {
		if s.Default.Span.StartLine == line {
			return true
		}
		expr(&s.Default.Value)
	}
	stmts(s.Statements)
	return found
}

// lineOf converts a byte offset of a RouteExpr to a 1-based line.
func lineOf(source []byte, offset int) int {
	if offset > len(source) {
		offset = len(source)
	}
	return strings.Count(string(source[:offset]), "\n") + 1
}

// templateCalls lists the calls `<object>.<method>(...)` in a file's
// class-decorator cache with their lines.
func templateCalls(f *typeormFile, method string) []int {
	var lines []int
	var walk func(e *facts.RouteExpr)
	walk = func(e *facts.RouteExpr) {
		if e == nil {
			return
		}
		if e.Kind == "call_expression" && e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Name == method {
			lines = append(lines, lineOf(f.source, e.Offset))
		}
		walk(e.Object)
		for i := range e.Args {
			walk(&e.Args[i])
		}
		for i := range e.Parts {
			walk(&e.Parts[i])
		}
		for k := range e.Properties {
			v := e.Properties[k]
			walk(&v)
		}
	}
	for _, d := range f.templates.Decorators {
		for i := range d.Entries {
			walk(&d.Entries[i].Value)
		}
	}
	return lines
}

// decoratorCallAt reports a call in the class-decorator cache at line
// (any method when method is "").
func decoratorCallAt(f *typeormFile, line int, method string) bool {
	for _, m := range []string{"forRoot", "forRootAsync", "forFeature"} {
		if method != "" && m != method {
			continue
		}
		for _, l := range templateCalls(f, m) {
			if l == line {
				return true
			}
		}
	}
	return false
}

// typeormTestFile is the TO-K4 test-file rule.
func typeormTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, ".spec.") || strings.Contains(base, ".test.") || strings.Contains(base, ".e2e-spec.") ||
		strings.Contains("/"+path, "/test/") || strings.Contains("/"+path, "/__tests__/")
}

// forFeature checks the TypeOrmModule.forFeature calls of the package's
// non-test files: their number, and that every inventoried call is at its
// line with an array of identifiers as its first argument.
func (c *typeormCensus) forFeature() {
	if c.corpus.forFeature == 0 {
		return
	}
	total := 0
	var files []string
	for path, f := range c.files {
		if strings.HasPrefix(path, c.corpus.prefix) && typeormSource(path) && !typeormTestFile(path) {
			lines := templateCalls(f, "forFeature")
			if n := len(lines); n > 0 {
				total += n
				files = append(files, path)
			}
			for _, l := range lines {
				// Since step TOB2 the module files are in G10, so the
				// payload holds the call too.
				if f.payload == nil || !payloadStartsAt(f.payload, l) {
					c.t.Errorf("%s:%d: forFeature call not in the payload", path, l)
				}
			}
		}
	}
	sort.Strings(files)
	if total != c.corpus.forFeature {
		c.t.Errorf("forFeature calls = %d, want %d (%v)", total, c.corpus.forFeature, files)
	}
}

// specific checks the discovery rows whose payload shape the linker reads
// in detail.
func (c *typeormCensus) specific() {
	t := c.t
	decl := func(file, name string) *ModelDeclaration {
		f := c.files[c.corpus.prefix+file]
		if f == nil || f.payload == nil {
			t.Errorf("%s: no payload", file)
			return nil
		}
		for i := range f.payload.Declarations {
			if f.payload.Declarations[i].Name == name {
				return &f.payload.Declarations[i]
			}
		}
		t.Errorf("%s: no declaration %s", file, name)
		return nil
	}
	switch c.corpus.name {
	case "seerr":
		for name, dialect := range map[string]string{"testConfig": "sqlite", "devConfig": "sqlite", "prodConfig": "sqlite", "postgresDevConfig": "postgres", "postgresProdConfig": "postgres"} {
			d := decl("server/datasource.ts", name)
			if d == nil {
				continue
			}
			keys := map[string]ModelEntry{}
			for _, e := range d.Value.Entries {
				keys[e.Key] = e
			}
			if d.Value.Kind != ModelObject || keys["type"].Value.Value != dialect || keys["entities"].Kind != ModelShorthand {
				t.Errorf("%s: options %s", name, shape(d.Value))
			}
			for _, scope := range []string{"entityPrefix", "namingStrategy", "schema"} {
				if _, ok := keys[scope]; ok {
					t.Errorf("%s: scope option %s", name, scope)
				}
			}
		}
		if d := decl("server/datasource.ts", "entities"); d != nil && (d.Value.Kind != ModelArray || len(d.Value.Elements) != 14) {
			t.Errorf("entities %s", shape(d.Value))
		}
		var selector string
		for _, st := range c.files["server/datasource.ts"].payload.Statements {
			if st.Kind == StatementFunction && st.Name == "getDataSource" {
				selector = shapeStatements(st.Value.Statements)
			}
		}
		if !regexp.MustCompile(`^if \(.*\) \{return testConfig\} else \{if \(.*\) \{return \(isPgsql \? postgresProdConfig : prodConfig\)\} else \{return \(isPgsql \? postgresDevConfig : devConfig\)\}\}$`).MatchString(selector) {
			t.Errorf("getDataSource %s", selector)
		}
		if d := decl("server/datasource.ts", "dataSource"); d != nil && shape(d.Value) != "new DataSource(getDataSource())" {
			t.Errorf("data source %s", shape(d.Value))
		}
	case "nest-admin":
		if d := decl("src/config/database.config.ts", "dbRegToken"); d != nil && shape(d.Value) != `"database"` {
			t.Errorf("token %s", shape(d.Value))
		}
		if d := decl("src/config/database.config.ts", "DatabaseConfig"); d != nil && shape(d.Value) != "registerAs(dbRegToken, () => dataSourceOptions)" {
			t.Errorf("registerAs %s", shape(d.Value))
		}
		if d := decl("src/config/database.config.ts", "dataSourceOptions"); d != nil && !strings.Contains(shape(d.Value), `type: "mysql"`) {
			t.Errorf("options %s", shape(d.Value))
		}
		f := c.files["src/shared/database/database.module.ts"]
		module := moduleClasses(f.payload)["DatabaseModule"]
		if module == nil || len(module.DecoratorList) != 1 {
			t.Fatalf("database module %+v", module)
		}
		var factory *ModelExpr
		for _, e := range module.DecoratorList[0].Args[0].Entries {
			if e.Key == "imports" {
				root := e.Value.Elements[0]
				if shape(*root.Callee) != "TypeOrmModule.forRootAsync" {
					t.Errorf("forRootAsync %s", shape(root))
				}
				for _, o := range root.Args[0].Entries {
					if o.Key == "useFactory" {
						factory = &o.Value
					}
				}
			}
		}
		if factory == nil || factory.Kind != ModelArrow || len(factory.Statements) == 0 {
			t.Fatalf("useFactory %+v", factory)
		}
		last := factory.Statements[len(factory.Statements)-1]
		if got := shapeStatement(last); got != `return {...configService.get("database"), autoLoadEntities: true, logging: loggerOptions, logger: new TypeORMLogger(loggerOptions)}` {
			t.Errorf("factory return %s", got)
		}
	case "maintainerr":
		if d := decl("src/datasource-config.ts", "datasource"); d != nil && !strings.HasPrefix(shape(d.Value), `new DataSource({type: "better-sqlite3", database: "../../data/maintainerr.sqlite", entities: ["./src/**/*.entities.ts"]`) {
			t.Errorf("data source %s", shape(d.Value))
		}
	case "aqualink-app":
		if d := decl("ormconfig.ts", "dataSourceInfo"); d != nil && (d.Value.Kind != ModelCond || d.Value.Then.Kind != ModelObject || d.Value.Else.Kind != ModelObject) {
			t.Errorf("dataSourceInfo %s", shape(d.Value))
		}
		if d := decl("ormconfig.ts", "dataSourceOptions"); d != nil {
			got := shape(d.Value)
			for _, part := range []string{`type: "postgres", ...dataSourceInfo`, "namingStrategy: new SnakeNamingStrategy()", `entities: [join(__dirname, "src/**", "*.entity.ts"), join(__dirname, "src/**", "*.entity.js")]`} {
				if !strings.Contains(got, part) {
					t.Errorf("dataSourceOptions lacks %s: %s", part, got)
				}
			}
		}
		if f := c.files["packages/api/ormconfig.ts"]; f.payload.Default == nil || shape(f.payload.Default.Value) != "new DataSource(dataSourceOptions)" {
			t.Errorf("default %+v", f.payload.Default)
		}
	}
}
