package schema

// Generator g5 (T018a2): g4's grammar plus
//
//   - P5 literals: string constants that each type's input function
//     accepts or refuses (white space Postgres does not skip, empty and
//     sign-only numerics, year 0000, zone offsets beyond 15:59, float
//     overflow and underflow, repeated interval fields, json escapes),
//     written as a DEFAULT, a SET DEFAULT, a typed literal and an INSERT
//     value, so that the oracle pins the literal grammars (literals.go);
//   - V5/V6 (empty tables): one or two rows into a table with a primary
//     key, unique keys and indexes, serial and identity columns and
//     foreign keys (NULL, omitted and given values, MATCH FULL and
//     SIMPLE), ON CONFLICT DO NOTHING with plain and deferrable keys,
//     after statements that keep the table empty or may write rows into
//     it (another INSERT, UPDATE, a DO block, a function that calls
//     nextval);
//   - O1t and N1.P2: trigger_updated_at-style functions (regclass or text
//     parameter, %s or %I, other bodies), called on held, quoted, dropped
//     and missing tables, twice on one table, with CREATE TRIGGER, DROP
//     TRIGGER and ALTER TRIGGER of the same names, WHEN conditions on
//     UPDATE and INSERT triggers;
//   - C2: foreign keys and ALTER COLUMN TYPE over pairs of key types in
//     and outside the verified list (integers, floats, text types,
//     numeric, uuid, collations);
//   - H7: SET DEFAULT of a refused literal on tables with and without rows
//     (a refusal at analysis is not E2).
//
// g1-g4 keep their grammars (generatorVersion). Every production's tag
// starts with the contract row it exercises and ends with its class.

import (
	"fmt"
	"strings"
)

var productions5 = []production{
	{8, genP5Literal, false},
	{8, genV5Empty, false},
	{6, genO1tTrigger, false},
	{6, genC2Pairs, false},
}

// literalPairs are column types with string constants that the type's
// input function accepts or refuses.
var literalPairs = [][2]string{
	{"numeric", ""}, {"numeric", "."}, {"numeric", "+"}, {"numeric", "-"}, {"numeric", "1."}, {"numeric", ".5"},
	{"numeric", " 12.5 "}, {"numeric", "1e5"}, {"numeric", "1e"}, {"numeric", "1 "}, {"numeric", "NaN"},
	{"numeric(5,2)", "123.45"}, {"numeric(5,2)", "1234.5"}, {"numeric(5,2)", "1.005"}, {"numeric(4,1)", "-999.9"},
	{"int", " 42 "}, {"int", "42 "}, {"int", " 42"}, {"int", "+7"}, {"int", "1_000"}, {"int", "0x1F"},
	{"int", "\t-3\n"}, {"smallint", "32768"}, {"bigint", "-9223372036854775808"},
	{"real", "1e39"}, {"real", "1e-46"}, {"real", "3.4e38"}, {"real", "1.5"}, {"real", "-inf"},
	{"float", "1e309"}, {"float", "1e-400"}, {"float", "0.0"}, {"float", "Infinity"}, {"float", "1 "},
	{"boolean", "yes"}, {"boolean", " on "}, {"boolean", "on "}, {"boolean", "of"}, {"boolean", "tru"},
	{"uuid", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"}, {"uuid", " a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"},
	{"uuid", "A0EEBC999C0B4EF8BB6D6BB9BD380A11"}, {"uuid", "{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11}"},
	{"date", "0000-01-01"}, {"date", "0001-01-01"}, {"date", "2024-02-29"}, {"date", "2023-02-29"}, {"date", " today "},
	{"date", "2024-01-01 "}, {"date", "epoch"},
	{"timestamptz", "2024-01-01 10:00:00+99"}, {"timestamptz", "2024-01-01 10:00:00+16"},
	{"timestamptz", "2024-01-01 10:00:00+15:59"}, {"timestamptz", "2024-01-01 10:00:00+05:60"},
	{"timestamptz", "2024-01-01T10:00:00.123456-0530"}, {"timestamptz", "2024-01-01 24:00"},
	{"timestamp", "0000-06-01 00:00"}, {"timestamp", "2024-01-01 10:00 "}, {"timestamp", "2024-12-31 23:59:59"},
	{"interval", "1 day"}, {"interval", "1 day 2 days"}, {"interval", "1 week 2 days 3 hours"}, {"interval", "-1.5 years"},
	{"interval", "1 fortnight"}, {"interval", "1"},
	{"jsonb", `"\u0000"`}, {"json", `"\ud800"`}, {"jsonb", `{"a": [1, 2.5e3, null, true]}`}, {"jsonb", "{"},
	{"json", " [] "}, {"jsonb", `{"a":1}` + " "},
	{"varchar(3)", "abcd"}, {"varchar(3)", "abc"}, {"char(2)", "abc"}, {"text", "anything"},
	{"int[]", " {} "}, {"int[]", "{} "}, {"bytea", `\x0a1B`}, {"bytea", `\x0`}, {"bytea", `\q`},
}

// quoteLiteral writes s as a standard SQL string constant.
func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func genP5Literal(g *gen) (string, string, bool) {
	p := pick(g, literalPairs)
	lit := quoteLiteral(p[1])
	t := g.newTable3(&gCol{name: "c", typ: p[0]}, &gCol{name: "id", typ: "int"})
	switch g.r.IntN(5) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (c %s DEFAULT %s, id int)", t.name, p[0], lit), "M1.default_literal_p5", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (c %s DEFAULT %s::%s, id int)", t.name, p[0], lit, p[0]), "M1.default_typed_literal_p5", true
	case 2:
		sql := fmt.Sprintf("CREATE TABLE %s (c %s, id int)", t.name, p[0])
		if g.chance(0.5) {
			// Rows: a refusal at analysis does not depend on them (H7).
			sql += fmt.Sprintf(";\nINSERT INTO %s (id) VALUES (1)", t.name)
		}
		return sql + fmt.Sprintf(";\nALTER TABLE %s ALTER COLUMN c SET DEFAULT %s", t.name, lit), "M5e.set_default_literal_p5", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (c %s, id int);\nINSERT INTO %s (c) VALUES (%s)", t.name, p[0], t.name, lit), "N3.insert_literal_p5", true
	default:
		return fmt.Sprintf("CREATE TABLE %s (c text, id int);\nALTER TABLE %s ALTER COLUMN c SET DEFAULT %s", t.name, t.name, typedLiteral(p[0], lit)), "M5e.typed_literal_p5", true
	}
}

// typedLiteral writes a typed literal: type 'string' where the grammar
// allows it (a simple type name), CAST otherwise; its text value is the
// default of a text column.
func typedLiteral(typ, lit string) string {
	if strings.ContainsAny(typ, "([") {
		return fmt.Sprintf("CAST(%s AS %s)::text", lit, typ)
	}
	return fmt.Sprintf("(%s %s)::text", typ, lit)
}

func genV5Empty(g *gen) (string, string, bool) {
	parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	parent.pk = []string{"id"}
	t := g.newTable3(&gCol{name: "k", typ: "int"}, &gCol{name: "u", typ: "text"}, &gCol{name: "r", typ: "int"}, &gCol{name: "s", typ: "int"})
	key := pick(g, []string{"int PRIMARY KEY", "int UNIQUE", "int UNIQUE DEFERRABLE INITIALLY DEFERRED", "serial PRIMARY KEY", "smallserial", "int GENERATED ALWAYS AS IDENTITY PRIMARY KEY", "bigint GENERATED BY DEFAULT AS IDENTITY UNIQUE", "int"})
	match := pick(g, []string{"", "", " MATCH FULL", " MATCH SIMPLE"})
	sql := fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\nCREATE TABLE %s (k %s, u text%s, r int, s int, FOREIGN KEY (r, s) REFERENCES %s (id, id)%s)",
		parent.name, t.name, key, pick(g, []string{"", " UNIQUE", " NOT NULL"}), parent.name, match)
	// A composite key of the parent for the two-column foreign key.
	sql = strings.Replace(sql, "(id int PRIMARY KEY)", "(id int PRIMARY KEY, UNIQUE (id, id2), id2 int)", 1)
	sql = strings.Replace(sql, "REFERENCES "+parent.name+" (id, id)", "REFERENCES "+parent.name+" (id, id2)", 1)
	if g.chance(0.2) {
		sql += fmt.Sprintf(";\nCREATE UNIQUE INDEX ON %s (u)", t.name)
	}
	switch g.r.IntN(7) {
	case 0:
		sql += fmt.Sprintf(";\nUPDATE %s SET r = 1", t.name)
	case 1:
		sql += fmt.Sprintf(";\nINSERT INTO %s VALUES (1, 2)", parent.name)
	case 2:
		sql += fmt.Sprintf(";\nDO $$ BEGIN INSERT INTO %s (k, u) VALUES (1, 'a'); END $$", t.name)
	case 3:
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN RETURN nextval(pg_get_serial_sequence('%s', 'k')); END $$", g.name("f"), t.name)
	case 4:
		sql += fmt.Sprintf(";\nINSERT INTO %s (k, u) VALUES (1, 'a')", t.name)
	}
	row := func() string {
		k := pick(g, []string{"1", "DEFAULT", "NULL"})
		u := pick(g, []string{"'a'", "'b'", "NULL", "DEFAULT"})
		r := pick(g, []string{"NULL", "NULL", "1", "DEFAULT"})
		sv := pick(g, []string{"NULL", "2", "DEFAULT"})
		return fmt.Sprintf("(%s, %s, %s, %s)", k, u, r, sv)
	}
	var ins string
	switch g.r.IntN(4) {
	case 0:
		ins = fmt.Sprintf("INSERT INTO %s (u) VALUES (%s)", t.name, pick(g, []string{"'a'", "NULL"}))
	case 1:
		ins = fmt.Sprintf("INSERT INTO %s VALUES %s, %s", t.name, row(), row())
	default:
		ins = fmt.Sprintf("INSERT INTO %s VALUES %s", t.name, row())
	}
	if g.chance(0.3) {
		ins += " ON CONFLICT DO NOTHING"
	}
	if g.chance(0.2) {
		ins += ";\n" + ins
	}
	return sql + ";\n" + ins, "N3.insert_empty_v5", true
}

func genO1tTrigger(g *gen) (string, string, bool) {
	fn, tf := g.name("tu"), g.name("su")
	g.trigfns = append(g.trigfns, tf)
	param := pick(g, []string{"tablename regclass", "tablename regclass", "tablename text"})
	spec := pick(g, []string{"%s", "%s", "%I"})
	when := pick(g, []string{" WHEN (OLD is distinct from NEW)", " WHEN (OLD is distinct from NEW)", "", " WHEN (NEW.a > 0)"})
	event := pick(g, []string{"BEFORE UPDATE", "BEFORE UPDATE", "AFTER INSERT OR UPDATE", "BEFORE INSERT"})
	body := fmt.Sprintf("execute format('CREATE TRIGGER %s %s ON %s FOR EACH ROW%s EXECUTE FUNCTION %s();', tablename);", "set_upd", event, spec, when, tf)
	if g.chance(0.15) {
		body = "perform 1; " + body
	}
	sql := fmt.Sprintf("CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE OR REPLACE FUNCTION %s(%s) RETURNS void AS $$\nbegin\n  %s\nend;\n$$ LANGUAGE plpgsql", tf, fn, param, body)
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	name := t.name
	if g.chance(0.2) {
		name = strings.ToUpper(name)
	}
	sql += fmt.Sprintf(";\nCREATE TABLE %s (a int)", t.name)
	switch g.r.IntN(6) {
	case 0:
		sql += fmt.Sprintf(";\nCREATE TRIGGER set_upd BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s()", t.name, tf)
	case 1:
		sql += fmt.Sprintf(";\nCREATE TRIGGER set_upd BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s();\nDROP TRIGGER set_upd ON %s", t.name, tf, t.name)
	case 2:
		sql += fmt.Sprintf(";\nCREATE TRIGGER set_upd BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s();\nALTER TRIGGER set_upd ON %s RENAME TO other_upd", t.name, tf, t.name)
	case 3:
		gone := g.name("gone")
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a int);\nDROP TABLE %s", gone, gone)
		name = gone
	}
	arg := name
	if g.chance(0.2) {
		arg = `"` + t.name + `"`
	}
	sql += fmt.Sprintf(";\nSELECT %s('%s')", fn, arg)
	if g.chance(0.3) {
		sql += fmt.Sprintf(";\nSELECT %s('%s')", fn, arg)
	}
	return sql, "O1t.trigger_call", true
}

var c2Types = []string{"smallint", "int", "bigint", "numeric", "numeric(10,2)", "real", "double precision", "text", "varchar", "varchar(36)", "char(36)", "uuid", "text COLLATE \"C\"", "date", "timestamptz"}

func genC2Pairs(g *gen) (string, string, bool) {
	parent := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	child := g.newTable3(&gCol{name: "pid", typ: "int"})
	pt, ct := pick(g, c2Types), pick(g, c2Types)
	sql := fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY);\nCREATE TABLE %s (pid %s REFERENCES %s)", parent.name, pt, child.name, ct, parent.name)
	if g.chance(0.4) {
		sql = fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY);\nCREATE TABLE %s (pid %s);\nALTER TABLE %s ADD FOREIGN KEY (pid) REFERENCES %s", parent.name, pt, child.name, ct, child.name, parent.name)
	}
	if g.chance(0.4) {
		to := strings.Fields(pick(g, c2Types))[0]
		if g.chance(0.5) {
			sql += fmt.Sprintf(";\nALTER TABLE %s ALTER COLUMN pid TYPE %s", child.name, to)
		} else {
			sql += fmt.Sprintf(";\nALTER TABLE %s ALTER COLUMN id TYPE %s", parent.name, to)
		}
	}
	return sql, "M3.c2_pair", true
}
