package typeorm

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// TestSnakeCase checks the port of TypeORM's snakeCase
// (util/StringUtils.js:29-36), the table and SnakeNamingStrategy names.
func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"Account": "account", "UserPushSubscription": "user_push_subscription", "HTTPServer": "http_server",
		"userID": "user_id", "OAuth2Token": "o_auth2_token", "media4k": "media4k", "already_snake": "already_snake",
	} {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEnumMembers checks the runtime values TypeORM reads from an enum
// (metadata/ColumnMetadata.js:184): auto-increment from the previous
// number, strings as written, and an unread member.
func TestEnumMembers(t *testing.T) {
	num := func(v string) *typescript.ModelExpr {
		return &typescript.ModelExpr{Kind: typescript.ModelNumber, Value: v}
	}
	str := func(v string) *typescript.ModelExpr {
		return &typescript.ModelExpr{Kind: typescript.ModelString, Value: v}
	}
	cases := []struct {
		members []typescript.ModelEnumMember
		want    string
	}{
		{[]typescript.ModelEnumMember{{Name: "A"}, {Name: "B"}, {Name: "C", Value: num("5")}, {Name: "D"}}, "number:0 number:1 number:5 number:6"},
		{[]typescript.ModelEnumMember{{Name: "A", Value: str("a")}, {Name: "B", Value: str("b")}}, "string:a string:b"},
		{[]typescript.ModelEnumMember{{Name: "A", Value: str("a")}, {Name: "B"}}, "unread"},
		{[]typescript.ModelEnumMember{{Name: "A", Value: &typescript.ModelExpr{Kind: typescript.ModelIdentifier, Name: "x"}}}, "unread"},
	}
	for _, c := range cases {
		values, ok := enumMembers(&typescript.ModelStatement{Kind: typescript.StatementEnum, Name: "E", EnumMembers: c.members})
		got := "unread"
		if ok {
			var parts []string
			for _, v := range values {
				parts = append(parts, v.kind+":"+v.text)
			}
			got = strings.Join(parts, " ")
		}
		if got != c.want {
			t.Errorf("%v: %s, want %s", c.members, got, c.want)
		}
	}
}

// TestClaimIsolation checks that a failure in one entity's step becomes a
// diagnostic and the other entities are still claimed.
func TestClaimIsolation(t *testing.T) {
	in := corpusInput(t, fixtureDir)
	faultHook = func(step, subject string) {
		if step == "entity" && strings.HasSuffix(subject, "#Profile") {
			panic("injected")
		}
	}
	defer func() { faultHook = nil }()
	nodes, _, diags := Link(in)
	found := map[string]bool{}
	for _, n := range nodes {
		found[n.Identity] = true
	}
	if found["table:app/src/entity/Profile.ts#Profile"] || !found["table:app/src/entity/Account.ts#Account"] {
		t.Errorf("isolation: Profile %v, Account %v", found["table:app/src/entity/Profile.ts#Profile"], found["table:app/src/entity/Account.ts#Account"])
	}
	if !hasDiag(diags, "schema_model_internal_error", "app/src/entity/Profile.ts", "injected") {
		t.Errorf("diagnostics %v", diagLines(diags))
	}
}

// TestDiscoveryIsolation checks that a failure (a panic) while
// discovering one package becomes a schema_model_internal_error
// diagnostic, drops that package's groups and leaves the other packages'
// tables claimed (Revision 4 C1).
func TestDiscoveryIsolation(t *testing.T) {
	in := corpusInput(t, fixtureDir)
	faultHook = func(step, subject string) {
		if step == "discovery" && subject == "TypeORM package nest/package.json" {
			panic("injected")
		}
	}
	defer func() { faultHook = nil }()
	nodes, _, diags := Link(in)
	found := map[string]bool{}
	for _, n := range nodes {
		found[n.Identity] = true
	}
	if found["table:nest/src/user/user.entity.ts#UserEntity"] || !found["table:app/src/entity/Account.ts#Account"] {
		t.Errorf("isolation: UserEntity %v, Account %v", found["table:nest/src/user/user.entity.ts#UserEntity"], found["table:app/src/entity/Account.ts#Account"])
	}
	if !hasDiag(diags, "schema_model_internal_error", "nest/package.json", "discovery step", "injected") {
		t.Errorf("diagnostics %v", diagLines(diags))
	}
}

// TestTestScopeGroups checks that a data source or an ormconfig in a test
// file is read like any other group and that its tables carry `scope:
// test` (Revision 3 addendum 3), while a non-test data source's tables
// carry none.
func TestTestScopeGroups(t *testing.T) {
	entity := `import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class %s {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  label: string;
}
`
	manifest := `{"name": "%s", "dependencies": {"typeorm": "0.3.31", "pg": "8.23.0"}}`
	tsconfig := `{"compilerOptions": {"experimentalDecorators": true, "emitDecoratorMetadata": true}}`
	in := input(t, map[string]string{
		"a/package.json":              fmt.Sprintf(manifest, "a"),
		"a/tsconfig.json":             tsconfig,
		"a/src/entity/Thing.ts":       fmt.Sprintf(entity, "Thing"),
		"a/src/data-source.ts":        "import { DataSource } from \"typeorm\";\nimport { Thing } from \"./entity/Thing\";\n\nexport const ds = new DataSource({ type: \"postgres\", entities: [Thing] });\n",
		"a/test/entity/Fake.ts":       fmt.Sprintf(entity, "Fake"),
		"a/test/data-source.ts":       "import { DataSource } from \"typeorm\";\nimport { Fake } from \"./entity/Fake\";\n\nexport const ds = new DataSource({ type: \"postgres\", entities: [Fake] });\n",
		"test/b/package.json":         fmt.Sprintf(manifest, "b"),
		"test/b/tsconfig.json":        tsconfig,
		"test/b/ormconfig.json":       `{"type": "postgres", "entities": ["src/entity/*.ts"]}`,
		"test/b/src/entity/Widget.ts": fmt.Sprintf(entity, "Widget"),
	})
	nodes, _, diags := Link(in)
	scope := map[string]any{}
	for _, n := range nodes {
		if n.Kind == "table" {
			scope[n.Identity] = n.Attributes["scope"]
		}
	}
	want := map[string]any{
		"table:a/src/entity/Thing.ts#Thing":        nil,
		"table:a/test/entity/Fake.ts#Fake":         "test",
		"table:test/b/src/entity/Widget.ts#Widget": "test",
	}
	if !reflect.DeepEqual(scope, want) {
		t.Errorf("table scopes %v, want %v\n%s", scope, want, strings.Join(diagLines(diags), "\n"))
	}
}
