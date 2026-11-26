package tableaccess

// AssumptionText is one environment assumption of the code-to-table
// contract as the schema API's catalogue serves it (GET
// /api/v0/schema/assumptions): Text is the entry of
// docs/extract/table-access.md "Assumptions" as written there
// (TestAssumptionTexts checks it against the document); Title is a short
// name for the catalogue, which the document does not give.
type AssumptionText struct {
	ID, Title, Text string
}

// AssumptionTexts lists Assumptions in their order, with their texts, so
// the impact panel and the Assumptions tab can show what an inferred edge's
// `assumes` id means instead of a bare code (T019b part 2).
var AssumptionTexts = []AssumptionText{
	{ID: "TA-V1", Title: "SQL runs against the schema source", Text: "The SQL text a modelled carrier runs, runs against the database the resolved schema source describes. sqlx `query!` checks this at compile time; for the other carriers it is an assumption, so their SQL edges are `inferred`."},
	{ID: "TA-V2", Title: "Model names its table", Text: "An ORM model class, delegate or table const names the table its schema source emits; runtime table renames are not followed."},
	{ID: "TA-V3a", Title: "Prisma receiver", Text: "A Prisma client receiver is what its name says."},
	{ID: "TA-V3b", Title: "Drizzle receiver", Text: "A Drizzle database or transaction receiver is what its name says."},
	{ID: "TA-V3c", Title: "SQLAlchemy receiver", Text: "A SQLAlchemy session or connection receiver is what its name says."},
	{ID: "TA-V4", Title: "Postgres SQL semantics", Text: "Postgres semantics for SQL text: search_path `public`, identifiers folded to lower case unless quoted."},
}
