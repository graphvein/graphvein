// The web UI's rows of the workspace claim contract (rows WS-41..WS-52,
// WS-55, WS-56, WS-58 and the default WS-69 of docs/contract/claims/workspace.md, "Web UI"),
// kept 1:1 with the table by rows.test.ts: every row the table lists is
// here, every entry here is a row of the table, and each names the test
// file and test that check it.

export interface WorkspaceRow {
  row: string;
  /** The test file (in this folder) and the test name that check it. */
  file: string;
  test: string;
}

export const WORKSPACE_UI_ROWS: readonly WorkspaceRow[] = [
  { row: "WS-41", file: "workspace.test.tsx", test: "draws a hollow dot until a check finished and never green from anything else" },
  { row: "WS-42", file: "workspace.test.tsx", test: "strikes through a folder that is gone and never opens it (WS-42)" },
  { row: "WS-43", file: "workspace.test.tsx", test: "keeps other projects collapsed with their member count, and shows no link state for them (WS-43)" },
  { row: "WS-44", file: "workspace.test.tsx", test: "shows a running job's phase and run-counted progress on its row, never an estimate (WS-44)" },
  { row: "WS-45", file: "workspace.test.tsx", test: "checks the typed path, shows what would happen, and reads nothing before Index" },
  { row: "WS-46", file: "workspace.test.tsx", test: "words every refusal under a plain heading, an unknown code with the server's code" },
  { row: "WS-47", file: "WorkspaceRoot.test.tsx", test: "says a link names an entry this server does not have, shows the current one and drops the selection (WS-47)" },
  { row: "WS-48", file: "workspace.test.tsx", test: "shows skeletons and the opening line, nothing of another index, while an index opens" },
  { row: "WS-49", file: "WorkspaceRoot.test.tsx", test: "switches without a reload, remembers the view and restores it on switching back (WS-49)" },
  { row: "WS-50", file: "workspace.test.tsx", test: "copies the entry, member and the section's whole query, and says what it opens" },
  { row: "WS-51", file: "workspace.test.tsx", test: "greys every section, claims no version and offers Add a folder with the command" },
  { row: "WS-52", file: "WorkspaceRoot.test.tsx", test: "asks for the run at once when the workspace list sees the viewed index's job change" },
  { row: "WS-55", file: "workspace.test.tsx", test: "offers Index it on an entry whose check found no index, and shows a refused run on its row" },
  { row: "WS-56", file: "workspace.test.tsx", test: "says the folder was added and not indexed when the run did not start, with the reason and a retry" },
  { row: "WS-58", file: "workspace.test.tsx", test: "says which entry holds a name shared in one store and where to rename one, with no freshness or Index it (WS-58)" },
  { row: "WS-69", file: "workspace.test.tsx", test: "keeps a state the UI does not know as the server's word" },
];
