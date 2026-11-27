// The libraries band (T067, design record T062 variant A) on the pdfcraft
// and owncast answers of the design round, reduced to names, directories
// and link counts (web/src/test/libraryBand.json).
import { describe, expect, it } from "vitest";

import type { Component, Link } from "@/api/architecture";
import { ElkClient, type WorkerLike } from "@/sections/api/flow/elk";
import { rowText } from "@/sections/architecture/Cards";
import { BUS_MIN_GAP, NARROW_W, elkGraph, overlaps, readLayout } from "@/sections/architecture/layout";
import { BAND_ID, BAND_ROW_CAP, DEFAULT_SHOW, MORE_ID, bandHoverNodes, bandLevels, buildMap, focusOf, selectionMarks, sharedPrefix, type MapNode } from "@/sections/architecture/model";
import { bandAnswer } from "@/test/architectureFixtures";

const workerModule = (await import("elkjs/lib/elk-worker.js")) as unknown as { default: { Worker: new () => Omit<WorkerLike, "terminate"> } };

function elk(): ElkClient {
  return new ElkClient(() => {
    const w = new workerModule.default.Worker() as WorkerLike;
    w.terminate = () => undefined;
    return w;
  });
}

const band = (nodes: readonly MapNode[]) => {
  const b = nodes.find((n) => n.type === "band");
  if (b?.type !== "band") throw new Error("no band");
  return b.band;
};
const display = (nodes: readonly MapNode[], ids: readonly string[]) => ids.map((id) => nodes.find((n) => n.id === id)).map((n) => (n?.type === "library" ? n.display : "?"));

describe("libraries band model", () => {
  it("pdfcraft: 10 narrow cards, 34 chips in rows, 18 card lines, 109 links counted", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    expect(m.cards).toBe(10);
    expect(m.nodes.every((n) => n.type !== "component" || n.narrow)).toBe(true);
    const b = band(m.nodes);
    expect(b).toMatchObject({ libraries: 34, between: 109, cardLines: 18, unlinked: 4, prefix: "pdfcraft-", dirs: false, folded: 0 });
    expect(b.rows.map((r) => [r.kind, r.level, r.total])).toEqual([
      ["card", 0, 13],
      ["level", 1, 11],
      ["level", 2, 3],
      ["level", 3, 3],
      ["none", 0, 4],
    ]);
    expect(display(m.nodes, b.rows[4]?.members ?? [])).toEqual(["content", "cos", "crypt", "filters"]);
    // Levels: by components using it, most first.
    expect(display(m.nodes, b.rows[3]?.members ?? [])).toEqual(["model", "fonts", "geom"]);
    // Every card-library pair is a line; library pairs are lines too,
    // drawn while one end is selected.
    expect(m.edges.filter((e) => e.band === "card")).toHaveLength(18);
    expect(m.edges.filter((e) => e.band === "between").reduce((a, e) => a + e.links.length, 0)).toBe(109);
    expect(m.edges.filter((e) => e.band === undefined)).toHaveLength(0);
  });

  it("pdfcraft: each card's line chip sums its library links (the audit's 82)", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    const fans = m.nodes.flatMap((n) => (n.type === "component" && n.fan !== null ? [[n.component.name, n.fan.count, n.fan.libraries] as const] : []));
    expect(Object.fromEntries(fans.map(([n, c, l]) => [n, `${String(c)}/${String(l)}`]))).toEqual({
      "pdfcraft-cli": "34/8",
      pdfcraft: "25/4",
      "pdfcraft-web": "9/1",
      xtask: "6/1",
      "pdfcraft-ocr-worker": "3/1",
      "pdfcraft-js-fuzz": "2/1",
      "pdfcraft-preflight-fuzz": "2/1",
      "pdfcraft-redact-fuzz": "1/1",
    });
    expect(fans.reduce((a, [, c]) => a + c, 0)).toBe(82);
  });

  it("pdfcraft: chip numbers are the components using each library", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    const users = Object.fromEntries(m.nodes.flatMap((n) => (n.type === "library" ? [[n.display, n.users]] : [])));
    expect(users).toMatchObject({ model: 15, fonts: 13, annot: 9, engine: 4, content: 0, oracle: 1 });
  });

  it("pdfcraft: a selected library lights its users and what it uses (A2), a card its libraries (A3)", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    const f = focusOf(m, { node: "lib:crates/engine", edge: null });
    const lit = [...(f?.nodes ?? [])];
    expect(lit.filter((id) => id.startsWith("c:")).sort()).toEqual(["c:apps/pdfcraft", "c:apps/pdfcraft-cli"]);
    expect(lit.filter((id) => id.startsWith("lib:"))).toHaveLength(1 + 2 + 24);
    const card = focusOf(m, { node: "c:apps/pdfcraft-cli", edge: null });
    expect([...(card?.nodes ?? [])].filter((id) => id.startsWith("lib:"))).toHaveLength(8);
  });

  it("pdfcraft: a selected library or card marks its neighbours with the stored counts by direction (T072)", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    const lib = selectionMarks(m, "lib:crates/engine");
    const f = focusOf(m, { node: "lib:crates/engine", edge: null });
    // Every lit neighbour carries a mark, nothing else does.
    expect(new Set(lib.keys())).toEqual(new Set([...(f?.nodes ?? [])].filter((id) => id !== "lib:crates/engine")));
    const users = [...lib].filter(([, k]) => k.usedBy > 0).map(([id]) => id);
    const uses = [...lib].filter(([, k]) => k.uses > 0).map(([id]) => id);
    expect(users.sort()).toEqual(["c:apps/pdfcraft", "c:apps/pdfcraft-cli", "lib:crates/automation", "lib:crates/ui-egui"]);
    expect(lib.get("lib:crates/ui-egui")).toEqual({ uses: 0, usedBy: 377 });
    expect(uses).toHaveLength(24);
    // The counts are the stored links' counts, summed per pair.
    const r = bandAnswer("pdfcraft");
    const xfa = r.links.filter((l) => (l.source === "crates/engine" && l.target === "crates/xfa") || (l.target === "crates/engine" && l.source === "crates/xfa"));
    expect(lib.get("lib:crates/xfa")?.uses).toBe(xfa.reduce((a, l) => a + (l.source === "crates/engine" ? l.count : l.back_count), 0));
    // A card marks the libraries it uses; a line selected alone marks nothing.
    const card = selectionMarks(m, "c:apps/pdfcraft-cli");
    expect(card.size).toBe(8);
    expect([...card.values()].every((k) => k.uses > 0 && k.usedBy === 0)).toBe(true);
    expect(selectionMarks(m, null).size).toBe(0);
    const edge = m.edges.find((e) => e.band === "card");
    expect(selectionMarks(m, edge?.id ?? "").size).toBe(0);
  });

  it("pdfcraft: the hovered card lights its band lines' other ends only", () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    expect(bandHoverNodes(m, "c:apps/pdfcraft-cli").size).toBe(9);
    expect(bandHoverNodes(m, null).size).toBe(0);
    // A library's library links are not band lines a hover lights.
    expect([...bandHoverNodes(m, "lib:crates/engine")].sort()).toEqual(["c:apps/pdfcraft", "c:apps/pdfcraft-cli", "lib:crates/engine"]);
  });

  it("owncast: two wide cards, ten chips with directories under No stored link (P1 unchanged)", () => {
    const m = buildMap(bandAnswer("owncast"), DEFAULT_SHOW, null);
    expect(m.cards).toBe(2);
    expect(m.nodes.every((n) => n.type !== "component" || (!n.narrow && n.fan === null))).toBe(true);
    const b = band(m.nodes);
    expect(b).toMatchObject({ libraries: 10, between: 0, cardLines: 0, unlinked: 10, prefix: null, dirs: true });
    expect(b.rows.map((r) => r.kind)).toEqual(["none"]);
    expect(rowText(b.rows[0] ?? { kind: "none", level: 0, members: [], total: 0 }).label).toBe("No stored link");
    expect(m.edges.map((e) => e.band ?? "card-card")).toEqual(["card-card"]);
  });

  it("never words a library unused", () => {
    for (const kind of ["card", "level", "nouser", "none"] as const) expect(JSON.stringify(rowText({ kind, level: 1, members: [], total: 0 }))).not.toMatch(/unused/i);
  });

  it("folds a row past the cap into one + n more chip, and opens it", () => {
    const r = bandAnswer("pdfcraft");
    // 40 more libraries no link touches: the No stored link row is capped.
    for (let i = 0; i < 40; i++) r.components.push({ ...structuredClone(r.components[0] as Component), id: `extra/${String(i)}`, name: `pdfcraft-extra${String(i)}`, directory: `extra/${String(i)}`, visibility: "folded" });
    const m = buildMap(r, DEFAULT_SHOW, null);
    const b = band(m.nodes);
    expect(b.folded).toBe(44 - BAND_ROW_CAP);
    expect(m.nodes.find((n) => n.id === MORE_ID)).toMatchObject({ count: 44 - BAND_ROW_CAP, expanded: false });
    const all = buildMap(r, DEFAULT_SHOW, null, { bandAll: true });
    expect(band(all.nodes)).toMatchObject({ folded: 0, expanded: true });
    expect(all.nodes.filter((n) => n.type === "library")).toHaveLength(74);
  });
});

describe("band rows", () => {
  const lib = (id: string): Component => ({ ...structuredClone(bandAnswer("owncast").components[2] as Component), id, name: id, directory: id });
  const link = (s: string, t: string): Link => ({ ...structuredClone(bandAnswer("owncast").links[0] as Link), source: s, target: t });

  it("puts a library below every library using it; a cycle stops at its first repeat", () => {
    const libs = ["a", "b", "c", "d", "e"].map(lib);
    const links = [link("card", "a"), link("a", "b"), link("b", "c"), link("c", "b"), link("a", "c"), link("d", "a")];
    const lv = bandLevels(libs, links, new Set(["card"]));
    // b ⇄ c: the walk from a reaches b first, so c → b is the link back.
    expect(Object.fromEntries(lv)).toEqual({ a: 0, b: 1, c: 2, d: -1, e: -2 });
  });

  it("drops a shared prefix only when three or more names share it", () => {
    expect(sharedPrefix(["pdfcraft-a", "pdfcraft-b", "pdfcraft-c"])).toBe("pdfcraft-");
    expect(sharedPrefix(["pdfcraft-a", "pdfcraft-b"])).toBeNull();
    expect(sharedPrefix(["lib_x", "lib_y", "libz"])).toBeNull();
    expect(sharedPrefix(["a-b-c", "a-b-d", "a-x"])).toBe("a-");
  });
});

describe("band layout", () => {
  it("pdfcraft: the band sits below the cards, chips never overlap, lines miss the cards", async () => {
    const m = buildMap(bandAnswer("pdfcraft"), DEFAULT_SHOW, null);
    const placed = readLayout(m, await elk().layout(elkGraph(m, null, (f) => f)));
    expect(placed.boxes.size).toBe(m.nodes.length);
    expect(placed.routes.size).toBe(m.edges.length);
    const bandBox = placed.boxes.get(BAND_ID);
    if (bandBox === undefined) throw new Error("no band box");
    const cards = m.nodes.filter((n) => n.type === "component").map((n) => placed.boxes.get(n.id));
    for (const c of cards) {
      expect(c?.width).toBe(NARROW_W);
      expect((c?.y ?? 0) + (c?.height ?? 0)).toBeLessThanOrEqual(bandBox.y - BUS_MIN_GAP);
    }
    const chips = m.nodes.filter((n) => n.type === "library").map((n) => placed.boxes.get(n.id));
    for (const c of chips) expect(c !== undefined && c.x >= bandBox.x && c.x + c.width <= bandBox.x + bandBox.width && c.y >= bandBox.y && c.y + c.height <= bandBox.y + bandBox.height).toBe(true);
    for (let i = 0; i < chips.length; i++) for (let j = i + 1; j < chips.length; j++) expect(overlaps(chips[i] as never, chips[j] as never)).toBe(false);
    for (const e of m.edges.filter((x) => x.band === "card")) {
      const pts = placed.routes.get(e.id)?.points ?? [];
      const chip = placed.boxes.get(e.target);
      // Into the chip's top or side (the bus, T072).
      const end = pts.at(-1);
      expect(end !== undefined && chip !== undefined && end.x >= chip.x && end.x <= chip.x + chip.width && end.y >= chip.y && end.y <= chip.y + chip.height).toBe(true);
      // No segment crosses another card.
      for (let i = 1; i < pts.length; i++) {
        const p = pts[i - 1];
        const q = pts[i];
        if (p === undefined || q === undefined) continue;
        const seg = { x: Math.min(p.x, q.x), y: Math.min(p.y, q.y), width: Math.abs(p.x - q.x), height: Math.abs(p.y - q.y) };
        for (const n of m.nodes.filter((x) => x.type === "component" && x.id !== e.source)) expect(overlaps(seg, placed.boxes.get(n.id) as never), `${e.id} crosses ${n.id}`).toBe(false);
      }
    }
    // The card row sits under its users: the first chip's users are left of the last chip's.
  });
});
