package drizzle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// The dump scripts/oracle/drizzle_catalog.sh writes: one run per config
// and pin, with drizzle-kit's SQL and the catalog of the database it
// created (postgresql: the pg_catalog; sqlite: sqlite_master and the
// pragmas; mysql: none, the SQL is the mapped reference), and the claimed
// expression texts put through the run's database (normalized, keyed by
// oracleRequest.key).
type oracleDump struct {
	Runs []oracleRun `json:"runs"`
}

type oracleRun struct {
	Dir        string             `json:"dir"`
	Config     string             `json:"config"`
	Dialect    string             `json:"dialect"`
	Kit        string             `json:"drizzle_kit"`
	ORM        string             `json:"drizzle_orm"`
	Command    string             `json:"command"`
	Dropped    []string           `json:"dropped"`
	SQL        string             `json:"sql"`
	Catalog    *oracleCatalog     `json:"catalog"`
	Normalized map[string]*string `json:"normalized"`
}

type oracleCatalog struct {
	Tables []oracleTable `json:"tables"`
	Enums  []struct {
		Schema string   `json:"schema"`
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"enums"`
}

type oracleColumn struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	NotNull   bool    `json:"not_null"`
	Default   *string `json:"default"`
	Generated *string `json:"generated"`
	Identity  string  `json:"identity"`
}

type oracleConstraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Columns    []string `json:"columns"`
	RefSchema  *string  `json:"ref_schema"`
	RefTable   *string  `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	OnDelete   string   `json:"on_delete"`
	OnUpdate   string   `json:"on_update"`
	Index      *string  `json:"index"`
	Definition string   `json:"definition"`
}

type oracleIndex struct {
	Name             string    `json:"name"`
	Unique           bool      `json:"unique"`
	Primary          bool      `json:"primary"`
	NullsNotDistinct bool      `json:"nulls_not_distinct"`
	Columns          []*string `json:"columns"`
	Keys             []string  `json:"keys"`
	Options          []int     `json:"options"`
	Opclasses        []struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
	} `json:"opclasses"`
	Method     string   `json:"method"`
	Partial    bool     `json:"partial"`
	Exprs      bool     `json:"expressions"`
	Where      *string  `json:"where"`
	Reloptions []string `json:"reloptions"`
	// SQLite only.
	Origin string  `json:"origin"`
	SQL    *string `json:"sql"`
}

type oracleTable struct {
	Schema      *string            `json:"schema"`
	Name        string             `json:"name"`
	SQL         string             `json:"sql"`
	Columns     []oracleColumn     `json:"columns"`
	Constraints []oracleConstraint `json:"constraints"`
	Indexes     []oracleIndex      `json:"indexes"`
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

// oracleRequest is a claimed expression text the harness puts through the
// run's database (scripts/oracle/drizzle_catalog.mjs normalize-sql).
type oracleRequest struct {
	Run    int    `json:"run"`
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Schema string `json:"schema,omitempty"`
	Table  string `json:"table"`
	Column string `json:"column,omitempty"`
	Type   string `json:"type,omitempty"`
	Text   string `json:"text"`
}

// Referential actions as the records write them: pg_constraint codes and
// SQLite's pragma text.
var pgActions = map[string]string{"a": "no action", "r": "restrict", "c": "cascade", "n": "set null", "d": "set default"}

// oracleComparison compares one run's claimed facts with its catalog.
type oracleComparison struct {
	t     *testing.T
	run   oracleRun
	index int
	// requests collects the expression texts to normalise (request mode);
	// nil when comparing.
	requests *[]oracleRequest
	facts    int
	tables   int
	columns  int
	// disagreements are the claimed facts the catalog lacks or contradicts.
	disagreements []string
	// compared are the claim forms compared (oracleForms keys).
	compared map[string]bool
}

func (c *oracleComparison) check(ok bool, format string, a ...any) {
	c.facts++
	if !ok {
		c.disagreements = append(c.disagreements, fmt.Sprintf(format, a...))
	}
}

// normalized returns the claimed text put through the run's database. In
// request mode it records the request and reports false; the caller then
// skips the comparison.
func (c *oracleComparison) normalized(kind, schema, table, column, typ, text string) (string, bool) {
	key := strings.Join([]string{kind, schema, table, column, typ, text}, "\x1f")
	if c.requests != nil {
		*c.requests = append(*c.requests, oracleRequest{Run: c.index, Key: key, Kind: kind, Schema: schema, Table: table, Column: column, Type: typ, Text: text})
		return "", false
	}
	v, ok := c.run.Normalized[key]
	if !ok || v == nil {
		c.check(false, "%s %s.%s %q: Postgres did not take the claimed text (or it was not normalised)", kind, table, column, text)
		return "", false
	}
	return *v, true
}

// TestDrizzleOracle compares every claimed table, column, type,
// nullability, default, primary key, unique constraint, index, check,
// enum and foreign key of the runs in the dump GRAPHVEIN_DRIZZLE_ORACLE
// names with the catalog drizzle-kit's own SQL created (step G2B5;
// precision stop S1 on any disagreement); MySQL claims with the SQL text
// drizzle-kit printed (the mapped reference). GRAPHVEIN_DRIZZLE_ORACLE_ROOT
// is the directory the runs are relative to (the fixture directory, or the
// directory holding the corpus checkouts). With
// GRAPHVEIN_DRIZZLE_ORACLE_REQUESTS set the test only writes the claimed
// expression texts to that file for the harness to normalise. Skipped
// without the dump.
func TestDrizzleOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_DRIZZLE_ORACLE"), os.Getenv("GRAPHVEIN_DRIZZLE_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_DRIZZLE_ORACLE or GRAPHVEIN_DRIZZLE_ORACLE_ROOT is not set")
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
	requestsPath := os.Getenv("GRAPHVEIN_DRIZZLE_ORACLE_REQUESTS")
	var requests *[]oracleRequest
	if requestsPath != "" {
		requests = &[]oracleRequest{}
	}
	linked := map[string][]contract.Node{}
	compared := map[string]bool{}
	fixture := true
	for i, run := range dump.Runs {
		fixture = fixture && run.Dir == "."
		name := fmt.Sprintf("%s/%s@%s", run.Dir, run.Config, run.Kit)
		t.Run(name, func(t *testing.T) {
			nodes, ok := linked[run.Dir]
			if !ok {
				nodes, _, _ = Link(corpusInput(t, filepath.Join(root, run.Dir)))
				linked[run.Dir] = nodes
			}
			c := &oracleComparison{t: t, run: run, index: i, requests: requests, compared: compared}
			c.compareRun(nodes)
			if requests != nil {
				return
			}
			t.Logf("%s (%s, %s): %d tables, %d columns, %d facts compared, %d disagreements, %d forms dropped",
				name, run.Dialect, run.Command, c.tables, c.columns, c.facts, len(c.disagreements), len(run.Dropped))
			for _, d := range c.disagreements {
				t.Errorf("disagreement: %s", d)
			}
		})
	}
	if requests != nil {
		out, err := json.MarshalIndent(*requests, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(requestsPath, out, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%d expression texts listed in %s", len(*requests), requestsPath)
		return
	}
	// A fact is oracle only for a form the oracle compared: over the
	// fixture's runs every form in oracleForms is compared (forms dropped
	// from a run must be compared elsewhere).
	if fixture {
		for _, f := range oracleForms {
			if !compared[f] {
				t.Errorf("oracle form %s is not compared by any run", f)
			}
		}
	} else {
		var extra []string
		for f := range compared {
			if !slices.Contains(oracleForms, f) {
				extra = append(extra, f)
			}
		}
		sort.Strings(extra)
		for _, f := range extra {
			t.Logf("form compared on the corpus only (not in the fixture): %s", f)
		}
	}
}

// identityKey holds a column record's node identity in the comparison
// (the record's own "identity" attribute is the identity column kind).
const identityKey = "\x00identity"

// dropped splits the run's dropped forms into table and column
// identities.
func (c *oracleComparison) dropped() (tables, columns map[string]bool) {
	tables, columns = map[string]bool{}, map[string]bool{}
	for _, d := range c.run.Dropped {
		i := strings.LastIndex(d, ":")
		file, decl := d[:i], d[i+1:]
		if strings.Contains(decl, ".") {
			columns["column:"+file+"#"+decl] = true
			tables["partial:table:"+file+"#"+decl[:strings.Index(decl, ".")]] = true
		} else {
			tables["table:"+file+"#"+decl] = true
		}
	}
	return tables, columns
}

func (c *oracleComparison) compareRun(nodes []contract.Node) {
	t := c.t
	set := "models/drizzle/" + c.run.Config
	droppedTables, droppedColumns := c.dropped()
	tables := map[string]map[string]any{}
	columns := map[string][]map[string]any{}
	ids := []string{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			a := record(t, n)
			if a["set"] == set {
				tables[n.Identity] = a
				ids = append(ids, n.Identity)
			}
		case "column":
			table := "table:" + n.Identity[len("column:"):strings.LastIndex(n.Identity, ".")]
			a := record(t, n)
			a[identityKey] = n.Identity
			columns[table] = append(columns[table], a)
		}
	}
	if len(tables) == 0 {
		t.Fatalf("no table records in set %s", set)
	}
	sort.Strings(ids)
	cat := map[string]*oracleTable{}
	if c.run.Catalog != nil {
		for i := range c.run.Catalog.Tables {
			ct := &c.run.Catalog.Tables[i]
			cat[deref(ct.Schema)+"."+ct.Name] = ct
		}
	}
	matched := map[string]bool{}
	for _, id := range ids {
		a := tables[id]
		if droppedTables[id] {
			c.check(false, "%s: a dropped table is claimed", id)
			continue
		}
		pins, _ := a["pins"].(map[string]any)
		if pins["drizzle-kit"] != c.run.Kit || pins["drizzle-orm"] != c.run.ORM {
			t.Fatalf("%s: pins %v, run drizzle-kit %s drizzle-orm %s", id, pins, c.run.Kit, c.run.ORM)
		}
		if a["dialect"] != c.run.Dialect {
			t.Fatalf("%s: dialect %v, run %s", id, a["dialect"], c.run.Dialect)
		}
		var cols []map[string]any
		for _, col := range columns[id] {
			if !droppedColumns[col[identityKey].(string)] {
				cols = append(cols, col)
			}
		}
		partialCopy := droppedTables["partial:"+id]
		c.tables++
		if c.run.Dialect == "mysql" {
			c.compareMySQL(id, a, cols)
			continue
		}
		want := wantVerification(c.run.Dialect)
		c.check(a["verification"] == want, "%s: verification %v, want %s", id, a["verification"], want)
		key := "." + fmt.Sprint(a["name"])
		if c.run.Dialect == "postgresql" {
			key = fmt.Sprint(a["schema"]) + key
		}
		ct := cat[key]
		c.check(ct != nil, "%s: table %s is not in the catalog", id, key)
		if ct == nil {
			continue
		}
		matched[key] = true
		if a["column_set"] == "complete" && !partialCopy {
			var names, catNames []string
			for _, col := range cols {
				names = append(names, fmt.Sprint(col["name"]))
			}
			for _, cc := range ct.Columns {
				catNames = append(catNames, cc.Name)
			}
			sort.Strings(names)
			sort.Strings(catNames)
			c.check(reflect.DeepEqual(names, catNames), "%s: columns %v, catalog %v", id, names, catNames)
		}
		if c.run.Dialect == "postgresql" {
			c.comparePgTable(id, a, cols, ct)
		} else {
			c.compareSQLiteTable(id, a, cols, ct)
		}
		for _, f := range oracleTableForms(c.run.Dialect, a) {
			c.form(f)
		}
	}
	for key := range cat {
		if !matched[key] {
			t.Logf("catalog table %s has no claimed record", key)
		}
	}
}

// wantVerification is the verification a dialect's facts carry.
func wantVerification(dialect string) string {
	if dialect == "mysql" {
		return "mapped"
	}
	return "oracle"
}

func (c *oracleComparison) form(f string) {
	c.compared[f] = true
}

// claimSet returns the claim facts of a column with their rows.
func claimSet(col map[string]any) map[string]string {
	out := map[string]string{}
	list, _ := col["claims"].([]any)
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out[fmt.Sprint(m["fact"])] = fmt.Sprint(m["row"])
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(list))
	for i, x := range list {
		if x == nil {
			out[i] = "<nil>"
			continue
		}
		out[i] = fmt.Sprint(x)
	}
	return out
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

func (c *oracleComparison) comparePgTable(id string, a map[string]any, cols []map[string]any, ct *oracleTable) {
	schema, table := fmt.Sprint(a["schema"]), fmt.Sprint(a["name"])
	complete := a["constraint_set"] == "complete"
	var pk *oracleConstraint
	uniques := map[string]oracleConstraint{}
	checks := map[string]oracleConstraint{}
	var fks []oracleConstraint
	backing := map[string]bool{}
	for _, con := range ct.Constraints {
		switch con.Type {
		case "p":
			con := con
			pk = &con
			backing[deref(con.Index)] = true
		case "u":
			uniques[con.Name] = con
			backing[deref(con.Index)] = true
		case "c":
			checks[con.Name] = con
		case "f":
			fks = append(fks, con)
		}
	}
	indexes := map[string]oracleIndex{}
	nnd := map[string]bool{}
	for _, ix := range ct.Indexes {
		if backing[ix.Name] {
			nnd[ix.Name] = ix.NullsNotDistinct
			continue
		}
		indexes[ix.Name] = ix
	}
	if list, ok := a["primary_key"].([]any); ok {
		got := strs(list)
		var want []string
		if pk != nil {
			want = pk.Columns
		}
		c.check(slices.Equal(got, want), "%s: primary key %v, catalog %v", id, got, want)
	}
	if name, ok := a["primary_key_name"].(string); ok {
		c.check(pk != nil && pk.Name == name, "%s: primary key name %s, catalog %v", id, name, pk)
	}
	if list, ok := a["unique_constraints"].([]any); ok {
		for _, x := range list {
			u, _ := x.(map[string]any)
			name, named := u["name"].(string)
			con, found := uniques[name]
			if !named {
				// A generated name drizzle-kit takes from the first table
				// that built a shared builder is unknown: matched by columns.
				for _, cand := range uniques {
					if slices.Equal(cand.Columns, strs(u["columns"])) {
						con, found = cand, true
					}
				}
			}
			c.check(found && slices.Equal(con.Columns, strs(u["columns"])), "%s: unique constraint %v, catalog %v", id, u, con)
			if v, ok := u["nulls_not_distinct"].(bool); ok {
				c.check(found && nnd[deref(con.Index)] == v, "%s: unique %s nulls_not_distinct %v, catalog %v", id, name, v, nnd[deref(con.Index)])
			}
		}
		if complete {
			c.check(len(list) == len(uniques), "%s: %d unique constraints, catalog %d", id, len(list), len(uniques))
		}
	}
	if list, ok := a["indexes"].([]any); ok {
		for _, x := range list {
			ix, _ := x.(map[string]any)
			c.comparePgIndex(id, schema, table, ix, indexes, cols)
		}
		if complete {
			c.check(len(list) == len(indexes), "%s: %d indexes, catalog %d", id, len(list), len(indexes))
		}
	}
	if list, ok := a["check_constraints"].([]any); ok {
		for _, x := range list {
			ck, _ := x.(map[string]any)
			name := fmt.Sprint(ck["name"])
			con, found := checks[name]
			c.check(found, "%s: check %s is not in the catalog", id, name)
			if text, ok := ck["expression"].(string); ok && found {
				if norm, ok := c.normalized("check", schema, table, "", "", text); ok {
					c.check(norm == con.Definition, "%s: check %s %s (normalised %s), catalog %s", id, name, text, norm, con.Definition)
				}
			}
		}
		if complete {
			c.check(len(list) == len(checks), "%s: %d checks, catalog %d", id, len(list), len(checks))
		}
	}
	catCols := map[string]oracleColumn{}
	for _, cc := range ct.Columns {
		catCols[cc.Name] = cc
	}
	enums := map[string][]string{}
	for _, e := range c.run.Catalog.Enums {
		enums[e.Schema+"."+e.Name] = e.Values
	}
	pkCols := []string{}
	if pk != nil {
		pkCols = pk.Columns
	}
	for _, col := range cols {
		claims := claimSet(col)
		if claims["name"] == "" {
			continue
		}
		colName := fmt.Sprint(col["name"])
		cc, ok := catCols[colName]
		c.columns++
		c.check(ok, "%s.%s: column is not in the catalog", id, colName)
		if !ok {
			continue
		}
		c.claimForm(col, "name", claims)
		if claims["type"] != "" {
			c.check(col["type"] == cc.Type, "%s.%s: type %v, catalog %s", id, colName, col["type"], cc.Type)
			c.claimForm(col, "type", claims)
			if en, ok := col["enum"].(map[string]any); ok {
				key := fmt.Sprint(en["schema"]) + "." + fmt.Sprint(en["name"])
				vals, found := enums[key]
				c.check(found && slices.Equal(strs(en["values"]), vals), "%s.%s: enum %s %v, catalog %v", id, colName, key, en["values"], vals)
			}
		}
		if claims["nullable"] != "" {
			c.check(col["nullable"] == !cc.NotNull, "%s.%s: nullable %v, catalog NOT NULL %v", id, colName, col["nullable"], cc.NotNull)
			c.claimForm(col, "nullable", claims)
		}
		if claims["primary_key"] != "" {
			c.check(col["primary_key"] == slices.Contains(pkCols, colName), "%s.%s: primary_key %v, catalog %v", id, colName, col["primary_key"], pkCols)
			c.claimForm(col, "primary_key", claims)
		}
		if claims["unique"] != "" {
			got := false
			for _, u := range uniques {
				got = got || slices.Equal(u.Columns, []string{colName})
			}
			for _, ix := range indexes {
				got = got || ix.Unique && !ix.Partial && !ix.Exprs && len(ix.Columns) == 1 && deref(ix.Columns[0]) == colName
			}
			c.check(col["unique"] == got, "%s.%s: unique %v, catalog %v", id, colName, col["unique"], got)
			c.claimForm(col, "unique", claims)
		}
		if claims["index"] != "" {
			got := false
			for _, ix := range indexes {
				got = got || !ix.Unique && !ix.Exprs && len(ix.Columns) == 1 && deref(ix.Columns[0]) == colName
			}
			c.check(col["index"] == got, "%s.%s: index %v, catalog %v", id, colName, col["index"], got)
			c.claimForm(col, "index", claims)
		}
		if claims["default"] != "" {
			switch col["default"] {
			case "server":
				c.check(cc.Default != nil, "%s.%s: default server, catalog none", id, colName)
			case "absent":
				c.check(cc.Default == nil, "%s.%s: default absent, catalog %s", id, colName, deref(cc.Default))
			default:
				c.check(false, "%s.%s: default %v claimed", id, colName, col["default"])
			}
			c.claimForm(col, "default", claims)
		}
		if claims["server_default"] != "" {
			c.check(col["server_default"] == (cc.Default != nil), "%s.%s: server_default %v, catalog %s", id, colName, col["server_default"], deref(cc.Default))
			c.claimForm(col, "server_default", claims)
		}
		if text, ok := col["server_default_text"].(string); ok {
			if norm, ok := c.normalized("default", schema, table, colName, "", text); ok {
				c.check(cc.Default != nil && norm == *cc.Default, "%s.%s: default %s (normalised %s), catalog %s", id, colName, text, norm, deref(cc.Default))
			}
		}
		if v, ok := col["identity"].(string); ok {
			want := map[string]string{"a": "always", "d": "by default"}[cc.Identity]
			c.check(v == want, "%s.%s: identity %s, catalog %q", id, colName, v, cc.Identity)
			c.form("column/postgresql/identity/" + v)
		}
		if v, ok := col["generated"].(string); ok {
			c.check(v == "stored" && cc.Generated != nil, "%s.%s: generated %s, catalog %s", id, colName, v, deref(cc.Generated))
			if text, ok := col["generated_expression"].(string); ok {
				if norm, ok := c.normalized("generated", schema, table, colName, cc.Type, text); ok {
					c.check(cc.Generated != nil && norm == *cc.Generated, "%s.%s: generated %s (normalised %s), catalog %s", id, colName, text, norm, deref(cc.Generated))
				}
			}
			c.form("column/postgresql/generated")
		}
		if claims["foreign_keys"] != "" {
			var catFKs []fkView
			for _, fk := range fks {
				if slices.Contains(fk.Columns, colName) {
					catFKs = append(catFKs, fkView{fk.Name, fk.Columns, deref(fk.RefSchema), deref(fk.RefTable), fk.RefColumns, pgActions[fk.OnDelete], pgActions[fk.OnUpdate]})
				}
			}
			c.compareFKs(id, colName, col, catFKs)
			c.claimForm(col, "foreign_keys", claims)
		}
	}
}

// fkView is a catalog foreign key in the record's terms.
type fkView struct {
	name       string
	columns    []string
	refSchema  string
	refTable   string
	refColumns []string
	onDelete   string
	onUpdate   string
}

func (c *oracleComparison) compareFKs(id, colName string, col map[string]any, catFKs []fkView) {
	fks, _ := col["foreign_keys"].([]any)
	c.check(len(fks) == len(catFKs), "%s.%s: %d foreign keys, catalog %d %+v", id, colName, len(fks), len(catFKs), catFKs)
	for _, x := range fks {
		fk, _ := x.(map[string]any)
		found := false
		for _, cf := range catFKs {
			if !slices.Equal(cf.columns, strs(fk["local_columns"])) || cf.refTable != fk["table"] || !slices.Equal(cf.refColumns, strs(fk["columns"])) {
				continue
			}
			if fk["on_delete"] != cf.onDelete || fk["on_update"] != cf.onUpdate {
				continue
			}
			if s, ok := fk["schema"].(string); ok && s != cf.refSchema {
				continue
			}
			if name, ok := fk["name"].(string); ok && name != cf.name {
				continue
			}
			found = true
		}
		c.check(found, "%s.%s: foreign key %v is not in the catalog %+v", id, colName, fk, catFKs)
	}
}

func (c *oracleComparison) comparePgIndex(id, schema, table string, ix map[string]any, indexes map[string]oracleIndex, cols []map[string]any) {
	name := fmt.Sprint(ix["name"])
	ci, found := indexes[name]
	c.check(found, "%s: index %s is not in the catalog", id, name)
	if !found {
		return
	}
	c.check(ci.Unique == (ix["unique"] == true), "%s: index %s unique %v, catalog %v", id, name, ix["unique"], ci.Unique)
	want := strs(ix["columns"])
	got := make([]string, len(ci.Columns))
	for i, col := range ci.Columns {
		got[i] = "<nil>"
		if col != nil {
			got[i] = *col
		}
	}
	c.check(slices.Equal(want, got), "%s: index %s columns %v, catalog %v", id, name, want, got)
	if m, ok := ix["method"].(string); ok {
		c.check(m == ci.Method, "%s: index %s method %s, catalog %s", id, name, m, ci.Method)
	}
	if orders := strs(ix["orders"]); orders != nil {
		nulls := strs(ix["nulls"])
		for k, o := range orders {
			if k >= len(ci.Options) {
				c.check(false, "%s: index %s has %d options, catalog %v", id, name, len(orders), ci.Options)
				break
			}
			c.check((o == "desc") == (ci.Options[k]&1 == 1), "%s: index %s column %d order %s, catalog option %d", id, name, k, o, ci.Options[k])
			if k < len(nulls) {
				c.check((nulls[k] == "first") == (ci.Options[k]&2 == 2), "%s: index %s column %d nulls %s, catalog option %d", id, name, k, nulls[k], ci.Options[k])
			}
		}
	}
	if ops, ok := ix["opclasses"].([]any); ok {
		for k, op := range ops {
			if k >= len(ci.Opclasses) {
				break
			}
			if s, ok := op.(string); ok {
				c.check(ci.Opclasses[k].Name == s, "%s: index %s column %d opclass %s, catalog %s", id, name, k, s, ci.Opclasses[k].Name)
			} else {
				c.check(ci.Opclasses[k].Default, "%s: index %s column %d default opclass, catalog %s", id, name, k, ci.Opclasses[k].Name)
			}
		}
	}
	if p, ok := ix["partial"].(bool); ok {
		c.check(p == ci.Partial, "%s: index %s partial %v, catalog %v", id, name, p, ci.Partial)
	}
	if text, ok := ix["where"].(string); ok {
		column := fmt.Sprint(cols[0]["name"])
		if norm, ok := c.normalized("where", schema, table, column, "", text); ok {
			c.check(norm == deref(ci.Where), "%s: index %s where %s (normalised %s), catalog %s", id, name, text, norm, deref(ci.Where))
		}
	}
	if exprs, ok := ix["expressions"].([]any); ok {
		for k, e := range exprs {
			text, ok := e.(string)
			if !ok {
				continue
			}
			if k >= len(ci.Keys) {
				c.check(false, "%s: index %s expression %d, catalog keys %v", id, name, k, ci.Keys)
				continue
			}
			if norm, ok := c.normalized("expression", schema, table, "", "", text); ok {
				c.check(norm == ci.Keys[k], "%s: index %s expression %s (normalised %s), catalog %s", id, name, text, norm, ci.Keys[k])
			}
		}
	}
	if sp, ok := ix["storage_parameters"].(map[string]any); ok {
		var want []string
		for k, v := range sp {
			want = append(want, fmt.Sprintf("%s=%v", k, v))
		}
		sort.Strings(want)
		got := append([]string{}, ci.Reloptions...)
		sort.Strings(got)
		c.check(slices.Equal(want, got), "%s: index %s storage %v, catalog %v", id, name, want, got)
	}
}

// sqliteParens strips one pair of outer parentheses: SQLite keeps a
// default expression without the parentheses drizzle-kit writes around it
// (`DEFAULT (unixepoch())` reads back as `unixepoch()`, G2B3).
func sqliteParens(s string) string {
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		depth := 0
		for i, r := range s {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i != len(s)-1 {
					return s
				}
			}
		}
		return s[1 : len(s)-1]
	}
	return s
}

// sqliteChecks reads `CONSTRAINT "name" CHECK(expr)` from a table's DDL.
var sqliteCheck = regexp.MustCompile(`CONSTRAINT "([^"]+)" CHECK\(`)

func sqliteChecks(ddl string) map[string]string {
	out := map[string]string{}
	for _, m := range sqliteCheck.FindAllStringSubmatchIndex(ddl, -1) {
		name := ddl[m[2]:m[3]]
		depth, start := 1, m[1]
		for i := start; i < len(ddl); i++ {
			switch ddl[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				out[name] = ddl[start:i]
				break
			}
		}
	}
	return out
}

func (c *oracleComparison) compareSQLiteTable(id string, a map[string]any, cols []map[string]any, ct *oracleTable) {
	complete := a["constraint_set"] == "complete"
	if list, ok := a["primary_key"].([]any); ok {
		got := strs(list)
		c.check(slices.Equal(got, ct.PrimaryKey) || len(got) == 0 && len(ct.PrimaryKey) == 0, "%s: primary key %v, catalog %v", id, got, ct.PrimaryKey)
	}
	indexes := map[string]oracleIndex{}
	for _, ix := range ct.Indexes {
		if ix.Origin != "pk" {
			indexes[ix.Name] = ix
		}
	}
	if list, ok := a["indexes"].([]any); ok {
		for _, x := range list {
			ix, _ := x.(map[string]any)
			name := fmt.Sprint(ix["name"])
			ci, found := indexes[name]
			c.check(found, "%s: index %s is not in the catalog", id, name)
			if !found {
				continue
			}
			got := make([]string, len(ci.Columns))
			for i, col := range ci.Columns {
				got[i] = deref(col)
			}
			c.check(slices.Equal(strs(ix["columns"]), got), "%s: index %s columns %v, catalog %v", id, name, ix["columns"], got)
			c.check(ci.Unique == (ix["unique"] == true), "%s: index %s unique %v, catalog %v", id, name, ix["unique"], ci.Unique)
			if p, ok := ix["partial"].(bool); ok {
				c.check(p == ci.Partial, "%s: index %s partial %v, catalog %v", id, name, p, ci.Partial)
			}
			if text, ok := ix["where"].(string); ok {
				_, where, _ := strings.Cut(deref(ci.SQL), " WHERE ")
				c.check(text == where, "%s: index %s where %s, catalog %s", id, name, text, where)
			}
		}
		if complete {
			c.check(len(list) == len(indexes), "%s: %d indexes, catalog %d", id, len(list), len(indexes))
		}
	}
	if list, ok := a["unique_constraints"].([]any); ok && complete {
		// SQLite has no unique constraint of drizzle-kit's making: .unique()
		// is a unique index (A10).
		n := 0
		for _, ix := range ct.Indexes {
			if ix.Origin == "u" {
				n++
			}
		}
		c.check(len(list) == n, "%s: %d unique constraints, catalog %d", id, len(list), n)
	}
	checks := sqliteChecks(ct.SQL)
	if list, ok := a["check_constraints"].([]any); ok {
		for _, x := range list {
			ck, _ := x.(map[string]any)
			name := fmt.Sprint(ck["name"])
			text, found := checks[name]
			c.check(found && text == ck["expression"], "%s: check %s %v, catalog %q", id, name, ck["expression"], text)
		}
		if complete {
			c.check(len(list) == len(checks), "%s: %d checks, catalog %d", id, len(list), len(checks))
		}
	}
	catCols := map[string]oracleColumn{}
	for _, cc := range ct.Columns {
		catCols[cc.Name] = cc
	}
	for _, col := range cols {
		claims := claimSet(col)
		if claims["name"] == "" {
			continue
		}
		colName := fmt.Sprint(col["name"])
		cc, ok := catCols[colName]
		c.columns++
		c.check(ok, "%s.%s: column is not in the catalog", id, colName)
		if !ok {
			continue
		}
		c.claimForm(col, "name", claims)
		if claims["type"] != "" {
			// SQLite keeps the declared type as written; affinity names
			// compare without case (G2B3).
			c.check(strings.EqualFold(fmt.Sprint(col["type"]), cc.Type), "%s.%s: type %v, catalog %s", id, colName, col["type"], cc.Type)
			c.claimForm(col, "type", claims)
		}
		if claims["nullable"] != "" {
			c.check(col["nullable"] == !cc.NotNull, "%s.%s: nullable %v, catalog NOT NULL %v", id, colName, col["nullable"], cc.NotNull)
			c.claimForm(col, "nullable", claims)
		}
		if claims["primary_key"] != "" {
			c.check(col["primary_key"] == slices.Contains(ct.PrimaryKey, colName), "%s.%s: primary_key %v, catalog %v", id, colName, col["primary_key"], ct.PrimaryKey)
			c.claimForm(col, "primary_key", claims)
		}
		if claims["unique"] != "" {
			got := false
			for _, ix := range indexes {
				got = got || ix.Unique && !ix.Partial && len(ix.Columns) == 1 && deref(ix.Columns[0]) == colName
			}
			c.check(col["unique"] == got, "%s.%s: unique %v, catalog %v", id, colName, col["unique"], got)
			c.claimForm(col, "unique", claims)
		}
		if claims["index"] != "" {
			got := false
			for _, ix := range indexes {
				got = got || !ix.Unique && len(ix.Columns) == 1 && deref(ix.Columns[0]) == colName
			}
			c.check(col["index"] == got, "%s.%s: index %v, catalog %v", id, colName, col["index"], got)
			c.claimForm(col, "index", claims)
		}
		if claims["default"] != "" {
			c.check((col["default"] == "server") == (cc.Default != nil), "%s.%s: default %v, catalog %s", id, colName, col["default"], deref(cc.Default))
			c.claimForm(col, "default", claims)
		}
		if claims["server_default"] != "" {
			c.check(col["server_default"] == (cc.Default != nil), "%s.%s: server_default %v, catalog %s", id, colName, col["server_default"], deref(cc.Default))
			c.claimForm(col, "server_default", claims)
		}
		if text, ok := col["server_default_text"].(string); ok {
			c.check(cc.Default != nil && sqliteParens(text) == sqliteParens(*cc.Default), "%s.%s: default %s, catalog %s", id, colName, text, deref(cc.Default))
		}
		if claims["foreign_keys"] != "" {
			var catFKs []fkView
			for _, fk := range ct.ForeignKeys {
				if slices.Contains(fk.Columns, colName) {
					catFKs = append(catFKs, fkView{"", fk.Columns, "", fk.RefTable, fk.RefColumns, strings.ToLower(fk.OnDelete), strings.ToLower(fk.OnUpdate)})
				}
			}
			fks, _ := col["foreign_keys"].([]any)
			for _, x := range fks {
				if fk, _ := x.(map[string]any); fk["name"] != nil {
					c.check(false, "%s.%s: a sqlite foreign key name %v is claimed (SQLite keeps none)", id, colName, fk["name"])
				}
			}
			c.compareFKs(id, colName, col, catFKs)
			c.claimForm(col, "foreign_keys", claims)
		}
	}
}

// compareMySQL checks the mapped MySQL claims against drizzle-kit's
// printed SQL (no database): the table, each claimed column's line with
// its type, NOT NULL and DEFAULT. These facts are mapped, never oracle.
func (c *oracleComparison) compareMySQL(id string, a map[string]any, cols []map[string]any) {
	c.check(a["verification"] == "mapped", "%s: verification %v, want mapped", id, a["verification"])
	head := "CREATE TABLE `" + fmt.Sprint(a["name"]) + "` ("
	start := strings.Index(c.run.SQL, head)
	c.check(start >= 0, "%s: no %s in the SQL", id, head)
	if start < 0 {
		return
	}
	body := c.run.SQL[start:]
	if end := strings.Index(body, "\n);"); end >= 0 {
		body = body[:end]
	}
	lines := map[string]string{}
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "`") {
			name := l[1 : strings.Index(l[1:], "`")+1]
			lines[name] = strings.TrimSuffix(l, ",")
		}
	}
	for _, col := range cols {
		claims := claimSet(col)
		if claims["name"] == "" {
			continue
		}
		colName := fmt.Sprint(col["name"])
		l, ok := lines[colName]
		c.columns++
		c.check(ok, "%s.%s: no column line in the SQL", id, colName)
		if !ok {
			continue
		}
		if claims["type"] != "" {
			rest := strings.TrimPrefix(l, "`"+colName+"` ")
			word, _, _ := strings.Cut(rest, " ")
			typ := fmt.Sprint(col["type"])
			c.check(rest == typ || strings.HasPrefix(rest, typ+" ") || mysqlReported[word] == typ, "%s.%s: type %v, SQL %s", id, colName, col["type"], l)
		}
		if claims["nullable"] != "" {
			c.check(col["nullable"] == !strings.Contains(l, "NOT NULL"), "%s.%s: nullable %v, SQL %s", id, colName, col["nullable"], l)
		}
		if claims["server_default"] != "" {
			c.check(col["server_default"] == strings.Contains(l, " DEFAULT "), "%s.%s: server_default %v, SQL %s", id, colName, col["server_default"], l)
		}
	}
}

// mysqlReported are the types drizzle-kit writes that MySQL 8.0.19+
// reports in information_schema COLUMN_TYPE in another form:
// SERIAL is BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE, BOOLEAN is
// TINYINT(1), CHAR is CHAR(1). Every other type of the run list reads back
// as written.
var mysqlReported = map[string]string{"serial": "bigint unsigned", "boolean": "tinyint(1)", "char": "char(1)"}

// claimForm records the compared forms of a claimed column fact.
func (c *oracleComparison) claimForm(col map[string]any, fact string, claims map[string]string) {
	for _, f := range columnForm(c.run.Dialect, col, fact, claims[fact]) {
		c.form(f)
	}
}

var numberText = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// defaultTextClass is the shape of a claimed default text: the form key
// of a server default.
func defaultTextClass(text any) string {
	s, ok := text.(string)
	switch {
	case !ok:
		return "presence"
	case strings.HasSuffix(s, "'::json") || strings.HasSuffix(s, "'::jsonb"):
		return "json"
	case strings.HasPrefix(s, "'{"):
		// A Postgres array literal, or JSON text on sqlite.
		return "brace-string"
	case strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'"):
		return "string"
	case numberText.MatchString(s):
		return "number"
	case s == "true" || s == "false":
		return "boolean"
	case s == "now()" || s == "gen_random_uuid()":
		return s
	}
	return "sql"
}

var typeArgs = regexp.MustCompile(`\([^)]*\)`)

// columnForm is the form keys of one claimed column fact: dialect, fact,
// row and the class of the value (the type without its arguments, an
// enum, the shape of a default text); a foreign key list gives one key per
// shape (composite, unnamed) and one per referential action.
func columnForm(dialect string, col map[string]any, fact, row string) []string {
	base := "column/" + dialect + "/" + fact + "/" + row + "/"
	switch fact {
	case "name":
		return []string{base}
	case "type":
		class := typeArgs.ReplaceAllString(fmt.Sprint(col["type"]), "")
		if _, ok := col["enum"].(map[string]any); ok {
			class = "enum"
			if strings.HasSuffix(fmt.Sprint(col["type"]), "[]") {
				class += "[]"
			}
		}
		return []string{base + class}
	case "default":
		class := fmt.Sprint(col["default"])
		if class == "server" {
			class += ":" + defaultTextClass(col["server_default_text"])
		}
		return []string{base + class}
	case "foreign_keys":
		fks, _ := col["foreign_keys"].([]any)
		if len(fks) == 0 {
			return []string{base + "none"}
		}
		var out []string
		for _, x := range fks {
			fk, _ := x.(map[string]any)
			shape := "fk"
			if len(strs(fk["local_columns"])) > 1 {
				shape += "/composite"
			}
			if fk["name"] == nil {
				shape += "/unnamed"
			}
			out = append(out, base+shape, fmt.Sprintf("%son_delete=%v", base, fk["on_delete"]), fmt.Sprintf("%son_update=%v", base, fk["on_update"]))
		}
		return out
	}
	return []string{base + fmt.Sprint(col[fact])}
}

// oracleTableForms are the form keys of a table's facts as its record states
// them: the table, a complete column set, the primary key (one or several
// columns) and its name, unique constraints by nulls_not_distinct, each
// index by the options it states, checks.
func oracleTableForms(dialect string, a map[string]any) []string {
	base := "table/" + dialect
	out := []string{base}
	if a["column_set"] == "complete" {
		out = append(out, base+"/column_set")
	}
	if list, ok := a["primary_key"].([]any); ok {
		out = append(out, fmt.Sprintf("%s/primary_key/%d", base, min(len(list), 2)))
	}
	if _, ok := a["primary_key_name"].(string); ok {
		out = append(out, base+"/primary_key_name")
	}
	list, _ := a["unique_constraints"].([]any)
	for _, x := range list {
		u, _ := x.(map[string]any)
		if v, ok := u["nulls_not_distinct"].(bool); ok {
			out = append(out, fmt.Sprintf("%s/unique/nulls_not_distinct=%v", base, v))
		}
	}
	list, _ = a["indexes"].([]any)
	for _, x := range list {
		ix, _ := x.(map[string]any)
		out = append(out, indexForm(base, ix))
	}
	list, _ = a["check_constraints"].([]any)
	if len(list) > 0 {
		out = append(out, base+"/check")
	}
	return out
}

// indexForm is the form key of one index record.
func indexForm(base string, ix map[string]any) string {
	form := base + "/index"
	if ix["unique"] == true {
		form += "/unique"
	}
	if m, ok := ix["method"].(string); ok {
		form += "/" + m
	}
	orders, nulls := strs(ix["orders"]), strs(ix["nulls"])
	for k, o := range orders {
		if o == "desc" || k < len(nulls) && nulls[k] == "first" {
			form += "/order"
			break
		}
	}
	ops, _ := ix["opclasses"].([]any)
	for _, op := range ops {
		if op != nil {
			form += "/opclass"
			break
		}
	}
	if ix["partial"] == true {
		form += "/partial"
	}
	if _, ok := ix["where"].(string); ok {
		form += "/where"
	}
	if _, ok := ix["expressions"].([]any); ok {
		form += "/expression"
	}
	if sp, ok := ix["storage_parameters"].(map[string]any); ok && len(sp) > 0 {
		form += "/storage"
	}
	return form
}

// oracleColumnForms are the form keys of a column's oracle facts: each claim
// with verification oracle, and the identity and generated attributes.
func oracleColumnForms(dialect string, col map[string]any) []string {
	var out []string
	list, _ := col["claims"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if m["verification"] == "oracle" {
			out = append(out, columnForm(dialect, col, fmt.Sprint(m["fact"]), fmt.Sprint(m["row"]))...)
		}
	}
	if v, ok := col["identity"].(string); ok {
		out = append(out, "column/"+dialect+"/identity/"+v)
	}
	if _, ok := col["generated"].(string); ok {
		out = append(out, "column/"+dialect+"/generated")
	}
	return out
}

// fixtureOracleForms links the fixture and returns the form keys of every
// oracle-labelled fact: the tables with verification oracle and their
// columns' oracle claims.
func fixtureOracleForms(t *testing.T) map[string]bool {
	t.Helper()
	nodes, _, _, _ := linkFixture(t)
	forms := map[string]bool{}
	dialects := map[string]string{}
	for _, n := range nodes {
		if n.Kind == "table" {
			a := record(t, n)
			dialects[n.Identity] = fmt.Sprint(a["dialect"])
			if a["verification"] == "oracle" {
				for _, f := range oracleTableForms(dialects[n.Identity], a) {
					forms[f] = true
				}
			}
		}
	}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		table := "table:" + n.Identity[len("column:"):strings.LastIndex(n.Identity, ".")]
		col := record(t, n)
		for _, f := range oracleColumnForms(dialects[table], col) {
			if dialects[table] == "mysql" {
				t.Errorf("%s: a mysql fact is labelled oracle (%s)", n.Identity, f)
			}
			forms[f] = true
		}
	}
	return forms
}

// TestDrizzleOracleForms is the oracle rule (T018g0): a fact is labelled
// oracle only for a form the oracle compared. The forms of the fixture's
// oracle-labelled facts are exactly oracleForms, and TestDrizzleOracle
// over the fixture's dump checks that each of them was compared (0
// disagreements). A new form needs an oracle run before it is
// added here.
func TestDrizzleOracleForms(t *testing.T) {
	forms := fixtureOracleForms(t)
	for f := range forms {
		if !slices.Contains(oracleForms, f) {
			t.Errorf("oracle-labelled form %s is not one the oracle compared (oracleForms)", f)
		}
	}
	for _, f := range oracleForms {
		if !forms[f] {
			t.Errorf("oracleForms lists %s, which no fixture fact has", f)
		}
	}
	if !slices.IsSorted(oracleForms) {
		t.Error("oracleForms is not sorted")
	}
}

// oracleForms are the forms TestDrizzleOracle compared over the fixture's
// runs (scripts/oracle/drizzle_catalog.sh --fixture, 0 disagreements).
var oracleForms = []string{
	"column/postgresql/default/DR-C1/server:presence",
	"column/postgresql/default/DR-C2/absent",
	"column/postgresql/default/DR-C2/server:boolean",
	"column/postgresql/default/DR-C2/server:brace-string",
	"column/postgresql/default/DR-C2/server:gen_random_uuid()",
	"column/postgresql/default/DR-C2/server:json",
	"column/postgresql/default/DR-C2/server:now()",
	"column/postgresql/default/DR-C2/server:number",
	"column/postgresql/default/DR-C2/server:sql",
	"column/postgresql/default/DR-C2/server:string",
	"column/postgresql/foreign_keys/DR-C2/fk",
	"column/postgresql/foreign_keys/DR-C2/none",
	"column/postgresql/foreign_keys/DR-C2/on_delete=cascade",
	"column/postgresql/foreign_keys/DR-C2/on_delete=no action",
	"column/postgresql/foreign_keys/DR-C2/on_delete=restrict",
	"column/postgresql/foreign_keys/DR-C2/on_delete=set null",
	"column/postgresql/foreign_keys/DR-C2/on_update=cascade",
	"column/postgresql/foreign_keys/DR-C2/on_update=no action",
	"column/postgresql/foreign_keys/DR-C2/on_update=set null",
	"column/postgresql/foreign_keys/DR-X1/fk",
	"column/postgresql/foreign_keys/DR-X1/fk/composite",
	"column/postgresql/foreign_keys/DR-X1/on_delete=cascade",
	"column/postgresql/foreign_keys/DR-X1/on_delete=no action",
	"column/postgresql/foreign_keys/DR-X1/on_update=cascade",
	"column/postgresql/foreign_keys/DR-X1/on_update=no action",
	"column/postgresql/generated",
	"column/postgresql/identity/always",
	"column/postgresql/identity/by default",
	"column/postgresql/index/DR-T1/false",
	"column/postgresql/index/DR-X1/false",
	"column/postgresql/index/DR-X1/true",
	"column/postgresql/name/DR-C1/",
	"column/postgresql/name/DR-K2/",
	"column/postgresql/name/DR-K3/",
	"column/postgresql/nullable/DR-C1/false",
	"column/postgresql/nullable/DR-C2/false",
	"column/postgresql/nullable/DR-C2/true",
	"column/postgresql/nullable/DR-X1/false",
	"column/postgresql/primary_key/DR-C2/false",
	"column/postgresql/primary_key/DR-C2/true",
	"column/postgresql/primary_key/DR-X1/true",
	"column/postgresql/server_default/DR-C1/true",
	"column/postgresql/server_default/DR-C2/false",
	"column/postgresql/server_default/DR-C2/true",
	"column/postgresql/type/DR-C1/bigint",
	"column/postgresql/type/DR-C1/boolean",
	"column/postgresql/type/DR-C1/character",
	"column/postgresql/type/DR-C1/character varying",
	"column/postgresql/type/DR-C1/date",
	"column/postgresql/type/DR-C1/double precision",
	"column/postgresql/type/DR-C1/inet",
	"column/postgresql/type/DR-C1/integer",
	"column/postgresql/type/DR-C1/integer[]",
	"column/postgresql/type/DR-C1/interval",
	"column/postgresql/type/DR-C1/json",
	"column/postgresql/type/DR-C1/jsonb",
	"column/postgresql/type/DR-C1/numeric",
	"column/postgresql/type/DR-C1/real",
	"column/postgresql/type/DR-C1/smallint",
	"column/postgresql/type/DR-C1/text",
	"column/postgresql/type/DR-C1/text[]",
	"column/postgresql/type/DR-C1/time without time zone",
	"column/postgresql/type/DR-C1/timestamp with time zone",
	"column/postgresql/type/DR-C1/timestamp without time zone",
	"column/postgresql/type/DR-C1/uuid",
	"column/postgresql/type/DR-C3/bytea",
	"column/postgresql/type/DR-C3/text",
	"column/postgresql/type/DR-E1/enum",
	"column/postgresql/type/DR-E1/enum[]",
	"column/postgresql/unique/DR-C2/false",
	"column/postgresql/unique/DR-C2/true",
	"column/postgresql/unique/DR-X1/true",
	"column/sqlite/default/DR-C2/absent",
	"column/sqlite/default/DR-C2/server:boolean",
	"column/sqlite/default/DR-C2/server:brace-string",
	"column/sqlite/default/DR-C2/server:number",
	"column/sqlite/default/DR-C2/server:sql",
	"column/sqlite/default/DR-C2/server:string",
	"column/sqlite/foreign_keys/DR-C2/fk/unnamed",
	"column/sqlite/foreign_keys/DR-C2/none",
	"column/sqlite/foreign_keys/DR-C2/on_delete=cascade",
	"column/sqlite/foreign_keys/DR-C2/on_update=no action",
	"column/sqlite/index/DR-T1/false",
	"column/sqlite/index/DR-X1/false",
	"column/sqlite/index/DR-X1/true",
	"column/sqlite/name/DR-C1/",
	"column/sqlite/nullable/DR-C2/false",
	"column/sqlite/nullable/DR-C2/true",
	"column/sqlite/primary_key/DR-C2/false",
	"column/sqlite/primary_key/DR-C2/true",
	"column/sqlite/primary_key/DR-X1/true",
	"column/sqlite/server_default/DR-C2/false",
	"column/sqlite/server_default/DR-C2/true",
	"column/sqlite/type/DR-C1/blob",
	"column/sqlite/type/DR-C1/integer",
	"column/sqlite/type/DR-C1/numeric",
	"column/sqlite/type/DR-C1/real",
	"column/sqlite/type/DR-C1/text",
	"column/sqlite/unique/DR-C2/false",
	"column/sqlite/unique/DR-C2/true",
	"column/sqlite/unique/DR-X1/true",
	"table/postgresql",
	"table/postgresql/check",
	"table/postgresql/column_set",
	"table/postgresql/index/brin/partial/where",
	"table/postgresql/index/btree",
	"table/postgresql/index/btree/opclass",
	"table/postgresql/index/btree/order",
	"table/postgresql/index/btree/partial/where",
	"table/postgresql/index/unique/btree",
	"table/postgresql/index/unique/btree/expression",
	"table/postgresql/index/unique/btree/partial/where",
	"table/postgresql/primary_key/0",
	"table/postgresql/primary_key/1",
	"table/postgresql/primary_key/2",
	"table/postgresql/primary_key_name",
	"table/postgresql/unique/nulls_not_distinct=false",
	"table/postgresql/unique/nulls_not_distinct=true",
	"table/sqlite",
	"table/sqlite/check",
	"table/sqlite/column_set",
	"table/sqlite/index/partial/where",
	"table/sqlite/index/unique",
	"table/sqlite/primary_key/1",
	"table/sqlite/primary_key/2",
}
