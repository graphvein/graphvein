import { describe, expect, it } from "vitest";

import { DEFAULT_PARAMS, apiHash, isGraphView, parseApiParams, parseGap } from "@/sections/api/params";

describe("API section deep links", () => {
  it("parses every parameter", () => {
    const id = "route:server/app.ts#GET%20%2Ft016d-items";
    const hash = `#/api?transport=http&tests=1&root=src%2Ffrontend&sel=${encodeURIComponent(id)}&gap=http:dynamic`;
    expect(parseApiParams(hash)).toEqual({
      ...DEFAULT_PARAMS,
      transport: "http",
      tests: true,
      root: "src/frontend",
      sel: id,
      gap: { transport: "http", category: "dynamic" },
    });
    expect(parseApiParams(`#/api?sel=x&tab=flow&hops=1&nm=0&tiers=exact,fact&node=a%23b&edge=e3`)).toEqual({
      ...DEFAULT_PARAMS,
      sel: "x",
      tab: "flow",
      hops: 1,
      nm: false,
      tiers: ["fact", "exact"],
      node: "a#b",
      edge: "e3",
    });
  });

  it("ignores unknown parameters and values", () => {
    expect(parseApiParams("#/api?foo=1&transport=grpc&tests=yes&gap=http:linked")).toEqual(DEFAULT_PARAMS);
    expect(parseGap("tauri_ipc:never")).toEqual({ transport: "tauri_ipc", category: "never" });
    expect(parseGap("tauri_ipc:unproven")).toEqual({ transport: "tauri_ipc", category: "unproven" });
    expect(parseGap("http")).toBeNull();
    expect(parseGap("ws:dynamic")).toBeNull();
    expect(parseApiParams("#/api")).toEqual(DEFAULT_PARAMS);
    // Unknown Flow values fall back to the defaults (2 hops, near misses
    // on, every tier); a tiers value naming no known tier is the default.
    expect(parseApiParams("#/api?tab=map&hops=3&nm=no&tiers=dynamic")).toEqual(DEFAULT_PARAMS);
    expect(parseApiParams("#/api?tiers=").tiers).toEqual([]);
  });

  it("reads and writes the peek, leaving it out by default", () => {
    expect(parseApiParams("#/api?sel=x&peek=client_call%3Aa.ts%23b").peek).toBe("client_call:a.ts#b");
    expect(parseApiParams("#/api?sel=x&tab=flow&node=n&peek=0").peek).toBe("0");
    expect(parseApiParams("#/api?peek=").peek).toBeNull();
    expect(apiHash({ ...DEFAULT_PARAMS, sel: "x" })).toBe("#/api?sel=x");
    const p = { ...DEFAULT_PARAMS, sel: "x", peek: "function:a b.ts#c" };
    expect(apiHash(p)).toBe("#/api?sel=x&peek=function%3Aa+b.ts%23c");
    expect(parseApiParams(apiHash(p))).toEqual(p);
    const f = { ...DEFAULT_PARAMS, sel: "x", tab: "flow" as const, node: "n", peek: "0" };
    expect(apiHash(f)).toBe("#/api?sel=x&tab=flow&node=n&peek=0");
    expect(parseApiParams(apiHash(f))).toEqual(f);
  });

  it("says when a hash opens a graph view", () => {
    expect(isGraphView("#/api?sel=x&tab=flow")).toBe(true);
    expect(isGraphView("#/api?tab=flow")).toBe(false);
    expect(isGraphView("#/api?sel=x&tab=overview")).toBe(false);
  });

  it("round-trips through apiHash, leaving defaults out", () => {
    expect(apiHash(DEFAULT_PARAMS)).toBe("#/api");
    const p = {
      ...DEFAULT_PARAMS,
      transport: "tauri_ipc" as const,
      tests: true,
      root: ".",
      sel: "ipc_command:a#b%20c",
      gap: { transport: "tauri_ipc" as const, category: "wrapper" as const },
    };
    expect(parseApiParams(apiHash(p))).toEqual(p);
    const f = { ...p, tab: "flow" as const, hops: 1 as const, nm: false, tiers: ["exact" as const], node: "client_call:x#y z", edge: "c0" };
    expect(parseApiParams(apiHash(f))).toEqual(f);
    expect(parseApiParams(apiHash({ ...f, tiers: [] })).tiers).toEqual([]);
  });
});
