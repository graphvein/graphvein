// The code peek used on its own with a stub client, outside the API
// section (T016e2: the Flow tab and trace lanes reuse it).

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { SourceParams } from "@/api/source";
import { CodePeek, kindLabel, lineParts, stateInfo } from "@/components/CodePeek";
import { parseSource, type SourceClient } from "@/api/source";
import { NO_SOURCE, RUN, goldenNode, noSource, notFound, sourceBody, stubSource, type SourceOver } from "@/test/sourceFixtures";

const ID = goldenNode().identity as string;

async function peek(o: SourceOver = {}, props: Partial<Parameters<typeof CodePeek>[0]> = {}) {
  const requests: SourceParams[] = [];
  const onClose = vi.fn();
  render(<CodePeek id={ID} client={stubSource(() => sourceBody(o), requests)} onClose={onClose} {...props} />);
  const panel = await screen.findByTestId("code-peek");
  await waitFor(() => {
    expect(panel.getAttribute("data-state")).not.toBe("loading");
  });
  return { panel, requests, onClose };
}

function banded(panel: HTMLElement): number[] {
  return [...panel.querySelectorAll("[data-on]")].map((e) => Number(e.getAttribute("data-line")));
}

function marks(panel: HTMLElement): string[] {
  return [...panel.querySelectorAll("[data-testid=peek-mark]")].map((m) => m.textContent);
}

describe("lineParts", () => {
  const h = { start_line: 2, start_column: 3, end_line: 4, end_column: 2 };
  it("bands only the lines the range covers, with the exact columns", () => {
    expect(lineParts(1, "abcdef", h).on).toBe(false);
    expect(lineParts(2, "abcdef", h)).toEqual({ on: true, start: 3, end: 6 });
    expect(lineParts(3, "xyz", h)).toEqual({ on: true, start: 0, end: 3 });
    expect(lineParts(4, "klmn", h)).toEqual({ on: true, start: 0, end: 2 });
    expect(lineParts(5, "abc", h).on).toBe(false);
    expect(lineParts(2, "abc", null).on).toBe(false);
  });
  it("does not band an end line the range only reaches at column 0", () => {
    expect(lineParts(4, "klmn", { ...h, end_column: 0 }).on).toBe(false);
    // A one-line empty range still bands its line (nothing is marked).
    expect(lineParts(2, "abc", { start_line: 2, start_column: 1, end_line: 2, end_column: 1 })).toEqual({ on: true, start: 1, end: 1 });
  });
  it("clamps columns to the line", () => {
    expect(lineParts(2, "ab", h)).toEqual({ on: true, start: 2, end: 2 });
  });
});

describe("CodePeek", () => {
  it("requests the node with the default context and marks exactly the given range", async () => {
    const { panel, requests } = await peek();
    expect(requests).toEqual([{ id: ID, context: 8 }]);
    expect(banded(panel)).toEqual([4]);
    expect(marks(panel)).toEqual(["fetch('/t016d-items')"]);
    expect(within(panel).getByTestId("peek-state").textContent).toBe("Exact· file unchanged since index");
    expect(within(panel).getByTestId("peek-path").textContent).toBe("root-call.ts:4:10");
    expect(within(panel).queryByTestId("peek-banner")).toBeNull();
    const ide = within(panel).getByRole("link", { name: "Open in VS Code" });
    expect(ide.getAttribute("href")).toMatch(/^vscode:\/\/file\//);
    expect(within(panel).getByText("Client call")).toBeTruthy();
    expect(within(panel).getByRole("heading").textContent).toBe("atRoot.GET '/t016d-items'");
  });

  it("highlights a multi-line UTF-16 range with an astral character", async () => {
    const lines = ["before", "a😀bc start", "middle", "end😀z after", "after"];
    // UTF-16 columns: 😀 is two code units.
    const { panel } = await peek({ lines, highlight: { start_line: 2, start_column: 1, end_line: 4, end_column: 5 } });
    expect(banded(panel)).toEqual([2, 3, 4]);
    expect(marks(panel)).toEqual(["😀bc start", "middle", "end😀"]);
    // The text outside the marks is unchanged.
    expect(panel.querySelector('[data-line="2"]')?.textContent).toBe("2a😀bc start");
    expect(panel.querySelector('[data-line="4"]')?.textContent).toBe("4end😀z after");
  });

  it("text_match: highlight plus the amber banner", async () => {
    const { panel } = await peek({ state: "text_match", highlight: { start_line: 3, start_column: 0, end_line: 5, end_column: 1 } });
    expect(banded(panel)).toEqual([3, 4, 5]);
    const banner = within(panel).getByTestId("peek-banner");
    expect(banner.textContent).toContain("File changed since the last index; this span's text was found unchanged at lines 3–5.");
    expect(banner.className).toContain("border-stale");
    expect(within(panel).getByTestId("peek-state").getAttribute("data-state")).toBe("text_match");
  });

  it.each(["changed", "span_invalid"])("%s: current lines, no highlight even if one is sent, strong banner", async (state) => {
    const { panel } = await peek({ state, reason: state === "changed" ? "span_not_unique" : null });
    expect(panel.querySelectorAll("[data-line]")).toHaveLength(5);
    expect(banded(panel)).toEqual([]);
    expect(marks(panel)).toEqual([]);
    const banner = within(panel).getByTestId("peek-banner");
    expect(banner.className).toContain("bg-stale");
    expect(banner.textContent).toContain("Span not shown");
    if (state === "changed") expect(banner.textContent).toContain("the indexed text occurs more than once in the file now");
    // The stored line is named, not marked; the path has no column.
    expect(banner.textContent).toContain("stored line 2");
    expect(within(panel).getByTestId("peek-path").textContent).toBe("root-call.ts:2");
    expect(within(panel).getByRole("link", { name: "Open in VS Code" })).toBeTruthy();
  });

  it.each(NO_SOURCE)("%s: keeps the panel, states the reason, disables the IDE button", async (state) => {
    const { panel } = await peek(noSource(state, { reason: state === "unreadable" ? "permission denied" : null, size: state === "too_large" ? 5 * 1024 * 1024 : 120 }));
    const empty = within(panel).getByTestId("peek-empty");
    expect(empty.textContent).not.toBe("");
    expect(panel.querySelector("[data-line]")).toBeNull();
    expect(within(panel).getByTestId("peek-state").getAttribute("data-state")).toBe(state);
    expect(within(panel).queryByRole("link")).toBeNull();
    const ide = within(panel).getByRole("button", { name: /^Open in VS Code: No source to open/ });
    expect(ide.hasAttribute("disabled")).toBe(true);
    const want: Record<string, string> = {
      missing: "File not found",
      not_regular: "Not a regular file",
      too_large: "5.0 MiB",
      binary: "NUL byte",
      unreadable: "permission denied",
      no_location: "No source location",
      not_indexed: "File not in the index",
    };
    expect(empty.textContent).toContain(want[state]);
  });

  it("an unknown state shows its lines without highlight and names the state", async () => {
    const { panel } = await peek({ state: "moved" });
    expect(banded(panel)).toEqual([]);
    expect(within(panel).getByTestId("peek-banner").textContent).toContain("Unknown source state moved.");
    expect(within(panel).getByTestId("peek-state").textContent).toContain("State moved");
    cleanup();
    const empty = await peek(noSource("vanished"));
    expect(within(empty.panel).getByTestId("peek-empty").textContent).toContain("Unknown state vanished");
  });

  it("marks cut lines and a truncated window", async () => {
    const { panel } = await peek({ cut_lines: [3], truncated: true });
    expect(panel.querySelector('[data-line="3"] [data-cut]')).not.toBeNull();
    expect(panel.querySelector('[data-line="2"] [data-cut]')).toBeNull();
    expect(within(panel).getByTestId("peek-truncated")).toBeTruthy();
  });

  it("labels the IDE button from ide.label, else by scheme", async () => {
    const a = await peek({ ide: { available: true, error: null, label: "Open in VS Code Insiders" } });
    expect(within(a.panel).getByRole("link", { name: "Open in VS Code Insiders" })).toBeTruthy();
    cleanup();
    const node = { ...goldenNode(), ide: { link: "zed://file/x/root-call.ts:4:10", line: 4, column: 10, exact: true, line_only: false, caveat: null } };
    const b = await peek({ node, ide: { available: true, error: null, label: null } });
    expect(within(b.panel).getByRole("link", { name: "Open in Zed" })).toBeTruthy();
  });

  it("Show more re-requests with 8 more lines up to 50", async () => {
    const requests: SourceParams[] = [];
    // A long file so both directions can grow.
    const client = stubSource((p) => {
      const lines = Array.from({ length: 2 * p.context + 1 }, (_, i) => `line ${String(i)}`);
      return sourceBody({ lines, first_line: 100 - p.context, line_count: 1000, highlight: { start_line: 100, start_column: 0, end_line: 100, end_column: 4 } });
    }, requests);
    render(<CodePeek id={ID} client={client} />);
    for (const want of [16, 24, 32, 40, 48, 50]) {
      const btns = await screen.findAllByRole("button", { name: /Show more (above|below)/ });
      expect(btns).toHaveLength(2);
      fireEvent.click(btns[want % 16 === 0 ? 0 : 1] as HTMLElement);
      await waitFor(() => {
        expect(requests.at(-1)?.context).toBe(want);
      });
      await waitFor(() => {
        expect(screen.getByTestId("code-peek").getAttribute("aria-busy")).toBe("false");
      });
    }
    expect(screen.queryByRole("button", { name: /Show more/ })).toBeNull();
    expect(screen.getByText("Showing at most 50 lines of context on each side.")).toBeTruthy();
    // The highlight stays on the server's line.
    expect(banded(screen.getByTestId("code-peek"))).toEqual([100]);
  });

  it("offers no Show more at the file's edges", async () => {
    const { panel } = await peek();
    expect(within(panel).queryByRole("button", { name: /Show more/ })).toBeNull();
  });

  it("closes with Esc and the close button", async () => {
    const { panel, onClose } = await peek();
    fireEvent.keyDown(within(panel).getByTestId("peek-body"), { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(within(panel).getByRole("button", { name: "Close code peek (Esc)" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("does not show an answer of another index run", async () => {
    const { panel } = await peek({ finished_at: "2026-11-04T00:00:00Z" }, { expectedRun: RUN });
    expect(panel.getAttribute("data-state")).toBe("mismatch");
    expect(panel.querySelector("[data-line]")).toBeNull();
    expect(within(panel).getByTestId("peek-mismatch").textContent).toContain("another index run");
    expect(within(panel).getByRole<HTMLButtonElement>("button", { name: /^Open in VS Code/ }).disabled).toBe(true);
  });

  it("shows the same run and the stale mark", async () => {
    const { panel } = await peek({}, { expectedRun: RUN, stale: true, title: "atRoot", step: "caller 1 of 5" });
    expect(panel.getAttribute("data-state")).toBe("exact");
    expect(within(panel).getByText("index stale")).toBeTruthy();
    expect(within(panel).getByText("caller 1 of 5")).toBeTruthy();
    expect(within(panel).getByRole("heading").textContent).toBe("atRoot");
  });

  it("explains a failed request and retries", async () => {
    let fail = true;
    const requests: SourceParams[] = [];
    render(<CodePeek id={ID} client={stubSource(() => (fail ? notFound(ID) : sourceBody()), requests)} />);
    const failed = await screen.findByTestId("peek-failed");
    expect(failed.textContent).toContain("no longer in the index");
    fail = false;
    fireEvent.click(within(failed).getByRole("button", { name: "Retry" }));
    await waitFor(() => {
      expect(screen.getByTestId("code-peek").getAttribute("data-state")).toBe("exact");
    });
    expect(requests).toHaveLength(2);
  });

  it("a new node starts again at the default context", async () => {
    const requests: SourceParams[] = [];
    const client = stubSource(() => sourceBody({ first_line: 10, line_count: 100, lines: ["a", "b"], highlight: { start_line: 10, start_column: 0, end_line: 10, end_column: 1 } }), requests);
    const { rerender } = render(<CodePeek id={ID} client={client} />);
    fireEvent.click(await screen.findByRole("button", { name: /Show more above/ }));
    await waitFor(() => {
      expect(requests.at(-1)?.context).toBe(16);
    });
    rerender(<CodePeek id="client_call:other.ts#x" client={client} />);
    await waitFor(() => {
      expect(requests.at(-1)).toEqual({ id: "client_call:other.ts#x", context: 8 });
    });
  });

  describe("a slow answer for an earlier request", () => {
    const B = "client_call:other.ts#other";
    // A client whose answers the test releases by hand, in any order. It
    // ignores the abort signal, as a response already on its way would.
    function manual() {
      const pending: { p: SourceParams; release: () => void }[] = [];
      const client: SourceClient = {
        source: (p) =>
          new Promise((resolve) => {
            const body =
              p.id === B
                ? sourceBody({ node: { ...goldenNode(), identity: B, file: "other.ts", qualified_name: "other", name: "other" }, path: "other.ts", lines: ["const b = 1;", "other();"], highlight: { start_line: 2, start_column: 0, end_line: 2, end_column: 5 } })
                : sourceBody({ first_line: 10 - Math.min(p.context, 9), line_count: 100, lines: Array.from({ length: 2 * Math.min(p.context, 9) + 1 }, (_, i) => `a line ${String(i)}`), highlight: { start_line: 10, start_column: 0, end_line: 10, end_column: 6 } });
            pending.push({ p, release: () => { resolve(parseSource(body)); } });
          }),
      };
      const release = async (id: string, context: number) => {
        const i = pending.findIndex((x) => x.p.id === id && x.p.context === context);
        if (i < 0) throw new Error(`no pending request ${id} ${String(context)}`);
        const [x] = pending.splice(i, 1);
        x?.release();
        // Let the resolved promise settle.
        await new Promise((r) => setTimeout(r, 0));
      };
      return { client, release, pending };
    }

    function expectOnlyB(panel: HTMLElement) {
      expect(panel.getAttribute("data-node")).toBe(B);
      expect(within(panel).getByRole("heading").textContent).toBe("other");
      expect(panel.textContent).not.toContain("a line");
      expect(marks(panel).every((m) => m === "other")).toBe(true);
      expect(within(panel).getByTestId("peek-path").textContent).not.toContain("root-call.ts");
    }

    it("after switching to another node never shows the old file under the new header", async () => {
      const { client, release } = manual();
      const { rerender } = render(<CodePeek id={ID} client={client} title="atRoot" />);
      rerender(<CodePeek id={B} client={client} title="other" />);
      // A's answer arrives while B is still loading.
      await release(ID, 8);
      const panel = screen.getByTestId("code-peek");
      expect(panel.getAttribute("data-state")).toBe("loading");
      expect(panel.querySelector("[data-line]")).toBeNull();
      expect(marks(panel)).toEqual([]);
      expectOnlyB(panel);
      await release(B, 8);
      await waitFor(() => {
        expect(panel.getAttribute("data-state")).toBe("exact");
      });
      expectOnlyB(panel);
      expect(marks(panel)).toEqual(["other"]);
      expect(within(panel).getByTestId("peek-path").textContent).toBe("other.ts:2:1");
    });

    it("after Show more and a switch never shows the old file under the new header", async () => {
      const { client, release } = manual();
      const { rerender } = render(<CodePeek id={ID} client={client} title="atRoot" />);
      await release(ID, 8);
      fireEvent.click(await screen.findByRole("button", { name: /Show more above/ }));
      rerender(<CodePeek id={B} client={client} title="other" />);
      // The Show more answer for A arrives after the switch, then B's.
      await release(ID, 16);
      const panel = screen.getByTestId("code-peek");
      expect(panel.getAttribute("data-state")).toBe("loading");
      expect(panel.querySelector("[data-line]")).toBeNull();
      expectOnlyB(panel);
      await release(B, 8);
      await waitFor(() => {
        expect(panel.getAttribute("data-state")).toBe("exact");
      });
      expectOnlyB(panel);
      expect(marks(panel)).toEqual(["other"]);
    });
  });

  it("names kinds and states", () => {
    expect(kindLabel("client_call")).toBe("Client call");
    expect(kindLabel("ipc_call")).toBe("IPC call");
    expect(kindLabel("function")).toBe("Function");
    expect(stateInfo("exact").tone).toBe("exact");
    expect(stateInfo("whatever").label).toBe("State whatever");
  });
});
