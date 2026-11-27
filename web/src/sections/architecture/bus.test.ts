// The bus routing of the band's card-library lines (T072, the T071 review's
// recommendation V1) on the pdfcraft answer of the T062 design round and
// on answers built from it for the cases the review named for hardening:
// the "+ n more libraries" chip as a target, a card lane of three rows, a
// card row of one line (no inner bus) and channels with more than four
// cards.
import { describe, expect, it } from "vitest";

import type { ArchitectureResponse, Component, Link } from "@/api/architecture";
import { ElkClient, type WorkerLike } from "@/sections/api/flow/elk";
import { BAND_GAP, BUS_GAP_X, BUS_PAD, BUS_PITCH, LIB_GAP, busGap, elkGraph, readLayout, trackLevels, type Box, type Placed } from "@/sections/architecture/layout";
import { BAND_ID, DEFAULT_SHOW, MORE_ID, buildMap, type MapModel } from "@/sections/architecture/model";
import { bandAnswer } from "@/test/architectureFixtures";

const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };

function elk(): ElkClient {
  return new ElkClient(() => {
    const w = new workerModule.default.Worker() as WorkerLike;
    w.terminate = () => undefined;
    return w;
  });
}

type Point = { x: number; y: number };

async function place(r: ArchitectureResponse): Promise<{ m: MapModel; placed: Placed }> {
  const m = buildMap(r, DEFAULT_SHOW, null);
  return { m, placed: readLayout(m, await elk().layout(elkGraph(m, null, (f) => f))) };
}

/** crossings counts the proper crossings of orthogonal routes: a vertical
 * segment of one line strictly through a horizontal segment of another.
 * Lines of one card share their trunk: a crossing point is one place,
 * however many lines of the two trunks pass it. */
function crossings(routes: readonly Point[][]): number {
  const segs = routes.map((pts) => pts.slice(1).map((q, i) => [pts[i] as Point, q] as const));
  const seen = new Set<string>();
  for (let i = 0; i < segs.length; i++) {
    for (let j = 0; j < segs.length; j++) {
      if (i === j) continue;
      for (const [a, b] of segs[i] ?? []) {
        if (Math.abs(a.x - b.x) > 0.01) continue;
        for (const [c, d] of segs[j] ?? []) {
          if (Math.abs(c.y - d.y) > 0.01) continue;
          const inX = a.x > Math.min(c.x, d.x) + 0.5 && a.x < Math.max(c.x, d.x) - 0.5;
          const inY = c.y > Math.min(a.y, b.y) + 0.5 && c.y < Math.max(a.y, b.y) - 0.5;
          if (inX && inY) seen.add(`${a.x.toFixed(1)},${c.y.toFixed(1)}`);
        }
      }
    }
  }
  return seen.size;
}

/** checkBus asserts the bus's invariants: every card-library line is
 * routed, orthogonal, starts at its card's line chip, ends on its chip's
 * border and passes under no card or chip that is not one of its ends;
 * its run above the band is its card's one track. Returns the routes. */
function checkBus(m: MapModel, placed: Placed): Point[][] {
  const card = m.edges.filter((e) => e.band === "card");
  const bandBox = placed.boxes.get(BAND_ID) as Box;
  const laneBottom = Math.max(...m.nodes.filter((n) => n.type === "component").map((n) => (placed.boxes.get(n.id) as Box).y + (placed.boxes.get(n.id) as Box).height));
  const solid = m.nodes.filter((n) => n.type === "component" || n.type === "library" || n.type === "more");
  const tracks = new Map<string, number>();
  const routes = card.map((e) => {
    const pts = placed.routes.get(e.id)?.points ?? [];
    expect(pts.length, e.id).toBeGreaterThan(1);
    for (let i = 1; i < pts.length; i++) expect(Math.abs((pts[i]?.x ?? 0) - (pts[i - 1]?.x ?? 0)) < 0.01 || Math.abs((pts[i]?.y ?? 0) - (pts[i - 1]?.y ?? 0)) < 0.01, `${e.id} orthogonal`).toBe(true);
    const a = placed.boxes.get(e.source) as Box;
    expect(pts[0]).toEqual({ x: a.x + a.width / 2, y: a.y + a.height });
    const b = placed.boxes.get(e.target) as Box;
    const end = pts.at(-1) as Point;
    const onTop = Math.abs(end.y - b.y) < 0.01 && end.x > b.x && end.x < b.x + b.width;
    const onSide = (Math.abs(end.x - b.x) < 0.01 || Math.abs(end.x - b.x - b.width) < 0.01) && end.y > b.y && end.y < b.y + b.height;
    expect(onTop || onSide, `${e.id} ends on its chip`).toBe(true);
    const ys = [...new Set(pts.filter((p) => p.y > laneBottom + 1 && p.y < bandBox.y).map((p) => p.y))];
    expect(ys.length, `${e.id} one track`).toBeLessThanOrEqual(1);
    const y = ys[0];
    if (y !== undefined) {
      expect(tracks.get(e.source) ?? y).toBe(y);
      tracks.set(e.source, y);
      expect(y).toBeGreaterThanOrEqual(laneBottom + BUS_PAD - 0.01);
      expect(y).toBeLessThanOrEqual(bandBox.y - BUS_PAD + 0.01);
    }
    for (const n of solid) {
      if (n.id === e.source || n.id === e.target) continue;
      const box = placed.boxes.get(n.id) as Box;
      for (let i = 1; i < pts.length; i++) {
        const p = pts[i - 1] as Point;
        const q = pts[i] as Point;
        const hit = Math.max(p.x, q.x) > box.x + 0.5 && Math.min(p.x, q.x) < box.x + box.width - 0.5 && Math.max(p.y, q.y) > box.y + 0.5 && Math.min(p.y, q.y) < box.y + box.height - 0.5;
        expect(hit, `${e.id} under ${n.id}`).toBe(false);
      }
    }
    return pts;
  });
  return routes;
}

const component = (r: ArchitectureResponse, id: string, folded: boolean): Component => ({ ...structuredClone(r.components[1] as Component), id, name: id.replaceAll("/", "-"), directory: id, visibility: folded ? "folded" : "shown" });
const link = (r: ArchitectureResponse, s: string, t: string, n = 1): Link => ({ ...structuredClone(r.links[0] as Link), id: `in_process:${s}->${t}`, source: s, target: t, count: n, back_count: 0, members: [] });

describe("bus routing (T072)", () => {
  it("pdfcraft: one track per card, 10 crossing points, no line under a chip or card it does not end at", async () => {
    const { m, placed } = await place(bandAnswer("pdfcraft"));
    expect(m.edges.filter((e) => e.band === "card")).toHaveLength(18);
    const bandBox = placed.boxes.get(BAND_ID) as Box;
    const lane = m.nodes.filter((n) => n.type === "component").map((n) => placed.boxes.get(n.id) as Box);
    const laneBottom = Math.max(...lane.map((b) => b.y + b.height));
    expect(bandBox.y - laneBottom).toBeLessThanOrEqual(busGap(8, BUS_PITCH));
    const routes = checkBus(m, placed);
    // Chips keep their grid with the wider column gap.
    const chips = m.nodes.filter((n) => n.type === "library").map((n) => placed.boxes.get(n.id) as Box);
    const xs = [...new Set(chips.map((b) => b.x))].sort((a, b) => a - b);
    expect((xs[1] ?? 0) - (xs[0] ?? 0)).toBe(120 + BUS_GAP_X);
    // The top chip line holds the libraries of the single-library cards:
    // each is entered straight from the bus.
    const top = Math.min(...chips.map((b) => b.y));
    for (const name of ["pdfcraft-web", "xtask", "pdfcraft-ocr-worker", "pdfcraft-js-fuzz"]) {
      const e = m.edges.find((x) => x.band === "card" && m.nodes.some((n) => n.id === x.source && n.type === "component" && n.component.name === name));
      expect(placed.boxes.get(e?.target ?? "")?.y, name).toBe(top);
    }
    // 9 crossing points (the T071 prototype measured 10, the band as T067
    // built it 21); every one is a right angle between two trunks.
    expect(crossings(routes)).toBe(9);
  });

  it("owncast: no card line, the band placed as before", async () => {
    const { m, placed } = await place(bandAnswer("owncast"));
    expect(m.edges.filter((e) => e.band !== undefined)).toHaveLength(0);
    const lane = m.nodes.filter((n) => n.type === "component").map((n) => placed.boxes.get(n.id) as Box);
    expect((placed.boxes.get(BAND_ID) as Box).y - Math.max(...lane.map((b) => b.y + b.height))).toBe(BAND_GAP);
    const xs = [...new Set(m.nodes.filter((n) => n.type === "library").map((n) => (placed.boxes.get(n.id) as Box).x))].sort((a, b) => a - b);
    expect((xs[1] ?? 0) - (xs[0] ?? 0)).toBe(230 + LIB_GAP);
  });

  it("a card row of one line: no inner bus, every chip entered from above", async () => {
    const r = bandAnswer("owncast");
    r.links.push(link(r, "web", "test/automated/api", 3), link(r, ".", "test/automated/api", 2), link(r, ".", "test/automated/auth", 1));
    const { m, placed } = await place(r);
    const routes = checkBus(m, placed);
    expect(routes).toHaveLength(3);
    for (const [i, e] of m.edges.filter((x) => x.band === "card").entries()) {
      const b = placed.boxes.get(e.target) as Box;
      expect(routes[i]?.at(-1)?.y, e.id).toBe(b.y);
    }
  });

  it("the + n more libraries chip as a target: one line from the card, entered from the side", async () => {
    const r = bandAnswer("pdfcraft");
    // 30 more libraries pdfcraft-cli uses: the card row passes its cap of
    // 24 and the rest fold into the "+ n more" chip at the band's end.
    for (let i = 0; i < 30; i++) {
      r.components.push(component(r, `extra/${String(i)}`, true));
      r.links.push(link(r, "apps/pdfcraft-cli", `extra/${String(i)}`));
    }
    const { m, placed } = await place(r);
    const more = m.edges.filter((e) => e.band === "card" && e.target === MORE_ID);
    expect(more).toHaveLength(1);
    checkBus(m, placed);
  });

  it("a card lane of three rows: trunks find a clear gutter", async () => {
    const r = bandAnswer("pdfcraft");
    // 14 more cards, each using one library and one other card's library.
    const libs = r.components.filter((c) => c.visibility === "folded").map((c) => c.id);
    for (let i = 0; i < 14; i++) {
      const id = `apps/extra${String(i)}`;
      r.components.push(component(r, id, false));
      r.links.push(link(r, id, libs[(3 * i) % libs.length] ?? ""), link(r, id, libs[(5 * i + 1) % libs.length] ?? ""));
    }
    const { m, placed } = await place(r);
    const rows = new Set(m.nodes.filter((n) => n.type === "component").map((n) => Math.round((placed.boxes.get(n.id) as Box).y)));
    expect(rows.size).toBeGreaterThanOrEqual(3);
    checkBus(m, placed);
  });

  it("channels with more than four cards stay inside their column gap", async () => {
    const r = bandAnswer("pdfcraft");
    // Six cards using the same 13 libraries the cards use: every lower
    // chip is entered by six lines.
    const used = [...new Set(r.links.filter((l) => r.components.some((c) => c.id === l.source && c.visibility !== "folded") && r.components.some((c) => c.id === l.target && c.visibility === "folded")).map((l) => l.target))];
    for (let i = 0; i < 6; i++) {
      const id = `apps/many${String(i)}`;
      r.components.push(component(r, id, false));
      for (const t of used) r.links.push(link(r, id, t));
    }
    const { m, placed } = await place(r);
    const routes = checkBus(m, placed);
    // A chip below the top line gets one port per card.
    const chips = m.nodes.filter((n) => n.type === "library").map((n) => placed.boxes.get(n.id) as Box);
    const top = Math.min(...chips.map((b) => b.y));
    const lower = m.edges.filter((e) => e.band === "card" && (placed.boxes.get(e.target) as Box).y > top);
    expect(lower.length).toBeGreaterThan(0);
    const ends = new Map<string, Set<string>>();
    lower.forEach((e) => {
      const p = routes[m.edges.filter((x) => x.band === "card").indexOf(e)]?.at(-1) as Point;
      ends.set(e.target, (ends.get(e.target) ?? new Set()).add(`${p.x.toFixed(2)},${p.y.toFixed(2)}`));
    });
    for (const [chip, set] of ends) expect(set.size, chip).toBe(lower.filter((e) => e.target === chip).length);
  });
});

describe("trackLevels", () => {
  it("shares a level between tracks whose spans are apart and orders overlapping ones to cross least", () => {
    // a and b are apart: one level.
    const apart = trackLevels([
      { key: "a", fx: 0, drops: [40] },
      { key: "b", fx: 200, drops: [260] },
    ]);
    expect(apart.get("a")).toBe(0);
    expect(apart.get("b")).toBe(0);
    // c's drop at 30 lies in a's span, a's in none of c's: c goes below a
    // (nearer to the chips), so neither crosses the other.
    const nested = trackLevels([
      { key: "a", fx: 0, drops: [40] },
      { key: "c", fx: 100, drops: [30] },
    ]);
    expect(nested.get("c")).toBeLessThan(nested.get("a") ?? 0);
  });
});
