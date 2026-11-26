package supabasetypes

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// Row is one row of the generated-types claim contract (rows SB-G of
// docs/contract/claims/nextjs-supabase.md, copied into
// docs/extract/schema-supabase-types.md "Claim contract");
// TestSupabaseTypesContract keeps the code, the task table and the docs
// copy 1:1.
type Row struct {
	ID string
	// Branch is the function holding the row's claim branch ("" for D).
	Branch string
	// Assumptions are the ids in the row's assumptions column.
	Assumptions []string
	// Forms are the forms of the row the fixture must reach (each tagged
	// "<row>/<form>").
	Forms []string
	// Summary is the claim as the catalogue serves it.
	Summary string
}

// Contract is the claim contract in the docs' order.
var Contract = []Row{
	{ID: "SB-G1", Branch: "buildTable", Assumptions: []string{"SB-V6"}, Forms: Forms,
		Summary: "a schema source `tool: supabase_types` labelled \"generated types\" (mapped): tables from `Tables`, columns from `Insert` with the `Row` type as written and nullability from `| null`; relationships listed as written, not resolved; views not recorded"},
	{ID: "SB-G2", Branch: "compare",
		Summary: "the package's Supabase migration set is the schema source; the generated types are compared with it and every difference is a `supabase_types_disagreement` diagnostic"},
	{ID: "D", Summary: "nothing"},
}

// Forms are SB-G1's forms and its refusal.
var Forms = []string{
	"export type Database", "export interface Database", "__InternalSupabase", "[_ in never]: never",
	"| null", "unknown", "computed field", "Relationships", "Views", codeUnread,
}

// Assumption is SB-V6, the assumption of every fact read from generated
// types (and of every SB edge into such a table).
var Assumption = models.Assumption{ID: "SB-V6", Title: "Generated types are current",
	Text: "A generated types file (SB-G1) was generated from the database the client talks to and is current; it is the package's schema source only when the package has no Supabase migration set."}

// Catalogue returns the contract's catalogue for the schema API.
func Catalogue() models.Catalogue {
	c := models.Catalogue{Tool: Tool, Assumptions: []models.Assumption{Assumption}}
	for _, r := range Contract {
		branch := r.Branch
		if branch == "" {
			branch = "none"
		}
		c.Rows = append(c.Rows, models.ContractEntry{ID: r.ID, Branch: branch, Summary: r.Summary, Assumptions: append([]string{}, slices.Clone(r.Assumptions)...)})
	}
	return c
}

func init() {
	models.Register(Catalogue())
}
