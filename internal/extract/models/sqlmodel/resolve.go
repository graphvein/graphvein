package sqlmodel

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// sqlalchemyReexports are the SQLAlchemy names that SQLModel 0.0.39 re-exports
// unchanged (`sqlmodel/__init__.py`): `from sqlmodel import Column` is
// `sqlalchemy.Column` (row L1). Only the names the contract reads are listed.
var sqlalchemyReexports = map[string]bool{
	"BigInteger": true, "Boolean": true, "CheckConstraint": true, "Column": true, "Date": true, "DateTime": true,
	"Enum": true, "Float": true, "ForeignKey": true, "ForeignKeyConstraint": true, "Index": true, "Integer": true,
	"Interval": true, "JSON": true, "LargeBinary": true, "Numeric": true, "PrimaryKeyConstraint": true,
	"SmallInteger": true, "String": true, "Table": true, "Text": true, "Time": true, "TypeDecorator": true,
	"Unicode": true, "UnicodeText": true, "UniqueConstraint": true, "Uuid": true, "text": true,
}

// sqlalchemyModules are SQLAlchemy submodules whose type and schema names are
// the top-level ones (`sqlalchemy.types.String` is `sqlalchemy.String`).
var sqlalchemyModules = []string{"sqlalchemy.sql.sqltypes.", "sqlalchemy.sql.schema.", "sqlalchemy.types.", "sqlalchemy.schema.", "sqlalchemy.sql."}

// builtinNames are the builtins row L1 reads: the type names of F5 and R-B2,
// and the method decorators of K-dec (`classmethod`, `staticmethod`,
// `property`, Revision 3.3). Python resolves such a name to the builtin
// only when no binding in the module or an enclosing scope shadows it, so
// resolve checks that too.
var builtinNames = map[string]bool{"str": true, "int": true, "float": true, "bool": true, "bytes": true, "object": true, "dict": true, "list": true, "set": true, "tuple": true, "type": true, "frozenset": true,
	"classmethod": true, "staticmethod": true, "property": true}

// ref is the outcome of resolving one dotted name in a scope (row L1):
// exactly one of canonical (an external or builtin name such as
// "sqlmodel.Field" or "builtins.str") or class (a repository class binding)
// is set, or neither when the name does not resolve. binding keeps the
// resolver's answer so aliases (A5) can read a non-class binding.
type ref struct {
	canonical string
	class     *classKey
	binding   python.Binding
}

// classKey names a repository class binding: file, enclosing scope, name.
type classKey struct {
	file  string
	scope int
	name  string
}

func canonicalExternal(module, name string) string {
	full := module
	if name != "" {
		full += "." + name
	}
	if strings.HasPrefix(full, "sqlmodel.") {
		rest := strings.TrimPrefix(full, "sqlmodel.")
		if sqlalchemyReexports[rest] {
			return "sqlalchemy." + rest
		}
		if rest == "sql.sqltypes.AutoString" {
			return "sqlmodel.AutoString"
		}
	}
	for _, prefix := range sqlalchemyModules {
		if strings.HasPrefix(full, prefix) {
			rest := strings.TrimPrefix(full, prefix)
			if !strings.Contains(rest, ".") {
				return "sqlalchemy." + rest
			}
		}
	}
	if strings.HasPrefix(full, "typing_extensions.") {
		return "typing." + strings.TrimPrefix(full, "typing_extensions.")
	}
	if strings.HasPrefix(full, "pydantic.functional_validators.") || strings.HasPrefix(full, "pydantic.networks.") || strings.HasPrefix(full, "pydantic.fields.") {
		return "pydantic." + full[strings.LastIndex(full, ".")+1:]
	}
	return full
}

// resolve answers row L1 for a dotted name used in file at scope.
func (l *linker) resolve(file string, scope int, dotted string) ref {
	if dotted == "" {
		return ref{}
	}
	tag("L1")
	b := l.in.Resolver.Callable(file, scope, dotted)
	if b.External {
		if b.Name == "" {
			return ref{binding: b}
		}
		c := canonicalExternal(b.File, b.Name)
		if strings.HasPrefix(c, "builtins.") {
			// A builtin counts only when the name is unbound (L1): a
			// binding such as `classmethod = builtins.classmethod`
			// shadows it, so the name stays unresolved.
			return ref{binding: b}
		}
		return ref{canonical: c, binding: b}
	}
	if b.File != "" && b.Syntax.Value.Kind == "class_definition" {
		if b.Syntax.Count == 1 && b.Syntax.Stable {
			return ref{class: &classKey{b.File, b.Scope, b.Name}, binding: b}
		}
		return ref{binding: b}
	}
	if b.File == "" && b.Module == nil && !strings.Contains(dotted, ".") && builtinNames[dotted] && !l.bound(file, scope, dotted) {
		return ref{canonical: "builtins." + dotted}
	}
	return ref{binding: b}
}

// bound reports whether name has any binding in the scope chain, so an
// unresolved builtin spelling is never read as the builtin when a module
// rebinds it.
func (l *linker) bound(file string, scope int, name string) bool {
	p := l.in.Python[file].Python
	if p == nil {
		return true
	}
	for scope >= 0 && scope < len(p.Scopes) {
		if _, ok := p.Scopes[scope].Bindings[name]; ok {
			return true
		}
		if scope == 0 {
			break
		}
		scope = p.Scopes[scope].Parent
	}
	return false
}

// canon resolves an expression's dotted name (a name or a call's callee).
func (l *linker) canon(file string, scope int, e *facts.PythonModelExpr) string {
	if e == nil || e.Kind != "name" && e.Kind != "call" && e.Kind != "subscript" {
		return ""
	}
	return l.resolve(file, scope, e.Name).canonical
}

// aliasExpr reads row A5: a name bound to one unconditional module-level
// assignment whose value is a type expression, bound before the class that
// uses it (Rev 2, re-check). The value is re-read from the current source at
// the binding's offset with the type-expression reader below. It returns the
// expression and the alias module's file, or ok false (A3).
func (l *linker) aliasExpr(r ref, beforeLine int, useFile string) (facts.PythonModelExpr, string, bool) {
	b := r.binding
	if b.File == "" || b.External || b.Syntax.Import != nil || b.Syntax.Count != 1 || !b.Syntax.Stable || b.Scope != 0 {
		return facts.PythonModelExpr{}, "", false
	}
	switch b.Syntax.Value.Kind {
	case "subscript", "binary_operator", "identifier", "attribute":
	default:
		return facts.PythonModelExpr{}, "", false
	}
	src := l.in.Sources[b.File]
	off := b.Syntax.Value.Offset
	if off <= 0 || off >= len(src) {
		return facts.PythonModelExpr{}, "", false
	}
	if b.File == useFile && lineOf(src, off) >= beforeLine {
		return facts.PythonModelExpr{}, "", false
	}
	e, ok := readTypeExpr(string(src[off:]))
	if !ok {
		return facts.PythonModelExpr{}, "", false
	}
	return e, b.File, true
}

// callBinding reads a module-level name bound once to a call (for A1m's
// "a name bound to such a call") and returns the callee's dotted name.
func (l *linker) callBinding(r ref) (string, string, bool) {
	b := r.binding
	if b.File == "" || b.External || b.Syntax.Import != nil || b.Syntax.Count != 1 || !b.Syntax.Stable || b.Syntax.Value.Kind != "call" {
		return "", "", false
	}
	src := l.in.Sources[b.File]
	off := b.Syntax.Value.Offset
	if off <= 0 || off >= len(src) {
		return "", "", false
	}
	e, ok := readTypeExpr(string(src[off:]))
	if !ok || e.Kind != "call" {
		return "", "", false
	}
	return e.Name, b.File, true
}

func lineOf(src []byte, off int) int {
	line := 1
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return line
}

// readTypeExpr parses the type expression at the start of text, up to the
// end of its logical line: dotted names, subscripts, `|` unions, string and
// None literals, and calls (callee only, arguments skipped). Anything else
// fails, so an alias it cannot read stays A3.
func readTypeExpr(text string) (facts.PythonModelExpr, bool) {
	p := &typeReader{s: text}
	e, ok := p.union()
	if !ok {
		return e, false
	}
	p.space(false)
	if p.i < len(p.s) && p.s[p.i] != '\n' && p.s[p.i] != '#' && p.s[p.i] != ';' && p.s[p.i] != '\r' {
		return e, false
	}
	return e, true
}

type typeReader struct {
	s     string
	i     int
	depth int
}

func (p *typeReader) space(inBrackets bool) {
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ' ' || c == '\t' || (inBrackets || p.depth > 0) && (c == '\n' || c == '\r') {
			p.i++
			continue
		}
		if c == '\\' && p.i+1 < len(p.s) && p.s[p.i+1] == '\n' {
			p.i += 2
			continue
		}
		break
	}
}

func (p *typeReader) union() (facts.PythonModelExpr, bool) {
	first, ok := p.primary()
	if !ok {
		return first, false
	}
	parts := []facts.PythonModelExpr{first}
	for {
		p.space(false)
		if p.i < len(p.s) && p.s[p.i] == '|' {
			p.i++
			p.space(false)
			next, ok := p.primary()
			if !ok {
				return next, false
			}
			parts = append(parts, next)
			continue
		}
		break
	}
	if len(parts) == 1 {
		return first, true
	}
	var flat []facts.PythonModelExpr
	for _, x := range parts {
		if x.Kind == "union" {
			flat = append(flat, x.Args...)
		} else {
			flat = append(flat, x)
		}
	}
	return facts.PythonModelExpr{Kind: "union", Args: flat}, true
}

func isIdent(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || !first && c >= '0' && c <= '9'
}

func (p *typeReader) primary() (facts.PythonModelExpr, bool) {
	p.space(false)
	if p.i >= len(p.s) {
		return facts.PythonModelExpr{}, false
	}
	c := p.s[p.i]
	if c == '"' || c == '\'' {
		end := strings.IndexByte(p.s[p.i+1:], c)
		if end < 0 {
			return facts.PythonModelExpr{}, false
		}
		v := p.s[p.i+1 : p.i+1+end]
		p.i += end + 2
		return facts.PythonModelExpr{Kind: "literal", LitKind: "str", Literal: v}, true
	}
	if !isIdent(c, true) {
		return facts.PythonModelExpr{}, false
	}
	start := p.i
	for p.i < len(p.s) && (isIdent(p.s[p.i], false) || p.s[p.i] == '.') {
		p.i++
	}
	name := p.s[start:p.i]
	if name == "None" {
		return facts.PythonModelExpr{Kind: "literal", LitKind: "none", Literal: "None"}, true
	}
	if p.i < len(p.s) && p.s[p.i] == '[' {
		p.i++
		p.depth++
		var args []facts.PythonModelExpr
		for {
			p.space(true)
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				break
			}
			a, ok := p.union()
			if !ok {
				return a, false
			}
			args = append(args, a)
			p.space(true)
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				break
			}
			return facts.PythonModelExpr{}, false
		}
		p.depth--
		return facts.PythonModelExpr{Kind: "subscript", Name: name, Args: args}, true
	}
	if p.i < len(p.s) && p.s[p.i] == '(' {
		depth := 0
		for p.i < len(p.s) {
			switch p.s[p.i] {
			case '(':
				depth++
			case ')':
				depth--
			case '"', '\'':
				q := p.s[p.i]
				end := strings.IndexByte(p.s[p.i+1:], q)
				if end < 0 {
					return facts.PythonModelExpr{}, false
				}
				p.i += end + 1
			}
			p.i++
			if depth == 0 {
				return facts.PythonModelExpr{Kind: "call", Name: name}, true
			}
		}
		return facts.PythonModelExpr{}, false
	}
	return facts.PythonModelExpr{Kind: "name", Name: name}, true
}
