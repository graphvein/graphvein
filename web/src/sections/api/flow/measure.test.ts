// Card widths and the middle ellipsis (T016i D2). jsdom has no canvas, so
// textWidth is the per-character estimate (title 8 px, sub 7.4 px, note
// 6.8 px, badge 7 px per character); the rules are what is under test.
import { describe, expect, it } from "vitest";

import { buildGraph, type GraphNode } from "@/sections/api/flow/graph";
import {
  CAP_STEP,
  CARD_MAX_W,
  CARD_MIN_CAP,
  CHAIN_PAD,
  ELLIPSIS,
  MIN_TEXT_ROOM,
  cardCap,
  cardLayout,
  cardSize,
  chainWidth,
  nameEnd,
  textWidth,
  truncateMiddle,
  truncateSub,
} from "@/sections/api/flow/measure";
import { FILTER_TIERS } from "@/sections/api/flow/tiers";
import { flowGolden } from "@/test/flowFixtures";

const chars = (n: number) => (s: string) => s.length <= n;

function card(over: Partial<GraphNode> = {}): GraphNode {
  return {
    id: "x",
    type: "card",
    members: ["x"],
    role: "callee",
    roles: ["callee"],
    hop: 1,
    column: 6,
    peekId: "x",
    node: null,
    title: "short",
    method: null,
    sub: "a.ts:1",
    badges: [],
    wrapper: null,
    changed: false,
    omitted: { callers: 0, callees: 0 },
    muted: false,
    focus: false,
    notes: [],
    step: "callee, hop 1",
    dim: false,
    ...over,
  };
}

describe("truncateMiddle", () => {
  it("leaves a text that fits unchanged", () => {
    expect(truncateMiddle("src/a.ts:12", chars(11))).toBe("src/a.ts:12");
  });

  it("keeps the tail from the last / and cuts the head from its end", () => {
    const path = "src/frontend/src/controllers/API/queries/flows/use-get-flow.ts:22";
    const out = truncateMiddle(path, chars(40));
    expect(out.endsWith("/use-get-flow.ts:22")).toBe(true);
    expect(out.length).toBe(40);
    expect(out).toBe(`${path.slice(0, 40 - 1 - "/use-get-flow.ts:22".length)}${ELLIPSIS}/use-get-flow.ts:22`);
    // Route templates and call expressions follow the same rule.
    expect(truncateMiddle("/api/v1/flows/{flow_id}/versions/{version_id}", chars(28))).toBe(`/api/v1/flows/${ELLIPSIS}/{version_id}`);
    expect(truncateMiddle("GET `${API}/flows/update`", chars(20)).endsWith("/update`")).toBe(true);
  });

  it("when the tail alone does not fit, keeps the longest end of it", () => {
    const out = truncateMiddle("src/a-very-long-file-name-indeed.ts:221", chars(14));
    expect(out).toBe(`${ELLIPSIS}indeed.ts:221`);
  });

  it("cuts a qualified name at its last dot", () => {
    expect(truncateMiddle("useGetDownloadFlows.getDownloadFlowsFn", chars(31), true)).toBe(`useGetDownl${ELLIPSIS}.getDownloadFlowsFn`);
    // Not a qualified name: plain halves.
    expect(truncateMiddle("useGetDownloadFlows.getDownloadFlowsFn", chars(11))).toBe(`useGe${ELLIPSIS}owsFn`);
  });

  it("cuts a plain text into halves, the head one longer when odd", () => {
    expect(truncateMiddle("abcdefghijklmnop", chars(8))).toBe(`abcd${ELLIPSIS}nop`);
    expect(truncateMiddle("abcdefghijklmnop", chars(9))).toBe(`abcd${ELLIPSIS}mnop`);
  });
});

describe("truncateSub", () => {
  it("cuts the name part first, down to 8 characters, then the path", () => {
    const sub = "GET flag?'/t016d-items':url · web/src/http.ts:12";
    expect(truncateSub(sub, chars(100))).toBe(sub);
    // Room enough once the name is cut.
    const a = truncateSub("fetchEverythingFromTheServer · web/a.ts:1", chars(30));
    expect(a.endsWith(" · web/a.ts:1")).toBe(true);
    expect(a.length).toBe(30);
    // Not enough: the name stops at 8 characters and the path is cut.
    const b = truncateSub("fetchEverythingFromTheServer · web/src/deeply/nested/a.ts:1", chars(30));
    const [name, path] = b.split(" · ");
    expect(name).toHaveLength(8);
    expect(path?.endsWith("/a.ts:1")).toBe(true);
    expect(b.length).toBeLessThanOrEqual(30);
  });
});

describe("cardLayout", () => {
  it("does not change a card under the cap", () => {
    const l = cardLayout(card());
    expect(l.truncated).toBe(false);
    expect(l.title).toBe("short");
    // The 150 px minimum.
    expect(l.width).toBe(150);
  });

  it("caps a wide card at CARD_MAX_W and cuts its texts to fit", () => {
    const c = card({
      title: "useGetDownloadFlows.getDownloadFlowsFn.withAVeryLongSuffix",
      sub: "src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts:25",
      notes: ["calls useGetFlow.someVeryLongFunctionNameThatIsWide (contains this call)"],
      node: { kind: "function" } as GraphNode["node"],
    });
    const l = cardLayout(c);
    expect(l.width).toBe(CARD_MAX_W);
    expect(l.truncated).toBe(true);
    const inner = CARD_MAX_W - 30;
    expect(textWidth(l.title, "title")).toBeLessThanOrEqual(inner);
    expect(textWidth(l.sub, "sub")).toBeLessThanOrEqual(inner);
    expect(textWidth(l.notes[0] ?? "", "note")).toBeLessThanOrEqual(inner);
    expect(l.sub.endsWith("/use-get-download-flows.ts:25")).toBe(true);
    expect(l.title).toContain(ELLIPSIS);
    // The full texts stay on the node.
    expect(c.sub).toBe("src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts:25");
    expect(cardSize(c)).toEqual({ width: l.width, height: l.height });
  });

  it("grows past the cap only for fixed parts plus 80 px of title", () => {
    const badges = Array.from({ length: 6 }, (_, i) => ({ text: `badge number ${String(i)}`, tone: "other" as const }));
    const c = card({ title: "a".repeat(60), method: "DELETE", badges });
    const l = cardLayout(c);
    // Fixed: method 6 x 7.4 + 7, six badges of 14 chars x 7 + 12 + 7.
    const fixed = Math.ceil(6 * 7.4) + 7 + 6 * (14 * 7 + 12 + 7);
    expect(l.width).toBe(fixed + MIN_TEXT_ROOM + 30);
    expect(l.width).toBeGreaterThan(CARD_MAX_W);
    expect(textWidth(l.title, "title")).toBeLessThanOrEqual(MIN_TEXT_ROOM);
    expect(l.title).toContain(ELLIPSIS);
  });

  it("keeps the sub without the call's name (D2.4) on every golden", () => {
    const g = buildGraph(flowGolden("flow-graph"), { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) });
    for (const n of g.nodes) expect(n.sub.includes(" · ")).toBe(false);
  });
});

describe("cardLayout at a smaller cap (T016i2 D2.3)", () => {
  const wide = () =>
    card({
      title: "useGetDownloadFlows.getDownloadFlowsFn.withAVeryLongSuffix",
      sub: "src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts:25",
      node: { kind: "function" } as GraphNode["node"],
    });

  it("cuts more and stays within the cap; the default cap is CARD_MAX_W", () => {
    const c = wide();
    const at360 = cardLayout(c);
    const at240 = cardLayout(c, CARD_MIN_CAP);
    expect(cardLayout(c, CARD_MAX_W)).toBe(at360);
    expect(at360.width).toBe(CARD_MAX_W);
    expect(at240.width).toBe(CARD_MIN_CAP);
    expect(at240.title.length).toBeLessThan(at360.title.length);
    expect(textWidth(at240.title, "title")).toBeLessThanOrEqual(CARD_MIN_CAP - 30);
    // The tail alone is too long at 240: its end stays (D2.3).
    expect(at240.sub.startsWith(ELLIPSIS)).toBe(true);
    expect(at240.sub.endsWith("flows.ts:25")).toBe(true);
    expect(cardSize(c, CARD_MIN_CAP)).toEqual({ width: at240.width, height: at240.height });
    expect(nameEnd(c, CARD_MIN_CAP)).toBeLessThan(nameEnd(c));
  });

  it("leaves a card under the smaller cap unchanged", () => {
    const c = card();
    expect(cardLayout(c, CARD_MIN_CAP)).toEqual(cardLayout(c));
  });
});

describe("cardCap (T016i2 D2.1)", () => {
  // Three chain columns of one wide card each (natural width over 360).
  const wideCard = (i: number) =>
    card({
      id: `w${String(i)}`,
      title: `someVeryLongQualifiedOwnerName${String(i)}.someVeryLongMethodName`,
      sub: "src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts:25",
      node: { kind: "function" } as GraphNode["node"],
    });
  const chain = () => [[wideCard(1)], [wideCard(2)], [wideCard(3)]];
  const gap = 300;

  it("is CARD_MAX_W on a canvas wide enough for the chain at that cap", () => {
    const cols = chain();
    const need = chainWidth(cols, gap, CARD_MAX_W) + 2 * CHAIN_PAD;
    expect(need).toBe(3 * CARD_MAX_W + gap + 2 * CHAIN_PAD);
    expect(cardCap(need, cols, gap)).toBe(CARD_MAX_W);
    expect(cardCap(1920, cols, gap)).toBe(CARD_MAX_W);
  });

  it("is CARD_MIN_CAP on a canvas too narrow even for the floor", () => {
    expect(cardCap(800, chain(), gap)).toBe(CARD_MIN_CAP);
    expect(cardCap(0, chain(), gap)).toBe(CARD_MIN_CAP);
  });

  it("is otherwise the largest step from CARD_MIN_CAP that fits", () => {
    const cols = chain();
    // Room for 3 x 300 exactly.
    const width = 3 * 300 + gap + 2 * CHAIN_PAD;
    const cap = cardCap(width, cols, gap);
    expect((cap - CARD_MIN_CAP) % CAP_STEP).toBe(0);
    expect(cap).toBe(296);
    expect(chainWidth(cols, gap, cap) + 2 * CHAIN_PAD).toBeLessThanOrEqual(width);
    expect(chainWidth(cols, gap, cap + CAP_STEP) + 2 * CHAIN_PAD).toBeGreaterThan(width);
  });

  it("never shrinks as the canvas widens", () => {
    const cols = chain();
    let last = 0;
    for (let w = 600; w <= 2000; w += 7) {
      const cap = cardCap(w, cols, gap);
      expect(cap).toBeGreaterThanOrEqual(last);
      expect(cap).toBeGreaterThanOrEqual(CARD_MIN_CAP);
      expect(cap).toBeLessThanOrEqual(CARD_MAX_W);
      last = cap;
    }
  });

  it("counts only the columns given, their widest card, and the gaps", () => {
    const narrow = card({ id: "n", title: "short" });
    // Endpoint and handler only; a narrow card in a column with a wide one
    // does not lower that column's width.
    const two = [[wideCard(1), narrow], [wideCard(2)]];
    expect(chainWidth(two, 100, 300)).toBe(300 + 300 + 100);
    expect(cardCap(2 * 360 + 100 + 2 * CHAIN_PAD, two, 100)).toBe(CARD_MAX_W);
    expect(cardCap(2 * 360 + 100 + 2 * CHAIN_PAD, two, 101)).toBe(352);
    // Narrow cards never need a smaller cap.
    expect(cardCap(500, [[narrow]], 0)).toBe(CARD_MAX_W);
  });

  it("counts a card grown past the cap by its fixed parts at its grown width", () => {
    const badges = Array.from({ length: 6 }, (_, i) => ({ text: `badge number ${String(i)}`, tone: "other" as const }));
    const grown = card({ title: "a".repeat(60), method: "DELETE", badges });
    const w = cardLayout(grown, CARD_MIN_CAP).width;
    expect(w).toBeGreaterThan(CARD_MAX_W);
    expect(chainWidth([[grown]], 0, CARD_MIN_CAP)).toBe(w);
    expect(cardCap(w + 2 * CHAIN_PAD - 1, [[grown]], 0)).toBe(CARD_MIN_CAP);
  });
});
