// Keeps the workspace UI's rows (rows.ts) 1:1 with the "Web UI" table of
// the claim contract (docs/contract/claims/workspace.md), and checks that
// every row names a test that exists.
import { describe, expect, it } from "vitest";

import contract from "../../../../docs/contract/claims/workspace.md?raw";

import { WORKSPACE_UI_ROWS } from "@/shell/workspace/rows";

const TESTS = import.meta.glob<string>("./*.test.tsx", { query: "?raw", import: "default", eager: true });

function tableRows(md: string): string[] {
  const start = md.indexOf("## Web UI");
  return md
    .slice(start)
    .split("\n")
    .filter((l) => l.startsWith("| WS-"))
    .map((l) => l.split("|")[1]?.trim() ?? "");
}

describe("workspace UI rows (T038, WS-41..WS-69)", () => {
  it("lists exactly the table's rows, in order", () => {
    const table = tableRows(contract);
    expect(table.length).toBeGreaterThan(0);
    expect(WORKSPACE_UI_ROWS.map((r) => r.row)).toEqual(table);
  });

  it("keeps row ids inside the reserved range", () => {
    for (const r of WORKSPACE_UI_ROWS) {
      const n = Number(r.row.slice(3));
      expect(n >= 41 && n <= 69, r.row).toBe(true);
    }
    expect(new Set(WORKSPACE_UI_ROWS.map((r) => r.row)).size).toBe(WORKSPACE_UI_ROWS.length);
  });

  it("names an existing test for every row", () => {
    for (const r of WORKSPACE_UI_ROWS) {
      const source = TESTS[`./${r.file}`];
      expect(source, `${r.row}: ${r.file}`).toBeDefined();
      expect(source?.includes(`it(${JSON.stringify(r.test)}`) || source?.includes(`it(\`${r.test}\``), `${r.row}: ${r.test}`).toBe(true);
    }
  });
});
