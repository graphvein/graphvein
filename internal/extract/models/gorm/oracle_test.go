package gorm

import (
	"context"
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
	"github.com/graphvein/graphvein/internal/scan"
)

// The dump scripts/oracle/gorm_catalog.sh writes: one run per
// module and dialect, with the migrate calls GORM ran (each error kept),
// GORM's parsed schemas (for reading a disagreement) and the catalog of
// the scratch database after the calls (postgres: pg_catalog; sqlite:
// sqlite_master and the pragmas, through glebarez).
type gormOracleDump struct {
	Runs []gormOracleRun `json:"runs"`
}

type gormOracleRun struct {
	Name         string            `json:"name"`
	Repo         string            `json:"repo"`
	Group        string            `json:"group"`
	Dialect      string            `json:"dialect"`
	Driver       string            `json:"driver"`
	CrossDialect bool              `json:"cross_dialect"`
	Versions     map[string]string `json:"versions"`
	Setup        []string          `json:"setup"`
	Steps        []struct {
		Site        string   `json:"site"`
		CreateTable bool     `json:"create_table"`
		Models      []string `json:"models"`
		Error       string   `json:"error"`
	} `json:"steps"`
	Catalog *struct {
		Tables []oracleTable `json:"tables"`
	} `json:"catalog"`
}

type oracleTable struct {
	Name    string `json:"name"`
	Columns []struct {
		Name          string  `json:"name"`
		Type          string  `json:"type"`
		NotNull       bool    `json:"not_null"`
		Default       *string `json:"default"`
		Autoincrement bool    `json:"autoincrement"`
		Generated     bool    `json:"generated"`
	} `json:"columns"`
	PrimaryKey []string `json:"primary_key"`
	Uniques    []struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
	} `json:"uniques"`
	Indexes []struct {
		Name       string   `json:"name"`
		Columns    []string `json:"columns"`
		Descending []bool   `json:"descending"`
		Unique     bool     `json:"unique"`
		Partial    bool     `json:"partial"`
	} `json:"indexes"`
	ForeignKeys []struct {
		Name       string   `json:"name"`
		Columns    []string `json:"columns"`
		RefTable   string   `json:"ref_table"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
	} `json:"foreign_keys"`
	Checks int `json:"checks"`
}

// oracleDialects are the dialects the oracle runs; MySQL facts are mapped
// (GO-V5) and never compared.
var oracleDialects = []string{"postgres", "sqlite"}

// gormRecords is one linked repository: its table and column nodes and
// which columns each table contains.
type gormRecords struct {
	tables  []contract.Node
	columns map[string][]contract.Node
}

func linkRepo(t *testing.T, dir string) gormRecords {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
	}
	nodes, edges, _ := Link(build(t, sources))
	byID := map[string]contract.Node{}
	for _, n := range nodes {
		byID[n.Identity] = n
	}
	out := gormRecords{columns: map[string][]contract.Node{}}
	for _, n := range nodes {
		if n.Kind == "table" {
			out.tables = append(out.tables, n)
		}
	}
	for _, e := range edges {
		if e.Kind == "contains" && strings.HasPrefix(e.Source, "table:") {
			if c, ok := byID[e.Target]; ok && c.Kind == "column" {
				out.columns[e.Source] = append(out.columns[e.Source], c)
			}
		}
	}
	return out
}

// jsonAttrs reads a node's attributes as JSON values.
func jsonAttrs(t *testing.T, n contract.Node) map[string]any {
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

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, x := range list {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// groupDialects are the dialects a table's facts are claimed for.
func groupDialects(a map[string]any) []string {
	if d, ok := a["dialect"].(string); ok {
		return []string{d}
	}
	return anyStrings(a["dialects"])
}

// claimKey names one fact of one record on one dialect.
func claimKey(identity, fact, dialect string) string {
	return identity + " " + fact + " on " + dialect
}

// requiredOracle lists the facts labelled oracle that the oracle must
// compare: each oracle claim of a column on its dialect (a flat claim on
// every oracle dialect of the group), and the claimed table facts of a
// table whose verification is oracle. A MySQL claim labelled oracle, or an
// oracle label in a group of no oracle dialect, is an error.
func requiredOracle(t *testing.T, recs gormRecords) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, tn := range recs.tables {
		a := jsonAttrs(t, tn)
		var dialects []string
		for _, d := range groupDialects(a) {
			for _, o := range oracleDialects {
				if d == o {
					dialects = append(dialects, d)
				}
			}
		}
		if a["verification"] == models.VerificationOracle {
			if len(dialects) == 0 {
				t.Errorf("%s: verification oracle on dialects %v", tn.Identity, groupDialects(a))
			}
			for _, d := range dialects {
				for _, f := range tableFacts(a) {
					out[claimKey(tn.Identity, f, d)] = true
				}
			}
		}
		for _, cn := range recs.columns[tn.Identity] {
			ca := jsonAttrs(t, cn)
			list, _ := ca["claims"].([]any)
			for _, x := range list {
				m, _ := x.(map[string]any)
				if m["verification"] != models.VerificationOracle {
					continue
				}
				fact := fmt.Sprint(m["fact"])
				if d, ok := m["dialect"].(string); ok {
					if d == "mysql" {
						t.Errorf("%s: %s on mysql is labelled oracle (GO-V5: mapped)", cn.Identity, fact)
					}
					out[claimKey(cn.Identity, fact, d)] = true
					continue
				}
				if len(dialects) == 0 {
					t.Errorf("%s: %s labelled oracle on dialects %v", cn.Identity, fact, groupDialects(a))
				}
				for _, d := range dialects {
					out[claimKey(cn.Identity, fact, d)] = true
				}
			}
		}
	}
	return out
}

// tableFacts are the table facts a record claims: its name, and the column
// set, primary key, index list, unique constraints and check constraints
// where they are not unknown.
func tableFacts(a map[string]any) []string {
	out := []string{"name"}
	if a["column_set"] == "complete" {
		out = append(out, "column_set")
	}
	for _, f := range []string{"primary_key", "indexes", "unique_constraints"} {
		if a[f] != nil {
			out = append(out, f)
		}
	}
	if a["check_constraints"] != nil && a["constraint_set"] == "complete" {
		out = append(out, "check_constraints")
	}
	return out
}

// gormComparison holds one run's comparison.
type gormComparison struct {
	t        *testing.T
	run      gormOracleRun
	compared map[string]bool
	tables   int
	columns  int
	facts    int
	unknown  int
	disagree []string
}

func (c *gormComparison) check(key string, ok bool, format string, a ...any) {
	c.facts++
	if !c.run.CrossDialect {
		c.compared[key] = true
	}
	if !ok {
		c.disagree = append(c.disagree, fmt.Sprintf(format, a...))
	}
}

// TestGormOracle compares every claimed fact of the linked modules with the
// catalog GORM's own migrate calls left in a scratch database (step GB5,
// task file "Oracle design"): per table its existence, the column set when
// complete, the primary key in key order, the index list (name, columns in
// order, unique, partial, descending) and the unique and check constraints
// when claimed (all of them when the constraint set is complete); per
// column its existence, SQL type (case-insensitive on sqlite) and the
// type's length, precision and scale, nullability, primary-key membership,
// autoincrement, index membership, single-column uniqueness, default
// presence and text, and its foreign keys (local and referenced columns,
// target, actions, and the name where claimed), both ways. A run-time
// dialect group is compared per dialect: by_dialect facts on that dialect,
// flat facts on every oracle dialect. A claimed fact the catalog lacks or
// contradicts is a disagreement (precision stop S1). A cross-dialect run
// (realworld on postgres) compares only the dialect-independent facts and
// counts toward no coverage. Every fact labelled oracle in a linked
// repository must be compared on its dialect, and no MySQL fact may be
// labelled oracle. Set GRAPHVEIN_GORM_ORACLE to the dump and
// GRAPHVEIN_GORM_ORACLE_ROOT to the directory the runs' repo paths are
// relative to (the fixture directory, or the corpus directory).
func TestGormOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_GORM_ORACLE"), os.Getenv("GRAPHVEIN_GORM_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_GORM_ORACLE or GRAPHVEIN_GORM_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump gormOracleDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Runs) == 0 {
		t.Fatal("no runs in the dump")
	}
	repos := map[string]gormRecords{}
	var repoNames []string
	compared := map[string]bool{}
	for _, run := range dump.Runs {
		t.Run(run.Name+"/"+run.Dialect, func(t *testing.T) {
			recs, ok := repos[run.Repo]
			if !ok {
				recs = linkRepo(t, filepath.Join(root, run.Repo))
				repos[run.Repo] = recs
				repoNames = append(repoNames, run.Repo)
			}
			c := &gormComparison{t: t, run: run, compared: compared}
			c.compareRun(recs)
			failed := 0
			for _, s := range run.Steps {
				if s.Error != "" {
					failed++
					t.Logf("call %s fails on %s: %s", s.Site, run.Dialect, s.Error)
				}
			}
			mode := ""
			if run.CrossDialect {
				mode = ", cross-dialect"
			}
			t.Logf("%s on %s (%s, gorm %s%s): %d calls (%d fail), %d tables, %d read columns, %d facts compared, %d disagreements, %d facts unknown",
				run.Name, run.Dialect, run.Driver, run.Versions["gorm.io/gorm"], mode, len(run.Steps), failed, c.tables, c.columns, c.facts, len(c.disagree), c.unknown)
			for _, d := range c.disagree {
				t.Errorf("disagreement: %s", d)
			}
		})
	}
	sort.Strings(repoNames)
	for _, repo := range repoNames {
		required := requiredOracle(t, repos[repo])
		var missing []string
		for k := range required {
			if !compared[k] {
				missing = append(missing, k)
			}
		}
		sort.Strings(missing)
		for _, k := range missing {
			t.Errorf("%s: fact labelled oracle is not compared: %s", repo, k)
		}
		t.Logf("%s: %d facts labelled oracle, %d compared", repo, len(required), len(required)-len(missing))
	}
}

func (c *gormComparison) compareRun(recs gormRecords) {
	t := c.t
	if c.run.Catalog == nil {
		t.Fatal("the run has no catalog")
	}
	cat := map[string]*oracleTable{}
	for i := range c.run.Catalog.Tables {
		cat[c.run.Catalog.Tables[i].Name] = &c.run.Catalog.Tables[i]
	}
	matched := map[string]bool{}
	found := 0
	for _, tn := range recs.tables {
		a := jsonAttrs(t, tn)
		if a["set"] != c.run.Group {
			continue
		}
		found++
		dialects := groupDialects(a)
		in := false
		for _, d := range dialects {
			in = in || d == c.run.Dialect
		}
		if in == c.run.CrossDialect {
			t.Fatalf("%s: dialects %v, the run is on %s (cross-dialect %v)", tn.Identity, dialects, c.run.Dialect, c.run.CrossDialect)
		}
		if pins, _ := a["pins"].(map[string]any); pins["gorm.io/gorm"] != c.run.Versions["gorm.io/gorm"] {
			t.Fatalf("%s: pins %v, the run used gorm %s", tn.Identity, pins, c.run.Versions["gorm.io/gorm"])
		}
		c.tables++
		name := fmt.Sprint(a["name"])
		ct := cat[name]
		c.check(claimKey(tn.Identity, "name", c.run.Dialect), ct != nil, "%s: table %s is not in the catalog", tn.Identity, name)
		if ct == nil {
			continue
		}
		matched[name] = true
		c.compareTable(tn, a, ct, recs.columns[tn.Identity])
	}
	if found == 0 {
		t.Fatalf("no table records in set %s", c.run.Group)
	}
	var extra []string
	for name := range cat {
		if !matched[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		t.Logf("catalog table %s has no record (not claimed)", name)
	}
}

func (c *gormComparison) compareTable(tn contract.Node, a map[string]any, ct *oracleTable, cols []contract.Node) {
	t := c.t
	id, d := tn.Identity, c.run.Dialect
	var catCols []string
	for _, cc := range ct.Columns {
		catCols = append(catCols, cc.Name)
	}
	if a["column_set"] == "complete" {
		var names []string
		for _, cn := range cols {
			names = append(names, fmt.Sprint(jsonAttrs(t, cn)["name"]))
		}
		c.check(claimKey(id, "column_set", d), reflect.DeepEqual(sortedStrings(names), sortedStrings(catCols)), "%s: columns %v, catalog %v", id, sortedStrings(names), sortedStrings(catCols))
	}
	if a["primary_key"] != nil {
		pk := anyStrings(a["primary_key"])
		c.check(claimKey(id, "primary_key", d), reflect.DeepEqual(pk, ct.PrimaryKey), "%s: primary key %v, catalog %v", id, pk, ct.PrimaryKey)
	}
	complete := a["constraint_set"] == "complete"
	if a["indexes"] != nil {
		c.compareIndexes(id, a, ct, complete)
	}
	if a["unique_constraints"] != nil {
		list, _ := a["unique_constraints"].([]any)
		ok := true
		var why []string
		for _, x := range list {
			m, _ := x.(map[string]any)
			cs := strings.Join(anyStrings(m["columns"]), ",")
			name, named := m["name"].(string)
			hit := false
			for _, u := range ct.Uniques {
				hit = hit || strings.Join(u.Columns, ",") == cs && (!named || c.run.CrossDialect || u.Name == name)
			}
			if !hit {
				ok = false
				why = append(why, fmt.Sprintf("%v (%s)", m["name"], cs))
			}
		}
		if complete && len(list) != len(ct.Uniques) {
			ok = false
			why = append(why, fmt.Sprintf("%d claimed, catalog %d", len(list), len(ct.Uniques)))
		}
		c.check(claimKey(id, "unique_constraints", d), ok, "%s: unique constraints %v disagree with the catalog %+v", id, why, ct.Uniques)
	}
	if a["check_constraints"] != nil && complete {
		n := len(anyStrings(a["check_constraints"]))
		c.check(claimKey(id, "check_constraints", d), n == ct.Checks, "%s: %d check constraints, catalog %d", id, n, ct.Checks)
	}
	byName := map[string]int{}
	for i, cc := range ct.Columns {
		byName[cc.Name] = i
	}
	for _, cn := range cols {
		ca := jsonAttrs(t, cn)
		if ca["state"] != "read" {
			c.unknown++
			continue
		}
		c.unknown += countReasons(ca)
		c.compareColumn(cn.Identity, ca, ct, byName)
	}
}

func (c *gormComparison) compareIndexes(id string, a map[string]any, ct *oracleTable, complete bool) {
	list, _ := a["indexes"].([]any)
	var why []string
	used := map[int]bool{}
	for _, x := range list {
		m, _ := x.(map[string]any)
		name, named := m["name"].(string)
		cols := m["columns"]
		k := -1
		for i, ix := range ct.Indexes {
			if used[i] {
				continue
			}
			if named && ix.Name == name || !named && cols != nil && reflect.DeepEqual(anyStrings(cols), ix.Columns) && m["unique"] == ix.Unique {
				k = i
				break
			}
		}
		if k < 0 {
			why = append(why, fmt.Sprintf("%v %v not in the catalog", m["name"], cols))
			continue
		}
		used[k] = true
		ix := ct.Indexes[k]
		if cols != nil && !reflect.DeepEqual(anyStrings(cols), ix.Columns) {
			why = append(why, fmt.Sprintf("%s columns %v, catalog %v", ix.Name, cols, ix.Columns))
		}
		if m["unique"] != ix.Unique || m["partial"] != ix.Partial {
			why = append(why, fmt.Sprintf("%s unique %v partial %v, catalog %v %v", ix.Name, m["unique"], m["partial"], ix.Unique, ix.Partial))
		}
		if desc, ok := m["descending"].([]any); ok {
			var got []bool
			for _, v := range desc {
				got = append(got, v == true)
			}
			if !reflect.DeepEqual(got, ix.Descending) {
				why = append(why, fmt.Sprintf("%s descending %v, catalog %v", ix.Name, got, ix.Descending))
			}
		}
	}
	if complete && len(list) != len(ct.Indexes) {
		why = append(why, fmt.Sprintf("%d indexes claimed, catalog %d", len(list), len(ct.Indexes)))
	}
	c.check(claimKey(id, "indexes", c.run.Dialect), len(why) == 0, "%s: indexes: %s", id, strings.Join(why, "; "))
}

// countReasons counts the facts a read column leaves unknown.
func countReasons(col map[string]any) int {
	n := 0
	list, _ := col["reasons"].([]any)
	for _, r := range list {
		if m, ok := r.(map[string]any); ok && m["fact"] != nil {
			n++
		}
	}
	return n
}

func sortedStrings(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

// modifierRE reads the modifiers of a catalog type: character varying(10),
// numeric(10,2), timestamp(6) with time zone, varchar(120).
var modifierRE = regexp.MustCompile(`\((\d+)(?:,\s*(\d+))?\)`)

// crossDialectFacts are the facts compared on a dialect the group does not
// open (task file, dialect-independent facts); nullability is compared
// only for columns that are neither primary key nor autoincrement.
var crossDialectFacts = map[string]bool{"name": true, "nullable": true, "primary_key": true, "index": true, "unique": true, "foreign_keys": true}

// columnFacts are the claimed facts of a column on the run's dialect: a
// per-dialect claim from by_dialect, else the flat claim.
func (c *gormComparison) columnFacts(col map[string]any) map[string]any {
	out := map[string]any{}
	by, _ := col["by_dialect"].(map[string]any)
	mine, _ := by[c.run.Dialect].(map[string]any)
	list, _ := col["claims"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		fact := fmt.Sprint(m["fact"])
		switch d, ok := m["dialect"].(string); {
		case ok && d == c.run.Dialect && !c.run.CrossDialect:
			out[fact] = mine[fact]
			if fact == "server_default" {
				out["server_default_text"] = mine["server_default_text"]
			}
		case !ok:
			out[fact] = col[fact]
			if fact == "server_default" {
				out["server_default_text"] = col["server_default_text"]
			}
		}
	}
	if c.run.CrossDialect {
		for f := range out {
			if !crossDialectFacts[f] {
				delete(out, f)
			}
		}
		// A column whose key membership is unknown (open point O8) may be
		// a primary-key column, NOT NULL on postgres only.
		if col["primary_key"] != false || col["autoincrement"] != false {
			delete(out, "nullable")
		}
	}
	return out
}

func (c *gormComparison) compareColumn(colID string, col map[string]any, ct *oracleTable, byName map[string]int) {
	c.columns++
	d := c.run.Dialect
	key := func(fact string) string { return claimKey(colID, fact, d) }
	name := fmt.Sprint(col["name"])
	i, ok := byName[name]
	c.check(key("name"), ok, "%s: column %s is not in the catalog", colID, name)
	if !ok {
		return
	}
	cc := ct.Columns[i]
	facts := c.columnFacts(col)
	if v, ok := facts["type"]; ok {
		same := fmt.Sprint(v) == cc.Type
		if d == "sqlite" {
			same = strings.EqualFold(fmt.Sprint(v), cc.Type)
		}
		c.check(key("type"), same, "%s: type %v, catalog %s", colID, v, cc.Type)
	}
	mods := modifierRE.FindStringSubmatch(cc.Type)
	modifier := func(n int) any {
		if mods == nil || mods[n] == "" {
			return nil
		}
		var v float64
		fmt.Sscan(mods[n], &v)
		return v
	}
	for fact, n := range map[string]int{"length": 1, "precision": 1, "scale": 2} {
		if v, ok := facts[fact]; ok {
			got := modifier(n)
			c.check(key(fact), v == got, "%s: %s %v, catalog type %s", colID, fact, v, cc.Type)
		}
	}
	if v, ok := facts["nullable"]; ok {
		c.check(key("nullable"), v == !cc.NotNull, "%s: nullable %v, catalog not null %v", colID, v, cc.NotNull)
	}
	if v, ok := facts["primary_key"]; ok {
		in := false
		for _, p := range ct.PrimaryKey {
			in = in || p == name
		}
		c.check(key("primary_key"), v == in, "%s: primary key %v, catalog %v %v", colID, v, ct.PrimaryKey, in)
	}
	if v, ok := facts["autoincrement"]; ok {
		c.check(key("autoincrement"), v == cc.Autoincrement, "%s: autoincrement %v, catalog %v", colID, v, cc.Autoincrement)
	}
	if v, ok := facts["index"]; ok {
		in := false
		for _, ix := range ct.Indexes {
			for _, n := range ix.Columns {
				in = in || n == name
			}
		}
		c.check(key("index"), v == in, "%s: index %v, catalog %v", colID, v, in)
	}
	if v, ok := facts["unique"]; ok {
		in := false
		for _, u := range ct.Uniques {
			in = in || len(u.Columns) == 1 && u.Columns[0] == name
		}
		for _, ix := range ct.Indexes {
			in = in || ix.Unique && !ix.Partial && len(ix.Columns) == 1 && ix.Columns[0] == name
		}
		c.check(key("unique"), v == in, "%s: unique %v, catalog %v", colID, v, in)
	}
	present := cc.Default != nil
	catDefault := ""
	if present {
		catDefault = *cc.Default
	}
	if v, ok := facts["default"]; ok {
		c.check(key("default"), v == "server" && present || v == "absent" && !present, "%s: default %v, catalog %q (present %v)", colID, v, catDefault, present)
	}
	if v, ok := facts["server_default"]; ok {
		same := v == present
		if text, ok := facts["server_default_text"].(string); ok && same {
			same = text == catDefault
		}
		c.check(key("server_default"), same, "%s: server default %v text %v, catalog %q (present %v)", colID, v, facts["server_default_text"], catDefault, present)
	}
	if _, ok := facts["foreign_keys"]; ok {
		ok, why := c.compareForeignKeys(name, col, ct)
		c.check(key("foreign_keys"), ok, "%s: foreign keys %s", colID, why)
	}
}

// compareForeignKeys compares the column's foreign keys with the catalog's
// constraints that hold the column, both ways; why describes both sides.
func (c *gormComparison) compareForeignKeys(name string, col map[string]any, ct *oracleTable) (bool, string) {
	format := func(local, table string, ref []string, del, upd string) string {
		return fmt.Sprintf("(%s) -> %s(%s) %s/%s", local, table, strings.Join(ref, ","), del, upd)
	}
	var got, want []string
	names := map[string]string{}
	list, _ := col["foreign_keys"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		local := anyStrings(m["local_columns"])
		if len(local) == 0 {
			local = []string{name}
		}
		k := format(strings.Join(local, ","), fmt.Sprint(m["table"]), anyStrings(m["columns"]), fmt.Sprint(m["on_delete"]), fmt.Sprint(m["on_update"]))
		got = append(got, k)
		if n, ok := m["name"].(string); ok && !c.run.CrossDialect {
			names[k] = n
		}
	}
	catNames := map[string]string{}
	for _, fk := range ct.ForeignKeys {
		holds := false
		for _, n := range fk.Columns {
			holds = holds || n == name
		}
		if !holds {
			continue
		}
		k := format(strings.Join(fk.Columns, ","), fk.RefTable, fk.RefColumns, fk.OnDelete, fk.OnUpdate)
		want = append(want, k)
		catNames[k] = fk.Name
	}
	sort.Strings(got)
	sort.Strings(want)
	ok := reflect.DeepEqual(got, want) || len(got) == 0 && len(want) == 0
	for k, n := range names {
		ok = ok && catNames[k] == n
	}
	return ok, fmt.Sprintf("%v (names %v), catalog %v (names %v)", got, names, want, catNames)
}
