package schema

// Oracle harness (T018a2 investigation step A2): differential testing of the schema
// pass against a real PostgreSQL 17. This file holds the parts that need no
// database: the history model, the extraction through Link, the harness's
// own reading of the records and the comparison with a recorded verdict.
// oracle_pg_test.go runs histories on Postgres; oracle_gen_test.go
// generates and minimises histories.
//
// The harness is written from sqlx 0.8.6 and Postgres REL_17_6, not from
// the extractor's code: sqlx order (Q1-Q3), the type alias table and the
// catalog reading are its own. It only calls the pass's entry point (Link)
// and reads the emitted records, as index.Emit would store them.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
)

// oracleHistory is one migration history: a sqlx set (Cargo.toml plus the
// direct entries of migrations/) and the prelude that sets up the objects
// environment assumption E1 lets the set use without creating them.
type oracleHistory struct {
	ID    string       `json:"id"`
	Cargo string       `json:"cargo,omitempty"`
	Files []oracleFile `json:"-"`
	// Prelude runs before the first migration, outside any migration (E1:
	// roles, extensions, schemas and tables the set never creates).
	Prelude []string `json:"prelude,omitempty"`
	// FreshDB runs the history in a fresh database from template0 instead
	// of a fresh schema (histories that use qualified names, schemas,
	// roles or extensions).
	FreshDB bool `json:"fresh_db,omitempty"`
	// Workspace, when set, is a workspace root manifest: the package is
	// written to pkg/ below it (fixture sets that inherit sqlx).
	Workspace string `json:"workspace,omitempty"`
	// Unreadable are members written to disk but not given to the pass as
	// sources (the scanner skips files that are not UTF-8).
	Unreadable []rawFile `json:"unreadable,omitempty"`
	// Tags are the generator's contract-row tags per file, statement by
	// statement (generated histories only).
	Tags map[string][]string `json:"tags,omitempty"`
}

type oracleFile struct {
	Name string
	SQL  string
}

// rawFile is a member that is not valid UTF-8 (JSON keeps its bytes).
type rawFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// defaultCargo is the manifest of generated and rehosted histories: a
// Postgres-only sqlx 0.8.6 dependency, so the pass treats the set as
// Postgres and sqlx reads `-- no-transaction` (A1: since 0.8.0).
const defaultCargo = "[package]\nname = \"oracle-history\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nsqlx = { version = \"0.8.6\", features = [\"runtime-tokio\", \"postgres\"] }\n"

func (h *oracleHistory) cargo() string {
	if h.Cargo != "" {
		return h.Cargo
	}
	return defaultCargo
}

// all returns the files and the unreadable members.
func (h *oracleHistory) all() []oracleFile {
	out := slices.Clone(h.Files)
	for _, r := range h.Unreadable {
		out = append(out, oracleFile{Name: r.Name, SQL: string(r.Data)})
	}
	return out
}

// writeTo materialises the history as a sqlx package under dir.
func (h *oracleHistory) writeTo(dir string) error {
	if h.Workspace != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(h.Workspace), 0o644); err != nil {
			return fmt.Errorf("writing the workspace manifest: %w", err)
		}
		dir = filepath.Join(dir, "pkg")
	}
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(h.cargo()), 0o644); err != nil {
		return fmt.Errorf("writing Cargo.toml: %w", err)
	}
	for _, f := range h.all() {
		if err := os.WriteFile(filepath.Join(dir, "migrations", f.Name), []byte(f.SQL), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", f.Name, err)
		}
	}
	return nil
}

// sqlxMigration is one migration in the harness's own sqlx order.
type sqlxMigration struct {
	file    oracleFile
	version int64
	down    bool
	noTx    bool
}

// sqlxOrder applies sqlx 0.8.6 resolve_blocking (sqlx-core/src/migrate/
// source.rs 57-145): an entry is a migration when its name splits at the
// first "_" into two parts and the second ends in ".sql" (97-101); the
// first part must parse as i64 or the whole set is refused (104-108); the
// suffix .up.sql/.down.sql sets the direction (migration_type.rs 19-27);
// `-- no-transaction` must be the very start of the file (127); the sort is
// stable by version (142). ok is false when sqlx refuses the set.
func sqlxOrder(files []oracleFile, readsNoTx bool) (out []sqlxMigration, ok bool) {
	for _, f := range files {
		if !utf8.ValidString(f.SQL) {
			// fs::read_to_string fails (source.rs 118-124).
			if _, rest, found := strings.Cut(f.Name, "_"); found && strings.HasSuffix(rest, ".sql") {
				return nil, false
			}
		}
		version, rest, found := strings.Cut(f.Name, "_")
		if !found || !strings.HasSuffix(rest, ".sql") {
			continue
		}
		v, valid := rustI64(version)
		if !valid {
			return nil, false
		}
		out = append(out, sqlxMigration{file: f, version: v, down: strings.HasSuffix(rest, ".down.sql"),
			noTx: readsNoTx && strings.HasPrefix(f.SQL, "-- no-transaction")})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, true
}

// rustI64 is Rust's i64::from_str: an optional sign, then one or more
// ASCII digits, in range.
func rustI64(s string) (int64, bool) {
	digits := strings.TrimPrefix(strings.TrimPrefix(s, "+"), "-")
	if len(s)-len(digits) > 1 || digits == "" {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

// sqlxReadsNoTx reports whether every sqlx version the manifest's
// requirement allows reads `-- no-transaction` (0.8.0 and later, sqlx
// CHANGELOG 0.8.0 #3181). The harness runs the lowest allowed version, so
// a requirement it cannot read counts as not reading the marker.
func sqlxReadsNoTx(cargo string) bool {
	m := regexp.MustCompile(`(?m)^\s*sqlx\s*=\s*(?:\{[^}\n]*version\s*=\s*"([^"]+)"|"([^"]+)")`).FindStringSubmatch(cargo)
	if m == nil {
		return false
	}
	req := m[1] + m[2]
	req = strings.TrimLeft(req, "^=~ ")
	parts := strings.Split(req, ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 0 || minor >= 8
}

// Extraction ------------------------------------------------------------

// xMigration is a migration record as the harness reads it.
type xMigration struct {
	file    string
	applied *bool
}

type xColumn struct {
	name      string
	typ       string
	nullable  *bool
	hasDef    bool // default text or default_implied
	generated bool
	identity  bool
	refs      []xRef
	// nullDefault is a written DEFAULT NULL (possibly cast).
	nullDefault bool
	// previous are the column's earlier names (RENAME COLUMN).
	previous []string
}

type xRef struct {
	linked     bool
	table      string // qualified name as emitted
	columns    []string
	local      []string
	constraint *string
}

type xIndex struct {
	name     *string
	unique   bool
	elements []string
	where    bool
}

type xTable struct {
	schema   *string
	name     string
	complete bool
	opaque   int
	// derive are the migration files the table's facts come from: the
	// creating migration and every migration with an alters edge.
	derive  []string
	pk      []string
	pkKnown bool
	uniques []xKey
	uqKnown bool
	columns []xColumn
	indexes []xIndex
	// restored is set on a table a possibly failing migration dropped
	// ("may still exist"): its existence is not claimed.
	restored bool
	// typeChanged is set when a partial reason says that a type a column
	// uses was renamed or changed (R4): the written type text (M2) is then
	// not compared with format_type (H6).
	typeChanged bool
	// placementUnknown is set when a partial reason says that the schema
	// of the table's name is unknown (search_path, U3): H13 compares a
	// schema: null record then only by name.
	placementUnknown bool
}

type xKey struct {
	name    *string
	columns []string
}

type extraction struct {
	migrations map[string]*xMigration // by base file name
	tables     []*xTable
	// diagnostics are the pass's, for the coverage-loss ranking (A6).
	diagnostics []contract.Diagnostic
}

// extract runs the pass on the history as index.Emit would: the sources
// are the files under a temporary package root, LF-normalised.
func extract(h *oracleHistory, root string) (*extraction, error) {
	if err := h.writeTo(root); err != nil {
		return nil, err
	}
	pkg := ""
	sources := map[string][]byte{}
	if h.Workspace != "" {
		pkg = "pkg/"
		sources["Cargo.toml"] = []byte(h.Workspace)
	}
	sources[pkg+"Cargo.toml"] = []byte(h.cargo())
	for _, f := range h.Files {
		sources[pkg+"migrations/"+f.Name] = []byte(strings.ReplaceAll(f.SQL, "\r\n", "\n"))
	}
	nodes, edges, diagnostics := Link(Input{Sources: sources, Root: root})
	x := readRecords(nodes, edges)
	x.diagnostics = diagnostics
	return x, nil
}

func readRecords(nodes []contract.Node, edges []contract.Edge) *extraction {
	out := &extraction{migrations: map[string]*xMigration{}}
	byID := map[string]contract.Node{}
	migrationFile := map[string]string{}
	for _, n := range nodes {
		byID[n.Identity] = n
		if n.Kind == "migration" {
			m := &xMigration{file: path.Base(n.File)}
			if v, ok := n.Attributes["applied"].(bool); ok {
				m.applied = &v
			}
			out.migrations[m.file] = m
			migrationFile[n.Identity] = m.file
		}
	}
	children := map[string][]string{}
	derive := map[string][]string{}
	for _, e := range edges {
		switch e.Kind {
		case "contains":
			children[e.Source] = append(children[e.Source], e.Target)
			if f, ok := migrationFile[e.Source]; ok {
				derive[e.Target] = append(derive[e.Target], f)
			}
		case "alters":
			if f, ok := migrationFile[e.Source]; ok {
				derive[e.Target] = append(derive[e.Target], f)
			}
		}
	}
	for _, n := range nodes {
		if n.Kind != "table" {
			continue
		}
		a := n.Attributes
		t := &xTable{name: a["name"].(string), complete: a["state"] == "complete", derive: derive[n.Identity]}
		if s, ok := a["schema"].(string); ok {
			t.schema = &s
		}
		t.opaque, _ = a["opaque_after"].(int)
		if pk, ok := a["primary_key"].([]string); ok {
			t.pk, t.pkKnown = pk, true
		}
		if uq, ok := a["unique_keys"].([]any); ok {
			t.uqKnown = true
			for _, u := range uq {
				m := u.(map[string]any)
				k := xKey{columns: m["columns"].([]string)}
				if s, ok := m["constraint"].(string); ok {
					k.name = &s
				}
				t.uniques = append(t.uniques, k)
			}
		}
		if reasons, ok := a["reasons"].([]any); ok {
			for _, r := range reasons {
				text, _ := r.(map[string]any)["reason"].(string)
				if strings.HasPrefix(text, "dropped by ") {
					t.restored = true
				}
				if strings.Contains(text, "changes a type this column uses") {
					t.typeChanged = true
				}
				if strings.Contains(text, "the schema of this name is unknown") {
					t.placementUnknown = true
				}
			}
		}
		var cols []contract.Node
		for _, id := range children[n.Identity] {
			c := byID[id]
			switch c.Kind {
			case "column":
				cols = append(cols, c)
			case "db_index":
				ix := xIndex{unique: c.Attributes["unique"] == true, elements: c.Attributes["elements"].([]string), where: c.Attributes["where"] != nil}
				if s, ok := c.Attributes["name"].(string); ok {
					ix.name = &s
				}
				t.indexes = append(t.indexes, ix)
			}
		}
		sort.Slice(cols, func(i, j int) bool {
			return cols[i].Attributes["position"].(int) < cols[j].Attributes["position"].(int)
		})
		for _, c := range cols {
			ca := c.Attributes
			xc := xColumn{name: ca["name"].(string), typ: ca["type"].(string), generated: ca["generated"] != nil, identity: ca["identity"] != nil}
			xc.previous, _ = ca["previous_names"].([]string)
			if v, ok := ca["nullable"].(bool); ok {
				xc.nullable = &v
			}
			// A NULL constant default is not stored (heap.c
			// AddRelationNewConstraints: "the default behavior is
			// equivalent"), except on a domain, which the harness does
			// not tell apart: such a default is not compared.
			def, _ := ca["default"].(string)
			xc.hasDef = ca["default"] != nil && !nullDefault.MatchString(def) || ca["default_implied"] != nil
			xc.nullDefault = ca["default"] != nil && nullDefault.MatchString(def)
			var refs []any
			if r, ok := ca["references"].(map[string]any); ok {
				refs = append(refs, r)
			}
			if more, ok := ca["additional_references"].([]any); ok {
				refs = append(refs, more...)
			}
			for _, r := range refs {
				m := r.(map[string]any)
				xr := xRef{linked: m["status"] == "linked", table: fmt.Sprint(m["table"]), local: m["local_columns"].([]string)}
				if cs, ok := m["columns"].([]string); ok {
					xr.columns = cs
				}
				if s, ok := m["constraint"].(string); ok {
					xr.constraint = &s
				}
				xc.refs = append(xc.refs, xr)
			}
			t.columns = append(t.columns, xc)
		}
		out.tables = append(out.tables, t)
	}
	sort.Slice(out.tables, func(i, j int) bool { return out.tables[i].key("") < out.tables[j].key("") })
	return out
}

// key is the table's catalog key "schema.name"; an unqualified table lives
// in the history's default schema (E4).
func (t *xTable) key(defaultSchema string) string {
	if t.schema != nil {
		return *t.schema + "." + t.name
	}
	return defaultSchema + "." + t.name
}

// Verdict ---------------------------------------------------------------

// outcome is the oracle's result for one migration in sqlx order.
type outcome struct {
	File string `json:"file"`
	// Status is accepted, refused, not_run (after a refusal), down (never
	// applied) or set_refused (sqlx refuses the whole set).
	Status   string `json:"status"`
	SQLState string `json:"sqlstate,omitempty"`
	Message  string `json:"message,omitempty"`
	Where    string `json:"where,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// Statement is the 1-based statement the refusal came from (0 when not
	// attributed); attribution only, the verdict is the whole migration's.
	Statement int    `json:"statement,omitempty"`
	StmtText  string `json:"statement_text,omitempty"`
	// Assumption is E1-E4 when the refusal breaks an environment
	// assumption (an assumption hit, not a disagreement).
	Assumption string `json:"assumption,omitempty"`
	NoTx       bool   `json:"no_transaction,omitempty"`
	// RowFree is set when the replay refuses the attributed class 22 or
	// 23 statement with the same SQLSTATE again after every table it can
	// reach was emptied: the refusal does not depend on rows, so it is
	// never E2 (B2, harness rule H7).
	RowFree bool `json:"row_free,omitempty"`
	// PostCommit is the SQLSTATE and message of sqlx's post-commit UPDATE
	// of execution_time when it failed (H24, Revision 1.9): the migration
	// is applied and the later ones are not run.
	PostCommit string `json:"post_commit,omitempty"`
}

type catColumn struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	NotNull   bool   `json:"not_null"`
	HasDef    bool   `json:"has_default"`
	Identity  bool   `json:"identity,omitempty"`
	Generated bool   `json:"generated,omitempty"`
}

type catKey struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

type catFK struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"` // schema.name
	RefColumns []string `json:"ref_columns"`
}

type catIndex struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"` // "" for an expression element
	Partial bool     `json:"partial,omitempty"`
}

type catTable struct {
	Key     string      `json:"table"` // schema.name
	Columns []catColumn `json:"columns"`
	PK      []string    `json:"primary_key"`
	Uniques []catKey    `json:"unique_keys"`
	FKs     []catFK     `json:"foreign_keys"`
	Indexes []catIndex  `json:"indexes"`
}

type catEnum struct {
	Key    string   `json:"type"`
	Labels []string `json:"labels"`
}

// verdict is what the oracle recorded for a history.
type verdict struct {
	Schema   string     `json:"default_schema"`
	Outcomes []outcome  `json:"outcomes"`
	Tables   []catTable `json:"tables"`
	Enums    []catEnum  `json:"enums,omitempty"`
	// Prelude are the catalog tables the prelude created (E1); they are
	// outside the set and not compared.
	Prelude []string `json:"prelude_tables,omitempty"`
	// RowSecurity is read only by the Supabase oracle (catalogHook), by
	// schema-qualified table name.
	RowSecurity map[string]catRowSecurity `json:"row_security,omitempty"`
}

// Comparison ------------------------------------------------------------

// disagreement is a known fact (or applied value) that differs from the
// catalog. Kind and Object identify it for the minimiser.
type disagreement struct {
	Kind   string `json:"kind"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	// Class is K1, K2 or S1 (set by classify).
	Class string `json:"class,omitempty"`
	// Statement is the refused statement for applied disagreements.
	Statement string `json:"statement,omitempty"`
	Tag       string `json:"tag,omitempty"`
}

func (d disagreement) signature() string { return d.Kind + " " + d.Object }

// counts are the coverage metrics of one history (A6).
type counts struct {
	Tables          int `json:"tables"`
	TablesComplete  int `json:"tables_complete"`
	Migrations      int `json:"migrations"`
	Applied         int `json:"migrations_applied"`
	AppliedNull     int `json:"migrations_applied_null"`
	CatalogFacts    int `json:"catalog_facts"`
	KnownFacts      int `json:"known_facts"`
	Conditional     int `json:"conditional_facts"`
	NullFacts       int `json:"null_facts"`
	ComparedFacts   int `json:"compared_facts"`
	OpaqueTables    int `json:"opaque_tables"`
	CutAt           int `json:"cut_at,omitempty"`
	AssumptionHits  int `json:"assumption_hits"`
	Disagreements   int `json:"disagreements"`
	UncomparedTypes int `json:"uncompared_types,omitempty"`
}

// compare checks every fact the extraction marks as known against the
// verdict, and counts the conditional and null ones (A2).
func compare(x *extraction, v *verdict) ([]disagreement, counts) {
	var out []disagreement
	var c counts
	status := map[string]outcome{}
	cut := -1
	for i, o := range v.Outcomes {
		status[o.File] = o
		if o.Status == "refused" && o.Assumption != "" && cut < 0 {
			cut = i
			c.CutAt = i + 1
			c.AssumptionHits++
		}
	}
	beyondCut := map[string]bool{}
	if cut >= 0 {
		for _, o := range v.Outcomes[cut:] {
			beyondCut[o.File] = true
		}
	}
	// applied per migration.
	for _, o := range v.Outcomes {
		m := x.migrations[o.File]
		if m == nil || o.Status == "down" {
			continue
		}
		c.Migrations++
		if m.applied == nil {
			c.AppliedNull++
			continue
		}
		if *m.applied {
			c.Applied++
		}
		if beyondCut[o.File] {
			continue
		}
		if *m.applied && o.Status != "accepted" {
			out = append(out, disagreement{Kind: "applied", Object: o.File, Detail: fmt.Sprintf("applied: true, Postgres %s %s: %s", o.Status, o.SQLState, o.Message), Statement: o.StmtText})
		}
		if !*m.applied && o.Status == "accepted" {
			out = append(out, disagreement{Kind: "applied", Object: o.File, Detail: "applied: false, Postgres accepted it"})
		}
	}
	catalog := map[string]*catTable{}
	for i := range v.Tables {
		catalog[v.Tables[i].Key] = &v.Tables[i]
	}
	prelude := map[string]bool{}
	for _, p := range v.Prelude {
		prelude[p] = true
	}
	modeOf := func(t *xTable) string {
		mode := "known"
		for _, f := range t.derive {
			if beyondCut[f] {
				return "cut"
			}
			m := x.migrations[f]
			if m == nil || m.applied == nil {
				// Conditional: after a possible failure the pass keeps what a
				// change may not have removed (WF4), so these facts are
				// hedges, not claims; they are counted, not compared.
				mode = "conditional"
			} else if !*m.applied {
				return "skip"
			}
		}
		return mode
	}
	// H4: a foreign key names its referenced table by that table's current
	// name, so the reference is a known fact only when the referenced
	// table's facts are known too (a rename in a possibly failing or cut
	// migration changes the name the record shows).
	var unsettled []string
	for _, t := range x.tables {
		if modeOf(t) != "known" {
			unsettled = append(unsettled, t.key(v.Schema))
		}
	}
	refKnown := func(name, self string) bool {
		return !slices.ContainsFunc(unsettled, func(k string) bool { return sameTableName(k, name, self) })
	}
	for _, t := range x.tables {
		c.Tables++
		if t.complete && t.opaque == 0 {
			c.TablesComplete++
		}
		mode := modeOf(t)
		if t.opaque > 0 {
			c.OpaqueTables++
		}
		key := t.key(v.Schema)
		ct := catalog[key]
		if mode == "cut" || mode == "skip" {
			continue
		}
		if t.schema == nil && t.placementUnknown {
			// H13 (Revision 1.5): a record whose schema the pass reports
			// unknown (search_path) is not compared as the default
			// schema's: only with the one relation of its name, a
			// disagreement when no schema holds the name, and not
			// compared when several do.
			var same []string
			for k := range catalog {
				if _, name, _ := strings.Cut(k, "."); name == t.name {
					same = append(same, k)
				}
			}
			switch len(same) {
			case 0:
				key = "?." + t.name
			case 1:
				key = same[0]
				ct = catalog[key]
			default:
				continue
			}
		}
		if mode == "conditional" {
			countConditional(t, &c)
			continue
		}
		f := factCounter{c: &c}
		// Facts on a table an opaque statement ran after are stated as the
		// modelled statements leave them; opaque_after says any may have
		// changed (ADR 0047), so they are not claims.
		if t.opaque > 0 {
			f.opaque = true
		}
		f.refKnown = refKnown
		out = append(out, compareTable(t, ct, key, &f)...)
	}
	// Catalog facts of the comparable kinds, for the facts-known share.
	for _, t := range v.Tables {
		if prelude[t.Key] {
			continue
		}
		c.CatalogFacts += 1 + 4*len(t.Columns) + 1 + len(t.Uniques) + len(t.FKs) + len(t.Indexes)
	}
	c.Disagreements = len(out)
	return out, c
}

// countConditional counts the facts of a table that derives from an
// applied: null migration.
func countConditional(t *xTable, c *counts) {
	c.Conditional += 1 + 4*len(t.columns) + 1 + len(t.indexes)
}

type factCounter struct {
	c      *counts
	opaque bool
	// refKnown reports whether a referenced table's name is a known fact
	// (H4); nil means every name is.
	refKnown func(name, self string) bool
}

// claim counts one fact the extraction states; it returns whether the
// fact is compared.
func (f *factCounter) claim() bool {
	if f.opaque {
		f.c.NullFacts++
		return false
	}
	f.c.ComparedFacts++
	return true
}

func (f *factCounter) unknown() { f.c.NullFacts++ }

func compareTable(t *xTable, ct *catTable, key string, f *factCounter) []disagreement {
	var out []disagreement
	add := func(kind, object, format string, args ...any) {
		out = append(out, disagreement{Kind: kind, Object: object, Detail: fmt.Sprintf(format, args...)})
	}
	if t.restored {
		return nil
	}
	if !f.claim() {
		return nil
	}
	if ct == nil {
		add("table", key, "table %s is in the records but not in the catalog", key)
		return out
	}
	full := t.complete && t.opaque == 0
	// Columns: on a complete table the exact list in order; otherwise the
	// known columns in their relative order.
	catCols := map[string]int{}
	for i, c := range ct.Columns {
		catCols[c.Name] = i
	}
	if f.claim() {
		if full {
			var names, want []string
			for _, c := range t.columns {
				names = append(names, c.name)
			}
			for _, c := range ct.Columns {
				want = append(want, c.Name)
			}
			if !slices.Equal(names, want) {
				add("columns", key, "records %v, catalog %v", names, want)
			}
		} else {
			last := -1
			for _, c := range t.columns {
				i, ok := catCols[c.name]
				if !ok {
					add("columns", key+"."+c.name, "column %s is not in the catalog", c.name)
					continue
				}
				if i < last {
					add("columns", key+"."+c.name, "column %s is out of order", c.name)
				}
				last = i
			}
		}
	}
	for _, c := range t.columns {
		i, ok := catCols[c.name]
		if !ok {
			continue
		}
		cc := ct.Columns[i]
		obj := key + "." + c.name
		if want, comparable := harnessType(c.typ); !comparable || t.typeChanged {
			f.c.UncomparedTypes++
		} else if f.claim() {
			if got := catalogType(cc.Type); want != got {
				add("type", obj, "records %q (%s), catalog %s", c.typ, want, got)
			}
		}
		if c.nullable == nil {
			f.unknown()
		} else if f.claim() && *c.nullable == cc.NotNull {
			add("nullable", obj, "records nullable %v, catalog not null %v", *c.nullable, cc.NotNull)
		}
		switch {
		case c.nullDefault:
			f.unknown()
		case c.generated:
			if f.claim() && !cc.Generated {
				add("default", obj, "records a generated column, catalog has none")
			}
		case c.hasDef:
			if f.claim() && (!cc.HasDef || cc.Generated) {
				add("default", obj, "records a default, catalog has none")
			}
		case full:
			if f.claim() && cc.HasDef && !cc.Generated {
				add("default", obj, "records no default, catalog has one")
			}
		default:
			f.unknown()
		}
	}
	if t.pkKnown {
		if f.claim() && !slices.Equal(t.pk, ct.PK) && !(len(t.pk) == 0 && len(ct.PK) == 0) {
			add("primary_key", key, "records %v, catalog %v", t.pk, ct.PK)
		}
	} else {
		f.unknown()
	}
	if t.uqKnown {
		if f.claim() {
			var got, want []string
			for _, u := range t.uniques {
				got = append(got, strings.Join(u.columns, ","))
			}
			for _, u := range ct.Uniques {
				want = append(want, strings.Join(u.Columns, ","))
			}
			sort.Strings(got)
			sort.Strings(want)
			if full && !slices.Equal(got, want) || !full && !subset(got, want) {
				add("unique_keys", key, "records %v, catalog %v", got, want)
			}
			for _, u := range t.uniques {
				if u.name != nil && !slices.ContainsFunc(ct.Uniques, func(k catKey) bool { return k.Name == *u.name }) {
					add("unique_keys", key+"."+*u.name, "records a UNIQUE constraint named %s, catalog has none", *u.name)
				}
			}
		}
	} else {
		f.unknown()
	}
	// Foreign keys: a linked reference is a constraint with these column
	// pairs to that table; on a complete table every catalog foreign key
	// must be in the records.
	var recordFKs []string
	for _, c := range t.columns {
		for _, r := range c.refs {
			if r.local[0] != c.name {
				continue
			}
			recordFKs = append(recordFKs, strings.Join(r.local, ","))
			if !r.linked || f.refKnown != nil && !f.refKnown(r.table, key) {
				f.unknown()
				continue
			}
			if !f.claim() {
				continue
			}
			found := false
			for _, fk := range ct.FKs {
				if slices.Equal(fk.Columns, r.local) && slices.Equal(fk.RefColumns, r.columns) && sameTableName(fk.RefTable, r.table, key) {
					found = true
				}
			}
			if !found {
				add("foreign_key", key+"."+strings.Join(r.local, ","), "records %v -> %s%v, catalog %v", r.local, r.table, r.columns, ct.FKs)
			}
		}
	}
	if full {
		var want []string
		for _, fk := range ct.FKs {
			want = append(want, strings.Join(fk.Columns, ","))
		}
		sort.Strings(recordFKs)
		sort.Strings(want)
		if f.claim() && !slices.Equal(recordFKs, want) {
			add("foreign_key", key, "records foreign keys on %v, catalog on %v", recordFKs, want)
		}
	}
	// Indexes (not constraint-backed). H5: written names are matched
	// before unnamed indexes, so that an unnamed index never takes the
	// catalog index a written name means. H11 (Revision 1.4, g4-s32590,
	// g4-s44332, g6-s98403 of the 100,000-seed run): unnamed indexes whose
	// columns are known are matched before unnamed expression indexes,
	// whose unknown columns match any catalog index of the same
	// uniqueness and would otherwise take a plain index's entry.
	used := make([]bool, len(ct.Indexes))
	ordered := slices.Clone(t.indexes)
	rank := func(i int) int {
		switch {
		case ordered[i].name != nil:
			return 0
		case renamedColumns(t, indexColumns(ordered[i].elements)) != nil:
			return 1
		}
		return 2
	}
	sort.SliceStable(ordered, func(i, j int) bool { return rank(i) < rank(j) })
	for _, ix := range ordered {
		if !f.claim() {
			continue
		}
		cols := renamedColumns(t, indexColumns(ix.elements))
		match := func(ci catIndex) bool {
			return ci.Unique == ix.unique && (cols == nil || slices.Equal(cols, ci.Columns))
		}
		found := false
		for i, ci := range ct.Indexes {
			if used[i] {
				continue
			}
			if ix.name != nil && ci.Name == *ix.name || ix.name == nil && match(ci) {
				found = true
				used[i] = true
				if ix.name != nil && !match(ci) {
					add("index", key+"."+*ix.name, "records unique %v %v, catalog unique %v %v", ix.unique, cols, ci.Unique, ci.Columns)
				}
				break
			}
		}
		if !found {
			name := "(" + strings.Join(ix.elements, ",") + ")"
			if ix.name != nil {
				name = *ix.name
			}
			add("index", key+"."+name, "records index %s (unique %v), catalog has %v", name, ix.unique, ct.Indexes)
		}
	}
	if full && f.claim() && len(t.indexes) != len(ct.Indexes) {
		add("index", key, "records %d indexes, catalog %d (%v)", len(t.indexes), len(ct.Indexes), ct.Indexes)
	}
	f.c.KnownFacts += coveredFacts(t, ct, full, used)
	return out
}

// coveredFacts counts the catalog facts of ct (the kinds CatalogFacts
// counts) that the records state as known: the table; per column its
// existence, type, nullability and default presence; the primary key;
// UNIQUE keys; foreign keys the records link; indexes the records name or
// match. It never exceeds the catalog's count.
func coveredFacts(t *xTable, ct *catTable, full bool, matched []bool) int {
	n := 1
	cols := map[string]xColumn{}
	for _, c := range t.columns {
		cols[c.name] = c
	}
	for _, cc := range ct.Columns {
		c, ok := cols[cc.Name]
		if !ok {
			continue
		}
		n++
		if _, comparable := harnessType(c.typ); comparable {
			n++
		}
		if c.nullable != nil {
			n++
		}
		if !c.nullDefault && (c.generated || c.hasDef || full) {
			n++
		}
	}
	if t.pkKnown {
		n++
	}
	if t.uqKnown {
		var have []string
		for _, u := range t.uniques {
			have = append(have, strings.Join(u.columns, ","))
		}
		for _, u := range ct.Uniques {
			if i := slices.Index(have, strings.Join(u.Columns, ",")); i >= 0 {
				have = slices.Delete(have, i, i+1)
				n++
			}
		}
	}
	for _, fk := range ct.FKs {
		for _, c := range t.columns {
			if slices.ContainsFunc(c.refs, func(r xRef) bool { return r.linked && slices.Equal(r.local, fk.Columns) }) {
				n++
				break
			}
		}
	}
	for _, m := range matched {
		if m {
			n++
		}
	}
	return n
}

func subset(a, b []string) bool {
	rest := slices.Clone(b)
	for _, x := range a {
		i := slices.Index(rest, x)
		if i < 0 {
			return false
		}
		rest = slices.Delete(rest, i, i+1)
	}
	return true
}

// sameTableName compares a catalog "schema.name" with an emitted qualified
// name (unqualified names are in the referencing table's schema, which is
// the default schema in every history the harness runs).
func sameTableName(cat, emitted, self string) bool {
	emitted = strings.ReplaceAll(emitted, `"`, "")
	if !strings.Contains(emitted, ".") {
		schema, _, _ := strings.Cut(self, ".")
		emitted = schema + "." + emitted
	}
	return cat == emitted
}

// renamedColumns maps the plain-column elements of an index to the
// columns' current names (H2, Revision 1): an index stores attribute
// numbers (pg_index.indkey), so it follows its columns through RENAME
// COLUMN, while the records keep the written element text. An element that
// is a current name and another column's earlier name is ambiguous: the
// columns are then not compared (nil).
func renamedColumns(t *xTable, cols []string) []string {
	if cols == nil {
		return nil
	}
	out := make([]string, len(cols))
	for i, n := range cols {
		current := slices.ContainsFunc(t.columns, func(c xColumn) bool { return c.name == n })
		var renamed []string
		for _, c := range t.columns {
			if slices.Contains(c.previous, n) {
				renamed = append(renamed, c.name)
			}
		}
		switch {
		case len(renamed) == 0:
			out[i] = n
		case !current && len(renamed) == 1:
			out[i] = renamed[0]
		default:
			return nil
		}
	}
	return out
}

// indexColumns returns the column names of index elements that are plain
// (possibly quoted) identifiers, optionally with ASC/DESC/NULLS; nil when an
// element is anything else (expressions, opclasses and collations are not
// compared).
func indexColumns(elements []string) []string {
	var out []string
	plain := regexp.MustCompile(`^("(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_$]*)(\s+(?i:asc|desc))?(\s+(?i:nulls\s+(first|last)))?$`)
	for _, e := range elements {
		m := plain.FindStringSubmatch(strings.TrimSpace(e))
		if m == nil {
			return nil
		}
		name := m[1]
		if strings.HasPrefix(name, `"`) {
			name = strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
		} else {
			name = strings.ToLower(name)
		}
		out = append(out, name)
	}
	return out
}

// Types -------------------------------------------------------------------

var nullDefault = regexp.MustCompile(`(?i)^\s*\(?\s*null\s*\)?\s*(::\s*[a-z_ ]+(\([0-9, ]*\))?)?\s*$`)

var (
	typeArray   = regexp.MustCompile(`(?i)(\s*\[\s*\d*\s*\])+$|\s+array(\s*\[\s*\d*\s*\])?$`)
	typeGeneric = regexp.MustCompile(`^([a-z_][a-z0-9_$]*(?:\s+[a-z_][a-z0-9_$]*)*)\s*(?:\(([^)]*)\))?$`)
	typeSpaces  = regexp.MustCompile(`\s+`)
)

// harnessType maps a written column type to the text format_type prints,
// from the Typename grammar (gram.y REL_17_6 14294-14760) and the
// pg_catalog type names. It is the harness's own table, independent of the
// extractor's canonicalType. comparable is false for spellings it does not
// map (they are counted, not compared).
func harnessType(written string) (string, bool) {
	s := strings.TrimSpace(written)
	if strings.Contains(s, `"`) {
		// A quoted name stands for itself: it is the pg_catalog type of
		// that internal name (format_type prints the SQL spelling), or a
		// serial pseudo-type (parse_utilcmd.c compares the name);
		// schema qualifications are not compared.
		s = strings.ReplaceAll(s, `"`, "")
		if i := strings.LastIndex(s, "."); i >= 0 && !strings.Contains(s[i:], ")") {
			s = s[i+1:]
		}
		internal := map[string]string{"int4": "integer", "int2": "smallint", "int8": "bigint", "float4": "real", "float8": "double precision",
			"bool": "boolean", "varchar": "character varying", "timestamptz": "timestamp with time zone", "timestamp": "timestamp without time zone",
			"timetz": "time with time zone", "time": "time without time zone", "serial": "integer", "serial4": "integer", "bigserial": "bigint",
			"serial8": "bigint", "smallserial": "smallint", "serial2": "smallint", "numeric": "numeric", "bit": "bit", "varbit": "bit varying"}
		if m, ok := internal[s]; ok {
			return m, true
		}
		return s, true
	}
	s = strings.ToLower(typeSpaces.ReplaceAllString(s, " "))
	array := ""
	if loc := typeArray.FindStringIndex(s); loc != nil {
		array, s = "[]", strings.TrimSpace(s[:loc[0]])
	}
	if i := strings.LastIndex(s, "."); i >= 0 && !strings.Contains(s[:i], "(") {
		schema := s[:i]
		s = s[i+1:]
		if schema != "pg_catalog" && schema != "public" {
			return s + array, true
		}
	}
	zone := ""
	for _, z := range []string{"with time zone", "without time zone"} {
		if strings.HasSuffix(s, " "+z) {
			s, zone = strings.TrimSpace(strings.TrimSuffix(s, z)), z
		}
	}
	m := typeGeneric.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	name, mods := m[1], strings.ReplaceAll(m[2], " ", "")
	hasMods := strings.Contains(s, "(")
	paren := func(def string) string {
		if hasMods {
			return "(" + mods + ")"
		}
		return def
	}
	var out string
	switch name {
	case "int", "integer", "int4", "serial", "serial4":
		out = "integer"
	case "smallint", "int2", "smallserial", "serial2":
		out = "smallint"
	case "bigint", "int8", "bigserial", "serial8":
		out = "bigint"
	case "real", "float4":
		out = "real"
	case "float8", "double precision":
		out = "double precision"
	case "float":
		out = "double precision"
		if hasMods {
			p, err := strconv.Atoi(mods)
			if err != nil {
				return "", false
			}
			if p <= 24 {
				out = "real"
			}
		}
	case "bool", "boolean":
		out = "boolean"
	case "varchar", "character varying", "char varying", "national character varying", "national char varying", "nchar varying":
		out = "character varying" + paren("")
	case "char", "character", "nchar", "national character", "national char":
		out = "character" + paren("(1)")
	case "bpchar":
		out = "bpchar" + paren("")
	case "decimal", "dec", "numeric":
		out = "numeric"
		if hasMods {
			if !strings.Contains(mods, ",") {
				mods += ",0"
			}
			out += "(" + mods + ")"
		}
	case "timestamp", "timestamptz", "time", "timetz":
		base := strings.TrimSuffix(name, "tz")
		out = base + paren("")
		switch {
		case strings.HasSuffix(name, "tz") || zone == "with time zone":
			out += " with time zone"
		default:
			out += " without time zone"
		}
	case "bit":
		out = "bit" + paren("(1)")
	case "varbit", "bit varying":
		out = "bit varying" + paren("")
	default:
		if strings.Contains(name, " ") && name != "double precision" {
			if strings.HasPrefix(name, "interval") {
				out = name + paren("")
				break
			}
			return "", false
		}
		out = name + paren("")
	}
	if zone != "" && !strings.HasPrefix(out, "time") {
		return "", false
	}
	return out + array, true
}

// catalogType strips the schema qualification and quotes format_type adds
// for types outside the search_path.
func catalogType(t string) string {
	t = strings.ReplaceAll(t, `"`, "")
	if i := strings.Index(t, "."); i >= 0 && !strings.Contains(t[:i], "(") {
		t = t[i+1:]
	}
	return t
}

// lossCauses classifies the pass's may-fail and partial-table diagnostics
// by the contract row or default-branch kind that causes the coverage loss
// (A6). The patterns read the pass's own diagnostic texts.
var lossCauses = []struct {
	label string
	re    *regexp.Regexp
}{
	// Revision 1 classes: diagnostics that cite a precondition id.
	{"P5 values of DML (Rev 1.3)", regexp.MustCompile(`\[contract N3\.P5\]`)},
	{"P1 columns of DML and queries (Rev 1.2)", regexp.MustCompile(`\[contract N3\.P1\]`)},
	{"P1 name the set creates, not held (Rev 1)", regexp.MustCompile(`\[contract [A-Z][0-9a-z-]*\.P1\]`)},
	{"P2 shared namespace (Rev 1)", regexp.MustCompile(`\[contract [A-Z][0-9a-z-]*\.P2\]`)},
	{"P3 conditional form (Rev 1)", regexp.MustCompile(`\[contract [A-Z][0-9a-z-]*\.P3\]`)},
	{"P10 set reading (Rev 1)", regexp.MustCompile(`\[contract [A-Z][0-9a-z-]*\.P10\]`)},
	{"K2 enum labels (Rev 1)", regexp.MustCompile(`\[contract [A-Z][0-9a-z-]*\.K2\]`)},
	{"C4 CONCURRENTLY in a transaction (M4/M7)", regexp.MustCompile(`CONCURRENTLY`)},
	{"F1 refused in a transaction", regexp.MustCompile(`VACUUM|DROP DATABASE|DROP TABLESPACE|temporary schema|cannot run inside a transaction`)},
	{"C3 DROP CONSTRAINT (M5g, Revision 7)", regexp.MustCompile(`DROP CONSTRAINT|primary key may still exist|keys of this table are unknown|may have dropped|forgets`)},
	{"C5 generated-name shape", regexp.MustCompile(`generated name`)},
	{"C2 foreign-key / ALTER TYPE type pair (M3, M5c)", regexp.MustCompile(`(?i)not the same type|types? (differ|of the referenced|of both)|rebuilds the foreign key|incompatible`)},
	{"D statement not read (parse error, lexing failure)", regexp.MustCompile(`could not be read|could not be lexed|lexing`)},
	{"O4 lexing setting", regexp.MustCompile(`standard_conforming_strings|backslash_quote`)},
	{"O3 search_path / qualification", regexp.MustCompile(`search_path|qualification|temporary table of this name`)},
	{"O opaque statement / function call", regexp.MustCompile(`opaque|DO block|extension|function defined in the set|runs code`)},
	{"M5k/D unmodelled ALTER TABLE action", regexp.MustCompile(`is not modeled|not modeled`)},
	{"Revision 8 unknown facts (column, key or dependents may differ)", regexp.MustCompile(`may not exist|unknown|may meet|dependents|may be refused|may fail`)},
	{"R namespace record (relation or type may exist)", regexp.MustCompile(`may exist|exists|is created twice|namespace`)},
	{"R1/R4/R5 relation, type or dependency change", regexp.MustCompile(`view|type|domain|sequence|collation|depend`)},
}

func lossCause(message string) string {
	for _, c := range lossCauses {
		if c.re.MatchString(message) {
			return c.label
		}
	}
	return "other"
}

// losses counts the statements whose may-fail and partial-table
// diagnostics fall under each cause (a statement counts once per cause);
// first is the cause of the first possible failure (every later migration
// is applied: null because of it).
func (x *extraction) losses() (first string, mayFail, partial map[string]int) {
	mayFail, partial = map[string]int{}, map[string]int{}
	seen := map[string]bool{}
	for _, d := range x.diagnostics {
		// "file:line: bytes a-b: message" locates the statement.
		at := d.Message
		if i := strings.Index(at, ": bytes "); i >= 0 {
			if j := strings.Index(at[i+8:], ":"); j >= 0 {
				at = at[:i+8+j]
			}
		}
		cause := lossCause(d.Message)
		key := d.Code + "\x00" + cause + "\x00" + at
		switch d.Code {
		case "schema_migration_may_fail":
			if first == "" {
				first = cause + ": " + d.Message
			}
			if !seen[key] {
				mayFail[cause]++
			}
		case "schema_partial_table":
			if !seen[key] {
				partial[cause]++
			}
		}
		seen[key] = true
	}
	return first, mayFail, partial
}
