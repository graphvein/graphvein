package schema

import (
	"strings"
)

// Row O1t (T018a2 Revision 1, coverage step B2): a call SELECT f('<rel>')
// of a set-defined PL/pgSQL function whose whole body is
//
//	BEGIN EXECUTE format('<constant>', <its one parameter>); [RETURN;] END
//
// runs exactly the statement the constant becomes with the argument
// substituted for its one %s (or %I) specifier. When that statement is a
// CREATE TRIGGER, the call is analysed as that statement (row N1 with its
// preconditions), instead of as an opaque call (O1). Everything else stays
// O1: another body, another parameter type or count, another specifier, an
// overload of the name, an argument that is not one plain or quoted
// identifier, or a template forgotten because the function may have
// changed since (an opaque statement, ALTER or DROP FUNCTION, another
// CREATE FUNCTION of the name).
//
// Substitution follows Postgres: a regclass parameter reads the literal as
// a relation name (regclassin: an unquoted name is folded to lower case)
// and %s prints it as a quoted identifier when needed (regclassout); the
// pass prints it always quoted, which names the same relation. A text
// parameter with %I is quoted (quote_ident); with %s it is inserted as
// written. regclass with %I quotes the printed name again and names
// another relation, so it is not modelled.

// o1tTemplate is a recorded function body of the O1t form.
type o1tTemplate struct {
	regclass bool
	// before and after surround the one specifier in the format string;
	// quote is set by %I.
	before, after string
	quote         bool
}

// o1tFunctions tracks the set's CREATE FUNCTION statements by name for
// O1t: a name maps to its template, or to nil once it may mean anything
// else (an overload, a body of another form).
type o1tFunctions struct {
	templates map[string]*o1tTemplate
	// signature is the parameter type of the definition seen, to detect
	// overloads.
	signature map[string]string
}

// o1tDefinition reads CREATE [OR REPLACE] FUNCTION n (p regclass|text)
// RETURNS void AS $$body$$ LANGUAGE plpgsql (options in either order) and
// returns the function name and its template, or nil when the body is not
// of the O1t form. ok is false when the statement is not a CREATE
// FUNCTION whose name could be read.
func o1tDefinition(toks []token, src []byte) (name string, tmpl *o1tTemplate, sig string, ok bool) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create") {
		return "", nil, "", false
	}
	c.acceptKw("or", "replace")
	if !c.acceptKw("function") {
		return "", nil, "", false
	}
	q, err := c.name()
	if err != nil || q.schema != nil {
		return "", nil, "", false
	}
	name = q.name
	if !c.acceptPunct("(") {
		return name, nil, "?", true
	}
	param, err := c.ident()
	if err != nil {
		return name, nil, "?", true
	}
	var typ string
	switch {
	case c.acceptKw("regclass"):
		typ = "regclass"
	case c.acceptKw("text"):
		typ = "text"
	default:
		return name, nil, "?", true
	}
	if !c.acceptPunct(")") || !c.acceptKw("returns", "void") {
		return name, nil, typ, true
	}
	var body *token
	plpgsql := false
	for !c.done() {
		switch {
		case c.acceptKw("as"):
			if c.done() || c.toks[c.i].kind != tokString || c.src[c.toks[c.i].start] != '$' {
				return name, nil, typ, true
			}
			body = &c.toks[c.i]
			c.i++
		case c.acceptKw("language", "plpgsql"):
			plpgsql = true
		case c.isPunct(";") && c.i == len(c.toks)-1:
			c.i++
		default:
			return name, nil, typ, true
		}
	}
	if body == nil || !plpgsql {
		return name, nil, typ, true
	}
	return name, o1tBody(body.value, param, typ), typ, true
}

// o1tBody reads the closed body form; nil for anything else.
func o1tBody(text, param, typ string) *o1tTemplate {
	statements, failure := lexStatements([]byte(text))
	if failure != nil {
		return nil
	}
	var toks []token
	for _, st := range statements {
		toks = append(toks, st.tokens...)
		toks = append(toks, token{kind: tokPunct, value: ";"})
	}
	src := []byte(text)
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("begin", "execute", "format") || !c.acceptPunct("(") || c.done() {
		return nil
	}
	f, ok := literalValue(c.toks[c.i], src)
	if !ok || src[c.toks[c.i].start] != '\'' {
		return nil
	}
	c.i++
	if !c.acceptPunct(",") {
		return nil
	}
	arg, err := c.ident()
	if err != nil || arg != param || !c.acceptPunct(")") || !c.acceptPunct(";") {
		return nil
	}
	if c.acceptKw("return") && !c.acceptPunct(";") {
		return nil
	}
	if !c.acceptKw("end") {
		return nil
	}
	for c.acceptPunct(";") {
	}
	if !c.done() {
		return nil
	}
	// Exactly one %s or %I; %% is a literal percent sign (format()).
	tmpl := &o1tTemplate{regclass: typ == "regclass"}
	var b strings.Builder
	found := false
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			b.WriteByte(f[i])
			continue
		}
		if i+1 >= len(f) {
			return nil
		}
		i++
		switch f[i] {
		case '%':
			b.WriteByte('%')
		case 's', 'I':
			if found {
				return nil
			}
			found = true
			tmpl.before, tmpl.quote = b.String(), f[i] == 'I'
			b.Reset()
		default:
			return nil
		}
	}
	if !found || tmpl.regclass && tmpl.quote {
		return nil
	}
	tmpl.after = b.String()
	return tmpl
}

// track records one statement for O1t: a CREATE FUNCTION defines or
// forgets a template; an opaque statement, or any ALTER/DROP FUNCTION,
// forgets every template (the functions may have changed).
func (o *o1tFunctions) track(toks []token, src []byte, a analysis) {
	if o.templates == nil {
		o.templates, o.signature = map[string]*o1tTemplate{}, map[string]string{}
	}
	c := &cursor{toks: toks, src: src}
	switch {
	case c.peekKw("create", "function"), c.peekKw("create", "or", "replace", "function"):
		name, tmpl, sig, ok := o1tDefinition(toks, src)
		if !ok {
			o.forget()
			return
		}
		if prev, seen := o.signature[name]; seen && prev != sig || sig == "?" {
			// An overload (or a signature not read): the call may resolve
			// to another function.
			o.signature[name] = "?"
			o.templates[name] = nil
			return
		}
		o.signature[name] = sig
		o.templates[name] = tmpl
	case c.peekKw("alter", "function"), c.peekKw("drop", "function"), c.peekKw("alter", "routine"), c.peekKw("drop", "routine"),
		c.peekKw("create", "procedure"), c.peekKw("create", "or", "replace", "procedure"):
		o.forget()
	case a.class == classOpaque || a.class == classUnsupported || a.class == classError:
		o.forget()
	}
}

func (o *o1tFunctions) forget() {
	for k := range o.templates {
		o.templates[k] = nil
		o.signature[k] = "?"
	}
}

// call returns the statement a SELECT f('<rel>') of a template function
// runs, when it is a CREATE TRIGGER; ok is false otherwise.
func (o *o1tFunctions) call(toks []token, src []byte) (string, bool) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("select") || c.done() || c.toks[c.i].kind != tokIdent {
		return "", false
	}
	tmpl := o.templates[c.toks[c.i].value]
	c.i++
	if tmpl == nil || !c.acceptPunct("(") || c.done() {
		return "", false
	}
	arg, ok := literalValue(c.toks[c.i], src)
	if !ok {
		return "", false
	}
	c.i++
	if !c.acceptPunct(")") {
		return "", false
	}
	c.acceptPunct(";")
	if !c.done() {
		return "", false
	}
	var ident string
	switch {
	case tmpl.regclass || !tmpl.quote:
		// regclassin, or the text inserted as written: one plain or
		// quoted identifier.
		name, ok := plainIdentifier(arg)
		if !ok {
			return "", false
		}
		if tmpl.regclass {
			ident = quoteIdent(name)
		} else {
			ident = arg
		}
	default:
		// quote_ident keeps the text as the name: a text with a double
		// quote names a relation whose name contains quotes, which the
		// substituted statement would not read as written (Revision 1.9:
		// g5-s2274, judged since H21).
		if strings.Contains(arg, `"`) {
			return "", false
		}
		ident = quoteIdent(arg)
	}
	text := tmpl.before + ident + tmpl.after
	statements, failure := lexStatements([]byte(text))
	if failure != nil || len(statements) != 1 {
		return "", false
	}
	sc := &cursor{toks: statements[0].tokens, src: []byte(text)}
	if !sc.peekKw("create", "trigger") && !sc.peekKw("create", "or", "replace", "trigger") {
		return "", false
	}
	return text, true
}

// plainIdentifier reads s as one identifier the way regclassin does: an
// unquoted name folded to lower case, or a double-quoted name.
func plainIdentifier(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		if inner == "" || strings.Contains(strings.ReplaceAll(inner, `""`, ""), `"`) {
			return "", false
		}
		return strings.ReplaceAll(inner, `""`, `"`), true
	}
	if s == "" || !identStart(s[0]) {
		return "", false
	}
	for i := 1; i < len(s); i++ {
		if !identCont(s[i]) || s[i] >= 0x80 {
			return "", false
		}
	}
	if s[0] >= 0x80 {
		return "", false
	}
	return strings.ToLower(s), true
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
