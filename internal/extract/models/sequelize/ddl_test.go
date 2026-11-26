package sequelize

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestDDLRendering compares the claim pass with Sequelize 6.37.8's own
// query generator on every dialect: testdata/ddl-cases.js is rendered by
// attributesToSQL and createTableQuery in a scratch Node process (stub
// driver module, nothing connects; testdata/ddl-6.37.8.json) and linked
// here with the same source. For each model the claimed column set is the
// generator's; for each read column the generator's definition starts
// with the type text the pass rendered, has NOT NULL exactly when the
// attribute says allowNull false, a DEFAULT with the text the pass claims
// (or none when it claims none), UNIQUE when the pass claims an inline
// unique, PRIMARY KEY exactly for the claimed primary keys; composite
// unique constraints and sqlite's integer primary-key rewrite are in the
// CREATE TABLE text. This checks the rendering, not the database: the
// catalog forms (postgres format_type, the sqlite catalog) are the
// oracle's, and mysql, mariadb and mssql stay mapped. Every
// claimed foreign-key list is compared with the definition's REFERENCES
// clause: none for an empty list, else the table, column and the ON
// DELETE / ON UPDATE actions the generator writes (an absent clause is
// the database's NO ACTION; mssql's cleared actions).
func TestDDLRendering(t *testing.T) {
	data, err := os.ReadFile("testdata/ddl-6.37.8.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Dialects map[string]map[string]struct {
			Table      string            `json:"table"`
			Attributes map[string]string `json:"attributes"`
			SQL        string            `json:"sql"`
			Throws     string            `json:"throws"`
		} `json:"dialects"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile("testdata/ddl-cases.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []string{"postgres", "sqlite", "mysql", "mariadb", "mssql"} {
		t.Run(dialect, func(t *testing.T) {
			src := strings.Replace(string(cases), "'__DIALECT__'", "'"+dialect+"'", 1)
			driver := map[string]string{"postgres": "pg", "sqlite": "sqlite3", "mysql": "mysql2", "mariadb": "mariadb", "mssql": "tedious"}[dialect]
			in := input(t, map[string]string{
				"package.json":      `{"dependencies": {"sequelize": "6.37.8", "` + driver + `": "1.0.0"}}`,
				"package-lock.json": lockJSON("6.37.8"),
				"cases.js":          src,
			})
			facts := map[string]*colFacts{}
			ddlHook = func(table string, c *colFacts) { facts[table+"."+c.a.field] = c }
			defer func() { ddlHook = nil }()
			nodes, _, diags := Link(in)
			for _, d := range diags {
				if d.Code == codeImportFailure || d.Code == codeNotClaimed {
					t.Errorf("%s", d.Message)
				}
			}
			tables := map[string]contract.Node{}
			for _, n := range nodes {
				if n.Kind == "table" {
					tables[n.Name] = n
				}
			}
			byTable := map[string]map[string]contract.Node{}
			for _, n := range nodes {
				if n.Kind != "column" {
					continue
				}
				for name, tn := range tables {
					if strings.HasPrefix(n.QualifiedName, tn.QualifiedName+".") && n.File == tn.File {
						if byTable[name] == nil {
							byTable[name] = map[string]contract.Node{}
						}
						byTable[name][n.Name] = n
					}
				}
			}
			checked, fks := 0, 0
			for model, w := range want.Dialects[dialect] {
				tn, ok := tables[w.Table]
				if !ok {
					t.Errorf("%s: table %s not claimed", model, w.Table)
					continue
				}
				cols := byTable[w.Table]
				if w.Throws != "" {
					// The generator rejects the model on this dialect; the
					// pass must not claim the column it rejects.
					for _, c := range cols {
						if c.Attributes["state"] == "read" && c.Attributes["type"] == "JSON" {
							t.Errorf("%s.%s: claimed though the generator throws (%s)", w.Table, c.Name, w.Throws)
						}
					}
					continue
				}
				if len(cols) != len(w.Attributes) {
					t.Errorf("%s: %d columns, the generator writes %d", w.Table, len(cols), len(w.Attributes))
				}
				for field, def := range w.Attributes {
					c, ok := cols[field]
					if !ok {
						t.Errorf("%s.%s: no column record", w.Table, field)
						continue
					}
					if c.Attributes["state"] != "read" {
						continue
					}
					checked++
					checkDefinition(t, dialect, w.Table+"."+field, def, w.SQL, c, facts[w.Table+"."+field])
					fks += checkReferences(t, dialect, w.Table+"."+field, def, c)
				}
				for _, u := range listOf(tn.Attributes["unique_constraints"]) {
					cols := u["columns"].([]string)
					if len(cols) < 2 {
						continue
					}
					q := make([]string, len(cols))
					for i, c := range cols {
						q[i] = quoteFor(dialect, c)
					}
					if !strings.Contains(w.SQL, "("+strings.Join(q, ", ")+")") {
						t.Errorf("%s: unique %v not in %s", w.Table, cols, w.SQL)
					}
				}
			}
			if checked < 50 {
				t.Errorf("only %d columns checked", checked)
			}
			if fks < 7 {
				t.Errorf("only %d foreign keys checked", fks)
			}
		})
	}
}

func listOf(v any) []map[string]any {
	l, _ := v.([]map[string]any)
	return l
}

func quoteFor(dialect, name string) string {
	switch dialect {
	case "postgres":
		return `"` + name + `"`
	case "mssql":
		return "[" + name + "]"
	}
	return "`" + name + "`"
}

// checkDefinition compares one read column with the generator's column
// definition def (attributesToSQL) and the table's CREATE TABLE text.
func checkDefinition(t *testing.T, dialect, id, def, sql string, n contract.Node, c *colFacts) {
	t.Helper()
	a := n.Attributes
	if c == nil {
		t.Errorf("%s: no rendering recorded", id)
		return
	}
	if !strings.HasPrefix(def, c.tt.sql) {
		t.Errorf("%s: the generator writes %q, the pass rendered the type %q", id, def, c.tt.sql)
	}
	if dialect != "postgres" && !c.intPK && a["type"] != c.tt.sql {
		t.Errorf("%s: type %v, rendered %q", id, a["type"], c.tt.sql)
	}
	notNull := strings.Contains(def, " NOT NULL")
	if notNull != (c.a.allowNull == triFalse) {
		t.Errorf("%s: NOT NULL in %q, allowNull %v", id, def, c.a.allowNull)
	}
	switch {
	case c.intPK:
		if !strings.Contains(sql, quoteFor(dialect, n.Name)+" INTEGER PRIMARY KEY") || a["nullable"] != true || a["type"] != "INTEGER" {
			t.Errorf("%s: sqlite integer primary key not in %s (%v %v)", id, sql, a["type"], a["nullable"])
		}
	case a["nullable"] == false && !c.pk && !c.serial && !notNull:
		t.Errorf("%s: claimed not null, the generator writes %q", id, def)
	case a["nullable"] == true && notNull:
		t.Errorf("%s: claimed nullable, the generator writes %q", id, def)
	case dialect == "mssql" && a["nullable"] == true && !strings.Contains(def, " NULL"):
		t.Errorf("%s: mssql nullable claimed without NULL in %q", id, def)
	}
	hasDefault := strings.Contains(def, " DEFAULT ") && !strings.Contains(def, " DEFAULT NULL")
	switch a["server_default"] {
	case true:
		// SERIAL's nextval default is written by postgres, not as a
		// DEFAULT clause.
		if !hasDefault && !c.serial {
			t.Errorf("%s: default claimed, the generator writes %q", id, def)
		}
		if text, ok := a["server_default_text"].(string); ok && !strings.Contains(def, " DEFAULT "+text) {
			t.Errorf("%s: default text %q, the generator writes %q", id, text, def)
		}
	case false:
		if hasDefault {
			t.Errorf("%s: no default claimed, the generator writes %q", id, def)
		}
	}
	if (a["unique"] == true && c.inlineUnique) != strings.Contains(def, " UNIQUE") && !(dialect == "sqlite" && c.intPK) {
		t.Errorf("%s: inline unique %v, the generator writes %q", id, c.inlineUnique, def)
	}
	if (a["primary_key"] == true) != strings.Contains(def, "PRIMARY KEY") {
		t.Errorf("%s: primary key %v, the generator writes %q", id, a["primary_key"], def)
	}
	if c.serial && !strings.Contains(def, " SERIAL") {
		t.Errorf("%s: serial, the generator writes %q", id, def)
	}
}

// referencesClause matches the generator's REFERENCES clause: table,
// column, then the optional ON DELETE and ON UPDATE actions.
var referencesClause = regexp.MustCompile(`REFERENCES [\["\x60]([^\]"\x60]+)[\]"\x60] \([\["\x60]([^\]"\x60]+)[\]"\x60]\)( ON DELETE (CASCADE|SET NULL|SET DEFAULT|RESTRICT|NO ACTION))?( ON UPDATE (CASCADE|SET NULL|SET DEFAULT|RESTRICT|NO ACTION))?`)

// checkReferences compares a read column's claimed foreign-key list with
// the generator's definition; it returns the number of foreign keys
// compared.
func checkReferences(t *testing.T, dialect, id, def string, n contract.Node) int {
	t.Helper()
	if n.Attributes["foreign_keys"] == nil {
		return 0
	}
	raw, _ := json.Marshal(n.Attributes["foreign_keys"])
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	m := referencesClause.FindStringSubmatch(def)
	switch {
	case len(list) == 0 && m == nil:
		return 0
	case len(list) == 0:
		t.Errorf("%s: no foreign key claimed, the generator writes %q", id, def)
		return 0
	case len(list) > 1 || m == nil:
		t.Errorf("%s: foreign keys %s, the generator writes %q", id, raw, def)
		return 0
	}
	action := func(s string) string {
		if s == "" {
			return "NO ACTION"
		}
		return s
	}
	fk := list[0]
	if fk["table"] != m[1] || fk["column"] != m[2] || fk["on_delete"] != action(m[4]) || fk["on_update"] != action(m[6]) {
		t.Errorf("%s (%s): foreign key %s, the generator writes %q", id, dialect, raw, def)
	}
	return 1
}

// lockJSON is a package-lock.json pinning sequelize and inflection.
func lockJSON(version string) string {
	return `{"lockfileVersion": 3, "packages": {"": {}, "node_modules/sequelize": {"version": "` + version + `"}, "node_modules/inflection": {"version": "1.13.4"}}}`
}
