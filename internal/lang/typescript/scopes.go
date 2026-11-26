package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// The call binding model (T046, rows TSX-1 to TSX-26 of
// docs/lang/typescript-resolution.md). Link time resolves a call's callee
// name by qualified names: the enclosing named declarations outward, then
// the file's imports. Qualified names do not see anonymous functions,
// blocks, parameters or most local bindings, so the extractor builds the
// file's lexical scopes and checks, for every call, that the name's
// nearest lexical binding is the declaration link time would pick. When it
// is not, or when the syntax does not fix the binding, the call is marked
// Shadowed and stays unresolved: a missing edge, never a wrong one. When the
// binding is a single, never-written function declaration or import and
// link time would pick another declaration of the same name, the call
// carries its binding instead (Lexical, LexicalImport: rows TSX-22, TSX-23).

// bindingKind says what link time can do with a binding.
type bindingKind int

const (
	// bindOpaque is a binding no qualified name stands for (a parameter, a
	// plain variable, a catch parameter): a call through it resolves to
	// nothing.
	bindOpaque bindingKind = iota
	// bindNamed is a declaration with a qualified name: a function, a
	// function-valued variable, a class or an object-literal owner.
	bindNamed
	// bindImport is an import: link time resolves it through the module
	// resolver, never through the file's own declarations.
	bindImport
)

// binding is one name a scope binds.
type binding struct {
	kind bindingKind
	// q is the qualified name of a bindNamed declaration.
	q string
	// decl is the syntax node that declared the name: a second
	// declaration of the name in one scope by another node makes it
	// opaque (TSX-16).
	decl *parse.Node
	// row is the binding form's row, reported when the binding decides a
	// call.
	row string
	// pattern marks a name a destructuring pattern binds (row LF-3 of the
	// client matcher reads the same flag).
	pattern bool
	// constant marks a binding no write can change: a class's own name
	// inside the class, a named function expression's own name. Const
	// declarators and imports are constant too (writeChecks reads them
	// from the declaration).
	constant bool
	// written marks a binding some code of the file writes after its
	// declaration (TSX-19): a call through it may reach another value.
	written bool
}

// member is a method that is an own property of a class or of an
// object-literal owner: `C.m()`, `this.m()` and `o.m()` reach it.
type member struct {
	// owner is the declaration of the class or of the object owner's
	// variable declarator (the decl of its binding).
	owner *parse.Node
	// static marks a static class method: the class has it, its instances
	// do not.
	static bool
}

// thisKind says what `this` is in a scope that binds it.
type thisKind int

const (
	// thisNone: the scope does not bind `this` (arrow functions, blocks).
	thisNone thisKind = iota
	// thisMethod: a class method, accessor or constructor: `this` is the
	// instance (the class for a static member), whose members link time
	// names through the method's owner.
	thisMethod
	// thisOther: a plain function, an object-literal method, a class
	// field initializer or static block, the module: what `this` holds
	// depends on how the code is called, or is not a class member link
	// time names.
	thisOther
)

// lexScope is one lexical scope.
type lexScope struct {
	parent *lexScope
	// function marks the scopes var declarations land in: function bodies,
	// parameter lists, class static blocks and the module.
	function bool
	// uncertain marks a scope whose bindings the syntax does not fix: a
	// with statement's body, a function body or module that calls eval
	// directly (TSX-15).
	uncertain bool
	this      thisKind
	// method is the qualified name of the class method a thisMethod scope
	// belongs to, class its class's declaration and static whether `this`
	// is the class (a static method) or an instance.
	method string
	class  *parse.Node
	static bool
	// thisClass is the class `this` is (or is an instance of) in a
	// thisOther scope that belongs to a class: its body (field
	// initializers) and its static blocks; thisSide says which. Member
	// writes through `this` there reach the class (TSX-25).
	thisClass *parse.Node
	thisSide  side
	names     map[string]binding
}

func newScope(parent *lexScope, function bool) *lexScope {
	return &lexScope{parent: parent, function: function, names: map[string]binding{}}
}

// bind records name in s. A second declaration of the name by another node
// leaves it opaque: which one a call sees depends on evaluation order or
// on declaration merging the extractor does not model.
func (s *lexScope) bind(name string, b binding) {
	if name == "" {
		return
	}
	if old, ok := s.names[name]; ok && old.decl != b.decl {
		s.names[name] = binding{kind: bindOpaque, decl: old.decl, row: "TSX-16"}
		return
	}
	s.names[name] = b
}

// named upgrades the binding decl made of name in s to the named
// declaration q.
func (s *lexScope) named(name string, decl *parse.Node, q, row string) {
	if old, ok := s.names[name]; ok && old.decl == decl && old.row != "TSX-16" {
		s.names[name] = binding{kind: bindNamed, q: q, decl: decl, row: row, constant: old.constant}
	}
}

// varScope is the nearest scope var declarations land in.
func (s *lexScope) varScope() *lexScope {
	for s.parent != nil && !s.function {
		s = s.parent
	}
	return s
}

// bindPattern binds every name a declaration, parameter or catch pattern
// holds (boundNames): a plain identifier under row, a name inside a
// destructuring pattern under TSX-4.
func (s *lexScope) bindPattern(n *parse.Node, decl *parse.Node, row string) {
	// A default (`a = 1`) or rest (`...a`) around a plain name is no
	// destructuring.
	for n != nil && (n.Kind == "assignment_pattern" || n.Kind == "rest_pattern") {
		if n.Kind == "assignment_pattern" {
			n = n.Fields["left"]
		} else if len(n.Children) == 1 {
			n = n.Children[0]
		} else {
			break
		}
	}
	if n == nil {
		return
	}
	if n.Kind == "identifier" {
		s.bind(n.Text, binding{kind: bindOpaque, decl: decl, row: row})
		return
	}
	for _, name := range boundNames(n) {
		s.bind(name, binding{kind: bindOpaque, decl: decl, row: "TSX-4", pattern: true})
	}
}

// functionScope opens the scope of function n's parameters and binds them
// (TSX-5; destructured names TSX-4). Default values are read in this
// scope: they see the parameters and the enclosing scopes, never the
// body's declarations, which the body's own scope holds.
func functionScope(parent *lexScope, n *parse.Node, this thisKind) *lexScope {
	s := newScope(parent, true)
	s.this = this
	params := n.Fields["parameters"]
	if params == nil {
		// An arrow function's single unparenthesised parameter.
		params = n.Fields["parameter"]
	}
	if params == nil {
		return s
	}
	if params.Kind != "formal_parameters" {
		s.bindPattern(params, n, "TSX-5")
		return s
	}
	for _, p := range params.Children {
		switch p.Kind {
		case "required_parameter", "optional_parameter":
			s.bindPattern(p.Fields["pattern"], n, "TSX-5")
		default:
			s.bindPattern(p, n, "TSX-5")
		}
	}
	return s
}

// callSite is a call waiting for the binding check.
type callSite struct {
	index int
	scope *lexScope
}

// writeSite is a write of a name: an assignment, compound or logical
// assignment, ++/--, a destructuring assignment or a for-in/of target.
type writeSite struct {
	name  string
	scope *lexScope
}

// markWrites marks every binding a write of the file reaches as written
// (TSX-19). A direct eval may assign any binding it sees, so every binding
// visible from an eval call is written too. Bindings no write can change
// (constant, an import, a const declarator in constDecls) stay as they are.
func markWrites(writes []writeSite, evals []*lexScope, constDecls map[*parse.Node]bool) {
	mark := func(s *lexScope, name string) {
		b := s.names[name]
		if b.constant || b.kind == bindImport || constDecls[b.decl] {
			return
		}
		b.written = true
		s.names[name] = b
	}
	for _, w := range writes {
		for s := w.scope; s != nil; s = s.parent {
			if _, ok := s.names[w.name]; ok {
				mark(s, w.name)
				break
			}
		}
	}
	for _, e := range evals {
		for s := e; s != nil; s = s.parent {
			for name := range s.names {
				mark(s, name)
			}
		}
	}
}

// fileSymbols are the file's function and method qualified names as link
// time reads them: symbols all of them, methods the methods, repeated those
// whose qualified name a second declaration repeats (ADR 0011).
type fileSymbols struct {
	symbols, methods, repeated map[string]bool
}

// checkCalls marks every call whose name link time would resolve to
// another declaration than the call's lexical binding, or whose binding
// the syntax does not fix, as Shadowed; a call whose binding is a
// never-written function or an import gets that binding instead (TSX-22,
// TSX-23). symbols are the file's function and method qualified names, as
// link time reads them; members the methods that are own properties of a
// class or object owner, by qualified name; written the member writes of
// the file by owner (TSX-25) and through receivers of unknown owner
// (TSX-26).
func checkCalls(calls []facts.Call, sites []callSite, file fileSymbols, members map[string]member, written memberWrites) {
	symbols := file.symbols
	for _, site := range sites {
		call := &calls[site.index]
		if call.Name == "" {
			continue
		}
		base, rest, _ := strings.Cut(call.Name, ".")
		linked := linkSymbol(symbols, call.Scope, call.Name)
		row, ok := decideCall(call, site.scope, base, rest, linked, file, members, written)
		if !ok {
			call.Shadowed = true
		}
		if row == "TSX-25" || row == "TSX-26" {
			call.MemberWritten = true
		}
		bindingTag(BindingDecision{Row: row, Source: call.Source, Name: call.Name, Span: call.Span, Resolved: ok, Declared: linked != ""})
	}
}

// decideCall is the binding row that decides one call and whether the call
// stays resolvable (false: Shadowed). It sets the call's Lexical or
// LexicalImport binding for rows TSX-22 and TSX-23. A member call through
// an import is TSX-26 when the file writes a key of its path through a
// receiver of unknown owner: the import may be that receiver.
func decideCall(call *facts.Call, scope *lexScope, base, rest, linked string, file fileSymbols, members map[string]member, written memberWrites) (string, bool) {
	if base == "this" {
		return thisResolves(scope, call.Scope, rest, file.symbols, members, written)
	}
	var found *binding
	uncertain := false
	for s := scope; s != nil; s = s.parent {
		// A with object or a direct eval may add a binding here.
		uncertain = uncertain || s.uncertain
		if b, ok := s.names[base]; ok {
			found = &b
			break
		}
	}
	switch {
	case uncertain:
		return "TSX-15", false
	case found == nil:
		// A name no scope of the file binds is a global or an undeclared
		// name: no declaration of the file stands for it.
		return "TSX-17", linked == ""
	case found.kind == bindImport:
		if rest != "" && written.unknownBlocks(rest) {
			// The import is a receiver of unknown owner: a write of a
			// key on the call's path, through it or through another such
			// receiver of the file, may replace the member (TSX-26).
			return "TSX-26", false
		}
		if linked == "" {
			return found.row, true
		}
		// A declaration of the file has the name, but the call evaluates
		// the import.
		call.LexicalImport = true
		return "TSX-23", true
	case found.kind == bindNamed && found.written:
		return "TSX-19", false
	case found.kind == bindNamed && rest == "":
		want := found.q
		switch {
		case found.row != "TSX-3" && found.row != "TSX-8" && found.row != "TSX-18":
			// A class or an object is not callable.
			return found.row, false
		case linked == want:
			return found.row, true
		case file.repeated[want] && !file.methods[linked]:
			// The binding's qualified name is repeated and link time would
			// pick a function of the name, not a nearer method: the
			// repeated rule of ADR 0011 keeps it unresolved.
			return "TSX-24", false
		}
		// Link time would pick another declaration of the name (a nearer
		// method, or a function of another scope): the call evaluates the
		// binding's own declaration.
		call.Lexical = want
		return "TSX-22", true
	case found.kind == bindNamed:
		return memberResolves(found, rest, linked, members, written)
	}
	if found.pattern {
		patternShadowTag("LF-3")
	}
	return found.row, false
}

// memberResolves reports whether `x.rest()`, x bound to the named
// declaration b, reaches the method link time picks (linked): it must be
// an own property of b's declaration (a class or object owner; a
// function's nested declarations are not its properties), for a class a
// static method (TSX-7, TSX-9; otherwise TSX-20 or TSX-21), and no code of
// the file may write the member (TSX-25), nor a key of its path through a
// receiver of unknown owner (TSX-26).
func memberResolves(b *binding, rest, linked string, members map[string]member, written memberWrites) (string, bool) {
	m, isMember := members[linked]
	switch {
	case linked != b.q+"."+rest || !isMember || m.owner != b.decl || (b.row != "TSX-7" && b.row != "TSX-9"):
		return "TSX-21", false
	case b.row == "TSX-7" && !m.static:
		// Instance methods live on the prototype, not on the class.
		return "TSX-20", false
	case written.blocks(b.decl, rest, b.row == "TSX-7"):
		return "TSX-25", false
	case written.unknownBlocks(rest):
		return "TSX-26", false
	}
	return b.row, true
}

// thisResolves reports a `this.rest` call whose `this` is the class (in a
// static method) or an instance (in any other method) of the class of the
// method link time reads from the call's scope, and whose target is an own
// method of that class of the matching staticness (TSX-13); `this` outside
// a class method is TSX-14, a member of the other staticness TSX-20, a
// member the file writes TSX-25, a key of its path written through a
// receiver of unknown owner TSX-26, anything else link time would pick
// TSX-21.
func thisResolves(s *lexScope, scope, rest string, symbols map[string]bool, members map[string]member, written memberWrites) (string, bool) {
	for ; s != nil; s = s.parent {
		if s.uncertain || s.this == thisOther {
			break
		}
		if s.this == thisMethod {
			if s.method != scope {
				break
			}
			i := strings.LastIndex(scope, ".")
			if i < 0 {
				break
			}
			target := scope[:i] + "." + rest
			m, isMember := members[target]
			switch {
			case !symbols[target]:
				// Link time finds no member: no edge either way.
				return "TSX-13", true
			case !isMember || m.owner != s.class:
				return "TSX-21", false
			case m.static != s.static:
				return "TSX-20", false
			case written.blocks(s.class, rest, s.static):
				return "TSX-25", false
			case written.unknownBlocks(rest):
				return "TSX-26", false
			}
			return "TSX-13", true
		}
	}
	return "TSX-14", false
}

// side is what a member write of a class reaches: its instances (and the
// prototype they share), the class itself (its static members), or, when
// the syntax does not tell, both. An object owner's writes are both.
type side int

const (
	sideInstance side = 1 << iota
	sideClass
	sideBoth = sideInstance | sideClass
)

// memberWrite is a write of a property of the object `this` or a name
// holds (TSX-25): an assignment, compound or logical assignment, ++/--, a
// destructuring assignment target, a for-in/of target, a delete, or an
// Object/Reflect call that defines, assigns or deletes properties.
type memberWrite struct {
	// base is the receiver: `this` or an identifier.
	base *parse.Node
	// path is the chain of static keys from the receiver to the written
	// property (`this.a.b = x`: a, b).
	path []string
	// anyKey marks a write whose key under path is not a static name (a
	// computed key, Object.assign): it may write every property there.
	anyKey bool
	// depth is the number of keys from the receiver to the written
	// property, computed ones included (`this.a[k].b = x`: 3): with one
	// key the written object is the receiver itself, with more it is a
	// value the receiver holds, whose owner the model does not know.
	depth int
	// last is the written property's static name, or "" when its key is
	// computed or unknown.
	last  string
	scope *lexScope
	// side is set once the receiver is resolved.
	side side
}

// alias is the value a plain variable declarator binds, when it is `this`,
// another name, `new C(...)` or a class expression: a member write through
// the variable writes that object (TSX-25); or when it is a fresh literal
// or function, which no owner is.
type alias struct {
	value *parse.Node
	scope *lexScope
}

// memberWrites are the member writes of a file. owners holds them by the
// owner they reach: the declaration of a class (its instances and the
// class itself) or of an object owner's declarator. A write whose written
// object has no owner the model knows (TSX-26) may reach any owner: it
// leaves its property name in unknown, or sets unknownAny when that name
// is not static.
type memberWrites struct {
	owners     map[*parse.Node][]memberWrite
	unknown    map[string]bool
	unknownAny bool
}

// blocks reports whether a write of owner reaches the member path rest
// (`a` or `a.b`) of a static member (static) or an instance member or
// object property: a write of the member itself or of a property on its
// key path, or a write of an unknown key above it, on that side.
func (w memberWrites) blocks(owner *parse.Node, rest string, static bool) bool {
	want := sideInstance
	if static {
		want = sideClass
	}
	for _, mw := range w.owners[owner] {
		if mw.side&want == 0 {
			continue
		}
		p := strings.Join(mw.path, ".")
		under := p == "" || strings.HasPrefix(rest, p+".")
		if under || (!mw.anyKey && rest == p) {
			return true
		}
	}
	return false
}

// unknownBlocks reports whether a write through a receiver of unknown
// owner (TSX-26) may replace a member on the path rest (`a` or `a.b`): it
// wrote a property named like one of the path's keys, or a property whose
// name is not static.
func (w memberWrites) unknownBlocks(rest string) bool {
	if w.unknownAny {
		return true
	}
	for _, key := range strings.Split(rest, ".") {
		if w.unknown[key] {
			return true
		}
	}
	return false
}

// resolveMemberWrites maps every member write to the owner its receiver
// holds: `this` in a class method, static block or field initializer is
// the class (static) or an instance of it; a name is the class or object
// owner it binds, or what a plain declarator binding it holds (`this`,
// another name, `new C(...)`, a class expression). A write of a class's
// `prototype` writes the instances' methods, a write of an instance's
// `constructor` the class. A write whose written object is not such a
// receiver itself (a receiver of unknown owner, or a value reached through
// a key of any receiver: `this.a.m = x`) is a write of unknown owner
// (TSX-26); one whose receiver is a fresh literal or a function, which no
// owner is, reaches nothing.
func resolveMemberWrites(writes []memberWrite, aliases map[*parse.Node]alias) memberWrites {
	out := memberWrites{owners: map[*parse.Node][]memberWrite{}, unknown: map[string]bool{}}
	for _, w := range writes {
		owner, sd, known := receiverOwner(w.base, w.scope, aliases, 0)
		if owner != nil {
			w.side = sd
			if len(w.path) > 0 {
				switch {
				case sd == sideClass && w.path[0] == "prototype":
					w.path, w.side, w.depth = w.path[1:], sideInstance, w.depth-1
				case sd == sideInstance && w.path[0] == "constructor":
					w.path, w.side, w.depth = w.path[1:], sideClass, w.depth-1
				}
			}
		}
		if !known || w.depth != 1 {
			if w.last == "" {
				out.unknownAny = true
			} else {
				out.unknown[w.last] = true
			}
		}
		if owner == nil {
			continue
		}
		if len(w.path) == 0 && !w.anyKey {
			// `C.prototype = ...` replaces every instance method.
			w.anyKey = true
		}
		out.owners[owner] = append(out.owners[owner], w)
	}
	return out
}

// receiverOwner is the class or object owner declaration the receiver n
// holds at scope, and which side of it (an object owner: sideBoth). known
// is false when the model cannot tell what n holds: `this` outside a class,
// a parameter, a destructured name, an import, a global, a written name, a
// call result. A receiver known to hold no owner (a fresh literal, a
// function) gives a nil owner and known true.
func receiverOwner(n *parse.Node, scope *lexScope, aliases map[*parse.Node]alias, depth int) (*parse.Node, side, bool) {
	n = unwrapExpression(n)
	if n == nil || depth > 8 {
		return nil, 0, false
	}
	switch n.Kind {
	case "this":
		for s := scope; s != nil; s = s.parent {
			switch s.this {
			case thisNone:
				continue
			case thisMethod:
				if s.static {
					return s.class, sideClass, true
				}
				return s.class, sideInstance, true
			}
			return s.thisClass, s.thisSide, s.thisClass != nil
		}
	case "identifier":
		for s := scope; s != nil; s = s.parent {
			b, ok := s.names[n.Text]
			if !ok {
				continue
			}
			switch {
			case b.written:
				// The name may hold any value written to it.
			case b.kind == bindNamed && b.row == "TSX-7":
				return b.decl, sideClass, true
			case b.kind == bindNamed && b.row == "TSX-9":
				return b.decl, sideBoth, true
			case b.kind == bindNamed:
				// A function: its properties are no owner's members
				// (TSX-21).
				return nil, 0, true
			case b.kind == bindOpaque && !b.pattern:
				if a, ok := aliases[b.decl]; ok {
					return receiverOwner(a.value, a.scope, aliases, depth+1)
				}
			}
			return nil, 0, false
		}
	case "new_expression":
		if owner, sd, _ := receiverOwner(n.Fields["constructor"], scope, aliases, depth+1); sd == sideClass {
			return owner, sideInstance, true
		}
	case "class":
		// A class expression: the class itself.
		return n, sideClass, true
	case "array", "object", "string", "number", "template_string", "regex", "true", "false", "null", "arrow_function", "function_expression", "function", "generator_function":
		// A fresh value no owner is.
		return nil, 0, true
	}
	return nil, 0, false
}

// unwrapExpression strips parentheses, non-null assertions and type
// assertions, which do not change the value.
func unwrapExpression(n *parse.Node) *parse.Node {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "non_null_expression", "as_expression", "satisfies_expression", "type_assertion":
			if len(n.Children) == 0 {
				return nil
			}
			if n.Kind == "type_assertion" {
				n = n.Children[len(n.Children)-1]
			} else {
				n = n.Children[0]
			}
		default:
			return n
		}
	}
	return nil
}

// memberTargets appends the member writes of the write target t: a member
// or subscript expression, or the member leaves of a destructuring
// pattern.
func memberTargets(t *parse.Node, scope *lexScope, out []memberWrite) []memberWrite {
	t = unwrapExpression(t)
	if t == nil {
		return out
	}
	switch t.Kind {
	case "member_expression", "subscript_expression":
		if w, ok := memberTarget(t, scope); ok {
			out = append(out, w)
		}
	case "pair_pattern":
		out = memberTargets(t.Fields["value"], scope, out)
	case "assignment_pattern", "object_assignment_pattern":
		out = memberTargets(t.Fields["left"], scope, out)
	case "array_pattern", "object_pattern", "rest_pattern", "array", "object":
		for _, c := range t.Children {
			out = memberTargets(c, scope, out)
		}
	}
	return out
}

// memberTarget reads `base.a.b`, `base["a"]` or `base[k]` as a member write
// of base along its static key path; a non-static key ends the path and
// marks the write anyKey. base is `this`, a name or any other expression
// (a call result, `super`, a literal), whose owner receiverOwner decides.
func memberTarget(t *parse.Node, scope *lexScope) (memberWrite, bool) {
	var keys []string
	for {
		t = unwrapExpression(t)
		if t == nil {
			return memberWrite{}, false
		}
		switch t.Kind {
		case "member_expression":
			keys = append(keys, propertyName(t.Fields["property"]))
			t = t.Fields["object"]
			continue
		case "subscript_expression":
			name := ""
			if i := unwrapExpression(t.Fields["index"]); i != nil && (i.Kind == "string" || i.Kind == "number") {
				name = propertyName(i)
			}
			keys = append(keys, name)
			t = t.Fields["object"]
			continue
		}
		break
	}
	w := memberWrite{base: t, depth: len(keys), scope: scope}
	if len(keys) > 0 {
		w.last = keys[0]
	}
	// keys run from the written property back to the receiver.
	for i := len(keys) - 1; i >= 0; i-- {
		if keys[i] == "" {
			w.anyKey = true
			break
		}
		w.path = append(w.path, keys[i])
	}
	return w, true
}

// objectWrites reads `Object.defineProperty(x, "m", ...)`,
// `Object.defineProperties(x, ...)`, `Object.assign(x, ...)`,
// `Reflect.defineProperty(x, "m", ...)`, `Reflect.set(x, "m", ...)` and
// `Reflect.deleteProperty(x, "m")` as member writes of x: with a literal
// key that member, and `Object.assign` whose sources are object literals
// of static keys (assignedKeys) exactly those keys; otherwise any member.
func objectWrites(call *parse.Node, scope *lexScope) []memberWrite {
	callee := unwrapExpression(call.Fields["function"])
	if callee == nil || callee.Kind != "member_expression" {
		return nil
	}
	object := unwrapExpression(callee.Fields["object"])
	if object == nil || object.Kind != "identifier" {
		return nil
	}
	keyed, assign := false, false
	switch object.Text + "." + text(callee.Fields["property"]) {
	case "Object.defineProperty", "Reflect.defineProperty", "Reflect.set", "Reflect.deleteProperty":
		keyed = true
	case "Object.assign":
		assign = true
	case "Object.defineProperties":
	default:
		return nil
	}
	args := call.Fields["arguments"]
	if args == nil || len(args.Children) == 0 {
		return nil
	}
	w, ok := memberTarget(args.Children[0], scope)
	if !ok {
		return nil
	}
	// The written object is x: its properties are one key deeper.
	w.depth++
	w.last = ""
	if w.anyKey {
		return []memberWrite{w}
	}
	if assign {
		if keys, ok := assignedKeys(args.Children[1:]); ok {
			out := make([]memberWrite, 0, len(keys))
			for _, k := range keys {
				kw := w
				kw.path = append(append([]string(nil), w.path...), k)
				kw.last = k
				out = append(out, kw)
			}
			return out
		}
	}
	key := ""
	if keyed && len(args.Children) > 1 {
		if k := unwrapExpression(args.Children[1]); k != nil && (k.Kind == "string" || k.Kind == "number") {
			key = propertyName(k)
		}
	}
	if key == "" {
		w.anyKey = true
	} else {
		w.path = append(w.path, key)
		w.last = key
	}
	return []memberWrite{w}
}

// assignedKeys is the keys `Object.assign(x, ...sources)` writes when
// there is at least one source and every source is an object literal
// whose entries all have a static key: a shorthand property `a`, a pair
// keyed by an identifier or a string literal (`b: v`, `"b": v`), or a
// pair under a computed string literal (`["b"]: v`). Any other source
// (a name, a call, a spread argument) or entry (a spread, a method, an
// accessor, a numeric or computed non-literal key) gives ok false: the
// call may then write any member (TSX-25). A `__proto__` key gives ok
// false too, since copying it may replace the target's prototype.
func assignedKeys(sources []*parse.Node) (keys []string, ok bool) {
	seen := 0
	for _, src := range sources {
		if src.Kind == "comment" {
			continue
		}
		seen++
		src = unwrapExpression(src)
		if src == nil || src.Kind != "object" {
			return nil, false
		}
		for _, e := range src.Children {
			name := ""
			switch e.Kind {
			case "comment":
				continue
			case "shorthand_property_identifier":
				name = e.Text
			case "pair":
				name = staticKey(e.Fields["key"])
			}
			if name == "" || name == "__proto__" {
				return nil, false
			}
			keys = append(keys, name)
		}
	}
	return keys, seen > 0
}

// staticKey is the name an object literal pair's key gives when it is an
// identifier, a string literal or a computed string literal; otherwise "".
func staticKey(key *parse.Node) string {
	if key == nil {
		return ""
	}
	if key.Kind == "computed_property_name" {
		if len(key.Children) != 1 || key.Children[0].Kind != "string" {
			return ""
		}
		key = key.Children[0]
	}
	switch key.Kind {
	case "property_identifier":
		return key.Text
	case "string":
		if name, ok := jsString(key.Text); ok {
			return name
		}
	}
	return ""
}

// selfBind reports `this.m = this.m.bind(this, ...)` and
// `C.m = C.m.bind(C, ...)`: the member keeps a function that calls the
// method it replaces, so it is no write of TSX-25.
func selfBind(n *parse.Node) bool {
	if n.Kind != "assignment_expression" {
		return false
	}
	left := unwrapExpression(n.Fields["left"])
	right := unwrapExpression(n.Fields["right"])
	if left == nil || right == nil || left.Kind != "member_expression" || right.Kind != "call_expression" {
		return false
	}
	receiver := unwrapExpression(left.Fields["object"])
	if receiver == nil || (receiver.Kind != "this" && receiver.Kind != "identifier") || left.Fields["property"] == nil || left.Fields["property"].Kind != "property_identifier" {
		return false
	}
	callee := unwrapExpression(right.Fields["function"])
	if callee == nil || callee.Kind != "member_expression" || text(callee.Fields["property"]) != "bind" {
		return false
	}
	bound := unwrapExpression(callee.Fields["object"])
	if bound == nil || bound.Kind != "member_expression" || text(bound.Fields["property"]) != text(left.Fields["property"]) {
		return false
	}
	if o := unwrapExpression(bound.Fields["object"]); o == nil || o.Kind != receiver.Kind || o.Text != receiver.Text {
		return false
	}
	args := right.Fields["arguments"]
	if args == nil || len(args.Children) == 0 {
		return false
	}
	first := unwrapExpression(args.Children[0])
	return first != nil && first.Kind == receiver.Kind && first.Text == receiver.Text
}

// linkSymbol is the qualified name link time resolves name to through the
// file's declarations from scope outward (LinkWithResolver), or "".
func linkSymbol(symbols map[string]bool, scope, name string) string {
	for {
		if q := join(scope, name); symbols[q] {
			return q
		}
		if scope == "" {
			return ""
		}
		if i := strings.LastIndex(scope, "."); i >= 0 {
			scope = scope[:i]
		} else {
			scope = ""
		}
	}
}

// BindingRows are the rows of the call binding model, in document order
// (docs/lang/typescript-resolution.md, "Call binding model"); a contract
// test keeps them 1:1 with the table and with the branches that report
// them.
var BindingRows = []string{"TSX-1", "TSX-2", "TSX-3", "TSX-4", "TSX-5", "TSX-6", "TSX-7", "TSX-8", "TSX-9", "TSX-10", "TSX-11", "TSX-12", "TSX-13", "TSX-14", "TSX-15", "TSX-16", "TSX-17", "TSX-18", "TSX-19", "TSX-20", "TSX-21", "TSX-22", "TSX-23", "TSX-24", "TSX-25", "TSX-26"}

// BindingDecision is what the call binding model decided for one call:
// the row, and whether the call stays resolvable. Tests read it through
// SetBindingTagHook to check each row's outcome per call.
type BindingDecision struct {
	Row string
	// Source is the caller's identity, Name the callee as written.
	Source, Name string
	Span         contract.Span
	// Resolved is false when the call is marked Shadowed (unresolved).
	Resolved bool
	// Declared reports that link time's qualified-name lookup finds a
	// declaration of the file for the callee (row TSX-17's condition).
	Declared bool
}

// bindingTagHook is a test-only hook: every call the binding model decides
// reports its decision. It is nil in production.
var bindingTagHook func(BindingDecision)

// SetBindingTagHook installs (or, with nil, removes) the hook the call
// binding decisions report to. Tests only.
func SetBindingTagHook(hook func(BindingDecision)) { bindingTagHook = hook }

func bindingTag(d BindingDecision) {
	if bindingTagHook != nil {
		bindingTagHook(d)
	}
}

// ownMethods records the methods of class body that are own properties of
// the class (static) or of its instances: plain methods with a static name,
// no accessor, no second method or field of the same name and staticness
// (a field initializer would replace the method on the instance or class).
// Every method of the body, accessors and constructors included, goes to
// methodOf: what `this` is in it.
func ownMethods(body, class *parse.Node, memberOf, methodOf map[*parse.Node]member) {
	if body == nil {
		return
	}
	type key struct {
		name   string
		static bool
	}
	keys := map[key]int{}
	for _, c := range body.Children {
		switch c.Kind {
		case "method_definition", "public_field_definition", "field_definition":
			name := propertyName(c.Fields["name"]) + propertyName(c.Fields["property"])
			keys[key{name, hasToken(c, "static")}]++
		}
	}
	for _, c := range body.Children {
		if c.Kind != "method_definition" {
			continue
		}
		static := hasToken(c, "static")
		methodOf[c] = member{owner: class, static: static}
		if accessor(c) {
			continue
		}
		name := propertyName(c.Fields["name"])
		if name != "" && keys[key{name, static}] == 1 {
			memberOf[c] = member{owner: class, static: static}
		}
	}
}

// accessor reports a getter or setter: calling `o.x()` calls the value the
// getter returns, not the getter.
func accessor(n *parse.Node) bool {
	return hasToken(n, "get") || hasToken(n, "set")
}

// propertyName is the name a property key spells statically, or "" for a
// computed key.
func propertyName(key *parse.Node) string {
	if key == nil {
		return ""
	}
	switch key.Kind {
	case "property_identifier", "private_property_identifier", "identifier", "number":
		return key.Text
	case "string":
		if len(key.Text) >= 2 {
			return key.Text[1 : len(key.Text)-1]
		}
	}
	return ""
}

// aliasValue reports a declarator value whose object receiverOwner can
// name: `this`, another name, `new C(...)`, a class expression (the class
// and, through its prototype, its instances: TSX-25), or a fresh literal
// or function, which holds no owner (TSX-26).
func aliasValue(v *parse.Node) bool {
	switch v.Kind {
	case "this", "identifier", "new_expression", "class",
		"array", "object", "string", "number", "template_string", "regex", "true", "false", "null",
		"arrow_function", "function_expression", "function", "generator_function":
		return true
	}
	return false
}

// names lists the property names written through receivers of unknown
// owner (TSX-26), sorted, with "*" when one of them is not static; link
// time reads them for the file-scope static calls of ADR 0026.
func (w memberWrites) names() []string {
	var out []string
	if w.unknownAny {
		out = append(out, "*")
	}
	for name := range w.unknown {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// unknownWritten reports whether a callee `a.b.m` has a key written through
// a receiver of unknown owner (names from memberWrites.names): TSX-26 for
// the static member calls link time adds at file scope.
func unknownWritten(names []string, callee facts.RouteExpr) bool {
	if len(names) == 0 {
		return false
	}
	written := map[string]bool{}
	for _, name := range names {
		written[name] = true
	}
	if written["*"] {
		return true
	}
	for e := &callee; e != nil && e.Kind == "member_expression"; e = e.Object {
		if written[e.Name] {
			return true
		}
	}
	return false
}
