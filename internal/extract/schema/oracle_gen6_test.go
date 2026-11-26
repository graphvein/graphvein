package schema

// Generator g6 (T018a2 Revision 1.4): g5's grammar plus valid and refused
// variants of the forms Revision 1.4 added, so that the oracle pins
// the class rules R1-R5 (rev14.go):
//
//   - R1: CHECK expressions (column and table constraints, ADD CONSTRAINT,
//     ADD COLUMN) over existing and missing columns, of boolean and other
//     types, with aggregates, window functions, subqueries, set-returning
//     functions, IN, LIKE and BETWEEN; EXCLUDE with btree and gist; CREATE
//     DOMAIN checks and defaults;
//   - R2: constraint names written twice across kinds, held by the table,
//     equal to generated names of the same statement, through ADD COLUMN
//     and ALTER INDEX RENAME of a key's index;
//   - R3: column clause combinations (DEFAULT, serial, identity with and
//     without options and on other types, generation expressions,
//     PRIMARY KEY, COLLATE, STORAGE, COMPRESSION), duplicate key columns,
//     MATCH PARTIAL, ON DELETE / ON UPDATE SET column lists, table options
//     and PARTITION BY;
//   - R4: type modifiers in and outside each typmodin's range, in column
//     types, ALTER COLUMN TYPE, casts, domains and composite types;
//   - R5: rows the set's own INSERT and UPDATE wrote, then SET NOT NULL,
//     ADD CONSTRAINT (validated and NOT VALID), ADD COLUMN, ALTER COLUMN
//     TYPE and unique or expression indexes.
//
// g1-g5 keep their grammars (generatorVersion). Every production's tag
// starts with the contract row it exercises and ends with its rule.

import (
	"fmt"
	"strings"
)

var productions6 = []production{
	{8, genR1Check, false},
	{6, genR2Names, false},
	{8, genR3Columns, false},
	{6, genR4Typmods, false},
	{6, genR5Rows, false},
}

// r1Checks are CHECK expressions over a (int), b (text) and r (int4range):
// some Postgres accepts, some it refuses (42703, 42803, 42804, 0A000,
// 42P20, 22P02, 42883).
var r1Checks = []string{
	"a > 0", "a IN (1, 2, 3)", "a NOT IN (4)", "b <> ''", "b LIKE 'x%'", "b NOT ILIKE 'y%'", "a BETWEEN 1 AND 9",
	"a IS NOT NULL", "char_length(b) <= 10", "lower(b) = b", "(a > 0) = (b IS NULL)", "NOT (a < 0)",
	"a + 1 > a", "b ~ '^x'", "a > 0 AND b IS NOT NULL", "random() > 0", "now() > '2020-01-01'",
	"a = ANY ('{1,2}')", "NULL", "'t'", "TRUE",
	"zz > 0", "count(a) > 0", "a", "b", "a IN (SELECT 1)", "sum(a) OVER () > 0", "generate_series(1, a) > 0",
	"a > 'x'", "b IN ('a', 1)", "a LIKE 'x'", "a BETWEEN 'a' AND 'b'", "max(a) = 1", "upper(a) = 'X'",
	"r && int4range(1, 2)", "a > 0 OR zz", "exists (SELECT 1)",
}

func genR1Check(g *gen) (string, string, bool) {
	ex := pick(g, r1Checks)
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "text"}, &gCol{name: "r", typ: "int4range"})
	cols := "a int, b text, r int4range"
	switch g.r.IntN(7) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int CHECK (%s), b text, r int4range)", t.name, ex), "M1.check_column_r1", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (%s, CONSTRAINT %s CHECK (%s))", t.name, cols, g.name("ck"), ex), "M1.check_table_r1", true
	case 2:
		return fmt.Sprintf("CREATE TABLE %s (%s);\nALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", t.name, cols, t.name, g.name("ck"), ex), "M5f.check_r1", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a int, r int4range);\nALTER TABLE %s ADD COLUMN b text CHECK (%s)", t.name, t.name, ex), "M5a.check_r1", true
	case 4:
		excl := pick(g, []string{"EXCLUDE (a WITH =)", "EXCLUDE USING btree (a WITH =, b WITH =)", "EXCLUDE USING btree (a WITH <>)",
			"EXCLUDE USING gist (r WITH &&)", "EXCLUDE USING gist (r WITH =)", "EXCLUDE USING gist (a WITH =)", "EXCLUDE USING hash (a WITH =)",
			"EXCLUDE (zz WITH =)", "EXCLUDE (a WITH =) WHERE (a > 0)", "EXCLUDE (lower(b) WITH =)"})
		if g.chance(0.5) {
			return fmt.Sprintf("CREATE TABLE %s (%s, %s)", t.name, cols, excl), "M1.exclude_r1", true
		}
		return fmt.Sprintf("CREATE TABLE %s (%s);\nALTER TABLE %s ADD %s", t.name, cols, t.name, excl), "M5f.exclude_r1", true
	case 5:
		d := g.name("d")
		dom := pick(g, []string{"int CHECK (VALUE > 0)", "int CHECK (VALUE > zz)", "int CHECK (count(VALUE) > 0)", "text CHECK (VALUE)",
			"int DEFAULT 1 DEFAULT 2", "int NULL NOT NULL", "int DEFAULT 'x'", "int DEFAULT 5 NOT NULL", "text COLLATE \"C\"",
			"int COLLATE \"C\"", "varchar(3) CHECK (VALUE <> '')", "int CONSTRAINT k CHECK (VALUE IN (1, 2))"})
		g.tables = g.tables[:len(g.tables)-1]
		return fmt.Sprintf("CREATE DOMAIN %s AS %s", d, dom), "R1.domain_r1", true
	default:
		expr := pick(g, []string{"a * 2", "count(a)", "zz + 1", "a::text", "lower(b)", "(SELECT 1)", "random()"})
		return fmt.Sprintf("CREATE TABLE %s (a int, b text, r int4range, g text GENERATED ALWAYS AS (%s) STORED)", t.name, expr), "M1.generated_r1", true
	}
}

func genR2Names(g *gen) (string, string, bool) {
	p := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	p.pk = []string{"id"}
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "int"})
	head := fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\n", p.name)
	x := g.name("k")
	kinds := []string{
		"CONSTRAINT %s CHECK (a > 0)", "CONSTRAINT %s UNIQUE (a)", "CONSTRAINT %s PRIMARY KEY (a)",
		"CONSTRAINT %s FOREIGN KEY (b) REFERENCES " + p.name, "CONSTRAINT %s EXCLUDE (b WITH =)",
	}
	k1 := fmt.Sprintf(pick(g, kinds), x)
	k2 := fmt.Sprintf(pick(g, kinds), x)
	genName := pick(g, []string{t.name + "_a_check", t.name + "_check", t.name + "_a_key", t.name + "_pkey", t.name + "_b_fkey", t.name + "_a_check1", t.name + "_x_check"})
	unnamed := pick(g, []string{"CHECK (a > 0)", "CHECK (a > b)", "UNIQUE (a)", "PRIMARY KEY (a)", "FOREIGN KEY (b) REFERENCES " + p.name})
	switch g.r.IntN(7) {
	case 0:
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s, %s)", t.name, k1, k2), "M1.name_twice_r2", true
	case 1:
		named := fmt.Sprintf(pick(g, kinds), genName)
		if g.chance(0.5) {
			return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s, %s)", t.name, unnamed, named), "M1.generated_name_r2", true
		}
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s, %s)", t.name, named, unnamed), "M1.generated_name_first_r2", true
	case 2:
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s);\nALTER TABLE %s ADD %s", t.name, k1, t.name, k2), "M5f.name_held_r2", true
	case 3:
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s);\nALTER TABLE %s ADD COLUMN c int CONSTRAINT %s CHECK (c > 0)", t.name, k1, t.name, x), "M5a.name_held_r2", true
	case 4:
		named := fmt.Sprintf(pick(g, kinds), genName)
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s);\nALTER TABLE %s ADD %s", t.name, unnamed, t.name, named), "M5f.generated_name_r2", true
	case 5:
		pk := g.name("pk")
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, CONSTRAINT %s PRIMARY KEY (a), %s);\nALTER INDEX %s RENAME TO %s", t.name, pk, k1, pk, x), "M8.rename_key_r2", true
	default:
		y := g.name("k")
		return head + fmt.Sprintf("CREATE TABLE %s (a int, b int, %s);\nALTER TABLE %s ADD CONSTRAINT %s CHECK (a > 1), ADD %s", t.name, k1, t.name, y, fmt.Sprintf(pick(g, kinds), y)), "M5f.name_same_statement_r2", true
	}
}

// r3Columns are column definitions of a column c; the other columns are
// a int and y int.
var r3Columns = []string{
	"int DEFAULT 1 DEFAULT 2", "int DEFAULT 1 GENERATED ALWAYS AS IDENTITY", "serial GENERATED ALWAYS AS IDENTITY",
	"int GENERATED ALWAYS AS (a) STORED DEFAULT 1", "int GENERATED BY DEFAULT AS IDENTITY GENERATED ALWAYS AS IDENTITY",
	"int GENERATED ALWAYS AS IDENTITY GENERATED ALWAYS AS (a) STORED", "serial DEFAULT 1",
	"text GENERATED ALWAYS AS IDENTITY", "uuid GENERATED BY DEFAULT AS IDENTITY", "numeric GENERATED ALWAYS AS IDENTITY",
	"bigint GENERATED BY DEFAULT AS IDENTITY", "smallint GENERATED ALWAYS AS IDENTITY", "int GENERATED ALWAYS AS IDENTITY",
	"int GENERATED BY DEFAULT AS IDENTITY (START WITH 0)", "int GENERATED BY DEFAULT AS IDENTITY (START WITH 5)",
	"int GENERATED ALWAYS AS IDENTITY (INCREMENT BY 0)", "int PRIMARY KEY PRIMARY KEY", "int COLLATE \"C\"", "text COLLATE \"C\"",
	"varchar(5) COLLATE \"C\"", "text[] COLLATE \"C\"", "text COLLATE \"C\" COLLATE \"POSIX\"", "text STORAGE EXTERNAL",
	"int STORAGE EXTERNAL", "text COMPRESSION pglz", "int COMPRESSION pglz", "serial[]", "int NULL", "int NOT NULL NOT NULL",
	"int DEFAULT 7", "int UNIQUE UNIQUE", "int CHECK (c > 0) CHECK (c < 9)",
}

func genR3Columns(g *gen) (string, string, bool) {
	p := g.newTable3(&gCol{name: "x", typ: "int", notNull: true}, &gCol{name: "z", typ: "int"})
	p.pk = []string{"x"}
	head := fmt.Sprintf("CREATE TABLE %s (x int PRIMARY KEY, z int, UNIQUE (x, z));\n", p.name)
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "y", typ: "int"})
	col := pick(g, r3Columns)
	switch g.r.IntN(6) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int, y int, c %s)", t.name, col), "M1.column_r3", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (a int, y int);\nALTER TABLE %s ADD COLUMN c %s", t.name, t.name, col), "M5a.column_r3", true
	case 2:
		key := pick(g, []string{"UNIQUE (a, a)", "PRIMARY KEY (a, a)", "UNIQUE (a, y)", "UNIQUE (a) INCLUDE (a)", "UNIQUE (a) INCLUDE (y, y)",
			"PRIMARY KEY (a) INCLUDE (y)", "UNIQUE (a), PRIMARY KEY (y), PRIMARY KEY (a)"})
		if g.chance(0.5) {
			return fmt.Sprintf("CREATE TABLE %s (a int, y int, %s)", t.name, key), "M1.key_columns_r3", true
		}
		return fmt.Sprintf("CREATE TABLE %s (a int, y int);\nALTER TABLE %s ADD %s", t.name, t.name, key), "M5f.key_columns_r3", true
	case 3:
		fk := pick(g, []string{
			"FOREIGN KEY (a) REFERENCES %s MATCH PARTIAL", "FOREIGN KEY (a) REFERENCES %s MATCH FULL",
			"FOREIGN KEY (a, y) REFERENCES %s (x, z) ON DELETE SET NULL (y)", "FOREIGN KEY (a, y) REFERENCES %s (x, z) ON DELETE SET NULL (a, a)",
			"FOREIGN KEY (a) REFERENCES %s ON DELETE SET NULL (y)", "FOREIGN KEY (a) REFERENCES %s ON DELETE SET DEFAULT (zz)",
			"FOREIGN KEY (a) REFERENCES %s ON UPDATE SET NULL (a)", "FOREIGN KEY (g) REFERENCES %s ON DELETE SET NULL",
			"FOREIGN KEY (g) REFERENCES %s ON UPDATE CASCADE", "FOREIGN KEY (g) REFERENCES %s ON DELETE CASCADE",
		})
		fk = fmt.Sprintf(fk, p.name)
		if g.chance(0.5) {
			return head + fmt.Sprintf("CREATE TABLE %s (a int, y int, g int GENERATED ALWAYS AS (a + 1) STORED, %s)", t.name, fk), "M3.fk_options_r3", true
		}
		return head + fmt.Sprintf("CREATE TABLE %s (a int, y int, g int GENERATED ALWAYS AS (a + 1) STORED);\nALTER TABLE %s ADD %s", t.name, t.name, fk), "M3.fk_options_alter_r3", true
	case 4:
		opt := pick(g, []string{"WITH (fillfactor = 50)", "WITH (fillfactor = 5)", "WITH ()", "USING heap", "USING btree", "ON COMMIT DELETE ROWS",
			"TABLESPACE pg_default", "WITHOUT OIDS"})
		return fmt.Sprintf("CREATE TABLE %s (a int, y int) %s", t.name, opt), "M1.table_options_r3", true
	default:
		part := pick(g, []string{
			"(a int, y int) PARTITION BY RANGE (a)", "(a int PRIMARY KEY, y int) PARTITION BY RANGE (a)",
			"(a int, y int PRIMARY KEY) PARTITION BY RANGE (a)", "(a int, y int, UNIQUE (a, y)) PARTITION BY LIST (y)",
			"(a int, y int) PARTITION BY HASH (a)", "(a int, y int) PARTITION BY RANGE (lower(a))", "(a int, y int) PARTITION BY RANGE (zz)",
			"(a int, y int, EXCLUDE (a WITH =)) PARTITION BY RANGE (a)",
		})
		unlogged := ""
		if g.chance(0.2) {
			unlogged = "UNLOGGED "
		}
		return fmt.Sprintf("CREATE %sTABLE %s %s", unlogged, t.name, part), "M1.partition_r3", true
	}
}

// r4Types are written types with modifiers in and outside each typmodin's
// range.
var r4Types = []string{
	"varchar(0)", "varchar(1)", "varchar(10485760)", "varchar(10485761)", "char(0)", "char(3)", "character varying(2)",
	"bit(0)", "bit(3)", "varbit(0)", "bit varying(5)", "numeric(0)", "numeric(1000)", "numeric(1001)", "numeric(5,6)",
	"numeric(5,-2)", "numeric(5,1001)", "numeric(5,-1001)", "decimal(3,1)", "timestamp(7)", "timestamp(6)", "timestamp(0)",
	"timestamptz(9)", "timestamp(8) with time zone", "time(7)", "timetz(3)", "time(9) with time zone", "interval(7)",
	"interval(3)", "interval second(2)", "float(0)", "float(54)", "float(24)", "float(53)", "int4(5)", "text(5)",
	"varchar(3)[]", "char(0)[]", "uuid(1)",
}

func genR4Typmods(g *gen) (string, string, bool) {
	typ := pick(g, r4Types)
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	switch g.r.IntN(6) {
	case 0, 1:
		return fmt.Sprintf("CREATE TABLE %s (a int, c %s)", t.name, typ), "M2.typmod_r4", true
	case 2:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ADD COLUMN c %s", t.name, t.name, typ), "M5a.typmod_r4", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a int, c text);\nALTER TABLE %s ALTER COLUMN c TYPE %s USING NULL", t.name, t.name, typ), "M5c.typmod_r4", true
	case 4:
		return fmt.Sprintf("CREATE TABLE %s (a int, c text DEFAULT CAST(NULL AS %s))", t.name, typ), "M1.typmod_cast_r4", true
	default:
		g.tables = g.tables[:len(g.tables)-1]
		if g.chance(0.5) {
			return fmt.Sprintf("CREATE DOMAIN %s AS %s", g.name("d"), typ), "R1.domain_typmod_r4", true
		}
		return fmt.Sprintf("CREATE TYPE %s AS (a int, c %s)", g.name("ct"), typ), "R1.composite_typmod_r4", true
	}
}

func genR5Rows(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "a", typ: "int"}, &gCol{name: "b", typ: "int"})
	p := g.newTable3(&gCol{name: "id", typ: "int", notNull: true})
	p.pk = []string{"id"}
	var sql strings.Builder
	fmt.Fprintf(&sql, "CREATE TABLE %s (id int PRIMARY KEY);\nCREATE TABLE %s (a int, b int)", p.name, t.name)
	rows := pick(g, []string{"(1, 1)", "(1, NULL)", "(1, 2), (1, 3)", "(1, -1)", "(2, 70000)", "(NULL, 1)"})
	switch g.r.IntN(4) {
	case 0:
	case 1:
		fmt.Fprintf(&sql, ";\nINSERT INTO %s (a, b) VALUES %s", t.name, rows)
	case 2:
		fmt.Fprintf(&sql, ";\nINSERT INTO %s (a) VALUES (1)", t.name)
	default:
		fmt.Fprintf(&sql, ";\nINSERT INTO %s (a, b) VALUES %s;\nUPDATE %s SET b = NULL", t.name, rows, t.name)
	}
	if g.chance(0.2) {
		fmt.Fprintf(&sql, ";\nTRUNCATE %s", t.name)
	}
	stmt := pick(g, []string{
		"ALTER TABLE %[1]s ALTER COLUMN b SET NOT NULL", "ALTER TABLE %[1]s ADD CONSTRAINT %[2]s CHECK (b > 0)",
		"ALTER TABLE %[1]s ADD CONSTRAINT %[2]s CHECK (b > 0) NOT VALID", "ALTER TABLE %[1]s ADD PRIMARY KEY (a)",
		"ALTER TABLE %[1]s ADD UNIQUE (a)", "CREATE UNIQUE INDEX ON %[1]s (a)", "CREATE INDEX ON %[1]s ((10 / b))",
		"CREATE INDEX ON %[1]s (a)", "ALTER TABLE %[1]s ALTER COLUMN b TYPE smallint", "ALTER TABLE %[1]s ALTER COLUMN b TYPE text",
		"ALTER TABLE %[1]s ADD COLUMN c int NOT NULL", "ALTER TABLE %[1]s ADD COLUMN c int NOT NULL DEFAULT 0",
		"ALTER TABLE %[1]s ADD COLUMN c int UNIQUE DEFAULT 1", "ALTER TABLE %[1]s ADD COLUMN c int DEFAULT 1",
		"ALTER TABLE %[1]s ADD CONSTRAINT %[2]s FOREIGN KEY (a) REFERENCES %[3]s",
		"ALTER TABLE %[1]s ADD CONSTRAINT %[2]s FOREIGN KEY (a) REFERENCES %[3]s NOT VALID",
		"ALTER TABLE %[1]s ALTER COLUMN b DROP NOT NULL",
	})
	fmt.Fprintf(&sql, ";\n"+stmt, t.name, g.name("k"), p.name)
	return sql.String(), "M5.rows_r5", true
}
