package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

// TestUnreadToolsTypeORM covers the TypeORM rule (T018g6,
// Revision 3 addendum 2 OB, row TO-U1): a folder a data source's
// migrations option names, counted by its script files at any depth; a
// named folder without stored scripts (build output) adds no entry, and
// the entries keep the list's path order.
func TestUnreadToolsTypeORM(t *testing.T) {
	str := func(s string) *string { return &s }
	n := func(i int) *int { return &i }
	named := map[string]bool{"server/migration/sqlite": true, "server/migration/postgres": true, "dist/migration/sqlite": true}
	for _, tc := range []struct {
		name  string
		files []string
		in    []schemaUnreadTool
		want  []schemaUnreadTool
	}{
		{"scripts at any depth", []string{"server/migration/sqlite/1-a.ts", "server/migration/sqlite/old/2-b.js", "server/migration/postgres/1-a.ts", "server/migration/sqlite/readme.md"}, nil,
			[]schemaUnreadTool{{Tool: "typeorm-migrations", Path: str("server/migration/postgres"), Files: n(1)}, {Tool: "typeorm-migrations", Path: str("server/migration/sqlite"), Files: n(2)}}},
		{"no stored scripts", []string{"server/migration/sqlite/readme.md", "web/migration/1.ts"}, []schemaUnreadTool{}, []schemaUnreadTool{}},
		{"merged in path order", []string{"server/migration/sqlite/1.ts"}, []schemaUnreadTool{{Tool: "alembic", Path: str("api/alembic"), Files: n(3)}, {Tool: "sequelize-migrations", Path: str("web/migrations"), Files: n(1)}},
			[]schemaUnreadTool{{Tool: "alembic", Path: str("api/alembic"), Files: n(3)}, {Tool: "typeorm-migrations", Path: str("server/migration/sqlite"), Files: n(1)}, {Tool: "sequelize-migrations", Path: str("web/migrations"), Files: n(1)}}},
	} {
		got := withTypeORM(tc.in, tc.files, named)
		if got == nil {
			got = []schemaUnreadTool{}
		}
		if !reflect.DeepEqual(got, tc.want) {
			b, _ := json.Marshal(got)
			t.Errorf("%s: %s", tc.name, b)
		}
	}
	if got := withTypeORM(nil, []string{"server/migration/sqlite/1.ts"}, nil); got != nil {
		t.Errorf("no named folder: %v", got)
	}
}

// TestSchemaTypeORMMigrations checks that a TypeORM data source's
// migrations folder reaches unread_tools through the linker's marker,
// on both backends.
func TestSchemaTypeORMMigrations(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := t.TempDir()
		for name, body := range map[string]string{
			"api/package.json":      `{"name": "api", "dependencies": {"typeorm": "0.3.31", "pg": "8.23.0"}, "devDependencies": {"typescript": "5.4.5"}}`,
			"api/package-lock.json": `{"lockfileVersion": 3, "packages": {"node_modules/typeorm": {"version": "0.3.31"}, "node_modules/typescript": {"version": "5.4.5"}}}`,
			"api/tsconfig.json":     `{"compilerOptions": {"experimentalDecorators": true, "emitDecoratorMetadata": true}}`,
			"api/src/data-source.ts": "import { DataSource } from 'typeorm';\nimport { Team } from './entity/Team';\n" +
				"export const dataSource = new DataSource({ type: 'postgres', entities: [Team], migrations: ['src/migration/*.ts', 'dist/migration/*.js'] });\n",
			"api/src/entity/Team.ts":          "import { Column, Entity, PrimaryGeneratedColumn } from 'typeorm';\n@Entity()\nexport class Team {\n  @PrimaryGeneratedColumn() id: number;\n  @Column() name: string;\n}\n",
			"api/src/migration/1700-Team.ts":  "import { MigrationInterface, QueryRunner } from 'typeorm';\nexport class Team1700 implements MigrationInterface {\n  async up(q: QueryRunner) {}\n  async down(q: QueryRunner) {}\n}\n",
			"api/src/migration/1701-Index.ts": "import { MigrationInterface, QueryRunner } from 'typeorm';\nexport class Index1701 implements MigrationInterface {\n  async up(q: QueryRunner) {}\n  async down(q: QueryRunner) {}\n}\n",
		} {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		indexRoot(t, root, w)
		var body struct {
			UnreadTools []schemaUnreadTool `json:"unread_tools"`
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/schema/sets", nil), &body)
		dir, two := "api/src/migration", 2
		want := []schemaUnreadTool{{Tool: "typeorm-migrations", Path: &dir, Files: &two}}
		if !reflect.DeepEqual(body.UnreadTools, want) {
			b, _ := json.Marshal(body.UnreadTools)
			t.Fatalf("unread_tools %s", b)
		}
	})
}
