package testscope

import "testing"

func TestBuiltinClassification(t *testing.T) {
	for _, file := range []string{"tests/app.py", "src/test/app.ts", "a/__tests__/app.js", "__mocks__/api.ts", "testdata/items.rs", "src/test_api.py", "src/api_test.py", "conftest.py", "a_test.go", "a.spec.ts", "a.test.mjs", "spec.ts", "test.tsx", "a.test.cts", "a.cy.jsx", "a.spec.test.mtsx", "a.testspec.ts"} {
		if Classify(file, Rules{}) != Test {
			t.Errorf("not test: %s", file)
		}
	}
	for _, file := range []string{"src/main.rs", "examples/app.rs", "benches/app.rs", "docs/example.ts", "Test/app.py", "src/tests.ts", "a.SPEC.ts", "test.py", "spec.json", "a.cy.mjs", "a.test.vue"} {
		if Classify(file, Rules{}) != "" {
			t.Errorf("not production: %s", file)
		}
	}
}

func TestPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, file string
		want          bool
	}{
		{"checks/", "checks/a.ts", true}, {"checks/", "a/checks/b.ts", true}, {"checks/", "checks", false},
		{"checks", "a/checks/b.ts", true}, {"checks", "a/checks", true},
		{"a/checks", "a/checks/b.ts", true}, {"a/checks", "x/a/checks/b.ts", false},
		{"/checks", "checks/a.ts", true}, {"/checks", "a/checks/b.ts", false},
		{"a/**/x?.[jt]s", "a/x1.ts", true}, {"a/**/x?.[jt]s", "a/b/c/x2.js", true},
		{"a/**/x?.[jt]s", "a/b/x22.ts", false}, {"**/checks/**", "checks/a.ts", true},
		{"[!a]*.ts", "b.ts", true}, {"[^a]*.ts", "a.ts", false},
		{"Checks", "checks/a.ts", false}, {"a/*/x.ts", "a/b/c/x.ts", false},
	} {
		rules, err := Compile([]string{tc.pattern}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := Classify(tc.file, rules) == Test; got != tc.want {
			t.Errorf("%s vs %s: %v", tc.pattern, tc.file, got)
		}
	}
	rules, err := Compile([]string{"checks/**"}, []string{"tests/**", "checks/keep.ts"})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"tests/a.ts", "checks/keep.ts"} {
		if Classify(file, rules) != "" {
			t.Fatal(file)
		}
	}
	if Classify("checks/run.ts", rules) != Test {
		t.Fatal("include ignored")
	}
	for _, p := range []string{"", "!test", "a\\b", "a\x00b", "[", "[]"} {
		if _, err := Compile([]string{p}, nil); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
}
