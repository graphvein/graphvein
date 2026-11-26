// Package clients recognizes source-backed HTTP calls and rebuilds route links.
package clients

import (
	"fmt"
	pathpkg "path"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

type expr = facts.RouteExpr
type object struct {
	kind, key, file, name string
	scope                 int
	base                  *expr
	returns               []expr
}
type resolver struct {
	heyClients        map[string]*heyClient
	angularBases      map[string]angularBase
	angularTouches    bool
	angularOpaque     bool
	overrides         map[string]bool
	overrideLocations map[string][]contract.Evidence
	files             map[string]facts.Facts
	modules           *typescript.ModuleResolver
	defaults          map[string][]baseAssignment
	memo              map[string]memoValue
	// ambient holds the build-time constants of the repository: names
	// declared only by `declare const NAME: string` in declaration files
	// (built on first use; docs/extract/clients.md "Build-time constant
	// bases").
	ambient map[string]bool
}

// ambientConstant reports whether name is a build-time constant: a global
// `declare const name: string` in a scanned declaration file, with no
// value definition anywhere in the repository (facts.RouteFacts
// GlobalDefinitions). Its value comes from outside the source, typically a
// bundler define, so it can only be an unresolved base.
func (r *resolver) ambientConstant(name string) bool {
	if r.ambient == nil {
		r.ambient = map[string]bool{}
		defined := map[string]bool{}
		for _, f := range r.files {
			if f.Routes == nil {
				continue
			}
			for _, n := range f.Routes.AmbientStrings {
				r.ambient[n] = true
			}
			for _, n := range f.Routes.GlobalDefinitions {
				defined[n] = true
			}
		}
		for n := range defined {
			delete(r.ambient, n)
		}
	}
	return r.ambient[name]
}

// autoImport returns the declaration file and module of the generated
// auto-import declaration that binds name for a module in file (row UP-41,
// facts.RouteFacts AutoImports): the declaration file's directory must
// hold file, and exactly one such declaration may bind the name. A
// declaration file never uses one.
func (r *resolver) autoImport(file, name string) (string, string, bool) {
	if strings.HasSuffix(file, ".d.ts") || strings.HasSuffix(file, ".d.mts") || strings.HasSuffix(file, ".d.cts") {
		return "", "", false
	}
	declaration, module, count := "", "", 0
	for path, f := range r.files {
		if f.Routes == nil {
			continue
		}
		dir := pathpkg.Dir(path)
		if dir != "." && !strings.HasPrefix(file, dir+"/") {
			continue
		}
		for _, a := range f.Routes.AutoImports {
			if a.Name == name {
				declaration, module = path, a.Module
				count++
			}
		}
	}
	return declaration, module, count == 1
}

type baseAssignment struct{ value, kind, expression string }

func (r *resolver) freeze(file string, scope int, e expr) expr {
	if s, ok := r.literal(file, scope, e, nil); ok {
		return expr{Kind: "literal", Name: s, Text: e.Text}
	}
	e.Parts = append([]expr(nil), e.Parts...)
	for i := range e.Parts {
		e.Parts[i] = r.freeze(file, scope, e.Parts[i])
	}
	return e
}

func (r *resolver) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
	d := r.files[file].Routes
	if d != nil {
		for scope >= 0 && scope < len(d.Scopes) {
			if b, ok := d.Scopes[scope].Bindings[name]; ok {
				return b, scope, true
			}
			scope = d.Scopes[scope].Parent
		}
	}
	return facts.RouteBinding{}, 0, false
}

func (r *resolver) imported(b typescript.ExportBinding, depth int, seen map[string]bool) object {
	if b.Ambiguous {
		return object{}
	}
	if b.External {
		return object{kind: "external", file: b.File, name: b.Name}
	}
	if b.Namespace {
		return object{kind: "namespace", file: b.File}
	}
	if b.File != "" && b.Name != "" {
		return r.resolve(b.File, 0, expr{Kind: "identifier", Name: b.Name}, depth, seen)
	}
	return object{}
}

func (r *resolver) resolve(file string, scope int, e expr, depth int, seen map[string]bool) object {
	if seen == nil {
		seen = map[string]bool{}
	}
	key := fmt.Sprintf("%s:%d:%s:%s:%d", file, scope, e.Kind, e.Name, e.Offset)
	if seen[key] {
		return object{}
	}
	seen[key] = true
	defer delete(seen, key)
	switch e.Kind {
	case "identifier":
		b, at, ok := r.binding(file, scope, e.Name)
		if !ok {
			return object{}
		}
		if b.Module != "" {
			return r.imported(r.modules.ImportBinding(file, b.Module, b.Export, ""), depth, seen)
		}
		if at == 0 && r.heyClients[file+"#"+e.Name] != nil {
			return object{kind: "hey-api", key: file + "#" + e.Name, file: file, name: e.Name}
		}
		if b.Value.Kind == "function" {
			return object{kind: "function", file: file, scope: b.ReturnScope, returns: b.Returns}
		}
		return r.resolve(file, at, b.Value, depth, seen)
	case "member_expression":
		if e.Object == nil {
			return object{}
		}
		v := r.resolve(file, scope, *e.Object, depth, seen)
		if v.kind == "namespace" {
			return r.imported(r.modules.Export(v.file, e.Name), depth, seen)
		}
		if v.kind == "external" {
			if v.name == "*" {
				v.name = e.Name
			} else {
				v.name += "." + e.Name
			}
			return v
		}
	case "call_expression":
		if e.Object == nil {
			return object{}
		}
		if e.Object.Kind == "identifier" && e.Object.Name == "require" && len(e.Args) == 1 && e.Args[0].Kind == "literal" {
			if _, _, shadow := r.binding(file, scope, "require"); !shadow {
				return r.imported(r.modules.ImportBinding(file, e.Args[0].Name, "default", ""), depth, seen)
			}
		}
		callee := *e.Object
		if callee.Kind == "member_expression" && callee.Name == "create" && callee.Object != nil {
			v := r.resolve(file, scope, *callee.Object, depth, seen)
			if isAxios(v) {
				v = object{kind: "axios", key: fmt.Sprintf("%s:%d", file, e.Offset), file: file, scope: scope}
				if len(e.Args) > 0 {
					if b, ok := property(e.Args[0], "baseURL"); ok {
						v.base = &b
					}
				}
				return v
			}
		}
		v := r.resolve(file, scope, callee, depth, seen)
		if v.kind == "function" && depth == 0 && len(v.returns) > 0 {
			var result object
			for i, e := range v.returns {
				next := r.resolve(v.file, v.scope, e, depth+1, seen)
				if !isAxios(next) || i > 0 && next.key != result.key {
					return object{}
				}
				result = next
			}
			return result
		}
	}
	return object{}
}

func isAxios(v object) bool {
	return v.kind == "axios" || v.kind == "external" && v.file == "axios" && v.name == "default"
}
func objectKey(v object) string {
	if v.kind == "external" {
		return "axios:default"
	}
	return v.key
}
func property(e expr, name string) (expr, bool) {
	if e.Kind != "object" {
		return expr{}, false
	}
	v, ok := e.Properties[name]
	return v, ok
}

// literal follows only module-level constant literals through the shared resolver.
func (r *resolver) literal(file string, scope int, e expr, seen map[string]bool) (string, bool) {
	if e.Kind == "literal" {
		return e.Name, true
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	key := fmt.Sprintf("%s:%d:%s:%s", file, scope, e.Kind, e.Name)
	if seen[key] {
		return "", false
	}
	seen[key] = true
	defer delete(seen, key)
	if e.Kind == "identifier" {
		b, at, ok := r.binding(file, scope, e.Name)
		if !ok {
			return "", false
		}
		if b.Module != "" {
			return r.importLiteral(r.modules.ImportBinding(file, b.Module, b.Export, ""), seen)
		}
		if at == 0 && b.Constant && b.Value.Kind == "literal" {
			return b.Value.Name, true
		}
	}
	if e.Kind == "member_expression" && e.Object != nil && e.Object.Kind == "identifier" {
		b, _, ok := r.binding(file, scope, e.Object.Name)
		if ok && b.Module != "" {
			return r.importLiteral(r.modules.ImportBinding(file, b.Module, b.Export, e.Name), seen)
		}
	}
	return "", false
}
func (r *resolver) importLiteral(b typescript.ExportBinding, seen map[string]bool) (string, bool) {
	if b.External || b.Ambiguous || b.Namespace || b.File == "" {
		return "", false
	}
	return r.literal(b.File, 0, expr{Kind: "identifier", Name: b.Name}, seen)
}

func envMember(e expr) bool {
	if e.Kind != "member_expression" || e.Object == nil {
		return false
	}
	s := compact(e.Text)
	if strings.HasPrefix(s, "import.meta.env.") || strings.HasPrefix(s, "process.env.") {
		return true
	}
	return e.Object.Kind == "member_expression" && e.Object.Name == "env" && e.Object.Object != nil && (e.Object.Object.Kind == "identifier" && e.Object.Object.Name == "process" || e.Object.Object.Kind == "meta_property" && compact(e.Object.Object.Text) == "import.meta")
}
func envBase(e expr) bool {
	if e.Kind == "environment_base" {
		return true
	}
	if envMember(e) {
		return true
	}
	return e.Kind == "binary_expression" && (e.Operator == "??" || e.Operator == "||") && len(e.Parts) == 2 && envMember(e.Parts[0]) && e.Parts[1].Kind == "literal"
}
func (r *resolver) baseShaped(file string, scope int, e expr) bool {
	if envBase(e) {
		return true
	}
	if e.Kind != "identifier" {
		return false
	}
	b, at, ok := r.binding(file, scope, e.Name)
	if !ok {
		// An unbound name is a base only as a build-time constant.
		return r.ambientConstant(e.Name)
	}
	if b.Module != "" {
		v := r.modules.ImportBinding(file, b.Module, b.Export, "")
		if v.External || v.Ambiguous || v.File == "" {
			return false
		}
		b, at, ok = r.binding(v.File, 0, v.Name)
		file, e.Name = v.File, v.Name
	}
	if ok && at == 0 && b.ModuleConst {
		if envBase(b.Value) {
			return true
		}
		// Rich client syntax has already removed transparent assertions.
		v, _, found, _ := newEvaluation(r).binding(evaluationContext{file: file}, e.Name)
		return found && v.Evaluation != nil && envBase(v.Evaluation.Value)
	}
	return false
}

// compact preserves literal text while removing trivia from template substitutions too.
func compact(s string) string {
	chars := []rune(s)
	var out strings.Builder
	at := 0
	var code func(bool)
	var literal func(rune)
	literal = func(quote rune) {
		out.WriteRune(quote)
		at++
		for at < len(chars) {
			c := chars[at]
			out.WriteRune(c)
			at++
			if c == '\\' && at < len(chars) {
				out.WriteRune(chars[at])
				at++
				continue
			}
			if c == quote {
				return
			}
			if quote == '`' && c == '$' && at < len(chars) && chars[at] == '{' {
				out.WriteRune('{')
				at++
				code(true)
			}
		}
	}
	code = func(substitution bool) {
		depth := 0
		for at < len(chars) {
			c := chars[at]
			if c == '\'' || c == '"' || c == '`' {
				literal(c)
				continue
			}
			if c == '/' && at+1 < len(chars) && chars[at+1] == '/' {
				for at < len(chars) && chars[at] != '\n' {
					at++
				}
				continue
			}
			if c == '/' && at+1 < len(chars) && chars[at+1] == '*' {
				at += 2
				for at+1 < len(chars) && !(chars[at] == '*' && chars[at+1] == '/') {
					at++
				}
				if at+1 < len(chars) {
					at += 2
				}
				continue
			}
			at++
			if c == '{' {
				depth++
			}
			if c == '}' {
				if substitution && depth == 0 {
					out.WriteRune(c)
					return
				}
				depth--
			}
			if !unicode.IsSpace(c) {
				out.WriteRune(c)
			}
		}
	}
	code(false)
	return out.String()
}
