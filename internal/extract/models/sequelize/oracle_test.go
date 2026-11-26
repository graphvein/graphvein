package sequelize

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
	"github.com/graphvein/graphvein/internal/extract/models"
)

// The dump scripts/oracle/sequelize_catalog.sh writes: one run
// per Sequelize instance, with the models Sequelize built, the sync steps
// (sync, or the harness's replay of Sequelize's own steps with every
// failure) and the catalog of the scratch database after sync (postgres:
// pg_catalog; sqlite: sqlite_master and the pragmas).
type sqOracleDump struct {
	Runs []sqOracleRun `json:"runs"`
}

type sqOracleRun struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Group     string `json:"group"`
	Dialect   string `json:"dialect"`
	Sequelize string `json:"sequelize"`
	LoadError string `json:"load_error"`
	Harness   struct {
		DialectSupplied bool     `json:"dialect_supplied"`
		SchemasCreated  []string `json:"schemas_created"`
		Stubs           []string `json:"stubs"`
	} `json:"harness"`
	Sync struct {
		Path     string `json:"path"`
		Failures []struct {
			Step, Model, Table, Error string
		} `json:"failures"`
	} `json:"sync"`
	Catalog *sqCatalog `json:"catalog"`
}

type sqCatalog struct {
	Tables []sqTable `json:"tables"`
	Enums  []struct {
		Schema string   `json:"schema"`
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"enums"`
}

type sqTable struct {
	Schema  *string `json:"schema"`
	Name    string  `json:"name"`
	Columns []struct {
		Name    string  `json:"name"`
		Type    string  `json:"type"`
		NotNull bool    `json:"not_null"`
		Default *string `json:"default"`
	} `json:"columns"`
	Constraints []struct {
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Columns    []string `json:"columns"`
		RefSchema  *string  `json:"ref_schema"`
		RefTable   *string  `json:"ref_table"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
	} `json:"constraints"`
	Indexes []struct {
		Name       string   `json:"name"`
		Unique     bool     `json:"unique"`
		Primary    bool     `json:"primary"`
		Constraint bool     `json:"constraint"`
		Columns    []string `json:"columns"`
		Method     string   `json:"method"`
		Partial    bool     `json:"partial"`
		Exprs      bool     `json:"expressions"`
		Origin     string   `json:"origin"`
	} `json:"indexes"`
	PrimaryKey  []string `json:"primary_key"`
	ForeignKeys []struct {
		RefTable   string   `json:"ref_table"`
		Columns    []string `json:"columns"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
	} `json:"foreign_keys"`
}

// sqFK is a foreign key as both sides are compared: target schema, table,
// columns and the two actions.
type sqFK struct{ Schema, Table, Columns, OnDelete, OnUpdate string }

var pgActions = map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}

// catalogView is one catalog table reduced to the claim facts.
type catalogView struct {
	cols    map[string]int
	order   []string
	pk      []string
	uniques [][]string // unique constraints (pg contype u; sqlite origin u), duplicates removed
	// uniqueCols and indexCols are the columns that are the only column
	// of a non-partial unique index (constraint or not, never the primary
	// key) and of a non-unique index.
	uniqueCols, indexCols map[string]bool
	// indexes are the indexes that are not constraints (pg) or that
	// CREATE INDEX made (sqlite origin c).
	indexes map[string]catIndex
	fks     map[string][]sqFK
	checks  int
}

type catIndex struct {
	columns         []string
	unique, partial bool
	method          string
}

func viewOf(dialect string, ct *sqTable) catalogView {
	v := catalogView{cols: map[string]int{}, uniqueCols: map[string]bool{}, indexCols: map[string]bool{}, indexes: map[string]catIndex{}, fks: map[string][]sqFK{}}
	for i, c := range ct.Columns {
		v.cols[c.Name] = i
		v.order = append(v.order, c.Name)
	}
	seen := map[string]bool{}
	addUnique := func(cols []string) {
		k := strings.Join(cols, ",")
		if !seen[k] {
			seen[k] = true
			v.uniques = append(v.uniques, cols)
		}
	}
	if dialect == "postgres" {
		for _, c := range ct.Constraints {
			switch c.Type {
			case "p":
				v.pk = c.Columns
			case "u":
				addUnique(c.Columns)
			case "f":
				if len(c.Columns) == 1 {
					v.fks[c.Columns[0]] = append(v.fks[c.Columns[0]], sqFK{deref(c.RefSchema), deref(c.RefTable), strings.Join(c.RefColumns, ","), pgActions[c.OnDelete], pgActions[c.OnUpdate]})
				}
			case "c":
				v.checks++
			}
		}
		for _, ix := range ct.Indexes {
			if ix.Primary || ix.Exprs {
				continue
			}
			if len(ix.Columns) == 1 && ix.Unique && !ix.Partial {
				v.uniqueCols[ix.Columns[0]] = true
			}
			if len(ix.Columns) == 1 && !ix.Unique {
				v.indexCols[ix.Columns[0]] = true
			}
			if !ix.Constraint {
				v.indexes[ix.Name] = catIndex{ix.Columns, ix.Unique, ix.Partial, ix.Method}
			}
		}
		return v
	}
	v.pk = ct.PrimaryKey
	for _, ix := range ct.Indexes {
		if ix.Origin == "pk" {
			continue
		}
		if ix.Origin == "u" {
			addUnique(ix.Columns)
		}
		if len(ix.Columns) == 1 && ix.Unique && !ix.Partial {
			v.uniqueCols[ix.Columns[0]] = true
		}
		if len(ix.Columns) == 1 && !ix.Unique {
			v.indexCols[ix.Columns[0]] = true
		}
		if ix.Origin == "c" {
			v.indexes[ix.Name] = catIndex{ix.Columns, ix.Unique, ix.Partial, ""}
		}
	}
	for _, fk := range ct.ForeignKeys {
		if len(fk.Columns) == 1 {
			v.fks[fk.Columns[0]] = append(v.fks[fk.Columns[0]], sqFK{"", fk.RefTable, strings.Join(fk.RefColumns, ","), fk.OnDelete, fk.OnUpdate})
		}
	}
	return v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pgCast matches a trailing cast Postgres adds to a rendered default.
var pgCast = regexp.MustCompile(`::[a-z_ "A-Z]+(\([0-9, ]+\))?(\[\])?$`)

// normDefault strips the catalog's casts and one level of quotes (task
// file, "Oracle design": defaults are compared after stripping the
// catalog's casts and quotes); ok is false for no default, and for
// DEFAULT NULL, which equals no default.
func normDefault(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	v := *s
	for {
		w := pgCast.ReplaceAllString(v, "")
		if strings.HasPrefix(w, "(") && strings.HasSuffix(w, ")") && !strings.Contains(w[1:len(w)-1], "(") {
			w = w[1 : len(w)-1]
		}
		if w == v {
			break
		}
		v = w
	}
	if strings.EqualFold(v, "NULL") {
		return "", false
	}
	return unquote(v), true
}

// sameDefault compares a claimed default text with the catalog's after
// stripping; on a jsonb column both are compared as JSON values, because
// Postgres stores jsonb in its own canonical form ('{"a":1}' reads back
// as '{"a": 1}').
func sameDefault(typ, claimed, catalog string) bool {
	if claimed == catalog {
		return true
	}
	if typ != "jsonb" {
		return false
	}
	var a, b any
	return json.Unmarshal([]byte(claimed), &a) == nil && json.Unmarshal([]byte(catalog), &b) == nil && reflect.DeepEqual(a, b)
}

func unquote(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// sqComparison holds one run's comparison.
type sqComparison struct {
	t             *testing.T
	run           sqOracleRun
	forms         map[string]map[string]string
	compared      map[string]bool
	columnsSeen   map[string]bool
	tables        int
	columns       int
	facts         int
	unknown       int
	disagreements []string
}

func (c *sqComparison) check(ok bool, format string, a ...any) {
	c.facts++
	if !ok {
		c.disagreements = append(c.disagreements, fmt.Sprintf(format, a...))
	}
}

// TestSequelizeOracle compares every claimed fact of the linked repository
// with the catalog Sequelize's own sync left in a scratch database:
// per table its existence, the column set when complete, the
// primary key, the unique constraints (per column and per column set,
// duplicates such as the cyclic path's `_key1` counted once), the indexes
// and check constraints; per read column its type, nullability, primary
// key membership, single-column uniqueness and index, default (presence,
// and the text when one is claimed, after stripping the catalog's casts
// and quotes; DEFAULT NULL equals no default), enum labels and its
// foreign keys (target, columns and actions). A claimed fact the catalog
// lacks or contradicts is a disagreement (precision stop S1). Over the
// fixture's runs every oracle form of TestSequelizeClaims (oracleForms)
// must be compared, and the attribute helper columns (row SQ-A1, form
// "helper member") too. Set GRAPHVEIN_SEQUELIZE_ORACLE to the dump and
// GRAPHVEIN_SEQUELIZE_ORACLE_ROOT to the directory the runs' repo paths
// are relative to (the fixture directory, or the .corpus directory).
func TestSequelizeOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_SEQUELIZE_ORACLE"), os.Getenv("GRAPHVEIN_SEQUELIZE_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_SEQUELIZE_ORACLE or GRAPHVEIN_SEQUELIZE_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump sqOracleDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Runs) == 0 {
		t.Fatal("no runs in the dump")
	}
	type linked struct {
		nodes []contract.Node
		forms map[string]map[string]string
	}
	repos := map[string]linked{}
	compared := map[string]bool{}
	columnsSeen := map[string]bool{}
	fixture := true
	for _, run := range dump.Runs {
		fixture = fixture && run.Repo == "."
		t.Run(run.Name, func(t *testing.T) {
			l, ok := repos[run.Repo]
			if !ok {
				forms := map[string]map[string]string{}
				claimHook = func(col *models.Column, fact, row, form string) {
					k := fmt.Sprintf("%s:%d:%s", col.File, col.Line, col.Attribute)
					if forms[k] == nil {
						forms[k] = map[string]string{}
					}
					forms[k][fact] = form
				}
				nodes, _, _ := Link(corpusInput(t, filepath.Join(root, run.Repo)))
				claimHook = nil
				l = linked{nodes, forms}
				repos[run.Repo] = l
			}
			c := &sqComparison{t: t, run: run, forms: l.forms, compared: compared, columnsSeen: columnsSeen}
			c.compareRun(l.nodes)
			t.Logf("%s (%s, sync %s, %d sync failures%s): %d tables, %d read columns, %d facts compared, %d disagreements, %d facts unknown",
				run.Name, run.Dialect, run.Sync.Path, len(run.Sync.Failures), loadNote(run), c.tables, c.columns, c.facts, len(c.disagreements), c.unknown)
			for _, d := range c.disagreements {
				t.Errorf("disagreement: %s", d)
			}
		})
	}
	if !fixture {
		return
	}
	for fact, list := range oracleForms {
		for _, form := range list {
			ok := false
			for k := range compared {
				ok = ok || k == fact+"/"+form || strings.HasSuffix(form, " on *") && strings.HasPrefix(k, fact+"/"+strings.TrimSuffix(form, "*"))
			}
			if !ok {
				t.Errorf("oracle form %s/%s is not compared by any fixture run", fact, form)
			}
		}
	}
	for _, id := range helperColumns {
		if !columnsSeen[id] {
			t.Errorf("helper column %s (row SQ-A1, form \"helper member\") is not compared", id)
		}
	}
}

// helperColumns are the fixture's read attribute helper columns (row SQ-A1,
// form "helper member"; open point O4), which the oracle must compare.
var helperColumns = []string{
	"column:pg/src/models/Wallet.ts#Wallet.currency",
	"column:pg/src/models/Wallet.ts#Wallet.country",
	"column:pg/src/models/Wallet.ts#Wallet.balance",
	"column:pg/src/models/Wallet.ts#Wallet.fee",
}

func loadNote(run sqOracleRun) string {
	if run.LoadError == "" {
		return ""
	}
	return ", load error by design"
}

func attrs(t *testing.T, n contract.Node) map[string]any {
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

func strList(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, x := range list {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

func (c *sqComparison) compareRun(nodes []contract.Node) {
	t := c.t
	set := "models/sequelize/" + c.run.Group
	tables := map[string]map[string]any{}
	var ids []string
	columns := map[string][]map[string]any{}
	colIDs := map[string][]string{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			a := attrs(t, n)
			if a["set"] == set {
				tables[n.Identity] = a
				ids = append(ids, n.Identity)
			}
		case "column":
			tid := "table:" + n.File + "#" + strings.SplitN(n.QualifiedName, ".", 2)[0]
			columns[tid] = append(columns[tid], attrs(t, n))
			colIDs[tid] = append(colIDs[tid], n.Identity)
		}
	}
	if len(ids) == 0 {
		t.Fatalf("no table records in set %s", set)
	}
	sort.Strings(ids)
	if c.run.Catalog == nil {
		t.Fatal("the run has no catalog")
	}
	cat := map[string]*sqTable{}
	for i := range c.run.Catalog.Tables {
		ct := &c.run.Catalog.Tables[i]
		cat[c.key(deref(ct.Schema), ct.Name)] = ct
	}
	enums := map[string][]string{}
	for _, e := range c.run.Catalog.Enums {
		enums[e.Schema+"."+e.Name] = e.Values
	}
	matched := map[string]bool{}
	for _, id := range ids {
		a := tables[id]
		c.tables++
		if a["verification"] != models.VerificationOracle || a["dialect"] != c.run.Dialect {
			t.Fatalf("%s: verification %v on %v, the run is %s", id, a["verification"], a["dialect"], c.run.Dialect)
		}
		if pins, _ := a["pins"].(map[string]any); pins["sequelize"] != c.run.Sequelize {
			t.Fatalf("%s: pins %v, the run loaded sequelize %s", id, pins, c.run.Sequelize)
		}
		schema, _ := a["schema"].(string)
		name := fmt.Sprint(a["name"])
		key := c.key(schema, name)
		ct := cat[key]
		c.check(ct != nil, "%s: table %s is not in the catalog", id, key)
		if ct == nil {
			continue
		}
		matched[key] = true
		v := viewOf(c.run.Dialect, ct)
		cols := columns[id]
		if a["column_set"] == "complete" {
			var names []string
			for _, col := range cols {
				if col["state"] == "read" {
					names = append(names, fmt.Sprint(col["name"]))
				}
			}
			c.check(reflect.DeepEqual(sortedCopy(names), sortedCopy(v.order)), "%s: columns %v, catalog %v", id, sortedCopy(names), sortedCopy(v.order))
		}
		if a["primary_key"] != nil {
			pk := strList(a["primary_key"])
			c.check(reflect.DeepEqual(sortedCopy(pk), sortedCopy(v.pk)), "%s: primary key %v, catalog %v", id, pk, v.pk)
		}
		c.compareUniques(id, a, v)
		c.compareIndexes(id, a, v)
		if a["constraint_set"] == "complete" && c.run.Dialect == "postgres" {
			c.check(len(strList(a["check_constraints"])) == v.checks, "%s: %d check constraints, catalog %d", id, len(strList(a["check_constraints"])), v.checks)
		}
		for i, col := range cols {
			switch col["state"] {
			case "read":
				c.compareColumn(id, colIDs[id][i], col, ct, v, enums)
			default:
				c.unknown++
			}
			c.unknown += countUnknown(col)
		}
	}
	for key := range cat {
		if !matched[key] {
			t.Logf("catalog table %s has no claimed record", key)
		}
	}
}

func (c *sqComparison) key(schema, name string) string {
	if c.run.Dialect != "postgres" {
		return name
	}
	if schema == "" {
		schema = "public"
	}
	return schema + "." + name
}

// countUnknown counts the facts a read column leaves unknown (its reasons
// that name a fact).
func countUnknown(col map[string]any) int {
	n := 0
	list, _ := col["reasons"].([]any)
	for _, r := range list {
		if m, ok := r.(map[string]any); ok && m["fact"] != nil {
			n++
		}
	}
	return n
}

func (c *sqComparison) compareUniques(id string, a map[string]any, v catalogView) {
	list, _ := a["unique_constraints"].([]any)
	claimed := map[string]bool{}
	for _, x := range list {
		m, _ := x.(map[string]any)
		cols := strList(m["columns"])
		k := strings.Join(cols, ",")
		claimed[k] = true
		found := false
		for _, u := range v.uniques {
			found = found || strings.Join(u, ",") == k
		}
		c.check(found, "%s: unique (%s) is not a unique constraint of the catalog %v", id, k, v.uniques)
	}
	if a["constraint_set"] != "complete" {
		return
	}
	for _, u := range v.uniques {
		k := strings.Join(u, ",")
		c.check(claimed[k], "%s: the catalog's unique (%s) is not claimed, the constraint set is complete", id, k)
	}
}

func (c *sqComparison) compareIndexes(id string, a map[string]any, v catalogView) {
	list, _ := a["indexes"].([]any)
	claimed := map[string]bool{}
	for _, x := range list {
		m, _ := x.(map[string]any)
		name := fmt.Sprint(m["name"])
		claimed[name] = true
		ix, ok := v.indexes[name]
		c.check(ok, "%s: index %s is not in the catalog", id, name)
		if !ok {
			continue
		}
		cols := strList(m["columns"])
		c.check(reflect.DeepEqual(cols, ix.columns), "%s: index %s columns %v, catalog %v", id, name, cols, ix.columns)
		c.check(m["unique"] == ix.unique, "%s: index %s unique %v, catalog %v", id, name, m["unique"], ix.unique)
		c.check(m["partial"] == ix.partial, "%s: index %s partial %v, catalog %v", id, name, m["partial"], ix.partial)
		if c.run.Dialect == "postgres" {
			c.check(m["method"] == ix.method, "%s: index %s method %v, catalog %s", id, name, m["method"], ix.method)
		}
	}
	if a["constraint_set"] != "complete" {
		return
	}
	for name := range v.indexes {
		c.check(claimed[name], "%s: the catalog's index %s is not claimed, the constraint set is complete", id, name)
	}
}

// claimed returns the facts a column claims.
func claimed(col map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := col["claims"].([]any)
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out[fmt.Sprint(m["fact"])] = true
		}
	}
	return out
}

func (c *sqComparison) compareColumn(tableID, colID string, col map[string]any, ct *sqTable, v catalogView, enums map[string][]string) {
	c.columns++
	c.columnsSeen[colID] = true
	name := fmt.Sprint(col["name"])
	i, ok := v.cols[name]
	c.check(ok, "%s: column %s is not in the catalog", tableID, name)
	if !ok {
		return
	}
	cc := ct.Columns[i]
	facts := claimed(col)
	def, _ := col["defined_in"].(map[string]any)
	forms := c.forms[fmt.Sprintf("%v:%v:%v", def["file"], def["line"], col["attribute"])]
	mark := func(fact string) {
		if f, ok := forms[fact]; ok {
			c.compared[fact+"/"+f] = true
		}
	}
	if facts["type"] {
		c.check(fmt.Sprint(col["type"]) == cc.Type, "%s: type %v, catalog %s", colID, col["type"], cc.Type)
		mark("type")
		if e, ok := col["enum"].(map[string]any); ok && e["values"] != nil && c.run.Dialect == "postgres" {
			got := strList(e["values"])
			want := enums[fmt.Sprint(e["schema"])+"."+fmt.Sprint(e["name"])]
			c.check(reflect.DeepEqual(got, want), "%s: enum %v labels %v, catalog %v", colID, e["name"], got, want)
		}
	}
	if facts["nullable"] {
		c.check(col["nullable"] == !cc.NotNull, "%s: nullable %v, catalog not null %v", colID, col["nullable"], cc.NotNull)
		mark("nullable")
	}
	if facts["primary_key"] {
		in := false
		for _, p := range v.pk {
			in = in || p == name
		}
		c.check(col["primary_key"] == in, "%s: primary key %v, catalog %v", colID, col["primary_key"], in)
	}
	if facts["unique"] {
		c.check(col["unique"] == v.uniqueCols[name], "%s: unique %v, catalog %v (per column)", colID, col["unique"], v.uniqueCols[name])
	}
	if facts["index"] {
		c.check(col["index"] == v.indexCols[name], "%s: index %v, catalog %v", colID, col["index"], v.indexCols[name])
	}
	text, present := normDefault(cc.Default)
	if facts["server_default"] {
		c.check(col["server_default"] == present, "%s: server default %v, catalog %v", colID, col["server_default"], cc.Default)
		mark("server_default")
	}
	if facts["default"] {
		switch col["default"] {
		case "server":
			c.check(present, "%s: default server, catalog none", colID)
		case "absent", "client":
			c.check(!present, "%s: default %v, catalog %v", colID, col["default"], deref(cc.Default))
		}
		if s, ok := col["server_default_text"].(string); ok {
			c.check(sameDefault(cc.Type, unquote(s), text), "%s: default text %s, catalog %v (%q after stripping)", colID, s, deref(cc.Default), text)
		}
		mark("default")
	}
	if facts["foreign_keys"] {
		var got []sqFK
		list, _ := col["foreign_keys"].([]any)
		for _, x := range list {
			m, _ := x.(map[string]any)
			schema, _ := m["schema"].(string)
			if c.run.Dialect == "postgres" && schema == "" {
				schema = "public"
			}
			if c.run.Dialect != "postgres" {
				schema = ""
			}
			got = append(got, sqFK{schema, fmt.Sprint(m["table"]), strings.Join(strList(m["columns"]), ","), fmt.Sprint(m["on_delete"]), fmt.Sprint(m["on_update"])})
		}
		want := v.fks[name]
		key := func(l []sqFK) []string {
			out := []string{}
			for _, f := range l {
				out = append(out, fmt.Sprintf("%s.%s(%s) %s/%s", f.Schema, f.Table, f.Columns, f.OnDelete, f.OnUpdate))
			}
			sort.Strings(out)
			return out
		}
		c.check(reflect.DeepEqual(key(got), key(want)), "%s: foreign keys %v, catalog %v", colID, key(got), key(want))
	}
}
