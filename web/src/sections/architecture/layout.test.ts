// The map's ELK input and reader, against elkjs's real layered algorithm
// (its in-process worker build, as the Flow tab's test).
import { describe, expect, it } from "vitest";

import { ElkClient, type WorkerLike } from "@/sections/api/flow/elk";
import { CARD_W, EXPANDED_W, NARROW_W, LIST_ROWS_H, elkGraph, expandedHeight, onLine, overlaps, readLayout } from "@/sections/architecture/layout";
import { BAND_ID, DEFAULT_SHOW, buildMap } from "@/sections/architecture/model";
import { LIST_ROWS } from "@/sections/architecture/rows";
import { archGolden } from "@/test/architectureFixtures";

const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };

function realClient(): ElkClient {
  return new ElkClient(() => {
    const w = new workerModule.default.Worker() as WorkerLike;
    w.terminate = () => undefined;
    return w;
  });
}

describe("map layout", () => {
  it("places every card and line, chips off the cards", async () => {
    for (const show of [DEFAULT_SHOW, { ...DEFAULT_SHOW, libraries: true, tests: true, docs: true, configured: true }]) {
      const m = buildMap(archGolden("components"), show, null);
      const placed = readLayout(m, await realClient().layout(elkGraph(m, null, (f) => f)));
      expect(placed.boxes.size).toBe(m.nodes.length);
      expect(placed.routes.size).toBe(m.edges.length);
      const boxes = [...placed.boxes.values()];
      for (const [id, r] of placed.routes) {
        if (r.chip === null) continue;
        for (const b of boxes) expect(overlaps(r.chip, b), id).toBe(false);
      }
      // Cards never overlap each other (the band holds its chips).
      const cards = [...placed.boxes].filter(([id]) => id !== BAND_ID).map(([, b]) => b);
      for (let i = 0; i < cards.length; i++) for (let j = i + 1; j < cards.length; j++) expect(overlaps(cards[i] as never, cards[j] as never)).toBe(false);
    }
  });

  it("lays out an expanded component at its list height", async () => {
    const m = buildMap(archGolden("synthetic"), DEFAULT_SHOW, "svc");
    const placed = readLayout(m, await realClient().layout(elkGraph(m, 40, (f) => f)));
    expect(placed.boxes.get("c:svc")).toMatchObject({ width: EXPANDED_W, height: expandedHeight(40) });
    const ui = m.nodes.find((n) => n.id === "c:ui");
    expect(placed.boxes.get("c:ui")?.width).toBe(ui?.type === "component" && ui.narrow ? NARROW_W : CARD_W);
    expect(expandedHeight(40)).toBe(expandedHeight(LIST_ROWS + 1));
    expect(expandedHeight(40)).toBeGreaterThan(LIST_ROWS_H);
    expect(expandedHeight(3)).toBeLessThan(expandedHeight(LIST_ROWS));
  });

  it("refuses a partial answer", () => {
    const m = buildMap(archGolden("synthetic"), DEFAULT_SHOW, null);
    expect(() => readLayout(m, { id: "root", children: [], edges: [] })).toThrow(/no position/);
  });

  it("centres a chip on the nearest segment of its line", () => {
    const pts = [
      { x: 100, y: 0 },
      { x: 100, y: 200 },
    ];
    expect(onLine(pts, { x: 10, y: 90, width: 60, height: 20 })).toEqual({ x: 70, y: 90, width: 60, height: 20 });
  });
});
