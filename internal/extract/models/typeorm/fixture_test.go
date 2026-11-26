package typeorm

import (
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

// fixtureDir is the TypeORM models fixture (step TOB3 part 1; README.md
// there lists the files and the rows each one reaches).
const fixtureDir = "../../../../testdata/fixtures/typeorm-models"

// fixtureRun is the claim pass over the fixture.
type fixtureRun struct {
	nodes  map[string]contract.Node
	claims map[string][]modeltest.Claim
	diags  []string
	tags   *modeltest.Tags
}

func runFixture(t *testing.T) fixtureRun {
	t.Helper()
	in := corpusInput(t, fixtureDir)
	tags := modeltest.NewTags()
	in.Tag = tags.Tag
	nodes, _, diags := Link(in)
	out := fixtureRun{nodes: map[string]contract.Node{}, diags: diagLines(diags), tags: tags}
	out.claims = modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	modeltest.CheckDialectFacts(t, nodes)
	for _, n := range nodes {
		out.nodes[n.Identity] = n
	}
	return out
}

// fact reads a node attribute by a dotted path ("by_dialect.postgres.type").
func (f fixtureRun) fact(t *testing.T, id, path string) any {
	t.Helper()
	n, ok := f.nodes[id]
	if !ok {
		t.Errorf("no node %s", id)
		return nil
	}
	var v any = n.Attributes
	for _, p := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

// claimRow returns the row and verification of a column's claim on fact
// (dialect "" for a dialect-independent fact).
func (f fixtureRun) claimRow(id, fact, dialect string) (string, string) {
	for _, c := range f.claims[id] {
		if c.Fact == fact && c.Dialect == dialect {
			return c.Row, c.Verification
		}
	}
	return "", ""
}

func col(file, model, name string) string { return "column:" + file + "#" + model + "." + name }

const (
	account = "app/src/entity/Account.ts"
	typed   = "app/src/entity/Typed.ts"
	tagged  = "app/src/entity/Tagged.ts"
	nestUsr = "nest/src/user/user.entity.ts"
	snakeU  = "snake/src/entity/SnakeUser.ts"
	widget  = "ormcfg/src/entity/Widget.ts"
)

// TestTypeORMFixtureRows checks one or more fixture cases per claim and
// refusal row of step TOB3 (TO-K2, TO-K3, TO-K6, TO-E1-E3, TO-C1-C6,
// TO-P1, TO-P2, D): the fact, its claim row and its verification.
func TestTypeORMFixtureRows(t *testing.T) {
	f := runFixture(t)
	type want struct {
		row, id, path string
		value         any
		// claim is "<fact>@<dialect>" whose claim must name row.
		claim string
	}
	cases := []want{
		// TO-K2: class name in snake case, a literal name, a column name.
		{"TO-K2", "table:" + account + "#Account", "name", "account", ""},
		{"TO-K2", "table:app/src/entity/Profile.ts#Profile", "name", "profiles", ""},
		{"TO-K2", "table:app/src/entity/Note.ts#Note", "name", "notes", ""},
		{"TO-K2", col(account, "Account", "display_name"), "name", "display_name", "name@"},
		{"TO-K2", col(account, "Account", "displayName"), "", nil, ""},
		// TO-K6: SnakeNamingStrategy names.
		{"TO-K6", col(snakeU, "SnakeUser", "first_name"), "name", "first_name", "name@"},
		{"TO-K6", col(snakeU, "SnakeUser", "lastName"), "name", "lastName", "name@"},
		{"TO-K6", col(snakeU, "SnakeUser", "created_at"), "type", "timestamp without time zone", ""},
		{"TO-C5", col(snakeU, "SnakeUser", "admin_level"), "type", "users_admin_level_enum", "type@"},
		// TO-K3: forRootAsync with a try statement and an async block
		// dataSourceFactory reaches the forFeature entity.
		{"TO-K3", "table:" + nestUsr + "#UserEntity", "name", "sys_user", ""},
		// TO-E1: synchronize false is mapped; listeners have no fact.
		// TO-E2: the abstract base's columns are the entity's.
		{"TO-C1", col(account, "Account", "id"), "primary_key", true, "primary_key@"},
		{"TO-E1", "table:app/src/entity/Archive.ts#Archive", "verification", models.VerificationMapped, ""},
		{"TO-K2", col("app/src/entity/Archive.ts", "Archive", "label"), "name", "label", "name@"},
		{"TO-E1", col(account, "Account", "touch"), "", nil, ""},
		// TO-E3: @ApiProperty and @Exclude members are read.
		{"TO-C2", col(nestUsr, "UserEntity", "username"), "type", "varchar(32)", "type@"},
		{"TO-C4", col(nestUsr, "UserEntity", "password"), "nullable", false, "nullable@"},
		// TO-C1: generation per dialect, uuid, identity, composite keys.
		{"TO-C1", col(account, "Account", "id"), "by_dialect.postgres.autoincrement", true, "autoincrement@postgres"},
		{"TO-C1", col(account, "Account", "id"), "by_dialect.sqlite.autoincrement", true, "autoincrement@sqlite"},
		{"TO-C1", col(account, "Account", "id"), "by_dialect.postgres.server_default", true, "server_default@postgres"},
		{"TO-C1", col(account, "Account", "id"), "by_dialect.sqlite.server_default", false, "server_default@sqlite"},
		{"TO-C1", col("app/src/entity/Note.ts", "Note", "id"), "by_dialect.postgres.server_default_text", "uuid_generate_v4()", ""},
		{"TO-C1", col("app/src/entity/Note.ts", "Note", "id"), "by_dialect.sqlite.type", "varchar", "type@sqlite"},
		{"TO-C1", col(typed, "Typed", "id"), "autoincrement", true, "autoincrement@"},
		{"TO-C1", "table:app/src/entity/Profile.ts#Profile", "primary_key", []string{"handle", "region"}, ""},
		{"TO-C1", col(nestUsr, "UserEntity", "id"), "type", "varchar(36)", "type@"},
		// TO-C2: explicit types, modifiers and design types per dialect.
		{"TO-C2", col(account, "Account", "code"), "by_dialect.postgres.type", "character varying(40)", "type@postgres"},
		{"TO-C2", col(account, "Account", "code"), "by_dialect.sqlite.type", "varchar(40)", "type@sqlite"},
		{"TO-C2", col(account, "Account", "code"), "by_dialect.sqlite.length", 40, "length@sqlite"},
		{"TO-C2", col(account, "Account", "balance"), "by_dialect.postgres.type", "numeric(10,2)", "type@postgres"},
		{"TO-C2", col(account, "Account", "balance"), "by_dialect.sqlite.type", "decimal(10,2)", "type@sqlite"},
		{"TO-C2", col(account, "Account", "opensAt"), "by_dialect.postgres.type", "time without time zone", "type@postgres"},
		{"TO-C2", col(account, "Account", "count"), "by_dialect.postgres.type", "integer", "type@postgres"},
		{"TO-C2", col(account, "Account", "tags"), "by_dialect.sqlite.type", "text", "type@sqlite"},
		{"TO-C2", col(typed, "Typed", "labels"), "type", "text[]", "type@"},
		{"TO-C2", col(typed, "Typed", "at"), "type", "timestamp with time zone", "type@"},
		{"TO-C2", col("app/src/entity/Profile.ts", "Profile", "country"), "by_dialect.postgres.type", "character(3)", "type@postgres"},
		{"TO-C2", col(nestUsr, "UserEntity", "password"), "type", "varchar(255)", "type@"},
		{"TO-C2", col(nestUsr, "UserEntity", "is_ext"), "type", "tinyint", "type@"},
		{"TO-C2", col(account, "Account", "bio"), "by_dialect.postgres.type", "text", "type@postgres"},
		// TO-C3: special columns.
		{"TO-C3", col(account, "Account", "createdAt"), "by_dialect.postgres.server_default_text", "now()", ""},
		{"TO-C3", col(account, "Account", "createdAt"), "by_dialect.sqlite.server_default_text", "datetime('now')", ""},
		{"TO-C3", col(account, "Account", "createdAt"), "by_dialect.sqlite.type", "datetime", "type@sqlite"},
		{"TO-C3", col(account, "Account", "deletedAt"), "nullable", true, "nullable@"},
		{"TO-C3", col(account, "Account", "version"), "by_dialect.postgres.type", "integer", "type@postgres"},
		{"TO-C3", col(nestUsr, "UserEntity", "created_at"), "type", "datetime(6)", "type@"},
		{"TO-C3", col(nestUsr, "UserEntity", "created_at"), "server_default_text", "CURRENT_TIMESTAMP(6)", ""},
		{"TO-C3", col("v1/src/entity/Item.ts", "Item", "updatedAt"), "server_default", nil, ""},
		// TO-C4: literal options.
		{"TO-C4", col(account, "Account", "active"), "by_dialect.postgres.server_default_text", "true", ""},
		{"TO-C4", col(account, "Account", "active"), "by_dialect.sqlite.server_default_text", "1", ""},
		{"TO-C4", col(account, "Account", "active"), "default", "server", "default@"},
		{"TO-C4", col(account, "Account", "display_name"), "by_dialect.sqlite.server_default_text", "'anon'", ""},
		{"TO-C4", col(account, "Account", "level"), "by_dialect.postgres.server_default_text", "2", ""},
		{"TO-C4", col(account, "Account", "seenAt"), "by_dialect.postgres.server_default_text", "now()", ""},
		{"TO-C4", col(account, "Account", "seenAt"), "by_dialect.sqlite.server_default_text", "CURRENT_TIMESTAMP", ""},
		{"TO-C4", col(account, "Account", "handle"), "unique", true, "unique@"},
		{"TO-C4", col(account, "Account", "bio"), "nullable", true, "nullable@"},
		{"TO-C4", col(account, "Account", "bio"), "default", "absent", "default@"},
		{"TO-C4", col(account, "Account", "nick"), "default", nil, ""},
		{"TO-C4", col(typed, "Typed", "precise"), "server_default", nil, ""},
		{"TO-C4", col(nestUsr, "UserEntity", "status"), "server_default_text", "'1'", ""},
		{"TO-C4", col(nestUsr, "UserEntity", "is_ext"), "server_default_text", "0", ""},
		// TO-C5: enum columns.
		{"TO-C5", col(account, "Account", "shade"), "by_dialect.postgres.type", "account_shade_enum", "type@postgres"},
		{"TO-C5", col(account, "Account", "shade"), "by_dialect.sqlite.type", "varchar", "type@sqlite"},
		{"TO-C5", col(account, "Account", "shade"), "enum.values", []string{"red", "green"}, ""},
		{"TO-C5", col(typed, "Typed", "level"), "enum.values", []string{"1", "2", "5"}, ""},
		{"TO-C5", col(typed, "Typed", "level"), "server_default_text", "'1'", ""},
		{"TO-C5", col(typed, "Typed", "kind"), "type", "ab_kind", "type@"},
		// TO-C6: the DbAwareColumn wrapper.
		{"TO-C6", col(tagged, "Tagged", "seenAt"), "name", "seenAt", "name@"},
		{"TO-C6", col(tagged, "Tagged", "seenAt"), "nullable", false, "nullable@"},
		{"TO-C6", col(tagged, "Tagged", "seenAt"), "by_dialect.postgres.type", nil, ""},
		{"TO-C4", col(tagged, "Tagged", "seenAt"), "by_dialect.postgres.server_default_text", "now()", ""},
		{"TO-C6", col(tagged, "Tagged", "lastAt"), "nullable", true, "nullable@"},
		// TO-P2: a project call as the type; a wrapper that renames.
		{"TO-P2", col(tagged, "Tagged", "other"), "by_dialect.sqlite.type", nil, ""},
		{"TO-P2", col(tagged, "Tagged", "renamed"), "", nil, ""},
		{"TO-P2", "table:" + tagged + "#Tagged", "column_set", "partial", ""},
		// TO-P1 (2): the postgres candidate fails, sqlite is claimed.
		{"TO-P1", "table:failures/src/entity/PgDatetime.ts#PgDatetime", "dialect", "sqlite", ""},
		{"TO-C2", col("failures/src/entity/PgDatetime.ts", "PgDatetime", "at"), "type", "datetime", "type@"},
		// Revision 4 A1: string defaults on date and time columns are not
		// claimed; A2: number and boolean-string defaults on boolean
		// columns as the database stores them; B1: a common function
		// default the oracle fixture compares.
		{"TO-C4", col(typed, "Typed", "since"), "server_default", nil, ""},
		{"TO-C4", col(typed, "Typed", "frozen"), "server_default", nil, ""},
		{"TO-C4", col(widget, "Widget", "since"), "server_default", nil, ""},
		{"TO-C4", col(typed, "Typed", "flagNum"), "server_default_text", "true", ""},
		{"TO-C4", col(typed, "Typed", "flagStr"), "server_default_text", "true", ""},
		{"TO-C4", col(widget, "Widget", "flagNum"), "server_default_text", "1", ""},
		{"TO-C4", col(widget, "Widget", "flagStr"), "server_default_text", "'false'", ""},
		{"TO-C4", col(typed, "Typed", "token"), "server_default_text", "gen_random_uuid()", "server_default@"},
		{"TO-C4", col(widget, "Widget", "stamped"), "server_default_text", "datetime('now')", "server_default@"},
		{"TO-C4", col(widget, "Widget", "total"), "server_default_text", "(1 + 1)", "server_default@"},
		// Revision 4 A3: MySQL's own index for a foreign key no declared
		// index leads with, named as the foreign key; a declared index
		// covers the other.
		{"TO-R1", "table:" + nestUsr + "#UserEntity", "indexes", []map[string]any{
			{"columns": []string{"backupRoleId"}, "name": "IDX_9cd4550e75e30841c85aeb772a", "partial": false, "unique": false},
			{"columns": []string{"mainRoleId"}, "name": "FK_14b1b037cac0e764b929b249104", "partial": false, "unique": false},
		}, ""},
		{"TO-R1", col(nestUsr, "UserEntity", "mainRoleId"), "index", true, "index@"},
		// Revision 4 A4: two roots in one package each reach only the
		// forFeature calls of the files they import.
		{"TO-K4", "table:multiroot/src/entity/alpha.entity.ts#Alpha", "set", "models/typeorm/multiroot/src/a/a.module.ts", ""},
		{"TO-K4", "table:multiroot/src/entity/beta.entity.ts#Beta", "set", "models/typeorm/multiroot/src/b/b.module.ts", ""},
		{"TO-K4", "table:multiroot/src/entity/gamma.entity.ts#Gamma", "", nil, ""},
		// D: @Tree and an embedded column leave the set partial.
		{"D", "table:app/src/entity/Holder.ts#Holder", "column_set", "partial", ""},
		// Step TOB4 reads the owning relations: the join columns are listed.
		{"TO-R1", "table:app/src/entity/Note.ts#Note", "column_set", "complete", ""},
	}
	for _, c := range cases {
		if c.path == "" {
			if _, ok := f.nodes[c.id]; ok {
				t.Errorf("%s: %s is claimed", c.row, c.id)
			}
			continue
		}
		got := f.fact(t, c.id, c.path)
		if list, ok := got.([]string); ok {
			got = list
		} else if list, ok := got.([]any); ok {
			var s []string
			for _, x := range list {
				s = append(s, x.(string))
			}
			got = s
		}
		if !reflect.DeepEqual(got, c.value) {
			t.Errorf("%s: %s %s = %#v, want %#v", c.row, c.id, c.path, got, c.value)
		}
		if c.claim != "" {
			fact, dialect, _ := strings.Cut(c.claim, "@")
			if row, _ := f.claimRow(c.id, fact, dialect); row != c.row {
				t.Errorf("%s: %s claim %s is row %q", c.row, c.id, c.claim, row)
			}
		}
	}
	// Verification: oracle on postgres and sqlite, mapped on mysql and for
	// a synchronize: false entity.
	for _, v := range []struct{ id, fact, dialect, want string }{
		{col(account, "Account", "code"), "type", "postgres", models.VerificationOracle},
		{col(account, "Account", "code"), "nullable", "", models.VerificationOracle},
		{col(nestUsr, "UserEntity", "username"), "type", "", models.VerificationMapped},
		{col("app/src/entity/Archive.ts", "Archive", "label"), "type", "sqlite", models.VerificationMapped},
		{col("app/src/entity/Archive.ts", "Archive", "label"), "nullable", "", models.VerificationMapped},
		// Revision 4 B1: a default form the oracle fixture compares is the
		// dialect's; another form is mapped, and so is the flat default.
		{col(widget, "Widget", "stamped"), "server_default", "", models.VerificationOracle},
		{col(widget, "Widget", "total"), "server_default", "", models.VerificationMapped},
		{col(widget, "Widget", "total"), "default", "", models.VerificationMapped},
		{col(widget, "Widget", "total"), "nullable", "", models.VerificationOracle},
	} {
		if _, got := f.claimRow(v.id, v.fact, v.dialect); got != v.want {
			t.Errorf("%s %s@%s verification %q, want %q", v.id, v.fact, v.dialect, got, v.want)
		}
	}
	// TO-P1 and D markers.
	for _, d := range []struct {
		row   string
		parts []string
	}{
		{"TO-P1 (1)", []string{"schema_model_import_failure failures/src/f1.ts", "TO-P1 (1)", "no primary column", "on postgres"}},
		{"TO-P1 (2)", []string{"schema_model_import_failure failures/src/f2.ts", "TO-P1 (2)", "not supported by postgres"}},
		{"TO-P1 (3)", []string{"schema_model_import_failure failures/src/f3.ts", "TO-P1 (3)", "design type Object"}},
		{"TO-P1 (4)", []string{"schema_model_import_failure failures/src/f4.ts", "TO-P1 (4)", "contains a quote"}},
		{"TO-P1 (5)", []string{"schema_model_import_failure failures/src/f5.ts", "TO-P1 (5)", "fractional default"}},
		{"TO-P1 (6)", []string{"schema_model_import_failure failures/src/f6.ts", "TO-P1 (6)", "not supported by sqlite"}},
		{"TO-P1 (7)", []string{"schema_model_import_failure v1/src/legacy-data-source.ts", "TO-P1 (7)"}},
		{"TO-P1 (8)", []string{"schema_model_import_failure v1/src/count-data-source.ts", "TO-P1 (8)", "@RelationCount"}},
		{"TO-P1 (9)", []string{"schema_model_import_failure v1/src/index-type-data-source.ts", "TO-P1 (9)", "@Index type option", "on sqlite"}},
		{"TO-P1 (10)", []string{"schema_model_import_failure failures/src/f10.ts", "TO-P1 (10)", `"CURRENT_TIMESTAMP" on a date or time column`, "on postgres"}},
		{"TO-P1 (11)", []string{"schema_model_import_failure failures/src/f11.ts", "TO-P1 (11)", "uuid column", "on postgres"}},
		{"TO-P1 (12)", []string{"schema_model_import_failure failures/src/f12.ts", "TO-P1 (12)", "MySQL rejects", "on mysql"}},
		{"TO-P1 (13) postgres", []string{"schema_model_import_failure failures/src/f13.ts", "TO-P1 (13)", "IntLength.count", "TextLength.body", "on postgres"}},
		{"TO-P1 (13) sqlite", []string{"schema_model_import_failure failures/src/f13s.ts", "TO-P1 (13)", "IntLength.count", "on sqlite"}},
		{"TO-P1 (14)", []string{"schema_model_import_failure failures/src/f14.ts", "TO-P1 (14)", "IntPrecision.count", "TextPrecision.body", "on postgres"}},
		{"TO-K4 unreached", []string{"schema_model_unread multiroot/src/c/c.module.ts", "gamma.module.ts:5", "no root module's static imports reach", "may be incomplete"}},
		{"D @Tree", []string{"schema_model_unread app/src/entity/Holder.ts", "@Tree on entity Holder is not read (row D)"}},
		{"D embedded", []string{"schema_model_unread app/src/entity/Holder.ts", "embedded column Holder.address"}},
		{"D naming", []string{"schema_model_unread snake/src/custom-data-source.ts", "namingStrategy"}},
		{"D major", []string{"schema_model_not_proven legacy/package.json", "0.2.45"}},
	} {
		if !hasLine(f.diags, d.parts...) {
			t.Errorf("%s: no diagnostic with %q", d.row, d.parts)
		}
	}
	for _, id := range []string{"table:failures/src/entity/PgKeyword.ts#PgKeyword", "table:failures/src/entity/PgUuidText.ts#PgUuidText", "table:failures/src/entity/MyDatetime.ts#MyDatetime", "table:failures/src/entity/IntLength.ts#IntLength", "table:failures/src/entity/TextLength.ts#TextLength", "table:failures/src/entity/IntPrecision.ts#IntPrecision", "table:failures/src/entity/TextPrecision.ts#TextPrecision", "table:failures/src/entity/NoKey.ts#NoKey", "table:failures/src/entity/Quoted.ts#Quoted", "table:v1/src/entity/Counted.ts#Counted", "table:v1/src/entity/Indexed.ts#Indexed", "table:legacy/src/Old.ts#Old"} {
		if _, ok := f.nodes[id]; ok {
			t.Errorf("%s is claimed on a failing dialect", id)
		}
	}
}

// TestTypeORMFixtureTags is the tag half of the contract test:
// over the fixture, every emitted tag is a contract row or a listed
// form of its row, the emitted rows equal the rows with a branch (every
// row but TO-V), and every listed form of every row is reached.
func TestTypeORMFixtureTags(t *testing.T) {
	f := runFixture(t)
	rowsByID := map[string]Row{}
	for _, r := range Contract {
		rowsByID[r.ID] = r
	}
	for id := range f.tags.Rows {
		if _, ok := rowsByID[id]; !ok {
			t.Errorf("tag %s has no contract row", id)
		}
	}
	for form := range f.tags.Forms {
		id, name, _ := strings.Cut(form, "/")
		found := false
		for _, x := range rowsByID[id].Forms {
			found = found || x == name
		}
		if !found {
			t.Errorf("form tag %s is not listed in the row forms", form)
		}
	}
	for _, r := range Contract {
		switch {
		case r.Side == SideNone && f.tags.Rows[r.ID]:
			t.Errorf("row %s has no branch but is tagged", r.ID)
		case r.Side != SideNone && !f.tags.Rows[r.ID]:
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
		for _, form := range r.Forms {
			if !f.tags.Forms[r.ID+"/"+form] {
				t.Errorf("row %s form %q is not reached by the fixture", r.ID, form)
			}
		}
	}
}
