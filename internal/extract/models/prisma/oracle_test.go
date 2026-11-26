package prisma

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// The dump scripts/oracle/prisma_catalog.sh writes: one run per schema
// and Prisma version, with the catalog of the database Prisma's own SQL
// created (postgresql: the pg_catalog; sqlite: sqlite_master and the
// pragmas).
type oracleDump struct {
	Runs []oracleRun `json:"runs"`
}

type oracleRun struct {
	Dir      string        `json:"dir"`
	Schema   string        `json:"schema"`
	Prisma   string        `json:"prisma"`
	Provider string        `json:"provider"`
	Dropped  []string      `json:"dropped"`
	Catalog  oracleCatalog `json:"catalog"`
}

type oracleCatalog struct {
	Tables []oracleTable `json:"tables"`
	Enums  []struct {
		Schema string   `json:"schema"`
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"enums"`
}

type oracleTable struct {
	Schema  *string `json:"schema"`
	Name    string  `json:"name"`
	SQL     string  `json:"sql"`
	Columns []struct {
		Name       string  `json:"name"`
		Type       string  `json:"type"`
		NotNull    bool    `json:"not_null"`
		Default    *string `json:"default"`
		TypeSchema *string `json:"type_schema"`
		TypeName   *string `json:"type_name"`
		Array      bool    `json:"array"`
	} `json:"columns"`
	// Postgres only.
	Constraints []struct {
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Columns    []string `json:"columns"`
		RefSchema  *string  `json:"ref_schema"`
		RefTable   *string  `json:"ref_table"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
		Index      *string  `json:"index"`
	} `json:"constraints"`
	Indexes []struct {
		Name    string   `json:"name"`
		Unique  bool     `json:"unique"`
		Primary bool     `json:"primary"`
		Origin  string   `json:"origin"`
		Columns []string `json:"columns"`
		Method  string   `json:"method"`
		Partial bool     `json:"partial"`
		Exprs   bool     `json:"expressions"`
	} `json:"indexes"`
	// SQLite only.
	PrimaryKey  []string `json:"primary_key"`
	ForeignKeys []struct {
		RefTable   string   `json:"ref_table"`
		Columns    []string `json:"columns"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
	} `json:"foreign_keys"`
}

// catTable is a catalog table in the record's terms.
type catTable struct {
	schema  *string
	name    string
	sql     string
	columns map[string]catColumn
	order   []string
	pk      []string
	pkName  *string
	indexes []catIndex
	fks     []catFK
	uniques int
}

type catColumn struct {
	typ        string
	notNull    bool
	def        *string
	typeSchema *string
	typeName   *string
	array      bool
}

type catIndex struct {
	name    string
	unique  bool
	columns []string
	method  string
}

type catFK struct {
	name       *string
	columns    []string
	refSchema  *string
	refTable   string
	refColumns []string
	onDelete   string
	onUpdate   string
}

// Referential actions as Prisma names them: pg_constraint codes and
// SQLite's pragma text.
var pgAction = map[string]string{"a": "NoAction", "r": "Restrict", "c": "Cascade", "n": "SetNull", "d": "SetDefault"}
var sqliteAction = map[string]string{"NO ACTION": "NoAction", "RESTRICT": "Restrict", "CASCADE": "Cascade", "SET NULL": "SetNull", "SET DEFAULT": "SetDefault"}

func catalogTables(run oracleRun) []*catTable {
	var out []*catTable
	for _, t := range run.Catalog.Tables {
		ct := &catTable{schema: t.Schema, name: t.Name, sql: t.SQL, columns: map[string]catColumn{}}
		for _, c := range t.Columns {
			ct.columns[c.Name] = catColumn{c.Type, c.NotNull, c.Default, c.TypeSchema, c.TypeName, c.Array}
			ct.order = append(ct.order, c.Name)
		}
		backing := map[string]bool{}
		for _, con := range t.Constraints {
			switch con.Type {
			case "p":
				ct.pk = con.Columns
				name := con.Name
				ct.pkName = &name
				if con.Index != nil {
					backing[*con.Index] = true
				}
			case "u":
				ct.uniques++
				if con.Index != nil {
					backing[*con.Index] = true
				}
			case "f":
				name := con.Name
				ct.fks = append(ct.fks, catFK{&name, con.Columns, con.RefSchema, deref(con.RefTable), con.RefColumns, pgAction[con.OnDelete], pgAction[con.OnUpdate]})
			}
		}
		if run.Provider == providerSQLite {
			ct.pk = t.PrimaryKey
			for _, fk := range t.ForeignKeys {
				ct.fks = append(ct.fks, catFK{nil, fk.Columns, nil, fk.RefTable, fk.RefColumns, sqliteAction[fk.OnDelete], sqliteAction[fk.OnUpdate]})
			}
		}
		for _, ix := range t.Indexes {
			switch {
			case ix.Primary || backing[ix.Name] || ix.Origin == "pk":
				continue
			case ix.Origin == "u":
				ct.uniques++
				continue
			}
			if ix.Partial || ix.Exprs {
				ct.indexes = append(ct.indexes, catIndex{name: ix.Name, unique: ix.Unique, method: "partial or expression"})
				continue
			}
			ct.indexes = append(ct.indexes, catIndex{ix.Name, ix.Unique, ix.Columns, ix.Method})
		}
		out = append(out, ct)
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// lockPin matches the root lockfile entry of the Prisma CLI, which the
// test rewrites to link the fixture as if locked at a run's version.
var lockPin = regexp.MustCompile(`("node_modules/prisma":\s*\{\s*"version":\s*")[^"]+(")`)

// oracleStats counts the comparison of one run.
type oracleStats struct {
	tables, columns, facts int
	disagreements          []string
}

func (s *oracleStats) check(ok bool, format string, a ...any) {
	s.facts++
	if !ok {
		s.disagreements = append(s.disagreements, fmt.Sprintf(format, a...))
	}
}

// TestPrismaOracle compares every claimed table and column fact of the
// runs in the dump GRAPHVEIN_PRISMA_ORACLE names with the catalog Prisma's
// own SQL created (precision stop S1 on any disagreement).
// GRAPHVEIN_PRISMA_ORACLE_ROOT is the directory the dump's runs are
// relative to (the fixture directory, or the directory holding the corpus
// checkouts). Skipped without them.
func TestPrismaOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_PRISMA_ORACLE"), os.Getenv("GRAPHVEIN_PRISMA_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_PRISMA_ORACLE or GRAPHVEIN_PRISMA_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump oracleDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Runs) == 0 {
		t.Fatal("no runs in the dump")
	}
	for _, run := range dump.Runs {
		name := fmt.Sprintf("%s/%s@%s", run.Dir, run.Schema, run.Prisma)
		t.Run(name, func(t *testing.T) {
			sources := corpusSources(t, filepath.Join(root, run.Dir))
			for p, src := range sources {
				if filepath.Base(p) == "package-lock.json" && lockPin.Match(src) {
					sources[p] = lockPin.ReplaceAll(src, []byte("${1}"+run.Prisma+"${2}"))
				}
			}
			nodes, _, _ := Link(Input{Sources: sources})
			stats := compareRun(t, run, nodes)
			t.Logf("%s (%s): %d tables, %d columns, %d facts compared, %d disagreements, dropped %d forms",
				name, run.Provider, stats.tables, stats.columns, stats.facts, len(stats.disagreements), len(run.Dropped))
			for _, d := range stats.disagreements {
				t.Errorf("disagreement: %s", d)
			}
		})
	}
}

// record is a node's attributes as the store holds them (JSON).
func record(t *testing.T, n contract.Node) map[string]any {
	t.Helper()
	data, err := json.Marshal(n.Attributes)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func strs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = fmt.Sprint(x)
	}
	return out
}

func compareRun(t *testing.T, run oracleRun, nodes []contract.Node) *oracleStats {
	stats := &oracleStats{}
	set := "models/prisma/" + run.Schema
	droppedBlock, droppedField := map[string]bool{}, map[string]bool{}
	for _, d := range run.Dropped {
		switch {
		case strings.Contains(d, ":"):
		case strings.Contains(d, "."):
			droppedField[d] = true
		default:
			droppedBlock[d] = true
		}
	}
	cat := catalogTables(run)
	enums := map[string][]string{}
	for _, e := range run.Catalog.Enums {
		enums[e.Schema+"."+e.Name] = e.Values
	}
	tables := map[string]map[string]any{}
	columns := map[string][]map[string]any{}
	matched := map[*catTable]bool{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			a := record(t, n)
			if a["set"] == set {
				tables[n.Identity] = a
			}
		case "column":
			table := "table:" + n.Identity[len("column:"):strings.LastIndex(n.Identity, ".")]
			columns[table] = append(columns[table], record(t, n))
		}
	}
	if len(tables) == 0 {
		t.Fatalf("no table records in set %s", set)
	}
	ids := make([]string, 0, len(tables))
	for id := range tables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := tables[id]
		model, _ := a["model"].(string)
		if droppedBlock[model] {
			continue
		}
		if pin, _ := a["pins"].(map[string]any); pin["prisma"] != run.Prisma {
			t.Fatalf("%s: pins %v, run %s", id, a["pins"], run.Prisma)
		}
		stats.tables++
		ct := findTable(cat, a)
		stats.check(ct != nil, "%s: table %v.%v is not in the catalog", id, a["schema"], a["name"])
		if ct == nil {
			continue
		}
		if matched[ct] {
			stats.check(false, "%s: catalog table %s matched twice", id, ct.name)
		}
		matched[ct] = true
		var cols []map[string]any
		for _, c := range columns[id] {
			if !droppedField[model+"."+fmt.Sprint(c["attribute"])] {
				cols = append(cols, c)
			}
		}
		compareTable(stats, run, id, a, cols, ct, enums)
	}
	// Every table the catalog holds is a claimed table, unless a block of
	// the set was refused (PR-P1, PR-AA4) or the set has a partial table
	// list: those are reported, not failed.
	for _, ct := range cat {
		if !matched[ct] {
			t.Logf("catalog table %v.%s has no compared record", deref(ct.schema), ct.name)
		}
	}
	return stats
}

func findTable(cat []*catTable, a map[string]any) *catTable {
	var found *catTable
	for _, ct := range cat {
		if ct.name != a["name"] {
			continue
		}
		if a["schema_known"] == true && fmt.Sprint(a["schema"]) != fmt.Sprint(derefAny(ct.schema)) {
			continue
		}
		if found != nil {
			return nil
		}
		found = ct
	}
	return found
}

func derefAny(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func claimed(c map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := c["claims"].([]any)
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out[fmt.Sprint(m["fact"])] = true
		}
	}
	return out
}

var pgMethod = map[string]string{"BTree": "btree", "Hash": "hash", "Gist": "gist", "Gin": "gin", "SpGist": "spgist", "Brin": "brin"}

func compareTable(s *oracleStats, run oracleRun, id string, a map[string]any, cols []map[string]any, ct *catTable, enums map[string][]string) {
	complete := a["constraint_set"] == "complete"
	if a["column_set"] == "complete" {
		var names []string
		for _, c := range cols {
			names = append(names, fmt.Sprint(c["name"]))
		}
		sort.Strings(names)
		want := append([]string{}, ct.order...)
		sort.Strings(want)
		s.check(reflect.DeepEqual(names, want), "%s: columns %v, catalog %v", id, names, want)
	}
	if pk, ok := a["primary_key"].([]any); ok {
		got := strs(pk)
		s.check(reflect.DeepEqual(got, ct.pk) || len(got) == 0 && len(ct.pk) == 0, "%s: primary key %v, catalog %v", id, got, ct.pk)
	}
	if name, ok := a["primary_key_name"].(string); ok && run.Provider == providerPostgres {
		s.check(ct.pkName != nil && *ct.pkName == name, "%s: primary key name %s, catalog %v", id, name, derefAny(ct.pkName))
	}
	if list, ok := a["unique_constraints"].([]any); ok && complete {
		s.check(len(list) == ct.uniques, "%s: %d unique constraints, catalog %d", id, len(list), ct.uniques)
	}
	// Indexes: each claimed index is in the catalog; a complete constraint
	// set lists every catalog index.
	used := map[int]bool{}
	list, _ := a["indexes"].([]any)
	for _, x := range list {
		ix, _ := x.(map[string]any)
		cols := strs(ix["columns"])
		found := -1
		for i, ci := range ct.indexes {
			if used[i] || ci.unique != (ix["unique"] == true) || !reflect.DeepEqual(ci.columns, cols) {
				continue
			}
			if name, ok := ix["name"].(string); ok && name != ci.name {
				continue
			}
			if m, ok := ix["method"].(string); ok && pgMethod[m] != ci.method {
				continue
			}
			found = i
			break
		}
		s.check(found >= 0, "%s: index %v is not in the catalog %+v", id, ix, ct.indexes)
		if found >= 0 {
			used[found] = true
		}
	}
	if complete {
		s.check(len(list) == len(ct.indexes), "%s: %d indexes, catalog %d %+v", id, len(list), len(ct.indexes), ct.indexes)
	}
	// Foreign keys: the columns' lists, one constraint per local column
	// list, against the catalog's.
	fkSeen := map[string]bool{}
	allFKsKnown := true
	for _, c := range cols {
		colName := fmt.Sprint(c["name"])
		cc, ok := ct.columns[colName]
		claims := claimed(c)
		if !claims["name"] {
			allFKsKnown = false
			continue
		}
		s.columns++
		s.check(ok, "%s.%s: column is not in the catalog", id, colName)
		if !ok {
			continue
		}
		if claims["nullable"] {
			s.check(c["nullable"] == !cc.notNull, "%s.%s: nullable %v, catalog NOT NULL %v", id, colName, c["nullable"], cc.notNull)
		}
		if claims["type"] {
			compareType(s, run, id, colName, c, cc, enums)
		}
		for _, f := range []string{"length", "precision", "scale", "timezone"} {
			if !claims[f] {
				continue
			}
			want := postgresSizes(cc.typ)[f]
			s.check(fmt.Sprint(c[f]) == fmt.Sprint(want), "%s.%s: %s %v, catalog type %s", id, colName, f, c[f], cc.typ)
		}
		if claims["primary_key"] {
			in := false
			for _, p := range ct.pk {
				in = in || p == colName
			}
			s.check(c["primary_key"] == in, "%s.%s: primary_key %v, catalog %v", id, colName, c["primary_key"], ct.pk)
		}
		single := func(unique bool) bool {
			for _, ix := range ct.indexes {
				if ix.unique == unique && len(ix.columns) == 1 && ix.columns[0] == colName {
					return true
				}
			}
			return false
		}
		if claims["unique"] {
			s.check(c["unique"] == single(true), "%s.%s: unique %v, catalog indexes %+v", id, colName, c["unique"], ct.indexes)
		}
		if claims["index"] {
			s.check(c["index"] == single(false), "%s.%s: index %v, catalog indexes %+v", id, colName, c["index"], ct.indexes)
		}
		if run.Provider == providerSQLite && sqliteAutoincrement(ct.sql, colName) {
			// AUTOINCREMENT is a column property, not a DEFAULT: neither
			// default fact is claimed for it (decision
			// 2026-11-09).
			s.check(!claims["server_default"] && !claims["default"], "%s.%s: AUTOINCREMENT column with a default claim (%v, %v)", id, colName, c["default"], c["server_default"])
		}
		if claims["server_default"] {
			s.check(c["server_default"] == (cc.def != nil), "%s.%s: server_default %v, catalog default %v", id, colName, c["server_default"], derefAny(cc.def))
		}
		if claims["default"] {
			switch c["default"] {
			case "server":
				s.check(cc.def != nil, "%s.%s: default server, catalog none", id, colName)
			default:
				s.check(cc.def == nil, "%s.%s: default %v, catalog %v", id, colName, c["default"], derefAny(cc.def))
			}
		}
		if text, ok := c["server_default_text"].(string); ok {
			s.check(cc.def != nil && defaultTextAgrees(text, *cc.def, cc.typ), "%s.%s: server_default_text %s, catalog %v", id, colName, text, derefAny(cc.def))
		}
		if !claims["foreign_keys"] {
			allFKsKnown = false
			continue
		}
		var catFKs []catFK
		for _, fk := range ct.fks {
			for _, l := range fk.columns {
				if l == colName {
					catFKs = append(catFKs, fk)
				}
			}
		}
		fks, _ := c["foreign_keys"].([]any)
		s.check(len(fks) == len(catFKs), "%s.%s: %d foreign keys, catalog %d %+v", id, colName, len(fks), len(catFKs), catFKs)
		for _, x := range fks {
			fk, _ := x.(map[string]any)
			local := strs(fk["local_columns"])
			if local == nil {
				local = []string{colName}
			}
			ref := strs(fk["columns"])
			if ref == nil {
				ref = []string{fmt.Sprint(fk["column"])}
			}
			fkSeen[strings.Join(local, ",")] = true
			found := false
			for _, cf := range catFKs {
				if !reflect.DeepEqual(cf.columns, local) || cf.refTable != fk["table"] || !reflect.DeepEqual(cf.refColumns, ref) {
					continue
				}
				if fk["on_delete"] != cf.onDelete || (fk["on_update"] != nil && fk["on_update"] != cf.onUpdate) {
					continue
				}
				if sch, ok := fk["schema"].(string); ok && cf.refSchema != nil && sch != *cf.refSchema {
					continue
				}
				if name, ok := fk["name"].(string); ok && (cf.name == nil || *cf.name != name) {
					continue
				}
				found = true
			}
			s.check(found, "%s.%s: foreign key %v is not in the catalog %+v", id, colName, fk, catFKs)
		}
	}
	if complete && allFKsKnown {
		s.check(len(fkSeen) == len(ct.fks), "%s: %d foreign keys, catalog %d", id, len(fkSeen), len(ct.fks))
	}
}

// compareType compares the claimed type: the Postgres catalog type
// (format_type), an enum by its type name, schema and values, or the
// SQLite declared type.
func compareType(s *oracleStats, run oracleRun, id, colName string, c map[string]any, cc catColumn, enums map[string][]string) {
	sqlType := fmt.Sprint(c["sql_type"])
	en, isEnum := c["enum"].(map[string]any)
	if isEnum && run.Provider == providerPostgres {
		want := strings.TrimSuffix(sqlType, "[]")
		s.check(cc.typeName != nil && *cc.typeName == want && cc.array == strings.HasSuffix(sqlType, "[]"),
			"%s.%s: enum type %s, catalog %s", id, colName, sqlType, cc.typ)
		key := deref(cc.typeSchema) + "." + deref(cc.typeName)
		if sch, ok := en["schema"].(string); ok {
			s.check(deref(cc.typeSchema) == sch, "%s.%s: enum schema %s, catalog %s", id, colName, sch, deref(cc.typeSchema))
		}
		if vals := strs(en["values"]); vals != nil {
			s.check(reflect.DeepEqual(vals, enums[key]), "%s.%s: enum values %v, catalog %v", id, colName, vals, enums[key])
		}
		return
	}
	s.check(sqlType == cc.typ, "%s.%s: type %s, catalog %s", id, colName, sqlType, cc.typ)
}

// sqliteAutoincrement says whether a SQLite table's DDL declares the
// column AUTOINCREMENT.
func sqliteAutoincrement(ddl, column string) bool {
	re := regexp.MustCompile(`(?m)^\s*"` + regexp.QuoteMeta(strings.ReplaceAll(column, `"`, `""`)) + `"[^,\n]*\bAUTOINCREMENT\b`)
	return re.MatchString(ddl)
}

// defaultTextAgrees says whether a claimed default text is the catalog's:
// Postgres keeps a literal with its cast ('x'::text, 'user'::role_kind),
// and the cast may be the column's own type as format_type spells it,
// with or without its modifier ('it”s'::character varying on a
// character varying(20) column).
func defaultTextAgrees(text, catalog, columnType string) bool {
	if text == catalog {
		return true
	}
	rest, ok := strings.CutPrefix(catalog, text+"::")
	if !ok || rest == "" {
		return false
	}
	if base, _, cut := strings.Cut(columnType, "("); rest == columnType || cut && rest == strings.TrimSpace(base) {
		return true
	}
	return !strings.ContainsAny(rest, " ()'")
}

// TestDefaultTextAgrees covers the comparator's cast rule.
func TestDefaultTextAgrees(t *testing.T) {
	for _, tc := range []struct {
		text, catalog, typ string
		want               bool
	}{
		{"'x'", "'x'::text", "text", true},
		{"'it''s'", "'it''s'::character varying", "character varying(20)", true},
		{"'ab'", "'ab'::bpchar", "character(2)", true},
		{"'a b'", "'a b'::character varying", "character varying", true},
		{"'x'", "'x'::character varying", "text", false},
		{"'x'", "'y'::text", "text", false},
		{"42", "42", "integer", true},
	} {
		if got := defaultTextAgrees(tc.text, tc.catalog, tc.typ); got != tc.want {
			t.Errorf("%s vs %s (%s): %v, want %v", tc.text, tc.catalog, tc.typ, got, tc.want)
		}
	}
}
