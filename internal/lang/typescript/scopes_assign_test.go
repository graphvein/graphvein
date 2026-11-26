package typescript

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

// TestObjectAssignWrites checks the Object.assign forms of row TSX-25 one
// by one: object literal sources whose entries all have a static key
// (shorthand, identifier or string key, computed string literal) write
// exactly those keys; every other source or entry writes every member.
func TestObjectAssignWrites(t *testing.T) {
	every := []string{"*"}
	cases := []struct {
		form, source string
		want         []string
	}{
		{"shorthand", `Object.assign(this, { a, b })`, []string{"a", "b"}},
		{"identifier key", `Object.assign(this, { a: 1, default: 2 })`, []string{"a", "default"}},
		{"string key", `Object.assign(this, { "a": 1, 'bc': 2 })`, []string{"a", "bc"}},
		{"computed string literal", `Object.assign(this, { ["a"]: 1 })`, []string{"a"}},
		{"several literal sources", `Object.assign(this, { a }, { b: 1 })`, []string{"a", "b"}},
		{"wrapped literal source", `Object.assign(this, ({ a } as Partial<T>))`, []string{"a"}},
		{"empty literal", `Object.assign(this, {})`, nil},
		{"key path of the target", `Object.assign(this.opts, { a })`, []string{"opts.a"}},
		{"class prototype target", `Object.assign(C.prototype, { a })`, []string{"prototype.a"}},
		{"no source", `Object.assign(this)`, every},
		{"name source", `Object.assign(this, init)`, every},
		{"call source", `Object.assign(this, make())`, every},
		{"spread argument", `Object.assign(this, ...list)`, every},
		{"literal and name", `Object.assign(this, { a }, init)`, every},
		{"spread entry", `Object.assign(this, { a, ...rest })`, every},
		{"computed non-literal key", `Object.assign(this, { [k]: 1 })`, every},
		{"computed template key", "Object.assign(this, { [`a`]: 1 })", every},
		{"numeric key", `Object.assign(this, { 1: 1 })`, every},
		{"method entry", `Object.assign(this, { a() {} })`, every},
		{"accessor entry", `Object.assign(this, { get a() { return 1; } })`, every},
		{"__proto__ key", `Object.assign(this, { __proto__: p })`, every},
		{"__proto__ shorthand", `Object.assign(this, { __proto__ })`, every},
		{"computed target key", `Object.assign(this[k], { a })`, every},
	}
	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			src := []byte(c.source + ";\n")
			results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: src}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
			if err != nil || results[0].Err != nil {
				t.Fatal(err, results[0].Err)
			}
			call := firstCall(results[0].Root)
			if call == nil {
				t.Fatal("no call")
			}
			// Each write renders as its key path, "*" for any key.
			var got []string
			for _, w := range objectWrites(call, nil) {
				p := w.path
				if w.anyKey {
					p = append(p, "*")
				}
				got = append(got, strings.Join(p, "."))
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("%s: writes %v, want %v", c.source, got, c.want)
			}
		})
	}
}

// firstCall is the first call expression in n, depth first.
func firstCall(n *parse.Node) *parse.Node {
	if n == nil {
		return nil
	}
	if n.Kind == "call_expression" {
		return n
	}
	for _, c := range n.Children {
		if f := firstCall(c); f != nil {
			return f
		}
	}
	return nil
}
