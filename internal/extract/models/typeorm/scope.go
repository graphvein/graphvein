package typeorm

import (
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file gives an options reader the scope of the function whose
// returns it reads (a forRootAsync useFactory, a registerAs factory, a
// selector function): its parameters and the names its body declares
// shadow the module-level names of the file, so an option value naming
// one of them is read from the function, never from the module
// (T018g6, follow-up OP2). A shadowing name is unknown unless it
// is a const local bound to a literal (string, number, boolean, null or a
// template without substitutions), which is read as that literal.

// localScope maps a function's local names to their literal value, nil
// for a name whose value is not read (a parameter, a let or var, a const
// bound to anything else, a destructured name).
type localScope map[string]*typescript.ModelExpr

// functionScope collects the parameters of fn and the names declared
// anywhere in its body (nested if, else, try, catch and finally blocks
// included: a block-scoped name is treated as shadowing the whole body,
// which can only make more values unknown).
func functionScope(fn *typescript.ModelExpr) localScope {
	s := localScope{}
	if fn == nil {
		return s
	}
	for _, p := range fn.Params {
		if p != "" {
			s[p] = nil
		}
	}
	s.declare(fn.Statements)
	return s
}

func (s localScope) declare(list []typescript.ModelStatement) {
	for i := range list {
		st := &list[i]
		switch st.Kind {
		case typescript.StatementDeclaration:
			if st.Name == "" {
				continue
			}
			var lit *typescript.ModelExpr
			if st.Keyword == "const" && literalExpr(st.Value) {
				lit = st.Value
			}
			if _, seen := s[st.Name]; seen {
				// Declared twice (in two blocks): its value is not one
				// literal.
				lit = nil
			}
			s[st.Name] = lit
		case typescript.StatementDestructure:
			for _, b := range st.Bindings {
				if b.Name != "" {
					s[b.Name] = nil
				}
			}
		}
		s.declare(st.Body)
		s.declare(st.Else)
		s.declare(st.Catch)
		s.declare(st.Finally)
	}
}

// literalExpr reports a literal value expression.
func literalExpr(e *typescript.ModelExpr) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case typescript.ModelString, typescript.ModelNumber, typescript.ModelBoolean, typescript.ModelNull:
		return true
	case typescript.ModelTemplate:
		return len(e.Substitutions) == 0
	}
	return false
}

// shadows reports a name the scope declares, and its literal value (nil
// when the name is unknown).
func (s localScope) shadows(name string) (*typescript.ModelExpr, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s[name]
	return v, ok
}

// substitute returns e with every identifier the scope shadows replaced by
// its literal value or, when the value is not read, by an unknown
// expression; e itself when it names no shadowed identifier. A nested
// arrow or function expression that names a shadowed identifier other
// than its own parameters becomes unknown as a whole.
func (s localScope) substitute(e *typescript.ModelExpr) *typescript.ModelExpr {
	if len(s) == 0 || e == nil || !s.mentions(e, nil) {
		return e
	}
	return s.copyExpr(e)
}

func unknownAt(e *typescript.ModelExpr) *typescript.ModelExpr {
	return &typescript.ModelExpr{Kind: typescript.ModelUnknown, Span: e.Span}
}

// mentions reports an identifier of e that the scope shadows and that is
// not bound by an enclosing arrow or function of e (bound).
func (s localScope) mentions(e *typescript.ModelExpr, bound map[string]bool) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		_, ok := s[e.Name]
		return ok && !bound[e.Name]
	case typescript.ModelArrow, typescript.ModelFunction:
		inner := map[string]bool{}
		for k := range bound {
			inner[k] = true
		}
		for _, p := range e.Params {
			inner[p] = true
		}
		if s.mentions(e.Body, inner) {
			return true
		}
		// A body's statements may name the shadowed identifier anywhere;
		// they are not walked, so any statement makes the function
		// unknown when the scope is not empty.
		return len(e.Statements) > 0
	}
	for _, c := range children(e) {
		if s.mentions(c, bound) {
			return true
		}
	}
	return false
}

// copyExpr copies e, replacing shadowed identifiers (substitute).
func (s localScope) copyExpr(e *typescript.ModelExpr) *typescript.ModelExpr {
	if e == nil || !s.mentions(e, nil) {
		return e
	}
	switch e.Kind {
	case typescript.ModelIdentifier:
		if v := s[e.Name]; v != nil {
			return v
		}
		return unknownAt(e)
	case typescript.ModelArrow, typescript.ModelFunction:
		return unknownAt(e)
	}
	out := *e
	out.Callee = s.copyExpr(e.Callee)
	out.Object = s.copyExpr(e.Object)
	out.Tag = s.copyExpr(e.Tag)
	out.Left = s.copyExpr(e.Left)
	out.Right = s.copyExpr(e.Right)
	out.Index = s.copyExpr(e.Index)
	out.Test = s.copyExpr(e.Test)
	out.Then = s.copyExpr(e.Then)
	out.Else = s.copyExpr(e.Else)
	out.Body = s.copyExpr(e.Body)
	out.Args = s.copyList(e.Args)
	out.Elements = s.copyList(e.Elements)
	out.Substitutions = s.copyList(e.Substitutions)
	if e.Entries != nil {
		out.Entries = make([]typescript.ModelEntry, len(e.Entries))
		for i, en := range e.Entries {
			en.Value = *s.copyExpr(&e.Entries[i].Value)
			out.Entries[i] = en
		}
	}
	return &out
}

func (s localScope) copyList(list []typescript.ModelExpr) []typescript.ModelExpr {
	if list == nil {
		return nil
	}
	out := make([]typescript.ModelExpr, len(list))
	for i := range list {
		out[i] = *s.copyExpr(&list[i])
	}
	return out
}

// children lists an expression's sub-expressions (not the statements of a
// function body).
func children(e *typescript.ModelExpr) []*typescript.ModelExpr {
	out := []*typescript.ModelExpr{e.Callee, e.Object, e.Tag, e.Left, e.Right, e.Index, e.Test, e.Then, e.Else, e.Body}
	for i := range e.Args {
		out = append(out, &e.Args[i])
	}
	for i := range e.Elements {
		out = append(out, &e.Elements[i])
	}
	for i := range e.Substitutions {
		out = append(out, &e.Substitutions[i])
	}
	for i := range e.Entries {
		out = append(out, &e.Entries[i].Value)
	}
	return out
}
