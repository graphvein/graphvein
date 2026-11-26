package sequelize

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads row SQ-A1's "helper member" form (T018g3 Revision 2, open
// point O4): an attribute value `H(args).member` or `H(args)` where H
// resolves to a module-level function whose body is a single `return` of
// an object literal (opencollective-api's
// `CustomDataTypes(DataTypes).currency`). The member (or the whole
// returned object) is read as the attribute it would be if written at the
// call site, with the helper's parameters substituted only by the call's
// own arguments: Sequelize's DataTypes namespace (used only as `p.X`) or a
// literal. Every other helper stays unknown.

// helperForm is the form tag of the helper member (row SQ-A1).
const helperForm = "SQ-A1/helper member"

// helperValue reports whether v calls a helper and, when it does, returns
// the substituted attribute value, or why the helper is not read. ok is
// false when v is no call of a function value (the value is then read as
// written).
func (s scope) helperValue(v *typescript.ModelExpr) (sub *typescript.ModelExpr, why string, ok bool) {
	call, member := v, ""
	if v.Kind == typescript.ModelMember && v.Object != nil {
		call, member = v.Object, v.Name
	}
	if call.Kind != typescript.ModelCall || call.Callee == nil || call.Callee.Kind != typescript.ModelIdentifier {
		return nil, "", false
	}
	fn := s.resolve(call.Callee)
	if fn.Expr == nil || (fn.Expr.Kind != typescript.ModelFunction && fn.Expr.Kind != typescript.ModelArrow) {
		return nil, "", false
	}
	name := call.Callee.Name
	returned, why := helperReturn(fn.Expr)
	if why != "" {
		return nil, fmt.Sprintf("the helper %s (%s) %s; the column is not read (row SQ-A1)", name, fn.File, why), true
	}
	params := map[string]*typescript.ModelExpr{}
	types := map[string]bool{}
	if len(call.Args) != len(fn.Expr.Params) {
		return nil, fmt.Sprintf("the helper %s takes %d parameters and the call passes %d arguments; the column is not read (row SQ-A1)", name, len(fn.Expr.Params), len(call.Args)), true
	}
	for i, p := range fn.Expr.Params {
		if p == "" || params[p] != nil {
			return nil, fmt.Sprintf("the helper %s has a parameter that is not a plain, distinct identifier; the column is not read (row SQ-A1)", name), true
		}
		arg := &call.Args[i]
		switch {
		case arg.Kind == typescript.ModelString || arg.Kind == typescript.ModelNumber || arg.Kind == typescript.ModelBoolean || arg.Kind == typescript.ModelNull:
		case s.isTypes(arg):
			types[p] = true
		default:
			return nil, fmt.Sprintf("the helper %s is called with an argument that is neither Sequelize's DataTypes nor a literal; the column is not read (row SQ-A1)", name), true
		}
		params[p] = arg
	}
	value := returned
	if member != "" {
		value = objectEntry(returned, member)
		if value == nil {
			return nil, fmt.Sprintf("the object the helper %s returns has no plain property %s that a later entry cannot override; the column is not read (row SQ-A1)", name, member), true
		}
	}
	if p := namespaceMisuse(value, types); p != "" {
		return nil, fmt.Sprintf("the helper %s uses its parameter %s other than as the DataTypes namespace; the column is not read (row SQ-A1)", name, p), true
	}
	out := substitute(value, params)
	return &out, "", true
}

// helperReturn returns the object literal a helper returns: a function
// whose body is a single `return <object>`, or an arrow with an object
// body or such a block.
func helperReturn(fn *typescript.ModelExpr) (*typescript.ModelExpr, string) {
	var returned *typescript.ModelExpr
	switch {
	case fn.Kind == typescript.ModelArrow && !fn.Block:
		returned = fn.Body
	default:
		if len(fn.Statements) != 1 || fn.Statements[0].Kind != typescript.StatementReturn {
			return nil, "has a body other than a single return statement"
		}
		returned = fn.Statements[0].Value
	}
	if returned == nil || returned.Kind != typescript.ModelObject {
		return nil, "does not return an object literal"
	}
	return returned, ""
}

// namespaceMisuse returns a DataTypes-bound parameter that the value uses
// other than as the object of a member expression (`p.X`), or "". Nested
// functions and arrows are not entered: their bodies are never read for a
// table fact (a function default is a client-side default).
func namespaceMisuse(e *typescript.ModelExpr, types map[string]bool) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		if types[e.Name] {
			return e.Name
		}
		return ""
	case typescript.ModelMember:
		if e.Object != nil && e.Object.Kind == typescript.ModelIdentifier && types[e.Object.Name] {
			return ""
		}
	case typescript.ModelArrow, typescript.ModelFunction:
		return ""
	}
	for _, c := range children(e) {
		if p := namespaceMisuse(c, types); p != "" {
			return p
		}
	}
	return ""
}

// substitute copies e with each parameter replaced by the call's argument
// and every other identifier but `undefined` replaced by an unknown
// expression: a name the helper's module binds is not resolved in the
// model's scope, so a fact that needs it stays unknown. Nested functions
// and arrows are kept as written.
func substitute(e *typescript.ModelExpr, params map[string]*typescript.ModelExpr) typescript.ModelExpr {
	if e == nil {
		return typescript.ModelExpr{Kind: typescript.ModelUnknown}
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		if arg := params[e.Name]; arg != nil {
			return *arg
		}
		if isUndefined(e) {
			return *e
		}
		return typescript.ModelExpr{Kind: typescript.ModelUnknown, Span: e.Span}
	case typescript.ModelArrow, typescript.ModelFunction:
		return *e
	}
	out := *e
	one := func(p *typescript.ModelExpr) *typescript.ModelExpr {
		if p == nil {
			return nil
		}
		c := substitute(p, params)
		return &c
	}
	list := func(l []typescript.ModelExpr) []typescript.ModelExpr {
		if l == nil {
			return nil
		}
		r := make([]typescript.ModelExpr, len(l))
		for i := range l {
			r[i] = substitute(&l[i], params)
		}
		return r
	}
	out.Callee, out.Object, out.Tag = one(e.Callee), one(e.Object), one(e.Tag)
	out.Left, out.Right, out.Index, out.Body = one(e.Left), one(e.Right), one(e.Index), one(e.Body)
	out.Args, out.Substitutions, out.Elements = list(e.Args), list(e.Substitutions), list(e.Elements)
	if e.Entries != nil {
		out.Entries = make([]typescript.ModelEntry, len(e.Entries))
		for i, en := range e.Entries {
			en.Value = substitute(&e.Entries[i].Value, params)
			out.Entries[i] = en
		}
	}
	return out
}

// children lists an expression's sub-expressions (not function bodies).
func children(e *typescript.ModelExpr) []*typescript.ModelExpr {
	var out []*typescript.ModelExpr
	for _, p := range []*typescript.ModelExpr{e.Callee, e.Object, e.Tag, e.Left, e.Right, e.Index, e.Body} {
		if p != nil {
			out = append(out, p)
		}
	}
	for _, l := range [][]typescript.ModelExpr{e.Args, e.Substitutions, e.Elements} {
		for i := range l {
			out = append(out, &l[i])
		}
	}
	for i := range e.Entries {
		out = append(out, &e.Entries[i].Value)
	}
	return out
}
