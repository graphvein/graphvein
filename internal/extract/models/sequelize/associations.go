package sequelize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// groupModules are the modules a group reaches: the instance's file, the
// files of its models and of the factory calls, and every file of the
// package that imports or requires one of them.
func (d *discovery) groupModules(g *Instance) []string {
	core := map[string]bool{g.File: true}
	for _, m := range g.Models {
		core[m.File] = true
		if m.Factory != nil {
			core[m.Factory.CallFile] = true
		}
	}
	set := map[string]bool{}
	for f := range core {
		set[f] = true
	}
	for _, f := range d.packageFiles(g.Package) {
		if set[f] {
			continue
		}
		for _, imp := range d.in.TypeScript[f].Imports {
			if res := d.in.Resolver.Resolve(f, imp.Module); res.File != "" && core[res.File] {
				set[f] = true
			}
		}
		if m := d.in.Models[f]; m != nil {
			walkModule(m, func(e *typescript.ModelExpr, inFunction bool) {
				if spec := requireSpec(e); spec != "" && !inFunction {
					if target, _ := d.resolveRequire(f, spec); core[target] {
						set[f] = true
					}
				}
			})
		}
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// modelRef returns the model of g an expression of file resolves to: an
// init model's class, or a module-level define's binding.
func (d *discovery) modelRef(g *Instance, file string, e *typescript.ModelExpr) *Model {
	v := d.expr(file, e, 0)
	for _, m := range g.Models {
		switch {
		case v.Class != nil && m.Class == v.Class:
			return m
		case v.Expr != nil && m.Context == "module" && m.Call == v.Expr:
			return m
		}
	}
	return nil
}

// reachAssociations fills g.Associations: the association calls at module
// level of the group's modules in statement order, with the calls of the
// associate functions that a recognised loop runs at the loop's position.
// Associate functions no loop runs, and associate bodies with statements
// this pass does not read, make the group incomplete.
func (d *discovery) reachAssociations(g *Instance) {
	modulesWithCalls := 0
	ran := map[*typescript.ModelExpr]bool{}
	for _, f := range d.groupModules(g) {
		m := d.in.Models[f]
		if m == nil {
			continue
		}
		n := 0
		for i := range m.Statements {
			s := &m.Statements[i]
			if s.Kind == typescript.StatementForOf {
				if obj := associateForOf(s); obj != "" {
					d.in.tag("SQ-K1/sequelize-cli loop")
					n += d.runLoop(g, f, m, obj, s.Span, ran)
				}
				continue
			}
			if s.Kind != typescript.StatementExpression {
				continue
			}
			if method := methodCall(s.Value); associationMethods[method] {
				d.in.tag("SQ-K1/module level")
				a := &Association{File: f, Span: s.Value.Span, Method: method, Call: s.Value, Context: "module", Source: d.modelRef(g, f, s.Value.Callee.Object)}
				if len(s.Value.Args) > 0 {
					a.Target = d.modelRef(g, f, &s.Value.Args[0])
				}
				g.Associations = append(g.Associations, a)
				d.reached[siteKey(f, s.Value.Span)] = true
				n++
				continue
			}
			if obj := associateLoop(s.Value); obj != "" {
				if filterLoop(s.Value) == "" {
					d.in.tag("SQ-K1/sequelize-cli loop")
				}
				n += d.runLoop(g, f, m, obj, s.Span, ran)
			}
		}
		if n > 0 {
			modulesWithCalls++
		}
	}
	g.OrderProven = modulesWithCalls <= 1
	// Associate functions that no recognised loop runs.
	for _, model := range g.Models {
		fn, file, why := associateOf(d, model)
		switch {
		case fn != nil && !ran[fn]:
			d.reportAssociate(g, file, fn.Span.StartLine, "an associate function that no recognised associate(models) loop runs")
		case why != "":
			d.reportAssociate(g, model.File, model.Span.StartLine, why)
		}
	}
}

// associateLoop recognises a module-level associate(models) loop
// statement over the models object M and returns M, or "": the filter
// form `Object.values(M).filter((m) => typeof m.associate ===
// "function").forEach((m) => m.associate(M))`, and the sequelize-cli forms
// `Object.keys(M).forEach((k) => <guarded M[k]>)` and
// `Object.values(M).forEach((m) => <guarded m>)`, where the guarded call is
// `R.associate && R.associate(M)` or `if (R.associate) R.associate(M)`
// (the guard may be `typeof R.associate === "function"`).
func associateLoop(e *typescript.ModelExpr) string {
	if methodCall(e) != "forEach" || len(e.Args) != 1 {
		return ""
	}
	if obj := filterLoop(e); obj != "" {
		return obj
	}
	fn := &e.Args[0]
	if fn.Kind != typescript.ModelArrow && fn.Kind != typescript.ModelFunction || len(fn.Params) != 1 || fn.Params[0] == "" {
		return ""
	}
	obj, isRef := iterated(e.Callee.Object, fn.Params[0])
	if obj == "" {
		return ""
	}
	stmts := fn.Statements
	if fn.Kind == typescript.ModelArrow && !fn.Block {
		stmts = []typescript.ModelStatement{{Kind: typescript.StatementExpression, Span: fn.Body.Span, Value: fn.Body}}
	}
	if !guardedAssociate(stmts, isRef, obj) {
		return ""
	}
	return obj
}

// associateForOf recognises the for-of form of the sequelize-cli
// loop: `for (const k of Object.keys(M)) <guarded M[k]>` or
// `for (const m of Object.values(M)) <guarded m>`.
func associateForOf(s *typescript.ModelStatement) string {
	obj, isRef := iterated(s.Value, s.Name)
	if obj == "" || !guardedAssociate(s.Body, isRef, obj) {
		return ""
	}
	return obj
}

// iterated reads `Object.keys(M)` (each element a key: the reference is
// `M[name]`) or `Object.values(M)` (the reference is `name`) and returns M
// and the reference test.
func iterated(src *typescript.ModelExpr, name string) (string, func(*typescript.ModelExpr) bool) {
	if src == nil || src.Kind != typescript.ModelCall || len(src.Args) != 1 || src.Args[0].Kind != typescript.ModelIdentifier {
		return "", nil
	}
	obj := src.Args[0].Name
	switch memberPath(src.Callee) {
	case "Object.keys":
		return obj, func(x *typescript.ModelExpr) bool {
			return x.Kind == typescript.ModelIndex && x.Object.Kind == typescript.ModelIdentifier && x.Object.Name == obj && x.Index.Kind == typescript.ModelIdentifier && x.Index.Name == name
		}
	case "Object.values":
		return obj, func(x *typescript.ModelExpr) bool {
			return x.Kind == typescript.ModelIdentifier && x.Name == name
		}
	}
	return "", nil
}

// guardedAssociate reports one statement that calls R.associate(obj)
// behind a guard on R.associate: `G && R.associate(obj)` or `if (G)
// R.associate(obj)` (a block of that one call or the call alone), G being
// `R.associate` or `typeof R.associate === "function"`.
func guardedAssociate(stmts []typescript.ModelStatement, isRef func(*typescript.ModelExpr) bool, obj string) bool {
	if len(stmts) != 1 {
		return false
	}
	member := func(x *typescript.ModelExpr) bool {
		return x != nil && x.Kind == typescript.ModelMember && x.Name == "associate" && isRef(x.Object)
	}
	guard := func(x *typescript.ModelExpr) bool {
		if member(x) {
			return true
		}
		if x == nil || x.Kind != typescript.ModelBinary || (x.Value != "===" && x.Value != "==") {
			return false
		}
		isTypeof := func(y *typescript.ModelExpr) bool {
			return y.Kind == typescript.ModelUnary && y.Value == "typeof" && member(y.Object)
		}
		isFunction := func(y *typescript.ModelExpr) bool { return y.Kind == typescript.ModelString && y.Value == "function" }
		return isTypeof(x.Left) && isFunction(x.Right) || isTypeof(x.Right) && isFunction(x.Left)
	}
	call := func(x *typescript.ModelExpr) bool {
		return x != nil && methodCall(x) == "associate" && member(x.Callee) && len(x.Args) == 1 && x.Args[0].Kind == typescript.ModelIdentifier && x.Args[0].Name == obj
	}
	s := &stmts[0]
	switch s.Kind {
	case typescript.StatementExpression:
		v := s.Value
		return v.Kind == typescript.ModelBinary && v.Value == "&&" && guard(v.Left) && call(v.Right)
	case typescript.StatementIf:
		return guard(s.Value) && len(s.Body) == 1 && s.Body[0].Kind == typescript.StatementExpression && call(s.Body[0].Value)
	}
	return false
}

// filterLoop recognises `Object.values(M).filter((m) => typeof
// m.associate === "function").forEach((m) => m.associate(M))` and returns
// M, or "".
func filterLoop(e *typescript.ModelExpr) string {
	filter := e.Callee.Object
	if methodCall(filter) != "filter" || len(filter.Args) != 1 {
		return ""
	}
	values := filter.Callee.Object
	if methodCall(values) != "values" || memberPath(values.Callee) != "Object.values" || len(values.Args) != 1 || values.Args[0].Kind != typescript.ModelIdentifier {
		return ""
	}
	obj := values.Args[0].Name
	pred := single(&filter.Args[0])
	if pred == nil || pred.Kind != typescript.ModelBinary || (pred.Value != "===" && pred.Value != "==") {
		return ""
	}
	param := filter.Args[0].Params[0]
	isTypeof := func(x *typescript.ModelExpr) bool {
		return x.Kind == typescript.ModelUnary && x.Value == "typeof" && memberPath(x.Object) == param+".associate"
	}
	isFunction := func(x *typescript.ModelExpr) bool { return x.Kind == typescript.ModelString && x.Value == "function" }
	if !(isTypeof(pred.Left) && isFunction(pred.Right)) && !(isTypeof(pred.Right) && isFunction(pred.Left)) {
		return ""
	}
	body := single(&e.Args[0])
	if body == nil || methodCall(body) != "associate" || memberPath(body.Callee) != e.Args[0].Params[0]+".associate" {
		return ""
	}
	if len(body.Args) != 1 || body.Args[0].Kind != typescript.ModelIdentifier || body.Args[0].Name != obj {
		return ""
	}
	return obj
}

// single returns the expression a one-parameter arrow evaluates: its
// expression body, or the expression of a block body's only statement
// (an expression statement or a return).
func single(fn *typescript.ModelExpr) *typescript.ModelExpr {
	if fn.Kind != typescript.ModelArrow || len(fn.Params) != 1 || fn.Params[0] == "" {
		return nil
	}
	if !fn.Block {
		return fn.Body
	}
	if len(fn.Statements) == 1 && (fn.Statements[0].Kind == typescript.StatementExpression || fn.Statements[0].Kind == typescript.StatementReturn) {
		return fn.Statements[0].Value
	}
	return nil
}

// objEntry is one entry of the models object in key order.
type objEntry struct {
	key   string
	value *typescript.ModelExpr
	line  int
}

// modelsObject reads the models object obj of file as the loop at loop
// sees it: a module-level const object literal's plain entries, then the
// module-level assignments `obj.<Key> = <value>` before the loop (the
// sequelize-cli form `db.User = require("./user")(sequelize, DataTypes)`;
// a key keeps its first position and takes its last
// value). Any other use of obj before the loop (a computed assignment, a
// call that receives it, a directory loader's `db[model.name] = model`)
// may add entries this pass does not read (why).
func (d *discovery) modelsObject(file string, m *typescript.ModelSyntax, obj string, loop contract.Span) ([]objEntry, []string, string) {
	decl := declaration(m, obj)
	if decl == nil || decl.Keyword != "const" || decl.Value.Kind != typescript.ModelObject {
		return nil, nil, fmt.Sprintf("%s: an associate(models) loop over %s, which is not a module-level const object literal", file, obj)
	}
	var out []objEntry
	var unread []string
	index := map[string]int{}
	add := func(key string, v *typescript.ModelExpr, line int) {
		if i, ok := index[key]; ok {
			out[i].value = v
			return
		}
		index[key] = len(out)
		out = append(out, objEntry{key: key, value: v, line: line})
	}
	for i := range decl.Value.Entries {
		en := &decl.Value.Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
			unread = append(unread, fmt.Sprintf("%s:%d: an entry of the models object %s that is not a plain property; the models and associations it adds are not read", file, en.Span.StartLine, obj))
			continue
		}
		add(en.Key, &en.Value, en.Span.StartLine)
	}
	before := func(sp contract.Span) bool {
		return sp.StartLine < loop.StartLine || sp.StartLine == loop.StartLine && sp.StartColumn < loop.StartColumn
	}
	if !before(decl.Span) {
		return nil, nil, fmt.Sprintf("%s: the associate(models) loop runs before the models object %s is declared", file, obj)
	}
	mentions := func(e *typescript.ModelExpr) bool {
		found := false
		walk(e, false, func(x *typescript.ModelExpr, _ bool) {
			found = found || x.Kind == typescript.ModelIdentifier && x.Name == obj
		})
		return found
	}
	for i := range m.Statements {
		s := &m.Statements[i]
		if !before(s.Span) || !after(s.Span, decl.Span) {
			continue
		}
		if s.Kind == typescript.StatementExpression && s.Value.Kind == typescript.ModelAssign {
			l := s.Value.Left
			if l.Kind == typescript.ModelMember && l.Object.Kind == typescript.ModelIdentifier && l.Object.Name == obj {
				d.in.tag("SQ-K1/models object assignments")
				add(l.Name, s.Value.Right, s.Span.StartLine)
				if mentions(s.Value.Right) {
					unread = append(unread, fmt.Sprintf("%s:%d: the models object %s is used in the value of its entry %s", file, s.Span.StartLine, obj, l.Name))
				}
				continue
			}
		}
		used := mentions(s.Value) || mentions(s.Extends)
		for j := range s.Body {
			used = used || mentions(s.Body[j].Value)
		}
		if used {
			unread = append(unread, fmt.Sprintf("%s:%d: a statement uses the models object %s before the associate(models) loop; the models it may add are not read", file, s.Span.StartLine, obj))
		}
	}
	for i := range m.Declarations {
		x := &m.Declarations[i]
		if x != decl && before(x.Span) && after(x.Span, decl.Span) && mentions(&x.Value) {
			unread = append(unread, fmt.Sprintf("%s:%d: a declaration uses the models object %s before the associate(models) loop; the models it may add are not read", file, x.Span.StartLine, obj))
		}
	}
	return out, unread, ""
}

// after reports a span that starts after another's start.
func after(a, b contract.Span) bool {
	return a.StartLine > b.StartLine || a.StartLine == b.StartLine && a.StartColumn > b.StartColumn
}

// runLoop applies a recognised loop (the statement at loop) over the
// models object obj of file: for each entry in key order, the model it
// holds and that model's associate function, whose association calls are
// reached in statement order. It returns the number of calls reached.
func (d *discovery) runLoop(g *Instance, file string, m *typescript.ModelSyntax, obj string, loop contract.Span, ran map[*typescript.ModelExpr]bool) int {
	entries, unread, why := d.modelsObject(file, m, obj, loop)
	if why != "" {
		g.addAttributesIncomplete(why)
		return 0
	}
	d.in.tag("SQ-K1/`associate(models)` loop")
	for _, u := range unread {
		g.addAttributesIncomplete(u)
	}
	keys := map[string]*Model{}
	var order []*Model
	for _, en := range entries {
		var model *Model
		for _, cand := range g.Models {
			if cand.Factory != nil && cand.Factory.Call == en.value {
				model = cand
			}
		}
		if model == nil {
			model = d.modelRef(g, file, en.value)
		}
		if model == nil {
			if d.noAssociate(g, file, en.value) {
				// The instance or the Sequelize package: no associate.
				continue
			}
			g.addAttributesIncomplete(fmt.Sprintf("%s:%d: the models object entry %s does not resolve to a model of the group; its associate function is not read", file, en.line, en.key))
			continue
		}
		keys[en.key] = model
		order = append(order, model)
	}
	n := 0
	for _, model := range order {
		fn, fnFile, why := associateOf(d, model)
		if fn == nil {
			if why != "" {
				g.addAttributesIncomplete(fmt.Sprintf("%s:%d: model %s is in the associate(models) loop; %s", model.File, model.Span.StartLine, model.ClassName, why))
			}
			continue
		}
		ran[fn] = true
		param := ""
		if len(fn.Params) > 0 {
			param = fn.Params[0]
		}
		stmts := fn.Statements
		if fn.Kind == typescript.ModelArrow && !fn.Block {
			stmts = []typescript.ModelStatement{{Kind: typescript.StatementExpression, Span: fn.Body.Span, Value: fn.Body}}
		}
		for i := range stmts {
			s := &stmts[i]
			method := methodCall(s.Value)
			if s.Kind != typescript.StatementExpression || !associationMethods[method] {
				d.reportAssociate(g, fnFile, fn.Span.StartLine, fmt.Sprintf("a statement at line %d of an associate function that is not an association call", s.Span.StartLine))
				continue
			}
			a := &Association{File: fnFile, Span: s.Value.Span, Method: method, Call: s.Value, Context: "associate", LoopFile: file, LoopSpan: loop}
			a.Source = d.associateRef(g, model, fnFile, param, keys, s.Value.Callee.Object)
			if len(s.Value.Args) > 0 {
				a.Target = d.associateRef(g, model, fnFile, param, keys, &s.Value.Args[0])
			}
			g.Associations = append(g.Associations, a)
			d.reached[siteKey(fnFile, s.Value.Span)] = true
			n++
		}
	}
	return n
}

// noAssociate reports a models object entry that is the group's instance
// or the Sequelize package (the template's `db.sequelize` and
// `db.Sequelize`): neither has an associate method.
func (d *discovery) noAssociate(g *Instance, file string, e *typescript.ModelExpr) bool {
	v := d.expr(file, e, 0)
	return v.Decl == g.Name && v.File == g.File || v.isSequelizeModule() || v.isSequelizeClass()
}

// associateRef resolves a model reference inside an associate function:
// the owning model's binding or class name, or `<param>.<Key>` of the
// models object.
func (d *discovery) associateRef(g *Instance, owner *Model, file, param string, keys map[string]*Model, e *typescript.ModelExpr) *Model {
	if e.Kind == typescript.ModelIdentifier && (e.Name == owner.Binding && owner.Binding != "" || e.Name == owner.ClassName && owner.ClassName != "") {
		return owner
	}
	if e.Kind == typescript.ModelMember && e.Object.Kind == typescript.ModelIdentifier && e.Object.Name == param && param != "" {
		return keys[e.Name]
	}
	if owner.Context == "module" {
		return d.modelRef(g, file, e)
	}
	return nil
}

// associateOf returns a model's associate function: `<binding>.associate
// = fn` in its factory body, `<Class or binding>.associate = fn` at module
// level of its file, or a plain `static associate(...)` method of an init
// model's class (payload version 4). why says why a static
// associate member of the class or of a class it extends is not read (a
// static field, accessor, async method, computed name or static block, or
// one inherited from a base class: its `this` is the subclass).
func associateOf(d *discovery, model *Model) (*typescript.ModelExpr, string, string) {
	name := model.Binding
	if model.Form == "init" {
		name = model.ClassName
	}
	if name == "" {
		return nil, "", ""
	}
	var stmts []typescript.ModelStatement
	if model.Factory != nil {
		stmts = model.Factory.Expr.Statements
	} else if m := d.in.Models[model.File]; m != nil {
		stmts = m.Statements
	}
	for i := range stmts {
		s := &stmts[i]
		if s.Kind != typescript.StatementExpression || s.Value.Kind != typescript.ModelAssign || memberPath(s.Value.Left) != name+".associate" {
			continue
		}
		if fn := s.Value.Right; fn.Kind == typescript.ModelArrow || fn.Kind == typescript.ModelFunction {
			return fn, model.File, ""
		}
	}
	if model.Form != "init" || model.Class == nil {
		return nil, "", ""
	}
	file, c := model.File, model.Class
	for depth := 0; c != nil && depth <= maxDepth; depth++ {
		for i := range c.Statics {
			st := &c.Statics[i]
			switch {
			case st.Kind == typescript.StatementFunction && st.Name == "associate" && depth == 0:
				d.in.tag("SQ-K1/`static associate(models)`")
				return st.Value, file, ""
			case st.Name == "associate" || st.Name == "" && st.Kind == typescript.StatementUnknown:
				return nil, "", fmt.Sprintf("a static member of class %s (%s:%d) may be its associate method, which is not read", c.Name, file, st.Span.StartLine)
			}
		}
		if c.Extends == nil {
			break
		}
		v := scope{d: d, file: file}.resolve(c.Extends)
		file, c = v.File, v.Class
	}
	return nil, "", ""
}

// reportAssociate records an associate function's unread part once per
// function and reason.
func (d *discovery) reportAssociate(g *Instance, file string, line int, what string) {
	msg := fmt.Sprintf("%s:%d: %s; the attributes its association calls may add are not read", file, line, what)
	for _, x := range g.Incomplete {
		if x == msg {
			return
		}
	}
	g.addAttributesIncomplete(msg)
	d.diag(file, codeUnread, msg+listMarker(g.SetKey()))
}

// unreachedCalls reports the association calls of the package's files
// (the ungated call-site cache, every call with its callee chain) that no
// instance reaches: their targets are unknown, so every instance of the
// package is incomplete. Test files are counted in one marker.
func (d *discovery) unreachedCalls(p *Package) {
	var markers []string
	tests := d.testSites[p]
	for _, f := range d.packageFiles(p) {
		for _, c := range clientCalls(d.in.TypeScript[f]) {
			method := c.Expression.Object.Name
			if !associationMethods[method] || len(c.Expression.Args) == 0 || d.reached[siteKey(f, c.Span)] {
				continue
			}
			if d.isTest(f) {
				tests++
				continue
			}
			msg := fmt.Sprintf("%s:%d: %s(...) outside the reach rule (not at module level of a module the group reaches, nor in an associate function a recognised loop runs); the attributes it may add are not read", f, c.Span.StartLine, c.Expression.Object.Text)
			if !containsMarker(p, msg) {
				markers = append(markers, msg)
				d.unreadDiag(p, f, msg)
			}
		}
	}
	if tests > 0 {
		d.diag(p.Manifest, codeUnread, fmt.Sprintf("%s: %d model definition or association call site(s) in test files are not read", p.Manifest, tests))
	}
	for _, g := range p.Instances {
		for _, m := range markers {
			g.addAttributesIncomplete(m)
		}
	}
}

// addAttributesIncomplete records an incompleteness about association
// attributes: in Incomplete and in AttributesIncomplete.
func (g *Instance) addAttributesIncomplete(msg string) {
	g.Incomplete = append(g.Incomplete, msg)
	g.AttributesIncomplete = append(g.AttributesIncomplete, msg)
}

func containsMarker(p *Package, msg string) bool {
	for _, g := range p.Instances {
		for _, x := range g.Incomplete {
			if strings.Contains(x, msg) {
				return true
			}
		}
	}
	return false
}
