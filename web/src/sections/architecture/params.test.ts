import { describe, expect, it } from "vitest";

import { DEFAULT_SHOW } from "@/sections/architecture/model";
import { ARCH_HASH, archHash, parseArchParams } from "@/sections/architecture/params";

describe("architecture hash", () => {
  it("defaults to the map with libraries, tests and docs hidden and SDK stubs shown", () => {
    expect(parseArchParams("#/architecture")).toEqual({ tab: "map", show: DEFAULT_SHOW, expanded: null, node: null, edge: null, item: null, chain: null, dir: "both", depth: null, at: null });
    expect(archHash(parseArchParams("#/architecture"))).toBe(ARCH_HASH);
  });

  it("round-trips every field", () => {
    const p = { tab: "links" as const, show: { libraries: true, tests: true, docs: false, configured: true, sdk: false }, expanded: "src/backend/base", node: "c:src/backend/base", edge: "e:c:a|c:b", item: "route:x.py#GET%20%2F", chain: "route:x.py#GET%20%2F", dir: "up" as const, depth: 8, at: "function:x.py#f" };
    expect(parseArchParams(archHash(p))).toEqual(p);
  });

  it("drops dir, depth and at without a chain", () => {
    expect(archHash({ ...parseArchParams("#/architecture"), dir: "up", depth: 9, at: "x" })).toBe(ARCH_HASH);
  });

  it("reads only a depth 1 to 10 (T042)", () => {
    expect(parseArchParams("#/architecture?chain=x&depth=10").depth).toBe(10);
    for (const bad of ["0", "11", "-1", "6.5", "x", ""]) expect(parseArchParams(`#/architecture?chain=x&depth=${bad}`).depth).toBeNull();
  });
});
