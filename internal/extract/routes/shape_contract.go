package routes

// ExpressShapeRow is one row of the Express and Next.js Pages Router API
// shape claim table (docs/contract/claims/route-shape-express.md).
type ExpressShapeRow struct {
	ID, Title string
}

// ExpressShapeRows lists the SH-E rows in document order; a test keeps it
// equal to the document's table and checks the fixture reaches each row.
var ExpressShapeRows = []ExpressShapeRow{
	{"SH-E1", "Express path parameters from the route template"},
	{"SH-E2", "a template without parameters"},
	{"SH-E3", "Pages Router API path parameters from the file name"},
	{"SH-E4", "query names read"},
	{"SH-E5", "Pages: a query name that is a dynamic segment"},
	{"SH-E6", "a request object read as a whole"},
	{"SH-E7", "header names read"},
	{"SH-E8", "cookie names read"},
	{"SH-E9", "cookies without a proven cookie parser"},
	{"SH-E10", "body fields read"},
	{"SH-E11", "a body passed on changed"},
	{"SH-E12", "zod parse"},
	{"SH-E13", "zod safeParse with the result tested"},
	{"SH-E14", "Joi validate with the error tested, validateAsync"},
	{"SH-E15", "celebrate on the route"},
	{"SH-E16", "express-validator chains on the route"},
	{"SH-E17", "Request and RequestHandler type arguments"},
	{"SH-E18", "type assertions and typed callee parameters"},
	{"SH-E19", "a validation that is not tested or not enforced"},
	{"SH-E20", "zod schema forms"},
	{"SH-E21", "Joi schema forms"},
	{"SH-E22", "TypeScript types as models"},
	{"SH-E23", "a send with a literal status"},
	{"SH-E24", "sendStatus"},
	{"SH-E25", "a send without a status"},
	{"SH-E26", "redirect"},
	{"SH-E27", "response bodies written in the call"},
	{"SH-E28", "the declared return type of a proven callee"},
	{"SH-E29", "thrown errors and next"},
	{"SH-E30", "the response passed to a proven function"},
	{"SH-E31", "a status not read"},
	{"SH-E32", "a mounted OpenAPI validator"},
	{"SH-E33", "body parsers"},
	{"SH-E34", "Pages Router API routes serve every method"},
	{"SH-E35", "call following through proven targets"},
	{"SH-E36", "a handler that is not a proven function"},
	{"D", "anything no row covers"},
}
