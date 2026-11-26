package drizzle

import (
	"reflect"
	"strings"
	"testing"
)

// lock is a pnpm 9 lockfile with one importer.
func lock(importer, orm, kit string) string {
	return `lockfileVersion: '9.0'
importers:
  ` + importer + `:
    dependencies:
      drizzle-orm:
        specifier: ^` + orm + `
        version: ` + orm + `(@types/pg@8.11.0)
    devDependencies:
      drizzle-kit:
        specifier: ^` + kit + `
        version: ` + kit + `
`
}

func manifestJSON(scripts string, deps ...string) string {
	return `{"name": "p", "scripts": {` + scripts + `}, "dependencies": {` + strings.Join(deps, ", ") + `}}`
}

const (
	ormDep   = `"drizzle-orm": "^0.45.4"`
	kitDep   = `"drizzle-kit": "^0.31.11"`
	tableSrc = "import { pgTable, text } from \"drizzle-orm/pg-core\";\nexport const t = pgTable(\"t\", { id: text() });\n"
)

func discover(t *testing.T, sources map[string]string) ([]*Package, *Input, func(code, file string, parts ...string) bool, func()) {
	t.Helper()
	in := input(t, sources)
	pkgs, diags := Discover(in)
	return pkgs, &in, func(code, file string, parts ...string) bool {
			return hasDiag(diags, code, file, parts...)
		}, func() {
			for _, l := range diagLines(diags) {
				t.Log(l)
			}
		}
}

// TestDiscoverDefaultConfig is kaneo's shape: the default config name in the
// package directory, run by `drizzle-kit generate` without a flag; the
// schema path relative to the package directory; pins from the workspace
// root lockfile; a stale schema file outside the schema.
func TestDiscoverDefaultConfig(t *testing.T) {
	pkgs, _, has, dump := discover(t, map[string]string{
		"pnpm-lock.yaml":        lock("apps/api", "0.45.4", "0.31.11"),
		"package.json":          `{"name": "root"}`,
		"apps/api/package.json": manifestJSON(`"db:generate": "drizzle-kit generate", "db:studio": "drizzle-kit studio"`, ormDep, kitDep),
		"apps/api/drizzle.config.ts": `import { type Config, defineConfig } from "drizzle-kit";
export default defineConfig({
  out: "./drizzle",
  schema: "./src/database/schema.ts",
  dialect: "postgresql",
  dbCredentials: { url: process.env.URL! },
}) satisfies Config;
`,
		"apps/api/src/database/schema.ts":    tableSrc,
		"apps/api/src/database/relations.ts": "import { relations } from \"drizzle-orm\";\nimport { t } from \"./schema\";\nexport const r = relations(t, () => ({}));\n",
		"apps/api/auth-schema.ts":            tableSrc + "export const u = pgTable(\"u\", { id: text() });\n",
	})
	if len(pkgs) != 1 || len(pkgs[0].Configs) != 1 {
		dump()
		t.Fatalf("packages %+v", pkgs)
	}
	p, c := pkgs[0], pkgs[0].Configs[0]
	if p.Dir != "apps/api" || !reflect.DeepEqual(p.Pins, map[string]any{"drizzle-orm": "0.45.4", "drizzle-kit": "0.31.11"}) || p.Lockfile != "pnpm-lock.yaml" {
		t.Errorf("package %+v", p)
	}
	if c.Path != "apps/api/drizzle.config.ts" || c.Named != "default" || c.SetKey() != "models/drizzle/apps/api/drizzle.config.ts" || c.Dialect != "postgresql" || !c.CasingKnown || c.Casing != "" || !c.SchemaFilterKnown || c.SchemaFilter != nil {
		t.Errorf("config %+v", c)
	}
	if !reflect.DeepEqual(c.Scripts, []string{"db:generate", "db:studio"}) {
		t.Errorf("scripts %v", c.Scripts)
	}
	if !reflect.DeepEqual(c.Files, []string{"apps/api/src/database/schema.ts"}) || !reflect.DeepEqual(moduleFiles(c), c.Files) {
		t.Errorf("files %v, modules %v", c.Files, moduleFiles(c))
	}
	if !has(codeUnread, c.Path, "apps/api/auth-schema.ts (2)", "outside the configured schema") {
		dump()
		t.Error("no marker for the stale schema file")
	}
	if has(codeUnread, c.Path, "relations.ts") {
		t.Error("relations.ts calls no table builder and must not be in the marker")
	}
}

// TestDiscoverScriptConfig is dokploy's shape: a config
// outside the package root named by --config in six scripts, a schema path
// that exists only relative to the package directory, an `export *` barrel
// that reaches another workspace package through tsconfig paths, and that
// package's own dependency on drizzle-orm without a config.
func TestDiscoverScriptConfig(t *testing.T) {
	scripts := `"studio": "drizzle-kit studio --config ./server/db/drizzle.config.ts", "migration:generate": "drizzle-kit generate --config=./server/db/drizzle.config.ts", "generate:other": "pnpm dlx  @better-auth/cli generate --output x.ts --config src/lib/auth-cli.ts"`
	pkgs, _, has, dump := discover(t, map[string]string{
		"pnpm-lock.yaml":             lock("apps/dokploy", "0.45.2", "0.31.9") + "  packages/server:\n    dependencies:\n      drizzle-orm:\n        specifier: 0.45.2\n        version: 0.45.2\n",
		"apps/dokploy/package.json":  manifestJSON(scripts, `"drizzle-orm": "0.45.2"`, `"drizzle-kit": "^0.31.4"`),
		"apps/dokploy/tsconfig.json": `{"compilerOptions": {"baseUrl": ".", "paths": {"@srv/*": ["../../packages/server/src/*"]}}}`,
		"apps/dokploy/server/db/drizzle.config.ts": `import { defineConfig } from "drizzle-kit";
export default defineConfig({ schema: "./server/db/schema/index.ts", dialect: "postgresql", out: "drizzle" });
`,
		"apps/dokploy/server/db/schema/index.ts": "export * from \"@srv/db/schema\";\n",
		"packages/server/package.json":           manifestJSON(`"generate:drizzle": "pnpm dlx @better-auth/cli generate --config src/lib/auth-cli.ts"`, `"drizzle-orm": "0.45.2"`),
		"packages/server/src/db/schema/index.ts": "export * from \"./a\";\nexport * from \"./b\";\nexport type { X } from \"./types\";\nexport * as ns from \"./c\";\n",
		"packages/server/src/db/schema/a.ts":     tableSrc,
		"packages/server/src/db/schema/b.ts":     strings.ReplaceAll(tableSrc, "\"t\"", "\"b\""),
		"packages/server/src/db/schema/c.ts":     tableSrc,
		"packages/server/src/db/schema/types.ts": "export type X = string;\n",
		"packages/server/auth-schema2.ts":        tableSrc,
		"packages/server/auth-schema.ts":         "// import { pgTable } from \"drizzle-orm/pg-core\";\n// export const t = pgTable(\"t\", {});\n",
		"apps/schedules/package.json":            manifestJSON("", `"drizzle-orm": "0.45.2"`),
		"apps/schedules/index.ts":                "import { eq } from \"drizzle-orm\";\nexport const f = eq;\n",
	})
	byDir := map[string]*Package{}
	for _, p := range pkgs {
		byDir[p.Dir] = p
	}
	app := byDir["apps/dokploy"]
	if len(pkgs) != 3 || app == nil || len(app.Configs) != 1 {
		dump()
		t.Fatalf("packages %+v", pkgs)
	}
	c := app.Configs[0]
	if c.Path != "apps/dokploy/server/db/drizzle.config.ts" || c.Named != "--config" || !reflect.DeepEqual(c.Scripts, []string{"migration:generate", "studio"}) {
		t.Errorf("config %+v", c)
	}
	if !reflect.DeepEqual(app.Pins, map[string]any{"drizzle-orm": "0.45.2", "drizzle-kit": "0.31.9"}) {
		t.Errorf("pins %v", app.Pins)
	}
	want := []string{"apps/dokploy/server/db/schema/index.ts", "packages/server/src/db/schema/index.ts", "packages/server/src/db/schema/a.ts", "packages/server/src/db/schema/b.ts"}
	if !reflect.DeepEqual(moduleFiles(c), want) {
		t.Errorf("modules %v, want %v", moduleFiles(c), want)
	}
	// c.ts is reached only through `export * as ns`, so it is outside too.
	if !has(codeUnread, c.Path, "2 file(s) with 2 table builder call(s): packages/server/auth-schema2.ts (1), packages/server/src/db/schema/c.ts (1) outside") {
		dump()
		t.Error("no marker for auth-schema2.ts and c.ts")
	}
	server := byDir["packages/server"]
	if len(server.Configs) != 0 || server.Pins != nil || server.Versions["drizzle-orm"] != "0.45.2" {
		t.Errorf("server package %+v", server)
	}
	if !has(codeNotProven, "packages/server/package.json", "no drizzle-kit config", "packages/server/auth-schema2.ts (1)", "packages/server/src/db/schema/a.ts (1)") {
		dump()
		t.Error("no no-config marker for packages/server")
	}
	if has(codeNotProven, "apps/schedules/package.json") {
		t.Error("a package without table builders gets no marker")
	}
}

func TestDiscoverNamespaceModuleMarker(t *testing.T) {
	// c.ts above is reached only through `export * as ns`: drizzle-kit sees
	// one namespace object, never its tables, so it is outside the schema.
	pkgs, _, has, dump := discover(t, map[string]string{
		"package.json":      manifestJSON(`"g": "drizzle-kit generate"`, ormDep, kitDep),
		"drizzle.config.ts": `export default { schema: "./db/index.ts", dialect: "postgresql" };`,
		"db/index.ts":       "export * as ns from \"./c\";\nexport { t as renamed } from \"./d\";\nexport type { u } from \"./e\";\n",
		"db/c.ts":           tableSrc,
		"db/d.ts":           tableSrc,
		"db/e.ts":           tableSrc,
	})
	c := pkgs[0].Configs[0]
	if !reflect.DeepEqual(moduleFiles(c), []string{"db/index.ts", "db/d.ts"}) || !reflect.DeepEqual(c.Modules[1].Names, []string{"t"}) || c.Modules[1].All {
		dump()
		t.Errorf("modules %+v", c.Modules)
	}
	if !has(codeUnread, "drizzle.config.ts", "db/c.ts (1), db/e.ts (1)") {
		dump()
		t.Error("namespace and type-only targets are not in the marker")
	}
	if pkgs[0].Pins != nil {
		t.Errorf("pins without a lockfile: %v", pkgs[0].Pins)
	}
}

// TestDiscoverSchemaPaths covers the glob 8 subset: a literal file, `*`,
// `**`, `?`, arrays, a matched directory (direct files only), dot files,
// other extensions (marker) and unsupported syntax (unproven).
func TestDiscoverSchemaPaths(t *testing.T) {
	files := []string{
		"pkg/db/a.ts", "pkg/db/b.ts", "pkg/db/notes.sql", "pkg/db/.hidden.ts", "pkg/db/sub/c.ts", "pkg/db/sub/deep/d.mts",
		"pkg/db/.dot/e.ts", "pkg/other/x1.ts", "pkg/other/x22.ts", "pkg/drizzle.config.ts", "top.ts",
	}
	cases := []struct {
		patterns []string
		want     []string
		why      string
	}{
		{[]string{"./db/a.ts"}, []string{"pkg/db/a.ts"}, ""},
		{[]string{"db/*.ts"}, []string{"pkg/db/a.ts", "pkg/db/b.ts"}, ""},
		{[]string{"./db"}, []string{"pkg/db/a.ts", "pkg/db/b.ts", "pkg/db/notes.sql", "pkg/db/.hidden.ts"}, ""},
		{[]string{"./db/**/*.ts"}, []string{"pkg/db/a.ts", "pkg/db/b.ts", "pkg/db/sub/c.ts"}, ""},
		{[]string{"./db/**/*"}, []string{"pkg/db/a.ts", "pkg/db/b.ts", "pkg/db/notes.sql", "pkg/db/sub/c.ts", "pkg/db/sub/deep/d.mts"}, ""},
		{[]string{"./db/.dot/*.ts"}, []string{"pkg/db/.dot/e.ts"}, ""},
		{[]string{"other/x?.ts", "db/a.ts"}, []string{"pkg/other/x1.ts", "pkg/db/a.ts"}, ""},
		{[]string{"../top.ts"}, []string{"top.ts"}, ""},
		{[]string{"db/missing.ts"}, nil, ""},
		{[]string{"db/{a,b}.ts"}, nil, "braces"},
		{[]string{"db/[ab].ts"}, nil, "classes"},
		{[]string{"!db/a.ts"}, nil, "negation"},
		{[]string{"db/@(a).ts"}, nil, "extglob"},
		{[]string{"/abs/a.ts"}, nil, "cannot be placed"},
		{[]string{"../../a.ts"}, nil, "outside the repository"},
		{[]string{"db/*/../a.ts"}, nil, "after a wildcard"},
	}
	for _, tc := range cases {
		got, why := resolveSchema(sortedCopy(files), "pkg", tc.patterns)
		if tc.why != "" {
			if !strings.Contains(why, tc.why) || got != nil {
				t.Errorf("%v: got %v, %q; want refusal %q", tc.patterns, got, why, tc.why)
			}
			continue
		}
		if why != "" || !reflect.DeepEqual(sortedCopy(got), sortedCopy(tc.want)) {
			t.Errorf("%v: got %v (%q), want %v", tc.patterns, got, why, tc.want)
		}
	}
}

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TestDiscoverConfigKeys covers the config object: JSON configs, arrays
// and globs, casing and schemaFilter literal and not, the D dialects, and
// every unproven form.
func TestDiscoverConfigKeys(t *testing.T) {
	cases := []struct {
		name, config, file string
		check              func(t *testing.T, c *Config)
		reason             string
	}{
		{name: "json", file: "drizzle.config.json", config: `{"schema": ["./db/a.ts", "./db/*.ts"], "dialect": "sqlite", "casing": "snake_case", "schemaFilter": "app"}`, check: func(t *testing.T, c *Config) {
			if c.Dialect != "sqlite" || c.Casing != "snake_case" || !reflect.DeepEqual(c.SchemaFilter, []string{"app"}) || !reflect.DeepEqual(c.Files, []string{"db/a.ts", "db/b.ts"}) {
				t.Errorf("%+v", c)
			}
		}},
		{name: "object", config: "export default { schema: `./db/a.ts`, dialect: \"turso\", casing: \"camelCase\", schemaFilter: [\"public\", \"app\"] } as Config;", check: func(t *testing.T, c *Config) {
			if c.Dialect != "turso" || c.Casing != "camelCase" || !reflect.DeepEqual(c.SchemaFilter, []string{"public", "app"}) || !reflect.DeepEqual(c.Files, []string{"db/a.ts"}) {
				t.Errorf("%+v", c)
			}
		}},
		{name: "casing not literal", config: "const k = \"snake_case\";\nexport default { schema: \"./db/a.ts\", dialect: \"mysql\", casing: k, schemaFilter: f() };", check: func(t *testing.T, c *Config) {
			if !c.Proven() || c.CasingKnown || c.SchemaFilterKnown || c.Dialect != "mysql" {
				t.Errorf("%+v", c)
			}
		}},
		{name: "casing unknown value", config: "export default { schema: \"./db/a.ts\", dialect: \"postgresql\", casing: \"kebab\" };", check: func(t *testing.T, c *Config) {
			if !c.Proven() || c.CasingKnown {
				t.Errorf("%+v", c)
			}
		}},
		{name: "later key wins", config: "export default { schema: \"./db/x.ts\", dialect: \"postgresql\", schema: \"./db/b.ts\" };", check: func(t *testing.T, c *Config) {
			if !reflect.DeepEqual(c.Files, []string{"db/b.ts"}) {
				t.Errorf("%+v", c)
			}
		}},
		{name: "dialect not literal", config: "const d = \"postgresql\";\nexport default { schema: \"./db/a.ts\", dialect: d };", reason: "dialect of drizzle.config.ts is not a string literal"},
		{name: "dialect missing", config: "export default { schema: \"./db/a.ts\" };", reason: "has no dialect"},
		{name: "dialect bogus", config: "export default { schema: \"./db/a.ts\", dialect: \"oracle\" };", reason: "not a drizzle-kit dialect"},
		{name: "schema not literal", config: "export default { schema: [\"./db/a.ts\", x], dialect: \"postgresql\" };", reason: "schema of drizzle.config.ts is not a string literal"},
		{name: "schema template", config: "export default { schema: `./db/${n}.ts`, dialect: \"postgresql\" };", reason: "is not a string literal"},
		{name: "schema missing", config: "export default { dialect: \"postgresql\" };", reason: "has no schema"},
		{name: "spread", config: "const base = { dialect: \"postgresql\" };\nexport default { ...base, schema: \"./db/a.ts\" };", reason: "spread or a computed key"},
		{name: "not an object", config: "const c = { schema: \"./db/a.ts\", dialect: \"postgresql\" };\nexport default c;", reason: "is not `defineConfig({...})` or an object literal"},
		{name: "other defineConfig", config: "import { defineConfig } from \"./mine\";\nexport default defineConfig({ schema: \"./db/a.ts\", dialect: \"postgresql\" });", reason: "is not `defineConfig({...})`"},
		{name: "commonjs", config: "module.exports = { schema: \"./db/a.ts\", dialect: \"postgresql\" };", reason: "no `export default`"},
		{name: "glob unsupported", config: "export default { schema: \"./db/{a,b}.ts\", dialect: \"postgresql\" };", reason: "braces"},
		{name: "no match", config: "export default { schema: \"./db/none/*.ts\", dialect: \"postgresql\" };", reason: "matches no .ts"},
		{name: "only sql", config: "export default { schema: \"./db/*.sql\", dialect: \"postgresql\" };", reason: "matches no .ts"},
		{name: "json broken", file: "drizzle.config.json", config: `[1]`, reason: "not a JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := tc.file
			if file == "" {
				file = "drizzle.config.ts"
			}
			pkgs, _, has, dump := discover(t, map[string]string{
				"package.json": manifestJSON("", ormDep, kitDep),
				file:           tc.config,
				"db/a.ts":      tableSrc,
				"db/b.ts":      tableSrc,
				"db/x.sql":     "select 1;",
			})
			c := pkgs[0].Configs[0]
			if tc.reason != "" {
				if c.Proven() || !strings.Contains(c.Reason, tc.reason) || !has(codeNotProven, file, tc.reason) {
					dump()
					t.Errorf("reason %q, want %q", c.Reason, tc.reason)
				}
				return
			}
			if !c.Proven() {
				t.Fatalf("not proven: %s", c.Reason)
			}
			tc.check(t, c)
		})
	}
}

func TestDiscoverUnsupportedDialect(t *testing.T) {
	for _, d := range []string{"singlestore", "gel"} {
		pkgs, _, has, _ := discover(t, map[string]string{
			"package.json":      manifestJSON("", ormDep, kitDep),
			"drizzle.config.ts": `export default { schema: "./a.ts", dialect: "` + d + `" };`,
			"a.ts":              tableSrc,
		})
		c := pkgs[0].Configs[0]
		if c.Proven() || !c.Unsupported || c.Dialect != d || !has(codeUnread, "drizzle.config.ts", "row D") {
			t.Errorf("%s: %+v", d, c)
		}
	}
}

// TestDiscoverScripts covers the scripts reader: default config order,
// flag configurations, a changed working directory, non-literal and
// colliding options, a --config that does not exist, and config files no
// package loads.
func TestDiscoverScripts(t *testing.T) {
	scripts := strings.Join([]string{
		`"a": "drizzle-kit push --schema ./db/a.ts --dialect postgresql"`,
		`"b": "dotenv -- drizzle-kit generate --dialect=postgresql --schema=./db/a.ts"`,
		`"c": "drizzle-kit generate --schema ./db/b.ts --dialect sqlite --casing snake_case"`,
		`"d": "cd db && drizzle-kit generate"`,
		`"e": "pnpm -C sub drizzle-kit generate"`,
		`"f": "drizzle-kit generate --config $CONFIG"`,
		`"g": "drizzle-kit generate --config ./alt.config.ts --schema ./db/a.ts"`,
		`"h": "drizzle-kit generate --config ./missing.ts"`,
		`"i": "drizzle-kit generate --schema ./db/a.ts"`,
		`"j": "tsc && node_modules/.bin/drizzle-kit migrate"`,
		`"k": "drizzle-kit generate --config '../outside/c.ts' && echo done"`,
	}, ", ")
	pkgs, _, has, dump := discover(t, map[string]string{
		"package.json":         manifestJSON(scripts, ormDep, kitDep),
		"drizzle.config.js":    `export default { schema: "./db/a.ts", dialect: "postgresql" };`,
		"drizzle.config.json":  `{"schema": "./db/b.ts", "dialect": "postgresql"}`,
		"alt.config.ts":        `import { defineConfig } from "drizzle-kit"; export default defineConfig({ schema: "./db/a.ts", dialect: "postgresql" });`,
		"db/a.ts":              tableSrc,
		"db/b.ts":              tableSrc,
		"db/drizzle.config.ts": `export default { schema: "./a.ts", dialect: "postgresql" };`,
		"outside/c.ts":         "export {};",
	})
	var roots []string
	for _, c := range pkgs[0].Configs {
		roots = append(roots, c.Root+" "+c.Named+" "+strings.Join(c.Scripts, ","))
	}
	want := []string{
		"drizzle.config.js default j",
		"package.json#a flags a,b",
		"package.json#c flags c",
	}
	if !reflect.DeepEqual(roots, want) {
		dump()
		t.Errorf("configs %q, want %q", roots, want)
	}
	if c := pkgs[0].Configs[2]; c.Casing != "snake_case" || c.Dialect != "sqlite" || !reflect.DeepEqual(c.Files, []string{"db/b.ts"}) || c.SetKey() != "models/drizzle/package.json#c" {
		t.Errorf("flag config %+v", c)
	}
	for _, msg := range []string{
		`script "d" runs drizzle-kit in another working directory`,
		`script "e" runs drizzle-kit in another working directory`,
		`script "f" passes a non-literal --config`,
		`script "g" passes --config with other options`,
		`script "h" names config missing.ts, which does not exist`,
		`script "i" passes drizzle-kit options without a literal --schema and --dialect`,
		`script "k" names config "../outside/c.ts", which cannot be placed`,
	} {
		if !has(codeNotProven, "package.json", msg) {
			dump()
			t.Errorf("no marker %q", msg)
		}
	}
	for _, f := range []string{"drizzle.config.json", "db/drizzle.config.ts"} {
		if !has(codeUnread, f, "not read") {
			dump()
			t.Errorf("no unread marker for %s", f)
		}
	}
	if has(codeUnread, "alt.config.ts") || has(codeUnread, "drizzle.config.js", "not read", "default config") {
		dump()
		t.Error("a loaded config is marked unread")
	}
}

// TestDiscoverVersions: drizzle-orm 1.x and its release candidates are
// refused (no configs read); a missing lockfile gives pins null with
// claims kept; a 0.x range without a lockfile is read.
func TestDiscoverVersions(t *testing.T) {
	config := `export default { schema: "./a.ts", dialect: "postgresql" };`
	for _, tc := range []struct {
		name    string
		sources map[string]string
		refused bool
	}{
		{"rc locked", map[string]string{"pnpm-lock.yaml": lock(".", "1.0.0-rc.1", "1.0.0-rc.1")}, true},
		{"1.x range", map[string]string{"package.json": manifestJSON("", `"drizzle-orm": "^1.0.0"`, kitDep)}, true},
		{"0.x range", map[string]string{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources := map[string]string{"package.json": manifestJSON("", ormDep, kitDep), "drizzle.config.ts": config, "a.ts": tableSrc}
			for k, v := range tc.sources {
				sources[k] = v
			}
			pkgs, _, has, dump := discover(t, sources)
			p := pkgs[0]
			if tc.refused != (p.Refused != "") || tc.refused && (len(p.Configs) != 0 || !has(codeNotProven, "package.json", "0.x only")) {
				dump()
				t.Errorf("package %+v", p)
			}
			if !tc.refused && (len(p.Configs) != 1 || !p.Configs[0].Proven() || p.Pins != nil) {
				t.Errorf("package %+v", p)
			}
		})
	}
}

// TestDiscoverUnresolvedReexport: a re-export that resolves to no file is
// reported; the rest of the schema is kept.
func TestDiscoverUnresolvedReexport(t *testing.T) {
	pkgs, _, has, dump := discover(t, map[string]string{
		"package.json":      manifestJSON("", ormDep, kitDep),
		"drizzle.config.ts": `export default { schema: "./db/index.ts", dialect: "postgresql" };`,
		"db/index.ts":       "export * from \"./a\";\nexport * from \"./gone\";\nexport * from \"drizzle-orm\";\n",
		"db/a.ts":           tableSrc,
	})
	c := pkgs[0].Configs[0]
	if !reflect.DeepEqual(moduleFiles(c), []string{"db/index.ts", "db/a.ts"}) || len(c.Unresolved) != 1 || !has(codeUnread, "drizzle.config.ts", `re-export of "./gone" at db/index.ts:2`) {
		dump()
		t.Errorf("modules %v, unresolved %v", moduleFiles(c), c.Unresolved)
	}
}

// TestDiscoverScopeForms checks that discovery tags every DR-K1 form it
// reads (ScopeForms) and nothing outside the contract.
func TestDiscoverScopeForms(t *testing.T) {
	in := input(t, map[string]string{
		"app/package.json":      manifestJSON(`"g": "drizzle-kit generate --config ./cfg/alt.ts"`, ormDep, kitDep),
		"app/tsconfig.json":     `{"compilerOptions": {"paths": {"@lib/*": ["../lib/*"]}}}`,
		"app/drizzle.config.ts": `export default { schema: ["./db/index.ts", "./db/extra/*.ts"], dialect: "postgresql", casing: "snake_case", schemaFilter: ["public"] };`,
		"app/cfg/alt.ts":        `import { defineConfig } from "drizzle-kit"; export default defineConfig({ schema: "./db/index.ts", dialect: "sqlite" });`,
		"app/db/index.ts":       "export * from \"./a\";\nexport { t as lib } from \"@lib/t\";\n",
		"app/db/a.ts":           tableSrc,
		"app/db/extra/e.ts":     tableSrc,
		"lib/t.ts":              tableSrc,
	})
	var tags []string
	seen := map[string]bool{}
	in.Tag = func(r string) {
		if !seen[r] {
			seen[r] = true
			tags = append(tags, r)
		}
	}
	pkgs, diags := Discover(in)
	if len(pkgs) != 1 || len(pkgs[0].Configs) != 2 {
		t.Fatalf("packages %+v, diagnostics %v", pkgs, diagLines(diags))
	}
	for _, f := range ScopeForms {
		if !seen["DR-K1/"+f] {
			t.Errorf("form %q not reached", f)
		}
	}
	for _, tag := range tags {
		r, form, _ := strings.Cut(tag, "/")
		ok := false
		for _, row := range Contract {
			if row.ID != r {
				continue
			}
			ok = form == ""
			for _, f := range row.Forms {
				ok = ok || f == form
			}
		}
		if !ok {
			t.Errorf("tag %q is not a contract row or listed form", tag)
		}
	}
}

// TestDiscoverOut: a config's drizzle-kit migrations folder (literal
// `out`, default "drizzle") is resolved against the package directory and
// reported as a schema_model_migrations marker, proven config or not; a
// non-literal `out`, an unreadable config or a folder outside the
// repository names none.
func TestDiscoverOut(t *testing.T) {
	cases := []struct {
		name, config, want string
	}{
		{"default", `export default { dialect: "postgresql", schema: "./db/a.ts" };`, "app/drizzle"},
		{"literal", `export default { dialect: "postgresql", schema: "./db/a.ts", out: "./migrations/pg" };`, "app/migrations/pg"},
		{"parent", `export default { dialect: "postgresql", schema: "./db/a.ts", out: "../shared/drizzle" };`, "shared/drizzle"},
		{"unproven", `export default { schema: x, out: "./sql" };`, "app/sql"},
		{"not literal", `export default { dialect: "postgresql", schema: "./db/a.ts", out: dir };`, ""},
		{"outside", `export default { dialect: "postgresql", schema: "./db/a.ts", out: "../../elsewhere" };`, ""},
		{"absolute", `export default { dialect: "postgresql", schema: "./db/a.ts", out: "/tmp/drizzle" };`, ""},
		{"spread", `export default { ...base, dialect: "postgresql", schema: "./db/a.ts" };`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkgs, diags := Discover(input(t, map[string]string{
				"app/package.json":      manifestJSON("", ormDep, kitDep),
				"app/drizzle.config.ts": tc.config,
				"app/db/a.ts":           tableSrc,
			}))
			c := pkgs[0].Configs[0]
			var got []string
			for _, d := range diags {
				if d.Code == codeMigrations {
					got = append(got, d.File+" "+d.Message)
				}
			}
			var want []string
			if tc.want != "" {
				want = []string{"app/drizzle.config.ts app/drizzle.config.ts: " + migrationsMarker + tc.want}
			}
			if c.Out != tc.want || !reflect.DeepEqual(got, want) {
				t.Errorf("out %q, markers %v; want %q", c.Out, got, tc.want)
			}
		})
	}
}
