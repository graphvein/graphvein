package schema

import (
	"slices"
	"strings"
)

// The catalogue of the migration contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A): the environment
// assumptions with their titles and first sentences (TestCatalogueTexts
// checks them against docs/extract/schema.md "Environment assumptions"),
// and every row and precondition with the assumptions its claim rests on
// (contractAssumptions).

// Assumption is one environment assumption of a contract. FoldedInto names
// the assumption that now covers it (E7 into E8), or is empty while it is
// in force.
type Assumption struct {
	ID, Title, Text, FoldedInto string
}

// environmentAssumptions are E1-E8; E7 is folded into E8 (Revision 2.1).
var environmentAssumptions = []Assumption{
	{"E1", "Objects outside the set", "roles, extensions, schemas, tables, functions and operators the set never creates in any migration exist with a signature that fits their use, as do server files COPY names.", ""},
	{"E2", "Rows", "no existing row breaks a constraint, NOT NULL or cast that a later ALTER TABLE or CREATE INDEX checks, and the rows of a server file COPY reads are valid.", ""},
	{"E3", "Code", "DO, CALL, function and trigger bodies and extension scripts finish without raising.", ""},
	{"E4", "Session", "Postgres's default settings (standard_conforming_strings on, check_function_bodies on) and the history's schema as search_path (\"$user\", public), in a UTF8 database (the built-in collations ucs_basic and pg_c_utf8 of M2.P1 exist only in UTF8 databases, pg_collation.dat collencoding 6).", ""},
	{"E5", "Built-in schemas", "the database has the schemas initdb creates in every database, public (owned by pg_database_owner) and information_schema, besides pg_catalog and pg_toast; the pass already relies on public for unqualified names and sqlx's bookkeeping table.", ""},
	{"E6", "Privileges short of superuser", "the migrating role is not a superuser.", ""},
	{"E7", "Runtime resources", "E7, runtime resources, belongs to Revision 2.0 and is subsumed by E8 in v1.", "E8"},
	{"E8", "History applies", "the committed migration history applies cleanly, in order, as it did in the project's own environments.", ""},
}

// Assumptions returns the migration contract's environment assumptions in
// id order.
func Assumptions() []Assumption { return slices.Clone(environmentAssumptions) }

// ContractEntry is one row or precondition of a claim contract as the
// catalogue serves it: Branch is the form the row covers (a precondition's
// text for a precondition), Summary what it claims, Assumptions the
// assumption ids its claims rest on (empty: none).
type ContractEntry struct {
	ID, Branch, Summary string
	Assumptions         []string
}

// Contract returns every row and then every precondition of the claim
// contract, in the contract's order, each with contractAssumptions' list.
// A precondition's summary is the claim of its row.
func Contract() []ContractEntry {
	claims := map[string]string{}
	out := []ContractEntry{}
	for _, r := range contractRows {
		claims[r.id] = r.claim
		out = append(out, ContractEntry{ID: r.id, Branch: r.form, Summary: r.claim, Assumptions: rowAssumptions(r.id)})
	}
	for _, p := range contractPreconditions {
		row, _, _ := strings.Cut(p.id, ".")
		out = append(out, ContractEntry{ID: p.id, Branch: p.text, Summary: claims[row], Assumptions: rowAssumptions(p.id)})
	}
	return out
}

// rowAssumptions is contractAssumptions[id], never nil.
func rowAssumptions(id string) []string {
	if list, ok := contractAssumptions[id]; ok {
		return slices.Clone(list)
	}
	return []string{}
}
