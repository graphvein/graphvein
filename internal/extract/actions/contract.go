package actions

// Row is one row of the server action claim contract
// (docs/extract/server-actions.md; docs/contract/claims/nextjs-supabase.md,
// "Claim contract: server actions"). Every branch of the linker that
// decides a server action fact, or refuses one, reports its row through
// tag, so TestServerActionsContract can check that the code, the docs
// table and the fixture stay 1:1.
type Row struct {
	ID string
}

// Contract lists the rows in table order; D is the single default branch
// and comes last.
var Contract = []Row{
	{ID: "SA-M1"},
	{ID: "SA-I1"},
	{ID: "SA-C1"},
	{ID: "SA-C2"},
	{ID: "SA-C3"},
	{ID: "D"},
}

var tagHook func(row string)

// SetTagHook installs (or, with nil, removes) the hook every server action
// branch reports its row to. Tests only; linking is single-threaded.
func SetTagHook(hook func(row string)) { tagHook = hook }

// tag reports that a branch of row ran.
func tag(row string) {
	if tagHook != nil {
		tagHook(row)
	}
}
