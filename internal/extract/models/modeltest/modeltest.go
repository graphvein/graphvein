// Package modeltest holds the contract-test helpers every ORM front end of
// the model kit shares (T018g0, plan shared piece 4): reading a Markdown
// claim-contract table, recording the row tags the claim branches emit,
// checking that the code table, the docs table and the emitted tags stay
// 1:1, and checking the emitted column claims (CheckClaims). Only tests
// import it. It does not import the core package, so the core's own tests
// can use it too.
package modeltest

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TableRows reads the row lines of a Markdown file's "## Claim contract"
// table, keyed by row id (the first cell). A duplicate id fails the test.
func TableRows(t testing.TB, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatalf("%s: no Claim contract section", path)
	}
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| id |") {
			continue
		}
		cells := strings.Split(strings.ReplaceAll(line, `\|`, "\x00"), "|")
		id := strings.TrimSpace(cells[1])
		if _, dup := rows[id]; dup {
			t.Fatalf("%s: duplicate row %s", path, id)
		}
		rows[id] = line
	}
	return rows
}

// Tags records the row tags a front end's claim branches emit. A tag
// "<row>/<form>" names one listed form of a multi-form row and also counts
// as its row.
type Tags struct {
	Rows  map[string]bool
	Forms map[string]bool
}

// NewTags returns an empty recorder.
func NewTags() *Tags { return &Tags{Rows: map[string]bool{}, Forms: map[string]bool{}} }

// Tag records one tag; pass it as the front end's tag hook.
func (r *Tags) Tag(row string) {
	if i := strings.Index(row, "/"); i >= 0 {
		r.Forms[row] = true
		row = row[:i]
	}
	r.Rows[row] = true
}

// CheckTags checks the emitted tags against the code table: branches maps
// every row id to the function holding its claim branch ("" for a row
// without one), forms lists the forms of multi-form rows. Every row with a
// branch must be reached and every row without one never tagged; every tag
// must be a row, and every listed form reached and every reached form
// listed.
func CheckTags(t testing.TB, tags *Tags, branches map[string]string, forms map[string][]string) {
	t.Helper()
	for _, id := range sortedKeys(branches) {
		fn := branches[id]
		if fn != "" && !tags.Rows[id] {
			t.Errorf("row %s has a claim branch (%s) that the fixtures never reach", id, fn)
		}
		if fn == "" && tags.Rows[id] {
			t.Errorf("row %s has no claim branch but a tag was emitted", id)
		}
	}
	for _, id := range sortedKeys(tags.Rows) {
		if _, ok := branches[id]; !ok {
			t.Errorf("tag %s has no contract row", id)
		}
	}
	listed := map[string]bool{}
	for _, id := range sortedKeys(forms) {
		for _, f := range forms[id] {
			listed[id+"/"+f] = true
			if !tags.Forms[id+"/"+f] {
				t.Errorf("row %s: form %s is never reached by the fixtures", id, f)
			}
		}
	}
	for _, f := range sortedKeys(tags.Forms) {
		if !listed[f] {
			t.Errorf("form tag %s is not listed in the row forms", f)
		}
	}
}

// CheckDocs checks that the docs table (docs, from TableRows) and the code
// table (branches) list the same rows, and that the docs name each row's
// claim function in the column at index cell ("`fn`", or "none" for a row
// without a branch).
func CheckDocs(t testing.TB, docs map[string]string, branches map[string]string, cell int) {
	t.Helper()
	for _, id := range sortedKeys(docs) {
		fn, ok := branches[id]
		if !ok {
			t.Errorf("docs row %s has no entry in the code table", id)
			continue
		}
		want := "`" + fn + "`"
		if fn == "" {
			want = "none"
		}
		cells := strings.Split(docs[id], "|")
		if len(cells) <= cell || strings.TrimSpace(cells[cell]) != want {
			t.Errorf("row %s: docs name %q, code %q", id, strings.TrimSpace(cells[min(cell, len(cells)-1)]), want)
		}
	}
	for _, id := range sortedKeys(branches) {
		if _, ok := docs[id]; !ok {
			t.Errorf("code row %s is not in the docs table", id)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Claim mirrors the core's emitted claim (models.Claim) through JSON, so
// CheckClaims reads claims in memory and after a store round trip alike.
type Claim struct {
	Fact         string         `json:"fact"`
	Dialect      string         `json:"dialect"`
	Row          string         `json:"row"`
	File         string         `json:"file"`
	Span         *contract.Span `json:"span"`
	Verification string         `json:"verification"`
}

// CheckClaims checks the column claims of a front end's emitted nodes
// (T018g0 Revision 2): every column carries a claims list; every known fact
// of a read column (facts: models.ClaimFacts) has exactly one claim; an
// unknown fact or a column with state unknown has none; and every claim
// names a row, a file, a line and a non-empty verification. Every front
// end's tests run it over their fixtures. It returns the claims by column
// identity for the front end's own checks (rows per fact, forms). Each
// table's verification_counts must equal its columns' claims counted by
// verification (ADR 0057: /schema/sets serves the stored counts instead of
// counting the claims).
//
// ADR 0065: a fact a column states per dialect (by_dialect) has one claim
// per dialect whose value is known, naming the dialect, and none of its
// own; its flat value is null. CheckDialectFacts and CheckTypeFamilies
// (run here) check the per-dialect and type-family shapes.
func CheckClaims(t testing.TB, nodes []contract.Node, facts []string) map[string][]Claim {
	t.Helper()
	known := map[string]bool{}
	for _, f := range facts {
		known[f] = true
	}
	out := map[string][]Claim{}
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		a := n.Attributes
		raw, err := json.Marshal(a["claims"])
		var list []Claim
		if err == nil {
			err = json.Unmarshal(raw, &list)
		}
		if err != nil || a["claims"] == nil {
			t.Errorf("%s: claims %T is not a list of claims", n.Identity, a["claims"])
			continue
		}
		out[n.Identity] = list
		byDialect := DialectEntries(n)
		seen := map[string]bool{}
		for _, c := range list {
			if !known[c.Fact] {
				t.Errorf("%s: claim on %q, not a claim fact", n.Identity, c.Fact)
			}
			key := c.Fact + "@" + c.Dialect
			if seen[key] {
				t.Errorf("%s: two claims for %s", n.Identity, key)
			}
			seen[key] = true
			value := a[c.Fact]
			if c.Dialect != "" {
				value = byDialect[c.Dialect][c.Fact]
			}
			if a["state"] != "read" || value == nil {
				t.Errorf("%s: claim %+v on an unknown fact", n.Identity, c)
			}
			if c.Row == "" || c.File == "" || c.Span == nil || c.Span.StartLine < 1 || c.Verification == "" {
				t.Errorf("%s: incomplete claim %+v", n.Identity, c)
			}
		}
		if a["state"] != "read" {
			continue
		}
		for _, f := range facts {
			if a[f] != nil && !seen[f+"@"] {
				t.Errorf("%s: known fact %s has no claim", n.Identity, f)
			}
			for d, entry := range byDialect {
				if entry[f] != nil && !seen[f+"@"+d] {
					t.Errorf("%s: known fact %s on %s has no claim", n.Identity, f, d)
				}
			}
		}
	}
	checkVerificationCounts(t, nodes, out)
	CheckDialectFacts(t, nodes)
	CheckTypeFamilies(t, nodes)
	return out
}

// DialectEntries reads a column node's by_dialect attribute (ADR 0065):
// the per-dialect facts by dialect; nil when the column states none.
func DialectEntries(n contract.Node) map[string]map[string]any {
	raw, err := json.Marshal(n.Attributes["by_dialect"])
	var out map[string]map[string]any
	if err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// DialectFacts mirrors the core's models.DialectFacts (this package does
// not import the core); a core test checks that the two lists are equal.
var DialectFacts = []string{
	"type", "type_family", "length", "timezone", "precision", "scale",
	"autoincrement", "server_default", "server_default_text",
}

var dialectFacts = func() map[string]bool {
	out := map[string]bool{}
	for _, f := range DialectFacts {
		out[f] = true
	}
	return out
}()

// CheckDialectFacts checks the per-dialect shape (ADR 0065): a table
// without a dialects list has no column with by_dialect; a table with one
// has no single dialect and no column with a flat per-dialect fact
// (T018g0b Revision 1, A3: such a group states them per dialect); each
// by_dialect lists exactly the table's dialects, each entry the same
// facts, all per-dialect facts, and the flat value of each of those facts
// is null.
func CheckDialectFacts(t testing.TB, nodes []contract.Node) {
	t.Helper()
	for _, table := range nodes {
		if table.Kind != "table" {
			continue
		}
		dialects, _ := stringList(table.Attributes["dialects"])
		if dialects != nil && table.Attributes["dialect"] != nil {
			t.Errorf("%s: both dialect %v and dialects %v", table.Identity, table.Attributes["dialect"], dialects)
		}
		for _, col := range tableColumns(nodes, table) {
			if dialects != nil {
				for _, f := range DialectFacts {
					if col.Attributes[f] != nil {
						t.Errorf("%s: %s is %v once in a table with dialects %v", col.Identity, f, col.Attributes[f], dialects)
					}
				}
			}
			entries := DialectEntries(col)
			if entries == nil {
				continue
			}
			if dialects == nil {
				t.Errorf("%s: by_dialect in a table without dialects", col.Identity)
				continue
			}
			got := sortedKeys(entries)
			if !reflect.DeepEqual(got, dialects) {
				t.Errorf("%s: by_dialect dialects %v, table dialects %v", col.Identity, got, dialects)
			}
			var facts []string
			for i, d := range got {
				keys := sortedKeys(entries[d])
				if i == 0 {
					facts = keys
				} else if !reflect.DeepEqual(keys, facts) {
					t.Errorf("%s: by_dialect %s lists %v, %s lists %v", col.Identity, d, keys, got[0], facts)
				}
			}
			for _, f := range facts {
				if !dialectFacts[f] {
					t.Errorf("%s: %s is not a per-dialect fact", col.Identity, f)
				}
				if col.Attributes[f] != nil {
					t.Errorf("%s: %s is %v beside its per-dialect values", col.Identity, f, col.Attributes[f])
				}
			}
		}
	}
}

// CheckTypeFamilies checks every type_family (ADR 0065), flat or per
// dialect: an object {name, unknown} with a non-empty name and a non-empty
// list of distinct modifiers among length, precision and scale; the type
// beside it null; and each modifier it lists unknown null.
func CheckTypeFamilies(t testing.TB, nodes []contract.Node) {
	t.Helper()
	for _, n := range nodes {
		if n.Kind != "column" {
			continue
		}
		maps := map[string]map[string]any{"": n.Attributes}
		for d, e := range DialectEntries(n) {
			maps[d] = e
		}
		for d, m := range maps {
			raw := m["type_family"]
			if raw == nil {
				continue
			}
			where := n.Identity
			if d != "" {
				where += " (" + d + ")"
			}
			data, _ := json.Marshal(raw)
			var fam struct {
				Name    *string  `json:"name"`
				Unknown []string `json:"unknown"`
			}
			if err := json.Unmarshal(data, &fam); err != nil || fam.Name == nil || *fam.Name == "" || len(fam.Unknown) == 0 {
				t.Errorf("%s: type_family %s is not {name, unknown}", where, data)
				continue
			}
			if m["type"] != nil {
				t.Errorf("%s: type %v beside type_family %s", where, m["type"], *fam.Name)
			}
			seen := map[string]bool{}
			for _, mod := range fam.Unknown {
				// "modifiers" is the kit's generic unknown modifier
				// (models.ModifiersUnknown): listed alone, no attribute.
				generic := mod == "modifiers" && len(fam.Unknown) == 1
				if seen[mod] || (mod != "length" && mod != "precision" && mod != "scale" && !generic) {
					t.Errorf("%s: type_family %s lists %q", where, *fam.Name, mod)
				}
				seen[mod] = true
				if m[mod] != nil {
					t.Errorf("%s: type_family %s lists %s unknown, but it is %v", where, *fam.Name, mod, m[mod])
				}
			}
		}
	}
}

// tableColumns are the column nodes of a table as the core writes them
// (see checkVerificationCounts).
func tableColumns(nodes []contract.Node, table contract.Node) []contract.Node {
	var out []contract.Node
	for _, col := range nodes {
		rest, ok := strings.CutPrefix(col.QualifiedName, table.QualifiedName+".")
		if col.Kind == "column" && col.File == table.File && ok && (rest == col.Name || !strings.Contains(rest, ".")) {
			out = append(out, col)
		}
	}
	return out
}

func stringList(v any) ([]string, bool) {
	raw, err := json.Marshal(v)
	var out []string
	if err != nil || v == nil || json.Unmarshal(raw, &out) != nil {
		return nil, false
	}
	return out, true
}

// checkVerificationCounts compares each table's stored verification_counts
// with the claims of its columns: the column nodes in the same file whose
// qualified name is the table's plus "." plus their name (or, for a column
// with an identity name, ADR 0057, plus that name), as the core writes
// them.
func checkVerificationCounts(t testing.TB, nodes []contract.Node, claims map[string][]Claim) {
	t.Helper()
	for _, table := range nodes {
		if table.Kind != "table" {
			continue
		}
		raw, err := json.Marshal(table.Attributes["verification_counts"])
		var stored map[string]int
		if err == nil {
			err = json.Unmarshal(raw, &stored)
		}
		if err != nil || stored == nil {
			t.Errorf("%s: verification_counts %v is not a count object", table.Identity, table.Attributes["verification_counts"])
			continue
		}
		want := map[string]int{}
		for _, col := range tableColumns(nodes, table) {
			for _, c := range claims[col.Identity] {
				want[c.Verification]++
			}
		}
		if !reflect.DeepEqual(stored, want) {
			t.Errorf("%s: verification_counts %v, its columns' claims %v", table.Identity, stored, want)
		}
	}
}
