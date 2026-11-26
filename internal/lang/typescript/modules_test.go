package typescript

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

func moduleResolver(sources map[string]string) *ModuleResolver {
	files := map[string]Facts{}
	data := map[string][]byte{}
	for p, source := range sources {
		files[p] = Facts{}
		data[p] = []byte(source)
	}
	return NewModuleResolver(files, data)
}

func TestExportCycleDoesNotMemoizePartialTraversal(t *testing.T) {
	files := map[string]Facts{
		"a.ts":    {ExportClauses: []facts.ExportClause{{Module: "./b", Star: true}, {Module: "./leaf", Star: true}}},
		"b.ts":    {ExportClauses: []facts.ExportClause{{Module: "./a", Star: true}}},
		"leaf.ts": {Exports: map[string]string{"x": "function:leaf.ts#x"}},
	}
	for _, order := range [][]string{{"a.ts", "b.ts"}, {"b.ts", "a.ts"}} {
		r := NewModuleResolver(files, nil)
		for _, file := range order {
			if b := r.Export(file, "x"); b.Identity != "function:leaf.ts#x" {
				t.Fatal(order, file, b)
			}
		}
	}
}

func TestExtensionSubstitutionAndDeclarationFallback(t *testing.T) {
	r := moduleResolver(map[string]string{
		"a.ts": "", "a.tsx": "", "a.js": "", "a.jsx": "", "a.mts": "", "a.mjs": "", "a.cts": "", "a.cjs": "", "a.d.ts": "",
		"only.d.ts": "", "onlym.d.mts": "", "onlyc.d.cts": "", "dir.tsx": "", "dir/index.ts": "", "file.gen.ts": "",
		"typed.tsx": "", "typedjs.js": "", "typedm.mjs": "", "typedc.cjs": "",
		"jsxsource.ts": "", "jsfallback.jsx": "", "jsxfallback.js": "",
	})
	for spec, want := range map[string]string{"./a.js": "a.ts", "./a.jsx": "a.tsx", "./a.mjs": "a.mts", "./a.cjs": "a.cts", "./only": "only.d.ts", "./onlym.mjs": "onlym.d.mts", "./onlyc.cjs": "onlyc.d.cts", "./dir": "dir.tsx", "./file.gen": "file.gen.ts"} {
		if got := r.Resolve("main.ts", spec).File; got != want {
			t.Errorf("%s: %s, want %s", spec, got, want)
		}
	}
	for spec, want := range map[string]string{"./typed.ts": "typed.tsx", "./typedjs.tsx": "typedjs.js", "./typedm.mts": "typedm.mjs", "./typedc.cts": "typedc.cjs", "./jsxsource.jsx": "jsxsource.ts", "./jsfallback.js": "jsfallback.jsx", "./jsxfallback.jsx": "jsxfallback.js"} {
		if got := r.Resolve("main.ts", spec).File; got != want {
			t.Errorf("%s: %s, want %s", spec, got, want)
		}
	}
}

func TestJSONCStringsAndErrors(t *testing.T) {
	for _, source := range []string{`{"url":"https://a/*literal*/,",/*comment*/"values":[1,],}`, "\ufeff{ // comment\n\"escaped\":\"\\\"//\",\n}"} {
		if _, err := jsonc([]byte(source)); err != nil {
			t.Fatal(source, err)
		}
	}
	for _, source := range []string{`{/*`, `{"a":}`, `{"a":1} {}`, `{"a":"unterminated}`} {
		if _, err := jsonc([]byte(source)); err == nil {
			t.Fatal("accepted invalid JSONC", source)
		}
	}
	r := moduleResolver(map[string]string{"bad/tsconfig.json": "{/*", "bad/main.ts": "", "tsconfig.json": `{"compilerOptions":{"baseUrl":"."}}`, "tool.ts": ""})
	// Row TS-C1's refusal: an unread nearest configuration leaves the
	// file's options unknown, so a bare specifier claims nothing.
	if got := r.Resolve("bad/main.ts", "tool"); got.File != "" || got.Refusal != unreadConfig {
		t.Fatal("unread config did not refuse", got)
	}
	if d := r.Diagnostics(); len(d) != 1 || d[0].Code != "config_parse_error" || d[0].File != "bad/tsconfig.json" {
		t.Fatal(d)
	}
}

func TestConfigInheritanceOriginPrecedenceAndCycles(t *testing.T) {
	r := moduleResolver(map[string]string{
		"cfg/base.json":          `{"compilerOptions":{"baseUrl":"../source","paths":{"alias":["tool"]}}}`,
		"cfg/next.json":          `{"extends":"./base"}`,
		"app/tsconfig.json":      `{"extends":"../cfg/next.json"}`,
		"app/deep/jsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"alias":["wrong"]}}}`,
		"app/deep/main.ts":       "", "app/deep/wrong.ts": "", "source/tool.ts": "",
		"cycles/tsconfig.json": `{"extends":"./parent.json","compilerOptions":{"baseUrl":"."}}`,
		"cycles/parent.json":   `{"extends":"./tsconfig.json","compilerOptions":{"paths":{"loop":["leaf"]}}}`,
		"cycles/leaf.ts":       "", "cycles/main.ts": "",
		"relative/base.json": `{"compilerOptions":{"paths":{"alias":["tool"]}}}`,
		"relative/tool.ts":   "", "relative/app/tsconfig.json": `{"extends":"../base.json"}`,
		"relative/app/main.ts": "",
		"multi/tsconfig.json":  `{"extends":["./a.json","./b.json"],"compilerOptions":{"paths":{"alias":["tool"]}}}`,
		"multi/a.json":         `{"compilerOptions":{"baseUrl":"a"}}`, "multi/b.json": `{"compilerOptions":{"baseUrl":"b"}}`,
		"multi/b/tool.ts": "", "multi/a/tool.ts": "", "multi/main.ts": "",
	})
	// The nearest directory's jsconfig.json governs app/deep (the compiler
	// looks for tsconfig.json, then jsconfig.json, directory by directory);
	// a circular extends is unread, so the bare name claims nothing.
	if got := r.Resolve("cycles/main.ts", "loop"); got.Refusal != unreadConfig {
		t.Error("circular extends", got)
	}
	for _, entry := range [][3]string{{"app/deep/main.ts", "alias", "app/deep/wrong.ts"}, {"relative/app/main.ts", "alias", "relative/tool.ts"}, {"multi/main.ts", "alias", "multi/b/tool.ts"}} {
		if got := r.Resolve(entry[0], entry[1]).File; got != entry[2] {
			t.Errorf("%v: %s", entry, got)
		}
	}
}

func TestPathSpecificityAndChildReplacement(t *testing.T) {
	r := moduleResolver(map[string]string{
		"tsconfig.json": `{"compilerOptions":{"paths":{"*tail":["wrong"],"pre*":["prefix"],"pre*tail":["both"],"preXtail":["exact"],"parent":["exact"]}}}`,
		"main.ts":       "", "wrong.ts": "", "prefix.ts": "", "both.ts": "", "exact.ts": "",
		"child/tsconfig.json": `{"extends":"../tsconfig.json","compilerOptions":{"paths":{}}}`,
		"child/main.ts":       "",
		// Row TS-I5: "parent" is an npm package only when declared.
		"package.json": `{"dependencies":{"parent":"1.0.0"}}`,
	})
	// The compiler takes the longest prefix and, on a tie, the first key
	// (findBestPatternMatch): pre* before pre*tail.
	for spec, want := range map[string]string{"preXtail": "exact.ts", "preYtail": "prefix.ts", "preOther": "prefix.ts", "other-tail": "wrong.ts"} {
		if got := r.Resolve("main.ts", spec).File; got != want {
			t.Error(spec, got, want)
		}
	}
	if got := r.Resolve("child/main.ts", "parent"); !got.External {
		t.Fatal("child paths did not replace parent", got)
	}
}

// TestDeclaredPackages covers row TS-I5's rule: an external
// only for a Node built-in or a name a package.json at or above the file
// declares, never when a paths key matched; the matched targets are kept
// as candidates (Orval).
func TestDeclaredPackages(t *testing.T) {
	r := moduleResolver(map[string]string{
		"package.json":      `{"devDependencies":{"root-dev":"1"},"optionalDependencies":{"@s/opt":"1"}}`,
		"app/package.json":  `{"name":"app","peerDependencies":{"app-peer":"1"}}`,
		"app/tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@gen/*":["./generated/*","./other/*"],"root-dev":["./missing"]}}}`,
		"app/main.ts":       "",
		"lib/main.ts":       "",
	})
	for _, c := range []struct {
		file, spec string
		external   bool
		refusal    string
	}{
		{"app/main.ts", "app-peer/x", true, ""},
		{"app/main.ts", "@s/opt", true, ""},
		{"lib/main.ts", "app-peer", false, unreadUndeclaredPackage},
		{"lib/main.ts", "root-dev", true, ""},
		{"app/main.ts", "root-dev", false, unreadPathsTargetMissing},
		{"app/main.ts", "@gen/api", false, unreadPathsTargetMissing},
		{"app/main.ts", "src/generated/client", false, unreadUndeclaredPackage},
		{"lib/main.ts", "fs/promises", true, ""},
		{"lib/main.ts", "node:fs", true, ""},
		{"lib/main.ts", "node:test", true, ""},
		{"lib/main.ts", "test", false, unreadUndeclaredPackage},
		{"lib/main.ts", "node:nope", false, unreadUndeclaredPackage},
	} {
		got := r.Resolve(c.file, c.spec)
		if got.External != c.external || got.Refusal != c.refusal {
			t.Errorf("%s %s: %+v", c.file, c.spec, got)
		}
	}
	got := r.Resolve("app/main.ts", "@gen/api")
	if strings.Join(got.Candidates, " ") != "app/generated/api app/other/api" {
		t.Errorf("paths candidates %v", got.Candidates)
	}
}

// TestUnreadDeclaredPackages covers the S3L rule for unread options (row
// TS-C1, a tsconfig.json extending a package outside the repository): a
// bare import stays refused (config_unread, no import edge, External
// false), and family consumers read it as the package (Package, the
// export bindings) exactly when rule (ii) of row TS-I5 holds for its
// package name and no workspace link or scanned package.json name may make
// it a repository file. Node built-ins, aliases, undeclared names and
// relative imports never are.
func TestUnreadDeclaredPackages(t *testing.T) {
	r := moduleResolver(map[string]string{
		"package.json":            `{"workspaces":["packages/*"],"dependencies":{"typeorm":"1","@s/pkg":"1","shadow":"1","member":"1"}}`,
		"app/tsconfig.json":       `{"extends":"@tsconfig/node20/tsconfig.json"}`,
		"app/main.ts":             "",
		"app/local.ts":            "",
		"vendor/package.json":     `{"name":"shadow"}`,
		"packages/m/package.json": `{"name":"member"}`,
	})
	for _, c := range []struct {
		spec    string
		pkg     bool
		refusal string
	}{
		{"typeorm", true, unreadConfig},
		{"typeorm/browser", true, unreadConfig},
		{"@s/pkg", true, unreadConfig},
		{"undeclared", false, unreadConfig},
		{"fs", false, unreadConfig},
		{"node:fs", false, unreadConfig},
		{"@/alias", false, unreadConfig},
		{"~icons/x", false, unreadConfig},
		{"#internal", false, unreadConfig},
		{"shadow", false, unreadConfig},
		{"member", false, unreadConfig},
	} {
		got := r.Resolve("app/main.ts", c.spec)
		if got.External || got.File != "" || got.Refusal != c.refusal || got.Package() != c.pkg || got.DeclaredPackage != c.pkg {
			t.Errorf("%s: %+v, want package %v", c.spec, got, c.pkg)
		}
		b := r.ImportBinding("app/main.ts", c.spec, "X", "")
		if b.External != c.pkg || (c.pkg && b.File != c.spec) {
			t.Errorf("%s: binding %+v, want external %v", c.spec, b, c.pkg)
		}
	}
	if got := r.Resolve("app/main.ts", "./local"); got.File != "app/local.ts" || got.Package() {
		t.Errorf("relative import: %+v", got)
	}
}
