package schema

// Generator g7 (T018a2 Revision 1.5): g6's grammar plus productions7,
// valid and refused variants of the Revision 1.5 class rules, so that the
// oracle pins them. Step C1 adds U1 and U7-U10; steps C2 (U3-U6) and C3
// (U2) append their productions to productions7. Every production's tag
// starts with the contract row it exercises and ends with its rule.
//
//   - U1: a unique index, then ADD PRIMARY KEY / UNIQUE USING INDEX named
//     as the index, renamed or unnamed, over refused index shapes, other
//     tables, missing names, nullable columns with and without rows, a
//     second primary key, a partitioned table, taken names, later DROP and
//     ALTER INDEX of the old and new names, and beside other subcommands;
//   - U7: column types of pg_catalog, set-created, contrib and unknown
//     names; indexes and keys over types with and without a default class
//     per access method; COLLATE; SET / RESET of known, unknown,
//     placeholder and reserved-prefix names, contexts and values;
//   - U8: reserved, type_func_name, col_name and unreserved keywords in
//     each name position, quoted and after a dot;
//   - U9: SET / RESET / WITH storage parameters on tables, partitioned
//     tables and indexes of each method;
//   - U10: system column names in CREATE TABLE, ADD COLUMN [IF NOT
//     EXISTS], RENAME COLUMN, a view and a composite type, and oid;
//   - U3: SET, SET LOCAL, RESET, SET SCHEMA, set_config and
//     pg_dump's empty path, with a created schema, a missing one,
//     "$user", pg_catalog and public, then unqualified CREATE and
//     references, restored or not before the end of the migration (a
//     focused g7 history runs in a fresh database a quarter of the time,
//     so that it can create schemas);
//   - U4: RANGE, LIST and HASH partitioned tables, then keys,
//     EXCLUDE, ADD COLUMN ... UNIQUE / PRIMARY KEY and unique indexes
//     with and without the partition key, INCLUDE, hash, COLLATE, and
//     sub-partitioned PARTITION OF children;
//   - U5: CREATE and ALTER SEQUENCE with options inside and
//     outside their bounds, AS each integer type and others, repeated
//     options, OWNED BY held, missing and other-schema columns and NONE,
//     and ALTER SEQUENCE of serial and identity sequences;
//   - U6: CREATE OR REPLACE FUNCTION / PROCEDURE over held
//     routines with equal and changed return types, OUT parameters,
//     RETURNS TABLE, parameter names and defaults, the other kind, and
//     overloads;
//   - U2: DO blocks, SELECT and PERFORM of functions, CALL of
//     procedures, LANGUAGE sql bodies and trigger functions fired by an
//     INSERT, whose bodies create (tables, indexes, enums, sequences,
//     views, functions, triggers, constraints), alter or drop names, plain,
//     guarded, in an exception block, as a constant EXECUTE or a format()
//     EXECUTE; then later statements that create, use or drop those names
//     and the tables the code names, or touch nothing the code did.

import (
	"fmt"
	"strings"
)

var productions7 = []production{
	{8, genU1UsingIndex, false},
	{6, genU7Types, false},
	{4, genU7Settings, false},
	{6, genU8Keywords, false},
	{4, genU9Storage, false},
	{3, genU10SystemColumns, false},
	{6, genU3SearchPath, false},
	{5, genU4Partitioned, false},
	{6, genU5Sequences, false},
	{6, genU6Routines, false},
	{10, genU2Code, false},
}

func genU1UsingIndex(g *gen) (string, string, bool) {
	t := g.newTable3(&gCol{name: "a", typ: "int", notNull: true}, &gCol{name: "b", typ: "int"})
	ix := g.name("ix")
	create := fmt.Sprintf("CREATE TABLE %s (a int NOT NULL, b int)", t.name)
	if g.chance(0.15) {
		create = fmt.Sprintf("CREATE TABLE %s (a int NOT NULL, b int) PARTITION BY RANGE (a)", t.name)
	}
	index := pick(g, []string{
		"CREATE UNIQUE INDEX %s ON %s (a)", "CREATE UNIQUE INDEX %s ON %s (b)", "CREATE UNIQUE INDEX %s ON %s (a, b)",
		"CREATE INDEX %s ON %s (a)", "CREATE UNIQUE INDEX %s ON %s (a) WHERE a > 0", "CREATE UNIQUE INDEX %s ON %s ((a + 1))",
		"CREATE UNIQUE INDEX %s ON %s (a DESC)", "CREATE UNIQUE INDEX %s ON %s (a NULLS FIRST)", "CREATE UNIQUE INDEX %s ON %s (a int4_ops)",
		"CREATE INDEX %s ON %s USING hash (a)", "CREATE UNIQUE INDEX %s ON %s (a) NULLS NOT DISTINCT", "CREATE UNIQUE INDEX %s ON %s (a) INCLUDE (b)",
	})
	stmts := []string{create, fmt.Sprintf(index, ix, t.name)}
	if g.chance(0.2) {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO %s (a, b) VALUES (1, NULL)", t.name))
	}
	if g.chance(0.15) {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (b)", t.name))
	}
	target, name := ix, ""
	switch g.r.IntN(6) {
	case 0:
		name = "CONSTRAINT " + ix + " "
	case 1, 2:
		name = "CONSTRAINT " + g.name("k") + " "
	case 3:
		target = pick(g, []string{"nosuch_index", g.name("ix")})
	case 4:
		if g.chance(0.5) {
			// The new name taken in pg_class or among the constraints.
			taken := g.name("tk")
			stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s (x int)", taken))
			name = "CONSTRAINT " + taken + " "
		} else {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT ck1 CHECK (a > 0)", t.name))
			name = "CONSTRAINT ck1 "
		}
	}
	kind := pick(g, []string{"PRIMARY KEY", "UNIQUE"})
	tail := pick(g, []string{"", "", "", " DEFERRABLE", " NOT DEFERRABLE", ", ADD COLUMN c int"})
	stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD %s%s USING INDEX %s%s", t.name, name, kind, target, tail))
	if kind == "PRIMARY KEY" {
		t.pk = []string{"a"}
	}
	newName := strings.TrimPrefix(strings.TrimSpace(name), "CONSTRAINT ")
	if newName == "" {
		newName = target
	}
	switch g.r.IntN(4) {
	case 0:
		stmts = append(stmts, "DROP INDEX "+pick(g, []string{ix, newName}))
	case 1:
		stmts = append(stmts, fmt.Sprintf("ALTER INDEX %s RENAME TO %s", pick(g, []string{ix, newName}), g.name("rn")))
	}
	return strings.Join(stmts, ";\n"), "M5f-u.using_index_u1", true
}

// u7Types are column types: pg_catalog, contrib, unknown, arrays and the
// set's enum.
var u7Types = []string{"int", "text", "json", "jsonb", "xml", "point", "tsvector", "int4range", "uuid", "box",
	"int[]", "json[]", "point[]", "datetime", "nosuch_type", "citext", "pg_catalog.int4", "public.int4", "\"Int4\"", "money", "serial", "bigserial"}

func genU7Types(g *gen) (string, string, bool) {
	typ := pick(g, u7Types)
	t := g.newTable3(&gCol{name: "a", typ: typ})
	var pre []string
	if typ == "citext" {
		pre = append(pre, "CREATE EXTENSION IF NOT EXISTS citext")
	}
	if g.chance(0.2) {
		e := g.name("en")
		pre = append(pre, fmt.Sprintf("CREATE TYPE %s AS ENUM ('x', 'y')", e))
		typ = e
	}
	if g.chance(0.1) {
		c := g.name("ct")
		pre = append(pre, fmt.Sprintf("CREATE TYPE %s AS (x int, y text)", c))
		typ = c
	}
	collate := ""
	if g.chance(0.2) && (typ == "text" || typ == "citext") {
		collate = " COLLATE " + pick(g, []string{"\"C\"", "\"POSIX\"", "\"default\"", "ucs_basic", "\"en_US\"", "\"xx_YY\"", "pg_c_utf8"})
	}
	method := pick(g, []string{"", "", " USING btree", " USING hash", " USING gist", " USING gin", " USING brin", " USING spgist"})
	var body string
	tag := "M1.type_u7"
	switch g.r.IntN(6) {
	case 0:
		body = fmt.Sprintf("CREATE TABLE %s (a %s%s)", t.name, typ, collate)
	case 1:
		body = fmt.Sprintf("CREATE TABLE %s (a %s%s);\nCREATE INDEX %s ON %s%s (a)", t.name, typ, collate, g.name("ix"), t.name, method)
		tag = "M4.opclass_u7"
	case 2:
		body = fmt.Sprintf("CREATE TABLE %s (a %s%s %s)", t.name, typ, collate, pick(g, []string{"PRIMARY KEY", "UNIQUE"}))
		tag = "M1.key_opclass_u7"
	case 3:
		body = fmt.Sprintf("CREATE TABLE %s (a %s);\nALTER TABLE %s ADD CONSTRAINT %s %s (a)", t.name, typ, t.name, g.name("k"), pick(g, []string{"PRIMARY KEY", "UNIQUE"}))
		tag = "M5f.key_opclass_u7"
	case 4:
		body = fmt.Sprintf("CREATE TABLE %s (x int);\nALTER TABLE %s ADD COLUMN a %s%s", t.name, t.name, typ, collate)
		tag = "M5a.type_u7"
	default:
		body = fmt.Sprintf("CREATE TABLE %s (a text);\nALTER TABLE %s ALTER COLUMN a TYPE %s USING NULL", t.name, t.name, typ)
		tag = "M5c.type_u7"
	}
	return strings.Join(append(pre, body), ";\n"), tag, true
}

func genU7Settings(g *gen) (string, string, bool) {
	scope := pick(g, []string{"", "SESSION ", "LOCAL "})
	set := pick(g, []string{
		"work_mem = 4096", "work_mem = '64MB'", "work_mem = 'abc'", "work_mem = 1", "work_mem = 2147483647", "statement_timeout = 0",
		"lock_timeout = '1s'", "client_min_messages = warning", "client_min_messages = loud", "enable_seqscan = off", "enable_seqscan = maybe",
		"enable_seqscan TO 1", "timezone = 'UTC'", "timezone = 'Europe/Paris'", "TIME ZONE 'UTC'", "TIME ZONE LOCAL", "no_such_setting = 1",
		"myapp.flag = 'x'", "plpgsql.extra_warnings = 'none'", "pg_trgm.similarity_threshold = 0.5", "a.b.c = 1", "shared_buffers = 1000",
		"wal_level = minimal", "log_statement = 'all'", "default_with_oids = true", "application_name = 'x'", "random_page_cost = 1.5",
		"random_page_cost = -1", "work_mem TO DEFAULT", "seq_page_cost = 'x'",
	})
	var stmt string
	if g.chance(0.2) {
		stmt = "RESET " + pick(g, []string{"work_mem", "no_such_setting", "shared_buffers", "myapp.flag", "transaction_isolation", "timezone"})
	} else {
		stmt = "SET " + scope + set
	}
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	return fmt.Sprintf("%s;\nCREATE TABLE %s (a int)", stmt, t.name), "N2.setting_u7", true
}

// u8Words are keywords of each category (kwlist.h): reserved,
// type_func_name, col_name, unreserved.
var u8Words = []string{"order", "user", "select", "natural", "inner", "similar", "verbose", "concurrently", "between", "values", "coalesce", "int", "name", "data", "action", "comment"}

func genU8Keywords(g *gen) (string, string, bool) {
	w := pick(g, u8Words)
	if g.chance(0.2) {
		w = "\"" + w + "\""
	}
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	switch g.r.IntN(8) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int, %s int)", t.name, w), "M1.keyword_column_u8", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (a int)", w), "M1.keyword_table_u8", true
	case 2:
		if g.h.fresh {
			// public is shared by the histories of one database; only a
			// fresh database's is the history's own.
			return fmt.Sprintf("CREATE TABLE public.%s (a int)", w), "M1.keyword_after_dot_u8", true
		}
		return fmt.Sprintf("CREATE TABLE %s (a int, b %s.int4)", t.name, pick(g, []string{"pg_catalog", "public"})), "M1.keyword_after_dot_u8", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE INDEX %s ON %s (a)", t.name, w, t.name), "M4.keyword_index_u8", true
	case 4:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s RENAME COLUMN a TO %s", t.name, t.name, w), "M5h.keyword_rename_u8", true
	case 5:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ADD COLUMN %s text", t.name, t.name, w), "M5a.keyword_column_u8", true
	case 6:
		return fmt.Sprintf("CREATE TYPE %s AS ENUM ('a');\nCREATE TABLE %s (a int)", w, t.name), "R2.keyword_type_u8", true
	}
	return fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE TABLE %s (a int)", w, t.name), "R6.keyword_function_u8", true
}

func genU9Storage(g *gen) (string, string, bool) {
	opts := pick(g, []string{"fillfactor = 50", "fillfactor = 5", "fillfactor = 101", "fillfactor = 50.5", "fillfactor = 'x'", "fillfactor",
		"no_such_option = 1", "autovacuum_enabled = off", "autovacuum_enabled = maybe", "fillfactor = 70, fillfactor = 80",
		"toast.autovacuum_enabled = false", "parallel_workers = 4", "vacuum_index_cleanup = auto", "vacuum_index_cleanup = sometimes",
		"pages_per_range = 4", "deduplicate_items = off", "buffering = auto", "fastupdate = on", "gin_pending_list_limit = 64"})
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	partitioned := ""
	if g.chance(0.2) {
		partitioned = " PARTITION BY RANGE (a)"
	}
	switch g.r.IntN(5) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int)%s;\nALTER TABLE %s SET (%s)", t.name, partitioned, t.name, opts), "M5k-a.set_storage_u9", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (a int)%s;\nALTER TABLE %s RESET (%s)", t.name, partitioned, t.name, pick(g, []string{"fillfactor", "no_such", "fillfactor = 5"})), "M5k-a.reset_storage_u9", true
	case 2:
		return fmt.Sprintf("CREATE TABLE %s (a int)%s WITH (%s)", t.name, partitioned, opts), "M1.with_storage_u9", true
	case 3:
		method := pick(g, []string{"", " USING btree", " USING hash", " USING gist", " USING gin", " USING brin"})
		typ := "int"
		if strings.Contains(method, "gin") || strings.Contains(method, "gist") {
			typ = "int4range"
			if strings.Contains(method, "gin") {
				typ = "int[]"
			}
		}
		return fmt.Sprintf("CREATE TABLE %s (a %s);\nCREATE INDEX %s ON %s%s (a) WITH (%s)", t.name, typ, g.name("ix"), t.name, method, opts), "M4.with_storage_u9", true
	}
	ix := g.name("ix")
	return fmt.Sprintf("CREATE TABLE %s (a int);\nCREATE INDEX %s ON %s (a);\nALTER INDEX %s SET (%s)", t.name, ix, t.name, ix, opts), "M8.set_storage_u9", true
}

func genU10SystemColumns(g *gen) (string, string, bool) {
	c := pick(g, []string{"ctid", "xmin", "cmin", "xmax", "cmax", "tableoid", "oid", "\"xmin\"", "xmin2"})
	t := g.newTable3(&gCol{name: "a", typ: "int"})
	switch g.r.IntN(6) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (a int, %s int)", t.name, c), "M1.system_column_u10", true
	case 1:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ADD COLUMN %s int", t.name, t.name, c), "M5a.system_column_u10", true
	case 2:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s ADD COLUMN IF NOT EXISTS %s int", t.name, t.name, c), "M5a.system_column_u10", true
	case 3:
		return fmt.Sprintf("CREATE TABLE %s (a int);\nALTER TABLE %s RENAME COLUMN a TO %s", t.name, t.name, c), "M5h.system_column_u10", true
	case 4:
		return fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS %s;\nCREATE TABLE %s (a int)", g.name("v"), c, t.name), "R1.system_column_u10", true
	}
	return fmt.Sprintf("CREATE TYPE %s AS (%s int);\nCREATE TABLE %s (a int)", g.name("ct"), c, t.name), "R1.system_column_u10", true
}

func genU3SearchPath(g *gen) (string, string, bool) {
	var stmts []string
	target := pick(g, []string{"nosuch_sp", "pg_catalog", "public", "\"$user\""})
	if g.h.fresh && g.chance(0.8) {
		target = g.name("sp")
		stmts = append(stmts, "CREATE SCHEMA "+target)
	}
	quoted := strings.Trim(target, `"`)
	switch r := g.r.IntN(12); {
	case r == 0:
		stmts = append(stmts, "SELECT pg_catalog.set_config('search_path', '', false)")
	case r == 1:
		stmts = append(stmts, pick(g, []string{"SET search_path TO DEFAULT", "RESET search_path", "SET search_path TO ''", "RESET ALL", "SET LOCAL search_path TO DEFAULT"}))
	case r == 2:
		stmts = append(stmts, fmt.Sprintf(pick(g, []string{"SET SCHEMA '%s'", "SELECT pg_catalog.set_config('search_path', '%s', false)", "SELECT set_config('search_path', '%s', true)", "SELECT set_config('search_path', '%s, public', false)"}), quoted))
	default:
		stmts = append(stmts, fmt.Sprintf(pick(g, []string{"SET search_path TO %s", "SET search_path = %s, public", "SET SESSION search_path TO %s",
			"SET LOCAL search_path TO %s", "SET search_path TO nosuch_sp, %s", "SET search_path TO \"$user\", %s", "SET search_path TO pg_catalog, %s", "SET search_path TO %s, nosuch_sp"}), target))
	}
	t := g.name("pt")
	stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s (a int)", t))
	switch g.r.IntN(8) {
	case 0:
		stmts = append(stmts, fmt.Sprintf("INSERT INTO %s (a) VALUES (1)", t))
	case 1:
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN b int", t))
	case 2:
		stmts = append(stmts, fmt.Sprintf("CREATE INDEX %s ON %s (a)", g.name("pix"), t))
	}
	// Restored before the end of the migration (or not, so that the next
	// migration runs under the changed path).
	switch g.r.IntN(7) {
	case 0, 1, 2:
		stmts = append(stmts, "RESET search_path")
	case 3:
		stmts = append(stmts, "SET LOCAL search_path TO DEFAULT")
	case 4:
		stmts = append(stmts, "SET search_path TO DEFAULT")
	}
	return strings.Join(stmts, ";\n"), "O3.search_path_u3", true
}

func genU4Partitioned(g *gen) (string, string, bool) {
	t := g.name("pp")
	strategy := pick(g, []string{"RANGE", "LIST", "HASH", "RANGE", "LIST"})
	keyCol := pick(g, []string{"k", "k", "c", "k, id"})
	if strategy == "HASH" && keyCol == "c" {
		keyCol = "k"
	}
	parentKey := ""
	if g.chance(0.3) {
		parentKey = pick(g, []string{", PRIMARY KEY (id, k)", ", UNIQUE (k, id)", ", PRIMARY KEY (id)"})
	}
	stmts := []string{fmt.Sprintf("CREATE TABLE %s (id int, k int, j int, c text%s) PARTITION BY %s (%s)", t, parentKey, strategy, keyCol)}
	cols := pick(g, []string{"id", "id, k", "k", "k, id", "j", "id, k, j", "c", "c, id"})
	switch g.r.IntN(8) {
	case 0:
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", t, cols))
	case 1:
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD %sUNIQUE (%s)%s", t, pick(g, []string{"", "CONSTRAINT " + g.name("pu") + " "}), cols, pick(g, []string{"", " INCLUDE (k)", " INCLUDE (j)"})))
	case 2:
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s EXCLUDE USING btree (%s WITH =)", t, g.name("px"), pick(g, []string{"k", "id"})))
	case 3:
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s int %s", t, g.name("u"), pick(g, []string{"UNIQUE", "PRIMARY KEY"})))
	case 4, 5:
		method := pick(g, []string{"", "", " USING hash", " USING btree"})
		if method == " USING hash" {
			cols = pick(g, []string{"k", "id"})
		}
		el := cols
		if g.chance(0.2) {
			el = pick(g, []string{"k DESC", "c COLLATE \"C\"", "k int4_ops", "(k + 1)", "k NULLS FIRST"})
		}
		stmts = append(stmts, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s%s%s (%s)", g.name("pi"), pick(g, []string{"", "ONLY "}), t, method, el))
	default:
		bound := map[string]string{"RANGE": "FROM (1) TO (10)", "LIST": "IN (1)", "HASH": "WITH (MODULUS 2, REMAINDER 0)"}[strategy]
		if keyCol == "c" && strategy == "LIST" {
			bound = "IN ('a')"
		}
		if keyCol == "c" && strategy == "RANGE" {
			bound = "FROM ('a') TO ('b')"
		}
		if keyCol == "k, id" {
			bound = map[string]string{"RANGE": "FROM (1, 1) TO (10, 10)", "LIST": "DEFAULT", "HASH": bound}[strategy]
			if strategy == "LIST" {
				return "", "", false
			}
		}
		child := g.name("pc")
		stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s PARTITION OF %s FOR VALUES %s PARTITION BY %s (%s)", child, t, bound, pick(g, []string{"LIST", "RANGE", "HASH"}), pick(g, []string{"j", "id", "k"})))
		if g.chance(0.5) {
			stmts = append(stmts, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", g.name("pi"), t, pick(g, []string{"id, k", "id, k, j", "k, j"})))
		}
	}
	return strings.Join(stmts, ";\n"), "M5f.partitioned_unique_u4", true
}

// u5Options are sequence options inside and outside their bounds;
// u5Valid are options a default ascending sequence of any integer type
// accepts.
var u5Options = []string{"AS smallint", "AS integer", "AS bigint", "AS int8", "AS text", "AS numeric", "INCREMENT BY 0", "INCREMENT 2",
	"INCREMENT -1", "MINVALUE 10", "MINVALUE -5", "NO MINVALUE", "MAXVALUE 5", "MAXVALUE 100", "MAXVALUE 40000", "MAXVALUE 3000000000",
	"NO MAXVALUE", "START 0", "START WITH 7", "START 1000000", "RESTART", "RESTART 3", "RESTART WITH 200", "CACHE 0", "CACHE 10",
	"CYCLE", "NO CYCLE", "START 1.5", "MAXVALUE 99999999999999999999", "START -1"}

var u5Valid = []string{"AS smallint", "AS integer", "AS bigint", "INCREMENT 2", "MAXVALUE 100", "NO MAXVALUE", "NO MINVALUE",
	"START WITH 7", "RESTART", "RESTART 3", "CACHE 10", "CYCLE", "NO CYCLE"}

func genU5Sequences(g *gen) (string, string, bool) {
	opts := func() string {
		var out []string
		list := u5Valid
		if g.chance(0.4) {
			list = u5Options
		}
		for n := g.r.IntN(3); n > 0; n-- {
			out = append(out, pick(g, list))
		}
		if g.chance(0.05) {
			out = append(out, "START 1 START 2")
		}
		return strings.Join(out, " ")
	}
	t := g.name("st")
	var stmts []string
	seq := g.name("sq")
	switch g.r.IntN(3) {
	case 0:
		stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s (id %s, a int)", t, pick(g, []string{"serial", "bigserial", "smallserial"})))
		seq = t + "_id_seq"
	case 1:
		stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s (id %s GENERATED ALWAYS AS IDENTITY, a int)", t, pick(g, []string{"int", "bigint", "smallint"})))
		seq = t + "_id_seq"
	default:
		stmts = append(stmts, fmt.Sprintf("CREATE TABLE %s (id int, a int)", t))
		stmts = append(stmts, strings.TrimSpace("CREATE SEQUENCE "+seq+" "+opts()))
	}
	if g.chance(0.2) {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO %s (a) VALUES (1)", t))
	}
	owned := pick(g, []string{"", "", " OWNED BY " + t + ".a", " OWNED BY " + t + ".zz", " OWNED BY NONE", " OWNED BY nosuch_t.a"})
	if alter := opts() + owned; alter != "" {
		stmts = append(stmts, "ALTER SEQUENCE "+seq+" "+strings.TrimSpace(alter))
	}
	if len(stmts) == 1 {
		return "", "", false
	}
	return strings.Join(stmts, ";\n"), "R1.sequence_options_u5", true
}

// u6Routines are definitions of the routine %s.
var u6Routines = []string{
	"FUNCTION %s() RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s() RETURNS integer LANGUAGE plpgsql AS $$BEGIN RETURN 2; END$$",
	"FUNCTION %s() RETURNS text LANGUAGE plpgsql AS $$BEGIN RETURN 'x'; END$$",
	"FUNCTION %s() RETURNS SETOF int LANGUAGE plpgsql AS $$BEGIN RETURN; END$$",
	"FUNCTION %s(a int) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(b int) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(int) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(a text) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(a int DEFAULT 1) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(a int, b int DEFAULT 2) RETURNS int LANGUAGE plpgsql AS $$BEGIN RETURN 1; END$$",
	"FUNCTION %s(OUT a int, OUT b int) LANGUAGE plpgsql AS $$BEGIN END$$",
	"FUNCTION %s(OUT a int, OUT c int) LANGUAGE plpgsql AS $$BEGIN END$$",
	"FUNCTION %s(INOUT a int) LANGUAGE plpgsql AS $$BEGIN END$$",
	"FUNCTION %s() RETURNS TABLE (a int) LANGUAGE plpgsql AS $$BEGIN RETURN; END$$",
	"FUNCTION %s() RETURNS TABLE (b int) LANGUAGE plpgsql AS $$BEGIN RETURN; END$$",
	"FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$",
	"PROCEDURE %s() LANGUAGE plpgsql AS $$BEGIN END$$",
	"PROCEDURE %s(a int) LANGUAGE plpgsql AS $$BEGIN END$$",
	"PROCEDURE %s(a int, OUT b int) LANGUAGE plpgsql AS $$BEGIN END$$",
}

func genU6Routines(g *gen) (string, string, bool) {
	f := g.name("rf")
	first := fmt.Sprintf(pick(g, u6Routines), f)
	stmts := []string{pick(g, []string{"CREATE ", "CREATE OR REPLACE "}) + first}
	if g.chance(0.1) {
		stmts = append(stmts, "DROP "+strings.SplitN(first, " ", 2)[0]+" "+f)
	}
	stmts = append(stmts, "CREATE OR REPLACE "+fmt.Sprintf(pick(g, u6Routines), f))
	return strings.Join(stmts, ";\n"), "R6.replace_routine_u6", true
}

// u2Bodies are the DDL a U2 body runs: %[1]s is a table held before the
// code, %[2]s a name the code may create.
var u2Bodies = []string{
	"CREATE TABLE %[2]s (id int)",
	"CREATE TABLE IF NOT EXISTS %[2]s (id int)",
	"CREATE INDEX %[2]s ON %[1]s (a)",
	"CREATE TYPE %[2]s AS ENUM ('a')",
	"CREATE SEQUENCE %[2]s",
	"CREATE VIEW %[2]s AS SELECT a FROM %[1]s",
	"CREATE TABLE %[2]s (pid int REFERENCES %[1]s (id))",
	"ALTER TABLE %[1]s ADD COLUMN %[2]s int",
	"ALTER TABLE %[1]s ADD CONSTRAINT %[2]s UNIQUE (a)",
	"ALTER TABLE %[1]s RENAME COLUMN a TO %[2]s",
	"DROP TABLE %[1]s",
	"ALTER TABLE %[1]s RENAME TO %[2]s",
	"DROP INDEX %[1]s_pkey",
	"CREATE FUNCTION %[2]s() RETURNS int LANGUAGE sql AS 'SELECT 1'",
	"CREATE TRIGGER %[2]s BEFORE UPDATE ON %[1]s FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger()",
	"COMMENT ON TABLE %[1]s IS 'c'",
	"GRANT SELECT ON %[1]s TO PUBLIC",
}

// u2Later are the statements after the code: %[1]s and %[2]s as in
// u2Bodies, %[3]s a name no code touches.
var u2Later = []string{
	"CREATE TABLE %[2]s (id int)",
	"CREATE INDEX %[2]s ON %[1]s (a)",
	"CREATE TYPE %[2]s AS ENUM ('a')",
	"CREATE SEQUENCE %[2]s",
	"DROP TABLE %[1]s",
	"DROP INDEX %[1]s_pkey",
	"ALTER TABLE %[1]s DROP COLUMN a",
	"ALTER TABLE %[1]s ALTER COLUMN a TYPE bigint",
	"ALTER TABLE %[1]s ADD COLUMN %[2]s int",
	"INSERT INTO %[1]s (id, a) VALUES (1, 1)",
	"CREATE TABLE %[3]s (id int)",
	"CREATE TABLE %[3]s (id int)",
}

func genU2Code(g *gen) (string, string, bool) {
	t, x, fresh := g.name("ct"), g.name("cx"), g.name("cf")
	stmts := []string{fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY, a int)", t)}
	ddl := fmt.Sprintf(pick(g, u2Bodies), t, x)
	body := ddl
	switch g.r.IntN(7) {
	case 0:
		body = fmt.Sprintf("IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = '%s') THEN %s; END IF", x, ddl)
	case 1:
		body = "EXECUTE '" + strings.ReplaceAll(ddl, "'", "''") + "'"
	case 2:
		body = "EXECUTE format('" + strings.ReplaceAll(ddl, "'", "''") + "')"
	case 3:
		body = ddl + "; EXCEPTION WHEN others THEN NULL"
	}
	routine := g.name("cr")
	switch g.r.IntN(6) {
	case 0, 1:
		stmts = append(stmts, "DO $$ BEGIN "+body+"; END $$")
	case 2:
		stmts = append(stmts, fmt.Sprintf("CREATE FUNCTION %s() RETURNS void LANGUAGE plpgsql AS $$ BEGIN %s; END $$", routine, body))
		stmts = append(stmts, pick(g, []string{"SELECT " + routine + "()", "DO $$ BEGIN PERFORM " + routine + "(); END $$"}))
	case 3:
		stmts = append(stmts, fmt.Sprintf("CREATE PROCEDURE %s() LANGUAGE plpgsql AS $$ BEGIN %s; END $$", routine, body), "CALL "+routine+"()")
	case 4:
		stmts = append(stmts, fmt.Sprintf("CREATE FUNCTION %s() RETURNS void LANGUAGE sql AS $$ %s $$", routine, ddl), "SELECT "+routine+"()")
	case 5:
		src := g.name("cs")
		stmts = append(stmts,
			fmt.Sprintf("CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN %s; RETURN NEW; END $$", routine, body),
			fmt.Sprintf("CREATE TABLE %s (a int)", src),
			fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()", g.name("tg"), src, routine),
			fmt.Sprintf("INSERT INTO %s (a) VALUES (1)", src))
	}
	stmts = append(stmts, fmt.Sprintf(pick(g, u2Later), t, x, fresh))
	if g.chance(0.3) {
		stmts = append(stmts, fmt.Sprintf(pick(g, u2Later), t, x, g.name("cf")))
	}
	return strings.Join(stmts, ";\n"), "O1.code_effects_u2", true
}
