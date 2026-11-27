package facts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fill sets every exported field reachable from v to a distinct non-zero
// value (slices and maps with two elements), down to depth levels of
// RouteExpr nesting and of recursive types such as URLStatement, so a
// field the cache layout forgot shows up as a difference.
func fill(v reflect.Value, depth int, n *int) {
	fillLevel(v, depth, 0, n)
}

func fillLevel(v reflect.Value, depth, level int, n *int) {
	*n++
	if level > 12 {
		return
	}
	fill := func(v reflect.Value, depth int, n *int) { fillLevel(v, depth, level+1, n) }
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", *n))
	case reflect.Int, reflect.Int64, reflect.Int32:
		if v.Type() == reflect.TypeFor[int32]() {
			return // RouteExpr.Ref, zero outside the layout
		}
		v.SetInt(int64(*n))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Pointer:
		if v.Type().Elem() == routeExprType && depth == 0 {
			return
		}
		p := reflect.New(v.Type().Elem())
		fill(p.Elem(), depth, n)
		v.Set(p)
	case reflect.Struct:
		if v.Type() == routeExprType {
			if depth < 0 {
				return
			}
			depth--
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), depth, n)
			}
		}
	case reflect.Slice:
		if v.Type().Elem() == routeExprType && depth == 0 {
			return
		}
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			fill(s.Index(i), depth, n)
		}
		v.Set(s)
	case reflect.Map:
		if v.Type().Elem() == routeExprType && depth == 0 {
			return
		}
		m := reflect.MakeMap(v.Type())
		for range 2 {
			k := reflect.New(v.Type().Key()).Elem()
			fill(k, depth, n)
			e := reflect.New(v.Type().Elem()).Elem()
			fill(e, depth, n)
			m.SetMapIndex(k, e)
		}
		v.Set(m)
	}
}

func plainRoundTrip(t *testing.T, r *RouteFacts) routeFactsPlain {
	t.Helper()
	data, err := json.Marshal((*routeFactsPlain)(r))
	if err != nil {
		t.Fatal(err)
	}
	var out routeFactsPlain
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func layoutRoundTrip(t *testing.T, r *RouteFacts) (routeFactsPlain, []byte) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out RouteFacts
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	out.SetSource("", 0)
	return routeFactsPlain(out), data
}

// TestRouteFactsLayoutRoundTrip checks that the cache layout decodes every
// field of RouteFacts as the plain JSON layout does, including shared
// subexpressions, Text stored as source offsets, and empty versus nil.
func TestRouteFactsLayoutRoundTrip(t *testing.T) {
	var r RouteFacts
	n := 0
	fill(reflect.ValueOf(&r).Elem(), 2, &n)
	if got, _ := layoutRoundTrip(t, &r); !reflect.DeepEqual(got, plainRoundTrip(t, &r)) {
		t.Fatal("filled RouteFacts decode differently from the plain layout")
	}

	// Shared subexpressions, Text cut from the source (and not), and the
	// empty forms the plain layout keeps or drops.
	source := "  call(inner(x), {k: inner(x)})"
	x := RouteExpr{Kind: "identifier", Name: "x", Text: source[14:15], Offset: 14}
	inner := RouteExpr{Kind: "call_expression", Text: source[8:16], Offset: 8, Object: &RouteExpr{Kind: "identifier", Name: "inner", Text: source[8:13], Offset: 8}, Args: []RouteExpr{x}}
	outer := RouteExpr{Kind: "call_expression", Text: source[2:], Offset: 2, Args: []RouteExpr{inner, {Kind: "object", Properties: map[string]RouteExpr{"k": inner}, Keys: []string{}}}}
	empty := RouteExpr{Kind: "call_expression", Args: []RouteExpr{}, Parts: []RouteExpr{}, Keywords: []RouteKeyword{}, Properties: map[string]RouteExpr{}}
	moved := RouteExpr{Kind: "identifier", Text: "(x)", Offset: 14}
	r = RouteFacts{
		Scopes:  []RouteScope{{Parent: -1, Bindings: map[string]RouteBinding{"a": {Value: inner}, "b": {Value: empty}, "c": {Value: moved}}}},
		Clients: []ClientCall{{Expression: outer}, {Expression: inner}},
	}
	r.SetSource(source, 0)
	got, data := layoutRoundTrip(t, &r)
	if !reflect.DeepEqual(got, plainRoundTrip(t, &r)) {
		t.Fatalf("shared expressions decode differently from the plain layout:\n%s", data)
	}
	if strings.Count(string(data), `"k":"call_expression"`) != 3 {
		t.Errorf("want one table entry per distinct call (outer, inner, empty):\n%s", data)
	}
	if !strings.Contains(string(data), `"t":"(x)"`) || strings.Contains(string(data), `"t":"inner(x)"`) {
		t.Errorf("want only Text values not at their offset written out:\n%s", data)
	}
	// Decoded, the shared call is one value.
	var decoded RouteFacts
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Clients[0].Expression.Args[0].Object != decoded.Clients[1].Expression.Object {
		t.Error("decoded nested call is not shared with its own client record")
	}
}

// TestRouteExprFieldsAreInTheLayout fails when RouteExpr gains a field:
// exprEntry, intern and decodeExprTable must carry it too.
func TestRouteExprFieldsAreInTheLayout(t *testing.T) {
	var names []string
	for i := range routeExprType.NumField() {
		names = append(names, routeExprType.Field(i).Name)
	}
	want := "Kind Text Name Offset Object Args Keywords Parts Properties Operator Keys Invalid Spread Ref Span"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("RouteExpr fields are %q, the cache layout covers %q: extend exprEntry", got, want)
	}
}

// TestRouteFactsLayoutRejectsBadRecords: a record of another layout or with
// a broken reference fails to decode (a cache miss), never decodes to
// partial facts.
func TestRouteFactsLayoutRejectsBadRecords(t *testing.T) {
	for _, data := range []string{
		`{"Scopes":[],"Exports":{}}`,
		`{"layout":2,"facts":{}}`,
		`{"layout":1,"exprs":[{"b":1}],"facts":{}}`,
		`{"layout":1,"exprs":[{"k":"x"}],"facts":{"Clients":[{"Expression":{"Ref":2}}]}}`,
		`{"layout":1,"source":"ab","exprs":[{"o":1,"l":5}],"facts":{}}`,
	} {
		var r RouteFacts
		if err := json.Unmarshal([]byte(data), &r); err == nil {
			t.Errorf("%s: decoded without error", data)
		}
	}
}
