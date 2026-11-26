package schema

// Generator g9, step U-c productions (T018a2 Revision 1.9): the dependency
// rule (Y39 with Y40, Y41, Y44, Y45 and Y52), statements code runs (Y48,
// Y49, Y50) and the superuser-only forms (Y51), each with its refused,
// may-fail and accepted neighbours (renames, quoting, qualification, IF
// EXISTS, CASCADE, across migrations).

import "fmt"

func init() {
	productions9 = append(productions9,
		production{3, genY39Deps, false},
		production{2, genY40OwnedSeq, false},
		production{2, genY41AtomicCols, false},
		production{2, genY44CopiedTypes, false},
		production{1, genY45RenameDrop, false},
		production{2, genY48ConstExec, false},
		production{2, genY49DoStatic, false},
		production{2, genY50StaleCall, false},
		production{2, genY51Superuser, false},
		production{3, genY52CascadeCols, false},
	)
}

// genY39Deps writes a dependent whose name equals the object it depends
// on (all pairs of kinds that may share a name), quoted or qualified,
// sometimes in a later migration, then a drop of the object with or
// without the dependent dropped first, CASCADE or IF EXISTS.
func genY39Deps(g *gen) (string, string, bool) {
	n, t := g.name("dep"), g.name("t")
	q := n
	if g.chance(0.2) {
		q = `"` + n + `"`
	}
	sql := fmt.Sprintf("CREATE TABLE %s (a int, b timestamptz)", t)
	drop := fmt.Sprintf("DROP FUNCTION %s%s()", pick(g, []string{"", "IF EXISTS "}), q)
	first := ""
	switch g.r.IntN(7) {
	case 0: // a trigger named like its function
		sql += fmt.Sprintf(";\n%s;\nCREATE %sTRIGGER %s BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s()", trigFunc(q), pick(g, []string{"", "OR REPLACE "}), q, t, q)
		first = fmt.Sprintf("DROP TRIGGER %s ON %s", q, t)
	case 1: // a trigger on a table named like its function
		sql = fmt.Sprintf("CREATE TABLE %s (a int, b timestamptz);\n%s;\nCREATE TRIGGER %s_tg BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()", q, trigFunc(q), n, q, q)
		first = fmt.Sprintf("DROP TRIGGER %s_tg ON %s", n, q)
		if g.chance(0.5) {
			drop = "DROP TABLE " + q
			first = ""
		}
	case 2: // a domain default calling a function of its name
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE DOMAIN %s AS int%s", q, q, pick(g, []string{" DEFAULT " + q + "()", ""}))
		if g.chance(0.5) {
			sql += fmt.Sprintf(";\nALTER DOMAIN %s SET DEFAULT %s()", q, q)
		}
		first = "DROP DOMAIN " + q
	case 3: // a SQL-standard body named like the table it uses
		sql = fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE PROCEDURE %s() BEGIN ATOMIC DELETE FROM %s; END", q, q, q)
		drop = "DROP TABLE " + q
		first = "DROP PROCEDURE " + q
	case 4: // a function named like the sequence it uses
		sql = fmt.Sprintf("CREATE SEQUENCE %s;\nCREATE FUNCTION %s() RETURNS bigint BEGIN ATOMIC SELECT nextval('%s'); END", q, q, n)
		drop = "DROP SEQUENCE " + q
		first = "DROP FUNCTION " + q
	case 5: // a function named like the type it returns
		sql = fmt.Sprintf("CREATE TYPE %s AS (a int, b int);\nCREATE FUNCTION %s(a int, b int) RETURNS %s BEGIN ATOMIC SELECT ROW(a, b)::%s; END", q, q, q, q)
		drop = "DROP TYPE " + q
		first = "DROP FUNCTION " + q + "(int, int)"
	default: // a constraint trigger, and a trigger dropped on another table
		u := g.name("u")
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a int);\n%s;\nCREATE CONSTRAINT TRIGGER %s AFTER INSERT ON %s DEFERRABLE FOR EACH ROW EXECUTE FUNCTION %s();\nDROP TRIGGER IF EXISTS %s ON %s", u, trigFunc(q), q, t, q, q, u)
		first = fmt.Sprintf("DROP TRIGGER %s ON %s", q, t)
	}
	if first != "" && g.chance(0.5) {
		sql += ";\n" + first
	}
	if g.chance(0.3) {
		drop += " CASCADE"
	}
	return sql + ";\n" + drop, "R5.deps_y39", true
}

// genY40OwnedSeq uses a column's sequence by name (serial, identity,
// OWNED BY), sometimes after renaming the table, then drops the table,
// the column or the identity.
func genY40OwnedSeq(g *gen) (string, string, bool) {
	t, f := g.name("t"), g.name("nx")
	col := pick(g, []string{"id serial PRIMARY KEY", "id bigint GENERATED ALWAYS AS IDENTITY", "id int"})
	sql := fmt.Sprintf("CREATE TABLE %s (%s, n int)", t, col)
	seq := t + "_id_seq"
	if col == "id int" {
		seq = g.name("sq")
		sql += fmt.Sprintf(";\nCREATE SEQUENCE %s OWNED BY %s.id", seq, t)
	}
	if g.chance(0.5) {
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS bigint BEGIN ATOMIC SELECT nextval('%s'); END", f, pick(g, []string{seq, seq, "other_seq"}))
	} else {
		u := g.name("u")
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a bigint DEFAULT nextval('%s'))", u, seq)
	}
	if g.chance(0.3) {
		t2 := g.name("t")
		sql += fmt.Sprintf(";\nALTER TABLE %s RENAME TO %s", t, t2)
		t = t2
	}
	end := pick(g, []string{"DROP TABLE " + t, "ALTER TABLE " + t + " DROP COLUMN id", "DROP TABLE " + t + " CASCADE"})
	if col != "id serial PRIMARY KEY" && col != "id int" && g.chance(0.3) {
		end = "ALTER TABLE " + t + " ALTER COLUMN id DROP IDENTITY"
	}
	sql += ";\n" + end
	if end == "DROP TABLE "+t+" CASCADE" && g.chance(0.5) {
		sql += fmt.Sprintf(";\nSELECT %s()", f)
	}
	return sql, "M6.ownedseq_y40", true
}

// genY41AtomicCols writes a SQL-standard body over a table, then drops
// or retypes a column of that table or of another one.
func genY41AtomicCols(g *gen) (string, string, bool) {
	t, o, f := g.name("t"), g.name("o"), g.name("fn")
	sql := fmt.Sprintf("CREATE TABLE %s (id int, name text);\nCREATE TABLE %s (x int, y text)", t, o)
	switch g.r.IntN(3) {
	case 0:
		sql += fmt.Sprintf(";\nCREATE PROCEDURE %s(n text) BEGIN ATOMIC INSERT INTO %s (name) VALUES (n); END", f, t)
	case 1:
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS text BEGIN ATOMIC SELECT name FROM %s LIMIT 1; END", f, t)
	default:
		sql += fmt.Sprintf(";\nCREATE FUNCTION %s() RETURNS bigint RETURN (SELECT count(*) FROM %s)", f, t)
	}
	target := pick(g, []string{t, t, o})
	col := "name"
	if target == o {
		col = "y"
	}
	return sql + ";\n" + pick(g, []string{
		fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", target, col),
		fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s CASCADE", target, col),
		fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE varchar(10)", target, col),
	}), "M5c.atomiccols_y41", true
}

// genY44CopiedTypes copies columns of a type (SELECT INTO, CREATE TABLE
// AS, LIKE, INHERITS, a view), drops the source, then the type.
func genY44CopiedTypes(g *gen) (string, string, bool) {
	ty, t, c := g.name("st"), g.name("o"), g.name("c")
	sql := fmt.Sprintf("CREATE TYPE %s AS ENUM ('a', 'b');\nCREATE TABLE %s (id int PRIMARY KEY, s %s)", ty, t, pick(g, []string{ty, ty, "text"}))
	switch g.r.IntN(5) {
	case 0:
		sql += fmt.Sprintf(";\nSELECT * INTO %s FROM %s", c, t)
	case 1:
		sql += fmt.Sprintf(";\nCREATE TABLE %s AS SELECT * FROM %s", c, t)
	case 2:
		sql += fmt.Sprintf(";\nCREATE TABLE %s (LIKE %s)", c, t)
	case 3:
		sql += fmt.Sprintf(";\nCREATE TABLE %s () INHERITS (%s);\nALTER TABLE %s NO INHERIT %s", c, t, c, t)
	default:
		sql += fmt.Sprintf(";\nCREATE VIEW %s AS SELECT s FROM %s;\nDROP VIEW %s", c, t, c)
	}
	return sql + fmt.Sprintf(";\nDROP TABLE %s;\nDROP TYPE %s", t, ty), "R5.copiedtypes_y44", true
}

// genY45RenameDrop renames an object with dependents, drops the old name
// (IF EXISTS: nothing is removed) or the new one, then the object it
// depends on.
func genY45RenameDrop(g *gen) (string, string, bool) {
	ty, f, f2 := g.name("st"), g.name("g"), g.name("g")
	sql := fmt.Sprintf("CREATE TYPE %s AS ENUM ('a');\nCREATE FUNCTION %s() RETURNS %s LANGUAGE sql AS $$ SELECT 'a'::%s $$;\nALTER FUNCTION %s() RENAME TO %s", ty, f, ty, ty, f, f2)
	return sql + ";\n" + pick(g, []string{
		fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", f),
		fmt.Sprintf("DROP FUNCTION %s()", f2),
		fmt.Sprintf("DROP FUNCTION IF EXISTS %s", f),
	}) + fmt.Sprintf(";\nDROP TYPE %s", ty), "R5.renamedrop_y45", true
}

// genY48ConstExec runs statements from constants in a DO body or a
// called procedure: CREATE FUNCTION with good and bad bodies, and DDL.
func genY48ConstExec(g *gen) (string, string, bool) {
	f, t := g.name("cf"), g.name("t")
	stmt := pick(g, []string{
		fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1; END $b$", f),
		fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1 END $b$", f),
		fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS ''SELECT 1 +''", f),
		fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS ''SELECT 1''", f),
		fmt.Sprintf("CREATE TABLE %s (a int)", t),
		fmt.Sprintf("DROP TABLE %s", t),
		fmt.Sprintf("DROP INDEX %s_pkey", t),
	})
	sql := fmt.Sprintf("CREATE TABLE %s_x (id int PRIMARY KEY)", t)
	if g.chance(0.5) {
		return sql + fmt.Sprintf(";\nDO $$ BEGIN EXECUTE '%s'; END $$", stmt), "O1.constexec_y48", true
	}
	p := g.name("pr")
	return sql + fmt.Sprintf(";\nCREATE PROCEDURE %s() LANGUAGE plpgsql AS $$ BEGIN EXECUTE '%s'; END $$;\nCALL %s()", p, stmt, p), "O1.constexec_y48", true
}

// genY49DoStatic writes static statements in a DO body or a called
// routine that name relations, columns and regclass arguments, held or
// not.
func genY49DoStatic(g *gen) (string, string, bool) {
	t, f := g.name("t"), g.name("rg")
	sql := fmt.Sprintf("CREATE TABLE %s (id int, a text);\nCREATE FUNCTION %s(r regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$", t, f)
	body := pick(g, []string{
		fmt.Sprintf("PERFORM %s('%s')", f, t),
		fmt.Sprintf("PERFORM %s('%sz')", f, t),
		fmt.Sprintf("PERFORM a FROM %s", t),
		fmt.Sprintf("PERFORM b FROM %s", t),
		fmt.Sprintf("INSERT INTO %s (id) VALUES (1)", t),
		fmt.Sprintf("INSERT INTO %s (nosuch) VALUES (1)", t),
		fmt.Sprintf("UPDATE %s SET a = 'x'", t),
		fmt.Sprintf("DELETE FROM %sz", t),
	})
	if g.chance(0.5) {
		return sql + fmt.Sprintf(";\nDO $$ BEGIN %s; END $$", body), "O1.dostatic_y49", true
	}
	p := g.name("pr")
	return sql + fmt.Sprintf(";\nCREATE PROCEDURE %s() LANGUAGE plpgsql AS $$ BEGIN %s; END $$;\nCALL %s()", p, body, p), "O1.dostatic_y49", true
}

// genY50StaleCall calls a PL/pgSQL routine after an object or column its
// body names was dropped, renamed or retyped (or before, or after a
// CREATE OR REPLACE), also through a trigger.
func genY50StaleCall(g *gen) (string, string, bool) {
	t, f := g.name("t"), g.name("sf")
	sql := fmt.Sprintf("CREATE TABLE %s (a int, b int)", t)
	fn := fmt.Sprintf("CREATE OR REPLACE FUNCTION %s() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT count(a) FROM %s); END $$", f, t)
	change := pick(g, []string{
		fmt.Sprintf("ALTER TABLE %s RENAME COLUMN a TO c", t),
		fmt.Sprintf("ALTER TABLE %s DROP COLUMN b", t),
		fmt.Sprintf("ALTER TABLE %s ALTER COLUMN a TYPE bigint", t),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s_r", t, t),
		fmt.Sprintf("CREATE INDEX ON %s (a)", t),
	})
	switch g.r.IntN(3) {
	case 0:
		sql += ";\n" + fn + ";\n" + change + fmt.Sprintf(";\nSELECT %s()", f)
	case 1:
		sql += ";\n" + change + ";\n" + fn + fmt.Sprintf(";\nSELECT %s()", f)
	default:
		sql += ";\n" + fn + ";\n" + change + ";\n" + fn + fmt.Sprintf(";\nSELECT %s()", f)
	}
	return sql, "O1.stalecall_y50", true
}

// genY51Superuser writes each superuser-only form once, and the trusted
// forms that stay claimed.
func genY51Superuser(g *gen) (string, string, bool) {
	r, f, t := g.name("su"), g.name("f"), g.name("t")
	forms := []string{
		fmt.Sprintf("CREATE FUNCTION %s() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;\nCREATE EVENT TRIGGER %s_e ON ddl_command_end EXECUTE FUNCTION %s()", f, f, f),
		fmt.Sprintf("CREATE ROLE %s SUPERUSER", r),
		fmt.Sprintf("CREATE ROLE %s;\nALTER ROLE %s %s", r, r, pick(g, []string{"SUPERUSER", "NOSUPERUSER", "CREATEDB", "SET work_mem = '4MB'", "SET gv9.x = '1'"})),
		fmt.Sprintf("CREATE FUNCTION %s(int) RETURNS int LANGUAGE %s AS 'int4abs'", f, pick(g, []string{"c", "internal"})),
		fmt.Sprintf("CREATE FUNCTION %s(int) RETURNS int LANGUAGE sql %s AS 'SELECT $1'", f, pick(g, []string{"LEAKPROOF", "NOT LEAKPROOF", "IMMUTABLE"})),
		fmt.Sprintf("CREATE TYPE %s", f),
		fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\nCREATE TABLE %s_c (p int REFERENCES %s);\nALTER TABLE %s_c %s", t, t, t, t, pick(g, []string{"DISABLE TRIGGER ALL", "DISABLE TRIGGER USER", "ENABLE TRIGGER ALL"})),
		fmt.Sprintf("CREATE ROLE %s;\nCREATE TABLE %s (a int);\nALTER TABLE %s OWNER TO %s", r, t, t, pick(g, []string{r, "CURRENT_USER", "gv_oracle_role"})),
		fmt.Sprintf("CREATE CAST (%s)", pick(g, []string{"int AS text WITH INOUT", "text AS int WITH INOUT"})),
		fmt.Sprintf("CREATE ROLE %s;\nSET ROLE %s;\nRESET ROLE", r, r),
		fmt.Sprintf("CREATE ROLE %s;\nSET ROLE %s;\nCREATE TABLE %s (a int)", r, r, t),
	}
	if g.h.fresh {
		forms = append(forms,
			pick(g, []string{"CREATE EXTENSION pg_stat_statements", "CREATE EXTENSION pgcrypto", "CREATE EXTENSION IF NOT EXISTS citext", "CREATE EXTENSION earthdistance CASCADE"}),
			fmt.Sprintf("CREATE PUBLICATION %s_p FOR ALL TABLES", f),
		)
	}
	return pick(g, forms), "O5.superuser_y51", true
}

// genY52CascadeCols uses tables, composite types, enums, domains and
// ranges as column types (directly, as arrays, through domains and
// ranges, as partition keys), renames the type, and drops it with
// CASCADE.
func genY52CascadeCols(g *gen) (string, string, bool) {
	ty, t := g.name("ct"), g.name("t")
	var def string
	switch g.r.IntN(5) {
	case 0:
		def = fmt.Sprintf("CREATE TABLE %s (b int)", ty)
	case 1:
		def = fmt.Sprintf("CREATE TYPE %s AS (x int)", ty)
	case 2:
		def = fmt.Sprintf("CREATE TYPE %s AS ENUM ('a', 'b')", ty)
	case 3:
		def = fmt.Sprintf("CREATE DOMAIN %s AS int", ty)
	default:
		def = fmt.Sprintf("CREATE TYPE %s AS RANGE (subtype = int4)", ty)
	}
	use := ty
	extra := ""
	switch g.r.IntN(5) {
	case 0:
		use = ty + "[]"
	case 1:
		d := g.name("d")
		extra = fmt.Sprintf(";\nCREATE DOMAIN %s AS %s", d, ty)
		use = d
	case 2:
		c := g.name("cc")
		extra = fmt.Sprintf(";\nCREATE TYPE %s AS (y %s, z int)", c, ty)
		use = c
	}
	sql := def + extra
	switch g.r.IntN(4) {
	case 0:
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a int, b %s)", t, use)
	case 1:
		sql += fmt.Sprintf(";\nCREATE TABLE %s (b %s)", t, use)
	case 2:
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a int, b %s);\nCREATE TABLE %s_c () INHERITS (%s)", t, use, t, t)
	default:
		sql += fmt.Sprintf(";\nCREATE TABLE %s (a %s, b int) PARTITION BY LIST (a)", t, use)
	}
	name := ty
	if g.chance(0.4) {
		name = g.name("rn")
		kind := "TYPE"
		if def[7:12] == "TABLE" {
			kind = "TABLE"
		} else if def[7:13] == "DOMAIN" {
			kind = "DOMAIN"
		}
		sql += fmt.Sprintf(";\nALTER %s %s RENAME TO %s", kind, ty, name)
	}
	kind := "TYPE"
	switch {
	case def[7:12] == "TABLE":
		kind = "TABLE"
	case def[7:13] == "DOMAIN":
		kind = "DOMAIN"
	}
	return sql + fmt.Sprintf(";\nDROP %s %s%s CASCADE", kind, pick(g, []string{"", "IF EXISTS "}), name), "M6.cascadecols_y52", true
}
