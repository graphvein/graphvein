package clients

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

func TestECMAScriptValuesAndKeyOrder(t *testing.T) {
	for _, tc := range []struct{ kind, text, want string }{{"undefined", "undefined", "undefined"}, {"null", "null", "null"}, {"true", "true", "true"}, {"false", "false", "false"}, {"number", "-0", "0"}, {"number", "1e21", "1e+21"}, {"number", "9007199254740993", "9007199254740992"}, {"number", "0x10", "16"}} {
		ev := newEvaluation(&resolver{})
		v := ev.expression(evaluationContext{}, expr{Kind: tc.kind, Name: tc.text})
		ps := v[0].stringify()
		if len(ps) != 1 || ps[0].text != tc.want {
			t.Errorf("%s %s: %v, want %s", tc.kind, tc.text, ps, tc.want)
		}
	}
	v := urlValue{keys: []string{"4294967295", "01", "10", "2", "0", "z", "-1"}}
	if got := strings.Join(ownKeys(v), ","); got != "0,2,10,4294967295,01,z,-1" {
		t.Fatal(got)
	}
	for _, number := range []string{"1.5", "NaN", "Infinity"} {
		ev := newEvaluation(&resolver{})
		if v := ev.expression(evaluationContext{}, expr{Kind: "number", Name: number}); v[0].kind != "dynamic" {
			t.Fatal(number, v)
		}
	}
}

func TestFrozenFetchMethodKeepsUndefinedScope(t *testing.T) {
	d := &facts.RouteFacts{URLScopes: []facts.RouteScope{
		{Parent: -1},
		{Parent: 0, Bindings: map[string]facts.RouteBinding{"undefined": {Value: expr{Kind: "parameter"}}}},
	}}
	r := &resolver{files: map[string]facts.Facts{"a.ts": {Routes: d}}}
	config := expr{Kind: "object", Keywords: []facts.RouteKeyword{{Name: "method", Value: expr{Kind: "undefined", Name: "undefined", Text: "undefined"}}}}
	frozen := r.freezeFetchConfig("a.ts", 0, config, nil)
	if method := r.fetchMethod("a.ts", 1, frozen); method.Kind != "literal" || method.Name != "GET" {
		t.Fatal("caller captured proven undefined", method)
	}
	frozen = r.freezeFetchConfig("a.ts", 1, config, nil)
	if method := r.fetchMethod("a.ts", 0, frozen); method.Kind != "dynamic" {
		t.Fatal("unknown source binding became global undefined", method)
	}
}

func TestWrapperUndefinedDefaultIsFetchOnly(t *testing.T) {
	for _, library := range []string{"fetch", "axios", "angular"} {
		w := wrapper{
			request: request{library: library, method: expr{Kind: "identifier", Name: "method"}},
			params:  []facts.ClientParameter{{Name: "method", Default: expr{Kind: "literal", Name: "POST"}}},
		}
		q := w.apply([]expr{{Kind: "undefined", Text: "undefined"}}, "wrapper")
		method, known := (&resolver{}).literal("", 0, q.method, nil)
		if library == "fetch" {
			if !known || method != "POST" {
				t.Fatal("fetch did not apply its undefined default", q.method)
			}
		} else if known {
			t.Fatal("fetch-only default changed another transport", library, method)
		}
	}
}

func TestFetchDefaultBinding(t *testing.T) {
	post := expr{Kind: "object", Keys: []string{"method"}, Properties: map[string]expr{"method": {Kind: "literal", Name: "POST"}}}
	d := &facts.RouteFacts{URLScopes: []facts.RouteScope{{Parent: -1, Bindings: map[string]facts.RouteBinding{
		"POST":   {Evaluation: &facts.URLBinding{Constant: true, Single: true, Value: post}},
		"OPAQUE": {Evaluation: &facts.URLBinding{Constant: true, Single: true, Value: expr{Kind: "call_expression"}}},
		"NIL":    {Evaluation: &facts.URLBinding{Constant: true, Single: true, Value: expr{Kind: "null"}}},
		"VOID":   {Evaluation: &facts.URLBinding{Constant: true, Single: true, Value: expr{Kind: "undefined", Name: "undefined"}}},
		"CHOICE": {Evaluation: &facts.URLBinding{Constant: true, Single: true, Value: expr{Kind: "ternary_expression", Parts: []expr{{Kind: "identifier", Name: "condition"}, post, {Kind: "null"}}}}},
	}}}}
	r := &resolver{files: map[string]facts.Facts{"a.ts": {Routes: d}}}
	c := evaluationContext{file: "a.ts", env: &environment{values: map[string]localValue{
		"earlier": {values: valueSet{unknownValue(expr{Kind: "identifier", Name: "earlier"})}},
	}}}
	for _, tc := range []struct {
		name  string
		value expr
		want  string
	}{
		{"absent", expr{}, "GET"},
		{"object", post, "POST"},
		{"const object", expr{Kind: "identifier", Name: "POST"}, "POST"},
		{"null", expr{Kind: "null"}, "GET"},
		{"const null", expr{Kind: "identifier", Name: "NIL"}, "GET"},
		{"undefined", expr{Kind: "undefined", Name: "undefined"}, "GET"},
		{"const undefined", expr{Kind: "identifier", Name: "VOID"}, "GET"},
		{"call", expr{Kind: "call_expression", Text: "makeOptions()"}, ""},
		{"opaque const", expr{Kind: "identifier", Name: "OPAQUE"}, ""},
		{"unknown const", expr{Kind: "identifier", Name: "MISSING"}, ""},
		{"multi-valued const", expr{Kind: "identifier", Name: "CHOICE"}, ""},
		{"earlier parameter", expr{Kind: "identifier", Name: "earlier"}, ""},
		{"primitive", expr{Kind: "literal", Name: "POST"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frozen := r.freezeFetchDefault(c, tc.value)
			options := expr{Kind: "identifier", Name: "options"}
			spread := facts.RouteKeyword{Value: expr{Kind: "spread_element", Parts: []expr{options}}}
			for _, supplied := range [][]expr{nil, {{Kind: "undefined"}}} {
				w := wrapper{request: request{library: "fetch", config: expr{Kind: "object", Keywords: []facts.RouteKeyword{spread}}}, params: []facts.ClientParameter{{Name: "options", Default: frozen}}}
				q := w.apply(supplied, "inner")
				method := r.fetchMethod("a.ts", 0, q.config)
				if method.Name != tc.want || tc.want == "" && method.Kind != "dynamic" {
					t.Fatalf("default method: %+v, want %q", method, tc.want)
				}
				// An unknown default stays unknown across another wrapper and
				// through an options.method read with a GET fallback.
				forwarded := w.apply([]expr{{Kind: "identifier", Name: "options"}}, "inner")
				w.request.config = forwarded.config
				q = w.apply(nil, "outer")
				if got := r.fetchMethod("a.ts", 0, q.config); got.Name != method.Name || got.Kind != method.Kind {
					t.Fatal("forwarding changed default", got, method)
				}
				if tc.want == "" {
					member := expr{Kind: "member_expression", Object: &options, Name: "method"}
					w.request.config = expr{Kind: "object", Keys: []string{"method"}, Properties: map[string]expr{"method": {Kind: "binary_expression", Operator: "??", Parts: []expr{member, defaultMethod()}}}}
					q = w.apply(supplied, "property")
					if got := r.fetchMethod("a.ts", 0, q.config); got.Kind != "dynamic" {
						t.Fatal("options.method default became known", got)
					}
				}
				w.request.config = expr{Kind: "object", Keywords: []facts.RouteKeyword{spread, {Name: "method", Value: expr{Kind: "literal", Name: "PATCH"}}}}
				q = w.apply(supplied, "override")
				if got := r.fetchMethod("a.ts", 0, q.config); got.Name != "PATCH" {
					t.Fatal("later own method did not override default", got)
				}
			}
		})
	}
}

func TestMemoKeyPreservesLoopSemantics(t *testing.T) {
	one := urlValue{kind: "object", keys: []string{"z", "a"}, properties: map[string]urlValue{"z": stringValue("z"), "a": stringValue("a")}, literalObject: true}
	key := func(v urlValue) string {
		return memoKey("helper", &environment{values: map[string]localValue{"o": {values: valueSet{v}}}})
	}
	a := key(one)
	two := one
	two.keys = []string{"a", "z"}
	if a == key(two) {
		t.Fatal("insertion order lost")
	}
	two = one
	two.literalObject = false
	if a == key(two) {
		t.Fatal("literal argument provenance lost")
	}
	two = one
	two.conditional = true
	if a == key(two) {
		t.Fatal("conditional alternatives lost")
	}
}

func TestBudgetAndRecursionStayDynamic(t *testing.T) {
	body := []facts.URLStatement{{Kind: "declaration", Name: "url", Expression: expr{Kind: "literal", Name: "/api"}}}
	for i := 0; i < 2001; i++ {
		body = append(body, facts.URLStatement{Kind: "declaration", Name: strings.Repeat("x", i+1), Expression: expr{Kind: "literal", Name: ""}})
	}
	body = append(body, facts.URLStatement{Kind: "return_statement", Expression: expr{Kind: "identifier", Name: "url"}})
	h := &facts.URLHelper{Identity: "function:a.ts#bounded", Eligible: true, Scope: 1, Body: body}
	d := &facts.RouteFacts{URLScopes: []facts.RouteScope{{Parent: -1, Bindings: map[string]facts.RouteBinding{"bounded": {Evaluation: &facts.URLBinding{Helper: h}}}}, {Parent: 0, Bindings: map[string]facts.RouteBinding{}}}}
	r := &resolver{files: map[string]facts.Facts{"a.ts": {Routes: d}}, memo: map[string]memoValue{}}
	call := expr{Kind: "call_expression", Object: &expr{Kind: "identifier", Name: "bounded"}}
	ev := newEvaluation(r)
	v := ev.expression(evaluationContext{file: "a.ts"}, call)
	if !ev.exhausted || v[0].kind != "dynamic" {
		t.Fatal("step budget", ev.steps, v)
	}
	h.Body = []facts.URLStatement{{Kind: "return_statement", Expression: call}}
	ev = newEvaluation(r)
	v = ev.expression(evaluationContext{file: "a.ts"}, call)
	if v[0].kind != "dynamic" || ev.steps > 20 {
		t.Fatal("recursion", ev.steps, v)
	}
}

func TestOpaquePrefixCannotBecomeParameter(t *testing.T) {
	r := &resolver{files: map[string]facts.Facts{}}
	ev := newEvaluation(r)
	e := expr{Kind: "template_string", Parts: []expr{{Kind: "identifier", Name: "url"}, {Kind: "literal", Name: "/items"}}}
	v := ev.expression(evaluationContext{}, e)[0]
	a := r.attributes("a.ts", 0, request{library: "fetch", url: valueExpr(v, e)}, "GET", true, map[string]string{".": "/api"})
	if a["path_template"] != nil || a["base"] != "unresolved" {
		t.Fatal(a)
	}
}

func TestLocalConstHelperDeclarationOrder(t *testing.T) {
	h := &facts.URLHelper{Identity: "function:a.ts#url", Eligible: true, BoundConst: true, Scope: 2, Body: []facts.URLStatement{{Kind: "return_statement", Expression: expr{Kind: "literal", Name: "/api"}}}}
	b := &facts.URLBinding{Helper: h, Constant: true, Single: true, Offset: 20}
	d := &facts.RouteFacts{URLScopes: []facts.RouteScope{{Parent: -1}, {Parent: 0, Function: true, Bindings: map[string]facts.RouteBinding{"url": {Evaluation: b}}}, {Parent: 1, Function: true}}}
	r := &resolver{files: map[string]facts.Facts{"a.ts": {Routes: d}}, memo: map[string]memoValue{}}
	for _, tc := range []struct {
		offset, scope int
		known         bool
	}{{10, 1, false}, {30, 1, true}, {10, 2, true}} {
		call := expr{Kind: "call_expression", Object: &expr{Kind: "identifier", Name: "url", Offset: tc.offset}}
		v := newEvaluation(r).expression(evaluationContext{file: "a.ts", scope: tc.scope}, call)
		if v[0].known() != tc.known {
			t.Fatalf("offset=%d scope=%d: %v", tc.offset, tc.scope, v)
		}
	}
	h.BoundConst = false // Function declarations are initialized on scope entry.
	call := expr{Kind: "call_expression", Object: &expr{Kind: "identifier", Name: "url", Offset: 10}}
	if v := newEvaluation(r).expression(evaluationContext{file: "a.ts", scope: 1}, call); !v[0].known() {
		t.Fatal(v)
	}
}

func TestObjectConversionHooksStayUnknown(t *testing.T) {
	for _, name := range []string{"toString", "valueOf"} {
		v := urlValue{kind: "object", properties: map[string]urlValue{name: stringValue("override")}}
		if allLiteral(v.stringify()) {
			t.Fatal("conversion hook was treated as the default Object conversion", name)
		}
	}
}

func TestLoopObjectBuiltinShadowedByLocal(t *testing.T) {
	c := evaluationContext{env: &environment{values: map[string]localValue{"Object": {values: valueSet{stringValue("shadow")}}}}}
	keys := expr{Kind: "call_expression", Object: &expr{Kind: "member_expression", Name: "keys", Object: &expr{Kind: "identifier", Name: "Object"}}, Args: []expr{{Kind: "object", Properties: map[string]expr{}}}}
	_, _, valid := newEvaluation(&resolver{}).statements(c, []facts.URLStatement{{Kind: "for_in_statement", Operator: "of", Expression: keys}})
	if valid {
		t.Fatal("shadowed Object.keys was evaluated as the builtin")
	}
}
