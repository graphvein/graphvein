package angular

import "testing"

func TestMajorRanges(t *testing.T) {
	for value, want := range map[string]int{"0.1.0": 0, "15.2.3": 15, "^17.1.0": 17, "~18.2": 18, "21.x": 21, "21.*.*": 21, "21.0.0-beta.1+build": 21, "21.x.1": -1, "021.0.0": -1, ">=19": -1, "~18 || ^19": -1, "*": -1, "latest": -1, "workspace:^21": -1, "npm:@angular/core@21": -1, "file:../core": -1, "https://example.invalid/core": -1} {
		if got := majorVersion(value); got != want {
			t.Errorf("%q major=%d, want %d", value, got, want)
		}
	}
}

func TestNearestLockAndDisagreement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources map[string][]byte
		want    int
	}{
		{"v1", map[string][]byte{"package-lock.json": []byte(`{"lockfileVersion":1,"dependencies":{"@angular/core":{"version":"15.2.3"},"@angular/compiler-cli":{"version":"15.2.3"}}}`)}, 15},
		{"ancestor", map[string][]byte{"package-lock.json": []byte(`{"lockfileVersion":2,"packages":{"node_modules/@angular/core":{"version":"21.2.12"},"node_modules/@angular/compiler-cli":{"version":"21.2.12"}}}`)}, 21},
		{"disagreement", map[string][]byte{"sub/package-lock.json": []byte(`{"lockfileVersion":3,"packages":{"node_modules/@angular/core":{"version":"21.2.11"},"node_modules/@angular/compiler-cli":{"version":"21.2.12"}}}`), "sub/bun.lock": []byte(`{"packages":{"@angular/core":["@angular/core@21.2.12"],"@angular/compiler-cli":["@angular/compiler-cli@21.2.12"]}}`)}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &linker{sources: tc.sources}
			core, compiler := l.lockMajors("sub")
			if core.major != tc.want || !core.found || compiler.major == 0 {
				t.Fatal(core, compiler)
			}
		})
	}
}
