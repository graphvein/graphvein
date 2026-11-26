package gorm

import (
	"encoding/json"
	"os"
	"testing"
)

// portCases are the outputs the real libraries produced in scratch
// (testdata/port-1.31.2.json, open point O4): gorm.io/gorm v1.31.2's
// schema.NamingStrategy and github.com/jinzhu/inflection v1.0.0 over every
// struct and field identifier of both corpora, the investigation probes and the
// fixture, and inflection's own test list.
type portCases struct {
	Names [][]string `json:"names"`
	Words [][]string `json:"words"`
}

func readPortCases(t *testing.T) portCases {
	t.Helper()
	data, err := os.ReadFile("testdata/port-1.31.2.json")
	if err != nil {
		t.Fatal(err)
	}
	var c portCases
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Names) < 800 || len(c.Words) < 1500 {
		t.Fatalf("%d names, %d words: the case file is truncated", len(c.Names), len(c.Words))
	}
	return c
}

// TestInflection compares the inflection port with the library's Plural
// and Singular.
func TestInflection(t *testing.T) {
	for _, w := range readPortCases(t).Words {
		if got := Plural(w[0]); got != w[1] {
			t.Errorf("Plural(%q) = %q, inflection v1.0.0 gives %q", w[0], got, w[1])
		}
		if got := Singular(w[0]); got != w[2] {
			t.Errorf("Singular(%q) = %q, inflection v1.0.0 gives %q", w[0], got, w[2])
		}
	}
}

// TestNaming compares the naming port with GORM's NamingStrategy: column
// and table names (default, prefixed singular, no lower case), join-table
// names, and index, unique and foreign-key names through formatName at
// the limits 63, 64 and 30 (with the sha1 suffix).
func TestNaming(t *testing.T) {
	def := namer{limit: 64}
	n63 := namer{limit: 63}
	n30 := namer{limit: 30}
	lit := namer{Naming: Naming{TablePrefix: "t_", SingularTable: true}, limit: 64}
	nl := namer{Naming: Naming{NoLowerCase: true}, limit: 64}
	for _, c := range readPortCases(t).Names {
		n := c[0]
		got := []string{n, def.ColumnName(n), def.TableName(n), lit.TableName(n), def.JoinTableName(n), nl.TableName(n),
			n63.IndexName("some_table_name_of_middling_length", n), def.IndexName("some_table_name_of_middling_length", n), n30.IndexName("t", n),
			n63.UniqueName("tbl", n), def.RelationshipFKName("owners", n)}
		for i := range got {
			if got[i] != c[i] {
				t.Errorf("%s: column %d = %q, gorm v1.31.2 gives %q", n, i, got[i], c[i])
			}
		}
	}
	// The dialect limits: 63 under postgres, else 64 (GO-K4);
	// a literal IdentifierMaxLength wins.
	for _, tc := range []struct {
		n       Naming
		dialect string
		limit   int
	}{{Naming{}, DialectPostgres, 63}, {Naming{}, DialectSQLite, 64}, {Naming{}, DialectMySQL, 64}, {Naming{IdentifierMaxLength: 30}, DialectPostgres, 30}} {
		if got := newNamer(tc.n, tc.dialect).limit; got != tc.limit {
			t.Errorf("%+v on %s: limit %d, want %d", tc.n, tc.dialect, got, tc.limit)
		}
	}
}
