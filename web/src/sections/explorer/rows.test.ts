// Keeps the Explorer's rows (rows.ts) 1:1 with the OX table of the claim
// contract (docs/contract/claims/try-it.md, "Explorer (OX)").
import { describe, expect, it } from "vitest";

import contract from "../../../../docs/contract/claims/try-it.md?raw";

import { EXPLORER_ROWS } from "@/sections/explorer/rows";

/** tableRows reads the OX rows of the contract: row id and Built column. */
function tableRows(md: string): { row: string; built: string }[] {
  const start = md.indexOf("## Explorer (OX)");
  return md
    .slice(start)
    .split("\n")
    .filter((l) => l.startsWith("| OX-"))
    .map((l) => {
      const cells = l.split("|").map((c) => c.trim());
      return { row: cells[1] ?? "", built: cells[2] ?? "" };
    });
}

describe("Explorer rows (T029, OX)", () => {
  it("lists exactly the table's rows, in order, with the same status", () => {
    const table = tableRows(contract);
    expect(table.length).toBeGreaterThan(0);
    expect(EXPLORER_ROWS.map((r) => r.row)).toEqual(table.map((r) => r.row));
    for (const [i, r] of EXPLORER_ROWS.entries()) {
      expect(r.status === "shown" ? "yes" : "pending", r.row).toBe(table[i]?.built);
      expect(r.where, r.row).not.toBe("");
    }
  });

  it("keeps row ids inside the reserved range", () => {
    for (const r of EXPLORER_ROWS) {
      if (r.row === "OX-D") continue;
      const n = Number(r.row.slice(3));
      expect(n >= 1 && n <= 49, r.row).toBe(true);
    }
    expect(new Set(EXPLORER_ROWS.map((r) => r.row)).size).toBe(EXPLORER_ROWS.length);
  });
});
