package facts

import (
	"cmp"
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/contract"
)

// The fact cache layout of RouteFacts (T069).
//
// RouteFacts hold an expression tree per call, binding and registration,
// and a nested call's tree is part of every enclosing call's tree. In
// memory the extractor shares such subtrees; written out as plain JSON every
// copy (with its Text) would be serialised again and decoded unshared on a
// warm run, about 32 times the source on Effect-style TypeScript.
//
// So RouteFacts are written as one table of distinct expressions per file,
// each naming its subexpressions by 1-based index (children before
// parents), and the facts themselves with every RouteExpr replaced by a
// reference (a RouteExpr with only Ref set). A Text that is the file's
// source at the expression's Offset is stored as its length, and the
// source text covering all of them is stored once. Decoding builds each
// table entry once and shares it, so a warm run's facts are as linear in
// the file as a fresh run's.
//
// Decoded facts equal what the plain JSON layout decodes to (null and []
// stay apart for Args only; every other empty slice or map decodes as nil,
// because those fields are omitempty), which TestRouteFactsLayoutRoundTrip
// checks on every RouteFacts field.

// routeFactsLayout versions the layout; a cached record of any other value
// fails to decode, so it is read as a cache miss.
const routeFactsLayout = 1

type routeFactsWire struct {
	Layout   int              `json:"layout"`
	Source   string           `json:"source,omitempty"`
	SourceAt int              `json:"source_at,omitempty"`
	Exprs    []exprEntry      `json:"exprs,omitempty"`
	Facts    *routeFactsPlain `json:"facts"`
}

// routeFactsPlain is RouteFacts without its JSON methods.
type routeFactsPlain RouteFacts

// exprEntry is one table entry: a RouteExpr with its subexpressions as
// table references (0: none) and its Text as a source length (TextLen) or
// written out (Text).
type exprEntry struct {
	Kind       string         `json:"k,omitempty"`
	Name       string         `json:"n,omitempty"`
	Offset     int            `json:"o,omitempty"`
	TextLen    int            `json:"l,omitempty"`
	Text       string         `json:"t,omitempty"`
	Object     int            `json:"b,omitempty"`
	Args       []int          `json:"a,omitempty"`
	EmptyArgs  bool           `json:"e,omitempty"`
	Keywords   []keywordEntry `json:"w,omitempty"`
	Parts      []int          `json:"p,omitempty"`
	Properties map[string]int `json:"m,omitempty"`
	Operator   string         `json:"x,omitempty"`
	Keys       []string       `json:"y,omitempty"`
	Invalid    bool           `json:"i,omitempty"`
	Spread     bool           `json:"s,omitempty"`
	Span       *contract.Span `json:"r,omitempty"`
}

type keywordEntry struct {
	Name  string `json:"n,omitempty"`
	Value int    `json:"v"`
}

// MarshalJSON writes the table layout described above.
func (r RouteFacts) MarshalJSON() ([]byte, error) {
	enc := newExprEncoder(r.source, r.sourceAt)
	plain, ok := enc.replace(reflect.ValueOf(routeFactsPlain(r))).Interface().(routeFactsPlain)
	if !ok {
		return nil, fmt.Errorf("encoding route facts: unexpected copy type")
	}
	wire := routeFactsWire{Layout: routeFactsLayout, Exprs: enc.entries, Facts: &plain}
	if enc.hi > enc.lo {
		wire.Source = r.source[enc.lo-r.sourceAt : enc.hi-r.sourceAt]
		wire.SourceAt = enc.lo
	}
	return json.Marshal(wire)
}

// UnmarshalJSON reads the table layout; any other layout is an error.
func (r *RouteFacts) UnmarshalJSON(data []byte) error {
	var wire routeFactsWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Layout != routeFactsLayout || wire.Facts == nil {
		return fmt.Errorf("route facts: unknown cache layout %d", wire.Layout)
	}
	table, err := decodeExprTable(wire.Exprs, wire.Source, wire.SourceAt)
	if err != nil {
		return err
	}
	if err := resolveRefs(reflect.ValueOf(wire.Facts).Elem(), table); err != nil {
		return err
	}
	*r = RouteFacts(*wire.Facts)
	r.source, r.sourceAt = wire.Source, wire.SourceAt
	return nil
}

// listKey identifies a slice by its first element's address and length:
// two slices with both equal hold the same elements.
type listKey struct {
	first *RouteExpr
	n     int
}

type keywordsKey struct {
	first *RouteKeyword
	n     int
}

// exprEncoder interns one file's expressions. Besides the content key of
// each entry, it remembers the slices, maps and Object pointers it has
// already written, so a subtree shared in memory is walked once.
type exprEncoder struct {
	source     string
	sourceAt   int
	lo, hi     int
	entries    []exprEntry
	ids        map[string]int
	objects    map[*RouteExpr]int
	lists      map[listKey][]int
	keywords   map[keywordsKey][]keywordEntry
	properties map[uintptr]map[string]int
}

func newExprEncoder(source string, at int) *exprEncoder {
	return &exprEncoder{source: source, sourceAt: at, ids: map[string]int{}, objects: map[*RouteExpr]int{}, lists: map[listKey][]int{}, keywords: map[keywordsKey][]keywordEntry{}, properties: map[uintptr]map[string]int{}}
}

// intern returns the table reference of e.
func (enc *exprEncoder) intern(e RouteExpr) int {
	w := exprEntry{Kind: e.Kind, Name: e.Name, Offset: e.Offset, Operator: e.Operator, Invalid: e.Invalid, Spread: e.Spread, Span: e.Span}
	if len(e.Keys) > 0 {
		w.Keys = e.Keys
	}
	if e.Text != "" {
		start := e.Offset - enc.sourceAt
		// Comparing a slice of the source with a Text cut from it is cheap:
		// equal strings at the same address compare without reading them.
		if start >= 0 && start+len(e.Text) <= len(enc.source) && enc.source[start:start+len(e.Text)] == e.Text {
			w.TextLen = len(e.Text)
			if enc.hi == enc.lo {
				enc.lo, enc.hi = e.Offset, e.Offset+len(e.Text)
			} else {
				enc.lo, enc.hi = min(enc.lo, e.Offset), max(enc.hi, e.Offset+len(e.Text))
			}
		} else {
			w.Text = e.Text
		}
	}
	if e.Object != nil {
		ref, ok := enc.objects[e.Object]
		if !ok {
			ref = enc.intern(*e.Object)
			enc.objects[e.Object] = ref
		}
		w.Object = ref
	}
	if e.Args != nil && len(e.Args) == 0 {
		w.EmptyArgs = true
	}
	w.Args = enc.list(e.Args)
	w.Parts = enc.list(e.Parts)
	if len(e.Keywords) > 0 {
		key := keywordsKey{&e.Keywords[0], len(e.Keywords)}
		list, ok := enc.keywords[key]
		if !ok {
			list = make([]keywordEntry, len(e.Keywords))
			for i, k := range e.Keywords {
				list[i] = keywordEntry{Name: k.Name, Value: enc.intern(k.Value)}
			}
			enc.keywords[key] = list
		}
		w.Keywords = list
	}
	if len(e.Properties) > 0 {
		key := reflect.ValueOf(e.Properties).Pointer()
		refs, ok := enc.properties[key]
		if !ok {
			refs = make(map[string]int, len(e.Properties))
			// Sorted, so the table (and the cached bytes) do not depend on
			// map order.
			for _, k := range slices.Sorted(maps.Keys(e.Properties)) {
				refs[k] = enc.intern(e.Properties[k])
			}
			enc.properties[key] = refs
		}
		w.Properties = refs
	}
	data, err := json.Marshal(w)
	if err != nil {
		// exprEntry holds only strings, numbers, booleans and a Span.
		panic(fmt.Sprintf("encoding expression entry: %v", err))
	}
	if ref, ok := enc.ids[string(data)]; ok {
		return ref
	}
	enc.entries = append(enc.entries, w)
	enc.ids[string(data)] = len(enc.entries)
	return len(enc.entries)
}

func (enc *exprEncoder) list(list []RouteExpr) []int {
	if len(list) == 0 {
		return nil
	}
	key := listKey{&list[0], len(list)}
	if refs, ok := enc.lists[key]; ok {
		return refs
	}
	refs := make([]int, len(list))
	for i, v := range list {
		refs[i] = enc.intern(v)
	}
	enc.lists[key] = refs
	return refs
}

var routeExprType = reflect.TypeFor[RouteExpr]()

// replace returns a copy of v in which every RouteExpr is a reference to
// its table entry. Values whose type holds no RouteExpr are shared, not
// copied; the copy is only marshalled.
func (enc *exprEncoder) replace(v reflect.Value) reflect.Value {
	t := v.Type()
	if t == routeExprType {
		ref := enc.intern(v.Interface().(RouteExpr))
		return reflect.ValueOf(RouteExpr{Ref: int32(ref)})
	}
	if !holdsExpr(t) {
		return v
	}
	switch t.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(t.Elem())
		out.Elem().Set(enc.replace(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(t).Elem()
		for i := range t.NumField() {
			if t.Field(i).IsExported() {
				out.Field(i).Set(enc.replace(v.Field(i)))
			}
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(t, v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(enc.replace(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(t).Elem()
		for i := range v.Len() {
			out.Index(i).Set(enc.replace(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(t, v.Len())
		for _, k := range sortedKeys(v) {
			out.SetMapIndex(k, enc.replace(v.MapIndex(k)))
		}
		return out
	}
	return v
}

// sortedKeys returns the keys of map v in a fixed order, so the table is
// interned in the same order on every run.
func sortedKeys(v reflect.Value) []reflect.Value {
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		switch a.Kind() {
		case reflect.String:
			return strings.Compare(a.String(), b.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return cmp.Compare(a.Int(), b.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return cmp.Compare(a.Uint(), b.Uint())
		}
		return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
	})
	return keys
}

// decodeExprTable builds the table's expressions, each once.
func decodeExprTable(entries []exprEntry, source string, at int) ([]RouteExpr, error) {
	table := make([]RouteExpr, len(entries))
	for i, w := range entries {
		// ref resolves a reference of entry i, which must name an earlier entry.
		ref := func(r int) (RouteExpr, error) {
			if r < 1 || r > i {
				return RouteExpr{}, fmt.Errorf("route facts: expression %d refers to %d", i+1, r)
			}
			return table[r-1], nil
		}
		list := func(refs []int) ([]RouteExpr, error) {
			if len(refs) == 0 {
				return nil, nil
			}
			out := make([]RouteExpr, len(refs))
			for j, r := range refs {
				v, err := ref(r)
				if err != nil {
					return nil, err
				}
				out[j] = v
			}
			return out, nil
		}
		e := RouteExpr{Kind: w.Kind, Name: w.Name, Offset: w.Offset, Text: w.Text, Operator: w.Operator, Keys: w.Keys, Invalid: w.Invalid, Spread: w.Spread, Span: w.Span}
		if w.TextLen > 0 {
			start := w.Offset - at
			if start < 0 || start+w.TextLen > len(source) {
				return nil, fmt.Errorf("route facts: expression %d text out of range", i+1)
			}
			e.Text = source[start : start+w.TextLen]
		}
		if w.Object != 0 {
			if _, err := ref(w.Object); err != nil {
				return nil, err
			}
			e.Object = &table[w.Object-1]
		}
		var err error
		if e.Args, err = list(w.Args); err != nil {
			return nil, err
		}
		if w.EmptyArgs {
			e.Args = []RouteExpr{}
		}
		if e.Parts, err = list(w.Parts); err != nil {
			return nil, err
		}
		if len(w.Keywords) > 0 {
			e.Keywords = make([]RouteKeyword, len(w.Keywords))
			for j, k := range w.Keywords {
				v, err := ref(k.Value)
				if err != nil {
					return nil, err
				}
				e.Keywords[j] = RouteKeyword{Name: k.Name, Value: v}
			}
		}
		if len(w.Properties) > 0 {
			e.Properties = make(map[string]RouteExpr, len(w.Properties))
			for k, r := range w.Properties {
				v, err := ref(r)
				if err != nil {
					return nil, err
				}
				e.Properties[k] = v
			}
		}
		table[i] = e
	}
	return table, nil
}

// resolveRefs replaces, in place, every reference in v by its table entry.
func resolveRefs(v reflect.Value, table []RouteExpr) error {
	t := v.Type()
	if t == routeExprType {
		e := v.Addr().Interface().(*RouteExpr)
		if e.Ref == 0 {
			return nil
		}
		if int(e.Ref) > len(table) || e.Ref < 0 {
			return fmt.Errorf("route facts: reference %d outside the table", e.Ref)
		}
		*e = table[e.Ref-1]
		return nil
	}
	if !holdsExpr(t) {
		return nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return resolveRefs(v.Elem(), table)
		}
	case reflect.Struct:
		for i := range t.NumField() {
			if t.Field(i).IsExported() {
				if err := resolveRefs(v.Field(i), table); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if err := resolveRefs(v.Index(i), table); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			value := reflect.New(t.Elem()).Elem()
			value.Set(iter.Value())
			if err := resolveRefs(value, table); err != nil {
				return err
			}
			v.SetMapIndex(iter.Key(), value)
		}
	}
	return nil
}

var holdsExprTypes sync.Map // reflect.Type -> bool

// holdsExpr reports whether a value of t can hold a RouteExpr that replace
// and resolveRefs reach. Types with their own JSON methods, interfaces and
// structs embedding an unexported type are written as they are (their
// expressions, if any, stay inline, which is still correct).
func holdsExpr(t reflect.Type) bool {
	if v, ok := holdsExprTypes.Load(t); ok {
		return v.(bool)
	}
	held := reachesExpr(t, map[reflect.Type]bool{})
	holdsExprTypes.Store(t, held)
	return held
}

// reachesExpr searches the type graph from t for RouteExpr; seen breaks
// the cycles of recursive types such as URLStatement.
func reachesExpr(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == routeExprType {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	if t.Implements(reflect.TypeFor[json.Marshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) || t.Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
		return false
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return reachesExpr(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.Anonymous && !f.IsExported() {
				return false
			}
		}
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && reachesExpr(f.Type, seen) {
				return true
			}
		}
	}
	return false
}
