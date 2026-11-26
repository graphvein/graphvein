import { afterEach, describe, expect, it, vi } from "vitest";

import { BUILT_IN_DEPTH, DEPTHS, DEPTH_KEYS, MAX_DEPTH, parseDepth, readDepth, writeDepth } from "@/lib/depthChoice";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("depth choice (T042)", () => {
  it("offers 1 to 10 with the built-in defaults 3 and 6", () => {
    expect(MAX_DEPTH).toBe(10);
    expect(DEPTHS).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    expect(BUILT_IN_DEPTH).toEqual({ impact: 3, chain: 6 });
    expect(DEPTH_KEYS).toEqual({ impact: "graphvein.impact-depth.v1", chain: "graphvein.chain-depth.v1" });
  });

  it("reads only an integer 1 to 10", () => {
    expect(parseDepth("1")).toBe(1);
    expect(parseDepth("10")).toBe(10);
    expect(parseDepth(7)).toBe(7);
    for (const bad of [null, undefined, "", "0", "11", "-1", "6.5", " 6", "06x", 6.5, 0, 11, "abc"]) expect(parseDepth(bad), String(bad)).toBeNull();
  });

  it("stores per walk", () => {
    expect(readDepth("impact")).toEqual({ depth: null, saved: true });
    expect(writeDepth("impact", 8)).toBe(true);
    expect(readDepth("impact")).toEqual({ depth: 8, saved: true });
    expect(readDepth("chain")).toEqual({ depth: null, saved: true });
    window.localStorage.setItem(DEPTH_KEYS.chain, "eleven");
    expect(readDepth("chain").depth).toBeNull();
  });

  it("blocked storage keeps the choice for the page", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(writeDepth("chain", 9)).toBe(false);
    expect(readDepth("chain")).toEqual({ depth: 9, saved: false });
  });
});
