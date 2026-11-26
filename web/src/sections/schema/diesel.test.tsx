// T018g4 Revision 2 (independent review, C UX): a Diesel table is a
// `table!` block, not a class: the detail header names it "table! <Rust
// path>", its declarations sit in the table! block, and the table-list
// reasons speak of the schema file and table! blocks. A type family of an
// extension type lists its generic modifiers unknown.
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { modelTypeCell, tableListReason, typeFamily } from "@/sections/schema/cells";
import { names as buildNames } from "@/sections/schema/names";
import { declaredIn, definedBy, TableDetailView } from "@/sections/schema/TableDetail";
import { setsGolden } from "@/test/schemaFixtures";

describe("a Diesel table's wording", () => {
  it("names the table! block by its Rust path", () => {
    expect(definedBy("diesel", { class: "x#Ignored" }, "audit.events")).toEqual({ label: "table!", name: "audit.events" });
    expect(definedBy("diesel", {}, "")).toEqual({ label: "table!", name: null });
    expect(definedBy("sqlmodel", { class: "app/models.py#Hero" }, "Hero")).toEqual({ label: "class", name: "Hero" });
    expect(declaredIn("diesel")).toBe("the table! block");
  });

  it("speaks of the schema file and table! blocks in the table-list reasons", () => {
    for (const r of ["model_unread", "model_counts_unknown", "model_not_claimed"]) {
      const diesel = tableListReason(r, null, "diesel");
      expect(diesel.text).not.toMatch(/class/);
      expect(diesel.rule).toBe(tableListReason(r, null).rule);
    }
    expect(tableListReason("model_not_claimed", null, "diesel").text).toContain("table! blocks");
    expect(tableListReason("model_unread", null).text).toContain("classes");
    expect(tableListReason("opaque_statements", 2, "diesel").text).toBe("2 opaque statements can create or change objects the migrations do not show.");
  });

  it("shows table! users in the detail header, not class unknown", () => {
    const sets = setsGolden("models");
    const g = sets.sets.find((x) => x.tool === "sqlmodel");
    const first = g?.tables[0];
    if (g === undefined || first === undefined) throw new Error("golden changed");
    const group = { ...g, tool: "diesel", set: "models/diesel/src/schema.rs" };
    const table = { ...first, node: { ...first.node, qualified_name: "users", attributes: { ...first.node.attributes, class: undefined, tool: "diesel" } } };
    render(
      <TableDetailView table={table} group={group} detail={null} names={buildNames(sets)} changed={new Set()} multiSet={false} onTable={vi.fn()} onMigration={vi.fn()} visible={() => true} onClose={vi.fn()} />,
    );
    const header = screen.getByTestId("detail-header");
    expect(header.textContent).toContain("table! users");
    expect(header.textContent).not.toMatch(/class/);
    expect(screen.getByTestId("model-order-key").textContent).toContain("Listed in the table! block's order");
    expect(screen.getByTestId("table-detail").textContent).not.toMatch(/class chain|class unknown/);
  });
});

describe("the generic modifiers of a type family", () => {
  it("shows an extension type's family as (modifiers unknown)", () => {
    const c = modelTypeCell({ type: null, type_family: { name: "geometry", unknown: ["modifiers"] } });
    expect(c.rule).toBe("type.family");
    expect(c.text).toBe("geometry (modifiers unknown)");
  });

  it("refuses the generic modifiers beside another modifier", () => {
    expect(typeFamily({ name: "geometry", unknown: ["modifiers", "length"] })).toBeNull();
    expect(modelTypeCell({ type: null, type_family: { name: "geometry", unknown: ["modifiers", "length"] } }).rule).toBe("type.unknown");
  });
});
