package typeorm

import (
	"testing"
)

// TestGeneratedNames checks the naming port against values TypeORM's own
// DefaultNamingStrategy and StringUtils returned at the pins (typeorm
// 0.3.31 and 1.1.3 from npm, run in scratch with Node): the
// keys of identifier characters hash the same under both majors, a key
// with a space, a quote or a non-ASCII character differs.
func TestGeneratedNames(t *testing.T) {
	for _, line := range []string{Line03, Line1} {
		c := &claimer{g: &Group{Package: &Package{Line: line}}}
		want := map[string]string{
			"pk":  "PK_cf3835db1f9330c2462d786311a",
			"uq":  "UQ_95deb4c758f55d1c91d7ed0a9e2",
			"rel": "REL_970ba064ef54f72b56cbfb29e3",
			"idx": "IDX_649261cf5b72fda95fa358263d",
			"chk": "CHK_c9a3cb265f5df7de34c1a66eea",
		}
		if line == Line1 {
			want["idx"], want["chk"] = "IDX_f07a9b0cc66beab0decd4245bf", "CHK_0d481439eb60f91dd9ef87d613"
		}
		got := map[string]string{
			"pk":  c.generatedName("PK_", "t", []string{"b", "a"}, ""),
			"uq":  c.generatedName("UQ_", "t", []string{"x"}, ""),
			"rel": c.generatedName("REL_", "t", []string{"a"}, ""),
			"idx": c.generatedName("IDX_", "café", []string{"a"}, `"a" IS NOT NULL`),
			"chk": c.checkName("t", `"a" > 0`),
		}
		for k, w := range want {
			if got[k] != w {
				t.Errorf("%s %s: %s, want %s", line, k, got[k], w)
			}
		}
	}
	for in, want := range map[string]string{"by_handle": "byHandle", "user_id": "userId", "a__b": "a_b", "X-y z": "xYZ", "_id": "Id", "Abc_def": "abcDef"} {
		if got := camelCase(in); got != want {
			t.Errorf("camelCase(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Thing~src/app.module.ts#default": "Thing~src_app.module.ts_default", "sys_user": "sys_user", "a b": "a_b"} {
		if got := identitySegment(in); got != want {
			t.Errorf("identitySegment(%q) = %q, want %q", in, got, want)
		}
	}
}
