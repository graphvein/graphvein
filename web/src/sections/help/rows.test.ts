// Keeps the Help page's rows (rows.ts) 1:1 with the HP table of the claim
// contract (docs/contract/claims/help.md).
import { describe, expect, it } from "vitest";

import contract from "../../../../docs/contract/claims/help.md?raw";

import { HELP_ROWS } from "@/sections/help/rows";

describe("Help rows (T045, HP)", () => {
  it("lists exactly the table's rows, in order, each with a test", () => {
    const table = contract
      .split("\n")
      .filter((l) => l.startsWith("| HP-"))
      .map((l) => l.split("|")[1]?.trim() ?? "");
    expect(table.length).toBeGreaterThan(0);
    expect(HELP_ROWS.map((r) => r.row)).toEqual(table);
    for (const r of HELP_ROWS) expect(r.where, r.row).toMatch(/\.test\.tsx?: /);
  });

  it("keeps row ids inside the reserved range", () => {
    for (const r of HELP_ROWS) {
      if (r.row === "HP-D") continue;
      const n = Number(r.row.slice(3));
      expect(n >= 1 && n <= 19, r.row).toBe(true);
    }
  });
});
