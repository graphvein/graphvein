package typescript

import "testing"

// TestCompilerFlags covers the tsconfig chain CompilerFlags reads: the
// nearest config, a relative parent overridden by the child, an extends
// array whose later package entry makes earlier settings unproven, a
// non-boolean value and a config that does not parse.
func TestCompilerFlags(t *testing.T) {
	sources := map[string][]byte{
		"tsconfig.base.json":    []byte(`{"compilerOptions": {"experimentalDecorators": true, "strict": true}}`),
		"app/tsconfig.json":     []byte(`{"extends": "../tsconfig.base.json", "compilerOptions": {"strict": false, "emitDecoratorMetadata": "yes"}}`),
		"lib/tsconfig.json":     []byte(`{"extends": ["../tsconfig.base.json", "@tsconfig/node20/tsconfig.json"], "compilerOptions": {"strictNullChecks": true}}`),
		"bad/tsconfig.json":     []byte(`{"compilerOptions": `),
		"app/src/a.ts":          nil,
		"lib/a.ts":              nil,
		"bad/a.ts":              nil,
		"other/a.ts":            nil,
		"other/x/tsconfig.json": []byte(`{}`),
	}
	files := map[string]Facts{}
	for f := range sources {
		files[f] = Facts{Exports: map[string]string{}}
	}
	r := NewModuleResolver(files, sources)
	b := func(v bool) *bool { return &v }
	cases := []struct {
		file, config string
		values       map[string]*bool
		unresolved   int
	}{
		{"app/src/a.ts", "app/tsconfig.json", map[string]*bool{"experimentalDecorators": b(true), "strict": b(false), "emitDecoratorMetadata": nil}, 0},
		{"lib/a.ts", "lib/tsconfig.json", map[string]*bool{"strictNullChecks": b(true)}, 1},
		{"bad/a.ts", "bad/tsconfig.json", map[string]*bool{}, 1},
		{"other/a.ts", "", map[string]*bool{}, 0},
	}
	for _, c := range cases {
		got := r.CompilerFlags(c.file)
		if got.Config != c.config || len(got.Unresolved) != c.unresolved || len(got.Values) != len(c.values) {
			t.Errorf("%s: %+v", c.file, got)
			continue
		}
		for k, v := range c.values {
			g, ok := got.Values[k]
			if !ok || (v == nil) != (g == nil) || v != nil && *v != *g {
				t.Errorf("%s: %s = %v, want %v", c.file, k, g, v)
			}
		}
	}
}
