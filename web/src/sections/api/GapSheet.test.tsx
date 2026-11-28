import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CallsResponse, Scope } from "@/api/endpoints";
import type { RequestState } from "@/hooks/useApiRequest";
import { GapSheet, copyText, groupItems, itemsFromCalls, searchItems } from "@/sections/api/GapSheet";
import type { Gap } from "@/sections/api/params";
import { callsGolden, endpointsGolden } from "@/test/apiFixtures";

afterEach(() => {
  vi.restoreAllMocks();
});

const summary = endpointsGolden("api-endpoints-production").transports.find((t) => t.transport === "http");

function Harness({
  gap,
  calls,
  scope,
  statuses = ["dynamic"],
  unlisted,
}: {
  gap: Gap;
  calls: RequestState<CallsResponse>;
  scope: Scope;
  statuses?: string[];
  unlisted?: string[];
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => {
          setOpen(true);
        }}
      >
        opener
      </button>
      {open && (
        <GapSheet
          gap={gap}
          repository="fixture"
          scope={scope}
          summary={summary}
          root={null}
          statuses={statuses}
          unlisted={unlisted}
          calls={calls}
          ide={{ available: true, error: null }}
          onRetry={() => undefined}
          onClose={() => {
            setOpen(false);
          }}
          onSelectEndpoint={() => undefined}
        />
      )}
    </>
  );
}

const gaps = callsGolden("api-calls-http-gaps");

function openSheet(gap: Gap = { transport: "http", category: "dynamic" }, scope: Scope = "production", data: CallsResponse = gaps) {
  render(<Harness gap={gap} calls={{ kind: "ok", data }} scope={scope} />);
  const opener = screen.getByRole("button", { name: "opener" });
  opener.focus();
  fireEvent.click(opener);
  return opener;
}

describe("GapSheet helpers", () => {
  const items = itemsFromCalls(gaps);
  it("searches owner, file, expression and reason text", () => {
    expect(searchItems(items, "methodunknown").map((i) => i.title)).toEqual(["methodUnknown"]);
    expect(searchItems(items, "TOOLS/CHECK").map((i) => i.file)).toEqual(["tools/check.ts"]);
    expect(searchItems(items, "not evaluated").length).toBeGreaterThan(0);
    expect(searchItems(items, "base url is not resolved").map((i) => i.title)).toEqual(["remoteUnmatched"]);
  });
  it("groups by primary reason, directory, root or nothing, largest first", () => {
    const byReason = groupItems(items, "reason");
    expect(byReason[0]?.key).toBe("no_matching_route");
    expect(byReason[0]?.items).toHaveLength(3);
    expect(byReason.reduce((n, g) => n + g.items.length, 0)).toBe(items.length);
    expect(groupItems(items, "file").map((g) => g.key)).toEqual(["web/src", "tools"]);
    expect(groupItems(items, "root").map((g) => g.key)).toEqual(["web", "tools"]);
    expect(groupItems(items, "none")).toHaveLength(1);
  });
  it("copies a Markdown table", () => {
    const text = copyText(items.slice(0, 2), false);
    expect(text).toBe(
      "| file:line | status | reason |\n|---|---|---|\n" +
        "| tools/check.ts:2 | unmatched | No route with a known full path matches this method and path |\n" +
        "| web/src/client.ts:3 | wrapper | Inner request of a wrapper; the wrapper's call sites are listed as calls |\n",
    );
    const first = items[0];
    if (first === undefined) throw new Error("fixture changed");
    expect(copyText([{ ...first, reason: "a|b" }], false)).toContain("a\\|b");
  });
});

describe("GapSheet", () => {
  it("opens as a modal dialog with the search focused", () => {
    openSheet();
    const dialog = screen.getByRole("dialog", { name: /Dynamic client calls/ });
    expect(dialog.hasAttribute("open")).toBe(true);
    expect(document.activeElement).toBe(within(dialog).getByRole("searchbox"));
  });

  it("highlights matches with mark elements and filters", () => {
    openSheet();
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByRole("searchbox"), { target: { value: "unknown" } });
    const marks = [...dialog.querySelectorAll("mark")];
    expect(marks.length).toBeGreaterThan(0);
    expect(marks.every((m) => m.textContent.toLowerCase() === "unknown")).toBe(true);
    expect(dialog.querySelectorAll("[data-item]")).toHaveLength(1);
    expect(dialog.textContent).toContain("1 shown of 9");
  });

  it("switches grouping between reason, file, root and none", () => {
    openSheet();
    const dialog = screen.getByRole("dialog");
    const sections = () => [...dialog.querySelectorAll("section")].map((s) => s.getAttribute("aria-label"));
    expect(sections()[0]).toBe("No route with a known full path matches this method and path: 3");
    fireEvent.click(within(dialog).getByRole("button", { name: "File" }));
    expect(sections()).toEqual(["web/src/: 8", "tools/: 1"]);
    fireEvent.click(within(dialog).getByRole("button", { name: "Root" }));
    expect(sections()).toEqual(["web: 8", "tools: 1"]);
    fireEvent.click(within(dialog).getByRole("button", { name: "None" }));
    expect(sections()).toEqual(["All: 9"]);
  });

  it("closes on Escape and returns focus to the opener", () => {
    const opener = openSheet();
    act(() => {
      fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it("closes with the close button and returns focus", () => {
    const opener = openSheet();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it("copies the shown list", async () => {
    const writeText = vi.fn<(s: string) => Promise<void>>().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    openSheet();
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByRole("searchbox"), { target: { value: "tools" } });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Copy list" }));
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith(
      "| file:line | status | reason |\n|---|---|---|\n| tools/check.ts:2 | unmatched | No route with a known full path matches this method and path |\n",
    );
    expect(within(dialog).getByRole("button", { name: "Copied" })).toBeTruthy();
  });

  it("lists endpoints without a linked caller grouped by file only", () => {
    openSheet({ transport: "http", category: "never" }, "all");
    const dialog = screen.getByRole("dialog", { name: /^No linked caller/ });
    expect(dialog.querySelectorAll("[data-item]")).toHaveLength(summary?.never_called ?? -1);
    expect(within(dialog).queryByRole("button", { name: "Reason" })).toBeNull();
    expect(within(dialog).getByRole("button", { name: "File" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("says the list without a linked caller is about production calls in production scope", () => {
    openSheet({ transport: "http", category: "never" });
    const dialog = screen.getByRole("dialog", { name: /^No linked caller in production/ });
    expect(dialog.textContent).not.toMatch(/never called|not called/i);
    expect(dialog.textContent).toContain(
      "No production client call is linked to these (test calls are excluded; turn on Include tests to see them).",
    );
  });

  it("keeps the plain explanation without a linked caller with Include tests", () => {
    openSheet({ transport: "http", category: "never" }, "all");
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("No stored client call is linked to these.");
    expect(dialog.textContent).not.toContain("production");
  });

  it("marks rows whose file changed since the index", () => {
    const data = structuredClone(gaps);
    const first = data.calls[0];
    if (first === undefined) throw new Error("fixture changed");
    first.node.file_state = "changed";
    openSheet({ transport: "http", category: "dynamic" }, "production", data);
    const row = screen.getByRole("dialog").querySelector(`[data-item="${first.node.identity}"]`);
    expect(row?.textContent).toContain("changed");
    const other = screen.getByRole("dialog").querySelector(`[data-item="${data.calls[1]?.node.identity ?? ""}"]`);
    expect(other?.textContent).not.toMatch(/changed|missing/);
  });

  it("shows only the cannot-be-listed note when every status of the category has a comma", () => {
    render(
      <Harness gap={{ transport: "http", category: "other" }} calls={{ kind: "loading" }} scope="production" statuses={[]} unlisted={["a,b"]} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "opener" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByTestId("unlisted-statuses").textContent).toContain('"a,b"');
    expect(dialog.textContent).not.toContain("…");
    expect(dialog.textContent).not.toContain("Nothing to list.");
    expect(dialog.querySelector("[aria-busy]")).toBeNull();
    expect(dialog.querySelectorAll("[data-item]")).toHaveLength(0);
  });
});
