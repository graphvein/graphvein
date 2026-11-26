package schema

import (
	"fmt"
	"slices"
	"strings"
)

// CREATE OR REPLACE of routines (T018a2 Revision 1.5 U6, class P3, row
// R6). A routine's identity is its schema, name and input argument types
// (functioncmds.c interpret_function_parameter_list: IN, INOUT and
// VARIADIC parameters; OUT and TABLE parameters are not part of it).
// ProcedureCreate (pg_proc.c REL_17_6 370-545) replaces a routine of the
// same identity only when the new definition keeps its kind (42809), its
// return type and set-ness (42P13), the row type its OUT parameters
// define, the name of every named input parameter, and at least as many
// parameter defaults of the same types (42P13). The pass proves these on
// the definitions it read; an identity it cannot decide may fail, and an
// identity no held routine has creates a new routine.

// routineDef is one routine definition the pass read.
type routineDef struct {
	schema *string
	name   string
	kind   string
	// inputs are the normalised input argument types; identityKnown is
	// set when every one is a pg_catalog type (only then can two
	// identities be told equal or different).
	inputs        []string
	identityKnown bool
	// ret and retSet are the result type; outCols the OUT, INOUT and
	// TABLE columns as "name type"; inNames the input parameter names in
	// order ("" for an unnamed one); defaults counts the parameter
	// defaults, polyDefault marks one on a polymorphic parameter.
	ret         string
	retSet      bool
	outCols     []string
	inNames     []string
	defaults    int
	polyDefault bool
	// defKnown is set when every part above was read.
	defKnown bool
	// pathUnknown is set when the name was unqualified after a search_path
	// change: its schema is not known.
	pathUnknown bool
	// paramsRead is set when every parameter of the header was read, so
	// inputs and inNames are complete (W2 binds call arguments to them).
	paramsRead bool
}

// catalogArgType normalises a written argument or result type: ok is false
// unless it is a pg_catalog type or an array of one.
func catalogArgType(written string) (string, bool) {
	base, ok := normalType(written)
	if !ok {
		return "", false
	}
	if isCatalogType(strings.TrimSuffix(base, "[]")) {
		return base, true
	}
	return base, false
}

// parseRoutine reads the header of CREATE [OR REPLACE] FUNCTION|PROCEDURE
// at c (after the kind word). It returns nil when the name cannot be read.
func parseRoutine(c *cursor, kind string) *routineDef {
	n, err := c.name()
	if err != nil {
		return nil
	}
	d := &routineDef{schema: n.schema, name: n.name, kind: kind, identityKnown: true, defKnown: true}
	if !c.isPunct("(") {
		d.identityKnown, d.defKnown = false, false
		return d
	}
	end, err := c.group()
	if err != nil {
		d.identityKnown, d.defKnown = false, false
		return d
	}
	var outTypes []string
	for _, r := range c.splitList(c.i+1, end-1) {
		if r[0] >= r[1] {
			continue
		}
		p, ok := routineParameter(c, r[0], r[1])
		if !ok {
			d.identityKnown, d.defKnown = false, false
			return d
		}
		typ, catalog := catalogArgType(p.typ)
		if p.mode != "out" {
			d.inputs = append(d.inputs, typ)
			d.inNames = append(d.inNames, p.name)
			if !catalog {
				d.identityKnown = false
			}
			if p.hasDefault {
				d.defaults++
				d.polyDefault = d.polyDefault || polymorphic(typ)
			}
		}
		if p.mode == "out" || p.mode == "inout" {
			if !catalog {
				d.defKnown = false
			}
			d.outCols = append(d.outCols, p.name+" "+typ)
			outTypes = append(outTypes, typ)
		}
	}
	d.paramsRead = true
	c.i = end
	switch {
	case kind == "procedure":
		d.ret = "void"
		if len(outTypes) > 0 {
			d.ret = "record"
		}
	case c.acceptKw("returns", "table"):
		d.ret, d.retSet, d.outCols = "record", true, nil
		if !c.isPunct("(") {
			d.defKnown = false
			break
		}
		e, err := c.group()
		if err != nil {
			d.defKnown = false
			break
		}
		for _, r := range c.splitList(c.i+1, e-1) {
			sub := &cursor{toks: c.toks[:r[1]], i: r[0], src: c.src}
			name, err := sub.ident()
			start := sub.i
			if err != nil || columnType(sub) != nil || !sub.done() {
				d.defKnown = false
				break
			}
			typ, catalog := catalogArgType(sub.text(start, sub.i))
			if !catalog {
				d.defKnown = false
			}
			d.outCols = append(d.outCols, name+" "+typ)
		}
	case c.acceptKw("returns"):
		d.retSet = c.acceptKw("setof")
		start := c.i
		if columnType(c) != nil {
			d.defKnown = false
			break
		}
		typ, catalog := catalogArgType(c.text(start, c.i))
		if !catalog {
			d.defKnown = false
		}
		d.ret = typ
	case len(outTypes) == 1:
		d.ret = outTypes[0]
	case len(outTypes) > 1:
		d.ret = "record"
	default:
		d.defKnown = false
	}
	if d.ret != "record" {
		d.outCols = nil
	}
	return d
}

// routineParam is one func_arg_with_default.
type routineParam struct {
	mode, name, typ string
	hasDefault      bool
}

// routineParameter reads one parameter between from and to: [mode] [name]
// [mode] type [DEFAULT e | = e] (gram.y func_arg).
func routineParameter(c *cursor, from, to int) (routineParam, bool) {
	var p routineParam
	end := to
	for j := from; j < to; j++ {
		t := c.toks[j]
		if t.kind == tokIdent && t.value == "default" || t.kind == tokOp && t.value == "=" {
			end, p.hasDefault = j, true
			break
		}
		if t.kind == tokPunct && t.value == "(" {
			sub := &cursor{toks: c.toks, i: j, src: c.src}
			if e, err := sub.group(); err == nil {
				j = e
			}
		}
	}
	modes := map[string]bool{"in": true, "out": true, "inout": true, "variadic": true}
	i := from
	mode := func() {
		if i < end && c.toks[i].kind == tokIdent && modes[c.toks[i].value] && i+1 < end {
			p.mode = c.toks[i].value
			i++
		}
	}
	mode()
	typeAt := func(k int) (string, bool) {
		sub := &cursor{toks: c.toks[:end], i: k, src: c.src}
		if columnType(sub) != nil || !sub.done() {
			return "", false
		}
		return c.text(k, end), true
	}
	if typ, ok := typeAt(i); ok {
		p.typ = typ
		return p, true
	}
	if i >= end || c.toks[i].kind != tokIdent && c.toks[i].kind != tokQuoted {
		return p, false
	}
	p.name = c.toks[i].value
	i++
	if p.mode == "" {
		mode()
	}
	typ, ok := typeAt(i)
	p.typ = typ
	return p, ok
}

// sameIdentity compares two definitions' identities: equal, different, or
// not decided (known false).
func sameIdentity(a, b *routineDef) (equal, known bool) {
	switch {
	case a.pathUnknown || b.pathUnknown:
		return false, false
	case a.schema != nil && b.schema != nil && *a.schema != *b.schema:
		return false, true
	case !sameSchema(a.schema, b.schema):
		return false, false
	case len(a.inputs) != len(b.inputs) && a.identityKnown && b.identityKnown:
		return false, true
	case !a.identityKnown || !b.identityKnown:
		return false, false
	}
	return slices.Equal(a.inputs, b.inputs), true
}

// replaceRoutine applies U6 to CREATE [OR REPLACE] FUNCTION|PROCEDURE and
// records the definition.
func (s *state) replaceRoutine(toks []token, i int, kind string, orReplace bool) {
	if s.routineDefs == nil {
		s.routineDefs, s.routineUnknown = map[string][]*routineDef{}, map[string]bool{}
	}
	d := parseRoutine(&cursor{toks: toks, i: i, src: s.m.source}, kind)
	if d == nil {
		return
	}
	d.pathUnknown = d.schema == nil && s.pathChanged
	held := s.routineDefs[d.name]
	what := fmt.Sprintf("CREATE OR REPLACE %s %s", strings.ToUpper(kind), d.name)
	if !orReplace {
		s.routineDefs[d.name] = append(held, d)
		return
	}
	if s.routineUnknown[d.name] {
		s.unknownFact(fmt.Sprintf("%s may be refused: a routine of this name was changed by a statement the pass did not read, so whether it is replaced, and with what, is not proven%s", what, precondition("R6.P3")))
		return
	}
	for k, h := range held {
		equal, known := sameIdentity(h, d)
		if !known {
			s.unknownFact(fmt.Sprintf("%s may be refused: whether it replaces a routine the set holds is not proven (argument types or schema not proven)%s", what, precondition("R6.P3")))
			s.routineUnknown[d.name] = true
			return
		}
		if !equal {
			continue
		}
		if why := replaceRefusal(h, d); why != "" {
			s.mayFail(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, why, precondition("R6.P3")))
			return
		}
		if !h.defKnown || !d.defKnown || h.polyDefault || d.polyDefault {
			s.unknownFact(fmt.Sprintf("%s may be refused: the held or the new definition's result type, OUT parameters or defaults are not proven%s", what, precondition("R6.P3")))
			s.routineUnknown[d.name] = true
			return
		}
		held[k] = d
		return
	}
	s.routineDefs[d.name] = append(held, d)
}

// replaceRefusal returns why ProcedureCreate refuses d over h of the same
// identity, judging only the parts both definitions prove.
func replaceRefusal(h, d *routineDef) string {
	if h.kind != d.kind {
		return fmt.Sprintf("cannot change routine kind: it is a %s (42809)", h.kind)
	}
	if h.defKnown && d.defKnown {
		if h.ret != d.ret || h.retSet != d.retSet {
			if d.kind == "procedure" {
				return "cannot change whether a procedure has output parameters (42P13)"
			}
			return "cannot change return type of existing function (42P13)"
		}
		if h.ret == "record" && !slices.Equal(h.outCols, d.outCols) {
			return "cannot change return type of existing function: the row type defined by OUT parameters is different (42P13)"
		}
	}
	for j, n := range h.inNames {
		if n == "" {
			continue
		}
		if j >= len(d.inNames) || d.inNames[j] != n {
			return fmt.Sprintf("cannot change name of input parameter %q (42P13)", n)
		}
	}
	if d.defaults < h.defaults {
		return "cannot remove parameter defaults from existing function (42P13)"
	}
	return ""
}

// forgetRoutines applies DROP and ALTER of routines to the definitions:
// a DROP of a name with an argument list the pass reads removes that
// identity, one without removes every definition of the name, and any
// other form (and RENAME TO, SET SCHEMA) leaves the names unknown.
func (s *state) forgetRoutines(toks []token) {
	if s.routineDefs == nil {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.acceptKw("drop", "function"), c.acceptKw("drop", "procedure"), c.acceptKw("drop", "routine"):
		c.acceptKw("if", "exists")
		for !c.done() {
			n, err := c.name()
			if err != nil {
				break
			}
			if !c.isPunct("(") {
				delete(s.routineDefs, n.name)
			} else {
				probe := append([]token{{kind: tokIdent, value: "x"}}, c.toks[c.i:]...)
				d := parseRoutine(&cursor{toks: probe, src: c.src}, "function")
				e, err := c.group()
				if err != nil || d == nil || !d.identityKnown {
					s.routineUnknown[n.name] = true
					break
				}
				c.i = e
				d.schema, d.name = n.schema, n.name
				d.pathUnknown = d.schema == nil && s.pathChanged
				kept := s.routineDefs[n.name][:0]
				for _, h := range s.routineDefs[n.name] {
					equal, known := sameIdentity(h, d)
					if !known {
						s.routineUnknown[n.name] = true
					}
					if !equal {
						kept = append(kept, h)
					}
				}
				s.routineDefs[n.name] = kept
			}
			if !c.acceptPunct(",") {
				break
			}
		}
	case c.acceptKw("alter", "function"), c.acceptKw("alter", "procedure"), c.acceptKw("alter", "routine"):
		n, err := c.name()
		if err != nil {
			return
		}
		for j := c.i; j+1 < len(toks); j++ {
			w := func(k int, v string) bool {
				return j+k < len(toks) && toks[j+k].kind == tokIdent && toks[j+k].value == v
			}
			if w(0, "rename") && w(1, "to") || w(0, "set") && w(1, "schema") {
				s.routineUnknown[n.name] = true
				if w(0, "rename") && j+2 < len(toks) {
					s.routineUnknown[toks[j+2].value] = true
				}
				return
			}
		}
	case c.acceptKw("drop", "schema"), c.acceptKw("drop", "owned"), c.acceptKw("drop", "extension"):
		for name := range s.routineDefs {
			s.routineUnknown[name] = true
		}
	}
}
