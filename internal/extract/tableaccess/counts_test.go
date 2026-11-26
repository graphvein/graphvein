package tableaccess

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestTableAccessCounts prints per-row site counts and unknown records by
// reason for one repository (the corpus counts of the implementation gates).
// Never run by CI: set GRAPHVEIN_TABLE_ACCESS_COUNTS_ROOT to a scratch copy
// of a corpus and run with -v. GRAPHVEIN_TABLE_ACCESS_SITES_OUT names a
// file that receives one JSON line per site and per owner unknown record,
// which the per-site reconciliation against the inventory reads.
func TestTableAccessCounts(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_TABLE_ACCESS_COUNTS_ROOT")
	if root == "" {
		t.Skip("set GRAPHVEIN_TABLE_ACCESS_COUNTS_ROOT to a repository")
	}
	in := loadRoot(t, root)
	a := analyze(in)
	counts := map[string]int{}
	claimed, fact, inferred := 0, 0, 0
	for _, s := range a.sites {
		area := pathArea(s.file)
		counts[area+" sites "+s.row]++
		if len(s.reads)+len(s.writes) > 0 {
			counts[area+" claimed "+s.row]++
			if s.fact {
				counts[area+" fact "+s.row]++
			}
			claimed++
			if s.fact {
				fact++
			} else {
				inferred++
			}
		}
		if s.unknown != nil {
			counts[area+" unknown "+s.row+" "+reasonCode(s.unknown.Reason)]++
		}
		for _, u := range s.extra {
			counts[area+" unknown "+u.Row+" "+reasonCode(u.Reason)]++
		}
	}
	for owner, list := range a.unknowns {
		for _, u := range list {
			counts[pathArea(owner)+" unknown "+u.Row+" "+reasonCode(u.Reason)]++
		}
	}
	edges := map[string]int{}
	for _, e := range Link(in).Edges {
		edges[e.Kind+"/"+e.Provenance]++
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %d", k, counts[k])
	}
	t.Logf("sites %d, claimed %d (fact %d, inferred %d), edges %v", len(a.sites), claimed, fact, inferred, edges)
	if out := os.Getenv("GRAPHVEIN_TABLE_ACCESS_SITES_OUT"); out != "" {
		if err := writeSiteLines(out, a); err != nil {
			t.Fatal(err)
		}
	}
}

// siteLine is one line of the per-site dump: a site with its claims and
// unknown entries, or (Site false) an unknown record of an owner that
// belongs to no site, such as an execution's statement_not_traced.
type siteLine struct {
	Site     bool                `json:"site"`
	File     string              `json:"file"`
	Line     int                 `json:"line"`
	Owner    string              `json:"owner"`
	Row      string              `json:"row"`
	Rows     []string            `json:"rows,omitempty"`
	Fact     bool                `json:"fact"`
	Role     string              `json:"role,omitempty"`
	Verify   string              `json:"verification,omitempty"`
	Reads    map[string][]string `json:"reads,omitempty"`
	Writes   map[string][]string `json:"writes,omitempty"`
	SQL      string              `json:"sql,omitempty"`
	Unknowns []Unknown           `json:"unknowns,omitempty"`
}

// writeSiteLines writes the per-site dump, sorted by file and line.
func writeSiteLines(path string, a analysis) error {
	var lines []siteLine
	ops := func(m map[string]map[string]bool) map[string][]string {
		if len(m) == 0 {
			return nil
		}
		out := map[string][]string{}
		for target, set := range m {
			out[target] = sortedSet(set)
		}
		return out
	}
	for _, s := range a.sites {
		l := siteLine{Site: true, File: s.file, Line: s.span.StartLine, Owner: s.owner, Row: s.row, Rows: s.rows,
			Fact: s.fact, Role: s.role, Verify: s.verification, Reads: ops(s.reads), Writes: ops(s.writes), SQL: s.sql}
		if s.unknown != nil {
			l.Unknowns = append(l.Unknowns, *s.unknown)
		}
		l.Unknowns = append(l.Unknowns, s.extra...)
		lines = append(lines, l)
	}
	for owner, list := range a.unknowns {
		for _, u := range list {
			lines = append(lines, siteLine{File: ownerFile(owner), Line: u.Line, Owner: owner, Row: u.Row, Unknowns: []Unknown{u}})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].File != lines[j].File {
			return lines[i].File < lines[j].File
		}
		return lines[i].Line < lines[j].Line
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// ownerFile is the file part of an owner node identity ("kind:file#name").
func ownerFile(owner string) string {
	_, rest, _ := strings.Cut(owner, ":")
	file, _, _ := strings.Cut(rest, "#")
	return file
}

// pathArea splits counts the way the inventory does: tests, Alembic
// revision modules, production.
func pathArea(path string) string {
	switch {
	case strings.HasPrefix(path, "tests/") || strings.Contains(path, "/tests/") || strings.Contains(path, "#tests/") || strings.Contains(path, "/test_"):
		return "test"
	case strings.Contains(path, "alembic/versions/") || strings.Contains(path, "migrations/versions/"):
		return "alembic"
	}
	return "prod"
}

// reasonCode drops a reason's detail ("code: detail").
func reasonCode(reason string) string {
	code, _, _ := strings.Cut(reason, ":")
	return code
}
