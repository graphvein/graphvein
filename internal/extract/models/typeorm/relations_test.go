package typeorm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// TestTypeORMFixtureRelations checks one or more fixture cases per row
// (TO-R1 to TO-R4, TO-X1 to TO-X4, the TO-P1 (3) narrowing for
// reused columns): the facts, their claim rows and the refusals. Names are
// the generated names TypeORM's DefaultNamingStrategy returned for the
// same keys (TestGeneratedNames).
func TestTypeORMFixtureRelations(t *testing.T) {
	f := runFixture(t)
	const (
		member = "app/src/entity/Member.ts"
		note   = "app/src/entity/Note.ts"
		book   = "v1/src/entity/Book.ts"
		shelf  = "v1/src/entity/Shelf.ts"
		role   = "nest/src/user/role.entity.ts"
	)
	// js renders a fact as compact JSON for comparison.
	js := func(id, path string) string {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(f.fact(t, id, path))
		return strings.TrimSpace(b.String())
	}
	for _, c := range []struct {
		row, id, path, want string
		claim               string
	}{
		// TO-R1: a default join column, typed as the referenced key per
		// dialect, nullable, NO ACTION, a generated FK_ name.
		{"TO-R1", col(note, "Note", "authorId"), "by_dialect.postgres.type", `"integer"`, "type@postgres"},
		{"TO-R1", col(note, "Note", "authorId"), "nullable", `true`, "nullable@"},
		{"TO-R1", col(note, "Note", "authorId"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["authorId"],"name":"FK_d358080cb403fe88e62cc9cba58","on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"account"}]`, "foreign_keys@"},
		{"TO-R1", col(note, "Note", "parentId"), "by_dialect.sqlite.type", `"varchar"`, "type@sqlite"},
		// TO-R1: a composite join column array in array order (0.3.x),
		// nullable false, onUpdate written upper case.
		{"TO-R1", col(member, "Member", "profile_region"), "foreign_keys", `[{"column":"region","columns":["region","handle"],"local_columns":["profile_region","profile_handle"],"name":"FK_2f7875ba81af0ee06891113796f","on_delete":"NO ACTION","on_update":"CASCADE","schema":null,"status":"resolved","table":"profiles"}]`, ""},
		{"TO-R1", col(member, "Member", "profile_region"), "nullable", `false`, "nullable@"},
		// 1.x: the same array sorted by the target's primary key (A34).
		{"TO-R1", col(book, "Book", "shelf_code"), "foreign_keys", `[{"column":"code","columns":["code","region"],"local_columns":["shelf_code","shelf_region"],"name":"FK_20b2a94d1c9cca2381cf0a0deea","on_delete":"SET NULL","on_update":"NO ACTION","schema":null,"status":"resolved","table":"shelf"}]`, ""},
		// TO-R1: a reused explicit column (A31): its own nullability, the
		// referenced key's type, an index through the relation property.
		{"TO-C4", col(member, "Member", "accountId"), "nullable", `true`, "nullable@"},
		{"TO-R1", col(member, "Member", "accountId"), "by_dialect.sqlite.type", `"integer"`, "type@sqlite"},
		{"TO-X1", col(member, "Member", "accountId"), "index", `true`, "index@"},
		{"TO-R1", col(member, "Member", "accountId"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["accountId"],"name":"FK_c1012c9a3cdedf2b00510cdd845","on_delete":"CASCADE","on_update":"NO ACTION","schema":null,"status":"resolved","table":"account"}]`, ""},
		// TO-R1: createForeignKeyConstraints false: proven no foreign key.
		{"TO-R1", col(member, "Member", "referrerId"), "foreign_keys", `[]`, "foreign_keys@"},
		// TO-R2: a one-to-one owner's join column is unique (REL_), with
		// an explicit foreign key name.
		{"TO-R2", col(member, "Member", "pinned_id"), "unique", `true`, ""},
		{"TO-R2", col(member, "Member", "pinned_id"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["pinned_id"],"name":"fk_member_pinned","on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"notes"}]`, "foreign_keys@"},
		// TO-R4: junction tables, their columns, keys and actions: the
		// inverse side states none, so NO ACTION on 0.3.x (A19) and
		// CASCADE on 1.x (A33).
		{"TO-R4", "table:" + member + "#member_tags_tag", "primary_key", `["memberId","tagId"]`, ""},
		{"TO-R4", col(member, "member_tags_tag", "tagId"), "nullable", `false`, "nullable@"},
		{"TO-R4", col(member, "member_tags_tag", "memberId"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["memberId"],"name":"FK_e3d52001bffb5b2d3d862287c01","on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"member"}]`, "foreign_keys@"},
		{"TO-R4", col(member, "member_tags_tag", "tagId"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["tagId"],"name":"FK_7068f531335f68f6dc38530eda2","on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"tag"}]`, ""},
		{"TO-R4", col(book, "book_readers_reader", "readerId"), "foreign_keys", `[{"column":"id","columns":["id"],"local_columns":["readerId"],"name":"FK_166b8d14942f38dafd4ddb3e069","on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"reader"}]`, ""},
		{"TO-R4", "table:" + member + "#member_tags_tag", "indexes", `[{"columns":["memberId"],"name":"IDX_e3d52001bffb5b2d3d862287c0","partial":false,"unique":false},{"columns":["tagId"],"name":"IDX_7068f531335f68f6dc38530eda","partial":false,"unique":false}]`, ""},
		// Named junction columns referencing a named column (A30), and a
		// self-relation's _1/_2.
		{"TO-R4", col(member, "member_follows", "account_handle"), "by_dialect.postgres.type", `"character varying(12)"`, "type@postgres"},
		{"TO-R4", col(member, "member_friends_member", "memberId_2"), "primary_key", `true`, "primary_key@"},
		{"TO-K6", col("snake/src/entity/SnakeUser.ts", "users_friends_users", "users_id_1"), "name", `"users_id_1"`, ""},
		{"TO-K6", col("snake/src/entity/SnakeUser.ts", "SnakeUser", "invited_by_id"), "type", `"integer"`, ""},
		{"TO-R4", col(role, "sys_user_roles", "sysUserId"), "type", `"varchar(36)"`, "type@"},
		// TO-X1 to TO-X3 on the multi-dialect app group (names generated,
		// no migrations: TO-X4).
		{"TO-X1", "table:" + member + "#Member", "indexes", `[{"columns":["nick","level"],"name":"IDX_eaf0b89928698989633b9f22ba","partial":false,"unique":false},{"columns":["code"],"name":"idx_member_code","partial":false,"unique":true},{"columns":["nick"],"name":"IDX_fa57e1314c142ec28dde53ef08","partial":false,"unique":false},{"columns":["level"],"name":"IDX_d1f999449dd38abc1595277b68","partial":true,"unique":true},{"columns":["accountId"],"name":"IDX_c1012c9a3cdedf2b00510cdd84","partial":false,"unique":false}]`, ""},
		{"TO-X2", "table:" + member + "#Member", "unique_constraints", `[{"columns":["accountId","nick"],"name":"UQ_20c533fb91e6c2288dd40e17636"},{"columns":["badge"],"name":"uq_member_badge"},{"columns":["pinned_id"],"name":"REL_4a89f4308a7fa3f02aab095cda"}]`, ""},
		{"TO-X3", "table:" + member + "#Member", "check_constraints", `[{"expression":"\"level\" >= 0","name":"CHK_539eef8eba1a9f5d15319df7a1"},{"expression":"\"nick\" <> ''","name":"chk_member_nick"}]`, ""},
		{"TO-X1", "table:" + member + "#Member", "constraint_set", `"complete"`, ""},
		{"TO-X2", col(member, "Member", "code"), "unique", `true`, "unique@"},
		{"TO-X2", col(member, "Member", "level"), "unique", `false`, "unique@"},
		{"TO-X1", col(member, "Member", "level"), "index", `false`, "index@"},
		{"TO-X1", col(member, "Member", "nick"), "index", `true`, "index@"},
		// TO-X4 under 1.x: the partial index and the check hash with
		// standard SHA-1 (A35); sqlite's simple-enum CHECK (TO-C5).
		{"TO-X4", "table:" + shelf + "#Shelf", "indexes", `[{"columns":["rank"],"name":"IDX_dccb5567826ac735e6d2b79ffa","partial":true,"unique":false}]`, ""},
		{"TO-X4", "table:" + shelf + "#Shelf", "check_constraints", `[{"expression":"\"size\" > 0","name":"CHK_4f0c31f079cfd3f4276f9a816a"},{"expression":"\"wood\" IN ('oak','pine')","name":null}]`, ""},
		// TO-X4: a data source with migrations claims no generated name.
		{"TO-X4", "table:snake/src/entity/SnakeUser.ts#SnakeUser", "indexes", `[{"columns":["invited_by_id"],"name":null,"partial":false,"unique":false}]`, ""},
		// MySQL (mapped): a unique column's index name is IDX_; a check
		// is never created there, so it is not read.
		{"TO-X2", "table:" + role + "#RoleEntity", "unique_constraints", `[{"columns":["name"],"name":"IDX_223de54d6badbe43a5490450c3"}]`, ""},
		{"TO-X3", "table:" + role + "#RoleEntity", "constraint_set", `"partial"`, ""},
		// Refusals: the Odd entity's forms leave both sets partial; the
		// string target's join column is not listed.
		{"TO-X1", "table:app/src/entity/Odd.ts#Odd", "constraint_set", `"partial"`, ""},
		{"TO-R1", "table:app/src/entity/Odd.ts#Odd", "column_set", `"partial"`, ""},
		// The sqlite-only enum CHECK leaves a multi-dialect set partial.
		{"TO-C5", "table:app/src/entity/Account.ts#Account", "constraint_set", `"partial"`, ""},
	} {
		if got := js(c.id, c.path); got != c.want {
			t.Errorf("%s: %s %s = %s, want %s", c.row, c.id, c.path, got, c.want)
		}
		if c.claim != "" {
			fact, dialect, _ := strings.Cut(c.claim, "@")
			if row, _ := f.claimRow(c.id, fact, dialect); row != c.row {
				t.Errorf("%s: %s claim %s is row %q", c.row, c.id, c.claim, row)
			}
		}
	}
	for _, id := range []string{col("app/src/entity/Tag.ts", "Tag", "noteId"), col("app/src/entity/Odd.ts", "Odd", "ownerId"), "table:app/src/entity/Odd.ts#odd_tags_tag"} {
		if _, ok := f.nodes[id]; ok {
			t.Errorf("%s is claimed", id)
		}
	}
	// TO-P1 (3) is no failure on a reused column: the app group claims
	// on both dialects.
	if got := js("table:"+member+"#Member", "dialects"); got != `["postgres","sqlite"]` {
		t.Errorf("Member dialects %s", got)
	}
	if hasLine(f.diags, "Member.accountId", "TO-P1 (3)") {
		t.Error("TO-P1 (3) on a reused join column")
	}
	if _, v := f.claimRow(col(role, "sys_user_roles", "sysUserId"), "type", ""); v != models.VerificationMapped {
		t.Errorf("a MySQL junction column's type is %q", v)
	}
	for _, parts := range [][]string{
		{"schema_model_unread app/src/entity/Odd.ts", "function form of @Index"},
		{"schema_model_unread app/src/entity/Odd.ts", "synchronize: false"},
		{"schema_model_unread app/src/entity/Odd.ts", "fulltext is not read (TO-X1)"},
		{"schema_model_unread app/src/entity/Odd.ts", "nullFiltered is not read (row D)"},
		{"schema_model_unread app/src/entity/Odd.ts", "deferrable: row D"},
		{"schema_model_unread app/src/entity/Odd.ts", "property-level @Check"},
		{"schema_model_unread app/src/entity/Odd.ts", "junction table of Odd.tags is not read"},
		{"schema_model_unread nest/src/user/role.entity.ts", "check constraints on MySQL"},
	} {
		if !hasLine(f.diags, parts...) {
			t.Errorf("no diagnostic with %q", parts)
		}
	}
}

// TestMySQLForeignKeyIndex checks Revision 4 A3 beyond the fixture's
// MySQL case: on MariaDB, and with MySQL and another dialect in the set,
// a foreign key no declared index leads with leaves the constraint set
// partial (never a complete empty index list); a foreign key covered by a
// declared index adds nothing.
func TestMySQLForeignKeyIndex(t *testing.T) {
	entities := map[string]string{
		"src/entity/User.ts": entityFile("User"),
		"src/entity/Post.ts": `import { Entity, Index, ManyToOne, PrimaryGeneratedColumn } from 'typeorm';
import { User } from './User';
@Entity()
export class Post {
  @PrimaryGeneratedColumn() id: number;
  @ManyToOne(() => User) author: User;
}
`,
		"src/entity/Note.ts": `import { Entity, Index, ManyToOne, PrimaryGeneratedColumn } from 'typeorm';
import { User } from './User';
@Entity()
export class Note {
  @PrimaryGeneratedColumn() id: number;
  @Index() @ManyToOne(() => User) author: User;
}
`,
	}
	for _, c := range []struct {
		name, ds string
		partial  bool
	}{
		{"mariadb", `{ type: 'mariadb', entities: [User, Post, Note] }`, true},
		{"mysql and sqlite", `process.env.X ? { type: 'mysql', entities: [User, Post, Note] } : { type: 'sqlite', database: 'x', entities: [User, Post, Note] }`, true},
		{"mysql", `{ type: 'mysql', entities: [User, Post, Note] }`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := map[string]string{
				"package.json":      `{"dependencies": {"typeorm": "0.3.31", "mysql2": "3.0.0", "sqlite3": "5.1.7"}, "devDependencies": {"typescript": "5.4.5"}}`,
				"package-lock.json": lock("0.3.31", "", ""),
				"tsconfig.json":     tsconfigOn,
				"src/data-source.ts": "import { DataSource } from 'typeorm';\nimport { User } from './entity/User';\nimport { Post } from './entity/Post';\nimport { Note } from './entity/Note';\n" +
					"export const ds = new DataSource(" + c.ds + ");\n",
			}
			for k, v := range entities {
				src[k] = v
			}
			nodes, _, _ := Link(input(t, src))
			tables := map[string]map[string]any{}
			for _, n := range nodes {
				if n.Kind == "table" {
					tables[n.Identity] = n.Attributes
				}
			}
			post, note := tables["table:src/entity/Post.ts#Post"], tables["table:src/entity/Note.ts#Note"]
			if post == nil || note == nil {
				t.Fatalf("tables %v", tables)
			}
			if got := post["constraint_set"] == "partial"; got != c.partial {
				t.Errorf("Post constraint set %v, indexes %v", post["constraint_set"], post["indexes"])
			}
			if !c.partial {
				idx, _ := post["indexes"].([]map[string]any)
				if len(idx) != 1 || !strings.HasPrefix(idx[0]["name"].(string), "FK_") {
					t.Errorf("Post indexes %#v", post["indexes"])
				}
			}
			if note["constraint_set"] != "complete" {
				t.Errorf("Note constraint set %v (a declared index covers its foreign key)", note["constraint_set"])
			}
		})
	}
}
