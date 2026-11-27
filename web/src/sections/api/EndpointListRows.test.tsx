import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { EndpointRow, TransportSummary } from "@/api/endpoints";
import { EndpointList, quietAssumed } from "@/sections/api/EndpointList";
import { endpointsGolden } from "@/test/apiFixtures";

// T073 (T071 questions 9 and 10): quiet row text instead of the "no linked
// caller" chip, and the path-assumption icon once in the group header when
// at least half the group has it.

const http = endpointsGolden("api-endpoints-production").transports.find((t) => t.transport === "http");

function group(assumed: number, size: number): TransportSummary {
  if (http === undefined) throw new Error("fixture changed");
  const row = http.endpoints[0];
  if (row === undefined) throw new Error("fixture changed");
  const endpoints: EndpointRow[] = Array.from({ length: size }, (_, i) => ({
    ...row,
    id: `r${String(i)}`,
    name: `/r${String(i)}`,
    path: `/r${String(i)}`,
    caller_count: i === 0 ? 2 : 0,
    project_caller_count: null,
    project_possible_caller_count: null,
    flags: i < assumed ? ["path_assumed"] : [],
  }));
  return { ...http, endpoints };
}

function list(t: TransportSummary, selected: string | null = null) {
  return render(<EndpointList transports={[t]} repository="owncast" scope="production" query="" onQuery={() => undefined} selected={selected} onSelect={() => undefined} />);
}

describe("quietAssumed", () => {
  it("moves the icon to the header from half the group", () => {
    expect(quietAssumed(0, 0)).toBe(false);
    expect(quietAssumed(1, 3)).toBe(false);
    expect(quietAssumed(2, 4)).toBe(true);
    expect(quietAssumed(266, 273)).toBe(true);
  });
});

describe("endpoint list rows (T073)", () => {
  it("says 0 linked as quiet text and n callers, never the chip", () => {
    list(group(0, 3));
    const quiet = screen.getAllByTestId("row-no-caller");
    expect(quiet).toHaveLength(2);
    expect(quiet[0]?.textContent).toBe("0 linked");
    expect(quiet[0]?.className).not.toMatch(/bg-never-soft/);
    expect(quiet[0]?.getAttribute("title")).toMatch(/^No linked caller in production\. /);
    expect(screen.getByText("2 callers")).toBeTruthy();
    expect(screen.queryByText("no linked caller in production")).toBeNull();
    // The list draws the header twice (in place and pinned).
    for (const h of screen.getAllByTestId("group-no-caller")) expect(h.textContent).toBe("2 no linked caller in production");
  });

  it("counts the assumption once in the header when half the group or more has it", () => {
    list(group(3, 4), "r1");
    const head = screen.getAllByTestId("group-path-assumed")[0];
    if (head === undefined) throw new Error("no header count");
    expect(head.textContent).toBe("3 path assumed");
    expect(head.getAttribute("title")).toContain("3 of 4 routes");
    const icons = screen.getAllByTestId("row-path-assumed");
    expect(icons).toHaveLength(3);
    // Hidden on the rows, except under the pointer and on the selected row.
    const selected = document.getElementById("ep-r1");
    const shown = icons.filter((i) => !i.className.includes("hidden"));
    expect(shown).toHaveLength(1);
    expect(selected === null ? null : within(selected).getByTestId("row-path-assumed")).toBe(shown[0]);
    for (const i of icons) if (i !== shown[0]) expect(i.className).toContain("group-hover/row:inline");
  });

  it("keeps the icon on every row that has it below half the group, and no header count", () => {
    list(group(1, 4));
    expect(screen.queryByTestId("group-path-assumed")).toBeNull();
    const icons = screen.getAllByTestId("row-path-assumed");
    expect(icons).toHaveLength(1);
    expect(icons[0]?.className ?? "").not.toContain("hidden");
  });
});
