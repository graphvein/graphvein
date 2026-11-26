package routes

import "testing"

// TestAxumVersions checks how the axum version of a crate is proven
// (forms version:* of rows SH-X11 and SH-X25): the nearest
// Cargo.lock wins, else the manifest's requirement, with workspace
// inheritance and path dependencies; anything else is not proven.
func TestAxumVersions(t *testing.T) {
	lock := func(entries string) string {
		return "version = 4\n\n[[package]]\nname = \"axum\"\nversion = \"0.3.4\"\nsource = \"registry+x\"\n\n[[package]]\nname = \"axum\"\nversion = \"0.7.9\"\nsource = \"registry+x\"\n\n[[package]]\nname = \"app\"\nversion = \"0.1.0\"\ndependencies = [" + entries + "]\n"
	}
	manifest := func(dep string) string {
		return "[package]\nname = \"app\"\nversion = \"0.1.0\"\n\n[dependencies]\n" + dep + "\n"
	}
	cases := []struct {
		name    string
		sources map[string]string
		known   bool
		lo, hi  semver
	}{
		{"caret", map[string]string{"Cargo.toml": manifest(`axum = "0.8.8"`)}, true, semver{0, 8, 8}, semver{0, 9, 0}},
		{"caret minor", map[string]string{"Cargo.toml": manifest(`axum = "^0.6"`)}, true, semver{0, 6, 0}, semver{0, 7, 0}},
		{"caret major", map[string]string{"Cargo.toml": manifest(`axum = "1.2"`)}, true, semver{1, 2, 0}, semver{2, 0, 0}},
		{"exact", map[string]string{"Cargo.toml": manifest(`axum = "=0.6.7"`)}, true, semver{0, 6, 7}, semver{0, 6, 8}},
		{"tilde", map[string]string{"Cargo.toml": manifest(`axum = { version = "~0.6.2", features = ["ws"] }`)}, true, semver{0, 6, 2}, semver{0, 7, 0}},
		{"range", map[string]string{"Cargo.toml": manifest(`axum = ">=0.6"`)}, false, semver{}, semver{}},
		{"star", map[string]string{"Cargo.toml": manifest(`axum = "*"`)}, false, semver{}, semver{}},
		{"two comparators", map[string]string{"Cargo.toml": manifest(`axum = ">=0.6, <0.8"`)}, false, semver{}, semver{}},
		{"pre-release", map[string]string{"Cargo.toml": manifest(`axum = "0.8.0-rc.1"`)}, false, semver{}, semver{}},
		{"git", map[string]string{"Cargo.toml": manifest(`axum = { git = "https://example.invalid/axum" }`)}, false, semver{}, semver{}},
		{"renamed", map[string]string{"Cargo.toml": manifest(`web = { package = "axum", version = "0.7.5" }`)}, true, semver{0, 7, 5}, semver{0, 8, 0}},
		{"twice differently", map[string]string{"Cargo.toml": manifest("axum = \"0.7\"\n\n[dev-dependencies]\naxum = \"0.6\"")}, false, semver{}, semver{}},
		{"no axum", map[string]string{"Cargo.toml": manifest(`serde = "1"`)}, false, semver{}, semver{}},
		{"workspace inheritance", map[string]string{
			"Cargo.toml":     "[workspace]\nmembers = [\"app\"]\n\n[workspace.dependencies]\naxum = { version = \"0.7.4\", default-features = false }\n",
			"app/Cargo.toml": manifest(`axum = { workspace = true, features = ["ws"] }`),
		}, true, semver{0, 7, 4}, semver{0, 8, 0}},
		{"workspace without entry", map[string]string{
			"Cargo.toml":     "[workspace]\nmembers = [\"app\"]\n",
			"app/Cargo.toml": manifest(`axum = { workspace = true }`),
		}, false, semver{}, semver{}},
		{"path dependency", map[string]string{
			"app/Cargo.toml":  manifest(`axum = { path = "../axum" }`),
			"axum/Cargo.toml": "[package]\nname = \"axum\"\nversion = \"0.6.5\"\n",
		}, true, semver{0, 6, 5}, semver{0, 6, 6}},
		{"path dependency with workspace version", map[string]string{
			"Cargo.toml":      "[workspace]\nmembers = [\"app\", \"axum\"]\n\n[workspace.package]\nversion = \"0.8.1\"\n",
			"app/Cargo.toml":  manifest(`axum = { path = "../axum" }`),
			"axum/Cargo.toml": "[package]\nname = \"axum\"\nversion.workspace = true\n",
		}, true, semver{0, 8, 1}, semver{0, 8, 2}},
		{"lock entry with version", map[string]string{"Cargo.toml": manifest(`axum = "0.7"`), "Cargo.lock": lock(`"axum 0.3.4"`)}, true, semver{0, 3, 4}, semver{0, 3, 5}},
		{"lock entry without version, one locked", map[string]string{"Cargo.toml": manifest(`axum = "0.7"`), "Cargo.lock": "version = 4\n\n[[package]]\nname = \"axum\"\nversion = \"0.7.9\"\nsource = \"registry+x\"\n\n[[package]]\nname = \"app\"\nversion = \"0.1.0\"\ndependencies = [\"axum\"]\n"}, true, semver{0, 7, 9}, semver{0, 7, 10}},
		{"lock entry without version, several locked", map[string]string{"Cargo.toml": manifest(`axum = "0.7"`), "Cargo.lock": lock(`"axum"`)}, false, semver{}, semver{}},
		{"lock without the crate's axum", map[string]string{"Cargo.toml": manifest(`axum = "0.7"`), "Cargo.lock": lock(`"serde"`)}, false, semver{}, semver{}},
		{"lock in the workspace root", map[string]string{
			"Cargo.toml":     "[workspace]\nmembers = [\"app\"]\n",
			"Cargo.lock":     lock(`"axum 0.7.9"`),
			"app/Cargo.toml": manifest(`axum = "0.3"`),
		}, true, semver{0, 7, 9}, semver{0, 7, 10}},
		{"unreadable lock", map[string]string{"Cargo.toml": manifest(`axum = "0.7"`), "Cargo.lock": "[[package]\n"}, false, semver{}, semver{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sources := map[string][]byte{}
			for k, v := range c.sources {
				sources[k] = []byte(v)
			}
			file := "src/lib.rs"
			if _, ok := c.sources["app/Cargo.toml"]; ok {
				file = "app/src/lib.rs"
			}
			v := (&axumVersions{sources: sources, cache: map[string]versionRange{}}).of(file)
			if v.known != c.known || c.known && (v.lo != c.lo || v.hi != c.hi) {
				t.Errorf("got %+v, want known %v [%v, %v)", v, c.known, c.lo, c.hi)
			}
			if !v.known && v.why == "" {
				t.Error("an unproven version has no reason")
			}
		})
	}
	axum067 := semver{0, 6, 7}
	r := requirementRange("0.6.7", "")
	if !r.atLeast(axum067) || r.below(axum067) || !r.atLeast(axum05) || r.atLeast(semver{0, 6, 8}) {
		t.Errorf("predicates of %+v", r)
	}
	if r := requirementRange("0.6", ""); r.atLeast(axum067) || r.below(axum067) {
		t.Errorf("0.6 proves neither side of 0.6.7: %+v", r)
	}
	if (versionRange{}).atLeast(semver{}) || (versionRange{}).below(semver{9, 9, 9}) {
		t.Error("an unknown range proves nothing")
	}
}

// TestSerdeRenameRules checks renameField and renameVariant against
// serde_derive 1.0.228's own tests (internals/case.rs, rename_fields and
// rename_variants) and against fields serde handles per character
// (checked with serde_json: `_foo` camelCase is `foo`, a non-snake field
// keeps its case under lowercase).
func TestSerdeRenameRules(t *testing.T) {
	rules := []string{"UPPERCASE", "PascalCase", "camelCase", "SCREAMING_SNAKE_CASE", "kebab-case", "SCREAMING-KEBAB-CASE"}
	fields := [][]string{
		// original, upper, pascal, camel, screaming, kebab, screaming kebab
		{"outcome", "OUTCOME", "Outcome", "outcome", "OUTCOME", "outcome", "OUTCOME"},
		{"very_tasty", "VERY_TASTY", "VeryTasty", "veryTasty", "VERY_TASTY", "very-tasty", "VERY-TASTY"},
		{"a", "A", "A", "a", "A", "a", "A"},
		{"z42", "Z42", "Z42", "z42", "Z42", "z42", "Z42"},
		{"_foo", "_FOO", "Foo", "foo", "_FOO", "-foo", "-FOO"},
		{"bar_baz_", "BAR_BAZ_", "BarBaz", "barBaz", "BAR_BAZ_", "bar-baz-", "BAR-BAZ-"},
		{"userId", "USERID", "UserId", "userId", "USERID", "userId", "USERID"},
		{"a__b", "A__B", "AB", "aB", "A__B", "a--b", "A--B"},
	}
	for _, f := range fields {
		for _, same := range []string{"lowercase", "snake_case"} {
			if got, ok := renameField(f[0], same); !ok || got != f[0] {
				t.Errorf("%s %s: %q", same, f[0], got)
			}
		}
		for i, rule := range rules {
			if got, ok := renameField(f[0], rule); !ok || got != f[i+1] {
				t.Errorf("field %s %s: %q, want %q", rule, f[0], got, f[i+1])
			}
		}
	}
	if _, ok := renameField("__", "camelCase"); ok {
		t.Error("serde fails on an all-underscore field under camelCase")
	}
	if _, ok := renameField("x", "Title Case"); ok {
		t.Error("unknown rule")
	}
	variants := [][]string{
		// original, lower, upper, camel, snake, screaming, kebab, screaming kebab
		{"Outcome", "outcome", "OUTCOME", "outcome", "outcome", "OUTCOME", "outcome", "OUTCOME"},
		{"VeryTasty", "verytasty", "VERYTASTY", "veryTasty", "very_tasty", "VERY_TASTY", "very-tasty", "VERY-TASTY"},
		{"A", "a", "A", "a", "a", "A", "a", "A"},
		{"Z42", "z42", "Z42", "z42", "z42", "Z42", "z42", "Z42"},
		{"HTTPCode", "httpcode", "HTTPCODE", "hTTPCode", "h_t_t_p_code", "H_T_T_P_CODE", "h-t-t-p-code", "H-T-T-P-CODE"},
	}
	for _, v := range variants {
		if got, ok := renameVariant(v[0], "PascalCase"); !ok || got != v[0] {
			t.Errorf("PascalCase %s: %q", v[0], got)
		}
		for i, rule := range []string{"lowercase", "UPPERCASE", "camelCase", "snake_case", "SCREAMING_SNAKE_CASE", "kebab-case", "SCREAMING-KEBAB-CASE"} {
			if got, ok := renameVariant(v[0], rule); !ok || got != v[i+1] {
				t.Errorf("variant %s %s: %q, want %q", rule, v[0], got, v[i+1])
			}
		}
	}
}
