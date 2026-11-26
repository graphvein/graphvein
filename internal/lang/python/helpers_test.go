package python

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

func TestHelperSignatureAndRebinding(t *testing.T) {
	for _, body := range []string{
		"target = object()", "target += other", "target: object = other", "target: object",
		"for target in values:\n        pass", "with resource() as target:\n        pass",
		"if (target := other):\n        pass", "del target", "global target", "nonlocal target",
	} {
		t.Run(body, func(t *testing.T) {
			files, _ := syntaxFiles(t, map[string]string{"main.py": "def helper(target):\n    " + body + "\n    target.include_router(router)\n"})
			h := files["main.py"].Python.Scopes[1].Helper
			if h == nil || len(h.Parameters) != 1 || !h.Parameters[0].Blocked {
				t.Fatalf("unsafe signature: %+v", h)
			}
		})
	}
	files, _ := syntaxFiles(t, map[string]string{"main.py": "def helper(a, /, b=None, *args, c, d=1, **kwargs):\n    a.include_router(b)\n"})
	h := files["main.py"].Python.Scopes[1].Helper
	want := []facts.PythonParameter{{Name: "a", Kind: "posonly"}, {Name: "b", Kind: "positional", Default: true}, {Name: "args", Kind: "varargs"}, {Name: "c", Kind: "kwonly"}, {Name: "d", Kind: "kwonly", Default: true}, {Name: "kwargs", Kind: "kwargs"}}
	if h == nil || !reflect.DeepEqual(h.Parameters, want) {
		t.Fatalf("signature: %+v want %+v", h, want)
	}
}

func TestHelperArgumentBinding(t *testing.T) {
	arg := facts.RouteExpr{Kind: "identifier", Name: "app"}
	param := func(name, kind string, optional, blocked bool) facts.PythonParameter {
		return facts.PythonParameter{Name: name, Kind: kind, Default: optional, Blocked: blocked}
	}
	signature := []facts.PythonParameter{param("a", "posonly", false, false), param("b", "positional", true, false), param("c", "kwonly", false, false)}
	kw := func(name string) facts.RouteKeyword { return facts.RouteKeyword{Name: name, Value: arg} }
	for _, test := range []struct {
		name       string
		parameters []facts.PythonParameter
		call       facts.RouteExpr
		valid      bool
		bound      int
	}{
		{"mixed", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}, Keywords: []facts.RouteKeyword{kw("c")}}, true, 2},
		{"keywords", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}, Keywords: []facts.RouteKeyword{kw("b"), kw("c")}}, true, 3},
		{"posonly keyword", signature, facts.RouteExpr{Keywords: []facts.RouteKeyword{kw("a"), kw("c")}}, false, 0},
		{"missing kwonly", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}}, false, 0},
		{"too many", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg, arg, arg}, Keywords: []facts.RouteKeyword{kw("c")}}, false, 0},
		{"unknown keyword", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}, Keywords: []facts.RouteKeyword{kw("c"), kw("d")}}, false, 0},
		{"duplicate positional keyword", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg, arg}, Keywords: []facts.RouteKeyword{kw("b"), kw("c")}}, false, 0},
		{"duplicate keyword", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}, Keywords: []facts.RouteKeyword{kw("c"), kw("c")}}, false, 0},
		{"star args", signature, facts.RouteExpr{Args: []facts.RouteExpr{{Kind: "list_splat"}}, Keywords: []facts.RouteKeyword{kw("c")}}, false, 0},
		{"star kwargs", signature, facts.RouteExpr{Args: []facts.RouteExpr{arg}, Keywords: []facts.RouteKeyword{{Value: facts.RouteExpr{Kind: "dictionary_splat"}}}}, false, 0},
		{"default omitted", []facts.PythonParameter{param("a", "positional", true, false)}, facts.RouteExpr{}, true, 0},
		{"rebound", []facts.PythonParameter{param("a", "positional", false, true)}, facts.RouteExpr{Args: []facts.RouteExpr{arg}}, true, 0},
		{"variadics", []facts.PythonParameter{param("a", "posonly", false, false), param("args", "varargs", false, false), param("kwargs", "kwargs", false, false)}, facts.RouteExpr{Args: []facts.RouteExpr{arg, arg}, Keywords: []facts.RouteKeyword{kw("a"), kw("unknown")}}, true, 1},
		{"posonly missing despite kwargs", []facts.PythonParameter{param("a", "posonly", false, false), param("kwargs", "kwargs", false, false)}, facts.RouteExpr{Keywords: []facts.RouteKeyword{kw("a")}}, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			bound, valid := BindHelperArguments(test.parameters, test.call)
			if valid != test.valid || len(bound) != test.bound {
				t.Fatalf("bound=%v valid=%v, want count=%d valid=%v", bound, valid, test.bound, test.valid)
			}
		})
	}
}
