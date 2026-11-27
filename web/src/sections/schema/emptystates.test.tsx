// T066 (baseline defect 2): the found-none page lists what Graphvein reads
// today, never only SQLModel and sqlx, never Prisma as "not read yet", and
// its footer names what this index looked for from sources_read, never
// "every indexed Cargo manifest" on a repository of another stack.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { UnreadTool } from "@/api/schema";
import { NoneFound, noneTitle, schemaPassesText } from "@/sections/schema/EmptyStates";
import { craftedSummary } from "@/test/schemaFixtures";

describe("found-none page wording (T066)", () => {
  it("lists every source read today and keeps Prisma out of the not-read list", () => {
    render(<NoneFound summary={craftedSummary({ found: "none", repository: "owncast", sources_read: ["migrations", "models"], unread_tools: [] })} />);
    const reads = screen.getByTestId("none-looks-for").textContent;
    for (const s of ["sqlx", "Supabase", "SQLModel", "Prisma schemas", "Drizzle", "Sequelize", "TypeORM", "Diesel", "GORM v2", "Supabase generated types"]) {
      expect(reads).toContain(s);
    }
    const notRead = screen.getByTestId("none-not-read").textContent;
    expect(notRead).not.toMatch(/Prisma,/);
    expect(notRead).toContain("SQLAlchemy declarative classes that are not SQLModel");
    const footer = screen.getByTestId("none-footer").textContent;
    expect(footer).toContain("looked for migration sets and model tables in every indexed file.");
    expect(footer).not.toContain("Cargo");
  });

  it("names model sources only when the index proves the model pass ran", () => {
    render(<NoneFound summary={craftedSummary({ found: "none", sources_read: ["migrations"], unread_tools: [] })} />);
    expect(screen.getByTestId("none-looks-for").textContent).not.toContain("Prisma");
    expect(screen.getByTestId("none-not-read").textContent).toContain("ORM models: this index was built before Graphvein read them");
    expect(screen.getByTestId("none-footer").textContent).toContain("looked for migration sets in every indexed file; it was built before Graphvein read ORM models.");
  });

  it("says what the index looked for from sources_read only", () => {
    const index = craftedSummary({}).index;
    expect(schemaPassesText({ index: null, sources_read: ["migrations", "models"] })).toBe("The index looked for migration sets and model tables in every indexed file.");
    expect(schemaPassesText({ index: null, sources_read: null })).toBe("Which schema sources the index looked for is not known.");
    expect(schemaPassesText({ index: null, sources_read: [] })).toBe("The index was built before Graphvein extracted database schemas.");
    expect(schemaPassesText({ index, sources_read: ["migrations"] })).toMatch(/^The index of .+ looked for migration sets in every indexed file;/);
  });

  it("names each unread tool by what was found", () => {
    const tools: UnreadTool[] = [
      { tool: "gorm-v1", path: "model", marker: null, files: 3 },
      { tool: "diesel-migrations", path: "crates/db/migrations", marker: null, files: 1 },
    ];
    expect(noneTitle({ unread_tools: [tools[0] as UnreadTool], sources_read: ["migrations", "models"] })).toBe("Graphvein found GORM v1 models but does not read them yet");
    expect(noneTitle({ unread_tools: tools, sources_read: ["migrations", "models"] })).toBe("Graphvein found migrations it does not read yet");
    render(<NoneFound summary={craftedSummary({ found: "none", sources_read: ["migrations", "models"], unread_tools: tools })} />);
    const items = Array.from(screen.getByTestId("none-unread-tools").querySelectorAll("li"), (li) => li.textContent);
    expect(items).toEqual(["GORM v1 imports in model (3 files): not read yet.", "Diesel migrations directory crates/db/migrations (1 file): not read yet."]);
  });
});
