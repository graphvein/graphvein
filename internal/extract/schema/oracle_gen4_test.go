package schema

// Generator g4 (T018a2 Revision 1.3): g3's grammar plus valid
// and refused variants of the values of data-changing statements and of
// the column references of N1 statements, so that the oracle pins
// N3.P1, N3.P5 and N1.P5 against live verdicts. g1-g3 keep their
// grammars (generatorVersion). Every production's tag starts with the
// contract row it exercises and ends with its class.
//
//   - P5 (N3 values): INSERT with and without a column list, multi-row
//     VALUES, DEFAULT and DEFAULT VALUES, of constants that are valid or
//     invalid input for the column's type (lengths, precision, ranges,
//     dates, booleans, uuids), NULL into nullable and NOT NULL columns,
//     omitted columns with and without defaults, CHECK, UNIQUE, PRIMARY
//     KEY and FOREIGN KEY constraints met or broken, expressions that
//     raise per row (division by zero, casts), aggregates and window
//     functions where Postgres refuses them; UPDATE SET of the same
//     values, column copies and DEFAULT, with WHERE conditions of valid
//     and refused operand types, on tables with and without rows; DELETE
//     of rows a foreign key references (NO ACTION, CASCADE, SET NULL);
//     RETURNING items; top-level SELECT and VALUES of constants and of
//     expressions that raise;
//   - P1/P5 (N1 columns): CREATE TRIGGER ... UPDATE OF held, missing and
//     repeated columns, WHEN conditions, a trigger on a table later
//     written; CREATE POLICY USING / WITH CHECK; GRANT and REVOKE with
//     column lists.

import (
	"fmt"
	"strings"
)

var productions4 = []production{
	{8, genP5Insert, false},
	{6, genP5Update, false},
	{3, genP5Delete, false},
	{3, genP5Query, false},
	{4, genP5N1Columns, false},
}

// valuePairs are column types with values Postgres accepts or refuses as
// the value written to such a column.
var valuePairs = [][2]string{
	{"int", "12"}, {"int", "'12'"}, {"int", "'abc'"}, {"int", "2147483648"}, {"int", "-2147483648"}, {"int", "1.5"},
	{"int", "'  12 '"}, {"int", "'1_000'"}, {"int", "1/0"}, {"int", "NULL"}, {"int", "DEFAULT"}, {"int", "length('abc')"},
	{"smallint", "40000"}, {"smallint", "-5"}, {"bigint", "9223372036854775807"}, {"bigint", "9223372036854775808"},
	{"varchar(3)", "'abcd'"}, {"varchar(3)", "'abc'"}, {"varchar(3)", "'ab '"}, {"char(2)", "'abc'"}, {"char(2)", "'a'"},
	{"numeric(5,2)", "123.45"}, {"numeric(5,2)", "1234.5"}, {"numeric(5,2)", "1.005"}, {"numeric(5,2)", "'12.5'"},
	{"numeric(5,2)", "-999"}, {"numeric", "'abc'"}, {"numeric", "''"}, {"numeric", "'1e5'"}, {"numeric", ".5"},
	{"text", "1"}, {"text", "'x'"}, {"text", "NULL"}, {"text", "'a' || 'b'"}, {"text", "'x'::text"},
	{"boolean", "'yes'"}, {"boolean", "'maybe'"}, {"boolean", "true"}, {"boolean", "1"}, {"boolean", "'t'"},
	{"uuid", "'not-a-uuid'"}, {"uuid", "'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'"}, {"uuid", "gen_random_uuid()"},
	{"date", "'2020-02-30'"}, {"date", "'2020-02-28'"}, {"date", "'0000-01-01'"}, {"date", "current_date"},
	{"timestamp", "'2020-01-01 10:00'"}, {"timestamptz", "'2020-01-01 25:00'"}, {"timestamptz", "'2020-01-01 10:00:00+99'"},
	{"timestamptz", "now()"}, {"jsonb", "'{'"}, {"jsonb", "'{}'"}, {"real", "'1e39'"}, {"float", "'1e400'"},
	{"int[]", "'{1}'"}, {"int", "count(*)"}, {"int", "row_number() over ()"}, {"int", "'12'::int"}, {"int", "-'5'"},
}

// constraintVariants are column constraints of the written column.
var constraintVariants = []string{"", "", " NOT NULL", " NOT NULL DEFAULT %s", " DEFAULT %s", " UNIQUE", " PRIMARY KEY", " CHECK (%c IS NOT NULL)"}

// columnDefault is a default for the column type that Postgres accepts
// when the table is created, or "" when the generator has none.
func columnDefault(typ string) string {
	switch {
	case strings.Contains(typ, "int") && !strings.HasSuffix(typ, "[]"), strings.HasPrefix(typ, "numeric"):
		return "1"
	case strings.Contains(typ, "char") || typ == "text":
		return "'a'"
	case typ == "boolean":
		return "false"
	case typ == "date":
		return "current_date"
	case strings.HasPrefix(typ, "timestamp"):
		return pick2([]string{"now()", "CURRENT_TIMESTAMP"}, len(typ))
	case typ == "uuid":
		return "gen_random_uuid()"
	}
	return ""
}

func pick2(list []string, n int) string { return list[n%len(list)] }

// constrainedColumn writes "c <type><constraint>" and returns the
// constraint's kind for the generator's model.
func (g *gen) constrainedColumn(name, typ string) (string, bool) {
	v := pick(g, constraintVariants)
	if strings.Contains(v, "%s") {
		d := columnDefault(typ)
		if d == "" {
			v = ""
		} else {
			v = fmt.Sprintf(v, d)
		}
	}
	v = strings.ReplaceAll(v, "%c", name)
	return name + " " + typ + v, strings.Contains(v, "NOT NULL") || strings.Contains(v, "PRIMARY KEY")
}

func genP5Insert(g *gen) (string, string, bool) {
	p := pick(g, valuePairs)
	def, notNull := g.constrainedColumn("c", p[0])
	extra := pick(g, []string{"", "", "", "id int", "id int", "id int NOT NULL", "id int NOT NULL DEFAULT 0", "id serial", "id int GENERATED ALWAYS AS IDENTITY", "id int REFERENCES ext_a"})
	t := g.newTable3(&gCol{name: "c", typ: p[0], notNull: notNull})
	cols := def
	if extra != "" {
		cols += ", " + extra
		t.cols = append(t.cols, &gCol{name: "id", typ: "int"})
	}
	sql := fmt.Sprintf("CREATE TABLE %s (%s)", t.name, cols)
	value := p[1]
	var insert string
	switch g.r.IntN(7) {
	case 0:
		insert = fmt.Sprintf("INSERT INTO %s VALUES (%s)", t.name, value)
	case 1:
		insert = fmt.Sprintf("INSERT INTO %s (c) VALUES (%s), (%s)", t.name, value, value)
	case 2:
		insert = fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", t.name)
	case 3:
		insert = fmt.Sprintf("INSERT INTO %s (c) VALUES (%s) RETURNING %s", t.name, value, pick(g, []string{"*", "c", "c AS x", "1", "c + 1", "nosuch"}))
	case 4:
		insert = fmt.Sprintf("INSERT INTO %s (c) VALUES (%s) ON CONFLICT DO NOTHING", t.name, value)
	default:
		insert = fmt.Sprintf("INSERT INTO %s (c) VALUES (%s)", t.name, value)
	}
	if g.chance(0.25) {
		// A second, equal insert meets a UNIQUE or PRIMARY KEY.
		insert += ";\n" + insert
	}
	return sql + ";\n" + insert, "N3.insert_value_p5", true
}

func genP5Update(g *gen) (string, string, bool) {
	p := pick(g, valuePairs)
	def, notNull := g.constrainedColumn("c", p[0])
	other := pick(g, []string{p[0], "int", "text", "varchar(3)"})
	t := g.newTable3(&gCol{name: "c", typ: p[0], notNull: notNull}, &gCol{name: "d", typ: other}, &gCol{name: "id", typ: "int"})
	sql := fmt.Sprintf("CREATE TABLE %s (%s, d %s, id int)", t.name, def, other)
	if g.chance(0.5) {
		// Rows to update (the insert itself may be refused).
		sql += fmt.Sprintf(";\nINSERT INTO %s (id) VALUES (1), (2)", t.name)
	}
	if g.chance(0.2) {
		child := g.newTable3(&gCol{name: "r", typ: p[0]})
		sql += fmt.Sprintf(";\nALTER TABLE %s ADD UNIQUE (c);\nCREATE TABLE %s (r %s REFERENCES %s (c))", t.name, child.name, p[0], t.name)
	}
	value := pick(g, []string{p[1], p[1], "d", "c", "DEFAULT", "NULL", "c + 1", "count(*)", "row_number() over ()", "id"})
	set := fmt.Sprintf("UPDATE %s SET c = %s", t.name, value)
	if g.chance(0.6) {
		set += " WHERE " + pick(g, []string{"id = 1", "id = 'abc'", "id = '1'", "d = 1", "d IS NULL", "count(*) > 0", "id / 0 = 1", "true", "id = 1 AND d IS NOT NULL", "NOT (id <> 2)", "d = 'x'", "id = 1.5", "nosuch = 1", "c = d"})
	}
	if g.chance(0.2) {
		set += " RETURNING " + pick(g, []string{"*", "id", "c AS x", "id * 2", "'k'"})
	}
	return sql + ";\n" + set, "N3.update_value_p5", true
}

func genP5Delete(g *gen) (string, string, bool) {
	parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	parent.pk = []string{"id"}
	child := g.newTable3(&gCol{name: "p", typ: "int"})
	action := pick(g, []string{"", " ON DELETE CASCADE", " ON DELETE SET NULL", " ON DELETE RESTRICT"})
	notNull := ""
	if g.chance(0.3) {
		notNull = " NOT NULL"
	}
	sql := fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\nCREATE TABLE %s (p int%s REFERENCES %s%s)", parent.name, child.name, notNull, parent.name, action)
	if g.chance(0.6) {
		sql += fmt.Sprintf(";\nINSERT INTO %s VALUES (1);\nINSERT INTO %s VALUES (1)", parent.name, child.name)
	}
	target := parent.name
	if g.chance(0.3) {
		target = child.name
	}
	del := "DELETE FROM " + target
	if g.chance(0.5) {
		del += " WHERE " + pick(g, []string{"true", "false", "1 = 1", "id = 'x'", "p IS NULL", "id = 1", "p = 1"})
	}
	return sql + ";\n" + del, "N3.delete_value_p5", true
}

func genP5Query(g *gen) (string, string, bool) {
	return pick(g, []string{
		"SELECT 1", "SELECT 'x' AS y", "SELECT 1/0", "SELECT 'abc'::int", "SELECT now()", "SELECT length('x')",
		"VALUES (1, 'a')", "VALUES (1/0)", "VALUES ('x'::int)", "SELECT NULL", "SELECT true, false", "SELECT -1",
		"SELECT 99999999999999999999", "SELECT 2147483647 + 1", "SELECT count(*)",
	}), "N3.query_value_p5", true
}

func genP5N1Columns(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "text"})
	sql := fmt.Sprintf("CREATE TABLE %s (a int, b text)", t.name)
	switch g.r.IntN(3) {
	case 0:
		fn := g.name("trg")
		g.trigfns = append(g.trigfns, fn)
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$", fn)
		events := pick(g, []string{"INSERT", "UPDATE", "UPDATE OF a", "UPDATE OF a, b", "UPDATE OF nosuch", "UPDATE OF a, a", "INSERT OR UPDATE OF b OR DELETE"})
		when := pick(g, []string{"", "", " WHEN (NEW.a > 0)", " WHEN (NEW.nosuch > 0)", " WHEN (NEW.b = 1)"})
		forEach := " FOR EACH ROW"
		if when == "" && g.chance(0.3) {
			forEach = " FOR EACH STATEMENT"
		}
		sql += fmt.Sprintf(";\nCREATE TRIGGER %s BEFORE %s ON %s%s%s EXECUTE FUNCTION %s()", g.name("tg"), events, t.name, forEach, when, fn)
		if g.chance(0.5) {
			sql += fmt.Sprintf(";\nINSERT INTO %s (a) VALUES (1)", t.name)
		}
	case 1:
		expr := pick(g, []string{"USING (true)", "USING (a > 0)", "USING (nosuch > 0)", "WITH CHECK (false)", "WITH CHECK (b = 1)", "", "USING (a = 1) WITH CHECK (true)"})
		sql += fmt.Sprintf(";\nCREATE POLICY %s ON %s %s", g.name("pol"), t.name, expr)
	default:
		cols := pick(g, []string{"(a)", "(a, b)", "(nosuch)", "(a), UPDATE (b)", "(b, b)"})
		verb := pick(g, []string{"GRANT SELECT %s ON %s TO PUBLIC", "REVOKE SELECT %s ON %s FROM PUBLIC", "GRANT SELECT %s ON TABLE %s TO PUBLIC"})
		sql += ";\n" + fmt.Sprintf(verb, cols, t.name)
	}
	return sql, "N1.columns_p5", true
}
