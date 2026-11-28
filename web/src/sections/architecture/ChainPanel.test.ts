// One end note per chain step when every member carries the same, and
// chain chips above their lines (T072).
import { describe, expect, it } from "vitest";

import { sharedEnds } from "@/sections/architecture/ChainPanel";
import { CHAIN_CHIP_Z, RAISED_LINE_Z } from "@/sections/architecture/Line";

describe("chain line chips", () => {
  it("are painted above the raised lines, so no line strikes through a chip (T072)", () => {
    expect(CHAIN_CHIP_Z).toBeGreaterThan(RAISED_LINE_Z);
  });
});

describe("sharedEnds", () => {
  const note = "? not modelled: no stored caller";
  it("says a note once when every member of the step carries the identical list", () => {
    expect(sharedEnds(["a", "b"], new Map([["a", [note]], ["b", [note]]]))).toEqual([note]);
    expect(sharedEnds(["a", "b", "c"], new Map([["a", [note, "x"]], ["b", [note, "x"]], ["c", [note, "x"]]]))).toEqual([note, "x"]);
  });

  it("keeps the notes under each member when they differ, are missing or the step has one member", () => {
    expect(sharedEnds(["a", "b"], new Map([["a", [note]], ["b", ["other"]]]))).toBeNull();
    expect(sharedEnds(["a", "b"], new Map([["a", [note]]]))).toBeNull();
    expect(sharedEnds(["a", "b"], new Map([["a", [note, "x"]], ["b", ["x", note]]]))).toBeNull();
    expect(sharedEnds(["a"], new Map([["a", [note]]]))).toBeNull();
    expect(sharedEnds(["a", "b"], new Map())).toBeNull();
  });
});
