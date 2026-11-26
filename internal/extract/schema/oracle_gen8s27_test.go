package schema

// Generator g8, the 2026-11-07 production for the V4 kinds rule (T018a2
// Revision 1.6 V4, contract-violation fix of g8-s4299), appended to
// productions8: ALTER INDEX, DROP INDEX, ALTER TABLE, ALTER TYPE, ALTER
// DOMAIN and ALTER VIEW / MATERIALIZED VIEW ... RENAME COLUMN, with and
// without IF EXISTS, of names held as a table, an index, a sequence, a
// view, a materialized view, a composite type, an enum or a domain, and of
// a name not held. Relations share pg_class's namespace, so a form
// addressed to one kind meets a name held by another.

import (
	"fmt"
	"strings"
)

func init() {
	productions8 = append(productions8, production{5, genV4RelKinds, false})
	// Revision 1.7 (W1-W7), appended after the 2026-11-07 production so
	// the earlier productions keep their weights' order.
	productions8 = append(productions8,
		production{5, genW1ObjectNames, false},
		production{3, genW2RegArguments, false},
		production{2, genW3SQLBodies, false},
		production{2, genW4Databases, false},
		production{3, genW5Drops, false},
		production{3, genW6Family, false},
		production{3, genW7Labels, false},
	)
}

func genV4RelKinds(g *gen) (string, string, bool) {
	t, ix, seq, v, mv, ct, e, d := g.name("t"), g.name("ix"), g.name("sq"), g.name("v"), g.name("mv"), g.name("ct"), g.name("e"), g.name("d")
	setup := fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE INDEX %s ON %s (a);\nCREATE SEQUENCE %s;\nCREATE VIEW %s AS SELECT a FROM %s;\n"+
		"CREATE MATERIALIZED VIEW %s AS SELECT 1 AS a;\nCREATE TYPE %s AS (a int);\nCREATE TYPE %s AS ENUM ('a');\nCREATE DOMAIN %s AS int;\n",
		t, ix, t, seq, v, t, mv, ct, e, d)
	name := pick(g, []string{t, ix, seq, v, mv, ct, e, d, g.name("nosuch")})
	ifExists := ""
	if g.chance(0.5) {
		ifExists = "IF EXISTS "
	}
	var stmt, tag string
	switch g.r.IntN(6) {
	case 0:
		stmt = "ALTER INDEX " + ifExists + name + " " + pick(g, []string{"SET (fillfactor = 50)", "RESET (fillfactor)", "SET TABLESPACE pg_default",
			"RENAME TO " + g.name("ix"), "SET LOGGED", "SET UNLOGGED", "SET WITHOUT CLUSTER", "SET ACCESS METHOD heap"})
		tag = "M8.relkinds_v4"
	case 1:
		stmt = "DROP INDEX " + ifExists + name + pick(g, []string{"", " CASCADE"})
		tag = "M7.relkinds_v4"
	case 2:
		stmt = "ALTER TABLE " + ifExists + name + " " + pick(g, []string{"ADD COLUMN b int", "SET LOGGED", "SET UNLOGGED", "OWNER TO CURRENT_USER",
			"RENAME TO " + g.name("r"), "ALTER COLUMN a SET DEFAULT 1", "ALTER COLUMN a DROP DEFAULT", "DROP COLUMN a", "ENABLE ROW LEVEL SECURITY",
			"RENAME COLUMN a TO b", "ALTER COLUMN a TYPE bigint", "SET (fillfactor = 50)", "SET WITHOUT CLUSTER", "ADD CHECK (true)"})
		tag = "M5a.relkinds_v4"
	case 3:
		stmt = "ALTER TYPE " + name + " " + pick(g, []string{"OWNER TO CURRENT_USER", "RENAME TO " + g.name("ty"), "ADD ATTRIBUTE b int",
			"RENAME ATTRIBUTE a TO b", "ALTER ATTRIBUTE a TYPE bigint", "ADD VALUE 'z'"})
		tag = "R4.relkinds_v4"
	case 4:
		stmt = "ALTER DOMAIN " + name + " " + pick(g, []string{"SET NOT NULL", "SET DEFAULT 1", "OWNER TO CURRENT_USER", "DROP CONSTRAINT c",
			"DROP CONSTRAINT IF EXISTS c", "VALIDATE CONSTRAINT c"})
		tag = "R4.relkinds_v4"
	default:
		stmt = pick(g, []string{"ALTER VIEW ", "ALTER MATERIALIZED VIEW "}) + ifExists + name + " " + pick(g, []string{"RENAME COLUMN a TO b",
			"RENAME a TO b", "OWNER TO CURRENT_USER", "RENAME TO " + g.name("w")})
		tag = "R4.relkinds_v4"
	}
	return setup + stmt, tag, true
}

// genW1ObjectNames (Revision 1.7 W1): triggers and policies created,
// dropped and renamed on a table, the table renamed away or created again,
// a second table, a trigger created by code; then DROP / COMMENT ON
// TRIGGER | POLICY | RULE and ALTER POLICY of held, dropped, renamed,
// other-table and never-held names, with and without IF EXISTS.
func genW1ObjectNames(g *gen) (string, string, bool) {
	t, u, f, tr, po := g.name("t"), g.name("u"), g.name("tf"), g.name("tr"), g.name("po")
	sql := fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE TABLE %s (a int);\nCREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\n", t, u, f)
	if g.chance(0.2) {
		sql += fmt.Sprintf("DO $$ BEGIN EXECUTE 'CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()'; END $$;\n", tr, t, f)
	} else {
		sql += fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();\n", tr, t, f)
	}
	sql += fmt.Sprintf("CREATE POLICY %s ON %s USING (true);\n", po, t)
	switch g.r.IntN(5) {
	case 0:
		sql += fmt.Sprintf("DROP TRIGGER %s ON %s;\nDROP POLICY %s ON %s;\n", tr, t, po, t)
	case 1:
		q := g.name("po")
		sql += fmt.Sprintf("ALTER POLICY %s ON %s RENAME TO %s;\n", po, t, q)
	case 2:
		sql += fmt.Sprintf("ALTER TABLE %s RENAME TO %s;\nCREATE TABLE %s (a int);\n", t, g.name("old"), t)
	case 3:
		sql += fmt.Sprintf("DROP TABLE %s;\nCREATE TABLE %s (a int);\n", t, t)
	}
	table := pick(g, []string{t, t, t, u})
	name := pick(g, []string{tr, po, g.name("nosuch")})
	ifExists := ""
	if g.chance(0.2) {
		ifExists = "IF EXISTS "
	}
	var stmt string
	switch g.r.IntN(7) {
	case 0:
		stmt = fmt.Sprintf("DROP TRIGGER %s%s ON %s", ifExists, name, table)
	case 1:
		stmt = fmt.Sprintf("DROP POLICY %s%s ON %s", ifExists, name, table)
	case 2:
		stmt = fmt.Sprintf("DROP RULE %s%s ON %s", ifExists, name, table)
	case 3:
		stmt = fmt.Sprintf("ALTER POLICY %s ON %s USING (false)", name, table)
	case 4:
		stmt = fmt.Sprintf("COMMENT ON TRIGGER %s ON %s IS 'x'", name, table)
	case 5:
		stmt = fmt.Sprintf("COMMENT ON POLICY %s ON %s IS 'x'", name, table)
	default:
		stmt = fmt.Sprintf("COMMENT ON RULE %s ON %s IS 'x'", name, table)
	}
	return sql + stmt, "N1.objnames_w1", true
}

// genW2RegArguments (Revision 1.7 W2): set functions with reg*, text and
// set-type parameters called with held, missing, qualified and quoted
// names, positionally and by name.
func genW2RegArguments(g *gen) (string, string, bool) {
	t, f := g.name("t"), g.name("f")
	typ := pick(g, []string{"regclass", "regclass", "regclass", "regtype", "regnamespace", "text", t})
	sql := fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE FUNCTION %s(p %s) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\n", t, f, typ)
	arg := pick(g, []string{t, t, strings.ToUpper(t), `"` + t + `"`, "public." + t, g.name("nosuch"), "int4", "pg_catalog"})
	if typ == t {
		arg = "(1)"
	}
	call := fmt.Sprintf("SELECT %s('%s')", f, arg)
	switch g.r.IntN(4) {
	case 0:
		call = fmt.Sprintf("SELECT %s(p => '%s')", f, arg)
	case 1:
		call = fmt.Sprintf("CREATE TABLE %s (b int DEFAULT %s('%s'))", g.name("d"), f, arg)
	}
	return sql + call, "O1.regargs_w2", true
}

// genW3SQLBodies (Revision 1.7 W3): LANGUAGE sql bodies created with
// check_function_bodies off or on, valid, with a syntax error, naming a
// missing table, or SQL-standard, then called.
func genW3SQLBodies(g *gen) (string, string, bool) {
	f, t := g.name("f"), g.name("t")
	sql := fmt.Sprintf("CREATE TABLE %s (a int);\n", t)
	if g.chance(0.7) {
		sql += "SET check_function_bodies = " + pick(g, []string{"off", "false", "0"}) + ";\n"
	}
	body := pick(g, []string{
		"RETURNS int LANGUAGE sql AS 'SELECT 1'",
		"RETURNS int LANGUAGE sql AS 'SELECT 1 +'",
		fmt.Sprintf("RETURNS bigint LANGUAGE sql AS 'SELECT count(*) FROM %s'", t),
		fmt.Sprintf("RETURNS bigint LANGUAGE sql AS 'SELECT count(*) FROM %s'", g.name("nosuch")),
		"RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; END",
		"RETURNS int LANGUAGE sql RETURN 1",
	})
	sql += fmt.Sprintf("CREATE FUNCTION %s() %s;\n", f, body)
	return sql + pick(g, []string{"SELECT " + f + "()", "SELECT * FROM " + f + "()", "CREATE TABLE " + g.name("d") + " (b int DEFAULT " + f + "())"}), "O1.sqlbodies_w3", true
}

// genW4Databases (Revision 1.7 W4): statements that need a database, of
// names the cluster does not hold (never the harness's own database, whose
// settings would reach later histories).
func genW4Databases(g *gen) (string, string, bool) {
	db := g.name("nosuch_db")
	return pick(g, []string{
		"ALTER DATABASE " + db + " SET work_mem = '1MB'",
		"ALTER DATABASE " + db + " RESET work_mem",
		"ALTER DATABASE " + db + " OWNER TO CURRENT_USER",
		"ALTER DATABASE " + db + " CONNECTION LIMIT 5",
		"ALTER DATABASE " + db + " WITH ALLOW_CONNECTIONS true",
		"GRANT CONNECT ON DATABASE " + db + " TO PUBLIC",
		"REVOKE ALL ON DATABASE " + db + " FROM PUBLIC",
		"ALTER ROLE CURRENT_USER IN DATABASE " + db + " SET work_mem = '1MB'",
		"COMMENT ON DATABASE " + db + " IS 'x'",
	}), "N1.databases_w4", true
}

// genW5Drops (Revision 1.7 W5): DROP STATISTICS, PUBLICATION (fresh
// databases only: publications are database-wide), COLLATION, OPERATOR,
// AGGREGATE and CAST of held and not-held names, with and without IF
// EXISTS.
func genW5Drops(g *gen) (string, string, bool) {
	t, st, co := g.name("t"), g.name("st"), g.name("co")
	sql := fmt.Sprintf("CREATE TABLE %s (a int, b int);\nCREATE STATISTICS %s ON a, b FROM %s;\nCREATE COLLATION %s (provider = icu, locale = 'und');\n", t, st, t, co)
	ifExists := ""
	if g.chance(0.25) {
		ifExists = "IF EXISTS "
	}
	nosuch := g.name("nosuch")
	choices := []string{
		"DROP STATISTICS " + ifExists + pick(g, []string{st, nosuch}),
		"DROP COLLATION " + ifExists + pick(g, []string{co, nosuch}),
		"DROP COLLATION " + co + ";\nDROP COLLATION " + ifExists + co,
		"DROP OPERATOR " + ifExists + "=== (int, int)",
		"DROP AGGREGATE " + ifExists + nosuch + "(int)",
		"DROP CAST " + ifExists + "(int AS " + pick(g, []string{"point", "money"}) + ")",
	}
	if g.h.fresh {
		pub := g.name("pub")
		sql += "CREATE PUBLICATION " + pub + ";\n"
		choices = append(choices, "DROP PUBLICATION "+ifExists+pick(g, []string{pub, nosuch}), "DROP PUBLICATION "+pub+";\nDROP PUBLICATION "+ifExists+pub)
	}
	stmt := pick(g, choices)
	tag := "N1.drops_w5"
	if strings.Contains(stmt, "COLLATION") || strings.Contains(stmt, "AGGREGATE") {
		tag = "R5.drops_w5"
	}
	return sql + stmt, tag, true
}

// genW6Family (Revision 1.7 W6): rows written into inheritance children
// and partitions, by top-level INSERT and by DO blocks, then a statement
// that checks the parent's rows.
func genW6Family(g *gen) (string, string, bool) {
	p, c := g.name("p"), g.name("c")
	var sql string
	if g.chance(0.5) {
		sql = fmt.Sprintf("CREATE TABLE %s (k int, b int);\nCREATE TABLE %s () INHERITS (%s);\n", p, c, p)
	} else {
		sql = fmt.Sprintf("CREATE TABLE %s (k int, b int) PARTITION BY LIST (k);\nCREATE TABLE %s PARTITION OF %s FOR VALUES IN (1);\n", p, c, p)
	}
	rows := pick(g, []string{"(1, NULL)", "(1, 2), (1, 2)", "(1, 2)", "(1, -1)"})
	if g.chance(0.5) {
		sql += fmt.Sprintf("DO $$ BEGIN INSERT INTO %s VALUES %s; END $$;\n", c, rows)
	} else if g.chance(0.8) {
		sql += fmt.Sprintf("INSERT INTO %s VALUES %s;\n", c, rows)
	}
	return sql + pick(g, []string{
		"ALTER TABLE " + p + " ALTER COLUMN b SET NOT NULL",
		"ALTER TABLE " + p + " ADD CHECK (b > 0)",
		"ALTER TABLE " + p + " ADD UNIQUE (k, b)",
		"ALTER TABLE " + p + " ADD PRIMARY KEY (k, b)",
		"ALTER TABLE " + p + " ALTER COLUMN b TYPE smallint",
	}), "M5d.family_w6", true
}

// genW7Labels (Revision 1.7 W7): enum labels written as plain, escape,
// Unicode and dollar-quoted strings, then ADD VALUE and RENAME VALUE of
// equal and different labels.
func genW7Labels(g *gen) (string, string, bool) {
	e := g.name("e")
	forms := []string{"'ab'", "E'ab'", "E'a\\x62'", "E'a\\142'", "U&'\\0061b'", "$q$ab$q$", "E'a\\tb'", "'a\\x62'", "'a''b'", "E'a\\'b'"}
	sql := fmt.Sprintf("CREATE TYPE %s AS ENUM (%s, 'z');\n", e, pick(g, forms))
	switch g.r.IntN(3) {
	case 0:
		sql += fmt.Sprintf("ALTER TYPE %s ADD VALUE %s", e, pick(g, forms))
	case 1:
		sql += fmt.Sprintf("ALTER TYPE %s RENAME VALUE %s TO 'y'", e, pick(g, forms))
	default:
		sql += fmt.Sprintf("ALTER TYPE %s ADD VALUE IF NOT EXISTS %s BEFORE %s", e, pick(g, forms), pick(g, forms))
	}
	return sql, "R3.labels_w7", true
}
