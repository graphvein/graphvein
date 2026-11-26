// T024c1 (row SB-M3): a Supabase table's row level security flags and its
// policies as written; unknown values are never shown as "disabled" or
// "no policy".
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RowSecurity } from "@/sections/schema/RowSecurity";

describe("RowSecurity", () => {
  it("shows the flags and the policies of a complete table", () => {
    render(<RowSecurity completeness="complete" attrs={{ rls: { enabled: true, forced: false }, policies: [{ name: "Own profile", command: "UPDATE", table: "profiles" }] }} />);
    expect(screen.getByTestId("rls-flags").textContent).toBe("enabled · not forced");
    expect(screen.getAllByTestId("policy").map((r) => r.textContent)).toEqual(["Own profileUPDATEprofiles"]);
  });

  it("says unknown for a null flag and a null policy list", () => {
    render(<RowSecurity completeness="partial" attrs={{ rls: { enabled: true, forced: null }, policies: null }} />);
    expect(screen.getByTestId("rls-flags").textContent).toContain("unknown");
    expect(screen.getByTestId("policies-unknown")).not.toBeNull();
  });

  it("never states no policy on a partial table", () => {
    render(<RowSecurity completeness="partial" attrs={{ rls: { enabled: null, forced: null }, policies: [] }} />);
    expect(screen.getByTestId("policies-none").textContent).toContain("others may exist");
  });

  it("renders nothing for a record without rls", () => {
    const { container } = render(<RowSecurity completeness="complete" attrs={{ name: "t" }} />);
    expect(container.innerHTML).toBe("");
  });
});
