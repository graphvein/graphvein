package prisma

import (
	"reflect"
	"strings"
	"testing"
)

const pnpmLock = `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      '@prisma/client':
        specifier: ^7.10.0
        version: 7.10.0(prisma@7.10.0(typescript@6.0.3))(typescript@6.0.3)
      prisma:
        specifier: ^7.10.0
        version: 7.10.0(@types/node@26.5.1)(typescript@6.0.3)

  packages/backend:
    devDependencies:
      prisma:
        specifier: 7.9.1
        version: 7.9.1(typescript@5.9.3)
`

func discoverOne(t *testing.T, sources map[string]string, dir string) (*Package, []string) {
	t.Helper()
	src := map[string][]byte{}
	for k, v := range sources {
		src[k] = []byte(v)
	}
	pkgs, diags := Discover(src)
	var codes []string
	for _, d := range diags {
		codes = append(codes, d.Code+" "+d.File)
	}
	for _, p := range pkgs {
		if p.Dir == dir {
			return p, codes
		}
	}
	t.Fatalf("no package %s in %+v", dir, pkgs)
	return nil, nil
}

// TestDiscoverPnpmEnvironmentDocument: a pnpm lockfile whose first YAML
// document is pnpm's environment document (kftray, kaneo) pins the version
// from the lockfile document (shared reader, internal/extract/models/lockfile).
func TestDiscoverPnpmEnvironmentDocument(t *testing.T) {
	env := "---\nlockfileVersion: '9.0'\n\nimporters:\n\n  .:\n    configDependencies: {}\n    packageManagerDependencies:\n      pnpm:\n        specifier: 10.0.0\n        version: 10.0.0\n\n---\n"
	p, _ := discoverOne(t, map[string]string{
		"package.json":         `{"dependencies": {"prisma": "^7.10.0"}}`,
		"pnpm-lock.yaml":       env + pnpmLock,
		"prisma/schema.prisma": "",
	}, ".")
	if p.Version != "7.10.0" || !reflect.DeepEqual(p.Pins, map[string]any{"prisma": "7.10.0"}) {
		t.Errorf("two-document lockfile: version %q pins %v", p.Version, p.Pins)
	}
}

// TestDiscoverCorpusShapes covers the two corpus layouts:
// umami (config without schema, default lookup finds prisma/schema.prisma,
// pnpm lockfile beside the package) and hoppscotch (literal config schema,
// lockfile at the workspace root, importer of the package).
func TestDiscoverCorpusShapes(t *testing.T) {
	cfg := "import 'dotenv/config';\nimport { defineConfig, env } from 'prisma/config';\n\nexport default defineConfig({\n  datasource: {\n    url: env('DATABASE_URL'),\n  },\n});\n"
	p, codes := discoverOne(t, map[string]string{
		"package.json":         `{"dependencies": {"prisma": "^7.10.0", "@prisma/client": "^7.10.0"}}`,
		"pnpm-lock.yaml":       pnpmLock,
		"prisma.config.ts":     cfg,
		"prisma/schema.prisma": "",
	}, ".")
	want := &Package{Dir: ".", Manifest: "package.json", Version: "7.10.0", Lockfile: "pnpm-lock.yaml", Pins: map[string]any{"prisma": "7.10.0"}, Major: 7,
		Config: "prisma.config.ts", SchemaPath: "prisma/schema.prisma", Files: []string{"prisma/schema.prisma"}}
	if !reflect.DeepEqual(p, want) || len(codes) != 0 {
		t.Errorf("umami shape %+v, diagnostics %v", p, codes)
	}
	hcfg := "export default defineConfig({\n  schema: 'prisma/schema.prisma',\n  migrations: { path: 'prisma/migrations' },\n  datasource: { url: env('DATABASE_URL') },\n});\n"
	p, codes = discoverOne(t, map[string]string{
		"package.json":                          `{"pnpm": {"onlyBuiltDependencies": ["prisma", "@prisma/client"]}}`,
		"pnpm-lock.yaml":                        pnpmLock,
		"packages/backend/package.json":         `{"devDependencies": {"prisma": "7.9.1"}}`,
		"packages/backend/prisma.config.ts":     hcfg,
		"packages/backend/prisma/schema.prisma": "",
	}, "packages/backend")
	if p.Version != "7.9.1" || p.Lockfile != "pnpm-lock.yaml" || p.Config != "packages/backend/prisma.config.ts" ||
		!reflect.DeepEqual(p.Files, []string{"packages/backend/prisma/schema.prisma"}) || p.Migrations != "packages/backend/prisma/migrations" ||
		!reflect.DeepEqual(codes, []string{codeMigrations + " packages/backend/prisma.config.ts"}) {
		t.Errorf("hoppscotch shape %+v, diagnostics %v", p, codes)
	}
}

// TestDiscoverMigrationsPath covers a Prisma 7 config's migrations.path
// (T018g1 G1G): a literal is resolved against the config's directory and
// marked once per config; any other shape, or a path outside the
// repository, gives no marker (the server's default rule applies).
func TestDiscoverMigrationsPath(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"literal", "export default defineConfig({ migrations: { path: 'db/migrations' } })", "app/db/migrations"},
		{"literal with seed, schema not literal", "export default defineConfig({ schema: s, migrations: { seed: 'tsx seed.ts', path: `../shared/migrations` } })", "shared/migrations"},
		{"not literal", "export default defineConfig({ migrations: { path: path.join('db', 'm') } })", ""},
		{"migrations not an object literal", "export default defineConfig({ migrations })", ""},
		{"migrations from a variable", "export default defineConfig({ migrations: m })", ""},
		{"spread in migrations", "export default defineConfig({ migrations: { path: 'db', ...extra } })", ""},
		{"spread after migrations", "export default defineConfig({ migrations: { path: 'db' }, ...base })", ""},
		{"absent", "export default defineConfig({})", ""},
		{"outside the repository", "export default defineConfig({ migrations: { path: '../../m' } })", ""},
		{"absolute", "export default defineConfig({ migrations: { path: '/srv/m' } })", ""},
		{"unreadable config", "module.exports = { migrations: { path: 'db' } }", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, codes := discoverOne(t, map[string]string{
				"app/package.json":         `{"dependencies": {"prisma": "^7.10.0"}}`,
				"app/pnpm-lock.yaml":       pnpmLock,
				"app/prisma.config.ts":     tc.config,
				"app/prisma/schema.prisma": "",
			}, "app")
			marked := 0
			for _, c := range codes {
				if c == codeMigrations+" app/prisma.config.ts" {
					marked++
				}
			}
			if p.Migrations != tc.want || marked != map[bool]int{true: 1, false: 0}[tc.want != ""] {
				t.Errorf("migrations %q, diagnostics %v", p.Migrations, codes)
			}
		})
	}
}

// TestReadConfigMigrations covers the migrations.path reader directly.
func TestReadConfigMigrations(t *testing.T) {
	for _, tc := range []struct {
		src   string
		state int
		value string
	}{
		{`export default defineConfig({ migrations: { path: "m" } })`, configLiteral, "m"},
		{`export default { "migrations": { "path": 'm', }, } satisfies PrismaConfig`, configLiteral, "m"},
		{`export default defineConfig({ migrations: { seed: "x" } })`, configAbsent, ""},
		{`export default defineConfig({ schema: "a" })`, configAbsent, ""},
		{`export default defineConfig({ migrations: { path: "a" + "b" } })`, configNotLiteral, ""},
		{`export default defineConfig({ migrations: { ["path"]: "m" } })`, configNotLiteral, ""},
		{`export default defineConfig({ migrations: { path } })`, configNotLiteral, ""},
		{`export default defineConfig({ migrations: { path: "m" } as const })`, configNotLiteral, ""},
		{`export default defineConfig({ [k]: { path: "m" } })`, configNotLiteral, ""},
		{`export default defineConfig({ migrations: { path: "m" }`, configUnreadable, ""},
		{`export default defineConfig(base)`, configUnreadable, ""},
	} {
		state, value := readConfigMigrations(tc.src)
		if state != tc.state || value != tc.value {
			t.Errorf("%s: %d %q, want %d %q", tc.src, state, value, tc.state, tc.value)
		}
	}
}

// TestDiscoverRules covers the discovery rule's branches.
func TestDiscoverRules(t *testing.T) {
	lock := pnpmLock
	root := `{"dependencies": {"prisma": "^7.10.0"}}`
	for _, tc := range []struct {
		name    string
		sources map[string]string
		files   []string
		folder  bool
		reason  string
		config  string
	}{
		{"schema.prisma before prisma/schema.prisma (A8)", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "schema.prisma": "", "prisma/schema.prisma": ""},
			[]string{"schema.prisma"}, false, "", ""},
		{"no schema", map[string]string{"package.json": root, "pnpm-lock.yaml": lock}, nil, false, "neither schema.prisma nor prisma/schema.prisma", ""},
		{"package.json prisma key ignored", map[string]string{"package.json": `{"dependencies": {"prisma": "^7.10.0"}, "prisma": {"schema": "db/s.prisma"}}`, "pnpm-lock.yaml": lock, "db/s.prisma": ""},
			nil, false, "neither", ""},
		{"config literal file", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": `export default defineConfig({ schema: "db/main.prisma" })`, "db/main.prisma": "", "schema.prisma": ""},
			[]string{"db/main.prisma"}, false, "", "prisma.config.ts"},
		{"config literal directory (multi-file)", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.mjs": "export default {\n  // the folder\n  schema: `prisma/schema`,\n}\n",
			"prisma/schema/a.prisma": "", "prisma/schema/sub/b.prisma": "", "prisma/schema/readme.md": ""},
			[]string{"prisma/schema/a.prisma", "prisma/schema/sub/b.prisma"}, true, "", "prisma.config.mjs"},
		{"config non-literal", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "const s = 'x.prisma'\nexport default defineConfig({ schema: s })", "schema.prisma": ""},
			nil, false, "not a string literal", "prisma.config.ts"},
		{"config template with substitution", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ schema: `${dir}/s.prisma` })", "schema.prisma": ""},
			nil, false, "not a string literal", "prisma.config.ts"},
		{"config shorthand", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ schema })", "schema.prisma": ""},
			nil, false, "not a string literal", "prisma.config.ts"},
		{"config spread after schema", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ schema: 'a.prisma', ...base })", "a.prisma": ""},
			nil, false, "not a string literal", "prisma.config.ts"},
		{"config schema after spread", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ ...base, schema: 'a.prisma' })", "a.prisma": ""},
			[]string{"a.prisma"}, false, "", "prisma.config.ts"},
		{"config unreadable", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "const c = defineConfig({ schema: 'a.prisma' })\nexport default c\n", "a.prisma": ""},
			nil, false, "the reader can read", "prisma.config.ts"},
		{"config cjs unreadable", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.cjs": "module.exports = { schema: 'a.prisma' }", "a.prisma": ""},
			nil, false, "the reader can read", "prisma.config.cjs"},
		{"config path missing", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ schema: 'nope.prisma' })"},
			nil, false, "does not exist", "prisma.config.ts"},
		{"config path outside the repository", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.ts": "export default defineConfig({ schema: '../x.prisma' })"},
			nil, false, "outside the repository", "prisma.config.ts"},
		{"config without schema: lookup beside the config", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, ".config/prisma.ts": "export default defineConfig({})", ".config/schema.prisma": "", "schema.prisma": ""},
			[]string{".config/schema.prisma"}, false, "", ".config/prisma.ts"},
		{"config order: prisma.config before .config", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma.config.js": "export default {}", ".config/prisma.ts": "export default { schema: 'b.prisma' }", "schema.prisma": "", "b.prisma": ""},
			[]string{"schema.prisma"}, false, "", "prisma.config.js"},
		{"prisma7.config from 7.10.0", map[string]string{"package.json": root, "pnpm-lock.yaml": lock, "prisma7.config.ts": "export default { schema: 'seven.prisma' }", "prisma.config.ts": "export default {}", "seven.prisma": "", "schema.prisma": ""},
			[]string{"seven.prisma"}, false, "", "prisma7.config.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, codes := discoverOne(t, tc.sources, ".")
			if !reflect.DeepEqual(p.Files, tc.files) || p.Folder != tc.folder || p.Config != tc.config || !strings.Contains(p.Reason, tc.reason) {
				t.Errorf("package %+v", p)
			}
			notProven := false
			for _, c := range codes {
				if strings.HasPrefix(c, codeNotProven) {
					notProven = true
				}
			}
			if notProven != (tc.files == nil) {
				t.Errorf("diagnostics %v", codes)
			}
		})
	}
}

// TestDiscoverVersions covers pins and the version gate: prisma7.config is
// ignored before 7.10.0, an unproven version with a prisma7.config is not
// proven, other majors are refused, and npm and yarn lockfiles are read.
func TestDiscoverVersions(t *testing.T) {
	sub := `{"devDependencies": {"prisma": "7.9.1"}}`
	p, _ := discoverOne(t, map[string]string{"package.json": `{}`, "pnpm-lock.yaml": pnpmLock, "packages/backend/package.json": sub,
		"packages/backend/prisma7.config.ts": "export default { schema: 'seven.prisma' }", "packages/backend/seven.prisma": "", "packages/backend/schema.prisma": ""}, "packages/backend")
	if p.Config != "" || !reflect.DeepEqual(p.Files, []string{"packages/backend/schema.prisma"}) {
		t.Errorf("7.9.1 with prisma7.config %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"prisma": "^7.1.0"}}`, "prisma7.config.ts": "export default {}", "schema.prisma": ""}, ".")
	if p.Files != nil || p.Pins != nil || p.Major != 7 || !strings.Contains(p.Reason, "7.10.0 and later") {
		t.Errorf("unproven version with prisma7.config %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"prisma": "^7.1.0"}}`, "schema.prisma": ""}, ".")
	if p.Files == nil || p.Pins != nil || p.Major != 7 {
		t.Errorf("no lockfile, range major 7 %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"prisma": "latest"}}`, "schema.prisma": ""}, ".")
	if p.Files != nil || !strings.Contains(p.Reason, "version is not proven") {
		t.Errorf("unproven major %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"prisma": "^6.0.0"}}`, "prisma/schema.prisma": ""}, ".")
	if p.Major != 6 || !reflect.DeepEqual(p.Files, []string{"prisma/schema.prisma"}) {
		t.Errorf("Prisma 6 range %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"prisma": "^4.16.0"}}`, "prisma/schema.prisma": ""}, ".")
	if p.Files != nil || !strings.Contains(p.Reason, "Prisma 4") {
		t.Errorf("Prisma 4 %+v", p)
	}
	npm := `{"lockfileVersion": 3, "packages": {"": {}, "node_modules/prisma": {"version": "7.10.0"}, "apps/db/node_modules/prisma": {"version": "7.9.1"}}}`
	p, _ = discoverOne(t, map[string]string{"package-lock.json": npm, "package.json": `{}`, "apps/db/package.json": `{"dependencies": {"prisma": "^7"}}`, "apps/db/schema.prisma": ""}, "apps/db")
	if p.Version != "7.9.1" || p.Lockfile != "package-lock.json" {
		t.Errorf("npm nested %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package-lock.json": npm, "package.json": `{}`, "apps/web/package.json": `{"dependencies": {"prisma": "^7"}}`, "apps/web/schema.prisma": ""}, "apps/web")
	if p.Version != "7.10.0" {
		t.Errorf("npm hoisted %+v", p)
	}
	yarnClassic := "# yarn lockfile v1\n\n\"@prisma/client@^7.9.0\", prisma@^7.9.0:\n  version \"7.9.1\"\n  dependencies:\n    x \"1\"\n"
	p, _ = discoverOne(t, map[string]string{"yarn.lock": yarnClassic, "package.json": `{"dependencies": {"prisma": "^7.9.0"}}`, "schema.prisma": ""}, ".")
	if p.Version != "7.9.1" || p.Lockfile != "yarn.lock" {
		t.Errorf("yarn classic %+v", p)
	}
	yarnBerry := "__metadata:\n  version: 8\n\n\"prisma@npm:^7.10.0\":\n  version: 7.10.0\n  resolution: \"prisma@npm:7.10.0\"\n"
	p, _ = discoverOne(t, map[string]string{"yarn.lock": yarnBerry, "package.json": `{"dependencies": {"prisma": "^7.10.0"}}`, "schema.prisma": ""}, ".")
	if p.Version != "7.10.0" {
		t.Errorf("yarn berry %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"yarn.lock": yarnBerry, "package-lock.json": `{"lockfileVersion": 3, "packages": {"node_modules/prisma": {"version": "7.9.1"}}}`,
		"package.json": `{"dependencies": {"prisma": "^7.10.0"}}`, "schema.prisma": ""}, ".")
	if p.Version != "" || p.Pins != nil || p.Files == nil {
		t.Errorf("disagreeing lockfiles %+v", p)
	}
	p, _ = discoverOne(t, map[string]string{"package.json": `{"dependencies": {"@prisma/client": "^7.10.0"}}`, "pnpm-lock.yaml": pnpmLock, "schema.prisma": ""}, ".")
	if p.Pins != nil || p.Major != 7 || p.Files == nil {
		t.Errorf("client only %+v", p)
	}
}

// TestDiscoverMarkers checks the unread markers and that packages without
// the dependency are not discovered.
func TestDiscoverMarkers(t *testing.T) {
	src := map[string][]byte{
		"package.json":                []byte(`{"dependencies": {"prisma": "7.10.0"}, "prisma": {"schema": "x"}}`),
		"pnpm-lock.yaml":              []byte(pnpmLock),
		"schema.prisma":               []byte(""),
		"other/schema.prisma":         []byte(""),
		"tools/package.json":          []byte(`{"dependencies": {"left-pad": "1"}}`),
		"tools/schema.prisma":         []byte(""),
		"node_modules/x/package.json": []byte(`{"dependencies": {"prisma": "7.10.0"}}`),
		"bad/package.json":            []byte(`{not json`),
	}
	pkgs, diags := Discover(src)
	if len(pkgs) != 1 || pkgs[0].Dir != "." {
		t.Fatalf("packages %+v", pkgs)
	}
	var msgs []string
	for _, d := range diags {
		msgs = append(msgs, d.Code+": "+d.Message)
	}
	got := strings.Join(msgs, "\n")
	if len(diags) != 2 || !strings.Contains(got, `"prisma" key of package.json is not read`) || !strings.Contains(got, "2 .prisma file(s) are outside every discovered Prisma schema and not read: other/schema.prisma, tools/schema.prisma") {
		t.Errorf("diagnostics:\n%s", got)
	}
}

// TestReadConfigSchema covers the config reader's shapes directly.
func TestReadConfigSchema(t *testing.T) {
	for _, tc := range []struct {
		src   string
		state int
		value string
	}{
		{`export default defineConfig({ schema: "a.prisma" })`, configLiteral, "a.prisma"},
		{`export default defineConfig({ schema: 'a\'b.prisma', datasource: { url: env("X"), schema: 'not this' } })`, configLiteral, "a'b.prisma"},
		{`export default { "schema": "a.prisma" } satisfies PrismaConfig`, configLiteral, "a.prisma"},
		{"/* export default {schema: 'x'} */ export default defineConfig({ migrations: { path: 'm' } })", configAbsent, ""},
		{`export default defineConfig({ schema: "a" + ".prisma" })`, configNotLiteral, ""},
		{`export default defineConfig({ schema: path.join("a", "b") })`, configNotLiteral, ""},
		{`export default defineConfig({ ["schema"]: "a.prisma" })`, configNotLiteral, ""},
		{`export default defineConfig({ schema() { return "a" } })`, configNotLiteral, ""},
		{`export default defineConfig({ schema: "a\u0062.prisma" })`, configNotLiteral, ""},
		{`export default defineConfig(base)`, configUnreadable, ""},
		{`export default defineConfig({ schema: "a.prisma" }, extra)`, configUnreadable, ""},
		{`export default defineConfig({ schema: "a.prisma"`, configUnreadable, ""},
		{"export default defineConfig({ schema: 'open })", configUnreadable, ""},
		{`export default {}; export default {}`, configUnreadable, ""},
	} {
		state, value := readConfigSchema(tc.src)
		if state != tc.state || value != tc.value {
			t.Errorf("%s: %d %q, want %d %q", tc.src, state, value, tc.state, tc.value)
		}
	}
}

// TestDiscoverLegacy covers the Prisma 5 and 6 rules (discoverLegacy):
// package.json "prisma.schema", the 6.x config files by minor version,
// folders before and after prismaSchemaFolder became GA, and the default
// lookup's prisma/schema folder before 6.6.0.
func TestDiscoverLegacy(t *testing.T) {
	lock := func(v string) string {
		return `{"lockfileVersion": 3, "packages": {"": {}, "node_modules/prisma": {"version": "` + v + `"}}}`
	}
	pkg := func(extra string) string { return `{"devDependencies": {"prisma": "*"}` + extra + `}` }
	for _, tc := range []struct {
		name    string
		version string // locked version; "" for a range without a lockfile
		files   map[string]string
		want    []string
		config  string
		reason  string
	}{
		{"5 package.json file", "5.22.0", map[string]string{"package.json": pkg(`, "prisma": {"schema": "db/s.prisma"}`), "db/s.prisma": "", "schema.prisma": ""}, []string{"db/s.prisma"}, "", ""},
		{"5 package.json folder", "5.22.0", map[string]string{"package.json": pkg(`, "prisma": {"schema": "db"}`), "db/a.prisma": ""}, nil, "", "prismaSchemaFolder"},
		{"5 package.json not a string", "5.22.0", map[string]string{"package.json": pkg(`, "prisma": {"schema": 3}`), "schema.prisma": ""}, nil, "", "not a string"},
		{"5 package.json missing path", "5.22.0", map[string]string{"package.json": pkg(`, "prisma": {"schema": "nope.prisma"}`), "schema.prisma": ""}, nil, "", "does not exist"},
		{"5 package.json seed only", "5.22.0", map[string]string{"package.json": pkg(`, "prisma": {"seed": "x"}`), "schema.prisma": ""}, []string{"schema.prisma"}, "", ""},
		{"5 config ignored", "5.22.0", map[string]string{"package.json": pkg(""), "prisma.config.ts": "export default {schema: 'x.prisma'}", "x.prisma": "", "prisma/schema.prisma": ""}, []string{"prisma/schema.prisma"}, "", ""},
		{"5 root file before folder", "5.22.0", map[string]string{"package.json": pkg(""), "schema.prisma": "", "prisma/schema/a.prisma": ""}, []string{"schema.prisma"}, "", ""},
		{"5 file and folder conflict", "5.22.0", map[string]string{"package.json": pkg(""), "prisma/schema.prisma": "", "prisma/schema/a.prisma": ""}, nil, "", "conflicts"},
		{"5 folder only", "5.22.0", map[string]string{"package.json": pkg(""), "prisma/schema/a.prisma": ""}, nil, "", "preview feature"},
		{"6.2 config ignored", "6.2.0", map[string]string{"package.json": pkg(""), "prisma.config.ts": "export default {schema: 'x.prisma'}", "x.prisma": "", "schema.prisma": ""}, []string{"schema.prisma"}, "", ""},
		{"6.10 config not modelled", "6.10.0", map[string]string{"package.json": pkg(""), "prisma.config.ts": "export default {}", "schema.prisma": ""}, nil, "", "early-access"},
		{"6 range with a config", "", map[string]string{"package.json": `{"devDependencies": {"prisma": "^6.1.0"}}`, "prisma.config.ts": "export default {}", "schema.prisma": ""}, nil, "", "exact locked version"},
		{"6.19 config schema first", "6.19.3", map[string]string{"package.json": pkg(`, "prisma": {"schema": "p.prisma"}`), "prisma.config.ts": "export default defineConfig({schema: 'c.prisma'})", "c.prisma": "", "p.prisma": ""}, []string{"c.prisma"}, "prisma.config.ts", ""},
		{"6.19 config without schema", "6.19.3", map[string]string{"package.json": pkg(`, "prisma": {"schema": "p.prisma"}`), "prisma.config.ts": "export default defineConfig({})", "p.prisma": ""}, []string{"p.prisma"}, "prisma.config.ts", ""},
		{"6.19 config not literal", "6.19.3", map[string]string{"package.json": pkg(""), "prisma.config.ts": "export default defineConfig({schema: dir})", "schema.prisma": ""}, nil, "prisma.config.ts", "not a string literal"},
		{"6.19 package.json folder", "6.19.3", map[string]string{"package.json": pkg(`, "prisma": {"schema": "db"}`), "db/a.prisma": "", "db/b.prisma": ""}, []string{"db/a.prisma", "db/b.prisma"}, "", ""},
		{"6.6 file beside folder", "6.6.0", map[string]string{"package.json": pkg(""), "prisma/schema.prisma": "", "prisma/schema/a.prisma": ""}, []string{"prisma/schema.prisma"}, "", ""},
		{"6.6 package.json folder", "6.6.0", map[string]string{"package.json": pkg(`, "prisma": {"schema": "db"}`), "db/a.prisma": ""}, nil, "", "prismaSchemaFolder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range tc.files {
				files[k] = v
			}
			if tc.version != "" {
				files["package-lock.json"] = lock(tc.version)
			}
			p, codes := discoverOne(t, files, ".")
			if !reflect.DeepEqual(p.Files, tc.want) || p.Config != tc.config || !strings.Contains(p.Reason, tc.reason) {
				t.Errorf("got files %v config %q reason %q", p.Files, p.Config, p.Reason)
			}
			for _, c := range codes {
				if strings.HasPrefix(c, codeUnread+" package.json") {
					t.Errorf("Prisma 5 and 6 read package.json \"prisma\": %v", codes)
				}
			}
		})
	}
}
