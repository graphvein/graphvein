package golang

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file captures the request and response syntax of task
// T029 (docs/contract/claims/route-shape-go.md, section "Payload"): in a
// non-test file that imports net/http or gin, every function, method and
// function literal that declares a carrier parameter (*gin.Context,
// *http.Request, http.ResponseWriter), with its parameters, the blocks of
// its body, the names it declares and its calls; and, in every non-test
// file, the methods that change how encoding/json reads or writes a type.
// It reads go/parser's tree, which the extractor already builds for row
// GS-F2, so no file is parsed twice. It records syntax only: which
// function is a route's handler and what a call claims are decided at
// link time.

// Import paths of the carrier packages.
const (
	ginImport  = "github.com/gin-gonic/gin"
	httpImport = "net/http"
)

// marshalerMethods are the methods encoding/json calls instead of reading
// a type's fields, and gin's BindUnmarshaler method for form values.
var marshalerMethods = map[string]bool{"MarshalJSON": true, "UnmarshalJSON": true, "MarshalText": true, "UnmarshalText": true, "UnmarshalParam": true}

// captureShapes returns the shape payload of a parsed file: Shapes for a
// file that imports net/http or gin, Marshalers for every file.
func captureShapes(file *ast.File, fset *token.FileSet) ([]facts.GoShapeFunc, []facts.GoMarshaler) {
	var marshalers []facts.GoMarshaler
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !marshalerMethods[fn.Name.Name] {
			continue
		}
		if recv := astReceiver(fn); recv != "" {
			marshalers = append(marshalers, facts.GoMarshaler{Receiver: recv, Name: fn.Name.Name, Line: fset.Position(fn.Pos()).Line})
		}
	}
	gin, http := "", ""
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch p {
		case ginImport:
			if name == "" {
				name = "gin"
			}
			gin = name
		case httpImport:
			if name == "" {
				name = "http"
			}
			http = name
		}
	}
	if gin == "_" || gin == "." {
		gin = ""
	}
	if http == "_" || http == "." {
		http = ""
	}
	if gin == "" && http == "" {
		return nil, marshalers
	}
	c := &shapeCapture{fset: fset, gin: gin, http: http}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			c.function(d.Name.Name, astReceiver(d), d.Recv, d.Type, d.Body, d.Pos(), false)
		case *ast.GenDecl:
			// Function literals in package-level vars may be handlers.
			ast.Inspect(d, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					if c.carriers(lit.Type) {
						c.function("", "", nil, lit.Type, lit.Body, lit.Pos(), true)
					}
					return false
				}
				return true
			})
		}
	}
	return c.out, marshalers
}

// astReceiver is a method's receiver base type name (T, *T, T[...]).
func astReceiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// shapeCapture holds one file's capture state.
type shapeCapture struct {
	fset      *token.FileSet
	gin, http string
	out       []facts.GoShapeFunc
	// localTypes are the type names declared in the function bodies
	// being captured, outermost first (GoShapeFunc.LocalTypes).
	localTypes []string
}

// carrier names the carrier role of a parameter type.
func (c *shapeCapture) carrier(t ast.Expr) string {
	sel := func(e ast.Expr, pkg, name string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok || pkg == "" {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == pkg && s.Sel.Name == name
	}
	if s, ok := t.(*ast.StarExpr); ok {
		switch {
		case sel(s.X, c.gin, "Context"):
			return facts.GoCarrierGin
		case sel(s.X, c.http, "Request"):
			return facts.GoCarrierRequest
		}
		return ""
	}
	if sel(t, c.http, "ResponseWriter") {
		return facts.GoCarrierWriter
	}
	return ""
}

// carriers reports whether a signature declares a carrier parameter.
func (c *shapeCapture) carriers(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, f := range ft.Params.List {
		if c.carrier(f.Type) != "" {
			return true
		}
	}
	return false
}

// appendTypeNames adds the names of the type declarations in body (at any
// depth, function literals included) to names, each once.
func appendTypeNames(names []string, body *ast.BlockStmt) []string {
	if body == nil {
		return names
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name != nil && !slices.Contains(names, ts.Name.Name) {
			names = append(names, ts.Name.Name)
		}
		return true
	})
	return names
}

// shapeBody is the state of one captured function.
type shapeBody struct {
	c *shapeCapture
	// idx is the record's index in c.out (records nested literals append
	// may move the slice, so the record is always reached through it).
	idx    int
	params map[string]bool
	locals map[string]int
}

// function captures one function, method or literal if it declares a
// carrier parameter; literals with carrier parameters inside it are
// captured as records of their own.
func (c *shapeCapture) function(name, receiver string, recv *ast.FieldList, ft *ast.FuncType, body *ast.BlockStmt, pos token.Pos, literal bool) {
	// The type names in scope of this body: its own declarations and those
	// of the functions enclosing it (a literal's enclosing bodies).
	outer := c.localTypes
	c.localTypes = appendTypeNames(append([]string(nil), outer...), body)
	defer func() { c.localTypes = outer }()
	if !c.carriers(ft) {
		// Literals with carrier parameters inside a function without one.
		ast.Inspect(body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				if c.carriers(lit.Type) {
					c.function("", "", nil, lit.Type, lit.Body, lit.Pos(), true)
					return false
				}
			}
			return true
		})
		return
	}
	p := c.fset.Position(pos)
	f := facts.GoShapeFunc{Name: name, Receiver: receiver, Line: p.Line, LocalTypes: c.localTypes}
	if literal {
		f.Column = p.Column
	}
	b := &shapeBody{c: c, params: map[string]bool{}, locals: map[string]int{}}
	if recv != nil && len(recv.List) == 1 && len(recv.List[0].Names) == 1 {
		f.ReceiverName = recv.List[0].Names[0].Name
		b.params[f.ReceiverName] = true
	}
	for _, field := range ft.Params.List {
		role := c.carrier(field.Type)
		t := astReduce(field.Type, 0, true)
		if len(field.Names) == 0 {
			f.Params = append(f.Params, facts.GoShapeParam{Carrier: role, Type: t})
			continue
		}
		for _, n := range field.Names {
			f.Params = append(f.Params, facts.GoShapeParam{Name: n.Name, Carrier: role, Type: t})
			b.params[n.Name] = true
		}
	}
	b.idx = len(c.out)
	c.out = append(c.out, f)
	b.block(body.List, 0, false)
}

// rec is the record being captured.
func (b *shapeBody) rec() *facts.GoShapeFunc { return &b.c.out[b.idx] }

// newBlock adds a block with the given parent and returns its index.
func (b *shapeBody) newBlock(parent int) int {
	b.rec().Blocks = append(b.rec().Blocks, parent)
	return len(b.rec().Blocks)
}

// declare records a declared or assigned local name.
func (b *shapeBody) declare(name string, typ, value ast.Expr, pos token.Pos) {
	if name == "" || name == "_" {
		return
	}
	if b.params[name] {
		b.reassign(name)
		return
	}
	line := b.c.fset.Position(pos).Line
	for i := range b.rec().Locals {
		if b.rec().Locals[i].Name == name {
			b.rec().Locals[i].Count++
			return
		}
	}
	l := facts.GoShapeLocal{Name: name, Count: 1, Line: line}
	if typ != nil {
		t := astReduce(typ, 0, true)
		l.Type = &t
	}
	if value != nil && localValue(value) {
		v := astReduce(value, 0, false)
		l.Value = &v
	}
	b.rec().Locals = append(b.rec().Locals, l)
}

func (b *shapeBody) reassign(name string) {
	for _, n := range b.rec().Reassigned {
		if n == name {
			return
		}
	}
	b.rec().Reassigned = append(b.rec().Reassigned, name)
}

// localValue reports the value forms a local keeps: composite literals,
// & of one, calls and selector chains (the forms the linker types).
func localValue(e ast.Expr) bool {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.CompositeLit, *ast.CallExpr, *ast.SelectorExpr:
		return true
	case *ast.UnaryExpr:
		_, ok := ast.Unparen(x.X).(*ast.CompositeLit)
		return x.Op == token.AND && ok
	}
	return false
}

// block walks a statement list in block blk.
func (b *shapeBody) block(list []ast.Stmt, blk int, closure bool) {
	if len(list) > 0 {
		if _, ok := list[len(list)-1].(*ast.ReturnStmt); ok {
			b.rec().Terminal = append(b.rec().Terminal, blk)
		}
	}
	for i, s := range list {
		var next ast.Stmt
		if i+1 < len(list) {
			next = list[i+1]
		}
		b.stmt(s, next, blk, closure)
	}
}

// stmt walks one statement; next is the statement after it in the same
// list (for the err-check form of Checked).
func (b *shapeBody) stmt(s ast.Stmt, next ast.Stmt, blk int, closure bool) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		b.expr(s.X, blk, closure, nil, false)
	case *ast.AssignStmt:
		names := make([]string, len(s.Lhs))
		for i, l := range s.Lhs {
			if id, ok := l.(*ast.Ident); ok {
				names[i] = id.Name
			} else {
				b.expr(l, blk, closure, nil, false)
			}
		}
		if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
			checked := false
			if last := names[len(names)-1]; last != "" && last != "_" {
				checked = checks(next, last)
			}
			b.expr(s.Rhs[0], blk, closure, names, checked)
		} else {
			for i, r := range s.Rhs {
				var res []string
				checked := false
				if len(s.Rhs) == len(s.Lhs) {
					res = []string{names[i]}
					if names[i] != "" && names[i] != "_" && len(s.Rhs) == 1 {
						checked = checks(next, names[i])
					}
				}
				b.expr(r, blk, closure, res, checked)
			}
		}
		for i, n := range names {
			if n == "" {
				continue
			}
			var value ast.Expr
			if len(s.Rhs) == len(s.Lhs) {
				value = s.Rhs[i]
			}
			b.declare(n, nil, value, s.Pos())
		}
	case *ast.DeclStmt:
		g, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range g.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				b.expr(v, blk, closure, nil, false)
			}
			for i, n := range vs.Names {
				var value ast.Expr
				if len(vs.Values) == len(vs.Names) {
					value = vs.Values[i]
				}
				b.declare(n.Name, vs.Type, value, s.Pos())
			}
		}
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			b.expr(r, blk, closure, nil, false)
		}
	case *ast.IfStmt:
		inner := b.newBlock(blk)
		checked := false
		if s.Init != nil {
			if a, ok := s.Init.(*ast.AssignStmt); ok && len(a.Rhs) == 1 {
				if id, ok := a.Lhs[len(a.Lhs)-1].(*ast.Ident); ok && id.Name != "_" {
					checked = nilTest(s.Cond, id.Name) && endsInReturn(s.Body)
				}
				names := make([]string, len(a.Lhs))
				for i, l := range a.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						names[i] = id.Name
						b.declare(id.Name, nil, nil, a.Pos())
					}
				}
				b.expr(a.Rhs[0], inner, closure, names, checked)
			} else {
				b.stmt(s.Init, nil, inner, closure)
			}
		}
		if bin, ok := s.Cond.(*ast.BinaryExpr); ok && bin.Op == token.NEQ && isNil(bin.Y) && endsInReturn(s.Body) {
			// if X(...) != nil { ...; return }
			b.expr(bin.X, inner, closure, nil, true)
			b.expr(bin.Y, inner, closure, nil, false)
		} else {
			b.expr(s.Cond, inner, closure, nil, false)
		}
		b.block(s.Body.List, b.newBlock(inner), closure)
		if s.Else != nil {
			els := b.newBlock(inner)
			if eb, ok := s.Else.(*ast.BlockStmt); ok {
				b.block(eb.List, els, closure)
			} else {
				b.stmt(s.Else, nil, els, closure)
			}
		}
	case *ast.BlockStmt:
		b.block(s.List, b.newBlock(blk), closure)
	case *ast.ForStmt:
		inner := b.newBlock(blk)
		if s.Init != nil {
			b.stmt(s.Init, nil, inner, closure)
		}
		if s.Cond != nil {
			b.expr(s.Cond, inner, closure, nil, false)
		}
		if s.Post != nil {
			b.stmt(s.Post, nil, inner, closure)
		}
		b.block(s.Body.List, inner, closure)
	case *ast.RangeStmt:
		inner := b.newBlock(blk)
		b.expr(s.X, blk, closure, nil, false)
		for _, e := range []ast.Expr{s.Key, s.Value} {
			if id, ok := e.(*ast.Ident); ok {
				b.declare(id.Name, nil, nil, s.Pos())
			}
		}
		b.block(s.Body.List, inner, closure)
	case *ast.SwitchStmt:
		inner := b.newBlock(blk)
		if s.Init != nil {
			b.stmt(s.Init, nil, inner, closure)
		}
		if s.Tag != nil {
			b.expr(s.Tag, inner, closure, nil, false)
		}
		for _, cl := range s.Body.List {
			cc := cl.(*ast.CaseClause)
			for _, e := range cc.List {
				b.expr(e, inner, closure, nil, false)
			}
			b.block(cc.Body, b.newBlock(inner), closure)
		}
	case *ast.TypeSwitchStmt:
		inner := b.newBlock(blk)
		if s.Init != nil {
			b.stmt(s.Init, nil, inner, closure)
		}
		b.stmt(s.Assign, nil, inner, closure)
		for _, cl := range s.Body.List {
			b.block(cl.(*ast.CaseClause).Body, b.newBlock(inner), closure)
		}
	case *ast.SelectStmt:
		inner := b.newBlock(blk)
		for _, cl := range s.Body.List {
			cc := cl.(*ast.CommClause)
			cb := b.newBlock(inner)
			if cc.Comm != nil {
				b.stmt(cc.Comm, nil, cb, closure)
			}
			b.block(cc.Body, cb, closure)
		}
	case *ast.GoStmt:
		b.expr(s.Call, b.newBlock(blk), true, nil, false)
	case *ast.DeferStmt:
		b.expr(s.Call, b.newBlock(blk), true, nil, false)
	case *ast.LabeledStmt:
		b.stmt(s.Stmt, next, blk, closure)
	case *ast.SendStmt:
		b.expr(s.Chan, blk, closure, nil, false)
		b.expr(s.Value, blk, closure, nil, false)
	case *ast.IncDecStmt:
		if id, ok := s.X.(*ast.Ident); ok && b.params[id.Name] {
			b.reassign(id.Name)
		}
	}
}

// expr captures the outermost calls of an expression and walks function
// literals; results and checked describe the call when e is one.
func (b *shapeBody) expr(e ast.Expr, blk int, closure bool, results []string, checked bool) {
	if e == nil {
		return
	}
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		p := b.c.fset.Position(call.Pos())
		rec := facts.GoShapeCall{Call: astReduce(call, 0, false), Line: p.Line, Column: p.Column, Checked: checked, Block: blk, Closure: closure}
		for _, r := range results {
			if r == "_" {
				r = ""
			}
			rec.Results = append(rec.Results, r)
		}
		if allEmpty(rec.Results) {
			rec.Results = nil
		}
		b.rec().Calls = append(b.rec().Calls, rec)
		// Function literals among the arguments run in another context.
		for _, a := range call.Args {
			b.literals(a, blk)
		}
		b.literals(call.Fun, blk)
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			b.expr(x, blk, closure, nil, false)
			return false
		case *ast.FuncLit:
			b.literal(x, blk)
			return false
		}
		return true
	})
}

// literals walks the function literals inside an expression tree that is
// already captured as part of a call.
func (b *shapeBody) literals(e ast.Expr, blk int) {
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			b.literal(lit, blk)
			return false
		}
		return true
	})
}

// literal reads a function literal: one with a carrier parameter is a
// record of its own (captured after this one is complete), any other's
// calls belong to the enclosing record as closure calls.
func (b *shapeBody) literal(lit *ast.FuncLit, blk int) {
	if b.c.carriers(lit.Type) {
		b.c.function("", "", nil, lit.Type, lit.Body, lit.Pos(), true)
		return
	}
	if lit.Type.Params != nil {
		for _, f := range lit.Type.Params.List {
			for _, n := range f.Names {
				if b.params[n.Name] {
					b.reassign(n.Name)
				}
			}
		}
	}
	b.block(lit.Body.List, b.newBlock(blk), true)
}

func allEmpty(s []string) bool {
	for _, x := range s {
		if x != "" {
			return false
		}
	}
	return true
}

func isNil(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "nil"
}

// nilTest reports cond is `name != nil`.
func nilTest(cond ast.Expr, name string) bool {
	bin, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ || !isNil(bin.Y) {
		return false
	}
	id, ok := ast.Unparen(bin.X).(*ast.Ident)
	return ok && id.Name == name
}

// checks reports that stmt is `if name != nil { ...; return }`.
func checks(stmt ast.Stmt, name string) bool {
	s, ok := stmt.(*ast.IfStmt)
	return ok && s.Init == nil && nilTest(s.Cond, name) && endsInReturn(s.Body)
}

func endsInReturn(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

// astReduce reduces a go/ast expression to the payload's expression tree
// (facts.GoExpr), as the tree-sitter reducer does for the other payloads.
// typ reports a type context, where *T is a pointer type.
func astReduce(e ast.Expr, depth int, typ bool) facts.GoExpr {
	if e == nil {
		return facts.GoExpr{Kind: facts.GoUnknown}
	}
	if depth >= facts.GoMaxDepth {
		return facts.GoExpr{Kind: facts.GoUnknown}
	}
	sub := func(x ast.Expr, t bool) *facts.GoExpr {
		r := astReduce(x, depth+1, t)
		return &r
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return astReduce(x.X, depth, typ)
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return facts.GoExpr{Kind: facts.GoBoolean, Value: x.Name}
		case "nil":
			return facts.GoExpr{Kind: facts.GoNil}
		}
		return facts.GoExpr{Kind: facts.GoIdentifier, Name: x.Name}
	case *ast.SelectorExpr:
		return facts.GoExpr{Kind: facts.GoSelector, Object: sub(x.X, typ), Name: x.Sel.Name}
	case *ast.CallExpr:
		out := facts.GoExpr{Kind: facts.GoCall, Callee: sub(x.Fun, false)}
		for i, a := range x.Args {
			r := astReduce(a, depth+1, false)
			if x.Ellipsis.IsValid() && i == len(x.Args)-1 {
				inner := r
				r = facts.GoExpr{Kind: facts.GoSpread, Object: &inner}
			}
			out.Args = append(out.Args, r)
		}
		return out
	case *ast.CompositeLit:
		out := facts.GoExpr{Kind: facts.GoComposite}
		if x.Type != nil {
			out.Type = sub(x.Type, true)
		}
		for _, el := range x.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				k := astReduce(kv.Key, depth+1, false)
				out.Elements = append(out.Elements, facts.GoElement{Key: &k, Value: astReduce(kv.Value, depth+1, false)})
				continue
			}
			out.Elements = append(out.Elements, facts.GoElement{Value: astReduce(el, depth+1, false)})
		}
		return out
	case *ast.UnaryExpr:
		return facts.GoExpr{Kind: facts.GoUnary, Value: x.Op.String(), Object: sub(x.X, false)}
	case *ast.StarExpr:
		if typ {
			return facts.GoExpr{Kind: facts.GoPointer, Object: sub(x.X, true)}
		}
		return facts.GoExpr{Kind: facts.GoUnary, Value: "*", Object: sub(x.X, false)}
	case *ast.BinaryExpr:
		return facts.GoExpr{Kind: facts.GoBinary, Value: x.Op.String(), Left: sub(x.X, false), Right: sub(x.Y, false)}
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			if v, err := strconv.Unquote(x.Value); err == nil {
				return facts.GoExpr{Kind: facts.GoString, Value: v}
			}
			return facts.GoExpr{Kind: facts.GoUnknown}
		case token.INT, token.FLOAT, token.IMAG:
			return facts.GoExpr{Kind: facts.GoNumber, Value: x.Value}
		}
		return facts.GoExpr{Kind: facts.GoUnknown}
	case *ast.IndexExpr:
		return facts.GoExpr{Kind: facts.GoIndex, Object: sub(x.X, typ), Args: []facts.GoExpr{astReduce(x.Index, depth+1, typ)}}
	case *ast.IndexListExpr:
		out := facts.GoExpr{Kind: facts.GoIndex, Object: sub(x.X, typ)}
		for _, a := range x.Indices {
			out.Args = append(out.Args, astReduce(a, depth+1, true))
		}
		return out
	case *ast.ArrayType:
		if x.Len == nil {
			return facts.GoExpr{Kind: facts.GoSlice, Object: sub(x.Elt, true)}
		}
		length := "..."
		if l, ok := x.Len.(*ast.BasicLit); ok {
			length = l.Value
		} else if _, ok := x.Len.(*ast.Ellipsis); !ok {
			length = "?"
		}
		return facts.GoExpr{Kind: facts.GoArray, Value: length, Object: sub(x.Elt, true)}
	case *ast.MapType:
		return facts.GoExpr{Kind: facts.GoMap, Left: sub(x.Key, true), Right: sub(x.Value, true)}
	case *ast.ChanType:
		return facts.GoExpr{Kind: facts.GoChan}
	case *ast.FuncType:
		return facts.GoExpr{Kind: facts.GoFuncType}
	case *ast.InterfaceType:
		if x.Methods == nil || len(x.Methods.List) == 0 {
			// interface{}: the same as any for every reader.
			return facts.GoExpr{Kind: facts.GoInterface}
		}
		return facts.GoExpr{Kind: facts.GoInterface}
	case *ast.StructType:
		return facts.GoExpr{Kind: facts.GoStructType}
	case *ast.FuncLit:
		return facts.GoExpr{Kind: facts.GoFuncLit}
	case *ast.Ellipsis:
		return facts.GoExpr{Kind: facts.GoSlice, Object: sub(x.Elt, true)}
	}
	return facts.GoExpr{Kind: facts.GoUnknown}
}

// shapeTestFile reports a _test.go file, which carries no shape payload.
func shapeTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }
