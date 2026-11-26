package sqlmodel

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// oracleColumn and oracleTable mirror scripts/oracle/sqlmodel_metadata.py.
type oracleColumn struct {
	Type          string           `json:"type"`
	Length        any              `json:"length"`
	Timezone      any              `json:"timezone"`
	Precision     any              `json:"precision"`
	Scale         any              `json:"scale"`
	Nullable      bool             `json:"nullable"`
	PrimaryKey    bool             `json:"primary_key"`
	Index         bool             `json:"index"`
	Unique        bool             `json:"unique"`
	Default       string           `json:"default"`
	ServerDefault bool             `json:"server_default"`
	ForeignKeys   []map[string]any `json:"foreign_keys"`
}

type oracleTable struct {
	Name    string                  `json:"name"`
	Schema  *string                 `json:"schema"`
	Columns map[string]oracleColumn `json:"columns"`
	// PrimaryKey is the table's primary-key columns, sorted (Rev 3.1); nil
	// in output written before it was added.
	PrimaryKey        []string         `json:"primary_key"`
	Indexes           []map[string]any `json:"indexes"`
	UniqueConstraints []map[string]any `json:"unique_constraints"`
	CheckConstraints  []map[string]any `json:"check_constraints"`
}

// oracleV7 lists the fixture classes whose claim the oracle contradicts by
// decision: their module fails to import, or their table depends on other
// classes, and assumption V7 covers it (T018c1 Revision 2; the forms have no
// corpus use). The comparison reports them separately, never silently.
var oracleV7 = map[string]string{
	"fail_a5_later.py#Early":                 "A5: alias bound after the class (string annotation), the module fails",
	"rv_c2_dup_name.py#Second":               "duplicate table name in one metadata",
	"rv_c2_shared_sa_column.py#SecondShared": "shared sa_column Column object",
	"rv_c2_shared_table_args.py#SecondArgs":  "shared __table_args__ objects",
}

// TestModelOracle compares the claims for a repository with SQLAlchemy's
// MetaData (T018c2, Oracle gate). Set GRAPHVEIN_MODEL_ORACLE to the
// harness output and GRAPHVEIN_MODEL_ORACLE_ROOT to the repository.
func TestModelOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_MODEL_ORACLE"), os.Getenv("GRAPHVEIN_MODEL_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_MODEL_ORACLE or GRAPHVEIN_MODEL_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Tables map[string]oracleTable `json:"tables"`
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".venv") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".py") {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			sources[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges, _ := linkSources(t, sources)
	report := compareOracle(nodes, edges, oracle.Tables)
	for _, line := range report.v7 {
		t.Logf("V7 (by decision): %s", line)
	}
	for _, line := range report.disagreements {
		t.Errorf("disagreement: %s", line)
	}
	t.Logf("oracle tables %d, oracle columns %d; claimed tables %d, complete column sets %d, complete tables %d, columns read %d, column facts known %d/%d, table-args elements %d, disagreements %d",
		len(oracle.Tables), report.oracleColumns, report.tables, report.completeColumns, report.complete, report.columnsRead, report.factsKnown, report.factsTotal, report.elements, len(report.disagreements))
}

type oracleReport struct {
	disagreements, v7                                        []string
	tables, completeColumns, complete, columnsRead, elements int
	factsKnown, factsTotal, oracleColumns                    int
}

var comparedFacts = []string{"primary_key", "nullable", "index", "unique", "foreign_keys", "default", "server_default", "type"}

func compareOracle(nodes []contract.Node, edges []contract.Edge, tables map[string]oracleTable) oracleReport {
	var r oracleReport
	for _, ot := range tables {
		r.oracleColumns += len(ot.Columns)
	}
	columns := map[string][]contract.Node{}
	byID := map[string]contract.Node{}
	for _, n := range nodes {
		byID[n.Identity] = n
	}
	for _, e := range edges {
		if e.Kind == "contains" {
			if c, ok := byID[e.Target]; ok && c.Kind == "column" {
				columns[e.Source] = append(columns[e.Source], c)
			}
		}
	}
	for _, n := range nodes {
		if n.Kind != "table" {
			continue
		}
		key := n.File + "#" + n.QualifiedName
		var bad []string
		fail := func(format string, args ...any) { bad = append(bad, key+": "+fmt.Sprintf(format, args...)) }
		ot, ok := tables[key]
		r.tables++
		a := n.Attributes
		if a["column_set"] == "complete" {
			r.completeColumns++
		}
		if a["state"] == "complete" {
			r.complete++
		}
		if !ok {
			fail("claimed table %s is not in the oracle", n.Name)
		} else {
			if ot.Name != n.Name {
				fail("table name %q, oracle %q", n.Name, ot.Name)
			}
			if a["schema_known"] == true && ot.Schema != nil {
				fail("schema claimed default, oracle %s", *ot.Schema)
			}
			names := map[string]bool{}
			for _, c := range columns[n.Identity] {
				names[c.Name] = true
				compareColumn(c, ot, fail, &r)
			}
			if a["column_set"] == "complete" {
				for name := range ot.Columns {
					if !names[name] {
						fail("column set complete but the oracle also has %s", name)
					}
				}
			}
			if pk, isList := a["primary_key"].([]string); isList && ot.PrimaryKey != nil {
				got := append([]string(nil), pk...)
				sort.Strings(got)
				if fmt.Sprint(got) != fmt.Sprint(ot.PrimaryKey) {
					fail("primary_key %v, oracle %v", got, ot.PrimaryKey)
				}
			}
			compareConstraints(n, columns[n.Identity], ot, fail, &r)
		}
		if reason, v7 := oracleV7[key]; v7 {
			if len(bad) == 0 {
				r.disagreements = append(r.disagreements, key+": listed as V7 but agrees with the oracle")
			}
			r.v7 = append(r.v7, fmt.Sprintf("%s (%s): %d differences", key, reason, len(bad)))
			continue
		}
		r.disagreements = append(r.disagreements, bad...)
	}
	sort.Strings(r.disagreements)
	sort.Strings(r.v7)
	return r
}

func compareColumn(c contract.Node, ot oracleTable, fail func(string, ...any), r *oracleReport) {
	oc, ok := ot.Columns[c.Name]
	a := c.Attributes
	if !ok {
		fail("claimed column %s is not in the oracle", c.Name)
		return
	}
	if a["state"] == "read" {
		r.columnsRead++
	}
	for _, f := range comparedFacts {
		r.factsTotal++
		if a[f] != nil {
			r.factsKnown++
		}
	}
	for f, want := range map[string]bool{"primary_key": oc.PrimaryKey, "nullable": oc.Nullable, "index": oc.Index, "unique": oc.Unique, "server_default": oc.ServerDefault} {
		if v, isBool := a[f].(bool); isBool && v != want {
			fail("%s.%s = %v, oracle %v", c.Name, f, v, want)
		}
	}
	if d, isString := a["default"].(string); isString && d != oc.Default {
		fail("%s.default %s, oracle %s", c.Name, d, oc.Default)
	}
	if ty, isString := a["type"].(string); isString {
		if ty != oc.Type {
			fail("%s.type %s, oracle %s", c.Name, ty, oc.Type)
		}
		for _, f := range []string{"length", "timezone", "precision", "scale"} {
			got := a[f]
			want := map[string]any{"length": oc.Length, "timezone": oc.Timezone, "precision": oc.Precision, "scale": oc.Scale}[f]
			switch got {
			case nil:
			case "none":
				if want != nil {
					fail("%s.%s none, oracle %v", c.Name, f, want)
				}
			default:
				if fmt.Sprint(got) != fmt.Sprint(want) {
					fail("%s.%s %v, oracle %v", c.Name, f, got, want)
				}
			}
		}
	}
	if fks, isList := a["foreign_keys"].([]map[string]any); isList {
		var got, want []string
		for _, fk := range fks {
			got = append(got, fmt.Sprint(fk["schema"], fk["table"], fk["column"], fk["on_delete"]))
		}
		for _, fk := range oc.ForeignKeys {
			want = append(want, fmt.Sprint(fk["schema"], fk["table"], fk["column"], fk["on_delete"]))
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			fail("%s.foreign_keys %v, oracle %v", c.Name, got, want)
		}
	}
}

func compareConstraints(n contract.Node, cols []contract.Node, ot oracleTable, fail func(string, ...any), r *oracleReport) {
	a := n.Attributes
	ixs, _ := a["indexes"].([]map[string]any)
	uqs, _ := a["unique_constraints"].([]map[string]any)
	cks, _ := a["check_constraints"].([]map[string]any)
	r.elements += len(ixs) + len(uqs) + len(cks)
	find := func(list []map[string]any, name any) map[string]any {
		for _, x := range list {
			if fmt.Sprint(x["name"]) == fmt.Sprint(name) {
				return x
			}
		}
		return nil
	}
	for _, ix := range ixs {
		o := find(ot.Indexes, ix["name"])
		if o == nil {
			fail("index %v not in the oracle", ix["name"])
			continue
		}
		if o["unique"] != ix["unique"] {
			fail("index %v unique %v, oracle %v", ix["name"], ix["unique"], o["unique"])
		}
		if ix["columns"] != nil && fmt.Sprint(ix["columns"]) != fmt.Sprint(o["columns"]) {
			fail("index %v columns %v, oracle %v", ix["name"], ix["columns"], o["columns"])
		}
	}
	for _, uq := range uqs {
		found := false
		for _, o := range ot.UniqueConstraints {
			if fmt.Sprint(o["columns"]) == fmt.Sprint(uq["columns"]) && (uq["name"] == nil || fmt.Sprint(o["name"]) == fmt.Sprint(uq["name"])) {
				found = true
			}
		}
		if !found {
			fail("unique constraint %v %v not in the oracle", uq["name"], uq["columns"])
		}
	}
	for _, ck := range cks {
		var o map[string]any
		if ck["name"] != nil {
			o = find(ot.CheckConstraints, ck["name"])
		} else if len(ot.CheckConstraints) > 0 {
			o = ot.CheckConstraints[0]
		}
		if o == nil {
			fail("check constraint %v not in the oracle", ck["name"])
		} else if ck["sqltext"] != nil && ck["sqltext"] != o["sqltext"] {
			fail("check constraint %v text %v, oracle %v", ck["name"], ck["sqltext"], o["sqltext"])
		}
	}
	if a["constraint_set"] != "complete" {
		return
	}
	indexCols, uniqueCols := 0, 0
	for _, c := range cols {
		if c.Attributes["index"] == true {
			indexCols++
		} else if c.Attributes["unique"] == true {
			uniqueCols++
		}
	}
	if len(ot.CheckConstraints) != len(cks) {
		fail("constraint set complete with %d checks, oracle %d", len(cks), len(ot.CheckConstraints))
	}
	if len(ot.UniqueConstraints) != len(uqs)+uniqueCols {
		fail("constraint set complete with %d unique constraints, oracle %d", len(uqs)+uniqueCols, len(ot.UniqueConstraints))
	}
	if len(ot.Indexes) != len(ixs)+indexCols {
		fail("constraint set complete with %d indexes, oracle %d", len(ixs)+indexCols, len(ot.Indexes))
	}
}
