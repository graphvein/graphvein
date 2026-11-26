package schema

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Provenance of facts (T018f, ADR 0055 option 1A). The records say which
// contract row set each known fact and where: column and db_index records
// carry claims (one per known fact, naming the statement that last changed
// it), table reasons carry the row or precondition id whose branch made the
// table partial, and migration records list their opaque statements. These
// are metadata about claims the contract already makes: they add no claim
// and change no fact.
//
// The claims are found the way TestContractFacts checks rows: the facts of
// every held column and index are fingerprinted before and after each
// statement, and a fact whose fingerprint changed (or the facts of a new
// column or index) gets the statement as its claim, under the statement's
// row that fits the fact (claimRow). Fingerprints use object identity, not
// names, so a table rename does not rewrite its columns' references.

// claim is the statement behind one known fact.
type claim struct {
	row string
	m   *migration
	at  byteSpan
}

// Column facts that carry claims, in record attribute names. name is
// claimed only when a statement after the column's creation changed it (a
// rename); exists is the column itself.
var columnClaimFacts = []string{
	"additional_references", "collation", "default", "exists", "generated", "identity",
	"name", "nullable", "primary_key", "references", "type", "type_resolved", "unique",
}

// Index facts that carry claims.
var indexClaimFacts = []string{"elements", "exists", "include", "name", "unique", "using", "where"}

// claimRows are the rows a fact's claim prefers, in order, among the rows
// of the statement that set it: for a new column or index (created) and
// for one that existed before the statement (changed). A fact whose
// statement has none of them takes the statement's own row.
var claimRows = map[string]struct{ created, changed []string }{
	"exists":                {created: []string{"M1", "M5a", "M4", "M5f-u"}},
	"name":                  {changed: []string{"M5h", "M8", "M5i", "M5f-u"}},
	"type":                  {created: []string{"M2"}, changed: []string{"M5c"}},
	"type_resolved":         {created: []string{"M2"}, changed: []string{"M5c"}},
	"collation":             {created: []string{"M2"}, changed: []string{"M5c"}},
	"nullable":              {created: []string{"M1", "M5a"}, changed: []string{"M5d", "M5f", "M5f-u"}},
	"default":               {created: []string{"M1", "M5a"}, changed: []string{"M5e"}},
	"identity":              {created: []string{"M1", "M5a"}},
	"generated":             {created: []string{"M1", "M5a"}},
	"primary_key":           {created: []string{"M1", "M5a", "M5f"}, changed: []string{"M5f", "M5f-u"}},
	"unique":                {created: []string{"M1", "M5a", "M4", "M5f"}, changed: []string{"M4", "M5f", "M5f-u"}},
	"references":            {created: []string{"M3"}, changed: []string{"M3"}},
	"additional_references": {created: []string{"M3"}, changed: []string{"M3"}},
	"using":                 {created: []string{"M4", "M5f-u"}},
	"elements":              {created: []string{"M4", "M5f-u"}},
	"include":               {created: []string{"M4", "M5f-u"}},
	"where":                 {created: []string{"M4", "M5f-u"}},
}

// claimRow picks the row of a claim from the statement's rows (the first
// is the statement's own row).
func claimRow(fact string, created bool, rows []string) string {
	prefs := claimRows[fact].changed
	if created {
		prefs = claimRows[fact].created
	}
	for _, p := range prefs {
		if slices.Contains(rows, p) {
			return p
		}
	}
	if len(rows) == 0 {
		return "D"
	}
	return rows[0]
}

// inPrimaryKey reports whether c is one of t's primary-key columns.
func inPrimaryKey(t *table, c *column) bool {
	return t.primaryKey != nil && slices.Contains(t.primaryKey.cols, c)
}

// uniqueAlone reports whether c alone is a primary key, a UNIQUE
// constraint or the key of a unique index without predicate or
// expression (G3; emit states unique from it).
func uniqueAlone(t *table, c *column) bool {
	covered := t.primaryKey != nil && len(t.primaryKey.cols) == 1 && t.primaryKey.cols[0] == c
	for _, u := range t.uniques {
		if len(u.cols) == 1 && u.cols[0] == c {
			covered = true
		}
	}
	for _, ix := range t.indexes {
		if ix.unique && !ix.uncertain && ix.where == nil && len(ix.columns) == 1 && ix.columns[0] == c {
			covered = true
		}
	}
	return covered
}

// columnForeignKeys are t's foreign keys that have c as a local column, in
// t's order (emit states references from the first and
// additional_references from the others).
func columnForeignKeys(t *table, c *column) []*foreignKey {
	var out []*foreignKey
	for _, fk := range t.foreignKeys {
		if slices.Contains(fk.local, c) {
			out = append(out, fk)
		}
	}
	return out
}

func optText(p *string) string {
	if p == nil {
		return "\x00"
	}
	return *p
}

// fkPrint fingerprints a foreign key by identity: its object, target table
// and columns, name and actions.
func fkPrint(fk *foreignKey) string {
	return fmt.Sprintf("%p|%p|%v|%s|%s|%s", fk, fk.table, fk.columns, optText(fk.name), optText(fk.onDelete), optText(fk.onUpdate))
}

// columnPrint fingerprints the facts of c that carry claims.
func columnPrint(t *table, c *column) map[string]string {
	fks := columnForeignKeys(t, c)
	refs, more := "", []string{}
	for i, fk := range fks {
		if i == 0 {
			refs = fkPrint(fk)
		} else {
			more = append(more, fkPrint(fk))
		}
	}
	return map[string]string{
		"exists": "1", "name": c.name, "type": c.typ, "type_resolved": c.typeResolved,
		"nullable": fmt.Sprint(c.nullable), "default": optText(c.def), "collation": optText(c.collation),
		"identity": optText(c.identity), "generated": optText(c.generated),
		"primary_key": fmt.Sprint(inPrimaryKey(t, c)), "unique": fmt.Sprint(uniqueAlone(t, c)),
		"references": refs, "additional_references": strings.Join(more, ";"),
	}
}

// indexPrint fingerprints the facts of ix that carry claims.
func indexPrint(ix *index) map[string]string {
	elements := []string{}
	for _, el := range ix.elements {
		elements = append(elements, el.text)
	}
	return map[string]string{
		"exists": "1", "name": optText(ix.name), "unique": fmt.Sprint(ix.unique), "using": optText(ix.using),
		"elements": strings.Join(elements, "\x00"), "include": strings.Join(ix.include, "\x00"), "where": optText(ix.where),
	}
}

// factPrints are the fingerprints of every held column and index.
type factPrints struct {
	columns map[*column]map[string]string
	indexes map[*index]map[string]string
}

func (s *state) factPrints() factPrints {
	out := factPrints{columns: map[*column]map[string]string{}, indexes: map[*index]map[string]string{}}
	for _, t := range s.tables {
		for _, c := range t.columns {
			out.columns[c] = columnPrint(t, c)
		}
		for _, ix := range t.indexes {
			out.indexes[ix] = indexPrint(ix)
		}
	}
	return out
}

// recordClaims gives every fact the statement changed the statement as its
// claim. before are the fingerprints taken before the statement and rows
// the statement's rows (its own row first).
func (s *state) recordClaims(before factPrints, rows []string) {
	set := func(claims map[string]claim, old, now map[string]string, existed, isColumn bool) map[string]claim {
		for fact, v := range now {
			if existed && old[fact] == v || !existed && isColumn && fact == "name" {
				continue
			}
			if claims == nil {
				claims = map[string]claim{}
			}
			claims[fact] = claim{row: claimRow(fact, !existed, rows), m: s.m, at: s.at}
		}
		return claims
	}
	for _, t := range s.tables {
		for _, c := range t.columns {
			old, existed := before.columns[c]
			if !existed && c.claims != nil {
				// A copy the state restored (ROLLBACK TO, a refused
				// statement undone): its facts and claims are those of
				// the copied column.
				continue
			}
			c.claims = set(c.claims, old, columnPrint(t, c), existed, true)
		}
		for _, ix := range t.indexes {
			old, existed := before.indexes[ix]
			if !existed && ix.claims != nil {
				continue
			}
			ix.claims = set(ix.claims, old, indexPrint(ix), existed, false)
		}
	}
}

// claimList is the claims attribute of a record: one entry per fact of
// facts whose attribute value in attrs is not null, sorted by fact. The
// fact exists is always listed. A fact without a recorded claim is left
// out (TestClaimProvenance requires one for every known fact).
func claimList(claims map[string]claim, facts []string, attrs map[string]any) []any {
	out := []any{}
	for _, fact := range facts {
		if fact != "exists" && attrs[fact] == nil {
			continue
		}
		cl, ok := claims[fact]
		if !ok || cl.m == nil {
			continue
		}
		out = append(out, map[string]any{"fact": fact, "row": cl.row, "migration": cl.m.identity, "span": cl.m.span(cl.at)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].(map[string]any)["fact"].(string) < out[j].(map[string]any)["fact"].(string)
	})
	return out
}

// citedContractID finds the contract id a reason text cites
// (precondition's " [contract <id>]").
var citedContractID = regexp.MustCompile(`\[contract ([^\]]+)\]`)

// reasonRow is the row of a partial reason added while the state applies
// a statement: the precondition or row id the text cites when it is one
// of the contract's, else the statement's own row.
func (s *state) reasonRow(text string) string {
	if m := citedContractID.FindStringSubmatch(text); m != nil && contractID(m[1]) {
		return m[1]
	}
	return s.row
}

// contractID reports a row or precondition id of the contract.
func contractID(id string) bool {
	for _, r := range contractRows {
		if r.id == id {
			return true
		}
	}
	for _, p := range contractPreconditions {
		if p.id == id {
			return true
		}
	}
	return false
}

// opaqueSite is one statement counted in a migration's opaque count.
type opaqueSite struct {
	at   byteSpan
	row  string
	kind string
}

// nonEmpty is nil for the empty string.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
