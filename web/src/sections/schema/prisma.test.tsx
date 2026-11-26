// T018g1 review fixes in the schema view: a Prisma table's relations under
// relationMode "prisma" are listed as enforced by Prisma, not the database,
// without foreign-key chips (C2), and Prisma Migrate's directory is an
// unread tool ("Prisma migrations found, not read yet"), never "no
// migrations" (C5). A Prisma table's indexes are declared in its model
// block, not a class (G1G).
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { SchemaSummary, UnreadTool } from "@/api/schema";
import { noneTitle } from "@/sections/schema/EmptyStates";
import { schemaEntryTag } from "@/sections/schema/entry";
import { SourceCards, toolLabel, unreadTitle } from "@/sections/schema/SourceCards";
import { declaredIn, EmulatedRelations, ModelIndexes } from "@/sections/schema/TableDetail";

const prisma: UnreadTool = { tool: "prisma-migrate", path: "prisma/migrations", marker: "prisma/migrations/migration_lock.toml", files: 3 };

describe("emulated relations (C2)", () => {
  it("lists each relation as text under its own heading, with no link or chip", () => {
    render(<EmulatedRelations value={[{ columns: ["userId"], field: "user", model: "User", references: ["id"], table: "User" }, "odd"]} />);
    const section = screen.getByTestId("emulated-relations");
    expect(section.textContent).toContain("Relations enforced by Prisma, not the database");
    const items = within(section).getAllByTestId("emulated-relation");
    expect(items.map((i) => i.textContent)).toEqual(["user (userId) → User (id) · model User", "\"odd\""]);
    expect(section.querySelectorAll("a, button")).toHaveLength(0);
  });

  it("renders nothing without relations", () => {
    const { container } = render(<EmulatedRelations value={undefined} />);
    expect(container.textContent).toBe("");
    const empty = render(<EmulatedRelations value={[]} />);
    expect(empty.container.textContent).toBe("");
  });
});

describe("Prisma migrations as an unread tool (C5)", () => {
  it("names Prisma in the card title and label", () => {
    expect(toolLabel("prisma-migrate")).toBe("Prisma");
    expect(unreadTitle(prisma)).toBe("Prisma migrations found, not read yet");
    expect(unreadTitle({ tool: "alembic", path: "db", marker: null, files: 1 })).toBe("Alembic migrations found, not read yet");
    expect(unreadTitle({ tool: "flyway", path: "db", marker: null, files: 1 })).toBe("flyway found, not read yet");
  });

  it("the source card shows the directory and its files", () => {
    render(<SourceCards cards={[{ kind: "unread", tool: prisma }]} selected={null as never} onSelect={() => undefined} />);
    const card = screen.getByTestId("source-card");
    expect(card.getAttribute("data-tool")).toBe("prisma-migrate");
    expect(card.textContent).toContain("Prisma migrations found, not read yet");
    expect(card.textContent).toContain("prisma/migrations");
    expect(card.textContent).toContain("3 files");
  });

  it("the found-none page and the entry tag never say no migrations", () => {
    const summary = { found: "none", unread_tools: [prisma], sources_read: ["migrations", "models"] } as unknown as SchemaSummary;
    expect(noneTitle(summary)).toBe("Graphvein found Prisma migrations but does not read them yet");
    expect(schemaEntryTag(summary)).toEqual({ text: "not read yet", tone: "muted", title: "Prisma migrations found that Graphvein does not read yet" });
  });
});

describe("where a model table is declared (G1G)", () => {
  it("names the model block for Prisma and the class otherwise", () => {
    expect(declaredIn("prisma")).toBe("the model block");
    expect(declaredIn("sqlmodel")).toBe("the class");
    expect(declaredIn(null)).toBe("the class");
  });

  it("says none declared in the model block for an empty Prisma list", () => {
    const { container } = render(<ModelIndexes attrs={{ indexes: [], unique_constraints: [], check_constraints: [] }} tool="prisma" />);
    expect(container.textContent).toContain("none declared in the model block");
    expect(container.textContent).not.toContain("class");
  });
});
