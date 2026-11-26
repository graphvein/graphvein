package diesel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// The dump scripts/oracle/diesel_catalog.sh writes: one run per
// scratch database, with the catalog of the database and the
// information_schema rows diesel_cli's print-schema reads.
type dlOracleDump struct {
	Runs []dlOracleRun `json:"runs"`
}

type dlOracleRun struct {
	Name string `json:"name"`
	// Kind is corpus (the repository's migrations), fixture
	// (scripts/oracle/diesel_fixture.sql) or probe
	// (scripts/oracle/diesel_probe.sql, compared through the print-schema
	// emulation).
	Kind string `json:"kind"`
	// Repo is the directory, relative to GRAPHVEIN_DIESEL_ORACLE_ROOT,
	// whose claim pass the run compares; Sets are the model groups
	// compared (repository-relative schema files).
	Repo       string   `json:"repo"`
	Sets       []string `json:"sets"`
	Migrations struct {
		Applied  int      `json:"applied"`
		Failed   int      `json:"failed"`
		Failures []string `json:"failures"`
	} `json:"migrations"`
	DefaultSchema string     `json:"default_schema"`
	Catalog       dlCatalog  `json:"catalog"`
	Info          dlInfoDump `json:"info"`
}

type dlCatalog struct {
	Relations []struct {
		Schema, Name, Relkind string
	} `json:"relations"`
	Columns []dlCatColumn `json:"columns"`
	// Constraints are the p, u and f constraints with ordered columns.
	Constraints []struct {
		Schema     string   `json:"schema"`
		Table      string   `json:"table"`
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Columns    []string `json:"columns"`
		RefSchema  string   `json:"ref_schema"`
		RefTable   string   `json:"ref_table"`
		RefColumns []string `json:"ref_columns"`
	} `json:"constraints"`
}

type dlCatColumn struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Name   string `json:"name"`
	Num    int    `json:"num"`
	Type   string `json:"type"`
	// Base is format_type with no modifier (atttypid, NULL): what a type
	// family claim names.
	Base      string `json:"base"`
	Typname   string `json:"typname"`
	Typschema string `json:"typschema"`
	Typtype   string `json:"typtype"`
	NotNull   bool   `json:"notnull"`
}

type dlInfoDump struct {
	Tables []struct {
		Schema string `json:"table_schema"`
		Name   string `json:"table_name"`
		Type   string `json:"table_type"`
	} `json:"tables"`
	Columns []struct {
		Schema    string `json:"table_schema"`
		Table     string `json:"table_name"`
		Name      string `json:"column_name"`
		Ordinal   int    `json:"ordinal_position"`
		UDT       string `json:"udt_name"`
		UDTSchema string `json:"udt_schema"`
		Nullable  string `json:"is_nullable"`
		MaxLength *int   `json:"character_maximum_length"`
		Domain    string `json:"domain_name"`
	} `json:"columns"`
	PrimaryKeys []struct {
		Schema string `json:"table_schema"`
		Table  string `json:"table_name"`
		Column string `json:"column_name"`
	} `json:"primary_keys"`
}

// dlKey is a relation's schema-qualified name.
type dlKey struct{ schema, name string }

// dlTally counts one run's comparison per fact.
type dlTally struct {
	agree, disagree map[string]int
	unknown         map[string]int
	drift           []string
	disagreements   []string
}

func newTally() *dlTally {
	return &dlTally{agree: map[string]int{}, disagree: map[string]int{}, unknown: map[string]int{}}
}

func (d *dlTally) check(fact string, ok bool, format string, args ...any) {
	if ok {
		d.agree[fact]++
		return
	}
	d.disagree[fact]++
	d.disagreements = append(d.disagreements, fmt.Sprintf(fact+": "+format, args...))
}

func (d *dlTally) String() string {
	var facts []string
	for f := range d.agree {
		facts = append(facts, f)
	}
	for f := range d.disagree {
		if d.agree[f] == 0 {
			facts = append(facts, f)
		}
	}
	sort.Strings(facts)
	var b strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&b, " %s %d/%d;", f, d.agree[f], d.agree[f]+d.disagree[f])
	}
	var unk []string
	for f, n := range d.unknown {
		unk = append(unk, fmt.Sprintf("%s %d", f, n))
	}
	sort.Strings(unk)
	return fmt.Sprintf("agree/claimed:%s unknown: %s", b.String(), strings.Join(unk, ", "))
}

// TestDieselOracle compares every claimed fact of the Diesel claim pass
// with the catalog of a scratch Postgres 17: per table its
// name and schema, the base-table kind and the column set when complete,
// the primary key; per column its name, nullability, type (format_type's
// spelling, exactly), primary-key membership and its foreign keys (the
// catalog has the constraint from that column to the target's column). A
// claimed fact the catalog lacks or contradicts is a disagreement
// (precision stop S1). Facts withheld on hand-written lines are drift when
// a naive rule would have got them wrong (logged, never claimed). Over a
// dump with the fixture and probe runs every Postgres claim form the
// fixture labels oracle, and every form of oracleForms, must have been
// compared. Set GRAPHVEIN_DIESEL_ORACLE to the dump and
// GRAPHVEIN_DIESEL_ORACLE_ROOT to the directory the runs' repo paths are
// relative to (the fixture directory, or the crates.io checkout).
func TestDieselOracle(t *testing.T) {
	path, root := os.Getenv("GRAPHVEIN_DIESEL_ORACLE"), os.Getenv("GRAPHVEIN_DIESEL_ORACLE_ROOT")
	if path == "" || root == "" {
		t.Skip("GRAPHVEIN_DIESEL_ORACLE or GRAPHVEIN_DIESEL_ORACLE_ROOT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump dlOracleDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump.Runs) == 0 {
		t.Fatal("no runs in the dump")
	}
	compared := map[string]bool{}
	kinds := map[string]bool{}
	for _, run := range dump.Runs {
		kinds[run.Kind] = true
		t.Run(run.Name, func(t *testing.T) {
			if run.Migrations.Failed > 0 {
				t.Errorf("%d migrations failed: %v", run.Migrations.Failed, run.Migrations.Failures)
			}
			var sources map[string][]byte
			sets := run.Sets
			if run.Kind == "probe" {
				sources = emulatedPackage(t, root, run)
				sets = []string{"src/schema.rs", "src/other.rs"}
			} else {
				sources = readTree(t, filepath.Join(root, run.Repo))
			}
			nodes, _, _ := Link(Input{Sources: sources})
			nodes = normalized(t, nodes)
			tally := newTally()
			for _, set := range sets {
				compareGroup(t, run, nodes, set, tally, compared)
			}
			t.Logf("%s (%s, %d migrations): %s", run.Name, run.Kind, run.Migrations.Applied, tally)
			for _, d := range tally.drift {
				t.Logf("drift (not claimed): %s", d)
			}
			for _, d := range tally.disagreements {
				t.Errorf("disagreement: %s", d)
			}
		})
	}
	if !kinds["fixture"] || !kinds["probe"] {
		return
	}
	// Every form the oracle must have compared on Postgres.
	need := slices.Clone(oracleForms)
	fixtureNodes, _, _ := Link(Input{Sources: readTree(t, root)})
	for f := range oracleLabelledForms(normalized(t, fixtureNodes)) {
		if !slices.Contains(need, f) {
			need = append(need, f)
		}
	}
	sort.Strings(need)
	for _, f := range need {
		if !compared[f] {
			t.Errorf("form %s is labelled oracle but no fixture or probe run compared it", f)
		}
	}
	t.Logf("%d oracle forms compared", len(need))
}

// oracleForms are the Postgres claim forms the fixture and probe runs must
// compare (the forms labelled oracle; Revision 1 addendum 3 asks for the
// table #[sql_name], schema-qualified custom types and keyword quoting by
// name): every builtin type of DI-C2 that is claimed without a family, the
// length forms, arrays, sql_types structs with and without a schema, and
// the name, key and foreign-key forms.
var oracleForms = func() []string {
	out := []string{
		"type/Array<T>", "type/sql_types struct", "type/sql_types struct with schema", "family/sql_types struct",
		"name/table", "name/table #[sql_name]", "name/table schema.name", "name/table keyword", "name/table in pub mod",
		"name/column", "name/column #[sql_name]", "name/column keyword",
		"nullable/Nullable<...>", "nullable/not null", "primary_key/single", "primary_key/composite",
		"column_set/complete", "kind/base table", "foreign_keys/joinable!",
	}
	for _, b := range pgBuiltins {
		ident := upperCamel(b.udt)
		if len(b.dropped) > 0 {
			// Type families (Revision 1 O4): the type and its array.
			out = append(out, "family/"+ident, "family/Array<"+ident+">")
			continue
		}
		if b.length != noLength {
			// Arrays of length types and the types without a length.
			out = append(out, "family/Array<"+ident+">")
		}
		if b.length == requiredLength {
			out = append(out, "family/"+ident)
		}
		if b.length != requiredLength {
			out = append(out, "type/"+ident)
		}
		if b.length != noLength {
			out = append(out, "type/"+ident+" #[max_length]")
		}
	}
	return out
}()

// rustKeywords are the identifiers syn refuses as an Ident (diesel_cli
// 2.3.14 inference.rs:14-22, is_reserved_name): print-schema writes such a
// name with a trailing `_` and #[sql_name].
var rustKeywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields("_ abstract as async await become box break const continue crate do dyn else enum extern false final fn for if impl in let loop macro match mod move mut override priv pub ref return Self self static struct super trait true try type typeof unsafe unsized use virtual where while yield") {
		rustKeywords[k] = true
	}
}

// typeForm is the DI-C2 form of a claimed type, from the declared Diesel
// type.
func typeForm(dieselType, claimed string) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(dieselType, "Nullable<"), ">")
	if !strings.HasPrefix(dieselType, "Nullable<") {
		inner = dieselType
	}
	if strings.HasPrefix(inner, "Array<") {
		return "type/Array<T>"
	}
	if pgByIdent[inner] != nil {
		if strings.Contains(claimed, "(") {
			return "type/" + inner + " #[max_length]"
		}
		return "type/" + inner
	}
	if strings.Contains(claimed, ".") {
		return "type/sql_types struct with schema"
	}
	return "type/sql_types struct"
}

// familyForm is the DI-C2 form of a claimed type family, from the
// declared Diesel type: the ident, Array<ident>, or a sql_types struct.
func familyForm(dieselType string) string {
	inner := dieselType
	if strings.HasPrefix(inner, "Nullable<") {
		inner = strings.TrimSuffix(strings.TrimPrefix(inner, "Nullable<"), ">")
	}
	el := strings.TrimSuffix(strings.TrimPrefix(inner, "Array<"), ">")
	if strings.HasPrefix(el, "Nullable<") {
		el = strings.TrimSuffix(strings.TrimPrefix(el, "Nullable<"), ">")
	}
	if !builtinIdent(el) {
		return "family/sql_types struct"
	}
	if strings.HasPrefix(inner, "Array<") {
		return "family/Array<" + el + ">"
	}
	return "family/" + el
}

// builtinIdent reports an ident of the builtin list, the pg_catalog
// types print-schema writes only as a struct (timetz) included.
func builtinIdent(ident string) bool {
	for _, b := range pgBuiltins {
		if upperCamel(b.udt) == ident {
			return true
		}
	}
	return false
}

// oracleLabelledForms lists the forms of the Postgres claims labelled
// oracle.
func oracleLabelledForms(nodes []contract.Node) map[string]bool {
	out := map[string]bool{}
	tables := map[string]contract.Node{}
	for _, n := range nodes {
		if n.Kind == "table" && n.Attributes["dialect"] == "postgres" {
			tables[n.Identity] = n
		}
	}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		tid := "table:" + strings.TrimPrefix(n.Identity[:strings.LastIndex(n.Identity, ".")], "column:")
		if _, ok := tables[tid]; !ok {
			continue
		}
		for _, c := range columnClaims(n) {
			if c["verification"] != models.VerificationOracle {
				continue
			}
			for _, f := range columnForms(tables[tid], n, fmt.Sprint(c["fact"])) {
				out[f] = true
			}
		}
	}
	return out
}

func columnClaims(n contract.Node) []map[string]any {
	var out []map[string]any
	list, _ := n.Attributes["claims"].([]any)
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// normalized returns the nodes with their attributes as JSON reads them
// back (lists as []any, objects as map[string]any).
func normalized(t *testing.T, nodes []contract.Node) []contract.Node {
	t.Helper()
	out := make([]contract.Node, len(nodes))
	for i, n := range nodes {
		data, err := json.Marshal(n.Attributes)
		if err != nil {
			t.Fatal(err)
		}
		var a map[string]any
		if err := json.Unmarshal(data, &a); err != nil {
			t.Fatal(err)
		}
		n.Attributes = a
		out[i] = n
	}
	return out
}

// stringList reads a JSON string list.
func stringList(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, x := range list {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// columnForms names the forms of one column fact (and, for its name, the
// table's name forms).
func columnForms(table, col contract.Node, fact string) []string {
	a := col.Attributes
	switch fact {
	case "name":
		out := []string{"name/column"}
		attr := fmt.Sprint(a["attribute"])
		name := fmt.Sprint(a["name"])
		if col.Identity != "" && !strings.HasSuffix(col.Identity, "."+name) {
			out = append(out, "name/column #[sql_name]")
		}
		if rustKeywords[name] && (attr == "r#"+name || strings.HasSuffix(col.Identity, "."+name+"_")) {
			out = append(out, "name/column keyword")
		}
		return append(out, tableForms(table)...)
	case "nullable":
		if a["nullable"] == true {
			return []string{"nullable/Nullable<...>"}
		}
		return []string{"nullable/not null"}
	case "type":
		return []string{typeForm(fmt.Sprint(a["tool_type"]), fmt.Sprint(a["type"]))}
	case "type_family":
		return []string{familyForm(fmt.Sprint(a["tool_type"]))}
	case "primary_key":
		if len(stringList(table.Attributes["primary_key"])) > 1 {
			return []string{"primary_key/composite"}
		}
		return []string{"primary_key/single"}
	case "foreign_keys":
		return []string{"foreign_keys/joinable!"}
	}
	return nil
}

func tableForms(table contract.Node) []string {
	a := table.Attributes
	out := []string{"name/table"}
	model := table.QualifiedName
	last := model[strings.LastIndex(model, ".")+1:]
	name := fmt.Sprint(a["name"])
	if last != name {
		out = append(out, "name/table #[sql_name]")
	}
	if rustKeywords[name] {
		out = append(out, "name/table keyword")
	}
	if strings.Contains(model, ".") {
		out = append(out, "name/table in pub mod")
	}
	if s, ok := a["schema"].(string); ok && s != "" {
		out = append(out, "name/table schema.name")
	}
	if a["column_set"] == "complete" {
		out = append(out, "column_set/complete", "kind/base table")
	}
	return out
}

// compareGroup compares one model group with the run's catalog.
func compareGroup(t *testing.T, run dlOracleRun, nodes []contract.Node, set string, d *dlTally, compared map[string]bool) {
	t.Helper()
	defaultSchema := run.DefaultSchema
	if defaultSchema == "" {
		defaultSchema = "public"
	}
	rels := map[dlKey]string{}
	for _, r := range run.Catalog.Relations {
		rels[dlKey{r.Schema, r.Name}] = r.Relkind
	}
	cols := map[dlKey][]dlCatColumn{}
	for _, c := range run.Catalog.Columns {
		k := dlKey{c.Schema, c.Table}
		cols[k] = append(cols[k], c)
	}
	pks := map[dlKey][]string{}
	type fk struct{ schema, table, column, refSchema, refTable, refColumn string }
	fks := map[fk]bool{}
	for _, c := range run.Catalog.Constraints {
		switch {
		case c.Type == "p":
			pks[dlKey{c.Schema, c.Table}] = c.Columns
		case c.Type == "f" && len(c.Columns) == 1:
			fks[fk{c.Schema, c.Table, c.Columns[0], c.RefSchema, c.RefTable, c.RefColumns[0]}] = true
		}
	}
	tables := map[string]contract.Node{}
	keys := map[string]dlKey{}
	var ids []string
	for _, n := range nodes {
		if n.Kind == "table" && n.Attributes["set"] == "models/diesel/"+set {
			tables[n.Identity] = n
			ids = append(ids, n.Identity)
		}
	}
	if len(ids) == 0 {
		t.Errorf("no table records in set %s", set)
		return
	}
	sort.Strings(ids)
	byTable := map[string][]contract.Node{}
	for _, n := range nodes {
		if n.Kind != "column" || !strings.HasPrefix(n.Identity, "column:"+set+"#") {
			continue
		}
		tid := "table:" + strings.TrimPrefix(n.Identity[:strings.LastIndex(n.Identity, ".")], "column:")
		byTable[tid] = append(byTable[tid], n)
	}
	mark := func(forms ...string) {
		for _, f := range forms {
			compared[f] = true
		}
	}
	for _, id := range ids {
		tn := tables[id]
		a := tn.Attributes
		// A table on hand-written lines is mapped (Revision 2, A6): it
		// then claims names only.
		if v := a["verification"]; v != models.VerificationOracle && (v != models.VerificationMapped || a["column_set"] == "complete" || a["primary_key"] != nil) {
			t.Errorf("%s: verification %v on postgres", id, v)
		}
		schema := defaultSchema
		if s, ok := a["schema"].(string); ok && s != "" {
			schema = s
		}
		key := dlKey{schema, fmt.Sprint(a["name"])}
		keys[id] = key
		kind, exists := rels[key]
		d.check("table exists", exists, "%s: %s.%s is not in the catalog", id, key.schema, key.name)
		if !exists {
			continue
		}
		mark(tableForms(tn)...)
		catCols := cols[key]
		known := a["column_set"] == "complete"
		if known {
			d.check("base table", kind == "r" || kind == "p", "%s: relkind %s", id, kind)
			var mine, theirs []string
			for _, c := range byTable[id] {
				mine = append(mine, fmt.Sprint(c.Attributes["name"]))
			}
			for _, c := range catCols {
				theirs = append(theirs, c.Name)
			}
			sort.Strings(mine)
			sort.Strings(theirs)
			d.check("column set complete", slices.Equal(mine, theirs), "%s: columns %v, catalog %v", id, mine, theirs)
		} else {
			d.unknown["column set"]++
			if kind != "r" && kind != "p" {
				d.drift = append(d.drift, fmt.Sprintf("%s: relkind %s (kind not claimed)", id, kind))
			}
			if len(pks[key]) == 0 {
				d.drift = append(d.drift, fmt.Sprintf("%s: the catalog has no primary key (not claimed)", id))
			}
		}
		if a["primary_key"] != nil {
			pk := stringList(a["primary_key"])
			d.check("primary key", slices.Equal(pk, pks[key]), "%s: primary key %v, catalog %v", id, pk, pks[key])
		} else {
			d.unknown["primary key"]++
		}
		byName := map[string]dlCatColumn{}
		for _, c := range catCols {
			byName[c.Name] = c
		}
		for _, cn := range byTable[id] {
			ca := cn.Attributes
			name := fmt.Sprint(ca["name"])
			cc, ok := byName[name]
			claims := map[string]bool{}
			for _, c := range columnClaims(cn) {
				claims[fmt.Sprint(c["fact"])] = true
			}
			d.check("column exists", ok, "%s: column %s is not in %s.%s", cn.Identity, name, key.schema, key.name)
			if !ok {
				continue
			}
			mark(columnForms(tn, cn, "name")...)
			if claims["nullable"] {
				d.check("nullability", ca["nullable"] == !cc.NotNull, "%s: nullable %v, catalog not null %v", cn.Identity, ca["nullable"], cc.NotNull)
				mark(columnForms(tn, cn, "nullable")...)
			} else {
				d.unknown["nullability"]++
				if dt, ok := ca["tool_type"].(string); ok && strings.HasPrefix(dt, "Nullable<") == cc.NotNull {
					d.drift = append(d.drift, fmt.Sprintf("%s: written %s, catalog not null %v (nullability not claimed)", cn.Identity, dt, cc.NotNull))
				}
			}
			if claims["type"] {
				if cc.Typtype == "d" {
					// DI-V3's named failure case: print-schema writes a
					// domain's base type.
					d.drift = append(d.drift, fmt.Sprintf("%s: type %v, catalog domain %s (DI-V3)", cn.Identity, ca["type"], cc.Type))
				} else {
					d.check("type", ca["type"] == cc.Type, "%s: type %v, catalog %s", cn.Identity, ca["type"], cc.Type)
					mark(columnForms(tn, cn, "type")...)
				}
			} else if claims["type_family"] {
				// A family claim agrees when the catalog type's base name
				// (format_type with no modifier) is the family and the
				// modifiers it lists unknown are not asserted.
				name, unknown := familyParts(ca["type_family"])
				asserted := []string{}
				for _, m := range unknown {
					if ca[m] != nil {
						asserted = append(asserted, m)
					}
				}
				switch {
				case cc.Typtype == "d":
					d.drift = append(d.drift, fmt.Sprintf("%s: type family %v, catalog domain %s (DI-V3)", cn.Identity, name, cc.Type))
				default:
					d.check("type family", name == cc.Base && len(unknown) > 0 && len(asserted) == 0, "%s: type family %s (unknown %v, asserted %v), catalog %s (base %s)", cn.Identity, name, unknown, asserted, cc.Type, cc.Base)
					mark(columnForms(tn, cn, "type_family")...)
				}
			} else {
				d.unknown["type"]++
			}
			if claims["primary_key"] {
				d.check("primary key member", ca["primary_key"] == slices.Contains(pks[key], name), "%s: primary key %v, catalog %v", cn.Identity, ca["primary_key"], pks[key])
				mark(columnForms(tn, cn, "primary_key")...)
			}
			if claims["foreign_keys"] {
				list, _ := ca["foreign_keys"].([]any)
				for _, x := range list {
					f, _ := x.(map[string]any)
					if f["status"] != "resolved" {
						continue
					}
					target := keyOf(nodes, cn, f, defaultSchema)
					refCols := stringList(f["columns"])
					ok := len(refCols) == 1 && fks[fk{key.schema, key.name, name, target.schema, target.name, refCols[0]}]
					d.check("foreign key", ok, "%s: foreign key to %s.%s(%v) is not in the catalog", cn.Identity, target.schema, target.name, refCols)
					mark(columnForms(tn, cn, "foreign_keys")...)
				}
			}
		}
	}
}

// familyParts reads a type_family attribute as JSON reads it back.
func familyParts(v any) (string, []string) {
	m, _ := v.(map[string]any)
	return fmt.Sprint(m["name"]), stringList(m["unknown"])
}

// keyOf is the catalog key of a foreign key's target table.
func keyOf(nodes []contract.Node, col contract.Node, f map[string]any, defaultSchema string) dlKey {
	schema := defaultSchema
	if s, ok := f["schema"].(string); ok && s != "" {
		schema = s
	}
	return dlKey{schema, fmt.Sprint(f["table"])}
}

// pgDieselTypes are the type idents diesel provides on postgres, for which
// print-schema writes no sql_types struct (diesel_cli 2.3.14
// print_schema.rs:62-141).
var pgDieselTypes = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`Bool Integer SmallInt BigInt Binary Text Double Float Numeric Timestamp Date Time
Float4 Smallint Int2 Int4 Int8 Bigint Float8 Decimal VarChar Varchar Char Tinytext Mediumtext Longtext Tinyblob
Blob Mediumblob Longblob Varbinary Bit Cidr Citext Inet Jsonb MacAddr MacAddr8 Money Oid Range Timestamptz Uuid
Json PgLsn Record Interval Int4range Int8range Daterange Numrange Tsrange Tstzrange Int4multirange Int8multirange
Datemultirange Nummultirange Tsmultirange Tstzmultirange SmallSerial BigSerial Serial Bytea Bpchar Macaddr Macaddr8`) {
		pgDieselTypes[k] = true
	}
}

var unmappable = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// rustNameFor is diesel_cli 2.3.14's rust_name_for_sql_name
// (inference.rs:24-71): unmappable characters to one `_`, a leading digit
// prefixed with `_`, a reserved name or the table's own name suffixed with
// `_`.
func rustNameFor(sql, table string) string {
	s := unmappable.ReplaceAllString(sql, "_")
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	reserved := rustKeywords[s] || s == "table" || s == "columns" || s == "is_nullable" || strings.HasPrefix(s, "__") && !strings.HasSuffix(s, "_")
	if reserved || table != "" && s == table {
		s += "_"
	}
	return s
}

// emulatePrintSchema writes the schema file diesel_cli 2.3.14's
// print-schema writes for one schema of the probe database (postgres; no
// docs, no patch, columns in ordinal order): the header, the sql_types
// module, one table! per base table that has a primary key (print-schema
// refuses a table without one, inference.rs:193-195, so the section
// filters those out), the joinable! lines foreign_keys.rs:8-71 keeps and
// one allow_tables_to_appear_in_same_query!. With schema set the output is
// wrapped in `pub mod <schema>` and table names are schema-qualified
// (print_schema.rs:283-285, :560-582; table_data.rs:52-79). It is an
// emulation of the cited rules, not diesel_cli's own output: a test of the
// mapping table only.
func emulatePrintSchema(run dlOracleRun, schema string) (string, []string) {
	def := run.DefaultSchema
	qualified := schema != def
	pk := map[string][]string{}
	for _, p := range run.Info.PrimaryKeys {
		if p.Schema == schema {
			pk[p.Table] = append(pk[p.Table], p.Column)
		}
	}
	var tables, skipped []string
	for _, tb := range run.Info.Tables {
		if tb.Schema != schema || tb.Type != "BASE TABLE" || strings.HasPrefix(tb.Name, "__") {
			continue
		}
		if len(pk[tb.Name]) == 0 {
			skipped = append(skipped, tb.Name)
			continue
		}
		tables = append(tables, tb.Name)
	}
	sort.Strings(tables)
	rust := map[string]string{}
	for _, tb := range tables {
		rust[tb] = rustNameFor(tb, "")
	}
	type custom struct{ name, schema string }
	customs := map[string]custom{}
	var body strings.Builder
	for _, tb := range tables {
		var lines []string
		var imports []string
		for _, c := range run.Info.Columns {
			if c.Schema != schema || c.Table != tb {
				continue
			}
			udt, array := c.UDT, strings.HasPrefix(c.UDT, "_")
			if array {
				udt = udt[1:]
			}
			ident := upperCamel(udt)
			if !pgDieselTypes[ident] {
				s := c.UDTSchema
				if s == def {
					s = ""
				}
				customs[ident] = custom{udt, s}
				if !slices.Contains(imports, ident) {
					imports = append(imports, ident)
				}
			}
			ty := ident
			if array {
				ty = "Array<Nullable<" + ty + ">>"
			}
			if c.Nullable == "YES" {
				ty = "Nullable<" + ty + ">"
			}
			name := rustNameFor(c.Name, rust[tb])
			if name != c.Name {
				lines = append(lines, fmt.Sprintf("        #[sql_name = %q]", c.Name))
			}
			if c.MaxLength != nil {
				lines = append(lines, fmt.Sprintf("        #[max_length = %d]", *c.MaxLength))
			}
			lines = append(lines, fmt.Sprintf("        %s -> %s,", name, ty))
		}
		body.WriteString("diesel::table! {\n")
		if len(imports) > 0 {
			body.WriteString("    use diesel::sql_types::*;\n")
			for _, i := range imports {
				fmt.Fprintf(&body, "    use super::sql_types::%s;\n", i)
			}
			body.WriteString("\n")
		}
		full := tb
		prefix := ""
		if qualified {
			full = schema + "." + tb
			prefix = rustNameFor(schema, "") + "."
		}
		if rust[tb] != tb {
			fmt.Fprintf(&body, "    #[sql_name = %q]\n", full)
		}
		var keys []string
		for _, k := range pk[tb] {
			keys = append(keys, rustNameFor(k, rust[tb]))
		}
		fmt.Fprintf(&body, "    %s%s (%s) {\n%s\n    }\n}\n\n", prefix, rust[tb], strings.Join(keys, ", "), strings.Join(lines, "\n"))
	}
	// joinable!: foreign keys of the schema, minus self references, keys
	// to or from tables outside the output, composite keys, keys to
	// anything but the parent's single primary-key column, and every key
	// of a table pair that has several (counted over all loaded keys).
	type fk struct{ child, parent, column, ref string }
	var fks []fk
	pairs := map[[2]string]int{}
	for _, c := range run.Catalog.Constraints {
		if c.Type != "f" || c.Schema != schema {
			continue
		}
		p := [2]string{c.Table, c.RefSchema + "." + c.RefTable}
		q := [2]string{c.RefSchema + "." + c.RefTable, c.Table}
		if c.RefSchema == schema {
			p, q = [2]string{c.Table, c.RefTable}, [2]string{c.RefTable, c.Table}
		}
		if p[0] > p[1] {
			p = q
		}
		pairs[p]++
		if c.RefSchema != schema || len(c.Columns) != 1 {
			continue
		}
		fks = append(fks, fk{c.Table, c.RefTable, c.Columns[0], c.RefColumns[0]})
	}
	sort.Slice(fks, func(i, j int) bool {
		return fks[i].child+"\x00"+fks[i].parent+"\x00"+fks[i].column < fks[j].child+"\x00"+fks[j].parent+"\x00"+fks[j].column
	})
	for _, k := range fks {
		p := [2]string{k.child, k.parent}
		if p[0] > p[1] {
			p = [2]string{k.parent, k.child}
		}
		_, childOut := rust[k.child]
		_, parentOut := rust[k.parent]
		if k.child == k.parent || !childOut || !parentOut || len(pk[k.parent]) != 1 || pk[k.parent][0] != k.ref || pairs[p] > 1 {
			continue
		}
		fmt.Fprintf(&body, "diesel::joinable!(%s -> %s (%s));\n", rust[k.child], rust[k.parent], rustNameFor(k.column, rust[k.child]))
	}
	if len(tables) > 1 {
		var names []string
		for _, tb := range tables {
			names = append(names, rust[tb])
		}
		fmt.Fprintf(&body, "\ndiesel::allow_tables_to_appear_in_same_query!(%s);\n", strings.Join(names, ", "))
	}
	var mod strings.Builder
	if len(customs) > 0 {
		var idents []string
		for i := range customs {
			idents = append(idents, i)
		}
		sort.Strings(idents)
		mod.WriteString("pub mod sql_types {\n")
		for _, i := range idents {
			c := customs[i]
			attr := fmt.Sprintf("name = %q", c.name)
			if c.schema != "" {
				attr += fmt.Sprintf(", schema = %q", c.schema)
			}
			fmt.Fprintf(&mod, "    #[derive(diesel::sql_types::SqlType)]\n    #[diesel(postgres_type(%s))]\n    pub struct %s;\n\n", attr, i)
		}
		mod.WriteString("}\n\n")
	}
	text := mod.String() + body.String()
	if qualified {
		text = "pub mod " + rustNameFor(schema, "") + " {\n" + text + "}\n"
	}
	return "// @generated automatically by Diesel CLI.\n\n" + text, skipped
}

// emulatedPackage is the probe run's package: the fixture's postgres
// Cargo.toml and Cargo.lock (diesel 2.3.14, postgres), a diesel.toml with
// one section for public and one with schema = "other" (each filtering
// out the tables without a primary key, which print-schema refuses), and
// the two emulated schema files.
func emulatedPackage(t *testing.T, root string, run dlOracleRun) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, f := range []string{"Cargo.toml", "Cargo.lock"} {
		data, err := os.ReadFile(filepath.Join(root, "pg", f))
		if err != nil {
			t.Fatal(err)
		}
		out[f] = data
	}
	var toml strings.Builder
	for _, s := range []struct{ section, schema, file string }{{"print_schema", "", "src/schema.rs"}, {"print_schema.other", "other", "src/other.rs"}} {
		name := s.schema
		if name == "" {
			name = run.DefaultSchema
		}
		text, skipped := emulatePrintSchema(run, name)
		out[s.file] = []byte(text)
		fmt.Fprintf(&toml, "[%s]\nfile = %q\n", s.section, s.file)
		if s.schema != "" {
			fmt.Fprintf(&toml, "schema = %q\n", s.schema)
		}
		if len(skipped) > 0 {
			q := make([]string, len(skipped))
			for i, n := range skipped {
				q[i] = fmt.Sprintf("%q", "^"+regexp.QuoteMeta(n)+"$")
			}
			fmt.Fprintf(&toml, "filter = { except_tables = [%s] }\n", strings.Join(q, ", "))
		}
		toml.WriteString("\n")
		if dir := os.Getenv("GRAPHVEIN_DIESEL_ORACLE_EMULATED"); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, filepath.Base(s.file)), out[s.file], 0o644)
		}
	}
	out["diesel.toml"] = []byte(toml.String())
	return out
}
