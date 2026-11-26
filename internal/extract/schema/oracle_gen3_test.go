package schema

// Generator g3 (T018a2 Revision 1): g2's grammar plus valid and
// refused variants of each B1b precondition and the T018b1 syntax items.
// g1 and g2 keep their grammars, so their recorded seeds stay
// reproducible (generatorVersion). Every production's tag starts with the
// contract row whose precondition it exercises and ends with its class.
//
//   - P4: persistence of foreign-key pairs (UNLOGGED tables, SET
//     LOGGED/UNLOGGED), INHERIT / NO INHERIT with matching and missing
//     columns, routine kinds (CALL, SELECT, DROP, ALTER, COMMENT of a
//     function or procedure), identity on integer and other types, DROP of
//     a relation of another kind;
//   - P5: DEFAULT expressions of matching and refused types and invalid
//     constants, ALTER COLUMN TYPE with and without USING, expression
//     indexes and predicates of IMMUTABLE and STABLE functions and casts,
//     generated columns;
//   - P6: BEGIN, START TRANSACTION, COMMIT, END, ROLLBACK, SAVEPOINT,
//     RELEASE and ROLLBACK TO of held and missing savepoints;
//   - P7: unnamed CREATE INDEX IF NOT EXISTS, RENAME and SET SCHEMA in a
//     subcommand list, SELECT INTO followed by DISTINCT, INTO as an alias,
//     INTO in INSERT and in a view's query;
//   - P8: TRUNCATE with and without the referencing table or CASCADE,
//     CLUSTER with and without USING, on a partial index;
//   - P9: two subcommands on one column in and against the pass order.

import (
	"fmt"
	"strings"
)

var productions3 = []production{
	{3, genP4Persistence, false},
	{2, genP4Inherit, false},
	{3, genP4Routines, false},
	{2, genP4Identity, false},
	{2, genP4DropKind, false},
	{5, genP5Default, false},
	{4, genP5AlterType, false},
	{4, genP5Index, false},
	{2, genP5Generated, false},
	{4, genP6Transaction, false},
	{2, genP7Syntax, true},
	{3, genP8Truncate, false},
	{3, genP8Cluster, false},
	{4, genP9Order, false},
}

// newTable3 registers a table the g3 productions create.
func (g *gen) newTable3(cols ...*gCol) *gTable {
	t := &gTable{name: g.name("t"), cols: cols}
	g.tables = append(g.tables, t)
	return t
}

func colDefs(cols []*gCol) string {
	var parts []string
	for _, c := range cols {
		s := c.name + " " + c.typ
		if c.notNull {
			s += " NOT NULL"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func genP4Persistence(g *gen) (string, string, bool) {
	switch g.r.IntN(3) {
	case 0:
		parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
		parent.pk = []string{"id"}
		childUnlogged := g.chance(0.5)
		child := g.newTable3(&gCol{name: "id", typ: "int"})
		kw := ""
		if childUnlogged {
			kw = "UNLOGGED "
		}
		return fmt.Sprintf("CREATE UNLOGGED TABLE %s (id int PRIMARY KEY);\nCREATE %sTABLE %s (id int REFERENCES %s)", parent.name, kw, child.name, parent.name), "M3.unlogged_pair_p4", true
	case 1:
		parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
		parent.pk = []string{"id"}
		child := g.newTable3(&gCol{name: "id", typ: "int"})
		return fmt.Sprintf("CREATE UNLOGGED TABLE %s (id int PRIMARY KEY);\nCREATE UNLOGGED TABLE %s (id int REFERENCES %s)", parent.name, child.name, parent.name), "M3.unlogged_both_p4", true
	default:
		if len(g.tables) == 0 {
			return "", "", false
		}
		t := g.table()
		return fmt.Sprintf("ALTER TABLE %s %s", t.name, pick(g, []string{"SET UNLOGGED", "SET LOGGED"})), "M5k.set_logged_p4", true
	}
}

func genP4Inherit(g *gen) (string, string, bool) {
	parent := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "text"})
	childCols := []*gCol{{name: "a", typ: "int"}, {name: "b", typ: "text"}, {name: "x", typ: "int"}}
	tag := "M5k.inherit_p4"
	switch g.r.IntN(4) {
	case 0:
		childCols = childCols[1:]
		tag = "M5k.inherit_missing_p4"
	case 1:
		childCols[1].typ = "varchar(10)"
		tag = "M5k.inherit_type_p4"
	}
	child := g.newTable3(childCols...)
	child.parent = parent.name
	sql := fmt.Sprintf("CREATE TABLE %s (%s);\nCREATE TABLE %s (%s);\nALTER TABLE %s INHERIT %s", parent.name, colDefs(parent.cols), child.name, colDefs(child.cols), child.name, parent.name)
	if g.chance(0.4) {
		other := parent.name
		if g.chance(0.3) {
			other = child.name
		}
		sql += fmt.Sprintf(";\nALTER TABLE %s NO INHERIT %s", child.name, other)
	}
	return sql, tag, true
}

func genP4Routines(g *gen) (string, string, bool) {
	fn, proc := g.name("f"), g.name("p")
	setup := fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCREATE PROCEDURE %s() LANGUAGE sql AS $$ SELECT 1 $$;\n", fn, proc)
	uses := []struct{ sql, tag string }{
		{"CALL " + proc + "()", "O1.call_procedure_p4"},
		{"CALL " + fn + "()", "O1.call_function_p4"},
		{"SELECT " + fn + "()", "O1.select_function_p4"},
		{"SELECT " + proc + "()", "O1.select_procedure_p4"},
		{"DROP PROCEDURE " + fn + "()", "R5.drop_procedure_p4"},
		{"DROP ROUTINE " + proc + "()", "R5.drop_routine_p4"},
		{"DROP FUNCTION " + fn + "()", "R5.drop_function_kind_p4"},
		{"ALTER PROCEDURE " + proc + "() RENAME TO " + g.name("p"), "R6.rename_procedure_p4"},
		{"ALTER PROCEDURE " + fn + "() RENAME TO " + g.name("p"), "R6.rename_function_as_procedure_p4"},
		{"COMMENT ON FUNCTION " + proc + "() IS 'c'", "N1.comment_procedure_p4"},
		{"COMMENT ON PROCEDURE " + proc + "() IS 'c'", "N1.comment_procedure_ok_p4"},
	}
	u := pick(g, uses)
	return setup + u.sql, u.tag, true
}

func genP4Identity(g *gen) (string, string, bool) {
	typ := pick(g, []string{"int", "bigint", "smallint", "text", "numeric", "integer"})
	t := g.newTable3(&gCol{name: "c", typ: typ, notNull: true})
	return fmt.Sprintf("CREATE TABLE %s (c %s NOT NULL);\nALTER TABLE %s ALTER COLUMN c ADD GENERATED ALWAYS AS IDENTITY", t.name, typ, t.name), "M5k.add_identity_p4", true
}

func genP4DropKind(g *gen) (string, string, bool) {
	v := g.name("v")
	switch g.r.IntN(3) {
	case 0:
		return fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS x;\nDROP VIEW %s", v, v), "R5.drop_view_p4", true
	case 1:
		return fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS x;\nDROP %s %s", v, pick(g, []string{"SEQUENCE", "MATERIALIZED VIEW", "TYPE"}), v), "R5.drop_view_kind_p4", true
	default:
		if len(g.tables) == 0 {
			return "", "", false
		}
		return "DROP VIEW " + g.table().name, "R5.drop_view_table_p4", true
	}
}

// defaultPairs are column types with DEFAULT expressions Postgres accepts
// or refuses when the statement is analysed.
var defaultPairs = [][2]string{
	{"int", "'12'"}, {"int", "'abc'"}, {"int", "now()"}, {"date", "now()"}, {"text", "1"}, {"int", "true"},
	{"varchar(3)", "'abcd'"}, {"varchar(3)", "'abc'"}, {"timestamptz", "current_timestamp"}, {"boolean", "'yes'"},
	{"uuid", "gen_random_uuid()"}, {"uuid", "'not-a-uuid'"}, {"jsonb", "'{}'"}, {"jsonb", "'{'"}, {"int[]", "'{}'"},
	{"timestamptz", "now() + interval '1 day'"}, {"numeric(5,2)", "'123.45'"}, {"numeric(5,2)", "'1234.5'"},
	{"bigint", "1"}, {"smallint", "100000"}, {"bytea", "''"}, {"text", "NULL::int"}, {"int", "NULL::text"},
	{"timestamp", "'2020-01-01 10:00:00+00'"}, {"date", "'2020-02-30'"}, {"text[]", "ARRAY[]::text[]"},
	{"int", "length('x')"}, {"text", "lower('X')"}, {"int", "nosuch_e1_fn()"}, {"interval", "'1 day'"},
}

func genP5Default(g *gen) (string, string, bool) {
	p := pick(g, defaultPairs)
	if len(g.tables) > 0 && g.chance(0.4) {
		t := g.table()
		c := g.col(t)
		d := pick(g, defaultPairs)
		if g.chance(0.5) {
			d[0] = c.typ
		}
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", t.name, c.name, d[1]), "M5e.set_default_p5", true
	}
	t := g.newTable3(&gCol{name: "c", typ: p[0]})
	return fmt.Sprintf("CREATE TABLE %s (c %s DEFAULT %s)", t.name, p[0], p[1]), "M1.default_p5", true
}

// alterPairs are ALTER COLUMN TYPE changes with an optional USING.
var alterPairs = [][3]string{
	{"int", "bigint", ""}, {"bigint", "int", ""}, {"text", "int", ""}, {"text", "int", "c::int"}, {"int", "text", ""},
	{"uuid", "int", "c::int"}, {"timestamp", "timestamptz", ""}, {"varchar(10)", "text", ""}, {"int", "boolean", ""},
	{"int", "boolean", "c::boolean"}, {"numeric", "int", ""}, {"date", "timestamptz", ""}, {"jsonb", "text", ""},
	{"text", "jsonb", "c::jsonb"}, {"text", "varchar(5)", ""}, {"int", "numeric(4,1)", ""}, {"text", "json", ""},
}

func genP5AlterType(g *gen) (string, string, bool) {
	p := pick(g, alterPairs)
	using := ""
	if p[2] != "" {
		using = " USING " + p[2]
	}
	t := g.newTable3(&gCol{name: "c", typ: p[1]}, &gCol{name: "id", typ: "int"})
	extra := ""
	switch g.r.IntN(4) {
	case 0:
		extra = fmt.Sprintf(";\nCREATE INDEX ON %s (c)", t.name)
	case 1:
		extra = fmt.Sprintf(";\nCREATE INDEX ON %s ((c IS NULL))", t.name)
	case 2:
		extra = fmt.Sprintf(";\nALTER TABLE %s ADD CHECK (c IS NOT NULL)", t.name)
	}
	def := ""
	if g.chance(0.3) {
		def = " DEFAULT " + pick(g, []string{"NULL", "'1'", "0"})
	}
	return fmt.Sprintf("CREATE TABLE %s (c %s%s, id int)%s;\nALTER TABLE %s ALTER COLUMN c TYPE %s%s", t.name, p[0], def, extra, t.name, p[1], using), "M5c.type_p5", true
}

func genP5Index(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "a", typ: "text"}, &gCol{name: "b", typ: "int"}, &gCol{name: "d", typ: "date"}, &gCol{name: "ts", typ: "timestamptz"}, &gCol{name: "v", typ: "varchar(20)"})
	expr := pick(g, []string{
		"(lower(a))", "(lower(d::text))", "(upper(b))", "((b + 1))", "((a || 'x'))", "(lower(v))", "((d + 1))",
		"((ts::date))", "((b::text))", "(abs(b))", "(md5(a))", "((a = 'x'))", "(coalesce(a, ''))", "(length(a))",
	})
	where := ""
	if g.chance(0.4) {
		where = " WHERE " + pick(g, []string{"a IS NOT NULL", "d > now()", "b > 0", "ts > '2020-01-01'", "a <> ''", "b IS NULL AND a = 'x'", "d > current_date"})
	}
	unique := ""
	if g.chance(0.3) {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE TABLE %s (a text, b int, d date, ts timestamptz, v varchar(20));\nCREATE %sINDEX ON %s %s%s", t.name, unique, t.name, expr, where), "M4.expression_p5", true
}

func genP5Generated(g *gen) (string, string, bool) {
	expr := pick(g, []string{"a * 2", "now()", "lower(s)", "a + 1", "s || 'x'", "length(s)", "random()", "a::text"})
	typ := pick(g, []string{"int", "text", "bigint", "timestamptz"})
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "s", typ: "text"}, &gCol{name: "g", typ: typ})
	return fmt.Sprintf("CREATE TABLE %s (a int, s text, g %s GENERATED ALWAYS AS (%s) STORED)", t.name, typ, expr), "M1.generated_p5", true
}

func genP6Transaction(g *gen) (string, string, bool) {
	sp := "sp" + fmt.Sprint(g.r.IntN(3))
	return pick(g, []string{"BEGIN", "START TRANSACTION", "COMMIT", "END", "ROLLBACK", "SAVEPOINT " + sp, "RELEASE SAVEPOINT " + sp, "ROLLBACK TO SAVEPOINT " + sp, "ABORT"}), "T1.transaction_p6", true
}

func genP7Syntax(g *gen) (string, string, bool) {
	if len(g.tables) == 0 {
		return "", "", false
	}
	t := g.table()
	c := g.col(t)
	return pick(g, []string{
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s int, RENAME COLUMN %s TO %s", t.name, g.name("x"), c.name, g.name("r")),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s, ADD COLUMN %s int", t.name, g.name("t"), g.name("x")),
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s int, SET SCHEMA public", t.name, g.name("x")),
		fmt.Sprintf("SELECT INTO %s DISTINCT 1", g.name("t")),
		fmt.Sprintf("SELECT * FROM %s AS into", t.name),
		fmt.Sprintf("INSERT INTO %s SELECT * FROM %s INTO %s", t.name, t.name, g.name("t")),
		fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS x INTO %s", g.name("v"), g.name("t")),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS ON %s (%s)", t.name, c.name),
	}), "D.syntax_p7", true
}

func genP8Truncate(g *gen) (string, string, bool) {
	parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	parent.pk = []string{"id"}
	child := g.newTable3(&gCol{name: "p", typ: "int"})
	trunc := pick(g, []string{
		"TRUNCATE " + parent.name, "TRUNCATE " + parent.name + ", " + child.name, "TRUNCATE " + parent.name + " CASCADE",
		"TRUNCATE TABLE ONLY " + child.name, "TRUNCATE " + parent.name + " RESTART IDENTITY RESTRICT",
	})
	return fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\nCREATE TABLE %s (p int REFERENCES %s);\n%s", parent.name, child.name, parent.name, trunc), "N3.truncate_p8", true
}

func genP8Cluster(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "text"})
	ix := g.name("ix_")
	g.indexes[ix] = t.name
	index := pick(g, []string{
		fmt.Sprintf("CREATE INDEX %s ON %s (a)", ix, t.name),
		fmt.Sprintf("CREATE INDEX %s ON %s (a) WHERE a > 0", ix, t.name),
		fmt.Sprintf("CREATE INDEX %s ON %s USING hash (a)", ix, t.name),
	})
	use := pick(g, []string{
		"CLUSTER " + t.name + " USING " + ix,
		"CLUSTER " + t.name + " USING " + ix + ";\nCLUSTER " + t.name,
		"CLUSTER " + t.name,
		"CLUSTER " + ix + " ON " + t.name,
		"ALTER TABLE " + t.name + " SET WITHOUT CLUSTER;\nCLUSTER " + t.name,
	})
	return fmt.Sprintf("CREATE TABLE %s (a int, b text);\n%s;\n%s", t.name, index, use), "N3.cluster_p8", true
}

func genP9Order(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "c", typ: "int"}, &gCol{name: "d", typ: "text"})
	x := g.name("x")
	alter := pick(g, []string{
		fmt.Sprintf("ADD COLUMN %s int, ALTER COLUMN %s SET DEFAULT 1", x, x),
		fmt.Sprintf("ALTER COLUMN %s SET DEFAULT 1, ADD COLUMN %s int", x, x),
		"ALTER COLUMN c TYPE bigint, ALTER COLUMN c SET DEFAULT 0",
		"ALTER COLUMN c DROP DEFAULT, ALTER COLUMN c TYPE text",
		fmt.Sprintf("ADD COLUMN %s int, ALTER COLUMN %s TYPE bigint", x, x),
		"DROP COLUMN c, ADD COLUMN c int",
		"ALTER COLUMN c SET NOT NULL, DROP COLUMN c",
		fmt.Sprintf("ADD PRIMARY KEY (%s), ADD COLUMN %s int", x, x),
		"ALTER COLUMN c TYPE bigint, ALTER COLUMN c TYPE int",
		fmt.Sprintf("ADD COLUMN %s int NOT NULL DEFAULT 0, ADD UNIQUE (%s)", x, x),
		"ALTER COLUMN d SET NOT NULL, ALTER COLUMN d DROP NOT NULL",
	})
	return fmt.Sprintf("CREATE TABLE %s (c int DEFAULT 0, d text);\nALTER TABLE %s %s", t.name, t.name, alter), "M5a.order_p9", true
}
