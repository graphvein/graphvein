package drizzle

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// TestConvertCasing checks the casing rule against drizzle-orm's
// toSnakeCase/toCamelCase (casing.js:3-13), including the lookahead of
// [A-Z]+(?![a-z]) that RE2 lacks. The expected values are drizzle-orm
// 0.45.4's own output for these inputs.
func TestConvertCasing(t *testing.T) {
	snake := map[string]string{
		"displayName": "display_name", "userID": "user_id", "HTMLParser": "html_parser", "ExplicitName": "explicit_name",
		"apiKeyV2": "api_key_v_2", "already_snake": "already_snake", "ABC": "abc", "aB": "a_b", "x1Y2": "x1_y_2",
		"it'sOk": "its_ok", "Ünïcode": "n_code", "": "",
	}
	for in, want := range snake {
		if got := convertCasing("snake_case", in); got != want {
			t.Errorf("snake_case(%q) = %q, want %q", in, got, want)
		}
	}
	camel := map[string]string{"display_name": "displayName", "api_key_v2": "apiKeyV2", "HTMLParser": "htmlParser", "user_ID": "userID", "x": "x"}
	for in, want := range camel {
		if got := convertCasing("camelCase", in); got != want {
			t.Errorf("camelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSNumber(t *testing.T) {
	for in, want := range map[string]string{"0": "0", "-1": "-1", "+2": "2", "1.5": "1.5", "123456789012345": "123456789012345"} {
		if got, ok := jsNumber(in); !ok || got != want {
			t.Errorf("jsNumber(%q) = %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"1e3", "0x10", "1.50", ".5", "01", "-0", "-0.0", "1_000", "1234567890123456"} {
		if got, ok := jsNumber(in); ok {
			t.Errorf("jsNumber(%q) = %q, want not exact", in, got)
		}
	}
}

func lit(kind, value string) typescript.ModelExpr {
	return typescript.ModelExpr{Kind: kind, Value: value}
}

func TestJSONStringify(t *testing.T) {
	obj := typescript.ModelExpr{Kind: typescript.ModelObject, Entries: []typescript.ModelEntry{
		{Kind: typescript.ModelProperty, Key: "a", Value: lit(typescript.ModelString, "q\"\\\n\x01é")},
		{Kind: typescript.ModelProperty, Key: "b", Value: typescript.ModelExpr{Kind: typescript.ModelArray, Elements: []typescript.ModelExpr{lit(typescript.ModelNumber, "1"), {Kind: typescript.ModelNull}, lit(typescript.ModelBoolean, "false")}}},
	}}
	if got, ok := jsonStringify(obj); !ok || got != `{"a":"q\"\\\n\u0001é","b":[1,null,false]}` {
		t.Errorf("jsonStringify = %s %v", got, ok)
	}
	for name, e := range map[string]typescript.ModelExpr{
		"index key":  {Kind: typescript.ModelObject, Entries: []typescript.ModelEntry{{Kind: typescript.ModelProperty, Key: "1", Value: lit(typescript.ModelNumber, "1")}}},
		"duplicate":  {Kind: typescript.ModelObject, Entries: []typescript.ModelEntry{{Kind: typescript.ModelProperty, Key: "a", Value: lit(typescript.ModelNumber, "1")}, {Kind: typescript.ModelProperty, Key: "a", Value: lit(typescript.ModelNumber, "2")}}},
		"shorthand":  {Kind: typescript.ModelObject, Entries: []typescript.ModelEntry{{Kind: typescript.ModelShorthand, Key: "a"}}},
		"identifier": {Kind: typescript.ModelIdentifier, Name: "x"},
		"exponent":   lit(typescript.ModelNumber, "1e9"),
	} {
		if got, ok := jsonStringify(e); ok {
			t.Errorf("%s: jsonStringify = %s, want not exact", name, got)
		}
	}
}

func TestPgArrayString(t *testing.T) {
	arr := func(els ...typescript.ModelExpr) typescript.ModelExpr {
		return typescript.ModelExpr{Kind: typescript.ModelArray, Elements: els}
	}
	if got, ok := pgArrayString(arr(lit(typescript.ModelString, "a b"), lit(typescript.ModelString, "")), 0); !ok || got != `{"a b",""}` {
		t.Errorf("strings: %s %v", got, ok)
	}
	if got, ok := pgArrayString(arr(arr(lit(typescript.ModelNumber, "1")), arr(lit(typescript.ModelBoolean, "true"))), 0); !ok || got != "{{1},{true}}" {
		t.Errorf("nested: %s %v", got, ok)
	}
	for _, s := range []string{`a"b`, `a\b`, "it's"} {
		if _, ok := pgArrayString(arr(lit(typescript.ModelString, s)), 0); ok {
			t.Errorf("%q: drizzle-kit writes it unescaped; must not be rendered", s)
		}
	}
}

func TestTruncateIdentifier(t *testing.T) {
	long := strings.Repeat("a", 62) + "éz"
	if got := truncateIdentifier(long); got != strings.Repeat("a", 62) {
		t.Errorf("truncate at a character boundary: %q", got)
	}
	if got := truncateIdentifier("short"); got != "short" {
		t.Error(got)
	}
}

func TestCatalogType(t *testing.T) {
	for in, want := range map[string]string{
		"varchar(20)": "character varying(20)", "varchar": "character varying", "char": "character(1)", "char(3)": "character(3)",
		"numeric(10, 2)": "numeric(10,2)", "numeric(8)": "numeric(8,0)", "timestamp": "timestamp without time zone",
		"timestamp (3) with time zone": "timestamp(3) with time zone", "time(2)": "time(2) without time zone",
		"serial": "integer", "bigserial": "bigint", "smallserial": "smallint", "text[]": "text[]", "varchar(5)[][]": "character varying(5)[]",
		"double precision": "double precision", "app.app_role[]": "app.app_role[]",
	} {
		if got := catalogType("postgresql", in); got != want {
			t.Errorf("catalogType(%q) = %q, want %q", in, got, want)
		}
	}
	if got := catalogType("sqlite", "text(40)"); got != "text(40)" {
		t.Error(got)
	}
}
