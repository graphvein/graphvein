// T018g2 G2B4 in the schema view: a Drizzle table's relations() entries
// (attribute orm_relations, row DR-R1) are listed as ORM relations, never
// as foreign-key chips or links, like Prisma's emulated relations; a
// Drizzle table's indexes are declared in its table definition. G2B5: a
// drizzle-kit migrations folder is an unread tool ("Drizzle migrations
// found, not read yet"), never "no migrations".
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { SchemaSummary, UnreadTool } from "@/api/schema";
import { NoneFound, noneTitle } from "@/sections/schema/EmptyStates";
import { schemaEntryTag } from "@/sections/schema/entry";
import { SourceCards, toolLabel, unreadTitle } from "@/sections/schema/SourceCards";
import { declaredIn, EmulatedRelations, ModelIndexes, OrmRelations, relationTexts } from "@/sections/schema/TableDetail";

const drizzleKit: UnreadTool = { tool: "drizzle-kit", path: "apps/api/drizzle", marker: "apps/api/drizzle/meta/_journal.json", files: 2 };

describe("Drizzle migrations as an unread tool (G2B5)", () => {
  it("names Drizzle in the card title and label", () => {
    expect(toolLabel("drizzle-kit")).toBe("Drizzle");
    expect(unreadTitle(drizzleKit)).toBe("Drizzle migrations found, not read yet");
  });

  it("the source card shows the folder, its journal and its files", () => {
    render(<SourceCards cards={[{ kind: "unread", tool: drizzleKit }]} selected={null as never} onSelect={() => undefined} />);
    const card = screen.getByTestId("source-card");
    expect(card.getAttribute("data-tool")).toBe("drizzle-kit");
    expect(card.textContent).toContain("Drizzle migrations found, not read yet");
    expect(card.textContent).toContain("apps/api/drizzle");
    expect(card.textContent).toContain("2 files");
  });

  it("the found-none page and the entry tag never say no migrations", () => {
    const summary = { found: "none", repository: "kaneo", index: null, unread_tools: [drizzleKit], sources_read: ["migrations", "models"] } as unknown as SchemaSummary;
    expect(noneTitle(summary)).toBe("Graphvein found Drizzle migrations but does not read them yet");
    expect(schemaEntryTag(summary)).toEqual({ text: "not read yet", tone: "muted", title: "Drizzle migrations found that Graphvein does not read yet" });
    render(<NoneFound summary={summary} />);
    expect(screen.getByTestId("none-unread-tools").textContent).toBe("Drizzle migrations folder apps/api/drizzle (2 files): not read yet.");
  });
});

describe("Drizzle ORM relations (DR-R1)", () => {
  it("lists each relation as text under its own heading, with no link or chip", () => {
    render(
      <OrmRelations
        value={[
          { name: "author", kind: "one", source: "table:db/schema.ts#posts", target: "table:db/schema.ts#users", table: "users", columns: ["author_id"], references: ["id"] },
          { name: "comments", kind: "many", source: "table:db/schema.ts#posts", target: null, table: null, columns: null, references: null },
          "odd",
        ]}
      />,
    );
    const section = screen.getByTestId("orm-relations");
    // The text states nothing about the database's foreign keys:
    // a relations() entry may or may not have one beside it.
    expect(section.textContent).toContain("Used by Drizzle's relational queries only. Database foreign keys, if any, are listed under Foreign keys.");
    expect(section.textContent).not.toMatch(/no foreign key|not database foreign keys|not the database/i);
    const items = within(section).getAllByTestId("emulated-relation");
    expect(items.map((i) => i.textContent)).toEqual(["author (one) → users (author_id → id)", "comments (many) → unknown table", "\"odd\""]);
    expect(section.querySelectorAll("a, button")).toHaveLength(0);
  });

  it("keeps Prisma's emulated-relation text, which is true there", () => {
    render(<EmulatedRelations value={[{ columns: ["userId"], field: "user", references: ["id"], table: "User" }]} />);
    expect(screen.getByTestId("emulated-relations").textContent).toContain(relationTexts.prisma.note);
    expect(relationTexts.drizzle.note).not.toBe(relationTexts.prisma.note);
  });

  it("renders nothing without relations", () => {
    expect(render(<OrmRelations value={undefined} />).container.textContent).toBe("");
    expect(render(<OrmRelations value={[]} />).container.textContent).toBe("");
  });
});

describe("where a Drizzle table is declared", () => {
  it("names the table definition", () => {
    expect(declaredIn("drizzle")).toBe("the table definition");
    const { container } = render(<ModelIndexes attrs={{ indexes: [], unique_constraints: [], check_constraints: [] }} tool="drizzle" />);
    expect(container.textContent).toContain("none declared in the table definition");
    expect(container.textContent).not.toContain("class");
  });
});
