package typescript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sequelizeCensusGates are the gate counts per corpus (T018g3 task file,
// "Payload decision": opencollective-api 281 files import sequelize, 224 of
// them outside migrations/; Gladys 48 files require it outside migrations
// and 32 factories pass G6). G7 and G8 are the options module and the
// environment selector of open point O2 (Gladys's config/config.js and
// utils/getConfig.js). G9 (version 3, T018g3 Revision 2) is
// opencollective-api's attribute helper module server/models/DataTypes.ts.
// The second map counts the same files outside migrations/.
var sequelizeCensusGates = map[string][2]map[string]int{
	"opencollective-api": {{"G4": 281, "G9": 1}, {"G4": 224, "G9": 1}},
	"Gladys":             {{"G5": 52, "G6": 32, "G7": 1, "G8": 1}, {"G5": 48, "G6": 32, "G7": 1, "G8": 1}},
}

// sequelizeCorpusDirs maps the inventory's corpus names to checkout names.
var sequelizeCorpusDirs = map[string]string{"opencollective-api": "opencollective-api", "Gladys": "gladys"}

// TestSequelizeCensus checks that the version 2 payload captures exactly the
// syntax the investigation's inventory (docs/contract/claims/data/sequelize-models-inventory.jsonl) found in
// opencollective-api and Gladys: the gate counts, the instance, every model
// definition (module-level init statements with their class headers, define
// calls inside CommonJS factories), every attribute and attribute key, every
// option key and index, every association call in reach order, and the D
// forms the linker must see; that no init or define site outside the
// inventory is captured in the read scope; and the payload side of the
// discovery rows (see discovery). It only parses; set
// GRAPHVEIN_SEQUELIZE_CENSUS_CORPUS to the .corpus directory to run it.
func TestSequelizeCensus(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_SEQUELIZE_CENSUS_CORPUS")
	if dir == "" {
		t.Skip("GRAPHVEIN_SEQUELIZE_CENSUS_CORPUS is not set")
	}
	rows := sequelizeInventory(t, "read")
	discovery := sequelizeInventory(t, "discovery")
	for _, corpus := range []string{"opencollective-api", "Gladys"} {
		t.Run(corpus, func(t *testing.T) {
			root := filepath.Join(dir, sequelizeCorpusDirs[corpus])
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("%s: %v (run scripts/cloud-corpus.sh %s)", corpus, err, sequelizeCorpusDirs[corpus])
			}
			payloads, gates := drizzleCapture(t, root)
			want := sequelizeCensusGates[corpus]
			for gate, list := range gates {
				if gate == "G10" || gate == "G11" || gate == "G12" {
					// TypeORM rows (version 5): opencollective-api's enum
					// modules pass G12; TestTypeORMCensus counts them.
					t.Logf("%s files = %d", gate, len(list))
					continue
				}
				outside := 0
				for _, f := range list {
					if !strings.Contains(f, "migrations/") {
						outside++
					}
				}
				if len(list) != want[0][gate] || outside != want[1][gate] {
					t.Errorf("%s files = %d (%d outside migrations/), want %d (%d)", gate, len(list), outside, want[0][gate], want[1][gate])
				}
			}
			for gate := range want[0] {
				if len(gates[gate]) == 0 {
					t.Errorf("no %s files, want %d", gate, want[0][gate])
				}
			}
			c := newSequelizeCensus(t, payloads)
			for _, r := range rows[corpus] {
				c.check(r)
			}
			c.nothingExtra(corpus, rows[corpus])
			for _, r := range discovery[corpus] {
				c.discovery(r)
			}
			size := 0
			statements := 0
			for _, s := range payloads {
				data, _ := json.Marshal(s)
				size += len(data)
				statements += len(s.Statements)
			}
			t.Logf("%s: %d gated files, %d module-level statements, %d payload bytes; %d inventory rows checked", corpus, len(payloads), statements, size, len(rows[corpus]))
		})
	}
}

func sequelizeInventory(t *testing.T, scope string) map[string][]drizzleRow {
	t.Helper()
	f, err := os.Open("../../../docs/contract/claims/data/sequelize-models-inventory.jsonl")
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
		if r.Scope == scope {
			out[r.Corpus] = append(out[r.Corpus], r)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// sequelizeModel is one model definition found in the payload: the
// attributes and options objects of its init or define call.
type sequelizeModel struct {
	file       string
	line       int
	attributes *ModelExpr
	options    *ModelExpr
}

type sequelizeCensus struct {
	t        *testing.T
	payloads map[string]*ModelSyntax
	// models by inventory id: the class name (init) or the define name.
	models map[string]sequelizeModel
	// classes by file and name.
	classes map[string]ModelStatement
	// calls by file and line: module-level statements and statements of
	// associate bodies, the reach the inventory records.
	calls map[string][]ModelExpr
	// declarations by file and line.
	decls map[string]ModelDeclaration
	// moduleExports by file and line.
	moduleExports map[string]ModelExpr
}

func censusKey(file string, line int) string { return fmt.Sprintf("%s#%d", file, line) }

func newSequelizeCensus(t *testing.T, payloads map[string]*ModelSyntax) *sequelizeCensus {
	c := &sequelizeCensus{t: t, payloads: payloads, models: map[string]sequelizeModel{}, classes: map[string]ModelStatement{}, calls: map[string][]ModelExpr{}, decls: map[string]ModelDeclaration{}, moduleExports: map[string]ModelExpr{}}
	for file, s := range payloads {
		for _, d := range s.Declarations {
			c.decls[censusKey(file, d.Span.StartLine)] = d
		}
		for _, st := range s.Statements {
			switch {
			case st.Kind == StatementClass:
				c.classes[file+"#"+st.Name] = st
			case st.Value == nil:
			case st.Value.Kind == ModelCall:
				c.calls[censusKey(file, st.Span.StartLine)] = append(c.calls[censusKey(file, st.Span.StartLine)], *st.Value)
				c.addInit(file, *st.Value)
			case st.Value.Kind == ModelAssign && shape(*st.Value.Left) == "module.exports":
				c.moduleExports[censusKey(file, st.Span.StartLine)] = *st.Value.Right
				if fn := st.Value.Right; fn.Kind == ModelArrow || fn.Kind == ModelFunction {
					c.factory(file, fn.Statements)
				}
			}
		}
	}
	return c
}

// addInit records `X.init({...}, {...})`.
func (c *sequelizeCensus) addInit(file string, call ModelExpr) {
	if call.Callee.Kind != ModelMember || call.Callee.Name != "init" || call.Callee.Object.Kind != ModelIdentifier || len(call.Args) != 2 || call.Args[0].Kind != ModelObject || call.Args[1].Kind != ModelObject {
		return
	}
	c.models[call.Callee.Object.Name] = sequelizeModel{file: file, line: call.Span.StartLine, attributes: &call.Args[0], options: &call.Args[1]}
}

// factory reads a CommonJS factory body: `const x = sequelize.define(...)`
// and the statements of `x.associate = (models) => {...}`.
func (c *sequelizeCensus) factory(file string, body []ModelStatement) {
	for _, st := range body {
		switch {
		case st.Kind == StatementDeclaration && st.Value != nil && st.Value.Kind == ModelCall && st.Value.Callee.Kind == ModelMember && st.Value.Callee.Name == "define":
			args := st.Value.Args
			if len(args) < 2 || args[0].Kind != ModelString || args[1].Kind != ModelObject {
				continue
			}
			m := sequelizeModel{file: file, line: st.Span.StartLine, attributes: &args[1]}
			if len(args) > 2 {
				m.options = &args[2]
			}
			c.models[args[0].Value] = m
		case st.Kind == StatementExpression && st.Value.Kind == ModelAssign && st.Value.Left.Kind == ModelMember && st.Value.Left.Name == "associate" && st.Value.Right.Kind == ModelArrow:
			for _, inner := range st.Value.Right.Statements {
				if inner.Kind == StatementExpression && inner.Value.Kind == ModelCall {
					k := censusKey(file, inner.Span.StartLine)
					c.calls[k] = append(c.calls[k], *inner.Value)
				}
			}
		}
	}
}

// entry finds the entry of an object literal with the key at the line.
func entryAt(obj *ModelExpr, key string, line int) (ModelEntry, bool) {
	if obj == nil || obj.Kind != ModelObject {
		return ModelEntry{}, false
	}
	for _, e := range obj.Entries {
		if e.Key == key && (line == 0 || e.Span.StartLine == line) {
			return e, true
		}
	}
	return ModelEntry{}, false
}

// valueForm reports whether a captured value matches an inventory value
// form (see the inventory's "Aggregates per row and form").
func valueForm(e ModelEntry, form string) bool {
	v := e.Value
	switch {
	case form == "shorthand":
		return e.Kind == ModelShorthand
	case form == "method":
		return e.Kind == ModelMethod
	case form == "function":
		return v.Kind == ModelFunction || v.Kind == ModelArrow
	case form == "boolean", form == "string", form == "number", form == "null", form == "object", form == "identifier":
		return v.Kind == form
	case form == "array_literal", form == "array":
		return v.Kind == ModelArray
	case strings.HasPrefix(form, "member:$."):
		return v.Kind == ModelMember && v.Name == strings.TrimPrefix(form, "member:$.")
	case strings.HasPrefix(form, "call:$."):
		method, _, _ := strings.Cut(strings.TrimPrefix(form, "call:$."), ":")
		return v.Kind == ModelCall && v.Callee.Kind == ModelMember && v.Callee.Name == method
	case strings.HasPrefix(form, "object{"):
		if v.Kind != ModelObject {
			return false
		}
		var keys []string
		for _, en := range v.Entries {
			keys = append(keys, en.Key+":"+en.Value.Kind)
		}
		sort.Strings(keys)
		return "object{"+strings.Join(keys, ",")+"}" == form
	}
	return false
}

// typeForm checks a DataTypes value against an inventory type form such as
// `INTEGER`, `STRING(number)`, `ARRAY(new:ExpenseTypesEnum)`, `VIRTUAL(..)`
// (the `new DataTypes.VIRTUAL(...)` form) or `other:string`.
func typeForm(v ModelExpr, form string) bool {
	if form == "other:string" {
		return v.Kind == ModelString
	}
	if strings.HasPrefix(form, "other:member:") {
		return v.Kind == ModelMember && strings.HasSuffix(form, "."+v.Name)
	}
	name, args, call := strings.Cut(form, "(")
	switch {
	case !call:
		return v.Kind == ModelMember && v.Name == name
	case args == "..)":
		return v.Kind == ModelNew && v.Callee.Kind == ModelMember && v.Callee.Name == name
	case v.Kind == ModelCall && v.Callee.Kind == ModelMember && v.Callee.Name == name:
		return true
	}
	return false
}

func (c *sequelizeCensus) check(r drizzleRow) {
	t := c.t
	if c.payloads[r.File] == nil {
		t.Errorf("%s:%d %s %s: file has no payload", r.File, r.Line, r.Row, r.Form)
		return
	}
	model, attr, _ := strings.Cut(r.ID, ".")
	m, haveModel := c.models[model]
	fail := func(what string) { t.Errorf("%s:%d %s %s (%s): %s", r.File, r.Line, r.Row, r.Form, r.ID, what) }
	switch {
	case strings.HasPrefix(r.Form, "init:"):
		extends := strings.TrimSuffix(strings.TrimPrefix(r.Form, "init:extends="), ":module_statement")
		cls, ok := c.classes[r.File+"#"+r.ID]
		if !haveModel || m.file != r.File || m.line != r.Line {
			fail("no module-level init statement")
		} else if !ok || cls.Extends == nil || cls.Extends.Kind != ModelIdentifier || cls.Extends.Name != extends {
			fail("class header not captured with its extends")
		}
	case strings.HasPrefix(r.Form, "define:") && r.Row == "SQ-M1":
		if m, ok := c.models[r.Name]; !ok || m.file != r.File || m.line != r.Line {
			fail("define declaration not captured in the factory body")
		}
	case r.Form == "attribute_object" || strings.HasPrefix(r.Form, "attribute_shorthand_type:"):
		e, ok := entryAt(m.attributes, attr, r.Line)
		switch {
		case !haveModel || !ok:
			fail("attribute entry not captured")
		case r.Form == "attribute_object" && e.Value.Kind != ModelObject:
			fail("attribute is not an object literal")
		case r.Form != "attribute_object" && !typeForm(e.Value, strings.TrimPrefix(r.Form, "attribute_shorthand_type:")):
			fail("shorthand type " + shape(e.Value))
		}
	case strings.HasPrefix(r.Form, "attr:"):
		key, form, _ := strings.Cut(strings.TrimPrefix(r.Form, "attr:"), "=")
		outer, ok := entryAt(m.attributes, attr, 0)
		if !haveModel || !ok {
			fail("attribute not captured")
			return
		}
		e, ok := entryAt(&outer.Value, key, r.Line)
		switch {
		case !ok:
			fail("attribute key not captured")
		case key == "type" && !typeForm(e.Value, form):
			fail("type " + shape(e.Value))
		case key != "type" && !valueForm(e, form):
			fail("value " + e.Kind + " " + shape(e.Value))
		}
	case strings.HasPrefix(r.Form, "opt:"):
		key, form, _ := strings.Cut(strings.TrimPrefix(r.Form, "opt:"), "=")
		e, ok := entryAt(m.options, key, r.Line)
		if !haveModel || !ok || !valueForm(e, form) {
			fail("option entry not captured")
		}
	case r.Form == "options_object" || r.Form == "options_object_empty" || r.Form == "options_absent":
		switch {
		case !haveModel:
			fail("model not captured")
		case r.Form == "options_absent" && m.options != nil:
			fail("options present")
		case r.Form != "options_absent" && (m.options == nil || m.options.Kind != ModelObject || m.options.Span.StartLine != r.Line || (len(m.options.Entries) == 0) != (r.Form == "options_object_empty")):
			fail("options object not captured")
		}
	case strings.HasPrefix(r.Form, "index{"):
		indexes, ok := entryAt(m.options, "indexes", 0)
		found := false
		for _, el := range indexes.Value.Elements {
			if el.Kind == ModelObject && el.Span.StartLine == r.Line {
				var keys []string
				for _, en := range el.Entries {
					kind := en.Value.Kind
					if kind == ModelArray {
						kind = "array_strings"
						for _, x := range en.Value.Elements {
							if x.Kind != ModelString {
								kind = "array"
							}
						}
					}
					keys = append(keys, en.Key+"="+kind)
				}
				sort.Strings(keys)
				found = "index{"+strings.Join(keys, ",")+"}" == r.Form
			}
		}
		if !haveModel || !ok || !found {
			fail("index element not captured")
		}
	case r.Row == "SQ-R1" || r.Row == "SQ-R2":
		c.association(r)
	case strings.HasPrefix(r.Form, "new_Sequelize:"):
		d, ok := c.decls[censusKey(r.File, r.Line)]
		args := strings.Split(strings.TrimPrefix(r.Form, "new_Sequelize:args="), ",")
		if !ok || d.Value.Kind != ModelNew || shape(*d.Value.Callee) != "Sequelize" || len(d.Value.Args) != len(args) {
			fail("instance declaration not captured")
		}
	case strings.HasPrefix(r.Form, "module_exports:"):
		v, ok := c.moduleExports[censusKey(r.File, r.Line)]
		want := strings.TrimPrefix(r.Form, "module_exports:")
		if !ok || (want == "function" && v.Kind != ModelArrow && v.Kind != ModelFunction) || (want == "identifier" && v.Kind != ModelIdentifier) {
			fail("module.exports assignment not captured")
		}
	case r.Form == "project_wrapper:Temporal" || r.Form == "call:removeAttribute:ctx=module":
		method := map[bool]string{true: "Temporal", false: "removeAttribute"}[r.Form == "project_wrapper:Temporal"]
		found := false
		for _, call := range c.calls[censusKey(r.File, r.Line)] {
			found = found || shape(*call.Callee) == method || (call.Callee.Kind == ModelMember && call.Callee.Name == method)
		}
		if !found {
			fail("module-level call not captured")
		}
	case strings.HasPrefix(r.Form, "import:"):
		// Imports are Facts.Imports, not payload; the gate is checked above.
	case r.Row == "D":
		// D forms inside a function declaration (sequelize-temporal.ts): the
		// file is gated; function declarations are not captured (SD), and
		// the linker reports the define site as unread.
	default:
		fail("row form not handled by the census")
	}
}

// association checks an association call at its line: the method, the
// source and target, and the option keys the inventory lists.
func (c *sequelizeCensus) association(r drizzleRow) {
	method, rest, _ := strings.Cut(r.Form, ":ctx=")
	_, opts, _ := strings.Cut(rest, ":")
	source, target, _ := strings.Cut(r.ID, "->")
	for _, call := range c.calls[censusKey(r.File, r.Line)] {
		if call.Callee.Kind != ModelMember || call.Callee.Name != method || shape(*call.Callee.Object) != source || len(call.Args) == 0 || shape(call.Args[0]) != target {
			continue
		}
		var keys []string
		if len(call.Args) > 1 {
			for _, e := range call.Args[1].Entries {
				key := e.Key + "=" + e.Value.Kind
				if e.Key == "through" && e.Value.Kind == ModelObject {
					var inner []string
					for _, x := range e.Value.Entries {
						inner = append(inner, x.Key)
					}
					sort.Strings(inner)
					key = "through=object{" + strings.Join(inner, ",") + "}"
				}
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		got := "{" + strings.Join(keys, ",") + "}"
		if len(keys) == 0 {
			got = "{none}"
		}
		if got == opts {
			return
		}
		c.t.Errorf("%s:%d %s: options %s, inventory %s", r.File, r.Line, r.ID, got, opts)
		return
	}
	c.t.Errorf("%s:%d %s %s: association call not captured", r.File, r.Line, r.ID, r.Form)
}

// nothingExtra checks that the payload holds no model definition and no
// association call in the read scope that the inventory lacks.
func (c *sequelizeCensus) nothingExtra(corpus string, rows []drizzleRow) {
	models := map[string]bool{}
	assoc := map[string]bool{}
	for _, r := range rows {
		switch {
		case r.Row == "SQ-M2" && strings.HasPrefix(r.Form, "init:"):
			models[r.ID] = true
		case r.Row == "SQ-M1" && strings.HasPrefix(r.Form, "define:"):
			models[r.Name] = true
		case r.Row == "SQ-R1" || r.Row == "SQ-R2":
			assoc[censusKey(r.File, r.Line)] = true
		}
	}
	scope := map[string]string{"opencollective-api": "server/models/", "Gladys": "server/models/"}[corpus]
	for name, m := range c.models {
		if strings.HasPrefix(m.file, scope) && !models[name] {
			c.t.Errorf("%s:%d: model %s captured but not in the inventory", m.file, m.line, name)
		}
	}
	methods := map[string]bool{"belongsTo": true, "hasOne": true, "hasMany": true, "belongsToMany": true}
	for key, calls := range c.calls {
		for _, call := range calls {
			if strings.HasPrefix(key, scope) && call.Callee.Kind == ModelMember && methods[call.Callee.Name] && !assoc[key] {
				c.t.Errorf("association %s at %q captured but not in the inventory", shape(*call.Callee), key)
			}
		}
	}
}

// discovery checks the payload side of the inventory's discovery rows: the
// instance declaration, Gladys's options module and environment selector,
// the factory calls of the models object, the associate loop, and the
// sequelize option of every init call. Discovery rows about package.json,
// lockfiles, JSON configuration, migrations directories and sync sites are
// not TypeScript syntax and are SQB2's. The rows' lines for the loop and the
// association block name a region, not a statement, so those two are found
// by file and shape.
func (c *sequelizeCensus) discovery(r drizzleRow) {
	t := c.t
	fail := func(what string) { t.Errorf("%s:%d %s %s: %s", r.File, r.Line, r.Row, r.Form, what) }
	s := c.payloads[r.File]
	switch {
	case strings.HasPrefix(r.Form, "instance:new_Sequelize("):
		d, ok := c.decls[censusKey(r.File, r.Line)]
		if !ok || d.Value.Kind != ModelNew || shape(*d.Value.Callee) != "Sequelize" || len(d.Value.Args) != 4 {
			fail("instance declaration not captured as new Sequelize with four arguments")
		}
	case strings.HasPrefix(r.Form, "options:getConfig()"):
		v, ok := c.moduleExports[censusKey(r.File, r.Line)]
		if !ok || v.Kind != ModelObject || len(v.Entries) != 3 {
			fail("options module not captured as module.exports = {three entries}")
			return
		}
		for _, env := range v.Entries {
			dialect, okD := entryAt(&env.Value, "dialect", 0)
			define, okF := entryAt(&env.Value, "define", 0)
			if !okD || dialect.Value.Kind != ModelString || dialect.Value.Value != "sqlite" || !okF || define.Value.Kind != ModelObject || len(define.Value.Entries) != 4 {
				fail("entry " + env.Key + " lacks the literal dialect or the define object")
			}
		}
		selector := c.payloads["server/utils/getConfig.js"]
		found := false
		if selector != nil {
			for _, st := range selector.Statements {
				if st.Kind != StatementExpression || st.Value.Kind != ModelAssign || shape(*st.Value.Left) != "module.exports" || st.Value.Right.Kind != ModelArrow {
					continue
				}
				body := st.Value.Right.Statements
				last := body[len(body)-1]
				found = last.Kind == StatementReturn && last.Value != nil && last.Value.Kind == ModelIndex && shape(*last.Value.Object) == "config"
			}
		}
		if !found {
			fail("environment selector utils/getConfig.js not captured as a return of config[...]")
		}
	case r.Form == "model_reach:require_factory_called_with_instance":
		factories := 0
		for _, d := range s.Declarations {
			if d.Name != "models" || d.Value.Kind != ModelObject {
				continue
			}
			for _, e := range d.Value.Entries {
				v := e.Value
				if v.Kind == ModelCall && v.Callee.Kind == ModelIdentifier && len(v.Args) == 2 && shape(v.Args[0]) == "sequelize" && shape(v.Args[1]) == "Sequelize" {
					factories++
				}
			}
		}
		if factories != 32 {
			fail(fmt.Sprintf("%d factory calls in the models object, want 32", factories))
		}
	case strings.HasPrefix(r.Form, "associate_loop:"):
		found := false
		for _, st := range s.Statements {
			v := st.Value
			if st.Kind != StatementExpression || v.Kind != ModelCall || v.Callee.Kind != ModelMember || v.Callee.Name != "forEach" {
				continue
			}
			filter := v.Callee.Object
			if filter.Kind != ModelCall || filter.Callee.Kind != ModelMember || filter.Callee.Name != "filter" || len(filter.Args) != 1 {
				continue
			}
			test := filter.Args[0].Body
			found = test != nil && test.Kind == ModelBinary && test.Value == "===" && test.Left.Kind == ModelUnary && test.Left.Value == "typeof" && test.Right.Kind == ModelString
		}
		if !found {
			fail("associate loop not captured with its typeof filter")
		}
	case r.Form == "associations_module_level_index":
		methods := map[string]bool{"belongsTo": true, "hasOne": true, "hasMany": true, "belongsToMany": true}
		n := 0
		for _, st := range s.Statements {
			if st.Kind == StatementExpression && st.Value.Kind == ModelCall && st.Value.Callee.Kind == ModelMember && methods[st.Value.Callee.Name] {
				n++
			}
		}
		if n != 197 {
			fail(fmt.Sprintf("%d module-level association statements, want 197", n))
		}
	case strings.HasPrefix(r.Form, "model_reach:init_sequelize_option_import"):
		n := 0
		for _, m := range c.models {
			if e, ok := entryAt(m.options, "sequelize", 0); ok && (e.Kind == ModelShorthand || shape(e.Value) == "sequelize") {
				n++
			}
		}
		if n != 52 {
			fail(fmt.Sprintf("%d init calls pass the sequelize option, want 52", n))
		}
	}
}
