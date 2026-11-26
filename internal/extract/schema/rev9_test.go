package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestCanonicalType checks the alias table of gram.y REL_17_6 (Numeric,
// Character, Bit, ConstDatetime) and that only aliases are equated
// (Revision 9 C1).
func TestCanonicalType(t *testing.T) {
	for _, tc := range []struct {
		typ, want string
	}{
		{"int", "int4"}, {"INTEGER", "int4"}, {"int4", "int4"}, {`"int4"`, "int4"},
		{"smallint", "int2"}, {"int2", "int2"}, {"bigint", "int8"}, {"INT8", "int8"},
		{"real", "float4"}, {"float4", "float4"}, {"float(24)", "float4"}, {"float(1)", "float4"},
		{"double  precision", "float8"}, {"float", "float8"}, {"float(25)", "float8"}, {"float(53)", "float8"},
		{"boolean", "bool"}, {"bool", "bool"},
		{"varchar(36)", "varchar(36)"}, {"character varying(36)", "varchar(36)"}, {"char varying (36)", "varchar(36)"},
		{"national character varying(36)", "varchar(36)"}, {"varchar", "varchar"},
		{"char", "bpchar(1)"}, {"character(1)", "bpchar(1)"}, {"char(10)", "bpchar(10)"}, {"nchar(10)", "bpchar(10)"},
		{"decimal(10, 2)", "numeric(10,2)"}, {"dec(10,2)", "numeric(10,2)"}, {"NUMERIC", "numeric"},
		{"timestamp", "timestamp"}, {"timestamp without time zone", "timestamp"},
		{"timestamptz", "timestamptz"}, {"timestamp with time zone", "timestamptz"}, {"timestamp(3) with time zone", "timestamptz(3)"},
		{"time", "time"}, {"time with time zone", "timetz"}, {"timetz", "timetz"},
		{"bit", "bit(1)"}, {"bit varying(4)", "varbit(4)"},
		{"int[]", "int4[]"}, {"integer[3]", "int4[]"}, {"int ARRAY", "int4[]"}, {"int4 array[2]", "int4[]"}, {"int[][]", "int4[][]"},
		{"interval day to second", "interval day to second"},
		{`"integer"`, "integer"}, {`"char"`, "char"}, {`"Int4"`, "Int4"},
		{"app.my_type", "app.my_type"}, {"pg_catalog.int4", "pg_catalog.int4"},
	} {
		got, ok := canonicalType(tc.typ)
		if !ok || got != tc.want {
			t.Errorf("canonicalType(%q) = %q, %v; want %q", tc.typ, got, ok, tc.want)
		}
	}
	for _, typ := range []string{"float(0)", "float(54)", "int[", "varchar('x')", ""} {
		if got, ok := canonicalType(typ); ok {
			t.Errorf("canonicalType(%q) = %q; want unreadable", typ, got)
		}
	}
	for _, pair := range [][2]string{
		{"int", "bigint"}, {"text", "varchar(36)"}, {"varchar(36)", "varchar(40)"}, {"varchar", "varchar(36)"},
		{"char", `"char"`}, {`"integer"`, "integer"}, {"timestamp", "timestamptz"}, {"int[]", "int"},
		{"float(24)", "float(25)"}, {"numeric(10,2)", "numeric"}, {"bpchar", "char"},
	} {
		if sameType(pair[0], pair[1]) {
			t.Errorf("sameType(%q, %q) = true; want false", pair[0], pair[1])
		}
	}
}

// TestRevisionNineTypeAliases checks that a foreign key between aliases
// of one type links and leaves the migration applied, while different
// types still may fail (Revision 9 C1). Every "links" case gave may-fail
// on 2cfe977.
func TestRevisionNineTypeAliases(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	mayFail := want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true, "status ref c.pid": "unresolved"}
	links := want{"applied 1_a.sql": true, "applied 2_b.sql": true, "diag schema_migration_may_fail": false, "status ref c.pid": "linked"}
	fk := func(parent, child string) string {
		return fmt.Sprintf("-- FILE: 1_a.sql\nCREATE TABLE p (id %s PRIMARY KEY);\nCREATE TABLE c (pid %s REFERENCES p);\n", parent, child) + later
	}
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		{"serial and int link", fk("serial", "int"), links},
		{"int4 and INT link", fk("int4", "INT"), links},
		{"bigserial and int8 link", fk("bigserial", "int8"), links},
		{"smallserial and int2 link", fk("smallserial", "int2"), links},
		{"varchar(36) and character varying(36) link", fk("varchar(36)", "character varying(36)"), links},
		{"timestamptz and timestamp with time zone link", fk("timestamptz", "timestamp with time zone"), links},
		{"quoted int4 and integer link", fk(`"int4"`, "integer"), links},
		// C2 (T018a2 B2): verified compatible pairs link.
		{"int and bigint link (C2)", fk("int", "bigint"), links},
		{"text and varchar(36) link (C2)", fk("text", "varchar(36)"), links},
		{"varchar(36) and varchar(40) link (C2)", fk("varchar(36)", "varchar(40)"), links},
		{"numeric and int may fail", fk("int", "numeric"), mayFail},
		{"uuid and text may fail", fk("uuid", "text"), mayFail},
		{"ALTER COLUMN TYPE to an alias keeps the reference",
			"-- FILE: 1_a.sql\nCREATE TABLE p (id bigint PRIMARY KEY);\nCREATE TABLE c (pid int8 REFERENCES p);\nALTER TABLE c ALTER COLUMN pid TYPE int8;\nALTER TABLE p ALTER COLUMN id TYPE bigint;\n" + later, links},
		{"ALTER COLUMN TYPE to a compatible type links (C2)",
			"-- FILE: 1_a.sql\nCREATE TABLE p (id serial PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\nALTER TABLE c ALTER COLUMN pid TYPE bigint;\n" + later, links},
		{"ALTER COLUMN TYPE to a different type may fail",
			"-- FILE: 1_a.sql\nCREATE TABLE p (id serial PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\nALTER TABLE c ALTER COLUMN pid TYPE text;\n" + later, mayFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := linkCase(t, tc.sql)
			for q, w := range tc.want {
				got := l.fact(t, q)
				if fmt.Sprint(got) != fmt.Sprint(w) || !reflect.DeepEqual(normalize(got), normalize(w)) {
					t.Errorf("%s: got %#v, want %#v", q, got, w)
				}
			}
		})
	}
}
