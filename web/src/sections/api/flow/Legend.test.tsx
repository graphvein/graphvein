import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { LabelProvider } from "@/hooks/useLabels";
import { LABELS_KEY } from "@/lib/labelChoice";
import { RANK_NOTE } from "@/lib/labels";
import { Legend } from "@/sections/api/flow/Legend";
import { FILTER_TIERS, type Tier } from "@/sections/api/flow/tiers";

afterEach(() => {
  window.localStorage.clear();
});

const counts = new Map<Tier, number>([
  ["fact", 1],
  ["exact", 2],
  ["suffix", 1],
]);

function legend() {
  const noop = () => undefined;
  return render(
    <LabelProvider fetcher={() => new Promise(() => undefined)}>
      <Legend counts={counts} tiers={new Set(FILTER_TIERS)} onToggle={noop} onGap={noop} />
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
