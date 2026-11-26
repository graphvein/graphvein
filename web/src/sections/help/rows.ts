// The Help page's claim rows (HP-1..HP-19 of docs/contract/claims/help.md),
// kept 1:1 with the table by rows.test.ts. Each row names the test that
// checks it.

export interface HelpRow {
  row: string;
  /** The test file and name that check the row. */
  where: string;
}

export const HELP_ROWS: readonly HelpRow[] = [
  { row: "HP-1", where: "examples.test.ts: picks the first full HTTP route as the Fact, else the first endpoint" },
  { row: "HP-2", where: "examples.test.ts: picks the first inferred caller of the first called routes, at most three" },
  { row: "HP-3", where: "examples.test.ts: counts dynamic HTTP calls as the Unknown, only when positive" },
  { row: "HP-4", where: "HelpSection.test.tsx: shows only the fixed examples without an index, with an empty index or after a failed request" },
  { row: "HP-5", where: "HelpSection.test.tsx: requests the examples again for a newer index run" },
  { row: "HP-D", where: "examples.test.ts: ignores callers without an inferred edge and other transports' counts" },
];
