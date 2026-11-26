package sequelize

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Options are the instance options the claim rows read (SQ-K1): the
// dialect, the global `define` and `schema`. Sequelize 6.37.8 picks the
// options argument by the constructor's argument count
// (lib/sequelize.js:49-98): one object argument is the options; one
// string, or a string and an object, is the URL form, whose protocol
// overwrites any `dialect` option (:54-57); four arguments put the
// options last; any other count has no options.
type Options struct {
	// Known is false when an option-derived fact cannot be read (a spread
	// or a non-literal `define` without open point O1's proof, an unread
	// options value): every table and column name, the timestamps and the
	// implicit id are then unknown (SQB3). Reason says why.
	Known  bool
	Reason string
	// From says where the options were read: "literal" (an object literal
	// argument or a module-level const of one), "environment" (open point
	// O2: an environment-indexed object whose entries agree), "none" (no
	// options argument).
	From string
	// Source locates the object read ("<file>:<line>").
	Source string
	// Spread names the configuration member paths whose spread was read
	// under SQ-V5 (open point O1), with the files checked.
	Spread []string
	// Define is the literal `define` object, nil when the options have
	// none.
	Define *typescript.ModelExpr
	// Schema is the literal `schema` option; SchemaKnown is false when it
	// is not a string literal.
	Schema      string
	SchemaKnown bool
	// Dialect is the literal `dialect` option ("" when absent, not literal
	// or overwritten by the URL form); DialectNote says why it is not
	// used. DialectModule is true when a `dialectModule` or
	// `dialectModulePath` option is present (open point O3).
	Dialect       string
	DialectNote   string
	DialectModule bool
}

// Sequelize 6's dialects (lib/sequelize.js:166-191); "postgresql" is an
// alias of postgres (:138-140). The supported ones carry claims; db2,
// snowflake and oracle are row D.
var (
	supportedDialects = map[string]bool{"postgres": true, "sqlite": true, "mysql": true, "mariadb": true, "mssql": true}
	otherDialects     = map[string]bool{"db2": true, "snowflake": true, "oracle": true}
)

func canonicalDialect(d string) string {
	if d == "postgresql" {
		return "postgres"
	}
	return d
}

// optionKeys are the option keys whose values matter to a claim; the
// spread check looks for each of them.
var optionKeys = []string{"dialect", "define", "schema", "dialectModule", "dialectModulePath", "hooks"}

// readOptions reads the options of the instance construction n (a `new`
// expression of file).
func (d *discovery) readOptions(file string, n *typescript.ModelExpr, pkg *Package) Options {
	args := n.Args
	for _, a := range args {
		if a.Kind == typescript.ModelSpread {
			return Options{Reason: "the constructor arguments hold a spread, so the options argument is not known"}
		}
	}
	var arg *typescript.ModelExpr
	url := false
	switch {
	case len(args) == 1 && args[0].Kind == typescript.ModelString:
		url = true
	case len(args) == 1:
		arg = &args[0]
	case len(args) == 2:
		url = true
		arg = &args[1]
	case len(args) == 4:
		arg = &args[3]
	case len(args) == 3 || len(args) == 0:
		return Options{Known: true, From: "none", SchemaKnown: true}
	default:
		return Options{Reason: fmt.Sprintf("the constructor has %d arguments", len(args))}
	}
	if arg == nil || arg.Kind == typescript.ModelNull || (arg.Kind == typescript.ModelIdentifier && arg.Name == "undefined") {
		o := Options{Known: true, From: "none", SchemaKnown: true}
		if url {
			o.DialectNote = "URL form: the dialect is the URL's protocol (lib/sequelize.js:54-57)"
		}
		return o
	}
	obj, from, source, why := d.optionsObject(file, arg)
	if obj == nil {
		return Options{Reason: why}
	}
	o := d.readObject(obj, pkg)
	o.From, o.Source = from, source
	if url && o.Dialect != "" {
		o.Dialect = ""
		o.DialectNote = "URL form: the URL's protocol overwrites the dialect option (lib/sequelize.js:54-57)"
	}
	return o
}

// optionsObject finds the object literal an options argument stands for:
// the literal itself, a module-level const of one, or open point O2's
// environment selector (a call of an imported function whose body
// returns `<imported object literal>[<expr>]`, every entry agreeing).
func (d *discovery) optionsObject(file string, arg *typescript.ModelExpr) (*typescript.ModelExpr, string, string, string) {
	e, at := arg, file
	if e.Kind == typescript.ModelIdentifier {
		decl := declaration(d.in.Models[file], e.Name)
		if decl == nil {
			return nil, "", "", "the options argument " + e.Name + " is not a module-level declaration of " + file
		}
		if decl.Keyword != "const" {
			return nil, "", "", "the options argument " + e.Name + " is declared with " + decl.Keyword
		}
		if mutated(d.in.Models[file], e.Name) {
			return nil, "", "", "the options object " + e.Name + " is assigned to at module level"
		}
		e = &decl.Value
	}
	switch e.Kind {
	case typescript.ModelObject:
		return e, "literal", fmt.Sprintf("%s:%d", at, e.Span.StartLine), ""
	case typescript.ModelIndex:
		obj, source, why := d.requiredOptions(file, e)
		if obj == nil {
			return nil, "", "", why
		}
		return obj, "environment", source, ""
	case typescript.ModelCall:
		obj, source, why := d.environmentOptions(file, e)
		if obj == nil {
			return nil, "", "", why
		}
		return obj, "environment", source, ""
	}
	return nil, "", "", "the options argument is not an object literal (" + e.Kind + ")"
}

// mutated reports a module-level assignment whose target is name or one of
// its members.
func mutated(m *typescript.ModelSyntax, name string) bool {
	if m == nil {
		return false
	}
	for i := range m.Statements {
		s := &m.Statements[i]
		if s.Kind == typescript.StatementExpression && s.Value.Kind == typescript.ModelAssign {
			if p := memberPath(s.Value.Left); p == name || strings.HasPrefix(p, name+".") {
				return true
			}
		}
	}
	return false
}

// environmentOptions reads open point O2: `getConfig()` where getConfig
// is a relative require or import of a module whose exported function
// (module.exports or export default, gate row G8) only declares constants
// and returns `<id>[<expr>]`, <id> a relative require or import of a
// module exporting an object literal whose entries are object literals
// that agree on every option key. The first entry is read.
func (d *discovery) environmentOptions(file string, call *typescript.ModelExpr) (*typescript.ModelExpr, string, string) {
	if call.Callee.Kind != typescript.ModelIdentifier || len(call.Args) != 0 {
		return nil, "", "the options come from a call that is not an argument-free call of an imported function"
	}
	fn := d.local(file, call.Callee.Name, 0)
	if fn.Expr == nil || fn.File == "" || (fn.Expr.Kind != typescript.ModelArrow && fn.Expr.Kind != typescript.ModelFunction) {
		return nil, "", "the options come from " + call.Callee.Name + "(), which is not a function of a module this pass reads"
	}
	ret := returned(fn.Expr)
	if ret == nil || ret.Kind != typescript.ModelIndex || ret.Object.Kind != typescript.ModelIdentifier {
		return nil, "", "the options come from " + call.Callee.Name + "() in " + fn.File + ", whose body is not constant declarations and `return <object>[<key>]`"
	}
	table := d.local(fn.File, ret.Object.Name, 0)
	if table.Expr == nil || table.Expr.Kind != typescript.ModelObject || table.File == fn.File {
		return nil, "", "the options come from " + ret.Object.Name + "[...] in " + fn.File + ", which is not an object literal of a required or imported module"
	}
	first, why := agreeingEntry(table.Expr, table.File)
	if first == nil {
		return nil, "", why
	}
	return first, fmt.Sprintf("%s:%d", table.File, table.Expr.Span.StartLine), ""
}

// agreeingEntry returns the first entry of an environment-indexed options
// table whose entries are object literals that agree on every option key
// (open point O2), or why not.
func agreeingEntry(table *typescript.ModelExpr, file string) (*typescript.ModelExpr, string) {
	var first *typescript.ModelExpr
	for i := range table.Entries {
		en := &table.Entries[i]
		if en.Kind != typescript.ModelProperty || en.Value.Kind != typescript.ModelObject {
			return nil, fmt.Sprintf("entry %d of the options table in %s is not a property with an object literal", i+1, file)
		}
		if first == nil {
			first = &en.Value
			continue
		}
		for _, k := range optionKeys {
			a, b := optionValue(first, k), optionValue(&en.Value, k)
			if !sameExpr(a, b) {
				return nil, fmt.Sprintf("the entries of the options table in %s disagree on %s (%s and %s)", file, k, table.Entries[0].Key, en.Key)
			}
		}
	}
	if first == nil {
		return nil, "the options table in " + file + " is empty"
	}
	return first, ""
}

// requiredOptions reads the sequelize-cli form of open point
// O2: `require("<relative>")[<expr>]` (the template's
// `require(__dirname + "/../config/config.json")[env]`), directly or
// through a module-level const bound to the require, where the required
// module is a JSON file or a CommonJS module exporting an object literal
// whose entries agree on every option key; the first entry is read.
func (d *discovery) requiredOptions(file string, e *typescript.ModelExpr) (*typescript.ModelExpr, string, string) {
	spec := requireSpec(e.Object)
	if spec == "" && e.Object.Kind == typescript.ModelIdentifier {
		if decl := declaration(d.in.Models[file], e.Object.Name); decl != nil && decl.Keyword == "const" {
			spec = requireSpec(&decl.Value)
		}
	}
	if spec == "" || !strings.HasPrefix(spec, ".") {
		return nil, "", "the options come from an index into a value that is not a relative require"
	}
	target, why := d.resolveRequire(file, spec)
	if target == "" {
		return nil, "", "the options come from require(" + quote(spec) + "): " + why
	}
	var table *typescript.ModelExpr
	if path.Ext(target) == ".json" {
		t, err := jsonExpr(d.in.Sources[target])
		if err != nil {
			return nil, "", fmt.Sprintf("the options file %s does not parse as JSON: %v", target, err)
		}
		table = t
	} else {
		v := d.cjsExport(target, "", 0)
		if v.Expr == nil || v.Expr.Kind != typescript.ModelObject {
			return nil, "", "the options module " + target + " does not export an object literal"
		}
		table = v.Expr
	}
	if table.Kind != typescript.ModelObject {
		return nil, "", "the options file " + target + " is not an object"
	}
	first, why := agreeingEntry(table, target)
	if first == nil {
		return nil, "", why
	}
	d.in.tag("SQ-K1/`require(config)[env]`")
	return first, fmt.Sprintf("%s:%d", target, table.Span.StartLine), ""
}

// jsonExpr reduces a JSON document to a model expression, keeping the
// key order (objects become object literals of properties).
func jsonExpr(data []byte) (*typescript.ModelExpr, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var read func() (typescript.ModelExpr, error)
	read = func() (typescript.ModelExpr, error) {
		tok, err := dec.Token()
		if err != nil {
			return typescript.ModelExpr{}, err
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				e := typescript.ModelExpr{Kind: typescript.ModelObject, Span: contract.Span{StartLine: 1, EndLine: 1}}
				for dec.More() {
					k, err := dec.Token()
					if err != nil {
						return e, err
					}
					val, err := read()
					if err != nil {
						return e, err
					}
					key, _ := k.(string)
					e.Entries = append(e.Entries, typescript.ModelEntry{Kind: typescript.ModelProperty, Key: key, Value: val})
				}
				_, err := dec.Token()
				return e, err
			case '[':
				e := typescript.ModelExpr{Kind: typescript.ModelArray}
				for dec.More() {
					val, err := read()
					if err != nil {
						return e, err
					}
					e.Elements = append(e.Elements, val)
				}
				_, err := dec.Token()
				return e, err
			}
		case string:
			return typescript.ModelExpr{Kind: typescript.ModelString, Value: v}, nil
		case json.Number:
			return typescript.ModelExpr{Kind: typescript.ModelNumber, Value: v.String()}, nil
		case bool:
			return typescript.ModelExpr{Kind: typescript.ModelBoolean, Value: fmt.Sprint(v)}, nil
		case nil:
			return typescript.ModelExpr{Kind: typescript.ModelNull}, nil
		}
		return typescript.ModelExpr{}, fmt.Errorf("unexpected token %v", tok)
	}
	e, err := read()
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("data after the document")
	}
	return &e, nil
}

// returned returns the expression a function returns when its body is only
// constant declarations followed by one return.
func returned(fn *typescript.ModelExpr) *typescript.ModelExpr {
	if fn.Kind == typescript.ModelArrow && !fn.Block {
		return fn.Body
	}
	var ret *typescript.ModelExpr
	for i, s := range fn.Statements {
		switch {
		case s.Kind == typescript.StatementDeclaration && s.Keyword == "const":
		case s.Kind == typescript.StatementReturn && i == len(fn.Statements)-1:
			ret = s.Value
		default:
			return nil
		}
	}
	return ret
}

// optionValue returns the entry of an options object for key: the value,
// or a marker expression of kind "unknown" when a spread or unread entry
// could set it, or nil when absent.
func optionValue(obj *typescript.ModelExpr, key string) *typescript.ModelExpr {
	var found *typescript.ModelExpr
	for i := range obj.Entries {
		en := &obj.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand, typescript.ModelMethod:
			if en.Key == key {
				found = &en.Value
			}
		default:
			found = &typescript.ModelExpr{Kind: typescript.ModelUnknown}
		}
	}
	return found
}

// sameExpr compares two reduced expressions without their spans.
func sameExpr(a, b *typescript.ModelExpr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(stripSpans(*a), stripSpans(*b))
}

func stripSpans(e typescript.ModelExpr) typescript.ModelExpr {
	data, _ := json.Marshal(e)
	var m any
	_ = json.Unmarshal(data, &m)
	var walk func(any) any
	walk = func(x any) any {
		switch v := x.(type) {
		case map[string]any:
			delete(v, "span")
			for k, c := range v {
				v[k] = walk(c)
			}
		case []any:
			for i, c := range v {
				v[i] = walk(c)
			}
		}
		return x
	}
	data, _ = json.Marshal(walk(m))
	var out typescript.ModelExpr
	_ = json.Unmarshal(data, &out)
	return out
}

// readObject reads the option keys of a literal options object in order:
// a later entry overrides an earlier one; a spread overrides every key it
// may hold (open point O1 proves which keys a configuration spread cannot
// hold); an entry the payload could not read (computed key, accessor)
// could be any key.
func (d *discovery) readObject(obj *typescript.ModelExpr, pkg *Package) Options {
	o := Options{Known: true, SchemaKnown: true}
	vals := map[string]*typescript.ModelExpr{}
	for i := range obj.Entries {
		en := &obj.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand, typescript.ModelMethod:
			vals[en.Key] = &en.Value
		case typescript.ModelSpreadIn:
			keys, src, why := d.spreadKeys(obj, en, pkg)
			if why != "" {
				return Options{Reason: why}
			}
			o.Spread = append(o.Spread, src)
			d.in.tag("SQ-K1/a spread")
			for _, k := range keys {
				vals[k] = &typescript.ModelExpr{Kind: typescript.ModelUnknown}
			}
		default:
			return Options{Reason: fmt.Sprintf("the options object holds an entry the payload does not read (line %d)", en.Span.StartLine)}
		}
	}
	if vals["hooks"] != nil {
		// Instance hooks: beforeDefine and afterDefine run
		// on every define and init and may change any attribute or option.
		d.in.tag("D")
		return Options{Reason: "the options hold a hooks key: instance hooks (beforeDefine, afterDefine, ...) may change every model's attributes and options (row D)"}
	}
	if v := vals["define"]; v != nil {
		if v.Kind != typescript.ModelObject {
			return Options{Reason: "the define option is not an object literal (" + v.Kind + ")"}
		}
		o.Define = v
		d.in.tag("SQ-K1/`define`")
	}
	if v := vals["schema"]; v != nil {
		if v.Kind == typescript.ModelString {
			o.Schema = v.Value
			d.in.tag("SQ-K1/`schema`")
		} else {
			o.SchemaKnown = false
		}
	}
	if v := vals["dialect"]; v != nil {
		if v.Kind == typescript.ModelString {
			o.Dialect = canonicalDialect(v.Value)
		} else {
			o.DialectNote = "the dialect option is not a string literal (" + v.Kind + ")"
		}
	}
	o.DialectModule = vals["dialectModule"] != nil || vals["dialectModulePath"] != nil
	return o
}

// spreadKeys reads open point O1: a spread of a member path of the
// node-config object (a default import of the `config` package, or its
// require) is read under SQ-V5 when every file of the package's config/
// directory is checked: JSON files (comments allowed, as node-config
// reads them) have none of the option keys under that path, and no other
// file there names one. It returns the option keys the spread may set
// (a key a file holds under the path), with a description of the check.
func (d *discovery) spreadKeys(obj *typescript.ModelExpr, en *typescript.ModelEntry, pkg *Package) ([]string, string, string) {
	p := memberPath(&en.Value)
	root, rest, ok := strings.Cut(p, ".")
	if p == "" || !ok {
		return nil, "", fmt.Sprintf("the options object spreads a value that is not a member path of a configuration object (line %d)", en.Span.StartLine)
	}
	file := d.current
	v := d.local(file, root, 0)
	if v.External != "config" || (v.Name != "default" && v.Name != "") {
		return nil, "", fmt.Sprintf("the options object spreads %s, and %s is not the node-config object (an import or require of `config`)", p, root)
	}
	dir := path.Join(pkg.Dir, "config")
	var files []string
	for f := range d.in.Sources {
		if path.Dir(f) == dir {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, "", "the options object spreads " + p + ", and " + dir + "/ holds no configuration file"
	}
	segments := strings.Split(rest, ".")
	keys := map[string]bool{}
	for _, f := range files {
		src := d.in.Sources[f]
		if path.Ext(f) != ".json" {
			// A file node-config could load in another format: any key it
			// names as a word may be set.
			for _, k := range optionKeys {
				if regexp.MustCompile(`\b` + k + `\b`).Match(src) {
					keys[k] = true
				}
			}
			continue
		}
		var tree any
		if err := json.Unmarshal(stripJSONComments(src), &tree); err != nil {
			return nil, "", fmt.Sprintf("the options object spreads %s, and %s does not parse as JSON: %v", p, f, err)
		}
		node := tree
		for _, s := range segments {
			m, ok := node.(map[string]any)
			if !ok {
				node = nil
				break
			}
			node = m[s]
		}
		if node == nil {
			continue
		}
		m, ok := node.(map[string]any)
		if !ok {
			return nil, "", fmt.Sprintf("the options object spreads %s, and %s holds a non-object there", p, f)
		}
		for _, k := range optionKeys {
			if _, ok := m[k]; ok {
				keys[k] = true
			}
		}
	}
	if keys["define"] {
		return nil, "", "the options object spreads " + p + ", and a file of " + dir + "/ may set define under it"
	}
	if keys["hooks"] {
		return nil, "", "the options object spreads " + p + ", and a file of " + dir + "/ may set hooks under it (instance hooks, row D)"
	}
	var out []string
	for _, k := range optionKeys {
		if keys[k] && k != "define" && k != "hooks" {
			out = append(out, k)
		}
	}
	return out, fmt.Sprintf("%s (%d files of %s/ checked)", p, len(files), dir), ""
}

// stripJSONComments removes // and /* */ comments outside strings, as
// node-config's JSON parser does before JSON.parse.
func stripJSONComments(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(string(src[i+2:]), "*/")
			if end < 0 {
				return out
			}
			i += end + 3
		default:
			out = append(out, c)
		}
	}
	return out
}

// dialectRule decides the instance's dialect (SQ-V4, open point O3): the
// literal dialect option, else the package's single Sequelize driver; a
// dialectModule option, no driver or several make it unknown.
func dialectRule(o Options, pkg *Package, tag func(string)) (dialect, from, why string) {
	if o.Dialect != "" {
		if !supportedDialects[o.Dialect] {
			return o.Dialect, "literal", "dialect " + o.Dialect + " is not one the claim contract covers (row D)"
		}
		tag("SQ-K1/literal `dialect`")
		return o.Dialect, "literal", ""
	}
	prefix := ""
	if o.DialectNote != "" {
		prefix = o.DialectNote + "; "
	}
	if o.DialectModule {
		return "", "", prefix + "a dialectModule option replaces the driver, so the driver rule does not apply"
	}
	switch len(pkg.Drivers) {
	case 0:
		return "", "", prefix + "the package depends on no Sequelize driver"
	case 1:
	default:
		return "", "", prefix + "the package depends on several Sequelize drivers (" + strings.Join(pkg.Drivers, ", ") + ")"
	}
	d := driverDialects[pkg.Drivers[0]]
	if !supportedDialects[d] {
		return d, "driver", "driver " + pkg.Drivers[0] + " implies dialect " + d + ", which the claim contract does not cover (row D)"
	}
	tag("SQ-K1/single-driver rule")
	return d, "driver", ""
}

// driverDialects are the driver packages Sequelize 6.37.8 loads per
// dialect (lib/dialects/<dialect>/connection-manager.js, `_loadDialectModule`).
var driverDialects = map[string]string{
	"pg": "postgres", "sqlite3": "sqlite", "mysql2": "mysql", "mariadb": "mariadb", "tedious": "mssql",
	"ibm_db": "db2", "oracledb": "oracle", "snowflake-sdk": "snowflake",
}

// spanLine renders a span's start as "<file>:<line>".
func spanLine(file string, s contract.Span) string { return fmt.Sprintf("%s:%d", file, s.StartLine) }
