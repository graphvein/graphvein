package tableaccess

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The SQL oracle (T018h "Oracle design"; scripts/oracle/sql_tables.sh).
// Never run by CI. The script calls this test twice:
//
//	GRAPHVEIN_SQL_TABLES_ORACLE_PHASE=requests: link ROOT and write, per
//	migration set with a claimed SQL site, the set's migration files in
//	sqlx order and each claimed site's SQL text under DIR;
//	GRAPHVEIN_SQL_TABLES_ORACLE_PHASE=compare: link ROOT again and compare
//	each claimed site's (reads, writes) with the Postgres analyzer's
//	permission entries the script wrote next to the requests.
//
// with GRAPHVEIN_SQL_TABLES_ORACLE_ROOT=<repository> and
// GRAPHVEIN_SQL_TABLES_ORACLE_DIR=<exchange directory>.

// oracleSite is one claimed SQL site as the requests list it.
type oracleSite struct {
	Key     string `json:"key"`
	Set     int    `json:"set"`
	Index   int    `json:"index"`
	Partial bool   `json:"partial"`
}

func siteKey(s site) string {
	return fmt.Sprintf("%s:%d:%d", s.file, s.span.StartLine, s.span.StartColumn)
}

// claimedSQLSites lists the sites with at least one claim and a SQL text,
// grouped by their (first) migration set.
func claimedSQLSites(a analysis) ([]*migrationSet, map[string][]site) {
	var sets []*migrationSet
	bySet := map[string][]site{}
	for _, s := range a.sites {
		if len(s.reads)+len(s.writes) == 0 || len(s.sets) == 0 || s.sql == "" && (!toSQLSite(s) || os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_SQL_ONLY") != "") {
			continue
		}
		set := s.sets[0]
		if _, ok := bySet[set.name]; !ok {
			sets = append(sets, set)
		}
		bySet[set.name] = append(bySet[set.name], s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].name < sets[j].name })
	return sets, bySet
}

func TestSQLTablesOracle(t *testing.T) {
	phase := os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_PHASE")
	root, dir := os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_ROOT"), os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_DIR")
	if phase == "" || root == "" || dir == "" {
		t.Skip("set GRAPHVEIN_SQL_TABLES_ORACLE_PHASE, _ROOT and _DIR (scripts/oracle/sql_tables.sh)")
	}
	in := loadRoot(t, root)
	a := analyze(in)
	sets, bySet := claimedSQLSites(a)
	switch phase {
	case "requests":
		writeRequests(t, root, dir, sets, bySet, newDrizzleLinker(in))
	case "compare":
		compareOracle(t, dir, sets, bySet, nestedClaims(a))
	default:
		t.Fatalf("unknown phase %q", phase)
	}
}

func writeRequests(t *testing.T, root, dir string, sets []*migrationSet, bySet map[string][]site, drizzleLinker *drizzleLinker) {
	var index []oracleSite
	for n, set := range sets {
		setDir := filepath.Join(dir, "sets", strconv.Itoa(n))
		if err := os.MkdirAll(setDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var files []string
		for _, f := range set.files {
			files = append(files, filepath.Join(root, filepath.FromSlash(f)))
		}
		if set.name == sqlmodelGroupName {
			// The SQLModel group has no migrations: its schema is the
			// SQLModel.metadata DDL sql_tables_sqlalchemy.sh wrote.
			ddl := os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_MODEL_DDL")
			if ddl == "" {
				t.Fatalf("claimed CT-P5 sites need --model-ddl (scripts/oracle/sql_tables_sqlalchemy.sh writes it)")
			}
			files = []string{ddl}
		}
		if set.prismaSchema != "" {
			// A Prisma group has no migrations the link pass reads: the
			// script writes prisma.sql with prisma migrate diff from the
			// group's schema and pinned CLI version.
			write(t, filepath.Join(setDir, "prisma.schema"), filepath.Join(root, filepath.FromSlash(set.prismaSchema))+"\n")
			write(t, filepath.Join(setDir, "prisma.version"), set.prismaVersion+"\n")
			files = []string{filepath.Join(setDir, "prisma.sql")}
		}
		if set.drizzleRun != "" {
			// A Drizzle group: the script writes drizzle.sql with the
			// pinned drizzle-kit export of the group's schema modules.
			write(t, filepath.Join(setDir, "drizzle.run"), set.drizzleRun+"\n")
			files = []string{filepath.Join(setDir, "drizzle.sql")}
		}
		write(t, filepath.Join(setDir, "migrations.list"), strings.Join(files, "\n")+"\n")
		write(t, filepath.Join(setDir, "name"), set.name+"\n")
		for m, s := range bySet[set.name] {
			text := s.sql
			if s.row == "CT-P5" {
				text = numberBinds(text)
			}
			if toSQLSite(s) {
				// The script's toSQL harness writes site-m.sql from the
				// builder text and its bindings.
				writeToSQLRequest(t, filepath.Join(setDir, fmt.Sprintf("site-%d.request.json", m)), drizzleLinker, s)
			} else {
				write(t, filepath.Join(setDir, fmt.Sprintf("site-%d.sql", m)), text)
			}
			index = append(index, oracleSite{Key: siteKey(s), Set: n, Index: m, Partial: s.unknown != nil && s.unknown.Partial})
		}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "sites.json"), string(data)+"\n")
	t.Logf("%d sets, %d claimed SQL sites", len(sets), len(index))
}

// numberBinds rewrites SQLAlchemy text() binds (:name) as $n for PREPARE,
// outside quotes and casts (::).
func numberBinds(sql string) string {
	var b strings.Builder
	names := map[string]int{}
	quote := byte(0)
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ':' && i+1 < len(sql) && sql[i+1] == ':':
			b.WriteString("::")
			i++
			continue
		case c == ':' && i+1 < len(sql) && (sql[i+1] == '_' || sql[i+1] >= 'a' && sql[i+1] <= 'z' || sql[i+1] >= 'A' && sql[i+1] <= 'Z') && (i == 0 || sql[i-1] != ':'):
			j := i + 1
			for j < len(sql) && (sql[j] == '_' || sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= 'A' && sql[j] <= 'Z' || sql[j] >= '0' && sql[j] <= '9') {
				j++
			}
			name := sql[i+1 : j]
			if names[name] == 0 {
				names[name] = len(names) + 1
			}
			fmt.Fprintf(&b, "$%d", names[name])
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// relations reads the script's pg_class dump: oid, schema, name, relkind.
func relations(t *testing.T, path string) map[string][2]string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		cells := strings.Split(line, "\t")
		if len(cells) == 4 {
			out[cells[0]] = [2]string{cells[1] + "." + cells[2], cells[3]}
		}
	}
	return out
}

func compareOracle(t *testing.T, dir string, sets []*migrationSet, bySet map[string][]site, nested map[string]nestedClaim) {
	compared, agreed, partial := 0, 0, 0
	for n, set := range sets {
		setDir := filepath.Join(dir, "sets", strconv.Itoa(n))
		rels := relations(t, filepath.Join(setDir, "relations.tsv"))
		known := map[string]bool{}
		for _, tbl := range set.tables {
			schema := tbl.schema
			if schema == "" {
				schema = "public"
			}
			known[schema+"."+tbl.name] = true
		}
		for m, s := range bySet[set.name] {
			logData, err := os.ReadFile(filepath.Join(setDir, fmt.Sprintf("site-%d.log", m)))
			if err != nil {
				t.Fatal(err)
			}
			status, _ := os.ReadFile(filepath.Join(setDir, fmt.Sprintf("site-%d.status", m)))
			if strings.TrimSpace(string(status)) != "0" {
				t.Errorf("%s: the analyzer refused the statement (harness failure):\n%s", siteKey(s), logData)
				continue
			}
			reads, writes, err := analyzerAccess(string(logData), rels)
			if err != nil {
				t.Errorf("%s: %v", siteKey(s), err)
				continue
			}
			compared++
			claimedReads, claimedWrites := names(s.reads, s.tables), names(s.writes, s.tables)
			isPartial := s.unknown != nil && s.unknown.Partial
			if n, ok := nested[siteKey(s)]; ok {
				// A toSQL builder's SQL holds its nested sites (subqueries
				// that are sites of their own): their claims count, and an
				// unbound nested site or a partial one makes this a subset
				// comparison (the SQLAlchemy oracle's rule).
				claimedReads, claimedWrites = n.reads, n.writes
				if n.subset {
					partial++
					if !subset(claimedReads, reads) || !subset(claimedWrites, writes) {
						t.Errorf("DISAGREEMENT %s: claimed reads %v writes %v are not within the analyzer's reads %v writes %v\n%s", siteKey(s), claimedReads, claimedWrites, reads, writes, s.builder)
						continue
					}
					agreed++
					continue
				}
			}
			if isPartial {
				partial++
				reads, writes = onlyKnown(reads, known, claimedReads), onlyKnown(writes, known, claimedWrites)
			}
			if !reflect.DeepEqual(reads, claimedReads) || !reflect.DeepEqual(writes, claimedWrites) {
				t.Errorf("DISAGREEMENT %s: claimed reads %v writes %v, analyzer reads %v writes %v\n%s", siteKey(s), claimedReads, claimedWrites, reads, writes, s.sql)
				continue
			}
			agreed++
		}
	}
	t.Logf("oracle: %d sets, %d claimed sites compared (%d partial), %d agree, %d disagree", len(sets), compared, partial, agreed, compared-agreed)
	if out := os.Getenv("GRAPHVEIN_SQL_TABLES_ORACLE_OUT"); out != "" {
		write(t, out, fmt.Sprintf("{\"sets\": %d, \"compared\": %d, \"partial\": %d, \"agree\": %d, \"disagree\": %d}\n", len(sets), compared, partial, agreed, compared-agreed))
	}
}

// onlyKnown keeps the analyzer's relations a partial site could have
// claimed: those it claimed and those that are tables of the set (a view
// or another relation the set does not hold is the unresolved part).
func onlyKnown(rels []string, known map[string]bool, claimed []string) []string {
	out := []string{}
	for _, r := range rels {
		if known[r] || contains2(claimed, r) {
			out = append(out, r)
		}
	}
	return out
}

func contains2(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func names(m map[string]map[string]bool, tables map[string]string) []string {
	out := []string{}
	for id := range m {
		out = append(out, tables[id])
	}
	sort.Strings(out)
	return out
}

// Permission bits (parsenodes.h AclMode).
const (
	aclInsert = 1
	aclSelect = 2
	aclUpdate = 4
	aclDelete = 8
)

// analyzerAccess reads the debug_print_parse trees of a psql log and maps
// every RTEPERMISSIONINFO entry to a read or a write, with the two
// normalisations (A1: a SELECT query's UPDATE bit is its lock clause; A2:
// the SELECT-only entry of EXCLUDED is not a read).
func analyzerAccess(log string, rels map[string][2]string) ([]string, []string, error) {
	reads, writes := map[string]bool{}, map[string]bool{}
	trees := 0
	for _, part := range strings.Split(log, "DETAIL:")[1:] {
		if i := strings.Index(part, "\nLOG:"); i >= 0 {
			part = part[:i]
		}
		node, err := parseNodeTree(part)
		if err != nil {
			return nil, nil, err
		}
		trees++
		var walk func(v any)
		walk = func(v any) {
			switch x := v.(type) {
			case *pgNode:
				if x.typ == "QUERY" {
					if err2 := queryAccess(x, rels, reads, writes); err2 != nil && err == nil {
						err = err2
					}
				}
				for _, f := range x.fields {
					walk(f)
				}
			case []any:
				for _, e := range x {
					walk(e)
				}
			}
		}
		walk(node)
		if err != nil {
			return nil, nil, err
		}
	}
	if trees == 0 {
		return nil, nil, fmt.Errorf("no parse tree in the log")
	}
	return sortedSet(reads), sortedSet(writes), nil
}

func queryAccess(q *pgNode, rels map[string][2]string, reads, writes map[string]bool) error {
	command := atom(q.fields["commandType"])
	excluded := map[string]bool{}
	if list, ok := q.fields["rtable"].([]any); ok {
		for _, e := range list {
			rte, ok := e.(*pgNode)
			if !ok || rte.typ != "RANGETBLENTRY" {
				continue
			}
			eref, _ := rte.fields["eref"].(*pgNode)
			if atom(rte.fields["relkind"]) == "c" && eref != nil && atom(eref.fields["aliasname"]) == "excluded" {
				excluded[atom(rte.fields["perminfoindex"])] = true
			}
		}
	}
	list, _ := q.fields["rteperminfos"].([]any)
	for k, e := range list {
		info, ok := e.(*pgNode)
		if !ok || info.typ != "RTEPERMISSIONINFO" {
			continue
		}
		perms, err := strconv.Atoi(atom(info.fields["requiredPerms"]))
		if err != nil {
			return fmt.Errorf("requiredPerms %q", atom(info.fields["requiredPerms"]))
		}
		if excluded[strconv.Itoa(k+1)] && perms == aclSelect {
			continue
		}
		if command == "1" {
			perms &^= aclUpdate
		}
		rel, ok := rels[atom(info.fields["relid"])]
		if !ok {
			return fmt.Errorf("relid %s not in pg_class", atom(info.fields["relid"]))
		}
		if perms&(aclInsert|aclUpdate|aclDelete) != 0 {
			writes[rel[0]] = true
		} else {
			reads[rel[0]] = true
		}
	}
	return nil
}

// pgNode is one node of Postgres's outfuncs text format: {TYPE :field
// value ...}. Values are nodes, lists ([]any) or atoms (string).
type pgNode struct {
	typ    string
	fields map[string]any
}

func atom(v any) string {
	s, _ := v.(string)
	return s
}

// parseNodeTree reads the first node of a node-tree text.
func parseNodeTree(text string) (any, error) {
	p := &treeParser{s: text}
	p.skip()
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return nil, fmt.Errorf("no node tree")
	}
	return p.value()
}

type treeParser struct {
	s string
	i int
}

func (p *treeParser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\n' || p.s[p.i] == '\t' || p.s[p.i] == '\r') {
		p.i++
	}
}

// token reads an atom: up to whitespace or a structural character, with
// backslash escapes.
func (p *treeParser) token() string {
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\\' && p.i+1 < len(p.s) {
			b.WriteByte(p.s[p.i+1])
			p.i += 2
			continue
		}
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '{' || c == '}' || c == '(' || c == ')' {
			break
		}
		b.WriteByte(c)
		p.i++
	}
	return b.String()
}

func (p *treeParser) value() (any, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of tree")
	}
	switch p.s[p.i] {
	case '{':
		p.i++
		p.skip()
		n := &pgNode{typ: p.token(), fields: map[string]any{}}
		field := ""
		for {
			p.skip()
			if p.i >= len(p.s) {
				return nil, fmt.Errorf("unterminated node %s", n.typ)
			}
			switch c := p.s[p.i]; {
			case c == '}':
				p.i++
				return n, nil
			case c == ':':
				field = p.token()[1:]
				v, err := p.value()
				if err != nil {
					return nil, err
				}
				n.fields[field] = v
			case c == '[':
				// A datum's bytes: skip to the closing bracket.
				end := strings.IndexByte(p.s[p.i:], ']')
				if end < 0 {
					return nil, fmt.Errorf("unterminated datum")
				}
				p.i += end + 1
			default:
				// An extra token of the last field (a datum's length).
				if _, err := p.value(); err != nil {
					return nil, err
				}
			}
		}
	case '(':
		p.i++
		var list []any
		for {
			p.skip()
			if p.i >= len(p.s) {
				return nil, fmt.Errorf("unterminated list")
			}
			if p.s[p.i] == ')' {
				p.i++
				return list, nil
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
	case ')', '}':
		return nil, fmt.Errorf("unexpected %q at %d", p.s[p.i], p.i)
	}
	if p.s[p.i] == '[' {
		end := strings.IndexByte(p.s[p.i:], ']')
		if end < 0 {
			return nil, fmt.Errorf("unterminated datum")
		}
		p.i += end + 1
		return "", nil
	}
	return p.token(), nil
}

// TestAnalyzerAccess checks the analyzer reading and its two
// normalisations against psql logs captured from the fixture's oracle run
// (testdata/analyzer, Postgres 17.12): the EXCLUDED entry of an upsert is
// no read (A2), a lock clause reads (A1), an UPDATE ... FROM itself reads
// and writes its table, a writable CTE writes in its own query.
func TestAnalyzerAccess(t *testing.T) {
	rels := relations(t, "testdata/analyzer/relations.tsv")
	cases := map[string][2][]string{
		"upsert.log":       {{}, {"public.tags"}},
		"lock.log":         {{"public.accounts"}, {}},
		"self_update.log":  {{"public.posts"}, {"public.posts"}},
		"writable_cte.log": {{}, {"public.comments", "public.comments_archive"}},
	}
	for file, want := range cases {
		data, err := os.ReadFile(filepath.Join("testdata/analyzer", file))
		if err != nil {
			t.Fatal(err)
		}
		reads, writes, err := analyzerAccess(string(data), rels)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !reflect.DeepEqual(reads, want[0]) || !reflect.DeepEqual(writes, want[1]) {
			t.Errorf("%s: reads %v writes %v, want %v", file, reads, writes, want)
		}
	}
}
