package typescript

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

// parseModels parses one source and returns its facts and model payload.
func parseModels(t *testing.T, file, source string) (Facts, *ModelSyntax) {
	t.Helper()
	f, _, s := parseModelsRoot(t, file, source)
	return f, s
}

// parseModelsRoot also returns the parsed tree, which the gate reads.
func parseModelsRoot(t *testing.T, file, source string) (Facts, *parse.Node, *ModelSyntax) {
	t.Helper()
	language := "typescript"
	if strings.HasSuffix(file, ".js") {
		language = "javascript"
	}
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: language, Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(err, results[0].Err)
	}
	f := Extract(file, []byte(source), results[0].Root)
	return f, results[0].Root, CaptureModels(file, results[0].Root, f)
}

// shape renders a reduced expression compactly, so tests can state order.
func shape(e ModelExpr) string {
	list := func(items []ModelExpr) string {
		var parts []string
		for _, i := range items {
			parts = append(parts, shape(i))
		}
		return strings.Join(parts, ", ")
	}
	switch e.Kind {
	case ModelCall:
		return shape(*e.Callee) + "(" + list(e.Args) + ")"
	case ModelMember:
		return shape(*e.Object) + "." + e.Name
	case ModelIdentifier:
		return e.Name
	case ModelString:
		return `"` + e.Value + `"`
	case ModelNumber, ModelBoolean:
		return e.Value
	case ModelNull:
		return "null"
	case ModelTemplate, ModelTagged:
		var b strings.Builder
		if e.Kind == ModelTagged {
			b.WriteString(shape(*e.Tag))
		}
		b.WriteString("`")
		for i, s := range e.Strings {
			b.WriteString(s)
			if i < len(e.Substitutions) {
				b.WriteString("${" + shape(e.Substitutions[i]) + "}")
			}
		}
		return b.String() + "`"
	case ModelObject:
		var parts []string
		for _, entry := range e.Entries {
			switch entry.Kind {
			case ModelProperty:
				parts = append(parts, entry.Key+": "+shape(entry.Value))
			case ModelShorthand:
				parts = append(parts, entry.Key)
			case ModelSpreadIn:
				parts = append(parts, "..."+shape(entry.Value))
			case ModelMethod:
				parts = append(parts, entry.Key+"() "+shape(entry.Value))
			default:
				parts = append(parts, "?entry")
			}
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case ModelArray:
		return "[" + list(e.Elements) + "]"
	case ModelSpread:
		return "..." + shape(*e.Object)
	case ModelArrow:
		body := shape(*e.Body)
		if e.Block {
			body = "{return " + body + "}"
		}
		out := "(" + strings.Join(e.Params, ", ") + ") => " + body
		if e.Async {
			out = "async " + out
		}
		return out
	case ModelNew:
		return "new " + shape(*e.Callee) + "(" + list(e.Args) + ")"
	case ModelFunction:
		if e.Async {
			return "async function " + e.Name + "(" + strings.Join(e.Params, ", ") + ") {" + shapeStatements(e.Statements) + "}"
		}
		return "function " + e.Name + "(" + strings.Join(e.Params, ", ") + ") {" + shapeStatements(e.Statements) + "}"
	case ModelAssign:
		return shape(*e.Left) + " = " + shape(*e.Right)
	case ModelIndex:
		return shape(*e.Object) + "[" + shape(*e.Index) + "]"
	case ModelBinary:
		return "(" + shape(*e.Left) + " " + e.Value + " " + shape(*e.Right) + ")"
	case ModelUnary:
		return e.Value + " " + shape(*e.Object)
	case ModelCond:
		return "(" + shape(*e.Test) + " ? " + shape(*e.Then) + " : " + shape(*e.Else) + ")"
	}
	return "?"
}

// shapeStatements renders a statement list compactly, one entry per
// statement separated by "; ".
func shapeStatements(list []ModelStatement) string {
	var parts []string
	for _, st := range list {
		parts = append(parts, shapeStatement(st))
	}
	return strings.Join(parts, "; ")
}

func shapeStatement(st ModelStatement) string {
	value := func() string {
		if st.Value == nil {
			return ""
		}
		return shape(*st.Value)
	}
	switch st.Kind {
	case StatementExpression:
		return value()
	case StatementDeclaration:
		if st.Value == nil {
			return st.Keyword + " " + st.Name
		}
		return st.Keyword + " " + st.Name + " = " + value()
	case StatementDestructure:
		var b []string
		for _, x := range st.Bindings {
			b = append(b, x.Key+":"+x.Name)
		}
		return st.Keyword + " {" + strings.Join(b, ", ") + "} = " + value()
	case StatementReturn:
		return strings.TrimSpace("return " + value())
	case StatementClass:
		out := "class " + st.Name
		if st.Extends != nil {
			out += " extends " + shape(*st.Extends)
		}
		return out
	case StatementIf:
		out := "if " + value() + " {" + shapeStatements(st.Body) + "}"
		if st.Else != nil {
			out += " else {" + shapeStatements(st.Else) + "}"
		}
		return out
	case StatementTry:
		out := "try {" + shapeStatements(st.Body) + "}"
		if st.Catch != nil {
			out += " catch {" + shapeStatements(st.Catch) + "}"
		}
		if st.Finally != nil {
			out += " finally {" + shapeStatements(st.Finally) + "}"
		}
		return out
	case StatementUnknown:
		return "?"
	case StatementEnum:
		var members []string
		for _, m := range st.EnumMembers {
			if m.Value == nil {
				members = append(members, m.Name)
			} else {
				members = append(members, m.Name+" = "+shape(*m.Value))
			}
		}
		return "enum " + st.Name + " {" + strings.Join(members, ", ") + "}"
	}
	return "?stmt"
}

const drizzleImport = "import { pgTable, text, index } from \"drizzle-orm/pg-core\";\n"

func TestModelPayloadShapes(t *testing.T) {
	cases := map[string]string{
		// Order of columns and modifiers, arrays, arrows and tagged templates.
		"export const t = pgTable(\"t\", { b: text(\"b\").notNull(), a: text(\"a\") }, (x) => [index(\"i\").on(x.b).where(sql`${x.a} > ${1}`), ...more]);": `pgTable("t", {b: text("b").notNull(), a: text("a")}, (x) => [index("i").on(x.b).where(sql` + "`${x.a} > ${1}`" + `), ...more])`,
		// satisfies and as wrappers are transparent, type arguments dropped.
		"export const c = defineConfig<X>({ dialect: \"sqlite\" } satisfies Config) as Config;": `defineConfig({dialect: "sqlite"})`,
		// Shorthand, spread, method returning a literal, other entries unknown.
		"const o = { a, ...b, dataType() { return `bytea` }, toDriver(v) { return v }, [k]: 1, get g() { return 1 } };": "{a, ...b, dataType() `bytea`, toDriver() ?, ?entry, ?entry}",
		// Block arrow with one return; a block with more is unknown.
		"const f = call((t) => { return [t.a] }, () => { x(); return 1 }, async () => 1);": "call((t) => {return [t.a]}, () => {return ?}, ?)",
		// Literals: signed numbers, booleans, null, undefined, escapes.
		"const l = f(-1, +2, 0x1f, true, false, null, undefined, \"a\\tb\", 'c');": "f(-1, +2, 0x1f, true, false, null, undefined, \"a\tb\", \"c\")",
		// Comments are never captured, anywhere in the tree.
		"const k = f(/* a */ 1, // b\n [/* c */ 2], { /* d */ e: 3 });": "f(1, [2], {e: 3})",
		// Optional chains and other operators are unknown; new is kept (version 2);
		// conditionals are kept from version 5.
		"const u = f(a?.b, g?.(), a ? b : c, new D(), a + b, a?.[b], this, /re/g, typeof x === \"function\", !y, a || \"d\");": "f(?, ?, (a ? b : c), new D(), (a + b), ?, ?, ?, (typeof x === \"function\"), ! y, (a || \"d\"))",
		// Version 2 expressions: new without arguments, function expressions
		// (async and generator functions are unknown), assignments, index.
		"const v = f(new A.B<T>, function g(a) { let x; return a[x]; }, async function () {}, function* () {}, a = b, c += 1);": "f(new A.B(), function g(a) {let x; return a[x]}, ?, ?, a = b, ?)",
		// Block arrows keep the single return as Body and list every statement.
		"const w = f((m) => { m.x = 1; if (m) {} return m; });": "f((m) => {return ?})",
		// Parameters: destructured, defaulted and rest are "".
		"const p = f(({ a }, b = 1, ...c) => a, d => d);": "f((, , ) => a, (d) => d)",
	}
	for source, want := range cases {
		_, s := parseModels(t, "schema.ts", drizzleImport+source)
		if s == nil || len(s.Declarations) != 1 {
			t.Fatalf("%s: payload %+v", source, s)
		}
		if got := shape(s.Declarations[0].Value); got != want {
			t.Errorf("%s:\n got %s\nwant %s", source, got, want)
		}
	}
}

func TestModelPayloadDeclarations(t *testing.T) {
	source := drizzleImport + `const a = pgTable("a", {});
export const b = sql` + "`x`" + `, n = 5, s = "lit";
export let c = { x: 1 };
var d = (f());
const { e } = g();
declare const h: number;
function i() { const inner = pgTable("inner", {}); }
export { a };
export default { dialect: "postgresql" } as const;
`
	_, s := parseModels(t, "schema.ts", source)
	var got []string
	for _, d := range s.Declarations {
		got = append(got, d.Keyword+" "+d.Name+" "+map[bool]string{true: "exported", false: "local"}[d.Exported]+" "+d.Value.Kind)
	}
	// Version 5 keeps string initializers (s), never other literals (n).
	want := []string{"const a local call", "const b exported tagged", "const s exported string", "let c exported object", "var d local call"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declarations %v, want %v", got, want)
	}
	if s.Default == nil || shape(s.Default.Value) != `{dialect: "postgresql"}` || s.Default.Span.StartLine != 10 {
		t.Errorf("default %+v", s.Default)
	}
	if s.Declarations[0].Span.StartLine != 2 || s.Declarations[0].Value.Span.StartLine != 2 {
		t.Errorf("spans %+v", s.Declarations[0])
	}
}

func TestModelGate(t *testing.T) {
	cases := []struct {
		file, source, gate string
	}{
		{"a.ts", "import { Model } from \"sequelize\";\n", "G4"},
		{"a.ts", "import type { ThroughOptions } from \"sequelize\";\n", "G4"},
		{"a.ts", "import x from \"sequelize/lib/utils\";\n", "G4"},
		{"a.ts", "import { Table } from \"sequelize-typescript\";\n", ""},
		{"a.js", "const Sequelize = require('sequelize');\n", "G5"},
		{"a.js", "function f() { const { DataTypes } = require(\"sequelize\"); }\n", "G5"},
		{"a.js", "const s = require('sequelize-typescript');\n", ""},
		{"a.js", "// require('sequelize')\n", ""},
		{"a.js", "module.exports = (sequelize, DataTypes) => { const a = sequelize.define('t_a', {}, {}); return a; };\n", "G6"},
		{"a.js", "X.init({ a: {} }, { sequelize });\n", "G6"},
		{"a.js", "X.init({ a: {} });\nY.init(attrs, { sequelize });\nif (x) { Z.init({}, {}); }\n", ""},
		{"a.js", "customElements.define('x-el', El);\nwindow.define('x', [1]);\nobj?.define('y', {});\n", ""},
		{"a.js", "module.exports = { production: { dialect: 'sqlite', define: {} } };\n", "G7"},
		{"a.js", "module.exports = { dialect: 'postgresql' };\n", ""},
		{"a.js", "const config = require('../config/config');\nmodule.exports = () => { const env = 'x'; return config[env]; };\n", "G8"},
		{"a.ts", "import config from './config';\nexport default function () { return config[process.env.X]; }\n", "G8"},
		{"a.js", "const config = require('config');\nmodule.exports = () => config[env];\n", ""},
		{"a.js", "const config = require('./config');\nmodule.exports = async () => config[env];\n", ""},
		{"a.js", "const config = require('./config');\nmodule.exports = () => other[env];\n", ""},
		{"a.ts", "export default function (T) { return { a: { type: T.STRING(3), allowNull: false } }; }\n", "G9"},
		{"a.ts", "export function money(t) { return { type: t.INTEGER }; }\n", "G9"},
		{"a.js", "module.exports = (T) => ({ a: { type: T.INTEGER } });\n", "G9"},
		{"a.js", "exports.h = function (T) { return { a: { type: T.DATE() } }; };\n", "G9"},
		{"a.ts", "export default function (T) { return { a: { type: Other.STRING } }; }\n", ""},
		{"a.ts", "export default async function (T) { return { a: { type: T.STRING } }; }\n", ""},
		{"a.ts", "export default function (T) { const x = 1; return { a: { type: T.STRING } }; }\n", ""},
		{"a.ts", "function f(T) { if (x) { return { a: { type: T.STRING } }; } }\n", ""},
		{"a.ts", "const h = (T) => ({ a: { type: T.STRING } });\n", ""},
		{"a.ts", "export default function (T) { return { a: { kind: T.STRING } }; }\n", ""},
		{"a.ts", "import { pgTable } from \"drizzle-orm/pg-core\";\n", "G1"},
		{"a.ts", "import type { AnySQLiteColumn } from \"drizzle-orm/sqlite-core\";\n", "G1"},
		{"a.ts", "import { x } from \"drizzle-orm/mysql-core/columns\";\n", "G1"},
		{"a.ts", "import { defineConfig } from \"drizzle-kit\";\n", "G2"},
		{"a.ts", "import type { Config } from \"drizzle-kit\";\n", "G2"},
		{"drizzle.config.ts", "export default {};\n", "G3"},
		{"sub/drizzle.config.js", "export default {};\n", "G3"},
		{"drizzle.config.json.ts", "export default {};\n", ""},
		{"a.ts", "import { eq, sql } from \"drizzle-orm\";\n", ""},
		{"a.ts", "import { x } from \"drizzle-orm/-core\";\n", ""},
		{"a.ts", "import { x } from \"drizzle-orm/node-postgres\";\n", ""},
		{"a.ts", "import { x } from \"drizzle-orm/PG-core\";\n", ""},
		{"a.ts", "// import { pgTable } from \"drizzle-orm/pg-core\";\nconst t = pgTable(\"t\", {});\n", ""},
	}
	for _, c := range cases {
		f, root, s := parseModelsRoot(t, c.file, c.source)
		if got := ModelGate(c.file, root, f); got != c.gate {
			t.Errorf("%s %q: gate %q, want %q", c.file, c.source, got, c.gate)
		}
		if (s != nil) != (c.gate != "") {
			t.Errorf("%s %q: payload %v", c.file, c.source, s)
		}
	}
}

func TestModelPayloadTemplates(t *testing.T) {
	_, s := parseModels(t, "schema.ts", drizzleImport+"const q = sql`a\\n${b}\r\nc${d.e}`;\nconst r = f(`x\\u{zz}`, sql`\\u{zz}`);\n")
	q := s.Declarations[0].Value
	if !reflect.DeepEqual(q.Strings, []string{"a\n", "\nc", ""}) || len(q.Substitutions) != 2 || shape(q.Substitutions[1]) != "d.e" {
		t.Errorf("tagged %+v", q)
	}
	if got := shape(s.Declarations[1].Value); got != "f(?, ?)" {
		t.Errorf("undecodable templates %s", got)
	}
}

func TestModelPayloadValid(t *testing.T) {
	_, s := parseModels(t, "schema.ts", drizzleImport+"export const t = pgTable(\"t\", { a: text(\"a\") }, (x) => [sql`${x.a}`]);\nexport default {};\n"+
		"module.exports = function (s) { const a = 1; a.b = 2; return a; };\nclass C extends Model {}\nconst { d } = require(\"x\");\nconst n = new N();\nconst i = f(g(!a, b || c)[k]);\n")
	if !s.Valid() {
		t.Fatal("fresh payload invalid")
	}
	// A JSON round trip (the cache) stays valid and unchanged.
	data, err := json.Marshal(CachedFacts{Models: s})
	if err != nil {
		t.Fatal(err)
	}
	var back CachedFacts
	if err := json.Unmarshal(data, &back); err != nil || !back.Models.Valid() {
		t.Fatal("round trip invalid", err)
	}
	again, _ := json.Marshal(back)
	if string(again) != string(data) {
		t.Fatal("round trip changed the payload")
	}
	// A file outside the gate omits the field entirely.
	if data, _ := json.Marshal(CachedFacts{}); strings.Contains(string(data), "models") {
		t.Fatal("models field not omitted", string(data))
	}
	broken := []func(*ModelSyntax){
		func(s *ModelSyntax) { s.Version = 0 },
		func(s *ModelSyntax) { s.Version = ModelSyntaxVersion + 1 },
		func(s *ModelSyntax) { s.Declarations = nil },
		func(s *ModelSyntax) { s.Declarations[0].Name = "" },
		func(s *ModelSyntax) { s.Declarations[0].Keyword = "using" },
		func(s *ModelSyntax) { s.Declarations[0].Value.Kind = "lambda" },
		func(s *ModelSyntax) { s.Declarations[0].Value.Callee = nil },
		func(s *ModelSyntax) { s.Declarations[0].Value.Args[1].Entries[0].Kind = "" },
		func(s *ModelSyntax) { s.Declarations[0].Value.Args[2].Body = nil },
		func(s *ModelSyntax) { s.Declarations[0].Value.Args[2].Body.Elements[0].Strings = nil },
		func(s *ModelSyntax) { s.Default.Value.Kind = "" },
		func(s *ModelSyntax) { s.Statements[0].Kind = StatementReturn },
		func(s *ModelSyntax) { s.Statements[0].Value = nil },
		func(s *ModelSyntax) { s.Statements[0].Value.Right.Statements[0].Kind = "loop" },
		func(s *ModelSyntax) { s.Statements[0].Value.Right.Statements[0].Name = "" },
		func(s *ModelSyntax) { s.Statements[0].Value.Right.Statements[1].Value.Left = nil },
		func(s *ModelSyntax) { s.Statements[1].Name = "" },
		func(s *ModelSyntax) { s.Statements[1].Extends.Kind = "" },
		func(s *ModelSyntax) { s.Statements[2].Keyword = "" },
		func(s *ModelSyntax) { s.Statements[2].Bindings = nil },
		func(s *ModelSyntax) { s.Declarations[1].Value.Callee = nil },
		func(s *ModelSyntax) { s.Declarations[2].Value.Args[0].Index = nil },
		func(s *ModelSyntax) { s.Declarations[2].Value.Args[0].Object.Args[0].Value = "" },
		func(s *ModelSyntax) { s.Declarations[2].Value.Args[0].Object.Args[1].Right = nil },
	}
	plain, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for i, breakIt := range broken {
		var copy ModelSyntax
		if err := json.Unmarshal(plain, &copy); err != nil {
			t.Fatal(err)
		}
		breakIt(&copy)
		if copy.Valid() {
			t.Errorf("broken payload %d is valid", i)
		}
	}
	var nilSyntax *ModelSyntax
	if nilSyntax.Valid() {
		t.Error("nil payload is valid")
	}
}

// modelPayloadRows maps every payload-table row id (docs/lang/typescript-models.md,
// "Payload table") to the fields its capture branch fills. Rows with no
// fields have no capture side.
var modelPayloadRows = map[string][]string{
	"G1":              {"Version", "Declarations"},
	"G2":              {"Version", "Declarations"},
	"G3":              {"Version", "Declarations"},
	"G4":              {"Version", "Declarations"},
	"G5":              {"Version", "Declarations"},
	"G6":              {"Version", "Declarations"},
	"G7":              {"Version", "Declarations"},
	"G8":              {"Version", "Declarations"},
	"G9":              {"Version", "Declarations"},
	"G10":             {"Version", "Declarations"},
	"G11":             {"Version", "Declarations"},
	"G12":             {"Version", "Declarations"},
	"GD":              nil,
	"S1":              {"Name", "Keyword", "Exported", "Span", "Value"},
	"S2":              {"Span", "Value"},
	"S3":              {"Kind", "Span", "Value"},
	"S4":              {"Kind", "Span", "Name", "Exported", "Default", "Abstract", "Extends", "Decorators", "DecoratorList", "Members", "Statics"},
	"S5":              {"Kind", "Span", "Keyword", "Exported", "Bindings", "Value"},
	"S6":              {"Kind", "Span", "Name", "Exported", "Default", "Value"},
	"S7":              {"Kind", "Span", "Keyword", "Name", "Value", "Body"},
	"S8":              {"Kind", "Span", "Name", "Exported", "Const", "EnumMembers"},
	"SD":              nil,
	"B-declaration":   {"Kind", "Span", "Keyword", "Name", "Value"},
	"B-destructure":   {"Kind", "Span", "Keyword", "Bindings", "Value"},
	"B-expression":    {"Kind", "Span", "Value"},
	"B-return":        {"Kind", "Span", "Value"},
	"B-class":         {"Kind", "Span", "Name", "Abstract", "Extends", "Decorators", "DecoratorList", "Members", "Statics"},
	"B-if":            {"Kind", "Span", "Value", "Body", "Else"},
	"B-try":           {"Kind", "Span", "Body", "Catch", "Finally"},
	"B-unknown":       {"Kind", "Span"},
	"E-call":          {"Kind", "Span", "Callee", "Args"},
	"E-member":        {"Kind", "Span", "Object", "Name"},
	"E-identifier":    {"Kind", "Span", "Name"},
	"E-string":        {"Kind", "Span", "Value"},
	"E-number":        {"Kind", "Span", "Value"},
	"E-boolean":       {"Kind", "Span", "Value"},
	"E-null":          {"Kind", "Span"},
	"E-template":      {"Kind", "Span", "Strings", "Substitutions"},
	"E-tagged":        {"Kind", "Span", "Tag", "Strings", "Substitutions"},
	"E-object":        {"Kind", "Span", "Entries"},
	"E-property":      {"Kind", "Key", "Span", "Value"},
	"E-shorthand":     {"Kind", "Key", "Span", "Value"},
	"E-spread-entry":  {"Kind", "Span", "Value"},
	"E-method":        {"Kind", "Key", "Span", "Value"},
	"E-entry-unknown": {"Kind", "Span", "Value"},
	"E-array":         {"Kind", "Span", "Elements"},
	"E-spread":        {"Kind", "Span", "Object"},
	"E-arrow":         {"Kind", "Span", "Params", "Body"},
	"E-wrap":          {"Kind", "Span", "Callee", "Args"},
	"E-new":           {"Kind", "Span", "Callee", "Args"},
	"E-function":      {"Kind", "Span", "Name", "Params", "Statements"},
	"E-assign":        {"Kind", "Span", "Left", "Right"},
	"E-index":         {"Kind", "Span", "Object", "Index"},
	"E-binary":        {"Kind", "Span", "Value", "Left", "Right"},
	"E-unary":         {"Kind", "Span", "Value", "Object"},
	"E-await":         {"Kind", "Span", "Value", "Object"},
	"E-async":         {"Kind", "Span", "Params", "Body", "Async"},
	"E-conditional":   {"Kind", "Span", "Test", "Then", "Else"},
	"E-unknown":       {"Kind", "Span"},
	"M-property":      {"Kind", "Span", "Name", "Optional", "Readonly", "HasInitializer", "Type", "Decorators"},
	"M-method":        {"Kind", "Span", "Name", "Decorators"},
	"M-unknown":       {"Kind", "Span", "Decorators"},
	"T-keyword":       {"Kind", "Name"},
	"T-reference":     {"Kind", "Name", "Arguments", "Types"},
	"T-union":         {"Kind", "Types"},
	"T-intersection":  {"Kind", "Types"},
	"T-array":         {"Kind", "Types"},
	"T-literal":       {"Kind", "Literal", "Value"},
	"T-tuple":         {"Kind", "Types"},
	"T-function":      {"Kind"},
	"T-wrap":          {"Kind", "Name"},
	"T-unknown":       {"Kind"},
}

const sequelizeImport = "import { Model } from \"sequelize\";\n"

const typeormImport = "import { Column, Entity } from \"typeorm\";\n"

// modelSnippets holds one file per row with a capture side; the first record
// the capture tags with the row must fill exactly the row's fields.
var modelSnippets = map[string][2]string{
	"G1":              {"a.ts", drizzleImport},
	"G2":              {"a.ts", "import { defineConfig } from \"drizzle-kit\";\n"},
	"G3":              {"drizzle.config.ts", "export default {};\n"},
	"G4":              {"a.ts", sequelizeImport},
	"G5":              {"a.js", "const S = require(\"sequelize\");\n"},
	"G6":              {"a.js", "X.init({}, {});\n"},
	"G7":              {"a.js", "module.exports = { dialect: \"sqlite\" };\n"},
	"G8":              {"a.js", "const c = require(\"./c\");\nmodule.exports = () => c[e];\n"},
	"G9":              {"a.ts", "export default function (T) {\n  return { a: { type: T.STRING(3) } };\n}\n"},
	"S1":              {"a.ts", drizzleImport + "export const t = pgTable(\"t\", {});\n"},
	"S2":              {"a.ts", drizzleImport + "export default defineConfig({});\n"},
	"S3":              {"a.ts", sequelizeImport + "X.init({}, {});\n"},
	"S4":              {"a.ts", sequelizeImport + "@d export default abstract class X extends Model { @c y = 1; static associate(m) {} }\n"},
	"S8":              {"a.ts", typeormImport + "export const enum E { A = 1 }\n"},
	"G10":             {"a.ts", "import type { DataSourceOptions } from \"typeorm/data-source\";\n"},
	"G11":             {"a.ts", "import { Entity } from \"./orm\";\n@Entity()\nexport class T {}\n"},
	"G12":             {"a.ts", "export enum E { A }\n"},
	"M-property":      {"a.ts", typeormImport + "class T { @Column() readonly a?: string = \"x\"; }\n"},
	"M-method":        {"a.ts", typeormImport + "class T { @AfterLoad() m() {} }\n"},
	"M-unknown":       {"a.ts", typeormImport + "class T { @Column() [k]: number; }\n"},
	"T-keyword":       {"a.ts", typeormImport + "class T { a: string; }\n"},
	"T-reference":     {"a.ts", typeormImport + "class T { a: Promise<string>; }\n"},
	"T-union":         {"a.ts", typeormImport + "class T { a: string | null; }\n"},
	"T-intersection":  {"a.ts", typeormImport + "class T { a: A & B; }\n"},
	"T-array":         {"a.ts", typeormImport + "class T { a: A[]; }\n"},
	"T-literal":       {"a.ts", typeormImport + "class T { a: \"x\"; }\n"},
	"T-tuple":         {"a.ts", typeormImport + "class T { a: [A]; }\n"},
	"T-function":      {"a.ts", typeormImport + "class T { a: () => void; }\n"},
	"T-wrap":          {"a.ts", typeormImport + "class T { a: (string); }\n"},
	"T-unknown":       {"a.ts", typeormImport + "class T { a: typeof x; }\n"},
	"E-conditional":   {"a.ts", typeormImport + "const t = f(a ? b : c);\n"},
	"S5":              {"a.ts", sequelizeImport + "export const { a, b: c } = require(\"sequelize\");\n"},
	"S6":              {"a.ts", sequelizeImport + "export default function h(t) { return {}; }\n"},
	"S7":              {"a.ts", sequelizeImport + "for (const k of Object.keys(db)) db[k].f();\n"},
	"B-declaration":   {"a.ts", sequelizeImport + "f(() => { const a = 1; });\n"},
	"B-destructure":   {"a.ts", sequelizeImport + "f(() => { const { a } = b; });\n"},
	"B-expression":    {"a.ts", sequelizeImport + "f(() => { g(); });\n"},
	"B-return":        {"a.ts", sequelizeImport + "f(() => { return 1; });\n"},
	"B-class":         {"a.ts", sequelizeImport + "f(() => { @d abstract class X extends Model { @c y = 1; static get z() { return 1; } } });\n"},
	"B-if":            {"a.ts", sequelizeImport + "f(() => { if (a) { g(); } else h(); });\n"},
	"B-try":           {"a.ts", typeormImport + "f(() => { try { g(); } catch { h(); } finally { i(); } });\n"},
	"B-unknown":       {"a.ts", sequelizeImport + "f(() => { while (a) {} });\n"},
	"E-await":         {"a.ts", typeormImport + "f(async () => { await g(); });\n"},
	"E-async":         {"a.ts", typeormImport + "f(async (o) => o);\n"},
	"E-new":           {"a.ts", sequelizeImport + "const t = new S(1);\n"},
	"E-function":      {"a.ts", sequelizeImport + "f(function g(a) { return a; });\n"},
	"E-assign":        {"a.ts", sequelizeImport + "a.b = 1;\n"},
	"E-index":         {"a.ts", sequelizeImport + "f(a[b]);\n"},
	"E-binary":        {"a.ts", sequelizeImport + "f(a || b);\n"},
	"E-unary":         {"a.ts", sequelizeImport + "f(typeof a);\n"},
	"E-call":          {"a.ts", drizzleImport + "const t = pgTable(\"t\");\n"},
	"E-member":        {"a.ts", drizzleImport + "const t = f(a.b);\n"},
	"E-identifier":    {"a.ts", drizzleImport + "const t = f(a);\n"},
	"E-string":        {"a.ts", drizzleImport + "const t = f(\"s\");\n"},
	"E-number":        {"a.ts", drizzleImport + "const t = f(-1);\n"},
	"E-boolean":       {"a.ts", drizzleImport + "const t = f(true);\n"},
	"E-null":          {"a.ts", drizzleImport + "const t = f(null);\n"},
	"E-template":      {"a.ts", drizzleImport + "const t = f(`a${1}b`);\n"},
	"E-tagged":        {"a.ts", drizzleImport + "const t = sql`a${1}b`;\n"},
	"E-object":        {"a.ts", drizzleImport + "const t = { a: 1 };\n"},
	"E-property":      {"a.ts", drizzleImport + "const t = { a: 1 };\n"},
	"E-shorthand":     {"a.ts", drizzleImport + "const t = { a };\n"},
	"E-spread-entry":  {"a.ts", drizzleImport + "const t = { ...a };\n"},
	"E-method":        {"a.ts", drizzleImport + "const t = { m() { return 1 } };\n"},
	"E-entry-unknown": {"a.ts", drizzleImport + "const t = { [k]: 1 };\n"},
	"E-array":         {"a.ts", drizzleImport + "const t = f([1]);\n"},
	"E-spread":        {"a.ts", drizzleImport + "const t = f(...a);\n"},
	"E-arrow":         {"a.ts", drizzleImport + "const t = f((x) => x);\n"},
	"E-wrap":          {"a.ts", drizzleImport + "const t = f() satisfies T;\n"},
	"E-unknown":       {"a.ts", drizzleImport + "const t = f(a?.b);\n"},
}

// payloadRows parses the "## Payload table" rows (id -> row line).
func payloadRows(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Payload table\n")
	if start < 0 {
		t.Fatalf("%s: no Payload table section", path)
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
			t.Fatalf("%s: duplicate row %s", path, id)
		}
		rows[id] = line
		order = append(order, id)
	}
	return rows, order
}

// filledFields lists the non-zero exported fields of a tagged record; a
// record that is not a struct (a file name, a node kind) fills nothing.
func filledFields(record any) []string {
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
	return out
}

type taggedModel struct {
	row    string
	record any
}

// captureModelTags captures each file and returns every tag emitted.
func captureModelTags(t *testing.T, files map[string]string) []taggedModel {
	t.Helper()
	var tags []taggedModel
	modelTag = func(row string, record any) { tags = append(tags, taggedModel{row, record}) }
	defer func() { modelTag = nil }()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, s := parseModels(t, name, files[name]); s != nil && !s.Valid() {
			t.Fatalf("%s: invalid payload", name)
		}
	}
	return tags
}

func TestModelPayloadContract(t *testing.T) {
	docs, order := payloadRows(t, "../../../docs/lang/typescript-models.md")
	if len(order) != len(modelPayloadRows) {
		t.Errorf("payload table rows = %d, Go table %d", len(order), len(modelPayloadRows))
	}
	want := map[string]bool{}
	for _, id := range order {
		fields, ok := modelPayloadRows[id]
		if !ok {
			t.Errorf("row %s has no modelPayloadRows entry", id)
			continue
		}
		cells := strings.Split(docs[id], "|")
		nothing := strings.TrimSpace(cells[len(cells)-2]) == "nothing"
		if nothing != (len(fields) == 0) {
			t.Errorf("row %s: payload cell %q, Go fields %v", id, cells[len(cells)-2], fields)
		}
		if len(fields) > 0 {
			want[id] = true
		}
	}
	for id := range modelPayloadRows {
		if _, ok := docs[id]; !ok {
			t.Errorf("modelPayloadRows has %s, which is not a payload-table row", id)
		}
	}
	for id := range want {
		snippet, ok := modelSnippets[id]
		if !ok {
			t.Errorf("row %s has no snippet", id)
			continue
		}
		found := false
		for _, tag := range captureModelTags(t, map[string]string{snippet[0]: snippet[1]}) {
			if tag.row != id {
				continue
			}
			found = true
			got := filledFields(tag.record)
			sort.Strings(got)
			exp := append([]string(nil), modelPayloadRows[id]...)
			sort.Strings(exp)
			if !reflect.DeepEqual(got, exp) {
				t.Errorf("row %s: filled %v, want %v (%+v)", id, got, exp, tag.record)
			}
			break
		}
		if !found {
			t.Errorf("row %s: snippet emitted no %s tag", id, id)
		}
	}
	// The fixture emits exactly the rows of the table (with and without a
	// capture side), so a branch without a row and a row without a branch
	// both fail.
	sources := map[string]string{}
	for _, root := range []string{"../../../testdata/fixtures/drizzle-syntax", "../../../testdata/fixtures/sequelize-syntax", "../../../testdata/fixtures/typeorm-syntax"} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || (!strings.HasSuffix(p, ".ts") && !strings.HasSuffix(p, ".js")) {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(filepath.Dir(root), p)
			sources[filepath.ToSlash(rel)] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal("reading fixture modules", err)
		}
	}
	if len(sources) == 0 {
		t.Fatal("no fixture modules")
	}
	emitted := map[string]bool{}
	for _, tag := range captureModelTags(t, sources) {
		emitted[tag.row] = true
	}
	for _, id := range order {
		if !emitted[id] {
			t.Errorf("fixture emits no %s tag", id)
		}
	}
	for id := range emitted {
		if _, ok := docs[id]; !ok {
			t.Errorf("capture emitted tag %s, which is not a payload-table row", id)
		}
	}
}

// TestModelPayloadSequelize checks the version 2 statements on the Sequelize
// forms the T018g3 corpora use: a CommonJS factory with define and an
// associate assignment, destructured requires, the instance, the associate
// loop, classes with extends, init, and function bodies.
func TestModelPayloadSequelize(t *testing.T) {
	factory := `const { addHook } = require("../utils/hook");

module.exports = (sequelize, DataTypes) => {
  const area = sequelize.define("t_area", { id: { type: DataTypes.UUID, primaryKey: true } }, {});
  area.associate = (models) => {
    area.belongsTo(models.User, { foreignKey: "user_id" });
  };
  if (process.env.X) {
    area.y = 1;
  }
  return area;
};
`
	_, s := parseModels(t, "models/area.js", factory)
	if s == nil || len(s.Declarations) != 0 || len(s.Statements) != 2 {
		t.Fatalf("factory payload %+v", s)
	}
	if got := shapeStatement(s.Statements[0]); got != `const {addHook:addHook} = require("../utils/hook")` {
		t.Errorf("destructure %s", got)
	}
	exp := s.Statements[1]
	if exp.Kind != StatementExpression || exp.Value.Kind != ModelAssign || shape(*exp.Value.Left) != "module.exports" || exp.Span.StartLine != 3 {
		t.Fatalf("module.exports %+v", exp)
	}
	arrow := exp.Value.Right
	want := []string{
		`const area = sequelize.define("t_area", {id: {type: DataTypes.UUID, primaryKey: true}}, {})`,
		`area.associate = (models) => {return ?}`,
		"if process.env.X {area.y = 1}",
		"return area",
	}
	var got []string
	for _, st := range arrow.Statements {
		got = append(got, shapeStatement(st))
	}
	if !arrow.Block || !reflect.DeepEqual(arrow.Params, []string{"sequelize", "DataTypes"}) || !reflect.DeepEqual(got, want) {
		t.Errorf("factory body %v params %v", got, arrow.Params)
	}
	associate := arrow.Statements[1].Value.Right
	if len(associate.Statements) != 1 || shapeStatement(associate.Statements[0]) != `area.belongsTo(models.User, {foreignKey: "user_id"})` || associate.Statements[0].Span.StartLine != 6 {
		t.Errorf("associate body %+v", associate.Statements)
	}

	index := `const Sequelize = require("sequelize");
const AreaModel = require("./area");
const config = getConfig();
const sequelize = new Sequelize("mainDB", null, null, config);
const models = { Area: AreaModel(sequelize, Sequelize) };
Object.values(models)
  .filter((model) => typeof model.associate === "function")
  .forEach((model) => model.associate(models));
module.exports = { sequelize, ...models };
`
	_, s = parseModels(t, "models/index.js", index)
	var decls []string
	for _, d := range s.Declarations {
		decls = append(decls, d.Name+"="+shape(d.Value))
	}
	wantDecls := []string{`Sequelize=require("sequelize")`, `AreaModel=require("./area")`, "config=getConfig()", `sequelize=new Sequelize("mainDB", null, null, config)`, "models={Area: AreaModel(sequelize, Sequelize)}"}
	if !reflect.DeepEqual(decls, wantDecls) || len(s.Statements) != 2 {
		t.Fatalf("index declarations %v statements %d", decls, len(s.Statements))
	}
	if got := shapeStatement(s.Statements[0]); got != "Object.values(models).filter((model) => (typeof model.associate === \"function\")).forEach((model) => model.associate(models))" || s.Statements[0].Span.StartLine != 6 || s.Statements[0].Span.EndLine != 8 {
		t.Errorf("associate loop %s %+v", got, s.Statements[0].Span)
	}

	class := `import { DataTypes, Model } from "sequelize";
import sequelize from "../lib/sequelize";

export abstract class WithId<T> extends Model<T> {}

@Table
class Order extends WithId<OrderAttributes> implements Thing {
  @Column declare id: number;
  static associate() {}
}

Order.init(
  { id: { type: DataTypes.INTEGER, primaryKey: true }, total: DataTypes.VIRTUAL, link: { type: new DataTypes.VIRTUAL(DataTypes.STRING, ["slug"]) } },
  { sequelize, tableName: "Orders", paranoid: true },
);
Temporal(Order, sequelize);
Order.removeAttribute("id");
export default Order;
export class Plain {}
export default class extends Model {}
`
	_, s = parseModels(t, "models/Order.ts", class)
	var stmts []string
	for _, st := range s.Statements {
		line := shapeStatement(st)
		if st.Kind == StatementClass {
			line += " " + map[bool]string{true: "exported", false: "local"}[st.Exported] + " " + map[bool]string{true: "abstract", false: "concrete"}[st.Abstract] + " " + map[bool]string{true: "default", false: "named"}[st.Default] + " " + string(rune('0'+st.Decorators))
		}
		stmts = append(stmts, line)
	}
	wantStmts := []string{
		"class WithId extends Model exported abstract named 0",
		"class Order extends WithId local concrete named 2",
		`Order.init({id: {type: DataTypes.INTEGER, primaryKey: true}, total: DataTypes.VIRTUAL, link: {type: new DataTypes.VIRTUAL(DataTypes.STRING, ["slug"])}}, {sequelize, tableName: "Orders", paranoid: true})`,
		"Temporal(Order, sequelize)",
		`Order.removeAttribute("id")`,
		"class Plain exported concrete named 0",
		"class  extends Model exported concrete default 0",
	}
	if !reflect.DeepEqual(stmts, wantStmts) {
		t.Errorf("class statements\n got %q\nwant %q", stmts, wantStmts)
	}
	if s.Statements[1].Span.StartLine != 6 || s.Statements[2].Span.StartLine != 12 {
		t.Errorf("spans %+v %+v", s.Statements[1].Span, s.Statements[2].Span)
	}
	if s.Default == nil || s.Default.Value.Kind != ModelUnknown {
		t.Errorf("default class %+v", s.Default)
	}

	// A sequelize-cli style factory: a class inside a function body.
	cli := `"use strict";
const { Model } = require("sequelize");
module.exports = function (sequelize, DataTypes) {
  class User extends Model {}
  User.init({ name: DataTypes.STRING }, { sequelize, modelName: "User" });
  return User;
};
`
	_, s = parseModels(t, "models/user.js", cli)
	body := s.Statements[1].Value.Right
	if body.Kind != ModelFunction || shapeStatements(body.Statements) != `class User extends Model; User.init({name: DataTypes.STRING}, {sequelize, modelName: "User"}); return User` {
		t.Errorf("function body %s", shape(*body))
	}
}
