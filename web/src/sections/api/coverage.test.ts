import { describe, expect, it } from "vitest";

import type { RootCounts } from "@/api/endpoints";
import { foldRoots, moreRootsLabel, rootLabel, sortRootsByCalls, type FoldInput } from "@/sections/api/coverage";

function root(name: string, total: number): RootCounts {
  return { root: name, manifest: null, total, by_status: { linked: total }, repository_by_status: null };
}

// Server order: "." first, then byte order.
const SERVER = [root(".", 30), root("a", 5), root("b", 12), root("c", 5), root("d", 12), root("e", 1)];

function input(over: Partial<FoldInput> = {}): FoldInput {
  const roots = sortRootsByCalls(SERVER);
  return {
    roots,
    widths: new Map(roots.map((r) => [r.root, 100])),
    moreWidth: () => 90,
    gap: 6,
    available: 1000,
    active: null,
    ...over,
  };
}

const names = (r: ReturnType<typeof foldRoots>) => (r.kind === "folded" ? r.shown.map((x) => x.root) : null);

describe("sortRootsByCalls", () => {
  it("orders by total, largest first, ties in the server's order", () => {
    expect(sortRootsByCalls(SERVER).map((r) => r.root)).toEqual([".", "b", "d", "a", "c", "e"]);
    // The input is not changed.
    expect(SERVER.map((r) => r.root)).toEqual([".", "a", "b", "c", "d", "e"]);
  });
});

describe("foldRoots", () => {
  it("does not fold when every chip fits on one line", () => {
    // 6 x 100 + 5 gaps = 630.
    expect(foldRoots(input({ available: 630 }))).toEqual({ kind: "fits" });
  });

  it("shows the largest prefix that fits with the +N chip", () => {
    // 3 x 100 + 90 + 3 gaps = 408 fits in 420; a fourth root needs 514.
    const r = foldRoots(input({ available: 420 }));
    expect(names(r)).toEqual([".", "b", "d"]);
    expect(r.kind === "folded" && r.hidden).toBe(3);
  });

  it("is the chip alone when not even one root fits", () => {
    const r = foldRoots(input({ available: 150 }));
    expect(names(r)).toEqual([]);
    expect(r.kind === "folded" && r.hidden).toBe(6);
  });

  it("never skips the next root for a smaller one", () => {
    const roots = sortRootsByCalls(SERVER);
    // "b" (second largest) is wide; "d", "a" behind it would fit, but the
    // prefix stops at "b".
    const widths = new Map(roots.map((r) => [r.root, r.root === "b" ? 400 : 50]));
    const r = foldRoots(input({ widths, available: 300 }));
    expect(names(r)).toEqual(["."]);
  });

  it("rounds widths up and the available width down", () => {
    const roots = sortRootsByCalls(SERVER);
    const widths = new Map(roots.map((r) => [r.root, 99.2]));
    // Rounded: 2 x 100 + 90 + 2 gaps = 302. 302.9 floors to 302: fits.
    expect(names(foldRoots(input({ widths, available: 302.9 })))).toEqual([".", "b"]);
    // 301.99 floors to 301: one root less.
    expect(names(foldRoots(input({ widths, available: 301.99 })))).toEqual(["."]);
    // The chip width is rounded up too: 89.1 counts as 90.
    expect(names(foldRoots(input({ widths, moreWidth: () => 89.1, available: 302 })))).toEqual([".", "b"]);
    // All six: 6 x 100 + 5 gaps = 630; 629.9 does not fit.
    expect(foldRoots(input({ widths, available: 629.9 })).kind).toBe("folded");
    expect(foldRoots(input({ widths, available: 630 })).kind).toBe("fits");
  });

  it("asks the chip's width for the N it will show", () => {
    const seen: number[] = [];
    foldRoots(
      input({
        available: 420,
        moreWidth: (n) => {
          seen.push(n);
          return 90;
        },
      }),
    );
    expect(seen).toContain(3);
    expect(Math.max(...seen)).toBeLessThanOrEqual(5);
  });

  it("pins the active root last when it is outside the prefix (DD3 A)", () => {
    // Room for 3 chips + "+N": with "e" pinned, two of the largest remain.
    const r = foldRoots(input({ available: 420, active: "e" }));
    expect(names(r)).toEqual([".", "b", "e"]);
    expect(r.kind === "folded" && r.hidden).toBe(3);
    // Even with no room for any root, the active chip is shown.
    expect(names(foldRoots(input({ available: 150, active: "e" })))).toEqual(["e"]);
  });

  it("keeps the active root in its place when it is inside the prefix", () => {
    const r = foldRoots(input({ available: 420, active: "b" }));
    expect(names(r)).toEqual([".", "b", "d"]);
  });

  it("ignores an active root that is not in the list", () => {
    expect(names(foldRoots(input({ available: 420, active: "gone" })))).toEqual([".", "b", "d"]);
  });
});

describe("moreRootsLabel", () => {
  it("says root for one", () => {
    expect(moreRootsLabel(1)).toBe("+1 more root");
    expect(moreRootsLabel(17)).toBe("+17 more roots");
  });
});

describe("rootLabel", () => {
  it("reads the repository root as (repository root), other roots as their path", () => {
    expect(rootLabel(".")).toBe("(repository root)");
    expect(rootLabel("web/packages/ui")).toBe("web/packages/ui");
  });
});
