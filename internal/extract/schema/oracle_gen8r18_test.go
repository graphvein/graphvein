package schema

// Generator g8, the Revision 1.8 productions (T018a2 X1-X4), appended to
// productions8 after the Revision 1.7 ones so the earlier productions keep
// their weights' order: drops of objects with dependents of every kind
// pg_depend records (renamed between creation and drop, the dependent
// dropped first or not, with and without IF EXISTS, RESTRICT and CASCADE,
// then a use of the dependent), collation renames, relations that are not
// tables as CREATE INDEX and REFERENCES targets, and CREATE SCHEMA
// AUTHORIZATION.

import (
	"fmt"
	"strings"
)

func init() {
	productions8 = append(productions8,
		production{6, genX1Dependents, false},
		production{2, genX2Collations, false},
		production{2, genX3Kinds, false},
		production{1, genX4SchemaAuth, false},
	)
}

// x1Dependent is one dependent of a dropped object: the statement that
// creates it, one that drops it first, and one that uses it after.
type x1Dependent struct {
	create, drop, use string
}

// genX1Dependents (Revision 1.8 X1): an object, a dependent, maybe a
// rename, maybe the dependent dropped first, the DROP of the object, and
// maybe a use of the dependent.
func genX1Dependents(g *gen) (string, string, bool) {
	kinds := []string{"function", "trigger function", "type", "composite", "domain", "table", "view", "sequence", "collation"}
	if g.h.fresh {
		kinds = append(kinds, "schema", "event trigger function")
	}
	kind := pick(g, kinds)
	o, o2 := g.name("o"), g.name("o")
	t, d, d2 := g.name("t"), g.name("d"), g.name("d")
	var setup, dropKind, args, renameKind string
	var deps []x1Dependent
	ret := func(typ string) string {
		return fmt.Sprintf("CREATE FUNCTION %s() RETURNS %s LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$", d, typ)
	}
	typeUsers := func(name string) []x1Dependent {
		return []x1Dependent{
			{fmt.Sprintf("CREATE TABLE %s (a %s)", d, name), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TABLE %s (a %s[])", d, name), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TABLE %s (a _%s)", d, name), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TYPE %s AS (a %s, n int)", d, name), "DROP TYPE " + d, "CREATE TABLE " + d2 + " (x " + d + ")"},
			{fmt.Sprintf("CREATE TYPE %s AS (a %s[], n int)", d, name), "DROP TYPE " + d, "CREATE TABLE " + d2 + " (x " + d + ")"},
			{ret(name), "DROP FUNCTION " + d + "()", "SELECT " + d + "()"},
			{fmt.Sprintf("CREATE FUNCTION %s(x %s) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$", d, name), "DROP FUNCTION " + d + "(" + name + ")", "COMMENT ON FUNCTION " + d + " IS 'x'"},
			{fmt.Sprintf("CREATE FUNCTION %s(OUT x %s) LANGUAGE plpgsql AS $$ BEGIN x := NULL; END $$", d, name), "DROP FUNCTION " + d + "()", "SELECT " + d + "()"},
			{fmt.Sprintf("CREATE PROCEDURE %s(x %s) LANGUAGE plpgsql AS $$ BEGIN END $$", d, name), "DROP PROCEDURE " + d + "(" + name + ")", "COMMENT ON PROCEDURE " + d + " IS 'x'"},
			{fmt.Sprintf("CREATE FUNCTION %s() RETURNS %s LANGUAGE sql BEGIN ATOMIC SELECT NULL::%s; END", d, name, name), "DROP FUNCTION " + d + "()", "SELECT " + d + "()"},
			{fmt.Sprintf("CREATE TABLE %s (a int)", d), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
		}
	}
	switch kind {
	case "trigger function":
		setup = fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\n", t, o)
		dropKind, args, renameKind = "FUNCTION", "()", "FUNCTION"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()", d, t, o), "DROP TRIGGER " + d + " ON " + t, "COMMENT ON TRIGGER " + d + " ON " + t + " IS 'x'"},
			{fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE PROCEDURE %s()", d, t, o), "DROP TABLE " + t, "DROP TRIGGER " + d + " ON " + t},
			{fmt.Sprintf("CREATE FUNCTION %s(r regclass) RETURNS void LANGUAGE plpgsql AS $f$ BEGIN EXECUTE format('CREATE TRIGGER %s BEFORE UPDATE ON %%s FOR EACH ROW EXECUTE FUNCTION %s()', r); END $f$;\nSELECT %s('%s')", d2, d, o, d2, t), "DROP TRIGGER " + d + " ON " + t, "COMMENT ON TRIGGER " + d + " ON " + t + " IS 'x'"},
			{fmt.Sprintf("CREATE TABLE %s (a int)", d), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
		}
	case "event trigger function":
		setup = fmt.Sprintf("CREATE FUNCTION %s() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;\n", o)
		dropKind, args, renameKind = "FUNCTION", "()", "FUNCTION"
		deps = []x1Dependent{{fmt.Sprintf("CREATE EVENT TRIGGER %s ON ddl_command_end EXECUTE FUNCTION %s()", d, o), "DROP EVENT TRIGGER " + d, "ALTER EVENT TRIGGER " + d + " DISABLE"}}
	case "function":
		setup = fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE FUNCTION %s() RETURNS int LANGUAGE plpgsql IMMUTABLE AS $$ BEGIN RETURN 1; END $$;\n", t, o)
		dropKind, args, renameKind = "FUNCTION", "()", "FUNCTION"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE TABLE %s (a int DEFAULT %s())", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TABLE %s (a int, b int GENERATED ALWAYS AS (a + %s()) STORED)", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " (a) VALUES (1)"},
			{fmt.Sprintf("CREATE DOMAIN %s AS int DEFAULT %s()", d, o), "DROP DOMAIN " + d, "CREATE TABLE " + d2 + " (x " + d + ")"},
			{fmt.Sprintf("CREATE DOMAIN %s AS int CHECK (VALUE <> %s())", d, o), "DROP DOMAIN " + d, "CREATE TABLE " + d2 + " (x " + d + ")"},
			{fmt.Sprintf("CREATE PROCEDURE %s() BEGIN ATOMIC SELECT %s(); END", d, o), "DROP PROCEDURE " + d + "()", "CALL " + d + "()"},
			{fmt.Sprintf("CREATE VIEW %s AS SELECT %s() AS x", d, o), "DROP VIEW " + d, "SELECT * FROM " + d},
			{fmt.Sprintf("CREATE INDEX %s ON %s ((a + %s()))", d, t, o), "DROP INDEX " + d, "ALTER INDEX " + d + " RENAME TO " + d2},
			{fmt.Sprintf("CREATE POLICY %s ON %s USING (a = %s())", d, t, o), "DROP POLICY " + d + " ON " + t, "ALTER POLICY " + d + " ON " + t + " USING (true)"},
			{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN a SET DEFAULT %s()", t, o), "ALTER TABLE " + t + " ALTER COLUMN a DROP DEFAULT", "INSERT INTO " + t + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN %s(); END $$", d, o), "DROP FUNCTION " + d + "()", "SELECT " + d + "()"},
		}
	case "type":
		setup = fmt.Sprintf("CREATE TYPE %s AS ENUM ('a', 'b');\n", o)
		dropKind, renameKind = "TYPE", "TYPE"
		deps = typeUsers(o)
	case "composite":
		setup = fmt.Sprintf("CREATE TYPE %s AS (x int, y text);\n", o)
		dropKind, renameKind = "TYPE", "TYPE"
		deps = typeUsers(o)
	case "domain":
		setup = fmt.Sprintf("CREATE DOMAIN %s AS int CHECK (VALUE > 0);\n", o)
		dropKind, renameKind = "DOMAIN", "DOMAIN"
		deps = typeUsers(o)
	case "table":
		setup = fmt.Sprintf("CREATE TABLE %s (a int PRIMARY KEY, b int);\n", o)
		dropKind, renameKind = "TABLE", "TABLE"
		deps = append(typeUsers(o),
			x1Dependent{fmt.Sprintf("CREATE VIEW %s AS SELECT a FROM %s", d, o), "DROP VIEW " + d, "SELECT * FROM " + d},
			x1Dependent{fmt.Sprintf("CREATE TABLE %s (x int REFERENCES %s (a))", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			x1Dependent{fmt.Sprintf("CREATE TABLE %s () INHERITS (%s)", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			x1Dependent{fmt.Sprintf("CREATE TABLE %s (r regclass DEFAULT '%s'::regclass)", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			x1Dependent{fmt.Sprintf("CREATE PROCEDURE %s() BEGIN ATOMIC INSERT INTO %s VALUES (1, 1); END", d, o), "DROP PROCEDURE " + d + "()", "CALL " + d + "()"},
			x1Dependent{fmt.Sprintf("CREATE FUNCTION %s() RETURNS SETOF %s LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$", d, o), "DROP FUNCTION " + d + "()", "SELECT * FROM " + d + "()"},
			x1Dependent{fmt.Sprintf("CREATE INDEX %s ON %s (b)", d, o), "DROP INDEX " + d, "ALTER INDEX " + d + " RENAME TO " + d2},
			x1Dependent{fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
		)
	case "view":
		setup = fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE VIEW %s AS SELECT a FROM %s;\n", t, o, t)
		dropKind, renameKind = "VIEW", "VIEW"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE VIEW %s AS SELECT a FROM %s", d, o), "DROP VIEW " + d, "SELECT * FROM " + d},
			{fmt.Sprintf("CREATE TABLE %s (r %s)", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE FUNCTION %s() RETURNS SETOF %s LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$", d, o), "DROP FUNCTION " + d + "()", "SELECT * FROM " + d + "()"},
			{fmt.Sprintf("CREATE VIEW %s AS SELECT a FROM %s", d, t), "DROP VIEW " + d, "SELECT * FROM " + d},
		}
	case "sequence":
		setup = fmt.Sprintf("CREATE SEQUENCE %s;\n", o)
		dropKind, renameKind = "SEQUENCE", "SEQUENCE"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE TABLE %s (a bigint DEFAULT nextval('%s'))", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE VIEW %s AS SELECT nextval('%s') AS n", d, o), "DROP VIEW " + d, "SELECT * FROM " + d},
			{fmt.Sprintf("CREATE TABLE %s (a serial);\nCREATE TABLE %s (LIKE %s INCLUDING DEFAULTS);\nALTER TABLE %s ALTER COLUMN a DROP DEFAULT", t, d, t, t), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TABLE %s (a int)", d), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
		}
		if strings.Contains(deps[2].create, "serial") && g.chance(0.5) {
			o = t + "_a_seq"
		}
	case "collation":
		setup = fmt.Sprintf("CREATE COLLATION %s (provider = icu, locale = 'und');\nCREATE TABLE %s (a text);\n", o, t)
		dropKind, renameKind = "COLLATION", "COLLATION"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE TABLE %s (a text COLLATE %s)", d, o), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE INDEX %s ON %s (a COLLATE %s)", d, t, o), "DROP INDEX " + d, "ALTER INDEX " + d + " RENAME TO " + d2},
			{fmt.Sprintf("CREATE DOMAIN %s AS text COLLATE %s", d, o), "DROP DOMAIN " + d, "CREATE TABLE " + d2 + " (x " + d + ")"},
			{fmt.Sprintf("CREATE VIEW %s AS SELECT a COLLATE %s AS a FROM %s", d, o, t), "DROP VIEW " + d, "SELECT * FROM " + d},
		}
	case "schema":
		setup = fmt.Sprintf("CREATE SCHEMA %s;\n", o)
		dropKind, renameKind = "SCHEMA", "SCHEMA"
		deps = []x1Dependent{
			{fmt.Sprintf("CREATE TABLE %s.%s (a int)", o, d), "DROP TABLE " + o + "." + d, "INSERT INTO " + o + "." + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE FUNCTION %s.%s() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$", o, d), "DROP FUNCTION " + o + "." + d + "()", "SELECT " + o + "." + d + "()"},
			{fmt.Sprintf("SET search_path TO %s;\nCREATE TABLE %s (a int);\nSET search_path TO DEFAULT", o, d), "DROP TABLE " + o + "." + d, "INSERT INTO " + o + "." + d + " DEFAULT VALUES"},
			{fmt.Sprintf("CREATE TABLE %s (a int)", d), "DROP TABLE " + d, "INSERT INTO " + d + " DEFAULT VALUES"},
		}
	}
	dep := pick(g, deps)
	sql := setup + dep.create + ";\n"
	name := o
	if g.chance(0.3) && !strings.HasSuffix(o, "_seq") {
		sql += fmt.Sprintf("ALTER %s %s%s RENAME TO %s;\n", renameKind, o, args, o2)
		name = o2
	}
	if g.chance(0.3) {
		sql += dep.drop + ";\n"
	}
	ifExists := ""
	if g.chance(0.3) {
		ifExists = "IF EXISTS "
	}
	behaviour := pick(g, []string{"", "", " RESTRICT", " CASCADE"})
	sql += fmt.Sprintf("DROP %s %s%s%s%s", dropKind, ifExists, name, args, behaviour)
	if g.chance(0.5) {
		sql += ";\n" + dep.use
	}
	tag := "R5.deps_x1"
	if kind == "table" {
		tag = "M6.deps_x1"
	}
	return sql, tag, true
}

// genX2Collations (Revision 1.8 X2): a collation renamed, then named by
// its old or new name.
func genX2Collations(g *gen) (string, string, bool) {
	co, co2, t := g.name("co"), g.name("co"), g.name("t")
	sql := fmt.Sprintf("CREATE COLLATION %s (provider = icu, locale = 'und');\nCREATE TABLE %s (a text);\n", co, t)
	if g.chance(0.3) {
		sql += fmt.Sprintf("CREATE TABLE %s_u (a text COLLATE %s);\n", t, co)
	}
	if g.chance(0.8) {
		sql += fmt.Sprintf("ALTER COLLATION %s RENAME TO %s;\n", co, co2)
	}
	name := pick(g, []string{co, co2})
	return sql + pick(g, []string{
		"DROP COLLATION " + name,
		"DROP COLLATION IF EXISTS " + name,
		"COMMENT ON COLLATION " + name + " IS 'x'",
		"ALTER COLLATION " + name + " OWNER TO CURRENT_USER",
		"CREATE TABLE " + g.name("c") + " (a text COLLATE " + name + ")",
		"CREATE INDEX " + g.name("ix") + " ON " + t + " (a COLLATE " + name + ")",
		"CREATE COLLATION " + name + " (provider = icu, locale = 'und')",
	}), "R5.collations_x2", true
}

// genX3Kinds (Revision 1.8 X3): views, materialized views, composite types
// and sequences as CREATE INDEX and REFERENCES targets, maybe after CREATE
// TABLE IF NOT EXISTS over them.
func genX3Kinds(g *gen) (string, string, bool) {
	t, v, mv, ct, sq := g.name("t"), g.name("v"), g.name("mv"), g.name("ct"), g.name("sq")
	sql := fmt.Sprintf("CREATE TABLE %s (a int PRIMARY KEY);\nCREATE VIEW %s AS SELECT a FROM %s;\nCREATE MATERIALIZED VIEW %s AS SELECT a FROM %s;\nCREATE TYPE %s AS (a int);\nCREATE SEQUENCE %s;\n", t, v, t, mv, t, ct, sq)
	target := pick(g, []string{t, v, mv, ct, sq})
	if g.chance(0.4) {
		sql += fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (a int, b int);\n", target)
	}
	col := pick(g, []string{"a", "b"})
	if g.chance(0.5) {
		return sql + fmt.Sprintf("CREATE INDEX %s ON %s (%s)", g.name("ix"), target, col), "M4.kinds_x3", true
	}
	return sql + fmt.Sprintf("CREATE TABLE %s (x int REFERENCES %s (a))", g.name("c"), target), "M3.kinds_x3", true
}

// genX4SchemaAuth (Revision 1.8 X4): CREATE SCHEMA AUTHORIZATION of a role
// the history creates, with qualified uses before and after and DROP
// SCHEMA (a fresh database only: the schema outlives the history's own).
func genX4SchemaAuth(g *gen) (string, string, bool) {
	if !g.h.fresh {
		return "", "", false
	}
	r := g.name("r")
	use := fmt.Sprintf("CREATE TABLE %s.%s (a int)", r, g.name("t"))
	sql := "CREATE ROLE " + r + ";\n"
	if g.chance(0.3) {
		sql += use + ";\n"
	}
	sql += "CREATE SCHEMA AUTHORIZATION " + r + ";\n"
	if g.chance(0.3) {
		sql += "DROP SCHEMA " + r + ";\n"
	}
	return sql + use, "R1.schemaauth_x4", true
}
