// The Explorer's rows of the claim contract (rows OX-1..OX-49 of
// docs/contract/claims/try-it.md, "Explorer (OX)"), kept 1:1 with the
// table by rows.test.ts: every row the table lists is here with the same
// status, and every entry here is a row of the table. "shown" rows name
// the test that checks them; "pending" rows are not built yet and say who
// builds them.

export interface ExplorerRow {
  row: string;
  status: "shown" | "pending";
  /** The test file and name that check a shown row; who builds a pending
   * one. */
  where: string;
}

export const EXPLORER_ROWS: readonly ExplorerRow[] = [
  { row: "OX-1", status: "shown", where: "ExplorerSection.test.tsx: groups operations by router file with the document's counts" },
  { row: "OX-2", status: "shown", where: "model.test.ts: groups by the documenting spec's first tag, others under No tag" },
  { row: "OX-3", status: "shown", where: "model.test.ts: names path parameters from the template with their type not read" },
  { row: "OX-4", status: "shown", where: "ExplorerSection.test.tsx: says not read for a route without a shape and never shows an empty table" },
  { row: "OX-5", status: "shown", where: "model.test.ts: says an empty auth list is none read" },
  { row: "OX-6", status: "shown", where: "model.test.ts: marks shadowing, equivalent registrations and conditional routes" },
  { row: "OX-7", status: "shown", where: "ExplorerSection.test.tsx: lists unresolved routes with the unknown part and edits a value through preview and apply; says why a document has no operation from its own counts; model.test.ts: builds the empty-document sentence from the document's counts and reasons" },
  { row: "OX-8", status: "pending", where: "the document carries no precision label; the API section shows it" },
  { row: "OX-9", status: "pending", where: "a later Explorer unit (the document lists imported specs and documented_by only)" },
  { row: "OX-10", status: "pending", where: "a later Explorer unit" },
  { row: "OX-11", status: "shown", where: "ExplorerSection.test.tsx: shows the Spec tab with the document text" },
  { row: "OX-12", status: "shown", where: "ExplorerSection.test.tsx: reads the document again for a newer index run" },
  { row: "OX-13", status: "shown", where: "ExplorerSection.test.tsx: shows proven none with evidence and items with their provenance chip (body presence); model.test.ts: says a request body's presence not read as unknown, never optional; says a type's reason once" },
  { row: "OX-14", status: "shown", where: "model.test.ts: says none only for a stored provably_none state, with its evidence" },
  { row: "OX-15", status: "shown", where: "ExplorerSection.test.tsx: shows values set by you with their use and warnings; sets one prefix for every route under an unresolved mount hop with a preview" },
  { row: "OX-16", status: "shown", where: "ExplorerSection.test.tsx: filters operations and says how many match" },
  { row: "OX-17", status: "shown", where: "ExplorerSection.test.tsx: project mode names each member's operation count" },
  { row: "OX-18", status: "shown", where: "ExplorerSection.test.tsx: says not read for a route without a shape and never shows an empty table (callers link, Try it out); TryIt.test.tsx: moves focus into the panel on Try it out and back to Send after Cancel" },
  { row: "OX-20", status: "shown", where: "TryIt.test.tsx: warns once when switching to production, then marks every operation (bar, row labels); tryit.test.ts: reads the environments listing without any auth value; TryIt.test.tsx: moves through the environment listbox and the label radiogroup with the arrow keys" },
  { row: "OX-21", status: "shown", where: "TryIt.test.tsx: asks before POST with the exact request and reasons, and forwards don't ask again; shows the full body SHA-256 in the confirmation; closes an open confirmation when the environment changes, and offers the right action after a refused send; moves focus into the panel on Try it out and back to Send after Cancel" },
  { row: "OX-22", status: "shown", where: "TryIt.test.tsx: sends GET on a local environment directly and shows redacted headers and re-indented JSON; shows a JSON body exactly as received: re-indented token for token, never re-serialised; jsonIndent.test.ts; tryit/model.test.ts: never re-serialises" },
  { row: "OX-23", status: "shown", where: "TryIt.test.tsx: says why it cannot try without an environment, and adds one from a suggestion through preview and one confirmation" },
  { row: "OX-24", status: "shown", where: "TryIt.test.tsx: says why it cannot try without an environment, and adds one from a suggestion through preview and one confirmation (suggestions)" },
  { row: "OX-25", status: "shown", where: "TryIt.test.tsx: warns once when switching to production, then marks every operation; acknowledges a project production environment once for every member it covers; asks again in project mode for a member the warning did not list, a changed base URL or a changed manifest" },
  { row: "OX-26", status: "shown", where: "TryIt.test.tsx: says why it cannot try without an environment, and adds one from a suggestion through preview and one confirmation; moves through the environment listbox and the label radiogroup with the arrow keys" },
  { row: "OX-27", status: "shown", where: "TryIt.test.tsx: offers send without auth or a value for the session when the auth variable is not set" },
  { row: "OX-28", status: "shown", where: "TryIt.test.tsx: shows an untrusted certificate and pins it through the settings edit; shows a changed certificate with the old and new fingerprints and a warning" },
  { row: "OX-29", status: "shown", where: "TryIt.test.tsx: shows an untrusted certificate and pins it through the settings edit (preview, save, selected again)" },
  { row: "OX-30", status: "shown", where: "tryit/model.test.ts: pre-fills query and header names with their provenance; TryIt.test.tsx: pre-fills a JSON template marked as a template and offers files for a multipart body; sends a free-form path only after its confirmation" },
  { row: "OX-31", status: "shown", where: "TryIt.test.tsx: adds a redirect's origin as an environment and follows it once as a free-form request" },
  { row: "OX-32", status: "shown", where: "TryIt.test.tsx: says which limit cut a response and offers to raise it; offers to raise the request limit when the body is too large" },
  { row: "OX-33", status: "shown", where: "tryit/model.test.ts: copies as curl with the auth variable or a placeholder, never a value; TryIt.test.tsx: copies as curl with the variable, never the value" },
  { row: "OX-34", status: "shown", where: "TryIt.test.tsx: says an http environment's proxy sees the request in clear" },
  { row: "OX-35", status: "shown", where: "model.test.ts: names a security scheme reads (shows the header a scheme reads and never says none, proven beside it)" },
  { row: "OX-40", status: "shown", where: "shapeView.test.ts: keeps every stored state and lists items with their provenance; says not read in every section for a route without a stored shape, never none; RouteShapeBlock.test.tsx: shows each read section with its items and chips, and the sections not read in one line; says why a stored shape could not be read" },
  { row: "OX-41", status: "shown", where: "shapeView.test.ts: shows the header an http scheme reads and never says none, proven beside it; reads a FastAPI security dependency's class when no stored scheme describes it; RouteShapeBlock.test.tsx: shows None, proven with its evidence, and None besides the security scheme where the scheme reads" },
  { row: "OX-42", status: "shown", where: "ExplorerSection.test.tsx: opens the operations of a route an API section link names, then writes op in the hash; says why the document has no operation for a linked route; opens the operation a collapsed equivalent registration is written in, and says so; model.test.ts: links from the API section; RouteShapeBlock.test.tsx: shows the block in an HTTP route's Overview with the Explorer link" },
  { row: "OX-43", status: "shown", where: "RouteShapeBlock.test.tsx: names the member in project mode (the detail and the Explorer read the selected member)" },
  { row: "OX-44", status: "shown", where: "chipLinks.test.tsx: opens the same link as the IDE button for the same file:line; names the full action and is reachable by the keyboard; is hidden without links, for a position without a link, and when links are off; gives the route detail block's chips their link; ExplorerSection.test.tsx: requests the chip links of an opened operation's route once" },
  { row: "OX-D", status: "shown", where: "model.ts: every form no row covers is not shown" },
];
