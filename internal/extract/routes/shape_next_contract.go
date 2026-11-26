package routes

// NextShapeRow is one row of the Next.js App Router route handler and
// server action shape claim contract
// (docs/contract/claims/route-shape-nextjs.md).
type NextShapeRow struct {
	ID    string
	Title string
}

// NextShapeRows lists the rows in document order; TestNextShapeContract
// keeps it equal to the document's table and checks that the fixture
// reaches every row.
var NextShapeRows = []NextShapeRow{
	{"SH-N1", "App Router path parameters from the route template"},
	{"SH-N2", "a route template without parameters"},
	{"SH-N3", "query names read"},
	{"SH-N4", "a request value read as a whole or written"},
	{"SH-N5", "header names read on the request"},
	{"SH-N6", "header and cookie names read through next/headers"},
	{"SH-N7", "cookie names read on the request"},
	{"SH-N8", "JSON body fields read"},
	{"SH-N9", "form data fields read"},
	{"SH-N10", "a body read as text, bytes or a stream"},
	{"SH-N11", "a body passed on changed"},
	{"SH-N12", "zod parse of the JSON body"},
	{"SH-N13", "zod safeParse with the result tested"},
	{"SH-N14", "a validation that is not tested or not enforced"},
	{"SH-N15", "zod schema forms"},
	{"SH-N16", "TypeScript types of the body"},
	{"SH-N17", "Response.json and NextResponse.json"},
	{"SH-N18", "new Response and new NextResponse"},
	{"SH-N19", "redirect responses"},
	{"SH-N20", "notFound, redirect and permanentRedirect from next/navigation"},
	{"SH-N21", "the declared return type of a proven callee in a response body"},
	{"SH-N22", "a status or content type not read"},
	{"SH-N23", "thrown errors and returns not read"},
	{"SH-N24", "middleware, config rules and static route config before the handler"},
	{"SH-N25", "call following through proven targets"},
	{"SH-N26", "a handler that is not a proven function"},
	{"SH-N27", "server action arguments"},
	{"SH-N28", "server action results"},
	{"SH-N29", "a server action that is not a proven function"},
	{"SH-N30", "a next.config headers rule that sets no content header"},
	{"D", "anything no row covers"},
}
