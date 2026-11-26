package schema

// Generator g8 (T018a2 Revision 1.6): g7's grammar plus productions8,
// valid and refused variants of the Revision 1.6 class rules. g8 is a new
// generator rather than more g7 productions, so that g1-g7 stay
// reproducible at their recorded numbers. Its stream constant is
// 0x7018a28; half of the g8 histories use only productions8 in 1-3 short
// migrations. Every production's tag starts with the contract row it
// exercises and ends with its rule. Step D1 adds V2-V4, V7 and V9-V13;
// Step D2 appends V1, V5, V6 and V8 to productions8.
//
//   - V7: each whole-statement reader's statement as written, with one
//     extra word, with its last word missing and with a misspelled word;
//   - V3: reserved, type_func_name, col_name and unreserved keywords as
//     bare, quoted and qualified columns in index elements, expressions,
//     predicates, operator-class positions, partition keys, EXCLUDE
//     elements, CHECK and generation expressions, and the SQL value
//     functions there and in a DEFAULT;
//   - V4: ALTER INDEX / VIEW / MATERIALIZED VIEW / SEQUENCE, COMMENT ON,
//     GRANT ON SEQUENCE, REFRESH, REINDEX INDEX, CLUSTER ... USING, DROP of
//     the other kinds and DROP TRIGGER ... ON, of names held, held as
//     another kind, not held, and with IF EXISTS;
//   - V2: the languages sql, plpgsql, 'plpgsql', 'PLPGSQL', plperl,
//     plpython3u, internal and c in CREATE FUNCTION and DO;
//   - V9: written operator classes over matching, binary-coercible,
//     polymorphic and mismatching column types;
//   - V10: SET NAMES and SET client_encoding with UTF8 spellings, DEFAULT
//     and unknown names (other encodings are left out: the harness's
//     driver needs a UTF8 client encoding);
//   - V11: system column names in LIKE and INHERITS column lists, typed
//     tables, ADD ATTRIBUTE, CREATE TABLE AS, SELECT INTO, materialized
//     views and views;
//   - V12: view and materialized view options inside and outside their
//     kinds and bounds, check_option on updatable and other views;
//   - V13: serial and pseudo-types in every type position.

import (
	"fmt"
	"strings"
)

// productions8 is appendable: step D2 adds its rules' productions.
var productions8 = []production{
	{10, genV7Whole, false},
	{6, genV3Keywords, false},
	{6, genV4Kinds, false},
	{3, genV2Languages, false},
	{4, genV9Opclasses, false},
	{2, genV10Names, false},
	{4, genV11SystemColumns, false},
	{4, genV12ViewOptions, false},
	{4, genV13Types, false},
}

// v7Variant writes a statement as written, with one extra word, with its
// last word missing or with one word misspelled.
func v7Variant(g *gen, stmt string, misspell [2]string) string {
	switch g.r.IntN(5) {
	case 0:
		return stmt + " garbage"
	case 1:
		if i := strings.LastIndex(stmt, " "); i > 0 {
			return stmt[:i]
		}
	case 2:
		if misspell[0] != "" {
			return strings.Replace(stmt, misspell[0], misspell[1], 1)
		}
	}
	return stmt
}

func genV7Whole(g *gen) (string, string, bool) {
	t, ix := g.name("t"), g.name("ix")
	setup := fmt.Sprintf("CREATE TABLE %s (a int, b text);\nCREATE INDEX %s ON %s (a);\n", t, ix, t)
	type form struct {
		row, stmt string
		misspell  [2]string
	}
	e, v, f, p := g.name("e"), g.name("v"), g.name("f"), g.name("p")
	forms := []form{
		{"N1", fmt.Sprintf("COMMENT ON TABLE %s IS 'x'", t), [2]string{"IS", "ISS"}},
		{"N1", fmt.Sprintf("COMMENT ON COLUMN %s.a IS NULL", t), [2]string{"COLUMN", "COLUMNS"}},
		{"N1", fmt.Sprintf("GRANT SELECT ON %s TO PUBLIC", t), [2]string{"SELECT", "SELEC"}},
		{"N1", fmt.Sprintf("GRANT SELECT, UPDATE (b) ON TABLE %s TO CURRENT_USER WITH GRANT OPTION", t), [2]string{"OPTION", "OPTIONS"}},
		{"N1", fmt.Sprintf("REVOKE ALL ON %s FROM PUBLIC", t), [2]string{"ALL", "ALLL"}},
		{"N1", fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO CURRENT_USER"), [2]string{"SEQUENCES", "SEQUENCE"}},
		{"N1", fmt.Sprintf("CREATE TRIGGER %s BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger()", g.name("tr"), t), [2]string{"EACH", "EVERY"}},
		{"N1", fmt.Sprintf("CREATE POLICY %s ON %s FOR SELECT TO PUBLIC USING (true)", g.name("po"), t), [2]string{"USING", "USE"}},
		{"N1", fmt.Sprintf("CREATE COLLATION %s (provider = icu, locale = 'und')", g.name("co")), [2]string{"provider", "provder"}},
		{"N1", "NOTIFY gv_ch, 'x'", [2]string{"NOTIFY", "NOTIF"}},
		{"T1", fmt.Sprintf("LOCK TABLE %s IN SHARE ROW EXCLUSIVE MODE", t), [2]string{"SHARE", "SHAR"}},
		{"N2", "RESET work_mem", [2]string{"RESET", "RESETT"}},
		{"R2", fmt.Sprintf("CREATE TYPE %s AS ENUM ('a', 'b')", e), [2]string{"ENUM", "ENUMS"}},
		{"R3", fmt.Sprintf("CREATE TYPE %s AS ENUM ('a');\nALTER TYPE %s ADD VALUE IF NOT EXISTS 'b' AFTER 'a'", e, e), [2]string{"AFTER", "AFTR"}},
		{"R3", fmt.Sprintf("CREATE TYPE %s AS ENUM ('a');\nALTER TYPE %s RENAME VALUE 'a' TO 'c'", e, e), [2]string{"VALUE", "VALUES"}},
		{"R5", fmt.Sprintf("CREATE TYPE %s AS ENUM ('a');\nDROP TYPE %s RESTRICT", e, e), [2]string{"RESTRICT", "RESTRICTED"}},
		{"R5", fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS a;\nDROP VIEW IF EXISTS %s CASCADE", v, v), [2]string{"CASCADE", "CASCADES"}},
		{"R4", fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS a;\nALTER VIEW %s RENAME COLUMN a TO c", v, v), [2]string{"COLUMN", "COLUMNS"}},
		{"R6", fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS 'SELECT 1';\nALTER FUNCTION %s() STABLE STRICT", f, f), [2]string{"STABLE", "STABEL"}},
		{"R5", fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS 'SELECT 1';\nDROP FUNCTION %s()", f, f), [2]string{"FUNCTION", "FUNCTIONS"}},
		{"O1", fmt.Sprintf("CREATE PROCEDURE %s() LANGUAGE sql AS 'SELECT 1';\nCALL %s()", p, p), [2]string{"CALL", "CAL"}},
		{"N3", fmt.Sprintf("CLUSTER %s USING %s", t, ix), [2]string{"USING", "USIN"}},
		{"M5k-a", fmt.Sprintf("ALTER TABLE %s SET LOGGED", t), [2]string{"LOGGED", "LOGED"}},
		{"M5k-a", fmt.Sprintf("ALTER TABLE %s ENABLE TRIGGER ALL", t), [2]string{"TRIGGER", "TRIGGERS"}},
		{"M1b", fmt.Sprintf("CREATE TABLE %s (LIKE %s INCLUDING ALL)", g.name("tl"), t), [2]string{"INCLUDING", "INCLUDE"}},
		{"R1", fmt.Sprintf("CREATE TYPE %s AS (x int, y text)", g.name("ct")), [2]string{"AS", "ASS"}},
	}
	if g.h.fresh {
		forms = append(forms,
			form{"R1", fmt.Sprintf("CREATE SCHEMA %s", g.name("sc")), [2]string{"SCHEMA", "SCHEME"}},
			form{"O2", "CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public", [2]string{"SCHEMA", "SCHEME"}},
			form{"O2", "DROP EXTENSION IF EXISTS pgcrypto RESTRICT", [2]string{"RESTRICT", "RESTRIKT"}})
	}
	fm := pick(g, forms)
	return setup + v7Variant(g, fm.stmt, fm.misspell), fm.row + ".whole_v7", true
}

func genV3Keywords(g *gen) (string, string, bool) {
	k := pick(g, []string{"order", "left", "verbose", "similar", "inner", "user", "current_user", "int", "name", "value"})
	q := `"` + k + `"`
	ref := pick(g, []string{k, q, "%[1]s." + k, "%[1]s." + q})
	t := g.name("t")
	col := func(r string) string { return strings.ReplaceAll(r, "%[1]s", t) }
	create := fmt.Sprintf("CREATE TABLE %s (%s int, b int)", t, q)
	switch g.r.IntN(10) {
	case 0:
		return fmt.Sprintf("%s;\nCREATE INDEX %s ON %s (%s)", create, g.name("ix"), t, col(pick(g, []string{k, q}))), "M4.keywords_v3", true
	case 1:
		return fmt.Sprintf("%s;\nCREATE INDEX %s ON %s ((%s + 1))", create, g.name("ix"), t, col(ref)), "M4.keywords_v3", true
	case 2:
		return fmt.Sprintf("%s;\nCREATE INDEX %s ON %s (b) WHERE %s > 0", create, g.name("ix"), t, col(ref)), "M4.keywords_v3", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a text, b int);\nCREATE INDEX %s ON %s (a %s)", t, g.name("ix"), t, pick(g, []string{k, q, "text_ops"})), "M4.keywords_v3", true
	case 4:
		return fmt.Sprintf("CREATE TABLE %s (%s int) PARTITION BY %s (%s)", t, q, pick(g, []string{"RANGE", "LIST"}), pick(g, []string{k, q})), "M1.keywords_v3", true
	case 5:
		return fmt.Sprintf("CREATE TABLE %s (%s int, EXCLUDE (%s WITH =))", t, q, pick(g, []string{k, q})), "M1.keywords_v3", true
	case 6:
		return fmt.Sprintf("CREATE TABLE %s (%s int CHECK (%s > 0))", t, q, col(ref)), "M1.keywords_v3", true
	case 7:
		return fmt.Sprintf("CREATE TABLE %s (%s int, b int GENERATED ALWAYS AS (%s + 1) STORED)", t, q, col(ref)), "M1.keywords_v3", true
	case 8:
		// SQL value functions bare: refused where an IMMUTABLE expression is
		// needed, accepted in a DEFAULT.
		fn := pick(g, []string{"user", "current_user", "session_user", "current_role", "current_date", "localtimestamp"})
		return pick(g, []string{
			fmt.Sprintf("CREATE TABLE %s (a text);\nCREATE INDEX %s ON %s (%s)", t, g.name("ix"), t, fn),
			fmt.Sprintf("CREATE TABLE %s (a text);\nCREATE INDEX %s ON %s ((a || %s))", t, g.name("ix"), t, fn),
			fmt.Sprintf("CREATE TABLE %s (a text, b text GENERATED ALWAYS AS (%s::text) STORED)", t, fn),
			fmt.Sprintf("CREATE TABLE %s (a text DEFAULT %s)", t, fn),
		}), "M4.value_function_v3", true
	}
	return fmt.Sprintf("%s;\nALTER TABLE %s ADD CHECK (%s > 0)", create, t, col(ref)), "M5f.keywords_v3", true
}

func genV4Kinds(g *gen) (string, string, bool) {
	t, ix, seq, v, mv := g.name("t"), g.name("ix"), g.name("sq"), g.name("v"), g.name("mv")
	setup := fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE INDEX %s ON %s (a);\nCREATE SEQUENCE %s;\nCREATE VIEW %s AS SELECT a FROM %s;\nCREATE MATERIALIZED VIEW %s AS SELECT 1 AS a;\n", t, ix, t, seq, v, t, mv)
	held := map[string]string{"index": ix, "sequence": seq, "view": v, "materialized view": mv, "table": t}
	kind := pick(g, []string{"index", "sequence", "view", "materialized view"})
	name := held[kind]
	switch g.r.IntN(4) {
	case 0:
		name = g.name("nosuch")
	case 1:
		name = held[pick(g, []string{"index", "sequence", "view", "materialized view", "table"})]
	}
	ifExists := ""
	if g.chance(0.2) {
		ifExists = "IF EXISTS "
	}
	K := strings.ToUpper(kind)
	var stmts []string
	switch kind {
	case "index":
		stmts = []string{"ALTER INDEX " + ifExists + name + " RENAME TO " + g.name("ix"), "ALTER INDEX " + ifExists + name + " SET (fillfactor = 50)",
			"ALTER INDEX " + name + " ALTER COLUMN 1 SET STATISTICS 100", "COMMENT ON INDEX " + name + " IS 'x'", "REINDEX INDEX " + name,
			"CLUSTER " + t + " USING " + name}
	case "sequence":
		stmts = []string{"ALTER SEQUENCE " + ifExists + name + " RESTART", "ALTER SEQUENCE " + ifExists + name + " RENAME TO " + g.name("sq"),
			"COMMENT ON SEQUENCE " + name + " IS 'x'", "GRANT SELECT ON SEQUENCE " + name + " TO PUBLIC", "DROP SEQUENCE " + ifExists + name}
	default:
		stmts = []string{"ALTER " + K + " " + ifExists + name + " RENAME TO " + g.name("v"), "COMMENT ON " + K + " " + name + " IS 'x'",
			"DROP " + K + " " + ifExists + name}
		if kind == "materialized view" {
			stmts = append(stmts, "REFRESH MATERIALIZED VIEW "+name)
		} else {
			stmts = append(stmts, "ALTER VIEW "+ifExists+name+" ALTER COLUMN a SET DEFAULT 1")
		}
	}
	stmt := pick(g, stmts)
	if g.chance(0.15) {
		stmt = pick(g, []string{"DROP TRIGGER " + g.name("tr") + " ON ext_a", "DROP TRIGGER IF EXISTS " + g.name("tr") + " ON ext_a",
			"DROP POLICY " + g.name("po") + " ON ext_a", "COMMENT ON CONSTRAINT " + g.name("k") + " ON ext_a IS 'x'", "DROP TYPE " + g.name("ty")})
	}
	return setup + stmt, "N1.object_kinds_v4", true
}

func genV2Languages(g *gen) (string, string, bool) {
	lang := pick(g, []string{"sql", "plpgsql", "'plpgsql'", "'PLPGSQL'", "plperl", "plpython3u", "internal", "c", "'sql'", "SQL"})
	body := "'SELECT 1'"
	switch lang {
	case "plpgsql", "'plpgsql'", "'PLPGSQL'", "plperl", "plpython3u":
		body = "$$ BEGIN RETURN 1; END $$"
	case "internal":
		body = "'int4in'"
	case "c":
		body = "'nosuch_lib', 'f'"
	}
	if g.chance(0.3) {
		dobody := "$$ BEGIN NULL; END $$"
		if lang == "sql" || lang == "'sql'" || lang == "SQL" {
			dobody = "$$ SELECT 1 $$"
		}
		return fmt.Sprintf("DO LANGUAGE %s %s", lang, dobody), "O1.language_v2", true
	}
	return fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE %s AS %s", g.name("f"), lang, body), "R6.language_v2", true
}

func genV9Opclasses(g *gen) (string, string, bool) {
	typ := pick(g, []string{"int", "bigint", "smallint", "text", "varchar(10)", "char(3)", "int[]", "numeric", "date", "timestamptz", "bytea", "inet", "boolean"})
	class := pick(g, []string{"int4_ops", "int8_ops", "int2_ops", "text_ops", "text_pattern_ops", "varchar_ops", "varchar_pattern_ops", "bpchar_ops",
		"bpchar_pattern_ops", "array_ops", "numeric_ops", "date_ops", "timestamptz_ops", "bytea_ops", "inet_ops", "bool_ops", "float8_ops", "nosuch_ops"})
	method := pick(g, []string{"", "", " USING btree", " USING hash", " USING gin", " USING gist"})
	t := g.name("t")
	unique := pick(g, []string{"", "", "UNIQUE "})
	return fmt.Sprintf("CREATE TABLE %s (a %s);\nCREATE %sINDEX %s ON %s%s (a %s)", t, typ, unique, g.name("ix"), t, method, class), "M4.opclass_v9", true
}

func genV10Names(g *gen) (string, string, bool) {
	value := pick(g, []string{"'UTF8'", "'utf-8'", "'Unicode'", "'UTF_8'", "DEFAULT", "'nosuch_enc'", "'utf9'", "UTF8"})
	if g.chance(0.5) {
		return "SET NAMES " + value, "N2.names_v10", true
	}
	return "SET client_encoding TO " + value, "N2.names_v10", true
}

func genV11SystemColumns(g *gen) (string, string, bool) {
	c := pick(g, []string{"ctid", "xmin", "cmin", "xmax", "cmax", "tableoid", "oid", "xmin2", "a2"})
	src, t, ty := g.name("src"), g.name("t"), g.name("ct")
	switch g.r.IntN(9) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE TABLE %s (LIKE %s, %s int)", src, t, src, c), "M1b.system_column_v11", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE TABLE %s (%s int) INHERITS (%s)", src, t, c, src), "M1b.system_column_v11", true
	case 2:
		return fmt.Sprintf("CREATE TYPE %s AS (%s int);\nCREATE TABLE %s OF %s", ty, c, t, ty), "M1b.system_column_v11", true
	case 3:
		typed := ""
		if g.chance(0.6) {
			typed = fmt.Sprintf("CREATE TABLE %s OF %s;\n", t, ty)
		}
		return fmt.Sprintf("CREATE TYPE %s AS (a int);\n%sALTER TYPE %s ADD ATTRIBUTE %s int%s", ty, typed, ty, c, pick(g, []string{"", " CASCADE", " RESTRICT"})), "R4.system_column_v11", true
	case 4:
		return fmt.Sprintf("CREATE TABLE %s AS SELECT 1 AS %s", t, c), "R1.system_column_v11", true
	case 5:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE TABLE %s AS SELECT %s FROM %s", src, t, pick(g, []string{"ctid", "xmin", "a", "a AS " + c, "a " + c}), src), "R1.system_column_v11", true
	case 6:
		return fmt.Sprintf("SELECT 1 AS %s INTO %s", c, t), "R1.system_column_v11", true
	case 7:
		return fmt.Sprintf("CREATE MATERIALIZED VIEW %s AS SELECT 1 AS %s", t, c), "R1.system_column_v11", true
	}
	return fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS %s", t, c), "R1.system_column_v11", true
}

func genV12ViewOptions(g *gen) (string, string, bool) {
	t, v := g.name("t"), g.name("v")
	opt := pick(g, []string{"security_barrier = true", "security_barrier = maybe", "security_barrier", "security_invoker = false", "check_option = local",
		"check_option = cascaded", "check_option = sideways", "fillfactor = 50", "fillfactor = 5", "autovacuum_enabled = false", "toast.autovacuum_enabled = false"})
	setup := fmt.Sprintf("CREATE TABLE %s (a int);\n", t)
	query := pick(g, []string{"SELECT a FROM " + t, "SELECT 1 AS a"})
	switch g.r.IntN(5) {
	case 0:
		return fmt.Sprintf("%sCREATE VIEW %s WITH (%s) AS %s", setup, v, opt, query), "R1.view_options_v12", true
	case 1:
		return fmt.Sprintf("%sCREATE MATERIALIZED VIEW %s WITH (%s) AS %s", setup, v, opt, query), "R1.view_options_v12", true
	case 2:
		if g.chance(0.3) {
			return fmt.Sprintf("%sCREATE VIEW %s AS %s;\nALTER VIEW %s RESET (%s)", setup, v, query, v, strings.SplitN(opt, " ", 2)[0]), "R4.view_options_v12", true
		}
		return fmt.Sprintf("%sCREATE VIEW %s AS %s;\nALTER VIEW %s SET (%s)", setup, v, query, v, opt), "R4.view_options_v12", true
	case 3:
		return fmt.Sprintf("%sCREATE VIEW %s AS %s;\nALTER TABLE %s SET (%s)", setup, v, query, v, opt), "M5k-a.view_options_v12", true
	}
	return fmt.Sprintf("%sCREATE VIEW %s AS %s WITH %sCHECK OPTION", setup, v, query, pick(g, []string{"", "LOCAL ", "CASCADED "})), "R1.view_options_v12", true
}

func genV13Types(g *gen) (string, string, bool) {
	typ := pick(g, []string{"serial", "bigserial", "smallserial", "serial4", "int", "unknown", "record", "anyelement", "void", "cstring", "trigger", "text"})
	t := g.name("t")
	switch g.r.IntN(7) {
	case 0:
		return fmt.Sprintf("CREATE TYPE %s AS (a %s)", g.name("ct"), typ), "R1.type_position_v13", true
	case 1:
		return fmt.Sprintf("CREATE DOMAIN %s AS %s", g.name("d"), typ), "R1.type_position_v13", true
	case 2:
		return fmt.Sprintf("CREATE TABLE %s (a %s)", t, typ), "M1.type_position_v13", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ADD COLUMN b %s", t, t, typ), "M5a.type_position_v13", true
	case 4:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ALTER COLUMN a TYPE %s", t, t, typ), "M5c.type_position_v13", true
	case 5:
		return fmt.Sprintf("CREATE FUNCTION %s(a %s) RETURNS int LANGUAGE sql AS 'SELECT 1'", g.name("f"), typ), "R6.type_position_v13", true
	}
	return fmt.Sprintf("CREATE TYPE %s AS (a int);\nALTER TYPE %s ADD ATTRIBUTE b %s", t, t, typ), "R4.type_position_v13", true
}
