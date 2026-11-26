package typeorm

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

// The dump scripts/oracle/typeorm_catalog.sh writes: one run
// per scratch database, with the model groups it stands for, the TypeORM
// and driver versions it loaded, the error initialize() threw (a TO-P1
// run expects one), TypeORM's own table list and the catalog the
// synchronize (or, for a drift run, the migrations) left.
type toOracleDump struct {
	Runs []toOracleRun `json:"runs"`
}

type toOracleRun struct {
	Name          string            `json:"name"`
	Repo          string            `json:"repo"`
	Groups        []string          `json:"groups"`
	Dialect       string            `json:"dialect"`
	Driver        string            `json:"driver"`
	Cross         bool              `json:"cross"`
	Mode          string            `json:"mode"`
	Pair          string            `json:"pair"`
	ExpectFailure string            `json:"expect_failure"`
	TypeORM       string            `json:"typeorm"`
	TypeScript    string            `json:"typescript"`
	Drivers       map[string]string `json:"drivers"`
	LoadErrors    []struct {
		File, Error string
	} `json:"load_errors"`
	InitError *string `json:"init_error"`
	Harness   struct {
		Stubs []string `json:"stubs"`
	} `json:"harness"`
	Catalog *toCatalog `json:"catalog"`
}

type toCatalog struct {
	Tables []toCatTable `json:"tables"`
	Enums  []struct {
		Schema string   `json:"schema"`
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"enums"`
}

type toCatTable struct {
	Schema  *string `json:"schema"`
	Name    string  `json:"name"`
	SQL     *string `json:"sql"`
	Columns []struct {
		Name          string  `json:"name"`
		Type          string  `json:"type"`
		NotNull       bool    `json:"not_null"`
		Default       *string `json:"default"`
		Identity      string  `json:"identity"`
		Autoincrement bool    `json:"autoincrement"`
	} `json:"columns"`
	PrimaryKey []string `json:"primary_key"`
	Uniques    []struct {
		Name    *string  `json:"name"`
		Columns []string `json:"columns"`
	} `json:"uniques"`
	ForeignKeys []struct {
		Name       *string  `json:"name"`
		Columns    []string `json:"columns"`
		RefSchema  *string  `json:"ref_schema"`
		RefTable   string   `json:"ref_table"`
		RefColumns []string `json:"ref_columns"`
		OnDelete   string   `json:"on_delete"`
		OnUpdate   string   `json:"on_update"`
	} `json:"foreign_keys"`
	Checks []struct {
		Name *string `json:"name"`
		Def  string  `json:"def"`
	} `json:"checks"`
	Indexes []struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
		Unique  bool     `json:"unique"`
		Partial bool     `json:"partial"`
	} `json:"indexes"`
}

// toCrossFacts are the column facts a cross-dialect run compares (a group
// that claims MySQL, run on SQLite or a type-substituted Postgres copy,
// TA2 and TA3): the provider-independent ones.
var toCrossFacts = map[string]bool{"name": true, "nullable": true, "primary_key": true, "unique": true, "index": true, "foreign_keys": true}

// toLabel is one oracle-labelled fact on one dialect: a table fact (fact
// "table") or a column claim.
type toLabel struct{ id, fact, dialect string }

// toLinked is the claim pass over one repository, indexed for the runs.
type toLinked struct {
	tables  map[string][]contract.Node // by set
	columns map[string][]contract.Node // by table identity
	// required are the oracle labels of every group of the repository:
	// each must be compared by a run on its dialect.
	required map[toLabel]bool
	// forms are the default forms (oracleDefaultForms) of the columns'
	// known defaults on oracle dialects, by column identity and dialect.
	forms map[string]string
}

// TestTypeORMOracle compares every claimed fact of the linked repository
// with the catalog TypeORM's own synchronize left in a scratch database
// (the T018g6 task record (internal), "Oracle design"): per
// table its existence, the column set when complete, the primary key (in
// order), each unique constraint (columns, and the name when claimed),
// each index (columns, unique, partial, name), each check (the name; on
// sqlite the expression), and, when the constraint set is complete, that
// the catalog has no other; per read column every claim that applies to
// the run's dialect: its name, type (with length, precision and scale),
// timezone, nullability, primary key membership, autoincrement, the
// single-column unique and index facts, the default (presence, and the
// text after stripping the catalog's casts and quotes), the enum labels
// and the foreign keys (columns, target, actions, name when claimed). A
// claimed fact the catalog lacks or contradicts is a disagreement
// (precision stop S1). A run on a dialect its groups do not claim (cross)
// compares the provider-independent facts only. Every `oracle` label of
// every group of a repository in the dump must be compared by a run on
// that dialect. A TO-P1 run must fail with its expected error, and the
// group must claim nothing on that dialect. A migrations run (seerr's
// drift check, TO-V2) is diffed with its synchronize run and the diff is
// logged. Set GRAPHVEIN_TYPEORM_ORACLE to the dump and
// GRAPHVEIN_TYPEORM_ORACLE_ROOT to the directory the runs' repo paths are
// relative to (the fixture directory, or the corpus checkouts' parent).
func TestTypeORMOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_TYPEORM_ORACLE"), os.Getenv("GRAPHVEIN_TYPEORM_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_TYPEORM_ORACLE or GRAPHVEIN_TYPEORM_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump toOracleDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Runs) == 0 {
		t.Fatal("no runs in the dump")
	}
	repos := map[string]*toLinked{}
	var order []string
	compared := map[toLabel]bool{}
	formsCompared := map[string]bool{}
	byName := map[string]toOracleRun{}
	for _, run := range dump.Runs {
		byName[run.Name] = run
	}
	for _, run := range dump.Runs {
		l, ok := repos[run.Repo]
		if !ok {
			l = linkForOracle(t, filepath.Join(root, run.Repo))
			repos[run.Repo] = l
			order = append(order, run.Repo)
		}
		t.Run(run.Name, func(t *testing.T) {
			switch {
			case run.ExpectFailure != "":
				checkFailureRun(t, run, l)
			case run.Mode == "migrations":
				logDrift(t, run, byName[run.Pair])
			default:
				c := &toComparison{t: t, run: run, l: l, compared: compared, formsCompared: formsCompared}
				c.compareRun()
				t.Logf("%s (%s via %s, typeorm %s, typescript %s%s): %d tables, %d read columns, %d facts compared, %d disagreements, %d facts unknown",
					run.Name, run.Dialect, run.Driver, run.TypeORM, run.TypeScript, crossNote(run), c.tables, c.columns, c.facts, len(c.disagreements), c.unknown)
				for _, d := range c.disagreements {
					t.Errorf("disagreement: %s", d)
				}
			}
		})
	}
	for _, repo := range order {
		var missing []string
		for lb := range repos[repo].required {
			if !compared[lb] {
				missing = append(missing, fmt.Sprintf("%s %s on %s", lb.id, lb.fact, lb.dialect))
			}
		}
		sort.Strings(missing)
		for _, m := range missing {
			t.Errorf("%s: oracle label not compared on its dialect: %s", repo, m)
		}
		t.Logf("%s: %d oracle labels, %d not compared", repo, len(repos[repo].required), len(missing))
	}
	// Revision 4 B1: every default form the claim pass labels oracle
	// (oracleDefaultForms) must be compared by a fixture run on its
	// dialect.
	if filepath.Base(filepath.Clean(root)) == "typeorm-models" {
		var uncompared []string
		for d, forms := range oracleDefaultForms {
			for f := range forms {
				if !formsCompared[d+" "+f] {
					uncompared = append(uncompared, f+" on "+d)
				}
			}
		}
		sort.Strings(uncompared)
		for _, u := range uncompared {
			t.Errorf("default form %s is labelled oracle but no fixture run compares it", u)
		}
		t.Logf("default forms compared: %d", len(formsCompared))
	}
}

func crossNote(run toOracleRun) string {
	if run.Cross {
		return ", cross-dialect: provider-independent facts"
	}
	return ""
}

func toAttrs(t *testing.T, n contract.Node) map[string]any {
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

// liveDialects are the dialects a table claims for.
func liveDialects(a map[string]any) []string {
	if d, ok := a["dialect"].(string); ok && d != "" {
		return []string{d}
	}
	return toStrings(a["dialects"])
}

func toStrings(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, x := range list {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func linkForOracle(t *testing.T, dir string) *toLinked {
	t.Helper()
	forms := map[string]string{}
	defaultFormHook = func(column, dialect, form string) { forms[column+"|"+dialect] = form }
	nodes, _, _ := Link(corpusInput(t, dir))
	defaultFormHook = nil
	l := &toLinked{tables: map[string][]contract.Node{}, columns: map[string][]contract.Node{}, required: map[toLabel]bool{}, forms: forms}
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			a := toAttrs(t, n)
			byID[n.Identity] = a
			set := fmt.Sprint(a["set"])
			l.tables[set] = append(l.tables[set], n)
			if a["verification"] == models.VerificationOracle {
				for _, d := range liveDialects(a) {
					l.required[toLabel{n.Identity, "table", d}] = true
				}
			}
		}
	}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		// The table whose identity, as a column prefix, is the longest (a
		// shared entity's model segment holds dots).
		tid := ""
		for id := range byID {
			if strings.HasPrefix(n.Identity, "column:"+strings.TrimPrefix(id, "table:")+".") && len(id) > len(tid) {
				tid = id
			}
		}
		ta, ok := byID[tid]
		if !ok {
			t.Fatalf("column %s has no table %s", n.Identity, tid)
		}
		l.columns[tid] = append(l.columns[tid], n)
		a := toAttrs(t, n)
		for _, cl := range claimList(a) {
			if cl.verification != models.VerificationOracle {
				continue
			}
			if cl.dialect != "" {
				l.required[toLabel{n.Identity, cl.fact, cl.dialect}] = true
				continue
			}
			for _, d := range liveDialects(ta) {
				l.required[toLabel{n.Identity, cl.fact, d}] = true
			}
		}
	}
	return l
}

type toClaim struct{ fact, dialect, verification, row string }

func claimList(a map[string]any) []toClaim {
	var out []toClaim
	list, _ := a["claims"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		d, _ := m["dialect"].(string)
		out = append(out, toClaim{fmt.Sprint(m["fact"]), d, fmt.Sprint(m["verification"]), fmt.Sprint(m["row"])})
	}
	return out
}

// checkFailureRun checks a TO-P1 run: initialize() (or loading the
// entity module) failed with the expected error, and no group of the run
// claims a table on that dialect.
func checkFailureRun(t *testing.T, run toOracleRun, l *toLinked) {
	msg := ""
	if run.InitError != nil {
		msg = *run.InitError
	}
	for _, e := range run.LoadErrors {
		msg += " | " + e.Error
	}
	if !strings.Contains(msg, run.ExpectFailure) {
		t.Errorf("%s: want a failure containing %q, got %q", run.Name, run.ExpectFailure, msg)
	}
	for _, g := range run.Groups {
		for _, n := range l.tables[g] {
			for _, d := range liveDialects(toAttrs(t, n)) {
				if d == run.Dialect {
					t.Errorf("%s: %s claims %s on %s, where initialize() fails", run.Name, g, n.Identity, d)
				}
			}
		}
	}
	t.Logf("%s (%s, typeorm %s): fails as TO-P1 says: %s", run.Name, run.Dialect, run.TypeORM, strings.TrimPrefix(msg, " | "))
}

// toComparison holds one run's comparison.
type toComparison struct {
	t             *testing.T
	run           toOracleRun
	l             *toLinked
	compared      map[toLabel]bool
	formsCompared map[string]bool
	tables        int
	columns       int
	facts         int
	unknown       int
	disagreements []string
}

func (c *toComparison) check(ok bool, format string, a ...any) {
	c.facts++
	if !ok {
		c.disagreements = append(c.disagreements, fmt.Sprintf(format, a...))
	}
}

func (c *toComparison) key(schema, name string) string {
	if c.run.Dialect != "postgres" {
		return name
	}
	if schema == "" {
		schema = "public"
	}
	return schema + "." + name
}

func (c *toComparison) compareRun() {
	t := c.t
	if c.run.Catalog == nil {
		msg := "the run has no catalog"
		if c.run.InitError != nil {
			msg += ": " + *c.run.InitError
		}
		t.Fatal(msg)
	}
	if len(c.run.LoadErrors) > 0 {
		t.Fatalf("load errors: %v", c.run.LoadErrors)
	}
	cat := map[string]*toCatTable{}
	for i := range c.run.Catalog.Tables {
		ct := &c.run.Catalog.Tables[i]
		cat[c.key(deref(ct.Schema), ct.Name)] = ct
	}
	enums := map[string][]string{}
	for _, e := range c.run.Catalog.Enums {
		enums[e.Schema+"."+e.Name] = e.Values
	}
	matched := map[string]bool{}
	for _, g := range c.run.Groups {
		tables := c.l.tables[g]
		if len(tables) == 0 {
			t.Fatalf("no table records in set %s", g)
		}
		for _, n := range tables {
			a := toAttrs(t, n)
			if pins, _ := a["pins"].(map[string]any); pins["typeorm"] != c.run.TypeORM {
				t.Fatalf("%s: pins %v, the run loaded typeorm %s", n.Identity, pins, c.run.TypeORM)
			}
			native := false
			for _, d := range liveDialects(a) {
				native = native || d == c.run.Dialect
			}
			if native == c.run.Cross {
				t.Fatalf("%s: claimed for %v; the run is on %s (cross %v)", n.Identity, liveDialects(a), c.run.Dialect, c.run.Cross)
			}
			if a["synchronize"] == false {
				// TO-E1 synchronize: false: TypeORM does not create the
				// table (mapped, Revision 1 O4).
				c.unknown++
				continue
			}
			c.tables++
			schema, _ := a["schema"].(string)
			key := c.key(schema, fmt.Sprint(a["name"]))
			ct := cat[key]
			c.check(ct != nil, "%s: table %s is not in the catalog", n.Identity, key)
			if ct == nil {
				continue
			}
			matched[key] = true
			if native && a["verification"] == models.VerificationOracle {
				c.compared[toLabel{n.Identity, "table", c.run.Dialect}] = true
			}
			c.compareTable(n.Identity, a, ct, native, enums)
		}
	}
	for key := range cat {
		if !matched[key] {
			t.Logf("catalog table %s has no claimed record", key)
		}
	}
}

func catColumns(ct *toCatTable) map[string]int {
	out := map[string]int{}
	for i, col := range ct.Columns {
		out[col.Name] = i
	}
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

var spaces = regexp.MustCompile(`\s+`)

func (c *toComparison) compareTable(id string, a map[string]any, ct *toCatTable, native bool, enums map[string][]string) {
	complete := a["constraint_set"] == "complete" && native
	var catNames []string
	for _, col := range ct.Columns {
		catNames = append(catNames, col.Name)
	}
	if a["column_set"] == "complete" {
		var names []string
		for _, n := range c.l.columns[id] {
			names = append(names, n.Name)
		}
		c.check(reflect.DeepEqual(sorted(names), sorted(catNames)), "%s: columns %v, catalog %v", id, sorted(names), sorted(catNames))
	}
	if a["primary_key"] != nil {
		pk := toStrings(a["primary_key"])
		c.check(reflect.DeepEqual(pk, ct.PrimaryKey), "%s: primary key %v, catalog %v", id, pk, ct.PrimaryKey)
	}
	// Unique constraints.
	list, _ := a["unique_constraints"].([]any)
	claimedU := map[string]bool{}
	for _, x := range list {
		m, _ := x.(map[string]any)
		cols := strings.Join(toStrings(m["columns"]), ",")
		claimedU[cols] = true
		found := false
		for _, u := range ct.Uniques {
			if strings.Join(u.Columns, ",") != cols {
				continue
			}
			found = true
			if name, ok := m["name"].(string); ok && native {
				c.check(deref(u.Name) == name, "%s: unique (%s) name %s, catalog %s", id, cols, name, deref(u.Name))
			}
		}
		c.check(found, "%s: unique (%s) is not a unique constraint of the catalog", id, cols)
	}
	// Indexes.
	list, _ = a["indexes"].([]any)
	claimedI := map[string]bool{}
	// The index MySQL's InnoDB adds for a foreign key (Revision 4 A3,
	// row TO-R1) exists on MySQL only: a cross-dialect run skips it.
	fkCols := map[string]bool{}
	for _, n := range c.l.columns[id] {
		for _, fk := range anyList(toAttrs(c.t, n)["foreign_keys"]) {
			m, _ := fk.(map[string]any)
			fkCols[fmt.Sprint(toStrings(m["local_columns"]))] = true
		}
	}
	for _, x := range list {
		m, _ := x.(map[string]any)
		cols := toStrings(m["columns"])
		if !native && m["unique"] == false && fkCols[fmt.Sprint(cols)] && !c.hasIndex(ct, cols) {
			continue
		}
		name, named := m["name"].(string)
		idx := -1
		for i, ix := range ct.Indexes {
			if named && native {
				if ix.Name == name {
					idx = i
				}
			} else if reflect.DeepEqual(ix.Columns, cols) && ix.Unique == m["unique"] && !claimedI[ix.Name] {
				idx = i
			}
		}
		c.check(idx >= 0, "%s: index %v %v is not in the catalog", id, m["name"], cols)
		if idx < 0 {
			continue
		}
		ix := ct.Indexes[idx]
		claimedI[ix.Name] = true
		c.check(reflect.DeepEqual(ix.Columns, cols), "%s: index %s columns %v, catalog %v", id, ix.Name, cols, ix.Columns)
		c.check(m["unique"] == ix.Unique, "%s: index %s unique %v, catalog %v", id, ix.Name, m["unique"], ix.Unique)
		c.check(m["partial"] == ix.Partial, "%s: index %s partial %v, catalog %v", id, ix.Name, m["partial"], ix.Partial)
	}
	// Checks: the name; on sqlite also the expression as TypeORM writes it.
	list, _ = a["check_constraints"].([]any)
	if native {
		used := map[int]bool{}
		for _, x := range list {
			m, _ := x.(map[string]any)
			name, named := m["name"].(string)
			expr := spaces.ReplaceAllString(fmt.Sprint(m["expression"]), "")
			idx := -1
			for i, ch := range ct.Checks {
				if used[i] {
					continue
				}
				switch {
				case named && deref(ch.Name) == name:
					idx = i
				case !named && ch.Name == nil && spaces.ReplaceAllString(ch.Def, "") == expr:
					idx = i
				}
			}
			c.check(idx >= 0, "%s: check %v (%v) is not in the catalog", id, m["name"], m["expression"])
			if idx < 0 {
				continue
			}
			used[idx] = true
			if c.run.Dialect == "sqlite" {
				c.check(spaces.ReplaceAllString(ct.Checks[idx].Def, "") == expr, "%s: check %v expression %v, catalog %s", id, m["name"], m["expression"], ct.Checks[idx].Def)
			}
		}
		if complete {
			c.check(len(list) == len(ct.Checks), "%s: %d checks, catalog %d", id, len(list), len(ct.Checks))
		}
	}
	if complete {
		for _, u := range ct.Uniques {
			k := strings.Join(u.Columns, ",")
			c.check(claimedU[k], "%s: the catalog's unique (%s) is not claimed, the constraint set is complete", id, k)
		}
		for _, ix := range ct.Indexes {
			c.check(claimedI[ix.Name], "%s: the catalog's index %s %v is not claimed, the constraint set is complete", id, ix.Name, ix.Columns)
		}
	}
	cols := catColumns(ct)
	for _, n := range c.l.columns[id] {
		ca := toAttrs(c.t, n)
		if ca["state"] != "read" {
			c.unknown++
			continue
		}
		c.unknown += toCountUnknown(ca)
		c.compareColumn(n.Identity, ca, ct, cols, native, enums)
	}
	for _, r := range anyList(a["reasons"]) {
		if m, ok := r.(map[string]any); ok && m["fact"] != nil {
			c.unknown++
		}
	}
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func toCountUnknown(col map[string]any) int {
	n := 0
	for _, r := range anyList(col["reasons"]) {
		if m, ok := r.(map[string]any); ok && m["fact"] != nil {
			n++
		}
	}
	return n
}

// hasIndex reports a catalog index on exactly cols.
func (c *toComparison) hasIndex(ct *toCatTable, cols []string) bool {
	for _, ix := range ct.Indexes {
		if reflect.DeepEqual(ix.Columns, cols) {
			return true
		}
	}
	return false
}

// fkKey renders a foreign key as both sides are compared.
func fkKey(local []string, schema, table string, refs []string, del, upd string) string {
	return fmt.Sprintf("(%s) -> %s.%s(%s) %s/%s", strings.Join(local, ","), schema, table, strings.Join(refs, ","), del, upd)
}

// toPgCast matches a trailing cast Postgres adds to a rendered default.
var toPgCast = regexp.MustCompile(`::[a-z_ "A-Z.]+(\([0-9, ]+\))?(\[\])?$`)

// toNormDefault strips the catalog's casts, one level of outer
// parentheses and quotes ("Oracle design"); ok is false for no default.
func toNormDefault(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	v := *s
	for {
		w := toPgCast.ReplaceAllString(v, "")
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
	return toUnquote(v), true
}

func toUnquote(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// sameDefaultText compares a claimed default text with the catalog's after
// stripping; json and jsonb defaults are compared as JSON values (Postgres
// keeps jsonb in its own canonical form).
func sameDefaultText(typ, claimed, catalog string) bool {
	if claimed == catalog {
		return true
	}
	if !strings.HasPrefix(typ, "json") {
		return false
	}
	var a, b any
	return json.Unmarshal([]byte(claimed), &a) == nil && json.Unmarshal([]byte(catalog), &b) == nil && reflect.DeepEqual(a, b)
}

// factValue reads a column fact on the run's dialect: by_dialect's entry
// when the claim is per dialect, else the column attribute.
func factValue(col map[string]any, fact, dialect string) any {
	if dialect != "" {
		if bd, ok := col["by_dialect"].(map[string]any); ok {
			if m, ok := bd[dialect].(map[string]any); ok {
				return m[fact]
			}
		}
		return nil
	}
	return col[fact]
}

func number(v any) (int, bool) {
	f, ok := v.(float64)
	return int(f), ok
}

func (c *toComparison) compareColumn(colID string, col map[string]any, ct *toCatTable, cols map[string]int, native bool, enums map[string][]string) {
	c.columns++
	name := fmt.Sprint(col["name"])
	i, ok := cols[name]
	c.check(ok, "%s: column %s is not in the catalog", colID, name)
	if !ok {
		return
	}
	cc := ct.Columns[i]
	d := c.run.Dialect
	typ := strings.ToLower(cc.Type)
	inPK := false
	for _, p := range ct.PrimaryKey {
		inPK = inPK || p == name
	}
	text, present := toNormDefault(cc.Default)
	if d == "postgres" && cc.Identity != "" {
		present = true
	}
	// The single-column unique and index facts as the claim pass derives
	// them (constraints.go): a unique constraint or a non-partial unique
	// index on exactly this column; a non-unique index on exactly it.
	unique, index := false, false
	for _, u := range ct.Uniques {
		unique = unique || len(u.Columns) == 1 && u.Columns[0] == name
	}
	for _, ix := range ct.Indexes {
		if len(ix.Columns) == 1 && ix.Columns[0] == name {
			unique = unique || ix.Unique && !ix.Partial
			index = index || !ix.Unique
		}
	}
	for _, cl := range claimList(col) {
		if cl.dialect != "" && cl.dialect != d {
			continue
		}
		if !native && (cl.dialect != "" || !toCrossFacts[cl.fact]) {
			continue
		}
		v := factValue(col, cl.fact, cl.dialect)
		switch cl.fact {
		case "name":
			c.check(true, "")
		case "type":
			c.check(strings.ToLower(fmt.Sprint(v)) == typ, "%s: type %v on %s, catalog %s", colID, v, d, cc.Type)
			c.compareEnum(colID, col, name, typ, ct, enums)
		case "type_family":
			c.check(v == nil || strings.HasPrefix(typ, strings.ToLower(fmt.Sprint(v))), "%s: type family %v, catalog %s", colID, v, cc.Type)
		case "length":
			if n, ok := number(v); ok {
				c.check(strings.Contains(typ, fmt.Sprintf("(%d)", n)), "%s: length %d, catalog %s", colID, n, cc.Type)
			} else {
				c.check(!strings.Contains(typ, "("), "%s: length %v, catalog %s", colID, v, cc.Type)
			}
		case "precision", "scale":
			p, okp := number(factValue(col, "precision", cl.dialect))
			s, oks := number(factValue(col, "scale", cl.dialect))
			switch {
			case okp && oks:
				c.check(strings.Contains(typ, fmt.Sprintf("(%d,%d)", p, s)), "%s: precision %d scale %d, catalog %s", colID, p, s, cc.Type)
			case okp:
				c.check(strings.Contains(typ, fmt.Sprintf("(%d)", p)) || strings.Contains(typ, fmt.Sprintf("(%d,", p)), "%s: precision %d, catalog %s", colID, p, cc.Type)
			default:
				c.check(v == nil, "%s: %s %v, catalog %s", colID, cl.fact, v, cc.Type)
			}
		case "timezone":
			c.check(v == strings.Contains(typ, "with time zone"), "%s: timezone %v, catalog %s", colID, v, cc.Type)
		case "nullable":
			c.check(v == !cc.NotNull, "%s: nullable %v, catalog not null %v", colID, v, cc.NotNull)
		case "primary_key":
			c.check(v == inPK, "%s: primary key %v, catalog %v", colID, v, inPK)
		case "autoincrement":
			c.check(v == cc.Autoincrement, "%s: autoincrement %v on %s, catalog %v", colID, v, d, cc.Autoincrement)
		case "unique":
			c.check(v == unique, "%s: unique %v, catalog %v", colID, v, unique)
		case "index":
			if !native && cl.row == "TO-R1" {
				// MySQL's foreign key index (Revision 4 A3): MySQL only.
				continue
			}
			c.check(v == index, "%s: index %v, catalog %v", colID, v, index)
		case "default":
			switch v {
			case "server":
				c.check(present, "%s: default server, catalog none", colID)
			case "absent", "client":
				c.check(!present, "%s: default %v, catalog %v", colID, v, deref(cc.Default))
			default:
				c.check(false, "%s: default %v is not a default kind", colID, v)
			}
		case "server_default":
			c.check(v == present, "%s: server default %v on %s, catalog %v", colID, v, d, deref(cc.Default))
			if s, ok := factValue(col, "server_default_text", cl.dialect).(string); ok {
				claimed, _ := toNormDefault(&s)
				c.check(present && sameDefaultText(typ, claimed, text), "%s: default text %s on %s, catalog %v (%q after stripping)", colID, s, d, deref(cc.Default), text)
			}
		case "foreign_keys":
			c.compareForeignKeys(colID, v, ct, name, native)
		default:
			c.t.Errorf("%s: claim fact %s is not compared", colID, cl.fact)
			continue
		}
		if native && cl.verification == models.VerificationOracle {
			c.compared[toLabel{colID, cl.fact, d}] = true
			if cl.fact == "server_default" {
				form, ok := c.l.forms[colID+"|"+d]
				c.check(ok, "%s: the oracle-labelled default on %s has no recorded form", colID, d)
				c.formsCompared[d+" "+form] = true
			}
		}
	}
}

// compareEnum compares a column's enum labels: postgres the enum type the
// column's type names; sqlite the column's CHECK (col IN (...)).
func (c *toComparison) compareEnum(colID string, col map[string]any, name, typ string, ct *toCatTable, enums map[string][]string) {
	e, ok := col["enum"].(map[string]any)
	if !ok || e["values"] == nil {
		return
	}
	want := toStrings(e["values"])
	switch c.run.Dialect {
	case "postgres":
		key := strings.Trim(typ, `"`)
		if !strings.Contains(key, ".") {
			key = deref(ct.Schema) + "." + key
		}
		key = strings.ReplaceAll(key, `"`, "")
		c.check(reflect.DeepEqual(enums[key], want), "%s: enum %s labels %v, catalog %v", colID, key, want, enums[key])
	case "sqlite":
		var labels []string
		for _, v := range want {
			labels = append(labels, "'"+strings.ReplaceAll(v, "'", "''")+"'")
		}
		expr := `"` + name + `"IN(` + strings.Join(labels, ",") + `)`
		found := false
		for _, ch := range ct.Checks {
			found = found || strings.Contains(spaces.ReplaceAllString(ch.Def, ""), expr)
		}
		c.check(found, "%s: enum labels %v: no CHECK %s in the catalog", colID, want, expr)
	}
}

func (c *toComparison) compareForeignKeys(colID string, v any, ct *toCatTable, name string, native bool) {
	pg := c.run.Dialect == "postgres"
	var got, want []string
	names := map[string]string{}
	for _, x := range anyList(v) {
		m, _ := x.(map[string]any)
		schema, _ := m["schema"].(string)
		if pg && schema == "" {
			schema = "public"
		}
		if !pg {
			schema = ""
		}
		k := fkKey(toStrings(m["local_columns"]), schema, fmt.Sprint(m["table"]), toStrings(m["columns"]), fmt.Sprint(m["on_delete"]), fmt.Sprint(m["on_update"]))
		got = append(got, k)
		if n, ok := m["name"].(string); ok && native {
			names[k] = n
		}
	}
	for _, fk := range ct.ForeignKeys {
		in := false
		for _, col := range fk.Columns {
			in = in || col == name
		}
		if !in {
			continue
		}
		schema := ""
		if pg {
			schema = deref(fk.RefSchema)
		}
		k := fkKey(fk.Columns, schema, fk.RefTable, fk.RefColumns, fk.OnDelete, fk.OnUpdate)
		want = append(want, k)
		if n, ok := names[k]; ok {
			c.check(deref(fk.Name) == n, "%s: foreign key %s name %s, catalog %s", colID, k, n, deref(fk.Name))
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	c.check(reflect.DeepEqual(got, want) || len(got) == 0 && len(want) == 0, "%s: foreign keys %v, catalog %v", colID, got, want)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// logDrift diffs a migrations run's catalog with its synchronize run's
// (TO-V2: the migrations build the schema the code expects) and logs every
// difference: tables, columns (type, nullability, default), primary keys,
// unique constraints, foreign keys and indexes, names included
// (names drift in migration-built databases; generated names are
// claimed only without migrations, O5).
func logDrift(t *testing.T, mig, sync toOracleRun) {
	if mig.Catalog == nil || sync.Catalog == nil {
		t.Fatalf("drift %s: a catalog is missing (init error %v; pair %s)", mig.Name, deref(mig.InitError), sync.Name)
	}
	lines := func(cat *toCatalog) map[string]bool {
		out := map[string]bool{}
		for _, ct := range cat.Tables {
			if ct.Name == "migrations" || ct.Name == "typeorm_metadata" {
				continue
			}
			p := ct.Name + ": "
			out[p+"table"] = true
			for _, col := range ct.Columns {
				out[fmt.Sprintf("%scolumn %s %s notnull=%v default=%s", p, col.Name, strings.ToLower(col.Type), col.NotNull, deref(col.Default))] = true
			}
			out[fmt.Sprintf("%sprimary key %v", p, ct.PrimaryKey)] = true
			for _, u := range ct.Uniques {
				out[fmt.Sprintf("%sunique %v %s", p, u.Columns, deref(u.Name))] = true
			}
			for _, fk := range ct.ForeignKeys {
				out[fmt.Sprintf("%sforeign key %s %s", p, fkKey(fk.Columns, "", fk.RefTable, fk.RefColumns, fk.OnDelete, fk.OnUpdate), deref(fk.Name))] = true
			}
			for _, ix := range ct.Indexes {
				out[fmt.Sprintf("%sindex %s %v unique=%v partial=%v", p, ix.Name, ix.Columns, ix.Unique, ix.Partial)] = true
			}
		}
		return out
	}
	a, b := lines(sync.Catalog), lines(mig.Catalog)
	var diff []string
	for k := range a {
		if !b[k] {
			diff = append(diff, "synchronize only: "+k)
		}
	}
	for k := range b {
		if !a[k] {
			diff = append(diff, "migrations only: "+k)
		}
	}
	sort.Strings(diff)
	for _, d := range diff {
		t.Log(d)
	}
	t.Logf("%s (%s, typeorm %s): drift against %s: %d of %d catalog lines differ", mig.Name, mig.Dialect, mig.TypeORM, sync.Name, len(diff), len(a))
}
