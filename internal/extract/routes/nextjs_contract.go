package routes

// NextRow is one row of the Next.js route claim contract
// (docs/extract/routes.md, "Next.js App Router and Pages Router API";
// docs/contract/claims/nextjs-supabase.md). Every branch of the linker
// and of the client matcher that decides a Next.js fact reports the row
// it implements through NextTag, so TestNextRoutesContract can check that
// the code, the docs table and the fixture stay 1:1.
type NextRow struct {
	ID          string
	Assumptions []string
}

// NextAssumptions are the assumptions the rows may name.
var NextAssumptions = []string{"NX-V1", "NX-V2", "NX-V3"}

// NextContract lists the rows in table order; D is the single default
// branch and comes last.
var NextContract = []NextRow{
	{ID: "NX-P1"},
	{ID: "NX-P2", Assumptions: []string{"NX-V2"}},
	{ID: "NX-P3", Assumptions: []string{"NX-V2"}},
	{ID: "NX-R1"},
	{ID: "NX-R2"},
	{ID: "NX-R3"},
	{ID: "NX-R4"},
	{ID: "NX-L1", Assumptions: []string{"NX-V1"}},
	{ID: "NX-L2", Assumptions: []string{"NX-V1"}},
	{ID: "NX-L3", Assumptions: []string{"NX-V1", "NX-V3"}},
	{ID: "D"},
}

var nextTagHook func(row string)

// SetNextTagHook installs (or, with nil, removes) the hook every Next.js
// branch reports its row to. Tests only; linking is single-threaded.
func SetNextTagHook(hook func(row string)) { nextTagHook = hook }

// NextTag reports that a branch of row ran. The client matcher calls it
// too, so the rows NX-L1..NX-L3 stay in this one table.
func NextTag(row string) {
	if nextTagHook != nil {
		nextTagHook(row)
	}
}
