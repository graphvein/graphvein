import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { LabelProvider } from "@/hooks/useLabels";
import { LABELS_KEY } from "@/lib/labelChoice";
import { RANK_NOTE } from "@/lib/labels";
import { hiddenKinds, Legend } from "@/sections/api/flow/Legend";
import { FILTER_TIERS, type Tier } from "@/sections/api/flow/tiers";

afterEach(() => {
  window.localStorage.clear();
});

const counts = new Map<Tier, number>([
  ["fact", 1],
  ["exact", 2],
  ["suffix", 1],
]);

function legend(open = false, c: Map<Tier, number> = counts, onGap: (cat: "dynamic" | "unmatched") => void = () => undefined) {
  const noop = () => undefined;
  return render(
    <LabelProvider fetcher={() => new Promise(() => undefined)}>
      <Legend counts={c} tiers={new Set(FILTER_TIERS)} onToggle={noop} onGap={onGap} defaultOpen={open} />
    </LabelProvider>,
  );
}

describe("Flow legend label style (T025b)", () => {
  it("plain: words with the numbers (T041), the meaning in the tooltip, the rank note once", () => {
    legend();
    expect(screen.getByTestId("chip-fact").textContent).toContain("Fact · 1.0");
    expect(screen.getByTestId("chip-exact").textContent).toContain("Exact path match · 0.9");
    expect(screen.getByTestId("chip-exact").textContent).not.toContain("0.7");
    expect(screen.getByTestId("chip-suffix").textContent).toContain("Path suffix match · 0.6");
    expect(screen.getByTestId("chip-exact").getAttribute("title")).toContain("URL matches the route path; 0.7 when the call's origin is assumed, not proven");
    // Requirement 5: each plain chip with a number says in its tooltip what
    // the number is (one full stop, then the line count).
    for (const t of ["fact", "exact", "suffix"]) expect(screen.getByTestId(`chip-${t}`).getAttribute("title")).toContain(`${RANK_NOTE} `);
    expect(screen.getByTestId("chip-fact").getAttribute("title")).toMatch(/^Fact · 1\.0: exposure or invoke\. The number is a fixed rank per match rule, not a probability\. \d+ lines? drawn\./);
    // The rank note is said once, in the full legend.
    expect(screen.queryByTestId("flow-legend-rank")).toBeNull();
    fireEvent.click(screen.getByTestId("flow-legend-toggle"));
    expect(screen.getAllByText(/fixed rank per match rule, not a probability/)).toHaveLength(1);
    expect(screen.getByTestId("flow-legend-rank").getAttribute("title")).toBe(RANK_NOTE);
  });

  it("technical: the tiers with their confidence numbers, words in the tooltip", () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    legend();
    expect(screen.getByTestId("chip-fact").textContent).toContain("fact 1.0");
    expect(screen.getByTestId("chip-exact").textContent).toContain("exact 0.9 / 0.7");
    expect(screen.getByTestId("chip-suffix").textContent).toContain("suffix 0.6");
    expect(screen.getByTestId("chip-exact").getAttribute("title")).toContain("(Exact path match)");
    expect(screen.getByTestId("chip-exact").getAttribute("aria-label")).toBe("Exact: exact 0.9 / 0.7, 2 lines");
  });
});

// T073 (T071 question 8): the strip shows the kinds drawn here, counts the
// others and opens the full legend behind a button.
describe("Flow legend strip (T073)", () => {
  const drawn = new Map<Tier, number>([
    ["fact", 1],
    ["exact", 2],
  ]);

  it("shows only the kinds with a line, counts the others, closed by default", () => {
    legend(false, drawn);
    const strip = screen.getByTestId("flow-legend-strip");
    expect(within(strip).getAllByRole("button", { pressed: true }).map((b) => b.getAttribute("data-testid"))).toEqual(["chip-fact", "chip-exact"]);
    // suffix, call and near miss at 0, plus Dynamic and Unmatched.
    expect(hiddenKinds(drawn)).toBe(5);
    expect(screen.getByTestId("flow-legend-hidden").textContent).toBe("+ 5 kinds with no line here");
    expect(screen.getByTestId("flow-legend-toggle").getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByTestId("flow-legend-full")).toBeNull();
    expect(screen.queryByTestId("chip-suffix")).toBeNull();
    expect(screen.queryByTestId("chip-dynamic")).toBeNull();
  });

  it("opens the full legend with every kind, the gap sheets and the notes; Esc closes it", () => {
    const gaps: string[] = [];
    legend(false, drawn, (cat) => gaps.push(cat));
    const toggle = screen.getByTestId("flow-legend-toggle");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const full = screen.getByTestId("flow-legend-full");
    expect(toggle.getAttribute("aria-controls")).toBe(full.id);
    for (const t of ["fact", "exact", "suffix", "call", "candidate"]) expect(within(full).getByTestId(`legend-chip-${t}`)).toBeTruthy();
    expect(within(full).getByTestId("legend-chip-suffix").getAttribute("data-count")).toBe("0");
    expect(full.textContent).toContain("defined in");
    expect(full.textContent).toContain("via wrapper()");
    expect(full.textContent).toContain("Counts are lines, not callers.");
    fireEvent.click(within(screen.getByTestId("chip-unmatched")).getByRole("button", { name: "gap sheet" }));
    expect(gaps).toEqual(["unmatched"]);
    fireEvent.keyDown(full, { key: "Escape" });
    expect(screen.queryByTestId("flow-legend-full")).toBeNull();
    expect(document.activeElement).toBe(toggle);
  });

  it("technical: the hidden count in its short form", () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    legend(false, drawn);
    expect(screen.getByTestId("flow-legend-hidden").textContent).toBe("+5 at 0");
  });
});
