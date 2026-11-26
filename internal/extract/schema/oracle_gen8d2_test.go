package schema

// Generator g8, step D2's productions (T018a2 Revision 1.6 V1, V5, V6,
// V8), appended to productions8:
//
//   - V1: PL/pgSQL functions, procedures, trigger functions and DO blocks
//     drawn from the provable subset (declarations, assignments to
//     variables, parameters and NEW.col, RETURN per result kind, IF /
//     ELSIF / ELSE, NULL, RAISE, EXECUTE, PERFORM, embedded INSERT /
//     UPDATE / DELETE and V7 statements, handlers), each mutated at most
//     once (a dropped ;, an unterminated IF, a wrong end label, a
//     duplicate declaration, an undeclared target, NEW.x outside a
//     trigger function, a RETURN against the result, an unknown condition,
//     a bad SQLSTATE, an unknown or serial declared type, a pseudo-type
//     result or argument, a trigger function with a parameter, a
//     misspelled keyword in embedded SQL, text after END, a chained
//     comparison), bodies outside the subset (loops, CASE, GET
//     DIAGNOSTICS, nested blocks), and SET check_function_bodies = off
//     before a body and its call;
//   - V5: DO blocks, functions, triggers, rules and EXECUTE of a constant
//     writing NULLs, duplicates, unmatched keys or valid rows into tables
//     a later SET NOT NULL, ADD UNIQUE / PRIMARY KEY / FOREIGN KEY / CHECK,
//     CREATE UNIQUE INDEX or ALTER COLUMN TYPE checks;
//   - V6: CREATE SCHEMA named like the harness role, AUTHORIZATION
//     CURRENT_USER or another name (fresh databases only: schemas are
//     database-wide), then unqualified creations and references, SET
//     search_path and DROP SCHEMA;
//   - V8: tables code creates through EXECUTE of a constant (serial,
//     identity, PRIMARY KEY, UNIQUE, unnamed indexes, LIKE INCLUDING ALL),
//     then statements creating, dropping or using each generated name.

import (
	"fmt"
	"strings"
)

func init() {
	productions8 = append(productions8,
		production{12, genV1Bodies, false},
		production{5, genV5CodeRows, false},
		production{3, genV6UserSchema, false},
		production{4, genV8GeneratedNames, false},
	)
}

// v1Kind is the routine a generated body is compiled against.
type v1Kind struct {
	head    string // CREATE ... AS, or DO
	ret     string // "value", "none" or "trigger"
	call    string // a statement that calls it ("" for none)
	params  []string
	trigger bool
}

func genV1Bodies(g *gen) (string, string, bool) {
	t := g.name("t")
	setup := fmt.Sprintf("CREATE TABLE %s (a int, b text);\n", t)
	f := g.name("f")
	var k v1Kind
	switch g.r.IntN(8) {
	case 0:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE plpgsql AS", f), ret: "value", call: "SELECT " + f + "()"}
	case 1:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s(x int, y text) RETURNS text LANGUAGE plpgsql AS", f), ret: "value", call: "SELECT " + f + "(1, 'a')", params: []string{"x", "y"}}
	case 2:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s(x int) RETURNS void LANGUAGE plpgsql AS", f), ret: "none", call: "SELECT " + f + "(1)", params: []string{"x"}}
	case 3:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s(x int) RETURNS SETOF int LANGUAGE plpgsql AS", f), ret: "none", call: "SELECT * FROM " + f + "(1)", params: []string{"x"}}
	case 4:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s(x int, OUT y int) LANGUAGE plpgsql AS", f), ret: "none", call: "SELECT " + f + "(1)", params: []string{"x", "y"}}
	case 5:
		k = v1Kind{head: fmt.Sprintf("CREATE PROCEDURE %s(x int) LANGUAGE plpgsql AS", f), ret: "none", call: "CALL " + f + "(1)", params: []string{"x"}}
	case 6:
		k = v1Kind{head: fmt.Sprintf("CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS", f), ret: "trigger", trigger: true,
			call: fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s();\nINSERT INTO %s VALUES (1, 'x')", g.name("tr"), t, f, t)}
	default:
		k = v1Kind{head: "DO", ret: "none"}
	}
	var decls []string
	vars := append([]string{}, k.params...)
	for i := g.r.IntN(3); i > 0; i-- {
		v := g.name("v")
		vars = append(vars, v)
		switch g.r.IntN(5) {
		case 0:
			decls = append(decls, v+" int := 1;")
		case 1:
			decls = append(decls, v+" text DEFAULT 'a';")
		case 2:
			decls = append(decls, v+" numeric(10, 2);")
		case 3:
			decls = append(decls, v+" boolean = true;")
		default:
			decls = append(decls, v+" timestamptz;")
		}
	}
	target := func() string {
		if len(vars) == 0 {
			return ""
		}
		return pick(g, vars)
	}
	expr := func() string {
		return pick(g, []string{"1", "1 + 2 * 3", "'a' || 'b'", "now()", "current_timestamp", "coalesce(NULL, 1)", "(SELECT count(*) FROM " + t + ")",
			"1 IS NOT NULL", "NOT true", "length('abc')", "CAST(1 AS text)", "1::numeric(5, 1)", "CASE WHEN true THEN 1 ELSE 2 END", "ARRAY[1, 2]",
			"EXISTS (SELECT 1 FROM " + t + " WHERE a = 1)", "1 IN (1, 2)", "'a' LIKE 'b%'", "2 BETWEEN 1 AND 3", "-1", "E'x\\ty'", "$q$x$q$"})
	}
	cond := func() string {
		return pick(g, []string{"true", "1 = 1", "NOT FOUND", "1 IS DISTINCT FROM 2 AND 2 > 1", "(1 < 2)", "EXISTS (SELECT FROM " + t + ")", "'a' ILIKE 'A'"})
	}
	ret := func() string {
		switch k.ret {
		case "value":
			return "RETURN " + expr() + ";"
		case "trigger":
			return "RETURN " + pick(g, []string{"NEW", "NULL", "OLD"}) + ";"
		}
		return "RETURN;"
	}
	stmt := func() string {
		switch g.r.IntN(14) {
		case 0:
			if v := target(); v != "" {
				return v + pick(g, []string{" := ", " = "}) + expr() + ";"
			}
			return "NULL;"
		case 1:
			if k.trigger {
				return "NEW.a := 1;"
			}
			return "NULL;"
		case 2:
			return fmt.Sprintf("RAISE NOTICE 'v %%', %s;", expr())
		case 3:
			return "RAISE " + pick(g, []string{"DEBUG", "LOG", "INFO", "WARNING", ""}) + " 'done %%';"
		case 4:
			return "EXECUTE " + pick(g, []string{"'SELECT 1'", "format('SELECT %s', 1)", "'SELECT ' || 2"}) + ";"
		case 5:
			return "PERFORM " + pick(g, []string{"1", "count(*) FROM " + t, "a FROM " + t + " WHERE b = 'x'", "now()"}) + ";"
		case 6:
			return fmt.Sprintf("INSERT INTO %s (a, b) VALUES (%s, 'x');", t, pick(g, []string{"1", "2", "NULL"}))
		case 7:
			return fmt.Sprintf("UPDATE %s SET b = 'y' WHERE a = 1;", t)
		case 8:
			return fmt.Sprintf("DELETE FROM %s WHERE a > 5;", t)
		case 9:
			return pick(g, []string{"COMMENT ON TABLE " + t + " IS 'c';", "GRANT SELECT ON " + t + " TO PUBLIC;", "LOCK TABLE " + t + " IN SHARE MODE;", "NOTIFY ch;",
				"REVOKE ALL ON " + t + " FROM PUBLIC;", "COMMENT ON COLUMN " + t + ".a IS NULL;"})
		case 10:
			return fmt.Sprintf("IF %s THEN %s ELSIF %s THEN NULL; ELSE %s END IF;", cond(), ret(), cond(), "NULL;")
		case 11:
			return fmt.Sprintf("IF %s THEN NULL; END IF;", cond())
		case 12:
			if g.clean {
				return "NULL;"
			}
			// Outside the subset (may fail).
			return pick(g, []string{"FOR i IN 1..2 LOOP NULL; END LOOP;", "CASE WHEN true THEN NULL; END CASE;", "BEGIN NULL; END;",
				"DECLARE q int; BEGIN NULL; END;", "WHILE false LOOP NULL; END LOOP;"})
		}
		return "NULL;"
	}
	var body []string
	for i := 1 + g.r.IntN(3); i > 0; i-- {
		body = append(body, stmt())
	}
	body = append(body, ret())
	handler := ""
	switch g.r.IntN(6) {
	case 0:
		handler = " EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'e %', SQLERRM;"
	case 1:
		handler = " EXCEPTION WHEN division_by_zero OR unique_violation THEN NULL;"
	case 2:
		handler = " EXCEPTION WHEN SQLSTATE '22012' THEN NULL;"
	}
	label := ""
	if g.chance(0.15) {
		label = "<<" + g.name("l") + ">> "
	}
	endLabel := ""
	if label != "" && g.chance(0.5) {
		endLabel = " " + strings.Trim(label, "<> ")
	}
	declare := ""
	if len(decls) > 0 {
		declare = "DECLARE " + strings.Join(decls, " ") + " "
	}
	text := label + declare + "BEGIN " + strings.Join(body, " ") + handler + " END" + endLabel + pick(g, []string{"", ";"})
	head := k.head
	if !g.clean && g.chance(0.6) {
		// One mutation from the refusal list.
		switch g.r.IntN(16) {
		case 0:
			text = strings.Replace(text, ";", "", 1)
		case 1:
			text = strings.Replace(text, "END IF;", "END;", 1)
		case 2:
			text = text + " " + g.name("wrong")
		case 3:
			if len(decls) > 0 {
				text = strings.Replace(text, "DECLARE ", "DECLARE "+decls[0]+" ", 1)
			}
		case 4:
			text = strings.Replace(text, "BEGIN ", "BEGIN "+g.name("undeclared")+" := 1; ", 1)
		case 5:
			text = strings.Replace(text, "BEGIN ", "BEGIN NEW.a := 1; ", 1)
		case 6:
			text = strings.Replace(text, "BEGIN ", "BEGIN RETURN 1; ", 1)
		case 7:
			text = strings.Replace(text, "BEGIN ", "BEGIN RETURN; ", 1)
		case 8:
			text = strings.Replace(text, " END", " EXCEPTION WHEN no_such_condition THEN NULL; END", 1)
		case 9:
			text = strings.Replace(text, " END", " EXCEPTION WHEN SQLSTATE '2201' THEN NULL; END", 1)
		case 10:
			text = strings.Replace(text, "BEGIN ", "DECLARE "+g.name("d")+" "+pick(g, []string{"serial", "nosuchtype", "record", "int[]", "varchar(0)"})+"; BEGIN ", 1)
		case 11:
			if strings.HasPrefix(head, "CREATE FUNCTION") {
				head = strings.Replace(head, "RETURNS int", "RETURNS "+pick(g, []string{"cstring", "internal", "anyelement", "record", "event_trigger"}), 1)
			}
		case 12:
			if k.trigger {
				head = strings.Replace(head, "()", "(z int)", 1)
			}
		case 13:
			text = strings.Replace(text, "INSERT INTO", "INSRT INTO", 1)
		case 14:
			text = strings.Replace(text, "BEGIN ", "BEGIN PERFORM 1 = 1 = 1; ", 1)
		default:
			text = strings.Replace(text, "RAISE NOTICE 'v %'", "RAISE NOTICE 'v % %'", 1)
		}
	}
	sql := setup
	if g.chance(0.2) {
		sql += pick(g, []string{"SET check_function_bodies = off;\n", "SET LOCAL check_function_bodies TO false;\n", "SET check_function_bodies = on;\n"})
	}
	sql += head + " $$ " + text + " $$"
	if k.call != "" && g.chance(0.6) {
		sql += ";\n" + k.call
	}
	return sql, "R6.compile_v1", true
}

func genV5CodeRows(g *gen) (string, string, bool) {
	p, t := g.name("p"), g.name("t")
	setup := fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY);\nINSERT INTO %s VALUES (1);\nCREATE TABLE %s (a int);\n", p, p, t)
	rows := pick(g, []string{"(NULL)", "(1), (1)", "(5)", "(1)", "(-1)"})
	var writer string
	switch g.r.IntN(6) {
	case 0:
		writer = fmt.Sprintf("DO $$ BEGIN INSERT INTO %s VALUES %s; END $$;\n", t, rows)
	case 1:
		writer = fmt.Sprintf("DO $$ BEGIN EXECUTE $q$INSERT INTO %s VALUES %s$q$; END $$;\n", t, rows)
	case 2:
		f := g.name("f")
		writer = fmt.Sprintf("CREATE FUNCTION %s() RETURNS void LANGUAGE plpgsql AS $$ BEGIN INSERT INTO %s VALUES %s; END $$;\nSELECT %s();\n", f, t, rows, f)
	case 3:
		f, l := g.name("f"), g.name("l")
		writer = fmt.Sprintf("CREATE TABLE %s (x int);\nCREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO %s VALUES %s; RETURN NEW; END $$;\nCREATE TRIGGER %s AFTER INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();\nINSERT INTO %s VALUES (1);\n", l, f, t, rows, g.name("tr"), l, f, l)
	case 4:
		l := g.name("l")
		writer = fmt.Sprintf("CREATE TABLE %s (x int);\nCREATE RULE %s AS ON INSERT TO %s DO ALSO INSERT INTO %s VALUES %s;\nINSERT INTO %s VALUES (1);\n", l, g.name("r"), l, t, rows, l)
	default:
		u := g.name("u")
		writer = fmt.Sprintf("CREATE TABLE %s (a int);\nDO $$ BEGIN INSERT INTO %s VALUES %s; END $$;\n", u, u, rows)
	}
	check := pick(g, []string{
		"ALTER TABLE %[1]s ALTER COLUMN a SET NOT NULL",
		"ALTER TABLE %[1]s ADD UNIQUE (a)",
		"ALTER TABLE %[1]s ADD PRIMARY KEY (a)",
		"ALTER TABLE %[1]s ADD FOREIGN KEY (a) REFERENCES %[2]s",
		"ALTER TABLE %[1]s ADD CHECK (a > 0)",
		"CREATE UNIQUE INDEX " + g.name("ix") + " ON %[1]s (a)",
		"ALTER TABLE %[1]s ALTER COLUMN a TYPE smallint",
		"ALTER TABLE %[1]s ADD COLUMN c int NOT NULL",
	})
	return setup + writer + fmt.Sprintf(check, t, p), "M5d.code_rows_v5", true
}

func genV6UserSchema(g *gen) (string, string, bool) {
	if !g.h.fresh {
		// Schemas are database-wide: only a fresh database gives each
		// history its own.
		return "", "", false
	}
	s := pick(g, []string{"graphvein", "graphvein", g.name("s")})
	create := "CREATE SCHEMA " + s
	if g.chance(0.25) {
		create, s = "CREATE SCHEMA AUTHORIZATION CURRENT_USER", "graphvein"
	}
	t := g.name("t")
	sql := create + ";\n"
	switch g.r.IntN(6) {
	case 0:
		sql += fmt.Sprintf("CREATE TABLE %s (a int)", t)
	case 1:
		sql += fmt.Sprintf("CREATE TABLE public.%s (a int);\nCREATE TABLE %s (b int)", t, g.name("u"))
	case 2:
		sql += fmt.Sprintf("CREATE TABLE public.%s (a int);\nALTER TABLE %s ADD COLUMN b int", t, t)
	case 3:
		sql += fmt.Sprintf("SET search_path TO public;\nCREATE TABLE %s (a int)", t)
	case 4:
		sql += fmt.Sprintf("DROP SCHEMA %s;\nCREATE TABLE %s (a int)", s, t)
	default:
		sql += fmt.Sprintf("CREATE TABLE %s.%s (a int);\nINSERT INTO %s VALUES (1)", s, t, t)
	}
	return sql, "O3.user_schema_v6", true
}

func genV8GeneratedNames(g *gen) (string, string, bool) {
	x := g.name("x")
	var def string
	switch g.r.IntN(6) {
	case 0:
		def = "id serial"
	case 1:
		def = "id int GENERATED ALWAYS AS IDENTITY"
	case 2:
		def = "id int PRIMARY KEY"
	case 3:
		def = "id bigserial, e text UNIQUE"
	case 4:
		def = "id int, e int, EXCLUDE USING btree (e WITH =)"
	default:
		def = "LIKE ext_a INCLUDING ALL"
	}
	sql := fmt.Sprintf("DO $$ BEGIN EXECUTE 'CREATE TABLE %s (%s)'; END $$;\n", x, def)
	if g.chance(0.3) {
		sql += fmt.Sprintf("DO $$ BEGIN EXECUTE 'CREATE INDEX ON %s (id)'; END $$;\n", x)
	}
	name := pick(g, []string{x + "_id_seq", x + "_pkey", x + "_e_key", x + "_e_excl", x + "_id_idx", x + "_code_key", x + "_pkey1", "_" + x, x + "_other"})
	switch g.r.IntN(6) {
	case 0:
		sql += "CREATE SEQUENCE " + name
	case 1:
		sql += "DROP SEQUENCE " + pick(g, []string{"", "IF EXISTS "}) + name
	case 2:
		sql += fmt.Sprintf("CREATE TABLE %s (a int)", name)
	case 3:
		sql += fmt.Sprintf("CREATE INDEX %s ON ext_b (id)", name)
	case 4:
		sql += fmt.Sprintf("CREATE TYPE %s AS ENUM ('a')", name)
	default:
		sql += fmt.Sprintf("CREATE TABLE %s (a int)", g.name("y"))
	}
	return sql, "R1.generated_names_v8", true
}
