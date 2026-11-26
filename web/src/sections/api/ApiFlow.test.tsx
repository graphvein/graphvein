// The Flow tab inside the API section: the lazy chunk is not imported
// until the tab opens, the hash carries the tab and the Flow state, the
// selection opens the code peek of the node (a line: its source, with the
// stored facts above the code), and the list collapses to a rail.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FlowParams } from "@/api/flow";
import type { SourceParams } from "@/api/source";
import type { StatusView } from "@/hooks/useStatus";
import { ApiSection } from "@/sections/api/ApiSection";
import { MIN_SPLIT_WIDTH } from "@/sections/api/SplitPane";
import type { FlowTabProps } from "@/sections/api/flow/FlowTab";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeFlowClient } from "@/test/flowFixtures";
import { sourceBody, stubSource } from "@/test/sourceFixtures";

const loads = vi.hoisted(() => ({ count: 0 }));

// The lazy chunk, replaced by a stand-in that records its props and
// offers the selections the real canvas offers (its own tests cover it).
vi.mock("@/sections/api/flow/FlowTab", () => {
  loads.count++;
  return {
    default: (p: FlowTabProps) => (
      <div data-testid="flow-stub" data-hops={p.options.hops} data-nm={String(p.options.nearMisses)} data-node={p.selectedNode ?? ""} data-peek={String(p.peekOpen)}>
        {p.graph.nodes.map((n) => (
          <button key={n.id} type="button" data-flow-card={n.id} onClick={() => {
              p.onSelect({ node: n.id, edge: null });
            }}>
            {n.title}
          </button>
        ))}
        {p.graph.edges.map((e) => (
          <button key={e.id} type="button" data-line={e.id} onClick={() => {
              p.onSelect({ node: null, edge: e.id });
            }}>
            {e.id}
          </button>
        ))}
        <button type="button" onClick={p.onTogglePeek}>
          toggle peek
        </button>
      </div>
    ),
  };
});

const SEL = "route:server/app.ts#GET%20%2Ft016d-items";
const HANDLER = "function:server/app.ts#listItems";

function ok(): StatusView {
  return { kind: "ok", status: goldenStatus("fresh") };
}

function setup(hash: string) {
  const flows: FlowParams[] = [];
  const sources: SourceParams[] = [];
  const source = stubSource((p) => sourceBody({ node: { ...(sourceBody() as { node: Record<string, unknown> }).node, identity: p.id } }), sources);
  window.history.replaceState(null, "", hash);
  const view = render(<ApiSection status={ok()} client={fakeClient()} sourceClient={source} flowClient={fakeFlowClient(flows)} />);
  return { flows, sources, unmount: view.unmount };
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("Flow tab in the API section", () => {
  it("loads the Flow chunk and requests the flow only when the tab opens", async () => {
    const { flows } = setup(`#/api?sel=${encodeURIComponent(SEL)}`);
    await screen.findByTestId("endpoint-detail");
    expect(loads.count).toBe(0);
    expect(flows).toHaveLength(0);
    expect(screen.getByTestId("tab-callers").getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByTestId("tab-flow"));
    await screen.findByTestId("flow-stub");
    expect(loads.count).toBe(1);
    expect(flows).toEqual([{ id: SEL, scope: "production", root: null }]);
    expect(window.location.hash).toBe(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow`);
    // The list collapses to the rail; its button shows the list over the pane.
    const rail = screen.getByTestId("list-rail");
    expect(rail.textContent).toContain("GET /t016d-items");
    fireEvent.click(within(rail).getByRole("button", { name: "Show the endpoint list" }));
    expect(screen.getByTestId("list-overlay")).toBeTruthy();
    // The coverage cards and strip give the canvas their room.
    expect(screen.queryByTestId("coverage-cards")).toBeNull();
  });

  it("folds the list into the rail on every tab where the split's minimums do not fit (about 900 px wide)", async () => {
    // jsdom does no layout: an observer that reports at once and a fixed
    // element width stand in for the browser's.
    class Reporting {
      constructor(private readonly cb: ResizeObserverCallback) {}
      observe(): void {
        this.cb([], this);
      }
      unobserve(): void {
        // Nothing to stop.
      }
      disconnect(): void {
        // Nothing to stop.
      }
    }
    vi.stubGlobal("ResizeObserver", Reporting);
    let width = MIN_SPLIT_WIDTH - 1;
    const spy = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => width);
    try {
      const first = setup(`#/api?sel=${encodeURIComponent(SEL)}`);
      await screen.findByTestId("endpoint-detail");
      expect(screen.getByTestId("tab-callers").getAttribute("aria-selected")).toBe("true");
      expect(screen.getByTestId("list-rail")).toBeTruthy();
      expect(screen.queryByRole("listbox")).toBeNull();
      first.unmount();
      // Wide enough for both minimums: the list and detail side by side.
      width = MIN_SPLIT_WIDTH;
      setup(`#/api?sel=${encodeURIComponent(SEL)}`);
      await screen.findByTestId("endpoint-detail");
      expect(screen.queryByTestId("list-rail")).toBeNull();
      expect(screen.getByRole("separator", { name: "Resize the endpoint list" })).toBeTruthy();
    } finally {
      spy.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it("keeps hops and near misses in the hash; toggling never re-requests", async () => {
    const { flows } = setup(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow`);
    const stub = await screen.findByTestId("flow-stub");
    expect(stub.getAttribute("data-hops")).toBe("2");
    fireEvent.click(screen.getByRole("button", { name: "1 hop" }));
    await waitFor(() => {
      expect(screen.getByTestId("flow-stub").getAttribute("data-hops")).toBe("1");
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "Near misses" }));
    await waitFor(() => {
      expect(screen.getByTestId("flow-stub").getAttribute("data-nm")).toBe("false");
    });
    expect(window.location.hash).toBe(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow&hops=1&nm=0`);
    expect(flows).toHaveLength(1);
  });

  it("opens the code peek of a selected card, and of a line's source with its facts", async () => {
    const { sources } = setup(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow`);
    await screen.findByTestId("flow-stub");
    const handler = document.querySelector<HTMLElement>(`[data-flow-card="${HANDLER}"]`);
    if (handler === null) throw new Error("no handler card");
    fireEvent.click(handler);
    const panel = await screen.findByTestId("code-peek");
    expect(panel.getAttribute("data-node")).toBe(HANDLER);
    expect(sources.at(-1)?.id).toBe(HANDLER);
    expect(window.location.hash).toContain(`node=${encodeURIComponent(HANDLER)}`);
    expect(screen.getByTestId("flow-stub").getAttribute("data-peek")).toBe("true");
    expect(screen.queryByTestId("edge-facts")).toBeNull();
    // Closing the peek keeps the selection; the toolbar opens it again.
    fireEvent.click(within(panel).getByRole("button", { name: /close/i }));
    await waitFor(() => {
      expect(screen.queryByTestId("code-peek")).toBeNull();
    });
    expect(window.location.hash).toContain("node=");
    fireEvent.click(screen.getByRole("button", { name: "toggle peek" }));
    await screen.findByTestId("code-peek");
    // A line: the peek of its stored source, with the facts line.
    const line = [...document.querySelectorAll<HTMLElement>("[data-line]")].find((b) => b.textContent === "e11");
    if (line === undefined) throw new Error("no exposes line");
    fireEvent.click(line);
    await waitFor(() => {
      expect(screen.getByTestId("edge-facts").textContent).toBe("exposes_route · fact · 1.0 · evidence server/app.ts:13");
    });
    expect(screen.getByTestId("code-peek").getAttribute("data-node")).toBe(HANDLER);
    expect(window.location.hash).toContain("edge=e11");
  });

  it("closes the peek and clears the selection when another tab opens", async () => {
    setup(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow&node=${encodeURIComponent(HANDLER)}`);
    await screen.findByTestId("code-peek");
    fireEvent.click(screen.getByTestId("tab-callers"));
    await waitFor(() => {
      expect(screen.queryByTestId("code-peek")).toBeNull();
    });
    expect(window.location.hash).not.toContain("node=");
  });

  // T016i D4: a closed Flow peek is peek=0 in the hash.
  it("keeps a closed peek closed across a reload (peek=0); selecting opens it again", async () => {
    const hash = `#/api?sel=${encodeURIComponent(SEL)}&tab=flow&node=${encodeURIComponent(HANDLER)}`;
    const view = setup(hash);
    const panel = await screen.findByTestId("code-peek");
    fireEvent.click(within(panel).getByRole("button", { name: /close/i }));
    await waitFor(() => {
      expect(screen.queryByTestId("code-peek")).toBeNull();
    });
    expect(window.location.hash).toBe(`${hash}&peek=0`);
    view.unmount();
    setup(window.location.hash);
    const stub = await screen.findByTestId("flow-stub");
    expect(stub.getAttribute("data-peek")).toBe("false");
    expect(screen.queryByTestId("code-peek")).toBeNull();
    // The toolbar's Peek source opens it and removes peek.
    fireEvent.click(screen.getByRole("button", { name: "toggle peek" }));
    await screen.findByTestId("code-peek");
    expect(window.location.hash).toBe(hash);
    // Closing again, then selecting a card, removes peek=0 too.
    fireEvent.click(within(screen.getByTestId("code-peek")).getByRole("button", { name: /close/i }));
    await waitFor(() => {
      expect(window.location.hash).toBe(`${hash}&peek=0`);
    });
    const card = document.querySelector<HTMLElement>(`[data-flow-card="${HANDLER}"]`);
    if (card === null) throw new Error("no handler card");
    fireEvent.click(card);
    await screen.findByTestId("code-peek");
    expect(window.location.hash).not.toContain("peek=");
  });

  it("ignores any other peek value on the Flow tab, and switching tabs removes peek", async () => {
    setup(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow&node=${encodeURIComponent(HANDLER)}&peek=bogus`);
    expect((await screen.findByTestId("code-peek")).getAttribute("data-node")).toBe(HANDLER);
    fireEvent.click(screen.getByTestId("tab-callers"));
    await waitFor(() => {
      expect(window.location.hash).toBe(`#/api?sel=${encodeURIComponent(SEL)}`);
    });
  });

  it("switching from Callers to the Flow tab removes a Callers peek", async () => {
    setup(`#/api?sel=${encodeURIComponent(SEL)}&peek=${encodeURIComponent(HANDLER)}`);
    await screen.findByTestId("code-peek");
    fireEvent.click(screen.getByTestId("tab-flow"));
    await screen.findByTestId("flow-stub");
    expect(window.location.hash).toBe(`#/api?sel=${encodeURIComponent(SEL)}&tab=flow`);
    expect(screen.queryByTestId("code-peek")).toBeNull();
  });
});
