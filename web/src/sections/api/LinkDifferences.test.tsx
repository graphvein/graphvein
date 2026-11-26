// The request differences of a link (T029 W4): shown only when the server
// compared the link and found one, in the contract's words, each side
// with its provenance chip and the editor button of its side's links.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { IdeStatus } from "@/api/endpoints";
import type { ShapeDifferences } from "@/api/shapeDifferences";
import { LinkDifferences } from "@/sections/api/LinkDifferences";

const status: IdeStatus = { available: true, error: null, label: "Open in VS Code", editor: "vscode", source: "builtin" };

function diffs(over: Partial<ShapeDifferences> = {}): ShapeDifferences {
  return {
    crossRepository: false,
    compared: ["body"],
    differences: [
      { row: "SD-8", section: "body", field: "color", direction: "sent_not_read", text: "sent by the frontend, not read by the handler", call: { file: "web/api.ts", line: 8, rule: "SD-2", kind: "literal" }, route: { file: "backend/app/main.py", line: 7, rule: "SH-F22", kind: "fact" } },
      { row: "SD-9", section: "query", field: "q", direction: "read_not_sent", text: "read by the handler, not sent by this call", call: { file: "web/api.ts", line: 82, rule: "SD-3", kind: "literal" }, route: { file: "backend/app/main.py", line: 36, rule: "SH-F1", kind: "fact" } },
    ],
    callLinks: { "web/api.ts:8": { link: "vscode://file/r/web/api.ts:8:1", line: 8, column: 1, exact: true, line_only: false, caveat: null } },
    routeLinks: { "backend/app/main.py:7": { link: "vscode://file/r/backend/app/main.py:7:1", line: 7, column: 1, exact: true, line_only: false, caveat: null } },
    ...over,
  };
}

describe("LinkDifferences", () => {
  it("shows nothing when nothing was compared or nothing differs", () => {
    const { container, rerender } = render(<LinkDifferences diffs={null} ide={status} />);
    expect(container.textContent).toBe("");
    rerender(<LinkDifferences diffs={diffs({ differences: [] })} ide={status} />);
    expect(container.textContent).toBe("");
    expect(screen.queryByTestId("link-differences")).toBeNull();
  });

  it("names each difference in the contract's words, with both sides' chips and editor buttons", () => {
    render(<LinkDifferences diffs={diffs()} ide={status} />);
    const rows = screen.getAllByTestId("difference");
    expect(rows.map((r) => r.getAttribute("data-direction"))).toEqual(["sent_not_read", "read_not_sent"]);
    expect(rows[0]?.textContent).toContain("color");
    expect(rows[0]?.textContent).toContain("sent by the frontend, not read by the handler");
    expect(rows[1]?.textContent).toContain("?q");
    expect(rows[1]?.textContent).toContain("read by the handler, not sent by this call");
    expect(screen.getByTestId("link-differences").textContent).not.toMatch(/unused|broken|error|no differences/i);
    const first = within(rows[0] as HTMLElement).getAllByTestId("difference-chip");
    expect(first.map((c) => c.getAttribute("data-side"))).toEqual(["Call", "Handler"]);
    expect(first[0]?.textContent).toContain("api.ts:8");
    expect(first[1]?.textContent).toContain("main.py:7");
    // Each side opens its own side's link; a position without one has no button.
    expect(within(first[0] as HTMLElement).getByTestId("chip-ide-button").getAttribute("href")).toBe("vscode://file/r/web/api.ts:8:1");
    expect(within(first[1] as HTMLElement).getByTestId("chip-ide-button").getAttribute("href")).toBe("vscode://file/r/backend/app/main.py:7:1");
    const second = within(rows[1] as HTMLElement).getAllByTestId("difference-chip");
    expect(within(second[0] as HTMLElement).queryByTestId("chip-ide-button")).toBeNull();
    expect(screen.queryByTestId("cross-repository")).toBeNull();
  });

  it("labels a cross-repository link", () => {
    render(<LinkDifferences diffs={diffs({ crossRepository: true })} ide={status} />);
    expect(screen.getByTestId("cross-repository").textContent).toBe("cross-repository link");
  });
});
