// The Overview diagram and the compact map against elkjs's real worker
// code (its in-process build, as flow/elk.test.ts), so the layout runs.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ElkClient, type WorkerLike } from "@/sections/api/flow/elk";
import MiniMap from "@/sections/schema/MiniMap";
import OverviewTab, { EDGE_LABEL, FIT_INSET } from "@/sections/schema/OverviewTab";
import { setsGolden } from "@/test/schemaFixtures";

const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };

function elk(): ElkClient {
  return new ElkClient(() => {
    const w = new workerModule.default.Worker() as WorkerLike;
    w.terminate = () => undefined;
    return w;
  });
}

const group = () => {
  const g = setsGolden("states").sets[0];
  if (g === undefined) throw new Error("golden changed");
  return g;
};

describe("OverviewTab", () => {
  it("starts and fits the diagram below the zoom toolbar, never under it (Review fixes R2)", async () => {
    const { container } = render(<OverviewTab group={group()} run="r2" selected={null} onSelect={vi.fn()} panel={null} onClosePanel={vi.fn()} elk={elk()} />);
    await screen.findByTestId("er-canvas", undefined, { timeout: 5000 });
    // The toolbar sits 12 px from the top (top-3) with 32 px buttons (size-8).
    const toolbar = screen.getByRole("toolbar", { name: "Zoom" });
    expect(toolbar.className).toContain("top-3");
    for (const b of within(toolbar).getAllByRole("button")) expect(b.className).toContain("size-8");
    expect(FIT_INSET.y).toBeGreaterThanOrEqual(12 + 32 + 8);
    // React Flow renders the node elements after the canvas: wait until
    // every table node is in the DOM before measuring, else the minimum
    // over no nodes is Infinity.
    await waitFor(() => { expect(container.querySelectorAll(".react-flow__node")).toHaveLength(group().tables.length); }, { timeout: 5000 });
    fireEvent.click(screen.getByRole("button", { name: "Fit" }));
    // The top node row lands at FIT_INSET.y on screen: viewport offset
    // plus the smallest node y.
    const ty = (el: Element | null) => Number(/translate\([-\d.]+px, ?([-\d.]+)px\)/.exec((el as HTMLElement | null)?.style.transform ?? "")?.[1]);
    const nodeTop = Math.min(...[...container.querySelectorAll(".react-flow__node")].map(ty));
    expect(ty(container.querySelector(".react-flow__viewport")) + nodeTop).toBe(FIT_INSET.y);
  });

  it("draws every table, edges only from foreign_keys, with labelled zoom controls and the legend", async () => {
    const onSelect = vi.fn();
    const onClose = vi.fn();
    const g = group();
    const { rerender } = render(<OverviewTab group={g} run="r" selected={null} onSelect={onSelect} panel={null} onClosePanel={onClose} elk={elk()} />);
    const canvas = await screen.findByTestId("er-canvas", undefined, { timeout: 5000 });
    expect(canvas.getAttribute("data-nodes")).toBe(String(g.tables.length));
    expect(canvas.getAttribute("data-edges")).toBe("2");
    for (const [name, key] of [["Zoom in", "+"], ["Zoom out", "-"]] as const) {
      expect(screen.getByRole("button", { name }).getAttribute("aria-keyshortcuts")).toBe(key);
    }
    expect(screen.getByRole("button", { name: "Fit" }).getAttribute("aria-keyshortcuts")).toBe("0");
    expect(screen.getByTestId("er-legend").textContent).toContain("Nullability, defaults, indexes and history are in the table panel.");
    // The legend is a strip under the canvas, not laid over the nodes.
    expect(screen.getByTestId("er-legend").parentElement).toBe(canvas);
    expect(screen.getByTestId("er-legend").className).not.toMatch(/absolute/);
    // React Flow renders the nodes after the canvas (wait for them).
    const partial = (await screen.findAllByTestId("er-node")).find((n) => n.getAttribute("data-table") === "t_partial_empty");
    expect(partial?.textContent).toContain("partial");
    fireEvent.click(partial as HTMLElement);
    expect(onSelect).toHaveBeenCalledWith("table:a/migrations/2_more.sql#t_partial_empty");
    rerender(
      <OverviewTab group={g} run="r" selected="table:a/migrations/2_more.sql#t_partial_empty" onSelect={onSelect} panel={<button type="button">Close the table panel</button>} onClosePanel={onClose} elk={elk()} />,
    );
    const panel = screen.getByTestId("er-panel");
    expect(within(panel).getByRole("button", { name: "Close the table panel" })).toBeTruthy();
    fireEvent.keyDown(screen.getByTestId("er-canvas"), { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });
});

describe("MiniMap", () => {
  it("lays out boxes as buttons; partial and unknown say so", async () => {
    const onSelect = vi.fn();
    const g = group();
    render(<MiniMap group={g} run="r2" selected={null} onSelect={onSelect} onOverview={vi.fn()} elk={elk()} />);
    const boxes = await screen.findAllByTestId("map-box", undefined, { timeout: 5000 });
    expect(boxes).toHaveLength(g.tables.length);
    expect(boxes.find((b) => b.textContent.startsWith("t_weird"))?.textContent).toBe("t_weirdunknown");
    fireEvent.click(boxes[0] as HTMLElement);
    expect(onSelect).toHaveBeenCalled();
    expect(screen.getByTestId("map-header").textContent).toBe("8 tables · 4 FK pairs");
  });
});

describe("Overview text size", () => {
  it("edge labels are at least 12 px (React Flow's default is 10 px)", async () => {
    expect(EDGE_LABEL.fontSize).toBeGreaterThanOrEqual(12);
    // A table pair with two foreign keys gets a "2 pairs" label.
    const g = group();
    const fk = g.foreign_keys[0];
    if (fk === undefined) throw new Error("golden changed");
    const twice = { ...g, foreign_keys: [...g.foreign_keys, { ...fk, source_column: `${fk.source_column}#2` }] };
    render(<OverviewTab group={twice} run="r3" selected={null} onSelect={vi.fn()} panel={null} onClosePanel={vi.fn()} elk={elk()} />);
    await screen.findByTestId("er-canvas", undefined, { timeout: 5000 });
    const labels = [...document.querySelectorAll<SVGTextElement>(".react-flow__edge-text")];
    for (const l of labels) expect(parseFloat(l.style.fontSize)).toBeGreaterThanOrEqual(12);
  });
});
