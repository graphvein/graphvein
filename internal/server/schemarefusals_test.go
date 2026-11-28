package server

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestRefusalForms checks every refusal form against a message the
// linker's format string produces, and that per-class, per-site and
// per-key diagnostics of the same codes are not taken for a whole-source
// refusal (T070).
func TestRefusalForms(t *testing.T) {
	samples := []struct {
		id, file, code, message string
	}{
		{"gorm-module", "go.mod", codeModelNotProven, "go.mod: GORM module example.com/app is not read: no gorm.Open site is read, so the dialect and the configuration are unknown"},
		{"prisma-package", "package.json", codeModelNotProven, "package.json: no Prisma schema is proven for this package: prisma.config.ts: schema is not a string literal"},
		{"prisma-outside", "db/a.prisma", codeModelUnread, "2 .prisma file(s) are outside every discovered Prisma schema and not read: db/a.prisma, db/b.prisma"},
		{"prisma-mongodb", "prisma/schema.prisma", codeModelUnread, "prisma/schema.prisma:3: provider mongodb: collections are out of scope, no table is claimed"},
		{"drizzle-version", "package.json", codeModelNotProven, "package.json: drizzle-orm 1.0.0: the claim contract is verified for drizzle-orm 0.x only; no claim is made until a corpus pins 1.0"},
		{"drizzle-config", "drizzle.config.ts", codeModelNotProven, "no Drizzle schema is proven for drizzle.config.ts: schema is not a literal"},
		{"drizzle-no-config", "package.json", codeModelNotProven, "package.json: no drizzle-kit config is found for this package (no default config name, no --config in its scripts); 3 table builder calls in 2 files not read"},
		{"drizzle-script", "package.json", codeModelNotProven, "package.json: script \"db\" passes drizzle-kit options without a literal --schema and --dialect; its configuration is not read"},
		{"drizzle-script", "package.json", codeModelNotProven, "package.json: two scripts run drizzle-kit with different configs; its drizzle-kit configuration is not read"},
		{"drizzle-script-config", "package.json", codeModelNotProven, "package.json: script \"db\" names config \"cfg/x.ts\", which does not exist"},
		{"drizzle-script-config", "package.json", codeModelNotProven, "package.json: script \"db\" names config \"../x.ts\", which cannot be placed in the repository"},
		{"drizzle-dirs", "drizzle.config.ts", codeModelNotProven, "drizzle.config.ts: the config is run from the package directories a and b, which resolve its schema to different files (DR-V5: drizzle-kit reads the schema against its working directory); no table is claimed"},
		{"drizzle-unused-config", "x/drizzle.config.ts", codeModelUnread, "x/drizzle.config.ts: not drizzle-kit's default config of a Drizzle package and not named by a literal --config in its scripts; not read"},
		{"sequelize-version", "package.json", codeModelNotProven, "package.json: sequelize 7.0.0: the claim contract is verified for sequelize 6 only; no claim is made until a corpus pins this major"},
		{"sequelize-no-instance", "package.json", codeModelNotProven, "package.json: no module-level `new Sequelize(...)` instance is found in this package; its models are not read"},
		{"typeorm-version", "package.json", codeModelNotProven, "package.json: typeorm 0.2.45: the claim contract is verified for typeorm 0.3.x and 1.x only; no claim is made for another major (row D)"},
		{"typeorm-no-source", "package.json", codeModelNotProven, "package.json: no TypeORM data source, ormconfig or TypeOrmModule root is found in this package; its entities are not read"},
		{"typeorm-options", "src/db.ts", codeModelNotProven, "src/db.ts:4: the options of TypeORM DataSource AppDataSource are not read (a spread); its entities are not claimed"},
		{"typeorm-source", "src/db.ts", codeModelUnread, "src/db.ts:4: TypeORM DataSource AppDataSource is not read: the dialect cockroachdb is not read (row D)"},
		{"diesel-config", "diesel.toml", codeModelNotProven, "diesel.toml: [print_schema] file \"src/schema.rs\" does not exist"},
		{"diesel-unnamed", "src/schema.rs", codeModelUnread, "src/schema.rs: a schema.rs with table! that no diesel.toml names; it is not read: the package does not depend on diesel 2"},
	}
	byID := map[string]refusalForm{}
	for _, r := range refusalForms {
		if _, dup := byID[r.id]; dup {
			t.Fatalf("refusal form %s listed twice", r.id)
		}
		byID[r.id] = r
	}
	covered := map[string]bool{}
	for _, s := range samples {
		r, ok := byID[s.id]
		if !ok {
			t.Fatalf("sample names unknown form %s", s.id)
		}
		covered[s.id] = true
		d := contract.Diagnostic{File: s.file, Code: s.code, Message: s.message}
		if got := refusalTool(d); got != r.tool {
			t.Errorf("%s: tool %q, want %q (%s)", s.id, got, r.tool, s.message)
		}
		for _, other := range refusalForms {
			if other.id != s.id && other.code == s.code && other.match(s.file, s.message) {
				t.Errorf("%s: also matches form %s", s.id, other.id)
			}
		}
	}
	for id := range byID {
		if !covered[id] {
			t.Errorf("form %s has no sample", id)
		}
	}
	// Per-class, per-site and per-key diagnostics of the same codes.
	for _, d := range []contract.Diagnostic{
		{File: "app/models.py", Code: codeModelNotProven, Message: "app/models.py:4: model class User: a base is not proven non-table (R-B2); no table claimed"},
		{File: "app/models.py", Code: codeModelUnread, Message: "app/models.py: 3 model classes present, not read (see the per-class diagnostics)"},
		{File: "m/user.go", Code: codeModelNotProven, Message: "m/user.go:9: struct User has gorm tags or embeds gorm.Model, but no read migrate call lists it (model, not proven migrated)"},
		{File: "m/old.go", Code: codeModelUnread, Message: "m/old.go:3: github.com/jinzhu/gorm import (GORM v1): found, not read"},
		{File: "package.json", Code: codeModelUnread, Message: "package.json: the \"prisma\" key of package.json is not read by Prisma 7 and is ignored"},
		{File: "prisma/schema.prisma", Code: codeModelUnread, Message: "prisma/schema.prisma:12: view Stats is not read (views are out of scope)"},
		{File: "src/user.ts", Code: codeModelUnread, Message: "src/user.ts:3: @Entity class User is not read (row D)"},
		{File: "src/db.ts", Code: codeModelNotProven, Message: "src/db.ts:2: dialect of Sequelize instance db: not a literal"},
		{File: "diesel.toml", Code: codeModelUnread, Message: "diesel.toml: migrations_directory.dir cannot be placed in the repository; it is not read"},
		{File: "go.mod", Code: "schema_model_migrations", Message: "go.mod: GORM module example.com/app is not read: x"},
	} {
		if got := refusalTool(d); got != "" {
			t.Errorf("%q taken for a %s refusal", d.Message, got)
		}
	}
}

// TestRefusedSources checks the list: stored files only, one entry per
// distinct diagnostic, ordered by file, tool, code and message.
func TestRefusedSources(t *testing.T) {
	hashes := map[string]string{"go.mod": "h", "web/package.json": "h", "web/prisma/schema.prisma": "h"}
	gorm := contract.Diagnostic{File: "go.mod", Code: codeModelNotProven, Message: "go.mod: GORM module example.com/app is not read: why"}
	got := refusedSources([]contract.Diagnostic{
		{File: "web/prisma/schema.prisma", Code: codeModelUnread, Message: "1 .prisma file(s) are outside every discovered Prisma schema and not read: web/prisma/schema.prisma"},
		{File: "web/package.json", Code: codeModelNotProven, Message: "web/package.json: no Prisma schema is proven for this package: why"},
		gorm, gorm,
		{File: "gone/go.mod", Code: codeModelNotProven, Message: "gone/go.mod: GORM module x is not read: why"},
		{File: "go.mod", Code: codeModelNotProven, Message: "go.mod:3: struct X has gorm tags or embeds gorm.Model, but no read migrate call lists it (model, not proven migrated)"},
	}, hashes)
	want := []schemaRefusedSource{
		{Tool: "gorm", File: "go.mod", Code: codeModelNotProven, Message: gorm.Message},
		{Tool: "prisma", File: "web/package.json", Code: codeModelNotProven, Message: "web/package.json: no Prisma schema is proven for this package: why"},
		{Tool: "prisma", File: "web/prisma/schema.prisma", Code: codeModelUnread, Message: "1 .prisma file(s) are outside every discovered Prisma schema and not read: web/prisma/schema.prisma"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if got := refusedSources(nil, hashes); got == nil || len(got) != 0 {
		t.Fatalf("no diagnostics: got %#v, want []", got)
	}
}
