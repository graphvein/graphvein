package routes

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/testscope"
)

type helperInstance struct {
	file, context string
	scope         int
	bindings      map[string]pythonObject
	conditional   bool
	scopeLabel    testscope.Scope
}

func helperKey(file string, scope int) string { return fmt.Sprintf("%s:%d", file, scope) }

func (r *fastapiResolver) helperInstances(files map[string]facts.Facts) map[string][]helperInstance {
	instances := map[string][]helperInstance{}
	for _, file := range facts.Paths(files) {
		p := files[file].Python
		if p == nil {
			continue
		}
		for _, c := range p.HelperCalls {
			e := c.Expression
			if e.Object == nil {
				continue
			}
			b := r.modules.Callable(file, c.Scope, facts.CompactPython(e.Object.Text))
			if b.External || b.Scope != 0 || b.Syntax.Count != 1 {
				continue
			}
			data := files[b.File].Python
			at := b.Syntax.ClassScope
			if data == nil || at <= 0 || at >= len(data.Scopes) {
				continue
			}
			h := data.Scopes[at].Helper
			if h == nil || h.Identity != b.Identity || h.Async && !c.Awaited {
				continue
			}
			arguments, valid := python.BindHelperArguments(h.Parameters, e)
			if !valid {
				continue
			}
			instance := helperInstance{file: b.File, scope: at, context: fmt.Sprintf("%s:%d", file, e.Offset), bindings: map[string]pythonObject{}, conditional: c.Conditional}
			instance.scopeLabel = chainScope(testscope.Classify(file, r.rules), testscope.Classify(b.File, r.rules))
			for _, param := range h.Parameters {
				a, supplied := arguments[param.Name]
				if !supplied {
					continue
				}
				if enclosingParameter(p, c.Scope, a) {
					continue
				}
				// No active helper context exists here. An enclosing parameter
				// therefore remains unresolved, enforcing the one-level bound.
				o := r.object(file, c.Scope, a, map[string]bool{})
				if o.kind != "" {
					instance.bindings[param.Name] = o
				}
			}
			if len(instance.bindings) > 0 {
				key := helperKey(b.File, at)
				instances[key] = append(instances[key], instance)
			}
		}
	}
	return instances
}

func enclosingParameter(p *facts.PythonFacts, scope int, e facts.RouteExpr) bool {
	if e.Kind != "identifier" {
		return false
	}
	for scope > 0 {
		s := p.Scopes[scope]
		for _, name := range s.Parameters {
			if name == e.Name {
				return true
			}
		}
		if _, local := s.Bindings[e.Name]; local {
			return false
		}
		scope = s.Parent
	}
	return false
}

func helperParameterMount(p *facts.PythonFacts, route facts.PythonRoute) bool {
	c := route.Call
	if c.Method != "include_router" && c.Method != "mount" {
		return false
	}
	h := p.Scopes[c.Scope].Helper
	if h == nil {
		return false
	}
	target, _ := argument(route.Expression, "router", 0)
	if c.Method == "mount" {
		target, _ = argument(route.Expression, "app", 1)
	}
	for _, param := range h.Parameters {
		if c.Receiver.Kind == "identifier" && c.Receiver.Name == param.Name || target.Kind == "identifier" && target.Name == param.Name {
			return true
		}
	}
	return false
}

type fastapiPathEntry struct {
	pathEntry
	TrailingSlash bool `json:"trailing_slash,omitempty"`
}

type fastapiPathKey struct {
	template string
	slash    bool
}
