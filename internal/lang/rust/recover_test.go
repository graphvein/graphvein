package rust

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

// TestPatternAttributeRecovery checks row RS-F4 on single files: a file is
// read only when blanking struct pattern field attributes leaves a clean
// tree with every blanked attribute in a field position; any other error
// stays with RS-F2.
func TestPatternAttributeRecovery(t *testing.T) {
	cases := []struct {
		name, source string
		read         bool
		functions    []string
	}{
		{"let pattern", "pub fn f(s: S) {\n    let S {\n        a,\n        #[cfg(test)]\n        b,\n    } = s;\n}\npub fn g() {}\n", true, []string{"f", "g"}},
		{"several attributes and a field rename", "fn f(s: S) { let S { #[cfg(a)] #[cfg(b)] x: y, z } = s; }\n", true, []string{"f"}},
		{"match arm and statement attribute", "fn f(s: S) -> u8 {\n    #[cfg(x)]\n    let k = 0;\n    match s { S { #[cfg(x)] a, .. } => a + k }\n}\n", true, []string{"f"}},
		{"parameter pattern", "fn f(S { #[cfg(x)] a, b }: S) {}\n", true, []string{"f"}},
		// rustc 1.97: "expected identifier, found `..`".
		{"attribute before the rest pattern", "fn f(s: S) { let S { a, #[cfg(x)] .. } = s; }\n", false, nil},
		{"another syntax error", "fn f(s: S) { let S { a, #[cfg(x)] b } = s; a + }\nfn g() {}\n", false, nil},
		{"attribute text in a string only", "fn f() { let s = \"#[x]\"; let = ; }\n", false, nil},
		{"not an attribute", "fn f(s: S) { let S { a, #[] b } = s; }\n", false, nil},
		{"tuple struct pattern", "fn f(s: S) { let S(a, #[cfg(x)] b) = s; }\n", false, nil},
	}
	options := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	pipeline := func(ctx context.Context, jobs []parse.Job, consume func(int, parse.Result)) error {
		return parse.Consume(ctx, jobs, options, consume)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			source := []byte(c.source)
			first, err := parse.Batch(context.Background(), []parse.Job{{Language: "rust", Source: source}}, options)
			if err != nil || first[0].Err != nil {
				t.Fatal(err, first[0].Err)
			}
			if !first[0].SyntaxError {
				t.Fatal("tree-sitter reports no syntax error: the case tests nothing")
			}
			var got []string
			read := false
			rec := PatternAttributes(first[0].Root, source)
			var recs []*Recovery
			if rec != nil {
				recs = append(recs, rec)
			}
			if err := Recover(context.Background(), recs, pipeline, func(i int, root *parse.Node) {
				read = true
				if root.Text != c.source {
					t.Errorf("root text is not the original source")
				}
				for _, n := range Extract("a.rs", source, root).Nodes {
					got = append(got, n.QualifiedName)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if read != c.read {
				t.Fatalf("read = %v, want %v", read, c.read)
			}
			if len(got) != len(c.functions) {
				t.Fatalf("nodes %v, want %v", got, c.functions)
			}
			for i := range got {
				if got[i] != c.functions[i] {
					t.Errorf("nodes %v, want %v", got, c.functions)
				}
			}
		})
	}
}

// TestPatternAttributesNoErrorNode checks that a file whose only error is a
// MISSING token (no ERROR node) gives no RS-F4 candidate.
func TestPatternAttributesNoErrorNode(t *testing.T) {
	source := []byte("fn f() { let x = 1 }\n")
	first, err := parse.Batch(context.Background(), []parse.Job{{Language: "rust", Source: source}}, parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || first[0].Err != nil {
		t.Fatal(err, first[0].Err)
	}
	if !first[0].SyntaxError {
		t.Skip("tree-sitter reads the case without error")
	}
	if rec := PatternAttributes(first[0].Root, source); rec != nil {
		t.Errorf("candidates %v for a file without ERROR node", rec.candidates)
	}
}
