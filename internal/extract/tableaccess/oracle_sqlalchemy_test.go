package tableaccess

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The SQLAlchemy fixture oracle (Revision 1, O6; scripts/oracle/
// sql_tables_sqlalchemy.sh). Never run by CI. Phase requests links ROOT and
// writes, for every claimed CT-P1/CT-P2 site, its module, the statement's
// source text and the claimed table names to FILE; the script's Python
// half evaluates and compiles each statement with the pinned SQLAlchemy and
// compares the tables.
//
//	GRAPHVEIN_SQLALCHEMY_ORACLE_ROOT=<fixture> GRAPHVEIN_SQLALCHEMY_ORACLE_REQUESTS=<file>

type sqlalchemyRequest struct {
	Key    string   `json:"key"`
	Row    string   `json:"row"`
	Module string   `json:"module"`
	Text   string   `json:"text"`
	Reads  []string `json:"reads"`
	Writes []string `json:"writes"`
	// Partial marks a site with a partial unknown record: its claims must
	// be a subset of the oracle's tables, not equal to them.
	Partial bool `json:"partial"`
}

func TestSQLAlchemyOracle(t *testing.T) {
	root, file := os.Getenv("GRAPHVEIN_SQLALCHEMY_ORACLE_ROOT"), os.Getenv("GRAPHVEIN_SQLALCHEMY_ORACLE_REQUESTS")
	if root == "" || file == "" {
		t.Skip("set GRAPHVEIN_SQLALCHEMY_ORACLE_ROOT and _REQUESTS (scripts/oracle/sql_tables_sqlalchemy.sh)")
	}
	in := loadRoot(t, root)
	requests := sqlalchemyRequests(t, in, analyze(in))
	data, err := json.MarshalIndent(requests, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, file, string(data)+"\n")
	t.Logf("%d claimed CT-P1/CT-P2 sites", len(requests))
}

// sqlalchemyRequests lists the claimed CT-P1 and CT-P2 sites of an
// analysis with their statement text and claimed table names.
func sqlalchemyRequests(t testing.TB, in Input, a analysis) []sqlalchemyRequest {
	names := map[string]string{}
	for _, m := range sqlmodelTables(in.Models) {
		names[m.identity] = m.name
		if m.schema != "" {
			names[m.identity] = m.schema + "." + m.name
		}
	}
	tableNames := func(m map[string]map[string]bool) []string {
		out := []string{}
		for id := range m {
			out = append(out, names[id])
		}
		sort.Strings(out)
		return out
	}
	requests := []sqlalchemyRequest{}
	for _, s := range a.sites {
		if s.row != "CT-P1" && s.row != "CT-P2" || len(s.reads)+len(s.writes) == 0 {
			continue
		}
		text, ok := spanText(in.Sources[s.file], s.span.StartLine, s.span.StartColumn, s.span.EndLine, s.span.EndColumn)
		if !ok {
			t.Fatalf("%s: no source for the span", siteKey(s))
		}
		module := strings.ReplaceAll(strings.TrimSuffix(s.file, ".py"), "/", ".")
		requests = append(requests, sqlalchemyRequest{Key: siteKey(s), Row: s.row, Module: module, Text: text, Reads: tableNames(s.reads), Writes: tableNames(s.writes), Partial: s.unknown != nil && s.unknown.Partial})
	}
	return requests
}

func spanText(src []byte, startLine, startColumn, endLine, endColumn int) (string, bool) {
	start, end := offsetOf(src, startLine, startColumn), offsetOf(src, endLine, endColumn)
	if start < 0 || end < start {
		return "", false
	}
	return string(src[start:end]), true
}

// TestSQLAlchemyRequests keeps the requests phase working on the fixture:
// every claimed CT-P1/CT-P2 site has its statement text.
func TestSQLAlchemyRequests(t *testing.T) {
	in := loadRoot(t, filepath.FromSlash(sqlmodelFixtureRoot))
	requests := sqlalchemyRequests(t, in, analyze(in))
	if len(requests) == 0 {
		t.Fatal("no claimed CT-P1/CT-P2 site in the fixture")
	}
	for _, r := range requests {
		if !strings.Contains(r.Text, "(") {
			t.Errorf("%s: statement text %q", r.Key, r.Text)
		}
	}
}
