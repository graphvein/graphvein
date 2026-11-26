import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TooltipProvider } from "@/components/ui/tooltip";
import { ExtractorBadge, extractorNote } from "@/shell/ExtractorBadge";
import { goldenStatus } from "@/test/fixtures";

describe("ExtractorBadge", () => {
  it("says there is nothing to compare with when the store has no run", async () => {
    const status = goldenStatus("not_indexed");
    expect(status.index.last_run).toBeNull();
    expect(status.extractor.matches).toBeNull();
    const hasRun = status.index.last_run !== null;
    expect(extractorNote(status.extractor, hasRun)).toBe(
      "Extractor version of this binary. No index run to compare with.",
    );

    render(
      <TooltipProvider>
        <ExtractorBadge extractor={status.extractor} hasRun={hasRun} />
      </TooltipProvider>,
    );
    fireEvent.focus(screen.getByText(status.extractor.current));
    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toContain("No index run to compare with.");
    expect(tip.textContent).not.toContain("not built by these extractors");
  });

  it.each([true, false])("claims no comparison without a run even when matches is %s", (matches) => {
    const extractor = { current: "extractors-27", indexed: "extractors-26", matches };
    expect(extractorNote(extractor, false)).toBe(
      "Extractor version of this binary. No index run to compare with.",
    );
    render(
      <TooltipProvider>
        <ExtractorBadge extractor={extractor} hasRun={false} />
      </TooltipProvider>,
    );
    expect(screen.queryByText("re-index recommended")).toBeNull();
  });

  it("names another indexer only when a run exists", () => {
    const extractor = { current: "extractors-27", indexed: null, matches: null };
    expect(extractorNote(extractor, true)).toBe(
      "Extractor version of this binary. The index was not built by these extractors, so versions are not compared.",
    );
  });

  it("explains a match and a mismatch", () => {
    expect(extractorNote(goldenStatus("fresh").extractor, true)).toBe(
      "Extractor version of this binary; the index was built with the same version.",
    );
    expect(extractorNote({ current: "extractors-27", indexed: "extractors-26", matches: false }, true)).toBe(
      "Indexed with extractors-26; this binary has extractors-27. Run graphvein index.",
    );
  });
});
