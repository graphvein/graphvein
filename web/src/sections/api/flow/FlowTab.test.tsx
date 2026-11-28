// The Flow tab's canvas in jsdom: the real React Flow and the real ELK
// (bundled build, in-process), with the selection held by the test. Every
// keyboard action has a visible, labelled control (D7).
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import ELK from "elkjs/lib/elk.bundled.js";
import { useState } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";

import type { FlowResponse } from "@/api/flow";
import { buildGraph, COL, COLUMNS, type GraphOptions, type Hops } from "@/sections/api/flow/graph";
import FlowTab, { fitsChain, layoutAtCap, type LayoutRunner } from "@/sections/api/flow/FlowTab";
import { CAPTION_ROW, CAPTION_ROW_GAP, chainColumns, estimatedGap, placeCaption, TABLE_GAP } from "@/sections/api/flow/layout";
import { CARD_MAX_W, CARD_MIN_CAP, captionWidth, cardCap, cardSize } from "@/sections/api/flow/measure";
import { FILTER_TIERS, type FilterTier } from "@/sections/api/flow/tiers";
import { flowGolden } from "@/test/flowFixtures";

beforeAll(() => {
  // jsdom does no layout. React Flow measures nodes (offsetWidth/Height,
  // through a ResizeObserver) before it draws lines, and reads the
  // viewport transform through DOMMatrixReadOnly; these stand-ins are the
  // ones React Flow's testing guide gives, for this file only.
  class Observer {
    private readonly cb: ResizeObserverCallback;
    constructor(cb: ResizeObserverCallback) {
      this.cb = cb;
    }
    observe(target: Element): void {
      const entry = { target, contentRect: { width: 1200, height: 600 } } as unknown as ResizeObserverEntry;
      this.cb([entry], this);
    }
    unobserve(): void {
      // Nothing is held.
    }
    disconnect(): void {
      // Nothing is held.
    }
  }
  Object.defineProperty(globalThis, "ResizeObserver", { value: Observer, configurable: true });
  Object.defineProperties(HTMLElement.prototype, {
    offsetHeight: {
      configurable: true,
      get(this: HTMLElement) {
        return Number.parseFloat(this.style.height) || 1;
      },
    },
    offsetWidth: {
      configurable: true,
      get(this: HTMLElement) {
        return Number.parseFloat(this.style.width) || 1;
      },
    },
  });
  if (!("DOMMatrixReadOnly" in window)) {
    class Matrix {
      m22: number;
      constructor(t?: string) {
        const scale = t?.match(/scale\(([1-9.])/)?.[1];
        this.m22 = scale !== undefined ? Number(scale) : 1;
      }
    }
    Object.defineProperty(window, "DOMMatrixReadOnly", { value: Matrix, configurable: true });
  }
});

const elk = new ELK();
const runner: LayoutRunner = { layout: (g) => elk.layout(g) };
// The runner the next renderFlow uses (a test may count its calls).
let activeRunner: LayoutRunner = runner;
// The Architecture back link the next renderFlow passes (T026 AG-C3).
let activeBack: string | null = null;

interface Seen {
  select: { node: string | null; edge: string | null }[];
  hops: Hops[];
  tiers: FilterTier[][];
  gap: string[];
  peek: number;
}

// The record of what the tab asked for; reset per render.
let seen: Seen = newSeen();

function Harness({ flow, initial, size }: { flow: FlowResponse; initial?: { node: string | null; edge: string | null }; size?: { width: number; height: number } }) {
  const [sel, setSel] = useState(initial ?? { node: null, edge: null });
  const [peek, setPeek] = useState(true);
  const [hops, setHops] = useState<Hops>(2);
  const [tiers, setTiers] = useState<FilterTier[]>([...FILTER_TIERS]);
  const [nm, setNm] = useState(true);
  const options: GraphOptions = { hops, nearMisses: nm, tiers: new Set(tiers) };
  const graph = buildGraph(flow, options);
  const layoutGraph = hops === 2 ? graph : buildGraph(flow, { ...options, hops: 2 });
  return (
    <div style={{ height: 600 }}>
      {/* Stands in for the tab row's near-miss checkbox (outside the tab). */}
      <label>
        <input
          type="checkbox"
          checked={nm}
          onChange={(e) => {
            setNm(e.target.checked);
          }}
        />
        Near misses
      </label>
      <FlowTab
        flow={flow}
        graph={graph}
        layoutGraph={layoutGraph}
        options={options}
        selectedNode={sel.node}
        selectedEdge={sel.edge}
        peekOpen={peek && (sel.node !== null || sel.edge !== null)}
        onSelect={(s) => {
          seen.select.push(s);
          setSel(s);
          setPeek(true);
        }}
        onTogglePeek={() => {
          seen.peek++;
          setPeek((p) => !p);
        }}
        onHops={(h) => {
          seen.hops.push(h);
          setHops(h);
        }}
        onTiers={(t) => {
          seen.tiers.push(t);
          setTiers(t);
        }}
        onGap={(c) => {
          seen.gap.push(c);
        }}
        onCallers={vi.fn()}
        elk={activeRunner}
        back={activeBack}
        fallbackSize={size ?? { width: 1200, height: 600 }}
      />
    </div>
  );
}

function newSeen(): Seen {
  return { select: [], hops: [], tiers: [], gap: [], peek: 0 };
}

async function renderFlow(flow: FlowResponse, initial?: { node: string | null; edge: string | null }, size?: { width: number; height: number }) {
  seen = newSeen();
  render(<Harness flow={flow} {...(initial !== undefined ? { initial } : {})} {...(size !== undefined ? { size } : {})} />);
  await screen.findByTestId("flow-canvas", {}, { timeout: 5000 });
  await waitFor(() => {
    expect(document.querySelectorAll("[data-flow-card]").length).toBeGreaterThan(0);
  });
  activeRunner = runner;
  activeBack = null;
  return seen;
}

/** positions maps each drawn card's id to its React Flow transform. */
function positions(): Map<string, string> {
  return new Map([...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => [n.getAttribute("data-id") ?? "", n.style.transform]));
}

function viewportTransform(): string {
  return document.querySelector<HTMLElement>(".react-flow__viewport")?.style.transform ?? "";
}

/**
 * expectCssNumber checks a number read back from an inline style against the
 * value the component wrote. jsdom (31 and later) serialises CSS numbers to
 * six significant digits (638.3999999999999 reads back as 638.4), so the
 * check allows exactly that rounding and nothing more.
 */
function expectCssNumber(actual: number, expected: number, label: string): void {
  expect(Math.abs(actual - expected), label).toBeLessThanOrEqual(Math.abs(expected) * 5e-6 + 1e-12);
}

/** fresh gives a golden its own layout cache key (another index run). */
function fresh(f: FlowResponse, tag: string): FlowResponse {
  return { ...f, index: f.index === null ? null : { ...f.index, finished_at: `2026-11-04T00:00:${tag}Z` } };
}

function card(id: string): HTMLElement {
  const el = [...document.querySelectorAll<HTMLElement>("[data-flow-card]")].find((e) => e.getAttribute("data-flow-card") === id);
  if (el === undefined) throw new Error(`no card ${id}`);
  return el;
}

describe("Flow tab", () => {
  it("draws a card per node and a line per drawn edge, with the summary from counts", async () => {
    const f = flowGolden("flow-graph");
    // Wide enough for the 360 px cap (the notes are drawn whole).
    await renderFlow(f, undefined, { width: 2000, height: 600 });
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    expect(g.nodes).toHaveLength(16);
    await waitFor(
      () => {
        expect(document.querySelectorAll("[data-flow-card]")).toHaveLength(g.nodes.length);
      },
      { timeout: 4000 },
    );
    expect(document.querySelectorAll("[data-flow-line]")).toHaveLength(g.edges.length);
    expect(document.querySelectorAll('[data-tier="candidate"]')).toHaveLength(1);
    const summary = screen.getByTestId("flow-summary");
    expect(summary.textContent).toContain("4 callers (3 exact, 1 suffix)");
    expect(summary.textContent).toContain("1 near miss");
    expect(summary.textContent).toContain("3 functions call an owner or enclosing function (hop 2; not callers of this route)");
    expect(screen.getAllByText("calls makeHooks.useGetFlow (contains this call)")).toHaveLength(2);
  });

  it("selects a card on click, with a toolbar for every card action", async () => {
    const f = flowGolden("flow-route");
    const seen = await renderFlow(f);
    const handler = f.handler.node?.identity ?? "";
    fireEvent.click(card(handler));
    expect(seen.select.at(-1)).toEqual({ node: handler, edge: null });
    const toolbar = await screen.findByTestId("node-toolbar");
    const peek = within(toolbar).getByTestId("toolbar-peek");
    expect(peek.getAttribute("aria-pressed")).toBe("true");
    expect(peek.getAttribute("aria-keyshortcuts")).toBe("Enter");
    fireEvent.click(peek);
    expect(seen.peek).toBe(1);
    // The IDE button carries the configured label and its shortcut.
    const ide = within(toolbar).getByTestId("toolbar-ide");
    expect(ide.textContent).toBe("Open in VS Code");
    expect(ide.getAttribute("aria-keyshortcuts")).toBe("o");
    expect(ide.getAttribute("href")).toContain("vscode://file/");
    expect(within(toolbar).getByTestId("toolbar-incoming").textContent).toBe("Incoming 0");
    expect(within(toolbar).getByTestId("toolbar-outgoing").textContent).toBe("Outgoing 1");
    fireEvent.click(within(toolbar).getByTestId("toolbar-outgoing"));
    fireEvent.click(within(toolbar).getByRole("button", { name: /^Select the line to / }));
    expect(seen.select.at(-1)?.edge).toMatch(/^e\d+$/);
  });

  it("disables the IDE button without a link", async () => {
    const f = flowGolden("flow-route");
    if (f.handler.node !== null) {
      const h = f.nodes.find((n) => n.node.identity === f.handler.node?.identity);
      if (h !== undefined) h.node.ide = null;
    }
    await renderFlow(f, { node: f.handler.node?.identity ?? null, edge: null });
    const ide = within(await screen.findByTestId("node-toolbar")).getByTestId("toolbar-ide");
    expect(ide.tagName).toBe("BUTTON");
    expect((ide as HTMLButtonElement).disabled).toBe(true);
    expect(ide.getAttribute("title")).toBe("No IDE link for this node (no location)");
  });

  it("selects a line from its label", async () => {
    const f = flowGolden("flow-route");
    const seen = await renderFlow(f);
    const label = (await screen.findAllByRole("button", { name: "Line: Fact · 1.0" }))[0];
    if (label === undefined) throw new Error("no fact label");
    fireEvent.click(label);
    expect(seen.select.at(-1)?.node).toBeNull();
    expect(seen.select.at(-1)?.edge).toMatch(/^e\d+$/);
  });

  it("has a visible control for each shortcut, and the shortcuts work", async () => {
    const f = flowGolden("flow-graph");
    const seen = await renderFlow(f, { node: f.endpoint.identity, edge: null });
    for (const [name, key] of [
      ["Zoom out", "-"],
      ["Zoom in", "+"],
      ["Fit", "0"],
    ] as const) {
      expect(screen.getByRole("button", { name }).getAttribute("aria-keyshortcuts")).toBe(key);
    }
    fireEvent.keyDown(card(f.endpoint.identity), { key: "1" });
    expect(seen.hops).toEqual([1]);
    // Another hop setting is another layout.
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("12");
    });
    fireEvent.keyDown(card(f.endpoint.identity), { key: "2" });
    expect(seen.hops).toEqual([1, 2]);
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("16");
    });
    // Esc closes the peek first, then clears the selection.
    const canvas = screen.getByTestId("flow-canvas");
    fireEvent.keyDown(canvas, { key: "Escape" });
    expect(seen.peek).toBe(1);
    fireEvent.keyDown(canvas, { key: "Escape" });
    expect(seen.select.at(-1)).toEqual({ node: null, edge: null });
  });

  it("moves the focus to connected cards with the arrow keys", async () => {
    const f = flowGolden("flow-route");
    await renderFlow(f);
    const handler = f.handler.node?.identity ?? "";
    // Left and Right follow the columns: the endpoint is left of the
    // handler although the stored exposes edge runs handler -> endpoint.
    card(handler).focus();
    fireEvent.keyDown(card(handler), { key: "ArrowLeft" });
    await waitFor(() => {
      expect(document.activeElement).toBe(card(f.endpoint.identity));
    });
    fireEvent.keyDown(card(f.endpoint.identity), { key: "ArrowRight" });
    await waitFor(() => {
      expect(document.activeElement).toBe(card(handler));
    });
    fireEvent.keyDown(card(handler), { key: "ArrowLeft" });
    await waitFor(() => {
      expect(document.activeElement).toBe(card(f.endpoint.identity));
    });
    fireEvent.keyDown(card(f.endpoint.identity), { key: "ArrowLeft" });
    await waitFor(() => {
      expect(document.activeElement?.getAttribute("data-roles")).toContain("call");
    });
  });

  it("draws the handler's exposes fact left to right with its arrowhead at the stored target", async () => {
    const f = flowGolden("flow-route");
    await renderFlow(f);
    const handler = f.handler.node?.identity ?? "";
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const exposes = g.edges.find((e) => e.source === handler && e.target === f.endpoint.identity);
    if (exposes === undefined) throw new Error("no exposes edge");
    const line = await waitFor(() => {
      const el = document.querySelector(`[data-flow-line="${exposes.id}"]`);
      if (el === null) throw new Error("not drawn");
      return el;
    });
    // The arrowhead is the line group's last path: M base+ L tip L base- Z.
    const head = [...line.querySelectorAll("path")].at(-1)?.getAttribute("d") ?? "";
    const [bx, , tx, ty] = (head.match(/-?\d+(\.\d+)?/g) ?? []).map(Number);
    const box = (id: string) => {
      const el = [...document.querySelectorAll<HTMLElement>(".react-flow__node")].find((n) => n.getAttribute("data-id") === id);
      const m = /translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)/.exec(el?.style.transform ?? "");
      if (el === undefined || m === null) throw new Error(`no node ${id}`);
      return { x: Number(m[1]), y: Number(m[2]), width: Number.parseFloat(el.style.width), height: Number.parseFloat(el.style.height) };
    };
    const ep = box(f.endpoint.identity);
    const hd = box(handler);
    if (bx === undefined || tx === undefined || ty === undefined) throw new Error(head);
    expect(ep.x).toBeLessThan(hd.x);
    // The tip touches the endpoint's east side and points left, at it.
    expect(Math.abs(tx - (ep.x + ep.width))).toBeLessThanOrEqual(1.5);
    expect(ty).toBeGreaterThanOrEqual(ep.y);
    expect(ty).toBeLessThanOrEqual(ep.y + ep.height);
    expect(bx).toBeGreaterThan(tx);
    // The line is still the stored fact, from the handler.
    expect(exposes.label).toBe("Fact · 1.0");
    expect(line.getAttribute("data-tier")).toBe("fact");
  });

  it("keeps the focus on the same card when the hops change, else on the endpoint", async () => {
    const f = flowGolden("flow-graph");
    await renderFlow(f);
    const g2 = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const g1 = buildGraph(f, { hops: 1, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const kept = g1.nodes.find((n) => n.type === "card" && !n.focus);
    const hidden = g2.nodes.find((n) => n.type === "card" && !g1.nodes.some((m) => m.id === n.id));
    if (kept === undefined || hidden === undefined) throw new Error("golden changed");
    card(kept.id).focus();
    fireEvent.keyDown(card(kept.id), { key: "1" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("12");
      expect(document.activeElement).toBe(card(kept.id));
    });
    fireEvent.keyDown(card(kept.id), { key: "2" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("16");
      expect(document.activeElement).toBe(card(kept.id));
    });
    // A card that 1 hop no longer draws hands the focus to the endpoint.
    card(hidden.id).focus();
    fireEvent.keyDown(card(hidden.id), { key: "1" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("12");
      expect(document.activeElement).toBe(card(f.endpoint.identity));
    });
  });

  it("leaves the focus on the near-miss control used", async () => {
    const f = flowGolden("flow-graph");
    await renderFlow(f);
    const box = screen.getByRole("checkbox", { name: "Near misses" });
    box.focus();
    fireEvent.click(box);
    await waitFor(() => {
      expect(document.querySelectorAll("[data-flow-card]").length).toBeGreaterThan(0);
      expect(document.querySelectorAll('[data-tier="candidate"]')).toHaveLength(0);
    });
    expect(document.activeElement).toBe(box);
  });

  it("dims a tier from its chip without moving a card", async () => {
    const f = flowGolden("flow-graph");
    const seen = await renderFlow(f);
    const before = [...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => n.style.transform);
    const chip = screen.getByTestId("chip-call");
    expect(chip.getAttribute("aria-pressed")).toBe("true");
    expect(chip.getAttribute("data-count")).toBe("8");
    // The count is of drawn lines, and says so (the summary counts callers).
    expect(chip.getAttribute("aria-label")).toBe("Call: call, 8 lines");
    expect(chip.getAttribute("title")).toContain("8 lines drawn");
    fireEvent.click(screen.getByTestId("flow-legend-toggle"));
    expect(screen.getByTestId("flow-legend-full").textContent).toContain("Counts are lines, not callers");
    fireEvent.click(chip);
    expect(seen.tiers.at(-1)).not.toContain("call");
    await waitFor(() => {
      expect(document.querySelectorAll('[data-tier="call"][data-dim="true"]').length).toBe(8);
    });
    expect([...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => n.style.transform)).toEqual(before);
    // Dynamic and unmatched are never attached: disabled at 0, with the
    // way to their gap sheets.
    const dyn = screen.getByTestId("chip-dynamic");
    expect(dyn.textContent).toContain("never attached to an endpoint");
    expect(within(dyn).getAllByRole("button")[0]?.hasAttribute("disabled")).toBe(true);
    fireEvent.click(within(dyn).getByRole("button", { name: "gap sheet" }));
    expect(seen.gap).toEqual(["dynamic"]);
  });

  it("shows the overview only where it covers no card, with a Map button", async () => {
    const f = flowGolden("flow-route");
    const map = () => document.querySelector(".react-flow__minimap");
    const toggle = () => screen.getByTestId("map-toggle");
    // A tall canvas, the peek closed: shown, clear of every card.
    await renderFlow(f);
    expect(map()).not.toBeNull();
    expect(toggle().getAttribute("aria-pressed")).toBe("true");
    // Selecting a card opens the peek: hidden by default.
    const handler = f.handler.node?.identity ?? "";
    fireEvent.click(card(handler));
    await waitFor(() => {
      expect(map()).toBeNull();
    });
    expect(toggle().getAttribute("aria-pressed")).toBe("false");
    // The Map button shows it anyway; closing the peek drops that choice.
    fireEvent.click(toggle());
    expect(map()).not.toBeNull();
    fireEvent.keyDown(card(handler), { key: "Escape" });
    await waitFor(() => {
      expect(seen.peek).toBe(1);
    });
    expect(map()).not.toBeNull();
  });

  it("pans sideways only to the handler and downstream", async () => {
    await renderFlow(flowGolden("flow-graph"), undefined, { width: 600, height: 600 });
    const viewport = () => {
      const m = /translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)/.exec(document.querySelector<HTMLElement>(".react-flow__viewport")?.style.transform ?? "");
      if (m === null) throw new Error("no viewport transform");
      return { x: Number(m[1]), y: Number(m[2]) };
    };
    const before = viewport();
    fireEvent.click(screen.getByTestId("pan-pill"));
    await waitFor(() => {
      expect(viewport().x).toBeLessThan(before.x);
    });
    // The handler stays at the height the fit gave it, or lower when a card
    // in view would otherwise sit under the pills.
    expect(viewport().y).toBeGreaterThanOrEqual(before.y);
    // The endpoint is now off to the left: the left pill, a button in the
    // tab order, goes back to the fitted view.
    const back = await screen.findByTestId("pan-pill-left");
    expect(back.tagName).toBe("BUTTON");
    expect(back.textContent).toBe("Endpoint + callers");
    fireEvent.click(back);
    await waitFor(() => {
      expect(viewport()).toEqual(before);
    });
  });

  it("opens on the call sites, endpoint and handler with a pill to each side's cards", async () => {
    const f = flowGolden("flow-graph");
    await renderFlow(f, undefined, { width: 600, height: 600 });
    const pill = screen.queryByTestId("pan-pill-left");
    const hasLeft = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) }).nodes.some((n) => n.column < 3);
    expect(pill !== null).toBe(hasLeft);
    expect(screen.getByTestId("pan-pill").textContent).toBe("Handler + downstream");
  });

  it("hides the overview on a short canvas", async () => {
    await renderFlow(flowGolden("flow-route"), undefined, { width: 1200, height: 400 });
    expect(document.querySelector(".react-flow__minimap")).toBeNull();
    fireEvent.click(screen.getByTestId("map-toggle"));
    expect(document.querySelector(".react-flow__minimap")).not.toBeNull();
  });

  it("says when nothing calls the endpoint, and shows the cap banner", async () => {
    const f = flowGolden("flow-route-never");
    f.truncated = { nodes_omitted: 3, edges_omitted: 4, reasons: ["hop2_dropped"] };
    await renderFlow(f);
    const note = screen.getByTestId("flow-never");
    expect(note.textContent).toContain("No stored callers · No linked caller in production");
    // Below the pinned caption row, from the row's bottom.
    const row = screen.getByTestId("flow-captions");
    expect(Number.parseFloat(note.style.top)).toBe(Number.parseFloat(row.style.top) + Number.parseFloat(row.style.height) + CAPTION_ROW_GAP);
    expect(note.className).not.toContain("top-14");
    expect(screen.getByTestId("flow-truncated").textContent).toContain("hop 2 was left out");
  });

  it("switching 2 -> 1 -> 2 hops lays out once and moves no card or the view (T016i D3)", async () => {
    const f = fresh(flowGolden("flow-graph"), "01");
    let calls = 0;
    activeRunner = {
      layout: (g) => {
        calls++;
        return elk.layout(g);
      },
    };
    // Wide enough that the first layout's cap fits (one layout, T016i2 D2).
    await renderFlow(f, undefined, { width: 2000, height: 600 });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("16");
    });
    expect(calls).toBe(1);
    const before = positions();
    const view = viewportTransform();
    const canvas = screen.getByTestId("flow-canvas");
    fireEvent.click(card(f.endpoint.identity));
    card(f.endpoint.identity).focus();
    fireEvent.keyDown(card(f.endpoint.identity), { key: "1" });
    // Synchronously: no skeleton, the same canvas element.
    expect(screen.queryByText("Laying out the graph")).toBeNull();
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("12");
    });
    expect(screen.getByTestId("flow-canvas")).toBe(canvas);
    const one = positions();
    expect(one.size).toBe(12);
    for (const [id, t] of one) expect(t, id).toBe(before.get(id));
    expect(viewportTransform()).toBe(view);
    expect(document.activeElement).toBe(card(f.endpoint.identity));
    fireEvent.keyDown(card(f.endpoint.identity), { key: "2" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe("16");
    });
    expect(positions()).toEqual(before);
    expect(viewportTransform()).toBe(view);
    expect(calls).toBe(1);
  });

  it("with 1 hop, the pills and the overview count only drawn cards", async () => {
    const f = fresh(flowGolden("flow-graph"), "02");
    await renderFlow(f, undefined, { width: 600, height: 600 });
    const g1 = buildGraph(f, { hops: 1, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    expect(screen.queryByTestId("pan-pill-left")?.textContent ?? "").toContain("Hop-2 callers");
    fireEvent.keyDown(card(f.endpoint.identity), { key: "1" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe(String(g1.nodes.length));
    });
    expect(screen.queryByTestId("pan-pill-left")?.textContent ?? "").not.toContain("Hop-2");
    if (document.querySelector(".react-flow__minimap") === null) fireEvent.click(screen.getByTestId("map-toggle"));
    await waitFor(() => {
      expect(document.querySelectorAll(".react-flow__minimap-node")).toHaveLength(g1.nodes.length);
    });
  });

  it("caps wide cards, and shows the full texts in a tooltip and the accessible name", async () => {
    const f = fresh(flowGolden("flow-route"), "03");
    const long = "src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts";
    for (const n of f.nodes) if (n.node.kind === "client_call") n.node.file = long;
    await renderFlow(f);
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const cut = document.querySelector<HTMLElement>('[data-flow-cut="true"]');
    if (cut === null) throw new Error("no cut card");
    const id = cut.getAttribute("data-flow-card") ?? "";
    const node = g.nodes.find((n) => n.id === id);
    if (node === undefined) throw new Error(id);
    // Drawn cut, kept whole in the name.
    expect(within(cut).getByText((_, el) => el?.getAttribute("data-flow-text") === "sub").textContent).toContain("\u2026");
    expect(cut.getAttribute("aria-label")).toContain(node.sub);
    expect(node.sub.startsWith(long)).toBe(true);
    const wrapper = cut.closest<HTMLElement>(".react-flow__node");
    expect(Number.parseFloat(wrapper?.style.width ?? "0")).toBeLessThanOrEqual(360);
    act(() => {
      cut.focus();
    });
    const tips = await screen.findAllByTestId("card-tooltip");
    expect(tips[0]?.textContent).toContain(node.sub);
    expect(tips[0]?.textContent).toContain(node.title);
  });

  it("keeps column captions over their columns, clamped to the canvas edge, in a row pinned below the toolbar (T016i2 D1)", async () => {
    const f = flowGolden("flow-graph");
    await renderFlow(f, undefined, { width: 600, height: 600 });
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const row = screen.getByTestId("flow-captions");
    expect(row.getAttribute("aria-hidden")).toBe("true");
    // jsdom measures no toolbar: the fallback top.
    expect(row.style.top).toBe(`${String(CAPTION_ROW.top)}px`);
    expect(row.style.height).toBe(`${String(CAPTION_ROW.height)}px`);
    expect(row.classList.contains("gv-flow-caption-row")).toBe(true);
    const check = () => {
      const vp = /translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)\s*scale\(([\d.]+)\)/.exec(viewportTransform());
      if (vp === null) throw new Error("no viewport");
      const view = { x: Number(vp[1]), y: Number(vp[2]), zoom: Number(vp[3]) };
      const caps = new Map([...document.querySelectorAll<HTMLElement>("[data-flow-caption]")].map((c) => [c.getAttribute("data-flow-caption") ?? "", c]));
      for (const col of new Set(g.nodes.map((n) => n.column))) {
        const text = COLUMNS[col] ?? "";
        const boxes = g.nodes
          .filter((n) => n.column === col)
          .map((n) => {
            const el = document.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(n.id)}"]`);
            const m = /translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)/.exec(el?.style.transform ?? "");
            return { x: Number(m?.[1]), w: Number.parseFloat(el?.style.width ?? "0") };
          });
        const left = Math.min(...boxes.map((b) => b.x));
        const right = Math.max(...boxes.map((b) => b.x + b.w));
        const place = placeCaption({ left, right, width: captionWidth(text) }, view);
        const c = caps.get(text);
        if (place.hidden) {
          expect(c, text).toBeUndefined();
          continue;
        }
        if (c === undefined) throw new Error(`no caption ${text}`);
        // Screen space: 12 px at zoom >= 1, scaled below; no vertical offset.
        const ct = /^translateX\((-?[\d.e+-]+)px\) scale\(([\d.e+-]+)\)$/.exec(c.style.transform);
        if (ct === null) throw new Error(`caption ${text} transform ${c.style.transform}`);
        expectCssNumber(Number(ct[1]), place.x, text);
        expectCssNumber(Number(ct[2]), Math.min(view.zoom, 1), text);
        // Never beyond its own column on screen.
        expect(place.x, text).toBeGreaterThanOrEqual(left * view.zoom + view.x - 0.01);
        expect(place.x + place.maxWidth, text).toBeLessThanOrEqual(right * view.zoom + view.x + 0.01);
        expect(Number.parseFloat(c.style.maxWidth) * place.scale, text).toBeCloseTo(place.maxWidth, 5);
        if (place.clipped) expect(c.getAttribute("title"), text).toBe(text);
        if (right * view.zoom + view.x > 8 + place.maxWidth) expect(place.x, text).toBeGreaterThanOrEqual(8);
      }
      return view;
    };
    const top = `${String(CAPTION_ROW.top)}px`;
    const start = check();
    expect(start.zoom).toBe(1);
    // Zooming in moves the captions sideways with their columns; the row stays.
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    await waitFor(() => {
      expect(viewportTransform()).not.toContain("scale(1)");
    });
    expect(check().zoom).toBeGreaterThan(1);
    expect(screen.getByTestId("flow-captions").style.top).toBe(top);
    // Zooming out below 1 scales the captions and keeps each in its column.
    for (let i = 0; i < 6; i++) fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    await waitFor(() => {
      expect(check().zoom).toBeLessThan(1);
    });
    expect(screen.getByTestId("flow-captions").style.top).toBe(top);
    // A vertical pan leaves the row where it is: zoomed in, the arrow keys
    // walk down the call-site column and the view follows (setCenter).
    for (let i = 0; i < 10; i++) fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    await waitFor(() => {
      expect(check().zoom).toBe(2);
    });
    const before = check();
    const column = g.nodes
      .filter((n) => n.column === COL.calls && n.type === "card")
      .map((n) => ({ id: n.id, y: Number(/translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)/.exec(document.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(n.id)}"]`)?.style.transform ?? "")?.[2]) }))
      .sort((a, b) => a.y - b.y);
    act(() => {
      card(column[0]?.id ?? "").focus();
    });
    for (const next of column.slice(1)) {
      fireEvent.keyDown(document.activeElement ?? document.body, { key: "ArrowDown" });
      await waitFor(() => {
        expect(document.activeElement).toBe(card(next.id));
      });
      if (check().y !== before.y) break;
    }
    expect(check().y).not.toBe(before.y);
    expect(screen.getByTestId("flow-captions").style.top).toBe(top);
  });

  it("with 1 hop, takes the captions from the drawn cards only (T016i2 D1.4)", async () => {
    const f = fresh(flowGolden("flow-graph"), "04");
    await renderFlow(f, undefined, { width: 600, height: 600 });
    const g1 = buildGraph(f, { hops: 1, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const g2 = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    expect(document.querySelectorAll("[data-flow-caption]")).toHaveLength(new Set(g2.nodes.map((n) => n.column)).size);
    fireEvent.keyDown(card(f.endpoint.identity), { key: "1" });
    await waitFor(() => {
      expect(screen.getByTestId("flow-canvas").getAttribute("data-nodes")).toBe(String(g1.nodes.length));
    });
    const texts = [...document.querySelectorAll<HTMLElement>("[data-flow-caption]")].map((c) => c.getAttribute("data-flow-caption"));
    const want = [...new Set(g1.nodes.map((n) => n.column))].sort((a, b) => a - b).map((c) => COLUMNS[c]);
    expect(texts).toEqual(want);
    expect(texts).not.toContain(COLUMNS[0]);
  });

  it("narrows the cards to the canvas width, to the cap the measured chain gives (T016i2 D2)", async () => {
    const f = fresh(flowGolden("flow-route"), "05");
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    // T041: the plain edge labels carry their number, so the label gaps are
    // about 80 px wider than before; the widths moved up by as much.
    const width = 1260;
    const first = cardCap(width, chainColumns(g), estimatedGap(g));
    const want = await layoutAtCap(g, runner, width, first);
    expect(want.cap).toBeGreaterThan(CARD_MIN_CAP);
    expect(want.cap).toBeLessThan(CARD_MAX_W);
    expect(fitsChain(g, want.layout, width)).toBe(true);
    let calls = 0;
    activeRunner = {
      layout: (x) => {
        calls++;
        return elk.layout(x);
      },
    };
    await renderFlow(f, undefined, { width, height: 600 });
    expect(calls).toBeLessThanOrEqual(2);
    // Every card at its size at that cap (a card grows past it only for
    // its fixed parts): never wider than the cap would allow.
    for (const n of g.nodes) {
      const el = document.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(n.id)}"]`);
      expect(Number.parseFloat(el?.style.width ?? "0"), n.id).toBe(cardSize(n, want.cap).width);
    }
  });

  it("lays out again when a narrower canvas no longer fits a cached layout of the same first cap", async () => {
    const f = fresh(flowGolden("flow-route"), "06");
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const [wide, narrow] = [1300, 1260]; // T041: +80 px, see above
    // Same first-pass cap, so the same base key.
    expect(cardCap(narrow, chainColumns(g), estimatedGap(g))).toBe(cardCap(wide, chainColumns(g), estimatedGap(g)));
    let calls = 0;
    activeRunner = {
      layout: (x) => {
        calls++;
        return elk.layout(x);
      },
    };
    const run = activeRunner;
    seen = newSeen();
    const { rerender } = render(<Harness flow={f} size={{ width: wide, height: 600 }} />);
    await waitFor(() => {
      expect(document.querySelectorAll("[data-flow-card]").length).toBeGreaterThan(0);
    });
    const widths = () => [...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => Number.parseFloat(n.style.width));
    const before = Math.max(...widths());
    const atWide = calls;
    activeRunner = run;
    rerender(<Harness flow={f} size={{ width: narrow, height: 600 }} />);
    await waitFor(() => {
      expect(Math.max(...widths())).toBeLessThan(before);
    });
    expect(calls).toBeGreaterThan(atWide);
    // The drawn chain now fits the narrower canvas.
    const boxes = new Map(
      [...document.querySelectorAll<HTMLElement>(".react-flow__node")].map((n) => {
        const m = /translate\((-?[\d.]+)px,\s*(-?[\d.]+)px\)/.exec(n.style.transform);
        return [n.getAttribute("data-id") ?? "", { x: Number(m?.[1]), y: Number(m?.[2]), width: Number.parseFloat(n.style.width), height: Number.parseFloat(n.style.height) }];
      }),
    );
    expect(fitsChain(g, { boxes, routes: new Map(), width: 0, height: 0 }, narrow)).toBe(true);
    activeRunner = runner;
  });

  it("lays out again only when the measured gaps change the cap", async () => {
    const g = buildGraph(flowGolden("flow-route"), { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    let calls = 0;
    const counting: LayoutRunner = {
      layout: (x) => {
        calls++;
        return elk.layout(x);
      },
    };
    const wide = await layoutAtCap(g, counting, 3000, CARD_MAX_W);
    expect(wide.cap).toBe(CARD_MAX_W);
    expect(calls).toBe(1);
    calls = 0;
    const wrong = await layoutAtCap(g, counting, 3000, CARD_MIN_CAP);
    expect(calls).toBe(2);
    expect(wrong.cap).toBe(CARD_MAX_W);

    // A wider second layout whose chain no longer fits leaves the first.
    let n = 0;
    const spreading: LayoutRunner = {
      layout: async (x) => {
        const out = await elk.layout(x);
        if (++n === 2) for (const c of out.children ?? []) c.x = (c.x ?? 0) * 10;
        return out;
      },
    };
    const kept = await layoutAtCap(g, spreading, 3000, CARD_MIN_CAP);
    expect(n).toBe(2);
    expect(kept.cap).toBe(CARD_MIN_CAP);
  });

  it("shows the error state with Retry when the layout fails", async () => {
    const f = flowGolden("flow-command");
    let calls = 0;
    const failing: LayoutRunner = {
      layout: (g) => {
        calls++;
        return calls === 1 ? Promise.reject(new Error("worker crashed")) : elk.layout(g);
      },
    };
    const graph = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    render(
      <div style={{ height: 600 }}>
        <FlowTab
          flow={f}
          graph={graph}
          layoutGraph={graph}
          options={{ hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) }}
          selectedNode={null}
          selectedEdge={null}
          peekOpen={false}
          onSelect={vi.fn()}
          onTogglePeek={vi.fn()}
          onHops={vi.fn()}
          onTiers={vi.fn()}
          onGap={vi.fn()}
          onCallers={vi.fn()}
          elk={failing}
          fallbackSize={{ width: 1200, height: 600 }}
        />
      </div>,
    );
    const alert = await screen.findByTestId("flow-error");
    expect(alert.textContent).toContain("worker crashed");
    await act(async () => {
      fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
      await Promise.resolve();
    });
    await screen.findByTestId("flow-canvas");
  });
});

describe("Flow tab Tables column (T026 AG-C3)", () => {
  it("draws the tables, the unknown and fold boxes, and links back to the Architecture chain", async () => {
    activeBack = "#/architecture?chain=x";
    const f = fresh(flowGolden("flow-tables"), "31");
    await renderFlow(f, { node: "table:api/models.py#item", edge: null });
    expect(screen.getByTestId("flow-back").getAttribute("href")).toBe("#/architecture?chain=x");
    const tables = screen.getByTestId("flow-tables");
    expect(tables.getAttribute("data-tables")).toBe("3");
    expect(tables.textContent).toContain("2 written");
    expect(card("table:api/models.py#item").textContent).toContain("Reads · 1.0, builds a query on · 0.9");
    expect(document.querySelector('[data-flow-state="may_also_use"]')?.textContent).toContain("may also use");
    expect(document.querySelector('[data-flow-state="fold"]')?.textContent).toContain("+ 2 at hop 3");
    expect(COLUMNS[COL.tables]).toBe("Tables");
    expect(document.querySelectorAll('[data-tier="access"]').length).toBe(4);
  });

  it("packs the Tables column into one list and routes each table line into its card", async () => {
    const f = flowGolden("flow-tables");
    const g = buildGraph(f, { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    const { layout } = await layoutAtCap(g, runner, 1400, CARD_MAX_W);
    const col = g.nodes.filter((n) => n.column === COL.tables).map((n) => ({ n, b: layout.boxes.get(n.id) }));
    const xs = new Set(col.map((c) => c.b?.x));
    expect(xs.size).toBe(1);
    const ys = col.map((c) => c.b).sort((a, b) => (a?.y ?? 0) - (b?.y ?? 0));
    for (let i = 1; i < ys.length; i++) expect((ys[i]?.y ?? 0) - ((ys[i - 1]?.y ?? 0) + (ys[i - 1]?.height ?? 0))).toBe(TABLE_GAP);
    // State boxes last.
    expect(col.filter((c) => c.n.type === "state").every((c) => (c.b?.y ?? 0) > Math.max(...col.filter((x) => x.n.type !== "state").map((x) => x.b?.y ?? 0)))).toBe(true);
    for (const e of g.edges.filter((x) => x.tier === "access")) {
      const end = layout.routes.get(e.id)?.points.at(-1);
      const b = layout.boxes.get(e.target);
      expect(end?.x).toBe(b?.x);
      expect(end?.y).toBe((b?.y ?? 0) + (b?.height ?? 0) / 2);
    }
  });
});
