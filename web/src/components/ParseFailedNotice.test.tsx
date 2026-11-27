// The parse_failed notice (T074): its sentence, and nothing for a run that
// parsed every file.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { parseArchitecture } from "@/api/architecture";
import { ParseFailedNotice, parseFailedText } from "@/components/ParseFailedNotice";
import { ARCH_GOLDENS } from "@/test/architectureFixtures";

describe("ParseFailedNotice", () => {
  it("names the count with the right noun", () => {
    expect(parseFailedText(1)).toBe("Counts are incomplete: 1 file could not be parsed (parse_failed), so its facts are missing.");
    expect(parseFailedText(1234)).toBe("Counts are incomplete: 1,234 files could not be parsed (parse_failed), so their facts are missing.");
  });

  it("renders nothing for zero", () => {
    const { container } = render(<ParseFailedNotice count={0} />);
    expect(container.innerHTML).toBe("");
  });

  it("points to the Couldn't read list, or to its own list on that page", () => {
    const { unmount } = render(<ParseFailedNotice count={2} />);
    expect(screen.getByRole("link").getAttribute("href")).toBe("#/unread");
    unmount();
    render(<ParseFailedNotice count={2} onPage />);
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByTestId("parse-failed-notice").textContent).toContain("They are listed under Files not read.");
  });

  it("reads the architecture answer's parse_failed, 0 when omitted", () => {
    const raw = structuredClone(ARCH_GOLDENS.components) as Record<string, unknown>;
    expect(parseArchitecture(raw).parse_failed).toBe(0);
    raw.parse_failed = 3;
    expect(parseArchitecture(raw).parse_failed).toBe(3);
  });
});
